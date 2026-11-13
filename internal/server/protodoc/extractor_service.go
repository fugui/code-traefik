package protodoc

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"code-traefik/internal/server/decoder"
	"code-traefik/models"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"gorm.io/gorm"
)

// LoadExtraActiveFiles 加载除指定 Schema 外的其余活跃 ProtoSchema 描述集以辅助跨文件类型解析
func LoadExtraActiveFiles(db *gorm.DB, excludeSchemaID uint64) []*protoregistry.Files {
	if db == nil {
		return nil
	}
	var schemas []models.ProtoSchema
	q := db.Where("is_active = ?", true)
	if excludeSchemaID > 0 {
		q = q.Where("id <> ?", excludeSchemaID)
	}
	if err := q.Order("CASE WHEN category = 'BASE_LIB' THEN 0 ELSE 1 END, id ASC").Find(&schemas).Error; err != nil {
		return nil
	}
	var out []*protoregistry.Files
	for _, sc := range schemas {
		if len(sc.DescriptorBytes) == 0 {
			continue
		}
		var fds descriptorpb.FileDescriptorSet
		if err := proto.Unmarshal(sc.DescriptorBytes, &fds); err != nil {
			continue
		}
		if files, err := protodesc.NewFiles(&fds); err == nil {
			out = append(out, files)
		}
	}
	return out
}

// RebuildForSchema 抽取指定 ProtoSchema 的全部文档符号并原子重建 traefik_proto_doc_symbols 索引
func RebuildForSchema(db *gorm.DB, schema *models.ProtoSchema, extraFiles ...*protoregistry.Files) (int, error) {
	if db == nil || schema == nil || schema.ID == 0 {
		return 0, errors.New("db 或 schema 无效，无法重建文档符号")
	}
	if len(extraFiles) == 0 {
		extraFiles = LoadExtraActiveFiles(db, schema.ID)
	}

	symbols, _, err := ExtractSymbols(schema, extraFiles...)
	if err != nil {
		return 0, err
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		if delErr := tx.Where("schema_id = ?", schema.ID).Delete(&models.ProtoDocSymbol{}).Error; delErr != nil {
			return delErr
		}
		if len(symbols) == 0 {
			return nil
		}
		for i := range symbols {
			symbols[i].ID = 0
			symbols[i].SchemaID = schema.ID
		}
		return tx.CreateInBatches(&symbols, 100).Error
	})
	if err != nil {
		return 0, err
	}
	return len(symbols), nil
}

// EnsureSymbolsForSchema 惰性补建尚未建立文档符号的存量 Schema
func EnsureSymbolsForSchema(db *gorm.DB, schema *models.ProtoSchema) {
	if db == nil || schema == nil || schema.ID == 0 || len(schema.DescriptorBytes) == 0 {
		return
	}
	var cnt int64
	if err := db.Model(&models.ProtoDocSymbol{}).Where("schema_id = ?", schema.ID).Count(&cnt).Error; err == nil && cnt == 0 {
		_, _ = RebuildForSchema(db, schema)
	}
}

type serviceSubsystemInfo struct {
	SubsystemID   uint64
	SubsystemName string
}

func resolveServiceSubsystemMap(db *gorm.DB, serviceNames []string) map[string]serviceSubsystemInfo {
	out := make(map[string]serviceSubsystemInfo)
	if db == nil || len(serviceNames) == 0 {
		return out
	}

	// 1. 先按 ModuleService 认领记录解析子系统归属
	var modServices []models.ModuleService
	if err := db.Preload("Module.Subsystem").
		Where("service_name IN ?", serviceNames).
		Order("id ASC").
		Find(&modServices).Error; err == nil {
		for _, ms := range modServices {
			if ms.Module != nil && ms.Module.Subsystem != nil {
				if _, exists := out[ms.ServiceName]; !exists {
					out[ms.ServiceName] = serviceSubsystemInfo{
						SubsystemID:   ms.Module.Subsystem.ID,
						SubsystemName: ms.Module.Subsystem.Name,
					}
				}
			}
		}
	}

	// 2. 再补充 ServiceContract 定义方的子系统归属
	var contracts []models.ServiceContract
	if err := db.Preload("DefinerModule.Subsystem").
		Where("service_name IN ?", serviceNames).
		Find(&contracts).Error; err == nil {
		for _, ct := range contracts {
			if ct.DefinerModule != nil && ct.DefinerModule.Subsystem != nil {
				if _, exists := out[ct.ServiceName]; !exists {
					out[ct.ServiceName] = serviceSubsystemInfo{
						SubsystemID:   ct.DefinerModule.Subsystem.ID,
						SubsystemName: ct.DefinerModule.Subsystem.Name,
					}
				}
			}
		}
	}
	return out
}

// BuildDocTree 按子系统 -> 制品包 -> Package -> Service -> Method 聚合构造活体文档导航树
func BuildDocTree(db *gorm.DB, query DocTreeQuery) (*ProtoDocTreeResponse, error) {
	if db == nil {
		return nil, errors.New("db 不能为空")
	}

	var schemas []models.ProtoSchema
	schemaQ := db.Model(&models.ProtoSchema{})
	if query.SchemaID != nil && *query.SchemaID > 0 {
		schemaQ = schemaQ.Where("id = ?", *query.SchemaID)
	} else {
		schemaQ = schemaQ.Where("is_active = ?", true)
	}
	if strings.TrimSpace(query.Category) != "" {
		schemaQ = schemaQ.Where("category = ?", strings.TrimSpace(query.Category))
	}
	if err := schemaQ.Order("category ASC, name ASC, version DESC").Find(&schemas).Error; err != nil {
		return nil, err
	}

	resp := &ProtoDocTreeResponse{
		Items: make([]ProtoDocTreeNodeDTO, 0),
	}
	if len(schemas) == 0 {
		return resp, nil
	}

	schemaByID := make(map[uint64]models.ProtoSchema, len(schemas))
	schemaIDs := make([]uint64, 0, len(schemas))
	for i := range schemas {
		EnsureSymbolsForSchema(db, &schemas[i])
		schemaByID[schemas[i].ID] = schemas[i]
		schemaIDs = append(schemaIDs, schemas[i].ID)
	}

	var allSymbols []models.ProtoDocSymbol
	if err := db.Where("schema_id IN ?", schemaIDs).
		Order("schema_id ASC, package_name ASC, proto_file ASC, start_line ASC, full_name ASC").
		Find(&allSymbols).Error; err != nil {
		return nil, err
	}

	// 若指定了 search，则使用参数化 SQL 检索匹配任意层级的符号集合，定位命中的 Schema/Service
	var searchMatchedServices map[string]bool
	var searchMatchedSchemas map[uint64]bool
	trimmedSearch := strings.TrimSpace(query.Search)
	if trimmedSearch != "" {
		searchMatchedServices = make(map[string]bool)
		searchMatchedSchemas = make(map[uint64]bool)
		pattern := "%" + strings.ToLower(trimmedSearch) + "%"

		var matchedSyms []models.ProtoDocSymbol
		if err := db.Where("schema_id IN ?", schemaIDs).
			Where(
				"LOWER(full_name) LIKE ? OR LOWER(short_name) LIKE ? OR LOWER(summary) LIKE ? OR LOWER(leading_comments) LIKE ? OR LOWER(trailing_comments) LIKE ? OR LOWER(owner_tag) LIKE ? OR LOWER(error_codes_json::text) LIKE ? OR LOWER(fields_meta_json::text) LIKE ?",
				pattern, pattern, pattern, pattern, pattern, pattern, pattern, pattern,
			).
			Find(&matchedSyms).Error; err != nil {
			return nil, err
		}

		// 若制品名匹配搜索词，也纳入
		for _, sc := range schemas {
			if strings.Contains(strings.ToLower(sc.Name), strings.ToLower(trimmedSearch)) {
				searchMatchedSchemas[sc.ID] = true
			}
		}
		for _, ms := range matchedSyms {
			searchMatchedSchemas[ms.SchemaID] = true
			switch ms.SymbolKind {
			case models.DocSymbolService:
				searchMatchedServices[fmt.Sprintf("%d::%s", ms.SchemaID, ms.FullName)] = true
			case models.DocSymbolMethod:
				svcFull := parentServiceOfMethod(ms.FullName)
				if svcFull != "" {
					searchMatchedServices[fmt.Sprintf("%d::%s", ms.SchemaID, svcFull)] = true
				}
			}
		}
	}

	svcNamesSet := make(map[string]struct{})
	hasSourceMap := make(map[uint64]bool)
	servicesBySchema := make(map[uint64][]models.ProtoDocSymbol)
	methodsBySvcKey := make(map[string][]models.ProtoDocSymbol)

	for _, sym := range allSymbols {
		if sym.StartLine > 0 || sym.LeadingComments != "" {
			hasSourceMap[sym.SchemaID] = true
		}
		switch sym.SymbolKind {
		case models.DocSymbolService:
			svcNamesSet[sym.FullName] = struct{}{}
			servicesBySchema[sym.SchemaID] = append(servicesBySchema[sym.SchemaID], sym)
		case models.DocSymbolMethod:
			svcFull := parentServiceOfMethod(sym.FullName)
			key := fmt.Sprintf("%d::%s", sym.SchemaID, svcFull)
			methodsBySvcKey[key] = append(methodsBySvcKey[key], sym)
		}
	}

	svcNames := make([]string, 0, len(svcNamesSet))
	for name := range svcNamesSet {
		svcNames = append(svcNames, name)
	}
	subMap := resolveServiceSubsystemMap(db, svcNames)

	type subBucket struct {
		ID      uint64
		Name    string
		Schemas map[uint64]*schemaBucket
	}
	subBuckets := make(map[uint64]*subBucket)
	getOrCreateSub := func(subID uint64, subName string) *subBucket {
		if b, ok := subBuckets[subID]; ok {
			return b
		}
		b := &subBucket{ID: subID, Name: subName, Schemas: make(map[uint64]*schemaBucket)}
		subBuckets[subID] = b
		return b
	}

	for _, sc := range schemas {
		svcs := servicesBySchema[sc.ID]
		// 若该制品没有 SERVICE（如纯 BASE_LIB 公共类型包），在未限定 search 或命中制品搜索时保留入口展示
		if len(svcs) == 0 && (trimmedSearch == "" || searchMatchedSchemas[sc.ID]) {
			subID := uint64(0)
			subName := "未认领公共与基础契约"
			if query.SubsystemID != nil && *query.SubsystemID != subID {
				continue
			}
			sb := getOrCreateSub(subID, subName)
			schBucket := getOrCreateSchemaBucket(sb.Schemas, sc, hasSourceMap[sc.ID])
			pkg := "base"
			schBucket.addEmptyPackageIfNeeded(pkg)
			continue
		}

		for _, svc := range svcs {
			svcKey := fmt.Sprintf("%d::%s", sc.ID, svc.FullName)
			if !query.IncludeDeprecated && svc.IsDeprecated {
				continue
			}

			subID := uint64(0)
			subName := "未认领公共与基础契约"
			if info, ok := subMap[svc.FullName]; ok {
				subID = info.SubsystemID
				subName = info.SubsystemName
			}
			if trimmedSearch != "" {
				subMatch := strings.Contains(strings.ToLower(subName), strings.ToLower(trimmedSearch))
				schemaMatch := strings.Contains(strings.ToLower(sc.Name), strings.ToLower(trimmedSearch))
				if !subMatch && !schemaMatch && !searchMatchedServices[svcKey] && !searchMatchedSchemas[sc.ID] {
					continue
				}
				// 如果 Schema 内只有部分 Service 命中且 Schema/Sub 名没命中，精确收敛到命中的 Service
				if !subMatch && !schemaMatch && len(searchMatchedServices) > 0 && !searchMatchedServices[svcKey] {
					continue
				}
			}

			if query.SubsystemID != nil && *query.SubsystemID != subID {
				continue
			}

			sb := getOrCreateSub(subID, subName)
			schBucket := getOrCreateSchemaBucket(sb.Schemas, sc, hasSourceMap[sc.ID])

			methods := methodsBySvcKey[svcKey]
			filteredMethods := make([]models.ProtoDocSymbol, 0, len(methods))
			depMethodCnt := 0
			for _, m := range methods {
				if m.IsDeprecated {
					depMethodCnt++
				}
				if !query.IncludeDeprecated && m.IsDeprecated {
					continue
				}
				filteredMethods = append(filteredMethods, m)
			}

			resp.TotalServices++
			resp.TotalMethods += len(filteredMethods)
			if svc.IsDeprecated {
				resp.DeprecatedCount++
			}
			resp.DeprecatedCount += depMethodCnt

			pkgName := svc.PackageName
			if pkgName == "" {
				pkgName = "default"
			}
			schBucket.addService(pkgName, svc, filteredMethods)
		}
	}

	subIDs := make([]uint64, 0, len(subBuckets))
	for id := range subBuckets {
		subIDs = append(subIDs, id)
	}
	sort.Slice(subIDs, func(i, j int) bool {
		if subIDs[i] == 0 {
			return false
		}
		if subIDs[j] == 0 {
			return true
		}
		return subBuckets[subIDs[i]].Name < subBuckets[subIDs[j]].Name
	})

	for _, id := range subIDs {
		sb := subBuckets[id]
		var subIDPtr *uint64
		if id > 0 {
			v := id
			subIDPtr = &v
		}
		subNode := ProtoDocTreeNodeDTO{
			NodeKey:       fmt.Sprintf("sub:%d", id),
			NodeType:      "SUBSYSTEM",
			Label:         sb.Name,
			SubsystemID:   subIDPtr,
			HasSourceInfo: true,
			Children:      make([]ProtoDocTreeNodeDTO, 0, len(sb.Schemas)),
		}

		schIDs := make([]uint64, 0, len(sb.Schemas))
		for sid := range sb.Schemas {
			schIDs = append(schIDs, sid)
		}
		sort.Slice(schIDs, func(i, j int) bool {
			return sb.Schemas[schIDs[i]].Schema.Name < sb.Schemas[schIDs[j]].Schema.Name
		})
		for _, sid := range schIDs {
			schemaNode := sb.Schemas[sid].toDTO()
			subNode.MethodCount += schemaNode.MethodCount
			subNode.DeprecatedCount += schemaNode.DeprecatedCount
			subNode.Children = append(subNode.Children, schemaNode)
		}
		resp.Items = append(resp.Items, subNode)
	}

	return resp, nil
}

type schemaBucket struct {
	Schema        models.ProtoSchema
	HasSourceInfo bool
	Packages      map[string][]ProtoDocTreeNodeDTO
}

func getOrCreateSchemaBucket(m map[uint64]*schemaBucket, sc models.ProtoSchema, hasSource bool) *schemaBucket {
	if b, ok := m[sc.ID]; ok {
		return b
	}
	b := &schemaBucket{
		Schema:        sc,
		HasSourceInfo: hasSource,
		Packages:      make(map[string][]ProtoDocTreeNodeDTO),
	}
	m[sc.ID] = b
	return b
}

func (b *schemaBucket) addEmptyPackageIfNeeded(pkg string) {
	if _, ok := b.Packages[pkg]; !ok {
		b.Packages[pkg] = []ProtoDocTreeNodeDTO{}
	}
}

func (b *schemaBucket) addService(pkg string, svc models.ProtoDocSymbol, methods []models.ProtoDocSymbol) {
	scID := b.Schema.ID
	mNodes := make([]ProtoDocTreeNodeDTO, 0, len(methods))
	depCnt := 0
	for _, m := range methods {
		if m.IsDeprecated {
			depCnt++
		}
		mNodes = append(mNodes, ProtoDocTreeNodeDTO{
			NodeKey:       fmt.Sprintf("method:%d:%s", scID, m.FullName),
			NodeType:      "METHOD",
			Label:         m.ShortName,
			SchemaID:      &scID,
			SchemaName:    b.Schema.Name,
			ServiceName:   svc.FullName,
			Operation:     m.ShortName,
			Summary:       m.Summary,
			IsDeprecated:  m.IsDeprecated,
			HasSourceInfo: b.HasSourceInfo,
		})
	}
	b.Packages[pkg] = append(b.Packages[pkg], ProtoDocTreeNodeDTO{
		NodeKey:         fmt.Sprintf("svc:%d:%s", scID, svc.FullName),
		NodeType:        "SERVICE",
		Label:           svc.ShortName,
		SchemaID:        &scID,
		SchemaName:      b.Schema.Name,
		Category:        b.Schema.Category,
		ServiceName:     svc.FullName,
		Summary:         svc.Summary,
		IsDeprecated:    svc.IsDeprecated,
		MethodCount:     len(mNodes),
		DeprecatedCount: depCnt,
		HasSourceInfo:   b.HasSourceInfo,
		Children:        mNodes,
	})
}

func (b *schemaBucket) toDTO() ProtoDocTreeNodeDTO {
	scID := b.Schema.ID
	schNode := ProtoDocTreeNodeDTO{
		NodeKey:       fmt.Sprintf("schema:%d", scID),
		NodeType:      "SCHEMA",
		Label:         fmt.Sprintf("%s (v%d)", b.Schema.Name, b.Schema.Version),
		SchemaID:      &scID,
		SchemaName:    b.Schema.Name,
		Category:      b.Schema.Category,
		HasSourceInfo: b.HasSourceInfo,
		Children:      make([]ProtoDocTreeNodeDTO, 0, len(b.Packages)),
	}

	pkgs := make([]string, 0, len(b.Packages))
	for p := range b.Packages {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)

	for _, p := range pkgs {
		svcs := b.Packages[p]
		pkgNode := ProtoDocTreeNodeDTO{
			NodeKey:       fmt.Sprintf("pkg:%d:%s", scID, p),
			NodeType:      "PACKAGE",
			Label:         p,
			SchemaID:      &scID,
			SchemaName:    b.Schema.Name,
			HasSourceInfo: b.HasSourceInfo,
			Children:      svcs,
		}
		for _, s := range svcs {
			pkgNode.MethodCount += s.MethodCount
			pkgNode.DeprecatedCount += s.DeprecatedCount
			if s.IsDeprecated {
				pkgNode.DeprecatedCount++
			}
		}
		schNode.MethodCount += pkgNode.MethodCount
		schNode.DeprecatedCount += pkgNode.DeprecatedCount
		schNode.Children = append(schNode.Children, pkgNode)
	}
	return schNode
}

func parentServiceOfMethod(methodFullName string) string {
	idx := strings.LastIndex(methodFullName, ".")
	if idx <= 0 {
		return ""
	}
	return methodFullName[:idx]
}

// GetServiceDocDetail 查询单服务的完整活体文档详情、黄金样本、Enum 字典与 ADR-16 血缘矩阵
func GetServiceDocDetail(db *gorm.DB, serviceName string, schemaID *uint64) (*ProtoServiceDocDetailDTO, error) {
	if db == nil {
		return nil, errors.New("db 不能为空")
	}
	trimmedSvc := strings.TrimSpace(serviceName)
	if trimmedSvc == "" {
		return nil, ErrServiceDocNotFound
	}

	var schema models.ProtoSchema
	var svcSymbol models.ProtoDocSymbol

	if schemaID != nil && *schemaID > 0 {
		if err := db.First(&schema, *schemaID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrServiceDocNotFound
			}
			return nil, err
		}
		EnsureSymbolsForSchema(db, &schema)
		if err := db.Where("schema_id = ? AND symbol_kind = ? AND full_name = ?",
			schema.ID, models.DocSymbolService, trimmedSvc).
			First(&svcSymbol).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrServiceDocNotFound
			}
			return nil, err
		}
	} else {
		var syms []models.ProtoDocSymbol
		if err := db.Joins("JOIN traefik_proto_schemas s ON s.id = traefik_proto_doc_symbols.schema_id").
			Where("s.is_active = ? AND traefik_proto_doc_symbols.symbol_kind = ? AND traefik_proto_doc_symbols.full_name = ?",
				true, models.DocSymbolService, trimmedSvc).
			Order("s.id DESC").
			Limit(1).
			Find(&syms).Error; err != nil {
			return nil, err
		}
		if len(syms) > 0 {
			svcSymbol = syms[0]
			if err := db.First(&schema, svcSymbol.SchemaID).Error; err != nil {
				return nil, err
			}
		} else {
			// 惰性触发所有活跃 Schema 符号提取后再试一次
			var activeSchemas []models.ProtoSchema
			if err := db.Where("is_active = ?", true).Find(&activeSchemas).Error; err != nil {
				return nil, err
			}
			for i := range activeSchemas {
				EnsureSymbolsForSchema(db, &activeSchemas[i])
			}
			if err := db.Joins("JOIN traefik_proto_schemas s ON s.id = traefik_proto_doc_symbols.schema_id").
				Where("s.is_active = ? AND traefik_proto_doc_symbols.symbol_kind = ? AND traefik_proto_doc_symbols.full_name = ?",
					true, models.DocSymbolService, trimmedSvc).
				Limit(1).
				First(&svcSymbol).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, ErrServiceDocNotFound
				}
				return nil, err
			}
			if err := db.First(&schema, svcSymbol.SchemaID).Error; err != nil {
				return nil, err
			}
		}
	}

	var allSchemaSyms []models.ProtoDocSymbol
	if err := db.Where("schema_id = ?", schema.ID).
		Order("start_line ASC, full_name ASC").
		Find(&allSchemaSyms).Error; err != nil {
		return nil, err
	}

	extraFiles := LoadExtraActiveFiles(db, schema.ID)
	reg, _, _ := buildRegistryFromSchema(&schema, extraFiles)

	var svcMeta serviceMetaStored
	_ = json.Unmarshal(svcSymbol.FieldsMetaJSON, &svcMeta)
	var svcErrs []ErrorCodeDoc
	_ = json.Unmarshal(svcSymbol.ErrorCodesJSON, &svcErrs)
	if svcErrs == nil {
		svcErrs = []ErrorCodeDoc{}
	}

	hasSourceInfo := svcSymbol.StartLine > 0 || svcSymbol.LeadingComments != "" || svcMeta.FileComments != ""

	msgMetaByFull := make(map[string]MessageDocMeta)
	enumSymByFull := make(map[string]EnumDocDTO)
	var methodSyms []models.ProtoDocSymbol

	for _, sym := range allSchemaSyms {
		if sym.StartLine > 0 || sym.LeadingComments != "" {
			hasSourceInfo = true
		}
		switch sym.SymbolKind {
		case models.DocSymbolMethod:
			if strings.HasPrefix(sym.FullName, svcSymbol.FullName+".") {
				methodSyms = append(methodSyms, sym)
			}
		case models.DocSymbolMessage:
			var mm MessageDocMeta
			if err := json.Unmarshal(sym.FieldsMetaJSON, &mm); err == nil {
				msgMetaByFull[sym.FullName] = mm
			}
		case models.DocSymbolEnum:
			var vals []EnumValueDoc
			_ = json.Unmarshal(sym.FieldsMetaJSON, &vals)
			if vals == nil {
				vals = []EnumValueDoc{}
			}
			enumSymByFull[sym.FullName] = EnumDocDTO{
				FullName:          sym.FullName,
				ShortName:         sym.ShortName,
				ProtoFile:         sym.ProtoFile,
				StartLine:         sym.StartLine,
				EndLine:           sym.EndLine,
				Comment:           combineLeadingTrailing(sym.LeadingComments, sym.TrailingComments),
				IsDeprecated:      sym.IsDeprecated,
				DeprecationReason: sym.DeprecationReason,
				Values:            vals,
			}
		}
	}

	methods := make([]MethodDocDTO, 0, len(methodSyms))
	referencedEnums := make(map[string]EnumDocDTO)
	referencedMsgs := make(map[string]MessageDocMeta)

	for _, ms := range methodSyms {
		var mm methodMetaStored
		_ = json.Unmarshal(ms.FieldsMetaJSON, &mm)
		var mErrs []ErrorCodeDoc
		_ = json.Unmarshal(ms.ErrorCodesJSON, &mErrs)
		if mErrs == nil {
			mErrs = []ErrorCodeDoc{}
		}

		reqDesc := findMsgDescriptor(reg, mm.ReqMessageFullName)
		rspDesc := findMsgDescriptor(reg, mm.RspMessageFullName)

		reqFields := resolveFieldsForMessage(mm.ReqMessageFullName, reqDesc, msgMetaByFull)
		rspFields := resolveFieldsForMessage(mm.RspMessageFullName, rspDesc, msgMetaByFull)

		collectReferencedFromFields(reqFields, enumSymByFull, msgMetaByFull, reg, referencedEnums, referencedMsgs)
		collectReferencedFromFields(rspFields, enumSymByFull, msgMetaByFull, reg, referencedEnums, referencedMsgs)
		if stored, ok := msgMetaByFull[mm.ReqMessageFullName]; ok {
			referencedMsgs[mm.ReqMessageFullName] = stored
		}
		if stored, ok := msgMetaByFull[mm.RspMessageFullName]; ok {
			referencedMsgs[mm.RspMessageFullName] = stored
		}

		defReq := json.RawMessage(ms.DefaultRequestJSON)
		if (len(defReq) == 0 || string(defReq) == "null") && reqDesc != nil {
			if raw, err := BuildDefaultMessageJSON(reqDesc); err == nil {
				defReq = raw
			}
		}
		defRsp := json.RawMessage(ms.DefaultResponseJSON)
		if (len(defRsp) == 0 || string(defRsp) == "null") && rspDesc != nil {
			if raw, err := BuildDefaultMessageJSON(rspDesc); err == nil {
				defRsp = raw
			}
		}

		goldenSamples := MatchGoldenSamples(db, svcSymbol.FullName, ms.ShortName, reqDesc, rspDesc)

		methods = append(methods, MethodDocDTO{
			Operation:          ms.ShortName,
			FullName:           ms.FullName,
			ProtoFile:          ms.ProtoFile,
			StartLine:          ms.StartLine,
			EndLine:            ms.EndLine,
			Summary:            ms.Summary,
			LeadingComments:    ms.LeadingComments,
			TrailingComments:   ms.TrailingComments,
			IsDeprecated:       ms.IsDeprecated,
			DeprecationReason:  ms.DeprecationReason,
			OwnerTag:           ms.OwnerTag,
			ErrorCodes:         mErrs,
			ReqMessageFullName: mm.ReqMessageFullName,
			RspMessageFullName: mm.RspMessageFullName,
			ClientStreaming:    mm.ClientStreaming,
			ServerStreaming:    mm.ServerStreaming,
			ReqFields:          reqFields,
			RspFields:          rspFields,
			DefaultReqJSON:     defReq,
			DefaultRspJSON:     defRsp,
			GoldenSamples:      goldenSamples,
		})
	}

	// 同包同文件的其余 Enum 也纳入字典（便于查阅状态定义）
	for full, ed := range enumSymByFull {
		if ed.ProtoFile == svcSymbol.ProtoFile || strings.HasPrefix(full, svcSymbol.PackageName+".") {
			if _, exists := referencedEnums[full]; !exists {
				referencedEnums[full] = ed
			}
		}
	}

	enumsList := make([]EnumDocDTO, 0, len(referencedEnums))
	for _, ed := range referencedEnums {
		enumsList = append(enumsList, ed)
	}
	sort.Slice(enumsList, func(i, j int) bool {
		return enumsList[i].FullName < enumsList[j].FullName
	})

	refMsgsList := make([]MessageDocMeta, 0, len(referencedMsgs))
	for _, m := range referencedMsgs {
		refMsgsList = append(refMsgsList, m)
	}
	sort.Slice(refMsgsList, func(i, j int) bool {
		return refMsgsList[i].FullName < refMsgsList[j].FullName
	})

	lineage := ResolveServiceLineage(db, svcSymbol.FullName)

	return &ProtoServiceDocDetailDTO{
		SchemaID:          schema.ID,
		SchemaName:        schema.Name,
		SchemaVersion:     schema.Version,
		SchemaCategory:    schema.Category,
		SourceType:        schema.SourceType,
		HasSourceInfo:     hasSourceInfo,
		PackageName:       svcSymbol.PackageName,
		ServiceName:       svcSymbol.FullName,
		ShortName:         svcSymbol.ShortName,
		ProtoFile:         svcSymbol.ProtoFile,
		StartLine:         svcSymbol.StartLine,
		EndLine:           svcSymbol.EndLine,
		FileComments:      svcMeta.FileComments,
		Summary:           svcSymbol.Summary,
		LeadingComments:   svcSymbol.LeadingComments,
		TrailingComments:  svcSymbol.TrailingComments,
		IsDeprecated:      svcSymbol.IsDeprecated,
		DeprecationReason: svcSymbol.DeprecationReason,
		OwnerTag:          svcSymbol.OwnerTag,
		ErrorCodes:        svcErrs,
		Methods:           methods,
		Enums:             enumsList,
		ReferencedTypes:   refMsgsList,
		Lineage:           lineage,
	}, nil
}

func findMsgDescriptor(reg *protoregistry.Files, fullName string) protoreflect.MessageDescriptor {
	if reg == nil || fullName == "" {
		return nil
	}
	d, err := reg.FindDescriptorByName(protoreflect.FullName(strings.TrimPrefix(fullName, ".")))
	if err != nil {
		return nil
	}
	md, _ := d.(protoreflect.MessageDescriptor)
	return md
}

func resolveFieldsForMessage(
	fullName string,
	desc protoreflect.MessageDescriptor,
	stored map[string]MessageDocMeta,
) []FieldDocNode {
	if mm, ok := stored[fullName]; ok && len(mm.Fields) > 0 {
		return mm.Fields
	}
	if desc != nil {
		return ExtractMessageFieldTree(desc, nil, MaxDocFieldDepth)
	}
	return []FieldDocNode{}
}

func collectReferencedFromFields(
	fields []FieldDocNode,
	enumByFull map[string]EnumDocDTO,
	msgByFull map[string]MessageDocMeta,
	reg *protoregistry.Files,
	outEnums map[string]EnumDocDTO,
	outMsgs map[string]MessageDocMeta,
) {
	for _, f := range fields {
		if len(f.EnumValues) > 0 && f.TypeFullName != "" {
			if existing, ok := enumByFull[f.TypeFullName]; ok {
				outEnums[f.TypeFullName] = existing
			} else {
				var cmt string
				if reg != nil {
					if d, err := reg.FindDescriptorByName(protoreflect.FullName(f.TypeFullName)); err == nil {
						lm := lookupDescriptorLocation(d)
						cmt = combineLeadingTrailing(lm.Leading, lm.Trailing)
					}
				}
				outEnums[f.TypeFullName] = EnumDocDTO{
					FullName:  f.TypeFullName,
					ShortName: f.Type,
					ProtoFile: f.ProtoFile,
					Comment:   cmt,
					Values:    f.EnumValues,
				}
			}
		}
		if f.TypeFullName != "" {
			if mm, ok := msgByFull[f.TypeFullName]; ok {
				outMsgs[f.TypeFullName] = mm
			}
		}
		if len(f.Children) > 0 {
			collectReferencedFromFields(f.Children, enumByFull, msgByFull, reg, outEnums, outMsgs)
		}
	}
}

// ResolveServiceLineage 查询服务的 ADR-16 公共契约定义方、多实现模块及下游依赖消费方
func ResolveServiceLineage(db *gorm.DB, serviceName string) ServiceContractLineageDTO {
	lineage := ServiceContractLineageDTO{
		ClaimStatus:     "UNCLAIMED",
		Implementations: make([]ServiceImplementationDTO, 0),
		Consumers:       make([]ServiceConsumerDTO, 0),
	}
	if db == nil || serviceName == "" {
		return lineage
	}

	var contract models.ServiceContract
	hasContract := false
	if err := db.Preload("DefinerModule.Subsystem").
		Where("service_name = ?", serviceName).
		First(&contract).Error; err == nil {
		hasContract = true
		lineage.ClaimStatus = "CONTRACT"
		def := &ContractDefinerDTO{
			ContractID:      contract.ID,
			ServiceName:     contract.ServiceName,
			DefinerModuleID: contract.DefinerModuleID,
			ProvideMode:     contract.ProvideMode,
			ImplKeyLabel:    contract.ImplKeyLabel,
		}
		if contract.DefinerModule != nil {
			def.ModuleCode = contract.DefinerModule.Code
			def.ModuleName = contract.DefinerModule.Name
			if contract.DefinerModule.Subsystem != nil {
				def.SubsystemID = contract.DefinerModule.Subsystem.ID
				def.SubsystemName = contract.DefinerModule.Subsystem.Name
			}
		}
		lineage.Definer = def
	}

	var impls []models.ModuleService
	q := db.Preload("Module.Subsystem").Preload("Module.ArchProcess").Preload("Module.Owner")
	if hasContract {
		q = q.Where("contract_id = ? OR service_name = ?", contract.ID, serviceName)
	} else {
		q = q.Where("service_name = ?", serviceName)
	}
	if err := q.Order("impl_key ASC, id ASC").Find(&impls).Error; err == nil && len(impls) > 0 {
		if !hasContract {
			lineage.ClaimStatus = "SINGLE_IMPL"
		}
		for _, im := range impls {
			dto := ServiceImplementationDTO{
				ServiceID:           im.ID,
				ModuleID:            im.ModuleID,
				ImplKey:             im.ImplKey,
				IsCustomServiceName: im.IsCustomServiceName,
				ServiceName:         im.ServiceName,
			}
			if im.Module != nil {
				dto.ModuleCode = im.Module.Code
				dto.ModuleName = im.Module.Name
				if im.Module.Subsystem != nil {
					dto.SubsystemID = im.Module.Subsystem.ID
					dto.SubsystemName = im.Module.Subsystem.Name
				}
				if im.Module.ArchProcess != nil {
					dto.ArchProcessCode = im.Module.ArchProcess.Code
				}
				if im.Module.Owner != nil {
					if im.Module.Owner.Name != "" {
						dto.OwnerName = im.Module.Owner.Name
					} else {
						dto.OwnerName = im.Module.Owner.Username
					}
				}
			}
			lineage.Implementations = append(lineage.Implementations, dto)
		}
	}

	serviceIDs := make([]uint64, 0, len(impls))
	for _, im := range impls {
		serviceIDs = append(serviceIDs, im.ID)
	}

	if hasContract || len(serviceIDs) > 0 {
		var deps []models.ModuleDependency
		depQ := db.Preload("ConsumerModule.Subsystem").Preload("ConsumerModule.ArchProcess")
		switch {
		case hasContract && len(serviceIDs) > 0:
			depQ = depQ.Where("contract_id = ? OR service_id IN ?", contract.ID, serviceIDs)
		case hasContract:
			depQ = depQ.Where("contract_id = ?", contract.ID)
		default:
			depQ = depQ.Where("service_id IN ?", serviceIDs)
		}
		if err := depQ.Order("id ASC").Find(&deps).Error; err == nil {
			seenMod := make(map[uint64]struct{})
			for _, dep := range deps {
				if dep.ConsumerModule == nil {
					continue
				}
				if _, ok := seenMod[dep.ConsumerModule.ID]; ok {
					continue
				}
				seenMod[dep.ConsumerModule.ID] = struct{}{}
				depType := "DEPEND"
				if dep.ContractID != nil {
					depType = "DISPATCH"
				}
				dto := ServiceConsumerDTO{
					ModuleID:   dep.ConsumerModule.ID,
					ModuleCode: dep.ConsumerModule.Code,
					ModuleName: dep.ConsumerModule.Name,
					DepType:    depType,
				}
				if dep.ConsumerModule.Subsystem != nil {
					dto.SubsystemID = dep.ConsumerModule.Subsystem.ID
					dto.SubsystemName = dep.ConsumerModule.Subsystem.Name
				}
				if dep.ConsumerModule.ArchProcess != nil {
					dto.ArchProcessCode = dep.ConsumerModule.ArchProcess.Code
				}
				lineage.Consumers = append(lineage.Consumers, dto)
			}
		}
	}

	return lineage
}

// ExportMarkdown 按 service_name / schema_id / subsystem_id 范围生成离线 Markdown 手册及安全文件名
func ExportMarkdown(db *gorm.DB, req ExportMarkdownRequest) (string, string, error) {
	if db == nil {
		return "", "", errors.New("db 不能为空")
	}
	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format != "" && format != "markdown" && format != "md" {
		return "", "", ErrInvalidExportFormat
	}
	trimmedSvc := strings.TrimSpace(req.ServiceName)
	if trimmedSvc == "" && req.SchemaID == nil && req.SubsystemID == nil {
		return "", "", ErrMissingExportScope
	}

	var details []*ProtoServiceDocDetailDTO
	var filenameBase string
	var title string

	if trimmedSvc != "" {
		detail, err := GetServiceDocDetail(db, trimmedSvc, req.SchemaID)
		if err != nil {
			return "", "", err
		}
		details = append(details, detail)
		filenameBase = trimmedSvc
		title = trimmedSvc + " 接口参考手册"
	} else {
		tree, err := BuildDocTree(db, DocTreeQuery{
			SchemaID:          req.SchemaID,
			SubsystemID:       req.SubsystemID,
			IncludeDeprecated: true,
		})
		if err != nil {
			return "", "", err
		}
		type targetSvc struct {
			name     string
			schemaID uint64
		}
		var targets []targetSvc
		walkTreeServices(tree.Items, func(node ProtoDocTreeNodeDTO) {
			if node.NodeType == "SERVICE" && node.ServiceName != "" && node.SchemaID != nil {
				targets = append(targets, targetSvc{name: node.ServiceName, schemaID: *node.SchemaID})
			}
		})
		if len(targets) == 0 {
			return "", "", ErrServiceDocNotFound
		}
		for _, tgt := range targets {
			sid := tgt.schemaID
			d, err := GetServiceDocDetail(db, tgt.name, &sid)
			if err == nil {
				details = append(details, d)
			}
		}
		if len(details) == 0 {
			return "", "", ErrServiceDocNotFound
		}
		if req.SchemaID != nil {
			filenameBase = details[0].SchemaName + "-docs"
			title = details[0].SchemaName + " 接口参考手册"
		} else {
			filenameBase = fmt.Sprintf("subsystem-%d-docs", *req.SubsystemID)
			title = "子系统接口参考手册"
		}
	}

	md := RenderServicesMarkdown(details, title)
	return sanitizeFilename(filenameBase) + ".md", md, nil
}

func walkTreeServices(nodes []ProtoDocTreeNodeDTO, visit func(ProtoDocTreeNodeDTO)) {
	for _, n := range nodes {
		visit(n)
		if len(n.Children) > 0 {
			walkTreeServices(n.Children, visit)
		}
	}
}

func sanitizeFilename(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	res := strings.Trim(b.String(), ".-")
	if res == "" {
		return "proto-reference"
	}
	return res
}

// ReconstructProtoSources 在 M25 Git 原文库上线前，从 ProtoSchema 描述集与 SourceCodeInfo 确定性重构只读源码及行号锚点
func ReconstructProtoSources(schema *models.ProtoSchema, symbols []models.ProtoDocSymbol) ([]ReconstructedProtoFile, error) {
	if schema == nil || len(schema.DescriptorBytes) == 0 {
		return nil, errors.New("schema 或 DescriptorBytes 为空")
	}

	ownFiles, err := decoder.ExtractOwnFilesFromSchema(schema)
	if err != nil {
		return nil, err
	}

	symsByFile := make(map[string][]models.ProtoDocSymbol)
	for _, sym := range symbols {
		symsByFile[sym.ProtoFile] = append(symsByFile[sym.ProtoFile], sym)
	}

	out := make([]ReconstructedProtoFile, 0, len(ownFiles))
	for idx, fdProto := range ownFiles {
		locMap, hasInfo := buildLocationMap(fdProto)
		lines := make([]string, 0, 64)
		anchors := make([]ProtoSymbolAnchorDTO, 0)

		appendLine := func(line string) int {
			lines = append(lines, line)
			return len(lines)
		}
		padToLine := func(targetLine int) {
			if targetLine <= 1 {
				return
			}
			for len(lines)+1 < targetLine {
				lines = append(lines, "")
			}
		}

		syntax := fdProto.GetSyntax()
		if syntax == "" {
			syntax = "proto3"
		}
		appendLine(fmt.Sprintf("syntax = %q;", syntax))
		if pkg := fdProto.GetPackage(); pkg != "" {
			appendLine(fmt.Sprintf("package %s;", pkg))
		}
		for _, dep := range fdProto.Dependency {
			appendLine(fmt.Sprintf("import %q;", dep))
		}

		writeCommentLines := func(comment string) {
			if comment == "" {
				return
			}
			for _, cl := range strings.Split(comment, "\n") {
				appendLine("// " + cl)
			}
		}

		// 查找 DB 中是否有匹配锚点（以符号表 StartLine 为优先参考）
		targetStartMap := make(map[string]int)
		for _, s := range symsByFile[fdProto.GetName()] {
			if s.StartLine > 0 {
				targetStartMap[s.SymbolKind+":"+s.FullName] = s.StartLine
			}
		}

		pkgName := fdProto.GetPackage()

		// 1. 顶层 Enum
		for eIdx, ed := range fdProto.EnumType {
			ePath := []int32{5, int32(eIdx)}
			lm := locMap[formatLocPath(ePath)]
			full := joinFullName(pkgName, ed.GetName())
			desired := targetStartMap[models.DocSymbolEnum+":"+full]
			if desired == 0 {
				desired = lm.StartLine
			}
			if desired > len(lines)+1 && lm.Leading != "" {
				commentLinesCount := len(strings.Split(lm.Leading, "\n"))
				padToLine(desired - commentLinesCount)
			} else if desired > len(lines)+1 {
				padToLine(desired)
			} else {
				appendLine("")
			}
			writeCommentLines(lm.Leading)
			startLn := appendLine(fmt.Sprintf("enum %s {", ed.GetName()))
			for vIdx, ev := range ed.Value {
				vLM := locMap[formatLocPath(appendLocPath(ePath, 2, int32(vIdx)))]
				writeCommentLines(vLM.Leading)
				line := fmt.Sprintf("  %s = %d;", ev.GetName(), ev.GetNumber())
				if ev.Options != nil && ev.Options.GetDeprecated() {
					line = fmt.Sprintf("  %s = %d [deprecated = true];", ev.GetName(), ev.GetNumber())
				}
				if vLM.Trailing != "" {
					line += " // " + strings.ReplaceAll(vLM.Trailing, "\n", " ")
				}
				appendLine(line)
			}
			endLn := appendLine("}")
			anchors = append(anchors, ProtoSymbolAnchorDTO{
				SymbolKind:     models.DocSymbolEnum,
				SymbolFullName: full,
				StartLine:      startLn,
				EndLine:        endLn,
			})
		}

		// 2. 顶层 Message
		for mIdx, md := range fdProto.MessageType {
			anchors = appendReconstructedMessage(&lines, appendLine, padToLine, writeCommentLines, locMap, targetStartMap, pkgName, "", md, []int32{4, int32(mIdx)}, anchors, 0)
		}

		// 3. Service 与 RPC Methods
		for sIdx, sd := range fdProto.Service {
			sPath := []int32{6, int32(sIdx)}
			sLM := locMap[formatLocPath(sPath)]
			svcFull := joinFullName(pkgName, sd.GetName())
			desired := targetStartMap[models.DocSymbolService+":"+svcFull]
			if desired == 0 {
				desired = sLM.StartLine
			}
			if desired > len(lines)+1 && sLM.Leading != "" {
				commentLinesCount := len(strings.Split(sLM.Leading, "\n"))
				padToLine(desired - commentLinesCount)
			} else if desired > len(lines)+1 {
				padToLine(desired)
			} else {
				appendLine("")
			}
			writeCommentLines(sLM.Leading)
			svcStart := appendLine(fmt.Sprintf("service %s {", sd.GetName()))
			for mIdx, md := range sd.Method {
				mLM := locMap[formatLocPath(appendLocPath(sPath, 2, int32(mIdx)))]
				mFull := svcFull + "." + md.GetName()
				mDesired := targetStartMap[models.DocSymbolMethod+":"+mFull]
				if mDesired == 0 {
					mDesired = mLM.StartLine
				}
				if mDesired > len(lines)+1 && mLM.Leading != "" {
					padToLine(mDesired - len(strings.Split(mLM.Leading, "\n")))
				} else if mDesired > len(lines)+1 {
					padToLine(mDesired)
				}
				for _, cl := range splitNonEmpty(mLM.Leading) {
					appendLine("  // " + cl)
				}
				inType := strings.TrimPrefix(md.GetInputType(), ".")
				outType := strings.TrimPrefix(md.GetOutputType(), ".")
				mStart := appendLine(fmt.Sprintf("  rpc %s(%s) returns (%s);", md.GetName(), inType, outType))
				anchors = append(anchors, ProtoSymbolAnchorDTO{
					SymbolKind:     models.DocSymbolMethod,
					SymbolFullName: mFull,
					StartLine:      mStart,
					EndLine:        mStart,
				})
			}
			svcEnd := appendLine("}")
			anchors = append(anchors, ProtoSymbolAnchorDTO{
				SymbolKind:     models.DocSymbolService,
				SymbolFullName: svcFull,
				StartLine:      svcStart,
				EndLine:        svcEnd,
			})
		}

		fileID := schema.ID*1000 + uint64(idx+1)
		out = append(out, ReconstructedProtoFile{
			FileID:        fileID,
			SchemaID:      schema.ID,
			SchemaName:    schema.Name,
			SchemaVersion: schema.Version,
			FilePath:      fdProto.GetName(),
			PackageName:   fdProto.GetPackage(),
			Content:       strings.Join(lines, "\n") + "\n",
			LineCount:     len(lines),
			HasSourceInfo: hasInfo,
			SourceOrigin:  "DESCRIPTOR_RECONSTRUCTED",
			SymbolAnchors: anchors,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].FilePath < out[j].FilePath
	})
	return out, nil
}

func appendReconstructedMessage(
	lines *[]string,
	appendLine func(string) int,
	padToLine func(int),
	writeCommentLines func(string),
	locMap map[string]locMeta,
	targetStartMap map[string]int,
	pkgName, parentFull string,
	md *descriptorpb.DescriptorProto,
	msgPath []int32,
	anchors []ProtoSymbolAnchorDTO,
	indentLevel int,
) []ProtoSymbolAnchorDTO {
	if md == nil || (md.Options != nil && md.Options.GetMapEntry()) {
		return anchors
	}
	indent := strings.Repeat("  ", indentLevel)
	prefix := pkgName
	if parentFull != "" {
		prefix = parentFull
	}
	full := joinFullName(prefix, md.GetName())
	lm := locMap[formatLocPath(msgPath)]

	if indentLevel == 0 {
		desired := targetStartMap[models.DocSymbolMessage+":"+full]
		if desired == 0 {
			desired = lm.StartLine
		}
		if desired > len(*lines)+1 && lm.Leading != "" {
			padToLine(desired - len(strings.Split(lm.Leading, "\n")))
		} else if desired > len(*lines)+1 {
			padToLine(desired)
		} else {
			appendLine("")
		}
	}

	for _, cl := range splitNonEmpty(lm.Leading) {
		appendLine(indent + "// " + cl)
	}
	msgStart := appendLine(fmt.Sprintf("%smessage %s {", indent, md.GetName()))

	for nIdx, nested := range md.NestedType {
		anchors = appendReconstructedMessage(
			lines, appendLine, padToLine, writeCommentLines, locMap, targetStartMap,
			pkgName, full, nested, appendLocPath(msgPath, 3, int32(nIdx)), anchors, indentLevel+1,
		)
	}

	for fIdx, f := range md.Field {
		fLM := locMap[formatLocPath(appendLocPath(msgPath, 2, int32(fIdx)))]
		for _, cl := range splitNonEmpty(fLM.Leading) {
			appendLine(indent + "  // " + cl)
		}
		labelPrefix := ""
		if f.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED {
			labelPrefix = "repeated "
		}
		fType := strings.TrimPrefix(strings.ToLower(strings.TrimPrefix(f.GetType().String(), "TYPE_")), ".")
		if f.GetTypeName() != "" {
			fType = strings.TrimPrefix(f.GetTypeName(), ".")
		}
		line := fmt.Sprintf("%s  %s%s %s = %d;", indent, labelPrefix, fType, f.GetName(), f.GetNumber())
		if fLM.Trailing != "" {
			line += " // " + strings.ReplaceAll(fLM.Trailing, "\n", " ")
		}
		appendLine(line)
	}

	msgEnd := appendLine(indent + "}")
	anchors = append(anchors, ProtoSymbolAnchorDTO{
		SymbolKind:     models.DocSymbolMessage,
		SymbolFullName: full,
		StartLine:      msgStart,
		EndLine:        msgEnd,
	})
	return anchors
}

func splitNonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
