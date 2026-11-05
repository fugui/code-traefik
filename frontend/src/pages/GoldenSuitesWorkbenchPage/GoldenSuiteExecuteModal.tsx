import React, { useEffect, useState } from 'react';
import { X, Play, AlertCircle } from 'lucide-react';
import type { Agent, GoldenSuite, Process, ReplayTask } from '../../types';
import { executeGoldenSuite } from '../../api/golden_suite';
import { useToast } from '../../utils/toast';

interface GoldenSuiteExecuteModalProps {
  open: boolean;
  onClose: () => void;
  suite: GoldenSuite | null;
  allAgents: Agent[];
  allProcesses: Process[];
  onExecuted?: (task: ReplayTask) => void;
}

export const GoldenSuiteExecuteModal: React.FC<GoldenSuiteExecuteModalProps> = ({
  open,
  onClose,
  suite,
  allAgents,
  allProcesses,
  onExecuted,
}) => {
  const toast = useToast();
  const [selectedAgentId, setSelectedAgentId] = useState<string | number | undefined>();
  const [timeoutMs, setTimeoutMs] = useState<number>(30000);
  const [submitting, setSubmitting] = useState<boolean>(false);

  // 筛选出挂载了该套件标准架构进程的候选 Agent (安全兼容 number/string)
  const matchingProcesses = allProcesses.filter(
    (p) => p.arch_process_id != null && String(p.arch_process_id) === String(suite?.arch_process_id)
  );
  const candidateAgentIds = new Set(matchingProcesses.map((p) => String(p.agent_id)));
  const candidateAgents = allAgents.filter((a) => candidateAgentIds.has(String(a.id)));
  const defaultAgentId = candidateAgents[0]?.id;

  useEffect(() => {
    if (open && defaultAgentId) {
      setSelectedAgentId(defaultAgentId);
    }
  }, [open, defaultAgentId]);

  if (!open || !suite) return null;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedAgentId) {
      toast.error('请选择执行回归的目标环境 Agent');
      return;
    }

    setSubmitting(true);
    try {
      const task = await executeGoldenSuite(suite.id, {
        env_agent_id: Number(selectedAgentId),
        timeout_ms: timeoutMs,
      });

      toast.success(
        `⚡ 成功下发黄金套件脱机回归任务 [${task.task_no}]！可在防劣化回归台透视进度。`
      );
      if (onExecuted) {
        onExecuted(task);
      }
      onClose();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '下发脱机回归失败');
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
          width: '520px',
          maxWidth: '92vw',
          backgroundColor: 'var(--color-bg-primary, #ffffff)',
          borderRadius: '8px',
          border: '1px solid var(--color-border, #e2e8f0)',
          boxShadow: '0 20px 25px -5px rgba(0, 0, 0, 0.1)',
          overflow: 'hidden',
        }}
        onClick={(e) => e.stopPropagation()}
        data-testid="golden-suite-execute-modal"
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
            <Play size={18} color="var(--color-primary, #3b82f6)" />
            <h3 style={{ margin: 0, fontSize: '15px', fontWeight: 600 }}>
              发起黄金契约套件脱机回归
            </h3>
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
          <div
            style={{
              padding: '10px 14px',
              backgroundColor: 'var(--color-bg-secondary, #f8fafc)',
              borderRadius: '6px',
              border: '1px solid var(--color-border, #e2e8f0)',
              marginBottom: '16px',
            }}
          >
            <div style={{ fontSize: '13px', fontWeight: 600, marginBottom: '4px' }}>
              [{suite.code}] {suite.name}
            </div>
            <div style={{ fontSize: '12px', color: 'var(--color-text-secondary, #64748b)' }}>
              守护标准进程: <strong>{suite.arch_process?.name || `ID:${suite.arch_process_id}`}</strong> | 基线用例数: <strong>{suite.item_count}</strong>
            </div>
          </div>

          {candidateAgents.length === 0 ? (
            <div
              style={{
                padding: '14px',
                backgroundColor: 'var(--color-warning-bg, #fffbeb)',
                border: '1px solid var(--color-warning-border, #fef3c7)',
                borderRadius: '6px',
                color: 'var(--color-warning-text, #92400e)',
                fontSize: '12px',
                display: 'flex',
                gap: '8px',
                alignItems: 'flex-start',
                marginBottom: '16px',
              }}
            >
              <AlertCircle size={16} style={{ marginTop: '2px', flexShrink: 0 }} />
              <div>
                暂无可执行该套件的环境 Agent！当前没有 Agent 挂载该套件对应的标准架构进程 [
                {suite.arch_process?.name}]。请先在 <strong>Agent 与进程</strong> 模块完成同构挂载。
              </div>
            </div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '14px' }}>
              <div>
                <label style={{ display: 'block', fontSize: '12px', fontWeight: 500, marginBottom: '4px' }}>
                  目标执行环境 Agent <span style={{ color: 'var(--color-danger, #ef4444)' }}>*</span>
                </label>
                <select
                  value={selectedAgentId}
                  onChange={(e) => setSelectedAgentId(Number(e.target.value))}
                  style={{
                    width: '100%',
                    padding: '8px 10px',
                    fontSize: '13px',
                    borderRadius: '4px',
                    border: '1px solid var(--color-border, #cbd5e1)',
                  }}
                  data-testid="select-execute-agent"
                >
                  {candidateAgents.map((a) => (
                    <option key={a.id} value={a.id}>
                      [{a.scope}] {a.name} ({a.endpoint}) - {a.status}
                    </option>
                  ))}
                </select>
              </div>

              <div>
                <label style={{ display: 'block', fontSize: '12px', fontWeight: 500, marginBottom: '4px' }}>
                  单请求注入等待超时 (毫秒)
                </label>
                <input
                  type="number"
                  value={timeoutMs}
                  onChange={(e) => setTimeoutMs(Number(e.target.value))}
                  min={1000}
                  max={120000}
                  step={1000}
                  style={{
                    width: '100%',
                    padding: '7px 10px',
                    fontSize: '13px',
                    borderRadius: '4px',
                    border: '1px solid var(--color-border, #cbd5e1)',
                  }}
                />
              </div>

              <div
                style={{
                  padding: '8px 12px',
                  backgroundColor: 'var(--color-info-bg, #eff6ff)',
                  border: '1px solid var(--color-info-border, #bfdbfe)',
                  borderRadius: '4px',
                  fontSize: '12px',
                  color: 'var(--color-primary, #1e40af)',
                }}
              >
                💡 <strong>排他租约保护预检</strong>：下发回归时主控将向该 Agent 申请 REPLAY 排他租约，若目标 Agent 当前被其他录制或重放会话占用，系统将自动拦截并返回冲突提示。
              </div>
            </div>
          )}

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
              disabled={submitting || candidateAgents.length === 0}
              className="code-btn code-btn-primary"
              style={{
                padding: '6px 18px',
                fontSize: '13px',
                backgroundColor: 'var(--color-primary, #3b82f6)',
                color: '#ffffff',
                border: 'none',
                cursor: submitting || candidateAgents.length === 0 ? 'not-allowed' : 'pointer',
                display: 'flex',
                alignItems: 'center',
                gap: '6px',
              }}
              data-testid="btn-confirm-execute-suite"
            >
              <Play size={14} />
              {submitting ? '正在下发...' : '立即脱机回归'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
};
