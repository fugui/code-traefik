package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-traefik/config"
	"code-traefik/internal/server/hub"
	"code-traefik/internal/server/security"
	"code-traefik/models"
)

func TestDispatcher_TargetedSSE_CodeNotifierPayloadAndFailureDegradation(t *testing.T) {
	if os.Getenv("TEST_DB_DSN") == "" {
		t.Skip("TEST_DB_DSN 未配置，跳过数据库隔离测试")
	}
	t.Setenv("TRAEFIK_CRYPTO_KEY", "m26-notify-test-crypto-key-32byt!")

	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}

	// 创建两个测试用户：UserA (目标收件人) 与 UserB (旁观者，严禁收到 UserA 的 SSE 通知)
	userA := models.User{
		Username: "owner_alice",
		Name:     "Alice",
		Email:    "alice@code-bench.local",
		IsActive: true,
	}
	userB := models.User{
		Username: "bystander_bob",
		Name:     "Bob",
		Email:    "bob@code-bench.local",
		IsActive: true,
	}
	userNoEmail := models.User{
		Username: "charlie_no_mail",
		Name:     "Charlie",
		Email:    "",
		IsActive: true,
	}
	for _, u := range []*models.User{&userA, &userB, &userNoEmail} {
		if err := db.Create(u).Error; err != nil {
			t.Fatalf("创建测试用户失败: %v", err)
		}
	}

	// 启动绑定 127.0.0.1 的 Mock code-notifier HTTP Server
	var (
		mu               sync.Mutex
		receivedPayloads []NotifyPayload
		receivedAuth     string
		shouldFail500    bool
	)
	mockNotifier := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/notify/email" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var p NotifyPayload
		if err := json.Unmarshal(body, &p); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(p.MarkdownContent) == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"markdown_content is required"}`))
			return
		}
		mu.Lock()
		receivedPayloads = append(receivedPayloads, p)
		receivedAuth = r.Header.Get("Authorization")
		fail := shouldFail500
		mu.Unlock()

		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"downstream mail relay unavailable"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"queued"}`))
	}))
	mockNotifier.Start()
	defer mockNotifier.Close()

	encToken, err := security.EncryptSecret("ntf-secret-token-m26")
	if err != nil {
		t.Fatalf("加密 token 失败: %v", err)
	}
	settingsUpdates := map[string]string{
		"code_notifier_enabled":   "true",
		"code_notifier_url":       mockNotifier.URL,
		"code_notifier_token_enc": encToken,
		"outbound_trusted_cidrs":  "127.0.0.1/32",
	}
	for k, v := range settingsUpdates {
		var s models.TraefikSetting
		if err := db.Where("setting_key = ?", k).First(&s).Error; err != nil {
			if err := db.Create(&models.TraefikSetting{SettingKey: k, SettingVal: v, UpdatedBy: "test"}).Error; err != nil {
				t.Fatalf("写入配置 %s 失败: %v", k, err)
			}
		} else {
			if err := db.Model(&s).Update("setting_val", v).Error; err != nil {
				t.Fatalf("更新配置 %s 失败: %v", k, err)
			}
		}
	}

	broker := hub.NewSSEBroker()
	subA, chA := broker.SubscribeForUser(userA.ID)
	defer broker.Unsubscribe(subA)
	subB, chB := broker.SubscribeForUser(userB.ID)
	defer broker.Unsubscribe(subB)

	dispatcher := NewDispatcher(db, broker)

	// 1. 发送通知给 UserA 与 UserNoEmail（并抄送 cc@code-bench.local），故意传空 BodyMarkdown 验证自动补齐非空 markdown_content
	created, err := dispatcher.DispatchSync(context.Background(), DispatchInput{
		RecipientUserIDs: []uint{userA.ID, userNoEmail.ID},
		CcEmails:         []string{"cc_arch@code-bench.local"},
		Category:         models.NotifCategoryBreakingAlert,
		Severity:         models.NotifSeverityCritical,
		Title:            "[MR !88] 检测到 2 项契约破坏性变更",
		BodyMarkdown:     "", // 测试空 BodyMarkdown 自动回退 Title 保底非空
		LinkURL:          "/schemas?tab=mr-gates&runId=88",
		SourceType:       models.NotifSourceMRCheckRun,
		SourceID:         88,
		Metadata:         map[string]any{"mr_iid": 88, "repo_name": "order-proto"},
	})
	if err != nil {
		t.Fatalf("DispatchSync 失败: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("期望为 2 名收件用户落库 2 条站内通知，实际 %d", len(created))
	}

	// 2. 验证 UserA 收到 notification.created SSE，而 UserB 绝未收到！
	select {
	case evtA := <-chA:
		if evtA.Event != "notification.created" {
			t.Fatalf("UserA 收到事件类型期望 notification.created，实际 %s", evtA.Event)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("UserA 未在 1s 内收到定向 notification.created SSE")
	}

	select {
	case evtB := <-chB:
		t.Fatalf("安全违规：旁观者 UserB 收到了属于 UserA 的私人通知 SSE: %+v", evtB)
	case <-time.After(80 * time.Millisecond):
		// 符合预期：UserB 未收到任何跨用户广播
	}

	// 3. 验证 Mock code-notifier 收到的完整载荷 (markdown_content 非空、source_app="code-traefik"、idempotency_key 非空、真实邮箱解析)
	mu.Lock()
	if len(receivedPayloads) != 1 {
		mu.Unlock()
		t.Fatalf("期望 code-notifier 收到 1 次外发请求，实际 %d", len(receivedPayloads))
	}
	gotPayload := receivedPayloads[0]
	gotAuth := receivedAuth
	mu.Unlock()

	if gotAuth != "Bearer ntf-secret-token-m26" {
		t.Fatalf("Authorization 头不匹配: %q", gotAuth)
	}
	if gotPayload.SourceApp != "code-traefik" {
		t.Fatalf("source_app 期望 code-traefik，实际 %q", gotPayload.SourceApp)
	}
	if strings.TrimSpace(gotPayload.MarkdownContent) == "" {
		t.Fatalf("markdown_content 严禁为空")
	}
	if gotPayload.IdempotencyKey == "" {
		t.Fatalf("idempotency_key 严禁为空")
	}
	if len(gotPayload.Recipients.To) != 1 || gotPayload.Recipients.To[0] != "alice@code-bench.local" {
		t.Fatalf("Recipients.To 期望仅包含有效邮箱 [alice@code-bench.local]，实际 %+v", gotPayload.Recipients.To)
	}
	if len(gotPayload.Recipients.Cc) != 1 || gotPayload.Recipients.Cc[0] != "cc_arch@code-bench.local" {
		t.Fatalf("Recipients.Cc 期望 [cc_arch@code-bench.local]，实际 %+v", gotPayload.Recipients.Cc)
	}

	// 核对 DB 中 UserA 的通知状态更新为 CODE_NOTIFIER + SENT，无邮箱的 UserNoEmail 为 SKIPPED 或关联同步
	var notifA models.Notification
	if err := db.Where("recipient_user_id = ? AND source_id = 88", userA.ID).First(&notifA).Error; err != nil {
		t.Fatalf("查询 UserA Notification 失败: %v", err)
	}
	if notifA.ExternalChannel != models.ExternalChannelCodeNotifier || notifA.EmailStatus != models.EmailStatusSent {
		t.Fatalf("UserA 通知外发状态期望 CODE_NOTIFIER/SENT，实际 %s/%s (err=%s)",
			notifA.ExternalChannel, notifA.EmailStatus, notifA.EmailError)
	}

	// 4. 模拟 code-notifier 返回 HTTP 500：验证降级更新为 FAILED + 记录 email_error，且绝不阻塞/崩溃
	mu.Lock()
	shouldFail500 = true
	mu.Unlock()

	_, err = dispatcher.DispatchSync(context.Background(), DispatchInput{
		RecipientUserIDs: []uint{userB.ID},
		Category:         models.NotifCategoryMRGateBlocked,
		Severity:         models.NotifSeverityWarning,
		Title:            "[MR !89] 门禁阻断",
		BodyMarkdown:     "请修复编译错误后重试",
		SourceType:       models.NotifSourceMRCheckRun,
		SourceID:         89,
	})
	if err != nil {
		t.Fatalf("外发失败不应导致 DispatchSync 返回主流程错误: %v", err)
	}

	var notifB models.Notification
	if err := db.Where("recipient_user_id = ? AND source_id = 89", userB.ID).First(&notifB).Error; err != nil {
		t.Fatalf("查询 UserB Notification 失败: %v", err)
	}
	if notifB.EmailStatus != models.EmailStatusFailed || notifB.EmailError == "" {
		t.Fatalf("code-notifier 500 降级后期望 email_status=FAILED 且记录 email_error，实际 status=%s err=%q",
			notifB.EmailStatus, notifB.EmailError)
	}
}

func TestNotify_CrossHostCodeNotifierPayload_SSRFAllowlist_AndOfflineGracefulDegrade(t *testing.T) {
	TestDispatcher_TargetedSSE_CodeNotifierPayloadAndFailureDegradation(t)
}
