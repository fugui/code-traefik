package linter_test

import (
	"context"
	"testing"

	"code-traefik/internal/server/linter"
	"code-traefik/models"

	"gorm.io/datatypes"
)

func TestDuplicateASTDetector_ThreeGuardrailsZeroFalsePositive(t *testing.T) {
	rules := []models.CheckRule{
		{
			RuleCode:      "DUPLICATE_MESSAGE",
			Severity:      models.RuleSeverityWarning,
			CheckMode:     models.RuleModeASTDuplicate,
			Pattern:       "0.85",
			ExemptionRule: datatypes.JSON([]byte(`{"exclude_categories":["BASE_LIB"],"exclude_paths":["common/ignored/"]}`)),
			IsBuiltin:     true,
			IsEnabled:     true,
		},
	}

	// 护栏 1：<3 有效字段的常见分页/ID 请求不应误报重复
	// 护栏 2：BASE_LIB 基础库与 exclude_paths 豁免不触发重复报警
	// 护栏 3：同前缀跨版本演进 (dophi.order.v1 vs dophi.order.v2) 不触发重复报警
	// 强类型隔离：同名但字段类型不同 (int64 vs string) 不得计为同一 token
	// 正例：跨领域 dophi.trade.v1.TradeSummary 与 dophi.clearing.v1.DealSummary 6 个同类同义字段完全一致 -> Jaccard >= 0.85
	inputs := []linter.SourceInput{
		{
			ProtoFile: "dophi/alpha/v1/alpha.proto",
			Category:  "BUSINESS_API",
			Content: `syntax = "proto3";
package dophi.alpha.v1;

message Empty {}
message IdReq { int64 id = 1; }
message PageReq { int32 page = 1; int32 page_size = 2; }
`,
		},
		{
			ProtoFile: "dophi/beta/v1/beta.proto",
			Category:  "BUSINESS_API",
			Content: `syntax = "proto3";
package dophi.beta.v1;

message Empty {}
message IdReq { int64 id = 1; }
message PageReq { int32 page = 1; int32 page_size = 2; }
`,
		},
		{
			ProtoFile: "base/common/audit.proto",
			Category:  "BASE_LIB",
			Content: `syntax = "proto3";
package dophi.base.v1;

message CommonAuditMeta {
  int64 tenant_id = 1;
  int64 operator_id = 2;
  string trace_id = 3;
  int64 created_ts = 4;
}
`,
		},
		{
			ProtoFile: "dophi/other/v1/audit.proto",
			Category:  "BUSINESS_API",
			Content: `syntax = "proto3";
package dophi.other.v1;

message CommonAuditMetaCopy {
  int64 tenant_id = 1;
  int64 operator_id = 2;
  string trace_id = 3;
  int64 created_ts = 4;
}
`,
		},
		{
			ProtoFile: "dophi/order/v1/order.proto",
			Category:  "BUSINESS_API",
			Content: `syntax = "proto3";
package dophi.order.v1;

message OrderDetail {
  int64 order_id = 1;
  int64 user_id = 2;
  string symbol = 3;
  int64 qty = 4;
  int64 amt = 5;
}
`,
		},
		{
			ProtoFile: "dophi/order/v2/order.proto",
			Category:  "BUSINESS_API",
			Content: `syntax = "proto3";
package dophi.order.v2;

message OrderDetail {
  int64 order_id = 1;
  int64 user_id = 2;
  string symbol = 3;
  int64 qty = 4;
  int64 amt = 5;
}
`,
		},
		{
			ProtoFile: "dophi/billing/v1/billing.proto",
			Category:  "BUSINESS_API",
			Content: `syntax = "proto3";
package dophi.billing.v1;

message FeeNumericItem {
  int64 amount = 1;
  int64 tax = 2;
  int64 fee = 3;
  int64 rebate = 4;
}
`,
		},
		{
			ProtoFile: "dophi/notify/v1/notify.proto",
			Category:  "BUSINESS_API",
			Content: `syntax = "proto3";
package dophi.notify.v1;

message FeeDisplayItem {
  string amount = 1;
  string tax = 2;
  string fee = 3;
  string rebate = 4;
}
`,
		},
		{
			ProtoFile: "dophi/trade/v1/trade.proto",
			Category:  "BUSINESS_API",
			Content: `syntax = "proto3";
package dophi.trade.v1;

message TradeSummary {
  int64 trade_id = 1;
  int64 user_id = 2;
  int64 order_id = 3;
  int64 amt = 4;
  string ccy = 5;
  int64 ts = 6;
}
`,
		},
		{
			ProtoFile: "dophi/clearing/v1/clearing.proto",
			Category:  "BUSINESS_API",
			Content: `syntax = "proto3";
package dophi.clearing.v1;

message DealSummary {
  int64 trade_id = 1;
  int64 user_id = 2;
  int64 order_id = 3;
  int64 amt = 4;
  string ccy = 5;
  int64 ts = 6;
}
`,
		},
	}

	groups, err := linter.DetectDuplicateMessages(context.Background(), inputs, rules, 0.85)
	if err != nil {
		t.Fatalf("DetectDuplicateMessages 失败: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("期望精确命中 1 个跨服务重复结构体簇，实际命中 %d 个: %+v", len(groups), groups)
	}
	g := groups[0]
	if g.ClusterHash == "" || g.Similarity < 0.85 || len(g.Members) != 2 {
		t.Fatalf("重复簇详情不合法: %+v", g)
	}
	memberNames := map[string]bool{}
	for _, m := range g.Members {
		memberNames[m.Message] = true
		if m.FileLine <= 0 || m.FieldCnt != 6 {
			t.Fatalf("重复簇成员行号或字段数异常: %+v", m)
		}
	}
	if !memberNames["TradeSummary"] || !memberNames["DealSummary"] {
		t.Fatalf("期望命中 TradeSummary 与 DealSummary，实际成员: %+v", g.Members)
	}
}
