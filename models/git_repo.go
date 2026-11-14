package models

import (
	"encoding/json"
	"strings"
	"time"

	"code-traefik/internal/server/security"

	"gorm.io/datatypes"
)

const (
	GitProviderGitLab  = "GITLAB"
	GitProviderGitHub  = "GITHUB"
	GitProviderGitea   = "GITEA"
	GitProviderGeneric = "GENERIC"

	GitAuthNone   = "NONE"
	GitAuthToken  = "TOKEN"
	GitAuthSSHKey = "SSH_KEY"

	GitSyncWebhook = "WEBHOOK"
	GitSyncPoll    = "POLL"
	GitSyncManual  = "MANUAL"

	GitSyncStatusIdle    = "IDLE"
	GitSyncStatusRunning = "RUNNING"
	GitSyncStatusSuccess = "SUCCESS"
	GitSyncStatusFailed  = "FAILED"

	MRAutoPublishDisabled     = "DISABLED"
	MRAutoPublishDraftOnly    = "DRAFT_ONLY"
	MRAutoPublishAutoActivate = "AUTO_ACTIVATE"
)

// GitRepo 实体 #20：Git 契约仓库连接与鉴权配置表（SSOT 控制面元数据）
type GitRepo struct {
	ID               uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Name             string     `gorm:"size:128;not null;uniqueIndex:uk_traefik_git_repo_name" json:"name"`
	Provider         string     `gorm:"size:32;not null;default:'GITLAB'" json:"provider"` // GITLAB | GITHUB | GITEA | GENERIC
	RepoURL          string     `gorm:"size:512;not null" json:"repo_url"`
	APIBaseURL       string     `gorm:"size:512;not null;default:''" json:"api_base_url"`
	DefaultBranch    string     `gorm:"size:128;not null;default:'main'" json:"default_branch"`
	AuthType         string     `gorm:"size:32;not null;default:'TOKEN'" json:"auth_type"` // NONE | TOKEN | SSH_KEY
	CredentialsEnc   string     `gorm:"type:text;not null;default:''" json:"-"`            // AES-256-GCM 密文，严禁序列化泄漏
	CredentialHint   string     `gorm:"size:64;not null;default:''" json:"credential_hint"`
	WebhookSecretEnc string     `gorm:"type:text;not null;default:''" json:"-"`             // AES-256-GCM 密文，严禁序列化泄漏
	SyncMode         string     `gorm:"size:32;not null;default:'MANUAL'" json:"sync_mode"` // WEBHOOK | POLL | MANUAL
	PollIntervalSec  int        `gorm:"not null;default:300" json:"poll_interval_sec"`
	AutoCompile      bool       `gorm:"not null;default:true" json:"auto_compile"`
	AutoActivate     bool       `gorm:"not null;default:false" json:"auto_activate"`
	LintOnSync       bool       `gorm:"not null;default:true" json:"lint_on_sync"`
	LastSyncAt       *time.Time `json:"last_sync_at,omitempty"`
	LastSyncCommit   string     `gorm:"size:64;not null;default:''" json:"last_sync_commit"`
	LastSyncStatus   string     `gorm:"size:32;not null;default:'IDLE'" json:"last_sync_status"` // IDLE | RUNNING | SUCCESS | FAILED
	LastSyncError    string     `gorm:"type:text;not null;default:''" json:"last_sync_error"`
	Description      string     `gorm:"type:text;not null;default:''" json:"description"`
	CreatedBy        uint       `gorm:"not null;default:0" json:"created_by"`
	CreatedAt        time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (GitRepo) TableName() string {
	return "traefik_git_repos"
}

// GitRepoRedactedDTO 脱敏后的 GitRepo 接口输出模型（绝不返回 CredentialsEnc / WebhookSecretEnc / 明文）
type GitRepoRedactedDTO struct {
	ID               uint64           `json:"id"`
	Name             string           `json:"name"`
	Provider         string           `json:"provider"`
	RepoURL          string           `json:"repo_url"`
	APIBaseURL       string           `json:"api_base_url"`
	DefaultBranch    string           `json:"default_branch"`
	AuthType         string           `json:"auth_type"`
	HasCredentials   bool             `json:"has_credentials"`
	CredentialHint   string           `json:"credential_hint"`
	HasWebhookSecret bool             `json:"has_webhook_secret"`
	SyncMode         string           `json:"sync_mode"`
	PollIntervalSec  int              `json:"poll_interval_sec"`
	AutoCompile      bool             `json:"auto_compile"`
	AutoActivate     bool             `json:"auto_activate"`
	LintOnSync       bool             `json:"lint_on_sync"`
	LastSyncAt       *time.Time       `json:"last_sync_at,omitempty"`
	LastSyncCommit   string           `json:"last_sync_commit"`
	LastSyncStatus   string           `json:"last_sync_status"`
	LastSyncError    string           `json:"last_sync_error"`
	Description      string           `json:"description"`
	CreatedBy        uint             `json:"created_by"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
	Bundles          []GitProtoBundle `json:"bundles,omitempty"`
}

// MaskSecretHint 生成不可逆的脱敏凭证尾部摘要提示（永不暴露核心秘密片段）
func MaskSecretHint(plain string) string {
	trimmed := strings.TrimSpace(plain)
	if trimmed == "" {
		return ""
	}
	// SSH 私钥统一固定格式摘要，避免暴露关键头尾内容
	if strings.Contains(trimmed, "PRIVATE KEY") {
		last := trimmed
		if len(last) > 4 {
			last = last[len(last)-4:]
		}
		return "ssh-key****" + last
	}
	if len(trimmed) < 10 {
		return "****"
	}
	if len(trimmed) >= 12 {
		return trimmed[:4] + "****" + trimmed[len(trimmed)-4:]
	}
	return "****" + trimmed[len(trimmed)-4:]
}

// SetCredentials 使用 TRAEFIK_CRYPTO_KEY 通过 AES-256-GCM 加密认证凭证并生成脱敏提示
func (g *GitRepo) SetCredentials(plain string) error {
	trimmed := strings.TrimSpace(plain)
	if trimmed == "" {
		g.CredentialsEnc = ""
		g.CredentialHint = ""
		return nil
	}
	enc, err := security.EncryptSecret(trimmed)
	if err != nil {
		return err
	}
	g.CredentialsEnc = enc
	g.CredentialHint = MaskSecretHint(trimmed)
	return nil
}

// DecryptCredentials 使用 TRAEFIK_CRYPTO_KEY 在内存中按需解密认证凭证
func (g *GitRepo) DecryptCredentials() (string, error) {
	if strings.TrimSpace(g.CredentialsEnc) == "" {
		return "", nil
	}
	return security.DecryptSecret(g.CredentialsEnc)
}

// SetWebhookSecret 使用 TRAEFIK_CRYPTO_KEY 通过 AES-256-GCM 加密 Webhook Secret
func (g *GitRepo) SetWebhookSecret(plain string) error {
	trimmed := strings.TrimSpace(plain)
	if trimmed == "" {
		g.WebhookSecretEnc = ""
		return nil
	}
	enc, err := security.EncryptSecret(trimmed)
	if err != nil {
		return err
	}
	g.WebhookSecretEnc = enc
	return nil
}

// DecryptWebhookSecret 使用 TRAEFIK_CRYPTO_KEY 在内存中按需解密 Webhook Secret
func (g *GitRepo) DecryptWebhookSecret() (string, error) {
	if strings.TrimSpace(g.WebhookSecretEnc) == "" {
		return "", nil
	}
	return security.DecryptSecret(g.WebhookSecretEnc)
}

// ToRedactedDTO 转换为严格脱敏 API 视图对象
func (g *GitRepo) ToRedactedDTO(bundles ...GitProtoBundle) GitRepoRedactedDTO {
	hasCreds := strings.TrimSpace(g.CredentialsEnc) != ""
	hint := g.CredentialHint
	if hasCreds && hint == "" {
		if dec, err := g.DecryptCredentials(); err == nil && dec != "" {
			hint = MaskSecretHint(dec)
		} else {
			hint = "****"
		}
	}
	if !hasCreds {
		hint = ""
	}
	return GitRepoRedactedDTO{
		ID:               g.ID,
		Name:             g.Name,
		Provider:         g.Provider,
		RepoURL:          g.RepoURL,
		APIBaseURL:       g.APIBaseURL,
		DefaultBranch:    g.DefaultBranch,
		AuthType:         g.AuthType,
		HasCredentials:   hasCreds,
		CredentialHint:   hint,
		HasWebhookSecret: strings.TrimSpace(g.WebhookSecretEnc) != "",
		SyncMode:         g.SyncMode,
		PollIntervalSec:  g.PollIntervalSec,
		AutoCompile:      g.AutoCompile,
		AutoActivate:     g.AutoActivate,
		LintOnSync:       g.LintOnSync,
		LastSyncAt:       g.LastSyncAt,
		LastSyncCommit:   g.LastSyncCommit,
		LastSyncStatus:   g.LastSyncStatus,
		LastSyncError:    g.LastSyncError,
		Description:      g.Description,
		CreatedBy:        g.CreatedBy,
		CreatedAt:        g.CreatedAt,
		UpdatedAt:        g.UpdatedAt,
		Bundles:          bundles,
	}
}

// GitProtoBundle 实体 #21：Monorepo 多独立 Proto 子目录 / 编译打包单元 (1:N 归属 GitRepo)
type GitProtoBundle struct {
	ID               uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	GitRepoID        uint64         `gorm:"not null;uniqueIndex:uk_traefik_git_bundle_repo_name;index:idx_traefik_git_bundle_repo" json:"git_repo_id"`
	BundleName       string         `gorm:"size:128;not null;uniqueIndex:uk_traefik_git_bundle_repo_name" json:"bundle_name"`
	TargetSchemaName string         `gorm:"size:128;not null;uniqueIndex:uk_traefik_git_bundle_target" json:"target_schema_name"`
	Category         string         `gorm:"size:32;not null;default:'BUSINESS'" json:"category"` // BASE_LIB | BUSINESS | GATEWAY
	SubdirPath       string         `gorm:"size:512;not null;default:'.'" json:"subdir_path"`
	IncludeDirsJSON  datatypes.JSON `gorm:"column:include_dirs_json;type:jsonb;not null;default:'[\".\"]'" json:"include_dirs_json"`
	EntryFilesJSON   datatypes.JSON `gorm:"column:entry_files_json;type:jsonb;not null;default:'[]'" json:"entry_files_json"`
	FileGlobsJSON    datatypes.JSON `gorm:"column:file_globs_json;type:jsonb;not null;default:'[\"*.proto\"]'" json:"file_globs_json"`
	ExcludeGlobsJSON datatypes.JSON `gorm:"column:exclude_globs_json;type:jsonb;not null;default:'[]'" json:"exclude_globs_json"`
	AutoCompile      bool           `gorm:"not null;default:true" json:"auto_compile"`
	AutoActivate     bool           `gorm:"not null;default:false" json:"auto_activate"`
	MRAutoPublish    string         `gorm:"column:mr_auto_publish;size:32;not null;default:'DISABLED'" json:"mr_auto_publish"` // DISABLED | DRAFT_ONLY | AUTO_ACTIVATE
	LintOnSync       bool           `gorm:"not null;default:true" json:"lint_on_sync"`
	LastSyncCommit   string         `gorm:"size:64;not null;default:''" json:"last_sync_commit"`
	LastSyncAt       *time.Time     `json:"last_sync_at,omitempty"`
	LastSchemaID     *uint64        `json:"last_schema_id,omitempty"`
	Description      string         `gorm:"type:text;not null;default:''" json:"description"`
	CreatedAt        time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
}

func (GitProtoBundle) TableName() string {
	return "traefik_git_proto_bundles"
}

func decodeStringSliceJSON(raw datatypes.JSON, fallback []string) []string {
	if len(raw) == 0 {
		return fallback
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil || len(out) == 0 {
		return fallback
	}
	return out
}

// IncludeDirs 返回 Bundle 配置的编译 include 搜索目录（POSIX 相对路径）
func (b GitProtoBundle) IncludeDirs() []string {
	return decodeStringSliceJSON(b.IncludeDirsJSON, []string{"."})
}

// EntryFiles 返回显式指定的入口 .proto 文件列表
func (b GitProtoBundle) EntryFiles() []string {
	return decodeStringSliceJSON(b.EntryFilesJSON, []string{})
}

// FileGlobs 返回包含匹配 glob 模式
func (b GitProtoBundle) FileGlobs() []string {
	return decodeStringSliceJSON(b.FileGlobsJSON, []string{"*.proto"})
}

// ExcludeGlobs 返回排除匹配 glob 模式
func (b GitProtoBundle) ExcludeGlobs() []string {
	return decodeStringSliceJSON(b.ExcludeGlobsJSON, []string{})
}

// ProtoFile 实体 #22：Git 同步后的只读 Proto 源文件实体（严禁任何悲观写锁或 Web 编辑字段）
type ProtoFile struct {
	ID               uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	GitRepoID        uint64    `gorm:"not null;index:idx_traefik_pfile_repo" json:"git_repo_id"`
	BundleID         uint64    `gorm:"not null;index:idx_traefik_pfile_bundle" json:"bundle_id"`
	TargetSchemaName string    `gorm:"size:128;not null;index:idx_traefik_pfile_target_schema" json:"target_schema_name"`
	Category         string    `gorm:"size:32;not null;default:'BUSINESS'" json:"category"` // BASE_LIB | BUSINESS | GATEWAY
	FilePath         string    `gorm:"size:512;not null" json:"file_path"`                  // 相对于 Bundle SubdirPath 的 POSIX 路径
	RepoFilePath     string    `gorm:"size:768;not null;default:''" json:"repo_file_path"`  // 相对于 Git 仓库根目录的 POSIX 路径
	PackageName      string    `gorm:"size:255;not null;default:''" json:"package_name"`
	ContentSHA256    string    `gorm:"column:content_sha256;size:64;not null;default:''" json:"content_sha256"`
	LineCount        int       `gorm:"not null;default:0" json:"line_count"`
	LatestVersionID  *uint64   `gorm:"index:idx_traefik_pfile_latest_ver" json:"latest_version_id,omitempty"`
	LatestVersionNum int       `gorm:"not null;default:0" json:"latest_version"`
	LatestCommitSHA  string    `gorm:"size:64;not null;default:''" json:"latest_commit_sha"`
	IsDeleted        bool      `gorm:"not null;default:false;index:idx_traefik_pfile_deleted" json:"is_deleted"`
	CreatedAt        time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (ProtoFile) TableName() string {
	return "traefik_proto_files"
}

// ProtoFileVersion 实体 #23：只读 Proto 源文件的 Commit 不可变历史快照表
type ProtoFileVersion struct {
	ID            uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	ProtoFileID   uint64    `gorm:"not null;uniqueIndex:uk_traefik_pfile_ver_num;index:idx_traefik_pfv_file" json:"proto_file_id"`
	Version       int       `gorm:"not null;uniqueIndex:uk_traefik_pfile_ver_num" json:"version"`
	VersionTag    string    `gorm:"size:128;not null;default:''" json:"version_tag"`
	SourceType    string    `gorm:"size:32;not null;default:'GIT_SYNC'" json:"source_type"` // GIT_SYNC | GIT_MR_AUTO
	CommitSHA     string    `gorm:"size:64;not null;default:'';index:idx_traefik_pfv_commit" json:"commit_sha"`
	CommitMessage string    `gorm:"type:text;not null;default:''" json:"commit_message"`
	AuthorName    string    `gorm:"size:128;not null;default:''" json:"author_name"`
	AuthorEmail   string    `gorm:"size:128;not null;default:''" json:"author_email"`
	ContentText   string    `gorm:"type:text;not null" json:"-"` // 只读源码全文（通过 /content 与 /diff 按需返回）
	ContentSHA256 string    `gorm:"column:content_sha256;size:64;not null" json:"content_sha256"`
	LineCount     int       `gorm:"not null;default:0" json:"line_count"`
	SchemaID      *uint64   `gorm:"index:idx_traefik_pfv_schema" json:"schema_id,omitempty"`
	CommittedAt   time.Time `gorm:"not null" json:"committed_at"`
	CreatedAt     time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (ProtoFileVersion) TableName() string {
	return "traefik_proto_file_versions"
}
