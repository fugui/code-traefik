import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Plus, Server } from 'lucide-react';
import { Modal } from '@code/common';
import { useToast } from '../../utils/toast';
import { useModuleUser } from '../../context/ModuleUserContext';
import * as archApi from '../../api/arch';
import * as agentApi from '../../api/agent';
import * as trafficApi from '../../api/traffic';
import type {
  Subsystem,
  FunctionalGroupTreeNode,
  Module,
  ModuleInterface,
  StaticTopologyDTO,
  ConsistencyReportDTO,
  RecordingSession,
  Process,
  Agent,
} from '../../types';
import { SubsystemGroupTreePanel, TreeViewMode } from './SubsystemGroupTreePanel';
import { ModuleContractDetailPane } from './ModuleContractDetailPane';
import { ProcessDetailPane } from './ProcessDetailPane';
import { StaticTopologyCanvas } from './StaticTopologyCanvas';
import { ConsistencyVerifyOverlay } from './ConsistencyVerifyOverlay';

function findGroupInTree(
  nodes: FunctionalGroupTreeNode[],
  targetId: string
): FunctionalGroupTreeNode | null {
  for (const node of nodes) {
    if (node.id === targetId) return node;
    if (node.children && node.children.length > 0) {
      const found = findGroupInTree(node.children, targetId);
      if (found) return found;
    }
  }
  return null;
}

export const ArchitectureWorkbenchPage: React.FC = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const { user } = useModuleUser();
  const toast = useToast();
  const isAdmin = Boolean(user?.is_module_admin || user?.is_super_admin);

  // URL 状态同步
  const activeTab = (searchParams.get('tab') as 'logical' | 'physical' | 'verify') || 'logical';
  const urlSubsystemId = searchParams.get('subsystem_id') || '';
  const urlSessionId = searchParams.get('session_id') || '';

  // 基础数据状态
  const [subsystems, setSubsystems] = useState<Subsystem[]>([]);
  const [selectedSubsystemId, setSelectedSubsystemId] = useState<string>(urlSubsystemId);
  const [groupTree, setGroupTree] = useState<FunctionalGroupTreeNode[]>([]);
  const [groupTreeMap, setGroupTreeMap] = useState<Record<string, FunctionalGroupTreeNode[]>>({});
  const [selectedGroupId, setSelectedGroupId] = useState<string>('');
  const [modules, setModules] = useState<Module[]>([]);
  const [selectedModuleId, setSelectedModuleId] = useState<string>('');
  const [selectedModule, setSelectedModule] = useState<Module | null>(null);
  const [interfaces, setInterfaces] = useState<ModuleInterface[]>([]);

  // 拓扑与一致性数据
  const [topology, setTopology] = useState<StaticTopologyDTO | null>(null);
  const [topologyView, setTopologyView] = useState<'LOGICAL' | 'PHYSICAL'>('PHYSICAL');
  const [loadingTopo, setLoadingTopo] = useState(false);
  const [sessions, setSessions] = useState<RecordingSession[]>([]);
  const [selectedSessionId, setSelectedSessionId] = useState<string>(urlSessionId);
  const [consistencyReport, setConsistencyReport] = useState<ConsistencyReportDTO | null>(null);
  const [loadingReport, setLoadingReport] = useState(false);

  // 弹窗与表单状态
  const [subsystemModalOpen, setSubsystemModalOpen] = useState(false);
  const [editingSubsystem, setEditingSubsystem] = useState<Subsystem | null>(null);
  const [subsystemForm, setSubsystemForm] = useState({ code: '', name: '', description: '' });

  const [groupModalOpen, setGroupModalOpen] = useState(false);
  const [groupParentId, setGroupParentId] = useState<string | null>(null);
  const [editingGroup, setEditingGroup] = useState<FunctionalGroupTreeNode | null>(null);
  const [groupForm, setGroupForm] = useState({ code: '', name: '', description: '' });

  const [moveModalOpen, setMoveModalOpen] = useState(false);
  const [movingGroup, setMovingGroup] = useState<FunctionalGroupTreeNode | null>(null);
  const [newParentId, setNewParentId] = useState<string>('');

  // 视角模式状态（逻辑业务树 vs 物理架构树）
  const [treeViewMode, setTreeViewMode] = useState<TreeViewMode>('LOGICAL');

  // 物理进程数据状态
  const [processes, setProcesses] = useState<Process[]>([]);
  const [selectedProcessId, setSelectedProcessId] = useState<string>('');
  const [agents, setAgents] = useState<Agent[]>([]);

  // 进程弹窗状态
  const [processModalOpen, setProcessModalOpen] = useState(false);
  const [editingProcess, setEditingProcess] = useState<Process | null>(null);
  const [processForm, setProcessForm] = useState({
    group_name: '',
    name: '',
    listen_port: '',
    agent_id: '',
    cmd_pattern: '',
    description: '',
  });

  // FM 模块编辑弹窗（增加 process_id 与快速新建进程面板状态）
  const [moduleModalOpen, setModuleModalOpen] = useState(false);
  const [editingMod, setEditingMod] = useState<Module | null>(null);
  const [moduleForm, setModuleForm] = useState({ code: '', name: '', description: '', process_id: '' });
  const [quickProcessOpen, setQuickProcessOpen] = useState(false);
  const [quickProcessForm, setQuickProcessForm] = useState({ group_name: '', name: '', listen_port: '' });

  const [ifaceModalOpen, setIfaceModalOpen] = useState(false);
  const [ifaceForm, setIfaceForm] = useState<{
    relation_type: 'PROVIDE' | 'DEPEND';
    service_name: string;
    operation: string;
    diff_ignore_paths: string;
    description: string;
  }>({
    relation_type: 'PROVIDE',
    service_name: '',
    operation: '',
    diff_ignore_paths: '',
    description: '',
  });

  // 更新 URL 辅助函数
  const updateParams = useCallback(
    (updates: Record<string, string | undefined>) => {
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev);
        Object.entries(updates).forEach(([k, v]) => {
          if (v) next.set(k, v);
          else next.delete(k);
        });
        return next;
      });
    },
    [setSearchParams]
  );

  // 加载子系统列表
  const loadSubsystems = useCallback(async () => {
    try {
      const res = await archApi.listSubsystems({ page_size: 100 });
      const items = res.items ?? [];
      setSubsystems(items);
      if (items.length > 0) {
        setSelectedSubsystemId((prev) => {
          if (!prev) {
            updateParams({ subsystem_id: items[0].id });
            return items[0].id;
          }
          return prev;
        });
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '加载子系统失败');
    }
  }, [updateParams, toast]);

  useEffect(() => {
    void loadSubsystems();
  }, [loadSubsystems]);

  // 加载物理进程列表
  const loadProcesses = useCallback(async () => {
    try {
      const res = await agentApi.listProcesses({ page_size: 1000 });
      const items = res.items ?? [];
      setProcesses(items);
      if (items.length > 0) {
        setSelectedProcessId((prev) => {
          if (prev && items.some((p) => p.id === prev)) return prev;
          return items[0].id;
        });
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '加载进程失败');
    }
  }, [toast]);

  // 加载可用 Agent 宿主列表
  const loadAgents = useCallback(async () => {
    try {
      const res = await agentApi.listAgents({ page_size: 100 });
      setAgents(res.items ?? []);
    } catch {
      // 容错处理
    }
  }, []);

  useEffect(() => {
    void loadProcesses();
    void loadAgents();
  }, [loadProcesses, loadAgents]);

  // 进程按 group_name 分组映射
  const processGroupsMap = useMemo(() => {
    const map: Record<string, Process[]> = {};
    for (const p of processes) {
      const grp = (p.group_name || '默认进程组').trim();
      if (!map[grp]) map[grp] = [];
      map[grp].push(p);
    }
    return map;
  }, [processes]);

  // 当前选中的物理进程对象
  const selectedProcess = useMemo(() => {
    return processes.find((p) => p.id === selectedProcessId) || null;
  }, [processes, selectedProcessId]);

  const loadSubsystemTree = useCallback(
    async (subsystemId: string) => {
      try {
        const res = await archApi.getFunctionalGroupTree(subsystemId);
        const safeTree = res.tree ?? [];
        setGroupTreeMap((prev) => ({ ...prev, [subsystemId]: safeTree }));
        return safeTree;
      } catch (err) {
        toast.error(err instanceof Error ? err.message : '加载功能组失败');
        return [];
      }
    },
    [toast]
  );

  // 当子系统切换时，加载功能组树
  useEffect(() => {
    if (!selectedSubsystemId) return;
    void (async () => {
      try {
        const res = await archApi.getFunctionalGroupTree(selectedSubsystemId);
        const safeTree = res.tree ?? [];
        setGroupTree(safeTree);
        setGroupTreeMap((prev) => ({ ...prev, [selectedSubsystemId]: safeTree }));
        if (safeTree.length > 0) {
          setSelectedGroupId((prev) => (findGroupInTree(safeTree, prev) ? prev : safeTree[0].id));
        } else {
          setSelectedGroupId('');
          setModules([]);
          setSelectedModule(null);
        }
      } catch (err) {
        toast.error(err instanceof Error ? err.message : '加载功能组失败');
      }
    })();
  }, [selectedSubsystemId, toast]);

  // 当功能组切换时，加载模块列表
  useEffect(() => {
    if (!selectedGroupId) return;
    void (async () => {
      try {
        const res = await archApi.listModules({ group_id: selectedGroupId, page_size: 100 });
        const items = res.items ?? [];
        setModules(items);
        if (items.length > 0) {
          setSelectedModuleId(items[0].id);
        } else {
          setSelectedModuleId('');
          setSelectedModule(null);
          setInterfaces([]);
        }
      } catch (err) {
        toast.error(err instanceof Error ? err.message : '加载模块失败');
      }
    })();
  }, [selectedGroupId, toast]);

  // 当模块切换时，加载模块详情与接口契约
  useEffect(() => {
    if (!selectedModuleId) return;
    void (async () => {
      try {
        const res = await archApi.getModule(selectedModuleId);
        setSelectedModule(res.module);
        setInterfaces(res.interfaces ?? []);
      } catch (err) {
        toast.error(err instanceof Error ? err.message : '加载模块契约失败');
      }
    })();
  }, [selectedModuleId, toast]);

  // 拓扑加载
  const loadTopology = useCallback(
    async (viewMode: 'LOGICAL' | 'PHYSICAL') => {
      if (!selectedSubsystemId) return;
      setLoadingTopo(true);
      try {
        const res = await archApi.getStaticTopology({
          view: viewMode,
          subsystem_id: selectedSubsystemId,
        });
        setTopology(res);
      } catch (err) {
        toast.error(err instanceof Error ? err.message : '加载静态拓扑失败');
      } finally {
        setLoadingTopo(false);
      }
    },
    [selectedSubsystemId, toast]
  );

  useEffect(() => {
    if (activeTab === 'physical') void loadTopology(topologyView);
  }, [activeTab, topologyView, loadTopology]);

  // 一致性报告加载
  useEffect(() => {
    if (activeTab !== 'verify') return;
    void (async () => {
      try {
        const res = await trafficApi.listRecordingSessions({ page_size: 50 });
        const items = res.items ?? [];
        setSessions(items);
        if (items.length > 0) {
          setSelectedSessionId((prev) => prev || items[0].id);
        }
      } catch (err) {
        toast.error(err instanceof Error ? err.message : '加载录制批次失败');
      }
    })();
  }, [activeTab, toast]);

  const loadConsistency = useCallback(async () => {
    if (!selectedSessionId) return;
    setLoadingReport(true);
    try {
      const res = await trafficApi.getConsistencyReport(selectedSessionId);
      setConsistencyReport(res);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '比对一致性报告失败');
    } finally {
      setLoadingReport(false);
    }
  }, [selectedSessionId, toast]);

  useEffect(() => {
    if (activeTab === 'verify' && selectedSessionId) {
      void loadConsistency();
    }
  }, [activeTab, selectedSessionId, loadConsistency]);

  // 子系统 CRUD
  const handleSaveSubsystem = async () => {
    try {
      if (editingSubsystem) {
        await archApi.updateSubsystem(editingSubsystem.id, subsystemForm);
        toast.success('子系统更新成功');
      } else {
        await archApi.createSubsystem(subsystemForm);
        toast.success('子系统创建成功');
      }
      setSubsystemModalOpen(false);
      void loadSubsystems();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '操作子系统失败');
    }
  };

  const handleDeleteSubsystem = async (id: string) => {
    try {
      await archApi.deleteSubsystem(id);
      toast.success('子系统已删除');
      setSelectedSubsystemId('');
      void loadSubsystems();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '删除子系统失败');
    }
  };

  // 功能组 CRUD
  const handleSaveGroup = async () => {
    if (!selectedSubsystemId) return;
    try {
      if (editingGroup) {
        await archApi.updateFunctionalGroup(editingGroup.id, groupForm);
        toast.success('功能组更新成功');
      } else {
        await archApi.createFunctionalGroup({
          subsystem_id: selectedSubsystemId,
          parent_id: groupParentId,
          ...groupForm,
        });
        toast.success('功能组创建成功');
      }
      setGroupModalOpen(false);
      const res = await archApi.getFunctionalGroupTree(selectedSubsystemId);
      const safeTree = res.tree ?? [];
      setGroupTree(safeTree);
      setGroupTreeMap((prev) => ({ ...prev, [selectedSubsystemId]: safeTree }));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '操作功能组失败');
    }
  };

  const handleMoveGroup = async () => {
    if (!movingGroup) return;
    try {
      await archApi.moveFunctionalGroup(movingGroup.id, {
        new_parent_id: newParentId ? newParentId : null,
      });
      toast.success('功能组层级调整成功');
      setMoveModalOpen(false);
      const res = await archApi.getFunctionalGroupTree(selectedSubsystemId);
      const safeTree = res.tree ?? [];
      setGroupTree(safeTree);
      setGroupTreeMap((prev) => ({ ...prev, [selectedSubsystemId]: safeTree }));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '移动功能组失败');
    }
  };

  const handleDeleteGroup = async (id: string) => {
    try {
      await archApi.deleteFunctionalGroup(id);
      toast.success('功能组已删除');
      const res = await archApi.getFunctionalGroupTree(selectedSubsystemId);
      const safeTree = res.tree ?? [];
      setGroupTree(safeTree);
      setGroupTreeMap((prev) => ({ ...prev, [selectedSubsystemId]: safeTree }));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '删除功能组失败');
    }
  };

  // 模块 CRUD
  const handleSaveModule = async () => {
    if (!selectedGroupId) return;
    try {
      const procId = moduleForm.process_id ? moduleForm.process_id : null;
      if (editingMod) {
        await archApi.updateModule(editingMod.id, {
          group_id: selectedGroupId,
          code: moduleForm.code,
          name: moduleForm.name,
          description: moduleForm.description,
          process_id: procId,
        });
        toast.success('模块修改成功');
      } else {
        await archApi.createModule({
          group_id: selectedGroupId,
          code: moduleForm.code,
          name: moduleForm.name,
          description: moduleForm.description,
          process_id: procId,
        });
        toast.success('模块创建成功');
      }
      setModuleModalOpen(false);
      const res = await archApi.listModules({ group_id: selectedGroupId, page_size: 100 });
      setModules(res.items);
      if (selectedModuleId) {
        const modRes = await archApi.getModule(selectedModuleId);
        setSelectedModule(modRes.module);
      }
      void loadProcesses();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '操作模块失败');
    }
  };

  // 在创建/编辑 FM 模块时，快速创建物理进程并直接绑定
  const handleQuickCreateProcess = async () => {
    if (!quickProcessForm.name.trim()) {
      toast.error('请输入进程名称');
      return;
    }
    const port = parseInt(quickProcessForm.listen_port, 10);
    if (isNaN(port) || port < 1 || port > 65535) {
      toast.error('监听端口必须为 1~65535 之间的合法数字');
      return;
    }
    try {
      const defaultAgentId = agents.length > 0 ? agents[0].id : '';
      const newProc = await agentApi.createProcess({
        agent_id: defaultAgentId,
        group_name: quickProcessForm.group_name.trim() || '默认进程组',
        name: quickProcessForm.name.trim(),
        listen_port: port,
        cmd_pattern: `./bin/${quickProcessForm.name.trim()}`,
      });
      toast.success(`进程 [${newProc.name}] 创建成功并已自动关联`);
      const res = await agentApi.listProcesses({ page_size: 1000 });
      const items = res.items ?? [];
      setProcesses(items);
      setModuleForm((prev) => ({ ...prev, process_id: newProc.id }));
      setQuickProcessOpen(false);
      setQuickProcessForm({ group_name: '', name: '', listen_port: '' });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '快速创建进程失败');
    }
  };

  // 物理进程独立 CRUD
  const handleSaveProcess = async () => {
    if (!processForm.name.trim()) {
      toast.error('请输入进程名称');
      return;
    }
    const port = parseInt(processForm.listen_port, 10);
    if (isNaN(port) || port < 1 || port > 65535) {
      toast.error('监听端口必须为 1~65535 之间的合法数字');
      return;
    }
    try {
      if (editingProcess) {
        await agentApi.updateProcess(editingProcess.id, {
          name: processForm.name.trim(),
          group_name: processForm.group_name.trim() || '默认进程组',
          listen_port: port,
          cmd_pattern: processForm.cmd_pattern.trim() || `./bin/${processForm.name.trim()}`,
          description: processForm.description.trim(),
        });
        toast.success('进程更新成功');
      } else {
        const defaultAgentId = processForm.agent_id || (agents.length > 0 ? agents[0].id : '');
        const newProc = await agentApi.createProcess({
          agent_id: defaultAgentId,
          group_name: processForm.group_name.trim() || '默认进程组',
          name: processForm.name.trim(),
          listen_port: port,
          cmd_pattern: processForm.cmd_pattern.trim() || `./bin/${processForm.name.trim()}`,
          description: processForm.description.trim(),
        });
        toast.success('进程创建成功');
        setSelectedProcessId(newProc.id);
      }
      setProcessModalOpen(false);
      void loadProcesses();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '保存进程失败');
    }
  };

  const handleDeleteProcess = async (id: string) => {
    try {
      await agentApi.deleteProcess(id);
      toast.success('物理进程已删除');
      if (selectedProcessId === id) {
        setSelectedProcessId('');
      }
      void loadProcesses();
      if (selectedModuleId) {
        const modRes = await archApi.getModule(selectedModuleId);
        setSelectedModule(modRes.module);
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '删除物理进程失败');
    }
  };

  const handleDeleteModule = async (id: string) => {
    try {
      await archApi.deleteModule(id);
      toast.success('模块已删除');
      const res = await archApi.listModules({ group_id: selectedGroupId, page_size: 100 });
      setModules(res.items);
      setSelectedModule(null);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '删除模块失败');
    }
  };

  // 接口契约 CRUD
  const handleSaveInterface = async () => {
    if (!selectedModuleId) return;
    try {
      const paths = ifaceForm.diff_ignore_paths
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean);
      await archApi.createModuleInterface(selectedModuleId, {
        relation_type: ifaceForm.relation_type,
        service_name: ifaceForm.service_name,
        operation: ifaceForm.operation,
        diff_ignore_paths: paths,
        description: ifaceForm.description,
      });
      toast.success('接口契约添加成功');
      setIfaceModalOpen(false);
      const res = await archApi.getModule(selectedModuleId);
      setInterfaces(res.interfaces);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '添加接口契约失败');
    }
  };

  const handleDeleteInterface = async (ifaceId: string) => {
    try {
      await archApi.deleteModuleInterface(ifaceId);
      toast.success('接口契约已删除');
      const res = await archApi.getModule(selectedModuleId);
      setInterfaces(res.interfaces);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '删除接口契约失败');
    }
  };

  const handleUpdateIgnorePaths = async (ifaceId: string, paths: string[]) => {
    try {
      await archApi.updateModuleInterface(ifaceId, { diff_ignore_paths: paths });
      toast.success('忽略路径已更新');
      const res = await archApi.getModule(selectedModuleId);
      setInterfaces(res.interfaces);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '更新忽略路径失败');
    }
  };

  return (
    <div className="code-workbench-page code-arch-workbench" data-testid="architecture-workbench">
      {/* 顶部标签页切换 */}
      <div className="code-workbench-header">
        <div className="code-workbench-title-group">
          <h2 className="code-workbench-title">三级架构模型与拓扑中心</h2>
          <span className="code-workbench-subtitle">
            维护子系统、8 级功能组与模块接口契约 (PROVIDE/DEPEND)，推导静态拓扑并执行六分类一致性回归
          </span>
        </div>

        <div className="code-tab-pills">
          <button
            type="button"
            className={`code-pill-btn ${activeTab === 'logical' ? 'code-pill-btn--active' : ''}`}
            onClick={() => updateParams({ tab: 'logical' })}
          >
            架构建模与模块契约
          </button>
          <button
            type="button"
            className={`code-pill-btn ${activeTab === 'physical' ? 'code-pill-btn--active' : ''}`}
            onClick={() => updateParams({ tab: 'physical' })}
          >
            静态拓扑视图
          </button>
          <button
            type="button"
            className={`code-pill-btn ${activeTab === 'verify' ? 'code-pill-btn--active' : ''}`}
            onClick={() => updateParams({ tab: 'verify' })}
          >
            架构一致性比对
          </button>
        </div>
      </div>

      {/* 视图主体 */}
      <div className="code-workbench-body">
        {activeTab === 'logical' && (
          <div className="code-arch-split-layout">
            <SubsystemGroupTreePanel
              viewMode={treeViewMode}
              onViewModeChange={setTreeViewMode}
              subsystems={subsystems}
              selectedSubsystemId={selectedSubsystemId}
              onSelectSubsystem={(id) => {
                setSelectedSubsystemId(id);
                updateParams({ subsystem_id: id });
              }}
              groupTree={groupTree}
              groupTreeMap={groupTreeMap}
              onLoadSubsystemTree={loadSubsystemTree}
              selectedGroupId={selectedGroupId}
              onSelectGroup={setSelectedGroupId}
              processes={processes}
              selectedProcessId={selectedProcessId}
              onSelectProcess={(id) => setSelectedProcessId(id)}
              onCreateProcess={() => {
                setEditingProcess(null);
                setProcessForm({
                  group_name: '',
                  name: '',
                  listen_port: '',
                  agent_id: agents.length > 0 ? agents[0].id : '',
                  cmd_pattern: '',
                  description: '',
                });
                setProcessModalOpen(true);
              }}
              onEditProcess={(proc) => {
                setEditingProcess(proc);
                setProcessForm({
                  group_name: proc.group_name || '默认进程组',
                  name: proc.name,
                  listen_port: String(proc.listen_port),
                  agent_id: proc.agent_id || '',
                  cmd_pattern: proc.cmd_pattern || '',
                  description: proc.description || '',
                });
                setProcessModalOpen(true);
              }}
              onDeleteProcess={(id) => void handleDeleteProcess(id)}
              onSelectModule={(modId) => {
                setSelectedModuleId(modId);
                setTreeViewMode('LOGICAL');
              }}
              isAdmin={isAdmin}
              onCreateSubsystem={() => {
                setEditingSubsystem(null);
                setSubsystemForm({ code: '', name: '', description: '' });
                setSubsystemModalOpen(true);
              }}
              onEditSubsystem={(sub) => {
                setEditingSubsystem(sub);
                setSubsystemForm({ code: sub.code, name: sub.name, description: sub.description || '' });
                setSubsystemModalOpen(true);
              }}
              onDeleteSubsystem={(id) => void handleDeleteSubsystem(id)}
              onCreateGroup={(parentId, targetSubsystemId) => {
                if (targetSubsystemId && targetSubsystemId !== selectedSubsystemId) {
                  setSelectedSubsystemId(targetSubsystemId);
                  updateParams({ subsystem_id: targetSubsystemId });
                }
                setEditingGroup(null);
                setGroupParentId(parentId ?? null);
                setGroupForm({ code: '', name: '', description: '' });
                setGroupModalOpen(true);
              }}
              onEditGroup={(grp) => {
                setEditingGroup(grp);
                setGroupForm({ code: grp.code, name: grp.name, description: grp.description || '' });
                setGroupModalOpen(true);
              }}
              onMoveGroup={(grp) => {
                setMovingGroup(grp);
                setNewParentId('');
                setMoveModalOpen(true);
              }}
              onDeleteGroup={(id) => void handleDeleteGroup(id)}
            />

            {treeViewMode === 'PHYSICAL' ? (
              selectedProcess ? (
                <ProcessDetailPane
                  process={selectedProcess}
                  allModules={modules}
                  isAdmin={isAdmin}
                  onEditProcess={(proc) => {
                    setEditingProcess(proc);
                    setProcessForm({
                      group_name: proc.group_name || '默认进程组',
                      name: proc.name,
                      listen_port: String(proc.listen_port),
                      agent_id: proc.agent_id || '',
                      cmd_pattern: proc.cmd_pattern || '',
                      description: proc.description || '',
                    });
                    setProcessModalOpen(true);
                  }}
                  onDeleteProcess={(id) => void handleDeleteProcess(id)}
                  onSelectModule={(modId) => {
                    setSelectedModuleId(modId);
                    setTreeViewMode('LOGICAL');
                  }}
                />
              ) : (
                <div className="code-module-pane code-empty-pane" style={{ padding: '3.5rem 1.5rem', textAlign: 'center' }}>
                  <Server size={40} style={{ color: 'var(--color-text-muted)', marginBottom: '1rem' }} />
                  <h4 style={{ color: 'var(--color-text-primary)', marginBottom: '0.5rem' }}>未选择物理进程</h4>
                  <p style={{ color: 'var(--color-text-secondary)', fontSize: '0.875rem' }}>
                    请在左侧物理架构树中选择一个进程，或点击上方「+ 进程」新建物理进程
                  </p>
                </div>
              )
            ) : (
              <ModuleContractDetailPane
                modules={modules}
                selectedModuleId={selectedModuleId}
                onSelectModule={setSelectedModuleId}
                selectedModule={selectedModule}
                interfaces={interfaces}
                isAdmin={isAdmin}
                currentSubsystem={subsystems.find((s) => s.id === selectedSubsystemId) || null}
                currentGroup={selectedGroupId ? findGroupInTree(groupTree, selectedGroupId) : null}
                onViewProcess={(procId) => {
                  setTreeViewMode('PHYSICAL');
                  setSelectedProcessId(procId);
                }}
                onCreateModule={() => {
                  setEditingMod(null);
                  setModuleForm({ code: '', name: '', description: '', process_id: '' });
                  setQuickProcessOpen(false);
                  setModuleModalOpen(true);
                }}
                onEditModule={(mod) => {
                  setEditingMod(mod);
                  setModuleForm({
                    code: mod.code,
                    name: mod.name,
                    description: mod.description || '',
                    process_id: mod.process_id || (mod.process?.id ?? ''),
                  });
                  setQuickProcessOpen(false);
                  setModuleModalOpen(true);
                }}
                onDeleteModule={(id) => void handleDeleteModule(id)}
                onCreateInterface={() => {
                  setIfaceForm({
                    relation_type: 'PROVIDE',
                    service_name: '',
                    operation: '',
                    diff_ignore_paths: '',
                    description: '',
                  });
                  setIfaceModalOpen(true);
                }}
                onDeleteInterface={(id) => void handleDeleteInterface(id)}
                onUpdateIgnorePaths={(ifaceId, paths) => void handleUpdateIgnorePaths(ifaceId, paths)}
              />
            )}
          </div>
        )}

        {activeTab === 'physical' && (
          <StaticTopologyCanvas
            topology={topology}
            loading={loadingTopo}
            view={topologyView}
            onViewChange={(v) => {
              setTopologyView(v);
              void loadTopology(v);
            }}
          />
        )}

        {activeTab === 'verify' && (
          <div className="code-verify-container">
            <div className="code-verify-toolbar">
              <label className="code-field-label">关联录制批次:</label>
              <select
                className="code-select"
                value={selectedSessionId}
                onChange={(e) => {
                  setSelectedSessionId(e.target.value);
                  updateParams({ session_id: e.target.value });
                }}
              >
                {sessions?.map((sess) => (
                  <option key={sess.id} value={sess.id}>
                    {sess.name} ({sess.session_no} - {sess.status})
                  </option>
                ))}
              </select>
            </div>
            <ConsistencyVerifyOverlay
              report={consistencyReport}
              loading={loadingReport}
              onRefresh={() => void loadConsistency()}
            />
          </div>
        )}
      </div>

      {/* 子系统编辑弹窗 */}
      <Modal
        open={subsystemModalOpen}
        onClose={() => setSubsystemModalOpen(false)}
        title={editingSubsystem ? '编辑子系统' : '新建子系统'}
        footer={
          <div className="code-modal-footer">
            <button
              type="button"
              className="code-btn code-btn--secondary"
              onClick={() => setSubsystemModalOpen(false)}
            >
              取消
            </button>
            <button
              type="button"
              className="code-btn code-btn--primary"
              onClick={() => void handleSaveSubsystem()}
            >
              保存
            </button>
          </div>
        }
      >
        <div className="code-form">
          <div className="code-form-item">
            <label className="code-form-label">子系统编码 (2..64 大写字母/数字/中划线):</label>
            <input
              type="text"
              className="code-input"
              value={subsystemForm.code}
              disabled={Boolean(editingSubsystem)}
              onChange={(e) => setSubsystemForm({ ...subsystemForm, code: e.target.value.toUpperCase() })}
              placeholder="如 CORE_BANKING, ORDER_SYS"
            />
          </div>
          <div className="code-form-item">
            <label className="code-form-label">子系统名称:</label>
            <input
              type="text"
              className="code-input"
              value={subsystemForm.name}
              onChange={(e) => setSubsystemForm({ ...subsystemForm, name: e.target.value })}
              placeholder="如 核心账务子系统"
            />
          </div>
          <div className="code-form-item">
            <label className="code-form-label">描述说明:</label>
            <textarea
              className="code-textarea"
              value={subsystemForm.description}
              onChange={(e) => setSubsystemForm({ ...subsystemForm, description: e.target.value })}
              placeholder="说明子系统的业务范围与定位"
            />
          </div>
        </div>
      </Modal>

      {/* 功能组编辑弹窗 */}
      <Modal
        open={groupModalOpen}
        onClose={() => setGroupModalOpen(false)}
        title={editingGroup ? '编辑功能组' : '新建功能组'}
        footer={
          <div className="code-modal-footer">
            <button
              type="button"
              className="code-btn code-btn--secondary"
              onClick={() => setGroupModalOpen(false)}
            >
              取消
            </button>
            <button
              type="button"
              className="code-btn code-btn--primary"
              onClick={() => void handleSaveGroup()}
            >
              保存
            </button>
          </div>
        }
      >
        <div className="code-form">
          <div className="code-form-item">
            <label className="code-form-label">功能组编码:</label>
            <input
              type="text"
              className="code-input"
              value={groupForm.code}
              onChange={(e) => setGroupForm({ ...groupForm, code: e.target.value })}
              placeholder="如 G_PAYMENT_CHANNEL"
            />
          </div>
          <div className="code-form-item">
            <label className="code-form-label">功能组名称:</label>
            <input
              type="text"
              className="code-input"
              value={groupForm.name}
              onChange={(e) => setGroupForm({ ...groupForm, name: e.target.value })}
              placeholder="如 支付渠道组"
            />
          </div>
          <div className="code-form-item">
            <label className="code-form-label">描述说明:</label>
            <textarea
              className="code-textarea"
              value={groupForm.description}
              onChange={(e) => setGroupForm({ ...groupForm, description: e.target.value })}
            />
          </div>
        </div>
      </Modal>

      {/* 移动功能组弹窗 */}
      <Modal
        open={moveModalOpen}
        onClose={() => setMoveModalOpen(false)}
        title={`调整功能组 [${movingGroup?.name}] 层级 (防成环保护)`}
        footer={
          <div className="code-modal-footer">
            <button
              type="button"
              className="code-btn code-btn--secondary"
              onClick={() => setMoveModalOpen(false)}
            >
              取消
            </button>
            <button
              type="button"
              className="code-btn code-btn--primary"
              onClick={() => void handleMoveGroup()}
            >
              确认移动
            </button>
          </div>
        }
      >
        <div className="code-form">
          <div className="code-form-item">
            <label className="code-form-label">选择新的目标父节点 (留空表示移至根节点):</label>
            <select
              className="code-select"
              value={newParentId}
              onChange={(e) => setNewParentId(e.target.value)}
            >
              <option value="">移为一级根功能组 (None)</option>
              {(groupTree ?? []).map((g) => (
                <option key={g.id} value={g.id} disabled={g.id === movingGroup?.id}>
                  {g.name} ({g.code}) - Level {g.level}
                </option>
              ))}
            </select>
          </div>
        </div>
      </Modal>

      {/* 模块编辑弹窗 */}
      <Modal
        open={moduleModalOpen}
        onClose={() => setModuleModalOpen(false)}
        title={editingMod ? '编辑功能模块' : '新建功能模块'}
        footer={
          <div className="code-modal-footer">
            <button
              type="button"
              className="code-btn code-btn--secondary"
              onClick={() => setModuleModalOpen(false)}
            >
              取消
            </button>
            <button
              type="button"
              className="code-btn code-btn--primary"
              onClick={() => void handleSaveModule()}
            >
              保存
            </button>
          </div>
        }
      >
        <div className="code-form">
          <div className="code-form-item">
            <label className="code-form-label">模块全局唯一编码 (code):</label>
            <input
              type="text"
              className="code-input"
              value={moduleForm.code}
              disabled={Boolean(editingMod)}
              onChange={(e) => setModuleForm({ ...moduleForm, code: e.target.value })}
              placeholder="如 MOD_ACCOUNT_SVC"
            />
          </div>
          <div className="code-form-item">
            <label className="code-form-label">模块名称:</label>
            <input
              type="text"
              className="code-input"
              value={moduleForm.name}
              onChange={(e) => setModuleForm({ ...moduleForm, name: e.target.value })}
              placeholder="如 账户余额服务模块"
            />
          </div>
          <div className="code-form-item">
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <label className="code-form-label" style={{ marginBottom: 0 }}>宿主业务进程 (Process):</label>
              <button
                type="button"
                className="code-btn code-btn--secondary code-btn--xs"
                onClick={() => setQuickProcessOpen(!quickProcessOpen)}
                style={{ display: 'inline-flex', alignItems: 'center', gap: '0.25rem' }}
              >
                <Plus size={12} /> {quickProcessOpen ? '收起快捷新建' : '快速创建进程'}
              </button>
            </div>
            <select
              className="code-select"
              style={{ marginTop: '0.4rem' }}
              value={moduleForm.process_id}
              onChange={(e) => setModuleForm({ ...moduleForm, process_id: e.target.value })}
            >
              <option value="">-- 暂不绑定宿主进程 (稍后绑定) --</option>
              {Object.entries(processGroupsMap).map(([groupName, procs]) => (
                <optgroup key={groupName} label={`【${groupName}】`}>
                  {procs.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name} (Port: {p.listen_port})
                    </option>
                  ))}
                </optgroup>
              ))}
            </select>

            {quickProcessOpen && (
              <div className="code-quick-process-panel">
                <div className="code-quick-process-title">
                  <span>快速新建进程并装配绑定</span>
                  <span className="code-color-muted code-text-xs">支持按进程组归类</span>
                </div>
                <div className="code-quick-process-grid">
                  <input
                    type="text"
                    className="code-input code-input--sm"
                    placeholder="进程组 (如: 核心组)"
                    value={quickProcessForm.group_name}
                    onChange={(e) => setQuickProcessForm({ ...quickProcessForm, group_name: e.target.value })}
                  />
                  <input
                    type="text"
                    className="code-input code-input--sm"
                    placeholder="进程名 (如: backend-order)"
                    value={quickProcessForm.name}
                    onChange={(e) => setQuickProcessForm({ ...quickProcessForm, name: e.target.value })}
                  />
                  <input
                    type="number"
                    className="code-input code-input--sm"
                    placeholder="端口 (如: 9004)"
                    value={quickProcessForm.listen_port}
                    onChange={(e) => setQuickProcessForm({ ...quickProcessForm, listen_port: e.target.value })}
                  />
                  <button
                    type="button"
                    className="code-btn code-btn--primary code-btn--sm"
                    onClick={() => void handleQuickCreateProcess()}
                  >
                    创建并绑定
                  </button>
                </div>
              </div>
            )}
          </div>
          <div className="code-form-item">
            <label className="code-form-label">描述说明:</label>
            <textarea
              className="code-textarea"
              value={moduleForm.description}
              onChange={(e) => setModuleForm({ ...moduleForm, description: e.target.value })}
            />
          </div>
        </div>
      </Modal>

      {/* 物理进程创建/编辑独立弹窗 */}
      <Modal
        open={processModalOpen}
        onClose={() => setProcessModalOpen(false)}
        title={editingProcess ? `编辑业务进程 [${editingProcess.name}]` : '新建业务进程'}
        footer={
          <div className="code-modal-footer">
            <button
              type="button"
              className="code-btn code-btn--secondary"
              onClick={() => setProcessModalOpen(false)}
            >
              取消
            </button>
            <button
              type="button"
              className="code-btn code-btn--primary"
              onClick={() => void handleSaveProcess()}
            >
              保存
            </button>
          </div>
        }
      >
        <div className="code-form">
          <div className="code-form-item">
            <label className="code-form-label">所属进程组 (两级目录聚合):</label>
            <input
              type="text"
              className="code-input"
              value={processForm.group_name}
              onChange={(e) => setProcessForm({ ...processForm, group_name: e.target.value })}
              placeholder="如 核心交易组、基础支撑组、默认进程组"
            />
          </div>
          <div className="code-form-item">
            <label className="code-form-label">进程名称 (系统进程标识):</label>
            <input
              type="text"
              className="code-input"
              value={processForm.name}
              onChange={(e) => setProcessForm({ ...processForm, name: e.target.value })}
              placeholder="如 backend-inventory, sut-order"
            />
          </div>
          <div className="code-form-item">
            <label className="code-form-label">监听端口 (Listen Port 1~65535):</label>
            <input
              type="number"
              className="code-input"
              value={processForm.listen_port}
              onChange={(e) => setProcessForm({ ...processForm, listen_port: e.target.value })}
              placeholder="如 9003"
            />
          </div>
          {agents.length > 1 && (
            <div className="code-form-item">
              <label className="code-form-label">宿主 Agent:</label>
              <select
                className="code-select"
                value={processForm.agent_id}
                onChange={(e) => setProcessForm({ ...processForm, agent_id: e.target.value })}
              >
                {agents.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name} ({a.endpoint} - {a.scope})
                  </option>
                ))}
              </select>
            </div>
          )}
          <div className="code-form-item">
            <label className="code-form-label">命令行特征 (cmd_pattern):</label>
            <input
              type="text"
              className="code-input"
              value={processForm.cmd_pattern}
              onChange={(e) => setProcessForm({ ...processForm, cmd_pattern: e.target.value })}
              placeholder="如 ./bin/backend-inventory (留空将自动生成)"
            />
          </div>
          <div className="code-form-item">
            <label className="code-form-label">描述说明:</label>
            <textarea
              className="code-textarea"
              value={processForm.description}
              onChange={(e) => setProcessForm({ ...processForm, description: e.target.value })}
              placeholder="说明物理进程的部署与职责"
            />
          </div>
        </div>
      </Modal>

      {/* 接口契约添加弹窗 */}
      <Modal
        open={ifaceModalOpen}
        onClose={() => setIfaceModalOpen(false)}
        title={`为模块 [${selectedModule?.name}] 声明接口契约`}
        footer={
          <div className="code-modal-footer">
            <button
              type="button"
              className="code-btn code-btn--secondary"
              onClick={() => setIfaceModalOpen(false)}
            >
              取消
            </button>
            <button
              type="button"
              className="code-btn code-btn--primary"
              onClick={() => void handleSaveInterface()}
            >
              确认声明
            </button>
          </div>
        }
      >
        <div className="code-form">
          <div className="code-form-item">
            <label className="code-form-label">关系类型:</label>
            <select
              className="code-select"
              value={ifaceForm.relation_type}
              onChange={(e) =>
                setIfaceForm({
                  ...ifaceForm,
                  relation_type: e.target.value as 'PROVIDE' | 'DEPEND',
                })
              }
            >
              <option value="PROVIDE">PROVIDE (本模块对外暴露并实现的服务)</option>
              <option value="DEPEND">DEPEND (本模块运行依赖的下游服务)</option>
            </select>
          </div>
          <div className="code-form-item">
            <label className="code-form-label">服务名称 (格式: {'{package}-{serviceName}'}):</label>
            <input
              type="text"
              className="code-input"
              value={ifaceForm.service_name}
              onChange={(e) => setIfaceForm({ ...ifaceForm, service_name: e.target.value })}
              placeholder="如 dophi.account.v1-AccountService"
            />
          </div>
          <div className="code-form-item">
            <label className="code-form-label">调用方法名 (operation):</label>
            <input
              type="text"
              className="code-input"
              value={ifaceForm.operation}
              onChange={(e) => setIfaceForm({ ...ifaceForm, operation: e.target.value })}
              placeholder="如 GetAccountBalance"
            />
          </div>
          <div className="code-form-item">
            <label className="code-form-label">默认 Diff 忽略路径 (点分路径逗号分隔):</label>
            <input
              type="text"
              className="code-input"
              value={ifaceForm.diff_ignore_paths}
              onChange={(e) => setIfaceForm({ ...ifaceForm, diff_ignore_paths: e.target.value })}
              placeholder="如 header.timestamp, meta.token"
            />
          </div>
          <div className="code-form-item">
            <label className="code-form-label">说明:</label>
            <input
              type="text"
              className="code-input"
              value={ifaceForm.description}
              onChange={(e) => setIfaceForm({ ...ifaceForm, description: e.target.value })}
            />
          </div>
        </div>
      </Modal>
    </div>
  );
};

export default ArchitectureWorkbenchPage;
