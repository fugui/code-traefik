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

// ListProtoSchemas GET /api/proto-schemas
func ListProtoSchemas(c *gin.Context) {
	page, pageSize := parsePagination(c)
	name := strings.TrimSpace(c.Query("name"))
	activeOnlyStr := c.Query("active_only")
	search := strings.TrimSpace(c.Query("search"))

	query := models.DB.Model(&models.ProtoSchema{}).Omit("descriptor_bytes")
	if name != "" {
		query = query.Where("name = ?", name)
	}
	if activeOnlyStr == "true" {
		query = query.Where("is_active = true")
	}
	if search != "" {
		s := "%" + search + "%"
		query = query.Where("name ILIKE ? OR file_name ILIKE ?", s, s)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "统计描述集失败"})
		return
	}

	items := make([]models.ProtoSchema, 0)
	offset := (page - 1) * pageSize
	if err := query.Order("name ASC, version DESC").Offset(offset).Limit(pageSize).Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询描述集失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// UploadProtoSchema POST /api/proto-schemas (AdminOnly)
func UploadProtoSchema(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)

	// 安全限制：单次上传请求 Body 上限 4MB + 10KB 头部开销
	// TODO(security): 接入企业制品防病毒扫描网关对上传描述集做沙箱扫描
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4<<20+10240)

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少或非法的 file 文件参数（须 <= 4MB）"})
		return
	}
	defer file.Close()

	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "描述集标识 name 不能为空"})
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

	schema, ops, err := reg.UploadSchema(decoder.UploadSchemaInput{
		Name:         name,
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
		if errors.Is(err, decoder.ErrDescriptorEmpty) || errors.Is(err, decoder.ErrDescriptorTooLarge) ||
			errors.Is(err, decoder.ErrInvalidDescriptorProto) || errors.Is(err, decoder.ErrInvalidSchemaName) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "upload", commonModels.AuditLevelP1, fmt.Sprintf("上传描述集 [%s] 版本 %d", schema.Name, schema.Version), "proto_schema", schema.ID, schema.Name, nil, schema)
	c.JSON(http.StatusCreated, gin.H{
		"schema":     schema,
		"operations": ops,
	})
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

// ListProtoOperations GET /api/proto-operations
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

// DownloadProtoSchema GET /api/proto-schemas/:id/download
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

	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", cleanFileName))
	c.Header("Content-Length", strconv.Itoa(len(schema.DescriptorBytes)))
	c.Data(http.StatusOK, "application/octet-stream", schema.DescriptorBytes)
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

	// 聚合服务信息
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

	// 关联 ModuleService 查看认领情况
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
