import React from 'react';
import { Link } from 'react-router-dom';
import type { RecordingSession } from '../../types';

export interface ActiveRecordingBannerProps {
  session: RecordingSession | null;
  onStop: (sessionId: string) => Promise<void>;
  stopping?: boolean;
}

export const ActiveRecordingBanner: React.FC<ActiveRecordingBannerProps> = ({
  session,
  onStop,
  stopping = false,
}) => {
  if (!session || session.status !== 'RECORDING') {
    return null;
  }

  const reconnectDiagnostics = (session.process_diagnostics ?? []).filter(
    (d) => d.need_reconnect
  );

  return (
    <div className="code-active-recording-banner" data-testid="active-recording-banner">
      <div className="code-recording-banner-top">
        <div className="code-recording-banner-info">
          <div className="code-recording-pulse-tag">
            <span className="code-pulse-dot" />
            <span className="code-pulse-text">协同录制进行中</span>
          </div>
          <div className="code-recording-meta-row">
            <span className="code-meta-item">
              <span className="code-meta-label">批次号:</span>
              <strong className="code-font-mono">{session.session_no}</strong>
            </span>
            <span className="code-meta-item">
              <span className="code-meta-label">名称:</span>
              <span>{session.name}</span>
            </span>
            <span className="code-meta-item">
              <span className="code-meta-label">作用域:</span>
              <span className="code-badge code-badge--info">{session.scope}</span>
            </span>
            <span className="code-meta-item">
              <span className="code-meta-label">实时捕获:</span>
              <span className="code-font-mono code-color-primary code-font-bold">
                {`${session.captured_frames} 帧`}
              </span>
            </span>
          </div>
        </div>

        <div className="code-recording-banner-actions">
          <Link
            to={`/debug?session_id=${session.id}&live=1`}
            className="code-btn code-btn--primary code-btn--sm"
            data-testid="btn-live-debug"
          >
            实时跟流调试 &rarr;
          </Link>
          <button
            type="button"
            className="code-btn code-btn--danger code-btn--sm"
            disabled={stopping}
            onClick={() => void onStop(session.id)}
            data-testid="btn-stop-recording"
          >
            {stopping ? '正在停止...' : '停止录制'}
          </button>
        </div>
      </div>

      {reconnectDiagnostics.length > 0 && (
        <div className="code-alert-banner code-alert-banner--warning code-mt-sm" data-testid="reconnect-banner">
          <div className="code-alert-banner__icon">⚠</div>
          <div className="code-alert-banner__content">
            <strong>检测到存量长连接需重连提示 (A-01)：</strong>
            <span>以下业务进程在挂载监控探针前已存在活跃的长连接（已建立的 TCP 连接未在握手阶段捕获 SYN/ACK）：</span>
            <ul className="code-diag-list">
              {reconnectDiagnostics.map((d) => (
                <li key={d.process_id}>
                  <strong>{d.process_name || `PID:${d.pid}`}</strong> (监听端口:{' '}
                  <code>{d.listen_port}</code>, 存量连接: {d.established_conns_count} 条)
                  {d.reconnect_guide ? ` — ${d.reconnect_guide}` : ' — 建议重启该进程或客户端以完整捕获 CROS RPC 头'}
                </li>
              ))}
            </ul>
          </div>
        </div>
      )}
    </div>
  );
};

export default ActiveRecordingBanner;
