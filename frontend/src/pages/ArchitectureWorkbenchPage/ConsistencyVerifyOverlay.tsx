import React from 'react';
import type { ConsistencyReportDTO } from '../../types';

export interface ConsistencyVerifyOverlayProps {
  report: ConsistencyReportDTO | null;
  loading?: boolean;
  onRefresh?: () => void;
}

export const ConsistencyVerifyOverlay: React.FC<ConsistencyVerifyOverlayProps> = ({
  report,
  loading = false,
  onRefresh,
}) => {
  if (loading) {
    return <div className="code-tpl-loading-screen">正在比对实测流量与静态架构契约...</div>;
  }

  if (!report) {
    return (
      <div className="code-empty-pane code-p-xl">
        请在顶部选择一个录制批次以执行六分类架构一致性验证对比。
      </div>
    );
  }

  const categoryMeta: Record<
    'VERIFIED' | 'VIOLATION' | 'UNCOVERED' | 'NOT_MONITORED' | 'NOT_OBSERVABLE' | 'UNATTRIBUTED',
    { label: string; badgeClass: string; desc: string }
  > = {
    VERIFIED: {
      label: '完全验证 (VERIFIED)',
      badgeClass: 'code-badge--success',
      desc: '实测调用契约与静态 PROVIDE/DEPEND 声明完全匹配',
    },
    VIOLATION: {
      label: '违规未声明 (VIOLATION)',
      badgeClass: 'code-badge--danger',
      desc: '抓取到跨模块调用但静态拓扑未显式声明依赖关系',
    },
    UNCOVERED: {
      label: '声明未覆盖 (UNCOVERED)',
      badgeClass: 'code-badge--secondary',
      desc: '已在架构模型声明契约，但本次录制流量中未触发调用',
    },
    NOT_MONITORED: {
      label: '目标未纳管 (NOT_MONITORED)',
      badgeClass: 'code-badge--warning',
      desc: '流量指向的目标下游进程未在此次录制监控列表中',
    },
    NOT_OBSERVABLE: {
      label: '盲区不可见 (NOT_OBSERVABLE)',
      badgeClass: 'code-badge--purple',
      desc: '同进程内部函数调用或无探针插桩',
    },
    UNATTRIBUTED: {
      label: '未归属模块 (UNATTRIBUTED)',
      badgeClass: 'code-badge--orange',
      desc: '源地址无法推导到具体业务模块',
    },
  };

  const rawStats = report.stats || ({} as Record<string, number>);
  const stats: Record<string, number> = {
    VERIFIED: rawStats.VERIFIED ?? report.verified_count ?? 0,
    VIOLATION: rawStats.VIOLATION ?? report.violation_count ?? 0,
    UNCOVERED: rawStats.UNCOVERED ?? report.uncovered_count ?? 0,
    NOT_MONITORED: rawStats.NOT_MONITORED ?? report.not_monitored_count ?? 0,
    NOT_OBSERVABLE: rawStats.NOT_OBSERVABLE ?? report.not_observable_count ?? 0,
    UNATTRIBUTED: rawStats.UNATTRIBUTED ?? report.unattributed_count ?? 0,
  };

  return (
    <div className="code-consistency-overlay">
      <div className="code-consistency-header">
        <div>
          <h3 className="code-consistency-title">六分类架构一致性验证报告</h3>
          <p className="code-consistency-subtitle">关联批次 ID: {report.session_id}</p>
        </div>
        {onRefresh && (
          <button
            type="button"
            className="code-btn code-btn--secondary code-btn--sm"
            onClick={onRefresh}
          >
            重新比对
          </button>
        )}
      </div>

      {/* 统计指标卡片 */}
      <div className="code-stats-grid">
        {(Object.keys(categoryMeta) as Array<keyof typeof categoryMeta>).map((catKey) => {
          const count = stats[catKey] ?? 0;
          const meta = categoryMeta[catKey];
          return (
            <div key={catKey} className="code-stat-card">
              <div className="code-stat-card__header">
                <span className={`code-badge ${meta.badgeClass}`}>{meta.label}</span>
              </div>
              <div className="code-stat-card__value">{count}</div>
              <div className="code-stat-card__desc">{meta.desc}</div>
            </div>
          );
        })}
      </div>

      {/* 边明细列表 */}
      <div className="code-consistency-edges code-mt-md">
        <h4 className="code-section-subhead">一致性比对边清单 ({(report.edges ?? []).length} 条)</h4>
        <div className="code-table-wrapper">
          <table className="code-table">
            <thead>
              <tr>
                <th>判定分类</th>
                <th>调用源模块</th>
                <th>目标下游模块</th>
                <th>服务与方法 (service-operation)</th>
                <th>实测调用次数</th>
                <th>诊断细节说明</th>
              </tr>
            </thead>
            <tbody>
              {(report.edges ?? []).map((edge, idx) => {
                const cat = (edge.category || edge.status || 'UNATTRIBUTED') as keyof typeof categoryMeta;
                const meta = categoryMeta[cat] || categoryMeta.UNATTRIBUTED;
                const callCount = edge.call_count ?? edge.observed_call_count ?? 0;
                const srcName = edge.source_module_name || edge.src_module_name || '-';
                const dstName = edge.target_module_name || edge.dst_module_name || '-';
                return (
                  <tr key={`${edge.service_name}-${edge.operation}-${idx}`}>
                    <td>
                      <span className={`code-badge ${meta?.badgeClass ?? 'code-badge--secondary'}`}>
                        {cat}
                      </span>
                    </td>
                    <td className="code-font-mono">{srcName}</td>
                    <td className="code-font-mono">{dstName}</td>
                    <td className="code-font-mono code-color-primary">
                      {edge.service_name}.{edge.operation}
                    </td>
                    <td className="code-font-mono">{callCount}</td>
                    <td className="code-color-muted code-text-sm">{edge.detail || '-'}</td>
                  </tr>
                );
              })}
              {(report.edges ?? []).length === 0 && (
                <tr>
                  <td colSpan={6} className="code-table-empty">
                    当前批次暂未分析出任何跨模块边记录
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
};
