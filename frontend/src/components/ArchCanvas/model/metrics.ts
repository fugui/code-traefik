import type { RollupEdgeModel } from '../../../types/topology_graph';

export interface CycleDetectionResult {
  edges: RollupEdgeModel[];
  cycleNodeIds: Set<string>;
}

/**
 * 基于 Tarjan 强连通分量 (SCC) 算法检测当前代表节点导出的拓扑环路
 * 处于大小 >= 2 的强连通分量内的有向边标记 isCycle = true
 */
export function detectCycles(
  visibleNodeIds: string[],
  rollupEdges: RollupEdgeModel[]
): CycleDetectionResult {
  const adj = new Map<string, string[]>();
  for (const id of visibleNodeIds) {
    adj.set(id, []);
  }

  for (const e of rollupEdges) {
    if (adj.has(e.source) && adj.has(e.target)) {
      adj.get(e.source)?.push(e.target);
    }
  }

  let index = 0;
  const indices = new Map<string, number>();
  const lowlinks = new Map<string, number>();
  const onStack = new Map<string, boolean>();
  const stack: string[] = [];
  const sccs: string[][] = [];

  function strongConnect(u: string) {
    indices.set(u, index);
    lowlinks.set(u, index);
    index++;
    stack.push(u);
    onStack.set(u, true);

    const neighbors = adj.get(u) || [];
    for (const v of neighbors) {
      if (!indices.has(v)) {
        strongConnect(v);
        const uLow = lowlinks.get(u) ?? 0;
        const vLow = lowlinks.get(v) ?? 0;
        lowlinks.set(u, Math.min(uLow, vLow));
      } else if (onStack.get(v)) {
        const uLow = lowlinks.get(u) ?? 0;
        const vIdx = indices.get(v) ?? 0;
        lowlinks.set(u, Math.min(uLow, vIdx));
      }
    }

    if (lowlinks.get(u) === indices.get(u)) {
      const scc: string[] = [];
      let w = '';
      do {
        w = stack.pop() ?? '';
        onStack.set(w, false);
        scc.push(w);
      } while (w !== u && stack.length > 0);
      sccs.push(scc);
    }
  }

  for (const id of visibleNodeIds) {
    if (!indices.has(id)) {
      strongConnect(id);
    }
  }

  const nodeToSccId = new Map<string, number>();
  const cycleNodeIds = new Set<string>();

  sccs.forEach((scc, sccIdx) => {
    if (scc.length >= 2) {
      for (const nodeId of scc) {
        nodeToSccId.set(nodeId, sccIdx);
        cycleNodeIds.add(nodeId);
      }
    }
  });

  const updatedEdges = rollupEdges.map((e) => {
    const scc1 = nodeToSccId.get(e.source);
    const scc2 = nodeToSccId.get(e.target);
    const inSameCycle = scc1 !== undefined && scc2 !== undefined && scc1 === scc2;
    return {
      ...e,
      isCycle: inSameCycle,
    };
  });

  return {
    edges: updatedEdges,
    cycleNodeIds,
  };
}

