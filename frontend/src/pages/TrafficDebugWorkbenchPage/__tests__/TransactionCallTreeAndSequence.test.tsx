import { describe, it, expect } from 'vitest';
import { renderToString } from 'react-dom/server';
import type {
  TransactionTreeGroupDTO,
  TransactionSequenceDTO,
  TrafficRecordSummary,
  Process,
  Agent,
} from '../../../types';
import { TransactionCallTreeTable } from '../TransactionCallTreeTable';
import { InlineWaterfallBar } from '../InlineWaterfallBar';
import { DebugSessionContextBar } from '../DebugSessionContextBar';
import { TrafficRecordTable } from '../TrafficRecordTable';
import { EntityLifecycleFilterStrip } from '../EntityLifecycleFilterStrip';
import { SequenceSwimlaneCanvas } from '../SequenceSwimlaneCanvas';
import { SinglePacketReplayContent } from '../SinglePacketReplayDrawer';

describe('M13 Frontend Test Suite: Transaction Call Tree and Sequence Workbench', () => {
  const mockRootSummary: TrafficRecordSummary = {
    id: '101',
    session_id: '1',
    seq_no: 1,
    process_id: '10',
    process_name: 'Proc_Order',
    peer_proc_name: 'api-gw',
    conn_key: '127.0.0.1:40001->127.0.0.1:8091',
    conn_index: 1,
    direction: 'INGRESS',
    exchange_type: 'UNARY',
    sno: '1001',
    service_name: 'dophi.trade.v1.TradeService',
    operation: 'CreateOrder',
    latency_us: 45000,
    status_code: 0,
    biz_error: false,
    trace_id: 'trace-m13-001',
    span_id: 'span-root',
    father_span_id: '',
    decode_status: 'SUCCESS',
    is_replayable: true,
  };

  const mockChildLock: TrafficRecordSummary = {
    id: '102',
    session_id: '1',
    seq_no: 2,
    process_id: '10',
    process_name: 'Proc_Order',
    conn_key: '127.0.0.1:50001->127.0.0.1:8092',
    conn_index: 2,
    direction: 'EGRESS',
    exchange_type: 'UNARY',
    sno: '2001',
    service_name: 'dophi.inventory.v1.InventoryService',
    operation: 'LockStock',
    latency_us: 15000,
    status_code: '500',
    biz_error: true,
    biz_status_summary: 'code=5001',
    trace_id: 'trace-m13-001',
    span_id: 'span-lock',
    father_span_id: 'span-root',
    decode_status: 'SUCCESS',
    is_replayable: false,
  };

  const mockChildComp: TrafficRecordSummary = {
    id: '103',
    session_id: '1',
    seq_no: 3,
    process_id: '10',
    process_name: 'Proc_Order',
    conn_key: '127.0.0.1:50001->127.0.0.1:8092',
    conn_index: 2,
    direction: 'EGRESS',
    exchange_type: 'UNARY',
    sno: '2002',
    service_name: 'dophi.inventory.v1.InventoryService',
    operation: 'ReleaseStock',
    latency_us: 10000,
    status_code: 0,
    biz_error: false,
    trace_id: 'trace-m13-001',
    span_id: 'span-release',
    father_span_id: 'span-root',
    decode_status: 'SUCCESS',
    is_replayable: false,
  };

  const mockStitchedIngress: TrafficRecordSummary = {
    id: '104',
    session_id: '1',
    seq_no: 2,
    process_id: '12',
    process_name: 'Proc_Inventory',
    conn_key: '127.0.0.1:50001->127.0.0.1:8092',
    conn_index: 1,
    direction: 'INGRESS',
    exchange_type: 'UNARY',
    sno: '2001',
    service_name: 'dophi.inventory.v1.InventoryService',
    operation: 'LockStock',
    latency_us: 12000,
    status_code: '500',
    biz_error: true,
    trace_id: 'trace-m13-001',
    span_id: 'span-lock',
    father_span_id: 'span-root',
    decode_status: 'SUCCESS',
    is_replayable: false,
  };

  const mockTreeGroup: TransactionTreeGroupDTO = {
    root_record_id: '101',
    trace_id: 'trace-m13-001',
    total_node_count: 3,
    truncated_children_count: 0,
    total_duration_us: 45000,
    has_protocol_error: true,
    has_biz_error: true,
    has_compensation: true,
    entity_tags: ['order_id=ORD_9981'],
    root: {
      record: mockRootSummary,
      depth: 0,
      link_confidence: 'ROOT',
      sibling_relation: 'ROOT',
      waterfall_offset_pct: 0,
      waterfall_width_pct: 100,
      is_critical_path: true,
      matched_filter: false,
      children: [
        {
          record: mockChildLock,
          stitched_ingress_record: mockStitchedIngress,
          net_hop_us: 3000,
          depth: 1,
          link_confidence: 'DOTR_EXACT',
          sibling_relation: 'SEQUENTIAL',
          waterfall_offset_pct: 20,
          waterfall_width_pct: 33.3,
          is_critical_path: false,
          matched_filter: true,
          children: [],
        },
        {
          record: mockChildComp,
          depth: 1,
          link_confidence: 'DOTR_EXACT',
          sibling_relation: 'COMPENSATION',
          waterfall_offset_pct: 60,
          waterfall_width_pct: 22.2,
          is_critical_path: false,
          matched_filter: false,
          children: [],
        },
      ],
    },
  };

  // T-M13-11: 默认树表渲染、甘特瀑布、平铺切换与 SVG 时序泳道
  it('T-M13-11: 渲染调用树表、甘特瀑布条、视图切换开关与原生 SVG 泳道图', () => {
    // 1. 验证 InlineWaterfallBar 纯动态样式与语义变量
    const waterfallHtml = renderToString(
      <InlineWaterfallBar
        offsetPct={25.5}
        widthPct={40.2}
        durationUs={15000}
        relation="PARALLEL"
        hasError={false}
      />
    );
    expect(waterfallHtml).toContain('left:25.5%');
    expect(waterfallHtml).toContain('width:40.2%');
    expect(waterfallHtml).toContain('var(--color-info');

    // 2. 渲染调用树表格
    const treeHtml = renderToString(
      <TransactionCallTreeTable
        groups={[mockTreeGroup]}
        onSelectRecordForAnatomy={() => {}}
        onOpenSequenceDrawer={() => {}}
        onSinglePacketReplay={() => {}}
        forceExpandAll={true}
      />
    );
    // 断言根事务与子调用发起序号
    expect(treeHtml).toContain('#1');
    expect(treeHtml).toContain('#2');
    expect(treeHtml).toContain('#3');
    expect(treeHtml).toContain('Conn#1 · 1001');
    // 断言跨跳缝合徽章
    expect(treeHtml).toContain('🔗 Proc_Order ➔ Proc_Inventory');
    // 断言兄弟补偿标签
    expect(treeHtml).toContain('🔄 逆向补偿');
    // 断言双态状态码
    expect(treeHtml).toContain('0 OK');
    expect(treeHtml).toContain('Err: 500');
    expect(treeHtml).toContain('⚠️ 业务异常 (code=5001)');
    // 断言搜索命中高亮行
    expect(treeHtml).toContain('highlight-matched');

    // 3. 渲染顶部批次上下文导航条分段开关
    const barHtml = renderToString(
      <DebugSessionContextBar
        currentSessionId="1"
        sessions={[{ id: '1', session_no: 'S1', name: '批次1', scope: 'SHARED', status: 'COMPLETED', target_process_ids: [], max_duration_sec: 300, max_frames: 1000, captured_frames: 10 }]}
        onSelectSession={() => {}}
        liveStreamActive={false}
        onToggleLiveStream={() => {}}
        isPaused={false}
        onTogglePause={() => {}}
        viewMode="tree"
        onChangeViewMode={() => {}}
      />
    );
    expect(barHtml).toContain('🌳 事务调用树');
    expect(barHtml).toContain('📋 原始时序平铺');
    expect(barHtml).toContain('switch-view-tree');
    expect(barHtml).toContain('switch-view-flat');

    // 4. 渲染平铺时序表
    const flatHtml = renderToString(
      <TrafficRecordTable
        records={[mockRootSummary, mockChildLock]}
        onSelectRecord={() => {}}
      />
    );
    expect(flatHtml).toContain('#Seq 发起序');
    expect(flatHtml).toContain('Conn#1 · 1001');
    expect(flatHtml).toContain('Conn#2 · 2001');

    // 5. 渲染纯原生 React SVG 时序泳道
    const mockSequence: TransactionSequenceDTO = {
      root_record_id: '101',
      trace_id: 'trace-m13-001',
      total_duration_us: 45000,
      participants: [
        {
          participant_key: 'caller:client',
          title: 'api-gw',
          subtitle: '127.0.0.1:40001',
          kind: 'EXTERNAL_CALLER',
        },
        {
          participant_key: 'proc:10',
          title: 'Proc_Order',
          subtitle: '127.0.0.1:8091',
          kind: 'MONITORED_PROCESS',
        },
        {
          participant_key: 'proc:12',
          title: 'Proc_Inventory',
          subtitle: '127.0.0.1:8092',
          kind: 'MONITORED_PROCESS',
        },
      ],
      steps: [
        {
          step_index: 1,
          record: mockRootSummary,
          from_participant: 'caller:client',
          to_participant: 'proc:10',
          sibling_relation: 'ROOT',
          offset_us: 0,
          duration_us: 45000,
          stage_timing: {
            server_exec_us: 40000,
          },
        },
        {
          step_index: 2,
          record: mockChildLock,
          stitched_ingress_record: mockStitchedIngress,
          from_participant: 'proc:10',
          to_participant: 'proc:12',
          sibling_relation: 'SEQUENTIAL',
          offset_us: 2000,
          duration_us: 15000,
          replay_overlay: {
            status: 'SCOPED_HIT',
          },
        },
      ],
      parameter_lineages: [
        {
          from_record_id: '101',
          from_side: 'REQ',
          from_field_path: 'req.sku_id',
          to_record_id: '102',
          to_side: 'REQ',
          to_field_path: 'req.sku_id',
          sample_value: 'SKU_888',
        },
      ],
      entity_chains: [
        {
          entity_key: 'order_id:ORD_9981',
          field_name: 'order_id',
          entity_value: 'ORD_9981',
          steps: [{ root_record_id: '101', seq_no: 1, operation: 'CreateOrder', role: 'PRODUCER' }],
        },
      ],
    };

    const svgHtml = renderToString(
      <SequenceSwimlaneCanvas
        sequence={mockSequence}
        selectedStepIndex={1}
        onSelectStep={() => {}}
      />
    );
    expect(svgHtml).toContain('data-testid="sequence-svg-canvas"');
    expect(svgHtml).toContain('<svg');
    expect(svgHtml).toContain('api-gw');
    expect(svgHtml).toContain('Proc_Order');
    expect(svgHtml).toContain('Proc_Inventory');
    expect(svgHtml).toContain('CreateOrder');
    expect(svgHtml).toContain('[✅ SCOPED_HIT 精确匹配]');
    expect(svgHtml).toContain('data-testid="toggle-show-lineage"');
    expect(svgHtml).toContain('SKU_888');
  });

  // T-M13-12: 事务闭包重放抽屉、前置依赖链勾选回放与 XSS 安全防御
  it('T-M13-12: 闭包专属下游 Mock 展示、前置有状态依赖提示卡与 XSS 安全转义防护', () => {
    const mockProcs: Process[] = [
      {
        id: 'p-order-1',
        name: 'Proc_Order_SUT',
        listen_port: 8091,
        agent_id: 'agent-1',
        process_modules: [{ process_id: 'p-order-1', module_id: '1', module: { id: '1', name: 'OrderModule', code: 'order' } }],
      },
    ];
    const mockAgents: Agent[] = [
      {
        id: 'agent-1',
        name: 'Agent-Dev',
        endpoint: 'http://127.0.0.1:9091',
        scope: 'PRIVATE',
        owner_id: '1',
        status: 'ONLINE',
        lease_type: '',
      },
    ];

    // 1. 注入包含恶意 XSS 脚本的方法名与实体键值
    const maliciousRecord: TrafficRecordSummary = {
      id: '105',
      session_id: '1',
      seq_no: 5,
      process_id: '10',
      direction: 'INGRESS',
      exchange_type: 'UNARY',
      sno: '1005',
      service_name: '<script>evil(1)</script>',
      operation: 'CancelOrder<svg onload=evil(2)>',
      latency_us: 2000,
      status_code: 0,
      decode_status: 'SUCCESS',
      is_replayable: true,
    };

    const html = renderToString(
      <SinglePacketReplayContent
        record={maliciousRecord}
        allProcesses={mockProcs}
        allAgents={mockAgents}
        currentUserId={1}
      />
    );

    // 断言纯文本安全转义，绝不可执行
    expect(html).toContain('&lt;script&gt;evil(1)&lt;/script&gt;');
    expect(html).toContain('CancelOrder&lt;svg onload=evil(2)&gt;');
    expect(html).not.toContain('<script>evil(1)</script>');
    expect(html).not.toContain('<svg onload=evil(2)>');

    // 断言闭包预览卡片渲染
    expect(html).toContain('data-testid="scoped-mocks-card"');
    expect(html).toContain('专属下游 ScopedMock 闭包预览 (RecordID 隔离)');

    // 2. 渲染实体链胶囊过滤条
    const stripHtml = renderToString(
      <EntityLifecycleFilterStrip
        chains={[
          {
            entity_key: 'order_id:ORD_9981',
            field_name: 'order_id',
            entity_value: 'ORD_9981',
            steps: [
              { root_record_id: '101', seq_no: 1, operation: 'CreateOrder', role: 'PRODUCER' },
              { root_record_id: '105', seq_no: 5, operation: 'CancelOrder', role: 'CONSUMER' },
            ],
          },
        ]}
        selectedEntityKey={undefined}
        onSelectEntityKey={() => {}}
      />
    );
    expect(stripHtml).toContain('order_id=ORD_9981');
    expect(stripHtml).toContain('2 笔');
  });
});
