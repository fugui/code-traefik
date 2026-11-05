package callgraph

import (
	"encoding/json"
	"strconv"
	"time"

	"code-traefik/models"
	"gorm.io/gorm"
)

// HydrationCache 快照与元数据内存缓存
type HydrationCache struct {
	ProcessNames     map[uint64]string
	ArchProcessNames map[uint64]string
	ProcToArchID     map[uint64]uint64
	ModuleNames      map[uint64]string
	ConnIndices      map[string]int
	nextConnIndex    int
}

// NewHydrationCache 构造水合缓存
func NewHydrationCache() *HydrationCache {
	return &HydrationCache{
		ProcessNames:     make(map[uint64]string),
		ArchProcessNames: make(map[uint64]string),
		ProcToArchID:     make(map[uint64]uint64),
		ModuleNames:      make(map[uint64]string),
		ConnIndices:      make(map[string]int),
		nextConnIndex:    1,
	}
}

// LoadFromSession 快照预热缓存
func (c *HydrationCache) LoadFromSession(session *models.RecordingSession) {
	if session == nil || len(session.ProcessSnapshots) == 0 {
		return
	}
	var snapshots []models.ProcessSnapshotItem
	if err := json.Unmarshal(session.ProcessSnapshots, &snapshots); err == nil {
		for _, s := range snapshots {
			if s.ProcessID > 0 && s.Name != "" {
				c.ProcessNames[s.ProcessID] = s.Name
			}
			if s.ArchProcessID > 0 && s.Name != "" {
				c.ArchProcessNames[s.ArchProcessID] = s.Name
			}
			if s.ProcessID > 0 && s.ArchProcessID > 0 {
				c.ProcToArchID[s.ProcessID] = s.ArchProcessID
			}
		}
	}
}

// LoadFromDB 数据库批量加载补齐
func (c *HydrationCache) LoadFromDB(db *gorm.DB, procIDs []uint64, modIDs []uint64) {
	if db == nil {
		return
	}
	if len(procIDs) > 0 {
		var procs []models.Process
		if err := db.Select("id", "arch_process_id").Where("id IN (?)", procIDs).Find(&procs).Error; err == nil {
			var archIDs []uint64
			for _, p := range procs {
				if p.ArchProcessID > 0 {
					archIDs = append(archIDs, p.ArchProcessID)
					c.ProcToArchID[p.ID] = p.ArchProcessID
				}
			}
			if len(archIDs) > 0 {
				var archProcs []models.ArchProcess
				if err := db.Select("id", "name").Where("id IN (?)", archIDs).Find(&archProcs).Error; err == nil {
					archMap := make(map[uint64]string)
					for _, ap := range archProcs {
						archMap[ap.ID] = ap.Name
					}
					for _, p := range procs {
						if name, ok := archMap[p.ArchProcessID]; ok {
							c.ProcessNames[p.ID] = name
							c.ArchProcessNames[p.ArchProcessID] = name
						}
					}
				}
			}
		}
	}

	if len(modIDs) > 0 {
		var mods []models.Module
		if err := db.Select("id", "name").Where("id IN (?)", modIDs).Find(&mods).Error; err == nil {
			for _, m := range mods {
				c.ModuleNames[m.ID] = m.Name
			}
		}
	}
}

// GetConnIndex 获取连接紧凑短编号
func (c *HydrationCache) GetConnIndex(connKey string) int {
	if idx, exists := c.ConnIndices[connKey]; exists {
		return idx
	}
	idx := c.nextConnIndex
	c.nextConnIndex++
	c.ConnIndices[connKey] = idx
	return idx
}

type headerMeta struct {
	PeerProcName     string `json:"peer_proc_name"`
	BizError         bool   `json:"biz_error"`
	BizStatusSummary string `json:"biz_status_summary"`
}

type headerWrapper struct {
	Meta *headerMeta `json:"meta"`
}

// HydrateRecord 将单条 TrafficRecord 转换为水合摘要
func (c *HydrationCache) HydrateRecord(r *models.TrafficRecord) TrafficRecordSummary {
	var meta headerMeta
	if len(r.HeadersJSON) > 0 {
		var hw headerWrapper
		if err := json.Unmarshal(r.HeadersJSON, &hw); err == nil && hw.Meta != nil {
			meta = *hw.Meta
		}
	}

	connIdx := c.GetConnIndex(r.ConnKey)
	procName := c.ProcessNames[r.ProcessID]
	archProcName := ""
	if r.ProcessID > 0 {
		if archID, ok := c.ProcToArchID[r.ProcessID]; ok {
			archProcName = c.ArchProcessNames[archID]
		}
		if archProcName == "" {
			archProcName = c.ArchProcessNames[r.ProcessID]
		}
	}

	var srcModIDStr, dstModIDStr *string
	var srcModName, dstModName string
	if r.SrcModuleID != nil && *r.SrcModuleID > 0 {
		s := strconv.FormatUint(*r.SrcModuleID, 10)
		srcModIDStr = &s
		srcModName = c.ModuleNames[*r.SrcModuleID]
	}
	if r.DstModuleID != nil && *r.DstModuleID > 0 {
		s := strconv.FormatUint(*r.DstModuleID, 10)
		dstModIDStr = &s
		dstModName = c.ModuleNames[*r.DstModuleID]
	}

	var rspCapturedStr string
	if r.RspCapturedAtNs != nil {
		rspCapturedStr = strconv.FormatInt(*r.RspCapturedAtNs, 10)
	}

	snoStr := r.SNO.String()
	statusCodeStr := r.StatusCode.String()

	return TrafficRecordSummary{
		ID:               strconv.FormatUint(r.ID, 10),
		SessionID:        strconv.FormatUint(r.SessionID, 10),
		SeqNo:            r.SeqNo,
		ProcessID:        strconv.FormatUint(r.ProcessID, 10),
		ProcessName:      procName,
		ArchProcessName:  archProcName,
		PeerProcName:     meta.PeerProcName,
		ConnKey:          r.ConnKey,
		ConnIndex:        connIdx,
		Direction:        r.Direction,
		ExchangeType:     r.ExchangeType,
		SrcAddr:          r.SrcAddr,
		DstAddr:          r.DstAddr,
		ReqCapturedAtNs:  strconv.FormatInt(r.ReqCapturedAtNs, 10),
		RspCapturedAtNs:  rspCapturedStr,
		SNO:              snoStr,
		ServiceName:      r.ServiceName,
		Operation:        r.Operation,
		LatencyUs:        r.LatencyUs,
		StatusCode:       statusCodeStr,
		BizError:         meta.BizError,
		BizStatusSummary: meta.BizStatusSummary,
		TraceID:          r.TraceID,
		SpanID:           r.SpanID,
		FatherSpanID:     r.FatherSpanID,
		SrcModuleID:      srcModIDStr,
		SrcModuleName:    srcModName,
		DstModuleID:      dstModIDStr,
		DstModuleName:    dstModName,
		DecodeStatus:     r.DecodeStatus,
		IsTruncated:      r.IsTruncated,
		IsReplayable:     r.IsReplayable,
		CreatedAt:        r.CreatedAt.Format(time.RFC3339),
	}
}

// HydrateRecords 批量水合
func HydrateRecords(db *gorm.DB, session *models.RecordingSession, records []models.TrafficRecord) []TrafficRecordSummary {
	cache := NewHydrationCache()
	if session != nil {
		cache.LoadFromSession(session)
	}

	var missingProcIDs []uint64
	var modIDs []uint64
	seenProc := make(map[uint64]bool)
	seenMod := make(map[uint64]bool)

	for _, r := range records {
		if r.ProcessID > 0 && !seenProc[r.ProcessID] {
			seenProc[r.ProcessID] = true
			if cache.ProcessNames[r.ProcessID] == "" {
				missingProcIDs = append(missingProcIDs, r.ProcessID)
			}
		}
		if r.SrcModuleID != nil && *r.SrcModuleID > 0 && !seenMod[*r.SrcModuleID] {
			seenMod[*r.SrcModuleID] = true
			modIDs = append(modIDs, *r.SrcModuleID)
		}
		if r.DstModuleID != nil && *r.DstModuleID > 0 && !seenMod[*r.DstModuleID] {
			seenMod[*r.DstModuleID] = true
			modIDs = append(modIDs, *r.DstModuleID)
		}
	}

	if len(missingProcIDs) > 0 || len(modIDs) > 0 {
		cache.LoadFromDB(db, missingProcIDs, modIDs)
	}

	res := make([]TrafficRecordSummary, len(records))
	for i := range records {
		res[i] = cache.HydrateRecord(&records[i])
	}
	return res
}
