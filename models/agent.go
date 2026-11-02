package models

import (
	"time"

	"code-traefik/config"
	"gorm.io/datatypes"
)

// 统一 7 态探针就绪状态常量
const (
	ProbeStatusReady         = "READY"          // 🟢 已就绪：进程存活且建立连接数 == 0
	ProbeStatusArmed         = "READY"          // 兼容别名
	ProbeStatusNeedReconnect = "NEED_RECONNECT" // 🟡 待重连：进程存活但存量直连 Socket > 0
	ProbeStatusWaitingProc   = "WAITING_PROC"   // 🔵 待启动：目标进程尚未启动
	ProbeStatusMonitoring    = "MONITORING"     // 🟣 运行中：录制或重放进行中
	ProbeStatusDisarmed      = "DISARMED"       // ⚪ 已暂停：探针未启用
	ProbeStatusOffline       = "OFFLINE"        // ⚫ 离线：Agent 未心跳
	ProbeStatusCapDenied     = "CAP_DENIED"     // 🔴 权限不足：缺少 CAP_BPF/CAP_NET_ADMIN
	ProbeStatusError         = "ERROR"          // 🔴 异常：探针挂载或同步报错
	ProbeStatusErrorFailed   = "ERROR"          // 兼容别名

	ProbeDesiredStateArmed    = "ARMED"
	ProbeDesiredStateDisarmed = "DISARMED"
)

// 8. Agent 数据面宿主机守护进程纳管与动态排他租约表 (traefik_agents)
// 注：1 台 Agent 代表 1 套独立运行环境（支持多套 SHARED 公共测试环境与多套 PRIVATE 个人环境）
type Agent struct {
	ID                  uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Name                string     `gorm:"size:128;not null;uniqueIndex:uk_traefik_agents_name" json:"name"`
	Endpoint            string     `gorm:"size:128;not null;uniqueIndex:uk_traefik_agents_endpoint" json:"endpoint"` // 唯一 IP:Port
	Scope               string     `gorm:"size:16;not null;default:'SHARED';index:idx_traefik_agents_scope_owner,priority:1;check:chk_traefik_agents_scope,scope IN ('SHARED','PRIVATE')" json:"scope"`
	OwnerID             *uint      `gorm:"index:idx_traefik_agents_scope_owner,priority:2" json:"owner_id,omitempty"` // SHARED 为 nil；PRIVATE 必填创建人
	Owner               *User      `gorm:"foreignKey:OwnerID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"owner,omitempty"`
	FallbackAgentID     *uint64    `gorm:"index:idx_traefik_agents_fallback" json:"fallback_agent_id,omitempty"` // 可选关联的公共基准环境 Agent (仅允许指向 scope='SHARED' 且 != ID)
	FallbackAgent       *Agent     `gorm:"foreignKey:FallbackAgentID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL" json:"fallback_agent,omitempty"`
	ClaimCode           string     `gorm:"size:128;not null;default:''" json:"-"`     // 个人 Agent 认领鉴权码 (凭证绝密存储，禁止对外序列化)
	MountsRevision      uint64     `gorm:"not null;default:0" json:"mounts_revision"` // 挂载配置单调递增版本号 (防并发覆盖与 Agent 乱序下发)
	Status              string     `gorm:"size:32;not null;default:'OFFLINE';index:idx_traefik_agents_status" json:"status"`
	BootID              string     `gorm:"size:64;not null;default:''" json:"boot_id"`               // 感知 Agent 重启以触发预挂自动补齐
	ExpectedArmedDigest string     `gorm:"size:64;not null;default:''" json:"expected_armed_digest"` // 主控编译的期望 ARMED 规格 SHA-256 摘要
	ArmedDigest         string     `gorm:"size:64;not null;default:''" json:"armed_digest"`          // Agent 实际回报的已生效 ARMED 规格 SHA-256 摘要
	AgentVersion        string     `gorm:"size:64;not null;default:''" json:"agent_version"`
	ProtocolVersion     string     `gorm:"size:32;not null;default:''" json:"protocol_version"` // 如 "1.0"
	KernelVersion       string     `gorm:"size:128;not null;default:''" json:"kernel_version"`
	HasEbpfCap          bool       `gorm:"not null;default:false" json:"has_ebpf_cap"` // CAP_BPF + CAP_NET_ADMIN 具备状态
	CapDetail           string     `gorm:"size:512;not null;default:''" json:"cap_detail"`
	LastHeartbeatAt     *time.Time `gorm:"index:idx_traefik_agents_hb" json:"last_heartbeat_at"`

	// 动态排他租约列 (Agent Mutex Lease：仅活跃 RECORDING / REPLAY 占用；ARMED 预挂待命不占租约)
	LeaseType       string     `gorm:"size:16;not null;default:'';index:idx_traefik_agents_lease;check:chk_traefik_agents_lease,lease_type IN ('','RECORDING','REPLAY')" json:"lease_type"`
	LeaseRefID      *uint64    `json:"lease_ref_id,omitempty"`
	LeaseHolderID   *uint      `gorm:"index:idx_traefik_agents_lease_holder" json:"lease_holder_id,omitempty"`
	LeaseHolder     *User      `gorm:"foreignKey:LeaseHolderID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL" json:"lease_holder,omitempty"`
	LeaseAcquiredAt *time.Time `json:"lease_acquired_at,omitempty"`
	LeaseExpiresAt  *time.Time `gorm:"index:idx_traefik_agents_lease_exp" json:"lease_expires_at,omitempty"`

	Description    string    `gorm:"type:text" json:"description"`
	Processes      []Process `gorm:"foreignKey:AgentID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"processes,omitempty"`
	ProcessesCount int       `gorm:"-" json:"processes_count"`
	CreatedBy      uint      `gorm:"not null;default:0" json:"created_by"`
	UpdatedBy      uint      `gorm:"not null;default:0" json:"updated_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (Agent) TableName() string { return config.TablePrefix + "agents" }

// 9. Process 宿主机 Agent 架构进程 1:1 挂载实例、本地覆盖与预挂就绪表 (traefik_processes)
// 注：保持 process_id 主键与历史录制批次、流量分区表及重放任务的完全外键兼容
type Process struct {
	ID                  uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	AgentID             uint64         `gorm:"not null;uniqueIndex:uk_traefik_proc_agent_arch,priority:1;uniqueIndex:uk_traefik_proc_agent_port,priority:1,where:effective_listen_port > 0;index:idx_traefik_proc_agent" json:"agent_id"`
	Agent               *Agent         `gorm:"foreignKey:AgentID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"agent,omitempty"`
	ArchProcessID       uint64         `gorm:"not null;uniqueIndex:uk_traefik_proc_agent_arch,priority:2;index:idx_traefik_proc_arch" json:"arch_process_id"` // 单机严格 1:1 唯一约束
	ArchProcess         *ArchProcess   `gorm:"foreignKey:ArchProcessID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"arch_process,omitempty"`
	OverrideListenPort  *int           `gorm:"check:chk_traefik_proc_override_port,override_listen_port IS NULL OR (override_listen_port >= 0 AND override_listen_port <= 65535)" json:"override_listen_port"`                                                        // nil 表示继承 ArchProcess.DefaultListenPort
	EffectiveListenPort int            `gorm:"not null;default:0;uniqueIndex:uk_traefik_proc_agent_port,priority:2,where:effective_listen_port > 0;check:chk_traefik_proc_eff_port,effective_listen_port >= 0 AND effective_listen_port <= 65535" json:"listen_port"` // 物化有效端口，保障同机非零端口互斥 (A-02)
	OverrideCmdPattern  string         `gorm:"size:512;not null;default:''" json:"override_cmd_pattern"`                                                                                                                                                              // 为空表示继承 ArchProcess.DefaultCmdPattern
	AdvertiseIP         string         `gorm:"size:64;not null;default:''" json:"advertise_ip"`                                                                                                                                                                       // 可选本地业务通信 IP，为空取所属 Agent IP
	ExtraDownstreams    datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"extra_downstreams"`                                                                                                                                                             // 本地附加补充下游 ["IP:Port"]
	Description         string         `gorm:"type:text" json:"description"`

	// eBPF 默认自动预挂期望状态与最近一次宿主机 /proc 就绪诊断快照
	ProbeDesiredState string     `gorm:"size:16;not null;default:'ARMED';check:chk_traefik_proc_desired,probe_desired_state IN ('ARMED','DISARMED')" json:"probe_desired_state"`
	ProbeStatus       string     `gorm:"size:32;not null;default:'DISARMED';index:idx_traefik_proc_probe_status" json:"probe_status"`
	ResolvedPID       uint32     `gorm:"not null;default:0" json:"resolved_pid"`
	EstablishedConns  int        `gorm:"not null;default:0" json:"established_conns"`
	NeedReconnect     bool       `gorm:"not null;default:false" json:"need_reconnect"`
	DiagMessage       string     `gorm:"size:512;not null;default:''" json:"diag_message"`
	LastDiagAt        *time.Time `json:"last_diag_at,omitempty"`

	// DTO 序列化便捷透出字段（由预加载的 ArchProcess 自动计算填充，不落冗余列）
	GroupID     uint64 `gorm:"-" json:"group_id"`
	GroupName   string `gorm:"-" json:"group_name"`
	Code        string `gorm:"-" json:"code"`
	Name        string `gorm:"-" json:"name"`
	CmdPattern  string `gorm:"-" json:"cmd_pattern"`
	HasOverride bool   `gorm:"-" json:"has_override"`

	CreatedBy uint      `gorm:"not null;default:0" json:"created_by"`
	UpdatedBy uint      `gorm:"not null;default:0" json:"updated_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Process) TableName() string { return config.TablePrefix + "processes" }

// FillFromArchProcess 填充 Process 的 DTO 便捷展示字段
// FillFromArchProcess 填充 Process 的 DTO 便捷展示字段
func (p *Process) FillFromArchProcess() {
	if p.ArchProcess != nil {
		p.GroupID = p.ArchProcess.GroupID
		if p.ArchProcess.Group != nil {
			p.GroupName = p.ArchProcess.Group.Name
		}
		p.Code = p.ArchProcess.Code
		p.Name = p.ArchProcess.Name
		if p.OverrideCmdPattern != "" {
			p.CmdPattern = p.OverrideCmdPattern
		} else {
			p.CmdPattern = p.ArchProcess.DefaultCmdPattern
		}
	}
	p.HasOverride = p.OverrideListenPort != nil || p.OverrideCmdPattern != "" || p.AdvertiseIP != "" || len(p.ExtraDownstreams) > 2
}

// EffectiveCmdPattern 返回有效命令行匹配特征
func (p *Process) EffectiveCmdPattern() string {
	if p.OverrideCmdPattern != "" {
		return p.OverrideCmdPattern
	}
	if p.ArchProcess != nil {
		return p.ArchProcess.DefaultCmdPattern
	}
	return p.CmdPattern
}
