import React, { useEffect, useState } from 'react';
import { Modal } from '@code/common';
import { useToast } from '../../utils/toast';
import * as schemaApi from '../../api/schema';
import type {
  CompileDiagnosticDTO,
  CompileProtoFileItem,
  ConflictOrBrokenResponse,
  ProtoCategory,
} from '../../types';

export type SchemaUploadMode = 'upload' | 'compile';

export interface SchemaUploadModalProps {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
  initialName?: string;
  initialCategory?: ProtoCategory;
  initialMode?: SchemaUploadMode;
  onConflict?: (data: ConflictOrBrokenResponse) => void;
  onBreakingChange?: (
    data: ConflictOrBrokenResponse,
    onOverrideSubmit: (reason: string) => Promise<void>
  ) => void;
}

interface CompileErrorState {
  message: string;
  code?: string;
  diagnostics?: CompileDiagnosticDTO[];
  missingImports?: string[];
}

const DEFAULT_PROTO_FILE: CompileProtoFileItem = {
  path: 'service.proto',
  content: [
    'syntax = "proto3";',
    '',
    'package example.v1;',
    '',
    'service ExampleService {',
    '  rpc Ping (PingRequest) returns (PingResponse);',
    '}',
    '',
    'message PingRequest {',
    '  string payload = 1;',
    '}',
    '',
    'message PingResponse {',
    '  string message = 1;',
    '}',
    '',
  ].join('\n'),
};

const MAX_PB_BYTES = 16 * 1024 * 1024; // 16MB

export const SchemaUploadModal: React.FC<SchemaUploadModalProps> = ({
  open,
  onClose,
  onSuccess,
  initialName = '',
  initialCategory = 'BUSINESS',
  initialMode = 'upload',
  onConflict,
  onBreakingChange,
}) => {
  const toast = useToast();
  const [mode, setMode] = useState<SchemaUploadMode>(initialMode);
  const [file, setFile] = useState<File | null>(null);
  const [files, setFiles] = useState<CompileProtoFileItem[]>([{ ...DEFAULT_PROTO_FILE }]);
  const [includeDirsText, setIncludeDirsText] = useState('');
  const [compileError, setCompileError] = useState<CompileErrorState | null>(null);
  const [name, setName] = useState(initialName);
  const [category, setCategory] = useState<ProtoCategory>(initialCategory);
  const [versionTag, setVersionTag] = useState('');
  const [description, setDescription] = useState('');
  const [autoActivate, setAutoActivate] = useState(false);
  const [uploading, setUploading] = useState(false);

  const isNewVersionMode = Boolean(initialName);

  useEffect(() => {
    if (open) {
      setMode(initialMode);
      setName(initialName);
      setCategory(initialCategory);
      setFile(null);
      setFiles([{ ...DEFAULT_PROTO_FILE }]);
      setIncludeDirsText('');
      setCompileError(null);
      setVersionTag('');
      setDescription('');
      setAutoActivate(false);
    }
  }, [open, initialName, initialCategory, initialMode]);

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const selected = e.target.files?.[0];
    if (!selected) return;

    if (selected.size > MAX_PB_BYTES) {
      toast.error('上传的 .pb 描述集大小不能超过 16MB');
      e.target.value = '';
      return;
    }

    setFile(selected);
    if (!isNewVersionMode && !name) {
      setName(selected.name.replace(/\.[^/.]+$/, ''));
    }
  };

  const handleAddProtoFile = () => {
    setFiles((prev) => [
      ...prev,
      {
        path: `extra_${prev.length + 1}.proto`,
        content: 'syntax = "proto3";\n\npackage example.v1;\n',
      },
    ]);
  };

  const handleUpdateProtoFile = (index: number, patch: Partial<CompileProtoFileItem>) => {
    setFiles((prev) => prev.map((item, idx) => (idx === index ? { ...item, ...patch } : item)));
  };

  const handleRemoveProtoFile = (index: number) => {
    if (files.length <= 1) return;
    setFiles((prev) => prev.filter((_, idx) => idx !== index));
  };

  const handleUpload = async () => {
    if (!file) {
      toast.error('请选择 .pb 描述集文件');
      return;
    }
    if (!name.trim()) {
      toast.error('请填写描述集名称');
      return;
    }

    const formData = new FormData();
    formData.append('file', file);
    formData.append('name', name.trim());
    formData.append('category', category);
    if (versionTag.trim()) formData.append('version_tag', versionTag.trim());
    if (description.trim()) formData.append('description', description.trim());
    formData.append('auto_activate', String(autoActivate));

    setUploading(true);
    try {
      const res = await schemaApi.uploadProtoSchema(formData);
      const inheritedMsg =
        res.inherited_ignore_op_count && res.inherited_ignore_op_count > 0
          ? `，前向继承 ${res.inherited_ignore_op_count} 个接口 Diff 忽略规则`
          : '';
      toast.success(
        `上传成功，生成版本 v${res.schema.version} (解析 ${res.operations.length} 个方法)${inheritedMsg}`
      );
      onSuccess();
      onClose();
    } catch (err) {
      const respData = (err as unknown as { responseData?: ConflictOrBrokenResponse })?.responseData;
      if (respData && (respData.conflicts || respData.broken_dependents) && onConflict) {
        onClose();
        onConflict(respData);
        return;
      }
      toast.error(err instanceof Error ? err.message : '上传失败');
    } finally {
      setUploading(false);
    }
  };

  const handleCompile = async () => {
    setCompileError(null);
    if (!name.trim()) {
      toast.error('请填写描述集名称');
      return;
    }
    const sanitizedFiles = files.map((item) => ({
      path: item.path.trim(),
      content: item.content,
    }));
    if (sanitizedFiles.length === 0 || sanitizedFiles.some((f) => !f.path || !f.content.trim())) {
      toast.error('请确保每个 .proto 源码文件都填写了有效路径与内容');
      return;
    }

    const includeDirs = includeDirsText
      .split(/[\s,]+/)
      .map((s) => s.trim())
      .filter(Boolean);

    const payload = {
      name: name.trim(),
      version_tag: versionTag.trim() || undefined,
      category,
      include_dirs: includeDirs.length > 0 ? includeDirs : undefined,
      files: sanitizedFiles,
      auto_activate: autoActivate,
      description: description.trim() || undefined,
    };

    setUploading(true);
    try {
      const res = await schemaApi.compileProtoSchema(payload);
      const inheritedMsg =
        res.inherited_ignore_op_count && res.inherited_ignore_op_count > 0
          ? `，前向继承 ${res.inherited_ignore_op_count} 个接口 Diff 忽略规则`
          : '';
      toast.success(
        `在线编译成功，生成版本 v${res.schema.version} (解析 ${res.operations.length} 个方法)${inheritedMsg}`
      );
      onSuccess();
      onClose();
    } catch (err) {
      const respData = (err as unknown as { responseData?: ConflictOrBrokenResponse })?.responseData;
      if (
        respData &&
        (respData.code === 'BREAKING_CHANGE_DETECTED' ||
          (respData.breaking_rules && respData.breaking_rules.length > 0)) &&
        onBreakingChange
      ) {
        onClose();
        onBreakingChange(respData, async (reason: string) => {
          const overrideRes = await schemaApi.compileProtoSchema({
            ...payload,
            auto_activate: true,
            allow_breaking: true,
            breaking_reason: reason,
          });
          toast.success(
            `已强制放行并在线编译激活 v${overrideRes.schema.version} (${overrideRes.operations.length} 个契约)`
          );
          onSuccess();
        });
        return;
      }
      if (respData && (respData.conflicts || respData.broken_dependents) && onConflict) {
        onClose();
        onConflict(respData);
        return;
      }
      if (
        respData &&
        (respData.diagnostics?.length ||
          respData.missing_imports?.length ||
          respData.code === 'PATH_TRAVERSAL_DENIED' ||
          respData.code === 'PROTO_COMPILE_ERROR' ||
          respData.code === 'MISSING_IMPORTS')
      ) {
        setCompileError({
          message: respData.error || '在线编译失败',
          code: respData.code,
          diagnostics: respData.diagnostics,
          missingImports: respData.missing_imports,
        });
        toast.error(respData.error || '在线编译失败');
        return;
      }
      toast.error(err instanceof Error ? err.message : '在线编译失败');
    } finally {
      setUploading(false);
    }
  };

  const handleSubmit = () => {
    if (mode === 'compile') {
      return handleCompile();
    }
    return handleUpload();
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={
        mode === 'compile'
          ? isNewVersionMode
            ? `在线源码编译新版本: ${initialName}`
            : '在线源码直编并注册 Protobuf (.proto)'
          : isNewVersionMode
            ? `上传新版本描述集: ${initialName}`
            : '上传并注册 Protobuf 描述集 (.pb)'
      }
      footer={
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '8px' }}>
          <button type="button" className="btn btn-secondary" onClick={onClose} disabled={uploading}>
            取消
          </button>
          <button
            type="button"
            className="btn btn-primary"
            onClick={() => void handleSubmit()}
            disabled={uploading}
          >
            {uploading
              ? mode === 'compile'
                ? '纯 Go 编译解析中...'
                : '上传解析中...'
              : mode === 'compile'
                ? '确认在线编译并生成契约'
                : isNewVersionMode
                  ? '确认上传新版本'
                  : '确认上传并生成映射'}
          </button>
        </div>
      }
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: '16px', color: 'var(--color-text-primary)' }}>
        <div
          style={{
            display: 'flex',
            gap: '6px',
            background: 'var(--color-bg-muted)',
            padding: '4px',
            borderRadius: '8px',
            border: '1px solid var(--color-border-primary)',
          }}
        >
          <button
            type="button"
            onClick={() => {
              setMode('upload');
              setCompileError(null);
            }}
            style={{
              flex: 1,
              padding: '6px 12px',
              borderRadius: '6px',
              border: 'none',
              cursor: 'pointer',
              fontSize: '12px',
              fontWeight: mode === 'upload' ? 600 : 400,
              background: mode === 'upload' ? 'var(--color-bg-surface)' : 'transparent',
              color: mode === 'upload' ? 'var(--color-primary)' : 'var(--color-text-secondary)',
            }}
          >
            📦 上传二进制描述集 (.pb)
          </button>
          <button
            type="button"
            onClick={() => {
              setMode('compile');
              setCompileError(null);
            }}
            style={{
              flex: 1,
              padding: '6px 12px',
              borderRadius: '6px',
              border: 'none',
              cursor: 'pointer',
              fontSize: '12px',
              fontWeight: mode === 'compile' ? 600 : 400,
              background: mode === 'compile' ? 'var(--color-bg-surface)' : 'transparent',
              color: mode === 'compile' ? 'var(--color-primary)' : 'var(--color-text-secondary)',
            }}
          >
            ⚡ 在线源码直编 (.proto)
          </button>
        </div>

        {isNewVersionMode && (
          <div
            style={{
              padding: '10px 14px',
              borderRadius: '6px',
              background: 'var(--color-bg-muted)',
              border: '1px solid var(--color-border-primary)',
              fontSize: '13px',
              color: 'var(--color-primary)',
              display: 'flex',
              alignItems: 'center',
              gap: '8px',
            }}
          >
            <span>ℹ️</span>
            <span>当前为同名升级模式：系统将自动生成下一版本号，并自动前向继承已配置的接口 Diff 忽略规则。</span>
          </div>
        )}

        {mode === 'upload' ? (
          <div>
            <label style={{ display: 'block', fontSize: '13px', fontWeight: 600, marginBottom: '6px' }}>
              选择 .pb 描述集文件 (限制 ≤ 16MB):
            </label>
            <input
              type="file"
              accept=".pb,.bin"
              onChange={handleFileChange}
              style={{
                width: '100%',
                padding: '8px',
                borderRadius: '6px',
                border: '1px solid var(--color-border-primary)',
                background: 'var(--color-bg-surface)',
                color: 'var(--color-text-primary)',
              }}
            />
            {file && (
              <div style={{ marginTop: '4px', fontSize: '12px', color: 'var(--color-text-secondary)' }}>
                已选文件: {file.name} ({(file.size / 1024).toFixed(1)} KB)
              </div>
            )}
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '10px' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <label style={{ fontSize: '13px', fontWeight: 600 }}>
                多文件 .proto 源码包 (支持 import 平台已注册 BASE_LIB 与 well-known types):
              </label>
              <button
                type="button"
                className="btn btn-secondary"
                onClick={handleAddProtoFile}
                style={{ padding: '4px 10px', fontSize: '12px' }}
              >
                + 添加 .proto 文件
              </button>
            </div>

            {files.map((item, idx) => (
              <div
                key={`proto-file-${idx}`}
                style={{
                  border: '1px solid var(--color-border-primary)',
                  borderRadius: '6px',
                  padding: '8px',
                  background: 'var(--color-bg-muted)',
                  display: 'flex',
                  flexDirection: 'column',
                  gap: '6px',
                }}
              >
                <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
                  <input
                    type="text"
                    value={item.path}
                    onChange={(e) => handleUpdateProtoFile(idx, { path: e.target.value })}
                    placeholder="相对路径，如 order/v1/order.proto"
                    style={{
                      flex: 1,
                      padding: '6px 10px',
                      borderRadius: '6px',
                      border: '1px solid var(--color-border-primary)',
                      background: 'var(--color-bg-surface)',
                      color: 'var(--color-text-primary)',
                      fontFamily: 'monospace',
                      fontSize: '12px',
                    }}
                  />
                  {files.length > 1 && (
                    <button
                      type="button"
                      className="btn btn-secondary"
                      onClick={() => handleRemoveProtoFile(idx)}
                      style={{ padding: '4px 8px', fontSize: '11px' }}
                    >
                      移除
                    </button>
                  )}
                </div>
                <textarea
                  value={item.content}
                  onChange={(e) => handleUpdateProtoFile(idx, { content: e.target.value })}
                  rows={7}
                  spellCheck={false}
                  style={{
                    width: '100%',
                    padding: '8px 10px',
                    borderRadius: '6px',
                    border: '1px solid var(--color-border-primary)',
                    background: 'var(--color-bg-input)',
                    color: 'var(--color-text-primary)',
                    fontFamily: 'monospace',
                    fontSize: '12px',
                    resize: 'vertical',
                  }}
                />
              </div>
            ))}

            <div>
              <label style={{ display: 'block', fontSize: '12px', fontWeight: 600, marginBottom: '4px' }}>
                导入搜索根目录 include_dirs (选填，逗号分隔):
              </label>
              <input
                type="text"
                value={includeDirsText}
                onChange={(e) => setIncludeDirsText(e.target.value)}
                placeholder="如 proto, api/proto"
                style={{
                  width: '100%',
                  padding: '6px 10px',
                  borderRadius: '6px',
                  border: '1px solid var(--color-border-primary)',
                  background: 'var(--color-bg-surface)',
                  color: 'var(--color-text-primary)',
                  fontSize: '12px',
                }}
              />
            </div>

            {compileError && (
              <div
                style={{
                  padding: '10px 12px',
                  borderRadius: '6px',
                  border: '1px solid var(--color-danger)',
                  background: 'var(--color-bg-muted)',
                  color: 'var(--color-danger)',
                  fontSize: '12px',
                  display: 'flex',
                  flexDirection: 'column',
                  gap: '6px',
                }}
              >
                <div style={{ fontWeight: 600 }}>
                  {compileError.code ? `[${compileError.code}] ` : ''}
                  {compileError.message}
                </div>
                {compileError.diagnostics && compileError.diagnostics.length > 0 && (
                  <ul style={{ margin: 0, paddingLeft: '18px', fontFamily: 'monospace', fontSize: '11px' }}>
                    {compileError.diagnostics.map((d, i) => (
                      <li key={`diag-${i}`}>
                        {d.file}:{d.line}:{d.col} - {d.message}
                      </li>
                    ))}
                  </ul>
                )}
                {compileError.missingImports && compileError.missingImports.length > 0 && (
                  <div style={{ fontSize: '11px' }}>
                    缺失依赖文件: {compileError.missingImports.join(', ')}
                  </div>
                )}
              </div>
            )}
          </div>
        )}

        <div>
          <label style={{ display: 'block', fontSize: '13px', fontWeight: 600, marginBottom: '6px' }}>
            描述集类型 (Category):
          </label>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: '10px' }}>
            <div
              onClick={() => setCategory('BASE_LIB')}
              style={{
                border: `2px solid ${category === 'BASE_LIB' ? 'var(--color-primary, #3b82f6)' : 'var(--color-border-primary)'}`,
                background: category === 'BASE_LIB' ? 'rgba(59, 130, 246, 0.08)' : 'var(--color-bg-surface)',
                borderRadius: '8px',
                padding: '10px',
                cursor: 'pointer',
                transition: 'all 0.2s ease',
              }}
            >
              <div style={{ fontWeight: 600, fontSize: '13px', marginBottom: '4px' }}>🧱 基础公共库</div>
              <div style={{ fontSize: '11px', color: 'var(--color-text-secondary)' }}>
                纯公共消息/枚举底座，供其他业务包依赖引用 (支持 0 Service)
              </div>
            </div>

            <div
              onClick={() => setCategory('BUSINESS')}
              style={{
                border: `2px solid ${category === 'BUSINESS' ? 'var(--color-primary, #3b82f6)' : 'var(--color-border-primary)'}`,
                background: category === 'BUSINESS' ? 'rgba(59, 130, 246, 0.08)' : 'var(--color-bg-surface)',
                borderRadius: '8px',
                padding: '10px',
                cursor: 'pointer',
                transition: 'all 0.2s ease',
              }}
            >
              <div style={{ fontWeight: 600, fontSize: '13px', marginBottom: '4px' }}>📦 业务域服务</div>
              <div style={{ fontSize: '11px', color: 'var(--color-text-secondary)' }}>
                标准微服务 RPC 接口定义包，包含具体服务与方法契约
              </div>
            </div>

            <div
              onClick={() => setCategory('GATEWAY')}
              style={{
                border: `2px solid ${category === 'GATEWAY' ? 'var(--color-primary, #3b82f6)' : 'var(--color-border-primary)'}`,
                background: category === 'GATEWAY' ? 'rgba(59, 130, 246, 0.08)' : 'var(--color-bg-surface)',
                borderRadius: '8px',
                padding: '10px',
                cursor: 'pointer',
                transition: 'all 0.2s ease',
              }}
            >
              <div style={{ fontWeight: 600, fontSize: '13px', marginBottom: '4px' }}>🌐 网关聚合层</div>
              <div style={{ fontSize: '11px', color: 'var(--color-text-secondary)' }}>
                BFF / API 网关专属描述集，提供对客户端聚合契约
              </div>
            </div>
          </div>
        </div>

        <div>
          <label style={{ display: 'block', fontSize: '13px', fontWeight: 600, marginBottom: '6px' }}>
            描述集唯一标识 (name):
          </label>
          <input
            type="text"
            value={name}
            disabled={isNewVersionMode}
            onChange={(e) => setName(e.target.value)}
            placeholder="如 common-base 或 trade-service"
            style={{
              width: '100%',
              padding: '8px 12px',
              borderRadius: '6px',
              border: '1px solid var(--color-border-primary)',
              background: isNewVersionMode ? 'var(--color-bg-muted)' : 'var(--color-bg-surface)',
              color: 'var(--color-text-primary)',
            }}
          />
        </div>

        <div>
          <label style={{ display: 'block', fontSize: '13px', fontWeight: 600, marginBottom: '6px' }}>
            版本标签 (version_tag - 选填):
          </label>
          <input
            type="text"
            value={versionTag}
            onChange={(e) => setVersionTag(e.target.value)}
            placeholder="如 v1.2.0 或 git commit-hash"
            style={{
              width: '100%',
              padding: '8px 12px',
              borderRadius: '6px',
              border: '1px solid var(--color-border-primary)',
              background: 'var(--color-bg-surface)',
              color: 'var(--color-text-primary)',
            }}
          />
        </div>

        <div>
          <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer', fontSize: '13px' }}>
            <input
              type="checkbox"
              checked={autoActivate}
              onChange={(e) => setAutoActivate(e.target.checked)}
            />
            <span>上传后立即尝试激活为当前生效版本 (如有方法冲突或缺失下游依赖将拦截)</span>
          </label>
        </div>

        <div>
          <label style={{ display: 'block', fontSize: '13px', fontWeight: 600, marginBottom: '6px' }}>
            变更说明 / 备注:
          </label>
          <textarea
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="填写本次描述集迭代说明..."
            rows={3}
            style={{
              width: '100%',
              padding: '8px 12px',
              borderRadius: '6px',
              border: '1px solid var(--color-border-primary)',
              background: 'var(--color-bg-surface)',
              color: 'var(--color-text-primary)',
              resize: 'vertical',
            }}
          />
        </div>
      </div>
    </Modal>
  );
};
