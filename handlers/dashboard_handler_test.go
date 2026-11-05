package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"code-traefik/config"
	"code-traefik/internal/server/topology"
	"code-traefik/models"

	"gorm.io/datatypes"
)

// DashboardOverviewResponse 定义大盘概览响应契约结构
type DashboardOverviewResponse struct {
	HealthScore struct {
		Total              int      `json:"total"`
		Grade              string   `json:"grade"`
		ArchSubScore       int      `json:"arch_sub_score"`
		AssetSubScore      int      `json:"asset_sub_score"`
		RegressionSubScore int      `json:"regression_sub_score"`
		ActionHints        []string `json:"action_hints"`
	} `json:"health_score"`
	DriftRadar struct {
		VerifiedEdges        int `json:"verified_edges"`
		ActiveViolationEdges int `json:"active_violation_edges"`
		TopViolatedProcesses []struct {
			ArchProcessID   uint64 `json:"arch_process_id"`
			ArchProcessCode string `json:"arch_process_code"`
			ArchProcessName string `json:"arch_process_name"`
			ViolationCount  int    `json:"violation_count"`
			SubsystemID     uint64 `json:"subsystem_id,omitempty"`
			SubsystemName   string `json:"subsystem_name,omitempty"`
		} `json:"top_violated_processes"`
	} `json:"drift_radar"`
	ContractAssets struct {
		TotalActiveServices int     `json:"total_active_services"`
		ClaimedServices     int     `json:"claimed_services"`
		OrphanServices      int     `json:"orphan_services"`
		ClaimedRatio        float64 `json:"claimed_ratio"`
		TopOrphanServices   []struct {
			ServiceName    string `json:"service_name"`
			SchemaID       uint64 `json:"schema_id"`
			SchemaName     string `json:"schema_name"`
			OperationCount int    `json:"operation_count"`
		} `json:"top_orphan_services"`
	} `json:"contract_assets"`
	RegressionDefense struct {
		GateEvaluations30d        int     `json:"gate_evaluations_30d"`
		BlockedCount30d           int     `json:"blocked_count_30d"`
		PassRate30d               float64 `json:"pass_rate_30d"`
		UnhealthySuiteProcesses7d int     `json:"unhealthy_suite_processes_7d"`
	} `json:"regression_defense"`
}

// TestGetDashboardOverview_PerfectScore100_AndEmptyState
// 验证空库或无违规状态下治理大盘总分满分 100 分、各子维度满分及空态数据自洽
func TestGetDashboardOverview_PerfectScore100_AndEmptyState(t *testing.T) {
	r, db := setupRouterAndDB(t)

	user := createTestUserInDB(t, db, "user_empty_dash", "developer")
	token := generateTestJWT(t, user)

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/overview", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，实际: %d (body: %s)", w.Code, w.Body.String())
	}

	var resp DashboardOverviewResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析大盘响应失败: %v", err)
	}

	// 满分校验
	if resp.HealthScore.Total != 100 {
		t.Errorf("期望总分 100，实际: %d", resp.HealthScore.Total)
	}
	if resp.HealthScore.Grade != "EXCELLENT" {
		t.Errorf("期望评级 EXCELLENT，实际: %s", resp.HealthScore.Grade)
	}
	if resp.HealthScore.ArchSubScore != 40 {
		t.Errorf("期望架构子分 40，实际: %d", resp.HealthScore.ArchSubScore)
	}
	if resp.HealthScore.AssetSubScore != 30 {
		t.Errorf("期望契约资产子分 30，实际: %d", resp.HealthScore.AssetSubScore)
	}
	if resp.HealthScore.RegressionSubScore != 30 {
		t.Errorf("期望回归子分 30，实际: %d", resp.HealthScore.RegressionSubScore)
	}
	if len(resp.HealthScore.ActionHints) != 0 {
		t.Errorf("期望满分时整改建议为空，实际: %+v", resp.HealthScore.ActionHints)
	}

	// 漂移雷达空态
	if resp.DriftRadar.VerifiedEdges != 0 || resp.DriftRadar.ActiveViolationEdges != 0 {
		t.Errorf("期望漂移雷达边数为 0，实际: %+v", resp.DriftRadar)
	}
	if len(resp.DriftRadar.TopViolatedProcesses) != 0 {
		t.Errorf("期望违规进程为空，实际: %+v", resp.DriftRadar.TopViolatedProcesses)
	}

	// 契约资产空态 (0 Service 时认领率默认为 1.0)
	if resp.ContractAssets.TotalActiveServices != 0 || resp.ContractAssets.ClaimedServices != 0 || resp.ContractAssets.OrphanServices != 0 {
		t.Errorf("期望资产数全为 0，实际: %+v", resp.ContractAssets)
	}
	if resp.ContractAssets.ClaimedRatio != 1.0 {
		t.Errorf("期望空态认领率 1.0，实际: %f", resp.ContractAssets.ClaimedRatio)
	}

	// 回归防线空态
	if resp.RegressionDefense.GateEvaluations30d != 0 || resp.RegressionDefense.BlockedCount30d != 0 {
		t.Errorf("期望门禁评估为 0，实际: %+v", resp.RegressionDefense)
	}
	if resp.RegressionDefense.PassRate30d != 1.0 {
		t.Errorf("期望无回归时通过率默认为 1.0，实际: %f", resp.RegressionDefense.PassRate30d)
	}
	if resp.RegressionDefense.UnhealthySuiteProcesses7d != 0 {
		t.Errorf("期望亚健康进程数为 0，实际: %d", resp.RegressionDefense.UnhealthySuiteProcesses7d)
	}
}

// TestGetDashboardOverview_SubScoreCaps_30OrphansOnlyCapsAssetSubScore
// 验证三支柱子分封顶算法：即使存在 30 个孤儿契约，仅扣完契约资产子分 30 分，
// 架构子分与回归子分仍保持满分，总分为 70 分 (亚健康 SUBHEALTH)，避免单一维度恶化导致全局归零
func TestGetDashboardOverview_SubScoreCaps_30OrphansOnlyCapsAssetSubScore(t *testing.T) {
	r, db := setupRouterAndDB(t)

	admin := createTestUserInDB(t, db, "admin_orphan_dash", config.AdminRole)
	adminToken := generateTestJWT(t, admin)

	// 创建已激活的 Proto 描述集
	schema := models.ProtoSchema{
		Name:            "ecommerce_contracts",
		Version:         1,
		Category:        models.ProtoCategoryBusiness,
		FileName:        "ecommerce.pb",
		FileSHA256:      "sha_ecommerce_orphan",
		FileSize:        2048,
		DescriptorBytes: []byte{0x08, 0x01},
		IsActive:        true,
		ProtoFileCount:  1,
		ServiceCount:    30,
	}
	if err := db.Create(&schema).Error; err != nil {
		t.Fatalf("创建 Schema 失败: %v", err)
	}

	// 插入 30 个处于激活状态的 ProtoService / ProtoOperation (全部未认领)
	for i := 1; i <= 30; i++ {
		svcName := fmt.Sprintf("dophi.orphan.Service%02d", i)
		op := models.ProtoOperation{
			SchemaID:           schema.ID,
			ProtoFile:          "orphan.proto",
			ProtoPackage:       "dophi.orphan",
			ProtoService:       fmt.Sprintf("Service%02d", i),
			ServiceName:        svcName,
			Operation:          "DoAction",
			ReqMessageFullName: svcName + ".Req",
			RspMessageFullName: svcName + ".Rsp",
			IsActive:           true,
		}
		if err := db.Create(&op).Error; err != nil {
			t.Fatalf("创建 ProtoOperation 失败: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/overview", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，实际: %d (body: %s)", w.Code, w.Body.String())
	}

	var resp DashboardOverviewResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	// 核心断言：30 个孤儿契约仅封顶扣除 S_asset (30 -> 0)
	if resp.ContractAssets.TotalActiveServices != 30 {
		t.Errorf("期望生效服务 30，实际: %d", resp.ContractAssets.TotalActiveServices)
	}
	if resp.ContractAssets.ClaimedServices != 0 {
		t.Errorf("期望认领服务 0，实际: %d", resp.ContractAssets.ClaimedServices)
	}
	if resp.ContractAssets.OrphanServices != 30 {
		t.Errorf("期望孤儿服务 30，实际: %d", resp.ContractAssets.OrphanServices)
	}
	if resp.ContractAssets.ClaimedRatio != 0.0 {
		t.Errorf("期望认领率 0.0，实际: %f", resp.ContractAssets.ClaimedRatio)
	}

	if resp.HealthScore.AssetSubScore != 0 {
		t.Errorf("期望资产子分被封顶扣为 0，实际: %d", resp.HealthScore.AssetSubScore)
	}
	if resp.HealthScore.ArchSubScore != 40 {
		t.Errorf("期望架构防漂移子分仍为 40 满分，实际: %d", resp.HealthScore.ArchSubScore)
	}
	if resp.HealthScore.RegressionSubScore != 30 {
		t.Errorf("期望回归子分仍为 30 满分，实际: %d", resp.HealthScore.RegressionSubScore)
	}

	expectedTotal := 40 + 0 + 30 // 70
	if resp.HealthScore.Total != expectedTotal {
		t.Errorf("期望总分 %d，实际: %d", expectedTotal, resp.HealthScore.Total)
	}
	if resp.HealthScore.Grade != "SUBHEALTH" {
		t.Errorf("期望 70 分对应评级 SUBHEALTH，实际: %s", resp.HealthScore.Grade)
	}

	// 应当生成结构化整改建议
	if len(resp.HealthScore.ActionHints) == 0 {
		t.Errorf("期望存在孤儿契约治理建议，实际为空")
	}

	// 孤儿契约列表 TOP 应返回
	if len(resp.ContractAssets.TopOrphanServices) == 0 {
		t.Errorf("期望返回 TOP 孤儿契约清单")
	}
}

// TestGetDashboardOverview_ExcludesPrivateSessionsAndSingleDebugReplays
// 统计防污染铁律断言：验证研发个人 PRIVATE 录制素材产生的违规以及 /debug 单包调试 (SINGLE_DEBUG) 失败严禁混入全局扣分
func TestGetDashboardOverview_ExcludesPrivateSessionsAndSingleDebugReplays(t *testing.T) {
	r, db := setupRouterAndDB(t)

	alice := createTestUserInDB(t, db, "alice_private", "developer")
	bob := createTestUserInDB(t, db, "bob_reader", "developer")
	bobToken := generateTestJWT(t, bob)

	now := time.Now()

	// 1. 创建 Alice 的 PRIVATE 录制批次，带有架构违规边
	privReport := topology.ConsistencyReportDTO{
		SessionID:      101,
		VerifiedCount:  0,
		ViolationCount: 5,
		Edges: []topology.ConsistencyEdgeResult{
			{
				Status:      topology.StatusViolation,
				ServiceName: "dophi.private.NoiseService",
				CallCount:   50,
			},
		},
	}
	privReportJSON, _ := json.Marshal(privReport)

	privSession := models.RecordingSession{
		SessionNo:             "REC_PRIV_NOISE_01",
		Name:                  "Alice 本地私人录制",
		Scope:                 models.ScopePrivate,
		OwnerID:               alice.ID,
		Status:                models.SessionStatusCompleted,
		TargetProcessIDs:      datatypes.JSON(`[1]`),
		TopologyVerifySummary: datatypes.JSON(privReportJSON),
		StartedAt:             &now,
		StoppedAt:             &now,
		CreatedBy:             alice.ID,
		CreatedAt:             now,
	}
	if err := db.Create(&privSession).Error; err != nil {
		t.Fatalf("创建私有录制批次失败: %v", err)
	}

	// 2. 创建一个标准进程与一个 SINGLE_DEBUG 调试重放失败任务
	group := &models.ArchProcessGroup{Code: "GRP_DEBUG_TEST", Name: "调试测试组"}
	_ = db.Create(group).Error
	archProc := &models.ArchProcess{GroupID: group.ID, Code: "PROC_DEBUG_TEST", Name: "调试进程"}
	_ = db.Create(archProc).Error

	agent := &models.Agent{
		Name:      "Agent-01",
		Endpoint:  "127.0.0.1:8092",
		Scope:     models.ScopeShared,
		Status:    models.AgentStatusOnline,
		LeaseType: models.AgentLeaseNone,
	}
	_ = db.Create(agent).Error

	debugTask := models.ReplayTask{
		TaskNo:            "TASK_DEBUG_NOISE_01",
		Name:              "Alice 单包临时调试重放",
		TriggerSource:     models.TriggerSourceSingleDebug,
		OwnerID:           alice.ID,
		SessionID:         privSession.ID,
		ArchProcessID:     archProc.ID,
		AgentID:           agent.ID,
		SelectedRecordIDs: datatypes.JSON(`[1]`),
		LifecycleState:    "COMPLETED",
		ResultStatus:      models.ReplayResultFailed, // 失败状态
		CreatedAt:         now,
	}
	if err := db.Create(&debugTask).Error; err != nil {
		t.Fatalf("创建单包调试任务失败: %v", err)
	}

	// 3. Bob 访问大盘
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/overview", nil)
	req.Header.Set("Authorization", "Bearer "+bobToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，实际: %d", w.Code)
	}

	var resp DashboardOverviewResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	// 验证私有录制的 5 条违规完全不计入全局漂移雷达
	if resp.DriftRadar.ActiveViolationEdges != 0 {
		t.Errorf("PRIVATE 批次的违规边被错误纳入全局大盘: %d", resp.DriftRadar.ActiveViolationEdges)
	}
	if resp.HealthScore.ArchSubScore != 40 {
		t.Errorf("架构子分被 PRIVATE 违规错误扣分: %d", resp.HealthScore.ArchSubScore)
	}

	// 验证 SINGLE_DEBUG 的失败不计入回归防线和回归子分
	if resp.RegressionDefense.GateEvaluations30d != 0 {
		t.Errorf("SINGLE_DEBUG 重放被错误计入门禁评估: %d", resp.RegressionDefense.GateEvaluations30d)
	}
	if resp.HealthScore.RegressionSubScore != 30 {
		t.Errorf("回归子分被 SINGLE_DEBUG 错误扣分: %d", resp.HealthScore.RegressionSubScore)
	}

	// 总分仍然是满分 100
	if resp.HealthScore.Total != 100 {
		t.Errorf("期望总分保持 100 满分，实际: %d", resp.HealthScore.Total)
	}
}

// TestGetDashboardOverview_DeduplicatesViolationEdges_AndSubsystemFilter
// 验证同标准进程多批次违规边的去重以及 subsystem_id 子系统参数化过滤隔离
func TestGetDashboardOverview_DeduplicatesViolationEdges_AndSubsystemFilter(t *testing.T) {
	r, db := setupRouterAndDB(t)

	admin := createTestUserInDB(t, db, "admin_dedup", config.AdminRole)
	adminToken := generateTestJWT(t, admin)

	now := time.Now()

	// 1. 创建两个子系统 SUB_A 与 SUB_B
	subA := models.Subsystem{Code: "SUB_A", Name: "订单交易子系统"}
	subB := models.Subsystem{Code: "SUB_B", Name: "仓储物流子系统"}
	_ = db.Create(&subA).Error
	_ = db.Create(&subB).Error

	grpA := models.FunctionalGroup{SubsystemID: subA.ID, Code: "GRP_A", Name: "交易组", Level: 1, Path: fmt.Sprintf("/%d/", subA.ID)}
	grpB := models.FunctionalGroup{SubsystemID: subB.ID, Code: "GRP_B", Name: "仓储组", Level: 1, Path: fmt.Sprintf("/%d/", subB.ID)}
	_ = db.Create(&grpA).Error
	_ = db.Create(&grpB).Error

	// 2. 创建标准架构进程 PROC_A 与 PROC_B
	pGrp := models.ArchProcessGroup{Code: "PGRP_MAIN", Name: "主分组"}
	_ = db.Create(&pGrp).Error

	procA := models.ArchProcess{GroupID: pGrp.ID, Code: "PROC_A", Name: "订单核心标准进程"}
	procB := models.ArchProcess{GroupID: pGrp.ID, Code: "PROC_B", Name: "库存核心标准进程"}
	_ = db.Create(&procA).Error
	_ = db.Create(&procB).Error

	// 3. 将模块分别绑定到对应的子系统和标准进程
	modA := models.Module{SubsystemID: subA.ID, GroupID: grpA.ID, ArchProcessID: &procA.ID, Code: "MOD_ORDER", Name: "订单模块"}
	modB := models.Module{SubsystemID: subB.ID, GroupID: grpB.ID, ArchProcessID: &procB.ID, Code: "MOD_STOCK", Name: "库存模块"}
	_ = db.Create(&modA).Error
	_ = db.Create(&modB).Error

	// 4. 创建两个 SHARED 批次：
	// 批次 1 (较早)：PROC_A 产生违规边 (MOD_ORDER, "dophi.pay.PayService")
	// 批次 2 (较晚)：PROC_A 再次产生相同违规边 (MOD_ORDER, "dophi.pay.PayService")；同时 PROC_B 产生违规边 (MOD_STOCK, "dophi.wms.WmsService")
	earlier := now.Add(-2 * time.Hour)
	report1 := topology.ConsistencyReportDTO{
		SessionID:      201,
		ViolationCount: 1,
		Edges: []topology.ConsistencyEdgeResult{
			{
				Status:      topology.StatusViolation,
				SrcModuleID: &modA.ID,
				ServiceName: "dophi.pay.PayService",
				CallCount:   10,
			},
		},
	}
	r1JSON, _ := json.Marshal(report1)
	session1 := models.RecordingSession{
		SessionNo:             "REC_SHARED_01",
		Name:                  "公共录制批次 1",
		Scope:                 models.ScopeShared,
		OwnerID:               admin.ID,
		Status:                models.SessionStatusCompleted,
		TargetProcessIDs:      datatypes.JSON(`[1]`),
		ProcessSnapshots:      datatypes.JSON(fmt.Sprintf(`[{"process_id":1,"arch_process_id":%d,"code":"PROC_A","name":"订单核心标准进程","module_ids":[%d]}]`, procA.ID, modA.ID)),
		TopologyVerifySummary: datatypes.JSON(r1JSON),
		StartedAt:             &earlier,
		StoppedAt:             &earlier,
		CreatedAt:             earlier,
	}
	_ = db.Create(&session1).Error

	report2 := topology.ConsistencyReportDTO{
		SessionID:      202,
		ViolationCount: 2,
		Edges: []topology.ConsistencyEdgeResult{
			{
				Status:      topology.StatusViolation,
				SrcModuleID: &modA.ID,
				ServiceName: "dophi.pay.PayService", // 与批次 1 相同违规边
				CallCount:   20,
			},
			{
				Status:      topology.StatusViolation,
				SrcModuleID: &modB.ID,
				ServiceName: "dophi.wms.WmsService", // PROC_B 的违规边
				CallCount:   15,
			},
		},
	}
	r2JSON, _ := json.Marshal(report2)
	session2 := models.RecordingSession{
		SessionNo:             "REC_SHARED_02",
		Name:                  "公共录制批次 2",
		Scope:                 models.ScopeShared,
		OwnerID:               admin.ID,
		Status:                models.SessionStatusCompleted,
		TargetProcessIDs:      datatypes.JSON(`[1,2]`),
		ProcessSnapshots:      datatypes.JSON(fmt.Sprintf(`[{"process_id":1,"arch_process_id":%d,"code":"PROC_A","name":"订单核心标准进程","module_ids":[%d]},{"process_id":2,"arch_process_id":%d,"code":"PROC_B","name":"库存核心标准进程","module_ids":[%d]}]`, procA.ID, modA.ID, procB.ID, modB.ID)),
		TopologyVerifySummary: datatypes.JSON(r2JSON),
		StartedAt:             &now,
		StoppedAt:             &now,
		CreatedAt:             now,
	}
	_ = db.Create(&session2).Error

	// 5. 无参数全局查询：应当全局去重得到 2 条违规边（PayService 与 WmsService，而不是 3 条）
	{
		req := httptest.NewRequest(http.MethodGet, "/api/dashboard/overview", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("全量大盘期望 200，实际: %d", w.Code)
		}

		var resp DashboardOverviewResponse
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.DriftRadar.ActiveViolationEdges != 2 {
			t.Errorf("期望去重后活跃违规边数为 2，实际: %d", resp.DriftRadar.ActiveViolationEdges)
		}
		// S_arch = 40 - 2 * 10 = 20
		if resp.HealthScore.ArchSubScore != 20 {
			t.Errorf("期望架构子分 20，实际: %d", resp.HealthScore.ArchSubScore)
		}
		if len(resp.DriftRadar.TopViolatedProcesses) != 2 {
			t.Errorf("期望 TOP 违规进程数 2，实际: %d", len(resp.DriftRadar.TopViolatedProcesses))
		}
	}

	// 6. 带参数过滤查询：subsystem_id = subA.ID
	// 此时仅统计属于 subA 的 PROC_A 违规边（1 条 PayService），排除 subB 的 PROC_B 违规边
	{
		url := fmt.Sprintf("/api/dashboard/overview?subsystem_id=%d", subA.ID)
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("子系统过滤大盘期望 200，实际: %d", w.Code)
		}

		var resp DashboardOverviewResponse
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		if resp.DriftRadar.ActiveViolationEdges != 1 {
			t.Errorf("期望子系统 A 过滤后活跃违规边数为 1，实际: %d", resp.DriftRadar.ActiveViolationEdges)
		}
		// S_arch = 40 - 1 * 10 = 30
		if resp.HealthScore.ArchSubScore != 30 {
			t.Errorf("期望子系统 A 过滤后架构子分 30，实际: %d", resp.HealthScore.ArchSubScore)
		}
		if len(resp.DriftRadar.TopViolatedProcesses) != 1 {
			t.Errorf("期望子系统 A 仅有 1 个 TOP 违规进程，实际: %d", len(resp.DriftRadar.TopViolatedProcesses))
		}
		if len(resp.DriftRadar.TopViolatedProcesses) > 0 && resp.DriftRadar.TopViolatedProcesses[0].ArchProcessID != procA.ID {
			t.Errorf("期望违规进程为 PROC_A (%d)，实际: %d", procA.ID, resp.DriftRadar.TopViolatedProcesses[0].ArchProcessID)
		}
	}
}
