import { describe, it, expect } from 'vitest';
import { renderToString } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import type { RecordingSession } from '../types';
import { ActiveRecordingBanner } from '../pages/RecordingWorkbenchPage/ActiveRecordingBanner';
import { RecordingSessionTable } from '../pages/RecordingWorkbenchPage/RecordingSessionTable';
import { ModuleUserProvider } from '../context/ModuleUserContext';
import { resolveModulePath } from '../hooks/useModulePath';

export function testSessionLedger_ActionsAndCrossWorkbenchLinks(): void {
  const activeSession: RecordingSession = {
    id: 'active-1',
    session_no: 'REC-2026-ACTIVE',
    name: '在线全链路协同录制',
    scope: 'SHARED',
    status: 'RECORDING',
    target_process_ids: ['p1', 'p2'],
    max_duration_sec: 1800,
    max_frames: 100000,
    captured_frames: 8888,
    process_diagnostics: [
      {
        process_id: 'p1',
        process_name: 'OrderServer',
        pid: 1234,
        listen_port: 9091,
        established_conns_count: 3,
        need_reconnect: true,
      },
    ],
  };

  const stoppedSessions: RecordingSession[] = [
    {
      id: 'sess-shared',
      session_no: 'REC-SHARED-01',
      name: '公共基线素材',
      scope: 'SHARED',
      status: 'COMPLETED',
      owner_id: '999',
      owner_name: '王五',
      target_process_ids: ['p1'],
      max_duration_sec: 1800,
      max_frames: 100000,
      captured_frames: 1200,
      created_at: '2026-11-01T08:00:00Z',
    },
    {
      id: 'sess-private-me',
      session_no: 'REC-PRIVATE-02',
      name: '本人私有调试素材',
      scope: 'PRIVATE',
      owner_id: '100',
      owner_name: '张三',
      status: 'COMPLETED',
      target_process_ids: ['p2'],
      max_duration_sec: 1800,
      max_frames: 100000,
      captured_frames: 350,
      created_at: '2026-11-01T08:10:00Z',
    },
    {
      id: 'sess-private-other',
      session_no: 'REC-PRIVATE-03',
      name: '他人私有素材',
      scope: 'PRIVATE',
      owner_id: '999',
      owner_name: '李四',
      status: 'COMPLETED',
      target_process_ids: ['p3'],
      max_duration_sec: 1800,
      max_frames: 100000,
      captured_frames: 420,
      created_at: '2026-11-01T08:20:00Z',
    },
  ];

  const noop = () => {};
  const asyncNoop = async () => {};

  // 1. 顶部 ActiveRecordingBanner 展示活跃批次帧数与【实时跟流调试】链接（指向 /debug?session_id=active&live=1）
  const bannerHtml = renderToString(
    <MemoryRouter>
      <ActiveRecordingBanner session={activeSession} onStop={asyncNoop} />
    </MemoryRouter>
  );
  expect(bannerHtml).toContain('8888 帧');
  expect(bannerHtml).toContain('/debug?session_id=active-1&amp;live=1');
  expect(bannerHtml).toContain('实时跟流调试');
  expect(bannerHtml).toContain('停止录制');
  // A-01 存量长连接提示
  expect(bannerHtml).toContain('A-01');
  expect(bannerHtml).toContain('OrderServer');

  // 2. 台账行 [去调试]、[发起重放]、[架构验证] 链接格式断言
  const tableHtml = renderToString(
    <MemoryRouter>
      <RecordingSessionTable
        sessions={stoppedSessions}
        total={3}
        page={1}
        pageSize={25}
        onPageChange={noop}
        onPageSizeChange={noop}
        scopeFilter=""
        onScopeFilterChange={noop}
        search=""
        onSearchChange={noop}
        currentUserId={100}
        isAdmin={false}
        onPublish={asyncNoop}
        onDelete={asyncNoop}
      />
    </MemoryRouter>
  );

  // 校验跨工作台跳转链接
  expect(tableHtml).toContain('/debug?session_id=sess-shared');
  expect(tableHtml).toContain('/replay?session_id=sess-shared&amp;auto_create=1');
  expect(tableHtml).toContain('/topology?session_id=sess-shared');

  // 3. 普通用户仅对本人 PRIVATE 批次可见 [发布共享] 与 [删除]
  // 对于 sess-private-me (owner_id=100, currentUserId=100):
  expect(tableHtml).toContain('data-testid="action-publish-sess-private-me"');
  expect(tableHtml).toContain('data-testid="action-delete-sess-private-me"');

  // 对于 sess-shared: 普通用户不可发布、不可删除
  expect(tableHtml).not.toContain('data-testid="action-publish-sess-shared"');
  expect(tableHtml).not.toContain('data-testid="action-delete-sess-shared"');

  // 对于 sess-private-other (他人私有素材): 普通用户不可发布、不可删除
  expect(tableHtml).not.toContain('data-testid="action-publish-sess-private-other"');
  expect(tableHtml).not.toContain('data-testid="action-delete-sess-private-other"');
}

export function testSessionLedger_EmbeddedModulePrefixLinks(): void {
  const session: RecordingSession = {
    id: 'sess-embed',
    session_no: 'REC-EMBED-001',
    name: '嵌入模式批次',
    scope: 'PRIVATE',
    owner_id: '100',
    status: 'COMPLETED',
    target_process_ids: ['p1'],
    max_duration_sec: 600,
    max_frames: 1000,
    captured_frames: 1,
  };
  const noop = () => {};
  const asyncNoop = async () => {};

  const html = renderToString(
    <ModuleUserProvider
      value={{ user: null, loadingUser: false, isEmbedded: true, reloadUser: asyncNoop }}
    >
      <MemoryRouter>
        <RecordingSessionTable
          sessions={[session]}
          total={1}
          page={1}
          pageSize={25}
          onPageChange={noop}
          onPageSizeChange={noop}
          scopeFilter=""
          onScopeFilterChange={noop}
          search=""
          onSearchChange={noop}
          currentUserId={100}
          isAdmin={false}
          onPublish={asyncNoop}
          onDelete={asyncNoop}
        />
      </MemoryRouter>
    </ModuleUserProvider>
  );

  // 嵌入宿主门户时，所有模块内跳转必须携带 /traefik 前缀
  expect(html).toContain('href="/traefik/debug?session_id=sess-embed"');
  expect(html).toContain('href="/traefik/replay?session_id=sess-embed&amp;auto_create=1"');
  expect(html).toContain('href="/traefik/topology?session_id=sess-embed"');
  expect(html).not.toContain('href="/debug');

  // 批次流水号可点击，直达该批次录制数据流
  expect(html).toContain('data-testid="session-no-link-sess-embed"');

  // 操作列统一为图标 + Tips 模式
  expect(html).toContain('data-tip="进入流量调试"');
  expect(html).toContain('data-tip="发布为团队共享素材"');
  expect(html).toContain('data-tip="删除此录制批次"');
}

describe('RecordingWorkbench Test Suite', () => {
  it('TC-RECORD-01: 活跃录制横幅、素材台账操作列与跨页深链校验', () => {
    testSessionLedger_ActionsAndCrossWorkbenchLinks();
  });

  it('TC-RECORD-02: 嵌入模式下批次流水号与快捷操作跳转携带模块前缀', () => {
    testSessionLedger_EmbeddedModulePrefixLinks();
  });

  it('TC-RECORD-03: resolveModulePath 按运行模式解析模块内路径', () => {
    expect(resolveModulePath('/debug?session_id=s1', true)).toBe('/traefik/debug?session_id=s1');
    expect(resolveModulePath('/debug?session_id=s1', false)).toBe('/debug?session_id=s1');
  });
});
