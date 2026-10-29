import React from 'react';
import type { ReplayItem } from '../../../types';

export interface HeaderAssertionCardProps {
  item: ReplayItem;
}

export const HeaderAssertionCard: React.FC<HeaderAssertionCardProps> = ({ item }) => {
  return (
    <div className="code-anatomy-card">
      <div className="code-anatomy-card__header">
        <div className="code-anatomy-card__title">第一层：CROS RPC 协议头级断言 (Layer 1)</div>
        <span className={`code-badge ${item.header_diff_passed ? 'code-badge--success' : 'code-badge--danger'}`}>
          {item.header_diff_passed ? '头层比对通过 (PASS)' : '头层断言失败 (FAILED)'}
        </span>
      </div>

      <div className="code-anatomy-card__body">
        <div className="code-meta-grid">
          <div className="code-meta-item">
            <span className="code-meta-label">基线应答状态码:</span>
            <span className="code-meta-val code-font-mono">{item.baseline_status_code ?? 0}</span>
          </div>
          <div className="code-meta-item">
            <span className="code-meta-label">实际回收状态码:</span>
            <span className={`code-meta-val code-font-mono ${item.actual_status_code === item.baseline_status_code ? 'code-color-success' : 'code-color-danger'}`}>
              {item.actual_status_code ?? '-'}
            </span>
          </div>
          <div className="code-meta-item">
            <span className="code-meta-label">重放执行耗时:</span>
            <span className="code-meta-val code-font-mono">{item.latency_us} μs</span>
          </div>
        </div>

        <div className="code-info-box code-mt-sm">
          <strong>头层豁免原则：</strong>
          服务侧生成的时间戳（<code>ullServerRecvTime</code>、<code>ullServerSendTime</code>）以及客户端运行态回显字段（<code>ullSNO</code>、<code>aucTraceBuff</code>、<code>ullClientSendTime</code>）由 Agent 本地动态自愈，永久豁免比对。
        </div>
      </div>
    </div>
  );
};
