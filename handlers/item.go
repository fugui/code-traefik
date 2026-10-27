package handlers

import (
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	commonAudit "code-common/backend/audit"
	commonAuth "code-common/backend/auth"
	commonModels "code-common/backend/models"
	"code-common/backend/utils"
	"code-traefik/config"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
)

var validItemCodePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{2,64}$`)
var validCategoryPattern = regexp.MustCompile(`^[a-z0-9_-]{2,32}$`)

// ItemUpsertRequest 定义新建与更新业务条目的请求载荷
type ItemUpsertRequest struct {
	Code        string `json:"code"`
	Name        string `json:"name" binding:"required"`
	Category    string `json:"category"`
	Status      string `json:"status"`
	Priority    string `json:"priority"`
	Description string `json:"description"`
	OwnerID     *uint  `json:"owner_id"`
}

func canModifyItem(uc *commonAuth.UserContext, item *models.TemplateItem) bool {
	if uc == nil || uc.UserID == 0 {
		return false
	}
	for _, r := range uc.Roles {
		if r == commonAuth.RoleSuperAdmin || r == config.AdminRole {
			return true
		}
	}
	if item.CreatedBy != 0 && item.CreatedBy == uc.UserID {
		return true
	}
	if item.OwnerID != nil && *item.OwnerID == uc.UserID {
		return true
	}
	return false
}

func sanitizeItemOwner(item *models.TemplateItem) {
	if item == nil || item.Owner == nil {
		return
	}
	item.Owner.Password = ""
	item.Owner.Email = ""
}

// GetItemStats 返回概览看板所需的聚合指标 GET /api/items/stats
func GetItemStats(c *gin.Context) {
	type statusRow struct {
		Status string
		Count  int64
	}
	var rows []statusRow
	if err := models.DB.Model(&models.TemplateItem{}).
		Select("status, count(*) as count").
		Group("status").
		Scan(&rows).Error; err != nil {
		log.Printf("[%s] 统计业务条目状态失败: %v", config.ServiceName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "统计指标读取失败"})
		return
	}

	var total, activeCount, draftCount, archivedCount int64
	for _, row := range rows {
		total += row.Count
		switch row.Status {
		case models.ItemStatusActive:
			activeCount = row.Count
		case models.ItemStatusDraft:
			draftCount = row.Count
		case models.ItemStatusArchived:
			archivedCount = row.Count
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"total":    total,
		"active":   activeCount,
		"draft":    draftCount,
		"archived": archivedCount,
	})
}

// ListItems 分页查询业务条目列表 GET /api/items
func ListItems(c *gin.Context) {
	page, pageSize, offset := utils.ParsePagination(c, 25, 100)

	query := models.DB.Model(&models.TemplateItem{}).Preload("Owner")

	if search := strings.TrimSpace(c.Query("search")); search != "" {
		likePattern := "%" + search + "%"
		query = query.Where(
			"name LIKE ? OR code LIKE ? OR description LIKE ?",
			likePattern, likePattern, likePattern,
		)
	}

	if status := strings.TrimSpace(c.Query("status")); status != "" {
		if !models.IsValidItemStatus(status) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的状态筛选值"})
			return
		}
		query = query.Where("status = ?", status)
	}

	if category := strings.TrimSpace(c.Query("category")); category != "" {
		if !validCategoryPattern.MatchString(category) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的分类筛选值"})
			return
		}
		query = query.Where("category = ?", category)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		log.Printf("[%s] 查询条目总数失败: %v", config.ServiceName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询业务条目失败"})
		return
	}

	var items []models.TemplateItem
	if err := query.Order("updated_at DESC, id DESC").Offset(offset).Limit(pageSize).Find(&items).Error; err != nil {
		log.Printf("[%s] 分页读取条目失败: %v", config.ServiceName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询业务条目失败"})
		return
	}

	for i := range items {
		sanitizeItemOwner(&items[i])
	}

	utils.PaginatedJSON(c, items, total, page, pageSize)
}

// CreateItem 新建业务条目 POST /api/items
func CreateItem(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	if uc == nil || uc.UserID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	var req ItemUpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不完整或格式错误"})
		return
	}

	code := strings.ToUpper(strings.TrimSpace(req.Code))
	if !validItemCodePattern.MatchString(code) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "编码须由 2-64 位字母、数字、下划线或连字符组成"})
		return
	}

	name := strings.TrimSpace(req.Name)
	nameLen := utf8.RuneCountInString(name)
	if nameLen < 2 || nameLen > 128 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "名称长度须在 2 至 128 个字符之间"})
		return
	}

	category := strings.ToLower(strings.TrimSpace(req.Category))
	if category == "" {
		category = "general"
	}
	if !validCategoryPattern.MatchString(category) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "分类标识须由 2-32 位小写字母、数字、下划线或连字符组成"})
		return
	}

	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = models.ItemStatusActive
	}
	if !models.IsValidItemStatus(status) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "状态值不合法"})
		return
	}

	priority := strings.TrimSpace(req.Priority)
	if priority == "" {
		priority = models.ItemPriorityMedium
	}
	if !models.IsValidItemPriority(priority) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "优先级值不合法"})
		return
	}

	desc := strings.TrimSpace(req.Description)
	if utf8.RuneCountInString(desc) > 2000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "描述内容不能超过 2000 个字符"})
		return
	}

	var dupCount int64
	if err := models.DB.Model(&models.TemplateItem{}).Where("code = ?", code).Count(&dupCount).Error; err != nil {
		log.Printf("[%s] 校验唯一编码失败: %v", config.ServiceName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建条目失败"})
		return
	}
	if dupCount > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("条目编码 [%s] 已存在，请更换编码", code)})
		return
	}

	ownerID := req.OwnerID
	if ownerID == nil {
		uid := uc.UserID
		ownerID = &uid
	}

	item := models.TemplateItem{
		Code:        code,
		Name:        name,
		Category:    category,
		Status:      status,
		Priority:    priority,
		Description: desc,
		OwnerID:     ownerID,
		CreatedBy:   uc.UserID,
	}

	if err := models.DB.Create(&item).Error; err != nil {
		log.Printf("[%s] 写入新条目失败: %v", config.ServiceName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建条目失败"})
		return
	}

	if err := models.DB.Preload("Owner").First(&item, item.ID).Error; err != nil {
		log.Printf("[%s] 重新加载条目关联信息失败: %v", config.ServiceName, err)
	}
	sanitizeItemOwner(&item)

	commonAudit.SetAuditContext(
		c,
		"item",
		"create",
		commonModels.AuditLevelP2,
		fmt.Sprintf("创建业务条目 [%s] %s", item.Code, item.Name),
		"template_item",
		strconv.FormatUint(uint64(item.ID), 10),
		item.Name,
		nil,
		item,
	)

	c.JSON(http.StatusCreated, item)
}

// UpdateItem 更新指定业务条目 PUT /api/items/:id
func UpdateItem(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	if uc == nil || uc.UserID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || idVal == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的条目 ID"})
		return
	}

	var existing models.TemplateItem
	if err := models.DB.First(&existing, uint(idVal)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "目标条目不存在"})
		return
	}

	if !canModifyItem(uc, &existing) {
		c.JSON(http.StatusForbidden, gin.H{"error": "仅条目创建人、负责人或模块管理员可编辑此条目"})
		return
	}

	var req ItemUpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数格式错误"})
		return
	}

	beforeSnapshot := existing

	if strings.TrimSpace(req.Code) != "" {
		newCode := strings.ToUpper(strings.TrimSpace(req.Code))
		if !validItemCodePattern.MatchString(newCode) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "编码须由 2-64 位字母、数字、下划线或连字符组成"})
			return
		}
		if newCode != existing.Code {
			var dupCount int64
			if err := models.DB.Model(&models.TemplateItem{}).
				Where("code = ? AND id <> ?", newCode, existing.ID).
				Count(&dupCount).Error; err != nil {
				log.Printf("[%s] 校验更新编码唯一性失败: %v", config.ServiceName, err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "更新条目失败"})
				return
			}
			if dupCount > 0 {
				c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("条目编码 [%s] 已被占用", newCode)})
				return
			}
			existing.Code = newCode
		}
	}

	name := strings.TrimSpace(req.Name)
	nameLen := utf8.RuneCountInString(name)
	if nameLen < 2 || nameLen > 128 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "名称长度须在 2 至 128 个字符之间"})
		return
	}
	existing.Name = name

	if category := strings.ToLower(strings.TrimSpace(req.Category)); category != "" {
		if !validCategoryPattern.MatchString(category) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "分类标识格式不合法"})
			return
		}
		existing.Category = category
	}

	if status := strings.TrimSpace(req.Status); status != "" {
		if !models.IsValidItemStatus(status) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "状态值不合法"})
			return
		}
		existing.Status = status
	}

	if priority := strings.TrimSpace(req.Priority); priority != "" {
		if !models.IsValidItemPriority(priority) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "优先级值不合法"})
			return
		}
		existing.Priority = priority
	}

	desc := strings.TrimSpace(req.Description)
	if utf8.RuneCountInString(desc) > 2000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "描述内容不能超过 2000 个字符"})
		return
	}
	existing.Description = desc
	existing.OwnerID = req.OwnerID

	if err := models.DB.Save(&existing).Error; err != nil {
		log.Printf("[%s] 保存条目更新失败: %v", config.ServiceName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新条目失败"})
		return
	}

	if err := models.DB.Preload("Owner").First(&existing, existing.ID).Error; err != nil {
		log.Printf("[%s] 重新读取更新后条目失败: %v", config.ServiceName, err)
	}
	sanitizeItemOwner(&existing)

	commonAudit.SetAuditContext(
		c,
		"item",
		"update",
		commonModels.AuditLevelP2,
		fmt.Sprintf("更新业务条目 [%s] %s", existing.Code, existing.Name),
		"template_item",
		strconv.FormatUint(uint64(existing.ID), 10),
		existing.Name,
		beforeSnapshot,
		existing,
	)

	c.JSON(http.StatusOK, existing)
}

// DeleteItem 删除指定业务条目 DELETE /api/items/:id
func DeleteItem(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	if uc == nil || uc.UserID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || idVal == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的条目 ID"})
		return
	}

	var existing models.TemplateItem
	if err := models.DB.First(&existing, uint(idVal)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "目标条目不存在"})
		return
	}

	if !canModifyItem(uc, &existing) {
		c.JSON(http.StatusForbidden, gin.H{"error": "仅条目创建人、负责人或模块管理员可删除此条目"})
		return
	}

	if err := models.DB.Delete(&existing).Error; err != nil {
		log.Printf("[%s] 删除条目失败: %v", config.ServiceName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除条目失败"})
		return
	}

	commonAudit.SetAuditContext(
		c,
		"item",
		"delete",
		commonModels.AuditLevelP1,
		fmt.Sprintf("删除业务条目 [%s] %s", existing.Code, existing.Name),
		"template_item",
		strconv.FormatUint(uint64(existing.ID), 10),
		existing.Name,
		existing,
		nil,
	)

	c.JSON(http.StatusOK, gin.H{"message": "条目已删除"})
}
