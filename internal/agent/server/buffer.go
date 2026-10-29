package server

import (
	"context"
	"sync"
	"sync/atomic"
)

// DefaultMaxSessionBufferBytes 默认会话缓冲上限 64MB
const DefaultMaxSessionBufferBytes = 64 * 1024 * 1024

// BoundedSessionBuffer 线程安全且具备 Drop-Newest 背压丢帧特性的有界会话缓冲区
type BoundedSessionBuffer struct {
	mu            sync.Mutex
	cond          *sync.Cond
	maxBytes      int64
	usedBytes     int64
	droppedFrames int64
	totalCaptured int64
	queue         [][]byte
	closed        bool
}

// NewBoundedSessionBuffer 构造有界缓冲区
func NewBoundedSessionBuffer(maxBytes int64) *BoundedSessionBuffer {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxSessionBufferBytes
	}
	b := &BoundedSessionBuffer{
		maxBytes: maxBytes,
		queue:    make([][]byte, 0, 1024),
	}
	b.cond = sync.NewCond(&b.mu)
	return b
}

// Push 向缓冲队列投递新信封帧。若容量超出，执行非阻塞丢弃新帧 (Drop-Newest) 并累加丢帧计数
func (b *BoundedSessionBuffer) Push(frame []byte) bool {
	frameLen := int64(len(frame))

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return false
	}

	// 检查是否突破内存预算上限
	if b.usedBytes+frameLen > b.maxBytes {
		atomic.AddInt64(&b.droppedFrames, 1)
		return false
	}

	b.queue = append(b.queue, frame)
	b.usedBytes += frameLen
	atomic.AddInt64(&b.totalCaptured, 1)
	b.cond.Signal()
	return true
}

// Pop 阻塞提取最早进入的一条帧。若缓冲为空且未关闭，进入休眠等待；若已关闭返回 nil, false
func (b *BoundedSessionBuffer) Pop(ctx context.Context) ([]byte, bool) {
	// 启动 context 监听协程确保能被唤醒
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			b.mu.Lock()
			b.cond.Broadcast()
			b.mu.Unlock()
		case <-done:
		}
	}()
	defer close(done)

	b.mu.Lock()
	defer b.mu.Unlock()

	for len(b.queue) == 0 && !b.closed && ctx.Err() == nil {
		b.cond.Wait()
	}

	if ctx.Err() != nil {
		return nil, false
	}

	if len(b.queue) == 0 {
		return nil, false
	}

	item := b.queue[0]
	b.queue = b.queue[1:]
	b.usedBytes -= int64(len(item))
	return item, true
}

// Drain 排空并返回当前驻留的所有切片
func (b *BoundedSessionBuffer) Drain() [][]byte {
	b.mu.Lock()
	defer b.mu.Unlock()

	res := b.queue
	b.queue = nil
	b.usedBytes = 0
	return res
}

// Close 关闭缓冲区并唤醒全部等待协程
func (b *BoundedSessionBuffer) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.closed {
		b.closed = true
		b.cond.Broadcast()
	}
}

// Reset 清空队列与计数
func (b *BoundedSessionBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.queue = b.queue[:0]
	b.usedBytes = 0
}

// Stats 返回当前已用字节、累计背压丢帧数与总捕获帧数
func (b *BoundedSessionBuffer) Stats() (usedBytes int64, droppedFrames int64, totalCaptured int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.usedBytes, atomic.LoadInt64(&b.droppedFrames), atomic.LoadInt64(&b.totalCaptured)
}
