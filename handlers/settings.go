package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	commonAudit "code-common/backend/audit"
	commonAuth "code-common/backend/auth"
	commonModels "code-common/backend/models"
	"code-traefik/config"
	"code-traefik/internal/server/security"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
)

const (
	SettingKeyCIGateTokenSHA256    = "ci_gate_token_sha256"
	SettingKeyLLMAPIKeyEnc         = "llm_api_key_enc"
	SettingKeyCodeNotifierTokenEnc = "code_notifier_token_enc"
	SettingKeySMTPPasswordEnc      = "smtp_password_enc"
	SettingKeyOutboundTrustedCIDRs = "outbound_trusted_cidrs"
	auditRedactedMask              = "***REDACTED***"
)

var secretWriteKeyToEncKey = map[string]string{
	"llm_api_key":         SettingKeyLLMAPIKeyEnc,
	"code_notifier_token": SettingKeyCodeNotifierTokenEnc,
	"smtp_password":       SettingKeySMTPPasswordEnc,
}

type settingSecretFlags struct {
	CITokenConfigured           bool
	LLMKeyConfigured            bool
	CodeNotifierTokenConfigured bool
	SMTPPasswordConfigured      bool
}

func isInternalSecretSettingKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	if k == SettingKeyCIGateTokenSHA256 || k == SettingKeyLLMAPIKeyEnc || k == SettingKeyCodeNotifierTokenEnc || k == SettingKeySMTPPasswordEnc {
		return true
	}
	return strings.HasSuffix(k, "_enc") || strings.HasSuffix(k, "_sha256") || strings.HasSuffix(k, "_secret")
}

func isSensitiveSettingKeyForAudit(key string) bool {
	if isInternalSecretSettingKey(key) {
		return true
	}
	k := strings.ToLower(strings.TrimSpace(key))
	_, isSecretWrite := secretWriteKeyToEncKey[k]
	return isSecretWrite || strings.Contains(k, "password") || strings.Contains(k, "api_key") || strings.Contains(k, "token")
}

func sanitizeSettingsForView(allSettings []models.TraefikSetting) ([]models.TraefikSetting, settingSecretFlags) {
	visible := make([]models.TraefikSetting, 0, len(allSettings))
	var flags settingSecretFlags
	for _, s := range allSettings {
		configured := strings.TrimSpace(s.SettingVal) != ""
		switch s.SettingKey {
		case SettingKeyCIGateTokenSHA256:
			flags.CITokenConfigured = configured
			continue
		case SettingKeyLLMAPIKeyEnc:
			flags.LLMKeyConfigured = configured
			continue
		case SettingKeyCodeNotifierTokenEnc:
			flags.CodeNotifierTokenConfigured = configured
			continue
		case SettingKeySMTPPasswordEnc:
			flags.SMTPPasswordConfigured = configured
			continue
		}
		if isInternalSecretSettingKey(s.SettingKey) {
			continue
		}
		visible = append(visible, s)
	}
	return visible, flags
}

func redactSettingsForAudit(allSettings []models.TraefikSetting) []models.TraefikSetting {
	masked := make([]models.TraefikSetting, 0, len(allSettings))
	for _, s := range allSettings {
		item := s
		if isSensitiveSettingKeyForAudit(s.SettingKey) && strings.TrimSpace(s.SettingVal) != "" {
			item.SettingVal = auditRedactedMask
		}
		masked = append(masked, item)
	}
	return masked
}

var validSettingKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)

// GetModuleSettings 读取模块级参数列表 GET /api/admin/settings（仅模块管理员/超级管理员可访问）
func GetModuleSettings(c *gin.Context) {
	var allSettings []models.TraefikSetting
	if err := models.DB.Order("setting_key ASC").Find(&allSettings).Error; err != nil {
		log.Printf("[%s] 读取模块配置失败: %v", config.ServiceName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取模块配置失败"})
		return
	}

	visibleSettings, flags := sanitizeSettingsForView(allSettings)

	c.JSON(http.StatusOK, gin.H{
		"settings":                       visibleSettings,
		"ci_token_configured":            flags.CITokenConfigured,
		"llm_key_configured":             flags.LLMKeyConfigured,
		"code_notifier_token_configured": flags.CodeNotifierTokenConfigured,
		"smtp_password_configured":       flags.SMTPPasswordConfigured,
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

	type preparedSetting struct {
		key         string
		val         string
		description string
	}
	prepared := make([]preparedSetting, 0, len(req.Items))

	for _, entry := range req.Items {
		key := strings.TrimSpace(entry.SettingKey)
		if isInternalSecretSettingKey(key) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "禁止通过通用配置接口直接写入内部安全保留键 " + key})
			return
		}
		if !validSettingKeyPattern.MatchString(key) {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("配置键名 [%s] 格式不合法", key)})
			return
		}
		val := strings.TrimSpace(entry.SettingVal)
		maxLen := 512
		if key == SettingKeyOutboundTrustedCIDRs {
			maxLen = 4096
		}
		if utf8.RuneCountInString(val) > maxLen {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("配置项 [%s] 值长度超过上限 (%d)", key, maxLen)})
			return
		}
		if key == SettingKeyOutboundTrustedCIDRs {
			if _, parseErr := security.ParseTrustedAllowlist(val); parseErr != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": parseErr.Error()})
				return
			}
		}
		if key == "llm_timeout_ms" && val != "" {
			ms, convErr := strconv.Atoi(val)
			if convErr != nil || ms < 100 || ms > 3000 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "llm_timeout_ms 必须为 100 至 3000 之间的整数"})
				return
			}
		}
		if key == "code_notifier_url" && val != "" {
			parsedURL, uErr := url.Parse(val)
			if uErr != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "code_notifier_url 必须为合法的 http/https 地址"})
				return
			}
		}
		if key == "smtp_port" && val != "" {
			port, pErr := strconv.Atoi(val)
			if pErr != nil || port < 1 || port > 65535 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "smtp_port 必须为 1 至 65535 之间的合法端口号"})
				return
			}
		}

		targetKey := key
		targetVal := val
		if encKey, isSecretWrite := secretWriteKeyToEncKey[key]; isSecretWrite {
			if val == "" {
				// 空值提交不覆盖既有加密凭据，避免页面保存误清空
				continue
			}
			cipherText, encErr := security.EncryptSecret(val)
			if encErr != nil {
				log.Printf("[%s] 加密敏感配置 [%s] 失败: %v", config.ServiceName, key, encErr)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "敏感凭据加密失败，请检查 TRAEFIK_CRYPTO_KEY 环境变量配置"})
				return
			}
			targetKey = encKey
			targetVal = cipherText
		}

		prepared = append(prepared, preparedSetting{
			key:         targetKey,
			val:         targetVal,
			description: strings.TrimSpace(entry.Description),
		})
	}

	for _, entry := range prepared {
		var setting models.TraefikSetting
		err := models.DB.Where("setting_key = ?", entry.key).First(&setting).Error
		if err != nil {
			setting = models.TraefikSetting{
				SettingKey:  entry.key,
				SettingVal:  entry.val,
				Description: entry.description,
				UpdatedBy:   operator,
			}
			if createErr := models.DB.Create(&setting).Error; createErr != nil {
				log.Printf("[%s] 新增配置项 [%s] 失败: %v", config.ServiceName, entry.key, createErr)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "保存配置失败"})
				return
			}
		} else {
			setting.SettingVal = entry.val
			if entry.description != "" {
				setting.Description = entry.description
			}
			setting.UpdatedBy = operator
			if saveErr := models.DB.Save(&setting).Error; saveErr != nil {
				log.Printf("[%s] 更新配置项 [%s] 失败: %v", config.ServiceName, entry.key, saveErr)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "保存配置失败"})
				return
			}
		}
	}

	var afterList []models.TraefikSetting
	if err := models.DB.Order("setting_key ASC").Find(&afterList).Error; err != nil {
		log.Printf("[%s] 读取变更后配置失败: %v", config.ServiceName, err)
	}

	visibleAfter, flags := sanitizeSettingsForView(afterList)

	commonAudit.SetAuditContext(
		c,
		"settings",
		"update",
		commonModels.AuditLevelP1,
		fmt.Sprintf("管理员 [%s] 更新了 %d 项模块运行参数", operator, len(req.Items)),
		"module_settings",
		config.ModuleKey,
		config.ServiceName,
		redactSettingsForAudit(beforeList),
		redactSettingsForAudit(afterList),
	)

	c.JSON(http.StatusOK, gin.H{
		"message":                        "模块配置已保存",
		"settings":                       visibleAfter,
		"ci_token_configured":            flags.CITokenConfigured,
		"llm_key_configured":             flags.LLMKeyConfigured,
		"code_notifier_token_configured": flags.CodeNotifierTokenConfigured,
		"smtp_password_configured":       flags.SMTPPasswordConfigured,
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

	if delErr := models.DB.Where("setting_key = ?", SettingKeyCIGateTokenSHA256).Delete(&models.TraefikSetting{}).Error; delErr != nil {
		log.Printf("[%s] 吊销 CI 门禁 Token 失败: %v", config.ServiceName, delErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "吊销 CI 门禁凭据失败"})
		return
	}

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
