package callgraph

import (
	"time"
)

const (
	ConfidenceDOTRExact           = "DOTR_EXACT"
	ConfidenceFingerprintInferred = "FINGERPRINT_INFERRED"
	ConfidenceTemporalInferred    = "TEMPORAL_INFERRED"
	ConfidenceRoot                = "ROOT"

	SiblingRoot         = "ROOT"
	SiblingSequential   = "SEQUENTIAL"
	SiblingParallel     = "PARALLEL"
	SiblingCompensation = "COMPENSATION"

	MaxTreeDepth       = 16
	MaxChildrenPerNode = 200
	MaxLineageRecords  = 200
	MaxLineageDepth    = 6
	MaxScalarLeaves    = 500
)

// TrafficRecordSummary 水合增强后的单条往返记录摘要
type TrafficRecordSummary struct {
	ID               string  `json:"id"`
	SessionID        string  `json:"session_id"`
	SeqNo            int64   `json:"seq_no"`
	ProcessID        string  `json:"process_id"`
	ProcessName      string  `json:"process_name,omitempty"`
	ArchProcessName  string  `json:"arch_process_name,omitempty"`
	PeerProcName     string  `json:"peer_proc_name,omitempty"`
	ConnKey          string  `json:"conn_key"`
	ConnIndex        int     `json:"conn_index"`
	Direction        string  `json:"direction"`
	ExchangeType     string  `json:"exchange_type"`
	SrcAddr          string  `json:"src_addr"`
	DstAddr          string  `json:"dst_addr"`
	ReqCapturedAtNs  string  `json:"req_captured_at_ns"`
	RspCapturedAtNs  string  `json:"rsp_captured_at_ns,omitempty"`
	SNO              string  `json:"sno"`
	ServiceName      string  `json:"service_name"`
	Operation        string  `json:"operation"`
	LatencyUs        *int64  `json:"latency_us,omitempty"`
	StatusCode       string  `json:"status_code"`
	BizError         bool    `json:"biz_error"`
	BizStatusSummary string  `json:"biz_status_summary,omitempty"`
	TraceID          string  `json:"trace_id"`
	SpanID           string  `json:"span_id"`
	FatherSpanID     string  `json:"father_span_id"`
	SrcModuleID      *string `json:"src_module_id,omitempty"`
	SrcModuleName    string  `json:"src_module_name,omitempty"`
	DstModuleID      *string `json:"dst_module_id,omitempty"`
	DstModuleName    string  `json:"dst_module_name,omitempty"`
	DecodeStatus     string  `json:"decode_status"`
	IsTruncated      bool    `json:"is_truncated"`
	IsReplayable     bool    `json:"is_replayable"`
	CreatedAt        string  `json:"created_at"`
}

// TransactionTreeNodeDTO 事务调用树递归节点
type TransactionTreeNodeDTO struct {
	Record                TrafficRecordSummary     `json:"record"`
	StitchedIngressRecord *TrafficRecordSummary    `json:"stitched_ingress_record,omitempty"`
	NetHopUs              *int64                   `json:"net_hop_us,omitempty"`
	Depth                 int                      `json:"depth"`
	LinkConfidence        string                   `json:"link_confidence"`
	SiblingRelation       string                   `json:"sibling_relation"`
	WaterfallOffsetPct    float64                  `json:"waterfall_offset_pct"`
	WaterfallWidthPct     float64                  `json:"waterfall_width_pct"`
	SelfExecUs            *int64                   `json:"self_exec_us,omitempty"`
	IsCriticalPath        bool                     `json:"is_critical_path"`
	MatchedFilter         bool                     `json:"matched_filter"`
	Children              []TransactionTreeNodeDTO `json:"children"`
}

// EntityLifecycleStepDTO 业务实体生命周期单步
type EntityLifecycleStepDTO struct {
	RootRecordID string `json:"root_record_id"`
	SeqNo        int64  `json:"seq_no"`
	Operation    string `json:"operation"`
	Role         string `json:"role"` // PRODUCER | CONSUMER
}

// EntityLifecycleChainDTO 跨事务业务实体生命周期摘要项
type EntityLifecycleChainDTO struct {
	EntityKey            string                   `json:"entity_key"`
	FieldName            string                   `json:"field_name"`
	EntityValue          string                   `json:"entity_value"`
	ProducerRootRecordID string                   `json:"producer_root_record_id,omitempty"`
	Steps                []EntityLifecycleStepDTO `json:"steps"`
}

// TransactionTreeGroupDTO 单页根事务组
type TransactionTreeGroupDTO struct {
	RootRecordID           string                 `json:"root_record_id"`
	TraceID                string                 `json:"trace_id"`
	TotalNodeCount         int                    `json:"total_node_count"`
	TruncatedChildrenCount int                    `json:"truncated_children_count"`
	TotalDurationUs        int64                  `json:"total_duration_us"`
	HasProtocolError       bool                   `json:"has_protocol_error"`
	HasBizError            bool                   `json:"has_biz_error"`
	HasCompensation        bool                   `json:"has_compensation"`
	EntityTags             []string               `json:"entity_tags"`
	Root                   TransactionTreeNodeDTO `json:"root"`
}

// ParameterLineageEdgeDTO 单事务内参数数据流传递血缘有向边
type ParameterLineageEdgeDTO struct {
	FromRecordID  string `json:"from_record_id"`
	FromSide      string `json:"from_side"` // REQ | RSP
	FromFieldPath string `json:"from_field_path"`
	ToRecordID    string `json:"to_record_id"`
	ToSide        string `json:"to_side"` // REQ | RSP
	ToFieldPath   string `json:"to_field_path"`
	SampleValue   string `json:"sample_value"`
}

// SequenceParticipantDTO 交互式 UML 时序泳道参与者
type SequenceParticipantDTO struct {
	ParticipantKey string `json:"participant_key"`
	Title          string `json:"title"`
	Subtitle       string `json:"subtitle"`
	Kind           string `json:"kind"` // EXTERNAL_CALLER | MONITORED_PROCESS | EXTERNAL_DOWNSTREAM
}

// SequenceStageTimingDTO 阶段耗时
type SequenceStageTimingDTO struct {
	NetworkAndFramingUs *int64 `json:"network_and_framing_us,omitempty"`
	QueueWaitUs         *int64 `json:"queue_wait_us,omitempty"`
	ServerExecUs        *int64 `json:"server_exec_us,omitempty"`
}

// SequenceReplayOverlayDTO 重放裁决叠加
type SequenceReplayOverlayDTO struct {
	Status    string `json:"status"`
	DiffCount *int   `json:"diff_count,omitempty"`
	Summary   string `json:"summary,omitempty"`
}

// SequenceStepDTO 交互步
type SequenceStepDTO struct {
	StepIndex             int                       `json:"step_index"`
	Record                TrafficRecordSummary      `json:"record"`
	StitchedIngressRecord *TrafficRecordSummary     `json:"stitched_ingress_record,omitempty"`
	FromParticipant       string                    `json:"from_participant"`
	ToParticipant         string                    `json:"to_participant"`
	SiblingRelation       string                    `json:"sibling_relation"`
	OffsetUs              int64                     `json:"offset_us"`
	DurationUs            int64                     `json:"duration_us"`
	StageTiming           *SequenceStageTimingDTO   `json:"stage_timing,omitempty"`
	ReplayOverlay         *SequenceReplayOverlayDTO `json:"replay_overlay,omitempty"`
}

// TransactionSequenceDTO 全链路时序泳道图响应 DTO
type TransactionSequenceDTO struct {
	RootRecordID      string                    `json:"root_record_id"`
	TraceID           string                    `json:"trace_id"`
	TotalDurationUs   int64                     `json:"total_duration_us"`
	Participants      []SequenceParticipantDTO  `json:"participants"`
	Steps             []SequenceStepDTO         `json:"steps"`
	ParameterLineages []ParameterLineageEdgeDTO `json:"parameter_lineages"`
	EntityChains      []EntityLifecycleChainDTO `json:"entity_chains"`
}

// ReplayClosureMockItemDTO 下游 Mock 项
type ReplayClosureMockItemDTO struct {
	RecordID       string `json:"record_id"`
	SeqNo          int64  `json:"seq_no"`
	ServiceName    string `json:"service_name"`
	Operation      string `json:"operation"`
	ExchangeType   string `json:"exchange_type"`
	StatusCode     string `json:"status_code"`
	LinkConfidence string `json:"link_confidence"`
	UsableAsMock   bool   `json:"usable_as_mock"`
	Warning        string `json:"warning,omitempty"`
}

// ReplayClosurePrerequisiteDTO 前置依赖项
type ReplayClosurePrerequisiteDTO struct {
	RecordID        string `json:"record_id"`
	SeqNo           int64  `json:"seq_no"`
	ServiceName     string `json:"service_name"`
	Operation       string `json:"operation"`
	SharedEntityKey string `json:"shared_entity_key"`
	Role            string `json:"role"`
}

// ReplayClosurePreviewDTO 闭包预览响应 DTO
type ReplayClosurePreviewDTO struct {
	IngressRecord        TrafficRecordSummary           `json:"ingress_record"`
	ScopedEgressMocks    []ReplayClosureMockItemDTO     `json:"scoped_egress_mocks"`
	PrerequisiteChain    []ReplayClosurePrerequisiteDTO `json:"prerequisite_chain"`
	RecommendedRecordIDs []string                       `json:"recommended_record_ids"`
}

// TreeFilterOptions 树过滤条件
type TreeFilterOptions struct {
	ProcessID    uint64
	SubsystemID  uint64
	ModuleID     uint64
	Direction    string
	ExchangeType string
	ServiceName  string
	Operation    string
	SNO          uint64
	TraceID      string
	EntityKey    string
	HasError     *bool
	IsReplayable *bool
	Keyword      string
}

// RecordFilterMatches 判断单条记录是否命中过滤条件
func (f *TreeFilterOptions) Matches(r *TrafficRecordSummary, entityTags []string) bool {
	if f == nil {
		return true
	}
	if f.ProcessID > 0 && r.ProcessID != "" && r.ProcessID != "0" {
		if r.ProcessID != time.Duration(f.ProcessID).String() { // compare string
			// handled outside or string match
		}
	}
	if f.Direction != "" && r.Direction != f.Direction {
		return false
	}
	if f.ExchangeType != "" && r.ExchangeType != f.ExchangeType {
		return false
	}
	if f.ServiceName != "" && r.ServiceName != f.ServiceName {
		return false
	}
	if f.Operation != "" && r.Operation != f.Operation {
		return false
	}
	if f.TraceID != "" && r.TraceID != f.TraceID {
		return false
	}
	if f.IsReplayable != nil && r.IsReplayable != *f.IsReplayable {
		return false
	}
	if f.HasError != nil {
		hasErr := r.StatusCode != "0" || r.ExchangeType == "REQ_ONLY" || r.BizError
		if *f.HasError != hasErr {
			return false
		}
	}
	if f.EntityKey != "" {
		matched := false
		for _, tag := range entityTags {
			if tag == f.EntityKey {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
