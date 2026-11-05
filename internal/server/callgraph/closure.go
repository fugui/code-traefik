package callgraph

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"code-traefik/models"
	"code-traefik/pkg/agentproto"
	"gorm.io/gorm"
)

// BuildReplayClosurePreview 构建单入口事务的重放闭包预览与前置依赖分析
func BuildReplayClosurePreview(
	db *gorm.DB,
	sessionID uint64,
	ingressRecordID uint64,
) (*ReplayClosurePreviewDTO, error) {
	if db == nil {
		return nil, errors.New("数据库未就绪")
	}

	// 1. 加载目标 INGRESS 记录
	var inRec models.TrafficRecord
	if err := db.Where("id = ? AND session_id = ?", ingressRecordID, sessionID).First(&inRec).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("目标流量记录不存在")
		}
		return nil, fmt.Errorf("查询流量记录失败: %w", err)
	}

	if inRec.Direction != "INGRESS" {
		return nil, errors.New("400 Bad Request: 仅 INGRESS 入口记录支持事务闭包重放")
	}

	// 2. 加载同会话的全部记录 (轻量投影)
	var sessionRecords []models.TrafficRecord
	if err := db.Model(&models.TrafficRecord{}).
		Omit("raw_req_packet", "raw_rsp_packet").
		Where("session_id = ?", sessionID).
		Order("req_captured_at_ns ASC, seq_no ASC, id ASC").
		Find(&sessionRecords).Error; err != nil {
		return nil, fmt.Errorf("查询会话记录失败: %w", err)
	}

	// 加载 session 快照
	var session models.RecordingSession
	_ = db.Where("id = ?", sessionID).First(&session).Error

	hydratedList := HydrateRecords(db, &session, []models.TrafficRecord{inRec})
	ingressSummary := hydratedList[0]

	// 3. 构建调用树以找出当前 INGRESS 节点的全部下游子孙 EGRESS 记录
	trees := BuildTransactionTrees(db, sessionRecords, &session, nil)

	var targetNode *TransactionTreeNodeDTO
	for i := range trees {
		if trees[i].RootRecordID == strconv.FormatUint(inRec.ID, 10) {
			targetNode = &trees[i].Root
			break
		}
		// 递归搜索子树
		if found := findNodeByID(&trees[i].Root, inRec.ID); found != nil {
			targetNode = found
			break
		}
	}

	// 收集该节点下的全部 EGRESS 子孙
	var descendantEgressIDs []uint64
	if targetNode != nil {
		collectDescendantEgress(targetNode, &descendantEgressIDs)
	}

	// 查询子孙记录的完整字段（包括 raw_req_packet / raw_rsp_packet 状态）
	var scopedMocks []ReplayClosureMockItemDTO
	if len(descendantEgressIDs) > 0 {
		var egressRecords []models.TrafficRecord
		_ = db.Where("id IN (?)", descendantEgressIDs).Order("seq_no ASC, id ASC").Find(&egressRecords).Error
		for _, eg := range egressRecords {
			usable := eg.ExchangeType != "REQ_ONLY" && len(eg.RawRspPacket) > 0
			warning := ""
			if !usable {
				warning = "录制期仅捕获出站请求，无应答帧，重放时可能触发超时"
			}
			scopedMocks = append(scopedMocks, ReplayClosureMockItemDTO{
				RecordID:       strconv.FormatUint(eg.ID, 10),
				SeqNo:          eg.SeqNo,
				ServiceName:    eg.ServiceName,
				Operation:      eg.Operation,
				ExchangeType:   eg.ExchangeType,
				StatusCode:     eg.StatusCode.String(),
				LinkConfidence: ConfidenceDOTRExact,
				UsableAsMock:   usable,
				Warning:        warning,
			})
		}
	}

	// 4. 分析前置状态依赖链:
	// 当前 INGRESS 消费了哪些实体? 查找会话内较早的 PRODUCER
	_, consumedEntities := ExtractEntitiesFromMeta(inRec.HeadersJSON)
	if len(consumedEntities) == 0 && len(inRec.PayloadJSON) > 0 {
		var payload struct {
			Req map[string]interface{} `json:"req"`
		}
		if err := json.Unmarshal(inRec.PayloadJSON, &payload); err == nil && payload.Req != nil {
			for k, v := range payload.Req {
				if isEntityKeyCandidate(k) {
					consumedEntities = append(consumedEntities, fmt.Sprintf("%s=%v", k, v))
				}
			}
		}
	}

	var prereqs []ReplayClosurePrerequisiteDTO
	recommendedIDs := make([]string, 0)
	seenPrereqID := make(map[uint64]bool)

	if len(consumedEntities) > 0 {
		for _, ce := range consumedEntities {
			parts := strings.SplitN(ce, "=", 2)
			if len(parts) != 2 {
				continue
			}
			fn, fv := parts[0], parts[1]
			if noiseEntityFields[strings.ToLower(fn)] {
				continue
			}

			// 在较早记录中查找 PRODUCER
			for _, prev := range sessionRecords {
				if prev.SeqNo >= inRec.SeqNo || prev.Direction != "INGRESS" {
					continue
				}
				prod, _ := ExtractEntitiesFromMeta(prev.HeadersJSON)
				isProd := false
				for _, p := range prod {
					if p == ce {
						isProd = true
						break
					}
				}
				if !isProd && len(prev.PayloadJSON) > 0 {
					var pld struct {
						Rsp map[string]interface{} `json:"rsp"`
					}
					if err := json.Unmarshal(prev.PayloadJSON, &pld); err == nil && pld.Rsp != nil {
						if fmt.Sprintf("%v", pld.Rsp[fn]) == fv {
							isProd = true
						}
					}
				}

				if isProd && !seenPrereqID[prev.ID] {
					seenPrereqID[prev.ID] = true
					prereqs = append(prereqs, ReplayClosurePrerequisiteDTO{
						RecordID:        strconv.FormatUint(prev.ID, 10),
						SeqNo:           prev.SeqNo,
						ServiceName:     prev.ServiceName,
						Operation:       prev.Operation,
						SharedEntityKey: ce,
						Role:            "PRODUCER",
					})
					recommendedIDs = append(recommendedIDs, strconv.FormatUint(prev.ID, 10))
				}
			}
		}
	}

	// 推荐重放列表最后加上当前记录
	recommendedIDs = append(recommendedIDs, strconv.FormatUint(inRec.ID, 10))

	if scopedMocks == nil {
		scopedMocks = []ReplayClosureMockItemDTO{}
	}
	if prereqs == nil {
		prereqs = []ReplayClosurePrerequisiteDTO{}
	}

	return &ReplayClosurePreviewDTO{
		IngressRecord:        ingressSummary,
		ScopedEgressMocks:    scopedMocks,
		PrerequisiteChain:    prereqs,
		RecommendedRecordIDs: recommendedIDs,
	}, nil
}

func findNodeByID(node *TransactionTreeNodeDTO, id uint64) *TransactionTreeNodeDTO {
	idStr := strconv.FormatUint(id, 10)
	if node.Record.ID == idStr {
		return node
	}
	for i := range node.Children {
		if found := findNodeByID(&node.Children[i], id); found != nil {
			return found
		}
	}
	return nil
}

func collectDescendantEgress(node *TransactionTreeNodeDTO, out *[]uint64) {
	for i := range node.Children {
		child := &node.Children[i]
		if child.Record.Direction == "EGRESS" {
			cid, _ := strconv.ParseUint(child.Record.ID, 10, 64)
			if cid > 0 {
				*out = append(*out, cid)
			}
		}
		collectDescendantEgress(child, out)
	}
}

// BuildScopedMockRules 组装用于 Agent 下发的 MockRuleDownstream
func BuildScopedMockRules(egressRecords []models.TrafficRecord) []agentproto.MockRuleDownstream {
	var rules []agentproto.MockRuleDownstream
	for _, eg := range egressRecords {
		if len(eg.RawReqPacket) == 0 {
			continue
		}
		var rspPackets [][]byte
		if len(eg.RawRspPacket) > 0 {
			rspPackets = append(rspPackets, eg.RawRspPacket)
		}
		rules = append(rules, agentproto.MockRuleDownstream{
			ReqPacket:  eg.RawReqPacket,
			RspPackets: rspPackets,
		})
	}
	return rules
}
