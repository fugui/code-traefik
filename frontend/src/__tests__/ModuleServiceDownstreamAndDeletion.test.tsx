import { describe, it, expect } from 'vitest';
import { renderToString } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import type { Module, ModuleService, Subsystem, FunctionalGroupTreeNode } from '../types';
import { ModuleContractDetailPane } from '../pages/ArchitectureWorkbenchPage/ModuleContractDetailPane';

describe('ModuleServiceDownstreamAndDeletion Test Suite (P0 UX Optimization)', () => {
  const dummySubsystem: Subsystem = {
    id: 'sub-1',
    code: 'ECOMMERCE',
    name: '电商交易微服务系统',
    description: '',
    created_at: '2026-11-01T00:00:00Z',
    updated_at: '2026-11-01T00:00:00Z',
  };

  const dummyGroup: FunctionalGroupTreeNode = {
    id: 'grp-1',
    subsystem_id: 'sub-1',
    code: 'TRADE',
    name: '交易中枢与网关',
    sort_order: 1,
    level: 1,
    module_count: 1,
    path: '/1/',
    children: [],
  };

  const dummyModule: Module = {
    id: 'mod-order-core',
    subsystem_id: 'sub-1',
    group_id: 'grp-1',
    code: 'ORDER_CORE',
    name: '订单交易核心模块',
    description: '负责订单生命周期',
    created_at: '2026-11-01T00:00:00Z',
    updated_at: '2026-11-01T00:00:00Z',
  };

  const servicesWithDownstream: ModuleService[] = [
    {
      id: 'svc-order-1',
      module_id: dummyModule.id,
      service_name: 'demo.order.OrderService',
      diff_ignore_paths: [],
      description: 'CROS RPC 订单微服务对外契约',
      downstream_count: 1,
      downstream_modules: ['客户端交互终端模块 (客户端交互子系统)'],
      downstream_consumers: [
        {
          module_id: 'mod-client-terminal',
          module_name: '客户端交互终端模块',
          module_code: 'CLIENT_TERMINAL',
          subsystem_name: '客户端交互子系统',
        },
      ],
      created_at: '2026-11-01T00:00:00Z',
      updated_at: '2026-11-01T00:00:00Z',
    },
    {
      id: 'svc-order-2',
      module_id: dummyModule.id,
      service_name: 'demo.order.InternalHelperService',
      diff_ignore_paths: [],
      description: '内部未被引用的辅助服务',
      downstream_count: 0,
      downstream_modules: [],
      downstream_consumers: [],
      created_at: '2026-11-01T00:00:00Z',
      updated_at: '2026-11-01T00:00:00Z',
    },
  ];

  it('renders downstream consumers with names, subsystem labels, and non-disabled clickable delete button', () => {
    const html = renderToString(
      <MemoryRouter>
        <ModuleContractDetailPane
          modules={[dummyModule]}
          selectedModuleId={dummyModule.id}
          onSelectModule={() => {}}
          selectedModule={dummyModule}
          services={servicesWithDownstream}
          dependencies={[]}
          allProcesses={[]}
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

    // 1. 验证被依赖的服务显示带有 warning 状态的 1 个消费方 Badge
    expect(html).toContain('data-testid="badge-downstream-svc-order-1"');
    expect(html).toContain('code-badge--warning');
    expect(html).toMatch(/1(<!-- -->)?\s*个消费方/);

    // 2. 验证消费方清单中清晰呈现了消费模块名称与所属子系统标签
    expect(html).toContain('客户端交互终端模块');
    expect(html).toContain('客户端交互子系统');
    expect(html).toContain('点击在架构树中聚焦此消费模块：客户端交互终端模块 (客户端交互子系统)');

    // 3. 验证被依赖的删除按钮：不再具备原生 disabled 属性（杜绝死寂无响应），并带有警示提示
    expect(html).toContain('data-testid="btn-delete-service-svc-order-1"');
    // 确保不是 disabled 按钮
    const deleteBtnRegex = /<button[^>]*data-testid="btn-delete-service-svc-order-1"[^>]*>/;
    const match = html.match(deleteBtnRegex);
    expect(match).not.toBeNull();
    expect(match![0]).not.toContain('disabled');
    expect(match![0]).toContain('正被 1 个模块依赖消费，点击查看消费详情与解除指引');

    // 4. 验证零依赖的服务显示 info 状态的 0 个消费方 Badge 且删除按钮为纯净删除
    expect(html).toContain('data-testid="badge-downstream-svc-order-2"');
    expect(html).toMatch(/0(<!-- -->)?\s*个消费方/);
    expect(html).toContain('data-testid="btn-delete-service-svc-order-2"');
    const deleteBtnRegex2 = /<button[^>]*data-testid="btn-delete-service-svc-order-2"[^>]*>/;
    const match2 = html.match(deleteBtnRegex2);
    expect(match2).not.toBeNull();
    expect(match2![0]).not.toContain('disabled');
    expect(match2![0]).toContain('删除对外服务契约 [demo.order.InternalHelperService]');
  });
});
