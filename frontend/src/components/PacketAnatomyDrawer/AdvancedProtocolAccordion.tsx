import React, { useState } from 'react';
import type { AdvancedProtocolAccordionProps } from './types';
import { CommHeadBitmapCard } from './CommHeadBitmapCard';
import { SessionHeadDetailCard } from './SessionHeadDetailCard';
import { ExtendHeadTraceCard } from './ExtendHeadTraceCard';
import { PayloadJsonHexSplitView } from './PayloadJsonHexSplitView';

export const AdvancedProtocolAccordion: React.FC<AdvancedProtocolAccordionProps> = ({
  data,
  defaultExpanded = false,
  onFilterTrace,
}) => {
  // 如果遇到未解码原始二进制，默认展开方便底层排查
  const autoExpand =
    defaultExpanded ||
    data.reqSide?.payload.decodeStatus === 'NO_SCHEMA_RAW_HEX' ||
    data.rspSide?.payload.decodeStatus === 'NO_SCHEMA_RAW_HEX';

  const [expanded, setExpanded] = useState(autoExpand);
  const [activeSide, setActiveSide] = useState<'REQ' | 'RSP'>('REQ');

  const currentSideData = activeSide === 'REQ' ? data.reqSide : data.rspSide;

  return (
    <div className={`code-protocol-accordion ${expanded ? 'code-protocol-accordion--expanded' : ''}`} data-testid="advanced-protocol-accordion">
      {/* 折叠栏头部 */}
      <div
        className="code-protocol-accordion-header"
        onClick={() => setExpanded((prev) => !prev)}
        role="button"
        tabIndex={0}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            setExpanded((prev) => !prev);
          }
        }}
        title="点击展开或收起底层 CROS RPC 二进制协议与 16 字节对齐 Raw Hex 对照"
      >
        <div className="code-protocol-accordion-title">
          <span className="code-accordion-arrow">{expanded ? '▼' : '▶'}</span>
          <span className="code-accordion-label">🔬 底层二进制协议与十六进制解剖 (专家排错区)</span>
          <span className="code-badge code-badge--secondary code-text-xs">
            {expanded ? '已展开' : '默认收起 · 点击展开'}
          </span>
        </div>

        <div className="code-protocol-accordion-tags" onClick={(e) => e.stopPropagation()}>
          <span className="code-badge code-badge--info">24B CommHead</span>
          {currentSideData?.sessionHead.detectedVersion && (
            <span className="code-badge code-badge--success">{currentSideData.sessionHead.detectedVersion}</span>
          )}
          <span className="code-badge code-badge--purple">DOTR 扩展头</span>
          <span className="code-badge code-badge--muted">16B Raw Hex</span>
        </div>
      </div>

      {/* 折叠内容区：为保障测试与平滑展开，内容渲染在 DOM 中 */}
      <div className={`code-protocol-accordion-body ${expanded ? 'code-protocol-accordion-body--visible' : 'code-protocol-accordion-body--collapsed'}`}>
        {/* REQ / RSP 单侧切换 Tab */}
        <div className="code-frame-side-tabs code-mb-md">
          <button
            type="button"
            className={`code-frame-tab-btn ${activeSide === 'REQ' ? 'code-frame-tab-btn--active' : ''}`}
            disabled={!data.reqSide}
            onClick={() => setActiveSide('REQ')}
          >
            请求帧 REQ {data.reqSide ? `(1 段)` : '(缺失/孤立应答)'}
          </button>
          <button
            type="button"
            className={`code-frame-tab-btn ${activeSide === 'RSP' ? 'code-frame-tab-btn--active' : ''}`}
            disabled={!data.rspSide}
            onClick={() => setActiveSide('RSP')}
          >
            应答帧 RSP {data.rspSide ? `(${data.rspSide.segmentCount} 段)` : '(单向/未收到应答)'}
          </button>
        </div>

        {/* 四段结构展现 */}
        {currentSideData ? (
          <div className="code-anatomy-sections">
            <CommHeadBitmapCard commHead={currentSideData.commHead} />
            <SessionHeadDetailCard sessionHead={currentSideData.sessionHead} />
            <ExtendHeadTraceCard extendHead={currentSideData.extendHead} onFilterTrace={onFilterTrace} />
            <PayloadJsonHexSplitView payload={currentSideData.payload} />
          </div>
        ) : (
          <div className="code-empty-pane code-p-lg">
            {activeSide === 'REQ'
              ? '本记录为无头孤立应答帧 (RSP_ORPHAN)，未采集到请求帧。'
              : '本记录为单向请求 (ONEWAY) 或尚未捕获到对应应答 (REQ_ONLY)。'}
          </div>
        )}
      </div>
    </div>
  );
};
