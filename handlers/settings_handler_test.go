package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonAudit "code-common/backend/audit"
	"code-common/backend/testdb"
	"code-traefik/config"
	"code-traefik/internal/server/security"
	"code-traefik/models"
)

func TestSettingsUpdate_SecretMaskAndAuditRedaction(t *testing.T) {
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

	t.Setenv("TRAEFIK_CRYPTO_KEY", "unit-test-crypto-key-32bytes-seed!")
	r, _ := setupGoldenSuiteTestServer(t, db)
	_, adminToken := createTestUser(t, db, "settings_admin", config.AdminRole)

	// 1. 禁止直接通过通用接口写入内部保留键 *_enc 或 *_sha256
	for _, forbiddenKey := range []string{"llm_api_key_enc", "code_notifier_token_enc", "smtp_password_enc", "ci_gate_token_sha256"} {
		payload, _ := json.Marshal(map[string]any{
			"items": []map[string]string{
				{"setting_key": forbiddenKey, "setting_val": "raw-bypass-attempt"},
			},
		})
		req := httptest.NewRequest(http.MethodPut, "/api/admin/settings", bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("直接写入内部保留键 %s 期望返回 400，实际 %d body=%s", forbiddenKey, w.Code, w.Body.String())
		}
	}

	// 2. 写入非法的 outbound_trusted_cidrs 应当被格式校验拒绝
	badCIDRPayload, _ := json.Marshal(map[string]any{
		"items": []map[string]string{
			{"setting_key": "outbound_trusted_cidrs", "setting_val": "invalid/cidr/expression"},
		},
	})
	reqBadCIDR := httptest.NewRequest(http.MethodPut, "/api/admin/settings", bytes.NewReader(badCIDRPayload))
	reqBadCIDR.Header.Set("Authorization", "Bearer "+adminToken)
	reqBadCIDR.Header.Set("Content-Type", "application/json")
	wBadCIDR := httptest.NewRecorder()
	r.ServeHTTP(wBadCIDR, reqBadCIDR)
	if wBadCIDR.Code != http.StatusBadRequest {
		t.Fatalf("写入非法 outbound_trusted_cidrs 期望返回 400，实际 %d", wBadCIDR.Code)
	}

	// 3. 写入明文 LLM Key / 通知密钥 / SMTP 密码 / 长度 >512 且 <=4096 的 outbound_trusted_cidrs
	longTrusted := "192.168.56.0/24, 10.10.0.0/16, 172.20.1.5:8080, " + strings.Repeat("192.168.57.0/24,", 40)
	longTrusted = strings.TrimSuffix(longTrusted, ",")
	if len(longTrusted) <= 512 {
		t.Fatalf("测试构造的 outbound_trusted_cidrs 长度需 >512，当前=%d", len(longTrusted))
	}
	secretLLM := "sk-super-secret-llm-api-key-987654"
	secretNotifier := "ntf-secret-token-abcdef"
	secretSMTP := "smtp-pass-secret-xyz"

	putPayload, _ := json.Marshal(map[string]any{
		"items": []map[string]string{
			{"setting_key": "llm_api_key", "setting_val": secretLLM, "description": "LLM 密钥"},
			{"setting_key": "code_notifier_token", "setting_val": secretNotifier, "description": "Notifier Token"},
			{"setting_key": "smtp_password", "setting_val": secretSMTP, "description": "SMTP 密码"},
			{"setting_key": "outbound_trusted_cidrs", "setting_val": longTrusted, "description": "可信出向白名单"},
		},
	})
	reqPut := httptest.NewRequest(http.MethodPut, "/api/admin/settings", bytes.NewReader(putPayload))
	reqPut.Header.Set("Authorization", "Bearer "+adminToken)
	reqPut.Header.Set("Content-Type", "application/json")
	wPut := httptest.NewRecorder()
	r.ServeHTTP(wPut, reqPut)
	if wPut.Code != http.StatusOK {
		t.Fatalf("PUT /api/admin/settings 期望 200，实际 %d body=%s", wPut.Code, wPut.Body.String())
	}

	// 响应中严禁包含明文密钥或 *_enc 密文字段
	putBody := wPut.Body.String()
	for _, forbiddenSub := range []string{secretLLM, secretNotifier, secretSMTP, "llm_api_key_enc", "code_notifier_token_enc", "smtp_password_enc"} {
		if strings.Contains(putBody, forbiddenSub) {
			t.Fatalf("PUT 响应泄露了敏感内容 %q: %s", forbiddenSub, putBody)
		}
	}

	// 4. 数据库中应为 *_enc AES-GCM 密文且可正确解密，绝无明文落库
	var llmRow models.TraefikSetting
	if err := db.Where("setting_key = ?", "llm_api_key_enc").First(&llmRow).Error; err != nil {
		t.Fatalf("未找到加密落库的 llm_api_key_enc: %v", err)
	}
	if strings.Contains(llmRow.SettingVal, secretLLM) {
		t.Fatalf("llm_api_key_enc 不得明文落库")
	}
	decrypted, err := security.DecryptSecret(llmRow.SettingVal)
	if err != nil || decrypted != secretLLM {
		t.Fatalf("llm_api_key_enc 解密不一致: got=%q err=%v", decrypted, err)
	}

	// 5. GET /api/admin/settings 屏蔽 *_enc / *_sha256 并返回 *_configured=true 布尔标志
	reqGet := httptest.NewRequest(http.MethodGet, "/api/admin/settings", nil)
	reqGet.Header.Set("Authorization", "Bearer "+adminToken)
	wGet := httptest.NewRecorder()
	r.ServeHTTP(wGet, reqGet)
	if wGet.Code != http.StatusOK {
		t.Fatalf("GET /api/admin/settings 期望 200，实际 %d", wGet.Code)
	}
	getBody := wGet.Body.String()
	for _, forbiddenSub := range []string{secretLLM, secretNotifier, secretSMTP, llmRow.SettingVal, "llm_api_key_enc", "code_notifier_token_enc", "smtp_password_enc", "ci_gate_token_sha256"} {
		if strings.Contains(getBody, forbiddenSub) {
			t.Fatalf("GET 响应泄露敏感键或密文 %q: %s", forbiddenSub, getBody)
		}
	}
	var getResp struct {
		Settings                    []models.TraefikSetting `json:"settings"`
		CITokenConfigured           bool                    `json:"ci_token_configured"`
		LLMKeyConfigured            bool                    `json:"llm_key_configured"`
		CodeNotifierTokenConfigured bool                    `json:"code_notifier_token_configured"`
		SMTPPasswordConfigured      bool                    `json:"smtp_password_configured"`
	}
	if err := json.Unmarshal(wGet.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("解析 GET /api/admin/settings 失败: %v", err)
	}
	if !getResp.LLMKeyConfigured || !getResp.CodeNotifierTokenConfigured || !getResp.SMTPPasswordConfigured {
		t.Fatalf("期望已配置敏感项标志均为 true，实际: %+v", getResp)
	}

	// 6. 关闭异步审计缓冲并核对 sys_audit_logs：必须脱敏为 ***REDACTED***，不得出现任何明文或密文
	if err := commonAudit.Close(context.Background()); err != nil {
		t.Fatalf("刷新审计队列失败: %v", err)
	}
	var logs []models.SysAuditLog
	if err := db.Order("id DESC").Find(&logs).Error; err != nil {
		t.Fatalf("查询 sys_audit_logs 失败: %v", err)
	}
	var foundSettingsAudit bool
	for _, entry := range logs {
		if entry.Module == "settings" && entry.Action == "update" {
			foundSettingsAudit = true
			combined := entry.BeforeData + " " + entry.AfterData
			if !strings.Contains(entry.AfterData, "***REDACTED***") {
				t.Fatalf("审计日志 after_data 必须包含 ***REDACTED*** 掩码: %s", entry.AfterData)
			}
			for _, leak := range []string{secretLLM, secretNotifier, secretSMTP, llmRow.SettingVal} {
				if strings.Contains(combined, leak) {
					t.Fatalf("审计日志泄露敏感信息 %q! before=%s after=%s", leak, entry.BeforeData, entry.AfterData)
				}
			}
		}
	}
	if !foundSettingsAudit {
		t.Fatalf("未找到 settings/update 审计日志记录")
	}
}
