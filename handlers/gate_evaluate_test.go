package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-traefik/config"
	"code-traefik/handlers"
	"code-traefik/internal/server/hub"
	"code-traefik/models"
	"code-traefik/pkg/agentproto"
	"code-traefik/pkg/crosrpc"
)

// 核心用例 1: CI 机器 Token 生成轮换、SHA-256 常量时间鉴权、拒绝 URL Query 传参与吊销
func TestCITokenRotate_ConstantTimeAuth_RejectQueryToken_AndRevoke(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}
	r, _ := setupGoldenSuiteTestServer(t, db)

	_, adminToken := createTestUser(t, db, "admin_user", config.AdminRole)
	_, nonAdminToken := createTestUser(t, db, "dev_user", "developer")

	// 1. 普通用户无权轮换 CI Token -> 403 Forbidden
	reqRotateNonAdmin := httptest.NewRequest(http.MethodPost, "/api/admin/ci-token/rotate", bytes.NewReader([]byte("{}")))
	reqRotateNonAdmin.Header.Set("Authorization", "Bearer "+nonAdminToken)
	wRotateNonAdmin := httptest.NewRecorder()
	r.ServeHTTP(wRotateNonAdmin, reqRotateNonAdmin)
	if wRotateNonAdmin.Code != http.StatusForbidden {
		t.Fatalf("期望非管理员轮换 CI Token 返回 403，实际: %d", wRotateNonAdmin.Code)
	}

	// 2. 管理员轮换 CI Token -> 200 OK 并强制 Cache-Control: no-store
	rotateBody, _ := json.Marshal(map[string]string{"description": "GitLab CI 门禁流水线凭据"})
	reqRotate := httptest.NewRequest(http.MethodPost, "/api/admin/ci-token/rotate", bytes.NewReader(rotateBody))
	reqRotate.Header.Set("Authorization", "Bearer "+adminToken)
	reqRotate.Header.Set("Content-Type", "application/json")
	wRotate := httptest.NewRecorder()
	r.ServeHTTP(wRotate, reqRotate)

	if wRotate.Code != http.StatusOK {
		t.Fatalf("期望管理员轮换 CI Token 返回 200，实际: %d, body: %s", wRotate.Code, wRotate.Body.String())
	}
	cacheControl := wRotate.Header().Get("Cache-Control")
	if !strings.Contains(cacheControl, "no-store") {
		t.Errorf("期望响应头 Cache-Control 包含 no-store，实际: %s", cacheControl)
	}

	var rotateResp struct {
		CIToken   string `json:"ci_token"`
		RotatedAt string `json:"rotated_at"`
	}
	if err := json.Unmarshal(wRotate.Body.Bytes(), &rotateResp); err != nil {
		t.Fatalf("解析轮换响应失败: %v", err)
	}
	if !strings.HasPrefix(rotateResp.CIToken, "ctk_") || len(rotateResp.CIToken) < 64 {
		t.Fatalf("期望高熵机器 Token 前缀为 ctk_ 且长度大于 64，实际: %s", rotateResp.CIToken)
	}

	// 3. 校验数据库：仅持久化 SHA-256 摘要，绝无明文
	var setting models.TraefikSetting
	if err := db.Where("setting_key = ?", handlers.SettingKeyCIGateTokenSHA256).First(&setting).Error; err != nil {
		t.Fatalf("查询 traefik_settings 失败: %v", err)
	}
	if strings.Contains(setting.SettingVal, "ctk_") {
		t.Fatalf("安全红线违规：数据库中持久化了明文 Token: %s", setting.SettingVal)
	}
	if len(setting.SettingVal) != 64 {
		t.Fatalf("期望 SHA-256 哈希长度为 64，实际: %d", len(setting.SettingVal))
	}

	// 4. 读取通用设置 GET /api/admin/settings：自动屏蔽 ci_gate_token_sha256 内部键，并回传 ci_token_configured: true
	reqGetSettings := httptest.NewRequest(http.MethodGet, "/api/admin/settings", nil)
	reqGetSettings.Header.Set("Authorization", "Bearer "+adminToken)
	wGetSettings := httptest.NewRecorder()
	r.ServeHTTP(wGetSettings, reqGetSettings)
	if wGetSettings.Code != http.StatusOK {
		t.Fatalf("GET /api/admin/settings 失败: %d", wGetSettings.Code)
	}
	var settingsResp struct {
		Settings          []models.TraefikSetting `json:"settings"`
		CITokenConfigured bool                    `json:"ci_token_configured"`
	}
	_ = json.Unmarshal(wGetSettings.Body.Bytes(), &settingsResp)
	if !settingsResp.CITokenConfigured {
		t.Errorf("期望 ci_token_configured 为 true")
	}
	for _, s := range settingsResp.Settings {
		if s.SettingKey == handlers.SettingKeyCIGateTokenSHA256 {
			t.Fatalf("安全红线违规：通用设置列表中泄露了内部哈希项 %s", s.SettingKey)
		}
	}

	// 5. 禁止通过通用 PUT /api/admin/settings 修改保留键
	putForbiddenBody, _ := json.Marshal(map[string]interface{}{
		"items": []map[string]string{
			{
				"setting_key": handlers.SettingKeyCIGateTokenSHA256,
				"setting_val": "tampered_hash",
			},
		},
	})
	reqPutForbidden := httptest.NewRequest(http.MethodPut, "/api/admin/settings", bytes.NewReader(putForbiddenBody))
	reqPutForbidden.Header.Set("Authorization", "Bearer "+adminToken)
	reqPutForbidden.Header.Set("Content-Type", "application/json")
	wPutForbidden := httptest.NewRecorder()
	r.ServeHTTP(wPutForbidden, reqPutForbidden)
	if wPutForbidden.Code != http.StatusBadRequest {
		t.Fatalf("期望禁止修改保留键返回 400，实际: %d", wPutForbidden.Code)
	}

	// 6. 门禁中间件严格拦截在 URL Query 中传递 Token 的行为 -> 401
	evalQueryReq := httptest.NewRequest(http.MethodPost, "/api/gates/evaluate?token="+rotateResp.CIToken, bytes.NewReader([]byte("{}")))
	wQuery := httptest.NewRecorder()
	r.ServeHTTP(wQuery, evalQueryReq)
	if wQuery.Code != http.StatusUnauthorized {
		t.Fatalf("期望 URL Query 传参被 401 拦截，实际: %d", wQuery.Code)
	}

	evalQueryCIReq := httptest.NewRequest(http.MethodPost, "/api/gates/evaluate?ci_token="+rotateResp.CIToken, bytes.NewReader([]byte("{}")))
	wQueryCI := httptest.NewRecorder()
	r.ServeHTTP(wQueryCI, evalQueryCIReq)
	if wQueryCI.Code != http.StatusUnauthorized {
		t.Fatalf("期望 URL Query ci_token 被 401 拦截，实际: %d", wQueryCI.Code)
	}

	// 7. 携带错误或伪造 Token -> 401 Unauthorized
	evalWrongReq := httptest.NewRequest(http.MethodPost, "/api/gates/evaluate", bytes.NewReader([]byte("{}")))
	evalWrongReq.Header.Set("X-Traefik-CI-Token", "ctk_fake_invalid_token_1234567890abcdef")
	wWrong := httptest.NewRecorder()
	r.ServeHTTP(wWrong, evalWrongReq)
	if wWrong.Code != http.StatusUnauthorized {
		t.Fatalf("期望错误 Token 返回 401，实际: %d", wWrong.Code)
	}

	// 8. 管理员吊销 CI Token -> 200 OK
	reqRevoke := httptest.NewRequest(http.MethodDelete, "/api/admin/ci-token", nil)
	reqRevoke.Header.Set("Authorization", "Bearer "+adminToken)
	wRevoke := httptest.NewRecorder()
	r.ServeHTTP(wRevoke, reqRevoke)
	if wRevoke.Code != http.StatusOK {
		t.Fatalf("期望管理员吊销 CI Token 返回 200，实际: %d", wRevoke.Code)
	}

	// 吊销后再用原 Token 访问 -> 401
	evalAfterRevoke := httptest.NewRequest(http.MethodPost, "/api/gates/evaluate", bytes.NewReader([]byte("{}")))
	evalAfterRevoke.Header.Set("X-Traefik-CI-Token", rotateResp.CIToken)
	wAfterRevoke := httptest.NewRecorder()
	r.ServeHTTP(wAfterRevoke, evalAfterRevoke)
	if wAfterRevoke.Code != http.StatusUnauthorized {
		t.Fatalf("期望吊销后凭据返回 401，实际: %d", wAfterRevoke.Code)
	}
}

// 核心用例 2: 第一关架构防漂移强阻断，实测出站边包含未报备调用返回 422 GATED_VIOLATION
func TestGateEvaluate_ObservedEgressCallsBlocksViolationWith422(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}
	r, _ := setupGoldenSuiteTestServer(t, db)
	_, adminToken := createTestUser(t, db, "admin_eval_1", config.AdminRole)

	// 1. 生成 CI 机器凭据
	reqRotate := httptest.NewRequest(http.MethodPost, "/api/admin/ci-token/rotate", bytes.NewReader([]byte("{}")))
	reqRotate.Header.Set("Authorization", "Bearer "+adminToken)
	wRotate := httptest.NewRecorder()
	r.ServeHTTP(wRotate, reqRotate)
	var rot struct {
		CIToken string `json:"ci_token"`
	}
	_ = json.Unmarshal(wRotate.Body.Bytes(), &rot)
	ciToken := rot.CIToken

	// 2. 初始化标准架构进程与功能模块
	subsys := models.Subsystem{Code: "SUB_ORDER_1", Name: "Order Subsys"}
	db.Create(&subsys)
	fg := models.FunctionalGroup{SubsystemID: subsys.ID, Code: "FG_ORDER_1", Name: "Order FG"}
	db.Create(&fg)

	grp := models.ArchProcessGroup{Code: "GRP_ORDER_1", Name: "Order Group"}
	db.Create(&grp)
	archProc := models.ArchProcess{
		Code:              "sut-order-viol",
		Name:              "订单服务进程-架构漂移",
		GroupID:           grp.ID,
		DefaultListenPort: 18091,
	}
	db.Create(&archProc)

	mod := models.Module{
		SubsystemID:   subsys.ID,
		GroupID:       fg.ID,
		Code:          "MOD_ORDER_CORE",
		Name:          "订单核心模块",
		ArchProcessID: &archProc.ID,
	}
	db.Create(&mod)

	// 仅声明依赖了 com.demo.PaymentService
	targetMod := models.Module{
		SubsystemID: subsys.ID,
		GroupID:     fg.ID,
		Code:        "MOD_PAYMENT",
		Name:        "支付模块",
	}
	db.Create(&targetMod)
	paySvc := models.ModuleService{
		ModuleID:    targetMod.ID,
		ServiceName: "com.demo.PaymentService",
	}
	db.Create(&paySvc)
	dep := models.ModuleDependency{
		ConsumerModuleID: mod.ID,
		ServiceID:        &paySvc.ID,
		ProviderModuleID: &targetMod.ID,
	}
	db.Create(&dep)

	// 3. 创建黄金套件及用例
	suite := models.GoldenSuite{
		Code:          "SUITE_ORDER_VIOL",
		Name:          "订单回归套件-漂移拦截",
		ArchProcessID: archProc.ID,
		Status:        "ACTIVE",
	}
	db.Create(&suite)

	reqPkt := buildTestCrosReqFrame(1001, crosrpc.ReqTypeSync, "com.demo.OrderService", "CreateOrder", []byte("in_data"))
	rspPkt := buildTestCrosRspFrame(1001, crosrpc.MsgTypeRsp, 1, 0, "com.demo.OrderService", "CreateOrder", []byte("out_data"))
	item := models.GoldenSuiteItem{
		SuiteID:        suite.ID,
		OrderNo:        1,
		Name:           "下单回归用例",
		ServiceName:    "com.demo.OrderService",
		Operation:      "CreateOrder",
		RawReqPacket:   reqPkt,
		RawRspPacket:   rspPkt,
		BaselineStatus: 0,
	}
	db.Create(&item)

	// 4. 模拟 Agent 返回实测出站边：包含未报备的 com.demo.UnknownUnregisteredService 调用
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/agent/v1/replays/execute":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "RUNNING"})
		default:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(agentproto.ReplayTaskResultResp{
				TaskID:         1,
				Status:         models.ReplayResultPass,
				TotalRequests:  1,
				PassedRequests: 1,
				ObservedEgressCalls: []agentproto.ObservedEgressCall{
					{
						ServiceName: "com.demo.PaymentService",
						Operation:   "Pay",
						DstAddr:     "10.0.0.1:8080",
						CallCount:   1,
						MatchStatus: "SCOPED_HIT",
					},
					{
						ServiceName: "com.demo.UnknownUnregisteredService", // 未报备违规边
						Operation:   "Probe",
						DstAddr:     "10.0.0.99:9999",
						CallCount:   2,
						MatchStatus: "MOCK_MISS",
					},
				},
				Details: fmt.Sprintf(`[{"record_id":%d,"status":"%s","actual_rsp_hex":"%x"}]`,
					item.ID, models.ReplayResultPass, rspPkt),
			})
		}
	}))
	defer agentServer.Close()

	agent := models.Agent{
		Name:      "AGENT_EVAL_SHARED_01",
		Endpoint:  agentServer.URL,
		Scope:     models.ScopeShared,
		Status:    models.AgentStatusOnline,
		ClaimCode: "code-1",
	}
	db.Create(&agent)

	proc := models.Process{
		AgentID:             agent.ID,
		ArchProcessID:       archProc.ID,
		EffectiveListenPort: 18091,
		ProbeStatus:         models.ProbeStatusArmed,
	}
	db.Create(&proc)

	// 5. 流水线调用 POST /api/gates/evaluate
	evalReqBody, _ := json.Marshal(handlers.GateEvaluateReq{
		TargetArchProcessID: archProc.ID,
		EnvAgentID:          agent.ID,
		GoldenSuiteID:       &suite.ID,
		TimeoutSeconds:      10,
	})
	evalReq := httptest.NewRequest(http.MethodPost, "/api/gates/evaluate", bytes.NewReader(evalReqBody))
	evalReq.Header.Set("X-Traefik-CI-Token", ciToken)
	evalReq.Header.Set("Content-Type", "application/json")
	wEval := httptest.NewRecorder()
	r.ServeHTTP(wEval, evalReq)

	// 6. 断言结果：422 Unprocessable Entity 强阻断，gate_status 为 GATED_VIOLATION
	if wEval.Code != http.StatusUnprocessableEntity {
		t.Fatalf("期望违规边返回 422 阻断，实际状态码: %d, body: %s", wEval.Code, wEval.Body.String())
	}

	var evalResp handlers.GateEvaluateResp
	if err := json.Unmarshal(wEval.Body.Bytes(), &evalResp); err != nil {
		t.Fatalf("解析门禁响应失败: %v", err)
	}

	if evalResp.GateStatus != "GATED_VIOLATION" {
		t.Errorf("期望 gate_status 为 GATED_VIOLATION，实际: %s", evalResp.GateStatus)
	}
	if evalResp.ViolationCount != 1 {
		t.Errorf("期望 ViolationCount 为 1，实际: %d", evalResp.ViolationCount)
	}
	if len(evalResp.Violations) != 1 || evalResp.Violations[0].ServiceName != "com.demo.UnknownUnregisteredService" {
		t.Errorf("期望检测到违规服务 com.demo.UnknownUnregisteredService，实际: %+v", evalResp.Violations)
	}
	if !strings.Contains(evalResp.MarkdownReport, "GATED_VIOLATION") || !strings.Contains(evalResp.MarkdownReport, "com.demo.UnknownUnregisteredService") {
		t.Errorf("期望 Markdown 报告包含违规服务与状态，实际:\n%s", evalResp.MarkdownReport)
	}
	if !strings.Contains(evalResp.MarkdownReport, "/traefik/debug?replay_task_id=") {
		t.Errorf("期望 Markdown 报告包含 /traefik/debug 快速排障深链，实际:\n%s", evalResp.MarkdownReport)
	}
}

// 核心用例 3: 第二关黄金套件防劣化强阻断，契约 Diff 失败返回 422 GATED_BREAKING_DIFF
func TestGateEvaluate_BlocksBreakingDiffWith422(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}
	r, _ := setupGoldenSuiteTestServer(t, db)
	_, adminToken := createTestUser(t, db, "admin_eval_2", config.AdminRole)

	// 1. 生成 CI 机器凭据
	reqRotate := httptest.NewRequest(http.MethodPost, "/api/admin/ci-token/rotate", bytes.NewReader([]byte("{}")))
	reqRotate.Header.Set("Authorization", "Bearer "+adminToken)
	wRotate := httptest.NewRecorder()
	r.ServeHTTP(wRotate, reqRotate)
	var rot struct {
		CIToken string `json:"ci_token"`
	}
	_ = json.Unmarshal(wRotate.Body.Bytes(), &rot)
	ciToken := rot.CIToken

	// 2. 初始化标准架构进程
	grp := models.ArchProcessGroup{Code: "GRP_ORDER_2", Name: "Order Group 2"}
	db.Create(&grp)
	archProc := models.ArchProcess{
		Code:              "sut-order-diff",
		Name:              "订单服务进程-回归劣化",
		GroupID:           grp.ID,
		DefaultListenPort: 18092,
	}
	db.Create(&archProc)

	// 3. 创建黄金套件及用例 (基线期望状态码为 0 且 payload="expected_ok")
	suite := models.GoldenSuite{
		Code:          "SUITE_ORDER_DIFF",
		Name:          "订单回归套件-破坏性变更拦截",
		ArchProcessID: archProc.ID,
		Status:        "ACTIVE",
	}
	db.Create(&suite)

	reqPkt := buildTestCrosReqFrame(2001, crosrpc.ReqTypeSync, "com.demo.OrderService", "SubmitOrder", []byte("req_payload"))
	expRspPkt := buildTestCrosRspFrame(2001, crosrpc.MsgTypeRsp, 1, 0, "com.demo.OrderService", "SubmitOrder", []byte("expected_ok"))
	// 实际返回状态码为 500 (发生劣化异常)
	actRspPkt := buildTestCrosRspFrame(2001, crosrpc.MsgTypeRsp, 1, 500, "com.demo.OrderService", "SubmitOrder", []byte("internal_err"))

	item := models.GoldenSuiteItem{
		SuiteID:        suite.ID,
		OrderNo:        1,
		Name:           "提交订单核心用例",
		ServiceName:    "com.demo.OrderService",
		Operation:      "SubmitOrder",
		RawReqPacket:   reqPkt,
		RawRspPacket:   expRspPkt,
		BaselineStatus: 0,
	}
	db.Create(&item)

	// 4. 模拟 Agent 返回契约劣化结果 (出站边全部合法，但协议状态码异常)
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/agent/v1/replays/execute":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "RUNNING"})
		default:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(agentproto.ReplayTaskResultResp{
				TaskID:              2,
				Status:              models.ReplayResultFailed,
				TotalRequests:       1,
				PassedRequests:      0,
				ObservedEgressCalls: []agentproto.ObservedEgressCall{}, // 0 违规边
				Details: fmt.Sprintf(`[{"record_id":%d,"status":"%s","actual_rsp_hex":"%x"}]`,
					item.ID, models.ReplayResultFailed, actRspPkt),
			})
		}
	}))
	defer agentServer.Close()

	agent := models.Agent{
		Name:      "AGENT_EVAL_SHARED_02",
		Endpoint:  agentServer.URL,
		Scope:     models.ScopeShared,
		Status:    models.AgentStatusOnline,
		ClaimCode: "code-2",
	}
	db.Create(&agent)

	proc := models.Process{
		AgentID:             agent.ID,
		ArchProcessID:       archProc.ID,
		EffectiveListenPort: 18092,
		ProbeStatus:         models.ProbeStatusArmed,
	}
	db.Create(&proc)

	// 5. 流水线调用 POST /api/gates/evaluate
	evalReqBody, _ := json.Marshal(handlers.GateEvaluateReq{
		TargetArchProcessID: archProc.ID,
		EnvAgentID:          agent.ID,
		GoldenSuiteID:       &suite.ID,
		TimeoutSeconds:      10,
	})
	evalReq := httptest.NewRequest(http.MethodPost, "/api/gates/evaluate", bytes.NewReader(evalReqBody))
	evalReq.Header.Set("X-Traefik-CI-Token", ciToken)
	evalReq.Header.Set("Content-Type", "application/json")
	wEval := httptest.NewRecorder()
	r.ServeHTTP(wEval, evalReq)

	// 6. 断言结果：422 Unprocessable Entity 强阻断，gate_status 为 GATED_BREAKING_DIFF
	if wEval.Code != http.StatusUnprocessableEntity {
		t.Fatalf("期望契约劣化返回 422 阻断，实际状态码: %d, body: %s", wEval.Code, wEval.Body.String())
	}

	var evalResp handlers.GateEvaluateResp
	if err := json.Unmarshal(wEval.Body.Bytes(), &evalResp); err != nil {
		t.Fatalf("解析门禁响应失败: %v", err)
	}

	if evalResp.GateStatus != "GATED_BREAKING_DIFF" {
		t.Errorf("期望 gate_status 为 GATED_BREAKING_DIFF，实际: %s", evalResp.GateStatus)
	}
	if len(evalResp.FailedItems) != 1 {
		t.Errorf("期望 failed_items 长度为 1，实际: %d", len(evalResp.FailedItems))
	}
	if !strings.Contains(evalResp.MarkdownReport, "GATED_BREAKING_DIFF") {
		t.Errorf("期望 Markdown 报告标明 GATED_BREAKING_DIFF，实际:\n%s", evalResp.MarkdownReport)
	}
	if !strings.Contains(evalResp.MarkdownReport, "/traefik/debug?replay_task_id=") {
		t.Errorf("期望 Markdown 报告包含排障直达链接，实际:\n%s", evalResp.MarkdownReport)
	}
}

// 核心用例 4: 多流水线并发排他锁平滑排队与超时返回 503 GATED_ENV_BUSY / GATED_ENV_NOT_READY
func TestGateEvaluate_ConcurrentPipelinesQueueSmoothly_AndTimeoutReturns503(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}
	r, _ := setupGoldenSuiteTestServer(t, db)
	_, adminToken := createTestUser(t, db, "admin_eval_3", config.AdminRole)

	// 1. 生成 CI 机器凭据
	reqRotate := httptest.NewRequest(http.MethodPost, "/api/admin/ci-token/rotate", bytes.NewReader([]byte("{}")))
	reqRotate.Header.Set("Authorization", "Bearer "+adminToken)
	wRotate := httptest.NewRecorder()
	r.ServeHTTP(wRotate, reqRotate)
	var rot struct {
		CIToken string `json:"ci_token"`
	}
	_ = json.Unmarshal(wRotate.Body.Bytes(), &rot)
	ciToken := rot.CIToken

	// 2. 初始化标准架构进程与测试环境
	grp := models.ArchProcessGroup{Code: "GRP_ORDER_3", Name: "Order Group 3"}
	db.Create(&grp)
	archProc := models.ArchProcess{
		Code:              "sut-order-queue",
		Name:              "订单服务进程-排队测试",
		GroupID:           grp.ID,
		DefaultListenPort: 18093,
	}
	db.Create(&archProc)

	suite := models.GoldenSuite{
		Code:          "SUITE_ORDER_QUEUE",
		Name:          "订单回归套件-并发排队",
		ArchProcessID: archProc.ID,
		Status:        "ACTIVE",
	}
	db.Create(&suite)

	reqPkt := buildTestCrosReqFrame(3001, crosrpc.ReqTypeSync, "com.demo.OrderService", "QueueOrder", []byte("data"))
	rspPkt := buildTestCrosRspFrame(3001, crosrpc.MsgTypeRsp, 1, 0, "com.demo.OrderService", "QueueOrder", []byte("ok"))
	item := models.GoldenSuiteItem{
		SuiteID:        suite.ID,
		OrderNo:        1,
		Name:           "并发排队用例",
		ServiceName:    "com.demo.OrderService",
		Operation:      "QueueOrder",
		RawReqPacket:   reqPkt,
		RawRspPacket:   rspPkt,
		BaselineStatus: 0,
	}
	db.Create(&item)

	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/agent/v1/replays/execute":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "RUNNING"})
		default:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(agentproto.ReplayTaskResultResp{
				TaskID:         3,
				Status:         models.ReplayResultPass,
				TotalRequests:  1,
				PassedRequests: 1,
				Details: fmt.Sprintf(`[{"record_id":%d,"status":"%s","actual_rsp_hex":"%x"}]`,
					item.ID, models.ReplayResultPass, rspPkt),
			})
		}
	}))
	defer agentServer.Close()

	agent := models.Agent{
		Name:      "AGENT_EVAL_SHARED_03",
		Endpoint:  agentServer.URL,
		Scope:     models.ScopeShared,
		Status:    models.AgentStatusOnline,
		ClaimCode: "code-3",
	}
	db.Create(&agent)

	proc := models.Process{
		AgentID:             agent.ID,
		ArchProcessID:       archProc.ID,
		EffectiveListenPort: 18093,
		ProbeStatus:         models.ProbeStatusArmed,
	}
	db.Create(&proc)

	// -------------------------------------------------------------
	// 场景 A: 锁已被短期占用，第二路流水线携带 wait_lease_seconds=3 排队等待，
	// 前序任务释放后，第二路顺利获锁并执行成功 (200 OK，零 409 误杀)
	// -------------------------------------------------------------
	_ = hub.AcquireAgentLeases(context.Background(), db, []uint64{agent.ID}, models.AgentLeaseReplay, 8888, 1, 10*time.Second)

	// 异步在 150ms 后释放前序占用的锁
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = hub.ReleaseAgentLeases(context.Background(), db, []uint64{agent.ID}, models.AgentLeaseReplay, 8888)
	}()

	waitLeaseSec := 3
	evalReqBodyQueue, _ := json.Marshal(handlers.GateEvaluateReq{
		TargetArchProcessID: archProc.ID,
		EnvAgentID:          agent.ID,
		GoldenSuiteID:       &suite.ID,
		TimeoutSeconds:      10,
		WaitLeaseSeconds:    &waitLeaseSec,
	})
	reqQueue := httptest.NewRequest(http.MethodPost, "/api/gates/evaluate", bytes.NewReader(evalReqBodyQueue))
	reqQueue.Header.Set("X-Traefik-CI-Token", ciToken)
	reqQueue.Header.Set("Content-Type", "application/json")
	wQueue := httptest.NewRecorder()
	r.ServeHTTP(wQueue, reqQueue)

	if wQueue.Code != http.StatusOK {
		t.Fatalf("期望第二路并发请求经有界排队后顺利获锁执行返回 200，实际状态码: %d, body: %s", wQueue.Code, wQueue.Body.String())
	}

	// -------------------------------------------------------------
	// 场景 B: 锁被长时占用，wait_lease_seconds=1 超时 -> 返回 503 GATED_ENV_BUSY
	// -------------------------------------------------------------
	_ = hub.AcquireAgentLeases(context.Background(), db, []uint64{agent.ID}, models.AgentLeaseReplay, 9999, 1, 30*time.Second)
	defer func() {
		_ = hub.ReleaseAgentLeases(context.Background(), db, []uint64{agent.ID}, models.AgentLeaseReplay, 9999)
	}()

	timeoutWaitSec := 1
	evalReqBodyBusy, _ := json.Marshal(handlers.GateEvaluateReq{
		TargetArchProcessID: archProc.ID,
		EnvAgentID:          agent.ID,
		GoldenSuiteID:       &suite.ID,
		TimeoutSeconds:      5,
		WaitLeaseSeconds:    &timeoutWaitSec,
	})
	reqBusy := httptest.NewRequest(http.MethodPost, "/api/gates/evaluate", bytes.NewReader(evalReqBodyBusy))
	reqBusy.Header.Set("X-Traefik-CI-Token", ciToken)
	reqBusy.Header.Set("Content-Type", "application/json")
	wBusy := httptest.NewRecorder()
	r.ServeHTTP(wBusy, reqBusy)

	if wBusy.Code != http.StatusServiceUnavailable {
		t.Fatalf("期望等锁超时返回 503，实际: %d, body: %s", wBusy.Code, wBusy.Body.String())
	}
	if !strings.Contains(wBusy.Body.String(), "GATED_ENV_BUSY") {
		t.Errorf("期望返回状态包含 GATED_ENV_BUSY，实际: %s", wBusy.Body.String())
	}

	// -------------------------------------------------------------
	// 场景 C: 目标进程处于 WAITING_PROC / NEED_RECONNECT 态 -> 503 GATED_ENV_NOT_READY
	// -------------------------------------------------------------
	db.Model(&proc).Update("probe_status", models.ProbeStatusNeedReconnect)
	reqNotReady := httptest.NewRequest(http.MethodPost, "/api/gates/evaluate", bytes.NewReader(evalReqBodyQueue))
	reqNotReady.Header.Set("X-Traefik-CI-Token", ciToken)
	reqNotReady.Header.Set("Content-Type", "application/json")
	wNotReady := httptest.NewRecorder()
	r.ServeHTTP(wNotReady, reqNotReady)
	if wNotReady.Code != http.StatusServiceUnavailable {
		t.Fatalf("期望探针未就绪返回 503，实际: %d", wNotReady.Code)
	}
	if !strings.Contains(wNotReady.Body.String(), "GATED_ENV_NOT_READY") {
		t.Errorf("期望返回 GATED_ENV_NOT_READY，实际: %s", wNotReady.Body.String())
	}
}

// 核心用例 5: SC-10 CI/CD 质量门禁端到端评估、并发锁排队与自动化绿牌准出
func TestSC10_CICDQualityGateHardEvaluationAndReport(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}
	r, _ := setupGoldenSuiteTestServer(t, db)
	_, adminToken := createTestUser(t, db, "admin_sc10", config.AdminRole)

	// 1. 生成 CI 凭据
	reqRotate := httptest.NewRequest(http.MethodPost, "/api/admin/ci-token/rotate", bytes.NewReader([]byte("{}")))
	reqRotate.Header.Set("Authorization", "Bearer "+adminToken)
	wRotate := httptest.NewRecorder()
	r.ServeHTTP(wRotate, reqRotate)
	var rot struct {
		CIToken string `json:"ci_token"`
	}
	_ = json.Unmarshal(wRotate.Body.Bytes(), &rot)
	ciToken := rot.CIToken

	// 2. 初始化标准架构进程与模块
	subsys := models.Subsystem{Code: "SUB_SC10", Name: "SC10 Subsys"}
	db.Create(&subsys)
	fg := models.FunctionalGroup{SubsystemID: subsys.ID, Code: "FG_SC10", Name: "SC10 FG"}
	db.Create(&fg)

	grp := models.ArchProcessGroup{Code: "GRP_SC10", Name: "SC10 Group"}
	db.Create(&grp)
	archProc := models.ArchProcess{
		Code:              "sut-order-sc10",
		Name:              "订单服务进程-SC10",
		GroupID:           grp.ID,
		DefaultListenPort: 18095,
	}
	db.Create(&archProc)

	mod := models.Module{
		SubsystemID:   subsys.ID,
		GroupID:       fg.ID,
		Code:          "MOD_SC10_ORDER",
		Name:          "SC10 订单模块",
		ArchProcessID: &archProc.ID,
	}
	db.Create(&mod)

	// 声明依赖下游服务 com.demo.InventoryService
	invMod := models.Module{
		SubsystemID: subsys.ID,
		GroupID:     fg.ID,
		Code:        "MOD_SC10_INV",
		Name:        "库存模块",
	}
	db.Create(&invMod)
	invSvc := models.ModuleService{
		ModuleID:    invMod.ID,
		ServiceName: "com.demo.InventoryService",
	}
	db.Create(&invSvc)
	db.Create(&models.ModuleDependency{
		ConsumerModuleID: mod.ID,
		ServiceID:        &invSvc.ID,
		ProviderModuleID: &invMod.ID,
	})

	// 3. 创建黄金套件 (不传 golden_suite_id 时自动选取该 ArchProcess 最新的 ACTIVE 套件)
	suite := models.GoldenSuite{
		Code:          "SUITE_SC10_GOLDEN",
		Name:          "SC10 权威基线套件 v1.0.0",
		ArchProcessID: archProc.ID,
		Status:        "ACTIVE",
	}
	db.Create(&suite)

	reqPkt := buildTestCrosReqFrame(4001, crosrpc.ReqTypeSync, "com.demo.OrderService", "CreateOrder", []byte("good_req"))
	rspPkt := buildTestCrosRspFrame(4001, crosrpc.MsgTypeRsp, 1, 0, "com.demo.OrderService", "CreateOrder", []byte("good_rsp"))
	item := models.GoldenSuiteItem{
		SuiteID:        suite.ID,
		OrderNo:        1,
		Name:           "用例 1",
		ServiceName:    "com.demo.OrderService",
		Operation:      "CreateOrder",
		RawReqPacket:   reqPkt,
		RawRspPacket:   rspPkt,
		BaselineStatus: 0,
	}
	db.Create(&item)

	// 4. 模拟 Agent 返回实测出站边全部合规，且入站执行结果 100% PASS
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/agent/v1/replays/execute":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "RUNNING"})
		default:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(agentproto.ReplayTaskResultResp{
				TaskID:         10,
				Status:         models.ReplayResultPass,
				TotalRequests:  1,
				PassedRequests: 1,
				ObservedEgressCalls: []agentproto.ObservedEgressCall{
					{
						ServiceName: "com.demo.InventoryService", // 依赖已在 DEPEND 中报备
						Operation:   "CheckStock",
						DstAddr:     "10.0.0.5:9090",
						CallCount:   1,
						MatchStatus: "SCOPED_HIT",
					},
				},
				Details: fmt.Sprintf(`[{"record_id":%d,"status":"%s","actual_rsp_hex":"%x"}]`,
					item.ID, models.ReplayResultPass, rspPkt),
			})
		}
	}))
	defer agentServer.Close()

	agent := models.Agent{
		Name:      "AGENT_SC10_SHARED",
		Endpoint:  agentServer.URL,
		Scope:     models.ScopeShared,
		Status:    models.AgentStatusOnline,
		ClaimCode: "code-sc10",
	}
	db.Create(&agent)

	proc := models.Process{
		AgentID:             agent.ID,
		ArchProcessID:       archProc.ID,
		EffectiveListenPort: 18095,
		ProbeStatus:         models.ProbeStatusArmed,
	}
	db.Create(&proc)

	// 5. 调用 POST /api/gates/evaluate (缺省 golden_suite_id，自动匹配)
	evalReqBody, _ := json.Marshal(handlers.GateEvaluateReq{
		TargetArchProcessID: archProc.ID,
		EnvAgentID:          agent.ID,
		TimeoutSeconds:      10,
	})
	evalReq := httptest.NewRequest(http.MethodPost, "/api/gates/evaluate", bytes.NewReader(evalReqBody))
	evalReq.Header.Set("X-Traefik-CI-Token", ciToken)
	evalReq.Header.Set("Content-Type", "application/json")
	wEval := httptest.NewRecorder()
	r.ServeHTTP(wEval, evalReq)

	// 6. 断言准出通过: 200 OK, PASSED, 0 Violation, 1/1 Passed
	if wEval.Code != http.StatusOK {
		t.Fatalf("期望门禁全量通过返回 200，实际: %d, body: %s", wEval.Code, wEval.Body.String())
	}

	var evalResp handlers.GateEvaluateResp
	if err := json.Unmarshal(wEval.Body.Bytes(), &evalResp); err != nil {
		t.Fatalf("解析门禁通过响应失败: %v", err)
	}

	if evalResp.GateStatus != "PASSED" {
		t.Errorf("期望 gate_status 为 PASSED，实际: %s", evalResp.GateStatus)
	}
	if evalResp.ViolationCount != 0 {
		t.Errorf("期望 ViolationCount 为 0，实际: %d", evalResp.ViolationCount)
	}
	if evalResp.PassedRequests != 1 || evalResp.TotalRequests != 1 {
		t.Errorf("期望 1/1 请求全部通过，实际: %d/%d", evalResp.PassedRequests, evalResp.TotalRequests)
	}
	if !strings.Contains(evalResp.MarkdownReport, "✅ 通过 (PASSED)") {
		t.Errorf("期望 Markdown 报告包含 ✅ 通过 (PASSED)，实际:\n%s", evalResp.MarkdownReport)
	}
	if !strings.Contains(evalResp.MarkdownReport, "/traefik/debug?replay_task_id=") {
		t.Errorf("期望 Markdown 报告包含排障直达链接，实际:\n%s", evalResp.MarkdownReport)
	}

	// 7. 验证并发安全：两路流水线并发请求该 Agent 时平稳通过
	var wg sync.WaitGroup
	wg.Add(2)
	results := make([]int, 2)
	waitSec := 5

	for idx := 0; idx < 2; idx++ {
		go func(i int) {
			defer wg.Done()
			b, _ := json.Marshal(handlers.GateEvaluateReq{
				TargetArchProcessID: archProc.ID,
				EnvAgentID:          agent.ID,
				TimeoutSeconds:      10,
				WaitLeaseSeconds:    &waitSec,
			})
			req := httptest.NewRequest(http.MethodPost, "/api/gates/evaluate", bytes.NewReader(b))
			req.Header.Set("X-Traefik-CI-Token", ciToken)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			results[i] = w.Code
		}(idx)
	}
	wg.Wait()

	for i, code := range results {
		if code != http.StatusOK {
			t.Errorf("并发流水线 %d 期望返回 200，实际返回: %d", i, code)
		}
	}
}
