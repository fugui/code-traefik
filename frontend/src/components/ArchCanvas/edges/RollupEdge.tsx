import React from 'react';
import {
  type EdgeProps,
  getSmoothStepPath,
  EdgeLabelRenderer,
  BaseEdge,
} from '@xyflow/react';
import type { ArchCanvasEdge } from '../layout/layoutProcessor';

export const RollupEdge: React.FC<EdgeProps<ArchCanvasEdge>> = ({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  data,
  markerEnd,
}) => {
  const isDegraded = Boolean(data?.isDraggedDegraded);
  const bendPoints = data?.bendPoints;

  let edgePath = '';
  let labelX = (sourceX + targetX) / 2;
  let labelY = (sourceY + targetY) / 2;

  if (!isDegraded && bendPoints && bendPoints.length > 0) {
    const pts = [{ x: sourceX, y: sourceY }, ...bendPoints, { x: targetX, y: targetY }];
    edgePath = pts.reduce((acc, pt, i) => `${acc} ${i === 0 ? 'M' : 'L'} ${pt.x} ${pt.y}`, '');

    const midIdx = Math.floor(pts.length / 2);
    labelX = pts[midIdx].x;
    labelY = pts[midIdx].y;
  } else {
    const [smoothPath, sLabelX, sLabelY] = getSmoothStepPath({
      sourceX,
      sourceY,
      targetX,
      targetY,
      sourcePosition,
      targetPosition,
      borderRadius: 8,
    });
    edgePath = smoothPath;
    labelX = sLabelX;
    labelY = sLabelY;
  }

  const contractCount = data?.services?.length || 1;
  const strokeWidth = Math.max(1.5, Math.min(5, 1.5 + Math.log2(contractCount + 1)));

  // 依据状态收敛与六分类决定连线颜色与样式类
  let statusClass = 'code-arch-edge--default';
  let strokeColor = 'var(--color-primary)';
  let strokeDasharray: string | undefined;

  if (data?.status) {
    statusClass = `code-arch-edge--${data.status.toLowerCase()}`;
    switch (data.status) {
      case 'VIOLATION':
        strokeColor = 'var(--color-danger)';
        break;
      case 'UNATTRIBUTED':
        strokeColor = 'var(--color-warning)';
        break;
      case 'VERIFIED':
        strokeColor = 'var(--color-success)';
        break;
      case 'UNCOVERED':
        strokeColor = 'var(--color-text-muted)';
        strokeDasharray = '5 5';
        break;
      case 'NOT_MONITORED':
        strokeColor = 'var(--color-text-muted)';
        strokeDasharray = '2 4';
        break;
      case 'NOT_OBSERVABLE':
        strokeColor = 'var(--color-info)';
        break;
    }
  } else if (data?.isCycle) {
    statusClass = 'code-arch-edge--cycle';
    strokeColor = 'var(--color-danger)';
  } else if (data?.kind === 'INTRA_PROCESS') {
    statusClass = 'code-arch-edge--intra';
    strokeColor = 'var(--color-border-hover)';
    strokeDasharray = '4 4';
  } else if (data?.kind === 'EXTERNAL_UNREGISTERED') {
    statusClass = 'code-arch-edge--external';
    strokeColor = 'var(--color-warning)';
    strokeDasharray = '6 3';
  }

  const labelText =
    data?.totalCalls && data.totalCalls > 0
      ? `∑ ${data.totalCalls.toLocaleString()}`
      : `${contractCount} 契约`;

  return (
    <>
      <BaseEdge
        id={id}
        path={edgePath}
        markerEnd={markerEnd}
        className={`code-arch-edge ${statusClass}`}
        style={{
          stroke: strokeColor,
          strokeWidth,
          strokeDasharray,
        }}
      />
      <EdgeLabelRenderer>
        <div
          style={{
            position: 'absolute',
            transform: `translate(-50%, -50%) translate(${labelX}px,${labelY}px)`,
            pointerEvents: 'all',
          }}
          className="nodrag nopan"
        >
          <span
            className={`code-arch-edge-badge ${statusClass}`}
            title={`调用清单: ${contractCount} 个契约`}
          >
            {labelText}
          </span>
        </div>
      </EdgeLabelRenderer>
    </>
  );
};
