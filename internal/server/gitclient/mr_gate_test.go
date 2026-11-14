package gitclient

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
	"code-traefik/internal/server/decoder"
	"code-traefik/models"

	"gorm.io/datatypes"
)

func TestMRVirtualTree_BaselineMerge_DeleteAndRename(t *testing.T) {
	if os.Getenv("TEST_DB_DSN") == "" {
		t.Skip("TEST_DB_DSN 未配置，跳过数据库隔离测试")
	}
	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}

	repo := models.GitRepo{
		Name:          "vt-repo",
		Provider:      models.GitProviderGitLab,
		RepoURL:       "http://127.0.0.1:8080/org/vt-repo.git",
		DefaultBranch: "main",
		AuthType:      models.GitAuthNone,
		SyncMode:      models.GitSyncWebhook,
	}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("创建 GitRepo 失败: %v", err)
	}
	bundle := models.GitProtoBundle{
		GitRepoID:        repo.ID,
		BundleName:       "order-bundle",
		TargetSchemaName: "order-vt-schema",
		Category:         models.ProtoCategoryBusiness,
		SubdirPath:       "services/order/proto",
		IncludeDirsJSON:  datatypes.JSON([]byte(`["."]`)),
	}
	if err := db.Create(&bundle).Error; err != nil {
		t.Fatalf("创建 Bundle 失败: %v", err)
	}

	// 基线入库 2 个活跃文件：common_types.proto 与 order_service.proto (import "common_types.proto")
	seedFile := func(relPath, content string) {
		t.Helper()
		pf := models.ProtoFile{
			GitRepoID:        repo.ID,
			BundleID:         bundle.ID,
			TargetSchemaName: bundle.TargetSchemaName,
			Category:         bundle.Category,
			FilePath:         relPath,
			RepoFilePath:     "services/order/proto/" + relPath,
			PackageName:      "order.v1",
			ContentSHA256:    "sha-" + relPath,
			LineCount:        5,
			LatestVersionNum: 1,
			LatestCommitSHA:  "base111",
			IsDeleted:        false,
		}
		if err := db.Create(&pf).Error; err != nil {
			t.Fatalf("创建基线 ProtoFile 失败: %v", err)
		}
		ver := models.ProtoFileVersion{
			ProtoFileID:   pf.ID,
			Version:       1,
			SourceType:    models.ProtoSourceGitSync,
			CommitSHA:     "base111",
			ContentText:   content,
			ContentSHA256: "sha-" + relPath,
			LineCount:     5,
			CommittedAt:   time.Now().UTC(),
		}
		if err := db.Create(&ver).Error; err != nil {
			t.Fatalf("创建基线 ProtoFileVersion 失败: %v", err)
		}
		if err := db.Model(&pf).Update("latest_version_id", ver.ID).Error; err != nil {
			t.Fatalf("更新 latest_version_id 失败: %v", err)
		}
	}

	seedFile("common_types.proto", `syntax = "proto3";
package order.v1;
message Money { int64 cents = 1; }
`)
	seedFile("order_service.proto", `syntax = "proto3";
package order.v1;
import "common_types.proto";
message CreateOrderReq { Money total = 1; }
`)

	// 场景 A：MR Patch 执行 RENAMED (common_types.proto -> money_types.proto) 但 order_service.proto 仍 import "common_types.proto"
	patchRename := []MRPatchFile{
		{
			ChangeType: MRChangeRenamed,
			OldPath:    "services/order/proto/common_types.proto",
			NewPath:    "services/order/proto/money_types.proto",
			Content: `syntax = "proto3";
package order.v1;
message Money { int64 cents = 1; }
`,
		},
	}
	vTreeRename, changedRename, err := BuildMRVirtualTree(db, &bundle, patchRename)
	if err != nil {
		t.Fatalf("BuildMRVirtualTree (RENAMED) 失败: %v", err)
	}
	if _, existsOld := vTreeRename["common_types.proto"]; existsOld {
		t.Fatalf("RENAMED 后旧路径 common_types.proto 必须从虚拟树中移除")
	}
	if _, existsNew := vTreeRename["money_types.proto"]; !existsNew {
		t.Fatalf("RENAMED 后新路径 money_types.proto 必须写入虚拟树")
	}
	if len(changedRename) == 0 {
		t.Fatalf("changedFiles 不应为空")
	}

	// 场景 B：MR Patch 执行 DELETED (删除被导入的 common_types.proto)，验证 protocompile 立即报错拦截悬挂 import
	patchDelete := []MRPatchFile{
		{
			ChangeType: MRChangeDeleted,
			OldPath:    "services/order/proto/common_types.proto",
			NewPath:    "services/order/proto/common_types.proto",
		},
	}
	vTreeDelete, _, err := BuildMRVirtualTree(db, &bundle, patchDelete)
	if err != nil {
		t.Fatalf("BuildMRVirtualTree (DELETED) 失败: %v", err)
	}
	if _, exists := vTreeDelete["common_types.proto"]; exists {
		t.Fatalf("DELETED 后 common_types.proto 必须从虚拟树中物理移除")
	}
	_, compErr := decoder.CompileProtoBundle(context.Background(), nil, decoder.CompileProtoBundleInput{
		Name:             bundle.TargetSchemaName,
		Category:         bundle.Category,
		Files:            vTreeDelete,
		ProtoIncludeDirs: []string{"."},
	})
	if compErr == nil {
		t.Fatalf("删除被 order_service.proto 导入的 common_types.proto 后，protocompile 应当编译失败阻断 MR！")
	}
}

func TestMRRunner_FourInOneGate_BotCommentInPlaceUpsert_AndMergeAutoCompile(t *testing.T) {
	if os.Getenv("TEST_DB_DSN") == "" {
		t.Skip("TEST_DB_DSN 未配置，跳过数据库隔离测试")
	}
	t.Setenv("TRAEFIK_CRYPTO_KEY", "m26-mr-runner-crypto-key-32bytes")

	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}
	if err := db.Model(&models.TraefikSetting{}).Where("setting_key = ?", "outbound_trusted_cidrs").
		Update("setting_val", "127.0.0.1/32").Error; err != nil {
		t.Fatalf("设置 outbound_trusted_cidrs 失败: %v", err)
	}

	// 启动绑定 127.0.0.1 的 Mock GitLab API Server，记录评论 POST (新建) 与 PUT (原位更新) 次数
	var (
		mu          sync.Mutex
		postNotes   []string
		putNotes    map[string]string
		commitState string
	)
	putNotes = make(map[string]string)

	mockGitLab := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/merge_requests/15/notes"):
			body, _ := io.ReadAll(r.Body)
			var req struct {
				Body string `json:"body"`
			}
			_ = json.Unmarshal(body, &req)
			mu.Lock()
			postNotes = append(postNotes, req.Body)
			noteID := 9001
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id": 9001, "body": "created"}`))
			_ = noteID
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/merge_requests/15/notes/9001"):
			body, _ := io.ReadAll(r.Body)
			var req struct {
				Body string `json:"body"`
			}
			_ = json.Unmarshal(body, &req)
			mu.Lock()
			putNotes["9001"] = req.Body
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id": 9001, "body": "updated"}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/statuses/"):
			body, _ := io.ReadAll(r.Body)
			var req struct {
				State string `json:"state"`
			}
			_ = json.Unmarshal(body, &req)
			mu.Lock()
			commitState = req.State
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	mockGitLab.Start()
	defer mockGitLab.Close()

	repo := models.GitRepo{
		Name:          "payment-monorepo",
		Provider:      models.GitProviderGitLab,
		RepoURL:       mockGitLab.URL + "/fin/payment-monorepo.git",
		APIBaseURL:    mockGitLab.URL + "/api/v4",
		DefaultBranch: "main",
		AuthType:      models.GitAuthToken,
		SyncMode:      models.GitSyncWebhook,
		AutoCompile:   true,
	}
	if err := repo.SetCredentials("glpat-test-token"); err != nil {
		t.Fatalf("SetCredentials 失败: %v", err)
	}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("创建 GitRepo 失败: %v", err)
	}

	bundle := models.GitProtoBundle{
		GitRepoID:        repo.ID,
		BundleName:       "pay-core",
		TargetSchemaName: "pay-core-schema",
		Category:         models.ProtoCategoryBusiness,
		SubdirPath:       "proto",
		IncludeDirsJSON:  datatypes.JSON([]byte(`["."]`)),
		AutoCompile:      true,
		AutoActivate:     false,
		MRAutoPublish:    models.MRAutoPublishDraftOnly,
		LintOnSync:       true,
	}
	if err := db.Create(&bundle).Error; err != nil {
		t.Fatalf("创建 Bundle 失败: %v", err)
	}

	reg, err := decoder.NewRegistry(db)
	if err != nil {
		t.Fatalf("NewRegistry 失败: %v", err)
	}
	// 先编译并激活 v1 基线契约：含 PayService.CreatePayment 与 PaymentReq { int64 order_id = 1; string currency = 2; }
	baselineProto := `syntax = "proto3";
package fin.pay.v1;

// 支付核心服务
service PayService {
  // 创建支付单
  rpc CreatePayment (PaymentReq) returns (PaymentRsp);
}

// 支付请求
message PaymentReq {
  int64 order_id = 1;
  string currency = 2;
}

// 支付响应
message PaymentRsp {
  int64 pay_id = 1;
}
`
	baseSchema, _, _, err := reg.CompileAndSaveSchema(context.Background(), decoder.CompileProtoBundleInput{
		Name:             bundle.TargetSchemaName,
		Category:         bundle.Category,
		VersionTag:       "v1.0.0",
		Files:            map[string]string{"pay/v1/pay.proto": baselineProto},
		ProtoIncludeDirs: []string{"."},
		AutoActivate:     true,
		SourceType:       models.ProtoSourceGitSync,
		GitRepoID:        &repo.ID,
		SourceBundleID:   &bundle.ID,
		SourceCommitSHA:  "base0001",
	})
	if err != nil {
		t.Fatalf("激活基线 Schema 失败: %v", err)
	}
	if err := db.Model(&bundle).Update("last_schema_id", baseSchema.ID).Error; err != nil {
		t.Fatalf("更新 bundle.last_schema_id 失败: %v", err)
	}

	runner := NewMRGateRunner(db, reg, nil)

	// 第一次 MR 检查：提交含破坏性变更（将 order_id = 1 的类型从 int64 改为 string）的补丁
	breakingProto := `syntax = "proto3";
package fin.pay.v1;

// 支付核心服务
service PayService {
  // 创建支付单
  rpc CreatePayment (PaymentReq) returns (PaymentRsp);
}

// 支付请求
message PaymentReq {
  string order_id = 1;
  string currency = 2;
}

// 支付响应
message PaymentRsp {
  int64 pay_id = 1;
}
`
	run1, err := runner.CreateAndExecuteBundleRun(context.Background(), MRExecuteInput{
		Repo:           &repo,
		Bundle:         &bundle,
		MRIID:          15,
		EventPhase:     models.MREventPhaseGate,
		MRTitle:        "feat: update payment req",
		MRURL:          mockGitLab.URL + "/fin/payment-monorepo/-/merge_requests/15",
		SourceBranch:   "feat/pay-v2",
		TargetBranch:   "main",
		HeadCommitSHA:  "commit1111111111",
		TargetHeadSHA:  "base0001",
		AuthorUsername: "dev_pay",
		AuthorEmail:    "dev_pay@code-bench.local",
		PatchFiles: []MRPatchFile{
			{
				ChangeType: MRChangeModified,
				NewPath:    "proto/pay/v1/pay.proto",
				Content:    breakingProto,
			},
		},
	})
	if err != nil {
		t.Fatalf("第一次执行 MR Gate 失败: %v", err)
	}
	if run1.Status != models.MRCheckStatusFailed {
		t.Fatalf("包含破坏性变更的 MR 门禁期望 Status=FAILED，实际 %s", run1.Status)
	}
	if !run1.CompilePassed {
		t.Fatalf("语法合法仅含破坏性变更时 CompilePassed 应为 true，以便区分编译错误")
	}
	if run1.BreakingChangeCount == 0 {
		t.Fatalf("BreakingChangeCount 期望 > 0，实际 0")
	}
	if run1.RemoteCommentID != "9001" {
		t.Fatalf("首次评论应记录 RemoteCommentID='9001'，实际 %q", run1.RemoteCommentID)
	}

	// 第二次 MR 检查（同 MR #15 推送修复后的新 Commit）：验证复用 RemoteCommentID=9001 执行 PUT 原位更新，绝不重复 POST 新评论！
	compatibleProto := `syntax = "proto3";
package fin.pay.v1;

// 支付核心服务
service PayService {
  // 创建支付单
  rpc CreatePayment (PaymentReq) returns (PaymentRsp);
}

// 支付请求
message PaymentReq {
  int64 order_id = 1;
  string currency = 2;
  string remark = 3;
}

// 支付响应
message PaymentRsp {
  int64 pay_id = 1;
}
`
	run2, err := runner.CreateAndExecuteBundleRun(context.Background(), MRExecuteInput{
		Repo:           &repo,
		Bundle:         &bundle,
		MRIID:          15,
		EventPhase:     models.MREventPhaseGate,
		MRTitle:        "feat: update payment req",
		MRURL:          mockGitLab.URL + "/fin/payment-monorepo/-/merge_requests/15",
		SourceBranch:   "feat/pay-v2",
		TargetBranch:   "main",
		HeadCommitSHA:  "commit2222222222",
		TargetHeadSHA:  "base0001",
		AuthorUsername: "dev_pay",
		AuthorEmail:    "dev_pay@code-bench.local",
		PatchFiles: []MRPatchFile{
			{
				ChangeType: MRChangeModified,
				NewPath:    "proto/pay/v1/pay.proto",
				Content:    compatibleProto,
			},
		},
	})
	if err != nil {
		t.Fatalf("第二次执行 MR Gate 失败: %v", err)
	}
	if run2.Status != models.MRCheckStatusPassed && run2.Status != models.MRCheckStatusWarn {
		t.Fatalf("修复破坏性变更后期望 Status=PASSED/WARN，实际 %s (compileErr=%s)", run2.Status, run2.CompileErrorText)
	}
	if run2.RemoteCommentID != "9001" {
		t.Fatalf("第二次运行应继承并复用 RemoteCommentID='9001'，实际 %q", run2.RemoteCommentID)
	}

	mu.Lock()
	postCnt := len(postNotes)
	putBody := putNotes["9001"]
	lastState := commitState
	mu.Unlock()
	if postCnt != 1 {
		t.Fatalf("Bot 评论原位更新机制要求整个 MR 生命周期仅 POST 1 次，实际 POST 了 %d 次", postCnt)
	}
	if putBody == "" {
		t.Fatalf("期望对已有 Note #9001 执行 PUT 原位更新，但未收到 PUT 请求")
	}
	if lastState != "success" {
		t.Fatalf("远端 Commit Status 期望更新为 success，实际 %q", lastState)
	}

	// 第三次：MR 合入主干 (event_phase = MAIN_MERGE, head_commit_sha 与 run2 相同，验证 Fast-Forward 同 SHA 不漏触发并自动入池 GIT_MR_AUTO)
	mergeRun, err := runner.CreateAndExecuteBundleRun(context.Background(), MRExecuteInput{
		Repo:           &repo,
		Bundle:         &bundle,
		MRIID:          15,
		EventPhase:     models.MREventPhaseMainMerge,
		MRTitle:        "feat: update payment req",
		MRURL:          mockGitLab.URL + "/fin/payment-monorepo/-/merge_requests/15",
		SourceBranch:   "feat/pay-v2",
		TargetBranch:   "main",
		HeadCommitSHA:  "commit2222222222", // 同 SHA Fast-Forward 合入
		TargetHeadSHA:  "base0001",
		AuthorUsername: "dev_pay",
		AuthorEmail:    "dev_pay@code-bench.local",
		PatchFiles: []MRPatchFile{
			{
				ChangeType: MRChangeModified,
				NewPath:    "proto/pay/v1/pay.proto",
				Content:    compatibleProto,
			},
		},
	})
	if err != nil {
		t.Fatalf("执行 MAIN_MERGE 自动入池失败: %v", err)
	}
	if mergeRun.EventPhase != models.MREventPhaseMainMerge {
		t.Fatalf("期望 EventPhase=MAIN_MERGE，实际 %s", mergeRun.EventPhase)
	}

	var autoSchema models.ProtoSchema
	if err := db.Where("name = ? AND source_type = ?", bundle.TargetSchemaName, models.ProtoSourceGitMRAuto).
		Order("id DESC").First(&autoSchema).Error; err != nil {
		t.Fatalf("MAIN_MERGE 后未自动编译生成 source_type=GIT_MR_AUTO 的 ProtoSchema: %v", err)
	}
	if autoSchema.SourceBundleID == nil || *autoSchema.SourceBundleID != bundle.ID {
		t.Fatalf("GIT_MR_AUTO Schema 的 SourceBundleID 不匹配: %+v", autoSchema.SourceBundleID)
	}
	if autoSchema.SourceMRIID == nil || *autoSchema.SourceMRIID != 15 {
		t.Fatalf("GIT_MR_AUTO Schema 的 SourceMRIID 期望 15，实际 %+v", autoSchema.SourceMRIID)
	}
}

func TestVirtualTreeMerge_AppliesAddModifyRenameAndDelete_AndUpsertsBotComment(t *testing.T) {
	TestMRVirtualTree_BaselineMerge_DeleteAndRename(t)
	TestMRRunner_FourInOneGate_BotCommentInPlaceUpsert_AndMergeAutoCompile(t)
}
