import { describe, it, expect } from 'vitest';
import { renderToString } from 'react-dom/server';
import { MemoryRouter } from 'react-router-dom';
import { traefikMenuConfig, DEFAULT_ENTRY_PATH, DEFAULT_STANDALONE_PATH } from '../menu';
import { EmbeddedRoutes } from '../App';

function getRedirectTarget(html: string): string | null {
  const match = html.match(/data-testid="legacy-redirect"[^>]*data-target="([^"]+)"/);
  return match ? match[1].replace(/&amp;/g, '&') : null;
}

export function testMenuStructure_FourGroupsNineItems(): void {
  // 1. 严格包含 全局概览、契约核验与回归、架构与契约、环境与治理 4 个分组
  expect(traefikMenuConfig.groups.length).toBe(4);
  expect(traefikMenuConfig.groups[0].title).toBe('全局概览');
  expect(traefikMenuConfig.groups[1].title).toBe('契约核验与回归');
  expect(traefikMenuConfig.groups[2].title).toBe('架构与契约');
  expect(traefikMenuConfig.groups[3].title).toBe('环境与治理');

  const allItems = traefikMenuConfig.groups.flatMap((g) => g.items);
  // 严格共 9 个子项（第 10 项 /golden-suites 留待 M15 挂载，严禁在 M14 暴露空壳）
  expect(allItems.length).toBe(9);

  const group0Paths = traefikMenuConfig.groups[0].items.map((i) => i.path);
  expect(group0Paths).toEqual(['/dashboard']);
  expect(traefikMenuConfig.groups[0].items[0].label).toBe('治理大盘');

  const group1Paths = traefikMenuConfig.groups[1].items.map((i) => i.path);
  expect(group1Paths).toEqual(['/record', '/debug', '/replay']);
  expect(traefikMenuConfig.groups[1].items[0].label).toBe('契约采集');
  expect(traefikMenuConfig.groups[1].items[1].label).toBe('事务分析');
  expect(traefikMenuConfig.groups[1].items[2].label).toBe('防劣化回归');

  const group2Paths = traefikMenuConfig.groups[2].items.map((i) => i.path);
  expect(group2Paths).toEqual(['/topology', '/architecture', '/schemas']);
  expect(traefikMenuConfig.groups[2].items[0].label).toBe('架构拓扑');
  expect(traefikMenuConfig.groups[2].items[1].label).toBe('架构元素');
  expect(traefikMenuConfig.groups[2].items[2].label).toBe('Proto 契约');

  const group3Paths = traefikMenuConfig.groups[3].items.map((i) => i.path);
  expect(group3Paths).toEqual(['/agents', '/admin/settings']);
  expect(traefikMenuConfig.groups[3].items[0].label).toBe('Agent 与进程');
  expect(traefikMenuConfig.groups[3].items[1].label).toBe('模块参数配置');

  // 严禁存在 /golden-suites 空壳项
  expect(allItems.some((i) => i.path === '/golden-suites')).toBe(false);

  // 2. 模块根默认路径 path === '/traefik/dashboard'
  expect(DEFAULT_ENTRY_PATH).toBe('/traefik/dashboard');
  expect(traefikMenuConfig.defaultPath).toBe('/traefik/dashboard');
  expect(DEFAULT_STANDALONE_PATH).toBe('/dashboard');

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

  // 3. / 自动落地至 /dashboard (嵌入模式下为 /traefik/dashboard)
  {
    const html = renderToString(
      <MemoryRouter initialEntries={['/']}>
        <EmbeddedRoutes />
      </MemoryRouter>
    );
    expect(getRedirectTarget(html)).toBe('/traefik/dashboard');
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
  it('TC-MENU-01: 四组九项菜单结构与 /dashboard 默认落地路径校验', () => {
    testMenuStructure_FourGroupsNineItems();
  });

  it('TC-MENU-02: 历史旧路由带参无损平滑重定向校验', () => {
    testLegacyDeepLinks_LosslessRedirect();
  });
});
