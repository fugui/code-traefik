import React from 'react';
import type { MockTraceEvent } from '../../../types';

export interface MockHitMissTimelineProps {
  mockEvents?: MockTraceEvent[];
  itemMockTrace?: MockTraceEvent;
}

export const MockHitMissTimeline: React.FC<MockHitMissTimelineProps> = ({
  mockEvents,
  itemMockTrace,
}) => {
  const events = itemMockTrace ? [itemMockTrace] : mockEvents ?? [];

  return (
    <div className="code-anatomy-card">
      <div className="code-anatomy-card__header">
        <div className="code-anatomy-card__title">下游二进制 Mock 命中与自愈追踪</div>
        <div className="code-anatomy-card__tags">
          <span className="code-badge code-badge--info">{events.length} 次下游调用</span>
        </div>
      </div>

      <div className="code-anatomy-card__body">
        {events.length > 0 ? (
          <div className="code-table-wrapper">
            <table className="code-table code-table--compact">
              <thead>
                <tr>
                  <th>请求流水号 (SNO)</th>
                  <th>目标服务契约</th>
                  <th>调用方法</th>
                  <th>命中策略 (HitKind)</th>
                  <th>头部自愈字段回显</th>
                  <th>判定状态</th>
                </tr>
              </thead>
              <tbody>
                {(events ?? []).map((evt, idx) => (
                  <tr key={`${evt.sno}-${idx}`}>
                    <td className="code-font-mono">{evt.sno}</td>
                    <td className="code-font-mono code-color-primary">{evt.service_name}</td>
                    <td className="code-font-mono">{evt.operation}</td>
                    <td>
                      <span className={`code-badge ${evt.hit_kind === 'EXACT_HASH' ? 'code-badge--success' : evt.hit_kind === 'ORDER_FALLBACK' ? 'code-badge--warning' : 'code-badge--danger'}`}>
                        {evt.hit_kind}
                      </span>
                    </td>
                    <td>
                      <div className="code-tag-group">
                        {(evt.healed_fields ?? []).map((f, fIdx) => (
                          <span key={fIdx} className="code-tag code-tag--muted code-font-mono">
                            {f}
                          </span>
                        ))}
                        {(!evt.healed_fields || evt.healed_fields.length === 0) && (
                          <span className="code-color-muted code-text-xs">无</span>
                        )}
                      </div>
                    </td>
                    <td>
                      {evt.is_miss ? (
                        <span className="code-badge code-badge--danger">MOCK_MISS (Fail-Closed)</span>
                      ) : (
                        <span className="code-badge code-badge--success">MOCK_HIT</span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="code-empty-pane code-p-sm">
            本条请求执行期间未产生下游出站调用，或未触发 Mock 规则
          </div>
        )}
      </div>
    </div>
  );
};
