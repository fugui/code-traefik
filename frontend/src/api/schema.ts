import { AUTH_TOKEN_KEY } from '@code/common';
import { apiRequest } from './client';
import { MODULE_BASE_PATH } from '../constants';
import type {
  CheckReportDTO,
  CheckReportListResponse,
  CheckRuleDTO,
  CheckRuleListResponse,
  CreateCheckRulePayload,
  CreateVocabularyTermPayload,
  TriggerCheckScanPayload,
  UpdateCheckRulePayload,
  UpdateVocabularyTermPayload,
  VocabularyTermDTO,
  VocabularyTermListResponse,
  ProtoSchemaLedgerItemDTO,
  ProtoSchemaLedgerListResponse,
  ProtoOperation,
  PaginatedResponse,
  OperationSchemaTreeDTO,
  ProtoSchemaSummaryDTO,
  ServiceContractGroupDTO,
  ProtoDocTreeResponse,
  ProtoServiceDocDetailDTO,
  ProtoSchemaSourcesResponse,
  ProtoFileDTO,
  ProtoFileContentResponse,
  ProtoFileVersionSummaryDTO,
  ProtoFileDiffResponse,
  GitRepoDTO,
  GitProtoBundleDTO,
  CreateGitRepoPayload,
  UpdateGitRepoPayload,
  GitRepoBundleInput,
  TriggerGitSyncResponse,
  GitConsistencyReportDTO,
  GitRepoBranchesResponse,
  MRCheckRunDTO,
  MRCheckRunDetailDTO,
  MRCheckRunListResponse,
} from '../types';

export async function listProtoSchemas(params?: {
  name?: string;
  category?: string;
  source_type?: string;
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
  if (params?.source_type) query.set('source_type', params.source_type);
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

export interface ActivateOptions {
  allow_breaking?: boolean;
  breaking_reason?: string;
}

export async function activateProtoSchema(id: string | number, options?: ActivateOptions) {
  return apiRequest<{ schema: ProtoSchemaLedgerItemDTO; activated_operations: number; forced_breaking?: boolean }>(
    `/proto-schemas/${id}/activate`,
    {
      method: 'POST',
      body: options ? JSON.stringify(options) : undefined,
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

export async function batchActivateProtoSchemas(ids: (string | number)[], options?: ActivateOptions) {
  return apiRequest<{ message: string; activated_count: number; forced_breaking?: boolean }>(`/proto-schemas/batch-activate`, {
    method: 'POST',
    body: JSON.stringify({
      schema_ids: ids.map((id) => Number(id)),
      ...(options || {}),
    }),
  });
}

export async function deleteProtoSchema(id: string | number) {
  return apiRequest<{ message: string }>(`/proto-schemas/${id}`, {
    method: 'DELETE',
  });
}

export async function compileProtoSchema(payload: import('../types').CompileProtoSchemaPayload) {
  return apiRequest<{
    schema: import('../types').ProtoSchemaLedgerItemDTO;
    operations: import('../types').ProtoOperation[];
    proto_files: string[];
    external_imports: string[];
    resolved_dependencies?: import('../types').ResolvedSchemaDependencyDTO[];
    inherited_ignore_op_count?: number;
    forced_breaking?: boolean;
  }>('/proto-schemas/compile', {
    method: 'POST',
    body: JSON.stringify(payload),
  });
}

export async function getProtoDependenciesGraph(params?: { active_only?: boolean }) {
  const query = new URLSearchParams();
  if (params?.active_only !== undefined) {
    query.set('active_only', String(params.active_only));
  }
  const qs = query.toString();
  return apiRequest<import('../types').ProtoDependenciesGraphDTO>(
    `/proto-schemas/dependencies-graph${qs ? `?${qs}` : ''}`
  );
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

// ==========================================
// M23: 活体接口参考文档与过渡只读源码 API
// ==========================================

export async function getProtoDocsTree(params?: {
  subsystem_id?: number;
  category?: string;
  schema_id?: number;
  include_deprecated?: boolean;
  search?: string;
}) {
  const qs = new URLSearchParams();
  if (params?.subsystem_id) qs.set('subsystem_id', String(params.subsystem_id));
  if (params?.category) qs.set('category', params.category);
  if (params?.schema_id) qs.set('schema_id', String(params.schema_id));
  if (params?.include_deprecated !== undefined) {
    qs.set('include_deprecated', String(params.include_deprecated));
  }
  if (params?.search) qs.set('search', params.search);
  const query = qs.toString();
  return apiRequest<ProtoDocTreeResponse>(`/proto-docs/tree${query ? `?${query}` : ''}`);
}

export async function getProtoServiceDocDetail(serviceName: string, schemaId?: number | string) {
  const qs = new URLSearchParams();
  if (schemaId !== undefined && schemaId !== '') {
    qs.set('schema_id', String(schemaId));
  }
  const query = qs.toString();
  return apiRequest<ProtoServiceDocDetailDTO>(
    `/proto-docs/services/${encodeURIComponent(serviceName)}${query ? `?${query}` : ''}`
  );
}

export async function exportProtoDocsMarkdown(params: {
  service_name?: string;
  schema_id?: number | string;
  subsystem_id?: number | string;
}) {
  const qs = new URLSearchParams();
  if (params.service_name) qs.set('service_name', params.service_name);
  if (params.schema_id !== undefined && params.schema_id !== '') {
    qs.set('schema_id', String(params.schema_id));
  }
  if (params.subsystem_id !== undefined && params.subsystem_id !== '') {
    qs.set('subsystem_id', String(params.subsystem_id));
  }
  qs.set('format', 'markdown');

  const token = localStorage.getItem(AUTH_TOKEN_KEY) ?? '';
  const isEmbedded = typeof window !== 'undefined' && Boolean(window.__POWERED_BY_PORTAL__);
  const url = isEmbedded
    ? `${MODULE_BASE_PATH}/api/proto-docs/export?${qs.toString()}`
    : `/api/proto-docs/export?${qs.toString()}`;

  const res = await fetch(url, {
    method: 'GET',
    headers: {
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
  });
  if (!res.ok) {
    throw new Error('导出离线 Markdown 接口参考手册失败');
  }

  const blob = await res.blob();
  let fileName = `${params.service_name || 'proto-reference'}.md`;
  const disposition = res.headers.get('Content-Disposition');
  if (disposition && disposition.includes('filename=')) {
    const matched = /filename="?([^";]+)"?/i.exec(disposition);
    if (matched?.[1]) {
      fileName = matched[1];
    }
  }

  const objectUrl = window.URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = objectUrl;
  a.download = fileName;
  document.body.appendChild(a);
  a.click();
  a.remove();
  window.URL.revokeObjectURL(objectUrl);
}

export async function getProtoSchemaSources(schemaId: number | string, filePath?: string) {
  const qs = new URLSearchParams();
  if (filePath) qs.set('file_path', filePath);
  const query = qs.toString();
  return apiRequest<ProtoSchemaSourcesResponse>(
    `/proto-schemas/${schemaId}/sources${query ? `?${query}` : ''}`
  );
}

export async function listProtoFiles(params?: {
  git_repo_id?: number;
  bundle_id?: number;
  target_schema_name?: string;
  package_name?: string;
  include_deleted?: boolean;
  search?: string;
  page?: number;
  page_size?: number;
}) {
  const qs = new URLSearchParams();
  if (params?.git_repo_id) qs.set('git_repo_id', String(params.git_repo_id));
  if (params?.bundle_id) qs.set('bundle_id', String(params.bundle_id));
  if (params?.target_schema_name) qs.set('target_schema_name', params.target_schema_name);
  if (params?.package_name) qs.set('package_name', params.package_name);
  if (params?.include_deleted !== undefined) {
    qs.set('include_deleted', params.include_deleted ? 'true' : 'false');
  }
  if (params?.search) qs.set('search', params.search);
  if (params?.page) qs.set('page', String(params.page));
  if (params?.page_size) qs.set('page_size', String(params.page_size));
  const query = qs.toString();
  return apiRequest<PaginatedResponse<ProtoFileDTO> & { storage_mode?: string }>(
    `/proto-files${query ? `?${query}` : ''}`
  );
}

export async function getProtoFileContent(fileId: number | string, version?: number) {
  const qs = new URLSearchParams();
  if (version) qs.set('version', String(version));
  const query = qs.toString();
  return apiRequest<ProtoFileContentResponse>(
    `/proto-files/${fileId}/content${query ? `?${query}` : ''}`
  );
}

export async function listProtoFileVersions(
  fileId: number | string,
  params?: { page?: number; page_size?: number }
) {
  const qs = new URLSearchParams();
  if (params?.page) qs.set('page', String(params.page));
  if (params?.page_size) qs.set('page_size', String(params.page_size));
  const query = qs.toString();
  return apiRequest<PaginatedResponse<ProtoFileVersionSummaryDTO>>(
    `/proto-files/${fileId}/versions${query ? `?${query}` : ''}`
  );
}

export async function getProtoFileDiff(
  fileId: number | string,
  fromVersion: number | 'prev',
  toVersion: number | 'latest' = 'latest'
) {
  const qs = new URLSearchParams();
  qs.set('from_version', String(fromVersion));
  qs.set('to_version', String(toVersion));
  return apiRequest<ProtoFileDiffResponse>(`/proto-files/${fileId}/diff?${qs.toString()}`);
}

export async function listCheckRules(params?: {
  page?: number;
  page_size?: number;
  category?: string;
  severity?: string;
  is_enabled?: string;
  search?: string;
}) {
  const qs = new URLSearchParams();
  if (params?.page) qs.set('page', String(params.page));
  if (params?.page_size) qs.set('page_size', String(params.page_size));
  if (params?.category) qs.set('category', params.category);
  if (params?.severity) qs.set('severity', params.severity);
  if (params?.is_enabled) qs.set('is_enabled', params.is_enabled);
  if (params?.search) qs.set('search', params.search);
  const query = qs.toString();
  return apiRequest<CheckRuleListResponse>(`/check-rules${query ? `?${query}` : ''}`);
}

export async function createCheckRule(payload: CreateCheckRulePayload) {
  return apiRequest<CheckRuleDTO>('/check-rules', {
    method: 'POST',
    body: JSON.stringify(payload),
  });
}

export async function updateCheckRule(id: number, payload: UpdateCheckRulePayload) {
  return apiRequest<CheckRuleDTO>(`/check-rules/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  });
}

export async function deleteCheckRule(id: number) {
  return apiRequest<{ message: string; id: number }>(`/check-rules/${id}`, {
    method: 'DELETE',
  });
}

export async function listVocabularyTerms(params?: {
  page?: number;
  page_size?: number;
  domain?: string;
  source?: string;
  is_approved?: string;
  search?: string;
}) {
  const qs = new URLSearchParams();
  if (params?.page) qs.set('page', String(params.page));
  if (params?.page_size) qs.set('page_size', String(params.page_size));
  if (params?.domain) qs.set('domain', params.domain);
  if (params?.source) qs.set('source', params.source);
  if (params?.is_approved) qs.set('is_approved', params.is_approved);
  if (params?.search) qs.set('search', params.search);
  const query = qs.toString();
  return apiRequest<VocabularyTermListResponse>(`/vocabulary-terms${query ? `?${query}` : ''}`);
}

export async function createVocabularyTerm(payload: CreateVocabularyTermPayload) {
  return apiRequest<VocabularyTermDTO>('/vocabulary-terms', {
    method: 'POST',
    body: JSON.stringify(payload),
  });
}

export async function updateVocabularyTerm(id: number, payload: UpdateVocabularyTermPayload) {
  return apiRequest<VocabularyTermDTO>(`/vocabulary-terms/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  });
}

export async function deleteVocabularyTerm(id: number) {
  return apiRequest<{ message: string; id: number }>(`/vocabulary-terms/${id}`, {
    method: 'DELETE',
  });
}

export async function triggerCheckReportScan(payload: TriggerCheckScanPayload) {
  return apiRequest<CheckReportDTO>('/check-reports/scan', {
    method: 'POST',
    body: JSON.stringify(payload),
  });
}

export async function listCheckReports(params?: {
  page?: number;
  page_size?: number;
  schema_id?: number;
  status?: string;
  trigger_source?: string;
}) {
  const qs = new URLSearchParams();
  if (params?.page) qs.set('page', String(params.page));
  if (params?.page_size) qs.set('page_size', String(params.page_size));
  if (params?.schema_id) qs.set('schema_id', String(params.schema_id));
  if (params?.status) qs.set('status', params.status);
  if (params?.trigger_source) qs.set('trigger_source', params.trigger_source);
  const query = qs.toString();
  return apiRequest<CheckReportListResponse>(`/check-reports${query ? `?${query}` : ''}`);
}

export async function getCheckReportDetail(id: number) {
  return apiRequest<CheckReportDTO>(`/check-reports/${id}`);
}

// =====================================================================
// M25: Git 仓库、Monorepo 多包 (Bundles) 与只读一致性对账 API
// =====================================================================

export async function listGitRepos(params?: {
  provider?: string;
  last_sync_status?: string;
  search?: string;
  page?: number;
  page_size?: number;
}) {
  const qs = new URLSearchParams();
  if (params?.provider) qs.set('provider', params.provider);
  if (params?.last_sync_status) qs.set('last_sync_status', params.last_sync_status);
  if (params?.search) qs.set('search', params.search);
  if (params?.page) qs.set('page', String(params.page));
  if (params?.page_size) qs.set('page_size', String(params.page_size));
  const query = qs.toString();
  return apiRequest<PaginatedResponse<GitRepoDTO>>(`/git-repos${query ? `?${query}` : ''}`);
}

export async function getGitRepoDetail(id: number | string) {
  return apiRequest<GitRepoDTO>(`/git-repos/${id}`);
}

export async function createGitRepo(payload: CreateGitRepoPayload) {
  return apiRequest<GitRepoDTO>('/git-repos', {
    method: 'POST',
    body: JSON.stringify(payload),
  });
}

export async function updateGitRepo(id: number | string, payload: UpdateGitRepoPayload) {
  return apiRequest<GitRepoDTO>(`/git-repos/${id}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  });
}

export async function deleteGitRepo(id: number | string) {
  return apiRequest<{ deleted: boolean; id: number }>(`/git-repos/${id}`, {
    method: 'DELETE',
  });
}

export async function createGitRepoBundle(repoId: number | string, payload: GitRepoBundleInput) {
  return apiRequest<GitProtoBundleDTO>(`/git-repos/${repoId}/bundles`, {
    method: 'POST',
    body: JSON.stringify(payload),
  });
}

export async function updateGitRepoBundle(
  repoId: number | string,
  bundleId: number | string,
  payload: Partial<GitRepoBundleInput>
) {
  return apiRequest<GitProtoBundleDTO>(`/git-repos/${repoId}/bundles/${bundleId}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  });
}

export async function deleteGitRepoBundle(repoId: number | string, bundleId: number | string) {
  return apiRequest<{ deleted: boolean; bundle_id: number }>(
    `/git-repos/${repoId}/bundles/${bundleId}`,
    {
      method: 'DELETE',
    }
  );
}

export async function triggerGitRepoSync(
  repoId: number | string,
  payload?: { bundle_id?: number; branch?: string; force_recompile?: boolean }
) {
  return apiRequest<TriggerGitSyncResponse>(`/git-repos/${repoId}/sync`, {
    method: 'POST',
    body: JSON.stringify(payload ?? {}),
  });
}

export async function getGitRepoConsistency(
  repoId: number | string,
  params?: { bundle_id?: number; branch?: string }
) {
  const qs = new URLSearchParams();
  if (params?.bundle_id) qs.set('bundle_id', String(params.bundle_id));
  if (params?.branch) qs.set('branch', params.branch);
  const query = qs.toString();
  return apiRequest<GitConsistencyReportDTO>(
    `/git-repos/${repoId}/consistency${query ? `?${query}` : ''}`
  );
}

export async function getGitRepoBranches(repoId: number | string) {
  return apiRequest<GitRepoBranchesResponse>(`/git-repos/${repoId}/branches`);
}

// =====================================================================
// M26: Git MR / PR 四合一契约门禁 API
// =====================================================================

export async function listMRCheckRuns(params?: {
  git_repo_id?: number;
  bundle_id?: number;
  status?: string;
  event_phase?: string;
  mr_iid?: number;
  q?: string;
  page?: number;
  page_size?: number;
}) {
  const qs = new URLSearchParams();
  if (params?.git_repo_id) qs.set('git_repo_id', String(params.git_repo_id));
  if (params?.bundle_id) qs.set('bundle_id', String(params.bundle_id));
  if (params?.status) qs.set('status', params.status);
  if (params?.event_phase) qs.set('event_phase', params.event_phase);
  if (params?.mr_iid) qs.set('mr_iid', String(params.mr_iid));
  if (params?.q) qs.set('q', params.q);
  if (params?.page) qs.set('page', String(params.page));
  if (params?.page_size) qs.set('page_size', String(params.page_size));
  const query = qs.toString();
  return apiRequest<MRCheckRunListResponse>(`/mr-check-runs${query ? `?${query}` : ''}`);
}

export async function getMRCheckRunDetail(id: number | string) {
  return apiRequest<MRCheckRunDetailDTO>(`/mr-check-runs/${id}`);
}

export async function overrideMRCheckRun(id: number | string, overrideReason: string) {
  return apiRequest<MRCheckRunDTO>(`/mr-check-runs/${id}/override`, {
    method: 'POST',
    bodyData: { override_reason: overrideReason },
  });
}

export async function rerunMRCheckRun(id: number | string) {
  return apiRequest<MRCheckRunDTO>(`/mr-check-runs/${id}/rerun`, {
    method: 'POST',
  });
}
