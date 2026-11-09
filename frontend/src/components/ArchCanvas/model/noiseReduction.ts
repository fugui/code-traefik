import type {
  GraphNode,
  RollupEdgeModel,
  EdgeServiceItem,
} from '../../../types/topology_graph';
import { mergeStatus } from './overlay';

export const HUB_FANOUT_THRESHOLD = 8;
export const HUB_TOP_K_EDGES = 3;
export const LOD_COMPACT_ZOOM = 0.45;
export const LOD_DETAIL_ZOOM = 0.85;

export interface FoldHubEdgesOptions {
  foldHubEdges?: boolean;
  expandedHubNodeIds?: Set<string>;
  threshold?: number;
  topK?: number;
}

function isAbnormalEdge(edge: RollupEdgeModel): boolean {
  return edge.status === 'VIOLATION' || edge.status === 'UNATTRIBUTED';
}

function deduplicateServices(edges: RollupEdgeModel[]): EdgeServiceItem[] {
  const seen = new Set<string>();
  const result: EdgeServiceItem[] = [];
  for (const e of edges) {
    for (const s of e.services || []) {
      if (!seen.has(s.service_name)) {
        seen.add(s.service_name);
        result.push(s);
      }
    }
  }
  return result;
}

/**
 * M19 Stage-4a: 高扇出 Hub 节点边折叠
 * 当单个可见节点的可见出边或入边 > HUB_FANOUT_THRESHOLD (8) 且未加入 expandedHubNodeIds 时：
 * 1. 所有 VIOLATION / UNATTRIBUTED 异常边 100% 独立保留；
 * 2. 正常边按 totalCalls -> services.length -> peerId 字典序保留 Top-3 直连边；
 * 3. 其余正常边折叠为 1 条 HUB_SUMMARY 聚合边（携带 [+N 个节点] 展开徽章元数据）。
 */
export function foldHubEdges(
  rollupEdges: RollupEdgeModel[],
  visibleNodes: GraphNode[],
  options: FoldHubEdgesOptions = {}
): RollupEdgeModel[] {
  const {
    foldHubEdges: enabled = true,
    expandedHubNodeIds = new Set<string>(),
    threshold = HUB_FANOUT_THRESHOLD,
    topK = HUB_TOP_K_EDGES,
  } = options;

  if (!enabled || rollupEdges.length === 0) {
    return rollupEdges;
  }

  const visibleIdSet = new Set(visibleNodes.map((n) => n.id));
  const validEdges = rollupEdges.filter(
    (e) => visibleIdSet.has(e.source) && visibleIdSet.has(e.target)
  );

  const foldedEdgeIds = new Set<string>();
  const summaryEdges: RollupEdgeModel[] = [];

  // 1. 检查高扇出出边 (Outbound > threshold)
  const outboundBySource = new Map<string, RollupEdgeModel[]>();
  for (const e of validEdges) {
    const list = outboundBySource.get(e.source);
    if (list) list.push(e);
    else outboundBySource.set(e.source, [e]);
  }

  for (const [srcId, outEdges] of outboundBySource.entries()) {
    if (outEdges.length > threshold && !expandedHubNodeIds.has(srcId)) {
      const normalEdges = outEdges.filter((e) => !isAbnormalEdge(e));
      if (normalEdges.length > topK) {
        const sortedNormal = [...normalEdges].sort((a, b) => {
          const callDiff = (b.totalCalls ?? 0) - (a.totalCalls ?? 0);
          if (callDiff !== 0) return callDiff;
          const svcDiff = (b.services?.length ?? 0) - (a.services?.length ?? 0);
          if (svcDiff !== 0) return svcDiff;
          return a.target.localeCompare(b.target);
        });
        const toFold = sortedNormal.slice(topK);
        for (const fe of toFold) {
          foldedEdgeIds.add(fe.id);
        }
        let mergedSt = toFold[0].status;
        let sumCalls = 0;
        for (const fe of toFold) {
          mergedSt = mergeStatus(mergedSt, fe.status);
          sumCalls += fe.totalCalls ?? 0;
        }
        summaryEdges.push({
          id: `hub_summary_out_${srcId}`,
          source: srcId,
          target: toFold[0].target,
          kind: 'HUB_SUMMARY',
          services: deduplicateServices(toFold),
          rawEdges: toFold.flatMap((fe) => fe.rawEdges),
          status: mergedSt,
          totalCalls: sumCalls,
          isHubSummary: true,
          hubDirection: 'OUTBOUND',
          hubPeerIds: toFold.map((fe) => fe.target),
          foldedPeerCount: toFold.length,
        });
      }
    }
  }

  // 2. 检查高扇入入边 (Inbound > threshold，排除已被出边折叠的边)
  const inboundByTarget = new Map<string, RollupEdgeModel[]>();
  for (const e of validEdges) {
    if (foldedEdgeIds.has(e.id)) continue;
    const list = inboundByTarget.get(e.target);
    if (list) list.push(e);
    else inboundByTarget.set(e.target, [e]);
  }

  for (const [dstId, inEdges] of inboundByTarget.entries()) {
    if (inEdges.length > threshold && !expandedHubNodeIds.has(dstId)) {
      const normalEdges = inEdges.filter((e) => !isAbnormalEdge(e));
      if (normalEdges.length > topK) {
        const sortedNormal = [...normalEdges].sort((a, b) => {
          const callDiff = (b.totalCalls ?? 0) - (a.totalCalls ?? 0);
          if (callDiff !== 0) return callDiff;
          const svcDiff = (b.services?.length ?? 0) - (a.services?.length ?? 0);
          if (svcDiff !== 0) return svcDiff;
          return a.source.localeCompare(b.source);
        });
        const toFold = sortedNormal.slice(topK);
        for (const fe of toFold) {
          foldedEdgeIds.add(fe.id);
        }
        let mergedSt = toFold[0].status;
        let sumCalls = 0;
        for (const fe of toFold) {
          mergedSt = mergeStatus(mergedSt, fe.status);
          sumCalls += fe.totalCalls ?? 0;
        }
        summaryEdges.push({
          id: `hub_summary_in_${dstId}`,
          source: toFold[0].source,
          target: dstId,
          kind: 'HUB_SUMMARY',
          services: deduplicateServices(toFold),
          rawEdges: toFold.flatMap((fe) => fe.rawEdges),
          status: mergedSt,
          totalCalls: sumCalls,
          isHubSummary: true,
          hubDirection: 'INBOUND',
          hubPeerIds: toFold.map((fe) => fe.source),
          foldedPeerCount: toFold.length,
        });
      }
    }
  }

  const retainedDirectEdges = rollupEdges.filter((e) => !foldedEdgeIds.has(e.id));
  return [...retainedDirectEdges, ...summaryEdges];
}

export interface SpotlightSetResult {
  active: boolean;
  highlightedNodeIds: Set<string>;
  highlightedEdgeIds: Set<string>;
}

/**
 * M19 Stage-4b: 悬停 / 选中聚光灯高亮集合计算
 * 精确返回命中节点（或命中边两端节点）、1-Hop 关联边、对端节点以及两端节点各自的可见祖先容器，
 * 确保父容器边框与托盘在聚光灯模式下不被误压暗。
 */
export function computeSpotlightSet(
  activeId: string | null | undefined,
  visibleNodes: GraphNode[],
  rollupEdges: RollupEdgeModel[]
): SpotlightSetResult {
  if (!activeId) {
    return {
      active: false,
      highlightedNodeIds: new Set<string>(),
      highlightedEdgeIds: new Set<string>(),
    };
  }

  const nodeMap = new Map<string, GraphNode>();
  for (const n of visibleNodes) {
    nodeMap.set(n.id, n);
  }

  const highlightedNodeIds = new Set<string>();
  const highlightedEdgeIds = new Set<string>();

  const addNodeWithAncestors = (nodeId: string) => {
    const node = nodeMap.get(nodeId);
    if (!node) return;
    highlightedNodeIds.add(node.id);
    for (const ancId of node.ancestors) {
      if (nodeMap.has(ancId)) {
        highlightedNodeIds.add(ancId);
      }
    }
  };

  // 若 activeId 命中一条边
  const matchedEdge = rollupEdges.find((e) => e.id === activeId);
  if (matchedEdge) {
    highlightedEdgeIds.add(matchedEdge.id);
    addNodeWithAncestors(matchedEdge.source);
    addNodeWithAncestors(matchedEdge.target);
    return {
      active: true,
      highlightedNodeIds,
      highlightedEdgeIds,
    };
  }

  // 若 activeId 命中一个可见节点
  if (nodeMap.has(activeId)) {
    addNodeWithAncestors(activeId);
    for (const e of rollupEdges) {
      if (e.source === activeId || e.target === activeId) {
        highlightedEdgeIds.add(e.id);
        addNodeWithAncestors(e.source);
        addNodeWithAncestors(e.target);
      }
    }
    return {
      active: true,
      highlightedNodeIds,
      highlightedEdgeIds,
    };
  }

  return {
    active: false,
    highlightedNodeIds,
    highlightedEdgeIds,
  };
}
