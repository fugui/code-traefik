import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Layers, Server } from 'lucide-react';
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
  ArchProcessGroup,
  ArchProcess,
  ArchProcessDetailDTO,
  AutoPinnedOverride,
} from '../../types';
import { SubsystemGroupTreePanel } from './SubsystemGroupTreePanel';
import { ModuleContractDetailPane } from './ModuleContractDetailPane';
import { ArchProcessTreePanel } from './ArchProcessTreePanel';
import { ArchProcessDetailPane } from './ArchProcessDetailPane';

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

const DEFAULT_TREE_WIDTH = 380;
const MIN_TREE_WIDTH = 280;
const MAX_TREE_WIDTH = 640;

export const ArchitectureWorkbenchPage: React.FC = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const { user } = useModuleUser();
  const toast = useToast();
  const isAdmin = Boolean(user?.is_module_admin || user?.is_super_admin);

  // URL 状态读取
  const urlSubsystemId = searchParams.get('subsystem_id') || '';
  const urlGroupId = searchParams.get('group_id') || '';
  const urlModuleId = searchParams.get('module_id') || '';

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

  // 物理进程只读列表（供模块装配/绑定使用）
  const [processes, setProcesses] = useState<Process[]>([]);

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

  const [moduleDrawerOpen, setModuleDrawerOpen] = useState(false);
  const [editingMod, setEditingMod] = useState<Module | null>(null);
  const [moduleForm, setModuleForm] = useState({
    code: '',
    name: '',
    description: '',
    process_id: '',
    group_id: '',
  });
  // 视图模式: 逻辑架构 vs 物理进程 (URL 参数 ?tree=logical|physical)
  const initialTree = searchParams.get('tree') === 'physical' ? 'physical' : 'logical';
  const [treeView, setTreeView] = useState<'logical' | 'physical'>(initialTree);

  // 物理进程蓝图数据状态
  const urlProcessId = searchParams.get('process_id') || '';
  const [archProcessGroups, setArchProcessGroups] = useState<ArchProcessGroup[]>([]);
  const [selectedArchProcessId, setSelectedArchProcessId] = useState<string>(urlProcessId);
  const [selectedArchProcessDetail, setSelectedArchProcessDetail] = useState<ArchProcessDetailDTO | null>(null);
  const [autoPinnedOverrides, setAutoPinnedOverrides] = useState<AutoPinnedOverride[]>([]);

  // 物理进程组抽屉
  const [archGroupDrawerOpen, setArchGroupDrawerOpen] = useState(false);
  const [editingArchGroup, setEditingArchGroup] = useState<ArchProcessGroup | null>(null);
  const [archGroupForm, setArchGroupForm] = useState({ code: '', name: '', sort_order: 1, description: '' });

  // 物理进程抽屉
  const [archProcessDrawerOpen, setArchProcessDrawerOpen] = useState(false);
  const [editingArchProcess, setEditingArchProcess] = useState<ArchProcess | null>(null);
  const [archProcessForm, setArchProcessForm] = useState({
    group_id: '',
    code: '',
    name: '',
    default_listen_port: 8080,
    default_cmd_pattern: '',
    sort_order: 1,
    description: '',
  });

  // 左侧架构树面板宽度与拖拽调整 (支持持久化与双击复位)
  const [treePanelWidth, setTreePanelWidth] = useState<number>(() => {
    try {
      const saved = localStorage.getItem('code_arch_tree_panel_width');
      if (saved) {
        const parsed = parseInt(saved, 10);
        if (!isNaN(parsed) && parsed >= MIN_TREE_WIDTH && parsed <= MAX_TREE_WIDTH) {
          return parsed;
        }
      }
    } catch {
      // 容错
    }
    return DEFAULT_TREE_WIDTH;
  });
  const [isDraggingTreeWidth, setIsDraggingTreeWidth] = useState(false);

  const handleStartResizeTree = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    setIsDraggingTreeWidth(true);
    const startX = e.clientX;
    const startWidth = treePanelWidth;

    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';

    const handleMouseMove = (moveEvent: MouseEvent) => {
      const deltaX = moveEvent.clientX - startX;
      const newWidth = Math.min(MAX_TREE_WIDTH, Math.max(MIN_TREE_WIDTH, startWidth + deltaX));
      setTreePanelWidth(newWidth);
    };

    const handleMouseUp = (upEvent: MouseEvent) => {
      window.removeEventListener('mousemove', handleMouseMove);
      window.removeEventListener('mouseup', handleMouseUp);
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
      setIsDraggingTreeWidth(false);

      const deltaX = upEvent.clientX - startX;
      const finalWidth = Math.min(MAX_TREE_WIDTH, Math.max(MIN_TREE_WIDTH, startWidth + deltaX));
      try {
        localStorage.setItem('code_arch_tree_panel_width', String(finalWidth));
      } catch {
        // 容错
      }
    };

    window.addEventListener('mousemove', handleMouseMove);
    window.addEventListener('mouseup', handleMouseUp);
  }, [treePanelWidth]);

  const handleResetTreeWidth = useCallback(() => {
    setTreePanelWidth(DEFAULT_TREE_WIDTH);
    try {
      localStorage.setItem('code_arch_tree_panel_width', String(DEFAULT_TREE_WIDTH));
    } catch {
      // 容错
    }
  }, []);

  // 更新 URL Search Params 辅助函数
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

  // 监听浏览器 URL 变化
  useEffect(() => {
    const subId = searchParams.get('subsystem_id') || '';
    const grpId = searchParams.get('group_id') || '';
    const modId = searchParams.get('module_id') || '';
    const tree = searchParams.get('tree') === 'physical' ? 'physical' : 'logical';
    const procId = searchParams.get('process_id') || '';

    setTreeView((prev) => (prev !== tree ? tree : prev));
    if (subId) setSelectedSubsystemId((prev) => (String(prev) === String(subId) ? prev : String(subId)));
    if (grpId) setSelectedGroupId((prev) => (String(prev) === String(grpId) ? prev : String(grpId)));
    if (modId) setSelectedModuleId((prev) => (String(prev) === String(modId) ? prev : String(modId)));
    if (procId) setSelectedArchProcessId((prev) => (String(prev) === String(procId) ? prev : String(procId)));
  }, [searchParams]);

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

  // 加载全系统功能模块列表
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
    void loadSubsystemModules();
  }, [loadSubsystemModules]);

  // 加载物理进程列表（仅供模块装配绑定下拉框选用）
  const loadProcesses = useCallback(async () => {
    try {
      const res = await agentApi.listProcesses({ page_size: 1000 });
      setProcesses(res.items ?? []);
    } catch {
      // 容错处理
    }
  }, []);

  useEffect(() => {
    void loadProcesses();
  }, [loadProcesses]);

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
  }, [selectedSubsystemId, toast]);

  // 当功能组切换时，加载该组下的模块列表
  useEffect(() => {
    if (!selectedGroupId) return;
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
  }, [selectedGroupId, toast]);

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

  // 原子化选择功能模块叶子节点
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
      });
    },
    [updateParams]
  );

  // 当模块切换时，加载模块详情与服务契约
  useEffect(() => {
    if (!selectedModuleId) return;
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
  }, [selectedModuleId, toast]);

  // 加载物理进程树
  const loadArchProcessTree = useCallback(async () => {
    try {
      const res = await archApi.getArchProcessTree();
      const items = res.tree ?? res.items ?? [];
      setArchProcessGroups(items);
      if (items.length > 0) {
        setSelectedArchProcessId((prev) => {
          if (prev) {
            for (const g of items) {
              if (g.processes?.some((p) => String(p.id) === String(prev))) {
                return prev;
              }
            }
          }
          for (const g of items) {
            if (g.processes && g.processes.length > 0) {
              return String(g.processes[0].id);
            }
          }
          return '';
        });
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '加载物理进程树失败');
    }
  }, [toast]);

  useEffect(() => {
    void loadArchProcessTree();
  }, [loadArchProcessTree]);

  // 当选中的架构进程变更时，加载其详细契约与挂载血缘
  const loadArchProcessDetail = useCallback(async (id: string) => {
    if (!id) {
      setSelectedArchProcessDetail(null);
      return;
    }
    try {
      const res = await archApi.getArchProcess(id);
      setSelectedArchProcessDetail(res);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '加载架构进程详情失败');
    }
  }, [toast]);

  useEffect(() => {
    if (selectedArchProcessId) {
      void loadArchProcessDetail(selectedArchProcessId);
    } else {
      setSelectedArchProcessDetail(null);
    }
  }, [selectedArchProcessId, loadArchProcessDetail]);

  // 物理进程组 CRUD
  const handleSaveArchGroup = async () => {
    try {
      if (editingArchGroup) {
        await archApi.updateArchProcessGroup(editingArchGroup.id, archGroupForm);
        toast.success('物理进程组更新成功');
      } else {
        await archApi.createArchProcessGroup(archGroupForm);
        toast.success('物理进程组创建成功');
      }
      setArchGroupDrawerOpen(false);
      void loadArchProcessTree();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '保存物理进程组失败');
    }
  };

  const handleDeleteArchGroup = async (groupId: string | number) => {
    try {
      await archApi.deleteArchProcessGroup(groupId);
      toast.success('物理进程组已删除');
      void loadArchProcessTree();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '删除物理进程组失败');
    }
  };

  // 标准架构进程 CRUD
  const handleSaveArchProcess = async () => {
    try {
      if (editingArchProcess) {
        const res = await archApi.updateArchProcess(editingArchProcess.id, {
          group_id: archProcessForm.group_id,
          code: archProcessForm.code,
          name: archProcessForm.name,
          default_listen_port: archProcessForm.default_listen_port,
          default_cmd_pattern: archProcessForm.default_cmd_pattern,
          sort_order: archProcessForm.sort_order,
          description: archProcessForm.description,
        });
        toast.success('架构物理进程更新成功');
        if (res.auto_pinned_overrides && res.auto_pinned_overrides.length > 0) {
          setAutoPinnedOverrides(res.auto_pinned_overrides);
          toast.warning(`检测到端口冲突，已自动为 ${res.auto_pinned_overrides.length} 个实例固化覆盖端口`);
        } else {
          setAutoPinnedOverrides([]);
        }
      } else {
        const res = await archApi.createArchProcess({
          group_id: archProcessForm.group_id,
          code: archProcessForm.code,
          name: archProcessForm.name,
          default_listen_port: archProcessForm.default_listen_port,
          default_cmd_pattern: archProcessForm.default_cmd_pattern,
          sort_order: archProcessForm.sort_order,
          description: archProcessForm.description,
        });
        toast.success('架构物理进程创建成功');
        setSelectedArchProcessId(String(res.id));
        updateParams({ process_id: String(res.id) });
      }
      setArchProcessDrawerOpen(false);
      void loadArchProcessTree();
      if (selectedArchProcessId) {
        void loadArchProcessDetail(selectedArchProcessId);
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '保存架构物理进程失败');
    }
  };

  const handleDeleteArchProcess = async (procId: string | number) => {
    try {
      await archApi.deleteArchProcess(procId);
      toast.success('架构物理进程已删除');
      setSelectedArchProcessId('');
      setSelectedArchProcessDetail(null);
      void loadArchProcessTree();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '删除架构物理进程失败');
    }
  };

  // 模块与架构进程关联/解除
  const handleBindModuleToProcess = async (moduleId: string) => {
    if (!selectedArchProcessId) return;
    try {
      const curModIds = (selectedArchProcessDetail?.modules || []).map((m) => m.id);
      const newModIds = [...curModIds, moduleId];
      await archApi.updateArchProcess(selectedArchProcessId, {
        module_ids: newModIds,
      });
      toast.success('功能模块已成功关联至当前物理进程');
      void loadArchProcessDetail(selectedArchProcessId);
      void loadSubsystemModules();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '关联模块失败');
    }
  };

  const handleUnbindModuleFromProcess = async (moduleId: string) => {
    if (!selectedArchProcessId) return;
    try {
      const curModIds = (selectedArchProcessDetail?.modules || []).map((m) => String(m.id));
      const newModIds = curModIds.filter((id) => id !== String(moduleId));
      await archApi.updateArchProcess(selectedArchProcessId, {
        module_ids: newModIds,
      });
      toast.success('功能模块已成功从物理进程中解除关联');
      void loadArchProcessDetail(selectedArchProcessId);
      void loadSubsystemModules();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '解除模块关联失败');
    }
  };

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
        updateParams({ module_id: created.id, group_id: targetGroupId });
      }
      setModuleDrawerOpen(false);
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

  // 物理进程按 group_name 分组供模块编辑抽屉下拉选用
  const processGroupsMap = useMemo(() => {
    const map: Record<string, Process[]> = {};
    for (const p of processes) {
      const grp = (p.group_name || '默认进程组').trim();
      if (!map[grp]) map[grp] = [];
      map[grp].push(p);
    }
    return map;
  }, [processes]);

  return (
    <div className="code-workbench-page code-arch-workbench" data-testid="architecture-workbench">
      {/* 头部：视图切换与说明（居左布局，与架构拓扑保持一致） */}
      <div
        className="code-workbench-header code-arch-workbench-header"
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'flex-start',
          gap: '16px',
          marginBottom: '1rem',
          flexWrap: 'wrap',
        }}
      >
        {/* 逻辑架构 | 物理架构 双视图 Segmented Control 切换器（居左与拓扑保持一致） */}
        <div className="code-arch-segmented" role="tablist" data-testid="arch-tree-view-segmented">
          <button
            type="button"
            className={`code-arch-segmented__item ${treeView === 'logical' ? 'code-arch-segmented__item--active' : ''}`}
            onClick={() => {
              setTreeView('logical');
              updateParams({ tree: 'logical' });
            }}
            data-testid="toggle-tree-logical"
          >
            <Layers size={14} style={{ marginRight: 6, verticalAlign: 'text-bottom' }} />
            逻辑架构
          </button>
          <button
            type="button"
            className={`code-arch-segmented__item ${treeView === 'physical' ? 'code-arch-segmented__item--active' : ''}`}
            onClick={() => {
              setTreeView('physical');
              updateParams({ tree: 'physical' });
            }}
            data-testid="toggle-tree-physical"
          >
            <Server size={14} style={{ marginRight: 6, verticalAlign: 'text-bottom' }} />
            物理架构
          </button>
        </div>

        <div className="code-arch-toolbar__divider" style={{ height: 20 }} />

        <span className="code-workbench-subtitle" style={{ fontSize: '0.8125rem', color: 'var(--color-text-secondary)' }}>
          {treeView === 'physical'
            ? '维护标准物理进程、默认端口、特征命令行与多环境 Agent 探针挂载血缘'
            : '维护子系统、功能组与模块契约 (PROVIDE / DEPEND)'}
        </span>
      </div>

      {/* 视图主体：纯双栏布局（根据 treeView 切换） */}
      <div className="code-workbench-body">
        {treeView === 'physical' ? (
          <div className="code-arch-split-layout">
            <ArchProcessTreePanel
              panelWidth={treePanelWidth}
              groups={archProcessGroups}
              selectedProcessId={selectedArchProcessId}
              onSelectProcess={(id) => {
                setSelectedArchProcessId(id);
                updateParams({ process_id: id });
              }}
              isAdmin={isAdmin}
              onCreateGroup={() => {
                setEditingArchGroup(null);
                setArchGroupForm({ code: '', name: '', sort_order: 1, description: '' });
                setArchGroupDrawerOpen(true);
              }}
              onEditGroup={(g) => {
                setEditingArchGroup(g);
                setArchGroupForm({
                  code: g.code,
                  name: g.name,
                  sort_order: g.sort_order || 1,
                  description: g.description || '',
                });
                setArchGroupDrawerOpen(true);
              }}
              onDeleteGroup={(id) => void handleDeleteArchGroup(id)}
              onCreateProcess={(groupId) => {
                setEditingArchProcess(null);
                setArchProcessForm({
                  group_id: groupId
                    ? String(groupId)
                    : archProcessGroups[0]?.id
                    ? String(archProcessGroups[0].id)
                    : '',
                  code: '',
                  name: '',
                  default_listen_port: 8080,
                  default_cmd_pattern: '',
                  sort_order: 1,
                  description: '',
                });
                setArchProcessDrawerOpen(true);
              }}
            />

            <div
              className={`code-arch-split-resizer ${isDraggingTreeWidth ? 'code-arch-split-resizer--dragging' : ''}`}
              onMouseDown={handleStartResizeTree}
              onDoubleClick={handleResetTreeWidth}
              title="拖拽调整左侧树面板宽度 (双击重置为 380px)"
            />

            {selectedArchProcessDetail ? (
              <ArchProcessDetailPane
                archProcess={selectedArchProcessDetail.arch_process}
                modules={selectedArchProcessDetail.modules || []}
                providedServices={selectedArchProcessDetail.provided_services || []}
                dependencies={selectedArchProcessDetail.dependencies || []}
                agentMounts={selectedArchProcessDetail.agent_mounts || []}
                autoPinnedOverrides={autoPinnedOverrides}
                allModules={subsystemModules}
                isAdmin={isAdmin}
                onEditProcess={() => {
                  const p = selectedArchProcessDetail.arch_process;
                  setEditingArchProcess(p);
                  setArchProcessForm({
                    group_id: String(p.group_id),
                    code: p.code,
                    name: p.name,
                    default_listen_port: p.default_listen_port,
                    default_cmd_pattern: p.default_cmd_pattern || '',
                    sort_order: p.sort_order || 1,
                    description: p.description || '',
                  });
                  setArchProcessDrawerOpen(true);
                }}
                onDeleteProcess={() =>
                  void handleDeleteArchProcess(selectedArchProcessDetail.arch_process.id)
                }
                onUnbindModule={handleUnbindModuleFromProcess}
                onBindModule={handleBindModuleToProcess}
              />
            ) : (
              <div
                className="code-module-pane code-empty-pane"
                style={{
                  flex: 1,
                  display: 'flex',
                  flexDirection: 'column',
                  alignItems: 'center',
                  justifyContent: 'center',
                  padding: '4rem 2rem',
                  textAlign: 'center',
                  background: 'var(--color-bg-surface)',
                  borderRadius: 'var(--radius-lg)',
                  border: '1px solid var(--color-border-primary)',
                }}
                data-testid="arch-physical-empty-state"
              >
                <Server size={48} style={{ color: 'var(--color-text-muted)', marginBottom: '1rem' }} />
                <h4 style={{ color: 'var(--color-text-primary)', marginBottom: '0.5rem', fontSize: '1.125rem' }}>
                  请选择左侧标准架构进程
                </h4>
                <p style={{ color: 'var(--color-text-secondary)', fontSize: '0.875rem', maxWidth: '420px', lineHeight: 1.6 }}>
                  在左侧物理进程蓝图树中展开分组，选择一个进程以查阅其承载的功能模块、全系统对外契约全貌与多环境 Agent 挂载溯源
                </p>
              </div>
            )}
          </div>
        ) : (
          <div className="code-arch-split-layout">
            <SubsystemGroupTreePanel
              panelWidth={treePanelWidth}
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
                  updateParams({ module_id: modId });
                }
              }}
              onSelectModuleLeaf={handleSelectModuleLeaf}
              onCreateModule={(groupId) => {
                setEditingMod(null);
                setModuleForm({ code: '', name: '', description: '', process_id: '', group_id: groupId });
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
                setModuleDrawerOpen(true);
              }}
              onDeleteModule={(id) => void handleDeleteModule(id)}
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

            <div
              className={`code-arch-split-resizer ${isDraggingTreeWidth ? 'code-arch-split-resizer--dragging' : ''}`}
              onMouseDown={handleStartResizeTree}
              onDoubleClick={handleResetTreeWidth}
              title="拖拽调整左侧树面板宽度 (双击重置为 380px)"
            />

            {selectedModule ? (
              <ModuleContractDetailPane
                modules={modules}
                selectedModuleId={selectedModuleId}
                onSelectModule={(id) => {
                  const targetMod = subsystemModules.find((m) => String(m.id) === String(id));
                  if (targetMod && targetMod.subsystem_id && targetMod.group_id) {
                    handleSelectModuleLeaf(targetMod.subsystem_id, targetMod.group_id, id);
                  } else {
                    void (async () => {
                      try {
                        const res = await archApi.getModule(id);
                        if (res.module && res.module.subsystem_id && res.module.group_id) {
                          handleSelectModuleLeaf(res.module.subsystem_id, res.module.group_id, id);
                          return;
                        }
                      } catch {
                        // ignore
                      }
                      setSelectedModuleId(id);
                      updateParams({ module_id: id });
                    })();
                  }
                }}
                selectedModule={selectedModule}
                services={services}
                dependencies={dependencies}
                allProcesses={processes}
                isAdmin={isAdmin}
                currentSubsystem={subsystems.find((s) => s.id === selectedSubsystemId) || null}
                currentGroup={selectedGroupId ? findGroupInTree(groupTree, selectedGroupId) : null}
                onCreateModule={() => {
                  setEditingMod(null);
                  setModuleForm({
                    code: '',
                    name: '',
                    description: '',
                    process_id: '',
                    group_id: selectedGroupId,
                  });
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
                  setModuleDrawerOpen(true);
                }}
                onDeleteModule={(id) => void handleDeleteModule(id)}
                onViewArchProcess={(procId) => {
                  setTreeView('physical');
                  setSelectedArchProcessId(String(procId));
                  updateParams({ tree: 'physical', process_id: String(procId) });
                }}
                onCreateService={handleCreateService}
                onUpdateServiceIgnorePaths={handleUpdateServiceIgnorePaths}
                onDeleteService={handleDeleteService}
                onCreateDependency={handleCreateDependency}
                onDeleteDependency={handleDeleteDependency}
                onBindProcess={handleBindModuleProcess}
              />
            ) : (
              <div
                className="code-module-pane code-empty-pane"
                style={{
                  flex: 1,
                  display: 'flex',
                  flexDirection: 'column',
                  alignItems: 'center',
                  justifyContent: 'center',
                  padding: '4rem 2rem',
                  textAlign: 'center',
                  background: 'var(--color-bg-surface)',
                  borderRadius: 'var(--radius-lg)',
                  border: '1px solid var(--color-border-primary)',
                }}
                data-testid="arch-empty-state"
              >
                <Layers size={48} style={{ color: 'var(--color-text-muted)', marginBottom: '1rem' }} />
                <h4 style={{ color: 'var(--color-text-primary)', marginBottom: '0.5rem', fontSize: '1.125rem' }}>
                  请选择左侧功能模块
                </h4>
                <p style={{ color: 'var(--color-text-secondary)', fontSize: '0.875rem', maxWidth: '420px', lineHeight: 1.6 }}>
                  在左侧架构树中展开子系统与功能组，选择一个模块以查阅其服务提供 (PROVIDE)、依赖消费 (DEPEND) 与装载进程
                </p>
              </div>
            )}
          </div>
        )}
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
              <option value="">-- 作为顶级功能组 (无父节点) --</option>
              {groupTree
                .filter((g) => g.id !== movingGroup?.id)
                .map((g) => (
                  <option key={g.id} value={g.id}>
                    {g.name} ({g.code})
                  </option>
                ))}
            </select>
          </div>
        </div>
      </Drawer>

      {/* 4. 功能模块侧边栏 (Drawer) */}
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
            <label className="code-form-label">宿主业务进程 (Process):</label>
            <select
              className="code-select"
              value={moduleForm.process_id}
              onChange={(e) => setModuleForm({ ...moduleForm, process_id: e.target.value })}
            >
              <option value="">-- 暂不绑定宿主进程 (稍后在 Agent 中心绑定) --</option>
              {Object.entries(processGroupsMap).map(([groupName, procs]) => (
                <optgroup key={groupName} label={`【${groupName}】`}>
                  {procs.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name} {p.listen_port > 0 ? `(Port: ${p.listen_port})` : '(纯客户端)'}
                    </option>
                  ))}
                </optgroup>
              ))}
            </select>
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

      {/* 5. 物理进程组侧边栏 (Drawer) */}
      <Drawer
        open={archGroupDrawerOpen}
        onClose={() => setArchGroupDrawerOpen(false)}
        title={editingArchGroup ? '编辑物理进程组' : '新建物理进程组'}
        subtitle="维护逻辑划分的物理进程分组 (如 CORE_SRV, GATEWAY)"
        width="md"
        footer={
          <div className="code-drawer-footer">
            <button
              type="button"
              className="code-btn code-btn--secondary"
              onClick={() => setArchGroupDrawerOpen(false)}
            >
              取消
            </button>
            <button
              type="button"
              className="code-btn code-btn--primary"
              onClick={() => void handleSaveArchGroup()}
            >
              保存
            </button>
          </div>
        }
      >
        <div className="code-form">
          <div className="code-form-item">
            <label className="code-form-label">物理进程组编码 (2..64 大写字母/数字/下划线/中划线):</label>
            <input
              type="text"
              className="code-input"
              value={archGroupForm.code}
              onChange={(e) => setArchGroupForm({ ...archGroupForm, code: e.target.value.toUpperCase() })}
              placeholder="如 CORE_SERVICES"
            />
          </div>
          <div className="code-form-item">
            <label className="code-form-label">物理进程组名称:</label>
            <input
              type="text"
              className="code-input"
              value={archGroupForm.name}
              onChange={(e) => setArchGroupForm({ ...archGroupForm, name: e.target.value })}
              placeholder="如 核心业务服务组"
            />
          </div>
          <div className="code-form-item">
            <label className="code-form-label">排序权重 (数字越小越靠前):</label>
            <input
              type="number"
              className="code-input"
              value={archGroupForm.sort_order}
              onChange={(e) =>
                setArchGroupForm({ ...archGroupForm, sort_order: parseInt(e.target.value, 10) || 1 })
              }
            />
          </div>
          <div className="code-form-item">
            <label className="code-form-label">描述说明:</label>
            <textarea
              className="code-textarea"
              value={archGroupForm.description}
              onChange={(e) => setArchGroupForm({ ...archGroupForm, description: e.target.value })}
              placeholder="说明物理进程组的分类定位"
            />
          </div>
        </div>
      </Drawer>

      {/* 6. 标准架构物理进程侧边栏 (Drawer) */}
      <Drawer
        open={archProcessDrawerOpen}
        onClose={() => setArchProcessDrawerOpen(false)}
        title={editingArchProcess ? '编辑标准架构物理进程' : '新建标准架构物理进程'}
        subtitle="定义跨多套环境 1:1 挂载映射的蓝图进程底座"
        width="md"
        footer={
          <div className="code-drawer-footer">
            <button
              type="button"
              className="code-btn code-btn--secondary"
              onClick={() => setArchProcessDrawerOpen(false)}
            >
              取消
            </button>
            <button
              type="button"
              className="code-btn code-btn--primary"
              onClick={() => void handleSaveArchProcess()}
            >
              保存
            </button>
          </div>
        }
      >
        <div className="code-form">
          <div className="code-form-item">
            <label className="code-form-label">所属物理进程组:</label>
            <select
              className="code-select"
              value={archProcessForm.group_id}
              onChange={(e) => setArchProcessForm({ ...archProcessForm, group_id: e.target.value })}
            >
              <option value="">-- 请选择进程组 --</option>
              {archProcessGroups.map((g) => (
                <option key={g.id} value={g.id}>
                  {g.name} ({g.code})
                </option>
              ))}
            </select>
          </div>
          <div className="code-form-item">
            <label className="code-form-label">进程全局唯一编码 (code):</label>
            <input
              type="text"
              className="code-input"
              value={archProcessForm.code}
              onChange={(e) => setArchProcessForm({ ...archProcessForm, code: e.target.value })}
              placeholder="如 proc-account-service"
            />
          </div>
          <div className="code-form-item">
            <label className="code-form-label">进程名称:</label>
            <input
              type="text"
              className="code-input"
              value={archProcessForm.name}
              onChange={(e) => setArchProcessForm({ ...archProcessForm, name: e.target.value })}
              placeholder="如 账户账务中心进程"
            />
          </div>
          <div className="code-form-item">
            <label className="code-form-label">默认监听端口 (0 表示纯客户端):</label>
            <input
              type="number"
              className="code-input"
              value={archProcessForm.default_listen_port}
              onChange={(e) =>
                setArchProcessForm({
                  ...archProcessForm,
                  default_listen_port: parseInt(e.target.value, 10) || 0,
                })
              }
              min={0}
              max={65535}
            />
          </div>
          <div className="code-form-item">
            <label className="code-form-label">
              默认命令行匹配特征 (纯客户端必填, 支持 re: 正则):
            </label>
            <input
              type="text"
              className="code-input"
              value={archProcessForm.default_cmd_pattern}
              onChange={(e) =>
                setArchProcessForm({ ...archProcessForm, default_cmd_pattern: e.target.value })
              }
              placeholder="如 re:.*account-service.* 或 account-srv"
            />
          </div>
          <div className="code-form-item">
            <label className="code-form-label">排序权重 (数字越小越靠前):</label>
            <input
              type="number"
              className="code-input"
              value={archProcessForm.sort_order}
              onChange={(e) =>
                setArchProcessForm({
                  ...archProcessForm,
                  sort_order: parseInt(e.target.value, 10) || 1,
                })
              }
            />
          </div>
          <div className="code-form-item">
            <label className="code-form-label">描述说明:</label>
            <textarea
              className="code-textarea"
              value={archProcessForm.description}
              onChange={(e) => setArchProcessForm({ ...archProcessForm, description: e.target.value })}
              placeholder="说明物理进程的职能与部署特征"
            />
          </div>
        </div>
      </Drawer>
    </div>
  );
};

export default ArchitectureWorkbenchPage;
