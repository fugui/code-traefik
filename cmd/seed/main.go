package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"code-traefik/config"
	"code-traefik/internal/server/decoder"
	"code-traefik/models"
	"code-traefik/pkg/version"

	"gorm.io/datatypes"
)

func main() {
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	demoPbPath := flag.String("proto", "demo/demo.pb", "Protobuf 描述集路径")
	flag.Parse()

	log.Println("==========================================================")
	log.Println(" 开始清除 Traefik 数据并根据 Demo 进程拓扑重构数据...")
	log.Println("==========================================================")

	// 1. 加载配置
	if err := config.LoadConfig(*configPath); err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 2. 初始化数据库连接
	if err := models.InitDB(); err != nil {
		log.Fatalf("初始化数据库连接失败: %v", err)
	}

	db := models.DB

	// 3. 清理所有 traefik_* 业务数据表 (保留平台公共 users/departments/sys_audit_logs)
	log.Println("[1/7] 清空历史 traefik_* 业务数据表...")
	truncateSQL := `
		TRUNCATE TABLE 
			traefik_traffic_records,
			traefik_recording_sessions,
			traefik_replay_items,
			traefik_replay_tasks,
			traefik_processes,
			traefik_module_dependencies,
			traefik_module_services,
			traefik_service_contracts,
			traefik_modules,
			traefik_functional_groups,
			traefik_subsystems,
			traefik_arch_processes,
			traefik_arch_process_groups,
			traefik_proto_operations,
			traefik_proto_schemas,
			traefik_agents
		RESTART IDENTITY CASCADE;
	`
	if err := db.Exec(truncateSQL).Error; err != nil {
		log.Fatalf("清空表失败: %v", err)
	}

	// 4. 重建默认公共 Agent (demo-local-agent)
	log.Println("[2/7] 重建默认本地 Agent...")
	agent := models.Agent{
		Name:            "demo-local-agent",
		Endpoint:        "http://127.0.0.1:9091",
		Scope:           models.ScopeShared,
		Status:          models.AgentStatusOnline,
		KernelVersion:   "Linux",
		HasEbpfCap:      true,
		CapDetail:       "EUID=0 (Full root privileges)",
		AgentVersion:    version.Short(),
		ProtocolVersion: "1.0",
		MountsRevision:  1,
		CreatedBy:       1,
	}
	if err := db.Create(&agent).Error; err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}
	log.Printf("  • 已创建 Agent [%s] (ID: %d)", agent.Name, agent.ID)

	// 5. 重建子系统与功能组 (Subsystems & FunctionalGroups)
	log.Println("[3/7] 重建子系统与功能组...")
	subsysEcommerce := models.Subsystem{
		Code:        "ECOMMERCE",
		Name:        "电商交易微服务系统",
		Description: "基于 CROS RPC 协议构建的高并发电商分布式微服务集群（含交易中枢、账户资产与仓储履约）",
		CreatedBy:   1,
	}
	if err := db.Create(&subsysEcommerce).Error; err != nil {
		log.Fatalf("创建电商子系统失败: %v", err)
	}

	subsysClient := models.Subsystem{
		Code:        "CLIENT",
		Name:        "客户端交互子系统",
		Description: "包含 TUI 交互式控制台终端与压力测试流量发生器",
		CreatedBy:   1,
	}
	if err := db.Create(&subsysClient).Error; err != nil {
		log.Fatalf("创建客户端子系统失败: %v", err)
	}

	fgTrade := models.FunctionalGroup{
		SubsystemID: subsysEcommerce.ID,
		Code:        "TRADE_CORE",
		Name:        "交易中枢与网关",
		Description: "负责核心交易流程编排、两阶段履约锁定与分布式事务补偿",
		Level:       1,
		CreatedBy:   1,
	}
	db.Create(&fgTrade)
	fgTrade.Path = fmt.Sprintf("/%d/", fgTrade.ID)
	db.Model(&fgTrade).Update("path", fgTrade.Path)

	fgAsset := models.FunctionalGroup{
		SubsystemID: subsysEcommerce.ID,
		Code:        "ASSET_GROUP",
		Name:        "账户与资产服务",
		Description: "用户账本资产管理、余额扣减与退款逆向入账",
		Level:       1,
		CreatedBy:   1,
	}
	db.Create(&fgAsset)
	fgAsset.Path = fmt.Sprintf("/%d/", fgAsset.ID)
	db.Model(&fgAsset).Update("path", fgAsset.Path)

	fgInventory := models.FunctionalGroup{
		SubsystemID: subsysEcommerce.ID,
		Code:        "INVENTORY_GROUP",
		Name:        "仓储与履约服务",
		Description: "商品实时行情、库存预占锁定与出库实扣确认",
		Level:       1,
		CreatedBy:   1,
	}
	db.Create(&fgInventory)
	fgInventory.Path = fmt.Sprintf("/%d/", fgInventory.ID)
	db.Model(&fgInventory).Update("path", fgInventory.Path)

	fgPayment := models.FunctionalGroup{
		SubsystemID: subsysEcommerce.ID,
		Code:        "PAYMENT_GROUP",
		Name:        "支付通道与接口契约组",
		Description: "包含统一支付 LIB 契约库以及支付宝/微信等多渠道独立实现实例",
		Level:       1,
		CreatedBy:   1,
	}
	db.Create(&fgPayment)
	fgPayment.Path = fmt.Sprintf("/%d/", fgPayment.ID)
	db.Model(&fgPayment).Update("path", fgPayment.Path)

	fgCli := models.FunctionalGroup{
		SubsystemID: subsysClient.ID,
		Code:        "CLI_TOOLS",
		Name:        "终端工具组",
		Description: "包含键盘交互式终端与测试场景发压组件",
		Level:       1,
		CreatedBy:   1,
	}
	db.Create(&fgCli)
	fgCli.Path = fmt.Sprintf("/%d/", fgCli.ID)
	db.Model(&fgCli).Update("path", fgCli.Path)

	log.Printf("  • 已创建 2 个子系统与 5 个功能组")

	// 6. 重建标准架构物理进程组与蓝图 (ArchProcessGroups & ArchProcesses)
	log.Println("[4/7] 重建标准架构物理进程组与进程蓝图 (ADR-09, M10)...")
	apgGateway := models.ArchProcessGroup{
		Code:        "GROUP_GATEWAY",
		Name:        "网关与交易中枢组",
		Description: "承接外部业务流量并编排微服务调用",
		SortOrder:   1,
		CreatedBy:   1,
	}
	db.Create(&apgGateway)

	apgDownstream := models.ArchProcessGroup{
		Code:        "GROUP_DOWNSTREAM",
		Name:        "核心下游微服务组",
		Description: "高并发核心领域微服务（账户与库存履约）",
		SortOrder:   2,
		CreatedBy:   1,
	}
	db.Create(&apgDownstream)

	apgClient := models.ArchProcessGroup{
		Code:        "GROUP_CLIENT",
		Name:        "交互终端与发压组",
		Description: "测试控制台终端与流量发生器",
		SortOrder:   3,
		CreatedBy:   1,
	}
	db.Create(&apgClient)

	apSutOrder := models.ArchProcess{
		GroupID:                 apgGateway.ID,
		Code:                    "PROC_SUT_ORDER",
		Name:                    "sut-order (订单核心网关)",
		DefaultListenPort:       9001,
		DefaultCmdPattern:       "sut-order",
		DefaultExtraDownstreams: datatypes.JSON([]byte("[]")),
		SortOrder:               1,
		Description:             "电商交易被测核心网关服务，监听 9001 端口",
		CreatedBy:               1,
	}
	db.Create(&apSutOrder)

	apBackendAccount := models.ArchProcess{
		GroupID:                 apgDownstream.ID,
		Code:                    "PROC_BACKEND_ACCOUNT",
		Name:                    "backend-account (资产服务)",
		DefaultListenPort:       9002,
		DefaultCmdPattern:       "backend-account",
		DefaultExtraDownstreams: datatypes.JSON([]byte("[]")),
		SortOrder:               2,
		Description:             "核心下游账户资产服务，监听 9002 端口",
		CreatedBy:               1,
	}
	db.Create(&apBackendAccount)

	apBackendInventory := models.ArchProcess{
		GroupID:                 apgDownstream.ID,
		Code:                    "PROC_BACKEND_INVENTORY",
		Name:                    "backend-inventory (库存服务)",
		DefaultListenPort:       9003,
		DefaultCmdPattern:       "backend-inventory",
		DefaultExtraDownstreams: datatypes.JSON([]byte("[]")),
		SortOrder:               3,
		Description:             "核心下游仓储库存服务，监听 9003 端口",
		CreatedBy:               1,
	}
	db.Create(&apBackendInventory)

	apBackendPayAlipay := models.ArchProcess{
		GroupID:                 apgDownstream.ID,
		Code:                    "PROC_BACKEND_PAYMENT_ALIPAY",
		Name:                    "backend-payment-alipay (支付宝支付实例)",
		DefaultListenPort:       9004,
		DefaultCmdPattern:       "channel=ALIPAY",
		DefaultExtraDownstreams: datatypes.JSON([]byte("[]")),
		SortOrder:               4,
		Description:             "支付 LIB 契约的支付宝实现实例，监听 9004 端口",
		CreatedBy:               1,
	}
	db.Create(&apBackendPayAlipay)

	apBackendPayWechat := models.ArchProcess{
		GroupID:                 apgDownstream.ID,
		Code:                    "PROC_BACKEND_PAYMENT_WECHAT",
		Name:                    "backend-payment-wechat (微信支付实例)",
		DefaultListenPort:       9005,
		DefaultCmdPattern:       "channel=WECHAT",
		DefaultExtraDownstreams: datatypes.JSON([]byte("[]")),
		SortOrder:               5,
		Description:             "支付 LIB 契约的微信支付实现实例，监听 9005 端口",
		CreatedBy:               1,
	}
	db.Create(&apBackendPayWechat)

	apClientTui := models.ArchProcess{
		GroupID:                 apgClient.ID,
		Code:                    "PROC_CLIENT_TUI",
		Name:                    "client (TUI交互客户端)",
		DefaultListenPort:       0,
		DefaultCmdPattern:       "client",
		DefaultExtraDownstreams: datatypes.JSON([]byte("[]")),
		SortOrder:               6,
		Description:             "TUI 终端交互客户端，发压与交易触发器",
		CreatedBy:               1,
	}
	db.Create(&apClientTui)

	log.Printf("  • 已创建 3 个架构物理进程组与 6 个标准架构进程蓝图")

	// 7. 重建功能模块并绑定至标准架构进程 (Modules with ArchProcessID)
	log.Println("[5/7] 重建功能模块并绑定所属架构进程...")
	modOrder := models.Module{
		SubsystemID:   subsysEcommerce.ID,
		GroupID:       fgTrade.ID,
		ArchProcessID: &apSutOrder.ID,
		Code:          "ORDER_CORE",
		Name:          "订单交易核心模块",
		Description:   "负责订单生命周期、两阶段预占与状态流转",
		CreatedBy:     1,
	}
	db.Create(&modOrder)

	modAccount := models.Module{
		SubsystemID:   subsysEcommerce.ID,
		GroupID:       fgAsset.ID,
		ArchProcessID: &apBackendAccount.ID,
		Code:          "ACCOUNT_CORE",
		Name:          "账户资产核心模块",
		Description:   "资金账本与扣减补偿服务",
		CreatedBy:     1,
	}
	db.Create(&modAccount)

	modInventory := models.Module{
		SubsystemID:   subsysEcommerce.ID,
		GroupID:       fgInventory.ID,
		ArchProcessID: &apBackendInventory.ID,
		Code:          "INVENTORY_CORE",
		Name:          "仓储库存履约模块",
		Description:   "库存两阶段预占与释放管理",
		CreatedBy:     1,
	}
	db.Create(&modInventory)

	modClient := models.Module{
		SubsystemID:   subsysClient.ID,
		GroupID:       fgCli.ID,
		ArchProcessID: &apClientTui.ID,
		Code:          "CLIENT_TUI",
		Name:          "客户端交互终端模块",
		Description:   "TUI 控制台客户端与录制触发入口",
		CreatedBy:     1,
	}
	db.Create(&modClient)

	modLibPayment := models.Module{
		SubsystemID: subsysEcommerce.ID,
		GroupID:     fgPayment.ID,
		ModuleKind:  models.ModuleKindContractLib,
		Code:        "LIB_PAYMENT_CONTRACT",
		Name:        "统一支付通道接口契约库",
		Description: "ADR-16 v2.0 示例接口模块（CONTRACT_LIB，不绑定宿主进程，仅承载统一 .pb 服务契约）",
		CreatedBy:   1,
	}
	db.Create(&modLibPayment)

	modPayAlipay := models.Module{
		SubsystemID:   subsysEcommerce.ID,
		GroupID:       fgPayment.ID,
		ArchProcessID: &apBackendPayAlipay.ID,
		ModuleKind:    models.ModuleKindBusiness,
		Code:          "PAYMENT_ALIPAY",
		Name:          "支付宝渠道支付模块",
		Description:   "支付 LIB 契约的支付宝业务实现（发布自定义服务名 demo.payment.ALIPAY_PaymentService）",
		CreatedBy:     1,
	}
	db.Create(&modPayAlipay)

	modPayWechat := models.Module{
		SubsystemID:   subsysEcommerce.ID,
		GroupID:       fgPayment.ID,
		ArchProcessID: &apBackendPayWechat.ID,
		ModuleKind:    models.ModuleKindBusiness,
		Code:          "PAYMENT_WECHAT",
		Name:          "微信渠道支付模块",
		Description:   "支付 LIB 契约的微信支付业务实现（发布自定义服务名 demo.payment.WECHAT_PaymentService）",
		CreatedBy:     1,
	}
	db.Create(&modPayWechat)

	// 7.0 多实现接口契约定义 (ServiceContract)
	payContract := models.ServiceContract{
		DefinerModuleID: modLibPayment.ID,
		ServiceName:     "demo.payment.PaymentService",
		ProvideMode:     models.ContractProvideModeMultiImpl,
		ImplKeyLabel:    "支付渠道编码 (channel)",
		Description:     "统一支付渠道接口 LIB 契约，各实现实例可自定义运行期服务名并共用 .pb 解码",
		CreatedBy:       1,
	}
	if err := db.Create(&payContract).Error; err != nil {
		log.Fatalf("创建支付多实现契约失败: %v", err)
	}

	// 7.1 服务契约 (ModuleServices)
	svcOrder := models.ModuleService{
		ModuleID:        modOrder.ID,
		ServiceName:     "demo.order.OrderService",
		DiffIgnorePaths: datatypes.JSON([]byte("[]")),
		Description:     "CROS RPC 订单微服务对外契约",
		CreatedBy:       1,
	}
	db.Create(&svcOrder)

	svcAccount := models.ModuleService{
		ModuleID:        modAccount.ID,
		ServiceName:     "demo.account.AccountService",
		DiffIgnorePaths: datatypes.JSON([]byte("[]")),
		Description:     "CROS RPC 账户资产微服务契约",
		CreatedBy:       1,
	}
	db.Create(&svcAccount)

	svcInventory := models.ModuleService{
		ModuleID:        modInventory.ID,
		ServiceName:     "demo.inventory.InventoryService",
		DiffIgnorePaths: datatypes.JSON([]byte("[]")),
		Description:     "CROS RPC 仓储库存微服务契约",
		CreatedBy:       1,
	}
	db.Create(&svcInventory)

	svcPayAlipay := models.ModuleService{
		ModuleID:            modPayAlipay.ID,
		ServiceName:         "demo.payment.ALIPAY_PaymentService",
		ContractID:          &payContract.ID,
		ImplKey:             "ALIPAY",
		IsCustomServiceName: true,
		DiffIgnorePaths:     datatypes.JSON([]byte("[]")),
		Description:         "支付宝渠道运行期自定义服务名实现",
		CreatedBy:           1,
	}
	db.Create(&svcPayAlipay)

	svcPayWechat := models.ModuleService{
		ModuleID:            modPayWechat.ID,
		ServiceName:         "demo.payment.WECHAT_PaymentService",
		ContractID:          &payContract.ID,
		ImplKey:             "WECHAT",
		IsCustomServiceName: true,
		DiffIgnorePaths:     datatypes.JSON([]byte("[]")),
		Description:         "微信渠道运行期自定义服务名实现",
		CreatedBy:           1,
	}
	db.Create(&svcPayWechat)

	// 7.2 模块间依赖 (ModuleDependencies)
	depOrderAccount := models.ModuleDependency{
		ConsumerModuleID: modOrder.ID,
		IsExternal:       false,
		ServiceID:        &svcAccount.ID,
		ProviderModuleID: &modAccount.ID,
		Description:      "下单与退款时划扣或返还买家余额",
		CreatedBy:        1,
	}
	db.Create(&depOrderAccount)

	depOrderInventory := models.ModuleDependency{
		ConsumerModuleID: modOrder.ID,
		IsExternal:       false,
		ServiceID:        &svcInventory.ID,
		ProviderModuleID: &modInventory.ID,
		Description:      "下单时锁定与实扣库存，退款时释放库存",
		CreatedBy:        1,
	}
	db.Create(&depOrderInventory)

	depOrderPayDispatch := models.ModuleDependency{
		ConsumerModuleID: modOrder.ID,
		IsExternal:       false,
		ContractID:       &payContract.ID,
		Description:      "DISPATCH 泛化契约依赖：按支付渠道路由调用 ALIPAY 或 WECHAT 实例",
		CreatedBy:        1,
	}
	db.Create(&depOrderPayDispatch)

	depOrderUnionPay := models.ModuleDependency{
		ConsumerModuleID: modOrder.ID,
		IsExternal:       true,
		ExternalSystem:   "UNIONPAY",
		ExternalService:  "pay.v1.UnionPayService",
		Description:      "外部银联通道实名结算网关",
		CreatedBy:        1,
	}
	db.Create(&depOrderUnionPay)

	depClientOrder := models.ModuleDependency{
		ConsumerModuleID: modClient.ID,
		IsExternal:       false,
		ServiceID:        &svcOrder.ID,
		ProviderModuleID: &modOrder.ID,
		Description:      "TUI 交互控制台发起交易请求",
		CreatedBy:        1,
	}
	db.Create(&depClientOrder)

	log.Printf("  • 已创建 7 个功能模块（含 1 个 CONTRACT_LIB + 2 个自定义实现实例）、1 个多实现契约、5 个服务供给契约及 5 项服务调用依赖")

	// 8. 导入并激活 Protobuf 描述集 (demo.pb)
	log.Println("[6/7] 导入并激活 Protobuf 描述集 (demo.pb)...")
	rawBytes, err := os.ReadFile(*demoPbPath)
	if err != nil {
		log.Fatalf("读取 Protobuf 描述集 %s 失败: %v", *demoPbPath, err)
	}

	decoderReg, err := decoder.NewRegistry(db)
	if err != nil {
		log.Fatalf("初始化 Protobuf 解码注册中心失败: %v", err)
	}

	schema, ops, _, err := decoderReg.UploadSchema(decoder.UploadSchemaInput{
		Name:         "demo-ecommerce",
		Category:     models.ProtoCategoryBusiness,
		VersionTag:   "v1.0.0",
		FileName:     "demo.pb",
		RawBytes:     rawBytes,
		Description:  "CROS RPC 4进程微服务电商交易接口描述集",
		AutoActivate: true,
		OperatorID:   1,
	})
	if err != nil {
		log.Fatalf("上传并激活 Protobuf 描述集失败: %v", err)
	}
	log.Printf("  • 已导入描述集 [%s] v%d (已激活，共解析生成 %d 个 ProtoOperation 接口契约)",
		schema.Name, schema.Version, len(ops))

	// 9. 为本地 Agent 挂载全部 6 个物理业务进程 (traefik_processes)
	log.Println("[7/7] 为本地 Agent 挂载 6 个 Demo 物理业务进程 (traefik_processes)...")
	procs := []models.Process{
		{
			AgentID:             agent.ID,
			ArchProcessID:       apSutOrder.ID,
			EffectiveListenPort: 9001,
			OverrideCmdPattern:  "sut-order",
			AdvertiseIP:         "127.0.0.1",
			ExtraDownstreams:    datatypes.JSON([]byte("[]")),
			ProbeDesiredState:   models.ProbeDesiredStateArmed,
			ProbeStatus:         models.ProbeStatusArmed,
			Description:         "sut-order 核心网关进程实例 (:9001)",
			CreatedBy:           1,
		},
		{
			AgentID:             agent.ID,
			ArchProcessID:       apBackendAccount.ID,
			EffectiveListenPort: 9002,
			OverrideCmdPattern:  "backend-account",
			AdvertiseIP:         "127.0.0.1",
			ExtraDownstreams:    datatypes.JSON([]byte("[]")),
			ProbeDesiredState:   models.ProbeDesiredStateArmed,
			ProbeStatus:         models.ProbeStatusArmed,
			Description:         "backend-account 资产服务进程实例 (:9002)",
			CreatedBy:           1,
		},
		{
			AgentID:             agent.ID,
			ArchProcessID:       apBackendInventory.ID,
			EffectiveListenPort: 9003,
			OverrideCmdPattern:  "backend-inventory",
			AdvertiseIP:         "127.0.0.1",
			ExtraDownstreams:    datatypes.JSON([]byte("[]")),
			ProbeDesiredState:   models.ProbeDesiredStateArmed,
			ProbeStatus:         models.ProbeStatusArmed,
			Description:         "backend-inventory 库存履约进程实例 (:9003)",
			CreatedBy:           1,
		},
		{
			AgentID:             agent.ID,
			ArchProcessID:       apBackendPayAlipay.ID,
			EffectiveListenPort: 9004,
			OverrideCmdPattern:  "channel=ALIPAY",
			AdvertiseIP:         "127.0.0.1",
			ExtraDownstreams:    datatypes.JSON([]byte("[]")),
			ProbeDesiredState:   models.ProbeDesiredStateArmed,
			ProbeStatus:         models.ProbeStatusArmed,
			Description:         "backend-payment ALIPAY 渠道实例 (:9004)",
			CreatedBy:           1,
		},
		{
			AgentID:             agent.ID,
			ArchProcessID:       apBackendPayWechat.ID,
			EffectiveListenPort: 9005,
			OverrideCmdPattern:  "channel=WECHAT",
			AdvertiseIP:         "127.0.0.1",
			ExtraDownstreams:    datatypes.JSON([]byte("[]")),
			ProbeDesiredState:   models.ProbeDesiredStateArmed,
			ProbeStatus:         models.ProbeStatusArmed,
			Description:         "backend-payment WECHAT 渠道实例 (:9005)",
			CreatedBy:           1,
		},
		{
			AgentID:             agent.ID,
			ArchProcessID:       apClientTui.ID,
			EffectiveListenPort: 0,
			OverrideCmdPattern:  "client",
			AdvertiseIP:         "127.0.0.1",
			ExtraDownstreams:    datatypes.JSON([]byte("[]")),
			ProbeDesiredState:   models.ProbeDesiredStateArmed,
			ProbeStatus:         models.ProbeStatusArmed,
			Description:         "client TUI 客户端交互终端实例",
			CreatedBy:           1,
		},
	}

	for _, p := range procs {
		if err := db.Create(&p).Error; err != nil {
			log.Fatalf("挂载进程 [%d] 失败: %v", p.ArchProcessID, err)
		}
	}
	db.Model(&agent).Update("mounts_revision", 1)
	log.Printf("  • 已为 Agent [%s] 成功挂载 %d 个业务物理进程实例", agent.Name, len(procs))

	log.Println("==========================================================")
	log.Println(" 全部 Demo 进程与架构数据重构完成！")
	log.Println("==========================================================")
}
