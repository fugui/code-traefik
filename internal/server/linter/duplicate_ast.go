package linter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"code-traefik/models"
)

const (
	defaultDuplicateThreshold = 0.85
	maxDuplicatePairBudget    = 100000
)

var pkgVersionSuffixRe = regexp.MustCompile(`\.v[0-9]+((alpha|beta|rc)[0-9]*)?$`)

type dupCandidate struct {
	file      string
	pkg       string
	pkgPrefix string
	msg       parsedMessage
	tokens    map[string]struct{}
	tokenList []string
}

// DetectDuplicateMessages 执行三护栏倒排桶 AST 重复结构体探测（Jaccard >= threshold，默认 0.85）。
func DetectDuplicateMessages(ctx context.Context, inputs []SourceInput, rules []models.CheckRule, threshold float64) ([]models.DuplicateGroupDTO, error) {
	parsedFiles, err := parseInputsToAST(ctx, inputs)
	if err != nil {
		return nil, err
	}
	return detectDuplicatesOnParsed(ctx, parsedFiles, rules, threshold, nil)
}

func detectDuplicatesOnParsed(ctx context.Context, parsedFiles []parsedFile, rules []models.CheckRule, threshold float64, aliasNorm map[string]string) ([]models.DuplicateGroupDTO, error) {
	var dupRule *models.CheckRule
	for i := range rules {
		if rules[i].CheckMode == models.RuleModeASTDuplicate {
			if !rules[i].IsEnabled {
				return nil, nil
			}
			dupRule = &rules[i]
			break
		}
	}

	if threshold <= 0 {
		threshold = defaultDuplicateThreshold
		if dupRule != nil && strings.TrimSpace(dupRule.Pattern) != "" {
			if parsed, convErr := strconv.ParseFloat(strings.TrimSpace(dupRule.Pattern), 64); convErr == nil && parsed > 0 && parsed <= 1 {
				threshold = parsed
			}
		}
	}

	exempt := models.RuleExemptionConfig{ExcludeCategories: []string{"BASE_LIB"}}
	if dupRule != nil && len(dupRule.ExemptionRule) > 0 {
		exempt = parseExemptionRule(dupRule.ExemptionRule)
		hasBase := false
		for _, c := range exempt.ExcludeCategories {
			if strings.EqualFold(strings.TrimSpace(c), "BASE_LIB") {
				hasBase = true
				break
			}
		}
		if !hasBase {
			exempt.ExcludeCategories = append(exempt.ExcludeCategories, "BASE_LIB")
		}
	}

	candidates := make([]dupCandidate, 0)
	for _, pf := range parsedFiles {
		// 护栏 2：BASE_LIB 公共基础库与规则豁免路径/包直接跳过，防止公共基础结构被误报重复
		if strings.EqualFold(strings.TrimSpace(pf.Category), "BASE_LIB") || isExempted(exempt, pf.ProtoFile, pf.Package, pf.Category) {
			continue
		}
		pkgPrefix := stripPackageVersionSuffix(pf.Package)
		for _, msg := range pf.Messages {
			if msg.IsMapEntry {
				continue
			}
			// 护栏 1：有效字段数 < 3 的微消息（Empty / IdReq / PageReq）直接豁免
			if len(msg.Fields) < 3 {
				continue
			}
			tokenSet := make(map[string]struct{}, len(msg.Fields))
			for _, fld := range msg.Fields {
				stem := normalizeFieldStem(fld.Name, aliasNorm)
				tok := fmt.Sprintf("%s:%s:%s", fld.CanonicalType, stem, fld.Cardinality)
				tokenSet[tok] = struct{}{}
			}
			if len(tokenSet) < 3 {
				continue
			}
			tokenList := make([]string, 0, len(tokenSet))
			for tok := range tokenSet {
				tokenList = append(tokenList, tok)
			}
			sort.Strings(tokenList)
			candidates = append(candidates, dupCandidate{
				file:      pf.ProtoFile,
				pkg:       pf.Package,
				pkgPrefix: pkgPrefix,
				msg:       msg,
				tokens:    tokenSet,
				tokenList: tokenList,
			})
		}
	}

	if len(candidates) < 2 {
		return nil, nil
	}

	// 建立特征 Token -> 候选结构体索引的倒排桶
	invertedBuckets := make(map[string][]int)
	for idx, cand := range candidates {
		for _, tok := range cand.tokenList {
			invertedBuckets[tok] = append(invertedBuckets[tok], idx)
		}
	}

	type simEdge struct {
		a, b int
		sim  float64
	}
	edges := make([]simEdge, 0)
	pairEvaluations := 0

	for i := range candidates {
		if ctx.Err() != nil {
			break
		}
		sharedCounts := make(map[int]int)
		for _, tok := range candidates[i].tokenList {
			for _, j := range invertedBuckets[tok] {
				if j <= i {
					continue
				}
				sharedCounts[j]++
			}
		}

		// 排序候选索引保证结果确定性
		peerIDs := make([]int, 0, len(sharedCounts))
		for j := range sharedCounts {
			peerIDs = append(peerIDs, j)
		}
		sort.Ints(peerIDs)

		for _, j := range peerIDs {
			if pairEvaluations >= maxDuplicatePairBudget {
				break
			}
			// 护栏 3：同包前缀跨版本演进（如 dophi.order.v1 vs dophi.order.v2）豁免重复报警
			if candidates[i].pkgPrefix != "" &&
				candidates[i].pkgPrefix == candidates[j].pkgPrefix &&
				candidates[i].pkg != candidates[j].pkg {
				continue
			}
			shared := sharedCounts[j]
			minLen := len(candidates[i].tokenList)
			if len(candidates[j].tokenList) < minLen {
				minLen = len(candidates[j].tokenList)
			}
			minOverlap := int(math.Ceil(0.5 * float64(minLen)))
			if minOverlap < 2 {
				minOverlap = 2
			}
			if shared < minOverlap {
				continue
			}
			pairEvaluations++

			union := len(candidates[i].tokenList) + len(candidates[j].tokenList) - shared
			if union <= 0 {
				continue
			}
			jaccard := float64(shared) / float64(union)
			if jaccard+1e-9 >= threshold {
				edges = append(edges, simEdge{a: i, b: j, sim: math.Min(1.0, jaccard)})
			}
		}
		if pairEvaluations >= maxDuplicatePairBudget {
			break
		}
	}

	if len(edges) == 0 {
		return nil, nil
	}

	// 并查集将高相似边归并成重复结构簇
	parent := make([]int, len(candidates))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	unite := func(x, y int) {
		rx, ry := find(x), find(y)
		if rx != ry {
			if rx < ry {
				parent[ry] = rx
			} else {
				parent[rx] = ry
			}
		}
	}

	for _, e := range edges {
		unite(e.a, e.b)
	}

	clusterMembers := make(map[int][]int)
	clusterMinSim := make(map[int]float64)
	for _, e := range edges {
		root := find(e.a)
		if cur, ok := clusterMinSim[root]; !ok || e.sim < cur {
			clusterMinSim[root] = e.sim
		}
	}
	seenMember := make(map[int]struct{})
	for _, e := range edges {
		root := find(e.a)
		for _, idx := range []int{e.a, e.b} {
			if _, ok := seenMember[idx]; !ok {
				seenMember[idx] = struct{}{}
				clusterMembers[root] = append(clusterMembers[root], idx)
			}
		}
	}

	roots := make([]int, 0, len(clusterMembers))
	for r := range clusterMembers {
		roots = append(roots, r)
	}
	sort.Ints(roots)

	groups := make([]models.DuplicateGroupDTO, 0, len(roots))
	for _, r := range roots {
		idxs := clusterMembers[r]
		if len(idxs) < 2 {
			continue
		}
		members := make([]models.DuplicateMemberDTO, 0, len(idxs))
		memberKeys := make([]string, 0, len(idxs))
		names := make([]string, 0, len(idxs))
		for _, idx := range idxs {
			c := candidates[idx]
			members = append(members, models.DuplicateMemberDTO{
				ProtoFile: c.file,
				Package:   c.pkg,
				Message:   c.msg.Name,
				FileLine:  c.msg.Line,
				FieldCnt:  len(c.msg.Fields),
			})
		}
		sort.Slice(members, func(i, j int) bool {
			if members[i].Package != members[j].Package {
				return members[i].Package < members[j].Package
			}
			if members[i].Message != members[j].Message {
				return members[i].Message < members[j].Message
			}
			return members[i].ProtoFile < members[j].ProtoFile
		})
		for _, m := range members {
			memberKeys = append(memberKeys, fmt.Sprintf("%s:%s.%s", m.ProtoFile, m.Package, m.Message))
			names = append(names, fmt.Sprintf("%s.%s", m.Package, m.Message))
		}
		sum := sha256.Sum256([]byte(strings.Join(memberKeys, "|")))
		sim := math.Round(clusterMinSim[r]*10000) / 10000
		groups = append(groups, models.DuplicateGroupDTO{
			ClusterHash: hex.EncodeToString(sum[:8]),
			Similarity:  sim,
			Suggestion:  fmt.Sprintf("结构体 %s 字段拓扑相似度达 %.0f%%，建议抽取公共结构体沉淀至 BASE_LIB", strings.Join(names, " 与 "), sim*100),
			Members:     members,
		})
	}

	return groups, nil
}

func stripPackageVersionSuffix(pkg string) string {
	trimmed := strings.TrimSpace(pkg)
	if trimmed == "" {
		return ""
	}
	return pkgVersionSuffixRe.ReplaceAllString(trimmed, "")
}

func normalizeFieldStem(name string, aliasNorm map[string]string) string {
	tokens := SplitIdentifierTokens(name)
	if len(tokens) == 0 {
		return strings.ToLower(name)
	}
	if len(aliasNorm) > 0 {
		for i, tok := range tokens {
			if std, ok := aliasNorm[tok]; ok && std != "" {
				tokens[i] = std
			}
		}
	}
	return strings.Join(tokens, "_")
}
