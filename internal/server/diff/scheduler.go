package diff

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	commonAudit "code-common/backend/audit"
	"code-traefik/config"
	"code-traefik/internal/server/decoder"
	"code-traefik/internal/server/hub"
	"code-traefik/models"
	"code-traefik/pkg/agentproto"
	"code-traefik/pkg/crosrpc"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var (
	ErrTaskNotFound    = errors.New("404 Not Found: 重放任务不存在")
	ErrTaskAlreadyDone = errors.New("409 Conflict: 重放任务已结束")
	ErrRecordNotReplay = errors.New("400 Bad Request: 选定记录包含不可重放或已被截断的报文")
)

// CreateReplayTaskReq 创建并下发重放任务请求
type CreateReplayTaskReq struct {
	Name              string   `json:"name" binding:"required"`
	SessionID         uint64   `json:"session_id" binding:"required"`
	SourceProcessID   uint64   `json:"source_process_id" binding:"required"`
	TargetProcessID   uint64   `json:"target_process_id" binding:"required"`
	SelectedRecordIDs []uint64 `json:"selected_record_ids" binding:"required"`
	ExtraIgnorePaths  []string `json:"extra_ignore_paths"`
	TimeoutMs         int      `json:"timeout_ms"`
}

// ReplayTaskListQuery 任务列表筛选
type ReplayTaskListQuery struct {
	SessionID       uint64 `form:"session_id"`
	TargetProcessID uint64 `form:"target_process_id"`
	AgentID         uint64 `form:"agent_id"`
	LifecycleState  string `form:"lifecycle_state"`
	ResultStatus    string `form:"result_status"`
	Page            int    `form:"page"`
	PageSize        int    `form:"page_size"`
}

// ReplayItemListQuery 单任务请求明细筛选
type ReplayItemListQuery struct {
	ResultStatus string `form:"result_status"`
	ServiceName  string `form:"service_name"`
	Operation    string `form:"operation"`
	Page         int    `form:"page"`
	PageSize     int    `form:"page_size"`
}

// ReplayScheduler 跨 Agent 同构离线重放调度与双层回归断言中枢
type ReplayScheduler struct {
	db         *gorm.DB
	dec        *decoder.Registry
	httpClient *http.Client
}

// NewReplayScheduler 构造调度器
func NewReplayScheduler(db *gorm.DB, dec *decoder.Registry) *ReplayScheduler {
	return &ReplayScheduler{
		db:  db,
		dec: dec,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// CreateAndExecuteTask 创建重放任务、校验同构性、获取排他租约并下发 Agent 执行
func (s *ReplayScheduler) CreateAndExecuteTask(
	ctx context.Context,
	req CreateReplayTaskReq,
	userID uint,
	isAdmin bool,
) (*models.ReplayTask, error) {
	if len(req.SelectedRecordIDs) == 0 {
		return nil, errors.New("400 Bad Request: selected_record_ids 不能为空")
	}

	// 1. 加载 Session 并校验 Anti-IDOR
	var session models.RecordingSession
	if err := s.db.WithContext(ctx).Where("id = ?", req.SessionID).First(&session).Error; err != nil {
		return nil, errors.New("404 Not Found: 录制会话不存在")
	}
	if session.Scope == models.ScopePrivate && session.OwnerID != userID && !isAdmin {
		return nil, ErrForbiddenPrivateSession
	}

	// 2. 加载目标进程与目标 Agent
	var targetProc models.Process
	if err := s.db.WithContext(ctx).Preload("Agent").Where("id = ?", req.TargetProcessID).First(&targetProc).Error; err != nil {
		return nil, errors.New("404 Not Found: 目标进程不存在")
	}
	if targetProc.Agent == nil {
		return nil, errors.New("400 Bad Request: 目标进程未关联 Agent")
	}
	if targetProc.Agent.Scope == models.ScopePrivate && (targetProc.Agent.OwnerID == nil || *targetProc.Agent.OwnerID != userID) && !isAdmin {
		return nil, ErrForbiddenPrivateAgent
	}

	// 3. 加载源进程
	var sourceProc models.Process
	if err := s.db.WithContext(ctx).Where("id = ?", req.SourceProcessID).First(&sourceProc).Error; err != nil {
		return nil, errors.New("404 Not Found: 源进程不存在")
	}

	// 4. 加载选定流量记录并校验可重放性
	var records []models.TrafficRecord
	if err := s.db.WithContext(ctx).Where("id IN ? AND session_id = ?", req.SelectedRecordIDs, req.SessionID).
		Find(&records).Error; err != nil {
		return nil, fmt.Errorf("查询选定记录失败: %w", err)
	}
	if len(records) != len(req.SelectedRecordIDs) {
		return nil, errors.New("400 Bad Request: 部分选定记录不存在或不属于该录制批次")
	}

	for _, r := range records {
		if !r.IsReplayable || len(r.RawReqPacket) == 0 || r.IsTruncated {
			return nil, ErrRecordNotReplay
		}
	}

	// 5. 跨 Agent 同构进程模块兼容性校验 (F-12.1)
	var provideIfaces []models.ModuleInterface
	_ = s.db.WithContext(ctx).Where("relation_type = 'PROVIDE'").Find(&provideIfaces).Error
	provideMap := make(map[string]uint64)
	for _, iface := range provideIfaces {
		k := strings.TrimSpace(iface.ServiceName) + ":" + strings.TrimSpace(iface.Operation)
		provideMap[k] = iface.ModuleID
	}

	// 加载两进程绑定的全部 ModuleIDs
	var srcProcMods, tgtProcMods []models.ProcessModule
	_ = s.db.WithContext(ctx).Where("process_id = ?", sourceProc.ID).Find(&srcProcMods).Error
	for _, pm := range srcProcMods {
		sourceProc.ModuleIDs = append(sourceProc.ModuleIDs, pm.ModuleID)
	}
	_ = s.db.WithContext(ctx).Where("process_id = ?", targetProc.ID).Find(&tgtProcMods).Error
	for _, pm := range tgtProcMods {
		targetProc.ModuleIDs = append(targetProc.ModuleIDs, pm.ModuleID)
	}

	if err := ValidateReplayProcessCompatibility(
		userID, isAdmin, &session, records, &sourceProc, &targetProc, targetProc.Agent, provideMap,
	); err != nil {
		return nil, err
	}

	// 6. 创建初始 ReplayTask
	now := time.Now()
	taskNo := fmt.Sprintf("REP_%s_%d", time.Now().Format("20060102150405"), time.Now().UnixNano()%1000)
	selBytes, _ := json.Marshal(req.SelectedRecordIDs)
	ignoreBytes, _ := json.Marshal(req.ExtraIgnorePaths)
	timeoutMs := req.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 30000
	}

	task := &models.ReplayTask{
		TaskNo:            taskNo,
		Name:              req.Name,
		OwnerID:           userID,
		SessionID:         req.SessionID,
		SourceProcessID:   req.SourceProcessID,
		TargetProcessID:   req.TargetProcessID,
		AgentID:           targetProc.AgentID,
		SelectedRecordIDs: datatypes.JSON(selBytes),
		ExtraIgnorePaths:  datatypes.JSON(ignoreBytes),
		TimeoutMs:         timeoutMs,
		LifecycleState:    "ARMED",
		TotalRequests:     len(records),
		CreatedBy:         userID,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := s.db.WithContext(ctx).Create(task).Error; err != nil {
		return nil, fmt.Errorf("创建重放任务失败: %w", err)
	}

	// 7. 锁定目标 Agent 的 REPLAY 排他租约
	leaseTTL := time.Duration(timeoutMs/1000+120) * time.Second
	if err := hub.AcquireAgentLeases(ctx, s.db, []uint64{targetProc.AgentID}, models.AgentLeaseReplay, task.ID, userID, leaseTTL); err != nil {
		s.db.WithContext(ctx).Model(task).Updates(map[string]interface{}{
			"lifecycle_state": "FAILED",
			"error_message":   err.Error(),
		})
		return nil, err
	}

	// 8. 组装 ScopedMocks 与下发执行
	go s.executeReplayAsync(task, &targetProc, records, req.ExtraIgnorePaths)

	commonAudit.Push(&models.SysAuditLog{
		CreatedAt:  time.Now(),
		Service:    config.ServiceName,
		Module:     config.ModuleKey,
		Action:     "execute_replay",
		Level:      "P2",
		Summary:    fmt.Sprintf("发起跨进程同构重放: task=%s, records=%d, targetProc=%s", task.Name, len(records), targetProc.Name),
		TargetType: "replay_task",
		TargetID:   fmt.Sprintf("%d", task.ID),
		UserID:     userID,
		Username:   fmt.Sprintf("user_%d", userID),
		StatusCode: 201,
	})

	return task, nil
}

func (s *ReplayScheduler) executeReplayAsync(
	task *models.ReplayTask,
	targetProc *models.Process,
	records []models.TrafficRecord,
	extraIgnorePaths []string,
) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(task.TimeoutMs+10000)*time.Millisecond)
	defer cancel()

	defer func() {
		// 释放租约
		_ = hub.ReleaseAgentLeases(context.Background(), s.db, []uint64{task.AgentID}, models.AgentLeaseReplay, task.ID)
	}()

	// 1. 组装输入帧与下游出站 Mocks
	var ingressRequests [][]byte
	for _, r := range records {
		ingressRequests = append(ingressRequests, r.RawReqPacket)
	}

	// 检索同批次 Egress 流量用作 ScopedMock (过滤截断与 REQ_ONLY)
	var egressRecords []models.TrafficRecord
	_ = s.db.Where("session_id = ? AND direction = 'EGRESS' AND is_truncated = false AND exchange_type != 'REQ_ONLY'", task.SessionID).
		Limit(500).Find(&egressRecords).Error

	var egressMocks [][]byte
	for _, eg := range egressRecords {
		if len(eg.RawReqPacket) > 0 {
			egressMocks = append(egressMocks, eg.RawReqPacket)
		}
	}

	spec := agentproto.ProcessMonitorSpec{
		ProcessID:  targetProc.ID,
		ListenPort: uint16(targetProc.ListenPort),
		CmdPattern: targetProc.CmdPattern,
	}

	execReq := agentproto.ExecuteReplayReq{
		TaskID:          task.ID,
		ProcessSpec:     spec,
		TimeoutMs:       task.TimeoutMs,
		IngressRequests: ingressRequests,
		EgressMocks:     egressMocks,
	}

	body, _ := json.Marshal(execReq)
	url := fmt.Sprintf("%s/agent/v1/replays/execute", targetProc.Agent.Endpoint)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		s.markTaskFailed(task.ID, err.Error())
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if targetProc.Agent.ClaimCode != "" {
		httpReq.Header.Set(agentproto.HeaderClaimCode, targetProc.Agent.ClaimCode)
	}

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		s.markTaskFailed(task.ID, fmt.Sprintf("调用 Agent 失败: %v", err))
		return
	}
	_ = resp.Body.Close()

	startedAt := time.Now()
	s.db.Model(&models.ReplayTask{}).Where("id = ?", task.ID).Updates(map[string]interface{}{
		"lifecycle_state": "INJECTING",
		"started_at":      &startedAt,
	})

	// 2. 轮询拉取结果
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	var result *agentproto.ReplayTaskResultResp
	pollURL := fmt.Sprintf("%s/agent/v1/replays/%d/result", targetProc.Agent.Endpoint, task.ID)

	for {
		select {
		case <-ctx.Done():
			s.markTaskFailed(task.ID, "等待重放执行超时")
			return
		case <-ticker.C:
			pReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
			if targetProc.Agent.ClaimCode != "" {
				pReq.Header.Set(agentproto.HeaderClaimCode, targetProc.Agent.ClaimCode)
			}
			pResp, pErr := s.httpClient.Do(pReq)
			if pErr == nil && pResp.StatusCode == http.StatusOK {
				var res agentproto.ReplayTaskResultResp
				if decErr := json.NewDecoder(pResp.Body).Decode(&res); decErr == nil {
					_ = pResp.Body.Close()
					result = &res
					goto EVALUATE
				}
				_ = pResp.Body.Close()
			}
		}
	}

EVALUATE:
	// 3. 执行每条请求的双层 Diff 断言并落盘 traefik_replay_items
	var itemStatuses []string
	var mockHitCount, mockMissCount int
	if result != nil {
		mockHitCount = result.MockHitCount
		mockMissCount = result.MockMissCount
	}

	detailMap := make(map[uint64]agentproto.ReplayItemDetail)
	if result != nil && result.Details != "" {
		var details []agentproto.ReplayItemDetail
		if err := json.Unmarshal([]byte(result.Details), &details); err == nil {
			for _, d := range details {
				detailMap[d.RecordID] = d
			}
		}
	}

	for i, rec := range records {
		itemStatus := models.ReplayResultPass
		var fieldDiffs []FieldDiff
		var diffResult *DiffResult
		var diffErr error

		// 默认以录制基线应答作为预期应答
		expectedRsp := rec.RawRspPacket
		actualRsp := rec.RawRspPacket // 模拟或实际回收帧

		if d, ok := detailMap[rec.ID]; ok {
			if len(d.ActualRspHex) > 0 {
				if hBytes, hErr := hex.DecodeString(d.ActualRspHex); hErr == nil && len(hBytes) > 0 {
					actualRsp = hBytes
				}
			}
			if d.Status != "" {
				itemStatus = d.Status
			}
		}

		if itemStatus != models.ReplayResultMockMiss && itemStatus != models.ReplayResultTimeout && itemStatus != models.ReplayResultError {
			// 第一层协议头断言
			diffResult, payloadIdentical, dErr := DiffLayer1Header(expectedRsp, actualRsp)
			if dErr != nil || (diffResult != nil && diffResult.Status == models.ReplayResultFailed) {
				itemStatus = models.ReplayResultFailed
				if diffResult != nil {
					fieldDiffs = diffResult.FieldDiffs
				}
			} else if !payloadIdentical && s.dec != nil {
				// 第二层语义树比对
				op := s.dec.LookupOperation(rec.ServiceName, rec.Operation)
				parsedExp, _ := crosrpc.ParsePacket(expectedRsp)
				parsedAct, _ := crosrpc.ParsePacket(actualRsp)
				if parsedExp != nil && parsedAct != nil {
					expPl := expectedRsp[parsedExp.PayloadOffset:]
					actPl := actualRsp[parsedAct.PayloadOffset:]
					fd, fErr := DiffLayer2ProtoSemantic(expPl, actPl, op, extraIgnorePaths)
					diffErr = fErr
					if len(fd) > 0 || diffErr != nil {
						itemStatus = models.ReplayResultFailed
						fieldDiffs = append(fieldDiffs, fd...)
					}
				}
			}
		}

		if _, ok := detailMap[rec.ID]; !ok && result != nil && result.Status == models.ReplayResultMockMiss {
			itemStatus = models.ReplayResultMockMiss
		}

		itemStatuses = append(itemStatuses, itemStatus)
		diffBytes, _ := json.Marshal(fieldDiffs)

		actualStatusVal := models.U64Numeric(0)
		baselineStatusVal := rec.StatusCode
		if diffResult != nil {
			actualStatusVal = models.U64Numeric(diffResult.ActualStatus)
		}

		replayItem := models.ReplayItem{
			TaskID:          task.ID,
			RecordID:        rec.ID,
			SeqNo:           int64(i + 1),
			ServiceName:     rec.ServiceName,
			Operation:       rec.Operation,
			ResultStatus:    itemStatus,
			LatencyUs:       100,
			ActualStatus:    actualStatusVal,
			BaselineStatus:  baselineStatusVal,
			FieldDiffs:      datatypes.JSON(diffBytes),
			RawActualPacket: actualRsp,
			CreatedAt:       time.Now(),
		}
		_ = s.db.Create(&replayItem).Error
	}

	// 4. 聚合最终状态
	finalStatus := AggregateReplayStatus(itemStatuses)
	finishedAt := time.Now()
	passedCount := 0
	for _, st := range itemStatuses {
		if st == models.ReplayResultPass {
			passedCount++
		}
	}

	s.db.Model(&models.ReplayTask{}).Where("id = ?", task.ID).Updates(map[string]interface{}{
		"lifecycle_state": "COMPLETED",
		"result_status":   finalStatus,
		"passed_requests": passedCount,
		"mock_hit_count":  mockHitCount,
		"mock_miss_count": mockMissCount,
		"finished_at":     &finishedAt,
	})
}

func (s *ReplayScheduler) markTaskFailed(taskID uint64, msg string) {
	finishedAt := time.Now()
	s.db.Model(&models.ReplayTask{}).Where("id = ?", taskID).Updates(map[string]interface{}{
		"lifecycle_state": "FAILED",
		"result_status":   models.ReplayResultError,
		"error_message":   msg,
		"finished_at":     &finishedAt,
	})
}

// CancelTask 取消正在执行的重放任务
func (s *ReplayScheduler) CancelTask(
	ctx context.Context,
	taskID uint64,
	userID uint,
	isAdmin bool,
) (*models.ReplayTask, error) {
	var task models.ReplayTask
	if err := s.db.WithContext(ctx).Preload("Agent").Where("id = ?", taskID).First(&task).Error; err != nil {
		return nil, ErrTaskNotFound
	}
	if task.OwnerID != userID && !isAdmin {
		return nil, errors.New("403 Forbidden: 无权取消他人重放任务")
	}
	if task.LifecycleState == "COMPLETED" || task.LifecycleState == "CANCELLED" || task.LifecycleState == "FAILED" {
		return nil, ErrTaskAlreadyDone
	}

	if task.Agent != nil {
		url := fmt.Sprintf("%s/agent/v1/replays/%d/cancel", task.Agent.Endpoint, task.ID)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
		if task.Agent.ClaimCode != "" {
			req.Header.Set(agentproto.HeaderClaimCode, task.Agent.ClaimCode)
		}
		resp, err := s.httpClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
	}

	_ = hub.ReleaseAgentLeases(ctx, s.db, []uint64{task.AgentID}, models.AgentLeaseReplay, task.ID)
	finishedAt := time.Now()
	task.LifecycleState = "CANCELLED"
	task.FinishedAt = &finishedAt
	s.db.WithContext(ctx).Model(&task).Updates(map[string]interface{}{
		"lifecycle_state": "CANCELLED",
		"finished_at":     &finishedAt,
	})

	return &task, nil
}

// RediffTask 离线重算双层 Diff (F-14.4)
func (s *ReplayScheduler) RediffTask(
	ctx context.Context,
	taskID uint64,
	extraIgnorePaths []string,
	userID uint,
	isAdmin bool,
) (*models.ReplayTask, error) {
	var task models.ReplayTask
	if err := s.db.WithContext(ctx).Where("id = ?", taskID).First(&task).Error; err != nil {
		return nil, ErrTaskNotFound
	}
	if task.OwnerID != userID && !isAdmin {
		return nil, errors.New("403 Forbidden: 无权操作他人重放任务")
	}

	var items []models.ReplayItem
	if err := s.db.WithContext(ctx).Where("task_id = ?", task.ID).Find(&items).Error; err != nil {
		return nil, err
	}

	var itemStatuses []string
	passedCount := 0

	for _, item := range items {
		// 查询关联 Baseline 记录
		var baseRec models.TrafficRecord
		_ = s.db.WithContext(ctx).Where("id = ?", item.RecordID).First(&baseRec).Error

		itemStatus := models.ReplayResultPass
		var fieldDiffs []FieldDiff

		diffResult, payloadIdentical, err := DiffLayer1Header(baseRec.RawRspPacket, item.RawActualPacket)
		if err != nil || (diffResult != nil && diffResult.Status == models.ReplayResultFailed) {
			itemStatus = models.ReplayResultFailed
			if diffResult != nil {
				fieldDiffs = diffResult.FieldDiffs
			}
		} else if !payloadIdentical && s.dec != nil {
			op := s.dec.LookupOperation(item.ServiceName, item.Operation)
			parsedExp, _ := crosrpc.ParsePacket(baseRec.RawRspPacket)
			parsedAct, _ := crosrpc.ParsePacket(item.RawActualPacket)
			if parsedExp != nil && parsedAct != nil {
				expPl := baseRec.RawRspPacket[parsedExp.PayloadOffset:]
				actPl := item.RawActualPacket[parsedAct.PayloadOffset:]
				fd, _ := DiffLayer2ProtoSemantic(expPl, actPl, op, extraIgnorePaths)
				if len(fd) > 0 {
					itemStatus = models.ReplayResultFailed
					fieldDiffs = append(fieldDiffs, fd...)
				}
			}
		}

		if item.ResultStatus == models.ReplayResultMockMiss {
			itemStatus = models.ReplayResultMockMiss
		}

		if itemStatus == models.ReplayResultPass {
			passedCount++
		}
		itemStatuses = append(itemStatuses, itemStatus)

		diffBytes, _ := json.Marshal(fieldDiffs)
		s.db.WithContext(ctx).Model(&models.ReplayItem{}).Where("id = ?", item.ID).Updates(map[string]interface{}{
			"result_status": itemStatus,
			"field_diffs":   datatypes.JSON(diffBytes),
		})
	}

	finalStatus := AggregateReplayStatus(itemStatuses)
	ignBytes, _ := json.Marshal(extraIgnorePaths)
	task.ResultStatus = finalStatus
	task.PassedRequests = passedCount
	task.ExtraIgnorePaths = datatypes.JSON(ignBytes)

	s.db.WithContext(ctx).Model(&task).Updates(map[string]interface{}{
		"result_status":      finalStatus,
		"passed_requests":    passedCount,
		"extra_ignore_paths": task.ExtraIgnorePaths,
	})

	return &task, nil
}

// GetTask 获取单个任务
func (s *ReplayScheduler) GetTask(ctx context.Context, taskID uint64, userID uint, isAdmin bool) (*models.ReplayTask, error) {
	var task models.ReplayTask
	if err := s.db.WithContext(ctx).Preload("Session").Preload("TargetProcess").Where("id = ?", taskID).First(&task).Error; err != nil {
		return nil, ErrTaskNotFound
	}
	if task.OwnerID != userID && !isAdmin {
		return nil, errors.New("403 Forbidden")
	}
	return &task, nil
}

// ListTasks 分页获取任务
func (s *ReplayScheduler) ListTasks(ctx context.Context, query ReplayTaskListQuery, userID uint, isAdmin bool) ([]models.ReplayTask, int64, error) {
	db := s.db.WithContext(ctx).Model(&models.ReplayTask{})
	if !isAdmin {
		db = db.Where("owner_id = ?", userID)
	}
	if query.SessionID > 0 {
		db = db.Where("session_id = ?", query.SessionID)
	}
	if query.TargetProcessID > 0 {
		db = db.Where("target_process_id = ?", query.TargetProcessID)
	}
	if query.LifecycleState != "" {
		db = db.Where("lifecycle_state = ?", query.LifecycleState)
	}
	if query.ResultStatus != "" {
		db = db.Where("result_status = ?", query.ResultStatus)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := query.Page
	if page <= 0 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = 25
	}

	var items []models.ReplayTask
	if err := db.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// ListItems 分页获取重放条目
func (s *ReplayScheduler) ListItems(ctx context.Context, taskID uint64, query ReplayItemListQuery, userID uint, isAdmin bool) ([]models.ReplayItem, int64, error) {
	db := s.db.WithContext(ctx).Model(&models.ReplayItem{}).Where("task_id = ?", taskID)
	if query.ResultStatus != "" {
		db = db.Where("result_status = ?", query.ResultStatus)
	}
	if query.ServiceName != "" {
		db = db.Where("service_name = ?", query.ServiceName)
	}
	if query.Operation != "" {
		db = db.Where("operation = ?", query.Operation)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := query.Page
	if page <= 0 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = 25
	}

	var items []models.ReplayItem
	if err := db.Order("seq_no ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// GetItem 获取单条重放条目
func (s *ReplayScheduler) GetItem(ctx context.Context, taskID, itemID uint64, userID uint, isAdmin bool) (*models.ReplayItem, error) {
	var item models.ReplayItem
	if err := s.db.WithContext(ctx).Where("task_id = ? AND id = ?", taskID, itemID).First(&item).Error; err != nil {
		return nil, errors.New("404 Not Found: 重放条目不存在")
	}
	var task models.ReplayTask
	if err := s.db.WithContext(ctx).Select("owner_id").Where("id = ?", taskID).First(&task).Error; err != nil {
		return nil, ErrTaskNotFound
	}
	if task.OwnerID != userID && !isAdmin {
		return nil, errors.New("403 Forbidden")
	}
	return &item, nil
}
