package models

import (
	"time"

	"code-traefik/config"
	"gorm.io/datatypes"
)

// 12. ReplayTask 跨 Agent 同构离线自愈重放任务头表 (traefik_replay_tasks)
type ReplayTask struct {
	ID                uint64            `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskNo            string            `gorm:"size:64;not null;uniqueIndex:uk_traefik_replay_task_no" json:"task_no"`
	Name              string            `gorm:"size:128;not null" json:"name"`
	OwnerID           uint              `gorm:"not null;index:idx_traefik_replay_owner" json:"owner_id"`
	Owner             *User             `gorm:"foreignKey:OwnerID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"owner,omitempty"`
	SessionID         uint64            `gorm:"not null;index:idx_traefik_replay_session" json:"session_id"`
	Session           *RecordingSession `gorm:"foreignKey:SessionID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"session,omitempty"`
	SourceProcessID   uint64            `gorm:"not null;index:idx_traefik_replay_src_proc" json:"source_process_id"` // 录制源进程
	SourceProcess     *Process          `gorm:"foreignKey:SourceProcessID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"source_process,omitempty"`
	TargetProcessID   uint64            `gorm:"not null;index:idx_traefik_replay_tgt_proc" json:"target_process_id"` // 实际执行同构目标进程
	TargetProcess     *Process          `gorm:"foreignKey:TargetProcessID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"target_process,omitempty"`
	AgentID           uint64            `gorm:"not null;index:idx_traefik_replay_agent" json:"agent_id"` // TargetProcess 所属 Agent
	Agent             *Agent            `gorm:"foreignKey:AgentID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"agent,omitempty"`
	SelectedRecordIDs datatypes.JSON    `gorm:"type:jsonb;not null" json:"selected_record_ids"`             // []uint64
	ExtraIgnorePaths  datatypes.JSON    `gorm:"type:jsonb;not null;default:'[]'" json:"extra_ignore_paths"` // []string
	TimeoutMs         int               `gorm:"not null;default:30000" json:"timeout_ms"`
	LifecycleState    string            `gorm:"size:32;not null;default:'PENDING';index:idx_traefik_replay_state" json:"lifecycle_state"`
	ResultStatus      string            `gorm:"size:32;not null;default:'';index:idx_traefik_replay_result" json:"result_status"`
	TotalRequests     int               `gorm:"not null;default:0" json:"total_requests"`
	PassedRequests    int               `gorm:"not null;default:0" json:"passed_requests"`
	MockHitCount      int               `gorm:"not null;default:0" json:"mock_hit_count"`
	MockMissCount     int               `gorm:"not null;default:0" json:"mock_miss_count"`
	MockEvents        datatypes.JSON    `gorm:"type:jsonb;not null;default:'[]'" json:"mock_events"`
	ErrorMessage      string            `gorm:"type:text" json:"error_message"`
	StartedAt         *time.Time        `json:"started_at"`
	FinishedAt        *time.Time        `json:"finished_at"`
	CreatedBy         uint              `gorm:"not null;default:0;index:idx_traefik_replay_creator" json:"created_by"`
	CreatedAt         time.Time         `gorm:"index:idx_traefik_replay_created_at" json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

func (ReplayTask) TableName() string { return config.TablePrefix + "replay_tasks" }

// 13. ReplayItem 重放任务单请求独立执行结果与双层 Diff 明细表 (traefik_replay_items)
type ReplayItem struct {
	ID              uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID          uint64         `gorm:"not null;uniqueIndex:uk_traefik_replay_task_item,priority:1;index:idx_traefik_replay_item_task" json:"task_id"`
	Task            *ReplayTask    `gorm:"foreignKey:TaskID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
	RecordID        uint64         `gorm:"not null;uniqueIndex:uk_traefik_replay_task_item,priority:2;index:idx_traefik_replay_item_rec" json:"record_id"`
	SeqNo           int64          `gorm:"not null;index:idx_traefik_replay_item_seq" json:"seq_no"`
	ServiceName     string         `gorm:"size:128;not null" json:"service_name"`
	Operation       string         `gorm:"size:128;not null" json:"operation"`
	ResultStatus    string         `gorm:"size:32;not null;index:idx_traefik_replay_item_status" json:"result_status"` // PASS | FAILED | MOCK_MISS | TIMEOUT | ERROR
	LatencyUs       int64          `gorm:"not null;default:0" json:"latency_us"`
	ActualStatus    U64Numeric     `gorm:"type:numeric(20,0);not null;default:0" json:"actual_status,string"`
	BaselineStatus  U64Numeric     `gorm:"type:numeric(20,0);not null;default:0" json:"baseline_status,string"`
	FieldDiffs      datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"field_diffs"`
	ActualPayload   datatypes.JSON `gorm:"type:jsonb" json:"actual_payload,omitempty"`
	RawActualPacket []byte         `gorm:"type:bytea" json:"-"`
	MockTrace       datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"mock_trace"`
	ErrorMessage    string         `gorm:"type:text" json:"error_message"`
	CreatedAt       time.Time      `json:"created_at"`
}

func (ReplayItem) TableName() string { return config.TablePrefix + "replay_items" }
