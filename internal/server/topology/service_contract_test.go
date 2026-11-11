package topology

import (
	"errors"
	"testing"

	"code-traefik/models"
)

func TestServiceContract_CRUDAndExclusiveMutex(t *testing.T) {
	db := setupIsolatedTopologyDB(t)

	// 1. 创建子系统、功能组与模块
	sub, err := CreateSubsystem(db, SubsystemInput{Code: "SUB_M20", Name: "M20测试子系统"})
	if err != nil {
		t.Fatalf("CreateSubsystem 失败: %v", err)
	}
	grp, err := CreateFunctionalGroup(db, FunctionalGroupInput{SubsystemID: sub.ID, Code: "GRP_M20", Name: "M20功能组"})
	if err != nil {
		t.Fatalf("CreateFunctionalGroup 失败: %v", err)
	}

	// 接口库模块 A
	libMod, err := CreateModule(db, ModuleInput{
		GroupID:    grp.ID,
		Code:       "MOD_LIB_A",
		Name:       "接口定义库A",
		ModuleKind: models.ModuleKindContractLib,
	})
	if err != nil {
		t.Fatalf("创建 CONTRACT_LIB 模块失败: %v", err)
	}

	// 业务模块 B
	bizMod, err := CreateModule(db, ModuleInput{
		GroupID:    grp.ID,
		Code:       "MOD_BIZ_B",
		Name:       "业务模块B",
		ModuleKind: models.ModuleKindBusiness,
	})
	if err != nil {
		t.Fatalf("创建 BUSINESS 模块失败: %v", err)
	}

	// 2. 非 CONTRACT_LIB 模块登记契约应被拒绝 (400)
	_, err = CreateServiceContract(db, ServiceContractInput{
		ServiceName:     "demo.v1.OrderService",
		DefinerModuleID: bizMod.ID,
	})
	if !errors.Is(err, ErrDefinerNotContractLib) {
		t.Fatalf("非 CONTRACT_LIB 登记契约期望 ErrDefinerNotContractLib，实际: %v", err)
	}

	// 3. 在 CONTRACT_LIB 下登记契约成功
	contract, err := CreateServiceContract(db, ServiceContractInput{
		ServiceName:     "demo.v1.OrderService",
		DefinerModuleID: libMod.ID,
		ImplKeyLabel:    "业务类型",
		Description:     "多实现订单服务契约",
	})
	if err != nil {
		t.Fatalf("登记服务契约失败: %v", err)
	}
	if contract.ID == 0 || contract.ServiceName != "demo.v1.OrderService" {
		t.Fatalf("契约字段不正确: %+v", contract)
	}

	// 4. 契约详情与列表查询
	detail, err := GetServiceContract(db, contract.ID)
	if err != nil {
		t.Fatalf("GetServiceContract 失败: %v", err)
	}
	if detail.ServiceName != "demo.v1.OrderService" || detail.DefinerModuleCode != "MOD_LIB_A" {
		t.Fatalf("契约详情字段不正确: %+v", detail)
	}

	list, total, err := ListServiceContracts(db, ServiceContractFilter{Page: 1, PageSize: 10})
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("ListServiceContracts 期望 1 条，实际 total=%d len=%d err=%v", total, len(list), err)
	}

	// 5. 创建独占服务 "demo.v1.ExclusiveSvc"
	exclSvc, err := CreateModuleService(db, ModuleServiceInput{
		ModuleID:    bizMod.ID,
		ServiceName: "demo.v1.ExclusiveSvc",
	})
	if err != nil {
		t.Fatalf("创建独占服务失败: %v", err)
	}

	// 6. 用已被独占服务占用的 service_name 登记契约，应返回 409
	_, err = CreateServiceContract(db, ServiceContractInput{
		ServiceName:     exclSvc.ServiceName,
		DefinerModuleID: libMod.ID,
	})
	if !errors.Is(err, ErrServiceNameOccupiedByExclusive) {
		t.Fatalf("已被独占占用的名称登记契约期望 ErrServiceNameOccupiedByExclusive，实际: %v", err)
	}

	// 7. 用已被契约占用的 service_name 创建独占服务，应返回 409
	_, err = CreateModuleService(db, ModuleServiceInput{
		ModuleID:    bizMod.ID,
		ServiceName: "demo.v1.OrderService",
	})
	if !errors.Is(err, ErrServiceNameOccupiedByContract) {
		t.Fatalf("已被契约占用的名称创建独占服务期望 ErrServiceNameOccupiedByContract，实际: %v", err)
	}

	// 8. 更新与删除契约
	_, updated, err := UpdateServiceContract(db, contract.ID, UpdateServiceContractInput{
		ImplKeyLabel: "租户类型",
		Description:  "更新后的说明",
	})
	if err != nil || updated.ImplKeyLabel != "租户类型" {
		t.Fatalf("UpdateServiceContract 失败: %v", err)
	}

	_, err = DeleteServiceContract(db, contract.ID)
	if err != nil {
		t.Fatalf("DeleteServiceContract 失败: %v", err)
	}
}

func TestServiceContract_MultiImplRegistrationAndConstraints(t *testing.T) {
	db := setupIsolatedTopologyDB(t)

	sub, _ := CreateSubsystem(db, SubsystemInput{Code: "SUB_M20_2", Name: "M20约束测试子系统"})
	grp, _ := CreateFunctionalGroup(db, FunctionalGroupInput{SubsystemID: sub.ID, Code: "GRP_M20_2", Name: "M20功能组2"})

	// 创建进程组与 3 个不同的标准架构进程
	pgrp := models.ArchProcessGroup{Code: "PGRP_TEST", Name: "进程组"}
	if err := db.Create(&pgrp).Error; err != nil {
		t.Fatalf("创建进程组失败: %v", err)
	}
	procB := models.ArchProcess{GroupID: pgrp.ID, Code: "proc-b", Name: "进程B", DefaultListenPort: 9001}
	procC := models.ArchProcess{GroupID: pgrp.ID, Code: "proc-c", Name: "进程C", DefaultListenPort: 9002}
	procD := models.ArchProcess{GroupID: pgrp.ID, Code: "proc-d", Name: "进程D", DefaultListenPort: 9003}
	db.Create(&procB)
	db.Create(&procC)
	db.Create(&procD)

	// 接口库 A
	libA, _ := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_LIB_A2", Name: "库A", ModuleKind: models.ModuleKindContractLib})
	contract, err := CreateServiceContract(db, ServiceContractInput{
		ServiceName:     "demo.v1.OrderProcessor",
		DefinerModuleID: libA.ID,
		ImplKeyLabel:    "业务分支",
	})
	if err != nil {
		t.Fatalf("创建契约失败: %v", err)
	}

	// 业务模块 B/C/D 分别挂载在不同进程
	modB, _ := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_B", Name: "模块B", ArchProcessID: &procB.ID})
	modC, _ := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_C", Name: "模块C", ArchProcessID: &procC.ID})
	modD, _ := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_D", Name: "模块D", ArchProcessID: &procD.ID})

	// 1. B, C 分别登记实现成功
	svcB, err := CreateModuleService(db, ModuleServiceInput{
		ModuleID:   modB.ID,
		ContractID: &contract.ID,
		ImplKey:    "b",
	})
	if err != nil {
		t.Fatalf("模块B登记实现失败: %v", err)
	}
	if svcB.ServiceName != "demo.v1.OrderProcessor" || svcB.ImplKey != "b" {
		t.Fatalf("实现B属性异常: %+v", svcB)
	}

	_, err = CreateModuleService(db, ModuleServiceInput{
		ModuleID:   modC.ID,
		ContractID: &contract.ID,
		ImplKey:    "c",
	})
	if err != nil {
		t.Fatalf("模块C登记实现失败: %v", err)
	}

	// 2. D 重复使用 impl_key = "b" 期望 409 (ErrDuplicateImplKey)
	_, err = CreateModuleService(db, ModuleServiceInput{
		ModuleID:   modD.ID,
		ContractID: &contract.ID,
		ImplKey:    "b",
	})
	if !errors.Is(err, ErrDuplicateImplKey) {
		t.Fatalf("重复 impl_key 期望 ErrDuplicateImplKey，实际: %v", err)
	}

	// 3. 同一模块 B 对同一契约重复登记期望 409 (ErrModuleAlreadyImplementsContract)
	_, err = CreateModuleService(db, ModuleServiceInput{
		ModuleID:   modB.ID,
		ContractID: &contract.ID,
		ImplKey:    "b_another",
	})
	if !errors.Is(err, ErrModuleAlreadyImplementsContract) {
		t.Fatalf("同一模块重复实现期望 ErrModuleAlreadyImplementsContract，实际: %v", err)
	}

	// 4. CONTRACT_LIB 模块约束：禁止绑定进程、禁止 PROVIDE、禁止 DEPEND
	_, _, err = UpdateModule(db, libA.ID, ModuleInput{
		GroupID:          grp.ID,
		Code:             libA.Code,
		Name:             libA.Name,
		ArchProcessID:    &procB.ID,
		HasArchProcessID: true,
	})
	if !errors.Is(err, ErrContractLibNoProcess) {
		t.Fatalf("CONTRACT_LIB 绑定进程期望 ErrContractLibNoProcess，实际: %v", err)
	}

	_, err = CreateModuleService(db, ModuleServiceInput{
		ModuleID:    libA.ID,
		ServiceName: "demo.v1.LibForbiddenSvc",
	})
	if !errors.Is(err, ErrContractLibNoProvide) {
		t.Fatalf("CONTRACT_LIB 声明 PROVIDE 期望 ErrContractLibNoProvide，实际: %v", err)
	}

	_, err = CreateModuleDependency(db, ModuleDependencyInput{
		ConsumerModuleID: libA.ID,
		ServiceID:        &svcB.ID,
	})
	if !errors.Is(err, ErrContractLibNoDepend) {
		t.Fatalf("CONTRACT_LIB 声明 DEPEND 期望 ErrContractLibNoDepend，实际: %v", err)
	}

	// 5. 存在实现的契约禁止删除
	_, err = DeleteServiceContract(db, contract.ID)
	if !errors.Is(err, ErrContractHasImplementations) {
		t.Fatalf("存在实现的契约删除期望 ErrContractHasImplementations，实际: %v", err)
	}
}

func TestServiceContract_AssertImplProcessDisjoint_A09(t *testing.T) {
	db := setupIsolatedTopologyDB(t)

	sub, _ := CreateSubsystem(db, SubsystemInput{Code: "SUB_A09", Name: "A-09测试子系统"})
	grp, _ := CreateFunctionalGroup(db, FunctionalGroupInput{SubsystemID: sub.ID, Code: "GRP_A09", Name: "A-09功能组"})

	pgrp := models.ArchProcessGroup{Code: "PGRP_A09", Name: "A-09进程组"}
	if err := db.Create(&pgrp).Error; err != nil {
		t.Fatalf("创建进程组失败: %v", err)
	}
	proc1 := models.ArchProcess{GroupID: pgrp.ID, Code: "proc-1", Name: "进程1", DefaultListenPort: 9101}
	proc2 := models.ArchProcess{GroupID: pgrp.ID, Code: "proc-2", Name: "进程2", DefaultListenPort: 9102}
	db.Create(&proc1)
	db.Create(&proc2)

	lib, _ := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "LIB_A09", Name: "契约库", ModuleKind: models.ModuleKindContractLib})
	contract, _ := CreateServiceContract(db, ServiceContractInput{
		ServiceName:     "demo.v1.DisjointService",
		DefinerModuleID: lib.ID,
	})

	// 模块 B 绑定 proc-1 并实现契约
	modB, _ := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_B_A09", Name: "模块B", ArchProcessID: &proc1.ID})
	_, err := CreateModuleService(db, ModuleServiceInput{
		ModuleID:   modB.ID,
		ContractID: &contract.ID,
		ImplKey:    "b",
	})
	if err != nil {
		t.Fatalf("模块B登记实现失败: %v", err)
	}

	// 模块 C 已绑定同进程 proc-1，此时尝试实现同一契约应被 A-09 拦截 (409)
	modC, _ := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_C_A09", Name: "模块C", ArchProcessID: &proc1.ID})
	_, err = CreateModuleService(db, ModuleServiceInput{
		ModuleID:   modC.ID,
		ContractID: &contract.ID,
		ImplKey:    "c",
	})
	if !errors.Is(err, ErrContractImplProcessConflict) {
		t.Fatalf("同一进程内实现同一契约期望 ErrContractImplProcessConflict，实际: %v", err)
	}

	// 模块 D 初始未绑定进程，成功实现契约
	modD, _ := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_D_A09", Name: "模块D"})
	_, err = CreateModuleService(db, ModuleServiceInput{
		ModuleID:   modD.ID,
		ContractID: &contract.ID,
		ImplKey:    "d",
	})
	if err != nil {
		t.Fatalf("模块D未绑定进程时实现契约期望成功，实际: %v", err)
	}

	// 尝试将模块 D 改绑至已包含实现 B 的 proc-1 -> 应被 A-09 拦截
	_, _, err = UpdateModule(db, modD.ID, ModuleInput{
		GroupID:          grp.ID,
		Code:             modD.Code,
		Name:             modD.Name,
		ArchProcessID:    &proc1.ID,
		HasArchProcessID: true,
	})
	if !errors.Is(err, ErrContractImplProcessConflict) {
		t.Fatalf("改绑至已有同契约实现的进程期望 ErrContractImplProcessConflict，实际: %v", err)
	}

	// 改绑至空闲进程 proc-2 -> 成功
	_, _, err = UpdateModule(db, modD.ID, ModuleInput{
		GroupID:          grp.ID,
		Code:             modD.Code,
		Name:             modD.Name,
		ArchProcessID:    &proc2.ID,
		HasArchProcessID: true,
	})
	if err != nil {
		t.Fatalf("改绑至独立进程期望成功，实际: %v", err)
	}
}

func TestServiceContract_ConvertFromExclusive(t *testing.T) {
	db := setupIsolatedTopologyDB(t)

	sub, _ := CreateSubsystem(db, SubsystemInput{Code: "SUB_CONV", Name: "转换子系统"})
	grp, _ := CreateFunctionalGroup(db, FunctionalGroupInput{SubsystemID: sub.ID, Code: "GRP_CONV", Name: "转换功能组"})

	// 存量业务模块 B，提供独占服务
	modB, _ := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_B_CONV", Name: "供给模块B"})
	legacySvc, err := CreateModuleService(db, ModuleServiceInput{
		ModuleID:    modB.ID,
		ServiceName: "demo.v1.LegacyPayment",
		Description: "存量独占支付服务",
	})
	if err != nil {
		t.Fatalf("创建存量独占服务失败: %v", err)
	}

	// 消费模块 X，依赖 legacySvc
	modX, _ := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_X_CONV", Name: "消费模块X"})
	depX, err := CreateModuleDependency(db, ModuleDependencyInput{
		ConsumerModuleID: modX.ID,
		ServiceID:        &legacySvc.ID,
	})
	if err != nil {
		t.Fatalf("创建消费依赖失败: %v", err)
	}

	// 新建契约定义库模块 A
	libA, _ := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "LIB_A_CONV", Name: "契约定义库A", ModuleKind: models.ModuleKindContractLib})

	// 执行一键转换 (ADR-16 §7.4)
	contract, updatedSvc, err := ConvertExclusiveToMultiImplContract(db, ConvertExclusiveContractInput{
		ServiceID:                   legacySvc.ID,
		DefinerModuleID:             libA.ID,
		ImplKeyLabel:                "支付渠道",
		InitialImplKey:              "alipay",
		ConvertDependentsToDispatch: true,
		Description:                 "升级后的多实现支付契约",
	})
	if err != nil {
		t.Fatalf("ConvertExclusiveToMultiImplContract 失败: %v", err)
	}

	if contract.ServiceName != "demo.v1.LegacyPayment" || contract.DefinerModuleID != libA.ID {
		t.Fatalf("生成的契约属性不符: %+v", contract)
	}

	if updatedSvc.ID != legacySvc.ID || updatedSvc.ContractID == nil || *updatedSvc.ContractID != contract.ID || updatedSvc.ImplKey != "alipay" {
		t.Fatalf("更新后的原服务记录不符: %+v", updatedSvc)
	}

	// 验证消费模块 X 的依赖已被原子转换为 DISPATCH (service_id = nil, contract_id = contract.ID)
	var reloadedDep models.ModuleDependency
	if err := db.First(&reloadedDep, depX.ID).Error; err != nil {
		t.Fatalf("加载依赖记录失败: %v", err)
	}
	if reloadedDep.ServiceID != nil {
		t.Fatalf("期望 service_id 为 nil，实际: %v", reloadedDep.ServiceID)
	}
	if reloadedDep.ContractID == nil || *reloadedDep.ContractID != contract.ID {
		t.Fatalf("期望 contract_id 为 %d，实际: %v", contract.ID, reloadedDep.ContractID)
	}
}
