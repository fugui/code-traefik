package models

import (
	"time"

	"code-traefik/config"
	"gorm.io/datatypes"
)

// 10. RecordingSession 任务级多进程协同录制批次表 (traefik_recording_sessions)
type RecordingSession struct {
	ID                    uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionNo             string         `gorm:"size:64;not null;uniqueIndex:uk_traefik_rec_session_no" json:"session_no"`
	Name                  string         `gorm:"size:128;not null;index:idx_traefik_rec_name" json:"name"`
	Scope                 string         `gorm:"size:16;not null;default:'PRIVATE';index:idx_traefik_rec_scope_owner,priority:1;check:chk_traefik_rec_scope,scope IN ('SHARED','PRIVATE')" json:"scope"`
	OwnerID               uint           `gorm:"not null;index:idx_traefik_rec_scope_owner,priority:2" json:"owner_id"`
	Owner                 *User          `gorm:"foreignKey:OwnerID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"owner,omitempty"`
	Status                string         `gorm:"size:32;not null;default:'PENDING';index:idx_traefik_rec_status" json:"status"`
	LeasedAgentIDs        datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"leased_agent_ids"`
	TargetProcessIDs      datatypes.JSON `gorm:"type:jsonb;not null" json:"target_process_ids"`
	ProcessDiagnostics    datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"process_diagnostics"`
	MaxDurationSec        int            `gorm:"not null;default:1800" json:"max_duration_sec"`
	MaxFrames             int            `gorm:"not null;default:100000" json:"max_frames"`
	MaxFrameBytes         int            `gorm:"not null;default:4194304" json:"max_frame_bytes"`
	CapturedFrames        int64          `gorm:"not null;default:0" json:"captured_frames"`
	DroppedFrames         int64          `gorm:"not null;default:0" json:"dropped_frames"`
	StartedAt             *time.Time     `json:"started_at"`
	StoppedAt             *time.Time     `gorm:"index:idx_traefik_rec_stopped_at" json:"stopped_at"`
	StopReason            string         `gorm:"size:255;not null;default:''" json:"stop_reason"`
	TopologyVerifySummary datatypes.JSON `gorm:"type:jsonb" json:"topology_verify_summary,omitempty"`
	Description           string         `gorm:"type:text" json:"description"`
	CreatedBy             uint           `gorm:"not null;default:0;index:idx_traefik_rec_creator" json:"created_by"`
	CreatedAt             time.Time      `gorm:"index:idx_traefik_rec_created_at" json:"created_at"`
	UpdatedAt             time.Time      `json:"updated_at"`
}

func (RecordingSession) TableName() string { return config.TablePrefix + "recording_sessions" }

// 11. TrafficRecord CROS RPC 无损二进制 + 结构化 JSONB 双模往返物理分区表 (traefik_traffic_records)
// 采用 Exchange-per-Row 单次往返一行模型，按 created_at 进行 PostgreSQL PARTITION BY RANGE (created_at)
type TrafficRecord struct {
	ID                    uint64         `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	CreatedAt             time.Time      `gorm:"column:created_at;primaryKey;not null" json:"created_at"` // 分区键
	SessionID             uint64         `gorm:"column:session_id;not null;index:idx_traefik_tr_session_seq,priority:1;index:idx_traefik_tr_session_svc,priority:1;index:idx_traefik_tr_session_trace,priority:1" json:"session_id"`
	SeqNo                 int64          `gorm:"column:seq_no;not null;index:idx_traefik_tr_session_seq,priority:2" json:"seq_no"`
	AgentID               uint64         `gorm:"column:agent_id;not null;index:idx_traefik_tr_agent" json:"agent_id"`
	ProcessID             uint64         `gorm:"column:process_id;not null;index:idx_traefik_tr_proc_dir,priority:1" json:"process_id"`
	CapturedPID           uint32         `gorm:"column:captured_pid;not null" json:"captured_pid"`
	ConnKey               string         `gorm:"column:conn_key;size:128;not null;index:idx_traefik_tr_conn_sno,priority:1" json:"conn_key"`
	Direction             string         `gorm:"column:direction;size:16;not null;index:idx_traefik_tr_proc_dir,priority:2" json:"direction"`
	ExchangeType          string         `gorm:"column:exchange_type;size:24;not null;default:'UNARY';index:idx_traefik_tr_exchange;check:chk_traefik_tr_ex_type,exchange_type IN ('UNARY','STREAM_RSP','ONEWAY','REQ_ONLY','RSP_ORPHAN','MNG')" json:"exchange_type"`
	SrcAddr               string         `gorm:"column:src_addr;size:64;not null" json:"src_addr"`
	DstAddr               string         `gorm:"column:dst_addr;size:64;not null" json:"dst_addr"`
	ReqCapturedAtNs       int64          `gorm:"column:req_captured_at_ns;not null;index:idx_traefik_tr_req_ns" json:"req_captured_at_ns"`
	RspCapturedAtNs       *int64         `gorm:"column:rsp_captured_at_ns" json:"rsp_captured_at_ns,omitempty"`
	LatencyUs             *int64         `gorm:"column:latency_us" json:"latency_us,omitempty"`
	TransportProto        uint8          `gorm:"column:transport_proto;not null;default:1" json:"transport_proto"`
	SNO                   U64Numeric     `gorm:"column:sno;type:numeric(20,0);not null;index:idx_traefik_tr_conn_sno,priority:2;index:idx_traefik_tr_sno" json:"sno,string"`
	ReqType               uint8          `gorm:"column:req_type;not null;default:0" json:"req_type"`
	RspSegCount           int            `gorm:"column:rsp_seg_count;not null;default:0" json:"rsp_seg_count"`
	IsPacked              uint8          `gorm:"column:is_packed;not null;default:1" json:"is_packed"`
	ServiceName           string         `gorm:"column:service_name;size:128;not null;default:'';index:idx_traefik_tr_session_svc,priority:2" json:"service_name"`
	Operation             string         `gorm:"column:operation;size:128;not null;default:'';index:idx_traefik_tr_session_svc,priority:3" json:"operation"`
	StatusCode            U64Numeric     `gorm:"column:status_code;type:numeric(20,0);not null;default:0" json:"status_code,string"`
	TraceID               string         `gorm:"column:trace_id;size:32;not null;default:'';index:idx_traefik_tr_session_trace,priority:2" json:"trace_id"`
	SpanID                string         `gorm:"column:span_id;size:32;not null;default:'';index:idx_traefik_tr_span" json:"span_id"`
	FatherSpanID          string         `gorm:"column:father_span_id;size:32;not null;default:'';index:idx_traefik_tr_father_span" json:"father_span_id"`
	SubsystemID           *uint64        `gorm:"column:subsystem_id;index:idx_traefik_tr_subsystem" json:"subsystem_id,omitempty"`
	SrcModuleID           *uint64        `gorm:"column:src_module_id;index:idx_traefik_tr_src_mod" json:"src_module_id,omitempty"`
	SrcAttributionMode    string         `gorm:"column:src_attribution_mode;size:32;not null;default:''" json:"src_attribution_mode"`
	SrcCandidateModuleIDs datatypes.JSON `gorm:"column:src_candidate_module_ids;type:jsonb" json:"src_candidate_module_ids,omitempty"`
	DstModuleID           *uint64        `gorm:"column:dst_module_id;index:idx_traefik_tr_dst_mod" json:"dst_module_id,omitempty"`
	HeadersJSON           datatypes.JSON `gorm:"column:headers_json;type:jsonb;not null;default:'{}'" json:"headers_json"`
	SchemaID              *uint64        `gorm:"column:schema_id;index:idx_traefik_tr_schema" json:"schema_id,omitempty"`
	SchemaVersion         *int           `gorm:"column:schema_version" json:"schema_version,omitempty"`
	DecodeStatus          string         `gorm:"column:decode_status;size:32;not null;default:''" json:"decode_status"`
	PayloadJSON           datatypes.JSON `gorm:"column:payload_json;type:jsonb;not null;default:'{}'" json:"payload_json"`
	ReqPayloadSHA256      string         `gorm:"column:req_payload_sha256;size:64;not null;default:'';index:idx_traefik_tr_req_sha" json:"req_payload_sha256"`
	RawReqPacket          []byte         `gorm:"column:raw_req_packet;type:bytea" json:"-"`
	RawRspPacket          []byte         `gorm:"column:raw_rsp_packet;type:bytea" json:"-"`
	IsTruncated           bool           `gorm:"column:is_truncated;not null;default:false" json:"is_truncated"`
	IsReplayable          bool           `gorm:"column:is_replayable;not null;index:idx_traefik_tr_replayable" json:"is_replayable"`
}

func (TrafficRecord) TableName() string { return config.TablePrefix + "traffic_records" }
