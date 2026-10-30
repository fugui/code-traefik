import React, { useCallback, useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Modal } from '@code/common';
import { useToast } from '../../utils/toast';
import { useModuleUser } from '../../context/ModuleUserContext';
import * as archApi from '../../api/arch';
import * as trafficApi from '../../api/traffic';
import type {
  Subsystem,
  FunctionalGroupTreeNode,
  Module,
  ModuleInterface,
  StaticTopologyDTO,
  ConsistencyReportDTO,
  RecordingSession,
} from '../../types';
import { SubsystemGroupTreePanel } from './SubsystemGroupTreePanel';
import { ModuleContractDetailPane } from './ModuleContractDetailPane';
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

  // 弹窗状态
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

  const [moduleModalOpen, setModuleModalOpen] = useState(false);
  const [editingMod, setEditingMod] = useState<Module | null>(null);
  const [moduleForm, setModuleForm] = useState({ code: '', name: '', description: '' });

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
      if (editingMod) {
        await archApi.updateModule(editingMod.id, {
          group_id: selectedGroupId,
          ...moduleForm,
        });
        toast.success('模块修改成功');
      } else {
        await archApi.createModule({
          group_id: selectedGroupId,
          ...moduleForm,
        });
        toast.success('模块创建成功');
      }
      setModuleModalOpen(false);
      const res = await archApi.listModules({ group_id: selectedGroupId, page_size: 100 });
      setModules(res.items);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '操作模块失败');
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

            <ModuleContractDetailPane
              modules={modules}
              selectedModuleId={selectedModuleId}
              onSelectModule={setSelectedModuleId}
              selectedModule={selectedModule}
              interfaces={interfaces}
              isAdmin={isAdmin}
              currentSubsystem={subsystems.find((s) => s.id === selectedSubsystemId) || null}
              currentGroup={selectedGroupId ? findGroupInTree(groupTree, selectedGroupId) : null}
              onCreateModule={() => {
                setEditingMod(null);
                setModuleForm({ code: '', name: '', description: '' });
                setModuleModalOpen(true);
              }}
              onEditModule={(mod) => {
                setEditingMod(mod);
                setModuleForm({ code: mod.code, name: mod.name, description: mod.description || '' });
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
            <label className="code-form-label">描述说明:</label>
            <textarea
              className="code-textarea"
              value={moduleForm.description}
              onChange={(e) => setModuleForm({ ...moduleForm, description: e.target.value })}
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
