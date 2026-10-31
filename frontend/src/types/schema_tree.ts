import type { ProtoOperation, ProtoSchema } from './schema';

export type ProtoFieldKind =
  | 'INT32' | 'INT64' | 'UINT32' | 'UINT64' | 'SINT32' | 'SINT64'
  | 'BOOL' | 'ENUM' | 'STRING' | 'BYTES'
  | 'FLOAT' | 'DOUBLE' | 'MESSAGE';

export interface ProtoFieldNodeDTO {
  name: string;                // Proto 原名字段，如 "server_time"
  jsonName?: string;           // JSON 序列化键，如 "serverTime"
  json_name?: string;
  tagNumber: number;           // 字段编号，如 1, 2
  tag_number?: number;
  kind: ProtoFieldKind | string; // 数据类型
  typeName?: string;           // 嵌套 Message 或 Enum 的类型全名，如 "dophi.common.Head"
  type_name?: string;
  cardinality: 'OPTIONAL' | 'REQUIRED' | 'REPEATED' | string;
  isMap: boolean;              // 是否为 map<K, V> 映射
  is_map?: boolean;
  comments?: string;           // 提取自 SourceCodeInfo 的注释
  fieldPath: string;           // 自动计算的规范点分/数组通配路径，如 "header.server_time", "items[*].id"
  field_path?: string;
  isIgnoredInDiff: boolean;    // 当前方法或服务级是否已配置忽略
  is_ignored_in_diff?: boolean;
  isTruncated?: boolean;       // 达到防环递归深度上限 12 层
  is_truncated?: boolean;
  children?: ProtoFieldNodeDTO[]; // 针对 MESSAGE 递归展开的子字段树
}

export interface OperationSchemaTreeDTO {
  operationId: string;
  operation_id?: string;
  serviceName: string;         // "{package}-{serviceName}"
  service_name?: string;
  operation: string;           // "DeductBalance"
  protoFile: string;           // "account.proto"
  proto_file?: string;
  clientStreaming?: boolean;
  client_streaming?: boolean;
  serverStreaming?: boolean;
  server_streaming?: boolean;
  diffIgnorePaths: string[];   // 当前方法生效的忽略路径清单
  diff_ignore_paths?: string[];
  inheritedIgnorePaths: string[]; // 继承自架构服务级的忽略清单
  inherited_ignore_paths?: string[];
  reqMessage?: {
    fullName: string;
    full_name?: string;
    fields: ProtoFieldNodeDTO[];
  };
  req_message?: {
    fullName: string;
    full_name?: string;
    fields: ProtoFieldNodeDTO[];
  };
  rspMessage?: {
    fullName: string;
    full_name?: string;
    fields: ProtoFieldNodeDTO[];
  };
  rsp_message?: {
    fullName: string;
    full_name?: string;
    fields: ProtoFieldNodeDTO[];
  };
}

export interface ServiceContractGroupDTO {
  serviceName: string;         // 聚合键: "{package}-{serviceName}" 或 "{service_name}"
  protoPackage: string;
  protoService: string;
  protoFile: string;
  claimedByModule?: {          // 架构血缘: 认领此服务的公共架构模块
    id: string;
    code: string;
    name: string;
    subsystemName?: string;
    subsystem_name?: string;
  };
  totalOperations: number;
  unaryCount: number;
  streamCount: number;
  isActive: boolean;
  operations: ProtoOperation[]; // 展开后的子表方法清单
}

export interface ServiceSummaryItemDTO {
  service_name: string;
  proto_file: string;
  proto_package: string;
  proto_service: string;
  claimed_module_id?: number;
  claimed_module_name?: string;
  claimed_subsystem_name?: string;
  is_claimed: boolean;
  operation_count: number;
  unary_count: number;
  stream_count: number;
}

export interface ProtoSchemaSummaryDTO {
  schema: ProtoSchema;
  proto_files: string[];
  services_summary: ServiceSummaryItemDTO[];
  claimed_ratio: number;
}

