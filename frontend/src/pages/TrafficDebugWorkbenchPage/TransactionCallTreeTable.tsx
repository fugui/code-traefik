import React, { useState, useEffect } from 'react';
import type { TransactionTreeGroupDTO, TransactionTreeNodeDTO, TrafficRecordSummary } from '../../types';
import { InlineWaterfallBar } from './InlineWaterfallBar';

interface TransactionCallTreeTableProps {
  groups: TransactionTreeGroupDTO[];
  loading?: boolean;
  onSelectRecordForAnatomy: (record: TrafficRecordSummary) => void;
  onOpenSequenceDrawer: (rootRecordId: string) => void;
  onSinglePacketReplay?: (record: TrafficRecordSummary) => void;
  forceExpandAll?: boolean;
}

export const TransactionCallTreeTable: React.FC<TransactionCallTreeTableProps> = ({
  groups,
  loading,
  onSelectRecordForAnatomy,
  onOpenSequenceDrawer,
  onSinglePacketReplay,
  forceExpandAll,
}) => {
  const [expandedNodes, setExpandedNodes] = useState<Set<string>>(() => {
    const allIds = new Set<string>();
    const collect = (n: TransactionTreeNodeDTO) => {
      allIds.add(n.record.id);
      n.children?.forEach(collect);
    };
    if (forceExpandAll !== false) {
      groups.forEach((g) => collect(g.root));
    }
    return allIds;
  });

  // 初始化或响应展开/折叠全部
  useEffect(() => {
    if (forceExpandAll) {
      const allIds = new Set<string>();
      const collect = (n: TransactionTreeNodeDTO) => {
        allIds.add(n.record.id);
        n.children.forEach(collect);
      };
      groups.forEach((g) => collect(g.root));
      setExpandedNodes(allIds);
    } else if (forceExpandAll === false) {
      setExpandedNodes(new Set());
    } else {
      // 默认展开全部节点方便排障
      const allIds = new Set<string>();
      const collect = (n: TransactionTreeNodeDTO) => {
        allIds.add(n.record.id);
        n.children.forEach(collect);
      };
      groups.forEach((g) => collect(g.root));
      setExpandedNodes(allIds);
    }
  }, [groups, forceExpandAll]);

  const toggleExpand = (recordId: string) => {
    setExpandedNodes((prev) => {
      const next = new Set(prev);
      if (next.has(recordId)) {
        next.delete(recordId);
      } else {
        next.add(recordId);
      }
      return next;
    });
  };

  const renderStatus = (record: TrafficRecordSummary) => {
    const isProtoSuccess = record.status_code === '0' || record.status_code === 0;
    return (
      <div style={{ display: 'inline-flex', flexDirection: 'column', gap: '2px' }}>
        <span
          className={`code-badge ${isProtoSuccess ? 'code-badge-success' : 'code-badge-danger'}`}
          style={{
            fontSize: '11px',
            padding: '1px 6px',
            borderRadius: '4px',
            backgroundColor: isProtoSuccess ? 'var(--color-success-bg, #ecfdf5)' : 'var(--color-danger-bg, #fef2f2)',
            color: isProtoSuccess ? 'var(--color-success, #10b981)' : 'var(--color-danger, #ef4444)',
            border: `1px solid ${isProtoSuccess ? 'var(--color-success-border, #a7f3d0)' : 'var(--color-danger-border, #fecaca)'}`,
          }}
        >
          {isProtoSuccess ? '0 OK' : `Err: ${record.status_code}`}
        </span>
        {record.biz_error && (
          <span
            title="协议层成功但载荷返回业务软错误"
            style={{
              fontSize: '10px',
              padding: '1px 4px',
              borderRadius: '3px',
              backgroundColor: 'var(--color-danger-bg, #fef2f2)',
              color: 'var(--color-danger, #ef4444)',
              border: '1px dashed var(--color-danger, #ef4444)',
            }}
          >
            {`⚠️ 业务异常 ${record.biz_status_summary ? `(${record.biz_status_summary})` : ''}`}
          </span>
        )}
      </div>
    );
  };

  const renderRelationBadge = (relation: string, isRoot: boolean) => {
    if (isRoot) {
      return (
        <span
          style={{
            fontSize: '11px',
            padding: '1px 6px',
            borderRadius: '10px',
            backgroundColor: 'var(--color-primary-bg, #eff6ff)',
            color: 'var(--color-primary, #3b82f6)',
            fontWeight: 600,
          }}
        >
          根事务
        </span>
      );
    }
    if (relation === 'COMPENSATION') {
      return (
        <span
          style={{
            fontSize: '11px',
            padding: '1px 6px',
            borderRadius: '10px',
            backgroundColor: 'var(--color-warning-bg, #fffbeb)',
            color: 'var(--color-warning, #d97706)',
            fontWeight: 600,
          }}
        >
          🔄 逆向补偿
        </span>
      );
    }
    if (relation === 'PARALLEL') {
      return (
        <span
          style={{
            fontSize: '11px',
            padding: '1px 6px',
            borderRadius: '10px',
            backgroundColor: 'var(--color-info-bg, #ecfeff)',
            color: 'var(--color-info, #0891b2)',
            fontWeight: 600,
          }}
        >
          ⚡ 并发
        </span>
      );
    }
    return (
      <span
        style={{
          fontSize: '11px',
          padding: '1px 6px',
          borderRadius: '10px',
          backgroundColor: 'var(--color-bg-secondary, #f1f5f9)',
          color: 'var(--color-text-secondary, #64748b)',
        }}
      >
        串行
      </span>
    );
  };

  const renderNodeRows = (node: TransactionTreeNodeDTO, rootRecordId: string): React.ReactNode => {
    const isExpanded = expandedNodes.has(node.record.id);
    const hasChildren = node.children && node.children.length > 0;
    const isRoot = node.depth === 0;
    const isError = node.record.status_code !== '0' && node.record.status_code !== 0 || node.record.biz_error;

    return (
      <React.Fragment key={node.record.id}>
        <tr
          className={`code-tree-table-row ${node.matched_filter ? 'highlight-matched' : ''}`}
          style={{
            backgroundColor: node.matched_filter
              ? 'var(--color-warning-bg, rgba(254, 243, 199, 0.5))'
              : undefined,
            borderBottom: '1px solid var(--color-border, #f1f5f9)',
          }}
        >
          {/* 1. 层级与折叠 */}
          <td style={{ padding: '8px 12px', paddingLeft: `${node.depth * 20 + 12}px`, whiteSpace: 'nowrap' }}>
            <div style={{ display: 'inline-flex', alignItems: 'center', gap: '6px' }}>
              {hasChildren ? (
                <button
                  type="button"
                  onClick={() => toggleExpand(node.record.id)}
                  style={{
                    background: 'none',
                    border: 'none',
                    cursor: 'pointer',
                    padding: '2px 4px',
                    fontSize: '12px',
                    color: 'var(--color-text-secondary, #64748b)',
                  }}
                  title={isExpanded ? '折叠子调用' : '展开子调用'}
                >
                  {isExpanded ? '▼' : '▶'}
                </button>
              ) : (
                <span style={{ display: 'inline-block', width: '16px', textAlign: 'center', color: '#cbd5e1' }}>•</span>
              )}
              <span
                style={{
                  fontSize: '11px',
                  color: 'var(--color-text-secondary, #64748b)',
                  fontFamily: 'monospace',
                }}
              >
                {`#${node.record.seq_no ?? '-'}`}
              </span>
              <span
                style={{
                  fontSize: '11px',
                  padding: '1px 5px',
                  borderRadius: '3px',
                  backgroundColor: 'var(--color-bg-secondary, #f1f5f9)',
                  color: 'var(--color-text-secondary, #475569)',
                  fontFamily: 'monospace',
                }}
              >
                {`Conn#${node.record.conn_index ?? 1} · ${node.record.sno}`}
              </span>
            </div>
          </td>

          {/* 2. 架构链路与缝合 */}
          <td style={{ padding: '8px 12px', fontSize: '12px' }}>
            {node.stitched_ingress_record ? (
              <span
                title={`跨进程缝合: 网络跳耗时约 ${node.net_hop_us ?? 0}µs`}
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: '4px',
                  backgroundColor: 'var(--color-primary-bg, #eff6ff)',
                  padding: '2px 6px',
                  borderRadius: '4px',
                  color: 'var(--color-primary, #3b82f6)',
                  fontWeight: 500,
                }}
              >
                {`🔗 ${node.record.process_name || `Proc#${node.record.process_id}`} ➔ ${node.stitched_ingress_record.process_name || `Proc#${node.stitched_ingress_record.process_id}`}`}
              </span>
            ) : (
              <span>
                {`${node.record.src_module_name || node.record.peer_proc_name || '-'} ➔ ${node.record.dst_module_name || node.record.process_name || '-'}`}
              </span>
            )}
          </td>

          {/* 3. 服务契约与接口方法 */}
          <td style={{ padding: '8px 12px', fontSize: '13px' }}>
            <div style={{ display: 'flex', flexDirection: 'column' }}>
              <span style={{ fontWeight: 600, color: 'var(--color-text-primary, #1e293b)' }}>
                {node.record.operation}
              </span>
              <span style={{ fontSize: '11px', color: 'var(--color-text-secondary, #64748b)' }}>
                {node.record.service_name}
              </span>
            </div>
          </td>

          {/* 4. 方向与兄弟时序关系 */}
          <td style={{ padding: '8px 12px', fontSize: '12px', whiteSpace: 'nowrap' }}>
            <div style={{ display: 'inline-flex', alignItems: 'center', gap: '6px' }}>
              <span
                style={{
                  fontSize: '11px',
                  fontWeight: 600,
                  color: node.record.direction === 'INGRESS' ? 'var(--color-success, #10b981)' : 'var(--color-primary, #3b82f6)',
                }}
              >
                {node.record.direction}
              </span>
              {renderRelationBadge(node.sibling_relation, isRoot)}
            </div>
          </td>

          {/* 5. 双态状态码 */}
          <td style={{ padding: '8px 12px', whiteSpace: 'nowrap' }}>
            {renderStatus(node.record)}
          </td>

          {/* 6. 耗时 & 内联迷你甘特瀑布 */}
          <td style={{ padding: '8px 12px', whiteSpace: 'nowrap' }}>
            <div style={{ display: 'inline-flex', alignItems: 'center', gap: '8px' }}>
              <span style={{ fontSize: '12px', minWidth: '55px', textAlign: 'right', fontFamily: 'monospace' }}>
                {node.record.latency_us ? `${node.record.latency_us}µs` : '-'}
              </span>
              <InlineWaterfallBar
                offsetPct={node.waterfall_offset_pct}
                widthPct={node.waterfall_width_pct}
                durationUs={node.record.latency_us}
                relation={node.sibling_relation}
                hasError={isError}
              />
            </div>
          </td>

          {/* 7. 操作栏 */}
          <td style={{ padding: '8px 12px', whiteSpace: 'nowrap', textAlign: 'right' }}>
            <div style={{ display: 'inline-flex', gap: '6px' }}>
              {isRoot && (
                <button
                  type="button"
                  onClick={() => onOpenSequenceDrawer(rootRecordId)}
                  className="code-btn code-btn-secondary"
                  style={{
                    padding: '3px 8px',
                    fontSize: '12px',
                    borderRadius: '4px',
                    cursor: 'pointer',
                    backgroundColor: 'var(--color-bg-secondary, #f8fafc)',
                    border: '1px solid var(--color-border, #cbd5e1)',
                    color: 'var(--color-primary, #3b82f6)',
                    fontWeight: 500,
                  }}
                  title="查看该事务的全链路时序泳道图"
                >
                  📊 时序泳道
                </button>
              )}
              <button
                type="button"
                onClick={() => onSelectRecordForAnatomy(node.record)}
                className="code-btn code-btn-secondary"
                style={{
                  padding: '3px 8px',
                  fontSize: '12px',
                  borderRadius: '4px',
                  cursor: 'pointer',
                  backgroundColor: 'var(--color-bg-secondary, #f8fafc)',
                  border: '1px solid var(--color-border, #cbd5e1)',
                  color: 'var(--color-text-primary, #334155)',
                }}
              >
                🔬 报文解剖
              </button>
              {node.record.direction === 'INGRESS' && node.record.is_replayable && onSinglePacketReplay && (
                <button
                  type="button"
                  onClick={() => onSinglePacketReplay(node.record)}
                  className="code-btn code-btn-primary"
                  style={{
                    padding: '3px 8px',
                    fontSize: '12px',
                    borderRadius: '4px',
                    cursor: 'pointer',
                    backgroundColor: 'var(--color-primary, #3b82f6)',
                    border: 'none',
                    color: '#ffffff',
                  }}
                >
                  🔁 事务重放
                </button>
              )}
            </div>
          </td>
        </tr>

        {/* 递归渲染已展开的子节点 */}
        {hasChildren && isExpanded && node.children.map((child) => renderNodeRows(child, rootRecordId))}
      </React.Fragment>
    );
  };

  if (loading) {
    return (
      <div style={{ padding: '40px', textAlign: 'center', color: 'var(--color-text-secondary, #64748b)' }}>
        正在组装报文因果树...
      </div>
    );
  }

  if (!groups || groups.length === 0) {
    return (
      <div style={{ padding: '40px', textAlign: 'center', color: 'var(--color-text-secondary, #64748b)' }}>
        暂无符合筛选条件的事务树
      </div>
    );
  }

  return (
    <div className="code-tree-table-wrapper" style={{ overflowX: 'auto' }}>
      <table
        className="code-tree-table"
        style={{
          width: '100%',
          borderCollapse: 'collapse',
          textAlign: 'left',
          fontSize: '13px',
        }}
      >
        <thead>
          <tr
            style={{
              backgroundColor: 'var(--color-bg-secondary, #f8fafc)',
              borderBottom: '2px solid var(--color-border, #e2e8f0)',
              color: 'var(--color-text-secondary, #475569)',
              fontSize: '12px',
            }}
          >
            <th style={{ padding: '10px 12px' }}>层级 / 发起序 / 连接</th>
            <th style={{ padding: '10px 12px' }}>架构链路</th>
            <th style={{ padding: '10px 12px' }}>服务契约 & 接口方法</th>
            <th style={{ padding: '10px 12px' }}>方向 / 关系</th>
            <th style={{ padding: '10px 12px' }}>双态状态码</th>
            <th style={{ padding: '10px 12px' }}>耗时 & 甘特瀑布</th>
            <th style={{ padding: '10px 12px', textAlign: 'right' }}>操作</th>
          </tr>
        </thead>
        <tbody>
          {groups.map((group) => renderNodeRows(group.root, group.root_record_id))}
        </tbody>
      </table>
    </div>
  );
};
