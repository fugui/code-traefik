import { useState, useEffect, useRef, useCallback } from 'react';
import type { ElkNode } from 'elkjs';
import type {
  GraphNode,
  GraphEdge,
  ExpandStateMap,
  ContainerBadgeCounts,
  SavedNodePosition,
  NeighborhoodDirection,
} from '../../../types/topology_graph';
import type { ConsistencyReportDTO, ConsistencyEdge } from '../../../types/traffic';
import { synthesizeOverlay } from '../model/overlay';
import { scopeAndFilter } from '../model/subgraph';
import { aggregateRollup } from '../model/rollup';
import { foldHubEdges } from '../model/noiseReduction';
import { detectCycles } from '../model/metrics';
import {
  processLayoutCoordinates,
  extractNodeCoordinates,
  type ArchCanvasNode,
  type ArchCanvasEdge,
  type LodMode,
} from './layoutProcessor';
import { mergeLocalLayout, resolveOverlapsBottomUp, type NodeBox } from './layoutMerger';

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
  ownerId?: number | null;
  consistencyOnly?: boolean;
  hideIsolated?: boolean;
  foldHubEdgesEnabled?: boolean;
  expandedHubNodeIds?: Set<string>;
  searchKeyword?: string;
  savedPositions?: Record<string, SavedNodePosition> | null;
  pinnedNodeIds?: Set<string>;
  onFocusScope?: (nodeId: string) => void;
  onTogglePin?: (nodeId: string) => void;
  onExpandHub?: (nodeId: string) => void;
  onPositionsComputed?: (positions: Record<string, SavedNodePosition>) => void;
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
 * 递归构建 ELK 复合图输入结构（仅将 visibleNodes 注入 ELK，复杂度由 O(|V_visible|) 决定）
 */
export function buildElkGraph(
  visibleNodes: GraphNode[],
  edges: Array<{ id: string; source: string; target: string }>,
  expandState: ExpandStateMap
): ElkNode {
  const nodeMap = new Map(visibleNodes.map((n) => [n.id, n]));
  const childMap = new Map<string, GraphNode[]>();

  for (const n of visibleNodes) {
    if (n.parent_id && nodeMap.has(n.parent_id)) {
      if (!childMap.has(n.parent_id)) {
        childMap.set(n.parent_id, []);
      }
      childMap.get(n.parent_id)!.push(n);
    }
  }

  function convertNode(gn: GraphNode): ElkNode {
    const isExpanded = Boolean(expandState[gn.id]);
    const children = isExpanded ? childMap.get(gn.id) || [] : [];

    const isLeaf = children.length === 0;
    let width = 220;
    let height = 74;

    if (
      gn.type === 'SUBSYSTEM' ||
      gn.type === 'FUNCTIONAL_GROUP' ||
      gn.type === 'PROCESS_GROUP' ||
      gn.type === 'PROCESS' ||
      gn.type === 'UNDEPLOYED_CONTAINER' ||
      gn.type === 'EXTERNAL_CONTAINER'
    ) {
      if (!isExpanded) {
        width = 260;
        height = 80;
      }
    } else if (gn.type === 'EXTERNAL_CONTEXT') {
      width = 240;
      height = 112;
    } else if (
      gn.type === 'EXTERNAL_SYSTEM' ||
      gn.type === 'EXTERNAL_ENDPOINT' ||
      gn.type === 'UNREGISTERED_INTERFACE'
    ) {
      width = 190;
      height = 60;
    }

    const elkNode: ElkNode = {
      id: gn.id,
      layoutOptions: {
        'elk.padding': '[top=52,left=16,bottom=16,right=16]',
        'elk.spacing.nodeNode': '28',
      },
    };

    if (isLeaf) {
      elkNode.width = width;
      elkNode.height = height;
    }

    if (children.length > 0) {
      elkNode.children = children.map(convertNode);
    }

    return elkNode;
  }

  const rootNodes = visibleNodes.filter((n) => !n.parent_id || !nodeMap.has(n.parent_id));
  const visibleIdSet = new Set(visibleNodes.map((n) => n.id));
  const validEdges = edges.filter((e) => visibleIdSet.has(e.source) && visibleIdSet.has(e.target));
  const isSingleSubtreeLocalLayout = rootNodes.length === 1 && childMap.size > 0;
  const effectiveEdges = isSingleSubtreeLocalLayout
    ? validEdges.filter((e) => nodeMap.get(e.source)?.parent_id === nodeMap.get(e.target)?.parent_id)
    : validEdges;

  return {
    id: 'root',
    layoutOptions: {
      'elk.algorithm': 'layered',
      'elk.direction': 'RIGHT',
      'elk.spacing.nodeNode': '40',
      'elk.layered.spacing.nodeNodeBetweenLayers': '60',
      'elk.edgeRouting': isSingleSubtreeLocalLayout ? 'POLYLINE' : 'ORTHOGONAL',
      'elk.hierarchyHandling': 'INCLUDE_CHILDREN',
      'elk.padding': '[top=20,left=20,bottom=20,right=20]',
    },
    children: rootNodes.map(convertNode),
    edges: effectiveEdges.map((e) => ({
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
  ownerId = null,
  consistencyOnly = false,
  hideIsolated = false,
  foldHubEdgesEnabled = true,
  expandedHubNodeIds,
  searchKeyword = '',
  savedPositions = null,
  pinnedNodeIds,
  onFocusScope,
  onTogglePin,
  onExpandHub,
  onPositionsComputed,
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

  // 通过 Ref 读取非重排触发型属性，严格确保手动拖拽或 LOD 变化不触发 ELK Worker 重排
  const draggedNodeIdsRef = useRef<Set<string>>(draggedNodeIds);
  draggedNodeIdsRef.current = draggedNodeIds;
  const savedPositionsRef = useRef<Record<string, SavedNodePosition> | null>(savedPositions);
  savedPositionsRef.current = savedPositions;
  const pinnedNodeIdsRef = useRef<Set<string> | undefined>(pinnedNodeIds);
  pinnedNodeIdsRef.current = pinnedNodeIds;
  const onPositionsComputedRef = useRef(onPositionsComputed);
  onPositionsComputedRef.current = onPositionsComputed;
  const prevExpandRef = useRef<ExpandStateMap>(expandState);

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
          if (edge.data?.isDraggedDegraded) return edge;
          return {
            ...edge,
            data: edge.data ? { ...edge.data, isDraggedDegraded: true } : edge.data,
          };
        }
        return edge;
      })
    );
  }, [draggedNodeIds]);

  // 保持现有 flowNodes 中的 isPinned 状态与外部 pinnedNodeIds / savedPositions 同步，无需触发昂贵的 ELK Worker 重排
  useEffect(() => {
    setFlowNodes((prevNodes) => {
      if (prevNodes.length === 0) return prevNodes;
      let hasChanges = false;
      const nextNodes = prevNodes.map((n) => {
        const currentlyPinned = Boolean(
          pinnedNodeIds?.has(n.id) || savedPositions?.[n.id]?.pinned
        );
        if (n.data?.isPinned !== currentlyPinned) {
          hasChanges = true;
          return {
            ...n,
            data: {
              ...n.data,
              isPinned: currentlyPinned,
            },
          };
        }
        return n;
      });
      return hasChanges ? nextNodes : prevNodes;
    });
  }, [pinnedNodeIds, savedPositions]);

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

    // Stage 2: 聚焦范围裁剪、外部上下文摘要卡合成与多维降噪过滤
    const scoped = scopeAndFilter(overlay.workingNodes, overlay.workingEdges, expandState, {
      focusScope,
      seedNodeId,
      neighborhoodHop,
      neighborhoodDirection,
      ownerId,
      consistencyOnly,
      hideIsolated,
      searchKeyword,
      edgeStatusMap: overlay.edgeStatusMap,
    });
    setVisibleNodeCount(scoped.visibleIds.size);

    // Stage 3: 代表节点上卷与内部徽章聚合
    const aggregated = aggregateRollup(
      scoped.scopedNodes,
      scoped.scopedEdges,
      expandState,
      overlay
    );
    setBadgeCounts(aggregated.badgeCounts);

    // Stage 4: 高扇出 Hub 节点边折叠 + Tarjan SCC 环路检测
    const foldedEdges = foldHubEdges(aggregated.rollupEdges, scoped.visibleNodes, {
      foldHubEdges: foldHubEdgesEnabled,
      expandedHubNodeIds,
    });
    const visibleNodeIds = scoped.visibleNodes.map((n) => n.id);
    const cycleRes = detectCycles(visibleNodeIds, foldedEdges);

    // Stage 5: 构建 ELK 复合输入图（仅包含 visibleNodes）
    const elkInputGraph = buildElkGraph(scoped.visibleNodes, cycleRes.edges, expandState);

    const fullChildrenCountMap: Record<string, number> = {};
    for (const n of nodes) {
      if (n.parent_id) {
        fullChildrenCountMap[n.parent_id] = (fullChildrenCountMap[n.parent_id] || 0) + 1;
      }
    }

    const prevExpand = prevExpandRef.current;
    prevExpandRef.current = expandState;
    const currentReqId = ++layoutReqIdRef.current;

    const applyLayoutResult = (layoutedTree: ElkNode) => {
      if (currentReqId !== layoutReqIdRef.current) return;

      const elkCoords = extractNodeCoordinates(layoutedTree, 0, 0);
      const currSaved = savedPositionsRef.current;
      let effectivePositions: Record<string, SavedNodePosition> = {};
      const localTouchedIds = new Set<string>(draggedNodeIdsRef.current);

      // 判断是否为单容器展开/折叠触发的局部混合重排
      const changedExpandKeys = Array.from(
        new Set([...Object.keys(prevExpand), ...Object.keys(expandState)])
      ).filter((k) => Boolean(prevExpand[k]) !== Boolean(expandState[k]));

      if (currSaved && Object.keys(currSaved).length > 0 && changedExpandKeys.length === 1) {
        const toggledRootId = changedExpandKeys[0];
        const subtreeBoxes = new Map<string, NodeBox>();
        for (const vn of scoped.visibleNodes) {
          if (vn.id === toggledRootId || vn.ancestors.includes(toggledRootId)) {
            const c = elkCoords.get(vn.id);
            if (c) {
              subtreeBoxes.set(vn.id, {
                id: vn.id,
                parentId: vn.parent_id ?? null,
                x: c.x,
                y: c.y,
                width: c.width,
                height: c.height,
                pinned: pinnedNodeIdsRef.current?.has(vn.id) || currSaved[vn.id]?.pinned,
                isExpanded: Boolean(expandState[vn.id]),
              });
            }
          }
        }
        const merged = mergeLocalLayout({
          subtreeRootId: toggledRootId,
          subtreeBoxes,
          existingPositions: currSaved,
          nodes: scoped.visibleNodes,
          expandState,
        });
        effectivePositions = merged.positions;
        for (const tid of merged.touchedIds) {
          localTouchedIds.add(tid);
        }
      } else if (currSaved && Object.keys(currSaved).length > 0) {
        const boxMap = new Map<string, NodeBox>();
        for (const vn of scoped.visibleNodes) {
          const c = elkCoords.get(vn.id);
          const sp = currSaved[vn.id];
          boxMap.set(vn.id, {
            id: vn.id,
            parentId: vn.parent_id ?? null,
            x: sp ? sp.x : c?.x ?? 0,
            y: sp ? sp.y : c?.y ?? 0,
            width: c?.width ?? sp?.width ?? 260,
            height: c?.height ?? sp?.height ?? 80,
            pinned: pinnedNodeIdsRef.current?.has(vn.id) || sp?.pinned,
            isExpanded: Boolean(expandState[vn.id]),
          });
        }
        const resolved = resolveOverlapsBottomUp(boxMap, scoped.visibleNodes);
        for (const [id, box] of resolved.boxes.entries()) {
          effectivePositions[id] = {
            x: box.x,
            y: box.y,
            parent_id: box.parentId ?? null,
            is_expanded: Boolean(expandState[id]),
            pinned: box.pinned,
            width: box.width,
            height: box.height,
          };
        }
        for (const tid of resolved.touchedIds) {
          localTouchedIds.add(tid);
        }
      } else {
        for (const vn of scoped.visibleNodes) {
          const c = elkCoords.get(vn.id);
          effectivePositions[vn.id] = {
            x: c?.x ?? 0,
            y: c?.y ?? 0,
            parent_id: vn.parent_id ?? null,
            is_expanded: Boolean(expandState[vn.id]),
            pinned: pinnedNodeIdsRef.current?.has(vn.id),
            width: c?.width,
            height: c?.height,
          };
        }
      }

      onPositionsComputedRef.current?.(effectivePositions);

      const processed = processLayoutCoordinates(
        layoutedTree,
        scoped.visibleNodes,
        cycleRes.edges,
        localTouchedIds,
        expandState,
        aggregated.badgeCounts,
        onToggleExpand,
        fullChildrenCountMap,
        {
          savedPositions: effectivePositions,
          pinnedNodeIds: pinnedNodeIdsRef.current,
          onFocusScope,
          onTogglePin,
          onExpandHub,
          containerSearchMatchCounts,
          lodMode,
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
    ownerId,
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
