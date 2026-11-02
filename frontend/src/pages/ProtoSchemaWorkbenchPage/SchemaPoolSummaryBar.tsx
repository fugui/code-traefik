import React from 'react';
import type { ProtoPoolSummaryDTO } from '../../types';

export interface SchemaPoolSummaryBarProps {
  summary: ProtoPoolSummaryDTO | null;
  isAdmin: boolean;
  onUploadClick: () => void;
}

export const SchemaPoolSummaryBar: React.FC<SchemaPoolSummaryBarProps> = ({
  summary,
  isAdmin,
  onUploadClick,
}) => {
  const activeSchemas = summary?.active_schema_count ?? 0;
  const activeBaseLibs = summary?.active_base_lib_count ?? 0;
  const activeServices = summary?.active_service_count ?? 0;
  const activeOperations = summary?.active_operation_count ?? 0;
  const claimedRatio = summary ? Math.round(summary.claimed_ratio * 100) : 0;
  const unresolvedDeps = summary?.unresolved_dep_count ?? 0;
  const orphanServices = summary?.orphan_service_count ?? 0;

  return (
    <div
      className="code-schema-pool-summary glass-card"
      style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        padding: '12px 20px',
        marginBottom: '16px',
        background: 'var(--color-bg-surface)',
        border: '1px solid var(--color-border-primary)',
        borderRadius: '8px',
        flexWrap: 'wrap',
        gap: '12px',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: '24px', flexWrap: 'wrap' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
          <span style={{ fontSize: '18px' }}>🌐</span>
          <div>
            <div style={{ fontSize: '12px', color: 'var(--color-text-secondary)' }}>生效描述集池</div>
            <div style={{ fontSize: '16px', fontWeight: 600, color: 'var(--color-text-primary)' }}>
              {activeSchemas}{' '}
              <span style={{ fontSize: '12px', fontWeight: 'normal', color: 'var(--color-text-secondary)' }}>
                个 (.pb)
              </span>
            </div>
          </div>
        </div>

        <div style={{ width: '1px', height: '28px', background: 'var(--color-border-primary)' }} />

        <div>
          <div style={{ fontSize: '12px', color: 'var(--color-text-secondary)' }}>公共底座</div>
          <div style={{ fontSize: '16px', fontWeight: 600, color: 'var(--color-text-primary)' }}>
            {activeBaseLibs}{' '}
            <span style={{ fontSize: '12px', fontWeight: 'normal', color: 'var(--color-text-secondary)' }}>
              个基础库
            </span>
          </div>
        </div>

        <div style={{ width: '1px', height: '28px', background: 'var(--color-border-primary)' }} />

        <div>
          <div style={{ fontSize: '12px', color: 'var(--color-text-secondary)' }}>生效契约规模</div>
          <div style={{ fontSize: '16px', fontWeight: 600, color: 'var(--color-text-primary)' }}>
            {activeServices}{' '}
            <span style={{ fontSize: '12px', fontWeight: 'normal', color: 'var(--color-text-secondary)' }}>服务 /</span>{' '}
            {activeOperations}{' '}
            <span style={{ fontSize: '12px', fontWeight: 'normal', color: 'var(--color-text-secondary)' }}>方法</span>
          </div>
        </div>

        <div style={{ width: '1px', height: '28px', background: 'var(--color-border-primary)' }} />

        <div>
          <div style={{ fontSize: '12px', color: 'var(--color-text-secondary)' }}>架构认领覆盖率</div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            <span
              style={{
                fontSize: '16px',
                fontWeight: 600,
                color: claimedRatio === 100 ? 'var(--color-success, #10b981)' : 'var(--color-text-primary)',
              }}
            >
              {claimedRatio}%
            </span>
            <div
              style={{
                width: '60px',
                height: '6px',
                background: 'var(--color-bg-muted)',
                borderRadius: '3px',
                overflow: 'hidden',
              }}
            >
              <div
                style={{
                  width: `${claimedRatio}%`,
                  height: '100%',
                  background:
                    claimedRatio === 100 ? 'var(--color-success, #10b981)' : 'var(--color-primary, #3b82f6)',
                  borderRadius: '3px',
                  transition: 'width 0.3s ease',
                }}
              />
            </div>
          </div>
        </div>

        {unresolvedDeps > 0 && (
          <div
            className="status-badge"
            style={{
              background: 'rgba(239, 68, 68, 0.1)',
              color: 'var(--color-danger, #ef4444)',
              border: '1px solid rgba(239, 68, 68, 0.2)',
              padding: '4px 8px',
              borderRadius: '4px',
              fontSize: '12px',
              display: 'flex',
              alignItems: 'center',
              gap: '4px',
            }}
          >
            <span>⚠️</span>
            <span>{`${unresolvedDeps} 项缺失外部依赖`}</span>
          </div>
        )}

        {orphanServices > 0 && (
          <div
            className="status-badge"
            style={{
              background: 'rgba(245, 158, 11, 0.1)',
              color: 'var(--color-warning, #f59e0b)',
              border: '1px solid rgba(245, 158, 11, 0.2)',
              padding: '4px 8px',
              borderRadius: '4px',
              fontSize: '12px',
              display: 'flex',
              alignItems: 'center',
              gap: '4px',
            }}
          >
            <span>⚠️</span>
            <span>{`${orphanServices} 个未认领服务`}</span>
          </div>
        )}
      </div>

      {isAdmin && (
        <button
          type="button"
          className="btn btn-primary"
          onClick={onUploadClick}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '6px',
            padding: '6px 14px',
            fontSize: '13px',
          }}
        >
          <span>⬆️</span>
          <span>上传描述集 (.pb)</span>
        </button>
      )}
    </div>
  );
};
