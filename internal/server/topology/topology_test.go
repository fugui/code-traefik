package topology

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"code-common/backend/testdb"
	"code-traefik/config"
	"code-traefik/models"
	"gorm.io/gorm"
)

func setupIsolatedTopologyDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}
	return db
}

func TestArchHierarchyCRUDAndMoveValidation(t *testing.T) {
	db := setupIsolatedTopologyDB(t)

	// 1. 创建子系统、3 级功能组树与功能模块
	sub1, err := CreateSubsystem(db, SubsystemInput{
		Code:        "trade_sys",
		Name:        "交易核心子系统",
		Description: "处理订单与结算",
	})
	if err != nil {
		t.Fatalf("创建子系统 sub1 失败: %v", err)
	}
	if sub1.Code != "TRADE_SYS" {
		t.Fatalf("期望子系统编码自动转大写 TRADE_SYS，实际为 %q", sub1.Code)
	}

	g1, err := CreateFunctionalGroup(db, FunctionalGroupInput{
		SubsystemID: sub1.ID,
		Code:        "G1",
		Name:        "一级订单组",
	})
	if err != nil {
		t.Fatalf("创建 G1 失败: %v", err)
	}
	expectedG1Path := fmt.Sprintf("/%d/", g1.ID)
	if g1.Level != 1 || g1.Path != expectedG1Path {
		t.Fatalf("G1 初始 level/path 错误: level=%d path=%q, 期望 level=1 path=%q", g1.Level, g1.Path, expectedG1Path)
	}

	g2, err := CreateFunctionalGroup(db, FunctionalGroupInput{
		SubsystemID: sub1.ID,
		ParentID:    &g1.ID,
		Code:        "G2",
		Name:        "二级支付组",
	})
	if err != nil {
		t.Fatalf("创建 G2 失败: %v", err)
	}
	expectedG2Path := fmt.Sprintf("/%d/%d/", g1.ID, g2.ID)
	if g2.Level != 2 || g2.Path != expectedG2Path {
		t.Fatalf("G2 初始 level/path 错误: level=%d path=%q, 期望 level=2 path=%q", g2.Level, g2.Path, expectedG2Path)
	}

	g3, err := CreateFunctionalGroup(db, FunctionalGroupInput{
		SubsystemID: sub1.ID,
		ParentID:    &g2.ID,
		Code:        "G3",
		Name:        "三级清分功能组",
	})
	if err != nil {
		t.Fatalf("创建 G3 失败: %v", err)
	}
	expectedG3Path := fmt.Sprintf("/%d/%d/%d/", g1.ID, g2.ID, g3.ID)
	if g3.Level != 3 || g3.Path != expectedG3Path {
		t.Fatalf("G3 初始 level/path 错误: level=%d path=%q, 期望 level=3 path=%q", g3.Level, g3.Path, expectedG3Path)
	}

	mod, err := CreateModule(db, ModuleInput{
		GroupID: g3.ID,
		Code:    "mod_settle",
		Name:    "清分结算模块",
	})
	if err != nil {
		t.Fatalf("创建模块失败: %v", err)
	}
	if mod.SubsystemID != sub1.ID || mod.Code != "MOD_SETTLE" {
		t.Fatalf("模块冗余子系统或编码不匹配: subsystem_id=%d code=%q", mod.SubsystemID, mod.Code)
	}

	// 2. 将功能组 G1 移动到自身或其子孙 G3 下 -> 成环拦截
	if err := MoveFunctionalGroup(db, g1.ID, &g1.ID); !errors.Is(err, ErrCycleMoveToSelf) {
		t.Fatalf("移动到自身期望返回 ErrCycleMoveToSelf，实际为: %v", err)
	}
	if err := MoveFunctionalGroup(db, g1.ID, &g3.ID); !errors.Is(err, ErrCycleMoveToDescendant) {
		t.Fatalf("移动 G1 到其子孙 G3 下期望返回 ErrCycleMoveToDescendant，实际为: %v", err)
	}

	// 3. 将 G1 移动到跨子系统功能组下 -> ErrCrossSubsystemMove
	sub2, err := CreateSubsystem(db, SubsystemInput{
		Code: "RISK_SYS",
		Name: "风控子系统",
	})
	if err != nil {
		t.Fatalf("创建 sub2 失败: %v", err)
	}
	otherGroup, err := CreateFunctionalGroup(db, FunctionalGroupInput{
		SubsystemID: sub2.ID,
		Code:        "RISK_G1",
		Name:        "风控一级组",
	})
	if err != nil {
		t.Fatalf("创建跨子系统功能组失败: %v", err)
	}
	if err := MoveFunctionalGroup(db, g1.ID, &otherGroup.ID); !errors.Is(err, ErrCrossSubsystemMove) {
		t.Fatalf("跨子系统移动期望返回 ErrCrossSubsystemMove，实际为: %v", err)
	}

	// 4. 构造 8 级树后尝试将含 2 级子树的功能组移至第 7 级节点下 -> ErrExceedMaxTreeDepth
	chain := make([]*models.FunctionalGroup, 8)
	var prevID *uint64
	for lvl := 1; lvl <= 8; lvl++ {
		node, createErr := CreateFunctionalGroup(db, FunctionalGroupInput{
			SubsystemID: sub1.ID,
			ParentID:    prevID,
			Code:        fmt.Sprintf("DEPTH_L%d", lvl),
			Name:        fmt.Sprintf("深度节点L%d", lvl),
		})
		if createErr != nil {
			t.Fatalf("创建第 %d 级节点失败: %v", lvl, createErr)
		}
		chain[lvl-1] = node
		prevID = &node.ID
	}
	// 直接在第 8 级节点下新建第 9 级节点应被拦截
	if _, err := CreateFunctionalGroup(db, FunctionalGroupInput{
		SubsystemID: sub1.ID,
		ParentID:    &chain[7].ID,
		Code:        "DEPTH_L9",
		Name:        "超限节点L9",
	}); !errors.Is(err, ErrExceedMaxTreeDepth) {
		t.Fatalf("创建第 9 级功能组期望返回 ErrExceedMaxTreeDepth，实际为: %v", err)
	}
	// G2 含子节点 G3（共 2 级子树），移动到第 7 级节点 chain[6] 下将产生第 8、9 级 -> 必须拦截
	if err := MoveFunctionalGroup(db, g2.ID, &chain[6].ID); !errors.Is(err, ErrExceedMaxTreeDepth) {
		t.Fatalf("将 2 级子树移至第 7 级节点下期望返回 ErrExceedMaxTreeDepth，实际为: %v", err)
	}

	// 5. 合法移动 G2 至新父节点 (chain[1], level=2)，期望 G2 变为 level=3，G3 级联更新为 level=4
	targetParent := chain[1] // level = 2
	_, afterG2, updatedDesc, err := MoveFunctionalGroupWithResult(db, g2.ID, &targetParent.ID, nil, 1)
	if err != nil {
		t.Fatalf("合法移动 G2 至新父节点失败: %v", err)
	}
	if updatedDesc != 1 {
		t.Fatalf("期望级联更新子孙数量为 1，实际为 %d", updatedDesc)
	}
	wantG2Path := fmt.Sprintf("%s%d/", targetParent.Path, g2.ID)
	if afterG2.Level != 3 || afterG2.Path != wantG2Path {
		t.Fatalf("移动后 G2 level/path 不匹配: level=%d path=%q, 期望 level=3 path=%q", afterG2.Level, afterG2.Path, wantG2Path)
	}

	var refreshedG3 models.FunctionalGroup
	if err := db.First(&refreshedG3, g3.ID).Error; err != nil {
		t.Fatalf("查询移动后的 G3 失败: %v", err)
	}
	wantG3Path := fmt.Sprintf("%s%d/", wantG2Path, g3.ID)
	if refreshedG3.Level != 4 || refreshedG3.Path != wantG3Path {
		t.Fatalf("移动后子孙 G3 level/path 未正确级联更新: level=%d path=%q, 期望 level=4 path=%q",
			refreshedG3.Level, refreshedG3.Path, wantG3Path)
	}

	// 验证树查询接口结构完整
	tree, err := GetFunctionalGroupTree(db, sub1.ID)
	if err != nil {
		t.Fatalf("GetFunctionalGroupTree 失败: %v", err)
	}
	if len(tree) != 2 {
		t.Fatalf("期望 sub1 下有 2 棵根功能组树 (G1 与 DEPTH_L1)，实际为 %d", len(tree))
	}
}

func TestDeleteNonEmptyArchElementsConflict(t *testing.T) {
	db := setupIsolatedTopologyDB(t)

	sub, err := CreateSubsystem(db, SubsystemInput{Code: "DEL_SUB", Name: "删除守卫测试子系统"})
	if err != nil {
		t.Fatalf("CreateSubsystem 失败: %v", err)
	}
	parentGrp, err := CreateFunctionalGroup(db, FunctionalGroupInput{
		SubsystemID: sub.ID,
		Code:        "PARENT_GRP",
		Name:        "父功能组",
	})
	if err != nil {
		t.Fatalf("CreateFunctionalGroup parent 失败: %v", err)
	}
	childGrp, err := CreateFunctionalGroup(db, FunctionalGroupInput{
		SubsystemID: sub.ID,
		ParentID:    &parentGrp.ID,
		Code:        "CHILD_GRP",
		Name:        "子功能组",
	})
	if err != nil {
		t.Fatalf("CreateFunctionalGroup child 失败: %v", err)
	}
	mod, err := CreateModule(db, ModuleInput{
		GroupID: childGrp.ID,
		Code:    "DEL_MOD",
		Name:    "删除守卫测试模块",
	})
	if err != nil {
		t.Fatalf("CreateModule 失败: %v", err)
	}

	// 模块声明供给服务 + 挂载进程
	svc, err := CreateModuleService(db, ModuleServiceInput{
		ModuleID:    mod.ID,
		ServiceName: "dophi.del.v1-DelService",
	})
	if err != nil {
		t.Fatalf("CreateModuleService 失败: %v", err)
	}

	agent := models.Agent{
		Name:     "del-test-agent",
		Endpoint: "192.168.56.101:9091",
		Scope:    models.ScopeShared,
		Status:   models.AgentStatusOnline,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("创建测试 Agent 失败: %v", err)
	}
	proc, err := CreateProcess(db, ProcessInput{
		AgentID:    agent.ID,
		Name:       "del-proc",
		ListenPort: 19001,
		CmdPattern: "del-proc-bin",
		ModuleIDs:  []uint64{mod.ID},
	})
	if err != nil {
		t.Fatalf("CreateProcess 失败: %v", err)
	}

	// 1. 删除非空子系统 -> 409
	if _, err := DeleteSubsystem(db, sub.ID); !errors.Is(err, ErrSubsystemNotEmpty) {
		t.Fatalf("删除含功能组的子系统期望 ErrSubsystemNotEmpty，实际为: %v", err)
	}
	// 2. 删除含子功能组的父组 -> 409
	if _, err := DeleteFunctionalGroup(db, parentGrp.ID); !errors.Is(err, ErrGroupNotEmpty) {
		t.Fatalf("删除含子组的功能组期望 ErrGroupNotEmpty，实际为: %v", err)
	}
	// 3. 删除含模块的子组 -> 409
	if _, err := DeleteFunctionalGroup(db, childGrp.ID); !errors.Is(err, ErrGroupNotEmpty) {
		t.Fatalf("删除含模块的功能组期望 ErrGroupNotEmpty，实际为: %v", err)
	}
	// 4. 删除已声明接口且关联进程的模块 -> 409
	if _, err := DeleteModule(db, mod.ID); !errors.Is(err, ErrModuleNotEmpty) {
		t.Fatalf("删除含服务与进程关联的模块期望 ErrModuleNotEmpty，实际为: %v", err)
	}
	// 仅删除服务但仍关联进程 -> 依然 409
	if _, err := DeleteModuleService(db, svc.ID); err != nil {
		t.Fatalf("DeleteModuleService 失败: %v", err)
	}
	if _, err := DeleteModule(db, mod.ID); !errors.Is(err, ErrModuleNotEmpty) {
		t.Fatalf("删除仍被进程挂载的模块期望 ErrModuleNotEmpty，实际为: %v", err)
	}

	// 5. 解除进程关联并自底向上逐级删除 -> 全部成功
	if _, err := DeleteProcess(db, proc.ID); err != nil {
		t.Fatalf("DeleteProcess 失败: %v", err)
	}
	if _, err := DeleteModule(db, mod.ID); err != nil {
		t.Fatalf("解除全部关联后删除模块应成功，实际报错: %v", err)
	}
	if _, err := DeleteFunctionalGroup(db, childGrp.ID); err != nil {
		t.Fatalf("清空模块后删除子功能组应成功，实际报错: %v", err)
	}
	if _, err := DeleteFunctionalGroup(db, parentGrp.ID); err != nil {
		t.Fatalf("清空子组后删除父功能组应成功，实际报错: %v", err)
	}
	if _, err := DeleteSubsystem(db, sub.ID); err != nil {
		t.Fatalf("清空全部子项后删除子系统应成功，实际报错: %v", err)
	}
}

func TestModuleServiceProvideGlobalUnique(t *testing.T) {
	db := setupIsolatedTopologyDB(t)

	sub, err := CreateSubsystem(db, SubsystemInput{Code: "ORDER_SYS", Name: "订单子系统"})
	if err != nil {
		t.Fatalf("CreateSubsystem 失败: %v", err)
	}
	grp, err := CreateFunctionalGroup(db, FunctionalGroupInput{SubsystemID: sub.ID, Code: "ORD_GRP", Name: "订单组"})
	if err != nil {
		t.Fatalf("CreateFunctionalGroup 失败: %v", err)
	}

	modA, err := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_A", Name: "订单交易模块A"})
	if err != nil {
		t.Fatalf("CreateModule A 失败: %v", err)
	}
	modB, err := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_B", Name: "网关聚合模块B"})
	if err != nil {
		t.Fatalf("CreateModule B 失败: %v", err)
	}
	modC, err := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_C", Name: "结算对账模块C"})
	if err != nil {
		t.Fatalf("CreateModule C 失败: %v", err)
	}

	// 校验非法服务名（缺少中划线连接 package 与 service）拦截为 400
	if _, err := CreateModuleService(db, ModuleServiceInput{
		ModuleID:    modA.ID,
		ServiceName: "dophi.order.v1.TradeService",
	}); !errors.Is(err, ErrInvalidServiceNameFormat) {
		t.Fatalf("缺少中划线的服务名期望返回 ErrInvalidServiceNameFormat，实际为: %v", err)
	}

	// 1. 模块 A 声明 PROVIDE dophi.order.v1-TradeService
	svcA, err := CreateModuleService(db, ModuleServiceInput{
		ModuleID:    modA.ID,
		ServiceName: "dophi.order.v1-TradeService",
	})
	if err != nil {
		t.Fatalf("模块 A 声明 PROVIDE 失败: %v", err)
	}

	// 2. 模块 B 与模块 C 声明依赖模块 A 的服务 -> 均成功
	if _, err := CreateModuleDependency(db, ModuleDependencyInput{
		ConsumerModuleID: modB.ID,
		ServiceID:        &svcA.ID,
	}); err != nil {
		t.Fatalf("模块 B 声明依赖模块 A 失败: %v", err)
	}
	if _, err := CreateModuleDependency(db, ModuleDependencyInput{
		ConsumerModuleID: modC.ID,
		ServiceID:        &svcA.ID,
	}); err != nil {
		t.Fatalf("模块 C 声明依赖模块 A 失败: %v", err)
	}

	// 3. 模块 B 尝试声明相同 PROVIDE -> 409 Conflict 并指出已被模块 A 占用
	_, err = CreateModuleService(db, ModuleServiceInput{
		ModuleID:    modB.ID,
		ServiceName: "dophi.order.v1-TradeService",
	})
	if !errors.Is(err, ErrDuplicateModuleService) {
		t.Fatalf("重复 PROVIDE 期望返回 ErrDuplicateModuleService，实际为: %v", err)
	}
	var conflictErr *ProvideConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("期望错误类型为 *ProvideConflictError，实际为: %T (%v)", err, err)
	}
	if conflictErr.HolderModuleID != modA.ID || conflictErr.HolderModuleCode != "MOD_A" || !strings.Contains(conflictErr.Error(), "MOD_A") {
		t.Fatalf("冲突详情未准确指出占用方模块 A: %+v", conflictErr)
	}
}

func TestModuleDependenciesAndExternalSystem(t *testing.T) {
	db := setupIsolatedTopologyDB(t)

	sub, err := CreateSubsystem(db, SubsystemInput{Code: "EXT_SYS", Name: "外部测试子系统"})
	if err != nil {
		t.Fatalf("CreateSubsystem 失败: %v", err)
	}
	grp, err := CreateFunctionalGroup(db, FunctionalGroupInput{SubsystemID: sub.ID, Code: "EXT_GRP", Name: "功能组"})
	if err != nil {
		t.Fatalf("CreateFunctionalGroup 失败: %v", err)
	}
	mod1, err := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_1", Name: "模块1"})
	if err != nil {
		t.Fatalf("CreateModule 1 失败: %v", err)
	}
	mod2, err := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_2", Name: "模块2"})
	if err != nil {
		t.Fatalf("CreateModule 2 失败: %v", err)
	}

	svc1, err := CreateModuleService(db, ModuleServiceInput{
		ModuleID:    mod1.ID,
		ServiceName: "dophi.pay.v1-PayService",
	})
	if err != nil {
		t.Fatalf("CreateModuleService 1 失败: %v", err)
	}

	// 1. 自依赖校验拦截 (模块不能依赖自身)
	if _, err := CreateModuleDependency(db, ModuleDependencyInput{
		ConsumerModuleID: mod1.ID,
		ServiceID:        &svc1.ID,
	}); !errors.Is(err, ErrSelfDependency) {
		t.Fatalf("自循环依赖期望返回 ErrSelfDependency，实际为: %v", err)
	}

	// 2. 正常内部依赖
	dep, err := CreateModuleDependency(db, ModuleDependencyInput{
		ConsumerModuleID: mod2.ID,
		ServiceID:        &svc1.ID,
		Description:      "调用结算服务",
	})
	if err != nil {
		t.Fatalf("内部依赖创建失败: %v", err)
	}

	// 3. 重复依赖拦截
	if _, err := CreateModuleDependency(db, ModuleDependencyInput{
		ConsumerModuleID: mod2.ID,
		ServiceID:        &svc1.ID,
	}); !errors.Is(err, ErrDuplicateDependency) {
		t.Fatalf("重复依赖期望返回 ErrDuplicateDependency，实际为: %v", err)
	}

	// 4. 删除被依赖的 Service 应当被拦截 (ErrServiceHasDependencies)
	if _, err := DeleteModuleService(db, svc1.ID); !errors.Is(err, ErrServiceHasDependencies) {
		t.Fatalf("删除正被依赖的 Service 期望返回 ErrServiceHasDependencies，实际为: %v", err)
	}

	// 5. 解除依赖后再删除 Service -> 成功
	if _, err := DeleteModuleDependency(db, dep.ID); err != nil {
		t.Fatalf("DeleteModuleDependency 失败: %v", err)
	}
	if _, err := DeleteModuleService(db, svc1.ID); err != nil {
		t.Fatalf("解除依赖后删除 Service 失败: %v", err)
	}

	// 6. 外部第三方系统依赖支持
	extDep, err := CreateModuleDependency(db, ModuleDependencyInput{
		ConsumerModuleID: mod1.ID,
		IsExternal:       true,
		ExternalSystem:   "WeChatPay",
		ExternalService:  "UnifiedOrderService",
		Description:      "微信统一下单",
	})
	if err != nil {
		t.Fatalf("外部服务依赖创建失败: %v", err)
	}
	if !extDep.IsExternal || extDep.ExternalSystem != "WeChatPay" {
		t.Fatalf("外部依赖入库属性不符: %+v", extDep)
	}
}

func TestProcessModuleBindingPublicVsPrivate(t *testing.T) {
	db := setupIsolatedTopologyDB(t)

	// 创建普通测试用户
	alice := models.User{
		Username: "alice",
		Email:    "alice@example.com",
		Name:     "Alice",
		Password: "hashed-password",
		IsActive: true,
	}
	if err := db.Create(&alice).Error; err != nil {
		t.Fatalf("创建用户 Alice 失败: %v", err)
	}

	sub, err := CreateSubsystem(db, SubsystemInput{Code: "PAY_SYS", Name: "支付子系统"})
	if err != nil {
		t.Fatalf("CreateSubsystem 失败: %v", err)
	}
	grp, err := CreateFunctionalGroup(db, FunctionalGroupInput{SubsystemID: sub.ID, Code: "PAY_GRP", Name: "支付组"})
	if err != nil {
		t.Fatalf("CreateFunctionalGroup 失败: %v", err)
	}
	mod1, err := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_1", Name: "核心支付模块1"})
	if err != nil {
		t.Fatalf("CreateModule 1 失败: %v", err)
	}
	mod2, err := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_2", Name: "辅助风控模块2"})
	if err != nil {
		t.Fatalf("CreateModule 2 失败: %v", err)
	}

	sharedAgent1 := models.Agent{Name: "shared-agent-1", Endpoint: "192.168.56.21:9091", Scope: models.ScopeShared, Status: models.AgentStatusOnline}
	sharedAgent2 := models.Agent{Name: "shared-agent-2", Endpoint: "192.168.56.22:9091", Scope: models.ScopeShared, Status: models.AgentStatusOnline}
	privAgent := models.Agent{Name: "alice-priv-agent", Endpoint: "192.168.56.99:9091", Scope: models.ScopePrivate, OwnerID: &alice.ID, Status: models.AgentStatusOnline}
	if err := db.Create(&sharedAgent1).Error; err != nil {
		t.Fatalf("创建 sharedAgent1 失败: %v", err)
	}
	if err := db.Create(&sharedAgent2).Error; err != nil {
		t.Fatalf("创建 sharedAgent2 失败: %v", err)
	}
	if err := db.Create(&privAgent).Error; err != nil {
		t.Fatalf("创建 privAgent 失败: %v", err)
	}

	// 1. 在 SHARED Agent 下创建 Proc_A 绑定 Mod_1
	procA, err := CreateProcess(db, ProcessInput{
		AgentID:    sharedAgent1.ID,
		Name:       "Proc_A",
		ListenPort: 9101,
		CmdPattern: "./bin/proc_a",
		ModuleIDs:  []uint64{mod1.ID},
	})
	if err != nil {
		t.Fatalf("创建 SHARED Proc_A 失败: %v", err)
	}

	// 同一 Agent 下再建同端口进程 -> 409
	if _, err := CreateProcess(db, ProcessInput{
		AgentID:    sharedAgent1.ID,
		Name:       "Proc_DupPort",
		ListenPort: 9101,
		CmdPattern: "./bin/proc_dup",
		ModuleIDs:  []uint64{mod2.ID},
	}); !errors.Is(err, ErrDuplicateProcessPort) {
		t.Fatalf("同 Agent 端口重复期望返回 ErrDuplicateProcessPort，实际为: %v", err)
	}

	// 另一 SHARED Agent 下的进程重复绑定 Mod_1 -> 409
	if _, err := CreateProcess(db, ProcessInput{
		AgentID:    sharedAgent2.ID,
		Name:       "Proc_SharedDupMod",
		ListenPort: 9102,
		CmdPattern: "./bin/proc_shared_dup",
		ModuleIDs:  []uint64{mod1.ID},
	}); !errors.Is(err, ErrDuplicateModuleBinding) {
		t.Fatalf("SHARED 域重复挂载同一模块期望返回 ErrDuplicateModuleBinding，实际为: %v", err)
	}

	// 检查全局 Mod_1.ProcessID == Proc_A.ID
	var loadedMod1 models.Module
	if err := db.First(&loadedMod1, mod1.ID).Error; err != nil {
		t.Fatalf("查询 Mod_1 失败: %v", err)
	}
	if loadedMod1.ProcessID == nil || *loadedMod1.ProcessID != procA.ID {
		t.Fatalf("期望 Mod_1.ProcessID == %d，实际为 %v", procA.ID, loadedMod1.ProcessID)
	}

	// 2. 普通用户 Alice 在本人 PRIVATE Agent 下创建 PrivProc_A 同样挂载 Mod_1 -> 成功且不污染全局基线
	privProcA, err := CreateProcess(db, ProcessInput{
		AgentID:    privAgent.ID,
		Name:       "PrivProc_A",
		ListenPort: 19101,
		CmdPattern: "./bin/local_proc_a",
		ModuleIDs:  []uint64{mod1.ID},
		OperatorID: alice.ID,
	})
	if err != nil {
		t.Fatalf("创建 PRIVATE PrivProc_A 失败: %v", err)
	}

	var pmCount int64
	if err := db.Model(&models.ProcessModule{}).Where("process_id = ? AND module_id = ?", privProcA.ID, mod1.ID).Count(&pmCount).Error; err != nil {
		t.Fatalf("查询 traefik_process_modules 失败: %v", err)
	}
	if pmCount != 1 {
		t.Fatalf("期望 traefik_process_modules 写入 1 条关联记录，实际为 %d", pmCount)
	}

	var loadedMod1After models.Module
	if err := db.First(&loadedMod1After, mod1.ID).Error; err != nil {
		t.Fatalf("重新查询 Mod_1 失败: %v", err)
	}
	if loadedMod1After.ProcessID == nil || *loadedMod1After.ProcessID != procA.ID {
		t.Fatalf("全局 Mod_1.ProcessID 被私有进程污染: 期望保持 %d，实际变为 %v", procA.ID, loadedMod1After.ProcessID)
	}

	// 同一 PRIVATE Agent 内重复挂载 Mod_1 到第二个私有进程 -> 409
	if _, err := CreateProcess(db, ProcessInput{
		AgentID:    privAgent.ID,
		Name:       "PrivProc_B",
		ListenPort: 19102,
		CmdPattern: "./bin/local_proc_b",
		ModuleIDs:  []uint64{mod1.ID},
		OperatorID: alice.ID,
	}); !errors.Is(err, ErrDuplicateModuleBinding) {
		t.Fatalf("同台 PRIVATE Agent 内重复挂载同一模块期望返回 ErrDuplicateModuleBinding，实际为: %v", err)
	}
}

func TestBuildStaticTopologyLogicalAndPhysical(t *testing.T) {
	db := setupIsolatedTopologyDB(t)

	bob := models.User{
		Username: "bob",
		Email:    "bob@example.com",
		Name:     "Bob",
		Password: "hashed-password",
		IsActive: true,
	}
	if err := db.Create(&bob).Error; err != nil {
		t.Fatalf("创建用户 Bob 失败: %v", err)
	}

	sub, err := CreateSubsystem(db, SubsystemInput{Code: "TOPO_SYS", Name: "拓扑测试子系统"})
	if err != nil {
		t.Fatalf("CreateSubsystem 失败: %v", err)
	}
	grp, err := CreateFunctionalGroup(db, FunctionalGroupInput{SubsystemID: sub.ID, Code: "TOPO_GRP", Name: "拓扑组"})
	if err != nil {
		t.Fatalf("CreateFunctionalGroup 失败: %v", err)
	}

	mod1, err := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_1", Name: "入口模块1"})
	if err != nil {
		t.Fatalf("CreateModule 1 失败: %v", err)
	}
	mod2, err := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_2", Name: "同进程风控模块2"})
	if err != nil {
		t.Fatalf("CreateModule 2 失败: %v", err)
	}
	mod3, err := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_3", Name: "跨进程库存模块3"})
	if err != nil {
		t.Fatalf("CreateModule 3 失败: %v", err)
	}

	// 接口契约：
	// Mod_2 PROVIDE dophi.risk-RiskSvc
	// Mod_3 PROVIDE dophi.stock-StockSvc
	// Mod_1 DEPEND Mod_2 (同进程 INTRA_PROCESS) 与 Mod_3 (跨进程 CROSS_PROCESS)
	// Mod_3 DEPEND 外部三方服务 (EXTERNAL_UNREGISTERED)
	svc2, err := CreateModuleService(db, ModuleServiceInput{
		ModuleID: mod2.ID, ServiceName: "dophi.risk-RiskSvc",
	})
	if err != nil {
		t.Fatalf("Mod_2 PROVIDE 失败: %v", err)
	}
	svc3, err := CreateModuleService(db, ModuleServiceInput{
		ModuleID: mod3.ID, ServiceName: "dophi.stock-StockSvc",
	})
	if err != nil {
		t.Fatalf("Mod_3 PROVIDE 失败: %v", err)
	}
	if _, err := CreateModuleDependency(db, ModuleDependencyInput{
		ConsumerModuleID: mod1.ID, ServiceID: &svc2.ID,
	}); err != nil {
		t.Fatalf("Mod_1 DEPEND Mod_2 失败: %v", err)
	}
	if _, err := CreateModuleDependency(db, ModuleDependencyInput{
		ConsumerModuleID: mod1.ID, ServiceID: &svc3.ID,
	}); err != nil {
		t.Fatalf("Mod_1 DEPEND Mod_3 失败: %v", err)
	}
	if _, err := CreateModuleDependency(db, ModuleDependencyInput{
		ConsumerModuleID: mod3.ID, IsExternal: true,
		ExternalSystem: "BankGateway", ExternalService: "ext.bank-UnknownSvc",
	}); err != nil {
		t.Fatalf("Mod_3 DEPEND UnknownSvc 失败: %v", err)
	}

	// 部署映射：SHARED Agent 上 Proc_1 承载 [Mod_1, Mod_2]，Proc_2 承载 [Mod_3]
	sharedAgent := models.Agent{Name: "topo-shared-agent", Endpoint: "192.168.56.31:9091", Scope: models.ScopeShared, Status: models.AgentStatusOnline}
	privAgent := models.Agent{Name: "topo-bob-priv-agent", Endpoint: "192.168.56.88:9091", Scope: models.ScopePrivate, OwnerID: &bob.ID, Status: models.AgentStatusOnline}
	if err := db.Create(&sharedAgent).Error; err != nil {
		t.Fatalf("创建 sharedAgent 失败: %v", err)
	}
	if err := db.Create(&privAgent).Error; err != nil {
		t.Fatalf("创建 privAgent 失败: %v", err)
	}

	proc1, err := CreateProcess(db, ProcessInput{
		AgentID:          sharedAgent.ID,
		Name:             "SharedProc_1",
		ListenPort:       9201,
		CmdPattern:       "./bin/proc1",
		ModuleIDs:        []uint64{mod1.ID, mod2.ID},
		ExtraDownstreams: []string{"10.10.0.5:8800"},
	})
	if err != nil {
		t.Fatalf("创建 SharedProc_1 失败: %v", err)
	}
	proc2, err := CreateProcess(db, ProcessInput{
		AgentID:    sharedAgent.ID,
		Name:       "SharedProc_2",
		ListenPort: 9202,
		CmdPattern: "./bin/proc2",
		ModuleIDs:  []uint64{mod3.ID},
	})
	if err != nil {
		t.Fatalf("创建 SharedProc_2 失败: %v", err)
	}

	// 在 Bob 私有 Agent 下创建私有进程，验证不会混入公共基线拓扑
	privProc, err := CreateProcess(db, ProcessInput{
		AgentID:    privAgent.ID,
		Name:       "BobPrivProc",
		ListenPort: 19201,
		CmdPattern: "./bin/bob_proc",
		ModuleIDs:  []uint64{mod1.ID, mod3.ID}, // 即使私有进程把 Mod_1 与 Mod_3 放在同进程，公共拓扑仍必须是 CROSS_PROCESS！
	})
	if err != nil {
		t.Fatalf("创建 BobPrivProc 失败: %v", err)
	}

	// 1. 校验非法视角参数
	if _, err := BuildStaticTopology(db, "INVALID_VIEW", nil); !errors.Is(err, ErrInvalidTopologyView) {
		t.Fatalf("非法视角参数期望返回 ErrInvalidTopologyView，实际为: %v", err)
	}

	// 2. 校验 LOGICAL 逻辑拓扑
	logical, err := BuildStaticTopology(db, TopologyViewLogical, &sub.ID)
	if err != nil {
		t.Fatalf("BuildStaticTopology LOGICAL 失败: %v", err)
	}
	if len(logical.Edges) != 3 {
		t.Fatalf("LOGICAL 拓扑期望 3 条边，实际为 %d: %+v", len(logical.Edges), logical.Edges)
	}

	edgeKinds := make(map[string]StaticTopologyEdge)
	for _, e := range logical.Edges {
		edgeKinds[e.Kind] = e
	}
	intra, ok := edgeKinds[EdgeKindIntraProcess]
	if !ok || intra.SrcModuleID != mod1.ID || intra.DstModuleID == nil || *intra.DstModuleID != mod2.ID {
		t.Fatalf("INTRA_PROCESS 边推导错误: %+v", intra)
	}
	cross, ok := edgeKinds[EdgeKindCrossProcess]
	if !ok || cross.SrcModuleID != mod1.ID || cross.DstModuleID == nil || *cross.DstModuleID != mod3.ID {
		t.Fatalf("CROSS_PROCESS 边推导错误: %+v", cross)
	}
	ext, ok := edgeKinds[EdgeKindExternalUnregistered]
	if !ok || ext.SrcModuleID != mod3.ID || ext.TargetNodeID != VirtualExternalNodeID || ext.DstModuleID != nil {
		t.Fatalf("EXTERNAL_UNREGISTERED 边推导错误: %+v", ext)
	}

	// 3. 校验 PHYSICAL 物理拓扑：仅含 SHARED Agent 与 SHARED 进程，严禁混入私有 Agent/进程
	physical, err := BuildStaticTopology(db, TopologyViewPhysical, nil)
	if err != nil {
		t.Fatalf("BuildStaticTopology PHYSICAL 失败: %v", err)
	}
	hasSharedAgent := false
	hasExternalNode := false
	for _, n := range physical.Nodes {
		if n.NodeType == "AGENT" && n.EntityID == privAgent.ID {
			t.Fatalf("私有 Agent [%d] 严禁混入公共 PHYSICAL 静态拓扑节点！", privAgent.ID)
		}
		if n.NodeType == "PROCESS" && n.EntityID == privProc.ID {
			t.Fatalf("私有进程 [%d] 严禁混入公共 PHYSICAL 静态拓扑节点！", privProc.ID)
		}
		if n.NodeType == "AGENT" && n.EntityID == sharedAgent.ID {
			hasSharedAgent = true
		}
		if n.ID == VirtualExternalNodeID {
			hasExternalNode = true
		}
	}
	if !hasSharedAgent || !hasExternalNode {
		t.Fatalf("PHYSICAL 拓扑缺少 SHARED Agent 节点 (%v) 或外部虚拟占位节点 (%v)", hasSharedAgent, hasExternalNode)
	}

	// 4. 校验 CompileProcessDownstreams 自动推导下游端点（含同机 127.0.0.1 回环别名与 ExtraDownstreams）
	downstreams, err := CompileProcessDownstreams(db, proc1.ID, []uint64{proc1.ID, proc2.ID}, nil)
	if err != nil {
		t.Fatalf("CompileProcessDownstreams 失败: %v", err)
	}
	dsMap := make(map[string]bool)
	for _, ep := range downstreams {
		dsMap[fmt.Sprintf("%s:%d", ep.IP, ep.Port)] = true
	}
	for _, expectedEP := range []string{"192.168.56.31:9202", "127.0.0.1:9202", "10.10.0.5:8800"} {
		if !dsMap[expectedEP] {
			t.Fatalf("CompileProcessDownstreams 缺失期望下游端点 %s，实际列表: %+v", expectedEP, downstreams)
		}
	}
	if dsMap["192.168.56.31:9201"] || dsMap["127.0.0.1:9201"] {
		t.Fatalf("CompileProcessDownstreams 不应包含进程自身监听端口: %+v", downstreams)
	}
}
