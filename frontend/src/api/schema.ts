import { AUTH_TOKEN_KEY } from '@code/common';
import { apiRequest } from './client';
import { MODULE_BASE_PATH } from '../constants';
import type { ProtoSchema, ProtoOperation, PaginatedResponse, OperationSchemaTreeDTO, ProtoSchemaSummaryDTO } from '../types';

export async function listProtoSchemas(params?: {
  name?: string;
  active_only?: boolean;
  search?: string;
  page?: number;
  page_size?: number;
}) {
  const query = new URLSearchParams();
  if (params?.name) query.set('name', params.name);
  if (params?.active_only) query.set('active_only', 'true');
  if (params?.search) query.set('search', params.search);
  if (params?.page) query.set('page', String(params.page));
  if (params?.page_size) query.set('page_size', String(params.page_size));
  const qs = query.toString();
  return apiRequest<PaginatedResponse<ProtoSchema>>(`/proto-schemas${qs ? `?${qs}` : ''}`);
}

export async function uploadProtoSchema(formData: FormData) {
  const token = localStorage.getItem(AUTH_TOKEN_KEY) ?? '';
  const isEmbedded = typeof window !== 'undefined' && Boolean(window.__POWERED_BY_PORTAL__);
  const baseUrl = isEmbedded ? `${MODULE_BASE_PATH}/api/proto-schemas` : '/api/proto-schemas';

  const res = await fetch(baseUrl, {
    method: 'POST',
    headers: {
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: formData,
  });

  const data = (await res.json().catch(() => ({}))) as Record<string, unknown>;
  if (!res.ok) {
    const errMsg = typeof data.error === 'string' ? data.error : '上传 Proto 描述集失败';
    throw new Error(errMsg);
  }
  return data as unknown as { schema: ProtoSchema; operations: ProtoOperation[] };
}

export async function activateProtoSchema(id: string) {
  return apiRequest<{ schema: ProtoSchema; activated_operations: number }>(`/proto-schemas/${id}/activate`, {
    method: 'POST',
  });
}

export async function deleteProtoSchema(id: string) {
  return apiRequest<{ message: string }>(`/proto-schemas/${id}`, {
    method: 'DELETE',
  });
}

export async function listProtoOperations(params?: {
  schema_id?: string;
  active_only?: boolean;
  service_name?: string;
  operation?: string;
  search?: string;
  page?: number;
  page_size?: number;
}) {
  const query = new URLSearchParams();
  if (params?.schema_id) query.set('schema_id', params.schema_id);
  if (params?.active_only) query.set('active_only', 'true');
  if (params?.service_name) query.set('service_name', params.service_name);
  if (params?.operation) query.set('operation', params.operation);
  if (params?.search) query.set('search', params.search);
  if (params?.page) query.set('page', String(params.page));
  if (params?.page_size) query.set('page_size', String(params.page_size));
  const qs = query.toString();
  return apiRequest<PaginatedResponse<ProtoOperation>>(`/proto-operations${qs ? `?${qs}` : ''}`);
}

export async function getOperationSchemaTree(id: string) {
  return apiRequest<OperationSchemaTreeDTO>(`/proto-operations/${id}/schema-tree`);
}

export async function updateOperationIgnorePaths(id: string, paths: string[]) {
  return apiRequest<ProtoOperation>(`/proto-operations/${id}/ignore-paths`, {
    method: 'PUT',
    body: JSON.stringify({ diff_ignore_paths: paths }),
  });
}

export async function getProtoSchemaSummary(id: string) {
  return apiRequest<ProtoSchemaSummaryDTO>(`/proto-schemas/${id}/summary`);
}

export async function downloadProtoSchema(id: string, fallbackFileName?: string) {
  const token = localStorage.getItem(AUTH_TOKEN_KEY) ?? '';
  const isEmbedded = typeof window !== 'undefined' && Boolean(window.__POWERED_BY_PORTAL__);
  const baseUrl = isEmbedded
    ? `${MODULE_BASE_PATH}/api/proto-schemas/${id}/download`
    : `/api/proto-schemas/${id}/download`;

  const res = await fetch(baseUrl, {
    method: 'GET',
    headers: {
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
  });

  if (!res.ok) {
    throw new Error('下载 Protobuf 描述集二进制失败');
  }

  const blob = await res.blob();
  let fileName = fallbackFileName || `schema_${id}.pb`;
  const disposition = res.headers.get('Content-Disposition');
  if (disposition && disposition.includes('filename=')) {
    const matched = /filename="?([^";]+)"?/i.exec(disposition);
    if (matched?.[1]) {
      fileName = matched[1];
    }
  }

  const url = window.URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = fileName;
  document.body.appendChild(a);
  a.click();
  a.remove();
  window.URL.revokeObjectURL(url);
}
