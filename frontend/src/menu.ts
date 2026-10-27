import type { SubMenuItem, MenuGroup, ModuleMenuConfig } from '@code/common';
import { MODULE_KEY, MODULE_TITLE } from './constants';

export type { SubMenuItem, MenuGroup, ModuleMenuConfig };

// 标准侧边栏菜单契约（由 CodeBench 宿主在运行时通过 remoteEntry.js -> ./menu 动态拉取）
export const traefikMenuConfig: ModuleMenuConfig = {
  moduleKey: MODULE_KEY,
  moduleName: MODULE_TITLE,
  groups: [
    {
      title: '核心业务',
      items: [
        {
          path: '/dashboard',
          label: '概览看板',
          icon: 'M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-6 0a1 1 0 001-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 001 1m-6 0h6',
        },
        {
          path: '/items',
          label: '示例条目管理',
          icon: 'M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-6 9l2 2 4-4',
        },
      ],
    },
    {
      title: '模块治理',
      adminOnly: true, // 仅具备 <module_key>_admin 或 super_admin 角色的用户可见
      items: [
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

// 可选高阶能力：动态菜单变更订阅机制（当业务需要根据后端配置动态刷新宿主侧边栏时调用 notifyMenuUpdated）
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
