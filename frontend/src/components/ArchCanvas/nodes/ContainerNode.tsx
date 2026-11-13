import React from 'react';
import { type NodeProps } from '@xyflow/react';
import type { ContainerNodeType } from '../layout/layoutProcessor';
import { FourWayHandles } from './FourWayHandles';
import {
  ChevronRight,
  Layers,
  Server,
  Box,
  LayoutGrid,
  Globe,
  Radio,
  Pin,
} from 'lucide-react';

export const ContainerNode: React.FC<NodeProps<ContainerNodeType>> = ({ id, data }) => {
  const {
    graphNode,
    onFocusScope,
    onTogglePin,
    isPinned,
    badgeCounts,
    childCount = 0,
    searchMatchCount = 0,
    isSearchHighlighted = false,
  } = data;

  const handleEnterScopeClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (onFocusScope) {
      onFocusScope(id);
    }
  };

  const handlePinClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (onTogglePin) {
      onTogglePin(id);
    }
  };

  let icon = <Layers size={14} className="code-arch-container__icon" />;
  let typeTagText = '容器';
  let typeTagClass = 'code-arch-tag--default';
  let childUnit = '子项';

  switch (graphNode.type) {
    case 'PROCESS_GROUP':
      icon = <Layers size={14} className="code-arch-container__icon" />;
      typeTagText = '进程组';
      typeTagClass = 'code-arch-tag--process-group';
      childUnit = '进程';
      break;
    case 'PROCESS':
      icon = <Server size={14} className="code-arch-container__icon" />;
      typeTagText = '物理进程';
      typeTagClass = 'code-arch-tag--process';
      childUnit = '模块';
      break;
    case 'SUBSYSTEM':
      icon = <LayoutGrid size={14} className="code-arch-container__icon" />;
      typeTagText = '子系统';
      typeTagClass = 'code-arch-tag--subsystem';
      childUnit = '子项';
      break;
    case 'FUNCTIONAL_GROUP':
      icon = <Layers size={14} className="code-arch-container__icon" />;
      typeTagText = '功能组';
      typeTagClass = 'code-arch-tag--functional-group';
      childUnit = '模块';
      break;
    case 'UNDEPLOYED_CONTAINER':
      icon = <Box size={14} className="code-arch-container__icon" />;
      typeTagText = '未部署';
      typeTagClass = 'code-arch-tag--undeployed';
      childUnit = '模块';
      break;
    case 'EXTERNAL_CONTAINER':
      icon = <Globe size={14} className="code-arch-container__icon" />;
      typeTagText = '外部域';
      typeTagClass = 'code-arch-tag--external';
      childUnit = '端点';
      break;
  }

  const endpoint = graphNode.meta?.endpoint ? String(graphNode.meta.endpoint) : undefined;
  const internalEdges = badgeCounts?.internalEdgeCount ?? 0;
  const intraCalls = badgeCounts?.intraProcessCallCount ?? 0;
  const effLayer =
    graphNode.type === 'SUBSYSTEM'
      ? Number(graphNode.meta?.effective_layer_level ?? graphNode.meta?.layer_level ?? 0)
      : 0;
  const isUnmounted = graphNode.type === 'PROCESS' && graphNode.meta?.is_mounted === false;
  const probeStatus =
    graphNode.type === 'PROCESS' && graphNode.meta?.probe_status
      ? String(graphNode.meta.probe_status)
      : undefined;

  const probeBadgeClass =
    probeStatus === 'READY'
      ? 'code-arch-badge--probe-ready'
      : probeStatus === 'NEED_RECONNECT'
      ? 'code-arch-badge--probe-reconnect'
      : 'code-arch-badge--probe-default';

  const fullTitle = graphNode.code ? `${graphNode.code} · ${graphNode.name}` : graphNode.name;

  return (
    <div
      className={`code-arch-container code-arch-container--${graphNode.type.toLowerCase()} ${
        isUnmounted ? 'code-arch-container--unmounted' : ''
      } ${isPinned ? 'code-arch-node--pinned' : ''} ${
        isSearchHighlighted ? 'code-arch-node--pulse-highlight' : ''
      }`}
      title={`${fullTitle} · 双击或点击「进入」下钻至此域`}
      data-testid={`arch-container-node-${id}`}
    >
      <FourWayHandles />

      <div className="code-arch-container__header">
        {icon}
        <span className={`code-arch-tag ${typeTagClass}`}>{typeTagText}</span>

        <div className="code-arch-container__title-wrap" title={fullTitle}>
          {graphNode.code && (
            <span className="code-arch-container__code">{graphNode.code}</span>
          )}
          <span className="code-arch-container__name">{graphNode.name}</span>
        </div>

        <div className="code-arch-container__actions">
          {onTogglePin && (
            <button
              type="button"
              className={`code-arch-container__action-btn ${
                isPinned ? 'code-arch-container__action-btn--active' : ''
              }`}
              onClick={handlePinClick}
              title={isPinned ? '取消固定位置' : '固定位置（自动整理保持锁定）'}
              data-testid={`pin-node-btn-${id}`}
            >
              <Pin size={11} />
            </button>
          )}
          {onFocusScope && (
            <button
              type="button"
              className="code-arch-container__enter-btn"
              onClick={handleEnterScopeClick}
              title="下钻进入此容器作用域"
              data-testid={`focus-container-btn-${id}`}
            >
              <span>进入</span>
              <ChevronRight size={11} />
            </button>
          )}
        </div>
      </div>

      <div className="code-arch-container__collapsed-content">
        {endpoint && (
          <div className="code-arch-container__endpoint-row">
            <Radio size={11} className="code-arch-container__endpoint-icon" />
            <span className="code-arch-container__endpoint-text" title={`监听端点: ${endpoint}`}>
              {endpoint}
            </span>
          </div>
        )}

        <div className="code-arch-container__badges">
          {effLayer > 0 && (
            <span className="code-arch-badge code-arch-badge--counter" title={`子系统分层约束 L${effLayer}`}>
              L{effLayer}
            </span>
          )}
          {childCount > 0 && (
            <span className="code-arch-badge code-arch-badge--counter" title={`包含 ${childCount} 个${childUnit}`}>
              {childCount} {childUnit}
            </span>
          )}
          {internalEdges > 0 && (
            <span className="code-arch-badge code-arch-badge--internal" title={`内部已聚合 ${internalEdges} 条依赖调用`}>
              内部: {internalEdges}
            </span>
          )}
          {intraCalls > 0 && (
            <span className="code-arch-badge code-arch-badge--intra" title="进程内函数调用">
              进程内: {intraCalls}
            </span>
          )}
          {searchMatchCount > 0 && (
            <span className="code-arch-badge code-arch-badge--provide" title={`内部命中 ${searchMatchCount} 个搜索节点`}>
              命中: {searchMatchCount}
            </span>
          )}
          {isUnmounted && (
            <span className="code-arch-badge code-arch-badge--warning" title="该进程在选定环境尚未挂载">
              未挂载
            </span>
          )}
          {probeStatus && (
            <span className={`code-arch-badge ${probeBadgeClass}`} title={`探针状态: ${probeStatus}`}>
              {probeStatus}
            </span>
          )}
        </div>
      </div>
    </div>
  );
};
