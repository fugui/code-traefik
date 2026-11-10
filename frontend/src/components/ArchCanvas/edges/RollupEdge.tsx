import React, { useState, useCallback } from 'react';
import {
  type EdgeProps,
  getSmoothStepPath,
  EdgeLabelRenderer,
  BaseEdge,
} from '@xyflow/react';
import { resolveEdgeVisualStyle, type ArchCanvasEdge } from '../layout/layoutProcessor';

export const RollupEdge: React.FC<EdgeProps<ArchCanvasEdge>> = ({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  data,
}) => {
  const [isHovered, setIsHovered] = useState(false);

  const handleMouseEnter = useCallback(() => {
    setIsHovered(true);
  }, []);

  const handleMouseLeave = useCallback(() => {
    setIsHovered(false);
  }, []);

  const isDegraded = Boolean(data?.isDraggedDegraded);
  const isSpotlightDimmed = Boolean(data?.isSpotlightDimmed);
  const spotlightRole = data?.spotlightRole as
    | 'UPSTREAM'
    | 'DOWNSTREAM'
    | 'INTERNAL'
    | undefined;
  const isSpotlightExternal = spotlightRole === 'UPSTREAM' || spotlightRole === 'DOWNSTREAM';
  const spotlightClass = spotlightRole
    ? `code-arch-edge--spotlight-${spotlightRole.toLowerCase()}`
    : '';
  const isHubSummary = Boolean(data?.isHubSummary);
  const foldedPeerCount = Number(data?.foldedPeerCount || 0);
  const bendPoints = data?.bendPoints;

  let edgePath = '';
  let labelX = (sourceX + targetX) / 2;
  let labelY = (sourceY + targetY) / 2;

  if (!isDegraded && bendPoints && bendPoints.length > 0) {
    const validBendPoints = bendPoints.filter((bp) => {
      const distStart = Math.hypot(bp.x - sourceX, bp.y - sourceY);
      const distEnd = Math.hypot(bp.x - targetX, bp.y - targetY);
      return distStart > 4 && distEnd > 4;
    });
    const pts = [{ x: sourceX, y: sourceY }, ...validBendPoints, { x: targetX, y: targetY }];
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

  const { statusClass, strokeColor, strokeDasharray } = resolveEdgeVisualStyle(data);

  const safeMarkerId = `code-arch-arrow-${id.replace(/[^a-zA-Z0-9_-]/g, '_')}`;
  const markerEndUrl = `url(#${safeMarkerId})`;

  const labelText = isHubSummary
    ? `+${foldedPeerCount} 个节点`
    : data?.totalCalls && data.totalCalls > 0
    ? `∑ ${data.totalCalls.toLocaleString()}`
    : `${contractCount} 契约`;

  const handleHubExpandClick = (e: React.MouseEvent) => {
    if (!isHubSummary) return;
    e.stopPropagation();
    const onExpandHub = data?.onExpandHub as ((hubNodeId: string) => void) | undefined;
    if (onExpandHub) {
      const hubNodeId = data?.hubDirection === 'INBOUND' ? String(data?.target || '') : String(data?.source || '');
      if (hubNodeId) {
        onExpandHub(hubNodeId);
      }
    }
  };

  return (
    <>
      <defs>
        <marker
          id={safeMarkerId}
          viewBox="0 0 12 12"
          markerWidth={isHovered ? 12 : 10}
          markerHeight={isHovered ? 12 : 10}
          refX="9.5"
          refY="6"
          orient="auto"
          markerUnits="userSpaceOnUse"
        >
          <path
            d="M 1 2 L 10.5 6 L 1 10 L 2.8 6 Z"
            fill={strokeColor}
            stroke={strokeColor}
            strokeWidth="0.8"
            strokeLinejoin="round"
          />
        </marker>
      </defs>

      <g
        className={`code-arch-edge-group ${isHovered ? 'code-arch-edge-group--hovered' : ''} ${spotlightClass} ${
          isSpotlightDimmed ? 'code-arch-edge--spotlight-dimmed' : ''
        }`}
        onMouseEnter={handleMouseEnter}
        onMouseLeave={handleMouseLeave}
      >
        <BaseEdge
          id={id}
          path={edgePath}
          markerEnd={markerEndUrl}
          className={`code-arch-edge ${statusClass} ${isHovered ? 'code-arch-edge--hovered' : ''} ${spotlightClass} ${
            isSpotlightDimmed ? 'code-arch-edge--spotlight-dimmed' : ''
          }`}
          style={{
            stroke: strokeColor,
            strokeWidth: isHovered || isSpotlightExternal ? strokeWidth + 0.6 : strokeWidth,
            strokeDasharray: isHubSummary ? '6 4' : strokeDasharray,
          }}
        />
        <path
          d={edgePath}
          fill="none"
          className={`code-arch-edge__flow-stream ${
            isHovered || isSpotlightExternal ? 'code-arch-edge__flow-stream--active' : ''
          }`}
          style={{
            stroke: strokeColor,
            strokeWidth: strokeWidth + 0.8,
          }}
        />
      </g>

      <EdgeLabelRenderer>
        <div
          style={{
            position: 'absolute',
            transform: `translate(-50%, -50%) translate(${labelX}px,${labelY}px)`,
            pointerEvents: 'all',
          }}
          className="nodrag nopan"
          onMouseEnter={handleMouseEnter}
          onMouseLeave={handleMouseLeave}
        >
          <span
            className={`code-arch-edge-badge ${statusClass} ${isHovered ? 'code-arch-edge-badge--hovered' : ''} ${
              isHubSummary ? 'code-arch-edge-badge--hub-more' : ''
            } ${spotlightClass} ${isSpotlightDimmed ? 'code-arch-edge--spotlight-dimmed' : ''}`}
            onClick={isHubSummary ? handleHubExpandClick : undefined}
            data-testid={isHubSummary ? `hub-expand-badge-${id}` : undefined}
            title={
              isHubSummary
                ? `点击展开折叠的 ${foldedPeerCount} 条高扇出关联边`
                : `调用方向: ${data?.source || ''} → ${data?.target || ''} (${contractCount} 个契约)`
            }
          >
            {labelText}
          </span>
        </div>
      </EdgeLabelRenderer>
    </>
  );
};
