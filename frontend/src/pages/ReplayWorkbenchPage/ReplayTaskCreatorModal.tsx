import React, { useEffect, useState } from 'react';
import { Modal } from '@code/common';
import { useToast } from '../../utils/toast';
import * as replayApi from '../../api/replay';
import * as trafficApi from '../../api/traffic';
import type { RecordingSession, TrafficRecordSummary, Process } from '../../types';

export interface ReplayTaskCreatorModalProps {
  open: boolean;
  onClose: () => void;
  sessions: RecordingSession[];
  processes: Process[];
  onSuccess: () => void;
  defaultSessionId?: string;
}

export const ReplayTaskCreatorModal: React.FC<ReplayTaskCreatorModalProps> = ({
  open,
  onClose,
  sessions,
  processes,
  onSuccess,
  defaultSessionId,
}) => {
  const toast = useToast();
  const [name, setName] = useState('');
  const [selectedSessionId, setSelectedSessionId] = useState('');
  const [selectedProcessId, setSelectedProcessId] = useState('');
  const [extraIgnorePaths, setExtraIgnorePaths] = useState('');
  const [timeoutMs, setTimeoutMs] = useState(5000);

  const [ingressRecords, setIngressRecords] = useState<TrafficRecordSummary[]>([]);
  const [selectedRecordIds, setSelectedRecordIds] = useState<string[]>([]);
  const [loadingRecords, setLoadingRecords] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (defaultSessionId) {
      setSelectedSessionId(defaultSessionId);
    } else if (sessions.length > 0) {
      setSelectedSessionId((prev) => prev || sessions[0].id);
    }
    if (processes.length > 0) {
      setSelectedProcessId((prev) => prev || processes[0].id);
    }
  }, [sessions, processes, defaultSessionId]);

  // 当录制批次或目标进程变更时，加载其 INGRESS 且可重放的流量记录
  useEffect(() => {
    if (!selectedSessionId || !open) return;
    void (async () => {
      setLoadingRecords(true);
      try {
        const res = await trafficApi.listTrafficRecords({
          session_id: selectedSessionId,
          process_id: selectedProcessId || undefined,
          direction: 'INGRESS',
          is_replayable: true,
          page_size: 50,
        });
        const items = res.items ?? [];
        setIngressRecords(items);
        // 默认勾选前 5 条
        setSelectedRecordIds(items.slice(0, 5).map((r) => r.id));
      } catch (err) {
        toast.error(err instanceof Error ? err.message : '加载批次可重放记录失败');
      } finally {
        setLoadingRecords(false);
      }
    })();
  }, [selectedSessionId, selectedProcessId, open, toast]);

  const toggleRecord = (recId: string) => {
    setSelectedRecordIds((prev) => {
      const set = new Set(prev);
      if (set.has(recId)) set.delete(recId);
      else set.add(recId);
      return Array.from(set);
    });
  };

  const handleSelectAll = () => {
    if (selectedRecordIds.length === (ingressRecords ?? []).length) {
      setSelectedRecordIds([]);
    } else {
      setSelectedRecordIds((ingressRecords ?? []).map((r) => r.id));
    }
  };

  const handleSubmit = async () => {
    if (!name.trim()) {
      toast.error('请输入重放任务名称');
      return;
    }
    if (!selectedSessionId) {
      toast.error('请选择来源录制素材');
      return;
    }
    if (!selectedProcessId) {
      toast.error('请选择同构目标业务进程');
      return;
    }
    if (selectedRecordIds.length === 0) {
      toast.error('请至少勾选一条待重放的 INGRESS 请求');
      return;
    }

    const firstRec = ingressRecords.find((r) => r.id === selectedRecordIds[0]);
    const sourceProcId = firstRec?.process_id || selectedProcessId;

    const paths = extraIgnorePaths
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean);

    const targetProc = processes.find((p) => String(p.id) === String(selectedProcessId));
    if (targetProc?.probe_status === 'WAITING_PROC' || targetProc?.probe_status === 'NEED_RECONNECT') {
      toast.warning(`目标进程探针处于未就绪状态 (${targetProc.probe_status})，请先确保宿主机进程启动并建立网络监听`);
    }

    setSubmitting(true);
    try {
      await replayApi.createReplayTask({
        name,
        session_id: selectedSessionId,
        source_process_id: sourceProcId,
        target_process_id: selectedProcessId,
        selected_record_ids: selectedRecordIds,
        extra_ignore_paths: paths,
        timeout_ms: timeoutMs,
      });
      toast.success('同构重放任务已创建并进入调度队列');
      onSuccess();
      onClose();
    } catch (err: unknown) {
      const errMsg = err instanceof Error ? err.message : String(err);
      if (errMsg.includes('409') || errMsg.includes('WAITING_PROC') || errMsg.includes('NEED_RECONNECT')) {
        toast.error(`启动被拒绝 (409): 目标业务进程探针未就绪 (${errMsg})，请检查宿主机进程与网络连接`);
      } else {
        toast.error(errMsg || '创建重放任务失败');
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="创建跨 Agent 同构离线重放任务"
      width="lg"
      footer={
        <div className="code-modal-footer">
          <button type="button" className="code-btn code-btn--secondary" onClick={onClose} disabled={submitting}>
            取消
          </button>
          <button type="button" className="code-btn code-btn--primary" onClick={() => void handleSubmit()} disabled={submitting}>
            {submitting ? '排他加锁下发中...' : '启动同构重放与双层断言'}
          </button>
        </div>
      }
    >
      <div className="code-form">
        <div className="code-form-item">
          <label className="code-form-label">任务名称:</label>
          <input
            type="text"
            className="code-input"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="如 订单中枢回归测试_01"
          />
        </div>

        <div className="code-form-grid-2">
          <div className="code-form-item">
            <label className="code-form-label">来源录制素材 (session_id):</label>
            <select
              className="code-select"
              value={selectedSessionId}
              onChange={(e) => setSelectedSessionId(e.target.value)}
            >
              {(sessions ?? []).map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name} ({s.session_no}) [{s.scope}]
                </option>
              ))}
            </select>
          </div>

          <div className="code-form-item">
            <label className="code-form-label">重放同构目标业务进程 (target_process_id):</label>
            <select
              className="code-select"
              value={selectedProcessId}
              onChange={(e) => setSelectedProcessId(e.target.value)}
            >
              {(processes ?? []).map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} (Port: {p.listen_port})
                </option>
              ))}
            </select>
          </div>
        </div>

        {/* 勾选待重放的入站请求列表 */}
        <div className="code-form-item">
          <div className="code-form-label-row">
            <label className="code-form-label">
              勾选待重放的 INGRESS 请求 (已过滤截断帧与非重放帧):
            </label>
            <button type="button" className="code-btn-text code-text-xs" onClick={handleSelectAll}>
              {selectedRecordIds.length === ingressRecords.length ? '取消全选' : '全部全选'}
            </button>
          </div>

          {loadingRecords ? (
            <div className="code-tpl-loading-screen">正在加载入站请求...</div>
          ) : ingressRecords.length === 0 ? (
            <div className="code-empty-pane code-p-sm">当前批次无可用 INGRESS 请求</div>
          ) : (
            <div className="code-table-wrapper code-scroll-area-sm">
              <table className="code-table code-table--compact">
                <thead>
                  <tr>
                    <th style={{ width: 40 }}>勾选</th>
                    <th>流水号 (SNO)</th>
                    <th>服务契约</th>
                    <th>调用方法</th>
                    <th>原始耗时</th>
                    <th>状态码</th>
                  </tr>
                </thead>
                <tbody>
                  {(ingressRecords ?? []).map((r) => {
                    const checked = selectedRecordIds.includes(r.id);
                    return (
                      <tr key={r.id}>
                        <td>
                          <input
                            type="checkbox"
                            checked={checked}
                            onChange={() => toggleRecord(r.id)}
                          />
                        </td>
                        <td className="code-font-mono">{r.sno}</td>
                        <td className="code-font-mono code-color-primary">{r.service_name}</td>
                        <td className="code-font-mono">{r.operation}</td>
                        <td className="code-font-mono">{r.latency_us} μs</td>
                        <td>
                          <span className={`code-font-mono ${r.status_code === 0 ? 'code-color-success' : 'code-color-danger'}`}>
                            {r.status_code}
                          </span>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>

        <div className="code-form-grid-2">
          <div className="code-form-item">
            <label className="code-form-label">临时额外忽略字段路径 (diff_ignore_paths):</label>
            <input
              type="text"
              className="code-input"
              value={extraIgnorePaths}
              onChange={(e) => setExtraIgnorePaths(e.target.value)}
              placeholder="如 header.client_time, items[*].id"
            />
          </div>
          <div className="code-form-item">
            <label className="code-form-label">单请求超时时间 (ms):</label>
            <input
              type="number"
              className="code-input"
              value={timeoutMs}
              onChange={(e) => setTimeoutMs(Number(e.target.value))}
            />
          </div>
        </div>
      </div>
    </Modal>
  );
};
