import { describe, it, expect } from 'vitest';
import { renderToString } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import type {
  Agent,
  ArchProcess,
  GoldenSuite,
  Process,
  TrafficRecordSummary,
} from '../types';
import { GoldenCurateDrawer } from '../pages/TrafficDebugWorkbenchPage/GoldenCurateDrawer';
import { GoldenSuiteCreateModal } from '../pages/GoldenSuitesWorkbenchPage/GoldenSuiteCreateModal';
import { GoldenSuiteExecuteModal } from '../pages/GoldenSuitesWorkbenchPage/GoldenSuiteExecuteModal';
import { GoldenSuiteDetailDrawer } from '../pages/GoldenSuitesWorkbenchPage/GoldenSuiteDetailDrawer';
import { GoldenSuitesWorkbenchPage } from '../pages/GoldenSuitesWorkbenchPage/GoldenSuitesWorkbenchPage';

export function testGoldenCurateDrawer_RenderingAndSelfContainedSnapshot(): void {
  const dummyRecord: TrafficRecordSummary = {
    id: '101',
    session_id: '1',
    process_id: '10',
    direction: 'INGRESS',
    exchange_type: 'UNARY',
    sno: '10001',
    service_name: 'order.OrderService',
    operation: 'CreateOrder',
    latency_us: 1200,
    status_code: 0,
    decode_status: 'SUCCESS',
    dst_module_name: 'M-Order',
    src_module_name: 'M-Gateway',
    is_replayable: true,
    captured_at: '2026-11-05T12:00:00Z',
  };

  const dummyProcess: Process = {
    id: '10',
    agent_id: '1',
    name: 'OrderProcess',
    listen_port: 8080,
    arch_process_id: '100',
  };

  const dummyArchProcess: ArchProcess = {
    id: 100,
    group_id: 1,
    code: 'ORDER_SRV',
    name: '订单核心服务进程',
    default_listen_port: 8080,
    default_cmd_pattern: './order_srv',
    sort_order: 1,
  };

  const html = renderToString(
    <GoldenCurateDrawer
      open={true}
      onClose={() => {}}
      record={dummyRecord}
      sessionId={1}
      allProcesses={[dummyProcess]}
      archProcesses={[dummyArchProcess]}
      relatedRecordIds={[102, 103]}
    />
  );

  // 1. 抽屉必须正确挂载且具备 data-testid="golden-curate-drawer"
  expect(html).toContain('golden-curate-drawer');
  expect(html).toContain('沉淀为黄金契约用例快照 (完全自包含)');

  // 2. 具备 ADR-14 完全自包含快照保障文案
  expect(html).toContain('完全自包含快照保障 (ADR-14)');
  expect(html).toContain('即使底层流量分区超过 30 天被物理清理');

  // 3. 关联事务树联动勾选
  expect(html).toContain('连带前置依赖事务链一并固化');

  // 4. 服务方法与入站快照信息展示
  expect(html).toContain('order.OrderService');
  expect(html).toContain('CreateOrder');
  expect(html).toContain('订单核心服务进程');
}

export function testGoldenSuiteCreateAndExecuteModals(): void {
  const dummyArchProcesses: ArchProcess[] = [
    {
      id: 100,
      group_id: 1,
      code: 'PAY_SRV',
      name: '支付网关核心进程',
      default_listen_port: 8080,
      default_cmd_pattern: './pay_srv',
      sort_order: 1,
    },
  ];

  // 1. 测试创建模态窗渲染
  const createHtml = renderToString(
    <GoldenSuiteCreateModal
      open={true}
      onClose={() => {}}
      archProcesses={dummyArchProcesses}
      onCreated={() => {}}
    />
  );
  expect(createHtml).toContain('新建黄金契约测试套件');
  expect(createHtml).toContain('支付网关核心进程');
  expect(createHtml).toContain('套件编码 (全局唯一)');

  // 2. 测试脱机回归执行模态窗候选 Agent 过滤
  const dummySuite: GoldenSuite = {
    id: 1,
    code: 'GS_PAY_CORE',
    name: '支付契约回归套件',
    arch_process_id: 100,
    version: 'v1.0.0',
    status: 'ACTIVE',
    item_count: 5,
  };

  const dummyAgents: Agent[] = [
    {
      id: '1',
      name: 'Agent-Dev-01',
      endpoint: '127.0.0.1:9091',
      scope: 'SHARED',
      status: 'ONLINE',
      lease_type: '',
    },
    {
      id: '2',
      name: 'Agent-Dev-02',
      endpoint: '127.0.0.1:9092',
      scope: 'PRIVATE',
      status: 'ONLINE',
      lease_type: '',
    },
  ];

  const dummyProcesses: Process[] = [
    {
      id: '10',
      agent_id: '1',
      name: 'pay_srv',
      listen_port: 8080,
      arch_process_id: '100', // 匹配该套件架构进程
    },
    {
      id: '20',
      agent_id: '2',
      name: 'other_srv',
      listen_port: 8081,
      arch_process_id: '200', // 不匹配
    },
  ];

  const execHtml = renderToString(
    <GoldenSuiteExecuteModal
      open={true}
      onClose={() => {}}
      suite={dummySuite}
      allAgents={dummyAgents}
      allProcesses={dummyProcesses}
    />
  );

  expect(execHtml).toContain('发起黄金契约套件脱机回归');
  expect(execHtml).toContain('GS_PAY_CORE');
  // 必须命中挂载匹配进程的 Agent-Dev-01，过滤掉不相关的 Agent-Dev-02
  expect(execHtml).toContain('Agent-Dev-01');
  expect(execHtml).not.toContain('Agent-Dev-02');
  expect(execHtml).toContain('排他租约保护预检');
}

export function testGoldenSuiteDetailDrawer_LedgerAndMasks(): void {
  const html = renderToString(
    <GoldenSuiteDetailDrawer
      open={true}
      onClose={() => {}}
      suiteId={1}
    />
  );
  expect(html).toContain('golden-suite-detail-drawer');
  expect(html).toContain('黄金契约用例套件详情');
}

export function testGoldenSuitesWorkbenchPage_Layout(): void {
  const html = renderToString(
    <MemoryRouter initialEntries={['/golden-suites']}>
      <GoldenSuitesWorkbenchPage />
    </MemoryRouter>
  );

  expect(html).toContain('golden-suites-workbench');
  expect(html).toContain('黄金契约套件与脱机回归工作台');
  expect(html).toContain('新建黄金契约套件');
  expect(html).toContain('全部架构进程 (ALL)');
  expect(html).toContain('全部状态 (ALL)');
}

describe('GoldenSuiteWorkbench Test Suite (M15)', () => {
  it('TC-GS-01: 沉淀为黄金契约抽屉渲染与自包含保障断言', () => {
    testGoldenCurateDrawer_RenderingAndSelfContainedSnapshot();
  });

  it('TC-GS-02: 套件新建模态窗与同构候选 Agent 脱机回归执行预检', () => {
    testGoldenSuiteCreateAndExecuteModals();
  });

  it('TC-GS-03: 契约套件详情抽屉与用例账本骨架验证', () => {
    testGoldenSuiteDetailDrawer_LedgerAndMasks();
  });

  it('TC-GS-04: /golden-suites 工作台主体布局与筛选栏渲染', () => {
    testGoldenSuitesWorkbenchPage_Layout();
  });
});
