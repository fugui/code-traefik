import React from 'react';
import { ArrowUpRight, FileCode2 } from 'lucide-react';
import type { StaticComplianceDTO } from '../../types/dashboard';

export interface StaticComplianceCardProps {
  staticCompliance: StaticComplianceDTO;
  onNavigateMrGates: () => void;
  onNavigateProtoSources: () => void;
}

/**
 * 视窗五 (F-24): .proto 静态治理合规度大盘独立雷达卡。
 * 与主健康分（40+30+30 活体/资产/回归三维公式）完全解耦，独立展示注释覆盖率、命名合规率、AST 查重簇、待核词条与近期 MR 阻断数。
 */
export const StaticComplianceCard: React.FC<StaticComplianceCardProps> = ({
  staticCompliance,
  onNavigateMrGates,
  onNavigateProtoSources,
}) => {
  const complianceScore = staticCompliance.compliance_score ?? 100;
  const commentRate = staticCompliance.comment_coverage_rate ?? 0;
  const namingRate = staticCompliance.naming_pass_rate ?? 100;

  return (
    <section
      className="dashboard-card dashboard-card--full"
      aria-label="契约静态规范合规雷达"
      data-testid="static-compliance-card"
    >
      <div className="dashboard-card__header">
        <h2 className="dashboard-card__title">
          <FileCode2 size={18} />
          <span>视窗五 · .proto 静态治理合规大盘</span>
        </h2>
        <div className="dashboard-compliance-actions">
          <span className="dashboard-card__meta">
            纳入治理源文件：{staticCompliance.total_proto_files} 份
          </span>
          <button
            type="button"
            className="dashboard-link-btn"
            onClick={onNavigateMrGates}
          >
            前往 Git MR 四合一门禁 <ArrowUpRight size={13} />
          </button>
          <button
            type="button"
            className="dashboard-link-btn"
            onClick={onNavigateProtoSources}
          >
            前往契约源码体检台 <ArrowUpRight size={13} />
          </button>
        </div>
      </div>

      <div className="dashboard-stat-row">
        <div className="dashboard-stat-tile">
          <span
            className={`dashboard-stat-tile__value ${
              complianceScore >= 85
                ? 'dashboard-stat-tile__value--success'
                : complianceScore < 60
                  ? 'dashboard-stat-tile__value--danger'
                  : 'dashboard-stat-tile__value--warning'
            }`}
          >
            {complianceScore}
          </span>
          <span className="dashboard-stat-tile__label">静态综合合规分 (0~100)</span>
        </div>

        <div className="dashboard-stat-tile">
          <span className="dashboard-stat-tile__value">{commentRate.toFixed(1)}%</span>
          <span className="dashboard-stat-tile__label">字段与消息注释覆盖率</span>
        </div>

        <div className="dashboard-stat-tile">
          <span className="dashboard-stat-tile__value dashboard-stat-tile__value--success">
            {namingRate.toFixed(1)}%
          </span>
          <span className="dashboard-stat-tile__label">静态命名与词汇合规率</span>
        </div>

        <div className="dashboard-stat-tile">
          <span
            className={`dashboard-stat-tile__value ${
              staticCompliance.duplicate_group_count > 0 ? 'dashboard-stat-tile__value--warning' : ''
            }`}
          >
            {staticCompliance.duplicate_group_count}
          </span>
          <span className="dashboard-stat-tile__label">AST 高相似重复结构体簇</span>
        </div>

        <div className="dashboard-stat-tile">
          <span
            className={`dashboard-stat-tile__value ${
              staticCompliance.pending_vocab_terms > 0 ? 'dashboard-stat-tile__value--warning' : ''
            }`}
          >
            {staticCompliance.pending_vocab_terms}
          </span>
          <span className="dashboard-stat-tile__label">待审理业务术语词条</span>
        </div>

        <div className="dashboard-stat-tile">
          <span
            className={`dashboard-stat-tile__value ${
              staticCompliance.recent_mr_blocked_count > 0 ? 'dashboard-stat-tile__value--danger' : ''
            }`}
          >
            {staticCompliance.recent_mr_blocked_count}
          </span>
          <span className="dashboard-stat-tile__label">近期 MR 门禁阻断次数</span>
        </div>
      </div>
    </section>
  );
};

export default StaticComplianceCard;
