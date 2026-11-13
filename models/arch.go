package models

import (
	"time"

	"code-traefik/config"
	"gorm.io/datatypes"
)

// 1. Subsystem 顶层业务子系统表 (traefik_subsystems)
type Subsystem struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Code        string    `gorm:"size:64;not null;uniqueIndex:uk_traefik_subsystems_code" json:"code"` // 全局唯一大写编码
	Name        string    `gorm:"size:128;not null;index:idx_traefik_subsystems_name" json:"name"`
	OwnerID     *uint     `gorm:"index:idx_traefik_subsystems_owner" json:"owner_id"`
	Owner       *User     `gorm:"foreignKey:OwnerID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL" json:"owner,omitempty"`
	Description string    `gorm:"type:text" json:"description"`
	CreatedBy   uint      `gorm:"not null;default:0" json:"created_by"`
	UpdatedBy   uint      `gorm:"not null;default:0" json:"updated_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (Subsystem) TableName() string { return config.TablePrefix + "subsystems" }

// 2. FunctionalGroup 多级自嵌套功能组树表 (traefik_functional_groups)
type FunctionalGroup struct {
	ID          uint64           `gorm:"primaryKey;autoIncrement" json:"id"`
	SubsystemID uint64           `gorm:"not null;uniqueIndex:uk_traefik_fg_sub_code,priority:1;index:idx_traefik_fg_sub_path,priority:1" json:"subsystem_id"`
	Subsystem   *Subsystem       `gorm:"foreignKey:SubsystemID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"subsystem,omitempty"`
	ParentID    *uint64          `gorm:"index:idx_traefik_fg_parent" json:"parent_id"` // 根节点为 nil
	Parent      *FunctionalGroup `gorm:"foreignKey:ParentID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	Code        string           `gorm:"size:64;not null;uniqueIndex:uk_traefik_fg_sub_code,priority:2" json:"code"` // 同子系统内唯一
	Name        string           `gorm:"size:128;not null" json:"name"`
	Level       int              `gorm:"not null;default:1;check:chk_traefik_fg_level,level >= 1 AND level <= 8" json:"level"` // 最大深度 8 级
	Path        string           `gorm:"size:256;not null;index:idx_traefik_fg_sub_path,priority:2" json:"path"`               // 物化路径 "/10/12/"
	SortOrder   int              `gorm:"not null;default:0" json:"sort_order"`
	Description string           `gorm:"type:text" json:"description"`
	CreatedBy   uint             `gorm:"not null;default:0" json:"created_by"`
	UpdatedBy   uint             `gorm:"not null;default:0" json:"updated_by"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

func (FunctionalGroup) TableName() string { return config.TablePrefix + "functional_groups" }

// 3. ArchProcessGroup 全局架构物理进程组蓝图表 (traefik_arch_process_groups)
type ArchProcessGroup struct {
	ID          uint64        `gorm:"primaryKey;autoIncrement" json:"id"`
	Code        string        `gorm:"size:64;not null;uniqueIndex:uk_traefik_arch_pgrp_code" json:"code"` // 全局唯一大写编码，如 "CORE_TRADE"
	Name        string        `gorm:"size:128;not null;uniqueIndex:uk_traefik_arch_pgrp_name" json:"name"`
	SortOrder   int           `gorm:"not null;default:0" json:"sort_order"`
	Description string        `gorm:"type:text" json:"description"`
	Processes   []ArchProcess `gorm:"foreignKey:GroupID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"processes,omitempty"`
	CreatedBy   uint          `gorm:"not null;default:0" json:"created_by"`
	UpdatedBy   uint          `gorm:"not null;default:0" json:"updated_by"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

func (ArchProcessGroup) TableName() string { return config.TablePrefix + "arch_process_groups" }

// 4. ArchProcess 全局架构标准业务进程蓝图表 (traefik_arch_processes)
type ArchProcess struct {
	ID                      uint64            `gorm:"primaryKey;autoIncrement" json:"id"`
	GroupID                 uint64            `gorm:"not null;index:idx_traefik_arch_proc_group" json:"group_id"`
	Group                   *ArchProcessGroup `gorm:"foreignKey:GroupID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"group,omitempty"`
	Code                    string            `gorm:"size:64;not null;uniqueIndex:uk_traefik_arch_proc_code" json:"code"` // 全局唯一标准进程编码，如 "sut-order"
	Name                    string            `gorm:"size:128;not null;index:idx_traefik_arch_proc_name" json:"name"`
	DefaultListenPort       int               `gorm:"not null;default:0;check:chk_traefik_arch_proc_port,default_listen_port >= 0 AND default_listen_port <= 65535" json:"default_listen_port"` // 0 表示纯客户端进程；1..65535 为默认监听端口
	DefaultCmdPattern       string            `gorm:"size:512;not null;default:''" json:"default_cmd_pattern"`                                                                                  // 默认 /proc/<pid>/cmdline 匹配特征
	DefaultExtraDownstreams datatypes.JSON    `gorm:"type:jsonb;not null;default:'[]'" json:"default_extra_downstreams"`                                                                        // 默认补充下游 ["IP:Port"]
	SortOrder               int               `gorm:"not null;default:0" json:"sort_order"`
	Description             string            `gorm:"type:text" json:"description"`
	Modules                 []Module          `gorm:"foreignKey:ArchProcessID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL" json:"modules,omitempty"` // 组装承载的 FM 功能模块集合 (1:N)
	CreatedBy               uint              `gorm:"not null;default:0" json:"created_by"`
	UpdatedBy               uint              `gorm:"not null;default:0" json:"updated_by"`
	CreatedAt               time.Time         `json:"created_at"`
	UpdatedAt               time.Time         `json:"updated_at"`
}

func (ArchProcess) TableName() string { return config.TablePrefix + "arch_processes" }

// Module 模块类型常量 (ADR-16)
const (
	ModuleKindBusiness    = "BUSINESS"
	ModuleKindContractLib = "CONTRACT_LIB"
)

// 5. Module 最小接口契约功能模块表 (traefik_modules)
type Module struct {
	ID            uint64           `gorm:"primaryKey;autoIncrement" json:"id"`
	SubsystemID   uint64           `gorm:"not null;index:idx_traefik_modules_subsystem" json:"subsystem_id"` // 冗余所属子系统加速检索
	Subsystem     *Subsystem       `gorm:"foreignKey:SubsystemID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"subsystem,omitempty"`
	GroupID       uint64           `gorm:"not null;index:idx_traefik_modules_group" json:"group_id"` // 可挂载于任意层级功能组
	Group         *FunctionalGroup `gorm:"foreignKey:GroupID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"group,omitempty"`
	ArchProcessID *uint64          `gorm:"index:idx_traefik_modules_arch_proc" json:"arch_process_id"` // 指向全局架构标准进程蓝图 (A-03 单模块至多归属 1 个标准进程)
	ArchProcess   *ArchProcess     `gorm:"foreignKey:ArchProcessID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL" json:"arch_process,omitempty"`
	ModuleKind    string           `gorm:"size:16;not null;default:'BUSINESS';index:idx_traefik_modules_kind" json:"module_kind"` // BUSINESS | CONTRACT_LIB (ADR-16)
	Code          string           `gorm:"size:64;not null;uniqueIndex:uk_traefik_modules_code" json:"code"`                      // 全局唯一编码
	Name          string           `gorm:"size:128;not null;index:idx_traefik_modules_name" json:"name"`
	OwnerID       *uint            `gorm:"index:idx_traefik_modules_owner" json:"owner_id"`
	Owner         *User            `gorm:"foreignKey:OwnerID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL" json:"owner,omitempty"`
	Description   string           `gorm:"type:text" json:"description"`
	CreatedBy     uint             `gorm:"not null;default:0" json:"created_by"`
	UpdatedBy     uint             `gorm:"not null;default:0" json:"updated_by"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

func (Module) TableName() string { return config.TablePrefix + "modules" }

// 19. ServiceContract 多实现服务契约定义表 (traefik_service_contracts, ADR-16, M20)
const (
	ContractProvideModeMultiImpl = "MULTI_IMPL"
)

type ServiceContract struct {
	ID              uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	ServiceName     string    `gorm:"size:128;not null;uniqueIndex:uk_traefik_svc_contract_name" json:"service_name"` // 格式 {package}.{serviceName}
	DefinerModuleID uint64    `gorm:"not null;index:idx_traefik_svc_contract_definer" json:"definer_module_id"`
	DefinerModule   *Module   `gorm:"foreignKey:DefinerModuleID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"definer_module,omitempty"`
	ProvideMode     string    `gorm:"size:16;not null;default:'MULTI_IMPL'" json:"provide_mode"`
	ImplKeyLabel    string    `gorm:"size:64;not null;default:''" json:"impl_key_label"`
	Description     string    `gorm:"type:text" json:"description"`
	CreatedBy       uint      `gorm:"not null;default:0" json:"created_by"`
	UpdatedBy       uint      `gorm:"not null;default:0" json:"updated_by"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (ServiceContract) TableName() string { return config.TablePrefix + "service_contracts" }

// 6. ModuleService 模块级对外供给服务契约表 (traefik_module_services)
type DownstreamConsumer struct {
	ModuleID      uint64 `json:"module_id"`
	ModuleName    string `json:"module_name"`
	ModuleCode    string `json:"module_code"`
	SubsystemName string `json:"subsystem_name,omitempty"`
}

type ModuleService struct {
	ID                  uint64               `gorm:"primaryKey;autoIncrement" json:"id"`
	ModuleID            uint64               `gorm:"not null;index:idx_traefik_mod_svc_mod" json:"module_id"`
	Module              *Module              `gorm:"foreignKey:ModuleID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"module,omitempty"`
	ContractID          *uint64              `gorm:"index:idx_traefik_mod_svc_contract" json:"contract_id,omitempty"` // 指向多实现服务契约 (ADR-16)，NULL 表示独占服务 (EXCLUSIVE)
	Contract            *ServiceContract     `gorm:"foreignKey:ContractID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"contract,omitempty"`
	ServiceName         string               `gorm:"size:128;not null" json:"service_name"`                     // 运行期实际发布的 CROS 服务名（独占全局唯一；多实现可共享契约名或自定义唯一名如 DH_SyncService）
	ImplKey             string               `gorm:"size:64;not null;default:''" json:"impl_key"`               // 多实现时必填身份标识，独占服务恒为空串
	IsCustomServiceName bool                 `gorm:"not null;default:false" json:"is_custom_service_name"`      // true 表示该多实现服务使用了不同于契约 .pb 名的自定义运行期服务名
	ContractServiceName string               `gorm:"-" json:"contract_service_name,omitempty"`                  // 多实现时填充关联契约名 (.pb 定义服务名)
	DiffIgnorePaths     datatypes.JSON       `gorm:"type:jsonb;not null;default:'[]'" json:"diff_ignore_paths"` // 服务级默认回归 Diff 忽略字段路径 []string (F-14)
	Description         string               `gorm:"type:text" json:"description"`
	DownstreamCount     int                  `gorm:"-" json:"downstream_count"`               // 被下游模块依赖数量
	DownstreamModules   []string             `gorm:"-" json:"downstream_modules,omitempty"`   // 被下游模块名称清单
	DownstreamConsumers []DownstreamConsumer `gorm:"-" json:"downstream_consumers,omitempty"` // 被下游模块结构化清单
	CreatedBy           uint                 `gorm:"not null;default:0" json:"created_by"`
	CreatedAt           time.Time            `json:"created_at"`
	UpdatedAt           time.Time            `json:"updated_at"`
}

func (ModuleService) TableName() string { return config.TablePrefix + "module_services" }

// 7. ModuleDependency 模块间服务依赖关系表 (traefik_module_dependencies)
type ModuleDependency struct {
	ID               uint64           `gorm:"primaryKey;autoIncrement" json:"id"`
	ConsumerModuleID uint64           `gorm:"not null;uniqueIndex:uk_traefik_dep_tuple,priority:1;index:idx_traefik_dep_consumer" json:"consumer_module_id"`
	ConsumerModule   *Module          `gorm:"foreignKey:ConsumerModuleID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"consumer_module,omitempty"`
	IsExternal       bool             `gorm:"not null;default:false;index:idx_traefik_dep_external" json:"is_external"`                              // 是否依赖外部第三方/未登记系统
	ExternalSystem   string           `gorm:"size:128;not null;default:''" json:"external_system"`                                                   // 外部系统名称 (如 "WeChatPay")
	ExternalService  string           `gorm:"size:128;not null;default:''" json:"external_service"`                                                  // 外部服务名称 (如 "UnifiedOrderService")
	ServiceID        *uint64          `gorm:"uniqueIndex:uk_traefik_dep_tuple,priority:2;index:idx_traefik_dep_service" json:"service_id,omitempty"` // 内部具体实现依赖
	Service          *ModuleService   `gorm:"foreignKey:ServiceID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"service,omitempty"`
	ContractID       *uint64          `gorm:"index:idx_traefik_dep_contract" json:"contract_id,omitempty"` // 分发依赖契约 ID (DISPATCH, ADR-16)
	Contract         *ServiceContract `gorm:"foreignKey:ContractID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"contract,omitempty"`
	ProviderModuleID *uint64          `gorm:"index:idx_traefik_dep_provider" json:"provider_module_id,omitempty"` // 内部依赖冗余提供方 FM
	ProviderModule   *Module          `gorm:"foreignKey:ProviderModuleID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL" json:"provider_module,omitempty"`
	Description      string           `gorm:"type:text" json:"description"` // 调用与依赖业务场景说明
	CreatedBy        uint             `gorm:"not null;default:0" json:"created_by"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
}

func (ModuleDependency) TableName() string { return config.TablePrefix + "module_dependencies" }

// 18. TopologyLayout 拓扑画布双层布局表 (traefik_topology_layouts)，位置层 + 视角层分离 (ADR-15 v1.1.0 §4.2)：
//   - SHARED（团队基准布局 / 位置层）：每个 view 至多 1 条 (uk_traefik_topo_layout_shared)，由 traefik_admin 发布；
//     owner_id 为 NULL、focus_scope 恒为 'GLOBAL'；node_positions 为全视图完整相对父坐标；revision 乐观锁防并发覆盖；
//   - PRIVATE（个人视图书签 / 视角层）：单人上限 20 条，保存聚焦域、过滤条件、展开态与视口，
//     node_positions 仅存相对团队基准的坐标差量覆写（团队基准更新后未覆写节点自动继承）；
//   - 坐标均为相对 parentId 父容器的局部坐标，与聚焦域无关；聚焦根与外部邻居卡坐标不落库。
//   - 约束：chk_traefik_topo_layout_scope / chk_traefik_topo_layout_view / chk_traefik_topo_layout_owner。
type TopologyLayout struct {
	ID               uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	Scope            string         `gorm:"size:16;not null;default:'PRIVATE';index:idx_traefik_topo_layout_owner_view,priority:3" json:"scope"` // SHARED | PRIVATE
	View             string         `gorm:"size:16;not null;index:idx_traefik_topo_layout_owner_view,priority:2" json:"view"`                    // LOGICAL | PHYSICAL
	OwnerID          *uint          `gorm:"index:idx_traefik_topo_layout_owner_view,priority:1" json:"owner_id"`                                 // PRIVATE 归属人 ID；SHARED 为 NULL
	Owner            *User          `gorm:"foreignKey:OwnerID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"owner,omitempty"`
	Name             string         `gorm:"size:128;not null" json:"name"` // 书签名称（SHARED 固定为 "团队基准布局"）
	Description      string         `gorm:"size:512;not null;default:''" json:"description"`
	FocusScope       string         `gorm:"size:64;not null;default:'GLOBAL'" json:"focus_scope"` // 书签聚焦根容器 ID（"GLOBAL" | "sub_1" | "grp_12" | "pgrp_3" | "proc_7" ...）；SHARED 恒为 "GLOBAL"
	IsDefault        bool           `gorm:"not null;default:false" json:"is_default"`             // PRIVATE：是否为该 (view, focus_scope) 默认书签；SHARED 恒为 false
	SchemaVersion    int            `gorm:"not null;default:1" json:"schema_version"`             // JSON 载荷结构版本，前端按版本迁移或降级
	StructureVersion string         `gorm:"size:64;not null;default:''" json:"structure_version"` // 保存时刻 TopologyGraphDTO.structure_version（树结构指纹），供增量 Diff 合并
	Revision         int            `gorm:"not null;default:1" json:"revision"`                   // 乐观锁版本号，每次更新 +1
	NodePositions    datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"node_positions"`
	ExpandState      datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"expand_state"`
	FilterState      datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"filter_state"`
	Viewport         datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"viewport"`
	CreatedBy        uint           `gorm:"not null;default:0" json:"created_by"`
	UpdatedBy        uint           `gorm:"not null;default:0" json:"updated_by"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

// SavedNodePosition 单节点相对父容器坐标（x/y 必须为有限实数，键为节点 ID 且长度 <= 128，条目数 <= 5000）
type SavedNodePosition struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Pinned bool    `json:"pinned,omitempty"`
}

// SavedExpandState 展开态快照（level 为相对聚焦根的深度档位；overrides 条目数 <= 5000）
type SavedExpandState struct {
	Level     int             `json:"level"`
	Overrides map[string]bool `json:"overrides,omitempty"`
}

// SavedFilterState 过滤条件快照（与前端 TopologyFilterState 字段一一对应）
type SavedFilterState struct {
	SubsystemIDs []string `json:"subsystem_ids,omitempty"` // <= 100 项
	OnlyMyScope  bool     `json:"only_my_scope,omitempty"`
	Status       string   `json:"status,omitempty"` // ALL | ABNORMAL | VIOLATION_ONLY
	EgoNodeID    string   `json:"ego_node_id,omitempty"`
	EgoHops      int      `json:"ego_hops,omitempty"` // 1 | 2
	HideIsolated bool     `json:"hide_isolated,omitempty"`
	HubFold      bool     `json:"hub_fold,omitempty"`
}

// SavedViewport 镜头视口快照
type SavedViewport struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Zoom float64 `json:"zoom"`
}

func (TopologyLayout) TableName() string { return config.TablePrefix + "topology_layouts" }
