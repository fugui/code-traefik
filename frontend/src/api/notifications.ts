import { apiRequest } from './client';
import type {
  NotificationDTO,
  NotificationListResponse,
  UnreadCountResponse,
} from '../types';

export async function listNotifications(params?: {
  page?: number;
  page_size?: number;
  is_read?: boolean;
  category?: string;
  severity?: string;
}): Promise<NotificationListResponse> {
  const qs = new URLSearchParams();
  if (params?.page) qs.set('page', String(params.page));
  if (params?.page_size) qs.set('page_size', String(params.page_size));
  if (params?.is_read !== undefined) qs.set('is_read', String(params.is_read));
  if (params?.category) qs.set('category', params.category);
  if (params?.severity) qs.set('severity', params.severity);
  const query = qs.toString();
  return apiRequest<NotificationListResponse>(`/notifications${query ? `?${query}` : ''}`);
}

export async function getUnreadNotificationCount(): Promise<UnreadCountResponse> {
  return apiRequest<UnreadCountResponse>('/notifications/unread-count');
}

export async function markNotificationRead(id: number): Promise<NotificationDTO> {
  return apiRequest<NotificationDTO>(`/notifications/${id}/read`, {
    method: 'POST',
  });
}

export async function markAllNotificationsRead(): Promise<{ message: string; updated_count: number }> {
  return apiRequest<{ message: string; updated_count: number }>('/notifications/read-all', {
    method: 'POST',
  });
}
