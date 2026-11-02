export interface Agent {
  id: string;
  name: string;
  endpoint: string;
  scope: 'SHARED' | 'PRIVATE';
  owner_id?: string;
  owner_name?: string;
  status: 'ONLINE' | 'OFFLINE';
  boot_id?: string;
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

export interface Process {
  id: string;
  agent_id: string;
  group_name?: string;
  name: string;
  listen_port: number;
  advertise_ip?: string;
  cmd_pattern?: string;
  module_ids?: string[];
  extra_downstreams?: string[];
  description?: string;
  agent?: Agent;
  process_modules?: ProcessModuleBinding[];
  created_at?: string;
  updated_at?: string;
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
