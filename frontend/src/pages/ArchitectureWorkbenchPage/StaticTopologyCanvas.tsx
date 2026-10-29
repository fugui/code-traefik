import React, { useState } from 'react';
import type { StaticTopologyDTO, StaticTopologyNode, StaticTopologyEdge } from '../../types';

export interface StaticTopologyCanvasProps {
  topology: StaticTopologyDTO | null;
  loading?: boolean;
  view: 'LOGICAL' | 'PHYSICAL';
  onViewChange: (v: 'LOGICAL' | 'PHYSICAL') => void;
}

export const StaticTopologyCanvas: React.FC<StaticTopologyCanvasProps> = ({
  topology,
  loading = false,
  view,
  onViewChange,
}) => {
  const [selectedNode, setSelectedNode] = useState<StaticTopologyNode | null>(null);

  const nodes = topology?.nodes ?? [];
  const edges = topology?.edges ?? [];

  const topoNodes = topology?.nodes;
  // 简易环形/网格节点坐标布局计算
  const nodePositions = React.useMemo(() => {
    const list = topoNodes ?? [];
    const map = new Map<string, { x: number; y: number }>();
    const count = list.length;
    if (count === 0) return map;

    const centerX = 360;
    const centerY = 240;
    const radius = Math.min(centerX, centerY) - 80;

    list.forEach((node, idx) => {
      const angle = (2 * Math.PI * idx) / count;
      const x = centerX + radius * Math.cos(angle);
      const y = centerY + radius * Math.sin(angle);
      map.set(node.id, { x, y });
    });

    return map;
  }, [topoNodes]);

  const getEdgeStroke = (kind: StaticTopologyEdge['edge_kind']) => {
    switch (kind) {
      case 'CROSS_PROCESS':
        return 'var(--color-primary)';
      case 'INTRA_PROCESS':
        return 'var(--color-success)';
      case 'EXTERNAL_UNREGISTERED':
      default:
        return 'var(--color-warning)';
    }
  };

  return (
    <div className="code-topology-view">
      <div className="code-topology-toolbar">
        <div className="code-tab-pills">
          <button
            type="button"
            className={`code-pill-btn ${view === 'LOGICAL' ? 'code-pill-btn--active' : ''}`}
            onClick={() => onViewChange('LOGICAL')}
          >
            逻辑架构视图 (模块间依赖)
          </button>
          <button
            type="button"
            className={`code-pill-btn ${view === 'PHYSICAL' ? 'code-pill-btn--active' : ''}`}
            onClick={() => onViewChange('PHYSICAL')}
          >
            物理部署视图 (进程间调用)
          </button>
        </div>

        <div className="code-topology-legend">
          <span className="code-legend-dot code-legend-dot--cross">跨进程调用 (CROSS_PROCESS)</span>
          <span className="code-legend-dot code-legend-dot--intra">同进程内调用 (INTRA_PROCESS)</span>
          <span className="code-legend-dot code-legend-dot--external">外部未注册 (EXTERNAL)</span>
        </div>
      </div>

      <div className="code-topology-canvas-wrap">
        {loading ? (
          <div className="code-tpl-loading-screen">正在推导并渲染架构静态拓扑图...</div>
        ) : nodes.length === 0 ? (
          <div className="code-empty-pane code-p-xl">当前子系统下未声明足够的接口契约或尚未建立依赖拓扑</div>
        ) : (
          <div className="code-svg-container">
            <svg className="code-topology-svg" viewBox="0 0 720 480">
              <defs>
                <marker
                  id="topo-arrow"
                  viewBox="0 0 10 10"
                  refX="18"
                  refY="5"
                  markerWidth="6"
                  markerHeight="6"
                  orient="auto-start-reverse"
                >
                  <path d="M 0 0 L 10 5 L 0 10 z" fill="var(--color-border-hover)" />
                </marker>
              </defs>

              {/* 边连线 */}
              {edges.map((edge) => {
                const srcPos = nodePositions.get(edge.source);
                const dstPos = nodePositions.get(edge.target);
                if (!srcPos || !dstPos) return null;
                return (
                  <g key={edge.id} className="code-topology-edge-group">
                    <line
                      x1={srcPos.x}
                      y1={srcPos.y}
                      x2={dstPos.x}
                      y2={dstPos.y}
                      stroke={getEdgeStroke(edge.edge_kind)}
                      strokeWidth="2"
                      strokeDasharray={edge.edge_kind === 'EXTERNAL_UNREGISTERED' ? '4 3' : undefined}
                      markerEnd="url(#topo-arrow)"
                    />
                    <text
                      x={(srcPos.x + dstPos.x) / 2}
                      y={(srcPos.y + dstPos.y) / 2 - 6}
                      className="code-edge-label"
                      fill="var(--color-text-muted)"
                      textAnchor="middle"
                    >
                      {edge.operation}
                    </text>
                  </g>
                );
              })}

              {/* 节点 */}
              {nodes.map((node) => {
                const pos = nodePositions.get(node.id);
                if (!pos) return null;
                const isSelected = selectedNode?.id === node.id;
                return (
                  <g
                    key={node.id}
                    className={`code-topology-node-group ${isSelected ? 'code-node-selected' : ''}`}
                    transform={`translate(${pos.x}, ${pos.y})`}
                    onClick={() => setSelectedNode(node)}
                  >
                    <circle
                      r="24"
                      fill="var(--color-bg-surface)"
                      stroke={isSelected ? 'var(--color-primary)' : 'var(--color-border-primary)'}
                      strokeWidth={isSelected ? '3' : '1.5'}
                    />
                    <text
                      y="4"
                      textAnchor="middle"
                      fill="var(--color-text-primary)"
                      className="code-node-text"
                    >
                      {node.code.slice(0, 5)}
                    </text>
                    <text
                      y="38"
                      textAnchor="middle"
                      fill="var(--color-text-secondary)"
                      className="code-node-subtext"
                    >
                      {node.name}
                    </text>
                  </g>
                );
              })}
            </svg>
          </div>
        )}

        {/* 选定节点详情浮层 */}
        {selectedNode && (
          <div className="code-node-popover">
            <div className="code-node-popover__header">
              <span className="code-node-popover__title">{selectedNode.name}</span>
              <button
                type="button"
                className="code-btn-text"
                onClick={() => setSelectedNode(null)}
              >
                ✕
              </button>
            </div>
            <div className="code-meta-grid">
              <div className="code-meta-item">
                <span className="code-meta-label">编码:</span>
                <span className="code-meta-val code-font-mono">{selectedNode.code}</span>
              </div>
              <div className="code-meta-item">
                <span className="code-meta-label">类型:</span>
                <span className="code-badge code-badge--info">{selectedNode.kind}</span>
              </div>
              {selectedNode.process_name && (
                <div className="code-meta-item">
                  <span className="code-meta-label">承载进程:</span>
                  <span className="code-meta-val code-font-mono">
                    {selectedNode.process_name} (Port: {selectedNode.listen_port})
                  </span>
                </div>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  );
};
