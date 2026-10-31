import type { ElkNode, ElkExtendedEdge } from 'elkjs';
import type { Node, Edge } from '@xyflow/react';
import type {
  GraphNode,
  RollupEdgeModel,
  ContainerBadgeCounts,
  BendPoint,
} from '../../../types/topology_graph';
import { findLCA } from '../model/hierarchy';

export interface LayoutedNodeInfo {
  id: string;
  x: number;
  y: number;
  absX: number;
  absY: number;
  width: number;
  height: number;
}

export interface CanvasNodeData {
  graphNode: GraphNode;
  isExpanded: boolean;
  onToggleExpand?: (nodeId: string) => void;
  badgeCounts?: ContainerBadgeCounts;
  childCount?: number;
  [key: string]: unknown;
}

export type ContainerNodeType = Node<CanvasNodeData, 'containerNode'>;
export type ModuleNodeType = Node<CanvasNodeData, 'moduleCard'>;
export type ExternalNodeType = Node<CanvasNodeData, 'externalNode'>;
export type UnregisteredInterfaceNodeType = Node<CanvasNodeData, 'unregisteredInterfaceNode'>;
export type ArchCanvasNode =
  | ContainerNodeType
  | ModuleNodeType
  | ExternalNodeType
  | UnregisteredInterfaceNodeType;

export type ArchCanvasEdge = Edge<RollupEdgeModel, 'rollupEdge'>;

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
 * 3. 手动拖拽节点后关联边自动标记 isDraggedDegraded = true 退化为 SmoothStep
 */
export function processLayoutCoordinates(
  layoutedRoot: ElkNode,
  visibleNodes: GraphNode[],
  rollupEdges: RollupEdgeModel[],
  draggedNodeIds: Set<string>,
  expandState: Record<string, boolean> = {},
  badgeCounts: Record<string, ContainerBadgeCounts> = {},
  onToggleExpand?: (nodeId: string) => void,
  childCounts: Record<string, number> = {}
): {
  nodes: ArchCanvasNode[];
  edges: ArchCanvasEdge[];
} {
  const coordMap = extractNodeCoordinates(layoutedRoot, 0, 0);
  const nodeMap = new Map(visibleNodes.map((n) => [n.id, n]));

  // 1. 父优先拓扑排序 (Parent-First Order)
  // 按照树深度 depth 升序排序，且同深度按 sort_order 升序，确保父节点索引必小于子节点
  const sortedGraphNodes = [...visibleNodes].sort((a, b) => {
    if (a.depth !== b.depth) {
      return a.depth - b.depth;
    }
    return a.sort_order - b.sort_order;
  });

  const nodes: ArchCanvasNode[] = [];

  for (const gn of sortedGraphNodes) {
    const layout = coordMap.get(gn.id);
    const posX = layout?.x ?? 0;
    const posY = layout?.y ?? 0;

    // 确定 React Flow 节点类型
    let nodeType: 'containerNode' | 'moduleCard' | 'externalNode' | 'unregisteredInterfaceNode' = 'moduleCard';
    if (
      gn.type === 'SUBSYSTEM' ||
      gn.type === 'FUNCTIONAL_GROUP' ||
      gn.type === 'PROCESS_GROUP' ||
      gn.type === 'PROCESS' ||
      gn.type === 'UNDEPLOYED_CONTAINER' ||
      gn.type === 'EXTERNAL_CONTAINER'
    ) {
      nodeType = 'containerNode';
    } else if (gn.type === 'EXTERNAL_SYSTEM' || gn.type === 'EXTERNAL_ENDPOINT') {
      nodeType = 'externalNode';
    } else if (gn.type === 'UNREGISTERED_INTERFACE') {
      nodeType = 'unregisteredInterfaceNode';
    }

    const flowNode: ArchCanvasNode = {
      id: gn.id,
      type: nodeType,
      position: { x: posX, y: posY },
      data: {
        graphNode: gn,
        isExpanded: Boolean(expandState[gn.id]),
        onToggleExpand,
        badgeCounts: badgeCounts[gn.id],
        childCount: childCounts[gn.id] ?? 0,
      },
      style: {
        width: layout?.width,
        height: layout?.height,
      },
    };

    if (gn.parent_id && nodeMap.has(gn.parent_id)) {
      flowNode.parentId = gn.parent_id;
    }

    nodes.push(flowNode);
  }

  // 2. 收集 ELK 输出的边拐点 sections
  const elkEdgeSectionsMap = new Map<string, { startPoint?: BendPoint; endPoint?: BendPoint; bendPoints?: BendPoint[] }>();

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

  // 3. 构建 React Flow 边模型，累加跨复合容器全局坐标偏移与拖拽退化标记
  const edges: ArchCanvasEdge[] = [];

  for (const re of rollupEdges) {
    const isDegraded = draggedNodeIds.has(re.source) || draggedNodeIds.has(re.target);
    const elkSection = elkEdgeSectionsMap.get(re.id);

    let finalBendPoints: BendPoint[] | undefined;

    if (elkSection?.bendPoints && elkSection.bendPoints.length > 0) {
      // 跨容器复合边：查找 source 与 target 在可见层级中的 LCA (containingNode)
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
    };

    edges.push({
      id: re.id,
      source: re.source,
      target: re.target,
      type: 'rollupEdge',
      data: updatedModel,
    });
  }

  return { nodes, edges };
}
