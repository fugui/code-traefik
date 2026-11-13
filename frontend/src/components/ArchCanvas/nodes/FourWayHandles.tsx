import React from 'react';
import { Handle, Position } from '@xyflow/react';

/**
 * M21-P2: 四向双向锚点组 (top / right / bottom / left)
 * 每条边支持通过 selectOptimalHandles 动态绑定至最佳方位，彻底消除绕背连线与箭头穿卡。
 */
export const FourWayHandles: React.FC = () => (
  <>
    <Handle id="tgt-top" type="target" position={Position.Top} className="code-arch-handle" />
    <Handle id="src-top" type="source" position={Position.Top} className="code-arch-handle" />
    <Handle id="tgt-right" type="target" position={Position.Right} className="code-arch-handle" />
    <Handle id="src-right" type="source" position={Position.Right} className="code-arch-handle" />
    <Handle id="tgt-bottom" type="target" position={Position.Bottom} className="code-arch-handle" />
    <Handle id="src-bottom" type="source" position={Position.Bottom} className="code-arch-handle" />
    <Handle id="tgt-left" type="target" position={Position.Left} className="code-arch-handle" />
    <Handle id="src-left" type="source" position={Position.Left} className="code-arch-handle" />
  </>
);
