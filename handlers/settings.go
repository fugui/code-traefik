package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	commonAudit "code-common/backend/audit"
	commonAuth "code-common/backend/auth"
	commonModels "code-common/backend/models"
	"code-traefik/config"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
)

const SettingKeyCIGateTokenSHA256 = "ci_gate_token_sha256"

var validSettingKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)

// GetModuleSettings 读取模块级参数列表 GET /api/admin/settings（仅模块管理员/超级管理员可访问）
func GetModuleSettings(c *gin.Context) {
	var allSettings []models.TraefikSetting
	if err := models.DB.Order("setting_key ASC").Find(&allSettings).Error; err != nil {
		log.Printf("[%s] 读取模块配置失败: %v", config.ServiceName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取模块配置失败"})
		return
	}

	var visibleSettings []models.TraefikSetting
	ciTokenConfigured := false
	for _, s := range allSettings {
		if s.SettingKey == SettingKeyCIGateTokenSHA256 {
			if strings.TrimSpace(s.SettingVal) != "" {
				ciTokenConfigured = true
			}
			continue
		}
		visibleSettings = append(visibleSettings, s)
	}

	if visibleSettings == nil {
		visibleSettings = []models.TraefikSetting{}
	}

	c.JSON(http.StatusOK, gin.H{
		"settings":            visibleSettings,
		"ci_token_configured": ciTokenConfigured,
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

	var beforeList []models.TraefikSetting
	if err := models.DB.Order("setting_key ASC").Find(&beforeList).Error; err != nil {
		log.Printf("[%s] 读取变更前配置失败: %v", config.ServiceName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新配置失败"})
		return
	}

	for _, entry := range req.Items {
		key := strings.TrimSpace(entry.SettingKey)
		if key == SettingKeyCIGateTokenSHA256 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "禁止通过通用配置接口修改保留键 " + SettingKeyCIGateTokenSHA256})
			return
		}
		if !validSettingKeyPattern.MatchString(key) {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("配置键名 [%s] 格式不合法", key)})
			return
		}
		val := strings.TrimSpace(entry.SettingVal)
		if utf8.RuneCountInString(val) > 512 {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("配置项 [%s] 值长度超过上限", key)})
			return
		}

		var setting models.TraefikSetting
		err := models.DB.Where("setting_key = ?", key).First(&setting).Error
		if err != nil {
			setting = models.TraefikSetting{
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

	var afterList []models.TraefikSetting
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

// RotateCIToken 轮换或首次生成 CI/CD 机器 Token POST /api/admin/ci-token/rotate (AdminOnly)
func RotateCIToken(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	operator := "admin"
	if uc != nil && uc.Name != "" {
		operator = uc.Name
	} else if uc != nil && uc.Username != "" {
		operator = uc.Username
	}

	var req struct {
		Description string `json:"description"`
	}
	_ = c.ShouldBindJSON(&req)

	// 密码学安全随机数生成 32B 高熵机器 Token
	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成随机凭据失败"})
		return
	}
	rawToken := "ctk_" + hex.EncodeToString(rawBytes)

	// 计算 SHA-256 摘要
	sum := sha256.Sum256([]byte(rawToken))
	sha256Hex := hex.EncodeToString(sum[:])

	desc := "CI/CD 自动化强阻断门禁机器 Token (SHA-256)"
	if strings.TrimSpace(req.Description) != "" {
		desc = strings.TrimSpace(req.Description)
	}

	var setting models.TraefikSetting
	err := models.DB.Where("setting_key = ?", SettingKeyCIGateTokenSHA256).First(&setting).Error
	if err != nil {
		setting = models.TraefikSetting{
			SettingKey:  SettingKeyCIGateTokenSHA256,
			SettingVal:  sha256Hex,
			Description: desc,
			UpdatedBy:   operator,
		}
		if cErr := models.DB.Create(&setting).Error; cErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存 CI 凭据失败"})
			return
		}
	} else {
		setting.SettingVal = sha256Hex
		setting.Description = desc
		setting.UpdatedBy = operator
		if sErr := models.DB.Save(&setting).Error; sErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "更新 CI 凭据失败"})
			return
		}
	}

	commonAudit.SetAuditContext(
		c,
		"settings",
		"rotate_ci_token",
		commonModels.AuditLevelP1,
		fmt.Sprintf("管理员 [%s] 轮换了 CI 门禁机器 Token", operator),
		"ci_token",
		config.ModuleKey,
		config.ServiceName,
		nil,
		map[string]interface{}{"key": SettingKeyCIGateTokenSHA256, "status": "rotated"},
	)

	// 核心安全红线：强制禁用 HTTP 响应缓存
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"ci_token":   rawToken,
		"rotated_at": time.Now().Format(time.RFC3339),
	})
}

// RevokeCIToken 立即吊销 CI/CD 机器 Token DELETE /api/admin/ci-token (AdminOnly)
func RevokeCIToken(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	operator := "admin"
	if uc != nil && uc.Name != "" {
		operator = uc.Name
	} else if uc != nil && uc.Username != "" {
		operator = uc.Username
	}

	_ = models.DB.Where("setting_key = ?", SettingKeyCIGateTokenSHA256).Delete(&models.TraefikSetting{}).Error

	commonAudit.SetAuditContext(
		c,
		"settings",
		"revoke_ci_token",
		commonModels.AuditLevelP1,
		fmt.Sprintf("管理员 [%s] 吊销了 CI 门禁机器 Token", operator),
		"ci_token",
		config.ModuleKey,
		config.ServiceName,
		nil,
		map[string]interface{}{"key": SettingKeyCIGateTokenSHA256, "status": "revoked"},
	)

	c.JSON(http.StatusOK, gin.H{
		"message": "CI 门禁凭据已吊销",
	})
}
