import type { Process } from './agent';

export interface Subsystem {
  id: string;
  code: string;
  name: string;
  owner_id?: string;
  owner_name?: string;
  layer_level?: number;
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

export interface DownstreamConsumer {
  module_id: string | number;
  module_name: string;
  module_code: string;
  subsystem_name?: string;
}

export type ModuleKind = 'BUSINESS' | 'CONTRACT_LIB';
export type ContractProvideMode = 'EXCLUSIVE' | 'MULTI_IMPL';

export interface ContractImplementationItem {
  service_id: string | number;
  service_name: string;
  is_custom_service_name?: boolean;
  impl_key: string;
  module_id: string | number;
  module_code: string;
  module_name: string;
  subsystem_name?: string;
  arch_process_id?: string | number | null;
  arch_process_code?: string;
  arch_process_name?: string;
  owner?: string;
  created_at?: string;
}

export interface ContractDispatchConsumerItem {
  dependency_id: string | number;
  module_id: string | number;
  module_code: string;
  module_name: string;
  subsystem_name?: string;
  arch_process_id?: string | number | null;
  arch_process_code?: string;
  arch_process_name?: string;
  created_at?: string;
}

export interface ServiceContract {
  id: string | number;
  definer_module_id: string | number;
  definer_module_code?: string;
  definer_module_name?: string;
  service_name: string;
  provide_mode: ContractProvideMode;
  impl_key_label?: string;
  description?: string;
  definer_module?: Module;
  impl_count?: number;
  deployed_impl_count?: number;
  dispatch_consumer_count?: number;
  implementation_count?: number;
  implementations?: Array<ModuleService | ContractImplementationItem>;
  dispatch_consumers?: ContractDispatchConsumerItem[];
  dispatch_dependency_count?: number;
  dispatch_dependencies?: ModuleDependency[];
  consumer_dependencies?: ModuleDependency[];
  created_at?: string;
  updated_at?: string;
}

export interface ModuleService {
  id: string;
  module_id: string;
  module?: Module;
  service_name: string;
  contract_id?: string | number | null;
  impl_key?: string | null;
  is_custom_service_name?: boolean;
  contract_service_name?: string;
  contract?: ServiceContract;
  diff_ignore_paths?: string[];
  description?: string;
  downstream_count?: number;
  downstream_modules?: string[];
  downstream_consumers?: DownstreamConsumer[];
  created_at?: string;
  updated_at?: string;
}

export interface ModuleDependency {
  id: string;
  consumer_module_id: string;
  consumer_module?: Module;
  module?: Module;
  is_external: boolean;
  external_system?: string;
  external_service?: string;
  service_id?: string | null;
  service?: ModuleService;
  contract_id?: string | number | null;
  contract?: ServiceContract;
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
    arch_process_id?: string | number | null;
    arch_process?: ArchProcess;
  };
  description?: string;
  created_at?: string;
  updated_at?: string;
}

export interface AvailableServiceItem {
  service_id: string;
  service_name: string;
  contract_service_name?: string;
  is_custom_service_name?: boolean;
  module_id: string;
  module_code: string;
  module_name: string;
  subsystem_id: string;
  subsystem_name: string;
  contract_id?: string | number | null;
  is_multi_impl?: boolean;
  impl_key?: string | null;
  impl_key_label?: string;
  description?: string;
}

export interface Module {
  id: string;
  subsystem_id: string;
  group_id: string;
  code: string;
  name: string;
  module_kind?: ModuleKind;
  layer_level?: number;
  owner_id?: string;
  owner_name?: string;
  process_id?: string | null;
  arch_process_id?: string | number | null;
  arch_process?: ArchProcess;
  description?: string;
  subsystem?: Subsystem;
  group?: FunctionalGroup;
  contract_count?: number;
  impl_count?: number;
  service_contracts?: ServiceContract[];
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

export interface ArchProcessGroup {
  id: string | number;
  code: string;
  name: string;
  sort_order: number;
  description?: string;
  process_count?: number;
  processes?: ArchProcess[];
  created_at?: string;
  updated_at?: string;
}

export interface ArchProcess {
  id: string | number;
  group_id: string | number;
  code: string;
  name: string;
  default_listen_port: number;
  default_cmd_pattern: string;
  default_extra_downstreams?: string[];
  sort_order: number;
  description?: string;
  group?: ArchProcessGroup;
  modules?: Module[];
  mount_count?: number;
  created_at?: string;
  updated_at?: string;
}

export interface AutoPinnedOverride {
  process_id: number;
  agent_id: number;
  override_listen_port: number;
}

export interface ArchProcessDetailDTO {
  arch_process: ArchProcess;
  modules: Module[];
  provided_services: ModuleService[];
  dependencies: ModuleDependency[];
  agent_mounts: Process[];
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
  is_multi_impl?: boolean;
  contract_id?: string | number | null;
  impl_key_label?: string;
}

export type {
  SavedNodePosition,
  SavedExpandState,
  SavedFilterState,
  SavedViewport,
  TopologyLayoutSummaryDTO,
  TopologyLayoutDetailDTO,
  TopologyLayoutListResponse,
  NeighborhoodDirection,
  NeighborhoodHop,
} from './topology_graph';

