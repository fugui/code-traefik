export type ItemStatus = 'active' | 'draft' | 'archived';
export type ItemPriority = 'high' | 'medium' | 'low';

export interface ItemOwnerSummary {
  id: number;
  name: string;
  username?: string;
  employee_id?: string;
}

export interface TemplateItem {
  id: number;
  code: string;
  name: string;
  category: string;
  status: ItemStatus;
  priority: ItemPriority;
  description: string;
  owner_id: number | null;
  owner?: ItemOwnerSummary | null;
  created_by: number;
  created_at: string;
  updated_at: string;
}

export interface ItemUpsertPayload {
  code: string;
  name: string;
  category: string;
  status: ItemStatus;
  priority: ItemPriority;
  description: string;
  owner_id: number | null;
}

export interface PaginatedItemsResponse {
  items: TemplateItem[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
}

export interface ItemStatsResponse {
  total: number;
  active: number;
  draft: number;
  archived: number;
}

export interface HealthStatusResponse {
  status: string;
  service: string;
  module_key: string;
  version: string;
  timestamp: string;
}

export interface CurrentUserProfile {
  id: number;
  username: string;
  name: string;
  employee_id?: string;
  department_id?: number | null;
  department_name?: string;
  roles: string[];
  is_super_admin: boolean;
  is_module_admin: boolean;
  required_ad_role: string;
}

export interface ModuleSettingItem {
  id?: number;
  setting_key: string;
  setting_val: string;
  description: string;
  updated_by?: string;
  updated_at?: string;
}

export interface ModuleSettingsResponse {
  settings: ModuleSettingItem[];
}
