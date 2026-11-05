package models

import (
	"encoding/json"
	"time"

	"code-traefik/config"
	"gorm.io/datatypes"
)

const (
	GoldenSuiteStatusActive   = "ACTIVE"   // 权威生效态（参与 CI/CD 默认门禁匹配）
	GoldenSuiteStatusArchived = "ARCHIVED" // 已归档历史基线
)

// MaskRule 用例级动态掩码与忽略路径规则
type MaskRule struct {
	Path     string `json:"path"`              // 例如 "token", "payload.timestamp"
	Strategy string `json:"strategy"`          // "IGNORE" | "REGEX" | "FIXED"
	Pattern  string `json:"pattern,omitempty"` // 正则模式或替换固定值
}

// 16. GoldenSuite 黄金契约测试套件资产主表 (traefik_golden_suites, F-25, ADR-14)
type GoldenSuite struct {
	ID               uint64            `gorm:"primaryKey;autoIncrement" json:"id"`
	Code             string            `gorm:"size:64;not null;uniqueIndex:uk_traefik_golden_suite_code" json:"code"` // 全局唯一套件编码
	Name             string            `gorm:"size:128;not null;index:idx_traefik_golden_suite_name" json:"name"`
	ArchProcessID    uint64            `gorm:"not null;index:idx_traefik_golden_suite_arch_proc,priority:1" json:"arch_process_id"` // 绑定的标准架构进程蓝图 SSOT
	ArchProcess      *ArchProcess      `gorm:"foreignKey:ArchProcessID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"arch_process,omitempty"`
	Version          string            `gorm:"size:64;not null;default:'v1.0.0'" json:"version"`
	Status           string            `gorm:"size:24;not null;default:'ACTIVE';index:idx_traefik_golden_suite_arch_proc,priority:2;check:chk_traefik_golden_suite_status,status IN ('ACTIVE','ARCHIVED')" json:"status"`
	ExtraIgnorePaths datatypes.JSON    `gorm:"type:jsonb;not null;default:'[]'" json:"extra_ignore_paths"` // 套件级默认 Diff 忽略字段路径 []string
	ItemCount        int               `gorm:"not null;default:0" json:"item_count"`                       // 冗余用例总数，零 N+1 列表展示
	Description      string            `gorm:"type:text" json:"description"`
	Items            []GoldenSuiteItem `gorm:"foreignKey:SuiteID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"items,omitempty"`
	CreatedBy        uint              `gorm:"not null;default:0;index:idx_traefik_golden_suite_creator" json:"created_by"`
	UpdatedBy        uint              `gorm:"not null;default:0" json:"updated_by"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

func (GoldenSuite) TableName() string { return config.TablePrefix + "golden_suites" }

// 17. GoldenSuiteItem 黄金契约套件完全自包含事务快照表 (traefik_golden_suite_items, F-25, ADR-14)
// 关键防腐设计：完整深拷贝固化入站请求帧、基线应答帧及下游成对 ScopedMocks 闭包，
// 100% 免疫 traefik_traffic_records 30 天按日分区 DROP TABLE 物理清理与源 RecordingSession 删除！
type GoldenSuiteItem struct {
	ID              uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	SuiteID         uint64         `gorm:"not null;uniqueIndex:uk_traefik_golden_item_order,priority:1;index:idx_traefik_golden_item_suite" json:"suite_id"`
	Suite           *GoldenSuite   `gorm:"foreignKey:SuiteID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
	OrderNo         int            `gorm:"not null;uniqueIndex:uk_traefik_golden_item_order,priority:2" json:"order_no"` // 套件内严格串行执行序 (1, 2, 3...)
	Name            string         `gorm:"size:128;not null" json:"name"`                                                // 用例业务语义名称
	SourceSessionID *uint64        `gorm:"index:idx_traefik_golden_item_src_sess" json:"source_session_id,omitempty"`    // 可空溯源批次弱引用
	SourceRecordID  *uint64        `gorm:"index:idx_traefik_golden_item_src_rec" json:"source_record_id,omitempty"`      // 可空溯源流量行弱引用（绝不强依赖回查）
	ServiceName     string         `gorm:"size:128;not null;index:idx_traefik_golden_item_svc,priority:1" json:"service_name"`
	Operation       string         `gorm:"size:128;not null;index:idx_traefik_golden_item_svc,priority:2" json:"operation"`
	BaselineStatus  U64Numeric     `gorm:"type:numeric(20,0);not null;default:0" json:"baseline_status,string"`
	RspSegCount     int            `gorm:"not null;default:1" json:"rsp_seg_count"`
	RawReqPacket    []byte         `gorm:"type:bytea;not null" json:"-"`                             // 深拷贝固化的无损入站请求 CROS RPC 帧
	RawRspPacket    []byte         `gorm:"type:bytea;not null" json:"-"`                             // 深拷贝固化的无损预期应答 CROS RPC 帧
	ScopedMocks     datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"scoped_mocks"`     // []agentproto.MockRuleDownstream 固化的专属下游请求+应答闭包
	EgressEndpoints datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"egress_endpoints"` // []agentproto.DownstreamEndpoint 固化的历史下游端点集
	MaskRulesJSON   datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"mask_rules_json"`  // []MaskRule 用例级动态掩码与忽略路径规则
	CreatedBy       uint           `gorm:"not null;default:0" json:"created_by"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

func (GoldenSuiteItem) TableName() string { return config.TablePrefix + "golden_suite_items" }

// GoldenSuiteItemSummary 用例摘要 DTO（不透出 raw_*_packet 大字段）
type GoldenSuiteItemSummary struct {
	ID              uint64         `json:"id"`
	SuiteID         uint64         `json:"suite_id"`
	OrderNo         int            `json:"order_no"`
	Name            string         `json:"name"`
	SourceSessionID *uint64        `json:"source_session_id,omitempty"`
	SourceRecordID  *uint64        `json:"source_record_id,omitempty"`
	ServiceName     string         `json:"service_name"`
	Operation       string         `json:"operation"`
	BaselineStatus  U64Numeric     `json:"baseline_status,string"`
	RspSegCount     int            `json:"rsp_seg_count"`
	RawReqLen       int            `json:"raw_req_len"`
	RawRspLen       int            `json:"raw_rsp_len"`
	ScopedMockCount int            `json:"scoped_mock_count"`
	EgressEndpoints datatypes.JSON `json:"egress_endpoints"`
	MaskRulesJSON   datatypes.JSON `json:"mask_rules_json"`
	CreatedBy       uint           `json:"created_by"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

// ToSummary 转换为不含 bytea 大字段的轻量摘要对象
func (i *GoldenSuiteItem) ToSummary() GoldenSuiteItemSummary {
	mockCount := 0
	if len(i.ScopedMocks) > 0 {
		var mocks []interface{}
		if err := json.Unmarshal(i.ScopedMocks, &mocks); err == nil {
			mockCount = len(mocks)
		}
	}
	return GoldenSuiteItemSummary{
		ID:              i.ID,
		SuiteID:         i.SuiteID,
		OrderNo:         i.OrderNo,
		Name:            i.Name,
		SourceSessionID: i.SourceSessionID,
		SourceRecordID:  i.SourceRecordID,
		ServiceName:     i.ServiceName,
		Operation:       i.Operation,
		BaselineStatus:  i.BaselineStatus,
		RspSegCount:     i.RspSegCount,
		RawReqLen:       len(i.RawReqPacket),
		RawRspLen:       len(i.RawRspPacket),
		ScopedMockCount: mockCount,
		EgressEndpoints: i.EgressEndpoints,
		MaskRulesJSON:   i.MaskRulesJSON,
		CreatedBy:       i.CreatedBy,
		CreatedAt:       i.CreatedAt,
		UpdatedAt:       i.UpdatedAt,
	}
}
