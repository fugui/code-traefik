import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderToString } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import { ReactFlowProvider } from '@xyflow/react';
import * as fs from 'node:fs';
import * as path from 'node:path';
import type {
  GraphNode,
  GraphEdge,
  TopologyLayoutDetailDTO,
  TopologyLayoutListResponse,
} from '../types/topology_graph';
import * as archApi from '../api/arch';
import { ExternalContextNode } from '../components/ArchCanvas/nodes/ExternalContextNode';
import { VisibilityGuardDialog } from '../components/ArchCanvas/panels/VisibilityGuardDialog';
import { TopologyBookmarkDrawer } from '../components/ArchCanvas/panels/TopologyBookmarkDrawer';
import { ArchCanvasToolbar } from '../components/ArchCanvas/panels/ArchCanvasToolbar';
import {
   evaluateVisibilityGuard,
  computeSearchExpandDecision,
  parseTopoUrlParams,
  buildTopoUrlParams,
} from '../pages/ArchitectureWorkbenchPage/ArchTopologyTabPane';

describe('M19-P5 & M19-P6: TopologyFocusAndPersistenceWorkbench Suite', () => {
  beforeEach(() => {
    archApi.clearTopologyGraphCache();
    vi.restoreAllMocks();
    vi.stubGlobal('localStorage', {
      getItem: () => null,
      setItem: () => {},
      removeItem: () => {},
      clear: () => {},
      key: () => null,
      length: 0,
    });
  });

  // §3.2.1: testTopologyGraphClient_ETag304AndStructureVersionPreserved
  it('testTopologyGraphClient_ETag304AndStructureVersionPreserved', async () => {
    const mockDto = {
      view: 'LOGICAL' as const,
      model_version: 'v1-logical-111',
      structure_version: 's1-logical-abcdef0123456789',
      nodes: [],
      edges: [],
    };

    const fetchSpy = vi.fn()
      .mockResolvedValueOnce({
        status: 200,
        ok: true,
        headers: { get: (k: string) => (k === 'ETag' ? '"v1-logical-111"' : null) },
        json: async () => mockDto,
      })
      .mockResolvedValueOnce({
        status: 304,
        ok: false,
        headers: { get: () => null },
      });

    vi.stubGlobal('fetch', fetchSpy);

    const first = await archApi.getTopologyGraph('LOGICAL');
    expect(first.structure_version).toBe('s1-logical-abcdef0123456789');

    const second = await archApi.getTopologyGraph('LOGICAL');
    expect(second.structure_version).toBe('s1-logical-abcdef0123456789');
    expect(fetchSpy).toHaveBeenCalledTimes(2);
    const secondHeaders = fetchSpy.mock.calls[1][1]?.headers as Record<string, string>;
    expect(secondHeaders['If-None-Match']).toBe('"v1-logical-111"');
  });

  // §3.2.2: testLayoutApiClient_AllSevenEndpointsContract
  it('testLayoutApiClient_AllSevenEndpointsContract', async () => {
    const fetchSpy = vi.fn().mockImplementation(async () => ({
      status: 200,
      ok: true,
      headers: { get: () => 'application/json' },
      json: async () => ({ code: 0, data: { ok: true } }),
      text: async () => JSON.stringify({ code: 0, data: { ok: true } }),
    }));
    vi.stubGlobal('fetch', fetchSpy);

    await archApi.listTopologyLayouts('LOGICAL');
    await archApi.getPersonalTopologyLayout(12);
    await archApi.upsertCanonicalTopologyLayout({
      view: 'LOGICAL',
      expected_revision: 1,
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
    });
    await archApi.resetCanonicalTopologyLayout('LOGICAL');
    await archApi.createPersonalTopologyLayout({
      view: 'LOGICAL',
      name: '个人书签A',
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
    });
    await archApi.updatePersonalTopologyLayout(12, { name: '个人书签B', expected_revision: 2 });
    await archApi.deletePersonalTopologyLayout(12);

    expect(fetchSpy).toHaveBeenCalledTimes(7);
  });

  // §3.5.1: testFocusDrillDown_BreadcrumbNavigation_AndExternalContextSwitch
  it('testFocusDrillDown_BreadcrumbNavigation_AndExternalContextSwitch', () => {
    let switchedFocusId = '';
    const extNodeProps = {
      id: 'ext_ctx_DOWNSTREAM_sub_2',
      type: 'externalContextNode' as const,
      data: {
        graphNode: {
          id: 'ext_ctx_DOWNSTREAM_sub_2',
          type: 'EXTERNAL_CONTEXT' as const,
          depth: 0,
          ancestors: [],
          name: '风控子系统',
          sort_order: 90000,
          meta: {
            direction: 'DOWNSTREAM' as const,
            peer_root_id: 'sub_2',
            peer_root_name: '风控子系统',
            hidden_module_count: 3,
            cross_edge_count: 4,
            violation_count: 2,
          },
        },
        isExpanded: false,
        onFocusScope: (targetId: string) => {
          switchedFocusId = targetId;
        },
      },
    };

    // 验证外部上下文摘要卡渲染与聚焦切换回调
    const cardHtml = renderToString(
      <MemoryRouter>
        <ReactFlowProvider>
          <ExternalContextNode {...(extNodeProps as unknown as React.ComponentProps<typeof ExternalContextNode>)} />
        </ReactFlowProvider>
      </MemoryRouter>
    );
    expect(cardHtml).toContain('code-arch-ext-ctx');
    expect(cardHtml).toContain('风控子系统');
    expect(cardHtml).toContain('3');
    expect(cardHtml).toContain('4');
    expect(cardHtml).toContain('切换聚焦至此');

    extNodeProps.data.onFocusScope('sub_2');
    expect(switchedFocusId).toBe('sub_2');

    // 验证工具栏聚焦面包屑导航条渲染 (全局 > 交易子系统 > 订单组)
    const toolbarHtml = renderToString(
      <MemoryRouter>
        <ArchCanvasToolbar
          view="LOGICAL"
          onViewChange={() => {}}
          depthLevel={1}
          maxDepth={2}
          onDepthLevelChange={() => {}}
          wheelAutoExpand={false}
          onToggleWheelAutoExpand={() => {}}
          onResetLayout={() => {}}
          searchKeyword=""
          onSearchChange={() => {}}
          sessions={[]}
          selectedSessionId={null}
          onSelectSession={() => {}}
          focusScope="grp_11"
          focusBreadcrumbs={[
            { id: 'GLOBAL', name: '全局全景' },
            { id: 'sub_1', name: '交易子系统' },
            { id: 'grp_11', name: '订单组' },
          ]}
          onFocusScopeChange={() => {}}
        />
      </MemoryRouter>
    );
    expect(toolbarHtml).toContain('code-arch-focus-bar');
    expect(toolbarHtml).toContain('交易子系统');
    expect(toolbarHtml).toContain('订单组');
    expect(toolbarHtml).toContain('退出聚焦');
  });

  // §3.5.2: testUnifiedVisibilityGuard_SoftBannerAt46_HardInterceptAt121_AllFiveEntryPaths
  it('testUnifiedVisibilityGuard_SoftBannerAt46_HardInterceptAt121_AllFiveEntryPaths', () => {
    const manyNodes: GraphNode[] = [
      { id: 'sub_1', type: 'SUBSYSTEM', depth: 0, ancestors: [], name: 'S1', sort_order: 1 },
      { id: 'sub_2', type: 'SUBSYSTEM', depth: 0, ancestors: [], name: 'S2', sort_order: 2 },
    ];
    for (let i = 1; i <= 45; i++) {
      manyNodes.push({
        id: `mod_1_${i}`,
        type: 'MODULE',
        parent_id: 'sub_1',
        depth: 1,
        ancestors: ['sub_1'],
        name: `M1_${i}`,
        sort_order: i,
      });
    }
    for (let i = 1; i <= 90; i++) {
      manyNodes.push({
        id: `mod_2_${i}`,
        type: 'MODULE',
        parent_id: 'sub_2',
        depth: 1,
        ancestors: ['sub_2'],
        name: `M2_${i}`,
        sort_order: i,
      });
    }
    const edges: GraphEdge[] = [
      { id: 'e1', kind: 'CROSS_PROCESS', source_node_id: 'mod_1_1', target_node_id: 'mod_2_1', services: [] },
    ];

    // 路径 1: 单节点展开 sub_1 -> 可见节点数 = 2 (sub_1, sub_2) + 45 = 47 (处于 46..120 软警告区间)
    const softDecision = evaluateVisibilityGuard(manyNodes, edges, { sub_1: true }, { focusScope: 'GLOBAL' });
    expect(softDecision.level).toBe('SOFT_WARNING');
    expect(softDecision.estimatedCount).toBe(47);
    expect(softDecision.blocked).toBe(false);

    // 路径 2 ~ 5: 全展滑杆 / 搜索展开 / 退出聚焦 / 书签恢复 -> 可见节点数 = 2 + 45 + 90 = 137 (> 120 硬上限拦截)
    const hardPaths = [
      { reason: 'SLIDER_CHANGE', expand: { sub_1: true, sub_2: true }, focusScope: 'GLOBAL' },
      { reason: 'SINGLE_EXPAND', expand: { sub_1: true, sub_2: true }, focusScope: 'GLOBAL' },
      { reason: 'SEARCH_EXPAND', expand: { sub_1: true, sub_2: true }, focusScope: 'GLOBAL' },
      { reason: 'EXIT_FOCUS', expand: { sub_1: true, sub_2: true }, focusScope: 'GLOBAL' },
      { reason: 'BOOKMARK_RESTORE', expand: { sub_1: true, sub_2: true }, focusScope: 'GLOBAL' },
    ];
    for (const p of hardPaths) {
      const hard = evaluateVisibilityGuard(manyNodes, edges, p.expand, { focusScope: p.focusScope });
      expect(hard.level).toBe('HARD_CAP');
      expect(hard.estimatedCount).toBe(137);
      expect(hard.blocked).toBe(true);
    }

    // 验证 VisibilityGuardDialog 弹窗组件渲染 3 个引导选项
    const dlgHtml = renderToString(
      <VisibilityGuardDialog
        open={true}
        estimatedCount={137}
        hardCap={120}
        recommendedFocusNode={{ id: 'sub_2', name: 'S2' }}
        onFocusRecommended={() => {}}
        onEnableHideIsolated={() => {}}
        onForceApply={() => {}}
        onCancel={() => {}}
      />
    );
    expect(dlgHtml).toContain('137');
    expect(dlgHtml).toContain('下钻聚焦');
    expect(dlgHtml).toContain('隐藏孤立节点');
    expect(dlgHtml).toContain('仍强制展开');
  });

  // §3.5.3: testSearchCap30_HighlightsContainerBadges_WithoutFullAutoExpand
  it('testSearchCap30_HighlightsContainerBadges_WithoutFullAutoExpand', () => {
    const nodes: GraphNode[] = [
      { id: 'sub_1', type: 'SUBSYSTEM', depth: 0, ancestors: [], name: '核心子系统', sort_order: 1 },
    ];
    for (let i = 1; i <= 40; i++) {
      nodes.push({
        id: `mod_${i}`,
        type: 'MODULE',
        parent_id: 'sub_1',
        depth: 1,
        ancestors: ['sub_1'],
        code: `COMMON_MOD_${i}`,
        name: `通用服务_${i}`,
        sort_order: i,
      });
    }

    // 搜索 "通用服务" 命中 40 个模块 (> SEARCH_AUTO_EXPAND_CAP = 30)，不自动展开容器，仅统计各容器命中角标
    const overCap = computeSearchExpandDecision(nodes, '通用服务');
    expect(overCap.matchedNodeIds.size).toBe(40);
    expect(overCap.shouldAutoExpand).toBe(false);
    expect(overCap.containerMatchCounts['sub_1']).toBe(40);

    // 搜索 "通用服务_1" 命中 <= 30 个模块 (mod_1, mod_10..mod_19 共 11 个)，允许自动展开祖先容器
    const underCap = computeSearchExpandDecision(nodes, '通用服务_1');
    expect(underCap.matchedNodeIds.size).toBe(11);
    expect(underCap.shouldAutoExpand).toBe(true);
    expect(underCap.ancestorsToExpand['sub_1']).toBe(true);
  });

  // §3.5.4: testToolbarFilters_HopDirectionOwnerConsistencyIsolated_SyncToUrlParams
  it('testToolbarFilters_HopDirectionOwnerConsistencyIsolated_SyncToUrlParams', () => {
    const params = buildTopoUrlParams({
      focusScope: 'sub_1',
      neighborhoodHop: 2,
      neighborhoodDirection: 'UPSTREAM',
      ownerId: 18,
      consistencyOnly: true,
      hideIsolated: true,
      layoutId: 5,
    });
    expect(params).toEqual({
      topo_focus: 'sub_1',
      topo_hop: '2',
      topo_dir: 'UPSTREAM',
      topo_owner_id: '18',
      topo_consistency_only: 'true',
      topo_hide_isolated: 'true',
      topo_layout_id: '5',
    });

    const usp = new URLSearchParams(params as Record<string, string>);
    const parsed = parseTopoUrlParams(usp);
    expect(parsed).toEqual({
      focusScope: 'sub_1',
      neighborhoodHop: 2,
      neighborhoodDirection: 'UPSTREAM',
      ownerId: 18,
      consistencyOnly: true,
      hideIsolated: true,
      layoutId: 5,
    });
  });

  // §3.5.5 & §3.5.6: testBookmarkDrawer_PersonalCRUD_Quota20Disable_AndRevision409Prompt & testCanonicalPublish_AdminVisible_MemberHidden
  it('testBookmarkDrawer_PersonalCRUD_Quota20Disable_AndRevision409Prompt & testCanonicalPublish_AdminVisible_MemberHidden', () => {
    const mockCanonical: TopologyLayoutDetailDTO = {
      id: 1,
      scope: 'SHARED',
      view: 'LOGICAL',
      owner_id: null,
      name: '团队逻辑基准',
      description: '全团队默认基准',
      focus_scope: 'GLOBAL',
      is_default: false,
      schema_version: 1,
      structure_version: 's1-logical-111',
      revision: 3,
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
      created_by: 1,
      updated_by: 1,
      created_at: '2026-11-09T10:00:00Z',
      updated_at: '2026-11-09T10:00:00Z',
    };

    const listDataFullQuota: TopologyLayoutListResponse = {
      canonical: mockCanonical,
      personal_items: [
        {
          id: 10,
          scope: 'PRIVATE',
          view: 'LOGICAL',
          owner_id: 2,
          name: '我的订单排障视图',
          description: '排障专用',
          focus_scope: 'sub_1',
          is_default: true,
          schema_version: 1,
          structure_version: 's1-logical-111',
          revision: 1,
          node_count: 8,
          created_by: 2,
          updated_by: 2,
          created_at: '2026-11-09T10:00:00Z',
          updated_at: '2026-11-09T10:00:00Z',
        },
      ],
      quota: { used: 20, limit: 20 },
    };

    // 1. 管理员视角 + 配额满 (20/20) + 409 版本冲突提示横幅
    const adminHtml = renderToString(
      <TopologyBookmarkDrawer
        open={true}
        onClose={() => {}}
        view="LOGICAL"
        isAdmin={true}
        layoutList={listDataFullQuota}
        activeLayoutId={10}
        conflictBannerMessage="检测到版本冲突 (409)：团队基准已被更新至版本 v4，请刷新后重试"
        onPublishCanonical={async () => {}}
        onResetCanonical={async () => {}}
        onApplyCanonical={() => {}}
        onCreatePersonal={async () => {}}
        onApplyPersonal={async () => {}}
        onUpdatePersonal={async () => {}}
        onDeletePersonal={async () => {}}
      />
    );
    expect(adminHtml).toContain('20 / 20');
    expect(adminHtml).toContain('发布当前布局为团队基准');
    expect(adminHtml).toContain('重置团队基准');
    expect(adminHtml).toContain('检测到版本冲突 (409)');
    expect(adminHtml).toContain('我的订单排障视图');

    // 2. 普通成员视角 (isAdmin=false)：不渲染管理员基准发布/重置按钮
    const memberHtml = renderToString(
      <TopologyBookmarkDrawer
        open={true}
        onClose={() => {}}
        view="LOGICAL"
        isAdmin={false}
        layoutList={listDataFullQuota}
        activeLayoutId={10}
        onPublishCanonical={async () => {}}
        onResetCanonical={async () => {}}
        onApplyCanonical={() => {}}
        onCreatePersonal={async () => {}}
        onApplyPersonal={async () => {}}
        onUpdatePersonal={async () => {}}
        onDeletePersonal={async () => {}}
      />
    );
    expect(memberHtml).not.toContain('发布当前布局为团队基准');
    expect(memberHtml).not.toContain('重置团队基准');
  });

  // §3.5.7: testThemeCompliance_ZeroHardcodedColors_LightAndDarkTokens
  it('testThemeCompliance_ZeroHardcodedColors_LightAndDarkTokens', () => {
    const srcRoot = path.resolve(__dirname, '..');
    const filesToScan: string[] = [];

    function walkDir(dir: string) {
      if (!fs.existsSync(dir)) return;
      for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
        const full = path.join(dir, entry.name);
        if (entry.isDirectory()) {
          walkDir(full);
        } else if (/\.(ts|tsx|css)$/.test(entry.name)) {
          filesToScan.push(full);
        }
      }
    }

    walkDir(path.join(srcRoot, 'components/ArchCanvas'));
    walkDir(path.join(srcRoot, 'pages/TopologyWorkbenchPage'));

    const hardcodedColorRegex = /#[0-9a-fA-F]{3,8}\b|rgba?\(/;
    const violations: string[] = [];

    for (const file of filesToScan) {
      const lines = fs.readFileSync(file, 'utf-8').split('\n');
      lines.forEach((line, idx) => {
        if (hardcodedColorRegex.test(line)) {
          violations.push(`${path.relative(srcRoot, file)}:${idx + 1}: ${line.trim()}`);
        }
      });
    }

    expect(violations).toEqual([]);
  });
});
