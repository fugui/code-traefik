package topology

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"code-traefik/models"
	"gorm.io/gorm"
)

const (
	// MaxGroupTreeDepth 功能组最大自嵌套层级深度
	MaxGroupTreeDepth = 8
)

var (
	ErrNotImplemented          = errors.New("topology: not implemented")
	ErrSubsystemNotFound       = errors.New("子系统不存在")
	ErrGroupNotFound           = errors.New("功能组不存在")
	ErrParentGroupNotFound     = errors.New("目标父功能组不存在")
	ErrModuleNotFound          = errors.New("功能模块不存在")
	ErrInvalidCodeFormat       = errors.New("架构元素编码格式不合法（须为 2~64 位大写字母、数字、下划线或中划线）")
	ErrInvalidName             = errors.New("架构元素名称不能为空且长度不得超过 128 个字符")
	ErrDuplicateCode           = errors.New("架构元素编码已存在")
	ErrCycleMoveToSelf         = errors.New("禁止将功能组移动到自身节点下")
	ErrCycleMoveToDescendant   = errors.New("禁止将功能组移动到其子孙节点下（成环拦截）")
	ErrCrossSubsystemMove      = errors.New("禁止跨子系统移动或挂载功能组")
	ErrExceedMaxTreeDepth      = errors.New("功能组树层级超过最大 8 级深度上限")
	ErrSubsystemNotEmpty       = errors.New("子系统下仍存在关联功能组或功能模块，禁止删除")
	ErrGroupNotEmpty           = errors.New("功能组下仍存在子功能组或挂载的功能模块，禁止删除")
	ErrModuleNotEmpty          = errors.New("功能模块仍存在已声明接口契约或进程承载关联，禁止删除")
	ErrInvalidBaseProcessScope = errors.New("公共基线进程仅允许绑定 SHARED 公共 Agent 下的业务进程")
)

var codeRegex = regexp.MustCompile(`^[A-Z0-9_-]{2,64}$`)

// validateAndNormalizeCode 校验并标准化大写编码
func validateAndNormalizeCode(raw string) (string, error) {
	code := strings.ToUpper(strings.TrimSpace(raw))
	if !codeRegex.MatchString(code) {
		return "", ErrInvalidCodeFormat
	}
	return code, nil
}

// validateName 校验名称
func validateName(name string) error {
	trimmed := strings.TrimSpace(name)
	count := utf8.RuneCountInString(trimmed)
	if count == 0 || count > 128 {
		return ErrInvalidName
	}
	return nil
}

// SubsystemInput 子系统新增/修改入参
type SubsystemInput struct {
	Code        string
	Name        string
	OwnerID     *uint
	Description string
	OperatorID  uint
}

// FunctionalGroupInput 功能组新增入参
type FunctionalGroupInput struct {
	SubsystemID uint64
	ParentID    *uint64
	Code        string
	Name        string
	SortOrder   int
	Description string
	OperatorID  uint
}

// FunctionalGroupTreeNode 功能组嵌套树节点 DTO
type FunctionalGroupTreeNode struct {
	ID          uint64                     `json:"id"`
	SubsystemID uint64                     `json:"subsystem_id"`
	ParentID    *uint64                    `json:"parent_id"`
	Code        string                     `json:"code"`
	Name        string                     `json:"name"`
	Level       int                        `json:"level"`
	Path        string                     `json:"path"`
	SortOrder   int                        `json:"sort_order"`
	Description string                     `json:"description"`
	ModuleCount int64                      `json:"module_count"`
	Children    []*FunctionalGroupTreeNode `json:"children"`
}

// ModuleInput 功能模块新增/修改入参
type ModuleInput struct {
	GroupID      uint64
	Code         string
	Name         string
	OwnerID      *uint
	ProcessID    *uint64
	HasProcessID bool
	Description  string
	OperatorID   uint
}

// CreateSubsystem 创建子系统
func CreateSubsystem(db *gorm.DB, in SubsystemInput) (*models.Subsystem, error) {
	code, err := validateAndNormalizeCode(in.Code)
	if err != nil {
		return nil, err
	}
	if err := validateName(in.Name); err != nil {
		return nil, err
	}

	var count int64
	if err := db.Model(&models.Subsystem{}).Where("code = ?", code).Count(&count).Error; err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, ErrDuplicateCode
	}

	sub := &models.Subsystem{
		Code:        code,
		Name:        strings.TrimSpace(in.Name),
		OwnerID:     in.OwnerID,
		Description: in.Description,
		CreatedBy:   in.OperatorID,
		UpdatedBy:   in.OperatorID,
	}
	if err := db.Create(sub).Error; err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "23505") {
			return nil, ErrDuplicateCode
		}
		return nil, err
	}
	return sub, nil
}

// UpdateSubsystem 更新子系统
func UpdateSubsystem(db *gorm.DB, id uint64, in SubsystemInput) (*models.Subsystem, *models.Subsystem, error) {
	var before models.Subsystem
	if err := db.First(&before, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrSubsystemNotFound
		}
		return nil, nil, err
	}

	code := before.Code
	if in.Code != "" {
		c, err := validateAndNormalizeCode(in.Code)
		if err != nil {
			return nil, nil, err
		}
		if c != before.Code {
			var count int64
			if err := db.Model(&models.Subsystem{}).Where("code = ? AND id != ?", c, id).Count(&count).Error; err != nil {
				return nil, nil, err
			}
			if count > 0 {
				return nil, nil, ErrDuplicateCode
			}
			code = c
		}
	}

	name := before.Name
	if in.Name != "" {
		if err := validateName(in.Name); err != nil {
			return nil, nil, err
		}
		name = strings.TrimSpace(in.Name)
	}

	after := before
	after.Code = code
	after.Name = name
	after.OwnerID = in.OwnerID
	after.Description = in.Description
	after.UpdatedBy = in.OperatorID

	if err := db.Model(&models.Subsystem{}).Where("id = ?", id).Updates(map[string]interface{}{
		"code":        code,
		"name":        name,
		"owner_id":    in.OwnerID,
		"description": in.Description,
		"updated_by":  in.OperatorID,
	}).Error; err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "23505") {
			return nil, nil, ErrDuplicateCode
		}
		return nil, nil, err
	}

	return &before, &after, nil
}

// DeleteSubsystem 删除子系统
func DeleteSubsystem(db *gorm.DB, id uint64) (*models.Subsystem, error) {
	var sub models.Subsystem
	if err := db.First(&sub, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSubsystemNotFound
		}
		return nil, err
	}

	var groupCount, modCount int64
	if err := db.Model(&models.FunctionalGroup{}).Where("subsystem_id = ?", id).Count(&groupCount).Error; err != nil {
		return nil, err
	}
	if groupCount > 0 {
		return nil, ErrSubsystemNotEmpty
	}
	if err := db.Model(&models.Module{}).Where("subsystem_id = ?", id).Count(&modCount).Error; err != nil {
		return nil, err
	}
	if modCount > 0 {
		return nil, ErrSubsystemNotEmpty
	}

	if err := db.Delete(&sub).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}

// CreateFunctionalGroup 创建功能组并计算 level 与 path
func CreateFunctionalGroup(db *gorm.DB, in FunctionalGroupInput) (*models.FunctionalGroup, error) {
	code, err := validateAndNormalizeCode(in.Code)
	if err != nil {
		return nil, err
	}
	if err := validateName(in.Name); err != nil {
		return nil, err
	}

	var sub models.Subsystem
	if err := db.First(&sub, in.SubsystemID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSubsystemNotFound
		}
		return nil, err
	}

	var parent models.FunctionalGroup
	level := 1
	parentPath := "/"

	if in.ParentID != nil {
		if err := db.First(&parent, *in.ParentID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrParentGroupNotFound
			}
			return nil, err
		}
		if parent.SubsystemID != in.SubsystemID {
			return nil, ErrCrossSubsystemMove
		}
		level = parent.Level + 1
		if level > MaxGroupTreeDepth {
			return nil, ErrExceedMaxTreeDepth
		}
		parentPath = parent.Path
	}

	var count int64
	if err := db.Model(&models.FunctionalGroup{}).Where("subsystem_id = ? AND code = ?", in.SubsystemID, code).Count(&count).Error; err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, ErrDuplicateCode
	}

	var group models.FunctionalGroup
	err = db.Transaction(func(tx *gorm.DB) error {
		// 加咨询事务锁，确保同一子系统下树形并发安全
		tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(in.SubsystemID))

		group = models.FunctionalGroup{
			SubsystemID: in.SubsystemID,
			ParentID:    in.ParentID,
			Code:        code,
			Name:        strings.TrimSpace(in.Name),
			Level:       level,
			Path:        "/0/", // 临时占位，待插入后生成真实 ID
			SortOrder:   in.SortOrder,
			Description: in.Description,
			CreatedBy:   in.OperatorID,
			UpdatedBy:   in.OperatorID,
		}
		if err := tx.Create(&group).Error; err != nil {
			if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "23505") {
				return ErrDuplicateCode
			}
			return err
		}

		// 根据真实 ID 刷新 path
		actualPath := fmt.Sprintf("%s%d/", parentPath, group.ID)
		if parentPath == "/" {
			actualPath = fmt.Sprintf("/%d/", group.ID)
		}
		group.Path = actualPath
		return tx.Model(&models.FunctionalGroup{}).Where("id = ?", group.ID).Update("path", actualPath).Error
	})
	if err != nil {
		return nil, err
	}
	return &group, nil
}

// UpdateFunctionalGroup 更新功能组元信息
func UpdateFunctionalGroup(db *gorm.DB, id uint64, code, name string, sortOrder *int, description *string, operatorID uint) (*models.FunctionalGroup, *models.FunctionalGroup, error) {
	var before models.FunctionalGroup
	if err := db.First(&before, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrGroupNotFound
		}
		return nil, nil, err
	}

	actualCode := before.Code
	if code != "" {
		c, err := validateAndNormalizeCode(code)
		if err != nil {
			return nil, nil, err
		}
		if c != before.Code {
			var count int64
			if err := db.Model(&models.FunctionalGroup{}).Where("subsystem_id = ? AND code = ? AND id != ?", before.SubsystemID, c, id).Count(&count).Error; err != nil {
				return nil, nil, err
			}
			if count > 0 {
				return nil, nil, ErrDuplicateCode
			}
			actualCode = c
		}
	}

	actualName := before.Name
	if name != "" {
		if err := validateName(name); err != nil {
			return nil, nil, err
		}
		actualName = strings.TrimSpace(name)
	}

	updates := map[string]interface{}{
		"code":       actualCode,
		"name":       actualName,
		"updated_by": operatorID,
	}
	if sortOrder != nil {
		updates["sort_order"] = *sortOrder
	}
	if description != nil {
		updates["description"] = *description
	}

	after := before
	after.Code = actualCode
	after.Name = actualName
	if sortOrder != nil {
		after.SortOrder = *sortOrder
	}
	if description != nil {
		after.Description = *description
	}
	after.UpdatedBy = operatorID

	if err := db.Model(&models.FunctionalGroup{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "23505") {
			return nil, nil, ErrDuplicateCode
		}
		return nil, nil, err
	}
	return &before, &after, nil
}

// MoveFunctionalGroup 移动功能组并级联更新子树 path 与 level
func MoveFunctionalGroup(db *gorm.DB, groupID uint64, newParentID *uint64) error {
	_, _, _, err := MoveFunctionalGroupWithResult(db, groupID, newParentID, nil, 0)
	return err
}

// MoveFunctionalGroupWithResult 移动功能组并返回前后快照与影响子树节点数
func MoveFunctionalGroupWithResult(db *gorm.DB, groupID uint64, newParentID *uint64, sortOrder *int, operatorID uint) (*models.FunctionalGroup, *models.FunctionalGroup, int, error) {
	var before models.FunctionalGroup
	if err := db.First(&before, groupID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, 0, ErrGroupNotFound
		}
		return nil, nil, 0, err
	}

	if newParentID != nil && *newParentID == groupID {
		return nil, nil, 0, ErrCycleMoveToSelf
	}

	var targetParentPath string
	var newLevel int

	if newParentID != nil {
		var newParent models.FunctionalGroup
		if err := db.First(&newParent, *newParentID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil, 0, ErrParentGroupNotFound
			}
			return nil, nil, 0, err
		}
		if newParent.SubsystemID != before.SubsystemID {
			return nil, nil, 0, ErrCrossSubsystemMove
		}
		// 成环检测：目标父节点不能是自身的子孙
		if strings.HasPrefix(newParent.Path, before.Path) {
			return nil, nil, 0, ErrCycleMoveToDescendant
		}
		targetParentPath = newParent.Path
		newLevel = newParent.Level + 1
	} else {
		targetParentPath = "/"
		newLevel = 1
	}

	// 计算当前节点下子树的最大相对深度
	var maxChildLevel int
	if err := db.Model(&models.FunctionalGroup{}).
		Where("path LIKE ?", before.Path+"%").
		Select("COALESCE(MAX(level), 0)").
		Scan(&maxChildLevel).Error; err != nil {
		return nil, nil, 0, err
	}

	subtreeHeight := maxChildLevel - before.Level
	if newLevel+subtreeHeight > MaxGroupTreeDepth {
		return nil, nil, 0, ErrExceedMaxTreeDepth
	}

	var after models.FunctionalGroup
	var affectedCount int

	err := db.Transaction(func(tx *gorm.DB) error {
		tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(before.SubsystemID))

		var newPath string
		if targetParentPath == "/" {
			newPath = fmt.Sprintf("/%d/", before.ID)
		} else {
			newPath = fmt.Sprintf("%s%d/", targetParentPath, before.ID)
		}

		levelDelta := newLevel - before.Level
		oldPrefixLen := len(before.Path) + 1 // PostgreSQL 1-based index

		// 原子更新子树的 path 和 level
		// 注意：使用 SUBSTRING(path FROM CAST(? AS INTEGER)) 显式类型转换，避免 PostgreSQL 误当作正则表达式
		res := tx.Exec(`
			UPDATE traefik_functional_groups
			SET path = ? || SUBSTRING(path FROM CAST(? AS INTEGER)),
			    level = level + CAST(? AS INTEGER)
			WHERE path LIKE ?
		`, newPath, oldPrefixLen, levelDelta, before.Path+"%")
		if res.Error != nil {
			return res.Error
		}
		affectedCount = int(res.RowsAffected)

		// 更新当前节点的 parent_id 和更新人/排序
		updates := map[string]interface{}{
			"parent_id":  newParentID,
			"updated_by": operatorID,
		}
		if sortOrder != nil {
			updates["sort_order"] = *sortOrder
		}
		if err := tx.Model(&models.FunctionalGroup{}).Where("id = ?", before.ID).Updates(updates).Error; err != nil {
			return err
		}

		return tx.First(&after, before.ID).Error
	})
	if err != nil {
		return nil, nil, 0, err
	}

	updatedDescendants := affectedCount - 1
	if updatedDescendants < 0 {
		updatedDescendants = 0
	}

	return &before, &after, updatedDescendants, nil
}

// DeleteFunctionalGroup 删除功能组
func DeleteFunctionalGroup(db *gorm.DB, id uint64) (*models.FunctionalGroup, error) {
	var group models.FunctionalGroup
	if err := db.First(&group, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrGroupNotFound
		}
		return nil, err
	}

	var childCount, modCount int64
	if err := db.Model(&models.FunctionalGroup{}).Where("parent_id = ?", id).Count(&childCount).Error; err != nil {
		return nil, err
	}
	if childCount > 0 {
		return nil, ErrGroupNotEmpty
	}
	if err := db.Model(&models.Module{}).Where("group_id = ?", id).Count(&modCount).Error; err != nil {
		return nil, err
	}
	if modCount > 0 {
		return nil, ErrGroupNotEmpty
	}

	if err := db.Delete(&group).Error; err != nil {
		return nil, err
	}
	return &group, nil
}

// GetFunctionalGroupTree 查询功能组树并附带直接挂载模块统计数
func GetFunctionalGroupTree(db *gorm.DB, subsystemID uint64) ([]*FunctionalGroupTreeNode, error) {
	var sub models.Subsystem
	if err := db.First(&sub, subsystemID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSubsystemNotFound
		}
		return nil, err
	}

	var groups []models.FunctionalGroup
	if err := db.Where("subsystem_id = ?", subsystemID).
		Order("level ASC, sort_order ASC, id ASC").
		Find(&groups).Error; err != nil {
		return nil, err
	}

	// 统计每个 group 下直接挂载的 module 数量
	type groupCount struct {
		GroupID uint64 `gorm:"column:group_id"`
		Count   int64  `gorm:"column:cnt"`
	}
	var counts []groupCount
	_ = db.Model(&models.Module{}).
		Select("group_id, count(*) as cnt").
		Where("subsystem_id = ?", subsystemID).
		Group("group_id").
		Scan(&counts).Error

	countMap := make(map[uint64]int64, len(counts))
	for _, c := range counts {
		countMap[c.GroupID] = c.Count
	}

	nodeMap := make(map[uint64]*FunctionalGroupTreeNode, len(groups))
	roots := make([]*FunctionalGroupTreeNode, 0)

	for _, g := range groups {
		node := &FunctionalGroupTreeNode{
			ID:          g.ID,
			SubsystemID: g.SubsystemID,
			ParentID:    g.ParentID,
			Code:        g.Code,
			Name:        g.Name,
			Level:       g.Level,
			Path:        g.Path,
			SortOrder:   g.SortOrder,
			Description: g.Description,
			ModuleCount: countMap[g.ID],
			Children:    make([]*FunctionalGroupTreeNode, 0),
		}
		nodeMap[g.ID] = node

		if g.ParentID == nil {
			roots = append(roots, node)
		} else if parentNode, exists := nodeMap[*g.ParentID]; exists {
			parentNode.Children = append(parentNode.Children, node)
		} else {
			roots = append(roots, node)
		}
	}

	return roots, nil
}

// CreateModule 创建功能模块
func CreateModule(db *gorm.DB, in ModuleInput) (*models.Module, error) {
	code, err := validateAndNormalizeCode(in.Code)
	if err != nil {
		return nil, err
	}
	if err := validateName(in.Name); err != nil {
		return nil, err
	}

	var group models.FunctionalGroup
	if err := db.First(&group, in.GroupID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrGroupNotFound
		}
		return nil, err
	}

	var count int64
	if err := db.Model(&models.Module{}).Where("code = ?", code).Count(&count).Error; err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, ErrDuplicateCode
	}

	if in.HasProcessID && in.ProcessID != nil {
		var proc models.Process
		if err := db.Preload("Agent").First(&proc, *in.ProcessID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrProcessNotFound
			}
			return nil, err
		}
		if proc.Agent == nil || proc.Agent.Scope != models.ScopeShared {
			return nil, ErrInvalidBaseProcessScope
		}
	}

	mod := &models.Module{
		SubsystemID: group.SubsystemID,
		GroupID:     in.GroupID,
		Code:        code,
		Name:        strings.TrimSpace(in.Name),
		OwnerID:     in.OwnerID,
		ProcessID:   in.ProcessID,
		Description: in.Description,
		CreatedBy:   in.OperatorID,
		UpdatedBy:   in.OperatorID,
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(mod).Error; err != nil {
			if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "23505") {
				return ErrDuplicateCode
			}
			return err
		}
		if in.HasProcessID && in.ProcessID != nil {
			pm := &models.ProcessModule{
				ProcessID: *in.ProcessID,
				ModuleID:  mod.ID,
			}
			if err := tx.Create(pm).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return mod, nil
}

// UpdateModule 更新功能模块
func UpdateModule(db *gorm.DB, id uint64, in ModuleInput) (*models.Module, *models.Module, error) {
	var before models.Module
	if err := db.First(&before, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrModuleNotFound
		}
		return nil, nil, err
	}

	groupID := before.GroupID
	subsystemID := before.SubsystemID
	if in.GroupID != 0 && in.GroupID != before.GroupID {
		var group models.FunctionalGroup
		if err := db.First(&group, in.GroupID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil, ErrGroupNotFound
			}
			return nil, nil, err
		}
		groupID = group.ID
		subsystemID = group.SubsystemID
	}

	actualCode := before.Code
	if in.Code != "" {
		c, err := validateAndNormalizeCode(in.Code)
		if err != nil {
			return nil, nil, err
		}
		if c != before.Code {
			var count int64
			if err := db.Model(&models.Module{}).Where("code = ? AND id != ?", c, id).Count(&count).Error; err != nil {
				return nil, nil, err
			}
			if count > 0 {
				return nil, nil, ErrDuplicateCode
			}
			actualCode = c
		}
	}

	actualName := before.Name
	if in.Name != "" {
		if err := validateName(in.Name); err != nil {
			return nil, nil, err
		}
		actualName = strings.TrimSpace(in.Name)
	}

	processID := before.ProcessID
	if in.HasProcessID {
		if in.ProcessID != nil {
			var proc models.Process
			if err := db.Preload("Agent").First(&proc, *in.ProcessID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, nil, ErrProcessNotFound
				}
				return nil, nil, err
			}
			if proc.Agent == nil || proc.Agent.Scope != models.ScopeShared {
				return nil, nil, ErrInvalidBaseProcessScope
			}
		}
		processID = in.ProcessID
	}

	after := before
	after.GroupID = groupID
	after.SubsystemID = subsystemID
	after.Code = actualCode
	after.Name = actualName
	after.OwnerID = in.OwnerID
	after.ProcessID = processID
	after.Description = in.Description
	after.UpdatedBy = in.OperatorID

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Module{}).Where("id = ?", id).Updates(map[string]interface{}{
			"group_id":     groupID,
			"subsystem_id": subsystemID,
			"code":         actualCode,
			"name":         actualName,
			"owner_id":     in.OwnerID,
			"process_id":   processID,
			"description":  in.Description,
			"updated_by":   in.OperatorID,
		}).Error; err != nil {
			if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "23505") {
				return ErrDuplicateCode
			}
			return err
		}

		if in.HasProcessID {
			// 同步更新 traefik_process_modules
			if before.ProcessID != nil {
				_ = tx.Where("process_id = ? AND module_id = ?", *before.ProcessID, id).Delete(&models.ProcessModule{}).Error
			}
			if in.ProcessID != nil {
				pm := &models.ProcessModule{
					ProcessID: *in.ProcessID,
					ModuleID:  id,
				}
				_ = tx.Create(pm).Error
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	return &before, &after, nil
}

// DeleteModule 删除功能模块
func DeleteModule(db *gorm.DB, id uint64) (*models.Module, error) {
	var mod models.Module
	if err := db.First(&mod, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrModuleNotFound
		}
		return nil, err
	}

	var ifaceCount int64
	if err := db.Model(&models.ModuleInterface{}).Where("module_id = ?", id).Count(&ifaceCount).Error; err != nil {
		return nil, err
	}
	if ifaceCount > 0 {
		return nil, ErrModuleNotEmpty
	}

	var pmCount int64
	if err := db.Model(&models.ProcessModule{}).Where("module_id = ?", id).Count(&pmCount).Error; err != nil {
		return nil, err
	}
	if pmCount > 0 {
		return nil, ErrModuleNotEmpty
	}

	if mod.ProcessID != nil {
		return nil, ErrModuleNotEmpty
	}

	if err := db.Delete(&mod).Error; err != nil {
		return nil, err
	}
	return &mod, nil
}
