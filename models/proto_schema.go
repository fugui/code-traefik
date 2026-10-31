package models

import (
	"time"

	"code-traefik/config"
)

// 8. ProtoSchema Protobuf 编译描述集 (.pb FileDescriptorSet) 多版本仓库表 (traefik_proto_schemas)
type ProtoSchema struct {
	ID              uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Name            string    `gorm:"size:128;not null;uniqueIndex:uk_traefik_proto_name_ver,priority:1" json:"name"`
	Version         int       `gorm:"not null;uniqueIndex:uk_traefik_proto_name_ver,priority:2" json:"version"` // 同名递增版本号 1, 2, 3...
	VersionTag      string    `gorm:"size:64;not null;default:''" json:"version_tag"`
	FileName        string    `gorm:"size:255;not null" json:"file_name"` // 经 filepath.Base 净化的展示文件名
	FileSHA256      string    `gorm:"size:64;not null;index:idx_traefik_proto_sha" json:"file_sha256"`
	FileSize        int       `gorm:"not null;check:chk_traefik_proto_size,file_size > 0 AND file_size <= 4194304" json:"file_size"` // <= 4MB
	DescriptorBytes []byte    `gorm:"type:bytea;not null" json:"-"`                                                                  // 二进制仅入库
	IsActive        bool      `gorm:"not null;default:false;index:idx_traefik_proto_active" json:"is_active"`
	OperationCount  int       `gorm:"not null;default:0" json:"operation_count"`
	Description     string    `gorm:"type:text" json:"description"`
	CreatedBy       uint      `gorm:"not null;default:0" json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (ProtoSchema) TableName() string { return config.TablePrefix + "proto_schemas" }

// 9. ProtoOperation 描述集解析出的 CROS RPC 服务方法映射表 (traefik_proto_operations)
type ProtoOperation struct {
	ID                 uint64       `gorm:"primaryKey;autoIncrement" json:"id"`
	SchemaID           uint64       `gorm:"not null;uniqueIndex:uk_traefik_proto_op_schema,priority:1;index:idx_traefik_proto_op_sid" json:"schema_id"`
	Schema             *ProtoSchema `gorm:"foreignKey:SchemaID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
	ProtoFile          string       `gorm:"size:255;not null" json:"proto_file"`
	ProtoPackage       string       `gorm:"size:128;not null" json:"proto_package"`                                                                                               // 原样点分结构，如 "dophi.order.v1"
	ProtoService       string       `gorm:"size:128;not null" json:"proto_service"`                                                                                               // Service 简称，如 "TradeService"
	ServiceName        string       `gorm:"size:128;not null;uniqueIndex:uk_traefik_proto_op_schema,priority:2;index:idx_traefik_proto_op_lookup,priority:1" json:"service_name"` // "{package}.{serviceName}"
	Operation          string       `gorm:"size:128;not null;uniqueIndex:uk_traefik_proto_op_schema,priority:3;index:idx_traefik_proto_op_lookup,priority:2" json:"operation"`    // Method 名
	ReqMessageFullName string       `gorm:"size:255;not null" json:"req_message_full_name"`
	RspMessageFullName string       `gorm:"size:255;not null" json:"rsp_message_full_name"`
	ClientStreaming    bool         `gorm:"not null;default:false" json:"client_streaming"`
	ServerStreaming    bool         `gorm:"not null;default:false" json:"server_streaming"`
	IsActive           bool         `gorm:"not null;default:false;index:idx_traefik_proto_op_active" json:"is_active"`
	CreatedAt          time.Time    `json:"created_at"`
}

func (ProtoOperation) TableName() string { return config.TablePrefix + "proto_operations" }
