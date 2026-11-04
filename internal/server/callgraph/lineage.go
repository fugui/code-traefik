package callgraph

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"code-traefik/models"
)

var noiseEntityFields = map[string]bool{
	"user_id":        true,
	"tenant_id":      true,
	"page":           true,
	"page_size":      true,
	"page_no":        true,
	"limit":          true,
	"offset":         true,
	"token":          true,
	"app_id":         true,
	"version":        true,
	"client_ip":      true,
	"trace_id":       true,
	"span_id":        true,
	"father_span_id": true,
}

var lowEntropyValues = map[string]bool{
	"":        true,
	"0":       true,
	"1":       true,
	"-1":      true,
	"true":    true,
	"false":   true,
	"null":    true,
	"nil":     true,
	"ok":      true,
	"success": true,
	"failed":  true,
	"error":   true,
}

// ExtractEntitiesFromMeta 从 HeadersJSON.meta 提取已识别实体
func ExtractEntitiesFromMeta(headersJSON []byte) ([]string, []string) {
	if len(headersJSON) == 0 {
		return nil, nil
	}
	var wrapper struct {
		Meta struct {
			ProducedEntities []string `json:"produced_entities"`
			ConsumedEntities []string `json:"consumed_entities"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(headersJSON, &wrapper); err == nil {
		return wrapper.Meta.ProducedEntities, wrapper.Meta.ConsumedEntities
	}
	return nil, nil
}

type scalarLeaf struct {
	Side      string // REQ | RSP
	FieldPath string // req.xxx or rsp.yyy
	Value     string
}

// ExtractParameterLineage 提取单事务内参数流转血缘边
func ExtractParameterLineage(records []models.TrafficRecord) []ParameterLineageEdgeDTO {
	if len(records) == 0 {
		return nil
	}

	limit := len(records)
	if limit > MaxLineageRecords {
		limit = MaxLineageRecords
	}
	subRecords := records[:limit]

	// 记录每条 record 的标量叶子
	leavesByRecord := make(map[uint64][]scalarLeaf)

	for _, r := range subRecords {
		if len(r.PayloadJSON) == 0 {
			continue
		}
		var payload struct {
			Req interface{} `json:"req"`
			Rsp interface{} `json:"rsp"`
		}
		if err := json.Unmarshal(r.PayloadJSON, &payload); err != nil {
			continue
		}

		var leaves []scalarLeaf
		leafCount := 0
		if payload.Req != nil {
			flattenScalars("req", payload.Req, 1, &leafCount, &leaves)
		}
		if payload.Rsp != nil {
			flattenScalars("rsp", payload.Rsp, 1, &leafCount, &leaves)
		}
		leavesByRecord[r.ID] = leaves
	}

	var edges []ParameterLineageEdgeDTO
	seenEdge := make(map[string]bool)

	// 双重循环寻找参数流转: 前序 RecordA (通常是 RSP) 传给 后序 RecordB (通常是 REQ)
	for i := 0; i < len(subRecords); i++ {
		rA := subRecords[i]
		leavesA := leavesByRecord[rA.ID]

		for j := i; j < len(subRecords); j++ {
			rB := subRecords[j]
			leavesB := leavesByRecord[rB.ID]

			for _, lA := range leavesA {
				// 优先关注从前序的 RSP 产出到后序 REQ 消费，或同请求 REQ 到下级 REQ
				if i == j && lA.Side == "REQ" {
					continue // 同一请求的 REQ 到 REQ 不产生边
				}

				for _, lB := range leavesB {
					if lB.Side != "REQ" && !(i != j && lB.Side == "RSP") {
						continue // 目标侧主要是 REQ 消费入参
					}
					if lA.Value == lB.Value {
						// 相同标量值
						edgeKey := fmt.Sprintf("%d:%s->%d:%s", rA.ID, lA.FieldPath, rB.ID, lB.FieldPath)
						if !seenEdge[edgeKey] {
							seenEdge[edgeKey] = true
							sampleVal := lA.Value
							if len(sampleVal) > 64 {
								sampleVal = sampleVal[:61] + "..."
							}
							edges = append(edges, ParameterLineageEdgeDTO{
								FromRecordID:  strconv.FormatUint(rA.ID, 10),
								FromSide:      lA.Side,
								FromFieldPath: lA.FieldPath,
								ToRecordID:    strconv.FormatUint(rB.ID, 10),
								ToSide:        lB.Side,
								ToFieldPath:   lB.FieldPath,
								SampleValue:   sampleVal,
							})
						}
					}
				}
			}
		}
	}

	return edges
}

func flattenScalars(prefix string, val interface{}, depth int, count *int, out *[]scalarLeaf) {
	if depth > MaxLineageDepth || *count >= MaxScalarLeaves {
		return
	}

	switch v := val.(type) {
	case map[string]interface{}:
		for k, child := range v {
			field := k
			if prefix != "" {
				field = prefix + "." + k
			}
			flattenScalars(field, child, depth+1, count, out)
		}
	case []interface{}:
		for idx, item := range v {
			if idx > 10 {
				break // 数组仅探查前 10 个元素
			}
			field := fmt.Sprintf("%s[%d]", prefix, idx)
			flattenScalars(field, item, depth+1, count, out)
		}
	case string:
		s := strings.TrimSpace(v)
		if isScalarEligible(s) {
			side := "REQ"
			if strings.HasPrefix(prefix, "rsp") {
				side = "RSP"
			}
			*out = append(*out, scalarLeaf{Side: side, FieldPath: prefix, Value: s})
			*count++
		}
	case float64:
		// 数字转换为整型或浮点字符串
		var s string
		if v == float64(int64(v)) {
			s = strconv.FormatInt(int64(v), 10)
		} else {
			s = strconv.FormatFloat(v, 'f', -1, 64)
		}
		if isScalarEligible(s) {
			side := "REQ"
			if strings.HasPrefix(prefix, "rsp") {
				side = "RSP"
			}
			*out = append(*out, scalarLeaf{Side: side, FieldPath: prefix, Value: s})
			*count++
		}
	case bool:
		// 布尔值归为低熵忽略
	}
}

func isScalarEligible(s string) bool {
	if len(s) < 3 {
		return false
	}
	lower := strings.ToLower(s)
	if lowEntropyValues[lower] {
		return false
	}
	return true
}

// BuildEntityChains 构建跨事务业务实体生命周期链
func BuildEntityChains(records []models.TrafficRecord) []EntityLifecycleChainDTO {
	// 实体主键 -> chain
	chainsMap := make(map[string]*EntityLifecycleChainDTO)

	for _, r := range records {
		produced, consumed := ExtractEntitiesFromMeta(r.HeadersJSON)

		// 如果 meta 中未预提取，尝试从 payload_json 提取常见实体键
		if len(produced) == 0 && len(consumed) == 0 && len(r.PayloadJSON) > 0 {
			var payload struct {
				Req map[string]interface{} `json:"req"`
				Rsp map[string]interface{} `json:"rsp"`
			}
			if err := json.Unmarshal(r.PayloadJSON, &payload); err == nil {
				if payload.Rsp != nil {
					for k, v := range payload.Rsp {
						if isEntityKeyCandidate(k) {
							valStr := fmt.Sprintf("%v", v)
							if isScalarEligible(valStr) {
								produced = append(produced, fmt.Sprintf("%s=%s", k, valStr))
							}
						}
					}
				}
				if payload.Req != nil {
					for k, v := range payload.Req {
						if isEntityKeyCandidate(k) {
							valStr := fmt.Sprintf("%v", v)
							if isScalarEligible(valStr) {
								consumed = append(consumed, fmt.Sprintf("%s=%s", k, valStr))
							}
						}
					}
				}
			}
		}

		rootIDStr := strconv.FormatUint(r.ID, 10)

		for _, p := range produced {
			parts := strings.SplitN(p, "=", 2)
			if len(parts) != 2 {
				continue
			}
			fn, ev := parts[0], parts[1]
			if noiseEntityFields[strings.ToLower(fn)] {
				continue
			}
			ek := fmt.Sprintf("%s:%s", fn, ev)
			chain, exists := chainsMap[ek]
			if !exists {
				chain = &EntityLifecycleChainDTO{
					EntityKey:            ek,
					FieldName:            fn,
					EntityValue:          ev,
					ProducerRootRecordID: rootIDStr,
					Steps:                make([]EntityLifecycleStepDTO, 0),
				}
				chainsMap[ek] = chain
			}
			chain.Steps = append(chain.Steps, EntityLifecycleStepDTO{
				RootRecordID: rootIDStr,
				SeqNo:        r.SeqNo,
				Operation:    r.Operation,
				Role:         "PRODUCER",
			})
		}

		for _, c := range consumed {
			parts := strings.SplitN(c, "=", 2)
			if len(parts) != 2 {
				continue
			}
			fn, ev := parts[0], parts[1]
			if noiseEntityFields[strings.ToLower(fn)] {
				continue
			}
			ek := fmt.Sprintf("%s:%s", fn, ev)
			chain, exists := chainsMap[ek]
			if !exists {
				chain = &EntityLifecycleChainDTO{
					EntityKey:   ek,
					FieldName:   fn,
					EntityValue: ev,
					Steps:       make([]EntityLifecycleStepDTO, 0),
				}
				chainsMap[ek] = chain
			}
			chain.Steps = append(chain.Steps, EntityLifecycleStepDTO{
				RootRecordID: rootIDStr,
				SeqNo:        r.SeqNo,
				Operation:    r.Operation,
				Role:         "CONSUMER",
			})
		}
	}

	result := make([]EntityLifecycleChainDTO, 0, len(chainsMap))
	for _, chain := range chainsMap {
		// 仅保留跨步实体（至少有 2 个步骤，或包含 PRODUCER）
		if len(chain.Steps) >= 2 || chain.ProducerRootRecordID != "" {
			sort.Slice(chain.Steps, func(i, j int) bool {
				return chain.Steps[i].SeqNo < chain.Steps[j].SeqNo
			})
			result = append(result, *chain)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].EntityKey < result[j].EntityKey
	})

	return result
}

func isEntityKeyCandidate(key string) bool {
	lk := strings.ToLower(key)
	if noiseEntityFields[lk] {
		return false
	}
	return strings.HasSuffix(lk, "_id") || strings.HasSuffix(lk, "_no") || strings.HasSuffix(lk, "id") || strings.HasSuffix(lk, "no")
}
