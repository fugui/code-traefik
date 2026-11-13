package topology

import (
	"errors"
	"testing"

	"code-traefik/models"
)

func intPtr(v int) *int {
	return &v
}

func TestLayerLevelGuard_PointerReset_DispatchBackcheck_AndCycles409(t *testing.T) {
	db := setupIsolatedTopologyDB(t)

	// 1. layer_level 非法取值 (< 0 或 > 99) 拒绝创建
	if _, err := CreateSubsystem(db, SubsystemInput{
		Code:       "SUB_INVALID_NEG",
		Name:       "非法层负值",
		LayerLevel: intPtr(-1),
	}); !errors.Is(err, ErrInvalidLayerLevel) {
		t.Fatalf("负数 layer_level 应返回 ErrInvalidLayerLevel，实测 %v", err)
	}
	if _, err := CreateSubsystem(db, SubsystemInput{
		Code:       "SUB_INVALID_HIGH",
		Name:       "非法层超上限",
		LayerLevel: intPtr(100),
	}); !errors.Is(err, ErrInvalidLayerLevel) {
		t.Fatalf("超过 99 的 layer_level 应返回 ErrInvalidLayerLevel，实测 %v", err)
	}

	// 2. 创建子系统 subL2 (L2) 与 subL0 (L0 无约束)、功能组与模块
	subL2, err := CreateSubsystem(db, SubsystemInput{
		Code:       "SUB_L2",
		Name:       "二层子系统",
		LayerLevel: intPtr(2),
	})
	if err != nil {
		t.Fatalf("CreateSubsystem L2 failed: %v", err)
	}
	subL0, err := CreateSubsystem(db, SubsystemInput{
		Code: "SUB_L0",
		Name: "豁免子系统",
	})
	if err != nil {
		t.Fatalf("CreateSubsystem L0 failed: %v", err)
	}

	grpL2, err := CreateFunctionalGroup(db, FunctionalGroupInput{
		SubsystemID: subL2.ID,
		Code:        "GRP_L2",
		Name:        "二层组",
	})
	if err != nil {
		t.Fatalf("CreateFunctionalGroup failed: %v", err)
	}
	grpL0, err := CreateFunctionalGroup(db, FunctionalGroupInput{
		SubsystemID: subL0.ID,
		Code:        "GRP_L0",
		Name:        "豁免组",
	})
	if err != nil {
		t.Fatalf("CreateFunctionalGroup failed: %v", err)
	}

	// modInherit 未设置 layer_level (0)，继承自 subL2 -> effective = 2
	modInherit, err := CreateModule(db, ModuleInput{
		GroupID: grpL2.ID,
		Code:    "MOD_INHERIT_L2",
		Name:    "继承L2模块",
	})
	if err != nil {
		t.Fatalf("CreateModule inherit failed: %v", err)
	}
	// modL3 显式设置 layer_level = 3 -> effective = 3
	modL3, err := CreateModule(db, ModuleInput{
		GroupID:    grpL2.ID,
		Code:       "MOD_HIGH_L3",
		Name:       "高层L3模块",
		LayerLevel: intPtr(3),
	})
	if err != nil {
		t.Fatalf("CreateModule L3 failed: %v", err)
	}
	// modL1 显式覆写为 layer_level = 1（低于其所属 subL2！）-> effective = 1
	modL1, err := CreateModule(db, ModuleInput{
		GroupID:    grpL2.ID,
		Code:       "MOD_LOW_L1",
		Name:       "底层L1模块",
		LayerLevel: intPtr(1),
	})
	if err != nil {
		t.Fatalf("CreateModule L1 failed: %v", err)
	}

	// 验证 *int 指针更新语义：nil 保留原值，&0 显式重置为 0
	_, updatedKeep, err := UpdateModule(db, modL3.ID, ModuleInput{
		GroupID:    grpL2.ID,
		Code:       modL3.Code,
		Name:       "高层L3模块-改名",
		LayerLevel: nil, // 不传应保留 3
	})
	if err != nil {
		t.Fatalf("UpdateModule nil layer_level failed: %v", err)
	}
	if updatedKeep.LayerLevel != 3 {
		t.Fatalf("LayerLevel=nil 应保留原值 3，实测 %d", updatedKeep.LayerLevel)
	}

	_, updatedReset, err := UpdateModule(db, modL3.ID, ModuleInput{
		GroupID:    grpL2.ID,
		Code:       modL3.Code,
		Name:       modL3.Name,
		LayerLevel: intPtr(0), // 显式 0 应重置为 0
	})
	if err != nil {
		t.Fatalf("UpdateModule &0 failed: %v", err)
	}
	if updatedReset.LayerLevel != 0 {
		t.Fatalf("LayerLevel=&0 应重置为 0，实测 %d", updatedReset.LayerLevel)
	}
	// 恢复 modL3 为 3 以便后续测试
	if _, _, err := UpdateModule(db, modL3.ID, ModuleInput{
		GroupID:    grpL2.ID,
		Code:       modL3.Code,
		Name:       modL3.Name,
		LayerLevel: intPtr(3),
	}); err != nil {
		t.Fatalf("restore modL3 failed: %v", err)
	}

	// 子系统 *int 指针保留与重置验证
	_, subKeep, err := UpdateSubsystem(db, subL2.ID, SubsystemInput{
		Code: subL2.Code,
		Name: "二层子系统-保持",
	})
	if err != nil || subKeep.LayerLevel != 2 {
		t.Fatalf("UpdateSubsystem nil 应保留 2，实测 err=%v layer=%d", err, subKeep.LayerLevel)
	}

	// 3. DEPEND 分层守卫：modInherit(eff=2) 试图依赖 modL3(eff=3) -> ERR_LAYER_VIOLATION
	svcL3, err := CreateModuleService(db, ModuleServiceInput{
		ModuleID:    modL3.ID,
		ServiceName: "l3.v1.HighService",
	})
	if err != nil {
		t.Fatalf("CreateModuleService L3 failed: %v", err)
	}
	svcL1, err := CreateModuleService(db, ModuleServiceInput{
		ModuleID:    modL1.ID,
		ServiceName: "l1.v1.LowService",
	})
	if err != nil {
		t.Fatalf("CreateModuleService L1 failed: %v", err)
	}

	_, err = CreateModuleDependency(db, ModuleDependencyInput{
		ConsumerModuleID: modInherit.ID,
		ServiceID:        &svcL3.ID,
	})
	var layerErr *LayerViolationError
	if !errors.As(err, &layerErr) || layerErr.Code != ErrCodeLayerViolation {
		t.Fatalf("低层(L2)依赖高层(L3) 应返回 ERR_LAYER_VIOLATION，实测: %v", err)
	}

	// 反向合规依赖：modL3(L3) -> modInherit(L2) 允许；modInherit(L2) -> modL1(L1) 允许
	svcInherit, err := CreateModuleService(db, ModuleServiceInput{
		ModuleID:    modInherit.ID,
		ServiceName: "l2.v1.MidService",
	})
	if err != nil {
		t.Fatalf("CreateModuleService inherit failed: %v", err)
	}
	if _, err := CreateModuleDependency(db, ModuleDependencyInput{
		ConsumerModuleID: modL3.ID,
		ServiceID:        &svcInherit.ID,
	}); err != nil {
		t.Fatalf("合法向下依赖 (L3 -> L2) 不应被拦截: %v", err)
	}
	if _, err := CreateModuleDependency(db, ModuleDependencyInput{
		ConsumerModuleID: modInherit.ID,
		ServiceID:        &svcL1.ID,
	}); err != nil {
		t.Fatalf("合法向下依赖 (L2 -> L1) 不应被拦截: %v", err)
	}

	// 若此时把 subL2 下调为 L0(无约束)，modInherit 仍受其 L1 吗？不：L0 允许；
	// 但若把 modL1 试图上调为 L3，应因既有消费方 modInherit(eff=2) < 3 而返回 ERR_LAYER_VIOLATION 并回滚！
	if _, _, err := UpdateModule(db, modL1.ID, ModuleInput{
		GroupID:    grpL2.ID,
		Code:       modL1.Code,
		Name:       modL1.Name,
		LayerLevel: intPtr(3),
	}); !errors.As(err, &layerErr) || layerErr.Code != ErrCodeLayerViolation {
		t.Fatalf("调高被依赖方层级导致上游不满足 >= 关系应返回 ERR_LAYER_VIOLATION，实测: %v", err)
	}

	// 4. 即使处于 layer_level = 0 豁免层级，内部有向环 (A -> B -> C -> A) 也必须被 ERR_CYCLIC_DEPENDENCY 拦截！
	modA, err := CreateModule(db, ModuleInput{GroupID: grpL0.ID, Code: "MOD_CYC_A", Name: "环A"})
	if err != nil {
		t.Fatalf("create modA failed: %v", err)
	}
	modB, err := CreateModule(db, ModuleInput{GroupID: grpL0.ID, Code: "MOD_CYC_B", Name: "环B"})
	if err != nil {
		t.Fatalf("create modB failed: %v", err)
	}
	modC, err := CreateModule(db, ModuleInput{GroupID: grpL0.ID, Code: "MOD_CYC_C", Name: "环C"})
	if err != nil {
		t.Fatalf("create modC failed: %v", err)
	}

	svcA, _ := CreateModuleService(db, ModuleServiceInput{ModuleID: modA.ID, ServiceName: "cyc.v1.SvcA"})
	svcB, _ := CreateModuleService(db, ModuleServiceInput{ModuleID: modB.ID, ServiceName: "cyc.v1.SvcB"})
	svcC, _ := CreateModuleService(db, ModuleServiceInput{ModuleID: modC.ID, ServiceName: "cyc.v1.SvcC"})

	if _, err := CreateModuleDependency(db, ModuleDependencyInput{ConsumerModuleID: modA.ID, ServiceID: &svcB.ID}); err != nil {
		t.Fatalf("A->B failed: %v", err)
	}
	if _, err := CreateModuleDependency(db, ModuleDependencyInput{ConsumerModuleID: modB.ID, ServiceID: &svcC.ID}); err != nil {
		t.Fatalf("B->C failed: %v", err)
	}
	_, err = CreateModuleDependency(db, ModuleDependencyInput{ConsumerModuleID: modC.ID, ServiceID: &svcA.ID})
	var cycErr *CyclicDependencyError
	if !errors.As(err, &cycErr) || cycErr.Code != ErrCodeCyclicDependency {
		t.Fatalf("L0 豁免态闭环 C->A 仍必须拦截并返回 ERR_CYCLIC_DEPENDENCY，实测: %v", err)
	}
	if len(cycErr.CyclePath) < 4 {
		t.Fatalf("CyclePath 应包含完整闭合路径，实测: %v", cycErr.CyclePath)
	}

	// 外部依赖免受分层和环检测限制
	if _, err := CreateModuleDependency(db, ModuleDependencyInput{
		ConsumerModuleID: modInherit.ID,
		IsExternal:       true,
		ExternalSystem:   "ALIPAY",
		ExternalService:  "alipay.trade.pay",
	}); err != nil {
		t.Fatalf("外部依赖不应受分层和防环限制: %v", err)
	}

	// 5. BuildTopologyGraph 在 logical 和 physical 视图中均输出 layer_level / effective_layer_level
	for _, view := range []string{"LOGICAL", "PHYSICAL"} {
		graph, err := BuildTopologyGraph(db, view, 0)
		if err != nil {
			t.Fatalf("BuildTopologyGraph(%s) failed: %v", view, err)
		}
		foundSub := false
		foundInherit := false
		for _, n := range graph.Nodes {
			if n.Type == NodeTypeSubsystem && n.EntityID == subL2.ID {
				foundSub = true
				if n.Meta["layer_level"] != 2 || n.Meta["effective_layer_level"] != 2 {
					t.Fatalf("%s 视图 SUBSYSTEM meta layer 异常: %+v", view, n.Meta)
				}
			}
			if n.Type == NodeTypeModule && n.EntityID == modInherit.ID {
				foundInherit = true
				if n.Meta["layer_level"] != 0 || n.Meta["effective_layer_level"] != 2 {
					t.Fatalf("%s 视图 MODULE 继承 effective_layer_level 异常: %+v", view, n.Meta)
				}
			}
		}
		if view == "LOGICAL" && !foundSub {
			t.Fatalf("LOGICAL 视图未找到 subL2 节点")
		}
		if !foundInherit {
			t.Fatalf("%s 视图未找到 modInherit 节点", view)
		}
	}
}

func TestRealizationLayerLevelGuard_InversionConflict409(t *testing.T) {
	db := setupIsolatedTopologyDB(t)

	sub, err := CreateSubsystem(db, SubsystemInput{
		Code:       "SUB_REALIZE",
		Name:       "契约实现子系统",
		LayerLevel: intPtr(2),
	})
	if err != nil {
		t.Fatalf("CreateSubsystem failed: %v", err)
	}
	grp, err := CreateFunctionalGroup(db, FunctionalGroupInput{
		SubsystemID: sub.ID,
		Code:        "GRP_REALIZE",
		Name:        "实现组",
	})
	if err != nil {
		t.Fatalf("CreateFunctionalGroup failed: %v", err)
	}

	// CONTRACT_LIB 位于 L3；MOD_LOW_L2 位于 L2；MOD_OK_L3 位于 L3；MOD_CONSUMER_L2 位于 L2
	libMod, err := CreateModule(db, ModuleInput{
		GroupID:    grp.ID,
		Code:       "LIB_L3",
		Name:       "L3契约库",
		ModuleKind: models.ModuleKindContractLib,
		LayerLevel: intPtr(3),
	})
	if err != nil {
		t.Fatalf("create libMod failed: %v", err)
	}
	modLowL2, err := CreateModule(db, ModuleInput{
		GroupID:    grp.ID,
		Code:       "MOD_LOW_L2",
		Name:       "L2低层模块",
		LayerLevel: intPtr(2),
	})
	if err != nil {
		t.Fatalf("create modLowL2 failed: %v", err)
	}
	modMidL2, err := CreateModule(db, ModuleInput{
		GroupID:    grp.ID,
		Code:       "MOD_IMPL_L2",
		Name:       "L2合规实现模块",
		LayerLevel: intPtr(2),
	})
	if err != nil {
		t.Fatalf("create modMidL2 failed: %v", err)
	}
	modHighL3, err := CreateModule(db, ModuleInput{
		GroupID:    grp.ID,
		Code:       "MOD_IMPL_L3",
		Name:       "L3高层实现模块",
		LayerLevel: intPtr(3),
	})
	if err != nil {
		t.Fatalf("create modHighL3 failed: %v", err)
	}
	modConsumerL2, err := CreateModule(db, ModuleInput{
		GroupID:    grp.ID,
		Code:       "MOD_CONS_L2",
		Name:       "L2分发消费模块",
		LayerLevel: intPtr(2),
	})
	if err != nil {
		t.Fatalf("create modConsumerL2 failed: %v", err)
	}

	// 场景 A：L3 契约库被 L2 模块 REALIZATION -> 409 ERR_REALIZATION_LAYER_INVERSION
	contractL3, err := CreateServiceContract(db, ServiceContractInput{
		ServiceName:     "spi.v1.PluginService",
		DefinerModuleID: libMod.ID,
		ImplKeyLabel:    "vendor",
	})
	if err != nil {
		t.Fatalf("create contractL3 failed: %v", err)
	}

	_, err = CreateModuleService(db, ModuleServiceInput{
		ModuleID:   modLowL2.ID,
		ContractID: &contractL3.ID,
		ImplKey:    "low_impl",
	})
	var layerErr *LayerViolationError
	if !errors.As(err, &layerErr) || layerErr.Code != ErrCodeRealizationLayerInversion {
		t.Fatalf("L2 模块实现 L3 契约库应返回 ERR_REALIZATION_LAYER_INVERSION，实测: %v", err)
	}
	// 确认回滚干净：modLowL2 没有残留服务
	var residualCount int64
	db.Model(&models.ModuleService{}).Where("module_id = ?", modLowL2.ID).Count(&residualCount)
	if residualCount != 0 {
		t.Fatalf("REALIZATION 被拦截后应原子回滚，实测残留 %d 条记录", residualCount)
	}

	// 场景 B：反向 DISPATCH 校核（先绑定 L2 契约库 + L2 实现 + L2 消费方，再尝试插入 L3 第二实现）
	libL2, err := CreateModule(db, ModuleInput{
		GroupID:    grp.ID,
		Code:       "LIB_L2",
		Name:       "L2契约库",
		ModuleKind: models.ModuleKindContractLib,
		LayerLevel: intPtr(2),
	})
	if err != nil {
		t.Fatalf("create libL2 failed: %v", err)
	}
	contractL2, err := CreateServiceContract(db, ServiceContractInput{
		ServiceName:     "pay.v1.ChannelService",
		DefinerModuleID: libL2.ID,
		ImplKeyLabel:    "channel",
	})
	if err != nil {
		t.Fatalf("create contractL2 failed: %v", err)
	}
	if _, err := CreateModuleService(db, ModuleServiceInput{
		ModuleID:   modMidL2.ID,
		ContractID: &contractL2.ID,
		ImplKey:    "alipay",
	}); err != nil {
		t.Fatalf("L2 实现绑定 L2 契约应成功: %v", err)
	}
	if _, err := CreateModuleDependency(db, ModuleDependencyInput{
		ConsumerModuleID: modConsumerL2.ID,
		ContractID:       &contractL2.ID,
	}); err != nil {
		t.Fatalf("L2 消费方 DISPATCH L2 契约应成功: %v", err)
	}

	// 尝试新绑定 L3 实现模块 modHighL3 到 contractL2 -> 虽然满足 impl(3) >= lib(2)，但既有消费方 modConsumerL2(2) < 3 -> 409 ERR_LAYER_VIOLATION！
	_, err = CreateModuleService(db, ModuleServiceInput{
		ModuleID:   modHighL3.ID,
		ContractID: &contractL2.ID,
		ImplKey:    "wechat_high",
	})
	if !errors.As(err, &layerErr) || layerErr.Code != ErrCodeLayerViolation {
		t.Fatalf("新实现 L3 违反既有 DISPATCH 消费方 L2 约束应返回 ERR_LAYER_VIOLATION，实测: %v", err)
	}

	// 确认回滚干净
	db.Model(&models.ModuleService{}).Where("module_id = ? AND contract_id = ?", modHighL3.ID, contractL2.ID).Count(&residualCount)
	if residualCount != 0 {
		t.Fatalf("DISPATCH 反向校验失败后应回滚，实测残留 %d 条记录", residualCount)
	}

	// 场景 C：独占服务归入已有契约 (Attach) 也受相同 REALIZATION / DISPATCH 守卫
	exclHigh, err := CreateModuleService(db, ModuleServiceInput{
		ModuleID:    modHighL3.ID,
		ServiceName: "pay.v1.LegacyChannelService",
	})
	if err != nil {
		t.Fatalf("create exclHigh failed: %v", err)
	}
	if _, _, _, err := AttachExclusiveToMultiImplContract(db, AttachExclusiveToContractInput{
		ContractID: contractL2.ID,
		ServiceID:  exclHigh.ID,
		ImplKey:    "legacy_high",
	}); !errors.As(err, &layerErr) || layerErr.Code != ErrCodeLayerViolation {
		t.Fatalf("AttachExclusiveToMultiImplContract L3 实现触发 DISPATCH 消费方 L2 越权应返回 ERR_LAYER_VIOLATION，实测: %v", err)
	}
}
