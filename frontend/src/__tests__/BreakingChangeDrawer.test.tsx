import { describe, it, expect, vi } from 'vitest';
import { renderToString } from 'react-dom/server';
import { BreakingChangeAlertDrawer } from '../pages/ProtoSchemasPage/BreakingChangeAlertDrawer';
import type { BreakingRuleViolation, AffectedDependent } from '../types';

describe('BreakingChangeAlertDrawer Unit Suite (M16 / SC-09)', () => {
  const mockBreakingRules: BreakingRuleViolation[] = [
    {
      rule: 'TAG_MUTATION',
      proto_file: 'account/v1/account.proto',
      symbol: 'account.v1.AccountReq.user_id',
      message: "字段 'user_id' 的 Tag 从 1 变更为 2",
      service_name: 'account.v1.AccountService',
    },
    {
      rule: 'SERVICE_OR_METHOD_REMOVED',
      proto_file: 'account/v1/account.proto',
      symbol: 'account.v1.AccountService.DeleteAccount',
      message: "RPC 方法 'DeleteAccount' 已在服务 'account.v1.AccountService' 中被删除",
      service_name: 'account.v1.AccountService',
    },
    {
      rule: 'REMOVED_WITHOUT_RESERVE',
      proto_file: 'account/v1/account.proto',
      symbol: 'account.v1.AccountReq.span_id',
      message: "已删除字段 'span_id' (Tag: 3) 未在保留范围 reserved 中声明数字 Tag",
      service_name: 'account.v1.AccountService',
    },
  ];

  const mockAffectedDependents: AffectedDependent[] = [
    {
      module_id: 14,
      module_code: 'ORDER_CORE',
      module_name: '订单核心模块',
      arch_process_code: 'sut-order',
      owner_name: '张三',
    },
    {
      module_id: 28,
      module_code: 'INVENTORY_SVC',
      module_name: '库存服务模块',
      arch_process_code: 'sut-inventory',
      owner_name: '李四',
    },
  ];

  it('testBreakingChangeDrawer_RendersRulesDependents_AndAdminOverrideReasonValidation', () => {
    // 1. 渲染抽屉开启态并断言违规项与下游消费者血缘
    const html = renderToString(
      <BreakingChangeAlertDrawer
        isOpen={true}
        onClose={vi.fn()}
        breakingRules={mockBreakingRules}
        affectedDependents={mockAffectedDependents}
        isAdmin={true}
        onConfirmOverride={vi.fn()}
      />
    );

    // 结构化规则与错误标签
    expect(html).toContain('破坏性契约变更拦截');
    expect(html).toContain('TAG_MUTATION');
    expect(html).toContain('SERVICE_OR_METHOD_REMOVED');
    expect(html).toContain('REMOVED_WITHOUT_RESERVE');
    expect(html).toContain('account/v1/account.proto');
    expect(html).toContain('account.v1.AccountReq.user_id');
    expect(html).toContain("字段 &#x27;user_id&#x27; 的 Tag 从 1 变更为 2");
    expect(html).toContain('account.v1.AccountService');

    // 消费方模块与负责人
    expect(html).toContain('波及消费方模块');
    expect(html).toContain('ORDER_CORE');
    expect(html).toContain('订单核心模块');
    expect(html).toContain('sut-order');
    expect(html).toContain('张三');
    expect(html).toContain('INVENTORY_SVC');
    expect(html).toContain('库存服务模块');
    expect(html).toContain('李四');

    // 管理员带因逃生舱表单
    expect(html).toContain('管理员强推 / 紧急回滚逃生舱');
    expect(html).toContain('至少输入 8 个字符');
    expect(html).toContain('强制激活 / 回滚');
  });

  it('renders closed drawer as null', () => {
    const html = renderToString(
      <BreakingChangeAlertDrawer
        isOpen={false}
        onClose={vi.fn()}
        breakingRules={mockBreakingRules}
        affectedDependents={mockAffectedDependents}
      />
    );
    expect(html).toBe('');
  });

  it('hides admin escape hatch when user is not admin', () => {
    const html = renderToString(
      <BreakingChangeAlertDrawer
        isOpen={true}
        onClose={vi.fn()}
        breakingRules={mockBreakingRules}
        affectedDependents={mockAffectedDependents}
        isAdmin={false}
      />
    );
    expect(html).not.toContain('管理员强推 / 紧急回滚逃生舱');
    expect(html).toContain('当前为只读视窗，如需紧急回滚或大版本强推，请联系具有 traefik_admin 权限的管理员操作');
  });
});
