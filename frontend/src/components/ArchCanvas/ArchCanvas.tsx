import React, { useState, useCallback, useMemo } from 'react';
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

import type { GraphNode, GraphEdge, ExpandStateMap, RollupEdgeModel } from '../../types/topology_graph';
import type { ConsistencyReportDTO } from '../../types/traffic';
import { useElkLayout } from './layout/useElkLayout';
import type { ArchCanvasNode, ArchCanvasEdge } from './layout/layoutProcessor';
import { ContainerNode } from './nodes/ContainerNode';
import { ModuleNode } from './nodes/ModuleNode';
import { ExternalNode } from './nodes/ExternalNode';
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
}) => {
  const [draggedNodeIds, setDraggedNodeIds] = useState<Set<string>>(new Set());
  const [rfInstance, setRfInstance] = useState<ReactFlowInstance<ArchCanvasNode, ArchCanvasEdge> | null>(null);
  const [isLegendExpanded, setIsLegendExpanded] = useState<boolean>(true);

  // 响应重置布局触发
  React.useEffect(() => {
    setDraggedNodeIds(new Set());
    if (rfInstance && resetKey != null && resetKey > 0) {
      void rfInstance.fitView({ duration: 300 });
    }
  }, [resetKey, rfInstance]);

  const nodeTypes = useMemo(
    () => ({
      containerNode: ContainerNode,
      moduleCard: ModuleNode,
      externalNode: ExternalNode,
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
  });

  const [flowNodes, setFlowNodes, onNodesChange] = useNodesState<ArchCanvasNode>(layoutNodes);
  const [flowEdges, setFlowEdges, onEdgesChange] = useEdgesState<ArchCanvasEdge>(layoutEdges);

  // 当排版引擎输出变动时更新 React Flow 状态
  React.useEffect(() => {
    setFlowNodes(layoutNodes);
    setFlowEdges(layoutEdges);
  }, [layoutNodes, layoutEdges, setFlowNodes, setFlowEdges]);

  // 当排版就绪且用户未拖拽时，自动平滑执行自适应视口居中 (Fit View)，充分利用画布可用视野
  React.useEffect(() => {
    if (!rfInstance || layoutNodes.length === 0 || isLayouting) return;
    if (draggedNodeIds.size > 0) return; // 尊重用户的单点拖拽位移

    const timer = setTimeout(() => {
      void rfInstance.fitView({ padding: 0.18, duration: 240 });
    }, 60);

    return () => clearTimeout(timer);
  }, [rfInstance, layoutNodes.length, isLayouting, draggedNodeIds.size]);

  // 手动触发自适应居中铺满
  const handleFitView = useCallback(() => {
    if (rfInstance) {
      void rfInstance.fitView({ padding: 0.18, duration: 250 });
    }
  }, [rfInstance]);

  // 记录用户手动拖拽的节点 ID，使关联边自动退化为动态 SmoothStep
  const handleNodeDrag: OnNodeDrag = useCallback((_, node) => {
    setDraggedNodeIds((prev) => {
      if (prev.has(node.id)) return prev;
      const next = new Set(prev);
      next.add(node.id);
      return next;
    });
  }, []);

  const handleNodeClick: NodeMouseHandler = useCallback(
    (_, node) => {
      const gn = (node.data as { graphNode?: GraphNode })?.graphNode;
      if (gn && onSelectNode) {
        onSelectNode(gn);
      }
    },
    [onSelectNode]
  );

  const handleEdgeClick: EdgeMouseHandler = useCallback(
    (_, edge) => {
      const edgeModel = edge.data as RollupEdgeModel | undefined;
      if (edgeModel && onSelectEdge) {
        onSelectEdge(edgeModel);
      }
    },
    [onSelectEdge]
  );

  // 滚轮缩放联动层级自动展开/收起
  const handleMoveEnd = useCallback(
    (_: unknown, viewport: { zoom: number }) => {
      if (!wheelAutoExpand || !onDepthLevelChange) return;
      if (viewport.zoom >= 1.25 && depthLevel < maxDepth) {
        onDepthLevelChange(depthLevel + 1);
      } else if (viewport.zoom <= 0.65 && depthLevel > 0) {
        onDepthLevelChange(depthLevel - 1);
      }
    },
    [wheelAutoExpand, onDepthLevelChange, depthLevel, maxDepth]
  );

  // 关键词检索高亮增强
  const highlightedFlowNodes = useMemo(() => {
    const kw = searchKeyword?.trim().toLowerCase();
    if (!kw) return flowNodes;
    return flowNodes.map((n) => {
      const gn = (n.data as { graphNode?: GraphNode })?.graphNode;
      const match =
        gn &&
        (gn.name.toLowerCase().includes(kw) ||
          (gn.code && gn.code.toLowerCase().includes(kw)) ||
          gn.id.toLowerCase().includes(kw));
      if (match) {
        return {
          ...n,
          className: `${n.className || ''} code-arch-node--search-match`.trim(),
        };
      }
      return n;
    });
  }, [flowNodes, searchKeyword]);

  return (
    <div className={`code-arch-canvas ${className}`} data-testid="arch-canvas">
      {isLayouting && (
        <div
          style={{
            position: 'absolute',
            top: 14,
            left: 14,
            zIndex: 10,
            padding: '4px 10px',
            backgroundColor: 'var(--color-bg-surface)',
            border: '1px solid var(--color-border-primary)',
            borderRadius: 4,
            fontSize: 11,
            color: 'var(--color-text-secondary)',
            boxShadow: 'var(--shadow-sm)',
          }}
        >
          正交排版计算中...
        </div>
      )}

      {/* 悬浮全景图例与视口辅助 HUD (利用画布空白，丰富信息维度) */}
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
          <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
            <button
              type="button"
              className="code-arch-toolbar__quick-depth-btn"
              onClick={(e) => {
                e.stopPropagation();
                handleFitView();
              }}
              title="一键自适应居中铺满画布"
            >
              <Maximize2 size={10} style={{ marginRight: 2 }} />
              居中
            </button>
            {isLegendExpanded ? <ChevronUp size={12} /> : <ChevronDown size={12} />}
          </div>
        </div>

        {isLegendExpanded && (
          <div className="code-arch-legend-hud__items">
            <div className="code-arch-legend-item">
              <span className="code-arch-legend-badge" style={{ backgroundColor: '#2563eb' }} />
              <span>{view === 'PHYSICAL' ? '进程组 (L0)' : '子系统 (L0)'}</span>
            </div>
            <div className="code-arch-legend-item">
              <span className="code-arch-legend-badge" style={{ backgroundColor: '#6366f1' }} />
              <span>{view === 'PHYSICAL' ? '物理进程 (L1)' : '功能组 (L1)'}</span>
            </div>
            <div className="code-arch-legend-item">
              <span className="code-arch-legend-badge" style={{ backgroundColor: '#0284c7' }} />
              <span>微服务模块 (L2)</span>
            </div>
            <div className="code-arch-legend-item">
              <span className="code-arch-legend-badge" style={{ backgroundColor: '#475569' }} />
              <span>外部依赖网络/系统</span>
            </div>
            <div className="code-arch-legend-item">
              <span className="code-arch-legend-badge" style={{ backgroundColor: '#059669' }} />
              <span>跨进程调用 / 契约链路</span>
            </div>
          </div>
        )}
      </div>

      <ReactFlow<ArchCanvasNode, ArchCanvasEdge>
        style={{ width: '100%', height: '100%', flex: 1 }}
        onInit={setRfInstance}
        nodes={highlightedFlowNodes}
        edges={flowEdges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onNodeDrag={handleNodeDrag}
        onNodeClick={handleNodeClick}
        onEdgeClick={handleEdgeClick}
        onMoveEnd={handleMoveEnd}
        fitView
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
