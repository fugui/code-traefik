export type GraphNodeType =
  | 'SUBSYSTEM'
  | 'FUNCTIONAL_GROUP'
  | 'MODULE'
  | 'EXTERNAL_CONTAINER'
  | 'EXTERNAL_SYSTEM'
  | 'PROCESS_GROUP'
  | 'PROCESS'
  | 'UNDEPLOYED_CONTAINER'
  | 'EXTERNAL_ENDPOINT'
  | 'UNREGISTERED_INTERFACE';

export interface GraphNodeMeta {
  endpoint?: string;
  listen_port?: number;
  cmd_pattern?: string;
  description?: string;
  is_deployed?: boolean;
  provide_count?: number;
  owner_id?: number;
  owner_name?: string;
  level?: number;
  subsystem_id?: number;
  group_id?: number;
  process_id?: number;
  group_name?: string;
  module_count?: number;
  [key: string]: string | number | boolean | undefined | null;
}

export interface GraphNode {
  id: string;
  type: GraphNodeType;
  parent_id?: string;
  ancestors: string[];
  depth: number;
  entity_id?: number;
  code?: string;
  name: string;
  sort_order: number;
  meta?: GraphNodeMeta;
}

export interface EdgeServiceItem {
  service_id?: number;
  service_name: string;
  dependency_id?: number;
  description?: string;
}

export type EdgeKind =
  | 'CROSS_PROCESS'
  | 'INTRA_PROCESS'
  | 'EXTERNAL_UNREGISTERED'
  | 'UNDEPLOYED';

export interface GraphEdge {
  id: string;
  kind: EdgeKind;
  source_node_id: string;
  target_node_id: string;
  src_module_id?: number;
  dst_module_id?: number;
  src_process_id?: number;
  dst_process_id?: number;
  services: EdgeServiceItem[];
  external_system?: string;
}

export interface TopologyGraphDTO {
  view: 'LOGICAL' | 'PHYSICAL';
  model_version: string;
  nodes: GraphNode[];
  edges: GraphEdge[];
}

export type ConsistencyEdgeStatus =
  | 'VERIFIED'
  | 'VIOLATION'
  | 'UNATTRIBUTED'
  | 'UNCOVERED'
  | 'NOT_MONITORED'
  | 'NOT_OBSERVABLE';

export interface BendPoint {
  x: number;
  y: number;
}

export interface RollupEdgeModel {
  [key: string]: unknown;
  id: string;
  source: string;
  target: string;
  kind: EdgeKind | string;
  services: EdgeServiceItem[];
  rawEdges: GraphEdge[];
  status?: ConsistencyEdgeStatus;
  isCycle?: boolean;
  totalCalls?: number;
  sampleTrafficRecordId?: number;
  bendPoints?: BendPoint[];
  isDraggedDegraded?: boolean;
}

export type ExpandStateMap = Record<string, boolean>;

export interface ContainerBadgeCounts {
  internalEdgeCount: number;
  intraProcessCallCount: number;
}
