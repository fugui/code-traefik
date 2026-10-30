import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ChevronRight,
  ChevronDown,
  Plus,
  Edit,
  Trash2,
  Folder,
  FolderOpen,
  Cpu,
  Search,
  ArrowRightLeft,
  Server,
  Layers,
  FileCode,
} from 'lucide-react';
import type { Subsystem, FunctionalGroupTreeNode, Process, Module } from '../../types';

export type TreeViewMode = 'LOGICAL' | 'PHYSICAL';

export interface SubsystemGroupTreePanelProps {
  // 视角模式
  viewMode?: TreeViewMode;
  onViewModeChange?: (mode: TreeViewMode) => void;

  // 逻辑架构树相关
  subsystems: Subsystem[];
  selectedSubsystemId: string;
  onSelectSubsystem: (id: string) => void;
  groupTree: FunctionalGroupTreeNode[];
  groupTreeMap?: Record<string, FunctionalGroupTreeNode[]>;
  onLoadSubsystemTree?: (subsystemId: string) => Promise<FunctionalGroupTreeNode[]>;
  selectedGroupId: string;
  onSelectGroup: (id: string) => void;

  // 逻辑树功能模块扩展（支持直接展开至 FM）
  modules?: Module[];
  selectedModuleId?: string;
  onSelectModule?: (moduleId: string) => void;
  onCreateModule?: (groupId: string) => void;
  onEditModule?: (mod: Module) => void;
  onDeleteModule?: (moduleId: string) => void;

  // 物理架构树相关
  processes?: Process[];
  selectedProcessId?: string;
  onSelectProcess?: (processId: string) => void;
  customProcessGroups?: string[];
  selectedProcessGroupName?: string;
  onSelectProcessGroup?: (groupName: string) => void;
  onCreateProcessGroup?: () => void;
  onEditProcessGroup?: (groupName: string) => void;
  onDeleteProcessGroup?: (groupName: string) => void;
  onCreateProcess?: (targetGroupName?: string) => void;
  onEditProcess?: (proc: Process) => void;
  onDeleteProcess?: (procId: string) => void;

  // 权限与逻辑架构管理操作
  isAdmin: boolean;
  onCreateSubsystem: () => void;
  onEditSubsystem: (sub: Subsystem) => void;
  onDeleteSubsystem: (id: string) => void;
  onCreateGroup: (parentId?: string | null, targetSubsystemId?: string) => void;
  onEditGroup: (group: FunctionalGroupTreeNode) => void;
  onMoveGroup: (group: FunctionalGroupTreeNode) => void;
  onDeleteGroup: (id: string) => void;
}

function isTextMatch(text?: string, query?: string): boolean {
  if (!text || !query?.trim()) return false;
  return text.toLowerCase().includes(query.trim().toLowerCase());
}

function isModuleMatch(mod: Module, query: string): boolean {
  if (!query?.trim()) return false;
  return (
    isTextMatch(mod.name, query) ||
    isTextMatch(mod.code, query) ||
    isTextMatch(mod.description, query) ||
    isTextMatch(mod.process?.name, query)
  );
}

function checkGroupTreeMatch(
  nodes: FunctionalGroupTreeNode[],
  modulesByGroup: Record<string, Module[]>,
  query: string,
  toExpand: Set<string>
): boolean {
  let hasMatch = false;
  for (const node of nodes) {
    const nodeMatched = isTextMatch(node.name, query) || isTextMatch(node.code, query);
    const moduleMatched = (modulesByGroup[node.id] ?? []).some((m) => isModuleMatch(m, query));
    let childMatched = false;
    if (node.children && node.children.length > 0) {
      childMatched = checkGroupTreeMatch(node.children, modulesByGroup, query, toExpand);
    }
    if (nodeMatched || moduleMatched || childMatched) {
      toExpand.add(node.id);
      hasMatch = true;
    }
  }
  return hasMatch;
}

function filterGroupNodes(
  nodes: FunctionalGroupTreeNode[],
  modulesByGroup: Record<string, Module[]>,
  query: string
): FunctionalGroupTreeNode[] {
  if (!query.trim()) return nodes;

  const result: FunctionalGroupTreeNode[] = [];
  for (const node of nodes) {
    const selfMatch = isTextMatch(node.name, query) || isTextMatch(node.code, query);
    const hasModuleMatch = (modulesByGroup[node.id] ?? []).some((m) => isModuleMatch(m, query));
    const filteredChildren = node.children ? filterGroupNodes(node.children, modulesByGroup, query) : [];
    if (selfMatch || hasModuleMatch || filteredChildren.length > 0) {
      result.push({
        ...node,
        children: filteredChildren,
      });
    }
  }
  return result;
}

export const SubsystemGroupTreePanel: React.FC<SubsystemGroupTreePanelProps> = ({
  viewMode: controlledViewMode,
  onViewModeChange,
  subsystems,
  selectedSubsystemId,
  onSelectSubsystem,
  groupTree,
  groupTreeMap = {},
  onLoadSubsystemTree,
  selectedGroupId,
  onSelectGroup,
  modules = [],
  selectedModuleId = '',
  onSelectModule,
  onCreateModule,
  onEditModule,
  onDeleteModule,
  processes = [],
  selectedProcessId = '',
  onSelectProcess,
  customProcessGroups = [],
  selectedProcessGroupName = '',
  onSelectProcessGroup,
  onCreateProcessGroup,
  onEditProcessGroup,
  onDeleteProcessGroup,
  onCreateProcess,
  onEditProcess,
  onDeleteProcess,
  isAdmin,
  onCreateSubsystem,
  onEditSubsystem,
  onDeleteSubsystem,
  onCreateGroup,
  onEditGroup,
  onMoveGroup,
  onDeleteGroup,
}) => {
  // 视角模式状态（逻辑树 vs 物理进程树）
  const [internalViewMode, setInternalViewMode] = useState<TreeViewMode>('LOGICAL');
  const viewMode = controlledViewMode ?? internalViewMode;

  const handleSwitchViewMode = (mode: TreeViewMode) => {
    if (onViewModeChange) {
      onViewModeChange(mode);
    } else {
      setInternalViewMode(mode);
    }
  };

  // 逻辑树状态
  const [expandedSubsystemIds, setExpandedSubsystemIds] = useState<Set<string>>(
    () => new Set(selectedSubsystemId ? [selectedSubsystemId] : [])
  );
  const [expandedGroupIds, setExpandedGroupIds] = useState<Set<string>>(new Set());

  // 物理架构树状态（两级目录：进程分组 ➔ 业务进程）
  const [expandedProcessGroupNames, setExpandedProcessGroupNames] = useState<Set<string>>(new Set());

  const [searchQuery, setSearchQuery] = useState('');

  // 模块按功能组归类映射
  const modulesByGroupId = useMemo(() => {
    const map: Record<string, Module[]> = {};
    for (const mod of modules) {
      if (!map[mod.group_id]) {
        map[mod.group_id] = [];
      }
      map[mod.group_id].push(mod);
    }
    return map;
  }, [modules]);

  // 默认展开当前选中的子系统与当前选中的功能组
  useEffect(() => {
    if (selectedSubsystemId) {
      setExpandedSubsystemIds((prev) => {
        const next = new Set(prev);
        next.add(selectedSubsystemId);
        return next;
      });
    }
  }, [selectedSubsystemId]);

  useEffect(() => {
    if (selectedGroupId) {
      setExpandedGroupIds((prev) => {
        const next = new Set(prev);
        next.add(selectedGroupId);
        return next;
      });
    }
  }, [selectedGroupId]);

  // 当选中某个模块时，自动展开其所在的功能组
  useEffect(() => {
    if (!selectedModuleId || !modules.length) return;
    const targetMod = modules.find((m) => m.id === selectedModuleId);
    if (targetMod?.group_id) {
      setExpandedGroupIds((prev) => {
        const next = new Set(prev);
        next.add(targetMod.group_id);
        return next;
      });
    }
  }, [selectedModuleId, modules]);

  // 当进入物理进程树时，默认展开所有进程组
  useEffect(() => {
    if (viewMode === 'PHYSICAL') {
      setExpandedProcessGroupNames((prev) => {
        const next = new Set(prev);
        next.add('默认进程组');
        for (const g of customProcessGroups) {
          if (g.trim()) next.add(g.trim());
        }
        for (const p of processes) {
          next.add(p.group_name || '默认进程组');
        }
        return next;
      });
    }
  }, [viewMode, processes, customProcessGroups]);

  // 辅助函数：获取某子系统下的功能组树
  const getSubsystemGroupTree = useCallback(
    (subsystemId: string): FunctionalGroupTreeNode[] => {
      if (subsystemId === selectedSubsystemId && groupTree.length > 0) {
        return groupTree;
      }
      return groupTreeMap[subsystemId] ?? (subsystemId === selectedSubsystemId ? groupTree : []);
    },
    [selectedSubsystemId, groupTree, groupTreeMap]
  );

  // 切换子系统展开
  const toggleSubsystemExpand = (id: string, e?: React.MouseEvent) => {
    if (e) e.stopPropagation();
    setExpandedSubsystemIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
        if (onLoadSubsystemTree && !groupTreeMap[id] && id !== selectedSubsystemId) {
          void onLoadSubsystemTree(id);
        }
      }
      return next;
    });
  };

  // 切换功能组展开
  const toggleGroupExpand = (id: string, e: React.MouseEvent) => {
    e.stopPropagation();
    setExpandedGroupIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  // 切换进程组展开
  const toggleProcessGroupExpand = (groupName: string, e: React.MouseEvent) => {
    e.stopPropagation();
    setExpandedProcessGroupNames((prev) => {
      const next = new Set(prev);
      if (next.has(groupName)) next.delete(groupName);
      else next.add(groupName);
      return next;
    });
  };

  // 搜索时自动展开匹配路径
  useEffect(() => {
    if (!searchQuery.trim()) return;

    if (viewMode === 'LOGICAL') {
      const toExpandSub = new Set<string>();
      const toExpandGrp = new Set<string>();

      for (const sub of subsystems) {
        const subTree = getSubsystemGroupTree(sub.id);
        const subMatched = isTextMatch(sub.name, searchQuery) || isTextMatch(sub.code, searchQuery);
        const grpMatched = checkGroupTreeMatch(subTree, modulesByGroupId, searchQuery, toExpandGrp);

        if (subMatched || grpMatched) {
          toExpandSub.add(sub.id);
        }
      }

      setExpandedSubsystemIds((prev) => new Set([...prev, ...toExpandSub]));
      setExpandedGroupIds((prev) => new Set([...prev, ...toExpandGrp]));
    } else {
      // 物理进程树搜索：展开匹配的组
      const toExpandGroups = new Set<string>();

      for (const p of processes) {
        const groupName = p.group_name || '默认进程组';
        const groupMatched = isTextMatch(groupName, searchQuery);
        const procMatched =
          isTextMatch(p.name, searchQuery) ||
          isTextMatch(String(p.listen_port), searchQuery) ||
          isTextMatch(p.cmd_pattern, searchQuery);

        let moduleMatched = false;
        for (const pm of p.process_modules ?? []) {
          if (
            isTextMatch(pm.module?.name, searchQuery) ||
            isTextMatch(pm.module?.code, searchQuery)
          ) {
            moduleMatched = true;
            break;
          }
        }

        if (groupMatched || procMatched || moduleMatched) {
          toExpandGroups.add(groupName);
        }
      }

      for (const g of customProcessGroups) {
        if (isTextMatch(g, searchQuery)) {
          toExpandGroups.add(g);
        }
      }

      setExpandedProcessGroupNames((prev) => new Set([...prev, ...toExpandGroups]));
    }
  }, [searchQuery, subsystems, processes, customProcessGroups, viewMode, getSubsystemGroupTree, modulesByGroupId]);

  // 过滤后的子系统列表
  const filteredSubsystems = useMemo(() => {
    if (!searchQuery.trim()) return subsystems;

    return subsystems.filter((sub) => {
      const subMatched = isTextMatch(sub.name, searchQuery) || isTextMatch(sub.code, searchQuery);
      if (subMatched) return true;
      const subTree = getSubsystemGroupTree(sub.id);
      const toExpandDummy = new Set<string>();
      return checkGroupTreeMatch(subTree, modulesByGroupId, searchQuery, toExpandDummy);
    });
  }, [subsystems, searchQuery, getSubsystemGroupTree, modulesByGroupId]);

  // 物理进程两级目录聚合（进程分组 ➔ 进程列表）
  const processGroupList = useMemo(() => {
    const map = new Map<string, Process[]>();

    // 默认进程组首先放入
    map.set('默认进程组', []);

    // 自定义显式创建的进程组放入
    for (const g of customProcessGroups) {
      const trimmed = g.trim();
      if (trimmed && !map.has(trimmed)) {
        map.set(trimmed, []);
      }
    }

    // 归入所有物理进程
    for (const proc of processes) {
      const groupName = (proc.group_name || '默认进程组').trim();
      if (!map.has(groupName)) {
        map.set(groupName, []);
      }
      map.get(groupName)!.push(proc);
    }

    const groups = Array.from(map.entries()).map(([groupName, procs]) => ({
      groupName,
      processes: procs,
    }));

    if (!searchQuery.trim()) return groups;

    // 过滤匹配的进程组
    return groups
      .map((g) => {
        const groupMatched = isTextMatch(g.groupName, searchQuery);
        const filteredProcs = g.processes.filter((p) => {
          if (groupMatched) return true;
          const procMatched =
            isTextMatch(p.name, searchQuery) ||
            isTextMatch(String(p.listen_port), searchQuery) ||
            isTextMatch(p.cmd_pattern, searchQuery);
          if (procMatched) return true;
          return (p.process_modules ?? []).some(
            (pm) =>
              isTextMatch(pm.module?.name, searchQuery) ||
              isTextMatch(pm.module?.code, searchQuery)
          );
        });

        if (groupMatched || filteredProcs.length > 0) {
          return {
            groupName: g.groupName,
            processes: filteredProcs,
          };
        }
        return null;
      })
      .filter((g): g is { groupName: string; processes: Process[] } => g !== null);
  }, [processes, customProcessGroups, searchQuery]);

  // 渲染逻辑功能组节点（一直递归展开到 功能模块 FM）
  const renderGroupNode = (node: FunctionalGroupTreeNode, subsystemId: string, depth = 1) => {
    const groupMods = modulesByGroupId[node.id] ?? [];
    const filteredMods = searchQuery.trim()
      ? groupMods.filter((m) => isModuleMatch(m, searchQuery) || isTextMatch(node.name, searchQuery))
      : groupMods;

    const hasChildrenGroups = Boolean(node.children && node.children.length > 0);
    const hasChildrenModules = groupMods.length > 0;
    const hasAnyChildren = hasChildrenGroups || hasChildrenModules;
    const isExpanded = expandedGroupIds.has(node.id) || Boolean(searchQuery.trim());
    const isSelected = selectedGroupId === node.id && !selectedModuleId;

    return (
      <div key={node.id} className="code-tree-node-wrapper">
        <div
          className={`code-tree-node code-tree-node--group ${isSelected ? 'code-tree-node--selected' : ''}`}
          style={{ paddingLeft: `${depth * 16 + 8}px` }}
          onClick={() => {
            onSelectSubsystem(subsystemId);
            onSelectGroup(node.id);
          }}
        >
          <div className="code-tree-node__left">
            <button
              type="button"
              className="code-tree-expand-btn"
              onClick={(e) => toggleGroupExpand(node.id, e)}
              style={{ visibility: hasAnyChildren ? 'visible' : 'hidden' }}
              title={isExpanded ? '折叠' : '展开'}
            >
              {isExpanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
            </button>
            {isExpanded ? (
              <FolderOpen size={16} className="code-tree-icon--group" style={{ color: 'var(--color-warning, #eab308)' }} />
            ) : (
              <Folder size={16} className="code-tree-icon--group" style={{ color: 'var(--color-warning, #eab308)' }} />
            )}
            <span className="code-tree-node__name" title={`${node.name} [${node.code}]`}>
              {node.name}
              <span className="code-tree-node__code">[{node.code}]</span>
            </span>
            {groupMods.length > 0 && (
              <span className="code-tree-node__badge" title={`包含 ${groupMods.length} 个功能模块`}>
                {groupMods.length} FM
              </span>
            )}
          </div>

          {isAdmin && (
            <div className="code-tree-node__actions" onClick={(e) => e.stopPropagation()}>
              {onCreateModule && (
                <button
                  type="button"
                  className="code-tree-action-btn"
                  onClick={() => onCreateModule(node.id)}
                  title="新建功能模块"
                >
                  <FileCode size={13} style={{ color: '#10b981' }} />
                </button>
              )}
              <button
                type="button"
                className="code-tree-action-btn"
                onClick={() => onCreateGroup(node.id, subsystemId)}
                title="新增子功能组"
              >
                <Plus size={14} />
              </button>
              <button
                type="button"
                className="code-tree-action-btn"
                onClick={() => onEditGroup(node)}
                title="编辑功能组"
              >
                <Edit size={14} />
              </button>
              <button
                type="button"
                className="code-tree-action-btn"
                onClick={() => onMoveGroup(node)}
                title="移动功能组 (变更父级)"
              >
                <ArrowRightLeft size={14} />
              </button>
              <button
                type="button"
                className="code-tree-action-btn code-tree-action-btn--danger"
                onClick={() => onDeleteGroup(node.id)}
                title="删除功能组"
              >
                <Trash2 size={14} />
              </button>
            </div>
          )}
        </div>

        {/* 展开子功能组与功能模块 */}
        {isExpanded && (
          <div className="code-tree-children">
            {/* 1. 子功能组递归 */}
            {hasChildrenGroups && (node.children ?? []).map((child) => renderGroupNode(child, subsystemId, depth + 1))}

            {/* 2. 属于当前功能组的功能模块 (FM 叶子节点) */}
            {filteredMods.map((mod) => {
              const isModSelected = selectedModuleId === mod.id;
              return (
                <div
                  key={mod.id}
                  className={`code-tree-node code-tree-node--module-leaf ${isModSelected ? 'code-tree-node--selected' : ''}`}
                  style={{ paddingLeft: `${(depth + 1) * 16 + 8}px` }}
                  onClick={(e) => {
                    e.stopPropagation();
                    onSelectSubsystem(subsystemId);
                    onSelectGroup(node.id);
                    if (onSelectModule) onSelectModule(mod.id);
                  }}
                >
                  <div className="code-tree-node__left">
                    <span style={{ width: '14px', flexShrink: 0 }} />
                    <FileCode size={14} style={{ color: '#10b981', flexShrink: 0 }} />
                    <span className="code-tree-node__name" title={`${mod.name} [${mod.code}]`}>
                      {mod.name}
                      <span className="code-tree-node__code">[{mod.code}]</span>
                    </span>
                    {mod.process && (
                      <span
                        className="code-tree-node__proc-tag"
                        title={`宿主进程: ${mod.process.name} (: ${mod.process.listen_port})`}
                      >
                        :{mod.process.listen_port}
                      </span>
                    )}
                  </div>

                  {isAdmin && (
                    <div className="code-tree-node__actions" onClick={(e) => e.stopPropagation()}>
                      {onEditModule && (
                        <button
                          type="button"
                          className="code-tree-action-btn"
                          onClick={() => onEditModule(mod)}
                          title="编辑功能模块"
                        >
                          <Edit size={13} />
                        </button>
                      )}
                      {onDeleteModule && (
                        <button
                          type="button"
                          className="code-tree-action-btn code-tree-action-btn--danger"
                          onClick={() => onDeleteModule(mod.id)}
                          title="删除功能模块"
                        >
                          <Trash2 size={13} />
                        </button>
                      )}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>
    );
  };

  // 渲染逻辑子系统节点
  const renderSubsystemNode = (sub: Subsystem) => {
    const rawTree = getSubsystemGroupTree(sub.id);
    const displayedTree = filterGroupNodes(rawTree, modulesByGroupId, searchQuery);
    const hasChildren = rawTree.length > 0;
    const isExpanded = expandedSubsystemIds.has(sub.id) || Boolean(searchQuery.trim());
    const isSelected = selectedSubsystemId === sub.id && !selectedGroupId && !selectedModuleId;

    return (
      <div key={sub.id} className="code-tree-node-wrapper">
        <div
          className={`code-tree-node code-tree-node--subsystem ${isSelected ? 'code-tree-node--selected' : ''}`}
          onClick={() => {
            onSelectSubsystem(sub.id);
            if (!isExpanded) toggleSubsystemExpand(sub.id);
          }}
        >
          <div className="code-tree-node__left">
            <button
              type="button"
              className="code-tree-expand-btn"
              onClick={(e) => toggleSubsystemExpand(sub.id, e)}
              title={isExpanded ? '折叠' : '展开'}
            >
              {isExpanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
            </button>
            <Cpu size={16} className="code-tree-icon--subsystem" style={{ color: 'var(--color-primary)' }} />
            <span className="code-tree-node__name" title={`${sub.name} [${sub.code}]`}>
              {sub.name}
              <span className="code-tree-node__code">[{sub.code}]</span>
            </span>
          </div>

          {isAdmin && (
            <div className="code-tree-node__actions" onClick={(e) => e.stopPropagation()}>
              <button
                type="button"
                className="code-tree-action-btn"
                onClick={() => onCreateGroup(null, sub.id)}
                title="新建根功能组"
              >
                <Plus size={14} />
              </button>
              <button
                type="button"
                className="code-tree-action-btn"
                onClick={() => onEditSubsystem(sub)}
                title="修改子系统"
              >
                <Edit size={14} />
              </button>
              <button
                type="button"
                className="code-tree-action-btn code-tree-action-btn--danger"
                onClick={() => onDeleteSubsystem(sub.id)}
                title="删除子系统"
              >
                <Trash2 size={14} />
              </button>
            </div>
          )}
        </div>

        {isExpanded && (
          <div className="code-tree-children">
            {displayedTree.length > 0 ? (
              displayedTree.map((child) => renderGroupNode(child, sub.id, 1))
            ) : hasChildren && searchQuery.trim() ? null : (
              <div className="code-tree-empty-sub" style={{ paddingLeft: '2.5rem' }}>
                暂无功能组
              </div>
            )}
          </div>
        )}
      </div>
    );
  };

  // 渲染物理架构两级树：进程分组 ➔ 业务进程 ➔ 装配承载模块
  const renderProcessGroupNode = (g: { groupName: string; processes: Process[] }) => {
    const isExpanded = expandedProcessGroupNames.has(g.groupName) || Boolean(searchQuery.trim());
    const isSelected = selectedProcessGroupName === g.groupName && !selectedProcessId;

    return (
      <div key={g.groupName} className="code-tree-node-wrapper">
        <div
          className={`code-tree-node code-tree-node--proc-group ${isSelected ? 'code-tree-node--selected' : ''}`}
          onClick={(e) => {
            toggleProcessGroupExpand(g.groupName, e);
            if (onSelectProcessGroup) onSelectProcessGroup(g.groupName);
          }}
        >
          <div className="code-tree-node__left">
            <button
              type="button"
              className="code-tree-expand-btn"
              onClick={(e) => toggleProcessGroupExpand(g.groupName, e)}
              title={isExpanded ? '折叠' : '展开'}
            >
              {isExpanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
            </button>
            <Layers size={16} style={{ color: 'var(--color-primary)' }} />
            <span className="code-tree-node__name" style={{ fontWeight: 600 }} title={g.groupName}>
              {g.groupName}
            </span>
            <span className="code-tree-node__badge">{g.processes.length} 进程</span>
          </div>

          {isAdmin && (
            <div className="code-tree-node__actions" onClick={(e) => e.stopPropagation()}>
              {onCreateProcess && (
                <button
                  type="button"
                  className="code-tree-action-btn"
                  onClick={() => onCreateProcess(g.groupName)}
                  title={`在 [${g.groupName}] 下新建进程`}
                >
                  <Plus size={14} />
                </button>
              )}
              {onEditProcessGroup && g.groupName !== '默认进程组' && (
                <button
                  type="button"
                  className="code-tree-action-btn"
                  onClick={() => onEditProcessGroup(g.groupName)}
                  title="重命名进程组"
                >
                  <Edit size={14} />
                </button>
              )}
              {onDeleteProcessGroup && g.groupName !== '默认进程组' && (
                <button
                  type="button"
                  className="code-tree-action-btn code-tree-action-btn--danger"
                  onClick={() => onDeleteProcessGroup(g.groupName)}
                  title="删除进程组"
                >
                  <Trash2 size={14} />
                </button>
              )}
            </div>
          )}
        </div>

        {/* 组内进程列表（二级目录，仅展示业务进程节点） */}
        {isExpanded && (
          <div className="code-tree-children">
            {g.processes.map((proc) => {
              const isSelected = selectedProcessId === proc.id;
              const boundMods = proc.process_modules ?? [];

              return (
                <div key={proc.id} className="code-tree-node-wrapper">
                  <div
                    className={`code-tree-node code-tree-node--process ${isSelected ? 'code-tree-node--selected' : ''}`}
                    style={{ paddingLeft: '28px' }}
                    onClick={() => {
                      if (onSelectProcess) onSelectProcess(proc.id);
                    }}
                  >
                    <div className="code-tree-node__left">
                      <Server size={15} style={{ color: 'var(--color-info, #0284c7)', flexShrink: 0 }} />
                      <span className="code-tree-node__name" title={`${proc.name} (Port: ${proc.listen_port})`}>
                        {proc.name}
                        <span className="code-tree-node__code">: {proc.listen_port}</span>
                      </span>
                      {boundMods.length > 0 && (
                        <span className="code-tree-node__badge" title={`承载 ${boundMods.length} 个功能模块`}>
                          {boundMods.length} FM
                        </span>
                      )}
                    </div>

                    {isAdmin && (
                      <div className="code-tree-node__actions" onClick={(e) => e.stopPropagation()}>
                        {onEditProcess && (
                          <button
                            type="button"
                            className="code-tree-action-btn"
                            onClick={() => onEditProcess(proc)}
                            title="修改进程"
                          >
                            <Edit size={14} />
                          </button>
                        )}
                        {onDeleteProcess && (
                          <button
                            type="button"
                            className="code-tree-action-btn code-tree-action-btn--danger"
                            onClick={() => onDeleteProcess(proc.id)}
                            title="删除进程"
                          >
                            <Trash2 size={14} />
                          </button>
                        )}
                      </div>
                    )}
                  </div>
                </div>
              );
            })}
            {g.processes.length === 0 && (
              <div className="code-tree-empty-sub" style={{ paddingLeft: '2.5rem' }}>
                组内暂无进程
                {isAdmin && onCreateProcess && (
                  <button
                    type="button"
                    className="code-btn code-btn--text code-btn--xs"
                    style={{ marginLeft: '0.5rem', color: 'var(--color-primary)' }}
                    onClick={() => onCreateProcess(g.groupName)}
                  >
                    + 添加进程
                  </button>
                )}
              </div>
            )}
          </div>
        )}
      </div>
    );
  };

  return (
    <div className="code-tree-panel">
      {/* 视角双模切换器 (方案 2: 逻辑架构树 vs 物理进程树) */}
      <div className="code-tree-view-tabs">
        <button
          type="button"
          className={`code-tree-view-tab ${viewMode === 'LOGICAL' ? 'code-tree-view-tab--active' : ''}`}
          onClick={() => handleSwitchViewMode('LOGICAL')}
        >
          逻辑业务树
        </button>
        <button
          type="button"
          className={`code-tree-view-tab ${viewMode === 'PHYSICAL' ? 'code-tree-view-tab--active' : ''}`}
          onClick={() => handleSwitchViewMode('PHYSICAL')}
        >
          物理架构树 (进程)
        </button>
      </div>

      {/* 顶部标题与操作栏 */}
      <div className="code-tree-panel__header">
        <h3 className="code-tree-panel__title">
          {viewMode === 'LOGICAL' ? '架构元素树' : '物理进程架构'}
        </h3>
        {isAdmin && viewMode === 'LOGICAL' && (
          <button
            type="button"
            className="code-btn code-btn--primary code-btn--sm"
            onClick={onCreateSubsystem}
            title="+ 子系统"
          >
            <Plus size={14} /> 子系统
          </button>
        )}
        {isAdmin && viewMode === 'PHYSICAL' && (
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.35rem' }}>
            {onCreateProcessGroup && (
              <button
                type="button"
                className="code-btn code-btn--secondary code-btn--sm"
                onClick={onCreateProcessGroup}
                title="新建进程分组"
              >
                <Plus size={14} /> 进程组
              </button>
            )}
            {onCreateProcess && (
              <button
                type="button"
                className="code-btn code-btn--primary code-btn--sm"
                onClick={() => onCreateProcess()}
                title="新建业务进程"
              >
                <Plus size={14} /> 进程
              </button>
            )}
          </div>
        )}
      </div>

      {/* 搜索框 */}
      <div className="code-tree-search-wrapper">
        <Search size={14} className="code-tree-search-icon" />
        <input
          type="text"
          className="code-tree-search-input"
          placeholder={viewMode === 'LOGICAL' ? '搜索子系统、功能组、功能模块...' : '搜索进程组、进程名、端口、承载模块...'}
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
        />
      </div>

      {/* 树渲染区域 */}
      <div className="code-tree-list-container">
        {viewMode === 'LOGICAL' ? (
          filteredSubsystems.length > 0 ? (
            filteredSubsystems.map((sub) => renderSubsystemNode(sub))
          ) : subsystems.length === 0 ? (
            <div className="code-tree-empty">暂无架构数据</div>
          ) : (
            <div className="code-tree-empty">无匹配的架构元素</div>
          )
        ) : processGroupList.length > 0 ? (
          processGroupList.map((g) => renderProcessGroupNode(g))
        ) : processes.length === 0 ? (
          <div className="code-tree-empty">暂无业务进程数据，点击上方「+ 进程」新建</div>
        ) : (
          <div className="code-tree-empty">无匹配的物理进程</div>
        )}
      </div>
    </div>
  );
};
