import { apiRequest } from './client';
import type {
  Subsystem,
  FunctionalGroupTreeNode,
  FunctionalGroup,
  Module,
  ModuleService,
  ModuleDependency,
  AvailableServiceItem,
  ClaimableProtoServiceItem,
  StaticTopologyDTO,
  PaginatedResponse,
} from '../types';

export async function listSubsystems(params?: { search?: string; page?: number; page_size?: number }) {
  const query = new URLSearchParams();
  if (params?.search) query.set('search', params.search);
  if (params?.page) query.set('page', String(params.page));
  if (params?.page_size) query.set('page_size', String(params.page_size));
  const qs = query.toString();
  return apiRequest<PaginatedResponse<Subsystem>>(`/arch/subsystems${qs ? `?${qs}` : ''}`);
}

export async function createSubsystem(data: { code: string; name: string; owner_id?: string; description?: string }) {
  return apiRequest<Subsystem>('/arch/subsystems', {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function updateSubsystem(id: string, data: { code?: string; name: string; owner_id?: string; description?: string }) {
  return apiRequest<Subsystem>(`/arch/subsystems/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  });
}

export async function deleteSubsystem(id: string) {
  return apiRequest<{ message: string }>(`/arch/subsystems/${id}`, {
    method: 'DELETE',
  });
}

export async function getFunctionalGroupTree(subsystemId: string) {
  return apiRequest<{ subsystem_id: string; tree: FunctionalGroupTreeNode[] }>(
    `/arch/functional-groups/tree?subsystem_id=${encodeURIComponent(subsystemId)}`
  );
}

export async function createFunctionalGroup(data: {
  subsystem_id: string;
  parent_id?: string | null;
  code: string;
  name: string;
  sort_order?: number;
  description?: string;
}) {
  return apiRequest<FunctionalGroup>('/arch/functional-groups', {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function updateFunctionalGroup(id: string, data: {
  code?: string;
  name: string;
  sort_order?: number;
  description?: string;
}) {
  return apiRequest<FunctionalGroup>(`/arch/functional-groups/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  });
}

export async function moveFunctionalGroup(id: string, data: {
  new_parent_id: string | null;
  sort_order?: number;
}) {
  return apiRequest<{ group: FunctionalGroup; updated_descendants: number }>(`/arch/functional-groups/${id}/move`, {
    method: 'PATCH',
    body: JSON.stringify(data),
  });
}

export async function deleteFunctionalGroup(id: string) {
  return apiRequest<{ message: string }>(`/arch/functional-groups/${id}`, {
    method: 'DELETE',
  });
}

export async function listModules(params?: {
  subsystem_id?: string;
  group_id?: string;
  include_descendants?: boolean;
  search?: string;
  page?: number;
  page_size?: number;
}) {
  const query = new URLSearchParams();
  if (params?.subsystem_id) query.set('subsystem_id', params.subsystem_id);
  if (params?.group_id) query.set('group_id', params.group_id);
  if (params?.include_descendants) query.set('include_descendants', 'true');
  if (params?.search) query.set('search', params.search);
  if (params?.page) query.set('page', String(params.page));
  if (params?.page_size) query.set('page_size', String(params.page_size));
  const qs = query.toString();
  return apiRequest<PaginatedResponse<Module>>(`/arch/modules${qs ? `?${qs}` : ''}`);
}

export async function getModule(id: string) {
  return apiRequest<{ module: Module; services: ModuleService[]; dependencies: ModuleDependency[] }>(`/arch/modules/${id}`);
}

export async function createModule(data: {
  group_id: string;
  code: string;
  name: string;
  owner_id?: string;
  process_id?: string | null;
  description?: string;
}) {
  return apiRequest<Module>('/arch/modules', {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function updateModule(id: string, data: {
  group_id: string;
  code?: string;
  name: string;
  owner_id?: string;
  process_id?: string | null;
  description?: string;
}) {
  return apiRequest<Module>(`/arch/modules/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  });
}

export async function deleteModule(id: string) {
  return apiRequest<{ message: string }>(`/arch/modules/${id}`, {
    method: 'DELETE',
  });
}

export async function bindModuleProcess(moduleId: string, processId: string | null) {
  return apiRequest<Module>(`/arch/modules/${moduleId}/process-binding`, {
    method: 'POST',
    body: JSON.stringify({ process_id: processId }),
  });
}

// --- Module Services (PROVIDE) ---
export async function listModuleServices(moduleId: string) {
  return apiRequest<{ items: ModuleService[] }>(`/arch/modules/${moduleId}/services`);
}

export async function createModuleService(moduleId: string, data: {
  service_name: string;
  diff_ignore_paths?: string[];
  description?: string;
}) {
  return apiRequest<ModuleService>(`/arch/modules/${moduleId}/services`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function updateModuleService(serviceId: string, data: {
  diff_ignore_paths: string[];
  description?: string;
}) {
  return apiRequest<ModuleService>(`/arch/module-services/${serviceId}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  });
}

export async function deleteModuleService(serviceId: string) {
  return apiRequest<{ message: string }>(`/arch/module-services/${serviceId}`, {
    method: 'DELETE',
  });
}

// --- Module Dependencies (DEPEND) ---
export async function listModuleDependencies(moduleId: string) {
  return apiRequest<{ items: ModuleDependency[] }>(`/arch/modules/${moduleId}/dependencies`);
}

export async function createModuleDependency(moduleId: string, data: {
  is_external: boolean;
  external_system?: string;
  external_service?: string;
  service_id?: string;
  description?: string;
}) {
  return apiRequest<ModuleDependency>(`/arch/modules/${moduleId}/dependencies`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function deleteModuleDependency(depId: string) {
  return apiRequest<{ message: string }>(`/arch/module-dependencies/${depId}`, {
    method: 'DELETE',
  });
}

export async function listAvailableServices(excludeModuleId?: string) {
  const query = new URLSearchParams();
  if (excludeModuleId) query.set('exclude_module_id', excludeModuleId);
  const qs = query.toString();
  return apiRequest<{ items: AvailableServiceItem[] }>(`/arch/services/available${qs ? `?${qs}` : ''}`);
}

export async function listClaimableProtoServices() {
  return apiRequest<{ items: ClaimableProtoServiceItem[] }>('/arch/proto-services/claimable');
}

export async function getStaticTopology(params?: { view?: 'LOGICAL' | 'PHYSICAL'; subsystem_id?: string }) {
  const query = new URLSearchParams();
  if (params?.view) query.set('view', params.view);
  if (params?.subsystem_id) query.set('subsystem_id', params.subsystem_id);
  const qs = query.toString();
  return apiRequest<StaticTopologyDTO>(`/arch/topology/static${qs ? `?${qs}` : ''}`);
}
