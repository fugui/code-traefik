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

// BuildTopologyGraph 构建全系统分层复合架构拓扑图 (支持 LOGICAL / PHYSICAL 视角，支持可选环境端点叠加)
func BuildTopologyGraph(db *gorm.DB, view string, envAgentIDs ...uint64) (*TopologyGraphDTO, error) {
	if view != TopologyViewLogical && view != TopologyViewPhysical {
		return nil, ErrInvalidTopologyView
	}

	var envAgentID uint64
	if len(envAgentIDs) > 0 {
		envAgentID = envAgentIDs[0]
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
		if err := buildPhysicalTopologyGraph(db, dto, envAgentID); err != nil {
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

		isDeployed := m.ArchProcessID != nil && *m.ArchProcessID > 0
		meta := map[string]any{
			"is_deployed":   isDeployed,
			"provide_count": provideCountMap[m.ID],
			"description":   m.Description,
			"subsystem_id":  m.SubsystemID,
			"group_id":      m.GroupID,
		}
		if isDeployed {
			meta["arch_process_id"] = *m.ArchProcessID
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
		if srcMod.ArchProcessID != nil && *srcMod.ArchProcessID > 0 {
			srcProcID = srcMod.ArchProcessID
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
			if ok2 && dstMod.ArchProcessID != nil && *dstMod.ArchProcessID > 0 {
				dstProcID = dstMod.ArchProcessID
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

// buildPhysicalTopologyGraph 物理进程架构蓝图去 Agent 拓扑与可选环境端点叠加 (F-04, ADR-09)
func buildPhysicalTopologyGraph(db *gorm.DB, dto *TopologyGraphDTO, envAgentID uint64) error {
	// 1. 查询全部架构进程组与架构进程蓝图
	var pgrps []models.ArchProcessGroup
	if err := db.Order("sort_order ASC, id ASC").Find(&pgrps).Error; err != nil {
		return err
	}

	var archProcs []models.ArchProcess
	if err := db.Order("group_id ASC, id ASC").Find(&archProcs).Error; err != nil {
		return err
	}

	// 2. 查询全部功能模块
	var allModules []models.Module
	if err := db.Order("id ASC").Find(&allModules).Error; err != nil {
		return err
	}
	moduleMap := make(map[uint64]models.Module)
	archModMap := make(map[uint64][]models.Module)
	for _, m := range allModules {
		moduleMap[m.ID] = m
		if m.ArchProcessID != nil && *m.ArchProcessID > 0 {
			archModMap[*m.ArchProcessID] = append(archModMap[*m.ArchProcessID], m)
		}
	}

	// 3. 统计各标准进程被多少套 SHARED 环境挂载
	type sharedCount struct {
		ArchProcessID uint64
		Count         int
	}
	var counts []sharedCount
	_ = db.Model(&models.Process{}).
		Select("traefik_processes.arch_process_id, count(DISTINCT traefik_processes.agent_id) as count").
		Joins("JOIN traefik_agents ON traefik_agents.id = traefik_processes.agent_id").
		Where("traefik_agents.scope = ?", models.ScopeShared).
		Group("traefik_processes.arch_process_id").
		Scan(&counts).Error
	sharedCountMap := make(map[uint64]int)
	for _, c := range counts {
		sharedCountMap[c.ArchProcessID] = c.Count
	}

	// 4. 若指定 envAgentID，加载该环境 Agent 及其挂载实例
	var envAgent models.Agent
	agentProcMap := make(map[uint64]models.Process)
	if envAgentID > 0 {
		_ = db.First(&envAgent, envAgentID).Error
		var envProcs []models.Process
		_ = db.Where("agent_id = ?", envAgentID).Find(&envProcs).Error
		for _, p := range envProcs {
			agentProcMap[p.ArchProcessID] = p
		}
	}

	// 5. 加载服务供给与依赖关系
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

	// 6. 按组组织架构进程
	groupArchProcMap := make(map[uint64][]models.ArchProcess)
	for _, ap := range archProcs {
		groupArchProcMap[ap.GroupID] = append(groupArchProcMap[ap.GroupID], ap)
	}

	deployedModuleIDs := make(map[uint64]bool)

	type procExtraEp struct {
		procID uint64
		ep     string
	}
	var procExtraEps []procExtraEp
	externalEndpoints := make(map[string]bool)

	// 7. 生成 PROCESS_GROUP (depth=0) -> PROCESS (depth=1) -> MODULE (depth=2)
	for gIdx, pgrp := range pgrps {
		pgrpID := fmt.Sprintf("pgrp_%d", pgrp.ID)
		dto.Nodes = append(dto.Nodes, GraphNode{
			ID:        pgrpID,
			Type:      NodeTypeProcessGroup,
			ParentID:  "",
			Ancestors: []string{},
			Depth:     0,
			EntityID:  pgrp.ID,
			Code:      pgrp.Code,
			Name:      pgrp.Name,
			SortOrder: gIdx + 1,
			Meta: map[string]any{
				"group_code":  pgrp.Code,
				"description": pgrp.Description,
			},
		})

		apList := groupArchProcMap[pgrp.ID]
		for _, ap := range apList {
			procNodeID := fmt.Sprintf("proc_%d", ap.ID)
			mountedSharedCount := sharedCountMap[ap.ID]

			var procMeta map[string]any
			var extraDownstreams []string
			if len(ap.DefaultExtraDownstreams) > 0 {
				_ = json.Unmarshal(ap.DefaultExtraDownstreams, &extraDownstreams)
			}

			if envAgentID > 0 {
				if procInst, isMounted := agentProcMap[ap.ID]; isMounted {
					var effectiveIP string
					if strings.TrimSpace(procInst.AdvertiseIP) != "" {
						effectiveIP = strings.TrimSpace(procInst.AdvertiseIP)
					} else if envAgent.ID > 0 {
						effectiveIP = extractHostFromEndpoint(envAgent.Endpoint)
					} else {
						effectiveIP = "127.0.0.1"
					}

					listenPort := procInst.EffectiveListenPort
					var effectiveEndpoint string
					if listenPort > 0 {
						effectiveEndpoint = fmt.Sprintf("%s:%d", effectiveIP, listenPort)
					} else {
						effectiveEndpoint = fmt.Sprintf("%s (纯客户端)", effectiveIP)
					}

					cmdPattern := ap.DefaultCmdPattern
					if procInst.OverrideCmdPattern != "" {
						cmdPattern = procInst.OverrideCmdPattern
					}

					if len(procInst.ExtraDownstreams) > 0 {
						var localExtras []string
						if err := json.Unmarshal(procInst.ExtraDownstreams, &localExtras); err == nil {
							extraDownstreams = append(extraDownstreams, localExtras...)
						}
					}

					procMeta = map[string]any{
						"arch_process_id":          ap.ID,
						"code":                     ap.Code,
						"is_mounted":               true,
						"process_id":               procInst.ID,
						"endpoint":                 effectiveEndpoint,
						"listen_port":              listenPort,
						"default_listen_port":      ap.DefaultListenPort,
						"probe_status":             procInst.ProbeStatus,
						"resolved_pid":             procInst.ResolvedPID,
						"established_conns":        procInst.EstablishedConns,
						"need_reconnect":           procInst.NeedReconnect,
						"cmd_pattern":              cmdPattern,
						"description":              ap.Description,
						"group_name":               pgrp.Name,
						"mounted_shared_env_count": mountedSharedCount,
					}
				} else {
					var effectiveEndpoint string
					if ap.DefaultListenPort > 0 {
						effectiveEndpoint = fmt.Sprintf(":%d", ap.DefaultListenPort)
					} else {
						effectiveEndpoint = ":0 (纯客户端)"
					}
					procMeta = map[string]any{
						"arch_process_id":          ap.ID,
						"code":                     ap.Code,
						"is_mounted":               false,
						"endpoint":                 effectiveEndpoint,
						"listen_port":              ap.DefaultListenPort,
						"default_listen_port":      ap.DefaultListenPort,
						"probe_status":             models.ProbeStatusOffline,
						"cmd_pattern":              ap.DefaultCmdPattern,
						"description":              ap.Description,
						"group_name":               pgrp.Name,
						"mounted_shared_env_count": mountedSharedCount,
					}
				}
			} else {
				var effectiveEndpoint string
				if ap.DefaultListenPort > 0 {
					effectiveEndpoint = fmt.Sprintf(":%d", ap.DefaultListenPort)
				} else {
					effectiveEndpoint = ":0 (纯客户端)"
				}
				procMeta = map[string]any{
					"arch_process_id":          ap.ID,
					"code":                     ap.Code,
					"is_mounted":               false,
					"endpoint":                 effectiveEndpoint,
					"listen_port":              ap.DefaultListenPort,
					"default_listen_port":      ap.DefaultListenPort,
					"cmd_pattern":              ap.DefaultCmdPattern,
					"description":              ap.Description,
					"group_name":               pgrp.Name,
					"mounted_shared_env_count": mountedSharedCount,
				}
			}

			dto.Nodes = append(dto.Nodes, GraphNode{
				ID:        procNodeID,
				Type:      NodeTypeProcess,
				ParentID:  pgrpID,
				Ancestors: []string{pgrpID},
				Depth:     1,
				EntityID:  ap.ID,
				Code:      ap.Code,
				Name:      ap.Name,
				SortOrder: int(ap.ID),
				Meta:      procMeta,
			})

			for _, ep := range extraDownstreams {
				ep = strings.TrimSpace(ep)
				if ep != "" {
					externalEndpoints[ep] = true
					procExtraEps = append(procExtraEps, procExtraEp{procID: ap.ID, ep: ep})
				}
			}

			// 挂载的模块 (depth=2)
			mountedMods := archModMap[ap.ID]
			for idx, mod := range mountedMods {
				deployedModuleIDs[mod.ID] = true
				dto.Nodes = append(dto.Nodes, GraphNode{
					ID:        fmt.Sprintf("mod_%d", mod.ID),
					Type:      NodeTypeModule,
					ParentID:  procNodeID,
					Ancestors: []string{pgrpID, procNodeID},
					Depth:     2,
					EntityID:  mod.ID,
					Code:      mod.Code,
					Name:      mod.Name,
					SortOrder: idx + 1,
					Meta: map[string]any{
						"is_deployed":     true,
						"arch_process_id": ap.ID,
						"provide_count":   provideCountMap[mod.ID],
						"description":     mod.Description,
					},
				})
			}
		}
	}

	// 8. 收集未在任何架构进程中装载的孤立模块 -> UNDEPLOYED_CONTAINER
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

	// 9. 收集外部依赖与网络端点 (EXTERNAL_CONTAINER + EXTERNAL_SYSTEM + EXTERNAL_ENDPOINT)
	externalSystems := make(map[string]bool)
	for _, dep := range dependencies {
		if dep.IsExternal && strings.TrimSpace(dep.ExternalSystem) != "" {
			externalSystems[strings.TrimSpace(dep.ExternalSystem)] = true
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

	// 10. 聚合物理叶子依赖边
	type edgeKey struct {
		src string
		dst string
	}
	edgeMap := make(map[edgeKey]*GraphEdge)

	for _, dep := range dependencies {
		srcMod, ok := moduleMap[dep.ConsumerModuleID]
		if !ok {
			continue
		}
		srcNodeID := fmt.Sprintf("mod_%d", dep.ConsumerModuleID)
		var dstNodeID string
		var kind string
		var dstModID *uint64
		var extSys string
		var srcProcID, dstProcID *uint64

		if srcMod.ArchProcessID != nil && *srcMod.ArchProcessID > 0 {
			srcProcID = srcMod.ArchProcessID
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

			if dstMod, ok2 := moduleMap[providerID]; ok2 && dstMod.ArchProcessID != nil && *dstMod.ArchProcessID > 0 {
				dstProcID = dstMod.ArchProcessID
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
