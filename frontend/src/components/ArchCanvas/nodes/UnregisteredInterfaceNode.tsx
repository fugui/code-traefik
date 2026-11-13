import React from 'react';
import { type NodeProps } from '@xyflow/react';
import type { UnregisteredInterfaceNodeType } from '../layout/layoutProcessor';
import { FourWayHandles } from './FourWayHandles';
import { ShieldAlert } from 'lucide-react';

export const UnregisteredInterfaceNode: React.FC<NodeProps<UnregisteredInterfaceNodeType>> = ({ data }) => {
  const { graphNode, isSearchHighlighted = false } = data;
  const isImplUnresolved = graphNode.id.startsWith('VIRTUAL_IMPL_UNRESOLVED');

  return (
    <div
      className={`code-arch-unregistered ${
        isSearchHighlighted ? 'code-arch-node--pulse-highlight' : ''
      }`}
      title={graphNode.meta?.description ? String(graphNode.meta.description) : graphNode.name}
    >
      <FourWayHandles />

      <div className="code-arch-unregistered__header">
        <ShieldAlert size={14} className="code-arch-unregistered__icon" />
        <span className="code-arch-unregistered__title" title={graphNode.name}>
          {graphNode.name}
        </span>
      </div>
      <div className="code-arch-unregistered__sub">
        {isImplUnresolved ? '多实现待定 (IMPL_UNRESOLVED)' : '动态 VIOLATION 告警'}
      </div>
    </div>
  );
};
