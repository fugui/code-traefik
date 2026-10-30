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
} from 'lucide-react';
import type { Subsystem, FunctionalGroupTreeNode } from '../../types';

export interface SubsystemGroupTreePanelProps {
  subsystems: Subsystem[];
  selectedSubsystemId: string;
  onSelectSubsystem: (id: string) => void;
  groupTree: FunctionalGroupTreeNode[];
  groupTreeMap?: Record<string, FunctionalGroupTreeNode[]>;
  onLoadSubsystemTree?: (subsystemId: string) => Promise<FunctionalGroupTreeNode[]>;
  selectedGroupId: string;
  onSelectGroup: (id: string) => void;
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

function checkGroupTreeMatch(
  nodes: FunctionalGroupTreeNode[],
  query: string,
  toExpand: Set<string>
): boolean {
  let hasMatch = false;
  for (const node of nodes) {
    const nodeMatched = isTextMatch(node.name, query) || isTextMatch(node.code, query);
    let childMatched = false;
    if (node.children && node.children.length > 0) {
      childMatched = checkGroupTreeMatch(node.children, query, toExpand);
    }
    if (nodeMatched || childMatched) {
      toExpand.add(node.id);
      hasMatch = true;
    }
  }
  return hasMatch;
}

function filterGroupNodes(
  nodes: FunctionalGroupTreeNode[],
  query: string
): FunctionalGroupTreeNode[] {
  if (!query.trim()) return nodes;

  const result: FunctionalGroupTreeNode[] = [];
  for (const node of nodes) {
    const selfMatch = isTextMatch(node.name, query) || isTextMatch(node.code, query);
    const filteredChildren = node.children ? filterGroupNodes(node.children, query) : [];
    if (selfMatch || filteredChildren.length > 0) {
      result.push({
        ...node,
        children: filteredChildren,
      });
    }
  }
  return result;
}

export const SubsystemGroupTreePanel: React.FC<SubsystemGroupTreePanelProps> = ({
  subsystems,
  selectedSubsystemId,
  onSelectSubsystem,
  groupTree,
  groupTreeMap = {},
  onLoadSubsystemTree,
  selectedGroupId,
  onSelectGroup,
  isAdmin,
  onCreateSubsystem,
  onEditSubsystem,
  onDeleteSubsystem,
  onCreateGroup,
  onEditGroup,
  onMoveGroup,
  onDeleteGroup,
}) => {
  const [expandedSubsystemIds, setExpandedSubsystemIds] = useState<Set<string>>(
    () => new Set(selectedSubsystemId ? [selectedSubsystemId] : [])
  );
  const [expandedGroupIds, setExpandedGroupIds] = useState<Set<string>>(new Set());
  const [searchQuery, setSearchQuery] = useState('');

  // 默认展开当前选中的子系统
  useEffect(() => {
    if (selectedSubsystemId) {
      setExpandedSubsystemIds((prev) => {
        const next = new Set(prev);
        next.add(selectedSubsystemId);
        return next;
      });
    }
  }, [selectedSubsystemId]);

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

  // 搜索时自动展开匹配路径
  useEffect(() => {
    if (!searchQuery.trim()) return;

    const toExpandSub = new Set<string>();
    const toExpandGrp = new Set<string>();

    for (const sub of subsystems) {
      const subTree = getSubsystemGroupTree(sub.id);
      const subMatched = isTextMatch(sub.name, searchQuery) || isTextMatch(sub.code, searchQuery);
      const grpMatched = checkGroupTreeMatch(subTree, searchQuery, toExpandGrp);

      if (subMatched || grpMatched) {
        toExpandSub.add(sub.id);
      }
    }

    setExpandedSubsystemIds((prev) => new Set([...prev, ...toExpandSub]));
    setExpandedGroupIds((prev) => new Set([...prev, ...toExpandGrp]));
  }, [searchQuery, subsystems, getSubsystemGroupTree]);

  // 过滤后的子系统列表
  const filteredSubsystems = useMemo(() => {
    if (!searchQuery.trim()) return subsystems;

    return subsystems.filter((sub) => {
      const subMatched = isTextMatch(sub.name, searchQuery) || isTextMatch(sub.code, searchQuery);
      if (subMatched) return true;
      const subTree = getSubsystemGroupTree(sub.id);
      const toExpandDummy = new Set<string>();
      return checkGroupTreeMatch(subTree, searchQuery, toExpandDummy);
    });
  }, [subsystems, searchQuery, getSubsystemGroupTree]);

  // 渲染功能组节点
  const renderGroupNode = (node: FunctionalGroupTreeNode, subsystemId: string, depth = 1) => {
    const hasChildren = Boolean(node.children && node.children.length > 0);
    // 默认或搜索时自动展开
    const isExpanded = expandedGroupIds.has(node.id) || Boolean(searchQuery.trim());
    const isSelected = selectedGroupId === node.id;

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
              style={{ visibility: hasChildren ? 'visible' : 'hidden' }}
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
            {typeof node.module_count === 'number' && (
              <span className="code-tree-node__badge" title={`包含 ${node.module_count} 个功能模块`}>
                {node.module_count}
              </span>
            )}
          </div>

          {isAdmin && (
            <div className="code-tree-node__actions" onClick={(e) => e.stopPropagation()}>
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

        {hasChildren && isExpanded && (
          <div className="code-tree-children">
            {(node.children ?? []).map((child) => renderGroupNode(child, subsystemId, depth + 1))}
          </div>
        )}
      </div>
    );
  };

  // 渲染子系统节点
  const renderSubsystemNode = (sub: Subsystem) => {
    const rawTree = getSubsystemGroupTree(sub.id);
    const displayedTree = filterGroupNodes(rawTree, searchQuery);
    const hasChildren = rawTree.length > 0;
    const isExpanded = expandedSubsystemIds.has(sub.id) || Boolean(searchQuery.trim());
    const isSelected = selectedSubsystemId === sub.id && !selectedGroupId;

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

  return (
    <div className="code-tree-panel">
      {/* 顶部标题与操作栏 */}
      <div className="code-tree-panel__header">
        <h3 className="code-tree-panel__title">架构元素树</h3>
        {isAdmin && (
          <button
            type="button"
            className="code-btn code-btn--primary code-btn--sm"
            onClick={onCreateSubsystem}
            title="新建子系统"
          >
            <Plus size={14} /> + 子系统
          </button>
        )}
      </div>

      {/* 搜索框 */}
      <div className="code-tree-search-wrapper">
        <Search size={14} className="code-tree-search-icon" />
        <input
          type="text"
          className="code-tree-search-input"
          placeholder="搜索架构元素..."
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
