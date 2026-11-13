package models

import (
	"time"

	"code-traefik/config"
	"gorm.io/datatypes"
)

const (
	DocSymbolService = "SERVICE"
	DocSymbolMethod  = "METHOD"
	DocSymbolMessage = "MESSAGE"
	DocSymbolEnum    = "ENUM"
)

// 24. ProtoDocSymbol 活体接口参考文档符号与注释元数据表 (traefik_proto_doc_symbols, F-18 / M23)
type ProtoDocSymbol struct {
	ID                  uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	SchemaID            uint64         `gorm:"not null;uniqueIndex:uk_traefik_pdoc_schema_kind_full,priority:1;index:idx_traefik_pdoc_schema" json:"schema_id"`
	Schema              *ProtoSchema   `gorm:"foreignKey:SchemaID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
	SymbolKind          string         `gorm:"size:16;not null;uniqueIndex:uk_traefik_pdoc_schema_kind_full,priority:2;check:chk_traefik_pdoc_kind,symbol_kind IN ('SERVICE','METHOD','MESSAGE','ENUM')" json:"symbol_kind"`
	FullName            string         `gorm:"size:255;not null;uniqueIndex:uk_traefik_pdoc_schema_kind_full,priority:3;index:idx_traefik_pdoc_fullname" json:"full_name"`
	PackageName         string         `gorm:"size:128;not null;index:idx_traefik_pdoc_pkg" json:"package_name"`
	ShortName           string         `gorm:"size:128;not null" json:"short_name"`
	ProtoFile           string         `gorm:"size:255;not null" json:"proto_file"`
	StartLine           int            `gorm:"not null;default:0" json:"start_line"`
	EndLine             int            `gorm:"not null;default:0" json:"end_line"`
	LeadingComments     string         `gorm:"type:text;not null;default:''" json:"leading_comments"`
	TrailingComments    string         `gorm:"type:text;not null;default:''" json:"trailing_comments"`
	Summary             string         `gorm:"size:512;not null;default:''" json:"summary"`
	IsDeprecated        bool           `gorm:"not null;default:false;index:idx_traefik_pdoc_deprecated" json:"is_deprecated"`
	DeprecationReason   string         `gorm:"size:512;not null;default:''" json:"deprecation_reason"`
	OwnerTag            string         `gorm:"size:128;not null;default:''" json:"owner_tag"`
	ErrorCodesJSON      datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"error_codes_json"`
	FieldsMetaJSON      datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"fields_meta_json"`
	DefaultRequestJSON  datatypes.JSON `gorm:"type:jsonb;not null;default:'null'" json:"default_request_json"`
	DefaultResponseJSON datatypes.JSON `gorm:"type:jsonb;not null;default:'null'" json:"default_response_json"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
}

func (ProtoDocSymbol) TableName() string { return config.TablePrefix + "proto_doc_symbols" }
