import React from 'react';
import type { ProtoSchema } from '../../types';

export interface SchemaVersionListProps {
  schemas: ProtoSchema[];
  selectedSchemaId: string;
  onSelectSchema: (id: string) => void;
  isAdmin: boolean;
  onActivate: (id: string) => void;
  onDelete: (id: string) => void;
}

export const SchemaVersionList: React.FC<SchemaVersionListProps> = ({
  schemas,
  selectedSchemaId,
  onSelectSchema,
  isAdmin,
  onActivate,
  onDelete,
}) => {
  return (
    <div className="code-schema-version-list">
      {(schemas ?? []).map((s) => {
        const isSelected = selectedSchemaId === s.id;
        return (
          <div
            key={s.id}
            className={`code-schema-card ${isSelected ? 'code-schema-card--selected' : ''}`}
            onClick={() => onSelectSchema(s.id)}
          >
            <div className="code-schema-card__header">
              <div className="code-schema-card__title">
                {s.name}
                <span className="code-badge code-badge--info">v{s.version}</span>
              </div>
              <span className={`code-badge ${s.is_active ? 'code-badge--success' : 'code-badge--secondary'}`}>
                {s.is_active ? '当前生效中' : '历史/未生效'}
              </span>
            </div>

            <div className="code-schema-card__meta">
              {s.version_tag && (
                <div className="code-meta-item">
                  <span className="code-meta-label">Tag:</span>
                  <span className="code-meta-val code-font-mono">{s.version_tag}</span>
                </div>
              )}
              <div className="code-meta-item">
                <span className="code-meta-label">SHA256:</span>
                <span className="code-meta-val code-font-mono code-text-xs code-color-muted" title={s.file_sha256}>
                  {s.file_sha256 ? `${s.file_sha256.slice(0, 12)}...` : '-'}
                </span>
              </div>
              <div className="code-meta-item">
                <span className="code-meta-label">方法映射:</span>
                <span className="code-meta-val">{s.operations_count ?? 0} 个</span>
              </div>
            </div>

            {isAdmin && (
              <div className="code-schema-card__actions" onClick={(e) => e.stopPropagation()}>
                {!s.is_active && (
                  <button
                    type="button"
                    className="code-btn code-btn--primary code-btn--xs"
                    onClick={() => onActivate(s.id)}
                  >
                    激活生效
                  </button>
                )}
                {!s.is_active && (
                  <button
                    type="button"
                    className="code-btn code-btn--danger code-btn--xs"
                    onClick={() => onDelete(s.id)}
                  >
                    删除
                  </button>
                )}
              </div>
            )}
          </div>
        );
      })}
      {schemas.length === 0 && (
        <div className="code-empty-pane code-p-xl">尚未上传任何 .pb 描述集</div>
      )}
    </div>
  );
};
