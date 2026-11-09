import { describe, it, expect } from 'vitest';
import { renderToString } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import { ArchProcessTreePanel } from '../pages/ArchitectureWorkbenchPage/ArchProcessTreePanel';
import { ArchProcessDetailPane } from '../pages/ArchitectureWorkbenchPage/ArchProcessDetailPane';
import { ModuleContractDetailPane } from '../pages/ArchitectureWorkbenchPage/ModuleContractDetailPane';
import { ArchitectureWorkbenchPage } from '../pages/ArchitectureWorkbenchPage/ArchitectureWorkbenchPage';
import { ModuleUserProvider } from '../context/ModuleUserContext';
import type {
  ArchProcessGroup,
  ArchProcess,
  Module,
  ModuleService,
  ModuleDependency,
  Process,
  AutoPinnedOverride,
  CurrentUserProfile,
} from '../types';

describe('ArchPhysicalBlueprintWorkbench Test Suite', () => {
  const dummyGroups: ArchProcessGroup[] = [
    {
      id: 1,
      code: 'CORE_SRV',
      name: '核心服务组',
      sort_order: 1,
      processes: [
        {
          id: 101,
          group_id: 1,
          code: 'proc-account-srv',
          name: '账户服务进程',
          default_listen_port: 8080,
          default_cmd_pattern: 're:.*account-service.*',
          sort_order: 1,
          mount_count: 2,
        },
        {
          id: 102,
          group_id: 1,
          code: 'proc-order-daemon',
          name: '订单处理守护进程',
          default_listen_port: 0, // 纯客户端
          default_cmd_pattern: 're:.*order-daemon.*',
          sort_order: 2,
          mount_count: 1,
        },
      ],
    },
  ];

  const dummyArchProcess: ArchProcess = {
    id: 101,
    group_id: 1,
    code: 'proc-account-srv',
    name: '账户服务进程',
    default_listen_port: 8080,
    default_cmd_pattern: 're:.*account-service.*',
    sort_order: 1,
    description: '承载核心账户余额查询与转账记账逻辑',
    group: dummyGroups[0],
  };

  const dummyModules: Module[] = [
    {
      id: 'm1',
      subsystem_id: 'sub1',
      group_id: 'grp1',
      code: 'MOD_ACCOUNT_QUERY',
      name: '账户查询模块',
      arch_process_id: 101,
      arch_process: dummyArchProcess,
    },
  ];

  const dummyServices: ModuleService[] = [
    {
      id: 'svc1',
      module_id: 'm1',
      service_name: 'AccountQueryService.GetBalance',
      diff_ignore_paths: ['timestamp', 'request_id'],
      description: '查询实时可用余额',
    },
  ];

  const dummyDependencies: ModuleDependency[] = [
    {
      id: 'dep1',
      consumer_module_id: 'm1',
      is_external: true,
      external_system: 'PAYMENT_CORE',
      external_service: 'PaymentRoute.QueryCard',
      description: '卡组织路由查询',
    },
  ];

  const dummyAgentMounts: Process[] = [
    {
      id: 'p-inst-1',
      agent_id: 'ag-1',
      arch_process_id: 101,
      name: 'proc-account-srv-inst1',
      listen_port: 8080,
      effective_listen_port: 8080,
      advertise_ip: '192.168.56.18',
      probe_desired_state: 'ARMED',
      probe_status: 'READY',
      agent: {
        id: 'ag-1',
        name: '测试集群-01',
        endpoint: 'http://192.168.56.18:9091',
        scope: 'SHARED',
        status: 'ONLINE',
        lease_type: '',
      },
    },
    {
      id: 'p-inst-2',
      agent_id: 'ag-2',
      arch_process_id: 101,
      name: 'proc-account-srv-inst2',
      listen_port: 8081,
      effective_listen_port: 8081,
      override_listen_port: 8081,
      advertise_ip: '192.168.56.20',
      probe_desired_state: 'ARMED',
      probe_status: 'NEED_RECONNECT',
      agent: {
        id: 'ag-2',
        name: '沙箱开发机-zhangsan',
        endpoint: 'http://192.168.56.20:9091',
        scope: 'PRIVATE',
        status: 'ONLINE',
        lease_type: '',
      },
    },
  ];

  const dummyAutoPinnedOverrides: AutoPinnedOverride[] = [
    {
      process_id: 202,
      agent_id: 2,
      override_listen_port: 8081,
    },
  ];

  const adminUser: CurrentUserProfile = {
    id: 1,
    username: 'admin',
    name: '系统管理员',
    roles: ['traefik_admin'],
    required_ad_role: 'traefik_admin',
    is_module_admin: true,
    is_super_admin: false,
  };

  it('renders ArchProcessTreePanel with groups, processes, port badges and mount counts', () => {
    const html = renderToString(
      <ArchProcessTreePanel
        groups={dummyGroups}
        selectedProcessId="101"
        onSelectProcess={() => {}}
        isAdmin={true}
        onCreateGroup={() => {}}
        onEditGroup={() => {}}
        onDeleteGroup={() => {}}
        onCreateProcess={() => {}}
      />
    );

    expect(html).toContain('物理进程蓝图');
    expect(html).toContain('核心服务组');
    expect(html).toContain('账户服务进程');
    expect(html).toContain('proc-account-srv');
    expect(html).toContain(':8080');
    expect(html).toContain('挂载');
    expect(html).toContain('订单处理守护进程');
    expect(html).toContain('纯客户端');
    expect(html).toContain('data-testid="create-process-group-btn"');
    expect(html).toContain('data-testid="create-arch-process-btn"');
  });

  it('renders ArchProcessDetailPane with modules, contracts, multi-env agent mounts and auto-pin warning banner', () => {
    const html = renderToString(
      <MemoryRouter>
        <ArchProcessDetailPane
          archProcess={dummyArchProcess}
          modules={dummyModules}
          providedServices={dummyServices}
          dependencies={dummyDependencies}
          agentMounts={dummyAgentMounts}
          autoPinnedOverrides={dummyAutoPinnedOverrides}
          allModules={dummyModules}
          isAdmin={true}
          onEditProcess={() => {}}
          onDeleteProcess={() => {}}
          onBindModule={async () => {}}
          onUnbindModule={async () => {}}
        />
      </MemoryRouter>
    );

    // Header info
    expect(html).toContain('账户服务进程');
    expect(html).toContain('proc-account-srv');
    expect(html).toContain('re:.*account-service.*');
    expect(html).toContain('承载核心账户余额查询与转账记账逻辑');
    expect(html).toContain('data-testid="edit-arch-process-btn"');
    expect(html).toContain('data-testid="delete-arch-process-btn"');

    // Auto-pinned overrides warning banner
    expect(html).toContain('data-testid="auto-pinned-overrides-banner"');
    expect(html).toContain('默认端口变更触发多环境 Agent 端口冲突保护机制');
    expect(html).toContain('Agent #');
    expect(html).toContain('8081');

    // Tabs
    expect(html).toContain('data-testid="tab-modules"');
    expect(html).toContain('data-testid="tab-contracts"');
    expect(html).toContain('data-testid="tab-mounts"');

    // Module list & bind/unbind actions (default active tab)
    expect(html).toContain('MOD_ACCOUNT_QUERY');
    expect(html).toContain('账户查询模块');
    expect(html).toContain('data-testid="bind-module-btn"');
    expect(html).toContain('data-testid="unbind-module-btn-m1"');
  });

  it('renders ModuleContractDetailPane with bound architecture process and 1-click link to physical tree', () => {
    const html = renderToString(
      <MemoryRouter>
        <ModuleContractDetailPane
          modules={dummyModules}
          selectedModuleId="m1"
          onSelectModule={() => {}}
          selectedModule={dummyModules[0]}
          services={dummyServices}
          dependencies={dummyDependencies}
          allProcesses={[]}
          isAdmin={true}
          onCreateModule={() => {}}
          onEditModule={() => {}}
          onDeleteModule={() => {}}
          onViewArchProcess={() => {}}
          onCreateService={async () => {}}
          onUpdateServiceIgnorePaths={async () => {}}
          onDeleteService={async () => {}}
          onCreateDependency={async () => {}}
          onDeleteDependency={async () => {}}
          onBindProcess={async () => {}}
        />
      </MemoryRouter>
    );

    expect(html).toContain('标准架构进程:');
    expect(html).toContain('账户服务进程');
    expect(html).toContain('proc-account-srv');
    expect(html).toContain(':8080');
    expect(html).toContain('data-testid="goto-arch-process-btn"');
    expect(html).toContain('查看物理进程树');
  });

  it('renders ArchitectureWorkbenchPage with segmented control and dual-view switcher', () => {
    const html = renderToString(
      <MemoryRouter initialEntries={['/architecture?tree=physical']}>
        <ModuleUserProvider
          value={{
            user: adminUser,
            loadingUser: false,
            isEmbedded: false,
            reloadUser: async () => {},
          }}
        >
          <ArchitectureWorkbenchPage />
        </ModuleUserProvider>
      </MemoryRouter>
    );

    expect(html).toContain('data-testid="arch-tree-view-segmented"');
    expect(html).toContain('逻辑架构');
    expect(html).toContain('物理架构');
    expect(html).toContain('data-testid="toggle-tree-logical"');
    expect(html).toContain('data-testid="toggle-tree-physical"');
    expect(html).not.toContain('三级逻辑架构台账');
    expect(html).toContain('data-testid="arch-physical-empty-state"');
  });
});
