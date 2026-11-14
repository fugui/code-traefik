package hub

import (
	"sync"
)

// EventBroadcaster 通用与按用户定向 SSE 推送接口（解耦 internal/server/* 与 handlers）
type EventBroadcaster interface {
	Broadcast(eventType string, data any)
	BroadcastToUsers(userIDs []uint, eventType string, data any)
}

type sseClient struct {
	id     uint64
	userID uint
	ch     chan UserSSEEvent
}

// UserSSEEvent 标准化 SSE 事件结构体
type UserSSEEvent struct {
	Event            string
	Data             any
	SessionID        uint64
	TaskID           uint64
	RecipientUserIDs []uint
}

// SSEBroker 负责管理按 userID 绑定的 SSE 订阅客户端与定向/广播投递
type SSEBroker struct {
	mu      sync.RWMutex
	nextID  uint64
	clients map[uint64]*sseClient
}

var (
	globalBroker   *SSEBroker
	globalBrokerMu sync.RWMutex
)

// NewSSEBroker 创建新的 SSEBroker
func NewSSEBroker() *SSEBroker {
	return &SSEBroker{
		clients: make(map[uint64]*sseClient),
	}
}

// GetGlobalSSEBroker 获取全局 SSEBroker 单例
func GetGlobalSSEBroker() *SSEBroker {
	globalBrokerMu.RLock()
	b := globalBroker
	globalBrokerMu.RUnlock()
	if b != nil {
		return b
	}
	globalBrokerMu.Lock()
	defer globalBrokerMu.Unlock()
	if globalBroker == nil {
		globalBroker = NewSSEBroker()
	}
	return globalBroker
}

// SetGlobalSSEBroker 注入或替换全局 SSEBroker
func SetGlobalSSEBroker(b *SSEBroker) {
	globalBrokerMu.Lock()
	defer globalBrokerMu.Unlock()
	globalBroker = b
}

// Subscribe 注册匿名/系统级订阅客户端 (userID = 0)
func (b *SSEBroker) Subscribe() (uint64, <-chan UserSSEEvent) {
	return b.SubscribeForUser(0)
}

// SubscribeForUser 注册绑定指定认证用户 userID 的订阅客户端
func (b *SSEBroker) SubscribeForUser(userID uint) (uint64, <-chan UserSSEEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	id := b.nextID
	ch := make(chan UserSSEEvent, 128)
	b.clients[id] = &sseClient{
		id:     id,
		userID: userID,
		ch:     ch,
	}
	return id, ch
}

// Unsubscribe 注销订阅客户端并安全关闭通道
func (b *SSEBroker) Unsubscribe(id uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if cli, ok := b.clients[id]; ok {
		delete(b.clients, id)
		close(cli.ch)
	}
}

// Dispatch 分发单条事件：若指定了 RecipientUserIDs 则严格按 userID 白名单定向投递，否则广播给全体在线客户端
func (b *SSEBroker) Dispatch(evt UserSSEEvent) {
	if b == nil {
		return
	}
	var targetUsers map[uint]struct{}
	if len(evt.RecipientUserIDs) > 0 {
		targetUsers = make(map[uint]struct{}, len(evt.RecipientUserIDs))
		for _, uid := range evt.RecipientUserIDs {
			if uid > 0 {
				targetUsers[uid] = struct{}{}
			}
		}
		if len(targetUsers) == 0 {
			return
		}
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, cli := range b.clients {
		if targetUsers != nil {
			if _, ok := targetUsers[cli.userID]; !ok {
				continue
			}
		}
		select {
		case cli.ch <- evt:
		default:
			// 慢客户端丢弃单帧，不阻塞核心控制面
		}
	}
}

// Broadcast 向全体在线客户端广播公共事件
func (b *SSEBroker) Broadcast(eventType string, data any) {
	b.Dispatch(UserSSEEvent{
		Event: eventType,
		Data:  data,
	})
}

// BroadcastToUsers 严格仅向 userID 匹配的在线客户端定向推送个人事件（如 notification.created，杜绝跨用户收件箱泄露）
func (b *SSEBroker) BroadcastToUsers(userIDs []uint, eventType string, data any) {
	if len(userIDs) == 0 {
		return
	}
	b.Dispatch(UserSSEEvent{
		Event:            eventType,
		Data:             data,
		RecipientUserIDs: userIDs,
	})
}
