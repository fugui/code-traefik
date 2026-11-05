package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	commonAudit "code-common/backend/audit"
	commonModels "code-common/backend/models"
	"code-traefik/config"
	"code-traefik/internal/server/diff"
	"code-traefik/internal/server/hub"
	"code-traefik/internal/server/topology"
	"code-traefik/models"
	"code-traefik/pkg/agentproto"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GateEvaluateReq CI/CD 自动化门禁评估入参 (ADR-14, M17, F-27)
type GateEvaluateReq struct {
	TargetArchProcessID uint64  `json:"target_arch_process_id" binding:"required"`
	EnvAgentID          uint64  `json:"env_agent_id" binding:"required"`
	GoldenSuiteID       *uint64 `json:"golden_suite_id,omitempty"`
	TimeoutSeconds      int     `json:"timeout_seconds"`
	WaitLeaseSeconds    *int    `json:"wait_lease_seconds"`
}

// GateFailedItemDTO 黄金套件回归失败用例明细
type GateFailedItemDTO struct {
	ItemID         uint64           `json:"item_id"`
	SeqNo          int64            `json:"seq_no"`
	ServiceName    string           `json:"service_name"`
	Operation      string           `json:"operation"`
	ResultStatus   string           `json:"result_status"`
	BaselineStatus uint64           `json:"baseline_status"`
	ActualStatus   uint64           `json:"actual_status"`
	FieldDiffs     []diff.FieldDiff `json:"field_diffs,omitempty"`
	ErrorMessage   string           `json:"error_message,omitempty"`
}

// GateEvaluateResp 门禁评估响应报文
type GateEvaluateResp struct {
	GateStatus     string                          `json:"gate_status"`
	TaskID         uint64                          `json:"task_id"`
	ViolationCount int                             `json:"violation_count"`
	PassedRequests int                             `json:"passed_requests"`
	TotalRequests  int                             `json:"total_requests"`
	Violations     []topology.ObservedViolationDTO `json:"violations,omitempty"`
	FailedItems    []GateFailedItemDTO             `json:"failed_items,omitempty"`
	MarkdownReport string                          `json:"markdown_report"`
}

// EvaluateGate 同步阻塞式自动化强阻断门禁评估 POST /api/gates/evaluate (CIOrAdminGuard)
func EvaluateGate(c *gin.Context) {
	var req GateEvaluateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数格式错误: " + err.Error()})
		return
	}

	if req.TargetArchProcessID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "target_arch_process_id 必填且大于 0"})
		return
	}
	if req.EnvAgentID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "env_agent_id 必填且大于 0"})
		return
	}

	// 1. 校验目标标准架构进程是否存在
	var archProc models.ArchProcess
	if err := models.DB.First(&archProc, req.TargetArchProcessID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "标准架构进程不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询架构进程失败"})
		return
	}

	// 2. 校验测试环境 Agent
	var agent models.Agent
	if err := models.DB.First(&agent, req.EnvAgentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "测试环境 Agent 不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询 Agent 失败"})
		return
	}

	if agent.Scope != models.ScopeShared {
		c.JSON(http.StatusForbidden, gin.H{"error": "CI 门禁评估仅允许指定团队共享 (SHARED) 环境 Agent"})
		return
	}

	if agent.Status != models.AgentStatusOnline {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"gate_status": "GATED_ENV_NOT_READY",
			"error":       "测试环境 Agent 处于离线状态",
			"message":     "测试环境 Agent 处于离线状态，请检查 Agent 节点心跳",
		})
		return
	}

	// 3. 校验目标 Agent 上是否挂载了对应的目标进程
	var targetProc models.Process
	if err := models.DB.Where("agent_id = ? AND arch_process_id = ?", req.EnvAgentID, req.TargetArchProcessID).First(&targetProc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "目标环境未挂载对应标准架构进程"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询目标进程挂载失败"})
		return
	}

	if targetProc.ProbeStatus == models.ProbeStatusWaitingProc || targetProc.ProbeStatus == models.ProbeStatusNeedReconnect || targetProc.NeedReconnect || targetProc.ProbeStatus == models.ProbeStatusDisarmed {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"gate_status": "GATED_ENV_NOT_READY",
			"error":       "目标进程未就绪 (WAITING_PROC 或 NEED_RECONNECT)，请检查环境进程状态",
			"message":     "目标进程未就绪 (WAITING_PROC 或 NEED_RECONNECT)，请检查环境进程状态",
		})
		return
	}

	// 4. 解析或定位黄金契约套件
	var suite models.GoldenSuite
	if req.GoldenSuiteID != nil && *req.GoldenSuiteID > 0 {
		if err := models.DB.Where("id = ?", *req.GoldenSuiteID).First(&suite).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": "SUITE_NOT_FOUND", "error": "指定的黄金契约套件不存在"})
			return
		}
	} else {
		if err := models.DB.Where("arch_process_id = ? AND status = 'ACTIVE'", req.TargetArchProcessID).
			Order("id DESC").First(&suite).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": "SUITE_NOT_FOUND", "error": "目标标准进程未配置生效中的黄金套件"})
			return
		}
	}

	var itemCount int64
	models.DB.Model(&models.GoldenSuiteItem{}).Where("suite_id = ?", suite.ID).Count(&itemCount)
	if itemCount == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "黄金套件中无可用用例"})
		return
	}

	// 5. 参数规约 (超时与租约等待)
	timeoutSec := req.TimeoutSeconds
	if timeoutSec <= 0 {
		timeoutSec = 60
	}
	if timeoutSec < 5 {
		timeoutSec = 5
	}
	if timeoutSec > 300 {
		timeoutSec = 300
	}

	waitLeaseSec := 60
	if req.WaitLeaseSeconds != nil {
		waitLeaseSec = *req.WaitLeaseSeconds
	}
	if waitLeaseSec < 0 {
		waitLeaseSec = 0
	}
	if waitLeaseSec > 300 {
		waitLeaseSec = 300
	}

	userID := uint(0)
	if uidVal, exists := c.Get("user_id"); exists {
		if uid, ok := uidVal.(uint); ok {
			userID = uid
		}
	}

	sched := GetGlobalReplayScheduler()
	if sched == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "重放调度器未就绪"})
		return
	}

	// 6. 执行同步阻塞式套件回归 (含排他租约有界排队等待)
	task, err := sched.ExecuteGoldenSuiteSync(
		c.Request.Context(),
		suite.ID,
		req.EnvAgentID,
		models.TriggerSourceCIGate,
		userID,
		true,
		timeoutSec*1000,
		waitLeaseSec,
	)
	if err != nil {
		if errors.Is(err, diff.ErrEnvBusy) {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"gate_status": "GATED_ENV_BUSY",
				"error":       "测试环境 Agent 排他租约等待超时",
				"message":     "测试环境 Agent 排他租约等待超时，请稍后重试",
			})
			return
		}
		if errors.Is(err, diff.ErrAgentOffline) {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"gate_status": "GATED_ENV_NOT_READY",
				"error":       "测试环境 Agent 处于离线状态",
			})
			return
		}
		var conflictErr *hub.LeaseConflictError
		if errors.As(err, &conflictErr) {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"gate_status": "GATED_ENV_BUSY",
				"error":       conflictErr.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("执行回归任务失败: %v", err)})
		return
	}

	// 7. 双重强阻断关卡判定
	// 第一关：架构防漂移强阻断 (422 GATED_VIOLATION)
	var observedCalls []agentproto.ObservedEgressCall
	if len(task.ObservedEgressCalls) > 0 {
		_ = json.Unmarshal(task.ObservedEgressCalls, &observedCalls)
	}
	violations, _ := topology.VerifyProcessObservedCalls(c.Request.Context(), models.DB, req.TargetArchProcessID, observedCalls)

	host := c.Request.Host
	if host == "" {
		host = "127.0.0.1:8091"
	}

	if len(violations) > 0 {
		report := renderMarkdownReport("GATED_VIOLATION", &archProc, &agent, &suite, task, violations, nil, host)
		commonAudit.SetAuditContext(
			c,
			"gate",
			"evaluate",
			commonModels.AuditLevelP2,
			fmt.Sprintf("门禁评估阻断 [GATED_VIOLATION]: 架构进程 %s 检出 %d 处未报备违规调用", archProc.Name, len(violations)),
			"gate_evaluation",
			config.ModuleKey,
			config.ServiceName,
			nil,
			map[string]interface{}{"gate_status": "GATED_VIOLATION", "task_id": task.ID, "violations": len(violations)},
		)
		c.JSON(http.StatusUnprocessableEntity, GateEvaluateResp{
			GateStatus:     "GATED_VIOLATION",
			TaskID:         task.ID,
			ViolationCount: len(violations),
			PassedRequests: task.PassedRequests,
			TotalRequests:  task.TotalRequests,
			Violations:     violations,
			FailedItems:    []GateFailedItemDTO{},
			MarkdownReport: report,
		})
		return
	}

	// 第二关：黄金契约套件防劣化强阻断 (422 GATED_BREAKING_DIFF)
	if task.ResultStatus != models.ReplayResultPass {
		var replayItems []models.ReplayItem
		_ = models.DB.Where("task_id = ? AND result_status <> ?", task.ID, models.ReplayResultPass).
			Order("seq_no ASC").Find(&replayItems).Error

		var failedItems []GateFailedItemDTO
		for _, it := range replayItems {
			var fds []diff.FieldDiff
			if len(it.FieldDiffs) > 0 {
				_ = json.Unmarshal(it.FieldDiffs, &fds)
			}
			failedItems = append(failedItems, GateFailedItemDTO{
				ItemID:         it.ID,
				SeqNo:          it.SeqNo,
				ServiceName:    it.ServiceName,
				Operation:      it.Operation,
				ResultStatus:   it.ResultStatus,
				BaselineStatus: uint64(it.BaselineStatus),
				ActualStatus:   uint64(it.ActualStatus),
				FieldDiffs:     fds,
			})
		}

		report := renderMarkdownReport("GATED_BREAKING_DIFF", &archProc, &agent, &suite, task, nil, failedItems, host)
		commonAudit.SetAuditContext(
			c,
			"gate",
			"evaluate",
			commonModels.AuditLevelP2,
			fmt.Sprintf("门禁评估阻断 [GATED_BREAKING_DIFF]: 黄金套件 %s 回归失败 (%d/%d)", suite.Code, len(failedItems), task.TotalRequests),
			"gate_evaluation",
			config.ModuleKey,
			config.ServiceName,
			nil,
			map[string]interface{}{"gate_status": "GATED_BREAKING_DIFF", "task_id": task.ID, "failed_items": len(failedItems)},
		)
		c.JSON(http.StatusUnprocessableEntity, GateEvaluateResp{
			GateStatus:     "GATED_BREAKING_DIFF",
			TaskID:         task.ID,
			ViolationCount: 0,
			PassedRequests: task.PassedRequests,
			TotalRequests:  task.TotalRequests,
			Violations:     []topology.ObservedViolationDTO{},
			FailedItems:    failedItems,
			MarkdownReport: report,
		})
		return
	}

	// 准出通过 (200 PASSED)
	report := renderMarkdownReport("PASSED", &archProc, &agent, &suite, task, nil, nil, host)
	commonAudit.SetAuditContext(
		c,
		"gate",
		"evaluate",
		commonModels.AuditLevelP2,
		fmt.Sprintf("门禁评估全量通过 [PASSED]: 架构进程 %s 黄金套件 %s 全部通过", archProc.Name, suite.Code),
		"gate_evaluation",
		config.ModuleKey,
		config.ServiceName,
		nil,
		map[string]interface{}{"gate_status": "PASSED", "task_id": task.ID, "total_requests": task.TotalRequests},
	)
	c.JSON(http.StatusOK, GateEvaluateResp{
		GateStatus:     "PASSED",
		TaskID:         task.ID,
		ViolationCount: 0,
		PassedRequests: task.PassedRequests,
		TotalRequests:  task.TotalRequests,
		MarkdownReport: report,
	})
}

// renderMarkdownReport 渲染 PR/MR 结构化 Markdown 门禁凭据报告 (ADR-14)
func renderMarkdownReport(
	gateStatus string,
	archProc *models.ArchProcess,
	agent *models.Agent,
	suite *models.GoldenSuite,
	task *models.ReplayTask,
	violations []topology.ObservedViolationDTO,
	failedItems []GateFailedItemDTO,
	host string,
) string {
	var sb strings.Builder
	debugURL := fmt.Sprintf("http://%s/traefik/debug?replay_task_id=%d", host, task.ID)

	switch gateStatus {
	case "PASSED":
		sb.WriteString("### 🛡️ Code-Traefik 契约门禁报告: ✅ 通过 (PASSED)\n")
		sb.WriteString(fmt.Sprintf("- **被测标准进程**: `%s` (ID: %d) | **验证环境**: `%s` | **黄金套件**: `%s`\n", archProc.Code, archProc.ID, agent.Name, suite.Name))
		sb.WriteString("- **架构一致性核验**: ✅ 0 Violation（实测出站依赖边全部合规报备）\n")
		sb.WriteString(fmt.Sprintf("- **黄金契约回归**: ✅ 全部 %d 笔事务回归通过 (0 Breaking Diff)\n", task.TotalRequests))
		sb.WriteString(fmt.Sprintf("> 🔗 [点击直达 Code-Traefik 事务分析台查看详情](%s)\n", debugURL))

	case "GATED_VIOLATION":
		sb.WriteString("### 🛡️ Code-Traefik 契约门禁报告: ❌ 阻断 (GATED_VIOLATION)\n")
		sb.WriteString(fmt.Sprintf("- **被测标准进程**: `%s` (ID: %d) | **验证环境**: `%s` | **黄金套件**: `%s`\n", archProc.Code, archProc.ID, agent.Name, suite.Name))
		sb.WriteString(fmt.Sprintf("- **架构一致性核验**: ❌ 检出 %d 处未报备违规出站调用（架构漂移阻断）\n", len(violations)))
		sb.WriteString("  | 未声明下游服务 | 接口方法 | 目标地址 | 调用次数 | 匹配状态 | 阻断说明 |\n")
		sb.WriteString("  | :--- | :--- | :--- | :--- | :--- | :--- |\n")
		for _, v := range violations {
			sb.WriteString(fmt.Sprintf("  | `%s` | `%s` | `%s` | %d | `%s` | %s |\n",
				v.ServiceName, v.Operation, v.DstAddr, v.CallCount, v.MatchStatus, v.Reason))
		}
		sb.WriteString(fmt.Sprintf("- **黄金契约回归**: ⚠️ 因第一关架构漂移违规已被直接强阻断\n"))
		sb.WriteString(fmt.Sprintf("> 🔗 [点击直达 Code-Traefik 事务分析台排查根因](%s)\n", debugURL))

	case "GATED_BREAKING_DIFF":
		sb.WriteString("### 🛡️ Code-Traefik 契约门禁报告: ❌ 阻断 (GATED_BREAKING_DIFF)\n")
		sb.WriteString(fmt.Sprintf("- **被测标准进程**: `%s` (ID: %d) | **验证环境**: `%s` | **黄金套件**: `%s`\n", archProc.Code, archProc.ID, agent.Name, suite.Name))
		sb.WriteString("- **架构一致性核验**: ✅ 0 Violation（实测出站依赖边全部合规）\n")
		sb.WriteString(fmt.Sprintf("- **黄金契约回归**: ❌ %d / %d 笔事务发生破坏性变更\n", len(failedItems), task.TotalRequests))
		sb.WriteString("  | 接口契约 | 字段路径 | 基线期望值 | 实测返回值 |\n")
		sb.WriteString("  | :--- | :--- | :--- | :--- |\n")
		for _, item := range failedItems {
			if len(item.FieldDiffs) > 0 {
				for _, fd := range item.FieldDiffs {
					sb.WriteString(fmt.Sprintf("  | `%s.%s` | `%s` | `%v` | `%v` |\n",
						item.ServiceName, item.Operation, fd.FieldPath, fd.Expected, fd.Actual))
				}
			} else {
				sb.WriteString(fmt.Sprintf("  | `%s.%s` | `status_code` | `%d` | `%d` |\n",
					item.ServiceName, item.Operation, item.BaselineStatus, item.ActualStatus))
			}
		}
		sb.WriteString(fmt.Sprintf("> 🔗 [点击直达 Code-Traefik 事务分析台排查根因](%s)\n", debugURL))
	}

	return sb.String()
}
