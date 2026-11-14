import React, { useCallback, useEffect, useState } from 'react';
import { CheckCheck, ExternalLink, Mail, RefreshCw } from 'lucide-react';
import { Drawer, EmptyState, Pagination, usePaginationState } from '@code/common';
import * as notificationsApi from '../../api/notifications';
import type { NotificationDTO } from '../../types';
import { MODULE_BASE_PATH } from '../../constants';
import { useToast } from '../../utils/toast';
import SafeMarkdownText from '../../utils/safeMarkdown';

export interface NotificationInboxDrawerProps {
  open: boolean;
  onClose: () => void;
  isEmbedded: boolean;
  onNavigateLink: (resolvedUrl: string) => void;
  onUnreadCountSync: (nextUnread: number) => void;
  refreshSignal?: number;
  initialItems?: NotificationDTO[];
  initialTotal?: number;
}

/** 根据独立模式/嵌入门户模式规范化通知跳转链接 */
export function normalizeNotificationLink(rawUrl: string, isEmbedded: boolean): string {
  const trimmed = (rawUrl || '').trim();
  if (!trimmed) return '';
  if (trimmed.startsWith('http://') || trimmed.startsWith('https://')) {
    return trimmed;
  }
  const normalized = trimmed.startsWith('/') ? trimmed : `/${trimmed}`;
  if (isEmbedded) {
    return normalized.startsWith(`${MODULE_BASE_PATH}/`) || normalized === MODULE_BASE_PATH
      ? normalized
      : `${MODULE_BASE_PATH}${normalized}`;
  }
  if (normalized.startsWith(`${MODULE_BASE_PATH}/`)) {
    return normalized.slice(MODULE_BASE_PATH.length) || '/dashboard';
  }
  return normalized;
}

export function getSeverityBadgeClass(severity: string): string {
  switch (severity) {
    case 'CRITICAL':
      return 'code-badge-danger';
    case 'WARNING':
      return 'code-badge-warning';
    default:
      return 'code-badge-info';
  }
}

export function formatNotificationTime(iso?: string | null): string {
  if (!iso) return '-';
  const dt = new Date(iso);
  if (Number.isNaN(dt.getTime())) return iso;
  return dt.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
}

export const NotificationInboxDrawer: React.FC<NotificationInboxDrawerProps> = ({
  open,
  onClose,
  isEmbedded,
  onNavigateLink,
  onUnreadCountSync,
  refreshSignal = 0,
  initialItems,
  initialTotal,
}) => {
  const toast = useToast();
  const { page, pageSize, setPage, setPageSize, resetPage } = usePaginationState({
    defaultPageSize: 25,
  });

  const [readFilter, setReadFilter] = useState<'all' | 'unread' | 'read'>('all');
  const [categoryFilter, setCategoryFilter] = useState<string>('');
  const [severityFilter, setSeverityFilter] = useState<string>('');
  const [items, setItems] = useState<NotificationDTO[]>(initialItems ?? []);
  const [total, setTotal] = useState(initialTotal ?? initialItems?.length ?? 0);
  const [loading, setLoading] = useState(false);

  const loadInbox = useCallback(async () => {
    setLoading(true);
    try {
      const isReadParam =
        readFilter === 'unread' ? false : readFilter === 'read' ? true : undefined;
      const res = await notificationsApi.listNotifications({
        page,
        page_size: pageSize,
        is_read: isReadParam,
        category: categoryFilter || undefined,
        severity: severityFilter || undefined,
      });
      setItems(res.items || []);
      setTotal(res.total || 0);
      if (typeof res.unread_count === 'number') {
        onUnreadCountSync(res.unread_count);
      }
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : '加载站内收件箱失败');
    } finally {
      setLoading(false);
    }
  }, [page, pageSize, readFilter, categoryFilter, severityFilter, onUnreadCountSync, toast]);

  useEffect(() => {
    if (!open) return;
    void loadInbox();
  }, [open, refreshSignal, loadInbox]);

  const handleMarkRead = async (item: NotificationDTO) => {
    if (!item.is_read) {
      try {
        await notificationsApi.markNotificationRead(item.id);
        setItems((prev) =>
          prev.map((n) => (n.id === item.id ? { ...n, is_read: true } : n))
        );
        const countRes = await notificationsApi.getUnreadNotificationCount();
        onUnreadCountSync(countRes.unread_count);
      } catch (err: unknown) {
        toast.error(err instanceof Error ? err.message : '标记已读失败');
      }
    }
  };

  const handleMarkAllRead = async () => {
    try {
      await notificationsApi.markAllNotificationsRead();
      toast.success('已将全部未读通知标记为已读');
      await loadInbox();
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : '全部标记已读失败');
    }
  };

  const handleOpenNotification = async (item: NotificationDTO) => {
    if (!item.is_read) {
      await handleMarkRead(item);
    }
    const target = normalizeNotificationLink(item.link_url, isEmbedded);
    if (target) {
      onClose();
      onNavigateLink(target);
    }
  };

  return (
    <Drawer
      open={open}
      onClose={onClose}
      title="站内治理通知收件箱"
      width="lg"
    >
      <div className="nbell-drawer-body" data-testid="notification-inbox-drawer">
        <div className="nbell-drawer-toolbar">
          <div className="nbell-drawer-filters">
            <select
              className="code-select"
              aria-label="按阅读状态筛选通知"
              value={readFilter}
              onChange={(e) => {
                setReadFilter(e.target.value as 'all' | 'unread' | 'read');
                resetPage();
              }}
            >
              <option value="all">全部消息</option>
              <option value="unread">仅看未读</option>
              <option value="read">仅看已读</option>
            </select>

            <select
              className="code-select"
              aria-label="按通知分类筛选"
              value={categoryFilter}
              onChange={(e) => {
                setCategoryFilter(e.target.value);
                resetPage();
              }}
            >
              <option value="">全部类型</option>
              <option value="BREAKING_ALERT">破坏性变更波及</option>
              <option value="MR_GATE_BLOCKED">MR 门禁阻断</option>
              <option value="MR_GATE_PASSED">MR 门禁放行</option>
              <option value="DUPLICATE_WARN">AST 重复结构体警告</option>
              <option value="BASE_LIB_IMPACT">公共基础库变更波及</option>
              <option value="SCHEMA_PUBLISHED">主干自动入池发布</option>
            </select>

            <select
              className="code-select"
              aria-label="按严重级别筛选"
              value={severityFilter}
              onChange={(e) => {
                setSeverityFilter(e.target.value);
                resetPage();
              }}
            >
              <option value="">全部级别</option>
              <option value="CRITICAL">CRITICAL</option>
              <option value="WARNING">WARNING</option>
              <option value="INFO">INFO</option>
            </select>
          </div>

          <div className="nbell-drawer-card__actions">
            <button
              type="button"
              className="btn btn-secondary btn-sm"
              onClick={() => void handleMarkAllRead()}
            >
              <CheckCheck size={14} />
              <span>全部已读</span>
            </button>
            <button
              type="button"
              className="btn btn-secondary btn-sm"
              onClick={() => void loadInbox()}
              disabled={loading}
            >
              <RefreshCw size={14} className={loading ? 'pgit-spin' : ''} />
              <span>刷新</span>
            </button>
          </div>
        </div>

        {loading && items.length === 0 ? (
          <EmptyState compact title="正在加载通知收件箱..." />
        ) : items.length === 0 ? (
          <EmptyState
            compact
            title="收件箱暂无匹配通知"
            description="当您负责的下游架构模块受到 .proto 破坏性变更波及，或 MR 门禁阻断/放行时，系统将自动在此推送提醒。"
          />
        ) : (
          <div className="nbell-drawer-list">
            {items.map((item) => (
              <div
                key={item.id}
                className={`nbell-drawer-card ${item.is_read ? '' : 'nbell-drawer-card--unread'}`}
                data-testid={`notification-drawer-item-${item.id}`}
              >
                <div className="nbell-item__top">
                  <button
                    type="button"
                    className="nbell-item__title-btn"
                    onClick={() => void handleOpenNotification(item)}
                  >
                    {item.title}
                  </button>
                  <span className={`code-badge ${getSeverityBadgeClass(item.severity)}`}>
                    {item.severity}
                  </span>
                </div>

                {item.body_markdown && <SafeMarkdownText content={item.body_markdown} />}

                <div className="nbell-item__meta">
                  <span>{formatNotificationTime(item.created_at)}</span>
                  <span>分类: {item.category}</span>
                  <span>
                    <Mail size={11} /> 邮件状态: {item.email_status || 'SKIPPED'}
                  </span>
                </div>

                <div className="nbell-drawer-card__actions">
                  {!item.is_read && (
                    <button
                      type="button"
                      className="btn btn-secondary btn-sm"
                      onClick={() => void handleMarkRead(item)}
                    >
                      标为已读
                    </button>
                  )}
                  {item.link_url && (
                    <button
                      type="button"
                      className="btn btn-primary btn-sm"
                      onClick={() => void handleOpenNotification(item)}
                    >
                      <ExternalLink size={12} />
                      <span>查看关联详情</span>
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}

        {total > 0 && (
          <Pagination
            page={page}
            pageSize={pageSize}
            totalItems={total}
            onPageChange={setPage}
            onPageSizeChange={(nextSize) => {
              setPageSize(nextSize);
              resetPage();
            }}
            pageSizeOptions={[15, 25, 50, 100]}
          />
        )}
      </div>
    </Drawer>
  );
};

export default NotificationInboxDrawer;
