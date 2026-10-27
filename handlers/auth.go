package handlers

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	commonAudit "code-common/backend/audit"
	commonAuth "code-common/backend/auth"
	commonModels "code-common/backend/models"
	"code-traefik/config"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

const tokenExpireDuration = 24 * time.Hour

var oauth2States = commonAuth.NewStateStore()

// GetAuthConfig 返回当前认证方式配置（独立调试模式下供 UnifiedLogin 读取）
func GetAuthConfig(c *gin.Context) {
	authCfg := config.AppConfig.Auth
	passwordEnabled := authCfg.StandaloneMode || authCfg.PasswordLoginEnabled
	oauth2Enabled := authCfg.OAuth2.Enabled

	c.JSON(http.StatusOK, gin.H{
		"oauth2_enabled":         oauth2Enabled,
		"password_login_enabled": passwordEnabled,
		"dept_api_url":           authCfg.OAuth2.DeptAPIURL,
	})
}

// Login 独立开发调试模式下的本地账密登录接口
// TODO(security): 独立对外暴露时请补充登录接口限流（Rate Limiting）、MFA 多因素认证与弱口令泄露库检测
func Login(c *gin.Context) {
	authCfg := config.AppConfig.Auth
	if !authCfg.StandaloneMode && !authCfg.PasswordLoginEnabled {
		c.JSON(http.StatusForbidden, gin.H{"error": "本地直接登录已停用，请通过 CodeBench 统一门户登录"})
		return
	}

	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数不合法"})
		return
	}

	identifier := strings.ToLower(strings.TrimSpace(req.Username))
	if identifier == "" {
		identifier = strings.ToLower(strings.TrimSpace(req.Email))
	}
	if identifier == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请输入用户名或邮箱"})
		return
	}

	var user models.User
	if err := models.DB.Where("LOWER(username) = ? OR LOWER(email) = ?", identifier, identifier).First(&user).Error; err != nil {
		commonAudit.SetAuditContext(c, "auth", "login", commonModels.AuditLevelP2,
			fmt.Sprintf("登录失败: 账号 [%s] 不存在", identifier),
			"user", "", identifier, nil, nil)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}

	if !user.IsActive {
		commonAudit.SetAuditContext(c, "auth", "login", commonModels.AuditLevelP2,
			fmt.Sprintf("登录拒绝: 账号 [%s] 已被禁用", identifier),
			"user", fmt.Sprintf("%d", user.ID), user.Name, nil, nil)
		c.JSON(http.StatusForbidden, gin.H{"error": "当前账号已被禁用，请联系管理员"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		commonAudit.SetAuditContext(c, "auth", "login", commonModels.AuditLevelP2,
			fmt.Sprintf("登录失败: 账号 [%s] 密码校验未通过", identifier),
			"user", fmt.Sprintf("%d", user.ID), user.Name, nil, nil)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}

	roles := user.GetRoles()
	token, err := commonAuth.GenerateTokenWithEmployeeID(
		user.ID,
		user.Username,
		user.Email,
		user.Name,
		user.EmployeeID,
		user.IsSuperAdmin(),
		roles,
		config.AppConfig.Auth.JWTSecret,
		tokenExpireDuration,
	)
	if err != nil {
		log.Printf("[%s] 生成登录凭证失败: %v", config.ServiceName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成登录凭证失败"})
		return
	}

	displayName := user.Name
	if displayName == "" {
		displayName = user.Username
	}

	commonAuth.SetUserContext(c, &commonAuth.UserContext{
		UserID:   user.ID,
		Username: user.Username,
		Name:     user.Name,
		Email:    user.Email,
		Roles:    roles,
	})
	commonAudit.SetAuditContext(c, "auth", "login", commonModels.AuditLevelP2,
		fmt.Sprintf("用户 [%s] 登录系统成功", displayName),
		"user", fmt.Sprintf("%d", user.ID), displayName, nil, nil)

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
			"name":     user.Name,
			"roles":    roles,
		},
	})
}

// GetMe 返回当前已认证用户的上下文信息
func GetMe(c *gin.Context) {
	uc := commonAuth.GetUserContext(c)
	if uc == nil || uc.UserID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录或会话已失效"})
		return
	}

	isModAdmin := false
	isSuperAdmin := false
	for _, r := range uc.Roles {
		if r == commonAuth.RoleSuperAdmin {
			isSuperAdmin = true
			isModAdmin = true
		}
		if r == config.AdminRole {
			isModAdmin = true
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"id":               uc.UserID,
		"username":         uc.Username,
		"name":             uc.Name,
		"employee_id":      uc.EmployeeID,
		"department_id":    uc.DepartmentID,
		"department_name":  uc.DepartmentName,
		"roles":            uc.Roles,
		"is_super_admin":   isSuperAdmin,
		"is_module_admin":  isModAdmin,
		"required_ad_role": config.AdminRole,
	})
}

// UpdatePassword 独立运行模式下修改当前用户密码
func UpdatePassword(c *gin.Context) {
	authCfg := config.AppConfig.Auth
	if !authCfg.StandaloneMode && !authCfg.PasswordLoginEnabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请前往 CodeBench 门户个人中心修改密码"})
		return
	}

	uc := commonAuth.GetUserContext(c)
	if uc == nil || uc.UserID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	var req struct {
		OldPassword string `json:"old_password" binding:"required"`
		NewPassword string `json:"new_password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请完整填写原密码与新密码"})
		return
	}

	newLen := utf8.RuneCountInString(req.NewPassword)
	if newLen < 8 || newLen > 128 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "新密码长度须在 8 至 128 个字符之间"})
		return
	}

	var user models.User
	if err := models.DB.First(&user, uc.UserID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.OldPassword)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "原密码不正确"})
		return
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("[%s] 密码哈希计算失败: %v", config.ServiceName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密码更新失败"})
		return
	}

	if err := models.DB.Model(&user).Update("password", string(hashed)).Error; err != nil {
		log.Printf("[%s] 更新用户密码失败: %v", config.ServiceName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密码更新失败"})
		return
	}

	displayName := user.Name
	if displayName == "" {
		displayName = user.Username
	}
	commonAudit.SetAuditContext(c, "auth", "update_password", commonModels.AuditLevelP1,
		fmt.Sprintf("用户 [%s] 修改密码成功", displayName),
		"user", fmt.Sprintf("%d", user.ID), displayName, nil, nil)

	c.JSON(http.StatusOK, gin.H{"message": "密码修改成功"})
}

// SearchUsers 提供轻量级成员检索接口 GET /api/users（供公共组件 MemberSearchSelect 在独立与嵌入模式下统一调用）
func SearchUsers(c *gin.Context) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		page = 1
	}
	pageSize, err := strconv.Atoi(c.DefaultQuery("pageSize", "15"))
	if err != nil || pageSize < 1 {
		pageSize = 15
	}
	if pageSize > 100 {
		pageSize = 100
	}

	query := models.DB.Model(&models.User{}).Preload("Department").Where("is_active = ?", true)

	if idStr := strings.TrimSpace(c.Query("id")); idStr != "" {
		idVal, convErr := strconv.Atoi(idStr)
		if convErr == nil && idVal > 0 {
			query = query.Where("id = ?", idVal)
		}
	}

	if search := strings.TrimSpace(c.Query("search")); search != "" {
		likePattern := "%" + search + "%"
		query = query.Where("name LIKE ? OR username LIKE ? OR employee_id LIKE ?", likePattern, likePattern, likePattern)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		log.Printf("[%s] 查询成员总数失败: %v", config.ServiceName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询成员列表失败"})
		return
	}

	var users []models.User
	offset := (page - 1) * pageSize
	if err := query.Order("id ASC").Offset(offset).Limit(pageSize).Find(&users).Error; err != nil {
		log.Printf("[%s] 查询成员分页失败: %v", config.ServiceName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询成员列表失败"})
		return
	}

	// 仅输出成员选择器所需的最小脱敏字段，避免暴露邮箱/密码哈希等敏感隐私信息
	sanitized := make([]gin.H, 0, len(users))
	for _, u := range users {
		item := gin.H{
			"id":          u.ID,
			"name":        u.Name,
			"username":    u.Username,
			"employee_id": u.EmployeeID,
		}
		if u.Department != nil {
			item["department"] = gin.H{
				"id":   u.Department.ID,
				"name": u.Department.Name,
			}
		}
		sanitized = append(sanitized, item)
	}

	c.JSON(http.StatusOK, gin.H{
		"items": sanitized,
		"users": sanitized,
		"total": total,
	})
}

// StartOAuth2Flow 独立运行模式下发起 OAuth2 授权跳转
func StartOAuth2Flow(c *gin.Context) {
	oauth2Cfg := config.AppConfig.Auth.OAuth2
	if !oauth2Cfg.Enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "OAuth2 单点登录未启用"})
		return
	}

	state, _, codeChallenge, err := oauth2States.GenerateState()
	if err != nil {
		log.Printf("[%s] 生成 OAuth2 state 失败: %v", config.ServiceName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "发起单点登录失败"})
		return
	}

	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {oauth2Cfg.ClientID},
		"redirect_uri":          {oauth2Cfg.RedirectURL},
		"scope":                 {strings.Join(oauth2Cfg.Scopes, " ")},
		"state":                 {state},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
	}

	c.Redirect(http.StatusFound, oauth2Cfg.AuthURL+"?"+params.Encode())
}

// OAuth2Callback 独立运行模式下处理 OAuth2 回调
func OAuth2Callback(c *gin.Context) {
	oauth2Cfg := config.AppConfig.Auth.OAuth2
	if !oauth2Cfg.Enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "OAuth2 单点登录未启用"})
		return
	}

	code := strings.TrimSpace(c.Query("code"))
	state := strings.TrimSpace(c.Query("state"))
	if code == "" || state == "" {
		c.Redirect(http.StatusFound, "/login?sso_error="+url.QueryEscape("单点登录回调参数缺失"))
		return
	}

	codeVerifier, ok := oauth2States.ValidateAndConsume(state)
	if !ok {
		c.Redirect(http.StatusFound, "/login?sso_error="+url.QueryEscape("登录状态已过期，请重试"))
		return
	}

	tokenData, err := commonAuth.ExchangeCodeForToken(oauth2Cfg, code, codeVerifier)
	if err != nil {
		log.Printf("[%s] OAuth2 Token 交换失败: %v", config.ServiceName, err)
		c.Redirect(http.StatusFound, "/login?sso_error="+url.QueryEscape("单点登录凭证交换失败"))
		return
	}

	accessToken, _ := tokenData["access_token"].(string)
	if accessToken == "" {
		c.Redirect(http.StatusFound, "/login?sso_error="+url.QueryEscape("未获取到有效访问令牌"))
		return
	}

	userInfo, err := commonAuth.FetchUserInfo(oauth2Cfg.UserInfoURL, oauth2Cfg.ClientID, oauth2Cfg.Scopes, accessToken)
	if err != nil {
		log.Printf("[%s] 获取 SSO 用户信息失败: %v", config.ServiceName, err)
		c.Redirect(http.StatusFound, "/login?sso_error="+url.QueryEscape("获取用户信息失败"))
		return
	}

	mapping := oauth2Cfg.FieldMapping
	email := strings.ToLower(strings.TrimSpace(commonAuth.GetStringField(userInfo, mapping.Email)))
	rawUsername := commonAuth.GetStringField(userInfo, mapping.Username)
	name := commonAuth.ParseSSOAttribute(rawUsername)
	if customName := commonAuth.GetStringField(userInfo, mapping.Name); customName != "" {
		name = customName
	}
	if email == "" {
		email = strings.ToLower(strings.TrimSpace(commonAuth.ParseSSOEnglishName(rawUsername)))
	}
	if email == "" {
		c.Redirect(http.StatusFound, "/login?sso_error="+url.QueryEscape("单点登录未返回有效用户标识"))
		return
	}

	var user models.User
	if err := models.DB.Where("LOWER(email) = ? OR LOWER(username) = ?", email, email).First(&user).Error; err != nil {
		placeholderHash, hashErr := bcrypt.GenerateFromPassword([]byte(state+codeVerifier), bcrypt.DefaultCost)
		if hashErr != nil {
			log.Printf("[%s] 生成随机占位哈希失败: %v", config.ServiceName, hashErr)
			c.Redirect(http.StatusFound, "/login?sso_error="+url.QueryEscape("自动创建用户失败"))
			return
		}
		user = models.User{
			Username:  email,
			Email:     email,
			Name:      name,
			Password:  string(placeholderHash),
			IsActive:  true,
			RegMethod: "sso",
		}
		if createErr := models.DB.Create(&user).Error; createErr != nil {
			log.Printf("[%s] 自动创建 SSO 用户失败: %v", config.ServiceName, createErr)
			c.Redirect(http.StatusFound, "/login?sso_error="+url.QueryEscape("自动创建用户失败"))
			return
		}
	}

	jwtToken, err := commonAuth.GenerateTokenWithEmployeeID(
		user.ID,
		user.Username,
		user.Email,
		user.Name,
		user.EmployeeID,
		user.IsSuperAdmin(),
		user.GetRoles(),
		config.AppConfig.Auth.JWTSecret,
		tokenExpireDuration,
	)
	if err != nil {
		log.Printf("[%s] 签发 SSO JWT 失败: %v", config.ServiceName, err)
		c.Redirect(http.StatusFound, "/login?sso_error="+url.QueryEscape("签发登录凭证失败"))
		return
	}

	c.Redirect(http.StatusFound, "/?token="+url.QueryEscape(jwtToken))
}
