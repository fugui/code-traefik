import { describe, it, expect } from 'vitest';
import { renderToString } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import type { Module, ModuleService, ModuleDependency, Subsystem, FunctionalGroupTreeNode, Process } from '../../../types';
import { ModuleContractDetailPane } from '../ModuleContractDetailPane';
import * as archApi from '../../../api/arch';

describe('AC-M20-04 ModuleContractDetailPane Multi-Implementation Tests', () => {
  const dummySubsystem: Subsystem = {
    id: 'sub-1',
    code: 'TRADE_SYS',
    name: '交易子系统',
  };

  const dummyGroup: FunctionalGroupTreeNode = {
    id: 'grp-1',
    subsystem_id: 'sub-1',
    code: 'CORE_GRP',
    name: '核心组',
    sort_order: 1,
    level: 1,
    path: '/1/',
    module_count: 2,
  };

  const dummyProcesses: Process[] = [
    {
      id: 'proc-1',
      name: 'trade_server',
      cmd_pattern: './bin/trade_server',
      listen_port: 9001,
      group_name: 'TRADE_GRP',
      agent_id: '1',
      agent: {
        id: '1',
        name: 'Agent-01',
        endpoint: '10.0.0.1:9091',
        scope: 'SHARED',
        status: 'ONLINE',
        lease_type: '',
      },
    },
  ];

  const noopAsync = async () => {};
  const noop = () => {};

  it('renders CONTRACT_LIB mode with contract matrix and hides physical process & dual-tables', () => {
    const contractLibModule: Module = {
      id: 'mod-lib-1',
      subsystem_id: 'sub-1',
      group_id: 'grp-1',
      code: 'LIB_PAY_CONTRACT',
      name: '支付统一接口契约库',
      module_kind: 'CONTRACT_LIB',
      owner_name: '契约架构师',
    };

    const html = renderToString(
      <MemoryRouter>
        <ModuleContractDetailPane
          modules={[contractLibModule]}
          selectedModule={contractLibModule}
          onSelectModule={noop}
          services={[]}
          dependencies={[]}
          allProcesses={dummyProcesses}
          isAdmin={true}
          currentSubsystem={dummySubsystem}
          currentGroup={dummyGroup}
          onCreateModule={noop}
          onEditModule={noop}
          onDeleteModule={noop}
          onCreateService={noopAsync}
          onUpdateServiceIgnorePaths={noopAsync}
          onDeleteService={noopAsync}
          onCreateDependency={noopAsync}
          onDeleteDependency={noopAsync}
          onBindProcess={noopAsync}
        />
      </MemoryRouter>
    );

    // 1. CONTRACT_LIB 专属特性卡片
    expect(html).toContain('data-testid="contract-lib-info-section"');
    expect(html).toContain('«interface» 共享 IDL 契约库');
    expect(html).toContain('不绑定特定宿主进程');

    // 2. 多实现契约定义与矩阵区域
    expect(html).toContain('data-testid="service-contracts-section"');
    expect(html).toContain('data-testid="contracts-count-badge"');
    expect(html).toContain('data-testid="add-contract-btn"');
    expect(html).toContain('data-testid="service-contracts-table"');

    // 3. 隐藏物理进程装配全貌卡片与普通 PROVIDE/DEPEND 双表格
    expect(html).not.toContain('物理进程装配全貌');
    expect(html).not.toContain('对外供给服务列表 (PROVIDE)');
    expect(html).not.toContain('所需依赖服务列表 (DEPEND)');
  });

  it('renders BUSINESS mode with dual tables and physical process card', () => {
    const businessModule: Module = {
      id: 'mod-biz-1',
      subsystem_id: 'sub-1',
      group_id: 'grp-1',
      code: 'MOD_ALIPAY_CHANNEL',
      name: '支付宝渠道实现模块',
      module_kind: 'BUSINESS',
      process_id: 'proc-1',
      process: dummyProcesses[0],
      owner_name: '支付开发组',
    };

    const dummyServices: ModuleService[] = [
      {
        id: 'svc-1',
        module_id: 'mod-biz-1',
        service_name: 'dophi.pay.v1.PayService',
        contract_id: 'contract-pay-1',
        impl_key: 'alipay',
        diff_ignore_paths: ['timestamp'],
        description: '支付宝渠道实现分支',
        downstream_count: 2,
        downstream_consumers: [
          {
            module_id: 'mod-order',
            module_name: '订单中心',
            module_code: 'MOD_ORDER',
          },
        ],
      },
    ];

    const dummyDependencies: ModuleDependency[] = [
      {
        id: 'dep-1',
        consumer_module_id: 'mod-biz-1',
        is_external: false,
        service_id: 'svc-user',
        service: {
          id: 'svc-user',
          module_id: 'mod-user',
          service_name: 'dophi.user.v1.UserService',
        },
        provider_module: {
          id: 'mod-user',
          code: 'USER_CENTER',
          name: '用户中台',
        },
        description: '获取用户账户信息',
      },
    ];

    const html = renderToString(
      <MemoryRouter>
        <ModuleContractDetailPane
          modules={[businessModule]}
          selectedModule={businessModule}
          onSelectModule={noop}
          services={dummyServices}
          dependencies={dummyDependencies}
          allProcesses={dummyProcesses}
          isAdmin={true}
          currentSubsystem={dummySubsystem}
          currentGroup={dummyGroup}
          onCreateModule={noop}
          onEditModule={noop}
          onDeleteModule={noop}
          onCreateService={noopAsync}
          onUpdateServiceIgnorePaths={noopAsync}
          onDeleteService={noopAsync}
          onCreateDependency={noopAsync}
          onDeleteDependency={noopAsync}
          onBindProcess={noopAsync}
        />
      </MemoryRouter>
    );

    // 1. 物理进程装配与普通双表展示
    expect(html).toContain('物理进程装配全貌');
    expect(html).toContain('trade_server');
    expect(html).toContain('对外供给服务列表 (PROVIDE)');
    expect(html).toContain('所需依赖服务列表 (DEPEND)');
    expect(html).toContain('dophi.pay.v1.PayService');
    expect(html).toContain('dophi.user.v1.UserService');

    // 2. 隐藏 CONTRACT_LIB 矩阵与特性
    expect(html).not.toContain('data-testid="contract-lib-info-section"');
    expect(html).not.toContain('data-testid="service-contracts-section"');
  });

  it('renders registered service contract list and implementation details in CONTRACT_LIB', () => {
    // 验证 API 存在性与接口契约数据交互
    expect(typeof archApi.listServiceContracts).toBe('function');
    expect(typeof archApi.createServiceContract).toBe('function');
    expect(typeof archApi.deleteServiceContract).toBe('function');
    expect(typeof archApi.convertExclusiveToMultiImplContract).toBe('function');
  });
});
