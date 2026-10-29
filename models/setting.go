package models

import (
	"time"

	"code-traefik/config"
)

// TraefikSetting 模块运行与治理参数配置模型 (traefik_settings)
type TraefikSetting struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	SettingKey  string    `gorm:"size:64;uniqueIndex:uk_traefik_settings_key;not null" json:"setting_key"`
	SettingVal  string    `gorm:"type:text;not null" json:"setting_val"`
	Description string    `gorm:"size:255" json:"description"`
	UpdatedBy   string    `gorm:"size:128" json:"updated_by"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TableName 显式添加模块专属表名前缀
func (TraefikSetting) TableName() string {
	return config.TablePrefix + "settings"
}

// TemplateSetting 向后兼容别名
type TemplateSetting = TraefikSetting
