import React from 'react';
import type { CommHeadAnatomyDTO } from '../../../types';

export interface CommHeadBitmapCardProps {
  commHead: CommHeadAnatomyDTO;
}

export const CommHeadBitmapCard: React.FC<CommHeadBitmapCardProps> = ({ commHead }) => {
  return (
    <div className="code-anatomy-card">
      <div className="code-anatomy-card__header">
        <div className="code-anatomy-card__title">① 24B 公共头位图与校验 (CommHead)</div>
        <div className="code-anatomy-card__tags">
          <span className="code-badge code-badge--info">{commHead.magicAscii}</span>
          <span className="code-badge code-badge--secondary">{commHead.transportName}</span>
          <span className="code-badge code-badge--primary">{commHead.msgTypeName}</span>
        </div>
      </div>

      <div className="code-anatomy-card__body">
        {/* 24 字节偏移色块可视化 */}
        <div className="code-bitmap-bar">
          {commHead.segments.map((seg) => (
            <div
              key={`${seg.offsetStart}-${seg.fieldName}`}
              className="code-bitmap-segment"
              title={`偏移 [${seg.offsetStart}..${seg.offsetStart + seg.byteLength - 1}]: ${seg.fieldName} (${seg.byteLength}B) = ${seg.displayValue}`}
            >
              <div className="code-bitmap-segment__name">{seg.fieldName}</div>
              <div className="code-bitmap-segment__range">{seg.offsetStart}..{seg.offsetStart + seg.byteLength - 1}</div>
              <div className="code-bitmap-segment__hex">{seg.rawHex}</div>
            </div>
          ))}
        </div>

        {/* 字段概览列表 */}
        <div className="code-meta-grid">
          <div className="code-meta-item">
            <span className="code-meta-label">流水号 (ullSNO):</span>
            <span className="code-meta-val code-font-mono">{commHead.snoDec} ({commHead.snoHex})</span>
          </div>
          <div className="code-meta-item">
            <span className="code-meta-label">整包长度 (uiLength):</span>
            <span className="code-meta-val code-font-mono">{commHead.uiLength} Bytes</span>
          </div>
          <div className="code-meta-item">
            <span className="code-meta-label">头部长度 (usHeadLen):</span>
            <span className="code-meta-val code-font-mono">{commHead.usHeadLen} Bytes</span>
          </div>
          <div className="code-meta-item">
            <span className="code-meta-label">打包状态 (isPacked):</span>
            <span className="code-meta-val">{commHead.isPacked === 1 ? '已打包' : '未打包'}</span>
          </div>
        </div>
      </div>
    </div>
  );
};
