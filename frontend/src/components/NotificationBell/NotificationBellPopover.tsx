import React, { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Bell, CheckCheck } from 'lucide-react';
import { EmptyState, useOutsideClick } from '@code/common';
import * as notificationsApi from '../../api/notifications';
import { subscribeServerEvents } from '../../api/sse';
import { useModuleUser } from '../../context/ModuleUserContext';
import type { NotificationDTO } from '../../types';
import { useToast } from '../../utils/toast';
import SafeMarkdownText from '../../utils/safeMarkdown';
import {
  NotificationInboxDrawer,
  formatNotificationTime,
  getSeverityBadgeClass,
  normalizeNotificationLink,
} from './NotificationInboxDrawer';
import './NotificationBell.css';

export interface NotificationBellProps {
  className?: string;
  initialUnreadCount?: number;
  initialRecentItems?: NotificationDTO[];
  initialPopoverOpen?: boolean;
  initialDrawerOpen?: boolean;
}

/** 将 SSE notification.created 事件以幂等方式合入最近通知列表与未读计数 */
export function applyIncomingSSENotification(
  prevItems: NotificationDTO[],
  prevUnread: number,
  incoming: Partial<NotificationDTO> | null
): { items: NotificationDTO[]; unreadCount: number; matched: boolean } {
  if (!incoming || typeof incoming.id !== 'number') {
    return { items: prevItems, unreadCount: prevUnread, matched: false };
  }
  const fullItem = incoming as NotificationDTO;
  if (prevItems.some((item) => item.id === fullItem.id)) {
    return { items: prevItems, unreadCount: prevUnread, matched: true };
  }
  const nextUnread = fullItem.is_read ? prevUnread : prevUnread + 1;
  return {
    items: [fullItem, ...prevItems].slice(0, 8),
    unreadCount: nextUnread,
    matched: true,
  };
}

export const NotificationBell: React.FC<NotificationBellProps> = ({
  className,
  initialUnreadCount = 0,
  initialRecentItems = [],
  initialPopoverOpen = false,
  initialDrawerOpen = false,
}) => {
  const navigate = useNavigate();
  const { isEmbedded } = useModuleUser();
  const toast = useToast();

  const rootRef = useRef<HTMLDivElement>(null);
  const [popoverOpen, setPopoverOpen] = useState(initialPopoverOpen);
  const [drawerOpen, setDrawerOpen] = useState(initialDrawerOpen);
  const [unreadCount, setUnreadCount] = useState(initialUnreadCount);
  const [recentItems, setRecentItems] = useState<NotificationDTO[]>(initialRecentItems);
  const [loadingRecent, setLoadingRecent] = useState(false);
  const [refreshSignal, setRefreshSignal] = useState(0);

  useOutsideClick(rootRef, () => {
    setPopoverOpen(false);
  });

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setPopoverOpen(false);
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, []);

  const refreshUnreadCount = useCallback(async () => {
    try {
      const res = await notificationsApi.getUnreadNotificationCount();
      setUnreadCount(res.unread_count || 0);
    } catch {
      // 未登录或轮询偶发失败静默降级，避免干扰主界面
    }
  }, []);

  const loadRecentNotifications = useCallback(async () => {
    setLoadingRecent(true);
    try {
      const res = await notificationsApi.listNotifications({
        page: 1,
        page_size: 15,
      });
      setRecentItems((res.items || []).slice(0, 8));
      if (typeof res.unread_count === 'number') {
        setUnreadCount(res.unread_count);
      }
    } catch {
      // ignore in popover quick peek
    } finally {
      setLoadingRecent(false);
    }
  }, []);

  useEffect(() => {
    void refreshUnreadCount();
  }, [refreshUnreadCount]);

  // 订阅 SSE 定向事件 notification.created：实时更新未读角标与最近消息
  useEffect(() => {
    const unsubscribe = subscribeServerEvents({
      onMessage: (evt) => {
        if (evt.event !== 'notification.created') {
          return;
        }
        const incoming = evt.data as Partial<NotificationDTO> | null;
        if (!incoming || typeof incoming.id !== 'number') {
          void refreshUnreadCount();
          return;
        }
        setRecentItems((prev) => {
          const applied = applyIncomingSSENotification(prev, 0, incoming);
          if (applied.unreadCount > 0) {
            setUnreadCount((c) => c + applied.unreadCount);
          }
          return applied.items;
        });
        setRefreshSignal((s) => s + 1);
      },
    });
    return () => {
      unsubscribe();
    };
  }, [refreshUnreadCount]);

  const handleTogglePopover = () => {
    const next = !popoverOpen;
    setPopoverOpen(next);
    if (next) {
      void loadRecentNotifications();
    }
  };

  const handleNavigateResolved = useCallback(
    (resolvedUrl: string) => {
      if (resolvedUrl.startsWith('http://') || resolvedUrl.startsWith('https://')) {
        window.open(resolvedUrl, '_blank', 'noopener,noreferrer');
        return;
      }
      navigate(resolvedUrl);
    },
    [navigate]
  );

  const handleClickPopoverItem = async (item: NotificationDTO) => {
    if (!item.is_read) {
      try {
        await notificationsApi.markNotificationRead(item.id);
        setRecentItems((prev) =>
          prev.map((n) => (n.id === item.id ? { ...n, is_read: true } : n))
        );
        setUnreadCount((prev) => Math.max(0, prev - 1));
      } catch (err: unknown) {
        toast.error(err instanceof Error ? err.message : '标记通知已读失败');
      }
    }
    const target = normalizeNotificationLink(item.link_url, isEmbedded);
    setPopoverOpen(false);
    if (target) {
      handleNavigateResolved(target);
    }
  };

  const handleMarkAllRead = async () => {
    try {
      await notificationsApi.markAllNotificationsRead();
      setRecentItems((prev) => prev.map((n) => ({ ...n, is_read: true })));
      setUnreadCount(0);
      setRefreshSignal((s) => s + 1);
      toast.success('已将全部未读通知标记为已读');
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : '全部标记已读失败');
    }
  };

  return (
    <div ref={rootRef} className={className ? `nbell-root ${className}` : 'nbell-root'}>
      <button
        type="button"
        className="nbell-trigger"
        aria-label="站内通知中心"
        data-testid="notification-bell-button"
        onClick={handleTogglePopover}
      >
        <Bell size={16} />
        {unreadCount > 0 && (
          <span className="nbell-badge" data-testid="notification-unread-badge">
            {unreadCount > 99 ? '99+' : unreadCount}
          </span>
        )}
      </button>

      {popoverOpen && (
        <div className="nbell-popover" role="dialog" aria-label="最近治理通知" data-testid="notification-popover">
          <div className="nbell-popover__head">
            <span className="nbell-popover__title">
              站内通知 {unreadCount > 0 ? `(未读 ${unreadCount})` : '(已全部阅读)'}
            </span>
            <button type="button" className="nbell-link-btn" onClick={() => void handleMarkAllRead()}>
              <CheckCheck size={12} /> 全部已读
            </button>
          </div>

          <div className="nbell-popover__list">
            {loadingRecent && recentItems.length === 0 ? (
              <EmptyState compact title="加载通知中..." />
            ) : recentItems.length === 0 ? (
              <EmptyState compact title="暂无站内通知" />
            ) : (
              recentItems.map((item) => (
                <div
                  key={item.id}
                  className={`nbell-item ${item.is_read ? '' : 'nbell-item--unread'}`}
                >
                  <div className="nbell-item__top">
                    <button
                      type="button"
                      className="nbell-item__title-btn"
                      onClick={() => void handleClickPopoverItem(item)}
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
                    <span>{item.is_read ? '已读' : '未读'}</span>
                  </div>
                </div>
              ))
            )}
          </div>

          <div className="nbell-popover__foot">
            <button
              type="button"
              className="nbell-link-btn"
              onClick={() => {
                setPopoverOpen(false);
                setDrawerOpen(true);
              }}
            >
              查看全部收件箱
            </button>
          </div>
        </div>
      )}

      <NotificationInboxDrawer
        open={drawerOpen}
        onClose={() => setDrawerOpen(false)}
        isEmbedded={isEmbedded}
        onNavigateLink={handleNavigateResolved}
        onUnreadCountSync={setUnreadCount}
        refreshSignal={refreshSignal}
      />
    </div>
  );
};

export default NotificationBell;
