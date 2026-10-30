package handlers

import (
	commonAudit "code-common/backend/audit"
	commonAuth "code-common/backend/auth"
	"code-traefik/config"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
)

// RejectQueryTokenMiddleware 严格拦截在 URL Query 中传递敏感 Token 的行为
func RejectQueryTokenMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Query("token") != "" {
			c.AbortWithStatusJSON(401, gin.H{"error": "严禁在 URL Query 参数中传递 Token，必须使用 Authorization: Bearer 标头"})
			return
		}
		c.Next()
	}
}

// BuildAuthMiddleware 构建统一 JWT 鉴权中间件（支持纯 Claims 模式与 DB 关联模式）
func BuildAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		commonAuth.AuthMiddleware(commonAuth.AuthConfig{
			JWTSecretGetter: func() string { return config.AppConfig.Auth.JWTSecret },
			DB:              models.DB,
			PreloadAssocs:   []string{"Department"},
		})(c)
	}
}

// RegisterAPIRoutes 集中注册所有开放路由、受保护业务路由与模块管理员路由
func RegisterAPIRoutes(r *gin.Engine) {
	// 1. 免密开放路由 (Unprotected)
	openAPI := r.Group("/api")
	{
		openAPI.GET("/health", HealthCheck)
		openAPI.GET("/auth/config", GetAuthConfig)
		openAPI.POST("/login", Login)
		openAPI.GET("/oauth2/authorize", StartOAuth2Flow)
		openAPI.GET("/oauth2/callback", OAuth2Callback)
	}

	// 2. 受保护业务路由 (Protected - 需携带合法平台 JWT)
	protected := r.Group("/api")
	protected.Use(RejectQueryTokenMiddleware())
	protected.Use(BuildAuthMiddleware())
	protected.Use(commonAudit.Middleware(config.ServiceName))
	{
		protected.GET("/me", GetMe)
		protected.PATCH("/password", UpdatePassword)
		protected.GET("/users", SearchUsers)

		requireAdmin := commonAuth.RequireAdmin(config.AdminRole)

		// 3. 模块管理员专属路由 (Settings)
		admin := protected.Group("/admin")
		admin.Use(requireAdmin)
		{
			admin.GET("/settings", GetModuleSettings)
			admin.PUT("/settings", UpdateModuleSettings)
		}

		// 4. 三级架构元素与静态拓扑域 (F-01, F-02, F-04)
		protected.GET("/arch/subsystems", ListSubsystems)
		protected.POST("/arch/subsystems", requireAdmin, CreateSubsystem)
		protected.PUT("/arch/subsystems/:id", requireAdmin, UpdateSubsystem)
		protected.DELETE("/arch/subsystems/:id", requireAdmin, DeleteSubsystem)

		protected.GET("/arch/functional-groups/tree", GetFunctionalGroupTree)
		protected.POST("/arch/functional-groups", requireAdmin, CreateFunctionalGroup)
		protected.PUT("/arch/functional-groups/:id", requireAdmin, UpdateFunctionalGroup)
		protected.PATCH("/arch/functional-groups/:id/move", requireAdmin, MoveFunctionalGroup)
		protected.DELETE("/arch/functional-groups/:id", requireAdmin, DeleteFunctionalGroup)

		protected.GET("/arch/modules", ListModules)
		protected.GET("/arch/modules/:id", GetModule)
		protected.POST("/arch/modules", requireAdmin, CreateModule)
		protected.PUT("/arch/modules/:id", requireAdmin, UpdateModule)
		protected.DELETE("/arch/modules/:id", requireAdmin, DeleteModule)
		protected.POST("/arch/modules/:id/process-binding", requireAdmin, BindModuleProcess)

		protected.GET("/arch/modules/:id/services", ListModuleServices)
		protected.POST("/arch/modules/:id/services", requireAdmin, CreateModuleService)
		protected.PUT("/arch/module-services/:serviceId", requireAdmin, UpdateModuleService)
		protected.DELETE("/arch/module-services/:serviceId", requireAdmin, DeleteModuleService)

		protected.GET("/arch/modules/:id/dependencies", ListModuleDependencies)
		protected.POST("/arch/modules/:id/dependencies", requireAdmin, CreateModuleDependency)
		protected.DELETE("/arch/module-dependencies/:depId", requireAdmin, DeleteModuleDependency)

		protected.GET("/arch/services/available", ListAvailableServices)

		protected.GET("/arch/topology/static", GetStaticTopology)

		// 5. Protobuf 描述集与方法映射域 (F-03)
		protected.GET("/proto-schemas", ListProtoSchemas)
		protected.POST("/proto-schemas", requireAdmin, UploadProtoSchema)
		protected.POST("/proto-schemas/:id/activate", requireAdmin, ActivateProtoSchema)
		protected.DELETE("/proto-schemas/:id", requireAdmin, DeleteProtoSchema)
		protected.GET("/proto-operations", ListProtoOperations)

		// 6. 公私域 Agent 纳管与排他租约管控域 (F-06, F-08)
		protected.GET("/agents", ListAgents)
		protected.GET("/agents/:id", GetAgent)
		protected.POST("/agents", RegisterAgent)
		protected.PUT("/agents/:id", UpdateAgent)
		protected.DELETE("/agents/:id", DeleteAgent)
		protected.POST("/agents/:id/handshake", HandshakeAgent)
		protected.GET("/agents/:id/discover-processes", DiscoverAgentProcesses)
		protected.POST("/agents/:id/lease/force-unlock", requireAdmin, ForceUnlockAgentLease)

		// 7. 业务进程部署映射域 (F-02)
		protected.GET("/processes", ListProcesses)
		protected.GET("/processes/:id", GetProcess)
		protected.POST("/processes", CreateProcess)
		protected.PUT("/processes/:id", UpdateProcess)
		protected.DELETE("/processes/:id", DeleteProcess)
		protected.GET("/processes/:id/downstreams", GetProcessDownstreams)

		// 8. 协同录制与架构拓扑域 (F-05, F-11, F-16)
		protected.GET("/recording-sessions", ListRecordingSessions)
		protected.GET("/recording-sessions/:id", GetRecordingSession)
		protected.POST("/recording-sessions", StartRecordingSession)
		protected.POST("/recording-sessions/:id/stop", StopRecordingSession)
		protected.DELETE("/recording-sessions/:id/processes/:processId", RemoveProcessFromSession)
		protected.PATCH("/recording-sessions/:id/publish", PublishRecordingSession)
		protected.DELETE("/recording-sessions/:id", DeleteRecordingSession)
		protected.GET("/recording-sessions/:id/consistency-report", GetConsistencyReport)

		// 9. 流量检索与解剖域 (F-03, F-11)
		protected.GET("/traffic-records", ListTrafficRecords)
		protected.GET("/traffic-records/:id/anatomy", GetTrafficRecordAnatomy)
		protected.POST("/traffic-records/:id/redecode", RedecodeTrafficRecord)

		// 10. 跨 Agent 同构离线重放与双层回归 Diff 域 (F-12, F-14)
		protected.GET("/replay-tasks", ListReplayTasks)
		protected.GET("/replay-tasks/:id", GetReplayTask)
		protected.POST("/replay-tasks", CreateReplayTask)
		protected.POST("/replay-tasks/:id/cancel", CancelReplayTask)
		protected.GET("/replay-tasks/:id/items", ListReplayItems)
		protected.GET("/replay-tasks/:id/items/:itemId", GetReplayItemDetail)
		protected.POST("/replay-tasks/:id/rediff", RediffReplayTask)

		// 11. SSE 实时事件流 (GET /api/events)
		protected.GET("/events", HandleEventsSSE)
	}
}
