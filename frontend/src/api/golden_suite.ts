import { apiRequest } from './client';
import type {
  CurateGoldenSuitePayload,
  CurateGoldenSuiteResponse,
  GoldenSuite,
  GoldenSuiteDetailResponse,
  GoldenSuiteItemSummary,
  GoldenSuiteListResponse,
  MaskRule,
  ReplayTask,
} from '../types';

export async function fetchGoldenSuites(params?: {
  arch_process_id?: number;
  status?: string;
  search?: string;
  page?: number;
  page_size?: number;
}): Promise<GoldenSuiteListResponse> {
  const q = new URLSearchParams();
  if (params?.arch_process_id) q.set('arch_process_id', String(params.arch_process_id));
  if (params?.status) q.set('status', params.status);
  if (params?.search) q.set('search', params.search);
  if (params?.page) q.set('page', String(params.page));
  if (params?.page_size) q.set('page_size', String(params.page_size));

  const queryStr = q.toString();
  return apiRequest<GoldenSuiteListResponse>(`/golden-suites${queryStr ? `?${queryStr}` : ''}`);
}

export async function fetchGoldenSuite(id: number): Promise<GoldenSuiteDetailResponse> {
  return apiRequest<GoldenSuiteDetailResponse>(`/golden-suites/${id}`);
}

export async function curateGoldenSuite(
  payload: CurateGoldenSuitePayload
): Promise<CurateGoldenSuiteResponse> {
  return apiRequest<CurateGoldenSuiteResponse>('/golden-suites/curate', {
    method: 'POST',
    body: JSON.stringify(payload),
  });
}

export async function updateGoldenSuite(
  id: number,
  payload: {
    name: string;
    version?: string;
    status: 'ACTIVE' | 'ARCHIVED';
    extra_ignore_paths?: string[];
    description?: string;
  }
): Promise<GoldenSuite> {
  return apiRequest<GoldenSuite>(`/golden-suites/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  });
}

export async function updateGoldenSuiteItem(
  suiteId: number,
  itemId: number,
  payload: {
    name?: string;
    order_no?: number;
    mask_rules_json?: MaskRule[];
  }
): Promise<GoldenSuiteItemSummary> {
  return apiRequest<GoldenSuiteItemSummary>(`/golden-suites/${suiteId}/items/${itemId}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  });
}

export async function deleteGoldenSuiteItem(
  suiteId: number,
  itemId: number
): Promise<{ message: string }> {
  return apiRequest<{ message: string }>(`/golden-suites/${suiteId}/items/${itemId}`, {
    method: 'DELETE',
  });
}

export async function deleteGoldenSuite(id: number): Promise<{ message: string }> {
  return apiRequest<{ message: string }>(`/golden-suites/${id}`, {
    method: 'DELETE',
  });
}

export async function executeGoldenSuite(
  id: number,
  payload: { env_agent_id: number; timeout_ms?: number }
): Promise<ReplayTask> {
  return apiRequest<ReplayTask>(`/golden-suites/${id}/execute`, {
    method: 'POST',
    body: JSON.stringify(payload),
  });
}
