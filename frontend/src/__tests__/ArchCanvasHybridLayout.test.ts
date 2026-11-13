import { describe, it, expect, beforeEach } from 'vitest';
import type {
  GraphNode,
  GraphEdge,
  RollupEdgeModel,
  SavedNodePosition,
  TopologyLayoutDetailDTO,
} from '../types/topology_graph';
import { toScopedPositionKey } from '../types/topology_graph';
import {
  planLayout,
  mergeLocalLayout,
  resolveOverlapsBottomUp,
  markDegradedEdges,
  placeNewNodes,
  diffLayoutAgainstGraph,
  computeViewportCompensation,
  clearUnpinnedInScope,
  type NodeBox,
} from '../components/ArchCanvas/layout/layoutMerger';
import { processLayoutCoordinates } from '../components/ArchCanvas/layout/layoutProcessor';
import {
  resolveInitialLayout,
  composeEffectivePositions,
  extractPersistablePositions,
  diffOverrides,
  getDraftStorageKey,
  saveTopologyDraft,
  loadTopologyDraft,
  clearTopologyDraft,
  clearAllTopologyDrafts,
  restoreExpandStateFromSaved,
  serializeExpandState,
  normalizeSavedFilterState,
  serializeBackendFilterState,
} from '../components/ArchCanvas/layout/layoutPersistence';

describe('M19-P4: ArchCanvasHybridLayout Suite', () => {
  const sampleNodes: GraphNode[] = [
    { id: 'sub_1', type: 'SUBSYSTEM', depth: 0, ancestors: [], name: '交易子系统', sort_order: 1 },
    { id: 'grp_11', type: 'FUNCTIONAL_GROUP', parent_id: 'sub_1', depth: 1, ancestors: ['sub_1'], name: '订单组', sort_order: 1 },
    { id: 'grp_12', type: 'FUNCTIONAL_GROUP', parent_id: 'sub_1', depth: 1, ancestors: ['sub_1'], name: '支付组', sort_order: 2 },
    { id: 'mod_101', type: 'MODULE', parent_id: 'grp_11', depth: 2, ancestors: ['sub_1', 'grp_11'], name: '下单模块', sort_order: 1 },
    { id: 'mod_102', type: 'MODULE', parent_id: 'grp_11', depth: 2, ancestors: ['sub_1', 'grp_11'], name: '拆单模块', sort_order: 2 },
    { id: 'sub_2', type: 'SUBSYSTEM', depth: 0, ancestors: [], name: '风控子系统', sort_order: 2 },
    { id: 'sub_3', type: 'SUBSYSTEM', depth: 0, ancestors: [], name: '清结算子系统', sort_order: 3 },
  ];

  beforeEach(() => {
    if (typeof localStorage !== 'undefined') {
      localStorage.clear();
    }
  });

  // 1. testPlanLayout_DragDoesNotTriggerElk_ContainerToggleRunsLocalSubtreeOnly
  it('testPlanLayout_DragDoesNotTriggerElk_ContainerToggleRunsLocalSubtreeOnly', () => {
    const existingPositions: Record<string, SavedNodePosition> = {
      sub_1: { x: 20, y: 20, is_expanded: false, width: 260, height: 80 },
      sub_2: { x: 340, y: 20, is_expanded: false, width: 260, height: 80 },
      sub_3: { x: 660, y: 20, is_expanded: false, width: 260, height: 80 },
    };

    // 1.1 单纯节点拖拽不触发 ELK 重排
    const dragPlan = planLayout({
      reason: 'DRAG',
      prevExpandState: {},
      nextExpandState: {},
      savedPositions: existingPositions,
      nodes: sampleNodes,
    });
    expect(dragPlan.mode).toBe('NONE');

    // 1.2 单个容器 sub_1 展开仅触发局部子树重排 ELK_SUBTREE，且外部节点 sub_2/sub_3 坐标零漂移
    const togglePlan = planLayout({
      reason: 'CONTAINER_TOGGLE',
      prevExpandState: {},
      nextExpandState: { sub_1: true },
      savedPositions: existingPositions,
      nodes: sampleNodes,
    });
    expect(togglePlan.mode).toBe('ELK_SUBTREE');
    expect(togglePlan.subtreeRootId).toBe('sub_1');

    // 合并局部子树排版后，sub_2 与 sub_3 的 Y 坐标以及未重叠时的位置保持不变
    const merged = mergeLocalLayout({
      subtreeRootId: 'sub_1',
      subtreeBoxes: new Map<string, NodeBox>([
        ['sub_1', { id: 'sub_1', x: 20, y: 20, width: 300, height: 180 }],
        ['grp_11', { id: 'grp_11', parentId: 'sub_1', x: 16, y: 52, width: 260, height: 80 }],
        ['grp_12', { id: 'grp_12', parentId: 'sub_1', x: 16, y: 148, width: 260, height: 80 }],
      ]),
      existingPositions,
      nodes: sampleNodes,
      expandState: { sub_1: true },
    });
    expect(merged.positions['sub_2'].y).toBe(20);
    expect(merged.positions['sub_3'].y).toBe(20);
  });

  // 2. testResolveOverlapsBottomUp_InnerContainerExpands_PushesSiblingsAndParentBounds
  it('testResolveOverlapsBottomUp_InnerContainerExpands_PushesSiblingsAndParentBounds', () => {
    // 初始状态：sub_1 内含 grp_11 (展开后变宽变高) 与同层兄弟 grp_12，且顶层 sub_2 紧贴 sub_1 右侧
    const boxes = new Map<string, NodeBox>([
      ['sub_1', { id: 'sub_1', x: 20, y: 20, width: 280, height: 160, isExpanded: true }],
      ['grp_11', { id: 'grp_11', parentId: 'sub_1', x: 16, y: 52, width: 320, height: 180, isExpanded: true }],
      ['grp_12', { id: 'grp_12', parentId: 'sub_1', x: 100, y: 60, width: 260, height: 80, isExpanded: false }],
      ['sub_2', { id: 'sub_2', x: 260, y: 20, width: 260, height: 80, isExpanded: false }],
    ]);

    const res = resolveOverlapsBottomUp(boxes, sampleNodes, 'grp_11');

    const grp11 = res.boxes.get('grp_11')!;
    const grp12 = res.boxes.get('grp_12')!;
    const sub1 = res.boxes.get('sub_1')!;
    const sub2 = res.boxes.get('sub_2')!;

    // 断言 1: grp_12 被 grp_11 推开，满足最小间距 16px
    const noInnerOverlapX = grp12.x >= grp11.x + grp11.width + 16 || grp11.x >= grp12.x + grp12.width + 16;
    const noInnerOverlapY = grp12.y >= grp11.y + grp11.height + 16 || grp11.y >= grp12.y + grp12.height + 16;
    expect(noInnerOverlapX || noInnerOverlapY).toBe(true);

    // 断言 2: 父容器 sub_1 自动撑大包围盒 (包含 grp_11 和 grp_12 + right/bottom padding 16)
    expect(sub1.width).toBeGreaterThanOrEqual(Math.max(grp11.x + grp11.width, grp12.x + grp12.width) + 16);
    expect(sub1.height).toBeGreaterThanOrEqual(Math.max(grp11.y + grp11.height, grp12.y + grp12.height) + 16);

    // 断言 3: 顶层兄弟 sub_2 被撑大的 sub_1 推开，无重叠
    const noRootOverlapX = sub2.x >= sub1.x + sub1.width + 16 || sub1.x >= sub2.x + sub2.width + 16;
    const noRootOverlapY = sub2.y >= sub1.y + sub1.height + 16 || sub1.y >= sub2.y + sub2.height + 16;
    expect(noRootOverlapX || noRootOverlapY).toBe(true);
    expect(res.touchedIds.has('grp_12')).toBe(true);
    expect(res.touchedIds.has('sub_1')).toBe(true);
    expect(res.touchedIds.has('sub_2')).toBe(true);
  });

  // 3. testMarkDegradedEdges_TouchedContainersAndDescendants_SwitchToSmoothStep
  it('testMarkDegradedEdges_TouchedContainersAndDescendants_SwitchToSmoothStep', () => {
    const edges: RollupEdgeModel[] = [
      {
        id: 'e_touched_child',
        source: 'mod_101', // 属于被触碰容器 sub_1 的后代
        target: 'sub_2',
        kind: 'CROSS_PROCESS',
        services: [],
        rawEdges: [],
        bendPoints: [{ x: 100, y: 100 }],
      },
      {
        id: 'e_untouched',
        source: 'sub_2',
        target: 'sub_3',
        kind: 'CROSS_PROCESS',
        services: [],
        rawEdges: [],
        bendPoints: [{ x: 400, y: 100 }],
      },
    ];

    const marked = markDegradedEdges(edges, new Set(['sub_1']), sampleNodes);
    const e1 = marked.find((e) => e.id === 'e_touched_child')!;
    const e2 = marked.find((e) => e.id === 'e_untouched')!;

    expect(e1.isDraggedDegraded).toBe(true);
    expect(e2.isDraggedDegraded).toBe(false);
    expect(e2.bendPoints).toEqual([{ x: 400, y: 100 }]);
  });

  // 4. testDiffLayoutAgainstGraph_StalePruned_ReparentInvalidated_NewPlacedAtCentroid
  it('testDiffLayoutAgainstGraph_StalePruned_ReparentInvalidated_NewPlacedAtCentroid', () => {
    const savedPositions: Record<string, SavedNodePosition> = {
      sub_1: { x: 20, y: 20, parent_id: null, is_expanded: true },
      grp_11: { x: 16, y: 52, parent_id: 'sub_1', is_expanded: true },
      mod_101: { x: 20, y: 60, parent_id: 'grp_11', is_expanded: false },
      // mod_102 被错误记录在旧父容器 grp_old 下 (re-parented)
      mod_102: { x: 500, y: 500, parent_id: 'grp_old', is_expanded: false },
      // mod_deleted 已从最新图中删除 (stale)
      mod_deleted: { x: 800, y: 800, parent_id: 'grp_11', is_expanded: false },
      sub_2: { x: 400, y: 20, parent_id: null, is_expanded: false },
    };

    const edges: GraphEdge[] = [
      { id: 'e1', kind: 'CROSS_PROCESS', source_node_id: 'mod_101', target_node_id: 'mod_102', services: [] },
    ];

    const diff = diffLayoutAgainstGraph(savedPositions, sampleNodes, edges);

    // 1. 已删除节点 mod_deleted 被自动清理
    expect(diff.staleIds).toEqual(['mod_deleted']);
    expect(diff.reconciledPositions['mod_deleted']).toBeUndefined();

    // 2. 跨容器迁移节点 mod_102 旧坐标失效并重新放置
    expect(diff.reparentedIds).toContain('mod_102');
    expect(diff.reconciledPositions['mod_102'].parent_id).toBe('grp_11');

    // 3. 新增节点 (grp_12, sub_3) 自动获得初始位置
    expect(diff.newIds).toContain('grp_12');
    expect(diff.newIds).toContain('sub_3');
    expect(diff.reconciledPositions['sub_3']).toBeDefined();
    expect(diff.driftCount).toBe(diff.staleIds.length + diff.reparentedIds.length + diff.newIds.length);

    // 验证 placeNewNodes 对同父容器有邻居关联的新节点放置在邻居质心附近
    const placed = placeNewNodes(
      {
        mod_101: { x: 40, y: 80, parent_id: 'grp_11', is_expanded: false },
      },
      ['mod_102'],
      sampleNodes,
      edges
    );
    expect(placed['mod_102']).toBeDefined();
    expect(Number.isFinite(placed['mod_102'].x)).toBe(true);
    expect(Number.isFinite(placed['mod_102'].y)).toBe(true);
  });

  // 5. testComputeViewportCompensation_KeepsAnchorScreenPositionInvariant
  it('testComputeViewportCompensation_KeepsAnchorScreenPositionInvariant', () => {
    const oldAnchor = { x: 120, y: 80 };
    const newAnchor = { x: 185, y: 110 };
    const prevViewport = { x: 50, y: -30, zoom: 1.25 };

    const nextViewport = computeViewportCompensation(oldAnchor, newAnchor, prevViewport);

    const screenBeforeX = oldAnchor.x * prevViewport.zoom + prevViewport.x;
    const screenBeforeY = oldAnchor.y * prevViewport.zoom + prevViewport.y;
    const screenAfterX = newAnchor.x * nextViewport.zoom + nextViewport.x;
    const screenAfterY = newAnchor.y * nextViewport.zoom + nextViewport.y;

    expect(Math.abs(screenAfterX - screenBeforeX)).toBeLessThan(0.01);
    expect(Math.abs(screenAfterY - screenBeforeY)).toBeLessThan(0.01);
    expect(nextViewport.zoom).toBe(prevViewport.zoom);
  });

  // 6. testResolveInitialLayout_PriorityChain_UrlOverDraftOverDefaultOverCanonicalOverElk
  it('testResolveInitialLayout_PriorityChain_UrlOverDraftOverDefaultOverCanonicalOverElk', () => {
    const makeLayout = (id: number, name: string, isDefault = false): TopologyLayoutDetailDTO => ({
      id,
      scope: id === 1 ? 'SHARED' : 'PRIVATE',
      view: 'LOGICAL',
      owner_id: id === 1 ? null : 7,
      name,
      description: '',
      focus_scope: 'GLOBAL',
      is_default: isDefault,
      schema_version: 1,
      structure_version: 's1-logical-abc',
      revision: 1,
      node_positions: { sub_1: { x: id * 10, y: 20, is_expanded: false } },
      expand_state: { depth_level: 0, expanded_node_ids: [] },
      filter_state: {
        neighborhood_hop: 0,
        neighborhood_direction: 'BOTH',
        owner_id: null,
        consistency_only: false,
        hide_isolated: false,
        fold_hub_edges: true,
      },
      viewport: { x: 0, y: 0, zoom: 1 },
      created_by: 7,
      updated_by: 7,
      created_at: '',
      updated_at: '',
    });

    const canonical = makeLayout(1, '团队基准');
    const defaultPersonal = makeLayout(2, '个人默认', true);
    const urlPersonal = makeLayout(3, 'URL指定书签', false);
    const draft = {
      view: 'LOGICAL' as const,
      structure_version: 's1-logical-abc',
      focus_scope: 'sub_1',
      node_positions: { sub_1: { x: 999, y: 999, is_expanded: true } },
      expand_state: { depth_level: 1, expanded_node_ids: ['sub_1'] },
      filter_state: {
        neighborhood_hop: 0,
        neighborhood_direction: 'BOTH' as const,
        owner_id: null,
        consistency_only: false,
        hide_isolated: false,
        fold_hub_edges: true,
      },
      viewport: { x: 10, y: 20, zoom: 1 },
      updated_at: Date.now(),
    };

    // P1: URL 显式指定优先于一切
    expect(
      resolveInitialLayout({
        urlBookmark: urlPersonal,
        draft,
        defaultBookmark: defaultPersonal,
        canonical,
      }).source
    ).toBe('URL_BOOKMARK');

    // P2: 本地草稿优先于个人默认与团队基准
    expect(
      resolveInitialLayout({
        urlBookmark: null,
        draft,
        defaultBookmark: defaultPersonal,
        canonical,
      }).source
    ).toBe('LOCAL_DRAFT');

    // P3: 个人默认书签优先于团队基准
    expect(
      resolveInitialLayout({
        urlBookmark: null,
        draft: null,
        defaultBookmark: defaultPersonal,
        canonical,
      }).source
    ).toBe('PERSONAL_DEFAULT');

    // P4: 团队基准优先于冷启动 ELK
    expect(
      resolveInitialLayout({
        urlBookmark: null,
        draft: null,
        defaultBookmark: null,
        canonical,
      }).source
    ).toBe('CANONICAL');

    // P5: 无任何持久化数据时回退 ELK_DEFAULT
    expect(
      resolveInitialLayout({
        urlBookmark: null,
        draft: null,
        defaultBookmark: null,
        canonical: null,
      }).source
    ).toBe('ELK_DEFAULT');
  });

  // 7. testComposeEffectivePositions_CanonicalPlusBookmarkOverrides_AndPinLock
  it('testComposeEffectivePositions_CanonicalPlusBookmarkOverrides_AndPinLock', () => {
    const canonicalPos: Record<string, SavedNodePosition> = {
      sub_1: { x: 10, y: 20, is_expanded: false },
      sub_2: { x: 300, y: 20, is_expanded: false },
    };
    const bookmarkPos: Record<string, SavedNodePosition> = {
      sub_2: { x: 350, y: 80, is_expanded: false, pinned: true },
    };
    const sessionPos: Record<string, SavedNodePosition> = {
      sub_1: { x: 40, y: 50, is_expanded: true },
    };

    const effective = composeEffectivePositions(canonicalPos, bookmarkPos, sessionPos);
    expect(effective['sub_1']).toEqual({ x: 40, y: 50, is_expanded: true });
    expect(effective['sub_2']).toEqual({ x: 350, y: 80, is_expanded: false, pinned: true });

    // 验证 diffOverrides 仅提取相对基准有变化的节点
    const overrides = diffOverrides(canonicalPos, effective);
    expect(Object.keys(overrides).sort()).toEqual(['sub_1', 'sub_2']);

    // 验证 pinned 节点在碰撞推挤中坐标保持锁定不被推走
    const boxes = new Map<string, NodeBox>([
      ['sub_1', { id: 'sub_1', x: 340, y: 75, width: 260, height: 80, pinned: false }],
      ['sub_2', { id: 'sub_2', x: 350, y: 80, width: 260, height: 80, pinned: true }],
    ]);
    const resolved = resolveOverlapsBottomUp(boxes, sampleNodes);
    expect(resolved.boxes.get('sub_2')?.x).toBe(350);
    expect(resolved.boxes.get('sub_2')?.y).toBe(80);
  });

  // 8. testExtractPersistablePositions_StripsDynamicViolationAndExternalContextNodes
  it('testExtractPersistablePositions_StripsDynamicViolationAndExternalContextNodes', () => {
    const rawPositions: Record<string, SavedNodePosition> = {
      sub_1: { x: 10, y: 20, is_expanded: false },
      mod_101: { x: 30, y: 60, parent_id: 'grp_11', is_expanded: false },
      VIRTUAL_UNREGISTERED_INTERFACE: { x: 500, y: 100, is_expanded: false },
      UNATTRIBUTED_ROOT: { x: 600, y: 100, is_expanded: false },
      ext_ctx_DOWNSTREAM_sub_2: { x: 700, y: 100, is_expanded: false },
      dyn_viol_mod_101_mod_201: { x: 800, y: 100, is_expanded: false },
    };

    const cleaned = extractPersistablePositions(rawPositions);
    expect(Object.keys(cleaned).sort()).toEqual(['mod_101', 'sub_1']);
    expect(cleaned['VIRTUAL_UNREGISTERED_INTERFACE']).toBeUndefined();
    expect(cleaned['ext_ctx_DOWNSTREAM_sub_2']).toBeUndefined();
  });

  // 9. testDraftStorage_UserIsolatedKey_ClearOnLogoutAndSave
  it('testDraftStorage_UserIsolatedKey_ClearOnLogoutAndSave', () => {
    expect(getDraftStorageKey(42, 'LOGICAL')).toBe('traefik_topo_draft_v2_42_LOGICAL');
    expect(getDraftStorageKey(99, 'PHYSICAL')).toBe('traefik_topo_draft_v2_99_PHYSICAL');

    saveTopologyDraft(42, 'LOGICAL', {
      view: 'LOGICAL',
      structure_version: 's1-logical-111',
      focus_scope: 'GLOBAL',
      node_positions: { sub_1: { x: 11, y: 22, is_expanded: false } },
      expand_state: { depth_level: 0, expanded_node_ids: [] },
      filter_state: {
        neighborhood_hop: 0,
        neighborhood_direction: 'BOTH',
        owner_id: null,
        consistency_only: false,
        hide_isolated: false,
        fold_hub_edges: true,
      },
      viewport: { x: 0, y: 0, zoom: 1 },
      updated_at: Date.now(),
    });

    // 用户 99 读取不到用户 42 的草稿
    expect(loadTopologyDraft(99, 'LOGICAL')).toBeNull();
    expect(loadTopologyDraft(42, 'LOGICAL')?.node_positions['sub_1'].x).toBe(11);

    // 保存/重置后清除当前用户当前视角草稿
    clearTopologyDraft(42, 'LOGICAL');
    expect(loadTopologyDraft(42, 'LOGICAL')).toBeNull();

    // 登出时一键清空所有用户草稿
    saveTopologyDraft(42, 'PHYSICAL', {
      view: 'PHYSICAL',
      structure_version: 's1-physical-111',
      focus_scope: 'GLOBAL',
      node_positions: {},
      expand_state: { depth_level: 0, expanded_node_ids: [] },
      filter_state: {
        neighborhood_hop: 0,
        neighborhood_direction: 'BOTH',
        owner_id: null,
        consistency_only: false,
        hide_isolated: false,
        fold_hub_edges: true,
      },
      viewport: { x: 0, y: 0, zoom: 1 },
      updated_at: Date.now(),
    });
    clearAllTopologyDrafts();
    expect(loadTopologyDraft(42, 'PHYSICAL')).toBeNull();
  });

  // 10. testRestoreFromBookmark_OnlyTouchesSavedVisibleSet_NeverExpandsFullTree
  it('testRestoreFromBookmark_OnlyTouchesSavedVisibleSet_NeverExpandsFullTree', () => {
    const manyNodes: GraphNode[] = Array.from({ length: 300 }, (_, i) => ({
      id: `sub_${i + 1}`,
      type: 'SUBSYSTEM',
      depth: 0,
      ancestors: [],
      name: `子系统_${i + 1}`,
      sort_order: i + 1,
    }));

    const restoredExpand = restoreExpandStateFromSaved(
      {
        depth_level: 0,
        expanded_node_ids: ['sub_1', 'sub_2'],
        collapsed_node_ids: ['sub_3'],
      },
      manyNodes
    );

    const expandedKeys = Object.keys(restoredExpand).filter((k) => restoredExpand[k]);
    expect(expandedKeys.sort()).toEqual(['sub_1', 'sub_2']);
    expect(expandedKeys.length).toBe(2);

    const serialized = serializeExpandState(
      { sub_1: true, sub_2: false, ext_ctx_DOWNSTREAM_sub_3: true },
      1
    );
    expect(serialized.depth_level).toBe(1);
    expect(serialized.expanded_node_ids).toEqual(['sub_1']);
    expect(serialized.collapsed_node_ids).toEqual(['sub_2']);

    expect(
      planLayout({
        reason: 'RESET',
        prevExpandState: {},
        nextExpandState: {},
        savedPositions: { sub_1: { x: 0, y: 0, is_expanded: false } },
        nodes: sampleNodes,
      }).mode
    ).toBe('ELK_FULL');

    expect(
      planLayout({
        reason: 'BOOKMARK_RESTORE',
        prevExpandState: {},
        nextExpandState: {},
        savedPositions: { sub_1: { x: 0, y: 0, is_expanded: false } },
        nodes: sampleNodes,
      }).mode
    ).toBe('SAVED_POSITIONS');

    expect(
      planLayout({
        reason: 'CONTAINER_TOGGLE',
        prevExpandState: {},
        nextExpandState: { sub_1: true, grp_11: true },
        savedPositions: { sub_1: { x: 0, y: 0, is_expanded: false } },
        nodes: sampleNodes,
      })
    ).toEqual({ mode: 'ELK_SUBTREE', subtreeRootId: 'sub_1' });
  });

  // 11. testDiffLayoutAgainstGraph_UnexpandedContainerChildrenNotMarkedNew
  it('testDiffLayoutAgainstGraph_UnexpandedContainerChildrenNotMarkedNew', () => {
    // 场景 A: 仅保存了 L0 折叠态顶层容器坐标
    const l0Saved: Record<string, SavedNodePosition> = {
      sub_1: { x: 20, y: 20, parent_id: null, is_expanded: false, width: 260, height: 80 },
      sub_2: { x: 340, y: 20, parent_id: null, is_expanded: false, width: 260, height: 80 },
      sub_3: { x: 660, y: 20, parent_id: null, is_expanded: false, width: 260, height: 80 },
    };

    const diff = diffLayoutAgainstGraph(l0Saved, sampleNodes, []);
    // 未展开容器 sub_1 内部的 grp_11 / grp_12 / mod_101 / mod_102 不应被误判为新增节点
    expect(diff.newIds).toEqual([]);
    expect(diff.driftCount).toBe(0);
    expect(diff.reconciledPositions['grp_11']).toBeUndefined();

    // 场景 B: processLayoutCoordinates 必须自底向上撑大展开态父容器包围盒，绝不允许子容器画到父框外
    const elkRoot = {
      id: 'root',
      x: 0,
      y: 0,
      width: 600,
      height: 300,
      children: [
        {
          id: 'sub_1',
          x: 20,
          y: 20,
          width: 496,
          height: 256,
          children: [
            { id: 'grp_11', x: 16, y: 52, width: 260, height: 80 },
            { id: 'grp_12', x: 296, y: 52, width: 260, height: 80 },
          ],
        },
      ],
    };

    // 模拟 grp_12 在保存坐标中位于 x=380, y=220 (超出了 ELK 给 sub_1 的 496x256)
    const savedWithDisplacedChild: Record<string, SavedNodePosition> = {
      sub_1: { x: 20, y: 20, parent_id: null, is_expanded: true, width: 496, height: 256 },
      grp_11: { x: 16, y: 52, parent_id: 'sub_1', is_expanded: false, width: 260, height: 80 },
      grp_12: { x: 380, y: 220, parent_id: 'sub_1', is_expanded: false, width: 260, height: 80 },
    };

    const visibleSub = sampleNodes.filter((n) => ['sub_1', 'grp_11', 'grp_12'].includes(n.id));
    const rollupEdges: RollupEdgeModel[] = [
      {
        id: 'e_grp11_grp12',
        source: 'grp_11',
        target: 'grp_12',
        kind: 'CROSS_PROCESS',
        services: [{ service_name: 'svc.Test' }],
        rawEdges: [],
      },
    ];

    const processed = processLayoutCoordinates(
      elkRoot,
      visibleSub,
      rollupEdges,
      new Set(),
      { sub_1: true },
      {},
      undefined,
      { sub_1: 2, grp_11: 2, grp_12: 0 },
      { savedPositions: savedWithDisplacedChild }
    );

    const sub1Node = processed.nodes.find((n) => n.id === 'sub_1')!;
    const grp12Node = processed.nodes.find((n) => n.id === 'grp_12')!;
    expect(Number(sub1Node.style?.width)).toBeGreaterThanOrEqual(
      grp12Node.position.x + Number(grp12Node.style?.width ?? 260) + 16
    );
    expect(Number(sub1Node.style?.height)).toBeGreaterThanOrEqual(
      grp12Node.position.y + Number(grp12Node.style?.height ?? 80) + 16
    );
    // 由于 grp_12 相对 ELK 坐标发生偏移，关联边必须自动标记为 isDraggedDegraded = true (SmoothStep)
    expect(processed.edges[0].data?.isDraggedDegraded).toBe(true);
  });
});

describe('M21-P3: Scoped Position Persistence, Pinning & Backend Contract Alignment', () => {
  // 1. 按 focusScope 隔离位置记忆，绝不跨作用域污染
  it('testScopedPositionMemory_IsolatedPerFocusScope_NeverLeaksAcrossContainers', () => {
    const visibleAtGlobal: GraphNode[] = [
      { id: 'sub_1', type: 'SUBSYSTEM', depth: 0, ancestors: [], name: '交易域', sort_order: 1 },
      { id: 'ext_ctx_UPSTREAM_sub_2', type: 'EXTERNAL_CONTEXT', depth: 0, ancestors: [], name: '外部上游', sort_order: 2 },
    ];
    const visibleAtSub1: GraphNode[] = [
      { id: 'grp_11', type: 'FUNCTIONAL_GROUP', parent_id: 'sub_1', depth: 1, ancestors: ['sub_1'], name: '订单组', sort_order: 1 },
      { id: 'ext_ctx_UPSTREAM_sub_2', type: 'EXTERNAL_CONTEXT', depth: 0, ancestors: [], name: '外部上游', sort_order: 2 },
    ];

    const savedPositions: Record<string, SavedNodePosition> = {
      [toScopedPositionKey('GLOBAL', 'ext_ctx_UPSTREAM_sub_2')]: {
        x: 50,
        y: 60,
        is_expanded: false,
        pinned: true,
        width: 240,
        height: 104,
      },
      [toScopedPositionKey('sub_1', 'ext_ctx_UPSTREAM_sub_2')]: {
        x: 420,
        y: 380,
        is_expanded: false,
        pinned: true,
        width: 240,
        height: 104,
      },
      [toScopedPositionKey('sub_1', 'grp_11')]: {
        x: 320,
        y: 120,
        is_expanded: false,
        pinned: true,
        width: 260,
        height: 96,
      },
    };

    const elkGlobalRoot = {
      id: 'root',
      children: [
        { id: 'sub_1', x: 100, y: 100, width: 260, height: 96 },
        { id: 'ext_ctx_UPSTREAM_sub_2', x: 10, y: 10, width: 240, height: 104 },
      ],
    };
    const elkSub1Root = {
      id: 'root',
      children: [
        { id: 'grp_11', x: 20, y: 20, width: 260, height: 96 },
        { id: 'ext_ctx_UPSTREAM_sub_2', x: 10, y: 10, width: 240, height: 104 },
      ],
    };

    const processedGlobal = processLayoutCoordinates(
      elkGlobalRoot,
      visibleAtGlobal,
      [],
      new Set(),
      {},
      {},
      undefined,
      {},
      { savedPositions, focusScope: 'GLOBAL' }
    );
    const processedSub1 = processLayoutCoordinates(
      elkSub1Root,
      visibleAtSub1,
      [],
      new Set(),
      {},
      {},
      undefined,
      {},
      { savedPositions, focusScope: 'sub_1' }
    );

    const extGlobal = processedGlobal.nodes.find((n) => n.id === 'ext_ctx_UPSTREAM_sub_2')!;
    const extSub1 = processedSub1.nodes.find((n) => n.id === 'ext_ctx_UPSTREAM_sub_2')!;
    expect(extGlobal.position).toEqual({ x: 50, y: 60 });
    expect(extSub1.position).toEqual({ x: 420, y: 380 });
    expect(processedGlobal.nodes.find((n) => n.id === 'grp_11')).toBeUndefined();
    expect(processedSub1.nodes.find((n) => n.id === 'grp_11')?.position).toEqual({ x: 320, y: 120 });
  });

  // 2. 仅显式拖拽或锁定才固化坐标，ELK 初始坐标保持自由态
  it('testDragAndPin_OnlyExplicitUserActionLocksPosition_ElkOutputStaysFluid', () => {
    const rawSaved: Record<string, SavedNodePosition> = {
      'GLOBAL::sub_1': { x: 120, y: 80, is_expanded: false, pinned: false },
      'GLOBAL::sub_2': { x: 480, y: 140, is_expanded: false, pinned: true },
    };

    const persisted = extractPersistablePositions(rawSaved, { onlyPinned: true });
    expect(persisted['GLOBAL::sub_1']).toBeUndefined();
    expect(persisted['GLOBAL::sub_2']).toEqual(expect.objectContaining({ x: 480, y: 140, pinned: true }));
  });

  // 3. 自动整理按钮仅清理当前 scope 下未钉住节点，保留 pinned 及其他 scope 数据
  it('testAutoRelayoutButton_ClearsUnpinnedAndKeepsPinnedNodesInCurrentScope', () => {
    const savedPositions: Record<string, SavedNodePosition> = {
      'GLOBAL::sub_1': { x: 100, y: 100, is_expanded: false, pinned: false },
      'GLOBAL::sub_2': { x: 420, y: 100, is_expanded: false, pinned: true },
      'GLOBAL::sub_3': { x: 740, y: 100, is_expanded: false, pinned: false },
      'sub_1::grp_11': { x: 200, y: 200, is_expanded: false, pinned: false },
      'sub_1::grp_12': { x: 500, y: 200, is_expanded: false, pinned: true },
    };

    const next = clearUnpinnedInScope(savedPositions, 'GLOBAL', new Set(['sub_2']));
    expect(next['GLOBAL::sub_1']).toBeUndefined();
    expect(next['GLOBAL::sub_3']).toBeUndefined();
    expect(next['GLOBAL::sub_2']).toEqual({ x: 420, y: 100, is_expanded: false, pinned: true });
    // 其他 scope 保持完全不动
    expect(next['sub_1::grp_11']).toEqual({ x: 200, y: 200, is_expanded: false, pinned: false });
    expect(next['sub_1::grp_12']).toEqual({ x: 500, y: 200, is_expanded: false, pinned: true });
  });

  // 4. 支持带作用域前缀的外部上下文卡片持久化，并与后端 filter_state 契约 1:1 对齐
  it('testLayoutPersistence_SupportsScopedExternalContextIds_AndAlignsFilterStateContract', () => {
    const persisted = extractPersistablePositions(
      {
        'sub_1::ext_ctx_UPSTREAM_sub_2': { x: 40, y: 80, is_expanded: false, pinned: true },
        ext_ctx_DOWNSTREAM_sub_3: { x: 90, y: 90, is_expanded: false, pinned: true }, // 历史无作用域前缀，应继续被过滤
        'GLOBAL::sub_1': { x: 300, y: 200, is_expanded: false, pinned: true },
        'GLOBAL::ghost_node': { x: 999, y: 999, is_expanded: false, pinned: true },
      },
      { validNodeIds: new Set(['sub_1', 'sub_2', 'sub_3']) }
    );

    expect(persisted['sub_1::ext_ctx_UPSTREAM_sub_2']).toBeDefined();
    expect(persisted['ext_ctx_DOWNSTREAM_sub_3']).toBeUndefined();
    expect(persisted['GLOBAL::sub_1']).toBeDefined();
    expect(persisted['GLOBAL::ghost_node']).toBeUndefined();

    const diff = diffLayoutAgainstGraph(
      persisted,
      [
        { id: 'sub_1', type: 'SUBSYSTEM', depth: 0, ancestors: [], name: 'sub1', sort_order: 1 },
        { id: 'sub_2', type: 'SUBSYSTEM', depth: 0, ancestors: [], name: 'sub2', sort_order: 2 },
        { id: 'sub_3', type: 'SUBSYSTEM', depth: 0, ancestors: [], name: 'sub3', sort_order: 3 },
      ],
      []
    );
    expect(diff.staleIds).toEqual([]);
    expect(diff.reconciledPositions['sub_1::ext_ctx_UPSTREAM_sub_2']).toBeDefined();

    const serializedFilter = serializeBackendFilterState({
      subsystemIds: ['sub_1', 'sub_2'],
      onlyMyScope: true,
      statusFilter: 'ABNORMAL',
      egoNodeId: 'mod_101',
      neighborhoodHop: 2,
      hideIsolated: true,
      foldHubEdges: true,
      ownerId: 42,
    });
    expect(serializedFilter).toEqual({
      subsystem_ids: ['sub_1', 'sub_2'],
      only_my_scope: true,
      status: 'ABNORMAL',
      ego_node_id: 'mod_101',
      ego_hops: 2,
      hide_isolated: true,
      hub_fold: true,
      owner_id: 42,
      consistency_only: true,
      fold_hub_edges: true,
      neighborhood_direction: 'BOTH',
      neighborhood_hop: 2,
    });

    const normalized = normalizeSavedFilterState(serializedFilter);
    expect(normalized.subsystemIds).toEqual(['sub_1', 'sub_2']);
    expect(normalized.onlyMyScope).toBe(true);
    expect(normalized.statusFilter).toBe('ABNORMAL');
    expect(normalized.egoNodeId).toBe('mod_101');
    expect(normalized.neighborhoodHop).toBe(2);
    expect(normalized.hideIsolated).toBe(true);
    expect(normalized.foldHubEdges).toBe(true);
  });
});
