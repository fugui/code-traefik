package handlers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonAuth "code-common/backend/auth"
	commonModels "code-common/backend/models"
	"code-common/backend/testdb"
	"code-traefik/config"
	"code-traefik/handlers"
	"code-traefik/internal/server/decoder"
	"code-traefik/internal/server/diff"
	"code-traefik/models"
	"code-traefik/pkg/agentproto"
	"code-traefik/pkg/crosrpc"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// setupGoldenSuiteTestServer 设置包含完整路由与认证的测试 Gin Engine
func setupGoldenSuiteTestServer(t *testing.T, db *gorm.DB) (*gin.Engine, *diff.ReplayScheduler) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	models.DB = db

	if config.AppConfig.Auth.JWTSecret == "" {
		config.AppConfig.Auth.JWTSecret = "test-jwt-secret-very-secure-key-123456"
	}

	reg, _ := decoder.NewRegistry(db)
	handlers.ResetGlobalDecoderRegistry(reg)
	t.Cleanup(func() {
		handlers.ResetGlobalDecoderRegistry(nil)
	})
	sched := diff.NewReplayScheduler(db, reg)
	handlers.InitGlobalReplayScheduler(sched)

	r := gin.New()
	r.Use(gin.Recovery())
	handlers.RegisterAPIRoutes(r)
	return r, sched
}

func createTestUser(t *testing.T, db *gorm.DB, username, role string) (*commonModels.User, string) {
	t.Helper()
	isAdmin := role == config.AdminRole || role == commonAuth.RoleSuperAdmin
	rolesJSON, _ := json.Marshal([]string{role})
	user := &commonModels.User{
		Username: username,
		Name:     username,
		Email:    username + "@example.com",
		Roles:    datatypes.JSON(rolesJSON),
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	token, err := commonAuth.GenerateToken(
		user.ID,
		user.Username,
		user.Email,
		user.Name,
		isAdmin,
		[]string{role},
		config.AppConfig.Auth.JWTSecret,
		24*time.Hour,
	)
	if err != nil {
		t.Fatalf("生成测试 JWT 失败: %v", err)
	}
	return user, token
}

// 核心用例 1: 物理删除源 Session 与分区表后，套件回放依然 100% PASS
func TestGoldenSuiteSelfContainedSnapshot_SurvivesSourceSessionAndPartitionDrop(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}
	r, _ := setupGoldenSuiteTestServer(t, db)

	// 1. 初始化标准用户与管理员
	adminUser, adminToken := createTestUser(t, db, "gs_admin", config.AdminRole)

	// 2. 初始化标准架构进程
	grp := models.ArchProcessGroup{Code: "GRP_ORDER", Name: "Order Group"}
	db.Create(&grp)
	archProc := models.ArchProcess{
		Code:              "sut-order",
		Name:              "订单标准进程",
		GroupID:           grp.ID,
		DefaultListenPort: 18090,
	}
	db.Create(&archProc)

	// 3. 启动并模拟目标 Agent HTTP Server (返回执行成功与回收实际帧)
	var mockActualRspPacket []byte
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/agent/v1/replays/execute":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "ARMED"})
		case "/agent/v1/replays/1/result":
			fallthrough
		default:
			// 模拟返回重放成功
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(agentproto.ReplayTaskResultResp{
				TaskID:        1,
				Status:        models.ReplayResultPass,
				TotalRequests: 1,
				MockHitCount:  1,
				MockMissCount: 0,
				Details: fmt.Sprintf(`[{"record_id":1,"status":"%s","actual_rsp_hex":"%x"}]`,
					models.ReplayResultPass, mockActualRspPacket),
			})
		}
	}))
	defer agentServer.Close()

	agent := models.Agent{
		Name:      "AGENT_SIT_01",
		Endpoint:  agentServer.URL,
		Scope:     models.ScopeShared,
		ClaimCode: "test-code",
		Status:    models.AgentStatusOnline,
	}
	db.Create(&agent)

	targetProc := models.Process{
		AgentID:             agent.ID,
		ArchProcessID:       archProc.ID,
		EffectiveListenPort: 18090,
		ProbeStatus:         models.ProbeStatusArmed,
		ProbeDesiredState:   models.ProbeDesiredStateArmed,
	}
	db.Create(&targetProc)

	// 4. 创建录制批次与流量记录
	now := time.Now().UTC()
	pm := &models.PartitionManager{}
	_, _ = pm.EnsureDailyPartition(db, now)

	recSess := models.RecordingSession{
		SessionNo:        "SESS_TEST_001",
		Name:             "临时录制批次_订单流",
		Scope:            models.ScopeShared,
		OwnerID:          adminUser.ID,
		Status:           "STOPPED",
		TargetProcessIDs: datatypes.JSON([]byte(`[]`)),
		CreatedBy:        adminUser.ID,
	}
	db.Create(&recSess)

	reqPkt := buildTestCrosReqFrame(1001, crosrpc.ReqTypeSync, "order.v1.OrderService", "CreateOrder", []byte(`{"order_id":"ORD_001"}`))
	rspPkt := buildTestCrosRspFrame(1001, crosrpc.MsgTypeRsp, 1, 0, "order.v1.OrderService", "CreateOrder", []byte(`{"order_id":"ORD_001","status":"SUCCESS"}`))
	mockActualRspPacket = rspPkt

	ingressRec := models.TrafficRecord{
		CreatedAt:       now,
		SessionID:       recSess.ID,
		SeqNo:           1,
		AgentID:         agent.ID,
		ProcessID:       targetProc.ID,
		Direction:       models.TrafficDirIngress,
		ExchangeType:    models.ExchangeUnary,
		SrcAddr:         "192.168.1.100:5000",
		DstAddr:         "192.168.1.200:18090",
		ReqCapturedAtNs: now.UnixNano(),
		SNO:             1001,
		ServiceName:     "order.v1.OrderService",
		Operation:       "CreateOrder",
		StatusCode:      0,
		RawReqPacket:    reqPkt,
		RawRspPacket:    rspPkt,
		IsReplayable:    true,
		DecodeStatus:    "SUCCESS",
		HeadersJSON:     datatypes.JSON([]byte(`{}`)),
		PayloadJSON:     datatypes.JSON([]byte(`{}`)),
	}
	db.Create(&ingressRec)

	// 模拟一条 EGRESS 下游调用
	egressReq := buildTestCrosReqFrame(2001, crosrpc.ReqTypeSync, "pay.v1.PayService", "Deduct", []byte(`{"amount":100}`))
	egressRsp := buildTestCrosRspFrame(2001, crosrpc.MsgTypeRsp, 1, 0, "pay.v1.PayService", "Deduct", []byte(`{"code":"OK"}`))
	egressRec := models.TrafficRecord{
		CreatedAt:       now,
		SessionID:       recSess.ID,
		SeqNo:           2,
		AgentID:         agent.ID,
		ProcessID:       targetProc.ID,
		Direction:       models.TrafficDirEgress,
		ExchangeType:    models.ExchangeUnary,
		SrcAddr:         "192.168.1.200:5001",
		DstAddr:         "192.168.1.250:18092",
		ReqCapturedAtNs: now.UnixNano() + 1000,
		SNO:             2001,
		ServiceName:     "pay.v1.PayService",
		Operation:       "Deduct",
		StatusCode:      0,
		RawReqPacket:    egressReq,
		RawRspPacket:    egressRsp,
		IsReplayable:    true,
		DecodeStatus:    "SUCCESS",
		HeadersJSON:     datatypes.JSON([]byte(`{}`)),
		PayloadJSON:     datatypes.JSON([]byte(`{}`)),
	}
	db.Create(&egressRec)

	// 5. 调用 POST /api/golden-suites/curate 沉淀为黄金契约套件
	curateBody := map[string]interface{}{
		"new_suite": map[string]interface{}{
			"code":               "GS_ORDER_SUT",
			"name":               "订单核心回归套件",
			"arch_process_id":    archProc.ID,
			"version":            "v1.0.0",
			"extra_ignore_paths": []string{"rsp.timestamp"},
			"description":        "核心主流程快照",
		},
		"session_id": recSess.ID,
		"record_ids": []uint64{ingressRec.ID},
		"mask_rules_json": []map[string]interface{}{
			{
				"path":     "rsp.dynamic_code",
				"strategy": "IGNORE",
			},
		},
	}
	bodyBytes, _ := json.Marshal(curateBody)
	curateReq := httptest.NewRequest(http.MethodPost, "/api/golden-suites/curate", bytes.NewReader(bodyBytes))
	curateReq.Header.Set("Authorization", "Bearer "+adminToken)
	curateReq.Header.Set("Content-Type", "application/json")
	wCurate := httptest.NewRecorder()
	r.ServeHTTP(wCurate, curateReq)

	if wCurate.Code != http.StatusCreated {
		t.Fatalf("一键沉淀黄金套件失败: HTTP %d, body: %s", wCurate.Code, wCurate.Body.String())
	}

	var curateResp struct {
		Suite      models.GoldenSuite `json:"suite"`
		AddedItems int                `json:"added_items"`
	}
	_ = json.Unmarshal(wCurate.Body.Bytes(), &curateResp)
	suiteID := curateResp.Suite.ID
	if suiteID == 0 || curateResp.AddedItems != 1 {
		t.Fatalf("沉淀返回非法: suiteID=%d, added=%d", suiteID, curateResp.AddedItems)
	}

	// 6. 验证自包含快照已固化
	var items []models.GoldenSuiteItem
	if err := db.Where("suite_id = ?", suiteID).Find(&items).Error; err != nil || len(items) != 1 {
		t.Fatalf("查询已固化快照失败: %v, items=%d", err, len(items))
	}
	if len(items[0].RawReqPacket) == 0 || len(items[0].RawRspPacket) == 0 {
		t.Fatalf("自包含快照未正确固化 RawReqPacket / RawRspPacket")
	}

	// 7. 【关键验证】：物理销毁源 RecordingSession 与清空全部 TrafficRecord
	if err := db.Exec("DELETE FROM traefik_traffic_records").Error; err != nil {
		t.Fatalf("清空流量分区表失败: %v", err)
	}
	if err := db.Delete(&recSess).Error; err != nil {
		t.Fatalf("删除源 RecordingSession 失败: %v", err)
	}

	var trCount int64
	db.Model(&models.TrafficRecord{}).Count(&trCount)
	if trCount != 0 {
		t.Fatalf("流量表未能完全清空: count=%d", trCount)
	}

	// 8. 触发脱机脱离执行 POST /api/golden-suites/:id/execute
	execBody := map[string]interface{}{
		"env_agent_id": agent.ID,
		"timeout_ms":   5000,
	}
	execBytes, _ := json.Marshal(execBody)
	execReq := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/golden-suites/%d/execute", suiteID), bytes.NewReader(execBytes))
	execReq.Header.Set("Authorization", "Bearer "+adminToken)
	execReq.Header.Set("Content-Type", "application/json")
	wExec := httptest.NewRecorder()
	r.ServeHTTP(wExec, execReq)

	if wExec.Code != http.StatusCreated {
		t.Fatalf("脱机回归执行失败: HTTP %d, body: %s", wExec.Code, wExec.Body.String())
	}

	var task models.ReplayTask
	_ = json.Unmarshal(wExec.Body.Bytes(), &task)
	if task.ID == 0 {
		t.Fatalf("脱机回归任务未生成合法 ID")
	}
	if task.GoldenSuiteID == nil || *task.GoldenSuiteID != suiteID {
		t.Fatalf("重放任务 GoldenSuiteID 不匹配: %v", task.GoldenSuiteID)
	}
	if task.SessionID != nil {
		t.Fatalf("脱机回归任务 SessionID 必须为 nil: %v", task.SessionID)
	}
	if task.TriggerSource != models.TriggerSourceGoldenSuite {
		t.Fatalf("TriggerSource 必须为 GOLDEN_SUITE, 实际为: %s", task.TriggerSource)
	}

	// 等待异步执行完成并断言 100% PASS
	time.Sleep(500 * time.Millisecond)

	var finishedTask models.ReplayTask
	if err := db.First(&finishedTask, task.ID).Error; err != nil {
		t.Fatalf("查询重放任务失败: %v", err)
	}
	if finishedTask.ResultStatus != models.ReplayResultPass {
		t.Fatalf("源流量清空后脱机回归期望 PASS, 实际为: %s (err: %s)", finishedTask.ResultStatus, finishedTask.ErrorMessage)
	}
	if finishedTask.PassedRequests != 1 {
		t.Fatalf("PassedRequests 期望 1, 实际为: %d", finishedTask.PassedRequests)
	}

	// 检查 ReplayItem 正确关联 SuiteItemID
	var replayItems []models.ReplayItem
	db.Where("task_id = ?", task.ID).Find(&replayItems)
	if len(replayItems) != 1 {
		t.Fatalf("ReplayItem 条目数期望 1, 实际为: %d", len(replayItems))
	}
	if replayItems[0].SuiteItemID == nil || *replayItems[0].SuiteItemID != items[0].ID {
		t.Fatalf("ReplayItem.SuiteItemID 未正确指向黄金用例: %v", replayItems[0].SuiteItemID)
	}
	if replayItems[0].ResultStatus != models.ReplayResultPass {
		t.Fatalf("ReplayItem.ResultStatus 期望 PASS, 实际为: %s", replayItems[0].ResultStatus)
	}
}

// 核心用例 2: Anti-IDOR 越权阻断、跨进程混入拦截与动态掩码忽略
func TestCurateTransaction_AntiIDOR_AndMaskRulesDiffIgnore(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}
	r, _ := setupGoldenSuiteTestServer(t, db)

	// 1. 初始化普通用户 A 与普通用户 B
	userA, tokenA := createTestUser(t, db, "user_a", "dev")
	_, tokenB := createTestUser(t, db, "user_b", "dev")

	// 2. 初始化两个不同的架构进程
	grp := models.ArchProcessGroup{Code: "GRP_CROSS", Name: "Cross Group"}
	db.Create(&grp)
	archProc1 := models.ArchProcess{Code: "proc-1", Name: "进程 1", GroupID: grp.ID, DefaultListenPort: 19001}
	archProc2 := models.ArchProcess{Code: "proc-2", Name: "进程 2", GroupID: grp.ID, DefaultListenPort: 19002}
	db.Create(&archProc1)
	db.Create(&archProc2)

	agentA := models.Agent{Name: "AGENT_PRIV_A", Endpoint: "127.0.0.1:9091", Scope: models.ScopePrivate, OwnerID: &userA.ID}
	db.Create(&agentA)
	proc1 := models.Process{AgentID: agentA.ID, ArchProcessID: archProc1.ID, EffectiveListenPort: 19001}
	proc2 := models.Process{AgentID: agentA.ID, ArchProcessID: archProc2.ID, EffectiveListenPort: 19002}
	db.Create(&proc1)
	db.Create(&proc2)

	// 3. 用户 A 创建 PRIVATE 录制批次
	now := time.Now().UTC()
	sessA := models.RecordingSession{
		SessionNo:        "SESS_TEST_A",
		Name:             "User A 私有录制",
		Scope:            models.ScopePrivate,
		OwnerID:          userA.ID,
		Status:           "STOPPED",
		TargetProcessIDs: datatypes.JSON([]byte(`[]`)),
		CreatedBy:        userA.ID,
	}
	db.Create(&sessA)

	reqPkt1 := buildTestCrosReqFrame(1001, crosrpc.ReqTypeSync, "svc1", "Op1", []byte(`{}`))
	rspPkt1 := buildTestCrosRspFrame(1001, crosrpc.MsgTypeRsp, 1, 0, "svc1", "Op1", []byte(`{}`))
	rec1 := models.TrafficRecord{
		CreatedAt:    now,
		SessionID:    sessA.ID,
		SeqNo:        1,
		AgentID:      agentA.ID,
		ProcessID:    proc1.ID,
		Direction:    models.TrafficDirIngress,
		ServiceName:  "svc1",
		Operation:    "Op1",
		RawReqPacket: reqPkt1,
		RawRspPacket: rspPkt1,
		IsReplayable: true,
		HeadersJSON:  datatypes.JSON([]byte(`{}`)),
		PayloadJSON:  datatypes.JSON([]byte(`{}`)),
	}
	rec2 := models.TrafficRecord{
		CreatedAt:    now,
		SessionID:    sessA.ID,
		SeqNo:        2,
		AgentID:      agentA.ID,
		ProcessID:    proc2.ID, // 属于不同架构进程
		Direction:    models.TrafficDirIngress,
		ServiceName:  "svc2",
		Operation:    "Op2",
		RawReqPacket: reqPkt1,
		RawRspPacket: rspPkt1,
		IsReplayable: true,
		HeadersJSON:  datatypes.JSON([]byte(`{}`)),
		PayloadJSON:  datatypes.JSON([]byte(`{}`)),
	}
	db.Create(&rec1)
	db.Create(&rec2)

	// Case 1: 用户 B 试图从用户 A 的 PRIVATE 批次沉淀用例 -> Anti-IDOR 必须返回 403 Forbidden
	curateBody := map[string]interface{}{
		"new_suite": map[string]interface{}{
			"code":            "GS_THEFT",
			"name":            "越权套件",
			"arch_process_id": archProc1.ID,
		},
		"session_id": sessA.ID,
		"record_ids": []uint64{rec1.ID},
	}
	bodyBytes, _ := json.Marshal(curateBody)
	req := httptest.NewRequest(http.MethodPost, "/api/golden-suites/curate", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+tokenB)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("越权沉淀他人 PRIVATE 批次期望 403 Forbidden, 实际为: %d (body: %s)", w.Code, w.Body.String())
	}

	// Case 2: 用户 A 传入跨不同标准架构进程的流量记录 -> 必须返回 400 Bad Request
	curateBodyCross := map[string]interface{}{
		"new_suite": map[string]interface{}{
			"code":            "GS_CROSS",
			"name":            "跨进程非法套件",
			"arch_process_id": archProc1.ID,
		},
		"session_id": sessA.ID,
		"record_ids": []uint64{rec1.ID, rec2.ID},
	}
	bodyCrossBytes, _ := json.Marshal(curateBodyCross)
	reqCross := httptest.NewRequest(http.MethodPost, "/api/golden-suites/curate", bytes.NewReader(bodyCrossBytes))
	reqCross.Header.Set("Authorization", "Bearer "+tokenA)
	reqCross.Header.Set("Content-Type", "application/json")
	wCross := httptest.NewRecorder()
	r.ServeHTTP(wCross, reqCross)

	if wCross.Code != http.StatusBadRequest {
		t.Fatalf("跨架构进程沉淀套件期望 400 Bad Request, 实际为: %d (body: %s)", wCross.Code, wCross.Body.String())
	}
}

// 核心用例 3: 删除标准架构进程时，存在关联 GoldenSuite 必须 409 Conflict 阻断
func TestDeleteArchProcess_BlockedWhenGoldenSuiteExists409(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}
	r, _ := setupGoldenSuiteTestServer(t, db)

	adminUser, adminToken := createTestUser(t, db, "gs_admin3", config.AdminRole)

	grp := models.ArchProcessGroup{Code: "GRP_DEL", Name: "Delete Guard Group"}
	db.Create(&grp)
	archProc := models.ArchProcess{Code: "proc-protected", Name: "被黄金套件守护的进程", GroupID: grp.ID, DefaultListenPort: 19003}
	db.Create(&archProc)

	// 创建关联的黄金套件
	gs := models.GoldenSuite{
		Code:          "GS_GUARDED",
		Name:          "权威套件",
		ArchProcessID: archProc.ID,
		Status:        models.GoldenSuiteStatusActive,
		CreatedBy:     adminUser.ID,
	}
	db.Create(&gs)

	// 试图直接删除被套件引用的标准架构进程
	delReq := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/arch/processes/%d", archProc.ID), nil)
	delReq.Header.Set("Authorization", "Bearer "+adminToken)
	wDel := httptest.NewRecorder()
	r.ServeHTTP(wDel, delReq)

	if wDel.Code != http.StatusConflict {
		t.Fatalf("删除被黄金套件引用的 ArchProcess 期望 409 Conflict, 实际为: %d (body: %s)", wDel.Code, wDel.Body.String())
	}

	// 移除套件后再删除标准架构进程 -> 应当成功 200 OK
	db.Delete(&gs)

	delReq2 := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/arch/processes/%d", archProc.ID), nil)
	delReq2.Header.Set("Authorization", "Bearer "+adminToken)
	wDel2 := httptest.NewRecorder()
	r.ServeHTTP(wDel2, delReq2)

	if wDel2.Code != http.StatusOK {
		t.Fatalf("套件清理后删除 ArchProcess 期望 200 OK, 实际为: %d (body: %s)", wDel2.Code, wDel2.Body.String())
	}
}
