import { describe, it, expect } from 'vitest';
import { renderToStaticMarkup, renderToString } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import { DashboardWorkbenchPage } from '../DashboardWorkbenchPage';
import type { DashboardOverviewDTO } from '../../../types/dashboard';

const mockOverview: DashboardOverviewDTO = {
  health_score: {
    total: 70,
    grade: 'SUBHEALTH',
    arch_sub_score: 40,
    asset_sub_score: 0,
    regression_sub_score: 30,
    action_hints: [
      '存在 15 个生效 Proto 服务契约未被功能模块认领（孤儿契约），请前往【Proto 契约】进行认领归属',
    ],
  },
  drift_radar: {
    verified_edges: 12,
    active_violation_edges: 0,
    top_violated_processes: [],
  },
  contract_assets: {
    total_active_services: 15,
    claimed_services: 0,
    orphan_services: 15,
    claimed_ratio: 0.0,
    top_orphan_services: [
      {
        service_name: 'dophi.order.v1.OrderService',
        schema_id: 10,
        schema_name: 'order_service.pb',
        operation_count: 5,
      },
    ],
  },
  regression_defense: {
    gate_evaluations_30d: 8,
    blocked_count_30d: 0,
    pass_rate_30d: 1.0,
    unhealthy_suite_processes_7d: 0,
  },
};

export function testDashboard_RendersHealthSubScores_DriftRadar_AndOrphanDeepLinks(): void {
  const subsystems = [
    {
      id: '1',
      code: 'SUB_TRADE',
      name: '交易子系统',
      created_at: '',
      updated_at: '',
    },
  ];

  const html = renderToString(
    <MemoryRouter initialEntries={['/dashboard']}>
      <DashboardWorkbenchPage
        initialData={mockOverview}
        initialSubsystems={subsystems}
      />
    </MemoryRouter>
  );

  // 1. 标题与子系统选择器
  expect(html).toContain('平台级契约治理全景大盘');
  expect(html).toContain('全系统 (全局视图)');
  expect(html).toContain('交易子系统 (SUB_TRADE)');

  // 2. 页面包含微前端四大视窗
  // 视窗 1：健康指数
  expect(html).toContain('微服务契约健康指数');
  expect(html).toContain('70');
  expect(html).toContain('亚健康 (SUBHEALTH)');
  expect(html).toContain('架构防漂移子分 (权重 40)');
  expect(html).toContain('40 / 40');
  expect(html).toContain('契约资产认领子分 (权重 30)');
  expect(html).toContain('0 / 30');
  expect(html).toContain('防劣化回归子分 (权重 30)');
  expect(html).toContain('30 / 30');
  expect(html).toContain('存在 15 个生效 Proto 服务契约未被功能模块认领');

  // 视窗 2：架构漂移雷达
  expect(html).toContain('架构漂移雷达');
  expect(html).toContain('已核验合规调用边');
  expect(html).toContain('活跃架构漂移违规边');
  expect(html).toContain('前往拓扑全景');

  // 视窗 3：契约资产认领看板与深链跳转
  expect(html).toContain('契约资产认领看板');
  expect(html).toContain('认领孤儿契约');
  expect(html).toContain('生效 Proto 服务总数');
  expect(html).toContain('未认领孤儿契约');
  expect(html).toContain('dophi.order.v1.OrderService');
  expect(html).toContain('前往认领');

  // 视窗 4：防劣化回归防线
  expect(html).toContain('防劣化回归防线');
  expect(html).toContain('近 30 天门禁评估');
  expect(html).toContain('破坏性阻断次数');
  expect(html).toContain('门禁通过率');
  expect(html).toContain('近 7 天亚健康进程');
}

export function testDashboard_F24_RendersStaticComplianceCardWithoutAlteringMainScore(): void {
  const overviewWithStatic: DashboardOverviewDTO = {
    ...mockOverview,
    static_compliance: {
      compliance_score: 88,
      comment_coverage_rate: 76.5,
      naming_pass_rate: 94.0,
      duplicate_group_count: 2,
      pending_vocab_terms: 4,
      recent_mr_blocked_count: 1,
      total_proto_files: 19,
    },
  };

  const html = renderToStaticMarkup(
    <MemoryRouter initialEntries={['/dashboard']}>
      <DashboardWorkbenchPage initialData={overviewWithStatic} initialSubsystems={[]} />
    </MemoryRouter>
  );

  // 主健康分公式（40 + 0 + 30 = 70）保持严格不变
  expect(html).toContain('微服务契约健康指数');
  expect(html).toContain('70');

  // F-24 独立静态合规卡片指标正确渲染
  expect(html).toContain('static-compliance-card');
  expect(html).toContain('视窗五 · .proto 静态治理合规大盘');
  expect(html).toContain('88');
  expect(html).toContain('76.5%');
  expect(html).toContain('94.0%');
  expect(html).toContain('前往 Git MR 四合一门禁');
  expect(html).toContain('纳入治理源文件：19 份');
}

describe('DashboardWorkbenchPage Test Suite', () => {
  it('TC-DASH-01: 大盘健康分、漂移雷达与孤儿契约深链渲染测试', () => {
    testDashboard_RendersHealthSubScores_DriftRadar_AndOrphanDeepLinks();
  });

  it('TC-DASH-02 (F-24): 静态治理合规卡片独立渲染且不改动 40+30+30 主健康分总分', () => {
    testDashboard_F24_RendersStaticComplianceCardWithoutAlteringMainScore();
  });
});
