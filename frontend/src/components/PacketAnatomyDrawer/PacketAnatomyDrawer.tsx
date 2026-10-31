import React, { useState } from 'react';
import { Drawer } from '@code/common';
import type { PacketAnatomyDrawerDTO } from '../../types';
import { CommHeadBitmapCard } from './CommHeadBitmapCard';
import { SessionHeadDetailCard } from './SessionHeadDetailCard';
import { ExtendHeadTraceCard } from './ExtendHeadTraceCard';
import { PayloadJsonHexSplitView } from './PayloadJsonHexSplitView';

export interface PacketAnatomyDrawerProps {
  open: boolean;
  onClose: () => void;
  data: PacketAnatomyDrawerDTO | null;
  loading?: boolean;
  onRedecode?: (recordId: string) => Promise<void>;
  onFilterTrace?: (traceId: string) => void;
}

/**
 * 纯内容渲染组件，方便测试与无模态嵌入
 */
export const PacketAnatomyContent: React.FC<{
  data: PacketAnatomyDrawerDTO;
  onRedecode?: (recordId: string) => Promise<void>;
  onFilterTrace?: (traceId: string) => void;
}> = ({ data, onRedecode, onFilterTrace }) => {
  const [activeSide, setActiveSide] = useState<'REQ' | 'RSP'>('REQ');
  const [redecoding, setRedecoding] = useState(false);

  const currentSideData = activeSide === 'REQ' ? data.reqSide : data.rspSide;

  const handleRedecode = async () => {
    if (!onRedecode) return;
    setRedecoding(true);
    try {
      await onRedecode(data.recordId);
    } finally {
      setRedecoding(false);
    }
  };

  return (
    <div className="code-anatomy-content" data-testid="packet-anatomy-content">
      {/* 顶部元数据与方向切换 */}
      <div className="code-anatomy-top-bar">
        <div className="code-anatomy-meta">
          <div className="code-anatomy-meta__item">
            <span className="code-meta-label">记录流水号:</span>
            <span className="code-meta-val code-font-mono">{data.recordId}</span>
          </div>
          <div className="code-anatomy-meta__item">
            <span className="code-meta-label">交换模式:</span>
            <span className="code-badge code-badge--info">{data.exchangeType}</span>
          </div>
          <div className="code-anatomy-meta__item">
            <span className="code-meta-label">流量方向:</span>
            <span className={`code-badge ${data.direction === 'INGRESS' ? 'code-badge--primary' : 'code-badge--secondary'}`}>
              {data.direction}
            </span>
          </div>
          {data.roundTripLatencyUs !== undefined && (
            <div className="code-anatomy-meta__item">
              <span className="code-meta-label">往返耗时 (RTT):</span>
              <span className="code-meta-val code-font-mono code-color-primary">{data.roundTripLatencyUs} μs</span>
            </div>
          )}
          {data.srcModuleName && (
            <div className="code-anatomy-meta__item">
              <span className="code-meta-label">架构链路:</span>
              <span className="code-meta-val">
                {data.srcModuleName} &rarr; {data.dstModuleName ?? '未知下游'}
              </span>
            </div>
          )}
        </div>

        <div className="code-anatomy-actions">
          {onRedecode && (
            <button
              type="button"
              className="code-btn code-btn--secondary code-btn--sm"
              disabled={redecoding}
              onClick={() => void handleRedecode()}
            >
              {redecoding ? '重解码中...' : '按生效 .pb 重新解码'}
            </button>
          )}
        </div>
      </div>

      {/* 请求帧 / 应答帧 切换 Tab */}
      <div className="code-frame-side-tabs">
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
          {activeSide === 'REQ' ? '本记录为无头孤立应答帧 (RSP_ORPHAN)，未采集到请求帧。' : '本记录为单向请求 (ONEWAY) 或尚未捕获到对应应答 (REQ_ONLY)。'}
        </div>
      )}
    </div>
  );
};

export const PacketAnatomyDrawer: React.FC<PacketAnatomyDrawerProps> = ({
  open,
  onClose,
  data,
  loading = false,
  onRedecode,
  onFilterTrace,
}) => {
  return (
    <Drawer
      open={open}
      onClose={onClose}
      width="xl"
      title="CROS RPC 深度报文四段解剖抽屉"
      subtitle={data ? `Session: ${data.sessionId} | Record: ${data.recordId}` : undefined}
    >
      {loading ? (
        <div className="code-tpl-loading-screen">正在加载四段深度报文解剖数据...</div>
      ) : data ? (
        <PacketAnatomyContent data={data} onRedecode={onRedecode} onFilterTrace={onFilterTrace} />
      ) : (
        <div className="code-empty-pane">未获取到报文解剖详情</div>
      )}
    </Drawer>
  );
};

export default PacketAnatomyDrawer;
