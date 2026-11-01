import React, { useEffect, useMemo, useState } from 'react';
import { Modal } from '@code/common';
import { useToast } from '../../utils/toast';
import * as agentApi from '../../api/agent';
import * as archApi from '../../api/arch';
import type { Agent, DiscoveredProcess, Module, Process } from '../../types';

export interface ProcessModuleBinderContentProps {
  agent: Agent;
  editingProcess: Process | null;
  existingGroups?: string[];
  allModules?: Module[];
  onSave?: (data: {
    group_name: string;
    name: string;
    listen_port: number;
    advertise_ip?: string;
    cmd_pattern?: string;
    module_ids: string[];
    description?: string;
  }) => void;
  onCancel?: () => void;
  onSuccess?: () => void;
}

export const ProcessModuleBinderContent: React.FC<ProcessModuleBinderContentProps> = ({
  agent,
  editingProcess,
  existingGroups = [],
  allModules: propModules,
  onSave,
  onCancel,
  onSuccess,
}) => {
  const toast = useToast();
  const [discovered, setDiscovered] = useState<DiscoveredProcess[]>([]);
  const [loadingDiscovered, setLoadingDiscovered] = useState(false);
  const [loadedModules, setLoadedModules] = useState<Module[]>([]);

  const allModules = propModules !== undefined ? propModules : loadedModules;

  // 合并进程组选项
  const groupOptions = useMemo(() => {
    const set = new Set<string>();
    set.add('默认进程组');
    set.add('核心业务组');
    set.add('支撑服务组');
    existingGroups.forEach((g) => {
      const trimmed = (g || '').trim();
      if (trimmed) set.add(trimmed);
    });
    if (editingProcess?.group_name) {
      set.add(editingProcess.group_name.trim());
    }
    return Array.from(set);
  }, [existingGroups, editingProcess]);

  // 惰性初始化状态以支持 SSR / Node.js renderToString 环境
  const [isNewGroupMode, setIsNewGroupMode] = useState(false);
  const [isClientOnly, setIsClientOnly] = useState(() => {
    if (editingProcess) {
      return Number(editingProcess.listen_port) === 0;
    }
    return false;
  });

  const [form, setForm] = useState(() => {
    if (editingProcess) {
      const port = Number(editingProcess.listen_port) || 0;
      const initialModuleIds =
        editingProcess.module_ids && editingProcess.module_ids.length > 0
          ? editingProcess.module_ids
          : (editingProcess.process_modules ?? []).map((pm) => pm.module_id);
      return {
        group_name: editingProcess.group_name || '默认进程组',
        name: editingProcess.name || '',
        listen_port: port,
        advertise_ip: editingProcess.advertise_ip || '',
        cmd_pattern: editingProcess.cmd_pattern || '',
        module_ids: initialModuleIds,
        description: editingProcess.description || '',
      };
    }
    return {
      group_name: '默认进程组',
      name: '',
      listen_port: 8080,
      advertise_ip: '',
      cmd_pattern: '',
      module_ids: [] as string[],
      description: '',
    };
  });

  useEffect(() => {
    if (editingProcess) {
      const port = Number(editingProcess.listen_port) || 0;
      const initialModuleIds =
        editingProcess.module_ids && editingProcess.module_ids.length > 0
          ? editingProcess.module_ids
          : (editingProcess.process_modules ?? []).map((pm) => pm.module_id);

      setIsClientOnly(port === 0);
      setIsNewGroupMode(false);
      setForm({
        group_name: editingProcess.group_name || '默认进程组',
        name: editingProcess.name,
        listen_port: port,
        advertise_ip: editingProcess.advertise_ip || '',
        cmd_pattern: editingProcess.cmd_pattern || '',
        module_ids: initialModuleIds,
        description: editingProcess.description || '',
      });
    } else {
      setIsClientOnly(false);
      setIsNewGroupMode(false);
      setForm({
        group_name: groupOptions[0] || '默认进程组',
        name: '',
        listen_port: 8080,
        advertise_ip: '',
        cmd_pattern: '',
        module_ids: [],
        description: '',
      });
    }
  }, [editingProcess, groupOptions]);

  // 加载公共可用模块（仅在未由外部 prop 传入时发起网络请求）
  useEffect(() => {
    if (propModules !== undefined) return;
    void (async () => {
      try {
        const res = await archApi.listModules({ page_size: 100 });
        setLoadedModules(res.items ?? []);
      } catch (err) {
        toast.error(err instanceof Error ? err.message : '加载架构模块失败');
      }
    })();
  }, [propModules, toast]);

  // 探测本机进程
  const handleDiscover = async () => {
    setLoadingDiscovered(true);
    try {
      const res = await agentApi.discoverAgentProcesses(agent.id);
      const items = res.items ?? [];
      setDiscovered(items);
      toast.success(`成功探测到 ${items.length} 个监听进程`);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '探测进程失败');
    } finally {
      setLoadingDiscovered(false);
    }
  };

  const handleSelectDiscovered = (p: DiscoveredProcess) => {
    setIsClientOnly(p.listen_port === 0);
    setForm((prev) => ({
      ...prev,
      name: p.exe_name || `proc_${p.listen_port}`,
      listen_port: p.listen_port,
      cmd_pattern: p.cmdline || '',
    }));
  };

  const toggleModule = (modId: string) => {
    setForm((prev) => {
      const set = new Set(prev.module_ids);
      if (set.has(modId)) set.delete(modId);
      else set.add(modId);
      return { ...prev, module_ids: Array.from(set) };
    });
  };

  const handleInternalSave = async () => {
    if (!form.name.trim()) {
      toast.error('请输入进程名称');
      return;
    }
    if (!form.group_name.trim()) {
      toast.error('请指定进程分组');
      return;
    }
    const finalPort = isClientOnly ? 0 : Number(form.listen_port);
    if (isNaN(finalPort) || finalPort < 0 || finalPort > 65535) {
      toast.error('监听端口必须为 0~65535 之间的合法数字 (0 表示纯客户端)');
      return;
    }

    const payload = {
      ...form,
      name: form.name.trim(),
      group_name: form.group_name.trim(),
      listen_port: finalPort,
    };

    if (onSave) {
      onSave(payload);
      return;
    }

    try {
      if (editingProcess) {
        await agentApi.updateProcess(editingProcess.id, payload);
        toast.success('进程映射已更新');
      } else {
        await agentApi.createProcess({
          agent_id: agent.id,
          ...payload,
        });
        toast.success('进程映射已创建');
      }
      onSuccess?.();
      onCancel?.();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '保存进程失败');
    }
  };

  return (
    <div className="code-process-modal-body" data-testid="process-module-binder-content">
      {/* Agent 探查快捷栏 */}
      <div className="code-discover-bar">
        <button
          type="button"
          className="code-btn code-btn--secondary code-btn--sm"
          disabled={loadingDiscovered}
          onClick={() => void handleDiscover()}
        >
          {loadingDiscovered ? '探查中...' : '通过 Agent 探查当前监听中的业务进程'}
        </button>
      </div>

      {discovered.length > 0 && (
        <div className="code-table-wrapper code-mb-md">
          <table className="code-table code-table--compact">
            <thead>
              <tr>
                <th>PID</th>
                <th>监听端口</th>
                <th>可执行程序</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {(discovered ?? []).map((dp, idx) => (
                <tr key={`${dp.pid}-${dp.listen_port}-${idx}`}>
                  <td className="code-font-mono">{dp.pid}</td>
                  <td className="code-font-mono code-color-primary">{dp.listen_port}</td>
                  <td>{dp.exe_name}</td>
                  <td>
                    <button
                      type="button"
                      className="code-btn code-btn--secondary code-btn--xs"
                      onClick={() => handleSelectDiscovered(dp)}
                    >
                      填入表单
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* 表单区域 */}
      <div className="code-form">
        {/* 进程分组与进程名称 */}
        <div className="code-form-grid-2">
          <div className="code-form-item">
            <label className="code-form-label">进程分组 (group_name):</label>
            {isNewGroupMode ? (
              <div style={{ display: 'flex', gap: '0.5rem' }}>
                <input
                  type="text"
                  className="code-input"
                  placeholder="输入新进程分组名称"
                  value={form.group_name}
                  data-testid="input-group-name"
                  onChange={(e) => setForm({ ...form, group_name: e.target.value })}
                  autoFocus
                />
                <button
                  type="button"
                  className="code-btn code-btn--secondary code-btn--sm"
                  onClick={() => {
                    setIsNewGroupMode(false);
                    setForm({ ...form, group_name: groupOptions[0] || '默认进程组' });
                  }}
                >
                  选择已有组
                </button>
              </div>
            ) : (
              <div style={{ display: 'flex', gap: '0.5rem' }}>
                <select
                  className="code-select"
                  value={form.group_name}
                  data-testid="select-group-name"
                  onChange={(e) => {
                    if (e.target.value === '__NEW__') {
                      setIsNewGroupMode(true);
                      setForm({ ...form, group_name: '' });
                    } else {
                      setForm({ ...form, group_name: e.target.value });
                    }
                  }}
                >
                  {groupOptions.map((g) => (
                    <option key={g} value={g}>
                      {g}
                    </option>
                  ))}
                  <option value="__NEW__">+ 新建进程组...</option>
                </select>
              </div>
            )}
          </div>

          <div className="code-form-item">
            <label className="code-form-label">进程名称:</label>
            <input
              type="text"
              className="code-input"
              data-testid="input-process-name"
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              placeholder="如 order_sut_node1"
            />
          </div>
        </div>

        {/* 端口与纯客户端设置 */}
        <div className="code-form-grid-2">
          <div className="code-form-item">
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <label className="code-form-label" style={{ marginBottom: 0 }}>
                监听 TCP 端口:
              </label>
              <label style={{ display: 'inline-flex', alignItems: 'center', gap: '0.25rem', fontSize: '0.75rem', cursor: 'pointer' }}>
                <input
                  type="checkbox"
                  checked={isClientOnly}
                  data-testid="checkbox-client-only"
                  onChange={(e) => {
                    const checked = e.target.checked;
                    setIsClientOnly(checked);
                    if (checked) {
                      setForm((prev) => ({ ...prev, listen_port: 0 }));
                    } else {
                      setForm((prev) => ({ ...prev, listen_port: 8080 }));
                    }
                  }}
                />
                <span>纯客户端 (Port 0)</span>
              </label>
            </div>
            <input
              type="number"
              className="code-input"
              data-testid="input-listen-port"
              value={form.listen_port}
              disabled={isClientOnly}
              onChange={(e) => setForm({ ...form, listen_port: Number(e.target.value) })}
              placeholder="0 表示纯客户端"
            />
          </div>

          <div className="code-form-item">
            <label className="code-form-label">对外通告 IP (选填):</label>
            <input
              type="text"
              className="code-input"
              value={form.advertise_ip}
              onChange={(e) => setForm({ ...form, advertise_ip: e.target.value })}
              placeholder="如 192.168.56.18"
            />
          </div>
        </div>

        <div className="code-form-item">
          <label className="code-form-label">命令行匹配特征 (cmdline):</label>
          <input
            type="text"
            className="code-input"
            value={form.cmd_pattern}
            onChange={(e) => setForm({ ...form, cmd_pattern: e.target.value })}
            placeholder="如 ./bin/order_server -config=conf.yaml"
          />
        </div>

        {/* 挂载架构模块 */}
        <div className="code-form-item">
          <label className="code-form-label">
            承载挂载的公共架构模块 (ModuleIDs - 决定重放同构匹配度):
          </label>
          <div className="code-module-checkbox-group" data-testid="module-checkbox-group">
            {(allModules ?? []).map((m) => {
              const checked = (form.module_ids ?? []).includes(m.id);
              return (
                <label
                  key={m.id}
                  className={`code-checkbox-chip ${checked ? 'code-checkbox-chip--checked' : ''}`}
                  data-testid={`module-chip-${m.id}`}
                >
                  <input
                    type="checkbox"
                    checked={checked}
                    onChange={() => toggleModule(m.id)}
                  />
                  <span>{m.name} ({m.code})</span>
                </label>
              );
            })}
            {allModules.length === 0 && (
              <div className="code-empty-pane code-p-xs">暂无可选模块，请先在架构中心创建</div>
            )}
          </div>
        </div>

        <div className="code-form-item">
          <label className="code-form-label">说明描述:</label>
          <input
            type="text"
            className="code-input"
            value={form.description}
            onChange={(e) => setForm({ ...form, description: e.target.value })}
          />
        </div>
      </div>

      <div className="code-modal-footer" style={{ marginTop: '1.5rem', display: 'flex', justifyContent: 'flex-end', gap: '0.75rem' }}>
        {onCancel && (
          <button type="button" className="code-btn code-btn--secondary" onClick={onCancel}>
            取消
          </button>
        )}
        <button
          type="button"
          className="code-btn code-btn--primary"
          onClick={() => void handleInternalSave()}
          data-testid="submit-process-binder-btn"
        >
          保存进程与模块绑定
        </button>
      </div>
    </div>
  );
};

export interface ProcessModuleBinderModalProps {
  open: boolean;
  onClose: () => void;
  agent: Agent;
  editingProcess: Process | null;
  existingGroups?: string[];
  allModules?: Module[];
  onSuccess: () => void;
}

export const ProcessModuleBinderModal: React.FC<ProcessModuleBinderModalProps> = ({
  open,
  onClose,
  agent,
  editingProcess,
  existingGroups = [],
  allModules,
  onSuccess,
}) => {
  return (
    <Modal
      open={open}
      onClose={onClose}
      title={editingProcess ? `修改业务进程 [${editingProcess.name}]` : `为 Agent [${agent.name}] 挂载业务进程`}
      width="lg"
      footer={null}
    >
      <ProcessModuleBinderContent
        agent={agent}
        editingProcess={editingProcess}
        existingGroups={existingGroups}
        allModules={allModules}
        onCancel={onClose}
        onSuccess={onSuccess}
      />
    </Modal>
  );
};
