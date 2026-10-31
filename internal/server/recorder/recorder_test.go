package recorder

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-traefik/config"
	"code-traefik/internal/server/decoder"
	"code-traefik/internal/server/hub"
	"code-traefik/models"
	"code-traefik/pkg/agentproto"
	"code-traefik/pkg/crosrpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func setupIsolatedRecorderDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}
	return db
}

// buildCrosReqFrame 辅助函数：构造 CROS RPC REQ 帧
func buildCrosReqFrame(sno uint64, reqType uint8, svc, op string, payload []byte) []byte {
	sessionHead := make([]byte, 160)
	binary.BigEndian.PutUint16(sessionHead[0:2], 1)
	binary.BigEndian.PutUint16(sessionHead[2:4], 160)
	copy(sessionHead[8:72], []byte(svc))
	copy(sessionHead[72:136], []byte(op))
	sessionHead[136] = reqType

	comm := crosrpc.CommHead{
		TransportProto: crosrpc.TransportTCP,
		SNO:            sno,
		Length:         uint32(160 + len(payload)),
		HeadLen:        160,
		MsgType:        crosrpc.MsgTypeReq,
		IsPacked:       1,
	}
	copy(comm.Magic[:], []byte(crosrpc.MagicPrefix))

	var pkt []byte
	pkt = append(pkt, comm.Marshal()...)
	pkt = append(pkt, sessionHead...)
	pkt = append(pkt, payload...)
	return pkt
}

// buildCrosRspFrame 辅助函数：构造 CROS RPC RSP / RSP_SEG 帧
func buildCrosRspFrame(sno uint64, msgType uint8, msgNo uint16, statusCode uint32, svc, op string, payload []byte) []byte {
	rawRsp := make([]byte, 156)
	binary.BigEndian.PutUint16(rawRsp[0:2], 1)
	binary.BigEndian.PutUint16(rawRsp[2:4], 156)
	binary.BigEndian.PutUint32(rawRsp[4:8], statusCode)
	copy(rawRsp[24:88], []byte(svc))
	copy(rawRsp[88:152], []byte(op))
	binary.BigEndian.PutUint16(rawRsp[152:154], msgNo)

	comm := crosrpc.CommHead{
		TransportProto: crosrpc.TransportTCP,
		SNO:            sno,
		Length:         uint32(156 + len(payload)),
		HeadLen:        156,
		MsgType:        msgType,
		IsPacked:       1,
	}
	copy(comm.Magic[:], []byte(crosrpc.MagicPrefix))

	var pkt []byte
	pkt = append(pkt, comm.Marshal()...)
	pkt = append(pkt, rawRsp...)
	pkt = append(pkt, payload...)
	return pkt
}

// buildCrosReqFrameWithDOTR 构造含 DOTR 扩展头的请求报文
func buildCrosReqFrameWithDOTR(sno uint64, svc, op string, traceID, spanID, fatherSpanID uint64, payload []byte) []byte {
	sessionHead := make([]byte, 160)
	binary.BigEndian.PutUint16(sessionHead[0:2], 1)
	binary.BigEndian.PutUint16(sessionHead[2:4], 160)
	copy(sessionHead[8:72], []byte(svc))
	copy(sessionHead[72:136], []byte(op))
	sessionHead[136] = crosrpc.ReqTypeSync

	// 36B TraceKey 扩展头 (4B comm + 32B data)
	extTrace := make([]byte, 36)
	binary.BigEndian.PutUint16(extTrace[0:2], 32)
	extTrace[2] = crosrpc.ExtTypeTraceKey
	binary.BigEndian.PutUint64(extTrace[4:12], traceID)
	binary.BigEndian.PutUint64(extTrace[12:20], spanID)
	binary.BigEndian.PutUint64(extTrace[20:28], fatherSpanID)
	extTrace[28] = 1 // RecordSwitch

	totalHeadLen := uint16(160 + len(extTrace))
	comm := crosrpc.CommHead{
		TransportProto: crosrpc.TransportTCP,
		SNO:            sno,
		Length:         uint32(int(totalHeadLen) + len(payload)),
		HeadLen:        totalHeadLen,
		MsgType:        crosrpc.MsgTypeReq,
		IsPacked:       1,
	}
	copy(comm.Magic[:], []byte(crosrpc.MagicPrefix))

	var pkt []byte
	pkt = append(pkt, comm.Marshal()...)
	pkt = append(pkt, sessionHead...)
	pkt = append(pkt, extTrace...)
	pkt = append(pkt, payload...)
	return pkt
}

// TestExchangeAssemblerDualModeAndLimitAutoStop 验证汇聚器双模往返聚合与上限停录 (F-11.1, F-11.3, F-11.4)
func TestExchangeAssemblerDualModeAndLimitAutoStop(t *testing.T) {
	db := setupIsolatedRecorderDB(t)
	ctx := context.Background()

	// 1. 初始化会话与 Agent 租约
	agt := models.Agent{
		Name:      "test-agent-rec",
		Endpoint:  "http://127.0.0.1:19890",
		Scope:     models.ScopeShared,
		Status:    models.AgentStatusOnline,
		CreatedBy: 1,
	}
	if err := db.Create(&agt).Error; err != nil {
		t.Fatalf("创建 Agent 失败: %v", err)
	}

	session := models.RecordingSession{
		SessionNo:        "REC_TEST_ASSEMBLER_001",
		Name:             "汇聚器测试会话",
		Scope:            models.ScopeShared,
		OwnerID:          1,
		Status:           models.SessionStatusRecording,
		TargetProcessIDs: datatypes.JSON([]byte("[1]")),
		LeasedAgentIDs:   datatypes.JSON([]byte(fmt.Sprintf("[%d]", agt.ID))),
		MaxFrames:        5,
		CreatedBy:        1,
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("创建录制会话失败: %v", err)
	}

	// 锁定排他租约
	if err := hub.AcquireAgentLeases(ctx, db, []uint64{agt.ID}, models.AgentLeaseRecording, session.ID, 1, time.Hour); err != nil {
		t.Fatalf("锁定 Agent 租约失败: %v", err)
	}

	// 模拟 Agent 服务接收 /stop 请求
	var agentStopCalled atomic.Bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/agent/v1/monitors/stop" {
			agentStopCalled.Store(true)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(agentproto.StopMonitorResp{})
		}
	}))
	defer ts.Close()
	db.Model(&agt).Update("endpoint", ts.URL)

	// 2. 构造 ExchangeAssembler 与 Orchestrator
	cfg := &AssemblerConfig{
		WorkerShards:  2,
		BatchSize:     10,
		FlushInterval: 50 * time.Millisecond,
	}

	var limitTriggeredSession atomic.Uint64
	var orchestrator *Orchestrator

	assembler := NewExchangeAssembler(db, nil, nil, func(sID uint64, reason string) {
		limitTriggeredSession.Store(sID)
		if orchestrator != nil {
			_, _ = orchestrator.StopSession(context.Background(), sID, 0, true, reason)
		}
	}, cfg)
	defer assembler.Close()

	orchestrator = NewOrchestrator(db, assembler)

	srcIP := net.IPv4(10, 0, 0, 1)
	dstIP := net.IPv4(10, 0, 0, 2)
	baseEnv := func(sno uint64, dir uint8) agentproto.EnvelopeHeader {
		var sIP, dIP [4]byte
		copy(sIP[:], srcIP.To4())
		copy(dIP[:], dstIP.To4())
		return agentproto.EnvelopeHeader{
			Magic:           agentproto.EnvelopeMagic,
			ProtoVer:        1,
			EventType:       agentproto.EventTrafficFrame,
			SessionOrTaskID: session.ID,
			ProcessID:       1,
			CapturedPID:     1234,
			Direction:       dir,
			SrcIPv4:         sIP,
			DstIPv4:         dIP,
			SrcPort:         10001,
			DstPort:         10002,
			ConnSeq:         1,
			CapturedAtNs:    time.Now().UnixNano(),
		}
	}

	// 场景 1: REQ + RSP (标准 UNARY)
	sno1 := uint64(101)
	req1 := buildCrosReqFrame(sno1, crosrpc.ReqTypeSync, "dophi.order-OrderService", "GetOrder", []byte("payload_req_1"))
	rsp1 := buildCrosRspFrame(sno1, crosrpc.MsgTypeRsp, 1, 0, "dophi.order-OrderService", "GetOrder", []byte("payload_rsp_1"))
	assembler.Feed(baseEnv(sno1, 1), req1)
	assembler.Feed(baseEnv(sno1, 2), rsp1)

	// 场景 2: REQ + 2*RSP_SEG + RSP (3 段 STREAM_RSP)
	sno2 := uint64(102)
	req2 := buildCrosReqFrame(sno2, crosrpc.ReqTypeAsync, "dophi.stream-StreamService", "FetchLarge", []byte("payload_req_2"))
	seg1 := buildCrosRspFrame(sno2, crosrpc.MsgTypeRspSeg, 1, 0, "dophi.stream-StreamService", "FetchLarge", []byte("seg_1"))
	seg2 := buildCrosRspFrame(sno2, crosrpc.MsgTypeRspSeg, 2, 0, "dophi.stream-StreamService", "FetchLarge", []byte("seg_2"))
	rsp2 := buildCrosRspFrame(sno2, crosrpc.MsgTypeRsp, 3, 0, "dophi.stream-StreamService", "FetchLarge", []byte("seg_final"))
	assembler.Feed(baseEnv(sno2, 1), req2)
	assembler.Feed(baseEnv(sno2, 2), seg1)
	assembler.Feed(baseEnv(sno2, 2), seg2)
	assembler.Feed(baseEnv(sno2, 2), rsp2)

	// 场景 3: ucReqType=3 Oneway 请求 (ONEWAY)
	sno3 := uint64(103)
	req3 := buildCrosReqFrame(sno3, crosrpc.ReqTypeOneway, "dophi.notify-NotifyService", "PushNotice", []byte("payload_oneway"))
	assembler.Feed(baseEnv(sno3, 1), req3)

	// 场景 4: 连接关闭未应答 (REQ_ONLY)
	sno4 := uint64(104)
	env4 := baseEnv(sno4, 1)
	env4.ConnSeq = 99 // 独立连接
	req4 := buildCrosReqFrame(sno4, crosrpc.ReqTypeSync, "dophi.slow-SlowService", "SlowQuery", []byte("payload_slow"))
	assembler.Feed(env4, req4)
	time.Sleep(20 * time.Millisecond)
	assembler.FeedConnClosed(env4) // 投递连接关闭事件

	// 场景 5: 孤立应答 (RSP_ORPHAN)
	sno5 := uint64(105)
	rsp5 := buildCrosRspFrame(sno5, crosrpc.MsgTypeRsp, 1, 0, "dophi.orphan-OrphanService", "OrphanOp", []byte("payload_orphan"))
	assembler.Feed(baseEnv(sno5, 2), rsp5)

	// 场景 6: 超阈值截断帧 (IS_TRUNCATED)
	sno6 := uint64(106)
	env6 := baseEnv(sno6, 1)
	env6.Flags |= agentproto.FlagIsTruncated
	req6 := buildCrosReqFrame(sno6, crosrpc.ReqTypeSync, "dophi.big-BigService", "BigOp", []byte("truncated_body"))
	assembler.Feed(env6, req6)
	rsp6 := buildCrosRspFrame(sno6, crosrpc.MsgTypeRsp, 1, 0, "dophi.big-BigService", "BigOp", []byte("rsp_body"))
	assembler.Feed(baseEnv(sno6, 2), rsp6)

	// 刷盘同步
	time.Sleep(100 * time.Millisecond)
	assembler.Flush()

	// 3. 严格断言落盘数据与字段
	// (1) 验证 UNARY: 单行同行非空 raw_req_packet + raw_rsp_packet
	var unaryRec models.TrafficRecord
	if err := db.Where("session_id = ? AND sno = ?", session.ID, models.U64Numeric(sno1)).First(&unaryRec).Error; err != nil {
		t.Fatalf("未找到 UNARY 记录: %v", err)
	}
	if unaryRec.ExchangeType != models.ExchangeUnary {
		t.Errorf("预期 ExchangeType=UNARY，实际: %s", unaryRec.ExchangeType)
	}
	if len(unaryRec.RawReqPacket) == 0 || len(unaryRec.RawRspPacket) == 0 {
		t.Errorf("UNARY 原始请求包或应答包为空: req=%d, rsp=%d", len(unaryRec.RawReqPacket), len(unaryRec.RawRspPacket))
	}
	if !unaryRec.IsReplayable {
		t.Errorf("预期 UNARY is_replayable = true")
	}

	// (2) 验证 STREAM_RSP: 段数 RspSegCount=3 且经 SplitFrames 无损还原
	var streamRec models.TrafficRecord
	if err := db.Where("session_id = ? AND sno = ?", session.ID, models.U64Numeric(sno2)).First(&streamRec).Error; err != nil {
		t.Fatalf("未找到 STREAM_RSP 记录: %v", err)
	}
	if streamRec.ExchangeType != models.ExchangeStreamRsp {
		t.Errorf("预期 ExchangeType=STREAM_RSP，实际: %s", streamRec.ExchangeType)
	}
	if streamRec.RspSegCount != 3 {
		t.Errorf("预期 RspSegCount=3，实际: %d", streamRec.RspSegCount)
	}
	splitFrames, err := crosrpc.SplitFrames(streamRec.RawRspPacket)
	if err != nil {
		t.Fatalf("SplitFrames 无损还原失败: %v", err)
	}
	if len(splitFrames) != 3 {
		t.Errorf("预期无损拆分为 3 段应答帧，实际: %d", len(splitFrames))
	}

	// (3) 验证 ONEWAY: raw_req_packet 非空，raw_rsp_packet 为空
	var onewayRec models.TrafficRecord
	if err := db.Where("session_id = ? AND sno = ?", session.ID, models.U64Numeric(sno3)).First(&onewayRec).Error; err != nil {
		t.Fatalf("未找到 ONEWAY 记录: %v", err)
	}
	if onewayRec.ExchangeType != models.ExchangeOneway {
		t.Errorf("预期 ExchangeType=ONEWAY，实际: %s", onewayRec.ExchangeType)
	}
	if len(onewayRec.RawReqPacket) == 0 || len(onewayRec.RawRspPacket) != 0 {
		t.Errorf("ONEWAY 数据异常: req=%d, rsp=%d", len(onewayRec.RawReqPacket), len(onewayRec.RawRspPacket))
	}

	// (4) 验证 REQ_ONLY 与截断帧标记 is_replayable = false
	var reqOnlyRec models.TrafficRecord
	if err := db.Where("session_id = ? AND sno = ?", session.ID, models.U64Numeric(sno4)).First(&reqOnlyRec).Error; err != nil {
		t.Fatalf("未找到 REQ_ONLY 记录: %v", err)
	}
	if reqOnlyRec.ExchangeType != models.ExchangeReqOnly || reqOnlyRec.IsReplayable {
		t.Errorf("REQ_ONLY 异常: type=%s, replayable=%v", reqOnlyRec.ExchangeType, reqOnlyRec.IsReplayable)
	}

	var truncRec models.TrafficRecord
	if err := db.Where("session_id = ? AND sno = ?", session.ID, models.U64Numeric(sno6)).First(&truncRec).Error; err != nil {
		t.Fatalf("未找到截断记录: %v", err)
	}
	if !truncRec.IsTruncated || truncRec.IsReplayable {
		t.Errorf("截断标记异常: is_truncated=%v, is_replayable=%v", truncRec.IsTruncated, truncRec.IsReplayable)
	}

	// 4. 测试双阈值自动停录: 先释放会话 1 占用的租约，将批次 2 的 MaxFrames 设为 5，设置监听并投递 6 帧
	_ = hub.ReleaseAgentLeases(ctx, db, []uint64{agt.ID}, models.AgentLeaseRecording, session.ID)

	session2 := models.RecordingSession{
		SessionNo:        "REC_TEST_LIMIT_002",
		Name:             "上限停录会话",
		Scope:            models.ScopeShared,
		OwnerID:          1,
		Status:           models.SessionStatusRecording,
		TargetProcessIDs: datatypes.JSON([]byte("[1]")),
		LeasedAgentIDs:   datatypes.JSON([]byte(fmt.Sprintf("[%d]", agt.ID))),
		MaxFrames:        5,
		CreatedBy:        1,
	}
	db.Create(&session2)
	if err := hub.AcquireAgentLeases(ctx, db, []uint64{agt.ID}, models.AgentLeaseRecording, session2.ID, 1, time.Hour); err != nil {
		t.Fatalf("锁定会话 2 租约失败: %v", err)
	}
	assembler.SetSessionMaxFrames(session2.ID, 5)

	agentStopCalled.Store(false)
	for i := 1; i <= 6; i++ {
		snoL := uint64(500 + i)
		frame := buildCrosReqFrame(snoL, crosrpc.ReqTypeOneway, "dophi.limit-LimitService", "LimitOp", []byte("data"))
		assembler.Feed(agentproto.EnvelopeHeader{
			Magic:           agentproto.EnvelopeMagic,
			ProtoVer:        1,
			EventType:       agentproto.EventTrafficFrame,
			SessionOrTaskID: session2.ID,
			ProcessID:       1,
			Direction:       1,
			CapturedAtNs:    time.Now().UnixNano(),
		}, frame)
	}

	// 等待自动停录协程处理
	time.Sleep(150 * time.Millisecond)

	if limitTriggeredSession.Load() != session2.ID {
		t.Errorf("预期触发上限停录会话 ID=%d，实际: %d", session2.ID, limitTriggeredSession.Load())
	}
	if !agentStopCalled.Load() {
		t.Errorf("预期自动调用 Agent /stop 接口，实际未调用")
	}

	// 断言会话终态为 COMPLETED_BY_LIMIT 且 Agent 排他租约已释放
	var updatedSession2 models.RecordingSession
	db.Where("id = ?", session2.ID).First(&updatedSession2)
	if updatedSession2.Status != models.SessionStatusCompletedByLimit {
		t.Errorf("预期终态为 COMPLETED_BY_LIMIT，实际: %s", updatedSession2.Status)
	}

	var agtAfter models.Agent
	db.Where("id = ?", agt.ID).First(&agtAfter)
	if agtAfter.LeaseType != models.AgentLeaseNone {
		t.Errorf("预期 Agent 租约已释放，实际 lease_type=%s", agtAfter.LeaseType)
	}
}

// buildOrderProtoDescriptorBytes 动态构造 Order 测试 IDL 描述集
func buildOrderProtoDescriptorBytes(t *testing.T, extraField bool) []byte {
	t.Helper()
	reqFields := []*descriptorpb.FieldDescriptorProto{
		{
			Name:   proto.String("order_id"),
			Number: proto.Int32(1),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_UINT64.Enum(),
		},
		{
			Name:   proto.String("buyer_name"),
			Number: proto.Int32(2),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
		},
	}
	if extraField {
		reqFields = append(reqFields, &descriptorpb.FieldDescriptorProto{
			Name:   proto.String("coupon_code"),
			Number: proto.Int32(3),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
		})
	}

	fileProto := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("dophi/order/v1/order.proto"),
		Package: proto.String("dophi.order.v1"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:  proto.String("OrderServiceReq"),
				Field: reqFields,
			},
			{
				Name: proto.String("OrderServiceRsp"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:   proto.String("status_code"),
						Number: proto.Int32(1),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
					},
				},
			},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: proto.String("OrderService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:       proto.String("CreateOrder"),
						InputType:  proto.String(".dophi.order.v1.OrderServiceReq"),
						OutputType: proto.String(".dophi.order.v1.OrderServiceRsp"),
					},
				},
			},
		},
	}

	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{fileProto},
	}
	data, err := proto.Marshal(fds)
	if err != nil {
		t.Fatalf("Marshal FileDescriptorSet 失败: %v", err)
	}
	return data
}

// TestPacketAnatomyBuilderAndRedecode 验证四段解剖 DTO 构建与按需重解码 (F-03.4, F-11.4)
func TestPacketAnatomyBuilderAndRedecode(t *testing.T) {
	db := setupIsolatedRecorderDB(t)
	ctx := context.Background()

	// 1. 初始化 IDL 注册中心并上传 V1 版本
	reg, err := decoder.NewRegistry(db)
	if err != nil {
		t.Fatalf("NewRegistry 失败: %v", err)
	}

	v1PB := buildOrderProtoDescriptorBytes(t, false)
	s1, _, err := reg.UploadSchema(decoder.UploadSchemaInput{
		Name:         "order_anatomy_idl",
		VersionTag:   "v1.0.0",
		FileName:     "order_anatomy.pb",
		RawBytes:     v1PB,
		AutoActivate: true,
		OperatorID:   1,
	})
	if err != nil {
		t.Fatalf("上传 V1 IDL 失败: %v", err)
	}

	// 2. 构造含 DOTR 扩展头的双模往返流量记录
	sno := uint64(9999)
	reqRaw := buildCrosReqFrameWithDOTR(sno, "dophi.order.v1.OrderService", "CreateOrder", 10001, 20002, 30003, []byte{0x08, 0x7b}) // order_id=123
	rspRaw := buildCrosRspFrame(sno, crosrpc.MsgTypeRsp, 1, 0, "dophi.order.v1.OrderService", "CreateOrder", []byte{0x08, 0x00})    // status_code=0

	rec := models.TrafficRecord{
		CreatedAt:     time.Now(),
		SessionID:     1,
		AgentID:       1,
		ProcessID:     1,
		Direction:     models.TrafficDirIngress,
		ExchangeType:  models.ExchangeUnary,
		ServiceName:   "dophi.order.v1.OrderService",
		Operation:     "CreateOrder",
		SNO:           models.U64Numeric(sno),
		RawReqPacket:  reqRaw,
		RawRspPacket:  rspRaw,
		SchemaID:      &s1.ID,
		SchemaVersion: &s1.Version,
		DecodeStatus:  decoder.DecodeStatusDecodedJSON,
		IsReplayable:  true,
	}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatalf("插入流量记录失败: %v", err)
	}

	// 3. 首次调用 BuildPacketAnatomy 构建四段解剖
	drawerDTO, err := BuildPacketAnatomy(&rec, reg)
	if err != nil {
		t.Fatalf("BuildPacketAnatomy 失败: %v", err)
	}

	// 断言请求侧与应答侧四段数据完整
	if drawerDTO.ReqSide == nil || drawerDTO.RspSide == nil {
		t.Fatalf("预期 reqSide 与 rspSide 均非空")
	}

	// ① 24B 位图 6 色块覆盖
	if len(drawerDTO.ReqSide.CommHead.Segments) != 6 {
		t.Errorf("预期 CommHead 包含 6 个色块切片，实际: %d", len(drawerDTO.ReqSide.CommHead.Segments))
	}
	// ② 会话头版本与耗时
	if drawerDTO.ReqSide.SessionHead.ServiceName != "dophi.order.v1.OrderService" {
		t.Errorf("SessionHead ServiceName 不匹配: %s", drawerDTO.ReqSide.SessionHead.ServiceName)
	}
	// ③ DOTR Trace 扩展头
	if drawerDTO.ReqSide.ExtendHead.Trace == nil {
		t.Fatalf("预期解析到 DOTR Trace 扩展头")
	}
	if drawerDTO.ReqSide.ExtendHead.Trace.TraceIDHex != fmt.Sprintf("%016x", 10001) {
		t.Errorf("TraceIDHex 不匹配: %s", drawerDTO.ReqSide.ExtendHead.Trace.TraceIDHex)
	}
	// ④ 16B 对齐 Raw Hex 与 JSON
	if len(drawerDTO.ReqSide.Payload.RawFrameHexLines) == 0 {
		t.Errorf("预期 RawFrameHexLines 非空")
	}
	if drawerDTO.ReqSide.Payload.SchemaVersion == nil || *drawerDTO.ReqSide.Payload.SchemaVersion != 1 {
		t.Errorf("预期历史 SchemaVersion=1")
	}

	// 4. 上传并激活新版本 V2 (带 extraField)
	v2PB := buildOrderProtoDescriptorBytes(t, true)
	s2, _, err := reg.UploadSchema(decoder.UploadSchemaInput{
		Name:         "order_anatomy_idl",
		VersionTag:   "v2.0.0",
		FileName:     "order_anatomy_v2.pb",
		RawBytes:     v2PB,
		AutoActivate: true,
		OperatorID:   1,
	})
	if err != nil {
		t.Fatalf("上传并激活 V2 IDL 失败: %v", err)
	}

	// 断言未显式重解码前，数据库中历史记录的 SchemaVersion 仍保持为 1 (不可变性)
	var recCheck models.TrafficRecord
	db.Where("id = ?", rec.ID).First(&recCheck)
	if recCheck.SchemaVersion == nil || *recCheck.SchemaVersion != 1 {
		t.Errorf("未手动重解码前历史 SchemaVersion 应保持为 1，实际: %v", recCheck.SchemaVersion)
	}

	// 5. 显式调用 RedecodeTrafficRecord
	redecodeDTO, err := RedecodeTrafficRecord(ctx, db, reg, rec.ID)
	if err != nil {
		t.Fatalf("RedecodeTrafficRecord 失败: %v", err)
	}

	// 断言重解码后 SchemaVersion 已升级为 2
	if redecodeDTO.ReqSide.Payload.SchemaVersion == nil || *redecodeDTO.ReqSide.Payload.SchemaVersion != s2.Version {
		t.Errorf("显式重解码后预期 SchemaVersion=%d，实际: %v", s2.Version, redecodeDTO.ReqSide.Payload.SchemaVersion)
	}

	var recAfter models.TrafficRecord
	db.Where("id = ?", rec.ID).First(&recAfter)
	if recAfter.SchemaVersion == nil || *recAfter.SchemaVersion != s2.Version {
		t.Errorf("数据库中更新后的 SchemaVersion 应为 %d，实际: %v", s2.Version, recAfter.SchemaVersion)
	}
}
