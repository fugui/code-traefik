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

export interface ModuleInterface {
  id: string;
  module_id: string;
  relation_type: 'PROVIDE' | 'DEPEND';
  service_name: string;
  operation: string;
  diff_ignore_paths?: string[];
  description?: string;
  created_at?: string;
  updated_at?: string;
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
  };
  interfaces_count?: number;
  interfaces?: ModuleInterface[];
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
