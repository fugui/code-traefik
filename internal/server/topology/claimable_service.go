package topology

import (
	"fmt"
	"sort"

	"code-traefik/models"

	"gorm.io/gorm"
)

// ClaimedModuleDTO 认领模块信息 DTO
type ClaimedModuleDTO struct {
	ID   string `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

// ClaimableProtoServiceDTO 可认领 Proto 服务 DTO
type ClaimableProtoServiceDTO struct {
	ServiceName     string            `json:"service_name"`
	ProtoPackage    string            `json:"proto_package"`
	ProtoService    string            `json:"proto_service"`
	SchemaID        uint64            `json:"schema_id"`
	SchemaName      string            `json:"schema_name"`
	OperationCount  int               `json:"operation_count"`
	Operations      []string          `json:"operations"`
	IsClaimed       bool              `json:"is_claimed"`
	ClaimedByModule *ClaimedModuleDTO `json:"claimed_by_module"`
}

// ListClaimableProtoServices 从已激活的 ProtoOperation 中聚合服务并关联已认领的 FM 信息
func ListClaimableProtoServices(db *gorm.DB) ([]ClaimableProtoServiceDTO, error) {
	// 1. 查询所有处于激活态的操作映射
	var ops []models.ProtoOperation
	if err := db.Where("is_active = true").Order("service_name ASC, operation ASC").Find(&ops).Error; err != nil {
		return nil, err
	}

	if len(ops) == 0 {
		return []ClaimableProtoServiceDTO{}, nil
	}

	// 2. 批量查询关联的已激活 ProtoSchema 名字
	schemaIDs := make([]uint64, 0)
	schemaIDMap := make(map[uint64]bool)
	for _, op := range ops {
		if !schemaIDMap[op.SchemaID] {
			schemaIDMap[op.SchemaID] = true
			schemaIDs = append(schemaIDs, op.SchemaID)
		}
	}

	schemaNameMap := make(map[uint64]string)
	if len(schemaIDs) > 0 {
		var schemas []models.ProtoSchema
		if err := db.Where("id IN ?", schemaIDs).Find(&schemas).Error; err != nil {
			return nil, err
		}
		for _, s := range schemas {
			name := s.Name
			if name == "" {
				name = s.FileName
			}
			schemaNameMap[s.ID] = name
		}
	}

	// 3. 查询当前已被 FM 声明的 ModuleService
	var modSvcs []models.ModuleService
	if err := db.Preload("Module").Find(&modSvcs).Error; err != nil {
		return nil, err
	}

	claimedMap := make(map[string]*models.Module)
	for _, ms := range modSvcs {
		claimedMap[ms.ServiceName] = ms.Module
	}

	// 4. 按 service_name 聚合与去重
	type serviceAgg struct {
		dto   ClaimableProtoServiceDTO
		opSet map[string]bool
	}
	aggMap := make(map[string]*serviceAgg)
	var orderList []string

	for _, op := range ops {
		svcName := op.ServiceName
		agg, exists := aggMap[svcName]
		if !exists {
			schName := schemaNameMap[op.SchemaID]
			agg = &serviceAgg{
				dto: ClaimableProtoServiceDTO{
					ServiceName:  svcName,
					ProtoPackage: op.ProtoPackage,
					ProtoService: op.ProtoService,
					SchemaID:     op.SchemaID,
					SchemaName:   schName,
					Operations:   make([]string, 0),
				},
				opSet: make(map[string]bool),
			}
			aggMap[svcName] = agg
			orderList = append(orderList, svcName)
		}

		if !agg.opSet[op.Operation] {
			agg.opSet[op.Operation] = true
			agg.dto.Operations = append(agg.dto.Operations, op.Operation)
		}
	}

	sort.Strings(orderList)

	items := make([]ClaimableProtoServiceDTO, 0, len(orderList))
	for _, svcName := range orderList {
		agg := aggMap[svcName]
		agg.dto.OperationCount = len(agg.dto.Operations)
		if mod, ok := claimedMap[svcName]; ok && mod != nil {
			agg.dto.IsClaimed = true
			agg.dto.ClaimedByModule = &ClaimedModuleDTO{
				ID:   fmt.Sprintf("%d", mod.ID),
				Code: mod.Code,
				Name: mod.Name,
			}
		} else {
			agg.dto.IsClaimed = false
			agg.dto.ClaimedByModule = nil
		}
		items = append(items, agg.dto)
	}

	return items, nil
}
