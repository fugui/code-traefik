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

func TestServiceContract_CustomPublishedNames_A09ExemptionAndAttach(t *testing.T) {
	db := setupIsolatedTopologyDB(t)

	sub, _ := CreateSubsystem(db, SubsystemInput{Code: "SUB_V2", Name: "ADR16-V2子系统"})
	grp, _ := CreateFunctionalGroup(db, FunctionalGroupInput{SubsystemID: sub.ID, Code: "GRP_V2", Name: "ADR16-V2功能组"})

	libMod, err := CreateModule(db, ModuleInput{
		GroupID:    grp.ID,
		Code:       "LIB_SYNC",
		Name:       "SyncService 接口库",
		ModuleKind: models.ModuleKindContractLib,
	})
	if err != nil {
		t.Fatalf("创建接口库失败: %v", err)
	}

	contract, err := CreateServiceContract(db, ServiceContractInput{
		ServiceName:     "SyncService",
		DefinerModuleID: libMod.ID,
		ImplKeyLabel:    "业务线",
	})
	if err != nil {
		t.Fatalf("CreateServiceContract 失败: %v", err)
	}

	pgrp := models.ArchProcessGroup{Code: "APG_V2", Name: "V2进程组"}
	db.Create(&pgrp)
	sharedProc := models.ArchProcess{GroupID: pgrp.ID, Code: "PROC_SHARED_SYNC", Name: "共享宿主进程", DefaultListenPort: 19001}
	db.Create(&sharedProc)

	modDH, _ := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_DH", Name: "DH实现", ArchProcessID: &sharedProc.ID})
	modKH, _ := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_KH", Name: "KH实现", ArchProcessID: &sharedProc.ID})
	modLegacy, _ := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_LEGACY", Name: "存量实现", ArchProcessID: &sharedProc.ID})
	modConsumer, _ := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_CONS", Name: "调用方"})

	// 1. 同一 ArchProcess 下允许同一契约的两个自定义唯一服务名实现共存（豁免 A-09）
	svcDH, err := CreateModuleService(db, ModuleServiceInput{
		ModuleID:    modDH.ID,
		ContractID:  &contract.ID,
		ImplKey:     "DH",
		ServiceName: "DH_SyncService",
	})
	if err != nil {
		t.Fatalf("创建自定义服务名实现 DH_SyncService 失败: %v", err)
	}
	if !svcDH.IsCustomServiceName || svcDH.ServiceName != "DH_SyncService" {
		t.Fatalf("期望 IsCustomServiceName=true 且 ServiceName=DH_SyncService，实际: %+v", svcDH)
	}

	svcKH, err := CreateModuleService(db, ModuleServiceInput{
		ModuleID:    modKH.ID,
		ContractID:  &contract.ID,
		ImplKey:     "KH",
		ServiceName: "KH_SyncService",
	})
	if err != nil {
		t.Fatalf("同一进程添加另一自定义名 KH_SyncService 应豁免 A-09 并成功，实际: %v", err)
	}
	if !svcKH.IsCustomServiceName {
		t.Fatalf("期望 KH_SyncService IsCustomServiceName=true")
	}

	// 2. 自定义服务名全局互斥：禁止其他模块以独占或自定义名重复声明 DH_SyncService
	_, err = CreateModuleService(db, ModuleServiceInput{
		ModuleID:    modConsumer.ID,
		ServiceName: "DH_SyncService",
	})
	var conflictErr *ProvideConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("重复声明已被占用的自定义运行期服务名期望 ProvideConflictError，实际: %v", err)
	}

	// 3. 共享契约名实现仍严格遵循 A-09
	modShared1, _ := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_SH1", Name: "共享实现1", ArchProcessID: &sharedProc.ID})
	modShared2, _ := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_SH2", Name: "共享实现2", ArchProcessID: &sharedProc.ID})
	if _, err := CreateModuleService(db, ModuleServiceInput{
		ModuleID:   modShared1.ID,
		ContractID: &contract.ID,
		ImplKey:    "SH1",
	}); err != nil {
		t.Fatalf("首个共享名实现期望成功，实际: %v", err)
	}
	if _, err := CreateModuleService(db, ModuleServiceInput{
		ModuleID:   modShared2.ID,
		ContractID: &contract.ID,
		ImplKey:    "SH2",
	}); !errors.Is(err, ErrContractImplProcessConflict) {
		t.Fatalf("同进程第二个共享契约名实现期望被 A-09 拦截 (ErrContractImplProcessConflict)，实际: %v", err)
	}

	// 试图通过 UpdateModuleService 将自定义名 KH_SyncService 改回共享契约名 SyncService，也应触发 A-09
	sharedName := "SyncService"
	if _, _, err := UpdateModuleServiceWithOptions(db, UpdateModuleServiceInput{
		ServiceID:   svcKH.ID,
		ServiceName: &sharedName,
	}); !errors.Is(err, ErrContractImplProcessConflict) {
		t.Fatalf("同进程将自定义名改回共享契约名期望触发 A-09，实际: %v", err)
	}

	// 4. GetServiceContract 详情应包含每个实现的 ServiceName 与 IsCustomServiceName
	detail, err := GetServiceContract(db, contract.ID)
	if err != nil {
		t.Fatalf("GetServiceContract 失败: %v", err)
	}
	foundDH := false
	for _, impl := range detail.Implementations {
		if impl.ServiceID == svcDH.ID {
			foundDH = true
			if impl.ServiceName != "DH_SyncService" || !impl.IsCustomServiceName {
				t.Fatalf("实现矩阵行缺少自定义服务名属性: %+v", impl)
			}
		}
	}
	if !foundDH {
		t.Fatalf("GetServiceContract 实现矩阵中未找到 DH_SyncService")
	}

	// 5. AttachExclusiveToMultiImplContract：将存量独占服务 LEGACY_SyncService 无损归入 SyncService 契约
	legacyExcl, err := CreateModuleService(db, ModuleServiceInput{
		ModuleID:    modLegacy.ID,
		ServiceName: "LEGACY_SyncService",
	})
	if err != nil {
		t.Fatalf("创建存量独占服务失败: %v", err)
	}
	dep, _ := CreateModuleDependency(db, ModuleDependencyInput{
		ConsumerModuleID: modConsumer.ID,
		ServiceID:        &legacyExcl.ID,
	})

	_, attachedSvc, converted, err := AttachExclusiveToMultiImplContract(db, AttachExclusiveToContractInput{
		ContractID:                  contract.ID,
		ServiceID:                   legacyExcl.ID,
		ImplKey:                     "LEGACY",
		ConvertDependentsToDispatch: true,
	})
	if err != nil {
		t.Fatalf("AttachExclusiveToMultiImplContract 失败: %v", err)
	}
	if attachedSvc.ID != legacyExcl.ID || attachedSvc.ContractID == nil || *attachedSvc.ContractID != contract.ID ||
		!attachedSvc.IsCustomServiceName || attachedSvc.ServiceName != "LEGACY_SyncService" || attachedSvc.ImplKey != "LEGACY" {
		t.Fatalf("归入后实现记录属性不符: %+v", attachedSvc)
	}
	if converted != 1 {
		t.Fatalf("期望转换 1 条依赖为 DISPATCH，实际: %d", converted)
	}
	var reloadedDep models.ModuleDependency
	db.First(&reloadedDep, dep.ID)
	if reloadedDep.ServiceID != nil || reloadedDep.ContractID == nil || *reloadedDep.ContractID != contract.ID {
		t.Fatalf("原直连依赖未转为 DISPATCH: %+v", reloadedDep)
	}
}
