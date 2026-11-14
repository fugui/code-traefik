package models

import (
	"time"

	"code-traefik/config"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	MREventPhaseGate      = "MR_GATE"
	MREventPhaseMainMerge = "MAIN_MERGE"

	MRCheckStatusPending    = "PENDING"
	MRCheckStatusRunning    = "RUNNING"
	MRCheckStatusPassed     = "PASSED"
	MRCheckStatusWarn       = "WARN"
	MRCheckStatusFailed     = "FAILED"
	MRCheckStatusOverridden = "OVERRIDDEN"
)

// 29. MRCheckRun Git Merge Request / Pull Request 质量门禁检查流水表 (traefik_mr_check_runs)
type MRCheckRun struct {
	ID                    uint64          `gorm:"primaryKey;autoIncrement" json:"id"`
	GitRepoID             uint64          `gorm:"not null;index:idx_traefik_mr_repo_iid,priority:1" json:"git_repo_id"`
	GitRepo               *GitRepo        `gorm:"foreignKey:GitRepoID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"git_repo,omitempty"`
	BundleID              *uint64         `gorm:"index:idx_traefik_mr_bundle" json:"bundle_id,omitempty"`
	Bundle                *GitProtoBundle `gorm:"foreignKey:BundleID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL" json:"bundle,omitempty"`
	MRIID                 int             `gorm:"column:mr_iid;not null;index:idx_traefik_mr_repo_iid,priority:2" json:"mr_iid"`
	EventPhase            string          `gorm:"size:16;not null;default:'MR_GATE'" json:"event_phase"` // MR_GATE | MAIN_MERGE
	MRTitle               string          `gorm:"column:mr_title;size:255;not null;default:''" json:"mr_title"`
	MRURL                 string          `gorm:"column:mr_url;size:512;not null;default:''" json:"mr_url"`
	SourceBranch          string          `gorm:"size:128;not null" json:"source_branch"`
	TargetBranch          string          `gorm:"size:128;not null" json:"target_branch"`
	HeadCommitSHA         string          `gorm:"column:head_commit_sha;size:64;not null;index:idx_traefik_mr_head_sha" json:"head_commit_sha"`
	TargetHeadSHA         string          `gorm:"column:target_head_sha;size:64;not null;default:''" json:"target_head_sha"`
	AuthorUsername        string          `gorm:"size:64;not null;default:''" json:"author_username"`
	AuthorEmail           string          `gorm:"size:128;not null;default:''" json:"author_email"`
	Status                string          `gorm:"size:24;not null;default:'PENDING';index:idx_traefik_mr_status" json:"status"` // PENDING | RUNNING | PASSED | WARN | FAILED | OVERRIDDEN
	CompilePassed         bool            `gorm:"not null;default:false" json:"compile_passed"`
	LintErrorCount        int             `gorm:"not null;default:0" json:"lint_error_count"`
	LintWarningCount      int             `gorm:"not null;default:0" json:"lint_warning_count"`
	DuplicateGroupCount   int             `gorm:"not null;default:0" json:"duplicate_group_count"`
	BreakingChangeCount   int             `gorm:"not null;default:0" json:"breaking_change_count"`
	AffectedModulesCount  int             `gorm:"not null;default:0" json:"affected_modules_count"`
	ChangedProtoFilesJSON datatypes.JSON  `gorm:"column:changed_proto_files;type:jsonb;not null;default:'[]'" json:"changed_proto_files"`
	CompileErrorText      string          `gorm:"type:text;not null;default:''" json:"compile_error_text"`
	BreakingDetailsJSON   datatypes.JSON  `gorm:"column:breaking_details;type:jsonb;not null;default:'[]'" json:"breaking_details"`
	AffectedModulesJSON   datatypes.JSON  `gorm:"column:affected_modules;type:jsonb;not null;default:'[]'" json:"affected_modules"`
	CheckReportID         *uint64         `gorm:"index:idx_traefik_mr_report" json:"check_report_id,omitempty"`
	CheckReport           *CheckReport    `gorm:"foreignKey:CheckReportID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL" json:"check_report,omitempty"`
	RemoteCommentID       string          `gorm:"size:64;not null;default:''" json:"remote_comment_id"`
	AttemptCount          int             `gorm:"not null;default:1" json:"attempt_count"`
	OverriddenBy          *uint           `gorm:"default:null" json:"overridden_by,omitempty"`
	OverrideReason        string          `gorm:"type:text;not null;default:''" json:"override_reason"`
	OverriddenAt          *time.Time      `gorm:"default:null" json:"overridden_at,omitempty"`
	DurationMs            int             `gorm:"not null;default:0" json:"duration_ms"`
	StartedAt             *time.Time      `gorm:"default:null" json:"started_at,omitempty"`
	CompletedAt           *time.Time      `gorm:"default:null" json:"completed_at,omitempty"`
	CreatedAt             time.Time       `json:"created_at"`
	UpdatedAt             time.Time       `json:"updated_at"`
}

func (MRCheckRun) TableName() string { return config.TablePrefix + "mr_check_runs" }

// ensureM26MRGateAndNotificationConstraints 幂等创建 M26 表达式唯一索引、CHECK 约束及外键
func ensureM26MRGateAndNotificationConstraints(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	stmts := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_traefik_mr_dedup
			ON traefik_mr_check_runs (git_repo_id, COALESCE(bundle_id, 0), event_phase, head_commit_sha, target_head_sha)`,
		`CREATE INDEX IF NOT EXISTS idx_traefik_mr_repo_iid
			ON traefik_mr_check_runs (git_repo_id, mr_iid)`,
		`CREATE INDEX IF NOT EXISTS idx_traefik_mr_status
			ON traefik_mr_check_runs (status, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_traefik_notif_user_read
			ON traefik_notifications (recipient_user_id, is_read, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_traefik_notif_category
			ON traefik_notifications (category, created_at DESC)`,
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_mr_phase') THEN
				ALTER TABLE traefik_mr_check_runs ADD CONSTRAINT chk_traefik_mr_phase
					CHECK (event_phase IN ('MR_GATE', 'MAIN_MERGE'));
			END IF;
		END $$;`,
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_mr_status') THEN
				ALTER TABLE traefik_mr_check_runs ADD CONSTRAINT chk_traefik_mr_status
					CHECK (status IN ('PENDING', 'RUNNING', 'PASSED', 'WARN', 'FAILED', 'OVERRIDDEN'));
			END IF;
		END $$;`,
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_check_reports_mr_run') THEN
				ALTER TABLE traefik_check_reports ADD CONSTRAINT fk_traefik_check_reports_mr_run
					FOREIGN KEY (mr_check_run_id) REFERENCES traefik_mr_check_runs(id) ON DELETE SET NULL;
			END IF;
		END $$;`,
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_notif_sev') THEN
				ALTER TABLE traefik_notifications ADD CONSTRAINT chk_traefik_notif_sev
					CHECK (severity IN ('INFO', 'WARNING', 'CRITICAL'));
			END IF;
		END $$;`,
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_notif_email_status') THEN
				ALTER TABLE traefik_notifications ADD CONSTRAINT chk_traefik_notif_email_status
					CHECK (email_status IN ('SKIPPED', 'PENDING', 'SENT', 'FAILED'));
			END IF;
		END $$;`,
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_notif_ext_chan') THEN
				ALTER TABLE traefik_notifications ADD CONSTRAINT chk_traefik_notif_ext_chan
					CHECK (external_channel IN ('NONE', 'CODE_NOTIFIER', 'SMTP'));
			END IF;
		END $$;`,
	}
	for _, stmt := range stmts {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}
