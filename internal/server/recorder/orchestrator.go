package recorder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	commonAudit "code-common/backend/audit"
	"code-traefik/config"
	"code-traefik/internal/server/hub"
	"code-traefik/models"
	"code-traefik/pkg/agentproto"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var (
	ErrForbiddenPrivateSession = errors.New("403 Forbidden: 无权访问私有录制批次")
	ErrForbiddenPrivateAgent   = errors.New("403 Forbidden: 包含无权访问的私有 Agent")
	ErrTargetProcessesEmpty    = errors.New("400 Bad Request: 目标进程列表不能为空")
	ErrSessionAlreadyEnded     = errors.New("409 Conflict: 录制批次已处于终态")
	ErrSessionNotFound         = errors.New("404 Not Found: 录制批次不存在")
	ErrSessionNotCompleted     = errors.New("400 Bad Request: 录制批次未完成，无法发布")
	ErrSessionInUse            = errors.New("409 Conflict: 录制批次正在录制中或被重放任务引用")
)

// StartSessionReq 创建协同录制请求
type StartSessionReq struct {
	Name             string   `json:"name" binding:"required"`
	Scope            string   `json:"scope"`
	TargetProcessIDs []uint64 `json:"target_process_ids" binding:"required"`
	MaxDurationSec   int      `json:"max_duration_sec"`
	MaxFrames        int      `json:"max_frames"`
	MaxFrameBytes    int      `json:"max_frame_bytes"`
	Description      string   `json:"description"`
}

// SessionListQuery 列表检索参数
type SessionListQuery struct {
	Scope     string `form:"scope"`
	Status    string `form:"status"`
	ProcessID uint64 `form:"process_id"`
	Search    string `form:"search"`
	Page      int    `form:"page"`
	PageSize  int    `form:"page_size"`
}

// Orchestrator 多进程协同录制中枢编排器
type Orchestrator struct {
	db         *gorm.DB
	assembler  *ExchangeAssembler
	httpClient *http.Client
	timers     sync.Map // sessionID (uint64) -> *time.Timer
	mu         sync.Mutex
}

// NewOrchestrator 构造 Orchestrator
func NewOrchestrator(db *gorm.DB, assembler *ExchangeAssembler) *Orchestrator {
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &Orchestrator{
		db:         db,
		assembler:  assembler,
		httpClient: client,
	}
}

// Assembler 返回绑定的 ExchangeAssembler
func (o *Orchestrator) Assembler() *ExchangeAssembler {
	return o.assembler
}

// StartSession 发起多进程协同录制
func (o *Orchestrator) StartSession(
	ctx context.Context,
	req StartSessionReq,
	userID uint,
	isAdmin bool,
) (*models.RecordingSession, error) {
	if len(req.TargetProcessIDs) == 0 {
		return nil, ErrTargetProcessesEmpty
	}

	scope := strings.ToUpper(strings.TrimSpace(req.Scope))
	if scope != models.ScopeShared && scope != models.ScopePrivate {
		scope = models.ScopePrivate
	}

	// 1. 查询目标进程及其所属 Agent
	var procs []models.Process
	if err := o.db.WithContext(ctx).Preload("Agent").
		Where("id IN ?", req.TargetProcessIDs).Find(&procs).Error; err != nil {
		return nil, fmt.Errorf("查询目标进程失败: %w", err)
	}
	if len(procs) == 0 {
		return nil, errors.New("404 Not Found: 指定目标进程不存在")
	}

	// 2. 权限校验与收集 Agent
	agentMap := make(map[uint64]*models.Agent)
	for _, p := range procs {
		if p.Agent == nil {
			return nil, fmt.Errorf("400 Bad Request: 进程 %s 未关联有效 Agent", p.Name)
		}
		if p.Agent.Scope == models.ScopePrivate && (p.Agent.OwnerID == nil || *p.Agent.OwnerID != userID) && !isAdmin {
			return nil, ErrForbiddenPrivateAgent
		}
		if p.Agent.Status != models.AgentStatusOnline {
			return nil, fmt.Errorf("400 Bad Request: 目标 Agent [%s] 处于离线状态", p.Agent.Name)
		}
		agentMap[p.AgentID] = p.Agent
	}

	var agentIDs []uint64
	for agtID := range agentMap {
		agentIDs = append(agentIDs, agtID)
	}
	// 严格按升序加锁防死锁
	sort.Slice(agentIDs, func(i, j int) bool { return agentIDs[i] < agentIDs[j] })

	// 3. 初始生成会话记录 (状态 PENDING)
	maxDur := req.MaxDurationSec
	if maxDur <= 0 {
		maxDur = 1800 // 默认 30 分钟
	}
	maxFrames := req.MaxFrames
	if maxFrames <= 0 {
		maxFrames = 100000
	}
	maxBytes := req.MaxFrameBytes
	if maxBytes <= 0 {
		maxBytes = 4194304 // 默认 4MB
	}

	now := time.Now()
	leasedBytes, _ := json.Marshal(agentIDs)
	targetProcBytes, _ := json.Marshal(req.TargetProcessIDs)
	sessionNo := fmt.Sprintf("REC_%s_%d", time.Now().Format("20060102150405"), time.Now().UnixNano()%1000)

	session := &models.RecordingSession{
		SessionNo:          sessionNo,
		Name:               req.Name,
		Scope:              scope,
		OwnerID:            userID,
		Status:             models.SessionStatusPending,
		LeasedAgentIDs:     datatypes.JSON(leasedBytes),
		TargetProcessIDs:   datatypes.JSON(targetProcBytes),
		ProcessDiagnostics: datatypes.JSON([]byte("[]")),
		MaxDurationSec:     maxDur,
		MaxFrames:          maxFrames,
		MaxFrameBytes:      maxBytes,
		CreatedBy:          userID,
		CreatedAt:          now,
		UpdatedAt:          now,
		Description:        req.Description,
	}

	if err := o.db.WithContext(ctx).Create(session).Error; err != nil {
		return nil, fmt.Errorf("创建录制会话失败: %w", err)
	}

	// 4. 原子升序获取涉及全部 Agent 的排他录制租约
	leaseTTL := time.Duration(maxDur+300) * time.Second
	if err := hub.AcquireAgentLeases(ctx, o.db, agentIDs, models.AgentLeaseRecording, session.ID, userID, leaseTTL); err != nil {
		// 加锁失败置状态为 FAILED
		o.db.WithContext(ctx).Model(session).Updates(map[string]interface{}{
			"status":      models.SessionStatusFailed,
			"stop_reason": err.Error(),
		})
		return nil, err
	}

	// 5. 并发向所有相关 Agent 下发 /agent/v1/monitors/sync
	var allDiags []agentproto.ProcessDiagStatus
	for agtID, agt := range agentMap {
		var specs []agentproto.ProcessMonitorSpec
		for _, p := range procs {
			if p.AgentID == agtID {
				specs = append(specs, agentproto.ProcessMonitorSpec{
					ProcessID:  p.ID,
					ListenPort: uint16(p.ListenPort),
					CmdPattern: p.CmdPattern,
				})
			}
		}

		syncReq := agentproto.SyncMonitorReq{
			SessionID: session.ID,
			Processes: specs,
		}
		diags, syncErr := o.callAgentSync(ctx, agt, syncReq)
		if syncErr != nil {
			log.Printf("[Orchestrator] 警告: 向 Agent [%s] 下发监控失败: %v", agt.Name, syncErr)
		} else {
			allDiags = append(allDiags, diags...)
		}
	}

	diagBytes, _ := json.Marshal(allDiags)
	startedAt := time.Now()
	session.Status = models.SessionStatusRecording
	session.StartedAt = &startedAt
	session.ProcessDiagnostics = datatypes.JSON(diagBytes)

	if err := o.db.WithContext(ctx).Model(session).Updates(map[string]interface{}{
		"status":              models.SessionStatusRecording,
		"started_at":          session.StartedAt,
		"process_diagnostics": session.ProcessDiagnostics,
	}).Error; err != nil {
		return nil, fmt.Errorf("更新录制会话状态失败: %w", err)
	}

	// 6. 注册汇聚器帧数上限与时长自动停录定时器
	if o.assembler != nil {
		o.assembler.SetSessionMaxFrames(session.ID, int64(session.MaxFrames))
	}

	timer := time.AfterFunc(time.Duration(session.MaxDurationSec)*time.Second, func() {
		_, _ = o.StopSession(context.Background(), session.ID, 0, true, "COMPLETED_BY_LIMIT")
	})
	o.timers.Store(session.ID, timer)

	// 7. 审计记录
	commonAudit.Push(&models.SysAuditLog{
		CreatedAt:  time.Now(),
		Service:    config.ServiceName,
		Module:     config.ModuleKey,
		Action:     "start_session",
		Level:      "P2",
		Summary:    fmt.Sprintf("启动录制会话: name=%s, scope=%s, agents=%v", session.Name, session.Scope, agentIDs),
		TargetType: "recording_session",
		TargetID:   fmt.Sprintf("%d", session.ID),
		UserID:     userID,
		Username:   fmt.Sprintf("user_%d", userID),
		StatusCode: 200,
	})

	return session, nil
}

// StopSession 停止协同录制并释放租约
func (o *Orchestrator) StopSession(
	ctx context.Context,
	sessionID uint64,
	userID uint,
	isAdmin bool,
	reason string,
) (*models.RecordingSession, error) {
	var session models.RecordingSession
	if err := o.db.WithContext(ctx).Where("id = ?", sessionID).First(&session).Error; err != nil {
		return nil, ErrSessionNotFound
	}

	if session.Scope == models.ScopePrivate && session.OwnerID != userID && !isAdmin && userID != 0 {
		return nil, ErrForbiddenPrivateSession
	}

	if session.Status == models.SessionStatusCompleted ||
		session.Status == models.SessionStatusCompletedByLimit ||
		session.Status == models.SessionStatusFailed {
		return nil, ErrSessionAlreadyEnded
	}

	// 取消定时器
	if tVal, ok := o.timers.LoadAndDelete(sessionID); ok {
		tVal.(*time.Timer).Stop()
	}

	// 1. 解析参与 Agent 列表
	var agentIDs []uint64
	_ = json.Unmarshal(session.LeasedAgentIDs, &agentIDs)

	var agents []models.Agent
	if len(agentIDs) > 0 {
		_ = o.db.WithContext(ctx).Where("id IN ?", agentIDs).Find(&agents).Error
	}

	// 2. 并发向所有相关 Agent 发送 /stop
	for _, agt := range agents {
		o.callAgentStop(ctx, &agt)
	}

	// 3. 刷空 ExchangeAssembler 挂起帧
	if o.assembler != nil {
		o.assembler.RemoveSessionLimit(sessionID)
		o.assembler.DrainAllAsReqOnly()
		o.assembler.Flush()
	}

	// 4. 原子释放全部 Agent 排他租约
	if len(agentIDs) > 0 {
		_ = hub.ReleaseAgentLeases(ctx, o.db, agentIDs, models.AgentLeaseRecording, session.ID)
	}

	// 5. 统计总帧数与丢包
	var capturedCount int64
	o.db.WithContext(ctx).Model(&models.TrafficRecord{}).Where("session_id = ?", session.ID).Count(&capturedCount)

	finalStatus := models.SessionStatusCompleted
	if reason == "COMPLETED_BY_LIMIT" {
		finalStatus = models.SessionStatusCompletedByLimit
	}
	stoppedAt := time.Now()

	updates := map[string]interface{}{
		"status":          finalStatus,
		"stopped_at":      &stoppedAt,
		"stop_reason":     reason,
		"captured_frames": capturedCount,
	}

	if err := o.db.WithContext(ctx).Model(&session).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("更新停录终态失败: %w", err)
	}
	session.Status = finalStatus
	session.StoppedAt = &stoppedAt
	session.StopReason = reason
	session.CapturedFrames = capturedCount

	// 6. 审计日志
	commonAudit.Push(&models.SysAuditLog{
		CreatedAt:  time.Now(),
		Service:    config.ServiceName,
		Module:     config.ModuleKey,
		Action:     "stop_session",
		Level:      "P2",
		Summary:    fmt.Sprintf("停录会话完成: status=%s, reason=%s, frames=%d", finalStatus, reason, capturedCount),
		TargetType: "recording_session",
		TargetID:   fmt.Sprintf("%d", session.ID),
		UserID:     userID,
		Username:   fmt.Sprintf("user_%d", userID),
		StatusCode: 200,
	})

	return &session, nil
}

// RemoveProcessFromSession 录制中热移除某进程监控
func (o *Orchestrator) RemoveProcessFromSession(
	ctx context.Context,
	sessionID, processID uint64,
	userID uint,
	isAdmin bool,
) (*models.RecordingSession, error) {
	var session models.RecordingSession
	if err := o.db.WithContext(ctx).Where("id = ?", sessionID).First(&session).Error; err != nil {
		return nil, ErrSessionNotFound
	}
	if session.Scope == models.ScopePrivate && session.OwnerID != userID && !isAdmin {
		return nil, ErrForbiddenPrivateSession
	}

	var proc models.Process
	if err := o.db.WithContext(ctx).Preload("Agent").Where("id = ?", processID).First(&proc).Error; err != nil {
		return nil, errors.New("进程不存在")
	}

	// 向 Agent 发送 DELETE /monitors/:processId
	if proc.Agent != nil {
		url := fmt.Sprintf("%s/agent/v1/monitors/%d", proc.Agent.Endpoint, processID)
		req, _ := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
		if proc.Agent.ClaimCode != "" {
			req.Header.Set(agentproto.HeaderClaimCode, proc.Agent.ClaimCode)
		}
		resp, err := o.httpClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
	}

	// 更新 target_process_ids
	var targetIDs []uint64
	_ = json.Unmarshal(session.TargetProcessIDs, &targetIDs)
	var filtered []uint64
	for _, id := range targetIDs {
		if id != processID {
			filtered = append(filtered, id)
		}
	}
	newBytes, _ := json.Marshal(filtered)
	session.TargetProcessIDs = datatypes.JSON(newBytes)
	o.db.WithContext(ctx).Model(&session).Update("target_process_ids", session.TargetProcessIDs)

	return &session, nil
}

// PublishSession 将本人私有录制批次发布为团队公共素材
func (o *Orchestrator) PublishSession(
	ctx context.Context,
	sessionID uint64,
	userID uint,
	isAdmin bool,
) (*models.RecordingSession, error) {
	var session models.RecordingSession
	if err := o.db.WithContext(ctx).Where("id = ?", sessionID).First(&session).Error; err != nil {
		return nil, ErrSessionNotFound
	}
	if session.Scope == models.ScopePrivate && session.OwnerID != userID && !isAdmin {
		return nil, ErrForbiddenPrivateSession
	}
	if session.Status != models.SessionStatusCompleted && session.Status != models.SessionStatusCompletedByLimit {
		return nil, ErrSessionNotCompleted
	}

	session.Scope = models.ScopeShared
	if err := o.db.WithContext(ctx).Model(&session).Update("scope", models.ScopeShared).Error; err != nil {
		return nil, fmt.Errorf("发布素材失败: %w", err)
	}

	commonAudit.Push(&models.SysAuditLog{
		CreatedAt:  time.Now(),
		Service:    config.ServiceName,
		Module:     config.ModuleKey,
		Action:     "publish_session",
		Level:      "P2",
		Summary:    fmt.Sprintf("将录制素材 [%s] 发布为团队公共共享素材", session.Name),
		TargetType: "recording_session",
		TargetID:   fmt.Sprintf("%d", session.ID),
		UserID:     userID,
		Username:   fmt.Sprintf("user_%d", userID),
		StatusCode: 200,
	})

	return &session, nil
}

// DeleteSession 删除录制批次
func (o *Orchestrator) DeleteSession(
	ctx context.Context,
	sessionID uint64,
	userID uint,
	isAdmin bool,
) error {
	var session models.RecordingSession
	if err := o.db.WithContext(ctx).Where("id = ?", sessionID).First(&session).Error; err != nil {
		return ErrSessionNotFound
	}
	if session.Scope == models.ScopePrivate && session.OwnerID != userID && !isAdmin {
		return ErrForbiddenPrivateSession
	}
	if session.Scope == models.ScopeShared && !isAdmin {
		return errors.New("403 Forbidden: 仅管理员可删除团队公共素材")
	}
	if session.Status == models.SessionStatusRecording || session.Status == models.SessionStatusPending {
		return ErrSessionInUse
	}

	// 检查是否有重放任务引用
	var replayCount int64
	o.db.WithContext(ctx).Model(&models.ReplayTask{}).Where("session_id = ?", sessionID).Count(&replayCount)
	if replayCount > 0 {
		return errors.New("409 Conflict: 存在依赖该素材的重放任务，无法删除")
	}

	// 删除关联流量记录与会话
	_ = o.db.WithContext(ctx).Where("session_id = ?", sessionID).Delete(&models.TrafficRecord{}).Error
	if err := o.db.WithContext(ctx).Delete(&session).Error; err != nil {
		return fmt.Errorf("删除录制会话失败: %w", err)
	}

	commonAudit.Push(&models.SysAuditLog{
		CreatedAt:  time.Now(),
		Service:    config.ServiceName,
		Module:     config.ModuleKey,
		Action:     "delete_session",
		Level:      "P2",
		Summary:    fmt.Sprintf("删除录制素材: session_id=%d, name=%s", sessionID, session.Name),
		TargetType: "recording_session",
		TargetID:   fmt.Sprintf("%d", sessionID),
		UserID:     userID,
		Username:   fmt.Sprintf("user_%d", userID),
		StatusCode: 200,
	})
	return nil
}

// GetSession 获取单条会话
func (o *Orchestrator) GetSession(
	ctx context.Context,
	sessionID uint64,
	userID uint,
	isAdmin bool,
) (*models.RecordingSession, error) {
	var session models.RecordingSession
	if err := o.db.WithContext(ctx).Where("id = ?", sessionID).First(&session).Error; err != nil {
		return nil, ErrSessionNotFound
	}
	if session.Scope == models.ScopePrivate && session.OwnerID != userID && !isAdmin {
		return nil, ErrForbiddenPrivateSession
	}
	return &session, nil
}

// ListSessions 分页查询会话
func (o *Orchestrator) ListSessions(
	ctx context.Context,
	query SessionListQuery,
	userID uint,
	isAdmin bool,
) ([]models.RecordingSession, int64, error) {
	db := o.db.WithContext(ctx).Model(&models.RecordingSession{})

	// 权限过滤：非管理员只能查 SHARED 或本人 PRIVATE
	if !isAdmin {
		db = db.Where("scope = ? OR (scope = ? AND owner_id = ?)", models.ScopeShared, models.ScopePrivate, userID)
	}

	if query.Scope != "" {
		db = db.Where("scope = ?", query.Scope)
	}
	if query.Status != "" {
		db = db.Where("status = ?", query.Status)
	}
	if query.Search != "" {
		like := "%" + query.Search + "%"
		db = db.Where("name LIKE ? OR session_no LIKE ?", like, like)
	}
	if query.ProcessID > 0 {
		db = db.Where("target_process_ids::jsonb @> ?", fmt.Sprintf("[%d]", query.ProcessID))
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

	items := make([]models.RecordingSession, 0)
	if err := db.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (o *Orchestrator) callAgentSync(ctx context.Context, agt *models.Agent, req agentproto.SyncMonitorReq) ([]agentproto.ProcessDiagStatus, error) {
	body, _ := json.Marshal(req)
	url := fmt.Sprintf("%s/agent/v1/monitors/sync", agt.Endpoint)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if agt.ClaimCode != "" {
		httpReq.Header.Set(agentproto.HeaderClaimCode, agt.ClaimCode)
	}

	resp, err := o.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var result struct {
		Diagnostics []agentproto.ProcessDiagStatus `json:"diagnostics"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	return result.Diagnostics, nil
}

func (o *Orchestrator) callAgentStop(ctx context.Context, agt *models.Agent) {
	url := fmt.Sprintf("%s/agent/v1/monitors/stop", agt.Endpoint)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return
	}
	if agt.ClaimCode != "" {
		httpReq.Header.Set(agentproto.HeaderClaimCode, agt.ClaimCode)
	}
	resp, err := o.httpClient.Do(httpReq)
	if err == nil {
		_ = resp.Body.Close()
	}
}
