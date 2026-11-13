package main

import (
	"path/filepath"
	"testing"

	"code-common/backend/testdb"
	"code-traefik/config"
	"code-traefik/internal/server/decoder"
	"code-traefik/internal/server/topology"
	"code-traefik/models"
)

func TestSeedDemoData_CompleteGraphAndDecoderAliases(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}

	demoPbPath := filepath.Join("..", "..", "demo", "demo.pb")

	// 连续执行两次验证 TRUNCATE + 重建幂等性
	for run := 1; run <= 2; run++ {
		if err := seedDemoData(db, demoPbPath); err != nil {
			t.Fatalf("第 %d 次执行 seedDemoData 失败: %v", run, err)
		}
	}

	// 1. 验证模块与 CONTRACT_LIB 定义
	var modules []models.Module
	if err := db.Find(&modules).Error; err != nil {
		t.Fatalf("查询模块失败: %v", err)
	}
	if len(modules) != 7 {
		t.Fatalf("期望 7 个功能模块，实际 %d", len(modules))
	}

	var libMod models.Module
	if err := db.Where("code = ?", "LIB_PAYMENT_CONTRACT").First(&libMod).Error; err != nil {
		t.Fatalf("查询 LIB_PAYMENT_CONTRACT 失败: %v", err)
	}
	if libMod.ModuleKind != models.ModuleKindContractLib {
		t.Fatalf("LIB_PAYMENT_CONTRACT module_kind 错误: got %q", libMod.ModuleKind)
	}

	// 2. 验证 ServiceContract 与多渠道自定义服务名实现
	var contract models.ServiceContract
	if err := db.Where("service_name = ?", "demo.payment.PaymentService").First(&contract).Error; err != nil {
		t.Fatalf("查询支付多实现契约失败: %v", err)
	}
	if contract.ProvideMode != models.ContractProvideModeMultiImpl || contract.DefinerModuleID != libMod.ID {
		t.Fatalf("支付契约属性不匹配: %+v", contract)
	}

	for _, tc := range []struct {
		serviceName string
		implKey     string
	}{
		{"demo.payment.ALIPAY_PaymentService", "ALIPAY"},
		{"demo.payment.WECHAT_PaymentService", "WECHAT"},
	} {
		var svc models.ModuleService
		if err := db.Where("service_name = ?", tc.serviceName).First(&svc).Error; err != nil {
			t.Fatalf("查询服务实现 %s 失败: %v", tc.serviceName, err)
		}
		if svc.ContractID == nil || *svc.ContractID != contract.ID {
			t.Fatalf("服务 %s 未关联支付契约 %d: %+v", tc.serviceName, contract.ID, svc)
		}
		if !svc.IsCustomServiceName || svc.ImplKey != tc.implKey {
			t.Fatalf("服务 %s 自定义服务名标记或 impl_key 错误: %+v", tc.serviceName, svc)
		}
	}

	// 3. 验证 ORDER_CORE -> PaymentService DISPATCH 依赖与 6 个物理挂载进程
	var dispatchCount int64
	if err := db.Model(&models.ModuleDependency{}).Where("contract_id = ?", contract.ID).Count(&dispatchCount).Error; err != nil {
		t.Fatalf("统计 DISPATCH 依赖失败: %v", err)
	}
	if dispatchCount != 1 {
		t.Fatalf("期望 1 条 DISPATCH 契约依赖，实际 %d", dispatchCount)
	}

	var procCount int64
	if err := db.Model(&models.Process{}).Count(&procCount).Error; err != nil {
		t.Fatalf("统计挂载进程失败: %v", err)
	}
	if procCount != 6 {
		t.Fatalf("期望 6 个挂载物理进程，实际 %d", procCount)
	}

	// 4. 验证 Protobuf 解码注册中心通过契约别名映射解析自定义服务名
	reg, err := decoder.NewRegistry(db)
	if err != nil {
		t.Fatalf("初始化 decoder.Registry 失败: %v", err)
	}
	for _, svcName := range []string{
		"demo.payment.ALIPAY_PaymentService",
		"demo.payment.WECHAT_PaymentService",
	} {
		for _, opName := range []string{"AuthorizePay", "RefundPay"} {
			op := reg.LookupOperation(svcName, opName)
			if op == nil {
				t.Fatalf("decoder 未能通过 LIB 契约别名解析 %s:%s", svcName, opName)
			}
			if op.ServiceName != svcName {
				t.Fatalf("LookupOperation 返回 ServiceName=%q，期望 %q", op.ServiceName, svcName)
			}
		}
	}

	// 5. 验证逻辑/物理拓扑图可正常构建且含 REALIZATION 边
	logicalGraph, err := topology.BuildTopologyGraph(db, topology.TopologyViewLogical)
	if err != nil {
		t.Fatalf("构建 LOGICAL 拓扑图失败: %v", err)
	}
	realizationCount := 0
	for _, edge := range logicalGraph.Edges {
		if edge.Kind == topology.EdgeKindRealization {
			realizationCount++
		}
	}
	if realizationCount != 2 {
		t.Fatalf("期望 LOGICAL 拓扑图含 2 条 REALIZATION 边，实际 %d", realizationCount)
	}

	physicalGraph, err := topology.BuildTopologyGraph(db, topology.TopologyViewPhysical)
	if err != nil {
		t.Fatalf("构建 PHYSICAL 拓扑图失败: %v", err)
	}
	if len(physicalGraph.Nodes) == 0 || len(physicalGraph.Edges) == 0 {
		t.Fatalf("PHYSICAL 拓扑图为空: nodes=%d edges=%d", len(physicalGraph.Nodes), len(physicalGraph.Edges))
	}
}
