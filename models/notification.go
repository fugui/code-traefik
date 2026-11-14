package models

import (
	"time"

	"code-traefik/config"
	"gorm.io/datatypes"
)

const (
	NotifCategoryMRGateBlocked   = "MR_GATE_BLOCKED"
	NotifCategoryMRGatePassed    = "MR_GATE_PASSED"
	NotifCategoryBreakingAlert   = "BREAKING_CHANGE_ALERT"
	NotifCategoryBaseLibImpact   = "BASE_LIB_IMPACT_ALERT"
	NotifCategoryDuplicateWarn   = "DUPLICATE_STRUCT_WARN"
	NotifCategorySchemaPublished = "SCHEMA_PUBLISHED"

	NotifSeverityInfo     = "INFO"
	NotifSeverityWarning  = "WARNING"
	NotifSeverityCritical = "CRITICAL"

	NotifSourceMRCheckRun  = "MR_CHECK_RUN"
	NotifSourceProtoSchema = "PROTO_SCHEMA"
	NotifSourceCheckReport = "CHECK_REPORT"

	ExternalChannelNone         = "NONE"
	ExternalChannelCodeNotifier = "CODE_NOTIFIER"
	ExternalChannelSMTP         = "SMTP"

	EmailStatusSkipped = "SKIPPED"
	EmailStatusPending = "PENDING"
	EmailStatusSent    = "SENT"
	EmailStatusFailed  = "FAILED"
)

// 30. Notification 站内精准收件箱与跨机邮件投递状态表 (traefik_notifications)
type Notification struct {
	ID              uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	RecipientUserID uint           `gorm:"not null;index:idx_traefik_notif_user_read,priority:1" json:"recipient_user_id"`
	RecipientEmail  string         `gorm:"size:128;not null;default:''" json:"recipient_email"`
	Category        string         `gorm:"size:32;not null;index:idx_traefik_notif_category" json:"category"`
	Severity        string         `gorm:"size:16;not null;default:'INFO'" json:"severity"` // INFO | WARNING | CRITICAL
	Title           string         `gorm:"size:255;not null" json:"title"`
	BodyMarkdown    string         `gorm:"type:text;not null;default:''" json:"body_markdown"`
	LinkURL         string         `gorm:"size:512;not null;default:''" json:"link_url"`
	SourceType      string         `gorm:"size:32;not null;default:''" json:"source_type"` // MR_CHECK_RUN | PROTO_SCHEMA | CHECK_REPORT
	SourceID        uint64         `gorm:"not null;default:0" json:"source_id"`
	MetadataJSON    datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"metadata_json"`
	IsRead          bool           `gorm:"not null;default:false;index:idx_traefik_notif_user_read,priority:2" json:"is_read"`
	ReadAt          *time.Time     `gorm:"default:null" json:"read_at,omitempty"`
	ExternalChannel string         `gorm:"size:32;not null;default:'NONE'" json:"external_channel"` // NONE | CODE_NOTIFIER | SMTP
	EmailStatus     string         `gorm:"size:24;not null;default:'SKIPPED'" json:"email_status"`  // SKIPPED | PENDING | SENT | FAILED
	EmailError      string         `gorm:"type:text;not null;default:''" json:"email_error"`
	EmailSentAt     *time.Time     `gorm:"default:null" json:"email_sent_at,omitempty"`
	CreatedAt       time.Time      `gorm:"index:idx_traefik_notif_user_read,priority:3,sort:desc" json:"created_at"`
}

func (Notification) TableName() string { return config.TablePrefix + "notifications" }
