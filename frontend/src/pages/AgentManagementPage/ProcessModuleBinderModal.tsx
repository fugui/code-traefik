import React, { useEffect, useState } from 'react';
import { Modal } from '@code/common';
import { useToast } from '../../utils/toast';
import * as agentApi from '../../api/agent';
import * as archApi from '../../api/arch';
import type { Agent, DiscoveredProcess, Module, Process } from '../../types';

export interface ProcessModuleBinderModalProps {
  open: boolean;
  onClose: () => void;
  agent: Agent;
  editingProcess: Process | null;
  onSuccess: () => void;
}

export const ProcessModuleBinderModal: React.FC<ProcessModuleBinderModalProps> = ({
  open,
  onClose,
  agent,
  editingProcess,
  onSuccess,
}) => {
  const toast = useToast();
  const [discovered, setDiscovered] = useState<DiscoveredProcess[]>([]);
  const [loadingDiscovered, setLoadingDiscovered] = useState(false);
  const [allModules, setAllModules] = useState<Module[]>([]);

  const [form, setForm] = useState({
    name: '',
    listen_port: 8080,
    advertise_ip: '',
    cmd_pattern: '',
    module_ids: [] as string[],
    description: '',
  });

  useEffect(() => {
    if (editingProcess) {
      setForm({
        name: editingProcess.name,
        listen_port: editingProcess.listen_port,
        advertise_ip: editingProcess.advertise_ip || '',
        cmd_pattern: editingProcess.cmd_pattern || '',
        module_ids: editingProcess.module_ids || [],
        description: editingProcess.description || '',
      });
    } else {
      setForm({
        name: '',
        listen_port: 8080,
        advertise_ip: '',
        cmd_pattern: '',
        module_ids: [],
        description: '',
      });
    }
  }, [editingProcess, open]);

  // 加载公共可用模块
  useEffect(() => {
    if (!open) return;
    void (async () => {
      try {
        const res = await archApi.listModules({ page_size: 100 });
        setAllModules(res.items ?? []);
      } catch (err) {
        toast.error(err instanceof Error ? err.message : '加载架构模块失败');
      }
    })();
  }, [open, toast]);

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

  const handleSave = async () => {
    if (!form.name.trim()) {
      toast.error('请输入进程名称');
      return;
    }
    if (form.module_ids.length === 0) {
      toast.error('请至少挂载一个公共功能模块');
      return;
    }

    try {
      if (editingProcess) {
        await agentApi.updateProcess(editingProcess.id, form);
        toast.success('进程映射已更新');
      } else {
        await agentApi.createProcess({
          agent_id: agent.id,
          ...form,
        });
        toast.success('进程映射已创建');
      }
      onSuccess();
      onClose();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '保存进程失败');
    }
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={editingProcess ? `修改业务进程 [${editingProcess.name}]` : `为 Agent [${agent.name}] 挂载业务进程`}
      width="lg"
      footer={
        <div className="code-modal-footer">
          <button type="button" className="code-btn code-btn--secondary" onClick={onClose}>
            取消
          </button>
          <button type="button" className="code-btn code-btn--primary" onClick={() => void handleSave()}>
            保存进程与模块绑定
          </button>
        </div>
      }
    >
      <div className="code-process-modal-body">
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
          <div className="code-form-grid-2">
            <div className="code-form-item">
              <label className="code-form-label">进程名称:</label>
              <input
                type="text"
                className="code-input"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="如 order_sut_node1"
              />
            </div>
            <div className="code-form-item">
              <label className="code-form-label">监听 TCP 端口 (1024..65535):</label>
              <input
                type="number"
                className="code-input"
                value={form.listen_port}
                onChange={(e) => setForm({ ...form, listen_port: Number(e.target.value) })}
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
            <div className="code-module-checkbox-group">
              {(allModules ?? []).map((m) => {
                const checked = (form.module_ids ?? []).includes(m.id);
                return (
                  <label
                    key={m.id}
                    className={`code-checkbox-chip ${checked ? 'code-checkbox-chip--checked' : ''}`}
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
      </div>
    </Modal>
  );
};
