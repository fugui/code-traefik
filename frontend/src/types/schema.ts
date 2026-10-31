export interface ProtoSchema {
  id: string;
  name: string;
  version: number;
  version_tag?: string;
  file_name?: string;
  file_sha256?: string;
  file_size?: number;
  is_active: boolean;
  description?: string;
  operations_count?: number;
  operation_count?: number;
  proto_files?: string[];
  created_at?: string;
  updated_at?: string;
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

export interface SchemaConflict {
  service_name: string;
  operation: string;
  conflict_schema_name: string;
  conflict_version: number;
}
