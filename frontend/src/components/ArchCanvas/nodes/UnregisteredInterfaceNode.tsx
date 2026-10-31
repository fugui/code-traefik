import React from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import type { UnregisteredInterfaceNodeType } from '../layout/layoutProcessor';
import { ShieldAlert } from 'lucide-react';

export const UnregisteredInterfaceNode: React.FC<NodeProps<UnregisteredInterfaceNodeType>> = ({ data }) => {
  const { graphNode } = data;

  return (
    <div className="code-arch-unregistered" title="动态检出的未登记外部接口告警节点">
      <Handle type="target" position={Position.Left} className="code-arch-handle" />

      <div className="code-arch-unregistered__header">
        <ShieldAlert size={14} className="code-arch-unregistered__icon" />
        <span className="code-arch-unregistered__title">{graphNode.name}</span>
      </div>
      <div className="code-arch-unregistered__sub">
        动态 VIOLATION 告警
      </div>

      <Handle type="source" position={Position.Right} className="code-arch-handle" />
    </div>
  );
};
