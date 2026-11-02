import React, { useEffect, useState } from 'react';
import { Drawer } from '@code/common';
import * as schemaApi from '../../api/schema';
import type {
  ProtoSchemaLedgerItemDTO,
  ProtoSchemaSummaryDTO,
} from '../../types';

export interface SchemaAssetInspectContentProps {
  schema: ProtoSchemaLedgerItemDTO;
  isAdmin: boolean;
  onActivate: (schema: ProtoSchemaLedgerItemDTO) => Promise<void>;
  onDeactivate: (schema: ProtoSchemaLedgerItemDTO) => Promise<void>;
  onDelete: (schema: ProtoSchemaLedgerItemDTO) => Promise<void>;
  onUploadNewVersion: (schema: ProtoSchemaLedgerItemDTO) => void;
  onNavigateToCatalog: (schemaId: string | number) => void;
}

export const SchemaAssetInspectContent: React.FC<SchemaAssetInspectContentProps> = ({
  schema,
  isAdmin,
  onActivate,
  onDeactivate,
  onDelete,
  onUploadNewVersion,
  onNavigateToCatalog,
}) => {
  const [activeTab, setActiveTab] = useState<'files' | 'dependencies' | 'history'>('files');
  const [summary, setSummary] = useState<ProtoSchemaSummaryDTO | null>(null);
  const [historyVersions, setHistoryVersions] = useState<ProtoSchemaLedgerItemDTO[]>([]);
  const [loadingHistory, setLoadingHistory] = useState(false);

  useEffect(() => {
    if (schema) {
      setActiveTab('files');
      // 加载血缘与服务摘要
      void schemaApi
        .getProtoSchemaSummary(schema.id)
        .then((res) => setSummary(res))
        .catch(() => setSummary(null));

      // 加载历史版本栈
      setLoadingHistory(true);
      void schemaApi
        .listProtoSchemas({ name: schema.name, latest_only: false })
        .then((res) => setHistoryVersions(res.items ?? []))
        .catch(() => setHistoryVersions([]))
        .finally(() => setLoadingHistory(false));
    }
  }, [schema]);

  const categoryLabel =
    schema.category === 'BASE_LIB'
      ? '🧱 基础公共库'
      : schema.category === 'GATEWAY'
        ? '🌐 网关聚合层'
        : '📦 业务域服务';

  return (
    <div className="code-schema-asset-inspect-content" style={{ display: 'flex', flexDirection: 'column', height: '100%', color: 'var(--color-text-primary)' }}>
      {/* 顶部标题与类型说明 */}
      <div style={{ marginBottom: '12px' }}>
        <h3 style={{ margin: 0, fontSize: '18px', fontWeight: 600 }}>
          {`描述集资产透视: ${schema.name}`}
        </h3>
        <div style={{ fontSize: '12px', color: 'var(--color-text-secondary)', marginTop: '4px' }}>
          {`版本: v${schema.version} ${schema.version_tag ? `(${schema.version_tag})` : ''} · 类型: ${categoryLabel}`}
        </div>
      </div>

      {/* 顶部元信息胶囊栏 */}
      <div
        style={{
          padding: '12px 16px',
          background: 'var(--color-bg-muted)',
          borderRadius: '8px',
          marginBottom: '16px',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          fontSize: '13px',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
          <span
            className="status-badge"
            style={{
              background: schema.is_active ? 'rgba(16, 185, 129, 0.1)' : 'var(--color-bg-surface)',
              color: schema.is_active ? 'var(--color-success, #10b981)' : 'var(--color-text-secondary)',
              border: `1px solid ${schema.is_active ? 'rgba(16, 185, 129, 0.3)' : 'var(--color-border-primary)'}`,
              padding: '2px 8px',
              borderRadius: '4px',
              fontWeight: 600,
            }}
          >
            {schema.is_active ? '🟢 生效中' : '⚪ 未生效 (草稿/历史)'}
          </span>
          <span style={{ color: 'var(--color-text-secondary)' }}>
            文件: <strong>{schema.file_name}</strong> ({(schema.file_size / 1024).toFixed(1)} KB)
          </span>
        </div>

        <div style={{ display: 'flex', gap: '8px' }}>
          {isAdmin && (
            <button
              type="button"
              className="btn btn-secondary"
              onClick={() => onUploadNewVersion(schema)}
              style={{ padding: '4px 10px', fontSize: '12px' }}
            >
              ⬆️ 上传新版本
            </button>
          )}
          <button
            type="button"
            className="btn btn-secondary"
            onClick={() => void schemaApi.downloadProtoSchema(schema.id, schema.file_name)}
            style={{ padding: '4px 10px', fontSize: '12px' }}
          >
            ⬇️ 下载 .pb
          </button>
        </div>
      </div>

      {/* 子面板切换导航 Tabs */}
      <div
        style={{
          display: 'flex',
          borderBottom: '1px solid var(--color-border-primary)',
          marginBottom: '16px',
        }}
      >
        <button
          type="button"
          onClick={() => setActiveTab('files')}
          style={{
            padding: '8px 16px',
            fontSize: '13px',
            fontWeight: activeTab === 'files' ? 600 : 400,
            color: activeTab === 'files' ? 'var(--color-primary, #3b82f6)' : 'var(--color-text-secondary)',
            borderBottom: activeTab === 'files' ? '2px solid var(--color-primary, #3b82f6)' : '2px solid transparent',
            background: 'none',
            borderTop: 'none',
            borderLeft: 'none',
            borderRight: 'none',
            cursor: 'pointer',
          }}
        >
          {`包含源文件与服务 (${schema.proto_file_count || schema.proto_files?.length || 0})`}
        </button>
        <button
          type="button"
          onClick={() => setActiveTab('dependencies')}
          style={{
            padding: '8px 16px',
            fontSize: '13px',
            fontWeight: activeTab === 'dependencies' ? 600 : 400,
            color: activeTab === 'dependencies' ? 'var(--color-primary, #3b82f6)' : 'var(--color-text-secondary)',
            borderBottom: activeTab === 'dependencies' ? '2px solid var(--color-primary, #3b82f6)' : '2px solid transparent',
            background: 'none',
            borderTop: 'none',
            borderLeft: 'none',
            borderRight: 'none',
            cursor: 'pointer',
          }}
        >
          {`跨包依赖关系 (${schema.resolved_dependencies?.length || 0})`}
        </button>
        <button
          type="button"
          onClick={() => setActiveTab('history')}
          style={{
            padding: '8px 16px',
            fontSize: '13px',
            fontWeight: activeTab === 'history' ? 600 : 400,
            color: activeTab === 'history' ? 'var(--color-primary, #3b82f6)' : 'var(--color-text-secondary)',
            borderBottom: activeTab === 'history' ? '2px solid var(--color-primary, #3b82f6)' : '2px solid transparent',
            background: 'none',
            borderTop: 'none',
            borderLeft: 'none',
            borderRight: 'none',
            cursor: 'pointer',
          }}
        >
          {`历史版本与回滚 (${historyVersions.length || schema.version_count || 1})`}
        </button>
      </div>

      {/* Tab 1: 包含源文件与服务 */}
      {activeTab === 'files' && (
        <div style={{ flex: 1, overflowY: 'auto' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '12px' }}>
            <div style={{ fontSize: '13px', fontWeight: 600 }}>自有 IDL 源文件列表:</div>
            <button
              type="button"
              className="btn btn-secondary"
              onClick={() => onNavigateToCatalog(schema.id)}
              style={{ fontSize: '12px', padding: '3px 8px', color: 'var(--color-primary, #3b82f6)' }}
            >
              在全局服务目录中查看 ➔
            </button>
          </div>

          <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', marginBottom: '20px' }}>
            {(schema.proto_files || []).map((file, idx) => (
              <div
                key={`${file}-${idx}`}
                style={{
                  padding: '8px 12px',
                  borderRadius: '6px',
                  background: 'var(--color-bg-surface)',
                  border: '1px solid var(--color-border-primary)',
                  fontFamily: 'monospace',
                  fontSize: '13px',
                  display: 'flex',
                  alignItems: 'center',
                  gap: '8px',
                }}
              >
                <span style={{ color: 'var(--color-primary, #3b82f6)' }}>📄</span>
                <span>{file}</span>
              </div>
            ))}
          </div>

          {summary && summary.services_summary && summary.services_summary.length > 0 && (
            <div>
              <div style={{ fontSize: '13px', fontWeight: 600, marginBottom: '8px' }}>
                {`定义服务与架构认领血缘 (${summary.services_summary.length}):`}
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
                {summary.services_summary.map((svc) => (
                  <div
                    key={svc.service_name}
                    style={{
                      padding: '10px 12px',
                      borderRadius: '6px',
                      background: 'var(--color-bg-surface)',
                      border: '1px solid var(--color-border-primary)',
                    }}
                  >
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '4px' }}>
                      <span style={{ fontWeight: 600, fontSize: '13px', fontFamily: 'monospace' }}>
                        {svc.service_name}
                      </span>
                      <span
                        className="status-badge"
                        style={{
                          fontSize: '11px',
                          padding: '2px 6px',
                          borderRadius: '4px',
                          background: svc.is_claimed ? 'rgba(16, 185, 129, 0.1)' : 'rgba(245, 158, 11, 0.1)',
                          color: svc.is_claimed ? 'var(--color-success, #10b981)' : 'var(--color-warning, #f59e0b)',
                        }}
                      >
                        {svc.is_claimed ? `已认领: ${svc.claimed_module_name || '已归属模块'}` : '⚠️ 未认领孤儿服务'}
                      </span>
                    </div>
                    <div style={{ fontSize: '12px', color: 'var(--color-text-secondary)' }}>
                      {`源文件: ${svc.proto_file} · 方法数: ${svc.operation_count} (Unary: ${svc.unary_count}, Stream: ${svc.stream_count})`}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      {/* Tab 2: 跨包依赖关系 */}
      {activeTab === 'dependencies' && (
        <div style={{ flex: 1, overflowY: 'auto' }}>
          <div style={{ fontSize: '13px', fontWeight: 600, marginBottom: '12px' }}>外部依赖导入清单:</div>
          {(!schema.resolved_dependencies || schema.resolved_dependencies.length === 0) ? (
            <div
              style={{
                padding: '24px',
                textAlign: 'center',
                background: 'var(--color-bg-muted)',
                borderRadius: '8px',
                color: 'var(--color-text-secondary)',
                fontSize: '13px',
              }}
            >
              ✅ 本描述集为完全自治独立包，无需外部依赖或仅依赖 Google WKT 标准库。
            </div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '10px' }}>
              {schema.resolved_dependencies.map((dep, idx) => {
                const isReady = dep.status === 'READY';
                return (
                  <div
                    key={`${dep.schema_name}-${idx}`}
                    style={{
                      padding: '12px',
                      borderRadius: '8px',
                      background: 'var(--color-bg-surface)',
                      border: `1px solid ${isReady ? 'var(--color-border-primary)' : 'rgba(239, 68, 68, 0.3)'}`,
                    }}
                  >
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '6px' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                        <span style={{ fontWeight: 600, fontSize: '13px' }}>
                          {dep.schema_name === 'google.protobuf' ? '🌐 Google Protobuf WKT' : dep.schema_name}
                        </span>
                        {dep.version && (
                          <span
                            style={{
                              fontSize: '11px',
                              padding: '1px 6px',
                              background: 'var(--color-bg-muted)',
                              borderRadius: '4px',
                            }}
                          >
                            v{dep.version}
                          </span>
                        )}
                        {dep.category && (
                          <span
                            style={{
                              fontSize: '11px',
                              padding: '1px 6px',
                              background: 'var(--color-bg-muted)',
                              borderRadius: '4px',
                            }}
                          >
                            {dep.category}
                          </span>
                        )}
                      </div>

                      <span
                        className="status-badge"
                        style={{
                          fontSize: '11px',
                          padding: '2px 8px',
                          borderRadius: '4px',
                          background: isReady ? 'rgba(16, 185, 129, 0.1)' : 'rgba(239, 68, 68, 0.1)',
                          color: isReady ? 'var(--color-success, #10b981)' : 'var(--color-danger, #ef4444)',
                          fontWeight: 600,
                        }}
                      >
                        {isReady ? '✅ 依赖已就绪' : '⚠️ 缺失依赖'}
                      </span>
                    </div>

                    <div style={{ fontSize: '12px', color: 'var(--color-text-secondary)', marginBottom: '4px' }}>
                      引用文件:
                    </div>
                    <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px' }}>
                      {dep.matched_protos.map((protoPath) => (
                        <span
                          key={protoPath}
                          style={{
                            fontSize: '11px',
                            padding: '2px 6px',
                            background: 'var(--color-bg-muted)',
                            borderRadius: '4px',
                            fontFamily: 'monospace',
                          }}
                        >
                          {protoPath}
                        </span>
                      ))}
                    </div>

                    {!isReady && (
                      <div style={{ marginTop: '8px', fontSize: '12px', color: 'var(--color-danger, #ef4444)' }}>
                        提示: 请先上传并激活提供上述 proto 文件的基础公共库，否则本包无法激活。
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          )}
        </div>
      )}

      {/* Tab 3: 历史版本与回滚 */}
      {activeTab === 'history' && (
        <div style={{ flex: 1, overflowY: 'auto' }}>
          <div style={{ fontSize: '13px', fontWeight: 600, marginBottom: '12px' }}>
            {`版本演进栈 (共 ${historyVersions.length} 个版本):`}
          </div>

          {loadingHistory ? (
            <div style={{ padding: '24px', textAlign: 'center', color: 'var(--color-text-secondary)' }}>
              加载版本历史中...
            </div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '10px' }}>
              {historyVersions.map((item) => (
                <div
                  key={item.id}
                  style={{
                    padding: '12px',
                    borderRadius: '8px',
                    background: item.is_active ? 'rgba(16, 185, 129, 0.04)' : 'var(--color-bg-surface)',
                    border: `1px solid ${item.is_active ? 'var(--color-success, #10b981)' : 'var(--color-border-primary)'}`,
                  }}
                >
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '6px' }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                      <span style={{ fontWeight: 600, fontSize: '14px' }}>v{item.version}</span>
                      {item.version_tag && (
                        <span
                          style={{
                            fontSize: '11px',
                            padding: '1px 6px',
                            background: 'var(--color-bg-muted)',
                            borderRadius: '4px',
                          }}
                        >
                          {item.version_tag}
                        </span>
                      )}
                      <span
                        className="status-badge"
                        style={{
                          fontSize: '11px',
                          padding: '1px 6px',
                          borderRadius: '4px',
                          background: item.is_active ? 'rgba(16, 185, 129, 0.1)' : 'var(--color-bg-muted)',
                          color: item.is_active ? 'var(--color-success, #10b981)' : 'var(--color-text-secondary)',
                          fontWeight: item.is_active ? 600 : 'normal',
                        }}
                      >
                        {item.is_active ? '🟢 生效中' : '⚪ 历史版本/草稿'}
                      </span>
                    </div>

                    <div style={{ display: 'flex', gap: '6px' }}>
                      {isAdmin && !item.is_active && (
                        <button
                          type="button"
                          className="btn btn-secondary"
                          onClick={() => void onActivate(item)}
                          style={{ fontSize: '11px', padding: '2px 8px' }}
                        >
                          切换激活
                        </button>
                      )}
                      {isAdmin && item.is_active && (
                        <button
                          type="button"
                          className="btn btn-secondary"
                          onClick={() => void onDeactivate(item)}
                          style={{ fontSize: '11px', padding: '2px 8px', color: 'var(--color-warning, #f59e0b)' }}
                        >
                          停用
                        </button>
                      )}
                      <button
                        type="button"
                        className="btn btn-secondary"
                        onClick={() => void schemaApi.downloadProtoSchema(item.id, item.file_name)}
                        style={{ fontSize: '11px', padding: '2px 8px' }}
                      >
                        ⬇️ 下载
                      </button>
                      {isAdmin && !item.is_active && (
                        <button
                          type="button"
                          className="btn btn-secondary"
                          onClick={() => void onDelete(item)}
                          style={{ fontSize: '11px', padding: '2px 8px', color: 'var(--color-danger, #ef4444)' }}
                        >
                          删除
                        </button>
                      )}
                    </div>
                  </div>

                  <div style={{ fontSize: '12px', color: 'var(--color-text-secondary)' }}>
                    {`文件: ${item.file_name} (${(item.file_size / 1024).toFixed(1)} KB) · 接口: ${item.operation_count} · 上传时间: ${item.created_at ? new Date(item.created_at).toLocaleDateString() : '-'}`}
                    {item.created_by_name && ` · 操作人: ${item.created_by_name}`}
                  </div>
                  {item.description && (
                    <div style={{ marginTop: '4px', fontSize: '12px', color: 'var(--color-text-primary)' }}>
                      备注: {item.description}
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
};

export interface SchemaAssetInspectDrawerProps {
  open: boolean;
  onClose: () => void;
  schema: ProtoSchemaLedgerItemDTO | null;
  isAdmin: boolean;
  onActivate: (schema: ProtoSchemaLedgerItemDTO) => Promise<void>;
  onDeactivate: (schema: ProtoSchemaLedgerItemDTO) => Promise<void>;
  onDelete: (schema: ProtoSchemaLedgerItemDTO) => Promise<void>;
  onUploadNewVersion: (schema: ProtoSchemaLedgerItemDTO) => void;
  onNavigateToCatalog: (schemaId: string | number) => void;
}

export const SchemaAssetInspectDrawer: React.FC<SchemaAssetInspectDrawerProps> = ({
  open,
  onClose,
  schema,
  isAdmin,
  onActivate,
  onDeactivate,
  onDelete,
  onUploadNewVersion,
  onNavigateToCatalog,
}) => {
  if (!open || !schema) return null;

  return (
    <Drawer
      open={open}
      onClose={onClose}
      title={`描述集资产透视: ${schema.name}`}
      width={680}
    >
      <SchemaAssetInspectContent
        schema={schema}
        isAdmin={isAdmin}
        onActivate={onActivate}
        onDeactivate={onDeactivate}
        onDelete={onDelete}
        onUploadNewVersion={onUploadNewVersion}
        onNavigateToCatalog={onNavigateToCatalog}
      />
    </Drawer>
  );
};
