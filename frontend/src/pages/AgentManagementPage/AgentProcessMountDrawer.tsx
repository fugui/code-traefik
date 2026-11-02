import React, { useState, useEffect, useMemo, useCallback } from 'react';
import { Drawer } from '@code/common';
import {
  Layers,
  Search,
  Zap,
  Sliders,
  ChevronDown,
  ChevronRight,
} from 'lucide-react';
import { useToast } from '../../utils/toast';
import * as agentApi from '../../api/agent';
import * as archApi from '../../api/arch';
import type {
  Agent,
  Process,
  ArchProcessGroup,
  ArchProcess,
  BatchProcessMountItem,
  DiscoveredProcess,
} from '../../types';

export interface AgentProcessMountDrawerProps {
  open: boolean;
  onClose: () => void;
  agent: Agent;
  existingProcesses?: Process[];
  onSuccess: () => void;
}

export interface AgentProcessMountContentProps {
  agent: Agent;
  existingProcesses?: Process[];
  initialArchGroups?: ArchProcessGroup[];
  onSuccess: () => void;
  onClose?: () => void;
}

interface ProcessOverrideForm {
  override_listen_port: string;
  override_cmd_pattern: string;
  advertise_ip: string;
  extra_downstreams: string;
  probe_desired_state: 'ARMED' | 'DISARMED';
  description: string;
}

export const AgentProcessMountContent: React.FC<AgentProcessMountContentProps> = ({
  agent,
  existingProcesses = [],
  initialArchGroups = [],
  onSuccess,
  onClose,
}) => {
  const toast = useToast();
  const [loadingTree, setLoadingTree] = useState(false);
  const [archGroups, setArchGroups] = useState<ArchProcessGroup[]>(initialArchGroups);
  const [searchQuery, setSearchQuery] = useState('');
  const [scanning, setScanning] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  // 选中的架构进程 ID 集合 (惰性初始化以支持 SSR)
  const [selectedProcessIds, setSelectedProcessIds] = useState<Set<string>>(() => {
    const s = new Set<string>();
    existingProcesses.forEach((p) => {
      if (p.arch_process_id) s.add(String(p.arch_process_id));
    });
    return s;
  });

  // 各架构进程的覆盖配置
  const [overrideConfigs, setOverrideConfigs] = useState<Record<string, ProcessOverrideForm>>(() => {
    const cfgs: Record<string, ProcessOverrideForm> = {};
    existingProcesses.forEach((p) => {
      const archProcId = p.arch_process_id ? String(p.arch_process_id) : '';
      if (archProcId) {
        cfgs[archProcId] = {
          override_listen_port:
            p.override_listen_port !== undefined && p.override_listen_port !== null
              ? String(p.override_listen_port)
              : '',
          override_cmd_pattern: p.override_cmd_pattern || '',
          advertise_ip: p.advertise_ip || '127.0.0.1',
          extra_downstreams: (p.extra_downstreams || []).join(', '),
          probe_desired_state: (p.probe_desired_state as 'ARMED' | 'DISARMED') || 'ARMED',
          description: p.description || '',
        };
      }
    });
    return cfgs;
  });

  // 展开配置面板的进程 ID
  const [activeConfigProcId, setActiveConfigProcId] = useState<string>(() => {
    for (const p of existingProcesses) {
      if (p.arch_process_id) return String(p.arch_process_id);
    }
    return '';
  });

  // 折叠的进程组
  const [collapsedGroups, setCollapsedGroups] = useState<Record<string, boolean>>({});

  // 扁平化所有 ArchProcess 映射方便查询
  const allArchProcesses = useMemo(() => {
    const map = new Map<string, ArchProcess>();
    for (const g of archGroups) {
      for (const p of g.processes || []) {
        map.set(String(p.id), p);
      }
    }
    return map;
  }, [archGroups]);

  // 加载标准物理进程树
  const loadTree = useCallback(async () => {
    setLoadingTree(true);
    try {
      const res = await archApi.getArchProcessTree();
      setArchGroups(res.items ?? []);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '加载架构进程树失败');
    } finally {
      setLoadingTree(false);
    }
  }, [toast]);

  useEffect(() => {
    if (initialArchGroups.length === 0) {
      void loadTree();
    }
  }, [loadTree, initialArchGroups.length]);

  // 初始化已有挂载进程
  useEffect(() => {
    const initialSelected = new Set<string>();
    const initialOverrides: Record<string, ProcessOverrideForm> = {};

    existingProcesses.forEach((p) => {
      const archProcId = p.arch_process_id ? String(p.arch_process_id) : '';
      if (archProcId) {
        initialSelected.add(archProcId);
        initialOverrides[archProcId] = {
          override_listen_port:
            p.override_listen_port !== undefined && p.override_listen_port !== null
              ? String(p.override_listen_port)
              : '',
          override_cmd_pattern: p.override_cmd_pattern || '',
          advertise_ip: p.advertise_ip || '127.0.0.1',
          extra_downstreams: (p.extra_downstreams || []).join(', '),
          probe_desired_state: (p.probe_desired_state as 'ARMED' | 'DISARMED') || 'ARMED',
          description: p.description || '',
        };
      }
    });

    setSelectedProcessIds(initialSelected);
    setOverrideConfigs(initialOverrides);

    if (initialSelected.size > 0) {
      setActiveConfigProcId((prev) => prev || Array.from(initialSelected)[0]);
    }
  }, [existingProcesses]);

  // 切换进程勾选状态
  const toggleProcessSelect = (procId: string) => {
    const key = String(procId);
    setSelectedProcessIds((prev) => {
      const next = new Set(prev);
      if (next.has(key)) {
        next.delete(key);
        if (activeConfigProcId === key) {
          const rem = Array.from(next);
          setActiveConfigProcId(rem.length > 0 ? rem[0] : '');
        }
      } else {
        next.add(key);
        setActiveConfigProcId(key);
        // 初始化默认配置表单
        if (!overrideConfigs[key]) {
          const archProc = allArchProcesses.get(key);
          setOverrideConfigs((cur) => ({
            ...cur,
            [key]: {
              override_listen_port: '',
              override_cmd_pattern: '',
              advertise_ip: '127.0.0.1',
              extra_downstreams: '',
              probe_desired_state: 'ARMED',
              description: archProc?.description || '',
            },
          }));
        }
      }
      return next;
    });
  };

  // 一键自动扫描宿主机进程并匹配架构树 (AC-M10-02)
  const handleAutoScanAndMatch = async () => {
    setScanning(true);
    try {
      const res = await agentApi.discoverAgentProcesses(agent.id);
      const discoveredList: DiscoveredProcess[] = res.items ?? [];

      if (discoveredList.length === 0) {
        toast.info('未在目标宿主机上扫描到处于 LISTEN 状态的活跃网络进程');
        return;
      }

      let matchedCount = 0;
      const nextSelected = new Set(selectedProcessIds);
      const nextOverrides = { ...overrideConfigs };

      allArchProcesses.forEach((archProc, procId) => {
        // 匹配逻辑：
        // 1. 默认监听端口精确匹配
        // 2. 命令行特征正则/子串匹配
        const matched = discoveredList.find((disc) => {
          if (archProc.default_listen_port > 0 && disc.listen_port === archProc.default_listen_port) {
            return true;
          }
          if (archProc.default_cmd_pattern) {
            const pat = archProc.default_cmd_pattern;
            if (pat.startsWith('re:')) {
              try {
                const reg = new RegExp(pat.slice(3));
                return reg.test(disc.cmdline);
              } catch {
                return disc.cmdline.includes(pat.slice(3));
              }
            }
            return disc.cmdline.includes(pat);
          }
          return false;
        });

        if (matched) {
          matchedCount++;
          nextSelected.add(procId);
          const currentConfig = nextOverrides[procId] || {
            override_listen_port: '',
            override_cmd_pattern: '',
            advertise_ip: '127.0.0.1',
            extra_downstreams: '',
            probe_desired_state: 'ARMED',
            description: archProc.description || '',
          };

          // 若扫描到的实际端口与架构默认端口不同，自动填入覆盖端口
          if (matched.listen_port > 0 && matched.listen_port !== archProc.default_listen_port) {
            currentConfig.override_listen_port = String(matched.listen_port);
          }
          if (matched.cmdline && !currentConfig.override_cmd_pattern) {
            currentConfig.override_cmd_pattern = matched.cmdline;
          }
          nextOverrides[procId] = currentConfig;
        }
      });

      setSelectedProcessIds(nextSelected);
      setOverrideConfigs(nextOverrides);

      if (nextSelected.size > 0 && !activeConfigProcId) {
        setActiveConfigProcId(Array.from(nextSelected)[0]);
      }

      toast.success(
        `自动扫描成功！共发现 ${discoveredList.length} 个宿主机进程，自动匹配到 ${matchedCount} 个架构进程`
      );
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '自动扫描宿主机进程失败');
    } finally {
      setScanning(false);
    }
  };

  // 提交全量批量挂载 (带 mounts_revision 乐观锁)
  const handleSubmit = async () => {
    const selectedList = Array.from(selectedProcessIds);

    const mounts: BatchProcessMountItem[] = selectedList.map((procId) => {
      const cfg = overrideConfigs[procId] || {
        override_listen_port: '',
        override_cmd_pattern: '',
        advertise_ip: '127.0.0.1',
        extra_downstreams: '',
        probe_desired_state: 'ARMED',
        description: '',
      };

      const portNum =
        cfg.override_listen_port !== '' && cfg.override_listen_port !== undefined
          ? parseInt(cfg.override_listen_port, 10)
          : null;

      const downstreams = cfg.extra_downstreams
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean);

      return {
        arch_process_id: Number(procId),
        override_listen_port: portNum,
        override_cmd_pattern: cfg.override_cmd_pattern.trim(),
        advertise_ip: cfg.advertise_ip.trim() || '127.0.0.1',
        extra_downstreams: downstreams,
        probe_desired_state: cfg.probe_desired_state,
        description: cfg.description.trim(),
      };
    });

    const expectedRevision = agent.mounts_revision || 1;
    setSubmitting(true);
    try {
      const res = await agentApi.batchUpdateProcessMounts(agent.id, {
        expected_revision: expectedRevision,
        mounts,
      });
      toast.success(
        `批量挂载成功！已纳管 ${res.mounts_count} 个进程 (版本 v${res.mounts_revision})，探针自动就绪对账中`
      );
      onSuccess();
      if (onClose) onClose();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '批量更新挂载失败');
    } finally {
      setSubmitting(false);
    }
  };

  // 过滤树
  const filteredGroups = useMemo(() => {
    const s = searchQuery.trim().toLowerCase();
    if (!s) return archGroups;

    return archGroups
      .map((g) => {
        const groupMatch = g.name.toLowerCase().includes(s) || g.code.toLowerCase().includes(s);
        const matchingProcs = (g.processes || []).filter(
          (p) =>
            p.name.toLowerCase().includes(s) ||
            p.code.toLowerCase().includes(s) ||
            String(p.default_listen_port).includes(s)
        );
        if (groupMatch) return g;
        if (matchingProcs.length > 0) {
          return { ...g, processes: matchingProcs };
        }
        return null;
      })
      .filter((g): g is ArchProcessGroup => g !== null);
  }, [archGroups, searchQuery]);

  const activeProcess = activeConfigProcId ? allArchProcesses.get(activeConfigProcId) : null;
  const activeConfig = activeConfigProcId ? overrideConfigs[activeConfigProcId] : null;

  return (
    <div
      className="code-agent-mount-drawer-body"
      data-testid="agent-process-mount-drawer"
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: '1rem',
        height: '100%',
        minHeight: '520px',
      }}
    >
      <div
        style={{
          display: 'flex',
          gap: '1rem',
          flex: 1,
          overflow: 'hidden',
        }}
      >
        {/* 左侧：全局标准架构进程树 (带复选框) */}
        <div
          style={{
            flex: '0 0 320px',
            display: 'flex',
            flexDirection: 'column',
            borderRight: '1px solid var(--color-border-primary)',
            paddingRight: '1rem',
          }}
        >
          {/* 工具栏：搜索与智能扫描 */}
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem', marginBottom: '0.75rem' }}>
            <button
              type="button"
              className="code-btn code-btn--primary code-btn--sm"
              onClick={() => void handleAutoScanAndMatch()}
              disabled={scanning}
              data-testid="auto-scan-procs-btn"
              style={{
                width: '100%',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                gap: '0.4rem',
              }}
            >
              <Zap size={14} />
              <span>{scanning ? '正在探测宿主机...' : '⚡ 自动扫描宿主机进程并匹配架构树'}</span>
            </button>

            <div style={{ position: 'relative' }}>
              <Search
                size={13}
                style={{
                  position: 'absolute',
                  left: '0.6rem',
                  top: '50%',
                  transform: 'translateY(-50%)',
                  color: 'var(--color-text-muted)',
                }}
              />
              <input
                type="text"
                className="code-input"
                placeholder="搜索标准架构进程..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                style={{ paddingLeft: '1.8rem', fontSize: '0.8rem', height: '30px' }}
              />
            </div>
          </div>

          {/* 进程树列表 */}
          <div style={{ flex: 1, overflowY: 'auto' }}>
            {loadingTree ? (
              <div style={{ padding: '2rem', textAlign: 'center', color: 'var(--color-text-muted)' }}>
                加载架构进程树...
              </div>
            ) : filteredGroups.length === 0 ? (
              <div style={{ padding: '2rem', textAlign: 'center', color: 'var(--color-text-muted)' }}>
                暂无标准架构进程
              </div>
            ) : (
              filteredGroups.map((g) => {
                const isCollapsed = Boolean(collapsedGroups[String(g.id)]) && !searchQuery;
                const procs = g.processes || [];

                return (
                  <div
                    key={g.id}
                    style={{
                      marginBottom: '0.5rem',
                      borderRadius: 'var(--radius-sm)',
                      border: '1px solid var(--color-border-secondary)',
                      background: 'var(--color-bg-base)',
                    }}
                  >
                    <div
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'space-between',
                        padding: '0.35rem 0.5rem',
                        cursor: 'pointer',
                        background: 'var(--color-bg-muted)',
                        userSelect: 'none',
                      }}
                      onClick={() =>
                        setCollapsedGroups((prev) => ({
                          ...prev,
                          [String(g.id)]: !prev[String(g.id)],
                        }))
                      }
                    >
                      <div style={{ display: 'flex', alignItems: 'center', gap: '0.3rem' }}>
                        <span style={{ color: 'var(--color-text-muted)' }}>
                          {isCollapsed ? <ChevronRight size={12} /> : <ChevronDown size={12} />}
                        </span>
                        <Layers size={13} style={{ color: 'var(--color-primary)' }} />
                        <span style={{ fontSize: '0.8rem', fontWeight: 600 }}>{g.name}</span>
                      </div>
                      <span className="code-badge code-badge--secondary" style={{ fontSize: '0.675rem' }}>
                        {procs.length}
                      </span>
                    </div>

                    {!isCollapsed && (
                      <div style={{ padding: '0.25rem' }}>
                        {procs.map((p) => {
                          const isChecked = selectedProcessIds.has(String(p.id));
                          const isActive = activeConfigProcId === String(p.id);

                          return (
                            <div
                              key={p.id}
                              style={{
                                display: 'flex',
                                alignItems: 'center',
                                justifyContent: 'space-between',
                                padding: '0.35rem 0.45rem',
                                borderRadius: 'var(--radius-sm)',
                                marginBottom: '2px',
                                background: isActive
                                  ? 'var(--color-bg-hover)'
                                  : 'transparent',
                                border: isActive
                                  ? '1px solid var(--color-primary)'
                                  : '1px solid transparent',
                                cursor: 'pointer',
                              }}
                              onClick={() => {
                                if (isChecked) {
                                  setActiveConfigProcId(String(p.id));
                                } else {
                                  toggleProcessSelect(String(p.id));
                                }
                              }}
                              data-testid={`mount-item-${p.id}`}
                            >
                              <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', minWidth: 0 }}>
                                <input
                                  type="checkbox"
                                  checked={isChecked}
                                  onChange={() => toggleProcessSelect(String(p.id))}
                                  onClick={(e) => e.stopPropagation()}
                                  data-testid={`checkbox-proc-${p.id}`}
                                />
                                <div style={{ display: 'flex', flexDirection: 'column', minWidth: 0 }}>
                                  <span
                                    style={{
                                      fontSize: '0.8rem',
                                      fontWeight: isChecked ? 600 : 400,
                                      color: isChecked
                                        ? 'var(--color-text-primary)'
                                        : 'var(--color-text-secondary)',
                                      whiteSpace: 'nowrap',
                                      overflow: 'hidden',
                                      textOverflow: 'ellipsis',
                                    }}
                                  >
                                    {p.name}
                                  </span>
                                  <span
                                    className="code-font-mono"
                                    style={{ fontSize: '0.675rem', color: 'var(--color-text-muted)' }}
                                  >
                                    {p.code}
                                  </span>
                                </div>
                              </div>

                              <span
                                className="code-font-mono"
                                style={{
                                  fontSize: '0.675rem',
                                  padding: '1px 4px',
                                  borderRadius: 'var(--radius-sm)',
                                  background: 'var(--color-bg-surface)',
                                  border: '1px solid var(--color-border-primary)',
                                }}
                              >
                                {p.default_listen_port === 0 ? '客户端' : `:${p.default_listen_port}`}
                              </span>
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

        {/* 右侧：选中进程的细粒度覆盖配置面板 */}
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflowY: 'auto' }}>
          {activeProcess && activeConfig ? (
            <div>
              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  paddingBottom: '0.75rem',
                  marginBottom: '1rem',
                  borderBottom: '1px solid var(--color-border-primary)',
                }}
              >
                <div>
                  <h4 style={{ margin: 0, fontSize: '1rem', fontWeight: 600 }}>
                    配置挂载实例 - [{activeProcess.name}]
                  </h4>
                  <span className="code-font-mono code-color-muted code-text-xs">
                    标准代码: {activeProcess.code} | 默认端口:{' '}
                    {activeProcess.default_listen_port === 0 ? '0 (纯客户端)' : activeProcess.default_listen_port}
                  </span>
                </div>
                <span className="code-badge code-badge--success" style={{ fontSize: '0.75rem' }}>
                  已勾选纳管
                </span>
              </div>

              <div className="code-form">
                <div className="code-form-item">
                  <label className="code-form-label">
                    覆盖监听端口 (Override Listen Port, 留空表示继承默认端口 {activeProcess.default_listen_port}):
                  </label>
                  <input
                    type="number"
                    className="code-input"
                    value={activeConfig.override_listen_port}
                    onChange={(e) =>
                      setOverrideConfigs((cur) => ({
                        ...cur,
                        [activeConfigProcId]: {
                          ...cur[activeConfigProcId],
                          override_listen_port: e.target.value,
                        },
                      }))
                    }
                    placeholder={`默认: ${activeProcess.default_listen_port}`}
                    min={0}
                    max={65535}
                    data-testid="input-override-port"
                  />
                </div>

                <div className="code-form-item">
                  <label className="code-form-label">
                    覆盖命令行匹配特征 (留空继承默认: {activeProcess.default_cmd_pattern || '无'}):
                  </label>
                  <input
                    type="text"
                    className="code-input"
                    value={activeConfig.override_cmd_pattern}
                    onChange={(e) =>
                      setOverrideConfigs((cur) => ({
                        ...cur,
                        [activeConfigProcId]: {
                          ...cur[activeConfigProcId],
                          override_cmd_pattern: e.target.value,
                        },
                      }))
                    }
                    placeholder={`默认: ${activeProcess.default_cmd_pattern || '无'}`}
                    data-testid="input-override-cmd"
                  />
                </div>

                <div className="code-form-item">
                  <label className="code-form-label">广播绑定 IP (Advertise IP):</label>
                  <input
                    type="text"
                    className="code-input"
                    value={activeConfig.advertise_ip}
                    onChange={(e) =>
                      setOverrideConfigs((cur) => ({
                        ...cur,
                        [activeConfigProcId]: {
                          ...cur[activeConfigProcId],
                          advertise_ip: e.target.value,
                        },
                      }))
                    }
                    placeholder="127.0.0.1"
                  />
                </div>

                <div className="code-form-item">
                  <label className="code-form-label">探针期望状态 (Desired State):</label>
                  <select
                    className="code-select"
                    value={activeConfig.probe_desired_state}
                    onChange={(e) =>
                      setOverrideConfigs((cur) => ({
                        ...cur,
                        [activeConfigProcId]: {
                          ...cur[activeConfigProcId],
                          probe_desired_state: e.target.value as 'ARMED' | 'DISARMED',
                        },
                      }))
                    }
                    data-testid="select-desired-state"
                  >
                    <option value="ARMED">ARMED (激活探针并待命录制/回放)</option>
                    <option value="DISARMED">DISARMED (探针已就绪但未激活)</option>
                  </select>
                </div>

                <div className="code-form-item">
                  <label className="code-form-label">附加下游依赖端点 (逗号分隔):</label>
                  <input
                    type="text"
                    className="code-input"
                    value={activeConfig.extra_downstreams}
                    onChange={(e) =>
                      setOverrideConfigs((cur) => ({
                        ...cur,
                        [activeConfigProcId]: {
                          ...cur[activeConfigProcId],
                          extra_downstreams: e.target.value,
                        },
                      }))
                    }
                    placeholder="如 127.0.0.1:9092, 10.0.0.1:8080"
                  />
                </div>

                <div className="code-form-item">
                  <label className="code-form-label">实例挂载备注:</label>
                  <textarea
                    className="code-textarea"
                    value={activeConfig.description}
                    onChange={(e) =>
                      setOverrideConfigs((cur) => ({
                        ...cur,
                        [activeConfigProcId]: {
                          ...cur[activeConfigProcId],
                          description: e.target.value,
                        },
                      }))
                    }
                    placeholder="说明该环境实例的特殊配置..."
                  />
                </div>
              </div>
            </div>
          ) : (
            <div
              style={{
                flex: 1,
                display: 'flex',
                flexDirection: 'column',
                alignItems: 'center',
                justifyContent: 'center',
                color: 'var(--color-text-muted)',
                padding: '2rem',
                textAlign: 'center',
              }}
            >
              <Sliders size={36} style={{ marginBottom: '0.75rem', opacity: 0.5 }} />
              <p style={{ margin: 0, fontSize: '0.875rem' }}>
                请在左侧勾选需要纳管的架构进程以配置细粒度端口与命令行参数
              </p>
            </div>
          )}
        </div>
      </div>

      {/* 底部操作栏 */}
      <div
        className="code-drawer-footer"
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          paddingTop: '0.75rem',
          borderTop: '1px solid var(--color-border-primary)',
        }}
      >
        <div style={{ fontSize: '0.85rem', color: 'var(--color-text-secondary)' }}>
          已勾选挂载: <strong style={{ color: 'var(--color-primary)' }}>{selectedProcessIds.size}</strong> 个架构进程
        </div>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          {onClose && (
            <button
              type="button"
              className="code-btn code-btn--secondary"
              onClick={onClose}
              disabled={submitting}
            >
              取消
            </button>
          )}
          <button
            type="button"
            className="code-btn code-btn--primary"
            onClick={() => void handleSubmit()}
            disabled={submitting}
            data-testid="save-mounts-btn"
          >
            {submitting ? '保存对账中...' : '确认全量挂载并同步探针'}
          </button>
        </div>
      </div>
    </div>
  );
};

export const AgentProcessMountDrawer: React.FC<AgentProcessMountDrawerProps> = ({
  open,
  onClose,
  agent,
  existingProcesses,
  onSuccess,
}) => {
  return (
    <Drawer
      open={open}
      onClose={onClose}
      title={`批量挂载架构进程 - [${agent.name}]`}
      subtitle={`作用域: ${agent.scope} | 当前版本: v${agent.mounts_revision || 1} | 端点: ${agent.endpoint}`}
      width="xl"
    >
      <AgentProcessMountContent
        agent={agent}
        existingProcesses={existingProcesses}
        onSuccess={onSuccess}
        onClose={onClose}
      />
    </Drawer>
  );
};

export default AgentProcessMountDrawer;
