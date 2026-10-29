package agentproto

import (
	"errors"
	"time"
)

const (
	// ProtocolVersion 主控与 Agent 协议版本号
	ProtocolVersion = "1.0"

	// EnvelopeMagic WebSocket 二进制流 2 字节魔术字 (0x4354 = "CT")
	EnvelopeMagic = uint16(0x4354)

	// EnvelopeProtoVer 当前信封版本号 (1)
	EnvelopeProtoVer = uint8(1)

	// EnvelopeHeaderLen 56 字节定长网络大端信封头
	EnvelopeHeaderLen = 56

	// Flags 位掩码
	FlagIsTruncated = uint8(0x01) // bit0: 超过单帧阈值发生物理截断
	FlagRefKind     = uint8(0x02) // bit1: 关联资产类型 (0 = RECORDING_SESSION, 1 = REPLAY_TASK)
	FlagHasGap      = uint8(0x04) // bit2: 内核丢包导致字节断流

	// Direction 流量流向
	DirectionIngress = uint8(1)
	DirectionEgress  = uint8(2)

	// EventType WebSocket 事件类型
	EventTrafficFrame     = uint8(0x01) // 采集到的单条完整或截断 CROS RPC 原始帧
	EventProcessLifecycle = uint8(0x02) // 目标进程生命周期事件
	EventSessionStats     = uint8(0x03) // 周期会话统计指标
	EventReplayItemDone   = uint8(0x04) // 单条重放请求完成
	EventReplayCompleted  = uint8(0x05) // 重放任务汇总完成
	EventConnClosed       = uint8(0x06) // 监控连接关闭
	EventSessionDrained   = uint8(0x07) // 会话积压已完全排空终结信号

	// RefKind 资产类型
	RefKindRecordingSession = uint8(0)
	RefKindReplayTask       = uint8(1)
)

var (
	ErrInvalidEnvelopeMagic   = errors.New("agentproto: invalid envelope magic")
	ErrEnvelopeBufferTooShort = errors.New("agentproto: envelope buffer too short")
	ErrPayloadLengthMismatch  = errors.New("agentproto: payload length mismatch")
)

// HandshakeResp 握手与心跳双向对账数据载荷
type HandshakeResp struct {
	AgentVersion    string    `json:"agent_version"`     // Agent 构建版本号
	ProtocolVersion string    `json:"protocol_version"`  // 主协议版本，如 "1.0"
	BootID          string    `json:"boot_id"`           // Agent 实例重启 UUID
	StartedAt       time.Time `json:"started_at"`        // 启动时刻
	KernelVersion   string    `json:"kernel_version"`    // 宿主机内核版本
	HasEbpfCap      bool      `json:"has_ebpf_cap"`      // 是否具备完整 eBPF 运行权限
	CapDetail       string    `json:"cap_detail"`        // 权限详情说明
	ActiveLeaseType string    `json:"active_lease_type"` // 当前内存持有的租约类型: "" | "RECORDING" | "REPLAY"
	ActiveRefID     uint64    `json:"active_ref_id"`     // 当前持有的 SessionID 或 TaskID
	UptimeSec       uint64    `json:"uptime_sec"`        // 运行秒数
}

// DownstreamEndpoint 描述目标进程允许访问并需被透明拦截的下游 CROS RPC 地址
type DownstreamEndpoint struct {
	IP   string `json:"ip"`   // IPv4 点分十进制地址，如 "192.168.56.19" 或 "127.0.0.1"
	Port uint16 `json:"port"` // 下游监听端口，1..65535
}

// ProcessMonitorSpec 单进程监控元信息（零 .pb Schema 依赖）
type ProcessMonitorSpec struct {
	ProcessID   uint64               `json:"process_id"`  // 主控 traefik_processes.id
	ListenPort  uint16               `json:"listen_port"` // 唯一监听端口 (A-02)
	CmdPattern  string               `json:"cmd_pattern"` // /proc/<pid>/cmdline 匹配表达式
	Downstreams []DownstreamEndpoint `json:"downstreams"` // 下游端点清单
}

// ProcessDiagStatus 下发监控后的实时定位与存量长连接诊断结果
type ProcessDiagStatus struct {
	ProcessID        uint64 `json:"process_id"`
	Status           string `json:"status"`            // "MONITORING" | "NOT_FOUND" | "CAP_DENIED" | "ERROR"
	ResolvedPID      uint32 `json:"resolved_pid"`      // 实时定位到的 OS PID
	EstablishedConns int    `json:"established_conns"` // 指向已登记下游的存量 TCP_ESTABLISHED 连接数
	NeedReconnect    bool   `json:"need_reconnect"`    // EstablishedConns > 0 时为 true
	Message          string `json:"message"`
}

// SyncMonitorReq 下发声明式监控进程规格
type SyncMonitorReq struct {
	SessionID      uint64               `json:"session_id"`
	MaxFrameBytes  int                  `json:"max_frame_bytes"`
	MaxBufferBytes int                  `json:"max_buffer_bytes"`
	Processes      []ProcessMonitorSpec `json:"processes"`
}

// StopMonitorResp 停止会话累计统计
type StopMonitorResp struct {
	CapturedFrames    int64 `json:"captured_frames"`
	DroppedFrames     int64 `json:"dropped_frames"`
	BypassedSelfCount int64 `json:"bypassed_self_count"`
}

// ExecuteReplayReq 下发重放执行规格
type ExecuteReplayReq struct {
	TaskID          uint64             `json:"task_id"`
	ProcessSpec     ProcessMonitorSpec `json:"process_spec"`
	TimeoutMs       int                `json:"timeout_ms"`
	IngressRequests [][]byte           `json:"ingress_requests"` // 纯二进制流切片传输，零 .pb
	EgressMocks     [][]byte           `json:"egress_mocks"`
	MngMocks        [][]byte           `json:"mng_mocks"`
}

// ReplayTaskResultResp 重放任务汇总结果
type ReplayTaskResultResp struct {
	TaskID         uint64 `json:"task_id"`
	Status         string `json:"status"` // PASS | FAILED | MOCK_MISS | TIMEOUT | ERROR
	TotalRequests  int    `json:"total_requests"`
	PassedRequests int    `json:"passed_requests"`
	MockHitCount   int    `json:"mock_hit_count"`
	MockMissCount  int    `json:"mock_miss_count"`
	Details        string `json:"details"`
}
