import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Modal, Pagination, usePaginationState } from '@code/common';
import { useToast } from '../../utils/toast';
import { useModuleUser } from '../../context/ModuleUserContext';
import * as agentApi from '../../api/agent';
import type { Agent, Process } from '../../types';
import { AgentScopeTabs } from './AgentScopeTabs';
import { AgentLeaseStatusBadge } from './AgentLeaseStatusBadge';
import { AgentProcessMountDrawer } from './AgentProcessMountDrawer';

export const AgentManagementPage: React.FC = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const { user } = useModuleUser();
  const toast = useToast();
  const isAdmin = Boolean(user?.is_module_admin || user?.is_super_admin);

  const scopeParam = (searchParams.get('scope') as 'SHARED' | 'PRIVATE') || 'SHARED';
  const urlAgentId = searchParams.get('agent_id') || '';
  const urlProcessId = searchParams.get('process_id') || '';

  const { page, pageSize, setPage, setPageSize } = usePaginationState({
    defaultPageSize: 25,
  });

  const [agents, setAgents] = useState<Agent[]>([]);
  const [totalAgents, setTotalAgents] = useState(0);
  const [loading, setLoading] = useState(false);

  // 选中的 Agent 及其进程列表
  const [selectedAgent, setSelectedAgent] = useState<Agent | null>(null);
  const [processes, setProcesses] = useState<Process[]>([]);

  // 弹窗状态
  const [registerModalOpen, setRegisterModalOpen] = useState(false);
  const [registerForm, setRegisterForm] = useState({
    name: '',
    endpoint: 'http://127.0.0.1:9091',
    scope: scopeParam,
    claim_code: '',
    description: '',
  });

  const [mountDrawerOpen, setMountDrawerOpen] = useState(false);

  const [unlockModalOpen, setUnlockModalOpen] = useState(false);
  const [unlockAgentId, setUnlockAgentId] = useState<string>('');
  const [unlockReason, setUnlockReason] = useState<string>('');

  // 判断当前用户对当前选中的 Agent 是否具备写权限 (RBAC)
  const canWriteSelectedAgent = useMemo(() => {
    if (isAdmin) return true;
    if (!selectedAgent) return false;
    if (selectedAgent.scope === 'SHARED') return false;
    if (!selectedAgent.owner_id) return true;
    return String(selectedAgent.owner_id) === String(user?.id);
  }, [isAdmin, selectedAgent, user]);

  const loadAgents = useCallback(async () => {
    setLoading(true);
    try {
      const res = await agentApi.listAgents({
        scope: scopeParam,
        page,
        page_size: pageSize,
      });
      const items = res.items ?? [];
      setAgents(items);
      setTotalAgents(res.total ?? 0);
      if (items.length > 0) {
        setSelectedAgent((prev) => {
          if (urlAgentId) {
            const matched = items.find((a) => String(a.id) === String(urlAgentId));
            if (matched) return matched;
          }
          if (prev && items.some((a) => String(a.id) === String(prev.id))) {
            return prev;
          }
          return items[0];
        });
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '加载 Agent 列表失败');
    } finally {
      setLoading(false);
    }
  }, [scopeParam, page, pageSize, urlAgentId, toast]);

  useEffect(() => {
    void loadAgents();
  }, [loadAgents]);

  // 当选中的 Agent 变更时，加载该 Agent 详情与进程
  const selectedAgentId = selectedAgent?.id;
  useEffect(() => {
    if (!selectedAgentId) return;
    void (async () => {
      try {
        const res = await agentApi.getAgent(selectedAgentId);
        setSelectedAgent(res.agent);
        setProcesses(res.processes ?? []);
      } catch (err) {
        toast.error(err instanceof Error ? err.message : '加载 Agent 进程失败');
      }
    })();
  }, [selectedAgentId, toast]);

  const handleScopeChange = (newScope: 'SHARED' | 'PRIVATE') => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      next.set('scope', newScope);
      next.set('page', '1');
      next.delete('agent_id');
      next.delete('process_id');
      return next;
    });
    setSelectedAgent(null);
    setRegisterForm((prev) => ({ ...prev, scope: newScope }));
  };

  const handleHandshake = async (agentId: string) => {
    try {
      const res = await agentApi.handshakeAgent(agentId);
      const version = res.handshake.agent_version || res.agent.agent_version || '未知';
      const status = res.agent.status || 'ONLINE';
      toast.success(`探活成功: 版本 ${version}, 状态 ${status}`);
      void loadAgents();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '探活通信失败');
    }
  };

  const handleRegister = async () => {
    if (!registerForm.name || !registerForm.endpoint) {
      toast.error('请填写完整名称与端点地址');
      return;
    }
    try {
      await agentApi.registerAgent(registerForm);
      toast.success('Agent 登记成功并已完成握手校验');
      setRegisterModalOpen(false);
      void loadAgents();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '登记 Agent 失败');
    }
  };

  const handleDeleteAgent = async (id: string) => {
    try {
      await agentApi.deleteAgent(id);
      toast.success('Agent 已注销');
      setSelectedAgent(null);
      void loadAgents();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '注销 Agent 失败');
    }
  };

  const handleConfirmUnlock = async () => {
    if (!unlockReason.trim()) {
      toast.error('请输入解锁原因');
      return;
    }
    try {
      await agentApi.forceUnlockAgentLease(unlockAgentId, unlockReason);
      toast.success('排他租约已强制释放并下发自洁');
      setUnlockModalOpen(false);
      void loadAgents();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '强制解锁失败');
    }
  };

  const handleDeleteProcess = async (procId: string) => {
    try {
      await agentApi.deleteProcess(procId);
      toast.success('进程映射已删除');
      if (selectedAgent) {
        const res = await agentApi.getAgent(selectedAgent.id);
        setProcesses(res.processes ?? []);
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '删除进程失败');
    }
  };

  const handleDisarmAllProbes = async () => {
    if (!selectedAgent) return;
    try {
      const res = await agentApi.disarmAllProbes(selectedAgent.id);
      toast.success(`已下发卸载指令，成功卸载 ${res.disarmed_count} 个进程探针`);
      const refreshed = await agentApi.getAgent(selectedAgent.id);
      setProcesses(refreshed.processes ?? []);
      setSelectedAgent(refreshed.agent);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '一键卸载探针失败');
    }
  };

  const handleDisarmProcess = async (procId: string) => {
    try {
      await agentApi.disarmProcessProbe(procId);
      toast.success('进程探针已卸载');
      if (selectedAgent) {
        const refreshed = await agentApi.getAgent(selectedAgent.id);
        setProcesses(refreshed.processes ?? []);
        setSelectedAgent(refreshed.agent);
      }
    } catch (err: unknown) {
      const errMsg = err instanceof Error ? err.message : String(err);
      if (errMsg.includes('409') || errMsg.includes('录制') || errMsg.includes('force')) {
        try {
          await agentApi.disarmProcessProbe(procId, true);
          toast.success('已强制卸载进程探针');
          if (selectedAgent) {
            const refreshed = await agentApi.getAgent(selectedAgent.id);
            setProcesses(refreshed.processes ?? []);
            setSelectedAgent(refreshed.agent);
          }
        } catch (forceErr) {
          toast.error(forceErr instanceof Error ? forceErr.message : '强制卸载探针失败');
        }
        return;
      }
      toast.error(errMsg || '卸载探针失败');
    }
  };

  const renderProbeStatusBadge = (status?: string, reason?: string) => {
    switch (status) {
      case 'READY':
        return (
          <span
            className="code-badge"
            style={{
              background: 'rgba(34, 197, 94, 0.15)',
              color: 'var(--color-success)',
              border: '1px solid var(--color-success)',
            }}
            title="探针就绪且心跳健康 (READY)"
            data-testid="probe-badge-ready"
          >
            🟢 READY
          </span>
        );
      case 'NEED_RECONNECT':
        return (
          <span
            className="code-badge"
            style={{
              background: 'rgba(234, 179, 8, 0.15)',
              color: 'var(--color-warning)',
              border: '1px solid var(--color-warning)',
            }}
            title="需重连以截获长连接 (NEED_RECONNECT)"
            data-testid="probe-badge-need-reconnect"
          >
            🟡 NEED_RECONNECT
          </span>
        );
      case 'WAITING_PROC':
        return (
          <span
            className="code-badge"
            style={{
              background: 'rgba(59, 130, 246, 0.15)',
              color: 'var(--color-primary)',
              border: '1px solid var(--color-primary)',
            }}
            title="等待宿主机目标进程启动 (WAITING_PROC)"
            data-testid="probe-badge-waiting-proc"
          >
            🔵 WAITING_PROC
          </span>
        );
      case 'MONITORING':
        return (
          <span
            className="code-badge"
            style={{
              background: 'rgba(168, 85, 247, 0.15)',
              color: '#a855f7',
              border: '1px solid #a855f7',
            }}
            title="录制监控中 (MONITORING)"
            data-testid="probe-badge-monitoring"
          >
            🟣 MONITORING
          </span>
        );
      case 'DISARMED':
        return (
          <span
            className="code-badge"
            style={{
              background: 'var(--color-bg-muted)',
              color: 'var(--color-text-muted)',
              border: '1px solid var(--color-border-primary)',
            }}
            title="探针未激活 (DISARMED)"
            data-testid="probe-badge-disarmed"
          >
            ⚪ DISARMED
          </span>
        );
      case 'OFFLINE':
        return (
          <span
            className="code-badge"
            style={{
              background: 'var(--color-bg-muted)',
              color: 'var(--color-text-muted)',
              border: '1px solid var(--color-border-secondary)',
            }}
            title="Agent 宿主机离线 (OFFLINE)"
            data-testid="probe-badge-offline"
          >
            ⚫ OFFLINE
          </span>
        );
      case 'ERROR':
      default:
        return (
          <span
            className="code-badge"
            style={{
              background: 'rgba(239, 68, 68, 0.15)',
              color: 'var(--color-danger)',
              border: '1px solid var(--color-danger)',
            }}
            title={reason || '探针异常 (ERROR)'}
            data-testid="probe-badge-error"
          >
            🔴 {status || 'ERROR'}
          </span>
        );
    }
  };

  return (
    <div className="code-workbench-page code-agent-workbench" data-testid="agent-management-page">
      <div className="code-workbench-header">
        <div className="code-workbench-title-group">
          <h2 className="code-workbench-title">公私域 Agent 纳管与排他租约控制台</h2>
          <span className="code-workbench-subtitle">
            纳管数据面 Agent、探查主机进程、挂载公共架构契约并保障单机单租约互斥安全
          </span>
        </div>

        <div className="code-header-actions">
          {(isAdmin || scopeParam === 'PRIVATE') && (
            <button
              type="button"
              className="code-btn code-btn--primary"
              data-testid="register-agent-btn"
              onClick={() => {
                setRegisterForm({
                  name: '',
                  endpoint: 'http://127.0.0.1:9091',
                  scope: scopeParam,
                  claim_code: '',
                  description: '',
                });
                setRegisterModalOpen(true);
              }}
            >
              {`+ 登记 ${scopeParam === 'SHARED' ? '公共 Agent' : '我的私有 Agent'}`}
            </button>
          )}
        </div>
      </div>

      {/* 作用域切换 */}
      <AgentScopeTabs currentScope={scopeParam} onChangeScope={handleScopeChange} />

      {/* Agent 列表与进程映射双栏 */}
      <div className="code-agent-layout code-mt-md">
        {/* 左栏：Agent 列表 */}
        <div className="code-agent-list-pane">
          <div className="code-pane-header">
            <span className="code-pane-title">
              {scopeParam === 'SHARED' ? '团队公共 Agent' : '我的私有 Agent'} ({totalAgents})
            </span>
          </div>

          {loading ? (
            <div className="code-tpl-loading-screen">正在加载 Agent 列表...</div>
          ) : agents.length === 0 ? (
            <div className="code-empty-pane code-p-xl">当前作用域下暂无已登记的 Agent</div>
          ) : (
            <div className="code-agent-cards-grid">
              {(agents ?? []).map((ag) => {
                const isSelected = selectedAgent?.id === ag.id;
                const canDeleteAgent = isAdmin || (ag.scope === 'PRIVATE' && (!ag.owner_id || String(ag.owner_id) === String(user?.id)));
                return (
                  <div
                    key={ag.id}
                    className={`code-agent-card ${isSelected ? 'code-agent-card--selected' : ''}`}
                    data-testid={`agent-card-${ag.id}`}
                    onClick={() => {
                      setSelectedAgent(ag);
                      setSearchParams((prev) => {
                        const next = new URLSearchParams(prev);
                        next.set('agent_id', ag.id);
                        return next;
                      });
                    }}
                  >
                    <div className="code-agent-card__header">
                      <div>
                        <div className="code-agent-card__title">{ag.name}</div>
                        <div className="code-agent-card__endpoint code-font-mono">{ag.endpoint}</div>
                      </div>
                      <span className={`code-status-dot ${ag.status === 'ONLINE' ? 'code-status-dot--online' : 'code-status-dot--offline'}`}>
                        {ag.status}
                      </span>
                    </div>

                    <div className="code-agent-card__meta">
                      <div className="code-meta-item">
                        <span className="code-meta-label">排他租约:</span>
                        <AgentLeaseStatusBadge
                          agent={ag}
                          isAdmin={isAdmin}
                          onForceUnlock={(id) => {
                            setUnlockAgentId(id);
                            setUnlockReason('');
                            setUnlockModalOpen(true);
                          }}
                        />
                      </div>
                      <div className="code-meta-item">
                        <span className="code-meta-label">纳管进程:</span>
                        <span className="code-meta-val">{ag.processes_count ?? 0} 个</span>
                      </div>
                      <div className="code-meta-item">
                        <span className="code-meta-label">归属作用域:</span>
                        <span className="code-badge code-badge--secondary">{ag.scope}</span>
                      </div>
                    </div>

                    <div className="code-agent-card__actions" onClick={(e) => e.stopPropagation()}>
                      <button
                        type="button"
                        className="code-btn code-btn--secondary code-btn--xs"
                        onClick={() => void handleHandshake(ag.id)}
                      >
                        探活
                      </button>
                      {canDeleteAgent && (
                        <button
                          type="button"
                          className="code-btn code-btn--danger code-btn--xs"
                          data-testid={`delete-agent-${ag.id}`}
                          onClick={() => void handleDeleteAgent(ag.id)}
                        >
                          注销
                        </button>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          )}

          <div className="code-mt-md">
            <Pagination
              totalItems={totalAgents}
              page={page}
              pageSize={pageSize}
              onPageChange={setPage}
              onPageSizeChange={setPageSize}
            />
          </div>
        </div>

        {/* 右栏：所选 Agent 的进程映射 */}
        <div className="code-agent-detail-pane">
          {selectedAgent ? (
            <div>
              <div className="code-pane-header">
                <div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.25rem' }}>
                    <h3 className="code-pane-title">
                      业务进程映射 - [{selectedAgent.name}]
                    </h3>
                    <span
                      className="code-badge code-badge--secondary"
                      style={{ fontSize: '0.75rem', fontWeight: 600 }}
                      data-testid="mounts-revision-badge"
                    >
                      v{selectedAgent.mounts_revision || 1}
                    </span>
                    {selectedAgent.fallback_agent_id && (
                      <span
                        className="code-badge"
                        style={{
                          fontSize: '0.75rem',
                          background: 'rgba(59, 130, 246, 0.12)',
                          color: 'var(--color-primary)',
                        }}
                        data-testid="fallback-agent-badge"
                      >
                        回退端点: Agent #{selectedAgent.fallback_agent_id}
                      </span>
                    )}
                  </div>
                  <span className="code-color-muted code-text-sm">
                    所属端点: {selectedAgent.endpoint} | 租约状态: {selectedAgent.lease_type || '空闲'}
                  </span>
                </div>
                {canWriteSelectedAgent && (
                  <div style={{ display: 'flex', gap: '0.4rem' }}>
                    <button
                      type="button"
                      className="code-btn code-btn--secondary code-btn--xs"
                      data-testid="disarm-all-probes-btn"
                      onClick={() => void handleDisarmAllProbes()}
                      title="一键卸载此 Agent 上的所有已激活探针"
                    >
                      一键卸载全部探针
                    </button>
                    <button
                      type="button"
                      className="code-btn code-btn--primary code-btn--xs"
                      data-testid="batch-mount-btn"
                      onClick={() => setMountDrawerOpen(true)}
                    >
                      ⚡ 批量挂载架构进程
                    </button>
                  </div>
                )}
              </div>

              <div className="code-table-wrapper code-mt-sm">
                <table className="code-table">
                  <thead>
                    <tr>
                      <th>架构物理进程</th>
                      <th>生效端点 (Effective IP:Port)</th>
                      <th>端口策略</th>
                      <th>命令行特征</th>
                      <th>探针就绪状态</th>
                      <th>期望状态</th>
                      <th>操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(processes ?? []).map((proc) => {
                      const isHighlighted = urlProcessId === proc.id;
                      const effPort = proc.effective_listen_port ?? proc.listen_port;
                      const hasOverride =
                        proc.override_listen_port !== undefined && proc.override_listen_port !== null;

                      return (
                        <tr
                          key={proc.id}
                          className={isHighlighted ? 'code-table-row--highlighted' : ''}
                          data-testid={`process-row-${proc.id}`}
                          style={
                            isHighlighted
                              ? { backgroundColor: 'var(--color-bg-hover)', outline: '1px solid var(--color-primary)' }
                              : undefined
                          }
                        >
                          <td>
                            <div style={{ display: 'flex', flexDirection: 'column' }}>
                              <strong className="code-color-primary" data-testid={`proc-name-${proc.id}`}>
                                {proc.arch_process?.name || proc.name}
                              </strong>
                              <span className="code-font-mono code-text-xs code-color-muted">
                                {proc.arch_process?.code || proc.group_name || '默认进程组'}
                              </span>
                            </div>
                          </td>
                          <td className="code-font-mono" style={{ fontWeight: 600 }}>
                            {effPort === 0 ? '0 (纯客户端)' : `${proc.advertise_ip || '127.0.0.1'}:${effPort}`}
                          </td>
                          <td>
                            {hasOverride ? (
                              <span
                                className="code-font-mono"
                                style={{
                                  fontSize: '0.75rem',
                                  color: 'var(--color-warning)',
                                  background: 'rgba(234, 179, 8, 0.1)',
                                  padding: '1px 6px',
                                  borderRadius: 'var(--radius-sm)',
                                }}
                              >
                                覆盖为 :{proc.override_listen_port}
                              </span>
                            ) : (
                              <span className="code-color-muted code-text-xs">
                                继承默认 ({proc.arch_process?.default_listen_port ?? proc.listen_port})
                              </span>
                            )}
                          </td>
                          <td className="code-font-mono code-text-xs code-color-muted">
                            {proc.override_cmd_pattern || proc.cmd_pattern || proc.arch_process?.default_cmd_pattern || '-'}
                          </td>
                          <td>{renderProbeStatusBadge(proc.probe_status, proc.probe_status_reason)}</td>
                          <td>
                            <span
                              className="code-badge"
                              style={{
                                background:
                                  proc.probe_desired_state === 'ARMED'
                                    ? 'rgba(34, 197, 94, 0.12)'
                                    : 'var(--color-bg-muted)',
                                color:
                                  proc.probe_desired_state === 'ARMED'
                                    ? 'var(--color-success)'
                                    : 'var(--color-text-muted)',
                              }}
                            >
                              {proc.probe_desired_state || 'ARMED'}
                            </span>
                          </td>
                          <td>
                            <div className="code-table-actions">
                              {canWriteSelectedAgent ? (
                                <>
                                  <button
                                    type="button"
                                    className="code-btn code-btn--secondary code-btn--xs"
                                    data-testid={`disarm-process-${proc.id}`}
                                    onClick={() => void handleDisarmProcess(proc.id)}
                                    title="卸载该进程的 eBPF 探针"
                                  >
                                    卸载探针
                                  </button>
                                  <button
                                    type="button"
                                    className="code-btn code-btn--danger code-btn--xs"
                                    data-testid={`delete-process-${proc.id}`}
                                    onClick={() => void handleDeleteProcess(proc.id)}
                                  >
                                    删除
                                  </button>
                                </>
                              ) : (
                                <span className="code-color-muted code-text-xs" data-testid={`read-only-${proc.id}`}>
                                  只读
                                </span>
                              )}
                            </div>
                          </td>
                        </tr>
                      );
                    })}
                    {processes.length === 0 && (
                      <tr>
                        <td colSpan={7} className="code-table-empty">
                          当前 Agent 尚未挂载任何业务进程，请点击上方「⚡ 批量挂载架构进程」
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          ) : (
            <div className="code-empty-pane code-p-xl">请在左侧选择一个 Agent 查看进程映射</div>
          )}
        </div>
      </div>

      {/* 登记 Agent 弹窗 */}
      <Modal
        open={registerModalOpen}
        onClose={() => setRegisterModalOpen(false)}
        title={registerForm.scope === 'SHARED' ? '登记团队公共 Agent (SHARED)' : '登记我的私有 Agent (PRIVATE)'}
        footer={
          <div className="code-modal-footer">
            <button type="button" className="code-btn code-btn--secondary" onClick={() => setRegisterModalOpen(false)}>
              取消
            </button>
            <button type="button" className="code-btn code-btn--primary" onClick={() => void handleRegister()}>
              登记并探活
            </button>
          </div>
        }
      >
        <div className="code-form">
          <div className="code-form-item">
            <label className="code-form-label">Agent 唯一名称:</label>
            <input
              type="text"
              className="code-input"
              value={registerForm.name}
              onChange={(e) => setRegisterForm({ ...registerForm, name: e.target.value })}
              placeholder="如 my-dev-agent-01"
            />
          </div>
          <div className="code-form-item">
            <label className="code-form-label">HTTP 管控端点 (遵守 SSRF 安全防腐策略):</label>
            <input
              type="text"
              className="code-input"
              value={registerForm.endpoint}
              onChange={(e) => setRegisterForm({ ...registerForm, endpoint: e.target.value })}
              placeholder="如 http://192.168.56.19:9091"
            />
          </div>
          {registerForm.scope === 'PRIVATE' && (
            <div className="code-form-item">
              <label className="code-form-label">私有 Agent 认领凭证 (ClaimCode - 选填):</label>
              <input
                type="text"
                className="code-input"
                value={registerForm.claim_code}
                onChange={(e) => setRegisterForm({ ...registerForm, claim_code: e.target.value })}
                placeholder="与 Agent 启动时的 --claim-code 一致"
              />
            </div>
          )}
          <div className="code-form-item">
            <label className="code-form-label">说明描述:</label>
            <input
              type="text"
              className="code-input"
              value={registerForm.description}
              onChange={(e) => setRegisterForm({ ...registerForm, description: e.target.value })}
            />
          </div>
        </div>
      </Modal>

      {/* 批量挂载架构进程抽屉 (680px) */}
      {selectedAgent && (
        <AgentProcessMountDrawer
          open={mountDrawerOpen}
          onClose={() => setMountDrawerOpen(false)}
          agent={selectedAgent}
          existingProcesses={processes}
          onSuccess={() => {
            void (async () => {
              const res = await agentApi.getAgent(selectedAgent.id);
              setProcesses(res.processes ?? []);
              setSelectedAgent(res.agent);
            })();
          }}
        />
      )}

      {/* 强制解锁排他租约弹窗 */}
      <Modal
        open={unlockModalOpen}
        onClose={() => setUnlockModalOpen(false)}
        title="管理员强制释放 Agent 排他租约"
        footer={
          <div className="code-modal-footer">
            <button type="button" className="code-btn code-btn--secondary" onClick={() => setUnlockModalOpen(false)}>
              取消
            </button>
            <button type="button" className="code-btn code-btn--danger" onClick={() => void handleConfirmUnlock()}>
              确认强制解锁
            </button>
          </div>
        }
      >
        <div className="code-form">
          <div className="code-form-item">
            <label className="code-form-label">强制解锁原因 (将记入系统操作审计日志):</label>
            <textarea
              className="code-textarea"
              value={unlockReason}
              onChange={(e) => setUnlockReason(e.target.value)}
              placeholder="如 前序录制任务异常挂起，持锁人已离线"
            />
          </div>
        </div>
      </Modal>
    </div>
  );
};

export default AgentManagementPage;
