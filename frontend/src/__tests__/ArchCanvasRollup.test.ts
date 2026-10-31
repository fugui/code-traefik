import { describe, it, expect } from 'vitest';
import type { GraphNode, GraphEdge, ExpandStateMap } from '../types/topology_graph';
import type { ConsistencyReportDTO } from '../types/traffic';
import { buildTreeIndex, findLCA, getFullAncestry } from '../components/ArchCanvas/model/hierarchy';
import { rep, rollupEdges, mergeStatus, STATUS_PRIORITY } from '../components/ArchCanvas/model/rollup';
import { detectCycles } from '../components/ArchCanvas/model/metrics';
import { processLayoutCoordinates } from '../components/ArchCanvas/layout/layoutProcessor';
import type { ElkNode } from 'elkjs';

describe('ArchCanvasRollup Model Suite', () => {
  // 模拟基础逻辑树节点: 2 个子系统 (sub_1, sub_2), 2 个功能组 (grp_11, grp_21), 4 个模块 (mod_101, mod_102, mod_201, mod_202)
  const mockNodes: GraphNode[] = [
    {
      id: 'sub_1',
      type: 'SUBSYSTEM',
      depth: 0,
      ancestors: [],
      name: '交易子系统',
      sort_order: 1,
    },
    {
      id: 'grp_11',
      type: 'FUNCTIONAL_GROUP',
      parent_id: 'sub_1',
      depth: 1,
      ancestors: ['sub_1'],
      name: '订单组',
      sort_order: 1,
    },
    {
      id: 'mod_101',
      type: 'MODULE',
      parent_id: 'grp_11',
      depth: 2,
      ancestors: ['sub_1', 'grp_11'],
      entity_id: 101,
      code: 'MOD_ORDER_CREATOR',
      name: '下单模块',
      sort_order: 1,
    },
    {
      id: 'mod_102',
      type: 'MODULE',
      parent_id: 'grp_11',
      depth: 2,
      ancestors: ['sub_1', 'grp_11'],
      entity_id: 102,
      code: 'MOD_ORDER_PAYER',
      name: '支付模块',
      sort_order: 2,
    },
    {
      id: 'sub_2',
      type: 'SUBSYSTEM',
      depth: 0,
      ancestors: [],
      name: '风控子系统',
      sort_order: 2,
    },
    {
      id: 'grp_21',
      type: 'FUNCTIONAL_GROUP',
      parent_id: 'sub_2',
      depth: 1,
      ancestors: ['sub_2'],
      name: '风控组',
      sort_order: 1,
    },
    {
      id: 'mod_201',
      type: 'MODULE',
      parent_id: 'grp_21',
      depth: 2,
      ancestors: ['sub_2', 'grp_21'],
      entity_id: 201,
      code: 'MOD_RISK_CHECKER',
      name: '风控检测模块',
      sort_order: 1,
    },
    {
      id: 'mod_202',
      type: 'MODULE',
      parent_id: 'grp_21',
      depth: 2,
      ancestors: ['sub_2', 'grp_21'],
      entity_id: 202,
      code: 'MOD_RISK_AUDITOR',
      name: '风控审计模块',
      sort_order: 2,
    },
  ];

  const nodeMap = new Map(mockNodes.map((n) => [n.id, n]));

  // 1. 测试代表节点查找与 LCA
  it('hierarchy: tests buildTreeIndex, getFullAncestry, and findLCA', () => {
    const tree = buildTreeIndex(mockNodes);
    expect(tree.rootNodes.length).toBe(2);
    expect(tree.childrenMap.get('sub_1')?.length).toBe(1);
    expect(tree.childrenMap.get('grp_11')?.length).toBe(2);

    expect(getFullAncestry('mod_101', nodeMap)).toEqual(['sub_1', 'grp_11', 'mod_101']);
    expect(getFullAncestry('unknown_node', nodeMap)).toEqual(['unknown_node']);

    // 同组内模块 LCA 应为 grp_11
    expect(findLCA(['mod_101', 'mod_102'], nodeMap)).toBe('grp_11');
    // 跨子系统模块无共同祖先，返回 null
    expect(findLCA(['mod_101', 'mod_201'], nodeMap)).toBeNull();
    // 单节点 LCA 为其自身
    expect(findLCA(['mod_101'], nodeMap)).toBe('mod_101');
    expect(findLCA([], nodeMap)).toBeNull();
  });

  // 2. 测试代表节点 rep() 逻辑
  it('rep: correctly resolves representative visible ancestor', () => {
    const expandState: ExpandStateMap = {};
    // L0 全部折叠态: 模块代表节点为顶层 sub_1
    expect(rep('mod_101', expandState, nodeMap)).toBe('sub_1');
    expect(rep('sub_1', expandState, nodeMap)).toBe('sub_1');

    // 展开 sub_1 后，其代表节点下钻到 grp_11
    expandState['sub_1'] = true;
    expect(rep('mod_101', expandState, nodeMap)).toBe('grp_11');

    // 再展开 grp_11 后，代表节点为模块自身
    expandState['grp_11'] = true;
    expect(rep('mod_101', expandState, nodeMap)).toBe('mod_101');
  });

  // 3. AC-M6-01.1: 顶层全部折叠与容器内部调用角标累计
  it('testRollup_AllCollapsedAndInternalBadge', () => {
    const expandState: ExpandStateMap = {}; // L0 折叠态

    const testEdges: GraphEdge[] = [
      // 跨子系统调用: mod_101 -> mod_201
      {
        id: 'edge_1',
        kind: 'CROSS_PROCESS',
        source_node_id: 'mod_101',
        target_node_id: 'mod_201',
        src_module_id: 101,
        dst_module_id: 201,
        services: [{ service_name: 'risk.v1.Check' }],
      },
      // 同子系统内部调用: mod_101 -> mod_102
      {
        id: 'edge_2',
        kind: 'INTRA_PROCESS',
        source_node_id: 'mod_101',
        target_node_id: 'mod_102',
        src_module_id: 101,
        dst_module_id: 102,
        services: [{ service_name: 'order.v1.Pay' }],
      },
    ];

    const result = rollupEdges(mockNodes, testEdges, expandState);

    // 1. 跨子系统边精确上卷至顶层 sub_1 -> sub_2
    expect(result.rollupEdges.length).toBe(1);
    const rollup = result.rollupEdges[0];
    expect(rollup.source).toBe('sub_1');
    expect(rollup.target).toBe('sub_2');
    expect(rollup.services.length).toBe(1);
    expect(rollup.services[0].service_name).toBe('risk.v1.Check');

    // 2. 同容器内部边不产生自环，精确累加至 sub_1 内部徽章计数
    expect(result.badgeCounts['sub_1'].internalEdgeCount).toBe(1);
    expect(result.badgeCounts['sub_1'].intraProcessCallCount).toBe(1);
    expect(result.badgeCounts['sub_2'].internalEdgeCount).toBe(0);
  });

  // 4. AC-M6-01.2: 非对称深度展开 (一侧到叶子，对端保持折叠)
  it('testRollup_AsymmetricDeepExpanded', () => {
    const expandState: ExpandStateMap = {
      sub_1: true,
      grp_11: true,
      // sub_2 与 grp_21 保持折叠
    };

    const testEdges: GraphEdge[] = [
      {
        id: 'edge_cross',
        kind: 'CROSS_PROCESS',
        source_node_id: 'mod_101',
        target_node_id: 'mod_201',
        src_module_id: 101,
        dst_module_id: 201,
        services: [{ service_name: 'risk.v1.Check' }],
      },
    ];

    const result = rollupEdges(mockNodes, testEdges, expandState);

    // 展开侧模块 mod_101 直接连向对端折叠的子系统容器 sub_2 (rep 命中最近可见祖先)
    expect(result.rollupEdges.length).toBe(1);
    expect(result.rollupEdges[0].source).toBe('mod_101');
    expect(result.rollupEdges[0].target).toBe('sub_2');
  });

  // 5. AC-M6-01.3: 六分类最差状态收敛、动态 VIOLATION 合成与 UNATTRIBUTED 挂载
  it('testOverlay_WorstStatusAndDynamicViolationUnattributed', () => {
    const expandState: ExpandStateMap = {
      sub_1: true,
      grp_11: true,
      sub_2: true,
      grp_21: true,
    };

    const testEdges: GraphEdge[] = [
      {
        id: 'edge_static',
        kind: 'CROSS_PROCESS',
        source_node_id: 'mod_101',
        target_node_id: 'mod_201',
        src_module_id: 101,
        dst_module_id: 201,
        services: [{ service_name: 'risk.v1.Check' }],
      },
    ];

    const mockReport: ConsistencyReportDTO = {
      session_id: '88',
      edges: [
        // 1. 静态边命中合规 VERIFIED
        {
          src_module_id: '101',
          dst_module_id: '201',
          service_name: 'risk.v1.Check',
          operation: 'CheckUser',
          category: 'VERIFIED',
          status: 'VERIFIED',
          call_count: 50,
        },
        // 2. 静态未声明的越权调用 VIOLATION (同一个汇总边 mod_101 -> mod_201)
        {
          src_module_id: '101',
          dst_module_id: '201',
          service_name: 'risk.v1.SecretOp',
          operation: 'DoSecret',
          category: 'VIOLATION',
          status: 'VIOLATION',
          call_count: 10,
        },
        // 3. 调用了未登记接口的 VIOLATION (dst_module_id = null)
        {
          src_module_id: '101',
          service_name: 'unknown.v1.ForeignSvc',
          operation: 'Query',
          category: 'VIOLATION',
          status: 'VIOLATION',
          call_count: 5,
        },
        // 4. 多模块共进程待定归属 UNATTRIBUTED
        {
          candidate_module_ids: ['101', '102'], // LCA 为 grp_11
          dst_module_id: '202',
          service_name: 'risk.v1.Audit',
          operation: 'Log',
          category: 'UNATTRIBUTED',
          status: 'UNATTRIBUTED',
          call_count: 3,
        },
        // 5. 跨子系统候选无 LCA 的 UNATTRIBUTED
        {
          candidate_module_ids: ['101', '201'], // 跨子系统无单一 LCA
          dst_module_id: '202',
          service_name: 'global.v1.Notify',
          operation: 'Send',
          category: 'UNATTRIBUTED',
          status: 'UNATTRIBUTED',
          call_count: 2,
        },
      ],
    };

    const result = rollupEdges(mockNodes, testEdges, expandState, mockReport, 'LOGICAL');

    // 1. 验证同汇总边 mod_101 -> mod_201 下，VERIFIED + VIOLATION 严格收敛为最差的 VIOLATION
    const crossEdge = result.rollupEdges.find(
      (e) => e.source === 'mod_101' && e.target === 'mod_201'
    );
    expect(crossEdge).toBeDefined();
    expect(crossEdge?.status).toBe('VIOLATION');
    expect(crossEdge?.totalCalls).toBe(60); // 50 + 10

    // 2. 验证动态未登记接口虚拟节点与连线
    const virtNode = result.dynamicNodes.find(
      (n) => n.id === 'VIRTUAL_UNREGISTERED_INTERFACE'
    );
    expect(virtNode).toBeDefined();
    expect(virtNode?.name).toBe('未登记接口');

    const unregEdge = result.rollupEdges.find(
      (e) => e.target === 'VIRTUAL_UNREGISTERED_INTERFACE'
    );
    expect(unregEdge).toBeDefined();
    expect(unregEdge?.status).toBe('VIOLATION');

    // 3. 验证 UNATTRIBUTED 在逻辑视图挂载至 LCA 节点 grp_11
    const unattrEdge = result.rollupEdges.find(
      (e) => e.source === 'grp_11' && e.target === 'mod_202'
    );
    expect(unattrEdge).toBeDefined();
    expect(unattrEdge?.status).toBe('UNATTRIBUTED');

    // 4. 验证跨子系统无 LCA 的待定边归入待定列表，未抛异常
    expect(result.unattributedPendingList.length).toBe(1);
    expect(result.unattributedPendingList[0].service_name).toBe('global.v1.Notify');
  });

  // 6. AC-M6-01.4: 状态优先级收敛函数独立单测
  it('mergeStatus: correctly computes priority ordering', () => {
    expect(STATUS_PRIORITY.VIOLATION).toBeGreaterThan(STATUS_PRIORITY.UNATTRIBUTED);
    expect(STATUS_PRIORITY.UNATTRIBUTED).toBeGreaterThan(STATUS_PRIORITY.UNCOVERED);
    expect(STATUS_PRIORITY.UNCOVERED).toBeGreaterThan(STATUS_PRIORITY.NOT_MONITORED);
    expect(STATUS_PRIORITY.NOT_MONITORED).toBeGreaterThan(STATUS_PRIORITY.NOT_OBSERVABLE);
    expect(STATUS_PRIORITY.NOT_OBSERVABLE).toBeGreaterThan(STATUS_PRIORITY.VERIFIED);

    expect(mergeStatus('VERIFIED', 'VIOLATION')).toBe('VIOLATION');
    expect(mergeStatus('UNCOVERED', 'UNATTRIBUTED')).toBe('UNATTRIBUTED');
    expect(mergeStatus('NOT_MONITORED', 'NOT_OBSERVABLE')).toBe('NOT_MONITORED');
    expect(mergeStatus('VERIFIED', undefined)).toBe('VERIFIED');
    expect(mergeStatus(undefined, undefined)).toBeUndefined();
  });

  // 7. AC-M6-01.5: Tarjan SCC 拓扑环路检测
  it('testTarjanCycleDetection: detects A -> B -> C -> A cycle while A -> D is uncycled', () => {
    const visibleNodes = ['A', 'B', 'C', 'D'];
    const rollupEdgesList = [
      {
        id: 'e_ab',
        source: 'A',
        target: 'B',
        kind: 'CROSS_PROCESS',
        services: [],
        rawEdges: [],
      },
      {
        id: 'e_bc',
        source: 'B',
        target: 'C',
        kind: 'CROSS_PROCESS',
        services: [],
        rawEdges: [],
      },
      {
        id: 'e_ca',
        source: 'C',
        target: 'A',
        kind: 'CROSS_PROCESS',
        services: [],
        rawEdges: [],
      },
      {
        id: 'e_ad',
        source: 'A',
        target: 'D',
        kind: 'CROSS_PROCESS',
        services: [],
        rawEdges: [],
      },
    ];

    const result = detectCycles(visibleNodes, rollupEdgesList);

    // 环内节点包含 A, B, C，不含 D
    expect(result.cycleNodeIds.has('A')).toBe(true);
    expect(result.cycleNodeIds.has('B')).toBe(true);
    expect(result.cycleNodeIds.has('C')).toBe(true);
    expect(result.cycleNodeIds.has('D')).toBe(false);

    // 环内 3 条有向边标记 isCycle = true
    const edgeMap = new Map(result.edges.map((e) => [e.id, e]));
    expect(edgeMap.get('e_ab')?.isCycle).toBe(true);
    expect(edgeMap.get('e_bc')?.isCycle).toBe(true);
    expect(edgeMap.get('e_ca')?.isCycle).toBe(true);

    // 分支 A -> D 必须为 false
    expect(edgeMap.get('e_ad')?.isCycle).toBe(false);
  });

  // 8. AC-M6-03 (P3): 父节点优先拓扑序、跨层折线拐点绝对坐标换算与手动拖拽退化
  it('testLayout_ParentFirstOrderAndCompoundBendPoints', () => {
    // 构造包含 offset 坐标的两级展开容器与跨组边
    const visibleNodes: GraphNode[] = [
      {
        id: 'child_mod_1',
        type: 'MODULE',
        parent_id: 'grp_1',
        depth: 2,
        ancestors: ['sub_1', 'grp_1'],
        name: '模块1',
        sort_order: 1,
      },
      {
        id: 'grp_1',
        type: 'FUNCTIONAL_GROUP',
        parent_id: 'sub_1',
        depth: 1,
        ancestors: ['sub_1'],
        name: '组1',
        sort_order: 1,
      },
      {
        id: 'sub_1',
        type: 'SUBSYSTEM',
        depth: 0,
        ancestors: [],
        name: '子系统1',
        sort_order: 1,
      },
      {
        id: 'child_mod_2',
        type: 'MODULE',
        parent_id: 'grp_1',
        depth: 2,
        ancestors: ['sub_1', 'grp_1'],
        name: '模块2',
        sort_order: 2,
      },
    ];

    // 模拟 ELK 输出树: sub_1 位于 (100, 50)，grp_1 位于 sub_1 内部 (20, 30)
    // 则 grp_1 的全局 absX = 120, absY = 80
    const mockElkTree: ElkNode = {
      id: 'root',
      children: [
        {
          id: 'sub_1',
          x: 100,
          y: 50,
          width: 500,
          height: 400,
          children: [
            {
              id: 'grp_1',
              x: 20,
              y: 30,
              width: 300,
              height: 200,
              children: [
                { id: 'child_mod_1', x: 10, y: 10, width: 100, height: 50 },
                { id: 'child_mod_2', x: 150, y: 10, width: 100, height: 50 },
              ],
            },
          ],
        },
      ],
      edges: [
        {
          id: 'rollup_child_mod_1_child_mod_2',
          sources: ['child_mod_1'],
          targets: ['child_mod_2'],
          sections: [
            {
              id: 'sec_1',
              startPoint: { x: 110, y: 35 },
              endPoint: { x: 150, y: 35 },
              // 局部拐点相对 grp_1 (LCA)
              bendPoints: [
                { x: 125, y: 35 },
                { x: 125, y: 60 },
              ],
            },
          ],
        },
      ],
    };

    const rollupEdgesList = [
      {
        id: 'rollup_child_mod_1_child_mod_2',
        source: 'child_mod_1',
        target: 'child_mod_2',
        kind: 'CROSS_PROCESS',
        services: [],
        rawEdges: [],
      },
    ];

    // 1. 未拖拽状态
    const resultNormal = processLayoutCoordinates(
      mockElkTree,
      visibleNodes,
      rollupEdgesList,
      new Set()
    );

    // 断言 1: 输出 nodes 数组严格满足每个 parentId 节点索引小于其所有子节点索引
    const nodeIndexMap = new Map<string, number>();
    resultNormal.nodes.forEach((n, idx) => {
      nodeIndexMap.set(n.id, idx);
    });

    for (const n of resultNormal.nodes) {
      if (n.parentId) {
        const parentIdx = nodeIndexMap.get(n.parentId);
        const childIdx = nodeIndexMap.get(n.id);
        expect(parentIdx).toBeDefined();
        expect(childIdx).toBeDefined();
        expect(parentIdx!).toBeLessThan(childIdx!);
      }
    }

    // 断言 2: 拐点坐标精确累加上所属 LCA (grp_1, absX=120, absY=80) 全局坐标
    const edgeNormal = resultNormal.edges[0];
    expect(edgeNormal.data?.isDraggedDegraded).toBe(false);
    expect(edgeNormal.data?.bendPoints).toBeDefined();
    expect(edgeNormal.data?.bendPoints?.length).toBe(2);
    // 125 + 120 = 245; 35 + 80 = 115
    expect(edgeNormal.data?.bendPoints?.[0]).toEqual({ x: 245, y: 115 });
    // 125 + 120 = 245; 60 + 80 = 140
    expect(edgeNormal.data?.bendPoints?.[1]).toEqual({ x: 245, y: 140 });

    // 2. 模拟关联节点 child_mod_1 被手动拖拽
    const draggedNodes = new Set(['child_mod_1']);
    const resultDragged = processLayoutCoordinates(
      mockElkTree,
      visibleNodes,
      rollupEdgesList,
      draggedNodes
    );

    // 断言 3: 边渲染模式自动切换为 isDraggedDegraded = true
    const edgeDragged = resultDragged.edges[0];
    expect(edgeDragged.data?.isDraggedDegraded).toBe(true);
  });
});
