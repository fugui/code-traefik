import type { GraphNode } from '../../../types/topology_graph';

export interface TreeIndex {
  nodeMap: Map<string, GraphNode>;
  childrenMap: Map<string, GraphNode[]>;
  rootNodes: GraphNode[];
}

/**
 * 构建树索引结构
 */
export function buildTreeIndex(nodes: GraphNode[]): TreeIndex {
  const nodeMap = new Map<string, GraphNode>();
  const childrenMap = new Map<string, GraphNode[]>();
  const rootNodes: GraphNode[] = [];

  for (const node of nodes) {
    nodeMap.set(node.id, node);
    if (!childrenMap.has(node.id)) {
      childrenMap.set(node.id, []);
    }
  }

  for (const node of nodes) {
    if (node.parent_id && nodeMap.has(node.parent_id)) {
      const children = childrenMap.get(node.parent_id);
      if (children) {
        children.push(node);
      }
    } else {
      rootNodes.push(node);
    }
  }

  return { nodeMap, childrenMap, rootNodes };
}

/**
 * 获取从根节点到当前节点的完整祖先链路 (从顶层至直接父节点, 再到当前节点)
 */
export function getFullAncestry(nodeId: string, nodeMap: Map<string, GraphNode>): string[] {
  const node = nodeMap.get(nodeId);
  if (!node) return [nodeId];
  return [...node.ancestors, node.id];
}

/**
 * 查找一组节点在层级树中的最近公共祖先 (LCA, Lowest Common Ancestor)
 * 若无公共祖先（例如跨根子系统），则返回 null
 */
export function findLCA(nodeIds: string[], nodeMap: Map<string, GraphNode>): string | null {
  if (nodeIds.length === 0) return null;
  if (nodeIds.length === 1) return nodeIds[0];

  let commonAncestors = getFullAncestry(nodeIds[0], nodeMap);

  for (let i = 1; i < nodeIds.length; i++) {
    const currentAncestry = getFullAncestry(nodeIds[i], nodeMap);
    const nextCommon: string[] = [];
    const minLen = Math.min(commonAncestors.length, currentAncestry.length);
    for (let j = 0; j < minLen; j++) {
      if (commonAncestors[j] === currentAncestry[j]) {
        nextCommon.push(commonAncestors[j]);
      } else {
        break;
      }
    }
    commonAncestors = nextCommon;
    if (commonAncestors.length === 0) return null;
  }

  return commonAncestors.length > 0 ? commonAncestors[commonAncestors.length - 1] : null;
}

