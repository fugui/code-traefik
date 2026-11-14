package models

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-traefik/config"
	"gorm.io/datatypes"
)

func TestM26_All30CoreTablesPlusSettings_MRDedupAndNotifications(t *testing.T) {
	if os.Getenv("TEST_DB_DSN") == "" {
		t.Skip("TEST_DB_DSN 未配置，跳过数据库隔离测试")
	}

	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}

	// 1. 验证 30 张核心业务表 + 1 张 traefik_settings (共 31 张表) 全部带 traefik_ 前缀且已迁移
	allModels := []tabler{
		Subsystem{},        // 1
		FunctionalGroup{},  // 2
		ArchProcessGroup{}, // 3
		ArchProcess{},      // 4
		Module{},           // 5
		ServiceContract{},  // 6
		ModuleService{},    // 7
		ModuleDependency{}, // 8
		Agent{},            // 9
		Process{},          // 10
		ProtoSchema{},      // 11
		ProtoOperation{},   // 12
		RecordingSession{}, // 13
		TrafficRecord{},    // 14
		ReplayTask{},       // 15
		ReplayItem{},       // 16
		GoldenSuite{},      // 17
		GoldenSuiteItem{},  // 18
		TopologyLayout{},   // 19
		ProtoDocSymbol{},   // 20
		CheckRule{},        // 21
		VocabularyTerm{},   // 22
		CheckReport{},      // 23
		CheckViolation{},   // 24
		GitRepo{},          // 25
		GitProtoBundle{},   // 26
		ProtoFile{},        // 27
		ProtoFileVersion{}, // 28
		MRCheckRun{},       // 29 (M26 #29)
		Notification{},     // 30 (M26 #30)
		TraefikSetting{},   // +1 配置表 (31)
	}
	if len(allModels) != 31 {
		t.Fatalf("期望 30 张核心业务表 + 1 张 TraefikSetting (共 31 张表)，实际定义 %d", len(allModels))
	}
	seenTables := make(map[string]struct{}, len(allModels))
	for _, m := range allModels {
		tbl := m.TableName()
		tName := reflect.TypeOf(m).Name()
		if !strings.HasPrefix(tbl, config.TablePrefix) {
			t.Fatalf("模型 %s 表名 %s 未以 %s 开头", tName, tbl, config.TablePrefix)
		}
		if !db.Migrator().HasTable(tbl) {
			t.Fatalf("数据库中缺少已迁移表 %s (模型 %s)", tbl, tName)
		}
		seenTables[tbl] = struct{}{}
	}
	if _, ok := seenTables["traefik_mr_check_runs"]; !ok {
		t.Fatalf("缺少 traefik_mr_check_runs")
	}
	if _, ok := seenTables["traefik_notifications"]; !ok {
		t.Fatalf("缺少 traefik_notifications")
	}

	// 2. 验证 M26 默认通知/外发配置项已幂等种子化
	expectedKeys := []string{
		"code_notifier_enabled",
		"code_notifier_url",
		"outbound_trusted_cidrs",
		"smtp_host",
		"smtp_port",
		"smtp_username",
		"smtp_from",
	}
	for _, k := range expectedKeys {
		var s TraefikSetting
		if err := db.Where("setting_key = ?", k).First(&s).Error; err != nil {
			t.Fatalf("M26 默认设置项 %s 未初始化: %v", k, err)
		}
	}

	// 3. 验证 uk_traefik_mr_dedup 表达式唯一索引:
	// (git_repo_id, COALESCE(bundle_id, 0), event_phase, head_commit_sha, target_head_sha)
	repo := GitRepo{
		Name:          "mr-gate-test-repo",
		Provider:      GitProviderGitLab,
		RepoURL:       "http://127.0.0.1:8080/org/mr-repo.git",
		DefaultBranch: "main",
		AuthType:      GitAuthToken,
		SyncMode:      GitSyncWebhook,
	}
	if err := db.Create(&repo).Error; err != nil {
		t.Fatalf("创建 GitRepo 失败: %v", err)
	}
	bundle := GitProtoBundle{
		GitRepoID:        repo.ID,
		BundleName:       "order-bundle",
		TargetSchemaName: "order-mr-schema",
		Category:         ProtoCategoryBusiness,
		SubdirPath:       "order/proto",
	}
	if err := db.Create(&bundle).Error; err != nil {
		t.Fatalf("创建 GitProtoBundle 失败: %v", err)
	}

	run1 := MRCheckRun{
		GitRepoID:             repo.ID,
		BundleID:              &bundle.ID,
		MRIID:                 42,
		EventPhase:            MREventPhaseGate,
		MRTitle:               "feat: add order v2",
		MRURL:                 "http://127.0.0.1:8080/org/mr-repo/-/merge_requests/42",
		SourceBranch:          "feat/order-v2",
		TargetBranch:          "main",
		HeadCommitSHA:         "aaaa1111bbbb2222cccc3333dddd4444eeee5555",
		TargetHeadSHA:         "0000111122223333444455556666777788889999",
		AuthorUsername:        "alice",
		AuthorEmail:           "alice@example.com",
		Status:                MRCheckStatusPassed,
		CompilePassed:         true,
		ChangedProtoFilesJSON: datatypes.JSON([]byte(`["order/v1/order.proto"]`)),
		BreakingDetailsJSON:   datatypes.JSON([]byte(`[]`)),
		AffectedModulesJSON:   datatypes.JSON([]byte(`[]`)),
	}
	if err := db.Create(&run1).Error; err != nil {
		t.Fatalf("创建首条 MRCheckRun (MR_GATE) 失败: %v", err)
	}

	// (a) 相同 (git_repo_id, bundle_id, event_phase='MR_GATE', head_commit_sha, target_head_sha) 重复插入必须被 uk_traefik_mr_dedup 拦截
	dupGateRun := run1
	dupGateRun.ID = 0
	if err := db.Create(&dupGateRun).Error; err == nil {
		t.Fatalf("违反契约：相同 (repo, bundle, MR_GATE, head_sha, target_sha) 应当被 uk_traefik_mr_dedup 唯一索引拦截")
	}

	// (b) Fast-Forward 合入场景：相同 head_commit_sha，但 event_phase='MAIN_MERGE' 必须允许独立写入，绝不漏触发！
	ffMergeRun := run1
	ffMergeRun.ID = 0
	ffMergeRun.EventPhase = MREventPhaseMainMerge
	if err := db.Create(&ffMergeRun).Error; err != nil {
		t.Fatalf("Fast-Forward 同 SHA 合入阶段 (MAIN_MERGE) 应当允许独立落库，却失败: %v", err)
	}

	// (c) 目标分支基线演进后 (target_head_sha 变化) 同一源 SHA 重新检查必须允许写入
	rebasedTargetRun := run1
	rebasedTargetRun.ID = 0
	rebasedTargetRun.TargetHeadSHA = "9999888877776666555544443333222211110000"
	if err := db.Create(&rebasedTargetRun).Error; err != nil {
		t.Fatalf("目标分支基线变化后 (新 target_head_sha) 应当允许创建新 MRCheckRun，却失败: %v", err)
	}

	// (d) bundle_id IS NULL 时 COALESCE(bundle_id, 0) 同样具备幂等去重保护
	nullBundle1 := MRCheckRun{
		GitRepoID:             repo.ID,
		BundleID:              nil,
		MRIID:                 43,
		EventPhase:            MREventPhaseGate,
		SourceBranch:          "feat/root",
		TargetBranch:          "main",
		HeadCommitSHA:         "1234567890abcdef1234567890abcdef12345678",
		TargetHeadSHA:         "fedcba0987654321fedcba0987654321fedcba09",
		Status:                MRCheckStatusPending,
		ChangedProtoFilesJSON: datatypes.JSON([]byte(`[]`)),
		BreakingDetailsJSON:   datatypes.JSON([]byte(`[]`)),
		AffectedModulesJSON:   datatypes.JSON([]byte(`[]`)),
	}
	if err := db.Create(&nullBundle1).Error; err != nil {
		t.Fatalf("创建 bundle_id=NULL 的 MRCheckRun 失败: %v", err)
	}
	nullBundleDup := nullBundle1
	nullBundleDup.ID = 0
	if err := db.Create(&nullBundleDup).Error; err == nil {
		t.Fatalf("违反契约：bundle_id=NULL 的相同 (repo, phase, head_sha, target_sha) 应被 COALESCE(bundle_id, 0) 唯一索引拦截")
	}

	// 4. 验证 traefik_notifications 写入、索引与状态枚举约束
	now := time.Now().UTC()
	notif := Notification{
		RecipientUserID: 7,
		RecipientEmail:  "bob@example.com",
		Category:        NotifCategoryBreakingAlert,
		Severity:        NotifSeverityCritical,
		Title:           "契约破坏性变更告警",
		BodyMarkdown:    "MR !42 检测到字段 Tag 变异",
		LinkURL:         "/schemas?tab=mr-gates&runId=1",
		SourceType:      NotifSourceMRCheckRun,
		SourceID:        run1.ID,
		MetadataJSON:    datatypes.JSON([]byte(`{"mr_iid":42}`)),
		IsRead:          false,
		ExternalChannel: ExternalChannelCodeNotifier,
		EmailStatus:     EmailStatusSent,
		EmailSentAt:     &now,
	}
	if err := db.Create(&notif).Error; err != nil {
		t.Fatalf("创建 Notification 失败: %v", err)
	}

	badNotif := notif
	badNotif.ID = 0
	badNotif.EmailStatus = "INVALID_STATUS"
	if err := db.Create(&badNotif).Error; err == nil {
		t.Fatalf("非法 email_status 应当被 chk_traefik_notif_email_status 拦截")
	}
}
