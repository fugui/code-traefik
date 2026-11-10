import React, { useState, useCallback, useMemo, useRef } from 'react';
import { Info, ChevronUp, ChevronDown, Maximize2 } from 'lucide-react';
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
  LOD_COMPACT_ZOOM,
  LOD_DETAIL_ZOOM,
} from './model/noiseReduction';
import { ContainerNode } from './nodes/ContainerNode';
import { ModuleNode } from './nodes/ModuleNode';
import { ExternalNode } from './nodes/ExternalNode';
import { ExternalContextNode } from './nodes/ExternalContextNode';
import { UnregisteredInterfaceNode } from './nodes/UnregisteredInterfaceNode';
import { RollupEdge } from './edges/RollupEdge';
import './ArchCanvas.css';

export interface ArchCanvasProps {
  nodes: GraphNode[];
  edges: GraphEdge[];
  expandState: ExpandStateMap;
  onToggleExpand: (nodeId: string) => void;
  onSelectNode?: (node: GraphNode) => void;
  onSelectEdge?: (edge: RollupEdgeModel) => void;
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
}

export const ArchCanvas: React.FC<ArchCanvasProps> = ({
  nodes,
  edges,
  expandState,
  onToggleExpand,
  onSelectNode,
  onSelectEdge,
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
}) => {
  const [draggedNodeIds, setDraggedNodeIds] = useState<Set<string>>(new Set());
  const [rfInstance, setRfInstance] = useState<ReactFlowInstance<ArchCanvasNode, ArchCanvasEdge> | null>(null);
  const [isLegendExpanded, setIsLegendExpanded] = useState<boolean>(true);
  const [lodMode, setLodMode] = useState<LodMode>('STANDARD');
  const [spotlightId, setSpotlightId] = useState<string | null>(null);
  const initialFitDoneRef = useRef<boolean>(false);
  const clickTimerRef = useRef<number | null>(null);

  // 组件卸载时安全清理点击消抖计时器
  React.useEffect(() => {
    return () => {
      if (clickTimerRef.current) {
        window.clearTimeout(clickTimerRef.current);
        clickTimerRef.current = null;
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

  const handleNodeDrag: OnNodeDrag = useCallback((_, node) => {
    setDraggedNodeIds((prev) => {
      if (prev.has(node.id)) return prev;
      const next = new Set(prev);
      next.add(node.id);
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

  // 节点单击/双击防抖调度：
  // 延时 220ms 区分单击与双击。若在时限内捕获到双击，则立刻取消单击打开画像抽屉，优先满足展开/折叠容器诉求
  const handleNodeClick: NodeMouseHandler = useCallback(
    (_, node) => {
      if (clickTimerRef.current) {
        window.clearTimeout(clickTimerRef.current);
        clickTimerRef.current = null;
      }

      clickTimerRef.current = window.setTimeout(() => {
        clickTimerRef.current = null;
        setSpotlightId((prev) => (prev === node.id ? null : node.id));
        const gn = (node.data as { graphNode?: GraphNode })?.graphNode;
        if (gn && onSelectNode) {
          onSelectNode(gn);
        }
      }, 220);
    },
    [onSelectNode]
  );

  const handleNodeDoubleClick: NodeMouseHandler = useCallback(
    (_, node) => {
      // 1. 立即清除单击延时计时器，避免触发单击打开画像抽屉拦截双击
      if (clickTimerRef.current) {
        window.clearTimeout(clickTimerRef.current);
        clickTimerRef.current = null;
      }

      // 2. 检查节点类型，容器节点双击触发展开/折叠下钻
      const gn = (node.data as { graphNode?: GraphNode })?.graphNode;
      const isContainer =
        gn &&
        (gn.type === 'SUBSYSTEM' ||
          gn.type === 'FUNCTIONAL_GROUP' ||
          gn.type === 'PROCESS_GROUP' ||
          gn.type === 'PROCESS' ||
          gn.type === 'UNDEPLOYED_CONTAINER' ||
          gn.type === 'EXTERNAL_CONTAINER');

      if (isContainer && onToggleExpand) {
        onToggleExpand(node.id);
      } else if (gn && onSelectNode) {
        onSelectNode(gn);
      }
    },
    [onToggleExpand, onSelectNode]
  );

  const handlePaneClick = useCallback(() => {
    if (clickTimerRef.current) {
      window.clearTimeout(clickTimerRef.current);
      clickTimerRef.current = null;
    }
    setSpotlightId(null);
  }, []);

  const handleNodeMouseEnter: NodeMouseHandler = useCallback((_, node) => {
    setSpotlightId(node.id);
  }, []);

  const handleNodeMouseLeave: NodeMouseHandler = useCallback(() => {
    setSpotlightId(null);
  }, []);

  const handleEdgeClick: EdgeMouseHandler = useCallback(
    (_, edge) => {
      setSpotlightId((prev) => (prev === edge.id ? null : edge.id));
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

  // 聚光灯高亮计算 (Hover / 选中时压暗无关节点与边)
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

  const spotlight = useMemo(
    () => computeSpotlightSet(spotlightId, visibleGraphNodes, visibleRollupModels),
    [spotlightId, visibleGraphNodes, visibleRollupModels]
  );

  // 关键词检索高亮 + 聚光灯压暗 + LOD 缩放样式注入
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
      const dimmed = spotlight.active && !spotlight.highlightedNodeIds.has(n.id);
      const classes = [
        n.className || '',
        match ? 'code-arch-node--search-match' : '',
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
  }, [flowNodes, searchKeyword, spotlight, lodMode]);

  const highlightedFlowEdges = useMemo(() => {
    if (!spotlight.active) return flowEdges;
    return flowEdges.map((e) => {
      const dimmed = !spotlight.highlightedEdgeIds.has(e.id);
      return {
        ...e,
        data: e.data ? { ...e.data, isSpotlightDimmed: dimmed } : e.data,
      };
    });
  }, [flowEdges, spotlight]);

  return (
    <div className={`code-arch-canvas ${className}`} data-testid="arch-canvas">
      {isLayouting && (
        <div className="code-arch-layouting-indicator" data-testid="arch-layouting-indicator">
          正交排版计算中...
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
