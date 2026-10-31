package decoder

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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
	"gorm.io/gorm"
)

const (
	MaxDescriptorFileSize = 4 * 1024 * 1024 // 4MB

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
	ErrDescriptorTooLarge      = errors.New("上传的 .pb 描述集文件超过 4MB 大小限制")
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

// CompiledOperation 编译后的单接口动态编解码元数据
type CompiledOperation struct {
	SchemaID        uint64
	SchemaName      string
	SchemaVersion   int
	ServiceName     string
	Operation       string
	ReqDesc         protoreflect.MessageDescriptor
	RspDesc         protoreflect.MessageDescriptor
	DiffIgnorePaths []string
}

// CompiledRegistry 不可变无锁内存注册表快照
type CompiledRegistry struct {
	Operations map[string]*CompiledOperation // key: serviceName + ":" + operation
}

// UploadSchemaInput 上传 .pb 描述集参数
type UploadSchemaInput struct {
	Name         string
	VersionTag   string
	FileName     string
	Description  string
	RawBytes     []byte
	AutoActivate bool
	OperatorID   uint
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
}

// NewRegistry 构造解码器实例并预热编译所有处于激活态的 Schema
func NewRegistry(db *gorm.DB) (*Registry, error) {
	r := &Registry{db: db}
	if err := r.Reload(); err != nil {
		return nil, err
	}
	return r, nil
}

// Reload 重载全量生效中的描述集并构建不可变 CompiledRegistry 快照
func (r *Registry) Reload() error {
	var activeSchemas []models.ProtoSchema
	if err := r.db.Where("is_active = true").Order("id ASC").Find(&activeSchemas).Error; err != nil {
		return err
	}

	opsMap := make(map[string]*CompiledOperation)

	for _, s := range activeSchemas {
		if len(s.DescriptorBytes) == 0 {
			continue
		}
		var fds descriptorpb.FileDescriptorSet
		if err := proto.Unmarshal(s.DescriptorBytes, &fds); err != nil {
			continue
		}
		files, err := protodesc.NewFiles(&fds)
		if err != nil {
			continue
		}

		var ops []models.ProtoOperation
		if err := r.db.Where("schema_id = ? AND is_active = true", s.ID).Find(&ops).Error; err != nil {
			continue
		}

		for _, op := range ops {
			key := op.ServiceName + ":" + op.Operation

			var reqDesc, rspDesc protoreflect.MessageDescriptor
			if op.ReqMessageFullName != "" {
				if d, err := files.FindDescriptorByName(protoreflect.FullName(op.ReqMessageFullName)); err == nil {
					if md, ok := d.(protoreflect.MessageDescriptor); ok {
						reqDesc = md
					}
				}
			}
			if op.RspMessageFullName != "" {
				if d, err := files.FindDescriptorByName(protoreflect.FullName(op.RspMessageFullName)); err == nil {
					if md, ok := d.(protoreflect.MessageDescriptor); ok {
						rspDesc = md
					}
				}
			}

			opsMap[key] = &CompiledOperation{
				SchemaID:      s.ID,
				SchemaName:    s.Name,
				SchemaVersion: s.Version,
				ServiceName:   op.ServiceName,
				Operation:     op.Operation,
				ReqDesc:       reqDesc,
				RspDesc:       rspDesc,
			}
		}
	}

	r.compiled.Store(&CompiledRegistry{Operations: opsMap})
	return nil
}

// extractOperationsFromDescriptor 解析 FileDescriptorSet 提取全部服务与方法映射
func extractOperationsFromDescriptor(rawBytes []byte) (*descriptorpb.FileDescriptorSet, *protoregistry.Files, []models.ProtoOperation, error) {
	if len(rawBytes) == 0 {
		return nil, nil, nil, ErrDescriptorEmpty
	}
	if len(rawBytes) > MaxDescriptorFileSize {
		return nil, nil, nil, ErrDescriptorTooLarge
	}

	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(rawBytes, &fds); err != nil {
		return nil, nil, nil, ErrInvalidDescriptorProto
	}
	// 校验反序列化完整度：必须含有文件，且不能有未识别字段
	if len(fds.File) == 0 || len(fds.ProtoReflect().GetUnknown()) > 0 {
		return nil, nil, nil, ErrInvalidDescriptorProto
	}

	files, err := protodesc.NewFiles(&fds)
	if err != nil {
		return nil, nil, nil, ErrInvalidDescriptorProto
	}

	var ops []models.ProtoOperation
	for _, file := range fds.File {
		pkg := file.GetPackage()
		fileName := file.GetName()
		for _, svc := range file.Service {
			svcName := svc.GetName()
			// 服务名规范：若包名非空采用 {package}.{serviceName}，否则采用 {serviceName}
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
					ClientStreaming:    method.GetClientStreaming(),
					ServerStreaming:    method.GetServerStreaming(),
					IsActive:           false,
				})
			}
		}
	}

	return &fds, files, ops, nil
}

// UploadSchema 上传并持久化 .pb 描述集
func (r *Registry) UploadSchema(in UploadSchemaInput) (*models.ProtoSchema, []models.ProtoOperation, error) {
	name := strings.TrimSpace(in.Name)
	runeCount := utf8.RuneCountInString(name)
	if runeCount == 0 || runeCount > 128 {
		return nil, nil, ErrInvalidSchemaName
	}

	_, _, ops, err := extractOperationsFromDescriptor(in.RawBytes)
	if err != nil {
		return nil, nil, err
	}

	cleanFileName := filepath.Base(in.FileName)
	if cleanFileName == "" || cleanFileName == "." {
		cleanFileName = name + ".pb"
	}

	shaBytes := sha256.Sum256(in.RawBytes)
	fileSHA256 := hex.EncodeToString(shaBytes[:])

	var schema models.ProtoSchema
	var insertedOps []models.ProtoOperation

	err = r.db.Transaction(func(tx *gorm.DB) error {
		// 计算同名自增版本号
		var maxVer int
		_ = tx.Model(&models.ProtoSchema{}).Where("name = ?", name).Select("COALESCE(MAX(version), 0)").Scan(&maxVer).Error
		nextVer := maxVer + 1

		schema = models.ProtoSchema{
			Name:            name,
			Version:         nextVer,
			VersionTag:      in.VersionTag,
			FileName:        cleanFileName,
			FileSHA256:      fileSHA256,
			FileSize:        len(in.RawBytes),
			DescriptorBytes: in.RawBytes,
			IsActive:        false,
			OperationCount:  len(ops),
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
			if err := tx.Create(&op).Error; err != nil {
				return err
			}
			insertedOps[i] = op
		}

		// 若指定自动激活，在此事务中直接执行激活逻辑
		if in.AutoActivate {
			var conflicts []OperationConflictItem
			for _, op := range insertedOps {
				var activeOther models.ProtoOperation
				err := tx.Joins("JOIN traefik_proto_schemas ON traefik_proto_schemas.id = traefik_proto_operations.schema_id").
					Where("traefik_proto_schemas.is_active = true AND traefik_proto_schemas.name != ? AND traefik_proto_operations.service_name = ? AND traefik_proto_operations.operation = ?",
						name, op.ServiceName, op.Operation).
					First(&activeOther).Error
				if err == nil {
					var otherSchema models.ProtoSchema
					_ = tx.First(&otherSchema, activeOther.SchemaID).Error
					conflicts = append(conflicts, OperationConflictItem{
						ServiceName:        op.ServiceName,
						Operation:          op.Operation,
						ConflictSchemaID:   otherSchema.ID,
						ConflictSchemaName: otherSchema.Name,
						ConflictVersion:    otherSchema.Version,
					})
				}
			}
			if len(conflicts) > 0 {
				return &SchemaConflictError{Conflicts: conflicts}
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
		return nil, nil, err
	}

	if in.AutoActivate {
		_ = r.Reload()
	}

	return &schema, insertedOps, nil
}

// ActivateSchemaVersion 切换生效版本并做跨 Schema 冲突检测
func (r *Registry) ActivateSchemaVersion(schemaID uint64) (*models.ProtoSchema, *models.ProtoSchema, int, error) {
	var target models.ProtoSchema
	if err := r.db.First(&target, schemaID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, 0, ErrSchemaNotFound
		}
		return nil, nil, 0, err
	}

	var targetOps []models.ProtoOperation
	if err := r.db.Where("schema_id = ?", schemaID).Find(&targetOps).Error; err != nil {
		return nil, nil, 0, err
	}

	// 检测与其他非同名已生效描述集之间的接口冲突
	var conflicts []OperationConflictItem
	for _, op := range targetOps {
		var activeOther models.ProtoOperation
		err := r.db.Joins("JOIN traefik_proto_schemas ON traefik_proto_schemas.id = traefik_proto_operations.schema_id").
			Where("traefik_proto_schemas.is_active = true AND traefik_proto_schemas.name != ? AND traefik_proto_operations.service_name = ? AND traefik_proto_operations.operation = ?",
				target.Name, op.ServiceName, op.Operation).
			First(&activeOther).Error
		if err == nil {
			var otherSchema models.ProtoSchema
			_ = r.db.First(&otherSchema, activeOther.SchemaID).Error
			conflicts = append(conflicts, OperationConflictItem{
				ServiceName:        op.ServiceName,
				Operation:          op.Operation,
				ConflictSchemaID:   otherSchema.ID,
				ConflictSchemaName: otherSchema.Name,
				ConflictVersion:    otherSchema.Version,
			})
		}
	}
	if len(conflicts) > 0 {
		return nil, nil, 0, &SchemaConflictError{Conflicts: conflicts}
	}

	var prevActive models.ProtoSchema
	var hasPrevActive bool

	err := r.db.Transaction(func(tx *gorm.DB) error {
		// 查找当前同名已生效的旧版本
		err := tx.Where("name = ? AND is_active = true", target.Name).First(&prevActive).Error
		if err == nil {
			hasPrevActive = true
			_ = tx.Model(&models.ProtoSchema{}).Where("id = ?", prevActive.ID).
				Updates(map[string]interface{}{"is_active": false}).Error
			_ = tx.Model(&models.ProtoOperation{}).Where("schema_id = ?", prevActive.ID).
				Updates(map[string]interface{}{"is_active": false}).Error
		}

		if err := tx.Model(&models.ProtoSchema{}).Where("id = ?", schemaID).
			Updates(map[string]interface{}{"is_active": true}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.ProtoOperation{}).Where("schema_id = ?", schemaID).
			Updates(map[string]interface{}{"is_active": true}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, nil, 0, err
	}

	_ = r.Reload()

	target.IsActive = true

	var prevPtr *models.ProtoSchema
	if hasPrevActive {
		prevActive.IsActive = false
		prevPtr = &prevActive
	}

	return prevPtr, &target, len(targetOps), nil
}

// DeleteSchemaVersion 删除未生效的描述集版本
func (r *Registry) DeleteSchemaVersion(schemaID uint64) (*models.ProtoSchema, error) {
	var schema models.ProtoSchema
	if err := r.db.First(&schema, schemaID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSchemaNotFound
		}
		return nil, err
	}

	if schema.IsActive {
		return nil, ErrDeleteActiveSchema
	}

	err := r.db.Transaction(func(tx *gorm.DB) error {
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

// LookupOperation 检索已编译生效的接口元数据
func (r *Registry) LookupOperation(serviceName, operation string) *CompiledOperation {
	compiled := r.compiled.Load()
	if compiled == nil {
		return nil
	}
	return compiled.Operations[serviceName+":"+operation]
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
