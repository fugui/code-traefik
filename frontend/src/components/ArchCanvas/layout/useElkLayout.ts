import { useState, useEffect, useRef, useCallback } from 'react';
import type { ElkNode } from 'elkjs';
import {
  normalizeScopeKey,
  toScopedPositionKey,
  type GraphNode,
  type GraphEdge,
  type ExpandStateMap,
  type ContainerBadgeCounts,
  type SavedNodePosition,
  type NeighborhoodDirection,
  type TopologyStatusFilter,
} from '../../../types/topology_graph';
import type { ConsistencyReportDTO, ConsistencyEdge } from '../../../types/traffic';
import { synthesizeOverlay } from '../model/overlay';
import { scopeAndFilter } from '../model/subgraph';
import { aggregateRollup } from '../model/rollup';
import { foldHubEdges } from '../model/noiseReduction';
import { detectCycles } from '../model/metrics';
import {
  getNodeDimensions,
  processLayoutCoordinates,
  type ArchCanvasNode,
  type ArchCanvasEdge,
  type LodMode,
} from './layoutProcessor';
import { resolveOverlapsBottomUp, type NodeBox } from './layoutMerger';

// 内联 Web Worker 引用 (Vite ?worker&inline 模式，微前端安全，无跨域脚本加载异常)
import ElkWorker from './elk.worker?worker&inline';

export interface UseElkLayoutOptions {
  nodes: GraphNode[];
  edges: GraphEdge[];
  expandState: ExpandStateMap;
  consistencyReport?: ConsistencyReportDTO | null;
  view: 'LOGICAL' | 'PHYSICAL';
  draggedNodeIds: Set<string>;
  onToggleExpand: (nodeId: string) => void;
  focusScope?: string;
  seedNodeId?: string | null;
  neighborhoodHop?: number;
  neighborhoodDirection?: NeighborhoodDirection;
  subsystemIds?: string[];
  onlyMyScope?: boolean;
  ownerId?: number | null;
  statusFilter?: TopologyStatusFilter;
  consistencyOnly?: boolean;
  hideIsolated?: boolean;
  foldHubEdgesEnabled?: boolean;
  expandedHubNodeIds?: Set<string>;
  searchKeyword?: string;
  savedPositions?: Record<string, SavedNodePosition> | null;
  pinnedNodeIds?: Set<string>;
  highlightedNodeId?: string | null;
  onFocusScope?: (nodeId: string) => void;
  onTogglePin?: (nodeId: string) => void;
  onExpandHub?: (nodeId: string) => void;
  containerSearchMatchCounts?: Record<string, number>;
  lodMode?: LodMode;
  showRealization?: boolean;
  contractMode?: 'AGG' | 'EXPAND';
}

export interface UseElkLayoutReturn {
  flowNodes: ArchCanvasNode[];
  flowEdges: ArchCanvasEdge[];
  isLayouting: boolean;
  badgeCounts: Record<string, ContainerBadgeCounts>;
  unattributedPendingList: ConsistencyEdge[];
  visibleNodeCount: number;
  refetchLayout: () => void;
}

/**
 * M21-P2: 构建单层扁平 ELK 正交排版图
 * - 不再构造嵌套子容器，所有当前作用域可见节点平铺在同一层次；
 * - 配置 110px 层间水平净空与 48px/36px/24px 节点/边间隔；
 * - 为上游/下游外部上下文传送门卡附加 FIRST / LAST 贴边层级约束。
 */
export function buildElkGraph(
  visibleNodes: GraphNode[],
  edges: Array<{ id: string; source: string; target: string }>,
  expandState?: ExpandStateMap
): ElkNode {
  void expandState;
  const visibleIdSet = new Set(visibleNodes.map((n) => n.id));
  const validEdges = edges.filter((e) => visibleIdSet.has(e.source) && visibleIdSet.has(e.target));

  // 统计外部上下文节点的入/出边数（当存在双向标识时决定贴左还是贴右）
  const inCountMap = new Map<string, number>();
  const outCountMap = new Map<string, number>();
  for (const e of validEdges) {
    outCountMap.set(e.source, (outCountMap.get(e.source) ?? 0) + 1);
    inCountMap.set(e.target, (inCountMap.get(e.target) ?? 0) + 1);
  }

  const children: ElkNode[] = visibleNodes.map((gn) => {
    const dims = getNodeDimensions(gn);
    const elkNode: ElkNode = {
      id: gn.id,
      width: dims.width,
      height: dims.height,
    };

    if (gn.type === 'EXTERNAL_CONTEXT') {
      const dir = gn.meta?.direction;
      let constraint: 'FIRST' | 'LAST' = 'LAST';
      if (dir === 'UPSTREAM') {
        constraint = 'FIRST';
      } else if (dir === 'DOWNSTREAM') {
        constraint = 'LAST';
      } else {
        const outC = outCountMap.get(gn.id) ?? 0;
        const inC = inCountMap.get(gn.id) ?? 0;
        constraint = outC >= inC ? 'FIRST' : 'LAST';
      }
      elkNode.layoutOptions = {
        'elk.layered.layering.layerConstraint': constraint,
      };
    }

    return elkNode;
  });

  return {
    id: 'root',
    layoutOptions: {
      'elk.algorithm': 'layered',
      'elk.direction': 'RIGHT',
      'elk.edgeRouting': 'ORTHOGONAL',
      'elk.layered.spacing.nodeNodeBetweenLayers': '110',
      'elk.spacing.nodeNode': '48',
      'elk.spacing.edgeNode': '36',
      'elk.spacing.edgeEdge': '24',
      'elk.layered.spacing.edgeNodeBetweenLayers': '36',
      'elk.layered.nodePlacement.strategy': 'BRANDES_KOEPF',
      'elk.layered.crossingMinimization.strategy': 'LAYER_SWEEP',
      'elk.padding': '[top=24,left=24,bottom=24,right=24]',
    },
    children,
    edges: validEdges.map((e) => ({
      id: e.id,
      sources: [e.source],
      targets: [e.target],
    })),
  };
}

export function useElkLayout({
  nodes,
  edges,
  expandState,
  consistencyReport,
  view,
  draggedNodeIds,
  onToggleExpand,
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
  foldHubEdgesEnabled = true,
  expandedHubNodeIds,
  searchKeyword = '',
  savedPositions = null,
  pinnedNodeIds,
  highlightedNodeId = null,
  onFocusScope,
  onTogglePin,
  onExpandHub,
  containerSearchMatchCounts,
  lodMode = 'STANDARD',
  showRealization = true,
  contractMode = 'AGG',
}: UseElkLayoutOptions): UseElkLayoutReturn {
  const [flowNodes, setFlowNodes] = useState<ArchCanvasNode[]>([]);
  const [flowEdges, setFlowEdges] = useState<ArchCanvasEdge[]>([]);
  const [isLayouting, setIsLayouting] = useState<boolean>(false);
  const [badgeCounts, setBadgeCounts] = useState<Record<string, ContainerBadgeCounts>>({});
  const [unattributedPendingList, setUnattributedPendingList] = useState<ConsistencyEdge[]>([]);
  const [visibleNodeCount, setVisibleNodeCount] = useState<number>(0);

  const workerRef = useRef<Worker | null>(null);
  const layoutReqIdRef = useRef<number>(0);

  const draggedNodeIdsRef = useRef<Set<string>>(draggedNodeIds);
  draggedNodeIdsRef.current = draggedNodeIds;
  const savedPositionsRef = useRef<Record<string, SavedNodePosition> | null>(savedPositions);
  savedPositionsRef.current = savedPositions;
  const pinnedNodeIdsRef = useRef<Set<string> | undefined>(pinnedNodeIds);
  pinnedNodeIdsRef.current = pinnedNodeIds;

  useEffect(() => {
    if (typeof Worker !== 'undefined') {
      try {
        const worker = new ElkWorker();
        workerRef.current = worker;
      } catch (err: unknown) {
        console.warn('ELK Inline Worker 创建失败，将回退同步模式:', err);
      }
    }
    return () => {
      workerRef.current?.terminate();
      workerRef.current = null;
    };
  }, []);

  // 当用户手动拖拽节点时，仅就地把关联边切换为 SmoothStep，严禁触发 ELK 重排
  useEffect(() => {
    if (draggedNodeIds.size === 0) return;
    setFlowEdges((prev) =>
      prev.map((edge) => {
        if (draggedNodeIds.has(edge.source) || draggedNodeIds.has(edge.target)) {
          if (edge.data?.isDraggedDegraded && !edge.data?.bendPoints) return edge;
          return {
            ...edge,
            data: edge.data
              ? { ...edge.data, isDraggedDegraded: true, bendPoints: undefined }
              : edge.data,
          };
        }
        return edge;
      })
    );
  }, [draggedNodeIds]);

  // 保持现有 flowNodes 中的 isPinned 与高亮脉冲状态与外部同步，无需触发 ELK 重排
  useEffect(() => {
    const scopeKey = normalizeScopeKey(focusScope);
    setFlowNodes((prevNodes) => {
      if (prevNodes.length === 0) return prevNodes;
      let hasChanges = false;
      const nextNodes = prevNodes.map((n) => {
        const scopedKey = toScopedPositionKey(scopeKey, n.id);
        const currentlyPinned = Boolean(
          pinnedNodeIds?.has(n.id) ||
            pinnedNodeIds?.has(scopedKey) ||
            savedPositions?.[scopedKey]?.pinned ||
            savedPositions?.[n.id]?.pinned
        );
        const currentlyHighlighted = highlightedNodeId === n.id;
        if (
          n.data?.isPinned !== currentlyPinned ||
          Boolean(n.data?.isSearchHighlighted) !== currentlyHighlighted
        ) {
          hasChanges = true;
          return {
            ...n,
            data: {
              ...n.data,
              isPinned: currentlyPinned,
              isSearchHighlighted: currentlyHighlighted,
            },
          };
        }
        return n;
      });
      return hasChanges ? nextNodes : prevNodes;
    });
  }, [pinnedNodeIds, savedPositions, focusScope, highlightedNodeId]);

  const runLayout = useCallback(async () => {
    if (nodes.length === 0) {
      setFlowNodes([]);
      setFlowEdges([]);
      setVisibleNodeCount(0);
      return;
    }

    setIsLayouting(true);

    // M20: 过滤实现关系边与多实现契约调用边模式
    let currentEdges = edges;
    if (showRealization === false) {
      currentEdges = currentEdges.filter((e) => e.kind !== 'REALIZATION');
    }
    if (view === 'LOGICAL') {
      if (contractMode === 'EXPAND') {
        const unresolvedContractIds = new Set<string | number>();
        if (consistencyReport?.edges) {
          for (const ce of consistencyReport.edges) {
            if ((ce.status === 'IMPL_UNRESOLVED' || ce.category === 'IMPL_UNRESOLVED') && ce.contract_id) {
              unresolvedContractIds.add(ce.contract_id);
            }
          }
        }
        currentEdges = currentEdges.filter((e) => {
          if (e.contract_id && e.definer_module_id && e.dst_module_id === e.definer_module_id) {
            return unresolvedContractIds.has(e.contract_id);
          }
          return true;
        });
      } else {
        currentEdges = currentEdges.filter((e) => {
          if (e.contract_id && e.impl_key && e.definer_module_id && e.dst_module_id !== e.definer_module_id) {
            return false;
          }
          return true;
        });
      }
    }

    // Stage 1: 一致性报告叠加与动态边合成 (索引加速)
    const overlay = synthesizeOverlay(nodes, currentEdges, consistencyReport, view);
    setUnattributedPendingList(overlay.unattributedPendingList);

    // Stage 2: 单层平铺聚焦裁剪、外部上下文传送门合成与多维降噪过滤
    const scoped = scopeAndFilter(overlay.workingNodes, overlay.workingEdges, expandState, {
      focusScope,
      seedNodeId,
      neighborhoodHop,
      neighborhoodDirection,
      subsystemIds,
      onlyMyScope,
      ownerId,
      statusFilter,
      consistencyOnly,
      hideIsolated,
      searchKeyword,
      edgeStatusMap: overlay.edgeStatusMap,
    });
    setVisibleNodeCount(scoped.visibleIds.size);

    // Stage 3: 基于单层 repOverrides 的代表节点上卷与内部徽章聚合
    const aggregated = aggregateRollup(
      scoped.visibleNodes,
      scoped.scopedEdges,
      expandState,
      overlay,
      scoped.repOverrides
    );
    setBadgeCounts(aggregated.badgeCounts);

    // Stage 4: 高扇出 Hub 节点边折叠 + Tarjan SCC 环路检测
    const foldedEdges = foldHubEdges(aggregated.rollupEdges, scoped.visibleNodes, {
      foldHubEdges: foldHubEdgesEnabled,
      expandedHubNodeIds,
    });
    const visibleNodeIds = scoped.visibleNodes.map((n) => n.id);
    const cycleRes = detectCycles(visibleNodeIds, foldedEdges);

    // Stage 5: 构建单层扁平 ELK 输入图（仅包含当前层可见节点）
    const elkInputGraph = buildElkGraph(scoped.visibleNodes, cycleRes.edges);

    const fullChildrenCountMap: Record<string, number> = {};
    for (const n of nodes) {
      if (n.parent_id) {
        fullChildrenCountMap[n.parent_id] = (fullChildrenCountMap[n.parent_id] || 0) + 1;
      }
    }

    const currentReqId = ++layoutReqIdRef.current;
    const scopeKey = normalizeScopeKey(focusScope);

    const applyLayoutResult = (layoutedTree: ElkNode) => {
      if (currentReqId !== layoutReqIdRef.current) return;

      const currSaved = savedPositionsRef.current;
      let mergedSavedPositions: Record<string, SavedNodePosition> | null = null;
      const localTouchedIds = new Set<string>(draggedNodeIdsRef.current);

      if (currSaved && Object.keys(currSaved).length > 0) {
        const boxMap = new Map<string, NodeBox>();
        let hasAnySavedInScope = false;

        for (const vn of scoped.visibleNodes) {
          const elkChild = layoutedTree.children?.find((c) => c.id === vn.id);
          const dims = getNodeDimensions(vn);
          const scopedKey = toScopedPositionKey(scopeKey, vn.id);
          const sp = currSaved[scopedKey] ?? currSaved[vn.id];
          if (sp) hasAnySavedInScope = true;

          boxMap.set(vn.id, {
            id: vn.id,
            parentId: null,
            x: sp ? sp.x : elkChild?.x ?? 0,
            y: sp ? sp.y : elkChild?.y ?? 0,
            width: elkChild?.width ?? dims.width,
            height: elkChild?.height ?? dims.height,
            pinned: Boolean(
              pinnedNodeIdsRef.current?.has(vn.id) ||
                pinnedNodeIdsRef.current?.has(scopedKey) ||
                sp?.pinned
            ),
            isExpanded: false,
          });
        }

        if (hasAnySavedInScope) {
          const resolved = resolveOverlapsBottomUp(boxMap, scoped.visibleNodes);
          mergedSavedPositions = {};
          for (const [id, box] of resolved.boxes.entries()) {
            mergedSavedPositions[toScopedPositionKey(scopeKey, id)] = {
              x: box.x,
              y: box.y,
              parent_id: null,
              is_expanded: false,
              pinned: box.pinned,
              width: box.width,
              height: box.height,
            };
          }
          for (const tid of resolved.touchedIds) {
            localTouchedIds.add(tid);
          }
        }
      }

      const processed = processLayoutCoordinates(
        layoutedTree,
        scoped.visibleNodes,
        cycleRes.edges,
        localTouchedIds,
        {},
        aggregated.badgeCounts,
        onToggleExpand,
        fullChildrenCountMap,
        {
          focusScope: scopeKey,
          savedPositions: mergedSavedPositions,
          pinnedNodeIds: pinnedNodeIdsRef.current,
          onFocusScope,
          onTogglePin,
          onExpandHub,
          containerSearchMatchCounts,
          lodMode,
          highlightedNodeId,
        }
      );

      setFlowNodes(processed.nodes);
      setFlowEdges(processed.edges);
      setIsLayouting(false);
    };

    if (workerRef.current) {
      const handleWorkerMessage = (
        e: MessageEvent<{ id: string; layouted?: ElkNode; error?: string }>
      ) => {
        if (e.data.id === String(currentReqId)) {
          workerRef.current?.removeEventListener('message', handleWorkerMessage);
          if (e.data.layouted) {
            applyLayoutResult(e.data.layouted);
          } else {
            console.error('ELK Worker 排版异常:', e.data.error);
            setIsLayouting(false);
          }
        }
      };

      workerRef.current.addEventListener('message', handleWorkerMessage);
      workerRef.current.postMessage({
        id: String(currentReqId),
        graph: elkInputGraph,
      });
    } else {
      try {
        const { default: ELK } = await import('elkjs/lib/elk.bundled.js');
        const elk = new ELK();
        const layouted = await elk.layout(elkInputGraph);
        applyLayoutResult(layouted);
      } catch (err: unknown) {
        console.error('同步 ELK 排版异常:', err);
        setIsLayouting(false);
      }
    }
  }, [
    nodes,
    edges,
    expandState,
    consistencyReport,
    view,
    onToggleExpand,
    focusScope,
    seedNodeId,
    neighborhoodHop,
    neighborhoodDirection,
    subsystemIds,
    onlyMyScope,
    ownerId,
    statusFilter,
    consistencyOnly,
    hideIsolated,
    foldHubEdgesEnabled,
    expandedHubNodeIds,
    searchKeyword,
    onFocusScope,
    onTogglePin,
    onExpandHub,
    containerSearchMatchCounts,
    lodMode,
    showRealization,
    contractMode,
    highlightedNodeId,
  ]);

  useEffect(() => {
    void runLayout();
  }, [runLayout]);

  return {
    flowNodes,
    flowEdges,
    isLayouting,
    badgeCounts,
    unattributedPendingList,
    visibleNodeCount,
    refetchLayout: runLayout,
  };
}
