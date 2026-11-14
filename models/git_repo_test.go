package models

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-traefik/config"
	"gorm.io/datatypes"
)

func TestGitRepoBundleProtoFileAndVersionMigration_ConstraintsAndEncryption(t *testing.T) {
	if os.Getenv("TEST_DB_DSN") == "" {
		t.Skip("TEST_DB_DSN 未配置，跳过数据库隔离测试")
	}

	t.Setenv("TRAEFIK_CRYPTO_KEY", "test-traefik-crypto-key-m25-32bytes!")

	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}

	// 1. 校验 4 张新实体表存在及 traefik_ 前缀隔离
	for _, m := range []tabler{
		GitRepo{},
		GitProtoBundle{},
		ProtoFile{},
		ProtoFileVersion{},
	} {
		tbl := m.TableName()
		if !strings.HasPrefix(tbl, config.TablePrefix) {
			t.Fatalf("模型表名 %s 未以 %s 开头", tbl, config.TablePrefix)
		}
		if !db.Migrator().HasTable(tbl) {
			t.Fatalf("数据库中缺少已迁移表 %s", tbl)
		}
	}

	// 2. 强制审计红线：traefik_proto_files 与 Go Struct 严格禁止任何悲观锁字段
	pfType := reflect.TypeOf(ProtoFile{})
	forbiddenCols := []string{"locked", "locked_by", "locked_at", "file_lock", "lock_owner"}
	for i := 0; i < pfType.NumField(); i++ {
		fName := strings.ToLower(pfType.Field(i).Name)
		tag := strings.ToLower(string(pfType.Field(i).Tag))
		for _, fc := range forbiddenCols {
			if strings.Contains(fName, fc) || strings.Contains(tag, fc) {
				t.Fatalf("ProtoFile Struct 违反无写锁红线，包含禁用字段/Tag: %s (%s)", pfType.Field(i).Name, fc)
			}
		}
	}
	var colNames []string
	if err := db.Raw(`
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'traefik_proto_files'
	`).Scan(&colNames).Error; err != nil {
		t.Fatalf("查询 information_schema 失败: %v", err)
	}
	for _, col := range colNames {
		lc := strings.ToLower(col)
		for _, fc := range forbiddenCols {
			if strings.Contains(lc, fc) {
				t.Fatalf("数据库表 traefik_proto_files 存在禁止锁字段: %s", col)
			}
		}
	}

	// 3. 校验 CredentialsEnc / WebhookSecretEnc AES-256-GCM 加密存储与 Redacted DTO 脱敏
	repo := GitRepo{
		Name:          "monorepo-order-billing",
		Provider:      GitProviderGitLab,
		RepoURL:       "http://127.0.0.1:8080/group/monorepo.git",
		APIBaseURL:    "http://127.0.0.1:8080/api/v4",
		DefaultBranch: "main",
		AuthType:      GitAuthToken,
		SyncMode:      GitSyncManual,
	}
	plainToken := "glpat-supersecret-token-9876"
	plainWebhook := "whsec-webhook-secret-5432"
	if err := repo.SetCredentials(plainToken); err != nil {
		t.Fatalf("SetCredentials 加密失败: %v", err)
	}
	if err := repo.SetWebhookSecret(plainWebhook); err != nil {
		t.Fatalf("SetWebhookSecret 加密失败: %v", err)
	}
	if repo.CredentialsEnc == "" || strings.Contains(repo.CredentialsEnc, plainToken) {
		t.Fatalf("CredentialsEnc 未正确 AES 加密: %q", repo.CredentialsEnc)
	}
	if repo.WebhookSecretEnc == "" || strings.Contains(repo.WebhookSecretEnc, plainWebhook) {
		t.Fatalf("WebhookSecretEnc 未正确 AES 加密: %q", repo.WebhookSecretEnc)
	}
	decToken, err := repo.DecryptCredentials()
	if err != nil || decToken != plainToken {
		t.Fatalf("DecryptCredentials 还原失败: dec=%q, err=%v", decToken, err)
	}
	decHook, err := repo.DecryptWebhookSecret()
	if err != nil || decHook != plainWebhook {
		t.Fatalf("DecryptWebhookSecret 还原失败: dec=%q, err=%v", decHook, err)
	}

	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("创建 GitRepo 失败: %v", err)
	}

	dto := repo.ToRedactedDTO()
	rawJSONBytes, _ := json.Marshal(dto)
	rawJSON := string(rawJSONBytes)
	if !dto.HasCredentials || !dto.HasWebhookSecret || dto.CredentialHint == "" {
		t.Fatalf("脱敏 DTO 状态标志错误: %+v", dto)
	}
	if strings.Contains(rawJSON, plainToken) || strings.Contains(rawJSON, plainWebhook) ||
		strings.Contains(rawJSON, "supersecret") || strings.Contains(rawJSON, "credentials_enc") ||
		strings.Contains(rawJSON, "webhook_secret_enc") {
		t.Fatalf("Redacted DTO 泄漏了明文凭证或密文字段: %s", rawJSON)
	}

	// 4. 创建 1:N Monorepo Bundle 并验证 target_schema_name 全局唯一约束
	b1 := GitProtoBundle{
		GitRepoID:        repo.ID,
		BundleName:       "order-v1",
		TargetSchemaName: "order-service-schema",
		Category:         ProtoCategoryBusiness,
		SubdirPath:       "services/order/proto",
		IncludeDirsJSON:  datatypes.JSON([]byte(`["."]`)),
		EntryFilesJSON:   datatypes.JSON([]byte(`[]`)),
		FileGlobsJSON:    datatypes.JSON([]byte(`["*.proto"]`)),
		ExcludeGlobsJSON: datatypes.JSON([]byte(`[]`)),
	}
	if err := db.Create(&b1).Error; err != nil {
		t.Fatalf("创建 Bundle b1 失败: %v", err)
	}

	repo2 := GitRepo{
		Name:          "another-repo",
		Provider:      GitProviderGitHub,
		RepoURL:       "http://127.0.0.1:8080/group/another.git",
		DefaultBranch: "main",
		AuthType:      GitAuthNone,
		SyncMode:      GitSyncManual,
	}
	if err := db.Create(&repo2).Error; err != nil {
		t.Fatalf("创建 repo2 失败: %v", err)
	}
	dupTarget := GitProtoBundle{
		GitRepoID:        repo2.ID,
		BundleName:       "order-dup",
		TargetSchemaName: "order-service-schema",
		Category:         ProtoCategoryBusiness,
		SubdirPath:       "proto",
	}
	if err := db.Create(&dupTarget).Error; err == nil {
		t.Fatalf("违反契约：跨 GitRepo 插入重复 target_schema_name 应被 uk_traefik_git_bundle_target 拦截")
	}

	// 5. 校验 ProtoFile 部分唯一索引 uk_traefik_pfile_bundle_path (bundle_id, file_path) WHERE is_deleted = false
	pf := ProtoFile{
		GitRepoID:        repo.ID,
		BundleID:         b1.ID,
		TargetSchemaName: b1.TargetSchemaName,
		Category:         b1.Category,
		FilePath:         "order/v1/order.proto",
		RepoFilePath:     "services/order/proto/order/v1/order.proto",
		PackageName:      "order.v1",
		ContentSHA256:    "abc123",
		LineCount:        10,
		LatestVersionNum: 1,
		LatestCommitSHA:  "1111111111111111111111111111111111111111",
		IsDeleted:        false,
	}
	if err := db.Create(&pf).Error; err != nil {
		t.Fatalf("创建 ProtoFile 失败: %v", err)
	}

	ver := ProtoFileVersion{
		ProtoFileID:   pf.ID,
		Version:       1,
		SourceType:    ProtoSourceGitSync,
		CommitSHA:     pf.LatestCommitSHA,
		CommitMessage: "init order proto",
		AuthorName:    "alice",
		ContentText:   "syntax = \"proto3\";\npackage order.v1;\n",
		ContentSHA256: "abc123",
		LineCount:     2,
		CommittedAt:   time.Now().UTC(),
	}
	if err := db.Create(&ver).Error; err != nil {
		t.Fatalf("创建 ProtoFileVersion 失败: %v", err)
	}
	if err := db.Model(&pf).Update("latest_version_id", ver.ID).Error; err != nil {
		t.Fatalf("更新 LatestVersionID 失败: %v", err)
	}

	// 活跃同名同路径必须冲突
	pfDupActive := ProtoFile{
		GitRepoID:        repo.ID,
		BundleID:         b1.ID,
		TargetSchemaName: b1.TargetSchemaName,
		Category:         b1.Category,
		FilePath:         "order/v1/order.proto",
		IsDeleted:        false,
	}
	if err := db.Create(&pfDupActive).Error; err == nil {
		t.Fatalf("违反契约：活跃状态下的 (bundle_id, file_path) 不得重复")
	}

	// 软删除旧文件后，允许重新插入同名活跃文件（partial unique index）
	if err := db.Model(&pf).Update("is_deleted", true).Error; err != nil {
		t.Fatalf("软删除 ProtoFile 失败: %v", err)
	}
	pfReadded := ProtoFile{
		GitRepoID:        repo.ID,
		BundleID:         b1.ID,
		TargetSchemaName: b1.TargetSchemaName,
		Category:         b1.Category,
		FilePath:         "order/v1/order.proto",
		IsDeleted:        false,
	}
	if err := db.Create(&pfReadded).Error; err != nil {
		t.Fatalf("软删除旧记录后重新插入同路径文件应当成功，却失败: %v", err)
	}

	// 6. 校验 ProtoSchema.SourceBundleID ON DELETE SET NULL
	sc := ProtoSchema{
		Name:            b1.TargetSchemaName,
		Category:        ProtoCategoryBusiness,
		Version:         1,
		VersionTag:      "git-1111111",
		SourceType:      ProtoSourceGitSync,
		GitRepoID:       &repo.ID,
		SourceBundleID:  &b1.ID,
		SourceCommitSHA: pf.LatestCommitSHA,
		FileName:        "order-service-schema.compiled.pb",
		FileSHA256:      "deadbeef",
		FileSize:        4,
		DescriptorBytes: []byte{0x0a, 0x00},
		IsActive:        false,
	}
	if err := db.Create(&sc).Error; err != nil {
		t.Fatalf("创建关联 Bundle 的 ProtoSchema 失败: %v", err)
	}
	if err := db.Delete(&b1).Error; err != nil {
		t.Fatalf("删除 GitProtoBundle 失败: %v", err)
	}
	var refreshed ProtoSchema
	if err := db.First(&refreshed, sc.ID).Error; err != nil {
		t.Fatalf("删除 Bundle 后 ProtoSchema 应被保留而非级联删除: %v", err)
	}
	if refreshed.SourceBundleID != nil {
		t.Fatalf("删除 GitProtoBundle 后 SourceBundleID 应通过 ON DELETE SET NULL 置空，实际=%v", *refreshed.SourceBundleID)
	}
}
