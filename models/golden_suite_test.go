package models_test

import (
	"strings"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-traefik/config"
	"code-traefik/models"

	"gorm.io/datatypes"
)

func TestGoldenSuiteModelsSchemaConstraintsAndDecoupling(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}

	// 1. 验证表名前缀
	suite := models.GoldenSuite{}
	suiteItem := models.GoldenSuiteItem{}
	if !strings.HasPrefix(suite.TableName(), config.TablePrefix) {
		t.Fatalf("GoldenSuite 表名 %s 未以 %s 开头", suite.TableName(), config.TablePrefix)
	}
	if !strings.HasPrefix(suiteItem.TableName(), config.TablePrefix) {
		t.Fatalf("GoldenSuiteItem 表名 %s 未以 %s 开头", suiteItem.TableName(), config.TablePrefix)
	}

	// 2. 创建基础架构数据
	ownerID := uint(1)
	sub := models.Subsystem{Code: "SUB_GS", Name: "GoldenSuite Subsystem", OwnerID: &ownerID}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatalf("创建 Subsystem 失败: %v", err)
	}
	grp := models.ArchProcessGroup{Code: "GRP_GS", Name: "GS Group"}
	if err := db.Create(&grp).Error; err != nil {
		t.Fatalf("创建 ArchProcessGroup 失败: %v", err)
	}
	archProc := models.ArchProcess{Code: "PROC_GS", Name: "GS Process", GroupID: grp.ID, DefaultListenPort: 9001}
	if err := db.Create(&archProc).Error; err != nil {
		t.Fatalf("创建 ArchProcess 失败: %v", err)
	}

	// 3. 创建黄金套件
	gs := models.GoldenSuite{
		Code:             "GS_ORDER_CORE",
		Name:             "订单核心黄金契约套件",
		ArchProcessID:    archProc.ID,
		Version:          "v1.0.0",
		Status:           models.GoldenSuiteStatusActive,
		ExtraIgnorePaths: datatypes.JSON([]byte(`["rsp.timestamp"]`)),
		Description:      "用于测试自包含快照模型",
		CreatedBy:        1,
	}
	if err := db.Create(&gs).Error; err != nil {
		t.Fatalf("创建 GoldenSuite 失败: %v", err)
	}

	// 验证 code 唯一性约束
	gsDuplicateCode := models.GoldenSuite{
		Code:          "GS_ORDER_CORE",
		Name:          "重复编码套件",
		ArchProcessID: archProc.ID,
		Status:        models.GoldenSuiteStatusActive,
	}
	if err := db.Create(&gsDuplicateCode).Error; err == nil {
		t.Fatalf("期望重复 Code 创建失败，实际成功了")
	}

	// 4. 创建完全自包含用例快照 (不依赖外部 traffic_records)
	item1 := models.GoldenSuiteItem{
		SuiteID:         gs.ID,
		OrderNo:         1,
		Name:            "创建订单成功正常流",
		ServiceName:     "order.v1.OrderService",
		Operation:       "CreateOrder",
		BaselineStatus:  0,
		RspSegCount:     1,
		RawReqPacket:    []byte("RAW_REQ_BYTES_001"),
		RawRspPacket:    []byte("RAW_RSP_BYTES_001"),
		ScopedMocks:     datatypes.JSON([]byte(`[{"service_name":"pay.v1.PayService"}]`)),
		EgressEndpoints: datatypes.JSON([]byte(`[{"ip":"127.0.0.1","port":9002}]`)),
		MaskRulesJSON:   datatypes.JSON([]byte(`[{"path":"rsp.token","strategy":"IGNORE"}]`)),
		CreatedBy:       1,
	}
	if err := db.Create(&item1).Error; err != nil {
		t.Fatalf("创建 GoldenSuiteItem 失败: %v", err)
	}

	// 验证 order_no 唯一性
	itemDuplicateOrder := models.GoldenSuiteItem{
		SuiteID:        gs.ID,
		OrderNo:        1,
		Name:           "序号重复用例",
		ServiceName:    "order.v1.OrderService",
		Operation:      "CreateOrder",
		RawReqPacket:   []byte("RAW_REQ_BYTES_002"),
		RawRspPacket:   []byte("RAW_RSP_BYTES_002"),
		BaselineStatus: 0,
	}
	if err := db.Create(&itemDuplicateOrder).Error; err == nil {
		t.Fatalf("期望同套件相同 OrderNo 创建失败，实际成功了")
	}

	// 5. 双源 ReplayTask 与 ReplayItem 测试
	agent := models.Agent{Name: "AGENT_GS", Endpoint: "127.0.0.1:9099", Scope: models.ScopeShared}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("创建 Agent 失败: %v", err)
	}

	// 测试黄金套件驱动的 ReplayTask (SessionID 为 nil, GoldenSuiteID 非空)
	suiteTask := models.ReplayTask{
		TaskNo:            "REP_GS_20261105001",
		Name:              "黄金套件脱机回归任务",
		TriggerSource:     models.TriggerSourceGoldenSuite,
		OwnerID:           1,
		GoldenSuiteID:     &gs.ID,
		SessionID:         nil,
		ArchProcessID:     archProc.ID,
		AgentID:           agent.ID,
		SelectedRecordIDs: datatypes.JSON([]byte(`[]`)),
		ExtraIgnorePaths:  datatypes.JSON([]byte(`[]`)),
		LifecycleState:    "ARMED",
		TotalRequests:     1,
		CreatedBy:         1,
	}
	if err := db.Create(&suiteTask).Error; err != nil {
		t.Fatalf("创建基于黄金套件的 ReplayTask 失败: %v", err)
	}

	// 测试黄金套件驱动的 ReplayItem (RecordID 为 nil, SuiteItemID 非空)
	suiteItemRec := models.ReplayItem{
		TaskID:         suiteTask.ID,
		SuiteItemID:    &item1.ID,
		RecordID:       nil,
		SeqNo:          1,
		ServiceName:    item1.ServiceName,
		Operation:      item1.Operation,
		ResultStatus:   "PASS",
		ActualStatus:   0,
		BaselineStatus: 0,
		CreatedAt:      time.Now(),
	}
	if err := db.Create(&suiteItemRec).Error; err != nil {
		t.Fatalf("创建基于黄金用例的 ReplayItem 失败: %v", err)
	}

	// 6. 测试源 RecordingSession 删除时，ReplayTask.SessionID 自动置 NULL，任务与套件毫发无损
	recSess := models.RecordingSession{
		SessionNo:        "SESS_TEST_001",
		Name:             "临时素材录制批次",
		Scope:            models.ScopeShared,
		OwnerID:          1,
		Status:           "STOPPED",
		TargetProcessIDs: datatypes.JSON([]byte(`[]`)),
		CreatedBy:        1,
	}
	if err := db.Create(&recSess).Error; err != nil {
		t.Fatalf("创建 RecordingSession 失败: %v", err)
	}

	sessionTask := models.ReplayTask{
		TaskNo:            "REP_SESS_20261105001",
		Name:              "批次重放任务",
		TriggerSource:     models.TriggerSourceManual,
		OwnerID:           1,
		SessionID:         &recSess.ID,
		ArchProcessID:     archProc.ID,
		AgentID:           agent.ID,
		SelectedRecordIDs: datatypes.JSON([]byte(`[]`)),
		ExtraIgnorePaths:  datatypes.JSON([]byte(`[]`)),
		LifecycleState:    "ARMED",
		CreatedBy:         1,
	}
	if err := db.Create(&sessionTask).Error; err != nil {
		t.Fatalf("创建基于批次的 ReplayTask 失败: %v", err)
	}

	// 删除 recSess
	if err := db.Delete(&recSess).Error; err != nil {
		t.Fatalf("删除 RecordingSession 失败: %v", err)
	}

	// 校验 sessionTask 依然存在，且 SessionID 已被置为 NULL
	var reloadedTask models.ReplayTask
	if err := db.First(&reloadedTask, sessionTask.ID).Error; err != nil {
		t.Fatalf("源 RecordingSession 删除后，ReplayTask 消失: %v", err)
	}
	if reloadedTask.SessionID != nil {
		t.Fatalf("期望 ReplayTask.SessionID 变为 nil (SET NULL)，实际为: %v", *reloadedTask.SessionID)
	}

	// 7. 测试级联删除 GoldenSuite 时，自动级联删除 GoldenSuiteItem，而绑定的 ReplayTask.GoldenSuiteID 被置为 NULL
	if err := db.Delete(&gs).Error; err != nil {
		t.Fatalf("删除 GoldenSuite 失败: %v", err)
	}
	var countItems int64
	db.Model(&models.GoldenSuiteItem{}).Where("suite_id = ?", gs.ID).Count(&countItems)
	if countItems != 0 {
		t.Fatalf("GoldenSuite 删除后，GoldenSuiteItem 未被级联删除，剩余: %d", countItems)
	}

	var reloadedSuiteTask models.ReplayTask
	if err := db.First(&reloadedSuiteTask, suiteTask.ID).Error; err != nil {
		t.Fatalf("GoldenSuite 删除后，ReplayTask 消失: %v", err)
	}
	if reloadedSuiteTask.GoldenSuiteID != nil {
		t.Fatalf("期望 ReplayTask.GoldenSuiteID 变为 nil (SET NULL)，实际为: %v", *reloadedSuiteTask.GoldenSuiteID)
	}
}
