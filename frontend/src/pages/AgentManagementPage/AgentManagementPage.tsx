import React, { useCallback, useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Modal, Pagination, usePaginationState } from '@code/common';
import { useToast } from '../../utils/toast';
import { useModuleUser } from '../../context/ModuleUserContext';
import * as agentApi from '../../api/agent';
import type { Agent, Process } from '../../types';
import { AgentScopeTabs } from './AgentScopeTabs';
import { AgentLeaseStatusBadge } from './AgentLeaseStatusBadge';
import { ProcessModuleBinderModal } from './ProcessModuleBinderModal';

export const AgentManagementPage: React.FC = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const { user } = useModuleUser();
  const toast = useToast();
  const isAdmin = Boolean(user?.is_module_admin || user?.is_super_admin);

  const scopeParam = (searchParams.get('scope') as 'SHARED' | 'PRIVATE') || 'SHARED';
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

  const [binderModalOpen, setBinderModalOpen] = useState(false);
  const [editingProcess, setEditingProcess] = useState<Process | null>(null);

  const [unlockModalOpen, setUnlockModalOpen] = useState(false);
  const [unlockAgentId, setUnlockAgentId] = useState<string>('');
  const [unlockReason, setUnlockReason] = useState<string>('');

  const loadAgents = useCallback(async () => {
    setLoading(true);
    try {
      const res = await agentApi.listAgents({
        scope: scopeParam,
        page,
        page_size: pageSize,
      });
      setAgents(res.items);
      setTotalAgents(res.total);
      if (res.items.length > 0) {
        setSelectedAgent((prev) => prev || res.items[0]);
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '加载 Agent 列表失败');
    } finally {
      setLoading(false);
    }
  }, [scopeParam, page, pageSize, toast]);

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
        setProcesses(res.processes);
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
      return next;
    });
    setSelectedAgent(null);
    setRegisterForm((prev) => ({ ...prev, scope: newScope }));
  };

  const handleHandshake = async (agentId: string) => {
    try {
      const res = await agentApi.handshakeAgent(agentId);
      toast.success(`探活成功: 版本 ${res.handshake.version}, 状态 ${res.handshake.status}`);
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
        setProcesses(res.processes);
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '删除进程失败');
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
              + 登记 {scopeParam === 'SHARED' ? '公共 Agent' : '我的私有 Agent'}
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
              {agents.map((ag) => {
                const isSelected = selectedAgent?.id === ag.id;
                return (
                  <div
                    key={ag.id}
                    className={`code-agent-card ${isSelected ? 'code-agent-card--selected' : ''}`}
                    onClick={() => setSelectedAgent(ag)}
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
                      {(isAdmin || ag.scope === 'PRIVATE') && (
                        <button
                          type="button"
                          className="code-btn code-btn--danger code-btn--xs"
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
                  <h3 className="code-pane-title">
                    业务进程映射 - [{selectedAgent.name}]
                  </h3>
                  <span className="code-color-muted code-text-sm">
                    所属端点: {selectedAgent.endpoint} | 租约状态: {selectedAgent.lease_type || '空闲'}
                  </span>
                </div>
                {(isAdmin || selectedAgent.scope === 'PRIVATE') && (
                  <button
                    type="button"
                    className="code-btn code-btn--primary code-btn--xs"
                    onClick={() => {
                      setEditingProcess(null);
                      setBinderModalOpen(true);
                    }}
                  >
                    + 挂载业务进程
                  </button>
                )}
              </div>

              <div className="code-table-wrapper code-mt-sm">
                <table className="code-table">
                  <thead>
                    <tr>
                      <th>进程名称</th>
                      <th>监听端口</th>
                      <th>命令行特征</th>
                      <th>挂载公共模块 (ModuleIDs)</th>
                      <th>说明</th>
                      <th>操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {processes.map((proc) => (
                      <tr key={proc.id}>
                        <td className="code-font-mono code-color-primary">{proc.name}</td>
                        <td className="code-font-mono">{proc.listen_port}</td>
                        <td className="code-font-mono code-text-xs code-color-muted">{proc.cmd_pattern || '-'}</td>
                        <td>
                          <div className="code-tag-group">
                            {(proc.process_modules ?? []).map((pm) => (
                              <span key={pm.module_id} className="code-tag code-tag--info">
                                {pm.module?.name || pm.module_id}
                              </span>
                            ))}
                            {(!proc.process_modules || proc.process_modules.length === 0) && (
                              <span className="code-color-muted code-text-xs">未绑定模块</span>
                            )}
                          </div>
                        </td>
                        <td>{proc.description || '-'}</td>
                        <td>
                          <div className="code-table-actions">
                            {(isAdmin || selectedAgent.scope === 'PRIVATE') && (
                              <>
                                <button
                                  type="button"
                                  className="code-btn code-btn--secondary code-btn--xs"
                                  onClick={() => {
                                    setEditingProcess(proc);
                                    setBinderModalOpen(true);
                                  }}
                                >
                                  修改
                                </button>
                                <button
                                  type="button"
                                  className="code-btn code-btn--danger code-btn--xs"
                                  onClick={() => void handleDeleteProcess(proc.id)}
                                >
                                  删除
                                </button>
                              </>
                            )}
                          </div>
                        </td>
                      </tr>
                    ))}
                    {processes.length === 0 && (
                      <tr>
                        <td colSpan={6} className="code-table-empty">
                          当前 Agent 尚未挂载任何业务进程
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

      {/* 进程挂载弹窗 */}
      {selectedAgent && (
        <ProcessModuleBinderModal
          open={binderModalOpen}
          onClose={() => setBinderModalOpen(false)}
          agent={selectedAgent}
          editingProcess={editingProcess}
          onSuccess={() => {
            void (async () => {
              const res = await agentApi.getAgent(selectedAgent.id);
              setProcesses(res.processes);
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
