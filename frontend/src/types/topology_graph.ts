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
  | 'UNREGISTERED_INTERFACE'
  | 'EXTERNAL_CONTEXT';

export type NeighborhoodDirection = 'BOTH' | 'UPSTREAM' | 'DOWNSTREAM';
export type NeighborhoodHop = 0 | 1 | 2;

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
  direction?: 'UPSTREAM' | 'DOWNSTREAM';
  peer_root_id?: string;
  peer_root_name?: string;
  hidden_module_count?: number;
  cross_edge_count?: number;
  violation_count?: number;
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
  contract_id?: number;
  impl_key?: string;
  dependency_id?: number;
  description?: string;
}

export type EdgeKind =
  | 'CROSS_PROCESS'
  | 'INTRA_PROCESS'
  | 'EXTERNAL_UNREGISTERED'
  | 'UNDEPLOYED'
  | 'REALIZATION';

export interface GraphEdge {
  id: string;
  kind: EdgeKind;
  source_node_id: string;
  target_node_id: string;
  src_module_id?: number;
  dst_module_id?: number;
  src_process_id?: number;
  dst_process_id?: number;
  contract_id?: number;
  impl_key?: string;
  definer_module_id?: number;
  services: EdgeServiceItem[];
  external_system?: string;
}

export interface TopologyGraphDTO {
  view: 'LOGICAL' | 'PHYSICAL';
  model_version: string;
  structure_version?: string;
  nodes: GraphNode[];
  edges: GraphEdge[];
}

export type ConsistencyEdgeStatus =
  | 'VERIFIED'
  | 'OUTBOUND_VERIFIED'
  | 'VIOLATION'
  | 'UNATTRIBUTED'
  | 'UNCOVERED'
  | 'NOT_MONITORED'
  | 'NOT_OBSERVABLE'
  | 'IMPL_UNRESOLVED';

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
  isHubSummary?: boolean;
  hubDirection?: 'OUTBOUND' | 'INBOUND';
  hubPeerIds?: string[];
  foldedPeerCount?: number;
}

export type ExpandStateMap = Record<string, boolean>;

export interface ContainerBadgeCounts {
  internalEdgeCount: number;
  intraProcessCallCount: number;
}

export interface SavedNodePosition {
  x: number;
  y: number;
  parent_id?: string | null;
  is_expanded: boolean;
  pinned?: boolean;
  width?: number;
  height?: number;
}

export interface SavedExpandState {
  depth_level: number;
  expanded_node_ids: string[];
  collapsed_node_ids?: string[];
}

export interface SavedFilterState {
  neighborhood_hop: number;
  neighborhood_direction: NeighborhoodDirection;
  owner_id?: number | null;
  consistency_only: boolean;
  hide_isolated: boolean;
  fold_hub_edges: boolean;
}

export interface SavedViewport {
  x: number;
  y: number;
  zoom: number;
}

export interface TopologyLayoutSummaryDTO {
  id: number;
  scope: 'SHARED' | 'PRIVATE';
  view: 'LOGICAL' | 'PHYSICAL';
  owner_id?: number | null;
  name: string;
  description: string;
  focus_scope: string;
  is_default: boolean;
  schema_version: number;
  structure_version: string;
  revision: number;
  node_count: number;
  created_by: number;
  updated_by: number;
  created_at: string;
  updated_at: string;
}

export interface TopologyLayoutDetailDTO extends Omit<TopologyLayoutSummaryDTO, 'node_count'> {
  node_positions: Record<string, SavedNodePosition>;
  expand_state: SavedExpandState;
  filter_state: SavedFilterState;
  viewport: SavedViewport;
}

export interface TopologyLayoutListResponse {
  canonical: TopologyLayoutDetailDTO | null;
  personal_items: TopologyLayoutSummaryDTO[];
  quota: {
    used: number;
    limit: number;
  };
}

