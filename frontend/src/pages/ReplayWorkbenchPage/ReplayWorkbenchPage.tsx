import React, { useCallback, useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Pagination, usePaginationState } from '@code/common';
import { useToast } from '../../utils/toast';
import { useModuleUser } from '../../context/ModuleUserContext';
import * as replayApi from '../../api/replay';
import * as trafficApi from '../../api/traffic';
import * as agentApi from '../../api/agent';
import type { ReplayTask, RecordingSession, Process } from '../../types';
import { ReplayTaskTable } from './ReplayTaskTable';
import { ReplayTaskCreatorModal } from './ReplayTaskCreatorModal';
import { ReplayReportDrawer } from './ReplayReportDrawer';

export const ReplayWorkbenchPage: React.FC = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const { user } = useModuleUser();
  const toast = useToast();
  const isAdmin = Boolean(user?.is_module_admin || user?.is_super_admin);

  const { page, pageSize, setPage, setPageSize } = usePaginationState({
    defaultPageSize: 25,
  });

  const urlStatus = searchParams.get('status') || '';
  const urlSessionId = searchParams.get('session_id') || '';

  const [tasks, setTasks] = useState<ReplayTask[]>([]);
  const [totalTasks, setTotalTasks] = useState(0);
  const [loading, setLoading] = useState(false);

  const [sessions, setSessions] = useState<RecordingSession[]>([]);
  const [processes, setProcesses] = useState<Process[]>([]);

  // 抽屉与弹窗状态
  const [creatorModalOpen, setCreatorModalOpen] = useState(false);
  const [reportDrawerOpen, setReportDrawerOpen] = useState(false);
  const [selectedTask, setSelectedTask] = useState<ReplayTask | null>(null);

  const loadTasks = useCallback(async () => {
    setLoading(true);
    try {
      const res = await replayApi.listReplayTasks({
        session_id: urlSessionId || undefined,
        result_status: urlStatus || undefined,
        page,
        page_size: pageSize,
      });
      setTasks(res.items);
      setTotalTasks(res.total);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '加载重放任务列表失败');
    } finally {
      setLoading(false);
    }
  }, [urlSessionId, urlStatus, page, pageSize, toast]);

  useEffect(() => {
    void loadTasks();
  }, [loadTasks]);

  // 加载可重放素材批次与目标进程
  useEffect(() => {
    void (async () => {
      try {
        const [sessRes, procRes] = await Promise.all([
          trafficApi.listRecordingSessions({ page_size: 100 }),
          agentApi.listProcesses({ page_size: 100 }),
        ]);
        setSessions(sessRes.items);
        setProcesses(procRes.items);
      } catch {
        // 静默
      }
    })();
  }, []);

  const handleCancelTask = async (taskId: string) => {
    try {
      await replayApi.cancelReplayTask(taskId);
      toast.success('重放任务已终止并释放排他租约');
      void loadTasks();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '终止重放任务失败');
    }
  };

  const handleStatusFilterChange = (status: string) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      if (status) next.set('status', status);
      else next.delete('status');
      next.set('page', '1');
      return next;
    });
  };

  return (
    <div className="code-workbench-page code-replay-workbench" data-testid="replay-workbench">
      <div className="code-workbench-header">
        <div className="code-workbench-title-group">
          <h2 className="code-workbench-title">跨 Agent 重放与回归断言工作台</h2>
          <span className="code-workbench-subtitle">
            选用公共或个人录制素材，向同构目标进程注入流量，依托 Agent 本地自愈与主控双层 Diff 检出业务突变与 Mock 漏网
          </span>
        </div>

        <div className="code-header-actions">
          <button
            type="button"
            className="code-btn code-btn--primary"
            onClick={() => setCreatorModalOpen(true)}
          >
            + 创建同构重放任务
          </button>
        </div>
      </div>

      {/* 筛选过滤条 */}
      <div className="code-pane-header code-mt-sm">
        <div className="code-filter-row">
          <label className="code-field-label">全序裁决状态:</label>
          <select
            className="code-select code-select--compact"
            value={urlStatus}
            onChange={(e) => handleStatusFilterChange(e.target.value)}
          >
            <option value="">全部状态 (ALL)</option>
            <option value="PASS">PASS (完全通过)</option>
            <option value="FAILED">FAILED (业务字段/状态码突变)</option>
            <option value="MOCK_MISS">MOCK_MISS (下游 Mock 漏网)</option>
            <option value="TIMEOUT">TIMEOUT (执行超时)</option>
            <option value="ERROR">ERROR (异常报错)</option>
          </select>
        </div>
      </div>

      {/* 任务主表 */}
      <div className="code-mt-sm">
        {loading ? (
          <div className="code-tpl-loading-screen">正在加载重放任务...</div>
        ) : (
          <ReplayTaskTable
            tasks={tasks}
            onSelectTask={(t) => {
              setSelectedTask(t);
              setReportDrawerOpen(true);
            }}
            onCancelTask={(id) => void handleCancelTask(id)}
            isAdmin={isAdmin}
          />
        )}

        <div className="code-mt-md">
          <Pagination
            totalItems={totalTasks}
            page={page}
            pageSize={pageSize}
            onPageChange={setPage}
            onPageSizeChange={setPageSize}
          />
        </div>
      </div>

      {/* 创建重放任务弹窗 */}
      <ReplayTaskCreatorModal
        open={creatorModalOpen}
        onClose={() => setCreatorModalOpen(false)}
        sessions={sessions}
        processes={processes}
        onSuccess={() => void loadTasks()}
      />

      {/* 双层 Diff 回归报告抽屉 */}
      <ReplayReportDrawer
        open={reportDrawerOpen}
        onClose={() => setReportDrawerOpen(false)}
        task={selectedTask}
        onRefreshTask={() => void loadTasks()}
      />
    </div>
  );
};

export default ReplayWorkbenchPage;
