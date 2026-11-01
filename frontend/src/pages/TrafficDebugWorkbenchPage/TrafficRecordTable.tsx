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
              <th>方向</th>
              <th>模式 (Exchange)</th>
              <th>流水号 (ullSNO)</th>
              <th>服务契约 (service_name)</th>
              <th>调用方法 (operation)</th>
              <th>耗时 (RTT)</th>
              <th>状态码</th>
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

                return (
                  <tr
                    key={r.id}
                    className={`code-table-row-clickable ${isSelected ? 'code-table-row--selected' : ''}`}
                    onClick={() => onSelectRecord(r)}
                    data-testid={`traffic-row-${r.id}`}
                  >
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
                    <td className="code-font-mono code-color-primary">{r.sno}</td>
                    <td className="code-font-mono">{r.service_name}</td>
                    <td className="code-font-mono">{r.operation}</td>
                    <td className="code-font-mono">{r.latency_us} μs</td>
                    <td>
                      <span
                        className={`code-font-mono ${
                          r.status_code === 0 ? 'code-color-success' : 'code-color-danger'
                        }`}
                      >
                        {r.status_code}
                      </span>
                    </td>
                    <td>
                      {r.src_module_name ? (
                        <span className="code-text-xs">
                          {r.src_module_name} &rarr; {r.dst_module_name || '未知下游'}
                        </span>
                      ) : (
                        <span className="code-color-muted code-text-xs">-</span>
                      )}
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
                            单包重放调试
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
