package topology

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"code-traefik/models"
	"code-traefik/pkg/agentproto"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type VerificationStatus string

const (
	StatusVerified      VerificationStatus = "VERIFIED"
	StatusViolation     VerificationStatus = "VIOLATION"
	StatusUncovered     VerificationStatus = "UNCOVERED"
	StatusNotMonitored  VerificationStatus = "NOT_MONITORED"
	StatusNotObservable VerificationStatus = "NOT_OBSERVABLE"
	StatusUnattributed  VerificationStatus = "UNATTRIBUTED"
)

// ConsistencyEdgeResult 单条边一致性验证结果
type ConsistencyEdgeResult struct {
	Status             VerificationStatus `json:"status"`
	Category           VerificationStatus `json:"category"`
	SrcModuleID        *uint64            `json:"src_module_id,omitempty"`
	SourceModuleName   string             `json:"source_module_name,omitempty"`
	SrcProcessID       *uint64            `json:"src_process_id,omitempty"`
	CandidateModuleIDs []uint64           `json:"candidate_module_ids,omitempty"`
	DstModuleID        *uint64            `json:"dst_module_id,omitempty"` // nil 表示未登记外部接口
	TargetModuleName   string             `json:"target_module_name,omitempty"`
	ServiceName        string             `json:"service_name"`
	Operation          string             `json:"operation"`
	ObservedCallCount  int64              `json:"observed_call_count"`
	CallCount          int64              `json:"call_count"`
	AttributionMode    string             `json:"attribution_mode,omitempty"`
	SampleTrafficRecID *uint64            `json:"sample_traffic_record_id,omitempty"`
	Detail             string             `json:"detail,omitempty"`
}

// ConsistencyReportDTO 一致性验证报告响应 DTO
type ConsistencyReportDTO struct {
	SessionID          uint64                  `json:"session_id"`
	StaticModelVersion time.Time               `json:"static_model_version"` // 静态架构版本快照时刻
	VerifiedCount      int                     `json:"verified_count"`
	ViolationCount     int                     `json:"violation_count"`
	UncoveredCount     int                     `json:"uncovered_count"`
	NotMonitoredCount  int                     `json:"not_monitored_count"`
	NotObservableCount int                     `json:"not_observable_count"`
	UnattributedCount  int                     `json:"unattributed_count"`
	Stats              map[string]int          `json:"stats"`
	Edges              []ConsistencyEdgeResult `json:"edges"`
}

// VerifySessionTopology 动态报文穿透还原实际调用并与静态契约比对 (F-05.3, SC-06)
func VerifySessionTopology(ctx context.Context, db *gorm.DB, sessionID uint64, refresh bool) (*ConsistencyReportDTO, error) {
	// 若存在尚未执行归属结算的出站报文，触发离线批量对账
	var unattrCount int64
	_ = db.WithContext(ctx).Model(&models.TrafficRecord{}).
		Where("session_id = ? AND direction = ? AND (src_attribution_mode = '' OR src_attribution_mode IS NULL)", sessionID, models.TrafficDirEgress).
		Count(&unattrCount).Error
	if unattrCount > 0 {
		_ = OfflineBatchAttribution(ctx, db, sessionID)
	}

	var session models.RecordingSession
	if err := db.WithContext(ctx).First(&session, sessionID).Error; err != nil {
		return nil, err
	}

	// 1. 解析目标监控进程 ID 集合
	var targetProcessIDs []uint64
	if len(session.TargetProcessIDs) > 0 {
		_ = json.Unmarshal(session.TargetProcessIDs, &targetProcessIDs)
	}
	isMonitoredProc := make(map[uint64]bool, len(targetProcessIDs))
	for _, pid := range targetProcessIDs {
		isMonitoredProc[pid] = true
	}

	// 2. 加载全部进程承载模块映射及公共模块默认进程 (基于 ArchProcessID)
	var allProcs []models.Process
	if err := db.WithContext(ctx).Find(&allProcs).Error; err != nil {
		return nil, err
	}
	var allModules []models.Module
	if err := db.WithContext(ctx).Find(&allModules).Error; err != nil {
		return nil, err
	}
	archModulesMap := make(map[uint64][]uint64)
	moduleNameMap := make(map[uint64]string, len(allModules))
	for _, m := range allModules {
		moduleNameMap[m.ID] = m.Name
		if m.ArchProcessID != nil {
			archModulesMap[*m.ArchProcessID] = append(archModulesMap[*m.ArchProcessID], m.ID)
		}
	}
	moduleProcessMap := make(map[uint64]uint64)
	procModulesMap := make(map[uint64][]uint64)
	for _, p := range allProcs {
		if mods, ok := archModulesMap[p.ArchProcessID]; ok {
			procModulesMap[p.ID] = mods
			for _, mid := range mods {
				if isMonitoredProc[p.ID] {
					moduleProcessMap[mid] = p.ID
				} else if _, exists := moduleProcessMap[mid]; !exists {
					moduleProcessMap[mid] = p.ID
				}
			}
		}
	}

	// 本次纳管的模块集合与所属纳管进程
	monitoredModuleIDs := make(map[uint64]uint64) // moduleID -> processID
	for _, pid := range targetProcessIDs {
		for _, mid := range procModulesMap[pid] {
			monitoredModuleIDs[mid] = pid
		}
	}

	// 3. 加载全局唯一 PROVIDE 服务 (Service 级)
	var provideServices []models.ModuleService
	if err := db.WithContext(ctx).Find(&provideServices).Error; err != nil {
		return nil, err
	}
	provideMap := make(map[string]uint64, len(provideServices))
	for _, svc := range provideServices {
		key := strings.TrimSpace(svc.ServiceName)
		provideMap[key] = svc.ModuleID
	}

	// 4. 加载本次纳管模块声明的所有依赖
	var declaredDependencies []models.ModuleDependency
	var monitoredModIDList []uint64
	for mid := range monitoredModuleIDs {
		monitoredModIDList = append(monitoredModIDList, mid)
	}

	if len(monitoredModIDList) > 0 {
		if err := db.WithContext(ctx).Preload("Service").
			Where("consumer_module_id IN ?", monitoredModIDList).
			Find(&declaredDependencies).Error; err != nil {
			return nil, err
		}
	}

	// 5. 查询本次 session 的出站流量记录 (EGRESS)
	var egressRecords []models.TrafficRecord
	if err := db.WithContext(ctx).
		Where("session_id = ? AND direction = ?", sessionID, models.TrafficDirEgress).
		Find(&egressRecords).Error; err != nil {
		return nil, err
	}

	// 聚合实测出站调用: key = (srcModID, dstModID, svc, op, mode)
	type callGroupKey struct {
		SrcModID uint64
		DstModID uint64
		Svc      string
		Op       string
		Mode     string
		ProcID   uint64
	}
	type callGroupVal struct {
		count    int64
		sampleID uint64
		candIDs  []uint64
	}
	observedMap := make(map[callGroupKey]*callGroupVal)
	// 供快速检查某个 (srcModID, svc, op) 是否被观测到
	srcCallCount := make(map[string]int64) // "srcModID:svc:op" -> count
	srcCallSample := make(map[string]uint64)

	for _, rec := range egressRecords {
		var srcID uint64
		if rec.SrcModuleID != nil {
			srcID = *rec.SrcModuleID
		}
		var dstID uint64
		if rec.DstModuleID != nil {
			dstID = *rec.DstModuleID
		}
		mode := rec.SrcAttributionMode
		if mode == "" {
			mode = AttributionProcessLevel
		}

		key := callGroupKey{
			SrcModID: srcID,
			DstModID: dstID,
			Svc:      strings.TrimSpace(rec.ServiceName),
			Op:       strings.TrimSpace(rec.Operation),
			Mode:     mode,
			ProcID:   rec.ProcessID,
		}

		val := observedMap[key]
		if val == nil {
			var candIDs []uint64
			if len(rec.SrcCandidateModuleIDs) > 0 {
				_ = json.Unmarshal(rec.SrcCandidateModuleIDs, &candIDs)
			}
			val = &callGroupVal{
				count:    0,
				sampleID: rec.ID,
				candIDs:  candIDs,
			}
			observedMap[key] = val
		}
		val.count++

		if srcID > 0 {
			ck := fmt.Sprintf("%d:%s:%s", srcID, key.Svc, key.Op)
			srcCallCount[ck] += 1
			if srcCallSample[ck] == 0 {
				srcCallSample[ck] = rec.ID
			}
		}
	}

	// 6. 对纳管模块的静态依赖逐边进行评估
	edges := make([]ConsistencyEdgeResult, 0)
	matchedDeclaredCalls := make(map[string]bool) // "srcModID:svc:op" -> true

	for _, dep := range declaredDependencies {
		srcModID := dep.ConsumerModuleID
		srcProcID := monitoredModuleIDs[srcModID]
		var svc string
		if dep.Service != nil {
			svc = strings.TrimSpace(dep.Service.ServiceName)
		} else if dep.IsExternal {
			svc = strings.TrimSpace(dep.ExternalService)
		}

		var dstModID *uint64
		if dep.ProviderModuleID != nil && *dep.ProviderModuleID > 0 {
			dstModID = dep.ProviderModuleID
		} else if dep.Service != nil && dep.Service.ModuleID > 0 {
			mid := dep.Service.ModuleID
			dstModID = &mid
		}

		ckPrefix := fmt.Sprintf("%d:%s:", srcModID, svc)
		var obsCount int64
		var sampleID *uint64
		for ck, cnt := range srcCallCount {
			if strings.HasPrefix(ck, ckPrefix) {
				obsCount += cnt
				if sampleID == nil && srcCallSample[ck] > 0 {
					sid := srcCallSample[ck]
					sampleID = &sid
				}
				matchedDeclaredCalls[ck] = true
			}
		}

		if dstModID == nil || dep.IsExternal {
			// 未登记外部接口或无内部供给者
			edge := ConsistencyEdgeResult{
				Status:             StatusNotMonitored,
				SrcModuleID:        &srcModID,
				SrcProcessID:       &srcProcID,
				DstModuleID:        nil,
				ServiceName:        svc,
				Operation:          "",
				ObservedCallCount:  obsCount,
				SampleTrafficRecID: sampleID,
			}
			edges = append(edges, edge)
			continue
		}

		dstProcID := moduleProcessMap[*dstModID]

		// 判定 ①: 进程内调用 (INTRA_PROCESS) -> NOT_OBSERVABLE
		if srcProcID > 0 && dstProcID > 0 && srcProcID == dstProcID {
			edge := ConsistencyEdgeResult{
				Status:             StatusNotObservable,
				SrcModuleID:        &srcModID,
				SrcProcessID:       &srcProcID,
				DstModuleID:        dstModID,
				ServiceName:        svc,
				Operation:          "",
				ObservedCallCount:  obsCount,
				SampleTrafficRecID: sampleID,
			}
			edges = append(edges, edge)
			continue
		}

		// 判定 ②: 目标供给模块所在物理进程未加入本次监控批次 -> NOT_MONITORED
		if !isMonitoredProc[dstProcID] {
			edge := ConsistencyEdgeResult{
				Status:             StatusNotMonitored,
				SrcModuleID:        &srcModID,
				SrcProcessID:       &srcProcID,
				DstModuleID:        dstModID,
				ServiceName:        svc,
				Operation:          "",
				ObservedCallCount:  obsCount,
				SampleTrafficRecID: sampleID,
			}
			edges = append(edges, edge)
			continue
		}

		// 判定 ③: 目标与来源均纳管，检查实测调用
		if obsCount > 0 {
			// VERIFIED: 静态跨进程声明且实测调用 >= 1
			edge := ConsistencyEdgeResult{
				Status:             StatusVerified,
				SrcModuleID:        &srcModID,
				SrcProcessID:       &srcProcID,
				DstModuleID:        dstModID,
				ServiceName:        svc,
				Operation:          "",
				ObservedCallCount:  obsCount,
				SampleTrafficRecID: sampleID,
				AttributionMode:    AttributionUniqueDepend,
			}
			edges = append(edges, edge)
		} else {
			// UNCOVERED: 均纳管但未捕获调用
			edge := ConsistencyEdgeResult{
				Status:             StatusUncovered,
				SrcModuleID:        &srcModID,
				SrcProcessID:       &srcProcID,
				DstModuleID:        dstModID,
				ServiceName:        svc,
				Operation:          "",
				ObservedCallCount:  0,
				SampleTrafficRecID: nil,
			}
			edges = append(edges, edge)
		}
	}

	// 7. 处理实测中捕获到的未声明调用 (VIOLATION) 与多模块歧义调用 (UNATTRIBUTED)
	for groupKey, val := range observedMap {
		sampleID := val.sampleID
		var sampleIDPtr *uint64
		if sampleID > 0 {
			sampleIDPtr = &sampleID
		}

		// UNATTRIBUTED: 进程级归属，无法区分是哪个模块发出的
		if groupKey.Mode == AttributionProcessLevel || groupKey.SrcModID == 0 {
			procID := groupKey.ProcID
			var dstModPtr *uint64
			if groupKey.DstModID > 0 {
				dstID := groupKey.DstModID
				dstModPtr = &dstID
			}
			edge := ConsistencyEdgeResult{
				Status:             StatusUnattributed,
				SrcModuleID:        nil,
				SrcProcessID:       &procID,
				CandidateModuleIDs: val.candIDs,
				DstModuleID:        dstModPtr,
				ServiceName:        groupKey.Svc,
				Operation:          groupKey.Op,
				ObservedCallCount:  val.count,
				AttributionMode:    groupKey.Mode,
				SampleTrafficRecID: sampleIDPtr,
			}
			edges = append(edges, edge)
			continue
		}

		// 已确切归属到 SrcModuleID，检查是否属于已知已匹配的声明调用
		ck := fmt.Sprintf("%d:%s:%s", groupKey.SrcModID, groupKey.Svc, groupKey.Op)
		if matchedDeclaredCalls[ck] {
			// 已在第 6 步生成 VERIFIED 边，更新实际 AttributionMode 即可
			for i := range edges {
				if edges[i].SrcModuleID != nil && *edges[i].SrcModuleID == groupKey.SrcModID &&
					edges[i].ServiceName == groupKey.Svc &&
					edges[i].Status == StatusVerified {
					edges[i].AttributionMode = groupKey.Mode
					break
				}
			}
			continue
		}

		// 未声明该 DEPEND (或调用了未声明/未登记接口) -> VIOLATION
		srcModID := groupKey.SrcModID
		srcProcID := monitoredModuleIDs[srcModID]
		if srcProcID == 0 {
			srcProcID = groupKey.ProcID
		}
		var dstModPtr *uint64
		if groupKey.DstModID > 0 {
			dstID := groupKey.DstModID
			dstModPtr = &dstID
		}

		edge := ConsistencyEdgeResult{
			Status:             StatusViolation,
			SrcModuleID:        &srcModID,
			SrcProcessID:       &srcProcID,
			DstModuleID:        dstModPtr,
			ServiceName:        groupKey.Svc,
			Operation:          groupKey.Op,
			ObservedCallCount:  val.count,
			AttributionMode:    groupKey.Mode,
			SampleTrafficRecID: sampleIDPtr,
		}
		edges = append(edges, edge)
	}

	// 8. 丰富边信息并汇总各状态计数
	for i := range edges {
		edges[i].Category = edges[i].Status
		edges[i].CallCount = edges[i].ObservedCallCount
		if edges[i].SrcModuleID != nil {
			if name, ok := moduleNameMap[*edges[i].SrcModuleID]; ok {
				edges[i].SourceModuleName = name
			}
		}
		if edges[i].DstModuleID != nil {
			if name, ok := moduleNameMap[*edges[i].DstModuleID]; ok {
				edges[i].TargetModuleName = name
			}
		}
		if edges[i].Detail == "" {
			switch edges[i].Status {
			case StatusVerified:
				edges[i].Detail = "实测跨模块调用与静态 PROVIDE/DEPEND 声明契约完全吻合"
			case StatusViolation:
				edges[i].Detail = "实测抓取到调用关系，但静态拓扑架构中未显式声明 DEPEND 依赖"
			case StatusUncovered:
				edges[i].Detail = "静态拓扑已声明依赖契约，但本次录制窗口内未观测到实际调用"
			case StatusNotMonitored:
				edges[i].Detail = "目标供给方进程未纳入本次会话录制监控范围"
			case StatusNotObservable:
				edges[i].Detail = "同进程内部调用或缺乏必要探针插桩"
			case StatusUnattributed:
				edges[i].Detail = "无法将源地址推导归属至具体单一业务模块"
			}
		}
	}

	report := &ConsistencyReportDTO{
		SessionID:          sessionID,
		StaticModelVersion: time.Now(),
		Edges:              edges,
	}

	for _, e := range edges {
		switch e.Status {
		case StatusVerified:
			report.VerifiedCount++
		case StatusViolation:
			report.ViolationCount++
		case StatusUncovered:
			report.UncoveredCount++
		case StatusNotMonitored:
			report.NotMonitoredCount++
		case StatusNotObservable:
			report.NotObservableCount++
		case StatusUnattributed:
			report.UnattributedCount++
		}
	}

	report.Stats = map[string]int{
		string(StatusVerified):      report.VerifiedCount,
		string(StatusViolation):     report.ViolationCount,
		string(StatusUncovered):     report.UncoveredCount,
		string(StatusNotMonitored):  report.NotMonitoredCount,
		string(StatusNotObservable): report.NotObservableCount,
		string(StatusUnattributed):  report.UnattributedCount,
	}

	// 9. 更新 session 表的 TopologyVerifySummary 缓存
	summaryJSON, err := json.Marshal(report)
	if err == nil {
		_ = db.WithContext(ctx).Model(&models.RecordingSession{}).
			Where("id = ?", sessionID).
			Update("topology_verify_summary", datatypes.JSON(summaryJSON)).Error
	}

	return report, nil
}

// ObservedViolationDTO 实测出站边违规明细 DTO (ADR-14, F-27)
type ObservedViolationDTO struct {
	ServiceName string `json:"service_name"`
	Operation   string `json:"operation"`
	DstAddr     string `json:"dst_addr"`
	CallCount   int    `json:"call_count"`
	MatchStatus string `json:"match_status"`
	Reason      string `json:"reason"`
}

// VerifyProcessObservedCalls 校验目标标准架构进程在重放实测期间捕获的出站调用是否合规 (ADR-14, M17, F-27)
// 比对目标 ArchProcess 所装载模块的静态 DEPEND 契约，识别所有未报备的违规出站调用并写入 ReplayTask.ViolationCount
func VerifyProcessObservedCalls(
	ctx context.Context,
	db *gorm.DB,
	archProcessID uint64,
	observedCalls []agentproto.ObservedEgressCall,
) ([]ObservedViolationDTO, error) {
	if len(observedCalls) == 0 {
		return nil, nil
	}

	// 1. 加载目标标准架构进程所属全部功能模块
	var modules []models.Module
	if err := db.WithContext(ctx).Where("arch_process_id = ?", archProcessID).Find(&modules).Error; err != nil {
		return nil, fmt.Errorf("查询架构进程所属模块失败: %w", err)
	}

	modIDs := make([]uint64, 0, len(modules))
	for _, m := range modules {
		modIDs = append(modIDs, m.ID)
	}

	allowedServices := make(map[string]bool)
	if len(modIDs) > 0 {
		// 2. 加载模块所声明的静态依赖服务 (DEPEND)
		var deps []models.ModuleDependency
		if err := db.WithContext(ctx).Preload("Service").Where("consumer_module_id IN ?", modIDs).Find(&deps).Error; err != nil {
			return nil, fmt.Errorf("查询模块依赖关系失败: %w", err)
		}
		for _, dep := range deps {
			if dep.Service != nil {
				svc := strings.TrimSpace(dep.Service.ServiceName)
				if svc != "" {
					allowedServices[svc] = true
				}
			}
			if dep.IsExternal && dep.ExternalService != "" {
				svc := strings.TrimSpace(dep.ExternalService)
				if svc != "" {
					allowedServices[svc] = true
				}
			}
		}

		// 3. 同时也允许本进程所属模块自身对外供给的服务 (PROVIDE，进程内自我调用)
		var provs []models.ModuleService
		if err := db.WithContext(ctx).Where("module_id IN ?", modIDs).Find(&provs).Error; err != nil {
			return nil, fmt.Errorf("查询模块供给服务失败: %w", err)
		}
		for _, p := range provs {
			svc := strings.TrimSpace(p.ServiceName)
			if svc != "" {
				allowedServices[svc] = true
			}
		}
	}

	// 4. 比对实测出站调用，检出未报备违规调用 (VIOLATION)
	var violations []ObservedViolationDTO
	for _, c := range observedCalls {
		svc := strings.TrimSpace(c.ServiceName)
		if svc == "" {
			continue
		}
		if !allowedServices[svc] {
			violations = append(violations, ObservedViolationDTO{
				ServiceName: c.ServiceName,
				Operation:   c.Operation,
				DstAddr:     c.DstAddr,
				CallCount:   c.CallCount,
				MatchStatus: c.MatchStatus,
				Reason:      fmt.Sprintf("未在标准进程装载模块的静态 DEPEND 契约中报备下游服务 [%s]", c.ServiceName),
			})
		}
	}

	return violations, nil
}
