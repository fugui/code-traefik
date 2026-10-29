import React, { useState } from 'react';
import type { Subsystem, FunctionalGroupTreeNode } from '../../types';

export interface SubsystemGroupTreePanelProps {
  subsystems: Subsystem[];
  selectedSubsystemId: string;
  onSelectSubsystem: (id: string) => void;
  groupTree: FunctionalGroupTreeNode[];
  selectedGroupId: string;
  onSelectGroup: (id: string) => void;
  isAdmin: boolean;
  onCreateSubsystem: () => void;
  onEditSubsystem: (sub: Subsystem) => void;
  onDeleteSubsystem: (id: string) => void;
  onCreateGroup: (parentId?: string | null) => void;
  onEditGroup: (group: FunctionalGroupTreeNode) => void;
  onMoveGroup: (group: FunctionalGroupTreeNode) => void;
  onDeleteGroup: (id: string) => void;
}

export const SubsystemGroupTreePanel: React.FC<SubsystemGroupTreePanelProps> = ({
  subsystems,
  selectedSubsystemId,
  onSelectSubsystem,
  groupTree,
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
  const [expandedNodeIds, setExpandedNodeIds] = useState<Set<string>>(new Set());

  const toggleExpand = (id: string, e: React.MouseEvent) => {
    e.stopPropagation();
    setExpandedNodeIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const renderTreeNode = (node: FunctionalGroupTreeNode) => {
    const isExpanded = expandedNodeIds.has(node.id) || true; // 默认展开
    const isSelected = selectedGroupId === node.id;
    const hasChildren = Boolean(node.children && node.children.length > 0);

    return (
      <div key={node.id} className="code-tree-node-wrapper">
        <div
          className={`code-tree-node ${isSelected ? 'code-tree-node--selected' : ''}`}
          onClick={() => onSelectGroup(node.id)}
          style={{ paddingLeft: `${node.level * 16}px` }}
        >
          <button
            type="button"
            className="code-tree-expand-btn"
            onClick={(e) => toggleExpand(node.id, e)}
            disabled={!hasChildren}
          >
            {hasChildren ? (isExpanded ? '▾' : '▸') : '•'}
          </button>
          <span className="code-tree-node__name" title={node.code}>
            {node.name}
          </span>
          <span className="code-tree-node__badge">{node.module_count}</span>

          {isAdmin && (
            <div className="code-tree-node__actions" onClick={(e) => e.stopPropagation()}>
              <button
                type="button"
                className="code-tree-action-btn"
                onClick={() => onCreateGroup(node.id)}
                title="新增子功能组"
              >
                +
              </button>
              <button
                type="button"
                className="code-tree-action-btn"
                onClick={() => onEditGroup(node)}
                title="编辑功能组"
              >
                ✎
              </button>
              <button
                type="button"
                className="code-tree-action-btn"
                onClick={() => onMoveGroup(node)}
                title="移动功能组 (变更父级)"
              >
                ⇄
              </button>
              <button
                type="button"
                className="code-tree-action-btn code-tree-action-btn--danger"
                onClick={() => onDeleteGroup(node.id)}
                title="删除功能组"
              >
                ✕
              </button>
            </div>
          )}
        </div>

        {hasChildren && isExpanded && (
          <div className="code-tree-children">
            {node.children?.map((child) => renderTreeNode(child))}
          </div>
        )}
      </div>
    );
  };

  return (
    <div className="code-tree-panel">
      {/* 子系统选择区 */}
      <div className="code-tree-panel__header">
        <div className="code-tree-panel__title-bar">
          <span className="code-tree-panel__title">子系统列表</span>
          {isAdmin && (
            <button
              type="button"
              className="code-btn code-btn--primary code-btn--xs"
              onClick={onCreateSubsystem}
            >
              + 子系统
            </button>
          )}
        </div>
        <select
          className="code-select"
          value={selectedSubsystemId}
          onChange={(e) => onSelectSubsystem(e.target.value)}
        >
          {subsystems.map((sub) => (
            <option key={sub.id} value={sub.id}>
              {sub.name} ({sub.code})
            </option>
          ))}
        </select>
        {isAdmin && selectedSubsystemId && (
          <div className="code-subsystem-quick-actions">
            <button
              type="button"
              className="code-btn-text"
              onClick={() => {
                const cur = subsystems.find((s) => s.id === selectedSubsystemId);
                if (cur) onEditSubsystem(cur);
              }}
            >
              修改子系统
            </button>
            <button
              type="button"
              className="code-btn-text code-color-danger"
              onClick={() => onDeleteSubsystem(selectedSubsystemId)}
            >
              删除子系统
            </button>
          </div>
        )}
      </div>

      {/* 功能组树 */}
      <div className="code-tree-panel__body">
        <div className="code-tree-panel__section-title">
          <span>8 级功能组架构树</span>
          {isAdmin && (
            <button
              type="button"
              className="code-btn code-btn--secondary code-btn--xs"
              onClick={() => onCreateGroup(null)}
              title="在根层级添加功能组"
            >
              + 根功能组
            </button>
          )}
        </div>
        <div className="code-tree-list">
          {groupTree.length > 0 ? (
            groupTree.map((node) => renderTreeNode(node))
          ) : (
            <div className="code-empty-pane code-p-sm">暂无功能组</div>
          )}
        </div>
      </div>
    </div>
  );
};
