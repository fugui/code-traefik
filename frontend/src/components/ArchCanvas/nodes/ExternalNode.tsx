import React from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import type { ExternalNodeType } from '../layout/layoutProcessor';
import { Globe, Radio } from 'lucide-react';

export const ExternalNode: React.FC<NodeProps<ExternalNodeType>> = ({ data }) => {
  const { graphNode } = data;
  const isEndpoint = graphNode.type === 'EXTERNAL_ENDPOINT';

  return (
    <div
      className={`code-arch-external ${isEndpoint ? 'code-arch-external--endpoint' : 'code-arch-external--system'}`}
      title={graphNode.name}
    >
      <Handle type="target" position={Position.Left} className="code-arch-handle" />

      <div className="code-arch-external__header">
        {isEndpoint ? <Radio size={13} className="code-arch-external__icon" /> : <Globe size={13} className="code-arch-external__icon" />}
        <span className="code-arch-external__type-label">
          {isEndpoint ? '外部端点' : '外部系统'}
        </span>
      </div>

      <div className="code-arch-external__name">
        {graphNode.name}
      </div>

      <Handle type="source" position={Position.Right} className="code-arch-handle" />
    </div>
  );
};
