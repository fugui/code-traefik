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

// ResolveDestinationModule 依据全局唯一供给原则 (A-05) 穿透定位目标功能模块 (Service 级别)
func ResolveDestinationModule(provideMap map[string]uint64, serviceName string) *uint64 {
	key := strings.TrimSpace(serviceName)
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
// 2. 唯一 DEPEND 声明：M(P) 中仅有 1 个模块声明了对该 Service 的 DEPEND -> UNIQUE_DEPEND
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

	// 2. 优先级 ②: 过滤 M(P) 中声明了 DEPEND(svc) 的子集
	callKey := strings.TrimSpace(egressRecord.ServiceName)
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
	// 1. 加载全局唯一 PROVIDE 映射 (Service 级)
	var services []models.ModuleService
	if err := db.WithContext(ctx).Find(&services).Error; err != nil {
		return err
	}
	provideMap := make(map[string]uint64, len(services))
	for _, svc := range services {
		key := strings.TrimSpace(svc.ServiceName)
		provideMap[key] = svc.ModuleID
	}

	// 2. 加载全局 DEPEND 映射: moduleID -> map[serviceName]bool
	var deps []models.ModuleDependency
	if err := db.WithContext(ctx).Preload("Service").Find(&deps).Error; err != nil {
		return err
	}
	dependMap := make(map[uint64]map[string]bool)
	for _, dep := range deps {
		if dependMap[dep.ConsumerModuleID] == nil {
			dependMap[dep.ConsumerModuleID] = make(map[string]bool)
		}
		if dep.Service != nil {
			key := strings.TrimSpace(dep.Service.ServiceName)
			dependMap[dep.ConsumerModuleID][key] = true
		} else if dep.IsExternal && dep.ExternalService != "" {
			dependMap[dep.ConsumerModuleID][strings.TrimSpace(dep.ExternalService)] = true
		}
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

	// 补充通过 module.process_id 关联的承载模块 (双保险)
	var allModules []models.Module
	if err := db.WithContext(ctx).Where("process_id IS NOT NULL AND process_id > 0").Find(&allModules).Error; err == nil {
		for _, m := range allModules {
			pid := *m.ProcessID
			already := false
			for _, mid := range processModules[pid] {
				if mid == m.ID {
					already = true
					break
				}
			}
			if !already {
				processModules[pid] = append(processModules[pid], m.ID)
			}
		}
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
		// 定位目标模块 (按最新契约补全或更新)
		if dstID := ResolveDestinationModule(provideMap, rec.ServiceName); dstID != nil {
			rec.DstModuleID = dstID
		}
		if rec.Direction == models.TrafficDirIngress && rec.SpanID != "" && rec.SpanID != "0" {
			ingressMap[rec.SpanID] = rec
		}
	}

	// 6. 批量处理出站记录的来源模块归属并回写数据库
	for i := range records {
		rec := &records[i]
		if rec.Direction == models.TrafficDirEgress {
			// 如果记录已具备明确有效的来源模块归属（如仿真测试或前序结算），予以保留
			if rec.SrcAttributionMode == "" || rec.SrcModuleID == nil {
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
