import React, { useEffect, useMemo, useState } from 'react';
import { Drawer } from '@code/common';
import { useToast } from '../../utils/toast';
import * as replayApi from '../../api/replay';
import { getReplayClosure } from '../../api/traffic';
import type {
  TrafficRecordSummary,
  Process,
  Agent,
  ReplayTask,
  ReplayItem,
  ReplayClosurePreviewDTO,
} from '../../types';
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
  onExecute?: (targetProcessId: string, recordIds: string[]) => Promise<void>;
  task?: ReplayTask | null;
  replayItem?: ReplayItem | null;
  loadingItem?: boolean;
  executing?: boolean;
  onOpenSequenceWithTask?: (rootRecordId: string, replayTaskId: string) => void;
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
  onOpenSequenceWithTask,
}) => {
  const [selectedProcessId, setSelectedProcessId] = useState<string>('');
  const [closure, setClosure] = useState<ReplayClosurePreviewDTO | null>(null);
  const [loadingClosure, setLoadingClosure] = useState(false);
  const [includePrerequisites, setIncludePrerequisites] = useState(true);

  // 加载下游 Mock 闭包与前置依赖链
  useEffect(() => {
    if (!record || record.direction !== 'INGRESS') {
      setClosure(null);
      return;
    }
    let isMounted = true;
    setLoadingClosure(true);
    getReplayClosure(record.id)
      .then((res) => {
        if (!isMounted) return;
        setClosure(res);
      })
      .catch(() => {
        if (!isMounted) return;
        setClosure(null);
      })
      .finally(() => {
        if (isMounted) setLoadingClosure(false);
      });

    return () => {
      isMounted = false;
    };
  }, [record]);

  // 同构服务端业务进程过滤与智能推荐排序
  const candidateProcesses = useMemo(() => {
    if (!record) return [];

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

  const hasPrereqs = closure && closure.prerequisite_chain.length > 0;
  const effectiveRecordIds =
    hasPrereqs && includePrerequisites
      ? closure.recommended_record_ids
      : [record.id];

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

      {/* 事务下游专属 ScopedMock 闭包预览 */}
      <div className="code-anatomy-card code-mt-md" data-testid="scoped-mocks-card">
        <div className="code-anatomy-card__header">
          <span className="code-anatomy-card__title">专属下游 ScopedMock 闭包预览 (RecordID 隔离)</span>
          <span className="code-badge code-badge--info">
            {closure ? `${closure.scoped_egress_mocks.length} 条下游 Mock` : '加载中...'}
          </span>
        </div>
        <div className="code-anatomy-card__body">
          {loadingClosure ? (
            <div style={{ padding: '12px', textAlign: 'center', color: '#64748b' }}>
              正在检索当前入口事务因果闭包...
            </div>
          ) : closure && closure.scoped_egress_mocks.length > 0 ? (
            <div className="code-table-wrapper">
              <table className="code-table code-table--compact">
                <thead>
                  <tr>
                    <th>#Seq</th>
                    <th>下游服务 & 方法</th>
                    <th>模式</th>
                    <th>Mock 状态</th>
                  </tr>
                </thead>
                <tbody>
                  {closure.scoped_egress_mocks.map((mock) => (
                    <tr key={mock.record_id}>
                      <td className="code-font-mono">#{mock.seq_no}</td>
                      <td>
                        <div style={{ display: 'flex', flexDirection: 'column' }}>
                          <span style={{ fontWeight: 600 }}>{mock.operation}</span>
                          <span className="code-text-xs code-color-muted">{mock.service_name}</span>
                        </div>
                      </td>
                      <td>
                        <span className="code-badge code-badge--secondary">{mock.exchange_type}</span>
                      </td>
                      <td>
                        {mock.usable_as_mock ? (
                          <span style={{ color: 'var(--color-success, #10b981)', fontSize: '11px', fontWeight: 600 }}>
                            ✓ 具备成对应答
                          </span>
                        ) : (
                          <span
                            title={mock.warning}
                            style={{ color: 'var(--color-warning, #d97706)', fontSize: '11px', fontWeight: 600 }}
                          >
                            ⚠️ 无应答帧 (可能超时)
                          </span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <div style={{ color: 'var(--color-text-secondary, #64748b)', fontSize: '12px' }}>
              本事务无直接下游出站调用，重放将作为纯独立计算执行。
            </div>
          )}
        </div>
      </div>

      {/* 前置业务状态依赖诊断与连带回放勾选项 */}
      {hasPrereqs && (
        <div
          className="code-alert-banner code-alert-banner--warning code-mt-md"
          data-testid="prerequisite-chain-card"
          style={{
            backgroundColor: 'var(--color-warning-bg, #fffbeb)',
            border: '1px solid var(--color-warning-border, #fde68a)',
            padding: '12px 16px',
            borderRadius: '6px',
          }}
        >
          <div style={{ fontWeight: 600, color: 'var(--color-warning, #d97706)', marginBottom: '6px' }}>
            💡 检测到有状态业务实体依赖前置事务:
          </div>
          <div style={{ fontSize: '12px', color: 'var(--color-text-primary, #334155)', marginBottom: '8px' }}>
            本请求消费了同批次前序事务产出的实体主键。直接单独重放可能因目标库不存在对应数据引发业务失败。
          </div>
          <ul style={{ margin: '0 0 10px 20px', padding: 0, fontSize: '12px' }}>
            {closure.prerequisite_chain.map((prereq) => (
              <li key={prereq.record_id}>
                前序 #{prereq.seq_no} <code>{prereq.operation}</code> (产出 <b>{prereq.shared_entity_key}</b>)
              </li>
            ))}
          </ul>
          <label style={{ display: 'inline-flex', alignItems: 'center', gap: '8px', cursor: 'pointer', fontWeight: 600, fontSize: '13px' }}>
            <input
              type="checkbox"
              checked={includePrerequisites}
              onChange={(e) => setIncludePrerequisites(e.target.checked)}
              data-testid="checkbox-include-prerequisites"
            />
            <span>按序连带回放前置依赖事务链 (共 {closure.recommended_record_ids.length} 笔)</span>
          </label>
        </div>
      )}

      {/* 目标进程选择与注入触发器 */}
      <div className="code-anatomy-card code-mt-md">
        <div className="code-anatomy-card__header">
          <span className="code-anatomy-card__title">同构目标进程智能路由</span>
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
                      void onExecute(selectedProcessId, effectiveRecordIds);
                    }
                  }}
                  data-testid="btn-execute-single-replay"
                >
                  {executing ? '注入重放中...' : `发起闭包验证 (${effectiveRecordIds.length} 笔)`}
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
          <div className="code-section-header code-mb-sm" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <div>
              <span className="code-section-title">重放回归断言结果</span>
              <span
                className={`code-status-tag code-status-tag--${replayItem.result_status.toLowerCase()}`}
                data-testid="replay-result-status-tag"
                style={{ marginLeft: '8px' }}
              >
                {replayItem.result_status}
              </span>
            </div>
            {task && onOpenSequenceWithTask && (
              <button
                type="button"
                className="code-btn code-btn--secondary code-btn--sm"
                onClick={() => onOpenSequenceWithTask(record.id, String(task.id))}
                data-testid="btn-view-fault-in-swimlane"
                style={{ color: 'var(--color-primary, #3b82f6)', fontWeight: 600 }}
              >
                📊 在时序泳道图中查看故障定位
              </button>
            )}
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
        <div className="code-alert-banner code-alert-banner--info code-mt-md" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <span>重放任务 #{task.id} 已完成，状态: {task.lifecycle_state} (裁决: {task.result_status})</span>
          {onOpenSequenceWithTask && (
            <button
              type="button"
              className="code-btn code-btn--secondary code-btn--xs"
              onClick={() => onOpenSequenceWithTask(record.id, String(task.id))}
            >
              📊 时序泳道定位
            </button>
          )}
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
  onOpenSequenceWithTask?: (rootRecordId: string, replayTaskId: string) => void;
}

export const SinglePacketReplayDrawer: React.FC<SinglePacketReplayDrawerProps> = ({
  open,
  onClose,
  record,
  allProcesses,
  allAgents,
  currentUserId,
  onOpenSequenceWithTask,
}) => {
  const toast = useToast();
  const [executing, setExecuting] = useState(false);
  const [task, setTask] = useState<ReplayTask | null>(null);
  const [replayItem, setReplayItem] = useState<ReplayItem | null>(null);
  const [loadingItem, setLoadingItem] = useState(false);

  const handleExecuteReplay = async (targetProcessId: string, recordIds: string[]) => {
    if (!record) return;

    setExecuting(true);
    setTask(null);
    setReplayItem(null);
    try {
      const created = await replayApi.createReplayTask({
        name: `闭包验证-SNO-${record.sno}`,
        session_id: record.session_id,
        source_process_id: record.process_id,
        target_process_id: targetProcessId,
        selected_record_ids: recordIds.map((id) => parseInt(id, 10)),
      });
      setTask(created);
      toast.success('事务闭包重放任务已投递并执行完毕');

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
      toast.error(err instanceof Error ? err.message : '执行事务重放失败');
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
      title="事务闭包重放与有状态前置链回放"
      subtitle={`来源批次: ${record.session_id} | 报文流水号: ${record.sno} | 方法: ${record.service_name}/${record.operation}`}
    >
      <SinglePacketReplayContent
        record={record}
        allProcesses={allProcesses}
        allAgents={allAgents}
        currentUserId={currentUserId}
        onExecute={handleExecuteReplay}
        task={task}
        replayItem={replayItem}
        loadingItem={loadingItem}
        executing={executing}
        onOpenSequenceWithTask={onOpenSequenceWithTask}
      />
    </Drawer>
  );
};

export default SinglePacketReplayDrawer;
