package topology

import (
	"context"
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
	ErrModuleServiceNotFound     = errors.New("模块供给服务契约不存在")
	ErrModuleDependencyNotFound  = errors.New("模块服务依赖关系不存在")
	ErrAgentNotFound             = errors.New("目标 Agent 不存在")
	ErrProcessNotFound           = errors.New("目标进程映射不存在")
	ErrInvalidServiceNameFormat  = errors.New("服务名必须符合 {package}.{serviceName} 点分规范且长度为 1..63 位可见字符")
	ErrDuplicateModuleService    = errors.New("该服务已被其他功能模块声明提供 (A-05)")
	ErrDuplicateDependency       = errors.New("当前功能模块已存在相同的服务依赖")
	ErrSelfDependency            = errors.New("功能模块不能依赖自身提供的服务")
	ErrServiceHasDependencies    = errors.New("该服务已被其他功能模块作为依赖引用，请先解除依赖关系")
	ErrInvalidExternalDependency = errors.New("外部服务依赖必须填写外部系统名称或外部服务名称")
	ErrInvalidTopologyView       = errors.New("静态拓扑视角参数非法，仅允许 LOGICAL 或 PHYSICAL")
	ErrInvalidListenPort         = errors.New("进程监听端口必须在 0..65535 之间 (0 表示纯客户端无监听端口)")
	ErrInvalidProcessName        = errors.New("进程名称与命令行匹配模式不能为空")
	ErrEmptyProcessModules       = errors.New("进程至少需挂载 1 个公共功能模块")
	ErrInvalidExtraDownstream    = errors.New("补充外部下游端点格式必须为合法 IPv4:Port")
	ErrDuplicateProcessPort      = errors.New("同一 Agent 下监听端口已被其他进程占用")
	ErrDuplicateProcessName      = errors.New("同一 Agent 下进程名称已存在")
	ErrDuplicateModuleBinding    = errors.New("同一部署域内同一功能模块禁止重复挂载到多个进程")
	ErrAgentHasActiveLease       = errors.New("所属 Agent 当前持有活跃排他租约，禁止变更或删除进程")
	ErrProcessReferencedByReplay = errors.New("进程仍被历史重放任务引用，禁止删除")
)

// serviceNameRegex: 服务名规范 {package}.{serviceName} (1..63 位可见字符)
var serviceNameRegex = regexp.MustCompile(`^[A-Za-z0-9._-]{1,63}$`)

// ProvideConflictError 全局唯一供给冲突详情错误 (A-05)
type ProvideConflictError struct {
	ServiceName      string `json:"service_name"`
	HolderModuleID   uint64 `json:"holder_module_id"`
	HolderModuleCode string `json:"holder_module_code"`
	HolderModuleName string `json:"holder_module_name"`
}

func (e *ProvideConflictError) Error() string {
	return fmt.Sprintf("供给服务 [%s] 已被功能模块 [%s (%s)] 声明占用",
		e.ServiceName, e.HolderModuleCode, e.HolderModuleName)
}

func (e *ProvideConflictError) Unwrap() error {
	return ErrDuplicateModuleService
}

// ModuleServiceInput 模块供给服务新增入参
type ModuleServiceInput struct {
	ModuleID        uint64
	ServiceName     string
	ContractID      *uint64
	ImplKey         string
	DiffIgnorePaths []string
	Description     string
	OperatorID      uint
}

// ModuleDependencyInput 模块依赖下游服务新增入参
type ModuleDependencyInput struct {
	ConsumerModuleID uint64
	IsExternal       bool
	ExternalSystem   string
	ExternalService  string
	ServiceID        *uint64
	ContractID       *uint64
	Description      string
	OperatorID       uint
}

// AvailableServiceDTO 可用服务下拉候选项
type AvailableServiceDTO struct {
	ServiceID           uint64  `json:"service_id"`
	ServiceName         string  `json:"service_name"`
	ContractID          *uint64 `json:"contract_id,omitempty"`
	ContractServiceName string  `json:"contract_service_name,omitempty"`
	ImplKey             string  `json:"impl_key,omitempty"`
	IsCustomServiceName bool    `json:"is_custom_service_name,omitempty"`
	IsMultiImpl         bool    `json:"is_multi_impl,omitempty"`
	ModuleID            uint64  `json:"module_id"`
	ModuleCode          string  `json:"module_code"`
	ModuleName          string  `json:"module_name"`
	SubsystemID         uint64  `json:"subsystem_id"`
	SubsystemName       string  `json:"subsystem_name"`
	Description         string  `json:"description"`
}

// UpdateModuleServiceInput 更新供给服务入参 (ADR-16 v2.0)
type UpdateModuleServiceInput struct {
	ServiceID       uint64
	ServiceName     *string
	ImplKey         *string
	DiffIgnorePaths []string
	Description     string
}

// ProcessInput 进程部署与模块承载映射新增/修改入参
type ProcessInput struct {
	AgentID            uint64
	ArchProcessID      uint64
	GroupName          string
	Name               string
	ListenPort         int
	OverrideListenPort *int
	OverrideCmdPattern string
	AdvertiseIP        string
	CmdPattern         string
	ModuleIDs          []uint64
	ExtraDownstreams   []string
	ProbeDesiredState  string
	Description        string
	OperatorID         uint
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
	ID             string  `json:"id"`
	Kind           string  `json:"kind"` // CROSS_PROCESS | INTRA_PROCESS | EXTERNAL_UNREGISTERED
	SourceNodeID   string  `json:"source_node_id"`
	TargetNodeID   string  `json:"target_node_id"`
	SrcModuleID    uint64  `json:"src_module_id"`
	DstModuleID    *uint64 `json:"dst_module_id,omitempty"`
	SrcProcessID   *uint64 `json:"src_process_id,omitempty"`
	DstProcessID   *uint64 `json:"dst_process_id,omitempty"`
	ServiceName    string  `json:"service_name"`
	Operation      string  `json:"operation,omitempty"`
	ExternalSystem string  `json:"external_system,omitempty"`
}

// StaticTopologyDTO 静态拓扑响应载荷
type StaticTopologyDTO struct {
	View  string               `json:"view"`
	Nodes []StaticTopologyNode `json:"nodes"`
	Edges []StaticTopologyEdge `json:"edges"`
}

// CreateModuleService 创建对外供给服务契约并执行唯一性校验 (独占 A-05 或多实现 A-09, ADR-16)
func CreateModuleService(db *gorm.DB, in ModuleServiceInput) (*models.ModuleService, error) {
	var mod models.Module
	if err := db.First(&mod, in.ModuleID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrModuleNotFound
		}
		return nil, err
	}

	if mod.ModuleKind == models.ModuleKindContractLib {
		return nil, ErrContractLibNoProvide
	}

	diffPathsJSON, _ := json.Marshal(in.DiffIgnorePaths)
	if in.DiffIgnorePaths == nil {
		diffPathsJSON = []byte("[]")
	}

	// 1. 多实现契约实现分支 (in.ContractID != nil)
	if in.ContractID != nil && *in.ContractID > 0 {
		implKey := strings.TrimSpace(in.ImplKey)
		if !implKeyRegex.MatchString(implKey) {
			return nil, ErrInvalidImplKey
		}

		var created *models.ModuleService
		err := db.Transaction(func(tx *gorm.DB) error {
			var contract models.ServiceContract
			if err := tx.First(&contract, *in.ContractID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrServiceContractNotFound
				}
				return err
			}

			// 运行期实际发布的 CROS 服务名：留空则沿用契约名；允许用户自定义唯一名（如 DH_SyncService）
			publishedName := strings.TrimSpace(in.ServiceName)
			if publishedName == "" {
				publishedName = contract.ServiceName
			}
			if len(publishedName) == 0 || len(publishedName) > 63 || !serviceNameRegex.MatchString(publishedName) {
				return ErrInvalidServiceNameFormat
			}
			isCustom := publishedName != contract.ServiceName

			if err := lockServiceNamesSorted(tx, contract.ServiceName, publishedName); err != nil {
				return err
			}

			if isCustom {
				// 自定义运行期服务名不得与已有服务契约同名，且不得与独占/其他自定义服务名冲突
				var contractCount int64
				if err := tx.Model(&models.ServiceContract{}).Where("service_name = ?", publishedName).Count(&contractCount).Error; err != nil {
					return err
				}
				if contractCount > 0 {
					return ErrServiceNameOccupiedByContract
				}

				var existing models.ModuleService
				findErr := tx.Where("service_name = ? AND (contract_id IS NULL OR is_custom_service_name = true)", publishedName).First(&existing).Error
				if findErr == nil {
					var holder models.Module
					_ = tx.First(&holder, existing.ModuleID).Error
					return &ProvideConflictError{
						ServiceName:      publishedName,
						HolderModuleID:   holder.ID,
						HolderModuleCode: holder.Code,
						HolderModuleName: holder.Name,
					}
				} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
					return findErr
				}
			}

			// 校验同一契约下 impl_key 唯一
			var implCount int64
			if err := tx.Model(&models.ModuleService{}).
				Where("contract_id = ? AND impl_key = ?", *in.ContractID, implKey).
				Count(&implCount).Error; err != nil {
				return err
			}
			if implCount > 0 {
				return ErrDuplicateImplKey
			}

			// 校验同一模块对同一契约至多一条实现
			var modImplCount int64
			if err := tx.Model(&models.ModuleService{}).
				Where("contract_id = ? AND module_id = ?", *in.ContractID, in.ModuleID).
				Count(&modImplCount).Error; err != nil {
				return err
			}
			if modImplCount > 0 {
				return ErrModuleAlreadyImplementsContract
			}

			// 校验 A-09：仅当多个实现共享同一契约名 (is_custom_service_name = false) 时限制不能部署在同一进程；
			// 自定义唯一运行期服务名（如 DH_SyncService）豁免 A-09
			if !isCustom {
				if err := AssertContractImplProcessDisjoint(tx, in.ModuleID, in.ContractID, mod.ArchProcessID); err != nil {
					return err
				}
			}

			svc := &models.ModuleService{
				ModuleID:            in.ModuleID,
				ContractID:          in.ContractID,
				ServiceName:         publishedName,
				ImplKey:             implKey,
				IsCustomServiceName: isCustom,
				ContractServiceName: contract.ServiceName,
				DiffIgnorePaths:     datatypes.JSON(diffPathsJSON),
				Description:         in.Description,
				CreatedBy:           in.OperatorID,
			}
			if err := tx.Create(svc).Error; err != nil {
				if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "23505") {
					return ErrDuplicateModuleService
				}
				return err
			}
			created = svc
			return nil
		})

		if err != nil {
			return nil, err
		}
		return created, nil
	}

	// 2. 独占服务供给分支 (EXCLUSIVE, in.ContractID == nil)
	if strings.TrimSpace(in.ImplKey) != "" {
		return nil, ErrInvalidImplKey
	}
	serviceName := strings.TrimSpace(in.ServiceName)
	if len(serviceName) == 0 || len(serviceName) > 63 || !serviceNameRegex.MatchString(serviceName) {
		return nil, ErrInvalidServiceNameFormat
	}

	var created *models.ModuleService
	err := db.Transaction(func(tx *gorm.DB) error {
		// 咨询锁防并发竞态 (SEC-M20-03)
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", serviceName).Error; err != nil {
			return err
		}

		// 检查是否已被多实现契约占用
		var contractCount int64
		if err := tx.Model(&models.ServiceContract{}).Where("service_name = ?", serviceName).Count(&contractCount).Error; err != nil {
			return err
		}
		if contractCount > 0 {
			return ErrServiceNameOccupiedByContract
		}

		// 全局独占与自定义运行期服务名 PROVIDE 唯一性检查
		var existing models.ModuleService
		err := tx.Where("service_name = ? AND (contract_id IS NULL OR is_custom_service_name = true)", serviceName).First(&existing).Error
		if err == nil {
			var holder models.Module
			_ = tx.First(&holder, existing.ModuleID).Error
			return &ProvideConflictError{
				ServiceName:      serviceName,
				HolderModuleID:   holder.ID,
				HolderModuleCode: holder.Code,
				HolderModuleName: holder.Name,
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		svc := &models.ModuleService{
			ModuleID:        in.ModuleID,
			ServiceName:     serviceName,
			DiffIgnorePaths: datatypes.JSON(diffPathsJSON),
			Description:     in.Description,
			CreatedBy:       in.OperatorID,
		}

		if err := tx.Create(svc).Error; err != nil {
			if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "23505") {
				return ErrDuplicateModuleService
			}
			return err
		}
		created = svc
		return nil
	})

	if err != nil {
		return nil, err
	}
	return created, nil
}

// UpdateModuleService 更新供给服务忽略路径与描述（向后兼容旧签名）
func UpdateModuleService(db *gorm.DB, serviceID uint64, diffIgnorePaths []string, description string) (*models.ModuleService, *models.ModuleService, error) {
	return UpdateModuleServiceWithOptions(db, UpdateModuleServiceInput{
		ServiceID:       serviceID,
		DiffIgnorePaths: diffIgnorePaths,
		Description:     description,
	})
}

// UpdateModuleServiceWithOptions 更新供给服务（支持多实现调整运行期服务名/impl_key、忽略路径与描述）
func UpdateModuleServiceWithOptions(db *gorm.DB, in UpdateModuleServiceInput) (*models.ModuleService, *models.ModuleService, error) {
	var before models.ModuleService
	if err := db.First(&before, in.ServiceID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrModuleServiceNotFound
		}
		return nil, nil, err
	}

	diffPathsJSON, _ := json.Marshal(in.DiffIgnorePaths)
	if in.DiffIgnorePaths == nil {
		diffPathsJSON = []byte("[]")
	}

	var after models.ModuleService
	err := db.Transaction(func(tx *gorm.DB) error {
		newServiceName := before.ServiceName
		if in.ServiceName != nil {
			trimmed := strings.TrimSpace(*in.ServiceName)
			if before.ContractID != nil && trimmed == "" {
				// 多实现传空串回退为契约名
				var contract models.ServiceContract
				if err := tx.First(&contract, *before.ContractID).Error; err != nil {
					return err
				}
				trimmed = contract.ServiceName
			}
			if len(trimmed) == 0 || len(trimmed) > 63 || !serviceNameRegex.MatchString(trimmed) {
				return ErrInvalidServiceNameFormat
			}
			newServiceName = trimmed
		}

		newImplKey := before.ImplKey
		if in.ImplKey != nil {
			newImplKey = strings.TrimSpace(*in.ImplKey)
		}

		// 独占服务不允许修改服务名或设置 impl_key
		if before.ContractID == nil {
			if newServiceName != before.ServiceName {
				return ErrInvalidServiceNameFormat
			}
			if newImplKey != "" {
				return ErrInvalidImplKey
			}
			after = before
			after.DiffIgnorePaths = datatypes.JSON(diffPathsJSON)
			after.Description = in.Description
			return tx.Model(&models.ModuleService{}).Where("id = ?", in.ServiceID).Updates(map[string]interface{}{
				"diff_ignore_paths": datatypes.JSON(diffPathsJSON),
				"description":       in.Description,
			}).Error
		}

		// 多实现契约服务更新分支
		if !implKeyRegex.MatchString(newImplKey) {
			return ErrInvalidImplKey
		}

		var contract models.ServiceContract
		if err := tx.First(&contract, *before.ContractID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrServiceContractNotFound
			}
			return err
		}

		newIsCustom := newServiceName != contract.ServiceName
		if err := lockServiceNamesSorted(tx, contract.ServiceName, before.ServiceName, newServiceName); err != nil {
			return err
		}

		if newImplKey != before.ImplKey {
			var dupImpl int64
			if err := tx.Model(&models.ModuleService{}).
				Where("contract_id = ? AND impl_key = ? AND id != ?", *before.ContractID, newImplKey, before.ID).
				Count(&dupImpl).Error; err != nil {
				return err
			}
			if dupImpl > 0 {
				return ErrDuplicateImplKey
			}
		}

		if newIsCustom && newServiceName != before.ServiceName {
			var contractCount int64
			if err := tx.Model(&models.ServiceContract{}).Where("service_name = ?", newServiceName).Count(&contractCount).Error; err != nil {
				return err
			}
			if contractCount > 0 {
				return ErrServiceNameOccupiedByContract
			}

			var existing models.ModuleService
			findErr := tx.Where("service_name = ? AND (contract_id IS NULL OR is_custom_service_name = true) AND id != ?", newServiceName, before.ID).First(&existing).Error
			if findErr == nil {
				var holder models.Module
				_ = tx.First(&holder, existing.ModuleID).Error
				return &ProvideConflictError{
					ServiceName:      newServiceName,
					HolderModuleID:   holder.ID,
					HolderModuleCode: holder.Code,
					HolderModuleName: holder.Name,
				}
			} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
				return findErr
			}
		}

		// 从自定义名切换回共享契约名时，须校验 A-09 进程互斥
		if !newIsCustom && (before.IsCustomServiceName || newServiceName != before.ServiceName) {
			if err := AssertContractImplProcessDisjoint(tx, before.ModuleID, before.ContractID, nil); err != nil {
				return err
			}
		}

		after = before
		after.ServiceName = newServiceName
		after.ImplKey = newImplKey
		after.IsCustomServiceName = newIsCustom
		after.ContractServiceName = contract.ServiceName
		after.DiffIgnorePaths = datatypes.JSON(diffPathsJSON)
		after.Description = in.Description

		return tx.Model(&models.ModuleService{}).Where("id = ?", in.ServiceID).Updates(map[string]interface{}{
			"service_name":           newServiceName,
			"impl_key":               newImplKey,
			"is_custom_service_name": newIsCustom,
			"diff_ignore_paths":      datatypes.JSON(diffPathsJSON),
			"description":            in.Description,
		}).Error
	})
	if err != nil {
		return nil, nil, err
	}

	return &before, &after, nil
}

// DeleteModuleService 删除供给服务契约 (若已被下游模块依赖则返回 ErrServiceHasDependencies 拦截)
func DeleteModuleService(db *gorm.DB, serviceID uint64) (*models.ModuleService, error) {
	var svc models.ModuleService
	if err := db.First(&svc, serviceID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrModuleServiceNotFound
		}
		return nil, err
	}

	var depCount int64
	if err := db.Model(&models.ModuleDependency{}).Where("service_id = ?", serviceID).Count(&depCount).Error; err != nil {
		return nil, err
	}
	if depCount > 0 {
		return nil, ErrServiceHasDependencies
	}

	if err := db.Delete(&svc).Error; err != nil {
		return nil, err
	}
	return &svc, nil
}

// ListModuleServices 获取模块提供的服务，并统计被依赖数量
func ListModuleServices(db *gorm.DB, moduleID uint64) ([]models.ModuleService, error) {
	var services []models.ModuleService
	if err := db.Preload("Contract.DefinerModule").Where("module_id = ?", moduleID).Order("id ASC").Find(&services).Error; err != nil {
		return nil, err
	}

	for i := range services {
		if services[i].Contract != nil {
			services[i].ContractServiceName = services[i].Contract.ServiceName
		}
		var deps []models.ModuleDependency
		_ = db.Preload("ConsumerModule.Subsystem").Where("service_id = ?", services[i].ID).Find(&deps).Error
		services[i].DownstreamCount = len(deps)
		modNames := make([]string, 0, len(deps))
		consumers := make([]models.DownstreamConsumer, 0, len(deps))
		for _, d := range deps {
			if d.ConsumerModule != nil {
				subName := ""
				if d.ConsumerModule.Subsystem != nil {
					subName = d.ConsumerModule.Subsystem.Name
				}
				name := d.ConsumerModule.Name
				if subName != "" {
					name = fmt.Sprintf("%s (%s)", d.ConsumerModule.Name, subName)
				}
				modNames = append(modNames, name)
				consumers = append(consumers, models.DownstreamConsumer{
					ModuleID:      d.ConsumerModule.ID,
					ModuleName:    d.ConsumerModule.Name,
					ModuleCode:    d.ConsumerModule.Code,
					SubsystemName: subName,
				})
			}
		}
		services[i].DownstreamModules = modNames
		services[i].DownstreamConsumers = consumers
	}
	return services, nil
}

// CreateModuleDependency 创建模块对下游服务的依赖 (支持内部具体服务、多实现契约分发或外部三方系统)
func CreateModuleDependency(db *gorm.DB, in ModuleDependencyInput) (*models.ModuleDependency, error) {
	var consumer models.Module
	if err := db.First(&consumer, in.ConsumerModuleID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrModuleNotFound
		}
		return nil, err
	}

	if consumer.ModuleKind == models.ModuleKindContractLib {
		return nil, ErrContractLibNoDepend
	}

	dep := &models.ModuleDependency{
		ConsumerModuleID: in.ConsumerModuleID,
		IsExternal:       in.IsExternal,
		Description:      in.Description,
		CreatedBy:        in.OperatorID,
	}

	if in.IsExternal {
		extSys := strings.TrimSpace(in.ExternalSystem)
		extSvc := strings.TrimSpace(in.ExternalService)
		if extSys == "" && extSvc == "" {
			return nil, ErrInvalidExternalDependency
		}
		dep.ExternalSystem = extSys
		dep.ExternalService = extSvc
	} else if in.ContractID != nil && *in.ContractID > 0 {
		// 分发依赖 (DISPATCH, ADR-16 §3.5)
		var contract models.ServiceContract
		if err := db.First(&contract, *in.ContractID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrServiceContractNotFound
			}
			return nil, err
		}

		// 检查是否已存在对该契约的具体实现依赖（语义重复拦截）
		var conflictCount int64
		if err := db.Table("traefik_module_dependencies d").
			Joins("JOIN traefik_module_services s ON s.id = d.service_id").
			Where("d.consumer_module_id = ? AND s.contract_id = ?", in.ConsumerModuleID, *in.ContractID).
			Count(&conflictCount).Error; err != nil {
			return nil, err
		}
		if conflictCount > 0 {
			return nil, ErrDuplicateContractDependency
		}

		dep.ContractID = in.ContractID
	} else {
		if in.ServiceID == nil || *in.ServiceID == 0 {
			return nil, errors.New("内部服务依赖必须指定目标 service_id 或 contract_id")
		}
		var targetSvc models.ModuleService
		if err := db.First(&targetSvc, *in.ServiceID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrModuleServiceNotFound
			}
			return nil, err
		}

		if targetSvc.ModuleID == in.ConsumerModuleID {
			return nil, ErrSelfDependency
		}

		// 若目标服务属于多实现契约，检查消费方是否已声明了该契约的分发依赖（语义重复拦截）
		if targetSvc.ContractID != nil {
			var dispatchCount int64
			if err := db.Model(&models.ModuleDependency{}).
				Where("consumer_module_id = ? AND contract_id = ?", in.ConsumerModuleID, *targetSvc.ContractID).
				Count(&dispatchCount).Error; err != nil {
				return nil, err
			}
			if dispatchCount > 0 {
				return nil, ErrDuplicateContractDependency
			}
		}

		dep.ServiceID = in.ServiceID
		dep.ProviderModuleID = &targetSvc.ModuleID
	}

	if err := db.Create(dep).Error; err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "23505") {
			return nil, ErrDuplicateDependency
		}
		return nil, err
	}

	return dep, nil
}

// DeleteModuleDependency 解除模块依赖关系
func DeleteModuleDependency(db *gorm.DB, depID uint64) (*models.ModuleDependency, error) {
	var dep models.ModuleDependency
	if err := db.First(&dep, depID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrModuleDependencyNotFound
		}
		return nil, err
	}

	if err := db.Delete(&dep).Error; err != nil {
		return nil, err
	}
	return &dep, nil
}

// ListModuleDependencies 获取指定模块的所有下游依赖
func ListModuleDependencies(db *gorm.DB, moduleID uint64) ([]models.ModuleDependency, error) {
	var deps []models.ModuleDependency
	if err := db.Preload("Service.Contract").
		Preload("Contract.DefinerModule").
		Preload("ProviderModule").
		Preload("ProviderModule.Subsystem").
		Preload("ProviderModule.ArchProcess").
		Where("consumer_module_id = ?", moduleID).
		Order("id ASC").Find(&deps).Error; err != nil {
		return nil, err
	}
	return deps, nil
}

// ListAvailableServices 查询系统中所有可供依赖的内部服务清单 (排除自身)
func ListAvailableServices(db *gorm.DB, excludeModuleID uint64) ([]AvailableServiceDTO, error) {
	var services []models.ModuleService
	query := db.Preload("Contract").Preload("Module").Preload("Module.Subsystem")
	if excludeModuleID > 0 {
		query = query.Where("module_id != ?", excludeModuleID)
	}
	if err := query.Order("id ASC").Find(&services).Error; err != nil {
		return nil, err
	}

	result := make([]AvailableServiceDTO, 0, len(services))
	for _, s := range services {
		item := AvailableServiceDTO{
			ServiceID:           s.ID,
			ServiceName:         s.ServiceName,
			ContractID:          s.ContractID,
			ImplKey:             s.ImplKey,
			IsCustomServiceName: s.IsCustomServiceName,
			IsMultiImpl:         s.ContractID != nil && *s.ContractID > 0,
			ModuleID:            s.ModuleID,
			Description:         s.Description,
		}
		if s.Contract != nil {
			item.ContractServiceName = s.Contract.ServiceName
		}
		if s.Module != nil {
			item.ModuleCode = s.Module.Code
			item.ModuleName = s.Module.Name
			item.SubsystemID = s.Module.SubsystemID
			if s.Module.Subsystem != nil {
				item.SubsystemName = s.Module.Subsystem.Name
			}
		}
		result = append(result, item)
	}
	return result, nil
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

// CreateProcess 创建进程并挂载架构进程 (多环境 1:1 挂载模型)
func CreateProcess(db *gorm.DB, in ProcessInput) (*models.Process, error) {
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
					Scope:       models.ScopeShared,
					Status:      models.AgentStatusOnline,
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

	if in.ListenPort < 0 || in.ListenPort > 65535 {
		return nil, ErrInvalidListenPort
	}
	if in.OverrideListenPort != nil && (*in.OverrideListenPort < 0 || *in.OverrideListenPort > 65535) {
		return nil, ErrInvalidListenPort
	}

	// 1. 查找或创建 ArchProcess
	var archProc models.ArchProcess
	if in.ArchProcessID > 0 {
		if err := db.First(&archProc, in.ArchProcessID).Error; err != nil {
			return nil, errors.New("架构进程不存在")
		}
	} else {
		name := strings.TrimSpace(in.Name)
		if name == "" {
			return nil, ErrInvalidProcessName
		}
		if err := db.Where("name = ?", name).First(&archProc).Error; err != nil {
			groupName := strings.TrimSpace(in.GroupName)
			if groupName == "" {
				groupName = "默认进程组"
			}
			var pgrp models.ArchProcessGroup
			if err := db.Where("name = ?", groupName).First(&pgrp).Error; err != nil {
				pgrp = models.ArchProcessGroup{
					Code: fmt.Sprintf("PGRP_%s", strings.ToUpper(strings.ReplaceAll(groupName, " ", "_"))),
					Name: groupName,
				}
				if err := db.Create(&pgrp).Error; err != nil {
					return nil, err
				}
			}
			cmdPattern := strings.TrimSpace(in.CmdPattern)
			if cmdPattern == "" {
				cmdPattern = "./bin/" + name
			}
			archProc = models.ArchProcess{
				GroupID:           pgrp.ID,
				Code:              fmt.Sprintf("PROC_%s", strings.ToUpper(strings.ReplaceAll(name, " ", "_"))),
				Name:              name,
				DefaultListenPort: in.ListenPort,
				DefaultCmdPattern: cmdPattern,
			}
			if err := db.Create(&archProc).Error; err != nil {
				return nil, err
			}
		}
		in.ArchProcessID = archProc.ID
	}

	// 2. 检查同一部署域内模块是否重复挂载 (A-03)
	if agent.Scope == models.ScopeShared && len(in.ModuleIDs) > 0 {
		var conflictModCount int64
		if err := db.Model(&models.Module{}).
			Where("id IN ? AND arch_process_id IS NOT NULL AND arch_process_id != ?", in.ModuleIDs, archProc.ID).
			Count(&conflictModCount).Error; err != nil {
			return nil, err
		}
		if conflictModCount > 0 {
			return nil, ErrDuplicateModuleBinding
		}
	}

	// 3. 计算 EffectiveListenPort 与端口/1:1约束
	effectiveListenPort := archProc.DefaultListenPort
	if in.OverrideListenPort != nil {
		effectiveListenPort = *in.OverrideListenPort
	} else if in.ListenPort > 0 && in.ListenPort != archProc.DefaultListenPort {
		effectiveListenPort = in.ListenPort
		in.OverrideListenPort = &in.ListenPort
	}

	if effectiveListenPort < 0 || effectiveListenPort > 65535 {
		return nil, ErrInvalidListenPort
	}

	if effectiveListenPort > 0 {
		var portCount int64
		if err := db.Model(&models.Process{}).Where("agent_id = ? AND effective_listen_port = ?", in.AgentID, effectiveListenPort).Count(&portCount).Error; err != nil {
			return nil, err
		}
		if portCount > 0 {
			return nil, ErrDuplicateProcessPort
		}
	}

	var archMountCount int64
	if err := db.Model(&models.Process{}).Where("agent_id = ? AND arch_process_id = ?", in.AgentID, in.ArchProcessID).Count(&archMountCount).Error; err != nil {
		return nil, err
	}
	if archMountCount > 0 {
		return nil, ErrDuplicateProcessName
	}

	extraJSON, _ := json.Marshal(in.ExtraDownstreams)
	if in.ExtraDownstreams == nil {
		extraJSON = []byte("[]")
	}

	desiredState := in.ProbeDesiredState
	if desiredState == "" {
		desiredState = models.ProbeDesiredStateArmed
	}

	proc := &models.Process{
		AgentID:             in.AgentID,
		ArchProcessID:       in.ArchProcessID,
		OverrideListenPort:  in.OverrideListenPort,
		EffectiveListenPort: effectiveListenPort,
		OverrideCmdPattern:  strings.TrimSpace(in.OverrideCmdPattern),
		AdvertiseIP:         strings.TrimSpace(in.AdvertiseIP),
		ExtraDownstreams:    datatypes.JSON(extraJSON),
		Description:         in.Description,
		ProbeDesiredState:   desiredState,
		ProbeStatus:         models.ProbeStatusReady,
		CreatedBy:           in.OperatorID,
		UpdatedBy:           in.OperatorID,
	}
	if in.CmdPattern != "" && in.CmdPattern != archProc.DefaultCmdPattern && proc.OverrideCmdPattern == "" {
		proc.OverrideCmdPattern = in.CmdPattern
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(proc).Error; err != nil {
			if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "23505") {
				return ErrDuplicateProcessPort
			}
			return err
		}

		if agent.Scope == models.ScopeShared && len(in.ModuleIDs) > 0 {
			if err := tx.Model(&models.Module{}).Where("id IN ?", in.ModuleIDs).Update("arch_process_id", archProc.ID).Error; err != nil {
				return err
			}
		}

		// 递增 Agent.MountsRevision
		return tx.Model(&models.Agent{}).Where("id = ?", in.AgentID).
			Update("mounts_revision", gorm.Expr("mounts_revision + 1")).Error
	})
	if err != nil {
		return nil, err
	}

	proc.ArchProcess = &archProc
	proc.FillFromArchProcess()
	return proc, nil
}

// UpdateProcess 更新进程挂载配置
func UpdateProcess(db *gorm.DB, procID uint64, in ProcessInput) (*models.Process, *models.Process, error) {
	for _, ep := range in.ExtraDownstreams {
		if err := validateIPv4Endpoint(ep); err != nil {
			return nil, nil, err
		}
	}

	var before models.Process
	if err := db.Preload("Agent").Preload("ArchProcess.Group").First(&before, procID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrProcessNotFound
		}
		return nil, nil, err
	}
	before.FillFromArchProcess()

	if before.Agent != nil && before.Agent.LeaseType != "" {
		return nil, nil, ErrAgentHasActiveLease
	}

	after := before
	effectiveListenPort := before.EffectiveListenPort
	if in.OverrideListenPort != nil {
		effectiveListenPort = *in.OverrideListenPort
		after.OverrideListenPort = in.OverrideListenPort
	} else if before.ArchProcess != nil {
		if in.ListenPort == before.ArchProcess.DefaultListenPort {
			effectiveListenPort = in.ListenPort
			after.OverrideListenPort = nil
		} else if in.ListenPort >= 0 {
			effectiveListenPort = in.ListenPort
			after.OverrideListenPort = &in.ListenPort
		}
	}

	if effectiveListenPort < 0 || effectiveListenPort > 65535 {
		return nil, nil, ErrInvalidListenPort
	}

	if effectiveListenPort > 0 {
		var portCount int64
		if err := db.Model(&models.Process{}).Where("agent_id = ? AND id != ? AND effective_listen_port = ?", before.AgentID, procID, effectiveListenPort).Count(&portCount).Error; err != nil {
			return nil, nil, err
		}
		if portCount > 0 {
			return nil, nil, ErrDuplicateProcessPort
		}
	}

	extraJSON, _ := json.Marshal(in.ExtraDownstreams)
	if in.ExtraDownstreams == nil {
		extraJSON = []byte("[]")
	}

	after.EffectiveListenPort = effectiveListenPort
	if in.OverrideCmdPattern != "" {
		after.OverrideCmdPattern = in.OverrideCmdPattern
	} else if in.CmdPattern != "" {
		after.OverrideCmdPattern = in.CmdPattern
	}
	after.AdvertiseIP = strings.TrimSpace(in.AdvertiseIP)
	after.ExtraDownstreams = datatypes.JSON(extraJSON)
	if in.ProbeDesiredState != "" {
		after.ProbeDesiredState = in.ProbeDesiredState
	}
	after.Description = in.Description
	after.UpdatedBy = in.OperatorID

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Process{}).Where("id = ?", procID).Updates(map[string]interface{}{
			"override_listen_port":  after.OverrideListenPort,
			"effective_listen_port": after.EffectiveListenPort,
			"override_cmd_pattern":  after.OverrideCmdPattern,
			"advertise_ip":          after.AdvertiseIP,
			"extra_downstreams":     after.ExtraDownstreams,
			"probe_desired_state":   after.ProbeDesiredState,
			"description":           after.Description,
			"updated_by":            after.UpdatedBy,
		}).Error; err != nil {
			return err
		}

		if len(in.ModuleIDs) > 0 && before.ArchProcessID > 0 {
			if err := tx.Model(&models.Module{}).Where("id IN ?", in.ModuleIDs).Update("arch_process_id", before.ArchProcessID).Error; err != nil {
				return err
			}
		}

		return tx.Model(&models.Agent{}).Where("id = ?", before.AgentID).
			Update("mounts_revision", gorm.Expr("mounts_revision + 1")).Error
	})
	if err != nil {
		return nil, nil, err
	}

	after.FillFromArchProcess()
	return &before, &after, nil
}

// DeleteProcess 卸载/删除进程挂载实例 (F-12.2 解耦重放，置空任务外键)
func DeleteProcess(db *gorm.DB, procID uint64) (*models.Process, error) {
	var proc models.Process
	if err := db.Preload("Agent").Preload("ArchProcess").First(&proc, procID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrProcessNotFound
		}
		return nil, err
	}
	proc.FillFromArchProcess()

	if proc.Agent != nil && proc.Agent.LeaseType != "" {
		return nil, ErrAgentHasActiveLease
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		// 置空历史重放任务引用 (ON DELETE SET NULL 软解耦)
		if err := tx.Model(&models.ReplayTask{}).Where("source_process_id = ?", procID).Update("source_process_id", nil).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.ReplayTask{}).Where("target_process_id = ?", procID).Update("target_process_id", nil).Error; err != nil {
			return err
		}

		if err := tx.Delete(&proc).Error; err != nil {
			return err
		}

		return tx.Model(&models.Agent{}).Where("id = ?", proc.AgentID).
			Update("mounts_revision", gorm.Expr("mounts_revision + 1")).Error
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
	if err := db.Preload("ArchProcess").Joins("JOIN traefik_agents ON traefik_agents.id = traefik_processes.agent_id").
		Where("traefik_agents.scope = ?", models.ScopeShared).
		Order("traefik_processes.id ASC").Find(&processes).Error; err != nil {
		return nil, err
	}
	for i := range processes {
		processes[i].FillFromArchProcess()
	}

	var allModulesList []models.Module
	_ = db.Where("arch_process_id IS NOT NULL").Find(&allModulesList).Error
	archModMap := make(map[uint64][]uint64)
	for _, m := range allModulesList {
		if m.ArchProcessID != nil {
			archModMap[*m.ArchProcessID] = append(archModMap[*m.ArchProcessID], m.ID)
		}
	}
	procModulesMap := make(map[uint64][]uint64) // processID -> []moduleID
	moduleProcMap := make(map[uint64]uint64)    // moduleID -> processID
	for _, p := range processes {
		if mIDs, ok := archModMap[p.ArchProcessID]; ok {
			procModulesMap[p.ID] = append(procModulesMap[p.ID], mIDs...)
			for _, mid := range mIDs {
				moduleProcMap[mid] = p.ID
			}
		}
	}

	var dependencies []models.ModuleDependency
	if err := db.Preload("Service").Preload("ProviderModule").Find(&dependencies).Error; err != nil {
		return nil, err
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
			ProcessID:    m.ArchProcessID,
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
				ListenPort:   p.EffectiveListenPort,
				ModuleIDs:    procModulesMap[p.ID],
			})
		}
	}

	// 构建依赖边
	edgeSet := make(map[string]bool)

	for _, dep := range dependencies {
		srcModID := dep.ConsumerModuleID
		srcProcIDVal, hasSrcProc := moduleProcMap[srcModID]
		var srcProcID *uint64
		if hasSrcProc {
			srcProcID = &srcProcIDVal
		}

		if dep.IsExternal || dep.ServiceID == nil || dep.Service == nil {
			// 指向外部系统
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

			svcLabel := dep.ExternalService
			if svcLabel == "" {
				svcLabel = "ExternalService"
			}
			edgeID := fmt.Sprintf("edge_ext_%d_%s", srcModID, svcLabel)
			if !edgeSet[edgeID] {
				edgeSet[edgeID] = true
				edges = append(edges, StaticTopologyEdge{
					ID:             edgeID,
					Kind:           EdgeKindExternalUnregistered,
					SourceNodeID:   srcNodeID,
					TargetNodeID:   tgtNodeID,
					SrcModuleID:    srcModID,
					SrcProcessID:   srcProcID,
					ServiceName:    svcLabel,
					ExternalSystem: dep.ExternalSystem,
				})
			}
			continue
		}

		dstModID := dep.Service.ModuleID
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

		edgeID := fmt.Sprintf("edge_%d_%d_%s", srcModID, dstModID, dep.Service.ServiceName)
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
				ServiceName:  dep.Service.ServiceName,
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

// CompileProcessDownstreams 四层聚合编译进程下游依赖端点 (F-02.3, ADR-09)
// 优先级策略：① 同机挂载优先 -> ② FallbackAgentID 绑定 SHARED 环境兜底（多套 SHARED 环境未绑定时绝不串绑）
// -> ③ 同机全部已挂载非零端口 (满足 ArmedDownstreams ⊇ SingleAgentCapturingDownstreams 超集不变量) + ExtraDownstreams + ReplayEgress
func CompileProcessDownstreams(db *gorm.DB, procID uint64, batchProcessIDs []uint64, replayEgress []agentproto.DownstreamEndpoint) ([]agentproto.DownstreamEndpoint, error) {
	var proc models.Process
	if err := db.Preload("Agent").Preload("ArchProcess").First(&proc, procID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrProcessNotFound
		}
		return nil, err
	}
	proc.FillFromArchProcess()

	// 1. 契约声明下游：从所承载模块的 DEPEND 依赖反查提供该服务的 PROVIDE 模块及对应架构进程
	var declaredEndpoints []agentproto.DownstreamEndpoint

	var modules []models.Module
	_ = db.Where("arch_process_id = ?", proc.ArchProcessID).Find(&modules).Error
	moduleIDs := make([]uint64, 0, len(modules))
	for _, m := range modules {
		moduleIDs = append(moduleIDs, m.ID)
	}

	if len(moduleIDs) > 0 {
		var deps []models.ModuleDependency
		_ = db.Preload("ProviderModule.ArchProcess").Where("consumer_module_id IN ? AND is_external = false", moduleIDs).Find(&deps).Error

		// 检查系统中 SHARED Agent 数量
		var sharedAgents []models.Agent
		_ = db.Where("scope = ?", models.ScopeShared).Find(&sharedAgents).Error

		addEndpointsForTargetArchPID := func(targetArchPID uint64, archCode string) {
			if targetArchPID == 0 {
				return
			}
			// 优先检查同机挂载 (同一 AgentID)
			var tgtProc models.Process
			err := db.Preload("Agent").Where("agent_id = ? AND arch_process_id = ?", proc.AgentID, targetArchPID).First(&tgtProc).Error
			if err == nil && tgtProc.EffectiveListenPort > 0 {
				tgtIP := extractHostFromEndpoint(proc.Agent.Endpoint)
				if tgtProc.AdvertiseIP != "" {
					tgtIP = tgtProc.AdvertiseIP
				}
				if tgtIP != "" {
					declaredEndpoints = append(declaredEndpoints, agentproto.DownstreamEndpoint{
						IP:     tgtIP,
						Port:   uint16(tgtProc.EffectiveListenPort),
						DstTag: archCode,
					})
				}
				declaredEndpoints = append(declaredEndpoints, agentproto.DownstreamEndpoint{
					IP:     "127.0.0.1",
					Port:   uint16(tgtProc.EffectiveListenPort),
					DstTag: archCode,
				})
				return
			}

			// 同机未挂载：检查 FallbackAgentID 兜底环境
			var fallbackAgentID uint64
			if proc.Agent != nil && proc.Agent.FallbackAgentID != nil && *proc.Agent.FallbackAgentID > 0 {
				fallbackAgentID = *proc.Agent.FallbackAgentID
			} else if len(sharedAgents) == 1 {
				// 仅当系统中恰好存在唯一 1 套 SHARED Agent 时作为默认 fallback
				fallbackAgentID = sharedAgents[0].ID
			}
			// 若 len(sharedAgents) >= 2 且未指定 FallbackAgentID，严格禁止跨环境串绑

			if fallbackAgentID > 0 {
				var fbProc models.Process
				err := db.Preload("Agent").Where("agent_id = ? AND arch_process_id = ?", fallbackAgentID, targetArchPID).First(&fbProc).Error
				if err == nil && fbProc.EffectiveListenPort > 0 && fbProc.Agent != nil {
					tgtIP := extractHostFromEndpoint(fbProc.Agent.Endpoint)
					if fbProc.AdvertiseIP != "" {
						tgtIP = fbProc.AdvertiseIP
					}
					if tgtIP != "" {
						declaredEndpoints = append(declaredEndpoints, agentproto.DownstreamEndpoint{
							IP:     tgtIP,
							Port:   uint16(fbProc.EffectiveListenPort),
							DstTag: archCode,
						})
					}
				}
			}
		}

		for _, dep := range deps {
			if dep.ContractID != nil {
				// DISPATCH 契约依赖：查找实现该契约的所有实现模块及所属 ArchProcess
				var implSvcs []models.ModuleService
				_ = db.Preload("Module.ArchProcess").Where("contract_id = ?", *dep.ContractID).Find(&implSvcs).Error
				for _, isvc := range implSvcs {
					if isvc.Module != nil && isvc.Module.ArchProcessID != nil && *isvc.Module.ArchProcessID > 0 {
						code := ""
						if isvc.Module.ArchProcess != nil {
							code = isvc.Module.ArchProcess.Code
						}
						addEndpointsForTargetArchPID(*isvc.Module.ArchProcessID, code)
					}
				}
				continue
			}

			var targetArchPID uint64
			var archCode string
			if dep.ProviderModule != nil && dep.ProviderModule.ArchProcessID != nil {
				targetArchPID = *dep.ProviderModule.ArchProcessID
				if dep.ProviderModule.ArchProcess != nil {
					archCode = dep.ProviderModule.ArchProcess.Code
				}
			} else if dep.ProviderModuleID != nil {
				var pm models.Module
				if err := db.Preload("ArchProcess").First(&pm, *dep.ProviderModuleID).Error; err == nil && pm.ArchProcessID != nil {
					targetArchPID = *pm.ArchProcessID
					if pm.ArchProcess != nil {
						archCode = pm.ArchProcess.Code
					}
				}
			} else if dep.ServiceID != nil {
				var svc models.ModuleService
				if err := db.First(&svc, *dep.ServiceID).Error; err == nil {
					var pm models.Module
					if err := db.Preload("ArchProcess").First(&pm, svc.ModuleID).Error; err == nil && pm.ArchProcessID != nil {
						targetArchPID = *pm.ArchProcessID
						if pm.ArchProcess != nil {
							archCode = pm.ArchProcess.Code
						}
					}
				}
			}
			addEndpointsForTargetArchPID(targetArchPID, archCode)
		}
	}

	// 2. 同机全部已挂载非零端口 (满足 ArmedDownstreams ⊇ SingleAgentCapturingDownstreams 超集不变量)
	var peerEndpoints []agentproto.DownstreamEndpoint
	var peerProcs []models.Process
	_ = db.Preload("Agent").Preload("ArchProcess").Where("agent_id = ? AND id != ? AND effective_listen_port > 0", proc.AgentID, proc.ID).Find(&peerProcs).Error
	for _, pp := range peerProcs {
		pIP := extractHostFromEndpoint(proc.Agent.Endpoint)
		if pp.AdvertiseIP != "" {
			pIP = pp.AdvertiseIP
		}
		pCode := ""
		if pp.ArchProcess != nil {
			pCode = pp.ArchProcess.Code
		}
		if pIP != "" {
			peerEndpoints = append(peerEndpoints, agentproto.DownstreamEndpoint{
				IP:     pIP,
				Port:   uint16(pp.EffectiveListenPort),
				DstTag: pCode,
			})
		}
		peerEndpoints = append(peerEndpoints, agentproto.DownstreamEndpoint{
			IP:     "127.0.0.1",
			Port:   uint16(pp.EffectiveListenPort),
			DstTag: pCode,
		})
	}

	// 补充批次协同进程端点 (如果提供)
	if len(batchProcessIDs) > 0 {
		var batchProcs []models.Process
		_ = db.Preload("Agent").Preload("ArchProcess").Where("id IN ? AND id != ?", batchProcessIDs, proc.ID).Find(&batchProcs).Error
		for _, bp := range batchProcs {
			if bp.EffectiveListenPort > 0 && bp.Agent != nil {
				bIP := extractHostFromEndpoint(bp.Agent.Endpoint)
				if bp.AdvertiseIP != "" {
					bIP = bp.AdvertiseIP
				}
				bCode := ""
				if bp.ArchProcess != nil {
					bCode = bp.ArchProcess.Code
				}
				if bIP != "" {
					peerEndpoints = append(peerEndpoints, agentproto.DownstreamEndpoint{
						IP:     bIP,
						Port:   uint16(bp.EffectiveListenPort),
						DstTag: bCode,
					})
				}
				if bp.AgentID == proc.AgentID {
					peerEndpoints = append(peerEndpoints, agentproto.DownstreamEndpoint{
						IP:     "127.0.0.1",
						Port:   uint16(bp.EffectiveListenPort),
						DstTag: bCode,
					})
				}
			}
		}
	}

	// 3. 补充外部固定下游: ArchProcess.DefaultExtraDownstreams + proc.ExtraDownstreams
	var extraEndpoints []agentproto.DownstreamEndpoint
	addExtras := func(raw datatypes.JSON) {
		if len(raw) > 0 {
			var eps []string
			if err := json.Unmarshal(raw, &eps); err == nil {
				for _, ep := range eps {
					host, portStr, err := net.SplitHostPort(strings.TrimSpace(ep))
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
		}
	}
	if proc.ArchProcess != nil {
		addExtras(proc.ArchProcess.DefaultExtraDownstreams)
	}
	addExtras(proc.ExtraDownstreams)

	// 4. 合并去重并剔除自身监听端口
	selfPort := uint16(proc.EffectiveListenPort)

	all := append(declaredEndpoints, peerEndpoints...)
	all = append(all, extraEndpoints...)
	all = append(all, replayEgress...)

	mergedMap := make(map[string]agentproto.DownstreamEndpoint)
	for _, ep := range all {
		if selfPort > 0 && ep.Port == selfPort {
			continue
		}
		addrKey := fmt.Sprintf("%s:%d", ep.IP, ep.Port)
		if existing, ok := mergedMap[addrKey]; !ok {
			mergedMap[addrKey] = ep
		} else if existing.DstTag == "" && ep.DstTag != "" {
			mergedMap[addrKey] = ep
		}
	}
	merged := make([]agentproto.DownstreamEndpoint, 0, len(mergedMap))
	for _, ep := range mergedMap {
		merged = append(merged, ep)
	}

	sort.Slice(merged, func(i, j int) bool {
		if merged[i].IP == merged[j].IP {
			if merged[i].Port == merged[j].Port {
				return merged[i].DstTag < merged[j].DstTag
			}
			return merged[i].Port < merged[j].Port
		}
		return merged[i].IP < merged[j].IP
	})

	return merged, nil
}

// CompileProcessEndpointSnapshots 编译会话涉及的全部物理进程端点快照 (3.2)
func CompileProcessEndpointSnapshots(
	ctx context.Context,
	db *gorm.DB,
	agentIDs []uint64,
	targetProcessIDs []uint64,
) ([]models.EndpointSnapshotItem, error) {
	if len(agentIDs) == 0 && len(targetProcessIDs) == 0 {
		return []models.EndpointSnapshotItem{}, nil
	}

	query := db.WithContext(ctx).Preload("Agent").Preload("ArchProcess.Modules")
	if len(agentIDs) > 0 && len(targetProcessIDs) > 0 {
		query = query.Where("agent_id IN ? OR id IN ?", agentIDs, targetProcessIDs)
	} else if len(agentIDs) > 0 {
		query = query.Where("agent_id IN ?", agentIDs)
	} else {
		query = query.Where("id IN ?", targetProcessIDs)
	}

	var procs []models.Process
	if err := query.Order("id ASC").Find(&procs).Error; err != nil {
		return nil, err
	}

	var snapshots []models.EndpointSnapshotItem
	seen := make(map[string]bool)

	for i := range procs {
		p := &procs[i]
		p.FillFromArchProcess()
		if p.EffectiveListenPort <= 0 {
			continue
		}

		var modIDs []uint64
		if p.ArchProcess != nil {
			for _, m := range p.ArchProcess.Modules {
				modIDs = append(modIDs, m.ID)
			}
		}

		var svcNames []string
		var contractIDs []uint64
		if len(modIDs) > 0 {
			var svcs []models.ModuleService
			if err := db.WithContext(ctx).Where("module_id IN ?", modIDs).Find(&svcs).Error; err == nil {
				svcNameSet := make(map[string]bool)
				contractSet := make(map[uint64]bool)
				for _, s := range svcs {
					sName := strings.TrimSpace(s.ServiceName)
					if sName != "" && !svcNameSet[sName] {
						svcNameSet[sName] = true
						svcNames = append(svcNames, sName)
					}
					if s.ContractID != nil && *s.ContractID > 0 && !contractSet[*s.ContractID] {
						contractSet[*s.ContractID] = true
						contractIDs = append(contractIDs, *s.ContractID)
					}
				}
			}
		}

		ips := make([]string, 0)
		if p.AdvertiseIP != "" {
			ips = append(ips, p.AdvertiseIP)
		} else if p.Agent != nil && p.Agent.Endpoint != "" {
			h := extractHostFromEndpoint(p.Agent.Endpoint)
			if h != "" {
				ips = append(ips, h)
			}
		}

		hasLocal := false
		for _, ip := range ips {
			if ip == "127.0.0.1" {
				hasLocal = true
				break
			}
		}
		if !hasLocal {
			ips = append(ips, "127.0.0.1")
		}

		for _, ip := range ips {
			key := fmt.Sprintf("%s:%d:%d", ip, p.EffectiveListenPort, p.ID)
			if seen[key] {
				continue
			}
			seen[key] = true

			snapshots = append(snapshots, models.EndpointSnapshotItem{
				Endpoint:      fmt.Sprintf("%s:%d", ip, p.EffectiveListenPort),
				IP:            ip,
				Port:          p.EffectiveListenPort,
				ArchProcessID: p.ArchProcessID,
				ProcessID:     p.ID,
				ModuleIDs:     modIDs,
				ServiceNames:  svcNames,
				ContractIDs:   contractIDs,
			})
		}
	}

	sort.Slice(snapshots, func(i, j int) bool {
		if snapshots[i].IP == snapshots[j].IP {
			if snapshots[i].Port == snapshots[j].Port {
				return snapshots[i].ProcessID < snapshots[j].ProcessID
			}
			return snapshots[i].Port < snapshots[j].Port
		}
		return snapshots[i].IP < snapshots[j].IP
	})

	return snapshots, nil
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
