import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import { Bug, Network, RotateCcw, Share2, Trash2, type LucideIcon } from 'lucide-react';
import { Modal, Pagination } from '@code/common';
import { useModulePath } from '../../hooks/useModulePath';
import type { RecordingSession } from '../../types';

const debugPath = (sessionId: string) => `/debug?session_id=${encodeURIComponent(sessionId)}`;

interface SessionNavAction {
  key: string;
  label: string;
  Icon: LucideIcon;
  buildPath: (sessionId: string) => string;
}

/** 快捷工作流跳转动作（图标 + Tips），路径为模块内相对路径，渲染时经 useModulePath 统一解析 */
const navActions: SessionNavAction[] = [
  { key: 'debug', label: '进入流量调试', Icon: Bug, buildPath: debugPath },
  {
    key: 'replay',
    label: '发起流量重放',
    Icon: RotateCcw,
    buildPath: (id) => `/replay?session_id=${encodeURIComponent(id)}&auto_create=1`,
  },
  {
    key: 'topology',
    label: '架构拓扑验证',
    Icon: Network,
    buildPath: (id) => `/topology?session_id=${encodeURIComponent(id)}`,
  },
];

export interface RecordingSessionTableProps {
  sessions: RecordingSession[];
  total: number;
  page: number;
  pageSize: number;
  onPageChange: (page: number) => void;
  onPageSizeChange: (pageSize: number) => void;
  scopeFilter: string;
  onScopeFilterChange: (scope: string) => void;
  search: string;
  onSearchChange: (search: string) => void;
  currentUserId?: number;
  isAdmin: boolean;
  onPublish: (sessionId: string) => Promise<void>;
  onDelete: (sessionId: string) => Promise<void>;
  loading?: boolean;
}

export const RecordingSessionTable: React.FC<RecordingSessionTableProps> = ({
  sessions,
  total,
  page,
  pageSize,
  onPageChange,
  onPageSizeChange,
  scopeFilter,
  onScopeFilterChange,
  search,
  onSearchChange,
  currentUserId,
  isAdmin,
  onPublish,
  onDelete,
  loading = false,
}) => {
  const [deleteModalSession, setDeleteModalSession] = useState<RecordingSession | null>(null);
  const [deleting, setDeleting] = useState(false);
  const modulePath = useModulePath();

  const confirmDelete = async () => {
    if (!deleteModalSession) return;
    setDeleting(true);
    try {
      await onDelete(deleteModalSession.id);
      setDeleteModalSession(null);
    } finally {
      setDeleting(false);
    }
  };

  return (
    <div className="code-recording-ledger" data-testid="recording-session-table">
      {/* 顶部过滤工具条 */}
      <div className="code-pane-header">
        <div className="code-scope-tabs">
          <button
            type="button"
            className={`code-scope-tab ${scopeFilter === '' ? 'code-scope-tab--active' : ''}`}
            onClick={() => onScopeFilterChange('')}
          >
            全部素材
          </button>
          <button
            type="button"
            className={`code-scope-tab ${scopeFilter === 'SHARED' ? 'code-scope-tab--active' : ''}`}
            onClick={() => onScopeFilterChange('SHARED')}
          >
            团队共享 (SHARED)
          </button>
          <button
            type="button"
            className={`code-scope-tab ${scopeFilter === 'PRIVATE' ? 'code-scope-tab--active' : ''}`}
            onClick={() => onScopeFilterChange('PRIVATE')}
          >
            个人调试 (PRIVATE)
          </button>
        </div>

        <div className="code-filter-search">
          <input
            type="text"
            className="code-input code-input--sm"
            placeholder="搜索批次名称或流水号..."
            value={search}
            onChange={(e) => onSearchChange(e.target.value)}
          />
        </div>
      </div>

      {/* 台账数据表格 */}
      <div className="code-table-wrapper code-mt-sm">
        {loading ? (
          <div className="code-tpl-loading-screen">正在加载录制素材台账...</div>
        ) : (
          <table className="code-table">
            <thead>
              <tr>
                <th>批次流水号</th>
                <th>批次名称</th>
                <th>作用域</th>
                <th>状态</th>
                <th>捕获帧数</th>
                <th>归属人</th>
                <th>创建时间</th>
                <th>快捷工作流操作</th>
              </tr>
            </thead>
            <tbody>
              {sessions.length === 0 ? (
                <tr>
                  <td colSpan={8} className="code-text-center code-text-muted code-p-md">
                    暂无符合条件的录制素材
                  </td>
                </tr>
              ) : (
                sessions.map((sess) => {
                  const isOwner = Boolean(
                    currentUserId && sess.owner_id && String(currentUserId) === String(sess.owner_id)
                  );
                  const canPublish =
                    sess.scope === 'PRIVATE' &&
                    (isOwner || isAdmin) &&
                    (sess.status === 'COMPLETED' || sess.status === 'COMPLETED_BY_LIMIT');
                  const canDelete = isAdmin || (sess.scope === 'PRIVATE' && isOwner);

                  return (
                    <tr key={sess.id} data-testid={`session-row-${sess.id}`}>
                      <td>
                        <Link
                          to={modulePath(debugPath(sess.id))}
                          className="code-session-no-link"
                          title="进入该批次录制数据流"
                          data-testid={`session-no-link-${sess.id}`}
                        >
                          {sess.session_no}
                        </Link>
                      </td>
                      <td>
                        <strong>{sess.name}</strong>
                        {sess.description && (
                          <div className="code-text-xs code-text-muted">{sess.description}</div>
                        )}
                      </td>
                      <td>
                        <span
                          className={`code-badge ${
                            sess.scope === 'SHARED' ? 'code-badge--primary' : 'code-badge--secondary'
                          }`}
                        >
                          {sess.scope}
                        </span>
                      </td>
                      <td>
                        <span
                          className={`code-status-tag code-status-tag--${sess.status.toLowerCase()}`}
                        >
                          {sess.status}
                        </span>
                      </td>
                      <td className="code-font-mono">{sess.captured_frames} 帧</td>
                      <td>{sess.owner_name || `用户#${sess.owner_id ?? '-'}`}</td>
                      <td className="code-text-xs code-font-mono">
                        {sess.created_at ? new Date(sess.created_at).toLocaleString() : '-'}
                      </td>
                      <td>
                        <div className="code-icon-actions">
                          {navActions.map(({ key, label, Icon, buildPath }) => (
                            <Link
                              key={key}
                              to={modulePath(buildPath(sess.id))}
                              className="code-icon-action"
                              data-tip={label}
                              aria-label={label}
                              data-testid={`action-${key}-${sess.id}`}
                            >
                              <Icon size={15} />
                            </Link>
                          ))}
                          {canPublish && (
                            <button
                              type="button"
                              className="code-icon-action code-icon-action--warning"
                              onClick={() => void onPublish(sess.id)}
                              data-tip="发布为团队共享素材"
                              aria-label="发布为团队共享素材"
                              data-testid={`action-publish-${sess.id}`}
                            >
                              <Share2 size={15} />
                            </button>
                          )}
                          {canDelete && (
                            <button
                              type="button"
                              className="code-icon-action code-icon-action--danger"
                              onClick={() => setDeleteModalSession(sess)}
                              data-tip="删除此录制批次"
                              aria-label="删除此录制批次"
                              data-testid={`action-delete-${sess.id}`}
                            >
                              <Trash2 size={15} />
                            </button>
                          )}
                        </div>
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        )}
      </div>

      {/* 分页栏 */}
      <div className="code-mt-md">
        <Pagination
          totalItems={total}
          page={page}
          pageSize={pageSize}
          onPageChange={onPageChange}
          onPageSizeChange={onPageSizeChange}
        />
      </div>

      {/* 自定义删除确认模态框（防误触，零原生 alert/confirm） */}
      {deleteModalSession && (
        <Modal
          open={Boolean(deleteModalSession)}
          onClose={() => setDeleteModalSession(null)}
          title="删除录制素材批次确认"
          width="sm"
          footer={
            <div className="code-modal-footer">
              <button
                type="button"
                className="code-btn code-btn--secondary"
                onClick={() => setDeleteModalSession(null)}
                disabled={deleting}
              >
                取消
              </button>
              <button
                type="button"
                className="code-btn code-btn--danger"
                disabled={deleting}
                onClick={() => void confirmDelete()}
                data-testid="btn-confirm-delete-session"
              >
                {deleting ? '删除中...' : '确认彻底删除'}
              </button>
            </div>
          }
        >
          <div className="code-p-md" data-testid="delete-confirm-dialog">
            <p>
              确定要删除录制批次 <strong>{deleteModalSession.name}</strong> (
              <code>{deleteModalSession.session_no}</code>) 吗？
            </p>
            <p className="code-color-danger code-text-sm code-mt-xs">
              ⚠ 警告：此操作将级联清理该批次下的所有往返物理流量记录，且不可撤销！
            </p>
          </div>
        </Modal>
      )}
    </div>
  );
};

export default RecordingSessionTable;

