import { apiRequest } from './client';
import type { Agent, Process, DiscoveredProcess, HandshakeResp, PaginatedResponse } from '../types';

export async function listAgents(params?: {
  scope?: 'SHARED' | 'PRIVATE';
  status?: 'ONLINE' | 'OFFLINE';
  lease_state?: 'IDLE' | 'LEASED';
  search?: string;
  page?: number;
  page_size?: number;
}) {
  const query = new URLSearchParams();
  if (params?.scope) query.set('scope', params.scope);
  if (params?.status) query.set('status', params.status);
  if (params?.lease_state) query.set('lease_state', params.lease_state);
  if (params?.search) query.set('search', params.search);
  if (params?.page) query.set('page', String(params.page));
  if (params?.page_size) query.set('page_size', String(params.page_size));
  const qs = query.toString();
  return apiRequest<PaginatedResponse<Agent>>(`/agents${qs ? `?${qs}` : ''}`);
}

export async function getAgent(id: string) {
  return apiRequest<{ agent: Agent; processes: Process[] }>(`/agents/${id}`);
}

export async function registerAgent(data: {
  name: string;
  endpoint: string;
  scope: 'SHARED' | 'PRIVATE';
  claim_code?: string;
  description?: string;
}) {
  return apiRequest<Agent>('/agents', {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function updateAgent(id: string, data: {
  name: string;
  endpoint: string;
  claim_code?: string;
  description?: string;
}) {
  return apiRequest<Agent>(`/agents/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  });
}

export async function deleteAgent(id: string) {
  return apiRequest<{ message: string }>(`/agents/${id}`, {
    method: 'DELETE',
  });
}

export async function handshakeAgent(id: string) {
  return apiRequest<{ agent: Agent; handshake: HandshakeResp }>(`/agents/${id}/handshake`, {
    method: 'POST',
  });
}

export async function discoverAgentProcesses(id: string) {
  return apiRequest<{ items: DiscoveredProcess[] }>(`/agents/${id}/discover-processes`);
}

export async function forceUnlockAgentLease(id: string, reason: string) {
  return apiRequest<{ message: string; agent: Agent }>(`/agents/${id}/lease/force-unlock`, {
    method: 'POST',
    body: JSON.stringify({ reason }),
  });
}

export async function listProcesses(params?: {
  agent_id?: string;
  scope?: 'SHARED' | 'PRIVATE';
  module_id?: string;
  search?: string;
  page?: number;
  page_size?: number;
}) {
  const query = new URLSearchParams();
  if (params?.agent_id) query.set('agent_id', params.agent_id);
  if (params?.scope) query.set('scope', params.scope);
  if (params?.module_id) query.set('module_id', params.module_id);
  if (params?.search) query.set('search', params.search);
  if (params?.page) query.set('page', String(params.page));
  if (params?.page_size) query.set('page_size', String(params.page_size));
  const qs = query.toString();
  return apiRequest<PaginatedResponse<Process>>(`/processes${qs ? `?${qs}` : ''}`);
}

export async function createProcess(data: {
  agent_id: string;
  name: string;
  listen_port: number;
  advertise_ip?: string;
  cmd_pattern?: string;
  module_ids: string[];
  extra_downstreams?: string[];
  description?: string;
}) {
  return apiRequest<Process>('/processes', {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function updateProcess(id: string, data: {
  name: string;
  listen_port: number;
  advertise_ip?: string;
  cmd_pattern?: string;
  module_ids: string[];
  extra_downstreams?: string[];
  description?: string;
}) {
  return apiRequest<Process>(`/processes/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  });
}

export async function deleteProcess(id: string) {
  return apiRequest<{ message: string }>(`/processes/${id}`, {
    method: 'DELETE',
  });
}
