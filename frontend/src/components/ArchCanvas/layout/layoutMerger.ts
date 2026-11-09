import type {
  GraphNode,
  GraphEdge,
  ExpandStateMap,
  RollupEdgeModel,
  SavedNodePosition,
  SavedViewport,
} from '../../../types/topology_graph';

export type LayoutPlanMode = 'NONE' | 'ELK_SUBTREE' | 'ELK_FULL' | 'SAVED_POSITIONS';

export interface LayoutPlanInput {
  reason: 'INIT' | 'RESET' | 'DRAG' | 'CONTAINER_TOGGLE' | 'FILTER_CHANGE' | 'BOOKMARK_RESTORE';
  prevExpandState: ExpandStateMap;
  nextExpandState: ExpandStateMap;
  savedPositions?: Record<string, SavedNodePosition> | null;
  nodes: GraphNode[];
}

export interface LayoutPlanResult {
  mode: LayoutPlanMode;
  subtreeRootId?: string;
}

export interface NodeBox {
  id: string;
  parentId?: string | null;
  x: number;
  y: number;
  width: number;
  height: number;
  pinned?: boolean;
  isExpanded?: boolean;
}

/**
 * 位置记忆混合布局调度规划器：
 * - 单纯节点拖拽 -> NONE (严禁触发 ELK 重排)
 * - 单一容器或单棵子树展开/折叠 -> ELK_SUBTREE (仅局部重排该子树)
 * - 书签/基准恢复且存在有效坐标 -> SAVED_POSITIONS
 * - 冷启动或用户显式点击重置布局 -> ELK_FULL
 */
export function planLayout(input: LayoutPlanInput): LayoutPlanResult {
  const { reason, prevExpandState, nextExpandState, savedPositions, nodes } = input;

  if (reason === 'DRAG') {
    return { mode: 'NONE' };
  }

  if (reason === 'RESET') {
    return { mode: 'ELK_FULL' };
  }

  const hasSaved = Boolean(savedPositions && Object.keys(savedPositions).length > 0);

  if (reason === 'BOOKMARK_RESTORE' || reason === 'INIT') {
    if (hasSaved) {
      return { mode: 'SAVED_POSITIONS' };
    }
    return { mode: 'ELK_FULL' };
  }

  if (reason === 'CONTAINER_TOGGLE' && hasSaved) {
    const allKeys = new Set([
      ...Object.keys(prevExpandState),
      ...Object.keys(nextExpandState),
    ]);
    const toggledIds: string[] = [];
    for (const k of allKeys) {
      if (Boolean(prevExpandState[k]) !== Boolean(nextExpandState[k])) {
        toggledIds.push(k);
      }
    }

    if (toggledIds.length === 1) {
      return { mode: 'ELK_SUBTREE', subtreeRootId: toggledIds[0] };
    }

    if (toggledIds.length > 1) {
      const nodeMap = new Map(nodes.map((n) => [n.id, n]));
      // 检查是否全部处于某个被切换容器的子树内
      for (const candidateRoot of toggledIds) {
        const allUnderRoot = toggledIds.every((id) => {
          if (id === candidateRoot) return true;
          const n = nodeMap.get(id);
          return Boolean(n && n.ancestors.includes(candidateRoot));
        });
        if (allUnderRoot) {
          return { mode: 'ELK_SUBTREE', subtreeRootId: candidateRoot };
        }
      }
    }
  }

  return { mode: 'ELK_FULL' };
}

function isBoxOverlapping(a: NodeBox, b: NodeBox, minGap: number): boolean {
  return (
    a.x < b.x + b.width + minGap &&
    a.x + a.width + minGap > b.x &&
    a.y < b.y + b.height + minGap &&
    a.y + a.height + minGap > b.y
  );
}

/**
 * 自底向上 (Bottom-Up) 碰撞推挤与父容器包围盒联动算法：
 * 1. 按树深度从最深层到根层 (depth=0) 逐层处理同父容器内的兄弟节点；
 * 2. 若两个兄弟节点 AABB 间距小于 minGap (16px)，沿主轴推开非固定节点（pinned=true 节点坐标锁定不动）；
 * 3. 子层处理完毕后，自动撑大展开态父容器的 width / height 包围盒并继续向上层传播。
 */
export function resolveOverlapsBottomUp(
  initialBoxes: Map<string, NodeBox>,
  nodes: GraphNode[],
  anchorNodeId?: string,
  minGap = 16
): {
  boxes: Map<string, NodeBox>;
  touchedIds: Set<string>;
} {
  const boxes = new Map<string, NodeBox>();
  for (const [k, v] of initialBoxes.entries()) {
    boxes.set(k, { ...v });
  }

  const touchedIds = new Set<string>();
  if (anchorNodeId) {
    touchedIds.add(anchorNodeId);
  }

  const nodeMap = new Map<string, GraphNode>();
  let maxDepth = 0;
  for (const n of nodes) {
    nodeMap.set(n.id, n);
    if (n.depth > maxDepth) {
      maxDepth = n.depth;
    }
  }

  for (let d = maxDepth; d >= 0; d--) {
    // 按 parentId 分组当前深度的可见节点
    const groups = new Map<string, NodeBox[]>();
    for (const box of boxes.values()) {
      const gn = nodeMap.get(box.id);
      const depth = gn?.depth ?? (box.parentId ? 1 : 0);
      if (depth !== d) continue;
      const pid = box.parentId ?? gn?.parent_id ?? '__ROOT__';
      const list = groups.get(pid);
      if (list) list.push(box);
      else groups.set(pid, [box]);
    }

    for (const [pid, siblings] of groups.entries()) {
      if (siblings.length > 1) {
        siblings.sort((a, b) => {
          if (a.pinned && !b.pinned) return -1;
          if (!a.pinned && b.pinned) return 1;
          if (a.id === anchorNodeId) return -1;
          if (b.id === anchorNodeId) return 1;
          if (a.x !== b.x) return a.x - b.x;
          if (a.y !== b.y) return a.y - b.y;
          return a.id.localeCompare(b.id);
        });

        for (let i = 0; i < siblings.length; i++) {
          for (let j = i + 1; j < siblings.length; j++) {
            const fixed = siblings[i];
            const movable = siblings[j];
            if (!isBoxOverlapping(fixed, movable, minGap)) continue;

            if (movable.pinned && !fixed.pinned) {
              fixed.x = movable.x + movable.width + minGap;
              touchedIds.add(fixed.id);
            } else if (!movable.pinned) {
              movable.x = fixed.x + fixed.width + minGap;
              touchedIds.add(movable.id);
            }
          }
        }
      }

      // 动态更新展开态父容器的包围盒
      if (pid !== '__ROOT__' && boxes.has(pid)) {
        const parentBox = boxes.get(pid)!;
        if (parentBox.isExpanded !== false && siblings.length > 0) {
          let maxRight = 0;
          let maxBottom = 0;
          for (const child of siblings) {
            maxRight = Math.max(maxRight, child.x + child.width + minGap);
            maxBottom = Math.max(maxBottom, child.y + child.height + minGap);
          }
          if (maxRight > parentBox.width || maxBottom > parentBox.height) {
            parentBox.width = Math.max(parentBox.width, maxRight);
            parentBox.height = Math.max(parentBox.height, maxBottom);
            touchedIds.add(parentBox.id);
          }
        }
      }
    }
  }

  return { boxes, touchedIds };
}

export interface MergeLocalLayoutInput {
  subtreeRootId: string;
  subtreeBoxes: Map<string, NodeBox>;
  existingPositions: Record<string, SavedNodePosition>;
  nodes: GraphNode[];
  expandState: ExpandStateMap;
}

/**
 * 将局部子树 ELK 排版结果合并回全局位置记忆表，并执行自底向上防重叠推挤
 */
export function mergeLocalLayout(input: MergeLocalLayoutInput): {
  positions: Record<string, SavedNodePosition>;
  touchedIds: Set<string>;
} {
  const { subtreeRootId, subtreeBoxes, existingPositions, nodes, expandState } = input;
  const nodeMap = new Map(nodes.map((n) => [n.id, n]));
  const combinedBoxes = new Map<string, NodeBox>();

  for (const [id, saved] of Object.entries(existingPositions)) {
    const gn = nodeMap.get(id);
    combinedBoxes.set(id, {
      id,
      parentId: saved.parent_id ?? gn?.parent_id ?? null,
      x: saved.x,
      y: saved.y,
      width: saved.width ?? (saved.is_expanded ? 320 : 260),
      height: saved.height ?? (saved.is_expanded ? 180 : 80),
      pinned: saved.pinned,
      isExpanded: Boolean(expandState[id]),
    });
  }

  const prevRoot = existingPositions[subtreeRootId];
  for (const [id, subBox] of subtreeBoxes.entries()) {
    const gn = nodeMap.get(id);
    if (id === subtreeRootId && prevRoot) {
      combinedBoxes.set(id, {
        ...subBox,
        parentId: prevRoot.parent_id ?? gn?.parent_id ?? null,
        x: prevRoot.x,
        y: prevRoot.y,
        pinned: prevRoot.pinned,
        isExpanded: Boolean(expandState[id]),
      });
    } else {
      combinedBoxes.set(id, {
        ...subBox,
        parentId: subBox.parentId ?? gn?.parent_id ?? null,
        isExpanded: Boolean(expandState[id]),
      });
    }
  }

  const resolved = resolveOverlapsBottomUp(combinedBoxes, nodes, subtreeRootId);
  const positions: Record<string, SavedNodePosition> = {};

  for (const [id, box] of resolved.boxes.entries()) {
    positions[id] = {
      x: box.x,
      y: box.y,
      parent_id: box.parentId ?? null,
      is_expanded: Boolean(expandState[id]),
      pinned: box.pinned,
      width: box.width,
      height: box.height,
    };
  }

  return {
    positions,
    touchedIds: resolved.touchedIds,
  };
}

/**
 * 标记受局部重排、推挤或拖拽影响的连线：
 * 任何端点属于 touchedIds 或其后代节点的汇总边自动切换 isDraggedDegraded = true (SmoothStep)，
 * 未受波及的外部连线保持原有正交折线拐点。
 */
export function markDegradedEdges(
  edges: RollupEdgeModel[],
  touchedIds: Set<string>,
  nodes: GraphNode[]
): RollupEdgeModel[] {
  if (touchedIds.size === 0) {
    return edges.map((e) => ({
      ...e,
      isDraggedDegraded: Boolean(e.isDraggedDegraded),
    }));
  }

  const effectiveTouched = new Set<string>(touchedIds);
  for (const n of nodes) {
    if (n.ancestors.some((anc) => touchedIds.has(anc))) {
      effectiveTouched.add(n.id);
    }
  }

  return edges.map((e) => {
    const degraded =
      Boolean(e.isDraggedDegraded) ||
      effectiveTouched.has(e.source) ||
      effectiveTouched.has(e.target);
    return {
      ...e,
      isDraggedDegraded: degraded,
    };
  });
}

/**
 * 为新增或重挂载节点分配增量初始坐标：
 * 优先放置在同父容器已定位关联邻居的质心下方，若无关联邻居则追加在同父容器最右侧兄弟右侧 (+28px)。
 */
export function placeNewNodes(
  existingPositions: Record<string, SavedNodePosition>,
  newIds: string[],
  nodes: GraphNode[],
  edges: GraphEdge[]
): Record<string, SavedNodePosition> {
  const result: Record<string, SavedNodePosition> = { ...existingPositions };
  const nodeMap = new Map(nodes.map((n) => [n.id, n]));

  const neighborMap = new Map<string, Set<string>>();
  for (const e of edges) {
    if (!neighborMap.has(e.source_node_id)) neighborMap.set(e.source_node_id, new Set());
    if (!neighborMap.has(e.target_node_id)) neighborMap.set(e.target_node_id, new Set());
    neighborMap.get(e.source_node_id)!.add(e.target_node_id);
    neighborMap.get(e.target_node_id)!.add(e.source_node_id);
  }

  for (const newId of newIds) {
    const gn = nodeMap.get(newId);
    if (!gn) continue;
    const parentId = gn.parent_id ?? null;

    // 1. 查找同父容器内已有坐标的直接邻居
    const neighbors = Array.from(neighborMap.get(newId) || []).filter((nbId) => {
      const nbNode = nodeMap.get(nbId);
      return Boolean(result[nbId] && (nbNode?.parent_id ?? null) === parentId);
    });

    if (neighbors.length > 0) {
      let sumX = 0;
      let sumY = 0;
      for (const nbId of neighbors) {
        sumX += result[nbId].x;
        sumY += result[nbId].y;
      }
      result[newId] = {
        x: Math.round(sumX / neighbors.length + 248),
        y: Math.round(sumY / neighbors.length),
        parent_id: parentId,
        is_expanded: false,
        width: gn.type === 'MODULE' ? 220 : 260,
        height: gn.type === 'MODULE' ? 74 : 80,
      };
      continue;
    }

    // 2. 查找同父容器内已有坐标的兄弟节点
    const siblings = nodes.filter(
      (n) => n.id !== newId && (n.parent_id ?? null) === parentId && Boolean(result[n.id])
    );
    if (siblings.length > 0) {
      let maxRight = 0;
      let baseY = parentId ? 52 : 20;
      for (const sib of siblings) {
        const pos = result[sib.id];
        const w = pos.width ?? 260;
        if (pos.x + w > maxRight) {
          maxRight = pos.x + w;
          baseY = pos.y;
        }
      }
      result[newId] = {
        x: maxRight + 28,
        y: baseY,
        parent_id: parentId,
        is_expanded: false,
        width: gn.type === 'MODULE' ? 220 : 260,
        height: gn.type === 'MODULE' ? 74 : 80,
      };
    } else {
      result[newId] = {
        x: parentId ? 16 : 20,
        y: parentId ? 52 : 20,
        parent_id: parentId,
        is_expanded: false,
        width: gn.type === 'MODULE' ? 220 : 260,
        height: gn.type === 'MODULE' ? 74 : 80,
      };
    }
  }

  return result;
}

export interface LayoutGraphDiffResult {
  staleIds: string[];
  reparentedIds: string[];
  newIds: string[];
  driftCount: number;
  reconciledPositions: Record<string, SavedNodePosition>;
}

/**
 * 拓扑结构演进对比与增量位置调和：
 * - 自动清理已删除的失效节点 (staleIds)
 * - 检测跨容器迁移节点 (reparentedIds) 并使其旧相对坐标失效重算
 * - 为新增节点 (newIds) 与迁移节点执行质心/兄弟邻接增量放置
 */
export function diffLayoutAgainstGraph(
  savedPositions: Record<string, SavedNodePosition>,
  nodes: GraphNode[],
  edges: GraphEdge[]
): LayoutGraphDiffResult {
  const nodeMap = new Map(nodes.map((n) => [n.id, n]));
  const staleIds: string[] = [];
  const reparentedIds: string[] = [];
  const newIds: string[] = [];
  const validSaved: Record<string, SavedNodePosition> = {};

  for (const [id, pos] of Object.entries(savedPositions)) {
    const gn = nodeMap.get(id);
    if (!gn) {
      staleIds.push(id);
      continue;
    }
    const currParent = gn.parent_id ?? null;
    if (pos.parent_id !== undefined && (pos.parent_id ?? null) !== currParent) {
      reparentedIds.push(id);
      continue;
    }
    validSaved[id] = {
      ...pos,
      parent_id: currParent,
    };
  }

  for (const n of nodes) {
    if (!validSaved[n.id] && !reparentedIds.includes(n.id)) {
      newIds.push(n.id);
    }
  }

  const reconciledPositions = placeNewNodes(
    validSaved,
    [...reparentedIds, ...newIds],
    nodes,
    edges
  );

  return {
    staleIds,
    reparentedIds,
    newIds,
    driftCount: staleIds.length + reparentedIds.length + newIds.length,
    reconciledPositions,
  };
}

/**
 * 视口锚点补偿算法：
 * 当操作的焦点容器在画布坐标系中发生位移 (oldAnchor -> newAnchor) 时，
 * 反向补偿平移 React Flow 视口 (x, y)，确保焦点容器在屏幕坐标系中的像素位置 0 跳动。
 */
export function computeViewportCompensation(
  oldAnchor: { x: number; y: number },
  newAnchor: { x: number; y: number },
  viewport: SavedViewport
): SavedViewport {
  const dx = newAnchor.x - oldAnchor.x;
  const dy = newAnchor.y - oldAnchor.y;
  return {
    x: viewport.x - dx * viewport.zoom,
    y: viewport.y - dy * viewport.zoom,
    zoom: viewport.zoom,
  };
}
