import React, { useState } from 'react';
import { Modal } from '@code/common';
import { useToast } from '../../utils/toast';
import * as trafficApi from '../../api/traffic';
import type { RecordingSession, Process, Agent } from '../../types';

export interface RecordingStartModalProps {
  open: boolean;
  onClose: () => void;
  onSuccess: (session: RecordingSession) => void;
  allProcesses: Process[];
  allAgents: Agent[];
  isAdmin: boolean;
}

export const RecordingStartModal: React.FC<RecordingStartModalProps> = ({
  open,
  onClose,
  onSuccess,
  allProcesses,
  allAgents,
  isAdmin,
}) => {
  const toast = useToast();
  const [name, setName] = useState('');
  const [scope, setScope] = useState<'SHARED' | 'PRIVATE'>('PRIVATE');
  const [targetProcessIds, setTargetProcessIds] = useState<string[]>([]);
  const [maxDurationSec, setMaxDurationSec] = useState(1800);
  const [maxFrames, setMaxFrames] = useState(100000);
  const [description, setDescription] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const toggleProcess = (procId: string) => {
    setTargetProcessIds((prev) => {
      const set = new Set(prev);
      if (set.has(procId)) set.delete(procId);
      else set.add(procId);
      return Array.from(set);
    });
  };

  const handleStart = async () => {
    if (!name.trim()) {
      toast.error('请输入录制批次名称');
      return;
    }
    if (targetProcessIds.length === 0) {
      toast.error('请至少选择一个监控目标业务进程');
      return;
    }

    setSubmitting(true);
    try {
      const sess = await trafficApi.startRecordingSession({
        name: name.trim(),
        scope,
        target_process_ids: targetProcessIds,
        max_duration_sec: maxDurationSec,
        max_frames: maxFrames,
        description: description.trim(),
      });
      toast.success(`协同录制批次 [${sess.session_no}] 启动成功`);
      onSuccess(sess);
      onClose();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '启动协同录制失败');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="发起多进程协同录制"
      width="lg"
      footer={
        <div className="code-modal-footer">
          <button
            type="button"
            className="code-btn code-btn--secondary"
            onClick={onClose}
            disabled={submitting}
          >
            取消
          </button>
          <button
            type="button"
            className="code-btn code-btn--primary"
            onClick={() => void handleStart()}
            disabled={submitting}
            data-testid="btn-confirm-start-recording"
          >
            {submitting ? '启动中...' : '立即启动录制'}
          </button>
        </div>
      }
    >
      <div className="code-form code-p-md" data-testid="recording-start-modal">
        <div className="code-form-item">
          <label className="code-form-label code-required">批次名称</label>
          <input
            type="text"
            className="code-input"
            placeholder="例如：20261101-订单中心核心结算流量录制"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </div>

        <div className="code-form-item">
          <label className="code-form-label">素材归属作用域</label>
          <div className="code-radio-group">
            <label className="code-radio-label">
              <input
                type="radio"
                name="rec_scope"
                value="PRIVATE"
                checked={scope === 'PRIVATE'}
                onChange={() => setScope('PRIVATE')}
              />
              <span>个人私有调试素材 (PRIVATE - 仅本人可见与可调试)</span>
            </label>
            {isAdmin && (
              <label className="code-radio-label">
                <input
                  type="radio"
                  name="rec_scope"
                  value="SHARED"
                  checked={scope === 'SHARED'}
                  onChange={() => setScope('SHARED')}
                />
                <span>团队公共基线素材 (SHARED - 供团队全员重放回归)</span>
              </label>
            )}
          </div>
        </div>

        <div className="code-form-item">
          <label className="code-form-label code-required">
            监控目标业务进程 (勾选即下发 eBPF 探针租约)
          </label>
          <div className="code-process-selector-box code-border code-p-sm code-rounded">
            {allProcesses.length === 0 ? (
              <div className="code-text-muted code-p-sm">暂无可用的业务进程</div>
            ) : (
              allProcesses.map((proc) => {
                const checked = targetProcessIds.includes(proc.id);
                const agent = allAgents.find((a) => a.id === proc.agent_id);
                return (
                  <label key={proc.id} className="code-checkbox-label code-p-xs code-block">
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={() => toggleProcess(proc.id)}
                    />
                    <span className="code-font-bold">{proc.name}</span>
                    <span className="code-font-mono code-text-sm code-ml-xs">
                      (端口: {proc.listen_port > 0 ? proc.listen_port : '纯客户端'})
                    </span>
                    {proc.group_name && (
                      <span className="code-badge code-badge--secondary code-ml-xs">
                        {proc.group_name}
                      </span>
                    )}
                    {agent && (
                      <span className="code-badge code-badge--info code-ml-xs">
                        Agent: {agent.name} [{agent.scope}]
                      </span>
                    )}
                  </label>
                );
              })
            )}
          </div>
        </div>

        <div className="code-grid code-grid-2 code-gap-md">
          <div className="code-form-item">
            <label className="code-form-label">最大持续时间 (秒)</label>
            <input
              type="number"
              className="code-input"
              value={maxDurationSec}
              onChange={(e) => setMaxDurationSec(Number(e.target.value) || 1800)}
            />
          </div>
          <div className="code-form-item">
            <label className="code-form-label">最大捕获帧数</label>
            <input
              type="number"
              className="code-input"
              value={maxFrames}
              onChange={(e) => setMaxFrames(Number(e.target.value) || 100000)}
            />
          </div>
        </div>

        <div className="code-form-item">
          <label className="code-form-label">批次描述与备注</label>
          <textarea
            className="code-textarea"
            rows={2}
            placeholder="说明本次录制的测试场景、涉及模块与用例编号..."
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </div>
      </div>
    </Modal>
  );
};

export default RecordingStartModal;
