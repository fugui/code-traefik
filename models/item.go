package models

import (
	"time"

	commonModels "code-common/backend/models"
	"code-traefik/config"
)

// 复用 code-common 公共基础模型（单一事实源，严禁在子系统中重复声明雷同结构）
type User = commonModels.User
type Department = commonModels.Department
type SysAuditLog = commonModels.SysAuditLog

// 支持的业务条目状态与优先级白名单常量
const (
	ItemStatusActive   = "active"
	ItemStatusDraft    = "draft"
	ItemStatusArchived = "archived"

	ItemPriorityHigh   = "high"
	ItemPriorityMedium = "medium"
	ItemPriorityLow    = "low"
)

// TemplateItem 示例业务实体模型（新业务开发时可替换或扩展为你自己的领域实体）
type TemplateItem struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Code        string    `gorm:"size:64;uniqueIndex;not null" json:"code"`
	Name        string    `gorm:"size:128;not null;index" json:"name"`
	Category    string    `gorm:"size:64;not null;default:'general';index" json:"category"`
	Status      string    `gorm:"size:32;not null;default:'active';index" json:"status"`
	Priority    string    `gorm:"size:32;not null;default:'medium'" json:"priority"`
	Description string    `gorm:"type:text" json:"description"`
	OwnerID     *uint     `gorm:"index" json:"owner_id"`
	Owner       *User     `gorm:"foreignKey:OwnerID" json:"owner,omitempty"`
	CreatedBy   uint      `gorm:"index" json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TableName 显式添加模块专属表名前缀，防止多模块共享 PostgreSQL 数据库时发生表名冲突
func (TemplateItem) TableName() string {
	return config.TablePrefix + "items"
}

// TemplateSetting 示例模块级配置项模型（由模块管理员维护）
type TemplateSetting struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	SettingKey  string    `gorm:"size:64;uniqueIndex;not null" json:"setting_key"`
	SettingVal  string    `gorm:"type:text;not null" json:"setting_val"`
	Description string    `gorm:"size:255" json:"description"`
	UpdatedBy   string    `gorm:"size:128" json:"updated_by"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TableName 显式添加模块专属表名前缀
func (TemplateSetting) TableName() string {
	return config.TablePrefix + "settings"
}

// IsValidItemStatus 校验条目状态是否属于允许列表
func IsValidItemStatus(status string) bool {
	switch status {
	case ItemStatusActive, ItemStatusDraft, ItemStatusArchived:
		return true
	default:
		return false
	}
}

// IsValidItemPriority 校验条目优先级是否属于允许列表
func IsValidItemPriority(priority string) bool {
	switch priority {
	case ItemPriorityHigh, ItemPriorityMedium, ItemPriorityLow:
		return true
	default:
		return false
	}
}
