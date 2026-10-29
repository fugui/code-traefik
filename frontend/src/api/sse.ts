import { AUTH_TOKEN_KEY } from '@code/common';
import { MODULE_BASE_PATH } from '../constants';

export interface SSEMessageEvent<T = unknown> {
  event: string;
  data: T;
}

export type SSEEventListener<T = unknown> = (event: SSEMessageEvent<T>) => void;

export interface SSESubscribeOptions {
  sessionId?: string;
  taskId?: string;
  onMessage?: SSEEventListener;
  onError?: (err: Error) => void;
  onOpen?: () => void;
}

/**
 * 基于 fetch + ReadableStream 的 /api/events SSE 客户端
 * 严格使用 Authorization: Bearer Header 传递凭证，严禁在 URL Query 中传递 Token
 */
export function subscribeServerEvents(options: SSESubscribeOptions): () => void {
  const controller = new AbortController();
  const token = localStorage.getItem(AUTH_TOKEN_KEY) ?? '';

  const isEmbedded = typeof window !== 'undefined' && Boolean(window.__POWERED_BY_PORTAL__);
  const baseUrl = isEmbedded ? `${MODULE_BASE_PATH}/api/events` : '/api/events';

  const params = new URLSearchParams();
  if (options.sessionId) params.set('session_id', options.sessionId);
  if (options.taskId) params.set('task_id', options.taskId);

  const queryStr = params.toString();
  const fullUrl = queryStr ? `${baseUrl}?${queryStr}` : baseUrl;

  void (async () => {
    try {
      const response = await fetch(fullUrl, {
        method: 'GET',
        headers: {
          Accept: 'text/event-stream',
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
        signal: controller.signal,
      });

      if (!response.ok) {
        throw new Error(`SSE 连接失败: HTTP ${response.status}`);
      }

      options.onOpen?.();

      const reader = response.body?.getReader();
      if (!reader) {
        throw new Error('无法读取响应流');
      }

      const decoder = new TextDecoder('utf-8');
      let buffer = '';

      while (!controller.signal.aborted) {
        const { value, done } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });

        const parts = buffer.split('\n\n');
        buffer = parts.pop() ?? '';

        for (const part of parts) {
          if (!part.trim()) continue;
          let eventType = 'message';
          let dataStr = '';

          const lines = part.split('\n');
          for (const line of lines) {
            if (line.startsWith('event:')) {
              eventType = line.slice(6).trim();
            } else if (line.startsWith('data:')) {
              dataStr += line.slice(5).trim();
            }
          }

          if (dataStr) {
            let parsed: unknown = dataStr;
            try {
              parsed = JSON.parse(dataStr);
            } catch {
              // fallback raw string
            }
            options.onMessage?.({ event: eventType, data: parsed });
          }
        }
      }
    } catch (err: unknown) {
      if (!controller.signal.aborted) {
        options.onError?.(err instanceof Error ? err : new Error(String(err)));
      }
    }
  })();

  return () => {
    controller.abort();
  };
}
