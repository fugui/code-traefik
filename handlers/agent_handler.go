package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	commonAuth "code-common/backend/auth"
	commonModels "code-common/backend/models"
	"code-traefik/config"
	"code-traefik/internal/server/hub"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var (
	GlobalHubManager *hub.Manager
	hubOnce          sync.Once
)

// GetHubManager 获取或懒加载全局 Hub 管理器
func GetHubManager() *hub.Manager {
	if GlobalHubManager != nil {
		return GlobalHubManager
	}
	hubOnce.Do(func() {
		GlobalHubManager = hub.NewManager(models.DB, &config.AppConfig)
	})
	if GlobalHubManager == nil {
		return hub.NewManager(models.DB, &config.AppConfig)
	}
	return GlobalHubManager
}

// ResetGlobalHubManager 供测试重置全局 Hub 管理器
func ResetGlobalHubManager(m *hub.Manager) {
	GlobalHubManager = m
}

// ListAgents GET /api/agents
func ListAgents(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	page, pageSize := parsePagination(c)
	scope := strings.ToUpper(strings.TrimSpace(c.Query("scope")))
	status := strings.ToUpper(strings.TrimSpace(c.Query("status")))
	leaseState := strings.ToUpper(strings.TrimSpace(c.Query("lease_state")))
	search := strings.TrimSpace(c.Query("search"))

	query := models.DB.Model(&models.Agent{}).Preload("Owner").Preload("LeaseHolder")

	// ScopeReadGuard: 普通用户仅能查看 SHARED 和本人 PRIVATE
	if !isAdmin {
		query = query.Where("scope = ? OR (scope = ? AND owner_id = ?)", models.ScopeShared, models.ScopePrivate, uc.UserID)
	} else if scope != "" {
		query = query.Where("scope = ?", scope)
	}

	if status != "" {
		query = query.Where("status = ?", status)
	}
	if leaseState == "LEASED" {
		query = query.Where("lease_type != ''")
	} else if leaseState == "IDLE" {
		query = query.Where("lease_type = ''")
	}
	if search != "" {
		s := "%" + search + "%"
		query = query.Where("name ILIKE ? OR endpoint ILIKE ?", s, s)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "统计 Agent 失败"})
		return
	}

	items := make([]models.Agent, 0)
	offset := (page - 1) * pageSize
	if err := query.Order("id ASC").Offset(offset).Limit(pageSize).Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询 Agent 失败"})
		return
	}

	for i := range items {
		if items[i].Owner != nil {
			items[i].Owner.Email = ""
		}
		if items[i].LeaseHolder != nil {
			items[i].LeaseHolder.Email = ""
		}
	}

	if len(items) > 0 {
		agentIDs := make([]uint64, len(items))
		for i, ag := range items {
			agentIDs[i] = ag.ID
		}
		type procCount struct {
			AgentID uint64
			Count   int
		}
		var counts []procCount
		if err := models.DB.Model(&models.Process{}).Select("agent_id, count(*) as count").Where("agent_id IN ?", agentIDs).Group("agent_id").Scan(&counts).Error; err == nil {
			countMap := make(map[uint64]int)
			for _, c := range counts {
				countMap[c.AgentID] = c.Count
			}
			for i := range items {
				items[i].ProcessesCount = countMap[items[i].ID]
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetAgent GET /api/agents/:id
func GetAgent(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 Agent ID"})
		return
	}

	var agent models.Agent
	if err := models.DB.Preload("Owner").Preload("LeaseHolder").First(&agent, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Agent 不存在"})
		return
	}

	// ScopeReadGuard
	if !isAdmin && agent.Scope == models.ScopePrivate && (agent.OwnerID == nil || *agent.OwnerID != uc.UserID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权访问他人的私有 Agent"})
		return
	}

	if agent.Owner != nil {
		agent.Owner.Email = ""
	}
	if agent.LeaseHolder != nil {
		agent.LeaseHolder.Email = ""
	}

	procs := make([]models.Process, 0)
	if err := models.DB.Preload("ArchProcess.Group").Preload("ArchProcess.Modules").
		Where("agent_id = ?", agent.ID).
		Order("effective_listen_port ASC, id ASC").
		Find(&procs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询 Agent 进程挂载失败: " + err.Error()})
		return
	}
	for i := range procs {
		procs[i].FillFromArchProcess()
	}
	agent.ProcessesCount = len(procs)

	c.JSON(http.StatusOK, gin.H{
		"agent":     agent,
		"processes": procs,
	})
}

// RegisterAgent POST /api/agents
func RegisterAgent(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	var req hub.AgentRegisterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
		return
	}

	mgr := GetHubManager()
	agent, err := mgr.RegisterAgent(c.Request.Context(), req, uc.UserID, isAdmin)
	if err != nil {
		if errors.Is(err, hub.ErrAgentForbidden) {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, hub.ErrAgentClaimCodeMismatch) {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, hub.ErrInvalidAgentEndpoint) || errors.Is(err, hub.ErrLoopbackDisallowed) ||
			errors.Is(err, hub.ErrMetadataEndpoint) || errors.Is(err, hub.ErrAgentPortOutOfRange) ||
			errors.Is(err, hub.ErrAgentVersionMismatch) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "已存在") {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "create", commonModels.AuditLevelP2, fmt.Sprintf("注册 Agent [%s]", agent.Name), "agent", agent.ID, agent.Name, nil, agent)
	c.JSON(http.StatusCreated, agent)
}

// UpdateAgent PUT /api/agents/:id
func UpdateAgent(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 Agent ID"})
		return
	}

	var req hub.AgentUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
		return
	}

	mgr := GetHubManager()
	updated, err := mgr.UpdateAgent(c.Request.Context(), id, req, uc.UserID, isAdmin)
	if err != nil {
		if errors.Is(err, hub.ErrAgentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, hub.ErrAgentForbidden) {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, hub.ErrAgentLeaseActive) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, hub.ErrInvalidAgentEndpoint) || errors.Is(err, hub.ErrLoopbackDisallowed) ||
			errors.Is(err, hub.ErrMetadataEndpoint) || errors.Is(err, hub.ErrAgentPortOutOfRange) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "冲突") {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "update", commonModels.AuditLevelP2, fmt.Sprintf("更新 Agent [%s]", updated.Name), "agent", updated.ID, updated.Name, nil, updated)
	c.JSON(http.StatusOK, updated)
}

// DeleteAgent DELETE /api/agents/:id
func DeleteAgent(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 Agent ID"})
		return
	}

	mgr := GetHubManager()
	err = mgr.DeleteAgent(c.Request.Context(), id, uc.UserID, isAdmin)
	if err != nil {
		if errors.Is(err, hub.ErrAgentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, hub.ErrAgentForbidden) {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, hub.ErrAgentHasProcesses) || errors.Is(err, hub.ErrAgentLeaseActive) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "delete", commonModels.AuditLevelP1, fmt.Sprintf("删除 Agent [%d]", id), "agent", id, strconv.FormatUint(id, 10), nil, nil)
	c.JSON(http.StatusOK, gin.H{"message": "Agent 已移除"})
}

// HandshakeAgent POST /api/agents/:id/handshake
func HandshakeAgent(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 Agent ID"})
		return
	}

	mgr := GetHubManager()
	resp, err := mgr.HandshakeAgent(c.Request.Context(), id, uc.UserID, isAdmin)
	if err != nil {
		if errors.Is(err, hub.ErrAgentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, hub.ErrAgentForbidden) {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	var agent models.Agent
	_ = models.DB.First(&agent, id).Error
	c.JSON(http.StatusOK, gin.H{
		"agent":     agent,
		"handshake": resp,
	})
}

// DiscoverAgentProcesses GET /api/agents/:id/discover-processes
func DiscoverAgentProcesses(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 Agent ID"})
		return
	}

	mgr := GetHubManager()
	items, err := mgr.DiscoverProcesses(c.Request.Context(), id, uc.UserID, isAdmin)
	if err != nil {
		if errors.Is(err, hub.ErrAgentNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, hub.ErrAgentForbidden) {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"items": items})
}

// ForceUnlockAgentLease POST /api/agents/:id/lease/force-unlock (AdminOnly)
func ForceUnlockAgentLease(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 Agent ID"})
		return
	}

	var req hub.ForceUnlockReq
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Reason) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "强制解锁必须填写 reason 原因说明"})
		return
	}

	var before models.Agent
	if err := models.DB.First(&before, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Agent 不存在"})
		return
	}

	if err := hub.ForceUnlockAgentLease(c.Request.Context(), models.DB, id, uc.UserID, req.Reason); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var after models.Agent
	_ = models.DB.First(&after, id).Error

	recordAudit(c, "force_unlock_lease", commonModels.AuditLevelP1, fmt.Sprintf("强制解锁 Agent [%s] 租约: %s", after.Name, req.Reason), "agent", after.ID, after.Name, before, after)
	c.JSON(http.StatusOK, gin.H{
		"message": "排他租约已强制释放",
		"agent":   after,
	})
}

// BatchProcessMountReq 批量全量进程挂载请求
type BatchProcessMountReq struct {
	ExpectedRevision uint64             `json:"expected_revision"`
	Mounts           []ProcessMountItem `json:"mounts"`
}

// ProcessMountItem 单个进程挂载项
type ProcessMountItem struct {
	ArchProcessID      uint64   `json:"arch_process_id" binding:"required"`
	OverrideListenPort *int     `json:"override_listen_port"`
	OverrideCmdPattern string   `json:"override_cmd_pattern"`
	AdvertiseIP        string   `json:"advertise_ip"`
	ExtraDownstreams   []string `json:"extra_downstreams"`
	ProbeDesiredState  string   `json:"probe_desired_state"`
	Description        string   `json:"description"`
}

// BatchUpdateProcessMounts PUT /api/agents/:id/process-mounts
// 批量全量挂载，要求携带 expected_revision 做乐观锁防冲突 (ADR-09, AC-M10-02)
func BatchUpdateProcessMounts(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 Agent ID"})
		return
	}

	var req BatchProcessMountReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法: " + err.Error()})
		return
	}

	var agent models.Agent
	if err := models.DB.First(&agent, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Agent 不存在"})
		return
	}

	// 权限核验
	if agent.Scope == models.ScopeShared && !isAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "普通用户无权修改公共 SHARED Agent 挂载"})
		return
	}
	if agent.Scope == models.ScopePrivate && !isAdmin && (agent.OwnerID == nil || *agent.OwnerID != uc.UserID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权操作他人私有 Agent 挂载"})
		return
	}

	// 乐观锁版本校验
	if agent.MountsRevision != req.ExpectedRevision {
		c.JSON(http.StatusConflict, gin.H{
			"error":            fmt.Sprintf("挂载版本冲突 (当前版本 %d != 预期版本 %d)，请刷新后重试", agent.MountsRevision, req.ExpectedRevision),
			"current_revision": agent.MountsRevision,
		})
		return
	}

	// 预校验 ArchProcess 与端口冲突
	seenArchPIDs := make(map[uint64]bool)
	seenPorts := make(map[int]bool)
	type preparedItem struct {
		mount         ProcessMountItem
		archProc      models.ArchProcess
		effectivePort int
		effectiveCmd  string
	}
	prepared := make([]preparedItem, 0, len(req.Mounts))

	for _, item := range req.Mounts {
		if seenArchPIDs[item.ArchProcessID] {
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("同一 Agent 上禁止重复挂载标准架构进程 (ID: %d)", item.ArchProcessID)})
			return
		}
		seenArchPIDs[item.ArchProcessID] = true

		var archProc models.ArchProcess
		if err := models.DB.First(&archProc, item.ArchProcessID).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("标准架构进程不存在 (ID: %d)", item.ArchProcessID)})
			return
		}

		effPort := archProc.DefaultListenPort
		if item.OverrideListenPort != nil {
			effPort = *item.OverrideListenPort
		}
		if effPort < 0 || effPort > 65535 {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("进程端口非法: %d", effPort)})
			return
		}

		effCmd := item.OverrideCmdPattern
		if effCmd == "" {
			effCmd = archProc.DefaultCmdPattern
		}
		if effPort == 0 && effCmd == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("纯客户端进程 (端口 0) 必须提供匹配特征以定位 PID (架构进程: %s)", archProc.Name)})
			return
		}

		if effPort > 0 {
			if seenPorts[effPort] {
				c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("当前挂载批次内部端口冲突: %d", effPort)})
				return
			}
			seenPorts[effPort] = true
		}

		prepared = append(prepared, preparedItem{
			mount:         item,
			archProc:      archProc,
			effectivePort: effPort,
			effectiveCmd:  effCmd,
		})
	}

	beforeAgent := agent

	// 事务执行挂载同步
	err = models.DB.Transaction(func(tx *gorm.DB) error {
		var existingProcs []models.Process
		if err := tx.Where("agent_id = ?", id).Find(&existingProcs).Error; err != nil {
			return err
		}

		existingMap := make(map[uint64]models.Process)
		for _, ep := range existingProcs {
			existingMap[ep.ArchProcessID] = ep
		}

		// 检查删除挂载的租约锁定
		for _, ep := range existingProcs {
			if !seenArchPIDs[ep.ArchProcessID] {
				// 检查是否有正在运行的录制/回放
				var activeRecording int64
				tx.Model(&models.RecordingSession{}).
					Where("status = ? AND target_process_ids::jsonb @> ?", models.SessionStatusRecording, fmt.Sprintf("[%d]", ep.ID)).
					Count(&activeRecording)
				if activeRecording > 0 {
					return fmt.Errorf("进程 [%s] 正在录制会话中，禁止删除挂载", ep.Name)
				}

				if err := tx.Delete(&ep).Error; err != nil {
					return err
				}
			}
		}

		// 批量插入或更新
		for _, prep := range prepared {
			desiredState := prep.mount.ProbeDesiredState
			if desiredState == "" {
				desiredState = models.ProbeDesiredStateArmed
			}

			extraBytes, _ := json.Marshal(prep.mount.ExtraDownstreams)
			if prep.mount.ExtraDownstreams == nil {
				extraBytes = []byte("[]")
			}

			if existing, ok := existingMap[prep.mount.ArchProcessID]; ok {
				// 更新
				existing.OverrideListenPort = prep.mount.OverrideListenPort
				existing.EffectiveListenPort = prep.effectivePort
				existing.OverrideCmdPattern = prep.mount.OverrideCmdPattern
				existing.AdvertiseIP = prep.mount.AdvertiseIP
				existing.ExtraDownstreams = datatypes.JSON(extraBytes)
				existing.ProbeDesiredState = desiredState
				if desiredState == models.ProbeDesiredStateDisarmed {
					existing.ProbeStatus = models.ProbeStatusDisarmed
				} else if existing.ProbeStatus == models.ProbeStatusDisarmed {
					existing.ProbeStatus = models.ProbeStatusArmed
				}
				existing.Description = prep.mount.Description
				existing.UpdatedBy = uc.UserID

				if err := tx.Save(&existing).Error; err != nil {
					return err
				}
			} else {
				// 新增
				newProc := models.Process{
					AgentID:             id,
					ArchProcessID:       prep.mount.ArchProcessID,
					OverrideListenPort:  prep.mount.OverrideListenPort,
					EffectiveListenPort: prep.effectivePort,
					OverrideCmdPattern:  prep.mount.OverrideCmdPattern,
					AdvertiseIP:         prep.mount.AdvertiseIP,
					ExtraDownstreams:    datatypes.JSON(extraBytes),
					ProbeDesiredState:   desiredState,
					ProbeStatus:         models.ProbeStatusArmed,
					Description:         prep.mount.Description,
					CreatedBy:           uc.UserID,
					UpdatedBy:           uc.UserID,
				}
				if desiredState == models.ProbeDesiredStateDisarmed {
					newProc.ProbeStatus = models.ProbeStatusDisarmed
				}
				if err := tx.Create(&newProc).Error; err != nil {
					return err
				}
			}
		}

		// 递增 MountsRevision
		agent.MountsRevision++
		if err := tx.Model(&models.Agent{}).Where("id = ?", id).
			Update("mounts_revision", agent.MountsRevision).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}

	// 触发 Single-Flight 预挂载对账
	go func(aid uint64) {
		_, _ = GetHubManager().AutoRearmAgent(context.Background(), aid)
	}(id)

	var updatedProcs []models.Process
	if err := models.DB.Preload("ArchProcess.Group").Preload("ArchProcess.Modules").
		Where("agent_id = ?", id).
		Order("effective_listen_port ASC, id ASC").
		Find(&updatedProcs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询更新后进程挂载失败: " + err.Error()})
		return
	}
	for i := range updatedProcs {
		updatedProcs[i].FillFromArchProcess()
	}

	if err := models.DB.First(&agent, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询 Agent 详情失败: " + err.Error()})
		return
	}
	recordAudit(c, "batch_process_mounts", commonModels.AuditLevelP2, fmt.Sprintf("批量挂载 Agent [%s] 进程 (共 %d 个)", agent.Name, len(updatedProcs)), "agent", agent.ID, agent.Name, beforeAgent, agent)

	c.JSON(http.StatusOK, gin.H{
		"agent":     agent,
		"processes": updatedProcs,
	})
}

// DisarmAllProbes POST /api/agents/:id/probes/disarm-all
// 一键卸载该 Agent 上全部进程探针 (ADR-09, AC-M10-02)
func DisarmAllProbes(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 Agent ID"})
		return
	}

	var agent models.Agent
	if err := models.DB.First(&agent, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Agent 不存在"})
		return
	}

	if agent.Scope == models.ScopeShared && !isAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "普通用户无权操作公共 SHARED Agent"})
		return
	}
	if agent.Scope == models.ScopePrivate && !isAdmin && (agent.OwnerID == nil || *agent.OwnerID != uc.UserID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权操作他人私有 Agent"})
		return
	}

	beforeAgent := agent

	err = models.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Process{}).Where("agent_id = ?", id).Updates(map[string]interface{}{
			"probe_desired_state": models.ProbeDesiredStateDisarmed,
			"probe_status":        models.ProbeStatusDisarmed,
			"diag_message":        "已一键批量卸载探针",
			"updated_by":          uc.UserID,
		}).Error; err != nil {
			return err
		}

		agent.MountsRevision++
		agent.ExpectedArmedDigest = ""
		return tx.Model(&models.Agent{}).Where("id = ?", id).Updates(map[string]interface{}{
			"mounts_revision":       agent.MountsRevision,
			"expected_armed_digest": "",
		}).Error
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "卸载全部探针失败: " + err.Error()})
		return
	}

	// 触发对账自愈 (下发空待命规格，让 Agent 用户态与内核探针平滑释放)
	go func(aid uint64) {
		_, _ = GetHubManager().AutoRearmAgent(context.Background(), aid)
	}(id)

	_ = models.DB.First(&agent, id).Error
	recordAudit(c, "disarm_all_probes", commonModels.AuditLevelP2, fmt.Sprintf("一键卸载 Agent [%s] 上全部进程探针", agent.Name), "agent", agent.ID, agent.Name, beforeAgent, agent)

	c.JSON(http.StatusOK, gin.H{
		"message": "已一键卸载全部探针",
		"agent":   agent,
	})
}
