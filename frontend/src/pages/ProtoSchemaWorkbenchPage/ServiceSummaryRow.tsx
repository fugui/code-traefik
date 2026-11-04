import React from 'react';
import { Link } from 'react-router-dom';
import { useModulePath } from '../../hooks/useModulePath';
import type { ServiceContractGroupDTO } from '../../types/schema_tree';

export interface ServiceSummaryRowProps {
  group: ServiceContractGroupDTO;
  isExpanded: boolean;
  onToggleExpand: () => void;
  searchKeyword?: string;
  renderHighlight: (text: string) => React.ReactNode;
  onInspectSchema?: (schemaId: string | number) => void;
}

export const ServiceSummaryRow: React.FC<ServiceSummaryRowProps> = ({
  group,
  isExpanded,
  onToggleExpand,
  renderHighlight,
  onInspectSchema,
}) => {
  const modulePath = useModulePath();
  const serviceName = group.serviceName || group.service_name || '';
  const protoFile = group.protoFile || group.proto_file || '';
  const protoPackage = group.protoPackage || group.proto_package || '';
  const totalOps = group.totalOperations ?? group.operation_count ?? group.operations?.length ?? 0;
  const unaryOps = group.unaryCount ?? group.unary_count ?? 0;
  const streamOps = group.streamCount ?? group.stream_count ?? 0;
  const active = group.isActive ?? group.is_active ?? false;
  const source = group.sourceSchema || group.source_schema;

  const claimed = group.claimedByModule
    ? {
        id: group.claimedByModule.id,
        code: group.claimedByModule.code || group.claimedByModule.name,
        name: group.claimedByModule.name,
        subsystemName: group.claimedByModule.subsystemName || group.claimedByModule.subsystem_name,
        subsystemId: group.claimedByModule.subsystemId ?? group.claimedByModule.subsystem_id,
        groupId: group.claimedByModule.groupId ?? group.claimedByModule.group_id,
      }
    : group.claimed_by_module
      ? {
          id: String(group.claimed_by_module.module_id),
          code: group.claimed_by_module.module_name,
          name: group.claimed_by_module.module_name,
          subsystemName: group.claimed_by_module.subsystem_name,
          subsystemId: group.claimed_by_module.subsystem_id,
          groupId: undefined,
        }
      : null;

  return (
    <tr
      className={`code-table-row-clickable code-service-summary-row ${
        isExpanded ? 'code-service-summary-row--expanded' : ''
      }`}
      onClick={onToggleExpand}
      data-testid={`service-row-${serviceName}`}
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
          {renderHighlight(serviceName)}
        </span>
        <span className="code-text-xs code-color-muted code-ml-xs">
          ({protoFile})
        </span>

        {source && (
          <span
            className="code-badge code-badge--info"
            style={{
              cursor: onInspectSchema ? 'pointer' : 'default',
              marginLeft: '8px',
              fontSize: '11px',
              padding: '1px 6px',
            }}
            onClick={(e) => {
              if (onInspectSchema && source.id) {
                e.stopPropagation();
                onInspectSchema(source.id);
              }
            }}
            title="点击透视来源描述集"
            data-testid={`badge-source-schema-${source.id}`}
          >
            📦 {source.name} v{source.version}
          </span>
        )}
      </td>

      <td className="code-font-mono code-text-xs">
        {protoPackage || '(默认包)'}
      </td>

      <td>
        {claimed ? (
          <Link
            to={modulePath(
              `/architecture?subsystem_id=${claimed.subsystemId ?? ''}&group_id=${claimed.groupId ?? ''}&module_id=${claimed.id}`
            )}
            className="code-badge-link"
            data-testid={`link-claimed-module-${claimed.id}`}
            title={`前往逻辑架构中心定位认领模块: ${claimed.name}`}
          >
            <span className="code-badge code-badge--info" title={claimed.subsystemName}>
              {claimed.code}
            </span>
          </Link>
        ) : (
          <span className="code-badge code-badge--warning">未认领孤儿契约</span>
        )}
      </td>

      <td>
        <span className="code-badge code-badge--secondary">
          {totalOps} 方法 (Unary {unaryOps} / Stream {streamOps})
        </span>
      </td>

      <td>
        <span className={`code-badge ${active ? 'code-badge--success' : 'code-badge--secondary'}`}>
          {active ? '生效中' : '未激活'}
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
