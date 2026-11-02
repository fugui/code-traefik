import React, { useMemo, useState, useEffect } from 'react';
import { Pagination } from '@code/common';
import type { ProtoOperation } from '../../types';
import type { ServiceContractGroupDTO } from '../../types/schema_tree';
import { ServiceSummaryRow } from './ServiceSummaryRow';

export interface OperationMappingTableProps {
  operations?: ProtoOperation[];
  serviceGroups?: ServiceContractGroupDTO[];
  totalServices?: number;
  page?: number;
  pageSize?: number;
  onPageChange?: (p: number) => void;
  onPageSizeChange?: (s: number) => void;
  onSelectOperation: (op: ProtoOperation) => void;
  searchKeyword?: string;
  claimMap?: Record<string, { id: string; code: string; name: string; subsystemName?: string }>;
  onInspectSchema?: (schemaId: string | number) => void;
  orphanOnly?: boolean;
  onOrphanOnlyChange?: (val: boolean) => void;
  selectedSchemaId?: string;
  onSelectSchemaId?: (id: string) => void;
  availableSchemas?: Array<{ id: string | number; name: string; version: number; category?: string }>;
  loading?: boolean;
}

/** 关键词高亮渲染纯函数 */
export function highlightText(text: string, keyword?: string): React.ReactNode {
  if (!keyword || !keyword.trim()) {
    return text;
  }
  const kw = keyword.trim();
  const lowerText = text.toLowerCase();
  const lowerKw = kw.toLowerCase();
  const index = lowerText.indexOf(lowerKw);
  if (index === -1) {
    return text;
  }

  const before = text.slice(0, index);
  const match = text.slice(index, index + kw.length);
  const after = text.slice(index + kw.length);

  return (
    <>
      {before}
      <mark className="code-search-highlight">{match}</mark>
      {highlightText(after, keyword)}
    </>
  );
}

/** 聚合平铺 Operations 为服务维度 DTO 列表 */
export function rollupServices(
  operations: ProtoOperation[],
  claimMap?: Record<string, { id: string; code: string; name: string; subsystemName?: string }>
): ServiceContractGroupDTO[] {
  const map = new Map<string, ServiceContractGroupDTO>();

  for (const op of operations ?? []) {
    const key = op.service_name || `${op.proto_package}.${op.proto_service}`;
    let group = map.get(key);
    if (!group) {
      group = {
        serviceName: key,
        protoPackage: op.proto_package,
        protoService: op.proto_service,
        protoFile: op.proto_file,
        claimedByModule: claimMap?.[key],
        totalOperations: 0,
        unaryCount: 0,
        streamCount: 0,
        isActive: false,
        operations: [],
      };
      map.set(key, group);
    }

    group.totalOperations = (group.totalOperations ?? 0) + 1;
    if (op.client_streaming || op.server_streaming) {
      group.streamCount = (group.streamCount ?? 0) + 1;
    } else {
      group.unaryCount = (group.unaryCount ?? 0) + 1;
    }
    if (op.is_active) {
      group.isActive = true;
    }
    group.operations.push(op);
  }

  return Array.from(map.values()).sort((a, b) => {
    const nameA = a.serviceName || a.service_name || '';
    const nameB = b.serviceName || b.service_name || '';
    return nameA.localeCompare(nameB);
  });
}

export const OperationMappingTable: React.FC<OperationMappingTableProps> = ({
  operations = [],
  serviceGroups,
  totalServices,
  page,
  pageSize,
  onPageChange,
  onPageSizeChange,
  onSelectOperation,
  searchKeyword = '',
  claimMap,
  onInspectSchema,
  orphanOnly = false,
  onOrphanOnlyChange,
  selectedSchemaId = '',
  onSelectSchemaId,
  availableSchemas = [],
  loading = false,
}) => {
  const [userExpandedKeys, setUserExpandedKeys] = useState<Set<string> | null>(null);

  // 1. 如果直接传入了后端分页聚合的 serviceGroups 则优先使用；否则从 operations 聚合并排序
  const allGroups = useMemo(() => {
    if (serviceGroups) {
      return serviceGroups;
    }
    return rollupServices(operations, claimMap);
  }, [serviceGroups, operations, claimMap]);

  // 2. 搜索穿透逻辑：计算包含匹配项的服务并自动展开
  const { filteredGroups, autoExpandedKeys } = useMemo(() => {
    const kw = searchKeyword.trim().toLowerCase();
    if (!kw && !orphanOnly) {
      return { filteredGroups: allGroups, autoExpandedKeys: new Set<string>() };
    }

    const autoKeys = new Set<string>();
    const matched: ServiceContractGroupDTO[] = [];

    for (const grp of allGroups) {
      const sName = grp.serviceName || grp.service_name || '';
      const pFile = grp.protoFile || grp.proto_file || '';
      const isClaimed = Boolean(grp.claimedByModule || grp.claimed_by_module || grp.isClaimed || grp.is_claimed);

      if (orphanOnly && isClaimed) {
        continue;
      }

      if (!kw) {
        matched.push(grp);
        continue;
      }

      const serviceMatch = sName.toLowerCase().includes(kw) || pFile.toLowerCase().includes(kw);
      const matchedOps = (grp.operations || []).filter(
        (op) =>
          op.operation.toLowerCase().includes(kw) ||
          (op.req_message_full_name && op.req_message_full_name.toLowerCase().includes(kw)) ||
          (op.rsp_message_full_name && op.rsp_message_full_name.toLowerCase().includes(kw))
      );

      if (serviceMatch || matchedOps.length > 0) {
        matched.push({
          ...grp,
          operations: serviceMatch ? grp.operations : matchedOps,
        });
        autoKeys.add(sName);
      }
    }

    return { filteredGroups: matched, autoExpandedKeys: autoKeys };
  }, [allGroups, searchKeyword, orphanOnly]);

  // 搜索关键字发生变动时重置手动展开缓存，恢复由搜索穿透接管
  useEffect(() => {
    setUserExpandedKeys(null);
  }, [searchKeyword]);

  // 最终生效的展开服务集合：用户手动操作优先，未操作时若搜索中则默认穿透展开
  const expandedServices = useMemo(() => {
    if (userExpandedKeys !== null) {
      return userExpandedKeys;
    }
    if (searchKeyword.trim()) {
      return autoExpandedKeys;
    }
    return new Set<string>();
  }, [userExpandedKeys, searchKeyword, autoExpandedKeys]);

  const toggleExpand = (serviceName: string) => {
    setUserExpandedKeys(() => {
      const next = new Set(expandedServices);
      if (next.has(serviceName)) {
        next.delete(serviceName);
      } else {
        next.add(serviceName);
      }
      return next;
    });
  };

  const handleExpandAll = () => {
    const allKeys = new Set<string>();
    for (const g of filteredGroups) {
      allKeys.add(g.serviceName || g.service_name || '');
    }
    setUserExpandedKeys(allKeys);
  };

  const handleCollapseAll = () => {
    setUserExpandedKeys(new Set<string>());
  };

  return (
    <div className="code-operation-mapping-pane">
      <div className="code-operation-mapping-toolbar" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '12px', flexWrap: 'wrap', gap: '8px' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '12px', flexWrap: 'wrap' }}>
          <span className="code-text-sm code-color-muted">
            聚合服务: <strong>{totalServices !== undefined ? totalServices : filteredGroups.length}</strong> 个
            {searchKeyword && ` (当前匹配 ${filteredGroups.length} 个)`}
          </span>

          {onOrphanOnlyChange && (
            <button
              type="button"
              className={`btn ${orphanOnly ? 'btn-primary' : 'btn-secondary'}`}
              onClick={() => onOrphanOnlyChange(!orphanOnly)}
              style={{ fontSize: '12px', padding: '3px 10px' }}
            >
              <span>{orphanOnly ? '✓ 仅看未认领孤儿' : '仅看未认领孤儿'}</span>
            </button>
          )}

          {onSelectSchemaId && availableSchemas.length > 0 && (
            <select
              value={selectedSchemaId}
              onChange={(e) => onSelectSchemaId(e.target.value)}
              style={{
                fontSize: '12px',
                padding: '4px 8px',
                borderRadius: '4px',
                border: '1px solid var(--color-border-primary)',
                background: 'var(--color-bg-surface)',
                color: 'var(--color-text-primary)',
              }}
            >
              <option value="">全部归属描述集</option>
              {availableSchemas.map((sch) => (
                <option key={sch.id} value={String(sch.id)}>
                  {sch.name} (v{sch.version})
                </option>
              ))}
            </select>
          )}
        </div>

        <div className="code-toolbar-actions" style={{ display: 'flex', gap: '8px' }}>
          <button
            type="button"
            className="code-btn code-btn--secondary code-btn--xs"
            onClick={handleExpandAll}
          >
            全部展开
          </button>
          <button
            type="button"
            className="code-btn code-btn--secondary code-btn--xs"
            onClick={handleCollapseAll}
          >
            全部收起
          </button>
        </div>
      </div>

      <div style={{ border: '1px solid var(--color-border-primary)', borderRadius: '8px', overflow: 'hidden', background: 'var(--color-bg-surface)' }}>
        <table className="code-table code-nested-table" style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ background: 'var(--color-bg-muted)', borderBottom: '1px solid var(--color-border-primary)', textAlign: 'left' }}>
              <th style={{ padding: '10px 12px' }}>服务定义名称 (service_name) / 来源描述集</th>
              <th style={{ padding: '10px 12px' }}>Proto Package</th>
              <th style={{ padding: '10px 12px' }}>架构认领模块</th>
              <th style={{ padding: '10px 12px' }}>RPC 方法统计</th>
              <th style={{ padding: '10px 12px' }}>生效状态</th>
              <th style={{ padding: '10px 12px', textAlign: 'right' }}>操作</th>
            </tr>
          </thead>
          <tbody>
            {filteredGroups.map((group) => {
              const sKey = group.serviceName || group.service_name || '';
              const isExpanded = expandedServices.has(sKey);
              return (
                <React.Fragment key={sKey}>
                  <ServiceSummaryRow
                    group={group}
                    isExpanded={isExpanded}
                    onToggleExpand={() => toggleExpand(sKey)}
                    searchKeyword={searchKeyword}
                    renderHighlight={(txt) => highlightText(txt, searchKeyword)}
                    onInspectSchema={onInspectSchema}
                  />

                  {isExpanded && (
                    <tr className="code-nested-detail-row">
                      <td colSpan={6} className="code-nested-detail-container" style={{ padding: '0', background: 'var(--color-bg-muted)' }}>
                        <div className="code-nested-subtable-pane" style={{ padding: '12px 16px' }}>
                          <table className="code-table code-table--compact code-subtable" style={{ width: '100%', borderCollapse: 'collapse', fontSize: '12px', background: 'var(--color-bg-surface)', borderRadius: '6px', overflow: 'hidden', border: '1px solid var(--color-border-primary)' }}>
                            <thead>
                              <tr style={{ background: 'var(--color-bg-muted)', borderBottom: '1px solid var(--color-border-primary)', textAlign: 'left' }}>
                                <th style={{ padding: '8px 12px' }}>调用方法 (operation)</th>
                                <th style={{ padding: '8px 12px' }}>请求消息体 (req_message)</th>
                                <th style={{ padding: '8px 12px' }}>应答消息体 (rsp_message)</th>
                                <th style={{ padding: '8px 12px' }}>流式模式</th>
                                <th style={{ padding: '8px 12px' }}>Diff 忽略字段</th>
                                <th style={{ padding: '8px 12px' }}>状态</th>
                                <th style={{ padding: '8px 12px', textAlign: 'right' }}>操作</th>
                              </tr>
                            </thead>
                            <tbody>
                              {(group.operations || []).map((op) => (
                                <tr key={op.id} className="code-subtable-row" data-testid={`op-row-${op.operation}`} style={{ borderBottom: '1px solid var(--color-border-primary)' }}>
                                  <td style={{ padding: '8px 12px' }} className="code-font-mono code-font-bold">
                                    {highlightText(op.operation, searchKeyword)}
                                  </td>
                                  <td style={{ padding: '8px 12px' }} className="code-font-mono code-text-xs code-color-muted">
                                    {op.req_message_full_name || '-'}
                                  </td>
                                  <td style={{ padding: '8px 12px' }} className="code-font-mono code-text-xs code-color-muted">
                                    {op.rsp_message_full_name || '-'}
                                  </td>
                                  <td style={{ padding: '8px 12px' }}>
                                    {op.client_streaming || op.server_streaming ? (
                                      <span className="code-badge code-badge--info">
                                        {op.client_streaming ? 'ClientStream ' : ''}
                                        {op.server_streaming ? 'ServerStream' : ''}
                                      </span>
                                    ) : (
                                      <span className="code-badge code-badge--secondary">Unary</span>
                                    )}
                                  </td>
                                  <td style={{ padding: '8px 12px' }}>
                                    {op.diff_ignore_paths && op.diff_ignore_paths.length > 0 ? (
                                      <span className="code-badge code-badge--warning" title={op.diff_ignore_paths.join(', ')}>
                                        {op.diff_ignore_paths.length} 项忽略
                                      </span>
                                    ) : (
                                      <span className="code-text-xs code-color-muted">-</span>
                                    )}
                                  </td>
                                  <td style={{ padding: '8px 12px' }}>
                                    <span className={`code-badge ${op.is_active ? 'code-badge--success' : 'code-badge--secondary'}`}>
                                      {op.is_active ? '生效中' : '未激活'}
                                    </span>
                                  </td>
                                  <td style={{ padding: '8px 12px', textAlign: 'right' }}>
                                    <button
                                      type="button"
                                      className="code-btn code-btn--secondary code-btn--xs"
                                      onClick={() => onSelectOperation(op)}
                                      data-testid={`inspect-op-${op.operation}`}
                                    >
                                      查看契约与字段树
                                    </button>
                                  </td>
                                </tr>
                              ))}
                            </tbody>
                          </table>
                        </div>
                      </td>
                    </tr>
                  )}
                </React.Fragment>
              );
            })}

            {loading ? (
              <tr>
                <td colSpan={6} className="code-table-empty" style={{ padding: '36px', textAlign: 'center', color: 'var(--color-text-secondary)' }}>
                  正在加载全局服务契约目录...
                </td>
              </tr>
            ) : filteredGroups.length === 0 ? (
              <tr>
                <td colSpan={6} className="code-table-empty" style={{ padding: '36px', textAlign: 'center', color: 'var(--color-text-secondary)' }}>
                  {searchKeyword ? `未检索到匹配 "${searchKeyword}" 的服务契约或方法` : '未检索到契约服务'}
                </td>
              </tr>
            ) : null}
          </tbody>
        </table>

        {totalServices !== undefined && totalServices > 0 && page !== undefined && pageSize !== undefined && onPageChange && onPageSizeChange && (
          <div style={{ padding: '12px 16px', borderTop: '1px solid var(--color-border-primary)', display: 'flex', justifyContent: 'flex-end' }}>
            <Pagination
              page={page}
              pageSize={pageSize}
              totalItems={totalServices}
              onPageChange={onPageChange}
              onPageSizeChange={onPageSizeChange}
              pageSizeOptions={[15, 25, 50, 100]}
            />
          </div>
        )}
      </div>
    </div>
  );
};
