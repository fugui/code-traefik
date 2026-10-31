import { describe, it } from 'vitest';
import { renderToString } from 'react-dom/server';
import { AgentLeaseStatusBadge } from '../pages/AgentManagementPage/AgentLeaseStatusBadge';
import { SubsystemGroupTreePanel } from '../pages/ArchitectureWorkbenchPage/SubsystemGroupTreePanel';
import type { Agent, Subsystem, FunctionalGroupTreeNode } from '../types';

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

// 自动执行断言函数以保证自检通过
describe('WorkbenchRBAC', () => {
  it('enforces public/private scope and admin guards', () => {
    TestPublicPrivateScopeAndAdminGuards();
  });
});
