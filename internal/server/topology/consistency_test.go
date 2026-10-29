package topology

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-traefik/config"
	"code-traefik/models"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func setupIsolatedConsistencyDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}
	return db
}

// TestResolveSourceModuleThreeTierPriority 验证出站请求三级优先级来源模块判定 (F-05.2)
func TestResolveSourceModuleThreeTierPriority(t *testing.T) {
	db := setupIsolatedConsistencyDB(t)
	ctx := context.Background()

	// 1. 初始化架构元素：1个子系统、1个功能组、2个功能模块
	sub := models.Subsystem{Code: "SUB_ORDER", Name: "订单子系统", CreatedBy: 1}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatalf("创建子系统失败: %v", err)
	}
	grp := models.FunctionalGroup{SubsystemID: sub.ID, Code: "GRP_CORE", Name: "核心组", Level: 1, CreatedBy: 1}
	if err := db.Create(&grp).Error; err != nil {
		t.Fatalf("创建功能组失败: %v", err)
	}

	modA := models.Module{GroupID: grp.ID, Code: "MOD_ORDER_A", Name: "订单模块A", CreatedBy: 1}
	modB := models.Module{GroupID: grp.ID, Code: "MOD_ORDER_B", Name: "订单模块B", CreatedBy: 1}
	if err := db.Create(&modA).Error; err != nil {
		t.Fatalf("创建模块A失败: %v", err)
	}
	if err := db.Create(&modB).Error; err != nil {
		t.Fatalf("创建模块B失败: %v", err)
	}

	// 2. 初始化 Agent 与多模块共存进程
	agt := models.Agent{Name: "agt-node-1", Endpoint: "http://127.0.0.1:9091", Scope: models.ScopeShared, Status: models.AgentStatusOnline, CreatedBy: 1}
	if err := db.Create(&agt).Error; err != nil {
		t.Fatalf("创建 Agent 失败: %v", err)
	}

	procMulti := models.Process{
		AgentID:    agt.ID,
		Name:       "proc-multi-service",
		ListenPort: 18001,
		CmdPattern: "/opt/bin/multi_proc",
		CreatedBy:  1,
	}
	if err := db.Create(&procMulti).Error; err != nil {
		t.Fatalf("创建进程失败: %v", err)
	}

	// 绑定 Mod_A 与 Mod_B 到 procMulti
	if err := db.Create(&models.ProcessModule{ProcessID: procMulti.ID, ModuleID: modA.ID}).Error; err != nil {
		t.Fatalf("绑定 Mod_A 失败: %v", err)
	}
	if err := db.Create(&models.ProcessModule{ProcessID: procMulti.ID, ModuleID: modB.ID}).Error; err != nil {
		t.Fatalf("绑定 Mod_B 失败: %v", err)
	}

	// 3. 配置接口契约：
	// Mod_B 声明 DEPEND("dophi.stock-StockService", "DeductStock")
	ifaceB := models.ModuleInterface{
		ModuleID:     modB.ID,
		RelationType: "DEPEND",
		ServiceName:  "dophi.stock-StockService",
		Operation:    "DeductStock",
		CreatedBy:    1,
	}
	if err := db.Create(&ifaceB).Error; err != nil {
		t.Fatalf("创建 Mod_B DEPEND 契约失败: %v", err)
	}

	// 构造环境数据结构
	processModules := map[uint64][]uint64{
		procMulti.ID: {modA.ID, modB.ID},
	}
	dependMap := map[uint64]map[string]bool{
		modB.ID: {
			"dophi.stock-StockService:DeductStock": true,
		},
	}

	// 场景 1: 带 FatherSpanID 匹配同进程入站 Mod_A 的 SpanID -> 优先级 ① TRACE_SPAN
	parentIngress := &models.TrafficRecord{
		ID:          101,
		ProcessID:   procMulti.ID,
		Direction:   models.TrafficDirIngress,
		SpanID:      "SPAN_ORDER_001",
		DstModuleID: &modA.ID,
	}
	ingressMap := map[string]*models.TrafficRecord{
		"SPAN_ORDER_001": parentIngress,
	}

	egress1 := &models.TrafficRecord{
		ProcessID:    procMulti.ID,
		Direction:    models.TrafficDirEgress,
		TraceID:      "TRACE_001",
		FatherSpanID: "SPAN_ORDER_001",
		ServiceName:  "dophi.pay-PaymentService",
		Operation:    "PreAuth",
	}

	srcMod1, mode1, cands1 := ResolveSourceModule(processModules, ingressMap, egress1, dependMap)
	if mode1 != AttributionTraceSpan {
		t.Fatalf("场景1 预期模式 TRACE_SPAN，实际: %s", mode1)
	}
	if srcMod1 == nil || *srcMod1 != modA.ID {
		t.Fatalf("场景1 预期归属 Mod_A (%d)，实际: %v", modA.ID, srcMod1)
	}
	if len(cands1) != 0 {
		t.Fatalf("场景1 candidates 预期为空，实际: %+v", cands1)
	}

	// 场景 2: 无 Trace 但仅 Mod_B 声明了该 DEPEND -> 优先级 ② UNIQUE_DEPEND
	egress2 := &models.TrafficRecord{
		ProcessID:   procMulti.ID,
		Direction:   models.TrafficDirEgress,
		ServiceName: "dophi.stock-StockService",
		Operation:   "DeductStock",
	}
	srcMod2, mode2, cands2 := ResolveSourceModule(processModules, ingressMap, egress2, dependMap)
	if mode2 != AttributionUniqueDepend {
		t.Fatalf("场景2 预期模式 UNIQUE_DEPEND，实际: %s", mode2)
	}
	if srcMod2 == nil || *srcMod2 != modB.ID {
		t.Fatalf("场景2 预期归属 Mod_B (%d)，实际: %v", modB.ID, srcMod2)
	}
	if len(cands2) != 0 {
		t.Fatalf("场景2 candidates 预期为空，实际: %+v", cands2)
	}

	// 场景 3: 无 Trace 且两模块均未声明 (或均声明) 该 DEPEND -> 优先级 ③ PROCESS_LEVEL
	egress3 := &models.TrafficRecord{
		ProcessID:   procMulti.ID,
		Direction:   models.TrafficDirEgress,
		ServiceName: "dophi.notify-NotifyService",
		Operation:   "SendSMS",
	}
	srcMod3, mode3, cands3 := ResolveSourceModule(processModules, ingressMap, egress3, dependMap)
	if mode3 != AttributionProcessLevel {
		t.Fatalf("场景3 预期模式 PROCESS_LEVEL，实际: %s", mode3)
	}
	if srcMod3 != nil {
		t.Fatalf("场景3 预期 SrcModule 为 nil，实际: %v", *srcMod3)
	}
	if len(cands3) != 2 || cands3[0] != modA.ID || cands3[1] != modB.ID {
		t.Fatalf("场景3 预期候选集包含 [Mod_A, Mod_B]，实际: %+v", cands3)
	}

	_ = ctx
}

// TestSC06_ArchitectureConsistencyVerification 验收场景 SC-06: 架构一致性验证引擎全六分类精准比对 (SC-06, F-05.3)
func TestSC06_ArchitectureConsistencyVerification(t *testing.T) {
	db := setupIsolatedConsistencyDB(t)
	ctx := context.Background()

	// 1. 基础架构元素
	sub := models.Subsystem{Code: "SUB_BIZ", Name: "业务子系统", CreatedBy: 1}
	db.Create(&sub)
	grp := models.FunctionalGroup{SubsystemID: sub.ID, Code: "GRP_BIZ", Name: "业务功能组", Level: 1, CreatedBy: 1}
	db.Create(&grp)

	mod1 := models.Module{GroupID: grp.ID, Code: "MOD_1", Name: "模块1", CreatedBy: 1}
	mod2 := models.Module{GroupID: grp.ID, Code: "MOD_2", Name: "模块2", CreatedBy: 1}
	mod3 := models.Module{GroupID: grp.ID, Code: "MOD_3", Name: "模块3", CreatedBy: 1}
	mod4 := models.Module{GroupID: grp.ID, Code: "MOD_4", Name: "模块4", CreatedBy: 1}
	modUnmon := models.Module{GroupID: grp.ID, Code: "MOD_UNMON", Name: "未监控模块", CreatedBy: 1}
	db.Create(&mod1)
	db.Create(&mod2)
	db.Create(&mod3)
	db.Create(&mod4)
	db.Create(&modUnmon)

	// 2. 进程部署
	agt := models.Agent{Name: "agt-sc06", Endpoint: "http://127.0.0.1:9092", Scope: models.ScopeShared, Status: models.AgentStatusOnline, CreatedBy: 1}
	db.Create(&agt)

	// procA 承载 mod1 与 mod2 (多模块进程)
	procA := models.Process{AgentID: agt.ID, Name: "proc-A", ListenPort: 19001, CmdPattern: "proc-a", CreatedBy: 1}
	// procB 承载 mod3
	procB := models.Process{AgentID: agt.ID, Name: "proc-B", ListenPort: 19002, CmdPattern: "proc-b", CreatedBy: 1}
	// procC 承载 mod4
	procC := models.Process{AgentID: agt.ID, Name: "proc-C", ListenPort: 19003, CmdPattern: "proc-c", CreatedBy: 1}
	// procUnmon 承载 modUnmon (未加入本次录制监控)
	procUnmon := models.Process{AgentID: agt.ID, Name: "proc-unmon", ListenPort: 19004, CmdPattern: "proc-unmon", CreatedBy: 1}

	db.Create(&procA)
	db.Create(&procB)
	db.Create(&procC)
	db.Create(&procUnmon)

	db.Create(&models.ProcessModule{ProcessID: procA.ID, ModuleID: mod1.ID})
	db.Create(&models.ProcessModule{ProcessID: procA.ID, ModuleID: mod2.ID})
	db.Create(&models.ProcessModule{ProcessID: procB.ID, ModuleID: mod3.ID})
	db.Create(&models.ProcessModule{ProcessID: procC.ID, ModuleID: mod4.ID})
	db.Create(&models.ProcessModule{ProcessID: procUnmon.ID, ModuleID: modUnmon.ID})

	// 3. 接口契约定义
	// (1) mod3 PROVIDE dophi.b-ServiceB:MethodB
	db.Create(&models.ModuleInterface{ModuleID: mod3.ID, RelationType: "PROVIDE", ServiceName: "dophi.b-ServiceB", Operation: "MethodB", CreatedBy: 1})
	// (2) mod2 PROVIDE dophi.a-ServiceA2:MethodA2 (同在 procA)
	db.Create(&models.ModuleInterface{ModuleID: mod2.ID, RelationType: "PROVIDE", ServiceName: "dophi.a-ServiceA2", Operation: "MethodA2", CreatedBy: 1})
	// (3) modUnmon PROVIDE dophi.unmon-ServiceUnmon:MethodUnmon
	db.Create(&models.ModuleInterface{ModuleID: modUnmon.ID, RelationType: "PROVIDE", ServiceName: "dophi.unmon-ServiceUnmon", Operation: "MethodUnmon", CreatedBy: 1})
	// (4) mod4 PROVIDE dophi.c-ServiceC:MethodC
	db.Create(&models.ModuleInterface{ModuleID: mod4.ID, RelationType: "PROVIDE", ServiceName: "dophi.c-ServiceC", Operation: "MethodC", CreatedBy: 1})

	// 静态依赖声明：
	// ① 期望 VERIFIED: mod1 DEPEND mod3(dophi.b-ServiceB:MethodB)
	db.Create(&models.ModuleInterface{ModuleID: mod1.ID, RelationType: "DEPEND", ServiceName: "dophi.b-ServiceB", Operation: "MethodB", CreatedBy: 1})
	// ③ 期望 NOT_OBSERVABLE: mod1 DEPEND mod2(dophi.a-ServiceA2:MethodA2) 同进程内依赖
	db.Create(&models.ModuleInterface{ModuleID: mod1.ID, RelationType: "DEPEND", ServiceName: "dophi.a-ServiceA2", Operation: "MethodA2", CreatedBy: 1})
	// ④ 期望 NOT_MONITORED: mod1 DEPEND modUnmon(dophi.unmon-ServiceUnmon:MethodUnmon)
	db.Create(&models.ModuleInterface{ModuleID: mod1.ID, RelationType: "DEPEND", ServiceName: "dophi.unmon-ServiceUnmon", Operation: "MethodUnmon", CreatedBy: 1})
	// ⑤ 期望 UNCOVERED: mod1 DEPEND mod4(dophi.c-ServiceC:MethodC) 均纳入监控但实测未调用
	db.Create(&models.ModuleInterface{ModuleID: mod1.ID, RelationType: "DEPEND", ServiceName: "dophi.c-ServiceC", Operation: "MethodC", CreatedBy: 1})

	// 4. 创建录制批次：纳入 procA, procB, procC (procUnmon 未纳入)
	targetProcBytes, _ := json.Marshal([]uint64{procA.ID, procB.ID, procC.ID})
	session := models.RecordingSession{
		SessionNo:        "REC_SC06_TEST_001",
		Name:             "SC-06 架构一致性验证会话",
		Scope:            models.ScopeShared,
		OwnerID:          1,
		Status:           models.SessionStatusCompleted,
		TargetProcessIDs: datatypes.JSON(targetProcBytes),
		CreatedBy:        1,
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("创建录制会话失败: %v", err)
	}

	now := time.Now()
	// 5. 插入实测流量记录：
	// 记录 1: 对应 ① VERIFIED (mod1 -> mod3: dophi.b-ServiceB:MethodB)
	rec1 := models.TrafficRecord{
		SessionID:          session.ID,
		CreatedAt:          now,
		AgentID:            agt.ID,
		ProcessID:          procA.ID,
		Direction:          models.TrafficDirEgress,
		ServiceName:        "dophi.b-ServiceB",
		Operation:          "MethodB",
		SrcModuleID:        &mod1.ID,
		DstModuleID:        &mod3.ID,
		SrcAttributionMode: AttributionUniqueDepend,
		ExchangeType:       models.ExchangeUnary,
		SNO:                models.U64Numeric(1001),
	}
	db.Create(&rec1)

	// 记录 2: 对应 ② VIOLATION (未声明的跨进程调用: mod1 私自调用 mod3 的未声明接口 dophi.b-ServiceB:UndeclaredMethod)
	rec2 := models.TrafficRecord{
		SessionID:          session.ID,
		CreatedAt:          now,
		AgentID:            agt.ID,
		ProcessID:          procA.ID,
		Direction:          models.TrafficDirEgress,
		ServiceName:        "dophi.b-ServiceB",
		Operation:          "UndeclaredMethod",
		SrcModuleID:        &mod1.ID,
		DstModuleID:        &mod3.ID,
		SrcAttributionMode: AttributionSingleModuleProc,
		ExchangeType:       models.ExchangeUnary,
		SNO:                models.U64Numeric(1002),
	}
	db.Create(&rec2)

	// 记录 3: 对应 ⑥ UNATTRIBUTED (procA 发出，缺少 Trace 且 mod1/mod2 均未声明，降级为 PROCESS_LEVEL)
	candBytes, _ := json.Marshal([]uint64{mod1.ID, mod2.ID})
	rec3 := models.TrafficRecord{
		SessionID:             session.ID,
		CreatedAt:             now,
		AgentID:               agt.ID,
		ProcessID:             procA.ID,
		Direction:             models.TrafficDirEgress,
		ServiceName:           "dophi.x-AmbiguousService",
		Operation:             "AmbiguousMethod",
		SrcModuleID:           nil,
		SrcAttributionMode:    AttributionProcessLevel,
		SrcCandidateModuleIDs: datatypes.JSON(candBytes),
		ExchangeType:          models.ExchangeUnary,
		SNO:                   models.U64Numeric(1003),
	}
	db.Create(&rec3)

	// 6. 执行 VerifySessionTopology
	report, err := VerifySessionTopology(ctx, db, session.ID, true)
	if err != nil {
		t.Fatalf("VerifySessionTopology 失败: %v", err)
	}

	t.Logf("一致性比对结果: Verified=%d, Violation=%d, NotObservable=%d, NotMonitored=%d, Uncovered=%d, Unattributed=%d",
		report.VerifiedCount, report.ViolationCount, report.NotObservableCount,
		report.NotMonitoredCount, report.UncoveredCount, report.UnattributedCount)

	// 7. 严格断言全部 6 类结果各出现且仅出现 1 次
	if report.VerifiedCount != 1 {
		t.Errorf("预期 VerifiedCount=1，实际: %d", report.VerifiedCount)
	}
	if report.ViolationCount != 1 {
		t.Errorf("预期 ViolationCount=1，实际: %d", report.ViolationCount)
	}
	if report.NotObservableCount != 1 {
		t.Errorf("预期 NotObservableCount=1，实际: %d", report.NotObservableCount)
	}
	if report.NotMonitoredCount != 1 {
		t.Errorf("预期 NotMonitoredCount=1，实际: %d", report.NotMonitoredCount)
	}
	if report.UncoveredCount != 1 {
		t.Errorf("预期 UncoveredCount=1，实际: %d", report.UncoveredCount)
	}
	if report.UnattributedCount != 1 {
		t.Errorf("预期 UnattributedCount=1，实际: %d", report.UnattributedCount)
	}
}
