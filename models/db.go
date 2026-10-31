package models

import (
	"fmt"
	"log"
	"time"

	commonAuth "code-common/backend/auth"
	"code-common/backend/gormdb"
	"code-traefik/config"

	"gorm.io/gorm"
)

// DB 模块全局 GORM 数据库连接句柄
var DB *gorm.DB

// InitDB 连接 PostgreSQL 数据库并执行表结构迁移与种子数据初始化
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

	// 1. 自动迁移标准实体表
	if err := DB.AutoMigrate(
		&Department{},
		&User{},
		&SysAuditLog{},
		&TraefikSetting{},
		&Subsystem{},
		&FunctionalGroup{},
		&Module{},
		&ModuleService{},
		&ModuleDependency{},
		&Agent{},
		&Process{},
		&ProcessModule{},
		&ProtoSchema{},
		&ProtoOperation{},
		&RecordingSession{},
		&ReplayTask{},
		&ReplayItem{},
	); err != nil {
		return fmt.Errorf("迁移数据表结构失败: %w", err)
	}

	// 2. 初始化 traefik_traffic_records 物理分区母表与近期按日分区
	pm := &PartitionManager{}
	if err := pm.EnsureParentTable(DB); err != nil {
		return fmt.Errorf("初始化流量分区母表失败: %w", err)
	}
	now := time.Now()
	if _, err := pm.EnsurePartitionsRange(DB, now.AddDate(0, 0, -1), now.AddDate(0, 0, 7)); err != nil {
		return fmt.Errorf("预建日常流量分区子表失败: %w", err)
	}

	// 3. 显式创建关键业务部分唯一索引与端口检查约束
	// 3.1 兼容纯客户端进程 (listen_port = 0 无监听端口)：升级检查约束与创建部分唯一索引 (WHERE listen_port > 0)
	procConstraintSQL := `
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_proc_port') THEN
        ALTER TABLE traefik_processes DROP CONSTRAINT chk_traefik_proc_port;
    END IF;
    ALTER TABLE traefik_processes ADD CONSTRAINT chk_traefik_proc_port CHECK (listen_port >= 0 AND listen_port <= 65535);

    -- 移除旧的全量唯一索引（若存在且未带 WHERE listen_port > 0）
    DROP INDEX IF EXISTS uk_traefik_proc_agent_port;
END $$;`
	if err := DB.Exec(procConstraintSQL).Error; err != nil {
		return fmt.Errorf("更新进程端口检查约束失败: %w", err)
	}

	partialIndexes := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_traefik_proto_active_name ON traefik_proto_schemas (name) WHERE is_active = true;`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_traefik_active_proto_op ON traefik_proto_operations (service_name, operation) WHERE is_active = true;`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_traefik_proc_agent_port ON traefik_processes (agent_id, listen_port) WHERE listen_port > 0;`,
	}
	for _, idxSQL := range partialIndexes {
		if err := DB.Exec(idxSQL).Error; err != nil {
			return fmt.Errorf("创建部分唯一索引失败: %w", err)
		}
	}

	// 4. 显式创建外键约束（由于工作区基础库默认配置 DisableForeignKeyConstraintWhenMigrating，此处补充权威外键以保障数据防腐）
	fkSQL := `
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_fg_subsystem') THEN
        ALTER TABLE traefik_functional_groups
        ADD CONSTRAINT fk_traefik_fg_subsystem
        FOREIGN KEY (subsystem_id) REFERENCES traefik_subsystems(id)
        ON UPDATE CASCADE ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_fg_parent') THEN
        ALTER TABLE traefik_functional_groups
        ADD CONSTRAINT fk_traefik_fg_parent
        FOREIGN KEY (parent_id) REFERENCES traefik_functional_groups(id)
        ON UPDATE CASCADE ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_modules_subsystem') THEN
        ALTER TABLE traefik_modules
        ADD CONSTRAINT fk_traefik_modules_subsystem
        FOREIGN KEY (subsystem_id) REFERENCES traefik_subsystems(id)
        ON UPDATE CASCADE ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_modules_group') THEN
        ALTER TABLE traefik_modules
        ADD CONSTRAINT fk_traefik_modules_group
        FOREIGN KEY (group_id) REFERENCES traefik_functional_groups(id)
        ON UPDATE CASCADE ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_mod_svc_module') THEN
        ALTER TABLE traefik_module_services
        ADD CONSTRAINT fk_traefik_mod_svc_module
        FOREIGN KEY (module_id) REFERENCES traefik_modules(id)
        ON UPDATE CASCADE ON DELETE CASCADE;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_dep_consumer') THEN
        ALTER TABLE traefik_module_dependencies
        ADD CONSTRAINT fk_traefik_dep_consumer
        FOREIGN KEY (consumer_module_id) REFERENCES traefik_modules(id)
        ON UPDATE CASCADE ON DELETE CASCADE;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_dep_service') THEN
        ALTER TABLE traefik_module_dependencies
        ADD CONSTRAINT fk_traefik_dep_service
        FOREIGN KEY (service_id) REFERENCES traefik_module_services(id)
        ON UPDATE CASCADE ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_dep_provider') THEN
        ALTER TABLE traefik_module_dependencies
        ADD CONSTRAINT fk_traefik_dep_provider
        FOREIGN KEY (provider_module_id) REFERENCES traefik_modules(id)
        ON UPDATE CASCADE ON DELETE SET NULL;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_proc_agent') THEN
        ALTER TABLE traefik_processes
        ADD CONSTRAINT fk_traefik_proc_agent
        FOREIGN KEY (agent_id) REFERENCES traefik_agents(id)
        ON UPDATE CASCADE ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_proc_mod_proc') THEN
        ALTER TABLE traefik_process_modules
        ADD CONSTRAINT fk_traefik_proc_mod_proc
        FOREIGN KEY (process_id) REFERENCES traefik_processes(id)
        ON UPDATE CASCADE ON DELETE CASCADE;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_proc_mod_module') THEN
        ALTER TABLE traefik_process_modules
        ADD CONSTRAINT fk_traefik_proc_mod_module
        FOREIGN KEY (module_id) REFERENCES traefik_modules(id)
        ON UPDATE CASCADE ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_proto_op_schema') THEN
        ALTER TABLE traefik_proto_operations
        ADD CONSTRAINT fk_traefik_proto_op_schema
        FOREIGN KEY (schema_id) REFERENCES traefik_proto_schemas(id)
        ON UPDATE CASCADE ON DELETE CASCADE;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_replay_task_session') THEN
        ALTER TABLE traefik_replay_tasks
        ADD CONSTRAINT fk_traefik_replay_task_session
        FOREIGN KEY (session_id) REFERENCES traefik_recording_sessions(id)
        ON UPDATE CASCADE ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_replay_task_item') THEN
        ALTER TABLE traefik_replay_items
        ADD CONSTRAINT fk_traefik_replay_task_item
        FOREIGN KEY (task_id) REFERENCES traefik_replay_tasks(id)
        ON UPDATE CASCADE ON DELETE CASCADE;
    END IF;
END $$;
`
	if err := DB.Exec(fkSQL).Error; err != nil {
		return fmt.Errorf("创建外键完整性约束失败: %w", err)
	}

	// 5. 确保独立运行模式下至少存在一个默认管理员账号
	if err := commonAuth.EnsureSeedAdmin(DB, config.AdminRole); err != nil {
		log.Printf("[%s] 提示: 初始化默认管理员账号未完成: %v", config.ServiceName, err)
	}

	// 6. 初始化治理参数种子数据
	if err := seedDefaultSettings(DB); err != nil {
		log.Printf("[%s] 提示: 初始化模块配置参数种子未完成: %v", config.ServiceName, err)
	}

	return nil
}

func seedDefaultSettings(db *gorm.DB) error {
	var settingCount int64
	if err := db.Model(&TraefikSetting{}).Count(&settingCount).Error; err != nil {
		return err
	}
	if settingCount == 0 {
		defaultSettings := []TraefikSetting{
			{
				SettingKey:  "traffic_retention_days",
				SettingVal:  "30",
				Description: "流量往返记录物理分区保留天数（超出自动执行 DROP TABLE 销毁）",
				UpdatedBy:   "system",
			},
			{
				SettingKey:  "default_max_frames",
				SettingVal:  "100000",
				Description: "录制任务默认单批次最大捕获帧数上限",
				UpdatedBy:   "system",
			},
			{
				SettingKey:  "default_max_duration_sec",
				SettingVal:  "1800",
				Description: "录制任务默认单批次最大录制时长（秒）",
				UpdatedBy:   "system",
			},
			{
				SettingKey:  "default_max_frame_bytes",
				SettingVal:  "4194304",
				Description: "单帧报文截断排空阈值（字节，默认 4MB）",
				UpdatedBy:   "system",
			},
		}
		if err := db.Create(&defaultSettings).Error; err != nil {
			return err
		}
	}
	return nil
}
