package main

import (
	"context"
	"embed"
	"flag"
	"log"

	commonAudit "code-common/backend/audit"
	commonAuth "code-common/backend/auth"
	commonServer "code-common/backend/server"
	"code-traefik/config"
	"code-traefik/handlers"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
)

//go:embed all:frontend/dist
var frontendFS embed.FS

var (
	Version   = "dev"
	CommitID  = "unknown"
	BuildTime = "unknown"
)

// securityHeadersMiddleware 为响应统一补充浏览器安全防御头
func securityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
		c.Writer.Header().Set("X-Frame-Options", "SAMEORIGIN")
		c.Writer.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		c.Next()
	}
}

// buildAuthMiddleware 构建统一 JWT 鉴权中间件（支持纯 Claims 模式与 DB 关联模式）
func buildAuthMiddleware() gin.HandlerFunc {
	return commonAuth.AuthMiddleware(commonAuth.AuthConfig{
		JWTSecretGetter: func() string { return config.AppConfig.Auth.JWTSecret },
		DB:              models.DB,
		PreloadAssocs:   []string{"Department"},
	})
}

// RegisterAPIRoutes 集中注册所有开放路由、受保护业务路由与模块管理员路由（同时供单元测试直接挂载）
func RegisterAPIRoutes(r *gin.Engine) {
	// 1. 免密开放路由 (Unprotected)
	openAPI := r.Group("/api")
	{
		openAPI.GET("/health", handlers.HealthCheck)
		openAPI.GET("/auth/config", handlers.GetAuthConfig)
		openAPI.POST("/login", handlers.Login)
		openAPI.GET("/oauth2/authorize", handlers.StartOAuth2Flow)
		openAPI.GET("/oauth2/callback", handlers.OAuth2Callback)
	}

	// 2. 受保护业务路由 (Protected - 需携带合法平台 JWT)
	protected := r.Group("/api")
	protected.Use(buildAuthMiddleware())
	{
		protected.GET("/me", handlers.GetMe)
		protected.PATCH("/password", handlers.UpdatePassword)
		protected.GET("/users", handlers.SearchUsers)

		// 示例业务资源 CRUD 路由
		protected.GET("/items/stats", handlers.GetItemStats)
		protected.GET("/items", handlers.ListItems)
		protected.POST("/items", handlers.CreateItem)
		protected.PUT("/items/:id", handlers.UpdateItem)
		protected.DELETE("/items/:id", handlers.DeleteItem)

		// 3. 模块管理员专属路由 (RequireAdmin - 仅 super_admin 或 <module_key>_admin 可访问)
		admin := protected.Group("/admin")
		admin.Use(commonAuth.RequireAdmin(config.AdminRole))
		{
			admin.GET("/settings", handlers.GetModuleSettings)
			admin.PUT("/settings", handlers.UpdateModuleSettings)
		}
	}
}

func main() {
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	flag.Parse()

	handlers.AppVersion = Version
	log.Printf("[%s] 启动微前端业务子服务 (Version=%s, Commit=%s, BuildTime=%s)",
		config.ServiceName, Version, CommitID, BuildTime)

	// 1. 加载配置
	if err := config.LoadConfig(*configPath); err != nil {
		log.Fatalf("[%s] 加载配置失败: %v", config.ServiceName, err)
	}

	// 2. 初始化数据库与模型自动迁移
	if err := models.InitDB(); err != nil {
		log.Fatalf("[%s] 初始化数据库失败: %v", config.ServiceName, err)
	}

	// 3. 初始化异步操作审计引擎
	commonAudit.Init(models.DB)

	// 4. 基于 code-common/backend/server 启动服务（自动处理 /<ModuleKey> 网关前缀剥离与前端静态产物内嵌托管）
	// TODO(security): 若独立直接暴露于公网环境，请在网关层配置严格 CORS 白名单与全局 API 速率限制
	err := commonServer.Run(commonServer.Options{
		ServiceName:       config.ServiceName,
		Prefix:            config.ModuleKey,
		Port:              config.AppConfig.Server.Port,
		GinLog:            config.AppConfig.Server.GinLog,
		ReadTimeout:       config.AppConfig.Server.ReadTimeout,
		ReadHeaderTimeout: config.AppConfig.Server.ReadHeaderTimeout,
		WriteTimeout:      config.AppConfig.Server.WriteTimeout,
		IdleTimeout:       config.AppConfig.Server.IdleTimeout,
		MaxHeaderBytes:    config.AppConfig.Server.MaxHeaderBytes,
		FrontendFS:        &frontendFS,
		FrontendDistPath:  "frontend/dist",
		CustomMiddlewares: []gin.HandlerFunc{
			securityHeadersMiddleware(),
			commonAudit.Middleware(config.ModuleKey),
		},
		OnShutdown: func(ctx context.Context) {
			if closeErr := commonAudit.Close(ctx); closeErr != nil {
				log.Printf("[%s] 关闭审计引擎出现异常: %v", config.ServiceName, closeErr)
			}
		},
		RegisterRoutes: RegisterAPIRoutes,
	})
	if err != nil {
		log.Fatalf("[%s] 服务运行异常退出: %v", config.ServiceName, err)
	}
}
