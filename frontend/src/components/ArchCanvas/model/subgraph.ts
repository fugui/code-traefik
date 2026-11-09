import type {
  GraphNode,
  GraphEdge,
  ExpandStateMap,
  ConsistencyEdgeStatus,
  NeighborhoodDirection,
} from '../../../types/topology_graph';

export const VISIBILITY_SOFT_THRESHOLD = 45;
export const VISIBILITY_HARD_CAP = 120;
export const SEARCH_AUTO_EXPAND_CAP = 30;

export interface ScopeAndFilterOptions {
  focusScope?: string;
  seedNodeId?: string | null;
  neighborhoodHop?: number;
  neighborhoodDirection?: NeighborhoodDirection;
  ownerId?: number | null;
  consistencyOnly?: boolean;
  hideIsolated?: boolean;
  searchKeyword?: string;
  edgeStatusMap?: Map<string, ConsistencyEdgeStatus>;
}

export interface ScopeAndFilterResult {
  scopedNodes: GraphNode[];
  scopedEdges: GraphEdge[];
  visibleNodes: GraphNode[];
  visibleIds: Set<string>;
}

function isSearchMatch(node: GraphNode, keyword: string): boolean {
  if (!keyword) return false;
  const kw = keyword.toLowerCase();
  return (
    node.name.toLowerCase().includes(kw) ||
    Boolean(node.code && node.code.toLowerCase().includes(kw)) ||
    node.id.toLowerCase().includes(kw)
  );
}

/**
 * 裁剪入度=0 且出度=0 的孤立叶子节点与空壳容器；
 * 若孤立节点命中 searchKeyword，则强制保留该节点及其完整祖先链路。
 */
export function pruneIsolated(
  nodes: GraphNode[],
  edges: GraphEdge[],
  searchKeyword = ''
): GraphNode[] {
  const kw = searchKeyword.trim().toLowerCase();
  const nodeMap = new Map<string, GraphNode>();
  for (const n of nodes) {
    nodeMap.set(n.id, n);
  }

  const activeIds = new Set<string>();

  const markNodeAndAncestors = (id: string) => {
    const n = nodeMap.get(id);
    if (!n) return;
    activeIds.add(n.id);
    for (const ancId of n.ancestors) {
      if (nodeMap.has(ancId)) {
        activeIds.add(ancId);
      }
    }
  };

  for (const e of edges) {
    if (nodeMap.has(e.source_node_id) && nodeMap.has(e.target_node_id)) {
      if (e.source_node_id !== e.target_node_id) {
        markNodeAndAncestors(e.source_node_id);
        markNodeAndAncestors(e.target_node_id);
      }
    }
  }

  if (kw) {
    for (const n of nodes) {
      if (isSearchMatch(n, kw)) {
        markNodeAndAncestors(n.id);
      }
    }
  }

  return nodes.filter((n) => activeIds.has(n.id));
}

/**
 * M19 Stage-2: 容器下钻聚焦裁剪 (Focus Scope)、跨边界外部上下文摘要卡合成、
 * 有向 N-Hop 邻域 BFS、负责人/异常过滤、孤立节点裁剪与可见集推导。
 */
export function scopeAndFilter(
  workingNodes: GraphNode[],
  workingEdges: GraphEdge[],
  expandState: ExpandStateMap,
  options: ScopeAndFilterOptions = {}
): ScopeAndFilterResult {
  const {
    focusScope = 'GLOBAL',
    seedNodeId = null,
    neighborhoodHop = 0,
    neighborhoodDirection = 'BOTH',
    ownerId = null,
    consistencyOnly = false,
    hideIsolated = false,
    searchKeyword = '',
    edgeStatusMap,
  } = options;

  const allNodeMap = new Map<string, GraphNode>();
  for (const n of workingNodes) {
    allNodeMap.set(n.id, n);
  }

  let candidateNodes: GraphNode[] = workingNodes;
  let candidateEdges: GraphEdge[] = workingEdges;
  let nodesFiltered = false;

  // 1. 容器下钻聚焦 (Focus Scope) + 跨边界外部上下文摘要卡合成
  if (focusScope && focusScope !== 'GLOBAL' && allNodeMap.has(focusScope)) {
    nodesFiltered = true;
    const focusNode = allNodeMap.get(focusScope)!;
    const focusAncestorSet = new Set<string>(focusNode.ancestors);
    const subtreeIds = new Set<string>();
    for (const n of workingNodes) {
      if (n.id === focusScope || n.ancestors.includes(focusScope)) {
        subtreeIds.add(n.id);
      }
    }

    const resolveExternalPeerRoot = (extNodeId: string): GraphNode | null => {
      const extNode = allNodeMap.get(extNodeId);
      if (!extNode) return null;
      for (const ancId of extNode.ancestors) {
        if (!focusAncestorSet.has(ancId) && ancId !== focusScope) {
          return allNodeMap.get(ancId) || extNode;
        }
      }
      return extNode;
    };

    interface ContextAcc {
      direction: 'UPSTREAM' | 'DOWNSTREAM';
      peerRoot: GraphNode;
      hiddenModules: Set<string>;
      crossEdgeCount: number;
      violationCount: number;
    }

    const contextMap = new Map<string, ContextAcc>();
    const nextEdges: GraphEdge[] = [];

    for (const e of workingEdges) {
      const srcIn = subtreeIds.has(e.source_node_id);
      const dstIn = subtreeIds.has(e.target_node_id);

      if (srcIn && dstIn) {
        nextEdges.push(e);
      } else if (srcIn && !dstIn) {
        const peerRoot = resolveExternalPeerRoot(e.target_node_id);
        if (!peerRoot) continue;
        const cardId = `ext_ctx_DOWNSTREAM_${peerRoot.id}`;
        let acc = contextMap.get(cardId);
        if (!acc) {
          acc = {
            direction: 'DOWNSTREAM',
            peerRoot,
            hiddenModules: new Set<string>(),
            crossEdgeCount: 0,
            violationCount: 0,
          };
          contextMap.set(cardId, acc);
        }
        acc.hiddenModules.add(e.target_node_id);
        acc.crossEdgeCount += 1;
        if (edgeStatusMap?.get(e.id) === 'VIOLATION') {
          acc.violationCount += 1;
        }
        nextEdges.push({
          ...e,
          target_node_id: cardId,
        });
      } else if (!srcIn && dstIn) {
        const peerRoot = resolveExternalPeerRoot(e.source_node_id);
        if (!peerRoot) continue;
        const cardId = `ext_ctx_UPSTREAM_${peerRoot.id}`;
        let acc = contextMap.get(cardId);
        if (!acc) {
          acc = {
            direction: 'UPSTREAM',
            peerRoot,
            hiddenModules: new Set<string>(),
            crossEdgeCount: 0,
            violationCount: 0,
          };
          contextMap.set(cardId, acc);
        }
        acc.hiddenModules.add(e.source_node_id);
        acc.crossEdgeCount += 1;
        if (edgeStatusMap?.get(e.id) === 'VIOLATION') {
          acc.violationCount += 1;
        }
        nextEdges.push({
          ...e,
          source_node_id: cardId,
        });
      }
    }

    const extContextCards: GraphNode[] = [];
    let extSortIdx = 90000;
    for (const [cardId, acc] of contextMap.entries()) {
      extContextCards.push({
        id: cardId,
        type: 'EXTERNAL_CONTEXT',
        name: acc.peerRoot.name,
        code: acc.peerRoot.code,
        depth: 0,
        ancestors: [],
        sort_order: extSortIdx++,
        meta: {
          direction: acc.direction,
          peer_root_id: acc.peerRoot.id,
          peer_root_name: acc.peerRoot.name,
          hidden_module_count: acc.hiddenModules.size,
          cross_edge_count: acc.crossEdgeCount,
          violation_count: acc.violationCount,
        },
      });
    }

    candidateNodes = [
      ...workingNodes.filter((n) => subtreeIds.has(n.id)),
      ...extContextCards,
    ];
    candidateEdges = nextEdges;
  }

  const rebuildCandidateMap = (list: GraphNode[]) => {
    const m = new Map<string, GraphNode>();
    for (const n of list) {
      m.set(n.id, n);
    }
    return m;
  };

  // 2. 有向 N-Hop 邻域 BFS 过滤
  const effectiveSeed = seedNodeId || (focusScope !== 'GLOBAL' ? focusScope : null);
  if (neighborhoodHop > 0 && effectiveSeed) {
    nodesFiltered = true;
    const currMap = rebuildCandidateMap(candidateNodes);
    if (currMap.has(effectiveSeed)) {
      const seedSet = new Set<string>();
      for (const n of candidateNodes) {
        if (n.id === effectiveSeed || n.ancestors.includes(effectiveSeed)) {
          seedSet.add(n.id);
        }
      }

      const outbound = new Map<string, Set<string>>();
      const inbound = new Map<string, Set<string>>();
      for (const e of candidateEdges) {
        if (!outbound.has(e.source_node_id)) outbound.set(e.source_node_id, new Set());
        if (!inbound.has(e.target_node_id)) inbound.set(e.target_node_id, new Set());
        outbound.get(e.source_node_id)!.add(e.target_node_id);
        inbound.get(e.target_node_id)!.add(e.source_node_id);
      }

      const visited = new Set<string>(seedSet);
      let frontier = new Set<string>(seedSet);

      for (let hop = 1; hop <= neighborhoodHop; hop++) {
        const nextFrontier = new Set<string>();
        for (const u of frontier) {
          if (neighborhoodDirection === 'DOWNSTREAM' || neighborhoodDirection === 'BOTH') {
            for (const v of outbound.get(u) || []) {
              if (!visited.has(v) && currMap.has(v)) {
                visited.add(v);
                nextFrontier.add(v);
              }
            }
          }
          if (neighborhoodDirection === 'UPSTREAM' || neighborhoodDirection === 'BOTH') {
            for (const v of inbound.get(u) || []) {
              if (!visited.has(v) && currMap.has(v)) {
                visited.add(v);
                nextFrontier.add(v);
              }
            }
          }
        }
        frontier = nextFrontier;
        if (frontier.size === 0) break;
      }

      const retainedWithAncestors = new Set<string>(visited);
      for (const id of visited) {
        const node = currMap.get(id);
        if (node) {
          for (const ancId of node.ancestors) {
            if (currMap.has(ancId)) {
              retainedWithAncestors.add(ancId);
            }
          }
        }
      }

      candidateNodes = candidateNodes.filter((n) => retainedWithAncestors.has(n.id));
      candidateEdges = candidateEdges.filter(
        (e) => retainedWithAncestors.has(e.source_node_id) && retainedWithAncestors.has(e.target_node_id)
      );
    }
  }

  // 3. 按负责人 owner_id 过滤
  if (ownerId != null) {
    nodesFiltered = true;
    const currMap = rebuildCandidateMap(candidateNodes);
    const matchedIds = new Set<string>();
    const targetOwner = Number(ownerId);

    for (const n of candidateNodes) {
      if (n.type === 'MODULE' && Number(n.meta?.owner_id) === targetOwner) {
        matchedIds.add(n.id);
        for (const ancId of n.ancestors) {
          if (currMap.has(ancId)) {
            matchedIds.add(ancId);
          }
        }
      }
    }

    // 若图中不存在 MODULE 级 owner_id，回退检查容器自身 owner_id
    if (matchedIds.size === 0) {
      for (const n of candidateNodes) {
        if (Number(n.meta?.owner_id) === targetOwner) {
          matchedIds.add(n.id);
          for (const ancId of n.ancestors) {
            if (currMap.has(ancId)) matchedIds.add(ancId);
          }
        }
      }
    }

    candidateNodes = candidateNodes.filter((n) => matchedIds.has(n.id));
    candidateEdges = candidateEdges.filter(
      (e) => matchedIds.has(e.source_node_id) && matchedIds.has(e.target_node_id)
    );
  }

  // 4. 按 consistency_only 过滤（仅保留 VIOLATION / UNATTRIBUTED 异常边及其两端节点与祖先）
  if (consistencyOnly) {
    nodesFiltered = true;
    const currMap = rebuildCandidateMap(candidateNodes);
    const abnormalNodeIds = new Set<string>();
    const abnormalEdges: GraphEdge[] = [];

    for (const e of candidateEdges) {
      const st = edgeStatusMap?.get(e.id);
      const isAbnormal =
        st === 'VIOLATION' ||
        st === 'UNATTRIBUTED' ||
        e.id.startsWith('dyn_viol_') ||
        e.id.startsWith('dyn_unattr_');
      if (isAbnormal && currMap.has(e.source_node_id) && currMap.has(e.target_node_id)) {
        abnormalEdges.push(e);
        for (const epId of [e.source_node_id, e.target_node_id]) {
          abnormalNodeIds.add(epId);
          const epNode = currMap.get(epId);
          if (epNode) {
            for (const ancId of epNode.ancestors) {
              if (currMap.has(ancId)) {
                abnormalNodeIds.add(ancId);
              }
            }
          }
        }
      }
    }

    candidateNodes = candidateNodes.filter((n) => abnormalNodeIds.has(n.id));
    candidateEdges = abnormalEdges;
  }

  // 5. 隐藏孤立节点与空壳容器 (hideIsolated)
  if (hideIsolated) {
    nodesFiltered = true;
    candidateNodes = pruneIsolated(candidateNodes, candidateEdges, searchKeyword);
    const keptSet = new Set(candidateNodes.map((n) => n.id));
    candidateEdges = candidateEdges.filter(
      (e) => keptSet.has(e.source_node_id) && keptSet.has(e.target_node_id)
    );
  }

  // 6. 根据当前 expandState 计算实际可见节点集合 visibleNodes / visibleIds
  const finalMap = nodesFiltered ? rebuildCandidateMap(candidateNodes) : allNodeMap;
  const visibleNodes: GraphNode[] = [];
  const visibleIds = new Set<string>();
  for (const gn of candidateNodes) {
    let isVisible = true;
    for (const ancId of gn.ancestors) {
      if (nodesFiltered && !finalMap.has(ancId)) continue;
      if (!expandState[ancId]) {
        isVisible = false;
        break;
      }
    }
    if (isVisible) {
      visibleNodes.push(gn);
      visibleIds.add(gn.id);
    }
  }

  return {
    scopedNodes: candidateNodes,
    scopedEdges: candidateEdges,
    visibleNodes,
    visibleIds,
  };
}

/**
 * 统一可见规模守卫的精确预估函数：
 * 在任何可能改变可见节点数的入口前调用，与 scopeAndFilter 共享完全一致的计算路径。
 */
export function estimateVisibleNodeCount(
  workingNodes: GraphNode[],
  workingEdges: GraphEdge[],
  candidateExpandState: ExpandStateMap,
  options: ScopeAndFilterOptions = {}
): number {
  return scopeAndFilter(workingNodes, workingEdges, candidateExpandState, options).visibleIds.size;
}
