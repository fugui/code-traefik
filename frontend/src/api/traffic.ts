import { apiRequest } from './client';
import type {
  RecordingSession,
  ConsistencyReportDTO,
  TrafficRecordSummary,
  PacketAnatomyDrawerDTO,
  PaginatedResponse,
  TransactionTreeGroupDTO,
  EntityLifecycleChainDTO,
  TransactionSequenceDTO,
  ReplayClosurePreviewDTO,
} from '../types';

export async function listRecordingSessions(params?: {
  scope?: 'SHARED' | 'PRIVATE';
  status?: string;
  process_id?: string;
  search?: string;
  page?: number;
  page_size?: number;
}) {
  const query = new URLSearchParams();
  if (params?.scope) query.set('scope', params.scope);
  if (params?.status) query.set('status', params.status);
  if (params?.process_id) query.set('process_id', params.process_id);
  if (params?.search) query.set('search', params.search);
  if (params?.page) query.set('page', String(params.page));
  if (params?.page_size) query.set('page_size', String(params.page_size));
  const qs = query.toString();
  return apiRequest<PaginatedResponse<RecordingSession>>(`/recording-sessions${qs ? `?${qs}` : ''}`);
}

export async function getRecordingSession(id: string) {
  return apiRequest<RecordingSession>(`/recording-sessions/${id}`);
}

export async function startRecordingSession(data: {
  name: string;
  scope: 'SHARED' | 'PRIVATE';
  target_process_ids: (string | number)[];
  max_duration_sec?: number;
  max_frames?: number;
  max_frame_bytes?: number;
  description?: string;
}) {
  const payload = {
    ...data,
    target_process_ids: (data.target_process_ids ?? []).map((id) => Number(id)),
  };
  return apiRequest<RecordingSession>('/recording-sessions', {
    method: 'POST',
    body: JSON.stringify(payload),
  });
}

export async function stopRecordingSession(id: string) {
  return apiRequest<RecordingSession>(`/recording-sessions/${id}/stop`, {
    method: 'POST',
  });
}

export async function removeProcessFromSession(sessionId: string, processId: string) {
  return apiRequest<{ message: string; session: RecordingSession }>(
    `/recording-sessions/${sessionId}/processes/${processId}`,
    {
      method: 'DELETE',
    }
  );
}

export async function publishRecordingSession(id: string) {
  return apiRequest<RecordingSession>(`/recording-sessions/${id}/publish`, {
    method: 'PATCH',
  });
}

export async function deleteRecordingSession(id: string) {
  return apiRequest<{ message: string }>(`/recording-sessions/${id}`, {
    method: 'DELETE',
  });
}

export async function getConsistencyReport(id: string, refresh?: boolean) {
  const query = new URLSearchParams();
  if (refresh) query.set('refresh', 'true');
  const qs = query.toString();
  return apiRequest<ConsistencyReportDTO>(`/recording-sessions/${id}/consistency-report${qs ? `?${qs}` : ''}`);
}

export async function listTrafficRecords(params: {
  session_id?: string;
  scope?: 'SHARED' | 'PRIVATE';
  subsystem_id?: string;
  module_id?: string;
  process_id?: string;
  direction?: 'INGRESS' | 'EGRESS';
  exchange_type?: string;
  service_name?: string;
  operation?: string;
  sno?: string;
  trace_id?: string;
  keyword?: string;
  sort_dir?: 'asc' | 'desc';
  is_replayable?: boolean;
  has_error?: boolean;
  page?: number;
  page_size?: number;
}) {
  const query = new URLSearchParams();
  if (params.session_id) query.set('session_id', params.session_id);
  if (params.scope) query.set('scope', params.scope);
  if (params.subsystem_id) query.set('subsystem_id', params.subsystem_id);
  if (params.module_id) query.set('module_id', params.module_id);
  if (params.process_id) query.set('process_id', params.process_id);
  if (params.direction) query.set('direction', params.direction);
  if (params.exchange_type) query.set('exchange_type', params.exchange_type);
  if (params.service_name) query.set('service_name', params.service_name);
  if (params.operation) query.set('operation', params.operation);
  if (params.sno) query.set('sno', params.sno);
  if (params.trace_id) query.set('trace_id', params.trace_id);
  if (params.keyword) query.set('keyword', params.keyword);
  if (params.sort_dir) query.set('sort_dir', params.sort_dir);
  if (params.is_replayable !== undefined) query.set('is_replayable', String(params.is_replayable));
  if (params.has_error !== undefined) query.set('has_error', String(params.has_error));
  if (params.page) query.set('page', String(params.page));
  if (params.page_size) query.set('page_size', String(params.page_size));
  const qs = query.toString();
  return apiRequest<PaginatedResponse<TrafficRecordSummary>>(`/traffic-records${qs ? `?${qs}` : ''}`);
}

export async function getTrafficRecordAnatomy(recordId: string, sessionId?: string) {
  const query = new URLSearchParams();
  if (sessionId) query.set('session_id', sessionId);
  const qs = query.toString();
  return apiRequest<PacketAnatomyDrawerDTO>(`/traffic-records/${recordId}/anatomy${qs ? `?${qs}` : ''}`);
}

export async function redecodeTrafficRecord(recordId: string) {
  return apiRequest<PacketAnatomyDrawerDTO>(`/traffic-records/${recordId}/redecode`, {
    method: 'POST',
  });
}

export interface TransactionListParams {
  process_id?: string;
  subsystem_id?: string;
  module_id?: string;
  direction?: string;
  exchange_type?: string;
  service_name?: string;
  operation?: string;
  sno?: string;
  trace_id?: string;
  entity_key?: string;
  has_error?: boolean;
  is_replayable?: boolean;
  keyword?: string;
  page?: number;
  page_size?: number;
}

export interface TransactionTreeListResponse {
  items: TransactionTreeGroupDTO[];
  total: number;
  page: number;
  page_size: number;
  entity_chains: EntityLifecycleChainDTO[];
}

export async function getTransactionTrees(sessionId: string, params: TransactionListParams = {}) {
  const query = new URLSearchParams();
  if (params.process_id) query.set('process_id', params.process_id);
  if (params.subsystem_id) query.set('subsystem_id', params.subsystem_id);
  if (params.module_id) query.set('module_id', params.module_id);
  if (params.direction) query.set('direction', params.direction);
  if (params.exchange_type) query.set('exchange_type', params.exchange_type);
  if (params.service_name) query.set('service_name', params.service_name);
  if (params.operation) query.set('operation', params.operation);
  if (params.sno) query.set('sno', params.sno);
  if (params.trace_id) query.set('trace_id', params.trace_id);
  if (params.entity_key) query.set('entity_key', params.entity_key);
  if (params.keyword) query.set('keyword', params.keyword);
  if (params.is_replayable !== undefined) query.set('is_replayable', String(params.is_replayable));
  if (params.has_error !== undefined) query.set('has_error', String(params.has_error));
  if (params.page) query.set('page', String(params.page));
  if (params.page_size) query.set('page_size', String(params.page_size));
  const qs = query.toString();
  return apiRequest<TransactionTreeListResponse>(`/recording-sessions/${sessionId}/transactions${qs ? `?${qs}` : ''}`);
}

export async function getTransactionSequence(sessionId: string, rootRecordId: string, replayTaskId?: string) {
  const query = new URLSearchParams();
  if (replayTaskId) query.set('replay_task_id', replayTaskId);
  const qs = query.toString();
  return apiRequest<TransactionSequenceDTO>(`/recording-sessions/${sessionId}/transactions/${rootRecordId}/sequence${qs ? `?${qs}` : ''}`);
}

export async function getReplayClosure(recordId: string) {
  return apiRequest<ReplayClosurePreviewDTO>(`/traffic-records/${recordId}/replay-closure`);
}
