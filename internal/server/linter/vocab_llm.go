package linter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"code-traefik/internal/server/security"
	"code-traefik/models"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	maxLLMTimeout     = 3000 * time.Millisecond
	llmCacheTTL       = 15 * time.Minute
	maxLLMCacheItems  = 4096
	maxUnknownQueries = 5
)

type llmCacheEntry struct {
	expiresAt time.Time
	term      string
	descEn    string
	descZh    string
	aliases   []string
}

var (
	llmCacheMu sync.RWMutex
	llmCache   = make(map[string]llmCacheEntry)
)

// ClearLLMCacheForTest 清空进程内 LLM 词根缓存（仅用于测试）
func ClearLLMCacheForTest() {
	llmCacheMu.Lock()
	defer llmCacheMu.Unlock()
	llmCache = make(map[string]llmCacheEntry)
}

// SplitIdentifierTokens 按下划线、短横线与 camelCase/PascalCase 边界切分标识符为小写词根。
func SplitIdentifierTokens(ident string) []string {
	var tokens []string
	parts := strings.FieldsFunc(ident, func(r rune) bool {
		return r == '_' || r == '-' || r == '.' || unicode.IsSpace(r)
	})
	for _, p := range parts {
		runes := []rune(p)
		if len(runes) == 0 {
			continue
		}
		start := 0
		for i := 1; i < len(runes); i++ {
			prev := runes[i-1]
			curr := runes[i]
			var next rune
			if i+1 < len(runes) {
				next = runes[i+1]
			}
			// lowercase/digit -> Upper (e.g. tradeTs -> trade | Ts)
			if (unicode.IsLower(prev) || unicode.IsDigit(prev)) && unicode.IsUpper(curr) {
				tok := strings.ToLower(string(runes[start:i]))
				if tok != "" {
					tokens = append(tokens, tok)
				}
				start = i
				continue
			}
			// Upper+Upper -> Lower (e.g. HTTPServer -> HTTP | Server)
			if unicode.IsUpper(prev) && unicode.IsUpper(curr) && next != 0 && unicode.IsLower(next) {
				tok := strings.ToLower(string(runes[start:i]))
				if tok != "" {
					tokens = append(tokens, tok)
				}
				start = i
			}
		}
		last := strings.ToLower(string(runes[start:]))
		if last != "" {
			tokens = append(tokens, last)
		}
	}
	return tokens
}

// 常见通用英文基础词与协议关键字集合，避免将正常英文单词误判定为罕见缩写触发 LLM 外部调用
var commonEnglishWords = map[string]struct{}{
	"account": {}, "active": {}, "address": {}, "admin": {}, "all": {}, "amount": {},
	"api": {}, "app": {}, "archive": {}, "audit": {}, "auth": {}, "available": {},
	"balance": {}, "base": {}, "batch": {}, "begin": {}, "billing": {}, "biz": {},
	"body": {}, "bool": {}, "branch": {}, "bundle": {}, "business": {}, "buy": {},
	"by": {}, "cache": {}, "call": {}, "callback": {}, "cancel": {}, "card": {},
	"category": {}, "center": {}, "chain": {}, "change": {}, "channel": {}, "check": {},
	"child": {}, "city": {}, "claim": {}, "class": {}, "clear": {}, "clearing": {},
	"client": {}, "close": {}, "code": {}, "column": {}, "comment": {}, "commit": {},
	"common": {}, "company": {}, "complete": {}, "completed": {}, "config": {}, "confirm": {},
	"content": {}, "context": {}, "contract": {}, "copy": {}, "core": {}, "cost": {},
	"count": {}, "country": {}, "create": {}, "created": {}, "currency": {}, "current": {},
	"cursor": {}, "cust": {}, "custom": {}, "customer": {}, "daily": {}, "data": {},
	"date": {}, "day": {}, "days": {}, "deal": {}, "debug": {}, "default": {},
	"delete": {}, "deleted": {}, "desc": {}, "description": {}, "detail": {}, "details": {},
	"device": {}, "diff": {}, "digest": {}, "dir": {}, "direction": {}, "display": {},
	"domain": {}, "done": {}, "down": {}, "duration": {}, "email": {}, "empty": {},
	"enable": {}, "enabled": {}, "end": {}, "endpoint": {}, "engine": {}, "entity": {},
	"entry": {}, "env": {}, "error": {}, "errors": {}, "event": {}, "events": {},
	"exchange": {}, "exclude": {}, "execute": {}, "expire": {}, "expired": {}, "export": {},
	"extra": {}, "fail": {}, "failed": {}, "file": {}, "filter": {}, "first": {},
	"flag": {}, "flow": {}, "for": {}, "format": {}, "from": {}, "full": {},
	"fund": {}, "gateway": {}, "general": {}, "get": {}, "git": {}, "global": {},
	"golden": {}, "group": {}, "hash": {}, "head": {}, "header": {}, "headers": {},
	"health": {}, "history": {}, "host": {}, "hour": {}, "http": {}, "https": {},
	"ignore": {}, "image": {}, "import": {}, "in": {}, "include": {}, "index": {},
	"info": {}, "init": {}, "input": {}, "instance": {}, "ip": {}, "is": {},
	"issue": {}, "item": {}, "items": {}, "job": {}, "json": {}, "key": {},
	"keys": {}, "kind": {}, "label": {}, "labels": {}, "last": {}, "latest": {},
	"layout": {}, "lease": {}, "level": {}, "limit": {}, "line": {}, "link": {},
	"list": {}, "load": {}, "local": {}, "lock": {}, "log": {}, "logs": {},
	"long": {}, "main": {}, "manual": {}, "map": {}, "mark": {}, "market": {},
	"mask": {}, "match": {}, "max": {}, "member": {}, "members": {}, "menu": {},
	"merge": {}, "message": {}, "messages": {}, "meta": {}, "metadata": {}, "method": {},
	"metric": {}, "min": {}, "minute": {}, "mock": {}, "mode": {}, "model": {},
	"module": {}, "money": {}, "month": {}, "ms": {}, "name": {}, "namespace": {},
	"need": {}, "new": {}, "next": {}, "node": {}, "no": {}, "none": {},
	"normal": {}, "not": {}, "note": {}, "notify": {}, "null": {}, "num": {},
	"number": {}, "obj": {}, "object": {}, "offset": {}, "ok": {}, "old": {},
	"on": {}, "oneway": {}, "open": {}, "op": {}, "operation": {}, "operator": {},
	"option": {}, "options": {}, "or": {}, "order": {}, "orders": {}, "org": {},
	"origin": {}, "out": {}, "outbound": {}, "output": {}, "owner": {}, "package": {},
	"page": {}, "params": {}, "parent": {}, "part": {}, "pass": {}, "passed": {},
	"password": {}, "path": {}, "paths": {}, "pay": {}, "payload": {}, "pending": {},
	"period": {}, "phone": {}, "port": {}, "position": {}, "post": {}, "prefix": {},
	"prev": {}, "price": {}, "primary": {}, "private": {}, "process": {}, "product": {},
	"profile": {}, "project": {}, "proto": {}, "provider": {}, "public": {}, "publish": {},
	"quantity": {}, "query": {}, "queue": {}, "range": {}, "rate": {}, "ratio": {},
	"raw": {}, "read": {}, "ready": {}, "reason": {}, "rebate": {}, "record": {},
	"records": {}, "ref": {}, "refresh": {}, "region": {}, "register": {}, "reject": {},
	"related": {}, "remark": {}, "remote": {}, "remove": {}, "replay": {}, "reply": {},
	"repo": {}, "report": {}, "reports": {}, "req": {}, "request": {}, "required": {},
	"reset": {}, "res": {}, "resource": {}, "resp": {}, "response": {}, "result": {},
	"retention": {}, "retry": {}, "return": {}, "risk": {}, "role": {}, "roles": {},
	"root": {}, "route": {}, "row": {}, "rpc": {}, "rsp": {}, "rule": {},
	"rules": {}, "run": {}, "save": {}, "scan": {}, "schema": {}, "schemas": {},
	"scope": {}, "score": {}, "search": {}, "sec": {}, "secret": {}, "section": {},
	"security": {}, "seed": {}, "segment": {}, "select": {}, "selected": {}, "sell": {},
	"send": {}, "sender": {}, "seq": {}, "sequence": {}, "server": {}, "service": {},
	"services": {}, "session": {}, "set": {}, "setting": {}, "settings": {}, "sha256": {},
	"shared": {}, "short": {}, "side": {}, "sign": {}, "size": {}, "skip": {},
	"snapshot": {}, "sort": {}, "source": {}, "sources": {}, "spec": {}, "src": {},
	"start": {}, "state": {}, "static": {}, "status": {}, "step": {}, "stop": {},
	"store": {}, "stream": {}, "str": {}, "string": {}, "style": {}, "sub": {},
	"subject": {}, "submit": {}, "subsystem": {}, "success": {}, "suffix": {}, "summary": {},
	"symbol": {}, "symbols": {}, "sync": {}, "system": {}, "table": {}, "tag": {},
	"tags": {}, "target": {}, "task": {}, "tax": {}, "team": {}, "template": {},
	"tenant": {}, "term": {}, "terms": {}, "test": {}, "text": {}, "time": {},
	"timeout": {}, "timestamp": {}, "title": {}, "to": {}, "token": {}, "total": {},
	"trace": {}, "track": {}, "trade": {}, "traefik": {}, "tree": {}, "trigger": {},
	"type": {}, "types": {}, "uid": {}, "unit": {}, "unknown": {}, "update": {},
	"updated": {}, "upload": {}, "url": {}, "use": {}, "user": {}, "users": {},
	"uuid": {}, "val": {}, "valid": {}, "value": {}, "values": {}, "ver": {},
	"verify": {}, "version": {}, "view": {}, "violation": {}, "vocab": {}, "warn": {},
	"warning": {}, "webhook": {}, "weight": {}, "with": {}, "work": {}, "year": {},
	"zero": {}, "zone": {},
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func parseJSONStringList(raw datatypes.JSON) []string {
	if len(raw) == 0 {
		return nil
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil
	}
	return arr
}

// ComputeVocabHash 对词汇库核心定义计算确定性摘要，用于进程内 LLM 缓存协同失效。
func ComputeVocabHash(vocab []models.VocabularyTerm) string {
	lines := make([]string, 0, len(vocab))
	for _, t := range vocab {
		aliases := parseJSONStringList(t.Aliases)
		sort.Strings(aliases)
		lines = append(lines, fmt.Sprintf("%s|%v|%s", strings.ToLower(t.Term), t.IsApproved, strings.Join(aliases, ",")))
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:8])
}

type tokenOccurrence struct {
	protoFile string
	msgFull   string
	fieldName string
	line      int
	col       int
}

func evaluateVocabAndLLM(ctx context.Context, files []parsedFile, vocab []models.VocabularyTerm, rules []models.CheckRule, opts EvaluateOptions) ([]models.CheckViolation, string) {
	var vocabRule *models.CheckRule
	for i := range rules {
		if rules[i].CheckMode == models.RuleModeVocab {
			if !rules[i].IsEnabled {
				return nil, models.LLMStatusSkipped
			}
			vocabRule = &rules[i]
			break
		}
	}
	if vocabRule == nil {
		defaultRule := models.CheckRule{
			RuleCode:           "VOCAB_STANDARD_TERM",
			Severity:           models.RuleSeverityWarning,
			CheckMode:          models.RuleModeVocab,
			SuggestionTemplate: "词根 {symbol} 非标准缩写，建议替换为标准词根 {expected}",
			ExemptionRule:      datatypes.JSON([]byte(`{}`)),
			IsEnabled:          true,
		}
		vocabRule = &defaultRule
	}

	exempt := parseExemptionRule(vocabRule.ExemptionRule)
	aliasToTerm := make(map[string]models.VocabularyTerm)
	knownTokens := make(map[string]struct{})
	for _, vt := range vocab {
		std := strings.ToLower(strings.TrimSpace(vt.Term))
		if std != "" {
			knownTokens[std] = struct{}{}
		}
		for _, sim := range parseJSONStringList(vt.SimilarTerms) {
			s := strings.ToLower(strings.TrimSpace(sim))
			if s != "" {
				knownTokens[s] = struct{}{}
			}
		}
		for _, al := range parseJSONStringList(vt.Aliases) {
			a := strings.ToLower(strings.TrimSpace(al))
			if a != "" && a != std {
				aliasToTerm[a] = vt
				knownTokens[a] = struct{}{}
			}
		}
	}

	violations := make([]models.CheckViolation, 0)
	unknownMap := make(map[string][]tokenOccurrence)

	for _, f := range files {
		if isExempted(exempt, f.ProtoFile, f.Package, f.Category) {
			continue
		}
		for _, msg := range f.Messages {
			for _, fld := range msg.Fields {
				tokens := SplitIdentifierTokens(fld.Name)
				for _, tok := range tokens {
					if target, isAlias := aliasToTerm[tok]; isAlias {
						v := makeRuleViolation(*vocabRule, f.ProtoFile, msg.FullName+"."+fld.Name, fld.Line, fld.Col,
							fmt.Sprintf("字段 %s 中的词根 %q 为非标准别名，标准词根应为 %q（%s）", fld.Name, tok, target.Term, target.DescriptionZh),
							renderSuggestion(vocabRule.SuggestionTemplate, tok, target.Term, target.Term))
						v.SuggestionSource = models.SuggestionSourceVocabMatch
						violations = append(violations, v)
						continue
					}
					if _, ok := knownTokens[tok]; ok {
						continue
					}
					if _, ok := commonEnglishWords[tok]; ok {
						continue
					}
					if len(tok) < 2 || isAllDigits(tok) {
						continue
					}
					unknownMap[tok] = append(unknownMap[tok], tokenOccurrence{
						protoFile: f.ProtoFile,
						msgFull:   msg.FullName,
						fieldName: fld.Name,
						line:      fld.Line,
						col:       fld.Col,
					})
				}
			}
		}
	}

	if !opts.IncludeLLM || opts.LLMOverride == nil || !opts.LLMOverride.Enabled || strings.TrimSpace(opts.LLMOverride.BaseURL) == "" || len(unknownMap) == 0 {
		return violations, models.LLMStatusSkipped
	}

	unknownTokens := make([]string, 0, len(unknownMap))
	for tok := range unknownMap {
		unknownTokens = append(unknownTokens, tok)
	}
	sort.Strings(unknownTokens)
	if len(unknownTokens) > maxUnknownQueries {
		unknownTokens = unknownTokens[:maxUnknownQueries]
	}

	vocabHash := ComputeVocabHash(vocab)
	allFromCache := true
	succeededAny := false
	llmCfg := *opts.LLMOverride

	var safeDialer *security.SafeDialer
	for _, tok := range unknownTokens {
		cacheKey := tok + "|" + vocabHash
		if cached, ok := lookupLLMCache(cacheKey); ok {
			succeededAny = true
			violations = append(violations, buildLLMSuggestedViolations(*vocabRule, tok, cached, unknownMap[tok])...)
			continue
		}
		allFromCache = false

		if safeDialer == nil {
			var err error
			safeDialer, err = security.NewSafeDialer(llmCfg.OutboundTrustedCIDRs)
			if err != nil {
				return violations, models.LLMStatusErrorFallback
			}
		}

		entry, llmStatus, callErr := queryLLMWithCircuitBreaker(ctx, safeDialer, llmCfg, tok)
		if callErr != nil {
			return violations, llmStatus
		}
		succeededAny = true
		storeLLMCache(cacheKey, entry)
		persistLLMSuggestedTerm(opts.VocabWriter, entry)
		violations = append(violations, buildLLMSuggestedViolations(*vocabRule, tok, entry, unknownMap[tok])...)
	}

	if allFromCache && succeededAny {
		return violations, models.LLMStatusHitCache
	}
	if succeededAny {
		return violations, models.LLMStatusSucceeded
	}
	return violations, models.LLMStatusSkipped
}

func buildLLMSuggestedViolations(vocabRule models.CheckRule, tok string, entry llmCacheEntry, occurrences []tokenOccurrence) []models.CheckViolation {
	if entry.term == "" || entry.term == tok {
		return nil
	}
	out := make([]models.CheckViolation, 0, len(occurrences))
	for _, occ := range occurrences {
		v := models.CheckViolation{
			RuleID:           ruleIDPtr(vocabRule),
			RuleCode:         vocabRule.RuleCode,
			Severity:         models.RuleSeverityInfo,
			ProtoFile:        occ.protoFile,
			SymbolPath:       occ.msgFull + "." + occ.fieldName,
			FileLine:         occ.line,
			FileColumn:       occ.col,
			Message:          fmt.Sprintf("LLM 识别未知词根 %q 建议标准化为 %q（%s）", tok, entry.term, entry.descZh),
			Suggestion:       fmt.Sprintf("建议替换为标准词根 %s (%s)", entry.term, entry.descZh),
			SuggestionSource: models.SuggestionSourceLLMSuggested,
		}
		out = append(out, v)
	}
	return out
}

func ruleIDPtr(r models.CheckRule) *uint64 {
	if r.ID == 0 {
		return nil
	}
	id := r.ID
	return &id
}

func lookupLLMCache(key string) (llmCacheEntry, bool) {
	now := time.Now()
	llmCacheMu.RLock()
	entry, ok := llmCache[key]
	llmCacheMu.RUnlock()
	if !ok || now.After(entry.expiresAt) {
		return llmCacheEntry{}, false
	}
	return entry, true
}

func storeLLMCache(key string, entry llmCacheEntry) {
	llmCacheMu.Lock()
	defer llmCacheMu.Unlock()
	if len(llmCache) >= maxLLMCacheItems {
		llmCache = make(map[string]llmCacheEntry)
	}
	entry.expiresAt = time.Now().Add(llmCacheTTL)
	llmCache[key] = entry
}

func persistLLMSuggestedTerm(db *gorm.DB, entry llmCacheEntry) {
	if db == nil || strings.TrimSpace(entry.term) == "" {
		return
	}
	term := strings.ToLower(strings.TrimSpace(entry.term))
	if len(term) > 64 {
		return
	}
	aliasesJSON, err := json.Marshal(entry.aliases)
	if err != nil {
		aliasesJSON = []byte("[]")
	}
	descZh := strings.TrimSpace(entry.descZh)
	if descZh == "" {
		descZh = "LLM 自动识别建议词根"
	}
	row := models.VocabularyTerm{
		Term:          term,
		DescriptionEn: strings.TrimSpace(entry.descEn),
		DescriptionZh: descZh,
		Aliases:       datatypes.JSON(aliasesJSON),
		SimilarTerms:  datatypes.JSON([]byte("[]")),
		Domain:        "GENERAL",
		Source:        models.VocabSourceLLMSuggested,
		IsApproved:    false,
	}
	if insertErr := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "term"}},
		DoNothing: true,
	}).Create(&row).Error; insertErr != nil {
		log.Printf("[linter] 写入 LLM 建议词根 %s 失败: %v", term, insertErr)
	}
}

func clampLLMTimeout(ms int) time.Duration {
	if ms <= 0 {
		return maxLLMTimeout
	}
	d := time.Duration(ms) * time.Millisecond
	if d > maxLLMTimeout {
		return maxLLMTimeout
	}
	return d
}

func buildChatCompletionsURL(baseURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(trimmed, "/chat/completions") {
		return trimmed
	}
	return trimmed + "/chat/completions"
}

func isTimeoutError(ctx context.Context, err error) bool {
	if ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func queryLLMWithCircuitBreaker(parentCtx context.Context, dialer *security.SafeDialer, cfg LLMSettings, unknownToken string) (llmCacheEntry, string, error) {
	timeout := clampLLMTimeout(cfg.TimeoutMs)
	endpoint := buildChatCompletionsURL(cfg.BaseURL)
	if err := dialer.ValidateURL(parentCtx, endpoint); err != nil {
		return llmCacheEntry{}, models.LLMStatusErrorFallback, err
	}

	callCtx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()

	modelName := strings.TrimSpace(cfg.Model)
	if modelName == "" {
		modelName = "gpt-4o-mini"
	}
	prompt := fmt.Sprintf("Analyze Protobuf identifier root token %q. Return strict JSON object: {\"term\":\"canonical_short_root\",\"description_en\":\"...\",\"description_zh\":\"...\",\"aliases\":[\"...\"]}.", unknownToken)
	reqBody, marshalErr := json.Marshal(map[string]any{
		"model":       modelName,
		"temperature": 0,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a Protobuf terminology naming assistant. Output strict JSON only."},
			{"role": "user", "content": prompt},
		},
	})
	if marshalErr != nil {
		return llmCacheEntry{}, models.LLMStatusErrorFallback, marshalErr
	}

	httpReq, err := http.NewRequestWithContext(callCtx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return llmCacheEntry{}, models.LLMStatusErrorFallback, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(cfg.APIKey) != "" {
		httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.APIKey))
	}

	client := dialer.HTTPClient(timeout)
	resp, err := client.Do(httpReq)
	if err != nil {
		if isTimeoutError(callCtx, err) {
			return llmCacheEntry{}, models.LLMStatusTimeoutFallback, err
		}
		return llmCacheEntry{}, models.LLMStatusErrorFallback, err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("[linter] 关闭 LLM HTTP 响应体失败: %v", closeErr)
		}
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return llmCacheEntry{}, models.LLMStatusErrorFallback, fmt.Errorf("LLM HTTP status %d", resp.StatusCode)
	}

	rawBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		if isTimeoutError(callCtx, readErr) {
			return llmCacheEntry{}, models.LLMStatusTimeoutFallback, readErr
		}
		return llmCacheEntry{}, models.LLMStatusErrorFallback, readErr
	}

	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rawBytes, &chatResp); err != nil || len(chatResp.Choices) == 0 {
		return llmCacheEntry{}, models.LLMStatusErrorFallback, errors.New("invalid LLM chat completion payload")
	}

	content := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var parsed struct {
		Term          string   `json:"term"`
		DescriptionEn string   `json:"description_en"`
		DescriptionZh string   `json:"description_zh"`
		Aliases       []string `json:"aliases"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return llmCacheEntry{}, models.LLMStatusErrorFallback, err
	}
	cleanTerm := strings.ToLower(strings.TrimSpace(parsed.Term))
	if cleanTerm == "" {
		cleanTerm = unknownToken
	}
	return llmCacheEntry{
		term:    cleanTerm,
		descEn:  parsed.DescriptionEn,
		descZh:  parsed.DescriptionZh,
		aliases: parsed.Aliases,
	}, models.LLMStatusSucceeded, nil
}
