export interface Subsystem {
  id: string;
  code: string;
  name: string;
  owner_id?: string;
  owner_name?: string;
  description?: string;
  module_count?: number;
  created_at?: string;
  updated_at?: string;
}

export interface FunctionalGroupTreeNode {
  id: string;
  subsystem_id: string;
  parent_id?: string | null;
  code: string;
  name: string;
  sort_order: number;
  level: number;
  path: string;
  description?: string;
  module_count: number;
  children?: FunctionalGroupTreeNode[];
}

export interface FunctionalGroup {
  id: string;
  subsystem_id: string;
  parent_id?: string | null;
  code: string;
  name: string;
  sort_order: number;
  level: number;
  path: string;
  description?: string;
  created_at?: string;
  updated_at?: string;
}

export interface ModuleService {
  id: string;
  module_id: string;
  service_name: string;
  diff_ignore_paths?: string[];
  description?: string;
  downstream_count?: number;
  downstream_modules?: string[];
  created_at?: string;
  updated_at?: string;
}

export interface ModuleDependency {
  id: string;
  consumer_module_id: string;
  is_external: boolean;
  external_system?: string;
  external_service?: string;
  service_id?: string | null;
  service?: ModuleService;
  provider_module_id?: string | null;
  provider_module?: {
    id: string;
    code: string;
    name: string;
    subsystem_id?: string;
    subsystem?: Subsystem;
    process_id?: string | null;
    process?: {
      id: string;
      name: string;
      listen_port: number;
    };
  };
  description?: string;
  created_at?: string;
  updated_at?: string;
}

export interface AvailableServiceItem {
  service_id: string;
  service_name: string;
  module_id: string;
  module_code: string;
  module_name: string;
  subsystem_id: string;
  subsystem_name: string;
  description?: string;
}

export interface Module {
  id: string;
  subsystem_id: string;
  group_id: string;
  code: string;
  name: string;
  owner_id?: string;
  owner_name?: string;
  process_id?: string | null;
  description?: string;
  subsystem?: Subsystem;
  group?: FunctionalGroup;
  process?: {
    id: string;
    name: string;
    listen_port: number;
    agent_id: string;
    cmd_pattern?: string;
    agent?: {
      id: string;
      name: string;
      endpoint: string;
      scope: 'SHARED' | 'PRIVATE';
      status: string;
    };
  };
  services_count?: number;
  services?: ModuleService[];
  dependencies_count?: number;
  dependencies?: ModuleDependency[];
  created_at?: string;
  updated_at?: string;
}

export interface StaticTopologyNode {
  id: string;
  name: string;
  code?: string;
  node_type?: string;
  kind?: string;
  subsystem_id?: string;
  subsystem_name?: string;
  process_id?: string | number;
  process_name?: string;
  listen_port?: number;
  is_external?: boolean;
}

export interface StaticTopologyEdge {
  id: string;
  source?: string;
  target?: string;
  source_node_id?: string;
  target_node_id?: string;
  service_name: string;
  operation: string;
  edge_kind?: 'CROSS_PROCESS' | 'INTRA_PROCESS' | 'EXTERNAL_UNREGISTERED';
  kind?: 'CROSS_PROCESS' | 'INTRA_PROCESS' | 'EXTERNAL_UNREGISTERED';
}

export interface StaticTopologyDTO {
  view: 'LOGICAL' | 'PHYSICAL';
  nodes: StaticTopologyNode[];
  edges: StaticTopologyEdge[];
}

export interface ClaimedByModuleInfo {
  id: string;
  code: string;
  name: string;
}

export interface ClaimableProtoServiceItem {
  service_name: string;
  proto_package: string;
  proto_service: string;
  schema_id: number;
  schema_name: string;
  operation_count: number;
  operations: string[];
  is_claimed: boolean;
  claimed_by_module?: ClaimedByModuleInfo | null;
}
