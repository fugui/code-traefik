import { useState, useEffect, useRef, useCallback } from 'react';
import type { ElkNode } from 'elkjs';
import type {
  GraphNode,
  GraphEdge,
  ExpandStateMap,
  ContainerBadgeCounts,
} from '../../../types/topology_graph';
import type { ConsistencyReportDTO, ConsistencyEdge } from '../../../types/traffic';
import { rollupEdges, type RollupResult } from '../model/rollup';
import { detectCycles } from '../model/metrics';
import {
  processLayoutCoordinates,
  type ArchCanvasNode,
  type ArchCanvasEdge,
} from './layoutProcessor';

// 内联 Web Worker 引用 (Vite ?worker&inline 模式，微前端安全，无跨域脚本加载异常)
import ElkWorker from './elk.worker?worker&inline';

export interface UseElkLayoutOptions {
  nodes: GraphNode[];
  edges: GraphEdge[];
  expandState: ExpandStateMap;
  consistencyReport?: ConsistencyReportDTO | null;
  view: 'LOGICAL' | 'PHYSICAL';
  draggedNodeIds: Set<string>;
  onToggleExpand: (nodeId: string) => void;
}

export interface UseElkLayoutReturn {
  flowNodes: ArchCanvasNode[];
  flowEdges: ArchCanvasEdge[];
  isLayouting: boolean;
  badgeCounts: Record<string, ContainerBadgeCounts>;
  unattributedPendingList: ConsistencyEdge[];
  refetchLayout: () => void;
}

/**
 * 递归构建 ELK 复合图输入结构
 */
function buildElkGraph(
  visibleNodes: GraphNode[],
  edges: RollupResult['rollupEdges'],
  expandState: ExpandStateMap
): ElkNode {
  const nodeMap = new Map(visibleNodes.map((n) => [n.id, n]));
  const childMap = new Map<string, GraphNode[]>();

  for (const n of visibleNodes) {
    if (n.parent_id && nodeMap.has(n.parent_id)) {
      if (!childMap.has(n.parent_id)) {
        childMap.set(n.parent_id, []);
      }
      childMap.get(n.parent_id)!.push(n);
    }
  }

  function convertNode(gn: GraphNode): ElkNode {
    const isExpanded = Boolean(expandState[gn.id]);
    const children = isExpanded ? childMap.get(gn.id) || [] : [];

    const isLeaf = children.length === 0;
    let width = 220;
    let height = 74;

    if (
      gn.type === 'SUBSYSTEM' ||
      gn.type === 'FUNCTIONAL_GROUP' ||
      gn.type === 'PROCESS_GROUP' ||
      gn.type === 'PROCESS' ||
      gn.type === 'UNDEPLOYED_CONTAINER' ||
      gn.type === 'EXTERNAL_CONTAINER'
    ) {
      if (!isExpanded) {
        width = 260;
        height = 80;
      }
    } else if (gn.type === 'EXTERNAL_SYSTEM' || gn.type === 'EXTERNAL_ENDPOINT' || gn.type === 'UNREGISTERED_INTERFACE') {
      width = 190;
      height = 60;
    }

    const elkNode: ElkNode = {
      id: gn.id,
      layoutOptions: {
        'elk.padding': '[top=48,left=16,bottom=16,right=16]',
        'elk.spacing.nodeNode': '36',
      },
    };

    if (isLeaf) {
      elkNode.width = width;
      elkNode.height = height;
    }

    if (children.length > 0) {
      elkNode.children = children.map(convertNode);
    }

    return elkNode;
  }

  const rootNodes = visibleNodes.filter((n) => !n.parent_id || !nodeMap.has(n.parent_id));
  const visibleIdSet = new Set(visibleNodes.map((n) => n.id));
  const validEdges = edges.filter((e) => visibleIdSet.has(e.source) && visibleIdSet.has(e.target));

  return {
    id: 'root',
    layoutOptions: {
      'elk.algorithm': 'layered',
      'elk.direction': 'RIGHT',
      'elk.spacing.nodeNode': '40',
      'elk.layered.spacing.nodeNodeBetweenLayers': '60',
      'elk.edgeRouting': 'ORTHOGONAL',
      'elk.hierarchyHandling': 'INCLUDE_CHILDREN',
      'elk.padding': '[top=20,left=20,bottom=20,right=20]',
    },
    children: rootNodes.map(convertNode),
    edges: validEdges.map((e) => ({
      id: e.id,
      sources: [e.source],
      targets: [e.target],
    })),
  };
}

export function useElkLayout({
  nodes,
  edges,
  expandState,
  consistencyReport,
  view,
  draggedNodeIds,
  onToggleExpand,
}: UseElkLayoutOptions): UseElkLayoutReturn {
  const [flowNodes, setFlowNodes] = useState<ArchCanvasNode[]>([]);
  const [flowEdges, setFlowEdges] = useState<ArchCanvasEdge[]>([]);
  const [isLayouting, setIsLayouting] = useState<boolean>(false);
  const [badgeCounts, setBadgeCounts] = useState<Record<string, ContainerBadgeCounts>>({});
  const [unattributedPendingList, setUnattributedPendingList] = useState<ConsistencyEdge[]>([]);

  const workerRef = useRef<Worker | null>(null);
  const layoutReqIdRef = useRef<number>(0);

  // 初始化 Web Worker
  useEffect(() => {
    if (typeof Worker !== 'undefined') {
      try {
        const worker = new ElkWorker();
        workerRef.current = worker;
      } catch (err: unknown) {
        // Fallback gracefully
        console.warn('ELK Inline Worker 创建失败，将回退同步模式:', err);
      }
    }
    return () => {
      workerRef.current?.terminate();
      workerRef.current = null;
    };
  }, []);

  const runLayout = useCallback(async () => {
    if (nodes.length === 0) {
      setFlowNodes([]);
      setFlowEdges([]);
      return;
    }

    setIsLayouting(true);

    // 1. 计算纯前端汇总边与动态节点
    const rollupRes = rollupEdges(nodes, edges, expandState, consistencyReport, view);
    setBadgeCounts(rollupRes.badgeCounts);
    setUnattributedPendingList(rollupRes.unattributedPendingList);

    // 2. 筛选在当前展开状态下可见的节点
    const allGraphNodes = [...nodes, ...rollupRes.dynamicNodes];
    const visibleGraphNodes = allGraphNodes.filter((gn) => {
      for (const ancId of gn.ancestors) {
        if (!expandState[ancId]) {
          return false;
        }
      }
      return true;
    });

    // 3. Tarjan SCC 环路检测
    const visibleNodeIds = visibleGraphNodes.map((n) => n.id);
    const cycleRes = detectCycles(visibleNodeIds, rollupRes.rollupEdges);

    // 4. 构建 ELK 复合输入图
    const elkInputGraph = buildElkGraph(visibleGraphNodes, cycleRes.edges, expandState);

    const currentReqId = ++layoutReqIdRef.current;

    const applyLayoutResult = (layoutedTree: ElkNode) => {
      if (currentReqId !== layoutReqIdRef.current) return;

      const processed = processLayoutCoordinates(
        layoutedTree,
        visibleGraphNodes,
        cycleRes.edges,
        draggedNodeIds,
        expandState,
        rollupRes.badgeCounts,
        onToggleExpand
      );

      setFlowNodes(processed.nodes);
      setFlowEdges(processed.edges);
      setIsLayouting(false);
    };

    // 5. Worker 执行排版，或回退至同步/动态导入
    if (workerRef.current) {
      const handleWorkerMessage = (e: MessageEvent<{ id: string; layouted?: ElkNode; error?: string }>) => {
        if (e.data.id === String(currentReqId)) {
          workerRef.current?.removeEventListener('message', handleWorkerMessage);
          if (e.data.layouted) {
            applyLayoutResult(e.data.layouted);
          } else {
            console.error('ELK Worker 排版异常:', e.data.error);
            setIsLayouting(false);
          }
        }
      };

      workerRef.current.addEventListener('message', handleWorkerMessage);
      workerRef.current.postMessage({
        id: String(currentReqId),
        graph: elkInputGraph,
      });
    } else {
      // Node.js 或降级环境：动态导入 ELK 同步计算
      try {
        const { default: ELK } = await import('elkjs/lib/elk.bundled.js');
        const elk = new ELK();
        const layouted = await elk.layout(elkInputGraph);
        applyLayoutResult(layouted);
      } catch (err: unknown) {
        console.error('同步 ELK 排版异常:', err);
        setIsLayouting(false);
      }
    }
  }, [nodes, edges, expandState, consistencyReport, view, draggedNodeIds, onToggleExpand]);

  useEffect(() => {
    void runLayout();
  }, [runLayout]);

  return {
    flowNodes,
    flowEdges,
    isLayouting,
    badgeCounts,
    unattributedPendingList,
    refetchLayout: runLayout,
  };
}

