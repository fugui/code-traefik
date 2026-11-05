package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"code-traefik/internal/server/callgraph"
	"code-traefik/models"
	"gorm.io/datatypes"
)

// T-M13-08: 报文因果树、按根事务不割裂分页与时序泳道闭包 API 集成测试
func TestTrafficCallGraph_Endpoints_Integration(t *testing.T) {
	r, db := setupRouterAndDB(t)

	alice := createTestUserInDB(t, db, "alice_cg_int", "operator")
	aliceToken := generateTestJWT(t, alice)

	// 1. 创建共享录制批次
	session := models.RecordingSession{
		SessionNo:        "REC_INT_M13_001",
		Name:             "M13 报文因果树全链路测试批次",
		Scope:            models.ScopeShared,
		OwnerID:          alice.ID,
		Status:           "STOPPED",
		TargetProcessIDs: datatypes.JSON([]byte(`[10]`)),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("创建录制批次失败: %v", err)
	}

	latRoot := int64(30000)
	lat1 := int64(10000)
	lat2 := int64(8000)
	tBase := int64(1700000000000000000)
	rspT1 := tBase + latRoot*1000
	rspT2 := tBase + 2_000_000 + lat1*1000
	rspT3 := tBase + 15_000_000 + lat2*1000

	// 2. 写入三条按因果关联的往返记录
	pldRoot := []byte(`{"req":{"sku_id":"SKU_888"},"rsp":{"order_id":"ORD_1001","code":0}}`)
	hdrRoot := []byte(`{"meta":{"peer_proc_name":"api-gw","produced_entities":["order_id=ORD_1001"]}}`)
	pldLock := []byte(`{"req":{"sku_id":"SKU_888"},"rsp":{"lock_id":"LK_77","code":0}}`)
	pldRelease := []byte(`{"req":{"lock_id":"LK_77"},"rsp":{"code":0}}`)

	recRoot := models.TrafficRecord{
		SessionID:       session.ID,
		SeqNo:           1,
		ProcessID:       10,
		Direction:       "INGRESS",
		ServiceName:     "TradeService",
		Operation:       "CreateOrder",
		TraceID:         "trace-int-01",
		SpanID:          "span-root",
		FatherSpanID:    "",
		ConnKey:         "192.168.1.1:40001->192.168.1.2:8091",
		SrcAddr:         "192.168.1.1:40001",
		DstAddr:         "192.168.1.2:8091",
		ReqCapturedAtNs: tBase,
		RspCapturedAtNs: &rspT1,
		LatencyUs:       &latRoot,
		StatusCode:      models.U64Numeric(0),
		PayloadJSON:     datatypes.JSON(pldRoot),
		HeadersJSON:     datatypes.JSON(hdrRoot),
		IsReplayable:    true,
		CreatedAt:       time.Now(),
	}
	recChild1 := models.TrafficRecord{
		SessionID:       session.ID,
		SeqNo:           2,
		ProcessID:       10,
		Direction:       "EGRESS",
		ServiceName:     "StockService",
		Operation:       "LockStock",
		TraceID:         "trace-int-01",
		SpanID:          "span-lock",
		FatherSpanID:    "span-root",
		ConnKey:         "192.168.1.2:50001->192.168.1.3:8092",
		SrcAddr:         "192.168.1.2:50001",
		DstAddr:         "192.168.1.3:8092",
		ReqCapturedAtNs: tBase + 2_000_000,
		RspCapturedAtNs: &rspT2,
		LatencyUs:       &lat1,
		StatusCode:      models.U64Numeric(0),
		PayloadJSON:     datatypes.JSON(pldLock),
		RawReqPacket:    []byte("req-lock-packet"),
		RawRspPacket:    []byte("rsp-lock-packet"),
		IsReplayable:    false,
		CreatedAt:       time.Now(),
	}
	recChild2 := models.TrafficRecord{
		SessionID:       session.ID,
		SeqNo:           3,
		ProcessID:       10,
		Direction:       "EGRESS",
		ServiceName:     "StockService",
		Operation:       "ReleaseStock",
		TraceID:         "trace-int-01",
		SpanID:          "span-release",
		FatherSpanID:    "span-root",
		ConnKey:         "192.168.1.2:50001->192.168.1.3:8092",
		SrcAddr:         "192.168.1.2:50001",
		DstAddr:         "192.168.1.3:8092",
		ReqCapturedAtNs: tBase + 15_000_000,
		RspCapturedAtNs: &rspT3,
		LatencyUs:       &lat2,
		StatusCode:      models.U64Numeric(0),
		PayloadJSON:     datatypes.JSON(pldRelease),
		RawReqPacket:    []byte("req-release-packet"),
		RawRspPacket:    []byte("rsp-release-packet"),
		IsReplayable:    false,
		CreatedAt:       time.Now(),
	}

	if err := db.Create(&recRoot).Error; err != nil {
		t.Fatalf("插入 recRoot 失败: %v", err)
	}
	if err := db.Create(&recChild1).Error; err != nil {
		t.Fatalf("插入 recChild1 失败: %v", err)
	}
	if err := db.Create(&recChild2).Error; err != nil {
		t.Fatalf("插入 recChild2 失败: %v", err)
	}

	// -------------------------------------------------------------------------
	// 1. GET /api/traffic-records 发起序与水合验证
	// -------------------------------------------------------------------------
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("/api/traffic-records?session_id=%d&sort_dir=asc", session.ID), nil)
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/traffic-records failed: %d %s", w.Code, w.Body.String())
	}

	var listResp struct {
		Items []callgraph.TrafficRecordSummary `json:"items"`
		Total int64                            `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("反序列化流量列表响应失败: %v", err)
	}
	if listResp.Total != 3 || len(listResp.Items) != 3 {
		t.Fatalf("预期 3 条记录，实际返回 total=%d, items=%d", listResp.Total, len(listResp.Items))
	}
	// 断言严格按发起时序与 seq_no 排序
	if listResp.Items[0].SeqNo != 1 || listResp.Items[1].SeqNo != 2 || listResp.Items[2].SeqNo != 3 {
		t.Errorf("记录未按发起时序升序返回: %#v", listResp.Items)
	}
	// 断言水合字段
	if listResp.Items[0].PeerProcName != "api-gw" {
		t.Errorf("expected peer_proc_name='api-gw', got '%s'", listResp.Items[0].PeerProcName)
	}
	if listResp.Items[0].ConnIndex != 1 {
		t.Errorf("expected conn_index=1, got %d", listResp.Items[0].ConnIndex)
	}

	// -------------------------------------------------------------------------
	// 2. GET /api/recording-sessions/:id/transactions 按根事务分页与祖先链保留
	// -------------------------------------------------------------------------
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, fmt.Sprintf("/api/recording-sessions/%d/transactions?page=1&page_size=25", session.ID), nil)
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /transactions failed: %d %s", w.Code, w.Body.String())
	}

	var txResp struct {
		Items        []callgraph.TransactionTreeGroupDTO `json:"items"`
		Total        int                                 `json:"total"`
		EntityChains []callgraph.EntityLifecycleChainDTO `json:"entity_chains"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &txResp); err != nil {
		t.Fatalf("反序列化 /transactions 失败: %v", err)
	}
	if txResp.Total != 1 || len(txResp.Items) != 1 {
		t.Fatalf("预期 1 组根事务，实际 total=%d, items=%d", txResp.Total, len(txResp.Items))
	}
	grp := txResp.Items[0]
	if grp.RootRecordID != fmt.Sprintf("%d", recRoot.ID) {
		t.Errorf("expected root record ID %d, got %s", recRoot.ID, grp.RootRecordID)
	}
	if len(grp.Root.Children) != 2 {
		t.Errorf("expected 2 child calls under root, got %d", len(grp.Root.Children))
	}

	// 过滤深层子方法 LockStock，断言保留父根行且子行 matched_filter=true
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, fmt.Sprintf("/api/recording-sessions/%d/transactions?operation=LockStock", session.ID), nil)
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /transactions?operation=LockStock failed: %d %s", w.Code, w.Body.String())
	}
	var filterResp struct {
		Items []callgraph.TransactionTreeGroupDTO `json:"items"`
		Total int                                 `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &filterResp)
	if filterResp.Total != 1 {
		t.Fatalf("expected 1 root group preserved on child filter, got %d", filterResp.Total)
	}
	if !filterResp.Items[0].Root.Children[0].MatchedFilter {
		t.Errorf("expected child LockStock to have matched_filter=true")
	}

	// -------------------------------------------------------------------------
	// 3. GET /api/recording-sessions/:id/transactions/:rootRecordId/sequence 时序泳道
	// -------------------------------------------------------------------------
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, fmt.Sprintf("/api/recording-sessions/%d/transactions/%d/sequence", session.ID, recRoot.ID), nil)
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /sequence failed: %d %s", w.Code, w.Body.String())
	}
	var seqResp callgraph.TransactionSequenceDTO
	if err := json.Unmarshal(w.Body.Bytes(), &seqResp); err != nil {
		t.Fatalf("反序列化 /sequence 失败: %v", err)
	}
	if len(seqResp.Participants) < 2 {
		t.Errorf("expected >= 2 participants, got %d", len(seqResp.Participants))
	}
	if len(seqResp.Steps) != 3 {
		t.Errorf("expected 3 sequence steps, got %d", len(seqResp.Steps))
	}
	// 验证血缘边 (LockStock.rsp.lock_id -> ReleaseStock.req.lock_id)
	if len(seqResp.ParameterLineages) == 0 {
		t.Errorf("expected parameter lineage edges in sequence DTO")
	}

	// -------------------------------------------------------------------------
	// 4. GET /api/traffic-records/:id/replay-closure 重放闭包预览
	// -------------------------------------------------------------------------
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, fmt.Sprintf("/api/traffic-records/%d/replay-closure", recRoot.ID), nil)
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /replay-closure failed: %d %s", w.Code, w.Body.String())
	}
	var closureResp callgraph.ReplayClosurePreviewDTO
	if err := json.Unmarshal(w.Body.Bytes(), &closureResp); err != nil {
		t.Fatalf("反序列化 /replay-closure 失败: %v", err)
	}
	if len(closureResp.ScopedEgressMocks) != 2 {
		t.Fatalf("expected 2 scoped egress mocks for root, got %d", len(closureResp.ScopedEgressMocks))
	}
	if !closureResp.ScopedEgressMocks[0].UsableAsMock {
		t.Errorf("expected LockStock mock to be usable")
	}
	if len(closureResp.RecommendedRecordIDs) != 1 || closureResp.RecommendedRecordIDs[0] != fmt.Sprintf("%d", recRoot.ID) {
		t.Errorf("expected recommended_record_ids=[%d], got %#v", recRoot.ID, closureResp.RecommendedRecordIDs)
	}
}

// T-M13-09: 公私域 Anti-IDOR 越权拦截安全测试
func TestTrafficCallGraph_AntiIDOR_Security(t *testing.T) {
	r, db := setupRouterAndDB(t)

	alice := createTestUserInDB(t, db, "alice_idor", "operator")
	aliceToken := generateTestJWT(t, alice)
	_ = aliceToken

	bob := createTestUserInDB(t, db, "bob_idor", "operator")
	bobToken := generateTestJWT(t, bob)

	// 1. 创建属于 Alice 的 PRIVATE 录制批次
	privSession := models.RecordingSession{
		SessionNo:        "REC_PRIV_ALICE_001",
		Name:             "Alice 私有录制批次",
		Scope:            models.ScopePrivate,
		OwnerID:          alice.ID,
		Status:           "STOPPED",
		TargetProcessIDs: datatypes.JSON([]byte(`[10]`)),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	if err := db.Create(&privSession).Error; err != nil {
		t.Fatalf("创建私有批次失败: %v", err)
	}

	recPriv := models.TrafficRecord{
		SessionID:       privSession.ID,
		SeqNo:           1,
		ProcessID:       10,
		Direction:       "INGRESS",
		ServiceName:     "PrivateService",
		Operation:       "PrivateOperation",
		TraceID:         "trace-priv-01",
		SpanID:          "span-priv",
		ReqCapturedAtNs: 1700000000000000000,
		StatusCode:      models.U64Numeric(0),
		IsReplayable:    true,
		CreatedAt:       time.Now(),
	}
	if err := db.Create(&recPriv).Error; err != nil {
		t.Fatalf("插入私有记录失败: %v", err)
	}

	// 2. Bob 尝试横向越权访问 Alice 的 PRIVATE 批次
	// a. GET /api/recording-sessions/:id/transactions
	{
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("/api/recording-sessions/%d/transactions", privSession.ID), nil)
		req.Header.Set("Authorization", "Bearer "+bobToken)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("Bob 越权访问私有 transactions 期望 403 Forbidden，实际返回: %d", w.Code)
		}
	}

	// b. GET /api/recording-sessions/:id/transactions/:rootRecordId/sequence
	{
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("/api/recording-sessions/%d/transactions/%d/sequence", privSession.ID, recPriv.ID), nil)
		req.Header.Set("Authorization", "Bearer "+bobToken)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("Bob 越权访问私有 sequence 期望 403 Forbidden，实际返回: %d", w.Code)
		}
	}

	// c. GET /api/traffic-records/:id/replay-closure
	{
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("/api/traffic-records/%d/replay-closure", recPriv.ID), nil)
		req.Header.Set("Authorization", "Bearer "+bobToken)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("Bob 越权访问私有 replay-closure 期望 403 Forbidden，实际返回: %d", w.Code)
		}
	}

	// 3. 公共批次下利用 replay_task_id 尝试跨权窥探 Alice 的私有重放任务结果
	pubSession := models.RecordingSession{
		SessionNo:        "REC_PUB_SHARED_001",
		Name:             "公开共享批次",
		Scope:            models.ScopeShared,
		OwnerID:          alice.ID,
		Status:           "STOPPED",
		TargetProcessIDs: datatypes.JSON([]byte(`[10]`)),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	if err := db.Create(&pubSession).Error; err != nil {
		t.Fatalf("创建公开批次失败: %v", err)
	}
	recPub := models.TrafficRecord{
		SessionID:       pubSession.ID,
		SeqNo:           1,
		ProcessID:       10,
		Direction:       "INGRESS",
		ServiceName:     "PublicService",
		Operation:       "PublicOp",
		TraceID:         "trace-pub-01",
		SpanID:          "span-pub",
		ReqCapturedAtNs: 1700000000000000000,
		StatusCode:      models.U64Numeric(0),
		IsReplayable:    true,
		CreatedAt:       time.Now(),
	}
	if err := db.Create(&recPub).Error; err != nil {
		t.Fatalf("插入公开记录失败: %v", err)
	}

	// 插入关联架构对象以满足外键约束
	grp := models.ArchProcessGroup{Code: "ORDER_GRP_SEC", Name: "Order Group Sec"}
	_ = db.Create(&grp).Error
	archProc := models.ArchProcess{GroupID: grp.ID, Code: "order_proc_sec", Name: "Order Process Sec"}
	_ = db.Create(&archProc).Error

	agent := models.Agent{Name: "agent-sec", Endpoint: "http://127.0.0.1:9999", ClaimCode: "claim-1", Status: models.AgentStatusOnline}
	_ = db.Create(&agent).Error

	// Alice 创建私有重放任务
	taskAlice := models.ReplayTask{
		TaskNo:            "REP_ALICE_SEC_01",
		Name:              "Alice 私有重放任务",
		OwnerID:           alice.ID,
		SessionID:         &pubSession.ID,
		ArchProcessID:     archProc.ID,
		AgentID:           agent.ID,
		SelectedRecordIDs: datatypes.JSON([]byte(`[1]`)),
		ExtraIgnorePaths:  datatypes.JSON([]byte(`[]`)),
		LifecycleState:    "COMPLETED",
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}
	if err := db.Create(&taskAlice).Error; err != nil {
		t.Fatalf("创建 Alice 重放任务失败: %v", err)
	}

	// Bob 访问公开批次 sequence，但附带 Alice 的私有 taskID
	{
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("/api/recording-sessions/%d/transactions/%d/sequence?replay_task_id=%d", pubSession.ID, recPub.ID, taskAlice.ID), nil)
		req.Header.Set("Authorization", "Bearer "+bobToken)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("Bob 借公开批次窥探 Alice 重放任务期望 403 Forbidden，实际返回: %d %s", w.Code, w.Body.String())
		}
	}
}
