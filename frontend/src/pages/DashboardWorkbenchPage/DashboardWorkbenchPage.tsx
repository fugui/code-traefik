import React, { useCallback, useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  ShieldCheck,
  AlertTriangle,
  Radar,
  FileQuestion,
  History,
  ArrowUpRight,
  RefreshCw,
  CheckCircle2,
} from 'lucide-react';
import { listSubsystems } from '../../api/arch';
import { fetchDashboardOverview } from '../../api/dashboard';
import { useModulePath } from '../../hooks/useModulePath';
import type { Subsystem } from '../../types/arch';
import type {
  DashboardOverviewDTO,
  HealthGrade,
  HealthScoreDTO,
  DriftRadarDTO,
  ContractAssetsDTO,
  RegressionDefenseDTO,
} from '../../types/dashboard';
import './dashboard.css';

const GRADE_MAP: Record<HealthGrade, { label: string; className: string }> = {
  EXCELLENT: { label: '优秀 (EXCELLENT)', className: 'dashboard-badge--excellent' },
  GOOD: { label: '良好 (GOOD)', className: 'dashboard-badge--good' },
  SUBHEALTH: { label: '亚健康 (SUBHEALTH)', className: 'dashboard-badge--subhealth' },
  CRITICAL: { label: '高危 (CRITICAL)', className: 'dashboard-badge--critical' },
};

const GAUGE_CLASS_MAP: Record<HealthGrade, string> = {
  EXCELLENT: 'dashboard-score-gauge--excellent',
  GOOD: 'dashboard-score-gauge--good',
  SUBHEALTH: 'dashboard-score-gauge--subhealth',
  CRITICAL: 'dashboard-score-gauge--critical',
};

export interface HealthScoreCardProps {
  healthScore: HealthScoreDTO;
}

export const HealthScoreCard: React.FC<HealthScoreCardProps> = ({ healthScore }) => {
  const gradeInfo = GRADE_MAP[healthScore.grade] || GRADE_MAP.EXCELLENT;
  const gaugeClass = GAUGE_CLASS_MAP[healthScore.grade] || GAUGE_CLASS_MAP.EXCELLENT;

  return (
    <section className="dashboard-card" data-testid="dashboard-health-card">
      <div className="dashboard-card__header">
        <h2 className="dashboard-card__title">
          <ShieldCheck size={18} />
          <span>微服务契约健康指数</span>
        </h2>
        <span className={`dashboard-badge ${gradeInfo.className}`}>
          {gradeInfo.label}
        </span>
      </div>

      <div className="dashboard-card__body">
        <div className="dashboard-score-overview">
          <div className={`dashboard-score-gauge ${gaugeClass}`}>
            <span className="dashboard-score-value">{healthScore.total}</span>
            <span className="dashboard-score-label">总健康分</span>
          </div>

          <div className="dashboard-score-meta">
            <div className="dashboard-subscore-bar-group">
              {/* 架构防漂移子分 */}
              <div className="dashboard-subscore-item">
                <div className="dashboard-subscore-header">
                  <span>架构防漂移子分 (权重 40)</span>
                  <strong>{`${healthScore.arch_sub_score} / 40`}</strong>
                </div>
                <div className="dashboard-progress-track">
                  <div
                    className="dashboard-progress-fill dashboard-progress-fill--arch"
                    style={{ width: `${(healthScore.arch_sub_score / 40) * 100}%` }}
                  />
                </div>
              </div>

              {/* 契约资产认领子分 */}
              <div className="dashboard-subscore-item">
                <div className="dashboard-subscore-header">
                  <span>契约资产认领子分 (权重 30)</span>
                  <strong>{`${healthScore.asset_sub_score} / 30`}</strong>
                </div>
                <div className="dashboard-progress-track">
                  <div
                    className="dashboard-progress-fill dashboard-progress-fill--asset"
                    style={{ width: `${(healthScore.asset_sub_score / 30) * 100}%` }}
                  />
                </div>
              </div>

              {/* 防劣化回归子分 */}
              <div className="dashboard-subscore-item">
                <div className="dashboard-subscore-header">
                  <span>防劣化回归子分 (权重 30)</span>
                  <strong>{`${healthScore.regression_sub_score} / 30`}</strong>
                </div>
                <div className="dashboard-progress-track">
                  <div
                    className="dashboard-progress-fill dashboard-progress-fill--reg"
                    style={{ width: `${(healthScore.regression_sub_score / 30) * 100}%` }}
                  />
                </div>
              </div>
            </div>
          </div>
        </div>

        {/* 结构化整改治理行动指南 */}
        <div className="dashboard-hints-container">
          {healthScore.action_hints.length === 0 ? (
            <div className="dashboard-hint-box dashboard-hint-box--healthy">
              <CheckCircle2 size={16} />
              <span>全系统架构与契约治理处于极佳状态，无活跃违规或未认领孤儿契约</span>
            </div>
          ) : (
            healthScore.action_hints.map((hint, idx) => (
              <div key={idx} className="dashboard-hint-box">
                <AlertTriangle size={16} style={{ flexShrink: 0, marginTop: '2px' }} />
                <span>{hint}</span>
              </div>
            ))
          )}
        </div>
      </div>
    </section>
  );
};

export interface DriftRadarCardProps {
  driftRadar: DriftRadarDTO;
  onNavigateTopology: () => void;
}

export const DriftRadarCard: React.FC<DriftRadarCardProps> = ({ driftRadar, onNavigateTopology }) => {
  return (
    <section className="dashboard-card" data-testid="dashboard-drift-radar">
      <div className="dashboard-card__header">
        <h2 className="dashboard-card__title">
          <Radar size={18} />
          <span>架构漂移雷达</span>
        </h2>
        <button
          type="button"
          className="dashboard-link-btn"
          onClick={onNavigateTopology}
          title="穿透前往架构拓扑全景"
        >
          <span>前往拓扑全景</span>
          <ArrowUpRight size={14} style={{ display: 'inline', verticalAlign: 'middle' }} />
        </button>
      </div>

      <div className="dashboard-card__body">
        <div className="dashboard-stat-row">
          <div className="dashboard-stat-tile">
            <span className="dashboard-stat-tile__value dashboard-stat-tile__value--success">
              {driftRadar.verified_edges}
            </span>
            <span className="dashboard-stat-tile__label">已核验合规调用边</span>
          </div>
          <div className="dashboard-stat-tile">
            <span
              className={`dashboard-stat-tile__value ${driftRadar.active_violation_edges > 0 ? 'dashboard-stat-tile__value--danger' : ''}`}
            >
              {driftRadar.active_violation_edges}
            </span>
            <span className="dashboard-stat-tile__label">活跃架构漂移违规边</span>
          </div>
        </div>

        <div className="dashboard-table-container">
          <table className="dashboard-table">
            <thead>
              <tr>
                <th>违规标准架构进程 (TOP 5)</th>
                <th>所属子系统</th>
                <th>违规调用边数</th>
                <th>排查指引</th>
              </tr>
            </thead>
            <tbody>
              {driftRadar.top_violated_processes.length === 0 ? (
                <tr>
                  <td colSpan={4} className="dashboard-table__empty">
                    🎉 暂无实测架构漂移调用，所有进程调用均已声明 DEPEND 契约
                  </td>
                </tr>
              ) : (
                driftRadar.top_violated_processes.map((proc) => (
                  <tr key={proc.arch_process_id}>
                    <td>
                      <strong>{proc.arch_process_name}</strong>
                      <div style={{ fontSize: '0.75rem', color: 'var(--color-text-muted)' }}>
                        {proc.arch_process_code}
                      </div>
                    </td>
                    <td>{proc.subsystem_name || '全局共享'}</td>
                    <td>
                      <span style={{ color: 'var(--color-danger)', fontWeight: 600 }}>
                        {proc.violation_count} 条
                      </span>
                    </td>
                    <td>
                      <button
                        type="button"
                        className="dashboard-link-btn"
                        onClick={onNavigateTopology}
                      >
                        排查调用拓扑
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>
    </section>
  );
};

export interface ContractAssetsCardProps {
  contractAssets: ContractAssetsDTO;
  onNavigateSchemas: () => void;
}

export const ContractAssetsCard: React.FC<ContractAssetsCardProps> = ({ contractAssets, onNavigateSchemas }) => {
  return (
    <section className="dashboard-card" data-testid="dashboard-contract-assets">
      <div className="dashboard-card__header">
        <h2 className="dashboard-card__title">
          <FileQuestion size={18} />
          <span>契约资产认领看板</span>
        </h2>
        <button
          type="button"
          className="dashboard-link-btn"
          onClick={onNavigateSchemas}
          title="直达未认领孤儿契约清单"
        >
          <span>认领孤儿契约</span>
          <ArrowUpRight size={14} style={{ display: 'inline', verticalAlign: 'middle' }} />
        </button>
      </div>

      <div className="dashboard-card__body">
        <div className="dashboard-stat-row">
          <div className="dashboard-stat-tile">
            <span className="dashboard-stat-tile__value">
              {contractAssets.total_active_services}
            </span>
            <span className="dashboard-stat-tile__label">生效 Proto 服务总数</span>
          </div>
          <div className="dashboard-stat-tile">
            <span className="dashboard-stat-tile__value dashboard-stat-tile__value--success">
              {contractAssets.claimed_services}
            </span>
            <span className="dashboard-stat-tile__label">已认领服务契约</span>
          </div>
          <div className="dashboard-stat-tile">
            <span
              className={`dashboard-stat-tile__value ${contractAssets.orphan_services > 0 ? 'dashboard-stat-tile__value--danger' : ''}`}
            >
              {contractAssets.orphan_services}
            </span>
            <span className="dashboard-stat-tile__label">未认领孤儿契约</span>
          </div>
          <div className="dashboard-stat-tile">
            <span className="dashboard-stat-tile__value">
              {(contractAssets.claimed_ratio * 100).toFixed(1)}%
            </span>
            <span className="dashboard-stat-tile__label">契约认领覆盖率</span>
          </div>
          <div className="dashboard-stat-tile">
            <span className="dashboard-stat-tile__value" data-testid="multi-impl-contract-count">
              {contractAssets.multi_impl_contract_count ?? 0}
            </span>
            <span className="dashboard-stat-tile__label">多实现契约数</span>
          </div>
          <div className="dashboard-stat-tile">
            <span className="dashboard-stat-tile__value" data-testid="multi-impl-service-count">
              {contractAssets.multi_impl_service_count ?? 0}
            </span>
            <span className="dashboard-stat-tile__label">实现分支服务数</span>
          </div>
        </div>

        <div className="dashboard-table-container">
          <table className="dashboard-table">
            <thead>
              <tr>
                <th>未认领服务契约名</th>
                <th>所属描述集</th>
                <th>RPC 方法数</th>
                <th>治理动作</th>
              </tr>
            </thead>
            <tbody>
              {contractAssets.top_orphan_services.length === 0 ? (
                <tr>
                  <td colSpan={4} className="dashboard-table__empty">
                    ✅ 所有生效 RPC 服务均已绑定功能模块，无遗留孤儿契约
                  </td>
                </tr>
              ) : (
                contractAssets.top_orphan_services.map((orphan) => (
                  <tr key={orphan.service_name}>
                    <td>
                      <strong>{orphan.service_name}</strong>
                    </td>
                    <td>{orphan.schema_name || `Schema #${orphan.schema_id}`}</td>
                    <td>{orphan.operation_count} 个接口</td>
                    <td>
                      <button
                        type="button"
                        className="dashboard-link-btn"
                        onClick={onNavigateSchemas}
                      >
                        前往认领
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>
    </section>
  );
};

export interface RegressionDefenseCardProps {
  regressionDefense: RegressionDefenseDTO;
}

export const RegressionDefenseCard: React.FC<RegressionDefenseCardProps> = ({ regressionDefense }) => {
  return (
    <section className="dashboard-card" data-testid="dashboard-regression-defense">
      <div className="dashboard-card__header">
        <h2 className="dashboard-card__title">
          <History size={18} />
          <span>防劣化回归防线</span>
        </h2>
        <span className="dashboard-card__meta">
          统计权威套件与 CI 门禁任务 (排除个人调试重放)
        </span>
      </div>

      <div className="dashboard-card__body">
        <div className="dashboard-stat-row">
          <div className="dashboard-stat-tile">
            <span className="dashboard-stat-tile__value">
              {regressionDefense.gate_evaluations_30d}
            </span>
            <span className="dashboard-stat-tile__label">近 30 天门禁评估</span>
          </div>
          <div className="dashboard-stat-tile">
            <span
              className={`dashboard-stat-tile__value ${regressionDefense.blocked_count_30d > 0 ? 'dashboard-stat-tile__value--danger' : ''}`}
            >
              {regressionDefense.blocked_count_30d}
            </span>
            <span className="dashboard-stat-tile__label">破坏性阻断次数</span>
          </div>
          <div className="dashboard-stat-tile">
            <span className="dashboard-stat-tile__value dashboard-stat-tile__value--success">
              {(regressionDefense.pass_rate_30d * 100).toFixed(1)}%
            </span>
            <span className="dashboard-stat-tile__label">门禁通过率</span>
          </div>
          <div className="dashboard-stat-tile">
            <span
              className={`dashboard-stat-tile__value ${regressionDefense.unhealthy_suite_processes_7d > 0 ? 'dashboard-stat-tile__value--danger' : ''}`}
            >
              {regressionDefense.unhealthy_suite_processes_7d}
            </span>
            <span className="dashboard-stat-tile__label">近 7 天亚健康进程</span>
          </div>
        </div>

        <div className="dashboard-hints-container" style={{ marginTop: '0.25rem' }}>
          {regressionDefense.unhealthy_suite_processes_7d === 0 ? (
            <div className="dashboard-hint-box dashboard-hint-box--healthy">
              <CheckCircle2 size={16} />
              <span>黄金套件与 CI 门禁防线稳固，近期无破坏性回归缺陷或亚健康标准进程</span>
            </div>
          ) : (
            <div className="dashboard-hint-box">
              <AlertTriangle size={16} style={{ flexShrink: 0, marginTop: '2px' }} />
              <span>
                检测到近 7 天有 {regressionDefense.unhealthy_suite_processes_7d} 个标准架构进程的权威套件回归发生 FAILED 或阻塞，请排查破坏性变更
              </span>
            </div>
          )}
        </div>
      </div>
    </section>
  );
};

export interface DashboardWorkbenchPageProps {
  initialData?: DashboardOverviewDTO;
  initialSubsystems?: Subsystem[];
}

export const DashboardWorkbenchPage: React.FC<DashboardWorkbenchPageProps> = ({
  initialData,
  initialSubsystems,
}) => {
  const navigate = useNavigate();
  const resolvePath = useModulePath();

  const [subsystems, setSubsystems] = useState<Subsystem[]>(initialSubsystems || []);
  const [selectedSubsystemId, setSelectedSubsystemId] = useState<number | undefined>(undefined);
  const [data, setData] = useState<DashboardOverviewDTO | null>(initialData || null);
  const [loading, setLoading] = useState<boolean>(!initialData);
  const [error, setError] = useState<string | null>(null);

  // 加载子系统下拉列表
  useEffect(() => {
    if (initialSubsystems && initialSubsystems.length > 0) return;
    let active = true;
    void (async () => {
      try {
        const res = await listSubsystems({ page_size: 100 });
        if (active && res.items) {
          setSubsystems(res.items);
        }
      } catch {
        // 子系统加载失败不阻塞大盘渲染
      }
    })();
    return () => {
      active = false;
    };
  }, [initialSubsystems]);

  // 加载大盘聚合指标
  const loadOverview = useCallback(async (subsystemId?: number) => {
    setLoading(true);
    setError(null);
    try {
      const resp = await fetchDashboardOverview(subsystemId);
      setData(resp);
    } catch (err) {
      setError(err instanceof Error ? err.message : '获取治理大盘聚合数据失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (initialData && selectedSubsystemId === undefined) return;
    void loadOverview(selectedSubsystemId);
  }, [loadOverview, selectedSubsystemId, initialData]);

  const handleSubsystemChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const val = e.target.value;
    setSelectedSubsystemId(val ? Number(val) : undefined);
  };

  const handleNavigateTopology = () => {
    navigate(resolvePath('/topology'));
  };

  const handleNavigateSchemas = () => {
    navigate(resolvePath('/schemas?tab=catalog&claimed_status=UNCLAIMED'));
  };

  return (
    <div className="dashboard-workbench">
      <header className="dashboard-workbench__header">
        <div className="dashboard-workbench__title-group">
          <h1 className="dashboard-workbench__title">平台级契约治理全景大盘</h1>
          <p className="dashboard-workbench__subtitle">
            基于活体真实流量、Protobuf AST 与黄金用例套件驱动的三维封顶微服务健康度治理工作台
          </p>
        </div>

        <div className="dashboard-workbench__controls">
          <select
            className="dashboard-workbench__subsystem-select"
            value={selectedSubsystemId !== undefined ? String(selectedSubsystemId) : ''}
            onChange={handleSubsystemChange}
            aria-label="选择子系统过滤"
          >
            <option value="">全系统 (全局视图)</option>
            {subsystems.map((sub) => (
              <option key={sub.id} value={sub.id}>
                {`${sub.name} (${sub.code})`}
              </option>
            ))}
          </select>

          <button
            type="button"
            className="code-tpl-btn code-tpl-btn--default"
            onClick={() => void loadOverview(selectedSubsystemId)}
            title="刷新大盘数据"
          >
            <RefreshCw size={14} />
            <span>刷新</span>
          </button>
        </div>
      </header>

      {error && (
        <div
          className="dashboard-hint-box"
          style={{
            borderColor: 'var(--color-danger-border)',
            background: 'var(--color-danger-subtle)',
            color: 'var(--color-danger)',
          }}
        >
          <AlertTriangle size={16} />
          <span>{error}</span>
        </div>
      )}

      {loading && !data && (
        <div className="dashboard-card" style={{ textAlign: 'center', padding: '3rem' }}>
          <div className="code-tpl-loading-spinner" />
          <p style={{ marginTop: '1rem', color: 'var(--color-text-muted)' }}>正在聚合计算全系统治理指标...</p>
        </div>
      )}

      {data && (
        <div className="dashboard-workbench__grid">
          {/* 视窗一：健康指数与整改建议卡 */}
          <HealthScoreCard healthScore={data.health_score} />

          {/* 视窗二：架构漂移雷达 */}
          <DriftRadarCard
            driftRadar={data.drift_radar}
            onNavigateTopology={handleNavigateTopology}
          />

          {/* 视窗三：契约资产认领看板 */}
          <ContractAssetsCard
            contractAssets={data.contract_assets}
            onNavigateSchemas={handleNavigateSchemas}
          />

          {/* 视窗四：防劣化回归防线 */}
          <RegressionDefenseCard regressionDefense={data.regression_defense} />
        </div>
      )}
    </div>
  );
};

export default DashboardWorkbenchPage;
