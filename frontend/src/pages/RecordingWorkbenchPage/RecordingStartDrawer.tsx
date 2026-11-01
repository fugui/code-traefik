import React, { useState, useEffect, useMemo, useCallback } from 'react';
import { Drawer } from '@code/common';
import { Search, ChevronDown, ChevronRight, AlertCircle, Sparkles } from 'lucide-react';
import { useToast } from '../../utils/toast';
import * as trafficApi from '../../api/traffic';
import type { RecordingSession, Process, Agent } from '../../types';
import './recording_drawer.css';

export interface RecordingStartDrawerProps {
  open: boolean;
  onClose: () => void;
  onSuccess: (session: RecordingSession) => void;
  allProcesses: Process[];
  allAgents: Agent[];
  isAdmin: boolean;
}

export interface RecordingStartContentProps {
  allProcesses: Process[];
  allAgents: Agent[];
  isAdmin: boolean;
  onClose: () => void;
  onSuccess: (session: RecordingSession) => void;
  onStateChange?: (state: {
    targetProcessIds: string[];
    currentAgent?: Agent;
    submitting: boolean;
    canSubmit: boolean;
    handleStart: () => Promise<void>;
  }) => void;
}

export interface GroupedProcesses {
  groupName: string;
  processes: Process[];
}

export const RecordingStartContent: React.FC<RecordingStartContentProps> = ({
  allProcesses,
  allAgents,
  isAdmin,
  onClose,
  onSuccess,
  onStateChange,
}) => {
  const toast = useToast();

  // 1. 基础信息状态
  const [name, setName] = useState('');
  const [scope, setScope] = useState<'SHARED' | 'PRIVATE'>('PRIVATE');

  // 2. 目标 Agent 状态 (初始化惰性选取首个在线空闲 Agent)
  const [selectedAgentId, setSelectedAgentId] = useState<string>(() => {
    if (!allAgents || allAgents.length === 0) return '';
    const ideal = allAgents.find((a) => a.status === 'ONLINE' && !a.lease_type);
    if (ideal) return ideal.id;
    const online = allAgents.find((a) => a.status === 'ONLINE');
    if (online) return online.id;
    return allAgents[0].id;
  });

  // 3. 业务进程树状态
  const [targetProcessIds, setTargetProcessIds] = useState<string[]>([]);
  const [searchQuery, setSearchQuery] = useState('');
  const [collapsedGroups, setCollapsedGroups] = useState<Record<string, boolean>>({});

  // 4. 阈值与高级配置
  const [maxDurationSec, setMaxDurationSec] = useState(1800);
  const [maxFrames, setMaxFrames] = useState(100000);
  const [description, setDescription] = useState('');
  const [submitting, setSubmitting] = useState(false);

  // 智能默认选中首个在线且空闲的 Agent
  useEffect(() => {
    if (allAgents.length === 0) {
      setSelectedAgentId('');
      return;
    }

    if (selectedAgentId && allAgents.some((a) => a.id === selectedAgentId)) {
      return;
    }

    const ideal = allAgents.find((a) => a.status === 'ONLINE' && !a.lease_type);
    if (ideal) {
      setSelectedAgentId(ideal.id);
      return;
    }

    const online = allAgents.find((a) => a.status === 'ONLINE');
    if (online) {
      setSelectedAgentId(online.id);
      return;
    }

    setSelectedAgentId(allAgents[0].id);
  }, [allAgents, selectedAgentId]);

  // 当切换选中的 Agent 时，清空选中的进程 ID，避免跨 Agent 混选
  const handleAgentChange = (newAgentId: string) => {
    setSelectedAgentId(newAgentId);
    setTargetProcessIds([]);
  };

  const currentAgent = useMemo(() => {
    return allAgents.find((a) => a.id === selectedAgentId);
  }, [allAgents, selectedAgentId]);

  const isAgentOffline = currentAgent?.status === 'OFFLINE';
  const isAgentLeased = Boolean(currentAgent?.lease_type);

  const currentAgentProcesses = useMemo(() => {
    if (!selectedAgentId) return [];
    return allProcesses.filter((p) => p.agent_id === selectedAgentId);
  }, [allProcesses, selectedAgentId]);

  const groupedProcesses = useMemo(() => {
    const map = new Map<string, Process[]>();
    const query = searchQuery.trim().toLowerCase();

    currentAgentProcesses.forEach((proc) => {
      if (query) {
        const matchName = proc.name.toLowerCase().includes(query);
        const matchPort = String(proc.listen_port).includes(query);
        const matchGroup = (proc.group_name || '').toLowerCase().includes(query);
        if (!matchName && !matchPort && !matchGroup) {
          return;
        }
      }

      const grp = proc.group_name && proc.group_name.trim() ? proc.group_name.trim() : '默认进程组';
      if (!map.has(grp)) {
        map.set(grp, []);
      }
      map.get(grp)!.push(proc);
    });

    const result: GroupedProcesses[] = [];
    map.forEach((processes, groupName) => {
      result.push({ groupName, processes });
    });
    return result;
  }, [currentAgentProcesses, searchQuery]);

  const handleAutoGenerateName = () => {
    const now = new Date();
    const pad = (n: number) => String(n).padStart(2, '0');
    const timeStr = `${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}-${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`;
    const agentName = currentAgent ? currentAgent.name.replace(/[^a-zA-Z0-9_-]/g, '') : 'traffic';
    setName(`rec-${agentName}-${timeStr}`);
  };

  const toggleProcess = (procId: string) => {
    setTargetProcessIds((prev) => {
      const set = new Set(prev);
      if (set.has(procId)) {
        set.delete(procId);
      } else {
        set.add(procId);
      }
      return Array.from(set);
    });
  };

  const toggleGroup = (group: GroupedProcesses) => {
    const groupProcIds = group.processes.map((p) => p.id);
    const allSelected = groupProcIds.every((id) => targetProcessIds.includes(id));

    setTargetProcessIds((prev) => {
      const set = new Set(prev);
      if (allSelected) {
        groupProcIds.forEach((id) => set.delete(id));
      } else {
        groupProcIds.forEach((id) => set.add(id));
      }
      return Array.from(set);
    });
  };

  const toggleGroupCollapse = (groupName: string) => {
    setCollapsedGroups((prev) => ({
      ...prev,
      [groupName]: !prev[groupName],
    }));
  };

  const handleSelectAll = () => {
    const allIds = currentAgentProcesses.map((p) => p.id);
    setTargetProcessIds(allIds);
  };

  const handleClearAll = () => {
    setTargetProcessIds([]);
  };

  const handleStart = useCallback(async () => {
    if (!name.trim()) {
      toast.error('请输入录制批次名称');
      return;
    }
    if (!selectedAgentId) {
      toast.error('请选择有效的目标宿主机 Agent');
      return;
    }
    if (isAgentOffline) {
      toast.error('目标 Agent 处于离线状态，无法下发 eBPF 探针租约');
      return;
    }
    if (isAgentLeased) {
      toast.error('目标 Agent 当前已有排他租约占用中，无法发起新录制');
      return;
    }
    if (targetProcessIds.length === 0) {
      toast.error('请至少在业务进程树中勾选一个监控目标进程');
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
  }, [
    name,
    selectedAgentId,
    isAgentOffline,
    isAgentLeased,
    targetProcessIds,
    scope,
    maxDurationSec,
    maxFrames,
    description,
    toast,
    onSuccess,
    onClose,
  ]);

  const canSubmit =
    !submitting &&
    !isAgentOffline &&
    !isAgentLeased &&
    targetProcessIds.length > 0 &&
    Boolean(name.trim());

  useEffect(() => {
    onStateChange?.({
      targetProcessIds,
      currentAgent,
      submitting,
      canSubmit,
      handleStart,
    });
  }, [targetProcessIds, currentAgent, submitting, canSubmit, handleStart, onStateChange]);

  return (
    <div className="code-recording-start-drawer" data-testid="recording-start-drawer">
      {/* 1. 基础配置区 */}
      <section className="code-recording-drawer-section">
        <div className="code-recording-drawer-section-title">1. 基础配置</div>

        <div className="code-form-item">
          <label className="code-form-label code-required">批次名称</label>
          <div className="code-input-with-action">
            <input
              type="text"
              className="code-input"
              placeholder="例如：20261101-订单中心核心结算流量录制"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
            <button
              type="button"
              className="code-btn code-btn--secondary"
              onClick={handleAutoGenerateName}
              title="根据当前时间与 Agent 自动填充批次名称"
            >
              <Sparkles size={14} className="code-mr-xs" />
              自动生成名称
            </button>
          </div>
        </div>

        <div className="code-form-item">
          <label className="code-form-label code-required">素材归属作用域</label>
          <div className="code-scope-cards-grid">
            <div
              className={`code-scope-card ${scope === 'PRIVATE' ? 'code-scope-card--active' : ''}`}
              onClick={() => setScope('PRIVATE')}
              data-testid="scope-card-private"
            >
              <div className="code-scope-card-header">
                <input
                  type="radio"
                  name="rec_drawer_scope"
                  value="PRIVATE"
                  checked={scope === 'PRIVATE'}
                  onChange={() => setScope('PRIVATE')}
                />
                <span className="code-scope-card-title">个人私有素材</span>
                <span className="code-badge code-badge--info">PRIVATE</span>
              </div>
              <div className="code-scope-card-desc">
                仅本人可见与调试，用后可随时清理，不影响团队回归基线
              </div>
            </div>

            {isAdmin && (
              <div
                className={`code-scope-card ${scope === 'SHARED' ? 'code-scope-card--active' : ''}`}
                onClick={() => setScope('SHARED')}
                data-testid="scope-card-shared"
              >
                <div className="code-scope-card-header">
                  <input
                    type="radio"
                    name="rec_drawer_scope"
                    value="SHARED"
                    checked={scope === 'SHARED'}
                    onChange={() => setScope('SHARED')}
                  />
                  <span className="code-scope-card-title">团队公共基线</span>
                  <span className="code-badge code-badge--success">SHARED</span>
                </div>
                <div className="code-scope-card-desc">
                  全员可见，可作为架构一致性比对与自动化回归基线
                </div>
              </div>
            )}
          </div>
        </div>
      </section>

      {/* 2. 目标运行环境与业务进程树 */}
      <section className="code-recording-drawer-section">
        <div className="code-recording-drawer-section-title">2. 目标运行环境与业务进程树</div>

        <div className="code-form-item">
          <label className="code-form-label code-required">目标宿主机 Agent</label>
          <div className="code-agent-selector-wrapper">
            <select
              className="code-select code-agent-select-field"
              value={selectedAgentId}
              onChange={(e) => handleAgentChange(e.target.value)}
              data-testid="select-target-agent"
            >
              {allAgents.length === 0 ? (
                <option value="">暂无可用 Agent</option>
              ) : (
                allAgents.map((agt) => {
                  const isOnline = agt.status === 'ONLINE';
                  const isBusy = Boolean(agt.lease_type);
                  let statusLabel = isOnline ? '🟢 在线 / 空闲' : '🔴 离线';
                  if (isOnline && isBusy) {
                    statusLabel = `🔒 占用中 (${agt.lease_type})`;
                  }
                  return (
                    <option key={agt.id} value={agt.id}>
                      {agt.name} ({agt.endpoint || '无端点'}) [{statusLabel}] [{agt.scope}]
                    </option>
                  );
                })
              )}
            </select>

            {/* 异常状态告警门禁 */}
            {isAgentOffline && (
              <div className="code-agent-alert">
                <AlertCircle size={16} />
                <span>当前选中的 Agent 处于离线状态，无法下发 eBPF 探针租约，请先确保宿主机 Agent 已启动。</span>
              </div>
            )}
            {isAgentLeased && (
              <div className="code-agent-alert code-agent-alert--warning">
                <AlertCircle size={16} />
                <span>
                  当前 Agent 处于 <strong>{currentAgent?.lease_type}</strong> 排他租约中
                  {currentAgent?.lease_holder_name ? ` (租用人: ${currentAgent.lease_holder_name})` : ''}，暂不可发起并发录制。
                </span>
              </div>
            )}
          </div>
        </div>

        <div className="code-form-item">
          <div className="code-form-label-row code-flex-between">
            <label className="code-form-label code-required">
              监控目标业务进程 (勾选即下发 eBPF 探针租约)
            </label>
            <span className="code-text-sm code-text-muted">
              当前主机共 {currentAgentProcesses.length} 个进程
            </span>
          </div>

          <div
            className={`code-process-tree-box ${isAgentOffline || isAgentLeased ? 'code-process-tree-box--disabled' : ''}`}
          >
            {/* 树工具栏 */}
            <div className="code-process-tree-toolbar">
              <div className="code-process-tree-search">
                <Search size={14} className="code-process-tree-search-icon" />
                <input
                  type="text"
                  className="code-process-tree-search-input"
                  placeholder="过滤进程名、端口或组名..."
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                />
              </div>
              <div className="code-process-tree-actions">
                <button
                  type="button"
                  className="code-process-tree-btn"
                  onClick={handleSelectAll}
                  disabled={currentAgentProcesses.length === 0}
                >
                  全选
                </button>
                <button
                  type="button"
                  className="code-process-tree-btn"
                  onClick={handleClearAll}
                  disabled={targetProcessIds.length === 0}
                >
                  清空
                </button>
              </div>
            </div>

            {/* 树主体 */}
            <div className="code-process-tree-body">
              {currentAgentProcesses.length === 0 ? (
                <div className="code-process-tree-empty">该 Agent 下暂无纳管的业务进程</div>
              ) : groupedProcesses.length === 0 ? (
                <div className="code-process-tree-empty">未匹配到符合过滤条件的进程</div>
              ) : (
                groupedProcesses.map((group) => {
                  const isCollapsed = Boolean(collapsedGroups[group.groupName]) && !searchQuery;
                  const groupProcIds = group.processes.map((p) => p.id);
                  const selectedCount = groupProcIds.filter((id) =>
                    targetProcessIds.includes(id)
                  ).length;
                  const isAllSelected =
                    selectedCount === groupProcIds.length && groupProcIds.length > 0;
                  const isIndeterminate =
                    selectedCount > 0 && selectedCount < groupProcIds.length;

                  return (
                    <div key={group.groupName} className="code-process-group-item">
                      {/* 组头 */}
                      <div
                        className="code-process-group-header"
                        onClick={() => toggleGroupCollapse(group.groupName)}
                      >
                        <div className="code-process-group-left">
                          <span className="code-process-group-toggle">
                            {isCollapsed ? <ChevronRight size={14} /> : <ChevronDown size={14} />}
                          </span>
                          <input
                            type="checkbox"
                            checked={isAllSelected}
                            ref={(el) => {
                              if (el) el.indeterminate = isIndeterminate;
                            }}
                            onChange={(e) => {
                              e.stopPropagation();
                              toggleGroup(group);
                            }}
                            onClick={(e) => e.stopPropagation()}
                          />
                          <span className="code-process-group-name">{group.groupName}</span>
                        </div>
                        <span className="code-process-group-count">
                          ({selectedCount}/{group.processes.length})
                        </span>
                      </div>

                      {/* 进程子项 */}
                      {!isCollapsed && (
                        <div className="code-process-leaf-list">
                          {group.processes.map((proc) => {
                            const checked = targetProcessIds.includes(proc.id);
                            const moduleBinding = proc.process_modules?.[0];
                            const isClientOnly = proc.listen_port === 0;

                            return (
                              <div
                                key={proc.id}
                                className="code-process-leaf-item"
                                onClick={() => toggleProcess(proc.id)}
                              >
                                <div className="code-process-leaf-left">
                                  <input
                                    type="checkbox"
                                    checked={checked}
                                    onChange={() => toggleProcess(proc.id)}
                                    onClick={(e) => e.stopPropagation()}
                                  />
                                  <span className="code-process-leaf-name">{proc.name}</span>
                                  {isClientOnly ? (
                                    <span className="code-process-port-badge code-process-port-badge--client">
                                      纯客户端
                                    </span>
                                  ) : (
                                    <span className="code-process-port-badge">
                                      端口: {proc.listen_port}
                                    </span>
                                  )}
                                </div>
                                <div className="code-process-leaf-right">
                                  {moduleBinding?.module && (
                                    <span
                                      className="code-process-module-tag"
                                      title={`承载模块: ${moduleBinding.module.name} (${moduleBinding.module.code})`}
                                    >
                                      模块: {moduleBinding.module.name}
                                    </span>
                                  )}
                                </div>
                              </div>
                            );
                          })}
                        </div>
                      )}
                    </div>
                  );
                })
              )}
            </div>
          </div>
        </div>
      </section>

      {/* 3. 录制阈值与备注区 */}
      <section className="code-recording-drawer-section">
        <div className="code-recording-drawer-section-title">3. 录制阈值与备注</div>

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
      </section>

      {/* 底部操作区 (供直接渲染或吸底容器使用) */}
      <div className="code-recording-drawer-footer">
        <div className="code-drawer-footer-summary">
          <span className="code-text-muted">已选 </span>
          <strong className="code-text-primary code-font-mono">{targetProcessIds.length}</strong>
          <span className="code-text-muted"> 个业务进程</span>
          {currentAgent && (
            <span className="code-text-secondary code-text-sm code-ml-xs">
              · 宿主: <strong>{currentAgent.name}</strong>
            </span>
          )}
        </div>
        <div className="code-drawer-footer-actions">
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
            disabled={
              submitting ||
              isAgentOffline ||
              isAgentLeased ||
              targetProcessIds.length === 0
            }
            data-testid="btn-confirm-start-recording"
          >
            {submitting ? '启动中...' : '立即启动录制'}
          </button>
        </div>
      </div>
    </div>
  );
};

export const RecordingStartDrawer: React.FC<RecordingStartDrawerProps> = ({
  open,
  onClose,
  onSuccess,
  allProcesses,
  allAgents,
  isAdmin,
}) => {
  return (
    <Drawer
      open={open}
      onClose={onClose}
      width={680}
      title="发起多进程协同录制"
      subtitle="在指定 Agent 上下发 eBPF 探针租约，纳管微服务链路通信并聚合录制流量"
    >
      <RecordingStartContent
        allProcesses={allProcesses}
        allAgents={allAgents}
        isAdmin={isAdmin}
        onClose={onClose}
        onSuccess={onSuccess}
      />
    </Drawer>
  );
};

export default RecordingStartDrawer;
