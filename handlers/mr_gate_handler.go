package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	commonAudit "code-common/backend/audit"
	commonAuth "code-common/backend/auth"
	commonModels "code-common/backend/models"
	"code-traefik/internal/server/decoder"
	"code-traefik/internal/server/gitclient"
	"code-traefik/internal/server/linter"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const maxWebhookBodyBytes = 4 * 1024 * 1024 // 4MB

// MRCheckRunDTO MR 质量门禁检查标准化输出 DTO
type MRCheckRunDTO struct {
	ID                   uint64                          `json:"id"`
	GitRepoID            uint64                          `json:"git_repo_id"`
	GitRepoName          string                          `json:"git_repo_name"`
	Provider             string                          `json:"provider"`
	BundleID             *uint64                         `json:"bundle_id,omitempty"`
	BundleName           string                          `json:"bundle_name"`
	TargetSchemaName     string                          `json:"target_schema_name"`
	Category             string                          `json:"category"`
	MRIID                int                             `json:"mr_iid"`
	EventPhase           string                          `json:"event_phase"`
	MRTitle              string                          `json:"mr_title"`
	MRURL                string                          `json:"mr_url"`
	SourceBranch         string                          `json:"source_branch"`
	TargetBranch         string                          `json:"target_branch"`
	HeadCommitSHA        string                          `json:"head_commit_sha"`
	TargetHeadSHA        string                          `json:"target_head_sha"`
	AuthorUsername       string                          `json:"author_username"`
	AuthorEmail          string                          `json:"author_email"`
	Status               string                          `json:"status"`
	CompilePassed        bool                            `json:"compile_passed"`
	LintErrorCount       int                             `json:"lint_error_count"`
	LintWarningCount     int                             `json:"lint_warning_count"`
	DuplicateGroupCount  int                             `json:"duplicate_group_count"`
	BreakingChangeCount  int                             `json:"breaking_change_count"`
	AffectedModulesCount int                             `json:"affected_modules_count"`
	ChangedProtoFiles    []string                        `json:"changed_proto_files"`
	CompileErrorText     string                          `json:"compile_error_text"`
	BreakingDetails      []decoder.BreakingRuleViolation `json:"breaking_details"`
	AffectedModules      []decoder.AffectedDependent     `json:"affected_modules"`
	CheckReportID        *uint64                         `json:"check_report_id,omitempty"`
	RemoteCommentID      string                          `json:"remote_comment_id"`
	AttemptCount         int                             `json:"attempt_count"`
	OverriddenBy         *uint                           `json:"overridden_by,omitempty"`
	OverriddenByName     string                          `json:"overridden_by_name,omitempty"`
	OverrideReason       string                          `json:"override_reason"`
	OverriddenAt         *time.Time                      `json:"overridden_at,omitempty"`
	DurationMs           int                             `json:"duration_ms"`
	StartedAt            *time.Time                      `json:"started_at,omitempty"`
	CompletedAt          *time.Time                      `json:"completed_at,omitempty"`
	CreatedAt            time.Time                       `json:"created_at"`
	UpdatedAt            time.Time                       `json:"updated_at"`
}

// MRCheckRunDetailDTO MR 门禁流水详情响应（含关联 CheckReport 的违规明细与 AST 重复结构体簇）
type MRCheckRunDetailDTO struct {
	MRCheckRunDTO
	Violations      []models.CheckViolation    `json:"violations"`
	DuplicateGroups []models.DuplicateGroupDTO `json:"duplicate_groups"`
}

func ensureDecoderRegistry(db *gorm.DB) *decoder.Registry {
	reg, err := GetDecoderRegistry()
	if err != nil {
		log.Printf("[mr-gate] 获取全局 ProtoRegistry 异常，尝试按当前 DB 重建: %v", err)
	}
	if reg != nil {
		return reg
	}
	if db == nil {
		return nil
	}
	rebuilt, rebuildErr := decoder.NewRegistry(db)
	if rebuildErr != nil {
		log.Printf("[mr-gate] 初始化 DecoderRegistry 失败: %v", rebuildErr)
		return nil
	}
	return rebuilt
}

// HandleGitMRWebhook 接收 GitLab / GitHub / Gitea Merge Request 或 Pull Request Webhook
// POST /api/webhooks/git-mr/:repoId 与兼容路由 POST /api/webhooks/git-mr
func HandleGitMRWebhook(c *gin.Context) {
	db := models.DB
	reg := ensureDecoderRegistry(db)
	// 机器高频 Webhook 豁免通用操作审计表刷屏
	commonAudit.Skip(c)

	rawBody, err := io.ReadAll(io.LimitReader(c.Request.Body, maxWebhookBodyBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取 Webhook 请求体失败"})
		return
	}
	if len(rawBody) > maxWebhookBodyBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Webhook 载荷超过 4MB 上限"})
		return
	}

	parsed, parseErr := parseUnifiedMRWebhookPayload(rawBody)
	if parseErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "解析 Webhook JSON 载荷失败: " + parseErr.Error()})
		return
	}

	repo, repoErr := resolveWebhookGitRepo(db, c.Param("repoId"), parsed)
	if repoErr != nil || repo == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "未找到匹配的 Git 仓库配置"})
		return
	}

	secret, decErr := repo.DecryptWebhookSecret()
	if decErr != nil || strings.TrimSpace(secret) == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":  "ERR_WEBHOOK_SIGNATURE_INVALID",
			"error": "该 Git 仓库未配置有效的 Webhook 签名密钥，拒绝未授权回调",
		})
		return
	}

	if !verifyWebhookConstantTimeSignature(repo.Provider, secret, c.Request.Header, rawBody) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":  "ERR_WEBHOOK_SIGNATURE_INVALID",
			"error": "Webhook 签名或 Token 常量时间校验未通过",
		})
		return
	}

	if parsed.IgnoreEvent {
		c.JSON(http.StatusOK, gin.H{
			"status": "ignored",
			"reason": parsed.IgnoreReason,
		})
		return
	}
	if parsed.MRIID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Webhook 载荷缺少有效的 mr_iid / pull_request number"})
		return
	}

	var bundles []models.GitProtoBundle
	q := db.Where("git_repo_id = ?", repo.ID)
	if parsed.BundleID != nil && *parsed.BundleID > 0 {
		q = q.Where("id = ?", *parsed.BundleID)
	}
	if err := q.Order("id ASC").Find(&bundles).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取仓库 Bundle 配置失败"})
		return
	}

	affectedBundles, matchErr := gitclient.FilterAffectedBundles(bundles, parsed.PatchFiles)
	if matchErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":  "PATH_TRAVERSAL_DENIED",
			"error": matchErr.Error(),
		})
		return
	}
	if len(affectedBundles) == 0 && len(bundles) > 0 && len(parsed.PatchFiles) == 0 {
		affectedBundles = bundles
	}
	if len(affectedBundles) == 0 {
		c.JSON(http.StatusAccepted, gin.H{
			"status":         "skipped_no_proto_bundle_matched",
			"check_run_ids":  []uint64{},
			"affected_count": 0,
		})
		return
	}

	runner := gitclient.NewMRGateRunner(db, reg, GetGlobalSSEBroker())
	checkRunIDs := make([]uint64, 0, len(affectedBundles))
	runsOut := make([]MRCheckRunDTO, 0, len(affectedBundles))
	allDeduplicated := true
	runAsync := strings.EqualFold(c.Query("async"), "true")

	for i := range affectedBundles {
		b := affectedBundles[i]
		execIn := gitclient.MRExecuteInput{
			Repo:           repo,
			Bundle:         &b,
			MRIID:          parsed.MRIID,
			EventPhase:     parsed.EventPhase,
			MRTitle:        parsed.MRTitle,
			MRURL:          parsed.MRURL,
			SourceBranch:   parsed.SourceBranch,
			TargetBranch:   parsed.TargetBranch,
			HeadCommitSHA:  parsed.HeadCommitSHA,
			TargetHeadSHA:  parsed.TargetHeadSHA,
			AuthorUsername: parsed.AuthorUsername,
			AuthorEmail:    parsed.AuthorEmail,
			PatchFiles:     parsed.PatchFiles,
		}

		run, existed, createErr := runner.CreateOrGetCheckRun(c.Request.Context(), execIn)
		if createErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "创建 MR 检查记录失败: " + createErr.Error()})
			return
		}
		checkRunIDs = append(checkRunIDs, run.ID)

		isTerminal := run.Status == models.MRCheckStatusPassed ||
			run.Status == models.MRCheckStatusWarn ||
			run.Status == models.MRCheckStatusFailed ||
			run.Status == models.MRCheckStatusOverridden
		if existed && isTerminal {
			runsOut = append(runsOut, toMRCheckRunDTO(db, *run, repo, &b))
			continue
		}

		allDeduplicated = false
		if runAsync {
			runIDCopy := run.ID
			patchesCopy := append([]gitclient.MRPatchFile{}, parsed.PatchFiles...)
			go func() {
				bgCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				if _, execErr := runner.ExecuteCheckRun(bgCtx, runIDCopy, patchesCopy, false); execErr != nil {
					log.Printf("[mr-gate] 异步执行 MRCheckRun #%d 失败: %v", runIDCopy, execErr)
				}
			}()
			runsOut = append(runsOut, toMRCheckRunDTO(db, *run, repo, &b))
		} else {
			executed, execErr := runner.ExecuteCheckRun(c.Request.Context(), run.ID, parsed.PatchFiles, false)
			if execErr != nil {
				log.Printf("[mr-gate] 执行 MRCheckRun #%d 异常: %v", run.ID, execErr)
			}
			if executed != nil {
				runsOut = append(runsOut, toMRCheckRunDTO(db, *executed, repo, &b))
			} else {
				runsOut = append(runsOut, toMRCheckRunDTO(db, *run, repo, &b))
			}
		}
	}

	c.JSON(http.StatusAccepted, gin.H{
		"status":        "accepted",
		"deduplicated":  allDeduplicated,
		"event_phase":   parsed.EventPhase,
		"check_run_ids": checkRunIDs,
		"runs":          runsOut,
	})
}

// ListMRCheckRuns 分页查询 MR 质量门禁检查流水列表 (GET /api/mr-check-runs)
func ListMRCheckRuns(c *gin.Context) {
	db := models.DB
	page, pageSize := parseMRPageParams(c)
	q := db.Model(&models.MRCheckRun{}).Preload("GitRepo").Preload("Bundle")

	if repoIDStr := strings.TrimSpace(c.Query("git_repo_id")); repoIDStr != "" {
		if repoID, err := strconv.ParseUint(repoIDStr, 10, 64); err == nil && repoID > 0 {
			q = q.Where("git_repo_id = ?", repoID)
		}
	}
	if bundleIDStr := strings.TrimSpace(c.Query("bundle_id")); bundleIDStr != "" {
		if bundleID, err := strconv.ParseUint(bundleIDStr, 10, 64); err == nil && bundleID > 0 {
			q = q.Where("bundle_id = ?", bundleID)
		}
	}
	if status := strings.ToUpper(strings.TrimSpace(c.Query("status"))); status != "" {
		q = q.Where("status = ?", status)
	}
	if phase := strings.ToUpper(strings.TrimSpace(c.Query("event_phase"))); phase != "" {
		q = q.Where("event_phase = ?", phase)
	}
	if mrIIDStr := strings.TrimSpace(c.Query("mr_iid")); mrIIDStr != "" {
		if mrIID, err := strconv.Atoi(mrIIDStr); err == nil && mrIID > 0 {
			q = q.Where("mr_iid = ?", mrIID)
		}
	}
	if kw := strings.TrimSpace(c.Query("q")); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("mr_title ILIKE ? OR source_branch ILIKE ? OR author_username ILIKE ? OR head_commit_sha ILIKE ?",
			like, like, like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询 MR 门禁流水总数失败"})
		return
	}

	var rows []models.MRCheckRun
	offset := (page - 1) * pageSize
	if err := q.Order("id DESC").Offset(offset).Limit(pageSize).Find(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询 MR 门禁流水列表失败"})
		return
	}

	items := make([]MRCheckRunDTO, 0, len(rows))
	for i := range rows {
		items = append(items, toMRCheckRunDTO(db, rows[i], rows[i].GitRepo, rows[i].Bundle))
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetMRCheckRunDetail 查询单次 MR 质量门禁检查详情 (GET /api/mr-check-runs/:id)
func GetMRCheckRunDetail(c *gin.Context) {
	db := models.DB
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的检查记录 ID"})
		return
	}

	var run models.MRCheckRun
	if err := db.Preload("GitRepo").Preload("Bundle").First(&run, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "MR 门禁检查记录不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取 MR 门禁检查记录失败"})
		return
	}

	baseDTO := toMRCheckRunDTO(db, run, run.GitRepo, run.Bundle)
	violations := make([]models.CheckViolation, 0)
	dupGroups := make([]models.DuplicateGroupDTO, 0)

	if run.CheckReportID != nil && *run.CheckReportID > 0 {
		if vErr := db.Where("report_id = ?", *run.CheckReportID).Order("id ASC").Find(&violations).Error; vErr == nil {
			dupGroups = linter.RebuildDuplicateGroupsFromViolations(violations)
		}
	}

	c.JSON(http.StatusOK, MRCheckRunDetailDTO{
		MRCheckRunDTO:   baseDTO,
		Violations:      violations,
		DuplicateGroups: dupGroups,
	})
}

// OverrideMRCheckRun 管理员强制豁免放行 MR 门禁 (POST /api/mr-check-runs/:id/override, AdminOnly)
// 强制红线：compile_passed == false 时返回 409 ERR_MR_OVERRIDE_FORBIDDEN_COMPILE_FAILED，成功豁免时记录 P1 级操作审计！
func OverrideMRCheckRun(c *gin.Context) {
	db := models.DB
	reg := ensureDecoderRegistry(db)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的检查记录 ID"})
		return
	}

	var req struct {
		OverrideReason string `json:"override_reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数格式不合法"})
		return
	}
	reason := strings.TrimSpace(req.OverrideReason)
	if utf8.RuneCountInString(reason) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":  "ERR_OVERRIDE_REASON_TOO_SHORT",
			"error": "override_reason 豁免原因不得少于 8 个字符",
		})
		return
	}

	uc := commonAuth.GetUserContext(c)
	var operatorID uint
	operatorName := "admin"
	if uc != nil {
		operatorID = uc.UserID
		if uc.Name != "" {
			operatorName = uc.Name
		} else if uc.Username != "" {
			operatorName = uc.Username
		}
	}

	runner := gitclient.NewMRGateRunner(db, reg, GetGlobalSSEBroker())
	updatedRun, beforeRun, overrideErr := runner.ApplyAdminOverride(c.Request.Context(), id, operatorID, operatorName, reason)
	if overrideErr != nil {
		switch {
		case errors.Is(overrideErr, gitclient.ErrMRCheckRunNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": overrideErr.Error()})
		case errors.Is(overrideErr, gitclient.ErrMROverrideForbiddenCompileFailed):
			c.JSON(http.StatusConflict, gin.H{
				"code":  "ERR_MR_OVERRIDE_FORBIDDEN_COMPILE_FAILED",
				"error": overrideErr.Error(),
			})
		case errors.Is(overrideErr, gitclient.ErrMROverrideInvalidState):
			c.JSON(http.StatusConflict, gin.H{
				"code":  "ERR_MR_OVERRIDE_INVALID_STATE",
				"error": overrideErr.Error(),
			})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "执行 Override 豁免失败: " + overrideErr.Error()})
		}
		return
	}

	targetLabel := fmt.Sprintf("MR !%d (Run #%d)", updatedRun.MRIID, updatedRun.ID)
	commonAudit.SetAuditContext(
		c,
		"traefik_mr_gate",
		"override_mr_gate",
		commonModels.AuditLevelP1,
		fmt.Sprintf("管理员 [%s] 强制豁免放行了 %s，原因：%s", operatorName, targetLabel, reason),
		"mr_check_run",
		strconv.FormatUint(updatedRun.ID, 10),
		targetLabel,
		map[string]any{
			"status":          beforeRun.Status,
			"compile_passed":  beforeRun.CompilePassed,
			"overridden_by":   beforeRun.OverriddenBy,
			"override_reason": beforeRun.OverrideReason,
		},
		map[string]any{
			"status":          updatedRun.Status,
			"compile_passed":  updatedRun.CompilePassed,
			"overridden_by":   updatedRun.OverriddenBy,
			"override_reason": updatedRun.OverrideReason,
			"overridden_at":   updatedRun.OverriddenAt,
		},
	)

	c.JSON(http.StatusOK, toMRCheckRunDTO(db, *updatedRun, updatedRun.GitRepo, updatedRun.Bundle))
}

// RerunMRCheckRun 管理员手动重跑 MR 四合一质量门禁 (POST /api/mr-check-runs/:id/rerun, AdminOnly)
func RerunMRCheckRun(c *gin.Context) {
	db := models.DB
	reg := ensureDecoderRegistry(db)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的检查记录 ID"})
		return
	}

	runner := gitclient.NewMRGateRunner(db, reg, GetGlobalSSEBroker())
	updatedRun, rerunErr := runner.ExecuteCheckRun(c.Request.Context(), id, nil, true)
	if rerunErr != nil {
		if errors.Is(rerunErr, gitclient.ErrMRCheckRunNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": rerunErr.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "重跑 MR 门禁失败: " + rerunErr.Error()})
		return
	}

	uc := commonAuth.GetUserContext(c)
	operatorName := "admin"
	if uc != nil && uc.Name != "" {
		operatorName = uc.Name
	} else if uc != nil && uc.Username != "" {
		operatorName = uc.Username
	}
	targetLabel := fmt.Sprintf("MR !%d (Run #%d)", updatedRun.MRIID, updatedRun.ID)
	commonAudit.SetAuditContext(
		c,
		"traefik_mr_gate",
		"rerun_mr_gate",
		commonModels.AuditLevelP2,
		fmt.Sprintf("管理员 [%s] 触发重跑 %s (第 %d 次)", operatorName, targetLabel, updatedRun.AttemptCount),
		"mr_check_run",
		strconv.FormatUint(updatedRun.ID, 10),
		targetLabel,
		nil,
		map[string]any{
			"status":        updatedRun.Status,
			"attempt_count": updatedRun.AttemptCount,
		},
	)

	c.JSON(http.StatusOK, toMRCheckRunDTO(db, *updatedRun, updatedRun.GitRepo, updatedRun.Bundle))
}

type unifiedMRWebhook struct {
	GitRepoID      uint64
	RepoURL        string
	BundleID       *uint64
	MRIID          int
	EventPhase     string
	MRTitle        string
	MRURL          string
	SourceBranch   string
	TargetBranch   string
	HeadCommitSHA  string
	TargetHeadSHA  string
	AuthorUsername string
	AuthorEmail    string
	PatchFiles     []gitclient.MRPatchFile
	IgnoreEvent    bool
	IgnoreReason   string
}

func parseUnifiedMRWebhookPayload(raw []byte) (*unifiedMRWebhook, error) {
	var payload struct {
		GitRepoID      uint64                  `json:"git_repo_id"`
		BundleID       *uint64                 `json:"bundle_id"`
		ObjectKind     string                  `json:"object_kind"`
		Action         string                  `json:"action"`
		EventPhase     string                  `json:"event_phase"`
		MRIID          int                     `json:"mr_iid"`
		Number         int                     `json:"number"`
		MRTitle        string                  `json:"mr_title"`
		MRURL          string                  `json:"mr_url"`
		SourceBranch   string                  `json:"source_branch"`
		TargetBranch   string                  `json:"target_branch"`
		HeadCommitSHA  string                  `json:"head_commit_sha"`
		TargetHeadSHA  string                  `json:"target_head_sha"`
		AuthorUsername string                  `json:"author_username"`
		AuthorEmail    string                  `json:"author_email"`
		PatchFiles     []gitclient.MRPatchFile `json:"patch_files"`
		ChangedFiles   []gitclient.MRPatchFile `json:"changed_files"`

		Project struct {
			WebURL     string `json:"web_url"`
			GitHTTPURL string `json:"git_http_url"`
			PathWithNS string `json:"path_with_namespace"`
		} `json:"project"`

		ObjectAttributes struct {
			IID          int    `json:"iid"`
			Title        string `json:"title"`
			URL          string `json:"url"`
			Action       string `json:"action"`
			State        string `json:"state"`
			SourceBranch string `json:"source_branch"`
			TargetBranch string `json:"target_branch"`
			MergeCommit  string `json:"merge_commit_sha"`
			LastCommit   struct {
				ID     string `json:"id"`
				Author struct {
					Name  string `json:"name"`
					Email string `json:"email"`
				} `json:"author"`
			} `json:"last_commit"`
		} `json:"object_attributes"`

		User struct {
			Username string `json:"username"`
			Name     string `json:"name"`
			Email    string `json:"email"`
		} `json:"user"`

		PullRequest struct {
			Number  int    `json:"number"`
			Title   string `json:"title"`
			HTMLURL string `json:"html_url"`
			State   string `json:"state"`
			Merged  bool   `json:"merged"`
			Head    struct {
				Ref string `json:"ref"`
				SHA string `json:"sha"`
			} `json:"head"`
			Base struct {
				Ref string `json:"ref"`
				SHA string `json:"sha"`
			} `json:"base"`
			User struct {
				Login string `json:"login"`
				Email string `json:"email"`
			} `json:"user"`
		} `json:"pull_request"`

		Repository struct {
			HTMLURL  string `json:"html_url"`
			CloneURL string `json:"clone_url"`
		} `json:"repository"`
	}

	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}

	out := &unifiedMRWebhook{
		GitRepoID:      payload.GitRepoID,
		BundleID:       payload.BundleID,
		MRIID:          payload.MRIID,
		MRTitle:        payload.MRTitle,
		MRURL:          payload.MRURL,
		SourceBranch:   payload.SourceBranch,
		TargetBranch:   payload.TargetBranch,
		HeadCommitSHA:  payload.HeadCommitSHA,
		TargetHeadSHA:  payload.TargetHeadSHA,
		AuthorUsername: payload.AuthorUsername,
		AuthorEmail:    payload.AuthorEmail,
		PatchFiles:     payload.PatchFiles,
	}
	if len(out.PatchFiles) == 0 && len(payload.ChangedFiles) > 0 {
		out.PatchFiles = payload.ChangedFiles
	}

	// GitLab Merge Request Event 适配
	if payload.ObjectAttributes.IID > 0 && out.MRIID == 0 {
		out.MRIID = payload.ObjectAttributes.IID
	}
	if payload.ObjectAttributes.Title != "" && out.MRTitle == "" {
		out.MRTitle = payload.ObjectAttributes.Title
	}
	if payload.ObjectAttributes.URL != "" && out.MRURL == "" {
		out.MRURL = payload.ObjectAttributes.URL
	}
	if payload.ObjectAttributes.SourceBranch != "" && out.SourceBranch == "" {
		out.SourceBranch = payload.ObjectAttributes.SourceBranch
	}
	if payload.ObjectAttributes.TargetBranch != "" && out.TargetBranch == "" {
		out.TargetBranch = payload.ObjectAttributes.TargetBranch
	}
	if payload.ObjectAttributes.LastCommit.ID != "" && out.HeadCommitSHA == "" {
		out.HeadCommitSHA = payload.ObjectAttributes.LastCommit.ID
	}
	if payload.User.Username != "" && out.AuthorUsername == "" {
		out.AuthorUsername = payload.User.Username
	}
	if payload.User.Email != "" && out.AuthorEmail == "" {
		out.AuthorEmail = payload.User.Email
	} else if payload.ObjectAttributes.LastCommit.Author.Email != "" && out.AuthorEmail == "" {
		out.AuthorEmail = payload.ObjectAttributes.LastCommit.Author.Email
	}
	if payload.Project.GitHTTPURL != "" {
		out.RepoURL = payload.Project.GitHTTPURL
	} else if payload.Project.WebURL != "" {
		out.RepoURL = payload.Project.WebURL
	}

	// GitHub / Gitea Pull Request Event 适配
	if payload.Number > 0 && out.MRIID == 0 {
		out.MRIID = payload.Number
	}
	if payload.PullRequest.Number > 0 && out.MRIID == 0 {
		out.MRIID = payload.PullRequest.Number
	}
	if payload.PullRequest.Title != "" && out.MRTitle == "" {
		out.MRTitle = payload.PullRequest.Title
	}
	if payload.PullRequest.HTMLURL != "" && out.MRURL == "" {
		out.MRURL = payload.PullRequest.HTMLURL
	}
	if payload.PullRequest.Head.Ref != "" && out.SourceBranch == "" {
		out.SourceBranch = payload.PullRequest.Head.Ref
	}
	if payload.PullRequest.Head.SHA != "" && out.HeadCommitSHA == "" {
		out.HeadCommitSHA = payload.PullRequest.Head.SHA
	}
	if payload.PullRequest.Base.Ref != "" && out.TargetBranch == "" {
		out.TargetBranch = payload.PullRequest.Base.Ref
	}
	if payload.PullRequest.Base.SHA != "" && out.TargetHeadSHA == "" {
		out.TargetHeadSHA = payload.PullRequest.Base.SHA
	}
	if payload.PullRequest.User.Login != "" && out.AuthorUsername == "" {
		out.AuthorUsername = payload.PullRequest.User.Login
	}
	if payload.PullRequest.User.Email != "" && out.AuthorEmail == "" {
		out.AuthorEmail = payload.PullRequest.User.Email
	}
	if out.RepoURL == "" {
		if payload.Repository.CloneURL != "" {
			out.RepoURL = payload.Repository.CloneURL
		} else if payload.Repository.HTMLURL != "" {
			out.RepoURL = payload.Repository.HTMLURL
		}
	}

	// 判定 EventPhase (MR_GATE vs MAIN_MERGE)
	explicitPhase := strings.ToUpper(strings.TrimSpace(payload.EventPhase))
	action := strings.ToLower(strings.TrimSpace(payload.Action))
	if action == "" {
		action = strings.ToLower(strings.TrimSpace(payload.ObjectAttributes.Action))
	}
	state := strings.ToLower(strings.TrimSpace(payload.ObjectAttributes.State))
	if state == "" {
		state = strings.ToLower(strings.TrimSpace(payload.PullRequest.State))
	}

	switch {
	case explicitPhase == models.MREventPhaseMainMerge:
		out.EventPhase = models.MREventPhaseMainMerge
	case explicitPhase == models.MREventPhaseGate:
		out.EventPhase = models.MREventPhaseGate
	case action == "merge" || action == "merged" || state == "merged" || (action == "closed" && payload.PullRequest.Merged):
		out.EventPhase = models.MREventPhaseMainMerge
	case action == "close" || (action == "closed" && !payload.PullRequest.Merged):
		out.IgnoreEvent = true
		out.IgnoreReason = "mr_closed_without_merge"
	default:
		out.EventPhase = models.MREventPhaseGate
	}

	return out, nil
}

func resolveWebhookGitRepo(db *gorm.DB, repoIDParam string, parsed *unifiedMRWebhook) (*models.GitRepo, error) {
	if trimmedID := strings.TrimSpace(repoIDParam); trimmedID != "" {
		id, err := strconv.ParseUint(trimmedID, 10, 64)
		if err != nil || id == 0 {
			return nil, errors.New("无效的 repoId")
		}
		var repo models.GitRepo
		if err := db.First(&repo, id).Error; err != nil {
			return nil, err
		}
		return &repo, nil
	}
	if parsed != nil && parsed.GitRepoID > 0 {
		var repo models.GitRepo
		if err := db.First(&repo, parsed.GitRepoID).Error; err == nil {
			return &repo, nil
		}
	}
	if parsed != nil && strings.TrimSpace(parsed.RepoURL) != "" {
		normTarget := normalizeRepoURLKey(parsed.RepoURL)
		var repos []models.GitRepo
		if err := db.Order("id ASC").Find(&repos).Error; err == nil {
			for i := range repos {
				if normalizeRepoURLKey(repos[i].RepoURL) == normTarget {
					return &repos[i], nil
				}
			}
		}
	}
	return nil, gitclient.ErrGitRepoNotFound
}

func normalizeRepoURLKey(raw string) string {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	trimmed = strings.TrimRight(trimmed, "/")
	trimmed = strings.TrimSuffix(trimmed, ".git")
	if u, err := url.Parse(trimmed); err == nil && u.Host != "" {
		return u.Host + "/" + strings.Trim(u.Path, "/")
	}
	return trimmed
}

func verifyWebhookConstantTimeSignature(provider, secret string, hdr http.Header, rawBody []byte) bool {
	cleanSecret := strings.TrimSpace(secret)
	if cleanSecret == "" {
		return false
	}

	verifyHMAC := func(sigHeader string) bool {
		sig := strings.TrimSpace(sigHeader)
		sig = strings.TrimPrefix(sig, "sha256=")
		gotBytes, err := hex.DecodeString(sig)
		if err != nil || len(gotBytes) == 0 {
			return false
		}
		mac := hmac.New(sha256.New, []byte(cleanSecret))
		mac.Write(rawBody)
		expected := mac.Sum(nil)
		return hmac.Equal(gotBytes, expected)
	}

	switch strings.ToUpper(strings.TrimSpace(provider)) {
	case models.GitProviderGitLab:
		token := hdr.Get("X-Gitlab-Token")
		if token == "" {
			return false
		}
		return subtle.ConstantTimeCompare([]byte(token), []byte(cleanSecret)) == 1
	case models.GitProviderGitHub:
		return verifyHMAC(hdr.Get("X-Hub-Signature-256"))
	case models.GitProviderGitea:
		if sig := hdr.Get("X-Gitea-Signature"); sig != "" {
			return verifyHMAC(sig)
		}
		return verifyHMAC(hdr.Get("X-Hub-Signature-256"))
	default:
		if token := hdr.Get("X-Gitlab-Token"); token != "" {
			return subtle.ConstantTimeCompare([]byte(token), []byte(cleanSecret)) == 1
		}
		if token := hdr.Get("X-Webhook-Token"); token != "" {
			return subtle.ConstantTimeCompare([]byte(token), []byte(cleanSecret)) == 1
		}
		return verifyHMAC(hdr.Get("X-Hub-Signature-256"))
	}
}

func toMRCheckRunDTO(db *gorm.DB, run models.MRCheckRun, repo *models.GitRepo, bundle *models.GitProtoBundle) MRCheckRunDTO {
	changed := make([]string, 0)
	if len(run.ChangedProtoFilesJSON) > 0 {
		if err := json.Unmarshal(run.ChangedProtoFilesJSON, &changed); err != nil {
			changed = make([]string, 0)
		}
	}
	breaking := make([]decoder.BreakingRuleViolation, 0)
	if len(run.BreakingDetailsJSON) > 0 {
		if err := json.Unmarshal(run.BreakingDetailsJSON, &breaking); err != nil {
			breaking = make([]decoder.BreakingRuleViolation, 0)
		}
	}
	affected := make([]decoder.AffectedDependent, 0)
	if len(run.AffectedModulesJSON) > 0 {
		if err := json.Unmarshal(run.AffectedModulesJSON, &affected); err != nil {
			affected = make([]decoder.AffectedDependent, 0)
		}
	}

	var repoName, provider string
	if repo != nil {
		repoName = repo.Name
		provider = repo.Provider
	}
	var bundleName, targetSchemaName, category string
	if bundle != nil {
		bundleName = bundle.BundleName
		targetSchemaName = bundle.TargetSchemaName
		category = bundle.Category
	}

	var overriddenByName string
	if run.OverriddenBy != nil && *run.OverriddenBy > 0 && db != nil {
		var u models.User
		if err := db.Select("id", "name", "username").First(&u, *run.OverriddenBy).Error; err == nil {
			if u.Name != "" {
				overriddenByName = u.Name
			} else {
				overriddenByName = u.Username
			}
		}
	}

	return MRCheckRunDTO{
		ID:                   run.ID,
		GitRepoID:            run.GitRepoID,
		GitRepoName:          repoName,
		Provider:             provider,
		BundleID:             run.BundleID,
		BundleName:           bundleName,
		TargetSchemaName:     targetSchemaName,
		Category:             category,
		MRIID:                run.MRIID,
		EventPhase:           run.EventPhase,
		MRTitle:              run.MRTitle,
		MRURL:                run.MRURL,
		SourceBranch:         run.SourceBranch,
		TargetBranch:         run.TargetBranch,
		HeadCommitSHA:        run.HeadCommitSHA,
		TargetHeadSHA:        run.TargetHeadSHA,
		AuthorUsername:       run.AuthorUsername,
		AuthorEmail:          run.AuthorEmail,
		Status:               run.Status,
		CompilePassed:        run.CompilePassed,
		LintErrorCount:       run.LintErrorCount,
		LintWarningCount:     run.LintWarningCount,
		DuplicateGroupCount:  run.DuplicateGroupCount,
		BreakingChangeCount:  run.BreakingChangeCount,
		AffectedModulesCount: run.AffectedModulesCount,
		ChangedProtoFiles:    changed,
		CompileErrorText:     run.CompileErrorText,
		BreakingDetails:      breaking,
		AffectedModules:      affected,
		CheckReportID:        run.CheckReportID,
		RemoteCommentID:      run.RemoteCommentID,
		AttemptCount:         run.AttemptCount,
		OverriddenBy:         run.OverriddenBy,
		OverriddenByName:     overriddenByName,
		OverrideReason:       run.OverrideReason,
		OverriddenAt:         run.OverriddenAt,
		DurationMs:           run.DurationMs,
		StartedAt:            run.StartedAt,
		CompletedAt:          run.CompletedAt,
		CreatedAt:            run.CreatedAt,
		UpdatedAt:            run.UpdatedAt,
	}
}

func parseMRPageParams(c *gin.Context) (int, int) {
	page := 1
	if p, err := strconv.Atoi(c.DefaultQuery("page", "1")); err == nil && p > 0 {
		page = p
	}
	pageSize := 25
	if psStr := c.Query("page_size"); psStr != "" {
		if ps, err := strconv.Atoi(psStr); err == nil && ps > 0 {
			pageSize = ps
		}
	} else if psStr := c.Query("pageSize"); psStr != "" {
		if ps, err := strconv.Atoi(psStr); err == nil && ps > 0 {
			pageSize = ps
		}
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}
