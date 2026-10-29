package main

import (
	"context"
	"flag"
	"log"

	commonAudit "code-common/backend/audit"
	commonServer "code-common/backend/server"
	traefik "code-traefik"
	"code-traefik/config"
	"code-traefik/handlers"
	"code-traefik/internal/server/decoder"
	"code-traefik/internal/server/hub"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
)

var (
	Version   = "dev"
	CommitID  = "unknown"
	BuildTime = "unknown"
)

// securityHeadersMiddleware 为响应统一补充浏览器安全防御头 (CSP, Nosniff, Frame-Options)
func securityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
		c.Writer.Header().Set("X-Frame-Options", "SAMEORIGIN")
		c.Writer.Header().Set("Content-Security-Policy", "default-src 'self' 'unsafe-inline' 'unsafe-eval' data: blob:;")
		c.Writer.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		c.Next()
	}
}

// RegisterAPIRoutes 集中注册所有开放路由、受保护业务路由与模块管理员路由
func RegisterAPIRoutes(r *gin.Engine) {
	handlers.RegisterAPIRoutes(r)
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

	// 5. 初始化协同录制双轨管道与双层 Diff 重放调度引擎
	assembler, _, _ := handlers.InitRecorderAndDiff(models.DB, decoderReg)

	// 6. 执行主控启动恢复流水线与后台心跳探活 (ADR-03)
	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	if recErr := hubMgr.ServerRecoveryPipeline(rootCtx); recErr != nil {
		log.Printf("[%s] 主控恢复流水线执行警告: %v", config.ServiceName, recErr)
	}
	hubMgr.StartHeartbeatLoop(rootCtx)

	// 7. 基于 code-common/backend/server 启动服务（自动处理 /<ModuleKey> 网关前缀剥离与前端静态产物内嵌托管）
	// TODO(security): 若独立直接暴露于公网环境，请在网关层配置严格 CORS 白名单与全局 API 速率限制
	// TODO(security): 若未来切换 Cookie 会话须挂载 Double-Submit CSRF Token 中间件
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
		FrontendFS:        &traefik.FrontendFS,
		FrontendDistPath:  "frontend/dist",
		CustomMiddlewares: []gin.HandlerFunc{
			securityHeadersMiddleware(),
			commonAudit.Middleware(config.ModuleKey),
		},
		OnShutdown: func(ctx context.Context) {
			rootCancel()
			if assembler != nil {
				assembler.Close()
			}
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
