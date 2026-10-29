package handlers

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	commonAuth "code-common/backend/auth"
	commonModels "code-common/backend/models"
	"code-traefik/internal/server/decoder"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
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

	query := models.DB.Model(&models.ProtoSchema{}).Select("id, name, version, version_tag, file_name, file_size, is_active, activated_at, description, created_by, created_at, updated_at")
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

	var items []models.ProtoSchema
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

	var items []models.ProtoOperation
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
