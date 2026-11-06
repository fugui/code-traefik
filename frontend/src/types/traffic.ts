export interface ByteSliceSegment {
  offsetStart: number;
  byteLength: number;
  fieldName: string;
  rawHex: string;
  displayValue: string;
}

/** ① 第一段：24B 公共头偏移位图与校验概览 */
export interface CommHeadAnatomyDTO {
  magicAscii: string;
  transportProtocol: number;
  transportName: string;
  snoDec: string;
  snoHex: string;
  uiLength: number;
  usHeadLen: number;
  ucMsgType: number;
  msgTypeName: string;
  isPacked: number;
  segments: ByteSliceSegment[];
}

/** ② 第二段：多版本会话头明细与阶段耗时计算 */
export interface SessionHeadAnatomyDTO {
  headKind: 'REQ' | 'RSP' | 'MNG';
  detectedVersion: string;
  sessionHeadLen: number;
  serviceName: string;
  operation: string;
  reqType?: number;
  uiStatusCode?: number;
  ullStatusCode?: string;
  rspMsgNo?: number;
  traceBuffHex?: string;
  timingBreakdownUs?: {
    clientToServerUs?: number;
    serverProcessUs?: number;
    networkAndFramingUs?: number;
    queueWaitUs?: number;
    serverExecUs?: number;
  };
  fields: Array<{ name: string; offsetRange: string; value: string }>;
}

/** ③ 第三段：0..N 段扩展头（DOTR Trace 与入队时间戳） */
export interface ExtendHeadAnatomyDTO {
  totalExtendBytes: number;
  trace?: {
    traceIdHex: string;
    spanIdHex: string;
    fatherSpanIdHex: string;
    recordMsgSwitch: number;
  };
  serverEnqueueTime?: string;
  queueWaitUs?: number;
  items: Array<{ ucType: number; typeName: string; usHeadLen: number; rawHex: string }>;
}

/** ④ 第四段：Protobuf 动态 JSON 与 16B 对齐 Raw Hex 对照 */
export interface PayloadAnatomyDTO {
  payloadOffset: number;
  payloadLength: number;
  isTruncated: boolean;
  isReplayable: boolean;
  decodeStatus: 'DECODED_JSON' | 'NO_SCHEMA_RAW_HEX' | 'DECODE_ERR_RAW_HEX' | 'TRUNCATED' | 'MNG_RAW';
  schemaName?: string;
  schemaVersion?: number;
  messageFullName?: string;
  decodedJson?: Record<string, unknown> | Array<Record<string, unknown>>;
  rawFrameHexLines: Array<{
    offsetHex: string;
    hexCols: string;
    asciiCols: string;
    region: 'COMM_HEAD' | 'SESSION_HEAD' | 'EXTEND_HEAD' | 'PAYLOAD';
  }>;
}

/** 单侧报文四段完整解剖结构 */
export interface FrameSideAnatomyDTO {
  commHead: CommHeadAnatomyDTO;
  sessionHead: SessionHeadAnatomyDTO;
  extendHead: ExtendHeadAnatomyDTO;
  payload: PayloadAnatomyDTO;
  segmentCount: number;
}

export interface PacketAnatomyDrawerDTO {
  recordId: string;
  sessionId: string;
  direction: 'INGRESS' | 'EGRESS';
  exchangeType: 'UNARY' | 'STREAM_RSP' | 'ONEWAY' | 'REQ_ONLY' | 'RSP_ORPHAN' | 'MNG';
  roundTripLatencyUs?: number;
  srcModuleName?: string;
  dstModuleName?: string;
  reqSide?: FrameSideAnatomyDTO;
  rspSide?: FrameSideAnatomyDTO;
}

export type CallLinkConfidence = 'DOTR_EXACT' | 'FINGERPRINT_INFERRED' | 'TEMPORAL_INFERRED' | 'ROOT';
export type SiblingRelationKind = 'ROOT' | 'SEQUENTIAL' | 'PARALLEL' | 'COMPENSATION';

export interface TrafficRecordSummary {
  id: string;
  session_id: string;
  seq_no?: number;
  process_id: string;
  process_name?: string;
  arch_process_name?: string;
  peer_proc_name?: string;
  conn_key?: string;
  conn_index?: number;
  direction: 'INGRESS' | 'EGRESS';
  exchange_type: 'UNARY' | 'STREAM_RSP' | 'ONEWAY' | 'REQ_ONLY' | 'RSP_ORPHAN' | 'MNG';
  src_addr?: string;
  dst_addr?: string;
  req_captured_at_ns?: string;
  rsp_captured_at_ns?: string;
  sno: string;
  service_name: string;
  operation: string;
  latency_us?: number;
  status_code: string | number;
  biz_error?: boolean;
  biz_status_summary?: string;
  trace_id?: string;
  span_id?: string;
  father_span_id?: string;
  src_module_id?: string;
  src_module_name?: string;
  dst_module_id?: string;
  dst_module_name?: string;
  decode_status: string;
  is_truncated?: boolean;
  is_replayable: boolean;
  created_at?: string;
  captured_at?: string;
}

export interface TransactionTreeNodeDTO {
  record: TrafficRecordSummary;
  stitched_ingress_record?: TrafficRecordSummary;
  net_hop_us?: number;
  depth: number;
  link_confidence: CallLinkConfidence;
  sibling_relation: SiblingRelationKind;
  waterfall_offset_pct: number;
  waterfall_width_pct: number;
  self_exec_us?: number;
  is_critical_path: boolean;
  matched_filter: boolean;
  children: TransactionTreeNodeDTO[];
}

export interface EntityLifecycleStepDTO {
  root_record_id: string;
  seq_no: number;
  operation: string;
  role: 'PRODUCER' | 'CONSUMER';
}

export interface EntityLifecycleChainDTO {
  entity_key: string;
  field_name: string;
  entity_value: string;
  producer_root_record_id?: string;
  steps: EntityLifecycleStepDTO[];
}

export interface TransactionTreeGroupDTO {
  root_record_id: string;
  trace_id: string;
  total_node_count: number;
  truncated_children_count: number;
  total_duration_us: number;
  has_protocol_error: boolean;
  has_biz_error: boolean;
  has_compensation: boolean;
  entity_tags?: string[] | null;
  root: TransactionTreeNodeDTO;
}

export interface ParameterLineageEdgeDTO {
  from_record_id: string;
  from_side: 'REQ' | 'RSP';
  from_field_path: string;
  to_record_id: string;
  to_side: 'REQ' | 'RSP';
  to_field_path: string;
  sample_value: string;
}

export interface SequenceParticipantDTO {
  participant_key: string;
  title: string;
  subtitle: string;
  kind: 'EXTERNAL_CALLER' | 'MONITORED_PROCESS' | 'EXTERNAL_DOWNSTREAM';
}

export interface SequenceStageTimingDTO {
  network_and_framing_us?: number;
  queue_wait_us?: number;
  server_exec_us?: number;
}

export interface SequenceReplayOverlayDTO {
  status: 'PASS' | 'FAILED' | 'MOCK_MISS' | 'SCOPED_HIT' | 'GLOBAL_FALLBACK_HIT' | 'TIMEOUT' | 'ERROR';
  diff_count?: number;
  summary?: string;
}

export interface SequenceStepDTO {
  step_index: number;
  record: TrafficRecordSummary;
  stitched_ingress_record?: TrafficRecordSummary;
  from_participant: string;
  to_participant: string;
  sibling_relation: SiblingRelationKind;
  offset_us: number;
  duration_us: number;
  stage_timing?: SequenceStageTimingDTO;
  replay_overlay?: SequenceReplayOverlayDTO;
}

export interface TransactionSequenceDTO {
  root_record_id: string;
  trace_id: string;
  total_duration_us: number;
  participants: SequenceParticipantDTO[];
  steps: SequenceStepDTO[];
  parameter_lineages?: ParameterLineageEdgeDTO[] | null;
  entity_chains?: EntityLifecycleChainDTO[] | null;
}

export interface ReplayClosureMockItemDTO {
  record_id: string;
  seq_no: number;
  service_name: string;
  operation: string;
  exchange_type: string;
  status_code: string;
  link_confidence: CallLinkConfidence;
  usable_as_mock: boolean;
  warning?: string;
}

export interface ReplayClosurePrerequisiteDTO {
  record_id: string;
  seq_no: number;
  service_name: string;
  operation: string;
  shared_entity_key: string;
  role: 'PRODUCER' | 'STATE_MUTATOR';
}

export interface ReplayClosurePreviewDTO {
  ingress_record: TrafficRecordSummary;
  scoped_egress_mocks?: ReplayClosureMockItemDTO[] | null;
  prerequisite_chain?: ReplayClosurePrerequisiteDTO[] | null;
  recommended_record_ids?: string[] | null;
}

export interface ProcessDiagnostic {
  process_id: string;
  process_name?: string;
  pid: number;
  listen_port: number;
  established_conns_count: number;
  need_reconnect: boolean;
  reconnect_guide?: string;
}

export interface RecordingSession {
  id: string;
  session_no: string;
  name: string;
  scope: 'SHARED' | 'PRIVATE';
  owner_id?: string;
  owner_name?: string;
  status: 'RECORDING' | 'STOPPING' | 'COMPLETED' | 'COMPLETED_BY_LIMIT' | 'FAILED' | 'ABORTED';
  target_process_ids: string[];
  leased_agent_ids?: string[];
  max_duration_sec: number;
  max_frames: number;
  captured_frames: number;
  process_diagnostics?: ProcessDiagnostic[];
  description?: string;
  created_at?: string;
  updated_at?: string;
}

export interface ConsistencyEdge {
  source_module_id?: string | number;
  src_module_id?: string | number;
  source_module_name?: string;
  src_module_name?: string;
  src_process_id?: string | number;
  candidate_module_ids?: Array<string | number>;
  target_module_id?: string | number;
  dst_module_id?: string | number;
  target_module_name?: string;
  dst_module_name?: string;
  service_name: string;
  operation: string;
  category: 'VERIFIED' | 'OUTBOUND_VERIFIED' | 'VIOLATION' | 'UNCOVERED' | 'NOT_MONITORED' | 'NOT_OBSERVABLE' | 'UNATTRIBUTED';
  status?: 'VERIFIED' | 'OUTBOUND_VERIFIED' | 'VIOLATION' | 'UNCOVERED' | 'NOT_MONITORED' | 'NOT_OBSERVABLE' | 'UNATTRIBUTED';
  call_count: number;
  observed_call_count?: number;
  sample_traffic_record_id?: string | number;
  attribution_mode?: string;
  detail?: string;
}

export interface ConsistencyReportDTO {
  session_id: string;
  stats?: Record<'VERIFIED' | 'OUTBOUND_VERIFIED' | 'VIOLATION' | 'UNCOVERED' | 'NOT_MONITORED' | 'NOT_OBSERVABLE' | 'UNATTRIBUTED', number>;
  edges: ConsistencyEdge[];
  verified_count?: number;
  outbound_verified_count?: number;
  violation_count?: number;
  uncovered_count?: number;
  not_monitored_count?: number;
  not_observable_count?: number;
  unattributed_count?: number;
}
