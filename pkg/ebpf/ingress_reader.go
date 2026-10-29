package ebpf

import (
	"container/list"
	"net"
	"sync"
	"time"

	"code-traefik/pkg/agentproto"
	"code-traefik/pkg/crosrpc"
)

// DefaultGlobalFramerBudgetPool 默认 64MB 全局分帧预算池
const DefaultGlobalFramerBudgetPool = 64 * 1024 * 1024

// ConnStreamKey 连接单向流唯一标识
type ConnStreamKey struct {
	PID       uint32
	ConnSeq   uint32
	Direction uint8
}

// FramerEntry 封装单个连接单向流分帧器与 LRU 元信息
type FramerEntry struct {
	key         ConnStreamKey
	framer      *crosrpc.StreamFramer
	expectedOff uint64
	hasGap      bool
	lastActive  time.Time
	element     *list.Element
	memUsed     int
}

// GlobalFramerBudgetPool 全局分帧内存预算池 (LRU 淘汰)
type GlobalFramerBudgetPool struct {
	mu         sync.Mutex
	maxBytes   int
	usedBytes  int
	framers    map[ConnStreamKey]*FramerEntry
	lruList    *list.List
	evictCount int64
}

// NewGlobalFramerBudgetPool 创建全局预算池
func NewGlobalFramerBudgetPool(maxBytes int) *GlobalFramerBudgetPool {
	if maxBytes <= 0 {
		maxBytes = DefaultGlobalFramerBudgetPool
	}
	return &GlobalFramerBudgetPool{
		maxBytes: maxBytes,
		framers:  make(map[ConnStreamKey]*FramerEntry),
		lruList:  list.New(),
	}
}

// GetOrCreate 获取或新建连接的分帧器
func (p *GlobalFramerBudgetPool) GetOrCreate(key ConnStreamKey, maxCaptureBytes int) *FramerEntry {
	p.mu.Lock()
	defer p.mu.Unlock()

	entry, ok := p.framers[key]
	if ok {
		entry.lastActive = time.Now()
		p.lruList.MoveToFront(entry.element)
		return entry
	}

	// 预估单分帧器初始占用
	initialBytes := 8192
	for p.usedBytes+initialBytes > p.maxBytes && p.lruList.Len() > 0 {
		p.evictOldest()
	}

	entry = &FramerEntry{
		key:         key,
		framer:      crosrpc.NewStreamFramer(maxCaptureBytes),
		expectedOff: 0,
		hasGap:      false,
		lastActive:  time.Now(),
		memUsed:     initialBytes,
	}
	elem := p.lruList.PushFront(entry)
	entry.element = elem
	p.framers[key] = entry
	p.usedBytes += initialBytes
	return entry
}

// UpdateMemUsage 更新特定连接分帧器内存占用
func (p *GlobalFramerBudgetPool) UpdateMemUsage(entry *FramerEntry, delta int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.usedBytes += delta
	entry.memUsed += delta

	// 若超出上限，触发 LRU 淘汰冷连接
	for p.usedBytes > p.maxBytes && p.lruList.Len() > 0 {
		p.evictOldest()
	}
}

// Remove 显式移除已关闭连接的分帧器
func (p *GlobalFramerBudgetPool) Remove(key ConnStreamKey) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if entry, ok := p.framers[key]; ok {
		p.lruList.Remove(entry.element)
		p.usedBytes -= entry.memUsed
		delete(p.framers, key)
	}
}

// EvictCount 淘汰连接总数
func (p *GlobalFramerBudgetPool) EvictCount() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.evictCount
}

// UsedBytes 当前占用字节
func (p *GlobalFramerBudgetPool) UsedBytes() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.usedBytes
}

func (p *GlobalFramerBudgetPool) evictOldest() {
	oldest := p.lruList.Back()
	if oldest == nil {
		return
	}
	entry := oldest.Value.(*FramerEntry)
	p.lruList.Remove(oldest)
	p.usedBytes -= entry.memUsed
	delete(p.framers, entry.key)
	p.evictCount++
}

// IngressChunk 内核投递的入站切片数据
type IngressChunk struct {
	SessionID uint64
	ProcessID uint64
	PID       uint32
	Direction uint8
	SrcIP     net.IP
	DstIP     net.IP
	SrcPort   uint16
	DstPort   uint16
	ConnSeq   uint32
	StreamOff uint64
	Data      []byte
}

// ProcessChunkResult 处理入站切片得到的结果信封包列表
type ProcessChunkResult struct {
	Frames [][]byte // Marshal 后的 56B 信封 + Payload 切片
	HasGap bool
}

// GapAwareIngressReader 丢包感知的入站字节分帧重组器
type GapAwareIngressReader struct {
	pool            *GlobalFramerBudgetPool
	maxCaptureBytes int
}

// NewGapAwareIngressReader 构造阅读器
func NewGapAwareIngressReader(pool *GlobalFramerBudgetPool, maxCaptureBytes int) *GapAwareIngressReader {
	if pool == nil {
		pool = NewGlobalFramerBudgetPool(DefaultGlobalFramerBudgetPool)
	}
	if maxCaptureBytes <= 0 {
		maxCaptureBytes = crosrpc.DefaultMaxCaptureBytes
	}
	return &GapAwareIngressReader{
		pool:            pool,
		maxCaptureBytes: maxCaptureBytes,
	}
}

// FeedChunk 接收内核入站数据块，执行 Gap 诊断与分帧切片
func (r *GapAwareIngressReader) FeedChunk(chunk IngressChunk) (ProcessChunkResult, error) {
	key := ConnStreamKey{
		PID:       chunk.PID,
		ConnSeq:   chunk.ConnSeq,
		Direction: chunk.Direction,
	}
	entry := r.pool.GetOrCreate(key, r.maxCaptureBytes)

	hasGapNow := false
	// 若非初始偏移且与期望偏移不连续，判定 RingBuf 丢包断流
	if entry.expectedOff != 0 && chunk.StreamOff != entry.expectedOff {
		entry.hasGap = true
		hasGapNow = true
		// 丢包断流：重置分帧器内部缓冲，强制滑窗重对齐魔术字
		entry.framer = crosrpc.NewStreamFramer(r.maxCaptureBytes)
	}
	entry.expectedOff = chunk.StreamOff + uint64(len(chunk.Data))

	rawFrames, err := entry.framer.Feed(chunk.Data)
	if err != nil {
		return ProcessChunkResult{HasGap: hasGapNow}, err
	}

	var envelopes [][]byte
	nowNs := time.Now().UnixNano()

	for _, rf := range rawFrames {
		flags := uint8(0)
		if rf.IsTruncated {
			flags |= agentproto.FlagIsTruncated
		}
		if entry.hasGap {
			flags |= agentproto.FlagHasGap
			// 成功切出完整帧后清除流级别的 Gap 标记
			entry.hasGap = false
		}

		envHeader := agentproto.EnvelopeHeader{
			Magic:           agentproto.EnvelopeMagic,
			ProtoVer:        agentproto.EnvelopeProtoVer,
			EventType:       agentproto.EventTrafficFrame,
			SessionOrTaskID: chunk.SessionID,
			ProcessID:       chunk.ProcessID,
			CapturedPID:     chunk.PID,
			Direction:       chunk.Direction,
			Flags:           flags,
			CapturedAtNs:    nowNs,
			SrcPort:         chunk.SrcPort,
			DstPort:         chunk.DstPort,
			ConnSeq:         chunk.ConnSeq,
		}
		envHeader.SetSrcIP(chunk.SrcIP)
		envHeader.SetDstIP(chunk.DstIP)

		envelopes = append(envelopes, agentproto.MarshalEnvelope(envHeader, rf.Data))
	}

	return ProcessChunkResult{
		Frames: envelopes,
		HasGap: hasGapNow,
	}, nil
}
