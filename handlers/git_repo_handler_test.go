package handlers_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	commonModels "code-common/backend/models"
	"code-traefik/config"
	"code-traefik/handlers"
	"code-traefik/models"

	"gorm.io/gorm/clause"
)

type handlerMockCommit struct {
	SHA     string
	Message string
	Author  string
	Email   string
	Files   map[string]string
}

type handlerMockGitLab struct {
	mu      sync.RWMutex
	token   string
	commits map[string]*handlerMockCommit
}

func newHandlerMockGitLab(token string) *handlerMockGitLab {
	return &handlerMockGitLab{
		token:   token,
		commits: make(map[string]*handlerMockCommit),
	}
}

func (m *handlerMockGitLab) setBranch(branch string, snap *handlerMockCommit) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commits[branch] = snap
	m.commits[snap.SHA] = snap
}

func (m *handlerMockGitLab) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if m.token != "" {
		if r.Header.Get("PRIVATE-TOKEN") != m.token && r.Header.Get("Authorization") != "Bearer "+m.token {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"401 Unauthorized"}`))
			return
		}
	}
	rawPath := r.URL.RawPath
	if rawPath == "" {
		rawPath = r.URL.Path
	}
	w.Header().Set("Content-Type", "application/json")

	if strings.HasSuffix(rawPath, "/repository/branches") {
		m.mu.RLock()
		defer m.mu.RUnlock()
		type branchResp struct {
			Name string `json:"name"`
		}
		var out []branchResp
		for k := range m.commits {
			if len(k) < 40 {
				out = append(out, branchResp{Name: k})
			}
		}
		_ = json.NewEncoder(w).Encode(out)
		return
	}

	if idx := strings.Index(rawPath, "/repository/commits/"); idx >= 0 {
		ref := rawPath[idx+len("/repository/commits/"):]
		if unesc, err := url.PathUnescape(ref); err == nil {
			ref = unesc
		}
		m.mu.RLock()
		snap := m.commits[ref]
		m.mu.RUnlock()
		if snap == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":             snap.SHA,
			"message":        snap.Message,
			"author_name":    snap.Author,
			"author_email":   snap.Email,
			"committed_date": time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	if strings.HasSuffix(rawPath, "/repository/tree") {
		ref := r.URL.Query().Get("ref")
		subPath := strings.Trim(r.URL.Query().Get("path"), "/")
		m.mu.RLock()
		snap := m.commits[ref]
		m.mu.RUnlock()
		if snap == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		type treeItem struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Type string `json:"type"`
			Path string `json:"path"`
		}
		var items []treeItem
		for p, content := range snap.Files {
			if subPath != "" && !strings.HasPrefix(p, subPath+"/") && p != subPath {
				continue
			}
			sum := sha256.Sum256([]byte(content))
			items = append(items, treeItem{
				ID:   hex.EncodeToString(sum[:])[:40],
				Name: p,
				Type: "blob",
				Path: p,
			})
		}
		_ = json.NewEncoder(w).Encode(items)
		return
	}

	if idx := strings.Index(rawPath, "/repository/files/"); idx >= 0 && strings.HasSuffix(rawPath, "/raw") {
		escapedFile := rawPath[idx+len("/repository/files/") : len(rawPath)-len("/raw")]
		filePath, _ := url.PathUnescape(escapedFile)
		ref := r.URL.Query().Get("ref")
		m.mu.RLock()
		snap := m.commits[ref]
		var body string
		var ok bool
		if snap != nil {
			body, ok = snap.Files[filePath]
		}
		m.mu.RUnlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(body))
		return
	}

	w.WriteHeader(http.StatusNotFound)
}

func TestGitRepoAndBundles_AESEncryptionRedaction_AuditAndSync(t *testing.T) {
	t.Setenv("TRAEFIK_CRYPTO_KEY", "test-traefik-crypto-key-m25-32bytes!")
	r, db := setupProtoDocRouterAndDB(t)

	const secretToken = "glpat-secret-token-987654"
	const webhookSecret = "whsec-very-secret-4321"
	mockGit := newHandlerMockGitLab(secretToken)
	mockGit.setBranch("main", &handlerMockCommit{
		SHA:     "1111111111111111111111111111111111111111",
		Message: "feat: init common and order contracts",
		Author:  "Alice",
		Email:   "alice@example.com",
		Files: map[string]string{
			"common/proto/common.proto": `syntax = "proto3";
package dophi.common.v1;

// 通用请求头
message RequestMeta {
  string trace_id = 1;
}
`,
			"services/order/order.proto": `syntax = "proto3";
package dophi.order.v1;
import "common.proto";

// 订单请求
message CreateOrderReq {
  dophi.common.v1.RequestMeta meta = 1;
  string order_no = 2;
}

// 订单响应
message CreateOrderRsp {
  int32 code = 1;
}

// 订单契约服务
service OrderService {
  // 创建订单
  rpc CreateOrder (CreateOrderReq) returns (CreateOrderRsp);
}
`,
		},
	})
	mockGit.setBranch("release/v1", mockGit.commits["main"])

	gitSrv := httptest.NewServer(mockGit)
	defer gitSrv.Close()

	adminUser := createTestUserInDB(t, db, "git_admin", config.AdminRole)
	normalUser := createTestUserInDB(t, db, "git_viewer", "viewer")
	adminJWT := generateTestJWT(t, adminUser)
	viewerJWT := generateTestJWT(t, normalUser)

	// 1. 非管理员写接口必须返回 403
	{
		body, _ := json.Marshal(map[string]any{
			"name":     "viewer-attempt",
			"provider": "GITLAB",
			"repo_url": gitSrv.URL + "/org/contracts.git",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/git-repos", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+viewerJWT)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("普通用户创建 GitRepo 期望 403，实际 %d: %s", w.Code, w.Body.String())
		}
	}

	// 2. SSRF 防护拦截私有/回环地址（尚未放行 outbound_trusted_cidrs）以及目录穿越拦截
	{
		ssrfBody, _ := json.Marshal(map[string]any{
			"name":     "ssrf-blocked-repo",
			"provider": "GITLAB",
			"repo_url": "http://169.254.169.254/org/contracts.git",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/git-repos", bytes.NewReader(ssrfBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminJWT)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("未放行元数据地址期望 SSRF 拦截返回 400，实际 %d: %s", w.Code, w.Body.String())
		}
	}

	// 为测试 Mock GitLab Server 写入 127.0.0.0/8 白名单
	if err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "setting_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"setting_val"}),
	}).Create(&models.TraefikSetting{
		SettingKey:  "outbound_trusted_cidrs",
		SettingVal:  "127.0.0.0/8",
		Description: "test trusted git server loopback",
	}).Error; err != nil {
		t.Fatalf("写入 outbound_trusted_cidrs 失败: %v", err)
	}

	// 路径穿越校验（.. 禁止）
	{
		badPathBody, _ := json.Marshal(map[string]any{
			"name":     "traversal-repo",
			"provider": "GITLAB",
			"repo_url": gitSrv.URL + "/org/contracts.git",
			"default_bundle": map[string]any{
				"bundle_name":        "bad-bundle",
				"subdir_path":        "../secrets",
				"target_schema_name": "bad-schema",
			},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/git-repos", bytes.NewReader(badPathBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminJWT)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("路径穿越 subdir_path 期望返回 400，实际 %d: %s", w.Code, w.Body.String())
		}
	}

	// 3. 管理员创建 Monorepo Git 仓库（内联默认 common-proto BASE_LIB Bundle）
	var repoID uint64
	var commonBundleID uint64
	{
		createBody, _ := json.Marshal(map[string]any{
			"name":                 "dophi-monorepo",
			"provider":             "GITLAB",
			"repo_url":             gitSrv.URL + "/dophi/contracts.git",
			"api_base_url":         gitSrv.URL,
			"default_branch":       "main",
			"auth_type":            "TOKEN",
			"credential_plain":     secretToken,
			"webhook_secret_plain": webhookSecret,
			"sync_mode":            "MANUAL",
			"auto_compile":         true,
			"lint_on_sync":         true,
			"default_bundle": map[string]any{
				"bundle_name":        "common-base",
				"subdir_path":        "common/proto",
				"include_dirs":       []string{"."},
				"file_globs":         []string{"*.proto"},
				"target_schema_name": "common-proto",
				"category":           "BASE_LIB",
				"auto_compile":       true,
				"auto_activate":      true,
				"lint_on_sync":       true,
			},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/git-repos", bytes.NewReader(createBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminJWT)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("创建 GitRepo 期望 201，实际 %d: %s", w.Code, w.Body.String())
		}
		respRaw := w.Body.String()
		if strings.Contains(respRaw, secretToken) || strings.Contains(respRaw, webhookSecret) ||
			strings.Contains(respRaw, "credentials_enc") || strings.Contains(respRaw, "webhook_secret_enc") {
			t.Fatalf("POST /api/git-repos 响应禁止包含任何明文或密文字段: %s", respRaw)
		}
		var created struct {
			ID               uint64 `json:"id"`
			HasCredentials   bool   `json:"has_credentials"`
			CredentialHint   string `json:"credential_hint"`
			HasWebhookSecret bool   `json:"has_webhook_secret"`
			Bundles          []struct {
				ID               uint64 `json:"id"`
				TargetSchemaName string `json:"target_schema_name"`
			} `json:"bundles"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
			t.Fatalf("解析创建响应失败: %v", err)
		}
		if created.ID == 0 || !created.HasCredentials || !created.HasWebhookSecret || !strings.Contains(created.CredentialHint, "****") {
			t.Fatalf("脱敏元数据字段异常: %+v", created)
		}
		if len(created.Bundles) != 1 || created.Bundles[0].TargetSchemaName != "common-proto" {
			t.Fatalf("内联默认 Bundle 未创建成功: %+v", created.Bundles)
		}
		repoID = created.ID
		commonBundleID = created.Bundles[0].ID
	}

	// 验证数据库确实 AES 加密存储，且 GET /api/git-repos 也严格脱敏
	var storedRepo models.GitRepo
	if err := db.First(&storedRepo, repoID).Error; err != nil {
		t.Fatalf("查询 storedRepo 失败: %v", err)
	}
	if storedRepo.CredentialsEnc == "" || storedRepo.CredentialsEnc == secretToken || storedRepo.WebhookSecretEnc == webhookSecret {
		t.Fatalf("数据库凭据未经过 AES-256-GCM 加密")
	}

	{
		req := httptest.NewRequest(http.MethodGet, "/api/git-repos?page=1&page_size=25", nil)
		req.Header.Set("Authorization", "Bearer "+viewerJWT)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /api/git-repos 期望 200，实际 %d: %s", w.Code, w.Body.String())
		}
		listRaw := w.Body.String()
		if strings.Contains(listRaw, secretToken) || strings.Contains(listRaw, webhookSecret) || strings.Contains(listRaw, storedRepo.CredentialsEnc) {
			t.Fatalf("GET /api/git-repos 泄漏凭据/密文: %s", listRaw)
		}
	}

	// 4. 添加第二个 Monorepo Bundle (order-proto) 并验证重复 target_schema_name 返回 409
	var orderBundleID uint64
	{
		bBody, _ := json.Marshal(map[string]any{
			"bundle_name":        "order-svc",
			"subdir_path":        "services/order",
			"include_dirs":       []string{"."},
			"target_schema_name": "order-proto",
			"category":           "BUSINESS",
			"auto_compile":       true,
			"auto_activate":      true,
			"lint_on_sync":       true,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/git-repos/"+strconv.FormatUint(repoID, 10)+"/bundles", bytes.NewReader(bBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminJWT)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("添加第 2 个 Bundle 期望 201，实际 %d: %s", w.Code, w.Body.String())
		}
		var bResp struct {
			ID uint64 `json:"id"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &bResp)
		orderBundleID = bResp.ID
		if orderBundleID == 0 {
			t.Fatalf("orderBundleID 为 0: %s", w.Body.String())
		}
	}

	{
		dupBody, _ := json.Marshal(map[string]any{
			"bundle_name":        "order-dup-target",
			"subdir_path":        "services/other",
			"target_schema_name": "common-proto",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/git-repos/"+strconv.FormatUint(repoID, 10)+"/bundles", bytes.NewReader(dupBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminJWT)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusConflict {
			t.Fatalf("重复 target_schema_name 期望返回 409 Conflict，实际 %d: %s", w.Code, w.Body.String())
		}
	}

	// 5. PUT 更新描述时留空凭据，必须保持原有加密凭据不变
	{
		updBody, _ := json.Marshal(map[string]any{
			"description": "updated monorepo desc",
		})
		req := httptest.NewRequest(http.MethodPut, "/api/git-repos/"+strconv.FormatUint(repoID, 10), bytes.NewReader(updBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminJWT)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("PUT /api/git-repos/:id 期望 200，实际 %d: %s", w.Code, w.Body.String())
		}
		var reloaded models.GitRepo
		_ = db.First(&reloaded, repoID).Error
		dec, decErr := reloaded.DecryptCredentials()
		if decErr != nil || dec != secretToken {
			t.Fatalf("留空 credential_plain 更新后凭据应当保留原值，实际 dec=%q err=%v", dec, decErr)
		}
	}

	// 6. 订阅 SSE 并触发 POST /api/git-repos/:id/sync，断言 202 与 SSE git_sync.completed 事件
	subID, sseCh := handlers.GetGlobalSSEBroker().Subscribe()
	defer handlers.GetGlobalSSEBroker().Unsubscribe(subID)

	{
		req := httptest.NewRequest(http.MethodPost, "/api/git-repos/"+strconv.FormatUint(repoID, 10)+"/sync", bytes.NewBufferString(`{"branch":"main"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminJWT)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusAccepted {
			t.Fatalf("POST /api/git-repos/:id/sync 期望 202 Accepted，实际 %d: %s", w.Code, w.Body.String())
		}
		var syncAck struct {
			RepoID uint64 `json:"repo_id"`
			Branch string `json:"branch"`
			Status string `json:"status"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &syncAck)
		if syncAck.RepoID != repoID || syncAck.Status != models.GitSyncStatusRunning {
			t.Fatalf("同步 202 响应异常: %+v", syncAck)
		}
	}

	// 等待 SSE git_sync.completed 事件到达
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	gotCompleted := false
	for !gotCompleted {
		select {
		case evt := <-sseCh:
			if evt.Event == "git_sync.completed" {
				gotCompleted = true
			}
		case <-ctx.Done():
			t.Fatalf("超时未收到 SSE git_sync.completed 推送事件")
		}
	}

	// 验证 2 个 Bundle 各自编译生成了 GIT_SYNC ProtoSchema
	var schemas []models.ProtoSchema
	if err := db.Where("git_repo_id = ?", repoID).Order("id ASC").Find(&schemas).Error; err != nil {
		t.Fatalf("查询编译后的 ProtoSchema 失败: %v", err)
	}
	if len(schemas) != 2 {
		t.Fatalf("期望生成 2 条 GIT_SYNC ProtoSchema，实际 %d", len(schemas))
	}
	for _, sc := range schemas {
		if sc.SourceType != models.ProtoSourceGitSync || sc.SourceBundleID == nil {
			t.Fatalf("ProtoSchema 来源元数据不完整: %+v", sc)
		}
	}

	// 7. 分支查询与 SHA256 只读对账接口验证
	{
		req := httptest.NewRequest(http.MethodGet, "/api/git-repos/"+strconv.FormatUint(repoID, 10)+"/branches", nil)
		req.Header.Set("Authorization", "Bearer "+viewerJWT)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /branches 期望 200，实际 %d: %s", w.Code, w.Body.String())
		}
		var brResp struct {
			DefaultBranch string   `json:"default_branch"`
			Branches      []string `json:"branches"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &brResp)
		if brResp.DefaultBranch != "main" || len(brResp.Branches) < 2 {
			t.Fatalf("分支列表返回值异常: %+v", brResp)
		}
	}

	{
		req := httptest.NewRequest(http.MethodGet, "/api/git-repos/"+strconv.FormatUint(repoID, 10)+"/consistency?branch=main", nil)
		req.Header.Set("Authorization", "Bearer "+viewerJWT)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /consistency 期望 200，实际 %d: %s", w.Code, w.Body.String())
		}
		var consResp struct {
			InSync  bool `json:"in_sync"`
			Summary struct {
				InSync int `json:"in_sync"`
			} `json:"summary"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &consResp)
		if !consResp.InSync || consResp.Summary.InSync != 2 {
			t.Fatalf("同步后一致性对账应为 2 条 in_sync，实际 %+v", consResp)
		}
	}

	// 推进远端 Commit 修改 order.proto -> 验证 consistency 检测出 modified_files
	mockGit.setBranch("main", &handlerMockCommit{
		SHA:     "2222222222222222222222222222222222222222",
		Message: "feat: modify order",
		Author:  "Alice",
		Email:   "alice@example.com",
		Files: map[string]string{
			"common/proto/common.proto":  mockGit.commits["main"].Files["common/proto/common.proto"],
			"services/order/order.proto": mockGit.commits["main"].Files["services/order/order.proto"] + "\n// changed\n",
		},
	})
	{
		req := httptest.NewRequest(http.MethodGet, "/api/git-repos/"+strconv.FormatUint(repoID, 10)+"/consistency?branch=main", nil)
		req.Header.Set("Authorization", "Bearer "+viewerJWT)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("二次 GET /consistency 期望 200，实际 %d: %s", w.Code, w.Body.String())
		}
		var consResp struct {
			InSync        bool     `json:"in_sync"`
			ModifiedFiles []string `json:"modified_files"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &consResp)
		if consResp.InSync || len(consResp.ModifiedFiles) != 1 {
			t.Fatalf("修改远端文件后一致性对账应检出 1 条 modified，实际 %+v", consResp)
		}
	}

	// 验证审计日志写入且脱敏（不包含 secretToken / webhookSecret）
	var auditLogs []commonModels.SysAuditLog
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = db.Where("action LIKE ?", "traefik_git:%").Find(&auditLogs).Error
		if len(auditLogs) >= 3 {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if len(auditLogs) == 0 {
		t.Fatalf("期望写入 traefik_git:* 审计日志，实际为 0 条")
	}
	auditRaw, _ := json.Marshal(auditLogs)
	if strings.Contains(string(auditRaw), secretToken) || strings.Contains(string(auditRaw), webhookSecret) {
		t.Fatalf("SysAuditLog 绝对不能记录明文凭据: %s", string(auditRaw))
	}

	// 8. 删除单个 Bundle 与整个仓库，验证历史 ProtoSchema 安全保留并 ON DELETE SET NULL
	{
		req := httptest.NewRequest(http.MethodDelete, "/api/git-repos/"+strconv.FormatUint(repoID, 10)+"/bundles/"+strconv.FormatUint(orderBundleID, 10), nil)
		req.Header.Set("Authorization", "Bearer "+adminJWT)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("DELETE bundle 期望 200，实际 %d: %s", w.Code, w.Body.String())
		}
		var orderSchema models.ProtoSchema
		if err := db.Where("name = ?", "order-proto").First(&orderSchema).Error; err != nil {
			t.Fatalf("删除 Bundle 后历史 ProtoSchema 应当保留，查询失败: %v", err)
		}
		if orderSchema.SourceBundleID != nil {
			t.Fatalf("删除 Bundle 后 ProtoSchema.SourceBundleID 应当置 NULL (ON DELETE SET NULL)，实际 %v", *orderSchema.SourceBundleID)
		}
	}
	_ = commonBundleID

	{
		req := httptest.NewRequest(http.MethodDelete, "/api/git-repos/"+strconv.FormatUint(repoID, 10), nil)
		req.Header.Set("Authorization", "Bearer "+adminJWT)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("DELETE /api/git-repos/:id 期望 200，实际 %d: %s", w.Code, w.Body.String())
		}
		var remainFiles int64
		_ = db.Model(&models.ProtoFile{}).Where("git_repo_id = ?", repoID).Count(&remainFiles).Error
		if remainFiles != 0 {
			t.Fatalf("删除仓库后应级联删除 ProtoFile，实际剩余 %d", remainFiles)
		}
		var commonSchema models.ProtoSchema
		if err := db.Where("name = ?", "common-proto").First(&commonSchema).Error; err != nil {
			t.Fatalf("删除 GitRepo 后已入池 ProtoSchema 归档应保留: %v", err)
		}
		if commonSchema.GitRepoID != nil || commonSchema.SourceBundleID != nil {
			t.Fatalf("删除 GitRepo 后 ProtoSchema 外键应置 NULL: %+v", commonSchema)
		}
	}
}
