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

// ProcessReq 进程创建/修改请求体
type ProcessReq struct {
	AgentID          uint64   `json:"agent_id"`
	Name             string   `json:"name"`
	ListenPort       int      `json:"listen_port"`
	AdvertiseIP      string   `json:"advertise_ip"`
	CmdPattern       string   `json:"cmd_pattern"`
	ModuleIDs        []uint64 `json:"module_ids"`
	ExtraDownstreams []string `json:"extra_downstreams"`
	Description      string   `json:"description"`
}

// ListProcesses GET /api/processes
func ListProcesses(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	page, pageSize := parsePagination(c)
	agentIDStr := c.Query("agent_id")
	moduleIDStr := c.Query("module_id")
	search := strings.TrimSpace(c.Query("search"))

	query := models.DB.Model(&models.Process{}).Preload("Agent").Preload("ProcessModules.Module")

	// ScopeReadGuard: 普通用户仅能看到挂载在 SHARED Agent 或本人 PRIVATE Agent 下的进程
	if !isAdmin {
		query = query.Joins("LEFT JOIN traefik_agents ON traefik_agents.id = traefik_processes.agent_id").
			Where("traefik_agents.scope = ? OR (traefik_agents.scope = ? AND traefik_agents.owner_id = ?)",
				models.ScopeShared, models.ScopePrivate, uc.UserID)
	}

	if agentIDStr != "" {
		if aid, err := strconv.ParseUint(agentIDStr, 10, 64); err == nil {
			query = query.Where("traefik_processes.agent_id = ?", aid)
		}
	}
	if moduleIDStr != "" {
		if mid, err := strconv.ParseUint(moduleIDStr, 10, 64); err == nil {
			query = query.Joins("JOIN traefik_process_modules ON traefik_process_modules.process_id = traefik_processes.id").
				Where("traefik_process_modules.module_id = ?", mid)
		}
	}
	if search != "" {
		s := "%" + search + "%"
		query = query.Where("traefik_processes.name ILIKE ?", s)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "统计进程映射失败"})
		return
	}

	items := make([]models.Process, 0)
	offset := (page - 1) * pageSize
	if err := query.Order("traefik_processes.id ASC").Offset(offset).Limit(pageSize).Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询进程映射失败"})
		return
	}

	for i := range items {
		if items[i].Agent != nil && items[i].Agent.Owner != nil {
			items[i].Agent.Owner.Email = ""
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetProcess GET /api/processes/:id
func GetProcess(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的进程 ID"})
		return
	}

	var proc models.Process
	if err := models.DB.Preload("Agent").Preload("ProcessModules.Module").First(&proc, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "进程映射不存在"})
		return
	}

	// ScopeReadGuard
	if !isAdmin && proc.Agent != nil && proc.Agent.Scope == models.ScopePrivate && (proc.Agent.OwnerID == nil || *proc.Agent.OwnerID != uc.UserID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权访问他人的私有进程"})
		return
	}

	c.JSON(http.StatusOK, proc)
}

// CreateProcess POST /api/processes
func CreateProcess(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	var req ProcessReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
		return
	}

	// 权限核验：检查目标 Agent 是否为本人 PRIVATE 或 SHARED(仅管理员可写)
	var targetAgent models.Agent
	if err := models.DB.First(&targetAgent, req.AgentID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "所属 Agent 不存在"})
		return
	}

	if targetAgent.Scope == models.ScopeShared && !isAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "普通用户无权在公共 SHARED Agent 上创建进程"})
		return
	}
	if targetAgent.Scope == models.ScopePrivate && !isAdmin && (targetAgent.OwnerID == nil || *targetAgent.OwnerID != uc.UserID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权在他人私有 Agent 上创建进程"})
		return
	}

	proc, err := topology.CreateProcess(models.DB, topology.ProcessInput{
		AgentID:          req.AgentID,
		Name:             req.Name,
		ListenPort:       req.ListenPort,
		AdvertiseIP:      req.AdvertiseIP,
		CmdPattern:       req.CmdPattern,
		ModuleIDs:        req.ModuleIDs,
		ExtraDownstreams: req.ExtraDownstreams,
		Description:      req.Description,
		OperatorID:       uc.UserID,
	})
	if err != nil {
		if errors.Is(err, topology.ErrDuplicateProcessPort) || errors.Is(err, topology.ErrDuplicateProcessName) ||
			errors.Is(err, topology.ErrDuplicateModuleBinding) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrAgentNotFound) || errors.Is(err, topology.ErrModuleNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrInvalidListenPort) || errors.Is(err, topology.ErrInvalidProcessName) ||
			errors.Is(err, topology.ErrEmptyProcessModules) || errors.Is(err, topology.ErrInvalidExtraDownstream) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrAgentHasActiveLease) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "create", commonModels.AuditLevelP2, fmt.Sprintf("创建进程映射 [%s]", proc.Name), "process", proc.ID, proc.Name, nil, proc)
	c.JSON(http.StatusCreated, proc)
}

// UpdateProcess PUT /api/processes/:id
func UpdateProcess(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的进程 ID"})
		return
	}

	var req ProcessReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
		return
	}

	var existing models.Process
	if err := models.DB.Preload("Agent").First(&existing, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "进程映射不存在"})
		return
	}
	if existing.Agent != nil {
		if existing.Agent.Scope == models.ScopeShared && !isAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "普通用户无权修改公共 SHARED 进程"})
			return
		}
		if existing.Agent.Scope == models.ScopePrivate && !isAdmin && (existing.Agent.OwnerID == nil || *existing.Agent.OwnerID != uc.UserID) {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权修改他人私有进程"})
			return
		}
	}

	before, after, err := topology.UpdateProcess(models.DB, id, topology.ProcessInput{
		AgentID:          existing.AgentID,
		Name:             req.Name,
		ListenPort:       req.ListenPort,
		AdvertiseIP:      req.AdvertiseIP,
		CmdPattern:       req.CmdPattern,
		ModuleIDs:        req.ModuleIDs,
		ExtraDownstreams: req.ExtraDownstreams,
		Description:      req.Description,
		OperatorID:       uc.UserID,
	})
	if err != nil {
		if errors.Is(err, topology.ErrProcessNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrDuplicateProcessPort) || errors.Is(err, topology.ErrDuplicateProcessName) ||
			errors.Is(err, topology.ErrDuplicateModuleBinding) || errors.Is(err, topology.ErrAgentHasActiveLease) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrInvalidListenPort) || errors.Is(err, topology.ErrInvalidProcessName) ||
			errors.Is(err, topology.ErrEmptyProcessModules) || errors.Is(err, topology.ErrInvalidExtraDownstream) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "update", commonModels.AuditLevelP2, fmt.Sprintf("更新进程映射 [%s]", after.Name), "process", after.ID, after.Name, before, after)
	c.JSON(http.StatusOK, after)
}

// DeleteProcess DELETE /api/processes/:id
func DeleteProcess(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的进程 ID"})
		return
	}

	var existing models.Process
	if err := models.DB.Preload("Agent").First(&existing, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "进程映射不存在"})
		return
	}
	if existing.Agent != nil {
		if existing.Agent.Scope == models.ScopeShared && !isAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "普通用户无权删除公共 SHARED 进程"})
			return
		}
		if existing.Agent.Scope == models.ScopePrivate && !isAdmin && (existing.Agent.OwnerID == nil || *existing.Agent.OwnerID != uc.UserID) {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权删除他人私有进程"})
			return
		}
	}

	deleted, err := topology.DeleteProcess(models.DB, id)
	if err != nil {
		if errors.Is(err, topology.ErrProcessNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, topology.ErrAgentHasActiveLease) || errors.Is(err, topology.ErrProcessReferencedByReplay) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "delete", commonModels.AuditLevelP1, fmt.Sprintf("删除进程映射 [%s]", deleted.Name), "process", deleted.ID, deleted.Name, deleted, nil)
	c.JSON(http.StatusOK, gin.H{"message": "进程映射已删除"})
}

// GetProcessDownstreams GET /api/processes/:id/downstreams
func GetProcessDownstreams(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的进程 ID"})
		return
	}

	downstreams, err := topology.CompileProcessDownstreams(models.DB, id, nil, nil)
	if err != nil {
		if errors.Is(err, topology.ErrProcessNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"downstreams": downstreams})
}
