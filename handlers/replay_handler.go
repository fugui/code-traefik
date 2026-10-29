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
	"code-traefik/internal/server/diff"
	"code-traefik/internal/server/recorder"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
)

var (
	globalReplayScheduler   *diff.ReplayScheduler
	globalReplaySchedulerMu sync.RWMutex
)

// InitGlobalReplayScheduler 初始化全局重放调度器
func InitGlobalReplayScheduler(s *diff.ReplayScheduler) {
	globalReplaySchedulerMu.Lock()
	defer globalReplaySchedulerMu.Unlock()
	globalReplayScheduler = s
}

// GetGlobalReplayScheduler 获取全局重放调度器
func GetGlobalReplayScheduler() *diff.ReplayScheduler {
	globalReplaySchedulerMu.RLock()
	defer globalReplaySchedulerMu.RUnlock()
	return globalReplayScheduler
}

// ResetGlobalReplayScheduler 重置全局重放调度器
func ResetGlobalReplayScheduler(s *diff.ReplayScheduler) {
	globalReplaySchedulerMu.Lock()
	defer globalReplaySchedulerMu.Unlock()
	globalReplayScheduler = s
}

// ListReplayTasks 分页检索重放任务 (GET /api/replay-tasks)
func ListReplayTasks(c *gin.Context) {
	sched := GetGlobalReplayScheduler()
	if sched == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "重放调度器未就绪"})
		return
	}

	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	userID := uint(0)
	if uc != nil {
		userID = uc.UserID
	}

	var query diff.ReplayTaskListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的查询参数: " + err.Error()})
		return
	}

	items, total, err := sched.ListTasks(c.Request.Context(), query, userID, isAdmin)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询重放任务失败: " + err.Error()})
		return
	}
	if items == nil {
		items = make([]models.ReplayTask, 0)
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      query.Page,
		"page_size": query.PageSize,
	})
}

// GetReplayTask 获取单条重放任务详情 (GET /api/replay-tasks/:id)
func GetReplayTask(c *gin.Context) {
	sched := GetGlobalReplayScheduler()
	if sched == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "重放调度器未就绪"})
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务 ID"})
		return
	}

	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	userID := uint(0)
	if uc != nil {
		userID = uc.UserID
	}

	task, err := sched.GetTask(c.Request.Context(), id, userID, isAdmin)
	if err != nil {
		if strings.Contains(err.Error(), "403") {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, diff.ErrTaskNotFound) || strings.Contains(err.Error(), "404") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, task)
}

// CreateReplayTask 创建并下发重放任务 (POST /api/replay-tasks)
func CreateReplayTask(c *gin.Context) {
	sched := GetGlobalReplayScheduler()
	if sched == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "重放调度器未就绪"})
		return
	}

	var req diff.CreateReplayTaskReq
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

	task, err := sched.CreateAndExecuteTask(c.Request.Context(), req, userID, isAdmin)
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
		if errors.Is(err, diff.ErrRecordNotReplay) || strings.Contains(err.Error(), "400") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(
		c,
		"execute_replay",
		commonModels.AuditLevelP2,
		fmt.Sprintf("用户 %s 创建并执行重放任务: %s", userName, task.Name),
		"REPLAY_TASK",
		task.ID,
		task.Name,
		nil,
		task,
	)

	c.JSON(http.StatusCreated, task)
}

// CancelReplayTask 取消正在执行的重放任务 (POST /api/replay-tasks/:id/cancel)
func CancelReplayTask(c *gin.Context) {
	sched := GetGlobalReplayScheduler()
	if sched == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "重放调度器未就绪"})
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务 ID"})
		return
	}

	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	userID := uint(0)
	if uc != nil {
		userID = uc.UserID
	}

	task, err := sched.CancelTask(c.Request.Context(), id, userID, isAdmin)
	if err != nil {
		if strings.Contains(err.Error(), "403") {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, diff.ErrTaskNotFound) || strings.Contains(err.Error(), "404") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, diff.ErrTaskAlreadyDone) || strings.Contains(err.Error(), "409") {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(
		c,
		"cancel_replay",
		commonModels.AuditLevelP1,
		fmt.Sprintf("取消重放任务: %s", task.Name),
		"REPLAY_TASK",
		task.ID,
		task.Name,
		nil,
		task,
	)

	c.JSON(http.StatusOK, task)
}

// ListReplayItems 分页检索重放条目 (GET /api/replay-tasks/:id/items)
func ListReplayItems(c *gin.Context) {
	sched := GetGlobalReplayScheduler()
	if sched == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "重放调度器未就绪"})
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务 ID"})
		return
	}

	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	userID := uint(0)
	if uc != nil {
		userID = uc.UserID
	}

	var query diff.ReplayItemListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的查询参数: " + err.Error()})
		return
	}

	items, total, err := sched.ListItems(c.Request.Context(), id, query, userID, isAdmin)
	if err != nil {
		if strings.Contains(err.Error(), "403") {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, diff.ErrTaskNotFound) || strings.Contains(err.Error(), "404") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if items == nil {
		items = make([]models.ReplayItem, 0)
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      query.Page,
		"page_size": query.PageSize,
	})
}

// ReplayItemDetailResponse 重放明细四段解剖响应结构
type ReplayItemDetailResponse struct {
	Item               *models.ReplayItem            `json:"item"`
	BaselineRspAnatomy *recorder.FrameSideAnatomyDTO `json:"baseline_rsp_anatomy,omitempty"`
	ActualRspAnatomy   *recorder.FrameSideAnatomyDTO `json:"actual_rsp_anatomy,omitempty"`
}

// GetReplayItemDetail 查询单条重放结果的基线与实际报文四段解剖 (GET /api/replay-tasks/:id/items/:itemId)
func GetReplayItemDetail(c *gin.Context) {
	sched := GetGlobalReplayScheduler()
	if sched == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "重放调度器未就绪"})
		return
	}

	taskID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务 ID"})
		return
	}

	itemID, err := strconv.ParseUint(c.Param("itemId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的条目 ID"})
		return
	}

	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	userID := uint(0)
	if uc != nil {
		userID = uc.UserID
	}

	item, err := sched.GetItem(c.Request.Context(), taskID, itemID, userID, isAdmin)
	if err != nil {
		if strings.Contains(err.Error(), "403") {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		if strings.Contains(err.Error(), "404") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	resp := ReplayItemDetailResponse{
		Item: item,
	}

	dec := GlobalDecoderRegistry
	db := models.DB
	if db != nil {
		var baseRec models.TrafficRecord
		if err := db.WithContext(c.Request.Context()).Where("id = ?", item.RecordID).First(&baseRec).Error; err == nil {
			if len(baseRec.RawRspPacket) > 0 {
				segCount := baseRec.RspSegCount
				if segCount <= 0 {
					segCount = 1
				}
				baseAnatomy, bErr := recorder.BuildSingleFrameSide(baseRec.RawRspPacket, baseRec.IsTruncated, baseRec.IsReplayable, true, segCount, dec)
				if bErr == nil {
					resp.BaselineRspAnatomy = baseAnatomy
				}
			}
		}
	}

	if len(item.RawActualPacket) > 0 {
		actAnatomy, aErr := recorder.BuildSingleFrameSide(item.RawActualPacket, false, false, true, 1, dec)
		if aErr == nil {
			resp.ActualRspAnatomy = actAnatomy
		}
	}

	c.JSON(http.StatusOK, resp)
}

// RediffReplayTask 离线重算双层回归 Diff (POST /api/replay-tasks/:id/rediff)
func RediffReplayTask(c *gin.Context) {
	sched := GetGlobalReplayScheduler()
	if sched == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "重放调度器未就绪"})
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务 ID"})
		return
	}

	var req struct {
		ExtraIgnorePaths []string `json:"extra_ignore_paths"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数解析失败: " + err.Error()})
		return
	}

	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	userID := uint(0)
	if uc != nil {
		userID = uc.UserID
	}

	task, err := sched.RediffTask(c.Request.Context(), id, req.ExtraIgnorePaths, userID, isAdmin)
	if err != nil {
		if strings.Contains(err.Error(), "403") {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, diff.ErrTaskNotFound) || strings.Contains(err.Error(), "404") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(
		c,
		"execute_replay",
		commonModels.AuditLevelP2,
		fmt.Sprintf("离线重算重放任务双层 Diff: %s", task.Name),
		"REPLAY_TASK",
		task.ID,
		task.Name,
		nil,
		task,
	)

	c.JSON(http.StatusOK, task)
}
