package models

import (
	"errors"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 静态检查与词汇治理领域常量
const (
	RuleCategoryNaming    = "NAMING"
	RuleCategoryComment   = "COMMENT"
	RuleCategoryPackage   = "PACKAGE"
	RuleCategoryStyle     = "STYLE"
	RuleCategoryDuplicate = "DUPLICATE"
	RuleCategoryVocab     = "VOCAB"

	RuleScopePackage   = "PACKAGE"
	RuleScopeService   = "SERVICE"
	RuleScopeRPC       = "RPC"
	RuleScopeMessage   = "MESSAGE"
	RuleScopeField     = "FIELD"
	RuleScopeEnum      = "ENUM"
	RuleScopeEnumValue = "ENUM_VALUE"

	RuleSeverityError   = "ERROR"
	RuleSeverityWarning = "WARNING"
	RuleSeverityInfo    = "INFO"

	RuleModeRegex        = "REGEX"
	RuleModeStructure    = "STRUCTURE"
	RuleModeComment      = "COMMENT"
	RuleModeASTDuplicate = "AST_DUPLICATE"
	RuleModeVocab        = "VOCAB"

	VocabSourceManual       = "MANUAL"
	VocabSourceBuiltinSeed  = "BUILTIN_SEED"
	VocabSourceLLMSuggested = "LLM_SUGGESTED"

	CheckTriggerGitSync  = "GIT_SYNC"
	CheckTriggerMRGate   = "MR_GATE"
	CheckTriggerSchedule = "SCHEDULE"

	ReportStatusPassed = "PASSED"
	ReportStatusWarn   = "WARN"
	ReportStatusFailed = "FAILED"

	LLMStatusSucceeded       = "SUCCEEDED"
	LLMStatusHitCache        = "HIT_CACHE"
	LLMStatusTimeoutFallback = "TIMEOUT_FALLBACK"
	LLMStatusErrorFallback   = "ERROR_FALLBACK"
	LLMStatusSkipped         = "SKIPPED"

	SuggestionSourceRuleTemplate = "RULE_TEMPLATE"
	SuggestionSourceVocabMatch   = "VOCAB_MATCH"
	SuggestionSourceASTDup       = "AST_DUP"
	SuggestionSourceLLMSuggested = "LLM_SUGGESTED"
)

// RuleExemptionConfig 规则豁免配置结构（对应 exemption_rule JSONB）
type RuleExemptionConfig struct {
	ExcludePaths      []string `json:"exclude_paths,omitempty"`
	ExcludePackages   []string `json:"exclude_packages,omitempty"`
	ExcludeCategories []string `json:"exclude_categories,omitempty"`
}

// DuplicateMemberDTO 重复结构体簇成员契约（冻结）
type DuplicateMemberDTO struct {
	ProtoFile string `json:"proto_file"`
	Package   string `json:"package"`
	Message   string `json:"message"`
	FileLine  int    `json:"file_line"`
	FieldCnt  int    `json:"field_cnt"`
}

// DuplicateGroupDTO 重复结构体探测簇前端渲染 DTO（冻结）
type DuplicateGroupDTO struct {
	ClusterHash string               `json:"cluster_hash"`
	Similarity  float64              `json:"similarity"` // Jaccard >= 0.85
	Suggestion  string               `json:"suggestion"`
	Members     []DuplicateMemberDTO `json:"members"`
}

// CheckRule #25 Protobuf 静态检查规则配置表
type CheckRule struct {
	ID                 uint64         `gorm:"primaryKey" json:"id"`
	RuleCode           string         `gorm:"size:64;uniqueIndex;not null" json:"rule_code"`
	Name               string         `gorm:"size:128;not null" json:"name"`
	Category           string         `gorm:"size:32;not null;index" json:"category"`
	TargetScope        string         `gorm:"size:32;not null" json:"target_scope"`
	Severity           string         `gorm:"size:16;not null;default:'WARNING'" json:"severity"`
	CheckMode          string         `gorm:"size:32;not null;default:'REGEX'" json:"check_mode"`
	Pattern            string         `gorm:"type:text" json:"pattern"`
	Description        string         `gorm:"type:text" json:"description"`
	SuggestionTemplate string         `gorm:"type:text" json:"suggestion_template"`
	ExemptionRule      datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"exemption_rule"`
	IsBuiltin          bool           `gorm:"not null;default:false" json:"is_builtin"`
	IsEnabled          bool           `gorm:"not null;default:true;index" json:"is_enabled"`
	UpdatedBy          uint           `gorm:"default:0" json:"updated_by"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
}

// TableName 指定带模块前缀的数据表名
func (CheckRule) TableName() string {
	return "traefik_check_rules"
}

// VocabularyTerm #26 Protobuf 标准词汇/词根库表
type VocabularyTerm struct {
	ID            uint64         `gorm:"primaryKey" json:"id"`
	Term          string         `gorm:"size:64;uniqueIndex;not null" json:"term"`
	DescriptionEn string         `gorm:"size:256" json:"description_en"`
	DescriptionZh string         `gorm:"size:256;not null" json:"description_zh"`
	Aliases       datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"aliases"`
	SimilarTerms  datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"similar_terms"`
	Domain        string         `gorm:"size:64;index;not null;default:'GENERAL'" json:"domain"`
	Source        string         `gorm:"size:32;not null;default:'MANUAL';index" json:"source"`
	IsApproved    bool           `gorm:"not null;default:true;index" json:"is_approved"`
	CreatedBy     uint           `gorm:"default:0" json:"created_by"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

// TableName 指定带模块前缀的数据表名
func (VocabularyTerm) TableName() string {
	return "traefik_vocabulary_terms"
}

// CheckReport #27 Protobuf 静态检查报告汇总表
type CheckReport struct {
	ID                  uint64              `gorm:"primaryKey" json:"id"`
	GitRepoID           *uint64             `gorm:"index" json:"git_repo_id,omitempty"`
	BundleID            *uint64             `gorm:"index" json:"bundle_id,omitempty"`
	ProtoFileID         *uint64             `gorm:"index" json:"proto_file_id,omitempty"`
	SchemaID            *uint64             `gorm:"index" json:"schema_id,omitempty"`
	MRCheckRunID        *uint64             `gorm:"index" json:"mr_check_run_id,omitempty"`
	TriggerSource       string              `gorm:"size:32;not null;default:'MANUAL'" json:"trigger_source"`
	Status              string              `gorm:"size:16;not null" json:"status"`
	ErrorCount          int                 `gorm:"not null;default:0" json:"error_count"`
	WarningCount        int                 `gorm:"not null;default:0" json:"warning_count"`
	InfoCount           int                 `gorm:"not null;default:0" json:"info_count"`
	DuplicateGroupCount int                 `gorm:"not null;default:0" json:"duplicate_group_count"`
	LLMStatus           string              `gorm:"size:32;not null;default:'SKIPPED'" json:"llm_status"`
	DurationMs          int                 `gorm:"not null;default:0" json:"duration_ms"`
	TriggeredBy         uint                `gorm:"default:0" json:"triggered_by"`
	CreatedAt           time.Time           `json:"created_at"`
	Violations          []CheckViolation    `gorm:"foreignKey:ReportID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"violations,omitempty"`
	DuplicateGroups     []DuplicateGroupDTO `gorm:"-" json:"duplicate_groups,omitempty"`
}

// TableName 指定带模块前缀的数据表名
func (CheckReport) TableName() string {
	return "traefik_check_reports"
}

// CheckViolation #28 Protobuf 检查报告明细项表
type CheckViolation struct {
	ID                   uint64         `gorm:"primaryKey" json:"id"`
	ReportID             uint64         `gorm:"index;not null" json:"report_id"`
	RuleID               *uint64        `gorm:"index" json:"rule_id,omitempty"`
	RuleCode             string         `gorm:"size:64;not null;index" json:"rule_code"`
	Severity             string         `gorm:"size:16;not null;index" json:"severity"`
	ProtoFile            string         `gorm:"size:512;not null;index" json:"proto_file"`
	SymbolPath           string         `gorm:"size:256;not null;index" json:"symbol_path"`
	FileLine             int            `gorm:"not null;default:1" json:"file_line"`
	FileColumn           int            `gorm:"not null;default:1" json:"file_column"`
	Message              string         `gorm:"type:text;not null" json:"message"`
	Suggestion           string         `gorm:"type:text" json:"suggestion"`
	SuggestionSource     string         `gorm:"size:32;not null;default:'RULE_TEMPLATE'" json:"suggestion_source"`
	DuplicateClusterHash string         `gorm:"size:64;index" json:"duplicate_cluster_hash,omitempty"`
	DuplicateSimilarity  float64        `gorm:"default:0" json:"duplicate_similarity,omitempty"`
	MetadataJSON         datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"metadata_json,omitempty"`
	CreatedAt            time.Time      `json:"created_at"`
}

// TableName 指定带模块前缀的数据表名
func (CheckViolation) TableName() string {
	return "traefik_check_violations"
}

// EnsureBuiltinCheckRules 幂等初始化内置静态检查规则（ON CONFLICT DO NOTHING，绝不覆盖管理员修改）
func EnsureBuiltinCheckRules(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	builtins := []CheckRule{
		{
			RuleCode:           "PKG_NAMING",
			Name:               "多段小写包名规范",
			Category:           RuleCategoryPackage,
			TargetScope:        RuleScopePackage,
			Severity:           RuleSeverityError,
			CheckMode:          RuleModeRegex,
			Pattern:            `^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*(\.v[0-9]+((alpha|beta|rc)[0-9]*)?)?$`,
			Description:        "Protobuf package 命名必须为小写点分格式（支持多段如 dophi.order.v1），禁止大写字母、中划线或前置点",
			SuggestionTemplate: "包名 {symbol} 应采用多段小写点分格式并建议以版本结尾，例如 {example}",
			ExemptionRule:      datatypes.JSON([]byte(`{}`)),
			IsBuiltin:          true,
			IsEnabled:          true,
		},
		{
			RuleCode:           "SVC_PASCAL_CASE",
			Name:               "Service 大驼峰命名",
			Category:           RuleCategoryNaming,
			TargetScope:        RuleScopeService,
			Severity:           RuleSeverityError,
			CheckMode:          RuleModeRegex,
			Pattern:            `^[A-Z][A-Za-z0-9]*$`,
			Description:        "Service 服务定义名必须采用 PascalCase 大驼峰格式，且不含下划线",
			SuggestionTemplate: "服务名 {symbol} 应采用大驼峰 (PascalCase) 命名，例如 {example}",
			ExemptionRule:      datatypes.JSON([]byte(`{}`)),
			IsBuiltin:          true,
			IsEnabled:          true,
		},
		{
			RuleCode:           "RPC_PASCAL_CASE",
			Name:               "RPC 方法大驼峰命名",
			Category:           RuleCategoryNaming,
			TargetScope:        RuleScopeRPC,
			Severity:           RuleSeverityError,
			CheckMode:          RuleModeRegex,
			Pattern:            `^[A-Z][A-Za-z0-9]*$`,
			Description:        "RPC 方法名必须采用 PascalCase 大驼峰格式，且不含下划线",
			SuggestionTemplate: "RPC 方法名 {symbol} 应采用大驼峰 (PascalCase) 命名，例如 {example}",
			ExemptionRule:      datatypes.JSON([]byte(`{}`)),
			IsBuiltin:          true,
			IsEnabled:          true,
		},
		{
			RuleCode:           "MSG_PASCAL_CASE",
			Name:               "Message 大驼峰命名",
			Category:           RuleCategoryNaming,
			TargetScope:        RuleScopeMessage,
			Severity:           RuleSeverityError,
			CheckMode:          RuleModeRegex,
			Pattern:            `^[A-Z][A-Za-z0-9]*$`,
			Description:        "Message 结构体名称必须采用 PascalCase 大驼峰格式",
			SuggestionTemplate: "消息名 {symbol} 应采用大驼峰 (PascalCase) 命名，例如 {example}",
			ExemptionRule:      datatypes.JSON([]byte(`{}`)),
			IsBuiltin:          true,
			IsEnabled:          true,
		},
		{
			RuleCode:           "FIELD_SNAKE_CASE",
			Name:               "Field 小写下划线命名",
			Category:           RuleCategoryNaming,
			TargetScope:        RuleScopeField,
			Severity:           RuleSeverityError,
			CheckMode:          RuleModeRegex,
			Pattern:            `^[a-z][a-z0-9]*(_[a-z0-9]+)*$`,
			Description:        "Message 字段必须采用 lowercase snake_case 命名（禁止驼峰与大写字母）",
			SuggestionTemplate: "字段名 {symbol} 应采用小写下划线 (snake_case) 命名，例如 {expected}",
			ExemptionRule:      datatypes.JSON([]byte(`{}`)),
			IsBuiltin:          true,
			IsEnabled:          true,
		},
		{
			RuleCode:           "ENUM_UPPER_SNAKE_WITH_PREFIX",
			Name:               "Enum 枚举值全大写下划线及前缀规范",
			Category:           RuleCategoryNaming,
			TargetScope:        RuleScopeEnumValue,
			Severity:           RuleSeverityWarning,
			CheckMode:          RuleModeStructure,
			Pattern:            `^[A-Z][A-Z0-9]*(_[A-Z0-9]+)*$`,
			Description:        "枚举值应使用 UPPER_SNAKE_CASE 格式，并推荐携带枚举名 UPPER_SNAKE 前缀避免同包命名冲突",
			SuggestionTemplate: "枚举值 {symbol} 建议使用大写下划线并带枚举前缀，例如 {expected}",
			ExemptionRule:      datatypes.JSON([]byte(`{}`)),
			IsBuiltin:          true,
			IsEnabled:          true,
		},
		{
			RuleCode:           "ENUM_ZERO_UNSPECIFIED",
			Name:               "Enum 0号枚举 UNSPECIFIED 规范",
			Category:           RuleCategoryStyle,
			TargetScope:        RuleScopeEnumValue,
			Severity:           RuleSeverityWarning,
			CheckMode:          RuleModeStructure,
			Pattern:            `.*_UNSPECIFIED$`,
			Description:        "Proto3 枚举 0 号默认值应以 _UNSPECIFIED 结尾，明确未赋值状态语义",
			SuggestionTemplate: "枚举 0 号默认值 {symbol} 建议命名为 {expected}",
			ExemptionRule:      datatypes.JSON([]byte(`{}`)),
			IsBuiltin:          true,
			IsEnabled:          true,
		},
		{
			RuleCode:           "COMMENT_REQUIRED",
			Name:               "Service/RPC/Message 前导注释要求",
			Category:           RuleCategoryComment,
			TargetScope:        RuleScopeRPC,
			Severity:           RuleSeverityWarning,
			CheckMode:          RuleModeComment,
			Pattern:            "",
			Description:        "核心 Service 与 RPC 方法建议补充清晰的前导说明注释（默认 WARNING，不阻塞历史基线）",
			SuggestionTemplate: "请为 {symbol} 补充清晰的前导业务说明注释（如：// 获取订单详情）",
			ExemptionRule:      datatypes.JSON([]byte(`{}`)),
			IsBuiltin:          true,
			IsEnabled:          true,
		},
		{
			RuleCode:           "VOCAB_STANDARD_TERM",
			Name:               "团队标准词汇词根规范",
			Category:           RuleCategoryVocab,
			TargetScope:        RuleScopeField,
			Severity:           RuleSeverityWarning,
			CheckMode:          RuleModeVocab,
			Pattern:            "",
			Description:        "切分字段标识符并匹配企业标准词根库，识别非标准简写别名并提供标准词根替换建议",
			SuggestionTemplate: "字段词根 {symbol} 非标准缩写，建议替换为标准词根 {expected}",
			ExemptionRule:      datatypes.JSON([]byte(`{}`)),
			IsBuiltin:          true,
			IsEnabled:          true,
		},
		{
			RuleCode:           "DUPLICATE_MESSAGE",
			Name:               "AST 高相似度重复结构体探测",
			Category:           RuleCategoryDuplicate,
			TargetScope:        RuleScopeMessage,
			Severity:           RuleSeverityWarning,
			CheckMode:          RuleModeASTDuplicate,
			Pattern:            "0.85",
			Description:        "基于字段规范类型+语义词根倒排桶探测跨服务高相似度重复结构体（Jaccard >= 0.85）",
			SuggestionTemplate: "结构体与 {expected} 高度重复（相似度 {example}），建议抽离复用公共基础库 Message",
			ExemptionRule:      datatypes.JSON([]byte(`{"exclude_categories":["BASE_LIB"]}`)),
			IsBuiltin:          true,
			IsEnabled:          true,
		},
	}

	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "rule_code"}},
		DoNothing: true,
	}).Create(&builtins).Error
}

// EnsureBuiltinVocabularyTerms 幂等初始化基础标准词根库（BUILTIN_SEED）
func EnsureBuiltinVocabularyTerms(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	seeds := []VocabularyTerm{
		{
			Term:          "amt",
			DescriptionEn: "Amount",
			DescriptionZh: "金额/数额",
			Aliases:       datatypes.JSON([]byte(`["amnt","amt_val"]`)),
			SimilarTerms:  datatypes.JSON([]byte(`["amount","money","val"]`)),
			Domain:        "FINANCE",
			Source:        VocabSourceBuiltinSeed,
			IsApproved:    true,
		},
		{
			Term:          "ts",
			DescriptionEn: "Timestamp",
			DescriptionZh: "时间戳（毫秒/秒）",
			Aliases:       datatypes.JSON([]byte(`["timestmp","tmstmp","t_stamp"]`)),
			SimilarTerms:  datatypes.JSON([]byte(`["timestamp","time","created_at","updated_at"]`)),
			Domain:        "GENERAL",
			Source:        VocabSourceBuiltinSeed,
			IsApproved:    true,
		},
		{
			Term:          "uid",
			DescriptionEn: "User Identifier",
			DescriptionZh: "用户唯一标识",
			Aliases:       datatypes.JSON([]byte(`["usr_id","userid_val","uid_str"]`)),
			SimilarTerms:  datatypes.JSON([]byte(`["user_id","account_id"]`)),
			Domain:        "GENERAL",
			Source:        VocabSourceBuiltinSeed,
			IsApproved:    true,
		},
		{
			Term:          "id",
			DescriptionEn: "Identifier",
			DescriptionZh: "通用主键标识",
			Aliases:       datatypes.JSON([]byte(`["ident","idx_id"]`)),
			SimilarTerms:  datatypes.JSON([]byte(`["uid","key"]`)),
			Domain:        "GENERAL",
			Source:        VocabSourceBuiltinSeed,
			IsApproved:    true,
		},
		{
			Term:          "seq",
			DescriptionEn: "Sequence Number",
			DescriptionZh: "递增序列号/流水号",
			Aliases:       datatypes.JSON([]byte(`["seqno","sq_no"]`)),
			SimilarTerms:  datatypes.JSON([]byte(`["sequence","nonce"]`)),
			Domain:        "GENERAL",
			Source:        VocabSourceBuiltinSeed,
			IsApproved:    true,
		},
		{
			Term:          "qty",
			DescriptionEn: "Quantity",
			DescriptionZh: "数量/成交股数",
			Aliases:       datatypes.JSON([]byte(`["qunatity","qnty","qt"]`)),
			SimilarTerms:  datatypes.JSON([]byte(`["quantity","count","volume"]`)),
			Domain:        "TRADE",
			Source:        VocabSourceBuiltinSeed,
			IsApproved:    true,
		},
		{
			Term:          "ccy",
			DescriptionEn: "Currency Code",
			DescriptionZh: "ISO 货币币种代码",
			Aliases:       datatypes.JSON([]byte(`["crncy","curcy","curr_cd"]`)),
			SimilarTerms:  datatypes.JSON([]byte(`["currency"]`)),
			Domain:        "FINANCE",
			Source:        VocabSourceBuiltinSeed,
			IsApproved:    true,
		},
		{
			Term:          "px",
			DescriptionEn: "Price",
			DescriptionZh: "价格/委托价",
			Aliases:       datatypes.JSON([]byte(`["prc","prce"]`)),
			SimilarTerms:  datatypes.JSON([]byte(`["price","rate"]`)),
			Domain:        "TRADE",
			Source:        VocabSourceBuiltinSeed,
			IsApproved:    true,
		},
		{
			Term:          "fee",
			DescriptionEn: "Fee / Commission",
			DescriptionZh: "手续费/佣金",
			Aliases:       datatypes.JSON([]byte(`["comm_fee","fe"]`)),
			SimilarTerms:  datatypes.JSON([]byte(`["commission","charge"]`)),
			Domain:        "FINANCE",
			Source:        VocabSourceBuiltinSeed,
			IsApproved:    true,
		},
		{
			Term:          "acct",
			DescriptionEn: "Account",
			DescriptionZh: "资金/业务账户",
			Aliases:       datatypes.JSON([]byte(`["acnt","accnt","acc_no"]`)),
			SimilarTerms:  datatypes.JSON([]byte(`["account"]`)),
			Domain:        "FINANCE",
			Source:        VocabSourceBuiltinSeed,
			IsApproved:    true,
		},
		{
			Term:          "ord",
			DescriptionEn: "Order",
			DescriptionZh: "订单/委托单",
			Aliases:       datatypes.JSON([]byte(`["ordr"]`)),
			SimilarTerms:  datatypes.JSON([]byte(`["order"]`)),
			Domain:        "TRADE",
			Source:        VocabSourceBuiltinSeed,
			IsApproved:    true,
		},
		{
			Term:          "trd",
			DescriptionEn: "Trade / Deal",
			DescriptionZh: "成交/交易记录",
			Aliases:       datatypes.JSON([]byte(`["trde"]`)),
			SimilarTerms:  datatypes.JSON([]byte(`["trade","deal"]`)),
			Domain:        "TRADE",
			Source:        VocabSourceBuiltinSeed,
			IsApproved:    true,
		},
		{
			Term:          "sts",
			DescriptionEn: "Status",
			DescriptionZh: "状态标识",
			Aliases:       datatypes.JSON([]byte(`["stts","statuz"]`)),
			SimilarTerms:  datatypes.JSON([]byte(`["status","state"]`)),
			Domain:        "GENERAL",
			Source:        VocabSourceBuiltinSeed,
			IsApproved:    true,
		},
		{
			Term:          "err",
			DescriptionEn: "Error",
			DescriptionZh: "错误信息/错误对象",
			Aliases:       datatypes.JSON([]byte(`["errr","errmsg_raw"]`)),
			SimilarTerms:  datatypes.JSON([]byte(`["error","code","msg"]`)),
			Domain:        "GENERAL",
			Source:        VocabSourceBuiltinSeed,
			IsApproved:    true,
		},
		{
			Term:          "msg",
			DescriptionEn: "Message",
			DescriptionZh: "消息描述/提示文本",
			Aliases:       datatypes.JSON([]byte(`["mesg","mssg"]`)),
			SimilarTerms:  datatypes.JSON([]byte(`["message","desc"]`)),
			Domain:        "GENERAL",
			Source:        VocabSourceBuiltinSeed,
			IsApproved:    true,
		},
		{
			Term:          "req",
			DescriptionEn: "Request",
			DescriptionZh: "请求报文",
			Aliases:       datatypes.JSON([]byte(`["rqst","reqst"]`)),
			SimilarTerms:  datatypes.JSON([]byte(`["request"]`)),
			Domain:        "GENERAL",
			Source:        VocabSourceBuiltinSeed,
			IsApproved:    true,
		},
		{
			Term:          "rsp",
			DescriptionEn: "Response",
			DescriptionZh: "响应报文",
			Aliases:       datatypes.JSON([]byte(`["resp_obj","rslt_rsp"]`)),
			SimilarTerms:  datatypes.JSON([]byte(`["response","reply"]`)),
			Domain:        "GENERAL",
			Source:        VocabSourceBuiltinSeed,
			IsApproved:    true,
		},
		{
			Term:          "cfg",
			DescriptionEn: "Configuration",
			DescriptionZh: "配置参数",
			Aliases:       datatypes.JSON([]byte(`["confg","confs"]`)),
			SimilarTerms:  datatypes.JSON([]byte(`["config","setting"]`)),
			Domain:        "GENERAL",
			Source:        VocabSourceBuiltinSeed,
			IsApproved:    true,
		},
	}

	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "term"}},
		DoNothing: true,
	}).Create(&seeds).Error
}

// ensureLinterConstraints 为静态检查与词库表补充幂等 CHECK 约束
func ensureLinterConstraints(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	stmts := []string{
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_vocab_source') THEN
				ALTER TABLE traefik_vocabulary_terms ADD CONSTRAINT chk_traefik_vocab_source
					CHECK (source IN ('MANUAL', 'BUILTIN_SEED', 'LLM_SUGGESTED'));
			END IF;
		END $$;`,
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_check_rule_severity') THEN
				ALTER TABLE traefik_check_rules ADD CONSTRAINT chk_traefik_check_rule_severity
					CHECK (severity IN ('ERROR', 'WARNING', 'INFO'));
			END IF;
		END $$;`,
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_traefik_check_report_status') THEN
				ALTER TABLE traefik_check_reports ADD CONSTRAINT chk_traefik_check_report_status
					CHECK (status IN ('PASSED', 'WARN', 'FAILED'));
			END IF;
		END $$;`,
	}
	for _, stmt := range stmts {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

// ensureM24DefaultSettings 幂等补充 M24 SSRF 白名单与 LLM 默认系统参数
func ensureM24DefaultSettings(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	defaults := []TraefikSetting{
		{
			SettingKey:  "outbound_trusted_cidrs",
			SettingVal:  "",
			Description: "出向 HTTP/Git/LLM 受信网段白名单（逗号分隔 CIDR 或 host:port，云元数据 169.254.0.0/16 与 100.100.100.200/32 永久硬阻断）",
			UpdatedBy:   "system",
		},
		{
			SettingKey:  "llm_enabled",
			SettingVal:  "false",
			Description: "是否启用 3s 超时熔断 LLM 辅助词根识别",
			UpdatedBy:   "system",
		},
		{
			SettingKey:  "llm_base_url",
			SettingVal:  "",
			Description: "OpenAI 兼容 LLM 接口 Base URL（经 SafeDialer 校验）",
			UpdatedBy:   "system",
		},
		{
			SettingKey:  "llm_model",
			SettingVal:  "gpt-4o-mini",
			Description: "LLM 词根识别模型名称",
			UpdatedBy:   "system",
		},
		{
			SettingKey:  "llm_timeout_ms",
			SettingVal:  "3000",
			Description: "LLM 单次请求超时毫秒数（硬上限 3000ms）",
			UpdatedBy:   "system",
		},
		{
			SettingKey:  "code_notifier_enabled",
			SettingVal:  "false",
			Description: "是否启用跨机 LAN code-notifier 统一邮件通知通道",
			UpdatedBy:   "system",
		},
		{
			SettingKey:  "code_notifier_url",
			SettingVal:  "",
			Description: "code-notifier 服务地址（如 http://192.168.56.18:8090，经 SafeDialer 校验）",
			UpdatedBy:   "system",
		},
		{
			SettingKey:  "smtp_host",
			SettingVal:  "",
			Description: "备用 SMTP 邮件网关主机名",
			UpdatedBy:   "system",
		},
		{
			SettingKey:  "smtp_port",
			SettingVal:  "587",
			Description: "备用 SMTP 邮件网关端口",
			UpdatedBy:   "system",
		},
		{
			SettingKey:  "smtp_username",
			SettingVal:  "",
			Description: "备用 SMTP 认证用户名",
			UpdatedBy:   "system",
		},
		{
			SettingKey:  "smtp_from",
			SettingVal:  "",
			Description: "备用 SMTP 发件人邮箱地址",
			UpdatedBy:   "system",
		},
	}
	for _, item := range defaults {
		var existing TraefikSetting
		err := db.Where("setting_key = ?", item.SettingKey).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if createErr := db.Create(&item).Error; createErr != nil {
				return createErr
			}
		} else if err != nil {
			return err
		}
	}
	return nil
}
