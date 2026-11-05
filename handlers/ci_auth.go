package handlers

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	commonAuth "code-common/backend/auth"
	"code-traefik/config"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
)

// slidingWindowLimiter 简单的内存滑动窗口限流器 (保护 CI Token 鉴权接口免遭高频暴力嗅探)
type slidingWindowLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	limit    int
	window   time.Duration
}

func newSlidingWindowLimiter(limit int, window time.Duration) *slidingWindowLimiter {
	return &slidingWindowLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
}

func (l *slidingWindowLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-l.window)

	times := l.requests[key]
	valid := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= l.limit {
		l.requests[key] = valid
		return false
	}

	l.requests[key] = append(valid, now)
	return true
}

var ciGateRateLimiter = newSlidingWindowLimiter(120, time.Minute)

// CIOrAdminGuard 双模鉴权守卫：支持平台管理员 JWT 或专用 X-Traefik-CI-Token 请求头 (ADR-14, M17, F-27)
func CIOrAdminGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 严格检查 URL Query：严禁在 Query 参数中传递任何 Token
		if c.Query("token") != "" || c.Query("ci_token") != "" || c.Query("ci-token") != "" || c.Query("x-traefik-ci-token") != "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "严禁在 URL Query 中传递 Token，必须使用 X-Traefik-CI-Token 请求头或 Authorization 标头",
			})
			return
		}

		ciToken := strings.TrimSpace(c.GetHeader("X-Traefik-CI-Token"))
		if ciToken != "" {
			// 2. CI 机器 Token 路径
			clientIP := c.ClientIP()
			if !ciGateRateLimiter.Allow(clientIP) {
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error": "CI 门禁请求过于频繁，请稍后重试",
				})
				return
			}

			// 查询库中存储的 SHA-256 摘要
			var setting models.TraefikSetting
			err := models.DB.Where("setting_key = ?", SettingKeyCIGateTokenSHA256).First(&setting).Error
			if err != nil || strings.TrimSpace(setting.SettingVal) == "" {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error": "CI 机器门禁凭据未配置或已吊销",
				})
				return
			}

			// 计算输入 Token 的 SHA-256 摘要
			sum := sha256.Sum256([]byte(ciToken))
			inputHex := hex.EncodeToString(sum[:])
			storedHex := strings.TrimSpace(setting.SettingVal)

			// 核心安全红线：常量时间比对，严禁普通 == 比对防时序侧信道攻击
			if subtle.ConstantTimeCompare([]byte(inputHex), []byte(storedHex)) != 1 {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error": "无效的 CI 机器凭据",
				})
				return
			}

			// 注入机器用户上下文
			c.Set("is_ci_token", true)
			c.Set("operator", "CI-Pipeline")
			c.Set("user_id", uint(0))
			c.Set("user_name", "CI-Pipeline")
			c.Next()
			return
		}

		// 3. 管理员 JWT 路径
		authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
		if authHeader != "" {
			// 执行通用 Auth 中间件
			commonAuth.AuthMiddleware(commonAuth.AuthConfig{
				JWTSecretGetter: func() string { return config.AppConfig.Auth.JWTSecret },
				DB:              models.DB,
				PreloadAssocs:   []string{"Department"},
			})(c)

			if c.IsAborted() {
				return
			}

			uc := commonAuth.GetUserContext(c)
			if uc == nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未提供有效认证凭据"})
				return
			}

			isAdmin := isCurrentUserAdmin(uc)
			if !isAdmin {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "权限不足：需要模块管理员或超级管理员权限"})
				return
			}

			c.Set("is_ci_token", false)
			c.Set("operator", uc.Username)
			c.Set("user_id", uc.UserID)
			c.Set("user_name", uc.Name)
			c.Next()
			return
		}

		// 4. 未提供任何凭据
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error": "未提供认证凭据：需要 X-Traefik-CI-Token 或管理员 Authorization 标头",
		})
	}
}
