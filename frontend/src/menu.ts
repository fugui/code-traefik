import type { SubMenuItem, MenuGroup, ModuleMenuConfig } from '@code/common';
import { MODULE_KEY, MODULE_TITLE } from './constants';

export type { SubMenuItem, MenuGroup, ModuleMenuConfig };

// 标准侧边栏菜单契约（由 CodeBench 宿主在运行时通过 remoteEntry.js -> ./menu 动态拉取）
export const traefikMenuConfig: ModuleMenuConfig = {
  moduleKey: MODULE_KEY,
  moduleName: MODULE_TITLE,
  groups: [
    {
      title: '流量与架构',
      items: [
        {
          path: '/architecture',
          label: '架构中心与拓扑',
          icon: 'M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10',
        },
        {
          path: '/traffic',
          label: '协同录制与流量解剖',
          icon: 'M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z',
        },
        {
          path: '/replay',
          label: '跨Agent重放与回归',
          icon: 'M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15',
        },
        {
          path: '/schemas',
          label: 'Proto描述集工作台',
          icon: 'M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4',
        },
      ],
    },
    {
      title: '资源与治理',
      items: [
        {
          path: '/agents',
          label: 'Agent纳管与租约',
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
