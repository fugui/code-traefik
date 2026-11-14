package gitclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"code-traefik/internal/server/decoder"
	"code-traefik/internal/server/linter"
	"code-traefik/internal/server/protodoc"
	"code-traefik/internal/server/security"
	"code-traefik/models"

	"gorm.io/gorm"
)

var (
	ErrGitRepoNotFound   = errors.New("未找到指定的 Git 仓库配置")
	ErrGitBundleNotFound = errors.New("未找到指定的 Monorepo Proto Bundle 配置")
)

var packageLinePattern = regexp.MustCompile(`(?m)^\s*package\s+([a-zA-Z0-9_.]+)\s*;`)

// GitSyncEventPayload 异步/手动同步完成后向 SSE 推送的标准化载荷 (API §2.7)
type GitSyncEventPayload struct {
	RepoID           uint64  `json:"repo_id"`
	RepoName         string  `json:"repo_name"`
	BundleID         *uint64 `json:"bundle_id,omitempty"`
	BundleName       string  `json:"bundle_name,omitempty"`
	TargetSchemaName string  `json:"target_schema_name,omitempty"`
	Branch           string  `json:"branch"`
	CommitSHA        string  `json:"commit_sha"`
	AddedFiles       int     `json:"added_files"`
	ModifiedFiles    int     `json:"modified_files"`
	DeletedFiles     int     `json:"deleted_files"`
	CompiledSchemaID *uint64 `json:"compiled_schema_id,omitempty"`
	LintReportID     *uint64 `json:"lint_report_id,omitempty"`
	Status           string  `json:"status"`
	Message          string  `json:"message,omitempty"`
}

// EventBroadcaster SSE 事件广播回调（解耦 gitclient 与 handlers）
type EventBroadcaster func(event GitSyncEventPayload)

// SyncOptions 同步触发选项
type SyncOptions struct {
	Branch        string
	TriggerSource string // GIT_SYNC | MANUAL | WEBHOOK
	OperatorID    uint
	ForceCompile  bool
	PreferredMode string // "" | "rest" | "memfs"
	Broadcaster   EventBroadcaster
}

// SyncBundleResult 单个 Monorepo Bundle 的同步、编译、文档提取与静态扫描汇总结果
type SyncBundleResult struct {
	BundleID         uint64  `json:"bundle_id"`
	BundleName       string  `json:"bundle_name"`
	TargetSchemaName string  `json:"target_schema_name"`
	Category         string  `json:"category"`
	Branch           string  `json:"branch"`
	CommitSHA        string  `json:"commit_sha"`
	TotalRemoteFiles int     `json:"total_remote_files"`
	AddedFiles       int     `json:"added_files"`
	ModifiedFiles    int     `json:"modified_files"`
	UnchangedFiles   int     `json:"unchanged_files"`
	DeletedFiles     int     `json:"deleted_files"`
	CompiledSchemaID *uint64 `json:"compiled_schema_id,omitempty"`
	CompileSkipped   bool    `json:"compile_skipped"`
	CompileError     string  `json:"compile_error,omitempty"`
	LintReportID     *uint64 `json:"lint_report_id,omitempty"`
}

// SyncRepoResult 仓库级同步输出聚合结构
type SyncRepoResult struct {
	RepoID        uint64             `json:"repo_id"`
	RepoName      string             `json:"repo_name"`
	Branch        string             `json:"branch"`
	CommitSHA     string             `json:"commit_sha"`
	Status        string             `json:"status"`
	BundleResults []SyncBundleResult `json:"bundle_results"`
	DurationMs    int64              `json:"duration_ms"`
}

// ConsistencyStatus SHA256 只读对账状态常量
const (
	ConsistencyInSync     = "in_sync"
	ConsistencyModified   = "modified"
	ConsistencyRemoteOnly = "remote_only"
	ConsistencyDBOnly     = "db_only"
)

// ConsistencySummary SHA256 对账聚合计数
type ConsistencySummary struct {
	InSync     int `json:"in_sync"`
	Modified   int `json:"modified"`
	RemoteOnly int `json:"remote_only"`
	DBOnly     int `json:"db_only"`
}

// ConsistencyFileItem 单文件只读 SHA256 对账明细
type ConsistencyFileItem struct {
	BundleID         uint64 `json:"bundle_id"`
	BundleName       string `json:"bundle_name"`
	TargetSchemaName string `json:"target_schema_name"`
	FilePath         string `json:"file_path"`
	Status           string `json:"status"` // in_sync | modified | remote_only | db_only
	RemoteSHA256     string `json:"remote_sha256,omitempty"`
	DBSHA256         string `json:"db_sha256,omitempty"`
	DBVersion        int    `json:"db_version,omitempty"`
}

// ConsistencyReport GitRepo 与本地 DB 只读对账响应结构
type ConsistencyReport struct {
	RepoID    uint64                `json:"repo_id"`
	RepoName  string                `json:"repo_name"`
	Branch    string                `json:"branch"`
	CommitSHA string                `json:"commit_sha"`
	Summary   ConsistencySummary    `json:"summary"`
	Items     []ConsistencyFileItem `json:"items"`
}

// SlicedProtoFile 按 Bundle 子目录切片并清洗后的只读源码条目
type SlicedProtoFile struct {
	FilePath     string
	RepoFilePath string
	PackageName  string
	Content      []byte
	SHA256       string
	LineCount    int
}

// SyncService Git-Only 契约无状态同步与只读对账核心引擎
type SyncService struct {
	db          *gorm.DB
	reg         *decoder.Registry
	broadcaster EventBroadcaster
}

// NewSyncService 初始化 Git 同步服务
func NewSyncService(db *gorm.DB, reg *decoder.Registry, broadcaster EventBroadcaster) *SyncService {
	return &SyncService{
		db:          db,
		reg:         reg,
		broadcaster: broadcaster,
	}
}

func loadOutboundTrustedCIDRs(db *gorm.DB) string {
	if db == nil {
		return ""
	}
	var setting models.TraefikSetting
	if err := db.Where("setting_key = ?", "outbound_trusted_cidrs").Limit(1).Find(&setting).Error; err != nil {
		return ""
	}
	return strings.TrimSpace(setting.SettingVal)
}

func (s *SyncService) fetchRemoteSnapshot(
	ctx context.Context,
	repo *models.GitRepo,
	branch string,
	subdir string,
	preferredMode string,
) (*RemoteCommitSnapshot, error) {
	dialer, err := security.NewSafeDialer(loadOutboundTrustedCIDRs(s.db))
	if err != nil {
		return nil, fmt.Errorf("初始化 SSRF 出站白名单守卫失败: %w", err)
	}
	creds, err := repo.DecryptCredentials()
	if err != nil {
		return nil, fmt.Errorf("解密 Git 仓库访问凭证失败: %w", err)
	}

	mode := strings.ToLower(strings.TrimSpace(preferredMode))
	useMemFS := mode == "memfs" ||
		repo.Provider == models.GitProviderGeneric ||
		repo.AuthType == models.GitAuthSSHKey

	if useMemFS {
		return fetchViaMemFS(ctx, dialer, repo, creds, branch, subdir)
	}

	snap, restErr := fetchViaRESTAPI(ctx, dialer, repo, creds, branch, subdir)
	if restErr != nil {
		if errors.Is(restErr, ErrRESTFallbackAllowed) && mode == "" && strings.HasPrefix(strings.ToLower(repo.RepoURL), "http") {
			return fetchViaMemFS(ctx, dialer, repo, creds, branch, subdir)
		}
		return nil, restErr
	}
	return snap, nil
}

// ListBranches 无状态枚举远端分支列表
func (s *SyncService) ListBranches(ctx context.Context, repoID uint64) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("SyncService 未初始化")
	}
	var repo models.GitRepo
	if err := s.db.First(&repo, repoID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrGitRepoNotFound
		}
		return nil, err
	}
	dialer, err := security.NewSafeDialer(loadOutboundTrustedCIDRs(s.db))
	if err != nil {
		return nil, fmt.Errorf("初始化 SSRF 出站白名单守卫失败: %w", err)
	}
	creds, err := repo.DecryptCredentials()
	if err != nil {
		return nil, fmt.Errorf("解密 Git 仓库访问凭证失败: %w", err)
	}

	if repo.Provider == models.GitProviderGeneric || repo.AuthType == models.GitAuthSSHKey {
		return listBranchesViaMemFS(ctx, dialer, &repo, creds)
	}
	branches, restErr := listBranchesViaREST(ctx, dialer, &repo, creds)
	if restErr != nil && errors.Is(restErr, ErrRESTFallbackAllowed) && strings.HasPrefix(strings.ToLower(repo.RepoURL), "http") {
		return listBranchesViaMemFS(ctx, dialer, &repo, creds)
	}
	return branches, restErr
}

// SyncRepo 同步指定 GitRepo 下的全部 Monorepo Bundle（严格按 BASE_LIB -> BUSINESS -> GATEWAY 顺序编译）
func (s *SyncService) SyncRepo(ctx context.Context, repoID uint64, opts SyncOptions) (*SyncRepoResult, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("SyncService 未初始化")
	}
	started := time.Now()

	var repo models.GitRepo
	if err := s.db.First(&repo, repoID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrGitRepoNotFound
		}
		return nil, err
	}

	branch := strings.TrimSpace(opts.Branch)
	if branch == "" {
		branch = strings.TrimSpace(repo.DefaultBranch)
		if branch == "" {
			branch = "main"
		}
	}

	var bundles []models.GitProtoBundle
	if err := s.db.Where("git_repo_id = ?", repo.ID).
		Order("CASE WHEN category = 'BASE_LIB' THEN 0 WHEN category = 'BUSINESS' THEN 1 ELSE 2 END, id ASC").
		Find(&bundles).Error; err != nil {
		return nil, err
	}
	if len(bundles) == 0 {
		return nil, errors.New("该 Git 仓库尚未配置任何 Proto Bundle")
	}

	_ = s.db.Model(&repo).Updates(map[string]any{
		"last_sync_status": models.GitSyncStatusRunning,
		"last_sync_error":  "",
	}).Error

	snap, fetchErr := s.fetchRemoteSnapshot(ctx, &repo, branch, "", opts.PreferredMode)
	if fetchErr != nil {
		now := time.Now().UTC()
		_ = s.db.Model(&repo).Updates(map[string]any{
			"last_sync_at":     &now,
			"last_sync_status": models.GitSyncStatusFailed,
			"last_sync_error":  fetchErr.Error(),
		}).Error
		return nil, fetchErr
	}

	results := make([]SyncBundleResult, 0, len(bundles))
	var partialErrs []string
	for _, b := range bundles {
		bres, bErr := s.applyBundleSnapshot(ctx, &repo, &b, branch, snap, opts)
		if bErr != nil {
			partialErrs = append(partialErrs, fmt.Sprintf("[%s]: %s", b.BundleName, bErr.Error()))
		}
		if bres != nil {
			results = append(results, *bres)
		}
	}

	now := time.Now().UTC()
	status := models.GitSyncStatusSuccess
	errText := ""
	if len(partialErrs) > 0 {
		status = models.GitSyncStatusFailed
		errText = strings.Join(partialErrs, "; ")
	}
	if err := s.db.Model(&repo).Updates(map[string]any{
		"last_sync_at":     &now,
		"last_sync_commit": snap.CommitSHA,
		"last_sync_status": status,
		"last_sync_error":  errText,
	}).Error; err != nil {
		return nil, err
	}

	if status == models.GitSyncStatusFailed && len(bundles) == len(partialErrs) {
		return &SyncRepoResult{
			RepoID:        repo.ID,
			RepoName:      repo.Name,
			Branch:        branch,
			CommitSHA:     snap.CommitSHA,
			Status:        status,
			BundleResults: results,
			DurationMs:    time.Since(started).Milliseconds(),
		}, errors.New(errText)
	}

	return &SyncRepoResult{
		RepoID:        repo.ID,
		RepoName:      repo.Name,
		Branch:        branch,
		CommitSHA:     snap.CommitSHA,
		Status:        status,
		BundleResults: results,
		DurationMs:    time.Since(started).Milliseconds(),
	}, nil
}

// SyncBundle 单独同步并编译指定 ID 的 Monorepo Bundle
func (s *SyncService) SyncBundle(ctx context.Context, bundleID uint64, opts SyncOptions) (*SyncBundleResult, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("SyncService 未初始化")
	}
	var bundle models.GitProtoBundle
	if err := s.db.First(&bundle, bundleID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrGitBundleNotFound
		}
		return nil, err
	}
	var repo models.GitRepo
	if err := s.db.First(&repo, bundle.GitRepoID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrGitRepoNotFound
		}
		return nil, err
	}
	branch := strings.TrimSpace(opts.Branch)
	if branch == "" {
		branch = strings.TrimSpace(repo.DefaultBranch)
		if branch == "" {
			branch = "main"
		}
	}

	snap, err := s.fetchRemoteSnapshot(ctx, &repo, branch, bundle.SubdirPath, opts.PreferredMode)
	if err != nil {
		return nil, err
	}
	res, applyErr := s.applyBundleSnapshot(ctx, &repo, &bundle, branch, snap, opts)
	now := time.Now().UTC()
	if applyErr != nil {
		_ = s.db.Model(&repo).Updates(map[string]any{
			"last_sync_at":     &now,
			"last_sync_commit": snap.CommitSHA,
			"last_sync_status": models.GitSyncStatusFailed,
			"last_sync_error":  applyErr.Error(),
		}).Error
		return res, applyErr
	}
	_ = s.db.Model(&repo).Updates(map[string]any{
		"last_sync_at":     &now,
		"last_sync_commit": snap.CommitSHA,
		"last_sync_status": models.GitSyncStatusSuccess,
		"last_sync_error":  "",
	}).Error
	return res, nil
}

func (s *SyncService) applyBundleSnapshot(
	ctx context.Context,
	repo *models.GitRepo,
	bundle *models.GitProtoBundle,
	branch string,
	snap *RemoteCommitSnapshot,
	opts SyncOptions,
) (*SyncBundleResult, error) {
	sliced, _, err := sliceBundleFiles(snap, *bundle)
	if err != nil {
		return nil, err
	}

	res := &SyncBundleResult{
		BundleID:         bundle.ID,
		BundleName:       bundle.BundleName,
		TargetSchemaName: bundle.TargetSchemaName,
		Category:         bundle.Category,
		Branch:           branch,
		CommitSHA:        snap.CommitSHA,
		TotalRemoteFiles: len(sliced),
	}

	touchedVersionIDs := make([]uint64, 0, len(sliced))
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if lockErr := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", fmt.Sprintf("traefik_git_sync_%d", bundle.ID)).Error; lockErr != nil {
			return lockErr
		}

		var existingFiles []models.ProtoFile
		if findErr := tx.Where("bundle_id = ? AND is_deleted = false", bundle.ID).Find(&existingFiles).Error; findErr != nil {
			return findErr
		}
		existingByPath := make(map[string]*models.ProtoFile, len(existingFiles))
		for i := range existingFiles {
			existingByPath[existingFiles[i].FilePath] = &existingFiles[i]
		}

		remoteSet := make(map[string]struct{}, len(sliced))
		versionTag := shortCommitTag(snap.CommitSHA)
		sourceType := models.ProtoSourceGitSync

		for _, rf := range sliced {
			remoteSet[rf.FilePath] = struct{}{}
			cur := existingByPath[rf.FilePath]
			if cur == nil {
				pf := models.ProtoFile{
					GitRepoID:        repo.ID,
					BundleID:         bundle.ID,
					TargetSchemaName: bundle.TargetSchemaName,
					Category:         bundle.Category,
					FilePath:         rf.FilePath,
					RepoFilePath:     rf.RepoFilePath,
					PackageName:      rf.PackageName,
					ContentSHA256:    rf.SHA256,
					LineCount:        rf.LineCount,
					LatestVersionNum: 1,
					LatestCommitSHA:  snap.CommitSHA,
					IsDeleted:        false,
				}
				if createErr := tx.Create(&pf).Error; createErr != nil {
					return createErr
				}
				ver := models.ProtoFileVersion{
					ProtoFileID:   pf.ID,
					Version:       1,
					VersionTag:    versionTag,
					SourceType:    sourceType,
					CommitSHA:     snap.CommitSHA,
					CommitMessage: snap.CommitMessage,
					AuthorName:    snap.AuthorName,
					AuthorEmail:   snap.AuthorEmail,
					ContentText:   string(rf.Content),
					ContentSHA256: rf.SHA256,
					LineCount:     rf.LineCount,
					CommittedAt:   snap.CommittedAt,
				}
				if verErr := tx.Create(&ver).Error; verErr != nil {
					return verErr
				}
				if updErr := tx.Model(&pf).Update("latest_version_id", ver.ID).Error; updErr != nil {
					return updErr
				}
				touchedVersionIDs = append(touchedVersionIDs, ver.ID)
				res.AddedFiles++
				continue
			}

			if cur.ContentSHA256 == rf.SHA256 {
				// 内容 SHA256 完全一致时保持版本历史幂等，不重复追加 Version
				updates := map[string]any{}
				if cur.PackageName != rf.PackageName {
					updates["package_name"] = rf.PackageName
				}
				if cur.RepoFilePath != rf.RepoFilePath {
					updates["repo_file_path"] = rf.RepoFilePath
				}
				if len(updates) > 0 {
					if updErr := tx.Model(cur).Updates(updates).Error; updErr != nil {
						return updErr
					}
				}
				res.UnchangedFiles++
				continue
			}

			var maxVer int
			if maxErr := tx.Model(&models.ProtoFileVersion{}).
				Where("proto_file_id = ?", cur.ID).
				Select("COALESCE(MAX(version), 0)").
				Scan(&maxVer).Error; maxErr != nil {
				return maxErr
			}
			nextVer := maxVer + 1
			ver := models.ProtoFileVersion{
				ProtoFileID:   cur.ID,
				Version:       nextVer,
				VersionTag:    versionTag,
				SourceType:    sourceType,
				CommitSHA:     snap.CommitSHA,
				CommitMessage: snap.CommitMessage,
				AuthorName:    snap.AuthorName,
				AuthorEmail:   snap.AuthorEmail,
				ContentText:   string(rf.Content),
				ContentSHA256: rf.SHA256,
				LineCount:     rf.LineCount,
				CommittedAt:   snap.CommittedAt,
			}
			if verErr := tx.Create(&ver).Error; verErr != nil {
				return verErr
			}
			if updErr := tx.Model(cur).Updates(map[string]any{
				"repo_file_path":     rf.RepoFilePath,
				"package_name":       rf.PackageName,
				"content_sha256":     rf.SHA256,
				"line_count":         rf.LineCount,
				"latest_version_id":  ver.ID,
				"latest_version_num": nextVer,
				"latest_commit_sha":  snap.CommitSHA,
			}).Error; updErr != nil {
				return updErr
			}
			touchedVersionIDs = append(touchedVersionIDs, ver.ID)
			res.ModifiedFiles++
		}

		// 对远端已被移除的文件执行软删除（保留历史 Commit 快照）
		for p, existing := range existingByPath {
			if _, ok := remoteSet[p]; !ok {
				if delErr := tx.Model(existing).Update("is_deleted", true).Error; delErr != nil {
					return delErr
				}
				res.DeletedFiles++
			}
		}

		now := time.Now().UTC()
		return tx.Model(bundle).Updates(map[string]any{
			"last_sync_commit": snap.CommitSHA,
			"last_sync_at":     &now,
		}).Error
	})
	if err != nil {
		return res, err
	}

	// 自动触发纯 Go protocompile 编译入库与文档符号索引重建
	shouldCompile := bundle.AutoCompile && len(sliced) > 0 &&
		(opts.ForceCompile || bundle.LastSchemaID == nil || res.AddedFiles > 0 || res.ModifiedFiles > 0 || res.DeletedFiles > 0)
	if !shouldCompile {
		res.CompileSkipped = true
		res.CompiledSchemaID = bundle.LastSchemaID
	} else if s.reg != nil {
		filesMap := make(map[string]string, len(sliced))
		for _, sf := range sliced {
			filesMap[sf.FilePath] = string(sf.Content)
		}
		repoIDCopy := repo.ID
		bundleIDCopy := bundle.ID
		schema, _, _, compErr := s.reg.CompileAndSaveSchema(ctx, decoder.CompileProtoBundleInput{
			Name:             bundle.TargetSchemaName,
			Category:         bundle.Category,
			VersionTag:       shortCommitTag(snap.CommitSHA),
			Description:      strings.TrimSpace(snap.CommitMessage),
			Files:            filesMap,
			ProtoIncludeDirs: bundle.IncludeDirs(),
			EntryPoints:      bundle.EntryFiles(),
			AutoActivate:     bundle.AutoActivate,
			SourceType:       models.ProtoSourceGitSync,
			GitRepoID:        &repoIDCopy,
			SourceBundleID:   &bundleIDCopy,
			SourceCommitSHA:  snap.CommitSHA,
			OperatorID:       opts.OperatorID,
			SourceProvider:   decoder.NewDBProtoSourceProvider(s.db),
		})
		if compErr != nil {
			res.CompileError = compErr.Error()
		} else if schema != nil {
			scID := schema.ID
			res.CompiledSchemaID = &scID
			if len(touchedVersionIDs) > 0 {
				if bindErr := s.db.Model(&models.ProtoFileVersion{}).
					Where("id IN ?", touchedVersionIDs).
					Update("schema_id", scID).Error; bindErr != nil {
					log.Printf("[GitSync] 关联 Schema #%d 到文件版本失败: %v", scID, bindErr)
				}
			}
			if updErr := s.db.Model(bundle).Update("last_schema_id", scID).Error; updErr != nil {
				log.Printf("[GitSync] 更新 Bundle #%d last_schema_id 失败: %v", bundle.ID, updErr)
			}
			if _, docErr := protodoc.RebuildForSchema(s.db, schema); docErr != nil {
				log.Printf("[GitSync] 重建 Schema #%d 文档符号失败: %v", scID, docErr)
			}
		}
	}

	// 自动触发静态检查扫描（Linter CheckReport）
	if bundle.LintOnSync && len(sliced) > 0 {
		lintSources := make([]linter.SourceInput, 0, len(sliced))
		var scID uint64
		if res.CompiledSchemaID != nil {
			scID = *res.CompiledSchemaID
		} else if bundle.LastSchemaID != nil {
			scID = *bundle.LastSchemaID
		}
		for _, sf := range sliced {
			lintSources = append(lintSources, linter.SourceInput{
				ProtoFile: sf.FilePath,
				Content:   string(sf.Content),
				Category:  bundle.Category,
				SchemaID:  scID,
			})
		}
		repoIDCopy := repo.ID
		bundleIDCopy := bundle.ID
		var scPtr *uint64
		if scID > 0 {
			scPtr = &scID
		}
		report, lintErr := linter.RunAndPersistScan(ctx, linter.ScanRequest{
			DB:                s.db,
			TriggerSource:     models.CheckTriggerGitSync,
			TriggeredBy:       opts.OperatorID,
			GitRepoID:         &repoIDCopy,
			BundleID:          &bundleIDCopy,
			SchemaID:          scPtr,
			Sources:           lintSources,
			IncludeDuplicates: false,
			IncludeLLM:        false,
		})
		if lintErr != nil {
			log.Printf("[GitSync] Bundle #%d 自动 Linter 扫描失败: %v", bundle.ID, lintErr)
		} else if report != nil {
			rID := report.ID
			res.LintReportID = &rID
		}
	}

	broadcaster := opts.Broadcaster
	if broadcaster == nil {
		broadcaster = s.broadcaster
	}
	if broadcaster != nil {
		bID := bundle.ID
		status := models.GitSyncStatusSuccess
		msg := fmt.Sprintf("Bundle [%s] 同步完成 (+%d ~%d -%d)", bundle.BundleName, res.AddedFiles, res.ModifiedFiles, res.DeletedFiles)
		if res.CompileError != "" {
			status = models.GitSyncStatusFailed
			msg = res.CompileError
		}
		broadcaster(GitSyncEventPayload{
			RepoID:           repo.ID,
			RepoName:         repo.Name,
			BundleID:         &bID,
			BundleName:       bundle.BundleName,
			TargetSchemaName: bundle.TargetSchemaName,
			Branch:           branch,
			CommitSHA:        snap.CommitSHA,
			AddedFiles:       res.AddedFiles,
			ModifiedFiles:    res.ModifiedFiles,
			DeletedFiles:     res.DeletedFiles,
			CompiledSchemaID: res.CompiledSchemaID,
			LintReportID:     res.LintReportID,
			Status:           status,
			Message:          msg,
		})
	}

	if res.CompileError != "" {
		return res, errors.New(res.CompileError)
	}
	return res, nil
}

// ComputeConsistency 执行无状态只读 SHA256 对账（严禁写入任何数据库表）
func (s *SyncService) ComputeConsistency(
	ctx context.Context,
	repoID uint64,
	bundleID *uint64,
	branch string,
) (*ConsistencyReport, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("SyncService 未初始化")
	}
	var repo models.GitRepo
	if err := s.db.First(&repo, repoID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrGitRepoNotFound
		}
		return nil, err
	}

	targetBranch := strings.TrimSpace(branch)
	if targetBranch == "" {
		targetBranch = strings.TrimSpace(repo.DefaultBranch)
		if targetBranch == "" {
			targetBranch = "main"
		}
	}

	var bundles []models.GitProtoBundle
	q := s.db.Where("git_repo_id = ?", repo.ID)
	if bundleID != nil && *bundleID > 0 {
		q = q.Where("id = ?", *bundleID)
	}
	if err := q.Order("id ASC").Find(&bundles).Error; err != nil {
		return nil, err
	}
	if len(bundles) == 0 {
		return nil, ErrGitBundleNotFound
	}

	subdir := ""
	if bundleID != nil && *bundleID > 0 {
		subdir = bundles[0].SubdirPath
	}
	snap, err := s.fetchRemoteSnapshot(ctx, &repo, targetBranch, subdir, "")
	if err != nil {
		return nil, err
	}

	report := &ConsistencyReport{
		RepoID:    repo.ID,
		RepoName:  repo.Name,
		Branch:    targetBranch,
		CommitSHA: snap.CommitSHA,
		Items:     make([]ConsistencyFileItem, 0),
	}

	for _, b := range bundles {
		sliced, _, sliceErr := sliceBundleFiles(snap, b)
		if sliceErr != nil {
			return nil, sliceErr
		}
		remoteByPath := make(map[string]SlicedProtoFile, len(sliced))
		for _, sf := range sliced {
			remoteByPath[sf.FilePath] = sf
		}

		var dbFiles []models.ProtoFile
		if findErr := s.db.Where("bundle_id = ? AND is_deleted = false", b.ID).Order("file_path ASC").Find(&dbFiles).Error; findErr != nil {
			return nil, findErr
		}
		dbByPath := make(map[string]models.ProtoFile, len(dbFiles))
		for _, df := range dbFiles {
			dbByPath[df.FilePath] = df
		}

		allPathsSet := make(map[string]struct{}, len(remoteByPath)+len(dbByPath))
		for p := range remoteByPath {
			allPathsSet[p] = struct{}{}
		}
		for p := range dbByPath {
			allPathsSet[p] = struct{}{}
		}
		allPaths := make([]string, 0, len(allPathsSet))
		for p := range allPathsSet {
			allPaths = append(allPaths, p)
		}
		sort.Strings(allPaths)

		for _, p := range allPaths {
			rf, hasRemote := remoteByPath[p]
			df, hasDB := dbByPath[p]
			item := ConsistencyFileItem{
				BundleID:         b.ID,
				BundleName:       b.BundleName,
				TargetSchemaName: b.TargetSchemaName,
				FilePath:         p,
			}
			switch {
			case hasRemote && hasDB && rf.SHA256 == df.ContentSHA256:
				item.Status = ConsistencyInSync
				item.RemoteSHA256 = rf.SHA256
				item.DBSHA256 = df.ContentSHA256
				item.DBVersion = df.LatestVersionNum
				report.Summary.InSync++
			case hasRemote && hasDB:
				item.Status = ConsistencyModified
				item.RemoteSHA256 = rf.SHA256
				item.DBSHA256 = df.ContentSHA256
				item.DBVersion = df.LatestVersionNum
				report.Summary.Modified++
			case hasRemote:
				item.Status = ConsistencyRemoteOnly
				item.RemoteSHA256 = rf.SHA256
				report.Summary.RemoteOnly++
			default:
				item.Status = ConsistencyDBOnly
				item.DBSHA256 = df.ContentSHA256
				item.DBVersion = df.LatestVersionNum
				report.Summary.DBOnly++
			}
			report.Items = append(report.Items, item)
		}
	}

	return report, nil
}

// SanitizeSubdirPath 规范并校验 Monorepo SubdirPath（仅允许 '.' 或 POSIX 相对安全子目录）
func SanitizeSubdirPath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "." || trimmed == "/" {
		return ".", nil
	}
	if strings.ContainsAny(trimmed, "\x00\\") {
		return "", fmt.Errorf("%w: subdir_path 包含非法控制字符或反斜杠 (%q)", decoder.ErrPathTraversalDenied, raw)
	}
	if strings.HasPrefix(trimmed, "/") {
		return "", fmt.Errorf("%w: subdir_path 禁止绝对路径 (%q)", decoder.ErrPathTraversalDenied, raw)
	}
	for _, seg := range strings.Split(trimmed, "/") {
		if seg == ".." {
			return "", fmt.Errorf("%w: subdir_path 禁止 '..' (%q)", decoder.ErrPathTraversalDenied, raw)
		}
	}
	cleaned := strings.Trim(path.Clean(trimmed), "/")
	if cleaned == "" || cleaned == "." {
		return ".", nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("%w: subdir_path 非法 (%q)", decoder.ErrPathTraversalDenied, raw)
	}
	return cleaned, nil
}

func sliceBundleFiles(snap *RemoteCommitSnapshot, bundle models.GitProtoBundle) ([]SlicedProtoFile, []string, error) {
	if snap == nil {
		return nil, nil, errors.New("RemoteCommitSnapshot 不能为空")
	}
	subdir, err := SanitizeSubdirPath(bundle.SubdirPath)
	if err != nil {
		return nil, nil, err
	}

	includeGlobs := bundle.FileGlobs()
	excludeGlobs := bundle.ExcludeGlobs()

	out := make([]SlicedProtoFile, 0, len(snap.Files))
	paths := make([]string, 0, len(snap.Files))
	var totalBytes int64

	for _, rf := range snap.Files {
		rawRepoPath := strings.TrimSpace(rf.RepoFilePath)
		if rawRepoPath == "" {
			rawRepoPath = strings.TrimSpace(rf.FilePath)
		}
		if !strings.HasSuffix(rawRepoPath, ".proto") {
			continue
		}
		// 无论是否落在本 Bundle 子目录下，任何疑似穿越路径均立即拒绝
		repoRel, sErr := decoder.SanitizeProtoRelPath(rawRepoPath, true)
		if sErr != nil {
			return nil, nil, sErr
		}

		relPath := repoRel
		if subdir != "." {
			prefix := subdir + "/"
			if !strings.HasPrefix(repoRel, prefix) {
				continue
			}
			relPath = strings.TrimPrefix(repoRel, prefix)
		}
		cleanRel, relErr := decoder.SanitizeProtoRelPath(relPath, true)
		if relErr != nil {
			return nil, nil, relErr
		}

		if len(includeGlobs) > 0 && !matchAnyProtoGlob(includeGlobs, cleanRel) {
			continue
		}
		if len(excludeGlobs) > 0 && matchAnyProtoGlob(excludeGlobs, cleanRel) {
			continue
		}

		contentBytes := rf.Content
		totalBytes += int64(len(contentBytes))
		if totalBytes > int64(decoder.MaxDescriptorFileSize) {
			return nil, nil, fmt.Errorf("Bundle [%s] 的 .proto 源码总大小超出 16MB (%d Bytes) 上限", bundle.BundleName, decoder.MaxDescriptorFileSize)
		}
		shaHex := rf.SHA256
		if shaHex == "" {
			sum := sha256.Sum256(contentBytes)
			shaHex = hex.EncodeToString(sum[:])
		}
		contentStr := string(contentBytes)
		out = append(out, SlicedProtoFile{
			FilePath:     cleanRel,
			RepoFilePath: repoRel,
			PackageName:  parseProtoPackageName(contentStr),
			Content:      contentBytes,
			SHA256:       shaHex,
			LineCount:    countNormalizedLines(contentStr),
		})
		paths = append(paths, cleanRel)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].FilePath < out[j].FilePath })
	sort.Strings(paths)
	return out, paths, nil
}

func matchAnyProtoGlob(globs []string, relPath string) bool {
	for _, g := range globs {
		p := strings.TrimSpace(g)
		if p == "" {
			continue
		}
		if matchProtoGlob(p, relPath) {
			return true
		}
	}
	return false
}

func matchProtoGlob(pattern, relPath string) bool {
	if pattern == "*" || pattern == "**" || pattern == "*.proto" || pattern == "**/*.proto" {
		return strings.HasSuffix(relPath, ".proto")
	}
	if strings.Contains(pattern, "**") {
		parts := strings.Split(pattern, "**")
		if len(parts) == 2 {
			prefix := strings.TrimRight(parts[0], "/")
			suffix := strings.TrimLeft(parts[1], "/")
			if prefix != "" && !strings.HasPrefix(relPath, prefix+"/") && relPath != prefix {
				return false
			}
			if suffix == "" {
				return true
			}
			if ok, err := path.Match(suffix, path.Base(relPath)); err == nil && ok {
				return true
			}
			return strings.HasSuffix(relPath, strings.TrimPrefix(suffix, "*"))
		}
	}
	if !strings.Contains(pattern, "/") {
		ok, err := path.Match(pattern, path.Base(relPath))
		return err == nil && ok
	}
	ok, err := path.Match(pattern, relPath)
	return err == nil && ok
}

func parseProtoPackageName(content string) string {
	m := packageLinePattern.FindStringSubmatch(content)
	if len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func countNormalizedLines(content string) int {
	trimmed := strings.TrimSuffix(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "\n"))
}

func shortCommitTag(commitSHA string) string {
	trimmed := strings.TrimSpace(commitSHA)
	if len(trimmed) > 7 {
		return "git-" + trimmed[:7]
	}
	if trimmed == "" {
		return "git-sync"
	}
	return "git-" + trimmed
}
