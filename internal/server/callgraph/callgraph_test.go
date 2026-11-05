package callgraph

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-traefik/config"
	"code-traefik/models"
	"gorm.io/datatypes"
)

// T-M13-05: DOTR 建树、并发/补偿分类与破环防爆
func TestCallGraph_DOTR_BuildTree_Parallel_Compensation_AndCycleProtection(t *testing.T) {
	traceID := "trace-ord-001"
	tBase := int64(1700000000000000000)

	// 1. 构造三级调用集:
	// Root INGRESS: CreateOrder (SpanID: span-1, FatherSpanID: "")
	//   Child 1 EGRESS: LockStock (SpanID: span-2, FatherSpanID: span-1, t=tBase+10ms, dur=20ms) -> 失败 (status_code=500)
	//   Child 2 EGRESS: QueryCoupon (SpanID: span-3, FatherSpanID: span-1, t=tBase+15ms, dur=10ms) -> 与 Child 1 时间重叠 (PARALLEL)
	//   Child 3 EGRESS: ReleaseStock (SpanID: span-4, FatherSpanID: span-1, t=tBase+35ms, dur=10ms) -> 前序失败后的补偿 (COMPENSATION)
	latRoot := int64(60000)
	lat1 := int64(20000)
	lat2 := int64(10000)
	lat3 := int64(10000)

	rspT0 := tBase + latRoot*1000
	rspT1 := tBase + 10_000_000 + lat1*1000
	rspT2 := tBase + 15_000_000 + lat2*1000
	rspT3 := tBase + 35_000_000 + lat3*1000

	records := []models.TrafficRecord{
		{
			ID:              101,
			SessionID:       1,
			SeqNo:           1,
			ProcessID:       10,
			Direction:       "INGRESS",
			ServiceName:     "OrderService",
			Operation:       "CreateOrder",
			TraceID:         traceID,
			SpanID:          "span-1",
			FatherSpanID:    "",
			ReqCapturedAtNs: tBase,
			RspCapturedAtNs: &rspT0,
			LatencyUs:       &latRoot,
			StatusCode:      models.U64Numeric(0),
			CreatedAt:       time.Now(),
		},
		{
			ID:              102,
			SessionID:       1,
			SeqNo:           2,
			ProcessID:       10,
			Direction:       "EGRESS",
			ServiceName:     "StockService",
			Operation:       "LockStock",
			TraceID:         traceID,
			SpanID:          "span-2",
			FatherSpanID:    "span-1",
			ReqCapturedAtNs: tBase + 10_000_000,
			RspCapturedAtNs: &rspT1,
			LatencyUs:       &lat1,
			StatusCode:      models.U64Numeric(500), // 协议错误
			CreatedAt:       time.Now(),
		},
		{
			ID:              103,
			SessionID:       1,
			SeqNo:           3,
			ProcessID:       10,
			Direction:       "EGRESS",
			ServiceName:     "CouponService",
			Operation:       "QueryCoupon",
			TraceID:         traceID,
			SpanID:          "span-3",
			FatherSpanID:    "span-1",
			ReqCapturedAtNs: tBase + 15_000_000, // 与 102 重叠 (10ms+20ms=30ms, 15ms < 30ms-50us)
			RspCapturedAtNs: &rspT2,
			LatencyUs:       &lat2,
			StatusCode:      models.U64Numeric(0),
			CreatedAt:       time.Now(),
		},
		{
			ID:              104,
			SessionID:       1,
			SeqNo:           4,
			ProcessID:       10,
			Direction:       "EGRESS",
			ServiceName:     "StockService",
			Operation:       "ReleaseStock", // 命中补偿方法名正则且发生在前序失败之后
			TraceID:         traceID,
			SpanID:          "span-4",
			FatherSpanID:    "span-1",
			ReqCapturedAtNs: tBase + 35_000_000,
			RspCapturedAtNs: &rspT3,
			LatencyUs:       &lat3,
			StatusCode:      models.U64Numeric(0),
			CreatedAt:       time.Now(),
		},
	}

	trees := BuildTransactionTrees(nil, records, nil, nil)
	if len(trees) != 1 {
		t.Fatalf("expected 1 tree group, got %d", len(trees))
	}

	group := trees[0]
	if group.RootRecordID != "101" {
		t.Errorf("expected root record ID 101, got %s", group.RootRecordID)
	}
	if len(group.Root.Children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(group.Root.Children))
	}

	// 断言兄弟关系
	c1 := group.Root.Children[0]
	c2 := group.Root.Children[1]
	c3 := group.Root.Children[2]

	if c1.Record.ID != "102" || c1.SiblingRelation != SiblingSequential {
		t.Errorf("child 1 expected SEQUENTIAL, got id=%s relation=%s", c1.Record.ID, c1.SiblingRelation)
	}
	if c2.Record.ID != "103" || c2.SiblingRelation != SiblingParallel {
		t.Errorf("child 2 expected PARALLEL, got id=%s relation=%s", c2.Record.ID, c2.SiblingRelation)
	}
	if c3.Record.ID != "104" || c3.SiblingRelation != SiblingCompensation {
		t.Errorf("child 3 expected COMPENSATION, got id=%s relation=%s", c3.Record.ID, c3.SiblingRelation)
	}

	// 断言瀑布百分比有效区间 [0, 100]
	if group.Root.WaterfallOffsetPct != 0 || group.Root.WaterfallWidthPct <= 0 {
		t.Errorf("invalid root waterfall: offset=%.2f width=%.2f", group.Root.WaterfallOffsetPct, group.Root.WaterfallWidthPct)
	}
	if c1.WaterfallOffsetPct < 0 || c1.WaterfallWidthPct <= 0 {
		t.Errorf("invalid c1 waterfall: offset=%.2f width=%.2f", c1.WaterfallOffsetPct, c1.WaterfallWidthPct)
	}

	// 2. 构造恶意环路 SpanA <-> SpanB 与 30 层超深链
	loopRecords := []models.TrafficRecord{
		{
			ID:              201,
			SessionID:       2,
			SeqNo:           1,
			ProcessID:       10,
			Direction:       "INGRESS",
			ServiceName:     "LoopService",
			Operation:       "Entry",
			TraceID:         "trace-loop",
			SpanID:          "span-root",
			FatherSpanID:    "",
			ReqCapturedAtNs: tBase,
			CreatedAt:       time.Now(),
		},
		{
			ID:              202,
			SessionID:       2,
			SeqNo:           2,
			ProcessID:       10,
			Direction:       "EGRESS",
			ServiceName:     "LoopService",
			Operation:       "StepA",
			TraceID:         "trace-loop",
			SpanID:          "span-A",
			FatherSpanID:    "span-B", // 互环 A->B
			ReqCapturedAtNs: tBase + 1000,
			CreatedAt:       time.Now(),
		},
		{
			ID:              203,
			SessionID:       2,
			SeqNo:           3,
			ProcessID:       10,
			Direction:       "EGRESS",
			ServiceName:     "LoopService",
			Operation:       "StepB",
			TraceID:         "trace-loop",
			SpanID:          "span-B",
			FatherSpanID:    "span-A", // 互环 B->A
			ReqCapturedAtNs: tBase + 2000,
			CreatedAt:       time.Now(),
		},
	}

	// 补充 30 层超深链挂在 span-root 下
	prevSpan := "span-root"
	for depth := 1; depth <= 30; depth++ {
		curSpan := fmt.Sprintf("deep-%d", depth)
		loopRecords = append(loopRecords, models.TrafficRecord{
			ID:              uint64(300 + depth),
			SessionID:       2,
			SeqNo:           int64(10 + depth),
			ProcessID:       10,
			Direction:       "EGRESS",
			ServiceName:     "DeepService",
			Operation:       fmt.Sprintf("Depth_%d", depth),
			TraceID:         "trace-loop",
			SpanID:          curSpan,
			FatherSpanID:    prevSpan,
			ReqCapturedAtNs: tBase + int64(depth)*10000,
			CreatedAt:       time.Now(),
		})
		prevSpan = curSpan
	}

	// 执行建树，必须在毫秒内返回且不崩溃、不无限递归
	start := time.Now()
	loopTrees := BuildTransactionTrees(nil, loopRecords, nil, nil)
	duration := time.Since(start)

	if duration > 1*time.Second {
		t.Fatalf("tree build took too long: %v, possible cycle hang", duration)
	}

	// 验证最大深度不超过 MaxTreeDepth (16)
	var maxDepthFound int
	var checkMaxDepth func(n *TransactionTreeNodeDTO)
	checkMaxDepth = func(n *TransactionTreeNodeDTO) {
		if n.Depth > maxDepthFound {
			maxDepthFound = n.Depth
		}
		for i := range n.Children {
			checkMaxDepth(&n.Children[i])
		}
	}
	for i := range loopTrees {
		checkMaxDepth(&loopTrees[i].Root)
	}

	if maxDepthFound > MaxTreeDepth {
		t.Errorf("max depth %d exceeded limit %d", maxDepthFound, MaxTreeDepth)
	}
}

// T-M13-06: 多进程 EgressProxy 跨跳缝合
func TestCallGraph_CrossProcessHopStitching(t *testing.T) {
	tBase := int64(1700000000000000000)

	// 1. DOTR 精确跨跳缝合:
	// Proc_Order (EGRESS, EphemeralPort=51234) ➔ Proc_Inv (INGRESS, Port=58901)
	latEg := int64(3500)
	latIn := int64(2800)
	rspEg := tBase + latEg*1000
	rspIn := tBase + 300_000 + latIn*1000

	dotrRecords := []models.TrafficRecord{
		{
			ID:              401,
			SessionID:       10,
			SeqNo:           1,
			ProcessID:       11, // Proc_Order
			Direction:       "EGRESS",
			ServiceName:     "dophi.inventory.v1.InventoryService",
			Operation:       "LockStock",
			TraceID:         "trace-stitch-1",
			SpanID:          "span-stitch-1",
			FatherSpanID:    "span-root",
			SrcAddr:         "127.0.0.1:51234",
			DstAddr:         "127.0.0.1:58901",
			ReqCapturedAtNs: tBase,
			RspCapturedAtNs: &rspEg,
			LatencyUs:       &latEg,
			StatusCode:      models.U64Numeric(0),
			CreatedAt:       time.Now(),
		},
		{
			ID:              402,
			SessionID:       10,
			SeqNo:           2,
			ProcessID:       12, // Proc_Inv
			Direction:       "INGRESS",
			ServiceName:     "dophi.inventory.v1.InventoryService",
			Operation:       "LockStock",
			TraceID:         "trace-stitch-1",
			SpanID:          "span-stitch-1", // 同 Trace 同 Span
			FatherSpanID:    "span-root",
			SrcAddr:         "127.0.0.1:51234",
			DstAddr:         "127.0.0.1:58901",
			ReqCapturedAtNs: tBase + 300_000, // 300μs 后到达下游
			RspCapturedAtNs: &rspIn,
			LatencyUs:       &latIn,
			StatusCode:      models.U64Numeric(0),
			CreatedAt:       time.Now(),
		},
	}

	hydrated := HydrateRecords(nil, nil, dotrRecords)
	hMap := make(map[uint64]*TrafficRecordSummary)
	for i := range dotrRecords {
		hMap[dotrRecords[i].ID] = &hydrated[i]
	}

	stitchedMap, netHopMap, confMap, consumed := StitchCrossHops(dotrRecords, hMap)
	if stitchedMap[401] == nil {
		t.Fatalf("expected record 401 to be stitched")
	}
	if stitchedMap[401].ID != "402" {
		t.Errorf("expected stitched ingress ID 402, got %s", stitchedMap[401].ID)
	}
	if !consumed[402] {
		t.Errorf("expected ingress 402 to be marked consumed")
	}
	if confMap[401] != ConfidenceDOTRExact {
		t.Errorf("expected confidence DOTR_EXACT, got %s", confMap[401])
	}
	expectedHopUs := latEg - latIn // 3500 - 2800 = 700us
	if netHopMap[401] != expectedHopUs {
		t.Errorf("expected netHopUs=%d, got %d", expectedHopUs, netHopMap[401])
	}

	// 2. 无 Trace 场景下的 SHA256 指纹兜底缝合
	reqPayload := []byte(`{"sku_id":"SKU_888","count":2}`)
	hash := sha256.Sum256(reqPayload)
	hashHex := hex.EncodeToString(hash[:])

	fpRecords := []models.TrafficRecord{
		{
			ID:               501,
			SessionID:        20,
			SeqNo:            1,
			ProcessID:        21,
			Direction:        "EGRESS",
			ServiceName:      "ConfigService",
			Operation:        "GetConfig",
			TraceID:          "", // 无 Trace
			SpanID:           "",
			SrcAddr:          "192.168.1.10:43210",
			DstAddr:          "192.168.1.20:8090",
			ReqPayloadSHA256: hashHex,
			ReqCapturedAtNs:  tBase,
			LatencyUs:        &latEg,
			StatusCode:       models.U64Numeric(0),
			CreatedAt:        time.Now(),
		},
		{
			ID:               502,
			SessionID:        20,
			SeqNo:            2,
			ProcessID:        22,
			Direction:        "INGRESS",
			ServiceName:      "ConfigService",
			Operation:        "GetConfig",
			TraceID:          "", // 无 Trace
			SpanID:           "",
			SrcAddr:          "192.168.1.10:43210",
			DstAddr:          "192.168.1.20:8090",
			ReqPayloadSHA256: hashHex,
			ReqCapturedAtNs:  tBase + 2_000_000, // 2ms 延迟 (<= 50ms)
			LatencyUs:        &latIn,
			StatusCode:       models.U64Numeric(0),
			CreatedAt:        time.Now(),
		},
	}

	fpHydrated := HydrateRecords(nil, nil, fpRecords)
	fpHMap := make(map[uint64]*TrafficRecordSummary)
	for i := range fpRecords {
		fpHMap[fpRecords[i].ID] = &fpHydrated[i]
	}

	fpStitched, _, fpConf, fpConsumed := StitchCrossHops(fpRecords, fpHMap)
	if fpStitched[501] == nil || fpStitched[501].ID != "502" {
		t.Fatalf("expected fingerprint stitch 501 -> 502")
	}
	if !fpConsumed[502] {
		t.Errorf("expected 502 to be consumed")
	}
	if fpConf[501] != ConfidenceFingerprintInferred {
		t.Errorf("expected FINGERPRINT_INFERRED, got %s", fpConf[501])
	}
}

// T-M13-07: 参数血缘与跨事务实体链
func TestCallGraph_ParameterLineage_AndEntityLifecycleChains(t *testing.T) {
	// 1. 单事务内参数传递血缘
	pldLock := []byte(`{"req":{"sku_id":"SKU-1001","qty":1},"rsp":{"code":0,"lock_id":"LK_778899","status":"LOCKED"}}`)
	pldConfirm := []byte(`{"req":{"lock_id":"LK_778899","user_id":"U123","flag":true,"count":1},"rsp":{"code":0,"success":true}}`)

	singleTxRecords := []models.TrafficRecord{
		{
			ID:          601,
			SeqNo:       1,
			PayloadJSON: datatypes.JSON(pldLock),
			CreatedAt:   time.Now(),
		},
		{
			ID:          602,
			SeqNo:       2,
			PayloadJSON: datatypes.JSON(pldConfirm),
			CreatedAt:   time.Now(),
		},
	}

	lineages := ExtractParameterLineage(singleTxRecords)
	if len(lineages) == 0 {
		t.Fatalf("expected parameter lineage edges, got none")
	}

	// 验证精确匹配到了 lock_id = "LK_778899"
	foundLockLineage := false
	for _, l := range lineages {
		if l.FromRecordID == "601" && l.ToRecordID == "602" && l.SampleValue == "LK_778899" {
			foundLockLineage = true
			if l.FromFieldPath != "rsp.lock_id" || l.ToFieldPath != "req.lock_id" {
				t.Errorf("unexpected field paths: from=%s to=%s", l.FromFieldPath, l.ToFieldPath)
			}
		}
		// 断言低熵值未生成血缘
		if l.SampleValue == "true" || l.SampleValue == "1" || l.SampleValue == "0" {
			t.Errorf("low entropy value leaked into lineage: %s", l.SampleValue)
		}
	}

	if !foundLockLineage {
		t.Errorf("did not find expected lock_id lineage edge")
	}

	// 2. 跨事务实体链与噪声过滤:
	// Tx 1: CreateOrder (rsp 产出 order_id="ORD_9981", user_id="U123") -> PRODUCER
	// Tx 2: GetOrder (req 消费 order_id="ORD_9981", user_id="U123") -> CONSUMER
	// Tx 3: CancelOrder (req 消费 order_id="ORD_9981", user_id="U123") -> CONSUMER
	pldCreate := []byte(`{"req":{"amount":99},"rsp":{"order_id":"ORD_9981","user_id":"U123"}}`)
	pldGet := []byte(`{"req":{"order_id":"ORD_9981","user_id":"U123"},"rsp":{"status":"PENDING"}}`)
	pldCancel := []byte(`{"req":{"order_id":"ORD_9981","user_id":"U123"},"rsp":{"status":"CANCELLED"}}`)

	multiTxRecords := []models.TrafficRecord{
		{
			ID:          701,
			SeqNo:       1,
			Operation:   "CreateOrder",
			PayloadJSON: datatypes.JSON(pldCreate),
			CreatedAt:   time.Now(),
		},
		{
			ID:          702,
			SeqNo:       2,
			Operation:   "GetOrder",
			PayloadJSON: datatypes.JSON(pldGet),
			CreatedAt:   time.Now(),
		},
		{
			ID:          703,
			SeqNo:       3,
			Operation:   "CancelOrder",
			PayloadJSON: datatypes.JSON(pldCancel),
			CreatedAt:   time.Now(),
		},
	}

	chains := BuildEntityChains(multiTxRecords)
	if len(chains) == 0 {
		t.Fatalf("expected entity chains, got none")
	}

	var orderChain *EntityLifecycleChainDTO
	for i := range chains {
		if chains[i].FieldName == "order_id" && chains[i].EntityValue == "ORD_9981" {
			orderChain = &chains[i]
		}
		// 断言 user_id 被噪声词表过滤
		if chains[i].FieldName == "user_id" {
			t.Errorf("user_id should be filtered by noise blacklist")
		}
	}

	if orderChain == nil {
		t.Fatalf("expected order_id entity chain not found")
	}

	if len(orderChain.Steps) != 3 {
		t.Fatalf("expected 3 steps in orderChain, got %d", len(orderChain.Steps))
	}

	if orderChain.Steps[0].Role != "PRODUCER" || orderChain.Steps[0].Operation != "CreateOrder" {
		t.Errorf("step 0 expected PRODUCER CreateOrder, got role=%s op=%s", orderChain.Steps[0].Role, orderChain.Steps[0].Operation)
	}
	if orderChain.Steps[1].Role != "CONSUMER" || orderChain.Steps[1].Operation != "GetOrder" {
		t.Errorf("step 1 expected CONSUMER GetOrder, got role=%s op=%s", orderChain.Steps[1].Role, orderChain.Steps[1].Operation)
	}
	if orderChain.Steps[2].Role != "CONSUMER" || orderChain.Steps[2].Operation != "CancelOrder" {
		t.Errorf("step 2 expected CONSUMER CancelOrder, got role=%s op=%s", orderChain.Steps[2].Role, orderChain.Steps[2].Operation)
	}
}

// AC-01: 树构建水合透传真实 db 实例，断言 Record.SrcModuleName 与 DstModuleName 正确填充
func TestBuildTransactionTrees_WithDBHydration(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}

	// 1. 创建子系统与功能组
	subsystem := models.Subsystem{Code: "COMMERCE", Name: "电商交易子系统"}
	if err := db.Create(&subsystem).Error; err != nil {
		t.Fatalf("创建子系统失败: %v", err)
	}
	fnGroup := models.FunctionalGroup{SubsystemID: subsystem.ID, Code: "COMM_FN", Name: "电商功能组"}
	if err := db.Create(&fnGroup).Error; err != nil {
		t.Fatalf("创建功能组失败: %v", err)
	}

	// 2. 创建进程组与架构进程蓝图
	pgrp := models.ArchProcessGroup{Code: "PGRP_TEST", Name: "测试进程组"}
	if err := db.Create(&pgrp).Error; err != nil {
		t.Fatalf("创建进程组失败: %v", err)
	}
	archOrder := models.ArchProcess{GroupID: pgrp.ID, Code: "order-svc", Name: "订单交易服务"}
	if err := db.Create(&archOrder).Error; err != nil {
		t.Fatalf("创建架构进程失败: %v", err)
	}

	// 3. 创建测试模块
	modOrder := models.Module{SubsystemID: subsystem.ID, GroupID: fnGroup.ID, Code: "MOD_ORDER", Name: "订单交易核心模块", ArchProcessID: &archOrder.ID}
	modStock := models.Module{SubsystemID: subsystem.ID, GroupID: fnGroup.ID, Code: "MOD_STOCK", Name: "仓储库存履约模块"}
	if err := db.Create(&modOrder).Error; err != nil {
		t.Fatalf("创建模块 modOrder 失败: %v", err)
	}
	if err := db.Create(&modStock).Error; err != nil {
		t.Fatalf("创建模块 modStock 失败: %v", err)
	}

	// 4. 创建物理 Agent 与 Process 实例
	agent := models.Agent{Name: "agt-test", Endpoint: "127.0.0.1:8090", Status: models.AgentStatusOnline, Scope: models.ScopeShared}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("创建 Agent 失败: %v", err)
	}
	procOrder := models.Process{AgentID: agent.ID, ArchProcessID: archOrder.ID, EffectiveListenPort: 9001}
	if err := db.Create(&procOrder).Error; err != nil {
		t.Fatalf("创建 Process 失败: %v", err)
	}

	targetProcIDs, _ := json.Marshal([]uint64{procOrder.ID})
	session := models.RecordingSession{
		SessionNo:        "REC_TEST_HYDRATE",
		Name:             "水合测试会话",
		Scope:            models.ScopeShared,
		Status:           models.SessionStatusCompleted,
		TargetProcessIDs: datatypes.JSON(targetProcIDs),
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("创建 Session 失败: %v", err)
	}

	// 5. 构造 records
	lat := int64(15000)
	rspT := int64(1700000000015000000)
	records := []models.TrafficRecord{
		{
			ID:              201,
			SessionID:       session.ID,
			ProcessID:       procOrder.ID,
			TraceID:         "trace-hydrate-01",
			SpanID:          "span-root",
			Direction:       "INGRESS",
			ServiceName:     "demo.order.OrderService",
			Operation:       "CreateOrder",
			SrcModuleID:     nil, // 外部请求
			DstModuleID:     &modOrder.ID,
			ReqCapturedAtNs: 1700000000000000000,
			RspCapturedAtNs: &rspT,
			LatencyUs:       &lat,
			StatusCode:      models.U64Numeric(0),
			CreatedAt:       time.Now(),
		},
		{
			ID:              202,
			SessionID:       session.ID,
			ProcessID:       procOrder.ID,
			TraceID:         "trace-hydrate-01",
			SpanID:          "span-child",
			FatherSpanID:    "span-root",
			Direction:       "EGRESS",
			ServiceName:     "demo.inventory.InventoryService",
			Operation:       "LockStock",
			SrcModuleID:     &modOrder.ID,
			DstModuleID:     &modStock.ID,
			ReqCapturedAtNs: 1700000000005000000,
			RspCapturedAtNs: &rspT,
			LatencyUs:       &lat,
			StatusCode:      models.U64Numeric(0),
			CreatedAt:       time.Now(),
		},
	}

	trees := BuildTransactionTrees(db, records, &session, nil)
	if len(trees) != 1 {
		t.Fatalf("expected 1 tree, got %d", len(trees))
	}

	root := trees[0].Root

	// 断言根节点 (INGRESS)
	if root.Record.DstModuleName != "订单交易核心模块" {
		t.Errorf("expected DstModuleName '订单交易核心模块', got '%s'", root.Record.DstModuleName)
	}
	if root.Record.ProcessName != "订单交易服务" {
		t.Errorf("expected ProcessName '订单交易服务', got '%s'", root.Record.ProcessName)
	}
	if root.Record.ArchProcessName != "订单交易服务" {
		t.Errorf("expected ArchProcessName '订单交易服务', got '%s'", root.Record.ArchProcessName)
	}

	// 断言子节点 (EGRESS)
	if len(root.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(root.Children))
	}
	child := root.Children[0]
	if child.Record.SrcModuleName != "订单交易核心模块" {
		t.Errorf("expected child SrcModuleName '订单交易核心模块', got '%s'", child.Record.SrcModuleName)
	}
	if child.Record.DstModuleName != "仓储库存履约模块" {
		t.Errorf("expected child DstModuleName '仓储库存履约模块', got '%s'", child.Record.DstModuleName)
	}
}
