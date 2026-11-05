import type { SubMenuItem, MenuGroup, ModuleMenuConfig } from '@code/common';
import { MODULE_KEY, MODULE_TITLE, MODULE_BASE_PATH } from './constants';

export type { SubMenuItem, MenuGroup, ModuleMenuConfig };

export interface TraefikMenuConfig extends ModuleMenuConfig {
  defaultPath?: string;
}

export const DEFAULT_ENTRY_PATH = `${MODULE_BASE_PATH}/dashboard`;
export const DEFAULT_STANDALONE_PATH = '/dashboard';

// 标准侧边栏菜单契约（由 CodeBench 宿主在运行时通过 remoteEntry.js -> ./menu 动态拉取）
// 按照 ADR-14 升维为「全局概览 ➔ 契约核验与回归 ➔ 架构与契约 ➔ 环境与治理」4 组 9 项金字塔
// 第 10 项 /golden-suites 留待 M15 挂载，严禁在 M14 暴露空壳页
export const traefikMenuConfig: TraefikMenuConfig = {
  moduleKey: MODULE_KEY,
  moduleName: MODULE_TITLE,
  defaultPath: DEFAULT_ENTRY_PATH,
  groups: [
    {
      title: '全局概览',
      items: [
        {
          path: '/dashboard',
          label: '治理大盘',
          icon: 'M3 13h8V3H3v10zm0 8h8v-6H3v6zm10 0h8V11h-8v10zm0-18v6h8V3h-8z',
        },
      ],
    },
    {
      title: '契约核验与回归',
      items: [
        {
          path: '/record',
          label: '契约采集',
          icon: 'M15 10l4.553-2.276A1 1 0 0121 8.618v6.764a1 1 0 01-1.447.894L15 14M5 18h8a2 2 0 002-2V8a2 2 0 00-2-2H5a2 2 0 00-2 2v8a2 2 0 002 2z',
        },
        {
          path: '/debug',
          label: '事务分析',
          icon: 'M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z',
        },
        {
          path: '/replay',
          label: '防劣化回归',
          icon: 'M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15',
        },
      ],
    },
    {
      title: '架构与契约',
      items: [
        {
          path: '/topology',
          label: '架构拓扑',
          icon: 'M13 10V3L4 14h7v7l9-11h-7z',
        },
        {
          path: '/architecture',
          label: '架构元素',
          icon: 'M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10',
        },
        {
          path: '/schemas',
          label: 'Proto 契约',
          icon: 'M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4',
        },
      ],
    },
    {
      title: '环境与治理',
      items: [
        {
          path: '/agents',
          label: 'Agent 与进程',
          icon: 'M5 12h14M12 5l7 7-7 7',
        },
        {
          path: '/admin/settings',
          label: '模块参数配置',
          icon: 'M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z M15 12a3 3 0 11-6 0 3 3 0 016 0z',
          adminOnly: true,
        },
      ],
    },
  ],
};

export const menuGroups: MenuGroup[] = traefikMenuConfig.groups;
export const menuItems: SubMenuItem[] = traefikMenuConfig.groups.flatMap((group) => group.items);

type MenuChangeListener = (config: ModuleMenuConfig) => void;
const listeners = new Set<MenuChangeListener>();
let currentConfig: ModuleMenuConfig = traefikMenuConfig;

export function subscribeMenuChanges(listener: MenuChangeListener): () => void {
  listeners.add(listener);
  listener(currentConfig);
  return () => {
    listeners.delete(listener);
  };
}

export function notifyMenuUpdated(nextConfig: ModuleMenuConfig): void {
  currentConfig = nextConfig;
  listeners.forEach((fn) => fn(currentConfig));
}

export default traefikMenuConfig;
