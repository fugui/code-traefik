package topology

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"code-traefik/models"
	"gorm.io/gorm"
)

var (
	ErrServiceContractNotFound             = errors.New("多实现服务契约不存在")
	ErrInvalidContractServiceName          = errors.New("服务契约名称不合法（须为 1~63 位，且符合 {package}.{serviceName} 规范）")
	ErrDefinerNotContractLib               = errors.New("契约定义方必须为接口契约库模块 (CONTRACT_LIB)")
	ErrServiceNameOccupiedByExclusive      = errors.New("该服务已被存量独占服务占用，请使用一键转换或先解绑该服务")
	ErrServiceNameOccupiedByContract       = errors.New("该服务已被多实现契约占用，请作为该契约的实现进行声明")
	ErrInvalidImplKey                      = errors.New("实现标识 (impl_key) 不合法（须为 1~64 位字母、数字、点、下划线或短横线）")
	ErrDuplicateImplKey                    = errors.New("同一契约下已存在相同的实现标识 (impl_key)")
	ErrModuleAlreadyImplementsContract     = errors.New("同一模块对同一契约仅允许登记一条实现")
	ErrContractHasImplementations          = errors.New("该契约仍存在关联的实现模块，禁止删除")
	ErrContractHasDependencies             = errors.New("该契约仍存在关联的分发依赖，禁止删除")
	ErrContractImplProcessConflict         = errors.New("同一进程内不支持挂载同一服务契约的多个实现模块，请合入单一模块或拆分标准架构进程")
	ErrContractLibNoProcess                = errors.New("接口契约库模块 (CONTRACT_LIB) 禁止绑定架构进程")
	ErrContractLibNoProvide                = errors.New("接口契约库模块禁止声明直接提供服务 (PROVIDE)")
	ErrContractLibNoDepend                 = errors.New("接口契约库模块禁止声明服务依赖 (DEPEND)")
	ErrContractLibCannotSwitchToBusiness   = errors.New("该接口库模块已定义多实现契约，禁止变更为业务模块")
	ErrContractLibHasContractsCannotDelete = errors.New("该接口库模块仍存在已登记的服务契约，禁止删除")
	ErrNotExclusiveService                 = errors.New("该服务不是独占服务，无法转换为多实现契约")
	ErrDuplicateContractDependency         = errors.New("已存在对该契约的分发依赖或具体实现依赖")
)

var (
	contractServiceNameRegex = regexp.MustCompile(`^[A-Za-z0-9._-]{1,63}$`)
	implKeyRegex             = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
)

// ServiceContractInput 创建多实现服务契约入参
type ServiceContractInput struct {
	ServiceName         string
	DefinerModuleID     uint64
	ProvideMode         string
	ImplKeyLabel        string
	Description         string
	OperatorID          uint
	SkipProtoValidation bool
}

// UpdateServiceContractInput 更新多实现服务契约入参
type UpdateServiceContractInput struct {
	ImplKeyLabel string
	Description  string
	OperatorID   uint
}

// ConvertExclusiveContractInput 独占服务转多实现契约入参 (ADR-16 §7.4)
type ConvertExclusiveContractInput struct {
	ServiceID                   uint64
	DefinerModuleID             uint64
	ImplKeyLabel                string
	InitialImplKey              string
	ConvertDependentsToDispatch bool
	Description                 string
	OperatorID                  uint
}

// ServiceContractFilter 列表查询过滤器
type ServiceContractFilter struct {
	DefinerModuleID uint64
	ServiceName     string
	Search          string
	Page            int
	PageSize        int
}

// ServiceContractListItemDTO 列表项 DTO
type ServiceContractListItemDTO struct {
	ID                    uint64    `json:"id"`
	ServiceName           string    `json:"service_name"`
	DefinerModuleID       uint64    `json:"definer_module_id"`
	DefinerModuleCode     string    `json:"definer_module_code"`
	DefinerModuleName     string    `json:"definer_module_name"`
	SubsystemName         string    `json:"subsystem_name,omitempty"`
	ProvideMode           string    `json:"provide_mode"`
	ImplKeyLabel          string    `json:"impl_key_label"`
	Description           string    `json:"description"`
	ImplCount             int       `json:"impl_count"`
	DeployedImplCount     int       `json:"deployed_impl_count"`
	DispatchConsumerCount int       `json:"dispatch_consumer_count"`
	CreatedBy             uint      `json:"created_by"`
	UpdatedBy             uint      `json:"updated_by"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// ContractImplementationDTO 实现矩阵行 DTO
type ContractImplementationDTO struct {
	ServiceID       uint64    `json:"service_id"`
	ImplKey         string    `json:"impl_key"`
	ModuleID        uint64    `json:"module_id"`
	ModuleCode      string    `json:"module_code"`
	ModuleName      string    `json:"module_name"`
	SubsystemName   string    `json:"subsystem_name,omitempty"`
	ArchProcessID   *uint64   `json:"arch_process_id,omitempty"`
	ArchProcessCode string    `json:"arch_process_code,omitempty"`
	ArchProcessName string    `json:"arch_process_name,omitempty"`
	Owner           string    `json:"owner,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

// ContractDispatchConsumerDTO 分发消费方 DTO
type ContractDispatchConsumerDTO struct {
	DependencyID    uint64    `json:"dependency_id"`
	ModuleID        uint64    `json:"module_id"`
	ModuleCode      string    `json:"module_code"`
	ModuleName      string    `json:"module_name"`
	SubsystemName   string    `json:"subsystem_name,omitempty"`
	ArchProcessID   *uint64   `json:"arch_process_id,omitempty"`
	ArchProcessCode string    `json:"arch_process_code,omitempty"`
	ArchProcessName string    `json:"arch_process_name,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

// ServiceContractDetailDTO 契约详情 DTO
type ServiceContractDetailDTO struct {
	ID                uint64                        `json:"id"`
	ServiceName       string                        `json:"service_name"`
	DefinerModuleID   uint64                        `json:"definer_module_id"`
	DefinerModuleCode string                        `json:"definer_module_code"`
	DefinerModuleName string                        `json:"definer_module_name"`
	SubsystemName     string                        `json:"subsystem_name,omitempty"`
	ProvideMode       string                        `json:"provide_mode"`
	ImplKeyLabel      string                        `json:"impl_key_label"`
	Description       string                        `json:"description"`
	CreatedBy         uint                          `json:"created_by"`
	UpdatedBy         uint                          `json:"updated_by"`
	CreatedAt         time.Time                     `json:"created_at"`
	UpdatedAt         time.Time                     `json:"updated_at"`
	Implementations   []ContractImplementationDTO   `json:"implementations"`
	DispatchConsumers []ContractDispatchConsumerDTO `json:"dispatch_consumers"`
}

// AssertContractImplProcessDisjoint A-09 校验：同一契约的多实现模块所属标准架构进程必须两两不相交
// moduleID: 待校验的模块 ID
// contractID: 契约 ID（若非空则针对该契约校验；若为空则自动查询 moduleID 关联的所有多实现契约）
// targetArchProcID: 目标绑定的标准进程 ID（若为 nil 则取 moduleID 当前绑定的进程）
func AssertContractImplProcessDisjoint(db *gorm.DB, moduleID uint64, contractID *uint64, targetArchProcID *uint64) error {
	var procID uint64
	if targetArchProcID != nil && *targetArchProcID > 0 {
		procID = *targetArchProcID
	} else {
		var mod models.Module
		if err := db.Select("id, arch_process_id").First(&mod, moduleID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if mod.ArchProcessID == nil || *mod.ArchProcessID == 0 {
			return nil
		}
		procID = *mod.ArchProcessID
	}

	var contractIDs []uint64
	if contractID != nil && *contractID > 0 {
		contractIDs = []uint64{*contractID}
	} else {
		if err := db.Model(&models.ModuleService{}).
			Where("module_id = ? AND contract_id IS NOT NULL", moduleID).
			Pluck("contract_id", &contractIDs).Error; err != nil {
			return err
		}
	}

	if len(contractIDs) == 0 {
		return nil
	}

	// 检查同一 procID 下是否已有其他模块实现了其中任意一个契约
	var conflictCount int64
	err := db.Table("traefik_module_services ms").
		Joins("JOIN traefik_modules m ON m.id = ms.module_id").
		Where("ms.contract_id IN ? AND ms.module_id != ? AND m.arch_process_id = ?", contractIDs, moduleID, procID).
		Count(&conflictCount).Error
	if err != nil {
		return err
	}
	if conflictCount > 0 {
		return ErrContractImplProcessConflict
	}
	return nil
}

// CreateServiceContract 登记多实现服务契约
func CreateServiceContract(db *gorm.DB, in ServiceContractInput) (*models.ServiceContract, error) {
	serviceName := strings.TrimSpace(in.ServiceName)
	if len(serviceName) == 0 || len(serviceName) > 63 || !contractServiceNameRegex.MatchString(serviceName) {
		return nil, ErrInvalidContractServiceName
	}

	var definer models.Module
	if err := db.First(&definer, in.DefinerModuleID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrModuleNotFound
		}
		return nil, err
	}
	if definer.ModuleKind != models.ModuleKindContractLib {
		return nil, ErrDefinerNotContractLib
	}

	provideMode := strings.TrimSpace(in.ProvideMode)
	if provideMode == "" {
		provideMode = models.ContractProvideModeMultiImpl
	}

	var result *models.ServiceContract

	err := db.Transaction(func(tx *gorm.DB) error {
		// 1. 获取 service_name 对应的咨询锁，串行化跨表唯一性判定 (SEC-M20-03)
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", serviceName).Error; err != nil {
			return err
		}

		// 2. 检查独占服务表中是否已存在该 service_name
		var exclCount int64
		if err := tx.Model(&models.ModuleService{}).
			Where("service_name = ? AND contract_id IS NULL", serviceName).
			Count(&exclCount).Error; err != nil {
			return err
		}
		if exclCount > 0 {
			return ErrServiceNameOccupiedByExclusive
		}

		// 3. 检查契约表中是否已存在
		var contractCount int64
		if err := tx.Model(&models.ServiceContract{}).
			Where("service_name = ?", serviceName).
			Count(&contractCount).Error; err != nil {
			return err
		}
		if contractCount > 0 {
			return ErrServiceNameOccupiedByContract
		}

		contract := &models.ServiceContract{
			ServiceName:     serviceName,
			DefinerModuleID: in.DefinerModuleID,
			ProvideMode:     provideMode,
			ImplKeyLabel:    strings.TrimSpace(in.ImplKeyLabel),
			Description:     strings.TrimSpace(in.Description),
			CreatedBy:       in.OperatorID,
			UpdatedBy:       in.OperatorID,
		}

		if err := tx.Create(contract).Error; err != nil {
			if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "23505") {
				return ErrServiceNameOccupiedByContract
			}
			return err
		}

		result = contract
		return nil
	})

	if err != nil {
		return nil, err
	}
	return result, nil
}

// GetServiceContract 查询契约详情及其实现矩阵与分发消费方
func GetServiceContract(db *gorm.DB, id uint64) (*ServiceContractDetailDTO, error) {
	var contract models.ServiceContract
	if err := db.Preload("DefinerModule.Subsystem").First(&contract, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrServiceContractNotFound
		}
		return nil, err
	}

	subName := ""
	defCode := ""
	defName := ""
	if contract.DefinerModule != nil {
		defCode = contract.DefinerModule.Code
		defName = contract.DefinerModule.Name
		if contract.DefinerModule.Subsystem != nil {
			subName = contract.DefinerModule.Subsystem.Name
		}
	}

	detail := &ServiceContractDetailDTO{
		ID:                contract.ID,
		ServiceName:       contract.ServiceName,
		DefinerModuleID:   contract.DefinerModuleID,
		DefinerModuleCode: defCode,
		DefinerModuleName: defName,
		SubsystemName:     subName,
		ProvideMode:       contract.ProvideMode,
		ImplKeyLabel:      contract.ImplKeyLabel,
		Description:       contract.Description,
		CreatedBy:         contract.CreatedBy,
		UpdatedBy:         contract.UpdatedBy,
		CreatedAt:         contract.CreatedAt,
		UpdatedAt:         contract.UpdatedAt,
		Implementations:   make([]ContractImplementationDTO, 0),
		DispatchConsumers: make([]ContractDispatchConsumerDTO, 0),
	}

	// 1. 查询所有实现记录
	var implServices []models.ModuleService
	if err := db.Preload("Module.Subsystem").
		Preload("Module.ArchProcess").
		Preload("Module.Owner").
		Where("contract_id = ?", id).
		Order("impl_key ASC, id ASC").
		Find(&implServices).Error; err != nil {
		return nil, err
	}

	for _, svc := range implServices {
		item := ContractImplementationDTO{
			ServiceID: svc.ID,
			ImplKey:   svc.ImplKey,
			CreatedAt: svc.CreatedAt,
		}
		if svc.Module != nil {
			item.ModuleID = svc.Module.ID
			item.ModuleCode = svc.Module.Code
			item.ModuleName = svc.Module.Name
			if svc.Module.Subsystem != nil {
				item.SubsystemName = svc.Module.Subsystem.Name
			}
			if svc.Module.ArchProcess != nil {
				item.ArchProcessID = &svc.Module.ArchProcess.ID
				item.ArchProcessCode = svc.Module.ArchProcess.Code
				item.ArchProcessName = svc.Module.ArchProcess.Name
			}
			if svc.Module.Owner != nil {
				item.Owner = svc.Module.Owner.Username
			}
		}
		detail.Implementations = append(detail.Implementations, item)
	}

	// 2. 查询所有分发依赖
	var deps []models.ModuleDependency
	if err := db.Preload("ConsumerModule.Subsystem").
		Preload("ConsumerModule.ArchProcess").
		Where("contract_id = ?", id).
		Order("id ASC").
		Find(&deps).Error; err != nil {
		return nil, err
	}

	for _, d := range deps {
		item := ContractDispatchConsumerDTO{
			DependencyID: d.ID,
			CreatedAt:    d.CreatedAt,
		}
		if d.ConsumerModule != nil {
			item.ModuleID = d.ConsumerModule.ID
			item.ModuleCode = d.ConsumerModule.Code
			item.ModuleName = d.ConsumerModule.Name
			if d.ConsumerModule.Subsystem != nil {
				item.SubsystemName = d.ConsumerModule.Subsystem.Name
			}
			if d.ConsumerModule.ArchProcess != nil {
				item.ArchProcessID = &d.ConsumerModule.ArchProcess.ID
				item.ArchProcessCode = d.ConsumerModule.ArchProcess.Code
				item.ArchProcessName = d.ConsumerModule.ArchProcess.Name
			}
		}
		detail.DispatchConsumers = append(detail.DispatchConsumers, item)
	}

	return detail, nil
}

// ListServiceContracts 分页筛选多实现服务契约
func ListServiceContracts(db *gorm.DB, filter ServiceContractFilter) ([]ServiceContractListItemDTO, int64, error) {
	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 25
	}

	query := db.Model(&models.ServiceContract{}).Preload("DefinerModule.Subsystem")
	if filter.DefinerModuleID > 0 {
		query = query.Where("definer_module_id = ?", filter.DefinerModuleID)
	}
	if filter.ServiceName != "" {
		query = query.Where("service_name = ?", filter.ServiceName)
	}
	if filter.Search != "" {
		s := "%" + filter.Search + "%"
		query = query.Where("service_name ILIKE ? OR description ILIKE ?", s, s)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var contracts []models.ServiceContract
	offset := (page - 1) * pageSize
	if err := query.Order("id DESC").Offset(offset).Limit(pageSize).Find(&contracts).Error; err != nil {
		return nil, 0, err
	}

	if len(contracts) == 0 {
		return []ServiceContractListItemDTO{}, total, nil
	}

	contractIDs := make([]uint64, len(contracts))
	for i, c := range contracts {
		contractIDs[i] = c.ID
	}

	// 聚合统计实现数与部署数
	type implAgg struct {
		ContractID    uint64
		TotalCount    int
		DeployedCount int
	}
	var implAggs []implAgg
	_ = db.Table("traefik_module_services ms").
		Select("ms.contract_id, COUNT(ms.id) as total_count, COUNT(CASE WHEN m.arch_process_id IS NOT NULL THEN 1 END) as deployed_count").
		Joins("JOIN traefik_modules m ON m.id = ms.module_id").
		Where("ms.contract_id IN ?", contractIDs).
		Group("ms.contract_id").
		Scan(&implAggs)

	implMap := make(map[uint64]implAgg)
	for _, a := range implAggs {
		implMap[a.ContractID] = a
	}

	// 聚合统计分发消费方数
	type depAgg struct {
		ContractID uint64
		Count      int
	}
	var depAggs []depAgg
	_ = db.Table("traefik_module_dependencies").
		Select("contract_id, COUNT(id) as count").
		Where("contract_id IN ?", contractIDs).
		Group("contract_id").
		Scan(&depAggs)

	depMap := make(map[uint64]int)
	for _, a := range depAggs {
		depMap[a.ContractID] = a.Count
	}

	items := make([]ServiceContractListItemDTO, len(contracts))
	for i, c := range contracts {
		subName := ""
		defCode := ""
		defName := ""
		if c.DefinerModule != nil {
			defCode = c.DefinerModule.Code
			defName = c.DefinerModule.Name
			if c.DefinerModule.Subsystem != nil {
				subName = c.DefinerModule.Subsystem.Name
			}
		}
		agg := implMap[c.ID]
		items[i] = ServiceContractListItemDTO{
			ID:                    c.ID,
			ServiceName:           c.ServiceName,
			DefinerModuleID:       c.DefinerModuleID,
			DefinerModuleCode:     defCode,
			DefinerModuleName:     defName,
			SubsystemName:         subName,
			ProvideMode:           c.ProvideMode,
			ImplKeyLabel:          c.ImplKeyLabel,
			Description:           c.Description,
			ImplCount:             agg.TotalCount,
			DeployedImplCount:     agg.DeployedCount,
			DispatchConsumerCount: depMap[c.ID],
			CreatedBy:             c.CreatedBy,
			UpdatedBy:             c.UpdatedBy,
			CreatedAt:             c.CreatedAt,
			UpdatedAt:             c.UpdatedAt,
		}
	}

	return items, total, nil
}

// UpdateServiceContract 更新契约属性（标签与说明）
func UpdateServiceContract(db *gorm.DB, id uint64, in UpdateServiceContractInput) (*models.ServiceContract, *models.ServiceContract, error) {
	var before models.ServiceContract
	if err := db.First(&before, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrServiceContractNotFound
		}
		return nil, nil, err
	}

	after := before
	after.ImplKeyLabel = strings.TrimSpace(in.ImplKeyLabel)
	after.Description = strings.TrimSpace(in.Description)
	after.UpdatedBy = in.OperatorID

	if err := db.Model(&models.ServiceContract{}).Where("id = ?", id).Updates(map[string]interface{}{
		"impl_key_label": after.ImplKeyLabel,
		"description":    after.Description,
		"updated_by":     after.UpdatedBy,
	}).Error; err != nil {
		return nil, nil, err
	}

	return &before, &after, nil
}

// DeleteServiceContract 删除契约（存在关联实现或分发依赖时阻断）
func DeleteServiceContract(db *gorm.DB, id uint64) (*models.ServiceContract, error) {
	var contract models.ServiceContract
	if err := db.First(&contract, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrServiceContractNotFound
		}
		return nil, err
	}

	var implCount int64
	if err := db.Model(&models.ModuleService{}).Where("contract_id = ?", id).Count(&implCount).Error; err != nil {
		return nil, err
	}
	if implCount > 0 {
		return nil, ErrContractHasImplementations
	}

	var depCount int64
	if err := db.Model(&models.ModuleDependency{}).Where("contract_id = ?", id).Count(&depCount).Error; err != nil {
		return nil, err
	}
	if depCount > 0 {
		return nil, ErrContractHasDependencies
	}

	if err := db.Delete(&contract).Error; err != nil {
		return nil, err
	}

	return &contract, nil
}

// ConvertExclusiveToMultiImplContract 单事务将既有独占 ModuleService 升级为 ServiceContract + 首个实现 (ADR-16 §7.4)
func ConvertExclusiveToMultiImplContract(db *gorm.DB, in ConvertExclusiveContractInput) (*models.ServiceContract, *models.ModuleService, error) {
	implKey := strings.TrimSpace(in.InitialImplKey)
	if !implKeyRegex.MatchString(implKey) {
		return nil, nil, ErrInvalidImplKey
	}

	var definer models.Module
	if err := db.First(&definer, in.DefinerModuleID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrModuleNotFound
		}
		return nil, nil, err
	}
	if definer.ModuleKind != models.ModuleKindContractLib {
		return nil, nil, ErrDefinerNotContractLib
	}

	var targetSvc models.ModuleService
	if err := db.First(&targetSvc, in.ServiceID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrModuleServiceNotFound
		}
		return nil, nil, err
	}
	if targetSvc.ContractID != nil {
		return nil, nil, ErrNotExclusiveService
	}

	var createdContract *models.ServiceContract
	var updatedSvc *models.ModuleService

	err := db.Transaction(func(tx *gorm.DB) error {
		// 1. 获取 service_name 锁
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", targetSvc.ServiceName).Error; err != nil {
			return err
		}

		// 2. 检查契约是否已存在
		var contractCount int64
		if err := tx.Model(&models.ServiceContract{}).Where("service_name = ?", targetSvc.ServiceName).Count(&contractCount).Error; err != nil {
			return err
		}
		if contractCount > 0 {
			return ErrServiceNameOccupiedByContract
		}

		// 3. 创建 ServiceContract
		contract := &models.ServiceContract{
			ServiceName:     targetSvc.ServiceName,
			DefinerModuleID: in.DefinerModuleID,
			ProvideMode:     models.ContractProvideModeMultiImpl,
			ImplKeyLabel:    strings.TrimSpace(in.ImplKeyLabel),
			Description:     strings.TrimSpace(in.Description),
			CreatedBy:       in.OperatorID,
			UpdatedBy:       in.OperatorID,
		}
		if err := tx.Create(contract).Error; err != nil {
			return err
		}

		// 4. 更新存量 ModuleService 为首个实现记录（保留其原始 service_id）
		if err := tx.Model(&models.ModuleService{}).Where("id = ?", targetSvc.ID).Updates(map[string]interface{}{
			"contract_id": contract.ID,
			"impl_key":    implKey,
		}).Error; err != nil {
			return err
		}

		// 5. 若请求将既有依赖转换为 DISPATCH
		if in.ConvertDependentsToDispatch {
			if err := tx.Model(&models.ModuleDependency{}).
				Where("service_id = ?", targetSvc.ID).
				Updates(map[string]interface{}{
					"service_id":  nil,
					"contract_id": contract.ID,
				}).Error; err != nil {
				return err
			}
		}

		targetSvc.ContractID = &contract.ID
		targetSvc.ImplKey = implKey
		createdContract = contract
		updatedSvc = &targetSvc
		return nil
	})

	if err != nil {
		return nil, nil, err
	}
	return createdContract, updatedSvc, nil
}
