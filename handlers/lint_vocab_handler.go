package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	commonAudit "code-common/backend/audit"
	commonAuth "code-common/backend/auth"
	commonModels "code-common/backend/models"
	"code-traefik/internal/server/linter"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var (
	validRuleCodePattern   = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,63}$`)
	validVocabTermPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	allowedRuleSeverities  = map[string]bool{models.RuleSeverityError: true, models.RuleSeverityWarning: true, models.RuleSeverityInfo: true}
	allowedRuleCategories  = map[string]bool{models.RuleCategoryNaming: true, models.RuleCategoryComment: true, models.RuleCategoryPackage: true, models.RuleCategoryStyle: true, models.RuleCategoryDuplicate: true, models.RuleCategoryVocab: true}
	allowedRuleTargetScope = map[string]bool{models.RuleScopePackage: true, models.RuleScopeService: true, models.RuleScopeRPC: true, models.RuleScopeMessage: true, models.RuleScopeField: true, models.RuleScopeEnum: true, models.RuleScopeEnumValue: true, "ALL": true}
	allowedRuleCheckModes  = map[string]bool{models.RuleModeRegex: true, models.RuleModeStructure: true, models.RuleModeComment: true, models.RuleModeASTDuplicate: true, models.RuleModeVocab: true}
	allowedVocabSources    = map[string]bool{models.VocabSourceManual: true, models.VocabSourceBuiltinSeed: true, models.VocabSourceLLMSuggested: true}
)

func currentOperatorName(c *gin.Context) (uint, string) {
	uc := commonAuth.GetUserContext(c)
	if uc == nil {
		return 0, "system"
	}
	name := strings.TrimSpace(uc.Name)
	if name == "" {
		name = strings.TrimSpace(uc.Username)
	}
	if name == "" {
		name = "admin"
	}
	return uc.UserID, name
}

func escapeLikePattern(raw string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(raw)
}

// ListCheckRules 分页查询静态检查规则矩阵 GET /api/check-rules (JWT)
func ListCheckRules(c *gin.Context) {
	page, pageSize := parseStandardPagination(c)
	query := models.DB.Model(&models.CheckRule{})

	if cat := strings.ToUpper(strings.TrimSpace(c.Query("category"))); cat != "" {
		query = query.Where("category = ?", cat)
	}
	if sev := strings.ToUpper(strings.TrimSpace(c.Query("severity"))); sev != "" {
		query = query.Where("severity = ?", sev)
	}
	if enRaw := strings.TrimSpace(c.Query("is_enabled")); enRaw != "" {
		if parsed, err := strconv.ParseBool(enRaw); err == nil {
			query = query.Where("is_enabled = ?", parsed)
		}
	}
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		like := "%" + escapeLikePattern(search) + "%"
		query = query.Where("rule_code ILIKE ? OR name ILIKE ? OR description ILIKE ?", like, like, like)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "统计静态检查规则失败"})
		return
	}

	var items []models.CheckRule
	offset := (page - 1) * pageSize
	if err := query.Order("is_builtin DESC, id ASC").Offset(offset).Limit(pageSize).Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询静态检查规则失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

type createCheckRulePayload struct {
	RuleCode           string                      `json:"rule_code" binding:"required"`
	Name               string                      `json:"name" binding:"required"`
	Category           string                      `json:"category" binding:"required"`
	TargetScope        string                      `json:"target_scope" binding:"required"`
	Severity           string                      `json:"severity"`
	CheckMode          string                      `json:"check_mode"`
	Pattern            string                      `json:"pattern"`
	Description        string                      `json:"description"`
	SuggestionTemplate string                      `json:"suggestion_template"`
	ExemptionRule      *models.RuleExemptionConfig `json:"exemption_rule"`
	IsEnabled          *bool                       `json:"is_enabled"`
}

// CreateCheckRule 新增自定义静态检查规则 POST /api/check-rules (AdminOnly)
func CreateCheckRule(c *gin.Context) {
	var req createCheckRulePayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数格式非法: " + err.Error()})
		return
	}

	ruleCode := strings.ToUpper(strings.TrimSpace(req.RuleCode))
	if !validRuleCodePattern.MatchString(ruleCode) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "rule_code 须为 2-64 位大写字母/数字/下划线组合"})
		return
	}
	category := strings.ToUpper(strings.TrimSpace(req.Category))
	if !allowedRuleCategories[category] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的规则分类 category"})
		return
	}
	scope := strings.ToUpper(strings.TrimSpace(req.TargetScope))
	if !allowedRuleTargetScope[scope] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的目标作用范围 target_scope"})
		return
	}
	severity := strings.ToUpper(strings.TrimSpace(req.Severity))
	if severity == "" {
		severity = models.RuleSeverityWarning
	}
	if !allowedRuleSeverities[severity] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的告警级别 severity，须为 ERROR/WARNING/INFO"})
		return
	}
	checkMode := strings.ToUpper(strings.TrimSpace(req.CheckMode))
	if checkMode == "" {
		checkMode = models.RuleModeRegex
	}
	if !allowedRuleCheckModes[checkMode] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的检查模式 check_mode"})
		return
	}
	pattern := strings.TrimSpace(req.Pattern)
	if checkMode == models.RuleModeRegex {
		if pattern == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "REGEX 模式规则必须提供正则表达式 pattern"})
			return
		}
		if _, err := regexp.Compile(pattern); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "正则表达式 pattern 语法非法: " + err.Error()})
			return
		}
	}

	var existing int64
	if err := models.DB.Model(&models.CheckRule{}).Where("rule_code = ?", ruleCode).Count(&existing).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "校验规则唯一性失败"})
		return
	}
	if existing > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("规则编码 %s 已存在", ruleCode)})
		return
	}

	exemptJSON := datatypes.JSON([]byte(`{}`))
	if req.ExemptionRule != nil {
		if raw, err := json.Marshal(req.ExemptionRule); err == nil {
			exemptJSON = datatypes.JSON(raw)
		}
	}
	isEnabled := true
	if req.IsEnabled != nil {
		isEnabled = *req.IsEnabled
	}

	uid, opName := currentOperatorName(c)
	rule := models.CheckRule{
		RuleCode:           ruleCode,
		Name:               strings.TrimSpace(req.Name),
		Category:           category,
		TargetScope:        scope,
		Severity:           severity,
		CheckMode:          checkMode,
		Pattern:            pattern,
		Description:        strings.TrimSpace(req.Description),
		SuggestionTemplate: strings.TrimSpace(req.SuggestionTemplate),
		ExemptionRule:      exemptJSON,
		IsBuiltin:          false,
		IsEnabled:          isEnabled,
		UpdatedBy:          uid,
	}
	if err := models.DB.Create(&rule).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建规则失败: " + err.Error()})
		return
	}

	commonAudit.SetAuditContext(
		c,
		"traefik_linter",
		"create_rule",
		commonModels.AuditLevelP1,
		fmt.Sprintf("管理员 [%s] 新增静态检查规则 %s (%s)", opName, rule.RuleCode, rule.Severity),
		"check_rule",
		strconv.FormatUint(rule.ID, 10),
		rule.RuleCode,
		nil,
		rule,
	)

	c.JSON(http.StatusCreated, rule)
}

type updateCheckRulePayload struct {
	Name               *string                     `json:"name"`
	Category           *string                     `json:"category"`
	TargetScope        *string                     `json:"target_scope"`
	Severity           *string                     `json:"severity"`
	CheckMode          *string                     `json:"check_mode"`
	Pattern            *string                     `json:"pattern"`
	Description        *string                     `json:"description"`
	SuggestionTemplate *string                     `json:"suggestion_template"`
	ExemptionRule      *models.RuleExemptionConfig `json:"exemption_rule"`
	IsEnabled          *bool                       `json:"is_enabled"`
}

// UpdateCheckRule 修改检查规则（启停开关、严重级别、正则/豁免配置等）PUT /api/check-rules/:id (AdminOnly)
func UpdateCheckRule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的规则 ID"})
		return
	}

	var rule models.CheckRule
	if err := models.DB.First(&rule, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "未找到目标静态检查规则"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询规则失败"})
		return
	}

	var req updateCheckRulePayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求载荷格式错误"})
		return
	}

	beforeSnapshot := rule

	if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
		rule.Name = strings.TrimSpace(*req.Name)
	}
	if req.Category != nil {
		cat := strings.ToUpper(strings.TrimSpace(*req.Category))
		if !allowedRuleCategories[cat] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "非法的规则分类 category"})
			return
		}
		rule.Category = cat
	}
	if req.TargetScope != nil {
		scope := strings.ToUpper(strings.TrimSpace(*req.TargetScope))
		if !allowedRuleTargetScope[scope] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 target_scope"})
			return
		}
		rule.TargetScope = scope
	}
	if req.Severity != nil {
		sev := strings.ToUpper(strings.TrimSpace(*req.Severity))
		if !allowedRuleSeverities[sev] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "severity 须为 ERROR/WARNING/INFO"})
			return
		}
		rule.Severity = sev
	}
	if req.CheckMode != nil && !rule.IsBuiltin {
		mode := strings.ToUpper(strings.TrimSpace(*req.CheckMode))
		if !allowedRuleCheckModes[mode] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 check_mode"})
			return
		}
		rule.CheckMode = mode
	}
	if req.Pattern != nil {
		p := strings.TrimSpace(*req.Pattern)
		if rule.CheckMode == models.RuleModeRegex && p != "" {
			if _, compErr := regexp.Compile(p); compErr != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "正则表达式语法非法: " + compErr.Error()})
				return
			}
		}
		rule.Pattern = p
	}
	if req.Description != nil {
		rule.Description = strings.TrimSpace(*req.Description)
	}
	if req.SuggestionTemplate != nil {
		rule.SuggestionTemplate = strings.TrimSpace(*req.SuggestionTemplate)
	}
	if req.ExemptionRule != nil {
		if raw, mErr := json.Marshal(req.ExemptionRule); mErr == nil {
			rule.ExemptionRule = datatypes.JSON(raw)
		}
	}
	if req.IsEnabled != nil {
		rule.IsEnabled = *req.IsEnabled
	}

	uid, opName := currentOperatorName(c)
	rule.UpdatedBy = uid

	if err := models.DB.Save(&rule).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存规则失败"})
		return
	}

	commonAudit.SetAuditContext(
		c,
		"traefik_linter",
		"update_rule",
		commonModels.AuditLevelP1,
		fmt.Sprintf("管理员 [%s] 更新规则 %s (enabled=%v, severity=%s)", opName, rule.RuleCode, rule.IsEnabled, rule.Severity),
		"check_rule",
		strconv.FormatUint(rule.ID, 10),
		rule.RuleCode,
		beforeSnapshot,
		rule,
	)

	c.JSON(http.StatusOK, rule)
}

// DeleteCheckRule 删除自定义检查规则 DELETE /api/check-rules/:id (AdminOnly；内置规则返回 409)
func DeleteCheckRule(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的规则 ID"})
		return
	}

	var rule models.CheckRule
	if err := models.DB.First(&rule, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "未找到目标静态检查规则"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询规则失败"})
		return
	}

	if rule.IsBuiltin {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("内置系统规则 %s 不可删除，请改用禁用开关关闭", rule.RuleCode)})
		return
	}

	if err := models.DB.Delete(&rule).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除规则失败"})
		return
	}

	_, opName := currentOperatorName(c)
	commonAudit.SetAuditContext(
		c,
		"traefik_linter",
		"delete_rule",
		commonModels.AuditLevelP1,
		fmt.Sprintf("管理员 [%s] 删除自定义规则 %s", opName, rule.RuleCode),
		"check_rule",
		strconv.FormatUint(rule.ID, 10),
		rule.RuleCode,
		rule,
		nil,
	)

	c.JSON(http.StatusOK, gin.H{"message": "规则已删除", "id": rule.ID})
}

// ListVocabularyTerms 分页查询标准词汇库 GET /api/vocabulary-terms (JWT)
func ListVocabularyTerms(c *gin.Context) {
	page, pageSize := parseStandardPagination(c)
	query := models.DB.Model(&models.VocabularyTerm{})

	if domain := strings.ToUpper(strings.TrimSpace(c.Query("domain"))); domain != "" {
		query = query.Where("domain = ?", domain)
	}
	if source := strings.ToUpper(strings.TrimSpace(c.Query("source"))); source != "" {
		query = query.Where("source = ?", source)
	}
	if appRaw := strings.TrimSpace(c.Query("is_approved")); appRaw != "" {
		if parsed, err := strconv.ParseBool(appRaw); err == nil {
			query = query.Where("is_approved = ?", parsed)
		}
	}
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		like := "%" + escapeLikePattern(search) + "%"
		query = query.Where("term ILIKE ? OR description_en ILIKE ? OR description_zh ILIKE ? OR aliases::text ILIKE ?", like, like, like, like)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "统计标准词库失败"})
		return
	}

	var items []models.VocabularyTerm
	offset := (page - 1) * pageSize
	if err := query.Order("is_approved ASC, term ASC").Offset(offset).Limit(pageSize).Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询标准词库失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

type createVocabularyTermPayload struct {
	Term          string   `json:"term" binding:"required"`
	DescriptionEn string   `json:"description_en"`
	DescriptionZh string   `json:"description_zh" binding:"required"`
	Aliases       []string `json:"aliases"`
	SimilarTerms  []string `json:"similar_terms"`
	Domain        string   `json:"domain"`
	Source        string   `json:"source"`
	IsApproved    *bool    `json:"is_approved"`
}

func normalizeStringSlice(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		clean := strings.ToLower(strings.TrimSpace(s))
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}

// CreateVocabularyTerm 手动录入标准词根 POST /api/vocabulary-terms (AdminOnly)
func CreateVocabularyTerm(c *gin.Context) {
	var req createVocabularyTermPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数格式非法: " + err.Error()})
		return
	}

	term := strings.ToLower(strings.TrimSpace(req.Term))
	if !validVocabTermPattern.MatchString(term) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "标准词根 term 须为 1-64 位小写字母/数字/下划线标识"})
		return
	}

	source := strings.ToUpper(strings.TrimSpace(req.Source))
	if source == "" {
		source = models.VocabSourceManual
	}
	if !allowedVocabSources[source] || source != models.VocabSourceManual {
		c.JSON(http.StatusBadRequest, gin.H{"error": "手动创建词条仅支持 source=MANUAL，禁止写入非授权来源 " + source})
		return
	}

	var dupCnt int64
	if err := models.DB.Model(&models.VocabularyTerm{}).Where("term = ?", term).Count(&dupCnt).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "校验词根唯一性失败"})
		return
	}
	if dupCnt > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("标准词根 %q 已存在", term)})
		return
	}

	domain := strings.ToUpper(strings.TrimSpace(req.Domain))
	if domain == "" {
		domain = "GENERAL"
	}
	isApproved := true
	if req.IsApproved != nil {
		isApproved = *req.IsApproved
	}
	aliasesJSON, marshalErr := json.Marshal(normalizeStringSlice(req.Aliases))
	if marshalErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "序列化 aliases 失败"})
		return
	}
	similarJSON, marshalErr := json.Marshal(normalizeStringSlice(req.SimilarTerms))
	if marshalErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "序列化 similar_terms 失败"})
		return
	}

	uid, opName := currentOperatorName(c)
	item := models.VocabularyTerm{
		Term:          term,
		DescriptionEn: strings.TrimSpace(req.DescriptionEn),
		DescriptionZh: strings.TrimSpace(req.DescriptionZh),
		Aliases:       datatypes.JSON(aliasesJSON),
		SimilarTerms:  datatypes.JSON(similarJSON),
		Domain:        domain,
		Source:        source,
		IsApproved:    isApproved,
		CreatedBy:     uid,
	}
	if err := models.DB.Create(&item).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "新增标准词根失败: " + err.Error()})
		return
	}

	commonAudit.SetAuditContext(
		c,
		"traefik_linter",
		"upsert_vocab",
		commonModels.AuditLevelP1,
		fmt.Sprintf("管理员 [%s] 新增标准词根 %s (%s)", opName, item.Term, item.DescriptionZh),
		"vocabulary_term",
		strconv.FormatUint(item.ID, 10),
		item.Term,
		nil,
		item,
	)

	c.JSON(http.StatusCreated, item)
}

type updateVocabularyTermPayload struct {
	DescriptionEn *string   `json:"description_en"`
	DescriptionZh *string   `json:"description_zh"`
	Aliases       *[]string `json:"aliases"`
	SimilarTerms  *[]string `json:"similar_terms"`
	Domain        *string   `json:"domain"`
	IsApproved    *bool     `json:"is_approved"`
}

// UpdateVocabularyTerm 更新词根详情或一键采纳 LLM_SUGGESTED 词条 PUT /api/vocabulary-terms/:id (AdminOnly)
func UpdateVocabularyTerm(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的词根 ID"})
		return
	}

	var item models.VocabularyTerm
	if err := models.DB.First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "未找到目标标准词根"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询词根失败"})
		return
	}

	var req updateVocabularyTermPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求载荷格式错误"})
		return
	}

	beforeSnapshot := item

	if req.DescriptionEn != nil {
		item.DescriptionEn = strings.TrimSpace(*req.DescriptionEn)
	}
	if req.DescriptionZh != nil && strings.TrimSpace(*req.DescriptionZh) != "" {
		item.DescriptionZh = strings.TrimSpace(*req.DescriptionZh)
	}
	if req.Aliases != nil {
		raw, marshalErr := json.Marshal(normalizeStringSlice(*req.Aliases))
		if marshalErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "序列化 aliases 失败"})
			return
		}
		item.Aliases = datatypes.JSON(raw)
	}
	if req.SimilarTerms != nil {
		raw, marshalErr := json.Marshal(normalizeStringSlice(*req.SimilarTerms))
		if marshalErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "序列化 similar_terms 失败"})
			return
		}
		item.SimilarTerms = datatypes.JSON(raw)
	}
	if req.Domain != nil && strings.TrimSpace(*req.Domain) != "" {
		item.Domain = strings.ToUpper(strings.TrimSpace(*req.Domain))
	}
	if req.IsApproved != nil {
		item.IsApproved = *req.IsApproved
	}

	if err := models.DB.Save(&item).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存标准词根失败"})
		return
	}

	_, opName := currentOperatorName(c)
	commonAudit.SetAuditContext(
		c,
		"traefik_linter",
		"upsert_vocab",
		commonModels.AuditLevelP1,
		fmt.Sprintf("管理员 [%s] 更新标准词根 %s (approved=%v)", opName, item.Term, item.IsApproved),
		"vocabulary_term",
		strconv.FormatUint(item.ID, 10),
		item.Term,
		beforeSnapshot,
		item,
	)

	c.JSON(http.StatusOK, item)
}

// DeleteVocabularyTerm 删除标准词根 DELETE /api/vocabulary-terms/:id (AdminOnly)
func DeleteVocabularyTerm(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的词根 ID"})
		return
	}

	var item models.VocabularyTerm
	if err := models.DB.First(&item, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "未找到目标标准词根"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询词根失败"})
		return
	}

	if err := models.DB.Delete(&item).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除词根失败"})
		return
	}

	_, opName := currentOperatorName(c)
	commonAudit.SetAuditContext(
		c,
		"traefik_linter",
		"delete_vocab",
		commonModels.AuditLevelP1,
		fmt.Sprintf("管理员 [%s] 删除词根 %s", opName, item.Term),
		"vocabulary_term",
		strconv.FormatUint(item.ID, 10),
		item.Term,
		item,
		nil,
	)

	c.JSON(http.StatusOK, gin.H{"message": "标准词根已删除", "id": item.ID})
}

type triggerScanPayload struct {
	GitRepoID         *uint64  `json:"git_repo_id"`
	BundleID          *uint64  `json:"bundle_id"`
	ProtoFileID       *uint64  `json:"proto_file_id"`
	ProtoFileIDs      []uint64 `json:"proto_file_ids"`
	SchemaID          *uint64  `json:"schema_id"`
	SchemaIDs         []uint64 `json:"schema_ids"`
	IncludeDuplicates *bool    `json:"include_duplicates"`
	IncludeLLM        *bool    `json:"include_llm"`
	Files             []struct {
		ProtoFile string `json:"proto_file"`
		Content   string `json:"content"`
		Category  string `json:"category"`
	} `json:"files"`
}

// TriggerCheckReportScan 手动触发指定作用域（描述集/虚拟文件/源码切片）静态检查与重复探测 POST /api/check-reports/scan (JWT)
func TriggerCheckReportScan(c *gin.Context) {
	var req triggerScanPayload
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, http.ErrBodyReadAfterClose) && c.Request.ContentLength > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求载荷格式非法: " + err.Error()})
		return
	}

	includeDuplicates := true
	if req.IncludeDuplicates != nil {
		includeDuplicates = *req.IncludeDuplicates
	}
	includeLLM := false
	if req.IncludeLLM != nil {
		includeLLM = *req.IncludeLLM
	}

	sources := make([]linter.SourceInput, 0, len(req.Files))
	for _, f := range req.Files {
		if strings.TrimSpace(f.ProtoFile) == "" || strings.TrimSpace(f.Content) == "" {
			continue
		}
		sources = append(sources, linter.SourceInput{
			ProtoFile: strings.TrimSpace(f.ProtoFile),
			Content:   f.Content,
			Category:  strings.TrimSpace(f.Category),
		})
	}

	schemaIDs := append([]uint64{}, req.SchemaIDs...)
	realProtoFileCount := int64(0)
	if len(req.ProtoFileIDs) > 0 && models.DB != nil {
		_ = models.DB.Model(&models.ProtoFile{}).Where("id IN ?", req.ProtoFileIDs).Count(&realProtoFileCount).Error
	}
	if realProtoFileCount == 0 {
		for _, fid := range req.ProtoFileIDs {
			if fid >= 1000 {
				schemaIDs = append(schemaIDs, fid/1000)
			}
		}
	}
	protoFileID := req.ProtoFileID
	if protoFileID == nil && len(req.ProtoFileIDs) == 1 {
		idCopy := req.ProtoFileIDs[0]
		protoFileID = &idCopy
	}

	uid, opName := currentOperatorName(c)
	report, err := linter.RunAndPersistScan(c.Request.Context(), linter.ScanRequest{
		DB:                models.DB,
		TriggerSource:     models.TriggerSourceManual,
		TriggeredBy:       uid,
		GitRepoID:         req.GitRepoID,
		BundleID:          req.BundleID,
		ProtoFileID:       protoFileID,
		SchemaID:          req.SchemaID,
		SchemaIDs:         schemaIDs,
		Sources:           sources,
		IncludeDuplicates: includeDuplicates,
		IncludeLLM:        includeLLM,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "执行静态检查扫描失败: " + err.Error()})
		return
	}

	commonAudit.SetAuditContext(
		c,
		"traefik_linter",
		"run_scan",
		commonModels.AuditLevelP2,
		fmt.Sprintf("[%s] 触发静态检查报告 #%d (status=%s, err=%d, warn=%d, dup=%d)", opName, report.ID, report.Status, report.ErrorCount, report.WarningCount, report.DuplicateGroupCount),
		"check_report",
		strconv.FormatUint(report.ID, 10),
		fmt.Sprintf("check-report-%d", report.ID),
		nil,
		gin.H{
			"report_id":             report.ID,
			"status":                report.Status,
			"error_count":           report.ErrorCount,
			"warning_count":         report.WarningCount,
			"duplicate_group_count": report.DuplicateGroupCount,
			"llm_status":            report.LLMStatus,
		},
	)

	c.JSON(http.StatusOK, report)
}

// ListCheckReports 分页查询静态检查历史报告 GET /api/check-reports (JWT)
func ListCheckReports(c *gin.Context) {
	page, pageSize := parseStandardPagination(c)
	query := models.DB.Model(&models.CheckReport{})

	if sidRaw := strings.TrimSpace(c.Query("schema_id")); sidRaw != "" {
		if sid, err := strconv.ParseUint(sidRaw, 10, 64); err == nil && sid > 0 {
			query = query.Where("schema_id = ?", sid)
		}
	}
	if status := strings.ToUpper(strings.TrimSpace(c.Query("status"))); status != "" {
		query = query.Where("status = ?", status)
	}
	if src := strings.ToUpper(strings.TrimSpace(c.Query("trigger_source"))); src != "" {
		query = query.Where("trigger_source = ?", src)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "统计检查报告失败"})
		return
	}

	var items []models.CheckReport
	offset := (page - 1) * pageSize
	if err := query.Order("id DESC").Offset(offset).Limit(pageSize).Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询检查报告列表失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetCheckReportDetail 查询单次检查报告详情（含 Violations 明细与 DuplicateGroupDTO 聚合视图）GET /api/check-reports/:id (JWT)
func GetCheckReportDetail(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的报告 ID"})
		return
	}

	var report models.CheckReport
	if err := models.DB.First(&report, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "未找到目标静态检查报告"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询检查报告失败"})
		return
	}

	var violations []models.CheckViolation
	if err := models.DB.Where("report_id = ?", report.ID).Order("severity ASC, proto_file ASC, file_line ASC, id ASC").Find(&violations).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询报告违规明细失败"})
		return
	}
	if violations == nil {
		violations = []models.CheckViolation{}
	}
	report.Violations = violations
	report.DuplicateGroups = linter.RebuildDuplicateGroupsFromViolations(violations)
	if report.DuplicateGroups == nil {
		report.DuplicateGroups = []models.DuplicateGroupDTO{}
	}

	c.JSON(http.StatusOK, report)
}
