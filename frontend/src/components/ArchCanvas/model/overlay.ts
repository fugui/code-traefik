import type {
  GraphNode,
  GraphEdge,
  ConsistencyEdgeStatus,
} from '../../../types/topology_graph';
import type { ConsistencyReportDTO, ConsistencyEdge } from '../../../types/traffic';
import { findLCA } from './hierarchy';

export const STATUS_PRIORITY: Record<ConsistencyEdgeStatus, number> = {
  VIOLATION: 7,
  UNATTRIBUTED: 6,
  UNCOVERED: 5,
  NOT_MONITORED: 4,
  OUTBOUND_VERIFIED: 3,
  NOT_OBSERVABLE: 2,
  VERIFIED: 1,
};

/**
 * 七分类最差状态优先级合并
 * VIOLATION > UNATTRIBUTED > UNCOVERED > NOT_MONITORED > OUTBOUND_VERIFIED > NOT_OBSERVABLE > VERIFIED
 */
export function mergeStatus(
  a?: ConsistencyEdgeStatus,
  b?: ConsistencyEdgeStatus
): ConsistencyEdgeStatus | undefined {
  if (!a) return b;
  if (!b) return a;
  return STATUS_PRIORITY[a] >= STATUS_PRIORITY[b] ? a : b;
}

export interface OverlaySynthesisResult {
  workingNodes: GraphNode[];
  dynamicNodes: GraphNode[];
  workingEdges: GraphEdge[];
  edgeStatusMap: Map<string, ConsistencyEdgeStatus>;
  edgeCallCountMap: Map<string, number>;
  edgeSampleRecordMap: Map<string, number>;
  unattributedPendingList: ConsistencyEdge[];
}

/**
 * M19 Stage-1: 一致性报告叠加与动态 VIOLATION / UNATTRIBUTED 边合成
 * 预构建 (src_module_id, dst_module_id) -> GraphEdge[] 索引，将静态边匹配从 O(|E|*|R|) 降为 O(|E|+|R|)
 */
export function synthesizeOverlay(
  nodes: GraphNode[],
  edges: GraphEdge[],
  consistencyReport?: ConsistencyReportDTO | null,
  view: 'LOGICAL' | 'PHYSICAL' = 'LOGICAL'
): OverlaySynthesisResult {
  const nodeMap = new Map<string, GraphNode>();
  for (const n of nodes) {
    nodeMap.set(n.id, n);
  }

  const dynamicNodes: GraphNode[] = [];
  const workingEdges: GraphEdge[] =
    consistencyReport && consistencyReport.edges && consistencyReport.edges.length > 0
      ? [...edges]
      : edges;
  const edgeStatusMap = new Map<string, ConsistencyEdgeStatus>();
  const edgeCallCountMap = new Map<string, number>();
  const edgeSampleRecordMap = new Map<string, number>();
  const unattributedPendingList: ConsistencyEdge[] = [];

  const ensureUnregisteredInterfaceNode = () => {
    const virtId = 'VIRTUAL_UNREGISTERED_INTERFACE';
    if (!nodeMap.has(virtId)) {
      const virtNode: GraphNode = {
        id: virtId,
        type: 'UNREGISTERED_INTERFACE',
        name: '未登记接口',
        depth: 0,
        ancestors: [],
        sort_order: 99999,
        meta: { description: '动态检出的未登记外部接口告警节点' },
      };
      dynamicNodes.push(virtNode);
      nodeMap.set(virtId, virtNode);
    }
  };

  if (consistencyReport && consistencyReport.edges && consistencyReport.edges.length > 0) {
    // 仅对报告中出现的 (src_module_id, dst_module_id) 建立数字键桶，避免为全量 10k 静态边分配字符串与数组
    const neededPairs = new Set<number>();
    for (const item of consistencyReport.edges) {
      const st = (item.status || item.category) as ConsistencyEdgeStatus;
      if (st !== 'VIOLATION' && st !== 'UNATTRIBUTED') {
        const sId = item.src_module_id ?? item.source_module_id;
        const dId = item.dst_module_id ?? item.target_module_id;
        if (sId != null && dId != null) {
          neededPairs.add(Number(sId) * 1000000 + Number(dId));
        }
      }
    }

    const staticEdgeByModulePair = new Map<number, GraphEdge[]>();
    if (neededPairs.size > 0) {
      for (const e of edges) {
        if (e.src_module_id != null && e.dst_module_id != null) {
          const pairKey = Number(e.src_module_id) * 1000000 + Number(e.dst_module_id);
          if (neededPairs.has(pairKey)) {
            const bucket = staticEdgeByModulePair.get(pairKey);
            if (bucket) {
              bucket.push(e);
            } else {
              staticEdgeByModulePair.set(pairKey, [e]);
            }
          }
        }
      }
    }

    for (const item of consistencyReport.edges) {
      const status = (item.status || item.category) as ConsistencyEdgeStatus;
      const callCount = Number(item.call_count || item.observed_call_count || 0);
      const sampleId = item.sample_traffic_record_id ? Number(item.sample_traffic_record_id) : undefined;

      const srcModuleId = item.src_module_id ?? item.source_module_id;
      const dstModuleId = item.dst_module_id ?? item.target_module_id;

      if (status === 'VIOLATION') {
        let srcNodeId: string | null = null;
        if (srcModuleId != null) {
          srcNodeId = `mod_${srcModuleId}`;
        } else if (view === 'PHYSICAL' && item.src_process_id != null && nodeMap.has(`proc_${item.src_process_id}`)) {
          srcNodeId = `proc_${item.src_process_id}`;
        } else if (item.candidate_module_ids && item.candidate_module_ids.length > 0) {
          const candIds = item.candidate_module_ids.map((id: string | number) => `mod_${id}`);
          srcNodeId = findLCA(candIds, nodeMap) || candIds[0];
        }

        if (!srcNodeId) {
          srcNodeId = 'UNATTRIBUTED_ROOT';
        }

        let dstNodeId: string;
        if (dstModuleId != null) {
          dstNodeId = `mod_${dstModuleId}`;
        } else {
          ensureUnregisteredInterfaceNode();
          dstNodeId = 'VIRTUAL_UNREGISTERED_INTERFACE';
        }

        const dynEdgeId = `dyn_viol_${srcNodeId}_${dstNodeId}_${item.service_name}_${item.operation}`;
        const dynEdge: GraphEdge = {
          id: dynEdgeId,
          kind: 'CROSS_PROCESS',
          source_node_id: srcNodeId,
          target_node_id: dstNodeId,
          src_module_id: srcModuleId != null ? Number(srcModuleId) : undefined,
          dst_module_id: dstModuleId != null ? Number(dstModuleId) : undefined,
          services: [
            {
              service_name: `${item.service_name}.${item.operation}`,
              description: item.detail || '未声明越权调用 (VIOLATION)',
            },
          ],
        };

        workingEdges.push(dynEdge);
        edgeStatusMap.set(dynEdgeId, 'VIOLATION');
        edgeCallCountMap.set(dynEdgeId, callCount);
        if (sampleId) edgeSampleRecordMap.set(dynEdgeId, sampleId);
      } else if (status === 'UNATTRIBUTED') {
        let srcNodeId: string | null = null;
        if (view === 'PHYSICAL') {
          if (item.src_process_id != null && nodeMap.has(`proc_${item.src_process_id}`)) {
            srcNodeId = `proc_${item.src_process_id}`;
          } else if (item.candidate_module_ids && item.candidate_module_ids.length > 0) {
            const candIds = item.candidate_module_ids.map((id: string | number) => `mod_${id}`);
            srcNodeId = findLCA(candIds, nodeMap);
          }
        } else {
          if (item.candidate_module_ids && item.candidate_module_ids.length > 0) {
            const candIds = item.candidate_module_ids.map((id: string | number) => `mod_${id}`);
            srcNodeId = findLCA(candIds, nodeMap);
          }
        }

        if (srcNodeId) {
          let dstNodeId: string;
          if (dstModuleId != null) {
            dstNodeId = `mod_${dstModuleId}`;
          } else {
            ensureUnregisteredInterfaceNode();
            dstNodeId = 'VIRTUAL_UNREGISTERED_INTERFACE';
          }

          const dynEdgeId = `dyn_unattr_${srcNodeId}_${dstNodeId}_${item.service_name}_${item.operation}`;
          const dynEdge: GraphEdge = {
            id: dynEdgeId,
            kind: 'CROSS_PROCESS',
            source_node_id: srcNodeId,
            target_node_id: dstNodeId,
            dst_module_id: dstModuleId != null ? Number(dstModuleId) : undefined,
            services: [
              {
                service_name: `${item.service_name}.${item.operation}`,
                description: item.detail || '多模块共进程待定归属 (UNATTRIBUTED)',
              },
            ],
          };

          workingEdges.push(dynEdge);
          edgeStatusMap.set(dynEdgeId, 'UNATTRIBUTED');
          edgeCallCountMap.set(dynEdgeId, callCount);
          if (sampleId) edgeSampleRecordMap.set(dynEdgeId, sampleId);
        } else {
          unattributedPendingList.push(item);
        }
      } else if (srcModuleId != null && dstModuleId != null) {
        const pairKey = Number(srcModuleId) * 1000000 + Number(dstModuleId);
        const candidates = staticEdgeByModulePair.get(pairKey);
        if (candidates) {
          for (const e of candidates) {
            const matchSvc = e.services.some(
              (s) =>
                s.service_name === item.service_name ||
                s.service_name === `${item.service_name}.${item.operation}` ||
                s.service_name.includes(item.service_name)
            );

            if (matchSvc) {
              const currStatus = edgeStatusMap.get(e.id);
              edgeStatusMap.set(e.id, mergeStatus(currStatus, status) || status);
              edgeCallCountMap.set(e.id, (edgeCallCountMap.get(e.id) || 0) + callCount);
              if (sampleId && !edgeSampleRecordMap.has(e.id)) {
                edgeSampleRecordMap.set(e.id, sampleId);
              }
            }
          }
        }
      }
    }
  }

  return {
    workingNodes: dynamicNodes.length > 0 ? [...nodes, ...dynamicNodes] : nodes,
    dynamicNodes,
    workingEdges,
    edgeStatusMap,
    edgeCallCountMap,
    edgeSampleRecordMap,
    unattributedPendingList,
  };
}
