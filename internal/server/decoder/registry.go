package decoder

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"code-traefik/models"
	"code-traefik/pkg/crosrpc"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	_ "google.golang.org/protobuf/types/known/anypb"
	_ "google.golang.org/protobuf/types/known/durationpb"
	_ "google.golang.org/protobuf/types/known/emptypb"
	_ "google.golang.org/protobuf/types/known/fieldmaskpb"
	_ "google.golang.org/protobuf/types/known/structpb"
	_ "google.golang.org/protobuf/types/known/timestamppb"
	_ "google.golang.org/protobuf/types/known/wrapperspb"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	MaxDescriptorFileSize = 16 * 1024 * 1024 // 16MB (16,777,216 Bytes)

	DecodeStatusSuccess         = "SUCCESS"
	DecodeStatusDecodedJSON     = "DECODED_JSON"
	DecodeStatusNoSchemaRawHex  = "NO_SCHEMA_RAW_HEX"
	DecodeStatusDecodeErrRawHex = "DECODE_ERR_RAW_HEX"
	DecodeStatusTruncated       = "TRUNCATED"
	DecodeStatusMngRaw          = "MNG_RAW"
)

var (
	ErrNotImplemented          = errors.New("decoder: not implemented")
	ErrSchemaNotFound          = errors.New("Protobuf 描述集版本不存在")
	ErrInvalidSchemaName       = errors.New("描述集名称不能为空且长度不得超过 128 个字符")
	ErrDescriptorEmpty         = errors.New("上传的 .pb 描述集文件内容为空")
	ErrDescriptorTooLarge      = errors.New("上传的 .pb 描述集文件超过 16MB 大小限制")
	ErrInvalidDescriptorProto  = errors.New("非法的 FileDescriptorSet 二进制结构或缺失 import 依赖")
	ErrSchemaOperationConflict = errors.New("待激活描述集与其他已生效描述集存在接口映射冲突")
	ErrDeleteActiveSchema      = errors.New("当前描述集版本处于生效状态，禁止删除")
)

// OperationConflictItem 跨描述集激活冲突明细项
type OperationConflictItem struct {
	ServiceName        string `json:"service_name"`
	Operation          string `json:"operation"`
	ConflictSchemaID   uint64 `json:"conflict_schema_id"`
	ConflictSchemaName string `json:"conflict_schema_name"`
	ConflictVersion    int    `json:"conflict_version"`
}

// SchemaConflictError 激活冲突结构化错误 (409 Conflict)
type SchemaConflictError struct {
	Conflicts []OperationConflictItem `json:"conflicts"`
}

func (e *SchemaConflictError) Error() string {
	if len(e.Conflicts) == 0 {
		return ErrSchemaOperationConflict.Error()
	}
	first := e.Conflicts[0]
	return fmt.Sprintf("接口 [%s:%s] 已由生效描述集 [%s v%d] 占用（共 %d 处冲突）",
		first.ServiceName, first.Operation, first.ConflictSchemaName, first.ConflictVersion, len(e.Conflicts))
}

func (e *SchemaConflictError) Unwrap() error {
	return ErrSchemaOperationConflict
}

// BrokenDependentItemDTO 依赖受损/缺失明细项
type BrokenDependentItemDTO struct {
	SchemaID      uint64   `json:"schema_id,omitempty"`
	SchemaName    string   `json:"schema_name"`
	Version       int      `json:"version,omitempty"`
	MissingProtos []string `json:"missing_protos,omitempty"`
	Reason        string   `json:"reason,omitempty"`
}

// SchemaBrokenDependentsError 破坏反向依赖或闭包不全结构化错误 (409 Conflict)
type SchemaBrokenDependentsError struct {
	BrokenDependents []BrokenDependentItemDTO `json:"broken_dependents"`
}

func (e *SchemaBrokenDependentsError) Error() string {
	if len(e.BrokenDependents) == 0 {
		return "存在破坏下游描述集依赖闭包或缺失前置依赖"
	}
	first := e.BrokenDependents[0]
	if first.Reason != "" {
		return fmt.Sprintf("描述集 [%s] 依赖闭包不自洽: %s", first.SchemaName, first.Reason)
	}
	if len(first.MissingProtos) > 0 {
		return fmt.Sprintf("描述集 [%s] 缺失外部依赖文件: %v", first.SchemaName, first.MissingProtos)
	}
	return fmt.Sprintf("描述集 [%s] 依赖闭包不自洽或破坏下游依赖", first.SchemaName)
}

func (e *SchemaBrokenDependentsError) Unwrap() error {
	return ErrInvalidDescriptorProto
}

// CompiledOperation 编译后的单接口动态编解码元数据
type CompiledOperation struct {
	SchemaID           uint64
	SchemaName         string
	SchemaVersion      int
	ServiceName        string
	Operation          string
	ReqDesc            protoreflect.MessageDescriptor
	RspDesc            protoreflect.MessageDescriptor
	DiffIgnorePaths    []string // 方法级忽略规则 (Operation.DiffIgnorePaths)
	ServiceIgnorePaths []string // 服务级默认忽略规则 (ModuleService.DiffIgnorePaths)
}

// CompiledRegistry 不可变无锁内存注册表快照
type CompiledRegistry struct {
	Files          *protoregistry.Files
	Operations     map[string]*CompiledOperation // key: serviceName + ":" + operation
	ServiceAliases map[string]string             // publishedCustomServiceName -> contractServiceName (ADR-16 v2.0)
	ServiceIgnores map[string][]string           // 运行期发布服务名 -> DiffIgnorePaths 列表
}

// UploadSchemaInput 上传 .pb 描述集参数
type UploadSchemaInput struct {
	Name           string
	Category       string // BASE_LIB | BUSINESS | GATEWAY
	VersionTag     string
	FileName       string
	Description    string
	RawBytes       []byte
	AutoActivate   bool
	AllowBreaking  bool
	BreakingReason string
	OperatorID     uint
}

// DecodeResult 单次 Payload 动态反序列化结果
type DecodeResult struct {
	DecodeStatus    string  `json:"decode_status"`
	SchemaID        *uint64 `json:"schema_id,omitempty"`
	SchemaVersion   *int    `json:"schema_version,omitempty"`
	SchemaName      string  `json:"schema_name,omitempty"`
	MessageFullName string  `json:"message_full_name,omitempty"`
	JSONBytes       []byte  `json:"json_bytes,omitempty"`
	RawHex          string  `json:"raw_hex,omitempty"`
}

// DecodedFrameResult 单帧完整 CROS RPC 头解析 + Payload 动态解码结果
type DecodedFrameResult struct {
	CommHead    crosrpc.CommHead     `json:"comm_head"`
	ReqHead     *crosrpc.ReqHead     `json:"req_head,omitempty"`
	RspHead     *crosrpc.RspHead     `json:"rsp_head,omitempty"`
	MngHead     *crosrpc.MngHead     `json:"mng_head,omitempty"`
	ExtendHeads []crosrpc.ExtendHead `json:"extend_heads,omitempty"`
	ServiceName string               `json:"service_name"`
	Operation   string               `json:"operation"`
	Payload     []byte               `json:"-"`
	Decode      DecodeResult         `json:"decode"`
}

// Registry 线程安全无锁 Protobuf 描述集与动态解码中心
type Registry struct {
	db       *gorm.DB
	compiled atomic.Pointer[CompiledRegistry]
	reloadMu sync.Mutex
}

// NewRegistry 构造解码器实例并预热编译所有处于激活态的 Schema
func NewRegistry(db *gorm.DB) (*Registry, error) {
	r := &Registry{db: db}
	if err := r.Reload(); err != nil {
		return nil, err
	}
	return r, nil
}

// findWKTFileDescriptor 获取标准库内置 google/protobuf/ Well-Known Type 描述
func findWKTFileDescriptor(path string) *descriptorpb.FileDescriptorProto {
	if strings.HasPrefix(path, "google/protobuf/") {
		if fd, err := protoregistry.GlobalFiles.FindFileByPath(path); err == nil && fd != nil {
			return protodesc.ToFileDescriptorProto(fd)
		}
	}
	return nil
}

// ensureWKTs 递归解析并补全需要的 google/protobuf/ Well-Known Types
func ensureWKTs(filesMap map[string]*descriptorpb.FileDescriptorProto) {
	queue := make([]string, 0)
	for _, f := range filesMap {
		for _, dep := range f.Dependency {
			if strings.HasPrefix(dep, "google/protobuf/") {
				if _, ok := filesMap[dep]; !ok {
					queue = append(queue, dep)
				}
			}
		}
	}
	for len(queue) > 0 {
		dep := queue[0]
		queue = queue[1:]
		if _, ok := filesMap[dep]; ok {
			continue
		}
		if wkt := findWKTFileDescriptor(dep); wkt != nil {
			filesMap[dep] = wkt
			for _, subDep := range wkt.Dependency {
				if strings.HasPrefix(subDep, "google/protobuf/") {
					if _, ok := filesMap[subDep]; !ok {
						queue = append(queue, subDep)
					}
				}
			}
		}
	}
}

// topologicalSortFiles 对 FileDescriptorProto 按 dependency 做 DAG 拓扑排序
func topologicalSortFiles(filesMap map[string]*descriptorpb.FileDescriptorProto) ([]*descriptorpb.FileDescriptorProto, []string, error) {
	ensureWKTs(filesMap)

	var result []*descriptorpb.FileDescriptorProto
	visited := make(map[string]int) // 0: unvisited, 1: visiting, 2: visited
	var missingImports []string

	var visit func(name string)
	visit = func(name string) {
		state := visited[name]
		if state == 2 {
			return
		}
		if state == 1 {
			return
		}

		f, ok := filesMap[name]
		if !ok {
			if wkt := findWKTFileDescriptor(name); wkt != nil {
				filesMap[name] = wkt
				f = wkt
			} else {
				missingImports = append(missingImports, name)
				return
			}
		}

		visited[name] = 1
		for _, dep := range f.Dependency {
			visit(dep)
		}
		visited[name] = 2
		result = append(result, f)
	}

	// 稳定排序文件名遍历
	names := make([]string, 0, len(filesMap))
	for n := range filesMap {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		if visited[name] == 0 {
			visit(name)
		}
	}

	if len(missingImports) > 0 {
		dedupSet := make(map[string]struct{})
		var dedup []string
		for _, m := range missingImports {
			if _, ok := dedupSet[m]; !ok {
				dedupSet[m] = struct{}{}
				dedup = append(dedup, m)
			}
		}
		return nil, dedup, fmt.Errorf("missing imports: %v", dedup)
	}

	return result, nil, nil
}

// extractOperationsFromDescriptor 解析 FileDescriptorSet 提取本包自有服务与方法映射及包含的源文件清单
func extractOperationsFromDescriptor(
	rawBytes []byte,
	category string,
	existingBaseLibProtos []string,
) (*descriptorpb.FileDescriptorSet, []*descriptorpb.FileDescriptorProto, []models.ProtoOperation, []string, []string, error) {
	if len(rawBytes) == 0 {
		return nil, nil, nil, nil, nil, ErrDescriptorEmpty
	}
	if len(rawBytes) > MaxDescriptorFileSize {
		return nil, nil, nil, nil, nil, ErrDescriptorTooLarge
	}

	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(rawBytes, &fds); err != nil {
		return nil, nil, nil, nil, nil, ErrInvalidDescriptorProto
	}
	if len(fds.File) == 0 || len(fds.ProtoReflect().GetUnknown()) > 0 {
		return nil, nil, nil, nil, nil, ErrInvalidDescriptorProto
	}

	baseLibSet := make(map[string]struct{}, len(existingBaseLibProtos))
	for _, p := range existingBaseLibProtos {
		baseLibSet[p] = struct{}{}
	}

	// 区分自有文件 (own_proto_files) 与借用已生效 BASE_LIB 文件
	var ownFiles []*descriptorpb.FileDescriptorProto
	for _, file := range fds.File {
		fileName := file.GetName()
		if category != models.ProtoCategoryBaseLib {
			if _, isBase := baseLibSet[fileName]; isBase {
				// 已由生效 BASE_LIB 托管，不属于本包自有定义
				continue
			}
		}
		ownFiles = append(ownFiles, file)
	}
	if len(ownFiles) == 0 {
		ownFiles = fds.File
	}

	ownFileNamesSet := make(map[string]struct{}, len(ownFiles))
	protoFiles := make([]string, 0, len(ownFiles))
	for _, file := range ownFiles {
		name := file.GetName()
		ownFileNamesSet[name] = struct{}{}
		protoFiles = append(protoFiles, name)
	}
	sort.Strings(protoFiles)

	// 计算外部依赖 external_imports = 遍历 ownFiles 的 Dependency 中不属于 ownFiles 的项目
	extSet := make(map[string]struct{})
	for _, file := range ownFiles {
		for _, dep := range file.Dependency {
			if _, ok := ownFileNamesSet[dep]; !ok {
				extSet[dep] = struct{}{}
			}
		}
	}
	externalImports := make([]string, 0, len(extSet))
	for dep := range extSet {
		externalImports = append(externalImports, dep)
	}
	sort.Strings(externalImports)

	// 仅从自有文件 ownFiles 中提取 ProtoOperation
	var ops []models.ProtoOperation
	for _, file := range ownFiles {
		pkg := file.GetPackage()
		fileName := file.GetName()
		for _, svc := range file.Service {
			svcName := svc.GetName()
			fullServiceName := svcName
			if pkg != "" {
				fullServiceName = pkg + "." + svcName
			}

			for _, method := range svc.Method {
				opName := method.GetName()
				reqType := strings.TrimPrefix(method.GetInputType(), ".")
				rspType := strings.TrimPrefix(method.GetOutputType(), ".")

				ops = append(ops, models.ProtoOperation{
					ProtoFile:          fileName,
					ProtoPackage:       pkg,
					ProtoService:       svcName,
					ServiceName:        fullServiceName,
					Operation:          opName,
					ReqMessageFullName: reqType,
					RspMessageFullName: rspType,
					DiffIgnorePaths:    datatypes.JSON("[]"),
					ClientStreaming:    method.GetClientStreaming(),
					ServerStreaming:    method.GetServerStreaming(),
					IsActive:           false,
				})
			}
		}
	}

	return &fds, ownFiles, ops, protoFiles, externalImports, nil
}

// Reload 重载全量生效中的描述集并构建不可变 CompiledRegistry 快照
func (r *Registry) Reload() error {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()

	var activeSchemas []models.ProtoSchema
	if err := r.db.Where("is_active = true").Order("id ASC").Find(&activeSchemas).Error; err != nil {
		return err
	}

	opsMap := make(map[string]*CompiledOperation)

	// 1. 加载多实现服务契约映射，用于构建自定义运行期服务名到 LIB 契约名的别名字典 (ADR-16 v2.0)
	var serviceContracts []models.ServiceContract
	contractNameByID := make(map[uint64]string)
	if err := r.db.Find(&serviceContracts).Error; err == nil {
		for _, c := range serviceContracts {
			contractNameByID[c.ID] = strings.TrimSpace(c.ServiceName)
		}
	}

	var moduleServices []models.ModuleService
	svcIgnoresMap := make(map[string][]string)
	serviceAliases := make(map[string]string)
	if err := r.db.Find(&moduleServices).Error; err == nil {
		for _, ms := range moduleServices {
			msName := strings.TrimSpace(ms.ServiceName)
			if ms.ContractID != nil && *ms.ContractID > 0 {
				if contractSvc, ok := contractNameByID[*ms.ContractID]; ok && contractSvc != "" && msName != "" && msName != contractSvc {
					serviceAliases[msName] = contractSvc
				}
			}
			if len(ms.DiffIgnorePaths) > 0 {
				var paths []string
				if err := json.Unmarshal(ms.DiffIgnorePaths, &paths); err == nil && len(paths) > 0 {
					svcIgnoresMap[msName] = append(svcIgnoresMap[msName], paths...)
				}
			}
		}
	}

	if len(activeSchemas) == 0 {
		r.compiled.Store(&CompiledRegistry{
			Files:          nil,
			Operations:     opsMap,
			ServiceAliases: serviceAliases,
			ServiceIgnores: svcIgnoresMap,
		})
		return nil
	}

	// 跨集依赖联合编译：构建 deduplicated filesMap
	// 严格规则：每个 Schema 仅向联合池贡献其自有 ProtoFiles 清单中的文件
	// 优先级：BASE_LIB > 其他 Schema
	filesMap := make(map[string]*descriptorpb.FileDescriptorProto)

	// 先放非 BASE_LIB 的自有文件
	for _, s := range activeSchemas {
		if s.Category == models.ProtoCategoryBaseLib || len(s.DescriptorBytes) == 0 {
			continue
		}
		var ownNames []string
		if len(s.ProtoFiles) > 0 {
			_ = json.Unmarshal(s.ProtoFiles, &ownNames)
		}
		ownSet := make(map[string]struct{}, len(ownNames))
		for _, on := range ownNames {
			ownSet[on] = struct{}{}
		}

		var fds descriptorpb.FileDescriptorSet
		if err := proto.Unmarshal(s.DescriptorBytes, &fds); err == nil {
			for _, f := range fds.File {
				if _, isOwn := ownSet[f.GetName()]; isOwn || len(ownSet) == 0 {
					filesMap[f.GetName()] = f
				}
			}
		}
	}
	// 再覆盖 BASE_LIB 的自有文件（高优先级）
	for _, s := range activeSchemas {
		if s.Category != models.ProtoCategoryBaseLib || len(s.DescriptorBytes) == 0 {
			continue
		}
		var ownNames []string
		if len(s.ProtoFiles) > 0 {
			_ = json.Unmarshal(s.ProtoFiles, &ownNames)
		}
		ownSet := make(map[string]struct{}, len(ownNames))
		for _, on := range ownNames {
			ownSet[on] = struct{}{}
		}

		var fds descriptorpb.FileDescriptorSet
		if err := proto.Unmarshal(s.DescriptorBytes, &fds); err == nil {
			for _, f := range fds.File {
				if _, isOwn := ownSet[f.GetName()]; isOwn || len(ownSet) == 0 {
					filesMap[f.GetName()] = f
				}
			}
		}
	}

	sortedFiles, _, err := topologicalSortFiles(filesMap)
	if err != nil {
		// 若拓扑排序失败，存储空或部分快照
		r.compiled.Store(&CompiledRegistry{Files: nil, Operations: opsMap})
		return err
	}

	unifiedFiles, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{File: sortedFiles})
	if err != nil {
		r.compiled.Store(&CompiledRegistry{Files: nil, Operations: opsMap})
		return err
	}

	for _, s := range activeSchemas {
		var ops []models.ProtoOperation
		if err := r.db.Where("schema_id = ? AND is_active = true", s.ID).Find(&ops).Error; err != nil {
			continue
		}

		for _, op := range ops {
			key := op.ServiceName + ":" + op.Operation

			var reqDesc, rspDesc protoreflect.MessageDescriptor
			if op.ReqMessageFullName != "" {
				if d, err := unifiedFiles.FindDescriptorByName(protoreflect.FullName(op.ReqMessageFullName)); err == nil {
					if md, ok := d.(protoreflect.MessageDescriptor); ok {
						reqDesc = md
					}
				}
			}
			if op.RspMessageFullName != "" {
				if d, err := unifiedFiles.FindDescriptorByName(protoreflect.FullName(op.RspMessageFullName)); err == nil {
					if md, ok := d.(protoreflect.MessageDescriptor); ok {
						rspDesc = md
					}
				}
			}

			var ignorePaths []string
			if len(op.DiffIgnorePaths) > 0 {
				_ = json.Unmarshal(op.DiffIgnorePaths, &ignorePaths)
			}

			opsMap[key] = &CompiledOperation{
				SchemaID:           s.ID,
				SchemaName:         s.Name,
				SchemaVersion:      s.Version,
				ServiceName:        op.ServiceName,
				Operation:          op.Operation,
				ReqDesc:            reqDesc,
				RspDesc:            rspDesc,
				DiffIgnorePaths:    ignorePaths,
				ServiceIgnorePaths: svcIgnoresMap[op.ServiceName],
			}
		}
	}

	r.compiled.Store(&CompiledRegistry{
		Files:          unifiedFiles,
		Operations:     opsMap,
		ServiceAliases: serviceAliases,
		ServiceIgnores: svcIgnoresMap,
	})
	return nil
}

// CompileTrialPool 针对候选生效池 S_next 执行干跑预编译门禁 (冲突检测 + 依赖闭包自洽性验证)
func (r *Registry) CompileTrialPool(sNext []models.ProtoSchema, sNextOps map[uint64][]models.ProtoOperation) error {
	// 1. 全局方法 (service_name, operation) 唯一性仲裁
	type opEntry struct {
		schemaID   uint64
		schemaName string
		version    int
	}
	opOwners := make(map[string]opEntry)
	var conflicts []OperationConflictItem

	for _, s := range sNext {
		ops, ok := sNextOps[s.ID]
		if !ok {
			var dbOps []models.ProtoOperation
			_ = r.db.Where("schema_id = ?", s.ID).Find(&dbOps).Error
			ops = dbOps
		}

		for _, op := range ops {
			key := op.ServiceName + ":" + op.Operation
			if existing, exists := opOwners[key]; exists {
				conflicts = append(conflicts, OperationConflictItem{
					ServiceName:        op.ServiceName,
					Operation:          op.Operation,
					ConflictSchemaID:   existing.schemaID,
					ConflictSchemaName: existing.schemaName,
					ConflictVersion:    existing.version,
				})
			} else {
				opOwners[key] = opEntry{
					schemaID:   s.ID,
					schemaName: s.Name,
					version:    s.Version,
				}
			}
		}
	}

	if len(conflicts) > 0 {
		return &SchemaConflictError{Conflicts: conflicts}
	}

	// 2. 仅凭 S_next ∪ WKT 执行联合 protodesc.NewFiles 并核验全部 Req/Rsp 可解析
	// 严格规则：每个 Schema 仅向候选池贡献其自有 ProtoFiles 清单中的文件
	filesMap := make(map[string]*descriptorpb.FileDescriptorProto)
	// 普通业务包自有文件
	for _, s := range sNext {
		if s.Category == models.ProtoCategoryBaseLib || len(s.DescriptorBytes) == 0 {
			continue
		}
		var ownNames []string
		if len(s.ProtoFiles) > 0 {
			_ = json.Unmarshal(s.ProtoFiles, &ownNames)
		}
		ownSet := make(map[string]struct{}, len(ownNames))
		for _, on := range ownNames {
			ownSet[on] = struct{}{}
		}

		var fds descriptorpb.FileDescriptorSet
		if err := proto.Unmarshal(s.DescriptorBytes, &fds); err == nil {
			for _, f := range fds.File {
				if _, isOwn := ownSet[f.GetName()]; isOwn || len(ownSet) == 0 {
					filesMap[f.GetName()] = f
				}
			}
		}
	}
	// BASE_LIB 包自有文件高优覆盖
	for _, s := range sNext {
		if s.Category != models.ProtoCategoryBaseLib || len(s.DescriptorBytes) == 0 {
			continue
		}
		var ownNames []string
		if len(s.ProtoFiles) > 0 {
			_ = json.Unmarshal(s.ProtoFiles, &ownNames)
		}
		ownSet := make(map[string]struct{}, len(ownNames))
		for _, on := range ownNames {
			ownSet[on] = struct{}{}
		}

		var fds descriptorpb.FileDescriptorSet
		if err := proto.Unmarshal(s.DescriptorBytes, &fds); err == nil {
			for _, f := range fds.File {
				if _, isOwn := ownSet[f.GetName()]; isOwn || len(ownSet) == 0 {
					filesMap[f.GetName()] = f
				}
			}
		}
	}

	sortedFiles, missingImports, err := topologicalSortFiles(filesMap)
	if err != nil {
		var broken []BrokenDependentItemDTO
		for _, s := range sNext {
			var exts []string
			if len(s.ExternalImports) > 0 {
				_ = json.Unmarshal(s.ExternalImports, &exts)
			}
			var missingForThis []string
			for _, mi := range missingImports {
				for _, ext := range exts {
					if ext == mi {
						missingForThis = append(missingForThis, mi)
					}
				}
			}
			if len(missingForThis) > 0 {
				broken = append(broken, BrokenDependentItemDTO{
					SchemaID:      s.ID,
					SchemaName:    s.Name,
					Version:       s.Version,
					MissingProtos: missingForThis,
					Reason:        fmt.Sprintf("缺失外部导入文件: %v", missingForThis),
				})
			}
		}
		if len(broken) == 0 && len(sNext) > 0 {
			broken = append(broken, BrokenDependentItemDTO{
				SchemaID:      sNext[0].ID,
				SchemaName:    sNext[0].Name,
				Version:       sNext[0].Version,
				MissingProtos: missingImports,
				Reason:        fmt.Sprintf("全局生效池依赖断裂: %v", missingImports),
			})
		}
		return &SchemaBrokenDependentsError{BrokenDependents: broken}
	}

	files, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{File: sortedFiles})
	if err != nil {
		return &SchemaBrokenDependentsError{BrokenDependents: []BrokenDependentItemDTO{{
			Reason: fmt.Sprintf("联合编译失败: %v", err),
		}}}
	}

	// 校验全部方法请求/应答 Message 可解析
	var brokenResolutions []BrokenDependentItemDTO
	for _, s := range sNext {
		ops, ok := sNextOps[s.ID]
		if !ok {
			var dbOps []models.ProtoOperation
			_ = r.db.Where("schema_id = ?", s.ID).Find(&dbOps).Error
			ops = dbOps
		}

		for _, op := range ops {
			if op.ReqMessageFullName != "" {
				if d, err := files.FindDescriptorByName(protoreflect.FullName(op.ReqMessageFullName)); err != nil || d == nil {
					brokenResolutions = append(brokenResolutions, BrokenDependentItemDTO{
						SchemaID:   s.ID,
						SchemaName: s.Name,
						Version:    s.Version,
						Reason:     fmt.Sprintf("无法解析请求消息 [%s]", op.ReqMessageFullName),
					})
					break
				}
			}
			if op.RspMessageFullName != "" {
				if d, err := files.FindDescriptorByName(protoreflect.FullName(op.RspMessageFullName)); err != nil || d == nil {
					brokenResolutions = append(brokenResolutions, BrokenDependentItemDTO{
						SchemaID:   s.ID,
						SchemaName: s.Name,
						Version:    s.Version,
						Reason:     fmt.Sprintf("无法解析应答消息 [%s]", op.RspMessageFullName),
					})
					break
				}
			}
		}
	}

	if len(brokenResolutions) > 0 {
		return &SchemaBrokenDependentsError{BrokenDependents: brokenResolutions}
	}

	return nil
}

// fetchBaseLibProtoFiles 获取已知 BASE_LIB 的所有 proto 文件列表
func (r *Registry) fetchBaseLibProtoFiles(tx *gorm.DB, activeOnly bool) []string {
	var schemas []models.ProtoSchema
	q := tx.Model(&models.ProtoSchema{}).Where("category = ?", models.ProtoCategoryBaseLib)
	if activeOnly {
		q = q.Where("is_active = true")
	}
	_ = q.Find(&schemas).Error

	var res []string
	for _, s := range schemas {
		if len(s.ProtoFiles) > 0 {
			var files []string
			if err := json.Unmarshal(s.ProtoFiles, &files); err == nil {
				res = append(res, files...)
			}
		}
	}
	return res
}

// UploadSchema 上传并持久化 .pb 描述集
func (r *Registry) UploadSchema(in UploadSchemaInput) (*models.ProtoSchema, []models.ProtoOperation, int, error) {
	name := strings.TrimSpace(in.Name)
	runeCount := utf8.RuneCountInString(name)
	if runeCount == 0 || runeCount > 128 {
		return nil, nil, 0, ErrInvalidSchemaName
	}
	if len(in.RawBytes) == 0 {
		return nil, nil, 0, ErrDescriptorEmpty
	}
	if len(in.RawBytes) > MaxDescriptorFileSize {
		return nil, nil, 0, ErrDescriptorTooLarge
	}

	category := in.Category
	if category == "" {
		category = models.ProtoCategoryBusiness
	}
	if category != models.ProtoCategoryBaseLib && category != models.ProtoCategoryBusiness && category != models.ProtoCategoryGateway {
		return nil, nil, 0, errors.New("非法的 category 分类，仅支持 BASE_LIB, BUSINESS, GATEWAY")
	}

	cleanFileName := filepath.Base(in.FileName)
	if cleanFileName == "" || cleanFileName == "." {
		cleanFileName = name + ".pb"
	}

	shaBytes := sha256.Sum256(in.RawBytes)
	fileSHA256 := hex.EncodeToString(shaBytes[:])

	var schema models.ProtoSchema
	var insertedOps []models.ProtoOperation
	inheritedOpCount := 0

	err := r.db.Transaction(func(tx *gorm.DB) error {
		// 获取 PostgreSQL 事务级咨询锁防并发竞争
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext('traefik_proto_schema_registry'))").Error; err != nil {
			return fmt.Errorf("获取描述集事务咨询锁失败: %w", err)
		}

		// 收集可用 BASE_LIB proto files
		existingBaseProtos := r.fetchBaseLibProtoFiles(tx, in.AutoActivate)
		if !in.AutoActivate {
			// 草稿模式允许借用未激活的最新 BASE_LIB
			existingBaseProtos = r.fetchBaseLibProtoFiles(tx, false)
		}

		_, ownFiles, ops, protoFiles, externalImports, err := extractOperationsFromDescriptor(in.RawBytes, category, existingBaseProtos)
		if err != nil {
			return err
		}

		// 计算同名自增版本号
		var maxVer int
		_ = tx.Model(&models.ProtoSchema{}).Where("name = ?", name).Select("COALESCE(MAX(version), 0)").Scan(&maxVer).Error
		nextVer := maxVer + 1

		// 静态合法性/外部依赖闭包预检
		filesMap := make(map[string]*descriptorpb.FileDescriptorProto)
		for _, f := range ownFiles {
			filesMap[f.GetName()] = f
		}

		// 仅从可用描述集中寻找当前 Schema 所需的外部依赖 (externalImports)
		needed := make(map[string]struct{}, len(externalImports))
		for _, ext := range externalImports {
			if !strings.HasPrefix(ext, "google/protobuf/") {
				needed[ext] = struct{}{}
			}
		}

		if len(needed) > 0 {
			var availSchemas []models.ProtoSchema
			q := tx.Model(&models.ProtoSchema{}).Where("is_active = true")
			if !in.AutoActivate {
				q = tx.Model(&models.ProtoSchema{}).Where("is_active = true OR category = ?", models.ProtoCategoryBaseLib)
			}
			_ = q.Find(&availSchemas).Error

			for _, as := range availSchemas {
				if len(as.DescriptorBytes) == 0 {
					continue
				}
				var fds descriptorpb.FileDescriptorSet
				if err := proto.Unmarshal(as.DescriptorBytes, &fds); err == nil {
					for _, f := range fds.File {
						if _, isNeeded := needed[f.GetName()]; isNeeded {
							if _, ok := filesMap[f.GetName()]; !ok {
								filesMap[f.GetName()] = f
								for _, dep := range f.Dependency {
									if _, inMap := filesMap[dep]; !inMap && !strings.HasPrefix(dep, "google/protobuf/") {
										needed[dep] = struct{}{}
									}
								}
							}
						}
					}
				}
			}
		}

		sortedFiles, missingImports, err := topologicalSortFiles(filesMap)
		if err != nil {
			return &SchemaBrokenDependentsError{BrokenDependents: []BrokenDependentItemDTO{{
				SchemaName:    name,
				Version:       nextVer,
				MissingProtos: missingImports,
				Reason:        fmt.Sprintf("缺失外部依赖: %v", missingImports),
			}}}
		}
		if _, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{File: sortedFiles}); err != nil {
			return fmt.Errorf("静态构建描述集类型库失败: %w", err)
		}

		// 同名新版本 DiffIgnorePaths 前向自动继承
		prevIgnoreMap := make(map[string]datatypes.JSON)
		if nextVer > 1 {
			var prevSchema models.ProtoSchema
			// 优先读取同名已生效版本，否则取最大历史版本
			err := tx.Where("name = ? AND is_active = true", name).First(&prevSchema).Error
			if err != nil {
				_ = tx.Where("name = ? AND version < ?", name, nextVer).Order("version DESC").First(&prevSchema).Error
			}
			if prevSchema.ID > 0 {
				var prevOps []models.ProtoOperation
				_ = tx.Where("schema_id = ?", prevSchema.ID).Find(&prevOps).Error
				for _, pop := range prevOps {
					if len(pop.DiffIgnorePaths) > 0 && string(pop.DiffIgnorePaths) != "[]" {
						prevIgnoreMap[pop.ServiceName+":"+pop.Operation] = pop.DiffIgnorePaths
					}
				}
			}
		}

		protoFilesBytes, _ := json.Marshal(protoFiles)
		extImportsBytes, _ := json.Marshal(externalImports)

		// 计算导出的 ServiceCount
		svcSet := make(map[string]struct{})
		for _, op := range ops {
			svcSet[op.ServiceName] = struct{}{}
		}

		schema = models.ProtoSchema{
			Name:            name,
			Version:         nextVer,
			VersionTag:      in.VersionTag,
			Category:        category,
			FileName:        cleanFileName,
			FileSHA256:      fileSHA256,
			FileSize:        len(in.RawBytes),
			DescriptorBytes: in.RawBytes,
			IsActive:        false,
			ProtoFileCount:  len(protoFiles),
			ServiceCount:    len(svcSet),
			OperationCount:  len(ops),
			ProtoFiles:      datatypes.JSON(protoFilesBytes),
			ExternalImports: datatypes.JSON(extImportsBytes),
			Description:     in.Description,
			CreatedBy:       in.OperatorID,
		}
		if err := tx.Create(&schema).Error; err != nil {
			return err
		}

		insertedOps = make([]models.ProtoOperation, len(ops))
		for i, op := range ops {
			op.SchemaID = schema.ID
			op.IsActive = false
			key := op.ServiceName + ":" + op.Operation
			if inherited, ok := prevIgnoreMap[key]; ok {
				op.DiffIgnorePaths = inherited
				inheritedOpCount++
			}
			if err := tx.Create(&op).Error; err != nil {
				return err
			}
			insertedOps[i] = op
		}

		// 若指定自动激活，在此事务中先执行同名版本破坏性变更检测门禁，再执行 CompileTrialPool 预演
		if in.AutoActivate {
			var activeSchema models.ProtoSchema
			if err := tx.Where("name = ? AND is_active = true AND id != ?", name, schema.ID).First(&activeSchema).Error; err == nil {
				activeOwn, err := ExtractOwnFilesFromSchema(&activeSchema)
				if err == nil {
					violations := DetectBreakingChanges(activeOwn, ownFiles)
					if len(violations) > 0 {
						if !in.AllowBreaking {
							affected := FindAffectedDependents(tx, violations)
							return &SchemaBreakingError{
								BreakingRules:      violations,
								AffectedDependents: affected,
							}
						}
						reason := strings.TrimSpace(in.BreakingReason)
						if len(reason) < 8 {
							return errors.New("强制放行破坏性变更必须填写不少于 8 个字符的原因 (breaking_reason)")
						}
					}
				}
			}

			var activeSchemas []models.ProtoSchema
			_ = tx.Where("is_active = true AND name != ?", name).Find(&activeSchemas).Error
			sNext := append(activeSchemas, schema)

			sNextOps := make(map[uint64][]models.ProtoOperation)
			sNextOps[schema.ID] = insertedOps

			if err := r.CompileTrialPool(sNext, sNextOps); err != nil {
				return err
			}

			// 停用同名旧版本
			_ = tx.Model(&models.ProtoSchema{}).Where("name = ? AND id != ?", name, schema.ID).
				Updates(map[string]interface{}{"is_active": false}).Error
			_ = tx.Model(&models.ProtoOperation{}).
				Where("schema_id IN (SELECT id FROM traefik_proto_schemas WHERE name = ? AND id != ?)", name, schema.ID).
				Updates(map[string]interface{}{"is_active": false}).Error

			if err := tx.Model(&models.ProtoSchema{}).Where("id = ?", schema.ID).
				Updates(map[string]interface{}{"is_active": true}).Error; err != nil {
				return err
			}
			if err := tx.Model(&models.ProtoOperation{}).Where("schema_id = ?", schema.ID).
				Updates(map[string]interface{}{"is_active": true}).Error; err != nil {
				return err
			}

			schema.IsActive = true
			for i := range insertedOps {
				insertedOps[i].IsActive = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, 0, err
	}

	if in.AutoActivate {
		_ = r.Reload()
	}

	return &schema, insertedOps, inheritedOpCount, nil
}

// BatchActivateOptions 批量或单描述集激活控制参数
type BatchActivateOptions struct {
	AllowBreaking  bool
	BreakingReason string
}

// ActivateSchemaVersion 切换单个描述集为生效态
func (r *Registry) ActivateSchemaVersion(schemaID uint64, opts ...BatchActivateOptions) (*models.ProtoSchema, *models.ProtoSchema, int, error) {
	schemas, count, err := r.BatchActivate([]uint64{schemaID}, opts...)
	if err != nil {
		return nil, nil, 0, err
	}
	if len(schemas) == 0 {
		return nil, nil, 0, ErrSchemaNotFound
	}
	return nil, &schemas[0], count, nil
}

// BatchActivate 批量原子激活多个描述集 (单事务咨询锁 + CompileTrialPool 预编译仲裁)
func (r *Registry) BatchActivate(schemaIDs []uint64, opts ...BatchActivateOptions) ([]models.ProtoSchema, int, error) {
	if len(schemaIDs) == 0 {
		return nil, 0, errors.New("待激活描述集列表不能为空")
	}
	if len(schemaIDs) > 100 {
		return nil, 0, errors.New("单次批量激活不能超过 100 个描述集")
	}

	var allowBreaking bool
	var breakingReason string
	if len(opts) > 0 {
		allowBreaking = opts[0].AllowBreaking
		breakingReason = opts[0].BreakingReason
	}

	var activatedSchemas []models.ProtoSchema
	var totalActiveOps int

	err := r.db.Transaction(func(tx *gorm.DB) error {
		// 咨询锁防并发竞争
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext('traefik_proto_schema_registry'))").Error; err != nil {
			return fmt.Errorf("获取描述集事务咨询锁失败: %w", err)
		}

		var targets []models.ProtoSchema
		if err := tx.Where("id IN ?", schemaIDs).Find(&targets).Error; err != nil {
			return err
		}
		if len(targets) != len(schemaIDs) {
			return ErrSchemaNotFound
		}

		// 检查同批次是否包含同名多版本
		targetNamesMap := make(map[string]uint64)
		for _, t := range targets {
			if existingID, exists := targetNamesMap[t.Name]; exists && existingID != t.ID {
				return errors.New("同一批次激活请求中不能包含同名描述集的不同版本")
			}
			targetNamesMap[t.Name] = t.ID
		}

		// 检查候选版本与同名当前生效版本的 AST 破坏性变更 (F-26 / M16)
		var allViolations []BreakingRuleViolation
		for _, t := range targets {
			var active models.ProtoSchema
			if err := tx.Where("name = ? AND is_active = true AND id != ?", t.Name, t.ID).First(&active).Error; err == nil {
				activeOwn, err := ExtractOwnFilesFromSchema(&active)
				if err == nil {
					targetOwn, err := ExtractOwnFilesFromSchema(&t)
					if err == nil {
						v := DetectBreakingChanges(activeOwn, targetOwn)
						allViolations = append(allViolations, v...)
					}
				}
			}
		}

		if len(allViolations) > 0 {
			if !allowBreaking {
				affected := FindAffectedDependents(tx, allViolations)
				return &SchemaBreakingError{
					BreakingRules:      allViolations,
					AffectedDependents: affected,
				}
			}
			reason := strings.TrimSpace(breakingReason)
			if len(reason) < 8 {
				return errors.New("强制放行破坏性变更必须填写不少于 8 个字符的原因 (breaking_reason)")
			}
		}

		// 构造 S_next = 当前生效池中不包含 targets 同名项目的 Schema ∪ targets
		var currentActive []models.ProtoSchema
		_ = tx.Where("is_active = true").Find(&currentActive).Error

		sNextMap := make(map[string]models.ProtoSchema)
		for _, ca := range currentActive {
			if _, replaced := targetNamesMap[ca.Name]; !replaced {
				sNextMap[ca.Name] = ca
			}
		}
		targetIDSet := make(map[uint64]bool, len(targets))
		for _, t := range targets {
			targetIDSet[t.ID] = true
			t.IsActive = true
			sNextMap[t.Name] = t
		}

		// 确定性排序 sNext：已有生效契约在前、待激活候选契约在后，同组按 ID 升序，消除 Go map 随机遍历导致的冲突归属抖动
		sNext := make([]models.ProtoSchema, 0, len(sNextMap))
		for _, s := range sNextMap {
			sNext = append(sNext, s)
		}
		sort.Slice(sNext, func(i, j int) bool {
			iTarget := targetIDSet[sNext[i].ID]
			jTarget := targetIDSet[sNext[j].ID]
			if iTarget != jTarget {
				return !iTarget
			}
			return sNext[i].ID < sNext[j].ID
		})

		sNextOps := make(map[uint64][]models.ProtoOperation)
		for _, s := range sNext {
			var ops []models.ProtoOperation
			_ = tx.Where("schema_id = ?", s.ID).Find(&ops).Error
			sNextOps[s.ID] = ops
		}

		// 执行试编译演练门禁
		if err := r.CompileTrialPool(sNext, sNextOps); err != nil {
			return err
		}

		// 停用所有被替换的同名旧版本
		targetNames := make([]string, 0, len(targetNamesMap))
		for name := range targetNamesMap {
			targetNames = append(targetNames, name)
		}

		_ = tx.Model(&models.ProtoSchema{}).
			Where("name IN ? AND id NOT IN ?", targetNames, schemaIDs).
			Updates(map[string]interface{}{"is_active": false}).Error
		_ = tx.Model(&models.ProtoOperation{}).
			Where("schema_id IN (SELECT id FROM traefik_proto_schemas WHERE name IN ? AND id NOT IN ?)", targetNames, schemaIDs).
			Updates(map[string]interface{}{"is_active": false}).Error

		// 激活 targets
		if err := tx.Model(&models.ProtoSchema{}).Where("id IN ?", schemaIDs).
			Updates(map[string]interface{}{"is_active": true}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.ProtoOperation{}).Where("schema_id IN ?", schemaIDs).
			Updates(map[string]interface{}{"is_active": true}).Error; err != nil {
			return err
		}

		activatedSchemas = targets
		for i := range activatedSchemas {
			activatedSchemas[i].IsActive = true
		}

		for _, s := range sNext {
			totalActiveOps += len(sNextOps[s.ID])
		}

		return nil
	})
	if err != nil {
		return nil, 0, err
	}

	_ = r.Reload()
	return activatedSchemas, totalActiveOps, nil
}

// DeactivateSchemaVersion 停用指定已生效描述集 (带反向依赖 CompileTrialPool 保护)
func (r *Registry) DeactivateSchemaVersion(schemaID uint64) (*models.ProtoSchema, int, error) {
	var target models.ProtoSchema
	var deactivatedOps int

	err := r.db.Transaction(func(tx *gorm.DB) error {
		// 咨询锁防并发竞争
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext('traefik_proto_schema_registry'))").Error; err != nil {
			return fmt.Errorf("获取描述集事务咨询锁失败: %w", err)
		}

		if err := tx.First(&target, schemaID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSchemaNotFound
			}
			return err
		}
		if !target.IsActive {
			return errors.New("该描述集版本本来未生效")
		}

		// 构造 S_next = 当前所有生效 Schema 剔除 target
		var currentActive []models.ProtoSchema
		_ = tx.Where("is_active = true AND id != ?", schemaID).Find(&currentActive).Error

		sNextOps := make(map[uint64][]models.ProtoOperation)
		for _, s := range currentActive {
			var ops []models.ProtoOperation
			_ = tx.Where("schema_id = ?", s.ID).Find(&ops).Error
			sNextOps[s.ID] = ops
		}

		// 对 S_next 进行预编译校验反向依赖
		if err := r.CompileTrialPool(currentActive, sNextOps); err != nil {
			return err
		}

		if err := tx.Model(&models.ProtoSchema{}).Where("id = ?", schemaID).
			Updates(map[string]interface{}{"is_active": false}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.ProtoOperation{}).Where("schema_id = ?", schemaID).
			Updates(map[string]interface{}{"is_active": false}).Error; err != nil {
			return err
		}

		var count int64
		_ = tx.Model(&models.ProtoOperation{}).Where("schema_id = ?", schemaID).Count(&count).Error
		deactivatedOps = int(count)

		target.IsActive = false
		return nil
	})
	if err != nil {
		return nil, 0, err
	}

	_ = r.Reload()
	return &target, deactivatedOps, nil
}

// DeleteSchemaVersion 删除未生效的描述集版本
func (r *Registry) DeleteSchemaVersion(schemaID uint64) (*models.ProtoSchema, error) {
	var schema models.ProtoSchema
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext('traefik_proto_schema_registry'))").Error; err != nil {
			return fmt.Errorf("获取描述集事务咨询锁失败: %w", err)
		}

		if err := tx.First(&schema, schemaID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSchemaNotFound
			}
			return err
		}

		if schema.IsActive {
			return ErrDeleteActiveSchema
		}

		if err := tx.Where("schema_id = ?", schemaID).Delete(&models.ProtoOperation{}).Error; err != nil {
			return err
		}
		return tx.Delete(&schema).Error
	})
	if err != nil {
		return nil, err
	}

	return &schema, nil
}

// DetectBreakingChangesAgainstActive 对比指定描述集名称的当前生效版本与候选描述集二进制，检出破坏性改动
func (r *Registry) DetectBreakingChangesAgainstActive(name string, candidateBytes []byte) ([]BreakingRuleViolation, error) {
	var active models.ProtoSchema
	err := r.db.Where("name = ? AND is_active = true", name).First(&active).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	activeOwn, err := ExtractOwnFilesFromSchema(&active)
	if err != nil {
		return nil, err
	}
	candidateSchema := &models.ProtoSchema{
		Name:            name,
		DescriptorBytes: candidateBytes,
	}
	candidateOwn, err := ExtractOwnFilesFromSchema(candidateSchema)
	if err != nil {
		return nil, err
	}
	return DetectBreakingChanges(activeOwn, candidateOwn), nil
}

// LookupOperation 检索已编译生效的接口元数据（支持自定义运行期服务名通过 LIB 契约别名映射到唯一 .pb 描述符）
func (r *Registry) LookupOperation(serviceName, operation string) *CompiledOperation {
	compiled := r.compiled.Load()
	if compiled == nil {
		return nil
	}
	if op, ok := compiled.Operations[serviceName+":"+operation]; ok {
		return op
	}
	if aliasTarget, ok := compiled.ServiceAliases[serviceName]; ok {
		if op, ok2 := compiled.Operations[aliasTarget+":"+operation]; ok2 {
			// 返回克隆副本以绑定当前运行期服务名专属的 DiffIgnorePaths，不污染契约模板快照
			cloned := *op
			cloned.ServiceName = serviceName
			cloned.ServiceIgnorePaths = compiled.ServiceIgnores[serviceName]
			return &cloned
		}
	}
	return nil
}

// DecodePayloadToJSON 动态反序列化单条载荷并转换为规范 JSON
func (r *Registry) DecodePayloadToJSON(serviceName, operation string, msgType uint8, payload []byte) DecodeResult {
	op := r.LookupOperation(serviceName, operation)
	if op == nil {
		return DecodeResult{
			DecodeStatus: DecodeStatusNoSchemaRawHex,
			RawHex:       hex.EncodeToString(payload),
		}
	}

	var desc protoreflect.MessageDescriptor
	switch msgType {
	case crosrpc.MsgTypeReq:
		desc = op.ReqDesc
	case crosrpc.MsgTypeRsp, crosrpc.MsgTypeRspSeg:
		desc = op.RspDesc
	default:
		if op.ReqDesc != nil {
			desc = op.ReqDesc
		} else {
			desc = op.RspDesc
		}
	}

	if desc == nil {
		return DecodeResult{
			DecodeStatus:  DecodeStatusNoSchemaRawHex,
			SchemaID:      &op.SchemaID,
			SchemaVersion: &op.SchemaVersion,
			SchemaName:    op.SchemaName,
			RawHex:        hex.EncodeToString(payload),
		}
	}

	msg := dynamicpb.NewMessage(desc)
	if err := proto.Unmarshal(payload, msg); err != nil {
		return DecodeResult{
			DecodeStatus:    DecodeStatusDecodeErrRawHex,
			SchemaID:        &op.SchemaID,
			SchemaVersion:   &op.SchemaVersion,
			SchemaName:      op.SchemaName,
			MessageFullName: string(desc.FullName()),
			RawHex:          hex.EncodeToString(payload),
		}
	}

	opts := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: false,
	}
	jsonBytes, err := opts.Marshal(msg)
	if err != nil {
		return DecodeResult{
			DecodeStatus:    DecodeStatusDecodeErrRawHex,
			SchemaID:        &op.SchemaID,
			SchemaVersion:   &op.SchemaVersion,
			SchemaName:      op.SchemaName,
			MessageFullName: string(desc.FullName()),
			RawHex:          hex.EncodeToString(payload),
		}
	}

	return DecodeResult{
		DecodeStatus:    DecodeStatusDecodedJSON,
		SchemaID:        &op.SchemaID,
		SchemaVersion:   &op.SchemaVersion,
		SchemaName:      op.SchemaName,
		MessageFullName: string(desc.FullName()),
		JSONBytes:       jsonBytes,
	}
}

// DecodeCrosFrame 解析单帧 CROS RPC 字节流并动态解码 Payload
func (r *Registry) DecodeCrosFrame(rawFrame []byte, isTruncated bool) (*DecodedFrameResult, error) {
	parsed, err := crosrpc.ParsePacket(rawFrame)
	if err != nil {
		return nil, err
	}

	res := &DecodedFrameResult{
		CommHead:    parsed.Comm,
		ReqHead:     parsed.Req,
		RspHead:     parsed.Rsp,
		MngHead:     parsed.Mng,
		ExtendHeads: parsed.ExtendHeads,
	}

	if isTruncated || parsed.IsTruncated {
		res.Decode = DecodeResult{
			DecodeStatus: DecodeStatusTruncated,
			RawHex:       hex.EncodeToString(rawFrame),
		}
		return res, nil
	}

	if parsed.Mng != nil {
		res.Decode = DecodeResult{
			DecodeStatus: DecodeStatusMngRaw,
			RawHex:       hex.EncodeToString(rawFrame),
		}
		return res, nil
	}

	if parsed.Req != nil {
		res.ServiceName = parsed.Req.ServiceName
		res.Operation = parsed.Req.Operation
	} else if parsed.Rsp != nil {
		res.ServiceName = parsed.Rsp.ServiceName
		res.Operation = parsed.Rsp.Operation
	}

	if parsed.PayloadOffset < len(rawFrame) {
		payloadEnd := parsed.PayloadOffset + parsed.PayloadLen
		if payloadEnd > len(rawFrame) {
			payloadEnd = len(rawFrame)
		}
		res.Payload = rawFrame[parsed.PayloadOffset:payloadEnd]
	}

	res.Decode = r.DecodePayloadToJSON(res.ServiceName, res.Operation, parsed.Comm.MsgType, res.Payload)
	return res, nil
}

// GetOperationSchemaTree 根据方法 ID 反射提取请求与应答字段字典树 (支持生效与未生效草稿包闭包解析)
func (r *Registry) GetOperationSchemaTree(opID uint64) (*OperationSchemaTreeDTO, error) {
	var op models.ProtoOperation
	if err := r.db.First(&op, opID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("方法不存在")
		}
		return nil, err
	}

	var schema models.ProtoSchema
	if err := r.db.First(&schema, op.SchemaID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSchemaNotFound
		}
		return nil, err
	}

	if len(schema.DescriptorBytes) == 0 {
		return nil, errors.New("描述集二进制为空")
	}

	var files *protoregistry.Files
	compiled := r.compiled.Load()

	// 若该描述集正处于生效态，且已编译注册表非空，直接复用 CompiledRegistry.Files
	if schema.IsActive && compiled != nil && compiled.Files != nil {
		files = compiled.Files
	} else {
		// 对未生效历史/草稿包，合并当前生效池（及最新 BASE_LIB）构建临时闭包
		filesMap := make(map[string]*descriptorpb.FileDescriptorProto)

		// 1. 载入当前所有生效包
		var activeSchemas []models.ProtoSchema
		_ = r.db.Where("is_active = true").Find(&activeSchemas).Error
		for _, s := range activeSchemas {
			if len(s.DescriptorBytes) > 0 {
				var fds descriptorpb.FileDescriptorSet
				if err := proto.Unmarshal(s.DescriptorBytes, &fds); err == nil {
					for _, f := range fds.File {
						filesMap[f.GetName()] = f
					}
				}
			}
		}

		// 2. 载入最新 BASE_LIB（即使未激活）
		var baseSchemas []models.ProtoSchema
		_ = r.db.Where("category = ?", models.ProtoCategoryBaseLib).Order("version DESC").Find(&baseSchemas).Error
		for _, bs := range baseSchemas {
			if len(bs.DescriptorBytes) > 0 {
				var fds descriptorpb.FileDescriptorSet
				if err := proto.Unmarshal(bs.DescriptorBytes, &fds); err == nil {
					for _, f := range fds.File {
						filesMap[f.GetName()] = f
					}
				}
			}
		}

		// 3. 载入目标未生效 Schema 自有文件（覆盖同名）
		var targetFds descriptorpb.FileDescriptorSet
		if err := proto.Unmarshal(schema.DescriptorBytes, &targetFds); err != nil {
			return nil, fmt.Errorf("解析描述集失败: %w", err)
		}
		for _, f := range targetFds.File {
			filesMap[f.GetName()] = f
		}

		sortedFiles, _, err := topologicalSortFiles(filesMap)
		if err != nil {
			return nil, fmt.Errorf("临时构建依赖闭包拓扑排序失败: %w", err)
		}

		tempFiles, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{File: sortedFiles})
		if err != nil {
			return nil, fmt.Errorf("构建反射类型库失败: %w", err)
		}
		files = tempFiles
	}

	var opIgnores []string
	if len(op.DiffIgnorePaths) > 0 {
		_ = json.Unmarshal(op.DiffIgnorePaths, &opIgnores)
	}
	if opIgnores == nil {
		opIgnores = []string{}
	}

	var inheritedIgnores []string
	var svc models.ModuleService
	if err := r.db.Where("service_name = ?", op.ServiceName).First(&svc).Error; err == nil {
		if len(svc.DiffIgnorePaths) > 0 {
			_ = json.Unmarshal(svc.DiffIgnorePaths, &inheritedIgnores)
		}
	}
	if inheritedIgnores == nil {
		inheritedIgnores = []string{}
	}

	allIgnores := append(append([]string{}, opIgnores...), inheritedIgnores...)

	dto := &OperationSchemaTreeDTO{
		OperationID:          fmt.Sprintf("%d", op.ID),
		OperationIDSnake:     fmt.Sprintf("%d", op.ID),
		ServiceName:          op.ServiceName,
		ServiceNameSnake:     op.ServiceName,
		Operation:            op.Operation,
		ProtoFile:            op.ProtoFile,
		ProtoFileSnake:       op.ProtoFile,
		ClientStreaming:      op.ClientStreaming,
		ServerStreaming:      op.ServerStreaming,
		DiffIgnorePaths:      opIgnores,
		DiffIgnoreSnake:      opIgnores,
		InheritedIgnorePaths: inheritedIgnores,
		InheritedIgnoreSnake: inheritedIgnores,
	}

	if op.ReqMessageFullName != "" {
		if d, err := files.FindDescriptorByName(protoreflect.FullName(op.ReqMessageFullName)); err == nil && d != nil {
			if md, ok := d.(protoreflect.MessageDescriptor); ok {
				fields := ExtractMessageFieldTreeWithIgnores(md, allIgnores)
				subTree := &MessageSubTreeDTO{
					FullName: op.ReqMessageFullName,
					Fields:   fields,
				}
				dto.ReqMessage = subTree
				dto.ReqMessageSnake = subTree
			}
		}
	}

	if op.RspMessageFullName != "" {
		if d, err := files.FindDescriptorByName(protoreflect.FullName(op.RspMessageFullName)); err == nil && d != nil {
			if md, ok := d.(protoreflect.MessageDescriptor); ok {
				fields := ExtractMessageFieldTreeWithIgnores(md, allIgnores)
				subTree := &MessageSubTreeDTO{
					FullName: op.RspMessageFullName,
					Fields:   fields,
				}
				dto.RspMessage = subTree
				dto.RspMessageSnake = subTree
			}
		}
	}

	return dto, nil
}

// UpdateOperationIgnorePaths 更新单接口方法级专属回归对比忽略字段清单
func (r *Registry) UpdateOperationIgnorePaths(opID uint64, paths []string) (*models.ProtoOperation, *models.ProtoOperation, error) {
	for _, p := range paths {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			return nil, nil, errors.New("忽略路径不能包含空字符串")
		}
		if strings.ContainsAny(trimmed, "\n\r\t\x00") {
			return nil, nil, errors.New("忽略路径包含非法控制字符")
		}
	}

	var op models.ProtoOperation
	if err := r.db.First(&op, opID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, errors.New("方法不存在")
		}
		return nil, nil, err
	}

	beforeOp := op

	pathsJSON, err := json.Marshal(paths)
	if err != nil {
		return nil, nil, fmt.Errorf("序列化忽略路径失败: %w", err)
	}

	if err := r.db.Model(&models.ProtoOperation{}).Where("id = ?", opID).
		Update("diff_ignore_paths", datatypes.JSON(pathsJSON)).Error; err != nil {
		return nil, nil, err
	}

	op.DiffIgnorePaths = datatypes.JSON(pathsJSON)
	_ = r.Reload()

	return &beforeOp, &op, nil
}
