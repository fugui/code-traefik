package recorder

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"code-traefik/models"
	"code-traefik/pkg/agentproto"
	"code-traefik/pkg/crosrpc"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// DestinationResolver 模块目的端唯一定位接口
type DestinationResolver interface {
	ResolveDestination(serviceName, operation string) *uint64
}

// SSEPreviewBroadcaster 快轨 SSE 实时预览广播器
type SSEPreviewBroadcaster interface {
	BroadcastPreview(sessionID, processID uint64, direction string, sno uint64, serviceName, operation string, capturedAtNs int64)
	BroadcastPersisted(sessionID, recordID uint64, sno uint64, exchangeType string, latencyUs int64, statusCode uint64, decodeStatus string)
}

// LimitReachedCallback 达到上限自动停录回调
type LimitReachedCallback func(sessionID uint64, reason string)

// AssemblerConfig 汇聚器配置
type AssemblerConfig struct {
	WorkerShards    int
	BatchSize       int
	FlushInterval   time.Duration
	DefaultTimeout  time.Duration
	MaxPendingLimit int
}

type envelopeFrame struct {
	header  agentproto.EnvelopeHeader
	payload []byte
}

type pendingExchange struct {
	sessionID       uint64
	processID       uint64
	capturedPID     uint32
	agentID         uint64
	connKey         string
	direction       string
	srcAddr         string
	dstAddr         string
	transportProto  uint8
	sno             uint64
	reqType         uint8
	isPacked        uint8
	serviceName     string
	operation       string
	traceID         string
	spanID          string
	fatherSpanID    string
	headersJSON     map[string]interface{}
	reqCapturedAtNs int64
	timeoutAt       time.Time
	rawReqPacket    []byte
	rawRspSegments  [][]byte
	rspSegCount     int
	isTruncated     bool
	schemaID        *uint64
	schemaVersion   *int
	decodeStatus    string
	reqJSON         interface{}
	reqSHA256       string
}

// ExchangeAssembler 连接哈希分片多协程往返聚合与双模落盘器
type ExchangeAssembler struct {
	db             *gorm.DB
	broadcaster    SSEPreviewBroadcaster
	dstResolver    DestinationResolver
	onLimitReached LimitReachedCallback

	cfg           AssemblerConfig
	workers       []*assemblerWorker
	totalCaptured int64
	sessionLimits sync.Map // sessionID (uint64) -> int64 (maxFrames)
	sessionCounts sync.Map // sessionID (uint64) -> *int64 (capturedCount)

	flushQueue chan *models.TrafficRecord
	flushReq   chan chan struct{}
	ctx        context.Context
	cancel     context.CancelFunc
}

type assemblerWorker struct {
	id        int
	parent    *ExchangeAssembler
	ch        chan *envelopeFrame
	pending   map[string]*pendingExchange // key: connKey + "#" + SNO
	pendingMu sync.Mutex
	stopCh    chan struct{}
}

// NewExchangeAssembler 构建 ExchangeAssembler
func NewExchangeAssembler(
	db *gorm.DB,
	broadcaster SSEPreviewBroadcaster,
	dstResolver DestinationResolver,
	onLimitReached LimitReachedCallback,
	cfg *AssemblerConfig,
) *ExchangeAssembler {
	if cfg == nil {
		cfg = &AssemblerConfig{}
	}
	if cfg.WorkerShards <= 0 {
		cfg.WorkerShards = runtime.NumCPU()
		if cfg.WorkerShards < 2 {
			cfg.WorkerShards = 2
		}
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 200
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 200 * time.Millisecond
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 30 * time.Second
	}
	if cfg.MaxPendingLimit <= 0 {
		cfg.MaxPendingLimit = 10000
	}

	ctx, cancel := context.WithCancel(context.Background())
	a := &ExchangeAssembler{
		db:             db,
		broadcaster:    broadcaster,
		dstResolver:    dstResolver,
		onLimitReached: onLimitReached,
		cfg:            *cfg,
		flushQueue:     make(chan *models.TrafficRecord, cfg.BatchSize*20),
		flushReq:       make(chan chan struct{}, 4),
		ctx:            ctx,
		cancel:         cancel,
	}

	a.workers = make([]*assemblerWorker, cfg.WorkerShards)
	for i := 0; i < cfg.WorkerShards; i++ {
		w := &assemblerWorker{
			id:      i,
			parent:  a,
			ch:      make(chan *envelopeFrame, 2048),
			pending: make(map[string]*pendingExchange),
			stopCh:  make(chan struct{}),
		}
		a.workers[i] = w
		go w.run()
	}

	go a.batchWriterLoop()
	return a
}

// SetSessionMaxFrames 设置录制批次的帧数阈值
func (a *ExchangeAssembler) SetSessionMaxFrames(sessionID uint64, maxFrames int64) {
	if maxFrames > 0 {
		a.sessionLimits.Store(sessionID, maxFrames)
		var cnt int64
		a.sessionCounts.Store(sessionID, &cnt)
	}
}

// RemoveSessionLimit 移除会话阈值监控
func (a *ExchangeAssembler) RemoveSessionLimit(sessionID uint64) {
	a.sessionLimits.Delete(sessionID)
	a.sessionCounts.Delete(sessionID)
}

func calcConnKey(srcIP net.IP, srcPort uint16, dstIP net.IP, dstPort uint16, connSeq uint32) string {
	ep1 := fmt.Sprintf("%s:%d", srcIP.String(), srcPort)
	ep2 := fmt.Sprintf("%s:%d", dstIP.String(), dstPort)
	if ep1 > ep2 {
		ep1, ep2 = ep2, ep1
	}
	return fmt.Sprintf("%s<->%s#%d", ep1, ep2, connSeq)
}

func (a *ExchangeAssembler) getWorker(sessionID, processID uint64, connKey string) *assemblerWorker {
	h := fnv.New64a()
	var b [16]byte
	binary.BigEndian.PutUint64(b[0:8], sessionID)
	binary.BigEndian.PutUint64(b[8:16], processID)
	h.Write(b[:])
	h.Write([]byte(connKey))
	idx := h.Sum64() % uint64(len(a.workers))
	return a.workers[idx]
}

// Feed 投递由 Agent 上报的 56B 信封与变长载荷帧
func (a *ExchangeAssembler) Feed(env agentproto.EnvelopeHeader, frame []byte) {
	connKey := calcConnKey(env.SrcIP(), env.SrcPort, env.DstIP(), env.DstPort, env.ConnSeq)
	w := a.getWorker(env.SessionOrTaskID, env.ProcessID, connKey)
	select {
	case w.ch <- &envelopeFrame{header: env, payload: frame}:
	default:
	}
}

// FeedConnClosed 投递连接关闭事件，刷空挂起请求为 REQ_ONLY
func (a *ExchangeAssembler) FeedConnClosed(env agentproto.EnvelopeHeader) {
	connKey := calcConnKey(env.SrcIP(), env.SrcPort, env.DstIP(), env.DstPort, env.ConnSeq)
	w := a.getWorker(env.SessionOrTaskID, env.ProcessID, connKey)
	env.EventType = agentproto.EventConnClosed
	select {
	case w.ch <- &envelopeFrame{header: env, payload: nil}:
	default:
	}
}

func (w *assemblerWorker) run() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.cleanExpiredPending()
		case item := <-w.ch:
			if item.header.EventType == agentproto.EventConnClosed {
				w.handleConnClosed(item.header)
			} else {
				w.processFrame(item.header, item.payload)
			}
		}
	}
}

func (w *assemblerWorker) processFrame(env agentproto.EnvelopeHeader, frame []byte) {
	connKey := calcConnKey(env.SrcIP(), env.SrcPort, env.DstIP(), env.DstPort, env.ConnSeq)
	dirStr := models.TrafficDirIngress
	if env.Direction == 2 {
		dirStr = models.TrafficDirEgress
	}

	// 1. 尝试解析 CROS RPC 报文
	parsed, err := crosrpc.ParsePacket(frame)
	isTruncated := env.IsTruncated()
	var comm crosrpc.CommHead
	if err == nil && parsed != nil {
		comm = parsed.Comm
		if parsed.IsTruncated {
			isTruncated = true
		}
	} else {
		if ch, parseErr := crosrpc.ParseCommHead(frame); parseErr == nil {
			comm = ch
		} else {
			return
		}
	}

	// 2. Track A 快轨实时预览通知 (非阻塞)
	svcName := ""
	opName := ""
	if parsed != nil {
		if parsed.Req != nil {
			svcName = parsed.Req.ServiceName
			opName = parsed.Req.Operation
		} else if parsed.Rsp != nil {
			svcName = parsed.Rsp.ServiceName
			opName = parsed.Rsp.Operation
		}
	}
	if w.parent.broadcaster != nil {
		w.parent.broadcaster.BroadcastPreview(
			env.SessionOrTaskID, env.ProcessID, dirStr, comm.SNO, svcName, opName, env.CapturedAtNs,
		)
	}

	// 3. 帧计数与双阈值上限检测
	w.parent.checkFrameLimit(env.SessionOrTaskID)

	// 4. 分流处理
	// (a) 管理帧 MsgType >= 5
	if comm.MsgType >= 5 {
		record := w.buildSingleRecord(env, comm, parsed, frame, isTruncated, models.ExchangeMng)
		w.parent.enqueueRecord(record)
		return
	}

	pendingKey := fmt.Sprintf("%d#%s#%d", env.ProcessID, connKey, comm.SNO)

	// (b) 请求帧 MsgType == 2 (REQ)
	if comm.MsgType == crosrpc.MsgTypeReq {
		isOneway := false
		if parsed != nil && parsed.Req != nil && parsed.Req.ReqType == crosrpc.ReqTypeOneway {
			isOneway = true
		}
		if isOneway {
			record := w.buildSingleRecord(env, comm, parsed, frame, isTruncated, models.ExchangeOneway)
			w.parent.enqueueRecord(record)
			return
		}

		// UNARY 或 STREAM_RSP 请求帧：暂存等待 RSP
		w.pendingMu.Lock()
		if len(w.pending) >= w.parent.cfg.MaxPendingLimit {
			for k, old := range w.pending {
				delete(w.pending, k)
				w.parent.enqueueRecord(w.pendingToRecord(old, models.ExchangeReqOnly))
				break
			}
		}

		timeoutDur := w.parent.cfg.DefaultTimeout
		if parsed != nil && parsed.Req != nil && parsed.Req.TimeOutSecondLimit != nil && *parsed.Req.TimeOutSecondLimit > 0 {
			reqTimeout := time.Duration(*parsed.Req.TimeOutSecondLimit) * time.Second
			if reqTimeout > timeoutDur {
				timeoutDur = reqTimeout
			}
		}

		headers := make(map[string]interface{})
		reqSHA := ""
		traceID, spanID, fatherSpanID := "", "", ""
		if parsed != nil {
			if parsed.Trace != nil {
				traceID = fmt.Sprintf("%d", parsed.Trace.TraceID)
				spanID = fmt.Sprintf("%d", parsed.Trace.SpanID)
				fatherSpanID = fmt.Sprintf("%d", parsed.Trace.FatherSpanID)
			}
			if len(parsed.ExtendHeads) > 0 {
				headers["extend_heads_count"] = len(parsed.ExtendHeads)
			}
			payloadBytes := frame[parsed.PayloadOffset:]
			if len(payloadBytes) > 0 {
				hash := sha256.Sum256(payloadBytes)
				reqSHA = hex.EncodeToString(hash[:])
			}
		}

		pendingItem := &pendingExchange{
			sessionID:       env.SessionOrTaskID,
			processID:       env.ProcessID,
			capturedPID:     env.CapturedPID,
			agentID:         1,
			connKey:         connKey,
			direction:       dirStr,
			srcAddr:         fmt.Sprintf("%s:%d", env.SrcIP().String(), env.SrcPort),
			dstAddr:         fmt.Sprintf("%s:%d", env.DstIP().String(), env.DstPort),
			transportProto:  comm.TransportProto,
			sno:             comm.SNO,
			reqType:         comm.MsgType,
			isPacked:        comm.IsPacked,
			serviceName:     svcName,
			operation:       opName,
			traceID:         traceID,
			spanID:          spanID,
			fatherSpanID:    fatherSpanID,
			headersJSON:     headers,
			reqCapturedAtNs: env.CapturedAtNs,
			timeoutAt:       time.Now().Add(timeoutDur),
			rawReqPacket:    frame,
			isTruncated:     isTruncated,
			reqSHA256:       reqSHA,
		}
		w.pending[pendingKey] = pendingItem
		w.pendingMu.Unlock()
		return
	}

	// (c) 应答分片帧 MsgType == 4 (RSP_SEG)
	if comm.MsgType == crosrpc.MsgTypeRspSeg {
		w.pendingMu.Lock()
		if item, ok := w.pending[pendingKey]; ok {
			item.rawRspSegments = append(item.rawRspSegments, frame)
			item.rspSegCount++
			if isTruncated {
				item.isTruncated = true
			}
		}
		w.pendingMu.Unlock()
		return
	}

	// (d) 应答终结帧 MsgType == 3 (RSP)
	if comm.MsgType == crosrpc.MsgTypeRsp {
		w.pendingMu.Lock()
		item, ok := w.pending[pendingKey]
		if ok {
			delete(w.pending, pendingKey)
		}
		w.pendingMu.Unlock()

		if !ok {
			// 孤立应答 RSP_ORPHAN
			record := w.buildSingleRecord(env, comm, parsed, frame, isTruncated, models.ExchangeRspOrphan)
			w.parent.enqueueRecord(record)
			return
		}

		// 往返配对成功 (UNARY 或 STREAM_RSP)
		record := w.assembleMatchedRecord(item, env, comm, parsed, frame, isTruncated)
		w.parent.enqueueRecord(record)
		return
	}
}

func (w *assemblerWorker) handleConnClosed(env agentproto.EnvelopeHeader) {
	connKey := calcConnKey(env.SrcIP(), env.SrcPort, env.DstIP(), env.DstPort, env.ConnSeq)
	prefix := fmt.Sprintf("%d#%s#", env.ProcessID, connKey)

	w.pendingMu.Lock()
	defer w.pendingMu.Unlock()

	for k, item := range w.pending {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			delete(w.pending, k)
			record := w.pendingToRecord(item, models.ExchangeReqOnly)
			w.parent.enqueueRecord(record)
		}
	}
}

func (w *assemblerWorker) cleanExpiredPending() {
	now := time.Now()
	w.pendingMu.Lock()
	defer w.pendingMu.Unlock()

	for k, item := range w.pending {
		if now.After(item.timeoutAt) {
			delete(w.pending, k)
			record := w.pendingToRecord(item, models.ExchangeReqOnly)
			w.parent.enqueueRecord(record)
		}
	}
}

func (w *assemblerWorker) buildSingleRecord(
	env agentproto.EnvelopeHeader,
	comm crosrpc.CommHead,
	parsed *crosrpc.ParsedPacket,
	frame []byte,
	isTruncated bool,
	exType string,
) *models.TrafficRecord {
	dirStr := models.TrafficDirIngress
	if env.Direction == 2 {
		dirStr = models.TrafficDirEgress
	}
	connKey := calcConnKey(env.SrcIP(), env.SrcPort, env.DstIP(), env.DstPort, env.ConnSeq)

	rec := &models.TrafficRecord{
		CreatedAt:       time.Now(),
		SessionID:       env.SessionOrTaskID,
		AgentID:         1,
		ProcessID:       env.ProcessID,
		CapturedPID:     env.CapturedPID,
		ConnKey:         connKey,
		Direction:       dirStr,
		ExchangeType:    exType,
		SrcAddr:         fmt.Sprintf("%s:%d", env.SrcIP().String(), env.SrcPort),
		DstAddr:         fmt.Sprintf("%s:%d", env.DstIP().String(), env.DstPort),
		ReqCapturedAtNs: env.CapturedAtNs,
		TransportProto:  comm.TransportProto,
		SNO:             models.U64Numeric(comm.SNO),
		IsPacked:        comm.IsPacked,
		IsTruncated:     isTruncated,
		IsReplayable:    !isTruncated && exType != models.ExchangeReqOnly && exType != models.ExchangeRspOrphan && exType != models.ExchangeMng,
	}

	payloadMap := make(map[string]interface{})
	if exType == models.ExchangeOneway {
		rec.RawReqPacket = frame
		payloadMap["req"] = nil
		payloadMap["rsp"] = nil
	} else if exType == models.ExchangeRspOrphan {
		rec.RawRspPacket = frame
		rec.RspCapturedAtNs = &env.CapturedAtNs
		payloadMap["req"] = nil
		payloadMap["rsp"] = nil
	} else if exType == models.ExchangeMng {
		rec.RawReqPacket = frame
	}

	headers := make(map[string]interface{})
	if parsed != nil {
		if parsed.Req != nil {
			rec.ServiceName = parsed.Req.ServiceName
			rec.Operation = parsed.Req.Operation
			rec.ReqType = parsed.Req.ReqType
		} else if parsed.Rsp != nil {
			rec.ServiceName = parsed.Rsp.ServiceName
			rec.Operation = parsed.Rsp.Operation
			rec.StatusCode = models.U64Numeric(parsed.Rsp.StatusCode)
		}
		if parsed.Trace != nil {
			rec.TraceID = fmt.Sprintf("%d", parsed.Trace.TraceID)
			rec.SpanID = fmt.Sprintf("%d", parsed.Trace.SpanID)
			rec.FatherSpanID = fmt.Sprintf("%d", parsed.Trace.FatherSpanID)
		}
		if len(parsed.ExtendHeads) > 0 {
			headers["extend_heads_count"] = len(parsed.ExtendHeads)
		}
		payloadBytes := frame[parsed.PayloadOffset:]
		if len(payloadBytes) > 0 && exType == models.ExchangeOneway {
			hash := sha256.Sum256(payloadBytes)
			rec.ReqPayloadSHA256 = hex.EncodeToString(hash[:])
		}
	}
	hdrJSON, _ := json.Marshal(headers)
	rec.HeadersJSON = datatypes.JSON(hdrJSON)

	pBytes, _ := json.Marshal(payloadMap)
	rec.PayloadJSON = datatypes.JSON(pBytes)

	if w.parent.dstResolver != nil && rec.ServiceName != "" {
		rec.DstModuleID = w.parent.dstResolver.ResolveDestination(rec.ServiceName, rec.Operation)
	}

	return rec
}

func (w *assemblerWorker) pendingToRecord(item *pendingExchange, exType string) *models.TrafficRecord {
	rec := &models.TrafficRecord{
		CreatedAt:        time.Now(),
		SessionID:        item.sessionID,
		AgentID:          item.agentID,
		ProcessID:        item.processID,
		CapturedPID:      item.capturedPID,
		ConnKey:          item.connKey,
		Direction:        item.direction,
		ExchangeType:     exType,
		SrcAddr:          item.srcAddr,
		DstAddr:          item.dstAddr,
		ReqCapturedAtNs:  item.reqCapturedAtNs,
		TransportProto:   item.transportProto,
		SNO:              models.U64Numeric(item.sno),
		ReqType:          item.reqType,
		IsPacked:         item.isPacked,
		ServiceName:      item.serviceName,
		Operation:        item.operation,
		TraceID:          item.traceID,
		SpanID:           item.spanID,
		FatherSpanID:     item.fatherSpanID,
		RawReqPacket:     item.rawReqPacket,
		ReqPayloadSHA256: item.reqSHA256,
		IsTruncated:      item.isTruncated,
		IsReplayable:     false,
	}

	hdrBytes, _ := json.Marshal(item.headersJSON)
	rec.HeadersJSON = datatypes.JSON(hdrBytes)

	payloadMap := map[string]interface{}{
		"req": item.reqJSON,
		"rsp": nil,
	}
	pBytes, _ := json.Marshal(payloadMap)
	rec.PayloadJSON = datatypes.JSON(pBytes)

	if w.parent.dstResolver != nil && rec.ServiceName != "" {
		rec.DstModuleID = w.parent.dstResolver.ResolveDestination(rec.ServiceName, rec.Operation)
	}
	return rec
}

func (w *assemblerWorker) assembleMatchedRecord(
	item *pendingExchange,
	env agentproto.EnvelopeHeader,
	comm crosrpc.CommHead,
	parsedRsp *crosrpc.ParsedPacket,
	frame []byte,
	isTruncated bool,
) *models.TrafficRecord {
	exType := models.ExchangeUnary
	rspSegCount := 1
	var rawRsp []byte
	if len(item.rawRspSegments) > 0 {
		exType = models.ExchangeStreamRsp
		for _, seg := range item.rawRspSegments {
			rawRsp = append(rawRsp, seg...)
		}
		rspSegCount = len(item.rawRspSegments) + 1
	}
	rawRsp = append(rawRsp, frame...)

	rspNs := env.CapturedAtNs
	latencyUs := int64(0)
	if rspNs > item.reqCapturedAtNs {
		latencyUs = (rspNs - item.reqCapturedAtNs) / 1000
	}

	hasTruncated := item.isTruncated || isTruncated
	rec := &models.TrafficRecord{
		CreatedAt:        time.Now(),
		SessionID:        item.sessionID,
		AgentID:          item.agentID,
		ProcessID:        item.processID,
		CapturedPID:      item.capturedPID,
		ConnKey:          item.connKey,
		Direction:        item.direction,
		ExchangeType:     exType,
		SrcAddr:          item.srcAddr,
		DstAddr:          item.dstAddr,
		ReqCapturedAtNs:  item.reqCapturedAtNs,
		RspCapturedAtNs:  &rspNs,
		LatencyUs:        &latencyUs,
		TransportProto:   item.transportProto,
		SNO:              models.U64Numeric(item.sno),
		ReqType:          item.reqType,
		RspSegCount:      rspSegCount,
		IsPacked:         item.isPacked,
		ServiceName:      item.serviceName,
		Operation:        item.operation,
		TraceID:          item.traceID,
		SpanID:           item.spanID,
		FatherSpanID:     item.fatherSpanID,
		RawReqPacket:     item.rawReqPacket,
		RawRspPacket:     rawRsp,
		ReqPayloadSHA256: item.reqSHA256,
		IsTruncated:      hasTruncated,
		IsReplayable:     !hasTruncated,
	}

	if parsedRsp != nil && parsedRsp.Rsp != nil {
		rec.StatusCode = models.U64Numeric(parsedRsp.Rsp.StatusCode)
		if rec.ServiceName == "" {
			rec.ServiceName = parsedRsp.Rsp.ServiceName
		}
		if rec.Operation == "" {
			rec.Operation = parsedRsp.Rsp.Operation
		}
	}

	hdrBytes, _ := json.Marshal(item.headersJSON)
	rec.HeadersJSON = datatypes.JSON(hdrBytes)

	payloadMap := map[string]interface{}{
		"req": item.reqJSON,
		"rsp": nil,
	}
	pBytes, _ := json.Marshal(payloadMap)
	rec.PayloadJSON = datatypes.JSON(pBytes)

	if w.parent.dstResolver != nil && rec.ServiceName != "" {
		rec.DstModuleID = w.parent.dstResolver.ResolveDestination(rec.ServiceName, rec.Operation)
	}

	return rec
}

func (a *ExchangeAssembler) checkFrameLimit(sessionID uint64) {
	atomic.AddInt64(&a.totalCaptured, 1)
	if val, ok := a.sessionLimits.Load(sessionID); ok {
		maxFrames := val.(int64)
		if countPtr, cOk := a.sessionCounts.Load(sessionID); cOk {
			cur := atomic.AddInt64(countPtr.(*int64), 1)
			if cur == maxFrames {
				if a.onLimitReached != nil {
					a.onLimitReached(sessionID, "COMPLETED_BY_LIMIT")
				}
			}
		}
	}
}

func (a *ExchangeAssembler) enqueueRecord(record *models.TrafficRecord) {
	if record == nil {
		return
	}
	select {
	case a.flushQueue <- record:
	default:
		go func(r *models.TrafficRecord) {
			a.flushQueue <- r
		}(record)
	}
}

func (a *ExchangeAssembler) batchWriterLoop() {
	ticker := time.NewTicker(a.cfg.FlushInterval)
	defer ticker.Stop()

	var batch []*models.TrafficRecord

	flush := func() {
		if len(batch) == 0 {
			return
		}
		toWrite := batch
		batch = nil

		if a.db != nil {
			if err := a.db.CreateInBatches(toWrite, len(toWrite)).Error; err != nil {
				log.Printf("[ExchangeAssembler] 批量写入流量记录失败: %v", err)
			}
		}

		if a.broadcaster != nil {
			for _, rec := range toWrite {
				lat := int64(0)
				if rec.LatencyUs != nil {
					lat = *rec.LatencyUs
				}
				a.broadcaster.BroadcastPersisted(
					rec.SessionID, rec.ID, uint64(rec.SNO), rec.ExchangeType,
					lat, uint64(rec.StatusCode), rec.DecodeStatus,
				)
			}
		}
	}

	for {
		select {
		case <-a.ctx.Done():
			flush()
			return
		case <-ticker.C:
			flush()
		case done := <-a.flushReq:
			for {
				select {
				case rec := <-a.flushQueue:
					batch = append(batch, rec)
				default:
					goto FLUSH_NOW
				}
			}
		FLUSH_NOW:
			flush()
			close(done)
		case rec := <-a.flushQueue:
			batch = append(batch, rec)
			if len(batch) >= a.cfg.BatchSize {
				flush()
			}
		}
	}
}

// Flush 立即将所有 Worker 中的挂起请求及写队列彻底排空并同步刷盘
func (a *ExchangeAssembler) Flush() {
	time.Sleep(50 * time.Millisecond)

	done := make(chan struct{})
	select {
	case a.flushReq <- done:
		<-done
	case <-time.After(2 * time.Second):
	}
}

// DrainAllAsReqOnly 停录时将当前全部连接内未匹配的 REQ 强制作为 REQ_ONLY 刷盘
func (a *ExchangeAssembler) DrainAllAsReqOnly() {
	for _, w := range a.workers {
		w.pendingMu.Lock()
		for k, item := range w.pending {
			delete(w.pending, k)
			record := w.pendingToRecord(item, models.ExchangeReqOnly)
			a.enqueueRecord(record)
		}
		w.pendingMu.Unlock()
	}
	a.Flush()
}

// Close 关闭汇聚器
func (a *ExchangeAssembler) Close() {
	a.cancel()
	for _, w := range a.workers {
		close(w.stopCh)
	}
}
