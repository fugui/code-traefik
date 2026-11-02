import { AUTH_TOKEN_KEY } from '@code/common';
import { apiRequest } from './client';
import { MODULE_BASE_PATH } from '../constants';
import type {
  ProtoSchemaLedgerItemDTO,
  ProtoSchemaLedgerListResponse,
  ProtoOperation,
  PaginatedResponse,
  OperationSchemaTreeDTO,
  ProtoSchemaSummaryDTO,
  ServiceContractGroupDTO,
} from '../types';

export async function listProtoSchemas(params?: {
  name?: string;
  category?: string;
  latest_only?: boolean;
  active_only?: boolean;
  search?: string;
  sort_by?: string;
  sort_dir?: string;
  page?: number;
  page_size?: number;
}) {
  const query = new URLSearchParams();
  if (params?.name) query.set('name', params.name);
  if (params?.category) query.set('category', params.category);
  if (params?.latest_only !== undefined) query.set('latest_only', String(params.latest_only));
  if (params?.active_only !== undefined) query.set('active_only', String(params.active_only));
  if (params?.search) query.set('search', params.search);
  if (params?.sort_by) query.set('sort_by', params.sort_by);
  if (params?.sort_dir) query.set('sort_dir', params.sort_dir);
  if (params?.page) query.set('page', String(params.page));
  if (params?.page_size) query.set('page_size', String(params.page_size));
  const qs = query.toString();
  return apiRequest<ProtoSchemaLedgerListResponse>(`/proto-schemas${qs ? `?${qs}` : ''}`);
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
    const err = new Error(typeof data.error === 'string' ? data.error : '上传 Proto 描述集失败');
    (err as unknown as { responseData?: unknown }).responseData = data;
    throw err;
  }
  return data as unknown as {
    schema: ProtoSchemaLedgerItemDTO;
    operations: ProtoOperation[];
    inherited_ignore_op_count?: number;
  };
}

export async function updateProtoSchema(
  id: string | number,
  data: {
    description?: string;
    category?: string;
    version_tag?: string;
  }
) {
  return apiRequest<ProtoSchemaLedgerItemDTO>(`/proto-schemas/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(data),
  });
}

export async function activateProtoSchema(id: string | number) {
  return apiRequest<{ schema: ProtoSchemaLedgerItemDTO; activated_operations: number }>(
    `/proto-schemas/${id}/activate`,
    {
      method: 'POST',
    }
  );
}

export async function deactivateProtoSchema(id: string | number) {
  return apiRequest<{ schema: ProtoSchemaLedgerItemDTO; deactivated_operations: number }>(
    `/proto-schemas/${id}/deactivate`,
    {
      method: 'POST',
    }
  );
}

export async function batchActivateProtoSchemas(ids: (string | number)[]) {
  return apiRequest<{ message: string; activated_count: number }>(`/proto-schemas/batch-activate`, {
    method: 'POST',
    body: JSON.stringify({ schema_ids: ids.map((id) => Number(id)) }),
  });
}

export async function deleteProtoSchema(id: string | number) {
  return apiRequest<{ message: string }>(`/proto-schemas/${id}`, {
    method: 'DELETE',
  });
}

export async function getProtoDependenciesGraph() {
  return apiRequest<{
    nodes: Array<{
      id: string;
      name: string;
      category: string;
      version: number;
      is_active: boolean;
      proto_files: string[];
      external_imports: string[];
    }>;
    edges: Array<{
      source: string;
      target: string;
      imported_proto: string;
      status: string;
    }>;
  }>('/proto-schemas/dependencies-graph');
}

export async function listProtoServices(params?: {
  schema_id?: string | number;
  category?: string;
  claimed_status?: string;
  active_only?: boolean;
  service_name?: string;
  search?: string;
  page?: number;
  page_size?: number;
}) {
  const query = new URLSearchParams();
  if (params?.schema_id) query.set('schema_id', String(params.schema_id));
  if (params?.category) query.set('category', params.category);
  if (params?.claimed_status) query.set('claimed_status', params.claimed_status);
  if (params?.active_only !== undefined) query.set('active_only', String(params.active_only));
  if (params?.service_name) query.set('service_name', params.service_name);
  if (params?.search) query.set('search', params.search);
  if (params?.page) query.set('page', String(params.page));
  if (params?.page_size) query.set('page_size', String(params.page_size));
  const qs = query.toString();
  return apiRequest<PaginatedResponse<ServiceContractGroupDTO>>(`/proto-services${qs ? `?${qs}` : ''}`);
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

export async function getProtoSchemaSummary(id: string | number) {
  return apiRequest<ProtoSchemaSummaryDTO>(`/proto-schemas/${id}/summary`);
}

export async function downloadProtoSchema(id: string | number, fallbackFileName?: string) {
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
