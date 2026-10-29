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
	"code-traefik/internal/server/hub"
	"code-traefik/internal/server/recorder"
	"code-traefik/internal/server/topology"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var (
	globalOrchestrator   *recorder.Orchestrator
	globalOrchestratorMu sync.RWMutex
)

// InitGlobalOrchestrator 初始化全局录制编排器
func InitGlobalOrchestrator(o *recorder.Orchestrator) {
	globalOrchestratorMu.Lock()
	defer globalOrchestratorMu.Unlock()
	globalOrchestrator = o
}

// GetGlobalOrchestrator 获取全局录制编排器
func GetGlobalOrchestrator() *recorder.Orchestrator {
	globalOrchestratorMu.RLock()
	defer globalOrchestratorMu.RUnlock()
	return globalOrchestrator
}

// ResetGlobalOrchestrator 重置全局录制编排器
func ResetGlobalOrchestrator(o *recorder.Orchestrator) {
	globalOrchestratorMu.Lock()
	defer globalOrchestratorMu.Unlock()
	globalOrchestrator = o
}

// ListRecordingSessions 分页检索录制批次 (GET /api/recording-sessions)
func ListRecordingSessions(c *gin.Context) {
	orch := GetGlobalOrchestrator()
	if orch == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "录制编排器未就绪"})
		return
	}

	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	userID := uint(0)
	if uc != nil {
		userID = uc.UserID
	}

	var query recorder.SessionListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的查询参数: " + err.Error()})
		return
	}

	items, total, err := orch.ListSessions(c.Request.Context(), query, userID, isAdmin)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询录制批次列表失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      query.Page,
		"page_size": query.PageSize,
	})
}

// GetRecordingSession 查询单条录制批次详情 (GET /api/recording-sessions/:id)
func GetRecordingSession(c *gin.Context) {
	orch := GetGlobalOrchestrator()
	if orch == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "录制编排器未就绪"})
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的批次 ID"})
		return
	}

	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	userID := uint(0)
	if uc != nil {
		userID = uc.UserID
	}

	session, err := orch.GetSession(c.Request.Context(), id, userID, isAdmin)
	if err != nil {
		if errors.Is(err, recorder.ErrForbiddenPrivateSession) || strings.Contains(err.Error(), "403") {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, recorder.ErrSessionNotFound) || strings.Contains(err.Error(), "404") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, session)
}

// StartRecordingSession 启动协同录制批次 (POST /api/recording-sessions)
func StartRecordingSession(c *gin.Context) {
	orch := GetGlobalOrchestrator()
	if orch == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "录制编排器未就绪"})
		return
	}

	var req recorder.StartSessionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数校验失败: " + err.Error()})
		return
	}

	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	userID := uint(0)
	userName := ""
	if uc != nil {
		userID = uc.UserID
		userName = uc.Username
	}

	session, err := orch.StartSession(c.Request.Context(), req, userID, isAdmin)
	if err != nil {
		if errors.Is(err, recorder.ErrForbiddenPrivateAgent) || strings.Contains(err.Error(), "403") {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, recorder.ErrTargetProcessesEmpty) || strings.Contains(err.Error(), "400") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		var leaseErr *hub.LeaseConflictError
		if errors.As(err, &leaseErr) || strings.Contains(err.Error(), "already leased") || strings.Contains(err.Error(), "409") {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(
		c,
		"start_session",
		commonModels.AuditLevelP2,
		fmt.Sprintf("用户 %s 启动协同录制批次: %s", userName, session.Name),
		"RECORDING_SESSION",
		session.ID,
		session.Name,
		nil,
		session,
	)

	// 广播生命周期事件
	if broker := GetGlobalSSEBroker(); broker != nil {
		broker.BroadcastSessionLifecycle(session.ID, gin.H{
			"session_id": session.ID,
			"status":     session.Status,
			"event":      "SESSION_STARTED",
		})
	}

	c.JSON(http.StatusCreated, session)
}

// StopRecordingSession 停止协同录制批次 (POST /api/recording-sessions/:id/stop)
func StopRecordingSession(c *gin.Context) {
	orch := GetGlobalOrchestrator()
	if orch == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "录制编排器未就绪"})
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的批次 ID"})
		return
	}

	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	userID := uint(0)
	if uc != nil {
		userID = uc.UserID
	}

	session, err := orch.StopSession(c.Request.Context(), id, userID, isAdmin, "USER_STOP")
	if err != nil {
		if errors.Is(err, recorder.ErrForbiddenPrivateSession) || strings.Contains(err.Error(), "403") {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, recorder.ErrSessionNotFound) || strings.Contains(err.Error(), "404") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, recorder.ErrSessionAlreadyEnded) || strings.Contains(err.Error(), "409") {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(
		c,
		"stop_session",
		commonModels.AuditLevelP2,
		fmt.Sprintf("停止协同录制批次: %s", session.Name),
		"RECORDING_SESSION",
		session.ID,
		session.Name,
		nil,
		session,
	)

	if broker := GetGlobalSSEBroker(); broker != nil {
		broker.BroadcastSessionLifecycle(session.ID, gin.H{
			"session_id": session.ID,
			"status":     session.Status,
			"event":      "SESSION_STOPPED",
		})
	}

	c.JSON(http.StatusOK, session)
}

// RemoveProcessFromSession 从录制中单独热移除某进程监控 (DELETE /api/recording-sessions/:id/processes/:processId)
func RemoveProcessFromSession(c *gin.Context) {
	orch := GetGlobalOrchestrator()
	if orch == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "录制编排器未就绪"})
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的批次 ID"})
		return
	}

	processID, err := strconv.ParseUint(c.Param("processId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的进程 ID"})
		return
	}

	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	userID := uint(0)
	if uc != nil {
		userID = uc.UserID
	}

	session, err := orch.RemoveProcessFromSession(c.Request.Context(), id, processID, userID, isAdmin)
	if err != nil {
		if strings.Contains(err.Error(), "403") {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		if strings.Contains(err.Error(), "404") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if strings.Contains(err.Error(), "409") {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(
		c,
		"stop_session",
		commonModels.AuditLevelP2,
		fmt.Sprintf("从录制批次 %s 移除进程监控 [ProcessID=%d]", session.Name, processID),
		"RECORDING_SESSION",
		session.ID,
		session.Name,
		nil,
		session,
	)

	c.JSON(http.StatusOK, gin.H{
		"message": "进程监控已移除",
		"session": session,
	})
}

// PublishRecordingSession 将本人合格的 PRIVATE 录制批次发布为团队 SHARED 素材 (PATCH /api/recording-sessions/:id/publish)
func PublishRecordingSession(c *gin.Context) {
	orch := GetGlobalOrchestrator()
	if orch == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "录制编排器未就绪"})
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的批次 ID"})
		return
	}

	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	userID := uint(0)
	if uc != nil {
		userID = uc.UserID
	}

	session, err := orch.PublishSession(c.Request.Context(), id, userID, isAdmin)
	if err != nil {
		if errors.Is(err, recorder.ErrForbiddenPrivateSession) || strings.Contains(err.Error(), "403") {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, recorder.ErrSessionNotFound) || strings.Contains(err.Error(), "404") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, recorder.ErrSessionNotCompleted) || strings.Contains(err.Error(), "400") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(
		c,
		"publish_session",
		commonModels.AuditLevelP2,
		fmt.Sprintf("发布录制批次为共享素材: %s", session.Name),
		"RECORDING_SESSION",
		session.ID,
		session.Name,
		nil,
		session,
	)

	c.JSON(http.StatusOK, session)
}

// DeleteRecordingSession 删除录制批次 (DELETE /api/recording-sessions/:id)
func DeleteRecordingSession(c *gin.Context) {
	orch := GetGlobalOrchestrator()
	if orch == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "录制编排器未就绪"})
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的批次 ID"})
		return
	}

	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	userID := uint(0)
	if uc != nil {
		userID = uc.UserID
	}

	err = orch.DeleteSession(c.Request.Context(), id, userID, isAdmin)
	if err != nil {
		if errors.Is(err, recorder.ErrForbiddenPrivateSession) || strings.Contains(err.Error(), "403") {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, recorder.ErrSessionNotFound) || strings.Contains(err.Error(), "404") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, recorder.ErrSessionInUse) || strings.Contains(err.Error(), "409") {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(
		c,
		"delete_session",
		commonModels.AuditLevelP1,
		fmt.Sprintf("删除录制批次 [ID=%d]", id),
		"RECORDING_SESSION",
		id,
		"",
		nil,
		nil,
	)

	c.JSON(http.StatusOK, gin.H{"message": "录制批次已删除"})
}

// GetConsistencyReport 架构一致性静态与运行时比对报告 (GET /api/recording-sessions/:id/consistency-report)
func GetConsistencyReport(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的批次 ID"})
		return
	}

	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	userID := uint(0)
	if uc != nil {
		userID = uc.UserID
	}

	db := models.DB
	if db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "数据库未就绪"})
		return
	}

	// 校验批次可见性
	var session models.RecordingSession
	if err := db.WithContext(c.Request.Context()).Select("id", "scope", "owner_id").Where("id = ?", id).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "录制批次不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询批次失败: " + err.Error()})
		return
	}

	if session.Scope == models.ScopePrivate && session.OwnerID != userID && !isAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权访问该私有录制批次一致性报告"})
		return
	}

	refresh, _ := strconv.ParseBool(c.Query("refresh"))

	report, err := topology.VerifySessionTopology(c.Request.Context(), db, id, refresh)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成架构一致性报告失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, report)
}
