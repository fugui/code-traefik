import { describe, it, expect } from 'vitest';
import { renderToString } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import { AgentLeaseStatusBadge } from '../pages/AgentManagementPage/AgentLeaseStatusBadge';
import { SubsystemGroupTreePanel } from '../pages/ArchitectureWorkbenchPage/SubsystemGroupTreePanel';
import { ProcessModuleBinderContent } from '../pages/AgentManagementPage/ProcessModuleBinderModal';
import { AgentManagementPage } from '../pages/AgentManagementPage/AgentManagementPage';
import { ModuleUserProvider } from '../context/ModuleUserContext';
import type { Agent, Subsystem, FunctionalGroupTreeNode, Process, Module, CurrentUserProfile } from '../types';

export function TestPublicPrivateScopeAndAdminGuards(): void {
  const dummyAgent: Agent = {
    id: '1',
    name: 'test-agent',
    endpoint: 'http://127.0.0.1:9091',
    scope: 'SHARED',
    status: 'ONLINE',
    lease_type: 'RECORDING',
    lease_holder_name: '张三',
  };

  const dummySubsystems: Subsystem[] = [
    { id: '1', code: 'TEST_SUB', name: '测试子系统' },
  ];

  const dummyGroups: FunctionalGroupTreeNode[] = [
    {
      id: '1',
      subsystem_id: '1',
      code: 'G_TEST',
      name: '测试功能组',
      sort_order: 1,
      level: 1,
      path: '/1/',
      module_count: 2,
    },
  ];

  const noop = () => {};

  // 1. 普通研发成员渲染 (isAdmin = false)
  const userAgentHtml = renderToString(
    <AgentLeaseStatusBadge agent={dummyAgent} isAdmin={false} />
  );
  if (userAgentHtml.includes('强制解锁')) {
    throw new Error('RBAC 越权漏洞: 普通用户视图中透出了管理员专属的「强制解锁」功能');
  }

  const userTreeHtml = renderToString(
    <SubsystemGroupTreePanel
      subsystems={dummySubsystems}
      selectedSubsystemId="1"
      onSelectSubsystem={noop}
      groupTree={dummyGroups}
      selectedGroupId="1"
      onSelectGroup={noop}
      isAdmin={false}
      onCreateSubsystem={noop}
      onEditSubsystem={noop}
      onDeleteSubsystem={noop}
      onCreateGroup={noop}
      onEditGroup={noop}
      onMoveGroup={noop}
      onDeleteGroup={noop}
    />
  );
  if (userTreeHtml.includes('+ 子系统') || userTreeHtml.includes('修改子系统')) {
    throw new Error('RBAC 越权漏洞: 普通用户视图中透出了公共子系统的新建/编辑入口');
  }

  // 2. 模块管理员渲染 (isAdmin = true)
  const adminAgentHtml = renderToString(
    <AgentLeaseStatusBadge agent={dummyAgent} isAdmin={true} onForceUnlock={noop} />
  );
  if (!adminAgentHtml.includes('强制解锁')) {
    throw new Error('断言失败: 管理员视图中未能正确展示「强制解锁」排他租约入口');
  }

  const adminTreeHtml = renderToString(
    <SubsystemGroupTreePanel
      subsystems={dummySubsystems}
      selectedSubsystemId="1"
      onSelectSubsystem={noop}
      groupTree={dummyGroups}
      selectedGroupId="1"
      onSelectGroup={noop}
      isAdmin={true}
      onCreateSubsystem={noop}
      onEditSubsystem={noop}
      onDeleteSubsystem={noop}
      onCreateGroup={noop}
      onEditGroup={noop}
      onMoveGroup={noop}
      onDeleteGroup={noop}
    />
  );
  if (!adminTreeHtml.includes('+ 子系统') || !adminTreeHtml.includes('修改子系统')) {
    throw new Error('断言失败: 管理员视图中未能展示子系统的新增与维护操作');
  }
}

export function testAgentProcessConsolidation_GroupNameAndRBAC(): void {
  const normalUser: CurrentUserProfile = {
    id: 100,
    username: 'zhangsan',
    name: '张三',
    roles: [],
    required_ad_role: '',
    is_module_admin: false,
    is_super_admin: false,
  };

  const adminUser: CurrentUserProfile = {
    id: 1,
    username: 'lisi_admin',
    name: '李四管理员',
    roles: ['traefik_admin'],
    required_ad_role: 'traefik_admin',
    is_module_admin: true,
    is_super_admin: false,
  };

  const dummyPrivateAgent: Agent = {
    id: 'agent-priv-01',
    name: '张三的开发节点',
    endpoint: 'http://127.0.0.1:9091',
    scope: 'PRIVATE',
    owner_id: '100',
    owner_name: '张三',
    status: 'ONLINE',
    lease_type: '',
    processes_count: 1,
  };

  const dummyModules: Module[] = [
    { id: 'mod-order-01', subsystem_id: 'sub-1', group_id: 'grp-1', code: 'M_ORDER', name: '订单服务模块' },
    { id: 'mod-pay-02', subsystem_id: 'sub-1', group_id: 'grp-1', code: 'M_PAY', name: '支付服务模块' },
  ];

  const editingClientProcess: Process = {
    id: 'proc-client-01',
    agent_id: dummyPrivateAgent.id,
    group_name: '核心业务组',
    name: 'order-client-daemon',
    listen_port: 0, // 纯客户端
    cmd_pattern: './bin/order-client-daemon',
    process_modules: [
      { process_id: 'proc-client-01', module_id: 'mod-order-01' },
    ],
  };

  // 1. 断言 ProcessModuleBinderContent: 支持 group_name 选择/新建、纯客户端 listen_port=0 及 process_modules 回填
  const binderHtml = renderToString(
    <ProcessModuleBinderContent
      agent={dummyPrivateAgent}
      editingProcess={editingClientProcess}
      existingGroups={['核心业务组', '结算测试组']}
      allModules={dummyModules}
    />
  );

  // 1.1 断言 group_name 正确加载并渲染已有组选项
  expect(binderHtml).toContain('select-group-name');
  expect(binderHtml).toContain('核心业务组');
  expect(binderHtml).toContain('结算测试组');
  expect(binderHtml).toContain('+ 新建进程组...');

  // 1.2 断言 listen_port = 0 纯客户端勾选状态生效
  expect(binderHtml).toContain('data-testid="checkbox-client-only"');
  expect(binderHtml).toContain('checked=""'); // isClientOnly = true

  // 1.3 断言编辑态 module_ids 自动通过 process_modules 成功回填高亮选中
  expect(binderHtml).toContain('data-testid="module-chip-mod-order-01"');
  expect(binderHtml).toContain('code-checkbox-chip--checked');
  expect(binderHtml).not.toMatch(/module-chip-mod-pay-02[^>]*code-checkbox-chip--checked/);

  // 2. 断言 AgentManagementPage RBAC 边界控制
  // 2.1 普通用户操作本人 PRIVATE Agent: 允许挂载业务进程、修改与注销
  const normalUserPrivateAgentHtml = renderToString(
    <MemoryRouter initialEntries={['/agents?scope=PRIVATE']}>
      <ModuleUserProvider
        value={{
          user: normalUser,
          loadingUser: false,
          isEmbedded: false,
          reloadUser: async () => {},
        }}
      >
        <AgentManagementPage />
      </ModuleUserProvider>
    </MemoryRouter>
  );
  expect(normalUserPrivateAgentHtml).toContain('我的私有 Agent');
  expect(normalUserPrivateAgentHtml).toContain('data-testid="register-agent-btn"');
  expect(normalUserPrivateAgentHtml).not.toContain('data-testid="toggle-all-owners-checkbox"');

  // 2.2 普通用户切换到公共 SHARED Agent: 禁绝新增/修改/注销等写入口
  const normalUserSharedAgentHtml = renderToString(
    <MemoryRouter initialEntries={['/agents?scope=SHARED']}>
      <ModuleUserProvider
        value={{
          user: normalUser,
          loadingUser: false,
          isEmbedded: false,
          reloadUser: async () => {},
        }}
      >
        <AgentManagementPage />
      </ModuleUserProvider>
    </MemoryRouter>
  );
  // 普通用户在公共 SHARED Agent 下不能登记公共 Agent
  expect(normalUserSharedAgentHtml).not.toContain('+ 登记 公共 Agent');
  expect(normalUserSharedAgentHtml).not.toContain('data-testid="register-agent-btn"');

  // 2.3 管理员在公共 SHARED Agent 下: 享有完整写权限
  const adminSharedAgentHtml = renderToString(
    <MemoryRouter initialEntries={['/agents?scope=SHARED']}>
      <ModuleUserProvider
        value={{
          user: adminUser,
          loadingUser: false,
          isEmbedded: false,
          reloadUser: async () => {},
        }}
      >
        <AgentManagementPage />
      </ModuleUserProvider>
    </MemoryRouter>
  );
  expect(adminSharedAgentHtml).toContain('+ 登记 公共 Agent');
  expect(adminSharedAgentHtml).toContain('data-testid="register-agent-btn"');

  // 2.4 管理员在私有 PRIVATE Agent 下: 默认“我的私有 Agent”，且具备“全量同事视角”开关
  const adminPrivateAgentHtml = renderToString(
    <MemoryRouter initialEntries={['/agents?scope=PRIVATE']}>
      <ModuleUserProvider
        value={{
          user: adminUser,
          loadingUser: false,
          isEmbedded: false,
          reloadUser: async () => {},
        }}
      >
        <AgentManagementPage />
      </ModuleUserProvider>
    </MemoryRouter>
  );
  expect(adminPrivateAgentHtml).toContain('我的私有 Agent');
  expect(adminPrivateAgentHtml).toContain('data-testid="toggle-all-owners-checkbox"');

  // 2.5 管理员携带 all_owners=true: 标题呈现“全量私有 Agent (管理员视角)”
  const adminAllOwnersHtml = renderToString(
    <MemoryRouter initialEntries={['/agents?scope=PRIVATE&all_owners=true']}>
      <ModuleUserProvider
        value={{
          user: adminUser,
          loadingUser: false,
          isEmbedded: false,
          reloadUser: async () => {},
        }}
      >
        <AgentManagementPage />
      </ModuleUserProvider>
    </MemoryRouter>
  );
  expect(adminAllOwnersHtml).toContain('全量私有 Agent (管理员视角)');
}

describe('WorkbenchRBAC', () => {
  it('enforces public/private scope and admin guards', () => {
    TestPublicPrivateScopeAndAdminGuards();
  });

  it('enforces agent process consolidation, group_name and RBAC (testAgentProcessConsolidation_GroupNameAndRBAC)', () => {
    testAgentProcessConsolidation_GroupNameAndRBAC();
  });
});
