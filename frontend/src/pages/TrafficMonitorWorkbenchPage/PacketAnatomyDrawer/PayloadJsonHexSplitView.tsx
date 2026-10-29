import React, { useMemo, useState } from 'react';
import type { PayloadAnatomyDTO } from '../../../types';

export interface PayloadJsonHexSplitViewProps {
  payload: PayloadAnatomyDTO;
}

export const PayloadJsonHexSplitView: React.FC<PayloadJsonHexSplitViewProps> = ({ payload }) => {
  const [activeTab, setActiveTab] = useState<'SPLIT' | 'JSON_ONLY' | 'HEX_ONLY'>('SPLIT');

  const formattedJson = useMemo(() => {
    if (!payload.decodedJson) return '';
    try {
      return JSON.stringify(payload.decodedJson, null, 2);
    } catch {
      return String(payload.decodedJson);
    }
  }, [payload.decodedJson]);

  const getRegionClass = (region: 'COMM_HEAD' | 'SESSION_HEAD' | 'EXTEND_HEAD' | 'PAYLOAD') => {
    switch (region) {
      case 'COMM_HEAD':
        return 'code-hex-region--comm';
      case 'SESSION_HEAD':
        return 'code-hex-region--session';
      case 'EXTEND_HEAD':
        return 'code-hex-region--extend';
      case 'PAYLOAD':
      default:
        return 'code-hex-region--payload';
    }
  };

  return (
    <div className="code-anatomy-card">
      <div className="code-anatomy-card__header">
        <div className="code-anatomy-card__title">④ 载荷解析与 16B 对齐十六进制对照 (Payload)</div>
        <div className="code-anatomy-card__tags">
          <span className={`code-badge ${payload.decodeStatus === 'DECODED_JSON' ? 'code-badge--success' : 'code-badge--warning'}`}>
            {payload.decodeStatus}
          </span>
          {payload.schemaName && (
            <span className="code-badge code-badge--info">
              {payload.schemaName} (v{payload.schemaVersion})
            </span>
          )}
          {payload.messageFullName && (
            <span className="code-badge code-badge--secondary">{payload.messageFullName}</span>
          )}
          <span className="code-badge code-badge--muted">{payload.payloadLength} Bytes</span>
          {payload.isTruncated && <span className="code-badge code-badge--danger">已截断 (&gt;4MB)</span>}
        </div>
      </div>

      <div className="code-anatomy-card__toolbar">
        <div className="code-tab-pills">
          <button
            type="button"
            className={`code-pill-btn ${activeTab === 'SPLIT' ? 'code-pill-btn--active' : ''}`}
            onClick={() => setActiveTab('SPLIT')}
          >
            左右并排对照
          </button>
          <button
            type="button"
            className={`code-pill-btn ${activeTab === 'JSON_ONLY' ? 'code-pill-btn--active' : ''}`}
            onClick={() => setActiveTab('JSON_ONLY')}
          >
            仅看 JSON
          </button>
          <button
            type="button"
            className={`code-pill-btn ${activeTab === 'HEX_ONLY' ? 'code-pill-btn--active' : ''}`}
            onClick={() => setActiveTab('HEX_ONLY')}
          >
            仅看 Raw Hex (16B 对齐)
          </button>
        </div>
        <div className="code-hex-legend">
          <span className="code-legend-item code-hex-region--comm">公共头 (24B)</span>
          <span className="code-legend-item code-hex-region--session">会话头</span>
          <span className="code-legend-item code-hex-region--extend">扩展头</span>
          <span className="code-legend-item code-hex-region--payload">载荷数据</span>
        </div>
      </div>

      <div className={`code-split-container code-split-container--${activeTab.toLowerCase()}`}>
        {/* 左侧：JSON 树 */}
        {(activeTab === 'SPLIT' || activeTab === 'JSON_ONLY') && (
          <div className="code-split-pane code-split-pane--left">
            <div className="code-split-pane__title">动态反序列化 JSON 结构</div>
            {formattedJson ? (
              <pre className="code-code-block">{formattedJson}</pre>
            ) : (
              <div className="code-empty-pane">
                {payload.decodeStatus === 'NO_SCHEMA_RAW_HEX' ? '未找到匹配的 Protobuf 描述集，保留原始字节流' : '无有效载荷数据'}
              </div>
            )}
          </div>
        )}

        {/* 右侧：16B 对齐 Hex / ASCII 对照 */}
        {(activeTab === 'SPLIT' || activeTab === 'HEX_ONLY') && (
          <div className="code-split-pane code-split-pane--right">
            <div className="code-split-pane__title">16 字节对齐报文 Raw Hex 视窗</div>
            <div className="code-hex-viewer">
              <div className="code-hex-header">
                <span className="code-hex-col--offset">Offset</span>
                <span className="code-hex-col--bytes">00 01 02 03 04 05 06 07  08 09 0A 0B 0C 0D 0E 0F</span>
                <span className="code-hex-col--ascii">Decoded ASCII</span>
              </div>
              <div className="code-hex-body">
                {payload.rawFrameHexLines.map((line, idx) => (
                  <div key={`${line.offsetHex}-${idx}`} className={`code-hex-row ${getRegionClass(line.region)}`}>
                    <span className="code-hex-col--offset code-font-mono">{line.offsetHex}</span>
                    <span className="code-hex-col--bytes code-font-mono">{line.hexCols}</span>
                    <span className="code-hex-col--ascii code-font-mono">{line.asciiCols}</span>
                  </div>
                ))}
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
};
