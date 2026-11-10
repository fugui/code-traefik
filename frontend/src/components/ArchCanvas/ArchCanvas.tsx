import React, { useState, useCallback, useMemo, useRef } from 'react';
import {
  Info,
  ChevronUp,
  ChevronDown,
  Maximize2,
  Crosshair,
  GitBranch,
  Pin,
  FolderOpen,
  FolderMinus,
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

import type {
  GraphNode,
  GraphEdge,
  ExpandStateMap,
  RollupEdgeModel,
  SavedNodePosition,
  SavedViewport,
  NeighborhoodDirection,
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
import './ArchCanvas.css';

export const SPOTLIGHT_HOVER_DEBOUNCE_MS = 90;

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
  seedNodeId?: string | null;
  neighborhoodHop?: number;
  neighborhoodDirection?: NeighborhoodDirection;
  onNeighborhoodHopChange?: (hop: number) => void;
  onNeighborhoodDirectionChange?: (dir: NeighborhoodDirection) => void;
  ownerId?: number | null;
  consistencyOnly?: boolean;
  hideIsolated?: boolean;
  foldHubEdgesEnabled?: boolean;
  expandedHubNodeIds?: Set<string>;
  onExpandHub?: (hubNodeId: string) => void;
  savedPositions?: Record<string, SavedNodePosition> | null;
  pinnedNodeIds?: Set<string>;
  onFocusScope?: (nodeId: string) => void;
  onTogglePin?: (nodeId: string) => void;
  onNodePositionChange?: (nodeId: string, position: { x: number; y: number }) => void;
  onPositionsComputed?: (positions: Record<string, SavedNodePosition>) => void;
  initialViewport?: SavedViewport | null;
  onViewportChange?: (viewport: SavedViewport) => void;
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
  wheelAutoExpand,
  depthLevel = 0,
  maxDepth = 0,
  onDepthLevelChange,
  focusScope = 'GLOBAL',
  seedNodeId = null,
  neighborhoodHop = 0,
  neighborhoodDirection = 'BOTH',
  onNeighborhoodHopChange,
  onNeighborhoodDirectionChange,
  ownerId = null,
  consistencyOnly = false,
  hideIsolated = false,
  foldHubEdgesEnabled = true,
  expandedHubNodeIds,
  onExpandHub,
  savedPositions = null,
  pinnedNodeIds,
  onFocusScope,
  onTogglePin,
  onNodePositionChange,
  onPositionsComputed,
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

  // 三态分离：selectedId (持久选中锁定) 与 hoverId (无选中时临时悬停预览)
  const [selectedId, setSelectedId] = useState<string | null>(selectedNodeId ?? null);
  const [hoverId, setHoverId] = useState<string | null>(null);

  const initialFitDoneRef = useRef<boolean>(false);
  const hoverTimerRef = useRef<number | null>(null);

  // 同步外部受控 selectedNodeId
  React.useEffect(() => {
    if (selectedNodeId !== undefined) {
      setSelectedId(selectedNodeId);
    }
  }, [selectedNodeId]);

  // 组件卸载时清理悬停防抖计时器
  React.useEffect(() => {
    return () => {
      if (hoverTimerRef.current) {
        window.clearTimeout(hoverTimerRef.current);
        hoverTimerRef.current = null;
      }
    };
  }, []);

  // 响应重置布局触发
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

  // 响应外部「定位对端节点」飞跃请求
  React.useEffect(() => {
    if (!rfInstance || !panToNodeRequest?.nodeId) return;
    setSelectedId(panToNodeRequest.nodeId);
    void rfInstance.fitView({
      nodes: [{ id: panToNodeRequest.nodeId }],
      padding: 0.35,
      maxZoom: 1.15,
      duration: 300,
    });
  }, [rfInstance, panToNodeRequest]);

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

  const { flowNodes: layoutNodes, flowEdges: layoutEdges, isLayouting } = useElkLayout({
    nodes,
    edges,
    expandState,
    consistencyReport,
    view,
    draggedNodeIds,
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
    savedPositions,
    pinnedNodeIds,
    onFocusScope,
    onTogglePin,
    onExpandHub,
    onPositionsComputed,
    containerSearchMatchCounts,
    lodMode,
  });

  const [flowNodes, setFlowNodes, onNodesChange] = useNodesState<ArchCanvasNode>(layoutNodes);
  const [flowEdges, setFlowEdges, onEdgesChange] = useEdgesState<ArchCanvasEdge>(layoutEdges);

  React.useEffect(() => {
    setFlowNodes(layoutNodes);
    setFlowEdges(layoutEdges);
  }, [layoutNodes, layoutEdges, setFlowNodes, setFlowEdges]);

  // 仅在冷启动首次排版完成且无预设视口时执行一次 fitView，严禁在单容器展开/折叠时触发全局 fitView
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

  // 拖拽子节点时实时撑大展开态父容器包围盒，防止拖拽中子元素穿出父框
  const handleNodeDrag: OnNodeDrag = useCallback(
    (_, draggedNode) => {
      setDraggedNodeIds((prev) => {
        if (prev.has(draggedNode.id)) return prev;
        const next = new Set(prev);
        next.add(draggedNode.id);
        return next;
      });

      if (draggedNode.parentId) {
        setFlowNodes((prev) => {
          const parentIdx = prev.findIndex((n) => n.id === draggedNode.parentId);
          if (parentIdx < 0) return prev;
          const parent = prev[parentIdx];
          const childW = Number(draggedNode.style?.width) || 260;
          const childH = Number(draggedNode.style?.height) || 80;
          const reqW = Math.max(16, draggedNode.position.x) + childW + 16;
          const reqH = Math.max(52, draggedNode.position.y) + childH + 16;
          const curW = Number(parent.style?.width) || 260;
          const curH = Number(parent.style?.height) || 140;
          if (reqW <= curW && reqH <= curH) return prev;

          const next = [...prev];
          next[parentIdx] = {
            ...parent,
            style: {
              ...parent.style,
              width: Math.max(curW, reqW),
              height: Math.max(curH, reqH),
            },
          };
          return next;
        });
      }
    },
    [setFlowNodes]
  );

  const handleNodeDragStop: OnNodeDrag = useCallback(
    (_, node) => {
      if (onNodePositionChange) {
        const clampedPos = node.parentId
          ? { x: Math.max(16, node.position.x), y: Math.max(52, node.position.y) }
          : node.position;
        onNodePositionChange(node.id, clampedPos);
      }
    },
    [onNodePositionChange]
  );

  const handleClearSelection = useCallback(() => {
    setSelectedId(null);
    setHoverId(null);
    onSelectNode?.(null);
  }, [onSelectNode]);

  // 单击节点：立即响应选中锁定（无 220ms 防抖延迟，默认不强制弹抽屉）
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

  // 双击节点：单向渐进下钻（折叠容器 -> 展开；已展开容器 -> 进入隔离聚焦；叶子模块 -> 打开画像抽屉「上下游关系」页）
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
        const isCurrentlyExpanded = Boolean(expandState[node.id]);
        if (!isCurrentlyExpanded) {
          onToggleExpand(node.id);
        } else if (onFocusScope && focusScope !== node.id) {
          onFocusScope(node.id);
        } else if (onInspectNode) {
          onInspectNode(gn, 'RELATIONS');
        }
        return;
      }

      // 叶子模块或外部系统：双击直接打开画像抽屉「上下游关系」页
      setSelectedId(node.id);
      if (onInspectNode) {
        onInspectNode(gn, 'RELATIONS');
      } else if (onSelectNode) {
        onSelectNode(gn);
      }
    },
    [expandState, focusScope, onToggleExpand, onFocusScope, onInspectNode, onSelectNode]
  );

  const handlePaneClick = useCallback(() => {
    if (hoverTimerRef.current) {
      window.clearTimeout(hoverTimerRef.current);
      hoverTimerRef.current = null;
    }
    handleClearSelection();
  }, [handleClearSelection]);

  // 画布空白处双击：若处于隔离聚焦态，返回上一级聚焦（或退回全局全景）
  const handleCanvasDoubleClick = useCallback(
    (e: React.MouseEvent<HTMLDivElement>) => {
      const target = e.target as HTMLElement | null;
      if (!target || !target.classList.contains('react-flow__pane')) return;
      if (!focusScope || focusScope === 'GLOBAL' || !onFocusScope) return;

      const currentFocusNode = nodes.find((n) => n.id === focusScope);
      const parentScope = currentFocusNode?.parent_id || 'GLOBAL';
      onFocusScope(parentScope);
    },
    [focusScope, nodes, onFocusScope]
  );

  // 鼠标悬停：仅在无锁定选中或悬停目标为卡片/展开容器标题栏时触发防抖预览
  const handleNodeMouseEnter: NodeMouseHandler = useCallback(
    (event, node) => {
      if (hoverTimerRef.current) {
        window.clearTimeout(hoverTimerRef.current);
        hoverTimerRef.current = null;
      }

      const gn = (node.data as { graphNode?: GraphNode })?.graphNode;
      const isExpandedContainer = Boolean(gn && isContainerNodeType(gn.type) && expandState[node.id]);
      if (isExpandedContainer) {
        const targetEl = event.target as HTMLElement | null;
        if (targetEl && !targetEl.closest('.code-arch-container__header')) {
          return;
        }
      }

      hoverTimerRef.current = window.setTimeout(() => {
        hoverTimerRef.current = null;
        setHoverId(node.id);
      }, SPOTLIGHT_HOVER_DEBOUNCE_MS);
    },
    [expandState]
  );

  // 鼠标移出：仅清除临时 hoverId，绝不清除单击锁定的 selectedId
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

      if (!wheelAutoExpand || !onDepthLevelChange) return;
      if (viewport.zoom >= 1.25 && depthLevel < maxDepth) {
        onDepthLevelChange(depthLevel + 1);
      } else if (viewport.zoom <= 0.65 && depthLevel > 0) {
        onDepthLevelChange(depthLevel - 1);
      }
    },
    [wheelAutoExpand, onDepthLevelChange, depthLevel, maxDepth, onViewportChange]
  );

  // 可见图节点与汇总边集合
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

  // 键盘快捷键支持：Esc 清除选中/退出聚焦，Enter 渐进下钻，Backspace 返回上一层，I 打开画像详情
  React.useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      const activeTag = (document.activeElement as HTMLElement | null)?.tagName;
      if (activeTag === 'INPUT' || activeTag === 'TEXTAREA' || activeTag === 'SELECT') {
        return;
      }

      if (e.key === 'Escape') {
        if (selectedId) {
          handleClearSelection();
        } else if (focusScope && focusScope !== 'GLOBAL' && onFocusScope) {
          const currentFocusNode = nodes.find((n) => n.id === focusScope);
          onFocusScope(currentFocusNode?.parent_id || 'GLOBAL');
        }
        return;
      }

      if (e.key === 'Backspace' && focusScope && focusScope !== 'GLOBAL' && onFocusScope) {
        const currentFocusNode = nodes.find((n) => n.id === focusScope);
        onFocusScope(currentFocusNode?.parent_id || 'GLOBAL');
        return;
      }

      if (!selectedId) return;
      const gn = visibleGraphNodes.find((n) => n.id === selectedId);
      if (!gn) return;

      if (e.key === 'Enter') {
        e.preventDefault();
        if (isContainerNodeType(gn.type)) {
          if (!expandState[gn.id]) {
            onToggleExpand(gn.id);
          } else if (onFocusScope && focusScope !== gn.id) {
            onFocusScope(gn.id);
          }
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
    focusScope,
    nodes,
    visibleGraphNodes,
    expandState,
    handleClearSelection,
    onFocusScope,
    onToggleExpand,
    onInspectNode,
  ]);

  // 聚光灯主状态：存在 selectedId 时严格锁定在 selectedId，不随鼠标移出或悬停切换闪烁
  const effectiveSpotlightId = selectedId ?? hoverId;

  const spotlight = useMemo(
    () => computeSpotlightSet(effectiveSpotlightId, visibleGraphNodes, visibleRollupModels),
    [effectiveSpotlightId, visibleGraphNodes, visibleRollupModels]
  );

  // 当前选中的图节点与上下游关系计数（供浮动操作条展示）
  const selectedGraphNode = useMemo(
    () => (selectedId ? visibleGraphNodes.find((n) => n.id === selectedId) ?? null : null),
    [selectedId, visibleGraphNodes]
  );

  const selectedNodeRelations = useMemo(
    () => computeNodeRelations(selectedGraphNode?.id, nodes, visibleRollupModels),
    [selectedGraphNode?.id, nodes, visibleRollupModels]
  );

  // 关键词检索高亮 + 子树分层聚光灯 + LOD 缩放样式注入
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

      const classes = [
        n.className || '',
        match ? 'code-arch-node--search-match' : '',
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
  }, [flowNodes, searchKeyword, selectedId, hoverId, spotlight, lodMode]);

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
  const isSelectedExpanded = Boolean(
    selectedGraphNode && expandState[selectedGraphNode.id]
  );
  const isSelectedPinned = Boolean(
    selectedGraphNode &&
      (pinnedNodeIds?.has(selectedGraphNode.id) || savedPositions?.[selectedGraphNode.id]?.pinned)
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
            {isSelectedContainer && !isSelectedExpanded && (
              <button
                type="button"
                className="code-arch-action-bar__btn code-arch-action-bar__btn--primary"
                onClick={() => onToggleExpand(selectedGraphNode.id)}
                data-testid="action-bar-expand-btn"
                title="原地展开容器查看内部子元素 (Enter / 双击)"
              >
                <FolderOpen size={12} />
                <span>展开</span>
              </button>
            )}

            {isSelectedContainer && onFocusScope && focusScope !== selectedGraphNode.id && (
              <button
                type="button"
                className="code-arch-action-bar__btn"
                onClick={() => onFocusScope(selectedGraphNode.id)}
                data-testid="action-bar-focus-btn"
                title="进入该容器隔离聚焦子图 (已展开时双击)"
              >
                <Crosshair size={12} />
                <span>聚焦子图</span>
              </button>
            )}

            {isSelectedContainer && isSelectedExpanded && (
              <button
                type="button"
                className="code-arch-action-bar__btn"
                onClick={() => onToggleExpand(selectedGraphNode.id)}
                data-testid="action-bar-collapse-btn"
                title="收起容器内部节点"
              >
                <FolderMinus size={12} />
                <span>折叠</span>
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

            {onTogglePin && (
              <button
                type="button"
                className={`code-arch-action-bar__btn ${
                  isSelectedPinned ? 'code-arch-action-bar__btn--active' : ''
                }`}
                onClick={() => onTogglePin(selectedGraphNode.id)}
                data-testid="action-bar-pin-btn"
                title={isSelectedPinned ? '取消固定位置' : '固定该元素位置'}
              >
                <Pin size={12} />
                <span>{isSelectedPinned ? '已固定' : '固定'}</span>
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
              <span>外部依赖网络/系统</span>
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
  );
};
