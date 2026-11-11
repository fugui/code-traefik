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

	modA := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_ORDER_A", Name: "订单模块A", CreatedBy: 1}
	modB := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_ORDER_B", Name: "订单模块B", CreatedBy: 1}
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

	pgrp := models.ArchProcessGroup{Code: "PGRP_TEST", Name: "测试组"}
	db.Create(&pgrp)
	archMulti := models.ArchProcess{
		GroupID:           pgrp.ID,
		Code:              "PROC_MULTI",
		Name:              "proc-multi-service",
		DefaultListenPort: 18001,
		DefaultCmdPattern: "/opt/bin/multi_proc",
	}
	db.Create(&archMulti)

	procMulti := models.Process{
		AgentID:             agt.ID,
		ArchProcessID:       archMulti.ID,
		EffectiveListenPort: 18001,
		ProbeDesiredState:   models.ProbeDesiredStateArmed,
		ProbeStatus:         models.ProbeStatusReady,
		CreatedBy:           1,
	}
	if err := db.Create(&procMulti).Error; err != nil {
		t.Fatalf("创建进程失败: %v", err)
	}

	// 绑定 Mod_A 与 Mod_B 到 archMulti
	db.Model(&modA).Update("arch_process_id", archMulti.ID)
	db.Model(&modB).Update("arch_process_id", archMulti.ID)

	// 3. 配置服务依赖契约：
	// Mod_B 声明依赖外部或内部 ("dophi.stock-StockService")
	depB := models.ModuleDependency{
		ConsumerModuleID: modB.ID,
		IsExternal:       true,
		ExternalService:  "dophi.stock-StockService",
		CreatedBy:        1,
	}
	if err := db.Create(&depB).Error; err != nil {
		t.Fatalf("创建 Mod_B DEPEND 契约失败: %v", err)
	}

	// 构造环境数据结构
	processModules := map[uint64][]uint64{
		procMulti.ID: {modA.ID, modB.ID},
	}
	dependMap := map[uint64]map[string]bool{
		modB.ID: {
			"dophi.stock-StockService":             true,
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

	mod1 := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_1", Name: "模块1", CreatedBy: 1}
	mod2 := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_2", Name: "模块2", CreatedBy: 1}
	mod3 := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_3", Name: "模块3", CreatedBy: 1}
	mod4 := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_4", Name: "模块4", CreatedBy: 1}
	modUnmon := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_UNMON", Name: "未监控模块", CreatedBy: 1}
	db.Create(&mod1)
	db.Create(&mod2)
	db.Create(&mod3)
	db.Create(&mod4)
	db.Create(&modUnmon)

	// 2. 进程部署
	agt := models.Agent{Name: "agt-sc06", Endpoint: "http://127.0.0.1:9092", Scope: models.ScopeShared, Status: models.AgentStatusOnline, CreatedBy: 1}
	db.Create(&agt)

	pgrpSc := models.ArchProcessGroup{Code: "PGRP_SC", Name: "SC进程组"}
	db.Create(&pgrpSc)

	archA := models.ArchProcess{GroupID: pgrpSc.ID, Code: "PROC_A", Name: "proc-A", DefaultListenPort: 19001, DefaultCmdPattern: "proc-a"}
	archB := models.ArchProcess{GroupID: pgrpSc.ID, Code: "PROC_B", Name: "proc-B", DefaultListenPort: 19002, DefaultCmdPattern: "proc-b"}
	archC := models.ArchProcess{GroupID: pgrpSc.ID, Code: "PROC_C", Name: "proc-C", DefaultListenPort: 19003, DefaultCmdPattern: "proc-c"}
	archUnmon := models.ArchProcess{GroupID: pgrpSc.ID, Code: "PROC_UNMON", Name: "proc-unmon", DefaultListenPort: 19004, DefaultCmdPattern: "proc-unmon"}
	db.Create(&archA)
	db.Create(&archB)
	db.Create(&archC)
	db.Create(&archUnmon)

	// procA 承载 mod1 与 mod2 (多模块进程)
	procA := models.Process{AgentID: agt.ID, ArchProcessID: archA.ID, EffectiveListenPort: 19001, ProbeDesiredState: models.ProbeDesiredStateArmed, ProbeStatus: models.ProbeStatusReady, CreatedBy: 1}
	// procB 承载 mod3
	procB := models.Process{AgentID: agt.ID, ArchProcessID: archB.ID, EffectiveListenPort: 19002, ProbeDesiredState: models.ProbeDesiredStateArmed, ProbeStatus: models.ProbeStatusReady, CreatedBy: 1}
	// procC 承载 mod4
	procC := models.Process{AgentID: agt.ID, ArchProcessID: archC.ID, EffectiveListenPort: 19003, ProbeDesiredState: models.ProbeDesiredStateArmed, ProbeStatus: models.ProbeStatusReady, CreatedBy: 1}
	// procUnmon 承载 modUnmon (未加入本次录制监控)
	procUnmon := models.Process{AgentID: agt.ID, ArchProcessID: archUnmon.ID, EffectiveListenPort: 19004, ProbeDesiredState: models.ProbeDesiredStateArmed, ProbeStatus: models.ProbeStatusReady, CreatedBy: 1}

	db.Create(&procA)
	db.Create(&procB)
	db.Create(&procC)
	db.Create(&procUnmon)

	db.Model(&mod1).Update("arch_process_id", archA.ID)
	db.Model(&mod2).Update("arch_process_id", archA.ID)
	db.Model(&mod3).Update("arch_process_id", archB.ID)
	db.Model(&mod4).Update("arch_process_id", archC.ID)
	db.Model(&modUnmon).Update("arch_process_id", archUnmon.ID)

	// 3. 服务契约与依赖定义
	// (1) mod3 PROVIDE dophi.b-ServiceB
	svcB := models.ModuleService{ModuleID: mod3.ID, ServiceName: "dophi.b-ServiceB", CreatedBy: 1}
	db.Create(&svcB)
	// (2) mod2 PROVIDE dophi.a-ServiceA2 (同在 procA)
	svcA2 := models.ModuleService{ModuleID: mod2.ID, ServiceName: "dophi.a-ServiceA2", CreatedBy: 1}
	db.Create(&svcA2)
	// (3) modUnmon PROVIDE dophi.unmon-ServiceUnmon 与 dophi.unmon-OutboundService
	svcUnmon := models.ModuleService{ModuleID: modUnmon.ID, ServiceName: "dophi.unmon-ServiceUnmon", CreatedBy: 1}
	db.Create(&svcUnmon)
	svcUnmonOutbound := models.ModuleService{ModuleID: modUnmon.ID, ServiceName: "dophi.unmon-OutboundService", CreatedBy: 1}
	db.Create(&svcUnmonOutbound)
	// (4) mod4 PROVIDE dophi.c-ServiceC
	svcC := models.ModuleService{ModuleID: mod4.ID, ServiceName: "dophi.c-ServiceC", CreatedBy: 1}
	db.Create(&svcC)
	// (5) mod3 PROVIDE dophi.v-ViolationService (未被 mod1 声明依赖)
	svcV := models.ModuleService{ModuleID: mod3.ID, ServiceName: "dophi.v-ViolationService", CreatedBy: 1}
	db.Create(&svcV)

	// 静态依赖声明：
	// ① 期望 VERIFIED: mod1 DEPEND mod3(dophi.b-ServiceB) 双端纳管闭环
	db.Create(&models.ModuleDependency{ConsumerModuleID: mod1.ID, ServiceID: &svcB.ID, ProviderModuleID: &mod3.ID, CreatedBy: 1})
	// ② 期望 VERIFIED (外部依赖实测证实): mod1 DEPEND pay.v1.ExternalGateway
	db.Create(&models.ModuleDependency{ConsumerModuleID: mod1.ID, IsExternal: true, ExternalService: "pay.v1.ExternalGateway", CreatedBy: 1})
	// ③ 期望 OUTBOUND_VERIFIED: mod1 DEPEND modUnmon(dophi.unmon-OutboundService) 目标未纳管但实测捕获出站
	db.Create(&models.ModuleDependency{ConsumerModuleID: mod1.ID, ServiceID: &svcUnmonOutbound.ID, ProviderModuleID: &modUnmon.ID, CreatedBy: 1})
	// ④ 期望 NOT_OBSERVABLE: mod1 DEPEND mod2(dophi.a-ServiceA2) 同进程内依赖
	db.Create(&models.ModuleDependency{ConsumerModuleID: mod1.ID, ServiceID: &svcA2.ID, ProviderModuleID: &mod2.ID, CreatedBy: 1})
	// ⑤ 期望 NOT_MONITORED: mod1 DEPEND modUnmon(dophi.unmon-ServiceUnmon) 目标未纳管且未触发调用
	db.Create(&models.ModuleDependency{ConsumerModuleID: mod1.ID, ServiceID: &svcUnmon.ID, ProviderModuleID: &modUnmon.ID, CreatedBy: 1})
	// ⑥ 期望 UNCOVERED: mod1 DEPEND mod4(dophi.c-ServiceC) 均纳入监控但实测未调用
	db.Create(&models.ModuleDependency{ConsumerModuleID: mod1.ID, ServiceID: &svcC.ID, ProviderModuleID: &mod4.ID, CreatedBy: 1})

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
	// 记录 1: 对应 ① VERIFIED 双端纳管 (mod1 -> mod3: dophi.b-ServiceB:MethodB)
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

	// 记录 1b: 对应 ② VERIFIED 外部服务实测出站 (mod1 -> pay.v1.ExternalGateway:PayMethod)
	recExternal := models.TrafficRecord{
		SessionID:          session.ID,
		CreatedAt:          now,
		AgentID:            agt.ID,
		ProcessID:          procA.ID,
		Direction:          models.TrafficDirEgress,
		ServiceName:        "pay.v1.ExternalGateway",
		Operation:          "PayMethod",
		SrcModuleID:        &mod1.ID,
		DstModuleID:        nil,
		SrcAttributionMode: AttributionSingleModuleProc,
		ExchangeType:       models.ExchangeUnary,
		SNO:                models.U64Numeric(1004),
	}
	db.Create(&recExternal)

	// 记录 1c: 对应 ③ OUTBOUND_VERIFIED 单端已出站待闭环 (mod1 -> modUnmon: dophi.unmon-OutboundService:MethodOutbound)
	recOutbound := models.TrafficRecord{
		SessionID:          session.ID,
		CreatedAt:          now,
		AgentID:            agt.ID,
		ProcessID:          procA.ID,
		Direction:          models.TrafficDirEgress,
		ServiceName:        "dophi.unmon-OutboundService",
		Operation:          "MethodOutbound",
		SrcModuleID:        &mod1.ID,
		DstModuleID:        &modUnmon.ID,
		SrcAttributionMode: AttributionUniqueDepend,
		ExchangeType:       models.ExchangeUnary,
		SNO:                models.U64Numeric(1005),
	}
	db.Create(&recOutbound)

	// 记录 2: 对应 ⑦ VIOLATION (未声明的跨进程调用: mod1 私自调用 mod3 的未声明服务 dophi.v-ViolationService)
	rec2 := models.TrafficRecord{
		SessionID:          session.ID,
		CreatedAt:          now,
		AgentID:            agt.ID,
		ProcessID:          procA.ID,
		Direction:          models.TrafficDirEgress,
		ServiceName:        "dophi.v-ViolationService",
		Operation:          "UndeclaredMethod",
		SrcModuleID:        &mod1.ID,
		DstModuleID:        &mod3.ID,
		SrcAttributionMode: AttributionSingleModuleProc,
		ExchangeType:       models.ExchangeUnary,
		SNO:                models.U64Numeric(1002),
	}
	db.Create(&rec2)

	// 记录 3: 对应 ⑧ UNATTRIBUTED (procA 发出，缺少 Trace 且 mod1/mod2 均未声明，降级为 PROCESS_LEVEL)
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

	t.Logf("一致性比对结果: Verified=%d, OutboundVerified=%d, Violation=%d, NotObservable=%d, NotMonitored=%d, Uncovered=%d, Unattributed=%d",
		report.VerifiedCount, report.OutboundVerifiedCount, report.ViolationCount, report.NotObservableCount,
		report.NotMonitoredCount, report.UncoveredCount, report.UnattributedCount)

	// 7. 严格断言全部 7 类结果
	if report.VerifiedCount != 2 {
		t.Errorf("预期 VerifiedCount=2 (内部闭环1+外部出站1)，实际: %d", report.VerifiedCount)
	}
	if report.OutboundVerifiedCount != 1 {
		t.Errorf("预期 OutboundVerifiedCount=1，实际: %d", report.OutboundVerifiedCount)
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

func TestVerifySessionTopology_DispatchAndImplUnresolved(t *testing.T) {
	db := setupIsolatedConsistencyDB(t)
	ctx := context.Background()

	sub := models.Subsystem{Code: "SUB_DISP", Name: "分发一致性子系统", CreatedBy: 1}
	db.Create(&sub)
	grp := models.FunctionalGroup{SubsystemID: sub.ID, Code: "GRP_DISP", Name: "功能组", Level: 1, CreatedBy: 1}
	db.Create(&grp)

	libA := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_LIB_A", Name: "定义库A", ModuleKind: models.ModuleKindContractLib, CreatedBy: 1}
	modB := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_IMPL_B", Name: "实现模块B", ModuleKind: models.ModuleKindBusiness, CreatedBy: 1}
	modC := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_IMPL_C", Name: "实现模块C", ModuleKind: models.ModuleKindBusiness, CreatedBy: 1}
	modX := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_DISP_X", Name: "分发网关X", ModuleKind: models.ModuleKindBusiness, CreatedBy: 1}
	db.Create(&libA)
	db.Create(&modB)
	db.Create(&modC)
	db.Create(&modX)

	pgrp := models.ArchProcessGroup{Code: "PGRP_DISP", Name: "进程组"}
	db.Create(&pgrp)
	apX := models.ArchProcess{GroupID: pgrp.ID, Code: "ARCH_X", Name: "网关进程X", DefaultListenPort: 18020}
	apB := models.ArchProcess{GroupID: pgrp.ID, Code: "ARCH_B", Name: "实现进程B", DefaultListenPort: 18021}
	apC := models.ArchProcess{GroupID: pgrp.ID, Code: "ARCH_C", Name: "实现进程C", DefaultListenPort: 18022}
	db.Create(&apX)
	db.Create(&apB)
	db.Create(&apC)

	db.Model(&modX).Update("arch_process_id", apX.ID)
	db.Model(&modB).Update("arch_process_id", apB.ID)
	db.Model(&modC).Update("arch_process_id", apC.ID)

	agt := models.Agent{Name: "agt-disp", Endpoint: "http://127.0.0.1:18029", Scope: models.ScopeShared, Status: models.AgentStatusOnline, CreatedBy: 1}
	db.Create(&agt)
	procX := models.Process{AgentID: agt.ID, Agent: &agt, ArchProcessID: apX.ID, EffectiveListenPort: 18020, CreatedBy: 1}
	procB := models.Process{AgentID: agt.ID, Agent: &agt, ArchProcessID: apB.ID, EffectiveListenPort: 18021, CreatedBy: 1}
	procC := models.Process{AgentID: agt.ID, Agent: &agt, ArchProcessID: apC.ID, EffectiveListenPort: 18022, CreatedBy: 1}
	db.Create(&procX)
	db.Create(&procB)
	db.Create(&procC)

	// 注册契约与实现
	contract := models.ServiceContract{
		ServiceName:     "cros.dispatch.PayService",
		DefinerModuleID: libA.ID,
		ProvideMode:     models.ContractProvideModeMultiImpl,
		CreatedBy:       1,
	}
	db.Create(&contract)

	msB := models.ModuleService{ModuleID: modB.ID, ContractID: &contract.ID, ServiceName: contract.ServiceName, ImplKey: "alipay", CreatedBy: 1}
	msC := models.ModuleService{ModuleID: modC.ID, ContractID: &contract.ID, ServiceName: contract.ServiceName, ImplKey: "wechat", CreatedBy: 1}
	db.Create(&msB)
	db.Create(&msC)

	// X 声明对契约的 DISPATCH 依赖
	depX := models.ModuleDependency{ConsumerModuleID: modX.ID, ContractID: &contract.ID, CreatedBy: 1}
	db.Create(&depX)

	// 会话：监控 procX 和 procB (procC 未纳管)
	targetPIDs, _ := json.Marshal([]uint64{procX.ID, procB.ID})
	session := models.RecordingSession{
		SessionNo:          "REC_DISP_01",
		Name:               "分发一致性会话",
		Scope:              models.ScopeShared,
		Status:             models.SessionStatusCompleted,
		TargetProcessIDs:   datatypes.JSON(targetPIDs),
		LeasedAgentIDs:     datatypes.JSON([]byte("[]")),
		ProcessSnapshots:   datatypes.JSON([]byte("[]")),
		EndpointSnapshots:  datatypes.JSON([]byte("[]")),
		ProcessDiagnostics: datatypes.JSON([]byte("[]")),
		CreatedBy:          1,
	}
	db.Create(&session)

	now := time.Now()
	// 记录 1: X -> B (双端纳管 -> VERIFIED)
	rec1 := models.TrafficRecord{
		SessionID:          session.ID,
		CreatedAt:          now,
		AgentID:            agt.ID,
		ProcessID:          procX.ID,
		Direction:          models.TrafficDirEgress,
		ServiceName:        contract.ServiceName,
		Operation:          "Pay",
		SrcModuleID:        &modX.ID,
		DstModuleID:        &modB.ID,
		DstContractID:      &contract.ID,
		SrcAttributionMode: AttributionUniqueDepend,
	}
	// 记录 2: X -> 未知实现 (IMPL_UNRESOLVED)
	rec2 := models.TrafficRecord{
		SessionID:          session.ID,
		CreatedAt:          now,
		AgentID:            agt.ID,
		ProcessID:          procX.ID,
		Direction:          models.TrafficDirEgress,
		ServiceName:        contract.ServiceName,
		Operation:          "Pay",
		SrcModuleID:        &modX.ID,
		DstModuleID:        nil,
		DstContractID:      &contract.ID,
		SrcAttributionMode: AttributionUniqueDepend,
	}
	db.Create(&rec1)
	db.Create(&rec2)

	report, err := VerifySessionTopology(ctx, db, session.ID, true)
	if err != nil {
		t.Fatalf("VerifySessionTopology 失败: %v", err)
	}

	// 检查 IMPL_UNRESOLVED 边
	foundUnresolved := false
	foundVerified := false
	for _, edge := range report.Edges {
		if edge.Status == StatusImplUnresolved {
			foundUnresolved = true
			if edge.DstModuleID == nil || *edge.DstModuleID != libA.ID {
				t.Errorf("IMPL_UNRESOLVED 边的目标模块应为定义库 FM A (%d), got %v", libA.ID, edge.DstModuleID)
			}
			if edge.ContractID == nil || *edge.ContractID != contract.ID {
				t.Errorf("IMPL_UNRESOLVED 边应携带 ContractID=%d, got %v", contract.ID, edge.ContractID)
			}
		}
		if edge.Status == StatusVerified && edge.DstModuleID != nil && *edge.DstModuleID == modB.ID {
			foundVerified = true
		}
	}

	if !foundUnresolved {
		t.Errorf("预期生成 IMPL_UNRESOLVED 边，实际未检出")
	}
	if !foundVerified {
		t.Errorf("预期 X -> B 为 VERIFIED 边，实际未检出")
	}
	if report.ImplUnresolvedCount != 1 {
		t.Errorf("预期 ImplUnresolvedCount=1，实际: %d", report.ImplUnresolvedCount)
	}
}

func TestVerifySessionTopology_SpecificImplCrossCallViolation(t *testing.T) {
	db := setupIsolatedConsistencyDB(t)
	ctx := context.Background()

	sub := models.Subsystem{Code: "SUB_CROSS", Name: "越权调用子系统", CreatedBy: 1}
	db.Create(&sub)
	grp := models.FunctionalGroup{SubsystemID: sub.ID, Code: "GRP_CROSS", Name: "功能组", Level: 1, CreatedBy: 1}
	db.Create(&grp)

	libA := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_LIB_AC", Name: "定义库AC", ModuleKind: models.ModuleKindContractLib, CreatedBy: 1}
	modB := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_B_ONLY", Name: "模块B", ModuleKind: models.ModuleKindBusiness, CreatedBy: 1}
	modC := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_C_OTHER", Name: "模块C", ModuleKind: models.ModuleKindBusiness, CreatedBy: 1}
	modX := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_X_CALLER", Name: "调用方X", ModuleKind: models.ModuleKindBusiness, CreatedBy: 1}
	db.Create(&libA)
	db.Create(&modB)
	db.Create(&modC)
	db.Create(&modX)

	pgrp := models.ArchProcessGroup{Code: "PGRP_CROSS", Name: "进程组"}
	db.Create(&pgrp)
	apX := models.ArchProcess{GroupID: pgrp.ID, Code: "ARCH_X2", Name: "网关进程X2", DefaultListenPort: 18030}
	apB := models.ArchProcess{GroupID: pgrp.ID, Code: "ARCH_B2", Name: "实现进程B2", DefaultListenPort: 18031}
	apC := models.ArchProcess{GroupID: pgrp.ID, Code: "ARCH_C2", Name: "实现进程C2", DefaultListenPort: 18032}
	db.Create(&apX)
	db.Create(&apB)
	db.Create(&apC)

	db.Model(&modX).Update("arch_process_id", apX.ID)
	db.Model(&modB).Update("arch_process_id", apB.ID)
	db.Model(&modC).Update("arch_process_id", apC.ID)

	agt := models.Agent{Name: "agt-cross", Endpoint: "http://127.0.0.1:18039", Scope: models.ScopeShared, Status: models.AgentStatusOnline, CreatedBy: 1}
	db.Create(&agt)
	procX := models.Process{AgentID: agt.ID, Agent: &agt, ArchProcessID: apX.ID, EffectiveListenPort: 18030, CreatedBy: 1}
	procB := models.Process{AgentID: agt.ID, Agent: &agt, ArchProcessID: apB.ID, EffectiveListenPort: 18031, CreatedBy: 1}
	procC := models.Process{AgentID: agt.ID, Agent: &agt, ArchProcessID: apC.ID, EffectiveListenPort: 18032, CreatedBy: 1}
	db.Create(&procX)
	db.Create(&procB)
	db.Create(&procC)

	contract := models.ServiceContract{
		ServiceName:     "cros.cross.PayService",
		DefinerModuleID: libA.ID,
		ProvideMode:     models.ContractProvideModeMultiImpl,
		CreatedBy:       1,
	}
	db.Create(&contract)

	msB := models.ModuleService{ModuleID: modB.ID, ContractID: &contract.ID, ServiceName: contract.ServiceName, ImplKey: "alipay", CreatedBy: 1}
	msC := models.ModuleService{ModuleID: modC.ID, ContractID: &contract.ID, ServiceName: contract.ServiceName, ImplKey: "wechat", CreatedBy: 1}
	db.Create(&msB)
	db.Create(&msC)

	// X 明确仅声明对具体实现 B 的依赖 (未声明 C)
	depXtoB := models.ModuleDependency{ConsumerModuleID: modX.ID, ServiceID: &msB.ID, ProviderModuleID: &modB.ID, CreatedBy: 1}
	db.Create(&depXtoB)

	// 会话：监控 procX, procB, procC
	targetPIDs, _ := json.Marshal([]uint64{procX.ID, procB.ID, procC.ID})
	session := models.RecordingSession{
		SessionNo:          "REC_CROSS_01",
		Name:               "越权调用测试会话",
		Scope:              models.ScopeShared,
		Status:             models.SessionStatusCompleted,
		TargetProcessIDs:   datatypes.JSON(targetPIDs),
		LeasedAgentIDs:     datatypes.JSON([]byte("[]")),
		ProcessSnapshots:   datatypes.JSON([]byte("[]")),
		EndpointSnapshots:  datatypes.JSON([]byte("[]")),
		ProcessDiagnostics: datatypes.JSON([]byte("[]")),
		CreatedBy:          1,
	}
	db.Create(&session)

	// 实测中 X 却调用了 C (越权调用)
	now := time.Now()
	recCallC := models.TrafficRecord{
		SessionID:          session.ID,
		CreatedAt:          now,
		AgentID:            agt.ID,
		ProcessID:          procX.ID,
		Direction:          models.TrafficDirEgress,
		ServiceName:        contract.ServiceName,
		Operation:          "Pay",
		SrcModuleID:        &modX.ID,
		DstModuleID:        &modC.ID,
		DstContractID:      &contract.ID,
		SrcAttributionMode: AttributionUniqueDepend,
	}
	db.Create(&recCallC)

	report, err := VerifySessionTopology(ctx, db, session.ID, true)
	if err != nil {
		t.Fatalf("VerifySessionTopology 失败: %v", err)
	}

	// 验证: X -> C 必须判定为 VIOLATION; 声明的 X -> B 未调用应判定为 UNCOVERED
	foundViolationC := false
	foundUncoveredB := false
	for _, edge := range report.Edges {
		if edge.Status == StatusViolation && edge.DstModuleID != nil && *edge.DstModuleID == modC.ID {
			foundViolationC = true
		}
		if edge.Status == StatusUncovered && edge.DstModuleID != nil && *edge.DstModuleID == modB.ID {
			foundUncoveredB = true
		}
	}

	if !foundViolationC {
		t.Errorf("针对未声明的实现 C 调用必须判定为 VIOLATION，实际未检出")
	}
	if !foundUncoveredB {
		t.Errorf("已声明但未调用的实现 B 必须判定为 UNCOVERED，实际未检出")
	}
}
