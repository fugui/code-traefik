import React, { useState } from 'react';
import { X, Sparkles } from 'lucide-react';
import type { ArchProcess, GoldenSuite } from '../../types';
import { curateGoldenSuite } from '../../api/golden_suite';
import { useToast } from '../../utils/toast';

interface GoldenSuiteCreateModalProps {
  open: boolean;
  onClose: () => void;
  archProcesses: ArchProcess[];
  onCreated: (suite: GoldenSuite) => void;
}

export const GoldenSuiteCreateModal: React.FC<GoldenSuiteCreateModalProps> = ({
  open,
  onClose,
  archProcesses,
  onCreated,
}) => {
  const toast = useToast();
  const [code, setCode] = useState('');
  const [name, setName] = useState('');
  const [archProcessId, setArchProcessId] = useState<number | undefined>(
    archProcesses[0] ? Number(archProcesses[0].id) : undefined
  );
  const [version, setVersion] = useState('v1.0.0');
  const [extraIgnorePaths, setExtraIgnorePaths] = useState('rsp.timestamp');
  const [description, setDescription] = useState('');
  const [submitting, setSubmitting] = useState(false);

  if (!open) return null;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!code.trim()) {
      toast.error('请输入全局唯一套件编码');
      return;
    }
    if (!name.trim()) {
      toast.error('请输入套件名称');
      return;
    }
    if (!archProcessId) {
      toast.error('请选择绑定的标准架构进程');
      return;
    }

    setSubmitting(true);
    try {
      const paths = extraIgnorePaths
        .split(',')
        .map((p) => p.trim())
        .filter(Boolean);

      const res = await curateGoldenSuite({
        new_suite: {
          code: code.trim(),
          name: name.trim(),
          arch_process_id: archProcessId,
          version: version.trim() || 'v1.0.0',
          extra_ignore_paths: paths,
          description: description.trim(),
        },
        session_id: 0,
        record_ids: [],
      });

      toast.success(`成功创建黄金套件 [${res.suite.name}]`);
      onCreated(res.suite);
      onClose();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '创建黄金套件失败');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div
      className="code-modal-mask"
      style={{
        position: 'fixed',
        top: 0,
        left: 0,
        right: 0,
        bottom: 0,
        backgroundColor: 'rgba(15, 23, 42, 0.45)',
        backdropFilter: 'blur(2px)',
        zIndex: 1050,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
      }}
      onClick={onClose}
    >
      <div
        className="code-modal"
        style={{
          width: '540px',
          maxWidth: '92vw',
          backgroundColor: 'var(--color-bg-primary, #ffffff)',
          borderRadius: '8px',
          border: '1px solid var(--color-border, #e2e8f0)',
          boxShadow: '0 20px 25px -5px rgba(0, 0, 0, 0.1)',
          overflow: 'hidden',
        }}
        onClick={(e) => e.stopPropagation()}
        data-testid="golden-suite-create-modal"
      >
        <div
          style={{
            padding: '16px 20px',
            borderBottom: '1px solid var(--color-border, #e2e8f0)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            backgroundColor: 'var(--color-bg-secondary, #f8fafc)',
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
            <Sparkles size={18} color="var(--color-warning, #d97706)" />
            <h3 style={{ margin: 0, fontSize: '15px', fontWeight: 600 }}>新建黄金契约测试套件</h3>
          </div>
          <button
            type="button"
            className="code-btn-icon"
            onClick={onClose}
            style={{ background: 'none', border: 'none', cursor: 'pointer' }}
          >
            <X size={16} />
          </button>
        </div>

        <form onSubmit={handleSubmit} style={{ padding: '20px' }}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '14px' }}>
            <div>
              <label style={{ display: 'block', fontSize: '12px', fontWeight: 500, marginBottom: '4px' }}>
                套件编码 (全局唯一) <span style={{ color: 'var(--color-danger, #ef4444)' }}>*</span>
              </label>
              <input
                type="text"
                value={code}
                onChange={(e) => setCode(e.target.value)}
                placeholder="如 GS_ORDER_CORE"
                style={{
                  width: '100%',
                  padding: '7px 10px',
                  fontSize: '13px',
                  borderRadius: '4px',
                  border: '1px solid var(--color-border, #cbd5e1)',
                }}
                data-testid="input-modal-suite-code"
              />
            </div>

            <div>
              <label style={{ display: 'block', fontSize: '12px', fontWeight: 500, marginBottom: '4px' }}>
                套件名称 <span style={{ color: 'var(--color-danger, #ef4444)' }}>*</span>
              </label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="如 订单核心业务回归基线套件"
                style={{
                  width: '100%',
                  padding: '7px 10px',
                  fontSize: '13px',
                  borderRadius: '4px',
                  border: '1px solid var(--color-border, #cbd5e1)',
                }}
                data-testid="input-modal-suite-name"
              />
            </div>

            <div style={{ display: 'flex', gap: '12px' }}>
              <div style={{ flex: 1 }}>
                <label style={{ display: 'block', fontSize: '12px', fontWeight: 500, marginBottom: '4px' }}>
                  绑定的标准架构进程 <span style={{ color: 'var(--color-danger, #ef4444)' }}>*</span>
                </label>
                <select
                  value={archProcessId}
                  onChange={(e) => setArchProcessId(Number(e.target.value))}
                  style={{
                    width: '100%',
                    padding: '7px 10px',
                    fontSize: '13px',
                    borderRadius: '4px',
                    border: '1px solid var(--color-border, #cbd5e1)',
                  }}
                  data-testid="select-modal-arch-proc"
                >
                  {archProcesses.map((ap) => (
                    <option key={ap.id} value={ap.id}>
                      [{ap.code}] {ap.name}
                    </option>
                  ))}
                </select>
              </div>

              <div style={{ width: '120px' }}>
                <label style={{ display: 'block', fontSize: '12px', fontWeight: 500, marginBottom: '4px' }}>
                  版本号
                </label>
                <input
                  type="text"
                  value={version}
                  onChange={(e) => setVersion(e.target.value)}
                  style={{
                    width: '100%',
                    padding: '7px 10px',
                    fontSize: '13px',
                    borderRadius: '4px',
                    border: '1px solid var(--color-border, #cbd5e1)',
                  }}
                />
              </div>
            </div>

            <div>
              <label style={{ display: 'block', fontSize: '12px', fontWeight: 500, marginBottom: '4px' }}>
                套件级默认忽略字段路径 (逗号分隔)
              </label>
              <input
                type="text"
                value={extraIgnorePaths}
                onChange={(e) => setExtraIgnorePaths(e.target.value)}
                placeholder="如 rsp.timestamp, rsp.trace_id"
                style={{
                  width: '100%',
                  padding: '7px 10px',
                  fontSize: '13px',
                  borderRadius: '4px',
                  border: '1px solid var(--color-border, #cbd5e1)',
                }}
              />
            </div>

            <div>
              <label style={{ display: 'block', fontSize: '12px', fontWeight: 500, marginBottom: '4px' }}>
                套件描述说明
              </label>
              <textarea
                rows={2}
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="说明该套件所覆盖的用例范围与回归目标..."
                style={{
                  width: '100%',
                  padding: '7px 10px',
                  fontSize: '13px',
                  borderRadius: '4px',
                  border: '1px solid var(--color-border, #cbd5e1)',
                }}
              />
            </div>
          </div>

          <div
            style={{
              marginTop: '20px',
              display: 'flex',
              justifyContent: 'flex-end',
              gap: '10px',
            }}
          >
            <button
              type="button"
              onClick={onClose}
              className="code-btn code-btn-secondary"
              style={{ padding: '6px 14px', fontSize: '13px' }}
            >
              取消
            </button>
            <button
              type="submit"
              disabled={submitting}
              className="code-btn code-btn-primary"
              style={{
                padding: '6px 18px',
                fontSize: '13px',
                backgroundColor: 'var(--color-primary, #3b82f6)',
                color: '#ffffff',
                border: 'none',
                cursor: submitting ? 'not-allowed' : 'pointer',
              }}
              data-testid="btn-submit-create-suite"
            >
              {submitting ? '正在创建...' : '确认创建'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
};
