import { apiRequest } from './client';
import type { DashboardOverviewDTO } from '../types/dashboard';

export async function fetchDashboardOverview(subsystemId?: number): Promise<DashboardOverviewDTO> {
  const query = subsystemId ? `?subsystem_id=${subsystemId}` : '';
  return apiRequest<DashboardOverviewDTO>(`/dashboard/overview${query}`);
}
