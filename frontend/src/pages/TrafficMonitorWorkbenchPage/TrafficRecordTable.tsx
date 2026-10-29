import React from 'react';
import type { TrafficRecordSummary } from '../../types';

export interface TrafficRecordTableProps {
  records: TrafficRecordSummary[];
  onSelectRecord: (record: TrafficRecordSummary) => void;
  selectedRecordId?: string;
}

export const TrafficRecordTable: React.FC<TrafficRecordTableProps> = ({
  records,
  onSelectRecord,
  selectedRecordId,
}) => {
  return (
    <div className="code-table-wrapper">
      <table className="code-table">
        <thead>
          <tr>
            <th>方向</th>
            <th>模式 (Exchange)</th>
            <th>流水号 (ullSNO)</th>
            <th>服务契约 (service_name)</th>
            <th>调用方法 (operation)</th>
            <th>往返耗时 (RTT)</th>
            <th>状态码</th>
            <th>解码态</th>
            <th>架构穿透链路</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          {(records ?? []).map((r) => {
            const isSelected = selectedRecordId === r.id;
            return (
              <tr
                key={r.id}
                className={`code-table-row-clickable ${isSelected ? 'code-table-row--selected' : ''}`}
                onClick={() => onSelectRecord(r)}
              >
                <td>
                  <span className={`code-badge ${r.direction === 'INGRESS' ? 'code-badge--primary' : 'code-badge--secondary'}`}>
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
                  <span className={`code-font-mono ${r.status_code === 0 ? 'code-color-success' : 'code-color-danger'}`}>
                    {r.status_code}
                  </span>
                </td>
                <td>
                  <span className={`code-badge ${r.decode_status === 'DECODED_JSON' ? 'code-badge--success' : 'code-badge--warning'}`}>
                    {r.decode_status}
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
                  <button
                    type="button"
                    className="code-btn code-btn--secondary code-btn--xs"
                    onClick={(e) => {
                      e.stopPropagation();
                      onSelectRecord(r);
                    }}
                  >
                    四段解剖
                  </button>
                </td>
              </tr>
            );
          })}
          {records.length === 0 && (
            <tr>
              <td colSpan={10} className="code-table-empty">
                当前批次与筛选条件下暂无归档流量记录
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
};
