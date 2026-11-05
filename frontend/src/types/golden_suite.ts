import type { ArchProcess } from './arch';

export interface MaskRule {
  path: string;
  strategy: 'IGNORE' | 'REGEX' | 'FIXED';
  pattern?: string;
}

export interface GoldenSuiteItemSummary {
  id: number;
  suite_id: number;
  order_no: number;
  name: string;
  source_session_id?: number;
  source_record_id?: number;
  service_name: string;
  operation: string;
  baseline_status: string;
  rsp_seg_count: number;
  raw_req_len: number;
  raw_rsp_len: number;
  scoped_mock_count: number;
  egress_endpoints?: Array<{ ip: string; port: number }>;
  mask_rules_json?: MaskRule[];
  created_by?: number;
  created_at?: string;
  updated_at?: string;
}

export interface GoldenSuite {
  id: number;
  code: string;
  name: string;
  arch_process_id: number;
  arch_process?: ArchProcess;
  version: string;
  status: 'ACTIVE' | 'ARCHIVED';
  extra_ignore_paths?: string[];
  item_count: number;
  description?: string;
  items?: GoldenSuiteItemSummary[];
  created_by?: number;
  updated_by?: number;
  created_at?: string;
  updated_at?: string;
  last_replay_status?: 'PASS' | 'FAILED' | 'MOCK_MISS' | 'TIMEOUT' | 'ERROR';
  last_replay_at?: string;
}

export interface CurateGoldenSuitePayload {
  suite_id?: number;
  new_suite?: {
    code: string;
    name: string;
    arch_process_id: number;
    version?: string;
    extra_ignore_paths?: string[];
    description?: string;
  };
  session_id: number;
  record_ids: number[];
  mask_rules_json?: MaskRule[];
}

export interface CurateGoldenSuiteResponse {
  suite: GoldenSuite;
  added_items: number;
}

export interface GoldenSuiteDetailResponse {
  suite: GoldenSuite;
  items: GoldenSuiteItemSummary[];
}

export interface GoldenSuiteListResponse {
  items: GoldenSuite[];
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
}
