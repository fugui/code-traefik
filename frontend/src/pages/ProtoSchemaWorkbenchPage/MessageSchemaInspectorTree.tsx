import React, { useState } from 'react';
import type { ProtoFieldNodeDTO } from '../../types/schema_tree';
import { DiffIgnoreFieldToggle } from './DiffIgnoreFieldToggle';

export interface MessageSchemaInspectorTreeProps {
  fields: ProtoFieldNodeDTO[];
  activeIgnorePaths: string[];
  inheritedIgnorePaths?: string[];
  onToggleIgnore?: (fieldPath: string, willIgnore: boolean) => void;
  isAdmin?: boolean;
}

export function isPathMatched(fieldPath: string, patterns: string[]): boolean {
  for (const pattern of patterns) {
    const p = pattern.trim();
    if (!p) continue;
    if (p === fieldPath) return true;
    if (fieldPath === `${p}[*]` || `${fieldPath}[*]` === p) return true;
    if (fieldPath.startsWith(`${p}.`) || fieldPath.startsWith(`${p}[`)) return true;
    if (p.includes('[*]')) {
      const regexStr = '^' + p.replace(/\[\*\]/g, '(\\[\\d+\\]|\\[\\*\\])') + '($|\\..*|\\[.*)';
      if (new RegExp(regexStr).test(fieldPath)) return true;
    }
  }
  return false;
}

export const MessageFieldTreeNode: React.FC<{
  node: ProtoFieldNodeDTO;
  depth: number;
  activeIgnorePaths: string[];
  inheritedIgnorePaths: string[];
  onToggleIgnore?: (fieldPath: string, willIgnore: boolean) => void;
  isAdmin: boolean;
}> = ({
  node,
  depth,
  activeIgnorePaths,
  inheritedIgnorePaths,
  onToggleIgnore,
  isAdmin,
}) => {
  const [expanded, setExpanded] = useState(true);
  const hasChildren = Boolean(node.children && node.children.length > 0);

  const path = node.fieldPath || node.field_path || node.name;
  const isDirectlyIgnored = isPathMatched(path, activeIgnorePaths);
  const isInheritedIgnored = isPathMatched(path, inheritedIgnorePaths);
  const isIgnored = isDirectlyIgnored || isInheritedIgnored;

  const tagNumber = node.tagNumber ?? node.tag_number ?? 0;
  const kind = (node.kind || 'UNKNOWN').toUpperCase();

  const getKindBadgeClass = (k: string) => {
    switch (k) {
      case 'INT32':
      case 'INT64':
      case 'UINT32':
      case 'UINT64':
      case 'SINT32':
      case 'SINT64':
        return 'code-badge--info';
      case 'STRING':
        return 'code-badge--success';
      case 'BOOL':
        return 'code-badge--primary';
      case 'MESSAGE':
        return 'code-badge--purple';
      case 'ENUM':
        return 'code-badge--warning';
      case 'BYTES':
        return 'code-badge--secondary';
      default:
        return 'code-badge--secondary';
    }
  };

  return (
    <div
      className={`code-tree-node ${isIgnored ? 'code-tree-node--ignored' : ''}`}
      data-testid={`tree-node-${path}`}
    >
      <div
        className="code-tree-node__content"
        style={{ paddingLeft: `${depth * 18 + 8}px` }}
        onClick={() => hasChildren && setExpanded(!expanded)}
      >
        <span className="code-tree-expand-slot">
          {hasChildren ? (
            <button
              type="button"
              className="code-tree-toggle-btn"
              onClick={(e) => {
                e.stopPropagation();
                setExpanded(!expanded);
              }}
              aria-label={expanded ? '折叠' : '展开'}
            >
              <span className="code-tree-icon">{expanded ? '▼' : '▶'}</span>
            </button>
          ) : (
            <span className="code-tree-bullet">•</span>
          )}
        </span>

        <span className="code-font-mono code-tag-badge" title="字段序号 Tag">
          {`#${tagNumber}`}
        </span>

        <span className="code-font-mono code-field-name" title={`标准路径: ${path}`}>
          {node.name}
        </span>

        <span className={`code-badge ${getKindBadgeClass(kind)} code-text-xs`}>
          {kind}
        </span>

        {node.cardinality === 'REPEATED' && (
          <span className="code-badge code-badge--warning code-text-xs">
            {node.isMap ? 'MAP' : 'REPEATED'}
          </span>
        )}

        {node.typeName && (
          <span className="code-font-mono code-text-xs code-color-muted" title={node.typeName}>
            {node.typeName.split('.').pop()}
          </span>
        )}

        <span className="code-font-mono code-text-xs code-field-path-preview" title="点分路径">
          {path}
        </span>

        {isInheritedIgnored && (
          <span className="code-badge code-badge--secondary code-text-xs" title="继承自架构服务级默认忽略">
            服务级继承
          </span>
        )}

        {node.comments && (
          <span className="code-field-comments" title={node.comments}>
            {`// ${node.comments}`}
          </span>
        )}

        {node.isTruncated && (
          <span className="code-badge code-badge--danger code-text-xs" title="递归深度达到 12 层截断">
            深度截断
          </span>
        )}

        <div className="code-tree-node__actions" onClick={(e) => e.stopPropagation()}>
          {onToggleIgnore && (
            <DiffIgnoreFieldToggle
              fieldPath={path}
              isIgnored={isIgnored}
              disabled={!isAdmin || isInheritedIgnored}
              onToggle={(p, willIgnore) => onToggleIgnore(p, willIgnore)}
            />
          )}
        </div>
      </div>

      {hasChildren && expanded && (
        <div className="code-tree-node__children">
          {node.children!.map((child) => (
            <MessageFieldTreeNode
              key={child.fieldPath || child.field_path || child.name}
              node={child}
              depth={depth + 1}
              activeIgnorePaths={activeIgnorePaths}
              inheritedIgnorePaths={inheritedIgnorePaths}
              onToggleIgnore={onToggleIgnore}
              isAdmin={isAdmin}
            />
          ))}
        </div>
      )}
    </div>
  );
};

export const MessageSchemaInspectorTree: React.FC<MessageSchemaInspectorTreeProps> = ({
  fields,
  activeIgnorePaths,
  inheritedIgnorePaths = [],
  onToggleIgnore,
  isAdmin = true,
}) => {
  if (!fields || fields.length === 0) {
    return <div className="code-empty-pane code-p-md">未定义字段（可能为空消息体或未找到定义）</div>;
  }

  return (
    <div className="code-schema-inspector-tree" data-testid="schema-inspector-tree">
      <div className="code-tree-header">
        <span className="code-tree-header__title">
          消息体字段字典树 ({fields.length} 个字段)
        </span>
        <span className="code-tree-header__hint code-text-xs code-color-muted">
          勾选右侧「忽略对比」复选框可自动生成规范点分/数组通配路径并持久化
        </span>
      </div>
      <div className="code-tree-body">
        {fields.map((field) => (
          <MessageFieldTreeNode
            key={field.fieldPath || field.field_path || field.name}
            node={field}
            depth={0}
            activeIgnorePaths={activeIgnorePaths}
            inheritedIgnorePaths={inheritedIgnorePaths}
            onToggleIgnore={onToggleIgnore}
            isAdmin={isAdmin}
          />
        ))}
      </div>
    </div>
  );
};
