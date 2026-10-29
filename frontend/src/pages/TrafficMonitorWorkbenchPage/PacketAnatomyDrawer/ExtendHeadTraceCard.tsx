import React from 'react';
import type { ExtendHeadAnatomyDTO } from '../../../types';

export interface ExtendHeadTraceCardProps {
  extendHead: ExtendHeadAnatomyDTO;
  onFilterTrace?: (traceId: string) => void;
}

export const ExtendHeadTraceCard: React.FC<ExtendHeadTraceCardProps> = ({ extendHead, onFilterTrace }) => {
  return (
    <div className="code-anatomy-card">
      <div className="code-anatomy-card__header">
        <div className="code-anatomy-card__title">③ 扩展头链路与排队耗时 (ExtendHead)</div>
        <div className="code-anatomy-card__tags">
          <span className="code-badge code-badge--purple">{extendHead.totalExtendBytes} Bytes</span>
          <span className="code-badge code-badge--secondary">{extendHead.items.length} 个扩展段</span>
        </div>
      </div>

      <div className="code-anatomy-card__body">
        {/* DOTR 全链路追踪标识 */}
        {extendHead.trace ? (
          <div className="code-meta-grid">
            <div className="code-meta-item">
              <span className="code-meta-label">全链路 TraceID:</span>
              <div className="code-trace-link-wrap">
                <span className="code-meta-val code-font-mono code-color-primary">{extendHead.trace.traceIdHex}</span>
                {onFilterTrace && (
                  <button
                    type="button"
                    className="code-btn-text"
                    onClick={() => onFilterTrace(extendHead.trace?.traceIdHex ?? '')}
                    title="一键筛选同批次所有该 TraceID 的报文"
                  >
                    按此 Trace 筛选
                  </button>
                )}
              </div>
            </div>
            <div className="code-meta-item">
              <span className="code-meta-label">本节点 SpanID:</span>
              <span className="code-meta-val code-font-mono">{extendHead.trace.spanIdHex}</span>
            </div>
            <div className="code-meta-item">
              <span className="code-meta-label">父节点 FatherSpanID:</span>
              <span className="code-meta-val code-font-mono">{extendHead.trace.fatherSpanIdHex}</span>
            </div>
            <div className="code-meta-item">
              <span className="code-meta-label">录制开关 (Switch):</span>
              <span className="code-meta-val code-font-mono">{extendHead.trace.recordMsgSwitch}</span>
            </div>
          </div>
        ) : (
          <div className="code-color-muted code-text-sm">本帧无 DOTR 链路追踪扩展头</div>
        )}

        {/* 入队耗时 */}
        {extendHead.queueWaitUs !== undefined && (
          <div className="code-timing-box code-mt-sm">
            <div className="code-timing-title">服务端入队等待指标</div>
            <div className="code-timing-bars">
              <div className="code-timing-item">
                <span className="code-timing-label">入队等待耗时:</span>
                <span className="code-timing-val">{extendHead.queueWaitUs} μs</span>
              </div>
              {extendHead.serverEnqueueTime && (
                <div className="code-timing-item">
                  <span className="code-timing-label">入队时间戳:</span>
                  <span className="code-timing-val code-font-mono">{extendHead.serverEnqueueTime}</span>
                </div>
              )}
            </div>
          </div>
        )}

        {/* 扩展头原始段列表 */}
        {(extendHead?.items?.length ?? 0) > 0 && (
          <div className="code-table-wrapper code-mt-sm">
            <table className="code-table code-table--compact">
              <thead>
                <tr>
                  <th>类型码 (ucType)</th>
                  <th>段描述</th>
                  <th>长度</th>
                  <th>原始十六进制载荷</th>
                </tr>
              </thead>
              <tbody>
                {(extendHead?.items ?? []).map((item, idx) => (
                  <tr key={`${item.ucType}-${idx}`}>
                    <td className="code-font-mono">{item.ucType}</td>
                    <td>{item.typeName}</td>
                    <td className="code-font-mono">{item.usHeadLen}B</td>
                    <td className="code-font-mono code-color-muted code-text-xs">{item.rawHex}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
};
