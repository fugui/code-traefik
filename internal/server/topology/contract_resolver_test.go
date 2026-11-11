package topology

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"code-traefik/models"
	"gorm.io/datatypes"
)

func TestResolveDestinationModule_MultiImplThreeTierFallback(t *testing.T) {
	db := setupIsolatedTopologyDB(t)
	ctx := context.Background()

	// 1. 架构拓扑搭建
	sub, _ := CreateSubsystem(db, SubsystemInput{Code: "SUB_RES", Name: "解析器测试子系统"})
	grp, _ := CreateFunctionalGroup(db, FunctionalGroupInput{SubsystemID: sub.ID, Code: "GRP_RES", Name: "解析器功能组"})

	// 定义库 A
	libA, err := CreateModule(db, ModuleInput{
		GroupID:    grp.ID,
		Code:       "MOD_LIB_A",
		Name:       "接口定义库A",
		ModuleKind: models.ModuleKindContractLib,
	})
	if err != nil {
		t.Fatalf("创建接口库失败: %v", err)
	}

	// 架构进程 B 与 C
	apg := models.ArchProcessGroup{Code: "APG_RES", Name: "进程组"}
	db.Create(&apg)
	apB := models.ArchProcess{GroupID: apg.ID, Code: "PROC_B", Name: "进程B", DefaultListenPort: 8081}
	apC := models.ArchProcess{GroupID: apg.ID, Code: "PROC_C", Name: "进程C", DefaultListenPort: 8082}
	db.Create(&apB)
	db.Create(&apC)

	// 业务模块 B 与 C
	modB, err := CreateModule(db, ModuleInput{
		GroupID:       grp.ID,
		Code:          "MOD_BIZ_B",
		Name:          "业务实现模块B",
		ModuleKind:    models.ModuleKindBusiness,
		ArchProcessID: &apB.ID,
	})
	if err != nil {
		t.Fatalf("创建模块B失败: %v", err)
	}
	modC, err := CreateModule(db, ModuleInput{
		GroupID:       grp.ID,
		Code:          "MOD_BIZ_C",
		Name:          "业务实现模块C",
		ModuleKind:    models.ModuleKindBusiness,
		ArchProcessID: &apC.ID,
	})
	if err != nil {
		t.Fatalf("创建模块C失败: %v", err)
	}

	// 2. 注册多实现契约
	contract, err := CreateServiceContract(db, ServiceContractInput{
		ServiceName:     "cros.trade.PayService",
		DefinerModuleID: libA.ID,
	})
	if err != nil {
		t.Fatalf("注册契约失败: %v", err)
	}

	// 模块 B 登记实现 alipay
	_, err = CreateModuleService(db, ModuleServiceInput{
		ModuleID:    modB.ID,
		ServiceName: "cros.trade.PayService",
		ContractID:  &contract.ID,
		ImplKey:     "alipay",
	})
	if err != nil {
		t.Fatalf("注册实现 alipay 失败: %v", err)
	}

	// 模块 C 登记实现 wechat
	_, err = CreateModuleService(db, ModuleServiceInput{
		ModuleID:    modC.ID,
		ServiceName: "cros.trade.PayService",
		ContractID:  &contract.ID,
		ImplKey:     "wechat",
	})
	if err != nil {
		t.Fatalf("注册实现 wechat 失败: %v", err)
	}

	// 模块 B 登记独占服务 RefundService
	_, err = CreateModuleService(db, ModuleServiceInput{
		ModuleID:    modB.ID,
		ServiceName: "cros.trade.RefundService",
	})
	if err != nil {
		t.Fatalf("注册独占服务失败: %v", err)
	}

	// 3. 加载解析器
	resolver, err := LoadContractResolver(ctx, db)
	if err != nil {
		t.Fatalf("LoadContractResolver 失败: %v", err)
	}

	// --- Tier 1 验证：独占服务直穿模块 B，ContractID 为 nil ---
	mID, cID := resolver.ResolveDestinationFull("cros.trade.RefundService", models.TrafficDirEgress, 0, "127.0.0.1:8081", nil, nil, nil)
	if mID == nil || *mID != modB.ID || cID != nil {
		t.Fatalf("Tier 1 独占服务定位失败, got mod=%v, contract=%v", mID, cID)
	}

	// --- Tier 2a 验证：多实现契约 INGRESS 流量，通过 targetArchProcID 直穿模块 B ---
	mID, cID = resolver.ResolveDestinationFull("cros.trade.PayService", models.TrafficDirIngress, apB.ID, "127.0.0.1:8081", nil, nil, nil)
	if mID == nil || *mID != modB.ID {
		t.Fatalf("Tier 2a INGRESS 模块B判定失败, got mod=%v", mID)
	}
	if cID == nil || *cID != contract.ID {
		t.Fatalf("Tier 2a 期望 contractID=%d, got %v", contract.ID, cID)
	}

	// --- Tier 2b 验证：多实现契约 EGRESS 流量，通过 endpoint_snapshots 匹配 port 8082 直穿模块 C ---
	snapshots := []models.EndpointSnapshotItem{
		{
			IP:            "127.0.0.1",
			Port:          8082,
			ArchProcessID: apC.ID,
			ModuleIDs:     []uint64{modC.ID},
		},
	}
	mID, cID = resolver.ResolveDestinationFull("cros.trade.PayService", models.TrafficDirEgress, 0, "127.0.0.1:8082", snapshots, nil, nil)
	if mID == nil || *mID != modC.ID {
		t.Fatalf("Tier 2b 快照直穿模块C判定失败, got mod=%v", mID)
	}
	if cID == nil || *cID != contract.ID {
		t.Fatalf("Tier 2b 期望 contractID=%d, got %v", contract.ID, cID)
	}

	// --- Tier 2c 验证：多实现契约 EGRESS 流量，通过 envProcesses 直穿模块 B ---
	envProcs := map[string]uint64{
		"192.168.1.100:8081": apB.ID,
	}
	mID, cID = resolver.ResolveDestinationFull("cros.trade.PayService", models.TrafficDirEgress, 0, "192.168.1.100:8081", nil, envProcs, nil)
	if mID == nil || *mID != modB.ID {
		t.Fatalf("Tier 2c envProcesses 直穿模块B判定失败, got mod=%v", mID)
	}
	if cID == nil || *cID != contract.ID {
		t.Fatalf("Tier 2c 期望 contractID=%d, got %v", contract.ID, cID)
	}

	// --- Tier 2e 验证：无法裁决实现时，返回 dstModuleID=nil, dstContractID=contract.ID (严禁盲分 FM A) ---
	mID, cID = resolver.ResolveDestinationFull("cros.trade.PayService", models.TrafficDirEgress, 0, "10.0.0.99:9999", nil, nil, nil)
	if mID != nil {
		t.Fatalf("Tier 2e 无法裁决时 dstModuleID 必须为 nil, got %v", *mID)
	}
	if cID == nil || *cID != contract.ID {
		t.Fatalf("Tier 2e 期望保留 contractID=%d, got %v", contract.ID, cID)
	}

	// --- Tier 3 验证：未登记服务，返回 nil, nil ---
	mID, cID = resolver.ResolveDestinationFull("unknown.Service", models.TrafficDirEgress, 0, "127.0.0.1:8081", nil, nil, nil)
	if mID != nil || cID != nil {
		t.Fatalf("Tier 3 未知服务期望 nil, nil, got mod=%v, contract=%v", mID, cID)
	}
}

func TestCompileProcessDownstreams_ExpandDispatchContract(t *testing.T) {
	db := setupIsolatedTopologyDB(t)

	sub, _ := CreateSubsystem(db, SubsystemInput{Code: "SUB_DS", Name: "下游展开子系统"})
	grp, _ := CreateFunctionalGroup(db, FunctionalGroupInput{SubsystemID: sub.ID, Code: "GRP_DS", Name: "下游展开功能组"})

	// 定义库 A
	libA, _ := CreateModule(db, ModuleInput{
		GroupID:    grp.ID,
		Code:       "MOD_LIB_A2",
		Name:       "接口库A2",
		ModuleKind: models.ModuleKindContractLib,
	})

	// 架构进程 X, B, C
	apg := models.ArchProcessGroup{Code: "APG_DS", Name: "进程组"}
	db.Create(&apg)
	apX := models.ArchProcess{GroupID: apg.ID, Code: "PROC_X", Name: "分发网关进程X", DefaultListenPort: 8080}
	apB := models.ArchProcess{GroupID: apg.ID, Code: "PROC_B", Name: "实现进程B", DefaultListenPort: 8081}
	apC := models.ArchProcess{GroupID: apg.ID, Code: "PROC_C", Name: "实现进程C", DefaultListenPort: 8082}
	db.Create(&apX)
	db.Create(&apB)
	db.Create(&apC)

	// 模块 X, B, C
	modX, _ := CreateModule(db, ModuleInput{
		GroupID:       grp.ID,
		Code:          "MOD_GW_X",
		Name:          "分发网关模块X",
		ModuleKind:    models.ModuleKindBusiness,
		ArchProcessID: &apX.ID,
	})
	modB, _ := CreateModule(db, ModuleInput{
		GroupID:       grp.ID,
		Code:          "MOD_IMPL_B",
		Name:          "实现模块B",
		ModuleKind:    models.ModuleKindBusiness,
		ArchProcessID: &apB.ID,
	})
	modC, _ := CreateModule(db, ModuleInput{
		GroupID:       grp.ID,
		Code:          "MOD_IMPL_C",
		Name:          "实现模块C",
		ModuleKind:    models.ModuleKindBusiness,
		ArchProcessID: &apC.ID,
	})

	// 物理 Agent
	agt := models.Agent{Name: "agt-local", Endpoint: "127.0.0.1:8090", Scope: models.ScopeShared, Status: models.AgentStatusOnline}
	db.Create(&agt)

	// 物理进程 X, B, C
	pX, _ := CreateProcess(db, ProcessInput{AgentID: agt.ID, ArchProcessID: apX.ID, ListenPort: 8080})
	_, _ = CreateProcess(db, ProcessInput{AgentID: agt.ID, ArchProcessID: apB.ID, ListenPort: 8081})
	_, _ = CreateProcess(db, ProcessInput{AgentID: agt.ID, ArchProcessID: apC.ID, ListenPort: 8082})

	// 契约与实现
	contract, _ := CreateServiceContract(db, ServiceContractInput{
		ServiceName:     "cros.dispatch.OrderService",
		DefinerModuleID: libA.ID,
	})
	_, _ = CreateModuleService(db, ModuleServiceInput{
		ModuleID:    modB.ID,
		ServiceName: "cros.dispatch.OrderService",
		ContractID:  &contract.ID,
		ImplKey:     "impl_b",
	})
	_, _ = CreateModuleService(db, ModuleServiceInput{
		ModuleID:    modC.ID,
		ServiceName: "cros.dispatch.OrderService",
		ContractID:  &contract.ID,
		ImplKey:     "impl_c",
	})

	// 网关 X 声明 DISPATCH 契约依赖
	_, err := CreateModuleDependency(db, ModuleDependencyInput{
		ConsumerModuleID: modX.ID,
		ContractID:       &contract.ID,
	})
	if err != nil {
		t.Fatalf("声明 DISPATCH 契约依赖失败: %v", err)
	}

	// 编译网关进程 X 的下游端点
	downstreams, err := CompileProcessDownstreams(db, pX.ID, nil, nil)
	if err != nil {
		t.Fatalf("CompileProcessDownstreams 失败: %v", err)
	}

	// 验证：下游端点中必须同时包含进程 B (8081) 和进程 C (8082)，且携带对应的 DstTag
	foundB := false
	foundC := false
	for _, ds := range downstreams {
		if ds.Port == 8081 && ds.DstTag == apB.Code {
			foundB = true
		}
		if ds.Port == 8082 && ds.DstTag == apC.Code {
			foundC = true
		}
	}

	if !foundB {
		t.Fatalf("下游清单中缺少实现进程 B 的端点 (8081, DstTag=%s), downstreams=%+v", apB.Code, downstreams)
	}
	if !foundC {
		t.Fatalf("下游清单中缺少实现进程 C 的端点 (8082, DstTag=%s), downstreams=%+v", apC.Code, downstreams)
	}
}

func TestOfflineBatchAttribution_MultiImplThreeTier(t *testing.T) {
	db := setupIsolatedTopologyDB(t)
	ctx := context.Background()

	sub, _ := CreateSubsystem(db, SubsystemInput{Code: "SUB_ATTR", Name: "归集测试子系统"})
	grp, _ := CreateFunctionalGroup(db, FunctionalGroupInput{SubsystemID: sub.ID, Code: "GRP_ATTR", Name: "归集功能组"})

	libA, _ := CreateModule(db, ModuleInput{
		GroupID:    grp.ID,
		Code:       "MOD_LIB_ATTR",
		Name:       "接口库ATTR",
		ModuleKind: models.ModuleKindContractLib,
	})

	apg := models.ArchProcessGroup{Code: "APG_ATTR", Name: "归集进程组"}
	db.Create(&apg)
	apB := models.ArchProcess{GroupID: apg.ID, Code: "PROC_ATTR_B", Name: "进程B", DefaultListenPort: 8081}
	apC := models.ArchProcess{GroupID: apg.ID, Code: "PROC_ATTR_C", Name: "进程C", DefaultListenPort: 8082}
	db.Create(&apB)
	db.Create(&apC)

	modB, _ := CreateModule(db, ModuleInput{
		GroupID:       grp.ID,
		Code:          "MOD_B",
		Name:          "模块B",
		ModuleKind:    models.ModuleKindBusiness,
		ArchProcessID: &apB.ID,
	})
	modC, _ := CreateModule(db, ModuleInput{
		GroupID:       grp.ID,
		Code:          "MOD_C",
		Name:          "模块C",
		ModuleKind:    models.ModuleKindBusiness,
		ArchProcessID: &apC.ID,
	})

	agt := models.Agent{Name: "agt-attr", Endpoint: "127.0.0.1:8090", Scope: models.ScopeShared, Status: models.AgentStatusOnline}
	db.Create(&agt)
	pB, _ := CreateProcess(db, ProcessInput{AgentID: agt.ID, ArchProcessID: apB.ID, ListenPort: 8081})
	pC, _ := CreateProcess(db, ProcessInput{AgentID: agt.ID, ArchProcessID: apC.ID, ListenPort: 8082})

	contract, _ := CreateServiceContract(db, ServiceContractInput{
		ServiceName:     "cros.attr.PaymentService",
		DefinerModuleID: libA.ID,
	})
	_, _ = CreateModuleService(db, ModuleServiceInput{
		ModuleID:    modB.ID,
		ServiceName: "cros.attr.PaymentService",
		ContractID:  &contract.ID,
		ImplKey:     "alipay",
	})
	_, _ = CreateModuleService(db, ModuleServiceInput{
		ModuleID:    modC.ID,
		ServiceName: "cros.attr.PaymentService",
		ContractID:  &contract.ID,
		ImplKey:     "wechat",
	})

	// 录制会话，包含 EndpointSnapshots
	epSnapshots := []models.EndpointSnapshotItem{
		{
			IP:            "127.0.0.1",
			Port:          8082,
			ArchProcessID: apC.ID,
			ProcessID:     pC.ID,
			ModuleIDs:     []uint64{modC.ID},
		},
	}
	epBytes, _ := json.Marshal(epSnapshots)
	session := models.RecordingSession{
		SessionNo:          "REC_ATTR_01",
		Name:               "归集测试批次",
		Scope:              models.ScopeShared,
		Status:             models.SessionStatusCompleted,
		TargetProcessIDs:   datatypes.JSON([]byte("[]")),
		LeasedAgentIDs:     datatypes.JSON([]byte("[]")),
		ProcessSnapshots:   datatypes.JSON([]byte("[]")),
		ProcessDiagnostics: datatypes.JSON([]byte("[]")),
		EndpointSnapshots:  datatypes.JSON(epBytes),
	}
	db.Create(&session)

	// 3条记录：
	// rec1: INGRESS 到 pB (应该归集到 modB, contract)
	// rec2: EGRESS 到 127.0.0.1:8082 (匹配快照，应该归集到 modC, contract)
	// rec3: EGRESS 到 10.10.10.10:9999 (未解析实现，归集到 nil, contract)
	now := time.Now()
	rec1 := models.TrafficRecord{
		CreatedAt:   now,
		SessionID:   session.ID,
		ProcessID:   pB.ID,
		Direction:   models.TrafficDirIngress,
		ServiceName: "cros.attr.PaymentService",
		Operation:   "Pay",
		DstAddr:     "127.0.0.1:8081",
	}
	rec2 := models.TrafficRecord{
		CreatedAt:   now,
		SessionID:   session.ID,
		ProcessID:   pB.ID,
		Direction:   models.TrafficDirEgress,
		ServiceName: "cros.attr.PaymentService",
		Operation:   "Pay",
		DstAddr:     "127.0.0.1:8082",
	}
	rec3 := models.TrafficRecord{
		CreatedAt:   now,
		SessionID:   session.ID,
		ProcessID:   pB.ID,
		Direction:   models.TrafficDirEgress,
		ServiceName: "cros.attr.PaymentService",
		Operation:   "Pay",
		DstAddr:     "10.10.10.10:9999",
	}
	db.Create(&rec1)
	db.Create(&rec2)
	db.Create(&rec3)

	// 执行离线批量结算
	err := OfflineBatchAttribution(ctx, db, session.ID)
	if err != nil {
		t.Fatalf("OfflineBatchAttribution 失败: %v", err)
	}

	var res1, res2, res3 models.TrafficRecord
	db.First(&res1, rec1.ID)
	db.First(&res2, rec2.ID)
	db.First(&res3, rec3.ID)

	if res1.DstModuleID == nil || *res1.DstModuleID != modB.ID || res1.DstContractID == nil || *res1.DstContractID != contract.ID {
		t.Fatalf("rec1 归集不符合预期: mod=%v, contract=%v", res1.DstModuleID, res1.DstContractID)
	}
	if res2.DstModuleID == nil || *res2.DstModuleID != modC.ID || res2.DstContractID == nil || *res2.DstContractID != contract.ID {
		t.Fatalf("rec2 归集不符合预期: mod=%v, contract=%v", res2.DstModuleID, res2.DstContractID)
	}
	if res3.DstModuleID != nil || res3.DstContractID == nil || *res3.DstContractID != contract.ID {
		t.Fatalf("rec3 归集不符合预期 (应为 nil, contractID): mod=%v, contract=%v", res3.DstModuleID, res3.DstContractID)
	}
}
