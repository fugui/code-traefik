import type { ElkNode, ElkExtendedEdge } from 'elkjs';
import { MarkerType, type Node, type Edge } from '@xyflow/react';
import {
  normalizeScopeKey,
  toScopedPositionKey,
  type GraphNode,
  type RollupEdgeModel,
  type ContainerBadgeCounts,
  type BendPoint,
  type SavedNodePosition,
} from '../../../types/topology_graph';
import { findLCA } from '../model/hierarchy';

/**
 * M21-P2 五类节点 1:1 border-box 固定视觉尺寸契约 (与 CSS 严格对齐)
 */
export const NODE_DIMENSIONS = {
  CONTAINER: { width: 260, height: 96 },
  MODULE: { width: 240, height: 88 },
  EXTERNAL_CONTEXT: { width: 240, height: 104 },
  EXTERNAL: { width: 216, height: 72 },
  UNREGISTERED: { width: 216, height: 72 },
} as const;

export function getNodeDimensions(gn: Pick<GraphNode, 'type'>): { width: number; height: number } {
  if (
    gn.type === 'SUBSYSTEM' ||
    gn.type === 'FUNCTIONAL_GROUP' ||
    gn.type === 'PROCESS_GROUP' ||
    gn.type === 'PROCESS' ||
    gn.type === 'UNDEPLOYED_CONTAINER' ||
    gn.type === 'EXTERNAL_CONTAINER'
  ) {
    return { ...NODE_DIMENSIONS.CONTAINER };
  }
  if (gn.type === 'EXTERNAL_CONTEXT') {
    return { ...NODE_DIMENSIONS.EXTERNAL_CONTEXT };
  }
  if (gn.type === 'EXTERNAL_SYSTEM' || gn.type === 'EXTERNAL_ENDPOINT') {
    return { ...NODE_DIMENSIONS.EXTERNAL };
  }
  if (gn.type === 'UNREGISTERED_INTERFACE') {
    return { ...NODE_DIMENSIONS.UNREGISTERED };
  }
  return { ...NODE_DIMENSIONS.MODULE };
}

export interface NodeRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface HandleSelection {
  sourceHandle: 'src-top' | 'src-right' | 'src-bottom' | 'src-left';
  targetHandle: 'tgt-top' | 'tgt-right' | 'tgt-bottom' | 'tgt-left';
  isStandardHorizontal: boolean;
}

/**
 * M21-P2: 根据源/目标节点几何包围盒相对方位自动选择最佳四向锚点：
 * - 纵向主导（如上下排布的 REALIZATION 实现边或垂直分布节点）：选 bottom -> top / top -> bottom
 * - 反向水平（目标在源左侧）：选 left -> right
 * - 正向水平（ELK 默认左到右分层流向）：选 right -> left (isStandardHorizontal = true)
 */
export function selectOptimalHandles(sourceRect: NodeRect, targetRect: NodeRect): HandleSelection {
  const srcCx = sourceRect.x + sourceRect.width / 2;
  const srcCy = sourceRect.y + sourceRect.height / 2;
  const tgtCx = targetRect.x + targetRect.width / 2;
  const tgtCy = targetRect.y + targetRect.height / 2;

  const dx = tgtCx - srcCx;
  const dy = tgtCy - srcCy;

  const overlapX =
    Math.min(sourceRect.x + sourceRect.width, targetRect.x + targetRect.width) -
    Math.max(sourceRect.x, targetRect.x);

  if (Math.abs(dy) > Math.abs(dx) * 1.15 && (overlapX > -40 || Math.abs(dx) < 80)) {
    if (dy >= 0) {
      return { sourceHandle: 'src-bottom', targetHandle: 'tgt-top', isStandardHorizontal: false };
    }
    return { sourceHandle: 'src-top', targetHandle: 'tgt-bottom', isStandardHorizontal: false };
  }

  if (dx < -40) {
    return { sourceHandle: 'src-left', targetHandle: 'tgt-right', isStandardHorizontal: false };
  }

  return { sourceHandle: 'src-right', targetHandle: 'tgt-left', isStandardHorizontal: true };
}

/**
 * 依据边的一致性状态、环路标记与分类解析视觉样式与语义颜色 Token
 */
export function resolveEdgeVisualStyle(data?: Partial<RollupEdgeModel>): {
  statusClass: string;
  strokeColor: string;
  strokeDasharray?: string;
} {
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
      case 'OUTBOUND_VERIFIED':
        strokeColor = 'var(--color-info)';
        strokeDasharray = '6 3';
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
      case 'IMPL_UNRESOLVED':
        strokeColor = 'var(--color-warning)';
        strokeDasharray = '8 4';
        break;
    }
  } else if (data?.isCycle) {
    statusClass = 'code-arch-edge--cycle';
    strokeColor = 'var(--color-danger)';
  } else if (data?.kind === 'REALIZATION') {
    statusClass = 'code-arch-edge--realization';
    strokeColor = 'var(--color-text-muted)';
    strokeDasharray = '4 4';
  } else if (data?.isHubSummary) {
    statusClass = 'code-arch-edge--hub-summary';
    strokeColor = 'var(--color-text-secondary)';
    strokeDasharray = '6 4';
  } else if (data?.kind === 'INTRA_PROCESS') {
    statusClass = 'code-arch-edge--intra';
    strokeColor = 'var(--color-border-hover)';
    strokeDasharray = '4 4';
  } else if (data?.kind === 'EXTERNAL_UNREGISTERED') {
    statusClass = 'code-arch-edge--external';
    strokeColor = 'var(--color-warning)';
    strokeDasharray = '6 3';
  }

  return { statusClass, strokeColor, strokeDasharray };
}

export interface LayoutedNodeInfo {
  id: string;
  x: number;
  y: number;
  absX: number;
  absY: number;
  width: number;
  height: number;
}

export type LodMode = 'COMPACT' | 'STANDARD' | 'DETAIL';

export interface CanvasNodeData {
  graphNode: GraphNode;
  isExpanded: boolean;
  onToggleExpand?: (nodeId: string) => void;
  onFocusScope?: (nodeId: string) => void;
  onTogglePin?: (nodeId: string) => void;
  isPinned?: boolean;
  lodMode?: LodMode;
  badgeCounts?: ContainerBadgeCounts;
  childCount?: number;
  searchMatchCount?: number;
  isSearchHighlighted?: boolean;
  [key: string]: unknown;
}

export type ContainerNodeType = Node<CanvasNodeData, 'containerNode'>;
export type ModuleNodeType = Node<CanvasNodeData, 'moduleCard'>;
export type ExternalNodeType = Node<CanvasNodeData, 'externalNode'>;
export type ExternalContextNodeType = Node<CanvasNodeData, 'externalContextNode'>;
export type UnregisteredInterfaceNodeType = Node<CanvasNodeData, 'unregisteredInterfaceNode'>;
export type ArchCanvasNode =
  | ContainerNodeType
  | ModuleNodeType
  | ExternalNodeType
  | ExternalContextNodeType
  | UnregisteredInterfaceNodeType;

export type ArchCanvasEdge = Edge<RollupEdgeModel, 'rollupEdge'>;

export interface ProcessLayoutExtraOptions {
  focusScope?: string | null;
  savedPositions?: Record<string, SavedNodePosition> | null;
  pinnedNodeIds?: Set<string>;
  onFocusScope?: (nodeId: string) => void;
  onTogglePin?: (nodeId: string) => void;
  onExpandHub?: (nodeId: string) => void;
  containerSearchMatchCounts?: Record<string, number>;
  lodMode?: LodMode;
  highlightedNodeId?: string | null;
}

/**
 * 遍历 ELK 节点树，递归提取每个节点的局部坐标与全局绝对坐标 (absX, absY)
 */
export function extractNodeCoordinates(
  elkNode: ElkNode,
  parentAbsX = 0,
  parentAbsY = 0,
  result = new Map<string, LayoutedNodeInfo>()
): Map<string, LayoutedNodeInfo> {
  const currentAbsX = parentAbsX + (elkNode.x ?? 0);
  const currentAbsY = parentAbsY + (elkNode.y ?? 0);

  if (elkNode.id && elkNode.id !== 'root') {
    result.set(elkNode.id, {
      id: elkNode.id,
      x: elkNode.x ?? 0,
      y: elkNode.y ?? 0,
      absX: currentAbsX,
      absY: currentAbsY,
      width: elkNode.width ?? 240,
      height: elkNode.height ?? 88,
    });
  }

  if (elkNode.children) {
    for (const child of elkNode.children) {
      extractNodeCoordinates(child, currentAbsX, currentAbsY, result);
    }
  }

  return result;
}

/**
 * M21-P2: 为汇聚同一端点的多条 SmoothStep 平行边计算确定性错开偏移量 (26px 步进)，
 * 彻底防止同靶/同源平行边的路径和「1 契约」标签重叠遮挡。
 */
export function computeEdgeSiblingOffsets(
  edges: ArchCanvasEdge[],
  nodeRectMap: ReadonlyMap<string, NodeRect>
): ArchCanvasEdge[] {
  const targetGroups = new Map<string, ArchCanvasEdge[]>();
  const sourceGroups = new Map<string, ArchCanvasEdge[]>();

  for (const edge of edges) {
    const usesSmoothStep =
      Boolean(edge.data?.isDraggedDegraded) ||
      !edge.data?.bendPoints ||
      edge.data.bendPoints.length === 0;
    if (!usesSmoothStep) continue;

    const tgtKey = `${edge.target}::${edge.targetHandle ?? 'tgt-left'}`;
    const tgtBucket = targetGroups.get(tgtKey);
    if (tgtBucket) tgtBucket.push(edge);
    else targetGroups.set(tgtKey, [edge]);

    const srcKey = `${edge.source}::${edge.sourceHandle ?? 'src-right'}`;
    const srcBucket = sourceGroups.get(srcKey);
    if (srcBucket) srcBucket.push(edge);
    else sourceGroups.set(srcKey, [edge]);
  }

  const offsetMap = new Map<string, number>();

  const assignGroupOffsets = (bucket: ArchCanvasEdge[], bySourceCoord: boolean) => {
    if (bucket.length < 2) return;
    const sorted = [...bucket].sort((a, b) => {
      const peerA = nodeRectMap.get(bySourceCoord ? a.source : a.target);
      const peerB = nodeRectMap.get(bySourceCoord ? b.source : b.target);
      if (peerA && peerB) {
        const dy = peerA.y - peerB.y;
        if (Math.abs(dy) > 1) return dy;
        const dx = peerA.x - peerB.x;
        if (Math.abs(dx) > 1) return dx;
      }
      return a.id.localeCompare(b.id);
    });

    const count = sorted.length;
    sorted.forEach((edge, idx) => {
      const offset = (idx - (count - 1) / 2) * 26;
      const prev = offsetMap.get(edge.id);
      if (prev === undefined || Math.abs(offset) > Math.abs(prev)) {
        offsetMap.set(edge.id, offset);
      }
    });
  };

  for (const bucket of targetGroups.values()) {
    assignGroupOffsets(bucket, true);
  }
  for (const bucket of sourceGroups.values()) {
    assignGroupOffsets(bucket, false);
  }

  return edges.map((edge) => {
    const offset = offsetMap.get(edge.id);
    if (offset === undefined || !edge.data) return edge;
    return {
      ...edge,
      data: {
        ...edge.data,
        siblingOffset: offset,
      },
    };
  });
}

function resolveSavedPositionForScope(
  savedPositions: Record<string, SavedNodePosition> | null | undefined,
  nodeId: string,
  focusScope: string
): SavedNodePosition | undefined {
  if (!savedPositions) return undefined;
  const scopedKey = toScopedPositionKey(focusScope, nodeId);
  return savedPositions[scopedKey] ?? savedPositions[nodeId];
}

/**
 * 后处理 ELK 正交排版坐标：
 * 1. 优先读取当前作用域 (focusScope) 下已固化的节点坐标
 * 2. 依据四向方位自动选择最优锚点 (selectOptimalHandles)
 * 3. 非正向水平或节点位移退化边主动清空陈旧 bendPoints 并注入平行边 26px 标签错开偏移
 */
export function processLayoutCoordinates(
  layoutedRoot: ElkNode,
  visibleNodes: GraphNode[],
  rollupEdges: RollupEdgeModel[],
  draggedNodeIds: Set<string>,
  expandState: Record<string, boolean> = {},
  badgeCounts: Record<string, ContainerBadgeCounts> = {},
  onToggleExpand?: (nodeId: string) => void,
  childCounts: Record<string, number> = {},
  extraOptions?: ProcessLayoutExtraOptions
): {
  nodes: ArchCanvasNode[];
  edges: ArchCanvasEdge[];
} {
  const coordMap = extractNodeCoordinates(layoutedRoot, 0, 0);
  const nodeMap = new Map(visibleNodes.map((n) => [n.id, n]));
  const savedPositions = extraOptions?.savedPositions;
  const pinnedNodeIds = extraOptions?.pinnedNodeIds;
  const scopeKey = normalizeScopeKey(extraOptions?.focusScope);

  // 1. 父优先拓扑排序 (Parent-First Order)
  const sortedGraphNodes = [...visibleNodes].sort((a, b) => {
    if (a.depth !== b.depth) {
      return a.depth - b.depth;
    }
    return a.sort_order - b.sort_order;
  });

  const nodes: ArchCanvasNode[] = [];
  const flowNodeById = new Map<string, ArchCanvasNode>();
  const nodeRectMap = new Map<string, NodeRect>();
  const displacedNodeIds = new Set<string>();

  for (const gn of sortedGraphNodes) {
    const layout = coordMap.get(gn.id);
    const saved = resolveSavedPositionForScope(savedPositions, gn.id, scopeKey);
    const hasParent = Boolean(gn.parent_id && nodeMap.has(gn.parent_id));
    const hasSavedCoord = Boolean(saved && Number.isFinite(saved.x) && Number.isFinite(saved.y));
    const posX = hasSavedCoord
      ? hasParent
        ? Math.max(16, saved!.x)
        : saved!.x
      : layout?.x ?? 0;
    const posY = hasSavedCoord
      ? hasParent
        ? Math.max(52, saved!.y)
        : saved!.y
      : layout?.y ?? 0;
    const isExpanded = Boolean(expandState[gn.id]);
    const isPinned = Boolean(
      pinnedNodeIds?.has(gn.id) ||
        pinnedNodeIds?.has(toScopedPositionKey(scopeKey, gn.id)) ||
        saved?.pinned
    );

    if (hasSavedCoord && layout && (Math.abs(posX - layout.x) > 1 || Math.abs(posY - layout.y) > 1)) {
      displacedNodeIds.add(gn.id);
    }

    let nodeType:
      | 'containerNode'
      | 'moduleCard'
      | 'externalNode'
      | 'externalContextNode'
      | 'unregisteredInterfaceNode' = 'moduleCard';
    if (
      gn.type === 'SUBSYSTEM' ||
      gn.type === 'FUNCTIONAL_GROUP' ||
      gn.type === 'PROCESS_GROUP' ||
      gn.type === 'PROCESS' ||
      gn.type === 'UNDEPLOYED_CONTAINER' ||
      gn.type === 'EXTERNAL_CONTAINER'
    ) {
      nodeType = 'containerNode';
    } else if (gn.type === 'EXTERNAL_CONTEXT') {
      nodeType = 'externalContextNode';
    } else if (gn.type === 'EXTERNAL_SYSTEM' || gn.type === 'EXTERNAL_ENDPOINT') {
      nodeType = 'externalNode';
    } else if (gn.type === 'UNREGISTERED_INTERFACE') {
      nodeType = 'unregisteredInterfaceNode';
    }

    const defaultDims = getNodeDimensions(gn);
    const width =
      isExpanded && saved?.width
        ? Math.max(saved.width, layout?.width ?? 0)
        : layout?.width ?? saved?.width ?? defaultDims.width;
    const height =
      isExpanded && saved?.height
        ? Math.max(saved.height, layout?.height ?? 0)
        : layout?.height ?? saved?.height ?? defaultDims.height;

    const flowNode: ArchCanvasNode = {
      id: gn.id,
      type: nodeType,
      position: { x: posX, y: posY },
      data: {
        graphNode: gn,
        isExpanded,
        onToggleExpand,
        onFocusScope: extraOptions?.onFocusScope,
        onTogglePin: extraOptions?.onTogglePin,
        isPinned,
        lodMode: extraOptions?.lodMode,
        badgeCounts: badgeCounts[gn.id],
        childCount: childCounts[gn.id] ?? 0,
        searchMatchCount: extraOptions?.containerSearchMatchCounts?.[gn.id] ?? 0,
        isSearchHighlighted: extraOptions?.highlightedNodeId === gn.id,
      },
      style: {
        width,
        height,
      },
    };

    if (hasParent && gn.parent_id) {
      flowNode.parentId = gn.parent_id;
    }

    nodes.push(flowNode);
    flowNodeById.set(gn.id, flowNode);
    nodeRectMap.set(gn.id, {
      x: posX,
      y: posY,
      width,
      height,
    });
  }

  // 1b. 兼容旧嵌套测试：自底向上强制撑大显式展开父容器包围盒
  for (let i = nodes.length - 1; i >= 0; i--) {
    const child = nodes[i];
    if (!child.parentId) continue;
    const parent = flowNodeById.get(child.parentId);
    if (!parent || !parent.data.isExpanded) continue;

    const childW = Number(child.style?.width) || NODE_DIMENSIONS.MODULE.width;
    const childH = Number(child.style?.height) || NODE_DIMENSIONS.MODULE.height;

    const requiredWidth = child.position.x + childW + 16;
    const requiredHeight = child.position.y + childH + 16;
    const currParentW = Number(parent.style?.width) || NODE_DIMENSIONS.CONTAINER.width;
    const currParentH = Number(parent.style?.height) || 140;

    if (requiredWidth > currParentW || requiredHeight > currParentH) {
      parent.style = {
        ...parent.style,
        width: Math.max(currParentW, requiredWidth),
        height: Math.max(currParentH, requiredHeight),
      };
      nodeRectMap.set(parent.id, {
        x: parent.position.x,
        y: parent.position.y,
        width: Number(parent.style.width),
        height: Number(parent.style.height),
      });
    }
  }

  // 2. 收集 ELK 输出的边拐点 sections
  const elkEdgeSectionsMap = new Map<
    string,
    { startPoint?: BendPoint; endPoint?: BendPoint; bendPoints?: BendPoint[] }
  >();

  function collectEdgeSections(node: ElkNode) {
    if (node.edges) {
      for (const e of node.edges as ElkExtendedEdge[]) {
        if (e.id && e.sections && e.sections.length > 0) {
          const s = e.sections[0];
          elkEdgeSectionsMap.set(e.id, {
            startPoint: s.startPoint,
            endPoint: s.endPoint,
            bendPoints: s.bendPoints,
          });
        }
      }
    }
    if (node.children) {
      for (const c of node.children) {
        collectEdgeSections(c);
      }
    }
  }
  collectEdgeSections(layoutedRoot);

  // 3. 构建 React Flow 边模型：四向智能锚点选择 + 陈旧 bendPoints 清理 + 退化 SmoothStep 标记
  const baseTouched = new Set<string>([...draggedNodeIds, ...displacedNodeIds]);
  const effectiveTouched = new Set<string>(baseTouched);
  if (baseTouched.size > 0) {
    for (const gn of visibleNodes) {
      if (gn.ancestors.some((anc) => baseTouched.has(anc))) {
        effectiveTouched.add(gn.id);
      }
    }
  }

  const edges: ArchCanvasEdge[] = [];

  for (const re of rollupEdges) {
    const srcRect = nodeRectMap.get(re.source);
    const tgtRect = nodeRectMap.get(re.target);
    const handleSel: HandleSelection =
      srcRect && tgtRect
        ? selectOptimalHandles(srcRect, tgtRect)
        : { sourceHandle: 'src-right', targetHandle: 'tgt-left', isStandardHorizontal: true };

    const isNodeMovedOrFolded =
      Boolean(re.isDraggedDegraded) ||
      Boolean(re.isHubSummary) ||
      effectiveTouched.has(re.source) ||
      effectiveTouched.has(re.target);

    const isDegraded = isNodeMovedOrFolded || !handleSel.isStandardHorizontal;
    const elkSection = elkEdgeSectionsMap.get(re.id);

    let finalBendPoints: BendPoint[] | undefined;

    if (
      !isDegraded &&
      handleSel.isStandardHorizontal &&
      elkSection?.bendPoints &&
      elkSection.bendPoints.length > 0
    ) {
      const lcaId = findLCA([re.source, re.target], nodeMap);
      let containingAbsX = 0;
      let containingAbsY = 0;

      if (lcaId && coordMap.has(lcaId)) {
        const lcaLayout = coordMap.get(lcaId)!;
        containingAbsX = lcaLayout.absX;
        containingAbsY = lcaLayout.absY;
      }

      finalBendPoints = elkSection.bendPoints.map((bp) => ({
        x: bp.x + containingAbsX,
        y: bp.y + containingAbsY,
      }));
    }

    const updatedModel: RollupEdgeModel = {
      ...re,
      bendPoints: finalBendPoints,
      isDraggedDegraded: isDegraded,
      sourceHandle: handleSel.sourceHandle,
      targetHandle: handleSel.targetHandle,
      onExpandHub: extraOptions?.onExpandHub,
    };

    const { strokeColor } = resolveEdgeVisualStyle(updatedModel);

    edges.push({
      id: re.id,
      source: re.source,
      target: re.target,
      sourceHandle: handleSel.sourceHandle,
      targetHandle: handleSel.targetHandle,
      type: 'rollupEdge',
      data: updatedModel,
      markerEnd: {
        type: MarkerType.ArrowClosed,
        width: 16,
        height: 16,
        color: strokeColor,
      },
    });
  }

  return {
    nodes,
    edges: computeEdgeSiblingOffsets(edges, nodeRectMap),
  };
}
