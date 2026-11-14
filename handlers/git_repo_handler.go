package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	commonModels "code-common/backend/models"
	"code-traefik/internal/server/decoder"
	"code-traefik/internal/server/gitclient"
	"code-traefik/internal/server/security"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// activeGitRepoSyncs 进程内按仓库维度的同步互斥锁，防止单仓并发同步踩踏
var activeGitRepoSyncs sync.Map // map[uint64]struct{}

// GitProtoBundleDTO Monorepo Bundle 标准化响应模型（已将 JSONB 数组字段直接解码为字符串切片）
type GitProtoBundleDTO struct {
	ID               uint64     `json:"id"`
	GitRepoID        uint64     `json:"git_repo_id"`
	BundleName       string     `json:"bundle_name"`
	TargetSchemaName string     `json:"target_schema_name"`
	Category         string     `json:"category"`
	SubdirPath       string     `json:"subdir_path"`
	IncludeDirs      []string   `json:"include_dirs"`
	EntryFiles       []string   `json:"entry_files"`
	FileGlobs        []string   `json:"file_globs"`
	ExcludeGlobs     []string   `json:"exclude_globs"`
	AutoCompile      bool       `json:"auto_compile"`
	AutoActivate     bool       `json:"auto_activate"`
	MRAutoPublish    string     `json:"mr_auto_publish"`
	LintOnSync       bool       `json:"lint_on_sync"`
	LastSyncCommit   string     `json:"last_sync_commit"`
	LastSyncAt       *time.Time `json:"last_sync_at,omitempty"`
	LastSchemaID     *uint64    `json:"last_schema_id,omitempty"`
	Description      string     `json:"description"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// GitRepoResponseDTO 严格脱敏后的 GitRepo 接口输出模型（绝不返回任何明文或加密凭据）
type GitRepoResponseDTO struct {
	ID               uint64              `json:"id"`
	Name             string              `json:"name"`
	Provider         string              `json:"provider"`
	RepoURL          string              `json:"repo_url"`
	APIBaseURL       string              `json:"api_base_url"`
	DefaultBranch    string              `json:"default_branch"`
	AuthType         string              `json:"auth_type"`
	HasCredentials   bool                `json:"has_credentials"`
	CredentialHint   string              `json:"credential_hint"`
	HasWebhookSecret bool                `json:"has_webhook_secret"`
	SyncMode         string              `json:"sync_mode"`
	PollIntervalSec  int                 `json:"poll_interval_sec"`
	AutoCompile      bool                `json:"auto_compile"`
	AutoActivate     bool                `json:"auto_activate"`
	LintOnSync       bool                `json:"lint_on_sync"`
	LastSyncAt       *time.Time          `json:"last_sync_at,omitempty"`
	LastSyncCommit   string              `json:"last_sync_commit"`
	LastSyncStatus   string              `json:"last_sync_status"`
	LastSyncError    string              `json:"last_sync_error"`
	Description      string              `json:"description"`
	CreatedBy        uint                `json:"created_by"`
	CreatedAt        time.Time           `json:"created_at"`
	UpdatedAt        time.Time           `json:"updated_at"`
	Bundles          []GitProtoBundleDTO `json:"bundles"`
}

type bundleUpsertPayload struct {
	BundleName       string   `json:"bundle_name"`
	Name             string   `json:"name"` // 兼容契约别名
	TargetSchemaName string   `json:"target_schema_name"`
	Category         string   `json:"category"`
	TargetCategory   string   `json:"target_category"` // 兼容契约别名
	SubdirPath       string   `json:"subdir_path"`
	ProtoRootDir     string   `json:"proto_root_dir"` // 兼容契约别名
	IncludeDirs      []string `json:"include_dirs"`
	ProtoIncludeDirs []string `json:"proto_include_dirs"` // 兼容契约别名
	EntryFiles       []string `json:"entry_files"`
	FileGlobs        []string `json:"file_globs"`
	ExcludeGlobs     []string `json:"exclude_globs"`
	AutoCompile      *bool    `json:"auto_compile"`
	AutoCompileAlias *bool    `json:"auto_compile_on_merge"`
	AutoActivate     *bool    `json:"auto_activate"`
	AutoActAlias     *bool    `json:"auto_activate_on_merge"`
	MRAutoPublish    string   `json:"mr_auto_publish"`
	LintOnSync       *bool    `json:"lint_on_sync"`
	Description      string   `json:"description"`
}

type gitRepoCreateRequest struct {
	Name               string                `json:"name"`
	Provider           string                `json:"provider"`
	RepoURL            string                `json:"repo_url"`
	APIBaseURL         string                `json:"api_base_url"`
	WebBaseURL         string                `json:"web_base_url"` // 兼容契约别名
	DefaultBranch      string                `json:"default_branch"`
	AuthType           string                `json:"auth_type"`
	CredentialPlain    string                `json:"credential_plain"`
	WebhookSecretPlain string                `json:"webhook_secret_plain"`
	SyncMode           string                `json:"sync_mode"`
	PollIntervalSec    int                   `json:"poll_interval_sec"`
	AutoCompile        *bool                 `json:"auto_compile"`
	AutoActivate       *bool                 `json:"auto_activate"`
	LintOnSync         *bool                 `json:"lint_on_sync"`
	Description        string                `json:"description"`
	DefaultBundle      *bundleUpsertPayload  `json:"default_bundle"`
	Bundles            []bundleUpsertPayload `json:"bundles"`
	// 单仓顶层内联便捷参数
	TargetSchemaName string   `json:"target_schema_name"`
	TargetCategory   string   `json:"target_category"`
	SubdirPath       string   `json:"subdir_path"`
	ProtoRootDir     string   `json:"proto_root_dir"`
	IncludeDirs      []string `json:"include_dirs"`
	ProtoIncludeDirs []string `json:"proto_include_dirs"`
}

type gitRepoUpdateRequest struct {
	Name               *string `json:"name"`
	Provider           *string `json:"provider"`
	RepoURL            *string `json:"repo_url"`
	APIBaseURL         *string `json:"api_base_url"`
	WebBaseURL         *string `json:"web_base_url"`
	DefaultBranch      *string `json:"default_branch"`
	AuthType           *string `json:"auth_type"`
	CredentialPlain    *string `json:"credential_plain"`
	WebhookSecretPlain *string `json:"webhook_secret_plain"`
	SyncMode           *string `json:"sync_mode"`
	PollIntervalSec    *int    `json:"poll_interval_sec"`
	AutoCompile        *bool   `json:"auto_compile"`
	AutoActivate       *bool   `json:"auto_activate"`
	LintOnSync         *bool   `json:"lint_on_sync"`
	Description        *string `json:"description"`
}

type triggerGitSyncRequest struct {
	BundleID       *uint64 `json:"bundle_id"`
	Branch         string  `json:"branch"`
	ForceRecompile bool    `json:"force_recompile"`
}

func toGitProtoBundleDTO(b models.GitProtoBundle) GitProtoBundleDTO {
	return GitProtoBundleDTO{
		ID:               b.ID,
		GitRepoID:        b.GitRepoID,
		BundleName:       b.BundleName,
		TargetSchemaName: b.TargetSchemaName,
		Category:         b.Category,
		SubdirPath:       b.SubdirPath,
		IncludeDirs:      b.IncludeDirs(),
		EntryFiles:       b.EntryFiles(),
		FileGlobs:        b.FileGlobs(),
		ExcludeGlobs:     b.ExcludeGlobs(),
		AutoCompile:      b.AutoCompile,
		AutoActivate:     b.AutoActivate,
		MRAutoPublish:    b.MRAutoPublish,
		LintOnSync:       b.LintOnSync,
		LastSyncCommit:   b.LastSyncCommit,
		LastSyncAt:       b.LastSyncAt,
		LastSchemaID:     b.LastSchemaID,
		Description:      b.Description,
		CreatedAt:        b.CreatedAt,
		UpdatedAt:        b.UpdatedAt,
	}
}

func toGitRepoResponseDTO(repo models.GitRepo, bundles []models.GitProtoBundle) GitRepoResponseDTO {
	redacted := repo.ToRedactedDTO()
	bundleDTOs := make([]GitProtoBundleDTO, 0, len(bundles))
	for _, b := range bundles {
		bundleDTOs = append(bundleDTOs, toGitProtoBundleDTO(b))
	}
	return GitRepoResponseDTO{
		ID:               redacted.ID,
		Name:             redacted.Name,
		Provider:         redacted.Provider,
		RepoURL:          redacted.RepoURL,
		APIBaseURL:       redacted.APIBaseURL,
		DefaultBranch:    redacted.DefaultBranch,
		AuthType:         redacted.AuthType,
		HasCredentials:   redacted.HasCredentials,
		CredentialHint:   redacted.CredentialHint,
		HasWebhookSecret: redacted.HasWebhookSecret,
		SyncMode:         redacted.SyncMode,
		PollIntervalSec:  redacted.PollIntervalSec,
		AutoCompile:      redacted.AutoCompile,
		AutoActivate:     redacted.AutoActivate,
		LintOnSync:       redacted.LintOnSync,
		LastSyncAt:       redacted.LastSyncAt,
		LastSyncCommit:   redacted.LastSyncCommit,
		LastSyncStatus:   redacted.LastSyncStatus,
		LastSyncError:    redacted.LastSyncError,
		Description:      redacted.Description,
		CreatedBy:        redacted.CreatedBy,
		CreatedAt:        redacted.CreatedAt,
		UpdatedAt:        redacted.UpdatedAt,
		Bundles:          bundleDTOs,
	}
}

func loadSafeDialerFromSettings(db *gorm.DB) (*security.SafeDialer, error) {
	var setting models.TraefikSetting
	trusted := ""
	if db != nil {
		if err := db.Where("setting_key = ?", "outbound_trusted_cidrs").Limit(1).Find(&setting).Error; err == nil {
			trusted = strings.TrimSpace(setting.SettingVal)
		}
	}
	return security.NewSafeDialer(trusted)
}

func validateGitRepoEndpointURL(ctx context.Context, dialer *security.SafeDialer, rawRepoURL, rawAPIBaseURL string) error {
	trimmedRepo := strings.TrimSpace(rawRepoURL)
	if trimmedRepo == "" {
		return errors.New("repo_url 不能为空")
	}

	lowerRepo := strings.ToLower(trimmedRepo)
	switch {
	case strings.HasPrefix(lowerRepo, "http://") || strings.HasPrefix(lowerRepo, "https://"):
		if err := dialer.ValidateURL(ctx, trimmedRepo); err != nil {
			return err
		}
	case strings.HasPrefix(lowerRepo, "ssh://"):
		parsed, err := url.Parse(trimmedRepo)
		if err != nil {
			return fmt.Errorf("SSH repo_url 格式非法: %w", err)
		}
		host := parsed.Hostname()
		if host == "" {
			return errors.New("SSH repo_url 缺少主机名")
		}
		port := parsed.Port()
		if port == "" {
			port = "22"
		}
		if err := dialer.CheckAddress(ctx, "tcp", net.JoinHostPort(host, port)); err != nil {
			return err
		}
	case strings.HasPrefix(lowerRepo, "git@") && strings.Contains(trimmedRepo, ":"):
		afterAt := trimmedRepo[len("git@"):]
		colonIdx := strings.IndexByte(afterAt, ':')
		host := strings.TrimSpace(afterAt[:colonIdx])
		if host == "" {
			return errors.New("SCP 风格 Git URL 缺少主机名")
		}
		if err := dialer.CheckAddress(ctx, "tcp", net.JoinHostPort(host, "22")); err != nil {
			return err
		}
	default:
		return fmt.Errorf("不支持的 Git 仓库协议: 仅允许 http/https/ssh 或 git@host:path，拒绝 %q", rawRepoURL)
	}

	if strings.TrimSpace(rawAPIBaseURL) != "" {
		if err := dialer.ValidateURL(ctx, strings.TrimSpace(rawAPIBaseURL)); err != nil {
			return fmt.Errorf("api_base_url 校验失败: %w", err)
		}
	}
	return nil
}

func normalizeGitProvider(raw string) (string, error) {
	v := strings.ToUpper(strings.TrimSpace(raw))
	if v == "" {
		return models.GitProviderGitLab, nil
	}
	switch v {
	case models.GitProviderGitLab, models.GitProviderGitHub, models.GitProviderGitea, models.GitProviderGeneric:
		return v, nil
	default:
		return "", fmt.Errorf("非法的 provider 值 %q，允许 GITLAB/GITHUB/GITEA/GENERIC", raw)
	}
}

func normalizeGitAuthType(raw string) (string, error) {
	v := strings.ToUpper(strings.TrimSpace(raw))
	if v == "" {
		return models.GitAuthToken, nil
	}
	switch v {
	case models.GitAuthNone, models.GitAuthToken, models.GitAuthSSHKey:
		return v, nil
	default:
		return "", fmt.Errorf("非法的 auth_type 值 %q，允许 NONE/TOKEN/SSH_KEY", raw)
	}
}

func normalizeGitSyncMode(raw string) (string, error) {
	v := strings.ToUpper(strings.TrimSpace(raw))
	if v == "" {
		return models.GitSyncManual, nil
	}
	switch v {
	case models.GitSyncWebhook, models.GitSyncPoll, models.GitSyncManual:
		return v, nil
	default:
		return "", fmt.Errorf("非法的 sync_mode 值 %q，允许 WEBHOOK/POLL/MANUAL", raw)
	}
}

func normalizeBundleCategory(raw string) (string, error) {
	v := strings.ToUpper(strings.TrimSpace(raw))
	if v == "" {
		return models.ProtoCategoryBusiness, nil
	}
	switch v {
	case models.ProtoCategoryBaseLib, models.ProtoCategoryBusiness, models.ProtoCategoryGateway:
		return v, nil
	default:
		return "", fmt.Errorf("非法的 Bundle category 值 %q，允许 BASE_LIB/BUSINESS/GATEWAY", raw)
	}
}

func normalizeMRAutoPublish(raw string) (string, error) {
	v := strings.ToUpper(strings.TrimSpace(raw))
	if v == "" {
		return models.MRAutoPublishDisabled, nil
	}
	switch v {
	case models.MRAutoPublishDisabled, models.MRAutoPublishDraftOnly, models.MRAutoPublishAutoActivate:
		return v, nil
	default:
		return "", fmt.Errorf("非法的 mr_auto_publish 值 %q，允许 DISABLED/DRAFT_ONLY/AUTO_ACTIVATE", raw)
	}
}

func sanitizeRelativePathSlice(paths []string, allowGlob bool, fallback []string) (datatypes.JSON, error) {
	cleaned := make([]string, 0, len(paths))
	for _, p := range paths {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		if allowGlob {
			if strings.ContainsAny(trimmed, "\x00\\") || strings.HasPrefix(trimmed, "/") {
				return nil, fmt.Errorf("%w: 非法匹配模式 (%q)", decoder.ErrPathTraversalDenied, trimmed)
			}
			for _, seg := range strings.Split(trimmed, "/") {
				if seg == ".." {
					return nil, fmt.Errorf("%w: 匹配模式禁止 '..' (%q)", decoder.ErrPathTraversalDenied, trimmed)
				}
			}
			cleaned = append(cleaned, trimmed)
			continue
		}
		safe, err := gitclient.SanitizeSubdirPath(trimmed)
		if err != nil {
			return nil, err
		}
		cleaned = append(cleaned, safe)
	}
	if len(cleaned) == 0 && len(fallback) > 0 {
		cleaned = fallback
	}
	raw, err := json.Marshal(cleaned)
	if err != nil {
		return nil, err
	}
	return datatypes.JSON(raw), nil
}

func buildBundleModelFromPayload(repo *models.GitRepo, p bundleUpsertPayload) (*models.GitProtoBundle, error) {
	targetSchema := strings.TrimSpace(p.TargetSchemaName)
	if targetSchema == "" {
		return nil, errors.New("Bundle 必须指定 target_schema_name")
	}
	bName := strings.TrimSpace(p.BundleName)
	if bName == "" {
		bName = strings.TrimSpace(p.Name)
	}
	if bName == "" {
		bName = targetSchema
	}

	rawCat := strings.TrimSpace(p.Category)
	if rawCat == "" {
		rawCat = strings.TrimSpace(p.TargetCategory)
	}
	cat, err := normalizeBundleCategory(rawCat)
	if err != nil {
		return nil, err
	}

	rawSubdir := strings.TrimSpace(p.SubdirPath)
	if rawSubdir == "" {
		rawSubdir = strings.TrimSpace(p.ProtoRootDir)
	}
	subdir, err := gitclient.SanitizeSubdirPath(rawSubdir)
	if err != nil {
		return nil, err
	}

	rawInc := p.IncludeDirs
	if len(rawInc) == 0 {
		rawInc = p.ProtoIncludeDirs
	}
	incJSON, err := sanitizeRelativePathSlice(rawInc, false, []string{"."})
	if err != nil {
		return nil, err
	}
	entryJSON, err := sanitizeRelativePathSlice(p.EntryFiles, false, []string{})
	if err != nil {
		return nil, err
	}
	fileGlobsJSON, err := sanitizeRelativePathSlice(p.FileGlobs, true, []string{"*.proto"})
	if err != nil {
		return nil, err
	}
	excludeGlobsJSON, err := sanitizeRelativePathSlice(p.ExcludeGlobs, true, []string{})
	if err != nil {
		return nil, err
	}

	mrPub, err := normalizeMRAutoPublish(p.MRAutoPublish)
	if err != nil {
		return nil, err
	}

	autoCompile := repo.AutoCompile
	if p.AutoCompile != nil {
		autoCompile = *p.AutoCompile
	} else if p.AutoCompileAlias != nil {
		autoCompile = *p.AutoCompileAlias
	}

	autoActivate := repo.AutoActivate
	if p.AutoActivate != nil {
		autoActivate = *p.AutoActivate
	} else if p.AutoActAlias != nil {
		autoActivate = *p.AutoActAlias
	}

	lintOnSync := repo.LintOnSync
	if p.LintOnSync != nil {
		lintOnSync = *p.LintOnSync
	}

	return &models.GitProtoBundle{
		GitRepoID:        repo.ID,
		BundleName:       bName,
		TargetSchemaName: targetSchema,
		Category:         cat,
		SubdirPath:       subdir,
		IncludeDirsJSON:  incJSON,
		EntryFilesJSON:   entryJSON,
		FileGlobsJSON:    fileGlobsJSON,
		ExcludeGlobsJSON: excludeGlobsJSON,
		AutoCompile:      autoCompile,
		AutoActivate:     autoActivate,
		MRAutoPublish:    mrPub,
		LintOnSync:       lintOnSync,
		Description:      strings.TrimSpace(p.Description),
	}, nil
}

func loadRepoWithBundles(db *gorm.DB, repoID uint64) (*models.GitRepo, []models.GitProtoBundle, error) {
	var repo models.GitRepo
	if err := db.First(&repo, repoID).Error; err != nil {
		return nil, nil, err
	}
	var bundles []models.GitProtoBundle
	if err := db.Where("git_repo_id = ?", repo.ID).Order("id ASC").Find(&bundles).Error; err != nil {
		return nil, nil, err
	}
	return &repo, bundles, nil
}

// ListGitRepos GET /api/git-repos
// 标准分页查询已纳管的 Git 仓库及其 1:N Monorepo Bundles（严格脱敏）
func ListGitRepos(c *gin.Context) {
	page, pageSize := parseStandardPagination(c)
	q := models.DB.Model(&models.GitRepo{})

	if provider := strings.TrimSpace(c.Query("provider")); provider != "" {
		q = q.Where("provider = ?", strings.ToUpper(provider))
	}
	if syncStatus := strings.TrimSpace(c.Query("last_sync_status")); syncStatus != "" {
		q = q.Where("last_sync_status = ?", strings.ToUpper(syncStatus))
	}
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		like := "%" + search + "%"
		q = q.Where("name ILIKE ? OR repo_url ILIKE ? OR description ILIKE ?", like, like, like)
	}
	if subIDRaw := strings.TrimSpace(c.Query("subsystem_id")); subIDRaw != "" {
		// 预留按子系统关联或忽略非法过滤参数
		if _, err := strconv.ParseUint(subIDRaw, 10, 64); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 subsystem_id 参数"})
			return
		}
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var repos []models.GitRepo
	offset := (page - 1) * pageSize
	if err := q.Order("id DESC").Offset(offset).Limit(pageSize).Find(&repos).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	bundlesByRepo := make(map[uint64][]models.GitProtoBundle, len(repos))
	if len(repos) > 0 {
		repoIDs := make([]uint64, 0, len(repos))
		for _, r := range repos {
			repoIDs = append(repoIDs, r.ID)
		}
		var allBundles []models.GitProtoBundle
		if err := models.DB.Where("git_repo_id IN ?", repoIDs).Order("id ASC").Find(&allBundles).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, b := range allBundles {
			bundlesByRepo[b.GitRepoID] = append(bundlesByRepo[b.GitRepoID], b)
		}
	}

	items := make([]GitRepoResponseDTO, 0, len(repos))
	for _, repo := range repos {
		items = append(items, toGitRepoResponseDTO(repo, bundlesByRepo[repo.ID]))
	}

	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if totalPages == 0 {
		totalPages = 1
	}
	c.JSON(http.StatusOK, gin.H{
		"items":       items,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": totalPages,
	})
}

// CreateGitRepo POST /api/git-repos
// 创建 Git 仓库绑定，支持内联默认 Bundle 或多 Bundles（AdminOnly，AES 加密，SSRF 阻断）
func CreateGitRepo(c *gin.Context) {
	var req gitRepoCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求载荷 JSON 格式非法: " + err.Error()})
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仓库名称 name 不能为空"})
		return
	}
	provider, err := normalizeGitProvider(req.Provider)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	authType, err := normalizeGitAuthType(req.AuthType)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	syncMode, err := normalizeGitSyncMode(req.SyncMode)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	apiBase := strings.TrimSpace(req.APIBaseURL)
	if apiBase == "" {
		apiBase = strings.TrimSpace(req.WebBaseURL)
	}

	dialer, err := loadSafeDialerFromSettings(models.DB)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateGitRepoEndpointURL(c.Request.Context(), dialer, req.RepoURL, apiBase); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var repoNameCount int64
	if err := models.DB.Model(&models.GitRepo{}).Where("name = ?", name).Count(&repoNameCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if repoNameCount > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("Git 仓库名称 %q 已存在", name)})
		return
	}

	defaultBranch := strings.TrimSpace(req.DefaultBranch)
	if defaultBranch == "" {
		defaultBranch = "main"
	}
	pollSec := req.PollIntervalSec
	if pollSec <= 0 {
		pollSec = 300
	}
	autoCompile := true
	if req.AutoCompile != nil {
		autoCompile = *req.AutoCompile
	}
	autoActivate := false
	if req.AutoActivate != nil {
		autoActivate = *req.AutoActivate
	}
	lintOnSync := true
	if req.LintOnSync != nil {
		lintOnSync = *req.LintOnSync
	}

	uid, _ := currentOperatorName(c)
	repo := models.GitRepo{
		Name:            name,
		Provider:        provider,
		RepoURL:         strings.TrimSpace(req.RepoURL),
		APIBaseURL:      apiBase,
		DefaultBranch:   defaultBranch,
		AuthType:        authType,
		SyncMode:        syncMode,
		PollIntervalSec: pollSec,
		AutoCompile:     autoCompile,
		AutoActivate:    autoActivate,
		LintOnSync:      lintOnSync,
		LastSyncStatus:  models.GitSyncStatusIdle,
		Description:     strings.TrimSpace(req.Description),
		CreatedBy:       uid,
	}

	if strings.TrimSpace(req.CredentialPlain) != "" {
		if err := repo.SetCredentials(req.CredentialPlain); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "加密存储认证凭据失败: " + err.Error()})
			return
		}
	}
	if strings.TrimSpace(req.WebhookSecretPlain) != "" {
		if err := repo.SetWebhookSecret(req.WebhookSecretPlain); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "加密存储 Webhook 密钥失败: " + err.Error()})
			return
		}
	}

	bundlePayloads := make([]bundleUpsertPayload, 0, 1+len(req.Bundles))
	if req.DefaultBundle != nil {
		bundlePayloads = append(bundlePayloads, *req.DefaultBundle)
	} else if strings.TrimSpace(req.TargetSchemaName) != "" {
		bundlePayloads = append(bundlePayloads, bundleUpsertPayload{
			TargetSchemaName: req.TargetSchemaName,
			TargetCategory:   req.TargetCategory,
			SubdirPath:       req.SubdirPath,
			ProtoRootDir:     req.ProtoRootDir,
			IncludeDirs:      req.IncludeDirs,
			ProtoIncludeDirs: req.ProtoIncludeDirs,
		})
	}
	bundlePayloads = append(bundlePayloads, req.Bundles...)

	builtBundles := make([]*models.GitProtoBundle, 0, len(bundlePayloads))
	seenTargets := make(map[string]struct{}, len(bundlePayloads))
	seenNames := make(map[string]struct{}, len(bundlePayloads))
	for _, bp := range bundlePayloads {
		bModel, bErr := buildBundleModelFromPayload(&repo, bp)
		if bErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": bErr.Error()})
			return
		}
		if _, dup := seenTargets[bModel.TargetSchemaName]; dup {
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("target_schema_name %q 在请求内重复", bModel.TargetSchemaName)})
			return
		}
		seenTargets[bModel.TargetSchemaName] = struct{}{}
		if _, dup := seenNames[bModel.BundleName]; dup {
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("bundle_name %q 在同一仓库内重复", bModel.BundleName)})
			return
		}
		seenNames[bModel.BundleName] = struct{}{}

		var existTarget int64
		if err := models.DB.Model(&models.GitProtoBundle{}).Where("target_schema_name = ?", bModel.TargetSchemaName).Count(&existTarget).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if existTarget > 0 {
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("target_schema_name %q 已被其他 Bundle 占用", bModel.TargetSchemaName)})
			return
		}
		builtBundles = append(builtBundles, bModel)
	}

	if txErr := models.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&repo).Error; err != nil {
			return err
		}
		for _, bm := range builtBundles {
			bm.GitRepoID = repo.ID
			if err := tx.Create(bm).Error; err != nil {
				return err
			}
		}
		return nil
	}); txErr != nil {
		if isUniqueConstraintErr(txErr) {
			c.JSON(http.StatusConflict, gin.H{"error": "Git 仓库名或 Bundle target_schema_name 发生唯一键冲突"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": txErr.Error()})
		return
	}

	savedBundles := make([]models.GitProtoBundle, 0, len(builtBundles))
	for _, bm := range builtBundles {
		savedBundles = append(savedBundles, *bm)
	}
	dto := toGitRepoResponseDTO(repo, savedBundles)
	recordAudit(c, "traefik_git:create_repo", commonModels.AuditLevelP2,
		fmt.Sprintf("新建 Git 仓库绑定 [%s]（含 %d 个 Proto Bundle）", repo.Name, len(savedBundles)),
		"git_repo", repo.ID, repo.Name, nil, dto)

	c.JSON(http.StatusCreated, dto)
}

// UpdateGitRepo PUT /api/git-repos/:id
// 更新 Git 仓库连接信息；credential_plain / webhook_secret_plain 留空时严格保留原密文不变
func UpdateGitRepo(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的仓库 ID"})
		return
	}

	repo, bundles, err := loadRepoWithBundles(models.DB, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": gitclient.ErrGitRepoNotFound.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	beforeDTO := toGitRepoResponseDTO(*repo, bundles)

	var req gitRepoUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求载荷 JSON 格式非法: " + err.Error()})
		return
	}

	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "仓库名称 name 不能为空"})
			return
		}
		if trimmed != repo.Name {
			var cnt int64
			if err := models.DB.Model(&models.GitRepo{}).Where("name = ? AND id <> ?", trimmed, repo.ID).Count(&cnt).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if cnt > 0 {
				c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("仓库名称 %q 已存在", trimmed)})
				return
			}
			repo.Name = trimmed
		}
	}
	if req.Provider != nil {
		p, pErr := normalizeGitProvider(*req.Provider)
		if pErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": pErr.Error()})
			return
		}
		repo.Provider = p
	}
	if req.AuthType != nil {
		a, aErr := normalizeGitAuthType(*req.AuthType)
		if aErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": aErr.Error()})
			return
		}
		repo.AuthType = a
	}
	if req.SyncMode != nil {
		s, sErr := normalizeGitSyncMode(*req.SyncMode)
		if sErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": sErr.Error()})
			return
		}
		repo.SyncMode = s
	}
	if req.RepoURL != nil {
		repo.RepoURL = strings.TrimSpace(*req.RepoURL)
	}
	if req.APIBaseURL != nil {
		repo.APIBaseURL = strings.TrimSpace(*req.APIBaseURL)
	} else if req.WebBaseURL != nil {
		repo.APIBaseURL = strings.TrimSpace(*req.WebBaseURL)
	}
	if req.DefaultBranch != nil {
		b := strings.TrimSpace(*req.DefaultBranch)
		if b != "" {
			repo.DefaultBranch = b
		}
	}
	if req.PollIntervalSec != nil && *req.PollIntervalSec > 0 {
		repo.PollIntervalSec = *req.PollIntervalSec
	}
	if req.AutoCompile != nil {
		repo.AutoCompile = *req.AutoCompile
	}
	if req.AutoActivate != nil {
		repo.AutoActivate = *req.AutoActivate
	}
	if req.LintOnSync != nil {
		repo.LintOnSync = *req.LintOnSync
	}
	if req.Description != nil {
		repo.Description = strings.TrimSpace(*req.Description)
	}

	dialer, dErr := loadSafeDialerFromSettings(models.DB)
	if dErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": dErr.Error()})
		return
	}
	if err := validateGitRepoEndpointURL(c.Request.Context(), dialer, repo.RepoURL, repo.APIBaseURL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 留空保留原有 AES 密文；非空则重新 AES-256-GCM 加密覆盖
	if req.CredentialPlain != nil && strings.TrimSpace(*req.CredentialPlain) != "" {
		if err := repo.SetCredentials(*req.CredentialPlain); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "加密凭据失败: " + err.Error()})
			return
		}
	}
	if req.WebhookSecretPlain != nil && strings.TrimSpace(*req.WebhookSecretPlain) != "" {
		if err := repo.SetWebhookSecret(*req.WebhookSecretPlain); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "加密 Webhook 密钥失败: " + err.Error()})
			return
		}
	}

	if err := models.DB.Save(repo).Error; err != nil {
		if isUniqueConstraintErr(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "仓库名称冲突"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	afterDTO := toGitRepoResponseDTO(*repo, bundles)
	recordAudit(c, "traefik_git:update_repo", commonModels.AuditLevelP2,
		fmt.Sprintf("更新 Git 仓库绑定 [%s]", repo.Name),
		"git_repo", repo.ID, repo.Name, beforeDTO, afterDTO)

	c.JSON(http.StatusOK, afterDTO)
}

// DeleteGitRepo DELETE /api/git-repos/:id
// 删除 Git 仓库及其 Bundle 与只读快照，但保全历史已编译 ProtoSchema（ON DELETE SET NULL）
func DeleteGitRepo(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的仓库 ID"})
		return
	}

	repo, bundles, err := loadRepoWithBundles(models.DB, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": gitclient.ErrGitRepoNotFound.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if _, running := activeGitRepoSyncs.Load(repo.ID); running || repo.LastSyncStatus == models.GitSyncStatusRunning {
		c.JSON(http.StatusConflict, gin.H{
			"err_code": "GIT_SYNC_IN_PROGRESS",
			"error":    "当前仓库正在执行同步任务，请等待同步完成后再删除",
		})
		return
	}

	beforeDTO := toGitRepoResponseDTO(*repo, bundles)
	if txErr := models.DB.Transaction(func(tx *gorm.DB) error {
		// 显式保留所有历史 ProtoSchema 并置空来源外键，防止级联损坏历史档案
		if err := tx.Model(&models.ProtoSchema{}).
			Where("git_repo_id = ?", repo.ID).
			Updates(map[string]any{"git_repo_id": nil, "source_bundle_id": nil}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.ProtoFile{}).
			Where("git_repo_id = ?", repo.ID).
			Update("latest_version_id", nil).Error; err != nil {
			return err
		}
		if err := tx.Where("proto_file_id IN (SELECT id FROM traefik_proto_files WHERE git_repo_id = ?)", repo.ID).
			Delete(&models.ProtoFileVersion{}).Error; err != nil {
			return err
		}
		if err := tx.Where("git_repo_id = ?", repo.ID).Delete(&models.ProtoFile{}).Error; err != nil {
			return err
		}
		if err := tx.Where("git_repo_id = ?", repo.ID).Delete(&models.GitProtoBundle{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.GitRepo{}, repo.ID).Error
	}); txErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": txErr.Error()})
		return
	}

	recordAudit(c, "traefik_git:delete_repo", commonModels.AuditLevelP1,
		fmt.Sprintf("删除 Git 仓库绑定 [%s]（历史编译产物已保全归档）", repo.Name),
		"git_repo", repo.ID, repo.Name, beforeDTO, nil)

	c.JSON(http.StatusOK, gin.H{"message": "Git 仓库绑定已删除"})
}

// CreateGitRepoBundle POST /api/git-repos/:id/bundles
// 为 Monorepo 仓库新增独立的编译打包子目录单元
func CreateGitRepoBundle(c *gin.Context) {
	repoID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || repoID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的仓库 ID"})
		return
	}

	var repo models.GitRepo
	if err := models.DB.First(&repo, repoID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": gitclient.ErrGitRepoNotFound.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var req bundleUpsertPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求载荷 JSON 格式非法: " + err.Error()})
		return
	}

	bundle, bErr := buildBundleModelFromPayload(&repo, req)
	if bErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": bErr.Error()})
		return
	}

	var dupName int64
	if err := models.DB.Model(&models.GitProtoBundle{}).
		Where("git_repo_id = ? AND bundle_name = ?", repo.ID, bundle.BundleName).
		Count(&dupName).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if dupName > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("同一仓库下 bundle_name %q 已存在", bundle.BundleName)})
		return
	}

	var dupTarget int64
	if err := models.DB.Model(&models.GitProtoBundle{}).
		Where("target_schema_name = ?", bundle.TargetSchemaName).
		Count(&dupTarget).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if dupTarget > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("target_schema_name %q 已被其他 Bundle 占用", bundle.TargetSchemaName)})
		return
	}

	if err := models.DB.Create(bundle).Error; err != nil {
		if isUniqueConstraintErr(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "bundle_name 或 target_schema_name 唯一键冲突"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	dto := toGitProtoBundleDTO(*bundle)
	recordAudit(c, "traefik_git:upsert_bundle", commonModels.AuditLevelP2,
		fmt.Sprintf("新增 Monorepo Bundle [%s -> %s]", bundle.BundleName, bundle.TargetSchemaName),
		"git_proto_bundle", bundle.ID, bundle.BundleName, nil, dto)

	c.JSON(http.StatusCreated, dto)
}

// UpdateGitRepoBundle PUT /api/git-repos/:id/bundles/:bundleId
// 更新指定 Monorepo Bundle 配置
func UpdateGitRepoBundle(c *gin.Context) {
	repoID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || repoID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的仓库 ID"})
		return
	}
	bundleID, err := strconv.ParseUint(c.Param("bundleId"), 10, 64)
	if err != nil || bundleID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 Bundle ID"})
		return
	}

	var repo models.GitRepo
	if err := models.DB.First(&repo, repoID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": gitclient.ErrGitRepoNotFound.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var existing models.GitProtoBundle
	if err := models.DB.Where("id = ? AND git_repo_id = ?", bundleID, repoID).First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": gitclient.ErrGitBundleNotFound.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	beforeDTO := toGitProtoBundleDTO(existing)

	var req bundleUpsertPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求载荷 JSON 格式非法: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.TargetSchemaName) == "" {
		req.TargetSchemaName = existing.TargetSchemaName
	}
	if strings.TrimSpace(req.BundleName) == "" && strings.TrimSpace(req.Name) == "" {
		req.BundleName = existing.BundleName
	}
	if strings.TrimSpace(req.SubdirPath) == "" && strings.TrimSpace(req.ProtoRootDir) == "" {
		req.SubdirPath = existing.SubdirPath
	}

	built, bErr := buildBundleModelFromPayload(&repo, req)
	if bErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": bErr.Error()})
		return
	}

	var dupName int64
	if err := models.DB.Model(&models.GitProtoBundle{}).
		Where("git_repo_id = ? AND bundle_name = ? AND id <> ?", repo.ID, built.BundleName, existing.ID).
		Count(&dupName).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if dupName > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("同一仓库下 bundle_name %q 已存在", built.BundleName)})
		return
	}
	var dupTarget int64
	if err := models.DB.Model(&models.GitProtoBundle{}).
		Where("target_schema_name = ? AND id <> ?", built.TargetSchemaName, existing.ID).
		Count(&dupTarget).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if dupTarget > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("target_schema_name %q 已被其他 Bundle 占用", built.TargetSchemaName)})
		return
	}

	existing.BundleName = built.BundleName
	existing.TargetSchemaName = built.TargetSchemaName
	existing.Category = built.Category
	existing.SubdirPath = built.SubdirPath
	existing.IncludeDirsJSON = built.IncludeDirsJSON
	existing.EntryFilesJSON = built.EntryFilesJSON
	existing.FileGlobsJSON = built.FileGlobsJSON
	existing.ExcludeGlobsJSON = built.ExcludeGlobsJSON
	existing.AutoCompile = built.AutoCompile
	existing.AutoActivate = built.AutoActivate
	existing.MRAutoPublish = built.MRAutoPublish
	existing.LintOnSync = built.LintOnSync
	existing.Description = built.Description

	if err := models.DB.Save(&existing).Error; err != nil {
		if isUniqueConstraintErr(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "bundle_name 或 target_schema_name 唯一键冲突"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	afterDTO := toGitProtoBundleDTO(existing)
	recordAudit(c, "traefik_git:upsert_bundle", commonModels.AuditLevelP2,
		fmt.Sprintf("更新 Monorepo Bundle [%s -> %s]", existing.BundleName, existing.TargetSchemaName),
		"git_proto_bundle", existing.ID, existing.BundleName, beforeDTO, afterDTO)

	c.JSON(http.StatusOK, afterDTO)
}

// DeleteGitRepoBundle DELETE /api/git-repos/:id/bundles/:bundleId
// 删除单个 Bundle 及其旗下只读 ProtoFile（保留历史 ProtoSchema 并置空 source_bundle_id）
func DeleteGitRepoBundle(c *gin.Context) {
	repoID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || repoID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的仓库 ID"})
		return
	}
	bundleID, err := strconv.ParseUint(c.Param("bundleId"), 10, 64)
	if err != nil || bundleID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 Bundle ID"})
		return
	}

	var existing models.GitProtoBundle
	if err := models.DB.Where("id = ? AND git_repo_id = ?", bundleID, repoID).First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": gitclient.ErrGitBundleNotFound.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if _, running := activeGitRepoSyncs.Load(repoID); running {
		c.JSON(http.StatusConflict, gin.H{
			"err_code": "GIT_SYNC_IN_PROGRESS",
			"error":    "所属仓库正在同步中，请稍后再删除 Bundle",
		})
		return
	}

	beforeDTO := toGitProtoBundleDTO(existing)
	if txErr := models.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.ProtoSchema{}).
			Where("source_bundle_id = ?", existing.ID).
			Update("source_bundle_id", nil).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.ProtoFile{}).
			Where("bundle_id = ?", existing.ID).
			Update("latest_version_id", nil).Error; err != nil {
			return err
		}
		if err := tx.Where("proto_file_id IN (SELECT id FROM traefik_proto_files WHERE bundle_id = ?)", existing.ID).
			Delete(&models.ProtoFileVersion{}).Error; err != nil {
			return err
		}
		if err := tx.Where("bundle_id = ?", existing.ID).Delete(&models.ProtoFile{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.GitProtoBundle{}, existing.ID).Error
	}); txErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": txErr.Error()})
		return
	}

	recordAudit(c, "traefik_git:upsert_bundle", commonModels.AuditLevelP2,
		fmt.Sprintf("删除 Monorepo Bundle [%s -> %s]（已入池 Schema 已保留归档）", existing.BundleName, existing.TargetSchemaName),
		"git_proto_bundle", existing.ID, existing.BundleName, beforeDTO, nil)

	c.JSON(http.StatusOK, gin.H{"message": "Monorepo Proto Bundle 已删除"})
}

// TriggerGitRepoSync POST /api/git-repos/:id/sync
// 异步触发整仓或指定 Bundle 的无状态 Git 拉取、增量版本快照、编译与静态扫描，完成后推送 SSE git_sync.completed
func TriggerGitRepoSync(c *gin.Context) {
	repoID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || repoID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的仓库 ID"})
		return
	}

	var repo models.GitRepo
	if err := models.DB.First(&repo, repoID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": gitclient.ErrGitRepoNotFound.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var req triggerGitSyncRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请求载荷 JSON 格式非法: " + err.Error()})
			return
		}
	}

	if req.BundleID != nil && *req.BundleID > 0 {
		var b models.GitProtoBundle
		if err := models.DB.Where("id = ? AND git_repo_id = ?", *req.BundleID, repo.ID).First(&b).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": gitclient.ErrGitBundleNotFound.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	} else {
		var bundleCount int64
		if err := models.DB.Model(&models.GitProtoBundle{}).Where("git_repo_id = ?", repo.ID).Count(&bundleCount).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if bundleCount == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "该仓库尚未配置任何 Proto Bundle，请先添加 Bundle"})
			return
		}
	}

	branch := strings.TrimSpace(req.Branch)
	if branch == "" {
		branch = strings.TrimSpace(repo.DefaultBranch)
		if branch == "" {
			branch = "main"
		}
	}

	// SSRF 快速预检，非法 URL 不进入后台直接返回 400
	dialer, dErr := loadSafeDialerFromSettings(models.DB)
	if dErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": dErr.Error()})
		return
	}
	if err := validateGitRepoEndpointURL(c.Request.Context(), dialer, repo.RepoURL, repo.APIBaseURL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if _, loaded := activeGitRepoSyncs.LoadOrStore(repo.ID, struct{}{}); loaded {
		c.JSON(http.StatusConflict, gin.H{
			"err_code": "GIT_SYNC_IN_PROGRESS",
			"error":    "该仓库当前已有同步任务正在运行",
		})
		return
	}

	if repo.LastSyncStatus == models.GitSyncStatusRunning {
		// 若库内状态仍显示 RUNNING 且超过 15 分钟视为异常滞留允许接管；否则防重入拦截
		if time.Since(repo.UpdatedAt) < 15*time.Minute {
			activeGitRepoSyncs.Delete(repo.ID)
			c.JSON(http.StatusConflict, gin.H{
				"err_code": "GIT_SYNC_IN_PROGRESS",
				"error":    "该仓库当前处于同步执行中",
			})
			return
		}
	}

	if err := models.DB.Model(&repo).Updates(map[string]any{
		"last_sync_status": models.GitSyncStatusRunning,
		"last_sync_error":  "",
	}).Error; err != nil {
		activeGitRepoSyncs.Delete(repo.ID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	uid, _ := currentOperatorName(c)
	decReg, _ := GetDecoderRegistry()
	db := models.DB
	broker := GetGlobalSSEBroker()
	repoSnapshot := repo

	go func() {
		defer activeGitRepoSyncs.Delete(repoSnapshot.ID)
		bgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		syncSvc := gitclient.NewSyncService(db, decReg, nil)
		opts := gitclient.SyncOptions{
			Branch:        branch,
			TriggerSource: models.TriggerSourceManual,
			OperatorID:    uid,
			ForceCompile:  req.ForceRecompile,
		}

		payload := gitclient.GitSyncEventPayload{
			RepoID:   repoSnapshot.ID,
			RepoName: repoSnapshot.Name,
			BundleID: req.BundleID,
			Branch:   branch,
			Status:   models.GitSyncStatusSuccess,
		}

		var syncErr error
		if req.BundleID != nil && *req.BundleID > 0 {
			var bres *gitclient.SyncBundleResult
			bres, syncErr = syncSvc.SyncBundle(bgCtx, *req.BundleID, opts)
			if bres != nil {
				payload.BundleName = bres.BundleName
				payload.TargetSchemaName = bres.TargetSchemaName
				payload.CommitSHA = bres.CommitSHA
				payload.AddedFiles = bres.AddedFiles
				payload.ModifiedFiles = bres.ModifiedFiles
				payload.DeletedFiles = bres.DeletedFiles
				payload.CompiledSchemaID = bres.CompiledSchemaID
				payload.LintReportID = bres.LintReportID
			}
		} else {
			var res *gitclient.SyncRepoResult
			res, syncErr = syncSvc.SyncRepo(bgCtx, repoSnapshot.ID, opts)
			if res != nil {
				payload.CommitSHA = res.CommitSHA
				payload.Status = res.Status
				for _, br := range res.BundleResults {
					payload.AddedFiles += br.AddedFiles
					payload.ModifiedFiles += br.ModifiedFiles
					payload.DeletedFiles += br.DeletedFiles
					if br.CompiledSchemaID != nil && payload.CompiledSchemaID == nil {
						scID := *br.CompiledSchemaID
						payload.CompiledSchemaID = &scID
					}
				}
			}
		}
		if syncErr != nil {
			payload.Status = models.GitSyncStatusFailed
			payload.Message = syncErr.Error()
		}
		if broker != nil {
			broker.BroadcastGitSyncCompleted(payload)
		}
	}()

	recordAudit(c, "traefik_git:manual_sync", commonModels.AuditLevelP2,
		fmt.Sprintf("触发 Git 仓库 [%s] 分支 [%s] 异步契约同步", repo.Name, branch),
		"git_repo", repo.ID, repo.Name, nil, gin.H{
			"repo_id":   repo.ID,
			"bundle_id": req.BundleID,
			"branch":    branch,
		})

	resp := gin.H{
		"repo_id": repo.ID,
		"branch":  branch,
		"status":  models.GitSyncStatusRunning,
	}
	if req.BundleID != nil {
		resp["bundle_id"] = *req.BundleID
	}
	c.JSON(http.StatusAccepted, resp)
}

// GetGitRepoConsistency GET /api/git-repos/:id/consistency
// 只读对比远端最新分支源码 SHA256 与数据库 traefik_proto_files 快照差异（不写入磁盘/DB）
func GetGitRepoConsistency(c *gin.Context) {
	repoID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || repoID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的仓库 ID"})
		return
	}
	bundleID, ok := parseOptionalUint64Query(c, "bundle_id")
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 bundle_id 参数"})
		return
	}
	branch := strings.TrimSpace(c.Query("branch"))

	syncSvc := gitclient.NewSyncService(models.DB, nil, nil)
	report, err := syncSvc.ComputeConsistency(c.Request.Context(), repoID, bundleID, branch)
	if err != nil {
		switch {
		case errors.Is(err, gitclient.ErrGitRepoNotFound), errors.Is(err, gitclient.ErrGitBundleNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		case errors.Is(err, decoder.ErrPathTraversalDenied), strings.Contains(err.Error(), "SSRF"):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "远端一致性只读对账失败: " + err.Error()})
		}
		return
	}

	remoteOnlyFiles := make([]string, 0)
	dbOnlyFiles := make([]string, 0)
	modifiedFiles := make([]string, 0)
	for _, item := range report.Items {
		label := item.FilePath
		if bundleID == nil && item.BundleName != "" {
			label = item.BundleName + ":" + item.FilePath
		}
		switch item.Status {
		case gitclient.ConsistencyRemoteOnly:
			remoteOnlyFiles = append(remoteOnlyFiles, label)
		case gitclient.ConsistencyDBOnly:
			dbOnlyFiles = append(dbOnlyFiles, label)
		case gitclient.ConsistencyModified:
			modifiedFiles = append(modifiedFiles, label)
		}
	}
	inSync := len(remoteOnlyFiles) == 0 && len(dbOnlyFiles) == 0 && len(modifiedFiles) == 0

	c.JSON(http.StatusOK, gin.H{
		"repo_id":           report.RepoID,
		"repo_name":         report.RepoName,
		"bundle_id":         bundleID,
		"branch":            report.Branch,
		"commit_sha":        report.CommitSHA,
		"head_commit_sha":   report.CommitSHA,
		"in_sync":           inSync,
		"remote_only_files": remoteOnlyFiles,
		"db_only_files":     dbOnlyFiles,
		"modified_files":    modifiedFiles,
		"summary":           report.Summary,
		"items":             report.Items,
		"checked_at":        time.Now().UTC().Format(time.RFC3339),
	})
}

// ListGitRepoBranches GET /api/git-repos/:id/branches
// 经 SafeDialer 安全守卫拉取远端 Git 仓库分支列表
func ListGitRepoBranches(c *gin.Context) {
	repoID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || repoID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的仓库 ID"})
		return
	}

	var repo models.GitRepo
	if err := models.DB.First(&repo, repoID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": gitclient.ErrGitRepoNotFound.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	syncSvc := gitclient.NewSyncService(models.DB, nil, nil)
	branches, err := syncSvc.ListBranches(c.Request.Context(), repoID)
	if err != nil {
		if errors.Is(err, gitclient.ErrGitRepoNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if strings.Contains(err.Error(), "SSRF") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "拉取远端分支列表失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"default_branch": repo.DefaultBranch,
		"branches":       branches,
	})
}
