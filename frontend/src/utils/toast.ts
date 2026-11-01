import { useCallback, useMemo } from 'react';
import { useToast as useCommonToast } from '@code/common';

export function useToast() {
  let commonToast: ((message: string, type?: 'info' | 'success' | 'warning' | 'error') => void) | null = null;
  try {
    const common = useCommonToast();
    commonToast = common.showToast;
  } catch {
    // 允许在无 ToastProvider 包装的轻量 SSR 单测环境下优雅降级
    commonToast = null;
  }

  const showToast = useCallback(
    (message: string, type?: 'info' | 'success' | 'warning' | 'error') => {
      if (commonToast) {
        commonToast(message, type);
      }
    },
    [commonToast]
  );

  return useMemo(
    () => ({
      showToast,
      success: (message: string) => showToast(message, 'success'),
      error: (message: string) => showToast(message, 'error'),
      info: (message: string) => showToast(message, 'info'),
      warning: (message: string) => showToast(message, 'warning'),
    }),
    [showToast]
  );
}

export default useToast;
