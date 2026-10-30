package topology

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"code-traefik/models"
	"code-traefik/pkg/agentproto"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	TopologyViewLogical  = "LOGICAL"
	TopologyViewPhysical = "PHYSICAL"

	EdgeKindCrossProcess         = "CROSS_PROCESS"
	EdgeKindIntraProcess         = "INTRA_PROCESS"
	EdgeKindExternalUnregistered = "EXTERNAL_UNREGISTERED"

	VirtualExternalNodeID = "VIRTUAL_EXTERNAL_SYSTEM"
)

var (
	ErrModuleInterfaceNotFound   = errors.New("模块接口契约不存在")
	ErrAgentNotFound             = errors.New("目标 Agent 不存在")
	ErrProcessNotFound           = errors.New("目标进程映射不存在")
	ErrInvalidRelationType       = errors.New("接口关系类型必须为 PROVIDE 或 DEPEND")
	ErrInvalidServiceNameFormat  = errors.New("服务名必须符合 {package}-{serviceName} 格式且长度为 1..63 位可见 ASCII")
	ErrInvalidOperationFormat    = errors.New("方法名必须为 1..63 位字母、数字或下划线")
	ErrDuplicateModuleInterface  = errors.New("同一模块下已存在相同的接口契约声明")
	ErrDuplicateProvideInterface = errors.New("该供给接口 (PROVIDE) 已被其他功能模块占用")
	ErrInvalidTopologyView       = errors.New("静态拓扑视角参数非法，仅允许 LOGICAL 或 PHYSICAL")
	ErrInvalidListenPort         = errors.New("进程监听端口必须在 1..65535 之间")
	ErrInvalidProcessName        = errors.New("进程名称与命令行匹配模式不能为空")
	ErrEmptyProcessModules       = errors.New("进程至少需挂载 1 个公共功能模块")
	ErrInvalidExtraDownstream    = errors.New("补充外部下游端点格式必须为合法 IPv4:Port")
	ErrDuplicateProcessPort      = errors.New("同一 Agent 下监听端口已被其他进程占用")
	ErrDuplicateProcessName      = errors.New("同一 Agent 下进程名称已存在")
	ErrDuplicateModuleBinding    = errors.New("同一部署域内同一功能模块禁止重复挂载到多个进程")
	ErrAgentHasActiveLease       = errors.New("所属 Agent 当前持有活跃排他租约，禁止变更或删除进程")
	ErrProcessReferencedByReplay = errors.New("进程仍被历史重放任务引用，禁止删除")
)

// serviceNameRegex: {package}-{serviceName} 或 {serviceName} (1..63 位)
var serviceNameRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z0-9_]+)*-[A-Za-z_][A-Za-z0-9_]*$`)
var operationRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

// ProvideConflictError 全局唯一供给冲突详情错误 (A-05)
type ProvideConflictError struct {
	ServiceName      string `json:"service_name"`
	Operation        string `json:"operation"`
	HolderModuleID   uint64 `json:"holder_module_id"`
	HolderModuleCode string `json:"holder_module_code"`
	HolderModuleName string `json:"holder_module_name"`
}

func (e *ProvideConflictError) Error() string {
	return fmt.Sprintf("供给接口 [%s:%s] 已被功能模块 [%s (%s)] 声明占用",
		e.ServiceName, e.Operation, e.HolderModuleCode, e.HolderModuleName)
}

func (e *ProvideConflictError) Unwrap() error {
	return ErrDuplicateProvideInterface
}

// ModuleInterfaceInput 模块接口契约新增入参
type ModuleInterfaceInput struct {
	ModuleID        uint64
	RelationType    string
	ServiceName     string
	Operation       string
	DiffIgnorePaths []string
	Description     string
	OperatorID      uint
}

// ProcessInput 进程部署与模块承载映射新增/修改入参
type ProcessInput struct {
	AgentID          uint64
	GroupName        string
	Name             string
	ListenPort       int
	AdvertiseIP      string
	CmdPattern       string
	ModuleIDs        []uint64
	ExtraDownstreams []string
	Description      string
	OperatorID       uint
}

// StaticTopologyNode 静态架构拓扑节点 DTO
type StaticTopologyNode struct {
	ID           string   `json:"id"`
	NodeType     string   `json:"node_type"` // SUBSYSTEM | FUNCTIONAL_GROUP | MODULE | AGENT | PROCESS | EXTERNAL
	EntityID     uint64   `json:"entity_id,omitempty"`
	ParentNodeID string   `json:"parent_node_id,omitempty"`
	Code         string   `json:"code,omitempty"`
	Name         string   `json:"name"`
	Level        int      `json:"level,omitempty"`
	ListenPort   int      `json:"listen_port,omitempty"`
	Endpoint     string   `json:"endpoint,omitempty"`
	ProcessID    *uint64  `json:"process_id,omitempty"`
	ModuleIDs    []uint64 `json:"module_ids,omitempty"`
}

// StaticTopologyEdge 静态架构拓扑有向边 DTO
type StaticTopologyEdge struct {
	ID           string  `json:"id"`
	Kind         string  `json:"kind"` // CROSS_PROCESS | INTRA_PROCESS | EXTERNAL_UNREGISTERED
	SourceNodeID string  `json:"source_node_id"`
	TargetNodeID string  `json:"target_node_id"`
	SrcModuleID  uint64  `json:"src_module_id"`
	DstModuleID  *uint64 `json:"dst_module_id,omitempty"`
	SrcProcessID *uint64 `json:"src_process_id,omitempty"`
	DstProcessID *uint64 `json:"dst_process_id,omitempty"`
	ServiceName  string  `json:"service_name"`
	Operation    string  `json:"operation"`
}

// StaticTopologyDTO 静态拓扑响应载荷
type StaticTopologyDTO struct {
	View  string               `json:"view"`
	Nodes []StaticTopologyNode `json:"nodes"`
	Edges []StaticTopologyEdge `json:"edges"`
}

// CreateModuleInterface 创建接口契约并强制执行全局唯一 PROVIDE 校验 (A-05)
func CreateModuleInterface(db *gorm.DB, in ModuleInterfaceInput) (*models.ModuleInterface, error) {
	relType := strings.ToUpper(strings.TrimSpace(in.RelationType))
	if relType != "PROVIDE" && relType != "DEPEND" {
		return nil, ErrInvalidRelationType
	}

	serviceName := strings.TrimSpace(in.ServiceName)
	if len(serviceName) == 0 || len(serviceName) > 63 || !serviceNameRegex.MatchString(serviceName) {
		return nil, ErrInvalidServiceNameFormat
	}

	operation := strings.TrimSpace(in.Operation)
	if len(operation) == 0 || len(operation) > 63 || !operationRegex.MatchString(operation) {
		return nil, ErrInvalidOperationFormat
	}

	var mod models.Module
	if err := db.First(&mod, in.ModuleID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrModuleNotFound
		}
		return nil, err
	}

	// 模块内本地四元组唯一性检查
	var localCount int64
	if err := db.Model(&models.ModuleInterface{}).
		Where("module_id = ? AND relation_type = ? AND service_name = ? AND operation = ?",
			in.ModuleID, relType, serviceName, operation).
		Count(&localCount).Error; err != nil {
		return nil, err
	}
	if localCount > 0 {
		return nil, ErrDuplicateModuleInterface
	}

	// 全局 PROVIDE 唯一性检查
	if relType == "PROVIDE" {
		var existing models.ModuleInterface
		err := db.Where("relation_type = 'PROVIDE' AND service_name = ? AND operation = ?", serviceName, operation).
			First(&existing).Error
		if err == nil {
			// 查出占用该接口的模块信息
			var holder models.Module
			_ = db.First(&holder, existing.ModuleID).Error
			return nil, &ProvideConflictError{
				ServiceName:      serviceName,
				Operation:        operation,
				HolderModuleID:   holder.ID,
				HolderModuleCode: holder.Code,
				HolderModuleName: holder.Name,
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}

	diffPathsJSON, _ := json.Marshal(in.DiffIgnorePaths)
	if in.DiffIgnorePaths == nil {
		diffPathsJSON = []byte("[]")
	}

	iface := &models.ModuleInterface{
		ModuleID:        in.ModuleID,
		RelationType:    relType,
		ServiceName:     serviceName,
		Operation:       operation,
		DiffIgnorePaths: datatypes.JSON(diffPathsJSON),
		Description:     in.Description,
		CreatedBy:       in.OperatorID,
	}

	if err := db.Create(iface).Error; err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "23505") {
			if relType == "PROVIDE" {
				return nil, ErrDuplicateProvideInterface
			}
			return nil, ErrDuplicateModuleInterface
		}
		return nil, err
	}

	return iface, nil
}

// UpdateModuleInterface 更新接口契约忽略路径与描述
func UpdateModuleInterface(db *gorm.DB, ifaceID uint64, diffIgnorePaths []string, description string) (*models.ModuleInterface, *models.ModuleInterface, error) {
	var before models.ModuleInterface
	if err := db.First(&before, ifaceID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrModuleInterfaceNotFound
		}
		return nil, nil, err
	}

	diffPathsJSON, _ := json.Marshal(diffIgnorePaths)
	if diffIgnorePaths == nil {
		diffPathsJSON = []byte("[]")
	}

	after := before
	after.DiffIgnorePaths = datatypes.JSON(diffPathsJSON)
	after.Description = description

	if err := db.Model(&models.ModuleInterface{}).Where("id = ?", ifaceID).Updates(map[string]interface{}{
		"diff_ignore_paths": datatypes.JSON(diffPathsJSON),
		"description":       description,
	}).Error; err != nil {
		return nil, nil, err
	}

	return &before, &after, nil
}

// DeleteModuleInterface 删除接口契约
func DeleteModuleInterface(db *gorm.DB, ifaceID uint64) (*models.ModuleInterface, error) {
	var iface models.ModuleInterface
	if err := db.First(&iface, ifaceID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrModuleInterfaceNotFound
		}
		return nil, err
	}

	if err := db.Delete(&iface).Error; err != nil {
		return nil, err
	}
	return &iface, nil
}

// validateIPv4Endpoint 校验 IPv4:Port
func validateIPv4Endpoint(ep string) error {
	host, portStr, err := net.SplitHostPort(ep)
	if err != nil {
		return ErrInvalidExtraDownstream
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil {
		return ErrInvalidExtraDownstream
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return ErrInvalidExtraDownstream
	}
	return nil
}

// CreateProcess 创建进程并承载模块
func CreateProcess(db *gorm.DB, in ProcessInput) (*models.Process, error) {
	if in.ListenPort < 1 || in.ListenPort > 65535 {
		return nil, ErrInvalidListenPort
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, ErrInvalidProcessName
	}
	cmdPattern := strings.TrimSpace(in.CmdPattern)
	if cmdPattern == "" {
		cmdPattern = "./bin/" + name
	}
	groupName := strings.TrimSpace(in.GroupName)
	if groupName == "" {
		groupName = "默认进程组"
	}
	for _, ep := range in.ExtraDownstreams {
		if err := validateIPv4Endpoint(ep); err != nil {
			return nil, err
		}
	}

	var agent models.Agent
	if in.AgentID == 0 {
		if err := db.Where("scope = ?", "SHARED").First(&agent).Error; err != nil {
			if err := db.First(&agent).Error; err != nil {
				agent = models.Agent{
					Name:        "default-host-agent",
					Endpoint:    "127.0.0.1:8090",
					Scope:       "SHARED",
					Status:      "ONLINE",
					Description: "系统预置默认宿主 Agent",
				}
				if err := db.Create(&agent).Error; err != nil {
					return nil, err
				}
			}
		}
		in.AgentID = agent.ID
	} else {
		if err := db.First(&agent, in.AgentID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrAgentNotFound
			}
			return nil, err
		}
	}

	if agent.LeaseType != "" {
		return nil, ErrAgentHasActiveLease
	}

	// 端口与名称互斥
	var portCount, nameCount int64
	if err := db.Model(&models.Process{}).Where("agent_id = ? AND listen_port = ?", in.AgentID, in.ListenPort).Count(&portCount).Error; err != nil {
		return nil, err
	}
	if portCount > 0 {
		return nil, ErrDuplicateProcessPort
	}
	if err := db.Model(&models.Process{}).Where("agent_id = ? AND name = ?", in.AgentID, name).Count(&nameCount).Error; err != nil {
		return nil, err
	}
	if nameCount > 0 {
		return nil, ErrDuplicateProcessName
	}

	if len(in.ModuleIDs) > 0 {
		// 校验所有 Module 存在
		var modCount int64
		if err := db.Model(&models.Module{}).Where("id IN ?", in.ModuleIDs).Count(&modCount).Error; err != nil {
			return nil, err
		}
		if modCount != int64(len(in.ModuleIDs)) {
			return nil, ErrModuleNotFound
		}

		// 校验单域单进程挂载约束 (A-03)
		if agent.Scope == models.ScopeShared {
			// SHARED Agent 域：检查全部 SHARED 进程中是否已绑定该模块
			var conflictCount int64
			err := db.Model(&models.ProcessModule{}).
				Joins("JOIN traefik_processes ON traefik_processes.id = traefik_process_modules.process_id").
				Joins("JOIN traefik_agents ON traefik_agents.id = traefik_processes.agent_id").
				Where("traefik_agents.scope = ? AND traefik_process_modules.module_id IN ?", models.ScopeShared, in.ModuleIDs).
				Count(&conflictCount).Error
			if err != nil {
				return nil, err
			}
			if conflictCount > 0 {
				return nil, ErrDuplicateModuleBinding
			}
		} else {
			// PRIVATE Agent 域：检查当前 Agent 下是否已绑定该模块
			var conflictCount int64
			err := db.Model(&models.ProcessModule{}).
				Joins("JOIN traefik_processes ON traefik_processes.id = traefik_process_modules.process_id").
				Where("traefik_processes.agent_id = ? AND traefik_process_modules.module_id IN ?", in.AgentID, in.ModuleIDs).
				Count(&conflictCount).Error
			if err != nil {
				return nil, err
			}
			if conflictCount > 0 {
				return nil, ErrDuplicateModuleBinding
			}
		}
	}

	extraJSON, _ := json.Marshal(in.ExtraDownstreams)
	if in.ExtraDownstreams == nil {
		extraJSON = []byte("[]")
	}

	proc := &models.Process{
		AgentID:          in.AgentID,
		GroupName:        groupName,
		Name:             name,
		ListenPort:       in.ListenPort,
		AdvertiseIP:      strings.TrimSpace(in.AdvertiseIP),
		CmdPattern:       cmdPattern,
		ExtraDownstreams: datatypes.JSON(extraJSON),
		Description:      in.Description,
		CreatedBy:        in.OperatorID,
		UpdatedBy:        in.OperatorID,
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(proc).Error; err != nil {
			if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "23505") {
				return ErrDuplicateProcessPort
			}
			return err
		}

		for _, mid := range in.ModuleIDs {
			pm := &models.ProcessModule{
				ProcessID: proc.ID,
				ModuleID:  mid,
			}
			if err := tx.Create(pm).Error; err != nil {
				return err
			}

			// 若是公共 SHARED 进程，同步维护 Module.ProcessID 基线字段
			if agent.Scope == models.ScopeShared {
				if err := tx.Model(&models.Module{}).Where("id = ?", mid).Update("process_id", proc.ID).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return proc, nil
}

// UpdateProcess 更新进程
func UpdateProcess(db *gorm.DB, procID uint64, in ProcessInput) (*models.Process, *models.Process, error) {
	if in.ListenPort < 1 || in.ListenPort > 65535 {
		return nil, nil, ErrInvalidListenPort
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, nil, ErrInvalidProcessName
	}
	cmdPattern := strings.TrimSpace(in.CmdPattern)
	if cmdPattern == "" {
		cmdPattern = "./bin/" + name
	}
	for _, ep := range in.ExtraDownstreams {
		if err := validateIPv4Endpoint(ep); err != nil {
			return nil, nil, err
		}
	}

	var before models.Process
	if err := db.Preload("Agent").First(&before, procID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrProcessNotFound
		}
		return nil, nil, err
	}

	if before.Agent != nil && before.Agent.LeaseType != "" {
		return nil, nil, ErrAgentHasActiveLease
	}

	// 端口与名称冲突检查
	if in.ListenPort != before.ListenPort {
		var portCount int64
		if err := db.Model(&models.Process{}).Where("agent_id = ? AND listen_port = ? AND id != ?", before.AgentID, in.ListenPort, procID).Count(&portCount).Error; err != nil {
			return nil, nil, err
		}
		if portCount > 0 {
			return nil, nil, ErrDuplicateProcessPort
		}
	}
	if strings.TrimSpace(in.Name) != before.Name {
		var nameCount int64
		if err := db.Model(&models.Process{}).Where("agent_id = ? AND name = ? AND id != ?", before.AgentID, strings.TrimSpace(in.Name), procID).Count(&nameCount).Error; err != nil {
			return nil, nil, err
		}
		if nameCount > 0 {
			return nil, nil, ErrDuplicateProcessName
		}
	}

	// 校验单域单进程挂载约束 (A-03)
	if before.Agent != nil && before.Agent.Scope == models.ScopeShared {
		var conflictCount int64
		err := db.Model(&models.ProcessModule{}).
			Joins("JOIN traefik_processes ON traefik_processes.id = traefik_process_modules.process_id").
			Joins("JOIN traefik_agents ON traefik_agents.id = traefik_processes.agent_id").
			Where("traefik_agents.scope = ? AND traefik_process_modules.process_id != ? AND traefik_process_modules.module_id IN ?",
				models.ScopeShared, procID, in.ModuleIDs).
			Count(&conflictCount).Error
		if err != nil {
			return nil, nil, err
		}
		if conflictCount > 0 {
			return nil, nil, ErrDuplicateModuleBinding
		}
	} else {
		var conflictCount int64
		err := db.Model(&models.ProcessModule{}).
			Joins("JOIN traefik_processes ON traefik_processes.id = traefik_process_modules.process_id").
			Where("traefik_processes.agent_id = ? AND traefik_process_modules.process_id != ? AND traefik_process_modules.module_id IN ?",
				before.AgentID, procID, in.ModuleIDs).
			Count(&conflictCount).Error
		if err != nil {
			return nil, nil, err
		}
		if conflictCount > 0 {
			return nil, nil, ErrDuplicateModuleBinding
		}
	}

	extraJSON, _ := json.Marshal(in.ExtraDownstreams)
	if in.ExtraDownstreams == nil {
		extraJSON = []byte("[]")
	}

	after := before
	if in.GroupName != "" {
		after.GroupName = strings.TrimSpace(in.GroupName)
	}
	after.Name = strings.TrimSpace(in.Name)
	after.ListenPort = in.ListenPort
	after.AdvertiseIP = strings.TrimSpace(in.AdvertiseIP)
	after.CmdPattern = cmdPattern
	after.ExtraDownstreams = datatypes.JSON(extraJSON)
	after.Description = in.Description
	after.UpdatedBy = in.OperatorID

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Process{}).Where("id = ?", procID).Updates(map[string]interface{}{
			"group_name":        after.GroupName,
			"name":              after.Name,
			"listen_port":       after.ListenPort,
			"advertise_ip":      after.AdvertiseIP,
			"cmd_pattern":       after.CmdPattern,
			"extra_downstreams": after.ExtraDownstreams,
			"description":       after.Description,
			"updated_by":        after.UpdatedBy,
		}).Error; err != nil {
			return err
		}

		// 刷新 process_modules
		if before.Agent != nil && before.Agent.Scope == models.ScopeShared {
			// 解绑前清空旧 Module.ProcessID
			_ = tx.Model(&models.Module{}).Where("process_id = ?", procID).Update("process_id", nil).Error
		}
		if err := tx.Where("process_id = ?", procID).Delete(&models.ProcessModule{}).Error; err != nil {
			return err
		}

		for _, mid := range in.ModuleIDs {
			pm := &models.ProcessModule{
				ProcessID: procID,
				ModuleID:  mid,
			}
			if err := tx.Create(pm).Error; err != nil {
				return err
			}
			if before.Agent != nil && before.Agent.Scope == models.ScopeShared {
				if err := tx.Model(&models.Module{}).Where("id = ?", mid).Update("process_id", procID).Error; err != nil {
					return err
				}
			}
		}

		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	return &before, &after, nil
}

// DeleteProcess 删除进程映射
func DeleteProcess(db *gorm.DB, procID uint64) (*models.Process, error) {
	var proc models.Process
	if err := db.Preload("Agent").First(&proc, procID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrProcessNotFound
		}
		return nil, err
	}

	if proc.Agent != nil && proc.Agent.LeaseType != "" {
		return nil, ErrAgentHasActiveLease
	}

	// 检查是否仍被重放任务引用
	var replayCount int64
	if err := db.Model(&models.ReplayTask{}).
		Where("source_process_id = ? OR target_process_id = ?", procID, procID).
		Count(&replayCount).Error; err != nil {
		return nil, err
	}
	if replayCount > 0 {
		return nil, ErrProcessReferencedByReplay
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		if proc.Agent != nil && proc.Agent.Scope == models.ScopeShared {
			_ = tx.Model(&models.Module{}).Where("process_id = ?", procID).Update("process_id", nil).Error
		}
		if err := tx.Where("process_id = ?", procID).Delete(&models.ProcessModule{}).Error; err != nil {
			return err
		}
		return tx.Delete(&proc).Error
	})
	if err != nil {
		return nil, err
	}

	return &proc, nil
}

// BuildStaticTopology 编译并聚合静态架构拓扑 (仅包含公共 SHARED 元素)
func BuildStaticTopology(db *gorm.DB, view string, subsystemID *uint64) (*StaticTopologyDTO, error) {
	if view != TopologyViewLogical && view != TopologyViewPhysical {
		return nil, ErrInvalidTopologyView
	}

	var subsystems []models.Subsystem
	subQuery := db.Model(&models.Subsystem{})
	if subsystemID != nil {
		subQuery = subQuery.Where("id = ?", *subsystemID)
	}
	if err := subQuery.Order("id ASC").Find(&subsystems).Error; err != nil {
		return nil, err
	}

	var groups []models.FunctionalGroup
	grpQuery := db.Model(&models.FunctionalGroup{})
	if subsystemID != nil {
		grpQuery = grpQuery.Where("subsystem_id = ?", *subsystemID)
	}
	if err := grpQuery.Order("level ASC, sort_order ASC, id ASC").Find(&groups).Error; err != nil {
		return nil, err
	}

	var modules []models.Module
	modQuery := db.Model(&models.Module{})
	if subsystemID != nil {
		modQuery = modQuery.Where("subsystem_id = ?", *subsystemID)
	}
	if err := modQuery.Order("id ASC").Find(&modules).Error; err != nil {
		return nil, err
	}

	// 仅拉取公共 SHARED Agent 与其下的公共进程
	var agents []models.Agent
	if err := db.Where("scope = ?", models.ScopeShared).Order("id ASC").Find(&agents).Error; err != nil {
		return nil, err
	}

	var processes []models.Process
	if err := db.Joins("JOIN traefik_agents ON traefik_agents.id = traefik_processes.agent_id").
		Where("traefik_agents.scope = ?", models.ScopeShared).
		Order("traefik_processes.id ASC").Find(&processes).Error; err != nil {
		return nil, err
	}

	var pms []models.ProcessModule
	if err := db.Joins("JOIN traefik_processes ON traefik_processes.id = traefik_process_modules.process_id").
		Joins("JOIN traefik_agents ON traefik_agents.id = traefik_processes.agent_id").
		Where("traefik_agents.scope = ?", models.ScopeShared).
		Find(&pms).Error; err != nil {
		return nil, err
	}
	procModulesMap := make(map[uint64][]uint64) // processID -> []moduleID
	moduleProcMap := make(map[uint64]uint64)    // moduleID -> processID
	for _, pm := range pms {
		procModulesMap[pm.ProcessID] = append(procModulesMap[pm.ProcessID], pm.ModuleID)
		moduleProcMap[pm.ModuleID] = pm.ProcessID
	}

	var ifaces []models.ModuleInterface
	if err := db.Find(&ifaces).Error; err != nil {
		return nil, err
	}

	provideMap := make(map[string]models.ModuleInterface) // key: serviceName + ":" + operation -> PROVIDE iface
	var dependIfaces []models.ModuleInterface
	for _, iface := range ifaces {
		key := iface.ServiceName + ":" + iface.Operation
		if iface.RelationType == "PROVIDE" {
			provideMap[key] = iface
		} else if iface.RelationType == "DEPEND" {
			dependIfaces = append(dependIfaces, iface)
		}
	}

	nodes := make([]StaticTopologyNode, 0)
	edges := make([]StaticTopologyEdge, 0)
	hasExternalNode := false

	// 构建节点
	for _, sub := range subsystems {
		nodes = append(nodes, StaticTopologyNode{
			ID:       fmt.Sprintf("sub_%d", sub.ID),
			NodeType: "SUBSYSTEM",
			EntityID: sub.ID,
			Code:     sub.Code,
			Name:     sub.Name,
		})
	}

	for _, g := range groups {
		parentNodeID := fmt.Sprintf("sub_%d", g.SubsystemID)
		if g.ParentID != nil {
			parentNodeID = fmt.Sprintf("grp_%d", *g.ParentID)
		}
		nodes = append(nodes, StaticTopologyNode{
			ID:           fmt.Sprintf("grp_%d", g.ID),
			NodeType:     "FUNCTIONAL_GROUP",
			EntityID:     g.ID,
			ParentNodeID: parentNodeID,
			Code:         g.Code,
			Name:         g.Name,
			Level:        g.Level,
		})
	}

	for _, m := range modules {
		nodes = append(nodes, StaticTopologyNode{
			ID:           fmt.Sprintf("mod_%d", m.ID),
			NodeType:     "MODULE",
			EntityID:     m.ID,
			ParentNodeID: fmt.Sprintf("grp_%d", m.GroupID),
			Code:         m.Code,
			Name:         m.Name,
			ProcessID:    m.ProcessID,
		})
	}

	if view == TopologyViewPhysical {
		for _, a := range agents {
			nodes = append(nodes, StaticTopologyNode{
				ID:       fmt.Sprintf("agt_%d", a.ID),
				NodeType: "AGENT",
				EntityID: a.ID,
				Name:     a.Name,
				Endpoint: a.Endpoint,
			})
		}
		for _, p := range processes {
			nodes = append(nodes, StaticTopologyNode{
				ID:           fmt.Sprintf("proc_%d", p.ID),
				NodeType:     "PROCESS",
				EntityID:     p.ID,
				ParentNodeID: fmt.Sprintf("agt_%d", p.AgentID),
				Name:         p.Name,
				ListenPort:   p.ListenPort,
				ModuleIDs:    procModulesMap[p.ID],
			})
		}
	}

	// 构建依赖边
	edgeSet := make(map[string]bool)

	for _, dep := range dependIfaces {
		key := dep.ServiceName + ":" + dep.Operation
		srcModID := dep.ModuleID
		srcProcIDVal, hasSrcProc := moduleProcMap[srcModID]
		var srcProcID *uint64
		if hasSrcProc {
			srcProcID = &srcProcIDVal
		}

		provider, exists := provideMap[key]
		if !exists {
			// 指向外部未登记系统
			hasExternalNode = true
			var srcNodeID, tgtNodeID string
			if view == TopologyViewLogical {
				srcNodeID = fmt.Sprintf("mod_%d", srcModID)
				tgtNodeID = VirtualExternalNodeID
			} else {
				if hasSrcProc {
					srcNodeID = fmt.Sprintf("proc_%d", srcProcIDVal)
				} else {
					srcNodeID = fmt.Sprintf("mod_%d", srcModID)
				}
				tgtNodeID = VirtualExternalNodeID
			}

			edgeID := fmt.Sprintf("edge_ext_%d_%s", srcModID, key)
			if !edgeSet[edgeID] {
				edgeSet[edgeID] = true
				edges = append(edges, StaticTopologyEdge{
					ID:           edgeID,
					Kind:         EdgeKindExternalUnregistered,
					SourceNodeID: srcNodeID,
					TargetNodeID: tgtNodeID,
					SrcModuleID:  srcModID,
					SrcProcessID: srcProcID,
					ServiceName:  dep.ServiceName,
					Operation:    dep.Operation,
				})
			}
			continue
		}

		dstModID := provider.ModuleID
		dstProcIDVal, hasDstProc := moduleProcMap[dstModID]
		var dstProcID *uint64
		if hasDstProc {
			dstProcID = &dstProcIDVal
		}

		// 判定进程内调用 vs 跨进程调用
		kind := EdgeKindCrossProcess
		if hasSrcProc && hasDstProc && srcProcIDVal == dstProcIDVal {
			kind = EdgeKindIntraProcess
		}

		var srcNodeID, tgtNodeID string
		if view == TopologyViewLogical {
			srcNodeID = fmt.Sprintf("mod_%d", srcModID)
			tgtNodeID = fmt.Sprintf("mod_%d", dstModID)
		} else {
			if hasSrcProc {
				srcNodeID = fmt.Sprintf("proc_%d", srcProcIDVal)
			} else {
				srcNodeID = fmt.Sprintf("mod_%d", srcModID)
			}
			if hasDstProc {
				tgtNodeID = fmt.Sprintf("proc_%d", dstProcIDVal)
			} else {
				tgtNodeID = fmt.Sprintf("mod_%d", dstModID)
			}
		}

		edgeID := fmt.Sprintf("edge_%d_%d_%s", srcModID, dstModID, key)
		if !edgeSet[edgeID] {
			edgeSet[edgeID] = true
			edges = append(edges, StaticTopologyEdge{
				ID:           edgeID,
				Kind:         kind,
				SourceNodeID: srcNodeID,
				TargetNodeID: tgtNodeID,
				SrcModuleID:  srcModID,
				DstModuleID:  &dstModID,
				SrcProcessID: srcProcID,
				DstProcessID: dstProcID,
				ServiceName:  dep.ServiceName,
				Operation:    dep.Operation,
			})
		}
	}

	if hasExternalNode {
		nodes = append(nodes, StaticTopologyNode{
			ID:       VirtualExternalNodeID,
			NodeType: "EXTERNAL",
			Name:     "外部未登记系统",
		})
	}

	return &StaticTopologyDTO{
		View:  view,
		Nodes: nodes,
		Edges: edges,
	}, nil
}

// CompileProcessDownstreams 四层聚合编译进程下游依赖端点 (F-02.3)
func CompileProcessDownstreams(db *gorm.DB, procID uint64, batchProcessIDs []uint64, replayEgress []agentproto.DownstreamEndpoint) ([]agentproto.DownstreamEndpoint, error) {
	var proc models.Process
	if err := db.Preload("Agent").First(&proc, procID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrProcessNotFound
		}
		return nil, err
	}

	var pms []models.ProcessModule
	if err := db.Where("process_id = ?", procID).Find(&pms).Error; err != nil {
		return nil, err
	}
	moduleIDs := make([]uint64, 0, len(pms))
	for _, pm := range pms {
		moduleIDs = append(moduleIDs, pm.ModuleID)
	}

	// 1. 契约声明下游：从所承载模块的 DEPEND 接口反查提供该接口的 PROVIDE 模块及对应进程
	var declaredEndpoints []agentproto.DownstreamEndpoint
	if len(moduleIDs) > 0 {
		var dependIfaces []models.ModuleInterface
		_ = db.Where("module_id IN ? AND relation_type = 'DEPEND'", moduleIDs).Find(&dependIfaces).Error

		for _, dep := range dependIfaces {
			var provider models.ModuleInterface
			err := db.Where("relation_type = 'PROVIDE' AND service_name = ? AND operation = ?", dep.ServiceName, dep.Operation).First(&provider).Error
			if err == nil {
				var targetMod models.Module
				if err := db.First(&targetMod, provider.ModuleID).Error; err == nil && targetMod.ProcessID != nil {
					var tgtProc models.Process
					if err := db.Preload("Agent").First(&tgtProc, *targetMod.ProcessID).Error; err == nil && tgtProc.Agent != nil {
						ip := parseHostFromEndpoint(tgtProc.Agent.Endpoint)
						if tgtProc.AdvertiseIP != "" {
							ip = tgtProc.AdvertiseIP
						}
						if ip != "" && tgtProc.ListenPort > 0 {
							declaredEndpoints = append(declaredEndpoints, agentproto.DownstreamEndpoint{
								IP:   ip,
								Port: uint16(tgtProc.ListenPort),
							})
							// 同机 127.0.0.1 别名探测
							if proc.AgentID == tgtProc.AgentID {
								declaredEndpoints = append(declaredEndpoints, agentproto.DownstreamEndpoint{
									IP:   "127.0.0.1",
									Port: uint16(tgtProc.ListenPort),
								})
							}
						}
					}
				}
			}
		}
	}

	// 2. 批次协同进程监听端点
	var batchEndpoints []agentproto.DownstreamEndpoint
	if len(batchProcessIDs) > 0 {
		var batchProcs []models.Process
		_ = db.Preload("Agent").Where("id IN ?", batchProcessIDs).Find(&batchProcs).Error
		for _, bp := range batchProcs {
			if bp.ID == procID {
				continue
			}
			if bp.Agent != nil {
				ip := parseHostFromEndpoint(bp.Agent.Endpoint)
				if bp.AdvertiseIP != "" {
					ip = bp.AdvertiseIP
				}
				if ip != "" && bp.ListenPort > 0 {
					batchEndpoints = append(batchEndpoints, agentproto.DownstreamEndpoint{
						IP:   ip,
						Port: uint16(bp.ListenPort),
					})
					if proc.AgentID == bp.AgentID {
						batchEndpoints = append(batchEndpoints, agentproto.DownstreamEndpoint{
							IP:   "127.0.0.1",
							Port: uint16(bp.ListenPort),
						})
					}
				}
			}
		}
	}

	// 3. 补充外部固定下游 ExtraDownstreams
	var extraEndpoints []agentproto.DownstreamEndpoint
	if len(proc.ExtraDownstreams) > 0 {
		var eps []string
		_ = json.Unmarshal(proc.ExtraDownstreams, &eps)
		for _, ep := range eps {
			host, portStr, err := net.SplitHostPort(ep)
			if err == nil {
				port, err := strconv.Atoi(portStr)
				if err == nil && port > 0 && port <= 65535 {
					extraEndpoints = append(extraEndpoints, agentproto.DownstreamEndpoint{
						IP:   host,
						Port: uint16(port),
					})
				}
			}
		}
	}

	// 4. 合并去重并剔除自身监听端口
	merged := make([]agentproto.DownstreamEndpoint, 0)
	seen := make(map[string]bool)

	all := append(declaredEndpoints, batchEndpoints...)
	all = append(all, extraEndpoints...)
	all = append(all, replayEgress...)

	selfPort := uint16(proc.ListenPort)

	for _, ep := range all {
		if ep.Port == selfPort {
			continue
		}
		key := fmt.Sprintf("%s:%d", ep.IP, ep.Port)
		if !seen[key] {
			seen[key] = true
			merged = append(merged, ep)
		}
	}

	sort.Slice(merged, func(i, j int) bool {
		if merged[i].IP == merged[j].IP {
			return merged[i].Port < merged[j].Port
		}
		return merged[i].IP < merged[j].IP
	})

	return merged, nil
}

func parseHostFromEndpoint(ep string) string {
	if !strings.HasPrefix(ep, "http://") && !strings.HasPrefix(ep, "https://") {
		ep = "http://" + ep
	}
	u, err := url.Parse(ep)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
