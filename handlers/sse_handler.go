package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	commonAuth "code-common/backend/auth"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SSEEvent SSE 事件载荷
type SSEEvent struct {
	Event     string      `json:"event"`
	Data      interface{} `json:"data"`
	SessionID uint64      `json:"-"`
	TaskID    uint64      `json:"-"`
}

// SSEBroker 负责管理 SSE 订阅客户端与事件广播
type SSEBroker struct {
	db          *gorm.DB
	subscribers sync.Map // uint64 -> chan SSEEvent
	subCounter  atomic.Uint64
}

var (
	globalSSEBroker   *SSEBroker
	globalSSEBrokerMu sync.RWMutex
)

// InitGlobalSSEBroker 初始化全局 SSE Broker
func InitGlobalSSEBroker(db *gorm.DB) *SSEBroker {
	globalSSEBrokerMu.Lock()
	defer globalSSEBrokerMu.Unlock()
	globalSSEBroker = &SSEBroker{
		db: db,
	}
	return globalSSEBroker
}

// GetGlobalSSEBroker 获取全局 SSE Broker
func GetGlobalSSEBroker() *SSEBroker {
	globalSSEBrokerMu.RLock()
	defer globalSSEBrokerMu.RUnlock()
	return globalSSEBroker
}

// ResetGlobalSSEBroker 测试或重置时使用
func ResetGlobalSSEBroker(b *SSEBroker) {
	globalSSEBrokerMu.Lock()
	defer globalSSEBrokerMu.Unlock()
	globalSSEBroker = b
}

// Subscribe 注册订阅客户端
func (b *SSEBroker) Subscribe() (uint64, <-chan SSEEvent) {
	id := b.subCounter.Add(1)
	ch := make(chan SSEEvent, 128)
	b.subscribers.Store(id, ch)
	return id, ch
}

// Unsubscribe 注销订阅客户端
func (b *SSEBroker) Unsubscribe(id uint64) {
	if val, ok := b.subscribers.LoadAndDelete(id); ok {
		if ch, ok := val.(chan SSEEvent); ok {
			close(ch)
		}
	}
}

// BroadcastEvent 广播事件给所有订阅客户端
func (b *SSEBroker) BroadcastEvent(evt SSEEvent) {
	b.subscribers.Range(func(key, value any) bool {
		if ch, ok := value.(chan SSEEvent); ok {
			select {
			case ch <- evt:
			default:
				// 丢弃慢客户端堆积帧，避免阻塞全局
			}
		}
		return true
	})
}

// BroadcastPreview 实现 recorder.SSEPreviewBroadcaster 接口 (Track A 快轨预览)
func (b *SSEBroker) BroadcastPreview(
	sessionID, processID uint64,
	direction string,
	sno uint64,
	serviceName, operation string,
	capturedAtNs int64,
) {
	b.BroadcastEvent(SSEEvent{
		Event:     "traffic.preview",
		SessionID: sessionID,
		Data: gin.H{
			"session_id":     sessionID,
			"process_id":     processID,
			"direction":      direction,
			"sno":            sno,
			"service_name":   serviceName,
			"operation":      operation,
			"captured_at_ns": capturedAtNs,
		},
	})
}

// BroadcastPersisted 实现 recorder.SSEPreviewBroadcaster 接口 (Track B 落盘完成通知)
func (b *SSEBroker) BroadcastPersisted(
	sessionID, recordID uint64,
	sno uint64,
	exchangeType string,
	latencyUs int64,
	statusCode uint64,
	decodeStatus string,
) {
	b.BroadcastEvent(SSEEvent{
		Event:     "traffic.persisted",
		SessionID: sessionID,
		Data: gin.H{
			"session_id":    sessionID,
			"record_id":     recordID,
			"sno":           sno,
			"exchange_type": exchangeType,
			"latency_us":    latencyUs,
			"status_code":   statusCode,
			"decode_status": decodeStatus,
		},
	})
}

// BroadcastSessionLifecycle 广播录制批次生命周期状态变动
func (b *SSEBroker) BroadcastSessionLifecycle(sessionID uint64, data interface{}) {
	b.BroadcastEvent(SSEEvent{
		Event:     "session.lifecycle",
		SessionID: sessionID,
		Data:      data,
	})
}

// BroadcastAgentLeaseChanged 广播 Agent 租约变动通知
func (b *SSEBroker) BroadcastAgentLeaseChanged(agentID uint64, data interface{}) {
	b.BroadcastEvent(SSEEvent{
		Event: "agent.lease_changed",
		Data:  data,
	})
}

// BroadcastReplayItemDone 广播单条重放请求执行断言完成
func (b *SSEBroker) BroadcastReplayItemDone(taskID, itemID, recordID uint64, resultStatus string, latencyUs int64) {
	b.BroadcastEvent(SSEEvent{
		Event:  "replay.item_done",
		TaskID: taskID,
		Data: gin.H{
			"task_id":       taskID,
			"item_id":       itemID,
			"record_id":     recordID,
			"result_status": resultStatus,
			"latency_us":    latencyUs,
		},
	})
}

// BroadcastReplayCompleted 广播重放任务整包完成汇总
func (b *SSEBroker) BroadcastReplayCompleted(
	taskID uint64,
	lifecycleState, resultStatus string,
	totalRequests, passedRequests, mockHitCount, mockMissCount int,
) {
	b.BroadcastEvent(SSEEvent{
		Event:  "replay.completed",
		TaskID: taskID,
		Data: gin.H{
			"task_id":         taskID,
			"lifecycle_state": lifecycleState,
			"result_status":   resultStatus,
			"total_requests":  totalRequests,
			"passed_requests": passedRequests,
			"mock_hit_count":  mockHitCount,
			"mock_miss_count": mockMissCount,
		},
	})
}

// HandleEventsSSE SSE 实时事件流入口 (GET /api/events)
func HandleEventsSSE(c *gin.Context) {
	broker := GetGlobalSSEBroker()
	if broker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "SSE Broker 未就绪"})
		return
	}

	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	userID := uint(0)
	if uc != nil {
		userID = uc.UserID
	}

	var filterSessionID uint64
	if sIDStr := c.Query("session_id"); sIDStr != "" {
		sID, err := strconv.ParseUint(sIDStr, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 session_id"})
			return
		}
		filterSessionID = sID

		// 公私域权限过滤校验：他人 PRIVATE 批次禁止订阅
		var session models.RecordingSession
		if err := broker.db.WithContext(c.Request.Context()).Select("id", "scope", "owner_id").Where("id = ?", filterSessionID).First(&session).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "录制批次不存在"})
			return
		}
		if session.Scope == models.ScopePrivate && session.OwnerID != userID && !isAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权订阅他人的私有录制批次事件"})
			return
		}
	}

	var filterTaskID uint64
	if tIDStr := c.Query("task_id"); tIDStr != "" {
		tID, err := strconv.ParseUint(tIDStr, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 task_id"})
			return
		}
		filterTaskID = tID

		// 公私域权限过滤校验：他人重放任务禁止订阅
		var task models.ReplayTask
		if err := broker.db.WithContext(c.Request.Context()).Select("id", "owner_id").Where("id = ?", filterTaskID).First(&task).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "重放任务不存在"})
			return
		}
		if task.OwnerID != userID && !isAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权订阅他人的重放任务事件"})
			return
		}
	}

	// 开启 SSE 响应头
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Flush()

	subID, eventCh := broker.Subscribe()
	defer broker.Unsubscribe(subID)

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			// 发送 SSE 保持心跳行
			_, _ = fmt.Fprint(c.Writer, ": keep-alive\n\n")
			c.Writer.Flush()
		case evt, ok := <-eventCh:
			if !ok {
				return
			}
			// 过滤校验
			if filterSessionID > 0 && evt.SessionID > 0 && evt.SessionID != filterSessionID {
				continue
			}
			if filterTaskID > 0 && evt.TaskID > 0 && evt.TaskID != filterTaskID {
				continue
			}

			dataBytes, err := json.Marshal(evt.Data)
			if err != nil {
				continue
			}
			_, writeErr := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", evt.Event, string(dataBytes))
			if writeErr != nil {
				return
			}
			c.Writer.Flush()
		}
	}
}
