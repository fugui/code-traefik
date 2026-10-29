import React, { useState } from 'react';
import { Modal } from '@code/common';
import { useToast } from '../../utils/toast';
import * as trafficApi from '../../api/traffic';
import type { RecordingSession, Process, Agent } from '../../types';

export interface RecordingSessionBarProps {
  currentSession: RecordingSession | null;
  sessions: RecordingSession[];
  onSelectSession: (id: string) => void;
  onRefreshSessions: () => void;
  allProcesses: Process[];
  allAgents: Agent[];
  isAdmin: boolean;
  currentUserId?: number;
}

export const RecordingSessionBar: React.FC<RecordingSessionBarProps> = ({
  currentSession,
  sessions,
  onSelectSession,
  onRefreshSessions,
  allProcesses,
  allAgents,
  isAdmin,
  currentUserId,
}) => {
  const toast = useToast();
  const [startModalOpen, setStartModalOpen] = useState(false);
  const [startForm, setStartForm] = useState({
    name: '',
    scope: 'SHARED' as 'SHARED' | 'PRIVATE',
    target_process_ids: [] as string[],
    max_duration_sec: 1800,
    max_frames: 100000,
    description: '',
  });
  const [starting, setStarting] = useState(false);
  const [stopping, setStopping] = useState(false);

  // 检查是否有进程连接需要重启重连 (A-01 诊断)
  const reconnectDiagnostics = (currentSession?.process_diagnostics ?? []).filter(
    (d) => d.need_reconnect
  );

  const toggleTargetProcess = (procId: string) => {
    setStartForm((prev) => {
      const set = new Set(prev.target_process_ids);
      if (set.has(procId)) set.delete(procId);
      else set.add(procId);
      return { ...prev, target_process_ids: Array.from(set) };
    });
  };

  const handleStartSession = async () => {
    if (!startForm.name.trim()) {
      toast.error('请输入录制批次名称');
      return;
    }
    if (startForm.target_process_ids.length === 0) {
      toast.error('请至少选择一个监控目标进程');
      return;
    }

    setStarting(true);
    try {
      const sess = await trafficApi.startRecordingSession(startForm);
      toast.success(`录制批次 [${sess.session_no}] 已启动`);
      setStartModalOpen(false);
      onRefreshSessions();
      onSelectSession(sess.id);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : '启动录制失败';
      toast.error(msg);
    } finally {
      setStarting(false);
    }
  };

  const handleStopSession = async () => {
    if (!currentSession) return;
    setStopping(true);
    try {
      await trafficApi.stopRecordingSession(currentSession.id);
      toast.success('录制批次已停止，正在等待排空并释放租约');
      onRefreshSessions();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '停止录制失败');
    } finally {
      setStopping(false);
    }
  };

  const handlePublishSession = async () => {
    if (!currentSession) return;
    try {
      await trafficApi.publishRecordingSession(currentSession.id);
      toast.success('私有素材已发布为团队公共素材');
      onRefreshSessions();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '发布素材失败');
    }
  };

  const isOwner = Boolean(
    currentUserId && currentSession?.owner_id && String(currentUserId) === String(currentSession.owner_id)
  );
  const canPublish = Boolean(
    currentSession?.scope === 'PRIVATE' &&
      (isOwner || isAdmin) &&
      (currentSession?.status === 'COMPLETED' || currentSession?.status === 'COMPLETED_BY_LIMIT')
  );

  return (
    <div className="code-session-bar-wrap">
      {/* 顶部批次控制栏 */}
      <div className="code-session-bar">
        <div className="code-session-bar__left">
          <label className="code-field-label">当前录制批次:</label>
          <select
            className="code-select code-select--compact"
            value={currentSession?.id || ''}
            onChange={(e) => onSelectSession(e.target.value)}
          >
            {(sessions ?? []).map((s) => (
              <option key={s.id} value={s.id}>
                {s.name} ({s.session_no} - {s.status}) [{s.scope}]
              </option>
            ))}
            {(sessions ?? []).length === 0 && <option value="">暂无录制批次</option>}
          </select>

          {currentSession && (
            <div className="code-session-meta-chips">
              <span className={`code-status-tag code-status-tag--${currentSession.status.toLowerCase()}`}>
                {currentSession.status}
              </span>
              <span className="code-badge code-badge--secondary">{currentSession.scope}</span>
              <span className="code-font-mono code-text-sm">
                已捕获: {currentSession.captured_frames} 帧
              </span>
            </div>
          )}
        </div>

        <div className="code-session-bar__actions">
          {currentSession?.status === 'RECORDING' && (
            <button
              type="button"
              className="code-btn code-btn--danger code-btn--sm"
              disabled={stopping}
              onClick={() => void handleStopSession()}
            >
              {stopping ? '正在停止...' : '停止录制'}
            </button>
          )}

          {canPublish && (
            <button
              type="button"
              className="code-btn code-btn--secondary code-btn--sm"
              onClick={() => void handlePublishSession()}
              title="将本人私有录制批次晋升为团队共享公共回归资产"
            >
              发布为公共素材
            </button>
          )}

          <button
            type="button"
            className="code-btn code-btn--primary code-btn--sm"
            onClick={() => {
              setStartForm({
                name: '',
                scope: 'SHARED',
                target_process_ids: [],
                max_duration_sec: 1800,
                max_frames: 100000,
                description: '',
              });
              setStartModalOpen(true);
            }}
          >
            + 发起协同录制
          </button>
        </div>
      </div>

      {/* A-01 存量连接未插桩重启提示横幅 */}
      {reconnectDiagnostics.length > 0 && currentSession?.status === 'RECORDING' && (
        <div className="code-alert-banner code-alert-banner--warning">
          <div className="code-alert-banner__icon">⚠</div>
          <div className="code-alert-banner__content">
            <strong>检测到存量长连接需重连提示 (A-01)：</strong>
            <span>
              以下业务进程在挂载监控探针前已存在活跃的长连接（已建立的 TCP 链接未在握手阶段捕获 SYN/ACK）：
            </span>
            <ul className="code-diag-list">
              {(reconnectDiagnostics ?? []).map((d) => (
                <li key={d.process_id}>
                  进程 <code>{d.process_name || d.process_id}</code> (Port {d.listen_port}): 存量长连接数 {d.established_conns_count} 条。
                  {d.reconnect_guide || '建议触发业务客户端短暂重连以完整捕获 CROS RPC 会话头。'}
                </li>
              ))}
            </ul>
          </div>
        </div>
      )}

      {/* 发起录制弹窗 */}
      <Modal
        open={startModalOpen}
        onClose={() => setStartModalOpen(false)}
        title="发起单 Agent / 跨进程协同录制任务"
        width="lg"
        footer={
          <div className="code-modal-footer">
            <button
              type="button"
              className="code-btn code-btn--secondary"
              onClick={() => setStartModalOpen(false)}
              disabled={starting}
            >
              取消
            </button>
            <button
              type="button"
              className="code-btn code-btn--primary"
              onClick={() => void handleStartSession()}
              disabled={starting}
            >
              {starting ? '加锁下发中...' : '原子加锁并启动录制'}
            </button>
          </div>
        }
      >
        <div className="code-form">
          <div className="code-form-item">
            <label className="code-form-label">录制批次名称:</label>
            <input
              type="text"
              className="code-input"
              value={startForm.name}
              onChange={(e) => setStartForm({ ...startForm, name: e.target.value })}
              placeholder="如 订单中枢与风控全链路压测录制"
            />
          </div>

          <div className="code-form-grid-2">
            <div className="code-form-item">
              <label className="code-form-label">素材归属作用域:</label>
              <select
                className="code-select"
                value={startForm.scope}
                onChange={(e) =>
                  setStartForm({
                    ...startForm,
                    scope: e.target.value as 'SHARED' | 'PRIVATE',
                  })
                }
              >
                <option value="SHARED">团队公共素材 (SHARED - 全员可重放)</option>
                <option value="PRIVATE">个人私有素材 (PRIVATE - 仅本人及管理员可见)</option>
              </select>
            </div>
            <div className="code-form-item">
              <label className="code-form-label">自动停录上限 (秒 / 帧):</label>
              <div className="code-input-group-2">
                <input
                  type="number"
                  className="code-input"
                  value={startForm.max_duration_sec}
                  onChange={(e) => setStartForm({ ...startForm, max_duration_sec: Number(e.target.value) })}
                  title="时长上限秒"
                />
                <input
                  type="number"
                  className="code-input"
                  value={startForm.max_frames}
                  onChange={(e) => setStartForm({ ...startForm, max_frames: Number(e.target.value) })}
                  title="帧数上限"
                />
              </div>
            </div>
          </div>

          {/* 勾选目标进程 (含 Agent 租约冲突置灰提示) */}
          <div className="code-form-item">
            <label className="code-form-label">勾选要协同监控的业务进程 (自动原子锁定所属 Agent):</label>
            <div className="code-process-selector-list">
              {(allProcesses ?? []).map((proc) => {
                const parentAgent = allAgents.find((a) => a.id === proc.agent_id);
                const isLeased = Boolean(parentAgent?.lease_type);
                const checked = startForm.target_process_ids.includes(proc.id);

                return (
                  <label
                    key={proc.id}
                    className={`code-process-check-card ${checked ? 'code-process-check-card--checked' : ''} ${isLeased ? 'code-process-check-card--disabled' : ''}`}
                  >
                    <input
                      type="checkbox"
                      checked={checked}
                      disabled={isLeased}
                      onChange={() => toggleTargetProcess(proc.id)}
                    />
                    <div className="code-process-check-info">
                      <div className="code-process-check-name">
                        {proc.name} (Port: {proc.listen_port})
                      </div>
                      <div className="code-process-check-agent code-text-xs code-color-muted">
                        Agent: {parentAgent?.name || proc.agent_id}
                        {isLeased && (
                          <span className="code-color-danger code-ml-xs">
                            [已被 {parentAgent?.lease_holder_name || '其他任务'} 租用]
                          </span>
                        )}
                      </div>
                    </div>
                  </label>
                );
              })}
              {allProcesses.length === 0 && (
                <div className="code-empty-pane code-p-xs">暂无可用业务进程，请先在 Agent 管理台挂载</div>
              )}
            </div>
          </div>

          <div className="code-form-item">
            <label className="code-form-label">说明描述:</label>
            <textarea
              className="code-textarea"
              value={startForm.description}
              onChange={(e) => setStartForm({ ...startForm, description: e.target.value })}
            />
          </div>
        </div>
      </Modal>
    </div>
  );
};
