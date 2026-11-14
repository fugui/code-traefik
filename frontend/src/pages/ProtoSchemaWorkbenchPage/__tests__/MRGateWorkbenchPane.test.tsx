import React from 'react';
import { describe, expect, it, vi } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import { MRGateWorkbenchPane, canOverrideMRRun } from '../MRGateWorkbenchPane';
import { ProtoSchemaWorkbenchPage } from '../ProtoSchemaWorkbenchPage';
import type { MRCheckRunDTO, MRCheckRunDetailDTO } from '../../../types';

vi.mock('@code/common', async () => {
  const actual = await vi.importActual<typeof import('@code/common')>('@code/common');
  return {
    ...actual,
    Drawer: ({ open, title, children }: { open: boolean; title?: React.ReactNode; children?: React.ReactNode }) =>
      open ? (
        <div data-testid="mock-drawer">
          <h2>{title}</h2>
          {children}
        </div>
      ) : null,
    Modal: ({ open, title, children }: { open: boolean; title?: React.ReactNode; children?: React.ReactNode }) =>
      open ? (
        <div data-testid="mock-modal">
          <h3>{title}</h3>
          {children}
        </div>
      ) : null,
  };
});

const compileFailedRun: MRCheckRunDTO = {
  id: 101,
  git_repo_id: 1,
  git_repo_name: 'trade-core-proto',
  provider: 'GITLAB',
  bundle_id: 10,
  bundle_name: 'trade',
  target_schema_name: 'trade-core-schema',
  mr_iid: 88,
  event_phase: 'MR_GATE',
  mr_title: 'fix: broken syntax in order.proto',
  mr_url: 'https://git.example.com/trade/mr/88',
  source_branch: 'feat/bad-syntax',
  target_branch: 'main',
  head_commit_sha: 'aaaa11112222',
  target_head_sha: 'bbbb00001111',
  author_username: 'alice',
  author_email: 'alice@example.com',
  status: 'FAILED',
  compile_passed: false,
  lint_error_count: 1,
  lint_warning_count: 0,
  duplicate_group_count: 0,
  breaking_change_count: 0,
  affected_modules_count: 0,
  changed_proto_files: ['trade/v1/order.proto'],
  compile_error_text: 'trade/v1/order.proto:12:5: unexpected token',
  breaking_details: [],
  affected_modules: [],
  remote_comment_id: 'note-101',
  attempt_count: 1,
  override_reason: '',
  duration_ms: 120,
  created_at: '2026-11-14T10:00:00Z',
  updated_at: '2026-11-14T10:00:01Z',
};

const breakingCompiledRun: MRCheckRunDetailDTO = {
  ...compileFailedRun,
  id: 102,
  mr_iid: 89,
  mr_title: 'feat!: remove legacy fee_type field',
  source_branch: 'feat/breaking-fee',
  status: 'FAILED',
  compile_passed: true,
  compile_error_text: '',
  lint_error_count: 0,
  lint_warning_count: 1,
  duplicate_group_count: 1,
  breaking_change_count: 1,
  affected_modules_count: 1,
  breaking_details: [
    {
      rule: 'FIELD_NO_DELETE',
      proto_file: 'trade/v1/order.proto',
      symbol: 'trade.v1.Order.fee_type',
      message: '禁止删除保留字段 fee_type (tag=9)',
      service_name: 'trade.v1.OrderService',
    },
  ],
  affected_modules: [
    {
      module_id: 7,
      module_code: 'MOD_BILLING',
      module_name: '清结算账务中心',
      arch_process_code: 'PROC_SETTLE',
      owner_name: '王架构',
    },
  ],
  violations: [
    {
      id: 501,
      report_id: 20,
      rule_code: 'COMMENT_REQUIRED',
      severity: 'WARNING',
      proto_file: 'trade/v1/order.proto',
      symbol_path: 'trade.v1.Order.channel',
      file_line: 24,
      file_column: 3,
      message: '建议补充字段前导注释',
      suggestion: '添加 `// 支付渠道标识` 注释',
      suggestion_source: 'BUILTIN',
    },
  ],
  duplicate_groups: [
    {
      cluster_hash: 'dup-cluster-abc',
      similarity: 0.875,
      suggestion: '建议复用 common.MoneyAmount',
      members: [
        {
          proto_file: 'trade/v1/order.proto',
          package: 'trade.v1',
          message: 'OrderAmount',
          file_line: 40,
          field_cnt: 3,
        },
      ],
    },
  ],
};

describe('MRGateWorkbenchPane & /schemas?tab=mr-gates Test Suite', () => {
  it('TC-MRGATE-01: 访问 /schemas?tab=mr-gates 时渲染四合一门禁控制台而非占位符', () => {
    const html = renderToStaticMarkup(
      <MemoryRouter initialEntries={['/schemas?tab=mr-gates']}>
        <ProtoSchemaWorkbenchPage />
      </MemoryRouter>
    );

    expect(html).toContain('Git MR / PR 四合一契约门禁雷达');
    expect(html).not.toContain('proto-mr-gates-placeholder');
  });

  it('TC-MRGATE-02: 编译失败流水硬禁 Override，破坏性变更（编译通过）允许 Admin 豁免并展示波及模块明细', () => {
    expect(canOverrideMRRun(compileFailedRun, true)).toBe(false);
    expect(canOverrideMRRun(breakingCompiledRun, true)).toBe(true);
    expect(canOverrideMRRun(breakingCompiledRun, false)).toBe(false);

    const html = renderToStaticMarkup(
      <MemoryRouter initialEntries={['/schemas?tab=mr-gates']}>
        <MRGateWorkbenchPane
          isAdmin
          initialRuns={[compileFailedRun, breakingCompiledRun]}
          initialTotal={2}
          initialDetail={breakingCompiledRun}
        />
      </MemoryRouter>
    );

    expect(html).toContain('fix: broken syntax in order.proto');
    expect(html).toContain('编译失败');
    expect(html).toContain('不可豁免');
    expect(html).toContain('feat!: remove legacy fee_type field');
    expect(html).toContain('豁免放行');
    expect(html).toContain('FIELD_NO_DELETE');
    expect(html).toContain('清结算账务中心');
    expect(html).toContain('王架构');
    expect(html).toContain('相似度 87.5%');
  });
});
