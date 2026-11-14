package protodoc

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"code-traefik/models"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"gorm.io/datatypes"
)

const (
	// MaxDocFieldDepth 嵌套字段/AST 样例生成的最大递归深度（Strict <= 12）
	MaxDocFieldDepth = 12
)

var (
	// ErrServiceDocNotFound 目标服务文档符号不存在
	ErrServiceDocNotFound = errors.New("未找到匹配的 Protobuf 服务参考文档")
	// ErrInvalidExportFormat 非法的文档导出格式
	ErrInvalidExportFormat = errors.New("仅支持 markdown 格式导出")
	// ErrMissingExportScope 导出范围参数缺失
	ErrMissingExportScope = errors.New("请至少指定 service_name、schema_id 或 subsystem_id 之一")
)

// ErrorCodeDoc @error_code 结构化错误码声明
type ErrorCodeDoc struct {
	Code           string `json:"code"`
	HttpOrCrosCode string `json:"http_or_cros_code"`
	Description    string `json:"description"`
}

// EnumValueDoc 枚举值文档节点
type EnumValueDoc struct {
	Name              string `json:"name"`
	Number            int32  `json:"number"`
	Comment           string `json:"comment"`
	IsDeprecated      bool   `json:"is_deprecated"`
	DeprecationReason string `json:"deprecation_reason,omitempty"`
	StartLine         int    `json:"start_line"`
}

// EnumDocDTO 枚举字典完整 DTO
type EnumDocDTO struct {
	FullName          string         `json:"full_name"`
	ShortName         string         `json:"short_name"`
	ProtoFile         string         `json:"proto_file"`
	SourceFileID      *uint64        `json:"source_file_id,omitempty"`
	StartLine         int            `json:"start_line"`
	EndLine           int            `json:"end_line"`
	Comment           string         `json:"comment"`
	IsDeprecated      bool           `json:"is_deprecated"`
	DeprecationReason string         `json:"deprecation_reason,omitempty"`
	Values            []EnumValueDoc `json:"values"`
}

// FieldDocNode 递归字段树节点（含注释、标签、Oneof、Map 与防环截断状态）
type FieldDocNode struct {
	Name              string         `json:"name"`
	JSONName          string         `json:"json_name,omitempty"`
	Number            int32          `json:"number"`
	Type              string         `json:"type"`
	TypeFullName      string         `json:"type_full_name,omitempty"`
	ProtoFile         string         `json:"proto_file,omitempty"`
	StartLine         int            `json:"start_line,omitempty"`
	Label             string         `json:"label"`
	IsMap             bool           `json:"is_map,omitempty"`
	MapKeyType        string         `json:"map_key_type,omitempty"`
	MapValueType      string         `json:"map_value_type,omitempty"`
	OneofGroup        string         `json:"oneof_group,omitempty"`
	OneofComment      string         `json:"oneof_comment,omitempty"`
	Comment           string         `json:"comment"`
	IsDeprecated      bool           `json:"is_deprecated,omitempty"`
	DeprecationReason string         `json:"deprecation_reason,omitempty"`
	EnumValues        []EnumValueDoc `json:"enum_values,omitempty"`
	Truncated         bool           `json:"truncated,omitempty"`
	TruncatedReason   string         `json:"truncated_reason,omitempty"`
	Children          []FieldDocNode `json:"children,omitempty"`
}

// MessageDocMeta 持久化在 MESSAGE 符号 fields_meta_json 中的结构元数据
type MessageDocMeta struct {
	FullName     string         `json:"full_name"`
	ProtoFile    string         `json:"proto_file"`
	SourceFileID *uint64        `json:"source_file_id,omitempty"`
	StartLine    int            `json:"start_line"`
	EndLine      int            `json:"end_line"`
	Comment      string         `json:"comment"`
	Fields       []FieldDocNode `json:"fields"`
	Truncated    bool           `json:"truncated,omitempty"`
}

// methodMetaStored 持久化在 METHOD 符号 fields_meta_json 中的签名摘要
type methodMetaStored struct {
	FileComments       string `json:"file_comments,omitempty"`
	ReqMessageFullName string `json:"req_message_full_name"`
	RspMessageFullName string `json:"rsp_message_full_name"`
	ClientStreaming    bool   `json:"client_streaming"`
	ServerStreaming    bool   `json:"server_streaming"`
}

// serviceMetaStored 持久化在 SERVICE 符号 fields_meta_json 中的文件摘要
type serviceMetaStored struct {
	FileComments string `json:"file_comments,omitempty"`
}

// GoldenSampleDTO 关联 ACTIVE 黄金测试集的真实解码 JSON 样本
type GoldenSampleDTO struct {
	SuiteID      uint64          `json:"suite_id"`
	SuiteCode    string          `json:"suite_code"`
	SuiteName    string          `json:"suite_name"`
	ItemID       uint64          `json:"item_id"`
	OrderNo      int             `json:"order_no"`
	DecodeStatus string          `json:"decode_status"`
	ReqJSON      json.RawMessage `json:"req_json,omitempty"`
	RspJSON      json.RawMessage `json:"rsp_json,omitempty"`
	Remark       string          `json:"remark,omitempty"`
}

// ContractDefinerDTO ADR-16 公共契约库定义方信息
type ContractDefinerDTO struct {
	ContractID      uint64 `json:"contract_id"`
	ServiceName     string `json:"service_name"`
	DefinerModuleID uint64 `json:"definer_module_id"`
	ModuleCode      string `json:"module_code"`
	ModuleName      string `json:"module_name"`
	SubsystemID     uint64 `json:"subsystem_id"`
	SubsystemName   string `json:"subsystem_name"`
	ProvideMode     string `json:"provide_mode"`
	ImplKeyLabel    string `json:"impl_key_label"`
}

// ServiceImplementationDTO 服务实现模块 DTO
type ServiceImplementationDTO struct {
	ServiceID           uint64 `json:"service_id"`
	ModuleID            uint64 `json:"module_id"`
	ModuleCode          string `json:"module_code"`
	ModuleName          string `json:"module_name"`
	SubsystemID         uint64 `json:"subsystem_id"`
	SubsystemName       string `json:"subsystem_name"`
	ArchProcessCode     string `json:"arch_process_code"`
	OwnerName           string `json:"owner_name"`
	ImplKey             string `json:"impl_key"`
	IsCustomServiceName bool   `json:"is_custom_service_name"`
	ServiceName         string `json:"service_name"`
}

// ServiceConsumerDTO 服务下游调用消费方 DTO
type ServiceConsumerDTO struct {
	ModuleID        uint64 `json:"module_id"`
	ModuleCode      string `json:"module_code"`
	ModuleName      string `json:"module_name"`
	SubsystemID     uint64 `json:"subsystem_id"`
	SubsystemName   string `json:"subsystem_name"`
	ArchProcessCode string `json:"arch_process_code"`
	DepType         string `json:"dep_type"`
}

// ServiceContractLineageDTO 服务架构认领与多实现血缘矩阵
type ServiceContractLineageDTO struct {
	ClaimStatus     string                     `json:"claim_status"`
	Definer         *ContractDefinerDTO        `json:"definer,omitempty"`
	Implementations []ServiceImplementationDTO `json:"implementations"`
	Consumers       []ServiceConsumerDTO       `json:"consumers"`
}

// MethodDocDTO RPC 方法完整文档 DTO
type MethodDocDTO struct {
	Operation          string            `json:"operation"`
	FullName           string            `json:"full_name"`
	ProtoFile          string            `json:"proto_file"`
	SourceFileID       *uint64           `json:"source_file_id,omitempty"`
	StartLine          int               `json:"start_line"`
	EndLine            int               `json:"end_line"`
	Summary            string            `json:"summary"`
	LeadingComments    string            `json:"leading_comments"`
	TrailingComments   string            `json:"trailing_comments"`
	IsDeprecated       bool              `json:"is_deprecated"`
	DeprecationReason  string            `json:"deprecation_reason"`
	OwnerTag           string            `json:"owner_tag"`
	ErrorCodes         []ErrorCodeDoc    `json:"error_codes"`
	ReqMessageFullName string            `json:"req_message_full_name"`
	RspMessageFullName string            `json:"rsp_message_full_name"`
	ClientStreaming    bool              `json:"client_streaming"`
	ServerStreaming    bool              `json:"server_streaming"`
	ReqFields          []FieldDocNode    `json:"req_fields"`
	RspFields          []FieldDocNode    `json:"rsp_fields"`
	DefaultReqJSON     json.RawMessage   `json:"default_req_json"`
	DefaultRspJSON     json.RawMessage   `json:"default_rsp_json"`
	GoldenSamples      []GoldenSampleDTO `json:"golden_samples"`
}

// ProtoDocTreeNodeDTO 活体文档导航树节点
type ProtoDocTreeNodeDTO struct {
	NodeKey         string                `json:"node_key"`
	NodeType        string                `json:"node_type"`
	Label           string                `json:"label"`
	SubsystemID     *uint64               `json:"subsystem_id,omitempty"`
	SchemaID        *uint64               `json:"schema_id,omitempty"`
	SchemaName      string                `json:"schema_name,omitempty"`
	Category        string                `json:"category,omitempty"`
	ServiceName     string                `json:"service_name,omitempty"`
	Operation       string                `json:"operation,omitempty"`
	Summary         string                `json:"summary,omitempty"`
	IsDeprecated    bool                  `json:"is_deprecated,omitempty"`
	MethodCount     int                   `json:"method_count,omitempty"`
	DeprecatedCount int                   `json:"deprecated_count,omitempty"`
	HasSourceInfo   bool                  `json:"has_source_info"`
	Children        []ProtoDocTreeNodeDTO `json:"children,omitempty"`
}

// ProtoDocTreeResponse 文档导航树响应结构
type ProtoDocTreeResponse struct {
	Items           []ProtoDocTreeNodeDTO `json:"items"`
	TotalServices   int                   `json:"total_services"`
	TotalMethods    int                   `json:"total_methods"`
	DeprecatedCount int                   `json:"deprecated_count"`
}

// ProtoServiceDocDetailDTO 服务级活体接口参考文档详情 DTO
type ProtoServiceDocDetailDTO struct {
	SchemaID          uint64                    `json:"schema_id"`
	SchemaName        string                    `json:"schema_name"`
	SchemaVersion     int                       `json:"schema_version"`
	SchemaCategory    string                    `json:"schema_category"`
	SourceType        string                    `json:"source_type"`
	HasSourceInfo     bool                      `json:"has_source_info"`
	PackageName       string                    `json:"package_name"`
	ServiceName       string                    `json:"service_name"`
	ShortName         string                    `json:"short_name"`
	ProtoFile         string                    `json:"proto_file"`
	SourceFileID      *uint64                   `json:"source_file_id,omitempty"`
	StartLine         int                       `json:"start_line"`
	EndLine           int                       `json:"end_line"`
	FileComments      string                    `json:"file_comments"`
	Summary           string                    `json:"summary"`
	LeadingComments   string                    `json:"leading_comments"`
	TrailingComments  string                    `json:"trailing_comments"`
	IsDeprecated      bool                      `json:"is_deprecated"`
	DeprecationReason string                    `json:"deprecation_reason"`
	OwnerTag          string                    `json:"owner_tag"`
	ErrorCodes        []ErrorCodeDoc            `json:"error_codes"`
	Methods           []MethodDocDTO            `json:"methods"`
	Enums             []EnumDocDTO              `json:"enums"`
	ReferencedTypes   []MessageDocMeta          `json:"referenced_types"`
	Lineage           ServiceContractLineageDTO `json:"lineage"`
}

// DocTreeQuery 导航树查询参数
type DocTreeQuery struct {
	SubsystemID       *uint64
	Category          string
	SchemaID          *uint64
	IncludeDeprecated bool
	Search            string
}

// ExportMarkdownRequest Markdown 离线手册导出请求参数
type ExportMarkdownRequest struct {
	ServiceName string
	SchemaID    *uint64
	SubsystemID *uint64
	Format      string
}

// ProtoSymbolAnchorDTO 只读源码行号锚点 DTO
type ProtoSymbolAnchorDTO struct {
	SymbolKind     string `json:"symbol_kind"`
	SymbolFullName string `json:"symbol_full_name"`
	StartLine      int    `json:"start_line"`
	EndLine        int    `json:"end_line"`
}

// ReconstructedProtoFile M23 虚拟只读源码重构文件结构
type ReconstructedProtoFile struct {
	FileID        uint64                 `json:"file_id"`
	SchemaID      uint64                 `json:"schema_id"`
	SchemaName    string                 `json:"schema_name"`
	SchemaVersion int                    `json:"schema_version"`
	FilePath      string                 `json:"file_path"`
	PackageName   string                 `json:"package_name"`
	Content       string                 `json:"content"`
	LineCount     int                    `json:"line_count"`
	HasSourceInfo bool                   `json:"has_source_info"`
	SourceOrigin  string                 `json:"source_origin"`
	SymbolAnchors []ProtoSymbolAnchorDTO `json:"symbol_anchors"`
}

type parsedDocTags struct {
	Summary           string
	LeadingClean      string
	TrailingClean     string
	IsDeprecated      bool
	DeprecationReason string
	OwnerTag          string
	ErrorCodes        []ErrorCodeDoc
}

type locMeta struct {
	Leading         string
	Trailing        string
	LeadingDetached string
	StartLine       int
	EndLine         int
}

// formatLocPath 将 Protobuf SourceCodeInfo path 切片格式化为映射键
func formatLocPath(p []int32) string {
	if len(p) == 0 {
		return ""
	}
	parts := make([]string, len(p))
	for i, v := range p {
		parts[i] = strconv.FormatInt(int64(v), 10)
	}
	return strings.Join(parts, ".")
}

func appendLocPath(base []int32, elems ...int32) []int32 {
	out := make([]int32, 0, len(base)+len(elems))
	out = append(out, base...)
	out = append(out, elems...)
	return out
}

func cleanCommentText(raw string) string {
	if raw == "" {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		cleaned = append(cleaned, trimmed)
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

// buildLocationMap 按 SourceCodeInfo Path 建立 1-based 行号与注释索引
func buildLocationMap(fd *descriptorpb.FileDescriptorProto) (map[string]locMeta, bool) {
	out := make(map[string]locMeta)
	if fd == nil || fd.SourceCodeInfo == nil || len(fd.SourceCodeInfo.Location) == 0 {
		return out, false
	}

	for _, loc := range fd.SourceCodeInfo.Location {
		if loc == nil {
			continue
		}
		key := formatLocPath(loc.Path)
		startLine := 0
		endLine := 0
		if len(loc.Span) >= 3 {
			startLine = int(loc.Span[0]) + 1
			if len(loc.Span) >= 4 {
				endLine = int(loc.Span[2]) + 1
			} else {
				endLine = startLine
			}
		}
		var detached []string
		for _, d := range loc.LeadingDetachedComments {
			if c := cleanCommentText(d); c != "" {
				detached = append(detached, c)
			}
		}
		out[key] = locMeta{
			Leading:         cleanCommentText(loc.GetLeadingComments()),
			Trailing:        cleanCommentText(loc.GetTrailingComments()),
			LeadingDetached: strings.Join(detached, "\n\n"),
			StartLine:       startLine,
			EndLine:         endLine,
		}
	}
	return out, len(out) > 0
}

func extractFileHeaderComments(locMap map[string]locMeta) string {
	candidates := make([]string, 0, 3)
	for _, key := range []string{"", "12", "2"} {
		if lm, ok := locMap[key]; ok {
			if lm.LeadingDetached != "" {
				candidates = append(candidates, lm.LeadingDetached)
			}
			if lm.Leading != "" {
				candidates = append(candidates, lm.Leading)
			}
		}
	}
	return strings.TrimSpace(strings.Join(candidates, "\n"))
}

// ParseDocTags 从前置与尾随注释中解析 @deprecated、@owner、@error_code 及首行摘要
func ParseDocTags(leadingRaw, trailingRaw string, optionDeprecated bool) parsedDocTags {
	res := parsedDocTags{
		LeadingClean:  cleanCommentText(leadingRaw),
		TrailingClean: cleanCommentText(trailingRaw),
		IsDeprecated:  optionDeprecated,
		ErrorCodes:    make([]ErrorCodeDoc, 0),
	}

	combined := res.LeadingClean
	if res.TrailingClean != "" {
		if combined != "" {
			combined += "\n" + res.TrailingClean
		} else {
			combined = res.TrailingClean
		}
	}

	var summaryCandidates []string
	for _, line := range strings.Split(combined, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "@owner"):
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "@owner"))
			val = strings.TrimSpace(strings.TrimPrefix(val, ":"))
			if val != "" {
				res.OwnerTag = truncateString(val, 128)
			}
		case strings.HasPrefix(trimmed, "@deprecated"):
			res.IsDeprecated = true
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "@deprecated"))
			val = strings.TrimSpace(strings.TrimPrefix(val, ":"))
			if val != "" {
				res.DeprecationReason = truncateString(val, 512)
			}
		case strings.HasPrefix(trimmed, "@error_code"):
			if ec, ok := parseErrorCodeLine(trimmed); ok {
				res.ErrorCodes = append(res.ErrorCodes, ec)
			}
		default:
			summaryCandidates = append(summaryCandidates, trimmed)
		}
	}

	if res.IsDeprecated && res.DeprecationReason == "" {
		res.DeprecationReason = "已在 Proto 定义中标记为 deprecated"
	}
	if len(summaryCandidates) > 0 {
		res.Summary = truncateString(summaryCandidates[0], 512)
	}
	return res
}

func parseErrorCodeLine(line string) (ErrorCodeDoc, bool) {
	body := strings.TrimSpace(strings.TrimPrefix(line, "@error_code"))
	body = strings.TrimSpace(strings.TrimPrefix(body, ":"))
	if body == "" {
		return ErrorCodeDoc{}, false
	}
	fields := strings.Fields(body)
	if len(fields) == 0 {
		return ErrorCodeDoc{}, false
	}

	doc := ErrorCodeDoc{Code: fields[0]}
	if len(fields) >= 2 {
		doc.HttpOrCrosCode = fields[1]
	}
	if len(fields) >= 3 {
		rest := strings.Join(fields[2:], " ")
		rest = strings.TrimSpace(strings.TrimPrefix(rest, "-"))
		rest = strings.TrimSpace(strings.TrimPrefix(rest, ":"))
		doc.Description = rest
	}
	return doc, true
}

func truncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "…"
}

func buildRegistryFromSchema(schema *models.ProtoSchema, extraFiles []*protoregistry.Files) (*protoregistry.Files, *descriptorpb.FileDescriptorSet, error) {
	if schema == nil || len(schema.DescriptorBytes) == 0 {
		return nil, nil, errors.New("ProtoSchema 或 DescriptorBytes 为空")
	}
	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(schema.DescriptorBytes, &fds); err != nil {
		return nil, nil, fmt.Errorf("反序列化 FileDescriptorSet 失败: %w", err)
	}

	reg := &protoregistry.Files{}
	// 先注册额外补充池中的依赖文件（不覆盖本 Schema 自有文件）
	for _, ef := range extraFiles {
		if ef == nil {
			continue
		}
		ef.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
			if _, err := reg.FindFileByPath(fd.Path()); errors.Is(err, protoregistry.NotFound) {
				_ = reg.RegisterFile(fd)
			}
			return true
		})
	}

	// 将本 Schema 中的 DescriptorProto 解析为 FileDescriptor 并注册
	resolved, err := protodesc.NewFiles(&fds)
	if err == nil {
		resolved.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
			if _, findErr := reg.FindFileByPath(fd.Path()); errors.Is(findErr, protoregistry.NotFound) {
				_ = reg.RegisterFile(fd)
			}
			return true
		})
		return reg, &fds, nil
	}

	// 若 FDS 依赖来自全局/其他激活包且尚未内联在同一个 FDS 中，逐个借助 protodesc.NewFile 注册
	remaining := append([]*descriptorpb.FileDescriptorProto(nil), fds.File...)
	for len(remaining) > 0 {
		progressed := false
		var next []*descriptorpb.FileDescriptorProto
		for _, fp := range remaining {
			if _, findErr := reg.FindFileByPath(fp.GetName()); findErr == nil {
				progressed = true
				continue
			}
			fd, regErr := protodesc.NewFile(fp, reg)
			if regErr == nil {
				_ = reg.RegisterFile(fd)
				progressed = true
			} else {
				next = append(next, fp)
			}
		}
		if !progressed {
			break
		}
		remaining = next
	}

	return reg, &fds, nil
}

// ExtractSymbols 从 ProtoSchema 描述集中提取 SERVICE/METHOD/MESSAGE/ENUM 四类文档符号与行号锚点
func ExtractSymbols(schema *models.ProtoSchema, extraFiles ...*protoregistry.Files) ([]models.ProtoDocSymbol, bool, error) {
	if schema == nil {
		return nil, false, errors.New("schema 不能为空")
	}

	reg, fds, err := buildRegistryFromSchema(schema, extraFiles)
	if err != nil {
		return nil, false, err
	}

	ownSet := make(map[string]struct{})
	var protoFileNames []string
	if len(schema.ProtoFiles) > 0 {
		_ = json.Unmarshal(schema.ProtoFiles, &protoFileNames)
	}
	for _, fn := range protoFileNames {
		if fn != "" {
			ownSet[fn] = struct{}{}
		}
	}

	locByFile := make(map[string]map[string]locMeta)
	hasAnySourceInfo := false
	var targetProtos []*descriptorpb.FileDescriptorProto

	for _, fdProto := range fds.File {
		if fdProto == nil {
			continue
		}
		if len(ownSet) > 0 {
			if _, ok := ownSet[fdProto.GetName()]; !ok {
				continue
			}
		}
		targetProtos = append(targetProtos, fdProto)
		locMap, hasLoc := buildLocationMap(fdProto)
		locByFile[fdProto.GetName()] = locMap
		if hasLoc {
			hasAnySourceInfo = true
		}
	}

	// 若 schema.ProtoFiles 与 FDS 文件名未匹配，退化处理 FDS 全部文件
	if len(targetProtos) == 0 {
		for _, fdProto := range fds.File {
			if fdProto == nil {
				continue
			}
			targetProtos = append(targetProtos, fdProto)
			locMap, hasLoc := buildLocationMap(fdProto)
			locByFile[fdProto.GetName()] = locMap
			if hasLoc {
				hasAnySourceInfo = true
			}
		}
	}

	symbols := make([]models.ProtoDocSymbol, 0, 32)
	seenKey := make(map[string]struct{})
	appendUnique := func(sym models.ProtoDocSymbol) {
		k := sym.SymbolKind + "::" + sym.FullName
		if _, exists := seenKey[k]; exists {
			return
		}
		seenKey[k] = struct{}{}
		symbols = append(symbols, sym)
	}

	for _, fdProto := range targetProtos {
		fileName := fdProto.GetName()
		pkgName := fdProto.GetPackage()
		locMap := locByFile[fileName]
		fileComments := extractFileHeaderComments(locMap)

		// 1. 提取顶层 Enum
		for enumIdx, enumDesc := range fdProto.EnumType {
			sym := extractEnumSymbol(schema.ID, fileName, pkgName, enumDesc, "", []int32{5, int32(enumIdx)}, locMap)
			appendUnique(sym)
		}

		// 2. 递归提取 Message（含嵌套 Message 与嵌套 Enum）
		for msgIdx, msgDesc := range fdProto.MessageType {
			extractMessageRecursive(
				schema.ID,
				fileName,
				pkgName,
				"",
				msgDesc,
				[]int32{4, int32(msgIdx)},
				locMap,
				locByFile,
				reg,
				appendUnique,
			)
		}

		// 3. 提取 Service 与 Method
		for svcIdx, svcDesc := range fdProto.Service {
			svcPath := []int32{6, int32(svcIdx)}
			svcLM := locMap[formatLocPath(svcPath)]
			svcOptDep := svcDesc.Options != nil && svcDesc.Options.GetDeprecated()
			svcTags := ParseDocTags(svcLM.Leading, svcLM.Trailing, svcOptDep)

			svcFullName := joinFullName(pkgName, svcDesc.GetName())
			svcMetaBytes, _ := json.Marshal(serviceMetaStored{FileComments: fileComments})
			svcErrBytes, _ := json.Marshal(svcTags.ErrorCodes)

			appendUnique(models.ProtoDocSymbol{
				SchemaID:            schema.ID,
				SymbolKind:          models.DocSymbolService,
				FullName:            truncateString(svcFullName, 255),
				PackageName:         truncateString(pkgName, 128),
				ShortName:           truncateString(svcDesc.GetName(), 128),
				ProtoFile:           truncateString(fileName, 255),
				StartLine:           svcLM.StartLine,
				EndLine:             svcLM.EndLine,
				LeadingComments:     svcLM.Leading,
				TrailingComments:    svcLM.Trailing,
				Summary:             svcTags.Summary,
				IsDeprecated:        svcTags.IsDeprecated,
				DeprecationReason:   svcTags.DeprecationReason,
				OwnerTag:            svcTags.OwnerTag,
				ErrorCodesJSON:      datatypes.JSON(svcErrBytes),
				FieldsMetaJSON:      datatypes.JSON(svcMetaBytes),
				DefaultRequestJSON:  datatypes.JSON([]byte("null")),
				DefaultResponseJSON: datatypes.JSON([]byte("null")),
			})

			for mIdx, mDesc := range svcDesc.Method {
				mPath := []int32{6, int32(svcIdx), 2, int32(mIdx)}
				mLM := locMap[formatLocPath(mPath)]
				mOptDep := svcTags.IsDeprecated || (mDesc.Options != nil && mDesc.Options.GetDeprecated())
				mTags := ParseDocTags(mLM.Leading, mLM.Trailing, mOptDep)

				if mTags.OwnerTag == "" {
					mTags.OwnerTag = svcTags.OwnerTag
				}
				if svcTags.IsDeprecated && mTags.IsDeprecated && mLM.Leading == "" && mLM.Trailing == "" {
					mTags.DeprecationReason = svcTags.DeprecationReason
				}

				reqFull := strings.TrimPrefix(mDesc.GetInputType(), ".")
				rspFull := strings.TrimPrefix(mDesc.GetOutputType(), ".")
				defReq := buildDefaultJSONByName(reg, reqFull)
				defRsp := buildDefaultJSONByName(reg, rspFull)

				mMetaBytes, _ := json.Marshal(methodMetaStored{
					FileComments:       fileComments,
					ReqMessageFullName: reqFull,
					RspMessageFullName: rspFull,
					ClientStreaming:    mDesc.GetClientStreaming(),
					ServerStreaming:    mDesc.GetServerStreaming(),
				})
				mErrBytes, _ := json.Marshal(mTags.ErrorCodes)

				appendUnique(models.ProtoDocSymbol{
					SchemaID:            schema.ID,
					SymbolKind:          models.DocSymbolMethod,
					FullName:            truncateString(svcFullName+"."+mDesc.GetName(), 255),
					PackageName:         truncateString(pkgName, 128),
					ShortName:           truncateString(mDesc.GetName(), 128),
					ProtoFile:           truncateString(fileName, 255),
					StartLine:           mLM.StartLine,
					EndLine:             mLM.EndLine,
					LeadingComments:     mLM.Leading,
					TrailingComments:    mLM.Trailing,
					Summary:             mTags.Summary,
					IsDeprecated:        mTags.IsDeprecated,
					DeprecationReason:   mTags.DeprecationReason,
					OwnerTag:            mTags.OwnerTag,
					ErrorCodesJSON:      datatypes.JSON(mErrBytes),
					FieldsMetaJSON:      datatypes.JSON(mMetaBytes),
					DefaultRequestJSON:  datatypes.JSON(defReq),
					DefaultResponseJSON: datatypes.JSON(defRsp),
				})
			}
		}
	}

	sort.Slice(symbols, func(i, j int) bool {
		if symbols[i].ProtoFile != symbols[j].ProtoFile {
			return symbols[i].ProtoFile < symbols[j].ProtoFile
		}
		if symbols[i].StartLine != symbols[j].StartLine {
			return symbols[i].StartLine < symbols[j].StartLine
		}
		if symbols[i].SymbolKind != symbols[j].SymbolKind {
			return symbols[i].SymbolKind < symbols[j].SymbolKind
		}
		return symbols[i].FullName < symbols[j].FullName
	})

	return symbols, hasAnySourceInfo, nil
}

func joinFullName(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

func extractEnumSymbol(
	schemaID uint64,
	fileName, pkgName string,
	enumDesc *descriptorpb.EnumDescriptorProto,
	parentFullName string,
	enumPath []int32,
	locMap map[string]locMeta,
) models.ProtoDocSymbol {
	lm := locMap[formatLocPath(enumPath)]
	optDep := enumDesc.Options != nil && enumDesc.Options.GetDeprecated()
	tags := ParseDocTags(lm.Leading, lm.Trailing, optDep)

	fullPrefix := pkgName
	if parentFullName != "" {
		fullPrefix = parentFullName
	}
	fullName := joinFullName(fullPrefix, enumDesc.GetName())

	values := make([]EnumValueDoc, 0, len(enumDesc.Value))
	for vIdx, v := range enumDesc.Value {
		vPath := appendLocPath(enumPath, 2, int32(vIdx))
		vLM := locMap[formatLocPath(vPath)]
		vOptDep := v.Options != nil && v.Options.GetDeprecated()
		vTags := ParseDocTags(vLM.Leading, vLM.Trailing, vOptDep)
		vComment := combineLeadingTrailing(vTags.LeadingClean, vTags.TrailingClean)
		values = append(values, EnumValueDoc{
			Name:              v.GetName(),
			Number:            v.GetNumber(),
			Comment:           vComment,
			IsDeprecated:      vTags.IsDeprecated,
			DeprecationReason: vTags.DeprecationReason,
			StartLine:         vLM.StartLine,
		})
	}

	valuesJSON, _ := json.Marshal(values)
	errJSON, _ := json.Marshal(tags.ErrorCodes)

	return models.ProtoDocSymbol{
		SchemaID:            schemaID,
		SymbolKind:          models.DocSymbolEnum,
		FullName:            truncateString(fullName, 255),
		PackageName:         truncateString(pkgName, 128),
		ShortName:           truncateString(enumDesc.GetName(), 128),
		ProtoFile:           truncateString(fileName, 255),
		StartLine:           lm.StartLine,
		EndLine:             lm.EndLine,
		LeadingComments:     lm.Leading,
		TrailingComments:    lm.Trailing,
		Summary:             tags.Summary,
		IsDeprecated:        tags.IsDeprecated,
		DeprecationReason:   tags.DeprecationReason,
		OwnerTag:            tags.OwnerTag,
		ErrorCodesJSON:      datatypes.JSON(errJSON),
		FieldsMetaJSON:      datatypes.JSON(valuesJSON),
		DefaultRequestJSON:  datatypes.JSON([]byte("null")),
		DefaultResponseJSON: datatypes.JSON([]byte("null")),
	}
}

func extractMessageRecursive(
	schemaID uint64,
	fileName, pkgName, parentFullName string,
	msgDesc *descriptorpb.DescriptorProto,
	msgPath []int32,
	locMap map[string]locMeta,
	locByFile map[string]map[string]locMeta,
	reg *protoregistry.Files,
	emit func(models.ProtoDocSymbol),
) {
	if msgDesc == nil || (msgDesc.Options != nil && msgDesc.Options.GetMapEntry()) {
		return
	}
	fullPrefix := pkgName
	if parentFullName != "" {
		fullPrefix = parentFullName
	}
	fullName := joinFullName(fullPrefix, msgDesc.GetName())

	lm := locMap[formatLocPath(msgPath)]
	optDep := msgDesc.Options != nil && msgDesc.Options.GetDeprecated()
	tags := ParseDocTags(lm.Leading, lm.Trailing, optDep)

	var fields []FieldDocNode
	var defJSON []byte
	if reg != nil {
		if d, err := reg.FindDescriptorByName(protoreflect.FullName(fullName)); err == nil {
			if md, ok := d.(protoreflect.MessageDescriptor); ok {
				fields = ExtractMessageFieldTree(md, locByFile, MaxDocFieldDepth)
				defJSON, _ = BuildDefaultMessageJSON(md)
			}
		}
	}
	if fields == nil {
		fields = extractFlatFieldsFromProto(fileName, msgDesc, msgPath, locMap)
	}
	if len(defJSON) == 0 {
		defJSON = []byte("null")
	}

	meta := MessageDocMeta{
		FullName:  fullName,
		ProtoFile: fileName,
		StartLine: lm.StartLine,
		EndLine:   lm.EndLine,
		Comment:   combineLeadingTrailing(tags.LeadingClean, tags.TrailingClean),
		Fields:    fields,
	}
	metaJSON, _ := json.Marshal(meta)
	errJSON, _ := json.Marshal(tags.ErrorCodes)

	emit(models.ProtoDocSymbol{
		SchemaID:            schemaID,
		SymbolKind:          models.DocSymbolMessage,
		FullName:            truncateString(fullName, 255),
		PackageName:         truncateString(pkgName, 128),
		ShortName:           truncateString(msgDesc.GetName(), 128),
		ProtoFile:           truncateString(fileName, 255),
		StartLine:           lm.StartLine,
		EndLine:             lm.EndLine,
		LeadingComments:     lm.Leading,
		TrailingComments:    lm.Trailing,
		Summary:             tags.Summary,
		IsDeprecated:        tags.IsDeprecated,
		DeprecationReason:   tags.DeprecationReason,
		OwnerTag:            tags.OwnerTag,
		ErrorCodesJSON:      datatypes.JSON(errJSON),
		FieldsMetaJSON:      datatypes.JSON(metaJSON),
		DefaultRequestJSON:  datatypes.JSON(defJSON),
		DefaultResponseJSON: datatypes.JSON([]byte("null")),
	})

	for nestedEnumIdx, nestedEnum := range msgDesc.EnumType {
		ePath := appendLocPath(msgPath, 4, int32(nestedEnumIdx))
		emit(extractEnumSymbol(schemaID, fileName, pkgName, nestedEnum, fullName, ePath, locMap))
	}
	for nestedMsgIdx, nestedMsg := range msgDesc.NestedType {
		nPath := appendLocPath(msgPath, 3, int32(nestedMsgIdx))
		extractMessageRecursive(schemaID, fileName, pkgName, fullName, nestedMsg, nPath, locMap, locByFile, reg, emit)
	}
}

func extractFlatFieldsFromProto(
	fileName string,
	msgDesc *descriptorpb.DescriptorProto,
	msgPath []int32,
	locMap map[string]locMeta,
) []FieldDocNode {
	nodes := make([]FieldDocNode, 0, len(msgDesc.Field))
	for fIdx, f := range msgDesc.Field {
		fPath := appendLocPath(msgPath, 2, int32(fIdx))
		fLM := locMap[formatLocPath(fPath)]
		fOptDep := f.Options != nil && f.Options.GetDeprecated()
		fTags := ParseDocTags(fLM.Leading, fLM.Trailing, fOptDep)

		label := "optional"
		switch f.GetLabel() {
		case descriptorpb.FieldDescriptorProto_LABEL_REPEATED:
			label = "repeated"
		case descriptorpb.FieldDescriptorProto_LABEL_REQUIRED:
			label = "required"
		}
		typeName := strings.TrimPrefix(strings.ToLower(strings.TrimPrefix(f.GetType().String(), "TYPE_")), ".")
		typeFull := strings.TrimPrefix(f.GetTypeName(), ".")
		if typeFull != "" {
			typeName = path.Base(strings.ReplaceAll(typeFull, ".", "/"))
		}
		oneofGroup := ""
		if f.OneofIndex != nil {
			idx := int(*f.OneofIndex)
			if idx >= 0 && idx < len(msgDesc.OneofDecl) && !f.GetProto3Optional() {
				oneofGroup = msgDesc.OneofDecl[idx].GetName()
			}
		}

		nodes = append(nodes, FieldDocNode{
			Name:              f.GetName(),
			JSONName:          f.GetJsonName(),
			Number:            f.GetNumber(),
			Type:              typeName,
			TypeFullName:      typeFull,
			ProtoFile:         fileName,
			StartLine:         fLM.StartLine,
			Label:             label,
			OneofGroup:        oneofGroup,
			Comment:           combineLeadingTrailing(fTags.LeadingClean, fTags.TrailingClean),
			IsDeprecated:      fTags.IsDeprecated,
			DeprecationReason: fTags.DeprecationReason,
		})
	}
	return nodes
}

func combineLeadingTrailing(leading, trailing string) string {
	switch {
	case leading != "" && trailing != "":
		return leading + " (" + trailing + ")"
	case leading != "":
		return leading
	default:
		return trailing
	}
}

// ExtractMessageFieldTree 递归展开 Message 字段文档树，内置环形引用检测与 MaxDocFieldDepth(12) 深度熔断
func ExtractMessageFieldTree(
	msgDesc protoreflect.MessageDescriptor,
	locByFile map[string]map[string]locMeta,
	maxDepth int,
) []FieldDocNode {
	if msgDesc == nil {
		return []FieldDocNode{}
	}
	if maxDepth <= 0 || maxDepth > MaxDocFieldDepth {
		maxDepth = MaxDocFieldDepth
	}
	visited := map[string]bool{
		string(msgDesc.FullName()): true,
	}
	return buildFieldDocNodes(msgDesc, locByFile, visited, 1, maxDepth)
}

func buildFieldDocNodes(
	msgDesc protoreflect.MessageDescriptor,
	locByFile map[string]map[string]locMeta,
	visited map[string]bool,
	depth, maxDepth int,
) []FieldDocNode {
	fields := msgDesc.Fields()
	out := make([]FieldDocNode, 0, fields.Len())

	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		loc := lookupDescriptorLocation(fd)
		if loc.StartLine == 0 && locByFile != nil {
			if m, ok := locByFile[fd.ParentFile().Path()]; ok {
				// fallback lookup from raw FileDescriptorProto location map
				if lm, found := findFieldLocationByName(fd, m); found {
					loc = lm
				}
			}
		}

		optDep := isFieldDeprecated(fd)
		tags := ParseDocTags(loc.Leading, loc.Trailing, optDep)

		label := "optional"
		switch fd.Cardinality() {
		case protoreflect.Repeated:
			label = "repeated"
		case protoreflect.Required:
			label = "required"
		}

		node := FieldDocNode{
			Name:              string(fd.Name()),
			JSONName:          fd.JSONName(),
			Number:            int32(fd.Number()),
			Type:              fd.Kind().String(),
			ProtoFile:         fd.ParentFile().Path(),
			StartLine:         loc.StartLine,
			Label:             label,
			Comment:           combineLeadingTrailing(tags.LeadingClean, tags.TrailingClean),
			IsDeprecated:      tags.IsDeprecated,
			DeprecationReason: tags.DeprecationReason,
		}

		if oo := fd.ContainingOneof(); oo != nil && !oo.IsSynthetic() {
			node.OneofGroup = string(oo.Name())
			ooLoc := lookupDescriptorLocation(oo)
			if ooLoc.Leading != "" || ooLoc.Trailing != "" {
				node.OneofComment = combineLeadingTrailing(ooLoc.Leading, ooLoc.Trailing)
			}
		}

		if fd.IsMap() {
			node.IsMap = true
			node.Label = "map"
			kDesc := fd.MapKey()
			vDesc := fd.MapValue()
			node.MapKeyType = kDesc.Kind().String()
			node.MapValueType = vDesc.Kind().String()
			if vDesc.Kind() == protoreflect.MessageKind && vDesc.Message() != nil {
				node.MapValueType = string(vDesc.Message().Name())
				node.TypeFullName = string(vDesc.Message().FullName())
			} else if vDesc.Kind() == protoreflect.EnumKind && vDesc.Enum() != nil {
				node.MapValueType = string(vDesc.Enum().Name())
				node.TypeFullName = string(vDesc.Enum().FullName())
				node.EnumValues = extractEnumValuesFromReflect(vDesc.Enum())
			}
			node.Type = fmt.Sprintf("map<%s, %s>", node.MapKeyType, node.MapValueType)
			if vDesc.Kind() == protoreflect.MessageKind && vDesc.Message() != nil {
				subMsg := vDesc.Message()
				subName := string(subMsg.FullName())
				if visited[subName] {
					node.Truncated = true
					node.TruncatedReason = "CYCLE_DETECTED"
				} else if depth >= maxDepth {
					node.Truncated = true
					node.TruncatedReason = "MAX_DEPTH_12"
				} else {
					visited[subName] = true
					node.Children = buildFieldDocNodes(subMsg, locByFile, visited, depth+1, maxDepth)
					delete(visited, subName)
				}
			}
			out = append(out, node)
			continue
		}

		switch fd.Kind() {
		case protoreflect.EnumKind:
			if ed := fd.Enum(); ed != nil {
				node.Type = string(ed.Name())
				node.TypeFullName = string(ed.FullName())
				node.EnumValues = extractEnumValuesFromReflect(ed)
			}
		case protoreflect.MessageKind, protoreflect.GroupKind:
			if subMsg := fd.Message(); subMsg != nil {
				subName := string(subMsg.FullName())
				node.Type = string(subMsg.Name())
				node.TypeFullName = subName
				if visited[subName] {
					node.Truncated = true
					node.TruncatedReason = "CYCLE_DETECTED"
				} else if depth >= maxDepth {
					node.Truncated = true
					node.TruncatedReason = "MAX_DEPTH_12"
				} else {
					visited[subName] = true
					node.Children = buildFieldDocNodes(subMsg, locByFile, visited, depth+1, maxDepth)
					delete(visited, subName)
				}
			}
		}

		out = append(out, node)
	}

	return out
}

func extractEnumValuesFromReflect(ed protoreflect.EnumDescriptor) []EnumValueDoc {
	if ed == nil {
		return nil
	}
	vals := ed.Values()
	out := make([]EnumValueDoc, 0, vals.Len())
	for i := 0; i < vals.Len(); i++ {
		ev := vals.Get(i)
		loc := lookupDescriptorLocation(ev)
		optDep := isEnumValDeprecated(ev)
		tags := ParseDocTags(loc.Leading, loc.Trailing, optDep)
		out = append(out, EnumValueDoc{
			Name:              string(ev.Name()),
			Number:            int32(ev.Number()),
			Comment:           combineLeadingTrailing(tags.LeadingClean, tags.TrailingClean),
			IsDeprecated:      tags.IsDeprecated,
			DeprecationReason: tags.DeprecationReason,
			StartLine:         loc.StartLine,
		})
	}
	return out
}

func lookupDescriptorLocation(desc protoreflect.Descriptor) locMeta {
	if desc == nil || desc.ParentFile() == nil {
		return locMeta{}
	}
	sl := desc.ParentFile().SourceLocations().ByDescriptor(desc)
	startLine := 0
	endLine := 0
	if sl.StartLine > 0 || sl.StartColumn > 0 || sl.EndLine > 0 || sl.EndColumn > 0 {
		startLine = sl.StartLine + 1
		endLine = sl.EndLine + 1
	}
	return locMeta{
		Leading:   cleanCommentText(sl.LeadingComments),
		Trailing:  cleanCommentText(sl.TrailingComments),
		StartLine: startLine,
		EndLine:   endLine,
	}
}

func findFieldLocationByName(_ protoreflect.FieldDescriptor, _ map[string]locMeta) (locMeta, bool) {
	return locMeta{}, false
}

func isFieldDeprecated(fd protoreflect.FieldDescriptor) bool {
	if fd == nil {
		return false
	}
	opts, ok := fd.Options().(*descriptorpb.FieldOptions)
	return ok && opts != nil && opts.GetDeprecated()
}

func isEnumValDeprecated(ev protoreflect.EnumValueDescriptor) bool {
	if ev == nil {
		return false
	}
	opts, ok := ev.Options().(*descriptorpb.EnumValueOptions)
	return ok && opts != nil && opts.GetDeprecated()
}
