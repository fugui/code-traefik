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

// 3. Module 最小接口契约功能模块表 (traefik_modules)
type Module struct {
	ID          uint64           `gorm:"primaryKey;autoIncrement" json:"id"`
	SubsystemID uint64           `gorm:"not null;index:idx_traefik_modules_subsystem" json:"subsystem_id"` // 冗余所属子系统加速检索
	Subsystem   *Subsystem       `gorm:"foreignKey:SubsystemID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"subsystem,omitempty"`
	GroupID     uint64           `gorm:"not null;index:idx_traefik_modules_group" json:"group_id"` // 可挂载于任意层级功能组
	Group       *FunctionalGroup `gorm:"foreignKey:GroupID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"group,omitempty"`
	ProcessID   *uint64          `gorm:"index:idx_traefik_modules_process" json:"process_id"` // 指向公共 SHARED 环境绑定的唯一基线进程
	Process     *Process         `gorm:"foreignKey:ProcessID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL" json:"process,omitempty"`
	Code        string           `gorm:"size:64;not null;uniqueIndex:uk_traefik_modules_code" json:"code"` // 全局唯一编码
	Name        string           `gorm:"size:128;not null;index:idx_traefik_modules_name" json:"name"`
	OwnerID     *uint            `gorm:"index:idx_traefik_modules_owner" json:"owner_id"`
	Owner       *User            `gorm:"foreignKey:OwnerID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL" json:"owner,omitempty"`
	Description string           `gorm:"type:text" json:"description"`
	CreatedBy   uint             `gorm:"not null;default:0" json:"created_by"`
	UpdatedBy   uint             `gorm:"not null;default:0" json:"updated_by"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

func (Module) TableName() string { return config.TablePrefix + "modules" }

const (
	InterfaceRelationProvide = "PROVIDE" // 模块对外实现的供给接口 (全局唯一 A-05)
	InterfaceRelationDepend  = "DEPEND"  // 模块调用下游的依赖接口
)

// 4. ModuleInterface 模块级供给与依赖接口契约表 (traefik_module_interfaces)
type ModuleInterface struct {
	ID              uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	ModuleID        uint64         `gorm:"not null;uniqueIndex:uk_traefik_mod_iface_tuple,priority:1;index:idx_traefik_mod_iface_mod" json:"module_id"`
	Module          *Module        `gorm:"foreignKey:ModuleID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"module,omitempty"`
	RelationType    string         `gorm:"size:16;not null;uniqueIndex:uk_traefik_mod_iface_tuple,priority:2;check:chk_traefik_iface_rel,relation_type IN ('PROVIDE','DEPEND')" json:"relation_type"`
	ServiceName     string         `gorm:"size:128;not null;uniqueIndex:uk_traefik_mod_iface_tuple,priority:3;index:idx_traefik_iface_svc_op,priority:1" json:"service_name"` // 格式 {package}-{serviceName}
	Operation       string         `gorm:"size:128;not null;uniqueIndex:uk_traefik_mod_iface_tuple,priority:4;index:idx_traefik_iface_svc_op,priority:2" json:"operation"`    // 与 Method 名直接相等
	DiffIgnorePaths datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"diff_ignore_paths"`                                                                         // 接口级默认回归 Diff 忽略字段路径 []string (F-14)
	Description     string         `gorm:"size:512" json:"description"`
	CreatedBy       uint           `gorm:"not null;default:0" json:"created_by"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

func (ModuleInterface) TableName() string { return config.TablePrefix + "module_interfaces" }
