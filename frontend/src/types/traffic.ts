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

export interface TrafficRecordSummary {
  id: string;
  session_id: string;
  process_id: string;
  direction: 'INGRESS' | 'EGRESS';
  exchange_type: 'UNARY' | 'STREAM_RSP' | 'ONEWAY' | 'REQ_ONLY' | 'RSP_ORPHAN' | 'MNG';
  sno: string;
  service_name: string;
  operation: string;
  latency_us: number;
  status_code: number;
  decode_status: string;
  src_module_name?: string;
  dst_module_name?: string;
  is_replayable: boolean;
  captured_at: string;
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
  source_module_id: string;
  source_module_name: string;
  target_module_id: string;
  target_module_name: string;
  service_name: string;
  operation: string;
  category: 'VERIFIED' | 'VIOLATION' | 'UNCOVERED' | 'NOT_MONITORED' | 'NOT_OBSERVABLE' | 'UNATTRIBUTED';
  call_count: number;
  detail?: string;
}

export interface ConsistencyReportDTO {
  session_id: string;
  stats: Record<'VERIFIED' | 'VIOLATION' | 'UNCOVERED' | 'NOT_MONITORED' | 'NOT_OBSERVABLE' | 'UNATTRIBUTED', number>;
  edges: ConsistencyEdge[];
}
