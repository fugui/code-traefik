export type ProtoCategory = 'BASE_LIB' | 'BUSINESS' | 'GATEWAY';

export interface ResolvedSchemaDependencyDTO {
  schema_id?: number | string;
  schema_name: string;
  version?: number;
  category?: string;
  matched_protos: string[];
  status: 'READY' | 'MISSING';
}

export interface ProtoPoolSummaryDTO {
  active_schema_count: number;
  active_base_lib_count: number;
  active_service_count: number;
  active_operation_count: number;
  claimed_service_count: number;
  orphan_service_count: number;
  claimed_ratio: number;
  unresolved_dep_count: number;
}

export interface ProtoSchemaLedgerItemDTO {
  id: string | number;
  name: string;
  version: number;
  version_tag?: string;
  category: ProtoCategory;
  file_name: string;
  file_sha256?: string;
  file_size: number;
  is_active: boolean;
  proto_file_count: number;
  service_count: number;
  operation_count: number;
  proto_files: string[];
  external_imports?: string[];
  description?: string;
  created_by?: number;
  created_by_name?: string;
  created_at?: string;
  updated_at?: string;
  version_count: number;
  latest_version: number;
  active_version?: number;
  claimed_service_count: number;
  claimed_ratio: number;
  resolved_dependencies: ResolvedSchemaDependencyDTO[];
}

// 兼容别名与已有代码引用
export interface ProtoSchema extends Partial<ProtoSchemaLedgerItemDTO> {
  id: string;
  name: string;
  version: number;
  is_active: boolean;
  operations_count?: number;
}

export interface ProtoSchemaLedgerListResponse {
  items: ProtoSchemaLedgerItemDTO[];
  pool_summary: ProtoPoolSummaryDTO;
  total: number;
  page: number;
  page_size: number;
}

export interface ProtoOperation {
  id: string;
  schema_id: string;
  proto_file: string;
  proto_package: string;
  proto_service: string;
  service_name: string;
  operation: string;
  req_message_full_name?: string;
  rsp_message_full_name?: string;
  client_streaming?: boolean;
  server_streaming?: boolean;
  is_active: boolean;
  diff_ignore_paths?: string[];
  created_at?: string;
}

export interface ConflictItem {
  service_name: string;
  operation: string;
  conflict_schema_name: string;
  conflict_version: number;
}

export interface SchemaConflict extends ConflictItem {}

export interface BrokenDependentItemDTO {
  schema_id: number;
  schema_name: string;
  version: number;
  reason: string;
}

export interface BreakingRuleViolation {
  rule: string;
  proto_file: string;
  symbol: string;
  message: string;
  service_name?: string;
}

export interface AffectedDependent {
  module_id: number;
  module_code: string;
  module_name: string;
  arch_process_code: string;
  owner_name: string;
}

export interface ConflictOrBrokenResponse {
  error: string;
  code?: string;
  conflicts?: ConflictItem[];
  broken_dependents?: BrokenDependentItemDTO[];
  breaking_rules?: BreakingRuleViolation[];
  affected_dependents?: AffectedDependent[];
}

export interface ClaimedModuleDTO {
  module_id: number | string;
  module_name: string;
  subsystem_id?: number | string;
  subsystem_name?: string;
}

export interface SourceSchemaDTO {
  id: number | string;
  name: string;
  version: number;
  category: string;
  is_active: boolean;
}

export type { ServiceContractGroupDTO } from './schema_tree';
