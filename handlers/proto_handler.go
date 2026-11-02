package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	commonAuth "code-common/backend/auth"
	commonModels "code-common/backend/models"
	"code-traefik/internal/server/decoder"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var (
	GlobalDecoderRegistry *decoder.Registry
	decoderOnce           sync.Once
)

// GetDecoderRegistry 获取或按需懒加载全局 Protobuf 描述集编解码注册表
func GetDecoderRegistry() (*decoder.Registry, error) {
	if GlobalDecoderRegistry != nil {
		return GlobalDecoderRegistry, nil
	}
	var initErr error
	decoderOnce.Do(func() {
		reg, err := decoder.NewRegistry(models.DB)
		if err != nil {
			initErr = err
			return
		}
		GlobalDecoderRegistry = reg
	})
	if initErr != nil {
		return nil, initErr
	}
	if GlobalDecoderRegistry == nil {
		return decoder.NewRegistry(models.DB)
	}
	return GlobalDecoderRegistry, nil
}

// ResetGlobalDecoderRegistry 供测试重置全局编解码注册中心
func ResetGlobalDecoderRegistry(r *decoder.Registry) {
	GlobalDecoderRegistry = r
}

// ResolvedSchemaDependencyDTO 动态解析出的依赖状态契约
type ResolvedSchemaDependencyDTO struct {
	SchemaID      *uint64  `json:"schema_id,omitempty"`
	SchemaName    string   `json:"schema_name"`
	Version       *int     `json:"version,omitempty"`
	Category      string   `json:"category,omitempty"`
	MatchedProtos []string `json:"matched_protos"`
	Status        string   `json:"status"` // READY | MISSING
}

// ProtoSchemaLedgerItemDTO 描述集文件资产台账项 DTO
type ProtoSchemaLedgerItemDTO struct {
	ID                   uint64                        `json:"id"`
	Name                 string                        `json:"name"`
	Version              int                           `json:"version"`
	VersionTag           string                        `json:"version_tag"`
	Category             string                        `json:"category"`
	FileName             string                        `json:"file_name"`
	FileSHA256           string                        `json:"file_sha256"`
	FileSize             int                           `json:"file_size"`
	IsActive             bool                          `json:"is_active"`
	ProtoFileCount       int                           `json:"proto_file_count"`
	ServiceCount         int                           `json:"service_count"`
	OperationCount       int                           `json:"operation_count"`
	ProtoFiles           []string                      `json:"proto_files"`
	ExternalImports      []string                      `json:"external_imports"`
	Description          string                        `json:"description"`
	CreatedBy            uint                          `json:"created_by"`
	CreatedByName        string                        `json:"created_by_name,omitempty"`
	CreatedAt            time.Time                     `json:"created_at"`
	UpdatedAt            time.Time                     `json:"updated_at"`
	VersionCount         int                           `json:"version_count"`
	LatestVersion        int                           `json:"latest_version"`
	ActiveVersion        *int                          `json:"active_version,omitempty"`
	ClaimedServiceCount  int                           `json:"claimed_service_count"`
	ClaimedRatio         float64                       `json:"claimed_ratio"`
	ResolvedDependencies []ResolvedSchemaDependencyDTO `json:"resolved_dependencies"`
}

// ProtoPoolSummaryDTO 全局单一生效池统计摘要
type ProtoPoolSummaryDTO struct {
	ActiveSchemaCount    int     `json:"active_schema_count"`
	ActiveBaseLibCount   int     `json:"active_base_lib_count"`
	ActiveServiceCount   int     `json:"active_service_count"`
	ActiveOperationCount int     `json:"active_operation_count"`
	ClaimedServiceCount  int     `json:"claimed_service_count"`
	OrphanServiceCount   int     `json:"orphan_service_count"`
	ClaimedRatio         float64 `json:"claimed_ratio"`
	UnresolvedDepCount   int     `json:"unresolved_dep_count"`
}

// buildPoolSummary 计算全局生效池健康汇总
func buildPoolSummary(db *gorm.DB) ProtoPoolSummaryDTO {
	var summary ProtoPoolSummaryDTO

	var activeSchemas []models.ProtoSchema
	_ = db.Where("is_active = true").Find(&activeSchemas).Error
	summary.ActiveSchemaCount = len(activeSchemas)

	activeProtosSet := make(map[string]struct{})
	for _, s := range activeSchemas {
		if s.Category == models.ProtoCategoryBaseLib {
			summary.ActiveBaseLibCount++
		}
		var protos []string
		if len(s.ProtoFiles) > 0 {
			_ = json.Unmarshal(s.ProtoFiles, &protos)
			for _, p := range protos {
				activeProtosSet[p] = struct{}{}
			}
		}
	}

	// 检查未解析的外部依赖数
	unresolvedSet := make(map[string]struct{})
	for _, s := range activeSchemas {
		var exts []string
		if len(s.ExternalImports) > 0 {
			_ = json.Unmarshal(s.ExternalImports, &exts)
			for _, ext := range exts {
				if strings.HasPrefix(ext, "google/protobuf/") {
					continue
				}
				if _, ok := activeProtosSet[ext]; !ok {
					unresolvedSet[ext] = struct{}{}
				}
			}
		}
	}
	summary.UnresolvedDepCount = len(unresolvedSet)

	// 生效方法数与服务数
	var activeOps []models.ProtoOperation
	_ = db.Where("is_active = true").Find(&activeOps).Error
	summary.ActiveOperationCount = len(activeOps)

	svcMap := make(map[string]struct{})
	for _, op := range activeOps {
		svcMap[op.ServiceName] = struct{}{}
	}
	summary.ActiveServiceCount = len(svcMap)

	if len(svcMap) > 0 {
		activeSvcNames := make([]string, 0, len(svcMap))
		for name := range svcMap {
			activeSvcNames = append(activeSvcNames, name)
		}
		var claimedCount int64
		_ = db.Model(&models.ModuleService{}).
			Where("service_name IN ?", activeSvcNames).
			Distinct("service_name").
			Count(&claimedCount).Error

		summary.ClaimedServiceCount = int(claimedCount)
		summary.OrphanServiceCount = summary.ActiveServiceCount - summary.ClaimedServiceCount
		summary.ClaimedRatio = float64(summary.ClaimedServiceCount) / float64(summary.ActiveServiceCount)
	}

	return summary
}

// ListProtoSchemas GET /api/proto-schemas
func ListProtoSchemas(c *gin.Context) {
	page, pageSize := parsePagination(c)
	latestOnly := c.Query("latest_only") != "false"
	name := strings.TrimSpace(c.Query("name"))
	category := strings.TrimSpace(c.Query("category"))
	activeOnlyStr := c.Query("active_only")
	search := strings.TrimSpace(c.Query("search"))
	sortBy := strings.TrimSpace(c.Query("sort_by"))
	sortDir := strings.ToLower(strings.TrimSpace(c.Query("sort_dir")))

	if category != "" && category != models.ProtoCategoryBaseLib && category != models.ProtoCategoryBusiness && category != models.ProtoCategoryGateway {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 category 分类"})
		return
	}

	// 排序字段白名单校验防 SQL 注入
	sortByCol := "updated_at"
	switch sortBy {
	case "", "updated_at":
		sortByCol = "updated_at"
	case "created_at":
		sortByCol = "created_at"
	case "name":
		sortByCol = "name"
	case "file_size":
		sortByCol = "file_size"
	case "service_count":
		sortByCol = "service_count"
	case "operation_count":
		sortByCol = "operation_count"
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的排序字段 sort_by，仅支持 updated_at, created_at, name, file_size, service_count, operation_count"})
		return
	}

	if sortDir != "asc" && sortDir != "desc" {
		sortDir = "desc"
	}

	poolSummary := buildPoolSummary(models.DB)

	baseFilter := func(tx *gorm.DB) *gorm.DB {
		if name != "" {
			tx = tx.Where("name = ?", name)
		}
		if category != "" {
			tx = tx.Where("category = ?", category)
		}
		if activeOnlyStr == "true" {
			tx = tx.Where("is_active = true")
		}
		if search != "" {
			s := "%" + search + "%"
			tx = tx.Where("name ILIKE ? OR version_tag ILIKE ? OR file_name ILIKE ? OR proto_files::text ILIKE ?", s, s, s, s)
		}
		return tx
	}

	var total int64
	var schemas []models.ProtoSchema

	if latestOnly {
		// 按 name 聚合分组
		countQuery := baseFilter(models.DB.Model(&models.ProtoSchema{}))
		if err := countQuery.Select("COUNT(DISTINCT name)").Scan(&total).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "统计描述集台账失败"})
			return
		}

		if total == 0 {
			c.JSON(http.StatusOK, gin.H{
				"items":        []ProtoSchemaLedgerItemDTO{},
				"total":        0,
				"page":         page,
				"page_size":    pageSize,
				"pool_summary": poolSummary,
			})
			return
		}

		subQuery := baseFilter(models.DB.Model(&models.ProtoSchema{})).
			Select("DISTINCT ON (name) id, name, is_active, version, updated_at, created_at, file_size, service_count, operation_count").
			Order("name ASC, is_active DESC, version DESC")

		var chosenIDs []uint64
		wrapQuery := models.DB.Table("(?) AS chosen", subQuery).
			Order(fmt.Sprintf("%s %s, id DESC", sortByCol, sortDir)).
			Offset((page - 1) * pageSize).Limit(pageSize)

		if err := wrapQuery.Pluck("id", &chosenIDs).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查询描述集台账失败"})
			return
		}

		if len(chosenIDs) > 0 {
			var fetched []models.ProtoSchema
			_ = models.DB.Model(&models.ProtoSchema{}).Omit("descriptor_bytes").Where("id IN ?", chosenIDs).Find(&fetched).Error
			// 恢复 chosenIDs 排序
			fetchedMap := make(map[uint64]models.ProtoSchema, len(fetched))
			for _, item := range fetched {
				fetchedMap[item.ID] = item
			}
			for _, id := range chosenIDs {
				if item, ok := fetchedMap[id]; ok {
					schemas = append(schemas, item)
				}
			}
		}
	} else {
		// 平铺查询全量版本
		query := baseFilter(models.DB.Model(&models.ProtoSchema{})).Omit("descriptor_bytes")
		countQuery := query.Session(&gorm.Session{})
		if err := countQuery.Count(&total).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "统计描述集失败"})
			return
		}
		offset := (page - 1) * pageSize
		if err := query.Order(fmt.Sprintf("%s %s, id DESC", sortByCol, sortDir)).
			Offset(offset).Limit(pageSize).Find(&schemas).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查询描述集失败"})
			return
		}
	}

	// 零 N+1 批量富化台账项指标
	namesSet := make(map[string]struct{})
	userIDsSet := make(map[uint]struct{})
	schemaIDs := make([]uint64, 0, len(schemas))
	for _, s := range schemas {
		namesSet[s.Name] = struct{}{}
		if s.CreatedBy > 0 {
			userIDsSet[s.CreatedBy] = struct{}{}
		}
		schemaIDs = append(schemaIDs, s.ID)
	}

	names := make([]string, 0, len(namesSet))
	for n := range namesSet {
		names = append(names, n)
	}
	userIDs := make([]uint, 0, len(userIDsSet))
	for uid := range userIDsSet {
		userIDs = append(userIDs, uid)
	}

	// 1. 批量统计每个 name 的版本数量、最大版本与生效版本
	type nameStats struct {
		Name     string
		VerCount int
		MaxVer   int
		ActVer   *int
	}
	nameStatsMap := make(map[string]nameStats)
	if len(names) > 0 {
		var stats []nameStats
		_ = models.DB.Model(&models.ProtoSchema{}).
			Select("name, COUNT(*) AS ver_count, MAX(version) AS max_ver, MAX(CASE WHEN is_active THEN version ELSE NULL END) AS act_ver").
			Where("name IN ?", names).
			Group("name").Scan(&stats).Error
		for _, st := range stats {
			nameStatsMap[st.Name] = st
		}
	}

	// 2. 批量查找用户信息
	userMap := make(map[uint]string)
	if len(userIDs) > 0 {
		var users []models.User
		_ = models.DB.Where("id IN ?", userIDs).Find(&users).Error
		for _, u := range users {
			userMap[u.ID] = u.Name
			if userMap[u.ID] == "" {
				userMap[u.ID] = u.Username
			}
		}
	}

	// 3. 批量计算每个 Schema 的认领服务数
	claimedRatioMap := make(map[uint64]float64)
	claimedCountMap := make(map[uint64]int)
	if len(schemaIDs) > 0 {
		type opSvc struct {
			SchemaID    uint64
			ServiceName string
		}
		var opSvcs []opSvc
		_ = models.DB.Model(&models.ProtoOperation{}).
			Select("DISTINCT schema_id, service_name").
			Where("schema_id IN ?", schemaIDs).
			Scan(&opSvcs).Error

		schemaSvcNames := make(map[uint64][]string)
		allSvcNamesMap := make(map[string]struct{})
		for _, item := range opSvcs {
			schemaSvcNames[item.SchemaID] = append(schemaSvcNames[item.SchemaID], item.ServiceName)
			allSvcNamesMap[item.ServiceName] = struct{}{}
		}

		allSvcNames := make([]string, 0, len(allSvcNamesMap))
		for s := range allSvcNamesMap {
			allSvcNames = append(allSvcNames, s)
		}

		claimedSvcSet := make(map[string]struct{})
		if len(allSvcNames) > 0 {
			var claimedSvcs []string
			_ = models.DB.Model(&models.ModuleService{}).
				Where("service_name IN ?", allSvcNames).
				Pluck("service_name", &claimedSvcs).Error
			for _, cs := range claimedSvcs {
				claimedSvcSet[cs] = struct{}{}
			}
		}

		for sid, svcList := range schemaSvcNames {
			if len(svcList) == 0 {
				continue
			}
			claimed := 0
			for _, sn := range svcList {
				if _, ok := claimedSvcSet[sn]; ok {
					claimed++
				}
			}
			claimedCountMap[sid] = claimed
			claimedRatioMap[sid] = float64(claimed) / float64(len(svcList))
		}
	}

	// 4. 批量解析依赖状态 (resolved_dependencies)
	var allActiveSchemas []models.ProtoSchema
	_ = models.DB.Where("is_active = true").Find(&allActiveSchemas).Error

	type activeProtoInfo struct {
		schemaID   uint64
		schemaName string
		version    int
		category   string
	}
	activeProtosLookup := make(map[string]activeProtoInfo)
	for _, as := range allActiveSchemas {
		var apList []string
		if len(as.ProtoFiles) > 0 {
			_ = json.Unmarshal(as.ProtoFiles, &apList)
			for _, p := range apList {
				activeProtosLookup[p] = activeProtoInfo{
					schemaID:   as.ID,
					schemaName: as.Name,
					version:    as.Version,
					category:   as.Category,
				}
			}
		}
	}

	items := make([]ProtoSchemaLedgerItemDTO, 0, len(schemas))
	for _, s := range schemas {
		var protoFiles []string
		if len(s.ProtoFiles) > 0 {
			_ = json.Unmarshal(s.ProtoFiles, &protoFiles)
		}
		if protoFiles == nil {
			protoFiles = []string{}
		}

		var externalImports []string
		if len(s.ExternalImports) > 0 {
			_ = json.Unmarshal(s.ExternalImports, &externalImports)
		}
		if externalImports == nil {
			externalImports = []string{}
		}

		// 解析 resolved_dependencies
		resolvedDeps := make([]ResolvedSchemaDependencyDTO, 0)
		for _, imp := range externalImports {
			if strings.HasPrefix(imp, "google/protobuf/") {
				continue
			}
			if match, ok := activeProtosLookup[imp]; ok {
				sid := match.schemaID
				ver := match.version
				resolvedDeps = append(resolvedDeps, ResolvedSchemaDependencyDTO{
					SchemaID:      &sid,
					SchemaName:    match.schemaName,
					Version:       &ver,
					Category:      match.category,
					MatchedProtos: []string{imp},
					Status:        "READY",
				})
			} else {
				resolvedDeps = append(resolvedDeps, ResolvedSchemaDependencyDTO{
					SchemaName:    imp,
					MatchedProtos: []string{imp},
					Status:        "MISSING",
				})
			}
		}

		st := nameStatsMap[s.Name]
		claimedCount := claimedCountMap[s.ID]
		claimedRatio := claimedRatioMap[s.ID]

		items = append(items, ProtoSchemaLedgerItemDTO{
			ID:                   s.ID,
			Name:                 s.Name,
			Version:              s.Version,
			VersionTag:           s.VersionTag,
			Category:             s.Category,
			FileName:             s.FileName,
			FileSHA256:           s.FileSHA256,
			FileSize:             s.FileSize,
			IsActive:             s.IsActive,
			ProtoFileCount:       s.ProtoFileCount,
			ServiceCount:         s.ServiceCount,
			OperationCount:       s.OperationCount,
			ProtoFiles:           protoFiles,
			ExternalImports:      externalImports,
			Description:          s.Description,
			CreatedBy:            s.CreatedBy,
			CreatedByName:        userMap[s.CreatedBy],
			CreatedAt:            s.CreatedAt,
			UpdatedAt:            s.UpdatedAt,
			VersionCount:         st.VerCount,
			LatestVersion:        st.MaxVer,
			ActiveVersion:        st.ActVer,
			ClaimedServiceCount:  claimedCount,
			ClaimedRatio:         claimedRatio,
			ResolvedDependencies: resolvedDeps,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"items":        items,
		"total":        total,
		"page":         page,
		"page_size":    pageSize,
		"pool_summary": poolSummary,
	})
}

// UploadProtoSchema POST /api/proto-schemas (AdminOnly)
func UploadProtoSchema(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)

	// 安全限制：升级上限至 16MB + 64KB 头部开销
	// TODO(security): 接入企业制品防病毒扫描网关对上传描述集做沙箱扫描
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<20+65536)

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少或非法的 file 文件参数（须 <= 16MB）"})
		return
	}
	defer file.Close()

	// 校验扩展名白名单 (.pb, .desc, .protoset, .bin)
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".pb" && ext != ".desc" && ext != ".protoset" && ext != ".bin" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的描述集文件类型，仅支持 .pb, .desc, .protoset, .bin"})
		return
	}

	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "描述集标识 name 不能为空"})
		return
	}

	category := strings.TrimSpace(c.PostForm("category"))
	if category == "" {
		category = models.ProtoCategoryBusiness
	}
	if category != models.ProtoCategoryBaseLib && category != models.ProtoCategoryBusiness && category != models.ProtoCategoryGateway {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 category 分类，仅支持 BASE_LIB, BUSINESS, GATEWAY"})
		return
	}

	versionTag := strings.TrimSpace(c.PostForm("version_tag"))
	description := strings.TrimSpace(c.PostForm("description"))
	autoActivate := c.PostForm("auto_activate") == "true"

	rawBytes, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取上传文件内容失败"})
		return
	}

	reg, err := GetDecoderRegistry()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	schema, ops, inheritedCount, err := reg.UploadSchema(decoder.UploadSchemaInput{
		Name:         name,
		Category:     category,
		VersionTag:   versionTag,
		FileName:     header.Filename,
		RawBytes:     rawBytes,
		Description:  description,
		AutoActivate: autoActivate,
		OperatorID:   uc.UserID,
	})
	if err != nil {
		var conflictErr *decoder.SchemaConflictError
		if errors.As(err, &conflictErr) {
			c.JSON(http.StatusConflict, gin.H{
				"error":     conflictErr.Error(),
				"conflicts": conflictErr.Conflicts,
			})
			return
		}
		var brokenErr *decoder.SchemaBrokenDependentsError
		if errors.As(err, &brokenErr) {
			c.JSON(http.StatusConflict, gin.H{
				"error":             brokenErr.Error(),
				"broken_dependents": brokenErr.BrokenDependents,
			})
			return
		}
		if errors.Is(err, decoder.ErrDescriptorEmpty) || errors.Is(err, decoder.ErrDescriptorTooLarge) ||
			errors.Is(err, decoder.ErrInvalidDescriptorProto) || errors.Is(err, decoder.ErrInvalidSchemaName) ||
			strings.Contains(err.Error(), "缺失") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "upload", commonModels.AuditLevelP1, fmt.Sprintf("上传描述集 [%s] 版本 %d", schema.Name, schema.Version), "proto_schema", schema.ID, schema.Name, nil, schema)
	c.JSON(http.StatusCreated, gin.H{
		"schema":                    schema,
		"operations":                ops,
		"inherited_ignore_op_count": inheritedCount,
	})
}

// UpdateProtoSchema PATCH /api/proto-schemas/:id (AdminOnly)
func UpdateProtoSchema(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的描述集 ID"})
		return
	}

	var req struct {
		Category    *string `json:"category"`
		VersionTag  *string `json:"version_tag"`
		Description *string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 JSON 请求体"})
		return
	}

	if req.Category != nil {
		cat := strings.TrimSpace(*req.Category)
		if cat != models.ProtoCategoryBaseLib && cat != models.ProtoCategoryBusiness && cat != models.ProtoCategoryGateway {
			c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 category 分类，仅支持 BASE_LIB, BUSINESS, GATEWAY"})
			return
		}
	}

	var schema models.ProtoSchema
	if err := models.DB.First(&schema, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "描述集版本不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	before := schema
	updates := make(map[string]interface{})
	if req.Category != nil {
		updates["category"] = strings.TrimSpace(*req.Category)
		schema.Category = updates["category"].(string)
	}
	if req.VersionTag != nil {
		updates["version_tag"] = strings.TrimSpace(*req.VersionTag)
		schema.VersionTag = updates["version_tag"].(string)
	}
	if req.Description != nil {
		updates["description"] = strings.TrimSpace(*req.Description)
		schema.Description = updates["description"].(string)
	}

	if len(updates) > 0 {
		if err := models.DB.Model(&schema).Updates(updates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "更新描述集元数据失败"})
			return
		}
	}

	recordAudit(c, "update", commonModels.AuditLevelP2, fmt.Sprintf("编辑描述集 [%s] 版本 %d 元数据", schema.Name, schema.Version), "proto_schema", schema.ID, schema.Name, before, schema)
	c.JSON(http.StatusOK, schema)
}

// ActivateProtoSchema POST /api/proto-schemas/:id/activate (AdminOnly)
func ActivateProtoSchema(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的描述集 ID"})
		return
	}

	reg, err := GetDecoderRegistry()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	_, activatedSchema, count, err := reg.ActivateSchemaVersion(id)
	if err != nil {
		var conflictErr *decoder.SchemaConflictError
		if errors.As(err, &conflictErr) {
			c.JSON(http.StatusConflict, gin.H{
				"error":     conflictErr.Error(),
				"conflicts": conflictErr.Conflicts,
			})
			return
		}
		var brokenErr *decoder.SchemaBrokenDependentsError
		if errors.As(err, &brokenErr) {
			c.JSON(http.StatusConflict, gin.H{
				"error":             brokenErr.Error(),
				"broken_dependents": brokenErr.BrokenDependents,
			})
			return
		}
		if errors.Is(err, decoder.ErrSchemaNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "activate", commonModels.AuditLevelP1, fmt.Sprintf("激活描述集 [%s] 版本 %d", activatedSchema.Name, activatedSchema.Version), "proto_schema", activatedSchema.ID, activatedSchema.Name, nil, activatedSchema)
	c.JSON(http.StatusOK, gin.H{
		"schema":               activatedSchema,
		"activated_operations": count,
	})
}

// DeactivateProtoSchema POST /api/proto-schemas/:id/deactivate (AdminOnly)
func DeactivateProtoSchema(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的描述集 ID"})
		return
	}

	reg, err := GetDecoderRegistry()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	deactivated, count, err := reg.DeactivateSchemaVersion(id)
	if err != nil {
		var brokenErr *decoder.SchemaBrokenDependentsError
		if errors.As(err, &brokenErr) {
			c.JSON(http.StatusConflict, gin.H{
				"error":             brokenErr.Error(),
				"broken_dependents": brokenErr.BrokenDependents,
			})
			return
		}
		if errors.Is(err, decoder.ErrSchemaNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if strings.Contains(err.Error(), "未生效") {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "deactivate", commonModels.AuditLevelP1, fmt.Sprintf("停用描述集 [%s] 版本 %d", deactivated.Name, deactivated.Version), "proto_schema", deactivated.ID, deactivated.Name, deactivated, nil)
	c.JSON(http.StatusOK, gin.H{
		"schema":                 deactivated,
		"deactivated_operations": count,
	})
}

// BatchActivateProtoSchemas POST /api/proto-schemas/batch-activate (AdminOnly)
func BatchActivateProtoSchemas(c *gin.Context) {
	var req struct {
		SchemaIDs []uint64 `json:"schema_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的请求参数，期望 schema_ids 数组"})
		return
	}
	if len(req.SchemaIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "待激活描述集列表不能为空"})
		return
	}
	if len(req.SchemaIDs) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "单次批量激活不能超过 100 个描述集"})
		return
	}

	reg, err := GetDecoderRegistry()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	schemas, totalOps, err := reg.BatchActivate(req.SchemaIDs)
	if err != nil {
		var conflictErr *decoder.SchemaConflictError
		if errors.As(err, &conflictErr) {
			c.JSON(http.StatusConflict, gin.H{
				"error":     conflictErr.Error(),
				"conflicts": conflictErr.Conflicts,
			})
			return
		}
		var brokenErr *decoder.SchemaBrokenDependentsError
		if errors.As(err, &brokenErr) {
			c.JSON(http.StatusConflict, gin.H{
				"error":             brokenErr.Error(),
				"broken_dependents": brokenErr.BrokenDependents,
			})
			return
		}
		if errors.Is(err, decoder.ErrSchemaNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if strings.Contains(err.Error(), "同名") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "batch_activate", commonModels.AuditLevelP1, fmt.Sprintf("批量激活 %d 个描述集", len(schemas)), "proto_schema", 0, "batch_activate", nil, schemas)
	c.JSON(http.StatusOK, gin.H{
		"activated_schemas":       schemas,
		"total_active_operations": totalOps,
	})
}

// GetProtoDependenciesGraph GET /api/proto-schemas/dependencies-graph
func GetProtoDependenciesGraph(c *gin.Context) {
	activeOnly := c.Query("active_only") != "false"

	var schemas []models.ProtoSchema
	q := models.DB.Model(&models.ProtoSchema{}).Omit("descriptor_bytes")
	if activeOnly {
		q = q.Where("is_active = true")
	}
	_ = q.Order("id ASC").Find(&schemas).Error

	type NodeDTO struct {
		ID             uint64 `json:"id"`
		Name           string `json:"name"`
		Version        int    `json:"version"`
		Category       string `json:"category"`
		ProtoFileCount int    `json:"proto_file_count"`
		ServiceCount   int    `json:"service_count"`
		OperationCount int    `json:"operation_count"`
		IsActive       bool   `json:"is_active"`
	}

	type EdgeDTO struct {
		FromSchemaID  uint64   `json:"from_schema_id"`
		ToSchemaID    uint64   `json:"to_schema_id"`
		MatchedProtos []string `json:"matched_protos"`
	}

	nodes := make([]NodeDTO, 0, len(schemas))
	protoToSchemaID := make(map[string]uint64)
	for _, s := range schemas {
		nodes = append(nodes, NodeDTO{
			ID:             s.ID,
			Name:           s.Name,
			Version:        s.Version,
			Category:       s.Category,
			ProtoFileCount: s.ProtoFileCount,
			ServiceCount:   s.ServiceCount,
			OperationCount: s.OperationCount,
			IsActive:       s.IsActive,
		})

		var protos []string
		if len(s.ProtoFiles) > 0 {
			_ = json.Unmarshal(s.ProtoFiles, &protos)
			for _, p := range protos {
				protoToSchemaID[p] = s.ID
			}
		}
	}

	edgesMap := make(map[string]*EdgeDTO)
	for _, s := range schemas {
		var exts []string
		if len(s.ExternalImports) > 0 {
			_ = json.Unmarshal(s.ExternalImports, &exts)
			for _, ext := range exts {
				if targetID, ok := protoToSchemaID[ext]; ok && targetID != s.ID {
					key := fmt.Sprintf("%d->%d", s.ID, targetID)
					if edge, exists := edgesMap[key]; exists {
						edge.MatchedProtos = append(edge.MatchedProtos, ext)
					} else {
						edgesMap[key] = &EdgeDTO{
							FromSchemaID:  s.ID,
							ToSchemaID:    targetID,
							MatchedProtos: []string{ext},
						}
					}
				}
			}
		}
	}

	edges := make([]EdgeDTO, 0, len(edgesMap))
	for _, e := range edgesMap {
		edges = append(edges, *e)
	}

	poolSummary := buildPoolSummary(models.DB)

	c.JSON(http.StatusOK, gin.H{
		"pool_summary": poolSummary,
		"nodes":        nodes,
		"edges":        edges,
	})
}

// DeleteProtoSchema DELETE /api/proto-schemas/:id (AdminOnly)
func DeleteProtoSchema(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的描述集 ID"})
		return
	}

	reg, err := GetDecoderRegistry()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	deleted, err := reg.DeleteSchemaVersion(id)
	if err != nil {
		if errors.Is(err, decoder.ErrSchemaNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, decoder.ErrDeleteActiveSchema) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "delete", commonModels.AuditLevelP1, fmt.Sprintf("删除描述集 [%s] 版本 %d", deleted.Name, deleted.Version), "proto_schema", deleted.ID, deleted.Name, deleted, nil)
	c.JSON(http.StatusOK, gin.H{"message": "描述集版本已删除"})
}

// DownloadProtoSchema GET /api/proto-schemas/:id/download (带安全强化响应头与文件名净化)
func DownloadProtoSchema(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的描述集 ID"})
		return
	}

	var schema models.ProtoSchema
	if err := models.DB.First(&schema, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "描述集不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询描述集失败"})
		return
	}

	fileName := schema.FileName
	if fileName == "" {
		fileName = fmt.Sprintf("%s_v%d.pb", schema.Name, schema.Version)
	}
	cleanFileName := filepath.Base(fileName)
	cleanFileName = strings.ReplaceAll(cleanFileName, "\"", "")
	cleanFileName = strings.ReplaceAll(cleanFileName, "\\", "")
	cleanFileName = strings.ReplaceAll(cleanFileName, "\r", "")
	cleanFileName = strings.ReplaceAll(cleanFileName, "\n", "")

	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", cleanFileName))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Length", strconv.Itoa(len(schema.DescriptorBytes)))
	c.Data(http.StatusOK, "application/octet-stream", schema.DescriptorBytes)
}

// ServiceContractGroupDTO 服务级聚合契约目录项
type ServiceContractGroupDTO struct {
	ServiceName     string                  `json:"service_name"`
	ProtoFile       string                  `json:"proto_file"`
	ProtoPackage    string                  `json:"proto_package"`
	ProtoService    string                  `json:"proto_service"`
	SourceSchema    map[string]interface{}  `json:"source_schema"`
	ClaimedByModule *ClaimedModuleDTO       `json:"claimed_by_module,omitempty"`
	IsClaimed       bool                    `json:"is_claimed"`
	OperationCount  int                     `json:"operation_count"`
	UnaryCount      int                     `json:"unary_count"`
	StreamCount     int                     `json:"stream_count"`
	Operations      []models.ProtoOperation `json:"operations"`
}

// ClaimedModuleDTO 服务认领的架构模块详情
type ClaimedModuleDTO struct {
	ModuleID      uint64 `json:"module_id"`
	ModuleName    string `json:"module_name"`
	SubsystemID   uint64 `json:"subsystem_id,omitempty"`
	SubsystemName string `json:"subsystem_name,omitempty"`
}

// ListProtoServices GET /api/proto-services (按 Service 组级稳定分页契约目录树)
func ListProtoServices(c *gin.Context) {
	page, pageSize := parsePagination(c)
	schemaIDStr := c.Query("schema_id")
	activeOnlyStr := c.Query("active_only")
	category := strings.TrimSpace(c.Query("category"))
	claimedStatus := strings.ToUpper(strings.TrimSpace(c.Query("claimed_status")))
	serviceName := strings.TrimSpace(c.Query("service_name"))
	search := strings.TrimSpace(c.Query("search"))

	// 1. 构建针对 service_name 聚合的查询
	baseQuery := models.DB.Table("traefik_proto_operations AS po").
		Joins("JOIN traefik_proto_schemas AS ps ON ps.id = po.schema_id")

	if schemaIDStr != "" {
		if sid, err := strconv.ParseUint(schemaIDStr, 10, 64); err == nil {
			baseQuery = baseQuery.Where("po.schema_id = ?", sid)
		}
	}
	if activeOnlyStr != "false" {
		baseQuery = baseQuery.Where("ps.is_active = true AND po.is_active = true")
	}
	if category != "" {
		baseQuery = baseQuery.Where("ps.category = ?", category)
	}
	if serviceName != "" {
		baseQuery = baseQuery.Where("po.service_name = ?", serviceName)
	}
	if search != "" {
		s := "%" + search + "%"
		baseQuery = baseQuery.Where("po.service_name ILIKE ? OR po.operation ILIKE ? OR po.req_message_full_name ILIKE ? OR po.rsp_message_full_name ILIKE ? OR po.proto_file ILIKE ?", s, s, s, s, s)
	}

	if claimedStatus == "CLAIMED" {
		baseQuery = baseQuery.Where("po.service_name IN (SELECT service_name FROM traefik_module_services)")
	} else if claimedStatus == "UNCLAIMED" {
		baseQuery = baseQuery.Where("po.service_name NOT IN (SELECT service_name FROM traefik_module_services)")
	}

	// 统计不同 service_name 的总数
	var total int64
	countQuery := baseQuery.Session(&gorm.Session{})
	if err := countQuery.Select("COUNT(DISTINCT po.service_name)").Scan(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "统计服务目录失败"})
		return
	}

	if total == 0 {
		c.JSON(http.StatusOK, gin.H{
			"items":     []ServiceContractGroupDTO{},
			"total":     0,
			"page":      page,
			"page_size": pageSize,
		})
		return
	}

	// 分页查询当前页的 service_name 列表
	offset := (page - 1) * pageSize
	var pageServices []string
	pageQuery := baseQuery.Session(&gorm.Session{})
	if err := pageQuery.Select("po.service_name").
		Group("po.service_name").
		Order("po.service_name ASC").
		Offset(offset).Limit(pageSize).
		Pluck("po.service_name", &pageServices).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "分页查询服务目录失败"})
		return
	}

	if len(pageServices) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"items":     []ServiceContractGroupDTO{},
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		})
		return
	}

	// 批量拉取当前页所有方法映射
	opsQuery := models.DB.Model(&models.ProtoOperation{}).
		Preload("Schema").
		Where("service_name IN ?", pageServices)

	if schemaIDStr != "" {
		if sid, err := strconv.ParseUint(schemaIDStr, 10, 64); err == nil {
			opsQuery = opsQuery.Where("schema_id = ?", sid)
		}
	}
	if activeOnlyStr != "false" {
		opsQuery = opsQuery.Where("is_active = true")
	}

	var allOps []models.ProtoOperation
	if err := opsQuery.Order("service_name ASC, operation ASC").Find(&allOps).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取服务方法明细失败"})
		return
	}

	// 批量查询架构认领情况
	var moduleSvcs []models.ModuleService
	_ = models.DB.Preload("Module.Subsystem").Where("service_name IN ?", pageServices).Find(&moduleSvcs).Error
	claimedMap := make(map[string]*ClaimedModuleDTO)
	for _, ms := range moduleSvcs {
		if ms.Module != nil {
			dto := &ClaimedModuleDTO{
				ModuleID:   ms.ModuleID,
				ModuleName: ms.Module.Name,
			}
			if ms.Module.Subsystem != nil {
				dto.SubsystemID = ms.Module.Subsystem.ID
				dto.SubsystemName = ms.Module.Subsystem.Name
			}
			claimedMap[ms.ServiceName] = dto
		}
	}

	// 按 service_name 聚合方法
	svcOpsMap := make(map[string][]models.ProtoOperation)
	for _, op := range allOps {
		svcOpsMap[op.ServiceName] = append(svcOpsMap[op.ServiceName], op)
	}

	items := make([]ServiceContractGroupDTO, 0, len(pageServices))
	for _, sName := range pageServices {
		opsList := svcOpsMap[sName]
		if len(opsList) == 0 {
			continue
		}

		first := opsList[0]
		var sourceSchema map[string]interface{}
		if first.Schema != nil {
			sourceSchema = map[string]interface{}{
				"id":        first.Schema.ID,
				"name":      first.Schema.Name,
				"version":   first.Schema.Version,
				"category":  first.Schema.Category,
				"is_active": first.Schema.IsActive,
			}
		} else {
			sourceSchema = map[string]interface{}{
				"id": first.SchemaID,
			}
		}

		unaryCount := 0
		streamCount := 0
		for _, op := range opsList {
			if op.ClientStreaming || op.ServerStreaming {
				streamCount++
			} else {
				unaryCount++
			}
		}

		claimedInfo := claimedMap[sName]
		items = append(items, ServiceContractGroupDTO{
			ServiceName:     sName,
			ProtoFile:       first.ProtoFile,
			ProtoPackage:    first.ProtoPackage,
			ProtoService:    first.ProtoService,
			SourceSchema:    sourceSchema,
			ClaimedByModule: claimedInfo,
			IsClaimed:       claimedInfo != nil,
			OperationCount:  len(opsList),
			UnaryCount:      unaryCount,
			StreamCount:     streamCount,
			Operations:      opsList,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// ListProtoOperations GET /api/proto-operations (保留兼容)
func ListProtoOperations(c *gin.Context) {
	page, pageSize := parsePagination(c)
	schemaIDStr := c.Query("schema_id")
	activeOnlyStr := c.Query("active_only")
	serviceName := strings.TrimSpace(c.Query("service_name"))
	operation := strings.TrimSpace(c.Query("operation"))
	search := strings.TrimSpace(c.Query("search"))

	query := models.DB.Model(&models.ProtoOperation{})
	if schemaIDStr != "" {
		if sid, err := strconv.ParseUint(schemaIDStr, 10, 64); err == nil {
			query = query.Where("schema_id = ?", sid)
		}
	}
	if activeOnlyStr == "true" {
		query = query.Where("is_active = true")
	}
	if serviceName != "" {
		query = query.Where("service_name = ?", serviceName)
	}
	if operation != "" {
		query = query.Where("operation = ?", operation)
	}
	if search != "" {
		s := "%" + search + "%"
		query = query.Where("service_name ILIKE ? OR operation ILIKE ?", s, s)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "统计方法映射失败"})
		return
	}

	items := make([]models.ProtoOperation, 0)
	offset := (page - 1) * pageSize
	if err := query.Order("service_name ASC, operation ASC").Offset(offset).Limit(pageSize).Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询方法映射失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetOperationSchemaTree GET /api/proto-operations/:id/schema-tree
func GetOperationSchemaTree(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的方法 ID"})
		return
	}

	reg, err := GetDecoderRegistry()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	tree, err := reg.GetOperationSchemaTree(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, tree)
}

// UpdateOperationIgnorePaths PUT /api/proto-operations/:id/ignore-paths (AdminOnly)
func UpdateOperationIgnorePaths(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的方法 ID"})
		return
	}

	var req struct {
		DiffIgnorePaths []string `json:"diff_ignore_paths"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的请求体参数，期望 diff_ignore_paths 字符串数组"})
		return
	}

	reg, err := GetDecoderRegistry()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	beforeOp, afterOp, err := reg.UpdateOperationIgnorePaths(id, req.DiffIgnorePaths)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || strings.Contains(err.Error(), "不存在") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "update_op_ignores", commonModels.AuditLevelP2,
		fmt.Sprintf("更新方法 [%s:%s] Diff 忽略路径", afterOp.ServiceName, afterOp.Operation),
		"proto_operation", afterOp.ID, afterOp.Operation, beforeOp, afterOp)

	c.JSON(http.StatusOK, afterOp)
}

// ServiceSummaryItem 描述集内单服务聚合与认领摘要
type ServiceSummaryItem struct {
	ServiceName          string  `json:"service_name"`
	ProtoFile            string  `json:"proto_file"`
	ProtoPackage         string  `json:"proto_package"`
	ProtoService         string  `json:"proto_service"`
	ClaimedModuleID      *uint64 `json:"claimed_module_id,omitempty"`
	ClaimedModuleName    string  `json:"claimed_module_name,omitempty"`
	ClaimedSubsystemName string  `json:"claimed_subsystem_name,omitempty"`
	IsClaimed            bool    `json:"is_claimed"`
	OperationCount       int     `json:"operation_count"`
	UnaryCount           int     `json:"unary_count"`
	StreamCount          int     `json:"stream_count"`
}

// GetProtoSchemaSummary GET /api/proto-schemas/:id/summary
func GetProtoSchemaSummary(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的描述集 ID"})
		return
	}

	var schema models.ProtoSchema
	if err := models.DB.Omit("descriptor_bytes").First(&schema, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "描述集不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询描述集失败"})
		return
	}

	var ops []models.ProtoOperation
	if err := models.DB.Where("schema_id = ?", id).Order("service_name ASC, operation ASC").Find(&ops).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询方法列表失败"})
		return
	}

	svcMap := make(map[string]*ServiceSummaryItem)
	var orderedSvcNames []string

	for _, op := range ops {
		item, exists := svcMap[op.ServiceName]
		if !exists {
			item = &ServiceSummaryItem{
				ServiceName:  op.ServiceName,
				ProtoFile:    op.ProtoFile,
				ProtoPackage: op.ProtoPackage,
				ProtoService: op.ProtoService,
			}
			svcMap[op.ServiceName] = item
			orderedSvcNames = append(orderedSvcNames, op.ServiceName)
		}
		item.OperationCount++
		if op.ClientStreaming || op.ServerStreaming {
			item.StreamCount++
		} else {
			item.UnaryCount++
		}
	}

	var moduleSvcs []models.ModuleService
	if len(orderedSvcNames) > 0 {
		_ = models.DB.Preload("Module.Subsystem").Where("service_name IN ?", orderedSvcNames).Find(&moduleSvcs).Error
	}

	claimedCount := 0
	claimedModMap := make(map[string]*models.ModuleService)
	for i := range moduleSvcs {
		claimedModMap[moduleSvcs[i].ServiceName] = &moduleSvcs[i]
	}

	servicesSummary := make([]ServiceSummaryItem, 0, len(orderedSvcNames))
	for _, name := range orderedSvcNames {
		item := svcMap[name]
		if ms, ok := claimedModMap[name]; ok {
			item.IsClaimed = true
			item.ClaimedModuleID = &ms.ModuleID
			if ms.Module != nil {
				item.ClaimedModuleName = ms.Module.Name
				if ms.Module.Subsystem != nil {
					item.ClaimedSubsystemName = ms.Module.Subsystem.Name
				}
			}
			claimedCount++
		}
		servicesSummary = append(servicesSummary, *item)
	}

	var protoFiles []string
	if len(schema.ProtoFiles) > 0 {
		_ = json.Unmarshal(schema.ProtoFiles, &protoFiles)
	}
	if protoFiles == nil {
		protoFiles = []string{}
	}

	claimedRatio := 0.0
	if len(orderedSvcNames) > 0 {
		claimedRatio = float64(claimedCount) / float64(len(orderedSvcNames))
	}

	c.JSON(http.StatusOK, gin.H{
		"schema":           schema,
		"proto_files":      protoFiles,
		"services_summary": servicesSummary,
		"claimed_ratio":    claimedRatio,
	})
}
