import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ChevronRight,
  ChevronDown,
  Plus,
  Edit,
  Trash2,
  Folder,
  FolderOpen,
  Search,
  Layers,
  FileCode,
} from 'lucide-react';
import type { Subsystem, FunctionalGroupTreeNode, Module } from '../../types';

export interface SubsystemGroupTreePanelProps {
  // 面板宽度
  panelWidth?: number;

  // 逻辑架构树相关
  subsystems: Subsystem[];
  selectedSubsystemId: string;
  onSelectSubsystem: (id: string) => void;
  groupTree: FunctionalGroupTreeNode[];
  groupTreeMap?: Record<string, FunctionalGroupTreeNode[]>;
  onLoadSubsystemTree?: (subsystemId: string) => Promise<FunctionalGroupTreeNode[]>;
  selectedGroupId: string;
  onSelectGroup: (groupId: string, subsystemId?: string) => void;

  // 逻辑树功能模块扩展（支持直接展开至 FM）
  modules?: Module[];
  selectedModuleId?: string;
  onSelectModule?: (moduleId: string) => void;
  onSelectModuleLeaf?: (subsystemId: string | number, groupId: string | number, moduleId: string | number) => void;
  onCreateModule?: (groupId: string) => void;
  onEditModule?: (mod: Module) => void;
  onDeleteModule?: (moduleId: string) => void;

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
  if (!text || !query) return false;
  return text.toLowerCase().includes(query.toLowerCase());
}

function checkGroupTreeMatch(
  nodes: FunctionalGroupTreeNode[],
  modulesByGroupId: Map<string, Module[]>,
  query: string,
  toExpand: Set<string>
): boolean {
  let matched = false;
  for (const node of nodes) {
    const nameMatch = isTextMatch(node.name, query) || isTextMatch(node.code, query);
    const nodeMods = modulesByGroupId.get(String(node.id)) || [];
    const modMatch = nodeMods.some((m) => isTextMatch(m.name, query) || isTextMatch(m.code, query));

    let childMatched = false;
    if (node.children && node.children.length > 0) {
      if (checkGroupTreeMatch(node.children, modulesByGroupId, query, toExpand)) {
        childMatched = true;
      }
    }

    if (nameMatch || modMatch || childMatched) {
      toExpand.add(String(node.id));
      matched = true;
    }
  }
  return matched;
}

export const SubsystemGroupTreePanel: React.FC<SubsystemGroupTreePanelProps> = ({
  panelWidth,
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
  onSelectModuleLeaf,
  onCreateModule,
  onEditModule,
  onDeleteModule,
  isAdmin,
  onCreateSubsystem,
  onEditSubsystem,
  onDeleteSubsystem,
  onCreateGroup,
  onEditGroup,
  onMoveGroup,
  onDeleteGroup,
}) => {
  // 展开状态
  const [expandedSubsystemIds, setExpandedSubsystemIds] = useState<Set<string>>(() => new Set());
  const [expandedGroupIds, setExpandedGroupIds] = useState<Set<string>>(() => new Set());

  // 搜索关键字
  const [searchQuery, setSearchQuery] = useState('');

  // 模块按 group_id 分组索引
  const modulesByGroupId = useMemo(() => {
    const map = new Map<string, Module[]>();
    for (const m of modules) {
      const gid = String(m.group_id);
      if (!map.has(gid)) {
        map.set(gid, []);
      }
      map.get(gid)!.push(m);
    }
    return map;
  }, [modules]);

  // 获取子系统的功能组树
  const getSubsystemGroupTree = useCallback(
    (subsystemId: string | number): FunctionalGroupTreeNode[] => {
      const subIdStr = String(subsystemId);
      if (groupTreeMap[subIdStr]) {
        return groupTreeMap[subIdStr];
      }
      if (String(selectedSubsystemId) === subIdStr) {
        return groupTree;
      }
      return [];
    },
    [groupTreeMap, selectedSubsystemId, groupTree]
  );

  // 当外部选中的子系统、功能组或模块变更时，自动展开其父节点链路
  useEffect(() => {
    if (selectedSubsystemId) {
      setExpandedSubsystemIds((prev) => {
        const next = new Set(prev);
        next.add(String(selectedSubsystemId));
        return next;
      });
      if (!groupTreeMap[String(selectedSubsystemId)] && onLoadSubsystemTree) {
        void onLoadSubsystemTree(String(selectedSubsystemId));
      }
    }
    if (selectedGroupId) {
      setExpandedGroupIds((prev) => {
        const next = new Set(prev);
        next.add(String(selectedGroupId));
        return next;
      });
    }
  }, [selectedSubsystemId, selectedGroupId, groupTreeMap, onLoadSubsystemTree]);

  // 响应搜索过滤并自动展开匹配的树链路
  useEffect(() => {
    const q = searchQuery.trim();
    if (!q) return;

    const toExpandSubs = new Set<string>();
    const toExpandGrps = new Set<string>();

    for (const sub of subsystems) {
      const subIdStr = String(sub.id);
      const subTree = getSubsystemGroupTree(sub.id);
      const subMatched = isTextMatch(sub.name, q) || isTextMatch(sub.code, q);
      const groupMatched = checkGroupTreeMatch(subTree, modulesByGroupId, q, toExpandGrps);

      if (subMatched || groupMatched) {
        toExpandSubs.add(subIdStr);
        if (!groupTreeMap[subIdStr] && onLoadSubsystemTree) {
          void onLoadSubsystemTree(subIdStr);
        }
      }
    }

    setExpandedSubsystemIds(toExpandSubs);
    setExpandedGroupIds(toExpandGrps);
  }, [searchQuery, subsystems, getSubsystemGroupTree, modulesByGroupId, groupTreeMap, onLoadSubsystemTree]);

  const toggleSubsystemExpand = (id: string | number, e?: React.MouseEvent) => {
    e?.stopPropagation();
    const idStr = String(id);
    setExpandedSubsystemIds((prev) => {
      const next = new Set(prev);
      if (next.has(idStr)) {
        next.delete(idStr);
      } else {
        next.add(idStr);
        if (!groupTreeMap[idStr] && onLoadSubsystemTree) {
          void onLoadSubsystemTree(idStr);
        }
      }
      return next;
    });
  };

  const toggleGroupExpand = (id: string | number, e?: React.MouseEvent) => {
    e?.stopPropagation();
    const idStr = String(id);
    setExpandedGroupIds((prev) => {
      const next = new Set(prev);
      if (next.has(idStr)) {
        next.delete(idStr);
      } else {
        next.add(idStr);
      }
      return next;
    });
  };

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

  // 渲染模块叶子节点
  const renderModuleLeaf = (mod: Module, subsystemId: string | number, groupId: string | number) => {
    const isSelected = String(selectedModuleId) === String(mod.id);
    return (
      <div
        key={`mod-${mod.id}`}
        className={`code-tree-node code-tree-node--module ${isSelected ? 'code-tree-node--selected' : ''}`}
        title={`${mod.name} (${mod.code})`}
        onClick={(e) => {
          e.stopPropagation();
          if (onSelectModuleLeaf) {
            onSelectModuleLeaf(subsystemId, groupId, mod.id);
          } else if (onSelectModule) {
            onSelectModule(String(mod.id));
          }
        }}
      >
        <div className="code-tree-node__left">
          <span className="code-tree-expand-placeholder" aria-hidden="true" />
          <FileCode size={14} className="code-tree-icon code-tree-icon--module" />
          <span className="code-tree-node__name" title={`${mod.name} (${mod.code})`}>
            {mod.name}
          </span>
          <span className="code-tree-node__code-chip" title={mod.code}>{mod.code}</span>
        </div>

        {isAdmin && (
          <div className="code-tree-node__actions" onClick={(e) => e.stopPropagation()}>
            {onEditModule && (
              <button
                type="button"
                className="code-tree-action-btn"
                onClick={() => onEditModule(mod)}
                title="编辑模块"
              >
                <Edit size={13} />
              </button>
            )}
            {onDeleteModule && (
              <button
                type="button"
                className="code-tree-action-btn code-tree-action-btn--danger"
                onClick={() => onDeleteModule(String(mod.id))}
                title="删除模块"
              >
                <Trash2 size={13} />
              </button>
            )}
          </div>
        )}
      </div>
    );
  };

  // 递归渲染功能组节点
  const renderGroupNode = (
    node: FunctionalGroupTreeNode,
    currentSubsystemId: string | number,
    level = 1
  ): React.ReactNode => {
    const nodeIdStr = String(node.id);
    const hasChildren = node.children && node.children.length > 0;
    const groupMods = modulesByGroupId.get(nodeIdStr) || [];
    const hasModules = groupMods.length > 0;
    const hasExpandableContent = hasChildren || hasModules;

    const isExpanded = expandedGroupIds.has(nodeIdStr) || Boolean(searchQuery.trim());
    const isSelected = String(selectedGroupId) === nodeIdStr;

    return (
      <div key={`grp-${node.id}`} className="code-tree-node-wrapper code-tree-node-wrapper--group">
        <div
          className={`code-tree-node code-tree-node--group ${isSelected ? 'code-tree-node--selected' : ''}`}
          title={`${node.name} (${node.code})`}
          onClick={() => onSelectGroup(nodeIdStr, String(currentSubsystemId))}
        >
          <div className="code-tree-node__left">
            {hasExpandableContent ? (
              <button
                type="button"
                className="code-tree-expand-btn"
                onClick={(e) => toggleGroupExpand(node.id, e)}
                title={isExpanded ? '折叠' : '展开'}
              >
                {isExpanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
              </button>
            ) : (
              <span className="code-tree-expand-placeholder" aria-hidden="true" />
            )}
            {isExpanded ? (
              <FolderOpen size={16} className="code-tree-icon code-tree-icon--group code-tree-icon--open" />
            ) : (
              <Folder size={16} className="code-tree-icon code-tree-icon--group" />
            )}
            <span className="code-tree-node__name" title={`${node.name} (${node.code})`}>
              {node.name}
            </span>
            <span className="code-tree-node__code-chip" title={node.code}>{node.code}</span>
            {hasModules && <span className="code-tree-node__badge">{groupMods.length} 模块</span>}
          </div>

          {isAdmin && (
            <div className="code-tree-node__actions" onClick={(e) => e.stopPropagation()}>
              <button
                type="button"
                className="code-tree-action-btn"
                onClick={() => onCreateGroup(nodeIdStr, String(currentSubsystemId))}
                title="在此功能组下新建子功能组"
              >
                <Plus size={13} />
              </button>
              {onCreateModule && (
                <button
                  type="button"
                  className="code-tree-action-btn"
                  onClick={() => onCreateModule(nodeIdStr)}
                  title="在此组下新建功能模块"
                >
                  <FileCode size={13} />
                </button>
              )}
              <button
                type="button"
                className="code-tree-action-btn"
                onClick={() => onEditGroup(node)}
                title="编辑功能组"
              >
                <Edit size={13} />
              </button>
              <button
                type="button"
                className="code-tree-action-btn"
                onClick={() => onMoveGroup(node)}
                title="调整层级 (变更父节点)"
              >
                <Layers size={13} />
              </button>
              <button
                type="button"
                className="code-tree-action-btn code-tree-action-btn--danger"
                onClick={() => onDeleteGroup(nodeIdStr)}
                title="删除功能组"
              >
                <Trash2 size={13} />
              </button>
            </div>
          )}
        </div>

        {isExpanded && (
          <div className="code-tree-children code-tree-children--group">
            {hasChildren && node.children && node.children.map((child) => renderGroupNode(child, currentSubsystemId, level + 1))}
            {hasModules && (
              <div className="code-tree-modules-container">
                {groupMods.map((mod) => renderModuleLeaf(mod, currentSubsystemId, node.id))}
              </div>
            )}
          </div>
        )}
      </div>
    );
  };

  // 渲染子系统节点
  const renderSubsystemNode = (sub: Subsystem) => {
    const subIdStr = String(sub.id);
    const subTree = getSubsystemGroupTree(sub.id);
    const hasTree = subTree.length > 0;
    const isExpanded = expandedSubsystemIds.has(subIdStr) || Boolean(searchQuery.trim());
    const isSelected = String(selectedSubsystemId) === subIdStr;

    return (
      <div key={`sub-${sub.id}`} className="code-tree-node-wrapper code-tree-node-wrapper--subsystem">
        <div
          className={`code-tree-node code-tree-node--subsystem ${isSelected ? 'code-tree-node--selected' : ''}`}
          title={`${sub.name} (${sub.code})`}
          onClick={() => {
            onSelectSubsystem(subIdStr);
            if (!expandedSubsystemIds.has(subIdStr)) {
              toggleSubsystemExpand(sub.id);
            }
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
            <Layers size={16} className="code-tree-icon code-tree-icon--subsystem" />
            <span className="code-tree-node__name" title={`${sub.name} (${sub.code})`}>
              {sub.name}
            </span>
            <span className="code-tree-node__code-chip" title={sub.code}>{sub.code}</span>
          </div>

          {isAdmin && (
            <div className="code-tree-node__actions" onClick={(e) => e.stopPropagation()}>
              <button
                type="button"
                className="code-tree-action-btn"
                onClick={() => onCreateGroup(null, subIdStr)}
                title="在当前子系统下新建根功能组"
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
                onClick={() => onDeleteSubsystem(subIdStr)}
                title="删除子系统"
              >
                <Trash2 size={14} />
              </button>
            </div>
          )}
        </div>

        {isExpanded && (
          <div className="code-tree-children code-tree-children--subsystem">
            {hasTree ? (
              subTree.map((node) => renderGroupNode(node, sub.id, 1))
            ) : (
              <div className="code-tree-empty-sub">
                暂无功能组
              </div>
            )}
          </div>
        )}
      </div>
    );
  };

  return (
    <div
      className="code-tree-panel"
      style={panelWidth ? { width: `${panelWidth}px` } : undefined}
    >
      {/* 顶部标题与操作栏 */}
      <div className="code-tree-panel__header">
        <h3 className="code-tree-panel__title">逻辑架构元素树</h3>
        {isAdmin && (
          <button
            type="button"
            className="code-btn code-btn--primary code-btn--sm"
            onClick={onCreateSubsystem}
            title="+ 子系统"
          >
            <Plus size={14} /> 子系统
          </button>
        )}
      </div>

      {/* 搜索框 */}
      <div className="code-tree-search-wrapper">
        <Search size={14} className="code-tree-search-icon" />
        <input
          type="text"
          className="code-tree-search-input"
          placeholder="搜索子系统、功能组、功能模块..."
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
        />
      </div>

      {/* 树渲染区域 */}
      <div className="code-tree-list-container">
        {filteredSubsystems.length > 0 ? (
          filteredSubsystems.map((sub) => renderSubsystemNode(sub))
        ) : subsystems.length === 0 ? (
          <div className="code-tree-empty">暂无架构数据</div>
        ) : (
          <div className="code-tree-empty">无匹配的架构元素</div>
        )}
      </div>
    </div>
  );
};
