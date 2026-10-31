import React, { useMemo, useState, useEffect } from 'react';
import type { ProtoOperation } from '../../types';
import type { ServiceContractGroupDTO } from '../../types/schema_tree';
import { ServiceSummaryRow } from './ServiceSummaryRow';

export interface OperationMappingTableProps {
  operations: ProtoOperation[];
  onSelectOperation: (op: ProtoOperation) => void;
  searchKeyword?: string;
  claimMap?: Record<string, { id: string; code: string; name: string; subsystemName?: string }>;
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

    group.totalOperations++;
    if (op.client_streaming || op.server_streaming) {
      group.streamCount++;
    } else {
      group.unaryCount++;
    }
    if (op.is_active) {
      group.isActive = true;
    }
    group.operations.push(op);
  }

  return Array.from(map.values()).sort((a, b) => a.serviceName.localeCompare(b.serviceName));
}

export const OperationMappingTable: React.FC<OperationMappingTableProps> = ({
  operations,
  onSelectOperation,
  searchKeyword = '',
  claimMap,
}) => {
  const [userExpandedKeys, setUserExpandedKeys] = useState<Set<string> | null>(null);

  // 1. 聚合服务分组
  const allGroups = useMemo(() => rollupServices(operations, claimMap), [operations, claimMap]);

  // 2. 搜索穿透逻辑：计算包含匹配项的服务并自动展开
  const { filteredGroups, autoExpandedKeys } = useMemo(() => {
    const kw = searchKeyword.trim().toLowerCase();
    if (!kw) {
      return { filteredGroups: allGroups, autoExpandedKeys: new Set<string>() };
    }

    const autoKeys = new Set<string>();
    const matched: ServiceContractGroupDTO[] = [];

    for (const grp of allGroups) {
      const serviceMatch = grp.serviceName.toLowerCase().includes(kw) || grp.protoFile.toLowerCase().includes(kw);
      const matchedOps = grp.operations.filter(
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
        autoKeys.add(grp.serviceName);
      }
    }

    return { filteredGroups: matched, autoExpandedKeys: autoKeys };
  }, [allGroups, searchKeyword]);

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
    const allKeys = new Set(filteredGroups.map((g) => g.serviceName));
    setUserExpandedKeys(allKeys);
  };

  const handleCollapseAll = () => {
    setUserExpandedKeys(new Set());
  };

  const isAllExpanded = filteredGroups.length > 0 && filteredGroups.every((g) => expandedServices.has(g.serviceName));

  return (
    <div className="code-table-wrapper code-nested-table-wrapper">
      <div className="code-table-toolbar">
        <span className="code-table-summary-info">
          聚合展示 <strong>{filteredGroups.length}</strong> 个服务契约，共包含 <strong>{operations.length}</strong> 个 RPC 方法
        </span>
        <div className="code-table-toolbar-actions">
          {isAllExpanded ? (
            <button
              type="button"
              className="code-btn code-btn--secondary code-btn--xs"
              onClick={handleCollapseAll}
              data-testid="collapse-all-btn"
            >
              全部折叠
            </button>
          ) : (
            <button
              type="button"
              className="code-btn code-btn--secondary code-btn--xs"
              onClick={handleExpandAll}
              data-testid="expand-all-btn"
            >
              全部展开
            </button>
          )}
        </div>
      </div>

      <table className="code-table code-nested-table">
        <thead>
          <tr>
            <th>服务契约 (Service)</th>
            <th>所属 Proto 包</th>
            <th>架构认领状态</th>
            <th>RPC 统计分布</th>
            <th>生效态</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          {filteredGroups.map((group) => {
            const isExpanded = expandedServices.has(group.serviceName);
            return (
              <React.Fragment key={group.serviceName}>
                <ServiceSummaryRow
                  group={group}
                  isExpanded={isExpanded}
                  onToggleExpand={() => toggleExpand(group.serviceName)}
                  searchKeyword={searchKeyword}
                  renderHighlight={(txt) => highlightText(txt, searchKeyword)}
                />

                {isExpanded && (
                  <tr className="code-nested-detail-row">
                    <td colSpan={6} className="code-nested-detail-container">
                      <div className="code-nested-subtable-pane">
                        <table className="code-table code-table--compact code-subtable">
                          <thead>
                            <tr>
                              <th>调用方法 (operation)</th>
                              <th>请求消息体 (req_message)</th>
                              <th>应答消息体 (rsp_message)</th>
                              <th>流式模式</th>
                              <th>Diff 忽略字段</th>
                              <th>状态</th>
                              <th>操作</th>
                            </tr>
                          </thead>
                          <tbody>
                            {group.operations.map((op) => (
                              <tr key={op.id} className="code-subtable-row" data-testid={`op-row-${op.operation}`}>
                                <td className="code-font-mono code-font-bold">
                                  {highlightText(op.operation, searchKeyword)}
                                </td>
                                <td className="code-font-mono code-text-xs code-color-muted">
                                  {op.req_message_full_name || '-'}
                                </td>
                                <td className="code-font-mono code-text-xs code-color-muted">
                                  {op.rsp_message_full_name || '-'}
                                </td>
                                <td>
                                  {op.client_streaming || op.server_streaming ? (
                                    <span className="code-badge code-badge--info">
                                      {op.client_streaming ? 'ClientStream ' : ''}
                                      {op.server_streaming ? 'ServerStream' : ''}
                                    </span>
                                  ) : (
                                    <span className="code-badge code-badge--secondary">Unary</span>
                                  )}
                                </td>
                                <td>
                                  {op.diff_ignore_paths && op.diff_ignore_paths.length > 0 ? (
                                    <span className="code-badge code-badge--warning" title={op.diff_ignore_paths.join(', ')}>
                                      {op.diff_ignore_paths.length} 项忽略
                                    </span>
                                  ) : (
                                    <span className="code-text-xs code-color-muted">-</span>
                                  )}
                                </td>
                                <td>
                                  <span className={`code-badge ${op.is_active ? 'code-badge--success' : 'code-badge--secondary'}`}>
                                    {op.is_active ? '生效中' : '未激活'}
                                  </span>
                                </td>
                                <td>
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

          {filteredGroups.length === 0 && (
            <tr>
              <td colSpan={6} className="code-table-empty">
                {searchKeyword ? `未检索到匹配 "${searchKeyword}" 的服务契约或方法` : '当前描述集下未检索到契约映射'}
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
};
