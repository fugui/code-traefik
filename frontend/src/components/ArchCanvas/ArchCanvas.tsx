import React, { useState, useCallback, useMemo } from 'react';
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
            top: 12,
            right: 12,
            zIndex: 10,
            padding: '4px 8px',
            backgroundColor: 'var(--color-bg-surface)',
            border: '1px solid var(--color-border-primary)',
            borderRadius: 4,
            fontSize: 11,
            color: 'var(--color-text-secondary)',
          }}
        >
          正交排版计算中...
        </div>
      )}

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
