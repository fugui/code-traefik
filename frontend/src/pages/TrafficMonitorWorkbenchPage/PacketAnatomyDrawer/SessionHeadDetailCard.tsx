import React from 'react';
import type { SessionHeadAnatomyDTO } from '../../../types';

export interface SessionHeadDetailCardProps {
  sessionHead: SessionHeadAnatomyDTO;
}

export const SessionHeadDetailCard: React.FC<SessionHeadDetailCardProps> = ({ sessionHead }) => {
  return (
    <div className="code-anatomy-card">
      <div className="code-anatomy-card__header">
        <div className="code-anatomy-card__title">② 会话头结构与耗时拆解 (SessionHead)</div>
        <div className="code-anatomy-card__tags">
          <span className="code-badge code-badge--success">{sessionHead.detectedVersion}</span>
          <span className="code-badge code-badge--secondary">{sessionHead.headKind}</span>
          <span className="code-badge code-badge--info">{sessionHead.sessionHeadLen} Bytes</span>
        </div>
      </div>

      <div className="code-anatomy-card__body">
        <div className="code-meta-grid">
          <div className="code-meta-item">
            <span className="code-meta-label">目标服务 (ServiceName):</span>
            <span className="code-meta-val code-font-mono">{sessionHead.serviceName}</span>
          </div>
          <div className="code-meta-item">
            <span className="code-meta-label">调用方法 (Operation):</span>
            <span className="code-meta-val code-font-mono">{sessionHead.operation}</span>
          </div>
          {sessionHead.uiStatusCode !== undefined && (
            <div className="code-meta-item">
              <span className="code-meta-label">返回状态码 (StatusCode):</span>
              <span className={`code-meta-val code-font-mono ${sessionHead.uiStatusCode === 0 ? 'code-color-success' : 'code-color-danger'}`}>
                {sessionHead.uiStatusCode} {sessionHead.ullStatusCode ? `(${sessionHead.ullStatusCode})` : ''}
              </span>
            </div>
          )}
          {sessionHead.rspMsgNo !== undefined && (
            <div className="code-meta-item">
              <span className="code-meta-label">应答分片序号 (usRspMsgNo):</span>
              <span className="code-meta-val code-font-mono">{sessionHead.rspMsgNo}</span>
            </div>
          )}
        </div>

        {/* 耗时拆解 */}
        {sessionHead.timingBreakdownUs && (
          <div className="code-timing-box">
            <div className="code-timing-title">微秒级阶段耗时拆解</div>
            <div className="code-timing-bars">
              {sessionHead.timingBreakdownUs.clientToServerUs !== undefined && (
                <div className="code-timing-item">
                  <span className="code-timing-label">客户端至服务端传输:</span>
                  <span className="code-timing-val">{sessionHead.timingBreakdownUs.clientToServerUs} μs</span>
                </div>
              )}
              {sessionHead.timingBreakdownUs.serverProcessUs !== undefined && (
                <div className="code-timing-item">
                  <span className="code-timing-label">服务端业务处理耗时:</span>
                  <span className="code-timing-val">{sessionHead.timingBreakdownUs.serverProcessUs} μs</span>
                </div>
              )}
            </div>
          </div>
        )}

        {/* 会话头详细字段表 */}
        {sessionHead.fields.length > 0 && (
          <div className="code-table-wrapper code-mt-sm">
            <table className="code-table code-table--compact">
              <thead>
                <tr>
                  <th>字段名称</th>
                  <th>偏移范围</th>
                  <th>解析值</th>
                </tr>
              </thead>
              <tbody>
                {sessionHead.fields.map((f, idx) => (
                  <tr key={`${f.name}-${idx}`}>
                    <td className="code-font-mono">{f.name}</td>
                    <td className="code-font-mono code-color-muted">{f.offsetRange}</td>
                    <td className="code-font-mono">{f.value}</td>
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
