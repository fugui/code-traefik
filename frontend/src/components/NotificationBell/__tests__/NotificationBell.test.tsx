import React from 'react';
import { describe, expect, it, vi } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import {
  NotificationBell,
  NotificationInboxDrawer,
  applyIncomingSSENotification,
  normalizeNotificationLink,
} from '../index';
import { EmbeddedRoutes, StandaloneShell } from '../../../App';
import { MODULE_BASE_PATH } from '../../../constants';
import { ModuleUserProvider } from '../../../context/ModuleUserContext';
import type { NotificationDTO } from '../../../types';

vi.mock('@code/common', async () => {
  const actual = await vi.importActual<typeof import('@code/common')>('@code/common');
  return {
    ...actual,
    useTheme: () => ({ theme: 'light', toggleTheme: () => undefined }),
    Drawer: ({ open, title, children }: { open: boolean; title?: React.ReactNode; children?: React.ReactNode }) =>
      open ? (
        <div data-testid="mock-drawer">
          <h2>{title}</h2>
          {children}
        </div>
      ) : null,
  };
});

const sampleNotif: NotificationDTO = {
  id: 301,
  recipient_user_id: 9,
  category: 'BREAKING_ALERT',
  severity: 'CRITICAL',
  title: '下游模块清结算收到破坏性变更告警',
  body_markdown: '### MR !89 波及提示\n- 删除字段 `Order.fee_type`',
  link_url: '/schemas?tab=mr-gates&runId=102',
  source_type: 'mr_check_run',
  source_id: 102,
  is_read: false,
  external_channel: 'CODE_NOTIFIER',
  email_status: 'SENT',
  email_error: '',
  created_at: '2026-11-14T11:00:00Z',
};

describe('NotificationBell Dual-Mode & SSE Incremental State Test Suite', () => {
  it('TC-NOTIF-01: 铃铛渲染未读角标、Popover、收件箱 Drawer 安全 Markdown 与邮件状态', () => {
    const htmlPopover = renderToStaticMarkup(
      <MemoryRouter>
        <NotificationBell
          initialUnreadCount={3}
          initialRecentItems={[sampleNotif]}
          initialPopoverOpen
        />
      </MemoryRouter>
    );

    expect(htmlPopover).toContain('notification-bell-button');
    expect(htmlPopover).toContain('notification-unread-badge');
    expect(htmlPopover).toContain('>3<');
    expect(htmlPopover).toContain('下游模块清结算收到破坏性变更告警');
    expect(htmlPopover).toContain('查看全部收件箱');

    const htmlDrawer = renderToStaticMarkup(
      <MemoryRouter>
        <NotificationInboxDrawer
          open
          onClose={() => undefined}
          isEmbedded={false}
          onNavigateLink={() => undefined}
          onUnreadCountSync={() => undefined}
          initialItems={[sampleNotif]}
          initialTotal={1}
        />
      </MemoryRouter>
    );

    expect(htmlDrawer).toContain('站内治理通知收件箱');
    expect(htmlDrawer).toContain('Order.fee_type');
    expect(htmlDrawer).toContain('邮件状态: SENT');
    expect(htmlDrawer).toContain('查看关联详情');
  });

  it('TC-NOTIF-02: SSE notification.created 定向推送幂等递增未读计数且双模深链路径正确解析', () => {
    const first = applyIncomingSSENotification([], 1, sampleNotif);
    expect(first.matched).toBe(true);
    expect(first.unreadCount).toBe(2);
    expect(first.items).toHaveLength(1);

    // 同一 notification id 重放不会重复叠加角标
    const dup = applyIncomingSSENotification(first.items, first.unreadCount, sampleNotif);
    expect(dup.unreadCount).toBe(2);
    expect(dup.items).toHaveLength(1);

    expect(normalizeNotificationLink('/schemas?tab=mr-gates&runId=102', true)).toBe(
      `${MODULE_BASE_PATH}/schemas?tab=mr-gates&runId=102`
    );
    expect(normalizeNotificationLink(`${MODULE_BASE_PATH}/schemas?tab=mr-gates&runId=102`, false)).toBe(
      '/schemas?tab=mr-gates&runId=102'
    );
  });

  it('TC-NOTIF-03: 嵌入门户态 (WorkspaceUtilityHeader) 与独立运行态 (StandaloneShell) 均挂载 NotificationBell', () => {
    const embeddedHtml = renderToStaticMarkup(
      <MemoryRouter initialEntries={[`${MODULE_BASE_PATH}/dashboard`]}>
        <ModuleUserProvider
          value={{ user: null, loadingUser: false, isEmbedded: true, reloadUser: async () => undefined }}
        >
          <EmbeddedRoutes />
        </ModuleUserProvider>
      </MemoryRouter>
    );
    expect(embeddedHtml).toContain('workspace-utility-header');
    expect(embeddedHtml).toContain('notification-bell-button');

    const standaloneHtml = renderToStaticMarkup(
      <MemoryRouter initialEntries={['/dashboard']}>
        <ModuleUserProvider
          value={{
            user: {
              id: 1,
              username: 'admin',
              name: '管理员',
              roles: ['super_admin'],
              is_super_admin: true,
              is_module_admin: true,
              required_ad_role: 'traefik_admin',
            },
            loadingUser: false,
            isEmbedded: false,
            reloadUser: async () => undefined,
          }}
        >
          <StandaloneShell onLogout={() => undefined} />
        </ModuleUserProvider>
      </MemoryRouter>
    );
    expect(standaloneHtml).toContain('code-tpl-topbar-actions');
    expect(standaloneHtml).toContain('notification-bell-button');
  });
});
