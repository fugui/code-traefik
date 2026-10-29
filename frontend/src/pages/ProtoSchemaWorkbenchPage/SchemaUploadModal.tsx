import React, { useState } from 'react';
import { Modal } from '@code/common';
import { useToast } from '../../utils/toast';
import * as schemaApi from '../../api/schema';

export interface SchemaUploadModalProps {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
}

const MAX_PB_BYTES = 4 * 1024 * 1024; // 4MB

export const SchemaUploadModal: React.FC<SchemaUploadModalProps> = ({ open, onClose, onSuccess }) => {
  const toast = useToast();
  const [file, setFile] = useState<File | null>(null);
  const [name, setName] = useState('');
  const [versionTag, setVersionTag] = useState('');
  const [description, setDescription] = useState('');
  const [autoActivate, setAutoActivate] = useState(false);
  const [uploading, setUploading] = useState(false);

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const selected = e.target.files?.[0];
    if (!selected) return;

    if (selected.size > MAX_PB_BYTES) {
      toast.error('上传的 .pb 描述集大小不能超过 4MB');
      e.target.value = '';
      return;
    }

    setFile(selected);
    if (!name) {
      setName(selected.name.replace(/\.[^/.]+$/, ''));
    }
  };

  const handleUpload = async () => {
    if (!file) {
      toast.error('请选择 .pb 二进制文件');
      return;
    }
    if (!name.trim()) {
      toast.error('请填写描述集名称');
      return;
    }

    const formData = new FormData();
    formData.append('file', file);
    formData.append('name', name);
    if (versionTag) formData.append('version_tag', versionTag);
    if (description) formData.append('description', description);
    formData.append('auto_activate', String(autoActivate));

    setUploading(true);
    try {
      const res = await schemaApi.uploadProtoSchema(formData);
      toast.success(`上传成功，解析提取出 ${res.operations.length} 个方法映射契约`);
      onSuccess();
      onClose();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '上传失败');
    } finally {
      setUploading(false);
    }
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="上传并注册 Protobuf 描述集 (.pb)"
      footer={
        <div className="code-modal-footer">
          <button type="button" className="code-btn code-btn--secondary" onClick={onClose} disabled={uploading}>
            取消
          </button>
          <button type="button" className="code-btn code-btn--primary" onClick={() => void handleUpload()} disabled={uploading}>
            {uploading ? '上传解析中...' : '确认上传并生成映射'}
          </button>
        </div>
      }
    >
      <div className="code-form">
        <div className="code-form-item">
          <label className="code-form-label">选择 .pb 描述集文件 (限制 &lt;= 4MB):</label>
          <input
            type="file"
            accept=".pb,.bin"
            className="code-input"
            onChange={handleFileChange}
          />
        </div>

        <div className="code-form-item">
          <label className="code-form-label">描述集唯一名称 (name):</label>
          <input
            type="text"
            className="code-input"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="如 account_service.pb"
          />
        </div>

        <div className="code-form-item">
          <label className="code-form-label">版本标签 (version_tag - 选填):</label>
          <input
            type="text"
            className="code-input"
            value={versionTag}
            onChange={(e) => setVersionTag(e.target.value)}
            placeholder="如 v1.2.0 或 git-commit-hash"
          />
        </div>

        <div className="code-form-item">
          <label className="code-checkbox-chip">
            <input
              type="checkbox"
              checked={autoActivate}
              onChange={(e) => setAutoActivate(e.target.checked)}
            />
            <span>上传后立即尝试激活为当前生效版本 (如有契约冲突将回滚并提示)</span>
          </label>
        </div>

        <div className="code-form-item">
          <label className="code-form-label">描述说明:</label>
          <textarea
            className="code-textarea"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </div>
      </div>
    </Modal>
  );
};
