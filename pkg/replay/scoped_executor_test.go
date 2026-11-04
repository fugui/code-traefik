package replay

import (
	"bytes"
	"testing"

	"code-traefik/pkg/agentproto"
	"code-traefik/pkg/crosrpc"
)

// T-M13-03: 出站 Mock 请求+应答成对规则与精确匹配防错配
func TestScopedMockMatcher_ExactDownstreamRulePairs_AndRecordScopeIsolation(t *testing.T) {
	matcher := NewScopedMockMatcher()

	service := "dophi.inventory.v1.InventoryService"
	op := "LockStock"

	// 1. 同一入口请求 RecordID=1001 内，并发调用两次同一接口 InventoryService.LockStock
	// 入参分别针对 SKU-101 与 SKU-102
	reqPayload101 := []byte(`{"sku_id":"SKU-101","qty":10}`)
	rspPayload101 := []byte(`{"lock_id":"LOCK-101","success":true}`)
	ruleReq101 := buildTestReqPacket(2001, service, op, 1, [16]byte{}, 0, reqPayload101)
	ruleRsp101 := buildTestRspPacket(2001, crosrpc.MsgTypeRsp, 188, service, op, 1, rspPayload101)

	reqPayload102 := []byte(`{"sku_id":"SKU-102","qty":20}`)
	rspPayload102 := []byte(`{"lock_id":"LOCK-102","success":true}`)
	ruleReq102 := buildTestReqPacket(2002, service, op, 1, [16]byte{}, 0, reqPayload102)
	ruleRsp102 := buildTestRspPacket(2002, crosrpc.MsgTypeRsp, 188, service, op, 1, rspPayload102)

	// 下发携带成对 {ReqPacket, RspPackets} 的 MockRuleDownstream
	rule101 := agentproto.MockRuleDownstream{
		ReqPacket:  ruleReq101,
		RspPackets: [][]byte{ruleRsp101},
	}
	rule102 := agentproto.MockRuleDownstream{
		ReqPacket:  ruleReq102,
		RspPackets: [][]byte{ruleRsp102},
	}

	// 注册到 RecordID=1001 专属局部作用域
	matcher.RegisterScopedDownstreamRule(1001, rule101)
	matcher.RegisterScopedDownstreamRule(1001, rule102)

	// 激活 RecordID=1001
	matcher.SetActiveRecordID(1001)

	// 运行时被测程序并发发起调用，但到达顺序发生颠倒（SKU-102 先到达，SKU-101 后到达）
	liveReq102 := buildTestReqPacket(8802, service, op, 1, [16]byte{}, 0, reqPayload102)
	matchRes102, err := matcher.MatchAndHeal(liveReq102)
	if err != nil {
		t.Fatalf("MatchAndHeal liveReq102 failed: %v", err)
	}
	if matchRes102.Result != MatchHitScoped {
		t.Fatalf("liveReq102 expected SCOPED_HIT, got %v", matchRes102.Result)
	}
	if len(matchRes102.ResponseFrames) != 1 || !bytes.Contains(matchRes102.ResponseFrames[0], []byte("LOCK-102")) {
		t.Errorf("liveReq102 should accurately match rspPayload102, got response: %s", string(matchRes102.ResponseFrames[0]))
	}

	liveReq101 := buildTestReqPacket(8801, service, op, 1, [16]byte{}, 0, reqPayload101)
	matchRes101, err := matcher.MatchAndHeal(liveReq101)
	if err != nil {
		t.Fatalf("MatchAndHeal liveReq101 failed: %v", err)
	}
	if matchRes101.Result != MatchHitScoped {
		t.Fatalf("liveReq101 expected SCOPED_HIT, got %v", matchRes101.Result)
	}
	if len(matchRes101.ResponseFrames) != 1 || !bytes.Contains(matchRes101.ResponseFrames[0], []byte("LOCK-101")) {
		t.Errorf("liveReq101 should accurately match rspPayload101, got response: %s", string(matchRes101.ResponseFrames[0]))
	}

	// 2. 跨 RecordID 作用域隔离与全局兜底匹配验证
	// 在 RecordID=1002 注册 LockStock (SKU-999) 局部 Mock
	reqPayload999 := []byte(`{"sku_id":"SKU-999","qty":5}`)
	rspPayload999 := []byte(`{"lock_id":"LOCK-999","success":true}`)
	ruleReq999 := buildTestReqPacket(3001, service, op, 1, [16]byte{}, 0, reqPayload999)
	ruleRsp999 := buildTestRspPacket(3001, crosrpc.MsgTypeRsp, 188, service, op, 1, rspPayload999)
	matcher.RegisterScopedDownstreamRule(1002, agentproto.MockRuleDownstream{
		ReqPacket:  ruleReq999,
		RspPackets: [][]byte{ruleRsp999},
	})

	// 注册全局兜底 Mock (公共配置接口 dophi.config.v1.ConfigService.GetConfig)
	globalService := "dophi.config.v1.ConfigService"
	globalOp := "GetConfig"
	reqPayloadGlobal := []byte(`{"config_key":"FEATURE_FLAG_M13"}`)
	rspPayloadGlobal := []byte(`{"config_val":"ENABLED"}`)
	ruleReqGlobal := buildTestReqPacket(4001, globalService, globalOp, 1, [16]byte{}, 0, reqPayloadGlobal)
	ruleRspGlobal := buildTestRspPacket(4001, crosrpc.MsgTypeRsp, 188, globalService, globalOp, 1, rspPayloadGlobal)
	matcher.RegisterGlobalFallbackRule(agentproto.MockRuleDownstream{
		ReqPacket:  ruleReqGlobal,
		RspPackets: [][]byte{ruleRspGlobal},
	})

	// 当前活跃请求仍为 RecordID=1001 (未录入 SKU-999)
	matcher.SetActiveRecordID(1001)

	// 尝试在 RecordID=1001 下发送 SKU-999：严禁越权命中 RecordID=1002 的专属 Mock
	liveReq999On1001 := buildTestReqPacket(8899, service, op, 1, [16]byte{}, 0, reqPayload999)
	resIsolated, err := matcher.MatchAndHeal(liveReq999On1001)
	if err != nil {
		t.Fatalf("MatchAndHeal failed: %v", err)
	}
	if resIsolated.Result == MatchHitScoped {
		t.Errorf("cross-recordID scope breach: RecordID=1001 hit RecordID=1002 scoped mock!")
	}
	if resIsolated.Result != MatchMiss {
		t.Errorf("expected MOCK_MISS for unassigned request on RecordID=1001, got %v", resIsolated.Result)
	}

	// 切换到 RecordID=1002 后，再次发送 SKU-999：应精确命中 RecordID=1002 的专属 Mock
	matcher.SetActiveRecordID(1002)
	res1002, err := matcher.MatchAndHeal(liveReq999On1001)
	if err != nil {
		t.Fatalf("MatchAndHeal failed: %v", err)
	}
	if res1002.Result != MatchHitScoped {
		t.Errorf("expected SCOPED_HIT for RecordID=1002, got %v", res1002.Result)
	}
	if len(res1002.ResponseFrames) != 1 || !bytes.Contains(res1002.ResponseFrames[0], []byte("LOCK-999")) {
		t.Errorf("RecordID=1002 should return LOCK-999 response")
	}

	// 切换到 RecordID=1003，发送全局公共配置请求：应命中全局兜底
	matcher.SetActiveRecordID(1003)
	liveReqGlobalOn1003 := buildTestReqPacket(8877, globalService, globalOp, 1, [16]byte{}, 0, reqPayloadGlobal)
	resGlobal, err := matcher.MatchAndHeal(liveReqGlobalOn1003)
	if err != nil {
		t.Fatalf("MatchAndHeal failed: %v", err)
	}
	if resGlobal.Result != MatchHitGlobal {
		t.Errorf("expected GLOBAL_FALLBACK_HIT, got %v", resGlobal.Result)
	}
	if len(resGlobal.ResponseFrames) != 1 || !bytes.Contains(resGlobal.ResponseFrames[0], []byte("ENABLED")) {
		t.Errorf("global fallback should return ENABLED response")
	}
}
