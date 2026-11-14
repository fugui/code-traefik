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

	// 0. 清库直接重建检测：若存在旧版 traefik_process_modules 或旧版进程/模块列，执行幂等直接级联清理重建
	if err := cleanLegacyTablesIfNeeded(DB); err != nil {
		return fmt.Errorf("检测并清理旧版表结构失败: %w", err)
	}

	// 1. 自动迁移标准实体表（核心 traefik_* 表 + 平台公共审计/用户表，ADR-14 ~ ADR-18）
	if err := DB.AutoMigrate(
		&Department{},
		&User{},
		&SysAuditLog{},
		&TraefikSetting{},
		&Subsystem{},
		&FunctionalGroup{},
		&ArchProcessGroup{},
		&ArchProcess{},
		&Module{},
		&ServiceContract{},
		&ModuleService{},
		&ModuleDependency{},
		&Agent{},
		&Process{},
		&GitRepo{},
		&GitProtoBundle{},
		&ProtoFile{},
		&ProtoFileVersion{},
		&ProtoSchema{},
		&ProtoOperation{},
		&RecordingSession{},
		&ReplayTask{},
		&ReplayItem{},
		&GoldenSuite{},
		&GoldenSuiteItem{},
		&TopologyLayout{},
		&ProtoDocSymbol{},
		&CheckRule{},
		&VocabularyTerm{},
		&CheckReport{},
		&CheckViolation{},
		&MRCheckRun{},
		&Notification{},
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
	procConstraintSQL := `
DO $$
BEGIN
    -- arch_processes 默认端口检查约束
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_arch_proc_port') THEN
        ALTER TABLE traefik_arch_processes DROP CONSTRAINT chk_traefik_arch_proc_port;
    END IF;
    ALTER TABLE traefik_arch_processes ADD CONSTRAINT chk_traefik_arch_proc_port CHECK (default_listen_port >= 0 AND default_listen_port <= 65535);

    -- processes 物化有效端口检查约束
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_proc_port') THEN
        ALTER TABLE traefik_processes DROP CONSTRAINT chk_traefik_proc_port;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_proc_eff_port') THEN
        ALTER TABLE traefik_processes DROP CONSTRAINT chk_traefik_proc_eff_port;
    END IF;
    ALTER TABLE traefik_processes ADD CONSTRAINT chk_traefik_proc_eff_port CHECK (effective_listen_port >= 0 AND effective_listen_port <= 65535);

    -- processes 覆盖端口检查约束
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_proc_override_port') THEN
        ALTER TABLE traefik_processes DROP CONSTRAINT chk_traefik_proc_override_port;
    END IF;
    ALTER TABLE traefik_processes ADD CONSTRAINT chk_traefik_proc_override_port CHECK (override_listen_port IS NULL OR (override_listen_port >= 0 AND override_listen_port <= 65535));

    -- processes 预挂期望状态检查约束
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_proc_desired') THEN
        ALTER TABLE traefik_processes DROP CONSTRAINT chk_traefik_proc_desired;
    END IF;
    ALTER TABLE traefik_processes ADD CONSTRAINT chk_traefik_proc_desired CHECK (probe_desired_state IN ('ARMED','DISARMED'));

    -- 移除旧索引（若存在）
    DROP INDEX IF EXISTS uk_traefik_proc_agent_port;
    DROP INDEX IF EXISTS uk_traefik_proc_agent_name;
END $$;`
	if err := DB.Exec(procConstraintSQL).Error; err != nil {
		return fmt.Errorf("更新进程端口检查约束失败: %w", err)
	}

	protoConstraintSQL := `
DO $$
BEGIN
    -- traefik_proto_schemas 大小检查约束升级为 16MB (16777216 字节)
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_proto_size') THEN
        ALTER TABLE traefik_proto_schemas DROP CONSTRAINT chk_traefik_proto_size;
    END IF;
    ALTER TABLE traefik_proto_schemas ADD CONSTRAINT chk_traefik_proto_size CHECK (file_size > 0 AND file_size <= 16777216);

    -- traefik_proto_schemas 分类检查约束
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_proto_category') THEN
        ALTER TABLE traefik_proto_schemas DROP CONSTRAINT chk_traefik_proto_category;
    END IF;
    ALTER TABLE traefik_proto_schemas ADD CONSTRAINT chk_traefik_proto_category CHECK (category IN ('BASE_LIB', 'BUSINESS', 'GATEWAY'));

    -- traefik_proto_schemas 资产来源类型检查约束 (M22 ADR-18)
    UPDATE traefik_proto_schemas SET source_type = 'UPLOAD_PB' WHERE source_type = '' OR source_type IS NULL;
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_proto_source_type') THEN
        ALTER TABLE traefik_proto_schemas DROP CONSTRAINT chk_traefik_proto_source_type;
    END IF;
    ALTER TABLE traefik_proto_schemas ADD CONSTRAINT chk_traefik_proto_source_type CHECK (source_type IN ('UPLOAD_PB', 'ONLINE_COMPILE', 'GIT_SYNC', 'GIT_MR_AUTO'));

    -- traefik_subsystems / traefik_modules 架构分层约束 (M22 ADR-18)
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_subsystems_layer') THEN
        ALTER TABLE traefik_subsystems DROP CONSTRAINT chk_traefik_subsystems_layer;
    END IF;
    ALTER TABLE traefik_subsystems ADD CONSTRAINT chk_traefik_subsystems_layer CHECK (layer_level >= 0 AND layer_level <= 99);

    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_modules_layer') THEN
        ALTER TABLE traefik_modules DROP CONSTRAINT chk_traefik_modules_layer;
    END IF;
    ALTER TABLE traefik_modules ADD CONSTRAINT chk_traefik_modules_layer CHECK (layer_level >= 0 AND layer_level <= 99);

    -- traefik_golden_suites 状态检查约束 (F-25, ADR-14)
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_golden_suite_status') THEN
        ALTER TABLE traefik_golden_suites DROP CONSTRAINT chk_traefik_golden_suite_status;
    END IF;
    ALTER TABLE traefik_golden_suites ADD CONSTRAINT chk_traefik_golden_suite_status CHECK (status IN ('ACTIVE', 'ARCHIVED'));

    -- traefik_topology_layouts 公私域、视图与归属组合检查约束 (F-04.3, ADR-15)
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_topo_layout_scope') THEN
        ALTER TABLE traefik_topology_layouts DROP CONSTRAINT chk_traefik_topo_layout_scope;
    END IF;
    ALTER TABLE traefik_topology_layouts ADD CONSTRAINT chk_traefik_topo_layout_scope CHECK (scope IN ('SHARED','PRIVATE'));

    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_topo_layout_view') THEN
        ALTER TABLE traefik_topology_layouts DROP CONSTRAINT chk_traefik_topo_layout_view;
    END IF;
    ALTER TABLE traefik_topology_layouts ADD CONSTRAINT chk_traefik_topo_layout_view CHECK (view IN ('LOGICAL','PHYSICAL'));

    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_topo_layout_owner') THEN
        ALTER TABLE traefik_topology_layouts DROP CONSTRAINT chk_traefik_topo_layout_owner;
    END IF;
    ALTER TABLE traefik_topology_layouts ADD CONSTRAINT chk_traefik_topo_layout_owner
        CHECK ((scope = 'SHARED' AND owner_id IS NULL AND focus_scope = 'GLOBAL' AND is_default = false)
            OR (scope = 'PRIVATE' AND owner_id IS NOT NULL));

    -- traefik_proto_doc_symbols 符号类型检查约束 (F-18, M23)
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_pdoc_kind') THEN
        ALTER TABLE traefik_proto_doc_symbols DROP CONSTRAINT chk_traefik_pdoc_kind;
    END IF;
    ALTER TABLE traefik_proto_doc_symbols ADD CONSTRAINT chk_traefik_pdoc_kind
        CHECK (symbol_kind IN ('SERVICE','METHOD','MESSAGE','ENUM'));

    -- 移除旧版 uk_traefik_replay_task_item 索引（若存在），由 uk_traefik_replay_task_seq 替代
    DROP INDEX IF EXISTS uk_traefik_replay_task_item;
END $$;`
	if err := DB.Exec(protoConstraintSQL).Error; err != nil {
		return fmt.Errorf("更新描述集检查约束失败: %w", err)
	}

	contractConstraintSQL := `
DO $$
BEGIN
    -- 1. traefik_modules 类型与进程绑定检查约束 (ADR-16 §3.1)
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_modules_kind') THEN
        ALTER TABLE traefik_modules DROP CONSTRAINT chk_traefik_modules_kind;
    END IF;
    ALTER TABLE traefik_modules ADD CONSTRAINT chk_traefik_modules_kind CHECK (module_kind IN ('BUSINESS', 'CONTRACT_LIB'));

    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_modules_lib_no_proc') THEN
        ALTER TABLE traefik_modules DROP CONSTRAINT chk_traefik_modules_lib_no_proc;
    END IF;
    ALTER TABLE traefik_modules ADD CONSTRAINT chk_traefik_modules_lib_no_proc CHECK (module_kind = 'BUSINESS' OR arch_process_id IS NULL);

    -- 2. traefik_service_contracts 提供模式检查约束 (ADR-16 §3.2)
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_svc_contract_mode') THEN
        ALTER TABLE traefik_service_contracts DROP CONSTRAINT chk_traefik_svc_contract_mode;
    END IF;
    ALTER TABLE traefik_service_contracts ADD CONSTRAINT chk_traefik_svc_contract_mode CHECK (provide_mode IN ('MULTI_IMPL'));

    -- 3. traefik_module_services 契约实现与自定义运行期服务名约束 (ADR-16 v2.0)
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_mod_svc_contract_impl') THEN
        ALTER TABLE traefik_module_services DROP CONSTRAINT chk_traefik_mod_svc_contract_impl;
    END IF;
    ALTER TABLE traefik_module_services ADD CONSTRAINT chk_traefik_mod_svc_contract_impl CHECK ((contract_id IS NULL AND impl_key = '') OR (contract_id IS NOT NULL AND impl_key <> ''));

    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_mod_svc_custom_name') THEN
        ALTER TABLE traefik_module_services DROP CONSTRAINT chk_traefik_mod_svc_custom_name;
    END IF;
    ALTER TABLE traefik_module_services ADD CONSTRAINT chk_traefik_mod_svc_custom_name CHECK (contract_id IS NOT NULL OR is_custom_service_name = false);

    -- 4. traefik_module_dependencies 内部实现/分发契约/外部三选一检查约束 (ADR-16 §3.5)
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_dep_target_xor') THEN
        ALTER TABLE traefik_module_dependencies DROP CONSTRAINT chk_traefik_dep_target_xor;
    END IF;
    ALTER TABLE traefik_module_dependencies ADD CONSTRAINT chk_traefik_dep_target_xor CHECK (num_nonnulls(service_id, contract_id) + (CASE WHEN is_external THEN 1 ELSE 0 END) = 1);

    -- 5. 重建服务名部分唯一索引（兼容存量库，先删除旧索引再由下方 partialIndexes 幂等重建）
    DROP INDEX IF EXISTS uk_traefik_mod_svc_name;
    DROP INDEX IF EXISTS uk_traefik_mod_svc_name_excl;

    -- 6. 数据回填：依据与关联契约服务名是否一致修正 is_custom_service_name 标记
    UPDATE traefik_module_services ms
    SET is_custom_service_name = (ms.service_name <> sc.service_name)
    FROM traefik_service_contracts sc
    WHERE ms.contract_id = sc.id
      AND ms.is_custom_service_name IS DISTINCT FROM (ms.service_name <> sc.service_name);
END $$;`
	if err := DB.Exec(contractConstraintSQL).Error; err != nil {
		return fmt.Errorf("更新多实现服务契约检查约束失败: %w", err)
	}

	partialIndexes := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_traefik_proto_active_name ON traefik_proto_schemas (name) WHERE is_active = true;`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_traefik_active_proto_op ON traefik_proto_operations (service_name, operation) WHERE is_active = true;`,
		`CREATE INDEX IF NOT EXISTS idx_traefik_proto_schemas_cat_active ON traefik_proto_schemas (category, is_active);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_traefik_proc_agent_port ON traefik_processes (agent_id, effective_listen_port) WHERE effective_listen_port > 0;`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_traefik_proc_agent_arch ON traefik_processes (agent_id, arch_process_id);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_traefik_golden_suite_code ON traefik_golden_suites (code);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_traefik_golden_item_order ON traefik_golden_suite_items (suite_id, order_no);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_traefik_replay_task_seq ON traefik_replay_items (task_id, seq_no);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_traefik_topo_layout_shared ON traefik_topology_layouts (view) WHERE scope = 'SHARED';`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_traefik_topo_layout_private_name ON traefik_topology_layouts (owner_id, view, name) WHERE scope = 'PRIVATE';`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_traefik_topo_layout_private_default ON traefik_topology_layouts (owner_id, view, focus_scope) WHERE scope = 'PRIVATE' AND is_default;`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_traefik_mod_svc_name_excl ON traefik_module_services (service_name) WHERE contract_id IS NULL OR is_custom_service_name = true;`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_traefik_mod_svc_contract_impl ON traefik_module_services (contract_id, impl_key) WHERE contract_id IS NOT NULL;`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_traefik_mod_svc_contract_mod ON traefik_module_services (contract_id, module_id) WHERE contract_id IS NOT NULL;`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_traefik_dep_consumer_contract ON traefik_module_dependencies (consumer_module_id, contract_id) WHERE contract_id IS NOT NULL;`,
		`CREATE INDEX IF NOT EXISTS idx_traefik_pdoc_schema_kind ON traefik_proto_doc_symbols (schema_id, symbol_kind);`,
		`CREATE INDEX IF NOT EXISTS idx_traefik_pdoc_fts ON traefik_proto_doc_symbols USING gin (to_tsvector('simple', coalesce(full_name,'') || ' ' || coalesce(summary,'') || ' ' || coalesce(leading_comments,'') || ' ' || coalesce(trailing_comments,'') || ' ' || coalesce(owner_tag,'')));`,
		`CREATE INDEX IF NOT EXISTS idx_traefik_pdoc_fields_gin ON traefik_proto_doc_symbols USING gin (fields_meta_json);`,
		`CREATE INDEX IF NOT EXISTS idx_traefik_pdoc_err_gin ON traefik_proto_doc_symbols USING gin (error_codes_json);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uk_traefik_pfile_bundle_path ON traefik_proto_files (bundle_id, file_path) WHERE is_deleted = false;`,
		`CREATE INDEX IF NOT EXISTS idx_traefik_pfile_active_cat_path ON traefik_proto_files (file_path, category) WHERE is_deleted = false;`,
		`DO $$
BEGIN
	IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_git_repo_provider') THEN
		ALTER TABLE traefik_git_repos ADD CONSTRAINT chk_traefik_git_repo_provider CHECK (provider IN ('GITLAB','GITHUB','GITEA','GENERIC'));
	END IF;
	IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_git_repo_auth_type') THEN
		ALTER TABLE traefik_git_repos ADD CONSTRAINT chk_traefik_git_repo_auth_type CHECK (auth_type IN ('NONE','TOKEN','SSH_KEY'));
	END IF;
	IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_git_repo_sync_mode') THEN
		ALTER TABLE traefik_git_repos ADD CONSTRAINT chk_traefik_git_repo_sync_mode CHECK (sync_mode IN ('WEBHOOK','POLL','MANUAL'));
	END IF;
	IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_git_bundle_category') THEN
		ALTER TABLE traefik_git_proto_bundles ADD CONSTRAINT chk_traefik_git_bundle_category CHECK (category IN ('BASE_LIB','BUSINESS','GATEWAY'));
	END IF;
	IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_git_bundle_mr_publish') THEN
		ALTER TABLE traefik_git_proto_bundles ADD CONSTRAINT chk_traefik_git_bundle_mr_publish CHECK (mr_auto_publish IN ('DISABLED','DRAFT_ONLY','AUTO_ACTIVATE'));
	END IF;
	IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_git_bundles_repo') THEN
		ALTER TABLE traefik_git_proto_bundles ADD CONSTRAINT fk_traefik_git_bundles_repo FOREIGN KEY (git_repo_id) REFERENCES traefik_git_repos(id) ON DELETE CASCADE;
	END IF;
	IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_proto_files_repo') THEN
		ALTER TABLE traefik_proto_files ADD CONSTRAINT fk_traefik_proto_files_repo FOREIGN KEY (git_repo_id) REFERENCES traefik_git_repos(id) ON DELETE CASCADE;
	END IF;
	IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_proto_files_bundle') THEN
		ALTER TABLE traefik_proto_files ADD CONSTRAINT fk_traefik_proto_files_bundle FOREIGN KEY (bundle_id) REFERENCES traefik_git_proto_bundles(id) ON DELETE CASCADE;
	END IF;
	IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_proto_file_versions_file') THEN
		ALTER TABLE traefik_proto_file_versions ADD CONSTRAINT fk_traefik_proto_file_versions_file FOREIGN KEY (proto_file_id) REFERENCES traefik_proto_files(id) ON DELETE CASCADE;
	END IF;
	IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_proto_files_latest_ver') THEN
		ALTER TABLE traefik_proto_files ADD CONSTRAINT fk_traefik_proto_files_latest_ver FOREIGN KEY (latest_version_id) REFERENCES traefik_proto_file_versions(id) ON DELETE SET NULL;
	END IF;
	IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_proto_schemas_git_repo') THEN
		ALTER TABLE traefik_proto_schemas ADD CONSTRAINT fk_traefik_proto_schemas_git_repo FOREIGN KEY (git_repo_id) REFERENCES traefik_git_repos(id) ON DELETE SET NULL;
	END IF;
	IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_proto_schemas_source_bundle') THEN
		ALTER TABLE traefik_proto_schemas ADD CONSTRAINT fk_traefik_proto_schemas_source_bundle FOREIGN KEY (source_bundle_id) REFERENCES traefik_git_proto_bundles(id) ON DELETE SET NULL;
	END IF;
END $$;`,
	}
	for _, idxSQL := range partialIndexes {
		if err := DB.Exec(idxSQL).Error; err != nil {
			return fmt.Errorf("创建部分唯一索引失败: %w", err)
		}
	}

	// 存量描述集 proto_file_count 与 service_count 幂等回填
	backfillSQL := `
UPDATE traefik_proto_schemas 
SET proto_file_count = jsonb_array_length(proto_files) 
WHERE proto_file_count = 0 
  AND proto_files IS NOT NULL 
  AND jsonb_typeof(proto_files) = 'array' 
  AND jsonb_array_length(proto_files) > 0;

UPDATE traefik_proto_schemas 
SET service_count = sub.cnt
FROM (
    SELECT schema_id, COUNT(DISTINCT service_name) AS cnt
    FROM traefik_proto_operations
    GROUP BY schema_id
) sub
WHERE traefik_proto_schemas.id = sub.schema_id 
  AND traefik_proto_schemas.service_count = 0;
`
	if err := DB.Exec(backfillSQL).Error; err != nil {
		return fmt.Errorf("回填描述集统计计数失败: %w", err)
	}

	// 幂等补齐复合索引 idx_traefik_tr_session_req_ns (ADR-12, M13)
	if err := DB.Exec(`CREATE INDEX IF NOT EXISTS idx_traefik_tr_session_req_ns ON traefik_traffic_records (session_id, req_captured_at_ns, id)`).Error; err != nil {
		log.Printf("[models] 提示: 创建复合索引 idx_traefik_tr_session_req_ns: %v", err)
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
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_modules_arch_proc') THEN
        ALTER TABLE traefik_modules
        ADD CONSTRAINT fk_traefik_modules_arch_proc
        FOREIGN KEY (arch_process_id) REFERENCES traefik_arch_processes(id)
        ON UPDATE CASCADE ON DELETE SET NULL;
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
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_arch_proc_group') THEN
        ALTER TABLE traefik_arch_processes
        ADD CONSTRAINT fk_traefik_arch_proc_group
        FOREIGN KEY (group_id) REFERENCES traefik_arch_process_groups(id)
        ON UPDATE CASCADE ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_proc_agent') THEN
        ALTER TABLE traefik_processes
        ADD CONSTRAINT fk_traefik_proc_agent
        FOREIGN KEY (agent_id) REFERENCES traefik_agents(id)
        ON UPDATE CASCADE ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_proc_arch') THEN
        ALTER TABLE traefik_processes
        ADD CONSTRAINT fk_traefik_proc_arch
        FOREIGN KEY (arch_process_id) REFERENCES traefik_arch_processes(id)
        ON UPDATE CASCADE ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_agents_fallback') THEN
        ALTER TABLE traefik_agents
        ADD CONSTRAINT fk_traefik_agents_fallback
        FOREIGN KEY (fallback_agent_id) REFERENCES traefik_agents(id)
        ON UPDATE CASCADE ON DELETE SET NULL;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_proto_op_schema') THEN
        ALTER TABLE traefik_proto_operations
        ADD CONSTRAINT fk_traefik_proto_op_schema
        FOREIGN KEY (schema_id) REFERENCES traefik_proto_schemas(id)
        ON UPDATE CASCADE ON DELETE CASCADE;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_pdoc_schema') THEN
        ALTER TABLE traefik_proto_doc_symbols
        ADD CONSTRAINT fk_traefik_pdoc_schema
        FOREIGN KEY (schema_id) REFERENCES traefik_proto_schemas(id)
        ON UPDATE CASCADE ON DELETE CASCADE;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_replay_task_session') THEN
        ALTER TABLE traefik_replay_tasks DROP CONSTRAINT fk_traefik_replay_task_session;
    END IF;
    ALTER TABLE traefik_replay_tasks
    ADD CONSTRAINT fk_traefik_replay_task_session
    FOREIGN KEY (session_id) REFERENCES traefik_recording_sessions(id)
    ON UPDATE CASCADE ON DELETE SET NULL;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_golden_suites_arch_proc') THEN
        ALTER TABLE traefik_golden_suites
        ADD CONSTRAINT fk_traefik_golden_suites_arch_proc
        FOREIGN KEY (arch_process_id) REFERENCES traefik_arch_processes(id)
        ON UPDATE CASCADE ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_golden_suite_items_suite') THEN
        ALTER TABLE traefik_golden_suite_items
        ADD CONSTRAINT fk_traefik_golden_suite_items_suite
        FOREIGN KEY (suite_id) REFERENCES traefik_golden_suites(id)
        ON UPDATE CASCADE ON DELETE CASCADE;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_replay_task_suite') THEN
        ALTER TABLE traefik_replay_tasks
        ADD CONSTRAINT fk_traefik_replay_task_suite
        FOREIGN KEY (golden_suite_id) REFERENCES traefik_golden_suites(id)
        ON UPDATE CASCADE ON DELETE SET NULL;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_replay_item_suite_item') THEN
        ALTER TABLE traefik_replay_items
        ADD CONSTRAINT fk_traefik_replay_item_suite_item
        FOREIGN KEY (suite_item_id) REFERENCES traefik_golden_suite_items(id)
        ON UPDATE CASCADE ON DELETE SET NULL;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_replay_task_arch_proc') THEN
        ALTER TABLE traefik_replay_tasks
        ADD CONSTRAINT fk_traefik_replay_task_arch_proc
        FOREIGN KEY (arch_process_id) REFERENCES traefik_arch_processes(id)
        ON UPDATE CASCADE ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_replay_task_src_proc') THEN
        ALTER TABLE traefik_replay_tasks
        ADD CONSTRAINT fk_traefik_replay_task_src_proc
        FOREIGN KEY (source_process_id) REFERENCES traefik_processes(id)
        ON UPDATE CASCADE ON DELETE SET NULL;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_traefik_replay_task_tgt_proc') THEN
        ALTER TABLE traefik_replay_tasks
        ADD CONSTRAINT fk_traefik_replay_task_tgt_proc
        FOREIGN KEY (target_process_id) REFERENCES traefik_processes(id)
        ON UPDATE CASCADE ON DELETE SET NULL;
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

	if err := ensureLinterConstraints(DB); err != nil {
		return fmt.Errorf("初始化静态检查约束失败: %w", err)
	}
	if err := ensureM26MRGateAndNotificationConstraints(DB); err != nil {
		return fmt.Errorf("初始化 M26 MR 门禁与通知约束失败: %w", err)
	}
	if err := EnsureBuiltinCheckRules(DB); err != nil {
		return fmt.Errorf("初始化内置静态检查规则失败: %w", err)
	}
	if err := EnsureBuiltinVocabularyTerms(DB); err != nil {
		return fmt.Errorf("初始化基础标准词库失败: %w", err)
	}
	if err := ensureM24DefaultSettings(DB); err != nil {
		log.Printf("[%s] 提示: 初始化 M24 安全/LLM 默认参数未完成: %v", config.ServiceName, err)
	}

	return nil
}

func cleanLegacyTablesIfNeeded(db *gorm.DB) error {
	var hasLegacyTable bool
	var hasMissingArchCol bool
	var hasLegacyModProcessCol bool

	// Check traefik_process_modules
	_ = db.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'traefik_process_modules')`).Scan(&hasLegacyTable).Error

	// Check traefik_processes lacking arch_process_id
	var procTableExists bool
	_ = db.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'traefik_processes')`).Scan(&procTableExists).Error
	if procTableExists {
		var colExists bool
		_ = db.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'traefik_processes' AND column_name = 'arch_process_id')`).Scan(&colExists).Error
		if !colExists {
			hasMissingArchCol = true
		}
	}

	// Check traefik_modules.process_id
	_ = db.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'traefik_modules' AND column_name = 'process_id')`).Scan(&hasLegacyModProcessCol).Error

	if hasLegacyTable || hasMissingArchCol || hasLegacyModProcessCol {
		log.Println("[models] 检测到旧版表结构，执行清库直接重建级联清理...")
		cleanSQL := `
			DROP TABLE IF EXISTS traefik_replay_items CASCADE;
			DROP TABLE IF EXISTS traefik_replay_tasks CASCADE;
			DROP TABLE IF EXISTS traefik_golden_suite_items CASCADE;
			DROP TABLE IF EXISTS traefik_golden_suites CASCADE;
			DROP TABLE IF EXISTS traefik_traffic_records CASCADE;
			DROP TABLE IF EXISTS traefik_recording_sessions CASCADE;
			DROP TABLE IF EXISTS traefik_process_modules CASCADE;
			DROP TABLE IF EXISTS traefik_processes CASCADE;
			ALTER TABLE IF EXISTS traefik_modules DROP COLUMN IF EXISTS process_id CASCADE;
		`
		if err := db.Exec(cleanSQL).Error; err != nil {
			return fmt.Errorf("清理旧表结构失败: %w", err)
		}
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
			{
				SettingKey:  "default_probe_desired_state_shared",
				SettingVal:  "ARMED",
				Description: "公共环境 Agent 挂载进程默认预挂探针期望状态 (ARMED/DISARMED)",
				UpdatedBy:   "system",
			},
			{
				SettingKey:  "default_probe_desired_state_private",
				SettingVal:  "ARMED",
				Description: "个人环境 Agent 挂载进程默认预挂探针期望状态 (ARMED/DISARMED)",
				UpdatedBy:   "system",
			},
		}
		if err := db.Create(&defaultSettings).Error; err != nil {
			return err
		}
	}
	return nil
}
