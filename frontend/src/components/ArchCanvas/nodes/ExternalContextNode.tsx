import React from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import { ArrowUpRight, Compass, AlertTriangle } from 'lucide-react';
import type { ExternalContextNodeType } from '../layout/layoutProcessor';

export const ExternalContextNode: React.FC<NodeProps<ExternalContextNodeType>> = ({ data }) => {
  const { graphNode, onFocusScope } = data;
  const direction = graphNode.meta?.direction === 'UPSTREAM' ? 'UPSTREAM' : 'DOWNSTREAM';
  const peerRootId = graphNode.meta?.peer_root_id ? String(graphNode.meta.peer_root_id) : graphNode.id;
  const peerName = graphNode.meta?.peer_root_name ? String(graphNode.meta.peer_root_name) : graphNode.name;
  const hiddenModules = Number(graphNode.meta?.hidden_module_count || 0);
  const crossEdges = Number(graphNode.meta?.cross_edge_count || 0);
  const violations = Number(graphNode.meta?.violation_count || 0);

  const handleFocusClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (onFocusScope && peerRootId) {
      onFocusScope(peerRootId);
    }
  };

  return (
    <div
      className={`code-arch-ext-ctx code-arch-ext-ctx--${direction.toLowerCase()}`}
      data-testid={`ext-ctx-card-${graphNode.id}`}
      title={`外部关联容器: ${peerName}`}
    >
      <Handle type="target" position={Position.Left} className="code-arch-handle" />

      <div className="code-arch-ext-ctx__header">
        <Compass size={13} className="code-arch-ext-ctx__icon" />
        <span className="code-arch-ext-ctx__dir-badge">
          {direction === 'UPSTREAM' ? '上游外部域' : '下游外部域'}
        </span>
        {violations > 0 && (
          <span className="code-arch-badge code-arch-badge--danger" title={`检出 ${violations} 条越权违规调用`}>
            <AlertTriangle size={10} /> 异常: {violations}
          </span>
        )}
      </div>

      <div className="code-arch-ext-ctx__title" title={peerName}>
        {peerName}
      </div>

      <div className="code-arch-ext-ctx__metrics">
        <span className="code-arch-badge code-arch-badge--counter">
          模块: <strong>{hiddenModules}</strong>
        </span>
        <span className="code-arch-badge code-arch-badge--internal">
          跨边界边: <strong>{crossEdges}</strong>
        </span>
      </div>

      <div className="code-arch-ext-ctx__actions">
        <button
          type="button"
          className="code-arch-ext-ctx__focus-btn"
          onClick={handleFocusClick}
          data-testid={`ext-ctx-focus-btn-${peerRootId}`}
        >
          <span>切换聚焦至此</span>
          <ArrowUpRight size={11} />
        </button>
      </div>

      <Handle type="source" position={Position.Right} className="code-arch-handle" />
    </div>
  );
};
