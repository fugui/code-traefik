import React, { useState, useCallback, useMemo, useRef } from 'react';
import {
  Info,
  ChevronUp,
  ChevronDown,
  Maximize2,
  CornerDownRight,
  GitBranch,
  Pin,
  Share2,
  X,
} from 'lucide-react';
import {
  ReactFlow,
  Controls,
  Background,
  MiniMap,
  useNodesState,
  useEdgesState,
  type OnNodeDrag,
  type NodeMouseHandler,
  type EdgeMouseHandler,
  type ReactFlowInstance,
  BackgroundVariant,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';

import {
  normalizeScopeKey,
  toScopedPositionKey,
  type GraphNode,
  type GraphEdge,
  type ExpandStateMap,
  type RollupEdgeModel,
  type SavedNodePosition,
  type SavedViewport,
  type NeighborhoodDirection,
  type TopologyStatusFilter,
} from '../../types/topology_graph';
import type { ConsistencyReportDTO } from '../../types/traffic';
import { useElkLayout } from './layout/useElkLayout';
import type { ArchCanvasNode, ArchCanvasEdge, LodMode } from './layout/layoutProcessor';
import {
  computeSpotlightSet,
  computeNodeRelations,
  LOD_COMPACT_ZOOM,
  LOD_DETAIL_ZOOM,
} from './model/noiseReduction';
import { ContainerNode } from './nodes/ContainerNode';
import { ModuleNode } from './nodes/ModuleNode';
import { ExternalNode } from './nodes/ExternalNode';
import { ExternalContextNode } from './nodes/ExternalContextNode';
import { UnregisteredInterfaceNode } from './nodes/UnregisteredInterfaceNode';
import { RollupEdge } from './edges/RollupEdge';
import type { NodeInspectTab } from './panels/NodeInspectDrawer';
import {
  TopologyBreadcrumbBar,
  type FocusBreadcrumbItem,
} from './panels/TopologyBreadcrumbBar';
import './ArchCanvas.css';

export const SPOTLIGHT_HOVER_DEBOUNCE_MS = 90;
export const PULSE_HIGHLIGHT_DURATION_MS = 2000;

function isContainerNodeType(type?: string): boolean {
  return (
    type === 'SUBSYSTEM' ||
    type === 'FUNCTIONAL_GROUP' ||
    type === 'PROCESS_GROUP' ||
    type === 'PROCESS' ||
    type === 'UNDEPLOYED_CONTAINER' ||
    type === 'EXTERNAL_CONTAINER'
  );
}

export interface ArchCanvasProps {
  nodes: GraphNode[];
  edges: GraphEdge[];
  expandState: ExpandStateMap;
  onToggleExpand: (nodeId: string) => void;
  onSelectNode?: (node: GraphNode | null) => void;
  onInspectNode?: (node: GraphNode, tab?: NodeInspectTab) => void;
  onSelectEdge?: (edge: RollupEdgeModel) => void;
  selectedNodeId?: string | null;
  consistencyReport?: ConsistencyReportDTO | null;
  view: 'LOGICAL' | 'PHYSICAL';
  className?: string;
  resetKey?: number;
  searchKeyword?: string;
  wheelAutoExpand?: boolean;
  depthLevel?: number;
  maxDepth?: number;
  onDepthLevelChange?: (level: number) => void;
  focusScope?: string;
  breadcrumbs?: FocusBreadcrumbItem[];
  showBreadcrumbBar?: boolean;
  seedNodeId?: string | null;
  neighborhoodHop?: number;
  neighborhoodDirection?: NeighborhoodDirection;
  onNeighborhoodHopChange?: (hop: number) => void;
  onNeighborhoodDirectionChange?: (dir: NeighborhoodDirection) => void;
  subsystemIds?: string[];
  onlyMyScope?: boolean;
  ownerId?: number | null;
  statusFilter?: TopologyStatusFilter;
  consistencyOnly?: boolean;
  hideIsolated?: boolean;
  foldHubEdgesEnabled?: boolean;
  expandedHubNodeIds?: Set<string>;
  onExpandHub?: (hubNodeId: string) => void;
  savedPositions?: Record<string, SavedNodePosition> | null;
  pinnedNodeIds?: Set<string>;
  highlightedNodeId?: string | null;
  onFocusScope?: (nodeId: string) => void;
  onTogglePin?: (nodeId: string) => void;
  onNodePositionChange?: (nodeId: string, position: { x: number; y: number }) => void;
  onPositionsComputed?: (positions: Record<string, SavedNodePosition>) => void;
  initialViewport?: SavedViewport | null;
  onViewportChange?: (viewport: SavedViewport) => void;
  showRealization?: boolean;
  contractMode?: 'AGG' | 'EXPAND';
  containerSearchMatchCounts?: Record<string, number>;
  onVisibleRollupEdgesChange?: (edges: RollupEdgeModel[]) => void;
  panToNodeRequest?: { nodeId: string; seq: number } | null;
}

export const ArchCanvas: React.FC<ArchCanvasProps> = ({
  nodes,
  edges,
  expandState,
  onToggleExpand,
  onSelectNode,
  onInspectNode,
  onSelectEdge,
  selectedNodeId,
  consistencyReport,
  view,
  className = '',
  resetKey,
  searchKeyword,
  focusScope = 'GLOBAL',
  breadcrumbs,
  showBreadcrumbBar = true,
  seedNodeId = null,
  neighborhoodHop = 0,
  neighborhoodDirection = 'BOTH',
  onNeighborhoodHopChange,
  onNeighborhoodDirectionChange,
  subsystemIds,
  onlyMyScope = false,
  ownerId = null,
  statusFilter = 'ALL',
  consistencyOnly = false,
  hideIsolated = false,
  foldHubEdgesEnabled = true,
  expandedHubNodeIds,
  onExpandHub,
  savedPositions = null,
  pinnedNodeIds,
  highlightedNodeId = null,
  onFocusScope,
  onTogglePin,
  onNodePositionChange,
  showRealization = true,
  contractMode = 'AGG',
  initialViewport = null,
  onViewportChange,
  containerSearchMatchCounts,
  onVisibleRollupEdgesChange,
  panToNodeRequest = null,
}) => {
  const [draggedNodeIds, setDraggedNodeIds] = useState<Set<string>>(new Set());
  const [rfInstance, setRfInstance] = useState<ReactFlowInstance<ArchCanvasNode, ArchCanvasEdge> | null>(null);
  const [isLegendExpanded, setIsLegendExpanded] = useState<boolean>(true);
  const [lodMode, setLodMode] = useState<LodMode>('STANDARD');

  // 三态分离：selectedId (持久选中锁定)、hoverId (无选中时临时悬停预览)、pulseNodeId (跨层聚焦脉冲 2s)
  const [selectedId, setSelectedId] = useState<string | null>(selectedNodeId ?? null);
  const [hoverId, setHoverId] = useState<string | null>(null);
  const [pulseNodeId, setPulseNodeId] = useState<string | null>(null);

  const initialFitDoneRef = useRef<boolean>(false);
  const hoverTimerRef = useRef<number | null>(null);
  const pulseTimerRef = useRef<number | null>(null);
  const lastHandledPanSeqRef = useRef<number | null>(null);

  const normalizedScope = normalizeScopeKey(focusScope);

  // 同步外部受控 selectedNodeId
  React.useEffect(() => {
    if (selectedNodeId !== undefined) {
      setSelectedId(selectedNodeId);
    }
  }, [selectedNodeId]);

  // 组件卸载时清理计时器
  React.useEffect(() => {
    return () => {
      if (hoverTimerRef.current) {
        window.clearTimeout(hoverTimerRef.current);
        hoverTimerRef.current = null;
      }
      if (pulseTimerRef.current) {
        window.clearTimeout(pulseTimerRef.current);
        pulseTimerRef.current = null;
      }
    };
  }, []);

  // 响应自动整理 / 重置布局触发
  React.useEffect(() => {
    setDraggedNodeIds(new Set());
    if (rfInstance && resetKey != null && resetKey > 0) {
      void rfInstance.fitView({ padding: 0.18, duration: 280 });
    }
  }, [resetKey, rfInstance]);

  // 恢复保存的视口坐标 (当切换书签或基准时)
  React.useEffect(() => {
    if (rfInstance && initialViewport && Number.isFinite(initialViewport.zoom)) {
      void rfInstance.setViewport(initialViewport, { duration: 220 });
      initialFitDoneRef.current = true;
    }
  }, [rfInstance, initialViewport]);

  const nodeTypes = useMemo(
    () => ({
      containerNode: ContainerNode,
      moduleCard: ModuleNode,
      externalNode: ExternalNode,
      externalContextNode: ExternalContextNode,
      unregisteredInterfaceNode: UnregisteredInterfaceNode,
    }),
    []
  );

  const edgeTypes = useMemo(
    () => ({
      rollupEdge: RollupEdge,
    }),
    []
  );

  const effectivePulseId = pulseNodeId || highlightedNodeId || null;

  const { flowNodes: layoutNodes, flowEdges: layoutEdges, isLayouting } = useElkLayout({
    nodes,
    edges,
    expandState,
    consistencyReport,
    view,
    draggedNodeIds,
    onToggleExpand,
    focusScope: normalizedScope,
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
    savedPositions,
    pinnedNodeIds,
    highlightedNodeId: effectivePulseId,
    onFocusScope,
    onTogglePin,
    onExpandHub,
    containerSearchMatchCounts,
    lodMode,
    showRealization,
    contractMode,
  });

  const [flowNodes, setFlowNodes, onNodesChange] = useNodesState<ArchCanvasNode>(layoutNodes);
  const [flowEdges, setFlowEdges, onEdgesChange] = useEdgesState<ArchCanvasEdge>(layoutEdges);

  React.useEffect(() => {
    setFlowNodes(layoutNodes);
    setFlowEdges(layoutEdges);
  }, [layoutNodes, layoutEdges, setFlowNodes, setFlowEdges]);

  // 等待 ELK 布局就绪后响应外部「定位 / 跨层搜索飞跃」请求并激活 2s 脉冲高亮
  React.useEffect(() => {
    if (!panToNodeRequest?.nodeId || isLayouting) return;
    if (lastHandledPanSeqRef.current === panToNodeRequest.seq) return;
    const existsOnLayer = flowNodes.some((n) => n.id === panToNodeRequest.nodeId);
    if (!existsOnLayer) return;

    lastHandledPanSeqRef.current = panToNodeRequest.seq;
    setSelectedId(panToNodeRequest.nodeId);
    setPulseNodeId(panToNodeRequest.nodeId);

    if (pulseTimerRef.current) {
      window.clearTimeout(pulseTimerRef.current);
    }
    pulseTimerRef.current = window.setTimeout(() => {
      pulseTimerRef.current = null;
      setPulseNodeId(null);
    }, PULSE_HIGHLIGHT_DURATION_MS);

    if (rfInstance) {
      void rfInstance.fitView({
        nodes: [{ id: panToNodeRequest.nodeId }],
        padding: 0.35,
        maxZoom: 1.15,
        duration: 360,
      });
    }
  }, [rfInstance, panToNodeRequest, isLayouting, flowNodes]);

  // 当 pinnedNodeIds / savedPositions / pulse 变更时，就地同步 flowNodes 内的 isPinned 与 isHighlighted
  React.useEffect(() => {
    setFlowNodes((prevNodes) => {
      let changed = false;
      const next = prevNodes.map((node) => {
        const scopedKey = toScopedPositionKey(normalizedScope, node.id);
        const isPinned = Boolean(
          pinnedNodeIds?.has(scopedKey) ||
            pinnedNodeIds?.has(node.id) ||
            savedPositions?.[scopedKey]?.pinned ||
            savedPositions?.[node.id]?.pinned
        );
        const isHighlighted = effectivePulseId === node.id;
        if (
          node.data?.isPinned !== isPinned ||
          Boolean(node.data?.isHighlighted) !== isHighlighted
        ) {
          changed = true;
          return {
            ...node,
            data: {
              ...node.data,
              isPinned,
              isHighlighted,
            },
          };
        }
        return node;
      });
      return changed ? next : prevNodes;
    });
  }, [pinnedNodeIds, savedPositions, normalizedScope, effectivePulseId, setFlowNodes]);

  // 仅在冷启动或作用域切换后首次排版完成且无预设视口时执行 fitView
  React.useEffect(() => {
    initialFitDoneRef.current = false;
  }, [normalizedScope]);

  React.useEffect(() => {
    if (!rfInstance || layoutNodes.length === 0 || isLayouting) return;
    if (initialFitDoneRef.current) return;
    initialFitDoneRef.current = true;
    if (!initialViewport) {
      void rfInstance.fitView({ padding: 0.18, duration: 240 });
    }
  }, [rfInstance, layoutNodes.length, isLayouting, initialViewport]);

  const handleFitView = useCallback(() => {
    if (rfInstance) {
      void rfInstance.fitView({ padding: 0.18, duration: 250 });
    }
  }, [rfInstance]);

  // 单层平铺模型下仅记录拖拽位移标记并持久化到当前 focusScope
  const handleNodeDrag: OnNodeDrag = useCallback((_, draggedNode) => {
    setDraggedNodeIds((prev) => {
      if (prev.has(draggedNode.id)) return prev;
      const next = new Set(prev);
      next.add(draggedNode.id);
      return next;
    });
  }, []);

  const handleNodeDragStop: OnNodeDrag = useCallback(
    (_, node) => {
      if (onNodePositionChange) {
        onNodePositionChange(node.id, node.position);
      }
    },
    [onNodePositionChange]
  );

  const handleClearSelection = useCallback(() => {
    setSelectedId(null);
    setHoverId(null);
    onSelectNode?.(null);
  }, [onSelectNode]);

  // 单击节点：立即响应选中锁定
  const handleNodeClick: NodeMouseHandler = useCallback(
    (_, node) => {
      if (hoverTimerRef.current) {
        window.clearTimeout(hoverTimerRef.current);
        hoverTimerRef.current = null;
      }
      setSelectedId(node.id);
      const gn = (node.data as { graphNode?: GraphNode })?.graphNode;
      if (gn && onSelectNode) {
        onSelectNode(gn);
      }
    },
    [onSelectNode]
  );

  // 双击节点（单层平铺逐级下钻契约）：
  // - 外部摘要卡：进入对端容器作用域
  // - 容器节点：直接下钻进入该容器作用域
  // - 叶子模块/外部系统：打开画像抽屉「上下游关系」页
  const handleNodeDoubleClick: NodeMouseHandler = useCallback(
    (_, node) => {
      if (hoverTimerRef.current) {
        window.clearTimeout(hoverTimerRef.current);
        hoverTimerRef.current = null;
      }

      const gn = (node.data as { graphNode?: GraphNode })?.graphNode;
      if (!gn) return;

      if (gn.type === 'EXTERNAL_CONTEXT') {
        const peerRootId = gn.meta?.peer_root_id ? String(gn.meta.peer_root_id) : gn.id;
        if (onFocusScope && peerRootId) {
          onFocusScope(peerRootId);
        }
        return;
      }

      if (isContainerNodeType(gn.type)) {
        if (onFocusScope && normalizedScope !== gn.id) {
          onFocusScope(gn.id);
        } else if (onInspectNode) {
          onInspectNode(gn, 'RELATIONS');
        }
        return;
      }

      setSelectedId(node.id);
      if (onInspectNode) {
        onInspectNode(gn, 'RELATIONS');
      } else if (onSelectNode) {
        onSelectNode(gn);
      }
    },
    [normalizedScope, onFocusScope, onInspectNode, onSelectNode]
  );

  const handlePaneClick = useCallback(() => {
    if (hoverTimerRef.current) {
      window.clearTimeout(hoverTimerRef.current);
      hoverTimerRef.current = null;
    }
    handleClearSelection();
  }, [handleClearSelection]);

  // 画布空白处双击：若处于聚焦子图态，返回上一级（或退回全局全景）
  const handleCanvasDoubleClick = useCallback(
    (e: React.MouseEvent<HTMLDivElement>) => {
      const target = e.target as HTMLElement | null;
      if (!target || !target.classList.contains('react-flow__pane')) return;
      if (normalizedScope === 'GLOBAL' || !onFocusScope) return;

      const currentFocusNode = nodes.find((n) => n.id === normalizedScope);
      const parentScope = currentFocusNode?.parent_id || 'GLOBAL';
      onFocusScope(parentScope);
    },
    [normalizedScope, nodes, onFocusScope]
  );

  // 鼠标悬停防抖聚光灯预览
  const handleNodeMouseEnter: NodeMouseHandler = useCallback((_, node) => {
    if (hoverTimerRef.current) {
      window.clearTimeout(hoverTimerRef.current);
      hoverTimerRef.current = null;
    }
    hoverTimerRef.current = window.setTimeout(() => {
      hoverTimerRef.current = null;
      setHoverId(node.id);
    }, SPOTLIGHT_HOVER_DEBOUNCE_MS);
  }, []);

  const handleNodeMouseLeave: NodeMouseHandler = useCallback(() => {
    if (hoverTimerRef.current) {
      window.clearTimeout(hoverTimerRef.current);
      hoverTimerRef.current = null;
    }
    setHoverId(null);
  }, []);

  const handleEdgeClick: EdgeMouseHandler = useCallback(
    (_, edge) => {
      setSelectedId((prev) => (prev === edge.id ? null : edge.id));
      const edgeModel = edge.data as RollupEdgeModel | undefined;
      if (edgeModel && onSelectEdge) {
        onSelectEdge(edgeModel);
      }
    },
    [onSelectEdge]
  );

  const handleMoveEnd = useCallback(
    (_: unknown, viewport: { x: number; y: number; zoom: number }) => {
      const nextLod: LodMode =
        viewport.zoom < LOD_COMPACT_ZOOM
          ? 'COMPACT'
          : viewport.zoom >= LOD_DETAIL_ZOOM
          ? 'DETAIL'
          : 'STANDARD';
      setLodMode((curr) => (curr !== nextLod ? nextLod : curr));

      if (onViewportChange) {
        onViewportChange(viewport);
      }
    },
    [onViewportChange]
  );

  // 当前单层可见节点与汇总边
  const visibleGraphNodes = useMemo(
    () =>
      flowNodes
        .map((n) => (n.data as { graphNode?: GraphNode })?.graphNode)
        .filter((gn): gn is GraphNode => Boolean(gn)),
    [flowNodes]
  );

  const visibleRollupModels = useMemo(
    () =>
      flowEdges
        .map((e) => e.data)
        .filter((d): d is RollupEdgeModel => Boolean(d)),
    [flowEdges]
  );

  React.useEffect(() => {
    onVisibleRollupEdgesChange?.(visibleRollupModels);
  }, [visibleRollupModels, onVisibleRollupEdgesChange]);

  // 自动推导面包屑链路（当外部未传入时）
  const computedBreadcrumbs: FocusBreadcrumbItem[] = useMemo(() => {
    if (breadcrumbs && breadcrumbs.length > 0) return breadcrumbs;
    const base: FocusBreadcrumbItem[] = [{ id: 'GLOBAL', name: '全局全景' }];
    if (normalizedScope === 'GLOBAL') return base;
    const nodeMap = new Map(nodes.map((n) => [n.id, n]));
    const target = nodeMap.get(normalizedScope);
    if (!target) {
      base.push({ id: normalizedScope, name: normalizedScope });
      return base;
    }
    for (const ancId of target.ancestors) {
      const anc = nodeMap.get(ancId);
      if (anc) base.push({ id: anc.id, name: anc.name });
    }
    base.push({ id: target.id, name: target.name });
    return base;
  }, [breadcrumbs, normalizedScope, nodes]);

  const layerCounts = useMemo(() => {
    let containerCount = 0;
    let moduleCount = 0;
    let externalCount = 0;
    for (const gn of visibleGraphNodes) {
      if (gn.type === 'EXTERNAL_CONTEXT') {
        externalCount += 1;
      } else if (isContainerNodeType(gn.type)) {
        containerCount += 1;
      } else {
        moduleCount += 1;
      }
    }
    return {
      containerCount,
      moduleCount,
      externalCount,
      edgeCount: visibleRollupModels.length,
    };
  }, [visibleGraphNodes, visibleRollupModels.length]);

  // 键盘快捷键：Esc 清除选中/返回上一层，Backspace 返回上一层，Enter 进入容器，I 打开关系详情
  React.useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      const activeTag = (document.activeElement as HTMLElement | null)?.tagName;
      if (activeTag === 'INPUT' || activeTag === 'TEXTAREA' || activeTag === 'SELECT') {
        return;
      }

      if (e.key === 'Escape') {
        if (selectedId) {
          handleClearSelection();
        } else if (normalizedScope !== 'GLOBAL' && onFocusScope) {
          const currentFocusNode = nodes.find((n) => n.id === normalizedScope);
          onFocusScope(currentFocusNode?.parent_id || 'GLOBAL');
        }
        return;
      }

      if (e.key === 'Backspace' && normalizedScope !== 'GLOBAL' && onFocusScope) {
        const currentFocusNode = nodes.find((n) => n.id === normalizedScope);
        onFocusScope(currentFocusNode?.parent_id || 'GLOBAL');
        return;
      }

      if (!selectedId) return;
      const gn = visibleGraphNodes.find((n) => n.id === selectedId);
      if (!gn) return;

      if (e.key === 'Enter') {
        e.preventDefault();
        if (isContainerNodeType(gn.type)) {
          if (onFocusScope && normalizedScope !== gn.id) {
            onFocusScope(gn.id);
          }
        } else if (gn.type === 'EXTERNAL_CONTEXT') {
          const peerRootId = gn.meta?.peer_root_id ? String(gn.meta.peer_root_id) : undefined;
          if (onFocusScope && peerRootId) onFocusScope(peerRootId);
        } else if (onInspectNode) {
          onInspectNode(gn, 'RELATIONS');
        }
      } else if (e.key === 'i' || e.key === 'I') {
        e.preventDefault();
        onInspectNode?.(gn, 'RELATIONS');
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [
    selectedId,
    normalizedScope,
    nodes,
    visibleGraphNodes,
    handleClearSelection,
    onFocusScope,
    onInspectNode,
  ]);

  const effectiveSpotlightId = selectedId ?? hoverId;

  const spotlight = useMemo(
    () => computeSpotlightSet(effectiveSpotlightId, visibleGraphNodes, visibleRollupModels),
    [effectiveSpotlightId, visibleGraphNodes, visibleRollupModels]
  );

  const selectedGraphNode = useMemo(
    () => (selectedId ? visibleGraphNodes.find((n) => n.id === selectedId) ?? null : null),
    [selectedId, visibleGraphNodes]
  );

  const selectedNodeRelations = useMemo(
    () => computeNodeRelations(selectedGraphNode?.id, nodes, visibleRollupModels),
    [selectedGraphNode?.id, nodes, visibleRollupModels]
  );

  // 关键词检索高亮 + 分层聚光灯 + 搜索定位脉冲 + LOD 样式注入
  const highlightedFlowNodes = useMemo(() => {
    const kw = searchKeyword?.trim().toLowerCase();
    return flowNodes.map((n) => {
      const gn = (n.data as { graphNode?: GraphNode })?.graphNode;
      const match =
        Boolean(kw) &&
        Boolean(
          gn &&
            (gn.name.toLowerCase().includes(kw!) ||
              (gn.code && gn.code.toLowerCase().includes(kw!)) ||
              gn.id.toLowerCase().includes(kw!))
        );
      const isSelected = selectedId === n.id;
      const isHovered = hoverId === n.id && selectedId !== n.id;
      const isSubject = spotlight.active && spotlight.subjectNodeIds.has(n.id);
      const isUpstream = spotlight.active && spotlight.upstreamNodeIds.has(n.id);
      const isDownstream = spotlight.active && spotlight.downstreamNodeIds.has(n.id);
      const isContext = spotlight.active && spotlight.contextAncestorIds.has(n.id);
      const dimmed = spotlight.active && !spotlight.highlightedNodeIds.has(n.id);
      const isPulsed = effectivePulseId === n.id;

      const classes = [
        n.className || '',
        match ? 'code-arch-node--search-match' : '',
        isPulsed ? 'code-arch-node--pulse-highlight' : '',
        isSelected ? 'code-arch-node--selected' : '',
        isHovered ? 'code-arch-node--hovered' : '',
        isSubject ? 'code-arch-node--spotlight-subject' : '',
        isUpstream ? 'code-arch-node--spotlight-upstream' : '',
        isDownstream ? 'code-arch-node--spotlight-downstream' : '',
        isContext ? 'code-arch-node--spotlight-context' : '',
        dimmed ? 'code-arch-node--spotlight-dimmed' : '',
        lodMode === 'COMPACT' ? 'code-arch-node--lod-compact' : '',
      ]
        .filter(Boolean)
        .join(' ');

      return {
        ...n,
        className: classes,
      };
    });
  }, [flowNodes, searchKeyword, selectedId, hoverId, spotlight, effectivePulseId, lodMode]);

  const highlightedFlowEdges = useMemo(() => {
    if (!spotlight.active) return flowEdges;
    return flowEdges.map((e) => {
      const dimmed = !spotlight.highlightedEdgeIds.has(e.id);
      const spotlightRole: 'UPSTREAM' | 'DOWNSTREAM' | 'INTERNAL' | undefined =
        spotlight.upstreamEdgeIds.has(e.id)
          ? 'UPSTREAM'
          : spotlight.downstreamEdgeIds.has(e.id)
          ? 'DOWNSTREAM'
          : spotlight.internalEdgeIds.has(e.id)
          ? 'INTERNAL'
          : undefined;
      return {
        ...e,
        data: e.data ? { ...e.data, isSpotlightDimmed: dimmed, spotlightRole } : e.data,
      };
    });
  }, [flowEdges, spotlight]);

  const isSelectedContainer = Boolean(
    selectedGraphNode && isContainerNodeType(selectedGraphNode.type)
  );
  const selectedScopedKey = selectedGraphNode
    ? toScopedPositionKey(normalizedScope, selectedGraphNode.id)
    : '';
  const isSelectedPinned = Boolean(
    selectedGraphNode &&
      (pinnedNodeIds?.has(selectedScopedKey) ||
        pinnedNodeIds?.has(selectedGraphNode.id) ||
        savedPositions?.[selectedScopedKey]?.pinned ||
        savedPositions?.[selectedGraphNode.id]?.pinned)
  );
  const totalSelectedRelations =
    selectedNodeRelations.upstream.length + selectedNodeRelations.downstream.length;

  const handleCycleDirection = useCallback(() => {
    if (!onNeighborhoodDirectionChange) return;
    const nextDir: NeighborhoodDirection =
      neighborhoodDirection === 'BOTH'
        ? 'UPSTREAM'
        : neighborhoodDirection === 'UPSTREAM'
        ? 'DOWNSTREAM'
        : 'BOTH';
    onNeighborhoodDirectionChange(nextDir);
  }, [neighborhoodDirection, onNeighborhoodDirectionChange]);

  return (
    <div
      className={`code-arch-canvas ${className}`}
      data-testid="arch-canvas"
      onDoubleClick={handleCanvasDoubleClick}
    >
      {/* 画布顶置单层面包屑导航条 */}
      {showBreadcrumbBar && (
        <TopologyBreadcrumbBar
          focusScope={normalizedScope}
          breadcrumbs={computedBreadcrumbs}
          onNavigateScope={(scope) => onFocusScope?.(scope)}
          containerCount={layerCounts.containerCount}
          moduleCount={layerCounts.moduleCount}
          externalCount={layerCounts.externalCount}
          edgeCount={layerCounts.edgeCount}
        />
      )}

      <div className="code-arch-canvas-stage">
        {isLayouting && (
          <div className="code-arch-layouting-indicator" data-testid="arch-layouting-indicator">
            正交排版计算中...
          </div>
        )}

        {/* 选中节点上下文浮动操作条 (Selection Floating Action Bar) */}
        {selectedGraphNode && (
          <div className="code-arch-action-bar" data-testid="arch-node-action-bar">
            <div className="code-arch-action-bar__identity">
              <span className="code-arch-badge code-arch-badge--provide">
                {selectedGraphNode.code || selectedGraphNode.type}
              </span>
              <span className="code-arch-action-bar__name" title={selectedGraphNode.name}>
                {selectedGraphNode.name}
              </span>
            </div>

            <div className="code-arch-action-bar__divider" />

            <div className="code-arch-action-bar__buttons">
              {isSelectedContainer && onFocusScope && normalizedScope !== selectedGraphNode.id && (
                <button
                  type="button"
                  className="code-arch-action-bar__btn code-arch-action-bar__btn--primary"
                  onClick={() => onFocusScope(selectedGraphNode.id)}
                  data-testid="action-bar-focus-btn"
                  title="下钻进入该容器内部单层平铺视图 (Enter / 双击)"
                >
                  <CornerDownRight size={12} />
                  <span>进入容器 ▸</span>
                </button>
              )}

              {onTogglePin && (
                <button
                  type="button"
                  className={`code-arch-action-bar__btn ${
                    isSelectedPinned ? 'code-arch-action-bar__btn--active' : ''
                  }`}
                  onClick={() => onTogglePin(selectedGraphNode.id)}
                  data-testid="action-bar-pin-btn"
                  title={isSelectedPinned ? '取消固定位置' : '固定该元素在当前作用域中的坐标位置'}
                >
                  <Pin size={12} />
                  <span>{isSelectedPinned ? '已固定' : '固定'}</span>
                </button>
              )}

              {onNeighborhoodHopChange && (
                <>
                  <button
                    type="button"
                    className={`code-arch-action-bar__btn ${
                      neighborhoodHop === 1 ? 'code-arch-action-bar__btn--active' : ''
                    }`}
                    onClick={() => onNeighborhoodHopChange(neighborhoodHop === 1 ? 0 : 1)}
                    data-testid="action-bar-hop-1-btn"
                    title="仅过滤展示该节点的 1 跳上下游邻域子图"
                  >
                    <Share2 size={12} />
                    <span>1跳邻域</span>
                  </button>
                  <button
                    type="button"
                    className={`code-arch-action-bar__btn ${
                      neighborhoodHop === 2 ? 'code-arch-action-bar__btn--active' : ''
                    }`}
                    onClick={() => onNeighborhoodHopChange(neighborhoodHop === 2 ? 0 : 2)}
                    data-testid="action-bar-hop-2-btn"
                    title="仅过滤展示该节点的 2 跳上下游邻域子图"
                  >
                    <span>2跳</span>
                  </button>
                  {neighborhoodHop > 0 && onNeighborhoodDirectionChange && (
                    <button
                      type="button"
                      className="code-arch-action-bar__btn"
                      onClick={handleCycleDirection}
                      data-testid="action-bar-hop-dir-btn"
                      title="切换邻域追踪方向 (双向 / 仅上游 / 仅下游)"
                    >
                      <span>
                        {neighborhoodDirection === 'BOTH'
                          ? '⇅双向'
                          : neighborhoodDirection === 'UPSTREAM'
                          ? '↑上游'
                          : '↓下游'}
                      </span>
                    </button>
                  )}
                </>
              )}

              {onInspectNode && (
                <button
                  type="button"
                  className="code-arch-action-bar__btn"
                  onClick={() => onInspectNode(selectedGraphNode, 'RELATIONS')}
                  data-testid="action-bar-relations-btn"
                  title="打开画像抽屉查看上下游调用关系并逐跳追踪 (快捷键 I)"
                >
                  <GitBranch size={12} />
                  <span>
                    关系 ({selectedNodeRelations.upstream.length}↑ ·{' '}
                    {selectedNodeRelations.downstream.length}↓
                    {totalSelectedRelations === 0 && selectedNodeRelations.internalCount > 0
                      ? ` · ${selectedNodeRelations.internalCount}内`
                      : ''}
                    )
                  </span>
                </button>
              )}

              {onInspectNode && (
                <button
                  type="button"
                  className="code-arch-action-bar__btn"
                  onClick={() => onInspectNode(selectedGraphNode, 'OVERVIEW')}
                  data-testid="action-bar-overview-btn"
                  title="查看架构元素基础标识与运行元数据"
                >
                  <Info size={12} />
                  <span>详情</span>
                </button>
              )}

              <button
                type="button"
                className="code-arch-action-bar__btn code-arch-action-bar__btn--close"
                onClick={handleClearSelection}
                data-testid="action-bar-clear-btn"
                title="取消选中并恢复全局亮度 (Esc)"
              >
                <X size={12} />
              </button>
            </div>
          </div>
        )}

        {/* 悬浮全景图例与视口辅助 HUD */}
        <div className="code-arch-legend-hud" data-testid="arch-legend-hud">
          <div
            className="code-arch-legend-hud__header"
            onClick={() => setIsLegendExpanded((prev) => !prev)}
            title="点击折叠/展开拓扑图例"
          >
            <span className="code-arch-legend-hud__title">
              <Info size={12} />
              <span>架构图例</span>
            </span>
            <div className="code-arch-legend-hud__actions">
              <button
                type="button"
                className="code-arch-toolbar__quick-depth-btn"
                onClick={(e) => {
                  e.stopPropagation();
                  handleFitView();
                }}
                title="一键自适应居中铺满画布"
              >
                <Maximize2 size={10} />
                <span>居中</span>
              </button>
              {isLegendExpanded ? <ChevronUp size={12} /> : <ChevronDown size={12} />}
            </div>
          </div>

          {isLegendExpanded && (
            <div className="code-arch-legend-hud__items">
              <div className="code-arch-legend-item">
                <span className="code-arch-legend-badge code-arch-legend-badge--l0" />
                <span>{view === 'PHYSICAL' ? '进程组 (L0)' : '子系统 (L0)'}</span>
              </div>
              <div className="code-arch-legend-item">
                <span className="code-arch-legend-badge code-arch-legend-badge--l1" />
                <span>{view === 'PHYSICAL' ? '物理进程 (L1)' : '功能组 (L1)'}</span>
              </div>
              <div className="code-arch-legend-item">
                <span className="code-arch-legend-badge code-arch-legend-badge--l2" />
                <span>微服务模块 (L2)</span>
              </div>
              <div className="code-arch-legend-item">
                <span className="code-arch-legend-badge code-arch-legend-badge--external" />
                <span>外部依赖 / 上下文摘要</span>
              </div>
              <div className="code-arch-legend-item">
                <span className="code-arch-legend-badge code-arch-legend-badge--edge" />
                <span>跨进程调用 / 契约链路</span>
              </div>
            </div>
          )}
        </div>

        <ReactFlow<ArchCanvasNode, ArchCanvasEdge>
          onInit={setRfInstance}
          nodes={highlightedFlowNodes}
          edges={highlightedFlowEdges}
          nodeTypes={nodeTypes}
          edgeTypes={edgeTypes}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onNodeDrag={handleNodeDrag}
          onNodeDragStop={handleNodeDragStop}
          onNodeClick={handleNodeClick}
          onNodeDoubleClick={handleNodeDoubleClick}
          onPaneClick={handlePaneClick}
          onNodeMouseEnter={handleNodeMouseEnter}
          onNodeMouseLeave={handleNodeMouseLeave}
          onEdgeClick={handleEdgeClick}
          onMoveEnd={handleMoveEnd}
          zoomOnDoubleClick={false}
          fitView={!initialViewport}
          proOptions={{ hideAttribution: true }}
          minZoom={0.15}
          maxZoom={2.0}
        >
          <Controls showInteractive={false} />
          <Background variant={BackgroundVariant.Dots} gap={16} size={1} />
          <MiniMap
            nodeStrokeWidth={2}
            zoomable
            pannable
            nodeColor={() => 'var(--color-primary)'}
            maskColor="var(--color-bg-muted)"
          />
        </ReactFlow>
      </div>
    </div>
  );
};
