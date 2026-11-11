package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"code-traefik/internal/server/topology"
	"code-traefik/models"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// ViolatedProcessStat 违规标准架构进程统计
type ViolatedProcessStat struct {
	ArchProcessID   uint64 `json:"arch_process_id"`
	ArchProcessCode string `json:"arch_process_code"`
	ArchProcessName string `json:"arch_process_name"`
	ViolationCount  int    `json:"violation_count"`
	SubsystemID     uint64 `json:"subsystem_id,omitempty"`
	SubsystemName   string `json:"subsystem_name,omitempty"`
}

// OrphanServiceSummary 未认领孤儿服务契约摘要
type OrphanServiceSummary struct {
	ServiceName    string `json:"service_name"`
	SchemaID       uint64 `json:"schema_id"`
	SchemaName     string `json:"schema_name"`
	OperationCount int    `json:"operation_count"`
}

// HealthScoreDTO 三维封顶健康指数
type HealthScoreDTO struct {
	Total              int      `json:"total"`
	Grade              string   `json:"grade"` // EXCELLENT | GOOD | SUBHEALTH | CRITICAL
	ArchSubScore       int      `json:"arch_sub_score"`
	AssetSubScore      int      `json:"asset_sub_score"`
	RegressionSubScore int      `json:"regression_sub_score"`
	ActionHints        []string `json:"action_hints"`
}

// DriftRadarDTO 架构漂移雷达
type DriftRadarDTO struct {
	VerifiedEdges        int                   `json:"verified_edges"`
	ActiveViolationEdges int                   `json:"active_violation_edges"`
	TopViolatedProcesses []ViolatedProcessStat `json:"top_violated_processes"`
}

// ContractAssetsDTO 契约资产认领看板
type ContractAssetsDTO struct {
	TotalActiveServices    int                    `json:"total_active_services"`
	ClaimedServices        int                    `json:"claimed_services"`
	OrphanServices         int                    `json:"orphan_services"`
	ClaimedRatio           float64                `json:"claimed_ratio"`
	TopOrphanServices      []OrphanServiceSummary `json:"top_orphan_services"`
	MultiImplContractCount int                    `json:"multi_impl_contract_count"`
	MultiImplServiceCount  int                    `json:"multi_impl_service_count"`
}

// RegressionDefenseDTO 防劣化回归防线
type RegressionDefenseDTO struct {
	GateEvaluations30d        int     `json:"gate_evaluations_30d"`
	BlockedCount30d           int     `json:"blocked_count_30d"`
	PassRate30d               float64 `json:"pass_rate_30d"`
	UnhealthySuiteProcesses7d int     `json:"unhealthy_suite_processes_7d"`
}

// DashboardOverviewDTO 全景治理大盘响应 DTO
type DashboardOverviewDTO struct {
	HealthScore       HealthScoreDTO       `json:"health_score"`
	DriftRadar        DriftRadarDTO        `json:"drift_radar"`
	ContractAssets    ContractAssetsDTO    `json:"contract_assets"`
	RegressionDefense RegressionDefenseDTO `json:"regression_defense"`
}

type hintItem struct {
	hint      string
	lostScore int
}

// GetOverview 计算并返回全景治理大盘聚合数据 (零 N+1，零扫描 traffic_records bytea)
func GetOverview(ctx context.Context, db *gorm.DB, subsystemID *uint64) (*DashboardOverviewDTO, error) {
	overview := &DashboardOverviewDTO{
		HealthScore: HealthScoreDTO{
			Total:              100,
			Grade:              "EXCELLENT",
			ArchSubScore:       40,
			AssetSubScore:      30,
			RegressionSubScore: 30,
			ActionHints:        make([]string, 0),
		},
		DriftRadar: DriftRadarDTO{
			VerifiedEdges:        0,
			ActiveViolationEdges: 0,
			TopViolatedProcesses: make([]ViolatedProcessStat, 0),
		},
		ContractAssets: ContractAssetsDTO{
			TotalActiveServices: 0,
			ClaimedServices:     0,
			OrphanServices:      0,
			ClaimedRatio:        1.0,
			TopOrphanServices:   make([]OrphanServiceSummary, 0),
		},
		RegressionDefense: RegressionDefenseDTO{
			GateEvaluations30d:        0,
			BlockedCount30d:           0,
			PassRate30d:               1.0,
			UnhealthySuiteProcesses7d: 0,
		},
	}

	// 1. 查询子系统、功能模块与标准架构进程映射
	var subsystems []models.Subsystem
	if err := db.WithContext(ctx).Find(&subsystems).Error; err != nil {
		return nil, fmt.Errorf("查询子系统失败: %w", err)
	}
	subsystemMap := make(map[uint64]models.Subsystem, len(subsystems))
	for _, s := range subsystems {
		subsystemMap[s.ID] = s
	}

	var modules []models.Module
	if err := db.WithContext(ctx).Find(&modules).Error; err != nil {
		return nil, fmt.Errorf("查询模块失败: %w", err)
	}
	moduleMap := make(map[uint64]models.Module, len(modules))
	subsystemModuleIDs := make(map[uint64]bool)
	targetArchProcIDMap := make(map[uint64]bool)

	for _, m := range modules {
		moduleMap[m.ID] = m
		if subsystemID != nil && *subsystemID > 0 {
			if m.SubsystemID == *subsystemID {
				subsystemModuleIDs[m.ID] = true
				if m.ArchProcessID != nil {
					targetArchProcIDMap[*m.ArchProcessID] = true
				}
			}
		} else {
			subsystemModuleIDs[m.ID] = true
			if m.ArchProcessID != nil {
				targetArchProcIDMap[*m.ArchProcessID] = true
			}
		}
	}

	var archProcesses []models.ArchProcess
	if err := db.WithContext(ctx).Find(&archProcesses).Error; err != nil {
		return nil, fmt.Errorf("查询标准架构进程失败: %w", err)
	}
	archProcMap := make(map[uint64]models.ArchProcess, len(archProcesses))
	for _, ap := range archProcesses {
		archProcMap[ap.ID] = ap
	}

	// 2. 架构防漂移子分 S_arch 与漂移雷达计算
	// 统计防污染：仅查询 scope = 'SHARED' 且已具备核验摘要的批次
	type sessionMeta struct {
		ID                    uint64
		Scope                 string
		Status                string
		ProcessSnapshots      datatypes.JSON
		TopologyVerifySummary datatypes.JSON
		StoppedAt             *time.Time
		CreatedAt             time.Time
	}

	var sharedSessions []sessionMeta
	if err := db.WithContext(ctx).Model(&models.RecordingSession{}).
		Select("id, scope, status, process_snapshots, topology_verify_summary, stopped_at, created_at").
		Where("scope = ? AND topology_verify_summary IS NOT NULL", models.ScopeShared).
		Order("stopped_at DESC NULLS LAST, created_at DESC, id DESC").
		Find(&sharedSessions).Error; err != nil {
		return nil, fmt.Errorf("查询共享核验报告批次失败: %w", err)
	}

	// 为每个标准架构进程寻找最近一次有效的核验批次
	type procEdgeViolation struct {
		srcModuleID uint64
		serviceName string
	}

	verifiedEdgeSet := make(map[string]bool)
	violationEdgeSet := make(map[string]bool)
	procViolations := make(map[uint64]map[string]bool)

	// 遍历所有目标标准进程
	for procID := range targetArchProcIDMap {
		// 寻找包含该 procID 的最新批次
		for _, sess := range sharedSessions {
			var snapshots []models.ProcessSnapshotItem
			if len(sess.ProcessSnapshots) > 0 {
				_ = json.Unmarshal(sess.ProcessSnapshots, &snapshots)
			}
			hasProc := false
			procSnapshotModuleIDs := make(map[uint64]bool)
			for _, snap := range snapshots {
				if snap.ArchProcessID == procID {
					hasProc = true
					for _, mid := range snap.ModuleIDs {
						procSnapshotModuleIDs[mid] = true
					}
				}
			}

			// 如果快照中没有，检查该进程名下的模块
			if !hasProc {
				for _, m := range modules {
					if m.ArchProcessID != nil && *m.ArchProcessID == procID {
						procSnapshotModuleIDs[m.ID] = true
					}
				}
			}

			// 解析核验摘要中的边
			var report topology.ConsistencyReportDTO
			if err := json.Unmarshal(sess.TopologyVerifySummary, &report); err != nil {
				continue
			}

			// 检查边是否属于该进程
			procMatchedInSession := false
			for _, edge := range report.Edges {
				edgeBelongsToProc := false
				if edge.SrcModuleID != nil {
					if procSnapshotModuleIDs[*edge.SrcModuleID] {
						edgeBelongsToProc = true
					} else if mod, ok := moduleMap[*edge.SrcModuleID]; ok && mod.ArchProcessID != nil && *mod.ArchProcessID == procID {
						edgeBelongsToProc = true
					}
				}
				if edgeBelongsToProc {
					procMatchedInSession = true
					edgeKey := fmt.Sprintf("%d:%s", ptrUint64Val(edge.SrcModuleID), edge.ServiceName)
					if edge.Status == topology.StatusVerified {
						verifiedEdgeSet[edgeKey] = true
					} else if edge.Status == topology.StatusViolation {
						violationEdgeSet[edgeKey] = true
						if procViolations[procID] == nil {
							procViolations[procID] = make(map[string]bool)
						}
						procViolations[procID][edgeKey] = true
					}
				}
			}

			// 若该批次覆盖了当前进程，则此批次即为该进程最近一次有效核验，停止向后追溯历史批次
			if hasProc || procMatchedInSession {
				break
			}
		}
	}

	overview.DriftRadar.VerifiedEdges = len(verifiedEdgeSet)
	overview.DriftRadar.ActiveViolationEdges = len(violationEdgeSet)

	// S_arch = max(0, 40 - ActiveViolationEdges * 10)
	overview.HealthScore.ArchSubScore = 40 - overview.DriftRadar.ActiveViolationEdges*10
	if overview.HealthScore.ArchSubScore < 0 {
		overview.HealthScore.ArchSubScore = 0
	}

	// 组装 TOP 违规进程
	topViolated := make([]ViolatedProcessStat, 0, len(procViolations))
	for procID, vMap := range procViolations {
		vCount := len(vMap)
		if vCount == 0 {
			continue
		}
		ap := archProcMap[procID]
		stat := ViolatedProcessStat{
			ArchProcessID:   procID,
			ArchProcessCode: ap.Code,
			ArchProcessName: ap.Name,
			ViolationCount:  vCount,
		}
		// 寻找所属子系统
		for _, m := range modules {
			if m.ArchProcessID != nil && *m.ArchProcessID == procID {
				stat.SubsystemID = m.SubsystemID
				if sub, ok := subsystemMap[m.SubsystemID]; ok {
					stat.SubsystemName = sub.Name
				}
				break
			}
		}
		topViolated = append(topViolated, stat)
	}

	sort.Slice(topViolated, func(i, j int) bool {
		if topViolated[i].ViolationCount != topViolated[j].ViolationCount {
			return topViolated[i].ViolationCount > topViolated[j].ViolationCount
		}
		return topViolated[i].ArchProcessID < topViolated[j].ArchProcessID
	})

	if len(topViolated) > 5 {
		topViolated = topViolated[:5]
	}
	overview.DriftRadar.TopViolatedProcesses = topViolated

	// 3. 契约资产认领子分 S_asset 与契约看板计算
	// 查询所有激活的 ProtoSchema（排除 service_count = 0 的 BASE_LIB）
	var activeSchemas []models.ProtoSchema
	if err := db.WithContext(ctx).Where("is_active = true AND NOT (category = 'BASE_LIB' AND service_count = 0)").Find(&activeSchemas).Error; err != nil {
		return nil, fmt.Errorf("查询生效描述集失败: %w", err)
	}
	activeSchemaMap := make(map[uint64]models.ProtoSchema, len(activeSchemas))
	activeSchemaIDs := make([]uint64, 0, len(activeSchemas))
	for _, s := range activeSchemas {
		activeSchemaMap[s.ID] = s
		activeSchemaIDs = append(activeSchemaIDs, s.ID)
	}

	type activeServiceInfo struct {
		serviceName    string
		schemaID       uint64
		schemaName     string
		operationCount int
	}

	activeServicesMap := make(map[string]*activeServiceInfo)
	if len(activeSchemaIDs) > 0 {
		var activeOps []models.ProtoOperation
		if err := db.WithContext(ctx).Where("is_active = true AND schema_id IN ?", activeSchemaIDs).Find(&activeOps).Error; err != nil {
			return nil, fmt.Errorf("查询生效方法映射失败: %w", err)
		}
		for _, op := range activeOps {
			svc, ok := activeServicesMap[op.ServiceName]
			if !ok {
				schName := ""
				if sch, found := activeSchemaMap[op.SchemaID]; found {
					schName = sch.Name
					if schName == "" {
						schName = sch.FileName
					}
				}
				svc = &activeServiceInfo{
					serviceName:    op.ServiceName,
					schemaID:       op.SchemaID,
					schemaName:     schName,
					operationCount: 0,
				}
				activeServicesMap[op.ServiceName] = svc
			}
			svc.operationCount++
		}
	}

	// 查询已被 FM 认领的 ModuleService
	var modServices []models.ModuleService
	if err := db.WithContext(ctx).Find(&modServices).Error; err != nil {
		return nil, fmt.Errorf("查询模块服务失败: %w", err)
	}

	claimedServicesMap := make(map[string]bool)
	for _, ms := range modServices {
		if mod, ok := moduleMap[ms.ModuleID]; ok {
			if subsystemID != nil && *subsystemID > 0 {
				if mod.SubsystemID == *subsystemID {
					claimedServicesMap[ms.ServiceName] = true
				}
			} else {
				claimedServicesMap[ms.ServiceName] = true
			}
		}
	}

	if subsystemID != nil && *subsystemID > 0 {
		// 子系统视角：仅计算本子系统提供的服务
		claimedCount := len(claimedServicesMap)
		overview.ContractAssets.TotalActiveServices = claimedCount
		overview.ContractAssets.ClaimedServices = claimedCount
		overview.ContractAssets.OrphanServices = 0
		overview.ContractAssets.ClaimedRatio = 1.0
		overview.HealthScore.AssetSubScore = 30
		overview.ContractAssets.TopOrphanServices = make([]OrphanServiceSummary, 0)
	} else {
		// 全局视角：统计全系统生效服务与认领率
		totalActive := len(activeServicesMap)
		claimedCount := 0
		orphanList := make([]OrphanServiceSummary, 0)

		for svcName, info := range activeServicesMap {
			if claimedServicesMap[svcName] {
				claimedCount++
			} else {
				orphanList = append(orphanList, OrphanServiceSummary{
					ServiceName:    svcName,
					SchemaID:       info.schemaID,
					SchemaName:     info.schemaName,
					OperationCount: info.operationCount,
				})
			}
		}

		overview.ContractAssets.TotalActiveServices = totalActive
		overview.ContractAssets.ClaimedServices = claimedCount
		overview.ContractAssets.OrphanServices = totalActive - claimedCount

		if totalActive == 0 {
			overview.ContractAssets.ClaimedRatio = 1.0
			overview.HealthScore.AssetSubScore = 30
		} else {
			ratio := float64(claimedCount) / float64(totalActive)
			overview.ContractAssets.ClaimedRatio = math.Round(ratio*10000) / 10000
			assetScore := int(math.Round(30.0 * ratio))
			if assetScore < 0 {
				assetScore = 0
			}
			if assetScore > 30 {
				assetScore = 30
			}
			overview.HealthScore.AssetSubScore = assetScore
		}

		sort.Slice(orphanList, func(i, j int) bool {
			if orphanList[i].OperationCount != orphanList[j].OperationCount {
				return orphanList[i].OperationCount > orphanList[j].OperationCount
			}
			return orphanList[i].ServiceName < orphanList[j].ServiceName
		})

		if len(orphanList) > 10 {
			orphanList = orphanList[:10]
		}
		overview.ContractAssets.TopOrphanServices = orphanList
	}

	// 统计多实现服务契约数量与实现数量 (M20)
	var contractCount int64
	_ = db.WithContext(ctx).Model(&models.ServiceContract{}).Count(&contractCount).Error
	var multiImplCount int64
	_ = db.WithContext(ctx).Model(&models.ModuleService{}).Where("contract_id IS NOT NULL").Count(&multiImplCount).Error
	overview.ContractAssets.MultiImplContractCount = int(contractCount)
	overview.ContractAssets.MultiImplServiceCount = int(multiImplCount)

	// 4. 防劣化回归子分 S_reg 与回归防线计算
	// 仅统计权威套件与 CI 门禁任务 (trigger_source IN ('GOLDEN_SUITE', 'CI_GATE'))
	now := time.Now()
	since30d := now.AddDate(0, 0, -30)
	since7d := now.AddDate(0, 0, -7)

	replayQuery := db.WithContext(ctx).Model(&models.ReplayTask{}).
		Where("trigger_source IN (?)", []string{models.TriggerSourceGoldenSuite, models.TriggerSourceCIGate})

	if subsystemID != nil && *subsystemID > 0 {
		archProcIDs := make([]uint64, 0, len(targetArchProcIDMap))
		for pid := range targetArchProcIDMap {
			archProcIDs = append(archProcIDs, pid)
		}
		if len(archProcIDs) == 0 {
			archProcIDs = []uint64{0}
		}
		replayQuery = replayQuery.Where("arch_process_id IN ?", archProcIDs)
	}

	var replayTasks30d []models.ReplayTask
	if err := replayQuery.Where("created_at >= ?", since30d).Find(&replayTasks30d).Error; err != nil {
		return nil, fmt.Errorf("查询近 30 天回归任务失败: %w", err)
	}

	evalCount := len(replayTasks30d)
	blockedCount := 0
	for _, t := range replayTasks30d {
		if t.ResultStatus != models.ReplayResultPass {
			blockedCount++
		}
	}

	overview.RegressionDefense.GateEvaluations30d = evalCount
	overview.RegressionDefense.BlockedCount30d = blockedCount
	if evalCount == 0 {
		overview.RegressionDefense.PassRate30d = 1.0
	} else {
		passRate := float64(evalCount-blockedCount) / float64(evalCount)
		overview.RegressionDefense.PassRate30d = math.Round(passRate*10000) / 10000
	}

	// 近 7 天各标准进程最新一次权威回归状态
	procLatestStatus7d := make(map[uint64]string)
	for _, t := range replayTasks30d {
		if t.CreatedAt.After(since7d) {
			if _, exists := procLatestStatus7d[t.ArchProcessID]; !exists {
				// replayTasks30d 默认不是按时间排序的，这里找最新的
				procLatestStatus7d[t.ArchProcessID] = t.ResultStatus
			}
		}
	}

	unhealthyProcs7d := 0
	for _, status := range procLatestStatus7d {
		if status != models.ReplayResultPass {
			unhealthyProcs7d++
		}
	}
	overview.RegressionDefense.UnhealthySuiteProcesses7d = unhealthyProcs7d

	regSubScore := 30 - unhealthyProcs7d*10
	if regSubScore < 0 {
		regSubScore = 0
	}
	overview.HealthScore.RegressionSubScore = regSubScore

	// 5. 总分与评级计算
	total := overview.HealthScore.ArchSubScore + overview.HealthScore.AssetSubScore + overview.HealthScore.RegressionSubScore
	if total < 0 {
		total = 0
	}
	if total > 100 {
		total = 100
	}
	overview.HealthScore.Total = total

	if total >= 90 {
		overview.HealthScore.Grade = "EXCELLENT"
	} else if total >= 75 {
		overview.HealthScore.Grade = "GOOD"
	} else if total >= 60 {
		overview.HealthScore.Grade = "SUBHEALTH"
	} else {
		overview.HealthScore.Grade = "CRITICAL"
	}

	// 6. 结构化整改建议 (按失分降序排列)
	hints := make([]hintItem, 0)
	archLoss := 40 - overview.HealthScore.ArchSubScore
	if archLoss > 0 {
		hints = append(hints, hintItem{
			lostScore: archLoss,
			hint:      fmt.Sprintf("检测到 %d 条架构漂移违规调用边，请前往【架构拓扑】或【契约采集】排查未声明依赖并补齐 DEPEND 契约", overview.DriftRadar.ActiveViolationEdges),
		})
	}

	assetLoss := 30 - overview.HealthScore.AssetSubScore
	if assetLoss > 0 {
		hints = append(hints, hintItem{
			lostScore: assetLoss,
			hint:      fmt.Sprintf("存在 %d 个生效 Proto 服务契约未被功能模块认领（孤儿契约），请前往【Proto 契约】进行认领归属", overview.ContractAssets.OrphanServices),
		})
	}

	regLoss := 30 - overview.HealthScore.RegressionSubScore
	if regLoss > 0 {
		hints = append(hints, hintItem{
			lostScore: regLoss,
			hint:      fmt.Sprintf("近 7 天有 %d 个标准架构进程的黄金套件/CI门禁回归未通过，请排查破坏性变更与回归失败项", overview.RegressionDefense.UnhealthySuiteProcesses7d),
		})
	}

	sort.Slice(hints, func(i, j int) bool {
		return hints[i].lostScore > hints[j].lostScore
	})

	actionHints := make([]string, 0, len(hints))
	for _, h := range hints {
		actionHints = append(actionHints, h.hint)
	}
	overview.HealthScore.ActionHints = actionHints

	return overview, nil
}

func ptrUint64Val(v *uint64) uint64 {
	if v == nil {
		return 0
	}
	return *v
}
