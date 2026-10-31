import React from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import type { ContainerNodeType } from '../layout/layoutProcessor';
import { ChevronRight, ChevronDown, Layers, Server, Box } from 'lucide-react';

export const ContainerNode: React.FC<NodeProps<ContainerNodeType>> = ({ id, data }) => {
  const { graphNode, isExpanded, onToggleExpand, badgeCounts } = data;

  const handleDoubleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (onToggleExpand) {
      onToggleExpand(id);
    }
  };

  const handleToggleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (onToggleExpand) {
      onToggleExpand(id);
    }
  };

  let icon = <Layers size={14} className="code-arch-container__icon" />;
  if (graphNode.type === 'PROCESS_GROUP' || graphNode.type === 'PROCESS') {
    icon = <Server size={14} className="code-arch-container__icon" />;
  } else if (graphNode.type === 'UNDEPLOYED_CONTAINER' || graphNode.type === 'EXTERNAL_CONTAINER') {
    icon = <Box size={14} className="code-arch-container__icon" />;
  }

  const endpoint = graphNode.meta?.endpoint ? String(graphNode.meta.endpoint) : undefined;
  const internalEdges = badgeCounts?.internalEdgeCount ?? 0;
  const intraCalls = badgeCounts?.intraProcessCallCount ?? 0;

  return (
    <div
      className={`code-arch-container code-arch-container--${graphNode.type.toLowerCase()} ${
        isExpanded ? 'code-arch-container--expanded' : 'code-arch-container--collapsed'
      }`}
      onDoubleClick={handleDoubleClick}
    >
      <Handle type="target" position={Position.Left} className="code-arch-handle" />

      <div className="code-arch-container__header">
        <button
          type="button"
          className="code-arch-container__toggle"
          onClick={handleToggleClick}
          title={isExpanded ? '折叠容器' : '展开容器'}
        >
          {isExpanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        </button>

        {icon}

        <div className="code-arch-container__title-wrap">
          {graphNode.code && (
            <span className="code-arch-container__code">{graphNode.code}</span>
          )}
          <span className="code-arch-container__name" title={graphNode.name}>
            {graphNode.name}
          </span>
          {endpoint && (
            <span className="code-arch-container__endpoint" title={`监听端点: ${endpoint}`}>
              {endpoint}
            </span>
          )}
        </div>

        {/* 折叠态徽章提示 */}
        {!isExpanded && (
          <div className="code-arch-container__badges">
            {graphNode.type === 'UNDEPLOYED_CONTAINER' && graphNode.meta?.module_count != null && (
              <span className="code-arch-badge code-arch-badge--warning">
                孤立模块: {String(graphNode.meta.module_count)}
              </span>
            )}
            {internalEdges > 0 && (
              <span className="code-arch-badge code-arch-badge--internal" title="同容器内部依赖契约">
                内部依赖: {internalEdges}
              </span>
            )}
            {intraCalls > 0 && (
              <span className="code-arch-badge code-arch-badge--intra" title="进程内函数调用（不经网络栈）">
                进程内: {intraCalls}
              </span>
            )}
          </div>
        )}
      </div>

      <Handle type="source" position={Position.Right} className="code-arch-handle" />
    </div>
  );
};
