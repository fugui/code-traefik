import type {
  GraphNode,
  GraphEdge,
  ExpandStateMap,
  RollupEdgeModel,
  ConsistencyEdgeStatus,
  ContainerBadgeCounts,
} from '../../../types/topology_graph';
import type { ConsistencyReportDTO, ConsistencyEdge } from '../../../types/traffic';
import {
  synthesizeOverlay,
  mergeStatus,
  STATUS_PRIORITY,
  type OverlaySynthesisResult,
} from './overlay';

export { mergeStatus, STATUS_PRIORITY };

/**
 * 代表节点查找 rep(nodeId)
 * 沿当前节点的 ancestors[] 路径从顶层根（depth=0）向下扫描，首个存在于 nodeMap 且处于“折叠 (Collapsed)”状态的容器即为其可见代表节点；
 * 若其所有祖先均处于展开状态（或在聚焦裁剪下不在 nodeMap 中），则该叶子节点自身即为代表节点。
 */
export function rep(
  nodeId: string,
  expandState: ExpandStateMap,
  nodeMap: Map<string, GraphNode>
): string {
  const node = nodeMap.get(nodeId);
  if (!node) return nodeId;

  for (const ancId of node.ancestors) {
    if (!nodeMap.has(ancId)) continue;
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

export interface OverlayEdgeMeta {
  edgeStatusMap?: Map<string, ConsistencyEdgeStatus>;
  edgeCallCountMap?: Map<string, number>;
  edgeSampleRecordMap?: Map<string, number>;
  dynamicNodes?: GraphNode[];
  unattributedPendingList?: ConsistencyEdge[];
  repOverrides?: ReadonlyMap<string, string>;
}

/**
 * M19/M21 Stage-3: 纯代表节点上卷与容器徽章聚合
 * 支持单层平铺 repOverrides 映射，将所有深层后代边精确上卷至当前层直接子代表元或统计入容器内部角标。
 */
export function aggregateRollup(
  nodes: GraphNode[],
  workingEdges: GraphEdge[],
  expandState: ExpandStateMap,
  overlayMeta?: OverlayEdgeMeta,
  repOverridesArg?: ReadonlyMap<string, string>
): Pick<RollupResult, 'rollupEdges' | 'badgeCounts'> {
  const repOverrides = repOverridesArg ?? overlayMeta?.repOverrides;
  const nodeMap = new Map<string, GraphNode>();
  const nodeIdxMap = new Map<string, number>();
  const badgeCounts: Record<string, ContainerBadgeCounts> = {};
  for (let i = 0; i < nodes.length; i++) {
    const n = nodes[i];
    nodeMap.set(n.id, n);
    nodeIdxMap.set(n.id, i + 1);
    if (
      n.type !== 'MODULE' &&
      n.type !== 'EXTERNAL_SYSTEM' &&
      n.type !== 'EXTERNAL_ENDPOINT' &&
      n.type !== 'UNREGISTERED_INTERFACE' &&
      n.type !== 'EXTERNAL_CONTEXT'
    ) {
      badgeCounts[n.id] = { internalEdgeCount: 0, intraProcessCallCount: 0 };
    }
  }

  const edgeStatusMap = overlayMeta?.edgeStatusMap;
  const edgeCallCountMap = overlayMeta?.edgeCallCountMap;
  const edgeSampleRecordMap = overlayMeta?.edgeSampleRecordMap;

  const rollupEdgeMap = new Map<number, RollupEdgeModel>();
  const rollupServiceSetMap = new Map<number, Set<string>>();
  const repCache = new Map<string, string>();
  let dynSeq = nodes.length + 1;

  const resolveRepCached = (nodeId: string): string => {
    const cached = repCache.get(nodeId);
    if (cached !== undefined) return cached;
    const overridden = repOverrides?.get(nodeId);
    const resolved = overridden !== undefined ? overridden : rep(nodeId, expandState, nodeMap);
    repCache.set(nodeId, resolved);
    if (!nodeIdxMap.has(resolved)) {
      nodeIdxMap.set(resolved, dynSeq++);
    }
    return resolved;
  };

  for (const edge of workingEdges) {
    if (repOverrides && (!repOverrides.has(edge.source_node_id) || !repOverrides.has(edge.target_node_id))) {
      continue;
    }
    const sRep = resolveRepCached(edge.source_node_id);
    const tRep = resolveRepCached(edge.target_node_id);

    if (sRep === tRep) {
      if (edge.source_node_id !== edge.target_node_id) {
        let badge = badgeCounts[sRep];
        if (!badge) {
          badge = { internalEdgeCount: 0, intraProcessCallCount: 0 };
          badgeCounts[sRep] = badge;
        }
        badge.internalEdgeCount += 1;
        if (edge.kind === 'INTRA_PROCESS') {
          badge.intraProcessCallCount += 1;
        }
      }
    } else {
      const sIdx = nodeIdxMap.get(sRep)!;
      const tIdx = nodeIdxMap.get(tRep)!;
      const isRealization = edge.kind === 'REALIZATION';
      const rollupKeyNum = isRealization ? -(sIdx * 65536 + tIdx) : sIdx * 65536 + tIdx;
      const status = isRealization ? undefined : edgeStatusMap?.get(edge.id);
      const calls = isRealization ? 0 : edgeCallCountMap?.get(edge.id) || 0;
      const sampleId = isRealization ? undefined : edgeSampleRecordMap?.get(edge.id);

      const existing = rollupEdgeMap.get(rollupKeyNum);
      if (!existing) {
        const svcSet = new Set<string>();
        for (const s of edge.services) {
          svcSet.add(s.service_name);
        }
        rollupServiceSetMap.set(rollupKeyNum, svcSet);
        rollupEdgeMap.set(rollupKeyNum, {
          id: `rollup_${isRealization ? 'realization_' : ''}${sRep}_${tRep}`,
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
        if (!isRealization) {
          existing.status = mergeStatus(existing.status, status);
          existing.totalCalls = (existing.totalCalls || 0) + calls;
          if (!existing.sampleTrafficRecordId && sampleId) {
            existing.sampleTrafficRecordId = sampleId;
          }
        }

        const svcSet = rollupServiceSetMap.get(rollupKeyNum)!;
        for (const s of edge.services) {
          if (!svcSet.has(s.service_name)) {
            svcSet.add(s.service_name);
            existing.services.push(s);
          }
        }
      }
    }
  }

  return {
    rollupEdges: Array.from(rollupEdgeMap.values()),
    badgeCounts,
  };
}

/**
 * 向后兼容门面函数：串联 synthesizeOverlay 与 aggregateRollup
 */
export function rollupEdges(
  nodes: GraphNode[],
  edges: GraphEdge[],
  expandState: ExpandStateMap,
  consistencyReport?: ConsistencyReportDTO | null,
  view: 'LOGICAL' | 'PHYSICAL' = 'LOGICAL'
): RollupResult {
  const overlay: OverlaySynthesisResult = synthesizeOverlay(nodes, edges, consistencyReport, view);
  const aggregated = aggregateRollup(overlay.workingNodes, overlay.workingEdges, expandState, overlay);

  return {
    rollupEdges: aggregated.rollupEdges,
    badgeCounts: aggregated.badgeCounts,
    dynamicNodes: overlay.dynamicNodes,
    unattributedPendingList: overlay.unattributedPendingList,
  };
}
