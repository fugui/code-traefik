import { AUTH_TOKEN_KEY, createApiClient, type ApiFetchOptions } from '@code/common';
import { MODULE_BASE_PATH } from '../constants';

/**
 * 动态识别当前运行环境：
 * - 当嵌入在 CodeBench 宿主内运行时（window.__POWERED_BY_PORTAL__ === true），请求走 /<module_key>/api，由宿主网关反代并剥离前缀；
 * - 当本地独立调试运行时，直接请求 /api。
 */
const getBaseUrl = (): string => {
  if (typeof window !== 'undefined' && Boolean(window.__POWERED_BY_PORTAL__)) {
    return `${MODULE_BASE_PATH}/api`;
  }
  return '/api';
};

const client = createApiClient(getBaseUrl, AUTH_TOKEN_KEY);

/**
 * 类型安全的模块内部 API 请求封装
 */
export async function apiRequest<T>(endpoint: string, options: ApiFetchOptions = {}): Promise<T> {
  return (await client.apiFetch(endpoint, options)) as T;
}

/**
 * 跨模块调用 CodeBench 宿主公共底座 API（如 /api/departments、/api/repos 等）
 * 通过携带 'X-Portal-Request: true' 请求头，告知 fetchInterceptor 不要添加子应用前缀
 */
export async function portalApiRequest<T>(portalEndpoint: string, options: RequestInit = {}): Promise<T> {
  const token = localStorage.getItem(AUTH_TOKEN_KEY);
  const headers: Record<string, string> = {
    'X-Portal-Request': 'true',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  };

  if (options.headers) {
    const incoming = new Headers(options.headers);
    incoming.forEach((val, key) => {
      headers[key] = val;
    });
  }

  const res = await fetch(portalEndpoint, {
    ...options,
    headers,
  });

  if (res.status === 204) {
    return undefined as T;
  }

  const data = (await res.json().catch(() => ({}))) as Record<string, unknown>;
  if (!res.ok) {
    const errMsg = typeof data.error === 'string' ? data.error : '调用门户接口失败';
    throw new Error(errMsg);
  }
  return data as T;
}
