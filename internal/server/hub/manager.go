package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"code-traefik/config"
	"code-traefik/models"
	"code-traefik/pkg/agentproto"
	"gorm.io/gorm"
)

// AgentRegisterReq 注册 Agent 请求
type AgentRegisterReq struct {
	Name        string `json:"name"`
	Scope       string `json:"scope"`
	Endpoint    string `json:"endpoint"`
	ClaimCode   string `json:"claim_code"`
	Description string `json:"description"`
}

// AgentUpdateReq 更新 Agent 请求
type AgentUpdateReq struct {
	Name        *string `json:"name,omitempty"`
	Endpoint    *string `json:"endpoint,omitempty"`
	ClaimCode   *string `json:"claim_code,omitempty"`
	Description *string `json:"description,omitempty"`
}

// AgentListQuery 列表查询参数
type AgentListQuery struct {
	Scope    string `form:"scope"`
	Status   string `form:"status"`
	Keyword  string `form:"keyword"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

// ForceUnlockReq 强制解锁租约请求
type ForceUnlockReq struct {
	Reason string `json:"reason" binding:"required"`
}

// AgentEvent SSE 事件定义
type AgentEvent struct {
	Type      string      `json:"type"` // e.g. "STATUS_CHANGE", "LEASE_CHANGE", "DISCOVERED_PROCESSES"
	AgentID   uint64      `json:"agent_id"`
	Timestamp int64       `json:"timestamp"`
	Data      interface{} `json:"data"`
}

// Manager 控制面与 Agent 通信及租约调度中枢
type Manager struct {
	db               *gorm.DB
	cfg              *config.Config
	httpClient       *http.Client
	failureCounters  sync.Map // agentID (uint64) -> int (consecutive failures)
	eventSubscribers sync.Map // subscriberID -> chan AgentEvent
	subCounter       uint64
	subMutex         sync.Mutex
}

// NewManager 构造 Hub 管理器
func NewManager(db *gorm.DB, cfg *config.Config) *Manager {
	client := &http.Client{
		Timeout: HeartbeatTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// 严格禁止跟随 HTTP 重定向，防御 SSRF 绕过
			return http.ErrUseLastResponse
		},
	}
	return &Manager{
		db:         db,
		cfg:        cfg,
		httpClient: client,
	}
}

// RegisterAgent 注册并立即握手探活 Agent
func (m *Manager) RegisterAgent(ctx context.Context, req AgentRegisterReq, currentUserID uint, isAdmin bool) (*models.Agent, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New("Agent 名称不能为空")
	}

	scope := strings.ToUpper(strings.TrimSpace(req.Scope))
	if scope != models.ScopeShared && scope != models.ScopePrivate {
		scope = models.ScopeShared
	}

	// 权限核验：普通用户严禁创建公共 SHARED Agent (403 Forbidden)
	if scope == models.ScopeShared && !isAdmin {
		return nil, ErrAgentForbidden
	}

	devAllowLoopback := false
	if m.cfg != nil {
		devAllowLoopback = m.cfg.AgentHub.DevAllowLoopback
	}

	validEp, err := ValidateAgentEndpoint(req.Endpoint, devAllowLoopback)
	if err != nil {
		return nil, err
	}

	// 查重：名称与端点全局唯一
	var nameCount int64
	if err := m.db.Model(&models.Agent{}).Where("name = ?", name).Count(&nameCount).Error; err != nil {
		return nil, err
	}
	if nameCount > 0 {
		return nil, errors.New("Agent 名称已存在")
	}

	var epCount int64
	if err := m.db.Model(&models.Agent{}).Where("endpoint = ?", validEp).Count(&epCount).Error; err != nil {
		return nil, err
	}
	if epCount > 0 {
		return nil, errors.New("Agent 端点已存在")
	}

	// 立即向 Agent 发起握手探活请求，校验认领码与主协议版本
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, validEp+"/agent/v1/health", nil)
	if err != nil {
		return nil, err
	}
	if req.ClaimCode != "" {
		httpReq.Header.Set(agentproto.HeaderClaimCode, req.ClaimCode)
	}

	httpResp, err := m.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("连接 Agent 探针失败: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode == http.StatusForbidden {
		return nil, ErrAgentClaimCodeMismatch
	}
	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Agent 握手异常状态码: %d", httpResp.StatusCode)
	}

	var handshake agentproto.HandshakeResp
	if err := json.NewDecoder(httpResp.Body).Decode(&handshake); err != nil {
		return nil, fmt.Errorf("解析 Agent 握手载荷失败: %w", err)
	}

	// 校验协议版本主版本号必须为 "1.0"
	if handshake.ProtocolVersion != "" && !strings.HasPrefix(handshake.ProtocolVersion, "1.") {
		return nil, ErrAgentVersionMismatch
	}

	var ownerID *uint
	if scope == models.ScopePrivate {
		ownerID = &currentUserID
	}

	now := time.Now()
	agent := &models.Agent{
		Name:            name,
		Endpoint:        validEp,
		Scope:           scope,
		OwnerID:         ownerID,
		ClaimCode:       req.ClaimCode,
		Status:          models.AgentStatusOnline,
		BootID:          handshake.BootID,
		AgentVersion:    handshake.AgentVersion,
		ProtocolVersion: handshake.ProtocolVersion,
		KernelVersion:   handshake.KernelVersion,
		HasEbpfCap:      handshake.HasEbpfCap,
		CapDetail:       handshake.CapDetail,
		LastHeartbeatAt: &now,
		Description:     req.Description,
		CreatedBy:       currentUserID,
		UpdatedBy:       currentUserID,
	}

	if err := m.db.Create(agent).Error; err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "23505") {
			return nil, errors.New("Agent 名称或端点已存在")
		}
		return nil, err
	}

	m.BroadcastEvent(AgentEvent{
		Type:      "STATUS_CHANGE",
		AgentID:   agent.ID,
		Timestamp: now.Unix(),
		Data:      agent,
	})

	return agent, nil
}

// UpdateAgent 更新 Agent 信息
func (m *Manager) UpdateAgent(ctx context.Context, agentID uint64, req AgentUpdateReq, currentUserID uint, isAdmin bool) (*models.Agent, error) {
	var agent models.Agent
	if err := m.db.First(&agent, agentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAgentNotFound
		}
		return nil, err
	}

	// ScopeWriteGuard
	if agent.Scope == models.ScopeShared && !isAdmin {
		return nil, ErrAgentForbidden
	}
	if agent.Scope == models.ScopePrivate && !isAdmin && (agent.OwnerID == nil || *agent.OwnerID != currentUserID) {
		return nil, ErrAgentForbidden
	}

	updates := map[string]interface{}{
		"updated_by": currentUserID,
	}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, errors.New("Agent 名称不能为空")
		}
		if name != agent.Name {
			var count int64
			if err := m.db.Model(&models.Agent{}).Where("name = ? AND id != ?", name, agentID).Count(&count).Error; err != nil {
				return nil, err
			}
			if count > 0 {
				return nil, errors.New("Agent 名称已存在冲突")
			}
			updates["name"] = name
			agent.Name = name
		}
	}

	if req.Endpoint != nil {
		if agent.LeaseType != models.AgentLeaseNone {
			return nil, ErrAgentLeaseActive
		}
		devAllowLoopback := false
		if m.cfg != nil {
			devAllowLoopback = m.cfg.AgentHub.DevAllowLoopback
		}
		validEp, err := ValidateAgentEndpoint(*req.Endpoint, devAllowLoopback)
		if err != nil {
			return nil, err
		}
		if validEp != agent.Endpoint {
			var count int64
			if err := m.db.Model(&models.Agent{}).Where("endpoint = ? AND id != ?", validEp, agentID).Count(&count).Error; err != nil {
				return nil, err
			}
			if count > 0 {
				return nil, errors.New("Agent 访问端点已存在冲突")
			}
			updates["endpoint"] = validEp
			agent.Endpoint = validEp
		}
	}

	if req.ClaimCode != nil {
		updates["claim_code"] = *req.ClaimCode
		agent.ClaimCode = *req.ClaimCode
	}
	if req.Description != nil {
		updates["description"] = *req.Description
		agent.Description = *req.Description
	}

	if err := m.db.Model(&models.Agent{}).Where("id = ?", agentID).Updates(updates).Error; err != nil {
		return nil, err
	}

	return &agent, nil
}

// DeleteAgent 删除 Agent
func (m *Manager) DeleteAgent(ctx context.Context, agentID uint64, currentUserID uint, isAdmin bool) error {
	var agent models.Agent
	if err := m.db.First(&agent, agentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrAgentNotFound
		}
		return err
	}

	// ScopeWriteGuard
	if agent.Scope == models.ScopeShared && !isAdmin {
		return ErrAgentForbidden
	}
	if agent.Scope == models.ScopePrivate && !isAdmin && (agent.OwnerID == nil || *agent.OwnerID != currentUserID) {
		return ErrAgentForbidden
	}

	if agent.LeaseType != models.AgentLeaseNone {
		return ErrAgentLeaseActive
	}

	var procCount int64
	if err := m.db.Model(&models.Process{}).Where("agent_id = ?", agentID).Count(&procCount).Error; err != nil {
		return err
	}
	if procCount > 0 {
		return ErrAgentHasProcesses
	}

	return m.db.Delete(&agent).Error
}

// GetAgent 获取单个 Agent 详情
func (m *Manager) GetAgent(ctx context.Context, agentID uint64, currentUserID uint, isAdmin bool) (*models.Agent, error) {
	var agent models.Agent
	if err := m.db.Preload("Owner").Preload("LeaseHolder").First(&agent, agentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAgentNotFound
		}
		return nil, err
	}

	// ScopeReadGuard
	if !isAdmin && agent.Scope == models.ScopePrivate && (agent.OwnerID == nil || *agent.OwnerID != currentUserID) {
		return nil, ErrAgentForbidden
	}

	if agent.Owner != nil {
		agent.Owner.Email = ""
	}
	if agent.LeaseHolder != nil {
		agent.LeaseHolder.Email = ""
	}

	return &agent, nil
}

// ListAgents 分页查询 Agent 列表
func (m *Manager) ListAgents(ctx context.Context, query AgentListQuery, currentUserID uint, isAdmin bool) ([]models.Agent, int64, error) {
	dbQuery := m.db.Model(&models.Agent{}).Preload("Owner").Preload("LeaseHolder")

	if !isAdmin {
		dbQuery = dbQuery.Where("scope = ? OR (scope = ? AND owner_id = ?)", models.ScopeShared, models.ScopePrivate, currentUserID)
	} else if query.Scope != "" {
		dbQuery = dbQuery.Where("scope = ?", query.Scope)
	}

	if query.Status != "" {
		dbQuery = dbQuery.Where("status = ?", query.Status)
	}
	if query.Keyword != "" {
		k := "%" + query.Keyword + "%"
		dbQuery = dbQuery.Where("name ILIKE ? OR endpoint ILIKE ?", k, k)
	}

	var total int64
	if err := dbQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := query.Page
	if page < 1 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = 25
	}

	var items []models.Agent
	offset := (page - 1) * pageSize
	if err := dbQuery.Order("id ASC").Offset(offset).Limit(pageSize).Find(&items).Error; err != nil {
		return nil, 0, err
	}

	for i := range items {
		if items[i].Owner != nil {
			items[i].Owner.Email = ""
		}
		if items[i].LeaseHolder != nil {
			items[i].LeaseHolder.Email = ""
		}
	}

	return items, total, nil
}

// HandshakeAgent 主动探测并对齐 Agent 状态
func (m *Manager) HandshakeAgent(ctx context.Context, agentID uint64, currentUserID uint, isAdmin bool) (*agentproto.HandshakeResp, error) {
	agent, err := m.GetAgent(ctx, agentID, currentUserID, isAdmin)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, agent.Endpoint+"/agent/v1/health", nil)
	if err != nil {
		return nil, err
	}
	if agent.ClaimCode != "" {
		httpReq.Header.Set(agentproto.HeaderClaimCode, agent.ClaimCode)
	}
	if agent.LeaseType != "" {
		httpReq.Header.Set(agentproto.HeaderExpectedLeaseType, agent.LeaseType)
		if agent.LeaseRefID != nil {
			httpReq.Header.Set(agentproto.HeaderExpectedLeaseRef, strconv.FormatUint(*agent.LeaseRefID, 10))
		}
	}

	httpResp, err := m.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agent handshake returned status %d", httpResp.StatusCode)
	}

	var handshake agentproto.HandshakeResp
	if err := json.NewDecoder(httpResp.Body).Decode(&handshake); err != nil {
		return nil, err
	}

	return &handshake, nil
}

// DiscoverProcesses 触发 Agent 进程发现
func (m *Manager) DiscoverProcesses(ctx context.Context, agentID uint64, currentUserID uint, isAdmin bool) ([]agentproto.DiscoveredProcess, error) {
	var agent models.Agent
	if err := m.db.First(&agent, agentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAgentNotFound
		}
		return nil, err
	}

	// ScopeReadGuard: SHARED 仅管理员可触发发现；PRIVATE 仅本人或管理员可触发
	if agent.Scope == models.ScopeShared && !isAdmin {
		return nil, ErrAgentForbidden
	}
	if agent.Scope == models.ScopePrivate && !isAdmin && (agent.OwnerID == nil || *agent.OwnerID != currentUserID) {
		return nil, ErrAgentForbidden
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, agent.Endpoint+"/agent/v1/processes/discover", nil)
	if err != nil {
		return nil, err
	}
	if agent.ClaimCode != "" {
		httpReq.Header.Set(agentproto.HeaderClaimCode, agent.ClaimCode)
	}

	httpResp, err := m.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discover processes returned status %d", httpResp.StatusCode)
	}

	var wrapper struct {
		Items []agentproto.DiscoveredProcess `json:"items"`
	}
	if err := json.NewDecoder(httpResp.Body).Decode(&wrapper); err != nil {
		return nil, err
	}

	return wrapper.Items, nil
}

// ProbeAgentOnce 单次探活单个 Agent 并执行租约与 boot_id 状态自愈对齐 (ADR-03)
func (m *Manager) ProbeAgentOnce(ctx context.Context, agentID uint64) error {
	var agent models.Agent
	if err := m.db.First(&agent, agentID).Error; err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, agent.Endpoint+"/agent/v1/health", nil)
	if err != nil {
		return err
	}
	if agent.ClaimCode != "" {
		httpReq.Header.Set(agentproto.HeaderClaimCode, agent.ClaimCode)
	}
	if agent.LeaseType != "" {
		httpReq.Header.Set(agentproto.HeaderExpectedLeaseType, agent.LeaseType)
		if agent.LeaseRefID != nil {
			httpReq.Header.Set(agentproto.HeaderExpectedLeaseRef, strconv.FormatUint(*agent.LeaseRefID, 10))
		}
	}

	httpResp, doErr := m.httpClient.Do(httpReq)

	now := time.Now()

	if doErr != nil || (httpResp != nil && httpResp.StatusCode != http.StatusOK) {
		if httpResp != nil {
			_ = httpResp.Body.Close()
		}

		// 失败计数递增
		val, _ := m.failureCounters.LoadOrStore(agentID, 0)
		fails := val.(int) + 1
		m.failureCounters.Store(agentID, fails)

		if fails >= MaxMissedHeartbeats {
			// 连续 3 次探活失败：标记 OFFLINE，释放租约，并将关联进行中的任务标记为 FAILED
			_ = m.db.Transaction(func(tx *gorm.DB) error {
				if agent.LeaseType == models.AgentLeaseRecording && agent.LeaseRefID != nil {
					_ = tx.Model(&models.RecordingSession{}).Where("id = ? AND status = ?", *agent.LeaseRefID, models.SessionStatusRecording).
						Updates(map[string]interface{}{
							"status":      models.SessionStatusFailed,
							"stop_reason": "Agent offline after 3 missed heartbeats",
						}).Error
				} else if agent.LeaseType == models.AgentLeaseReplay && agent.LeaseRefID != nil {
					_ = tx.Model(&models.ReplayTask{}).Where("id = ? AND lifecycle_state = 'RUNNING'", *agent.LeaseRefID).
						Updates(map[string]interface{}{
							"lifecycle_state": models.SessionStatusFailed,
							"error_message":   "Agent offline after 3 missed heartbeats",
						}).Error
				}

				return tx.Model(&models.Agent{}).Where("id = ?", agentID).Updates(map[string]interface{}{
					"status":            models.AgentStatusOffline,
					"lease_type":        models.AgentLeaseNone,
					"lease_ref_id":      nil,
					"lease_holder_id":   nil,
					"lease_acquired_at": nil,
					"lease_expires_at":  nil,
				}).Error
			})

			m.BroadcastEvent(AgentEvent{
				Type:      "STATUS_CHANGE",
				AgentID:   agentID,
				Timestamp: now.Unix(),
				Data:      map[string]string{"status": models.AgentStatusOffline},
			})
		}
		return nil
	}

	defer httpResp.Body.Close()
	m.failureCounters.Store(agentID, 0) // 重置失败计数

	var handshake agentproto.HandshakeResp
	if err := json.NewDecoder(httpResp.Body).Decode(&handshake); err != nil {
		return err
	}

	// 检测 Agent 实例重启自愈 (Reboot Detection)
	isRebooted := false
	if agent.BootID != "" && handshake.BootID != "" && handshake.BootID != agent.BootID {
		isRebooted = true
	}

	err = m.db.Transaction(func(tx *gorm.DB) error {
		updates := map[string]interface{}{
			"status":            models.AgentStatusOnline,
			"last_heartbeat_at": &now,
			"boot_id":           handshake.BootID,
			"agent_version":     handshake.AgentVersion,
			"protocol_version":  handshake.ProtocolVersion,
			"kernel_version":    handshake.KernelVersion,
			"has_ebpf_cap":      handshake.HasEbpfCap,
			"cap_detail":        handshake.CapDetail,
		}

		if isRebooted {
			// 重启导致内存状态丢失：终止可能存在的未决任务并清空租约
			if agent.LeaseType == models.AgentLeaseRecording && agent.LeaseRefID != nil {
				_ = tx.Model(&models.RecordingSession{}).Where("id = ? AND status = ?", *agent.LeaseRefID, models.SessionStatusRecording).
					Updates(map[string]interface{}{
						"status":      models.SessionStatusFailed,
						"stop_reason": "Agent 实例发生重启导致录制中断",
					}).Error
			} else if agent.LeaseType == models.AgentLeaseReplay && agent.LeaseRefID != nil {
				_ = tx.Model(&models.ReplayTask{}).Where("id = ? AND lifecycle_state = 'RUNNING'", *agent.LeaseRefID).
					Updates(map[string]interface{}{
						"lifecycle_state": models.SessionStatusFailed,
						"error_message":   "Agent 实例发生重启导致重放中断",
					}).Error
			}

			updates["lease_type"] = models.AgentLeaseNone
			updates["lease_ref_id"] = nil
			updates["lease_holder_id"] = nil
			updates["lease_acquired_at"] = nil
			updates["lease_expires_at"] = nil
		} else if agent.LeaseType != models.AgentLeaseNone {
			// 正常持锁且存活：滑窗续约
			exp := now.Add(DefaultLeaseTTL)
			updates["lease_expires_at"] = &exp
		}

		return tx.Model(&models.Agent{}).Where("id = ?", agentID).Updates(updates).Error
	})
	if err != nil {
		return err
	}

	return nil
}

// ProbeAllAgentsOnce 对全量注册 Agent 进行批量并发探活
func (m *Manager) ProbeAllAgentsOnce(ctx context.Context) error {
	var agents []models.Agent
	if err := m.db.Find(&agents).Error; err != nil {
		return err
	}

	var wg sync.WaitGroup
	for _, agt := range agents {
		wg.Add(1)
		go func(id uint64) {
			defer wg.Done()
			_ = m.ProbeAgentOnce(ctx, id)
		}(agt.ID)
	}
	wg.Wait()
	return nil
}

// StartHeartbeatLoop 启动后台心跳协程
func (m *Manager) StartHeartbeatLoop(ctx context.Context) {
	interval := 5 * time.Second
	if m.cfg != nil && m.cfg.AgentHub.HeartbeatIntervalSec > 0 {
		interval = time.Duration(m.cfg.AgentHub.HeartbeatIntervalSec) * time.Second
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = m.ProbeAllAgentsOnce(ctx)
			}
		}
	}()
}

// ServerRecoveryPipeline 服务启动恢复流水线：修复因主控崩溃留下的孤儿任务与租约
func (m *Manager) ServerRecoveryPipeline(ctx context.Context) error {
	// 查找所有持有活跃租约的 Agent
	var leasedAgents []models.Agent
	if err := m.db.Where("lease_type != ''").Find(&leasedAgents).Error; err != nil {
		return err
	}

	for _, agt := range leasedAgents {
		// 检查关联的 Session 或 Task 是否已结束或悬挂
		needPurge := false
		if agt.LeaseType == models.AgentLeaseRecording && agt.LeaseRefID != nil {
			var sess models.RecordingSession
			if err := m.db.First(&sess, *agt.LeaseRefID).Error; err != nil || sess.Status != models.SessionStatusRecording {
				needPurge = true
			}
		} else if agt.LeaseType == models.AgentLeaseReplay && agt.LeaseRefID != nil {
			var task models.ReplayTask
			if err := m.db.First(&task, *agt.LeaseRefID).Error; err != nil || task.LifecycleState != "RUNNING" {
				needPurge = true
			}
		} else {
			needPurge = true
		}

		if needPurge {
			_ = ForceUnlockAgentLease(ctx, m.db, agt.ID, 0, "主控启动异常恢复流水线清理孤儿租约")
		}
	}

	// 全量探活一次同步真实在线状态
	_ = m.ProbeAllAgentsOnce(ctx)
	return nil
}

// SubscribeEvents 订阅 Agent 状态 SSE 事件
func (m *Manager) SubscribeEvents() (<-chan AgentEvent, func()) {
	m.subMutex.Lock()
	m.subCounter++
	id := m.subCounter
	ch := make(chan AgentEvent, 32)
	m.eventSubscribers.Store(id, ch)
	m.subMutex.Unlock()

	cancel := func() {
		m.eventSubscribers.Delete(id)
		close(ch)
	}
	return ch, cancel
}

// BroadcastEvent 广播事件给所有订阅者
func (m *Manager) BroadcastEvent(event AgentEvent) {
	m.eventSubscribers.Range(func(key, value interface{}) bool {
		if ch, ok := value.(chan AgentEvent); ok {
			select {
			case ch <- event:
			default:
			}
		}
		return true
	})
}
