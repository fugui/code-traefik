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
	"gorm.io/gorm"
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

// ModuleServiceReq 创建供给服务请求体
type ModuleServiceReq struct {
	ServiceName     string   `json:"service_name"`
	DiffIgnorePaths []string `json:"diff_ignore_paths"`
	Description     string   `json:"description"`
}

// UpdateModuleServiceReq 更新供给服务请求体
type UpdateModuleServiceReq struct {
	DiffIgnorePaths []string `json:"diff_ignore_paths"`
	Description     string   `json:"description"`
}

// ModuleDependencyReq 创建服务依赖请求体
type ModuleDependencyReq struct {
	IsExternal      bool    `json:"is_external"`
	ExternalSystem  string  `json:"external_system"`
	ExternalService string  `json:"external_service"`
	ServiceID       *uint64 `json:"service_id"`
	Description     string  `json:"description"`
}

// ProcessBindingReq 模块进程装载/绑定请求体
type ProcessBindingReq struct {
	ProcessID *uint64 `json:"process_id"`
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
	if err := models.DB.Preload("Group").Preload("Subsystem").Preload("Process.Agent").Preload("Owner").First(&mod, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "功能模块不存在"})
		return
	}
	if mod.Owner != nil {
		mod.Owner.Email = ""
	}

	services, _ := topology.ListModuleServices(models.DB, mod.ID)
	dependencies, _ := topology.ListModuleDependencies(models.DB, mod.ID)

	c.JSON(http.StatusOK, gin.H{
		"module":       mod,
		"services":     services,
		"dependencies": dependencies,
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

// --- Module Process Binding Handlers ---

// BindModuleProcess POST /api/arch/modules/:id/process-binding (AdminOnly)
func BindModuleProcess(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的模块 ID"})
		return
	}

	var req ProcessBindingReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
		return
	}

	var mod models.Module
	if err := models.DB.First(&mod, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "功能模块不存在"})
		return
	}

	oldPID := mod.ProcessID
	if req.ProcessID != nil && *req.ProcessID > 0 {
		var proc models.Process
		if err := models.DB.Preload("Agent").First(&proc, *req.ProcessID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "目标进程不存在"})
			return
		}
		if proc.Agent != nil && proc.Agent.Scope == models.ScopePrivate {
			c.JSON(http.StatusBadRequest, gin.H{"error": "公共功能模块不能绑定个人私有 Agent 下的进程作为基线"})
			return
		}

		mod.ProcessID = req.ProcessID
		if err := models.DB.Save(&mod).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存模块进程绑定失败"})
			return
		}

		// 确保 traefik_process_modules 映射存在
		var pm models.ProcessModule
		if err := models.DB.Where("process_id = ? AND module_id = ?", *req.ProcessID, id).First(&pm).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			models.DB.Create(&models.ProcessModule{ProcessID: *req.ProcessID, ModuleID: id})
		}
		// 若此前绑过其他旧进程，清理旧进程模块挂载
		if oldPID != nil && *oldPID != *req.ProcessID {
			models.DB.Where("process_id = ? AND module_id = ?", *oldPID, id).Delete(&models.ProcessModule{})
		}
	} else {
		// 解绑公共基线进程
		mod.ProcessID = nil
		models.DB.Model(&mod).Update("process_id", nil)
		if oldPID != nil {
			models.DB.Where("process_id = ? AND module_id = ?", *oldPID, id).Delete(&models.ProcessModule{})
		}
	}

	_ = models.DB.Preload("Group").Preload("Subsystem").Preload("Process.Agent").Preload("Owner").First(&mod, id)
	recordAudit(c, "update", commonModels.AuditLevelP2, fmt.Sprintf("变更模块 [%s] 进程绑定", mod.Name), "module", mod.ID, mod.Name, nil, mod)
	c.JSON(http.StatusOK, mod)
}

// --- Module Service & Dependency Handlers ---

// ListModuleServices GET /api/arch/modules/:id/services
func ListModuleServices(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的模块 ID"})
		return
	}

	services, err := topology.ListModuleServices(models.DB, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询供给服务契约失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"items": services})
}

// CreateModuleService POST /api/arch/modules/:id/services (AdminOnly)
func CreateModuleService(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	moduleID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的模块 ID"})
		return
	}

	var req ModuleServiceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
		return
	}

	item, err := topology.CreateModuleService(models.DB, topology.ModuleServiceInput{
		ModuleID:        moduleID,
		ServiceName:     req.ServiceName,
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
		if errors.Is(err, topology.ErrDuplicateModuleService) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrModuleNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrInvalidServiceNameFormat) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "create", commonModels.AuditLevelP2, fmt.Sprintf("创建供给服务契约 [%s]", item.ServiceName), "module_service", item.ID, item.ServiceName, nil, item)
	c.JSON(http.StatusCreated, item)
}

// UpdateModuleService PUT /api/arch/module-services/:serviceId (AdminOnly)
func UpdateModuleService(c *gin.Context) {
	serviceID, err := strconv.ParseUint(c.Param("serviceId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的服务契约 ID"})
		return
	}

	var req UpdateModuleServiceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
		return
	}

	before, after, err := topology.UpdateModuleService(models.DB, serviceID, req.DiffIgnorePaths, req.Description)
	if err != nil {
		if errors.Is(err, topology.ErrModuleServiceNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "update", commonModels.AuditLevelP2, fmt.Sprintf("更新供给服务契约 [%s]", after.ServiceName), "module_service", after.ID, after.ServiceName, before, after)
	c.JSON(http.StatusOK, after)
}

// DeleteModuleService DELETE /api/arch/module-services/:serviceId (AdminOnly)
func DeleteModuleService(c *gin.Context) {
	serviceID, err := strconv.ParseUint(c.Param("serviceId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的服务契约 ID"})
		return
	}

	deleted, err := topology.DeleteModuleService(models.DB, serviceID)
	if err != nil {
		if errors.Is(err, topology.ErrModuleServiceNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrServiceHasDependencies) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "delete", commonModels.AuditLevelP1, fmt.Sprintf("删除供给服务契约 [%s]", deleted.ServiceName), "module_service", deleted.ID, deleted.ServiceName, deleted, nil)
	c.JSON(http.StatusOK, gin.H{"message": "供给服务契约已删除"})
}

// ListModuleDependencies GET /api/arch/modules/:id/dependencies
func ListModuleDependencies(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的模块 ID"})
		return
	}

	deps, err := topology.ListModuleDependencies(models.DB, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询服务依赖关系失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"items": deps})
}

// CreateModuleDependency POST /api/arch/modules/:id/dependencies (AdminOnly)
func CreateModuleDependency(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	moduleID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的模块 ID"})
		return
	}

	var req ModuleDependencyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
		return
	}

	item, err := topology.CreateModuleDependency(models.DB, topology.ModuleDependencyInput{
		ConsumerModuleID: moduleID,
		IsExternal:       req.IsExternal,
		ExternalSystem:   req.ExternalSystem,
		ExternalService:  req.ExternalService,
		ServiceID:        req.ServiceID,
		Description:      req.Description,
		OperatorID:       uc.UserID,
	})
	if err != nil {
		if errors.Is(err, topology.ErrDuplicateDependency) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrSelfDependency) || errors.Is(err, topology.ErrInvalidExternalDependency) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrModuleNotFound) || errors.Is(err, topology.ErrModuleServiceNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "create", commonModels.AuditLevelP2, fmt.Sprintf("添加服务依赖 [模块ID:%d]", moduleID), "module_dependency", item.ID, fmt.Sprintf("dep_%d", item.ID), nil, item)
	c.JSON(http.StatusCreated, item)
}

// DeleteModuleDependency DELETE /api/arch/module-dependencies/:depId (AdminOnly)
func DeleteModuleDependency(c *gin.Context) {
	depID, err := strconv.ParseUint(c.Param("depId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的依赖关系 ID"})
		return
	}

	deleted, err := topology.DeleteModuleDependency(models.DB, depID)
	if err != nil {
		if errors.Is(err, topology.ErrModuleDependencyNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "delete", commonModels.AuditLevelP1, fmt.Sprintf("解除服务依赖 [ID:%d]", deleted.ID), "module_dependency", deleted.ID, fmt.Sprintf("dep_%d", deleted.ID), deleted, nil)
	c.JSON(http.StatusOK, gin.H{"message": "服务依赖关系已解除"})
}

// ListAvailableServices GET /api/arch/services/available
func ListAvailableServices(c *gin.Context) {
	excludeModID, _ := strconv.ParseUint(c.Query("exclude_module_id"), 10, 64)
	items, err := topology.ListAvailableServices(models.DB, excludeModID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询可用服务列表失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
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
