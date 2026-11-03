import { describe, it, expect } from 'vitest';
import { renderToString } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import type { Subsystem, FunctionalGroupTreeNode, Module, ModuleService, Process } from '../types';
import { ModuleContractDetailPane } from '../pages/ArchitectureWorkbenchPage/ModuleContractDetailPane';
import { SubsystemGroupTreePanel } from '../pages/ArchitectureWorkbenchPage/SubsystemGroupTreePanel';
import { ArchitectureWorkbenchPage } from '../pages/ArchitectureWorkbenchPage/ArchitectureWorkbenchPage';
import { TopologyWorkbenchPage } from '../pages/TopologyWorkbenchPage/TopologyWorkbenchPage';

export function testArchWorkbench_PureLogicalAndBloodlineLinks(): void {
  const dummySubsystem: Subsystem = {
    id: 'sub-trade',
    code: 'CORE_TRADE',
    name: '核心交易子系统',
    description: '处理交易订单结算',
    created_at: '2026-11-01T00:00:00Z',
    updated_at: '2026-11-01T00:00:00Z',
  };

  const dummyGroup: FunctionalGroupTreeNode = {
    id: 'grp-order',
    subsystem_id: 'sub-trade',
    parent_id: null,
    code: 'G_ORDER',
    name: '订单业务组',
    sort_order: 1,
    level: 1,
    path: '/sub-trade/grp-order/',
    module_count: 1,
    children: [],
  };

  const dummyProcess: Process = {
    id: 'proc-order-01',
    agent_id: 'agent-worker-01',
    group_name: '核心交易组',
    name: 'order-server',
    listen_port: 9091,
    advertise_ip: '127.0.0.1',
    cmd_pattern: './bin/order-server',
    agent: {
      id: 'agent-worker-01',
      name: 'Agent-Node-1',
      endpoint: 'http://127.0.0.1:9091',
      scope: 'PRIVATE',
      owner_id: '100',
      owner_name: '张三',
      status: 'ONLINE',
      lease_type: '',
      created_at: '2026-11-01T00:00:00Z',
      updated_at: '2026-11-01T00:00:00Z',
    },
  };

  const dummyModule: Module = {
    id: 'mod-order-svc',
    group_id: 'grp-order',
    subsystem_id: 'sub-trade',
    code: 'M_ORDER',
    name: '订单管理模块',
    description: '核心订单处理',
    process_id: dummyProcess.id,
    process: dummyProcess,
    created_at: '2026-11-01T00:00:00Z',
    updated_at: '2026-11-01T00:00:00Z',
  };

  const dummyServices: ModuleService[] = [
    {
      id: 'svc-order-create',
      module_id: dummyModule.id,
      service_name: 'order.OrderService/CreateOrder',
      diff_ignore_paths: ['/timestamp', '/trace_id'],
      created_at: '2026-11-01T00:00:00Z',
      updated_at: '2026-11-01T00:00:00Z',
    },
  ];

  // 1. 断言瘦身后的架构工作台 ModuleContractDetailPane：双向血缘深链精确定位
  const detailHtml = renderToString(
    <MemoryRouter>
      <ModuleContractDetailPane
        modules={[dummyModule]}
        selectedModuleId={dummyModule.id}
        onSelectModule={() => {}}
        selectedModule={dummyModule}
        services={dummyServices}
        dependencies={[]}
        allProcesses={[dummyProcess]}
        isAdmin={true}
        currentSubsystem={dummySubsystem}
        currentGroup={dummyGroup}
        onCreateModule={() => {}}
        onEditModule={() => {}}
        onDeleteModule={() => {}}
        onCreateService={async () => {}}
        onUpdateServiceIgnorePaths={async () => {}}
        onDeleteService={async () => {}}
        onCreateDependency={async () => {}}
        onDeleteDependency={async () => {}}
        onBindProcess={async () => {}}
      />
    </MemoryRouter>
  );

  // 1.1 断言服务契约深链指向 /schemas?active_only=true&search=...
  expect(detailHtml).toContain('href="/schemas?active_only=true&amp;search=order.OrderService%2FCreateOrder"');
  expect(detailHtml).toContain('data-testid="link-service-svc-order-create"');

  // 1.2 断言装载进程深链指向 /agents?scope=PRIVATE&agent_id=agent-worker-01&process_id=proc-order-01
  expect(detailHtml).toContain(
    'href="/agents?scope=PRIVATE&amp;agent_id=agent-worker-01&amp;process_id=proc-order-01"'
  );
  expect(detailHtml).toContain('data-testid="link-process-bloodline"');

  // 2. 断言纯逻辑树 SubsystemGroupTreePanel 零物理切换按钮与零物理进程写按钮
  const treeHtml = renderToString(
    <MemoryRouter>
      <SubsystemGroupTreePanel
        subsystems={[dummySubsystem]}
        selectedSubsystemId={dummySubsystem.id}
        onSelectSubsystem={() => {}}
        groupTree={[dummyGroup]}
        selectedGroupId={dummyGroup.id}
        onSelectGroup={() => {}}
        modules={[dummyModule]}
        selectedModuleId={dummyModule.id}
        isAdmin={true}
        onCreateSubsystem={() => {}}
        onEditSubsystem={() => {}}
        onDeleteSubsystem={() => {}}
        onCreateGroup={() => {}}
        onEditGroup={() => {}}
        onMoveGroup={() => {}}
        onDeleteGroup={() => {}}
      />
    </MemoryRouter>
  );

  // 确保没有物理树切换 Tab 或进程添加按钮
  expect(treeHtml).not.toContain('code-tree-view-tabs');
  expect(treeHtml).not.toContain('物理架构 (进程)');
  expect(treeHtml).not.toContain('新建物理进程');
  expect(treeHtml).not.toContain('+ 进程');

  // 3. 断言整个 ArchitectureWorkbenchPage 页面：零顶部胶囊 Tab、零拓扑画布内嵌
  const workbenchHtml = renderToString(
    <MemoryRouter initialEntries={['/architecture']}>
      <ArchitectureWorkbenchPage />
    </MemoryRouter>
  );

  expect(workbenchHtml).toContain('逻辑架构');
  expect(workbenchHtml).not.toContain('三级逻辑架构台账');
  expect(workbenchHtml).not.toContain('code-tab-pills');
  expect(workbenchHtml).not.toContain('静态拓扑与一致性画布');
  expect(workbenchHtml).not.toContain('code-topology-pane');

  // 4. 断言独立拓扑页面 TopologyWorkbenchPage 独立承载拓扑工作台
  const topologyHtml = renderToString(
    <MemoryRouter initialEntries={['/topology']}>
      <TopologyWorkbenchPage />
    </MemoryRouter>
  );

  expect(topologyHtml).toContain('code-topology-workbench');
  expect(topologyHtml).toContain('data-testid="topology-workbench-page"');
}

describe('M8-P3: 架构拓扑解耦与三级逻辑架构瘦身 (TopologyAndArchDecoupling)', () => {
  it('应当渲染纯三级逻辑台账，剥离顶部胶囊 Tab 与物理树，且具备精确的跨工作台深链', () => {
    testArchWorkbench_PureLogicalAndBloodlineLinks();
  });
});
