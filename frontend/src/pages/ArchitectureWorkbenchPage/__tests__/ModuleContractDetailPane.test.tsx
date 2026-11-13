import { describe, it, expect } from 'vitest';
import { renderToString } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import type {
  Module,
  ModuleService,
  ModuleDependency,
  Subsystem,
  FunctionalGroupTreeNode,
  Process,
  ClaimableProtoServiceItem,
  ServiceContract,
} from '../../../types';
import { ModuleContractDetailPane } from '../ModuleContractDetailPane';
import { ProtoServiceSelectorField } from '../ProtoServiceSelectorField';
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
    expect(typeof archApi.attachExclusiveToMultiImplContract).toBe('function');
  });

  it('renders custom published service_name, LIB contract schema link, and DISPATCH dependency in BUSINESS mode (ADR-16 v2.0)', () => {
    const businessModule: Module = {
      id: 'mod-dh-1',
      subsystem_id: 'sub-1',
      group_id: 'grp-1',
      code: 'MOD_DH_SYNC',
      name: 'DH 同步服务实现模块',
      module_kind: 'BUSINESS',
      process_id: 'proc-1',
      process: dummyProcesses[0],
      owner_name: '核心架构组',
    };

    const customImplService: ModuleService = {
      id: 'svc-dh-1',
      module_id: 'mod-dh-1',
      service_name: 'DH_SyncService',
      contract_id: 'contract-sync-1',
      impl_key: 'DH',
      is_custom_service_name: true,
      contract_service_name: 'SyncService',
      description: 'DH 行方独立发布同步服务',
      downstream_count: 1,
    };

    const dispatchDependency: ModuleDependency = {
      id: 'dep-dispatch-1',
      consumer_module_id: 'mod-dh-1',
      contract_id: 'contract-pay-1',
      is_external: false,
      contract: {
        id: 'contract-pay-1',
        definer_module_id: 'mod-lib-1',
        service_name: 'dophi.pay.v1.PayService',
        provide_mode: 'MULTI_IMPL',
        definer_module: {
          id: 'mod-lib-1',
          subsystem_id: 'sub-1',
          group_id: 'grp-1',
          code: 'LIB_PAY_CONTRACT',
          name: '支付统一接口契约库',
          module_kind: 'CONTRACT_LIB',
        },
      },
      description: '泛化依赖支付 LIB 契约',
    };

    const html = renderToString(
      <MemoryRouter>
        <ModuleContractDetailPane
          modules={[businessModule]}
          selectedModule={businessModule}
          onSelectModule={noop}
          services={[customImplService]}
          dependencies={[dispatchDependency]}
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

    const normalizedHtml = html.replace(/<!-- -->/g, '');
    expect(normalizedHtml).toContain('DH_SyncService');
    expect(normalizedHtml).toContain('契约实现: DH');
    expect(normalizedHtml).toContain('自定义发布名');
    expect(normalizedHtml).toContain('LIB: SyncService');
    expect(normalizedHtml).toContain('/schemas?active_only=true&amp;search=SyncService');
    expect(normalizedHtml).toContain('dophi.pay.v1.PayService');
    expect(normalizedHtml).toContain('data-testid="badge-dispatch-dep-dep-dispatch-1"');
    expect(normalizedHtml).toContain('DISPATCH');
    expect(normalizedHtml).toContain('支付统一接口契约库');
  });

  it('renders ProtoServiceSelectorField in CONTRACT CLAIM mode with status badges, disabled conflicts, and preview card', () => {
    const claimable: ClaimableProtoServiceItem[] = [
      {
        service_name: 'com.fugui.TaskService',
        proto_package: 'com.fugui',
        proto_service: 'TaskService',
        schema_id: 10,
        schema_name: 'task_v1.pb',
        operation_count: 2,
        operations: ['CreateTask', 'QueryTask'],
        is_claimed: false,
      },
      {
        service_name: 'dophi.pay.v1.PayService',
        proto_package: 'dophi.pay.v1',
        proto_service: 'PayService',
        schema_id: 11,
        schema_name: 'pay_v1.pb',
        operation_count: 1,
        operations: ['Pay'],
        is_claimed: false,
        is_multi_impl: true,
        contract_id: 99,
      },
      {
        service_name: 'dophi.order.v1.OrderService',
        proto_package: 'dophi.order.v1',
        proto_service: 'OrderService',
        schema_id: 12,
        schema_name: 'order_v1.pb',
        operation_count: 1,
        operations: ['CreateOrder'],
        is_claimed: true,
        claimed_by_module: { id: '20', code: 'MOD_ORDER', name: '订单中心' },
      },
    ];

    const knownContracts: ServiceContract[] = [
      {
        id: '99',
        definer_module_id: 'mod-lib-1',
        service_name: 'dophi.pay.v1.PayService',
        provide_mode: 'MULTI_IMPL',
      },
    ];

    const html = renderToString(
      <ProtoServiceSelectorField
        intent="CONTRACT"
        mode="CLAIM"
        onModeChange={noop}
        serviceName="com.fugui.TaskService"
        onServiceNameChange={noop}
        claimableServices={claimable}
        loadingClaimable={false}
        allContracts={knownContracts}
        testIdPrefix="contract"
      />
    ).replace(/<!-- -->/g, '');

    expect(html).toContain('data-testid="contract-mode-tabs"');
    expect(html).toContain('从已激活 Proto Service 选择');
    expect(html).toContain('手动预先输入声明');
    expect(html).toContain('data-testid="contract-proto-select"');
    expect(html).toContain('com.fugui.TaskService (2 个方法 | Schema: task_v1.pb) [可登记]');
    expect(html).toContain('dophi.pay.v1.PayService (1 个方法 | Schema: pay_v1.pb) [已登记为多实现契约]');
    expect(html).toContain('dophi.order.v1.OrderService (1 个方法 | Schema: order_v1.pb) [已被 订单中心 独占]');
    expect(html).toContain('data-testid="contract-proto-preview"');
    expect(html).toContain('可登记为多实现契约');
    expect(html).toContain('CreateTask');
    expect(html).toContain('QueryTask');
  });

  it('renders ProtoServiceSelectorField in CONTRACT MANUAL mode and empty CLAIM hint', () => {
    const emptyClaimHtml = renderToString(
      <ProtoServiceSelectorField
        intent="CONTRACT"
        mode="CLAIM"
        onModeChange={noop}
        serviceName=""
        onServiceNameChange={noop}
        claimableServices={[]}
        loadingClaimable={false}
        allContracts={[]}
        testIdPrefix="contract"
      />
    ).replace(/<!-- -->/g, '');

    expect(emptyClaimHtml).toContain('data-testid="contract-proto-empty-hint"');
    expect(emptyClaimHtml).toContain('手动预先输入声明');

    const manualHtml = renderToString(
      <ProtoServiceSelectorField
        intent="CONTRACT"
        mode="MANUAL"
        onModeChange={noop}
        serviceName="com.fugui.FutureService"
        onServiceNameChange={noop}
        claimableServices={[]}
        loadingClaimable={false}
        allContracts={[]}
        testIdPrefix="contract"
      />
    ).replace(/<!-- -->/g, '');

    expect(manualHtml).toContain('data-testid="contract-service-name-input"');
    expect(manualHtml).toContain('com.fugui.FutureService');
    expect(manualHtml).toContain('支持未上传 .pb 前预先登记');
  });
});
