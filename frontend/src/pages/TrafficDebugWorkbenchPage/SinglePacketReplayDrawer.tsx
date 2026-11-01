import React, { useEffect, useMemo, useState } from 'react';
import { Drawer } from '@code/common';
import { useToast } from '../../utils/toast';
import * as replayApi from '../../api/replay';
import type { TrafficRecordSummary, Process, Agent, ReplayTask, ReplayItem } from '../../types';
import {
  HeaderAssertionCard,
  ProtoFieldDiffTable,
  MockHitMissTimeline,
} from '../ReplayWorkbenchPage/ReplayReportDrawer';

export interface SinglePacketReplayContentProps {
  record: TrafficRecordSummary;
  allProcesses: Process[];
  allAgents: Agent[];
  currentUserId?: number;
  onExecute?: (targetProcessId: string) => Promise<void>;
  task?: ReplayTask | null;
  replayItem?: ReplayItem | null;
  loadingItem?: boolean;
  executing?: boolean;
}

export const SinglePacketReplayContent: React.FC<SinglePacketReplayContentProps> = ({
  record,
  allProcesses,
  allAgents,
  currentUserId,
  onExecute,
  task,
  replayItem,
  loadingItem = false,
  executing = false,
}) => {
  const [selectedProcessId, setSelectedProcessId] = useState<string>('');

  // 同构服务端业务进程过滤与智能推荐排序
  const candidateProcesses = useMemo(() => {
    if (!record) return [];

    // 过滤规则：
    // ① 排除 listen_port <= 0 的纯客户端进程
    // ② 必须挂载与报文目标模块同构的模块（匹配 module_id 或 module_name / module_code）
    const targetModuleName = (record.dst_module_name || record.src_module_name || '').toLowerCase();

    const isomorphicProcs = allProcesses.filter((p) => {
      if (!p.listen_port || p.listen_port <= 0) {
        return false;
      }
      const pModules = p.process_modules ?? [];
      if (pModules.length === 0) {
        return false;
      }
      if (!targetModuleName) return true;
      return pModules.some((pm) => {
        const mod = pm.module;
        if (!mod) {
          return String(pm.module_id).toLowerCase() === targetModuleName;
        }
        return (
          mod.name.toLowerCase() === targetModuleName ||
          mod.code.toLowerCase() === targetModuleName ||
          String(mod.id).toLowerCase() === targetModuleName
        );
      });
    });

    // 优先级排序：
    // ① 优先推荐本人在线空闲 PRIVATE Agent 进程
    // ② 其次推荐本人其他 PRIVATE 进程
    // ③ 最后公共 SHARED 在线进程
    return isomorphicProcs.sort((a, b) => {
      const agentA = allAgents.find((ag) => ag.id === a.agent_id);
      const agentB = allAgents.find((ag) => ag.id === b.agent_id);

      const scoreA =
        (agentA?.scope === 'PRIVATE' && String(agentA.owner_id) === String(currentUserId) ? 100 : 0) +
        (agentA?.status === 'ONLINE' ? 20 : 0) +
        (!agentA?.lease_type ? 10 : 0);

      const scoreB =
        (agentB?.scope === 'PRIVATE' && String(agentB.owner_id) === String(currentUserId) ? 100 : 0) +
        (agentB?.status === 'ONLINE' ? 20 : 0) +
        (!agentB?.lease_type ? 10 : 0);

      return scoreB - scoreA;
    });
  }, [record, allProcesses, allAgents, currentUserId]);

  useEffect(() => {
    if (candidateProcesses.length > 0) {
      setSelectedProcessId(candidateProcesses[0].id);
    } else {
      setSelectedProcessId('');
    }
  }, [candidateProcesses]);

  return (
    <div className="code-single-replay-drawer code-p-md" data-testid="single-packet-replay-drawer">
      {/* 来源报文元信息卡片 */}
      <div className="code-anatomy-card">
        <div className="code-anatomy-card__header">
          <span className="code-anatomy-card__title">待验证入站报文基线 (INGRESS)</span>
          <span className="code-badge code-badge--primary">{record.direction}</span>
        </div>
        <div className="code-anatomy-card__body">
          <div className="code-meta-grid">
            <div className="code-meta-item">
              <span className="code-meta-label">流水号 (ullSNO):</span>
              <span className="code-meta-val code-font-mono code-color-primary">{record.sno}</span>
            </div>
            <div className="code-meta-item">
              <span className="code-meta-label">服务契约:</span>
              <span className="code-meta-val code-font-mono">{record.service_name}</span>
            </div>
            <div className="code-meta-item">
              <span className="code-meta-label">接口方法:</span>
              <span className="code-meta-val code-font-mono">{record.operation}</span>
            </div>
            <div className="code-meta-item">
              <span className="code-meta-label">基线往返耗时:</span>
              <span className="code-meta-val code-font-mono">{record.latency_us} μs</span>
            </div>
          </div>
        </div>
      </div>

      {/* 目标进程选择与注入触发器 */}
      <div className="code-anatomy-card code-mt-md">
        <div className="code-anatomy-card__header">
          <span className="code-anatomy-card__title">同构目标进程智能路由 (排除纯客户端与异构进程)</span>
        </div>
        <div className="code-anatomy-card__body">
          {candidateProcesses.length === 0 ? (
            <div className="code-alert-banner code-alert-banner--warning">
              未检测到挂载了同构模块且 <code>listen_port &gt; 0</code> 的可用服务端业务进程。
            </div>
          ) : (
            <div className="code-flex code-flex-gap-md code-items-center">
              <div className="code-flex-1">
                <label className="code-field-label">选择注入验证的目标进程:</label>
                <select
                  className="code-select"
                  value={selectedProcessId}
                  onChange={(e) => setSelectedProcessId(e.target.value)}
                  data-testid="select-target-process"
                >
                  {candidateProcesses.map((p) => {
                    const agent = allAgents.find((a) => a.id === p.agent_id);
                    const isPrivateUser =
                      agent?.scope === 'PRIVATE' && String(agent.owner_id) === String(currentUserId);
                    return (
                      <option key={p.id} value={p.id}>
                        {p.name} (端口: {p.listen_port})
                        {p.group_name ? ` [组: ${p.group_name}]` : ''}
                        {isPrivateUser ? ' ★ 推荐个人调试' : ''}
                        {agent ? ` [Agent: ${agent.name}]` : ''}
                      </option>
                    );
                  })}
                </select>
              </div>
              <div style={{ paddingTop: '1.25rem' }}>
                <button
                  type="button"
                  className="code-btn code-btn--primary"
                  disabled={executing || !selectedProcessId}
                  onClick={() => {
                    if (onExecute && selectedProcessId) {
                      void onExecute(selectedProcessId);
                    }
                  }}
                  data-testid="btn-execute-single-replay"
                >
                  {executing ? '注入重放中...' : '发起单包验证'}
                </button>
              </div>
            </div>
          )}
        </div>
      </div>

      {/* 原地渲染双层 Diff 与断言结果 */}
      {loadingItem ? (
        <div className="code-tpl-loading-screen code-mt-md">
          正在拉取重放全序裁决与字段级 Diff 结果...
        </div>
      ) : replayItem ? (
        <div className="code-mt-md" data-testid="single-replay-result-section">
          <div className="code-section-header code-mb-sm">
            <span className="code-section-title">重放回归断言结果</span>
            <span
              className={`code-status-tag code-status-tag--${replayItem.result_status.toLowerCase()}`}
              data-testid="replay-result-status-tag"
            >
              {replayItem.result_status}
            </span>
          </div>

          <HeaderAssertionCard item={replayItem} />

          <div className="code-mt-sm">
            <ProtoFieldDiffTable fieldDiffs={replayItem.field_diffs} />
          </div>

          <div className="code-mt-sm">
            <MockHitMissTimeline itemMockTrace={replayItem.mock_trace} mockEvents={task?.mock_events} />
          </div>
        </div>
      ) : task ? (
        <div className="code-alert-banner code-alert-banner--info code-mt-md">
          重放任务 #{task.id} 已完成，状态: {task.lifecycle_state} (裁决: {task.result_status})
        </div>
      ) : null}
    </div>
  );
};

export interface SinglePacketReplayDrawerProps {
  open: boolean;
  onClose: () => void;
  record: TrafficRecordSummary | null;
  allProcesses: Process[];
  allAgents: Agent[];
  currentUserId?: number;
}

export const SinglePacketReplayDrawer: React.FC<SinglePacketReplayDrawerProps> = ({
  open,
  onClose,
  record,
  allProcesses,
  allAgents,
  currentUserId,
}) => {
  const toast = useToast();
  const [executing, setExecuting] = useState(false);
  const [task, setTask] = useState<ReplayTask | null>(null);
  const [replayItem, setReplayItem] = useState<ReplayItem | null>(null);
  const [loadingItem, setLoadingItem] = useState(false);

  const handleExecuteSingleReplay = async (targetProcessId: string) => {
    if (!record) return;

    setExecuting(true);
    setTask(null);
    setReplayItem(null);
    try {
      const created = await replayApi.createReplayTask({
        name: `单包验证-SNO-${record.sno}`,
        session_id: record.session_id,
        source_process_id: record.process_id,
        target_process_id: targetProcessId,
        selected_record_ids: [record.id],
      });
      setTask(created);
      toast.success('单包重放验证任务已投递并执行完毕');

      // 加载单包重放比对条目
      setLoadingItem(true);
      try {
        const itemsRes = await replayApi.listReplayItems(created.id, { page_size: 1 });
        if (itemsRes.items && itemsRes.items.length > 0) {
          setReplayItem(itemsRes.items[0]);
        }
      } catch (err) {
        toast.error(err instanceof Error ? err.message : '获取重放比对明细失败');
      } finally {
        setLoadingItem(false);
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '执行单包重放失败');
    } finally {
      setExecuting(false);
    }
  };

  if (!record) return null;

  return (
    <Drawer
      open={open}
      onClose={onClose}
      width="xl"
      title="单包快速重放验证与原地双层 Diff"
      subtitle={`来源批次: ${record.session_id} | 报文流水号: ${record.sno} | 方法: ${record.service_name}/${record.operation}`}
    >
      <SinglePacketReplayContent
        record={record}
        allProcesses={allProcesses}
        allAgents={allAgents}
        currentUserId={currentUserId}
        onExecute={handleExecuteSingleReplay}
        task={task}
        replayItem={replayItem}
        loadingItem={loadingItem}
        executing={executing}
      />
    </Drawer>
  );
};

export default SinglePacketReplayDrawer;
