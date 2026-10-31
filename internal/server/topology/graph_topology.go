package topology

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	"code-traefik/models"
	"gorm.io/gorm"
)

// GraphNodeType 全系统分层拓扑节点类型枚举 (零 Agent)
type GraphNodeType string

const (
	NodeTypeSubsystem             GraphNodeType = "SUBSYSTEM"
	NodeTypeFunctionalGroup       GraphNodeType = "FUNCTIONAL_GROUP"
	NodeTypeModule                GraphNodeType = "MODULE"
	NodeTypeExternalContainer     GraphNodeType = "EXTERNAL_CONTAINER"
	NodeTypeExternalSystem        GraphNodeType = "EXTERNAL_SYSTEM"
	NodeTypeProcessGroup          GraphNodeType = "PROCESS_GROUP"
	NodeTypeProcess               GraphNodeType = "PROCESS"
	NodeTypeUndeployedContainer   GraphNodeType = "UNDEPLOYED_CONTAINER"
	NodeTypeExternalEndpoint      GraphNodeType = "EXTERNAL_ENDPOINT"
	NodeTypeUnregisteredInterface GraphNodeType = "UNREGISTERED_INTERFACE" // 前端一致性叠加态按需动态注入
)

// GraphNode 复合图节点传输对象
type GraphNode struct {
	ID        string         `json:"id"`                  // 唯一标识: "sub_1", "grp_10", "mod_101", "pgrp_交易核心", "proc_7"
	Type      GraphNodeType  `json:"type"`                // 节点类型枚举（严禁出现 AGENT）
	ParentID  string         `json:"parent_id,omitempty"` // 父节点 ID，顶层节点 (depth=0) 为空字符串
	Ancestors []string       `json:"ancestors"`           // 从顶层祖先至直接父节点的有序 ID 数组，加速前端 rep() 计算
	Depth     int            `json:"depth"`               // 所在层级深度 (0: 顶层子系统/进程组/虚拟根容器)
	EntityID  uint64         `json:"entity_id,omitempty"` // 对应业务数据库主键 (Subsystem.ID, Module.ID, Process.ID)
	Code      string         `json:"code,omitempty"`      // 业务编码 (如 "TRADE_SYS", "MOD_ORDER")
	Name      string         `json:"name"`                // 展示名称
	SortOrder int            `json:"sort_order"`          // 排序权重，供 ELK considerModelOrder 保持排版稳定
	Meta      map[string]any `json:"meta,omitempty"`      // 视图专有元数据 (如 endpoint, listen_port, provide_count, owner_name；严禁含 agent_id)
}

// EdgeServiceItem 底层服务契约明细
type EdgeServiceItem struct {
	ServiceID    *uint64 `json:"service_id,omitempty"`
	ServiceName  string  `json:"service_name"`
	DependencyID uint64  `json:"dependency_id,omitempty"`
	Description  string  `json:"description,omitempty"`
}

// GraphEdge 原子叶子依赖边传输对象
type GraphEdge struct {
	ID             string            `json:"id"`                      // 如 "edge_mod_101_mod_102" 或 "edge_mod_101_ext_sys_WeChatPay"
	Kind           string            `json:"kind"`                    // CROSS_PROCESS | INTRA_PROCESS | EXTERNAL_UNREGISTERED | UNDEPLOYED
	SourceNodeID   string            `json:"source_node_id"`          // 源叶子节点 ID ("mod_101" 或 "proc_7")
	TargetNodeID   string            `json:"target_node_id"`          // 目标叶子节点 ID ("mod_102" / "ext_sys_WeChatPay" / "ext_ep_10.0.0.8:3306")
	SrcModuleID    uint64            `json:"src_module_id,omitempty"` // 关联源功能模块 ID
	DstModuleID    *uint64           `json:"dst_module_id,omitempty"` // 关联目标功能模块 ID (外部为 nil)
	SrcProcessID   *uint64           `json:"src_process_id,omitempty"`
	DstProcessID   *uint64           `json:"dst_process_id,omitempty"`
	Services       []EdgeServiceItem `json:"services"` // 聚合的底层服务调用清单
	ExternalSystem string            `json:"external_system,omitempty"`
}

// TopologyGraphDTO 全量架构拓扑图响应载荷
type TopologyGraphDTO struct {
	View         string      `json:"view"`          // LOGICAL | PHYSICAL
	ModelVersion string      `json:"model_version"` // 基于规范化 nodes+edges 计算的 SHA-256 内容指纹 (如 "v1-LOGICAL-9f86d081884c7d65")
	Nodes        []GraphNode `json:"nodes"`
	Edges        []GraphEdge `json:"edges"`
}

// extractHostFromEndpoint 提取 Agent 端点中的 Hostname/IP（兼容 http(s):// 或裸 host:port）
func extractHostFromEndpoint(ep string) string {
	ep = strings.TrimSpace(ep)
	if ep == "" {
		return "127.0.0.1"
	}
	if strings.Contains(ep, "://") {
		u, err := url.Parse(ep)
		if err == nil && u.Hostname() != "" {
			return u.Hostname()
		}
	}
	if host, _, err := net.SplitHostPort(ep); err == nil && host != "" {
		return host
	}
	return ep
}

// BuildTopologyGraph 构建全系统分层复合架构拓扑图 (支持 LOGICAL / PHYSICAL 视角)
func BuildTopologyGraph(db *gorm.DB, view string) (*TopologyGraphDTO, error) {
	if view != TopologyViewLogical && view != TopologyViewPhysical {
		return nil, ErrInvalidTopologyView
	}

	dto := &TopologyGraphDTO{
		View:  view,
		Nodes: make([]GraphNode, 0),
		Edges: make([]GraphEdge, 0),
	}

	if view == TopologyViewLogical {
		if err := buildLogicalTopologyGraph(db, dto); err != nil {
			return nil, err
		}
	} else {
		if err := buildPhysicalTopologyGraph(db, dto); err != nil {
			return nil, err
		}
	}

	// 规范化确定性排序
	sort.Slice(dto.Nodes, func(i, j int) bool {
		if dto.Nodes[i].Depth != dto.Nodes[j].Depth {
			return dto.Nodes[i].Depth < dto.Nodes[j].Depth
		}
		if dto.Nodes[i].Type != dto.Nodes[j].Type {
			return dto.Nodes[i].Type < dto.Nodes[j].Type
		}
		return dto.Nodes[i].ID < dto.Nodes[j].ID
	})

	sort.Slice(dto.Edges, func(i, j int) bool {
		if dto.Edges[i].SourceNodeID != dto.Edges[j].SourceNodeID {
			return dto.Edges[i].SourceNodeID < dto.Edges[j].SourceNodeID
		}
		return dto.Edges[i].TargetNodeID < dto.Edges[j].TargetNodeID
	})

	for i := range dto.Edges {
		sort.Slice(dto.Edges[i].Services, func(a, b int) bool {
			if dto.Edges[i].Services[a].ServiceName != dto.Edges[i].Services[b].ServiceName {
				return dto.Edges[i].Services[a].ServiceName < dto.Edges[i].Services[b].ServiceName
			}
			return dto.Edges[i].Services[a].DependencyID < dto.Edges[i].Services[b].DependencyID
		})
	}

	// 计算 SHA-256 强内容指纹作为 ModelVersion 与 ETag
	canonicalJSON, err := json.Marshal(struct {
		Nodes []GraphNode `json:"nodes"`
		Edges []GraphEdge `json:"edges"`
	}{
		Nodes: dto.Nodes,
		Edges: dto.Edges,
	})
	if err != nil {
		return nil, fmt.Errorf("序列化拓扑图内容计算指纹失败: %w", err)
	}

	sum := sha256.Sum256(canonicalJSON)
	dto.ModelVersion = fmt.Sprintf("v1-%s-%x", view, sum[:8])

	return dto, nil
}

// buildLogicalTopologyGraph 逻辑架构视角分层树与叶子依赖边构建
func buildLogicalTopologyGraph(db *gorm.DB, dto *TopologyGraphDTO) error {
	var subsystems []models.Subsystem
	if err := db.Order("id ASC").Find(&subsystems).Error; err != nil {
		return err
	}

	var groups []models.FunctionalGroup
	if err := db.Order("level ASC, sort_order ASC, id ASC").Find(&groups).Error; err != nil {
		return err
	}

	var modules []models.Module
	if err := db.Order("id ASC").Find(&modules).Error; err != nil {
		return err
	}

	var services []models.ModuleService
	if err := db.Find(&services).Error; err != nil {
		return err
	}
	svcProvideMap := make(map[uint64]uint64) // serviceID -> moduleID
	svcNameMap := make(map[uint64]string)    // serviceID -> serviceName
	provideCountMap := make(map[uint64]int)  // moduleID -> count
	for _, svc := range services {
		svcProvideMap[svc.ID] = svc.ModuleID
		svcNameMap[svc.ID] = svc.ServiceName
		provideCountMap[svc.ModuleID]++
	}

	var dependencies []models.ModuleDependency
	if err := db.Preload("Service").Find(&dependencies).Error; err != nil {
		return err
	}

	// 1. 构建顶层 Subsystem 节点 (depth=0)
	for idx, sub := range subsystems {
		dto.Nodes = append(dto.Nodes, GraphNode{
			ID:        fmt.Sprintf("sub_%d", sub.ID),
			Type:      NodeTypeSubsystem,
			ParentID:  "",
			Ancestors: []string{},
			Depth:     0,
			EntityID:  sub.ID,
			Code:      sub.Code,
			Name:      sub.Name,
			SortOrder: idx + 1,
			Meta: map[string]any{
				"description": sub.Description,
				"owner_id":    sub.OwnerID,
			},
		})
	}

	// 2. 构建 FunctionalGroup 节点树
	groupMap := make(map[uint64]models.FunctionalGroup)
	groupDepthMap := make(map[uint64]int)
	groupAncestorsMap := make(map[uint64][]string)
	for _, g := range groups {
		groupMap[g.ID] = g
	}

	for _, g := range groups {
		var parentID string
		var depth int
		var ancestors []string

		if g.ParentID == nil || *g.ParentID == 0 {
			parentID = fmt.Sprintf("sub_%d", g.SubsystemID)
			depth = 1
			ancestors = []string{parentID}
		} else {
			parentGroupID := *g.ParentID
			parentID = fmt.Sprintf("grp_%d", parentGroupID)
			pDepth := groupDepthMap[parentGroupID]
			if pDepth == 0 {
				pDepth = 1
			}
			depth = pDepth + 1
			pAncestors := groupAncestorsMap[parentGroupID]
			ancestors = make([]string, 0, len(pAncestors)+1)
			ancestors = append(ancestors, pAncestors...)
			ancestors = append(ancestors, parentID)
		}

		groupDepthMap[g.ID] = depth
		groupAncestorsMap[g.ID] = ancestors

		dto.Nodes = append(dto.Nodes, GraphNode{
			ID:        fmt.Sprintf("grp_%d", g.ID),
			Type:      NodeTypeFunctionalGroup,
			ParentID:  parentID,
			Ancestors: ancestors,
			Depth:     depth,
			EntityID:  g.ID,
			Code:      g.Code,
			Name:      g.Name,
			SortOrder: g.SortOrder,
			Meta: map[string]any{
				"level":        g.Level,
				"description":  g.Description,
				"subsystem_id": g.SubsystemID,
			},
		})
	}

	// 3. 构建 Module 叶子节点
	modMap := make(map[uint64]models.Module)
	for idx, m := range modules {
		modMap[m.ID] = m

		var parentID string
		var depth int
		var ancestors []string

		if g, exists := groupMap[m.GroupID]; exists {
			parentID = fmt.Sprintf("grp_%d", g.ID)
			depth = groupDepthMap[g.ID] + 1
			ancestors = make([]string, 0, len(groupAncestorsMap[g.ID])+1)
			ancestors = append(ancestors, groupAncestorsMap[g.ID]...)
			ancestors = append(ancestors, parentID)
		} else {
			// 防御性回退至所属子系统
			parentID = fmt.Sprintf("sub_%d", m.SubsystemID)
			depth = 1
			ancestors = []string{parentID}
		}

		isDeployed := m.ProcessID != nil && *m.ProcessID > 0
		meta := map[string]any{
			"is_deployed":   isDeployed,
			"provide_count": provideCountMap[m.ID],
			"description":   m.Description,
			"subsystem_id":  m.SubsystemID,
			"group_id":      m.GroupID,
		}
		if isDeployed {
			meta["process_id"] = *m.ProcessID
		}

		dto.Nodes = append(dto.Nodes, GraphNode{
			ID:        fmt.Sprintf("mod_%d", m.ID),
			Type:      NodeTypeModule,
			ParentID:  parentID,
			Ancestors: ancestors,
			Depth:     depth,
			EntityID:  m.ID,
			Code:      m.Code,
			Name:      m.Name,
			SortOrder: idx + 1,
			Meta:      meta,
		})
	}

	// 4. 收集外部依赖节点 (EXTERNAL_CONTAINER + EXTERNAL_SYSTEM)
	externalSystems := make(map[string]bool)
	for _, dep := range dependencies {
		if dep.IsExternal && strings.TrimSpace(dep.ExternalSystem) != "" {
			externalSystems[strings.TrimSpace(dep.ExternalSystem)] = true
		}
	}

	if len(externalSystems) > 0 {
		dto.Nodes = append(dto.Nodes, GraphNode{
			ID:        "ext_container",
			Type:      NodeTypeExternalContainer,
			ParentID:  "",
			Ancestors: []string{},
			Depth:     0,
			Name:      "外部依赖系统",
			SortOrder: 9999,
		})

		extSysNames := make([]string, 0, len(externalSystems))
		for name := range externalSystems {
			extSysNames = append(extSysNames, name)
		}
		sort.Strings(extSysNames)

		for idx, name := range extSysNames {
			dto.Nodes = append(dto.Nodes, GraphNode{
				ID:        fmt.Sprintf("ext_sys_%s", name),
				Type:      NodeTypeExternalSystem,
				ParentID:  "ext_container",
				Ancestors: []string{"ext_container"},
				Depth:     1,
				Name:      name,
				SortOrder: idx + 1,
			})
		}
	}

	// 5. 聚合逻辑叶子依赖边
	type edgeKey struct {
		src string
		dst string
	}
	edgeMap := make(map[edgeKey]*GraphEdge)

	for _, dep := range dependencies {
		srcNodeID := fmt.Sprintf("mod_%d", dep.ConsumerModuleID)
		var dstNodeID string
		var kind string
		var dstModID *uint64
		var extSys string
		var srcProcID, dstProcID *uint64

		srcMod, ok := modMap[dep.ConsumerModuleID]
		if !ok {
			continue
		}
		if srcMod.ProcessID != nil && *srcMod.ProcessID > 0 {
			srcProcID = srcMod.ProcessID
		}

		if dep.IsExternal {
			sysName := strings.TrimSpace(dep.ExternalSystem)
			if sysName == "" {
				continue
			}
			dstNodeID = fmt.Sprintf("ext_sys_%s", sysName)
			kind = EdgeKindExternalUnregistered
			extSys = sysName
		} else {
			if dep.ServiceID == nil {
				continue
			}
			svcIDVal := *dep.ServiceID
			providerID, ok := svcProvideMap[svcIDVal]
			if !ok {
				if dep.ProviderModuleID != nil {
					providerID = *dep.ProviderModuleID
				} else {
					continue
				}
			}
			dstNodeID = fmt.Sprintf("mod_%d", providerID)
			dstModID = &providerID
			dstMod, ok2 := modMap[providerID]
			if ok2 && dstMod.ProcessID != nil && *dstMod.ProcessID > 0 {
				dstProcID = dstMod.ProcessID
			}

			if srcProcID != nil && dstProcID != nil && *srcProcID == *dstProcID {
				kind = EdgeKindIntraProcess
			} else {
				kind = EdgeKindCrossProcess
			}
		}

		svcName := ""
		if dep.Service != nil && dep.Service.ServiceName != "" {
			svcName = dep.Service.ServiceName
		} else if dep.ServiceID != nil {
			svcName = svcNameMap[*dep.ServiceID]
		}
		if svcName == "" && dep.IsExternal {
			svcName = dep.ExternalService
			if svcName == "" {
				svcName = dep.ExternalSystem
			}
		}

		key := edgeKey{src: srcNodeID, dst: dstNodeID}
		edge, exists := edgeMap[key]
		if !exists {
			edge = &GraphEdge{
				ID:             fmt.Sprintf("edge_%s_%s", srcNodeID, dstNodeID),
				Kind:           kind,
				SourceNodeID:   srcNodeID,
				TargetNodeID:   dstNodeID,
				SrcModuleID:    dep.ConsumerModuleID,
				DstModuleID:    dstModID,
				SrcProcessID:   srcProcID,
				DstProcessID:   dstProcID,
				Services:       make([]EdgeServiceItem, 0),
				ExternalSystem: extSys,
			}
			edgeMap[key] = edge
		}

		edge.Services = append(edge.Services, EdgeServiceItem{
			ServiceID:    dep.ServiceID,
			ServiceName:  svcName,
			DependencyID: dep.ID,
			Description:  dep.Description,
		})
	}

	for _, edge := range edgeMap {
		dto.Edges = append(dto.Edges, *edge)
	}

	return nil
}

// buildPhysicalTopologyGraph 物理部署视角去 Agent 分层树与边构建
func buildPhysicalTopologyGraph(db *gorm.DB, dto *TopologyGraphDTO) error {
	// 仅拉取 SHARED 域 Agent
	var agents []models.Agent
	if err := db.Where("scope = ?", models.ScopeShared).Find(&agents).Error; err != nil {
		return err
	}
	agentMap := make(map[uint64]models.Agent)
	for _, a := range agents {
		agentMap[a.ID] = a
	}

	// 仅拉取 SHARED Agent 下的进程
	var processes []models.Process
	if err := db.Joins("JOIN traefik_agents ON traefik_agents.id = traefik_processes.agent_id").
		Where("traefik_agents.scope = ?", models.ScopeShared).
		Order("traefik_processes.id ASC").Find(&processes).Error; err != nil {
		return err
	}

	// 映射进程承载的模块
	var pms []models.ProcessModule
	if err := db.Joins("JOIN traefik_processes ON traefik_processes.id = traefik_process_modules.process_id").
		Joins("JOIN traefik_agents ON traefik_agents.id = traefik_processes.agent_id").
		Where("traefik_agents.scope = ?", models.ScopeShared).
		Find(&pms).Error; err != nil {
		return err
	}
	procModulesMap := make(map[uint64][]uint64)
	moduleProcMap := make(map[uint64]uint64)
	for _, pm := range pms {
		procModulesMap[pm.ProcessID] = append(procModulesMap[pm.ProcessID], pm.ModuleID)
		moduleProcMap[pm.ModuleID] = pm.ProcessID
	}

	var allModules []models.Module
	if err := db.Order("id ASC").Find(&allModules).Error; err != nil {
		return err
	}
	moduleMap := make(map[uint64]models.Module)
	for _, m := range allModules {
		moduleMap[m.ID] = m
	}

	var services []models.ModuleService
	if err := db.Find(&services).Error; err != nil {
		return err
	}
	svcProvideMap := make(map[uint64]uint64)
	svcNameMap := make(map[uint64]string)
	provideCountMap := make(map[uint64]int)
	for _, svc := range services {
		svcProvideMap[svc.ID] = svc.ModuleID
		svcNameMap[svc.ID] = svc.ServiceName
		provideCountMap[svc.ModuleID]++
	}

	var dependencies []models.ModuleDependency
	if err := db.Preload("Service").Find(&dependencies).Error; err != nil {
		return err
	}

	// 1. 按 Process.group_name 分组 (空值归一化为 "默认进程组")
	groupProcessMap := make(map[string][]models.Process)
	for _, p := range processes {
		gName := strings.TrimSpace(p.GroupName)
		if gName == "" {
			gName = "默认进程组"
		}
		groupProcessMap[gName] = append(groupProcessMap[gName], p)
	}

	sortedGroups := make([]string, 0, len(groupProcessMap))
	for gName := range groupProcessMap {
		sortedGroups = append(sortedGroups, gName)
	}
	sort.Strings(sortedGroups)

	deployedModuleIDs := make(map[uint64]bool)

	// 2. 依次生成 PROCESS_GROUP (depth=0) -> PROCESS (depth=1) -> MODULE (depth=2)
	for gIdx, gName := range sortedGroups {
		pgrpID := fmt.Sprintf("pgrp_%s", gName)
		dto.Nodes = append(dto.Nodes, GraphNode{
			ID:        pgrpID,
			Type:      NodeTypeProcessGroup,
			ParentID:  "",
			Ancestors: []string{},
			Depth:     0,
			Name:      gName,
			SortOrder: gIdx + 1,
		})

		procs := groupProcessMap[gName]
		for _, p := range procs {
			procID := fmt.Sprintf("proc_%d", p.ID)

			// 计算 EffectiveIP 并合成 endpoint
			var effectiveIP string
			if strings.TrimSpace(p.AdvertiseIP) != "" {
				effectiveIP = strings.TrimSpace(p.AdvertiseIP)
			} else if ag, ok := agentMap[p.AgentID]; ok {
				effectiveIP = extractHostFromEndpoint(ag.Endpoint)
			} else {
				effectiveIP = "127.0.0.1"
			}
			effectiveEndpoint := fmt.Sprintf("%s:%d", effectiveIP, p.ListenPort)

			// 绝对禁止透出 agent_id 字段
			procMeta := map[string]any{
				"endpoint":    effectiveEndpoint,
				"listen_port": p.ListenPort,
				"cmd_pattern": p.CmdPattern,
				"description": p.Description,
				"group_name":  gName,
			}

			dto.Nodes = append(dto.Nodes, GraphNode{
				ID:        procID,
				Type:      NodeTypeProcess,
				ParentID:  pgrpID,
				Ancestors: []string{pgrpID},
				Depth:     1,
				EntityID:  p.ID,
				Name:      p.Name,
				SortOrder: int(p.ID),
				Meta:      procMeta,
			})

			// 进程挂载的模块 (depth=2)
			mountedModIDs := procModulesMap[p.ID]
			for idx, modID := range mountedModIDs {
				deployedModuleIDs[modID] = true
				if mod, ok := moduleMap[modID]; ok {
					dto.Nodes = append(dto.Nodes, GraphNode{
						ID:        fmt.Sprintf("mod_%d", mod.ID),
						Type:      NodeTypeModule,
						ParentID:  procID,
						Ancestors: []string{pgrpID, procID},
						Depth:     2,
						EntityID:  mod.ID,
						Code:      mod.Code,
						Name:      mod.Name,
						SortOrder: idx + 1,
						Meta: map[string]any{
							"is_deployed":   true,
							"process_id":    p.ID,
							"provide_count": provideCountMap[mod.ID],
							"description":   mod.Description,
						},
					})
				}
			}
		}
	}

	// 3. 收集未在任何 SHARED 进程中装载的孤立模块 -> UNDEPLOYED_CONTAINER
	var undeployedMods []models.Module
	for _, m := range allModules {
		if !deployedModuleIDs[m.ID] {
			undeployedMods = append(undeployedMods, m)
		}
	}

	if len(undeployedMods) > 0 {
		dto.Nodes = append(dto.Nodes, GraphNode{
			ID:        "undeployed_container",
			Type:      NodeTypeUndeployedContainer,
			ParentID:  "",
			Ancestors: []string{},
			Depth:     0,
			Name:      "未部署模块",
			SortOrder: 8888,
			Meta: map[string]any{
				"module_count": len(undeployedMods),
			},
		})

		for idx, m := range undeployedMods {
			dto.Nodes = append(dto.Nodes, GraphNode{
				ID:        fmt.Sprintf("mod_%d", m.ID),
				Type:      NodeTypeModule,
				ParentID:  "undeployed_container",
				Ancestors: []string{"undeployed_container"},
				Depth:     1,
				EntityID:  m.ID,
				Code:      m.Code,
				Name:      m.Name,
				SortOrder: idx + 1,
				Meta: map[string]any{
					"is_deployed":   false,
					"provide_count": provideCountMap[m.ID],
					"description":   m.Description,
				},
			})
		}
	}

	// 4. 收集外部依赖与网络端点 (EXTERNAL_CONTAINER + EXTERNAL_SYSTEM + EXTERNAL_ENDPOINT)
	externalSystems := make(map[string]bool)
	for _, dep := range dependencies {
		if dep.IsExternal && strings.TrimSpace(dep.ExternalSystem) != "" {
			externalSystems[strings.TrimSpace(dep.ExternalSystem)] = true
		}
	}

	externalEndpoints := make(map[string]bool)
	type procExtraEp struct {
		procID uint64
		ep     string
	}
	var procExtraEps []procExtraEp

	for _, p := range processes {
		if len(p.ExtraDownstreams) > 0 {
			var eps []string
			if err := json.Unmarshal(p.ExtraDownstreams, &eps); err == nil {
				for _, ep := range eps {
					ep = strings.TrimSpace(ep)
					if ep != "" {
						externalEndpoints[ep] = true
						procExtraEps = append(procExtraEps, procExtraEp{procID: p.ID, ep: ep})
					}
				}
			}
		}
	}

	if len(externalSystems) > 0 || len(externalEndpoints) > 0 {
		dto.Nodes = append(dto.Nodes, GraphNode{
			ID:        "ext_container",
			Type:      NodeTypeExternalContainer,
			ParentID:  "",
			Ancestors: []string{},
			Depth:     0,
			Name:      "外部依赖与网络端点",
			SortOrder: 9999,
		})

		extSysNames := make([]string, 0, len(externalSystems))
		for name := range externalSystems {
			extSysNames = append(extSysNames, name)
		}
		sort.Strings(extSysNames)
		for idx, name := range extSysNames {
			dto.Nodes = append(dto.Nodes, GraphNode{
				ID:        fmt.Sprintf("ext_sys_%s", name),
				Type:      NodeTypeExternalSystem,
				ParentID:  "ext_container",
				Ancestors: []string{"ext_container"},
				Depth:     1,
				Name:      name,
				SortOrder: idx + 1,
			})
		}

		extEpList := make([]string, 0, len(externalEndpoints))
		for ep := range externalEndpoints {
			extEpList = append(extEpList, ep)
		}
		sort.Strings(extEpList)
		for idx, ep := range extEpList {
			dto.Nodes = append(dto.Nodes, GraphNode{
				ID:        fmt.Sprintf("ext_ep_%s", ep),
				Type:      NodeTypeExternalEndpoint,
				ParentID:  "ext_container",
				Ancestors: []string{"ext_container"},
				Depth:     1,
				Name:      ep,
				SortOrder: idx + 1 + len(extSysNames),
				Meta: map[string]any{
					"endpoint": ep,
				},
			})
		}
	}

	// 5. 聚合物理叶子依赖边
	type edgeKey struct {
		src string
		dst string
	}
	edgeMap := make(map[edgeKey]*GraphEdge)

	for _, dep := range dependencies {
		srcNodeID := fmt.Sprintf("mod_%d", dep.ConsumerModuleID)
		var dstNodeID string
		var kind string
		var dstModID *uint64
		var extSys string
		var srcProcID, dstProcID *uint64

		if pid, ok := moduleProcMap[dep.ConsumerModuleID]; ok && pid > 0 {
			pCopy := pid
			srcProcID = &pCopy
		}

		if dep.IsExternal {
			sysName := strings.TrimSpace(dep.ExternalSystem)
			if sysName == "" {
				continue
			}
			dstNodeID = fmt.Sprintf("ext_sys_%s", sysName)
			kind = EdgeKindExternalUnregistered
			extSys = sysName
		} else {
			if dep.ServiceID == nil {
				continue
			}
			svcIDVal := *dep.ServiceID
			providerID, ok := svcProvideMap[svcIDVal]
			if !ok {
				if dep.ProviderModuleID != nil {
					providerID = *dep.ProviderModuleID
				} else {
					continue
				}
			}
			dstNodeID = fmt.Sprintf("mod_%d", providerID)
			dstModID = &providerID

			if pid, ok := moduleProcMap[providerID]; ok && pid > 0 {
				pCopy := pid
				dstProcID = &pCopy
			}

			if srcProcID == nil || dstProcID == nil {
				kind = "UNDEPLOYED"
			} else if *srcProcID == *dstProcID {
				kind = EdgeKindIntraProcess
			} else {
				kind = EdgeKindCrossProcess
			}
		}

		svcName := ""
		if dep.Service != nil && dep.Service.ServiceName != "" {
			svcName = dep.Service.ServiceName
		} else if dep.ServiceID != nil {
			svcName = svcNameMap[*dep.ServiceID]
		}
		if svcName == "" && dep.IsExternal {
			svcName = dep.ExternalService
			if svcName == "" {
				svcName = dep.ExternalSystem
			}
		}

		key := edgeKey{src: srcNodeID, dst: dstNodeID}
		edge, exists := edgeMap[key]
		if !exists {
			edge = &GraphEdge{
				ID:             fmt.Sprintf("edge_%s_%s", srcNodeID, dstNodeID),
				Kind:           kind,
				SourceNodeID:   srcNodeID,
				TargetNodeID:   dstNodeID,
				SrcModuleID:    dep.ConsumerModuleID,
				DstModuleID:    dstModID,
				SrcProcessID:   srcProcID,
				DstProcessID:   dstProcID,
				Services:       make([]EdgeServiceItem, 0),
				ExternalSystem: extSys,
			}
			edgeMap[key] = edge
		}

		edge.Services = append(edge.Services, EdgeServiceItem{
			ServiceID:    dep.ServiceID,
			ServiceName:  svcName,
			DependencyID: dep.ID,
			Description:  dep.Description,
		})
	}

	// 补充 Process.ExtraDownstreams 边: proc_<id> -> ext_ep_<ep>
	for _, item := range procExtraEps {
		srcNodeID := fmt.Sprintf("proc_%d", item.procID)
		dstNodeID := fmt.Sprintf("ext_ep_%s", item.ep)
		pCopy := item.procID
		key := edgeKey{src: srcNodeID, dst: dstNodeID}
		edge, exists := edgeMap[key]
		if !exists {
			edge = &GraphEdge{
				ID:           fmt.Sprintf("edge_%s_%s", srcNodeID, dstNodeID),
				Kind:         EdgeKindExternalUnregistered,
				SourceNodeID: srcNodeID,
				TargetNodeID: dstNodeID,
				SrcProcessID: &pCopy,
				Services:     make([]EdgeServiceItem, 0),
			}
			edgeMap[key] = edge
		}
		edge.Services = append(edge.Services, EdgeServiceItem{
			ServiceName: "TCP:" + item.ep,
			Description: "补充外部端点依赖",
		})
	}

	for _, edge := range edgeMap {
		dto.Edges = append(dto.Edges, *edge)
	}

	return nil
}
