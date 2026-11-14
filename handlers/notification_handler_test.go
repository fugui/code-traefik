package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"code-common/backend/testdb"
	"code-traefik/config"
	"code-traefik/models"

	"gorm.io/datatypes"
)

func TestNotificationHandler_ListUnreadReadAll_AndAntiIDOR403(t *testing.T) {
	if os.Getenv("TEST_DB_DSN") == "" {
		t.Skip("TEST_DB_DSN 未配置，跳过数据库隔离测试")
	}

	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}

	r, _ := setupGoldenSuiteTestServer(t, db)
	userA, tokenA := createTestUser(t, db, "notif_user_a", "member")
	userB, tokenB := createTestUser(t, db, "notif_user_b", "member")

	// 为 UserA 创建 2 条未读通知，为 UserB 创建 1 条未读通知
	nA1 := models.Notification{
		RecipientUserID: userA.ID,
		RecipientEmail:  "usera@example.com",
		Category:        models.NotifCategoryBreakingAlert,
		Severity:        models.NotifSeverityCritical,
		Title:           "UserA 告警 1",
		BodyMarkdown:    "详情 1",
		LinkURL:         "/schemas?tab=mr-gates",
		MetadataJSON:    datatypes.JSON([]byte(`{}`)),
	}
	nA2 := models.Notification{
		RecipientUserID: userA.ID,
		RecipientEmail:  "usera@example.com",
		Category:        models.NotifCategoryDuplicateWarn,
		Severity:        models.NotifSeverityWarning,
		Title:           "UserA 告警 2",
		BodyMarkdown:    "详情 2",
		LinkURL:         "/schemas?tab=lint-governance",
		MetadataJSON:    datatypes.JSON([]byte(`{}`)),
	}
	nB1 := models.Notification{
		RecipientUserID: userB.ID,
		RecipientEmail:  "userb@example.com",
		Category:        models.NotifCategoryMRGateBlocked,
		Severity:        models.NotifSeverityWarning,
		Title:           "UserB 专属通知",
		BodyMarkdown:    "UserB 私有详情",
		MetadataJSON:    datatypes.JSON([]byte(`{}`)),
	}
	for _, item := range []*models.Notification{&nA1, &nA2, &nB1} {
		if err := db.Create(item).Error; err != nil {
			t.Fatalf("创建测试通知失败: %v", err)
		}
	}

	// 1. UserA 查询未读数 -> 2
	reqUnreadA := httptest.NewRequest(http.MethodGet, "/api/notifications/unread-count", nil)
	reqUnreadA.Header.Set("Authorization", "Bearer "+tokenA)
	wUnreadA := httptest.NewRecorder()
	r.ServeHTTP(wUnreadA, reqUnreadA)
	if wUnreadA.Code != http.StatusOK {
		t.Fatalf("GET /api/notifications/unread-count 期望 200，实际 %d", wUnreadA.Code)
	}
	var unreadResp struct {
		UnreadCount int64 `json:"unread_count"`
	}
	if err := json.Unmarshal(wUnreadA.Body.Bytes(), &unreadResp); err != nil || unreadResp.UnreadCount != 2 {
		t.Fatalf("UserA 未读数期望 2，实际 %d (err=%v)", unreadResp.UnreadCount, err)
	}

	// 2. UserA 查询收件箱列表 -> 仅返回 UserA 的 2 条，绝不包含 UserB 的通知
	reqListA := httptest.NewRequest(http.MethodGet, "/api/notifications?page=1&page_size=25", nil)
	reqListA.Header.Set("Authorization", "Bearer "+tokenA)
	wListA := httptest.NewRecorder()
	r.ServeHTTP(wListA, reqListA)
	if wListA.Code != http.StatusOK {
		t.Fatalf("GET /api/notifications 期望 200，实际 %d", wListA.Code)
	}
	var listResp struct {
		Items       []models.Notification `json:"items"`
		Total       int64                 `json:"total"`
		UnreadCount int64                 `json:"unread_count"`
	}
	if err := json.Unmarshal(wListA.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("解析通知列表失败: %v", err)
	}
	if listResp.Total != 2 || len(listResp.Items) != 2 {
		t.Fatalf("UserA 通知列表期望 2 条，实际 total=%d len=%d", listResp.Total, len(listResp.Items))
	}
	for _, it := range listResp.Items {
		if it.RecipientUserID != userA.ID {
			t.Fatalf("越权泄露：UserA 列表中出现了属于用户 #%d 的通知", it.RecipientUserID)
		}
	}

	// 3. Anti-IDOR 越权拦截：UserA 尝试标记 UserB 的通知 (nB1.ID) 为已读 -> 必须返回 403 Forbidden！
	reqIDOR := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/notifications/%d/read", nB1.ID), nil)
	reqIDOR.Header.Set("Authorization", "Bearer "+tokenA)
	wIDOR := httptest.NewRecorder()
	r.ServeHTTP(wIDOR, reqIDOR)
	if wIDOR.Code != http.StatusForbidden {
		t.Fatalf("Anti-IDOR 越权标记他人通知期望 403 Forbidden，实际 %d body=%s", wIDOR.Code, wIDOR.Body.String())
	}

	// 4. UserA 标记自己的 nA1 为已读 -> 200 OK，剩余未读数为 1
	reqRead1 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/notifications/%d/read", nA1.ID), nil)
	reqRead1.Header.Set("Authorization", "Bearer "+tokenA)
	wRead1 := httptest.NewRecorder()
	r.ServeHTTP(wRead1, reqRead1)
	if wRead1.Code != http.StatusOK {
		t.Fatalf("标记本人通知已读期望 200，实际 %d", wRead1.Code)
	}

	// 5. UserA 执行一键全部已读 POST /api/notifications/read-all -> UserA 未读归 0，UserB 的 nB1 仍保持未读
	reqReadAll := httptest.NewRequest(http.MethodPost, "/api/notifications/read-all", nil)
	reqReadAll.Header.Set("Authorization", "Bearer "+tokenA)
	wReadAll := httptest.NewRecorder()
	r.ServeHTTP(wReadAll, reqReadAll)
	if wReadAll.Code != http.StatusOK {
		t.Fatalf("POST /api/notifications/read-all 期望 200，实际 %d", wReadAll.Code)
	}

	var checkB models.Notification
	if err := db.First(&checkB, nB1.ID).Error; err != nil || checkB.IsRead {
		t.Fatalf("UserA read-all 绝不能影响 UserB 的未读状态: %+v", checkB)
	}
	_ = tokenB
}
