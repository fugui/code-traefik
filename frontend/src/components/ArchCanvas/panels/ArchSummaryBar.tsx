import React from 'react';
import type { ConsistencyReportDTO, ConsistencyEdge } from '../../../types/traffic';
import { CheckCircle2, AlertOctagon, HelpCircle, EyeOff, MinusCircle, Radio } from 'lucide-react';

export interface ArchSummaryBarProps {
  report: ConsistencyReportDTO;
  unattributedPendingList: ConsistencyEdge[];
  onOpenPendingList?: () => void;
}

export const ArchSummaryBar: React.FC<ArchSummaryBarProps> = ({
  report,
  unattributedPendingList,
  onOpenPendingList,
}) => {
  const verified = report.verified_count ?? report.stats?.VERIFIED ?? 0;
  const violation = report.violation_count ?? report.stats?.VIOLATION ?? 0;
  const unattributed = report.unattributed_count ?? report.stats?.UNATTRIBUTED ?? 0;
  const uncovered = report.uncovered_count ?? report.stats?.UNCOVERED ?? 0;
  const notMonitored = report.not_monitored_count ?? report.stats?.NOT_MONITORED ?? 0;
  const notObservable = report.not_observable_count ?? report.stats?.NOT_OBSERVABLE ?? 0;

  return (
    <div className="code-arch-summary-bar">
      <span className="code-arch-summary-bar__title">一致性比对报告:</span>

      <div className="code-arch-summary-item code-arch-summary-item--verified" title="架构声明与实测运行完全一致">
        <CheckCircle2 size={13} />
        <span>已验证 ({verified})</span>
      </div>

      <div className="code-arch-summary-item code-arch-summary-item--violation" title="未声明越权调用或调用未登记接口">
        <AlertOctagon size={13} />
        <span>越权违规 ({violation})</span>
      </div>

      <div
        className={`code-arch-summary-item code-arch-summary-item--unattributed ${
          unattributedPendingList.length > 0 ? 'code-arch-summary-item--clickable' : ''
        }`}
        title="多模块共进程且缺失 TraceID 扩展头"
        onClick={unattributedPendingList.length > 0 ? onOpenPendingList : undefined}
      >
        <HelpCircle size={13} />
        <span>待定归属 ({unattributed})</span>
        {unattributedPendingList.length > 0 && (
          <span className="code-arch-summary-item__sub-tag">
            待定待办: {unattributedPendingList.length}
          </span>
        )}
      </div>

      <div className="code-arch-summary-item code-arch-summary-item--uncovered" title="已声明但本次协同测试未覆盖">
        <MinusCircle size={13} />
        <span>未触发 ({uncovered})</span>
      </div>

      <div className="code-arch-summary-item code-arch-summary-item--not-monitored" title="目标服务所在物理进程未加入本次监控">
        <Radio size={13} />
        <span>目标未纳管 ({notMonitored})</span>
      </div>

      <div className="code-arch-summary-item code-arch-summary-item--not-observable" title="同进程内调用走函数调用，不经网络栈">
        <EyeOff size={13} />
        <span>进程内不可观测 ({notObservable})</span>
      </div>
    </div>
  );
};

