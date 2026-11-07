import React from 'react';
import type { RpcLatencyWaterfallProps } from './types';

export const RpcLatencyWaterfallBar: React.FC<RpcLatencyWaterfallProps> = ({ data }) => {
  const timing =
    data.rspSide?.sessionHead.timingBreakdownUs ||
    data.reqSide?.sessionHead.timingBreakdownUs;

  const queueWaitFromExtend =
    data.reqSide?.extendHead.queueWaitUs ||
    data.rspSide?.extendHead.queueWaitUs;

  const clientToServerUs = timing?.clientToServerUs ?? 0;
  const queueWaitUs = timing?.queueWaitUs ?? queueWaitFromExtend ?? 0;
  const serverExecUs = timing?.serverExecUs ?? timing?.serverProcessUs ?? 0;
  const networkAndFramingUs = timing?.networkAndFramingUs ?? 0;

  const totalCalculatedUs = clientToServerUs + queueWaitUs + serverExecUs + networkAndFramingUs;
  const totalUs = data.roundTripLatencyUs && data.roundTripLatencyUs > totalCalculatedUs
    ? data.roundTripLatencyUs
    : totalCalculatedUs;

  if (totalUs <= 0) {
    return null;
  }

  const clientPct = totalUs > 0 ? Math.max(2, Math.round((clientToServerUs / totalUs) * 100)) : 0;
  const queuePct = totalUs > 0 ? Math.max(2, Math.round((queueWaitUs / totalUs) * 100)) : 0;
  const serverPct = totalUs > 0 ? Math.max(2, Math.round((serverExecUs / totalUs) * 100)) : 0;
  const remainingPct = Math.max(0, 100 - clientPct - queuePct - serverPct);

  // 诊断结论
  let diagnosis = '各阶段耗时平稳';
  if (serverExecUs > totalUs * 0.7) {
    diagnosis = `耗时主要集中在服务端业务执行 (${Math.round((serverExecUs / totalUs) * 100)}%)，网络与排队无延迟`;
  } else if (queueWaitUs > totalUs * 0.4) {
    diagnosis = `网关或线程池排队等待耗时偏高 (${Math.round((queueWaitUs / totalUs) * 100)}%)，建议关注服务端并发瓶颈`;
  } else if (clientToServerUs > totalUs * 0.5) {
    diagnosis = `客户端网络传输耗时占主导 (${Math.round((clientToServerUs / totalUs) * 100)}%)，可能受网络跨机房或慢链路影响`;
  }

  return (
    <div className="code-latency-waterfall-card" data-testid="rpc-latency-waterfall">
      <div className="code-latency-waterfall-header">
        <div className="code-latency-waterfall-title">
          <span>⏱️ 微秒级调用阶段耗时瀑布</span>
          <span className="code-font-mono code-color-primary code-ml-xs">
            {totalUs} μs {totalUs >= 1000 ? `(${(totalUs / 1000).toFixed(2)} ms)` : ''}
          </span>
        </div>
        <div className="code-latency-diagnosis code-text-xs code-color-muted">
          💡 {diagnosis}
        </div>
      </div>

      {/* 比例横向甘特条 */}
      <div className="code-waterfall-bar-track">
        {clientToServerUs > 0 && (
          <div
            className="code-waterfall-segment code-waterfall-segment--client"
            style={{ width: `${clientPct}%` }}
            title={`客户端传输: ${clientToServerUs} μs (${clientPct}%)`}
          >
            {clientPct > 12 && <span>传输 {clientToServerUs}μs</span>}
          </div>
        )}
        {queueWaitUs > 0 && (
          <div
            className="code-waterfall-segment code-waterfall-segment--queue"
            style={{ width: `${queuePct}%` }}
            title={`入队排队: ${queueWaitUs} μs (${queuePct}%)`}
          >
            {queuePct > 12 && <span>排队 {queueWaitUs}μs</span>}
          </div>
        )}
        {serverExecUs > 0 && (
          <div
            className="code-waterfall-segment code-waterfall-segment--server"
            style={{ width: `${serverPct}%` }}
            title={`服务端业务执行: ${serverExecUs} μs (${serverPct}%)`}
          >
            {serverPct > 12 && <span>服务处理 {serverExecUs}μs</span>}
          </div>
        )}
        {remainingPct > 5 && (
          <div
            className="code-waterfall-segment code-waterfall-segment--other"
            style={{ width: `${remainingPct}%` }}
            title={`网络回传与分帧: ${totalUs - clientToServerUs - queueWaitUs - serverExecUs} μs`}
          />
        )}
      </div>

      {/* 图例与详细微秒列表 */}
      <div className="code-waterfall-legend">
        <div className="code-waterfall-legend-item">
          <span className="code-legend-dot code-bg-info" />
          <span className="code-color-muted">客户端传输:</span>
          <span className="code-font-mono">{clientToServerUs} μs</span>
        </div>
        <div className="code-waterfall-legend-item">
          <span className="code-legend-dot code-bg-warning" />
          <span className="code-color-muted">入队排队:</span>
          <span className="code-font-mono">{queueWaitUs} μs</span>
        </div>
        <div className="code-waterfall-legend-item">
          <span className="code-legend-dot code-bg-primary" />
          <span className="code-color-muted">服务端业务执行:</span>
          <span className="code-font-mono">{serverExecUs} μs</span>
        </div>
      </div>
    </div>
  );
};
