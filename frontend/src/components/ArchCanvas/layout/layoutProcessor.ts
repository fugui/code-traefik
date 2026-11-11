import type { ElkNode, ElkExtendedEdge } from 'elkjs';
import { MarkerType, type Node, type Edge } from '@xyflow/react';
import type {
  GraphNode,
  RollupEdgeModel,
  ContainerBadgeCounts,
  BendPoint,
  SavedNodePosition,
} from '../../../types/topology_graph';
import { findLCA } from '../model/hierarchy';

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
  savedPositions?: Record<string, SavedNodePosition> | null;
  pinnedNodeIds?: Set<string>;
  onFocusScope?: (nodeId: string) => void;
  onTogglePin?: (nodeId: string) => void;
  onExpandHub?: (nodeId: string) => void;
  containerSearchMatchCounts?: Record<string, number>;
  lodMode?: LodMode;
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
      width: elkNode.width ?? 200,
      height: elkNode.height ?? 70,
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
 * 后处理 ELK 复合正交排版坐标：
 * 1. 严格保证父节点优先（Parent-First Topological Order）
 * 2. 跨容器复合折线边 bendPoints 累加所属 containingNode 全局绝对坐标 (absX, absY)
 * 3. 手动拖拽或局部重排波及节点关联边自动标记 isDraggedDegraded = true 退化为 SmoothStep
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

  // 1. 父优先拓扑排序 (Parent-First Order)
  const sortedGraphNodes = [...visibleNodes].sort((a, b) => {
    if (a.depth !== b.depth) {
      return a.depth - b.depth;
    }
    return a.sort_order - b.sort_order;
  });

  const nodes: ArchCanvasNode[] = [];
  const flowNodeById = new Map<string, ArchCanvasNode>();
  const displacedNodeIds = new Set<string>();

  for (const gn of sortedGraphNodes) {
    const layout = coordMap.get(gn.id);
    const saved = savedPositions?.[gn.id];
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
    const isPinned = Boolean(pinnedNodeIds?.has(gn.id) || saved?.pinned);

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

    const width =
      isExpanded && saved?.width
        ? Math.max(saved.width, layout?.width ?? 0)
        : layout?.width ?? saved?.width;
    const height =
      isExpanded && saved?.height
        ? Math.max(saved.height, layout?.height ?? 0)
        : layout?.height ?? saved?.height;

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
  }

  // 1b. 自底向上 (Bottom-Up) 强制撑大展开态父容器包围盒，彻底杜绝子容器/子模块溢出父容器框外
  for (let i = nodes.length - 1; i >= 0; i--) {
    const child = nodes[i];
    if (!child.parentId) continue;
    const parent = flowNodeById.get(child.parentId);
    if (!parent || !parent.data.isExpanded) continue;

    const defaultChildW = child.type === 'containerNode' ? 260 : 220;
    const defaultChildH = child.type === 'containerNode' ? 80 : 74;
    const childW = Number(child.style?.width) || defaultChildW;
    const childH = Number(child.style?.height) || defaultChildH;

    const requiredWidth = child.position.x + childW + 16;
    const requiredHeight = child.position.y + childH + 16;
    const currParentW = Number(parent.style?.width) || 260;
    const currParentH = Number(parent.style?.height) || 140;

    if (requiredWidth > currParentW || requiredHeight > currParentH) {
      parent.style = {
        ...parent.style,
        width: Math.max(currParentW, requiredWidth),
        height: Math.max(currParentH, requiredHeight),
      };
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

  // 3. 构建 React Flow 边模型，累加跨复合容器全局坐标偏移与拖拽/重排退化标记
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
    const isDegraded =
      Boolean(re.isDraggedDegraded) ||
      Boolean(re.isHubSummary) ||
      effectiveTouched.has(re.source) ||
      effectiveTouched.has(re.target);
    const elkSection = elkEdgeSectionsMap.get(re.id);

    let finalBendPoints: BendPoint[] | undefined;

    if (elkSection?.bendPoints && elkSection.bendPoints.length > 0) {
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
      onExpandHub: extraOptions?.onExpandHub,
    };

    const { strokeColor } = resolveEdgeVisualStyle(updatedModel);

    edges.push({
      id: re.id,
      source: re.source,
      target: re.target,
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

  return { nodes, edges };
}
