package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	commonAuth "code-common/backend/auth"
	commonModels "code-common/backend/models"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var pgrpCodeRegex = regexp.MustCompile(`^[A-Z0-9_-]{2,64}$`)

// AutoPinnedOverrideDTO 默认端口变更冲突自动固化详情
type AutoPinnedOverrideDTO struct {
	ProcessID          uint64 `json:"process_id"`
	AgentID            uint64 `json:"agent_id"`
	OverrideListenPort int    `json:"override_listen_port"`
}

// ArchProcessGroupReq 物理进程组创建/修改请求体
type ArchProcessGroupReq struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	SortOrder   int    `json:"sort_order"`
	Description string `json:"description"`
}

// ArchProcessReq 标准架构业务进程创建/修改请求体
type ArchProcessReq struct {
	GroupID                 FlexUint64      `json:"group_id"`
	Code                    string          `json:"code"`
	Name                    string          `json:"name"`
	DefaultListenPort       int             `json:"default_listen_port"`
	DefaultCmdPattern       string          `json:"default_cmd_pattern"`
	DefaultExtraDownstreams []string        `json:"default_extra_downstreams"`
	ModuleIDs               FlexUint64Slice `json:"module_ids"`
	SortOrder               int             `json:"sort_order"`
	Description             string          `json:"description"`
}

// ListArchProcessGroups GET /api/arch/process-groups
func ListArchProcessGroups(c *gin.Context) {
	search := strings.TrimSpace(c.Query("search"))
	query := models.DB.Model(&models.ArchProcessGroup{})
	if search != "" {
		s := "%" + search + "%"
		query = query.Where("code ILIKE ? OR name ILIKE ?", s, s)
	}

	var groups []models.ArchProcessGroup
	if err := query.Order("sort_order ASC, id ASC").Find(&groups).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询物理进程组列表失败: " + err.Error()})
		return
	}

	// 统计各组下标准进程数量
	type countResult struct {
		GroupID uint64 `gorm:"column:group_id"`
		Count   int    `gorm:"column:count"`
	}
	var counts []countResult
	_ = models.DB.Model(&models.ArchProcess{}).Select("group_id, count(*) as count").Group("group_id").Scan(&counts).Error
	countMap := make(map[uint64]int)
	for _, cr := range counts {
		countMap[cr.GroupID] = cr.Count
	}

	type ArchProcessGroupDTO struct {
		models.ArchProcessGroup
		ProcessCount int `json:"process_count"`
	}

	res := make([]ArchProcessGroupDTO, len(groups))
	for i, g := range groups {
		res[i] = ArchProcessGroupDTO{
			ArchProcessGroup: g,
			ProcessCount:     countMap[g.ID],
		}
	}

	c.JSON(http.StatusOK, gin.H{"items": res})
}

// CreateArchProcessGroup POST /api/arch/process-groups (AdminOnly)
func CreateArchProcessGroup(c *gin.Context) {
	var req ArchProcessGroupReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法: " + err.Error()})
		return
	}

	code := strings.ToUpper(strings.TrimSpace(req.Code))
	if !pgrpCodeRegex.MatchString(code) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "物理进程组编码格式非法，仅允许 2-64 位大写字母、数字、下划线及减号"})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "物理进程组名称不能为空"})
		return
	}

	// 查重
	var count int64
	if err := models.DB.Model(&models.ArchProcessGroup{}).Where("code = ?", code).Count(&count).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "检查编码重复失败"})
		return
	}
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("物理进程组编码 [%s] 已存在", code)})
		return
	}
	if err := models.DB.Model(&models.ArchProcessGroup{}).Where("name = ?", name).Count(&count).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "检查名称重复失败"})
		return
	}
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("物理进程组名称 [%s] 已存在", name)})
		return
	}

	uc := commonAuth.GetUserContext(c)
	grp := models.ArchProcessGroup{
		Code:        code,
		Name:        name,
		SortOrder:   req.SortOrder,
		Description: req.Description,
		CreatedBy:   uc.UserID,
		UpdatedBy:   uc.UserID,
	}
	if err := models.DB.Create(&grp).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建物理进程组失败: " + err.Error()})
		return
	}

	recordAudit(c, "create", commonModels.AuditLevelP2, fmt.Sprintf("创建架构物理进程组 [%s]", grp.Name), "arch_process_group", grp.ID, grp.Name, nil, grp)
	c.JSON(http.StatusCreated, grp)
}

// UpdateArchProcessGroup PUT /api/arch/process-groups/:id (AdminOnly)
func UpdateArchProcessGroup(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的物理进程组 ID"})
		return
	}

	var req ArchProcessGroupReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法: " + err.Error()})
		return
	}

	var existing models.ArchProcessGroup
	if err := models.DB.First(&existing, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "物理进程组不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询物理进程组失败"})
		return
	}

	code := strings.ToUpper(strings.TrimSpace(req.Code))
	if code != "" && code != existing.Code {
		if !pgrpCodeRegex.MatchString(code) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "物理进程组编码格式非法，仅允许 2-64 位大写字母、数字、下划线及减号"})
			return
		}
		var count int64
		models.DB.Model(&models.ArchProcessGroup{}).Where("code = ? AND id != ?", code, id).Count(&count)
		if count > 0 {
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("物理进程组编码 [%s] 已存在", code)})
			return
		}
		existing.Code = code
	}

	name := strings.TrimSpace(req.Name)
	if name != "" && name != existing.Name {
		var count int64
		models.DB.Model(&models.ArchProcessGroup{}).Where("name = ? AND id != ?", name, id).Count(&count)
		if count > 0 {
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("物理进程组名称 [%s] 已存在", name)})
			return
		}
		existing.Name = name
	}

	uc := commonAuth.GetUserContext(c)
	before := existing
	existing.SortOrder = req.SortOrder
	existing.Description = req.Description
	existing.UpdatedBy = uc.UserID

	if err := models.DB.Save(&existing).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新物理进程组失败: " + err.Error()})
		return
	}

	recordAudit(c, "update", commonModels.AuditLevelP2, fmt.Sprintf("更新架构物理进程组 [%s]", existing.Name), "arch_process_group", existing.ID, existing.Name, before, existing)
	c.JSON(http.StatusOK, existing)
}

// DeleteArchProcessGroup DELETE /api/arch/process-groups/:id (AdminOnly)
func DeleteArchProcessGroup(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的物理进程组 ID"})
		return
	}

	var existing models.ArchProcessGroup
	if err := models.DB.First(&existing, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "物理进程组不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询物理进程组失败"})
		return
	}

	var procCount int64
	if err := models.DB.Model(&models.ArchProcess{}).Where("group_id = ?", id).Count(&procCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "检查关联进程失败"})
		return
	}
	if procCount > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "组下仍存在标准架构进程，禁止删除"})
		return
	}

	if err := models.DB.Delete(&existing).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除物理进程组失败: " + err.Error()})
		return
	}

	recordAudit(c, "delete", commonModels.AuditLevelP2, fmt.Sprintf("删除架构物理进程组 [%s]", existing.Name), "arch_process_group", existing.ID, existing.Name, existing, nil)
	c.JSON(http.StatusOK, gin.H{"message": "物理进程组已删除"})
}

// GetArchProcessTree GET /api/arch/processes/tree
func GetArchProcessTree(c *gin.Context) {
	search := strings.TrimSpace(c.Query("search"))

	var groups []models.ArchProcessGroup
	gQuery := models.DB.Model(&models.ArchProcessGroup{})
	if search != "" {
		s := "%" + search + "%"
		gQuery = gQuery.Where("code ILIKE ? OR name ILIKE ?", s, s)
	}
	if err := gQuery.Order("sort_order ASC, id ASC").Find(&groups).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询物理进程树失败: " + err.Error()})
		return
	}

	var processes []models.ArchProcess
	pQuery := models.DB.Model(&models.ArchProcess{}).Preload("Modules")
	if search != "" {
		s := "%" + search + "%"
		pQuery = pQuery.Where("code ILIKE ? OR name ILIKE ?", s, s)
	}
	if err := pQuery.Order("sort_order ASC, id ASC").Find(&processes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询进程列表失败: " + err.Error()})
		return
	}

	// 统计每个 ArchProcess 在所有环境下的挂载实例总数
	type mountCountResult struct {
		ArchProcessID uint64 `gorm:"column:arch_process_id"`
		Count         int    `gorm:"column:count"`
	}
	var mountCounts []mountCountResult
	_ = models.DB.Model(&models.Process{}).Select("arch_process_id, count(*) as count").Group("arch_process_id").Scan(&mountCounts).Error
	mountMap := make(map[uint64]int)
	for _, mc := range mountCounts {
		mountMap[mc.ArchProcessID] = mc.Count
	}

	// 组织二级树
	type ArchProcessTreeNode struct {
		models.ArchProcess
		MountCount int `json:"mount_count"`
	}
	type ArchProcessGroupTreeNode struct {
		models.ArchProcessGroup
		Processes []ArchProcessTreeNode `json:"processes"`
	}

	groupProcMap := make(map[uint64][]ArchProcessTreeNode)
	for _, p := range processes {
		node := ArchProcessTreeNode{
			ArchProcess: p,
			MountCount:  mountMap[p.ID],
		}
		groupProcMap[p.GroupID] = append(groupProcMap[p.GroupID], node)
	}

	res := make([]ArchProcessGroupTreeNode, 0, len(groups))
	for _, g := range groups {
		procs := groupProcMap[g.ID]
		if procs == nil {
			procs = []ArchProcessTreeNode{}
		}
		res = append(res, ArchProcessGroupTreeNode{
			ArchProcessGroup: g,
			Processes:        procs,
		})
	}

	// 统计孤立未分配至任何架构进程的模块
	var unassignedModuleCount int64
	_ = models.DB.Model(&models.Module{}).Where("arch_process_id IS NULL").Count(&unassignedModuleCount).Error

	c.JSON(http.StatusOK, gin.H{
		"tree":                    res,
		"items":                   res,
		"unassigned_module_count": unassignedModuleCount,
	})
}

// ListArchProcesses GET /api/arch/processes
func ListArchProcesses(c *gin.Context) {
	page, pageSize := parsePagination(c)
	groupIDStr := c.Query("group_id")
	moduleIDStr := c.Query("module_id")
	search := strings.TrimSpace(c.Query("search"))

	query := models.DB.Model(&models.ArchProcess{}).Preload("Group").Preload("Modules")
	if groupIDStr != "" {
		if gid, err := strconv.ParseUint(groupIDStr, 10, 64); err == nil {
			query = query.Where("group_id = ?", gid)
		}
	}
	if moduleIDStr != "" {
		if mid, err := strconv.ParseUint(moduleIDStr, 10, 64); err == nil {
			query = query.Joins("JOIN traefik_modules ON traefik_modules.arch_process_id = traefik_arch_processes.id").
				Where("traefik_modules.id = ?", mid)
		}
	}
	if search != "" {
		s := "%" + search + "%"
		query = query.Where("traefik_arch_processes.code ILIKE ? OR traefik_arch_processes.name ILIKE ?", s, s)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "统计标准进程列表失败"})
		return
	}

	items := make([]models.ArchProcess, 0)
	offset := (page - 1) * pageSize
	if err := query.Order("traefik_arch_processes.sort_order ASC, traefik_arch_processes.id ASC").
		Offset(offset).Limit(pageSize).Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询标准进程列表失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// ArchProcessDetailDTO 标准架构进程详情及挂载血缘 DTO
type ArchProcessDetailDTO struct {
	ArchProcess      models.ArchProcess        `json:"arch_process"`
	Modules          []models.Module           `json:"modules"`
	ProvidedServices []models.ModuleService    `json:"provided_services"`
	Dependencies     []models.ModuleDependency `json:"dependencies"`
	AgentMounts      []models.Process          `json:"agent_mounts"`
}

// GetArchProcess GET /api/arch/processes/:id
func GetArchProcess(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的标准架构进程 ID"})
		return
	}

	var archProc models.ArchProcess
	if err := models.DB.Preload("Group").Preload("Modules").First(&archProc, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "标准架构进程不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询标准架构进程失败"})
		return
	}

	// 查询该进程承载的功能模块所对外提供的服务与外部依赖
	moduleIDs := make([]uint64, 0, len(archProc.Modules))
	for _, m := range archProc.Modules {
		moduleIDs = append(moduleIDs, m.ID)
	}

	providedServices := make([]models.ModuleService, 0)
	dependencies := make([]models.ModuleDependency, 0)
	if len(moduleIDs) > 0 {
		_ = models.DB.Preload("Module").Where("module_id IN ?", moduleIDs).Find(&providedServices).Error
		_ = models.DB.Preload("ConsumerModule").
			Preload("Service").
			Preload("ProviderModule").
			Preload("ProviderModule.Subsystem").
			Preload("ProviderModule.ArchProcess").
			Where("consumer_module_id IN ?", moduleIDs).Find(&dependencies).Error
	}

	// 查询多环境 Agent 挂载血缘，严格经 ScopeReadGuard 过滤！
	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)

	var allMounts []models.Process
	_ = models.DB.Preload("Agent.Owner").Where("arch_process_id = ?", id).Order("id ASC").Find(&allMounts).Error

	filteredMounts := make([]models.Process, 0, len(allMounts))
	for _, m := range allMounts {
		if m.Agent == nil {
			continue
		}
		// ScopeReadGuard: 普通用户绝不泄露他人 PRIVATE Agent 信息
		if !isAdmin && m.Agent.Scope == models.ScopePrivate && (m.Agent.OwnerID == nil || *m.Agent.OwnerID != uc.UserID) {
			continue
		}
		if m.Agent.Owner != nil {
			m.Agent.Owner.Email = "" // 脱敏
		}
		m.ArchProcess = &archProc
		m.FillFromArchProcess()
		filteredMounts = append(filteredMounts, m)
	}

	c.JSON(http.StatusOK, gin.H{
		"arch_process":      archProc,
		"modules":           archProc.Modules,
		"provided_services": providedServices,
		"dependencies":      dependencies,
		"agent_mounts":      filteredMounts,
	})
}

func validateCmdPattern(pattern string, listenPort int) error {
	trimmed := strings.TrimSpace(pattern)
	if listenPort == 0 && trimmed == "" {
		return errors.New("纯客户端进程 (端口 0) 必须提供默认命令行匹配特征以定位 PID")
	}
	if len(trimmed) > 256 {
		return errors.New("命令行匹配特征长度不能超过 256 字符")
	}
	if strings.HasPrefix(trimmed, "re:") {
		regexStr := strings.TrimPrefix(trimmed, "re:")
		if _, err := regexp.Compile(regexStr); err != nil {
			return fmt.Errorf("正则表达式语法错误: %w", err)
		}
	}
	return nil
}

// CreateArchProcess POST /api/arch/processes (AdminOnly)
func CreateArchProcess(c *gin.Context) {
	var req ArchProcessReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法: " + err.Error()})
		return
	}

	groupID := req.GroupID.Uint64()
	if groupID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "必须选择所属架构物理进程组"})
		return
	}
	var grp models.ArchProcessGroup
	if err := models.DB.First(&grp, groupID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "所属物理进程组不存在"})
		return
	}

	code := strings.TrimSpace(req.Code)
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "标准架构进程编码不能为空"})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "标准架构进程名称不能为空"})
		return
	}

	if req.DefaultListenPort < 0 || req.DefaultListenPort > 65535 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "默认监听端口必须在 0..65535 范围内"})
		return
	}

	if err := validateCmdPattern(req.DefaultCmdPattern, req.DefaultListenPort); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 查重 code
	var count int64
	models.DB.Model(&models.ArchProcess{}).Where("code = ?", code).Count(&count)
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("标准架构进程编码 [%s] 已存在", code)})
		return
	}

	// A-03: 检查装载的 FM 功能模块是否已被其他标准进程分配
	moduleIDs := []uint64(req.ModuleIDs)
	if len(moduleIDs) > 0 {
		var conflictMods []models.Module
		models.DB.Where("id IN ? AND arch_process_id IS NOT NULL", moduleIDs).Find(&conflictMods)
		if len(conflictMods) > 0 {
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("功能模块 [%s] 已被分配给其他标准架构进程 (A-03)", conflictMods[0].Name)})
			return
		}
	}

	downstreamsJSON, _ := json.Marshal(req.DefaultExtraDownstreams)
	if req.DefaultExtraDownstreams == nil {
		downstreamsJSON = []byte("[]")
	}

	uc := commonAuth.GetUserContext(c)
	archProc := models.ArchProcess{
		GroupID:                 groupID,
		Code:                    code,
		Name:                    name,
		DefaultListenPort:       req.DefaultListenPort,
		DefaultCmdPattern:       strings.TrimSpace(req.DefaultCmdPattern),
		DefaultExtraDownstreams: datatypes.JSON(downstreamsJSON),
		SortOrder:               req.SortOrder,
		Description:             req.Description,
		CreatedBy:               uc.UserID,
		UpdatedBy:               uc.UserID,
	}

	err := models.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&archProc).Error; err != nil {
			return err
		}
		if len(moduleIDs) > 0 {
			if err := tx.Model(&models.Module{}).Where("id IN ?", moduleIDs).Update("arch_process_id", archProc.ID).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建标准架构进程失败: " + err.Error()})
		return
	}

	_ = models.DB.Preload("Group").Preload("Modules").First(&archProc, archProc.ID)
	recordAudit(c, "create", commonModels.AuditLevelP2, fmt.Sprintf("创建标准架构进程 [%s]", archProc.Name), "arch_process", archProc.ID, archProc.Name, nil, archProc)
	c.JSON(http.StatusCreated, archProc)
}

// UpdateArchProcess PUT /api/arch/processes/:id (AdminOnly)
func UpdateArchProcess(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的标准架构进程 ID"})
		return
	}

	var req ArchProcessReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法: " + err.Error()})
		return
	}

	var existing models.ArchProcess
	if err := models.DB.Preload("Group").Preload("Modules").First(&existing, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "标准架构进程不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询标准架构进程失败"})
		return
	}

	groupID := req.GroupID.Uint64()
	if groupID > 0 && groupID != existing.GroupID {
		var grp models.ArchProcessGroup
		if err := models.DB.First(&grp, groupID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "目标物理进程组不存在"})
			return
		}
		existing.GroupID = groupID
	}

	code := strings.TrimSpace(req.Code)
	if code != "" && code != existing.Code {
		var count int64
		models.DB.Model(&models.ArchProcess{}).Where("code = ? AND id != ?", code, id).Count(&count)
		if count > 0 {
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("标准架构进程编码 [%s] 已存在", code)})
			return
		}
		existing.Code = code
	}

	name := strings.TrimSpace(req.Name)
	if name != "" {
		existing.Name = name
	}

	if req.DefaultListenPort < 0 || req.DefaultListenPort > 65535 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "默认监听端口必须在 0..65535 范围内"})
		return
	}

	cmdPattern := strings.TrimSpace(req.DefaultCmdPattern)
	if err := validateCmdPattern(cmdPattern, req.DefaultListenPort); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	existing.DefaultCmdPattern = cmdPattern

	// A-03 校验新模块分配是否已被其他进程占用
	moduleIDs := []uint64(req.ModuleIDs)
	if len(moduleIDs) > 0 {
		var conflictMods []models.Module
		models.DB.Where("id IN ? AND arch_process_id IS NOT NULL AND arch_process_id != ?", moduleIDs, id).Find(&conflictMods)
		if len(conflictMods) > 0 {
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("功能模块 [%s] 已被分配给其他标准架构进程 (A-03)", conflictMods[0].Name)})
			return
		}
	}

	downstreamsJSON, _ := json.Marshal(req.DefaultExtraDownstreams)
	if req.DefaultExtraDownstreams == nil {
		downstreamsJSON = []byte("[]")
	}
	existing.DefaultExtraDownstreams = datatypes.JSON(downstreamsJSON)
	existing.SortOrder = req.SortOrder
	existing.Description = req.Description

	uc := commonAuth.GetUserContext(c)
	existing.UpdatedBy = uc.UserID

	// 核心架构特性：默认端口冲突 auto_pinned 保护与单事务固化
	autoPinnedList := make([]AutoPinnedOverrideDTO, 0)

	oldDefaultPort := existing.DefaultListenPort
	newDefaultPort := req.DefaultListenPort
	existing.DefaultListenPort = newDefaultPort

	txErr := models.DB.Transaction(func(tx *gorm.DB) error {
		// 1. 如果默认端口发生改变且非零，检查所有未设置 override_listen_port 的挂载实例
		if oldDefaultPort != newDefaultPort {
			var mounts []models.Process
			if err := tx.Where("arch_process_id = ?", existing.ID).Find(&mounts).Error; err != nil {
				return err
			}

			for _, m := range mounts {
				if m.OverrideListenPort != nil {
					// 已有本地覆盖端口，不受默认端口变更影响
					continue
				}

				if newDefaultPort > 0 {
					// 检查同机其他进程是否存在新默认端口冲突
					var conflictCount int64
					if err := tx.Model(&models.Process{}).
						Where("agent_id = ? AND id != ? AND effective_listen_port = ?", m.AgentID, m.ID, newDefaultPort).
						Count(&conflictCount).Error; err != nil {
						return err
					}

					if conflictCount > 0 {
						// 触发冲突！单事务自动将该冲突实例的 override_listen_port 固化为原有效端口 (oldDefaultPort)
						pinned := m.EffectiveListenPort
						if pinned <= 0 {
							pinned = oldDefaultPort
						}
						if err := tx.Model(&models.Process{}).Where("id = ?", m.ID).Updates(map[string]any{
							"override_listen_port":  pinned,
							"effective_listen_port": pinned,
						}).Error; err != nil {
							return err
						}
						autoPinnedList = append(autoPinnedList, AutoPinnedOverrideDTO{
							ProcessID:          m.ID,
							AgentID:            m.AgentID,
							OverrideListenPort: pinned,
						})
					} else {
						// 无冲突，物化更新为新默认端口
						if err := tx.Model(&models.Process{}).Where("id = ?", m.ID).Update("effective_listen_port", newDefaultPort).Error; err != nil {
							return err
						}
					}
				} else {
					// 新端口为 0 (纯客户端)，直接更新物化有效端口为 0
					if err := tx.Model(&models.Process{}).Where("id = ?", m.ID).Update("effective_listen_port", 0).Error; err != nil {
						return err
					}
				}
			}
		}

		// 2. 保存 ArchProcess
		if err := tx.Save(&existing).Error; err != nil {
			return err
		}

		// 3. 更新模块装配：剔除不再装配的模块，并绑定新模块
		if err := tx.Model(&models.Module{}).Where("arch_process_id = ? AND id NOT IN ?", existing.ID, append(moduleIDs, 0)).
			Update("arch_process_id", nil).Error; err != nil {
			return err
		}
		if len(moduleIDs) > 0 {
			if err := tx.Model(&models.Module{}).Where("id IN ?", moduleIDs).
				Update("arch_process_id", existing.ID).Error; err != nil {
				return err
			}
		}

		return nil
	})

	if txErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新标准架构进程失败: " + txErr.Error()})
		return
	}

	_ = models.DB.Preload("Group").Preload("Modules").First(&existing, existing.ID)
	recordAudit(c, "update", commonModels.AuditLevelP2, fmt.Sprintf("更新标准架构进程 [%s]", existing.Name), "arch_process", existing.ID, existing.Name, nil, existing)

	c.JSON(http.StatusOK, gin.H{
		"arch_process":          existing,
		"auto_pinned_overrides": autoPinnedList,
	})
}

// DeleteArchProcess DELETE /api/arch/processes/:id (AdminOnly)
func DeleteArchProcess(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的标准架构进程 ID"})
		return
	}

	var existing models.ArchProcess
	if err := models.DB.First(&existing, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "标准架构进程不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询标准架构进程失败"})
		return
	}

	// 校验是否已被任何环境 Agent 挂载实例化
	var mountCount int64
	if err := models.DB.Model(&models.Process{}).Where("arch_process_id = ?", id).Count(&mountCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "检查挂载实例失败"})
		return
	}
	if mountCount > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "该标准架构进程已被宿主机 Agent 挂载实例化，禁止删除"})
		return
	}

	// 校验是否被历史重放任务引用
	var replayCount int64
	if err := models.DB.Model(&models.ReplayTask{}).Where("arch_process_id = ?", id).Count(&replayCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "检查重放任务引用失败"})
		return
	}
	if replayCount > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "该标准架构进程已被历史重放任务引用，禁止删除"})
		return
	}

	// 级联解绑已装载模块
	_ = models.DB.Model(&models.Module{}).Where("arch_process_id = ?", id).Update("arch_process_id", nil).Error

	if err := models.DB.Delete(&existing).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除标准架构进程失败: " + err.Error()})
		return
	}

	recordAudit(c, "delete", commonModels.AuditLevelP2, fmt.Sprintf("删除标准架构进程 [%s]", existing.Name), "arch_process", existing.ID, existing.Name, existing, nil)
	c.JSON(http.StatusOK, gin.H{"message": "标准架构进程已删除"})
}
