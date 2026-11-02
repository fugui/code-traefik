import React, { useState, useMemo } from 'react';
import {
  ChevronRight,
  ChevronDown,
  Server,
  Layers,
  Plus,
  Edit2,
  Trash2,
  Search,
  Radio,
} from 'lucide-react';
import type { ArchProcessGroup } from '../../types';

export interface ArchProcessTreePanelProps {
  groups: ArchProcessGroup[];
  selectedProcessId: string;
  onSelectProcess: (processId: string) => void;
  isAdmin: boolean;
  onCreateGroup: () => void;
  onEditGroup: (group: ArchProcessGroup) => void;
  onDeleteGroup: (groupId: string | number) => void;
  onCreateProcess: (groupId?: string | number) => void;
}

export const ArchProcessTreePanel: React.FC<ArchProcessTreePanelProps> = ({
  groups,
  selectedProcessId,
  onSelectProcess,
  isAdmin,
  onCreateGroup,
  onEditGroup,
  onDeleteGroup,
  onCreateProcess,
}) => {
  const [search, setSearch] = useState('');
  const [collapsedGroups, setCollapsedGroups] = useState<Record<string, boolean>>({});

  const toggleGroup = (groupId: string | number) => {
    const key = String(groupId);
    setCollapsedGroups((prev) => ({ ...prev, [key]: !prev[key] }));
  };

  // 搜索过滤与自动展开
  const filteredGroups = useMemo(() => {
    const s = search.trim().toLowerCase();
    if (!s) return groups;

    return groups
      .map((g) => {
        const groupMatch =
          g.name.toLowerCase().includes(s) || g.code.toLowerCase().includes(s);
        const matchingProcesses = (g.processes || []).filter(
          (p) =>
            p.name.toLowerCase().includes(s) ||
            p.code.toLowerCase().includes(s) ||
            String(p.default_listen_port).includes(s)
        );

        if (groupMatch) {
          return g;
        }
        if (matchingProcesses.length > 0) {
          return {
            ...g,
            processes: matchingProcesses,
          };
        }
        return null;
      })
      .filter((g): g is ArchProcessGroup => g !== null);
  }, [groups, search]);

  return (
    <div
      className="code-arch-tree-panel"
      data-testid="arch-process-tree-panel"
      style={{
        width: '350px',
        flexShrink: 0,
        display: 'flex',
        flexDirection: 'column',
        background: 'var(--color-bg-surface)',
        borderRight: '1px solid var(--color-border-primary)',
        height: '100%',
      }}
    >
      {/* 顶部工具栏与搜索 */}
      <div
        style={{
          padding: '0.75rem 1rem',
          borderBottom: '1px solid var(--color-border-primary)',
          display: 'flex',
          flexDirection: 'column',
          gap: '0.5rem',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', fontWeight: 600 }}>
            <Server size={16} style={{ color: 'var(--color-primary)' }} />
            <span>物理进程蓝图</span>
          </div>
          {isAdmin && (
            <div style={{ display: 'flex', gap: '0.25rem' }}>
              <button
                type="button"
                className="code-btn code-btn--secondary code-btn--xs"
                onClick={onCreateGroup}
                title="新建物理进程组"
                data-testid="create-process-group-btn"
              >
                <Plus size={12} />
                <span>分组</span>
              </button>
              <button
                type="button"
                className="code-btn code-btn--primary code-btn--xs"
                onClick={() => onCreateProcess()}
                title="新建标准架构进程"
                data-testid="create-arch-process-btn"
              >
                <Plus size={12} />
                <span>进程</span>
              </button>
            </div>
          )}
        </div>

        <div style={{ position: 'relative' }}>
          <Search
            size={13}
            style={{
              position: 'absolute',
              left: '0.6rem',
              top: '50%',
              transform: 'translateY(-50%)',
              color: 'var(--color-text-muted)',
            }}
          />
          <input
            type="text"
            className="code-input"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="过滤进程组、进程名、端口..."
            style={{ paddingLeft: '1.8rem', fontSize: '0.8rem', height: '30px' }}
          />
        </div>
      </div>

      {/* 进程树列表 */}
      <div style={{ flex: 1, overflowY: 'auto', padding: '0.5rem' }}>
        {filteredGroups.length === 0 ? (
          <div
            style={{
              padding: '2rem 1rem',
              textAlign: 'center',
              color: 'var(--color-text-muted)',
              fontSize: '0.85rem',
            }}
          >
            {search ? '无匹配的标准架构进程' : '暂无物理进程组，请点击右上角新建'}
          </div>
        ) : (
          filteredGroups.map((g) => {
            const isCollapsed = Boolean(collapsedGroups[String(g.id)]) && !search;
            const procs = g.processes || [];

            return (
              <div
                key={g.id}
                style={{
                  marginBottom: '0.4rem',
                  borderRadius: 'var(--radius-md)',
                  background: 'var(--color-bg-base)',
                  border: '1px solid var(--color-border-secondary)',
                  overflow: 'hidden',
                }}
                data-testid={`process-group-node-${g.id}`}
              >
                {/* 组头部 */}
                <div
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    padding: '0.4rem 0.6rem',
                    cursor: 'pointer',
                    userSelect: 'none',
                    background: 'var(--color-bg-muted)',
                  }}
                  onClick={() => toggleGroup(g.id)}
                >
                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.35rem', overflow: 'hidden' }}>
                    <span style={{ color: 'var(--color-text-muted)' }}>
                      {isCollapsed ? <ChevronRight size={13} /> : <ChevronDown size={13} />}
                    </span>
                    <Layers size={13} style={{ color: 'var(--color-primary)', flexShrink: 0 }} />
                    <span
                      style={{
                        fontWeight: 600,
                        fontSize: '0.825rem',
                        color: 'var(--color-text-primary)',
                        whiteSpace: 'nowrap',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                      }}
                      title={`${g.name} (${g.code})`}
                    >
                      {g.name}
                    </span>
                    <span
                      className="code-badge code-badge--secondary"
                      style={{ fontSize: '0.7rem', padding: '1px 5px' }}
                    >
                      {procs.length}
                    </span>
                  </div>

                  {isAdmin && (
                    <div
                      style={{ display: 'flex', gap: '0.2rem' }}
                      onClick={(e) => e.stopPropagation()}
                    >
                      <button
                        type="button"
                        className="code-btn-icon"
                        onClick={() => onCreateProcess(g.id)}
                        title="在此组下新建架构进程"
                        style={{ padding: '2px', color: 'var(--color-text-secondary)' }}
                      >
                        <Plus size={12} />
                      </button>
                      <button
                        type="button"
                        className="code-btn-icon"
                        onClick={() => onEditGroup(g)}
                        title="编辑进程组"
                        style={{ padding: '2px', color: 'var(--color-text-secondary)' }}
                      >
                        <Edit2 size={12} />
                      </button>
                      <button
                        type="button"
                        className="code-btn-icon"
                        onClick={() => onDeleteGroup(g.id)}
                        title="删除进程组"
                        style={{ padding: '2px', color: 'var(--color-danger)' }}
                      >
                        <Trash2 size={12} />
                      </button>
                    </div>
                  )}
                </div>

                {/* 进程子列表 */}
                {!isCollapsed && (
                  <div style={{ padding: '0.25rem 0.35rem' }}>
                    {procs.length === 0 ? (
                      <div
                        style={{
                          padding: '0.5rem',
                          color: 'var(--color-text-muted)',
                          fontSize: '0.75rem',
                          textAlign: 'center',
                        }}
                      >
                        组内暂无进程
                      </div>
                    ) : (
                      procs.map((p) => {
                        const isSelected = String(p.id) === String(selectedProcessId);
                        const isClient = p.default_listen_port === 0;

                        return (
                          <div
                            key={p.id}
                            onClick={() => onSelectProcess(String(p.id))}
                            data-testid={`arch-process-item-${p.id}`}
                            style={{
                              display: 'flex',
                              alignItems: 'center',
                              justifyContent: 'space-between',
                              padding: '0.4rem 0.5rem',
                              borderRadius: 'var(--radius-sm)',
                              marginBottom: '2px',
                              cursor: 'pointer',
                              background: isSelected
                                ? 'var(--color-bg-hover)'
                                : 'transparent',
                              border: isSelected
                                ? '1px solid var(--color-primary)'
                                : '1px solid transparent',
                              transition: 'all 0.15s ease',
                            }}
                          >
                            <div
                              style={{
                                display: 'flex',
                                alignItems: 'center',
                                gap: '0.35rem',
                                minWidth: 0,
                                flex: 1,
                              }}
                            >
                              <Radio
                                size={12}
                                style={{
                                  color: isSelected
                                    ? 'var(--color-primary)'
                                    : 'var(--color-text-muted)',
                                  flexShrink: 0,
                                }}
                              />
                              <div style={{ display: 'flex', flexDirection: 'column', minWidth: 0 }}>
                                <span
                                  style={{
                                    fontSize: '0.8rem',
                                    fontWeight: isSelected ? 600 : 500,
                                    color: isSelected
                                      ? 'var(--color-primary)'
                                      : 'var(--color-text-primary)',
                                    whiteSpace: 'nowrap',
                                    overflow: 'hidden',
                                    textOverflow: 'ellipsis',
                                  }}
                                  title={p.name}
                                >
                                  {p.name}
                                </span>
                                <span
                                  className="code-font-mono"
                                  style={{
                                    fontSize: '0.675rem',
                                    color: 'var(--color-text-muted)',
                                    whiteSpace: 'nowrap',
                                    overflow: 'hidden',
                                    textOverflow: 'ellipsis',
                                  }}
                                  title={p.code}
                                >
                                  {p.code}
                                </span>
                              </div>
                            </div>

                            <div style={{ display: 'flex', alignItems: 'center', gap: '0.3rem', flexShrink: 0 }}>
                              <span
                                className="code-font-mono"
                                style={{
                                  fontSize: '0.7rem',
                                  padding: '1px 5px',
                                  borderRadius: 'var(--radius-sm)',
                                  background: isClient
                                    ? 'var(--color-bg-muted)'
                                    : 'var(--color-bg-surface)',
                                  border: '1px solid var(--color-border-primary)',
                                  color: isClient
                                    ? 'var(--color-text-muted)'
                                    : 'var(--color-primary)',
                                }}
                              >
                                {isClient ? '纯客户端' : `:${p.default_listen_port}`}
                              </span>
                              {p.mount_count !== undefined && p.mount_count > 0 && (
                                <span
                                  style={{
                                    fontSize: '0.675rem',
                                    padding: '1px 4px',
                                    borderRadius: 'var(--radius-sm)',
                                    background: 'rgba(34, 197, 94, 0.12)',
                                    color: 'var(--color-success)',
                                    fontWeight: 500,
                                  }}
                                  title={`在 ${p.mount_count} 个环境中已挂载`}
                                >
                                  {p.mount_count} 挂载
                                </span>
                              )}
                            </div>
                          </div>
                        );
                      })
                    )}
                  </div>
                )}
              </div>
            );
          })
        )}
      </div>
    </div>
  );
};
export default ArchProcessTreePanel;
