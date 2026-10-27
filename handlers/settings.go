package handlers

import (
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	commonAudit "code-common/backend/audit"
	commonAuth "code-common/backend/auth"
	commonModels "code-common/backend/models"
	"code-traefik/config"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
)

var validSettingKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)

// GetModuleSettings 读取模块级参数列表 GET /api/admin/settings（仅模块管理员/超级管理员可访问）
func GetModuleSettings(c *gin.Context) {
	var settings []models.TemplateSetting
	if err := models.DB.Order("setting_key ASC").Find(&settings).Error; err != nil {
		log.Printf("[%s] 读取模块配置失败: %v", config.ServiceName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取模块配置失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"settings": settings,
	})
}

// UpdateModuleSettings 批量保存模块级参数 PUT /api/admin/settings（仅模块管理员/超级管理员可访问）
func UpdateModuleSettings(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	operator := "admin"
	if uc != nil && uc.Name != "" {
		operator = uc.Name
	} else if uc != nil && uc.Username != "" {
		operator = uc.Username
	}

	var req struct {
		Items []struct {
			SettingKey  string `json:"setting_key" binding:"required"`
			SettingVal  string `json:"setting_val"`
			Description string `json:"description"`
		} `json:"items" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求载荷格式错误"})
		return
	}
	if len(req.Items) == 0 || len(req.Items) > 50 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "单次更新配置项数量须在 1 至 50 之间"})
		return
	}

	var beforeList []models.TemplateSetting
	if err := models.DB.Order("setting_key ASC").Find(&beforeList).Error; err != nil {
		log.Printf("[%s] 读取变更前配置失败: %v", config.ServiceName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新配置失败"})
		return
	}

	for _, entry := range req.Items {
		key := strings.TrimSpace(entry.SettingKey)
		if !validSettingKeyPattern.MatchString(key) {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("配置键名 [%s] 格式不合法", key)})
			return
		}
		val := strings.TrimSpace(entry.SettingVal)
		if utf8.RuneCountInString(val) > 512 {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("配置项 [%s] 值长度超过上限", key)})
			return
		}

		var setting models.TemplateSetting
		err := models.DB.Where("setting_key = ?", key).First(&setting).Error
		if err != nil {
			setting = models.TemplateSetting{
				SettingKey:  key,
				SettingVal:  val,
				Description: strings.TrimSpace(entry.Description),
				UpdatedBy:   operator,
			}
			if createErr := models.DB.Create(&setting).Error; createErr != nil {
				log.Printf("[%s] 新增配置项 [%s] 失败: %v", config.ServiceName, key, createErr)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "保存配置失败"})
				return
			}
		} else {
			setting.SettingVal = val
			if strings.TrimSpace(entry.Description) != "" {
				setting.Description = strings.TrimSpace(entry.Description)
			}
			setting.UpdatedBy = operator
			if saveErr := models.DB.Save(&setting).Error; saveErr != nil {
				log.Printf("[%s] 更新配置项 [%s] 失败: %v", config.ServiceName, key, saveErr)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "保存配置失败"})
				return
			}
		}
	}

	var afterList []models.TemplateSetting
	if err := models.DB.Order("setting_key ASC").Find(&afterList).Error; err != nil {
		log.Printf("[%s] 读取变更后配置失败: %v", config.ServiceName, err)
	}

	commonAudit.SetAuditContext(
		c,
		"settings",
		"update",
		commonModels.AuditLevelP1,
		fmt.Sprintf("管理员 [%s] 更新了 %d 项模块运行参数", operator, len(req.Items)),
		"module_settings",
		config.ModuleKey,
		config.ServiceName,
		beforeList,
		afterList,
	)

	c.JSON(http.StatusOK, gin.H{
		"message":  "模块配置已保存",
		"settings": afterList,
	})
}
