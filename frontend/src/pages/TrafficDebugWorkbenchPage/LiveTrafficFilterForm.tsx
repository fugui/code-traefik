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
  has_error?: boolean;
}

export interface LiveTrafficFilterFormProps {
  filters: TrafficFilterValues;
  onChange: (updates: Partial<TrafficFilterValues>) => void;
  onReset: () => void;
}

export const LiveTrafficFilterForm: React.FC<LiveTrafficFilterFormProps> = ({
  filters,
  onChange,
  onReset,
}) => {
  return (
    <div className="code-traffic-filter-bar" data-testid="live-traffic-filter-form">
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
          <option value="">全部模式 (Exchange)</option>
          <option value="UNARY">UNARY (标准往返)</option>
          <option value="STREAM_RSP">STREAM_RSP (多分片流式)</option>
          <option value="ONEWAY">ONEWAY (单向请求)</option>
          <option value="REQ_ONLY">REQ_ONLY (孤立请求)</option>
          <option value="RSP_ORPHAN">RSP_ORPHAN (无头应答)</option>
        </select>

        <input
          type="text"
          className="code-input code-input--sm"
          placeholder="服务契约 (service_name)..."
          value={filters.service_name || ''}
          onChange={(e) => onChange({ service_name: e.target.value || undefined })}
        />

        <input
          type="text"
          className="code-input code-input--sm"
          placeholder="调用方法 (operation)..."
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
          placeholder="TraceID 串联..."
          value={filters.trace_id || ''}
          onChange={(e) => onChange({ trace_id: e.target.value || undefined })}
        />

        <input
          type="text"
          className="code-input code-input--sm"
          placeholder="载荷关键字检索..."
          value={filters.keyword || ''}
          onChange={(e) => onChange({ keyword: e.target.value || undefined })}
        />

        {/* has_error 仅看异常报文开关 */}
        <label className="code-checkbox-chip code-checkbox-chip--compact" data-testid="filter-has-error">
          <input
            type="checkbox"
            checked={Boolean(filters.has_error)}
            onChange={(e) => onChange({ has_error: e.target.checked ? true : undefined })}
          />
          <span className="code-color-danger">仅看异常报文</span>
        </label>

        <label className="code-checkbox-chip code-checkbox-chip--compact">
          <input
            type="checkbox"
            checked={Boolean(filters.is_replayable)}
            onChange={(e) => onChange({ is_replayable: e.target.checked ? true : undefined })}
          />
          <span>仅可重放</span>
        </label>

        <button
          type="button"
          className="code-btn code-btn--secondary code-btn--xs"
          onClick={onReset}
        >
          重置
        </button>
      </div>
    </div>
  );
};

export default LiveTrafficFilterForm;
