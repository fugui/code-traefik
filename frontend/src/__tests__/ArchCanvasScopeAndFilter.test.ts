import { describe, it, expect } from 'vitest';
import type { GraphNode, GraphEdge, ExpandStateMap } from '../types/topology_graph';
import type { ConsistencyReportDTO } from '../types/traffic';
import { synthesizeOverlay } from '../components/ArchCanvas/model/overlay';
import { aggregateRollup, rollupEdges } from '../components/ArchCanvas/model/rollup';
import {
  scopeAndFilter,
  estimateVisibleNodeCount,
  pruneIsolated,
} from '../components/ArchCanvas/model/subgraph';
import {
  foldHubEdges,
  computeSpotlightSet,
  HUB_FANOUT_THRESHOLD,
  HUB_TOP_K_EDGES,
} from '../components/ArchCanvas/model/noiseReduction';

describe('M19-P2 & M19-P3: ArchCanvasScopeAndFilter Suite', () => {
  const baseNodes: GraphNode[] = [
    { id: 'sub_1', type: 'SUBSYSTEM', depth: 0, ancestors: [], name: '交易子系统', sort_order: 1, meta: { owner_id: 10 } },
    { id: 'grp_11', type: 'FUNCTIONAL_GROUP', parent_id: 'sub_1', depth: 1, ancestors: ['sub_1'], name: '订单组', sort_order: 1 },
    { id: 'mod_101', type: 'MODULE', parent_id: 'grp_11', depth: 2, ancestors: ['sub_1', 'grp_11'], entity_id: 101, code: 'MOD_101', name: '下单服务', sort_order: 1, meta: { owner_id: 10 } },
    { id: 'mod_102', type: 'MODULE', parent_id: 'grp_11', depth: 2, ancestors: ['sub_1', 'grp_11'], entity_id: 102, code: 'MOD_102', name: '支付服务', sort_order: 2, meta: { owner_id: 20 } },
    { id: 'sub_2', type: 'SUBSYSTEM', depth: 0, ancestors: [], name: '风控子系统', sort_order: 2, meta: { owner_id: 20 } },
    { id: 'grp_21', type: 'FUNCTIONAL_GROUP', parent_id: 'sub_2', depth: 1, ancestors: ['sub_2'], name: '反欺诈组', sort_order: 1 },
    { id: 'mod_201', type: 'MODULE', parent_id: 'grp_21', depth: 2, ancestors: ['sub_2', 'grp_21'], entity_id: 201, code: 'MOD_201', name: '规则引擎', sort_order: 1, meta: { owner_id: 20 } },
    { id: 'mod_202', type: 'MODULE', parent_id: 'grp_21', depth: 2, ancestors: ['sub_2', 'grp_21'], entity_id: 202, code: 'MOD_202', name: '名单服务', sort_order: 2, meta: { owner_id: 20 } },
    { id: 'mod_203', type: 'MODULE', parent_id: 'grp_21', depth: 2, ancestors: ['sub_2', 'grp_21'], entity_id: 203, code: 'MOD_203', name: '画像服务', sort_order: 3, meta: { owner_id: 30 } },
    { id: 'sub_3', type: 'SUBSYSTEM', depth: 0, ancestors: [], name: '清结算子系统', sort_order: 3, meta: { owner_id: 30 } },
    { id: 'grp_31', type: 'FUNCTIONAL_GROUP', parent_id: 'sub_3', depth: 1, ancestors: ['sub_3'], name: '对账组', sort_order: 1 },
    { id: 'mod_301', type: 'MODULE', parent_id: 'grp_31', depth: 2, ancestors: ['sub_3', 'grp_31'], entity_id: 301, code: 'MOD_301', name: '孤立对账模块', sort_order: 1, meta: { owner_id: 30 } },
  ];

  // 1. testScopeAndFilter_FocusSubsystem_GeneratesExternalContextCards
  it('testScopeAndFilter_FocusSubsystem_GeneratesExternalContextCards', () => {
    const edges: GraphEdge[] = [
      { id: 'e1', kind: 'CROSS_PROCESS', source_node_id: 'mod_101', target_node_id: 'mod_201', src_module_id: 101, dst_module_id: 201, services: [{ service_name: 's1' }] },
      { id: 'e2', kind: 'CROSS_PROCESS', source_node_id: 'mod_101', target_node_id: 'mod_202', src_module_id: 101, dst_module_id: 202, services: [{ service_name: 's2' }] },
      { id: 'e3', kind: 'CROSS_PROCESS', source_node_id: 'mod_102', target_node_id: 'mod_202', src_module_id: 102, dst_module_id: 202, services: [{ service_name: 's3' }] },
      { id: 'e4', kind: 'CROSS_PROCESS', source_node_id: 'mod_102', target_node_id: 'mod_203', src_module_id: 102, dst_module_id: 203, services: [{ service_name: 's4' }] },
    ];
    const edgeStatusMap = new Map<string, 'VIOLATION' | 'VERIFIED'>([
      ['e1', 'VERIFIED'],
      ['e2', 'VIOLATION'],
      ['e3', 'VERIFIED'],
      ['e4', 'VIOLATION'],
    ]);
    const expandState: ExpandStateMap = { sub_1: true, grp_11: true };

    const res = scopeAndFilter(baseNodes, edges, expandState, {
      focusScope: 'sub_1',
      edgeStatusMap,
    });

    // 仅保留 sub_1 子树节点 + 1 张下游外部上下文摘要卡
    const nonContextNodes = res.scopedNodes.filter((n) => n.type !== 'EXTERNAL_CONTEXT');
    expect(nonContextNodes.map((n) => n.id).sort()).toEqual(['grp_11', 'mod_101', 'mod_102', 'sub_1']);

    const extCard = res.scopedNodes.find((n) => n.id === 'ext_ctx_DOWNSTREAM_sub_2');
    expect(extCard).toBeDefined();
    expect(extCard?.type).toBe('EXTERNAL_CONTEXT');
    expect(extCard?.meta?.hidden_module_count).toBe(3);
    expect(extCard?.meta?.cross_edge_count).toBe(4);
    expect(extCard?.meta?.violation_count).toBe(2);
    expect(extCard?.meta?.peer_root_id).toBe('sub_2');
    expect(extCard?.meta?.direction).toBe('DOWNSTREAM');

    // 所有跨边界边终点均重写为 ext_ctx_DOWNSTREAM_sub_2
    expect(res.scopedEdges).toHaveLength(4);
    for (const e of res.scopedEdges) {
      expect(e.target_node_id).toBe('ext_ctx_DOWNSTREAM_sub_2');
    }

    // 同时验证上游外部调用进入聚焦容器 sub_1 时生成 ext_ctx_UPSTREAM_sub_2
    const upstreamEdges: GraphEdge[] = [
      { id: 'eu1', kind: 'CROSS_PROCESS', source_node_id: 'mod_201', target_node_id: 'mod_101', services: [{ service_name: 'u1' }] },
      { id: 'eu2', kind: 'CROSS_PROCESS', source_node_id: 'mod_202', target_node_id: 'mod_102', services: [{ service_name: 'u2' }] },
      { id: 'ein', kind: 'INTRA_PROCESS', source_node_id: 'mod_101', target_node_id: 'mod_102', services: [{ service_name: 'in1' }] },
    ];
    const upFocusRes = scopeAndFilter(baseNodes, upstreamEdges, expandState, {
      focusScope: 'sub_1',
      edgeStatusMap: new Map([['eu1', 'VIOLATION']]),
    });
    const upCard = upFocusRes.scopedNodes.find((n) => n.id === 'ext_ctx_UPSTREAM_sub_2');
    expect(upCard).toBeDefined();
    expect(upCard?.meta?.direction).toBe('UPSTREAM');
    expect(upCard?.meta?.hidden_module_count).toBe(2);
    expect(upCard?.meta?.cross_edge_count).toBe(2);
    expect(upCard?.meta?.violation_count).toBe(1);
  });

  // 2. testScopeAndFilter_DirectedNeighborhood_1Hop_UpstreamDownstreamBoth
  it('testScopeAndFilter_DirectedNeighborhood_1Hop_UpstreamDownstreamBoth', () => {
    const chainNodes: GraphNode[] = [
      { id: 'sub_a', type: 'SUBSYSTEM', depth: 0, ancestors: [], name: 'SubA', sort_order: 1 },
      { id: 'A', type: 'MODULE', parent_id: 'sub_a', depth: 1, ancestors: ['sub_a'], name: 'A', sort_order: 1 },
      { id: 'sub_b', type: 'SUBSYSTEM', depth: 0, ancestors: [], name: 'SubB', sort_order: 2 },
      { id: 'B', type: 'MODULE', parent_id: 'sub_b', depth: 1, ancestors: ['sub_b'], name: 'B', sort_order: 1 },
      { id: 'sub_c', type: 'SUBSYSTEM', depth: 0, ancestors: [], name: 'SubC', sort_order: 3 },
      { id: 'C', type: 'MODULE', parent_id: 'sub_c', depth: 1, ancestors: ['sub_c'], name: 'C', sort_order: 1 },
      { id: 'sub_d', type: 'SUBSYSTEM', depth: 0, ancestors: [], name: 'SubD', sort_order: 4 },
      { id: 'D', type: 'MODULE', parent_id: 'sub_d', depth: 1, ancestors: ['sub_d'], name: 'D', sort_order: 1 },
      { id: 'sub_e', type: 'SUBSYSTEM', depth: 0, ancestors: [], name: 'SubE', sort_order: 5 },
      { id: 'E', type: 'MODULE', parent_id: 'sub_e', depth: 1, ancestors: ['sub_e'], name: 'E', sort_order: 1 },
    ];
    const chainEdges: GraphEdge[] = [
      { id: 'ab', kind: 'CROSS_PROCESS', source_node_id: 'A', target_node_id: 'B', services: [] },
      { id: 'bc', kind: 'CROSS_PROCESS', source_node_id: 'B', target_node_id: 'C', services: [] },
      { id: 'cd', kind: 'CROSS_PROCESS', source_node_id: 'C', target_node_id: 'D', services: [] },
      { id: 'de', kind: 'CROSS_PROCESS', source_node_id: 'D', target_node_id: 'E', services: [] },
    ];
    const expandAll: ExpandStateMap = { sub_a: true, sub_b: true, sub_c: true, sub_d: true, sub_e: true };

    const upRes = scopeAndFilter(chainNodes, chainEdges, expandAll, {
      seedNodeId: 'C',
      neighborhoodHop: 1,
      neighborhoodDirection: 'UPSTREAM',
    });
    expect(upRes.scopedNodes.map((n) => n.id).sort()).toEqual(['B', 'C', 'sub_b', 'sub_c']);

    const downRes = scopeAndFilter(chainNodes, chainEdges, expandAll, {
      seedNodeId: 'C',
      neighborhoodHop: 1,
      neighborhoodDirection: 'DOWNSTREAM',
    });
    expect(downRes.scopedNodes.map((n) => n.id).sort()).toEqual(['C', 'D', 'sub_c', 'sub_d']);

    const bothRes = scopeAndFilter(chainNodes, chainEdges, expandAll, {
      seedNodeId: 'C',
      neighborhoodHop: 1,
      neighborhoodDirection: 'BOTH',
    });
    expect(bothRes.scopedNodes.map((n) => n.id).sort()).toEqual(['B', 'C', 'D', 'sub_b', 'sub_c', 'sub_d']);
  });

  // 3. testScopeAndFilter_DirectedNeighborhood_2Hop_BoundaryAndCycle
  it('testScopeAndFilter_DirectedNeighborhood_2Hop_BoundaryAndCycle', () => {
    const cycleNodes: GraphNode[] = [
      { id: 'sub_1', type: 'SUBSYSTEM', depth: 0, ancestors: [], name: 'S1', sort_order: 1 },
      { id: 'A', type: 'MODULE', parent_id: 'sub_1', depth: 1, ancestors: ['sub_1'], name: 'A', sort_order: 1 },
      { id: 'B', type: 'MODULE', parent_id: 'sub_1', depth: 1, ancestors: ['sub_1'], name: 'B', sort_order: 2 },
      { id: 'C', type: 'MODULE', parent_id: 'sub_1', depth: 1, ancestors: ['sub_1'], name: 'C', sort_order: 3 },
      { id: 'sub_z', type: 'SUBSYSTEM', depth: 0, ancestors: [], name: 'SZ', sort_order: 2 },
      { id: 'Z', type: 'MODULE', parent_id: 'sub_z', depth: 1, ancestors: ['sub_z'], name: 'Z', sort_order: 1 },
    ];
    const cycleEdges: GraphEdge[] = [
      { id: 'ab', kind: 'CROSS_PROCESS', source_node_id: 'A', target_node_id: 'B', services: [] },
      { id: 'bc', kind: 'CROSS_PROCESS', source_node_id: 'B', target_node_id: 'C', services: [] },
      { id: 'ca', kind: 'CROSS_PROCESS', source_node_id: 'C', target_node_id: 'A', services: [] },
    ];
    const res = scopeAndFilter(cycleNodes, cycleEdges, { sub_1: true, sub_z: true }, {
      seedNodeId: 'A',
      neighborhoodHop: 2,
      neighborhoodDirection: 'DOWNSTREAM',
    });
    expect(res.scopedNodes.map((n) => n.id).sort()).toEqual(['A', 'B', 'C', 'sub_1']);
    expect(res.scopedNodes.some((n) => n.id === 'Z' || n.id === 'sub_z')).toBe(false);
  });

  // 4. testScopeAndFilter_OwnerAndConsistencyFilter_PreservesAncestors
  it('testScopeAndFilter_OwnerAndConsistencyFilter_PreservesAncestors', () => {
    const edges: GraphEdge[] = [
      { id: 'e_ok', kind: 'CROSS_PROCESS', source_node_id: 'mod_101', target_node_id: 'mod_102', services: [] },
      { id: 'e_viol', kind: 'CROSS_PROCESS', source_node_id: 'mod_101', target_node_id: 'mod_201', services: [] },
    ];
    const edgeStatusMap = new Map<string, 'VERIFIED' | 'VIOLATION'>([
      ['e_ok', 'VERIFIED'],
      ['e_viol', 'VIOLATION'],
    ]);
    const expandAll: ExpandStateMap = { sub_1: true, grp_11: true, sub_2: true, grp_21: true, sub_3: true, grp_31: true };

    // 按 owner_id=10 过滤
    const byOwner = scopeAndFilter(baseNodes, edges, expandAll, {
      ownerId: 10,
      edgeStatusMap,
    });
    expect(byOwner.scopedNodes.map((n) => n.id).sort()).toEqual(['grp_11', 'mod_101', 'sub_1']);

    // 按 consistency_only=true 过滤
    const byConsistency = scopeAndFilter(baseNodes, edges, expandAll, {
      consistencyOnly: true,
      edgeStatusMap,
    });
    expect(byConsistency.scopedNodes.map((n) => n.id).sort()).toEqual([
      'grp_11',
      'grp_21',
      'mod_101',
      'mod_201',
      'sub_1',
      'sub_2',
    ]);
  });

  // 5. testPruneIsolated_HidesZeroDegreeLeaves_KeepsSearchMatches
  it('testPruneIsolated_HidesZeroDegreeLeaves_KeepsSearchMatches', () => {
    const edges: GraphEdge[] = [
      { id: 'e1', kind: 'CROSS_PROCESS', source_node_id: 'mod_101', target_node_id: 'mod_201', services: [] },
    ];

    // 无搜索词时，mod_102、mod_202、mod_203 以及整个孤立的 sub_3/grp_31/mod_301 被隐藏
    const pruned = pruneIsolated(baseNodes, edges, '');
    const prunedIds = new Set(pruned.map((n) => n.id));
    expect(prunedIds.has('mod_101')).toBe(true);
    expect(prunedIds.has('mod_201')).toBe(true);
    expect(prunedIds.has('sub_1')).toBe(true);
    expect(prunedIds.has('grp_11')).toBe(true);
    expect(prunedIds.has('mod_301')).toBe(false);
    expect(prunedIds.has('grp_31')).toBe(false);
    expect(prunedIds.has('sub_3')).toBe(false);

    // 当孤立节点 mod_301 命中 searchKeyword 时，mod_301 及其祖先 sub_3 / grp_31 强制保留
    const prunedWithSearch = pruneIsolated(baseNodes, edges, '孤立对账');
    const searchIds = new Set(prunedWithSearch.map((n) => n.id));
    expect(searchIds.has('mod_301')).toBe(true);
    expect(searchIds.has('grp_31')).toBe(true);
    expect(searchIds.has('sub_3')).toBe(true);
  });

  // 6. testEstimateVisibleNodeCount_MatchesActualVisibleNodes_AcrossAllPaths
  it('testEstimateVisibleNodeCount_MatchesActualVisibleNodes_AcrossAllPaths', () => {
    const edges: GraphEdge[] = [
      { id: 'e1', kind: 'CROSS_PROCESS', source_node_id: 'mod_101', target_node_id: 'mod_201', services: [] },
    ];

    const scenarios: Array<{
      name: string;
      expand: ExpandStateMap;
      focusScope: string;
      hideIsolated?: boolean;
      searchKeyword?: string;
    }> = [
      { name: '单节点展开', expand: { sub_1: true }, focusScope: 'GLOBAL' },
      { name: '层级滑杆跳变全展', expand: { sub_1: true, grp_11: true, sub_2: true, grp_21: true, sub_3: true, grp_31: true }, focusScope: 'GLOBAL' },
      { name: '搜索自动展开+隐藏孤立', expand: { sub_1: true, grp_11: true, sub_3: true, grp_31: true }, focusScope: 'GLOBAL', hideIsolated: true, searchKeyword: '孤立' },
      { name: '聚焦子系统后退出聚焦', expand: { sub_1: true, grp_11: true }, focusScope: 'sub_1' },
      { name: '书签恢复态', expand: { sub_2: true, grp_21: true }, focusScope: 'GLOBAL', hideIsolated: true },
    ];

    for (const sc of scenarios) {
      const estimated = estimateVisibleNodeCount(baseNodes, edges, sc.expand, {
        focusScope: sc.focusScope,
        hideIsolated: sc.hideIsolated,
        searchKeyword: sc.searchKeyword,
      });
      const actual = scopeAndFilter(baseNodes, edges, sc.expand, {
        focusScope: sc.focusScope,
        hideIsolated: sc.hideIsolated,
        searchKeyword: sc.searchKeyword,
      }).visibleIds.size;
      expect(estimated).toBe(actual);
    }
  });

  // 7. testFoldHubEdges_Threshold8_RetainsViolationsAndTop3_ExpandsOnClick
  it('testFoldHubEdges_Threshold8_RetainsViolationsAndTop3_ExpandsOnClick', () => {
    expect(HUB_FANOUT_THRESHOLD).toBe(8);
    expect(HUB_TOP_K_EDGES).toBe(3);

    // 构造 hub 节点 mod_hub 向 10 个目标节点发起调用 (其中 2 条 VIOLATION, 8 条正常边)
    const visibleNodes: GraphNode[] = [
      { id: 'mod_hub', type: 'MODULE', depth: 0, ancestors: [], name: 'Hub', sort_order: 0 },
    ];
    const rollupEdgesList = [];
    for (let i = 1; i <= 10; i++) {
      const targetId = `mod_t_${String(i).padStart(2, '0')}`;
      visibleNodes.push({ id: targetId, type: 'MODULE', depth: 0, ancestors: [], name: targetId, sort_order: i });
      const isViolation = i === 9 || i === 10;
      rollupEdgesList.push({
        id: `rollup_mod_hub_${targetId}`,
        source: 'mod_hub',
        target: targetId,
        kind: 'CROSS_PROCESS',
        services: Array.from({ length: i % 3 + 1 }, (_, k) => ({ service_name: `svc_${i}_${k}` })),
        rawEdges: [],
        status: isViolation ? ('VIOLATION' as const) : ('VERIFIED' as const),
        totalCalls: i * 10, // mod_t_08 (80), mod_t_07 (70), mod_t_06 (60) 为正常边 Top-3
      });
    }

    const folded = foldHubEdges(rollupEdgesList, visibleNodes, {
      foldHubEdges: true,
      expandedHubNodeIds: new Set(),
    });

    // 2 条 VIOLATION + 3 条 Top-3 正常边 + 1 条 HUB_SUMMARY 聚合边 = 6 条
    expect(folded).toHaveLength(6);
    const violationEdges = folded.filter((e) => e.status === 'VIOLATION');
    expect(violationEdges).toHaveLength(2);

    const summaryEdge = folded.find((e) => e.isHubSummary);
    expect(summaryEdge).toBeDefined();
    expect(summaryEdge?.foldedPeerCount).toBe(5); // 8 - 3 = 5

    const directNormalTargets = folded
      .filter((e) => !e.isHubSummary && e.status !== 'VIOLATION')
      .map((e) => e.target)
      .sort();
    expect(directNormalTargets).toEqual(['mod_t_06', 'mod_t_07', 'mod_t_08']);

    // 点击展开该 Hub 节点后，全部 10 条直连边恢复
    const expanded = foldHubEdges(rollupEdgesList, visibleNodes, {
      foldHubEdges: true,
      expandedHubNodeIds: new Set(['mod_hub']),
    });
    expect(expanded).toHaveLength(10);
    expect(expanded.some((e) => e.isHubSummary)).toBe(false);

    // 验证入向高扇入 (Inbound > 8) 同样触发 HUB_SUMMARY 折叠，以及 foldHubEdges=false 直通
    const inboundEdges = rollupEdgesList.map((e) => ({
      ...e,
      id: `in_${e.id}`,
      source: e.target,
      target: 'mod_hub',
    }));
    const inboundFolded = foldHubEdges(inboundEdges, visibleNodes, {
      foldHubEdges: true,
      expandedHubNodeIds: new Set(),
    });
    expect(inboundFolded).toHaveLength(6);
    expect(inboundFolded.find((e) => e.isHubSummary)?.hubDirection).toBe('INBOUND');

    const disabledFold = foldHubEdges(inboundEdges, visibleNodes, { foldHubEdges: false });
    expect(disabledFold).toHaveLength(10);
  });

  // 8. testComputeSpotlightSet_HoverAndSelect_IncludesAncestors
  it('testComputeSpotlightSet_HoverAndSelect_IncludesAncestors', () => {
    const visibleNodes = baseNodes.filter((n) => ['sub_1', 'grp_11', 'mod_101', 'mod_102', 'sub_2', 'grp_21', 'mod_201'].includes(n.id));
    const edges = [
      {
        id: 'rollup_mod_101_mod_201',
        source: 'mod_101',
        target: 'mod_201',
        kind: 'CROSS_PROCESS',
        services: [],
        rawEdges: [],
      },
    ];

    const spot = computeSpotlightSet('mod_101', visibleNodes, edges);
    expect(spot.active).toBe(true);
    expect(spot.highlightedNodeIds.has('mod_101')).toBe(true);
    expect(spot.highlightedNodeIds.has('mod_201')).toBe(true);
    // 两端节点的可见祖先容器必须包含在内，防止父容器被误压暗
    expect(spot.highlightedNodeIds.has('grp_11')).toBe(true);
    expect(spot.highlightedNodeIds.has('sub_1')).toBe(true);
    expect(spot.highlightedNodeIds.has('grp_21')).toBe(true);
    expect(spot.highlightedNodeIds.has('sub_2')).toBe(true);
    // 无关同组兄弟节点 mod_102 不在聚光灯内
    expect(spot.highlightedNodeIds.has('mod_102')).toBe(false);
    expect(spot.highlightedEdgeIds.has('rollup_mod_101_mod_201')).toBe(true);

    // 验证连线自身被选中/Hover 与空 activeId 场景
    const edgeSpot = computeSpotlightSet('rollup_mod_101_mod_201', visibleNodes, edges);
    expect(edgeSpot.active).toBe(true);
    expect(edgeSpot.highlightedEdgeIds.has('rollup_mod_101_mod_201')).toBe(true);
    expect(edgeSpot.highlightedNodeIds.has('sub_1')).toBe(true);
    expect(edgeSpot.highlightedNodeIds.has('sub_2')).toBe(true);

    expect(computeSpotlightSet(null, visibleNodes, edges).active).toBe(false);
    expect(computeSpotlightSet('non_existent', visibleNodes, edges).active).toBe(false);
  });

  // 9. testSynthesizeOverlay_IndexedStaticMatch_AndDynamicViolationEdges
  it('testSynthesizeOverlay_IndexedStaticMatch_AndDynamicViolationEdges', () => {
    const staticEdges: GraphEdge[] = [
      {
        id: 'static_101_201',
        kind: 'CROSS_PROCESS',
        source_node_id: 'mod_101',
        target_node_id: 'mod_201',
        src_module_id: 101,
        dst_module_id: 201,
        services: [{ service_name: 'risk.v1.RuleService' }],
      },
    ];
    const report: ConsistencyReportDTO = {
      session_id: '99',
      edges: [
        {
          src_module_id: 101,
          dst_module_id: 201,
          service_name: 'risk.v1.RuleService',
          operation: 'Eval',
          status: 'VERIFIED',
          category: 'VERIFIED',
          call_count: 42,
          sample_traffic_record_id: 777,
        },
        {
          src_module_id: 101,
          service_name: 'rogue.v1.External',
          operation: 'Steal',
          status: 'VIOLATION',
          category: 'VIOLATION',
          call_count: 3,
        },
        {
          candidate_module_ids: [101, 102],
          dst_module_id: 201,
          service_name: 'risk.v1.UnattrSvc',
          operation: 'Check',
          status: 'UNATTRIBUTED',
          category: 'UNATTRIBUTED',
          call_count: 5,
          sample_traffic_record_id: 888,
        },
        {
          candidate_module_ids: [101, 201],
          dst_module_id: 202,
          service_name: 'risk.v1.PendingSvc',
          operation: 'Wait',
          status: 'UNATTRIBUTED',
          category: 'UNATTRIBUTED',
          call_count: 2,
        },
      ],
    };

    const overlay = synthesizeOverlay(baseNodes, staticEdges, report, 'LOGICAL');
    expect(overlay.edgeStatusMap.get('static_101_201')).toBe('VERIFIED');
    expect(overlay.edgeCallCountMap.get('static_101_201')).toBe(42);
    expect(overlay.edgeSampleRecordMap.get('static_101_201')).toBe(777);
    expect(overlay.dynamicNodes.some((n) => n.id === 'VIRTUAL_UNREGISTERED_INTERFACE')).toBe(true);
    expect(overlay.workingEdges).toHaveLength(3);
    expect(overlay.unattributedPendingList).toHaveLength(1);

    // 物理视角下 src_process_id 与 UNATTRIBUTED 归属验证
    const physNodes: GraphNode[] = [
      ...baseNodes,
      { id: 'proc_5', type: 'PROCESS', depth: 0, ancestors: [], name: 'Proc5', sort_order: 10 },
    ];
    const physReport: ConsistencyReportDTO = {
      session_id: '100',
      edges: [
        {
          src_process_id: 5,
          dst_module_id: 201,
          service_name: 'phys.v1.Viol',
          operation: 'Call',
          status: 'VIOLATION',
          category: 'VIOLATION',
          call_count: 4,
          sample_traffic_record_id: 901,
        },
        {
          src_process_id: 5,
          service_name: 'phys.v1.Unattr',
          operation: 'Call2',
          status: 'UNATTRIBUTED',
          category: 'UNATTRIBUTED',
          call_count: 6,
        },
      ],
    };
    const physOverlay = synthesizeOverlay(physNodes, staticEdges, physReport, 'PHYSICAL');
    expect(physOverlay.workingEdges.some((e) => e.source_node_id === 'proc_5')).toBe(true);

    // 验证 aggregateRollup 与 rollupEdges 门面输出完全一致
    const expand: ExpandStateMap = { sub_1: true, grp_11: true, sub_2: true, grp_21: true };
    const agg = aggregateRollup(overlay.workingNodes, overlay.workingEdges, expand, overlay);
    const facade = rollupEdges(baseNodes, staticEdges, expand, report, 'LOGICAL');
    expect(agg.rollupEdges).toEqual(facade.rollupEdges);
    expect(agg.badgeCounts).toEqual(facade.badgeCounts);
  });
});
