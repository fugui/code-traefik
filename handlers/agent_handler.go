package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	commonAuth "code-common/backend/auth"
	commonModels "code-common/backend/models"
	"code-traefik/config"
	"code-traefik/internal/server/hub"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
)

var (
	GlobalHubManager *hub.Manager
	hubOnce          sync.Once
)

// GetHubManager 获取或懒加载全局 Hub 管理器
func GetHubManager() *hub.Manager {
	if GlobalHubManager != nil {
		return GlobalHubManager
	}
	hubOnce.Do(func() {
		GlobalHubManager = hub.NewManager(models.DB, &config.AppConfig)
	})
	if GlobalHubManager == nil {
		return hub.NewManager(models.DB, &config.AppConfig)
	}
	return GlobalHubManager
}

// ResetGlobalHubManager 供测试重置全局 Hub 管理器
func ResetGlobalHubManager(m *hub.Manager) {
	GlobalHubManager = m
}

// ListAgents GET /api/agents
func ListAgents(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	page, pageSize := parsePagination(c)
	scope := strings.ToUpper(strings.TrimSpace(c.Query("scope")))
	status := strings.ToUpper(strings.TrimSpace(c.Query("status")))
	leaseState := strings.ToUpper(strings.TrimSpace(c.Query("lease_state")))
	search := strings.TrimSpace(c.Query("search"))

	query := models.DB.Model(&models.Agent{}).Preload("Owner").Preload("LeaseHolder")

	// ScopeReadGuard: 普通用户仅能查看 SHARED 和本人 PRIVATE
	if !isAdmin {
		query = query.Where("scope = ? OR (scope = ? AND owner_id = ?)", models.ScopeShared, models.ScopePrivate, uc.UserID)
	} else if scope != "" {
		query = query.Where("scope = ?", scope)
	}

	if status != "" {
		query = query.Where("status = ?", status)
	}
	if leaseState == "LEASED" {
		query = query.Where("lease_type != ''")
	} else if leaseState == "IDLE" {
		query = query.Where("lease_type = ''")
	}
	if search != "" {
		s := "%" + search + "%"
		query = query.Where("name ILIKE ? OR endpoint ILIKE ?", s, s)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "统计 Agent 失败"})
		return
	}

	items := make([]models.Agent, 0)
	offset := (page - 1) * pageSize
	if err := query.Order("id ASC").Offset(offset).Limit(pageSize).Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询 Agent 失败"})
		return
	}

	for i := range items {
		if items[i].Owner != nil {
			items[i].Owner.Email = ""
		}
		if items[i].LeaseHolder != nil {
			items[i].LeaseHolder.Email = ""
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetAgent GET /api/agents/:id
func GetAgent(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 Agent ID"})
		return
	}

	var agent models.Agent
	if err := models.DB.Preload("Owner").Preload("LeaseHolder").First(&agent, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Agent 不存在"})
		return
	}

	// ScopeReadGuard
	if !isAdmin && agent.Scope == models.ScopePrivate && (agent.OwnerID == nil || *agent.OwnerID != uc.UserID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权访问他人的私有 Agent"})
		return
	}

	if agent.Owner != nil {
		agent.Owner.Email = ""
	}
	if agent.LeaseHolder != nil {
		agent.LeaseHolder.Email = ""
	}

	procs := make([]models.Process, 0)
	_ = models.DB.Where("agent_id = ?", agent.ID).Order("listen_port ASC").Find(&procs).Error

	c.JSON(http.StatusOK, gin.H{
		"agent":     agent,
		"processes": procs,
	})
}

// RegisterAgent POST /api/agents
func RegisterAgent(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	var req hub.AgentRegisterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
		return
	}

	mgr := GetHubManager()
	agent, err := mgr.RegisterAgent(c.Request.Context(), req, uc.UserID, isAdmin)
	if err != nil {
		if errors.Is(err, hub.ErrAgentForbidden) {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, hub.ErrAgentClaimCodeMismatch) {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, hub.ErrInvalidAgentEndpoint) || errors.Is(err, hub.ErrLoopbackDisallowed) ||
			errors.Is(err, hub.ErrMetadataEndpoint) || errors.Is(err, hub.ErrAgentPortOutOfRange) ||
			errors.Is(err, hub.ErrAgentVersionMismatch) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "已存在") {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "create", commonModels.AuditLevelP2, fmt.Sprintf("注册 Agent [%s]", agent.Name), "agent", agent.ID, agent.Name, nil, agent)
	c.JSON(http.StatusCreated, agent)
}

// UpdateAgent PUT /api/agents/:id
func UpdateAgent(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 Agent ID"})
		return
	}

	var req hub.AgentUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
		return
	}

	mgr := GetHubManager()
	updated, err := mgr.UpdateAgent(c.Request.Context(), id, req, uc.UserID, isAdmin)
	if err != nil {
		if errors.Is(err, hub.ErrAgentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, hub.ErrAgentForbidden) {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, hub.ErrAgentLeaseActive) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, hub.ErrInvalidAgentEndpoint) || errors.Is(err, hub.ErrLoopbackDisallowed) ||
			errors.Is(err, hub.ErrMetadataEndpoint) || errors.Is(err, hub.ErrAgentPortOutOfRange) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "冲突") {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "update", commonModels.AuditLevelP2, fmt.Sprintf("更新 Agent [%s]", updated.Name), "agent", updated.ID, updated.Name, nil, updated)
	c.JSON(http.StatusOK, updated)
}

// DeleteAgent DELETE /api/agents/:id
func DeleteAgent(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 Agent ID"})
		return
	}

	mgr := GetHubManager()
	err = mgr.DeleteAgent(c.Request.Context(), id, uc.UserID, isAdmin)
	if err != nil {
		if errors.Is(err, hub.ErrAgentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, hub.ErrAgentForbidden) {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, hub.ErrAgentHasProcesses) || errors.Is(err, hub.ErrAgentLeaseActive) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "delete", commonModels.AuditLevelP1, fmt.Sprintf("删除 Agent [%d]", id), "agent", id, strconv.FormatUint(id, 10), nil, nil)
	c.JSON(http.StatusOK, gin.H{"message": "Agent 已移除"})
}

// HandshakeAgent POST /api/agents/:id/handshake
func HandshakeAgent(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 Agent ID"})
		return
	}

	mgr := GetHubManager()
	resp, err := mgr.HandshakeAgent(c.Request.Context(), id, uc.UserID, isAdmin)
	if err != nil {
		if errors.Is(err, hub.ErrAgentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, hub.ErrAgentForbidden) {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	var agent models.Agent
	_ = models.DB.First(&agent, id).Error
	c.JSON(http.StatusOK, gin.H{
		"agent":     agent,
		"handshake": resp,
	})
}

// DiscoverAgentProcesses GET /api/agents/:id/discover-processes
func DiscoverAgentProcesses(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 Agent ID"})
		return
	}

	mgr := GetHubManager()
	items, err := mgr.DiscoverProcesses(c.Request.Context(), id, uc.UserID, isAdmin)
	if err != nil {
		if errors.Is(err, hub.ErrAgentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, hub.ErrAgentForbidden) {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ForceUnlockAgentLease POST /api/agents/:id/lease/force-unlock (AdminOnly)
func ForceUnlockAgentLease(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 Agent ID"})
		return
	}

	var req hub.ForceUnlockReq
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Reason) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "强制解锁必须填写 reason 原因说明"})
		return
	}

	var before models.Agent
	if err := models.DB.First(&before, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Agent 不存在"})
		return
	}

	if err := hub.ForceUnlockAgentLease(c.Request.Context(), models.DB, id, uc.UserID, req.Reason); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var after models.Agent
	_ = models.DB.First(&after, id).Error

	recordAudit(c, "force_unlock_lease", commonModels.AuditLevelP1, fmt.Sprintf("强制解锁 Agent [%s] 租约: %s", after.Name, req.Reason), "agent", after.ID, after.Name, before, after)
	c.JSON(http.StatusOK, gin.H{
		"message": "排他租约已强制释放",
		"agent":   after,
	})
}
