package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonAudit "code-common/backend/audit"
	"code-common/backend/testdb"
	"code-traefik/config"
	"code-traefik/handlers"
	"code-traefik/internal/server/decoder"
	"code-traefik/models"
)

func TestSC15_StaticLinterVocabSeedDuplicateGuardrailsAndLLMTimeout(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}
	_ = commonAudit.Close(context.Background())
	commonAudit.Init(db, commonAudit.InitOptions{
		QueueCap:    2000,
		BatchSize:   1,
		FlushPeriod: 20 * time.Millisecond,
	})
	defer func() {
		_ = commonAudit.Close(context.Background())
	}()

	t.Setenv("TRAEFIK_CRYPTO_KEY", "sc15-e2e-aes-256-gcm-crypto-key!!")
	r, _ := setupGoldenSuiteTestServer(t, db)
	decReg, err := decoder.NewRegistry(db)
	if err != nil {
		t.Fatalf("NewRegistry 失败: %v", err)
	}
	handlers.ResetGlobalDecoderRegistry(decReg)
	t.Cleanup(func() {
		handlers.ResetGlobalDecoderRegistry(nil)
	})
	_, adminToken := createTestUser(t, db, "linter_admin", config.AdminRole)
	_, devToken := createTestUser(t, db, "linter_dev", "developer")

	// 1. 查询内置规则列表 (普通用户可读)，确认内置规则与 COMMENT_REQUIRED=WARNING 就绪
	reqListRules := httptest.NewRequest(http.MethodGet, "/api/check-rules?page=1&page_size=25", nil)
	reqListRules.Header.Set("Authorization", "Bearer "+devToken)
	wListRules := httptest.NewRecorder()
	r.ServeHTTP(wListRules, reqListRules)
	if wListRules.Code != http.StatusOK {
		t.Fatalf("GET /api/check-rules 期望 200，实际 %d body=%s", wListRules.Code, wListRules.Body.String())
	}
	var rulesPage struct {
		Items []models.CheckRule `json:"items"`
		Total int64              `json:"total"`
	}
	if err := json.Unmarshal(wListRules.Body.Bytes(), &rulesPage); err != nil {
		t.Fatalf("解析规则分页失败: %v", err)
	}
	if len(rulesPage.Items) < 6 {
		t.Fatalf("内置规则不足: %d", len(rulesPage.Items))
	}
	var builtinPkgID uint64
	for _, rule := range rulesPage.Items {
		if rule.RuleCode == "PKG_NAMING" {
			builtinPkgID = rule.ID
		}
	}
	if builtinPkgID == 0 {
		t.Fatalf("未在 /api/check-rules 返回中找到 PKG_NAMING")
	}

	// 2. RBAC 与内置规则保护校验
	customRuleBody, _ := json.Marshal(map[string]any{
		"rule_code":           "CUSTOM_DEMO_RULE",
		"name":                "自定义演示规则",
		"category":            "STYLE",
		"target_scope":        "MESSAGE",
		"severity":            "INFO",
		"check_mode":          "REGEX",
		"pattern":             "^[A-Z][A-Za-z0-9]*$",
		"suggestion_template": "建议修正 {symbol}",
		"is_enabled":          true,
	})
	reqNonAdminCreate := httptest.NewRequest(http.MethodPost, "/api/check-rules", bytes.NewReader(customRuleBody))
	reqNonAdminCreate.Header.Set("Authorization", "Bearer "+devToken)
	reqNonAdminCreate.Header.Set("Content-Type", "application/json")
	wNonAdminCreate := httptest.NewRecorder()
	r.ServeHTTP(wNonAdminCreate, reqNonAdminCreate)
	if wNonAdminCreate.Code != http.StatusForbidden {
		t.Fatalf("普通用户创建规则应返回 403，实际 %d", wNonAdminCreate.Code)
	}

	// 删除内置规则必须返回 409 Conflict
	reqDelBuiltin := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/check-rules/%d", builtinPkgID), nil)
	reqDelBuiltin.Header.Set("Authorization", "Bearer "+adminToken)
	wDelBuiltin := httptest.NewRecorder()
	r.ServeHTTP(wDelBuiltin, reqDelBuiltin)
	if wDelBuiltin.Code != http.StatusConflict {
		t.Fatalf("删除内置规则应返回 409，实际 %d body=%s", wDelBuiltin.Code, wDelBuiltin.Body.String())
	}

	// 管理员创建、修改、删除自定义规则全闭环
	reqAdminCreate := httptest.NewRequest(http.MethodPost, "/api/check-rules", bytes.NewReader(customRuleBody))
	reqAdminCreate.Header.Set("Authorization", "Bearer "+adminToken)
	reqAdminCreate.Header.Set("Content-Type", "application/json")
	wAdminCreate := httptest.NewRecorder()
	r.ServeHTTP(wAdminCreate, reqAdminCreate)
	if wAdminCreate.Code != http.StatusCreated {
		t.Fatalf("管理员创建规则期望 201，实际 %d body=%s", wAdminCreate.Code, wAdminCreate.Body.String())
	}
	var createdRule models.CheckRule
	_ = json.Unmarshal(wAdminCreate.Body.Bytes(), &createdRule)

	updateRuleBody, _ := json.Marshal(map[string]any{"severity": "WARNING", "is_enabled": true})
	reqAdminUpdate := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/check-rules/%d", createdRule.ID), bytes.NewReader(updateRuleBody))
	reqAdminUpdate.Header.Set("Authorization", "Bearer "+adminToken)
	reqAdminUpdate.Header.Set("Content-Type", "application/json")
	wAdminUpdate := httptest.NewRecorder()
	r.ServeHTTP(wAdminUpdate, reqAdminUpdate)
	if wAdminUpdate.Code != http.StatusOK {
		t.Fatalf("管理员更新规则期望 200，实际 %d body=%s", wAdminUpdate.Code, wAdminUpdate.Body.String())
	}

	reqAdminDel := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/check-rules/%d", createdRule.ID), nil)
	reqAdminDel.Header.Set("Authorization", "Bearer "+adminToken)
	wAdminDel := httptest.NewRecorder()
	r.ServeHTTP(wAdminDel, reqAdminDel)
	if wAdminDel.Code != http.StatusOK {
		t.Fatalf("删除自定义规则期望 200，实际 %d", wAdminDel.Code)
	}

	// 3. 标准词库 CRUD + 非法来源拦截 + BUILTIN_SEED 查验
	badVocabBody, _ := json.Marshal(map[string]any{
		"term":           "legacy_x",
		"description_zh": "非法来源词根",
		"source":         "PROTOHUB_IMPORT",
	})
	reqBadVocab := httptest.NewRequest(http.MethodPost, "/api/vocabulary-terms", bytes.NewReader(badVocabBody))
	reqBadVocab.Header.Set("Authorization", "Bearer "+adminToken)
	reqBadVocab.Header.Set("Content-Type", "application/json")
	wBadVocab := httptest.NewRecorder()
	r.ServeHTTP(wBadVocab, reqBadVocab)
	if wBadVocab.Code != http.StatusBadRequest {
		t.Fatalf("携带非法 source=PROTOHUB_IMPORT 期望返回 400，实际 %d", wBadVocab.Code)
	}

	vocabCreateBody, _ := json.Marshal(map[string]any{
		"term":           "cust",
		"description_en": "Customer",
		"description_zh": "客户主体",
		"aliases":        []string{"cstmr"},
		"similar_terms":  []string{"customer"},
		"domain":         "GENERAL",
		"is_approved":    true,
	})
	reqCreateVocab := httptest.NewRequest(http.MethodPost, "/api/vocabulary-terms", bytes.NewReader(vocabCreateBody))
	reqCreateVocab.Header.Set("Authorization", "Bearer "+adminToken)
	reqCreateVocab.Header.Set("Content-Type", "application/json")
	wCreateVocab := httptest.NewRecorder()
	r.ServeHTTP(wCreateVocab, reqCreateVocab)
	if wCreateVocab.Code != http.StatusCreated {
		t.Fatalf("管理员新增词根期望 201，实际 %d body=%s", wCreateVocab.Code, wCreateVocab.Body.String())
	}
	var createdVocab models.VocabularyTerm
	_ = json.Unmarshal(wCreateVocab.Body.Bytes(), &createdVocab)

	// 审核编辑词汇
	vocabUpdBody, _ := json.Marshal(map[string]any{
		"description_zh": "个人或机构客户主体",
		"is_approved":    true,
	})
	reqUpdVocab := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/vocabulary-terms/%d", createdVocab.ID), bytes.NewReader(vocabUpdBody))
	reqUpdVocab.Header.Set("Authorization", "Bearer "+adminToken)
	reqUpdVocab.Header.Set("Content-Type", "application/json")
	wUpdVocab := httptest.NewRecorder()
	r.ServeHTTP(wUpdVocab, reqUpdVocab)
	if wUpdVocab.Code != http.StatusOK {
		t.Fatalf("更新词根期望 200，实际 %d", wUpdVocab.Code)
	}

	// 4. 在线编译真实多段包 Schema 入库，用于静态扫描与 AST 三护栏探测
	compileSchema := func(name, category, filePath, content string) uint64 {
		body, _ := json.Marshal(map[string]any{
			"name":     name,
			"category": category,
			"files": []map[string]string{
				{"path": filePath, "content": content},
			},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/proto-schemas/compile", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated && w.Code != http.StatusOK {
			t.Fatalf("编译 Schema %s 失败: code=%d body=%s", name, w.Code, w.Body.String())
		}
		var resp struct {
			ID     uint64 `json:"id"`
			Schema *struct {
				ID uint64 `json:"id"`
			} `json:"schema"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Schema != nil && resp.Schema.ID > 0 {
			return resp.Schema.ID
		}
		return resp.ID
	}

	baseLibID := compileSchema("sc15-base-lib", "BASE_LIB", "dophi/base/v1/common.proto", `syntax = "proto3";
package dophi.base.v1;

message CommonAuditMeta {
  int64 tenant_id = 1;
  int64 operator_id = 2;
  string trace_id = 3;
  int64 created_ts = 4;
}
`)
	tradeID := compileSchema("sc15-trade", "BUSINESS", "dophi/trade/v1/trade.proto", `syntax = "proto3";
package dophi.trade.v1;

message Empty {}
message IdReq { int64 id = 1; }
message PageReq { int32 page = 1; int32 page_size = 2; }

message CommonAuditMetaCopy {
  int64 tenant_id = 1;
  int64 operator_id = 2;
  string trace_id = 3;
  int64 created_ts = 4;
}

message TradeSummary {
  int64 trade_id = 1;
  int64 user_id = 2;
  int64 order_id = 3;
  int64 cust_ord_amnt = 4;
  string ccy = 5;
  int64 ts = 6;
  string zqbx_flag = 7;
}

// 交易服务
service TradeService {
  rpc QueryTrade(IdReq) returns (TradeSummary);
}
`)
	clearingID := compileSchema("sc15-clearing", "BUSINESS", "dophi/clearing/v1/clearing.proto", `syntax = "proto3";
package dophi.clearing.v1;

message Empty {}
message IdReq { int64 id = 1; }

message DealSummary {
  int64 trade_id = 1;
  int64 user_id = 2;
  int64 order_id = 3;
  int64 cust_ord_amnt = 4;
  string ccy = 5;
  int64 ts = 6;
  string zqbx_flag = 7;
}
`)
	orderV1ID := compileSchema("sc15-order-v1", "BUSINESS", "dophi/order/v1/order.proto", `syntax = "proto3";
package dophi.order.v1;

message OrderDetail {
  int64 order_id = 1;
  int64 user_id = 2;
  string symbol = 3;
  int64 qty = 4;
  int64 amt = 5;
}
`)
	orderV2ID := compileSchema("sc15-order-v2", "BUSINESS", "dophi/order/v2/order.proto", `syntax = "proto3";
package dophi.order.v2;

message OrderDetail {
  int64 order_id = 1;
  int64 user_id = 2;
  string symbol = 3;
  int64 qty = 4;
  int64 amt = 5;
}
`)
	if baseLibID == 0 || tradeID == 0 || clearingID == 0 || orderV1ID == 0 || orderV2ID == 0 {
		t.Fatalf("Schema 编译未返回有效 ID: base=%d trade=%d clearing=%d v1=%d v2=%d", baseLibID, tradeID, clearingID, orderV1ID, orderV2ID)
	}

	// 5. 启动阻塞 5s 的 Mock LLM 服务端，并通过 /api/admin/settings 配置白名单与加密密钥
	hangingLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
			return
		}
	}))
	defer hangingLLM.Close()

	llmHostPort := strings.TrimPrefix(hangingLLM.URL, "http://")
	settingsBody, _ := json.Marshal(map[string]any{
		"items": []map[string]string{
			{"setting_key": "outbound_trusted_cidrs", "setting_val": llmHostPort},
			{"setting_key": "llm_enabled", "setting_val": "true"},
			{"setting_key": "llm_base_url", "setting_val": hangingLLM.URL + "/v1"},
			{"setting_key": "llm_model", "setting_val": "gpt-4o-mini"},
			{"setting_key": "llm_timeout_ms", "setting_val": "3000"},
			{"setting_key": "llm_api_key", "setting_val": "sk-sc15-llm-secret"},
		},
	})
	reqPutSet := httptest.NewRequest(http.MethodPut, "/api/admin/settings", bytes.NewReader(settingsBody))
	reqPutSet.Header.Set("Authorization", "Bearer "+adminToken)
	reqPutSet.Header.Set("Content-Type", "application/json")
	wPutSet := httptest.NewRecorder()
	r.ServeHTTP(wPutSet, reqPutSet)
	if wPutSet.Code != http.StatusOK {
		t.Fatalf("保存 LLM 设置失败: %d %s", wPutSet.Code, wPutSet.Body.String())
	}

	// 6. 触发静态检查扫描 POST /api/check-reports/scan（普通登录用户可触发）
	scanPayload, _ := json.Marshal(map[string]any{
		"schema_ids":         []uint64{baseLibID, tradeID, clearingID, orderV1ID, orderV2ID},
		"include_duplicates": true,
		"include_llm":        true,
	})
	start := time.Now()
	reqScan := httptest.NewRequest(http.MethodPost, "/api/check-reports/scan", bytes.NewReader(scanPayload))
	reqScan.Header.Set("Authorization", "Bearer "+devToken)
	reqScan.Header.Set("Content-Type", "application/json")
	wScan := httptest.NewRecorder()
	r.ServeHTTP(wScan, reqScan)
	elapsed := time.Since(start)
	if wScan.Code != http.StatusOK {
		t.Fatalf("POST /api/check-reports/scan 期望 200，实际 %d body=%s", wScan.Code, wScan.Body.String())
	}
	if elapsed > 4000*time.Millisecond {
		t.Fatalf("LLM 3s 熔断超时保护失败，实际耗时=%v", elapsed)
	}

	var report models.CheckReport
	if err := json.Unmarshal(wScan.Body.Bytes(), &report); err != nil {
		t.Fatalf("解析扫描报告失败: %v", err)
	}
	if report.LLMStatus != models.LLMStatusTimeoutFallback {
		t.Fatalf("期望 llm_status=TIMEOUT_FALLBACK，实际 %q", report.LLMStatus)
	}
	if report.DuplicateGroupCount != 1 || len(report.DuplicateGroups) != 1 {
		t.Fatalf("三护栏后应精确命中 1 个跨领域重复簇 (TradeSummary<->DealSummary)，实际 count=%d groups=%+v", report.DuplicateGroupCount, report.DuplicateGroups)
	}
	if report.DuplicateGroups[0].Similarity < 0.85 {
		t.Fatalf("重复簇相似度应 >= 0.85，实际 %.4f", report.DuplicateGroups[0].Similarity)
	}
	// 确认无 PKG_NAMING 误报，存在 COMMENT_REQUIRED WARNING 与 VOCAB_STANDARD_TERM
	var sawVocab, sawCommentWarn bool
	for _, v := range report.Violations {
		if v.RuleCode == "PKG_NAMING" {
			t.Fatalf("多段合规包名 dophi.trade.v1 等不应触发 PKG_NAMING 误报: %+v", v)
		}
		if v.RuleCode == "COMMENT_REQUIRED" && v.Severity == models.RuleSeverityWarning {
			sawCommentWarn = true
		}
		if v.RuleCode == "VOCAB_STANDARD_TERM" && strings.Contains(v.Suggestion, "amt") {
			sawVocab = true
		}
	}
	if !sawCommentWarn || !sawVocab {
		t.Fatalf("静态扫描结果缺失预期违规项: sawCommentWarn=%v sawVocab=%v violations=%+v", sawCommentWarn, sawVocab, report.Violations)
	}

	// 7. 分页列表 GET /api/check-reports 与详情 GET /api/check-reports/:id 校验
	reqReports := httptest.NewRequest(http.MethodGet, "/api/check-reports?page=1&page_size=25", nil)
	reqReports.Header.Set("Authorization", "Bearer "+devToken)
	wReports := httptest.NewRecorder()
	r.ServeHTTP(wReports, reqReports)
	if wReports.Code != http.StatusOK {
		t.Fatalf("GET /api/check-reports 期望 200，实际 %d", wReports.Code)
	}

	reqDetail := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/check-reports/%d", report.ID), nil)
	reqDetail.Header.Set("Authorization", "Bearer "+devToken)
	wDetail := httptest.NewRecorder()
	r.ServeHTTP(wDetail, reqDetail)
	if wDetail.Code != http.StatusOK {
		t.Fatalf("GET /api/check-reports/:id 期望 200，实际 %d body=%s", wDetail.Code, wDetail.Body.String())
	}
	var detail models.CheckReport
	if err := json.Unmarshal(wDetail.Body.Bytes(), &detail); err != nil {
		t.Fatalf("解析报告详情失败: %v", err)
	}
	if len(detail.Violations) == 0 || len(detail.DuplicateGroups) != 1 {
		t.Fatalf("报告详情应包含 violations 与重构的 duplicate_groups，实际 violations=%d dups=%d", len(detail.Violations), len(detail.DuplicateGroups))
	}

	// 8. 刷新并校验 traefik_linter 审计字典事件，且绝无 LLM Secret 泄露
	if err := commonAudit.Close(context.Background()); err != nil {
		t.Fatalf("关闭刷新审计日志失败: %v", err)
	}
	var auditLogs []models.SysAuditLog
	if err := db.Where("module = ?", "traefik_linter").Find(&auditLogs).Error; err != nil {
		t.Fatalf("查询审计日志失败: %v", err)
	}
	seenActions := map[string]bool{}
	for _, a := range auditLogs {
		seenActions[a.Action] = true
		if strings.Contains(a.BeforeData+a.AfterData, "sk-sc15-llm-secret") {
			t.Fatalf("审计日志泄露 LLM Secret!")
		}
	}
	for _, expected := range []string{"create_rule", "update_rule", "delete_rule", "upsert_vocab", "run_scan"} {
		if !seenActions[expected] {
			t.Fatalf("审计日志缺少事件 traefik_linter.%s，已记录: %v", expected, seenActions)
		}
	}
}
