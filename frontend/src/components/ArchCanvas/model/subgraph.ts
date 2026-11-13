import type {
  GraphNode,
  GraphEdge,
  ExpandStateMap,
  ConsistencyEdgeStatus,
  NeighborhoodDirection,
  TopologyStatusFilter,
} from '../../../types/topology_graph';
import { findTopRoot, resolveDirectChildRep } from './hierarchy';

export const VISIBILITY_SOFT_THRESHOLD = 45;
export const VISIBILITY_HARD_CAP = 120;
export const SEARCH_AUTO_EXPAND_CAP = 30;

const ABNORMAL_STATUS_SET = new Set<string>([
  'VIOLATION',
  'UNATTRIBUTED',
  'IMPL_UNRESOLVED',
  'UNCOVERED',
  'NOT_MONITORED',
  'DRIFT',
  'MISSING',
  'UNDECLARED',
]);

export interface ScopeAndFilterOptions {
  focusScope?: string;
  seedNodeId?: string | null;
  selectedNodeId?: string | null;
  neighborhoodHop?: number;
  neighborhoodDirection?: NeighborhoodDirection;
  subsystemIds?: string[];
  onlyMyScope?: boolean;
  ownerId?: number | null;
  statusFilter?: TopologyStatusFilter;
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
  repOverrides: Map<string, string>;
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
 * 裁剪入度=0 且出度=0 的孤立节点与空壳容器：
 * 1. 若某容器内部后代之间存在调用/实现边（即两端映射至同一代表容器且 s != t），该容器不算孤立并予以保留；
 * 2. 若节点或其后代命中 searchKeyword，则强制保留该节点及其完整祖先链路。
 */
export function pruneIsolated(
  nodes: GraphNode[],
  edges: GraphEdge[],
  searchKeyword = '',
  repOverrides?: ReadonlyMap<string, string>
): GraphNode[] {
  const kw = searchKeyword.trim().toLowerCase();
  const nodeMap = new Map<string, GraphNode>();
  for (const n of nodes) {
    nodeMap.set(n.id, n);
  }

  const activeIds = new Set<string>();

  const markNodeAndAncestors = (id: string) => {
    const repId = repOverrides?.get(id);
    if (repId && nodeMap.has(repId)) {
      activeIds.add(repId);
    }
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
    const sIn = nodeMap.has(e.source_node_id) || Boolean(repOverrides?.has(e.source_node_id));
    const tIn = nodeMap.has(e.target_node_id) || Boolean(repOverrides?.has(e.target_node_id));
    if (sIn && tIn && e.source_node_id !== e.target_node_id) {
      markNodeAndAncestors(e.source_node_id);
      markNodeAndAncestors(e.target_node_id);
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

function isVirtualIndependentNode(nodeId: string, nodeMap: Map<string, GraphNode>): boolean {
  if (nodeId.startsWith('VIRTUAL_')) return true;
  const n = nodeMap.get(nodeId);
  return Boolean(n && n.type === 'UNREGISTERED_INTERFACE' && !n.parent_id);
}

/**
 * M21-P1: 单层平铺聚焦裁剪与代表元映射流水线 (Step A -> B -> C -> D)
 * - Step A: 提取 focusScope 完整后代子树，并将跨边界边按对端顶层根与方向合成为外部上下文卡；
 * - Step B: 基于完整子树与跨界边执行有向 N-Hop BFS 过滤；
 * - Step C: 执行 subsystemIds / ownerId / status 切片过滤；
 * - Step D: 将存活内部节点精确投影为当前层直接子节点代表元 (relativeDepth === 1)，
 *           禁止将 focusScope 根容器自身放入画布，并在最后延迟执行孤立裁剪。
 */
export function scopeAndFilter(
  workingNodes: GraphNode[],
  workingEdges: GraphEdge[],
  _expandState: ExpandStateMap,
  options: ScopeAndFilterOptions = {}
): ScopeAndFilterResult {
  const {
    focusScope = 'GLOBAL',
    seedNodeId = null,
    neighborhoodHop = 0,
    neighborhoodDirection = 'BOTH',
    subsystemIds,
    onlyMyScope = false,
    ownerId = null,
    statusFilter = 'ALL',
    consistencyOnly = false,
    hideIsolated = false,
    searchKeyword = '',
    edgeStatusMap,
  } = options;

  const allNodeMap = new Map<string, GraphNode>();
  for (const n of workingNodes) {
    allNodeMap.set(n.id, n);
  }

  const hasValidFocus = Boolean(focusScope && focusScope !== 'GLOBAL' && allNodeMap.has(focusScope));

  let candidateNodes: GraphNode[] = workingNodes;
  let candidateEdges: GraphEdge[] = workingEdges;
  const virtualExternalIds = new Set<string>();

  // Step A: 容器下钻聚焦 -> 全后代提取 + 跨边界外部上下文摘要卡合成
  if (hasValidFocus) {
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
        if (isVirtualIndependentNode(e.target_node_id, allNodeMap)) {
          virtualExternalIds.add(e.target_node_id);
          nextEdges.push(e);
          continue;
        }
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
        if (isVirtualIndependentNode(e.source_node_id, allNodeMap)) {
          virtualExternalIds.add(e.source_node_id);
          nextEdges.push(e);
          continue;
        }
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
      ...workingNodes.filter((n) => subtreeIds.has(n.id) || virtualExternalIds.has(n.id)),
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

  // Step B: 基于完整子树与跨界边的有向 N-Hop 邻域 BFS 过滤
  const effectiveSeed = seedNodeId || (hasValidFocus && neighborhoodHop > 0 ? focusScope : null);
  if (neighborhoodHop > 0 && effectiveSeed) {
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

  // Step C1: 按顶层子系统 / 进程组 subsystemIds 多选切片
  if (subsystemIds && subsystemIds.length > 0) {
    const allowedSet = new Set(subsystemIds);
    const currMap = rebuildCandidateMap(candidateNodes);
    const matchedIds = new Set<string>();

    for (const n of candidateNodes) {
      if (n.type === 'EXTERNAL_CONTEXT') {
        matchedIds.add(n.id);
        continue;
      }
      if (isVirtualIndependentNode(n.id, currMap)) {
        matchedIds.add(n.id);
        continue;
      }
      const root = findTopRoot(n.id, currMap);
      if (root && allowedSet.has(root.id)) {
        matchedIds.add(n.id);
        for (const ancId of n.ancestors) {
          if (currMap.has(ancId)) matchedIds.add(ancId);
        }
      }
    }

    candidateNodes = candidateNodes.filter((n) => matchedIds.has(n.id));
    candidateEdges = candidateEdges.filter(
      (e) => matchedIds.has(e.source_node_id) && matchedIds.has(e.target_node_id)
    );
  }

  // Step C2: 按负责人 owner_id 过滤
  const effectiveOwnerFilter = ownerId != null ? ownerId : null;
  if (effectiveOwnerFilter != null || (onlyMyScope && effectiveOwnerFilter != null)) {
    const currMap = rebuildCandidateMap(candidateNodes);
    const matchedIds = new Set<string>();
    const targetOwner = Number(effectiveOwnerFilter);

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

  // Step C3: 按一致状态筛选 (ALL / ABNORMAL / VIOLATION_ONLY)
  const effectiveStatusFilter: TopologyStatusFilter =
    statusFilter !== 'ALL' ? statusFilter : consistencyOnly ? 'ABNORMAL' : 'ALL';

  if (effectiveStatusFilter !== 'ALL') {
    const currMap = rebuildCandidateMap(candidateNodes);
    const abnormalNodeIds = new Set<string>();
    const abnormalEdges: GraphEdge[] = [];

    for (const e of candidateEdges) {
      const st = edgeStatusMap?.get(e.id);
      const isViolation = st === 'VIOLATION' || e.id.startsWith('dyn_viol_');
      const isAnyAbnormal =
        (st != null && ABNORMAL_STATUS_SET.has(st)) ||
        e.id.startsWith('dyn_viol_') ||
        e.id.startsWith('dyn_unattr_') ||
        e.id.startsWith('dyn_impl_unres_');

      const matched = effectiveStatusFilter === 'VIOLATION_ONLY' ? isViolation : isAnyAbnormal;
      if (matched && currMap.has(e.source_node_id) && currMap.has(e.target_node_id)) {
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

  // Step D: 单层平铺代表元投影 & 延迟孤立节点裁剪
  const survivingMap = rebuildCandidateMap(candidateNodes);
  const repOverrides = new Map<string, string>();
  const directRepIdSet = new Set<string>();
  const scopedInternalNodes: GraphNode[] = [];

  for (const n of candidateNodes) {
    if (n.type === 'EXTERNAL_CONTEXT' || isVirtualIndependentNode(n.id, survivingMap)) {
      directRepIdSet.add(n.id);
      repOverrides.set(n.id, n.id);
      scopedInternalNodes.push(n);
      continue;
    }

    if (hasValidFocus && n.id === focusScope) {
      // 聚焦根容器自身绝不投影为画布可见节点
      continue;
    }

    const repId = resolveDirectChildRep(n.id, hasValidFocus ? focusScope : 'GLOBAL', survivingMap);
    if (repId && survivingMap.has(repId)) {
      directRepIdSet.add(repId);
      repOverrides.set(n.id, repId);
      scopedInternalNodes.push(n);
    }
  }

  // 过滤出两端均有存活代表元的有效边
  let projectedEdges = candidateEdges.filter(
    (e) => repOverrides.has(e.source_node_id) && repOverrides.has(e.target_node_id)
  );

  // 对孤立节点与无关联传送门卡进行最终裁剪
  const hasCrossOrInternalActivity = new Set<string>();
  const hasAnyExternalEdge = new Set<string>();
  for (const e of projectedEdges) {
    const sRep = repOverrides.get(e.source_node_id);
    const tRep = repOverrides.get(e.target_node_id);
    if (!sRep || !tRep) continue;
    if (e.source_node_id !== e.target_node_id) {
      hasCrossOrInternalActivity.add(sRep);
      hasCrossOrInternalActivity.add(tRep);
    }
    if (sRep !== tRep) {
      hasAnyExternalEdge.add(sRep);
      hasAnyExternalEdge.add(tRep);
    }
  }

  const kw = searchKeyword.trim().toLowerCase();
  const searchMatchedReps = new Set<string>();
  if (kw) {
    for (const n of candidateNodes) {
      if (isSearchMatch(n, kw)) {
        const r = repOverrides.get(n.id);
        if (r) searchMatchedReps.add(r);
      }
    }
  }

  const visibleNodes: GraphNode[] = [];
  const visibleIds = new Set<string>();

  for (const repId of directRepIdSet) {
    const node = survivingMap.get(repId);
    if (!node) continue;

    // 外部上下文卡与虚拟独立卡仅在仍有存活边连接（或命中搜索）时才保留
    if (node.type === 'EXTERNAL_CONTEXT' || isVirtualIndependentNode(node.id, survivingMap)) {
      if (!hasAnyExternalEdge.has(node.id) && !searchMatchedReps.has(node.id)) {
        continue;
      }
    } else if (hideIsolated) {
      if (!hasCrossOrInternalActivity.has(repId) && !searchMatchedReps.has(repId)) {
        continue;
      }
    }

    visibleNodes.push(node);
    visibleIds.add(node.id);
  }

  // 移除被 hideIsolated 裁掉节点的映射关系
  if (hideIsolated || visibleIds.size !== directRepIdSet.size) {
    for (const [k, v] of Array.from(repOverrides.entries())) {
      if (!visibleIds.has(v)) {
        repOverrides.delete(k);
      }
    }
    projectedEdges = projectedEdges.filter(
      (e) => repOverrides.has(e.source_node_id) && repOverrides.has(e.target_node_id)
    );
  }

  return {
    scopedNodes: scopedInternalNodes.filter((n) => {
      const rep = repOverrides.get(n.id);
      return Boolean(rep && visibleIds.has(rep));
    }),
    scopedEdges: projectedEdges,
    visibleNodes,
    visibleIds,
    repOverrides,
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
