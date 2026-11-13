import type {
  GraphNode,
  RollupEdgeModel,
  EdgeServiceItem,
  ConsistencyEdgeStatus,
} from '../../../types/topology_graph';
import { mergeStatus, STATUS_PRIORITY } from './overlay';

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
  return (
    edge.status === 'VIOLATION' ||
    edge.status === 'UNATTRIBUTED' ||
    edge.status === 'IMPL_UNRESOLVED'
  );
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
  /** 主体节点及其全部可见后代子树 S */
  subjectNodeIds: Set<string>;
  /** 调用主体子树的 1-Hop 上游对端节点及其可见后代 */
  upstreamNodeIds: Set<string>;
  /** 被主体子树调用的 1-Hop 下游对端节点及其可见后代 */
  downstreamNodeIds: Set<string>;
  /** 主体与关联对端的可见祖先容器外壳（仅作上下文，不属于主体或对端本身） */
  contextAncestorIds: Set<string>;
  /** 主体子树内部的边（两端均在 S 内） */
  internalEdgeIds: Set<string>;
  /** 指向主体子树的 1-Hop 上游入边 */
  upstreamEdgeIds: Set<string>;
  /** 从主体子树发出的 1-Hop 下游出边 */
  downstreamEdgeIds: Set<string>;
}

function createEmptySpotlightResult(): SpotlightSetResult {
  return {
    active: false,
    highlightedNodeIds: new Set<string>(),
    highlightedEdgeIds: new Set<string>(),
    subjectNodeIds: new Set<string>(),
    upstreamNodeIds: new Set<string>(),
    downstreamNodeIds: new Set<string>(),
    contextAncestorIds: new Set<string>(),
    internalEdgeIds: new Set<string>(),
    upstreamEdgeIds: new Set<string>(),
    downstreamEdgeIds: new Set<string>(),
  };
}

/**
 * M19.1: 子树级聚光灯高亮集合计算
 * 以主体子树 S = {activeId} ∪ descendants(activeId) 为计算单元：
 * 1. S 内所有可见后代节点与内部边 100% 保亮；
 * 2. 一端在 S 内、另一端在 S 外的跨边界边按方向划分为 upstream (入边) 与 downstream (出边)；
 * 3. 主体与关联对端的可见祖先容器归入 contextAncestorIds，避免父容器边框被压暗。
 */
export function computeSpotlightSet(
  activeId: string | null | undefined,
  visibleNodes: GraphNode[],
  rollupEdges: RollupEdgeModel[]
): SpotlightSetResult {
  if (!activeId) {
    return createEmptySpotlightResult();
  }

  const nodeMap = new Map<string, GraphNode>();
  for (const n of visibleNodes) {
    nodeMap.set(n.id, n);
  }

  const highlightedNodeIds = new Set<string>();
  const highlightedEdgeIds = new Set<string>();
  const subjectNodeIds = new Set<string>();
  const upstreamNodeIds = new Set<string>();
  const downstreamNodeIds = new Set<string>();
  const contextAncestorIds = new Set<string>();
  const internalEdgeIds = new Set<string>();
  const upstreamEdgeIds = new Set<string>();
  const downstreamEdgeIds = new Set<string>();

  const collectSubtree = (rootId: string, bucket: Set<string>) => {
    if (!nodeMap.has(rootId)) return;
    bucket.add(rootId);
    highlightedNodeIds.add(rootId);
    for (const n of visibleNodes) {
      if (n.id === rootId || n.ancestors.includes(rootId)) {
        bucket.add(n.id);
        highlightedNodeIds.add(n.id);
      }
    }
  };

  const addAncestors = (nodeId: string) => {
    const node = nodeMap.get(nodeId);
    if (!node) return;
    for (const ancId of node.ancestors) {
      if (nodeMap.has(ancId)) {
        highlightedNodeIds.add(ancId);
        contextAncestorIds.add(ancId);
      }
    }
  };

  const finalizeContextAncestors = () => {
    for (const id of Array.from(contextAncestorIds)) {
      if (subjectNodeIds.has(id) || upstreamNodeIds.has(id) || downstreamNodeIds.has(id)) {
        contextAncestorIds.delete(id);
      }
    }
  };

  // 若 activeId 命中一条边
  const matchedEdge = rollupEdges.find((e) => e.id === activeId);
  if (matchedEdge) {
    highlightedEdgeIds.add(matchedEdge.id);
    collectSubtree(matchedEdge.source, upstreamNodeIds);
    collectSubtree(matchedEdge.target, downstreamNodeIds);
    addAncestors(matchedEdge.source);
    addAncestors(matchedEdge.target);
    finalizeContextAncestors();
    return {
      active: true,
      highlightedNodeIds,
      highlightedEdgeIds,
      subjectNodeIds,
      upstreamNodeIds,
      downstreamNodeIds,
      contextAncestorIds,
      internalEdgeIds,
      upstreamEdgeIds,
      downstreamEdgeIds,
    };
  }

  // 若 activeId 命中一个可见节点（含展开态父容器）
  if (nodeMap.has(activeId)) {
    collectSubtree(activeId, subjectNodeIds);
    addAncestors(activeId);

    for (const e of rollupEdges) {
      const srcIn = subjectNodeIds.has(e.source);
      const dstIn = subjectNodeIds.has(e.target);

      if (srcIn && dstIn) {
        highlightedEdgeIds.add(e.id);
        internalEdgeIds.add(e.id);
      } else if (srcIn && !dstIn) {
        highlightedEdgeIds.add(e.id);
        downstreamEdgeIds.add(e.id);
        collectSubtree(e.target, downstreamNodeIds);
        addAncestors(e.target);
        if (e.isHubSummary && e.hubPeerIds) {
          for (const pid of e.hubPeerIds) {
            collectSubtree(pid, downstreamNodeIds);
            addAncestors(pid);
          }
        }
      } else if (!srcIn && dstIn) {
        highlightedEdgeIds.add(e.id);
        upstreamEdgeIds.add(e.id);
        collectSubtree(e.source, upstreamNodeIds);
        addAncestors(e.source);
        if (e.isHubSummary && e.hubPeerIds) {
          for (const pid of e.hubPeerIds) {
            collectSubtree(pid, upstreamNodeIds);
            addAncestors(pid);
          }
        }
      }
    }

    finalizeContextAncestors();
    return {
      active: true,
      highlightedNodeIds,
      highlightedEdgeIds,
      subjectNodeIds,
      upstreamNodeIds,
      downstreamNodeIds,
      contextAncestorIds,
      internalEdgeIds,
      upstreamEdgeIds,
      downstreamEdgeIds,
    };
  }

  return createEmptySpotlightResult();
}

export interface NodeRelationItem {
  edgeId: string;
  peerNode: GraphNode;
  direction: 'UPSTREAM' | 'DOWNSTREAM';
  contractCount: number;
  totalCalls: number;
  status?: ConsistencyEdgeStatus;
  services: EdgeServiceItem[];
  edgeModel: RollupEdgeModel;
}

export interface NodeRelationsResult {
  upstream: NodeRelationItem[];
  downstream: NodeRelationItem[];
  internalCount: number;
}

/**
 * M19.1: 计算指定节点（或容器子树）的 1-Hop 上下游关联关系清单
 * 排序规则：异常状态优先级 (VIOLATION > UNATTRIBUTED > ...) -> 调用次数降序 -> 契约数降序 -> 节点名称字典序
 */
export function computeNodeRelations(
  targetNodeId: string | null | undefined,
  nodes: GraphNode[],
  rollupEdges: RollupEdgeModel[]
): NodeRelationsResult {
  if (!targetNodeId) {
    return { upstream: [], downstream: [], internalCount: 0 };
  }

  const nodeMap = new Map<string, GraphNode>();
  const subtreeIds = new Set<string>();
  for (const n of nodes) {
    nodeMap.set(n.id, n);
    if (n.id === targetNodeId || n.ancestors.includes(targetNodeId)) {
      subtreeIds.add(n.id);
    }
  }

  if (!nodeMap.has(targetNodeId)) {
    return { upstream: [], downstream: [], internalCount: 0 };
  }

  const upstream: NodeRelationItem[] = [];
  const downstream: NodeRelationItem[] = [];
  let internalCount = 0;

  for (const e of rollupEdges) {
    const srcIn = subtreeIds.has(e.source);
    const dstIn = subtreeIds.has(e.target);

    if (srcIn && dstIn) {
      internalCount += 1;
      continue;
    }

    if (!srcIn && dstIn) {
      const peer = nodeMap.get(e.source);
      if (peer) {
        upstream.push({
          edgeId: e.id,
          peerNode: peer,
          direction: 'UPSTREAM',
          contractCount: e.services?.length || 1,
          totalCalls: e.totalCalls ?? 0,
          status: e.status,
          services: e.services || [],
          edgeModel: e,
        });
      }
    } else if (srcIn && !dstIn) {
      const peer = nodeMap.get(e.target);
      if (peer) {
        downstream.push({
          edgeId: e.id,
          peerNode: peer,
          direction: 'DOWNSTREAM',
          contractCount: e.services?.length || 1,
          totalCalls: e.totalCalls ?? 0,
          status: e.status,
          services: e.services || [],
          edgeModel: e,
        });
      }
    }
  }

  const sortRelations = (a: NodeRelationItem, b: NodeRelationItem): number => {
    const prioA = a.status ? STATUS_PRIORITY[a.status] ?? 0 : 0;
    const prioB = b.status ? STATUS_PRIORITY[b.status] ?? 0 : 0;
    if (prioA !== prioB) return prioB - prioA;
    if (b.totalCalls !== a.totalCalls) return b.totalCalls - a.totalCalls;
    if (b.contractCount !== a.contractCount) return b.contractCount - a.contractCount;
    return a.peerNode.name.localeCompare(b.peerNode.name);
  };

  upstream.sort(sortRelations);
  downstream.sort(sortRelations);

  return {
    upstream,
    downstream,
    internalCount,
  };
}
