package topology

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"code-traefik/models"
	"gorm.io/gorm"
)

const (
	// MinLayerLevel 层级最小值（0 表示未约束 / 继承所属子系统层级）
	MinLayerLevel = 0
	// MaxLayerLevel 层级最大值（数值越大代表架构层级越高，仅允许向下或同级依赖）
	MaxLayerLevel = 99

	// ErrCodeInvalidLayerLevel 层级入参越界错误码
	ErrCodeInvalidLayerLevel = "INVALID_LAYER_LEVEL"
	// ErrCodeLayerViolation 分层依赖违规错误码 (consumer_eff_layer < provider_eff_layer)
	ErrCodeLayerViolation = "ERR_LAYER_VIOLATION"
	// ErrCodeRealizationLayerInversion 契约实现层级倒挂错误码 (impl_eff_layer < definer_eff_layer)
	ErrCodeRealizationLayerInversion = "ERR_REALIZATION_LAYER_INVERSION"
	// ErrCodeCyclicDependency 内部有向环依赖错误码
	ErrCodeCyclicDependency = "ERR_CYCLIC_DEPENDENCY"
)

// ErrInvalidLayerLevel 表示层级配置超出 0~99 允许范围
var ErrInvalidLayerLevel = errors.New("架构层级 (layer_level) 必须为 0~99 之间的整数（0 表示继承或不限制）")

// LayerViolationError 表示违反单向向下依赖或契约实现层级约束的 409 冲突错误
type LayerViolationError struct {
	Code          string `json:"code"`
	Message       string `json:"error"`
	ConsumerID    uint64 `json:"consumer_module_id,omitempty"`
	ConsumerCode  string `json:"consumer_module_code,omitempty"`
	ConsumerLayer int    `json:"consumer_layer,omitempty"`
	ProviderID    uint64 `json:"provider_module_id,omitempty"`
	ProviderCode  string `json:"provider_module_code,omitempty"`
	ProviderLayer int    `json:"provider_layer,omitempty"`
}

// Error 实现 error 接口
func (e *LayerViolationError) Error() string {
	return e.Message
}

// CyclicDependencyError 表示内部模块依赖图中检测到有向闭环的 409 冲突错误
type CyclicDependencyError struct {
	Code      string   `json:"code"`
	Message   string   `json:"error"`
	CyclePath []string `json:"cycle_path"`
}

// Error 实现 error 接口
func (e *CyclicDependencyError) Error() string {
	return e.Message
}

type moduleLayerInfo struct {
	ID             uint64
	Code           string
	Name           string
	ModuleKind     string
	SubsystemID    uint64
	ModuleLayer    int
	SubsystemLayer int
	EffectiveLayer int
}

// EffectiveLayerLevel 计算模块有效架构层级：自身显式 > 0 优先，否则继承所属子系统层级（0 表示豁免）
func EffectiveLayerLevel(subsystemLayer, moduleLayer int) int {
	if moduleLayer > 0 {
		return moduleLayer
	}
	return subsystemLayer
}

// ValidateLayerLevelPtr 校验可选层级指针合法性（nil 合法，非 nil 须在 0~99）
func ValidateLayerLevelPtr(level *int) error {
	if level == nil {
		return nil
	}
	if *level < MinLayerLevel || *level > MaxLayerLevel {
		return ErrInvalidLayerLevel
	}
	return nil
}

func loadModuleLayerSnapshot(tx *gorm.DB) (map[uint64]moduleLayerInfo, error) {
	var subs []models.Subsystem
	if err := tx.Select("id, layer_level").Find(&subs).Error; err != nil {
		return nil, err
	}
	subLayerMap := make(map[uint64]int, len(subs))
	for _, s := range subs {
		subLayerMap[s.ID] = s.LayerLevel
	}

	var mods []models.Module
	if err := tx.Select("id, subsystem_id, code, name, module_kind, layer_level").Find(&mods).Error; err != nil {
		return nil, err
	}

	snap := make(map[uint64]moduleLayerInfo, len(mods))
	for _, m := range mods {
		subLayer := subLayerMap[m.SubsystemID]
		snap[m.ID] = moduleLayerInfo{
			ID:             m.ID,
			Code:           m.Code,
			Name:           m.Name,
			ModuleKind:     m.ModuleKind,
			SubsystemID:    m.SubsystemID,
			ModuleLayer:    m.LayerLevel,
			SubsystemLayer: subLayer,
			EffectiveLayer: EffectiveLayerLevel(subLayer, m.LayerLevel),
		}
	}
	return snap, nil
}

func checkLayerOrder(code string, lowerAllowedConsumer, higherForbiddenProvider moduleLayerInfo, relationDesc string) error {
	if lowerAllowedConsumer.EffectiveLayer <= 0 || higherForbiddenProvider.EffectiveLayer <= 0 {
		return nil
	}
	if lowerAllowedConsumer.EffectiveLayer < higherForbiddenProvider.EffectiveLayer {
		return &LayerViolationError{
			Code:          code,
			ConsumerID:    lowerAllowedConsumer.ID,
			ConsumerCode:  lowerAllowedConsumer.Code,
			ConsumerLayer: lowerAllowedConsumer.EffectiveLayer,
			ProviderID:    higherForbiddenProvider.ID,
			ProviderCode:  higherForbiddenProvider.Code,
			ProviderLayer: higherForbiddenProvider.EffectiveLayer,
			Message: fmt.Sprintf("%s违规：[%s (L%d)] 不可依赖/实现更高层级目标 [%s (L%d)]（要求源有效层级 >= 目标有效层级）",
				relationDesc,
				lowerAllowedConsumer.Code, lowerAllowedConsumer.EffectiveLayer,
				higherForbiddenProvider.Code, higherForbiddenProvider.EffectiveLayer),
		}
	}
	return nil
}

func buildInternalDependencyGraph(tx *gorm.DB) (map[uint64][]uint64, error) {
	var svcs []models.ModuleService
	if err := tx.Select("id, module_id, contract_id").Find(&svcs).Error; err != nil {
		return nil, err
	}
	svcModule := make(map[uint64]uint64, len(svcs))
	contractImplModules := make(map[uint64][]uint64)
	for _, s := range svcs {
		svcModule[s.ID] = s.ModuleID
		if s.ContractID != nil && *s.ContractID > 0 {
			contractImplModules[*s.ContractID] = append(contractImplModules[*s.ContractID], s.ModuleID)
		}
	}

	var deps []models.ModuleDependency
	if err := tx.Select("id, consumer_module_id, is_external, service_id, contract_id").
		Where("is_external = ?", false).
		Find(&deps).Error; err != nil {
		return nil, err
	}

	adjSet := make(map[uint64]map[uint64]struct{})
	addEdge := func(from, to uint64) {
		if from == 0 || to == 0 {
			return
		}
		if adjSet[from] == nil {
			adjSet[from] = make(map[uint64]struct{})
		}
		adjSet[from][to] = struct{}{}
	}

	for _, d := range deps {
		if d.ServiceID != nil && *d.ServiceID > 0 {
			if provMod, ok := svcModule[*d.ServiceID]; ok {
				addEdge(d.ConsumerModuleID, provMod)
			}
		}
		if d.ContractID != nil && *d.ContractID > 0 {
			for _, implMod := range contractImplModules[*d.ContractID] {
				addEdge(d.ConsumerModuleID, implMod)
			}
		}
	}

	adj := make(map[uint64][]uint64, len(adjSet))
	for from, targets := range adjSet {
		list := make([]uint64, 0, len(targets))
		for to := range targets {
			list = append(list, to)
		}
		sort.Slice(list, func(i, j int) bool { return list[i] < list[j] })
		adj[from] = list
	}
	return adj, nil
}

func detectDirectedCycle(adj map[uint64][]uint64, snap map[uint64]moduleLayerInfo, prioritySeeds ...uint64) error {
	keys := make([]uint64, 0, len(adj))
	seenSeed := make(map[uint64]bool)
	for _, s := range prioritySeeds {
		if s > 0 && !seenSeed[s] {
			seenSeed[s] = true
			keys = append(keys, s)
		}
	}
	remaining := make([]uint64, 0, len(adj))
	for k := range adj {
		if !seenSeed[k] {
			remaining = append(remaining, k)
		}
	}
	sort.Slice(remaining, func(i, j int) bool { return remaining[i] < remaining[j] })
	keys = append(keys, remaining...)

	// 0: 未访问, 1: 当前 DFS 栈内, 2: 已完成且无环
	state := make(map[uint64]int, len(snap))
	var stack []uint64

	var dfs func(u uint64) error
	dfs = func(u uint64) error {
		state[u] = 1
		stack = append(stack, u)

		for _, v := range adj[u] {
			switch state[v] {
			case 1:
				startIdx := 0
				for i, id := range stack {
					if id == v {
						startIdx = i
						break
					}
				}
				cycleIDs := append(append([]uint64(nil), stack[startIdx:]...), v)
				cycleCodes := make([]string, len(cycleIDs))
				for i, id := range cycleIDs {
					if info, ok := snap[id]; ok && info.Code != "" {
						cycleCodes[i] = info.Code
					} else {
						cycleCodes[i] = fmt.Sprintf("MOD_%d", id)
					}
				}
				return &CyclicDependencyError{
					Code:      ErrCodeCyclicDependency,
					CyclePath: cycleCodes,
					Message:   fmt.Sprintf("检测到内部模块闭环依赖：%s", strings.Join(cycleCodes, " -> ")),
				}
			case 0:
				if err := dfs(v); err != nil {
					return err
				}
			}
		}

		stack = stack[:len(stack)-1]
		state[u] = 2
		return nil
	}

	for _, start := range keys {
		if state[start] == 0 {
			if err := dfs(start); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateDependencyGuard 在事务内校验新增内部 DEPEND / DISPATCH 依赖的层级与有向防环约束
func ValidateDependencyGuard(tx *gorm.DB, dep *models.ModuleDependency) error {
	if dep == nil || dep.IsExternal {
		return nil
	}

	snap, err := loadModuleLayerSnapshot(tx)
	if err != nil {
		return err
	}
	consumer, ok := snap[dep.ConsumerModuleID]
	if !ok {
		return ErrModuleNotFound
	}

	if dep.ServiceID != nil && *dep.ServiceID > 0 {
		var svc models.ModuleService
		if err := tx.Select("id, module_id").First(&svc, *dep.ServiceID).Error; err != nil {
			return err
		}
		if provider, ok := snap[svc.ModuleID]; ok {
			if err := checkLayerOrder(ErrCodeLayerViolation, consumer, provider, "服务依赖 (DEPEND)"); err != nil {
				return err
			}
		}
	}

	if dep.ContractID != nil && *dep.ContractID > 0 {
		var contract models.ServiceContract
		if err := tx.Select("id, definer_module_id").First(&contract, *dep.ContractID).Error; err != nil {
			return err
		}
		if definer, ok := snap[contract.DefinerModuleID]; ok {
			if err := checkLayerOrder(ErrCodeLayerViolation, consumer, definer, "契约分发依赖 (DISPATCH)"); err != nil {
				return err
			}
		}
		var impls []models.ModuleService
		if err := tx.Select("id, module_id").Where("contract_id = ?", *dep.ContractID).Find(&impls).Error; err != nil {
			return err
		}
		for _, impl := range impls {
			if provider, ok := snap[impl.ModuleID]; ok {
				if err := checkLayerOrder(ErrCodeLayerViolation, consumer, provider, "契约分发依赖 (DISPATCH)"); err != nil {
					return err
				}
			}
		}
	}

	adj, err := buildInternalDependencyGraph(tx)
	if err != nil {
		return err
	}
	return detectDirectedCycle(adj, snap, dep.ConsumerModuleID)
}

// ValidateContractRealizationGuard 在事务内校验契约实现 (REALIZATION) 层级、既有 DISPATCH 消费方反向校核与有向防环
func ValidateContractRealizationGuard(tx *gorm.DB, contractID, implModuleID uint64) error {
	snap, err := loadModuleLayerSnapshot(tx)
	if err != nil {
		return err
	}
	implInfo, ok := snap[implModuleID]
	if !ok {
		return ErrModuleNotFound
	}

	var contract models.ServiceContract
	if err := tx.Select("id, definer_module_id").First(&contract, contractID).Error; err != nil {
		return err
	}
	if definerInfo, ok := snap[contract.DefinerModuleID]; ok {
		if err := checkLayerOrder(ErrCodeRealizationLayerInversion, implInfo, definerInfo, "契约实现 (REALIZATION)"); err != nil {
			return err
		}
	}

	// 反向校核该契约全部存量 DISPATCH 消费方：必须满足 consumer_eff >= impl_eff
	var dispatchDeps []models.ModuleDependency
	if err := tx.Select("id, consumer_module_id, is_external").
		Where("contract_id = ? AND is_external = ?", contractID, false).
		Order("id ASC").
		Find(&dispatchDeps).Error; err != nil {
		return err
	}
	seeds := make([]uint64, 0, len(dispatchDeps)+1)
	for _, dep := range dispatchDeps {
		seeds = append(seeds, dep.ConsumerModuleID)
		if consumerInfo, ok := snap[dep.ConsumerModuleID]; ok {
			if err := checkLayerOrder(ErrCodeLayerViolation, consumerInfo, implInfo, "契约分发依赖反向校核 (DISPATCH)"); err != nil {
				return err
			}
		}
	}
	seeds = append(seeds, implModuleID)

	adj, err := buildInternalDependencyGraph(tx)
	if err != nil {
		return err
	}
	return detectDirectedCycle(adj, snap, seeds...)
}

// ValidateAllTopologyLayerAndCycleConstraints 全量校验事务内所有内部 DEPEND、REALIZATION、DISPATCH 层级及有向环不变式
func ValidateAllTopologyLayerAndCycleConstraints(tx *gorm.DB) error {
	snap, err := loadModuleLayerSnapshot(tx)
	if err != nil {
		return err
	}

	var contracts []models.ServiceContract
	if err := tx.Select("id, definer_module_id").Find(&contracts).Error; err != nil {
		return err
	}
	contractDefiner := make(map[uint64]uint64, len(contracts))
	for _, c := range contracts {
		contractDefiner[c.ID] = c.DefinerModuleID
	}

	var svcs []models.ModuleService
	if err := tx.Select("id, module_id, contract_id").Find(&svcs).Error; err != nil {
		return err
	}
	svcModule := make(map[uint64]uint64, len(svcs))
	contractImpls := make(map[uint64][]uint64)
	for _, s := range svcs {
		svcModule[s.ID] = s.ModuleID
		if s.ContractID != nil && *s.ContractID > 0 {
			contractImpls[*s.ContractID] = append(contractImpls[*s.ContractID], s.ModuleID)
			if defModID, ok := contractDefiner[*s.ContractID]; ok {
				if err := checkLayerOrder(ErrCodeRealizationLayerInversion, snap[s.ModuleID], snap[defModID], "契约实现 (REALIZATION)"); err != nil {
					return err
				}
			}
		}
	}

	var deps []models.ModuleDependency
	if err := tx.Select("id, consumer_module_id, is_external, service_id, contract_id").
		Where("is_external = ?", false).
		Order("id ASC").
		Find(&deps).Error; err != nil {
		return err
	}
	for _, d := range deps {
		consumer := snap[d.ConsumerModuleID]
		if d.ServiceID != nil && *d.ServiceID > 0 {
			if provID, ok := svcModule[*d.ServiceID]; ok {
				if err := checkLayerOrder(ErrCodeLayerViolation, consumer, snap[provID], "服务依赖 (DEPEND)"); err != nil {
					return err
				}
			}
		}
		if d.ContractID != nil && *d.ContractID > 0 {
			if defID, ok := contractDefiner[*d.ContractID]; ok {
				if err := checkLayerOrder(ErrCodeLayerViolation, consumer, snap[defID], "契约分发依赖 (DISPATCH)"); err != nil {
					return err
				}
			}
			for _, implID := range contractImpls[*d.ContractID] {
				if err := checkLayerOrder(ErrCodeLayerViolation, consumer, snap[implID], "契约分发依赖 (DISPATCH)"); err != nil {
					return err
				}
			}
		}
	}

	adj, err := buildInternalDependencyGraph(tx)
	if err != nil {
		return err
	}
	return detectDirectedCycle(adj, snap)
}
