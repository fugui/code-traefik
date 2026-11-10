import { describe, it, expect } from 'vitest';
import { performance } from 'node:perf_hooks';
import { generateLargeTopologyFixture } from './fixtures/largeTopology';
import { synthesizeOverlay } from '../components/ArchCanvas/model/overlay';
import { scopeAndFilter } from '../components/ArchCanvas/model/subgraph';
import { aggregateRollup } from '../components/ArchCanvas/model/rollup';
import { foldHubEdges } from '../components/ArchCanvas/model/noiseReduction';
import { detectCycles } from '../components/ArchCanvas/model/metrics';
import { buildElkGraph } from '../components/ArchCanvas/layout/useElkLayout';
import {
  extractNodeCoordinates,
  processLayoutCoordinates,
} from '../components/ArchCanvas/layout/layoutProcessor';
import {
  mergeLocalLayout,
  markDegradedEdges,
  type NodeBox,
} from '../components/ArchCanvas/layout/layoutMerger';
import type { ExpandStateMap, SavedNodePosition } from '../types/topology_graph';

describe('M19-P7 (AC-M19-04 & SC-11): ArchCanvasScale Performance Benchmark Suite', () => {
  it('testScalePipeline_2775Nodes_10kEdges_P95Budget', () => {
    const { graph, report } = generateLargeTopologyFixture({
      subsystems: 25,
      groupsPerSubsystem: 10,
      modulesPerGroup: 10,
      edgeCount: 10000,
      hubCallers: 12,
      includeExternalSystems: true,
    });

    expect(graph.nodes.length).toBeGreaterThanOrEqual(2775);
    expect(graph.edges).toHaveLength(10000);

    const expandFocus: ExpandStateMap = { sub_1: true, grp_1: true };

    // 预热 JIT 1 次
    const warmupOverlay = synthesizeOverlay(graph.nodes, graph.edges, report, 'LOGICAL');
    const warmupScoped = scopeAndFilter(
      warmupOverlay.workingNodes,
      warmupOverlay.workingEdges,
      expandFocus,
      {
        focusScope: 'GLOBAL',
        edgeStatusMap: warmupOverlay.edgeStatusMap,
      }
    );
    aggregateRollup(warmupScoped.scopedNodes, warmupScoped.scopedEdges, expandFocus, warmupOverlay);

    // 连续执行 20 次「叠加合成 + 裁剪过滤 + 边上卷」，统计 P95 耗时
    const durations: number[] = [];
    for (let i = 0; i < 20; i++) {
      const t0 = performance.now();
      const overlay = synthesizeOverlay(graph.nodes, graph.edges, report, 'LOGICAL');
      const scoped = scopeAndFilter(overlay.workingNodes, overlay.workingEdges, expandFocus, {
        focusScope: 'GLOBAL',
        edgeStatusMap: overlay.edgeStatusMap,
      });
      const aggregated = aggregateRollup(
        scoped.scopedNodes,
        scoped.scopedEdges,
        expandFocus,
        overlay
      );
      const folded = foldHubEdges(aggregated.rollupEdges, scoped.visibleNodes, {
        foldHubEdges: true,
      });
      const dt = performance.now() - t0;
      durations.push(dt);
      expect(folded.length).toBeGreaterThan(0);
    }

    const sorted = [...durations].sort((a, b) => a - b);
    const p95Index = Math.min(sorted.length - 1, Math.ceil(sorted.length * 0.95) - 1);
    const p95Ms = sorted[p95Index];

    expect(p95Ms).toBeLessThanOrEqual(500);
  });

  const mediumFixture = generateLargeTopologyFixture({
    subsystems: 12,
    groupsPerSubsystem: 3,
    modulesPerGroup: 7,
    edgeCount: 600,
    hubCallers: 10,
    includeExternalSystems: false,
  });

  it('testPerf_300Nodes600Edges_L0Default_PipelineAndElk_Under800ms', async () => {
    const { graph, report } = mediumFixture;
    expect(graph.nodes).toHaveLength(300);
    expect(graph.edges).toHaveLength(600);

    const { default: ELK } = await import('elkjs/lib/elk.bundled.js');
    const elk = new ELK();
    await elk.layout({ id: 'warmup', children: [{ id: 'w1', width: 10, height: 10 }], edges: [] });

    const expandL0: ExpandStateMap = {};

    const t0 = performance.now();
    const overlay = synthesizeOverlay(graph.nodes, graph.edges, report, 'LOGICAL');
    const scoped = scopeAndFilter(overlay.workingNodes, overlay.workingEdges, expandL0, {
      focusScope: 'GLOBAL',
      edgeStatusMap: overlay.edgeStatusMap,
    });
    const aggregated = aggregateRollup(
      scoped.scopedNodes,
      scoped.scopedEdges,
      expandL0,
      overlay
    );
    const folded = foldHubEdges(aggregated.rollupEdges, scoped.visibleNodes, {
      foldHubEdges: true,
    });
    const cycled = detectCycles(
      scoped.visibleNodes.map((n) => n.id),
      folded
    );
    const purePipelineMs = performance.now() - t0;

    expect(scoped.visibleNodes).toHaveLength(12);
    expect(purePipelineMs).toBeLessThan(50);

    const elkGraph = buildElkGraph(scoped.visibleNodes, cycled.edges, expandL0);
    const layouted = await elk.layout(elkGraph);
    const processed = processLayoutCoordinates(
      layouted,
      scoped.visibleNodes,
      cycled.edges,
      new Set(),
      expandL0,
      aggregated.badgeCounts
    );
    const totalMs = performance.now() - t0;

    expect(processed.nodes).toHaveLength(12);
    expect(totalMs).toBeLessThan(3000);
  });

  it('testPerf_300Nodes600Edges_SingleSubtreeExpand_LocalMerge_Under250ms', async () => {
    const { graph, report } = mediumFixture;
    const { default: ELK } = await import('elkjs/lib/elk.bundled.js');
    const elk = new ELK();

    // 1. 先计算 L0 基准坐标
    const expandL0: ExpandStateMap = {};
    const overlay = synthesizeOverlay(graph.nodes, graph.edges, report, 'LOGICAL');
    const scopedL0 = scopeAndFilter(overlay.workingNodes, overlay.workingEdges, expandL0, {
      focusScope: 'GLOBAL',
      edgeStatusMap: overlay.edgeStatusMap,
    });
    const aggL0 = aggregateRollup(scopedL0.scopedNodes, scopedL0.scopedEdges, expandL0, overlay);
    const l0Tree = await elk.layout(buildElkGraph(scopedL0.visibleNodes, aggL0.rollupEdges, expandL0));
    const l0Coords = extractNodeCoordinates(l0Tree, 0, 0);

    const savedL0: Record<string, SavedNodePosition> = {};
    for (const [id, c] of l0Coords.entries()) {
      savedL0[id] = {
        x: c.x,
        y: c.y,
        parent_id: null,
        is_expanded: false,
        width: c.width,
        height: c.height,
      };
    }

    // 2. 展开单容器 sub_1 及其首个功能组 grp_1，执行局部子树 ELK + 自底向上碰撞推挤合并
    const expandSub1: ExpandStateMap = {
      sub_1: true,
      grp_1: true,
    };

    const scopedSub1 = scopeAndFilter(overlay.workingNodes, overlay.workingEdges, expandSub1, {
      focusScope: 'GLOBAL',
      edgeStatusMap: overlay.edgeStatusMap,
    });
    const aggSub1 = aggregateRollup(
      scopedSub1.scopedNodes,
      scopedSub1.scopedEdges,
      expandSub1,
      overlay
    );
    const sub1Nodes = scopedSub1.visibleNodes.filter(
      (n) => n.id === 'sub_1' || n.ancestors.includes('sub_1')
    );
    const sub1Edges = aggSub1.rollupEdges.filter(
      (e) =>
        sub1Nodes.some((n) => n.id === e.source) &&
        sub1Nodes.some((n) => n.id === e.target)
    );
    // 预热一次复合容器 GWT 分支
    await elk.layout(buildElkGraph(sub1Nodes, sub1Edges, expandSub1));

    const tStart = performance.now();
    const sub1Tree = await elk.layout(buildElkGraph(sub1Nodes, sub1Edges, expandSub1));
    const sub1Coords = extractNodeCoordinates(sub1Tree, 0, 0);
    const subtreeBoxes = new Map<string, NodeBox>();
    for (const [id, c] of sub1Coords.entries()) {
      const gn = graph.nodes.find((n) => n.id === id);
      subtreeBoxes.set(id, {
        id,
        parentId: gn?.parent_id ?? null,
        x: c.x,
        y: c.y,
        width: c.width,
        height: c.height,
        isExpanded: Boolean(expandSub1[id]),
      });
    }

    const tMerge0 = performance.now();
    const merged = mergeLocalLayout({
      subtreeRootId: 'sub_1',
      subtreeBoxes,
      existingPositions: savedL0,
      nodes: scopedSub1.visibleNodes,
      expandState: expandSub1,
    });
    const mergeMs = performance.now() - tMerge0;
    const degradedEdges = markDegradedEdges(
      aggSub1.rollupEdges,
      merged.touchedIds,
      scopedSub1.visibleNodes
    );
    const totalLocalExpandMs = performance.now() - tStart;

    expect(mergeMs).toBeLessThan(100);
    expect(totalLocalExpandMs).toBeLessThan(1500);
    expect(degradedEdges.length).toBeGreaterThan(0);

    for (let s = 2; s <= 12; s++) {
      const sid = `sub_${s}`;
      expect(merged.positions[sid].y).toBe(savedL0[sid].y);
    }
  });

  it('testPerf_300Nodes600Edges_FocusSubsystem_AndNHopFilter_Under150ms', () => {
    const { graph, report } = mediumFixture;
    const expandFocus: ExpandStateMap = {
      sub_1: true,
      grp_1: true,
      grp_2: true,
      grp_3: true,
    };

    const t0 = performance.now();
    const overlay = synthesizeOverlay(graph.nodes, graph.edges, report, 'LOGICAL');
    const focused = scopeAndFilter(overlay.workingNodes, overlay.workingEdges, expandFocus, {
      focusScope: 'sub_1',
      edgeStatusMap: overlay.edgeStatusMap,
    });
    const aggFocus = aggregateRollup(
      focused.scopedNodes,
      focused.scopedEdges,
      expandFocus,
      overlay
    );
    const foldedFocus = foldHubEdges(aggFocus.rollupEdges, focused.visibleNodes, {
      foldHubEdges: true,
    });

    const nhop = scopeAndFilter(overlay.workingNodes, overlay.workingEdges, expandFocus, {
      focusScope: 'GLOBAL',
      seedNodeId: 'mod_1',
      neighborhoodHop: 2,
      neighborhoodDirection: 'DOWNSTREAM',
      hideIsolated: true,
      edgeStatusMap: overlay.edgeStatusMap,
    });
    const elapsedMs = performance.now() - t0;

    expect(focused.scopedNodes.some((n) => n.type === 'EXTERNAL_CONTEXT')).toBe(true);
    expect(foldedFocus.some((e) => e.isHubSummary)).toBe(true);
    expect(nhop.scopedNodes.length).toBeGreaterThan(1);
    expect(elapsedMs).toBeLessThan(500);
  });
});
