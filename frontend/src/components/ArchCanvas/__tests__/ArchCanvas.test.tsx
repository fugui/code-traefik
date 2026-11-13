import { describe, it, expect } from 'vitest';
import { renderToString } from 'react-dom/server';
import { ReactFlowProvider } from '@xyflow/react';
import { ModuleNode } from '../nodes/ModuleNode';
import { resolveEdgeVisualStyle } from '../layout/layoutProcessor';
import { detectCycles } from '../model/metrics';
import { synthesizeOverlay } from '../model/overlay';
import { ArchCanvasToolbar } from '../panels/ArchCanvasToolbar';
import { ArchSummaryBar } from '../panels/ArchSummaryBar';
import type { GraphNode, GraphEdge, RollupEdgeModel } from '../../../types/topology_graph';
import type { ConsistencyReportDTO } from '../../../types/traffic';

describe('AC-M20-04 ArchCanvas Multi-Implementation Service Contract Tests', () => {
  it('renders CONTRACT_LIB node with «interface» badge, dashed style and contract/impl counters', () => {
    const contractLibNode: GraphNode = {
      id: 'mod_contract_lib',
      type: 'MODULE',
      name: '支付通用接口契约库',
      code: 'LIB_PAY_CONTRACT',
      depth: 2,
      ancestors: ['sub_1', 'grp_1'],
      sort_order: 1,
      meta: {
        module_kind: 'CONTRACT_LIB',
        contract_count: 4,
        impl_count: 7,
        owner_name: '架构组',
      },
    };

    const nodeProps = {
      id: 'mod_contract_lib',
      data: {
        graphNode: contractLibNode,
        isPinned: false,
        isExpanded: false,
        lodMode: 'STANDARD' as const,
      },
      selected: false,
      type: 'moduleCard' as const,
      zIndex: 1,
      isConnectable: false,
      positionAbsoluteX: 0,
      positionAbsoluteY: 0,
      dragging: false,
      draggable: false,
      selectable: false,
      deletable: false,
    };

    const html = renderToString(
      <ReactFlowProvider>
        <ModuleNode {...nodeProps} />
      </ReactFlowProvider>
    );

    expect(html).toContain('data-testid="interface-badge"');
    expect(html).toContain('«interface»');
    expect(html).toContain('code-arch-module--contract-lib');
    expect(html).toContain('4 契约 / 7 实现');
    expect(html).toContain('LIB_PAY_CONTRACT');
  });

  it('resolves visual styles for IMPL_UNRESOLVED and REALIZATION edges and isolates REALIZATION from cycles', () => {
    // 1. 样式映射验证
    const unresolvedStyle = resolveEdgeVisualStyle({ status: 'IMPL_UNRESOLVED' });
    expect(unresolvedStyle.strokeColor).toBe('var(--color-warning)');
    expect(unresolvedStyle.strokeDasharray).toBe('8 4');
    expect(unresolvedStyle.statusClass).toBe('code-arch-edge--impl_unresolved');

    const realizationStyle = resolveEdgeVisualStyle({ kind: 'REALIZATION' });
    expect(realizationStyle.strokeColor).toBe('var(--color-text-muted)');
    expect(realizationStyle.strokeDasharray).toBe('4 4');
    expect(realizationStyle.statusClass).toBe('code-arch-edge--realization');

    // 2. REALIZATION 边不参与环路检测 (即使存在 A -> B 调用, B -> A 实现，也不报环)
    const rollupEdges: RollupEdgeModel[] = [
      {
        id: 'edge_call',
        source: 'mod_a',
        target: 'mod_b',
        kind: 'CROSS_PROCESS',
        services: [],
        rawEdges: [],
      },
      {
        id: 'edge_realize',
        source: 'mod_b',
        target: 'mod_a',
        kind: 'REALIZATION',
        services: [],
        rawEdges: [],
      },
    ];

    const cycleResult = detectCycles(['mod_a', 'mod_b'], rollupEdges);
    expect(cycleResult.cycleNodeIds.size).toBe(0);
    expect(cycleResult.edges.every((e) => !e.isCycle)).toBe(true);
  });

  it('synthesizes IMPL_UNRESOLVED overlay stains in logical view and spawns virtual nodes in physical view', () => {
    const report: ConsistencyReportDTO = {
      session_id: '1',
      verified_count: 0,
      outbound_verified_count: 0,
      violation_count: 0,
      unattributed_count: 0,
      uncovered_count: 0,
      not_monitored_count: 0,
      not_observable_count: 0,
      impl_unresolved_count: 1,
      edges: [
        {
          src_module_id: 10,
          dst_module_id: 20,
          src_process_id: 100,
          service_name: 'dophi.pay.v1.PayService',
          operation: 'Pay',
          category: 'IMPL_UNRESOLVED',
          status: 'IMPL_UNRESOLVED',
          call_count: 5,
        },
      ],
    };

    // 逻辑视图: 原边状态被记录在 edgeStatusMap 中
    const logicalEdges: GraphEdge[] = [
      {
        id: 'edge_10_20',
        src_module_id: 10,
        dst_module_id: 20,
        source_node_id: 'mod_10',
        target_node_id: 'mod_20',
        kind: 'CROSS_PROCESS',
        services: [{ service_name: 'dophi.pay.v1.PayService' }],
      },
    ];
    const logicalOverlay = synthesizeOverlay(
      [
        { id: 'mod_10', type: 'MODULE', name: '模块10', depth: 2, ancestors: [], sort_order: 1, entity_id: 10 },
        { id: 'mod_20', type: 'MODULE', name: '模块20', depth: 2, ancestors: [], sort_order: 2, entity_id: 20 },
      ],
      logicalEdges,
      report,
      'LOGICAL'
    );
    expect(logicalOverlay.edgeStatusMap.get('edge_10_20')).toBe('IMPL_UNRESOLVED');

    // 物理视图: 生成 VIRTUAL_IMPL_UNRESOLVED 虚拟节点与边
    const physicalOverlay = synthesizeOverlay(
      [{ id: 'proc_100', type: 'PROCESS', name: '进程100', depth: 1, ancestors: [], sort_order: 1, entity_id: 100 }],
      [],
      report,
      'PHYSICAL'
    );
    const virtualNode = physicalOverlay.dynamicNodes.find((n) => n.id.startsWith('VIRTUAL_IMPL_UNRESOLVED'));
    expect(virtualNode).toBeDefined();
    expect(virtualNode?.name).toContain('实现待定');

    const virtualEdge = physicalOverlay.workingEdges.find((e) => e.target_node_id === virtualNode?.id);
    expect(virtualEdge).toBeDefined();
    expect(physicalOverlay.edgeStatusMap.get(virtualEdge!.id)).toBe('IMPL_UNRESOLVED');
  });

  it('renders ArchCanvasToolbar with showRealization toggle and AGG/EXPAND contract mode toggles', () => {
    const noop = () => {};
    const html = renderToString(
      <ArchCanvasToolbar
        view="LOGICAL"
        onViewChange={noop}
        depthLevel={2}
        maxDepth={3}
        onDepthLevelChange={noop}
        wheelAutoExpand={false}
        onToggleWheelAutoExpand={noop}
        onResetLayout={noop}
        searchKeyword=""
        onSearchChange={noop}
        sessions={[]}
        selectedSessionId={null}
        onSelectSession={noop}
        stats={{
          subsystems: 1,
          functionalGroups: 1,
          modules: 2,
          processGroups: 1,
          processes: 1,
          externals: 0,
          unreg: 0,
          edgeCount: 1,
        }}
        showRealization={true}
        onToggleShowRealization={noop}
        contractMode="AGG"
        onContractModeChange={noop}
      />
    );

    expect(html).toContain('data-testid="toggle-realization"');
    expect(html).toContain('data-testid="contract-mode-toggle"');
    expect(html).toContain('data-testid="contract-mode-agg"');
    expect(html).toContain('data-testid="contract-mode-expand"');
    expect(html).toContain('聚合到接口');
    expect(html).toContain('展开到实现');
    expect(html).toContain('实现关系');
  });

  it('renders ArchSummaryBar with 8th status item: 实现待定 (N)', () => {
    const mockReport: ConsistencyReportDTO = {
      session_id: '100',
      verified_count: 5,
      outbound_verified_count: 1,
      violation_count: 1,
      unattributed_count: 0,
      uncovered_count: 1,
      not_monitored_count: 0,
      not_observable_count: 0,
      impl_unresolved_count: 3,
      edges: [],
      stats: {
        VERIFIED: 5,
        OUTBOUND_VERIFIED: 1,
        VIOLATION: 1,
        UNATTRIBUTED: 0,
        UNCOVERED: 1,
        NOT_MONITORED: 0,
        NOT_OBSERVABLE: 0,
        IMPL_UNRESOLVED: 3,
      },
    };

    const html = renderToString(
      <ArchSummaryBar report={mockReport} unattributedPendingList={[]} />
    );

    expect(html).toContain('data-testid="summary-item-impl-unresolved"');
    expect(html).toContain('实现待定');
    expect(html).toContain('3');
  });

  it('stains AGG static edge in LOGICAL view when consistency edge matches by contract_id for custom published service_name (ADR-16 v2.0)', () => {
    const callerNode: GraphNode = {
      id: 'mod_1',
      type: 'MODULE',
      name: '调用方网关',
      code: 'MOD_CALLER',
      depth: 2,
      ancestors: ['sub_1', 'grp_1'],
      sort_order: 1,
      meta: { module_kind: 'BUSINESS' },
    };
    const contractLibNode: GraphNode = {
      id: 'mod_2',
      type: 'MODULE',
      name: '同步公共契约库',
      code: 'LIB_SYNC',
      depth: 2,
      ancestors: ['sub_1', 'grp_1'],
      sort_order: 2,
      meta: { module_kind: 'CONTRACT_LIB' },
    };
    const aggStaticEdge: GraphEdge = {
      id: 'e_agg_sync',
      kind: 'CROSS_PROCESS',
      source_node_id: 'mod_1',
      target_node_id: 'mod_2',
      src_module_id: 1,
      dst_module_id: 2,
      definer_module_id: 2,
      contract_id: 100,
      services: [
        {
          service_name: 'SyncService',
          contract_id: 100,
        },
      ],
    };

    const report: ConsistencyReportDTO = {
      session_id: 'sess-adr16-v2',
      edges: [
        {
          src_module_id: 1,
          src_module_name: '调用方网关',
          dst_module_id: 3,
          dst_module_name: 'DH 同步实现',
          service_name: 'DH_SyncService',
          operation: 'DoSync',
          contract_id: 100,
          contract_service_name: 'SyncService',
          category: 'VERIFIED',
          status: 'VERIFIED',
          call_count: 42,
          sample_traffic_record_id: 7,
        },
      ],
    };

    const result = synthesizeOverlay([callerNode, contractLibNode], [aggStaticEdge], report, 'LOGICAL');
    expect(result.edgeStatusMap.get('e_agg_sync')).toBe('VERIFIED');
    expect(result.edgeCallCountMap.get('e_agg_sync')).toBe(42);
    expect(result.edgeSampleRecordMap.get('e_agg_sync')).toBe(7);
    expect(result.workingEdges).toHaveLength(1);
  });
});
