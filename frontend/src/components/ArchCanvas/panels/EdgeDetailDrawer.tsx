import React from 'react';
import { Drawer } from '@code/common';
import type { GraphNode, RollupEdgeModel } from '../../../types/topology_graph';
import { ArrowDown, AlertTriangle, FileText, Info, Target } from 'lucide-react';

export interface EdgeDetailDrawerProps {
  open: boolean;
  onClose: () => void;
  edge: RollupEdgeModel | null;
  nodeMap?: ReadonlyMap<string, GraphNode> | Record<string, GraphNode>;
  onFocusScope?: (scopeId: string) => void;
  onViewTrafficRecord?: (recordId: string) => void;
}

const NODE_TYPE_ZH_LABEL: Record<string, string> = {
  SUBSYSTEM: '子系统',
  FUNCTIONAL_GROUP: '功能组',
  MODULE: '微服务模块',
  PROCESS_GROUP: '进程组',
  PROCESS: '物理进程',
  EXTERNAL_CONTAINER: '外部域',
  EXTERNAL_SYSTEM: '外部依赖',
  EXTERNAL_ENDPOINT: '外部端点',
  EXTERNAL_CONTEXT: '外部上下文',
  UNREGISTERED_INTERFACE: '未登记接口',
  UNDEPLOYED_CONTAINER: '未部署容器',
};

function isContainerScopeType(type?: string): boolean {
  return (
    type === 'SUBSYSTEM' ||
    type === 'FUNCTIONAL_GROUP' ||
    type === 'PROCESS_GROUP' ||
    type === 'PROCESS' ||
    type === 'UNDEPLOYED_CONTAINER' ||
    type === 'EXTERNAL_CONTAINER'
  );
}

function lookupNode(
  nodeMap: ReadonlyMap<string, GraphNode> | Record<string, GraphNode> | undefined,
  nodeId: string
): GraphNode | undefined {
  if (!nodeMap) return undefined;
  if (nodeMap instanceof Map) {
    return nodeMap.get(nodeId);
  }
  return (nodeMap as Record<string, GraphNode>)[nodeId];
}

function resolveEndpointDisplay(
  rawNodeId: string,
  nodeMap?: ReadonlyMap<string, GraphNode> | Record<string, GraphNode>
): {
  typeLabel: string;
  name: string;
  code?: string;
  jumpScopeId?: string;
} {
  const gn = lookupNode(nodeMap, rawNodeId);
  if (gn) {
    const jumpScopeId =
      gn.type === 'EXTERNAL_CONTEXT'
        ? gn.meta?.peer_root_id
          ? String(gn.meta.peer_root_id)
          : undefined
        : isContainerScopeType(gn.type)
        ? gn.id
        : gn.parent_id || 'GLOBAL';
    return {
      typeLabel: NODE_TYPE_ZH_LABEL[gn.type] || '架构元素',
      name: gn.name || gn.code || rawNodeId,
      code: gn.code,
      jumpScopeId,
    };
  }

  if (rawNodeId.startsWith('ext_ctx_')) {
    const stripped = rawNodeId.replace(/^ext_ctx_(UPSTREAM|DOWNSTREAM)_/, '');
    const peer = lookupNode(nodeMap, stripped);
    if (peer) {
      return {
        typeLabel: '外部域',
        name: peer.name || peer.code || '外部关联域',
        code: peer.code,
        jumpScopeId: isContainerScopeType(peer.type) ? peer.id : peer.parent_id || 'GLOBAL',
      };
    }
    return {
      typeLabel: '外部上下文',
      name: '跨作用域外部关联域',
      jumpScopeId: stripped || undefined,
    };
  }

  return {
    typeLabel: '架构元素',
    name: rawNodeId,
  };
}

export const EdgeDetailDrawer: React.FC<EdgeDetailDrawerProps> = ({
  open,
  onClose,
  edge,
  nodeMap,
  onFocusScope,
  onViewTrafficRecord,
}) => {
  if (!edge) return null;

  const totalCalls = edge.totalCalls || 0;
  const sourceEp = resolveEndpointDisplay(edge.source, nodeMap);
  const targetEp = resolveEndpointDisplay(edge.target, nodeMap);

  return (
    <Drawer
      open={open}
      onClose={onClose}
      title="架构依赖契约与实测边画像"
      width={520}
      mask={false}
    >
      <div className="code-edge-detail">
        {/* 顶部人类可读两端节点卡片与域下钻入口 */}
        <div className="code-edge-detail__readable-endpoints" data-testid="edge-detail-endpoints">
          <div className="code-edge-detail__endpoint-card" data-testid="edge-detail-source">
            <div className="code-edge-detail__endpoint-meta">
              <span className="code-arch-badge code-arch-badge--provide">
                {sourceEp.typeLabel}
              </span>
              <span className="code-edge-detail__endpoint-name">{sourceEp.name}</span>
              {sourceEp.code && (
                <span className="code-edge-detail__endpoint-code">({sourceEp.code})</span>
              )}
            </div>
            {onFocusScope && sourceEp.jumpScopeId && (
              <button
                type="button"
                className="code-btn code-btn--secondary code-btn--xs"
                onClick={() => onFocusScope(sourceEp.jumpScopeId!)}
                data-testid="edge-jump-source-btn"
              >
                <Target size={12} />
                <span>进入源域</span>
              </button>
            )}
          </div>

          <div className="code-edge-detail__row">
            <ArrowDown size={15} className="code-edge-detail__arrow" />
            <span className="code-edge-detail__label">调用 / 依赖方向 (Source → Target)</span>
          </div>

          <div className="code-edge-detail__endpoint-card" data-testid="edge-detail-target">
            <div className="code-edge-detail__endpoint-meta">
              <span className="code-arch-badge code-arch-badge--consume">
                {targetEp.typeLabel}
              </span>
              <span className="code-edge-detail__endpoint-name">{targetEp.name}</span>
              {targetEp.code && (
                <span className="code-edge-detail__endpoint-code">({targetEp.code})</span>
              )}
            </div>
            {onFocusScope && targetEp.jumpScopeId && (
              <button
                type="button"
                className="code-btn code-btn--secondary code-btn--xs"
                onClick={() => onFocusScope(targetEp.jumpScopeId!)}
                data-testid="edge-jump-target-btn"
              >
                <Target size={12} />
                <span>进入目标域</span>
              </button>
            )}
          </div>
        </div>

        {/* 边属性摘要 */}
        <div className="code-edge-detail__section">
          <h4 className="code-edge-detail__heading">契约关系属性</h4>
          <div className="code-edge-detail__row">
            <span className="code-edge-detail__label">连线分类:</span>
            <span className="code-edge-detail__value">{edge.kind}</span>
          </div>

          {edge.status && (
            <div className="code-edge-detail__row">
              <span className="code-edge-detail__label">一致性状态:</span>
              <span className={`code-arch-badge code-arch-edge-badge--${edge.status.toLowerCase()}`}>
                {edge.status}
              </span>
            </div>
          )}

          {edge.status === 'OUTBOUND_VERIFIED' && (
            <div className="code-edge-detail__info-alert">
              <Info size={14} />
              <span>
                实测已确凿捕获源端出站调用，但目标供给方物理进程未纳入本次监控。建议在录制会话中纳管目标进程以实现双端闭环验证。
              </span>
            </div>
          )}

          {edge.isCycle && (
            <div className="code-edge-detail__cycle-alert">
              <AlertTriangle size={14} />
              <span>检测到架构循环依赖 (Tarjan SCC 强连通分量闭环)，违反架构单向调用分层原则！</span>
            </div>
          )}

          {totalCalls > 0 && (
            <div className="code-edge-detail__row">
              <span className="code-edge-detail__label">实测调用总次数:</span>
              <span className="code-edge-detail__value font-mono">
                {totalCalls.toLocaleString()} 次
              </span>
            </div>
          )}

          {edge.sampleTrafficRecordId && onViewTrafficRecord && (
            <div className="code-edge-detail__row">
              <button
                type="button"
                className="code-btn code-btn--primary code-btn--sm"
                onClick={() => onViewTrafficRecord(String(edge.sampleTrafficRecordId))}
              >
                <FileText size={13} />
                <span>穿透查看采样报文解剖 (Record #{edge.sampleTrafficRecordId})</span>
              </button>
            </div>
          )}
        </div>

        {/* 聚合服务契约列表 */}
        <div className="code-edge-detail__section">
          <h4 className="code-edge-detail__heading">
            聚合服务契约明细 ({edge.services.length} 个)
          </h4>
          <table className="code-table">
            <thead>
              <tr>
                <th>序号</th>
                <th>服务契约名称</th>
                <th>描述说明</th>
              </tr>
            </thead>
            <tbody>
              {edge.services.map((svc, idx) => (
                <tr key={`${svc.service_name}-${idx}`}>
                  <td>{idx + 1}</td>
                  <td>
                    <code className="font-mono text-xs">{svc.service_name}</code>
                  </td>
                  <td>{svc.description || '-'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </Drawer>
  );
};
