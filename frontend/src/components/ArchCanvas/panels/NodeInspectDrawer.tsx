import React, { useState, useEffect } from 'react';
import { Drawer } from '@code/common';
import {
  ArrowUpRight,
  ArrowDownLeft,
  Crosshair,
  FileCode2,
  GitBranch,
  Info,
} from 'lucide-react';
import type { GraphNode, RollupEdgeModel } from '../../../types/topology_graph';
import type { NodeRelationItem, NodeRelationsResult } from '../model/noiseReduction';

export type NodeInspectTab = 'RELATIONS' | 'OVERVIEW';

export interface NodeInspectDrawerProps {
  open: boolean;
  onClose: () => void;
  node: GraphNode | null;
  activeTab?: NodeInspectTab;
  onTabChange?: (tab: NodeInspectTab) => void;
  relations?: NodeRelationsResult;
  onJumpToNode?: (node: GraphNode) => void;
  onInspectEdge?: (edge: RollupEdgeModel) => void;
  onFocusScope?: (nodeId: string) => void;
}

export const NodeInspectDrawer: React.FC<NodeInspectDrawerProps> = ({
  open,
  onClose,
  node,
  activeTab,
  onTabChange,
  relations,
  onJumpToNode,
  onInspectEdge,
  onFocusScope,
}) => {
  const [internalTab, setInternalTab] = useState<NodeInspectTab>(activeTab ?? 'RELATIONS');

  useEffect(() => {
    if (activeTab) {
      setInternalTab(activeTab);
    }
  }, [activeTab]);

  if (!node) return null;

  const currentTab: NodeInspectTab = activeTab ?? internalTab;
  const handleSelectTab = (tab: NodeInspectTab) => {
    setInternalTab(tab);
    onTabChange?.(tab);
  };

  const upstreamList = relations?.upstream ?? [];
  const downstreamList = relations?.downstream ?? [];
  const internalCount = relations?.internalCount ?? 0;
  const totalRelations = upstreamList.length + downstreamList.length;

  const isContainer =
    node.type === 'SUBSYSTEM' ||
    node.type === 'FUNCTIONAL_GROUP' ||
    node.type === 'PROCESS_GROUP' ||
    node.type === 'PROCESS' ||
    node.type === 'UNDEPLOYED_CONTAINER' ||
    node.type === 'EXTERNAL_CONTAINER';

  const renderRelationCard = (item: NodeRelationItem) => {
    const statusClass = item.status
      ? item.status === 'VIOLATION'
        ? 'code-arch-badge--danger'
        : item.status === 'UNATTRIBUTED'
        ? 'code-arch-badge--warning'
        : item.status === 'VERIFIED'
        ? 'code-arch-badge--provide'
        : 'code-arch-badge--counter'
      : 'code-arch-badge--counter';

    const servicePreview = (item.services || [])
      .slice(0, 3)
      .map((s) => s.service_name)
      .join(', ');

    return (
      <div
        key={item.edgeId}
        className={`code-node-inspect__rel-card code-node-inspect__rel-card--${item.direction.toLowerCase()}`}
        data-testid={`node-rel-card-${item.edgeId}`}
      >
        <div className="code-node-inspect__rel-header">
          <div className="code-node-inspect__rel-peer">
            <span className="code-arch-badge code-arch-badge--counter">{item.peerNode.type}</span>
            {item.peerNode.code && (
              <code className="code-node-inspect__rel-code">{item.peerNode.code}</code>
            )}
            <span className="code-node-inspect__rel-name" title={item.peerNode.name}>
              {item.peerNode.name}
            </span>
          </div>
          {item.status && (
            <span className={`code-arch-badge ${statusClass}`}>{item.status}</span>
          )}
        </div>

        {servicePreview && (
          <div className="code-node-inspect__rel-services" title={servicePreview}>
            {servicePreview}
            {item.services.length > 3 ? ` (+${item.services.length - 3})` : ''}
          </div>
        )}

        <div className="code-node-inspect__rel-footer">
          <div className="code-node-inspect__rel-metrics">
            <span className="code-arch-badge code-arch-badge--internal">
              {item.contractCount} 契约
            </span>
            {item.totalCalls > 0 && (
              <span className="code-arch-badge code-arch-badge--counter">
                ∑ {item.totalCalls.toLocaleString()} 次调用
              </span>
            )}
          </div>

          <div className="code-node-inspect__rel-actions">
            {onJumpToNode && (
              <button
                type="button"
                className="code-arch-toolbar__quick-depth-btn"
                onClick={() => onJumpToNode(item.peerNode)}
                data-testid={`relation-jump-node-${item.peerNode.id}`}
                title="选中对端节点并平移镜头追踪"
              >
                <Crosshair size={11} />
                <span>定位对端</span>
              </button>
            )}
            {onInspectEdge && (
              <button
                type="button"
                className="code-arch-toolbar__quick-depth-btn"
                onClick={() => onInspectEdge(item.edgeModel)}
                data-testid={`relation-inspect-edge-${item.edgeId}`}
                title="检视该连线的服务契约明细"
              >
                <FileCode2 size={11} />
                <span>查看契约</span>
              </button>
            )}
          </div>
        </div>
      </div>
    );
  };

  return (
    <Drawer
      open={open}
      onClose={onClose}
      title={`架构元素画像: ${node.name}`}
      width={480}
      mask={false}
    >
      <div className="code-node-inspect">
        {/* 顶部分段切换栏：上下游关系 vs 基础画像 */}
        <div className="code-node-inspect__toolbar">
          <div className="code-arch-segmented" role="tablist">
            <button
              type="button"
              role="tab"
              aria-selected={currentTab === 'RELATIONS'}
              className={`code-arch-segmented__item ${
                currentTab === 'RELATIONS' ? 'code-arch-segmented__item--active' : ''
              }`}
              onClick={() => handleSelectTab('RELATIONS')}
              data-testid="node-inspect-tab-relations"
            >
              <GitBranch size={13} />
              <span>上下游关系 ({totalRelations})</span>
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={currentTab === 'OVERVIEW'}
              className={`code-arch-segmented__item ${
                currentTab === 'OVERVIEW' ? 'code-arch-segmented__item--active' : ''
              }`}
              onClick={() => handleSelectTab('OVERVIEW')}
              data-testid="node-inspect-tab-overview"
            >
              <Info size={13} />
              <span>基础画像</span>
            </button>
          </div>

          {isContainer && onFocusScope && (
            <button
              type="button"
              className="code-arch-toolbar__quick-depth-btn"
              onClick={() => onFocusScope(node.id)}
              data-testid="node-inspect-focus-btn"
              title="进入该容器隔离聚焦视图"
            >
              <Crosshair size={12} />
              <span>聚焦子图</span>
            </button>
          )}
        </div>

        {currentTab === 'RELATIONS' ? (
          <div className="code-node-inspect__relations" data-testid="node-inspect-relations-pane">
            <div className="code-node-inspect__summary-bar">
              <span className="code-arch-badge code-arch-badge--counter">
                上游调用方: <strong>{upstreamList.length}</strong>
              </span>
              <span className="code-arch-badge code-arch-badge--counter">
                下游被调方: <strong>{downstreamList.length}</strong>
              </span>
              {internalCount > 0 && (
                <span className="code-arch-badge code-arch-badge--internal">
                  内部互调边: <strong>{internalCount}</strong>
                </span>
              )}
            </div>

            <div className="code-node-inspect__section">
              <h4 className="code-node-inspect__heading">
                <ArrowDownLeft size={13} />
                <span>调用此节点的上游 ({upstreamList.length})</span>
              </h4>
              {upstreamList.length === 0 ? (
                <div className="code-node-inspect__empty">当前可见范围内无上游调用方</div>
              ) : (
                <div className="code-node-inspect__rel-list">
                  {upstreamList.map(renderRelationCard)}
                </div>
              )}
            </div>

            <div className="code-node-inspect__section">
              <h4 className="code-node-inspect__heading">
                <ArrowUpRight size={13} />
                <span>此节点调用的下游 ({downstreamList.length})</span>
              </h4>
              {downstreamList.length === 0 ? (
                <div className="code-node-inspect__empty">当前可见范围内无下游被调方</div>
              ) : (
                <div className="code-node-inspect__rel-list">
                  {downstreamList.map(renderRelationCard)}
                </div>
              )}
            </div>
          </div>
        ) : (
          <div className="code-node-inspect__overview" data-testid="node-inspect-overview-pane">
            <div className="code-node-inspect__section">
              <h4 className="code-node-inspect__heading">基础标识</h4>
              <div className="code-node-inspect__row">
                <span className="code-node-inspect__label">节点 ID:</span>
                <code className="code-node-inspect__value">{node.id}</code>
              </div>
              <div className="code-node-inspect__row">
                <span className="code-node-inspect__label">节点类型:</span>
                <span className="code-node-inspect__value">{node.type}</span>
              </div>
              {node.code && (
                <div className="code-node-inspect__row">
                  <span className="code-node-inspect__label">业务编码:</span>
                  <code className="code-node-inspect__value">{node.code}</code>
                </div>
              )}
              <div className="code-node-inspect__row">
                <span className="code-node-inspect__label">层级深度 (Depth):</span>
                <span className="code-node-inspect__value">L{node.depth}</span>
              </div>
              {node.parent_id && (
                <div className="code-node-inspect__row">
                  <span className="code-node-inspect__label">直接父节点:</span>
                  <code className="code-node-inspect__value">{node.parent_id}</code>
                </div>
              )}
              {node.ancestors.length > 0 && (
                <div className="code-node-inspect__row">
                  <span className="code-node-inspect__label">祖先链路:</span>
                  <span className="code-node-inspect__value">{node.ancestors.join(' → ')}</span>
                </div>
              )}
            </div>

            {node.meta && Object.keys(node.meta).length > 0 && (
              <div className="code-node-inspect__section">
                <h4 className="code-node-inspect__heading">运行与视图元数据</h4>
                {node.meta.endpoint && (
                  <div className="code-node-inspect__row">
                    <span className="code-node-inspect__label">监听端点:</span>
                    <code className="code-node-inspect__value">{String(node.meta.endpoint)}</code>
                  </div>
                )}
                {node.meta.cmd_pattern && (
                  <div className="code-node-inspect__row">
                    <span className="code-node-inspect__label">命令行模式:</span>
                    <code className="code-node-inspect__value">{String(node.meta.cmd_pattern)}</code>
                  </div>
                )}
                {node.meta.provide_count != null && (
                  <div className="code-node-inspect__row">
                    <span className="code-node-inspect__label">对外供给服务:</span>
                    <span className="code-node-inspect__value">
                      {String(node.meta.provide_count)} 个
                    </span>
                  </div>
                )}
                {node.meta.description && (
                  <div className="code-node-inspect__row">
                    <span className="code-node-inspect__label">描述说明:</span>
                    <span className="code-node-inspect__value">
                      {String(node.meta.description)}
                    </span>
                  </div>
                )}
              </div>
            )}
          </div>
        )}
      </div>
    </Drawer>
  );
};
