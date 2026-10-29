package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	commonAuth "code-common/backend/auth"
	commonModels "code-common/backend/models"
	"code-traefik/internal/server/topology"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
)

// SubsystemReq 创建/更新子系统请求体
type SubsystemReq struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	OwnerID     *uint  `json:"owner_id"`
	Description string `json:"description"`
}

// FunctionalGroupReq 创建/更新功能组请求体
type FunctionalGroupReq struct {
	SubsystemID uint64  `json:"subsystem_id"`
	ParentID    *uint64 `json:"parent_id"`
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	SortOrder   int     `json:"sort_order"`
	Description string  `json:"description"`
}

// MoveFunctionalGroupReq 移动功能组请求体
type MoveFunctionalGroupReq struct {
	NewParentID *uint64 `json:"new_parent_id"`
	SortOrder   *int    `json:"sort_order"`
}

// ModuleReq 创建/更新功能模块请求体
type ModuleReq struct {
	GroupID     uint64  `json:"group_id"`
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	OwnerID     *uint   `json:"owner_id"`
	ProcessID   *uint64 `json:"process_id"`
	Description string  `json:"description"`
}

// ModuleInterfaceReq 创建接口契约请求体
type ModuleInterfaceReq struct {
	RelationType    string   `json:"relation_type"`
	ServiceName     string   `json:"service_name"`
	Operation       string   `json:"operation"`
	DiffIgnorePaths []string `json:"diff_ignore_paths"`
	Description     string   `json:"description"`
}

// UpdateModuleInterfaceReq 更新接口契约请求体
type UpdateModuleInterfaceReq struct {
	DiffIgnorePaths []string `json:"diff_ignore_paths"`
	Description     string   `json:"description"`
}

// normalizePageSize 通用分页归一化 (15/25/50/100，默认 25)
func normalizePageSize(size int) int {
	switch size {
	case 15, 25, 50, 100:
		return size
	default:
		return 25
	}
}

// parsePagination 解析分页参数
func parsePagination(c *gin.Context) (page int, pageSize int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ = strconv.Atoi(c.DefaultQuery("page_size", "25"))
	pageSize = normalizePageSize(pageSize)
	return page, pageSize
}

// --- Subsystem Handlers ---

// ListSubsystems GET /api/arch/subsystems
func ListSubsystems(c *gin.Context) {
	page, pageSize := parsePagination(c)
	search := strings.TrimSpace(c.Query("search"))
	ownerIDStr := c.Query("owner_id")

	query := models.DB.Model(&models.Subsystem{}).Preload("Owner")
	if search != "" {
		s := "%" + search + "%"
		query = query.Where("code ILIKE ? OR name ILIKE ?", s, s)
	}
	if ownerIDStr != "" {
		if oid, err := strconv.ParseUint(ownerIDStr, 10, 32); err == nil {
			query = query.Where("owner_id = ?", uint(oid))
		}
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "统计子系统失败"})
		return
	}

	items := make([]models.Subsystem, 0)
	offset := (page - 1) * pageSize
	if err := query.Order("id ASC").Offset(offset).Limit(pageSize).Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询子系统列表失败"})
		return
	}

	// 敏感字段脱敏
	for i := range items {
		if items[i].Owner != nil {
			items[i].Owner.Email = ""
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// CreateSubsystem POST /api/arch/subsystems (AdminOnly)
func CreateSubsystem(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	var req SubsystemReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
		return
	}

	item, err := topology.CreateSubsystem(models.DB, topology.SubsystemInput{
		Code:        req.Code,
		Name:        req.Name,
		OwnerID:     req.OwnerID,
		Description: req.Description,
		OperatorID:  uc.UserID,
	})
	if err != nil {
		if errors.Is(err, topology.ErrDuplicateCode) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrInvalidCodeFormat) || errors.Is(err, topology.ErrInvalidName) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "create", commonModels.AuditLevelP2, fmt.Sprintf("创建子系统 [%s]", item.Name), "subsystem", item.ID, item.Name, nil, item)
	c.JSON(http.StatusCreated, item)
}

// UpdateSubsystem PUT /api/arch/subsystems/:id (AdminOnly)
func UpdateSubsystem(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的子系统 ID"})
		return
	}

	var req SubsystemReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
		return
	}

	before, after, err := topology.UpdateSubsystem(models.DB, id, topology.SubsystemInput{
		Code:        req.Code,
		Name:        req.Name,
		OwnerID:     req.OwnerID,
		Description: req.Description,
		OperatorID:  uc.UserID,
	})
	if err != nil {
		if errors.Is(err, topology.ErrSubsystemNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrDuplicateCode) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrInvalidCodeFormat) || errors.Is(err, topology.ErrInvalidName) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "update", commonModels.AuditLevelP2, fmt.Sprintf("更新子系统 [%s]", after.Name), "subsystem", after.ID, after.Name, before, after)
	c.JSON(http.StatusOK, after)
}

// DeleteSubsystem DELETE /api/arch/subsystems/:id (AdminOnly)
func DeleteSubsystem(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的子系统 ID"})
		return
	}

	deleted, err := topology.DeleteSubsystem(models.DB, id)
	if err != nil {
		if errors.Is(err, topology.ErrSubsystemNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrSubsystemNotEmpty) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "delete", commonModels.AuditLevelP1, fmt.Sprintf("删除子系统 [%s]", deleted.Name), "subsystem", deleted.ID, deleted.Name, deleted, nil)
	c.JSON(http.StatusOK, gin.H{"message": "子系统已删除"})
}

// --- Functional Group Handlers ---

// GetFunctionalGroupTree GET /api/arch/functional-groups/tree?subsystem_id=...
func GetFunctionalGroupTree(c *gin.Context) {
	subsystemID, err := strconv.ParseUint(c.Query("subsystem_id"), 10, 64)
	if err != nil || subsystemID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "subsystem_id 参数必填且必须合法"})
		return
	}

	tree, err := topology.GetFunctionalGroupTree(models.DB, subsystemID)
	if err != nil {
		if errors.Is(err, topology.ErrSubsystemNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if tree == nil {
		tree = make([]*topology.FunctionalGroupTreeNode, 0)
	}

	c.JSON(http.StatusOK, gin.H{
		"subsystem_id": subsystemID,
		"tree":         tree,
	})
}

// CreateFunctionalGroup POST /api/arch/functional-groups (AdminOnly)
func CreateFunctionalGroup(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	var req FunctionalGroupReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
		return
	}

	item, err := topology.CreateFunctionalGroup(models.DB, topology.FunctionalGroupInput{
		SubsystemID: req.SubsystemID,
		ParentID:    req.ParentID,
		Code:        req.Code,
		Name:        req.Name,
		SortOrder:   req.SortOrder,
		Description: req.Description,
		OperatorID:  uc.UserID,
	})
	if err != nil {
		if errors.Is(err, topology.ErrDuplicateCode) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrSubsystemNotFound) || errors.Is(err, topology.ErrParentGroupNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrCrossSubsystemMove) || errors.Is(err, topology.ErrExceedMaxTreeDepth) ||
			errors.Is(err, topology.ErrInvalidCodeFormat) || errors.Is(err, topology.ErrInvalidName) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "create", commonModels.AuditLevelP2, fmt.Sprintf("创建功能组 [%s]", item.Name), "functional_group", item.ID, item.Name, nil, item)
	c.JSON(http.StatusCreated, item)
}

// UpdateFunctionalGroup PUT /api/arch/functional-groups/:id (AdminOnly)
func UpdateFunctionalGroup(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的功能组 ID"})
		return
	}

	var req struct {
		Code        string  `json:"code"`
		Name        string  `json:"name"`
		SortOrder   *int    `json:"sort_order"`
		Description *string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
		return
	}

	before, after, err := topology.UpdateFunctionalGroup(models.DB, id, req.Code, req.Name, req.SortOrder, req.Description, uc.UserID)
	if err != nil {
		if errors.Is(err, topology.ErrGroupNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrDuplicateCode) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrInvalidCodeFormat) || errors.Is(err, topology.ErrInvalidName) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "update", commonModels.AuditLevelP2, fmt.Sprintf("更新功能组 [%s]", after.Name), "functional_group", after.ID, after.Name, before, after)
	c.JSON(http.StatusOK, after)
}

// MoveFunctionalGroup PATCH /api/arch/functional-groups/:id/move (AdminOnly)
func MoveFunctionalGroup(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的功能组 ID"})
		return
	}

	var req MoveFunctionalGroupReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
		return
	}

	before, after, updatedCount, err := topology.MoveFunctionalGroupWithResult(models.DB, id, req.NewParentID, req.SortOrder, uc.UserID)
	if err != nil {
		if errors.Is(err, topology.ErrGroupNotFound) || errors.Is(err, topology.ErrParentGroupNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrCycleMoveToSelf) || errors.Is(err, topology.ErrCycleMoveToDescendant) ||
			errors.Is(err, topology.ErrCrossSubsystemMove) || errors.Is(err, topology.ErrExceedMaxTreeDepth) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "move", commonModels.AuditLevelP1, fmt.Sprintf("移动功能组 [%s]", after.Name), "functional_group", after.ID, after.Name, before, after)
	c.JSON(http.StatusOK, gin.H{
		"group":               after,
		"updated_descendants": updatedCount,
	})
}

// DeleteFunctionalGroup DELETE /api/arch/functional-groups/:id (AdminOnly)
func DeleteFunctionalGroup(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的功能组 ID"})
		return
	}

	deleted, err := topology.DeleteFunctionalGroup(models.DB, id)
	if err != nil {
		if errors.Is(err, topology.ErrGroupNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrGroupNotEmpty) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "delete", commonModels.AuditLevelP1, fmt.Sprintf("删除功能组 [%s]", deleted.Name), "functional_group", deleted.ID, deleted.Name, deleted, nil)
	c.JSON(http.StatusOK, gin.H{"message": "功能组已删除"})
}

// --- Module Handlers ---

// ListModules GET /api/arch/modules
func ListModules(c *gin.Context) {
	page, pageSize := parsePagination(c)
	subsystemIDStr := c.Query("subsystem_id")
	groupIDStr := c.Query("group_id")
	processIDStr := c.Query("process_id")
	search := strings.TrimSpace(c.Query("search"))

	query := models.DB.Model(&models.Module{}).Preload("Group").Preload("Subsystem").Preload("Process").Preload("Owner")
	if subsystemIDStr != "" {
		if sid, err := strconv.ParseUint(subsystemIDStr, 10, 64); err == nil {
			query = query.Where("subsystem_id = ?", sid)
		}
	}
	if groupIDStr != "" {
		if gid, err := strconv.ParseUint(groupIDStr, 10, 64); err == nil {
			query = query.Where("group_id = ?", gid)
		}
	}
	if processIDStr != "" {
		if pid, err := strconv.ParseUint(processIDStr, 10, 64); err == nil {
			query = query.Where("process_id = ?", pid)
		}
	}
	if search != "" {
		s := "%" + search + "%"
		query = query.Where("code ILIKE ? OR name ILIKE ?", s, s)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "统计模块失败"})
		return
	}

	items := make([]models.Module, 0)
	offset := (page - 1) * pageSize
	if err := query.Order("id ASC").Offset(offset).Limit(pageSize).Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询模块列表失败"})
		return
	}

	for i := range items {
		if items[i].Owner != nil {
			items[i].Owner.Email = ""
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetModule GET /api/arch/modules/:id
func GetModule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的模块 ID"})
		return
	}

	var mod models.Module
	if err := models.DB.Preload("Group").Preload("Subsystem").Preload("Process").Preload("Owner").First(&mod, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "功能模块不存在"})
		return
	}
	if mod.Owner != nil {
		mod.Owner.Email = ""
	}

	ifaces := make([]models.ModuleInterface, 0)
	_ = models.DB.Where("module_id = ?", mod.ID).Order("relation_type ASC, id ASC").Find(&ifaces).Error

	c.JSON(http.StatusOK, gin.H{
		"module":     mod,
		"interfaces": ifaces,
	})
}

// CreateModule POST /api/arch/modules (AdminOnly)
func CreateModule(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	var req ModuleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
		return
	}

	item, err := topology.CreateModule(models.DB, topology.ModuleInput{
		GroupID:      req.GroupID,
		Code:         req.Code,
		Name:         req.Name,
		OwnerID:      req.OwnerID,
		ProcessID:    req.ProcessID,
		HasProcessID: req.ProcessID != nil,
		Description:  req.Description,
		OperatorID:   uc.UserID,
	})
	if err != nil {
		if errors.Is(err, topology.ErrDuplicateCode) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrGroupNotFound) || errors.Is(err, topology.ErrProcessNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrInvalidCodeFormat) || errors.Is(err, topology.ErrInvalidName) ||
			errors.Is(err, topology.ErrInvalidBaseProcessScope) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "create", commonModels.AuditLevelP2, fmt.Sprintf("创建功能模块 [%s]", item.Name), "module", item.ID, item.Name, nil, item)
	c.JSON(http.StatusCreated, item)
}

// UpdateModule PUT /api/arch/modules/:id (AdminOnly)
func UpdateModule(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的模块 ID"})
		return
	}

	var req ModuleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
		return
	}

	before, after, err := topology.UpdateModule(models.DB, id, topology.ModuleInput{
		GroupID:      req.GroupID,
		Code:         req.Code,
		Name:         req.Name,
		OwnerID:      req.OwnerID,
		ProcessID:    req.ProcessID,
		HasProcessID: true,
		Description:  req.Description,
		OperatorID:   uc.UserID,
	})
	if err != nil {
		if errors.Is(err, topology.ErrModuleNotFound) || errors.Is(err, topology.ErrGroupNotFound) || errors.Is(err, topology.ErrProcessNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrDuplicateCode) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrInvalidCodeFormat) || errors.Is(err, topology.ErrInvalidName) ||
			errors.Is(err, topology.ErrInvalidBaseProcessScope) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "update", commonModels.AuditLevelP2, fmt.Sprintf("更新功能模块 [%s]", after.Name), "module", after.ID, after.Name, before, after)
	c.JSON(http.StatusOK, after)
}

// DeleteModule DELETE /api/arch/modules/:id (AdminOnly)
func DeleteModule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的模块 ID"})
		return
	}

	deleted, err := topology.DeleteModule(models.DB, id)
	if err != nil {
		if errors.Is(err, topology.ErrModuleNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrModuleNotEmpty) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "delete", commonModels.AuditLevelP1, fmt.Sprintf("删除功能模块 [%s]", deleted.Name), "module", deleted.ID, deleted.Name, deleted, nil)
	c.JSON(http.StatusOK, gin.H{"message": "功能模块已删除"})
}

// --- Module Interface Handlers ---

// ListModuleInterfaces GET /api/arch/modules/:id/interfaces
func ListModuleInterfaces(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的模块 ID"})
		return
	}

	var mod models.Module
	if err := models.DB.First(&mod, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "功能模块不存在"})
		return
	}

	relType := strings.ToUpper(strings.TrimSpace(c.Query("relation_type")))
	query := models.DB.Model(&models.ModuleInterface{}).Where("module_id = ?", id)
	if relType != "" {
		query = query.Where("relation_type = ?", relType)
	}

	items := make([]models.ModuleInterface, 0)
	if err := query.Order("id ASC").Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询接口契约失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"items": items})
}

// CreateModuleInterface POST /api/arch/modules/:id/interfaces (AdminOnly)
func CreateModuleInterface(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	moduleID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的模块 ID"})
		return
	}

	var req ModuleInterfaceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
		return
	}

	item, err := topology.CreateModuleInterface(models.DB, topology.ModuleInterfaceInput{
		ModuleID:        moduleID,
		RelationType:    req.RelationType,
		ServiceName:     req.ServiceName,
		Operation:       req.Operation,
		DiffIgnorePaths: req.DiffIgnorePaths,
		Description:     req.Description,
		OperatorID:      uc.UserID,
	})
	if err != nil {
		var conflictErr *topology.ProvideConflictError
		if errors.As(err, &conflictErr) {
			c.JSON(http.StatusConflict, gin.H{
				"error":              conflictErr.Error(),
				"conflict_module_id": conflictErr.HolderModuleID,
				"holder_module_code": conflictErr.HolderModuleCode,
				"holder_module_name": conflictErr.HolderModuleName,
			})
			return
		}
		if errors.Is(err, topology.ErrDuplicateModuleInterface) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrModuleNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrInvalidRelationType) || errors.Is(err, topology.ErrInvalidServiceNameFormat) ||
			errors.Is(err, topology.ErrInvalidOperationFormat) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "create", commonModels.AuditLevelP2, fmt.Sprintf("创建接口契约 [%s:%s]", item.ServiceName, item.Operation), "module_interface", item.ID, item.ServiceName+":"+item.Operation, nil, item)
	c.JSON(http.StatusCreated, item)
}

// UpdateModuleInterface PUT /api/arch/module-interfaces/:ifaceId (AdminOnly)
func UpdateModuleInterface(c *gin.Context) {
	ifaceID, err := strconv.ParseUint(c.Param("ifaceId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的接口契约 ID"})
		return
	}

	var req UpdateModuleInterfaceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
		return
	}

	before, after, err := topology.UpdateModuleInterface(models.DB, ifaceID, req.DiffIgnorePaths, req.Description)
	if err != nil {
		if errors.Is(err, topology.ErrModuleInterfaceNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "update", commonModels.AuditLevelP2, fmt.Sprintf("更新接口契约 [%s:%s]", after.ServiceName, after.Operation), "module_interface", after.ID, after.ServiceName+":"+after.Operation, before, after)
	c.JSON(http.StatusOK, after)
}

// DeleteModuleInterface DELETE /api/arch/module-interfaces/:ifaceId (AdminOnly)
func DeleteModuleInterface(c *gin.Context) {
	ifaceID, err := strconv.ParseUint(c.Param("ifaceId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的接口契约 ID"})
		return
	}

	deleted, err := topology.DeleteModuleInterface(models.DB, ifaceID)
	if err != nil {
		if errors.Is(err, topology.ErrModuleInterfaceNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "delete", commonModels.AuditLevelP1, fmt.Sprintf("删除接口契约 [%s:%s]", deleted.ServiceName, deleted.Operation), "module_interface", deleted.ID, deleted.ServiceName+":"+deleted.Operation, deleted, nil)
	c.JSON(http.StatusOK, gin.H{"message": "接口契约已删除"})
}

// GetStaticTopology GET /api/arch/topology/static
func GetStaticTopology(c *gin.Context) {
	view := strings.ToUpper(strings.TrimSpace(c.DefaultQuery("view", topology.TopologyViewLogical)))
	if view != topology.TopologyViewLogical && view != topology.TopologyViewPhysical {
		c.JSON(http.StatusBadRequest, gin.H{"error": "静态拓扑视角参数非法，仅允许 LOGICAL 或 PHYSICAL"})
		return
	}

	var subsystemID *uint64
	if sidStr := c.Query("subsystem_id"); sidStr != "" {
		if sid, err := strconv.ParseUint(sidStr, 10, 64); err == nil {
			subsystemID = &sid
		}
	}

	res, err := topology.BuildStaticTopology(models.DB, view, subsystemID)
	if err != nil {
		if errors.Is(err, topology.ErrInvalidTopologyView) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if res != nil {
		if res.Nodes == nil {
			res.Nodes = make([]topology.StaticTopologyNode, 0)
		}
		if res.Edges == nil {
			res.Edges = make([]topology.StaticTopologyEdge, 0)
		}
	}

	c.JSON(http.StatusOK, res)
}
