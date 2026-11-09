package topology

import (
	"encoding/json"
	"fmt"
	"testing"

	"code-traefik/models"
	"gorm.io/datatypes"
)

func TestBuildTopologyGraph_Logical(t *testing.T) {
	db := setupIsolatedTopologyDB(t)

	// 1. 构造 2 个子系统
	sub1, err := CreateSubsystem(db, SubsystemInput{Code: "SUB_TRADE", Name: "交易子系统"})
	if err != nil {
		t.Fatalf("创建子系统 sub1 失败: %v", err)
	}
	sub2, err := CreateSubsystem(db, SubsystemInput{Code: "SUB_RISK", Name: "风控子系统"})
	if err != nil {
		t.Fatalf("创建子系统 sub2 失败: %v", err)
	}

	// 2. 构造 3 层自嵌套功能组: G1(L1) -> G2(L2) -> G3(L3)
	g1, err := CreateFunctionalGroup(db, FunctionalGroupInput{
		SubsystemID: sub1.ID,
		Code:        "G1",
		Name:        "一级交易组",
	})
	if err != nil {
		t.Fatalf("创建 G1 失败: %v", err)
	}

	g2, err := CreateFunctionalGroup(db, FunctionalGroupInput{
		SubsystemID: sub1.ID,
		ParentID:    &g1.ID,
		Code:        "G2",
		Name:        "二级清算组",
	})
	if err != nil {
		t.Fatalf("创建 G2 失败: %v", err)
	}

	g3, err := CreateFunctionalGroup(db, FunctionalGroupInput{
		SubsystemID: sub1.ID,
		ParentID:    &g2.ID,
		Code:        "G3",
		Name:        "三级对账组",
	})
	if err != nil {
		t.Fatalf("创建 G3 失败: %v", err)
	}

	// SUB2 的功能组
	gSub2, err := CreateFunctionalGroup(db, FunctionalGroupInput{
		SubsystemID: sub2.ID,
		Code:        "GRISK",
		Name:        "风控规则组",
	})
	if err != nil {
		t.Fatalf("创建 gSub2 失败: %v", err)
	}

	// 3. 构造 4 个功能模块
	m1, err := CreateModule(db, ModuleInput{
		GroupID: g1.ID,
		Code:    "MOD_ORDER",
		Name:    "订单模块",
	})
	if err != nil {
		t.Fatalf("创建 m1 失败: %v", err)
	}

	m2, err := CreateModule(db, ModuleInput{
		GroupID: g3.ID,
		Code:    "MOD_RECONCILE",
		Name:    "对账模块",
	})
	if err != nil {
		t.Fatalf("创建 m2 失败: %v", err)
	}

	m3, err := CreateModule(db, ModuleInput{
		GroupID: gSub2.ID,
		Code:    "MOD_RULE",
		Name:    "风控规则引擎",
	})
	if err != nil {
		t.Fatalf("创建 m3 失败: %v", err)
	}

	m4, err := CreateModule(db, ModuleInput{
		GroupID: gSub2.ID,
		Code:    "MOD_AUDIT",
		Name:    "审计归档模块",
	})
	if err != nil {
		t.Fatalf("创建 m4 失败: %v", err)
	}

	// 4. 提供服务
	sReconcile, err := CreateModuleService(db, ModuleServiceInput{
		ModuleID:    m2.ID,
		ServiceName: "reconcile.v1.ReconcileService",
	})
	if err != nil {
		t.Fatalf("创建 sReconcile 失败: %v", err)
	}

	sRule, err := CreateModuleService(db, ModuleServiceInput{
		ModuleID:    m3.ID,
		ServiceName: "risk.v1.RuleEngineService",
	})
	if err != nil {
		t.Fatalf("创建 sRule 失败: %v", err)
	}

	// 5. 依赖边
	// 5.1 跨子系统依赖: m1 -> m3 (MOD_ORDER -> MOD_RULE)
	_, err = CreateModuleDependency(db, ModuleDependencyInput{
		ConsumerModuleID: m1.ID,
		ServiceID:        &sRule.ID,
		IsExternal:       false,
	})
	if err != nil {
		t.Fatalf("创建跨子系统依赖失败: %v", err)
	}

	// 5.2 子系统内跨组依赖: m1 -> m2 (MOD_ORDER -> MOD_RECONCILE)
	_, err = CreateModuleDependency(db, ModuleDependencyInput{
		ConsumerModuleID: m1.ID,
		ServiceID:        &sReconcile.ID,
		IsExternal:       false,
	})
	if err != nil {
		t.Fatalf("创建同子系统跨组依赖失败: %v", err)
	}

	// 5.3 外部系统依赖: m1 -> WeChatPay
	_, err = CreateModuleDependency(db, ModuleDependencyInput{
		ConsumerModuleID: m1.ID,
		IsExternal:       true,
		ExternalSystem:   "WeChatPay",
		ExternalService:  "UnifiedOrder",
	})
	if err != nil {
		t.Fatalf("创建外部依赖失败: %v", err)
	}

	_ = m4 // 保留未依赖模块

	// 执行 BuildTopologyGraph
	graph, err := BuildTopologyGraph(db, TopologyViewLogical)
	if err != nil {
		t.Fatalf("BuildTopologyGraph 失败: %v", err)
	}

	// 断言 1: 顶层节点 depth == 0 直接为 SUBSYSTEM 与 EXTERNAL_CONTAINER（无冗余外层 SYSTEM 框）
	depth0Nodes := make([]GraphNode, 0)
	for _, n := range graph.Nodes {
		if n.Depth == 0 {
			depth0Nodes = append(depth0Nodes, n)
		}
	}
	if len(depth0Nodes) != 3 {
		t.Fatalf("期望顶层 depth=0 节点数为 3 (2个子系统+1个外部容器)，实际为 %d", len(depth0Nodes))
	}
	for _, n := range depth0Nodes {
		if n.Type != NodeTypeSubsystem && n.Type != NodeTypeExternalContainer {
			t.Fatalf("顶层 depth=0 出现非预期类型 %s, ID=%s", n.Type, n.ID)
		}
		if n.ParentID != "" || len(n.Ancestors) != 0 {
			t.Fatalf("顶层节点 parent_id 或 ancestors 必须为空: %+v", n)
		}
	}

	// 断言 2: parent_id 与 ancestors 严格精确
	nodeMap := make(map[string]GraphNode)
	for _, n := range graph.Nodes {
		nodeMap[n.ID] = n
	}

	// G3 的 parent 必须是 G2，ancestors 必须是 [sub_1, grp_1, grp_2]
	g3Node, ok := nodeMap[fmt.Sprintf("grp_%d", g3.ID)]
	if !ok {
		t.Fatalf("找不到 G3 节点")
	}
	expectedG3Parent := fmt.Sprintf("grp_%d", g2.ID)
	if g3Node.ParentID != expectedG3Parent {
		t.Fatalf("G3 parentID 错误: 期望 %s, 实际 %s", expectedG3Parent, g3Node.ParentID)
	}
	expectedG3Ancestors := []string{
		fmt.Sprintf("sub_%d", sub1.ID),
		fmt.Sprintf("grp_%d", g1.ID),
		fmt.Sprintf("grp_%d", g2.ID),
	}
	if len(g3Node.Ancestors) != len(expectedG3Ancestors) {
		t.Fatalf("G3 ancestors 长度错误: 期望 %v, 实际 %v", expectedG3Ancestors, g3Node.Ancestors)
	}
	for i, anc := range expectedG3Ancestors {
		if g3Node.Ancestors[i] != anc {
			t.Fatalf("G3 ancestor[%d] 错误: 期望 %s, 实际 %s", i, anc, g3Node.Ancestors[i])
		}
	}

	// M2 的 parent 必须是 G3，ancestors 包含 G3
	m2Node, ok := nodeMap[fmt.Sprintf("mod_%d", m2.ID)]
	if !ok {
		t.Fatalf("找不到 M2 节点")
	}
	expectedM2Parent := fmt.Sprintf("grp_%d", g3.ID)
	if m2Node.ParentID != expectedM2Parent {
		t.Fatalf("M2 parentID 错误: 期望 %s, 实际 %s", expectedM2Parent, m2Node.ParentID)
	}

	// 断言 3: edges 完整聚合 services[]，零 AGENT 节点
	for _, n := range graph.Nodes {
		if n.Type == "AGENT" {
			t.Fatalf("逻辑图中严禁出现 AGENT 节点: %+v", n)
		}
	}

	if len(graph.Edges) != 3 {
		t.Fatalf("期望 3 条依赖边，实际 %d 条", len(graph.Edges))
	}
	for _, e := range graph.Edges {
		if len(e.Services) == 0 {
			t.Fatalf("边 %s 的 services 列表不能为空", e.ID)
		}
	}
}

func TestBuildTopologyGraph_Physical_ZeroAgents_EnvOverlayAndFallbackDownstreams(t *testing.T) {
	db := setupIsolatedTopologyDB(t)

	// 1. 构造架构进程蓝图与模块
	pgrpCore := models.ArchProcessGroup{Code: "PGRP_CORE", Name: "交易核心组", SortOrder: 1}
	pgrpGW := models.ArchProcessGroup{Code: "PGRP_GW", Name: "接入网关组", SortOrder: 2}
	if err := db.Create(&pgrpCore).Error; err != nil {
		t.Fatalf("创建核心组失败: %v", err)
	}
	if err := db.Create(&pgrpGW).Error; err != nil {
		t.Fatalf("创建网关组失败: %v", err)
	}

	extraDownstreams, _ := json.Marshal([]string{"10.0.0.8:3306"})
	p1 := models.ArchProcess{
		GroupID:           pgrpCore.ID,
		Code:              "PROC_ORDER",
		Name:              "order-process",
		DefaultListenPort: 9001,
		DefaultCmdPattern: "./order-server",
	}
	p2 := models.ArchProcess{
		GroupID:                 pgrpGW.ID,
		Code:                    "PROC_GW",
		Name:                    "gateway-process",
		DefaultListenPort:       8080,
		DefaultCmdPattern:       "./gateway",
		DefaultExtraDownstreams: datatypes.JSON(extraDownstreams),
	}
	p3 := models.ArchProcess{
		GroupID:           pgrpCore.ID,
		Code:              "PROC_STOCK",
		Name:              "stock-process",
		DefaultListenPort: 9003,
		DefaultCmdPattern: "./stock-server",
	}
	db.Create(&p1)
	db.Create(&p2)
	db.Create(&p3)

	// 构造功能组与模块
	sub, _ := CreateSubsystem(db, SubsystemInput{Code: "SUB_DEMO", Name: "演示系统"})
	grp, _ := CreateFunctionalGroup(db, FunctionalGroupInput{SubsystemID: sub.ID, Code: "GRP_DEMO", Name: "演示组"})

	m1, _ := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_P1", Name: "订单服务", ArchProcessID: &p1.ID})
	m2, _ := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_P2", Name: "网关服务", ArchProcessID: &p2.ID})
	m3, _ := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_P3", Name: "库存服务", ArchProcessID: &p3.ID})
	mIso, _ := CreateModule(db, ModuleInput{GroupID: grp.ID, Code: "MOD_ISO", Name: "未部署孤立服务"})

	// 服务契约与依赖: m3 提供 stock-service, m1 依赖 stock-service
	svcStock, _ := CreateModuleService(db, ModuleServiceInput{ModuleID: m3.ID, ServiceName: "stock-service"})
	_, _ = CreateModuleDependency(db, ModuleDependencyInput{ConsumerModuleID: m1.ID, ServiceID: &svcStock.ID})

	// === 测试 1: 0 台 Agent 时 PHYSICAL 拓扑完整出图 (零 Agent 依赖) ===
	graphZero, err := BuildTopologyGraph(db, TopologyViewPhysical)
	if err != nil {
		t.Fatalf("0 台 Agent 时 BuildTopologyGraph 失败: %v", err)
	}
	zeroNodeMap := make(map[string]GraphNode)
	for _, n := range graphZero.Nodes {
		zeroNodeMap[n.ID] = n
	}

	p1Zero, ok := zeroNodeMap[fmt.Sprintf("proc_%d", p1.ID)]
	if !ok {
		t.Fatalf("0 台 Agent 时缺失 p1 进程节点")
	}
	if ep, ok := p1Zero.Meta["endpoint"].(string); !ok || ep != ":9001" {
		t.Fatalf("0 台 Agent 期望输出纯架构蓝图端点 :9001，实际为 %v", p1Zero.Meta["endpoint"])
	}
	if cnt, ok := p1Zero.Meta["mounted_shared_env_count"].(int); !ok || cnt != 0 {
		t.Fatalf("0 台 Agent 期望 mounted_shared_env_count 为 0，实际为 %v", p1Zero.Meta["mounted_shared_env_count"])
	}
	if isM, ok := p1Zero.Meta["is_mounted"].(bool); !ok || isM {
		t.Fatalf("0 台 Agent 期望 is_mounted 为 false")
	}

	// 孤立模块进入 undeployed_container
	isoNode, ok := zeroNodeMap[fmt.Sprintf("mod_%d", mIso.ID)]
	if !ok || isoNode.ParentID != "undeployed_container" {
		t.Fatalf("孤立模块期望归入 undeployed_container")
	}

	// === 测试 2: 创建 SIT 和 UAT 两台 SHARED Agent 并 1:1 挂载 ===
	agentSIT := models.Agent{Name: "agent-sit", Endpoint: "http://192.168.56.20:8090", Scope: models.ScopeShared, Status: models.AgentStatusOnline}
	agentUAT := models.Agent{Name: "agent-uat", Endpoint: "http://192.168.56.30:8090", Scope: models.ScopeShared, Status: models.AgentStatusOnline}
	db.Create(&agentSIT)
	db.Create(&agentUAT)

	// SIT 挂载 P1, P2(显式 IP), P3
	procSIT1 := models.Process{AgentID: agentSIT.ID, ArchProcessID: p1.ID, EffectiveListenPort: 9001, ProbeStatus: models.ProbeStatusReady}
	procSIT2 := models.Process{AgentID: agentSIT.ID, ArchProcessID: p2.ID, EffectiveListenPort: 8080, AdvertiseIP: "10.0.0.1", ProbeStatus: models.ProbeStatusReady}
	procSIT3 := models.Process{AgentID: agentSIT.ID, ArchProcessID: p3.ID, EffectiveListenPort: 9003, ProbeStatus: models.ProbeStatusReady}
	db.Create(&procSIT1)
	db.Create(&procSIT2)
	db.Create(&procSIT3)

	// UAT 挂载 P1 (本地覆盖 19001), P3
	ovPort := 19001
	procUAT1 := models.Process{AgentID: agentUAT.ID, ArchProcessID: p1.ID, OverrideListenPort: &ovPort, EffectiveListenPort: 19001, ProbeStatus: models.ProbeStatusReady}
	procUAT3 := models.Process{AgentID: agentUAT.ID, ArchProcessID: p3.ID, EffectiveListenPort: 9003, ProbeStatus: models.ProbeStatusReady}
	db.Create(&procUAT1)
	db.Create(&procUAT3)

	// 再次查纯蓝图 (未传 env_agent_id): p1.mounted_shared_env_count 应为 2
	graphShared, _ := BuildTopologyGraph(db, TopologyViewPhysical)
	for _, n := range graphShared.Nodes {
		if n.ID == fmt.Sprintf("proc_%d", p1.ID) {
			if cnt := n.Meta["mounted_shared_env_count"].(int); cnt != 2 {
				t.Fatalf("期望 P1 mounted_shared_env_count 为 2，实际为 %d", cnt)
			}
		}
	}

	// 叠加 SIT 环境 (env_agent_id = agentSIT.ID)
	graphOverlaySIT, err := BuildTopologyGraph(db, TopologyViewPhysical, agentSIT.ID)
	if err != nil {
		t.Fatalf("叠加 SIT 环境拓扑构建失败: %v", err)
	}
	sitNodeMap := make(map[string]GraphNode)
	for _, n := range graphOverlaySIT.Nodes {
		sitNodeMap[n.ID] = n
	}
	p1SIT := sitNodeMap[fmt.Sprintf("proc_%d", p1.ID)]
	if p1SIT.Meta["endpoint"] != "192.168.56.20:9001" || p1SIT.Meta["is_mounted"] != true {
		t.Fatalf("SIT 叠加期望端点为 192.168.56.20:9001 且已挂载，实际为 %v", p1SIT.Meta)
	}
	p2SIT := sitNodeMap[fmt.Sprintf("proc_%d", p2.ID)]
	if p2SIT.Meta["endpoint"] != "10.0.0.1:8080" {
		t.Fatalf("SIT 叠加 P2 显式 IP 期望端点为 10.0.0.1:8080，实际为 %v", p2SIT.Meta["endpoint"])
	}

	// === 测试 3: 创建 PRIVATE Agent 并绑定 FallbackAgentID = agentSIT.ID ===
	agentPriv := models.Agent{
		Name:            "agent-priv-alice",
		Endpoint:        "http://192.168.56.99:8090",
		Scope:           models.ScopePrivate,
		FallbackAgentID: &agentSIT.ID,
		Status:          models.AgentStatusOnline,
	}
	db.Create(&agentPriv)

	// 私有 Agent 仅挂载 P1 (不挂载 P3)
	procPriv1 := models.Process{AgentID: agentPriv.ID, ArchProcessID: p1.ID, EffectiveListenPort: 9001, ProbeStatus: models.ProbeStatusReady}
	db.Create(&procPriv1)

	// 编译 procPriv1 下游:
	// m1 依赖 stock-service (P3 提供)；由于同机未挂载 P3，自动通过 FallbackAgentID (SIT) 兜底解析为 192.168.56.20:9003
	// 严格排除 UAT 端点 (192.168.56.30:9003)
	downstreams, err := CompileProcessDownstreams(db, procPriv1.ID, nil, nil)
	if err != nil {
		t.Fatalf("CompileProcessDownstreams 失败: %v", err)
	}

	foundSITStock := false
	foundUATStock := false
	for _, ds := range downstreams {
		if ds.IP == "192.168.56.20" && ds.Port == 9003 {
			foundSITStock = true
		}
		if ds.IP == "192.168.56.30" {
			foundUATStock = true
		}
	}
	if !foundSITStock {
		t.Fatalf("期望从 FallbackAgentID(SIT) 获得 192.168.56.20:9003，但未找到: %+v", downstreams)
	}
	if foundUATStock {
		t.Fatalf("严禁混入非绑定环境 UAT 的端点！实际包含: %+v", downstreams)
	}
	_ = m2
}

func TestBuildTopologyGraph_StructureVersion_IgnoresEdgesMetaAndEnv(t *testing.T) {
	db := setupIsolatedTopologyDB(t)

	sub1, err := CreateSubsystem(db, SubsystemInput{Code: "SUB_SV1", Name: "子系统1"})
	if err != nil {
		t.Fatalf("创建 sub1 失败: %v", err)
	}
	grp1, err := CreateFunctionalGroup(db, FunctionalGroupInput{SubsystemID: sub1.ID, Code: "GRP_SV1", Name: "组1"})
	if err != nil {
		t.Fatalf("创建 grp1 失败: %v", err)
	}
	grp2, err := CreateFunctionalGroup(db, FunctionalGroupInput{SubsystemID: sub1.ID, Code: "GRP_SV2", Name: "组2"})
	if err != nil {
		t.Fatalf("创建 grp2 失败: %v", err)
	}

	pgrp := models.ArchProcessGroup{Code: "PGRP_SV", Name: "物理组1", SortOrder: 1}
	if err := db.Create(&pgrp).Error; err != nil {
		t.Fatalf("创建 pgrp 失败: %v", err)
	}
	ap1 := models.ArchProcess{GroupID: pgrp.ID, Code: "PROC_SV1", Name: "proc-sv1", DefaultListenPort: 9101}
	if err := db.Create(&ap1).Error; err != nil {
		t.Fatalf("创建 ap1 失败: %v", err)
	}

	m1, err := CreateModule(db, ModuleInput{GroupID: grp1.ID, Code: "MOD_SV1", Name: "模块1", ArchProcessID: &ap1.ID})
	if err != nil {
		t.Fatalf("创建 m1 失败: %v", err)
	}
	m2, err := CreateModule(db, ModuleInput{GroupID: grp1.ID, Code: "MOD_SV2", Name: "模块2", ArchProcessID: &ap1.ID})
	if err != nil {
		t.Fatalf("创建 m2 失败: %v", err)
	}

	gBaseLog, err := BuildTopologyGraph(db, TopologyViewLogical)
	if err != nil {
		t.Fatalf("BuildTopologyGraph Logical 失败: %v", err)
	}
	if gBaseLog.StructureVersion == "" {
		t.Fatalf("期望 StructureVersion 非空")
	}

	// 1. 新增内部依赖边 + 修改模块名称/描述 -> structure_version 不变，model_version 改变
	svc2, err := CreateModuleService(db, ModuleServiceInput{ModuleID: m2.ID, ServiceName: "sv.v1.Svc2"})
	if err != nil {
		t.Fatalf("创建 svc2 失败: %v", err)
	}
	if _, err := CreateModuleDependency(db, ModuleDependencyInput{ConsumerModuleID: m1.ID, ServiceID: &svc2.ID}); err != nil {
		t.Fatalf("创建依赖失败: %v", err)
	}
	if err := db.Model(&models.Module{}).Where("id = ?", m1.ID).Updates(map[string]any{
		"name":        "模块1-改名后",
		"description": "新的描述信息",
	}).Error; err != nil {
		t.Fatalf("更新模块名称描述失败: %v", err)
	}

	gAfterEdgeMeta, err := BuildTopologyGraph(db, TopologyViewLogical)
	if err != nil {
		t.Fatalf("BuildTopologyGraph Logical 失败: %v", err)
	}
	if gAfterEdgeMeta.StructureVersion != gBaseLog.StructureVersion {
		t.Fatalf("新增内部边或改名/描述不应改变 structure_version: before=%s, after=%s", gBaseLog.StructureVersion, gAfterEdgeMeta.StructureVersion)
	}
	if gAfterEdgeMeta.ModelVersion == gBaseLog.ModelVersion {
		t.Fatalf("新增内部边或改名/描述应改变 model_version: before=%s, after=%s", gBaseLog.ModelVersion, gAfterEdgeMeta.ModelVersion)
	}

	// 2. PHYSICAL 叠加不同 env_agent_id -> structure_version 不变，model_version 改变
	gPhysBase, err := BuildTopologyGraph(db, TopologyViewPhysical)
	if err != nil {
		t.Fatalf("BuildTopologyGraph Physical 失败: %v", err)
	}
	agentEnv := models.Agent{Name: "agent-sv-env", Endpoint: "http://192.168.56.55:8090", Scope: models.ScopeShared, Status: models.AgentStatusOnline}
	if err := db.Create(&agentEnv).Error; err != nil {
		t.Fatalf("创建 agentEnv 失败: %v", err)
	}
	procInst := models.Process{AgentID: agentEnv.ID, ArchProcessID: ap1.ID, EffectiveListenPort: 19101, AdvertiseIP: "10.9.9.9", ProbeStatus: models.ProbeStatusReady}
	if err := db.Create(&procInst).Error; err != nil {
		t.Fatalf("创建 procInst 失败: %v", err)
	}
	gPhysEnv, err := BuildTopologyGraph(db, TopologyViewPhysical, agentEnv.ID)
	if err != nil {
		t.Fatalf("BuildTopologyGraph Physical env 失败: %v", err)
	}
	if gPhysEnv.StructureVersion != gPhysBase.StructureVersion {
		t.Fatalf("PHYSICAL 叠加 env_agent_id 不应改变 structure_version: base=%s, env=%s", gPhysBase.StructureVersion, gPhysEnv.StructureVersion)
	}
	if gPhysEnv.ModelVersion == gPhysBase.ModelVersion {
		t.Fatalf("PHYSICAL 叠加 env_agent_id 应改变 model_version: base=%s, env=%s", gPhysBase.ModelVersion, gPhysEnv.ModelVersion)
	}

	// 3. 移动模块 m2 至 grp2 -> structure_version 改变
	if err := db.Model(&models.Module{}).Where("id = ?", m2.ID).Update("group_id", grp2.ID).Error; err != nil {
		t.Fatalf("移动模块失败: %v", err)
	}
	gAfterMove, err := BuildTopologyGraph(db, TopologyViewLogical)
	if err != nil {
		t.Fatalf("BuildTopologyGraph AfterMove 失败: %v", err)
	}
	if gAfterMove.StructureVersion == gBaseLog.StructureVersion {
		t.Fatalf("移动模块改变 parent_id 后 structure_version 必须改变")
	}

	// 4. 新增模块 m3 -> structure_version 改变
	m3, err := CreateModule(db, ModuleInput{GroupID: grp2.ID, Code: "MOD_SV3", Name: "模块3"})
	if err != nil {
		t.Fatalf("创建 m3 失败: %v", err)
	}
	gAfterAdd, err := BuildTopologyGraph(db, TopologyViewLogical)
	if err != nil {
		t.Fatalf("BuildTopologyGraph AfterAdd 失败: %v", err)
	}
	if gAfterAdd.StructureVersion == gAfterMove.StructureVersion {
		t.Fatalf("新增模块后 structure_version 必须改变")
	}

	// 5. 删除模块 m3 -> structure_version 恢复为 gAfterMove.StructureVersion
	if err := db.Delete(&models.Module{}, m3.ID).Error; err != nil {
		t.Fatalf("删除 m3 失败: %v", err)
	}
	gAfterDel, err := BuildTopologyGraph(db, TopologyViewLogical)
	if err != nil {
		t.Fatalf("BuildTopologyGraph AfterDel 失败: %v", err)
	}
	if gAfterDel.StructureVersion != gAfterMove.StructureVersion {
		t.Fatalf("删除新增模块后 structure_version 应恢复: got=%s, want=%s", gAfterDel.StructureVersion, gAfterMove.StructureVersion)
	}
}

func TestBuildTopologyGraph_ModuleOwnerIDInMeta_LogicalAndPhysical(t *testing.T) {
	db := setupIsolatedTopologyDB(t)

	ownerID := uint(42)
	user := models.User{
		ID:       ownerID,
		Username: "owner42",
		Name:     "负责人42",
	}
	_ = db.Create(&user).Error

	sub, err := CreateSubsystem(db, SubsystemInput{Code: "SUB_OWN", Name: "归属子系统", OwnerID: &ownerID})
	if err != nil {
		t.Fatalf("创建子系统失败: %v", err)
	}
	grp, err := CreateFunctionalGroup(db, FunctionalGroupInput{SubsystemID: sub.ID, Code: "GRP_OWN", Name: "归属功能组"})
	if err != nil {
		t.Fatalf("创建功能组失败: %v", err)
	}
	pgrp := models.ArchProcessGroup{Code: "PGRP_OWN", Name: "物理归属组", SortOrder: 1}
	_ = db.Create(&pgrp).Error
	ap := models.ArchProcess{GroupID: pgrp.ID, Code: "PROC_OWN", Name: "proc-own", DefaultListenPort: 9201}
	_ = db.Create(&ap).Error

	modWithOwner, err := CreateModule(db, ModuleInput{
		GroupID:       grp.ID,
		Code:          "MOD_WITH_OWNER",
		Name:          "有负责人模块",
		OwnerID:       &ownerID,
		ArchProcessID: &ap.ID,
	})
	if err != nil {
		t.Fatalf("创建有负责人模块失败: %v", err)
	}
	modNoOwnerUndeployed, err := CreateModule(db, ModuleInput{
		GroupID: grp.ID,
		Code:    "MOD_NO_OWNER",
		Name:    "无负责人模块",
	})
	if err != nil {
		t.Fatalf("创建无负责人模块失败: %v", err)
	}

	for _, view := range []string{TopologyViewLogical, TopologyViewPhysical} {
		g, err := BuildTopologyGraph(db, view)
		if err != nil {
			t.Fatalf("BuildTopologyGraph(%s) 失败: %v", view, err)
		}
		nodeMap := make(map[string]GraphNode)
		for _, n := range g.Nodes {
			nodeMap[n.ID] = n
		}

		nWith, ok := nodeMap[fmt.Sprintf("mod_%d", modWithOwner.ID)]
		if !ok {
			t.Fatalf("[%s] 未找到有负责人模块节点", view)
		}
		gotOwner, hasOwner := nWith.Meta["owner_id"]
		if !hasOwner || gotOwner != ownerID {
			t.Fatalf("[%s] 有负责人模块 meta.owner_id 期望为 %v，实际为 %v (has=%v)", view, ownerID, gotOwner, hasOwner)
		}

		nWithout, ok := nodeMap[fmt.Sprintf("mod_%d", modNoOwnerUndeployed.ID)]
		if !ok {
			t.Fatalf("[%s] 未找到无负责人模块节点", view)
		}
		if val, exists := nWithout.Meta["owner_id"]; exists && val != nil {
			t.Fatalf("[%s] 未设置负责人时 meta.owner_id 应省略，实际为 %v", view, val)
		}
	}
}
