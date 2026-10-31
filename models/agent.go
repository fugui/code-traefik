package models

import (
	"time"

	"code-traefik/config"
	"gorm.io/datatypes"
)

// 5. Agent 数据面宿主机守护进程纳管与动态排他租约表 (traefik_agents)
type Agent struct {
	ID              uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Name            string     `gorm:"size:128;not null;uniqueIndex:uk_traefik_agents_name" json:"name"`
	Endpoint        string     `gorm:"size:128;not null;uniqueIndex:uk_traefik_agents_endpoint" json:"endpoint"` // 唯一 IP:Port
	Scope           string     `gorm:"size:16;not null;default:'SHARED';index:idx_traefik_agents_scope_owner,priority:1;check:chk_traefik_agents_scope,scope IN ('SHARED','PRIVATE')" json:"scope"`
	OwnerID         *uint      `gorm:"index:idx_traefik_agents_scope_owner,priority:2" json:"owner_id,omitempty"` // SHARED 为 nil；PRIVATE 必填创建人
	Owner           *User      `gorm:"foreignKey:OwnerID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"owner,omitempty"`
	ClaimCode       string     `gorm:"size:128;not null;default:''" json:"-"` // 个人 Agent 认领鉴权码 (凭证绝密存储，禁止对外序列化)
	Status          string     `gorm:"size:32;not null;default:'OFFLINE';index:idx_traefik_agents_status" json:"status"`
	BootID          string     `gorm:"size:64;not null;default:''" json:"boot_id,omitempty"`
	AgentVersion    string     `gorm:"size:64;not null;default:''" json:"agent_version"`
	ProtocolVersion string     `gorm:"size:32;not null;default:''" json:"protocol_version"` // 如 "1.0"
	KernelVersion   string     `gorm:"size:128;not null;default:''" json:"kernel_version"`
	HasEbpfCap      bool       `gorm:"not null;default:false" json:"has_ebpf_cap"` // CAP_BPF + CAP_NET_ADMIN 具备状态
	CapDetail       string     `gorm:"size:512;not null;default:''" json:"cap_detail"`
	LastHeartbeatAt *time.Time `gorm:"index:idx_traefik_agents_hb" json:"last_heartbeat_at"`

	// 动态排他租约列 (Agent Mutex Lease)
	LeaseType       string     `gorm:"size:16;not null;default:'';index:idx_traefik_agents_lease;check:chk_traefik_agents_lease,lease_type IN ('','RECORDING','REPLAY')" json:"lease_type"`
	LeaseRefID      *uint64    `json:"lease_ref_id,omitempty"`
	LeaseHolderID   *uint      `gorm:"index:idx_traefik_agents_lease_holder" json:"lease_holder_id,omitempty"`
	LeaseHolder     *User      `gorm:"foreignKey:LeaseHolderID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL" json:"lease_holder,omitempty"`
	LeaseAcquiredAt *time.Time `json:"lease_acquired_at,omitempty"`
	LeaseExpiresAt  *time.Time `gorm:"index:idx_traefik_agents_lease_exp" json:"lease_expires_at,omitempty"`

	Description    string    `gorm:"type:text" json:"description"`
	ProcessesCount int       `gorm:"-" json:"processes_count"`
	CreatedBy      uint      `gorm:"not null;default:0" json:"created_by"`
	UpdatedBy      uint      `gorm:"not null;default:0" json:"updated_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (Agent) TableName() string { return config.TablePrefix + "agents" }

// 6. Process 业务宿主机目标进程部署映射表 (traefik_processes)
type Process struct {
	ID               uint64          `gorm:"primaryKey;autoIncrement" json:"id"`
	AgentID          uint64          `gorm:"not null;uniqueIndex:uk_traefik_proc_agent_name,priority:1;index:idx_traefik_proc_agent" json:"agent_id"`
	Agent            *Agent          `gorm:"foreignKey:AgentID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"agent,omitempty"`
	GroupName        string          `gorm:"size:64;not null;default:'默认进程组';index:idx_traefik_proc_group" json:"group_name"`
	Name             string          `gorm:"size:128;not null;uniqueIndex:uk_traefik_proc_agent_name,priority:2" json:"name"`
	ListenPort       int             `gorm:"not null;default:0;check:chk_traefik_proc_port,listen_port >= 0 AND listen_port <= 65535" json:"listen_port"` // 监听端口 (0 表示纯客户端/无监听端口)
	AdvertiseIP      string          `gorm:"size:64;not null;default:''" json:"advertise_ip"`
	CmdPattern       string          `gorm:"size:512;not null" json:"cmd_pattern"`
	ProcessModules   []ProcessModule `gorm:"foreignKey:ProcessID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"process_modules,omitempty"`
	ModuleIDs        []uint64        `gorm:"-" json:"module_ids,omitempty"`
	ExtraDownstreams datatypes.JSON  `gorm:"type:jsonb;not null;default:'[]'" json:"extra_downstreams"`
	Description      string          `gorm:"type:text" json:"description"`
	CreatedBy        uint            `gorm:"not null;default:0" json:"created_by"`
	UpdatedBy        uint            `gorm:"not null;default:0" json:"updated_by"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

func (Process) TableName() string { return config.TablePrefix + "processes" }

// 7. ProcessModule 业务进程与功能模块同构承载关联表 (traefik_process_modules)
type ProcessModule struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	ProcessID uint64    `gorm:"not null;uniqueIndex:uk_traefik_proc_mod,priority:1;index:idx_traefik_proc_mod_pid" json:"process_id"`
	Process   *Process  `gorm:"foreignKey:ProcessID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
	ModuleID  uint64    `gorm:"not null;uniqueIndex:uk_traefik_proc_mod,priority:2;index:idx_traefik_proc_mod_mid" json:"module_id"`
	Module    *Module   `gorm:"foreignKey:ModuleID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"module,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (ProcessModule) TableName() string { return config.TablePrefix + "process_modules" }
