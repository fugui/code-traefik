package gitclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"code-traefik/internal/server/decoder"
	"code-traefik/internal/server/hub"
	"code-traefik/internal/server/linter"
	"code-traefik/internal/server/notify"
	"code-traefik/internal/server/protodoc"
	"code-traefik/internal/server/security"
	"code-traefik/models"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var (
	// ErrMRCheckRunNotFound 未找到指定的 MRCheckRun
	ErrMRCheckRunNotFound = errors.New("未找到指定的 MR 质量门禁检查记录")
	// ErrMROverrideForbiddenCompileFailed 严禁豁免存在语法或编译错误的 MR
	ErrMROverrideForbiddenCompileFailed = errors.New("当前 MR 存在 Protobuf 语法或链接编译错误，严禁强制豁免 (Approve with Override)")
	// ErrMROverrideInvalidState 仅 FAILED 状态的 MR 检查允许执行 Override
	ErrMROverrideInvalidState = errors.New("仅处于 FAILED 阻断状态的 MR 门禁流水允许执行 Override 豁免")
)

// MRExecuteInput 单 Bundle 的 MR 门禁检查或合入入参
type MRExecuteInput struct {
	Repo           *models.GitRepo
	Bundle         *models.GitProtoBundle
	MRIID          int
	EventPhase     string // MR_GATE | MAIN_MERGE
	MRTitle        string
	MRURL          string
	SourceBranch   string
	TargetBranch   string
	HeadCommitSHA  string
	TargetHeadSHA  string
	AuthorUsername string
	AuthorEmail    string
	PatchFiles     []MRPatchFile
}

// MRGateRunner Git MR 四合一质量门禁、原位评论回调、Override 与合入自动入池执行器
type MRGateRunner struct {
	db          *gorm.DB
	reg         *decoder.Registry
	broadcaster hub.EventBroadcaster
	notifier    *notify.Dispatcher
}

// NewMRGateRunner 创建 MR 质量门禁执行器
func NewMRGateRunner(db *gorm.DB, reg *decoder.Registry, broadcaster hub.EventBroadcaster) *MRGateRunner {
	if broadcaster == nil {
		broadcaster = hub.GetGlobalSSEBroker()
	}
	return &MRGateRunner{
		db:          db,
		reg:         reg,
		broadcaster: broadcaster,
		notifier:    notify.NewDispatcher(db, broadcaster),
	}
}

type overlayProtoSourceProvider struct {
	virtualTree map[string]string
	category    string
	fallback    decoder.ProtoSourceProvider
}

func (p *overlayProtoSourceProvider) ResolveProtoSource(ctx context.Context, cleanPath string) (*decoder.ProtoSourceEntry, error) {
	if p != nil && p.virtualTree != nil {
		if content, ok := p.virtualTree[cleanPath]; ok {
			return &decoder.ProtoSourceEntry{
				Category: p.category,
				Content:  []byte(content),
			}, nil
		}
	}
	if p != nil && p.fallback != nil {
		return p.fallback.ResolveProtoSource(ctx, cleanPath)
	}
	return nil, nil
}

// CreateOrGetCheckRun 按 uk_traefik_mr_dedup 幂等创建或复用 MRCheckRun 记录
// 返回 (run, alreadyExisted, err)
func (r *MRGateRunner) CreateOrGetCheckRun(ctx context.Context, in MRExecuteInput) (*models.MRCheckRun, bool, error) {
	if r == nil || r.db == nil {
		return nil, false, errors.New("MRGateRunner 未初始化")
	}
	if in.Repo == nil || in.Repo.ID == 0 {
		return nil, false, ErrGitRepoNotFound
	}
	phase := strings.ToUpper(strings.TrimSpace(in.EventPhase))
	if phase != models.MREventPhaseMainMerge {
		phase = models.MREventPhaseGate
	}
	headSHA := strings.TrimSpace(in.HeadCommitSHA)
	if headSHA == "" {
		headSHA = "HEAD"
	}
	targetSHA := strings.TrimSpace(in.TargetHeadSHA)

	var bundleIDPtr *uint64
	var bundleIDOrZero uint64
	if in.Bundle != nil && in.Bundle.ID > 0 {
		bID := in.Bundle.ID
		bundleIDPtr = &bID
		bundleIDOrZero = bID
	}

	var existing models.MRCheckRun
	findErr := r.db.WithContext(ctx).
		Where("git_repo_id = ? AND COALESCE(bundle_id, 0) = ? AND event_phase = ? AND head_commit_sha = ? AND target_head_sha = ?",
			in.Repo.ID, bundleIDOrZero, phase, headSHA, targetSHA).
		First(&existing).Error
	if findErr == nil && existing.ID > 0 {
		return &existing, true, nil
	}

	// 查找同 (git_repo_id, mr_iid, COALESCE(bundle_id, 0)) 最近一次已创建的 remote_comment_id 以实现原位更新
	var prevCommentRun models.MRCheckRun
	var inheritedCommentID string
	if err := r.db.WithContext(ctx).
		Select("id", "remote_comment_id").
		Where("git_repo_id = ? AND mr_iid = ? AND COALESCE(bundle_id, 0) = ? AND remote_comment_id <> ''",
			in.Repo.ID, in.MRIID, bundleIDOrZero).
		Order("id DESC").
		Limit(1).
		Find(&prevCommentRun).Error; err == nil && prevCommentRun.RemoteCommentID != "" {
		inheritedCommentID = prevCommentRun.RemoteCommentID
	}

	srcBranch := strings.TrimSpace(in.SourceBranch)
	if srcBranch == "" {
		srcBranch = "mr-source"
	}
	tgtBranch := strings.TrimSpace(in.TargetBranch)
	if tgtBranch == "" {
		tgtBranch = in.Repo.DefaultBranch
		if tgtBranch == "" {
			tgtBranch = "main"
		}
	}

	run := &models.MRCheckRun{
		GitRepoID:             in.Repo.ID,
		BundleID:              bundleIDPtr,
		MRIID:                 in.MRIID,
		EventPhase:            phase,
		MRTitle:               strings.TrimSpace(in.MRTitle),
		MRURL:                 strings.TrimSpace(in.MRURL),
		SourceBranch:          srcBranch,
		TargetBranch:          tgtBranch,
		HeadCommitSHA:         headSHA,
		TargetHeadSHA:         targetSHA,
		AuthorUsername:        strings.TrimSpace(in.AuthorUsername),
		AuthorEmail:           strings.TrimSpace(in.AuthorEmail),
		Status:                models.MRCheckStatusPending,
		ChangedProtoFilesJSON: datatypes.JSON([]byte(`[]`)),
		BreakingDetailsJSON:   datatypes.JSON([]byte(`[]`)),
		AffectedModulesJSON:   datatypes.JSON([]byte(`[]`)),
		RemoteCommentID:       inheritedCommentID,
		AttemptCount:          1,
	}

	if createErr := r.db.WithContext(ctx).Create(run).Error; createErr != nil {
		// 并发竞态下若已被另一个 worker 插入，重新查询返回既有记录
		var raced models.MRCheckRun
		if qErr := r.db.WithContext(ctx).
			Where("git_repo_id = ? AND COALESCE(bundle_id, 0) = ? AND event_phase = ? AND head_commit_sha = ? AND target_head_sha = ?",
				in.Repo.ID, bundleIDOrZero, phase, headSHA, targetSHA).
			First(&raced).Error; qErr == nil && raced.ID > 0 {
			return &raced, true, nil
		}
		return nil, false, createErr
	}
	return run, false, nil
}

// CreateAndExecuteBundleRun 幂等创建并同步执行单 Bundle 的四合一门禁检查（或合入自动编译）
func (r *MRGateRunner) CreateAndExecuteBundleRun(ctx context.Context, in MRExecuteInput) (*models.MRCheckRun, error) {
	run, existed, err := r.CreateOrGetCheckRun(ctx, in)
	if err != nil {
		return nil, err
	}
	if existed && (run.Status == models.MRCheckStatusPassed || run.Status == models.MRCheckStatusWarn ||
		run.Status == models.MRCheckStatusFailed || run.Status == models.MRCheckStatusOverridden) {
		return run, nil
	}
	return r.ExecuteCheckRun(ctx, run.ID, in.PatchFiles, false)
}

// ExecuteCheckRun 执行指定 MRCheckRun 的四合一流水线：
// (1) 基线+补丁虚拟树合并 -> (2) 纯 Go protocompile 语法与导入链接编译 ->
// (3) Linter 静态规则 + 词库 + 3s LLM + AST 结构体重复探测 ->
// (4) F-26 破坏性变更检测 + BASE_LIB 反向 import 穿透影响面分析 ->
// (5) Git MR 原位评论更新与 Commit Status 回写 -> (6) 通知与 MAIN_MERGE 自动入池
func (r *MRGateRunner) ExecuteCheckRun(ctx context.Context, runID uint64, patchFiles []MRPatchFile, isRerun bool) (*models.MRCheckRun, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("MRGateRunner 未初始化")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var run models.MRCheckRun
	if err := r.db.WithContext(ctx).First(&run, runID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMRCheckRunNotFound
		}
		return nil, err
	}

	var repo models.GitRepo
	if err := r.db.WithContext(ctx).First(&repo, run.GitRepoID).Error; err != nil {
		return nil, ErrGitRepoNotFound
	}

	var bundle models.GitProtoBundle
	if run.BundleID != nil && *run.BundleID > 0 {
		if err := r.db.WithContext(ctx).First(&bundle, *run.BundleID).Error; err != nil {
			return nil, ErrGitBundleNotFound
		}
	} else {
		var firstBundle models.GitProtoBundle
		if err := r.db.WithContext(ctx).Where("git_repo_id = ?", repo.ID).Order("id ASC").First(&firstBundle).Error; err == nil {
			bundle = firstBundle
		} else {
			bundle = models.GitProtoBundle{
				GitRepoID:        repo.ID,
				BundleName:       "default",
				TargetSchemaName: repo.Name,
				Category:         models.ProtoCategoryBusiness,
				SubdirPath:       ".",
			}
		}
	}

	startedAt := time.Now().UTC()
	attempt := run.AttemptCount
	if isRerun {
		attempt++
	}
	runUpdates := map[string]any{
		"status":          models.MRCheckStatusRunning,
		"started_at":      &startedAt,
		"attempt_count":   attempt,
		"overridden_by":   nil,
		"override_reason": "",
		"overridden_at":   nil,
	}
	if err := r.db.WithContext(ctx).Model(&run).Updates(runUpdates).Error; err != nil {
		return nil, err
	}
	run.Status = models.MRCheckStatusRunning
	run.StartedAt = &startedAt
	run.AttemptCount = attempt
	run.OverriddenBy = nil
	run.OverrideReason = ""
	run.OverriddenAt = nil

	if r.broadcaster != nil {
		r.broadcaster.Broadcast("mr_gate.updated", run)
	}

	// 若未直接内联 PatchFiles（如重跑或仅含元数据的 Webhook），尝试通过 REST API 拉取远端分支或恢复已存变更清单
	effectivePatches := patchFiles
	if len(effectivePatches) == 0 {
		effectivePatches = r.fetchFallbackPatchFiles(ctx, &repo, &bundle, &run)
	} else {
		effectivePatches = r.enrichMissingPatchContents(ctx, &repo, &run, effectivePatches)
	}

	// Step 1: 合并 DB 目标分支活跃基线 + MR 变更补丁生成完整内存虚拟树
	virtualTree, changedFiles, vtErr := BuildMRVirtualTree(r.db, &bundle, effectivePatches)
	if vtErr != nil {
		return r.failRunWithCompileError(ctx, &run, &repo, &bundle, startedAt, nil, vtErr.Error())
	}
	if len(changedFiles) == 0 && len(run.ChangedProtoFilesJSON) > 2 {
		var savedChanged []string
		if json.Unmarshal(run.ChangedProtoFilesJSON, &savedChanged) == nil {
			changedFiles = savedChanged
		}
	}

	// Step 2: 纯 Go protocompile 语法与跨包导入链接编译
	var compiled *decoder.CompileProtoOutput
	compilePassed := true
	compileErrorText := ""

	dbSourceProvider := decoder.NewDBProtoSourceProvider(r.db)
	overlayProvider := &overlayProtoSourceProvider{
		virtualTree: virtualTree,
		category:    bundle.Category,
		fallback:    dbSourceProvider,
	}

	if len(virtualTree) > 0 {
		compOut, compErr := decoder.CompileProtoBundle(ctx, nil, decoder.CompileProtoBundleInput{
			Name:             bundle.TargetSchemaName,
			Category:         bundle.Category,
			VersionTag:       shortCommitTag(run.HeadCommitSHA),
			Files:            virtualTree,
			ProtoIncludeDirs: bundle.IncludeDirs(),
			EntryPoints:      bundle.EntryFiles(),
			SourceProvider:   overlayProvider,
		})
		if compErr != nil {
			compilePassed = false
			compileErrorText = compErr.Error()
		} else {
			compiled = compOut
		}
	}

	// Step 3: 静态检查 + 标准词库 + 3s 熔断 LLM + 三护栏 AST 结构体重复探测 (Jaccard >= 0.85)
	var checkReport *models.CheckReport
	var lintErrCnt, lintWarnCnt, dupGroupCnt int
	if len(virtualTree) > 0 {
		paths := make([]string, 0, len(virtualTree))
		for p := range virtualTree {
			paths = append(paths, p)
		}
		sort.Strings(paths)

		var scID uint64
		if bundle.LastSchemaID != nil {
			scID = *bundle.LastSchemaID
		}
		lintSources := make([]linter.SourceInput, 0, len(paths))
		for _, p := range paths {
			lintSources = append(lintSources, linter.SourceInput{
				ProtoFile: p,
				Content:   virtualTree[p],
				Category:  bundle.Category,
				SchemaID:  scID,
			})
		}

		repoIDCopy := repo.ID
		var bundleIDPtr *uint64
		if bundle.ID > 0 {
			bID := bundle.ID
			bundleIDPtr = &bID
		}
		report, scanErr := linter.RunAndPersistScan(ctx, linter.ScanRequest{
			DB:                r.db,
			TriggerSource:     models.CheckTriggerMRGate,
			GitRepoID:         &repoIDCopy,
			BundleID:          bundleIDPtr,
			SchemaID:          bundle.LastSchemaID,
			Sources:           lintSources,
			IncludeDuplicates: true,
			IncludeLLM:        true,
		})
		if scanErr == nil && report != nil {
			checkReport = report
			lintErrCnt = report.ErrorCount
			lintWarnCnt = report.WarningCount
			dupGroupCnt = report.DuplicateGroupCount
			if updErr := r.db.WithContext(ctx).Model(report).Update("mr_check_run_id", run.ID).Error; updErr != nil {
				log.Printf("[mr-gate] 绑定 CheckReport #%d 至 MRCheckRun #%d 失败: %v", report.ID, run.ID, updErr)
			}
		}
	}

	// Step 4: F-26 破坏性变更检测与 BASE_LIB 反向 import 穿透影响面分析
	var breakingViolations []decoder.BreakingRuleViolation
	var affectedDependents []decoder.AffectedDependent
	if compilePassed && compiled != nil {
		oldOwnFiles := r.loadBaselineDescriptorFiles(ctx, &bundle)
		if len(oldOwnFiles) > 0 {
			newOwnFiles := extractOwnDescriptorFiles(compiled.DescriptorBytes, compiled.OwnProtoFiles)
			breakingViolations = decoder.DetectBreakingChanges(oldOwnFiles, newOwnFiles)
		}
		if len(breakingViolations) > 0 {
			affectedDependents = decoder.FindAffectedDependents(r.db, breakingViolations)
			if len(affectedDependents) == 0 && bundle.Category == models.ProtoCategoryBaseLib {
				affectedDependents = r.resolveBaseLibDependentsFromProtoFiles(ctx, changedFiles)
			}
		} else if bundle.Category == models.ProtoCategoryBaseLib && len(changedFiles) > 0 {
			affectedDependents = r.resolveBaseLibDependentsFromProtoFiles(ctx, changedFiles)
		}
	}

	// Step 5: 综合裁决 Status
	finalStatus := models.MRCheckStatusPassed
	switch {
	case !compilePassed || lintErrCnt > 0 || len(breakingViolations) > 0:
		finalStatus = models.MRCheckStatusFailed
	case lintWarnCnt > 0 || dupGroupCnt > 0:
		finalStatus = models.MRCheckStatusWarn
	default:
		finalStatus = models.MRCheckStatusPassed
	}

	changedBytes, _ := json.Marshal(nonNilStrings(changedFiles))
	breakingBytes, _ := json.Marshal(nonNilBreaking(breakingViolations))
	affectedBytes, _ := json.Marshal(nonNilAffected(affectedDependents))

	completedAt := time.Now().UTC()
	durationMs := int(completedAt.Sub(startedAt).Milliseconds())
	if durationMs < 0 {
		durationMs = 0
	}

	var reportIDPtr *uint64
	if checkReport != nil && checkReport.ID > 0 {
		rID := checkReport.ID
		reportIDPtr = &rID
	}

	// Step 6: 远端 Git MR 原位评论 Upsert (remote_comment_id) + Commit Status 回写
	commentBody := buildMRBotCommentMarkdown(&run, &bundle, finalStatus, compilePassed, compileErrorText,
		lintErrCnt, lintWarnCnt, dupGroupCnt, breakingViolations, affectedDependents, checkReport)
	remoteCommentID := r.upsertRemoteMRComment(ctx, &repo, run.MRIID, run.RemoteCommentID, commentBody)
	if remoteCommentID == "" {
		remoteCommentID = run.RemoteCommentID
	}
	r.postRemoteCommitStatus(ctx, &repo, &bundle, run.HeadCommitSHA, finalStatus)

	// Step 7: 持久化 MRCheckRun 结果
	finalUpdates := map[string]any{
		"status":                 finalStatus,
		"compile_passed":         compilePassed,
		"lint_error_count":       lintErrCnt,
		"lint_warning_count":     lintWarnCnt,
		"duplicate_group_count":  dupGroupCnt,
		"breaking_change_count":  len(breakingViolations),
		"affected_modules_count": len(affectedDependents),
		"changed_proto_files":    datatypes.JSON(changedBytes),
		"compile_error_text":     compileErrorText,
		"breaking_details":       datatypes.JSON(breakingBytes),
		"affected_modules":       datatypes.JSON(affectedBytes),
		"check_report_id":        reportIDPtr,
		"remote_comment_id":      remoteCommentID,
		"duration_ms":            durationMs,
		"completed_at":           &completedAt,
	}
	if err := r.db.WithContext(ctx).Model(&run).Updates(finalUpdates).Error; err != nil {
		return nil, err
	}

	if err := r.db.WithContext(ctx).Preload("GitRepo").Preload("Bundle").Preload("CheckReport").First(&run, run.ID).Error; err != nil {
		log.Printf("[mr-gate] 重新加载 MRCheckRun #%d 失败: %v", run.ID, err)
	}

	// Step 8: 若处于 MAIN_MERGE 合入主干阶段且编译通过，自动同步源文件并编译生成 GIT_MR_AUTO ProtoSchema
	var publishedSchema *models.ProtoSchema
	if run.EventPhase == models.MREventPhaseMainMerge && compilePassed && bundle.ID > 0 && len(virtualTree) > 0 {
		publishedSchema = r.syncAndPublishMergedBundle(ctx, &repo, &bundle, &run, virtualTree)
	}

	// Step 9: 广播 SSE 与触发精准通知/邮件
	if r.broadcaster != nil {
		r.broadcaster.Broadcast("mr_gate.updated", run)
		r.broadcaster.Broadcast("mr_gate.completed", run)
	}
	r.dispatchMRNotifications(ctx, &repo, &bundle, &run, breakingViolations, affectedDependents, publishedSchema)

	return &run, nil
}

// ApplyAdminOverride 执行管理员强制豁免 (Approve with Override)
// 严禁豁免 compile_passed == false 的语法/链接错误！
func (r *MRGateRunner) ApplyAdminOverride(
	ctx context.Context,
	runID uint64,
	operatorID uint,
	operatorName string,
	reason string,
) (*models.MRCheckRun, models.MRCheckRun, error) {
	if r == nil || r.db == nil {
		return nil, models.MRCheckRun{}, errors.New("MRGateRunner 未初始化")
	}
	var run models.MRCheckRun
	if err := r.db.WithContext(ctx).First(&run, runID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, models.MRCheckRun{}, ErrMRCheckRunNotFound
		}
		return nil, models.MRCheckRun{}, err
	}
	beforeSnapshot := run

	// 核心红线：语法错误或编译链接失败 (compile_passed == false) 绝不允许豁免！
	if !run.CompilePassed {
		return nil, beforeSnapshot, ErrMROverrideForbiddenCompileFailed
	}
	if run.Status != models.MRCheckStatusFailed {
		return nil, beforeSnapshot, ErrMROverrideInvalidState
	}

	var repo models.GitRepo
	if err := r.db.WithContext(ctx).First(&repo, run.GitRepoID).Error; err != nil {
		return nil, beforeSnapshot, ErrGitRepoNotFound
	}
	var bundle models.GitProtoBundle
	if run.BundleID != nil && *run.BundleID > 0 {
		if err := r.db.WithContext(ctx).First(&bundle, *run.BundleID).Error; err != nil {
			log.Printf("[mr-gate] 读取关联 Bundle #%d 失败: %v", *run.BundleID, err)
		}
	}

	now := time.Now().UTC()
	cleanReason := strings.TrimSpace(reason)
	updates := map[string]any{
		"status":          models.MRCheckStatusOverridden,
		"overridden_by":   &operatorID,
		"override_reason": cleanReason,
		"overridden_at":   &now,
	}
	if err := r.db.WithContext(ctx).Model(&run).Updates(updates).Error; err != nil {
		return nil, beforeSnapshot, err
	}
	run.Status = models.MRCheckStatusOverridden
	run.OverriddenBy = &operatorID
	run.OverrideReason = cleanReason
	run.OverriddenAt = &now

	// 更新远端 Git 评论（追加 Override 豁免横幅）并将 Commit Status 翻转为 success
	overrideComment := fmt.Sprintf("### ⚠️ [CodeTraefik Protobuf 门禁] 已由管理员强制豁免 (OVERRIDDEN)\n\n"+
		"- **MR**: `!%d` (%s)\n"+
		"- **Commit**: `%s`\n"+
		"- **豁免操作人**: `%s` (ID: %d)\n"+
		"- **豁免原因**: %s\n"+
		"- **豁免时间**: %s\n",
		run.MRIID, run.MRTitle, shortSHA(run.HeadCommitSHA), operatorName, operatorID, cleanReason, now.Format(time.RFC3339))
	remoteCommentID := r.upsertRemoteMRComment(ctx, &repo, run.MRIID, run.RemoteCommentID, overrideComment)
	if remoteCommentID != "" && remoteCommentID != run.RemoteCommentID {
		if err := r.db.WithContext(ctx).Model(&run).Update("remote_comment_id", remoteCommentID).Error; err != nil {
			log.Printf("[mr-gate] 更新 remote_comment_id 失败: %v", err)
		}
		run.RemoteCommentID = remoteCommentID
	}
	r.postRemoteCommitStatus(ctx, &repo, &bundle, run.HeadCommitSHA, models.MRCheckStatusOverridden)

	if err := r.db.WithContext(ctx).Preload("GitRepo").Preload("Bundle").Preload("CheckReport").First(&run, run.ID).Error; err != nil {
		log.Printf("[mr-gate] 重新加载 MRCheckRun #%d 失败: %v", run.ID, err)
	}

	if r.broadcaster != nil {
		r.broadcaster.Broadcast("mr_gate.updated", run)
	}

	// 通知受影响模块负责人与 MR 作者：门禁已被管理员豁免放行
	if r.notifier != nil {
		var affected []decoder.AffectedDependent
		if err := json.Unmarshal(run.AffectedModulesJSON, &affected); err != nil {
			affected = nil
		}
		modIDs := make([]uint64, 0, len(affected))
		for _, a := range affected {
			if a.ModuleID > 0 {
				modIDs = append(modIDs, a.ModuleID)
			}
		}
		rcpts := notify.ResolveRecipientUserIDsForModules(r.db, modIDs, run.AuthorUsername, run.AuthorEmail, true)
		var toEmails []string
		if strings.Contains(run.AuthorEmail, "@") {
			toEmails = append(toEmails, run.AuthorEmail)
		}
		if _, err := r.notifier.DispatchSync(ctx, notify.DispatchInput{
			RecipientUserIDs: rcpts,
			ToEmails:         toEmails,
			Category:         models.NotifCategoryMRGatePassed,
			Severity:         models.NotifSeverityWarning,
			Title:            fmt.Sprintf("[MR !%d] 契约门禁已由管理员 %s 强制豁免放行", run.MRIID, operatorName),
			BodyMarkdown:     overrideComment,
			LinkURL:          fmt.Sprintf("/schemas?tab=mr-gates&runId=%d", run.ID),
			SourceType:       models.NotifSourceMRCheckRun,
			SourceID:         run.ID,
			Metadata: map[string]any{
				"mr_iid":          run.MRIID,
				"overridden_by":   operatorName,
				"override_reason": cleanReason,
			},
		}); err != nil {
			log.Printf("[mr-gate] 发送 Override 通知失败: %v", err)
		}
	}

	return &run, beforeSnapshot, nil
}

func (r *MRGateRunner) failRunWithCompileError(
	ctx context.Context,
	run *models.MRCheckRun,
	repo *models.GitRepo,
	bundle *models.GitProtoBundle,
	startedAt time.Time,
	changedFiles []string,
	errMsg string,
) (*models.MRCheckRun, error) {
	completedAt := time.Now().UTC()
	durationMs := int(completedAt.Sub(startedAt).Milliseconds())
	changedBytes, _ := json.Marshal(nonNilStrings(changedFiles))

	commentBody := buildMRBotCommentMarkdown(run, bundle, models.MRCheckStatusFailed, false, errMsg, 0, 0, 0, nil, nil, nil)
	remoteCommentID := r.upsertRemoteMRComment(ctx, repo, run.MRIID, run.RemoteCommentID, commentBody)
	if remoteCommentID == "" {
		remoteCommentID = run.RemoteCommentID
	}
	r.postRemoteCommitStatus(ctx, repo, bundle, run.HeadCommitSHA, models.MRCheckStatusFailed)

	updates := map[string]any{
		"status":              models.MRCheckStatusFailed,
		"compile_passed":      false,
		"compile_error_text":  errMsg,
		"changed_proto_files": datatypes.JSON(changedBytes),
		"remote_comment_id":   remoteCommentID,
		"duration_ms":         durationMs,
		"completed_at":        &completedAt,
	}
	if err := r.db.WithContext(ctx).Model(run).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := r.db.WithContext(ctx).Preload("GitRepo").Preload("Bundle").First(run, run.ID).Error; err != nil {
		log.Printf("[mr-gate] 重新加载 MRCheckRun #%d 失败: %v", run.ID, err)
	}
	if r.broadcaster != nil {
		r.broadcaster.Broadcast("mr_gate.updated", *run)
		r.broadcaster.Broadcast("mr_gate.completed", *run)
	}
	r.dispatchMRNotifications(ctx, repo, bundle, run, nil, nil, nil)
	return run, nil
}

func (r *MRGateRunner) loadBaselineDescriptorFiles(ctx context.Context, bundle *models.GitProtoBundle) []*descriptorpb.FileDescriptorProto {
	if r == nil || r.db == nil || bundle == nil {
		return nil
	}
	var baseSchema models.ProtoSchema
	err := r.db.WithContext(ctx).
		Where("name = ? AND is_active = true", bundle.TargetSchemaName).
		Order("version DESC, id DESC").
		First(&baseSchema).Error
	if err != nil && bundle.LastSchemaID != nil && *bundle.LastSchemaID > 0 {
		err = r.db.WithContext(ctx).First(&baseSchema, *bundle.LastSchemaID).Error
	}
	if err != nil || len(baseSchema.DescriptorBytes) == 0 {
		return nil
	}
	var ownNames []string
	if len(baseSchema.ProtoFiles) > 0 {
		if uErr := json.Unmarshal(baseSchema.ProtoFiles, &ownNames); uErr != nil {
			ownNames = nil
		}
	}
	return extractOwnDescriptorFiles(baseSchema.DescriptorBytes, ownNames)
}

func extractOwnDescriptorFiles(descriptorBytes []byte, ownNames []string) []*descriptorpb.FileDescriptorProto {
	if len(descriptorBytes) == 0 {
		return nil
	}
	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(descriptorBytes, &fds); err != nil {
		return nil
	}
	ownSet := make(map[string]struct{}, len(ownNames))
	for _, n := range ownNames {
		ownSet[n] = struct{}{}
	}
	out := make([]*descriptorpb.FileDescriptorProto, 0, len(fds.File))
	for _, f := range fds.File {
		if len(ownSet) > 0 {
			if _, ok := ownSet[f.GetName()]; !ok {
				continue
			}
		}
		out = append(out, f)
	}
	return out
}

func (r *MRGateRunner) resolveBaseLibDependentsFromProtoFiles(ctx context.Context, changedBaseFiles []string) []decoder.AffectedDependent {
	if r == nil || r.db == nil || len(changedBaseFiles) == 0 {
		return []decoder.AffectedDependent{}
	}
	// 构造虚拟 BreakingRuleViolation (ServiceName == "") 以复用 decoder.FindAffectedDependents 的反向 import 穿透图
	synthetic := make([]decoder.BreakingRuleViolation, 0, len(changedBaseFiles))
	for _, fp := range changedBaseFiles {
		synthetic = append(synthetic, decoder.BreakingRuleViolation{
			Rule:      "BASE_LIB_MODIFIED",
			ProtoFile: fp,
		})
	}
	dependents := decoder.FindAffectedDependents(r.db.WithContext(ctx), synthetic)
	return dependents
}

func (r *MRGateRunner) syncAndPublishMergedBundle(
	ctx context.Context,
	repo *models.GitRepo,
	bundle *models.GitProtoBundle,
	run *models.MRCheckRun,
	virtualTree map[string]string,
) *models.ProtoSchema {
	if bundle.MRAutoPublish == models.MRAutoPublishDisabled && !bundle.AutoCompile {
		return nil
	}

	// 1. 将合入后的虚拟树文件同步落库至 traefik_proto_files & traefik_proto_file_versions (SourceType = GIT_MR_AUTO)
	paths := make([]string, 0, len(virtualTree))
	for p := range virtualTree {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	subdir, _ := SanitizeSubdirPath(bundle.SubdirPath)
	now := time.Now().UTC()
	versionTag := fmt.Sprintf("mr-%d-%s", run.MRIID, shortSHA(run.HeadCommitSHA))
	touchedVerIDs := make([]uint64, 0, len(paths))

	txErr := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing []models.ProtoFile
		if err := tx.Where("bundle_id = ? AND is_deleted = false", bundle.ID).Find(&existing).Error; err != nil {
			return err
		}
		byPath := make(map[string]*models.ProtoFile, len(existing))
		for i := range existing {
			byPath[existing[i].FilePath] = &existing[i]
		}

		activeSet := make(map[string]struct{}, len(paths))
		for _, p := range paths {
			activeSet[p] = struct{}{}
			content := virtualTree[p]
			sum := sha256.Sum256([]byte(content))
			shaHex := hex.EncodeToString(sum[:])
			repoPath := p
			if subdir != "." && subdir != "" {
				repoPath = subdir + "/" + p
			}
			lineCnt := countNormalizedLines(content)
			pkgName := parseProtoPackageName(content)

			cur := byPath[p]
			if cur == nil {
				pf := models.ProtoFile{
					GitRepoID:        repo.ID,
					BundleID:         bundle.ID,
					TargetSchemaName: bundle.TargetSchemaName,
					Category:         bundle.Category,
					FilePath:         p,
					RepoFilePath:     repoPath,
					PackageName:      pkgName,
					ContentSHA256:    shaHex,
					LineCount:        lineCnt,
					LatestVersionNum: 1,
					LatestCommitSHA:  run.HeadCommitSHA,
					IsDeleted:        false,
				}
				if err := tx.Create(&pf).Error; err != nil {
					return err
				}
				ver := models.ProtoFileVersion{
					ProtoFileID:   pf.ID,
					Version:       1,
					VersionTag:    versionTag,
					SourceType:    models.ProtoSourceGitMRAuto,
					CommitSHA:     run.HeadCommitSHA,
					CommitMessage: run.MRTitle,
					AuthorName:    run.AuthorUsername,
					AuthorEmail:   run.AuthorEmail,
					ContentText:   content,
					ContentSHA256: shaHex,
					LineCount:     lineCnt,
					CommittedAt:   now,
				}
				if err := tx.Create(&ver).Error; err != nil {
					return err
				}
				if err := tx.Model(&pf).Update("latest_version_id", ver.ID).Error; err != nil {
					return err
				}
				touchedVerIDs = append(touchedVerIDs, ver.ID)
				continue
			}

			if cur.ContentSHA256 == shaHex {
				if cur.LatestVersionID != nil {
					touchedVerIDs = append(touchedVerIDs, *cur.LatestVersionID)
				}
				continue
			}

			var maxVer int
			if err := tx.Model(&models.ProtoFileVersion{}).Where("proto_file_id = ?", cur.ID).
				Select("COALESCE(MAX(version), 0)").Scan(&maxVer).Error; err != nil {
				return err
			}
			nextVer := maxVer + 1
			ver := models.ProtoFileVersion{
				ProtoFileID:   cur.ID,
				Version:       nextVer,
				VersionTag:    versionTag,
				SourceType:    models.ProtoSourceGitMRAuto,
				CommitSHA:     run.HeadCommitSHA,
				CommitMessage: run.MRTitle,
				AuthorName:    run.AuthorUsername,
				AuthorEmail:   run.AuthorEmail,
				ContentText:   content,
				ContentSHA256: shaHex,
				LineCount:     lineCnt,
				CommittedAt:   now,
			}
			if err := tx.Create(&ver).Error; err != nil {
				return err
			}
			if err := tx.Model(cur).Updates(map[string]any{
				"repo_file_path":     repoPath,
				"package_name":       pkgName,
				"content_sha256":     shaHex,
				"line_count":         lineCnt,
				"latest_version_id":  ver.ID,
				"latest_version_num": nextVer,
				"latest_commit_sha":  run.HeadCommitSHA,
			}).Error; err != nil {
				return err
			}
			touchedVerIDs = append(touchedVerIDs, ver.ID)
		}

		for p, oldFile := range byPath {
			if _, ok := activeSet[p]; !ok {
				if err := tx.Model(oldFile).Update("is_deleted", true).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if txErr != nil {
		log.Printf("[mr-gate] MAIN_MERGE 同步 Bundle #%d 源文件失败: %v", bundle.ID, txErr)
	}

	if r.reg == nil {
		return nil
	}

	wantAutoActivate := bundle.AutoActivate || bundle.MRAutoPublish == models.MRAutoPublishAutoActivate
	repoIDCopy := repo.ID
	bundleIDCopy := bundle.ID
	mrIIDCopy := run.MRIID

	compileIn := decoder.CompileProtoBundleInput{
		Name:             bundle.TargetSchemaName,
		Category:         bundle.Category,
		VersionTag:       versionTag,
		Description:      fmt.Sprintf("Git MR !%d 合入自动编译: %s", run.MRIID, run.MRTitle),
		Files:            virtualTree,
		ProtoIncludeDirs: bundle.IncludeDirs(),
		EntryPoints:      bundle.EntryFiles(),
		AutoActivate:     wantAutoActivate,
		SourceType:       models.ProtoSourceGitMRAuto,
		GitRepoID:        &repoIDCopy,
		SourceBundleID:   &bundleIDCopy,
		SourceCommitSHA:  run.HeadCommitSHA,
		SourceMRIID:      &mrIIDCopy,
		SourceProvider:   decoder.NewDBProtoSourceProvider(r.db),
	}

	schema, _, _, compErr := r.reg.CompileAndSaveSchema(ctx, compileIn)
	if compErr != nil && wantAutoActivate {
		// 若自动激活因跨包接口冲突或破坏性变更受阻，自动降级为草稿版本入库 (is_active=false)
		compileIn.AutoActivate = false
		schema, _, _, compErr = r.reg.CompileAndSaveSchema(ctx, compileIn)
	}
	if compErr != nil || schema == nil {
		log.Printf("[mr-gate] MAIN_MERGE 自动编译 Schema [%s] 失败: %v", bundle.TargetSchemaName, compErr)
		return nil
	}

	if len(touchedVerIDs) > 0 {
		if err := r.db.WithContext(ctx).Model(&models.ProtoFileVersion{}).
			Where("id IN ?", touchedVerIDs).
			Update("schema_id", schema.ID).Error; err != nil {
			log.Printf("[mr-gate] 更新 ProtoFileVersion schema_id 失败: %v", err)
		}
	}
	if err := r.db.WithContext(ctx).Model(bundle).Updates(map[string]any{
		"last_schema_id":   schema.ID,
		"last_sync_commit": run.HeadCommitSHA,
		"last_sync_at":     &now,
	}).Error; err != nil {
		log.Printf("[mr-gate] 更新 Bundle last_schema_id 失败: %v", err)
	}
	if _, docErr := protodoc.RebuildForSchema(r.db, schema); docErr != nil {
		log.Printf("[mr-gate] MAIN_MERGE 重建 Schema #%d 文档符号失败: %v", schema.ID, docErr)
	}
	return schema
}

func (r *MRGateRunner) dispatchMRNotifications(
	ctx context.Context,
	repo *models.GitRepo,
	bundle *models.GitProtoBundle,
	run *models.MRCheckRun,
	breaking []decoder.BreakingRuleViolation,
	affected []decoder.AffectedDependent,
	publishedSchema *models.ProtoSchema,
) {
	if r == nil || r.notifier == nil || run == nil {
		return
	}

	modIDs := make([]uint64, 0, len(affected))
	for _, a := range affected {
		if a.ModuleID > 0 {
			modIDs = append(modIDs, a.ModuleID)
		}
	}
	rcptUserIDs := notify.ResolveRecipientUserIDsForModules(r.db, modIDs, run.AuthorUsername, run.AuthorEmail, true)
	var toEmails []string
	if strings.Contains(run.AuthorEmail, "@") {
		toEmails = append(toEmails, run.AuthorEmail)
	}

	linkURL := fmt.Sprintf("/schemas?tab=mr-gates&runId=%d", run.ID)
	meta := map[string]any{
		"run_id":        run.ID,
		"git_repo_id":   repo.ID,
		"repo_name":     repo.Name,
		"bundle_name":   bundle.BundleName,
		"mr_iid":        run.MRIID,
		"event_phase":   run.EventPhase,
		"status":        run.Status,
		"source_branch": run.SourceBranch,
		"target_branch": run.TargetBranch,
	}

	if run.EventPhase == models.MREventPhaseMainMerge && publishedSchema != nil {
		title := fmt.Sprintf("[契约发布] MR !%d 合入已生成 %s v%d (%s)", run.MRIID, publishedSchema.Name, publishedSchema.Version, publishedSchema.VersionTag)
		body := fmt.Sprintf("Git 仓库 **%s** 的 Merge Request `!%d` (%s) 已合入 `%s`，自动编译入池契约 **%s v%d**（激活状态：`%v`）。",
			repo.Name, run.MRIID, run.MRTitle, run.TargetBranch, publishedSchema.Name, publishedSchema.Version, publishedSchema.IsActive)
		if _, err := r.notifier.DispatchSync(ctx, notify.DispatchInput{
			RecipientUserIDs: rcptUserIDs,
			ToEmails:         toEmails,
			Category:         models.NotifCategorySchemaPublished,
			Severity:         models.NotifSeverityInfo,
			Title:            title,
			BodyMarkdown:     body,
			LinkURL:          linkURL,
			SourceType:       models.NotifSourceProtoSchema,
			SourceID:         publishedSchema.ID,
			Metadata:         meta,
		}); err != nil {
			log.Printf("[mr-gate] 发送 SchemaPublished 通知失败: %v", err)
		}
		return
	}

	// 根据门禁结果确定通知分类与级别
	var category, severity, title string
	switch {
	case len(breaking) > 0:
		category = models.NotifCategoryBreakingAlert
		severity = models.NotifSeverityCritical
		title = fmt.Sprintf("[MR !%d 阻断] %s 检测到 %d 项破坏性契约变更（波及 %d 个下游模块）",
			run.MRIID, bundle.TargetSchemaName, len(breaking), len(affected))
	case bundle.Category == models.ProtoCategoryBaseLib && len(affected) > 0 && run.Status != models.MRCheckStatusPassed:
		category = models.NotifCategoryBaseLibImpact
		severity = models.NotifSeverityWarning
		title = fmt.Sprintf("[公共库波及预警] MR !%d 变更 BASE_LIB %s 波及 %d 个下游模块",
			run.MRIID, bundle.TargetSchemaName, len(affected))
	case run.Status == models.MRCheckStatusFailed:
		category = models.NotifCategoryMRGateBlocked
		severity = models.NotifSeverityCritical
		title = fmt.Sprintf("[MR !%d 阻断] %s 契约四合一门禁未通过 (编译:%v / 错误:%d)",
			run.MRIID, bundle.TargetSchemaName, run.CompilePassed, run.LintErrorCount)
	case run.DuplicateGroupCount > 0:
		category = models.NotifCategoryDuplicateWarn
		severity = models.NotifSeverityWarning
		title = fmt.Sprintf("[MR !%d 告警] %s 检出 %d 组高相似重复结构体",
			run.MRIID, bundle.TargetSchemaName, run.DuplicateGroupCount)
	default:
		category = models.NotifCategoryMRGatePassed
		severity = models.NotifSeverityInfo
		title = fmt.Sprintf("[MR !%d 通过] %s 契约质量门禁检查通过", run.MRIID, bundle.TargetSchemaName)
	}

	bodyMD := fmt.Sprintf("仓库 **%s** (`%s -> %s`) MR `!%d` **%s** 门禁结果：**%s**\n\n"+
		"- 编译通过：`%v`\n- 静态错误：`%d` 项 / 警告：`%d` 项\n- 重复结构体簇：`%d` 组\n- 破坏性变更：`%d` 项（受影响下游模块：`%d` 个）",
		repo.Name, run.SourceBranch, run.TargetBranch, run.MRIID, run.MRTitle, run.Status,
		run.CompilePassed, run.LintErrorCount, run.LintWarningCount, run.DuplicateGroupCount,
		len(breaking), len(affected))
	if !run.CompilePassed && run.CompileErrorText != "" {
		bodyMD += "\n- 编译诊断：`" + run.CompileErrorText + "`"
	}

	if _, err := r.notifier.DispatchSync(ctx, notify.DispatchInput{
		RecipientUserIDs: rcptUserIDs,
		ToEmails:         toEmails,
		Category:         category,
		Severity:         severity,
		Title:            title,
		BodyMarkdown:     bodyMD,
		LinkURL:          linkURL,
		SourceType:       models.NotifSourceMRCheckRun,
		SourceID:         run.ID,
		Metadata:         meta,
	}); err != nil {
		log.Printf("[mr-gate] 发送 MR 门禁通知失败: %v", err)
	}
}

func (r *MRGateRunner) upsertRemoteMRComment(
	ctx context.Context,
	repo *models.GitRepo,
	mrIID int,
	existingCommentID string,
	markdownBody string,
) string {
	if repo == nil || mrIID <= 0 || strings.TrimSpace(markdownBody) == "" {
		return existingCommentID
	}
	coords, err := parseRESTRepoCoords(repo)
	if err != nil {
		return existingCommentID
	}
	dialer, err := security.NewSafeDialer(loadOutboundTrustedCIDRs(r.db))
	if err != nil {
		return existingCommentID
	}
	creds, err := repo.DecryptCredentials()
	if err != nil {
		return existingCommentID
	}
	client := newGuardedHTTPClient(dialer, 8*time.Second)
	defer client.CloseIdleConnections()

	payloadBytes, _ := json.Marshal(map[string]string{"body": markdownBody})

	// 1. 若已有 remote_comment_id，优先执行 PUT / PATCH 原位更新评论（严禁无脑新建造成刷屏）
	if strings.TrimSpace(existingCommentID) != "" {
		var updateURL, method string
		switch repo.Provider {
		case models.GitProviderGitLab:
			method = http.MethodPut
			updateURL = fmt.Sprintf("%s/projects/%s/merge_requests/%d/notes/%s",
				coords.APIBase, url.PathEscape(coords.OwnerRepo), mrIID, url.PathEscape(existingCommentID))
		case models.GitProviderGitHub, models.GitProviderGitea:
			method = http.MethodPatch
			updateURL = fmt.Sprintf("%s/repos/%s/issues/comments/%s",
				coords.APIBase, coords.OwnerRepo, url.PathEscape(existingCommentID))
		}
		if updateURL != "" {
			if status, _, uErr := doSafeWriteRESTRequest(ctx, client, dialer, repo.Provider, creds, method, updateURL, payloadBytes); uErr == nil && status >= 200 && status < 300 {
				return existingCommentID
			}
		}
	}

	// 2. 首次评论（或远端评论已被手工删除返回 404 时）执行 POST 新建并记录返回的 comment ID
	var createURL string
	switch repo.Provider {
	case models.GitProviderGitLab:
		createURL = fmt.Sprintf("%s/projects/%s/merge_requests/%d/notes",
			coords.APIBase, url.PathEscape(coords.OwnerRepo), mrIID)
	case models.GitProviderGitHub, models.GitProviderGitea:
		createURL = fmt.Sprintf("%s/repos/%s/issues/%d/comments",
			coords.APIBase, coords.OwnerRepo, mrIID)
	default:
		return existingCommentID
	}

	status, respBody, err := doSafeWriteRESTRequest(ctx, client, dialer, repo.Provider, creds, http.MethodPost, createURL, payloadBytes)
	if err != nil || status < 200 || status >= 300 {
		return existingCommentID
	}
	var createdNote struct {
		ID any `json:"id"`
	}
	if err := json.Unmarshal(respBody, &createdNote); err == nil && createdNote.ID != nil {
		switch v := createdNote.ID.(type) {
		case float64:
			return strconv.FormatInt(int64(v), 10)
		case string:
			return strings.TrimSpace(v)
		default:
			return fmt.Sprintf("%v", v)
		}
	}
	return existingCommentID
}

func (r *MRGateRunner) postRemoteCommitStatus(
	ctx context.Context,
	repo *models.GitRepo,
	bundle *models.GitProtoBundle,
	commitSHA string,
	gateStatus string,
) {
	if repo == nil || strings.TrimSpace(commitSHA) == "" {
		return
	}
	coords, err := parseRESTRepoCoords(repo)
	if err != nil {
		return
	}
	dialer, err := security.NewSafeDialer(loadOutboundTrustedCIDRs(r.db))
	if err != nil {
		return
	}
	creds, err := repo.DecryptCredentials()
	if err != nil {
		return
	}
	client := newGuardedHTTPClient(dialer, 6*time.Second)
	defer client.CloseIdleConnections()

	contextName := "code-traefik/proto-mr-gate"
	if bundle != nil && bundle.BundleName != "" {
		contextName = "code-traefik/proto-mr-gate/" + bundle.BundleName
	}

	var statusURL string
	var bodyMap map[string]string
	switch repo.Provider {
	case models.GitProviderGitLab:
		glState := "success"
		switch gateStatus {
		case models.MRCheckStatusFailed:
			glState = "failed"
		case models.MRCheckStatusPending, models.MRCheckStatusRunning:
			glState = "running"
		}
		statusURL = fmt.Sprintf("%s/projects/%s/statuses/%s",
			coords.APIBase, url.PathEscape(coords.OwnerRepo), url.PathEscape(commitSHA))
		bodyMap = map[string]string{
			"state":       glState,
			"name":        contextName,
			"description": fmt.Sprintf("CodeTraefik Proto Gate: %s", gateStatus),
		}
	case models.GitProviderGitHub, models.GitProviderGitea:
		ghState := "success"
		switch gateStatus {
		case models.MRCheckStatusFailed:
			ghState = "failure"
		case models.MRCheckStatusPending, models.MRCheckStatusRunning:
			ghState = "pending"
		}
		statusURL = fmt.Sprintf("%s/repos/%s/statuses/%s",
			coords.APIBase, coords.OwnerRepo, url.PathEscape(commitSHA))
		bodyMap = map[string]string{
			"state":       ghState,
			"context":     contextName,
			"description": fmt.Sprintf("CodeTraefik Proto Gate: %s", gateStatus),
		}
	default:
		return
	}

	payloadBytes, _ := json.Marshal(bodyMap)
	if _, _, err := doSafeWriteRESTRequest(ctx, client, dialer, repo.Provider, creds, http.MethodPost, statusURL, payloadBytes); err != nil {
		log.Printf("[mr-gate] 回写远端 Commit Status 失败: %v", err)
	}
}

func doSafeWriteRESTRequest(
	ctx context.Context,
	client *http.Client,
	dialer *security.SafeDialer,
	provider string,
	creds string,
	method string,
	rawURL string,
	jsonBody []byte,
) (int, []byte, error) {
	if err := dialer.ValidateURL(ctx, rawURL); err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, bytes.NewReader(jsonBody))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(creds) != "" {
		switch provider {
		case models.GitProviderGitLab:
			req.Header.Set("PRIVATE-TOKEN", creds)
		case models.GitProviderGitea:
			req.Header.Set("Authorization", "token "+creds)
		default:
			req.Header.Set("Authorization", "Bearer "+creds)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	return resp.StatusCode, body, err
}

func (r *MRGateRunner) fetchFallbackPatchFiles(
	ctx context.Context,
	repo *models.GitRepo,
	bundle *models.GitProtoBundle,
	run *models.MRCheckRun,
) []MRPatchFile {
	syncSvc := NewSyncService(r.db, r.reg, nil)
	ref := run.HeadCommitSHA
	if ref == "" || ref == "HEAD" {
		ref = run.SourceBranch
	}
	snap, err := syncSvc.fetchRemoteSnapshot(ctx, repo, ref, bundle.SubdirPath, "")
	if err != nil || snap == nil {
		return nil
	}
	patches := make([]MRPatchFile, 0, len(snap.Files))
	for _, f := range snap.Files {
		patches = append(patches, MRPatchFile{
			ChangeType: MRChangeModified,
			NewPath:    f.RepoFilePath,
			Content:    string(f.Content),
		})
	}
	return patches
}

func (r *MRGateRunner) enrichMissingPatchContents(
	ctx context.Context,
	repo *models.GitRepo,
	run *models.MRCheckRun,
	patches []MRPatchFile,
) []MRPatchFile {
	needFetch := false
	for _, p := range patches {
		ct := strings.ToUpper(strings.TrimSpace(p.ChangeType))
		if ct != MRChangeDeleted && strings.TrimSpace(p.Content) == "" && strings.HasSuffix(p.effectiveNewPath(), ".proto") {
			needFetch = true
			break
		}
	}
	if !needFetch {
		return patches
	}
	syncSvc := NewSyncService(r.db, r.reg, nil)
	ref := run.HeadCommitSHA
	if ref == "" || ref == "HEAD" {
		ref = run.SourceBranch
	}
	snap, err := syncSvc.fetchRemoteSnapshot(ctx, repo, ref, "", "")
	if err != nil || snap == nil {
		return patches
	}
	byRepoPath := make(map[string]string, len(snap.Files))
	for _, f := range snap.Files {
		byRepoPath[f.RepoFilePath] = string(f.Content)
	}
	out := make([]MRPatchFile, len(patches))
	copy(out, patches)
	for i := range out {
		if strings.ToUpper(strings.TrimSpace(out[i].ChangeType)) == MRChangeDeleted || out[i].Content != "" {
			continue
		}
		if content, ok := byRepoPath[out[i].effectiveNewPath()]; ok {
			out[i].Content = content
		}
	}
	return out
}

func buildMRBotCommentMarkdown(
	run *models.MRCheckRun,
	bundle *models.GitProtoBundle,
	status string,
	compilePassed bool,
	compileErr string,
	lintErrors, lintWarnings, dupGroups int,
	breaking []decoder.BreakingRuleViolation,
	affected []decoder.AffectedDependent,
	report *models.CheckReport,
) string {
	var b strings.Builder
	badge := "✅ PASSED"
	switch status {
	case models.MRCheckStatusFailed:
		badge = "❌ FAILED (阻断合入)"
	case models.MRCheckStatusWarn:
		badge = "⚠️ WARN (存在静态建议)"
	case models.MRCheckStatusOverridden:
		badge = "⚠️ OVERRIDDEN (管理员豁免)"
	}

	b.WriteString(fmt.Sprintf("### 🛡️ CodeTraefik Protobuf 契约四合一门禁报告 — %s\n\n", badge))
	b.WriteString(fmt.Sprintf("- **目标契约 Bundle**: `%s` (`%s`, 分类: `%s`)\n", bundle.BundleName, bundle.TargetSchemaName, bundle.Category))
	b.WriteString(fmt.Sprintf("- **分支与提交**: `%s -> %s` (`%s`)\n\n", run.SourceBranch, run.TargetBranch, shortSHA(run.HeadCommitSHA)))

	compileCell := "✅ 通过"
	if !compilePassed {
		compileCell = "❌ 失败"
	}
	b.WriteString("| 检查维度 | 状态 / 计数 | 说明 |\n")
	b.WriteString("| :--- | :--- | :--- |\n")
	b.WriteString(fmt.Sprintf("| 1. `protocompile` 语法与导入链接 | %s | 纯 Go 虚拟树编译校验 |\n", compileCell))
	b.WriteString(fmt.Sprintf("| 2. 静态规范与标准词汇库 | `%d` 错误 / `%d` 警告 | 命名/注释/包规范与词根校验 |\n", lintErrors, lintWarnings))
	b.WriteString(fmt.Sprintf("| 3. AST 结构体重复探测 | `%d` 组 | Jaccard >= 0.85 三护栏去重 |\n", dupGroups))
	b.WriteString(fmt.Sprintf("| 4. `F-26` 破坏性变更与下游影响 | `%d` 项违规 / 波及 `%d` 模块 | 含 `BASE_LIB` 反向 import 穿透 |\n\n", len(breaking), len(affected)))

	if !compilePassed && compileErr != "" {
		b.WriteString("#### ❌ 编译错误诊断\n```text\n" + compileErr + "\n```\n\n")
	}
	if len(breaking) > 0 {
		b.WriteString("#### 🚨 破坏性契约变更明细\n")
		for _, v := range breaking {
			b.WriteString(fmt.Sprintf("- **[%s]** `%s` (`%s`): %s\n", v.Rule, v.Symbol, v.ProtoFile, v.Message))
		}
		b.WriteString("\n")
	}
	if len(affected) > 0 {
		b.WriteString("#### 📡 波及下游消费模块与负责人\n")
		for _, m := range affected {
			b.WriteString(fmt.Sprintf("- `%s` (%s) — 架构进程: `%s`, 负责人: **%s**\n",
				m.ModuleCode, m.ModuleName, m.ArchProcessCode, m.OwnerName))
		}
		b.WriteString("\n")
	}
	if report != nil && len(report.Violations) > 0 {
		b.WriteString("#### 🔍 静态检查与词汇建议 (Top 5)\n")
		limit := len(report.Violations)
		if limit > 5 {
			limit = 5
		}
		for i := 0; i < limit; i++ {
			v := report.Violations[i]
			b.WriteString(fmt.Sprintf("- `[%s]` `%s:%d` **%s**: %s", v.Severity, v.ProtoFile, v.FileLine, v.RuleCode, v.Message))
			if v.Suggestion != "" {
				b.WriteString(fmt.Sprintf(" *(建议: %s)*", v.Suggestion))
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func shortSHA(sha string) string {
	trimmed := strings.TrimSpace(sha)
	if len(trimmed) > 8 {
		return trimmed[:8]
	}
	return trimmed
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func nonNilBreaking(in []decoder.BreakingRuleViolation) []decoder.BreakingRuleViolation {
	if in == nil {
		return []decoder.BreakingRuleViolation{}
	}
	return in
}

func nonNilAffected(in []decoder.AffectedDependent) []decoder.AffectedDependent {
	if in == nil {
		return []decoder.AffectedDependent{}
	}
	return in
}
