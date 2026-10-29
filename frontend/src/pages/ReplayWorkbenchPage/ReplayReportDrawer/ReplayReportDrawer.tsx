import React, { useEffect, useState } from 'react';
import { Drawer } from '@code/common';
import { useToast } from '../../../utils/toast';
import * as replayApi from '../../../api/replay';
import type { ReplayTask, ReplayItem, ReplayItemDetailDTO } from '../../../types';
import { HeaderAssertionCard } from './HeaderAssertionCard';
import { ProtoFieldDiffTable } from './ProtoFieldDiffTable';
import { MockHitMissTimeline } from './MockHitMissTimeline';

export interface ReplayReportDrawerProps {
  open: boolean;
  onClose: () => void;
  task: ReplayTask | null;
  onRefreshTask?: () => void;
}

export const ReplayReportDrawer: React.FC<ReplayReportDrawerProps> = ({
  open,
  onClose,
  task,
  onRefreshTask,
}) => {
  const toast = useToast();
  const [items, setItems] = useState<ReplayItem[]>([]);
  const [selectedItemId, setSelectedItemId] = useState<string>('');
  const [itemDetail, setItemDetail] = useState<ReplayItemDetailDTO | null>(null);
  const [loading, setLoading] = useState(false);

  // 离线重算 Diff
  const [rediffModalOpen, setRediffModalOpen] = useState(false);
  const [extraIgnorePaths, setExtraIgnorePaths] = useState('');
  const [rediffing, setRediffing] = useState(false);

  const taskId = task?.id;

  useEffect(() => {
    if (!open || !taskId) return;
    void (async () => {
      setLoading(true);
      try {
        const res = await replayApi.listReplayItems(taskId, { page_size: 100 });
        setItems(res.items);
        if (res.items.length > 0) {
          setSelectedItemId(res.items[0].id);
        }
      } catch (err) {
        toast.error(err instanceof Error ? err.message : '加载重放明细条目失败');
      } finally {
        setLoading(false);
      }
    })();
  }, [open, taskId, toast]);

  // 加载单条重放明细详情
  useEffect(() => {
    if (!taskId || !selectedItemId) return;
    void (async () => {
      try {
        const detail = await replayApi.getReplayItemDetail(taskId, selectedItemId);
        setItemDetail(detail);
      } catch (err) {
        toast.error(err instanceof Error ? err.message : '加载重放断言详情失败');
      }
    })();
  }, [taskId, selectedItemId, toast]);

  const handleRediff = async () => {
    if (!task) return;
    const paths = extraIgnorePaths
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean);
    setRediffing(true);
    try {
      await replayApi.rediffReplayTask(task.id, paths);
      toast.success('已基于保存的实际应答包离线重算双层 Diff');
      setRediffModalOpen(false);
      onRefreshTask?.();
      const res = await replayApi.listReplayItems(task.id, { page_size: 100 });
      setItems(res.items);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '离线重算 Diff 失败');
    } finally {
      setRediffing(false);
    }
  };

  const selectedItem = items.find((i) => i.id === selectedItemId);

  return (
    <Drawer
      open={open}
      onClose={onClose}
      width="xl"
      title={task ? `重放回归断言报告 - [${task.task_no}]` : '重放报告'}
      subtitle={task ? `任务名称: ${task.name} | 综合裁决: ${task.result_status}` : undefined}
      extra={
        <button
          type="button"
          className="code-btn code-btn--secondary code-btn--sm"
          onClick={() => {
            setExtraIgnorePaths((task?.extra_ignore_paths ?? []).join(', '));
            setRediffModalOpen(true);
          }}
        >
          离线重算双层 Diff
        </button>
      }
    >
      {loading ? (
        <div className="code-tpl-loading-screen">正在加载重放断言数据...</div>
      ) : !task ? (
        <div className="code-empty-pane">未选择重放任务</div>
      ) : (
        <div className="code-replay-report-body">
          {/* 任务指标汇总 */}
          <div className="code-replay-summary-bar">
            <div className="code-meta-item">
              <span className="code-meta-label">生命周期状态:</span>
              <span className="code-badge code-badge--info">{task.lifecycle_state}</span>
            </div>
            <div className="code-meta-item">
              <span className="code-meta-label">最终全序裁决:</span>
              <span className={`code-badge ${task.result_status === 'PASS' ? 'code-badge--success' : 'code-badge--danger'}`}>
                {task.result_status}
              </span>
            </div>
            <div className="code-meta-item">
              <span className="code-meta-label">总请求数:</span>
              <span className="code-meta-val code-font-mono">{task.total_requests}</span>
            </div>
            <div className="code-meta-item">
              <span className="code-meta-label">PASS 通过:</span>
              <span className="code-meta-val code-color-success code-font-mono">{task.passed_requests}</span>
            </div>
            <div className="code-meta-item">
              <span className="code-meta-label">FAILED 突变:</span>
              <span className="code-meta-val code-color-danger code-font-mono">{task.failed_requests}</span>
            </div>
            <div className="code-meta-item">
              <span className="code-meta-label">MOCK_MISS 漏网:</span>
              <span className="code-meta-val code-color-danger code-font-mono">{task.mock_miss_requests}</span>
            </div>
          </div>

          {/* 请求条目横向切换标签 */}
          <div className="code-replay-item-tabs code-mt-sm">
            {items.map((it) => (
              <button
                key={it.id}
                type="button"
                className={`code-replay-tab-chip ${selectedItemId === it.id ? 'code-replay-tab-chip--active' : ''}`}
                onClick={() => setSelectedItemId(it.id)}
              >
                <span className={`code-badge ${it.result_status === 'PASS' ? 'code-badge--success' : 'code-badge--danger'}`}>
                  {it.result_status}
                </span>
                <span className="code-font-mono code-ml-xs">
                  {it.service_name}.{it.operation}
                </span>
              </button>
            ))}
            {items.length === 0 && <div className="code-empty-pane code-p-xs">该任务暂无执行条目</div>}
          </div>

          {/* 选定条目的双层 Diff 与 Mock 命中详情 */}
          {(itemDetail?.item || selectedItem) && (
            <div className="code-replay-item-detail code-mt-md">
              <HeaderAssertionCard item={itemDetail?.item ?? selectedItem!} />
              <div className="code-mt-sm">
                <ProtoFieldDiffTable fieldDiffs={(itemDetail?.item ?? selectedItem!).field_diffs} />
              </div>
              <div className="code-mt-sm">
                <MockHitMissTimeline
                  mockEvents={task.mock_events}
                  itemMockTrace={(itemDetail?.item ?? selectedItem!).mock_trace}
                />
              </div>
            </div>
          )}

          {/* 离线重算 Diff 浮层 */}
          {rediffModalOpen && (
            <div className="code-inline-modal code-mt-md">
              <div className="code-inline-modal__header">
                <strong>追加临时忽略字段路径离线重算 Diff</strong>
                <button
                  type="button"
                  className="code-btn-text"
                  onClick={() => setRediffModalOpen(false)}
                >
                  ✕
                </button>
              </div>
              <div className="code-form">
                <div className="code-form-item">
                  <label className="code-form-label">
                    额外忽略字段路径 (支持点分与数组通配符，如 payload.server_time, items[*].token):
                  </label>
                  <input
                    type="text"
                    className="code-input"
                    value={extraIgnorePaths}
                    onChange={(e) => setExtraIgnorePaths(e.target.value)}
                  />
                </div>
                <div className="code-inline-modal__actions">
                  <button
                    type="button"
                    className="code-btn code-btn--secondary code-btn--xs"
                    onClick={() => setRediffModalOpen(false)}
                  >
                    取消
                  </button>
                  <button
                    type="button"
                    className="code-btn code-btn--primary code-btn--xs"
                    disabled={rediffing}
                    onClick={() => void handleRediff()}
                  >
                    {rediffing ? '计算中...' : '开始重算并更新全序状态'}
                  </button>
                </div>
              </div>
            </div>
          )}
        </div>
      )}
    </Drawer>
  );
};

export default ReplayReportDrawer;
