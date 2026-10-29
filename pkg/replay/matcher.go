package replay

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"code-traefik/pkg/crosrpc"
)

// MatchResultType 匹配分类
type MatchResultType string

const (
	MatchHitScoped    MatchResultType = "SCOPED_HIT"
	MatchHitScopedSeq MatchResultType = "SCOPED_SEQ_HIT"
	MatchHitGlobal    MatchResultType = "GLOBAL_FALLBACK_HIT"
	MatchHitMng       MatchResultType = "MNG_HIT"
	MatchOneway       MatchResultType = "ONEWAY_HIT"
	MatchMiss         MatchResultType = "MOCK_MISS"
)

// MatchResult 匹配结果封装
type MatchResult struct {
	Result         MatchResultType
	ServiceName    string
	Operation      string
	ResponseFrames [][]byte // 自愈后的应答帧（单帧或分片），MOCK_MISS 或 Oneway 为空
	Detail         string
}

// MockMissRecord 记录 Mock 未命中事件
type MockMissRecord struct {
	ServiceName string
	Operation   string
	SNO         uint64
	PayloadHash string
}

type mockEntry struct {
	consumed  bool
	exactKey  string
	methodKey string
	rsps      [][]byte
}

type scopedPool struct {
	exactMap map[string][]*mockEntry
	seqQueue map[string][]*mockEntry
}

func newScopedPool() *scopedPool {
	return &scopedPool{
		exactMap: make(map[string][]*mockEntry),
		seqQueue: make(map[string][]*mockEntry),
	}
}

func (p *scopedPool) add(exactKey, methodKey string, rsps [][]byte) {
	entry := &mockEntry{
		exactKey:  exactKey,
		methodKey: methodKey,
		rsps:      rsps,
	}
	p.exactMap[exactKey] = append(p.exactMap[exactKey], entry)
	p.seqQueue[methodKey] = append(p.seqQueue[methodKey], entry)
}

func (p *scopedPool) popExact(exactKey string) ([][]byte, bool) {
	entries := p.exactMap[exactKey]
	for i, entry := range entries {
		if !entry.consumed {
			entry.consumed = true
			p.exactMap[exactKey] = append(entries[:i], entries[i+1:]...)
			p.cleanSeqQueue(entry.methodKey)
			return entry.rsps, true
		}
	}
	return nil, false
}

func (p *scopedPool) popSeq(methodKey string) ([][]byte, bool) {
	entries := p.seqQueue[methodKey]
	for i, entry := range entries {
		if !entry.consumed {
			entry.consumed = true
			p.seqQueue[methodKey] = append(entries[:i], entries[i+1:]...)
			p.cleanExactMap(entry.exactKey)
			return entry.rsps, true
		}
	}
	return nil, false
}

func (p *scopedPool) cleanSeqQueue(methodKey string) {
	entries := p.seqQueue[methodKey]
	cleaned := entries[:0]
	for _, entry := range entries {
		if !entry.consumed {
			cleaned = append(cleaned, entry)
		}
	}
	p.seqQueue[methodKey] = cleaned
}

func (p *scopedPool) cleanExactMap(exactKey string) {
	entries := p.exactMap[exactKey]
	cleaned := entries[:0]
	for _, entry := range entries {
		if !entry.consumed {
			cleaned = append(cleaned, entry)
		}
	}
	p.exactMap[exactKey] = cleaned
}

// ScopedMockMatcher 零 .pb 纯二进制 Mock 分层匹配器
type ScopedMockMatcher struct {
	mu             sync.Mutex
	activeRecordID uint64
	scopedMocks    map[uint64]*scopedPool
	globalFallback *scopedPool
	mngMocks       map[uint8][]byte // msgType -> raw mng packet
	mockHitCount   int
	mockMissCount  int
	missRecords    []MockMissRecord
}

// NewScopedMockMatcher 创建匹配器
func NewScopedMockMatcher() *ScopedMockMatcher {
	return &ScopedMockMatcher{
		scopedMocks:    make(map[uint64]*scopedPool),
		globalFallback: newScopedPool(),
		mngMocks:       make(map[uint8][]byte),
	}
}

// SetActiveRecordID 设定当前活跃入站请求的 record_id
func (m *ScopedMockMatcher) SetActiveRecordID(recordID uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeRecordID = recordID
}

// AddScopedMock 录入专属局部 Mock
func (m *ScopedMockMatcher) AddScopedMock(recordID uint64, service, operation string, reqPayload []byte, rsps [][]byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	pool, ok := m.scopedMocks[recordID]
	if !ok {
		pool = newScopedPool()
		m.scopedMocks[recordID] = pool
	}

	methodKey := service + ":" + operation
	hash := sha256.Sum256(reqPayload)
	exactKey := methodKey + ":" + hex.EncodeToString(hash[:])
	pool.add(exactKey, methodKey, rsps)
}

// AddGlobalFallback 录入全局兜底 Mock
func (m *ScopedMockMatcher) AddGlobalFallback(service, operation string, reqPayload []byte, rsps [][]byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	methodKey := service + ":" + operation
	hash := sha256.Sum256(reqPayload)
	exactKey := methodKey + ":" + hex.EncodeToString(hash[:])
	m.globalFallback.add(exactKey, methodKey, rsps)
}

// AddMngMock 录入管理类消息
func (m *ScopedMockMatcher) AddMngMock(msgType uint8, rawMng []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mngMocks[msgType] = rawMng
}

// MatchAndHeal 针对 liveReq 执行匹配与表驱动头部自愈
func (m *ScopedMockMatcher) MatchAndHeal(liveReq []byte) (*MatchResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(liveReq) < crosrpc.CommHeadLen {
		return nil, ErrFrameTooShort
	}

	msgType := liveReq[22]

	// 1. 管理会话 (MNG) 消息处理
	if msgType == crosrpc.MsgTypeMng || msgType == crosrpc.MsgTypeMngReq || msgType == crosrpc.MsgTypeUUIDReq {
		if rawMng, ok := m.mngMocks[msgType]; ok {
			healed := make([]byte, len(rawMng))
			copy(healed, rawMng)
			copy(healed[8:16], liveReq[8:16]) // 覆写 SNO
			m.mockHitCount++
			return &MatchResult{
				Result:         MatchHitMng,
				ResponseFrames: [][]byte{healed},
			}, nil
		}
		// 没有录制 MNG 则安全忽略
		return &MatchResult{Result: MatchHitMng}, nil
	}

	// 2. 仅对 REQ (MsgType == 2) 进行匹配
	if msgType != crosrpc.MsgTypeReq {
		return &MatchResult{Result: MatchMiss}, nil
	}

	// 提取请求类型
	var reqType uint8
	if len(liveReq) > 24+136 {
		reqType = liveReq[24+136]
	}

	// 提取 ServiceName 与 Operation
	var serviceName, operation string
	if len(liveReq) >= 24+136 {
		serviceName = string(bytes.TrimRight(liveReq[24+8:24+72], "\x00"))
		operation = string(bytes.TrimRight(liveReq[24+72:24+136], "\x00"))
	}
	methodKey := serviceName + ":" + operation

	// 单向调用 (Oneway) 仅记录, 不回包
	if reqType == crosrpc.ReqTypeOneway {
		m.mockHitCount++
		return &MatchResult{
			Result:      MatchOneway,
			ServiceName: serviceName,
			Operation:   operation,
		}, nil
	}

	// 计算 Payload SHA256
	headLen := int(liveReq[20])<<8 | int(liveReq[21])
	if headLen == 0 && len(liveReq) >= 28 {
		headLen = int(liveReq[26])<<8 | int(liveReq[27])
	}
	payloadOffset := 24 + headLen
	var payload []byte
	if len(liveReq) > payloadOffset {
		payload = liveReq[payloadOffset:]
	}
	hash := sha256.Sum256(payload)
	exactKey := methodKey + ":" + hex.EncodeToString(hash[:])

	// 3. 检查专属局部池
	var matchedRsps [][]byte
	var hitType MatchResultType

	if pool, ok := m.scopedMocks[m.activeRecordID]; ok {
		// 3.1 尝试精确哈希匹配
		if rsps, found := pool.popExact(exactKey); found {
			matchedRsps = rsps
			hitType = MatchHitScoped
		} else if rsps, found := pool.popSeq(methodKey); found {
			// 3.2 尝试同接口次序兜底
			matchedRsps = rsps
			hitType = MatchHitScopedSeq
		}
	}

	// 4. 专属池未命中，检查全局兜底池
	if matchedRsps == nil && m.globalFallback != nil {
		if rsps, found := m.globalFallback.popExact(exactKey); found {
			matchedRsps = rsps
			hitType = MatchHitGlobal
		} else if rsps, found := m.globalFallback.popSeq(methodKey); found {
			matchedRsps = rsps
			hitType = MatchHitGlobal
		}
	}

	// 5. 命中处理：自愈头部并返回
	if matchedRsps != nil {
		healed, err := HealResponseFrames(liveReq, matchedRsps)
		if err != nil {
			return nil, err
		}
		m.mockHitCount++
		return &MatchResult{
			Result:         hitType,
			ServiceName:    serviceName,
			Operation:      operation,
			ResponseFrames: healed,
		}, nil
	}

	// 6. 严禁外泄连接真实下游！记录 MOCK_MISS 并保持静默
	m.mockMissCount++
	m.missRecords = append(m.missRecords, MockMissRecord{
		ServiceName: serviceName,
		Operation:   operation,
		PayloadHash: hex.EncodeToString(hash[:]),
	})

	return &MatchResult{
		Result:      MatchMiss,
		ServiceName: serviceName,
		Operation:   operation,
	}, nil
}

// MockHitCount 命中计数
func (m *ScopedMockMatcher) MockHitCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mockHitCount
}

// MockMissCount 未命中计数
func (m *ScopedMockMatcher) MockMissCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mockMissCount
}

// MissRecords 未命中详情
func (m *ScopedMockMatcher) MissRecords() []MockMissRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := make([]MockMissRecord, len(m.missRecords))
	copy(res, m.missRecords)
	return res
}
