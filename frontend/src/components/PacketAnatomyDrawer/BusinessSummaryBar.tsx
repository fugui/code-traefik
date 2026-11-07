import React, { useState } from 'react';
import type { BusinessSummaryProps } from './types';

export const BusinessSummaryBar: React.FC<BusinessSummaryProps> = ({
  data,
  viewMode,
  onViewModeChange,
  onFilterTrace,
  onRedecode,
  extraActions,
}) => {
  const [copiedKey, setCopiedKey] = useState<string | null>(null);
  const [redecoding, setRedecoding] = useState(false);

  const handleCopy = (text: string, key: string) => {
    try {
      if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
        void navigator.clipboard.writeText(text);
      }
    } catch {
      // 忽略 clipboard 异常
    }
    setCopiedKey(key);
    setTimeout(() => {
      setCopiedKey(null);
    }, 2000);
  };

  const handleRedecode = async () => {
    if (!onRedecode) return;
    setRedecoding(true);
    try {
      await onRedecode(data.recordId);
    } finally {
      setRedecoding(false);
    }
  };

  // 提取服务与方法名（优先从请求侧获取，其次从应答侧）
  const serviceName =
    data.reqSide?.sessionHead.serviceName ||
    data.rspSide?.sessionHead.serviceName ||
    '未知服务';
  const operation =
    data.reqSide?.sessionHead.operation ||
    data.rspSide?.sessionHead.operation ||
    '未知方法';

  // 状态判定
  const statusCode = data.rspSide?.sessionHead.uiStatusCode;
  const ullStatusCode = data.rspSide?.sessionHead.ullStatusCode;
  const isOneway = data.exchangeType === 'ONEWAY';
  const isReqOnly = data.exchangeType === 'REQ_ONLY';
  const isRspOrphan = data.exchangeType === 'RSP_ORPHAN';
  const isSuccess = statusCode === 0;
  const isBizError = statusCode !== undefined && statusCode !== 0;

  // Trace 信息
  const traceId =
    data.reqSide?.extendHead.trace?.traceIdHex ||
    data.rspSide?.extendHead.trace?.traceIdHex;

  // 耗时格式化
  const latencyUs = data.roundTripLatencyUs;
  const formattedLatency =
    latencyUs !== undefined
      ? latencyUs >= 1000
        ? `${latencyUs} μs (${(latencyUs / 1000).toFixed(2)} ms)`
        : `${latencyUs} μs`
      : undefined;

  return (
    <div className="code-biz-summary-card" data-testid="business-summary-bar">
      {/* 顶部主信息栏：服务与方法大标题 + 状态徽标 + 视图切换器 */}
      <div className="code-biz-summary-main">
        <div className="code-biz-summary-title-wrap">
          <div className="code-biz-summary-badges">
            {isSuccess && (
              <span className="code-badge code-badge--success" data-testid="status-success-badge">
                🟢 成功 (Code: 0)
              </span>
            )}
            {isBizError && (
              <span className="code-badge code-badge--danger" data-testid="status-error-badge">
                {`🔴 业务异常 (Code: ${statusCode}${ullStatusCode ? ` / ${ullStatusCode}` : ''})`}
              </span>
            )}
            {isOneway && (
              <span className="code-badge code-badge--info">🔵 单向通知 (ONEWAY)</span>
            )}
            {isReqOnly && (
              <span className="code-badge code-badge--warning">⚠️ 等待应答 (REQ_ONLY)</span>
            )}
            {isRspOrphan && (
              <span className="code-badge code-badge--muted">⚪ 孤立应答 (RSP_ORPHAN)</span>
            )}
            <span className="code-badge code-badge--secondary">{data.direction}</span>
            <span className="code-badge code-badge--muted">{data.exchangeType}</span>
          </div>

          <div className="code-biz-summary-heading">
            <span className="code-biz-summary-service code-font-mono" title={serviceName}>
              {serviceName}
            </span>
            <span className="code-biz-summary-divider">/</span>
            <span className="code-biz-summary-op code-font-mono">{operation}</span>
            <button
              type="button"
              className="code-btn-text code-ml-xs code-text-xs"
              onClick={() => handleCopy(`${serviceName}/${operation}`, 'rpc')}
              title="复制 RPC 接口全名"
            >
              {copiedKey === 'rpc' ? '已复制' : '复制全名'}
            </button>
          </div>
        </div>

        {/* 右侧动作区与模式切换 */}
        <div className="code-biz-summary-actions">
          {extraActions}
          {onRedecode && (
            <button
              type="button"
              className="code-btn code-btn--secondary code-btn--sm"
              disabled={redecoding}
              onClick={() => void handleRedecode()}
            >
              {redecoding ? '重解码中...' : '按生效 .pb 重新解码'}
            </button>
          )}
          <div className="code-view-mode-toggle" role="tablist">
            <button
              type="button"
              className={`code-view-mode-btn ${viewMode === 'BUSINESS' ? 'code-view-mode-btn--active' : ''}`}
              onClick={() => onViewModeChange('BUSINESS')}
              title="业务优先视图：聚焦业务入参、出参和耗时"
            >
              💼 业务视图
            </button>
            <button
              type="button"
              className={`code-view-mode-btn ${viewMode === 'EXPERT' ? 'code-view-mode-btn--active' : ''}`}
              onClick={() => onViewModeChange('EXPERT')}
              title="协议专家视图：平铺 24B 公共头、会话头结构体与 16B Raw Hex"
            >
              🔬 协议专家
            </button>
          </div>
        </div>
      </div>

      {/* 底部元数据网格：耗时、链路、Trace、SNO */}
      <div className="code-biz-meta-strip">
        {formattedLatency && (
          <div className="code-biz-meta-item">
            <span className="code-meta-label">往返耗时:</span>
            <span className="code-meta-val code-font-mono code-color-primary code-font-semibold">
              {formattedLatency}
            </span>
          </div>
        )}

        {data.srcModuleName && (
          <div className="code-biz-meta-item">
            <span className="code-meta-label">调用链路:</span>
            <span className="code-meta-val">
              <span className="code-font-mono">{data.srcModuleName}</span>
              <span className="code-mh-xs">&rarr;</span>
              <span className="code-font-mono">{data.dstModuleName ?? '未知下游'}</span>
            </span>
          </div>
        )}

        {traceId && (
          <div className="code-biz-meta-item">
            <span className="code-meta-label">全链路 TraceID:</span>
            <span className="code-meta-val code-font-mono code-color-secondary" title={traceId}>
              {traceId.length > 18 ? `${traceId.slice(0, 8)}...${traceId.slice(-6)}` : traceId}
            </span>
            <button
              type="button"
              className="code-btn-text code-text-xs"
              onClick={() => handleCopy(traceId, 'trace')}
              title="复制完整 TraceID"
            >
              {copiedKey === 'trace' ? '已复制' : '复制'}
            </button>
            {onFilterTrace && (
              <button
                type="button"
                className="code-btn-text code-text-xs"
                onClick={() => onFilterTrace(traceId)}
                title="在当前批次中筛选该 Trace 的所有调用"
              >
                关联筛选
              </button>
            )}
          </div>
        )}

        <div className="code-biz-meta-item">
          <span className="code-meta-label">流水号 (SNO):</span>
          <span className="code-meta-val code-font-mono">{data.recordId}</span>
        </div>
      </div>
    </div>
  );
};
