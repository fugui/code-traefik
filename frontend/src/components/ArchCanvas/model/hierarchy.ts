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

/**
 * 查找节点所属的顶层根容器（depth = 0 或最顶层存在于 nodeMap 的祖先）
 */
export function findTopRoot(nodeId: string, nodeMap: Map<string, GraphNode>): GraphNode | null {
  const node = nodeMap.get(nodeId);
  if (!node) return null;
  for (const ancId of node.ancestors) {
    const anc = nodeMap.get(ancId);
    if (anc) return anc;
  }
  return node;
}

/**
 * M21 单层平铺代表元解析：
 * - 全局模式 (GLOBAL / null): 返回节点自身（若是顶层根）或其顶层根节点 ID；
 * - 聚焦模式 (focusScope = containerId):
 *   - 聚焦根容器自身绝不作为画布节点渲染，返回 null；
 *   - 属于聚焦容器的子孙节点，返回其在 focusScope 下的直接子节点 ID (relativeDepth === 1)；
 *   - 不属于聚焦容器子树的节点返回 null。
 */
export function resolveDirectChildRep(
  nodeId: string,
  focusScope: string | null | undefined,
  nodeMap: Map<string, GraphNode>
): string | null {
  const node = nodeMap.get(nodeId);
  if (!node) return null;

  const isGlobal = !focusScope || focusScope === 'GLOBAL' || !nodeMap.has(focusScope);
  if (isGlobal) {
    if (!node.parent_id || !nodeMap.has(node.parent_id)) {
      return node.id;
    }
    for (const ancId of node.ancestors) {
      if (nodeMap.has(ancId)) {
        return ancId;
      }
    }
    return node.id;
  }

  if (nodeId === focusScope) {
    return null;
  }

  if (node.parent_id === focusScope) {
    return node.id;
  }

  const chain = [...node.ancestors, node.id];
  const focusIdx = chain.indexOf(focusScope);
  if (focusIdx >= 0 && focusIdx + 1 < chain.length) {
    const directChildId = chain[focusIdx + 1];
    return nodeMap.has(directChildId) ? directChildId : null;
  }

  return null;
}

/**
 * M21 单层平铺可见节点集合计算：
 * - GLOBAL 仅返回所有顶层节点；
 * - focusNodeId 返回该容器的直接子节点（不含 focusNodeId 自身，也不含孙子节点）。
 */
export function computeVisibleNodeIds(
  nodes: GraphNode[],
  focusNodeId?: string | null
): Set<string> {
  const nodeMap = new Map<string, GraphNode>();
  for (const n of nodes) {
    nodeMap.set(n.id, n);
  }

  const isGlobal = !focusNodeId || focusNodeId === 'GLOBAL' || !nodeMap.has(focusNodeId);
  const visibleIds = new Set<string>();

  for (const n of nodes) {
    if (isGlobal) {
      if (!n.parent_id || !nodeMap.has(n.parent_id)) {
        visibleIds.add(n.id);
      }
    } else if (n.parent_id === focusNodeId) {
      visibleIds.add(n.id);
    }
  }

  return visibleIds;
}

