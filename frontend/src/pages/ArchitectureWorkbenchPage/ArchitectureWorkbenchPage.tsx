import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Plus, Server } from 'lucide-react';
import { Drawer } from '@code/common';
import { useToast } from '../../utils/toast';
import { useModuleUser } from '../../context/ModuleUserContext';
import * as archApi from '../../api/arch';
import * as agentApi from '../../api/agent';
import type {
  Subsystem,
  FunctionalGroupTreeNode,
  Module,
  ModuleService,
  ModuleDependency,
  Process,
  Agent,
} from '../../types';
import { SubsystemGroupTreePanel, TreeViewMode } from './SubsystemGroupTreePanel';
import { ModuleContractDetailPane } from './ModuleContractDetailPane';
import { ProcessDetailPane } from './ProcessDetailPane';
import { ArchTopologyTabPane } from './ArchTopologyTabPane';

function findGroupInTree(
  nodes: FunctionalGroupTreeNode[],
  targetId: string | number
): FunctionalGroupTreeNode | null {
  const targetStr = String(targetId);
  for (const node of nodes) {
    if (String(node.id) === targetStr) return node;
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

  // URL 状态读取与平滑兼容
  const rawUrlTab = searchParams.get('tab');
  const urlTab: 'modeling' | 'topology' =
    rawUrlTab === 'physical' || rawUrlTab === 'verify' || rawUrlTab === 'topology'
      ? 'topology'
      : 'modeling';
  const urlViewMode = (searchParams.get('view_mode') as TreeViewMode) || 'LOGICAL';
  const urlSubsystemId = searchParams.get('subsystem_id') || '';
  const urlGroupId = searchParams.get('group_id') || '';
  const urlModuleId = searchParams.get('module_id') || '';
  const urlProcessId = searchParams.get('process_id') || '';

  // 顶层 Tab 与左侧视角 Tab
  const [activeTab, setActiveTab] = useState<'modeling' | 'topology'>(urlTab);
  const [treeViewMode, setTreeViewMode] = useState<TreeViewMode>(urlViewMode);

  // 基础数据状态
  const [subsystems, setSubsystems] = useState<Subsystem[]>([]);
  const [selectedSubsystemId, setSelectedSubsystemId] = useState<string>(urlSubsystemId);
  const [groupTree, setGroupTree] = useState<FunctionalGroupTreeNode[]>([]);
  const [groupTreeMap, setGroupTreeMap] = useState<Record<string, FunctionalGroupTreeNode[]>>({});
  const [selectedGroupId, setSelectedGroupId] = useState<string>(urlGroupId);
  const [modules, setModules] = useState<Module[]>([]);
  const [subsystemModules, setSubsystemModules] = useState<Module[]>([]);
  const [selectedModuleId, setSelectedModuleId] = useState<string>(urlModuleId);
  const [selectedModule, setSelectedModule] = useState<Module | null>(null);
  const [services, setServices] = useState<ModuleService[]>([]);
  const [dependencies, setDependencies] = useState<ModuleDependency[]>([]);

  // 物理进程数据状态与分组
  const [processes, setProcesses] = useState<Process[]>([]);
  const [selectedProcessId, setSelectedProcessId] = useState<string>(urlProcessId);
  const [selectedProcessGroupName, setSelectedProcessGroupName] = useState<string>('');
  const [agents, setAgents] = useState<Agent[]>([]);
  const [customProcessGroups, setCustomProcessGroups] = useState<string[]>(() => {
    try {
      const saved = localStorage.getItem('traefik_custom_proc_groups');
      if (saved) return JSON.parse(saved);
    } catch {
      // ignore
    }
    return ['核心业务组', '支撑服务组'];
  });

  const saveCustomProcessGroups = (groups: string[]) => {
    setCustomProcessGroups(groups);
    try {
      localStorage.setItem('traefik_custom_proc_groups', JSON.stringify(groups));
    } catch {
      // ignore
    }
  };

  // 抽屉（Drawer）与表单状态
  const [subsystemDrawerOpen, setSubsystemDrawerOpen] = useState(false);
  const [editingSubsystem, setEditingSubsystem] = useState<Subsystem | null>(null);
  const [subsystemForm, setSubsystemForm] = useState({ code: '', name: '', description: '' });

  const [groupDrawerOpen, setGroupDrawerOpen] = useState(false);
  const [groupParentId, setGroupParentId] = useState<string | null>(null);
  const [editingGroup, setEditingGroup] = useState<FunctionalGroupTreeNode | null>(null);
  const [groupForm, setGroupForm] = useState({ code: '', name: '', description: '', sort_order: 1 });

  const [moveGroupDrawerOpen, setMoveGroupDrawerOpen] = useState(false);
  const [movingGroup, setMovingGroup] = useState<FunctionalGroupTreeNode | null>(null);
  const [newParentId, setNewParentId] = useState<string>('');

  const [processGroupDrawerOpen, setProcessGroupDrawerOpen] = useState(false);
  const [editingProcessGroupName, setEditingProcessGroupName] = useState<string | null>(null);
  const [processGroupForm, setProcessGroupForm] = useState({ name: '' });

  const [processDrawerOpen, setProcessDrawerOpen] = useState(false);
  const [editingProcess, setEditingProcess] = useState<Process | null>(null);
  const [processForm, setProcessForm] = useState({
    group_name: '',
    name: '',
    listen_port: '',
    agent_id: '',
    cmd_pattern: '',
    description: '',
  });

  const [moduleDrawerOpen, setModuleDrawerOpen] = useState(false);
  const [editingMod, setEditingMod] = useState<Module | null>(null);
  const [moduleForm, setModuleForm] = useState({
    code: '',
    name: '',
    description: '',
    process_id: '',
    group_id: '',
  });
  const [quickProcessOpen, setQuickProcessOpen] = useState(false);
  const [quickProcessForm, setQuickProcessForm] = useState({ group_name: '', name: '', listen_port: '' });

  // 更新 URL Search Params 辅助函数（严格比对键值防引用抖动与死循环）
  const updateParams = useCallback(
    (updates: Record<string, string | null | undefined>) => {
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev);
        let changed = false;
        Object.entries(updates).forEach(([k, v]) => {
          const oldV = next.get(k);
          if (v !== undefined && v !== null && v !== '') {
            if (oldV !== String(v)) {
              next.set(k, String(v));
              changed = true;
            }
          } else if (oldV !== null) {
            next.delete(k);
            changed = true;
          }
        });
        return changed ? next : prev;
      });
    },
    [setSearchParams]
  );

  // 监听浏览器 URL 变化（支持前进/后退/外链深度直达与旧参数平滑兼容）
  useEffect(() => {
    const rawTab = searchParams.get('tab');
    if (rawTab === 'physical') {
      updateParams({ tab: 'topology', view: 'PHYSICAL' });
      setActiveTab('topology');
    } else if (rawTab === 'verify') {
      updateParams({ tab: 'topology' });
      setActiveTab('topology');
    } else if (rawTab === 'logical') {
      updateParams({ tab: 'modeling' });
      setActiveTab('modeling');
    } else if (rawTab === 'topology') {
      setActiveTab('topology');
    } else {
      setActiveTab('modeling');
    }

    const vm = (searchParams.get('view_mode') as TreeViewMode) || 'LOGICAL';
    const subId = searchParams.get('subsystem_id') || '';
    const grpId = searchParams.get('group_id') || '';
    const modId = searchParams.get('module_id') || '';
    const procId = searchParams.get('process_id') || '';

    setTreeViewMode(vm);
    if (subId) setSelectedSubsystemId((prev) => (String(prev) === String(subId) ? prev : String(subId)));
    if (grpId) setSelectedGroupId((prev) => (String(prev) === String(grpId) ? prev : String(grpId)));
    if (modId) setSelectedModuleId((prev) => (String(prev) === String(modId) ? prev : String(modId)));
    if (procId) setSelectedProcessId((prev) => (String(prev) === String(procId) ? prev : String(procId)));
  }, [searchParams, updateParams]);

  // 加载子系统列表
  const loadSubsystems = useCallback(async () => {
    try {
      const res = await archApi.listSubsystems({ page_size: 100 });
      const items = res.items ?? [];
      setSubsystems(items);
      if (items.length > 0) {
        setSelectedSubsystemId((prev) => {
          if (!prev) return items[0].id;
          return prev;
        });
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '加载子系统失败');
    }
  }, [toast]);

  useEffect(() => {
    void loadSubsystems();
  }, [loadSubsystems]);

  // 加载全系统功能模块列表（供架构元素树直接展开与节点定位）
  const loadSubsystemModules = useCallback(async () => {
    try {
      const res = await archApi.listModules({ page_size: 1000 });
      const items = res.items ?? [];
      setSubsystemModules(items);
      return items;
    } catch {
      return [];
    }
  }, []);

  useEffect(() => {
    if (activeTab !== 'modeling') return;
    void loadSubsystemModules();
  }, [activeTab, loadSubsystemModules]);

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
    if (activeTab !== 'modeling') return;
    void loadProcesses();
    void loadAgents();
  }, [activeTab, loadProcesses, loadAgents]);

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

  // 当子系统切换时，加载功能组树（仅在建模 Tab 下按需加载）
  useEffect(() => {
    if (activeTab !== 'modeling' || !selectedSubsystemId) return;
    let cancelled = false;
    void (async () => {
      try {
        const res = await archApi.getFunctionalGroupTree(selectedSubsystemId);
        if (cancelled) return;
        const safeTree = res.tree ?? [];
        setGroupTree(safeTree);
        setGroupTreeMap((prev) => ({ ...prev, [selectedSubsystemId]: safeTree }));
        if (safeTree.length > 0) {
          setSelectedGroupId((prev) => {
            if (prev && findGroupInTree(safeTree, prev)) return String(prev);
            return String(safeTree[0].id);
          });
        } else {
          setSelectedGroupId('');
          setModules([]);
          setSelectedModule(null);
        }
      } catch (err) {
        if (!cancelled) {
          toast.error(err instanceof Error ? err.message : '加载功能组失败');
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [activeTab, selectedSubsystemId, toast]);

  // 当功能组切换时，加载该组下的模块列表（仅在建模 Tab 下按需加载）
  useEffect(() => {
    if (activeTab !== 'modeling' || !selectedGroupId) return;
    let cancelled = false;
    void (async () => {
      try {
        const res = await archApi.listModules({ group_id: selectedGroupId, page_size: 100 });
        if (cancelled) return;
        const items = res.items ?? [];
        setModules(items);
        if (items.length > 0) {
          setSelectedModuleId((prev) => {
            if (prev && items.some((m) => String(m.id) === String(prev))) {
              return String(prev);
            }
            return String(items[0].id);
          });
        } else {
          setSelectedModuleId('');
          setSelectedModule(null);
          setServices([]);
          setDependencies([]);
        }
      } catch (err) {
        if (!cancelled) {
          toast.error(err instanceof Error ? err.message : '加载模块失败');
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [activeTab, selectedGroupId, toast]);

  // 加载与刷新模块详情、对外供给服务与下游服务依赖
  const refreshCurrentModule = useCallback(
    async (modId: string) => {
      if (!modId) return null;
      try {
        const res = await archApi.getModule(modId);
        setSelectedModule(res.module);
        setServices(res.services ?? []);
        setDependencies(res.dependencies ?? []);
        return res;
      } catch (err) {
        toast.error(err instanceof Error ? err.message : '加载模块契约失败');
        return null;
      }
    },
    [toast]
  );

  // 原子化选择功能模块叶子节点（避免中间状态置空引发视图抖动与闪烁）
  const handleSelectModuleLeaf = useCallback(
    (subsystemId: string | number, groupId: string | number, moduleId: string | number) => {
      const subId = String(subsystemId);
      const grpId = String(groupId);
      const modId = String(moduleId);
      setSelectedSubsystemId(subId);
      setSelectedGroupId(grpId);
      setSelectedModuleId(modId);
      updateParams({
        subsystem_id: subId,
        group_id: grpId,
        module_id: modId,
        view_mode: 'LOGICAL',
      });
    },
    [updateParams]
  );

  // 当模块切换时，加载模块详情与服务契约（仅在建模 Tab 下按需加载）
  useEffect(() => {
    if (activeTab !== 'modeling' || !selectedModuleId) return;
    let cancelled = false;
    void (async () => {
      try {
        const res = await archApi.getModule(selectedModuleId);
        if (cancelled) return;
        setSelectedModule(res.module);
        setServices(res.services ?? []);
        setDependencies(res.dependencies ?? []);
      } catch (err) {
        if (!cancelled) {
          toast.error(err instanceof Error ? err.message : '加载模块契约失败');
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [activeTab, selectedModuleId, toast]);

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
      setSubsystemDrawerOpen(false);
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
      updateParams({ subsystem_id: null, group_id: null, module_id: null });
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
        await archApi.updateFunctionalGroup(editingGroup.id, {
          code: groupForm.code,
          name: groupForm.name,
          description: groupForm.description,
          sort_order: groupForm.sort_order,
        });
        toast.success('功能组更新成功');
      } else {
        await archApi.createFunctionalGroup({
          subsystem_id: selectedSubsystemId,
          parent_id: groupParentId,
          code: groupForm.code,
          name: groupForm.name,
          description: groupForm.description,
          sort_order: groupForm.sort_order,
        });
        toast.success('功能组创建成功');
      }
      setGroupDrawerOpen(false);
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
      setMoveGroupDrawerOpen(false);
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
      if (selectedGroupId === id) {
        setSelectedGroupId('');
        updateParams({ group_id: null, module_id: null });
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '删除功能组失败');
    }
  };

  // 模块 CRUD
  const handleSaveModule = async () => {
    const targetGroupId = moduleForm.group_id || selectedGroupId;
    if (!targetGroupId) {
      toast.error('请选择所属功能组');
      return;
    }
    try {
      const procId = moduleForm.process_id ? moduleForm.process_id : null;
      if (editingMod) {
        await archApi.updateModule(editingMod.id, {
          group_id: targetGroupId,
          code: moduleForm.code,
          name: moduleForm.name,
          description: moduleForm.description,
          process_id: procId,
        });
        toast.success('模块修改成功');
      } else {
        const created = await archApi.createModule({
          group_id: targetGroupId,
          code: moduleForm.code,
          name: moduleForm.name,
          description: moduleForm.description,
          process_id: procId,
        });
        toast.success('模块创建成功');
        setSelectedModuleId(created.id);
        updateParams({ module_id: created.id, group_id: targetGroupId, view_mode: 'LOGICAL' });
      }
      setModuleDrawerOpen(false);
      // 重新加载组内模块与子系统全量模块
      const res = await archApi.listModules({ group_id: targetGroupId, page_size: 100 });
      setModules(res.items);
      void loadSubsystemModules();
      if (selectedModuleId) {
        const modRes = await archApi.getModule(selectedModuleId);
        setSelectedModule(modRes.module);
      }
      void loadProcesses();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '操作模块失败');
    }
  };

  const handleDeleteModule = async (id: string) => {
    try {
      await archApi.deleteModule(id);
      toast.success('模块已删除');
      if (selectedGroupId) {
        const res = await archApi.listModules({ group_id: selectedGroupId, page_size: 100 });
        setModules(res.items);
      }
      void loadSubsystemModules();
      if (selectedModuleId === id) {
        setSelectedModuleId('');
        setSelectedModule(null);
        updateParams({ module_id: null });
      }
      void loadProcesses();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '删除模块失败');
    }
  };

  // 在创建/编辑 FM 模块时快速新建物理进程并直接绑定
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
      const groupName = quickProcessForm.group_name.trim() || '默认进程组';
      const newProc = await agentApi.createProcess({
        agent_id: defaultAgentId,
        group_name: groupName,
        name: quickProcessForm.name.trim(),
        listen_port: port,
        cmd_pattern: `./bin/${quickProcessForm.name.trim()}`,
      });
      toast.success(`进程 [${newProc.name}] 创建成功并已自动关联`);
      const res = await agentApi.listProcesses({ page_size: 1000 });
      const items = res.items ?? [];
      setProcesses(items);
      // 若是全新进程组，加入自定义组列表
      if (!customProcessGroups.includes(groupName) && groupName !== '默认进程组') {
        saveCustomProcessGroups([...customProcessGroups, groupName]);
      }
      setModuleForm((prev) => ({ ...prev, process_id: newProc.id }));
      setQuickProcessOpen(false);
      setQuickProcessForm({ group_name: '', name: '', listen_port: '' });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '快速创建进程失败');
    }
  };

  // 物理进程组 CRUD
  const handleSaveProcessGroup = async () => {
    const newName = processGroupForm.name.trim();
    if (!newName) {
      toast.error('请输入进程组名称');
      return;
    }
    if (editingProcessGroupName) {
      try {
        const procsInGroup = processes.filter((p) => (p.group_name || '默认进程组') === editingProcessGroupName);
        for (const p of procsInGroup) {
          await agentApi.updateProcess(p.id, {
            name: p.name,
            group_name: newName,
            listen_port: p.listen_port,
            advertise_ip: p.advertise_ip,
            cmd_pattern: p.cmd_pattern,
            description: p.description,
          });
        }
        const updated = customProcessGroups.map((g) => (g === editingProcessGroupName ? newName : g));
        saveCustomProcessGroups(updated);
        toast.success(`进程组已重命名为 [${newName}]`);
        setProcessGroupDrawerOpen(false);
        void loadProcesses();
      } catch (err) {
        toast.error(err instanceof Error ? err.message : '重命名进程组失败');
      }
    } else {
      if (customProcessGroups.includes(newName) || newName === '默认进程组') {
        toast.error('该进程组已存在');
        return;
      }
      saveCustomProcessGroups([...customProcessGroups, newName]);
      toast.success(`进程组 [${newName}] 创建成功`);
      setProcessGroupDrawerOpen(false);
    }
  };

  const handleDeleteProcessGroup = (groupName: string) => {
    const procsInGroup = processes.filter((p) => (p.group_name || '默认进程组') === groupName);
    if (procsInGroup.length > 0) {
      toast.error(`进程组 [${groupName}] 下仍有 ${procsInGroup.length} 个业务进程，请先迁移或清空组内进程`);
      return;
    }
    const updated = customProcessGroups.filter((g) => g !== groupName);
    saveCustomProcessGroups(updated);
    toast.success(`进程组 [${groupName}] 已移除`);
  };

  // 物理进程 CRUD
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
    const groupName = processForm.group_name.trim() || '默认进程组';
    try {
      if (editingProcess) {
        await agentApi.updateProcess(editingProcess.id, {
          name: processForm.name.trim(),
          group_name: groupName,
          listen_port: port,
          cmd_pattern: processForm.cmd_pattern.trim() || `./bin/${processForm.name.trim()}`,
          description: processForm.description.trim(),
        });
        toast.success('进程更新成功');
      } else {
        const defaultAgentId = processForm.agent_id || (agents.length > 0 ? agents[0].id : '');
        const newProc = await agentApi.createProcess({
          agent_id: defaultAgentId,
          group_name: groupName,
          name: processForm.name.trim(),
          listen_port: port,
          cmd_pattern: processForm.cmd_pattern.trim() || `./bin/${processForm.name.trim()}`,
          description: processForm.description.trim(),
        });
        toast.success('进程创建成功');
        setSelectedProcessId(newProc.id);
        updateParams({ process_id: newProc.id, view_mode: 'PHYSICAL' });
      }
      if (!customProcessGroups.includes(groupName) && groupName !== '默认进程组') {
        saveCustomProcessGroups([...customProcessGroups, groupName]);
      }
      setProcessDrawerOpen(false);
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
        updateParams({ process_id: null });
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

  // Service 供给与依赖处理函数
  const handleCreateService = async (data: {
    service_name: string;
    diff_ignore_paths?: string[];
    description?: string;
  }) => {
    if (!selectedModuleId) return;
    try {
      await archApi.createModuleService(selectedModuleId, data);
      toast.success(`对外服务 [${data.service_name}] 声明成功`);
      await refreshCurrentModule(selectedModuleId);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '声明服务失败');
      throw err;
    }
  };

  const handleUpdateServiceIgnorePaths = async (serviceId: string, paths: string[]) => {
    if (!selectedModuleId) return;
    try {
      await archApi.updateModuleService(serviceId, { diff_ignore_paths: paths });
      toast.success('忽略路径已更新');
      await refreshCurrentModule(selectedModuleId);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '更新忽略路径失败');
      throw err;
    }
  };

  const handleDeleteService = async (serviceId: string) => {
    if (!selectedModuleId) return;
    try {
      await archApi.deleteModuleService(serviceId);
      toast.success('对外服务已删除');
      await refreshCurrentModule(selectedModuleId);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '删除服务失败');
      throw err;
    }
  };

  const handleCreateDependency = async (data: {
    is_external: boolean;
    external_system?: string;
    external_service?: string;
    service_id?: string;
    description?: string;
  }) => {
    if (!selectedModuleId) return;
    try {
      await archApi.createModuleDependency(selectedModuleId, data);
      toast.success('下游服务依赖添加成功');
      await refreshCurrentModule(selectedModuleId);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '添加依赖失败');
      throw err;
    }
  };

  const handleDeleteDependency = async (depId: string) => {
    if (!selectedModuleId) return;
    try {
      await archApi.deleteModuleDependency(depId);
      toast.success('下游依赖已解除');
      await refreshCurrentModule(selectedModuleId);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '解除依赖失败');
      throw err;
    }
  };

  const handleBindModuleProcess = async (moduleId: string, processId: string | null) => {
    try {
      await archApi.bindModuleProcess(moduleId, processId);
      toast.success(processId ? '物理进程装载成功' : '已解除物理进程装配');
      await refreshCurrentModule(moduleId);
      void loadProcesses();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '装配物理进程失败');
      throw err;
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
            className={`code-pill-btn ${activeTab === 'modeling' ? 'code-pill-btn--active' : ''}`}
            onClick={() => {
              setActiveTab('modeling');
              updateParams({ tab: 'modeling' });
            }}
          >
            架构建模与模块契约
          </button>
          <button
            type="button"
            className={`code-pill-btn ${activeTab === 'topology' ? 'code-pill-btn--active' : ''}`}
            onClick={() => {
              setActiveTab('topology');
              updateParams({ tab: 'topology' });
            }}
          >
            静态拓扑与一致性画布
          </button>
        </div>
      </div>

      {/* 视图主体 */}
      <div className="code-workbench-body">
        {activeTab === 'modeling' && (
          <div className="code-arch-split-layout">
            <SubsystemGroupTreePanel
              viewMode={treeViewMode}
              onViewModeChange={(mode) => {
                setTreeViewMode(mode);
                updateParams({ view_mode: mode });
              }}
              subsystems={subsystems}
              selectedSubsystemId={selectedSubsystemId}
              onSelectSubsystem={(id) => {
                const subId = String(id);
                setSelectedSubsystemId(subId);
                setSelectedGroupId('');
                setSelectedModuleId('');
                updateParams({ subsystem_id: subId, group_id: null, module_id: null });
              }}
              groupTree={groupTree}
              groupTreeMap={groupTreeMap}
              onLoadSubsystemTree={loadSubsystemTree}
              selectedGroupId={selectedGroupId}
              onSelectGroup={(groupId, targetSubsystemId) => {
                const subId = targetSubsystemId ? String(targetSubsystemId) : selectedSubsystemId;
                const grpId = String(groupId);
                setSelectedSubsystemId(subId);
                setSelectedGroupId(grpId);
                setSelectedModuleId('');
                updateParams({ subsystem_id: subId, group_id: grpId, module_id: null });
              }}
              modules={subsystemModules}
              selectedModuleId={selectedModuleId}
              onSelectModule={(modId) => {
                const targetMod = subsystemModules.find((m) => String(m.id) === String(modId));
                if (targetMod && targetMod.subsystem_id && targetMod.group_id) {
                  handleSelectModuleLeaf(targetMod.subsystem_id, targetMod.group_id, modId);
                } else {
                  setSelectedModuleId(modId);
                  updateParams({ module_id: modId, view_mode: 'LOGICAL' });
                }
              }}
              onSelectModuleLeaf={handleSelectModuleLeaf}
              onCreateModule={(groupId) => {
                setEditingMod(null);
                setModuleForm({ code: '', name: '', description: '', process_id: '', group_id: groupId });
                setQuickProcessOpen(false);
                setModuleDrawerOpen(true);
              }}
              onEditModule={(mod) => {
                setEditingMod(mod);
                setModuleForm({
                  code: mod.code,
                  name: mod.name,
                  description: mod.description || '',
                  process_id: mod.process_id || (mod.process?.id ?? ''),
                  group_id: mod.group_id,
                });
                setQuickProcessOpen(false);
                setModuleDrawerOpen(true);
              }}
              onDeleteModule={(id) => void handleDeleteModule(id)}
              processes={processes}
              selectedProcessId={selectedProcessId}
              onSelectProcess={(id) => {
                setSelectedProcessId(id);
                setSelectedProcessGroupName('');
                updateParams({ process_id: id, view_mode: 'PHYSICAL' });
              }}
              customProcessGroups={customProcessGroups}
              selectedProcessGroupName={selectedProcessGroupName}
              onSelectProcessGroup={(groupName) => {
                setSelectedProcessGroupName(groupName);
                setSelectedProcessId('');
              }}
              onCreateProcessGroup={() => {
                setEditingProcessGroupName(null);
                setProcessGroupForm({ name: '' });
                setProcessGroupDrawerOpen(true);
              }}
              onEditProcessGroup={(groupName) => {
                setEditingProcessGroupName(groupName);
                setProcessGroupForm({ name: groupName });
                setProcessGroupDrawerOpen(true);
              }}
              onDeleteProcessGroup={handleDeleteProcessGroup}
              onCreateProcess={(targetGroupName) => {
                setEditingProcess(null);
                setProcessForm({
                  group_name: targetGroupName || '默认进程组',
                  name: '',
                  listen_port: '',
                  agent_id: agents.length > 0 ? agents[0].id : '',
                  cmd_pattern: '',
                  description: '',
                });
                setProcessDrawerOpen(true);
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
                setProcessDrawerOpen(true);
              }}
              onDeleteProcess={(id) => void handleDeleteProcess(id)}
              isAdmin={isAdmin}
              onCreateSubsystem={() => {
                setEditingSubsystem(null);
                setSubsystemForm({ code: '', name: '', description: '' });
                setSubsystemDrawerOpen(true);
              }}
              onEditSubsystem={(sub) => {
                setEditingSubsystem(sub);
                setSubsystemForm({ code: sub.code, name: sub.name, description: sub.description || '' });
                setSubsystemDrawerOpen(true);
              }}
              onDeleteSubsystem={(id) => void handleDeleteSubsystem(id)}
              onCreateGroup={(parentId, targetSubsystemId) => {
                if (targetSubsystemId && targetSubsystemId !== selectedSubsystemId) {
                  setSelectedSubsystemId(targetSubsystemId);
                  updateParams({ subsystem_id: targetSubsystemId });
                }
                setEditingGroup(null);
                setGroupParentId(parentId ?? null);
                setGroupForm({ code: '', name: '', description: '', sort_order: 1 });
                setGroupDrawerOpen(true);
              }}
              onEditGroup={(grp) => {
                setEditingGroup(grp);
                setGroupForm({
                  code: grp.code,
                  name: grp.name,
                  description: grp.description || '',
                  sort_order: grp.sort_order || 1,
                });
                setGroupDrawerOpen(true);
              }}
              onMoveGroup={(grp) => {
                setMovingGroup(grp);
                setNewParentId('');
                setMoveGroupDrawerOpen(true);
              }}
              onDeleteGroup={(id) => void handleDeleteGroup(id)}
            />

            {treeViewMode === 'PHYSICAL' ? (
              selectedProcess ? (
                <ProcessDetailPane
                  process={selectedProcess}
                  allModules={subsystemModules.length > 0 ? subsystemModules : modules}
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
                    setProcessDrawerOpen(true);
                  }}
                  onDeleteProcess={(id) => void handleDeleteProcess(id)}
                  onSelectModule={(modId) => {
                    const targetMod = subsystemModules.find((m) => String(m.id) === String(modId));
                    if (targetMod && targetMod.subsystem_id && targetMod.group_id) {
                      handleSelectModuleLeaf(targetMod.subsystem_id, targetMod.group_id, modId);
                    } else {
                      setSelectedModuleId(modId);
                      setTreeViewMode('LOGICAL');
                      updateParams({ module_id: modId, view_mode: 'LOGICAL' });
                    }
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
                onSelectModule={(id) => {
                  const targetMod = subsystemModules.find((m) => String(m.id) === String(id));
                  if (targetMod && targetMod.subsystem_id && targetMod.group_id) {
                    handleSelectModuleLeaf(targetMod.subsystem_id, targetMod.group_id, id);
                  } else {
                    setSelectedModuleId(id);
                    updateParams({ module_id: id });
                  }
                }}
                selectedModule={selectedModule}
                services={services}
                dependencies={dependencies}
                allProcesses={processes}
                isAdmin={isAdmin}
                currentSubsystem={subsystems.find((s) => s.id === selectedSubsystemId) || null}
                currentGroup={selectedGroupId ? findGroupInTree(groupTree, selectedGroupId) : null}
                onViewProcess={(procId) => {
                  setTreeViewMode('PHYSICAL');
                  setSelectedProcessId(procId);
                  updateParams({ process_id: procId, view_mode: 'PHYSICAL' });
                }}
                onCreateModule={() => {
                  setEditingMod(null);
                  setModuleForm({
                    code: '',
                    name: '',
                    description: '',
                    process_id: '',
                    group_id: selectedGroupId,
                  });
                  setQuickProcessOpen(false);
                  setModuleDrawerOpen(true);
                }}
                onEditModule={(mod) => {
                  setEditingMod(mod);
                  setModuleForm({
                    code: mod.code,
                    name: mod.name,
                    description: mod.description || '',
                    process_id: mod.process_id || (mod.process?.id ?? ''),
                    group_id: mod.group_id,
                  });
                  setQuickProcessOpen(false);
                  setModuleDrawerOpen(true);
                }}
                onDeleteModule={(id) => void handleDeleteModule(id)}
                onCreateService={handleCreateService}
                onUpdateServiceIgnorePaths={handleUpdateServiceIgnorePaths}
                onDeleteService={handleDeleteService}
                onCreateDependency={handleCreateDependency}
                onDeleteDependency={handleDeleteDependency}
                onBindProcess={handleBindModuleProcess}
              />
            )}
          </div>
        )}

        {activeTab === 'topology' && <ArchTopologyTabPane />}
      </div>

      {/* 1. 子系统侧边栏 (Drawer) */}
      <Drawer
        open={subsystemDrawerOpen}
        onClose={() => setSubsystemDrawerOpen(false)}
        title={editingSubsystem ? '编辑子系统' : '新建子系统'}
        subtitle="定义顶层业务系统架构边界与唯一编码"
        width="md"
        footer={
          <div className="code-drawer-footer">
            <button
              type="button"
              className="code-btn code-btn--secondary"
              onClick={() => setSubsystemDrawerOpen(false)}
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
      </Drawer>

      {/* 2. 功能组侧边栏 (Drawer) */}
      <Drawer
        open={groupDrawerOpen}
        onClose={() => setGroupDrawerOpen(false)}
        title={editingGroup ? '编辑功能组' : '新建功能组'}
        subtitle="规划 1~8 级业务功能边界划分"
        width="md"
        footer={
          <div className="code-drawer-footer">
            <button
              type="button"
              className="code-btn code-btn--secondary"
              onClick={() => setGroupDrawerOpen(false)}
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
            <label className="code-form-label">排序权重 (数字越小越靠前):</label>
            <input
              type="number"
              className="code-input"
              value={groupForm.sort_order}
              onChange={(e) => setGroupForm({ ...groupForm, sort_order: parseInt(e.target.value, 10) || 1 })}
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
      </Drawer>

      {/* 3. 移动功能组侧边栏 (Drawer) */}
      <Drawer
        open={moveGroupDrawerOpen}
        onClose={() => setMoveGroupDrawerOpen(false)}
        title={`调整功能组 [${movingGroup?.name}] 层级`}
        subtitle="防成环保护，变更上级父节点"
        width="md"
        footer={
          <div className="code-drawer-footer">
            <button
              type="button"
              className="code-btn code-btn--secondary"
              onClick={() => setMoveGroupDrawerOpen(false)}
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
            <label className="code-form-label">选择新的目标父节点 (留空表示移为一级根节点):</label>
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
      </Drawer>

      {/* 4. 进程组管理侧边栏 (Drawer) */}
      <Drawer
        open={processGroupDrawerOpen}
        onClose={() => setProcessGroupDrawerOpen(false)}
        title={editingProcessGroupName ? `重命名进程组 [${editingProcessGroupName}]` : '新建进程分组'}
        subtitle="物理架构二级聚合，按业务单元归集业务进程"
        width="md"
        footer={
          <div className="code-drawer-footer">
            <button
              type="button"
              className="code-btn code-btn--secondary"
              onClick={() => setProcessGroupDrawerOpen(false)}
            >
              取消
            </button>
            <button
              type="button"
              className="code-btn code-btn--primary"
              onClick={() => void handleSaveProcessGroup()}
            >
              保存
            </button>
          </div>
        }
      >
        <div className="code-form">
          <div className="code-form-item">
            <label className="code-form-label">进程组名称 (建议简洁明了):</label>
            <input
              type="text"
              className="code-input"
              value={processGroupForm.name}
              onChange={(e) => setProcessGroupForm({ name: e.target.value })}
              placeholder="如 核心交易组、网关路由组、基础存储组"
            />
          </div>
        </div>
      </Drawer>

      {/* 5. 物理进程侧边栏 (Drawer) */}
      <Drawer
        open={processDrawerOpen}
        onClose={() => setProcessDrawerOpen(false)}
        title={editingProcess ? `编辑业务进程 [${editingProcess.name}]` : '新建业务进程'}
        subtitle="配置运行在宿主机操作系统上的物理进程容器"
        width="md"
        footer={
          <div className="code-drawer-footer">
            <button
              type="button"
              className="code-btn code-btn--secondary"
              onClick={() => setProcessDrawerOpen(false)}
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
      </Drawer>

      {/* 6. 功能模块侧边栏 (Drawer) */}
      <Drawer
        open={moduleDrawerOpen}
        onClose={() => setModuleDrawerOpen(false)}
        title={editingMod ? '编辑功能模块' : '新建功能模块'}
        subtitle="维护功能模块契约与宿主业务进程装配绑定"
        width="lg"
        footer={
          <div className="code-drawer-footer">
            <button
              type="button"
              className="code-btn code-btn--secondary"
              onClick={() => setModuleDrawerOpen(false)}
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
      </Drawer>

    </div>
  );
};

export default ArchitectureWorkbenchPage;
