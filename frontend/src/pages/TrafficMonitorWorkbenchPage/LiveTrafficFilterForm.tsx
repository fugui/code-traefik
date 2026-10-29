import React from 'react';

export interface TrafficFilterValues {
  direction?: 'INGRESS' | 'EGRESS';
  exchange_type?: string;
  service_name?: string;
  operation?: string;
  sno?: string;
  trace_id?: string;
  keyword?: string;
  is_replayable?: boolean;
}

export interface LiveTrafficFilterFormProps {
  filters: TrafficFilterValues;
  onChange: (updates: Partial<TrafficFilterValues>) => void;
  onReset: () => void;
  isSsePaused: boolean;
  onToggleSsePause: () => void;
}

export const LiveTrafficFilterForm: React.FC<LiveTrafficFilterFormProps> = ({
  filters,
  onChange,
  onReset,
  isSsePaused,
  onToggleSsePause,
}) => {
  return (
    <div className="code-traffic-filter-bar">
      <div className="code-filter-inputs">
        <select
          className="code-select code-select--compact"
          value={filters.direction || ''}
          onChange={(e) =>
            onChange({
              direction: (e.target.value as 'INGRESS' | 'EGRESS') || undefined,
            })
          }
        >
          <option value="">全部方向 (DIR)</option>
          <option value="INGRESS">INGRESS (入站请求)</option>
          <option value="EGRESS">EGRESS (出站调用)</option>
        </select>

        <select
          className="code-select code-select--compact"
          value={filters.exchange_type || ''}
          onChange={(e) => onChange({ exchange_type: e.target.value || undefined })}
        >
          <option value="">全部交换模式</option>
          <option value="UNARY">UNARY (标准请求应答)</option>
          <option value="STREAM_RSP">STREAM_RSP (多分片流式)</option>
          <option value="ONEWAY">ONEWAY (单向请求)</option>
          <option value="REQ_ONLY">REQ_ONLY (孤立请求)</option>
          <option value="RSP_ORPHAN">RSP_ORPHAN (无头应答)</option>
        </select>

        <input
          type="text"
          className="code-input code-input--sm"
          placeholder="服务名 (service_name)..."
          value={filters.service_name || ''}
          onChange={(e) => onChange({ service_name: e.target.value || undefined })}
        />

        <input
          type="text"
          className="code-input code-input--sm"
          placeholder="方法 (operation)..."
          value={filters.operation || ''}
          onChange={(e) => onChange({ operation: e.target.value || undefined })}
        />

        <input
          type="text"
          className="code-input code-input--sm"
          placeholder="流水号 (sno)..."
          value={filters.sno || ''}
          onChange={(e) => onChange({ sno: e.target.value || undefined })}
        />

        <input
          type="text"
          className="code-input code-input--sm"
          placeholder="TraceID (DOTR)..."
          value={filters.trace_id || ''}
          onChange={(e) => onChange({ trace_id: e.target.value || undefined })}
        />

        <input
          type="text"
          className="code-input code-input--sm"
          placeholder="载荷关键字 (GIN)..."
          value={filters.keyword || ''}
          onChange={(e) => onChange({ keyword: e.target.value || undefined })}
        />

        <label className="code-checkbox-chip code-checkbox-chip--compact">
          <input
            type="checkbox"
            checked={Boolean(filters.is_replayable)}
            onChange={(e) => onChange({ is_replayable: e.target.checked ? true : undefined })}
          />
          <span>仅可重放</span>
        </label>

        <button type="button" className="code-btn code-btn--secondary code-btn--xs" onClick={onReset}>
          重置
        </button>
      </div>

      <div className="code-filter-sse-action">
        <button
          type="button"
          className={`code-btn code-btn--xs ${isSsePaused ? 'code-btn--warning' : 'code-btn--secondary'}`}
          onClick={onToggleSsePause}
          title={isSsePaused ? '恢复 Track A 快轨 SSE 实时流推送' : '暂停实时推送以固定屏幕排查'}
        >
          {isSsePaused ? '▶ 恢复实时流' : '⏸ 暂停实时流'}
        </button>
      </div>
    </div>
  );
};
