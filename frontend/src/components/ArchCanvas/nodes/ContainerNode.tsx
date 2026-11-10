import React from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import type { ContainerNodeType } from '../layout/layoutProcessor';
import {
  ChevronRight,
  ChevronDown,
  Layers,
  Server,
  Box,
  LayoutGrid,
  Globe,
  Radio,
  Crosshair,
  Pin,
} from 'lucide-react';

export const ContainerNode: React.FC<NodeProps<ContainerNodeType>> = ({ id, data }) => {
  const {
    graphNode,
    isExpanded,
    onToggleExpand,
    onFocusScope,
    onTogglePin,
    isPinned,
    badgeCounts,
    childCount = 0,
    searchMatchCount = 0,
  } = data;

  const handleToggleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (onToggleExpand) {
      onToggleExpand(id);
    }
  };

  const handleFocusClick = (e: React.MouseEvent) => {
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
  let childUnit = '节点';

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
      childUnit = '组/模块';
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
      typeTagText = '外部网络';
      typeTagClass = 'code-arch-tag--external';
      childUnit = '端点';
      break;
  }

  const endpoint = graphNode.meta?.endpoint ? String(graphNode.meta.endpoint) : undefined;
  const internalEdges = badgeCounts?.internalEdgeCount ?? 0;
  const intraCalls = badgeCounts?.intraProcessCallCount ?? 0;
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

  const containerActions = (
    <div className="code-arch-container__actions">
      {onFocusScope && (
        <button
          type="button"
          className="code-arch-container__action-btn"
          onClick={handleFocusClick}
          title="下钻聚焦至此容器"
          data-testid={`focus-container-btn-${id}`}
        >
          <Crosshair size={12} />
        </button>
      )}
      {onTogglePin && (
        <button
          type="button"
          className={`code-arch-container__action-btn ${isPinned ? 'code-arch-container__action-btn--active' : ''}`}
          onClick={handlePinClick}
          title={isPinned ? '取消固定位置' : '固定容器位置（局部推挤中锁定）'}
          data-testid={`pin-node-btn-${id}`}
        >
          <Pin size={12} />
        </button>
      )}
    </div>
  );

  // 展开态渲染：大框容器（Container Box）
  if (isExpanded) {
    return (
      <div
        className={`code-arch-container code-arch-container--expanded code-arch-container--${graphNode.type.toLowerCase()} ${
          isUnmounted ? 'code-arch-container--unmounted' : ''
        } ${isPinned ? 'code-arch-node--pinned' : ''}`}
        title="双击折叠容器"
      >
        <Handle type="target" position={Position.Left} className="code-arch-handle" />

        <div className="code-arch-container__header">
          <button
            type="button"
            className="code-arch-container__toggle"
            onClick={handleToggleClick}
            title="点击折叠容器"
          >
            <ChevronDown size={14} />
          </button>

          {icon}

          <span className={`code-arch-tag ${typeTagClass}`}>{typeTagText}</span>

          <div className="code-arch-container__title-wrap">
            {graphNode.code && (
              <span className="code-arch-container__code">{graphNode.code}</span>
            )}
            <span className="code-arch-container__name" title={graphNode.name}>
              {graphNode.name}
            </span>
            {endpoint && (
              <span className="code-arch-container__endpoint" title={`监听端点: ${endpoint}`}>
                {endpoint}
              </span>
            )}
          </div>

          <div className="code-arch-container__badges">
            {searchMatchCount > 0 && (
              <span className="code-arch-badge code-arch-badge--provide" title={`内部命中 ${searchMatchCount} 个搜索节点`}>
                命中: {searchMatchCount}
              </span>
            )}
            {childCount > 0 && (
              <span className="code-arch-badge code-arch-badge--counter" title={`包含 ${childCount} 个${childUnit}`}>
                {childCount} {childUnit}
              </span>
            )}
            {internalEdges > 0 && (
              <span className="code-arch-badge code-arch-badge--internal" title="内部依赖契约">
                内部: {internalEdges}
              </span>
            )}
            {intraCalls > 0 && (
              <span className="code-arch-badge code-arch-badge--intra" title="进程内函数调用">
                进程内: {intraCalls}
              </span>
            )}
          </div>

          {containerActions}
        </div>

        <div className="code-arch-container__body" />

        <Handle type="source" position={Position.Right} className="code-arch-handle" />
      </div>
    );
  }

  // 折叠态渲染：实体卡片（Collapsed Card）
  return (
    <div
      className={`code-arch-container code-arch-container--collapsed code-arch-container--${graphNode.type.toLowerCase()} ${
        isUnmounted ? 'code-arch-container--unmounted' : ''
      } ${isPinned ? 'code-arch-node--pinned' : ''}`}
      title="双击展开容器查看内部节点"
    >
      <Handle type="target" position={Position.Left} className="code-arch-handle" />

      <div className="code-arch-container__header">
        <button
          type="button"
          className="code-arch-container__toggle"
          onClick={handleToggleClick}
          title="点击展开容器"
        >
          <ChevronRight size={14} />
        </button>

        {icon}

        <span className={`code-arch-tag ${typeTagClass}`}>{typeTagText}</span>

        <div className="code-arch-container__title-wrap">
          {graphNode.code && (
            <span className="code-arch-container__code">{graphNode.code}</span>
          )}
          <span className="code-arch-container__name" title={graphNode.name}>
            {graphNode.name}
          </span>
        </div>

        {searchMatchCount > 0 && (
          <span className="code-arch-badge code-arch-badge--provide" title={`容器内命中 ${searchMatchCount} 个搜索节点`}>
            命中: {searchMatchCount}
          </span>
        )}
        {childCount > 0 && (
          <span className="code-arch-badge code-arch-badge--counter" title={`包含 ${childCount} 个${childUnit}`}>
            {childCount} {childUnit}
          </span>
        )}

        {containerActions}
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
          {isUnmounted && (
            <span className="code-arch-badge code-arch-badge--warning" title="该进程在选定环境尚未挂载">
              未挂载
            </span>
          )}
          {probeStatus && (
            <span
              className={`code-arch-badge ${probeBadgeClass}`}
              title={`探针状态: ${probeStatus}`}
            >
              {probeStatus}
            </span>
          )}
          {graphNode.type === 'UNDEPLOYED_CONTAINER' && graphNode.meta?.module_count != null && (
            <span className="code-arch-badge code-arch-badge--warning">
              孤立模块: {String(graphNode.meta.module_count)}
            </span>
          )}
          {internalEdges > 0 && (
            <span className="code-arch-badge code-arch-badge--internal" title="同容器内部依赖契约">
              内部依赖: {internalEdges}
            </span>
          )}
          {intraCalls > 0 && (
            <span className="code-arch-badge code-arch-badge--intra" title="进程内函数调用（不经网络栈）">
              进程内: {intraCalls}
            </span>
          )}
        </div>
      </div>

      <Handle type="source" position={Position.Right} className="code-arch-handle" />
    </div>
  );
};
