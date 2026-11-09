import React, { useState } from 'react';
import { Drawer } from '@code/common';
import {
  Bookmark,
  Star,
  Trash2,
  Edit3,
  UploadCloud,
  RotateCcw,
  AlertCircle,
  Check,
  X,
} from 'lucide-react';
import type {
  TopologyLayoutListResponse,
  TopologyLayoutSummaryDTO,
} from '../../../types/topology_graph';

export interface TopologyBookmarkDrawerProps {
  open: boolean;
  onClose: () => void;
  view: 'LOGICAL' | 'PHYSICAL';
  isAdmin: boolean;
  layoutList: TopologyLayoutListResponse | null;
  activeLayoutId?: number | null;
  conflictBannerMessage?: string | null;
  driftSummaryMessage?: string | null;
  onPublishCanonical: () => Promise<void>;
  onResetCanonical: () => Promise<void>;
  onApplyCanonical: () => void;
  onCreatePersonal: (data: { name: string; description: string; is_default: boolean }) => Promise<void>;
  onApplyPersonal: (id: number) => Promise<void>;
  onUpdatePersonal: (
    id: number,
    data: { name?: string; description?: string; is_default?: boolean; updateSnapshot?: boolean }
  ) => Promise<void>;
  onDeletePersonal: (id: number) => Promise<void>;
}

export const TopologyBookmarkDrawer: React.FC<TopologyBookmarkDrawerProps> = ({
  open,
  onClose,
  view,
  isAdmin,
  layoutList,
  activeLayoutId = null,
  conflictBannerMessage = null,
  driftSummaryMessage = null,
  onPublishCanonical,
  onResetCanonical,
  onApplyCanonical,
  onCreatePersonal,
  onApplyPersonal,
  onUpdatePersonal,
  onDeletePersonal,
}) => {
  const [newName, setNewName] = useState('');
  const [newDesc, setNewDesc] = useState('');
  const [newIsDefault, setNewIsDefault] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [editingId, setEditingId] = useState<number | null>(null);
  const [editingName, setEditingName] = useState('');
  const [confirmDeleteId, setConfirmDeleteId] = useState<number | null>(null);
  const [confirmResetCanonical, setConfirmResetCanonical] = useState(false);

  const used = layoutList?.quota?.used ?? 0;
  const limit = layoutList?.quota?.limit ?? 20;
  const isQuotaReached = used >= limit;
  const canonical = layoutList?.canonical ?? null;
  const personalItems = layoutList?.personal_items ?? [];

  const handleCreateSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = newName.trim();
    if (!trimmed || isQuotaReached || submitting) return;
    setSubmitting(true);
    try {
      await onCreatePersonal({
        name: trimmed,
        description: newDesc.trim(),
        is_default: newIsDefault,
      });
      setNewName('');
      setNewDesc('');
      setNewIsDefault(false);
    } finally {
      setSubmitting(false);
    }
  };

  const handleStartRename = (item: TopologyLayoutSummaryDTO) => {
    setEditingId(item.id);
    setEditingName(item.name);
  };

  const handleSaveRename = async (item: TopologyLayoutSummaryDTO) => {
    const trimmed = editingName.trim();
    if (!trimmed) return;
    await onUpdatePersonal(item.id, { name: trimmed });
    setEditingId(null);
  };

  return (
    <Drawer
      open={open}
      onClose={onClose}
      title={`拓扑视图书签与团队基准 (${view === 'LOGICAL' ? '逻辑架构' : '物理架构'})`}
      width={560}
    >
      <div className="code-arch-bookmark-drawer" data-testid="topology-bookmark-drawer">
        {conflictBannerMessage && (
          <div className="code-arch-guard-banner" data-testid="bookmark-conflict-banner">
            <AlertCircle size={15} />
            <span>{conflictBannerMessage}</span>
          </div>
        )}

        {driftSummaryMessage && (
          <div className="code-arch-guard-banner" data-testid="bookmark-drift-banner">
            <AlertCircle size={15} />
            <span>{driftSummaryMessage}</span>
          </div>
        )}

        {/* 1. 团队共享基准布局区 (Canonical Baseline) */}
        <div className="code-arch-bookmark-drawer__section">
          <div className="code-arch-bookmark-drawer__section-header">
            <span className="code-arch-bookmark-drawer__section-title">团队共享基准视图 (SHARED)</span>
            {canonical && (
              <span className="code-arch-badge code-arch-badge--counter">
                {`版本 v${canonical.revision}`}
              </span>
            )}
          </div>

          <div className="code-arch-bookmark-drawer__card">
            {canonical ? (
              <div className="code-arch-bookmark-drawer__card-main">
                <div className="code-arch-bookmark-drawer__item-title">
                  <strong>{canonical.name || '默认团队基准'}</strong>
                </div>
                {canonical.description && (
                  <div className="code-arch-bookmark-drawer__item-desc">{canonical.description}</div>
                )}
              </div>
            ) : (
              <div className="code-arch-bookmark-drawer__empty">
                当前视角尚未发布团队基准布局，冷启动将使用 ELK L0 自动正交排版。
              </div>
            )}

            <div className="code-arch-bookmark-drawer__card-actions">
              {canonical && (
                <button
                  type="button"
                  className="code-btn code-btn--secondary code-btn--xs"
                  onClick={onApplyCanonical}
                  data-testid="apply-canonical-btn"
                >
                  恢复团队基准视图
                </button>
              )}

              {isAdmin && (
                <>
                  <button
                    type="button"
                    className="code-btn code-btn--primary code-btn--xs"
                    onClick={() => void onPublishCanonical()}
                    data-testid="publish-canonical-btn"
                  >
                    <UploadCloud size={12} />
                    <span>发布当前布局为团队基准</span>
                  </button>

                  {!confirmResetCanonical ? (
                    <button
                      type="button"
                      className="code-btn code-btn--danger code-btn--xs"
                      onClick={() => setConfirmResetCanonical(true)}
                      data-testid="reset-canonical-btn"
                    >
                      <RotateCcw size={12} />
                      <span>重置团队基准</span>
                    </button>
                  ) : (
                    <span className="code-arch-bookmark-drawer__inline-confirm">
                      <button
                        type="button"
                        className="code-btn code-btn--danger code-btn--xs"
                        onClick={() => {
                          setConfirmResetCanonical(false);
                          void onResetCanonical();
                        }}
                        data-testid="confirm-reset-canonical-btn"
                      >
                        确认重置
                      </button>
                      <button
                        type="button"
                        className="code-btn code-btn--secondary code-btn--xs"
                        onClick={() => setConfirmResetCanonical(false)}
                      >
                        取消
                      </button>
                    </span>
                  )}
                </>
              )}
            </div>
          </div>
        </div>

        {/* 2. 新建个人书签表单 */}
        <div className="code-arch-bookmark-drawer__section">
          <div className="code-arch-bookmark-drawer__section-header">
            <span className="code-arch-bookmark-drawer__section-title">保存当前状态为个人书签</span>
            <span
              className={`code-arch-bookmark-drawer__quota-badge ${
                isQuotaReached ? 'code-arch-badge--warning' : 'code-arch-badge--counter'
              }`}
              data-testid="bookmark-quota-badge"
            >
              {`配额 ${used} / ${limit}`}
            </span>
          </div>

          <form className="code-arch-bookmark-drawer__form" onSubmit={(e) => void handleCreateSubmit(e)}>
            <div className="code-arch-bookmark-drawer__form-row">
              <input
                type="text"
                className="code-arch-toolbar__search-input code-arch-bookmark-drawer__input"
                placeholder={isQuotaReached ? '个人书签配额已满 (20/20)，请先删除旧书签' : '输入书签名称 (如: 核心交易排障视图)...'}
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                disabled={isQuotaReached || submitting}
                maxLength={80}
                data-testid="new-bookmark-name-input"
              />
              <button
                type="submit"
                className="code-btn code-btn--primary code-btn--sm"
                disabled={isQuotaReached || !newName.trim() || submitting}
                data-testid="create-bookmark-submit-btn"
              >
                <Bookmark size={13} />
                <span>保存新书签</span>
              </button>
            </div>

            <div className="code-arch-bookmark-drawer__form-row">
              <input
                type="text"
                className="code-arch-toolbar__search-input code-arch-bookmark-drawer__input"
                placeholder="可选备注说明..."
                value={newDesc}
                onChange={(e) => setNewDesc(e.target.value)}
                disabled={isQuotaReached || submitting}
                maxLength={255}
              />
              <label className="code-arch-toolbar__checkbox-label">
                <input
                  type="checkbox"
                  checked={newIsDefault}
                  onChange={(e) => setNewIsDefault(e.target.checked)}
                  disabled={isQuotaReached || submitting}
                />
                <span>设为默认</span>
              </label>
            </div>
          </form>
        </div>

        {/* 3. 个人书签列表 */}
        <div className="code-arch-bookmark-drawer__section">
          <div className="code-arch-bookmark-drawer__section-header">
            <span className="code-arch-bookmark-drawer__section-title">
              我的视图书签 ({personalItems.length})
            </span>
          </div>

          {personalItems.length === 0 ? (
            <div className="code-arch-bookmark-drawer__empty">
              暂无个人拓扑书签，调整画布聚焦、筛选或拖拽位置后可在上方保存。
            </div>
          ) : (
            <div className="code-arch-bookmark-drawer__list">
              {personalItems.map((item) => {
                const isActive = activeLayoutId === item.id;
                const isEditing = editingId === item.id;
                const isConfirmingDelete = confirmDeleteId === item.id;

                return (
                  <div
                    key={item.id}
                    className={`code-arch-bookmark-drawer__item ${
                      isActive ? 'code-arch-bookmark-drawer__item--active' : ''
                    }`}
                    data-testid={`bookmark-item-${item.id}`}
                  >
                    <div className="code-arch-bookmark-drawer__item-main">
                      {isEditing ? (
                        <div className="code-arch-bookmark-drawer__inline-edit">
                          <input
                            type="text"
                            className="code-arch-toolbar__search-input"
                            value={editingName}
                            onChange={(e) => setEditingName(e.target.value)}
                            maxLength={80}
                          />
                          <button
                            type="button"
                            className="code-btn code-btn--primary code-btn--xs"
                            onClick={() => void handleSaveRename(item)}
                          >
                            <Check size={12} />
                          </button>
                          <button
                            type="button"
                            className="code-btn code-btn--secondary code-btn--xs"
                            onClick={() => setEditingId(null)}
                          >
                            <X size={12} />
                          </button>
                        </div>
                      ) : (
                        <div className="code-arch-bookmark-drawer__item-title">
                          <strong>{item.name}</strong>
                          {item.is_default && (
                            <span className="code-arch-badge code-arch-badge--provide">默认</span>
                          )}
                          <span className="code-arch-badge code-arch-badge--counter">
                            聚焦: {item.focus_scope}
                          </span>
                        </div>
                      )}
                      {item.description && (
                        <div className="code-arch-bookmark-drawer__item-desc">{item.description}</div>
                      )}
                    </div>

                    <div className="code-arch-bookmark-drawer__item-actions">
                      <button
                        type="button"
                        className="code-btn code-btn--primary code-btn--xs"
                        onClick={() => void onApplyPersonal(item.id)}
                        data-testid={`apply-bookmark-btn-${item.id}`}
                      >
                        加载
                      </button>
                      <button
                        type="button"
                        className="code-btn code-btn--secondary code-btn--xs"
                        onClick={() => void onUpdatePersonal(item.id, { updateSnapshot: true })}
                        title="将当前画布坐标与筛选快照覆盖保存至此书签"
                        data-testid={`overwrite-bookmark-btn-${item.id}`}
                      >
                        覆盖保存
                      </button>
                      <button
                        type="button"
                        className="code-btn code-btn--secondary code-btn--xs"
                        onClick={() => void onUpdatePersonal(item.id, { is_default: !item.is_default })}
                        title={item.is_default ? '取消默认书签' : '设为当前视角默认书签'}
                        data-testid={`default-bookmark-btn-${item.id}`}
                      >
                        <Star size={12} />
                      </button>
                      <button
                        type="button"
                        className="code-btn code-btn--secondary code-btn--xs"
                        onClick={() => handleStartRename(item)}
                        title="重命名书签"
                      >
                        <Edit3 size={12} />
                      </button>

                      {!isConfirmingDelete ? (
                        <button
                          type="button"
                          className="code-btn code-btn--danger code-btn--xs"
                          onClick={() => setConfirmDeleteId(item.id)}
                          title="删除书签"
                          data-testid={`delete-bookmark-btn-${item.id}`}
                        >
                          <Trash2 size={12} />
                        </button>
                      ) : (
                        <span className="code-arch-bookmark-drawer__inline-confirm">
                          <button
                            type="button"
                            className="code-btn code-btn--danger code-btn--xs"
                            onClick={() => {
                              setConfirmDeleteId(null);
                              void onDeletePersonal(item.id);
                            }}
                            data-testid={`confirm-delete-bookmark-btn-${item.id}`}
                          >
                            确认
                          </button>
                          <button
                            type="button"
                            className="code-btn code-btn--secondary code-btn--xs"
                            onClick={() => setConfirmDeleteId(null)}
                          >
                            取消
                          </button>
                        </span>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </div>
    </Drawer>
  );
};
