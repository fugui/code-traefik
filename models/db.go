package models

import (
	"fmt"
	"log"

	commonAuth "code-common/backend/auth"
	"code-common/backend/gormdb"
	"code-traefik/config"

	"gorm.io/gorm"
)

// DB 模块全局 GORM 数据库连接句柄
var DB *gorm.DB

// InitDB 连接 PostgreSQL 数据库并执行表结构迁移与种子数据初始化
// TODO(security): 生产环境请为本服务分配遵循最小权限原则的独立数据库账号，并开启 SSL/mTLS 传输加密
func InitDB() error {
	db, err := gormdb.Connect(config.AppConfig.Database, gormdb.Options{
		ServiceName: config.ServiceName,
	})
	if err != nil {
		return fmt.Errorf("连接数据库失败: %w", err)
	}
	return SetupWithDB(db)
}

// SetupWithDB 使用外部注入的 *gorm.DB 完成迁移与默认种子初始化（便于单元测试复用）
func SetupWithDB(db *gorm.DB) error {
	DB = db

	if err := DB.AutoMigrate(
		&Department{},
		&User{},
		&SysAuditLog{},
		&TemplateItem{},
		&TemplateSetting{},
	); err != nil {
		return fmt.Errorf("迁移数据表结构失败: %w", err)
	}

	// 确保独立运行模式下至少存在一个默认管理员账号
	if err := commonAuth.EnsureSeedAdmin(DB, config.AdminRole); err != nil {
		log.Printf("[%s] 提示: 初始化默认管理员账号未完成: %v", config.ServiceName, err)
	}

	if err := seedDefaultData(DB); err != nil {
		log.Printf("[%s] 提示: 初始化示例种子数据未完成: %v", config.ServiceName, err)
	}

	return nil
}

func seedDefaultData(db *gorm.DB) error {
	var itemCount int64
	if err := db.Model(&TemplateItem{}).Count(&itemCount).Error; err != nil {
		return err
	}
	if itemCount == 0 {
		samples := []TemplateItem{
			{
				Code:        "TPL-001",
				Name:        "接入统一 JWT 鉴权与 RBAC 角色守卫",
				Category:    "security",
				Status:      ItemStatusActive,
				Priority:    ItemPriorityHigh,
				Description: "复用 code-common/backend/auth 中间件，实现与 CodeBench 宿主的无感单点认证及模块管理员权限控制。",
			},
			{
				Code:        "TPL-002",
				Name:        "配置 Vite Module Federation 微前端暴露契约",
				Category:    "frontend",
				Status:      ItemStatusActive,
				Priority:    ItemPriorityHigh,
				Description: "通过 remoteEntry.js 动态导出 ./App 与 ./menu，无需修改宿主源码即可挂载二级侧边栏与业务路由。",
			},
			{
				Code:        "TPL-003",
				Name:        "适配亮色与暗黑双模 Design Tokens 主题",
				Category:    "ui",
				Status:      ItemStatusDraft,
				Priority:    ItemPriorityMedium,
				Description: "全面采用 theme.css 中的 --color-* 语义变量与 Flat BEM 类名，杜绝硬编码颜色导致的亮色黑块问题。",
			},
		}
		if err := db.Create(&samples).Error; err != nil {
			return err
		}
	}

	var settingCount int64
	if err := db.Model(&TemplateSetting{}).Count(&settingCount).Error; err != nil {
		return err
	}
	if settingCount == 0 {
		defaultSettings := []TemplateSetting{
			{
				SettingKey:  "enable_notification",
				SettingVal:  "true",
				Description: "是否在关键业务条目变更时触发平台通知",
				UpdatedBy:   "system",
			},
			{
				SettingKey:  "max_items_per_user",
				SettingVal:  "200",
				Description: "单个用户可创建的活跃业务条目配额上限",
				UpdatedBy:   "system",
			},
			{
				SettingKey:  "default_category",
				SettingVal:  "general",
				Description: "新建业务条目的默认分类标识",
				UpdatedBy:   "system",
			},
		}
		if err := db.Create(&defaultSettings).Error; err != nil {
			return err
		}
	}

	return nil
}
