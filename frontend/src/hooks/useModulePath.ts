import { useCallback } from 'react';
import { MODULE_BASE_PATH } from '../constants';
import { useModuleUser } from '../context/ModuleUserContext';

/**
 * 解析模块内部跳转路径：
 * - 嵌入宿主门户（isEmbedded=true）时，宿主路由以 /<module_key> 为挂载前缀，需拼接 MODULE_BASE_PATH（如 /traefik/debug）；
 * - 独立开发调试模式下直接使用模块内相对根路径（如 /debug）。
 */
export function resolveModulePath(path: string, isEmbedded: boolean): string {
  return isEmbedded ? `${MODULE_BASE_PATH}${path}` : path;
}

/** 返回绑定当前运行模式的模块内路径解析函数，供 Link / navigate 统一使用 */
export function useModulePath(): (path: string) => string {
  const { isEmbedded } = useModuleUser();
  return useCallback((path: string) => resolveModulePath(path, isEmbedded), [isEmbedded]);
}

