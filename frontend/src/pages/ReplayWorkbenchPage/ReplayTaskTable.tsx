import React from 'react';
import type { ReplayTask } from '../../types';

export interface ReplayTaskTableProps {
  tasks: ReplayTask[];
  onSelectTask: (task: ReplayTask) => void;
  onCancelTask: (taskId: string) => void;
  isAdmin?: boolean;
}

export const ReplayTaskTable: React.FC<ReplayTaskTableProps> = ({
  tasks,
  onSelectTask,
  onCancelTask,
}) => {
  const getStatusBadgeClass = (status: ReplayTask['result_status']) => {
    switch (status) {
      case 'PASS':
        return 'code-badge--success';
      case 'FAILED':
      case 'MOCK_MISS':
      case 'ERROR':
        return 'code-badge--danger';
      case 'TIMEOUT':
      default:
        return 'code-badge--warning';
    }
  };

  return (
    <div className="code-table-wrapper">
      <table className="code-table">
        <thead>
          <tr>
            <th>任务编号 (task_no)</th>
            <th>任务名称</th>
            <th>生命周期状态</th>
            <th>断言结果 (全序全集)</th>
            <th>请求总量</th>
            <th>通过 (PASS)</th>
            <th>突变 (FAILED)</th>
            <th>Mock未命中</th>
            <th>创建时间</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          {(tasks ?? []).map((t) => {
            const isRunning = t.lifecycle_state === 'ARMED' || t.lifecycle_state === 'INJECTING';
            return (
              <tr key={t.id}>
                <td className="code-font-mono code-color-primary">{t.task_no}</td>
                <td>{t.name}</td>
                <td>
                  <span className={`code-status-tag code-status-tag--${t.lifecycle_state.toLowerCase()}`}>
                    {t.lifecycle_state}
                  </span>
                </td>
                <td>
                  <span className={`code-badge ${getStatusBadgeClass(t.result_status)}`}>
                    {t.result_status}
                  </span>
                </td>
                <td className="code-font-mono">{t.total_requests}</td>
                <td className="code-font-mono code-color-success">{t.passed_requests}</td>
                <td className="code-font-mono code-color-danger">{t.failed_requests}</td>
                <td className="code-font-mono code-color-danger">{t.mock_miss_requests}</td>
                <td className="code-font-mono code-text-xs code-color-muted">{t.created_at || '-'}</td>
                <td>
                  <div className="code-table-actions">
                    <button
                      type="button"
                      className="code-btn code-btn--secondary code-btn--xs"
                      onClick={() => onSelectTask(t)}
                    >
                      双层 Diff 报告
                    </button>
                    {isRunning && (
                      <button
                        type="button"
                        className="code-btn code-btn--danger code-btn--xs"
                        onClick={() => onCancelTask(t.id)}
                      >
                        终止
                      </button>
                    )}
                  </div>
                </td>
              </tr>
            );
          })}
          {tasks.length === 0 && (
            <tr>
              <td colSpan={10} className="code-table-empty">
                暂无重放任务记录
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
};
