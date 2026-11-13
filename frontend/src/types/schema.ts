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

export type ProtoSourceType = 'UPLOAD_PB' | 'ONLINE_COMPILE' | 'GIT_SYNC' | 'GIT_MR_AUTO';

export interface ProtoSchemaLedgerItemDTO {
  id: string | number;
  name: string;
  version: number;
  version_tag?: string;
  category: ProtoCategory;
  source_type?: ProtoSourceType;
  git_repo_id?: number;
  source_bundle_id?: number;
  source_commit_sha?: string;
  source_mr_iid?: number | null;
  service_name?: string;
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

export interface CompileProtoFileItem {
  path: string;
  content: string;
}

export interface CompileProtoSchemaPayload {
  name: string;
  version_tag?: string;
  category: ProtoCategory;
  include_dirs?: string[];
  files: CompileProtoFileItem[];
  auto_activate?: boolean;
  allow_breaking?: boolean;
  breaking_reason?: string;
  description?: string;
}

export interface CompileDiagnosticDTO {
  file: string;
  line: number;
  col: number;
  message: string;
}

export interface ProtoDependencyNodeDTO {
  id: number | string;
  name: string;
  version: number;
  category: ProtoCategory;
  source_type?: ProtoSourceType;
  proto_file_count: number;
  service_count: number;
  operation_count: number;
  is_active: boolean;
}

export interface ProtoDependencyEdgeDTO {
  from_schema_id: number | string;
  to_schema_id: number | string;
  matched_protos: string[];
}

export interface ProtoDependenciesGraphDTO {
  pool_summary?: ProtoPoolSummaryDTO;
  nodes: ProtoDependencyNodeDTO[];
  edges: ProtoDependencyEdgeDTO[];
}

export interface ConflictOrBrokenResponse {
  error: string;
  code?: string;
  conflicts?: ConflictItem[];
  broken_dependents?: BrokenDependentItemDTO[];
  breaking_rules?: BreakingRuleViolation[];
  affected_dependents?: AffectedDependent[];
  diagnostics?: CompileDiagnosticDTO[];
  missing_imports?: string[];
  service_name?: string;
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

// ==========================================
// M23: 活体接口参考文档中心与只读源码浏览器 DTO (F-18 / F-28)
// ==========================================

export interface ErrorCodeDocDTO {
  code: string;
  http_or_cros_code: string;
  description: string;
}

export interface EnumValueDocDTO {
  name: string;
  number: number;
  comment: string;
  is_deprecated: boolean;
  deprecation_reason?: string;
  start_line: number;
}

export interface EnumDocDTO {
  full_name: string;
  short_name: string;
  proto_file: string;
  start_line: number;
  end_line: number;
  comment: string;
  is_deprecated: boolean;
  deprecation_reason?: string;
  values: EnumValueDocDTO[];
}

export interface FieldDocNodeDTO {
  name: string;
  json_name?: string;
  number: number;
  type: string;
  type_full_name?: string;
  proto_file?: string;
  start_line?: number;
  label: string;
  is_map?: boolean;
  map_key_type?: string;
  map_value_type?: string;
  oneof_group?: string;
  oneof_comment?: string;
  comment: string;
  is_deprecated?: boolean;
  deprecation_reason?: string;
  enum_values?: EnumValueDocDTO[];
  truncated?: boolean;
  truncated_reason?: string;
  children?: FieldDocNodeDTO[];
}

export interface MessageDocMetaDTO {
  full_name: string;
  proto_file: string;
  start_line: number;
  end_line: number;
  comment: string;
  fields: FieldDocNodeDTO[];
  truncated?: boolean;
}

export interface GoldenSampleDTO {
  suite_id: number;
  suite_code: string;
  suite_name: string;
  item_id: number;
  order_no: number;
  decode_status: 'DECODED_JSON' | 'DECODE_ERROR' | 'NO_SCHEMA' | string;
  req_json?: unknown;
  rsp_json?: unknown;
  remark?: string;
}

export interface MethodDocDTO {
  operation: string;
  full_name: string;
  proto_file: string;
  start_line: number;
  end_line: number;
  summary: string;
  leading_comments: string;
  trailing_comments: string;
  is_deprecated: boolean;
  deprecation_reason: string;
  owner_tag: string;
  error_codes: ErrorCodeDocDTO[];
  req_message_full_name: string;
  rsp_message_full_name: string;
  client_streaming: boolean;
  server_streaming: boolean;
  req_fields: FieldDocNodeDTO[];
  rsp_fields: FieldDocNodeDTO[];
  default_req_json: unknown;
  default_rsp_json: unknown;
  golden_samples: GoldenSampleDTO[];
}

export interface ContractDefinerDTO {
  contract_id: number;
  service_name: string;
  definer_module_id: number;
  module_code: string;
  module_name: string;
  subsystem_id: number;
  subsystem_name: string;
  provide_mode: string;
  impl_key_label: string;
}

export interface ServiceImplementationDTO {
  service_id: number;
  module_id: number;
  module_code: string;
  module_name: string;
  subsystem_id: number;
  subsystem_name: string;
  arch_process_code: string;
  owner_name: string;
  impl_key: string;
  is_custom_service_name: boolean;
  service_name: string;
}

export interface ServiceConsumerDTO {
  module_id: number;
  module_code: string;
  module_name: string;
  subsystem_id: number;
  subsystem_name: string;
  arch_process_code: string;
  dep_type: string;
}

export interface ServiceContractLineageDTO {
  claim_status: 'CONTRACT' | 'SINGLE_IMPL' | 'UNCLAIMED' | string;
  definer?: ContractDefinerDTO;
  implementations: ServiceImplementationDTO[];
  consumers: ServiceConsumerDTO[];
}

export interface ProtoDocTreeNodeDTO {
  node_key: string;
  node_type: 'SUBSYSTEM' | 'SCHEMA' | 'PACKAGE' | 'SERVICE' | 'METHOD' | string;
  label: string;
  subsystem_id?: number;
  schema_id?: number;
  schema_name?: string;
  category?: string;
  service_name?: string;
  operation?: string;
  summary?: string;
  is_deprecated?: boolean;
  method_count?: number;
  deprecated_count?: number;
  has_source_info: boolean;
  children?: ProtoDocTreeNodeDTO[];
}

export interface ProtoDocTreeResponse {
  items: ProtoDocTreeNodeDTO[];
  total_services: number;
  total_methods: number;
  deprecated_count: number;
}

export interface ProtoServiceDocDetailDTO {
  schema_id: number;
  schema_name: string;
  schema_version: number;
  schema_category: string;
  source_type: string;
  has_source_info: boolean;
  package_name: string;
  service_name: string;
  short_name: string;
  proto_file: string;
  start_line: number;
  end_line: number;
  file_comments: string;
  summary: string;
  leading_comments: string;
  trailing_comments: string;
  is_deprecated: boolean;
  deprecation_reason: string;
  owner_tag: string;
  error_codes: ErrorCodeDocDTO[];
  methods: MethodDocDTO[];
  enums: EnumDocDTO[];
  referenced_types: MessageDocMetaDTO[];
  lineage: ServiceContractLineageDTO;
}

export interface ProtoSymbolAnchorDTO {
  symbol_kind: string;
  symbol_full_name: string;
  start_line: number;
  end_line: number;
}

export interface ReconstructedProtoFileDTO {
  file_id: number;
  schema_id: number;
  schema_name: string;
  schema_version: number;
  file_path: string;
  package_name: string;
  content: string;
  line_count: number;
  has_source_info: boolean;
  source_origin: string;
  symbol_anchors: ProtoSymbolAnchorDTO[];
}

export interface ProtoSchemaSourcesResponse {
  schema_id: number;
  schema_name: string;
  schema_version: number;
  source_type: string;
  has_source_info: boolean;
  source_origin: string;
  files: ReconstructedProtoFileDTO[];
}

export interface ProtoSourceViolationDTO {
  rule_code: string;
  severity: 'ERROR' | 'WARNING' | 'INFO' | string;
  message: string;
  line: number;
  column?: number;
}

export interface ProtoFileDTO {
  id: number;
  schema_id: number;
  target_schema_name: string;
  category: string;
  file_path: string;
  package_name: string;
  latest_version: number;
  is_active_schema: boolean;
  has_source_info: boolean;
  source_origin: string;
  line_count: number;
  updated_at: string;
}

export interface ProtoFileVersionDTO {
  schema_id: number;
  version: number;
  version_tag: string;
  commit_sha: string;
  content_sha256: string;
  source_type: string;
  author_name: string;
  summary: string;
  created_at: string;
  line_count: number;
  is_active: boolean;
}

export interface ProtoFileVersionSummaryDTO {
  schema_id: number;
  version: number;
  version_tag: string;
  commit_sha: string;
  content_sha256: string;
  source_type: string;
  author_name: string;
  summary: string;
  is_active: boolean;
  created_at: string;
}

export interface ProtoFileContentResponse {
  file: ProtoFileDTO;
  version: ProtoFileVersionDTO;
  content: string;
  violations: ProtoSourceViolationDTO[];
  symbol_anchors: ProtoSymbolAnchorDTO[];
}

export interface ProtoFileDiffResponse {
  file: ProtoFileDTO;
  from_version: ProtoFileVersionDTO;
  to_version: ProtoFileVersionDTO;
  unified_diff: string;
  added_lines: number;
  deleted_lines: number;
}
