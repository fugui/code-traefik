import React, { useState } from 'react';
import { Drawer } from '@code/common';
import type { PacketAnatomyDrawerDTO } from '../../types';
import type { PacketInspectorViewMode } from './types';
import { BusinessSummaryBar } from './BusinessSummaryBar';
import { BusinessPayloadCompareView } from './BusinessPayloadCompareView';
import { RpcLatencyWaterfallBar } from './RpcLatencyWaterfallBar';
import { AdvancedProtocolAccordion } from './AdvancedProtocolAccordion';
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
  extraActions?: React.ReactNode;
}

/**
 * 纯内容渲染组件，方便测试与无模态嵌入
 */
export const PacketAnatomyContent: React.FC<{
  data: PacketAnatomyDrawerDTO;
  onRedecode?: (recordId: string) => Promise<void>;
  onFilterTrace?: (traceId: string) => void;
  extraActions?: React.ReactNode;
}> = ({ data, onRedecode, onFilterTrace, extraActions }) => {
  const [viewMode, setViewMode] = useState<PacketInspectorViewMode>('BUSINESS');
  const [activeSide, setActiveSide] = useState<'REQ' | 'RSP'>('REQ');

  const currentSideData = activeSide === 'REQ' ? data.reqSide : data.rspSide;

  return (
    <div className="code-anatomy-content" data-testid="packet-anatomy-content">
      {/* 顶部业务总览 Bar */}
      <BusinessSummaryBar
        data={data}
        viewMode={viewMode}
        onViewModeChange={setViewMode}
        onFilterTrace={onFilterTrace}
        onRedecode={onRedecode}
        extraActions={extraActions}
      />

      {viewMode === 'BUSINESS' ? (
        <>
          {/* 核心业务视窗：请求入参与返回结果并排孪生对比 */}
          <BusinessPayloadCompareView data={data} onRedecode={onRedecode} />

          {/* 阶段耗时甘特拆解瀑布条 */}
          <RpcLatencyWaterfallBar data={data} />

          {/* 底层协议专家排错折叠手风琴区 */}
          <AdvancedProtocolAccordion data={data} onFilterTrace={onFilterTrace} />
        </>
      ) : (
        /* 专家模式：平铺展示四段结构 */
        <div className="code-expert-mode-container" data-testid="expert-mode-sections">
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
  extraActions,
}) => {
  const serviceTitle =
    data?.reqSide?.sessionHead.serviceName ||
    data?.rspSide?.sessionHead.serviceName;
  const opTitle =
    data?.reqSide?.sessionHead.operation ||
    data?.rspSide?.sessionHead.operation;

  const drawerTitle = serviceTitle && opTitle
    ? `RPC 调用透视 · ${opTitle}`
    : 'CROS RPC 报文调用与深度解剖抽屉';

  return (
    <Drawer
      open={open}
      onClose={onClose}
      width="xl"
      title={drawerTitle}
      subtitle={data ? `Session: ${data.sessionId} | Record: ${data.recordId}${serviceTitle ? ` | ${serviceTitle}` : ''}` : undefined}
    >
      {loading ? (
        <div className="code-tpl-loading-screen">正在加载报文详情...</div>
      ) : data ? (
        <PacketAnatomyContent
          data={data}
          onRedecode={onRedecode}
          onFilterTrace={onFilterTrace}
          extraActions={extraActions}
        />
      ) : (
        <div className="code-empty-pane">未获取到报文详情</div>
      )}
    </Drawer>
  );
};

export default PacketAnatomyDrawer;
