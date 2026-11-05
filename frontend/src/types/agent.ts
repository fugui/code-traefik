import type { ArchProcess } from './arch';

export interface Agent {
  id: string;
  name: string;
  endpoint: string;
  scope: 'SHARED' | 'PRIVATE';
  owner_id?: string;
  owner_name?: string;
  owner?: { id: number | string; username?: string; name?: string };
  status: 'ONLINE' | 'OFFLINE';
  boot_id?: string;
  mounts_revision?: number;
  fallback_agent_id?: number | null;
  expected_armed_digest?: string;
  armed_digest?: string;
  agent_version?: string;
  protocol_version?: string;
  kernel_version?: string;
  has_ebpf_cap?: boolean;
  cap_detail?: string;
  lease_type: 'RECORDING' | 'REPLAY' | '';
  lease_ref_id?: string;
  lease_holder_id?: string;
  lease_holder_name?: string;
  lease_acquired_at?: string;
  lease_expires_at?: string;
  last_heartbeat_at?: string;
  claim_code?: string;
  description?: string;
  processes_count?: number;
  processes?: Process[];
  created_at?: string;
  updated_at?: string;
}

export interface ProcessModuleBinding {
  process_id: string;
  module_id: string;
  module?: {
    id: string;
    code: string;
    name: string;
  };
}

export type ProbeStatus =
  | 'READY'
  | 'NEED_RECONNECT'
  | 'WAITING_PROC'
  | 'MONITORING'
  | 'DISARMED'
  | 'OFFLINE'
  | 'CAP_DENIED'
  | 'ERROR';

export interface Process {
  id: string;
  agent_id: string;
  arch_process_id?: string | number | null;
  arch_process?: ArchProcess;
  group_name?: string;
  name: string;
  listen_port: number;
  effective_listen_port?: number;
  override_listen_port?: number | null;
  advertise_ip?: string;
  cmd_pattern?: string;
  override_cmd_pattern?: string;
  probe_desired_state?: 'ARMED' | 'DISARMED';
  probe_status?: ProbeStatus;
  probe_status_reason?: string;
  module_ids?: string[];
  extra_downstreams?: string[];
  description?: string;
  agent?: Agent;
  process_modules?: ProcessModuleBinding[];
  created_at?: string;
  updated_at?: string;
}

export interface BatchProcessMountItem {
  arch_process_id: number | string;
  override_listen_port?: number | null;
  override_cmd_pattern?: string;
  advertise_ip?: string;
  extra_downstreams?: string[];
  probe_desired_state?: 'ARMED' | 'DISARMED' | string;
  description?: string;
}

export interface BatchProcessMountReq {
  expected_revision: number;
  mounts: BatchProcessMountItem[];
}

export interface DiscoveredProcess {
  pid: number;
  listen_port: number;
  cmdline: string;
  exe_name: string;
}

export interface HandshakeResp {
  agent_version: string;
  protocol_version: string;
  boot_id: string;
  started_at: string;
  kernel_version: string;
  has_ebpf_cap: boolean;
  cap_detail?: string;
  active_lease_type: string;
  active_ref_id: number;
  uptime_sec: number;
}
