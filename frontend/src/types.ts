export * from './types/arch';
export * from './types/agent';
export * from './types/schema';
export * from './types/schema_tree';
export * from './types/traffic';
export * from './types/replay';

export interface PaginatedResponse<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
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
