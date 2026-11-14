package models_test

import (
	"strings"
	"testing"

	"code-common/backend/testdb"
	"code-traefik/config"
	"code-traefik/models"

	"gorm.io/datatypes"
)

func TestModels_BuiltinRulesAndVocabSeedIdempotent(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("首次 SetupWithDB 失败: %v", err)
	}

	// 验证四张表均符合 traefik_ 表前缀规范
	tables := []string{
		(models.CheckRule{}).TableName(),
		(models.VocabularyTerm{}).TableName(),
		(models.CheckReport{}).TableName(),
		(models.CheckViolation{}).TableName(),
	}
	for _, tbl := range tables {
		if !strings.HasPrefix(tbl, config.TablePrefix) {
			t.Fatalf("表 %q 未遵循 %s 前缀隔离规范", tbl, config.TablePrefix)
		}
	}

	// 验证内置规则已幂等写入，且 COMMENT_REQUIRED 默认 severity 为 WARNING，PKG_NAMING 支持多段包名
	var pkgRule models.CheckRule
	if err := db.Where("rule_code = ?", "PKG_NAMING").First(&pkgRule).Error; err != nil {
		t.Fatalf("未找到内置规则 PKG_NAMING: %v", err)
	}
	if !pkgRule.IsBuiltin || !pkgRule.IsEnabled {
		t.Fatalf("PKG_NAMING 应当为启用的内置规则: %+v", pkgRule)
	}

	var commentRule models.CheckRule
	if err := db.Where("rule_code = ?", "COMMENT_REQUIRED").First(&commentRule).Error; err != nil {
		t.Fatalf("未找到内置规则 COMMENT_REQUIRED: %v", err)
	}
	if commentRule.Severity != models.RuleSeverityWarning {
		t.Fatalf("COMMENT_REQUIRED 默认 severity 期望 WARNING，实际 %q", commentRule.Severity)
	}

	var ruleCountBefore int64
	if err := db.Model(&models.CheckRule{}).Count(&ruleCountBefore).Error; err != nil {
		t.Fatalf("统计规则失败: %v", err)
	}
	if ruleCountBefore < 6 {
		t.Fatalf("内置规则数量不足，期望 >= 6，实际 %d", ruleCountBefore)
	}

	var vocabCountBefore int64
	if err := db.Model(&models.VocabularyTerm{}).Where("source = ?", models.VocabSourceBuiltinSeed).Count(&vocabCountBefore).Error; err != nil {
		t.Fatalf("统计 BUILTIN_SEED 词条失败: %v", err)
	}
	if vocabCountBefore < 10 {
		t.Fatalf("BUILTIN_SEED 词条不足，期望 >= 10，实际 %d", vocabCountBefore)
	}

	// 人为将 COMMENT_REQUIRED 修改为 ERROR，再次执行幂等初始化应保留用户自定义修改（ON CONFLICT DO NOTHING）
	if err := db.Model(&models.CheckRule{}).Where("rule_code = ?", "COMMENT_REQUIRED").Update("severity", models.RuleSeverityError).Error; err != nil {
		t.Fatalf("修改 COMMENT_REQUIRED severity 失败: %v", err)
	}
	if err := models.EnsureBuiltinCheckRules(db); err != nil {
		t.Fatalf("二次执行 EnsureBuiltinCheckRules 失败: %v", err)
	}
	if err := models.EnsureBuiltinVocabularyTerms(db); err != nil {
		t.Fatalf("二次执行 EnsureBuiltinVocabularyTerms 失败: %v", err)
	}
	// 再次完整 SetupWithDB 应保持幂等
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("二次 SetupWithDB 失败: %v", err)
	}

	var ruleCountAfter int64
	if err := db.Model(&models.CheckRule{}).Count(&ruleCountAfter).Error; err != nil {
		t.Fatalf("二次统计规则失败: %v", err)
	}
	if ruleCountAfter != ruleCountBefore {
		t.Fatalf("重复种子初始化产生了重复规则: before=%d after=%d", ruleCountBefore, ruleCountAfter)
	}

	var commentRuleAfter models.CheckRule
	if err := db.Where("rule_code = ?", "COMMENT_REQUIRED").First(&commentRuleAfter).Error; err != nil {
		t.Fatalf("二次查询 COMMENT_REQUIRED 失败: %v", err)
	}
	if commentRuleAfter.Severity != models.RuleSeverityError {
		t.Fatalf("ON CONFLICT DO NOTHING 应保留用户修改后的 severity=ERROR，实际 %q", commentRuleAfter.Severity)
	}

	var vocabCountAfter int64
	if err := db.Model(&models.VocabularyTerm{}).Where("source = ?", models.VocabSourceBuiltinSeed).Count(&vocabCountAfter).Error; err != nil {
		t.Fatalf("二次统计 BUILTIN_SEED 词条失败: %v", err)
	}
	if vocabCountAfter != vocabCountBefore {
		t.Fatalf("重复种子初始化产生了重复词条: before=%d after=%d", vocabCountBefore, vocabCountAfter)
	}

	// 验证严禁写入未定义来源 (如已废弃的 PROTOHUB_IMPORT)
	invalidTerm := models.VocabularyTerm{
		Term:          "bad_legacy_root",
		DescriptionEn: "Invalid Source Test",
		DescriptionZh: "非法来源测试",
		Aliases:       datatypes.JSON([]byte(`[]`)),
		SimilarTerms:  datatypes.JSON([]byte(`[]`)),
		Domain:        "GENERAL",
		Source:        "PROTOHUB_IMPORT",
		IsApproved:    false,
	}
	if err := db.Create(&invalidTerm).Error; err == nil {
		t.Fatalf("数据库 CHECK 约束应当拦截非法词库来源 PROTOHUB_IMPORT，但插入竟成功")
	}
}
