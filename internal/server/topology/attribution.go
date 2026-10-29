package topology

import (
	"context"
	"encoding/json"
	"strings"

	"code-traefik/models"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	AttributionSingleModuleProc = "SINGLE_MODULE_PROC"
	AttributionTraceSpan        = "TRACE_SPAN"
	AttributionUniqueDepend     = "UNIQUE_DEPEND"
	AttributionProcessLevel     = "PROCESS_LEVEL"
)

// ResolveDestinationModule 依据全局唯一供给原则 (A-05) 穿透定位目标功能模块
func ResolveDestinationModule(provideMap map[string]uint64, serviceName, operation string) *uint64 {
	key := strings.TrimSpace(serviceName) + ":" + strings.TrimSpace(operation)
	if modID, ok := provideMap[key]; ok && modID > 0 {
		res := modID
		return &res
	}
	return nil
}

// ResolveSourceModule 针对进程出站请求，执行三级优先级来源模块判定 (F-05.2)
// 优先级：
// 0. 单模块独占进程：|M(P)| == 1 -> SINGLE_MODULE_PROC
// 1. Trace Span 匹配：FatherSpanID 匹配同批次该进程入站请求 SpanID -> TRACE_SPAN
// 2. 唯一 DEPEND 声明：M(P) 中仅有 1 个模块声明了该 DEPEND -> UNIQUE_DEPEND
// 3. 歧义或未声明：返回 PROCESS_LEVEL，SrcModule=nil，candidates 为 M(P)
func ResolveSourceModule(
	processModules map[uint64][]uint64,
	ingressReqsBySpanID map[string]*models.TrafficRecord,
	egressRecord *models.TrafficRecord,
	dependMap map[uint64]map[string]bool,
) (srcModuleID *uint64, mode string, candidates []uint64) {
	if egressRecord == nil {
		return nil, AttributionProcessLevel, nil
	}

	modules := processModules[egressRecord.ProcessID]
	if len(modules) == 0 {
		return nil, AttributionProcessLevel, nil
	}

	// 0. 单模块独占进程前置收敛
	if len(modules) == 1 {
		modID := modules[0]
		return &modID, AttributionSingleModuleProc, nil
	}

	// 1. 优先级 ①: DOTR 扩展头存在非零 TraceID 与 FatherSpanID
	fatherSpan := strings.TrimSpace(egressRecord.FatherSpanID)
	if fatherSpan != "" && fatherSpan != "0" && ingressReqsBySpanID != nil {
		if parentReq, ok := ingressReqsBySpanID[fatherSpan]; ok && parentReq != nil {
			if parentReq.DstModuleID != nil && *parentReq.DstModuleID > 0 {
				dstID := *parentReq.DstModuleID
				// 校验该 DstModule 属于当前进程所承载的模块
				for _, m := range modules {
					if m == dstID {
						return &dstID, AttributionTraceSpan, nil
					}
				}
			}
		}
	}

	// 2. 优先级 ②: 过滤 M(P) 中声明了 DEPEND(svc, op) 的子集
	callKey := strings.TrimSpace(egressRecord.ServiceName) + ":" + strings.TrimSpace(egressRecord.Operation)
	var depCandidates []uint64
	if dependMap != nil {
		for _, m := range modules {
			if mDeps, ok := dependMap[m]; ok && mDeps[callKey] {
				depCandidates = append(depCandidates, m)
			}
		}
	}

	if len(depCandidates) == 1 {
		modID := depCandidates[0]
		return &modID, AttributionUniqueDepend, nil
	}

	// 3. 优先级 ③: 歧义或均未声明，降级为进程级归属
	return nil, AttributionProcessLevel, modules
}

// OfflineBatchAttribution 在录制批次停止后，统一归集全链路报文并执行结算 (OfflineBatchAttribution)
func OfflineBatchAttribution(ctx context.Context, db *gorm.DB, sessionID uint64) error {
	// 1. 加载全局唯一 PROVIDE 映射
	var provideIfaces []models.ModuleInterface
	if err := db.WithContext(ctx).Where("relation_type = ?", "PROVIDE").Find(&provideIfaces).Error; err != nil {
		return err
	}
	provideMap := make(map[string]uint64, len(provideIfaces))
	for _, iface := range provideIfaces {
		key := strings.TrimSpace(iface.ServiceName) + ":" + strings.TrimSpace(iface.Operation)
		provideMap[key] = iface.ModuleID
	}

	// 2. 加载全局 DEPEND 映射: moduleID -> map[callKey]bool
	var dependIfaces []models.ModuleInterface
	if err := db.WithContext(ctx).Where("relation_type = ?", "DEPEND").Find(&dependIfaces).Error; err != nil {
		return err
	}
	dependMap := make(map[uint64]map[string]bool)
	for _, iface := range dependIfaces {
		if dependMap[iface.ModuleID] == nil {
			dependMap[iface.ModuleID] = make(map[string]bool)
		}
		key := strings.TrimSpace(iface.ServiceName) + ":" + strings.TrimSpace(iface.Operation)
		dependMap[iface.ModuleID][key] = true
	}

	// 3. 加载进程承载模块映射
	var procMods []models.ProcessModule
	if err := db.WithContext(ctx).Find(&procMods).Error; err != nil {
		return err
	}
	processModules := make(map[uint64][]uint64)
	for _, pm := range procMods {
		processModules[pm.ProcessID] = append(processModules[pm.ProcessID], pm.ModuleID)
	}

	// 4. 查询该 session 的所有流量记录
	var records []models.TrafficRecord
	if err := db.WithContext(ctx).Where("session_id = ?", sessionID).Order("id ASC").Find(&records).Error; err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}

	// 5. 构建入站 Span 索引: spanID -> *TrafficRecord (必须已定位 DstModule)
	ingressMap := make(map[string]*models.TrafficRecord)
	for i := range records {
		rec := &records[i]
		// 先定位目标模块
		if rec.DstModuleID == nil {
			rec.DstModuleID = ResolveDestinationModule(provideMap, rec.ServiceName, rec.Operation)
		}
		if rec.Direction == models.TrafficDirIngress && rec.SpanID != "" && rec.SpanID != "0" {
			ingressMap[rec.SpanID] = rec
		}
	}

	// 6. 批量处理出站记录的来源模块归属并回写数据库
	for i := range records {
		rec := &records[i]
		if rec.Direction == models.TrafficDirEgress {
			srcID, mode, candidates := ResolveSourceModule(processModules, ingressMap, rec, dependMap)
			rec.SrcModuleID = srcID
			rec.SrcAttributionMode = mode
			if len(candidates) > 0 {
				candBytes, _ := json.Marshal(candidates)
				rec.SrcCandidateModuleIDs = datatypes.JSON(candBytes)
			} else {
				rec.SrcCandidateModuleIDs = nil
			}
		}

		updates := map[string]interface{}{
			"dst_module_id":            rec.DstModuleID,
			"src_module_id":            rec.SrcModuleID,
			"src_attribution_mode":     rec.SrcAttributionMode,
			"src_candidate_module_ids": rec.SrcCandidateModuleIDs,
		}
		if err := db.WithContext(ctx).Model(&models.TrafficRecord{}).
			Where("id = ? AND created_at = ?", rec.ID, rec.CreatedAt).
			Updates(updates).Error; err != nil {
			return err
		}
	}

	return nil
}
