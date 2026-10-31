import React, { useState } from 'react';
import type { ProtoSchema, ProtoSchemaSummaryDTO } from '../../types';
import * as schemaApi from '../../api/schema';
import { useToast } from '../../utils/toast';

export interface SchemaVersionListProps {
  schemas: ProtoSchema[];
  selectedSchemaId: string;
  selectedSummary?: ProtoSchemaSummaryDTO | null;
  onSelectSchema: (id: string) => void;
  isAdmin: boolean;
  onActivate: (id: string) => void;
  onDelete: (id: string) => void;
}

export const SchemaVersionList: React.FC<SchemaVersionListProps> = ({
  schemas,
  selectedSchemaId,
  selectedSummary,
  onSelectSchema,
  isAdmin,
  onActivate,
  onDelete,
}) => {
  const toast = useToast();
  const [downloadingId, setDownloadingId] = useState<string | null>(null);

  const handleDownload = async (schema: ProtoSchema, e: React.MouseEvent) => {
    e.stopPropagation();
    try {
      setDownloadingId(schema.id);
      await schemaApi.downloadProtoSchema(schema.id, schema.file_name);
      toast.success(`已开始下载: ${schema.file_name || `${schema.name}.pb`}`);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '下载 .pb 二进制文件失败');
    } finally {
      setDownloadingId(null);
    }
  };

  return (
    <div className="code-schema-version-list">
      {(schemas ?? []).map((s) => {
        const isSelected = selectedSchemaId === s.id;
        return (
          <div
            key={s.id}
            className={`code-schema-card ${isSelected ? 'code-schema-card--selected' : ''}`}
            onClick={() => onSelectSchema(s.id)}
            data-testid={`schema-card-${s.id}`}
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
                <span className="code-meta-val">{s.operation_count ?? s.operations_count ?? 0} 个</span>
              </div>
            </div>

            {/* 包含的原始 .proto 文件列表 (按字母排序标签呈现) */}
            {s.proto_files && s.proto_files.length > 0 && (
              <div className="code-schema-proto-files">
                <span className="code-meta-label">{`IDL 源文件 (${s.proto_files.length}):`}</span>
                <div className="code-proto-tags">
                  {[...s.proto_files].sort().map((f) => (
                    <span key={f} className="code-proto-tag" title={f}>
                      {f}
                    </span>
                  ))}
                </div>
              </div>
            )}

            {/* 架构认领覆盖度提示与血缘诊断 (针对选中项呈现) */}
            {isSelected && selectedSummary && String(selectedSummary.schema?.id) === String(s.id) && (
              <div className="code-schema-bloodline-summary" data-testid="schema-bloodline-summary">
                <div className="code-bloodline-row">
                  <span className="code-meta-label">架构认领覆盖:</span>
                  <span
                    className={`code-badge ${
                      selectedSummary.claimed_ratio >= 1
                        ? 'code-badge--success'
                        : selectedSummary.claimed_ratio > 0
                        ? 'code-badge--info'
                        : 'code-badge--secondary'
                    }`}
                  >
                    {`${Math.round((selectedSummary.claimed_ratio ?? 0) * 100)}% 已认领`}
                  </span>
                </div>

                {(() => {
                  const orphanServices = (selectedSummary.services_summary || [])
                    .filter((item) => !item.is_claimed)
                    .map((item) => item.service_name);

                  if (orphanServices.length > 0) {
                    return (
                      <div
                        className="code-orphan-warning"
                        title={`未认领服务清单:\n${orphanServices.join('\n')}`}
                      >
                        <span className="code-badge code-badge--warning">
                          {`⚠️ ${orphanServices.length} 个未认领服务`}
                        </span>
                        <span className="code-text-xs code-color-muted code-ml-xs">
                          (建议前往拓扑图进行服务认领)
                        </span>
                      </div>
                    );
                  }
                  return (
                    <div className="code-bloodline-all-claimed">
                      <span className="code-badge code-badge--success">契约已全部血缘认领</span>
                    </div>
                  );
                })()}
              </div>
            )}

            <div className="code-schema-card__actions" onClick={(e) => e.stopPropagation()}>
              <button
                type="button"
                className="code-btn code-btn--secondary code-btn--xs"
                onClick={(e) => void handleDownload(s, e)}
                disabled={downloadingId === s.id}
                title="导出并下载原始 .pb 二进制描述集文件"
              >
                {downloadingId === s.id ? '下载中...' : '📥 导出 .pb'}
              </button>

              {isAdmin && !s.is_active && (
                <button
                  type="button"
                  className="code-btn code-btn--primary code-btn--xs"
                  onClick={() => onActivate(s.id)}
                >
                  激活生效
                </button>
              )}
              {isAdmin && !s.is_active && (
                <button
                  type="button"
                  className="code-btn code-btn--danger code-btn--xs"
                  onClick={() => onDelete(s.id)}
                >
                  删除
                </button>
              )}
            </div>
          </div>
        );
      })}
      {schemas.length === 0 && (
        <div className="code-empty-pane code-p-xl">尚未上传任何 .pb 描述集</div>
      )}
    </div>
  );
};
