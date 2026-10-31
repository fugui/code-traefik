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

func TestBuildTopologyGraph_Physical_NoAgentAndEffectiveIP(t *testing.T) {
	db := setupIsolatedTopologyDB(t)

	// 1. 构造公共 SHARED Agent，宿主端点为 http://192.168.56.20:8090
	agent := models.Agent{
		Name:     "agent-host-1",
		Endpoint: "http://192.168.56.20:8090",
		Scope:    models.ScopeShared,
		Status:   models.AgentStatusOnline,
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatalf("创建 Agent 失败: %v", err)
	}

	// 2. 构造进程 1: 留空 AdvertiseIP，期望回退为 192.168.56.20:9001
	p1 := models.Process{
		AgentID:     agent.ID,
		Name:        "order-process",
		GroupName:   "交易核心组",
		ListenPort:  9001,
		AdvertiseIP: "", // 留空
		CmdPattern:  "./order-server",
	}
	if err := db.Create(&p1).Error; err != nil {
		t.Fatalf("创建进程 p1 失败: %v", err)
	}

	// 3. 构造进程 2: 显式 AdvertiseIP = 10.0.0.1，带 ExtraDownstreams
	extraDownstreams, _ := json.Marshal([]string{"10.0.0.8:3306"})
	p2 := models.Process{
		AgentID:          agent.ID,
		Name:             "gateway-process",
		GroupName:        "接入网关组",
		ListenPort:       8080,
		AdvertiseIP:      "10.0.0.1",
		CmdPattern:       "./gateway",
		ExtraDownstreams: datatypes.JSON(extraDownstreams),
	}
	if err := db.Create(&p2).Error; err != nil {
		t.Fatalf("创建进程 p2 失败: %v", err)
	}

	// 4. 构造子系统与功能组
	sub, err := CreateSubsystem(db, SubsystemInput{Code: "SUB_DEMO", Name: "演示系统"})
	if err != nil {
		t.Fatalf("创建子系统失败: %v", err)
	}
	grp, err := CreateFunctionalGroup(db, FunctionalGroupInput{
		SubsystemID: sub.ID,
		Code:        "GRP_DEMO",
		Name:        "演示组",
	})
	if err != nil {
		t.Fatalf("创建功能组失败: %v", err)
	}

	// 5. 构造 3 个模块: 2 个绑定公共进程，1 个未部署孤立模块
	hasProc := true
	m1, err := CreateModule(db, ModuleInput{
		GroupID:      grp.ID,
		Code:         "MOD_P1",
		Name:         "订单服务",
		ProcessID:    &p1.ID,
		HasProcessID: hasProc,
	})
	if err != nil {
		t.Fatalf("创建 m1 失败: %v", err)
	}

	m2, err := CreateModule(db, ModuleInput{
		GroupID:      grp.ID,
		Code:         "MOD_P2",
		Name:         "网关服务",
		ProcessID:    &p2.ID,
		HasProcessID: hasProc,
	})
	if err != nil {
		t.Fatalf("创建 m2 失败: %v", err)
	}

	mIso, err := CreateModule(db, ModuleInput{
		GroupID: grp.ID,
		Code:    "MOD_ISO",
		Name:    "未部署孤立服务",
	})
	if err != nil {
		t.Fatalf("创建孤立模块 mIso 失败: %v", err)
	}
	_ = m1
	_ = m2
	_ = mIso

	// 执行 BuildTopologyGraph (PHYSICAL)
	graph, err := BuildTopologyGraph(db, TopologyViewPhysical)
	if err != nil {
		t.Fatalf("BuildTopologyGraph PHYSICAL 失败: %v", err)
	}

	// 断言 1: 物理图节点仅含 PROCESS_GROUP、PROCESS、MODULE、UNDEPLOYED_CONTAINER、EXTERNAL_*
	// 绝对不存在 AGENT 节点且 meta 不含 agent_id
	for _, n := range graph.Nodes {
		if n.Type == "AGENT" {
			t.Fatalf("物理图中严禁出现 AGENT 节点类型: %+v", n)
		}
		if _, exists := n.Meta["agent_id"]; exists {
			t.Fatalf("物理图节点 %s meta 严禁包含 agent_id", n.ID)
		}
	}

	nodeMap := make(map[string]GraphNode)
	for _, n := range graph.Nodes {
		nodeMap[n.ID] = n
	}

	// 断言 2: 留空 AdvertiseIP 的进程正确回退推导为 "192.168.56.20:9001"
	p1Node, ok := nodeMap[fmt.Sprintf("proc_%d", p1.ID)]
	if !ok {
		t.Fatalf("找不到 p1 进程节点")
	}
	if endpoint, ok := p1Node.Meta["endpoint"].(string); !ok || endpoint != "192.168.56.20:9001" {
		t.Fatalf("p1 期望 EffectiveIP endpoint 为 192.168.56.20:9001，实际为 %v", p1Node.Meta["endpoint"])
	}

	// 显式 AdvertiseIP 的进程 2 必须为 "10.0.0.1:8080"
	p2Node, ok := nodeMap[fmt.Sprintf("proc_%d", p2.ID)]
	if !ok {
		t.Fatalf("找不到 p2 进程节点")
	}
	if endpoint, ok := p2Node.Meta["endpoint"].(string); !ok || endpoint != "10.0.0.1:8080" {
		t.Fatalf("p2 期望 endpoint 为 10.0.0.1:8080，实际为 %v", p2Node.Meta["endpoint"])
	}

	// 断言 3: 未绑定公共进程的孤立模块归入 UNDEPLOYED_CONTAINER
	undeployedContainer, ok := nodeMap["undeployed_container"]
	if !ok {
		t.Fatalf("缺少 undeployed_container 根容器")
	}
	if undeployedContainer.Type != NodeTypeUndeployedContainer || undeployedContainer.Depth != 0 {
		t.Fatalf("undeployed_container 属性错误: %+v", undeployedContainer)
	}

	mIsoNode, ok := nodeMap[fmt.Sprintf("mod_%d", mIso.ID)]
	if !ok {
		t.Fatalf("找不到孤立模块节点")
	}
	if mIsoNode.ParentID != "undeployed_container" || mIsoNode.Depth != 1 {
		t.Fatalf("孤立模块必须挂载在 undeployed_container 下，实际 parentID=%s depth=%d", mIsoNode.ParentID, mIsoNode.Depth)
	}

	// 断言 4: ExtraDownstreams 生成 EXTERNAL_ENDPOINT 节点和边
	extEpNode, ok := nodeMap["ext_ep_10.0.0.8:3306"]
	if !ok {
		t.Fatalf("未找到 Process ExtraDownstreams 生成的 ext_ep_10.0.0.8:3306 节点")
	}
	if extEpNode.Type != NodeTypeExternalEndpoint || extEpNode.ParentID != "ext_container" {
		t.Fatalf("外部端点节点属性错误: %+v", extEpNode)
	}

	foundExtraEdge := false
	for _, e := range graph.Edges {
		if e.SourceNodeID == fmt.Sprintf("proc_%d", p2.ID) && e.TargetNodeID == "ext_ep_10.0.0.8:3306" {
			foundExtraEdge = true
			break
		}
	}
	if !foundExtraEdge {
		t.Fatalf("未找到从 proc_2 到 ext_ep_10.0.0.8:3306 的外部端点边")
	}
}
