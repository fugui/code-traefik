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
	"code-traefik/internal/server/decoder"
	"code-traefik/internal/server/hub"
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
	return func(c *gin.Context) {
		commonAuth.AuthMiddleware(commonAuth.AuthConfig{
			JWTSecretGetter: func() string { return config.AppConfig.Auth.JWTSecret },
			DB:              models.DB,
			PreloadAssocs:   []string{"Department"},
		})(c)
	}
}

// rejectQueryTokenMiddleware 严格拦截在 URL Query 中传递敏感 Token 的行为
func rejectQueryTokenMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Query("token") != "" {
			c.AbortWithStatusJSON(401, gin.H{"error": "严禁在 URL Query 参数中传递 Token，必须使用 Authorization: Bearer 标头"})
			return
		}
		c.Next()
	}
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
	protected.Use(rejectQueryTokenMiddleware())
	protected.Use(buildAuthMiddleware())
	protected.Use(commonAudit.Middleware(config.ServiceName))
	{
		protected.GET("/me", handlers.GetMe)
		protected.PATCH("/password", handlers.UpdatePassword)
		protected.GET("/users", handlers.SearchUsers)

		requireAdmin := commonAuth.RequireAdmin(config.AdminRole)

		// 3. 模块管理员专属路由 (Settings)
		admin := protected.Group("/admin")
		admin.Use(requireAdmin)
		{
			admin.GET("/settings", handlers.GetModuleSettings)
			admin.PUT("/settings", handlers.UpdateModuleSettings)
		}

		// 4. 三级架构元素与静态拓扑域 (F-01, F-02, F-04)
		protected.GET("/arch/subsystems", handlers.ListSubsystems)
		protected.POST("/arch/subsystems", requireAdmin, handlers.CreateSubsystem)
		protected.PUT("/arch/subsystems/:id", requireAdmin, handlers.UpdateSubsystem)
		protected.DELETE("/arch/subsystems/:id", requireAdmin, handlers.DeleteSubsystem)

		protected.GET("/arch/functional-groups/tree", handlers.GetFunctionalGroupTree)
		protected.POST("/arch/functional-groups", requireAdmin, handlers.CreateFunctionalGroup)
		protected.PUT("/arch/functional-groups/:id", requireAdmin, handlers.UpdateFunctionalGroup)
		protected.PATCH("/arch/functional-groups/:id/move", requireAdmin, handlers.MoveFunctionalGroup)
		protected.DELETE("/arch/functional-groups/:id", requireAdmin, handlers.DeleteFunctionalGroup)

		protected.GET("/arch/modules", handlers.ListModules)
		protected.GET("/arch/modules/:id", handlers.GetModule)
		protected.POST("/arch/modules", requireAdmin, handlers.CreateModule)
		protected.PUT("/arch/modules/:id", requireAdmin, handlers.UpdateModule)
		protected.DELETE("/arch/modules/:id", requireAdmin, handlers.DeleteModule)

		protected.GET("/arch/modules/:id/interfaces", handlers.ListModuleInterfaces)
		protected.POST("/arch/modules/:id/interfaces", requireAdmin, handlers.CreateModuleInterface)
		protected.PUT("/arch/module-interfaces/:ifaceId", requireAdmin, handlers.UpdateModuleInterface)
		protected.DELETE("/arch/module-interfaces/:ifaceId", requireAdmin, handlers.DeleteModuleInterface)

		protected.GET("/arch/topology/static", handlers.GetStaticTopology)

		// 5. Protobuf 描述集与方法映射域 (F-03)
		protected.GET("/proto-schemas", handlers.ListProtoSchemas)
		protected.POST("/proto-schemas", requireAdmin, handlers.UploadProtoSchema)
		protected.POST("/proto-schemas/:id/activate", requireAdmin, handlers.ActivateProtoSchema)
		protected.DELETE("/proto-schemas/:id", requireAdmin, handlers.DeleteProtoSchema)
		protected.GET("/proto-operations", handlers.ListProtoOperations)

		// 6. 公私域 Agent 纳管与排他租约管控域 (F-06, F-08)
		protected.GET("/agents", handlers.ListAgents)
		protected.GET("/agents/:id", handlers.GetAgent)
		protected.POST("/agents", handlers.RegisterAgent)
		protected.PUT("/agents/:id", handlers.UpdateAgent)
		protected.DELETE("/agents/:id", handlers.DeleteAgent)
		protected.POST("/agents/:id/handshake", handlers.HandshakeAgent)
		protected.GET("/agents/:id/discover-processes", handlers.DiscoverAgentProcesses)
		protected.POST("/agents/:id/lease/force-unlock", requireAdmin, handlers.ForceUnlockAgentLease)

		// 7. 业务进程部署映射域 (F-02)
		protected.GET("/processes", handlers.ListProcesses)
		protected.GET("/processes/:id", handlers.GetProcess)
		protected.POST("/processes", handlers.CreateProcess)
		protected.PUT("/processes/:id", handlers.UpdateProcess)
		protected.DELETE("/processes/:id", handlers.DeleteProcess)
		protected.GET("/processes/:id/downstreams", handlers.GetProcessDownstreams)
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

	// 4. 初始化 Protobuf 解码注册中心与 Agent 调度中枢
	decoderReg, err := decoder.NewRegistry(models.DB)
	if err != nil {
		log.Printf("[%s] 初始化 Protobuf 解码中心警告: %v", config.ServiceName, err)
	} else {
		handlers.ResetGlobalDecoderRegistry(decoderReg)
	}

	hubMgr := hub.NewManager(models.DB, &config.AppConfig)
	handlers.ResetGlobalHubManager(hubMgr)

	// 5. 执行主控启动恢复流水线与后台心跳探活 (ADR-03)
	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	if recErr := hubMgr.ServerRecoveryPipeline(rootCtx); recErr != nil {
		log.Printf("[%s] 主控恢复流水线执行警告: %v", config.ServiceName, recErr)
	}
	hubMgr.StartHeartbeatLoop(rootCtx)

	// 6. 基于 code-common/backend/server 启动服务（自动处理 /<ModuleKey> 网关前缀剥离与前端静态产物内嵌托管）
	// TODO(security): 若独立直接暴露于公网环境，请在网关层配置严格 CORS 白名单与全局 API 速率限制
	err = commonServer.Run(commonServer.Options{
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
			rootCancel()
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
