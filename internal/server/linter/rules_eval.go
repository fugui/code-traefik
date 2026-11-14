package linter

import (
	"encoding/json"
	"log"
	"regexp"
	"strings"
	"unicode"

	"code-traefik/models"

	"gorm.io/datatypes"
)

func parseExemptionRule(raw datatypes.JSON) models.RuleExemptionConfig {
	var cfg models.RuleExemptionConfig
	if len(raw) == 0 {
		return cfg
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		log.Printf("[linter] 解析规则豁免配置失败: %v", err)
	}
	return cfg
}

func isExempted(exempt models.RuleExemptionConfig, protoFile, pkg, category string) bool {
	if category != "" {
		for _, c := range exempt.ExcludeCategories {
			if strings.EqualFold(strings.TrimSpace(c), strings.TrimSpace(category)) {
				return true
			}
		}
	}
	if protoFile != "" {
		normFile := strings.TrimPrefix(strings.ReplaceAll(protoFile, "\\", "/"), "./")
		for _, p := range exempt.ExcludePaths {
			clean := strings.TrimPrefix(strings.TrimSpace(p), "./")
			if clean == "" {
				continue
			}
			if strings.HasPrefix(normFile, clean) || strings.Contains(normFile, clean) {
				return true
			}
		}
	}
	if pkg != "" {
		for _, exPkg := range exempt.ExcludePackages {
			clean := strings.TrimSpace(exPkg)
			if clean == "" {
				continue
			}
			if pkg == clean || strings.HasPrefix(pkg, clean+".") {
				return true
			}
		}
	}
	return false
}

func renderSuggestion(tmpl, symbol, expected, example string) string {
	s := strings.TrimSpace(tmpl)
	if s == "" {
		if expected != "" {
			return "建议修改为 " + expected
		}
		return "请参考命名与注释规范调整 " + symbol
	}
	if example == "" {
		example = expected
	}
	replacer := strings.NewReplacer(
		"{symbol}", symbol,
		"{expected}", expected,
		"{example}", example,
	)
	return replacer.Replace(s)
}

func toSnakeCase(ident string) string {
	tokens := SplitIdentifierTokens(ident)
	if len(tokens) == 0 {
		return strings.ToLower(ident)
	}
	return strings.Join(tokens, "_")
}

func toUpperSnakeCase(ident string) string {
	return strings.ToUpper(toSnakeCase(ident))
}

func toPascalCase(ident string) string {
	tokens := SplitIdentifierTokens(ident)
	if len(tokens) == 0 {
		return ident
	}
	var b strings.Builder
	for _, tok := range tokens {
		runes := []rune(tok)
		if len(runes) == 0 {
			continue
		}
		runes[0] = unicode.ToUpper(runes[0])
		b.WriteString(string(runes))
	}
	return b.String()
}

func evaluateStaticRules(files []parsedFile, rules []models.CheckRule) []models.CheckViolation {
	violations := make([]models.CheckViolation, 0)

	for _, rule := range rules {
		if !rule.IsEnabled {
			continue
		}
		if rule.CheckMode == models.RuleModeASTDuplicate || rule.CheckMode == models.RuleModeVocab {
			continue
		}
		exempt := parseExemptionRule(rule.ExemptionRule)
		var compiledRe *regexp.Regexp
		if strings.TrimSpace(rule.Pattern) != "" {
			re, err := regexp.Compile(strings.TrimSpace(rule.Pattern))
			if err != nil {
				continue
			}
			compiledRe = re
		}

		for _, f := range files {
			if isExempted(exempt, f.ProtoFile, f.Package, f.Category) {
				continue
			}
			switch rule.CheckMode {
			case models.RuleModeRegex:
				if compiledRe == nil {
					continue
				}
				violations = append(violations, evalRegexRuleOnFile(rule, compiledRe, f)...)
			case models.RuleModeComment:
				violations = append(violations, evalCommentRuleOnFile(rule, f)...)
			case models.RuleModeStructure:
				violations = append(violations, evalStructureRuleOnFile(rule, compiledRe, f)...)
			}
		}
	}

	return violations
}

func makeRuleViolation(rule models.CheckRule, protoFile, symbolPath string, line, col int, msg, suggestion string) models.CheckViolation {
	var ruleID *uint64
	if rule.ID > 0 {
		id := rule.ID
		ruleID = &id
	}
	if line <= 0 {
		line = 1
	}
	if col <= 0 {
		col = 1
	}
	return models.CheckViolation{
		RuleID:           ruleID,
		RuleCode:         rule.RuleCode,
		Severity:         rule.Severity,
		ProtoFile:        protoFile,
		SymbolPath:       symbolPath,
		FileLine:         line,
		FileColumn:       col,
		Message:          msg,
		Suggestion:       suggestion,
		SuggestionSource: models.SuggestionSourceRuleTemplate,
	}
}

func evalRegexRuleOnFile(rule models.CheckRule, re *regexp.Regexp, f parsedFile) []models.CheckViolation {
	out := make([]models.CheckViolation, 0)
	switch rule.TargetScope {
	case models.RuleScopePackage:
		if strings.TrimSpace(f.Package) == "" {
			sugg := renderSuggestion(rule.SuggestionTemplate, "<empty>", "domain.service.v1", "dophi.order.v1")
			out = append(out, makeRuleViolation(rule, f.ProtoFile, "package", f.PkgLine, f.PkgCol, "缺少 package 声明", sugg))
			return out
		}
		if !re.MatchString(f.Package) {
			expected := strings.ToLower(strings.ReplaceAll(f.Package, "-", "_"))
			sugg := renderSuggestion(rule.SuggestionTemplate, f.Package, expected, "dophi.order.v1")
			out = append(out, makeRuleViolation(rule, f.ProtoFile, f.Package, f.PkgLine, f.PkgCol, "包名 "+f.Package+" 不符合规范 "+rule.Pattern, sugg))
		}
	case models.RuleScopeService:
		for _, svc := range f.Services {
			if !re.MatchString(svc.Name) {
				exp := toPascalCase(svc.Name)
				out = append(out, makeRuleViolation(rule, f.ProtoFile, svc.FullName, svc.Line, svc.Col,
					"Service 命名 "+svc.Name+" 不符合大驼峰规范",
					renderSuggestion(rule.SuggestionTemplate, svc.Name, exp, exp)))
			}
		}
	case models.RuleScopeRPC:
		for _, svc := range f.Services {
			for _, rpc := range svc.Methods {
				if !re.MatchString(rpc.Name) {
					exp := toPascalCase(rpc.Name)
					out = append(out, makeRuleViolation(rule, f.ProtoFile, rpc.FullName, rpc.Line, rpc.Col,
						"RPC 方法 "+rpc.Name+" 不符合大驼峰规范",
						renderSuggestion(rule.SuggestionTemplate, rpc.Name, exp, exp)))
				}
			}
		}
	case models.RuleScopeMessage:
		for _, msg := range f.Messages {
			if !re.MatchString(msg.Name) {
				exp := toPascalCase(msg.Name)
				out = append(out, makeRuleViolation(rule, f.ProtoFile, msg.FullName, msg.Line, msg.Col,
					"Message 命名 "+msg.Name+" 不符合大驼峰规范",
					renderSuggestion(rule.SuggestionTemplate, msg.Name, exp, exp)))
			}
		}
	case models.RuleScopeField:
		for _, msg := range f.Messages {
			for _, fld := range msg.Fields {
				if !re.MatchString(fld.Name) {
					exp := toSnakeCase(fld.Name)
					out = append(out, makeRuleViolation(rule, f.ProtoFile, msg.FullName+"."+fld.Name, fld.Line, fld.Col,
						"字段 "+fld.Name+" 不符合 lowercase snake_case 规范",
						renderSuggestion(rule.SuggestionTemplate, fld.Name, exp, exp)))
				}
			}
		}
	case models.RuleScopeEnum:
		for _, en := range f.Enums {
			if !re.MatchString(en.Name) {
				exp := toPascalCase(en.Name)
				out = append(out, makeRuleViolation(rule, f.ProtoFile, en.FullName, en.Line, en.Col,
					"Enum 命名 "+en.Name+" 不符合大驼峰规范",
					renderSuggestion(rule.SuggestionTemplate, en.Name, exp, exp)))
			}
		}
	case models.RuleScopeEnumValue:
		for _, en := range f.Enums {
			for _, v := range en.Values {
				if !re.MatchString(v.Name) {
					exp := toUpperSnakeCase(v.Name)
					out = append(out, makeRuleViolation(rule, f.ProtoFile, en.FullName+"."+v.Name, v.Line, v.Col,
						"枚举值 "+v.Name+" 不符合正则规范",
						renderSuggestion(rule.SuggestionTemplate, v.Name, exp, exp)))
				}
			}
		}
	}
	return out
}

func evalCommentRuleOnFile(rule models.CheckRule, f parsedFile) []models.CheckViolation {
	out := make([]models.CheckViolation, 0)
	checkRPC := rule.TargetScope == models.RuleScopeRPC || rule.TargetScope == "" || rule.TargetScope == "ALL"
	checkSvc := rule.TargetScope == models.RuleScopeService || rule.TargetScope == "ALL"
	checkMsg := rule.TargetScope == models.RuleScopeMessage || rule.TargetScope == "ALL"

	if checkSvc {
		for _, svc := range f.Services {
			if strings.TrimSpace(svc.LeadingComment) == "" {
				out = append(out, makeRuleViolation(rule, f.ProtoFile, svc.FullName, svc.Line, svc.Col,
					"Service "+svc.Name+" 缺少前导注释",
					renderSuggestion(rule.SuggestionTemplate, svc.Name, "// "+svc.Name+" 服务说明", "// "+svc.Name)))
			}
		}
	}
	if checkRPC {
		for _, svc := range f.Services {
			for _, rpc := range svc.Methods {
				if strings.TrimSpace(rpc.LeadingComment) == "" {
					out = append(out, makeRuleViolation(rule, f.ProtoFile, rpc.FullName, rpc.Line, rpc.Col,
						"RPC 方法 "+rpc.Name+" 缺少前导注释",
						renderSuggestion(rule.SuggestionTemplate, rpc.FullName, "// "+rpc.Name+" 接口说明", "// "+rpc.Name)))
				}
			}
		}
	}
	if checkMsg {
		for _, msg := range f.Messages {
			if strings.TrimSpace(msg.LeadingComment) == "" {
				out = append(out, makeRuleViolation(rule, f.ProtoFile, msg.FullName, msg.Line, msg.Col,
					"Message "+msg.Name+" 缺少前导注释",
					renderSuggestion(rule.SuggestionTemplate, msg.FullName, "// "+msg.Name+" 结构说明", "// "+msg.Name)))
			}
		}
	}
	return out
}

func evalStructureRuleOnFile(rule models.CheckRule, re *regexp.Regexp, f parsedFile) []models.CheckViolation {
	out := make([]models.CheckViolation, 0)
	switch rule.RuleCode {
	case "ENUM_ZERO_UNSPECIFIED":
		for _, en := range f.Enums {
			enumPrefix := toUpperSnakeCase(en.Name)
			for _, val := range en.Values {
				if val.Number != 0 {
					continue
				}
				matched := strings.HasSuffix(val.Name, "_UNSPECIFIED")
				if re != nil {
					matched = re.MatchString(val.Name)
				}
				if !matched {
					expected := enumPrefix + "_UNSPECIFIED"
					out = append(out, makeRuleViolation(rule, f.ProtoFile, en.FullName+"."+val.Name, val.Line, val.Col,
						"枚举 0 号默认值 "+val.Name+" 应以 _UNSPECIFIED 结尾",
						renderSuggestion(rule.SuggestionTemplate, val.Name, expected, expected)))
				}
			}
		}
	case "ENUM_UPPER_SNAKE_WITH_PREFIX":
		for _, en := range f.Enums {
			enumPrefix := toUpperSnakeCase(en.Name) + "_"
			for _, val := range en.Values {
				badFormat := re != nil && !re.MatchString(val.Name)
				missingPrefix := enumPrefix != "_" && !strings.HasPrefix(val.Name, enumPrefix)
				if badFormat || missingPrefix {
					expected := enumPrefix + strings.TrimPrefix(toUpperSnakeCase(val.Name), enumPrefix)
					out = append(out, makeRuleViolation(rule, f.ProtoFile, en.FullName+"."+val.Name, val.Line, val.Col,
						"枚举值 "+val.Name+" 建议使用大写下划线并携带前缀 "+enumPrefix,
						renderSuggestion(rule.SuggestionTemplate, val.Name, expected, expected)))
				}
			}
		}
	default:
		if re != nil {
			out = append(out, evalRegexRuleOnFile(rule, re, f)...)
		}
	}
	return out
}
