import { apiRequest } from './client';
import type { ReplayTask, ReplayItem, ReplayItemDetailDTO, PaginatedResponse } from '../types';

export async function listReplayTasks(params?: {
  session_id?: string;
  target_process_id?: string;
  agent_id?: string;
  lifecycle_state?: string;
  result_status?: string;
  page?: number;
  page_size?: number;
}) {
  const query = new URLSearchParams();
  if (params?.session_id) query.set('session_id', params.session_id);
  if (params?.target_process_id) query.set('target_process_id', params.target_process_id);
  if (params?.agent_id) query.set('agent_id', params.agent_id);
  if (params?.lifecycle_state) query.set('lifecycle_state', params.lifecycle_state);
  if (params?.result_status) query.set('result_status', params.result_status);
  if (params?.page) query.set('page', String(params.page));
  if (params?.page_size) query.set('page_size', String(params.page_size));
  const qs = query.toString();
  return apiRequest<PaginatedResponse<ReplayTask>>(`/replay-tasks${qs ? `?${qs}` : ''}`);
}

export async function getReplayTask(id: string) {
  return apiRequest<ReplayTask>(`/replay-tasks/${id}`);
}

export async function createReplayTask(data: {
  name: string;
  session_id: string;
  source_process_id: string;
  target_process_id: string;
  selected_record_ids: string[];
  extra_ignore_paths?: string[];
  timeout_ms?: number;
}) {
  return apiRequest<ReplayTask>('/replay-tasks', {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function cancelReplayTask(id: string) {
  return apiRequest<ReplayTask>(`/replay-tasks/${id}/cancel`, {
    method: 'POST',
  });
}

export async function listReplayItems(taskId: string, params?: {
  result_status?: string;
  service_name?: string;
  operation?: string;
  page?: number;
  page_size?: number;
}) {
  const query = new URLSearchParams();
  if (params?.result_status) query.set('result_status', params.result_status);
  if (params?.service_name) query.set('service_name', params.service_name);
  if (params?.operation) query.set('operation', params.operation);
  if (params?.page) query.set('page', String(params.page));
  if (params?.page_size) query.set('page_size', String(params.page_size));
  const qs = query.toString();
  return apiRequest<PaginatedResponse<ReplayItem>>(`/replay-tasks/${taskId}/items${qs ? `?${qs}` : ''}`);
}

export async function getReplayItemDetail(taskId: string, itemId: string) {
  return apiRequest<ReplayItemDetailDTO>(`/replay-tasks/${taskId}/items/${itemId}`);
}

export async function rediffReplayTask(taskId: string, extraIgnorePaths: string[]) {
  return apiRequest<ReplayTask>(`/replay-tasks/${taskId}/rediff`, {
    method: 'POST',
    body: JSON.stringify({ extra_ignore_paths: extraIgnorePaths }),
  });
}
