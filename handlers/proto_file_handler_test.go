package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"code-traefik/handlers"
	"code-traefik/internal/server/gitclient"
	"code-traefik/models"

	"gorm.io/datatypes"
	"gorm.io/gorm/clause"
)

func TestProtoFiles_ReadOnlyVersionsDiff_ZeroLockColumns(t *testing.T) {
	t.Setenv("TRAEFIK_CRYPTO_KEY", "test-traefik-crypto-key-m25-32bytes!")
	r, db := setupProtoDocRouterAndDB(t)

	if err := models.EnsureBuiltinCheckRules(db); err != nil {
		t.Fatalf("EnsureBuiltinCheckRules 失败: %v", err)
	}
	if err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "setting_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"setting_val"}),
	}).Create(&models.TraefikSetting{
		SettingKey: "outbound_trusted_cidrs",
		SettingVal: "127.0.0.0/8",
	}).Error; err != nil {
		t.Fatalf("写入 outbound_trusted_cidrs 失败: %v", err)
	}

	// 1. 强校验 traefik_proto_files 绝对禁止存在任何写锁/悲观锁字段
	colTypes, err := db.Migrator().ColumnTypes(&models.ProtoFile{})
	if err != nil {
		t.Fatalf("获取 traefik_proto_files 列信息失败: %v", err)
	}
	for _, ct := range colTypes {
		name := strings.ToLower(ct.Name())
		if strings.Contains(name, "lock") {
			t.Fatalf("traefik_proto_files 严禁存在悲观锁列，发现禁止列: %s", name)
		}
	}

	// 2. 启动 Mock GitLab Server 并通过 SyncService 执行两轮 Commit 同步（覆盖多版本、软删除、静态违规、符号锚点）
	mockGit := newHandlerMockGitLab("glpat-pfile-token")
	mockGit.setBranch("main", &handlerMockCommit{
		SHA:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Message: "feat: initial order v1",
		Author:  "Bob",
		Email:   "bob@example.com",
		Files: map[string]string{
			"order/order.proto": `syntax = "proto3";
package dophi.order.v1;

// 创建请求
message CreateOrderReq {
  string order_no = 1;
}

// 创建响应
message CreateOrderRsp {
  int32 code = 1;
}

// 订单服务
service OrderService {
  // 创建订单
  rpc CreateOrder (CreateOrderReq) returns (CreateOrderRsp);
}
`,
			"order/legacy.proto": `syntax = "proto3";
package dophi.order.v1;

// 即将废弃的消息
message LegacyInfo {
  string tag = 1;
}
`,
		},
	})
	gitSrv := httptest.NewServer(mockGit)
	defer gitSrv.Close()

	repo := &models.GitRepo{
		Name:          "pfile-test-repo",
		Provider:      models.GitProviderGitLab,
		RepoURL:       gitSrv.URL + "/dophi/order.git",
		APIBaseURL:    gitSrv.URL,
		DefaultBranch: "main",
		AuthType:      models.GitAuthToken,
		AutoCompile:   true,
		AutoActivate:  true,
		LintOnSync:    true,
	}
	if err := repo.SetCredentials("glpat-pfile-token"); err != nil {
		t.Fatalf("SetCredentials 失败: %v", err)
	}
	if err := db.Create(repo).Error; err != nil {
		t.Fatalf("创建 GitRepo 失败: %v", err)
	}
	bundle := &models.GitProtoBundle{
		GitRepoID:        repo.ID,
		BundleName:       "order-bundle",
		TargetSchemaName: "order-git-schema",
		Category:         models.ProtoCategoryBusiness,
		SubdirPath:       "order",
		IncludeDirsJSON:  datatypes.JSON([]byte(`["."]`)),
		FileGlobsJSON:    datatypes.JSON([]byte(`["*.proto"]`)),
		AutoCompile:      true,
		AutoActivate:     false,
		LintOnSync:       true,
	}
	if err := db.Create(bundle).Error; err != nil {
		t.Fatalf("创建 GitProtoBundle 失败: %v", err)
	}

	decReg, decErr := handlers.GetDecoderRegistry()
	if decErr != nil {
		t.Fatalf("GetDecoderRegistry 失败: %v", decErr)
	}
	syncSvc := gitclient.NewSyncService(db, decReg, nil)
	if _, err := syncSvc.SyncRepo(context.Background(), repo.ID, gitclient.SyncOptions{Branch: "main", ForceCompile: true}); err != nil {
		t.Fatalf("第 1 轮 SyncRepo 失败: %v", err)
	}

	// Commit 2：正式发布前移除废弃 legacy.proto（验证软删除）并新增未写注释的 CancelOrder，开启 AutoActivate 发布生效
	if err := db.Model(bundle).Update("auto_activate", true).Error; err != nil {
		t.Fatalf("更新 bundle auto_activate 失败: %v", err)
	}
	mockGit.setBranch("main", &handlerMockCommit{
		SHA:     "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Message: "feat: add CancelOrder & remove legacy.proto",
		Author:  "Carol",
		Email:   "carol@example.com",
		Files: map[string]string{
			"order/order.proto": `syntax = "proto3";
package dophi.order.v1;

// 创建请求
message CreateOrderReq {
  string order_no = 1;
}

// 创建响应
message CreateOrderRsp {
  int32 code = 1;
}

// 订单服务
service OrderService {
  // 创建订单
  rpc CreateOrder (CreateOrderReq) returns (CreateOrderRsp);
  rpc CancelOrder (CreateOrderReq) returns (CreateOrderRsp);
}
`,
		},
	})
	if _, err := syncSvc.SyncRepo(context.Background(), repo.ID, gitclient.SyncOptions{Branch: "main", ForceCompile: true}); err != nil {
		t.Fatalf("第 2 轮 SyncRepo 失败: %v", err)
	}

	viewer := createTestUserInDB(t, db, "pfile_viewer", "viewer")
	jwtToken := generateTestJWT(t, viewer)

	// 3. 查询 GET /api/proto-files：默认排除 is_deleted=true，含分页，不含 lock 字段
	var orderFileID uint64
	{
		req := httptest.NewRequest(http.MethodGet, "/api/proto-files?git_repo_id="+strconv.FormatUint(repo.ID, 10)+"&page=1&page_size=25", nil)
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /api/proto-files 期望 200，实际 %d: %s", w.Code, w.Body.String())
		}
		raw := w.Body.String()
		if strings.Contains(raw, `"locked"`) || strings.Contains(raw, `"locked_by"`) {
			t.Fatalf("GET /api/proto-files 禁止返回锁字段: %s", raw)
		}
		var listResp struct {
			Items []struct {
				ID               uint64  `json:"id"`
				GitRepoID        *uint64 `json:"git_repo_id"`
				BundleID         *uint64 `json:"bundle_id"`
				FilePath         string  `json:"file_path"`
				LatestVersion    int     `json:"latest_version"`
				LatestCommitSHA  string  `json:"latest_commit_sha"`
				TargetSchemaName string  `json:"target_schema_name"`
				IsDeleted        bool    `json:"is_deleted"`
			} `json:"items"`
			Total    int `json:"total"`
			PageSize int `json:"page_size"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
			t.Fatalf("解析文件列表失败: %v", err)
		}
		if listResp.Total != 1 || len(listResp.Items) != 1 || listResp.PageSize != 25 {
			t.Fatalf("默认应仅返回 1 个未软删文件且 page_size=25，实际: %+v", listResp)
		}
		if listResp.Items[0].FilePath != "order.proto" || listResp.Items[0].LatestVersion != 2 ||
			listResp.Items[0].LatestCommitSHA != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" ||
			listResp.Items[0].GitRepoID == nil || *listResp.Items[0].GitRepoID != repo.ID {
			t.Fatalf("文件 DTO 元数据不符: %+v", listResp.Items[0])
		}
		orderFileID = listResp.Items[0].ID
	}

	// 验证非法 page_size=10 降级为 25 且 include_deleted=true 返回 2 条
	{
		req := httptest.NewRequest(http.MethodGet, "/api/proto-files?git_repo_id="+strconv.FormatUint(repo.ID, 10)+"&include_deleted=true&page_size=10", nil)
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("include_deleted=true 期望 200，实际 %d", w.Code)
		}
		var listResp struct {
			Total    int `json:"total"`
			PageSize int `json:"page_size"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &listResp)
		if listResp.Total != 2 || listResp.PageSize != 25 {
			t.Fatalf("期望 include_deleted=true 返回 2 条且非法 page_size 回退为 25，实际: %+v", listResp)
		}
	}

	// 4. GET /api/proto-files/:id/content 验证最新正文、Violation 叠加、SymbolAnchor 精确行号与指定 v1 历史查看
	{
		req := httptest.NewRequest(http.MethodGet, "/api/proto-files/"+strconv.FormatUint(orderFileID, 10)+"/content", nil)
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /content 期望 200，实际 %d: %s", w.Code, w.Body.String())
		}
		var contentResp struct {
			File struct {
				ID uint64 `json:"id"`
			} `json:"file"`
			Version struct {
				Version   int    `json:"version"`
				CommitSHA string `json:"commit_sha"`
				Author    string `json:"author_name"`
			} `json:"version"`
			Content       string                  `json:"content"`
			Violations    []models.CheckViolation `json:"violations"`
			SymbolAnchors []struct {
				SymbolKind     string `json:"symbol_kind"`
				SymbolFullName string `json:"symbol_full_name"`
				StartLine      int    `json:"start_line"`
			} `json:"symbol_anchors"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &contentResp); err != nil {
			t.Fatalf("解析 /content 失败: %v", err)
		}
		if contentResp.Version.Version != 2 || contentResp.Version.Author != "Carol" ||
			!strings.Contains(contentResp.Content, "CancelOrder") {
			t.Fatalf("最新版内容或提交作者异常: %+v", contentResp.Version)
		}
		if len(contentResp.Violations) == 0 {
			t.Fatalf("期望 CancelOrder 触发的 CheckViolation 叠加返回，实际为空")
		}
		if len(contentResp.SymbolAnchors) == 0 {
			t.Fatalf("期望返回 symbol_anchors 供 #L 行号锚点跳转，实际为空")
		}
	}

	// 验证指定历史版本 ?version=1 返回旧源码（不包含 CancelOrder）
	{
		req := httptest.NewRequest(http.MethodGet, "/api/proto-files/"+strconv.FormatUint(orderFileID, 10)+"/content?version=1", nil)
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /content?version=1 期望 200，实际 %d", w.Code)
		}
		var v1Resp struct {
			Version struct {
				Version   int    `json:"version"`
				CommitSHA string `json:"commit_sha"`
			} `json:"version"`
			Content string `json:"content"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &v1Resp)
		if v1Resp.Version.Version != 1 || strings.Contains(v1Resp.Content, "CancelOrder") {
			t.Fatalf("version=1 内容应当为旧快照: %+v", v1Resp)
		}
	}

	// 5. GET /api/proto-files/:id/versions 按 version DESC 返回摘要，不含大文本 content
	{
		req := httptest.NewRequest(http.MethodGet, "/api/proto-files/"+strconv.FormatUint(orderFileID, 10)+"/versions", nil)
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /versions 期望 200，实际 %d", w.Code)
		}
		if strings.Contains(w.Body.String(), `"content"`) || strings.Contains(w.Body.String(), "CreateOrderReq") {
			t.Fatalf("/versions 列表绝对不能返回大文本正文: %s", w.Body.String())
		}
		var verResp struct {
			Items []struct {
				Version   int    `json:"version"`
				CommitSHA string `json:"commit_sha"`
				Author    string `json:"author_name"`
			} `json:"items"`
			Total int `json:"total"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &verResp)
		if verResp.Total != 2 || len(verResp.Items) != 2 || verResp.Items[0].Version != 2 || verResp.Items[1].Version != 1 {
			t.Fatalf("版本历史列表排序或数量错误: %+v", verResp)
		}
	}

	// 6. GET /api/proto-files/:id/diff?from_version=prev&to_version=latest 验证 Unified Diff 与错误码
	{
		req := httptest.NewRequest(http.MethodGet, "/api/proto-files/"+strconv.FormatUint(orderFileID, 10)+"/diff?from_version=prev&to_version=latest", nil)
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /diff 期望 200，实际 %d: %s", w.Code, w.Body.String())
		}
		var diffResp struct {
			UnifiedDiff  string `json:"unified_diff"`
			AddedLines   int    `json:"added_lines"`
			DeletedLines int    `json:"deleted_lines"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &diffResp)
		if diffResp.AddedLines <= 0 || !strings.Contains(diffResp.UnifiedDiff, "+  rpc CancelOrder") {
			t.Fatalf("Unified Diff 内容异常: added=%d diff=%s", diffResp.AddedLines, diffResp.UnifiedDiff)
		}
	}

	{
		req := httptest.NewRequest(http.MethodGet, "/api/proto-files/"+strconv.FormatUint(orderFileID, 10)+"/diff?from_version=bad", nil)
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("非法 from_version 期望 400，实际 %d", w.Code)
		}
	}

	{
		req := httptest.NewRequest(http.MethodGet, "/api/proto-files/"+strconv.FormatUint(orderFileID, 10)+"/diff?from_version=1&to_version=99", nil)
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("不存在的 to_version 期望 404，实际 %d", w.Code)
		}
	}

	// 7. M23 活体文档详情需关联真实 source_file_id，支持 ?tab=sources&file_id=<id>#L<line> 无损跳链
	{
		req := httptest.NewRequest(http.MethodGet, "/api/proto-docs/services/dophi.order.v1.OrderService", nil)
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /api/proto-docs/services/... 期望 200，实际 %d: %s", w.Code, w.Body.String())
		}
		var svcDoc struct {
			SourceFileID *uint64 `json:"source_file_id"`
			StartLine    int     `json:"start_line"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &svcDoc)
		if svcDoc.SourceFileID == nil || *svcDoc.SourceFileID != orderFileID || svcDoc.StartLine <= 0 {
			t.Fatalf("文档详情应当返回精确的 source_file_id=%d 与 start_line>0，实际: %+v", orderFileID, svcDoc)
		}
	}
}
