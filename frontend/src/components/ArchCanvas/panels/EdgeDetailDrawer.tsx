import React from 'react';
import { Drawer } from '@code/common';
import type { RollupEdgeModel } from '../../../types/topology_graph';
import { ArrowRight, AlertTriangle, FileText } from 'lucide-react';

export interface EdgeDetailDrawerProps {
  open: boolean;
  onClose: () => void;
  edge: RollupEdgeModel | null;
  onViewTrafficRecord?: (recordId: string) => void;
}

export const EdgeDetailDrawer: React.FC<EdgeDetailDrawerProps> = ({
  open,
  onClose,
  edge,
  onViewTrafficRecord,
}) => {
  if (!edge) return null;

  const totalCalls = edge.totalCalls || 0;

  return (
    <Drawer open={open} onClose={onClose} title="架构依赖契约与实测边画像" width={520}>
      <div className="code-edge-detail">
        {/* 顶部两端节点信息 */}
        <div className="code-edge-detail__endpoints">
          <div className="code-edge-detail__ep-card">
            <span className="code-edge-detail__ep-label">调用源端 (Source)</span>
            <span className="code-edge-detail__ep-id">{edge.source}</span>
          </div>
          <ArrowRight size={18} className="code-edge-detail__arrow" />
          <div className="code-edge-detail__ep-card">
            <span className="code-edge-detail__ep-label">被调目标 (Target)</span>
            <span className="code-edge-detail__ep-id">{edge.target}</span>
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
            <div className="code-edge-detail__row" style={{ marginTop: 8 }}>
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

