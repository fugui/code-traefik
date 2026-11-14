package handlers_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	commonAudit "code-common/backend/audit"
	"code-common/backend/testdb"
	"code-traefik/config"
	"code-traefik/internal/server/decoder"
	"code-traefik/models"

	"gorm.io/datatypes"
)

func TestMRGateWebhook_SignatureGuard_Dedup_OverrideGuardAndP1Audit(t *testing.T) {
	if os.Getenv("TEST_DB_DSN") == "" {
		t.Skip("TEST_DB_DSN 未配置，跳过数据库隔离测试")
	}
	t.Setenv("TRAEFIK_CRYPTO_KEY", "m26-mr-handler-crypto-key-32byte")

	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}

	_ = commonAudit.Close(context.Background())
	commonAudit.Init(db, commonAudit.InitOptions{
		QueueCap:    1000,
		BatchSize:   1,
		FlushPeriod: 20 * time.Millisecond,
	})
	defer func() {
		_ = commonAudit.Close(context.Background())
	}()

	r, _ := setupGoldenSuiteTestServer(t, db)
	adminUser, adminToken := createTestUser(t, db, "mr_admin", config.AdminRole)
	_, memberToken := createTestUser(t, db, "mr_member", "member")

	// 准备 GitLab 与 GitHub 两个仓库及 Bundle
	gitlabRepo := models.GitRepo{
		Name:          "trade-proto-gitlab",
		Provider:      models.GitProviderGitLab,
		RepoURL:       "http://127.0.0.1:8080/trade/trade-proto.git",
		DefaultBranch: "main",
		AuthType:      models.GitAuthToken,
		SyncMode:      models.GitSyncWebhook,
		AutoCompile:   true,
	}
	if err := gitlabRepo.SetWebhookSecret("gl-webhook-secret-999"); err != nil {
		t.Fatalf("SetWebhookSecret 失败: %v", err)
	}
	if err := db.Create(&gitlabRepo).Error; err != nil {
		t.Fatalf("创建 gitlabRepo 失败: %v", err)
	}

	bundle := models.GitProtoBundle{
		GitRepoID:        gitlabRepo.ID,
		BundleName:       "trade-core",
		TargetSchemaName: "trade-core-schema",
		Category:         models.ProtoCategoryBusiness,
		SubdirPath:       "proto",
		IncludeDirsJSON:  datatypes.JSON([]byte(`["."]`)),
		AutoCompile:      true,
		MRAutoPublish:    models.MRAutoPublishDraftOnly,
		LintOnSync:       true,
	}
	if err := db.Create(&bundle).Error; err != nil {
		t.Fatalf("创建 bundle 失败: %v", err)
	}

	// 激活 v1 基线契约，使后续字段类型变更触发 Breaking Change
	reg, err := decoder.NewRegistry(db)
	if err != nil {
		t.Fatalf("NewRegistry 失败: %v", err)
	}
	baseSchema, _, _, err := reg.CompileAndSaveSchema(context.Background(), decoder.CompileProtoBundleInput{
		Name:             bundle.TargetSchemaName,
		Category:         bundle.Category,
		VersionTag:       "v1.0.0",
		Files:            map[string]string{"trade/v1/trade.proto": "syntax = \"proto3\";\npackage trade.v1;\n// 交易单\nmessage TradeOrder {\n  int64 order_id = 1;\n}\n"},
		ProtoIncludeDirs: []string{"."},
		AutoActivate:     true,
		SourceType:       models.ProtoSourceGitSync,
		GitRepoID:        &gitlabRepo.ID,
		SourceBundleID:   &bundle.ID,
		SourceCommitSHA:  "target0001",
	})
	if err != nil {
		t.Fatalf("创建基线 Schema 失败: %v", err)
	}
	_ = db.Model(&bundle).Update("last_schema_id", baseSchema.ID).Error

	// 1. 伪造错误的 X-Gitlab-Token 请求 POST /api/webhooks/git-mr/:repoId -> 必须 401 拒绝
	webhookURL := fmt.Sprintf("/api/webhooks/git-mr/%d", gitlabRepo.ID)
	breakingProto := "syntax = \"proto3\";\npackage trade.v1;\n// 交易单\nmessage TradeOrder {\n  string order_id = 1;\n}\n"
	mrPayloadMap := map[string]any{
		"object_kind": "merge_request",
		"object_attributes": map[string]any{
			"iid":           21,
			"title":         "feat: mutate order_id type",
			"url":           "http://127.0.0.1:8080/trade/trade-proto/-/merge_requests/21",
			"action":        "open",
			"state":         "opened",
			"source_branch": "feat/mutate",
			"target_branch": "main",
			"last_commit": map[string]any{
				"id": "sha_breaking_111",
			},
		},
		"target_head_sha": "target0001",
		"user": map[string]any{
			"username": "mr_author",
			"email":    "mr_author@code-bench.local",
		},
		"patch_files": []map[string]any{
			{
				"change_type": "MODIFIED",
				"new_path":    "proto/trade/v1/trade.proto",
				"content":     breakingProto,
			},
		},
	}
	mrPayloadBytes, _ := json.Marshal(mrPayloadMap)

	reqBadSig := httptest.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(mrPayloadBytes))
	reqBadSig.Header.Set("Content-Type", "application/json")
	reqBadSig.Header.Set("X-Gitlab-Token", "wrong-token")
	wBadSig := httptest.NewRecorder()
	r.ServeHTTP(wBadSig, reqBadSig)
	if wBadSig.Code != http.StatusUnauthorized {
		t.Fatalf("错误 Webhook Token 期望返回 401，实际 %d body=%s", wBadSig.Code, wBadSig.Body.String())
	}

	// 2. 携带合法 X-Gitlab-Token 触发 Webhook -> 返回 202 Accepted 并落库 FAILED (compile_passed=true, breaking_change_count>0)
	reqValid := httptest.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(mrPayloadBytes))
	reqValid.Header.Set("Content-Type", "application/json")
	reqValid.Header.Set("X-Gitlab-Token", "gl-webhook-secret-999")
	wValid := httptest.NewRecorder()
	r.ServeHTTP(wValid, reqValid)
	if wValid.Code != http.StatusAccepted {
		t.Fatalf("合法 Webhook 期望返回 202 Accepted，实际 %d body=%s", wValid.Code, wValid.Body.String())
	}

	// 重复重放完全相同的 Webhook 载荷 -> 幂等去重，数据库中仅 1 条 MRCheckRun
	reqDup := httptest.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(mrPayloadBytes))
	reqDup.Header.Set("Content-Type", "application/json")
	reqDup.Header.Set("X-Gitlab-Token", "gl-webhook-secret-999")
	wDup := httptest.NewRecorder()
	r.ServeHTTP(wDup, reqDup)
	if wDup.Code != http.StatusAccepted {
		t.Fatalf("幂等重放 Webhook 期望返回 202，实际 %d", wDup.Code)
	}
	var runCount int64
	db.Model(&models.MRCheckRun{}).Where("git_repo_id = ? AND mr_iid = 21", gitlabRepo.ID).Count(&runCount)
	if runCount != 1 {
		t.Fatalf("重复 Webhook 投递期望幂等去重保持 1 条记录，实际 %d 条", runCount)
	}

	// 3. 构造一条语法编译失败 (compile_passed = false) 的 MRCheckRun，验证管理员尝试 Override 被 409 ERR_MR_OVERRIDE_FORBIDDEN_COMPILE_FAILED 拦截！
	syntaxBadPayloadMap := map[string]any{
		"mr_iid":          22,
		"mr_title":        "broken syntax proto",
		"action":          "open",
		"source_branch":   "feat/broken",
		"target_branch":   "main",
		"head_commit_sha": "sha_syntax_broken_222",
		"target_head_sha": "target0001",
		"patch_files": []map[string]any{
			{
				"change_type": "MODIFIED",
				"new_path":    "proto/trade/v1/trade.proto",
				"content":     "syntax = \"proto3\";\npackage trade.v1;\nmessage Broken { int64 id = ; }\n",
			},
		},
	}
	syntaxBadBytes, _ := json.Marshal(syntaxBadPayloadMap)
	reqSyntax := httptest.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(syntaxBadBytes))
	reqSyntax.Header.Set("Content-Type", "application/json")
	reqSyntax.Header.Set("X-Gitlab-Token", "gl-webhook-secret-999")
	wSyntax := httptest.NewRecorder()
	r.ServeHTTP(wSyntax, reqSyntax)
	if wSyntax.Code != http.StatusAccepted {
		t.Fatalf("触发语法错误 MR Webhook 期望 202，实际 %d", wSyntax.Code)
	}

	var syntaxRun models.MRCheckRun
	if err := db.Where("git_repo_id = ? AND mr_iid = 22", gitlabRepo.ID).First(&syntaxRun).Error; err != nil {
		t.Fatalf("未找到 MR !22 记录: %v", err)
	}
	if syntaxRun.CompilePassed || syntaxRun.Status != models.MRCheckStatusFailed {
		t.Fatalf("语法错误的 MR !22 期望 compile_passed=false 且 status=FAILED，实际 %+v", syntaxRun)
	}

	overrideBody, _ := json.Marshal(map[string]string{
		"override_reason": "紧急上线申请强制豁免此门禁",
	})
	reqOverrideSyntax := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/mr-check-runs/%d/override", syntaxRun.ID), bytes.NewReader(overrideBody))
	reqOverrideSyntax.Header.Set("Authorization", "Bearer "+adminToken)
	reqOverrideSyntax.Header.Set("Content-Type", "application/json")
	wOverrideSyntax := httptest.NewRecorder()
	r.ServeHTTP(wOverrideSyntax, reqOverrideSyntax)
	if wOverrideSyntax.Code != http.StatusConflict {
		t.Fatalf("对 compile_passed=false 的 MR 执行 Override 期望 409 Conflict，实际 %d body=%s",
			wOverrideSyntax.Code, wOverrideSyntax.Body.String())
	}
	var errResp map[string]any
	_ = json.Unmarshal(wOverrideSyntax.Body.Bytes(), &errResp)
	if errResp["code"] != "ERR_MR_OVERRIDE_FORBIDDEN_COMPILE_FAILED" {
		t.Fatalf("期望错误码 ERR_MR_OVERRIDE_FORBIDDEN_COMPILE_FAILED，实际 %v", errResp["code"])
	}

	// 4. 对 MR !21 (compile_passed=true, status=FAILED) 执行 Override：
	// (a) 普通成员请求 -> 403 Forbidden
	var breakingRun models.MRCheckRun
	if err := db.Where("git_repo_id = ? AND mr_iid = 21", gitlabRepo.ID).First(&breakingRun).Error; err != nil {
		t.Fatalf("未找到 MR !21 记录: %v", err)
	}
	reqMemberOverride := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/mr-check-runs/%d/override", breakingRun.ID), bytes.NewReader(overrideBody))
	reqMemberOverride.Header.Set("Authorization", "Bearer "+memberToken)
	reqMemberOverride.Header.Set("Content-Type", "application/json")
	wMemberOverride := httptest.NewRecorder()
	r.ServeHTTP(wMemberOverride, reqMemberOverride)
	if wMemberOverride.Code != http.StatusForbidden {
		t.Fatalf("非管理员调用 Override 期望 403，实际 %d", wMemberOverride.Code)
	}

	// (b) 管理员请求 -> 200 OK，状态翻转为 OVERRIDDEN，且写入 P1 级 sys_audit_logs
	reqAdminOverride := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/mr-check-runs/%d/override", breakingRun.ID), bytes.NewReader(overrideBody))
	reqAdminOverride.Header.Set("Authorization", "Bearer "+adminToken)
	reqAdminOverride.Header.Set("Content-Type", "application/json")
	wAdminOverride := httptest.NewRecorder()
	r.ServeHTTP(wAdminOverride, reqAdminOverride)
	if wAdminOverride.Code != http.StatusOK {
		t.Fatalf("管理员对 compile_passed=true 的 FAILED MR 执行 Override 期望 200，实际 %d body=%s",
			wAdminOverride.Code, wAdminOverride.Body.String())
	}

	var refreshedRun models.MRCheckRun
	if err := db.First(&refreshedRun, breakingRun.ID).Error; err != nil {
		t.Fatalf("查询豁免后 MRCheckRun 失败: %v", err)
	}
	if refreshedRun.Status != models.MRCheckStatusOverridden || refreshedRun.OverriddenBy == nil || *refreshedRun.OverriddenBy != adminUser.ID {
		t.Fatalf("Override 后状态与操作人记录不正确: %+v", refreshedRun)
	}

	// 校验 P1 审计落库（FlushPeriod=20ms, BatchSize=1，稍等异步 flush 后查询）
	var auditLogs []models.SysAuditLog
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := db.Where("module = ? AND action = ?", "traefik_mr_gate", "override_mr_gate").Find(&auditLogs).Error; err == nil && len(auditLogs) > 0 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if len(auditLogs) == 0 || auditLogs[0].Level != "P1" {
		t.Fatalf("期望写入 Level=P1 的 traefik_mr_gate/override_mr_gate 审计日志，实际: %+v", auditLogs)
	}

	// 5. 验证 GitHub X-Hub-Signature-256 HMAC-SHA256 验签与 GET /api/mr-check-runs 列表/详情查询
	ghRepo := models.GitRepo{
		Name:          "gh-proto-repo",
		Provider:      models.GitProviderGitHub,
		RepoURL:       "https://github.com/acme/gh-proto.git",
		DefaultBranch: "main",
		AuthType:      models.GitAuthToken,
		SyncMode:      models.GitSyncWebhook,
	}
	_ = ghRepo.SetWebhookSecret("gh-hmac-secret-777")
	_ = db.Create(&ghRepo).Error
	ghBundle := models.GitProtoBundle{
		GitRepoID:        ghRepo.ID,
		BundleName:       "gh-bundle",
		TargetSchemaName: "gh-proto-schema",
		Category:         models.ProtoCategoryBusiness,
		SubdirPath:       ".",
	}
	_ = db.Create(&ghBundle).Error

	ghBody := []byte(`{"action":"opened","number":9,"pull_request":{"title":"add user proto","html_url":"https://github.com/acme/gh-proto/pull/9","head":{"ref":"feat/user","sha":"ghsha999"},"base":{"ref":"main","sha":"ghbase000"},"user":{"login":"gh_dev"}},"patch_files":[{"change_type":"ADDED","new_path":"user/v1/user.proto","content":"syntax = \"proto3\";\npackage user.v1;\n// 用户\nmessage UserInfo { int64 uid = 1; }\n"}]}`)
	mac := hmac.New(sha256.New, []byte("gh-hmac-secret-777"))
	mac.Write(ghBody)
	sigHex := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	reqGH := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/webhooks/git-mr/%d", ghRepo.ID), bytes.NewReader(ghBody))
	reqGH.Header.Set("Content-Type", "application/json")
	reqGH.Header.Set("X-Hub-Signature-256", sigHex)
	wGH := httptest.NewRecorder()
	r.ServeHTTP(wGH, reqGH)
	if wGH.Code != http.StatusAccepted {
		t.Fatalf("GitHub HMAC-SHA256 合法签名期望 202，实际 %d body=%s", wGH.Code, wGH.Body.String())
	}

	// GET /api/mr-check-runs
	reqList := httptest.NewRequest(http.MethodGet, "/api/mr-check-runs?page=1&page_size=25", nil)
	reqList.Header.Set("Authorization", "Bearer "+memberToken)
	wList := httptest.NewRecorder()
	r.ServeHTTP(wList, reqList)
	if wList.Code != http.StatusOK {
		t.Fatalf("GET /api/mr-check-runs 期望 200，实际 %d", wList.Code)
	}
	var listResp struct {
		Items []map[string]any `json:"items"`
		Total int64            `json:"total"`
	}
	if err := json.Unmarshal(wList.Body.Bytes(), &listResp); err != nil || listResp.Total < 3 {
		t.Fatalf("解析 MR 列表失败或数量不足: total=%d err=%v", listResp.Total, err)
	}

	// 验证 Fast-Forward 合入主干 (action=merge, 同 head_commit_sha="sha_breaking_111") 不会因 MR_GATE 记录存在而被误丢，
	// 而是以 event_phase='MAIN_MERGE' 放行并自动编译生成 GIT_MR_AUTO Schema
	ffMergeWebhook := map[string]any{
		"object_kind": "merge_request",
		"object_attributes": map[string]any{
			"iid":           21,
			"title":         "feat: mutate order_id type",
			"url":           "http://127.0.0.1:8080/trade/trade-proto/-/merge_requests/21",
			"source_branch": "feat/mutate",
			"target_branch": "main",
			"action":        "merge",
			"state":         "merged",
			"last_commit": map[string]any{
				"id": "sha_breaking_111",
			},
		},
		"target_head_sha": "target0001",
		"patch_files": []map[string]any{
			{
				"change_type": "MODIFIED",
				"new_path":    "proto/trade/v1/trade.proto",
				"content":     breakingProto,
			},
		},
	}
	ffRaw, _ := json.Marshal(ffMergeWebhook)
	reqFF := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/webhooks/git-mr/%d", gitlabRepo.ID), bytes.NewReader(ffRaw))
	reqFF.Header.Set("Content-Type", "application/json")
	reqFF.Header.Set("X-Gitlab-Token", "gl-webhook-secret-999")
	wFF := httptest.NewRecorder()
	r.ServeHTTP(wFF, reqFF)
	if wFF.Code != http.StatusAccepted {
		t.Fatalf("Fast-Forward Merge Webhook 期望 202，实际 %d body=%s", wFF.Code, wFF.Body.String())
	}
	var ffResp map[string]any
	_ = json.Unmarshal(wFF.Body.Bytes(), &ffResp)
	if ffResp["duplicate"] == true {
		t.Fatalf("Fast-Forward Merge (MAIN_MERGE) 严禁被 MR_GATE 历史去重吞掉: %+v", ffResp)
	}
	var mergeRuns []models.MRCheckRun
	if err := db.Where("git_repo_id = ? AND mr_iid = 21 AND event_phase = ?", gitlabRepo.ID, models.MREventPhaseMainMerge).
		Find(&mergeRuns).Error; err != nil || len(mergeRuns) != 1 {
		t.Fatalf("期望 Fast-Forward 合入生成 1 条 MAIN_MERGE 记录，实际 %d 条 err=%v", len(mergeRuns), err)
	}
	var autoSchema models.ProtoSchema
	if err := db.Where("name = ? AND source_type = ?", bundle.TargetSchemaName, models.ProtoSourceGitMRAuto).
		Order("id DESC").First(&autoSchema).Error; err != nil {
		t.Fatalf("MAIN_MERGE 合入后未自动入池 GIT_MR_AUTO Schema: %v", err)
	}
	var docSymbols int64
	if err := db.Model(&models.ProtoDocSymbol{}).Where("schema_id = ?", autoSchema.ID).Count(&docSymbols).Error; err != nil || docSymbols == 0 {
		t.Fatalf("MAIN_MERGE 入池后期望刷新 ProtoDocSymbol 符号索引 > 0，实际 %d err=%v", docSymbols, err)
	}

	// 验证站内通知与 Webhook 0 审计断言 (commonAudit.Skip)
	var notifCnt int64
	if err := db.Model(&models.Notification{}).Where("source_type = ?", models.NotifSourceMRCheckRun).Count(&notifCnt).Error; err != nil || notifCnt == 0 {
		t.Fatalf("期望 MR 门禁流水触发站内通知落库 > 0，实际 %d err=%v", notifCnt, err)
	}
	var webhookAuditCnt int64
	_ = commonAudit.Close(context.Background())
	if err := db.Model(&models.SysAuditLog{}).Where("request_path LIKE '%webhooks/git-mr%' OR summary LIKE '%webhooks/git-mr%'").Count(&webhookAuditCnt).Error; err != nil {
		t.Fatalf("查询 Webhook 审计记录失败: %v", err)
	}
	if webhookAuditCnt > 0 {
		t.Fatalf("Webhook 请求必须 Skip 通用操作审计表，实际产生 %d 条", webhookAuditCnt)
	}

	// 验证 /api/dashboard/overview 统计已刷新
	reqDash := httptest.NewRequest(http.MethodGet, "/api/dashboard/overview", nil)
	reqDash.Header.Set("Authorization", "Bearer "+adminToken)
	wDash := httptest.NewRecorder()
	r.ServeHTTP(wDash, reqDash)
	if wDash.Code != http.StatusOK {
		t.Fatalf("GET /api/dashboard/overview 期望 200，实际 %d", wDash.Code)
	}
	var dashResp struct {
		HealthScore struct {
			Total int `json:"total"`
		} `json:"health_score"`
		StaticCompliance struct {
			RecentMRBlockedCount int `json:"recent_mr_blocked_count"`
		} `json:"static_compliance"`
	}
	if err := json.Unmarshal(wDash.Body.Bytes(), &dashResp); err != nil {
		t.Fatalf("解析 dashboard 响应失败: %v", err)
	}
	if dashResp.StaticCompliance.RecentMRBlockedCount < 1 {
		t.Fatalf("期望大盘 static_compliance.recent_mr_blocked_count >= 1，实际 %d", dashResp.StaticCompliance.RecentMRBlockedCount)
	}
}

func TestSC16_EndToEndMRGateOverrideAutoMergeAndNotificationFlow(t *testing.T) {
	TestMRGateWebhook_SignatureGuard_Dedup_OverrideGuardAndP1Audit(t)
}
