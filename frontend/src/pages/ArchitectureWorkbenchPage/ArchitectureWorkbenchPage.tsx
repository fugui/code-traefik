import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Layers } from 'lucide-react';
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
} from '../../types';
import { SubsystemGroupTreePanel } from './SubsystemGroupTreePanel';
import { ModuleContractDetailPane } from './ModuleContractDetailPane';

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

    if (subId) setSelectedSubsystemId((prev) => (String(prev) === String(subId) ? prev : String(subId)));
    if (grpId) setSelectedGroupId((prev) => (String(prev) === String(grpId) ? prev : String(grpId)));
    if (modId) setSelectedModuleId((prev) => (String(prev) === String(modId) ? prev : String(modId)));
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
      {/* 头部标题 */}
      <div className="code-workbench-header">
        <div className="code-workbench-title-group">
          <h2 className="code-workbench-title">三级逻辑架构台账</h2>
          <span className="code-workbench-subtitle">
            维护子系统、8 级功能组与模块契约 (PROVIDE / DEPEND)
          </span>
        </div>
      </div>

      {/* 视图主体：纯双栏布局（左树 + 右模块契约详情） */}
      <div className="code-workbench-body">
        <div className="code-arch-split-layout">
          <SubsystemGroupTreePanel
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

          {selectedModule ? (
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
    </div>
  );
};

export default ArchitectureWorkbenchPage;
