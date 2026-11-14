package linter_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"code-traefik/internal/server/linter"
	"code-traefik/models"

	"gorm.io/datatypes"
)

func buildBuiltinRulesAndVocabFixture(t *testing.T) ([]models.CheckRule, []models.VocabularyTerm) {
	t.Helper()
	rules := []models.CheckRule{
		{
			ID:                 1,
			RuleCode:           "PKG_NAMING",
			Name:               "多段小写包名规范",
			Category:           models.RuleCategoryPackage,
			TargetScope:        models.RuleScopePackage,
			Severity:           models.RuleSeverityError,
			CheckMode:          models.RuleModeRegex,
			Pattern:            `^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*(\.v[0-9]+((alpha|beta|rc)[0-9]*)?)?$`,
			SuggestionTemplate: "包名 {symbol} 应采用多段小写点分格式，例如 {example}",
			ExemptionRule:      datatypes.JSON([]byte(`{}`)),
			IsBuiltin:          true,
			IsEnabled:          true,
		},
		{
			ID:                 2,
			RuleCode:           "SVC_PASCAL_CASE",
			Name:               "Service 大驼峰",
			Category:           models.RuleCategoryNaming,
			TargetScope:        models.RuleScopeService,
			Severity:           models.RuleSeverityError,
			CheckMode:          models.RuleModeRegex,
			Pattern:            `^[A-Z][A-Za-z0-9]*$`,
			SuggestionTemplate: "服务名 {symbol} 应采用大驼峰，例如 {example}",
			ExemptionRule:      datatypes.JSON([]byte(`{}`)),
			IsBuiltin:          true,
			IsEnabled:          true,
		},
		{
			ID:                 3,
			RuleCode:           "RPC_PASCAL_CASE",
			Name:               "RPC 大驼峰",
			Category:           models.RuleCategoryNaming,
			TargetScope:        models.RuleScopeRPC,
			Severity:           models.RuleSeverityError,
			CheckMode:          models.RuleModeRegex,
			Pattern:            `^[A-Z][A-Za-z0-9]*$`,
			SuggestionTemplate: "RPC 方法名 {symbol} 应采用大驼峰，例如 {example}",
			ExemptionRule:      datatypes.JSON([]byte(`{}`)),
			IsBuiltin:          true,
			IsEnabled:          true,
		},
		{
			ID:                 4,
			RuleCode:           "MSG_PASCAL_CASE",
			Name:               "Message 大驼峰",
			Category:           models.RuleCategoryNaming,
			TargetScope:        models.RuleScopeMessage,
			Severity:           models.RuleSeverityError,
			CheckMode:          models.RuleModeRegex,
			Pattern:            `^[A-Z][A-Za-z0-9]*$`,
			SuggestionTemplate: "消息名 {symbol} 应采用大驼峰，例如 {example}",
			ExemptionRule:      datatypes.JSON([]byte(`{}`)),
			IsBuiltin:          true,
			IsEnabled:          true,
		},
		{
			ID:                 5,
			RuleCode:           "FIELD_SNAKE_CASE",
			Name:               "Field 小写下划线",
			Category:           models.RuleCategoryNaming,
			TargetScope:        models.RuleScopeField,
			Severity:           models.RuleSeverityError,
			CheckMode:          models.RuleModeRegex,
			Pattern:            `^[a-z][a-z0-9]*(_[a-z0-9]+)*$`,
			SuggestionTemplate: "字段名 {symbol} 应使用 snake_case，例如 {expected}",
			ExemptionRule:      datatypes.JSON([]byte(`{}`)),
			IsBuiltin:          true,
			IsEnabled:          true,
		},
		{
			ID:                 6,
			RuleCode:           "ENUM_UPPER_SNAKE_WITH_PREFIX",
			Name:               "Enum 大写下划线+前缀",
			Category:           models.RuleCategoryNaming,
			TargetScope:        models.RuleScopeEnumValue,
			Severity:           models.RuleSeverityWarning,
			CheckMode:          models.RuleModeStructure,
			Pattern:            `^[A-Z][A-Z0-9]*(_[A-Z0-9]+)*$`,
			SuggestionTemplate: "枚举值 {symbol} 建议使用 {expected}",
			ExemptionRule:      datatypes.JSON([]byte(`{}`)),
			IsBuiltin:          true,
			IsEnabled:          true,
		},
		{
			ID:                 7,
			RuleCode:           "ENUM_ZERO_UNSPECIFIED",
			Name:               "Enum 0号 UNSPECIFIED",
			Category:           models.RuleCategoryStyle,
			TargetScope:        models.RuleScopeEnumValue,
			Severity:           models.RuleSeverityWarning,
			CheckMode:          models.RuleModeStructure,
			Pattern:            `.*_UNSPECIFIED$`,
			SuggestionTemplate: "枚举 0 号默认值 {symbol} 建议为 {expected}",
			ExemptionRule:      datatypes.JSON([]byte(`{}`)),
			IsBuiltin:          true,
			IsEnabled:          true,
		},
		{
			ID:                 8,
			RuleCode:           "COMMENT_REQUIRED",
			Name:               "注释必填(默认WARNING)",
			Category:           models.RuleCategoryComment,
			TargetScope:        models.RuleScopeRPC,
			Severity:           models.RuleSeverityWarning,
			CheckMode:          models.RuleModeComment,
			SuggestionTemplate: "请为 {symbol} 补充前导说明注释",
			ExemptionRule:      datatypes.JSON([]byte(`{}`)),
			IsBuiltin:          true,
			IsEnabled:          true,
		},
		{
			ID:                 9,
			RuleCode:           "VOCAB_STANDARD_TERM",
			Name:               "标准词汇词根规范",
			Category:           models.RuleCategoryVocab,
			TargetScope:        models.RuleScopeField,
			Severity:           models.RuleSeverityWarning,
			CheckMode:          models.RuleModeVocab,
			SuggestionTemplate: "词根 {symbol} 非标准缩写，建议替换为 {expected}",
			ExemptionRule:      datatypes.JSON([]byte(`{}`)),
			IsBuiltin:          true,
			IsEnabled:          true,
		},
	}
	vocab := []models.VocabularyTerm{
		{
			ID:            1,
			Term:          "amt",
			DescriptionEn: "Amount",
			DescriptionZh: "金额",
			Aliases:       datatypes.JSON([]byte(`["amnt","amt_val"]`)),
			SimilarTerms:  datatypes.JSON([]byte(`["amount"]`)),
			Domain:        "FINANCE",
			Source:        models.VocabSourceBuiltinSeed,
			IsApproved:    true,
		},
		{
			ID:            2,
			Term:          "ts",
			DescriptionEn: "Timestamp",
			DescriptionZh: "时间戳",
			Aliases:       datatypes.JSON([]byte(`["timestmp"]`)),
			SimilarTerms:  datatypes.JSON([]byte(`["timestamp"]`)),
			Domain:        "GENERAL",
			Source:        models.VocabSourceBuiltinSeed,
			IsApproved:    true,
		},
		{
			ID:            3,
			Term:          "ord",
			DescriptionEn: "Order",
			DescriptionZh: "订单",
			Aliases:       datatypes.JSON([]byte(`["ordr"]`)),
			SimilarTerms:  datatypes.JSON([]byte(`["order"]`)),
			Domain:        "TRADE",
			Source:        models.VocabSourceBuiltinSeed,
			IsApproved:    true,
		},
	}
	return rules, vocab
}

func TestLinter_MultiSegmentPkg_AndCommentRequiredWarningDefault(t *testing.T) {
	rules, vocab := buildBuiltinRulesAndVocabFixture(t)

	// 场景 A: 合法多段包名 dophi.order.v1 + 规范命名 + 仅缺失 RPC 注释 -> 仅 WARNING，状态应为 WARN 且 0 ERROR
	validMultiSegProto := `syntax = "proto3";
package dophi.order.v1;

// 订单请求
message CreateOrderReq {
  int64 order_id = 1;
  int64 amt = 2;
}

// 订单响应
message CreateOrderRsp {
  bool success = 1;
}

// 订单服务
service OrderService {
  rpc CreateOrder(CreateOrderReq) returns (CreateOrderRsp);
}
`
	outA, err := linter.EvaluateSources(context.Background(), rules, vocab, []linter.SourceInput{
		{ProtoFile: "dophi/order/v1/order.proto", Content: validMultiSegProto, Category: "BUSINESS_API"},
	}, linter.EvaluateOptions{TriggerSource: models.TriggerSourceManual})
	if err != nil {
		t.Fatalf("EvaluateSources 场景 A 失败: %v", err)
	}
	if outA.Report.ErrorCount != 0 {
		t.Fatalf("合法多段包名 dophi.order.v1 不应产生 ERROR，实际 ErrorCount=%d, violations=%+v", outA.Report.ErrorCount, outA.Violations)
	}
	if outA.Report.WarningCount == 0 {
		t.Fatalf("未注释 RPC CreateOrder 应当触发 COMMENT_REQUIRED WARNING")
	}
	if outA.Report.Status != models.ReportStatusWarn {
		t.Fatalf("仅存在 WARNING 时报告状态期望 WARN，实际 %q", outA.Report.Status)
	}

	var sawCommentWarn bool
	for _, v := range outA.Violations {
		if v.RuleCode == "PKG_NAMING" {
			t.Fatalf("dophi.order.v1 不应误报 PKG_NAMING: %+v", v)
		}
		if v.RuleCode == "COMMENT_REQUIRED" {
			sawCommentWarn = true
			if v.Severity != models.RuleSeverityWarning || v.FileLine <= 0 {
				t.Fatalf("COMMENT_REQUIRED 违规属性异常: %+v", v)
			}
		}
	}
	if !sawCommentWarn {
		t.Fatalf("期望捕捉到 COMMENT_REQUIRED 警告")
	}

	// 场景 B: 违规包名/驼峰字段/下划线 RPC/非标缩写 amnt/缺失前缀枚举
	badProto := `syntax = "proto3";
package Dophi.BadOrder;

enum OrderStatus {
  UNKNOWN = 0;
  ACTIVE = 1;
}

message bad_order_msg {
  int64 cust_ord_amnt = 1;
  string tradeTsMs = 2;
}

service order_svc {
  rpc create_order(bad_order_msg) returns (bad_order_msg);
}
`
	outB, err := linter.EvaluateSources(context.Background(), rules, vocab, []linter.SourceInput{
		{ProtoFile: "dophi/bad/order.proto", Content: badProto, Category: "BUSINESS_API"},
	}, linter.EvaluateOptions{TriggerSource: models.TriggerSourceManual})
	if err != nil {
		t.Fatalf("EvaluateSources 场景 B 失败: %v", err)
	}
	if outB.Report.Status != models.ReportStatusFailed || outB.Report.ErrorCount < 4 {
		t.Fatalf("包含多处 ERROR 违规时状态应为 FAILED 且 ErrorCount>=4，实际 status=%s err=%d violations=%+v", outB.Report.Status, outB.Report.ErrorCount, outB.Violations)
	}

	hitCodes := make(map[string]models.CheckViolation)
	for _, v := range outB.Violations {
		hitCodes[v.RuleCode] = v
		if v.FileLine <= 0 {
			t.Fatalf("规则 %s 必须附带精确 >0 的 FileLine: %+v", v.RuleCode, v)
		}
	}
	for _, requiredCode := range []string{"PKG_NAMING", "MSG_PASCAL_CASE", "FIELD_SNAKE_CASE", "RPC_PASCAL_CASE", "SVC_PASCAL_CASE", "ENUM_ZERO_UNSPECIFIED", "ENUM_UPPER_SNAKE_WITH_PREFIX", "VOCAB_STANDARD_TERM"} {
		if _, ok := hitCodes[requiredCode]; !ok {
			t.Fatalf("场景 B 漏检规则 %s，已检出规则=%v", requiredCode, hitCodes)
		}
	}
	if !strings.Contains(hitCodes["VOCAB_STANDARD_TERM"].Suggestion, "amt") {
		t.Fatalf("VOCAB_STANDARD_TERM 应建议将 amnt 替换为 amt，实际: %q", hitCodes["VOCAB_STANDARD_TERM"].Suggestion)
	}
}

func TestLinter_LLMFallbackOn3sTimeout_AndCacheHit(t *testing.T) {
	linter.ClearLLMCacheForTest()
	rules, vocab := buildBuiltinRulesAndVocabFixture(t)

	unknownProto := `syntax = "proto3";
package dophi.risk.v1;

// 风控检查请求
message RiskCheckReq {
  // 罕见定制词根 xkz
  string xkz_code = 1;
}

// 风控服务
service RiskService {
  // 执行风控检查
  rpc CheckRisk(RiskCheckReq) returns (RiskCheckReq);
}
`

	// 1. Mock 正常快速响应 LLM：验证 SUCCEEDED + 同 (token, vocab_hash) 二次命中缓存 HIT_CACHE
	var callCount int32
	fastSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]string{
						"content": `{"term":"xkz","description_en":"Risk Key Zone","description_zh":"风控识别区码","aliases":["xkz_cd"]}`,
					},
				},
			},
		})
	}))
	defer fastSrv.Close()

	srvHostPort := strings.TrimPrefix(fastSrv.URL, "http://")
	llmCfg := &linter.LLMSettings{
		Enabled:              true,
		BaseURL:              fastSrv.URL + "/v1",
		APIKey:               "sk-mock-key",
		Model:                "gpt-4o-mini",
		TimeoutMs:            3000,
		OutboundTrustedCIDRs: srvHostPort,
	}

	out1, err := linter.EvaluateSources(context.Background(), rules, vocab, []linter.SourceInput{
		{ProtoFile: "dophi/risk/v1/risk.proto", Content: unknownProto},
	}, linter.EvaluateOptions{
		IncludeLLM:  true,
		LLMOverride: llmCfg,
	})
	if err != nil {
		t.Fatalf("第一次 LLM 扫描失败: %v", err)
	}
	if out1.Report.LLMStatus != models.LLMStatusSucceeded {
		t.Fatalf("第一次 LLM 扫描状态期望 SUCCEEDED，实际 %q", out1.Report.LLMStatus)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Fatalf("期望调用 1 次 LLM，实际 %d", atomic.LoadInt32(&callCount))
	}

	// 二次相同输入必须直接命中进程内缓存，不发起任何 HTTP 调用
	out2, err := linter.EvaluateSources(context.Background(), rules, vocab, []linter.SourceInput{
		{ProtoFile: "dophi/risk/v1/risk.proto", Content: unknownProto},
	}, linter.EvaluateOptions{
		IncludeLLM:  true,
		LLMOverride: llmCfg,
	})
	if err != nil {
		t.Fatalf("第二次 LLM 缓存命中扫描失败: %v", err)
	}
	if out2.Report.LLMStatus != models.LLMStatusHitCache {
		t.Fatalf("第二次相同扫描状态期望 HIT_CACHE，实际 %q", out2.Report.LLMStatus)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Fatalf("命中缓存不应再次请求 LLM，期望 callCount=1，实际=%d", atomic.LoadInt32(&callCount))
	}

	// 2. Mock 阻塞 5s 不响应服务端：验证 3s 超时硬熔断，LLMStatus == TIMEOUT_FALLBACK 且静态违规不丢失
	linter.ClearLLMCacheForTest()
	slowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
			return
		}
	}))
	defer slowSrv.Close()

	slowHostPort := strings.TrimPrefix(slowSrv.URL, "http://")
	slowProto := strings.ReplaceAll(unknownProto, "xkz_code", "zqbx_amnt")
	start := time.Now()
	outTimeout, err := linter.EvaluateSources(context.Background(), rules, vocab, []linter.SourceInput{
		{ProtoFile: "dophi/risk/v1/slow.proto", Content: slowProto},
	}, linter.EvaluateOptions{
		IncludeLLM: true,
		LLMOverride: &linter.LLMSettings{
			Enabled:              true,
			BaseURL:              slowSrv.URL + "/v1",
			APIKey:               "sk-mock-key",
			Model:                "gpt-4o-mini",
			TimeoutMs:            3000,
			OutboundTrustedCIDRs: slowHostPort,
		},
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("LLM 超时降级扫描不应返回 error: %v", err)
	}
	if outTimeout.Report.LLMStatus != models.LLMStatusTimeoutFallback {
		t.Fatalf("期望 LLMStatus=TIMEOUT_FALLBACK，实际 %q", outTimeout.Report.LLMStatus)
	}
	if elapsed > 3800*time.Millisecond {
		t.Fatalf("3s 硬熔断失效，耗时过高: %v", elapsed)
	}
	// 静态规则 (amnt -> amt) 仍应完整保留
	var foundVocab bool
	for _, v := range outTimeout.Violations {
		if v.RuleCode == "VOCAB_STANDARD_TERM" && strings.Contains(v.Suggestion, "amt") {
			foundVocab = true
		}
	}
	if !foundVocab {
		t.Fatalf("LLM 超时降级后仍必须完整输出静态词库检查结果 (amnt -> amt)")
	}

	// 3. 验证 SSRF 目标 (169.254.169.254) 被 SafeDialer 阻断并降级 ERROR_FALLBACK
	linter.ClearLLMCacheForTest()
	outSSRF, err := linter.EvaluateSources(context.Background(), rules, vocab, []linter.SourceInput{
		{ProtoFile: "dophi/risk/v1/ssrf.proto", Content: slowProto},
	}, linter.EvaluateOptions{
		IncludeLLM: true,
		LLMOverride: &linter.LLMSettings{
			Enabled:              true,
			BaseURL:              "http://169.254.169.254/latest/v1",
			APIKey:               "sk-mock-key",
			Model:                "gpt-4o-mini",
			TimeoutMs:            1000,
			OutboundTrustedCIDRs: "169.254.0.0/16",
		},
	})
	if err != nil {
		t.Fatalf("SSRF 拦截 LLM 时扫描本身不应崩溃: %v", err)
	}
	if outSSRF.Report.LLMStatus != models.LLMStatusErrorFallback {
		t.Fatalf("LLM URL 命中元数据 SSRF 硬阻断后期望 ERROR_FALLBACK，实际 %q", outSSRF.Report.LLMStatus)
	}
}
