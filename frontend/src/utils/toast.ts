import { useToast as useCommonToast } from '@code/common';

export function useToast() {
  const { showToast } = useCommonToast();
  return {
    showToast,
    success: (message: string) => showToast(message, 'success'),
    error: (message: string) => showToast(message, 'error'),
    info: (message: string) => showToast(message, 'info'),
    warning: (message: string) => showToast(message, 'warning'),
  };
}
