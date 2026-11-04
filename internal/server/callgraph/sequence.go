package callgraph

import (
	"encoding/json"
	"fmt"
	"strconv"

	"code-traefik/models"
	"gorm.io/gorm"
)

// BuildTransactionSequence 构建单事务的时序泳道响应 DTO
func BuildTransactionSequence(
	db *gorm.DB,
	sessionID uint64,
	rootRecordID uint64,
	replayTaskID uint64,
) (*TransactionSequenceDTO, error) {
	// 1. 加载批次全部记录
	var records []models.TrafficRecord
	query := db.Model(&models.TrafficRecord{}).Where("session_id = ?", sessionID).Order("req_captured_at_ns ASC, seq_no ASC, id ASC")
	if err := query.Find(&records).Error; err != nil {
		return nil, fmt.Errorf("查询会话记录失败: %w", err)
	}

	var session models.RecordingSession
	_ = db.Where("id = ?", sessionID).First(&session).Error

	// 2. 建树并找到指定根事务
	trees := BuildTransactionTrees(records, &session, nil)
	var rootGroup *TransactionTreeGroupDTO
	for i := range trees {
		if trees[i].RootRecordID == strconv.FormatUint(rootRecordID, 10) {
			rootGroup = &trees[i]
			break
		}
	}
	if rootGroup == nil {
		return nil, fmt.Errorf("未找到根记录 ID=%d 对应的事务树", rootRecordID)
	}

	// 3. 收集该事务树中包含的全部记录
	recMap := make(map[uint64]*models.TrafficRecord)
	for i := range records {
		recMap[records[i].ID] = &records[i]
	}

	var treeRecords []models.TrafficRecord
	var collectNodes func(n *TransactionTreeNodeDTO)
	collectNodes = func(n *TransactionTreeNodeDTO) {
		id, _ := strconv.ParseUint(n.Record.ID, 10, 64)
		if r, ok := recMap[id]; ok {
			treeRecords = append(treeRecords, *r)
		}
		if n.StitchedIngressRecord != nil {
			sid, _ := strconv.ParseUint(n.StitchedIngressRecord.ID, 10, 64)
			if sr, ok := recMap[sid]; ok {
				treeRecords = append(treeRecords, *sr)
			}
		}
		for i := range n.Children {
			collectNodes(&n.Children[i])
		}
	}
	collectNodes(&rootGroup.Root)

	// 4. 分析参与者
	participantsMap := make(map[string]*SequenceParticipantDTO)
	callerTitle := "外部调用方"
	if rootGroup.Root.Record.PeerProcName != "" {
		callerTitle = rootGroup.Root.Record.PeerProcName
	}
	callerKey := "caller:client"
	participantsMap[callerKey] = &SequenceParticipantDTO{
		ParticipantKey: callerKey,
		Title:          callerTitle,
		Subtitle:       rootGroup.Root.Record.SrcAddr,
		Kind:           "EXTERNAL_CALLER",
	}

	for _, r := range treeRecords {
		pKey := fmt.Sprintf("proc:%d", r.ProcessID)
		if _, exists := participantsMap[pKey]; !exists {
			title := fmt.Sprintf("进程 #%d", r.ProcessID)
			if r.ProcessID > 0 {
				var proc models.Process
				if err := db.Select("id", "arch_process_id").Where("id = ?", r.ProcessID).First(&proc).Error; err == nil && proc.ArchProcessID > 0 {
					var ap models.ArchProcess
					if err := db.Select("id", "name").Where("id = ?", proc.ArchProcessID).First(&ap).Error; err == nil && ap.Name != "" {
						title = ap.Name
					}
				}
			}
			participantsMap[pKey] = &SequenceParticipantDTO{
				ParticipantKey: pKey,
				Title:          title,
				Subtitle:       r.DstAddr,
				Kind:           "MONITORED_PROCESS",
			}
		}
	}

	// 5. 加载可选的重放任务结果覆盖
	replayItemMap := make(map[uint64]*models.ReplayItem)
	if replayTaskID > 0 {
		var replayItems []models.ReplayItem
		if err := db.Where("task_id = ?", replayTaskID).Find(&replayItems).Error; err == nil {
			for i := range replayItems {
				replayItemMap[replayItems[i].RecordID] = &replayItems[i]
			}
		}
	}

	// 6. 生成泳道步骤
	var steps []SequenceStepDTO
	stepIdx := 1
	rootStartNs, _ := computeTimeSpan(&rootGroup.Root, recMap)

	var walkTree func(n *TransactionTreeNodeDTO, parentParticipant string)
	walkTree = func(n *TransactionTreeNodeDTO, parentParticipant string) {
		id, _ := strconv.ParseUint(n.Record.ID, 10, 64)
		rec := recMap[id]

		fromPart := parentParticipant
		toPart := fmt.Sprintf("proc:%d", rec.ProcessID)
		if rec.Direction == "EGRESS" {
			fromPart = fmt.Sprintf("proc:%d", rec.ProcessID)
			if n.StitchedIngressRecord != nil {
				sid, _ := strconv.ParseUint(n.StitchedIngressRecord.ID, 10, 64)
				srec := recMap[sid]
				toPart = fmt.Sprintf("proc:%d", srec.ProcessID)
			} else {
				toPart = fmt.Sprintf("downstream:%s", rec.ServiceName)
				if _, exists := participantsMap[toPart]; !exists {
					participantsMap[toPart] = &SequenceParticipantDTO{
						ParticipantKey: toPart,
						Title:          rec.ServiceName,
						Subtitle:       rec.DstAddr,
						Kind:           "EXTERNAL_DOWNSTREAM",
					}
				}
			}
		}

		offsetUs := (rec.ReqCapturedAtNs - rootStartNs) / 1000
		if offsetUs < 0 {
			offsetUs = 0
		}
		durationUs := int64(0)
		if rec.LatencyUs != nil {
			durationUs = *rec.LatencyUs
		}

		// 阶段耗时
		var stageTiming *SequenceStageTimingDTO
		if len(rec.HeadersJSON) > 0 {
			var hw struct {
				Timing *struct {
					NetworkAndFramingUs *int64 `json:"network_and_framing_us"`
					QueueWaitUs         *int64 `json:"queue_wait_us"`
					ServerExecUs        *int64 `json:"server_exec_us"`
				} `json:"timing"`
			}
			if err := json.Unmarshal(rec.HeadersJSON, &hw); err == nil && hw.Timing != nil {
				stageTiming = &SequenceStageTimingDTO{
					NetworkAndFramingUs: hw.Timing.NetworkAndFramingUs,
					QueueWaitUs:         hw.Timing.QueueWaitUs,
					ServerExecUs:        hw.Timing.ServerExecUs,
				}
			}
		}

		// 重放叠加
		var replayOverlay *SequenceReplayOverlayDTO
		if ri, ok := replayItemMap[rec.ID]; ok {
			diffCnt := 0
			if len(ri.FieldDiffs) > 0 {
				var diffList []interface{}
				if err := json.Unmarshal(ri.FieldDiffs, &diffList); err == nil {
					diffCnt = len(diffList)
				}
			}
			summary := ri.ErrorMessage
			if summary == "" && diffCnt > 0 {
				summary = fmt.Sprintf("%d 处字段差异", diffCnt)
			}
			replayOverlay = &SequenceReplayOverlayDTO{
				Status:    ri.ResultStatus,
				DiffCount: &diffCnt,
				Summary:   summary,
			}
		}

		step := SequenceStepDTO{
			StepIndex:             stepIdx,
			Record:                n.Record,
			StitchedIngressRecord: n.StitchedIngressRecord,
			FromParticipant:       fromPart,
			ToParticipant:         toPart,
			SiblingRelation:       n.SiblingRelation,
			OffsetUs:              offsetUs,
			DurationUs:            durationUs,
			StageTiming:           stageTiming,
			ReplayOverlay:         replayOverlay,
		}
		steps = append(steps, step)
		stepIdx++

		for i := range n.Children {
			walkTree(&n.Children[i], toPart)
		}
	}

	walkTree(&rootGroup.Root, callerKey)

	// 7. 计算血缘边与实体链
	lineages := ExtractParameterLineage(treeRecords)
	entityChains := BuildEntityChains(treeRecords)

	// 转换为列表
	participants := make([]SequenceParticipantDTO, 0, len(participantsMap))
	// 确保 caller 排在第一个
	if c, ok := participantsMap[callerKey]; ok {
		participants = append(participants, *c)
	}
	for k, p := range participantsMap {
		if k != callerKey {
			participants = append(participants, *p)
		}
	}

	return &TransactionSequenceDTO{
		RootRecordID:      rootGroup.RootRecordID,
		TraceID:           rootGroup.TraceID,
		TotalDurationUs:   rootGroup.TotalDurationUs,
		Participants:      participants,
		Steps:             steps,
		ParameterLineages: lineages,
		EntityChains:      entityChains,
	}, nil
}
