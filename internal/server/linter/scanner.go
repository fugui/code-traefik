package linter

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"code-traefik/internal/server/protodoc"
	"code-traefik/internal/server/security"
	"code-traefik/models"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// EvaluateSources 对内存中的 .proto 源或描述符集合执行静态检查、词汇识别、3s 熔断 LLM 与三护栏重复探测。
func EvaluateSources(ctx context.Context, rules []models.CheckRule, vocab []models.VocabularyTerm, sources []SourceInput, opts EvaluateOptions) (*EvaluateOutput, error) {
	started := time.Now()
	if opts.TriggerSource == "" {
		opts.TriggerSource = models.TriggerSourceManual
	}

	parsedFiles, err := parseInputsToAST(ctx, sources)
	if err != nil {
		return nil, err
	}

	violations := make([]models.CheckViolation, 0)
	violations = append(violations, evaluateStaticRules(parsedFiles, rules)...)

	vocabViolations, llmStatus := evaluateVocabAndLLM(ctx, parsedFiles, vocab, rules, opts)
	violations = append(violations, vocabViolations...)

	var dupGroups []models.DuplicateGroupDTO
	if opts.IncludeDuplicates {
		var dupErr error
		dupGroups, dupErr = detectDuplicatesOnParsed(ctx, parsedFiles, rules, opts.SimilarityThreshold, nil)
		if dupErr != nil {
			return nil, dupErr
		}
		var dupRule models.CheckRule
		for _, r := range rules {
			if r.CheckMode == models.RuleModeASTDuplicate {
				dupRule = r
				break
			}
		}
		if dupRule.RuleCode == "" {
			dupRule = models.CheckRule{
				RuleCode: "DUPLICATE_MESSAGE",
				Severity: models.RuleSeverityWarning,
			}
		}
		for _, g := range dupGroups {
			if len(g.Members) == 0 {
				continue
			}
			metaBytes, mErr := json.Marshal(g)
			if mErr != nil {
				log.Printf("[linter] 序列化重复结构体簇元数据失败: %v", mErr)
				metaBytes = []byte("{}")
			}
			primary := g.Members[0]
			violations = append(violations, models.CheckViolation{
				RuleID:               ruleIDPtr(dupRule),
				RuleCode:             dupRule.RuleCode,
				Severity:             dupRule.Severity,
				ProtoFile:            primary.ProtoFile,
				SymbolPath:           joinFullName(primary.Package, primary.Message),
				FileLine:             primary.FileLine,
				FileColumn:           1,
				Message:              fmt.Sprintf("AST 结构体重复探测命中簇 #%s（相似度 %.2f%%）", g.ClusterHash, g.Similarity*100),
				Suggestion:           g.Suggestion,
				SuggestionSource:     models.SuggestionSourceASTDup,
				DuplicateClusterHash: g.ClusterHash,
				DuplicateSimilarity:  g.Similarity,
				MetadataJSON:         datatypes.JSON(metaBytes),
			})
		}
	}

	var errCnt, warnCnt, infoCnt int
	for _, v := range violations {
		switch strings.ToUpper(v.Severity) {
		case models.RuleSeverityError:
			errCnt++
		case models.RuleSeverityInfo:
			infoCnt++
		default:
			warnCnt++
		}
	}

	status := models.ReportStatusPassed
	if errCnt > 0 {
		status = models.ReportStatusFailed
	} else if warnCnt > 0 {
		status = models.ReportStatusWarn
	}

	report := &models.CheckReport{
		TriggerSource:       opts.TriggerSource,
		Status:              status,
		ErrorCount:          errCnt,
		WarningCount:        warnCnt,
		InfoCount:           infoCnt,
		DuplicateGroupCount: len(dupGroups),
		LLMStatus:           llmStatus,
		DurationMs:          int(time.Since(started).Milliseconds()),
		TriggeredBy:         opts.TriggeredBy,
		Violations:          violations,
		DuplicateGroups:     dupGroups,
	}

	return &EvaluateOutput{
		Report:          report,
		Violations:      violations,
		DuplicateGroups: dupGroups,
	}, nil
}

// RunAndPersistScan 从数据库加载规则、词库、目标 Schema 与加密 LLM 参数，执行扫描并将 CheckReport 与 CheckViolation 事务落库。
func RunAndPersistScan(ctx context.Context, req ScanRequest) (*models.CheckReport, error) {
	if req.DB == nil {
		return nil, fmt.Errorf("db instance is nil")
	}

	var rules []models.CheckRule
	if err := req.DB.Order("id ASC").Find(&rules).Error; err != nil {
		return nil, fmt.Errorf("读取静态检查规则失败: %w", err)
	}
	var vocab []models.VocabularyTerm
	if err := req.DB.Order("id ASC").Find(&vocab).Error; err != nil {
		return nil, fmt.Errorf("读取标准词汇库失败: %w", err)
	}

	sources := req.Sources
	if len(sources) == 0 {
		var err error
		sources, err = loadSourcesFromDB(req)
		if err != nil {
			return nil, err
		}
	}

	llmCfg := req.LLMOverride
	if req.IncludeLLM && llmCfg == nil {
		llmCfg = loadLLMSettingsFromDB(req.DB)
	}

	out, err := EvaluateSources(ctx, rules, vocab, sources, EvaluateOptions{
		TriggerSource:     req.TriggerSource,
		TriggeredBy:       req.TriggeredBy,
		IncludeDuplicates: req.IncludeDuplicates,
		IncludeLLM:        req.IncludeLLM,
		LLMOverride:       llmCfg,
		VocabWriter:       req.DB,
	})
	if err != nil {
		return nil, err
	}

	report := out.Report
	report.GitRepoID = req.GitRepoID
	report.BundleID = req.BundleID
	report.ProtoFileID = req.ProtoFileID
	report.SchemaID = req.SchemaID
	report.Violations = nil

	txErr := req.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(report).Error; err != nil {
			return err
		}
		if len(out.Violations) > 0 {
			for i := range out.Violations {
				out.Violations[i].ReportID = report.ID
				if len(out.Violations[i].MetadataJSON) == 0 {
					out.Violations[i].MetadataJSON = datatypes.JSON([]byte(`{}`))
				}
			}
			if err := tx.CreateInBatches(&out.Violations, 100).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if txErr != nil {
		return nil, fmt.Errorf("保存扫描报告失败: %w", txErr)
	}

	report.Violations = out.Violations
	report.DuplicateGroups = out.DuplicateGroups
	return report, nil
}

// RebuildDuplicateGroupsFromViolations 从持久化的 CheckViolation 明细还原 DuplicateGroupDTO 视图列表。
func RebuildDuplicateGroupsFromViolations(violations []models.CheckViolation) []models.DuplicateGroupDTO {
	seen := make(map[string]struct{})
	groups := make([]models.DuplicateGroupDTO, 0)
	for _, v := range violations {
		if v.SuggestionSource != models.SuggestionSourceASTDup || v.DuplicateClusterHash == "" {
			continue
		}
		if _, ok := seen[v.DuplicateClusterHash]; ok {
			continue
		}
		seen[v.DuplicateClusterHash] = struct{}{}
		var dto models.DuplicateGroupDTO
		if len(v.MetadataJSON) > 0 && json.Unmarshal(v.MetadataJSON, &dto) == nil && dto.ClusterHash != "" {
			groups = append(groups, dto)
			continue
		}
		groups = append(groups, models.DuplicateGroupDTO{
			ClusterHash: v.DuplicateClusterHash,
			Similarity:  v.DuplicateSimilarity,
			Suggestion:  v.Suggestion,
			Members: []models.DuplicateMemberDTO{
				{
					ProtoFile: v.ProtoFile,
					Package:   "",
					Message:   v.SymbolPath,
					FileLine:  v.FileLine,
				},
			},
		})
	}
	return groups
}

func loadSourcesFromDB(req ScanRequest) ([]SourceInput, error) {
	var targetSchemas []models.ProtoSchema
	targetIDs := make([]uint64, 0, len(req.SchemaIDs)+1)
	if req.SchemaID != nil && *req.SchemaID > 0 {
		targetIDs = append(targetIDs, *req.SchemaID)
	}
	targetIDs = append(targetIDs, req.SchemaIDs...)

	if len(targetIDs) == 0 && req.ProtoFileID != nil && *req.ProtoFileID >= 1000 {
		sid := *req.ProtoFileID / 1000
		targetIDs = append(targetIDs, sid)
	}

	if len(targetIDs) > 0 {
		if err := req.DB.Where("id IN ?", targetIDs).Order("id ASC").Find(&targetSchemas).Error; err != nil {
			return nil, fmt.Errorf("查询目标 ProtoSchema 失败: %w", err)
		}
	} else {
		var allSchemas []models.ProtoSchema
		if err := req.DB.Order("name ASC, is_active DESC, version DESC, id DESC").Find(&allSchemas).Error; err != nil {
			return nil, fmt.Errorf("查询最新 ProtoSchema 清单失败: %w", err)
		}
		seenName := make(map[string]struct{})
		for _, sc := range allSchemas {
			if _, ok := seenName[sc.Name]; ok {
				continue
			}
			seenName[sc.Name] = struct{}{}
			targetSchemas = append(targetSchemas, sc)
		}
	}

	sources := make([]SourceInput, 0)
	for i := range targetSchemas {
		sc := &targetSchemas[i]
		protodoc.EnsureSymbolsForSchema(req.DB, sc)
		var syms []models.ProtoDocSymbol
		if symsErr := req.DB.Where("schema_id = ?", sc.ID).Find(&syms).Error; symsErr != nil {
			log.Printf("[linter] 查询 Schema #%d 文档符号失败（继续使用基础描述符）: %v", sc.ID, symsErr)
		}
		reconstructed, err := protodoc.ReconstructProtoSources(sc, syms)
		if err != nil {
			continue
		}

		// 若描述集本身携带有完整 FileDescriptorProto（含 SourceCodeInfo），保留之以提取精确字段类型与包级信息
		fdByPath := make(map[string]protoreflectFileRef)
		if len(sc.DescriptorBytes) > 0 {
			var fds descriptorpb.FileDescriptorSet
			if proto.Unmarshal(sc.DescriptorBytes, &fds) == nil {
				if reg, regErr := protodesc.NewFiles(&fds); regErr == nil {
					for _, rawFD := range fds.GetFile() {
						if fd, findErr := reg.FindFileByPath(rawFD.GetName()); findErr == nil {
							fdByPath[rawFD.GetName()] = protoreflectFileRef{fd: fd, hasSourceInfo: rawFD.GetSourceCodeInfo() != nil && len(rawFD.GetSourceCodeInfo().GetLocation()) > 0}
						}
					}
				}
			}
		}

		for _, rf := range reconstructed {
			if req.ProtoFileID != nil && *req.ProtoFileID >= 1000 && rf.FileID != *req.ProtoFileID {
				continue
			}
			in := SourceInput{
				ProtoFile: rf.FilePath,
				Content:   rf.Content,
				Category:  sc.Category,
				SchemaID:  sc.ID,
			}
			if ref, ok := fdByPath[rf.FilePath]; ok {
				in.FileDescriptor = ref.fd
				// 若二进制上传未含 SourceCodeInfo 且重建源码完整，使用重建源码触发精确 AST 行号定位；若含 SourceCodeInfo 可直接由 FileDescriptor 提取真实注释
				if ref.hasSourceInfo {
					in.Content = ""
				}
			}
			sources = append(sources, in)
		}
	}

	return sources, nil
}

type protoreflectFileRef struct {
	fd            protoreflect.FileDescriptor
	hasSourceInfo bool
}

func loadLLMSettingsFromDB(db *gorm.DB) *LLMSettings {
	var rows []models.TraefikSetting
	if err := db.Where("setting_key IN ?", []string{
		"llm_enabled",
		"llm_base_url",
		"llm_model",
		"llm_timeout_ms",
		"llm_api_key_enc",
		"outbound_trusted_cidrs",
	}).Find(&rows).Error; err != nil {
		return nil
	}
	kv := make(map[string]string, len(rows))
	for _, r := range rows {
		kv[r.SettingKey] = strings.TrimSpace(r.SettingVal)
	}

	enabled := strings.EqualFold(kv["llm_enabled"], "true") || kv["llm_enabled"] == "1"
	timeoutMs := 3000
	if parsed, err := strconv.Atoi(kv["llm_timeout_ms"]); err == nil && parsed > 0 {
		timeoutMs = parsed
	}
	var apiKey string
	if encVal := kv["llm_api_key_enc"]; encVal != "" {
		dec, err := security.DecryptSecret(encVal)
		if err != nil && enabled {
			// 解密失败时保留空 apiKey 并依赖调用或拒绝未授权，不泄漏密文
			apiKey = ""
		} else {
			apiKey = dec
		}
	}
	return &LLMSettings{
		Enabled:              enabled,
		BaseURL:              kv["llm_base_url"],
		APIKey:               apiKey,
		Model:                kv["llm_model"],
		TimeoutMs:            timeoutMs,
		OutboundTrustedCIDRs: kv["outbound_trusted_cidrs"],
	}
}
