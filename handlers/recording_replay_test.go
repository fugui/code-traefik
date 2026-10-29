package handlers_test

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	commonModels "code-common/backend/models"
	"code-traefik/handlers"
	"code-traefik/internal/server/diff"
	"code-traefik/internal/server/recorder"
	"code-traefik/models"
	"code-traefik/pkg/agentproto"
	"code-traefik/pkg/crosrpc"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// buildTestCrosReqFrame 构造请求帧
func buildTestCrosReqFrame(sno uint64, reqType uint8, svc, op string, payload []byte) []byte {
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

// buildTestCrosRspFrame 构造应答帧
func buildTestCrosRspFrame(sno uint64, msgType uint8, msgNo uint16, statusCode uint32, svc, op string, payload []byte) []byte {
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

func buildTestProtoDescriptor(t *testing.T) []byte {
	t.Helper()
	fd := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("order_service.proto"),
		Package: proto.String("order"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("CreateOrderReq"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("order_id"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("order_id"),
					},
					{
						Name:     proto.String("amount_cents"),
						Number:   proto.Int32(2),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
						JsonName: proto.String("amount_cents"),
					},
				},
			},
			{
				Name: proto.String("CreateOrderRsp"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("code"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
						JsonName: proto.String("code"),
					},
					{
						Name:     proto.String("message"),
						Number:   proto.Int32(2),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("message"),
					},
					{
						Name:     proto.String("order_id"),
						Number:   proto.Int32(3),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("order_id"),
					},
				},
			},
		},
	}
	fds := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fd}}
	raw, err := proto.Marshal(fds)
	if err != nil {
		t.Fatalf("marshal test pb descriptor failed: %v", err)
	}
	return raw
}

func TestSC05_SC07_SC08_EndToEndRecordingAndReplayPipeline(t *testing.T) {
	r, db := setupRouterAndDB(t)

	// 1. 创建基础测试用户
	admin := createTestUserInDB(t, db, "admin_m4", "traefik_admin")
	adminToken := generateTestJWT(t, admin)
	_ = adminToken

	alice := createTestUserInDB(t, db, "alice_m4", "operator")
	aliceToken := generateTestJWT(t, alice)

	// 2. 构造三级架构模型 (Order 与 Stock 模块)
	subsystem := models.Subsystem{
		Code:        "COMMERCE",
		Name:        "电商交易核心子系统",
		Description: "测试子系统",
	}
	if err := db.Create(&subsystem).Error; err != nil {
		t.Fatalf("创建子系统失败: %v", err)
	}

	fnGroup := models.FunctionalGroup{
		SubsystemID: subsystem.ID,
		Code:        "ORDER_GRP",
		Name:        "订单与履约功能组",
		SortOrder:   1,
	}
	if err := db.Create(&fnGroup).Error; err != nil {
		t.Fatalf("创建功能组失败: %v", err)
	}

	modOrder := models.Module{
		SubsystemID: subsystem.ID,
		GroupID:     fnGroup.ID,
		Code:        "Mod_Order",
		Name:        "订单处理模块",
	}
	if err := db.Create(&modOrder).Error; err != nil {
		t.Fatalf("创建 Mod_Order 失败: %v", err)
	}

	modStock := models.Module{
		SubsystemID: subsystem.ID,
		GroupID:     fnGroup.ID,
		Code:        "Mod_Stock",
		Name:        "库存扣减模块",
	}
	if err := db.Create(&modStock).Error; err != nil {
		t.Fatalf("创建 Mod_Stock 失败: %v", err)
	}

	// 接口契约：Mod_Order PROVIDE OrderService:CreateOrder, DEPEND StockService:DeductStock
	ifaceProvide := models.ModuleInterface{
		ModuleID:     modOrder.ID,
		RelationType: "PROVIDE",
		ServiceName:  "OrderService",
		Operation:    "CreateOrder",
	}
	if err := db.Create(&ifaceProvide).Error; err != nil {
		t.Fatalf("创建 OrderService 契约失败: %v", err)
	}

	ifaceDepend := models.ModuleInterface{
		ModuleID:     modOrder.ID,
		RelationType: "DEPEND",
		ServiceName:  "StockService",
		Operation:    "DeductStock",
	}
	if err := db.Create(&ifaceDepend).Error; err != nil {
		t.Fatalf("创建 StockService 契约失败: %v", err)
	}

	// 激活 Proto 描述集
	pbBytes := buildTestProtoDescriptor(t)
	schema := models.ProtoSchema{
		Name:            "order_service",
		Version:         1,
		FileSize:        len(pbBytes),
		DescriptorBytes: pbBytes,
		IsActive:        true,
	}
	if err := db.Create(&schema).Error; err != nil {
		t.Fatalf("创建 ProtoSchema 失败: %v", err)
	}
	opOrder := models.ProtoOperation{
		SchemaID:           schema.ID,
		ProtoFile:          "order_service.proto",
		ProtoPackage:       "order",
		ProtoService:       "OrderService",
		ServiceName:        "OrderService",
		Operation:          "CreateOrder",
		ReqMessageFullName: "order.CreateOrderReq",
		RspMessageFullName: "order.CreateOrderRsp",
		IsActive:           true,
	}
	if err := db.Create(&opOrder).Error; err != nil {
		t.Fatalf("创建 ProtoOperation 失败: %v", err)
	}

	// 重新载入 Registry
	if dec := handlers.GetGlobalReplayScheduler(); dec != nil {
		_ = dec
	}
	decReg, _ := handlers.GlobalDecoderRegistry, error(nil)
	if decReg != nil {
		_ = decReg.Reload()
	}

	// 3. 构造 Mock Agent 1 (录制源) 与 Mock Agent 2 (重放执行目标)
	var agent1StopCalled atomic.Bool
	agent1Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/agent/v1/handshake":
			_ = json.NewEncoder(w).Encode(agentproto.HandshakeResp{
				AgentVersion: "1.0", HasEbpfCap: true,
			})
		case r.Method == http.MethodPut && r.URL.Path == "/agent/v1/monitors/sync":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"diagnostics": []interface{}{}})
		case r.Method == http.MethodPost && r.URL.Path == "/agent/v1/monitors/stop":
			agent1StopCalled.Store(true)
			_ = json.NewEncoder(w).Encode(agentproto.StopMonitorResp{CapturedFrames: 2})
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer agent1Server.Close()

	var replayExecCallCount atomic.Int32
	var replayResultResp agentproto.ReplayTaskResultResp
	agent2Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/agent/v1/handshake":
			_ = json.NewEncoder(w).Encode(agentproto.HandshakeResp{
				AgentVersion: "1.0", HasEbpfCap: true,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/agent/v1/replays/execute":
			replayExecCallCount.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "ARMED"})
		case r.Method == http.MethodGet && len(r.URL.Path) > 17 && r.URL.Path[:17] == "/agent/v1/replays":
			_ = json.NewEncoder(w).Encode(replayResultResp)
		case r.Method == http.MethodPost && len(r.URL.Path) > 17 && r.URL.Path[:17] == "/agent/v1/replays":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"cancelled": true})
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer agent2Server.Close()

	// 注册 Agent 模型
	now := time.Now()
	agent1 := models.Agent{
		Name:            "agent_source_prod",
		Endpoint:        agent1Server.URL,
		Scope:           models.ScopeShared,
		Status:          models.AgentStatusOnline,
		HasEbpfCap:      true,
		LastHeartbeatAt: &now,
	}
	if err := db.Create(&agent1).Error; err != nil {
		t.Fatalf("创建 Agent 1 失败: %v", err)
	}

	agent2 := models.Agent{
		Name:            "agent_target_test",
		Endpoint:        agent2Server.URL,
		Scope:           models.ScopeShared,
		Status:          models.AgentStatusOnline,
		HasEbpfCap:      true,
		LastHeartbeatAt: &now,
	}
	if err := db.Create(&agent2).Error; err != nil {
		t.Fatalf("创建 Agent 2 失败: %v", err)
	}

	// 注册进程：proc1 (Order, Agent 1), proc2 (Stock, Agent 1), proc3 (Order, Agent 2)
	proc1 := models.Process{
		AgentID:     agent1.ID,
		Name:        "order_service_prod",
		ListenPort:  8081,
		AdvertiseIP: "192.168.56.18",
		CmdPattern:  "order_service",
	}
	if err := db.Create(&proc1).Error; err != nil {
		t.Fatalf("创建 Process 1 失败: %v", err)
	}
	_ = db.Create(&models.ProcessModule{ProcessID: proc1.ID, ModuleID: modOrder.ID}).Error

	proc2 := models.Process{
		AgentID:     agent1.ID,
		Name:        "stock_service_prod",
		ListenPort:  8082,
		AdvertiseIP: "192.168.56.18",
		CmdPattern:  "stock_service",
	}
	if err := db.Create(&proc2).Error; err != nil {
		t.Fatalf("创建 Process 2 失败: %v", err)
	}
	_ = db.Create(&models.ProcessModule{ProcessID: proc2.ID, ModuleID: modStock.ID}).Error

	proc3 := models.Process{
		AgentID:     agent2.ID,
		Name:        "order_service_test",
		ListenPort:  18081,
		AdvertiseIP: "192.168.56.18",
		CmdPattern:  "order_service",
	}
	if err := db.Create(&proc3).Error; err != nil {
		t.Fatalf("创建 Process 3 失败: %v", err)
	}
	_ = db.Create(&models.ProcessModule{ProcessID: proc3.ID, ModuleID: modOrder.ID}).Error

	// -------------------------------------------------------------------------
	// 4. SC-05 协同录制与双模微批落盘
	// -------------------------------------------------------------------------
	t.Log("===> Step 1: 启动协同录制会话 (POST /api/recording-sessions)")
	startReqBody := recorder.StartSessionReq{
		Name:             "电商业务全链路协同录制",
		Scope:            models.ScopeShared,
		TargetProcessIDs: []uint64{proc1.ID, proc2.ID},
		MaxFrames:        1000,
		MaxDurationSec:   300,
	}
	startJSON, _ := json.Marshal(startReqBody)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/recording-sessions", bytes.NewReader(startJSON))
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("启动协同录制期望 201 Created，实际返回 %d: %s", w.Code, w.Body.String())
	}

	var sessionResp models.RecordingSession
	if err := json.Unmarshal(w.Body.Bytes(), &sessionResp); err != nil {
		t.Fatalf("解析录制会话响应失败: %v", err)
	}
	if sessionResp.ID == 0 || sessionResp.Status != models.SessionStatusRecording {
		t.Fatalf("会话状态异常: ID=%d, Status=%s", sessionResp.ID, sessionResp.Status)
	}

	// 检查 Agent 1 租约已被占用
	var checkAgent1 models.Agent
	if err := db.First(&checkAgent1, agent1.ID).Error; err != nil {
		t.Fatalf("查询 Agent 1 失败: %v", err)
	}
	if checkAgent1.LeaseType != models.AgentLeaseRecording {
		t.Fatalf("Agent 1 租约期望 RECORDING，实际为: %s", checkAgent1.LeaseType)
	}

	t.Log("===> Step 2: 投递 CROS RPC 往返流量帧至 ExchangeAssembler")
	assembler := handlers.GetGlobalOrchestrator()
	if assembler == nil {
		t.Fatalf("Orchestrator 未注册")
	}

	// 注入同连接两笔往返事务
	// 事务 1: SNO=1001, UNARY 成功往返
	reqPkt1 := buildTestCrosReqFrame(1001, crosrpc.ReqTypeSync, "OrderService", "CreateOrder", []byte("req_payload_001"))
	rspPkt1 := buildTestCrosRspFrame(1001, crosrpc.MsgTypeRsp, 1, 0, "OrderService", "CreateOrder", []byte("rsp_payload_001"))

	// 事务 2: SNO=1002, UNARY 成功往返
	reqPkt2 := buildTestCrosReqFrame(1002, crosrpc.ReqTypeSync, "OrderService", "CreateOrder", []byte("req_payload_002"))
	rspPkt2 := buildTestCrosRspFrame(1002, crosrpc.MsgTypeRsp, 1, 0, "OrderService", "CreateOrder", []byte("rsp_payload_002"))

	// 取得全局 assembler 并 Feed
	glbAssembler := handlers.GetGlobalOrchestrator().Assembler()
	makeEnv := func(dir uint8, payloadLen int) agentproto.EnvelopeHeader {
		return agentproto.EnvelopeHeader{
			Magic:           agentproto.EnvelopeMagic,
			ProtoVer:        1,
			EventType:       agentproto.EventTrafficFrame,
			SessionOrTaskID: sessionResp.ID,
			ProcessID:       proc1.ID,
			CapturedPID:     10001,
			Direction:       dir,
			SrcIPv4:         [4]byte{192, 168, 56, 100},
			DstIPv4:         [4]byte{192, 168, 56, 18},
			SrcPort:         45678,
			DstPort:         8081,
			ConnSeq:         1,
			PayloadLen:      uint32(payloadLen),
			CapturedAtNs:    time.Now().UnixNano(),
		}
	}

	glbAssembler.Feed(makeEnv(agentproto.DirectionIngress, len(reqPkt1)), reqPkt1)
	glbAssembler.Feed(makeEnv(agentproto.DirectionIngress, len(rspPkt1)), rspPkt1)
	glbAssembler.Feed(makeEnv(agentproto.DirectionIngress, len(reqPkt2)), reqPkt2)
	glbAssembler.Feed(makeEnv(agentproto.DirectionIngress, len(rspPkt2)), rspPkt2)

	// 强制落盘
	glbAssembler.Flush()
	time.Sleep(50 * time.Millisecond)

	// 验证 traefik_traffic_records 表中同行落盘记录
	var savedRecords []models.TrafficRecord
	if err := db.Where("session_id = ?", sessionResp.ID).Order("sno ASC").Find(&savedRecords).Error; err != nil {
		t.Fatalf("查询流量记录失败: %v", err)
	}
	if len(savedRecords) < 2 {
		t.Fatalf("期望至少落盘 2 条往返记录，实际落盘 %d", len(savedRecords))
	}
	rec1 := savedRecords[0]
	rec2 := savedRecords[1]
	if rec1.ExchangeType != "UNARY" || !rec1.IsReplayable || len(rec1.RawReqPacket) == 0 || len(rec1.RawRspPacket) == 0 {
		t.Fatalf("往返记录 1 字段不符合预期: type=%s, replayable=%v, rawReqLen=%d, rawRspLen=%d",
			rec1.ExchangeType, rec1.IsReplayable, len(rec1.RawReqPacket), len(rec1.RawRspPacket))
	}

	t.Log("===> Step 3: 分页查询流量检索列表 (GET /api/traffic-records)")
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, fmt.Sprintf("/api/traffic-records?session_id=%d", sessionResp.ID), nil)
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("查询流量记录列表期望 200 OK，实际返回 %d: %s", w.Code, w.Body.String())
	}

	t.Log("===> Step 4: 查询单条报文四段解剖 DTO (GET /api/traffic-records/:id/anatomy)")
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, fmt.Sprintf("/api/traffic-records/%d/anatomy", rec1.ID), nil)
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("查询报文解剖期望 200 OK，实际返回 %d: %s", w.Code, w.Body.String())
	}
	var anatomyResp recorder.PacketAnatomyDrawerDTO
	if err := json.Unmarshal(w.Body.Bytes(), &anatomyResp); err != nil {
		t.Fatalf("解析报文解剖响应失败: %v", err)
	}
	if anatomyResp.ReqSide == nil || anatomyResp.RspSide == nil {
		t.Fatalf("报文解剖未返回完整的 reqSide 或 rspSide")
	}

	t.Log("===> Step 5: 停止录制会话并触发离线架构归属与租约释放 (POST /api/recording-sessions/:id/stop)")
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, fmt.Sprintf("/api/recording-sessions/%d/stop", sessionResp.ID), nil)
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("停止录制会话期望 200 OK，实际返回 %d: %s", w.Code, w.Body.String())
	}

	// 确认 Agent 1 租约已释放
	if err := db.First(&checkAgent1, agent1.ID).Error; err != nil {
		t.Fatalf("查询 Agent 1 失败: %v", err)
	}
	if checkAgent1.LeaseType != "" {
		t.Fatalf("停止会话后 Agent 1 租约期望释放为空，实际为: %s", checkAgent1.LeaseType)
	}

	t.Log("===> Step 6: 验证架构一致性比对报告 (GET /api/recording-sessions/:id/consistency-report)")
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, fmt.Sprintf("/api/recording-sessions/%d/consistency-report?refresh=true", sessionResp.ID), nil)
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("生成一致性报告期望 200 OK，实际返回 %d: %s", w.Code, w.Body.String())
	}

	// -------------------------------------------------------------------------
	// 5. SC-07 跨 Agent 同构离线重放与自愈验证 (PASS)
	// -------------------------------------------------------------------------
	t.Log("===> Step 7: 跨 Agent 创建同构重放任务 (POST /api/replay-tasks) -> PASS")
	replayResultResp = agentproto.ReplayTaskResultResp{
		TaskID:         1,
		Status:         models.ReplayResultPass,
		TotalRequests:  1,
		PassedRequests: 1,
		MockHitCount:   1,
		MockMissCount:  0,
		Details: fmt.Sprintf(`[{"record_id": %d, "status": "PASS", "actual_rsp_hex": "%s"}]`,
			rec1.ID, hex.EncodeToString(rec1.RawRspPacket)),
	}

	createReplayReq := diff.CreateReplayTaskReq{
		Name:              "跨Agent同构重放-基线测试",
		SessionID:         sessionResp.ID,
		SourceProcessID:   proc1.ID,
		TargetProcessID:   proc3.ID,
		SelectedRecordIDs: []uint64{rec1.ID},
		TimeoutMs:         5000,
	}
	replayJSON, _ := json.Marshal(createReplayReq)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/api/replay-tasks", bytes.NewReader(replayJSON))
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("创建重放任务期望 201 Created，实际返回 %d: %s", w.Code, w.Body.String())
	}

	var replayTaskResp models.ReplayTask
	if err := json.Unmarshal(w.Body.Bytes(), &replayTaskResp); err != nil {
		t.Fatalf("解析重放任务响应失败: %v", err)
	}

	// 等待调度器异步执行完成（轮询数据库状态）
	var finalTask models.ReplayTask
	for attempt := 0; attempt < 20; attempt++ {
		time.Sleep(100 * time.Millisecond)
		if err := db.First(&finalTask, replayTaskResp.ID).Error; err == nil {
			if finalTask.LifecycleState == "COMPLETED" {
				break
			}
		}
	}
	if finalTask.LifecycleState != "COMPLETED" || finalTask.ResultStatus != models.ReplayResultPass {
		t.Fatalf("重放任务状态不符合预期: state=%s, result=%s", finalTask.LifecycleState, finalTask.ResultStatus)
	}

	// 确认 Agent 2 租约在重放结束后已被释放
	var checkAgent2 models.Agent
	if err := db.First(&checkAgent2, agent2.ID).Error; err != nil {
		t.Fatalf("查询 Agent 2 失败: %v", err)
	}
	if checkAgent2.LeaseType != "" {
		t.Fatalf("重放结束后 Agent 2 租约期望释放为空，实际为: %s", checkAgent2.LeaseType)
	}

	// -------------------------------------------------------------------------
	// 6. SC-08 突变 Diff 与 MockMiss 检出与状态全序聚合
	// -------------------------------------------------------------------------
	t.Log("===> Step 8: 创建包含突变应答与 MockMiss 的重放任务并验证全序聚合")
	// 构造一笔突变报文 (篡改状态码为 5001)
	mutatedRsp := buildTestCrosRspFrame(1001, crosrpc.MsgTypeRsp, 1, 5001, "OrderService", "CreateOrder", []byte("rsp_payload_001"))

	replayResultResp = agentproto.ReplayTaskResultResp{
		TaskID:         2,
		Status:         models.ReplayResultMockMiss,
		TotalRequests:  2,
		PassedRequests: 0,
		MockHitCount:   0,
		MockMissCount:  1,
		Details: fmt.Sprintf(`[
			{"record_id": %d, "status": "FAILED", "actual_rsp_hex": "%s"},
			{"record_id": %d, "status": "MOCK_MISS"}
		]`, rec1.ID, hex.EncodeToString(mutatedRsp), rec2.ID),
	}

	createTask2Req := diff.CreateReplayTaskReq{
		Name:              "注入突变与MockMiss测试",
		SessionID:         sessionResp.ID,
		SourceProcessID:   proc1.ID,
		TargetProcessID:   proc3.ID,
		SelectedRecordIDs: []uint64{rec1.ID, rec2.ID},
		TimeoutMs:         5000,
	}
	task2JSON, _ := json.Marshal(createTask2Req)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/api/replay-tasks", bytes.NewReader(task2JSON))
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("创建任务 2 期望 201 Created，实际返回 %d: %s", w.Code, w.Body.String())
	}

	var task2 models.ReplayTask
	_ = json.Unmarshal(w.Body.Bytes(), &task2)

	// 等待完成
	for attempt := 0; attempt < 20; attempt++ {
		time.Sleep(100 * time.Millisecond)
		if err := db.First(&task2, task2.ID).Error; err == nil {
			if task2.LifecycleState == "COMPLETED" {
				break
			}
		}
	}

	// 状态全序聚合断言: ERROR > TIMEOUT > MOCK_MISS > FAILED > PASS
	// 包含 FAILED 与 MOCK_MISS，最终聚合状态必须为 MOCK_MISS
	if task2.ResultStatus != models.ReplayResultMockMiss {
		t.Fatalf("全序聚合期望 MOCK_MISS，实际为: %s", task2.ResultStatus)
	}

	t.Log("===> Step 9: 按 result_status=MOCK_MISS 分页检索重放明细条目")
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, fmt.Sprintf("/api/replay-tasks/%d/items?result_status=MOCK_MISS", task2.ID), nil)
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("筛选 MOCK_MISS 条目期望 200 OK，实际返回 %d: %s", w.Code, w.Body.String())
	}
	var itemsResp struct {
		Items []models.ReplayItem `json:"items"`
		Total int64               `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &itemsResp); err != nil {
		t.Fatalf("解析明细条目失败: %v", err)
	}
	if itemsResp.Total != 1 || len(itemsResp.Items) != 1 || itemsResp.Items[0].ResultStatus != "MOCK_MISS" {
		t.Fatalf("MOCK_MISS 筛选结果不符合预期: total=%d, items=%+v", itemsResp.Total, itemsResp.Items)
	}

	t.Log("===> Step 10: 测试重放条目四段解剖明细 (GET /api/replay-tasks/:id/items/:itemId)")
	failedItem := itemsResp.Items[0]
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, fmt.Sprintf("/api/replay-tasks/%d/items/%d", task2.ID, failedItem.ID), nil)
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("获取重放条目解剖明细期望 200 OK，实际返回 %d: %s", w.Code, w.Body.String())
	}

	t.Log("===> Step 11: 测试离线重算双层 Diff (POST /api/replay-tasks/:id/rediff)")
	rediffReqBody := []byte(`{"extra_ignore_paths":["error_code"]}`)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, fmt.Sprintf("/api/replay-tasks/%d/rediff", task2.ID), bytes.NewReader(rediffReqBody))
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("离线重算 Diff 期望 200 OK，实际返回 %d: %s", w.Code, w.Body.String())
	}

	// -------------------------------------------------------------------------
	// 7. 审计日志完整性门禁断言
	// -------------------------------------------------------------------------
	t.Log("===> Step 12: 验证操作审计日志落盘")
	// 等待审计日志异步刷盘
	time.Sleep(100 * time.Millisecond)

	var auditLogs []commonModels.SysAuditLog
	if err := db.Where("module = ?", "traefik").Find(&auditLogs).Error; err != nil {
		t.Fatalf("查询审计日志失败: %v", err)
	}

	actionsMap := make(map[string]bool)
	for _, l := range auditLogs {
		actionsMap[l.Action] = true
	}

	expectedActions := []string{"start_session", "stop_session", "execute_replay"}
	for _, act := range expectedActions {
		if !actionsMap[act] {
			t.Errorf("缺少预期的审计操作日志: %s (实际记录: %+v)", act, actionsMap)
		}
	}
	t.Logf("审计日志验证通过，共记录 %d 条审计事件", len(auditLogs))
}
