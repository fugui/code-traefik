import React from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import type { ModuleNodeType } from '../layout/layoutProcessor';
import { Cpu, AlertTriangle } from 'lucide-react';

export const ModuleNode: React.FC<NodeProps<ModuleNodeType>> = ({ data }) => {
  const { graphNode } = data;
  const isDeployed = graphNode.meta?.is_deployed !== false;
  const provideCount = Number(graphNode.meta?.provide_count || 0);

  return (
    <div
      className={`code-arch-module ${!isDeployed ? 'code-arch-module--undeployed' : ''}`}
      title={graphNode.name}
    >
      <Handle type="target" position={Position.Left} className="code-arch-handle" />

      <div className="code-arch-module__header">
        <Cpu size={13} className="code-arch-module__icon" />
        <span className="code-arch-module__code">{graphNode.code || graphNode.id}</span>
        {!isDeployed && (
          <span className="code-arch-module__warn-badge" title="未在公共环境绑定物理进程">
            <AlertTriangle size={11} /> 未部署
          </span>
        )}
      </div>

      <div className="code-arch-module__body">
        <span className="code-arch-module__name">{graphNode.name}</span>
      </div>

      {provideCount > 0 && (
        <div className="code-arch-module__footer">
          <span className="code-arch-badge code-arch-badge--provide" title={`对外提供 ${provideCount} 个服务契约`}>
            供给: {provideCount}
          </span>
        </div>
      )}

      <Handle type="source" position={Position.Right} className="code-arch-handle" />
    </div>
  );
};
