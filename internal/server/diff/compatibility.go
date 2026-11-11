package diff

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"code-traefik/models"
)

var (
	ErrForbiddenPrivateSession        = errors.New("403 Forbidden: 无权使用他人的私有录制素材")
	ErrForbiddenPrivateAgent          = errors.New("403 Forbidden: 无权在他人私有 Agent 上执行重放")
	ErrSourceProcessSnapshotNotFound  = errors.New("400 Bad Request: 录制会话中未找到源进程快照")
	ErrClientOnlyReplayTarget         = errors.New("400 Bad Request: 目标进程为纯客户端无监听端口，无法接收重放流量注入")
	ErrIncompatibleReplayTargetModule = errors.New("400 Bad Request: 目标进程未承载请求所需的全部功能模块")
)

// ValidateReplayProcessCompatibility 校验录制源进程选定报文能否安全重放到目标挂载进程 targetProc (F-12.1, SC-07, ADR-09)
func ValidateReplayProcessCompatibility(
	callerID uint,
	isAdmin bool,
	session *models.RecordingSession,
	selectedRecords []models.TrafficRecord,
	sourceSnap *models.ProcessSnapshotItem, // 优先从 session.ProcessSnapshots 提取
	targetProc *models.Process,
	targetAgent *models.Agent,
	archProcModules map[uint64][]uint64, // key: ArchProcessID -> []ModuleID
	provideMap map[string]uint64, // key: "serviceName" 或 "serviceName:operation" -> DstModuleID
) error {
	if session == nil || targetProc == nil || targetAgent == nil {
		return errors.New("400 Bad Request: 缺少必要参数")
	}

	// 1. 录制素材与目标 Agent 的公私域归属防越权校验 (Anti-IDOR)
	if session.Scope == models.ScopePrivate && session.OwnerID != callerID && !isAdmin {
		return ErrForbiddenPrivateSession
	}
	if targetAgent.Scope == models.ScopePrivate && (targetAgent.OwnerID == nil || *targetAgent.OwnerID != callerID) && !isAdmin {
		return ErrForbiddenPrivateAgent
	}

	// 2. 如果 sourceSnap 为 nil，尝试从 session.ProcessSnapshots 解析
	if sourceSnap == nil && len(session.ProcessSnapshots) > 0 {
		var snapshots []models.ProcessSnapshotItem
		if err := json.Unmarshal(session.ProcessSnapshots, &snapshots); err == nil && len(snapshots) > 0 {
			sourceSnap = &snapshots[0]
		}
	}
	if sourceSnap == nil {
		return ErrSourceProcessSnapshotNotFound
	}

	// 3. 目标挂载进程必须具备有效监听端口 (> 0，纯客户端进程无法接收重放注入)
	if targetProc.EffectiveListenPort <= 0 {
		return ErrClientOnlyReplayTarget
	}

	// 4. 原地同挂载实例 或 挂载同一标准架构进程蓝图 (ArchProcessID 相等) 天然 100% 同构兼容！
	if (sourceSnap.ProcessID > 0 && sourceSnap.ProcessID == targetProc.ID) ||
		(sourceSnap.ArchProcessID > 0 && targetProc.ArchProcessID > 0 && sourceSnap.ArchProcessID == targetProc.ArchProcessID) {
		return nil
	}

	// 5. 跨架构进程重构演进场景：校验目标进程是否完整承载选定请求命中的全部公共架构模块
	targetModSet := make(map[uint64]struct{})
	if targetProc.ArchProcessID > 0 && archProcModules != nil {
		for _, mid := range archProcModules[targetProc.ArchProcessID] {
			targetModSet[mid] = struct{}{}
		}
	}

	requiredMods := make(map[uint64]struct{})
	hasUnregisteredIface := false

	for _, rec := range selectedRecords {
		if rec.DstModuleID != nil && *rec.DstModuleID > 0 {
			requiredMods[*rec.DstModuleID] = struct{}{}
			continue
		}
		svc := strings.TrimSpace(rec.ServiceName)
		keyOp := svc + ":" + strings.TrimSpace(rec.Operation)
		if dstModID, ok := provideMap[svc]; ok {
			requiredMods[dstModID] = struct{}{}
		} else if dstModID, ok := provideMap[keyOp]; ok {
			requiredMods[dstModID] = struct{}{}
		} else {
			hasUnregisteredIface = true
		}
	}

	if hasUnregisteredIface || len(requiredMods) == 0 {
		sourceMods := make(map[uint64]struct{})
		for _, mid := range sourceSnap.ModuleIDs {
			sourceMods[mid] = struct{}{}
		}
		if len(sourceMods) == 0 && archProcModules != nil && sourceSnap.ArchProcessID > 0 {
			for _, mid := range archProcModules[sourceSnap.ArchProcessID] {
				sourceMods[mid] = struct{}{}
			}
		}
		if len(sourceMods) == 0 {
			return ErrIncompatibleReplayTargetModule
		}
		for sm := range sourceMods {
			if _, ok := targetModSet[sm]; !ok {
				return fmt.Errorf("%w: 缺失源进程公共模块 ID=%d", ErrIncompatibleReplayTargetModule, sm)
			}
		}
		return nil
	}

	for rm := range requiredMods {
		if _, ok := targetModSet[rm]; !ok {
			return fmt.Errorf("%w: 目标进程缺少公共模块 ID=%d", ErrIncompatibleReplayTargetModule, rm)
		}
	}
	return nil
}
