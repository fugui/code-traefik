import React, { useCallback, useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Edit3, Plus, RotateCcw, Trash2 } from 'lucide-react';
import {
  Drawer,
  EmptyState,
  MemberSearchSelect,
  Pagination,
  StatusTag,
  useConfirm,
  useDebounce,
  usePagination,
  useToast,
  type StatusType,
} from '@code/common';
import { apiRequest } from '../api/client';
import { useModuleUser } from '../context/ModuleUserContext';
import type {
  ItemPriority,
  ItemStatus,
  ItemUpsertPayload,
  PaginatedItemsResponse,
  TemplateItem,
} from '../types';

const STATUS_MAP: Record<ItemStatus, { label: string; tagStatus: StatusType }> = {
  active: { label: '生效中', tagStatus: 'success' },
  draft: { label: '草稿', tagStatus: 'warning' },
  archived: { label: '已归档', tagStatus: 'neutral' },
};

const PRIORITY_BADGE_MAP: Record<ItemPriority, { label: string; className: string }> = {
  high: { label: '高优 (High)', className: 'code-badge code-badge--danger' },
  medium: { label: '常规 (Medium)', className: 'code-badge code-badge--primary' },
  low: { label: '低优 (Low)', className: 'code-badge code-badge--muted' },
};

const CATEGORY_OPTIONS = [
  { value: 'general', label: '通用业务 (general)' },
  { value: 'security', label: '安全合规 (security)' },
  { value: 'frontend', label: '微前端集成 (frontend)' },
  { value: 'ui', label: '主题与交互 (ui)' },
  { value: 'quality', label: '质量看护 (quality)' },
];

function formatDateTime(iso: string): string {
  if (!iso) return '-';
  const dt = new Date(iso);
  if (Number.isNaN(dt.getTime())) return iso;
  return dt.toLocaleString('zh-CN', { hour12: false });
}

export const ItemListPage: React.FC = () => {
  const { user } = useModuleUser();
  const { showToast } = useToast();
  const confirm = useConfirm();

  // 严格遵守团队通用分页规范：默认 25 条/页，page/pageSize 与筛选条件全部双向同步至 URL Search Params
  const [searchParams, setSearchParams] = useSearchParams();
  const { page, pageSize } = usePagination({ defaultPageSize: 25 });

  const urlSearch = searchParams.get('search') || '';
  const urlStatus = searchParams.get('status') || '';
  const urlCategory = searchParams.get('category') || '';

  const [searchInput, setSearchInput] = useState<string>(urlSearch);
  const debouncedSearch = useDebounce(searchInput, 300);

  const [items, setItems] = useState<TemplateItem[]>([]);
  const [total, setTotal] = useState<number>(0);
  const [loading, setLoading] = useState<boolean>(true);

  // 抽屉新建/编辑表单状态
  const [drawerOpen, setDrawerOpen] = useState<boolean>(false);
  const [editingItem, setEditingItem] = useState<TemplateItem | null>(null);
  const [submitting, setSubmitting] = useState<boolean>(false);

  const [formCode, setFormCode] = useState<string>('');
  const [formName, setFormName] = useState<string>('');
  const [formCategory, setFormCategory] = useState<string>('general');
  const [formStatus, setFormStatus] = useState<ItemStatus>('active');
  const [formPriority, setFormPriority] = useState<ItemPriority>('medium');
  const [formOwnerId, setFormOwnerId] = useState<number | ''>('');
  const [formDesc, setFormDesc] = useState<string>('');

  // 当浏览器前进/后退改变 URL search 时，同步回输入框
  useEffect(() => {
    setSearchInput(urlSearch);
  }, [urlSearch]);

  // 将防抖后的搜索词写入 URL，并重置回第 1 页
  useEffect(() => {
    if (debouncedSearch === urlSearch) return;
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      if (debouncedSearch.trim()) {
        next.set('search', debouncedSearch.trim());
      } else {
        next.delete('search');
      }
      next.set('page', '1');
      return next;
    }, { replace: true });
  }, [debouncedSearch, urlSearch, setSearchParams]);

  const updateFilterParam = (key: string, value: string) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      if (value) {
        next.set(key, value);
      } else {
        next.delete(key);
      }
      next.set('page', '1');
      return next;
    });
  };

  const resetFilters = () => {
    setSearchInput('');
    setSearchParams((prev) => {
      const next = new URLSearchParams();
      const ps = prev.get('pageSize');
      if (ps) next.set('pageSize', ps);
      return next;
    });
  };

  const fetchItems = useCallback(async () => {
    setLoading(true);
    try {
      const qs = new URLSearchParams({
        page: String(page),
        pageSize: String(pageSize),
      });
      if (urlSearch) qs.set('search', urlSearch);
      if (urlStatus) qs.set('status', urlStatus);
      if (urlCategory) qs.set('category', urlCategory);

      const data = await apiRequest<PaginatedItemsResponse>(`/items?${qs.toString()}`);
      setItems(Array.isArray(data.items) ? data.items : []);
      setTotal(typeof data.total === 'number' ? data.total : 0);
    } catch (err) {
      const msg = err instanceof Error ? err.message : '加载条目列表失败';
      showToast(msg, 'error');
    } finally {
      setLoading(false);
    }
  }, [page, pageSize, urlSearch, urlStatus, urlCategory, showToast]);

  useEffect(() => {
    void fetchItems();
  }, [fetchItems]);

  const canManageRow = (row: TemplateItem): boolean => {
    if (!user) return false;
    if (user.is_super_admin || user.is_module_admin) return true;
    return row.created_by === user.id || row.owner_id === user.id;
  };

  const openCreateDrawer = () => {
    setEditingItem(null);
    setFormCode(`TPL-${String(Date.now()).slice(-4)}`);
    setFormName('');
    setFormCategory('general');
    setFormStatus('active');
    setFormPriority('medium');
    setFormOwnerId(user?.id || '');
    setFormDesc('');
    setDrawerOpen(true);
  };

  const openEditDrawer = (row: TemplateItem) => {
    setEditingItem(row);
    setFormCode(row.code);
    setFormName(row.name);
    setFormCategory(row.category);
    setFormStatus(row.status);
    setFormPriority(row.priority);
    setFormOwnerId(row.owner_id ?? '');
    setFormDesc(row.description || '');
    setDrawerOpen(true);
  };

  const handleSaveItem = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!formCode.trim() || !formName.trim()) {
      showToast('请完整填写条目编码与条目名称', 'warning');
      return;
    }

    const payload: ItemUpsertPayload = {
      code: formCode.trim().toUpperCase(),
      name: formName.trim(),
      category: formCategory,
      status: formStatus,
      priority: formPriority,
      description: formDesc.trim(),
      owner_id: typeof formOwnerId === 'number' ? formOwnerId : null,
    };

    setSubmitting(true);
    try {
      if (editingItem) {
        await apiRequest<TemplateItem>(`/items/${editingItem.id}`, {
          method: 'PUT',
          bodyData: payload,
        });
        showToast('业务条目更新成功', 'success');
      } else {
        await apiRequest<TemplateItem>('/items', {
          method: 'POST',
          bodyData: payload,
        });
        showToast('业务条目创建成功', 'success');
      }
      setDrawerOpen(false);
      await fetchItems();
    } catch (err) {
      const msg = err instanceof Error ? err.message : '保存失败';
      showToast(msg, 'error');
    } finally {
      setSubmitting(false);
    }
  };

  const handleDeleteItem = async (row: TemplateItem) => {
    const ok = await confirm({
      title: '确认删除业务条目',
      content: `即将删除条目「[${row.code}] ${row.name}」，该操作将记录至平台全局操作审计且不可恢复，是否继续？`,
      confirmText: '确认删除',
      cancelText: '取消',
      type: 'danger',
    });
    if (!ok) return;

    try {
      await apiRequest<{ message: string }>(`/items/${row.id}`, {
        method: 'DELETE',
      });
      showToast('条目已删除', 'success');
      await fetchItems();
    } catch (err) {
      const msg = err instanceof Error ? err.message : '删除条目失败';
      showToast(msg, 'error');
    }
  };

  const hasActiveFilters = Boolean(urlSearch || urlStatus || urlCategory);

  return (
    <div className="code-tpl-workspace">
      <div className="code-tpl-page-header">
        <div>
          <h1 className="code-tpl-page-title">示例条目管理</h1>
          <p className="code-tpl-page-subtitle">
            标准列表页样板：演示 URL 状态同步分页（5 页连续滑动窗口）、条件检索、Drawer 抽屉表单、MemberSearchSelect 与二次确认弹窗。
          </p>
        </div>
        <div className="code-tpl-page-actions">
          <button type="button" className="btn btn-primary" onClick={openCreateDrawer}>
            <Plus size={16} />
            <span>新建条目</span>
          </button>
        </div>
      </div>

      {/* 搜索与筛选工具栏 */}
      <div className="code-card code-tpl-toolbar">
        <div className="code-tpl-filter-group">
          <input
            type="text"
            className="code-input code-tpl-search-input"
            placeholder="搜索编码、名称或描述关键字..."
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
            aria-label="关键字搜索"
          />

          <select
            className="code-select code-tpl-filter-select"
            value={urlStatus}
            onChange={(e) => updateFilterParam('status', e.target.value)}
            aria-label="按状态筛选"
          >
            <option value="">全部状态</option>
            <option value="active">生效中 (Active)</option>
            <option value="draft">草稿 (Draft)</option>
            <option value="archived">已归档 (Archived)</option>
          </select>

          <select
            className="code-select code-tpl-filter-select"
            value={urlCategory}
            onChange={(e) => updateFilterParam('category', e.target.value)}
            aria-label="按分类筛选"
          >
            <option value="">全部分类</option>
            {CATEGORY_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>

          {hasActiveFilters && (
            <button type="button" className="btn btn-secondary btn-small" onClick={resetFilters}>
              <RotateCcw size={13} />
              <span>重置筛选</span>
            </button>
          )}
        </div>
      </div>

      {/* 数据表格与底部分页栏 */}
      <div className="code-card code-tpl-table-card">
        <div className="code-tpl-table-scroll">
          <table className="code-table">
            <thead>
              <tr>
                <th>编码</th>
                <th>条目名称与摘要</th>
                <th>分类</th>
                <th>状态</th>
                <th>优先级</th>
                <th>负责人</th>
                <th>更新时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <EmptyState
                  inTable
                  colSpan={8}
                  compact
                  title="正在加载业务条目..."
                  description="请稍候，正在拉取最新分页数据"
                />
              ) : items.length === 0 ? (
                <EmptyState
                  inTable
                  colSpan={8}
                  type={hasActiveFilters ? 'search' : 'data'}
                  title={hasActiveFilters ? '未找到符合筛选条件的条目' : '暂无业务条目'}
                  description={
                    hasActiveFilters
                      ? '请尝试调整搜索词或点击“重置筛选”查看全部记录'
                      : '点击右上角“新建条目”创建第一条示范记录'
                  }
                  action={
                    hasActiveFilters ? (
                      <button type="button" className="btn btn-secondary btn-small" onClick={resetFilters}>
                        清空筛选条件
                      </button>
                    ) : (
                      <button type="button" className="btn btn-primary btn-small" onClick={openCreateDrawer}>
                        立即新建
                      </button>
                    )
                  }
                />
              ) : (
                items.map((row) => {
                  const statusMeta = STATUS_MAP[row.status] || STATUS_MAP.active;
                  const priorityMeta = PRIORITY_BADGE_MAP[row.priority] || PRIORITY_BADGE_MAP.medium;
                  const editable = canManageRow(row);

                  return (
                    <tr key={row.id}>
                      <td>
                        <code className="code-tpl-code-inline">{row.code}</code>
                      </td>
                      <td>
                        <div className="code-tpl-item-name">{row.name}</div>
                        {row.description && (
                          <div className="code-tpl-item-desc" title={row.description}>
                            {row.description}
                          </div>
                        )}
                      </td>
                      <td>
                        <span className="code-badge code-badge--muted">{row.category}</span>
                      </td>
                      <td>
                        <StatusTag status={statusMeta.tagStatus} text={statusMeta.label} size="sm" />
                      </td>
                      <td>
                        <span className={priorityMeta.className}>{priorityMeta.label}</span>
                      </td>
                      <td>{row.owner?.name || row.owner?.username || '-'}</td>
                      <td className="text-secondary">{formatDateTime(row.updated_at)}</td>
                      <td>
                        <div className="code-tpl-row-actions">
                          <button
                            type="button"
                            className="btn btn-text btn-small"
                            onClick={() => openEditDrawer(row)}
                            disabled={!editable}
                            title={editable ? '编辑条目' : '仅创建人、负责人或管理员可编辑'}
                          >
                            <Edit3 size={14} />
                            <span>编辑</span>
                          </button>
                          <button
                            type="button"
                            className="btn btn-text btn-small code-tpl-danger-btn"
                            onClick={() => void handleDeleteItem(row)}
                            disabled={!editable}
                            title={editable ? '删除条目' : '仅创建人、负责人或管理员可删除'}
                          >
                            <Trash2 size={14} />
                            <span>删除</span>
                          </button>
                        </div>
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>

        {total > 0 && (
          <div className="code-tpl-pagination-bar">
            <Pagination totalItems={total} defaultPageSize={25} pageSizeOptions={[15, 25, 50, 100]} />
          </div>
        )}
      </div>

      {/* 新建 / 编辑抽屉表单 */}
      <Drawer
        open={drawerOpen}
        onClose={() => setDrawerOpen(false)}
        title={editingItem ? `编辑条目 · ${editingItem.code}` : '新建示例业务条目'}
        subtitle="所有字段变更均自动接入平台异步操作审计（支持字段级 Diff 对比）"
        width="md"
        destroyOnClose
        footer={
          <div className="code-tpl-drawer-footer">
            <button
              type="button"
              className="btn btn-secondary"
              onClick={() => setDrawerOpen(false)}
              disabled={submitting}
            >
              取消
            </button>
            <button
              type="submit"
              form="code-tpl-item-form"
              className="btn btn-primary"
              disabled={submitting}
            >
              {submitting ? '保存中...' : '保存提交'}
            </button>
          </div>
        }
      >
        <form id="code-tpl-item-form" className="code-tpl-form" onSubmit={(e) => void handleSaveItem(e)}>
          <div className="code-tpl-form-row">
            <div className="code-tpl-field">
              <label className="code-tpl-label" htmlFor="tpl-field-code">
                唯一编码 <span className="code-tpl-required">*</span>
              </label>
              <input
                id="tpl-field-code"
                type="text"
                className="code-input"
                value={formCode}
                onChange={(e) => setFormCode(e.target.value)}
                placeholder="例如 TPL-1001"
                maxLength={64}
                required
              />
              <span className="code-tpl-hint">仅支持字母、数字、下划线或连字符，保存时自动转为大写。</span>
            </div>

            <div className="code-tpl-field">
              <label className="code-tpl-label" htmlFor="tpl-field-category">
                业务分类
              </label>
              <select
                id="tpl-field-category"
                className="code-select"
                value={formCategory}
                onChange={(e) => setFormCategory(e.target.value)}
              >
                {CATEGORY_OPTIONS.map((opt) => (
                  <option key={opt.value} value={opt.value}>
                    {opt.label}
                  </option>
                ))}
              </select>
            </div>
          </div>

          <div className="code-tpl-field">
            <label className="code-tpl-label" htmlFor="tpl-field-name">
              条目名称 <span className="code-tpl-required">*</span>
            </label>
            <input
              id="tpl-field-name"
              type="text"
              className="code-input"
              value={formName}
              onChange={(e) => setFormName(e.target.value)}
              placeholder="请输入清晰的业务条目名称（2-128 字符）"
              maxLength={128}
              required
            />
          </div>

          <div className="code-tpl-form-row">
            <div className="code-tpl-field">
              <label className="code-tpl-label" htmlFor="tpl-field-status">
                当前状态
              </label>
              <select
                id="tpl-field-status"
                className="code-select"
                value={formStatus}
                onChange={(e) => setFormStatus(e.target.value as ItemStatus)}
              >
                <option value="active">生效中 (Active)</option>
                <option value="draft">草稿 (Draft)</option>
                <option value="archived">已归档 (Archived)</option>
              </select>
            </div>

            <div className="code-tpl-field">
              <label className="code-tpl-label" htmlFor="tpl-field-priority">
                优先级
              </label>
              <select
                id="tpl-field-priority"
                className="code-select"
                value={formPriority}
                onChange={(e) => setFormPriority(e.target.value as ItemPriority)}
              >
                <option value="high">高优 (High)</option>
                <option value="medium">常规 (Medium)</option>
                <option value="low">低优 (Low)</option>
              </select>
            </div>
          </div>

          <div className="code-tpl-field">
            <label className="code-tpl-label">负责人指派</label>
            <MemberSearchSelect
              value={formOwnerId}
              onChange={(uid) => setFormOwnerId(uid)}
            />
            <span className="code-tpl-hint">复用 @code/common 的成员检索选择器，自动关联平台共享用户库。</span>
          </div>

          <div className="code-tpl-field">
            <label className="code-tpl-label" htmlFor="tpl-field-desc">
              详细说明
            </label>
            <textarea
              id="tpl-field-desc"
              className="code-textarea"
              rows={4}
              maxLength={2000}
              value={formDesc}
              onChange={(e) => setFormDesc(e.target.value)}
              placeholder="可选：补充业务背景、验收标准或备注信息..."
            />
          </div>
        </form>
      </Drawer>
    </div>
  );
};

export default ItemListPage;
