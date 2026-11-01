import { describe, it, expect } from 'vitest';
import { renderToString } from 'react-dom/server';
import type { TrafficRecordSummary, Process, Agent, ReplayItem } from '../types';
import { SinglePacketReplayContent } from '../pages/TrafficDebugWorkbenchPage/SinglePacketReplayDrawer';
import {
  HeaderAssertionCard,
  ProtoFieldDiffTable,
  MockHitMissTimeline,
} from '../pages/ReplayWorkbenchPage/ReplayReportDrawer';

export function testSinglePacketReplay_IsomorphicFilterAndInlineDiff(): void {
  // 1. 构造报文数据：1 条可重放入站报文 (INGRESS) + 1 条出站报文 (EGRESS)
  const ingressRecord: TrafficRecordSummary = {
    id: 'rec-ingress-01',
    session_id: 'sess-01',
    process_id: 'p-source-01',
    direction: 'INGRESS',
    exchange_type: 'UNARY',
    sno: '10001',
    service_name: 'order.OrderService',
    operation: 'CreateOrder',
    latency_us: 1500,
    status_code: 0,
    decode_status: 'SUCCESS',
    dst_module_name: 'M-Order',
    src_module_name: 'M-Gateway',
    is_replayable: true,
    captured_at: '2026-11-01T08:00:00Z',
  };

  const egressRecord: TrafficRecordSummary = {
    id: 'rec-egress-02',
    session_id: 'sess-01',
    process_id: 'p-source-01',
    direction: 'EGRESS',
    exchange_type: 'UNARY',
    sno: '10002',
    service_name: 'pay.PayService',
    operation: 'PrePay',
    latency_us: 2000,
    status_code: 0,
    decode_status: 'SUCCESS',
    dst_module_name: 'M-Pay',
    src_module_name: 'M-Order',
    is_replayable: false,
    captured_at: '2026-11-01T08:00:01Z',
  };

  expect(ingressRecord.direction === 'INGRESS' && ingressRecord.is_replayable).toBe(true);
  expect(egressRecord.direction === 'INGRESS' && egressRecord.is_replayable).toBe(false);

  // 2. 构造进程与 Agent 数据
  const mockAgents: Agent[] = [
    {
      id: 'a-priv',
      name: 'Agent-Private-Alice',
      endpoint: 'http://127.0.0.1:9091',
      scope: 'PRIVATE',
      owner_id: '100',
      status: 'ONLINE',
      lease_type: '',
    },
    {
      id: 'a-shared',
      name: 'Agent-Shared-Prod',
      endpoint: 'http://127.0.0.1:9092',
      scope: 'SHARED',
      status: 'ONLINE',
      lease_type: '',
    },
  ];

  const mockProcesses: Process[] = [
    {
      id: 'p-order-priv',
      agent_id: 'a-priv',
      name: 'P-Order-Private',
      listen_port: 9100,
      group_name: '订单服务组',
      process_modules: [
        {
          process_id: 'p-order-priv',
          module_id: 'm-order',
          module: {
            id: 'm-order',
            code: 'MOD_ORDER',
            name: 'M-Order',
          },
        },
      ],
    },
    {
      id: 'p-client',
      agent_id: 'a-priv',
      name: 'P-Client',
      listen_port: 0, // 纯客户端进程，端口为 0
      group_name: '客户端组',
      process_modules: [
        {
          process_id: 'p-client',
          module_id: 'm-order',
          module: {
            id: 'm-order',
            code: 'MOD_ORDER',
            name: 'M-Order',
          },
        },
      ],
    },
    {
      id: 'p-pay',
      agent_id: 'a-shared',
      name: 'P-Pay',
      listen_port: 9200,
      group_name: '支付服务组',
      process_modules: [
        {
          process_id: 'p-pay',
          module_id: 'm-pay',
          module: {
            id: 'm-pay',
            code: 'MOD_PAY',
            name: 'M-Pay',
          },
        },
      ],
    },
  ];

  // 3. 渲染 SinglePacketReplayContent 并断言同构服务端进程过滤与排序
  const contentHtml = renderToString(
    <SinglePacketReplayContent
      record={ingressRecord}
      allProcesses={mockProcesses}
      allAgents={mockAgents}
      currentUserId={100}
    />
  );

  expect(contentHtml).toContain('P-Order-Private');
  expect(contentHtml).not.toContain('P-Client');
  expect(contentHtml).not.toContain('P-Pay');
  expect(contentHtml).toContain('推荐个人调试');

  // 4. 原地双层 Diff 与协议头断言卡片渲染
  const mockReplayItem: ReplayItem = {
    id: 'item-01',
    task_id: 'task-01',
    record_id: ingressRecord.id,
    sno: '10001',
    service_name: 'order.OrderService',
    operation: 'CreateOrder',
    header_diff_passed: true,
    baseline_status_code: 0,
    actual_status_code: 0,
    latency_us: 1200,
    result_status: 'PASS',
    field_diffs: [
      {
        field_path: 'amount_cents',
        diff_kind: 'MODIFIED',
        expected: '1000',
        actual: '1050',
      },
    ],
    mock_trace: {
      sno: '20001',
      service_name: 'stock.StockService',
      operation: 'Deduct',
      hit_kind: 'EXACT_HASH',
      is_miss: false,
    },
  };

  const inlineDiffHtml = renderToString(
    <div>
      <HeaderAssertionCard item={mockReplayItem} />
      <ProtoFieldDiffTable fieldDiffs={mockReplayItem.field_diffs} />
      <MockHitMissTimeline itemMockTrace={mockReplayItem.mock_trace} />
    </div>
  );

  expect(inlineDiffHtml).toContain('头层比对通过 (PASS)');
  expect(inlineDiffHtml).toContain('amount_cents');
  expect(inlineDiffHtml).toContain('MODIFIED');
  expect(inlineDiffHtml).toContain('stock.StockService');
  expect(inlineDiffHtml).toContain('EXACT_HASH');
}

describe('SinglePacketReplay Test Suite', () => {
  it('TC-REPLAY-SINGLE-01: 同构进程智能过滤、入站重放动作与原地 Diff 渲染断言', () => {
    testSinglePacketReplay_IsomorphicFilterAndInlineDiff();
  });
});
