import type {
  GraphNode,
  GraphEdge,
  ExpandStateMap,
  RollupEdgeModel,
  ConsistencyEdgeStatus,
  ContainerBadgeCounts,
} from '../../../types/topology_graph';
import type { ConsistencyReportDTO, ConsistencyEdge } from '../../../types/traffic';
import { findLCA } from './hierarchy';

export const STATUS_PRIORITY: Record<ConsistencyEdgeStatus, number> = {
  VIOLATION: 6,
  UNATTRIBUTED: 5,
  UNCOVERED: 4,
  NOT_MONITORED: 3,
  NOT_OBSERVABLE: 2,
  VERIFIED: 1,
};

/**
 * 六分类最差状态优先级合并
 * VIOLATION > UNATTRIBUTED > UNCOVERED > NOT_MONITORED > NOT_OBSERVABLE > VERIFIED
 */
export function mergeStatus(
  a?: ConsistencyEdgeStatus,
  b?: ConsistencyEdgeStatus
): ConsistencyEdgeStatus | undefined {
  if (!a) return b;
  if (!b) return a;
  return STATUS_PRIORITY[a] >= STATUS_PRIORITY[b] ? a : b;
}

/**
 * 代表节点查找 rep(nodeId)
 * 沿当前节点的 ancestors[] 路径从顶层根（depth=0）向下扫描，首个处于“折叠 (Collapsed)”状态的容器即为其可见代表节点；
 * 若其所有祖先均处于展开状态，则该叶子节点自身即为代表节点。
 */
export function rep(
  nodeId: string,
  expandState: ExpandStateMap,
  nodeMap: Map<string, GraphNode>
): string {
  const node = nodeMap.get(nodeId);
  if (!node) return nodeId;

  for (const ancId of node.ancestors) {
    if (!expandState[ancId]) {
      return ancId;
    }
  }

  return nodeId;
}

export interface RollupResult {
  rollupEdges: RollupEdgeModel[];
  badgeCounts: Record<string, ContainerBadgeCounts>;
  dynamicNodes: GraphNode[];
  unattributedPendingList: ConsistencyEdge[];
}

/**
 * 纯前端边汇总与动态 VIOLATION/UNATTRIBUTED 叠加引擎
 */
export function rollupEdges(
  nodes: GraphNode[],
  edges: GraphEdge[],
  expandState: ExpandStateMap,
  consistencyReport?: ConsistencyReportDTO | null,
  view: 'LOGICAL' | 'PHYSICAL' = 'LOGICAL'
): RollupResult {
  const nodeMap = new Map<string, GraphNode>();
  for (const n of nodes) {
    nodeMap.set(n.id, n);
  }

  const dynamicNodes: GraphNode[] = [];
  const workingEdges: GraphEdge[] = [...edges];
  const edgeStatusMap = new Map<string, ConsistencyEdgeStatus>();
  const edgeCallCountMap = new Map<string, number>();
  const edgeSampleRecordMap = new Map<string, number>();
  const unattributedPendingList: ConsistencyEdge[] = [];

  const ensureUnregisteredInterfaceNode = () => {
    const virtId = 'VIRTUAL_UNREGISTERED_INTERFACE';
    if (!nodeMap.has(virtId) && !dynamicNodes.some((n) => n.id === virtId)) {
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

  // 1. 若挂载了一致性报告，执行动态违规边合成与静态边状态映射
  if (consistencyReport && consistencyReport.edges) {
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
      } else {
        // 静态边匹配 (VERIFIED / UNCOVERED / NOT_MONITORED / NOT_OBSERVABLE)
        for (const e of workingEdges) {
          const matchSrc = srcModuleId != null && e.src_module_id === Number(srcModuleId);
          const matchDst = dstModuleId != null && e.dst_module_id === Number(dstModuleId);
          const matchSvc = e.services.some(
            (s) =>
              s.service_name === item.service_name ||
              s.service_name === `${item.service_name}.${item.operation}` ||
              s.service_name.includes(item.service_name)
          );

          if (matchSrc && matchDst && matchSvc) {
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

  // 2. 边上卷与代表节点聚合
  const badgeCounts: Record<string, ContainerBadgeCounts> = {};
  for (const n of nodes) {
    badgeCounts[n.id] = { internalEdgeCount: 0, intraProcessCallCount: 0 };
  }
  for (const n of dynamicNodes) {
    badgeCounts[n.id] = { internalEdgeCount: 0, intraProcessCallCount: 0 };
  }

  const rollupEdgeMap = new Map<string, RollupEdgeModel>();

  for (const edge of workingEdges) {
    const sRep = rep(edge.source_node_id, expandState, nodeMap);
    const tRep = rep(edge.target_node_id, expandState, nodeMap);

    if (sRep === tRep) {
      if (!badgeCounts[sRep]) {
        badgeCounts[sRep] = { internalEdgeCount: 0, intraProcessCallCount: 0 };
      }
      badgeCounts[sRep].internalEdgeCount += 1;
      if (edge.kind === 'INTRA_PROCESS') {
        badgeCounts[sRep].intraProcessCallCount += 1;
      }
    } else {
      const rollupKey = `${sRep}->${tRep}`;
      const status = edgeStatusMap.get(edge.id);
      const calls = edgeCallCountMap.get(edge.id) || 0;
      const sampleId = edgeSampleRecordMap.get(edge.id);

      const existing = rollupEdgeMap.get(rollupKey);
      if (!existing) {
        rollupEdgeMap.set(rollupKey, {
          id: `rollup_${sRep}_${tRep}`,
          source: sRep,
          target: tRep,
          kind: edge.kind,
          services: [...edge.services],
          rawEdges: [edge],
          status,
          totalCalls: calls,
          sampleTrafficRecordId: sampleId,
        });
      } else {
        existing.rawEdges.push(edge);
        existing.status = mergeStatus(existing.status, status);
        existing.totalCalls = (existing.totalCalls || 0) + calls;
        if (!existing.sampleTrafficRecordId && sampleId) {
          existing.sampleTrafficRecordId = sampleId;
        }

        for (const s of edge.services) {
          if (!existing.services.some((es) => es.service_name === s.service_name)) {
            existing.services.push(s);
          }
        }
      }
    }
  }

  return {
    rollupEdges: Array.from(rollupEdgeMap.values()),
    badgeCounts,
    dynamicNodes,
    unattributedPendingList,
  };
}
