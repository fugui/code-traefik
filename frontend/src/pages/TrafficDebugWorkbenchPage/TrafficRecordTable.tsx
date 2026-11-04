import React from 'react';
import type { TrafficRecordSummary } from '../../types';

export interface TrafficRecordTableProps {
  records: TrafficRecordSummary[];
  onSelectRecord: (record: TrafficRecordSummary) => void;
  selectedRecordId?: string;
  onSinglePacketReplay?: (record: TrafficRecordSummary) => void;
  loading?: boolean;
}

export const TrafficRecordTable: React.FC<TrafficRecordTableProps> = ({
  records,
  onSelectRecord,
  selectedRecordId,
  onSinglePacketReplay,
  loading = false,
}) => {
  return (
    <div className="code-table-wrapper" data-testid="traffic-record-table">
      {loading ? (
        <div className="code-tpl-loading-screen">正在检索报文流水记录...</div>
      ) : (
        <table className="code-table">
          <thead>
            <tr>
              <th>#Seq 发起序</th>
              <th>方向</th>
              <th>模式 (Exchange)</th>
              <th>连接 / 流水号</th>
              <th>服务契约 & 接口方法</th>
              <th>耗时 (RTT)</th>
              <th>双态状态码</th>
              <th>架构链路</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {records.length === 0 ? (
              <tr>
                <td colSpan={9} className="code-table-empty">
                  当前批次与筛选条件下暂无归档流量记录
                </td>
              </tr>
            ) : (
              records.map((r) => {
                const isSelected = selectedRecordId === r.id;
                const isIngressReplayable = r.direction === 'INGRESS' && r.is_replayable;
                const isProtoSuccess = r.status_code === 0 || r.status_code === '0';

                return (
                  <tr
                    key={r.id}
                    className={`code-table-row-clickable ${isSelected ? 'code-table-row--selected' : ''}`}
                    onClick={() => onSelectRecord(r)}
                    data-testid={`traffic-row-${r.id}`}
                  >
                    <td className="code-font-mono" style={{ color: 'var(--color-text-secondary, #64748b)' }}>
                      {`#${r.seq_no ?? '-'}`}
                    </td>
                    <td>
                      <span
                        className={`code-badge ${
                          r.direction === 'INGRESS' ? 'code-badge--primary' : 'code-badge--secondary'
                        }`}
                      >
                        {r.direction}
                      </span>
                    </td>
                    <td>
                      <span className="code-badge code-badge--info">{r.exchange_type}</span>
                    </td>
                    <td className="code-font-mono">
                      <span
                        style={{
                          fontSize: '11px',
                          padding: '1px 5px',
                          borderRadius: '3px',
                          backgroundColor: 'var(--color-bg-secondary, #f1f5f9)',
                          color: 'var(--color-text-secondary, #475569)',
                        }}
                      >
                        {`Conn#${r.conn_index ?? 1} · ${r.sno}`}
                      </span>
                    </td>
                    <td>
                      <div style={{ display: 'flex', flexDirection: 'column' }}>
                        <span style={{ fontWeight: 600 }}>{r.operation}</span>
                        <span className="code-text-xs code-color-muted">{r.service_name}</span>
                      </div>
                    </td>
                    <td className="code-font-mono">
                      {r.latency_us !== undefined && r.latency_us !== null ? `${r.latency_us} μs` : '-'}
                    </td>
                    <td>
                      <div style={{ display: 'inline-flex', flexDirection: 'column', gap: '2px' }}>
                        <span
                          className={`code-font-mono ${
                            isProtoSuccess ? 'code-color-success' : 'code-color-danger'
                          }`}
                        >
                          {isProtoSuccess ? '0 OK' : `Err: ${r.status_code}`}
                        </span>
                        {r.biz_error && (
                          <span
                            style={{
                              fontSize: '10px',
                              padding: '1px 4px',
                              borderRadius: '3px',
                              backgroundColor: 'var(--color-danger-bg, #fef2f2)',
                              color: 'var(--color-danger, #ef4444)',
                              border: '1px dashed var(--color-danger, #ef4444)',
                            }}
                          >
                            {`⚠️ 业务异常 ${r.biz_status_summary ? `(${r.biz_status_summary})` : ''}`}
                          </span>
                        )}
                      </div>
                    </td>
                    <td>
                      <span className="code-text-xs">
                        {`${r.src_module_name || r.peer_proc_name || '-'} → ${r.dst_module_name || r.process_name || '-'}`}
                      </span>
                    </td>
                    <td>
                      <div className="code-action-links">
                        <button
                          type="button"
                          className="code-btn code-btn--secondary code-btn--xs"
                          onClick={(e) => {
                            e.stopPropagation();
                            onSelectRecord(r);
                          }}
                          data-testid={`btn-anatomy-${r.id}`}
                        >
                          四段解剖
                        </button>
                        {isIngressReplayable && onSinglePacketReplay && (
                          <button
                            type="button"
                            className="code-btn code-btn--primary code-btn--xs"
                            onClick={(e) => {
                              e.stopPropagation();
                              onSinglePacketReplay(r);
                            }}
                            data-testid={`btn-single-replay-${r.id}`}
                          >
                            事务重放
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      )}
    </div>
  );
};

export default TrafficRecordTable;
