import React, { useState } from 'react';
import { Pagination } from '@code/common';
import type { ProtoCategory, ProtoSchemaLedgerItemDTO, ProtoSourceType } from '../../types';

const SOURCE_TYPE_META: Record<ProtoSourceType, { label: string; tone: string }> = {
  UPLOAD_PB: { label: '📦 手动上传 .pb', tone: 'var(--color-text-secondary)' },
  ONLINE_COMPILE: { label: '⚡ 在线源码编译', tone: 'var(--color-primary)' },
  GIT_SYNC: { label: '🔗 Git 同步', tone: 'var(--color-success)' },
  GIT_MR_AUTO: { label: '🤖 MR 自动发布', tone: 'var(--color-warning)' },
};

export interface SchemaAssetLedgerTableProps {
  schemas: ProtoSchemaLedgerItemDTO[];
  total: number;
  page: number;
  pageSize: number;
  onPageChange: (newPage: number) => void;
  onPageSizeChange: (newPageSize: number) => void;
  selectedCategory: string;
  onCategoryChange: (cat: string) => void;
  activeStatusFilter: string;
  onStatusFilterChange: (status: string) => void;
  sourceTypeFilter?: string;
  onSourceTypeFilterChange?: (sourceType: string) => void;
  highlightedSchemaId?: string | number | null;
  searchKeyword: string;
  onSearchChange: (keyword: string) => void;
  loading: boolean;
  isAdmin: boolean;
  onInspect: (schema: ProtoSchemaLedgerItemDTO) => void;
  onUploadNewVersion: (schema: ProtoSchemaLedgerItemDTO) => void;
  onActivate: (schema: ProtoSchemaLedgerItemDTO) => void;
  onDeactivate: (schema: ProtoSchemaLedgerItemDTO) => void;
  onBatchActivate: (selectedIds: (string | number)[]) => void;
  onDelete: (schema: ProtoSchemaLedgerItemDTO) => void;
  onDownload: (schema: ProtoSchemaLedgerItemDTO) => void;
}

export const SchemaAssetLedgerTable: React.FC<SchemaAssetLedgerTableProps> = ({
  schemas,
  total,
  page,
  pageSize,
  onPageChange,
  onPageSizeChange,
  selectedCategory,
  onCategoryChange,
  activeStatusFilter,
  onStatusFilterChange,
  sourceTypeFilter = '',
  onSourceTypeFilterChange,
  highlightedSchemaId = null,
  searchKeyword,
  onSearchChange,
  loading,
  isAdmin,
  onInspect,
  onUploadNewVersion,
  onActivate,
  onDeactivate,
  onBatchActivate,
  onDelete,
  onDownload,
}) => {
  const [selectedIds, setSelectedIds] = useState<(string | number)[]>([]);

  const handleSelectAll = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.checked) {
      setSelectedIds(schemas.map((s) => s.id));
    } else {
      setSelectedIds([]);
    }
  };

  const handleToggleSelect = (id: string | number) => {
    setSelectedIds((prev) =>
      prev.includes(id) ? prev.filter((item) => item !== id) : [...prev, id]
    );
  };

  const isAllSelected = schemas.length > 0 && selectedIds.length === schemas.length;

  const renderSourceTypeBadge = (sourceType?: ProtoSourceType) => {
    const key: ProtoSourceType = sourceType && sourceType in SOURCE_TYPE_META ? sourceType : 'UPLOAD_PB';
    const meta = SOURCE_TYPE_META[key];
    return (
      <span
        className="status-badge"
        style={{
          fontSize: '10px',
          padding: '1px 6px',
          borderRadius: '4px',
          background: 'var(--color-bg-muted)',
          border: '1px solid var(--color-border-primary)',
          color: meta.tone,
          fontWeight: 500,
        }}
      >
        {meta.label}
      </span>
    );
  };

  const renderCategoryBadge = (cat: ProtoCategory) => {
    switch (cat) {
      case 'BASE_LIB':
        return (
          <span
            className="status-badge"
            style={{
              background: 'rgba(99, 102, 241, 0.1)',
              color: 'var(--color-primary, #6366f1)',
              border: '1px solid rgba(99, 102, 241, 0.2)',
              padding: '2px 6px',
              borderRadius: '4px',
              fontSize: '11px',
              fontWeight: 600,
            }}
          >
            🧱 基础库
          </span>
        );
      case 'GATEWAY':
        return (
          <span
            className="status-badge"
            style={{
              background: 'rgba(168, 85, 247, 0.1)',
              color: '#a855f7',
              border: '1px solid rgba(168, 85, 247, 0.2)',
              padding: '2px 6px',
              borderRadius: '4px',
              fontSize: '11px',
              fontWeight: 600,
            }}
          >
            🌐 网关
          </span>
        );
      default:
        return (
          <span
            className="status-badge"
            style={{
              background: 'rgba(59, 130, 246, 0.1)',
              color: 'var(--color-primary, #3b82f6)',
              border: '1px solid rgba(59, 130, 246, 0.2)',
              padding: '2px 6px',
              borderRadius: '4px',
              fontSize: '11px',
              fontWeight: 600,
            }}
          >
            📦 业务域
          </span>
        );
    }
  };

  const renderDependencyBadge = (schema: ProtoSchemaLedgerItemDTO) => {
    const deps = schema.resolved_dependencies || [];
    if (deps.length === 0) {
      return (
        <span
          style={{
            fontSize: '11px',
            color: 'var(--color-text-secondary)',
            background: 'var(--color-bg-muted)',
            padding: '2px 6px',
            borderRadius: '4px',
          }}
        >
          独立无外部依赖
        </span>
      );
    }

    const missing = deps.filter((d) => d.status === 'MISSING');
    if (missing.length > 0) {
      return (
        <span
          className="status-badge"
          style={{
            background: 'rgba(239, 68, 68, 0.1)',
            color: 'var(--color-danger, #ef4444)',
            border: '1px solid rgba(239, 68, 68, 0.2)',
            padding: '2px 6px',
            borderRadius: '4px',
            fontSize: '11px',
            fontWeight: 600,
          }}
          title={missing.map((m) => m.schema_name).join(', ')}
        >
          {`⚠️ 缺 ${missing.length} 项依赖`}
        </span>
      );
    }

    return (
      <span
        className="status-badge"
        style={{
          background: 'rgba(16, 185, 129, 0.1)',
          color: 'var(--color-success, #10b981)',
          border: '1px solid rgba(16, 185, 129, 0.2)',
          padding: '2px 6px',
          borderRadius: '4px',
          fontSize: '11px',
          fontWeight: 600,
        }}
      >
        {`✅ ${deps.length} 项就绪`}
      </span>
    );
  };

  return (
    <div className="code-schema-asset-ledger">
      {/* 顶部工具栏: 分类筛选、搜索、状态筛选、批量激活 */}
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          flexWrap: 'wrap',
          gap: '12px',
          marginBottom: '16px',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' }}>
          {/* 分类切换分段按钮 */}
          <div
            style={{
              display: 'inline-flex',
              background: 'var(--color-bg-muted)',
              padding: '3px',
              borderRadius: '6px',
              border: '1px solid var(--color-border-primary)',
            }}
          >
            {[
              { label: '全部', value: '' },
              { label: '🧱 基础库', value: 'BASE_LIB' },
              { label: '📦 业务域', value: 'BUSINESS' },
              { label: '🌐 网关', value: 'GATEWAY' },
            ].map((tab) => (
              <button
                key={tab.value}
                type="button"
                onClick={() => onCategoryChange(tab.value)}
                style={{
                  padding: '4px 12px',
                  borderRadius: '4px',
                  fontSize: '12px',
                  border: 'none',
                  cursor: 'pointer',
                  background: selectedCategory === tab.value ? 'var(--color-bg-surface)' : 'transparent',
                  color:
                    selectedCategory === tab.value
                      ? 'var(--color-primary, #3b82f6)'
                      : 'var(--color-text-secondary)',
                  fontWeight: selectedCategory === tab.value ? 600 : 400,
                  boxShadow:
                    selectedCategory === tab.value ? '0 1px 3px rgba(0,0,0,0.1)' : 'none',
                  transition: 'all 0.2s ease',
                }}
              >
                {tab.label}
              </button>
            ))}
          </div>

          {/* 状态筛选下拉 */}
          <select
            value={activeStatusFilter}
            onChange={(e) => onStatusFilterChange(e.target.value)}
            style={{
              padding: '6px 10px',
              borderRadius: '6px',
              border: '1px solid var(--color-border-primary)',
              background: 'var(--color-bg-surface)',
              color: 'var(--color-text-primary)',
              fontSize: '12px',
            }}
          >
            <option value="ALL">全部状态</option>
            <option value="ACTIVE">🟢 已生效</option>
            <option value="INACTIVE">⚪ 未生效 (草稿/历史)</option>
          </select>

          {onSourceTypeFilterChange && (
            <select
              aria-label="按来源筛选"
              value={sourceTypeFilter}
              onChange={(e) => onSourceTypeFilterChange(e.target.value)}
              style={{
                padding: '6px 10px',
                borderRadius: '6px',
                border: '1px solid var(--color-border-primary)',
                background: 'var(--color-bg-surface)',
                color: 'var(--color-text-primary)',
                fontSize: '12px',
              }}
            >
              <option value="">全部来源</option>
              <option value="UPLOAD_PB">📦 手动上传 .pb</option>
              <option value="ONLINE_COMPILE">⚡ 在线源码编译</option>
              <option value="GIT_SYNC">🔗 Git 同步</option>
              <option value="GIT_MR_AUTO">🤖 MR 自动发布</option>
            </select>
          )}

          {/* 关键字搜索 */}
          <input
            type="text"
            placeholder="搜索描述集名称 / 文件名 / 备注..."
            value={searchKeyword}
            onChange={(e) => onSearchChange(e.target.value)}
            style={{
              padding: '6px 12px',
              borderRadius: '6px',
              border: '1px solid var(--color-border-primary)',
              background: 'var(--color-bg-surface)',
              color: 'var(--color-text-primary)',
              fontSize: '12px',
              minWidth: '220px',
            }}
          />
        </div>

        {/* 批量操作 */}
        {isAdmin && selectedIds.length > 0 && (
          <button
            type="button"
            className="btn btn-primary"
            onClick={() => onBatchActivate(selectedIds)}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: '6px',
              padding: '6px 14px',
              fontSize: '12px',
            }}
          >
            <span>⚡</span>
            <span>批量激活选中项 ({selectedIds.length})</span>
          </button>
        )}
      </div>

      {/* 描述集台账表格 */}
      <div
        className="glass-card"
        style={{
          border: '1px solid var(--color-border-primary)',
          borderRadius: '8px',
          overflow: 'hidden',
          background: 'var(--color-bg-surface)',
        }}
      >
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '13px' }}>
            <thead>
              <tr
                style={{
                  background: 'var(--color-bg-muted)',
                  borderBottom: '1px solid var(--color-border-primary)',
                  textAlign: 'left',
                }}
              >
                {isAdmin && (
                  <th style={{ width: '36px', padding: '10px 12px' }}>
                    <input
                      type="checkbox"
                      checked={isAllSelected}
                      onChange={handleSelectAll}
                    />
                  </th>
                )}
                <th style={{ padding: '10px 12px' }}>描述集名称与版本</th>
                <th style={{ padding: '10px 12px' }}>类型</th>
                <th style={{ padding: '10px 12px' }}>生效态</th>
                <th style={{ padding: '10px 12px' }}>二进制产物</th>
                <th style={{ padding: '10px 12px' }}>源文件 / 契约规模</th>
                <th style={{ padding: '10px 12px' }}>跨包依赖就绪度</th>
                <th style={{ padding: '10px 12px' }}>架构认领覆盖</th>
                <th style={{ padding: '10px 12px' }}>更新信息</th>
                <th style={{ padding: '10px 12px', textAlign: 'right' }}>操作</th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <tr>
                  <td
                    colSpan={isAdmin ? 10 : 9}
                    style={{ padding: '36px', textAlign: 'center', color: 'var(--color-text-secondary)' }}
                  >
                    加载描述集台账中...
                  </td>
                </tr>
              ) : schemas.length === 0 ? (
                <tr>
                  <td
                    colSpan={isAdmin ? 10 : 9}
                    style={{ padding: '48px', textAlign: 'center', color: 'var(--color-text-secondary)' }}
                  >
                    暂无匹配的 Protobuf 描述集文件资产
                  </td>
                </tr>
              ) : (
                schemas.map((s, idx) => {
                  const isChecked = selectedIds.includes(s.id);
                  const isHighlighted = highlightedSchemaId !== null && highlightedSchemaId !== undefined && String(s.id) === String(highlightedSchemaId);
                  const isLast = idx === schemas.length - 1;
                  const claimedPct = Math.round((s.claimed_ratio || 0) * 100);

                  return (
                    <tr
                      key={s.id}
                      data-schema-id={String(s.id)}
                      style={{
                        borderBottom: isLast ? 'none' : '1px solid var(--color-border-primary)',
                        background: isHighlighted
                          ? 'var(--color-bg-hover)'
                          : isChecked
                            ? 'var(--color-bg-muted)'
                            : 'transparent',
                        boxShadow: isHighlighted ? 'inset 3px 0 0 var(--color-primary)' : undefined,
                        transition: 'background 0.15s ease',
                      }}
                    >
                      {isAdmin && (
                        <td style={{ padding: '10px 12px' }}>
                          <input
                            type="checkbox"
                            checked={isChecked}
                            onChange={() => handleToggleSelect(s.id)}
                          />
                        </td>
                      )}

                      <td style={{ padding: '10px 12px' }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' }}>
                          <span
                            style={{
                              fontWeight: 600,
                              color: 'var(--color-text-primary)',
                              cursor: 'pointer',
                            }}
                            onClick={() => onInspect(s)}
                          >
                            {s.name}
                          </span>
                          <span
                            style={{
                              fontSize: '11px',
                              padding: '1px 6px',
                              background: 'var(--color-bg-muted)',
                              borderRadius: '4px',
                              fontWeight: 600,
                            }}
                          >
                            v{s.version}
                          </span>
                          {renderSourceTypeBadge(s.source_type)}
                          {s.source_commit_sha && (
                            <code
                              className="pgit-commit-pill"
                              title={`Git Commit SHA: ${s.source_commit_sha}`}
                            >
                              {s.source_commit_sha.slice(0, 7)}
                            </code>
                          )}
                          {s.version_count > 1 && (
                            <span
                              style={{
                                fontSize: '11px',
                                color: 'var(--color-text-secondary)',
                              }}
                            >
                              {`(共 ${s.version_count} 版)`}
                            </span>
                          )}
                        </div>
                        {s.version_tag && (
                          <div style={{ fontSize: '11px', color: 'var(--color-text-secondary)', marginTop: '2px' }}>
                            标签: {s.version_tag}
                          </div>
                        )}
                      </td>

                      <td style={{ padding: '10px 12px' }}>{renderCategoryBadge(s.category)}</td>

                      <td style={{ padding: '10px 12px' }}>
                        <span
                          className="status-badge"
                          style={{
                            fontSize: '11px',
                            padding: '2px 8px',
                            borderRadius: '4px',
                            background: s.is_active ? 'rgba(16, 185, 129, 0.1)' : 'var(--color-bg-muted)',
                            color: s.is_active ? 'var(--color-success, #10b981)' : 'var(--color-text-secondary)',
                            fontWeight: s.is_active ? 600 : 'normal',
                          }}
                        >
                          {s.is_active ? '🟢 已生效' : '⚪ 未生效'}
                        </span>
                      </td>

                      <td style={{ padding: '10px 12px' }}>
                        <div style={{ fontFamily: 'monospace', fontSize: '12px' }}>{s.file_name}</div>
                        <div style={{ fontSize: '11px', color: 'var(--color-text-secondary)' }}>
                          {(s.file_size / 1024).toFixed(1)} KB
                        </div>
                      </td>

                      <td style={{ padding: '10px 12px' }}>
                        <div style={{ fontSize: '12px' }}>
                          <strong>{s.proto_file_count || (s.proto_files?.length ?? 0)}</strong> 文件 /{' '}
                          <strong>{s.service_count}</strong> 服务 /{' '}
                          <strong>{s.operation_count}</strong> 方法
                        </div>
                      </td>

                      <td style={{ padding: '10px 12px' }}>{renderDependencyBadge(s)}</td>

                      <td style={{ padding: '10px 12px' }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                          <span style={{ fontSize: '12px', fontWeight: 600 }}>{claimedPct}%</span>
                          <span style={{ fontSize: '11px', color: 'var(--color-text-secondary)' }}>
                            ({s.claimed_service_count}/{s.service_count})
                          </span>
                        </div>
                        <div
                          style={{
                            width: '70px',
                            height: '4px',
                            background: 'var(--color-bg-muted)',
                            borderRadius: '2px',
                            overflow: 'hidden',
                            marginTop: '4px',
                          }}
                        >
                          <div
                            style={{
                              width: `${claimedPct}%`,
                              height: '100%',
                              background:
                                claimedPct === 100
                                  ? 'var(--color-success, #10b981)'
                                  : 'var(--color-primary, #3b82f6)',
                              borderRadius: '2px',
                            }}
                          />
                        </div>
                      </td>

                      <td style={{ padding: '10px 12px', fontSize: '12px', color: 'var(--color-text-secondary)' }}>
                        <div>{s.updated_at ? new Date(s.updated_at).toLocaleDateString() : '-'}</div>
                        {s.created_by_name && <div>{s.created_by_name}</div>}
                      </td>

                      <td style={{ padding: '10px 12px', textAlign: 'right' }}>
                        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '6px' }}>
                          <button
                            type="button"
                            className="btn btn-secondary"
                            onClick={() => onInspect(s)}
                            style={{ fontSize: '11px', padding: '3px 8px' }}
                            title="透视源文件、跨包依赖与版本历史"
                          >
                            🔍 透视
                          </button>

                          {isAdmin && (
                            <button
                              type="button"
                              className="btn btn-secondary"
                              onClick={() => onUploadNewVersion(s)}
                              style={{ fontSize: '11px', padding: '3px 8px' }}
                              title="基于同名上传生成新版本"
                            >
                              ⬆️ 新版
                            </button>
                          )}

                          {isAdmin && !s.is_active && (
                            <button
                              type="button"
                              className="btn btn-secondary"
                              onClick={() => onActivate(s)}
                              style={{ fontSize: '11px', padding: '3px 8px', color: 'var(--color-success, #10b981)' }}
                            >
                              激活
                            </button>
                          )}

                          {isAdmin && s.is_active && (
                            <button
                              type="button"
                              className="btn btn-secondary"
                              onClick={() => onDeactivate(s)}
                              style={{ fontSize: '11px', padding: '3px 8px', color: 'var(--color-warning, #f59e0b)' }}
                            >
                              停用
                            </button>
                          )}

                          <button
                            type="button"
                            className="btn btn-secondary"
                            onClick={() => onDownload(s)}
                            style={{ fontSize: '11px', padding: '3px 8px' }}
                          >
                            ⬇️
                          </button>

                          {isAdmin && !s.is_active && (
                            <button
                              type="button"
                              className="btn btn-secondary"
                              onClick={() => onDelete(s)}
                              style={{ fontSize: '11px', padding: '3px 8px', color: 'var(--color-danger, #ef4444)' }}
                            >
                              🗑️
                            </button>
                          )}
                        </div>
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>

        {total > 0 && (
          <div
            style={{
              padding: '12px 16px',
              borderTop: '1px solid var(--color-border-primary)',
              display: 'flex',
              justifyContent: 'flex-end',
            }}
          >
            <Pagination
              page={page}
              pageSize={pageSize}
              totalItems={total}
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
