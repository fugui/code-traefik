package gitclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-traefik/config"
	"code-traefik/internal/server/decoder"
	"code-traefik/models"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
	gitserver "github.com/go-git/go-git/v5/plumbing/transport/server"
	"github.com/go-git/go-git/v5/storage/memory"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type singleRepoLoader struct {
	st storer.Storer
}

func (l singleRepoLoader) Load(_ *transport.Endpoint) (storer.Storer, error) {
	return l.st, nil
}

type mockCommitSnapshot struct {
	SHA     string
	Message string
	Author  string
	Email   string
	Files   map[string]string // repo-relative POSIX path -> content
}

type mockGitLabServer struct {
	mu      sync.RWMutex
	commits map[string]*mockCommitSnapshot // branch or sha -> snapshot
	token   string
}

func newMockGitLabServer(token string) *mockGitLabServer {
	return &mockGitLabServer{
		commits: make(map[string]*mockCommitSnapshot),
		token:   token,
	}
}

func (m *mockGitLabServer) setBranch(branch string, snap *mockCommitSnapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commits[branch] = snap
	m.commits[snap.SHA] = snap
}

func (m *mockGitLabServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	// GET /api/v4/projects/:id/repository/branches
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

	// GET /api/v4/projects/:id/repository/commits/:ref
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

	// GET /api/v4/projects/:id/repository/tree?ref=...&path=...
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

	// GET /api/v4/projects/:id/repository/files/:file_path/raw?ref=...
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

func setupGitClientTestDB(t *testing.T) (*gorm.DB, *decoder.Registry) {
	t.Helper()
	if os.Getenv("TEST_DB_DSN") == "" {
		t.Skip("TEST_DB_DSN 未配置，跳过数据库隔离测试")
	}
	t.Setenv("TRAEFIK_CRYPTO_KEY", "test-traefik-crypto-key-m25-32bytes!")
	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}
	reg, err := decoder.NewRegistry(db)
	if err != nil {
		t.Fatalf("NewRegistry 失败: %v", err)
	}
	return db, reg
}

func TestSyncRepoMonorepoBundles_VersionsSoftDeleteAndConsistency(t *testing.T) {
	db, reg := setupGitClientTestDB(t)

	const mockToken = "glpat-sync-test-secret-8899"
	mockSrv := newMockGitLabServer(mockToken)
	srv := httptest.NewServer(mockSrv)
	defer srv.Close()

	c1SHA := "1111111111111111111111111111111111111111"
	mockSrv.setBranch("main", &mockCommitSnapshot{
		SHA:     c1SHA,
		Message: "feat: initial monorepo contracts",
		Author:  "alice",
		Email:   "alice@example.com",
		Files: map[string]string{
			"README.md": "# Proto Monorepo",
			"shared/proto/common/base.proto": `syntax = "proto3";
package common.v1;
// MetaHeader 公共请求头
message MetaHeader {
  string trace_id = 1;
}
`,
			"services/order/proto/order/v1/order.proto": `syntax = "proto3";
package order.v1;
import "common/base.proto";
message CreateOrderReq {
  common.v1.MetaHeader meta = 1;
  string sku = 2;
}
message CreateOrderRsp {
  string order_id = 1;
}
// OrderService 订单服务
service OrderService {
  // CreateOrder 下单接口
  rpc CreateOrder(CreateOrderReq) returns (CreateOrderRsp);
}
`,
			"services/order/proto/order/v1/extra.proto": `syntax = "proto3";
package order.v1;
message ExtraNote {
  string memo = 1;
}
`,
		},
	})

	repo := models.GitRepo{
		Name:          "monorepo-contracts",
		Provider:      models.GitProviderGitLab,
		RepoURL:       srv.URL + "/org/monorepo-contracts.git",
		APIBaseURL:    srv.URL + "/api/v4",
		DefaultBranch: "main",
		AuthType:      models.GitAuthToken,
		SyncMode:      models.GitSyncManual,
		AutoCompile:   true,
		LintOnSync:    true,
	}
	if err := repo.SetCredentials(mockToken); err != nil {
		t.Fatalf("加密凭证失败: %v", err)
	}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("创建 GitRepo 失败: %v", err)
	}

	baseBundle := models.GitProtoBundle{
		GitRepoID:        repo.ID,
		BundleName:       "common-base",
		TargetSchemaName: "monorepo-common-base",
		Category:         models.ProtoCategoryBaseLib,
		SubdirPath:       "shared/proto",
		FileGlobsJSON:    datatypes.JSON([]byte(`["*.proto"]`)),
		AutoCompile:      true,
		AutoActivate:     true,
		LintOnSync:       true,
	}
	orderBundle := models.GitProtoBundle{
		GitRepoID:        repo.ID,
		BundleName:       "order-svc",
		TargetSchemaName: "monorepo-order-svc",
		Category:         models.ProtoCategoryBusiness,
		SubdirPath:       "services/order/proto",
		FileGlobsJSON:    datatypes.JSON([]byte(`["*.proto"]`)),
		AutoCompile:      true,
		AutoActivate:     true,
		LintOnSync:       true,
	}
	if err := db.Create(&baseBundle).Error; err != nil {
		t.Fatalf("创建 baseBundle 失败: %v", err)
	}
	if err := db.Create(&orderBundle).Error; err != nil {
		t.Fatalf("创建 orderBundle 失败: %v", err)
	}

	var sseEvents []GitSyncEventPayload
	svc := NewSyncService(db, reg, func(ev GitSyncEventPayload) {
		sseEvents = append(sseEvents, ev)
	})

	// 1. SSRF 闭环检查：未将 127.0.0.1 加入 outbound_trusted_cidrs 时必须拒绝内网出站请求
	_, err := svc.SyncRepo(context.Background(), repo.ID, SyncOptions{OperatorID: 1})
	if err == nil {
		t.Fatalf("outbound_trusted_cidrs 未放行 127.0.0.1 时，SyncRepo 必须因 SSRF 防护而拒绝请求")
	}

	// 放行 127.0.0.1/32 白名单
	if err := db.Where("setting_key = ?", "outbound_trusted_cidrs").
		Assign(models.TraefikSetting{
			SettingKey: "outbound_trusted_cidrs",
			SettingVal: "127.0.0.1/32",
		}).
		FirstOrCreate(&models.TraefikSetting{}).Error; err != nil {
		t.Fatalf("更新 outbound_trusted_cidrs 失败: %v", err)
	}

	// 检查 ListBranches REST
	branches, err := svc.ListBranches(context.Background(), repo.ID)
	if err != nil || len(branches) == 0 || branches[0] != "main" {
		t.Fatalf("ListBranches 异常: branches=%v err=%v", branches, err)
	}

	// 2. 首次 Monorepo 全量同步 (Commit c1)
	res1, err := svc.SyncRepo(context.Background(), repo.ID, SyncOptions{OperatorID: 7})
	if err != nil {
		t.Fatalf("首次 SyncRepo 失败: %v", err)
	}
	if res1.CommitSHA != c1SHA || len(res1.BundleResults) != 2 {
		t.Fatalf("SyncRepo 返回结果异常: %+v", res1)
	}
	if len(sseEvents) < 2 {
		t.Fatalf("预期每个 Bundle 同步均广播 git_sync.completed SSE 事件，实际=%d", len(sseEvents))
	}

	// 校验 Schema 与 Lint Report 与 ProtoFile 入库
	var orderSchema models.ProtoSchema
	if err := db.Where("name = ? AND is_active = true", orderBundle.TargetSchemaName).First(&orderSchema).Error; err != nil {
		t.Fatalf("order-svc 编译激活后的 ProtoSchema 未落库: %v", err)
	}
	if orderSchema.SourceType != models.ProtoSourceGitSync ||
		orderSchema.SourceBundleID == nil || *orderSchema.SourceBundleID != orderBundle.ID ||
		orderSchema.SourceCommitSHA != c1SHA {
		t.Fatalf("ProtoSchema 溯源元数据不完整: %+v", orderSchema)
	}

	var reportCnt int64
	db.Model(&models.CheckReport{}).Where("bundle_id = ? AND trigger_source = ?", orderBundle.ID, models.CheckTriggerGitSync).Count(&reportCnt)
	if reportCnt == 0 {
		t.Fatalf("SyncBundle 未生成 GIT_SYNC 静态规范检查报告")
	}

	// 3. 第二次同步 (Commit c2)：common/base.proto 无改动(幂等 v1)、order.proto 升级 v2、extra.proto 软删
	c2SHA := "2222222222222222222222222222222222222222"
	mockSrv.setBranch("main", &mockCommitSnapshot{
		SHA:     c2SHA,
		Message: "feat: add remark to CreateOrderReq and remove extra.proto",
		Author:  "bob",
		Email:   "bob@example.com",
		Files: map[string]string{
			"shared/proto/common/base.proto": `syntax = "proto3";
package common.v1;
// MetaHeader 公共请求头
message MetaHeader {
  string trace_id = 1;
}
`,
			"services/order/proto/order/v1/order.proto": `syntax = "proto3";
package order.v1;
import "common/base.proto";
message CreateOrderReq {
  common.v1.MetaHeader meta = 1;
  string sku = 2;
  string remark = 3;
}
message CreateOrderRsp {
  string order_id = 1;
}
// OrderService 订单服务
service OrderService {
  // CreateOrder 下单接口
  rpc CreateOrder(CreateOrderReq) returns (CreateOrderRsp);
}
`,
		},
	})

	res2, err := svc.SyncRepo(context.Background(), repo.ID, SyncOptions{OperatorID: 7})
	if err != nil {
		t.Fatalf("Commit c2 SyncRepo 失败: %v", err)
	}
	if res2.CommitSHA != c2SHA {
		t.Fatalf("预期 CommitSHA=%s, 实际=%s", c2SHA, res2.CommitSHA)
	}

	// 校验幂等性：base.proto 仍为 v1（仅 1 条 version 记录）
	var baseFile models.ProtoFile
	if err := db.Where("bundle_id = ? AND file_path = ? AND is_deleted = false", baseBundle.ID, "common/base.proto").First(&baseFile).Error; err != nil {
		t.Fatalf("查询 baseFile 失败: %v", err)
	}
	if baseFile.LatestVersionNum != 1 {
		t.Fatalf("内容未变更的文件不应重复递增版本，预期 v1 实际 v%d", baseFile.LatestVersionNum)
	}

	// order.proto 升级到 v2
	var orderFile models.ProtoFile
	if err := db.Where("bundle_id = ? AND file_path = ? AND is_deleted = false", orderBundle.ID, "order/v1/order.proto").First(&orderFile).Error; err != nil {
		t.Fatalf("查询 orderFile 失败: %v", err)
	}
	if orderFile.LatestVersionNum != 2 || orderFile.LatestCommitSHA != c2SHA {
		t.Fatalf("order.proto 预期升级至 v2 (%s)，实际 v%d (%s)", c2SHA, orderFile.LatestVersionNum, orderFile.LatestCommitSHA)
	}

	// extra.proto 应被软删且历史版本保留
	var extraFile models.ProtoFile
	if err := db.Where("bundle_id = ? AND file_path = ?", orderBundle.ID, "order/v1/extra.proto").First(&extraFile).Error; err != nil {
		t.Fatalf("被移除文件应软删保留而非物理删: %v", err)
	}
	if !extraFile.IsDeleted {
		t.Fatalf("extra.proto 的 is_deleted 必须为 true")
	}

	// 4. Read-Only SHA256 对账测试 (ComputeConsistency)
	consAligned, err := svc.ComputeConsistency(context.Background(), repo.ID, nil, "main")
	if err != nil {
		t.Fatalf("ComputeConsistency (aligned) 失败: %v", err)
	}
	if consAligned.Summary.InSync != 2 || consAligned.Summary.Modified != 0 || consAligned.Summary.RemoteOnly != 0 || consAligned.Summary.DBOnly != 0 {
		t.Fatalf("刚同步后应全部 in_sync，实际 summary=%+v items=%+v", consAligned.Summary, consAligned.Items)
	}

	// 远端推进至 Commit c3，但暂不同步：修改 order.proto、新增 payment.proto (remote_only)、删除 common/base.proto (db_only)
	c3SHA := "3333333333333333333333333333333333333333"
	mockSrv.setBranch("main", &mockCommitSnapshot{
		SHA:     c3SHA,
		Message: "feat: unpulled remote drift",
		Author:  "carol",
		Email:   "carol@example.com",
		Files: map[string]string{
			"services/order/proto/order/v1/order.proto": `syntax = "proto3";
package order.v1;
message DriftOrder { string id = 1; }
`,
			"services/order/proto/order/v1/payment.proto": `syntax = "proto3";
package order.v1;
message PaymentInfo { int64 amount_fen = 1; }
`,
		},
	})

	consDrift, err := svc.ComputeConsistency(context.Background(), repo.ID, nil, "main")
	if err != nil {
		t.Fatalf("ComputeConsistency (drift) 失败: %v", err)
	}
	if consDrift.Summary.Modified != 1 || consDrift.Summary.RemoteOnly != 1 || consDrift.Summary.DBOnly != 1 {
		t.Fatalf("SHA256 只读对账汇总不符合预期 (期望 mod=1,remote=1,db=1)，实际: %+v items=%+v", consDrift.Summary, consDrift.Items)
	}
	// 确认 ComputeConsistency 为只读操作，未写入 c3 到 DB
	var afterCheckFile models.ProtoFile
	_ = db.First(&afterCheckFile, orderFile.ID).Error
	if afterCheckFile.LatestVersionNum != 2 {
		t.Fatalf("ComputeConsistency 严禁修改数据库版本，预期仍为 v2 实际 v%d", afterCheckFile.LatestVersionNum)
	}
}

func TestSyncViaMemFS_SmartHTTP_AndPathTraversalGuard(t *testing.T) {
	db, reg := setupGitClientTestDB(t)

	// 构建纯内存 Git 仓库（零磁盘、零 os/exec git）
	storer := memory.NewStorage()
	fs := memfs.New()
	gitRepo, err := git.Init(storer, fs)
	if err != nil {
		t.Fatalf("git.Init memfs 失败: %v", err)
	}
	wt, err := gitRepo.Worktree()
	if err != nil {
		t.Fatalf("Worktree 失败: %v", err)
	}
	_ = fs.MkdirAll("billing/v1", 0755)
	f, err := fs.Create("billing/v1/billing.proto")
	if err != nil {
		t.Fatalf("memfs Create 失败: %v", err)
	}
	_, _ = f.Write([]byte(`syntax = "proto3";
package billing.v1;
message ChargeReq { int64 amount = 1; }
message ChargeRsp { string tx_id = 1; }
service BillingService { rpc Charge(ChargeReq) returns (ChargeRsp); }
`))
	_ = f.Close()
	if _, err := wt.Add("billing/v1/billing.proto"); err != nil {
		t.Fatalf("wt.Add 失败: %v", err)
	}
	commitHash, err := wt.Commit("init billing proto", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "memfs-bot",
			Email: "bot@example.com",
			When:  time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("wt.Commit 失败: %v", err)
	}

	// 构建纯 Go Smart-HTTP Git Server 供 go-git + memfs 拉取
	srvHandler := gitserver.NewServer(singleRepoLoader{st: storer})
	smartSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ep, err := transport.NewEndpoint("/billing.git")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sess, err := srvHandler.NewUploadPackSession(ep, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		defer sess.Close()

		if strings.HasSuffix(r.URL.Path, "/info/refs") && r.URL.Query().Get("service") == "git-upload-pack" {
			adv, err := sess.AdvertisedReferencesContext(r.Context())
			if err != nil {
				t.Logf("smart-http adv error: %v", err)
				http.Error(w, "adv: "+err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
			var buf bytes.Buffer
			enc := pktline.NewEncoder(&buf)
			_ = enc.EncodeString("# service=git-upload-pack\n")
			_ = enc.Flush()
			if err := adv.Encode(&buf); err != nil {
				t.Logf("smart-http adv.Encode error: %v", err)
				http.Error(w, "adv.Encode: "+err.Error(), http.StatusInternalServerError)
				return
			}
			_, _ = w.Write(buf.Bytes())
			return
		}
		if strings.HasSuffix(r.URL.Path, "/git-upload-pack") {
			if _, err := sess.AdvertisedReferencesContext(r.Context()); err != nil {
				t.Logf("smart-http stateless-adv error: %v", err)
				http.Error(w, "stateless-adv: "+err.Error(), http.StatusInternalServerError)
				return
			}
			reqBody, err := io.ReadAll(r.Body)
			if err != nil {
				t.Logf("smart-http readBody error: %v", err)
				http.Error(w, "readBody: "+err.Error(), http.StatusBadRequest)
				return
			}
			upReq := packp.NewUploadPackRequest()
			if err := upReq.Decode(bytes.NewReader(reqBody)); err != nil {
				t.Logf("smart-http decode error: %v (body=%q)", err, string(reqBody))
				http.Error(w, "decode: "+err.Error(), http.StatusBadRequest)
				return
			}
			upResp, err := sess.UploadPack(r.Context(), upReq)
			if err != nil {
				t.Logf("smart-http uploadPack error: %v", err)
				http.Error(w, "uploadPack: "+err.Error(), http.StatusInternalServerError)
				return
			}
			defer upResp.Close()
			w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
			var out bytes.Buffer
			if err := upResp.Encode(&out); err != nil && !errors.Is(err, io.EOF) {
				t.Logf("smart-http encode error: %v", err)
				http.Error(w, "encode: "+err.Error(), http.StatusInternalServerError)
				return
			}
			_, _ = w.Write(out.Bytes())
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer smartSrv.Close()

	repo := models.GitRepo{
		Name:          "generic-billing-git",
		Provider:      models.GitProviderGeneric,
		RepoURL:       smartSrv.URL + "/billing.git",
		DefaultBranch: "master",
		AuthType:      models.GitAuthNone,
		SyncMode:      models.GitSyncManual,
		AutoCompile:   true,
	}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("创建 GENERIC GitRepo 失败: %v", err)
	}
	bundle := models.GitProtoBundle{
		GitRepoID:        repo.ID,
		BundleName:       "billing-root",
		TargetSchemaName: "memfs-billing-svc",
		Category:         models.ProtoCategoryBusiness,
		SubdirPath:       ".",
		AutoCompile:      true,
		AutoActivate:     true,
		LintOnSync:       false,
	}
	if err := db.Create(&bundle).Error; err != nil {
		t.Fatalf("创建 Bundle 失败: %v", err)
	}

	svc := NewSyncService(db, reg, nil)

	// 先验证 memfs go-git 客户端在未放行 127.0.0.1 时也被 SafeDialer 拒绝
	if _, err := svc.SyncRepo(context.Background(), repo.ID, SyncOptions{OperatorID: 1}); err == nil {
		t.Fatalf("未放行 127.0.0.1/32 时，memfs go-git 也必须触发 SSRF 防护拒绝")
	}

	if err := db.Where("setting_key = ?", "outbound_trusted_cidrs").
		Assign(models.TraefikSetting{
			SettingKey: "outbound_trusted_cidrs",
			SettingVal: "127.0.0.1/32",
		}).
		FirstOrCreate(&models.TraefikSetting{}).Error; err != nil {
		t.Fatalf("更新 outbound_trusted_cidrs 失败: %v", err)
	}

	res, err := svc.SyncRepo(context.Background(), repo.ID, SyncOptions{OperatorID: 1})
	if err != nil {
		t.Fatalf("memfs SyncRepo 失败: %v", err)
	}
	if res.CommitSHA != commitHash.String() || len(res.BundleResults) != 1 || res.BundleResults[0].AddedFiles != 1 {
		t.Fatalf("memfs SyncRepo 结果不符合预期: %+v", res)
	}

	// 验证 POSIX 路径穿越清洗防护
	if _, _, err := sliceBundleFiles(&RemoteCommitSnapshot{
		CommitSHA: "abc",
		Files: []RemoteProtoFile{{
			RepoFilePath: "../../etc/passwd.proto",
			FilePath:     "../../etc/passwd.proto",
		}},
	}, models.GitProtoBundle{SubdirPath: "."}); err == nil {
		t.Fatalf("预期路径穿越文件 ../../etc/passwd.proto 被拒绝")
	}

	// 验证超过 16MB 总限额被拒绝
	oversized := make([]byte, decoder.MaxDescriptorFileSize+1)
	if _, _, err := sliceBundleFiles(&RemoteCommitSnapshot{
		CommitSHA: "abc",
		Files: []RemoteProtoFile{{
			RepoFilePath: "big.proto",
			FilePath:     "big.proto",
			Content:      oversized,
			Size:         len(oversized),
		}},
	}, models.GitProtoBundle{SubdirPath: "."}); err == nil {
		t.Fatalf("预期超过 16MB 的 Bundle 被拒绝")
	}
}
