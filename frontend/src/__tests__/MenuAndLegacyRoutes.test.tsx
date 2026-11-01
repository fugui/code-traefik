import { describe, it, expect } from 'vitest';
import { renderToString } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import { traefikMenuConfig, DEFAULT_ENTRY_PATH } from '../menu';
import { EmbeddedRoutes } from '../App';

function getRedirectTarget(html: string): string | null {
  const match = html.match(/data-testid="legacy-redirect"[^>]*data-target="([^"]+)"/);
  return match ? match[1].replace(/&amp;/g, '&') : null;
}

export function testMenuStructure_ThreeGroupsEightItems(): void {
  // 1. 严格包含 流量作业、架构与契约、环境与治理 3 个分组
  expect(traefikMenuConfig.groups.length).toBe(3);
  expect(traefikMenuConfig.groups[0].title).toBe('流量作业');
  expect(traefikMenuConfig.groups[1].title).toBe('架构与契约');
  expect(traefikMenuConfig.groups[2].title).toBe('环境与治理');

  const allItems = traefikMenuConfig.groups.flatMap((g) => g.items);
  // 严格共 8 个子项
  expect(allItems.length).toBe(8);

  const group0Paths = traefikMenuConfig.groups[0].items.map((i) => i.path);
  expect(group0Paths).toEqual(['/record', '/debug', '/replay']);

  const group1Paths = traefikMenuConfig.groups[1].items.map((i) => i.path);
  expect(group1Paths).toEqual(['/topology', '/architecture', '/schemas']);

  const group2Paths = traefikMenuConfig.groups[2].items.map((i) => i.path);
  expect(group2Paths).toEqual(['/agents', '/admin/settings']);

  // 2. 模块根默认路径 path === '/traefik/record'
  expect(DEFAULT_ENTRY_PATH).toBe('/traefik/record');
  expect(traefikMenuConfig.defaultPath).toBe('/traefik/record');

  // 仅 /admin/settings (或 /traefik/admin/settings) 标记 adminOnly: true
  allItems.forEach((item) => {
    if (item.path === '/admin/settings' || item.path === '/traefik/admin/settings') {
      expect(item.adminOnly).toBe(true);
    } else {
      expect(Boolean(item.adminOnly)).toBe(false);
    }
  });
}

export function testLegacyDeepLinks_LosslessRedirect(): void {
  // 1. /traffic?session_id=s1&trace_id=t9 自动重定向至 /debug?session_id=s1&trace_id=t9（参数零丢失）
  {
    const html = renderToString(
      <MemoryRouter initialEntries={['/traffic?session_id=s1&trace_id=t9']}>
        <EmbeddedRoutes />
      </MemoryRouter>
    );
    expect(getRedirectTarget(html)).toBe('/traefik/debug?session_id=s1&trace_id=t9');
  }

  // 2. /architecture?tab=physical&session_id=s1 重定向至 /topology?view=PHYSICAL&session_id=s1
  {
    const html = renderToString(
      <MemoryRouter initialEntries={['/architecture?tab=physical&session_id=s1']}>
        <EmbeddedRoutes />
      </MemoryRouter>
    );
    expect(getRedirectTarget(html)).toBe('/traefik/topology?session_id=s1&view=PHYSICAL');
  }

  // 3. / 自动落地至 /record (嵌入模式下为 /traefik/record)
  {
    const html = renderToString(
      <MemoryRouter initialEntries={['/']}>
        <EmbeddedRoutes />
      </MemoryRouter>
    );
    expect(getRedirectTarget(html)).toBe('/traefik/record');
  }

  // 4. /sessions?page=1 自动落地至 /record (保留 query 参数)
  {
    const html = renderToString(
      <MemoryRouter initialEntries={['/sessions?page=1']}>
        <EmbeddedRoutes />
      </MemoryRouter>
    );
    expect(getRedirectTarget(html)).toBe('/traefik/record?page=1');
  }
}

describe('MenuAndLegacyRoutes Test Suite', () => {
  it('TC-MENU-01: 三层菜单结构与默认路径校验', () => {
    testMenuStructure_ThreeGroupsEightItems();
  });

  it('TC-MENU-02: 历史旧路由带参无损平滑重定向校验', () => {
    testLegacyDeepLinks_LosslessRedirect();
  });
});
