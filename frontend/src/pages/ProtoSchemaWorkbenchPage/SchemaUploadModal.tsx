import React, { useEffect, useState } from 'react';
import { Modal } from '@code/common';
import { useToast } from '../../utils/toast';
import * as schemaApi from '../../api/schema';
import type { ProtoCategory, ConflictOrBrokenResponse } from '../../types';

export interface SchemaUploadModalProps {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
  initialName?: string;
  initialCategory?: ProtoCategory;
  onConflict?: (data: ConflictOrBrokenResponse) => void;
}

const MAX_PB_BYTES = 16 * 1024 * 1024; // 16MB

export const SchemaUploadModal: React.FC<SchemaUploadModalProps> = ({
  open,
  onClose,
  onSuccess,
  initialName = '',
  initialCategory = 'BUSINESS',
  onConflict,
}) => {
  const toast = useToast();
  const [file, setFile] = useState<File | null>(null);
  const [name, setName] = useState(initialName);
  const [category, setCategory] = useState<ProtoCategory>(initialCategory);
  const [versionTag, setVersionTag] = useState('');
  const [description, setDescription] = useState('');
  const [autoActivate, setAutoActivate] = useState(false);
  const [uploading, setUploading] = useState(false);

  const isNewVersionMode = Boolean(initialName);

  useEffect(() => {
    if (open) {
      setName(initialName);
      setCategory(initialCategory);
      setFile(null);
      setVersionTag('');
      setDescription('');
      setAutoActivate(false);
    }
  }, [open, initialName, initialCategory]);

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

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={isNewVersionMode ? `上传新版本描述集: ${initialName}` : '上传并注册 Protobuf 描述集 (.pb)'}
      footer={
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '8px' }}>
          <button type="button" className="btn btn-secondary" onClick={onClose} disabled={uploading}>
            取消
          </button>
          <button
            type="button"
            className="btn btn-primary"
            onClick={() => void handleUpload()}
            disabled={uploading}
          >
            {uploading ? '上传解析中...' : isNewVersionMode ? '确认上传新版本' : '确认上传并生成映射'}
          </button>
        </div>
      }
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: '16px', color: 'var(--color-text-primary)' }}>
        {isNewVersionMode && (
          <div
            style={{
              padding: '10px 14px',
              borderRadius: '6px',
              background: 'var(--color-bg-muted)',
              border: '1px solid var(--color-border-primary)',
              fontSize: '13px',
              color: 'var(--color-primary, #3b82f6)',
              display: 'flex',
              alignItems: 'center',
              gap: '8px',
            }}
          >
            <span>ℹ️</span>
            <span>当前为同名升级模式：系统将自动生成下一版本号，并自动前向继承已配置的接口 Diff 忽略规则。</span>
          </div>
        )}

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
