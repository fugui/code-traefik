import React from 'react';
import type { ServiceContractGroupDTO } from '../../types/schema_tree';

export interface ServiceSummaryRowProps {
  group: ServiceContractGroupDTO;
  isExpanded: boolean;
  onToggleExpand: () => void;
  searchKeyword?: string;
  renderHighlight: (text: string) => React.ReactNode;
}

export const ServiceSummaryRow: React.FC<ServiceSummaryRowProps> = ({
  group,
  isExpanded,
  onToggleExpand,
  renderHighlight,
}) => {
  return (
    <tr
      className={`code-table-row-clickable code-service-summary-row ${
        isExpanded ? 'code-service-summary-row--expanded' : ''
      }`}
      onClick={onToggleExpand}
      data-testid={`service-row-${group.serviceName}`}
    >
      <td className="code-service-cell">
        <button
          type="button"
          className="code-tree-toggle-btn"
          aria-label={isExpanded ? '折叠服务' : '展开服务'}
          onClick={(e) => {
            e.stopPropagation();
            onToggleExpand();
          }}
        >
          <span className="code-tree-icon">{isExpanded ? '▼' : '▶'}</span>
        </button>
        <span className="code-font-mono code-color-primary code-font-bold">
          {renderHighlight(group.serviceName)}
        </span>
        <span className="code-text-xs code-color-muted code-ml-xs">
          ({group.protoFile})
        </span>
      </td>

      <td className="code-font-mono code-text-xs">
        {group.protoPackage || '(默认包)'}
      </td>

      <td>
        {group.claimedByModule ? (
          <span className="code-badge code-badge--info" title={group.claimedByModule.subsystemName}>
            {group.claimedByModule.code || group.claimedByModule.name}
          </span>
        ) : (
          <span className="code-badge code-badge--warning">未认领孤儿契约</span>
        )}
      </td>

      <td>
        <span className="code-badge code-badge--secondary">
          {group.totalOperations} 方法 (Unary {group.unaryCount} / Stream {group.streamCount})
        </span>
      </td>

      <td>
        <span className={`code-badge ${group.isActive ? 'code-badge--success' : 'code-badge--secondary'}`}>
          {group.isActive ? '生效中' : '未激活'}
        </span>
      </td>

      <td onClick={(e) => e.stopPropagation()}>
        <button
          type="button"
          className="code-btn code-btn--secondary code-btn--xs"
          onClick={onToggleExpand}
        >
          {isExpanded ? '收起方法' : '展开方法'}
        </button>
      </td>
    </tr>
  );
};

