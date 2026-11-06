package handlers_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	commonAudit "code-common/backend/audit"
	commonAuth "code-common/backend/auth"
	commonModels "code-common/backend/models"
	"code-common/backend/testdb"
	"code-traefik/config"
	"code-traefik/handlers"
	"code-traefik/internal/server/decoder"
	"code-traefik/internal/server/hub"
	"code-traefik/internal/server/topology"
	"code-traefik/models"
	"code-traefik/pkg/agentproto"

	"github.com/gin-gonic/gin"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"mime/multipart"
)

// setupRouterAndDB 初始化测试环境
func setupRouterAndDB(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN 未配置，跳过隔离集成测试")
	}

	gin.SetMode(gin.TestMode)

	// 使用隔离数据库
	db := testdb.SetupIsolatedDB(t, "traefik_api_test")
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}
	if err := db.AutoMigrate(&commonModels.SysAuditLog{}); err != nil {
		t.Fatalf("迁移审计表失败: %v", err)
	}

	// 初始化测试审计引擎
	_ = commonAudit.Close(context.Background())
	commonAudit.Init(db, commonAudit.InitOptions{
		BatchSize:   1,
		FlushPeriod: 20 * time.Millisecond,
	})

	// 初始化配置
	config.AppConfig = config.Config{
		Auth: config.AuthSection{
			JWTSecret: "test-jwt-secret-very-secure-key-123456",
		},
		AgentHub: config.AgentHubSection{
			DevAllowLoopback:     true, // 测试环境允许 loopback Mock Agent
			HeartbeatIntervalSec: 2,
		},
	}

	// 实例化 Hub 管理器并注入全局
	mgr := hub.NewManager(db, &config.AppConfig)
	handlers.ResetGlobalHubManager(mgr)

	decReg, _ := decoder.NewRegistry(db)
	handlers.ResetGlobalDecoderRegistry(decReg)
	assembler, _, _ := handlers.InitRecorderAndDiff(db, decReg)
	t.Cleanup(func() {
		if assembler != nil {
			assembler.Close()
		}
	})

	r := gin.New()
	r.Use(gin.Recovery())

	// 挂载主路由（调用 main.go 中定义的路由挂载逻辑）
	RegisterRoutesForTest(r)

	return r, db
}

// generateTestJWT 生成测试用 JWT 字符串
func generateTestJWT(t *testing.T, user *commonModels.User) string {
	t.Helper()
	var roles []string
	if len(user.Roles) > 0 {
		_ = json.Unmarshal(user.Roles, &roles)
	}
	isAdmin := false
	for _, r := range roles {
		if r == commonAuth.RoleSuperAdmin || r == config.AdminRole {
			isAdmin = true
			break
		}
	}
	tokenStr, err := commonAuth.GenerateToken(
		user.ID,
		user.Username,
		user.Email,
		user.Name,
		isAdmin,
		roles,
		config.AppConfig.Auth.JWTSecret,
		24*time.Hour,
	)
	if err != nil {
		t.Fatalf("签发测试 JWT 失败: %v", err)
	}
	return tokenStr
}

func createTestUserInDB(t *testing.T, db *gorm.DB, username, role string) *commonModels.User {
	t.Helper()
	user := &commonModels.User{
		Username: username,
		Name:     username,
		Email:    username + "@example.com",
		Password: "hashed_password",
		Roles:    datatypes.JSON([]byte(`["` + role + `"]`)),
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	return user
}

// RegisterRoutesForTest 在测试中直接映射 main.go 中的 RegisterAPIRoutes
func RegisterRoutesForTest(r *gin.Engine) {
	openAPI := r.Group("/api")
	{
		openAPI.GET("/health", handlers.HealthCheck)
		openAPI.GET("/auth/config", handlers.GetAuthConfig)
		openAPI.POST("/login", handlers.Login)
	}

	rejectQueryToken := func(c *gin.Context) {
		if c.Query("token") != "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "严禁在 URL Query 参数中传递 Token"})
			return
		}
		c.Next()
	}

	buildAuth := func(c *gin.Context) {
		commonAuth.AuthMiddleware(commonAuth.AuthConfig{
			JWTSecretGetter: func() string { return config.AppConfig.Auth.JWTSecret },
			DB:              models.DB,
			PreloadAssocs:   []string{"Department"},
		})(c)
	}

	protected := r.Group("/api")
	protected.Use(rejectQueryToken)
	protected.Use(buildAuth)
	protected.Use(commonAudit.Middleware(config.ServiceName))
	{
		protected.GET("/me", handlers.GetMe)
		protected.GET("/users", handlers.SearchUsers)
		protected.GET("/dashboard/overview", handlers.GetDashboardOverview)

		requireAdmin := commonAuth.RequireAdmin(config.AdminRole)

		// 架构元素
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

		protected.POST("/arch/modules/:id/process-binding", requireAdmin, handlers.BindModuleProcess)
		protected.GET("/arch/modules/:id/services", handlers.ListModuleServices)
		protected.POST("/arch/modules/:id/services", requireAdmin, handlers.CreateModuleService)
		protected.PUT("/arch/module-services/:serviceId", requireAdmin, handlers.UpdateModuleService)
		protected.DELETE("/arch/module-services/:serviceId", requireAdmin, handlers.DeleteModuleService)
		protected.GET("/arch/modules/:id/dependencies", handlers.ListModuleDependencies)
		protected.POST("/arch/modules/:id/dependencies", requireAdmin, handlers.CreateModuleDependency)
		protected.DELETE("/arch/module-dependencies/:depId", requireAdmin, handlers.DeleteModuleDependency)
		protected.GET("/arch/services/available", handlers.ListAvailableServices)
		protected.GET("/arch/proto-services/claimable", handlers.ListClaimableProtoServices)

		// 4.1 物理进程蓝图域 (ADR-09, M10)
		protected.GET("/arch/process-groups", handlers.ListArchProcessGroups)
		protected.POST("/arch/process-groups", requireAdmin, handlers.CreateArchProcessGroup)
		protected.PUT("/arch/process-groups/:id", requireAdmin, handlers.UpdateArchProcessGroup)
		protected.DELETE("/arch/process-groups/:id", requireAdmin, handlers.DeleteArchProcessGroup)

		protected.GET("/arch/processes/tree", handlers.GetArchProcessTree)
		protected.GET("/arch/processes", handlers.ListArchProcesses)
		protected.GET("/arch/processes/:id", handlers.GetArchProcess)
		protected.POST("/arch/processes", requireAdmin, handlers.CreateArchProcess)
		protected.PUT("/arch/processes/:id", requireAdmin, handlers.UpdateArchProcess)
		protected.DELETE("/arch/processes/:id", requireAdmin, handlers.DeleteArchProcess)

		protected.GET("/arch/topology/graph", handlers.GetTopologyGraph)
		protected.GET("/arch/topology/static", handlers.GetStaticTopology)

		// 描述集
		protected.GET("/proto-schemas", handlers.ListProtoSchemas)
		protected.GET("/proto-schemas/dependencies-graph", handlers.GetProtoDependenciesGraph)
		protected.POST("/proto-schemas/batch-activate", requireAdmin, handlers.BatchActivateProtoSchemas)
		protected.GET("/proto-schemas/:id/download", handlers.DownloadProtoSchema)
		protected.GET("/proto-schemas/:id/summary", handlers.GetProtoSchemaSummary)
		protected.POST("/proto-schemas", requireAdmin, handlers.UploadProtoSchema)
		protected.PATCH("/proto-schemas/:id", requireAdmin, handlers.UpdateProtoSchema)
		protected.POST("/proto-schemas/:id/activate", requireAdmin, handlers.ActivateProtoSchema)
		protected.POST("/proto-schemas/:id/deactivate", requireAdmin, handlers.DeactivateProtoSchema)
		protected.DELETE("/proto-schemas/:id", requireAdmin, handlers.DeleteProtoSchema)
		protected.GET("/proto-services", handlers.ListProtoServices)
		protected.GET("/proto-operations", handlers.ListProtoOperations)
		protected.GET("/proto-operations/:id/schema-tree", handlers.GetOperationSchemaTree)
		protected.PUT("/proto-operations/:id/ignore-paths", requireAdmin, handlers.UpdateOperationIgnorePaths)

		// Agent
		protected.GET("/agents", handlers.ListAgents)
		protected.GET("/agents/:id", handlers.GetAgent)
		protected.POST("/agents", handlers.RegisterAgent)
		protected.PUT("/agents/:id", handlers.UpdateAgent)
		protected.DELETE("/agents/:id", handlers.DeleteAgent)
		protected.POST("/agents/:id/handshake", handlers.HandshakeAgent)
		protected.GET("/agents/:id/discover-processes", handlers.DiscoverAgentProcesses)
		protected.POST("/agents/:id/lease/force-unlock", requireAdmin, handlers.ForceUnlockAgentLease)
		protected.PUT("/agents/:id/process-mounts", handlers.BatchUpdateProcessMounts)
		protected.POST("/agents/:id/probes/disarm-all", handlers.DisarmAllProbes)
		protected.POST("/agents/:id/probes/rearm", handlers.RearmAgentProbes)

		// 进程
		protected.GET("/processes", handlers.ListProcesses)
		protected.GET("/processes/:id", handlers.GetProcess)
		protected.POST("/processes", handlers.CreateProcess)
		protected.PUT("/processes/:id", handlers.UpdateProcess)
		protected.DELETE("/processes/:id", handlers.DeleteProcess)
		protected.GET("/processes/:id/downstreams", handlers.GetProcessDownstreams)
		protected.POST("/processes/:id/disarm", handlers.DisarmProcessProbe)

		// 8. 协同录制与架构拓扑域 (F-05, F-11, F-16)
		protected.GET("/recording-sessions", handlers.ListRecordingSessions)
		protected.GET("/recording-sessions/:id", handlers.GetRecordingSession)
		protected.POST("/recording-sessions", handlers.StartRecordingSession)
		protected.POST("/recording-sessions/:id/stop", handlers.StopRecordingSession)
		protected.DELETE("/recording-sessions/:id/processes/:processId", handlers.RemoveProcessFromSession)
		protected.PATCH("/recording-sessions/:id/publish", handlers.PublishRecordingSession)
		protected.DELETE("/recording-sessions/:id", handlers.DeleteRecordingSession)
		protected.GET("/recording-sessions/:id/consistency-report", handlers.GetConsistencyReport)
		protected.GET("/recording-sessions/:id/transactions", handlers.GetRecordingSessionTransactions)
		protected.GET("/recording-sessions/:id/transactions/:rootRecordId/sequence", handlers.GetRecordingSessionTransactionSequence)

		// 9. 流量检索与解剖域 (F-03, F-11)
		protected.GET("/traffic-records", handlers.ListTrafficRecords)
		protected.GET("/traffic-records/:id/anatomy", handlers.GetTrafficRecordAnatomy)
		protected.GET("/traffic-records/:id/replay-closure", handlers.GetTrafficRecordReplayClosure)
		protected.POST("/traffic-records/:id/redecode", handlers.RedecodeTrafficRecord)

		// 10. 跨 Agent 同构离线重放与双层回归 Diff 域 (F-12, F-14)
		protected.GET("/replay-tasks", handlers.ListReplayTasks)
		protected.GET("/replay-tasks/:id", handlers.GetReplayTask)
		protected.POST("/replay-tasks", handlers.CreateReplayTask)
		protected.POST("/replay-tasks/:id/cancel", handlers.CancelReplayTask)
		protected.GET("/replay-tasks/:id/items", handlers.ListReplayItems)
		protected.GET("/replay-tasks/:id/items/:itemId", handlers.GetReplayItemDetail)
		protected.POST("/replay-tasks/:id/rediff", handlers.RediffReplayTask)

		// 11. SSE 实时事件流 (GET /api/events)
		protected.GET("/events", handlers.HandleEventsSSE)
	}
}

func TestSC01_HealthAuthAndPublicPrivateRBAC(t *testing.T) {
	r, db := setupRouterAndDB(t)

	admin := createTestUserInDB(t, db, "admin_user", "traefik_admin")
	adminToken := generateTestJWT(t, admin)

	userA := createTestUserInDB(t, db, "user_alice", "operator")
	userAToken := generateTestJWT(t, userA)

	userB := createTestUserInDB(t, db, "user_bob", "operator")
	userBToken := generateTestJWT(t, userB)

	// 1. GET /api/health 免密检查
	t.Run("HealthCheck", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("期望健康检查返回 200，实际: %d", w.Code)
		}
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["service"] != "Code-Traefik" || resp["module_key"] != "traefik" {
			t.Fatalf("健康检查响应格式不正确: %+v", resp)
		}
	})

	// 2. Auth 与 Token 传递安全校验
	t.Run("AuthAndTokenSecurityGuard", func(t *testing.T) {
		// 未携带 Token 请求 /api/me 拦截返回 401
		reqNoToken := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, reqNoToken)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("期望未认证请求返回 401，实际: %d", w.Code)
		}

		// 在 URL Query 中携带 Token 必须被 rejectQueryTokenMiddleware 严格拦截 (401)
		reqQueryToken := httptest.NewRequest(http.MethodGet, "/api/me?token="+userAToken, nil)
		w = httptest.NewRecorder()
		r.ServeHTTP(w, reqQueryToken)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("期望 Query 携带 Token 返回 401，实际: %d", w.Code)
		}

		// 合法 Authorization: Bearer 标头正常返回 200
		reqValid := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		reqValid.Header.Set("Authorization", "Bearer "+userAToken)
		w = httptest.NewRecorder()
		r.ServeHTTP(w, reqValid)
		if w.Code != http.StatusOK {
			t.Fatalf("期望合法 JWT 返回 200，实际: %d (body: %s)", w.Code, w.Body.String())
		}
	})

	// 3. 普通用户越权创建受保护资源被拦截 (403)
	t.Run("OrdinaryUserPrivilegeGuard", func(t *testing.T) {
		// 普通用户创建子系统 -> 403
		body, _ := json.Marshal(map[string]string{"code": "SUB_DEMO", "name": "演示子系统"})
		req := httptest.NewRequest(http.MethodPost, "/api/arch/subsystems", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+userAToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("期望普通用户创建子系统返回 403，实际: %d", w.Code)
		}

		// 普通用户创建 SHARED Agent -> 403
		agentBody, _ := json.Marshal(map[string]interface{}{
			"name":     "Shared-By-User",
			"scope":    "SHARED",
			"endpoint": "http://192.168.1.100:9091",
		})
		req = httptest.NewRequest(http.MethodPost, "/api/agents", bytes.NewReader(agentBody))
		req.Header.Set("Authorization", "Bearer "+userAToken)
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("期望普通用户创建 SHARED Agent 返回 403，实际: %d", w.Code)
		}
	})

	// 4. Mock Agent 准备与公私域 RBAC 及横向越权防御
	t.Run("PrivateAgentIDORGuardAndAudit", func(t *testing.T) {
		expectedClaimCode := "alice-secret-code-666"

		mockAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/agent/v1/health" {
				claimCodeHeader := r.Header.Get(agentproto.HeaderClaimCode)
				if claimCodeHeader != expectedClaimCode {
					w.WriteHeader(http.StatusForbidden)
					_ = json.NewEncoder(w).Encode(map[string]string{"error": "forbidden"})
					return
				}
				resp := agentproto.HandshakeResp{
					AgentVersion:    "1.0.0",
					ProtocolVersion: "1.0",
					BootID:          "boot-uuid-alice",
					KernelVersion:   "5.15.0",
					HasEbpfCap:      true,
					CapDetail:       "ebpf_trace",
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
			if r.URL.Path == "/agent/v1/purge" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]bool{"purged": true})
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer mockAgent.Close()

		// 用户 A 注册 PRIVATE Agent -> 201 Created
		createReqBody, _ := json.Marshal(map[string]interface{}{
			"name":       "Alice-Private-Host",
			"scope":      "PRIVATE",
			"endpoint":   mockAgent.URL,
			"claim_code": expectedClaimCode,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/agents", bytes.NewReader(createReqBody))
		req.Header.Set("Authorization", "Bearer "+userAToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("期望用户 A 成功注册 PRIVATE Agent (201)，实际: %d (body: %s)", w.Code, w.Body.String())
		}

		var createdAgent models.Agent
		_ = json.Unmarshal(w.Body.Bytes(), &createdAgent)
		if createdAgent.ID == 0 || createdAgent.Scope != models.ScopePrivate {
			t.Fatalf("注册所得 Agent 数据非法: %+v", createdAgent)
		}

		// 用户 B 横向越权尝试查看用户 A 的 PRIVATE Agent -> 403 Forbidden
		reqGetB := httptest.NewRequest(http.MethodGet, createdAgentPath(createdAgent.ID), nil)
		reqGetB.Header.Set("Authorization", "Bearer "+userBToken)
		w = httptest.NewRecorder()
		r.ServeHTTP(w, reqGetB)
		if w.Code != http.StatusForbidden {
			t.Fatalf("期望用户 B 查看用户 A 私有 Agent 返回 403，实际: %d", w.Code)
		}

		// 用户 B 横向越权尝试删除用户 A 的 PRIVATE Agent -> 403 Forbidden
		reqDelB := httptest.NewRequest(http.MethodDelete, createdAgentPath(createdAgent.ID), nil)
		reqDelB.Header.Set("Authorization", "Bearer "+userBToken)
		w = httptest.NewRecorder()
		r.ServeHTTP(w, reqDelB)
		if w.Code != http.StatusForbidden {
			t.Fatalf("期望用户 B 删除用户 A 私有 Agent 返回 403，实际: %d", w.Code)
		}

		// 用户 B 横向越权尝试探活用户 A 的 PRIVATE Agent -> 403 Forbidden
		reqHandshakeB := httptest.NewRequest(http.MethodPost, createdAgentPath(createdAgent.ID)+"/handshake", nil)
		reqHandshakeB.Header.Set("Authorization", "Bearer "+userBToken)
		w = httptest.NewRecorder()
		r.ServeHTTP(w, reqHandshakeB)
		if w.Code != http.StatusForbidden {
			t.Fatalf("期望用户 B 探活用户 A 私有 Agent 返回 403，实际: %d", w.Code)
		}

		// 用户 A (owner) 探活自己的 PRIVATE Agent -> 200 OK
		reqHandshakeA := httptest.NewRequest(http.MethodPost, createdAgentPath(createdAgent.ID)+"/handshake", nil)
		reqHandshakeA.Header.Set("Authorization", "Bearer "+userAToken)
		w = httptest.NewRecorder()
		r.ServeHTTP(w, reqHandshakeA)
		if w.Code != http.StatusOK {
			t.Fatalf("期望用户 A 探活自己的私有 Agent 返回 200，实际: %d (body: %s)", w.Code, w.Body.String())
		}
		var handshakeBody struct {
			Agent     models.Agent             `json:"agent"`
			Handshake agentproto.HandshakeResp `json:"handshake"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &handshakeBody); err != nil {
			t.Fatalf("反序列化握手响应失败: %v", err)
		}
		if handshakeBody.Handshake.AgentVersion != "1.0.0" {
			t.Fatalf("期望握手返回版本 1.0.0，实际: %s", handshakeBody.Handshake.AgentVersion)
		}
		if handshakeBody.Agent.Status != models.AgentStatusOnline {
			t.Fatalf("期望探活后 Agent 状态为 ONLINE，实际: %s", handshakeBody.Agent.Status)
		}

		// 管理员巡检查看用户 A 的 PRIVATE Agent -> 200 OK
		reqGetAdmin := httptest.NewRequest(http.MethodGet, createdAgentPath(createdAgent.ID), nil)
		reqGetAdmin.Header.Set("Authorization", "Bearer "+adminToken)
		w = httptest.NewRecorder()
		r.ServeHTTP(w, reqGetAdmin)
		if w.Code != http.StatusOK {
			t.Fatalf("期望管理员查看用户 A 私有 Agent 返回 200，实际: %d", w.Code)
		}

		// 管理员强制解锁该 Agent 的租约 -> 200 OK
		unlockBody, _ := json.Marshal(map[string]string{"reason": "紧急运维解除挂死租约"})
		reqUnlock := httptest.NewRequest(http.MethodPost, createdAgentPath(createdAgent.ID)+"/lease/force-unlock", bytes.NewReader(unlockBody))
		reqUnlock.Header.Set("Authorization", "Bearer "+adminToken)
		reqUnlock.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		r.ServeHTTP(w, reqUnlock)
		if w.Code != http.StatusOK {
			t.Fatalf("期望管理员强制解锁租约返回 200，实际: %d (body: %s)", w.Code, w.Body.String())
		}

		// 等待并刷新审计日志入库
		_ = commonAudit.Close(context.Background())

		var auditLogs []commonModels.SysAuditLog
		if err := db.Where("module = ?", config.ModuleKey).Find(&auditLogs).Error; err != nil {
			t.Fatalf("查询操作审计日志失败: %v", err)
		}
		if len(auditLogs) == 0 {
			t.Fatalf("期望在 sys_audit_logs 中记录操作审计日志，但记录数为 0")
		}

		var foundCreate, foundForceUnlock bool
		for _, logItem := range auditLogs {
			if logItem.Action == "create" && logItem.TargetType == "agent" {
				foundCreate = true
			}
			if logItem.Action == "force_unlock_lease" && logItem.TargetType == "agent" {
				foundForceUnlock = true
			}
		}
		if !foundCreate || !foundForceUnlock {
			t.Fatalf("审计日志中未包含完整的 create 或 force_unlock_lease 动作: %+v", auditLogs)
		}
	})

	// 5. ListAgents 公私域范围与本人私有隔离及管理员全量视角
	t.Run("ListAgentsScopeAndOwnerIsolation", func(t *testing.T) {
		// 先为用户 B 注册一个私有 Agent
		mockAgentB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/agent/v1/health" {
				resp := agentproto.HandshakeResp{
					AgentVersion:    "1.0.0",
					ProtocolVersion: "1.0",
					BootID:          "boot-uuid-bob",
					KernelVersion:   "5.15.0",
					HasEbpfCap:      true,
					CapDetail:       "ebpf_trace",
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer mockAgentB.Close()

		createReqBodyB, _ := json.Marshal(map[string]interface{}{
			"name":     "Bob-Private-Host-For-Isolation",
			"scope":    "PRIVATE",
			"endpoint": mockAgentB.URL,
		})
		reqB := httptest.NewRequest(http.MethodPost, "/api/agents", bytes.NewReader(createReqBodyB))
		reqB.Header.Set("Authorization", "Bearer "+userBToken)
		reqB.Header.Set("Content-Type", "application/json")
		wB := httptest.NewRecorder()
		r.ServeHTTP(wB, reqB)
		if wB.Code != http.StatusCreated {
			t.Fatalf("用户 B 注册私有 Agent 失败: %d (body: %s)", wB.Code, wB.Body.String())
		}

		// 再由管理员注册一个团队公共 Agent
		mockAgentShared := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/agent/v1/health" {
				resp := agentproto.HandshakeResp{
					AgentVersion:    "1.0.0",
					ProtocolVersion: "1.0",
					BootID:          "boot-uuid-shared",
					KernelVersion:   "5.15.0",
					HasEbpfCap:      true,
					CapDetail:       "ebpf_trace",
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer mockAgentShared.Close()

		createReqBodyShared, _ := json.Marshal(map[string]interface{}{
			"name":     "Shared-Team-Host-For-Isolation",
			"scope":    "SHARED",
			"endpoint": mockAgentShared.URL,
		})
		reqShared := httptest.NewRequest(http.MethodPost, "/api/agents", bytes.NewReader(createReqBodyShared))
		reqShared.Header.Set("Authorization", "Bearer "+adminToken)
		reqShared.Header.Set("Content-Type", "application/json")
		wShared := httptest.NewRecorder()
		r.ServeHTTP(wShared, reqShared)
		if wShared.Code != http.StatusCreated {
			t.Fatalf("管理员注册公共 Agent 失败: %d (body: %s)", wShared.Code, wShared.Body.String())
		}

		type agentListResp struct {
			Items []models.Agent `json:"items"`
			Total int64          `json:"total"`
		}

		// (1) 用户 A 查询 scope=PRIVATE: 必须仅包含 Alice 的 Agent，严禁包含 Bob 的私有 Agent 或团队公共 Agent
		{
			req := httptest.NewRequest(http.MethodGet, "/api/agents?scope=PRIVATE", nil)
			req.Header.Set("Authorization", "Bearer "+userAToken)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("用户 A 查询私有 Agent 失败: %d", w.Code)
			}
			var list agentListResp
			_ = json.Unmarshal(w.Body.Bytes(), &list)
			if len(list.Items) != 1 || list.Items[0].Name != "Alice-Private-Host" {
				t.Fatalf("用户 A 查询私有 Agent 越权泄露或未隔离: %+v", list.Items)
			}
		}

		// (2) 用户 A 查询 scope=SHARED: 必须仅包含公共 Agent，严禁混入任何私有 Agent
		{
			req := httptest.NewRequest(http.MethodGet, "/api/agents?scope=SHARED", nil)
			req.Header.Set("Authorization", "Bearer "+userAToken)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("用户 A 查询公共 Agent 失败: %d", w.Code)
			}
			var list agentListResp
			_ = json.Unmarshal(w.Body.Bytes(), &list)
			for _, item := range list.Items {
				if item.Scope != models.ScopeShared {
					t.Fatalf("用户 A 查询公共 Agent 列表中混入了非 SHARED Agent: %+v", item)
				}
			}
		}

		// (3) 用户 B 查询 scope=PRIVATE: 仅返回 Bob 自己的 Agent
		{
			req := httptest.NewRequest(http.MethodGet, "/api/agents?scope=PRIVATE", nil)
			req.Header.Set("Authorization", "Bearer "+userBToken)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("用户 B 查询私有 Agent 失败: %d", w.Code)
			}
			var list agentListResp
			_ = json.Unmarshal(w.Body.Bytes(), &list)
			if len(list.Items) != 1 || list.Items[0].Name != "Bob-Private-Host-For-Isolation" {
				t.Fatalf("用户 B 查询私有 Agent 结果不符合预期: %+v", list.Items)
			}
		}

		// (4) 用户 A 尝试携带 ?scope=PRIVATE&all_owners=true: 非管理员无效，依然仅返回本人私有 Agent
		{
			req := httptest.NewRequest(http.MethodGet, "/api/agents?scope=PRIVATE&all_owners=true", nil)
			req.Header.Set("Authorization", "Bearer "+userAToken)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("用户 A 查询私有 Agent 失败: %d", w.Code)
			}
			var list agentListResp
			_ = json.Unmarshal(w.Body.Bytes(), &list)
			if len(list.Items) != 1 || list.Items[0].Name != "Alice-Private-Host" {
				t.Fatalf("普通用户携带 all_owners=true 发生越权泄露: %+v", list.Items)
			}
		}

		// (5) 管理员查询 scope=PRIVATE (默认未开启 all_owners): 仅返回管理员本人私有 Agent (此处管理员未建，应为 0)
		{
			req := httptest.NewRequest(http.MethodGet, "/api/agents?scope=PRIVATE", nil)
			req.Header.Set("Authorization", "Bearer "+adminToken)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("管理员查询私有 Agent 失败: %d", w.Code)
			}
			var list agentListResp
			_ = json.Unmarshal(w.Body.Bytes(), &list)
			if len(list.Items) != 0 {
				t.Fatalf("管理员未开启 all_owners 模式下泄露了其他同事私有 Agent: %+v", list.Items)
			}
		}

		// (6) 管理员开启 all_owners=true 查询 scope=PRIVATE: 放行全量私有 Agent (包含 Alice 与 Bob)
		{
			req := httptest.NewRequest(http.MethodGet, "/api/agents?scope=PRIVATE&all_owners=true", nil)
			req.Header.Set("Authorization", "Bearer "+adminToken)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("管理员全量查询私有 Agent 失败: %d", w.Code)
			}
			var list agentListResp
			_ = json.Unmarshal(w.Body.Bytes(), &list)
			if len(list.Items) < 2 {
				t.Fatalf("管理员全量查询私有 Agent 期望至少 2 个，实际: %d", len(list.Items))
			}
			var hasAlice, hasBob bool
			for _, item := range list.Items {
				if item.Name == "Alice-Private-Host" {
					hasAlice = true
				}
				if item.Name == "Bob-Private-Host-For-Isolation" {
					hasBob = true
				}
			}
			if !hasAlice || !hasBob {
				t.Fatalf("管理员全量查询私有 Agent 未包含全部同事 Agent: %+v", list.Items)
			}
		}
	})
}

func createdAgentPath(id uint64) string {
	var buf [32]byte
	n := 0
	for id > 0 {
		buf[n] = byte('0' + id%10)
		id /= 10
		n++
	}
	for i, j := 0, n-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return "/api/agents/" + string(buf[:n])
}

// buildTestFDS 构造测试用合法的 FileDescriptorSet 二进制
func buildTestFDS(pkgName, svcName, methodName string) []byte {
	fileProto := &descriptorpb.FileDescriptorProto{
		Name:    proto.String(pkgName + ".proto"),
		Package: proto.String(pkgName),
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: proto.String(svcName),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:       proto.String(methodName),
						InputType:  proto.String("." + pkgName + ".Req"),
						OutputType: proto.String("." + pkgName + ".Rsp"),
					},
				},
			},
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("Req")},
			{Name: proto.String("Rsp")},
		},
	}
	fds := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fileProto}}
	raw, _ := proto.Marshal(fds)
	return raw
}

func createMultipartUploadReq(url string, values map[string]string, fieldName, fileName string, fileBytes []byte) (*http.Request, error) {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for k, v := range values {
		_ = w.WriteField(k, v)
	}
	part, err := w.CreateFormFile(fieldName, fileName)
	if err != nil {
		return nil, err
	}
	_, _ = part.Write(fileBytes)
	_ = w.Close()

	req, err := http.NewRequest(http.MethodPost, url, &b)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req, nil
}

func TestSC02_ArchProtoAndTopologyFlows(t *testing.T) {
	r, db := setupRouterAndDB(t)

	admin := createTestUserInDB(t, db, "sc02_admin", "traefik_admin")
	adminToken := generateTestJWT(t, admin)

	// 1. 创建子系统 SUB_TRADE
	subBody, _ := json.Marshal(map[string]interface{}{
		"code": "SUB_TRADE",
		"name": "交易子系统",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/arch/subsystems", bytes.NewReader(subBody))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("创建子系统失败: %d (body: %s)", w.Code, w.Body.String())
	}
	var sub models.Subsystem
	_ = json.Unmarshal(w.Body.Bytes(), &sub)

	// 2. 创建功能组 G1 (根组) 与 G2 (子组)
	g1Body, _ := json.Marshal(map[string]interface{}{
		"subsystem_id": sub.ID,
		"code":         "GRP_PAY",
		"name":         "支付组",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/arch/functional-groups", bytes.NewReader(g1Body))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("创建功能组 G1 失败: %d", w.Code)
	}
	var g1 models.FunctionalGroup
	_ = json.Unmarshal(w.Body.Bytes(), &g1)

	g2Body, _ := json.Marshal(map[string]interface{}{
		"subsystem_id": sub.ID,
		"parent_id":    g1.ID,
		"code":         "GRP_CARD",
		"name":         "银行卡组",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/arch/functional-groups", bytes.NewReader(g2Body))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("创建功能组 G2 失败: %d", w.Code)
	}
	var g2 models.FunctionalGroup
	_ = json.Unmarshal(w.Body.Bytes(), &g2)

	// 查询树
	reqTree := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/arch/functional-groups/tree?subsystem_id=%d", sub.ID), nil)
	reqTree.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, reqTree)
	if w.Code != http.StatusOK {
		t.Fatalf("查询功能组树失败: %d", w.Code)
	}

	// 3. 创建模块 MOD_PAY (在 G2 下) 与 MOD_RISK (在 G1 下)
	m1Body, _ := json.Marshal(map[string]interface{}{
		"group_id": g2.ID,
		"code":     "MOD_PAY_CORE",
		"name":     "核心支付模块",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/arch/modules", bytes.NewReader(m1Body))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("创建模块 M1 失败: %d", w.Code)
	}
	var mod1 models.Module
	_ = json.Unmarshal(w.Body.Bytes(), &mod1)

	m2Body, _ := json.Marshal(map[string]interface{}{
		"group_id": g1.ID,
		"code":     "MOD_RISK_CHECK",
		"name":     "风控校验模块",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/arch/modules", bytes.NewReader(m2Body))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("创建模块 M2 失败: %d", w.Code)
	}
	var mod2 models.Module
	_ = json.Unmarshal(w.Body.Bytes(), &mod2)

	// 3.1 验证 GET /api/arch/modules 列表查询 (防 500 回归及 page_size=1000 支持)
	req = httptest.NewRequest(http.MethodGet, "/api/arch/modules?page_size=1000", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("查询全量模块列表失败: %d (body: %s)", w.Code, w.Body.String())
	}
	var allModsResp struct {
		Items    []models.Module `json:"items"`
		Total    int64           `json:"total"`
		PageSize int             `json:"page_size"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &allModsResp)
	if allModsResp.Total < 2 || allModsResp.PageSize != 1000 {
		t.Fatalf("预期返回至少 2 个模块且 page_size=1000，实际: total=%d, page_size=%d", allModsResp.Total, allModsResp.PageSize)
	}

	// 3.2 验证按 group_id 过滤模块列表
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/arch/modules?group_id=%d&page_size=100", g1.ID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("按 group_id 查询模块列表失败: %d (body: %s)", w.Code, w.Body.String())
	}
	var groupModsResp struct {
		Items []models.Module `json:"items"`
		Total int64           `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &groupModsResp)
	if groupModsResp.Total != 1 || len(groupModsResp.Items) != 1 || groupModsResp.Items[0].ID != mod2.ID {
		t.Fatalf("预期 group_id=%d 下仅有 1 个模块 (mod2)，实际: total=%d, len=%d", g1.ID, groupModsResp.Total, len(groupModsResp.Items))
	}
	if groupModsResp.Items[0].Group == nil || groupModsResp.Items[0].Group.ID != g1.ID {
		t.Fatalf("预期 Group 正确预加载，实际: %+v", groupModsResp.Items[0].Group)
	}

	// 4. 为 MOD_RISK 声明对外供给服务 PROVIDE dophi.risk.v1.RiskService
	svcBody, _ := json.Marshal(map[string]interface{}{
		"service_name": "dophi.risk.v1.RiskService",
	})
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/arch/modules/%d/services", mod2.ID), bytes.NewReader(svcBody))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("声明 PROVIDE 服务契约失败: %d", w.Code)
	}
	var svcRisk models.ModuleService
	_ = json.Unmarshal(w.Body.Bytes(), &svcRisk)

	// 5. 尝试在 MOD_PAY 上声明相同的 PROVIDE 服务 -> 409 Conflict (A-05 供给全局唯一)
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/arch/modules/%d/services", mod1.ID), bytes.NewReader(svcBody))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("期望声明冲突 PROVIDE 返回 409，实际: %d", w.Code)
	}

	// 5.1 为 MOD_PAY 声明依赖 MOD_RISK 的服务
	depBody, _ := json.Marshal(map[string]interface{}{
		"service_id": svcRisk.ID,
	})
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/arch/modules/%d/dependencies", mod1.ID), bytes.NewReader(depBody))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("添加内部服务依赖失败: %d (body: %s)", w.Code, w.Body.String())
	}

	// 5.2 为 MOD_PAY 声明依赖外部第三方系统
	extDepBody, _ := json.Marshal(map[string]interface{}{
		"is_external":      true,
		"external_system":  "BankGateway",
		"external_service": "UnifiedPay",
	})
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/arch/modules/%d/dependencies", mod1.ID), bytes.NewReader(extDepBody))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("添加外部服务依赖失败: %d (body: %s)", w.Code, w.Body.String())
	}

	// 5.3 查询 MOD_PAY 的所有下游依赖清单 (防 Process: unsupported relations for schema Module 回归)
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/arch/modules/%d/dependencies", mod1.ID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("查询模块下游依赖清单失败: %d (body: %s)", w.Code, w.Body.String())
	}
	var depsResp struct {
		Items []models.ModuleDependency `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &depsResp)
	if len(depsResp.Items) != 2 {
		t.Fatalf("预期返回 2 条依赖，实际返回: %d", len(depsResp.Items))
	}

	// 6. 上传 Protobuf 描述集
	pbBytes := buildTestFDS("dophi.risk.v1", "RiskService", "VerifyRisk")
	upReq, err := createMultipartUploadReq("/api/proto-schemas", map[string]string{
		"name":          "risk_v1.pb",
		"auto_activate": "true",
	}, "file", "risk_v1.pb", pbBytes)
	if err != nil {
		t.Fatalf("创建上传请求失败: %v", err)
	}
	upReq.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, upReq)
	if w.Code != http.StatusCreated {
		t.Fatalf("上传并激活描述集失败: %d (body: %s)", w.Code, w.Body.String())
	}

	// 6.1 验证 GET /api/arch/proto-services/claimable 能够查到已激活且已被认领的 dophi.risk.v1.RiskService
	reqClaim := httptest.NewRequest(http.MethodGet, "/api/arch/proto-services/claimable", nil)
	reqClaim.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, reqClaim)
	if w.Code != http.StatusOK {
		t.Fatalf("查询可认领 Proto 服务失败: %d (body: %s)", w.Code, w.Body.String())
	}
	var claimResp struct {
		Items []struct {
			ServiceName     string `json:"service_name"`
			ProtoPackage    string `json:"proto_package"`
			ProtoService    string `json:"proto_service"`
			SchemaName      string `json:"schema_name"`
			OperationCount  int    `json:"operation_count"`
			IsClaimed       bool   `json:"is_claimed"`
			ClaimedByModule *struct {
				ID   string `json:"id"`
				Code string `json:"code"`
				Name string `json:"name"`
			} `json:"claimed_by_module"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &claimResp); err != nil {
		t.Fatalf("解析可认领服务列表失败: %v", err)
	}
	var foundRiskClaim bool
	for _, item := range claimResp.Items {
		if item.ServiceName == "dophi.risk.v1.RiskService" {
			foundRiskClaim = true
			if !item.IsClaimed || item.ClaimedByModule == nil || item.ClaimedByModule.Code != "MOD_RISK_CHECK" {
				t.Fatalf("RiskService 认领状态校验失败: %+v (claimed_by: %+v)", item, item.ClaimedByModule)
			}
		}
	}
	if !foundRiskClaim {
		t.Fatalf("未在可认领列表中找到 dophi.risk.v1.RiskService: %+v", claimResp.Items)
	}

	// 7. 查询静态拓扑
	reqTopo := httptest.NewRequest(http.MethodGet, "/api/arch/topology/static?view=LOGICAL", nil)
	reqTopo.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, reqTopo)
	if w.Code != http.StatusOK {
		t.Fatalf("查询静态拓扑失败: %d", w.Code)
	}
}

func TestSC03_ProcessBindingAndLeaseGuards(t *testing.T) {
	r, db := setupRouterAndDB(t)

	admin := createTestUserInDB(t, db, "sc03_admin", "traefik_admin")
	adminToken := generateTestJWT(t, admin)

	sub := models.Subsystem{Code: "SUB_PROC", Name: "进程测试子系统"}
	_ = db.Create(&sub).Error
	grp := models.FunctionalGroup{SubsystemID: sub.ID, Code: "GRP_P", Name: "进程组", Level: 1, Path: fmt.Sprintf("/%d/", sub.ID)}
	_ = db.Create(&grp).Error
	modA := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_A", Name: "模块A"}
	modB := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_B", Name: "模块B"}
	_ = db.Create(&modA).Error
	_ = db.Create(&modB).Error

	mockAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := agentproto.HandshakeResp{
			AgentVersion:    "1.0.0",
			ProtocolVersion: "1.0",
			BootID:          "boot-uuid-proc",
			KernelVersion:   "5.15.0",
			HasEbpfCap:      true,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer mockAgent.Close()

	// 注册 SHARED Agent
	sharedAgt := models.Agent{Name: "Shared-Agent-P", Scope: models.ScopeShared, Endpoint: mockAgent.URL, Status: models.AgentStatusOnline}
	_ = db.Create(&sharedAgt).Error

	// 1. 创建进程 Proc_1 挂载 [modA]
	p1Body, _ := json.Marshal(map[string]interface{}{
		"agent_id":    sharedAgt.ID,
		"name":        "Proc_1",
		"listen_port": 8801,
		"cmd_pattern": "./proc1",
		"module_ids":  []uint64{modA.ID},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/processes", bytes.NewReader(p1Body))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("创建 Proc_1 失败: %d (body: %s)", w.Code, w.Body.String())
	}
	var proc1 models.Process
	_ = json.Unmarshal(w.Body.Bytes(), &proc1)

	// 验证 SHARED 进程同步回写 Module.ArchProcessID
	var refModA models.Module
	_ = db.First(&refModA, modA.ID).Error
	if refModA.ArchProcessID == nil || *refModA.ArchProcessID != proc1.ArchProcessID {
		t.Fatalf("模块 A 的 ArchProcessID 未同步为 Proc_1.ArchProcessID: %+v", refModA)
	}

	// 2. 尝试创建同端口进程 -> 409 Conflict
	pConflictPort, _ := json.Marshal(map[string]interface{}{
		"agent_id":    sharedAgt.ID,
		"name":        "Proc_DupPort",
		"listen_port": 8801,
		"cmd_pattern": "./dup",
		"module_ids":  []uint64{modB.ID},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/processes", bytes.NewReader(pConflictPort))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("期望同端口创建进程返回 409，实际: %d", w.Code)
	}

	// 3. 尝试创建 Proc_2 再次绑定已挂载的 modA -> 409 Conflict (A-03 单域单进程)
	pConflictMod, _ := json.Marshal(map[string]interface{}{
		"agent_id":    sharedAgt.ID,
		"name":        "Proc_DupMod",
		"listen_port": 8802,
		"cmd_pattern": "./dup_mod",
		"module_ids":  []uint64{modA.ID},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/processes", bytes.NewReader(pConflictMod))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("期望重复绑定模块返回 409，实际: %d", w.Code)
	}

	// 4. 查询编译后的下游端点
	reqDown := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/processes/%d/downstreams", proc1.ID), nil)
	reqDown.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, reqDown)
	if w.Code != http.StatusOK {
		t.Fatalf("查询下游编译端点失败: %d", w.Code)
	}

	// 5. 模拟 Agent 处于排他租约中，尝试删除进程 -> 409 Conflict
	_ = db.Model(&models.Agent{}).Where("id = ?", sharedAgt.ID).Update("lease_type", models.AgentLeaseRecording).Error
	reqDelProc := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/processes/%d", proc1.ID), nil)
	reqDelProc.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, reqDelProc)
	if w.Code != http.StatusConflict {
		t.Fatalf("期望租约占用中删除进程返回 409，实际: %d", w.Code)
	}
}

func TestGetTopologyGraph_ETag304AndHardDeleteInvalidation(t *testing.T) {
	r, db := setupRouterAndDB(t)

	admin := createTestUserInDB(t, db, "topo_admin", config.AdminRole)
	adminToken := generateTestJWT(t, admin)

	// 1. 构造基础架构: 1 个子系统，1 个功能组，2 个模块，1 条依赖
	sub, err := topology.CreateSubsystem(db, topology.SubsystemInput{Code: "SUB_ETAG", Name: "ETag测试子系统"})
	if err != nil {
		t.Fatalf("创建子系统失败: %v", err)
	}
	grp, err := topology.CreateFunctionalGroup(db, topology.FunctionalGroupInput{
		SubsystemID: sub.ID,
		Code:        "GRP_ETAG",
		Name:        "ETag功能组",
	})
	if err != nil {
		t.Fatalf("创建功能组失败: %v", err)
	}
	m1, err := topology.CreateModule(db, topology.ModuleInput{
		GroupID: grp.ID,
		Code:    "MOD_SRC",
		Name:    "源模块",
	})
	if err != nil {
		t.Fatalf("创建 m1 失败: %v", err)
	}
	m2, err := topology.CreateModule(db, topology.ModuleInput{
		GroupID: grp.ID,
		Code:    "MOD_DST",
		Name:    "目标模块",
	})
	if err != nil {
		t.Fatalf("创建 m2 失败: %v", err)
	}
	svc, err := topology.CreateModuleService(db, topology.ModuleServiceInput{
		ModuleID:    m2.ID,
		ServiceName: "etag.v1.Service",
	})
	if err != nil {
		t.Fatalf("创建服务契约失败: %v", err)
	}
	dep, err := topology.CreateModuleDependency(db, topology.ModuleDependencyInput{
		ConsumerModuleID: m1.ID,
		ServiceID:        &svc.ID,
	})
	if err != nil {
		t.Fatalf("创建依赖失败: %v", err)
	}

	// 2. 首次请求 GET /api/arch/topology/graph
	req1 := httptest.NewRequest(http.MethodGet, "/api/arch/topology/graph?view=LOGICAL", nil)
	req1.Header.Set("Authorization", "Bearer "+adminToken)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("首次请求拓扑图失败: %d, body: %s", w1.Code, w1.Body.String())
	}
	etag1 := w1.Header().Get("ETag")
	if etag1 == "" {
		t.Fatalf("首次请求期望返回非空 ETag")
	}

	var dto1 topology.TopologyGraphDTO
	if err := json.Unmarshal(w1.Body.Bytes(), &dto1); err != nil {
		t.Fatalf("反序列化 DTO1 失败: %v", err)
	}
	if len(dto1.Edges) != 1 {
		t.Fatalf("首次请求期望 1 条依赖边，实际为 %d", len(dto1.Edges))
	}

	// 3. 携带 If-None-Match 二次请求 (未变更) -> 304 Not Modified
	req2 := httptest.NewRequest(http.MethodGet, "/api/arch/topology/graph?view=LOGICAL", nil)
	req2.Header.Set("Authorization", "Bearer "+adminToken)
	req2.Header.Set("If-None-Match", etag1)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusNotModified {
		t.Fatalf("未变更时期望返回 304 Not Modified，实际返回: %d", w2.Code)
	}
	if w2.Body.Len() > 0 {
		t.Fatalf("304 响应包体必须为空，实际长度为 %d", w2.Body.Len())
	}

	// 4. 物理硬删除依赖边: db.Unscoped().Delete(&models.ModuleDependency{}, dep.ID)
	if err := db.Unscoped().Delete(&models.ModuleDependency{}, dep.ID).Error; err != nil {
		t.Fatalf("物理硬删除依赖边失败: %v", err)
	}

	// 5. 携带旧 ETag 再次请求 -> 必须返回 200 OK 与新 ETag (严禁误判为 304)，边数减 1
	req3 := httptest.NewRequest(http.MethodGet, "/api/arch/topology/graph?view=LOGICAL", nil)
	req3.Header.Set("Authorization", "Bearer "+adminToken)
	req3.Header.Set("If-None-Match", etag1)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)

	if w3.Code != http.StatusOK {
		t.Fatalf("硬删除后期望返回 200 OK，实际返回: %d", w3.Code)
	}
	etag3 := w3.Header().Get("ETag")
	if etag3 == "" || etag3 == etag1 {
		t.Fatalf("硬删除后 ETag 必须刷新！旧: %s, 新: %s", etag1, etag3)
	}

	var dto3 topology.TopologyGraphDTO
	if err := json.Unmarshal(w3.Body.Bytes(), &dto3); err != nil {
		t.Fatalf("反序列化 DTO3 失败: %v", err)
	}
	if len(dto3.Edges) != 0 {
		t.Fatalf("硬删除后边数必须为 0，实际为 %d", len(dto3.Edges))
	}
}

func TestDownloadProtoSchemaBinary_Integrity(t *testing.T) {
	r, db := setupRouterAndDB(t)
	adminUser := createTestUserInDB(t, db, "admin_download", config.AdminRole)
	adminToken := generateTestJWT(t, adminUser)

	rawBytes := []byte{0x0a, 0x05, 0x74, 0x65, 0x73, 0x74, 0x2e, 0x70, 0x62, 0x12, 0x34}
	shaBytes := sha256.Sum256(rawBytes)
	fileSHA := hex.EncodeToString(shaBytes[:])

	schema := models.ProtoSchema{
		Name:            "demo_download_schema",
		Version:         1,
		FileName:        "demo_download.pb",
		FileSHA256:      fileSHA,
		FileSize:        len(rawBytes),
		DescriptorBytes: rawBytes,
		IsActive:        false,
		ProtoFiles:      datatypes.JSON("[\"demo.proto\"]"),
	}
	if err := db.Create(&schema).Error; err != nil {
		t.Fatalf("创建测试描述集失败: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/proto-schemas/%d/download", schema.ID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("下载描述集期望状态码 200，实际返回: %d (body: %s)", w.Code, w.Body.String())
	}

	if w.Header().Get("Content-Type") != "application/octet-stream" {
		t.Errorf("Content-Type 期望 application/octet-stream，实际: %s", w.Header().Get("Content-Type"))
	}

	disp := w.Header().Get("Content-Disposition")
	if !strings.Contains(disp, "demo_download.pb") {
		t.Errorf("Content-Disposition 期望包含 demo_download.pb，实际: %s", disp)
	}

	rspBytes := w.Body.Bytes()
	rspSHA := hex.EncodeToString(func() []byte { s := sha256.Sum256(rspBytes); return s[:] }())
	if rspSHA != fileSHA {
		t.Errorf("响应二进制 SHA256 (%s) 与预期 (%s) 不一致", rspSHA, fileSHA)
	}
}

func TestUpdateOperationIgnorePaths_ValidationAndAudit(t *testing.T) {
	r, db := setupRouterAndDB(t)
	adminUser := createTestUserInDB(t, db, "admin_ignore", config.AdminRole)
	adminToken := generateTestJWT(t, adminUser)

	normalUser := createTestUserInDB(t, db, "user_ignore", "developer")
	normalToken := generateTestJWT(t, normalUser)

	schema := models.ProtoSchema{
		Name:            "test_ignore_schema",
		Version:         1,
		FileName:        "test.pb",
		FileSHA256:      "aabbcc",
		FileSize:        100,
		DescriptorBytes: []byte{0x08, 0x01},
	}
	if err := db.Create(&schema).Error; err != nil {
		t.Fatalf("创建 Schema 失败: %v", err)
	}

	op := models.ProtoOperation{
		SchemaID:        schema.ID,
		ProtoFile:       "order.proto",
		ProtoPackage:    "dophi.order",
		ProtoService:    "OrderService",
		ServiceName:     "dophi.order.OrderService",
		Operation:       "CreateOrder",
		DiffIgnorePaths: datatypes.JSON("[]"),
	}
	if err := db.Create(&op).Error; err != nil {
		t.Fatalf("创建 Operation 失败: %v", err)
	}

	// 1. 普通用户越权修改 -> 403 Forbidden
	reqBodyNorm, _ := json.Marshal(gin.H{
		"diff_ignore_paths": []string{"server_time"},
	})
	reqNorm := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/proto-operations/%d/ignore-paths", op.ID), bytes.NewReader(reqBodyNorm))
	reqNorm.Header.Set("Authorization", "Bearer "+normalToken)
	reqNorm.Header.Set("Content-Type", "application/json")
	wNorm := httptest.NewRecorder()
	r.ServeHTTP(wNorm, reqNorm)
	if wNorm.Code != http.StatusForbidden {
		t.Errorf("普通用户期望 403，实际: %d", wNorm.Code)
	}

	// 2. 非法路径校验：含空字符串 -> 400 Bad Request
	reqBodyEmpty, _ := json.Marshal(gin.H{
		"diff_ignore_paths": []string{"valid_path", ""},
	})
	reqEmpty := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/proto-operations/%d/ignore-paths", op.ID), bytes.NewReader(reqBodyEmpty))
	reqEmpty.Header.Set("Authorization", "Bearer "+adminToken)
	reqEmpty.Header.Set("Content-Type", "application/json")
	wEmpty := httptest.NewRecorder()
	r.ServeHTTP(wEmpty, reqEmpty)
	if wEmpty.Code != http.StatusBadRequest {
		t.Errorf("空路径期望 400，实际: %d", wEmpty.Code)
	}

	// 3. 非法路径校验：含控制字符 \n -> 400 Bad Request
	reqBodyControl, _ := json.Marshal(gin.H{
		"diff_ignore_paths": []string{"valid_path\ninjection"},
	})
	reqControl := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/proto-operations/%d/ignore-paths", op.ID), bytes.NewReader(reqBodyControl))
	reqControl.Header.Set("Authorization", "Bearer "+adminToken)
	reqControl.Header.Set("Content-Type", "application/json")
	wControl := httptest.NewRecorder()
	r.ServeHTTP(wControl, reqControl)
	if wControl.Code != http.StatusBadRequest {
		t.Errorf("包含控制字符期望 400，实际: %d", wControl.Code)
	}

	// 4. 合法更新 diff_ignore_paths: ["server_time", "items[*].token"] -> 200 OK
	targetPaths := []string{"server_time", "items[*].token"}
	reqBodyValid, _ := json.Marshal(gin.H{
		"diff_ignore_paths": targetPaths,
	})
	reqValid := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/proto-operations/%d/ignore-paths", op.ID), bytes.NewReader(reqBodyValid))
	reqValid.Header.Set("Authorization", "Bearer "+adminToken)
	reqValid.Header.Set("Content-Type", "application/json")
	wValid := httptest.NewRecorder()
	r.ServeHTTP(wValid, reqValid)
	if wValid.Code != http.StatusOK {
		t.Fatalf("合法更新期望 200，实际: %d (body: %s)", wValid.Code, wValid.Body.String())
	}

	// 验证 DB 持久化
	var updatedOp models.ProtoOperation
	if err := db.First(&updatedOp, op.ID).Error; err != nil {
		t.Fatalf("查询已更新 Operation 失败: %v", err)
	}
	var savedPaths []string
	_ = json.Unmarshal(updatedOp.DiffIgnorePaths, &savedPaths)
	if len(savedPaths) != 2 || savedPaths[0] != "server_time" || savedPaths[1] != "items[*].token" {
		t.Errorf("持久化忽略路径不符合预期: %v", savedPaths)
	}

	// 验证审计日志产生
	_ = commonAudit.Close(context.Background())
	var auditLogs []commonModels.SysAuditLog
	_ = db.Where("module = ? AND action = ?", config.ModuleKey, "update_op_ignores").Find(&auditLogs).Error
	if len(auditLogs) == 0 {
		t.Fatalf("期望产生 update_op_ignores 审计日志，但未检索到")
	}
}

func TestGetOperationSchemaTree_SuccessAndNotFound(t *testing.T) {
	r, db := setupRouterAndDB(t)
	adminUser := createTestUserInDB(t, db, "admin_tree", config.AdminRole)
	adminToken := generateTestJWT(t, adminUser)

	// 构造包含简单描述的 FileDescriptorSet
	fileProto := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("demo/order.proto"),
		Package: proto.String("demo.order"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("SimpleReq"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("order_id"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
						JsonName: proto.String("orderId"),
					},
				},
			},
			{
				Name: proto.String("SimpleRsp"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("status_code"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
						JsonName: proto.String("statusCode"),
					},
				},
			},
		},
	}
	fds := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fileProto}}
	rawBytes, _ := proto.Marshal(fds)

	schema := models.ProtoSchema{
		Name:            "demo_tree_schema",
		Version:         1,
		FileName:        "demo_tree.pb",
		FileSHA256:      "112233",
		FileSize:        len(rawBytes),
		DescriptorBytes: rawBytes,
		ProtoFiles:      datatypes.JSON("[\"demo/order.proto\"]"),
	}
	if err := db.Create(&schema).Error; err != nil {
		t.Fatalf("创建测试 Schema 失败: %v", err)
	}

	op := models.ProtoOperation{
		SchemaID:           schema.ID,
		ProtoFile:          "demo/order.proto",
		ProtoPackage:       "demo.order",
		ProtoService:       "OrderService",
		ServiceName:        "demo.order.OrderService",
		Operation:          "GetOrder",
		ReqMessageFullName: "demo.order.SimpleReq",
		RspMessageFullName: "demo.order.SimpleRsp",
		DiffIgnorePaths:    datatypes.JSON("[\"order_id\"]"),
	}
	if err := db.Create(&op).Error; err != nil {
		t.Fatalf("创建测试 Operation 失败: %v", err)
	}

	// 1. 获取存在的 Operation 字典树 -> 200 OK
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/proto-operations/%d/schema-tree", op.ID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("获取字典树期望 200，实际: %d (body: %s)", w.Code, w.Body.String())
	}

	var dto decoder.OperationSchemaTreeDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("反序列化 OperationSchemaTreeDTO 失败: %v", err)
	}
	if dto.Operation != "GetOrder" || dto.ServiceName != "demo.order.OrderService" {
		t.Errorf("DTO 元信息不符合预期: %+v", dto)
	}
	if dto.ReqMessage == nil || len(dto.ReqMessage.Fields) != 1 {
		t.Fatalf("ReqMessage 期望 1 个字段，实际: %+v", dto.ReqMessage)
	}
	if dto.ReqMessage.Fields[0].Name != "order_id" || !dto.ReqMessage.Fields[0].IsIgnoredInDiff {
		t.Errorf("order_id 期望被标记为 is_ignored_in_diff=true，实际: %+v", dto.ReqMessage.Fields[0])
	}

	// 2. 获取不存在的 Operation 字典树 -> 404 Not Found
	req404 := httptest.NewRequest(http.MethodGet, "/api/proto-operations/999999/schema-tree", nil)
	req404.Header.Set("Authorization", "Bearer "+adminToken)
	w404 := httptest.NewRecorder()
	r.ServeHTTP(w404, req404)
	if w404.Code != http.StatusNotFound {
		t.Errorf("不存在的方法期望 404，实际: %d", w404.Code)
	}
}

func TestProtoSchemaSummary_BloodlineAndOrphanServices(t *testing.T) {
	r, db := setupRouterAndDB(t)
	adminUser := createTestUserInDB(t, db, "admin_summary", config.AdminRole)
	adminToken := generateTestJWT(t, adminUser)

	schema := models.ProtoSchema{
		Name:            "summary_schema",
		Version:         1,
		FileName:        "summary.pb",
		FileSHA256:      "aabbccdd",
		FileSize:        200,
		DescriptorBytes: []byte{0x08, 0x01},
		ProtoFiles:      datatypes.JSON("[\"s1.proto\", \"s2.proto\"]"),
	}
	if err := db.Create(&schema).Error; err != nil {
		t.Fatalf("创建 Schema 失败: %v", err)
	}

	// 创建 2 个不同服务的 Operation
	op1 := models.ProtoOperation{
		SchemaID:     schema.ID,
		ProtoFile:    "s1.proto",
		ProtoPackage: "demo",
		ProtoService: "ClaimedService",
		ServiceName:  "demo.ClaimedService",
		Operation:    "Method1",
	}
	op2 := models.ProtoOperation{
		SchemaID:     schema.ID,
		ProtoFile:    "s2.proto",
		ProtoPackage: "demo",
		ProtoService: "OrphanService",
		ServiceName:  "demo.OrphanService",
		Operation:    "Method2",
	}
	_ = db.Create(&op1).Error
	_ = db.Create(&op2).Error

	// 建立一个 Subsystem + Module + ModuleService 认领 demo.ClaimedService
	sub := models.Subsystem{Code: "SUM_SUB", Name: "SummarySub"}
	_ = db.Create(&sub).Error
	grp := models.FunctionalGroup{SubsystemID: sub.ID, Code: "SUM_GRP", Name: "SummaryGrp"}
	_ = db.Create(&grp).Error
	mod := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "SUM_MOD", Name: "SummaryModule"}
	_ = db.Create(&mod).Error
	ms := models.ModuleService{ModuleID: mod.ID, ServiceName: "demo.ClaimedService"}
	_ = db.Create(&ms).Error

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/proto-schemas/%d/summary", schema.ID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("获取描述集摘要期望 200，实际: %d (body: %s)", w.Code, w.Body.String())
	}

	var res struct {
		ProtoFiles      []string                      `json:"proto_files"`
		ServicesSummary []handlers.ServiceSummaryItem `json:"services_summary"`
		ClaimedRatio    float64                       `json:"claimed_ratio"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("反序列化摘要响应失败: %v", err)
	}

	if len(res.ProtoFiles) != 2 {
		t.Errorf("ProtoFiles 数量期望 2，实际: %d", len(res.ProtoFiles))
	}
	if len(res.ServicesSummary) != 2 {
		t.Fatalf("ServicesSummary 期望 2 个服务，实际: %d", len(res.ServicesSummary))
	}

	// 认领率期望 50% (1 个认领，1 个未认领孤儿)
	if res.ClaimedRatio < 0.49 || res.ClaimedRatio > 0.51 {
		t.Errorf("认领率期望 0.5，实际: %f", res.ClaimedRatio)
	}

	for _, s := range res.ServicesSummary {
		if s.ServiceName == "demo.ClaimedService" {
			if !s.IsClaimed || s.ClaimedModuleName != "SummaryModule" {
				t.Errorf("demo.ClaimedService 期望已认领且模块名为 SummaryModule，实际: %+v", s)
			}
		} else if s.ServiceName == "demo.OrphanService" {
			if s.IsClaimed {
				t.Errorf("demo.OrphanService 期望为未认领孤儿契约，实际: %+v", s)
			}
		}
	}
}

// TestArchProcessCRUD_AutoPinPortConflict_RegexValidateAndAntiIDOR 验证架构进程蓝图 CRUD、默认端口变更冲突自动固化与 Anti-IDOR 挂载血缘过滤 (F-02, ADR-09)
func TestArchProcessCRUD_AutoPinPortConflict_RegexValidateAndAntiIDOR(t *testing.T) {
	r, db := setupRouterAndDB(t)

	// 1. 初始化测试用户
	admin := createTestUserInDB(t, db, "admin_arch", "traefik_admin")
	userA := createTestUserInDB(t, db, "user_a", "operator")
	userB := createTestUserInDB(t, db, "user_b", "operator")

	adminToken := generateTestJWT(t, admin)
	userAToken := generateTestJWT(t, userA)
	userBToken := generateTestJWT(t, userB)

	// 2. 管理员创建架构进程组
	grpPayload, _ := json.Marshal(map[string]interface{}{
		"code":        "PGRP_CORE_TEST",
		"name":        "核心服务组",
		"sort_order":  1,
		"description": "核心业务进程组",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/arch/process-groups", bytes.NewReader(grpPayload))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("创建进程组失败: %d (body: %s)", w.Code, w.Body.String())
	}
	var grp models.ArchProcessGroup
	_ = json.Unmarshal(w.Body.Bytes(), &grp)

	// 3. 管理员创建标准进程 P1(:9001) 与 P2(:9002)
	p1Payload, _ := json.Marshal(map[string]interface{}{
		"group_id":            grp.ID,
		"code":                "PROC_P1",
		"name":                "进程P1",
		"default_listen_port": 9001,
		"default_cmd_pattern": "./bin/p1",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/arch/processes", bytes.NewReader(p1Payload))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("创建 P1 失败: %d (body: %s)", w.Code, w.Body.String())
	}
	var p1 models.ArchProcess
	_ = json.Unmarshal(w.Body.Bytes(), &p1)

	p2Payload, _ := json.Marshal(map[string]interface{}{
		"group_id":            grp.ID,
		"code":                "PROC_P2",
		"name":                "进程P2",
		"default_listen_port": 9002,
		"default_cmd_pattern": "./bin/p2",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/arch/processes", bytes.NewReader(p2Payload))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("创建 P2 失败: %d (body: %s)", w.Code, w.Body.String())
	}
	var p2 models.ArchProcess
	_ = json.Unmarshal(w.Body.Bytes(), &p2)

	// 4. 用户 A 创建私有 Agent (Agent-Dev)，并挂载 P1 (继承 :9001) 与 P2 (覆盖为 :19001)
	agentDev := models.Agent{
		Name:      "agent-dev-a",
		Endpoint:  "http://192.168.56.99:8090",
		Scope:     models.ScopePrivate,
		OwnerID:   &userA.ID,
		Status:    models.AgentStatusOnline,
		CreatedBy: userA.ID,
	}
	db.Create(&agentDev)

	procP1 := models.Process{
		AgentID:             agentDev.ID,
		ArchProcessID:       p1.ID,
		EffectiveListenPort: 9001,
		ProbeStatus:         models.ProbeStatusReady,
		CreatedBy:           userA.ID,
	}
	ovPort := 19001
	procP2 := models.Process{
		AgentID:             agentDev.ID,
		ArchProcessID:       p2.ID,
		OverrideListenPort:  &ovPort,
		EffectiveListenPort: 19001,
		ProbeStatus:         models.ProbeStatusReady,
		CreatedBy:           userA.ID,
	}
	db.Create(&procP1)
	db.Create(&procP2)

	// 5. 校验非法正则表达式: "re:[unclosed" -> 400 Bad Request
	badRegexPayload, _ := json.Marshal(map[string]interface{}{
		"group_id":            grp.ID,
		"code":                "PROC_P1",
		"name":                "进程P1",
		"default_listen_port": 9001,
		"default_cmd_pattern": "re:[unclosed",
	})
	req = httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/arch/processes/%d", p1.ID), bytes.NewReader(badRegexPayload))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("提交非法正则期望返回 400 Bad Request，实际: %d (body: %s)", w.Code, w.Body.String())
	}

	// 6. 管理员调用 PUT /api/arch/processes/:p1 将 P1 默认端口改为 19001 (与 Agent-Dev 上的 P2 冲突)
	// 期望: 成功返回 200 (不返 409 死锁)，自动将 Agent-Dev 上的 P1 固化 override_listen_port = 9001 并返回 auto_pinned_overrides
	changePortPayload, _ := json.Marshal(map[string]interface{}{
		"group_id":            grp.ID,
		"code":                "PROC_P1",
		"name":                "进程P1",
		"default_listen_port": 19001,
		"default_cmd_pattern": "./bin/p1",
	})
	req = httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/arch/processes/%d", p1.ID), bytes.NewReader(changePortPayload))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("修改 P1 端口期望返回 200，实际: %d (body: %s)", w.Code, w.Body.String())
	}
	var updateResp struct {
		Process             models.ArchProcess               `json:"process"`
		AutoPinnedOverrides []handlers.AutoPinnedOverrideDTO `json:"auto_pinned_overrides"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &updateResp); err != nil {
		t.Fatalf("反序列化更新响应失败: %v", err)
	}
	if len(updateResp.AutoPinnedOverrides) != 1 {
		t.Fatalf("期望 auto_pinned_overrides 包含 1 条固化记录，实际: %d", len(updateResp.AutoPinnedOverrides))
	}
	if updateResp.AutoPinnedOverrides[0].AgentID != agentDev.ID || updateResp.AutoPinnedOverrides[0].OverrideListenPort != 9001 {
		t.Fatalf("固化记录内容错误: %+v", updateResp.AutoPinnedOverrides[0])
	}

	// 验证 DB 中 procP1 已经固化 override_listen_port = 9001
	var dbProcP1 models.Process
	_ = db.First(&dbProcP1, procP1.ID)
	if dbProcP1.OverrideListenPort == nil || *dbProcP1.OverrideListenPort != 9001 || dbProcP1.EffectiveListenPort != 9001 {
		t.Fatalf("DB 中 procP1 期望固化 override_listen_port = 9001, 实际: %+v", dbProcP1)
	}

	// 7. Anti-IDOR: 普通用户 B 查询 GET /api/arch/processes/:p1
	// 期望: 绝不泄露用户 A 的私有 Agent 挂载记录 (agent_mounts 为空)
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/arch/processes/%d", p1.ID), nil)
	req.Header.Set("Authorization", "Bearer "+userBToken)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("用户 B 查询 P1 详情失败: %d", w.Code)
	}
	var userBDetail handlers.ArchProcessDetailDTO
	_ = json.Unmarshal(w.Body.Bytes(), &userBDetail)
	if len(userBDetail.AgentMounts) != 0 {
		t.Fatalf("普通用户 B 查询绝不能泄露用户 A 的私有 Agent 挂载记录，实际返回: %+v", userBDetail.AgentMounts)
	}

	// 用户 A 查询可以查到自己的挂载记录
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/arch/processes/%d", p1.ID), nil)
	req.Header.Set("Authorization", "Bearer "+userAToken)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("用户 A 查询 P1 详情失败: %d", w.Code)
	}
	var userADetail handlers.ArchProcessDetailDTO
	_ = json.Unmarshal(w.Body.Bytes(), &userADetail)
	if len(userADetail.AgentMounts) != 1 || userADetail.AgentMounts[0].AgentID != agentDev.ID {
		t.Fatalf("用户 A 查询应包含本人私有 Agent 挂载记录，实际返回: %+v", userADetail.AgentMounts)
	}
}

// TestBatchProcessMounts_OptimisticLock_AndDisarm 测试批量全量挂载乐观锁、冲突防卫与一键/单进程卸载探针 (ADR-09, AC-M10-02)
func TestBatchProcessMounts_OptimisticLock_AndDisarm(t *testing.T) {
	r, db := setupRouterAndDB(t)

	admin := createTestUserInDB(t, db, "admin_mount", "traefik_admin")
	adminToken := generateTestJWT(t, admin)

	// 1. 创建架构进程
	pgrp := models.ArchProcessGroup{Code: "PGRP_MOUNT", Name: "挂载测试组"}
	_ = db.Create(&pgrp).Error

	p1 := models.ArchProcess{GroupID: pgrp.ID, Code: "ARCH_P1", Name: "标准进程1", DefaultListenPort: 18001}
	p2 := models.ArchProcess{GroupID: pgrp.ID, Code: "ARCH_P2", Name: "标准进程2", DefaultListenPort: 18002}
	_ = db.Create(&p1).Error
	_ = db.Create(&p2).Error

	// 2. 创建 Agent
	agent := models.Agent{
		Name:           "Agent-Mount-Test",
		Endpoint:       "http://127.0.0.1:28099",
		Scope:          models.ScopeShared,
		Status:         models.AgentStatusOnline,
		MountsRevision: 5,
	}
	_ = db.Create(&agent).Error

	// 3. 乐观锁冲突测试：带入错误的 expected_revision = 4
	mountPayloadWrongRev := map[string]interface{}{
		"expected_revision": 4,
		"mounts": []map[string]interface{}{
			{"arch_process_id": p1.ID},
		},
	}
	bodyBytes, _ := json.Marshal(mountPayloadWrongRev)
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/agents/%d/process-mounts", agent.ID), bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict on revision mismatch, got %d: %s", w.Code, w.Body.String())
	}

	// 4. 端口冲突防卫测试：批次内两个进程配置相同非零端口
	portConflict := 19999
	mountPayloadPortConflict := map[string]interface{}{
		"expected_revision": 5,
		"mounts": []map[string]interface{}{
			{"arch_process_id": p1.ID, "override_listen_port": portConflict},
			{"arch_process_id": p2.ID, "override_listen_port": portConflict},
		},
	}
	bodyBytes, _ = json.Marshal(mountPayloadPortConflict)
	req = httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/agents/%d/process-mounts", agent.ID), bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict on port conflict, got %d: %s", w.Code, w.Body.String())
	}

	// 5. 成功批量全量挂载 (expected_revision = 5)
	mountPayloadSuccess := map[string]interface{}{
		"expected_revision": 5,
		"mounts": []map[string]interface{}{
			{"arch_process_id": p1.ID, "override_listen_port": 18001, "probe_desired_state": "ARMED"},
			{"arch_process_id": p2.ID, "override_listen_port": 18002, "probe_desired_state": "ARMED"},
		},
	}
	bodyBytes, _ = json.Marshal(mountPayloadSuccess)
	req = httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/agents/%d/process-mounts", agent.ID), bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("batch process mounts failed: %d, %s", w.Code, w.Body.String())
	}

	// 检查 MountsRevision 递增至 6
	var freshAgent models.Agent
	_ = db.First(&freshAgent, agent.ID).Error
	if freshAgent.MountsRevision != 6 {
		t.Fatalf("expected MountsRevision 6, got %d", freshAgent.MountsRevision)
	}

	var procs []models.Process
	_ = db.Where("agent_id = ?", agent.ID).Order("id ASC").Find(&procs).Error
	if len(procs) != 2 {
		t.Fatalf("expected 2 mounted processes, got %d", len(procs))
	}
	proc1ID := procs[0].ID

	// 6. 测试单进程卸载探针 POST /api/processes/:id/disarm
	// 构造活跃录制会话占用该进程
	sess := models.RecordingSession{
		SessionNo:        "REC_MOUNT_BUSY",
		Name:             "录制会话进行中",
		Scope:            models.ScopeShared,
		Status:           models.SessionStatusRecording,
		TargetProcessIDs: datatypes.JSON(fmt.Sprintf("[%d]", proc1ID)),
	}
	_ = db.Create(&sess).Error

	// 未传 force=true 拦截 409
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/processes/%d/disarm", proc1ID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict without force on active session, got %d: %s", w.Code, w.Body.String())
	}

	// 携带 force=true 强制卸载成功
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/processes/%d/disarm?force=true", proc1ID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with force=true, got %d: %s", w.Code, w.Body.String())
	}

	var freshProc1 models.Process
	_ = db.First(&freshProc1, proc1ID).Error
	if freshProc1.ProbeDesiredState != models.ProbeDesiredStateDisarmed || freshProc1.ProbeStatus != models.ProbeStatusDisarmed {
		t.Fatalf("expected DISARMED states on proc1, got %s / %s", freshProc1.ProbeDesiredState, freshProc1.ProbeStatus)
	}

	// 7. 测试一键卸载全部探针 POST /api/agents/:id/probes/disarm-all
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/agents/%d/probes/disarm-all", agent.ID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on disarm-all, got %d: %s", w.Code, w.Body.String())
	}

	var allProcsAfter []models.Process
	_ = db.Where("agent_id = ?", agent.ID).Find(&allProcsAfter).Error
	for _, p := range allProcsAfter {
		if p.ProbeDesiredState != models.ProbeDesiredStateDisarmed || p.ProbeStatus != models.ProbeStatusDisarmed {
			t.Fatalf("expected all procs DISARMED after disarm-all, got %s / %s", p.ProbeDesiredState, p.ProbeStatus)
		}
	}

	// 8. 测试探针重试对账 POST /api/agents/:id/probes/rearm
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/agents/%d/probes/rearm", agent.ID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on rearm, got %d: %s", w.Code, w.Body.String())
	}
	var rearmResp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &rearmResp)
	if rearmResp["message"] == nil {
		t.Fatalf("expected message in rearm response, got: %v", rearmResp)
	}
}

// buildTestBaseLibDescriptorBytes 构造 0 Service 纯公共消息包 common-base.pb (引用 google/protobuf/timestamp.proto)
func buildTestBaseLibDescriptorBytes(t *testing.T, withMoney bool) []byte {
	t.Helper()

	baseHeaderFields := []*descriptorpb.FieldDescriptorProto{
		{
			Name:   proto.String("trace_id"),
			Number: proto.Int32(1),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
		},
		{
			Name:     proto.String("timestamp"),
			Number:   proto.Int32(2),
			Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
			TypeName: proto.String(".google.protobuf.Timestamp"),
		},
	}

	messages := []*descriptorpb.DescriptorProto{
		{
			Name:  proto.String("BaseHeader"),
			Field: baseHeaderFields,
		},
	}

	if withMoney {
		moneyFields := []*descriptorpb.FieldDescriptorProto{
			{
				Name:   proto.String("currency"),
				Number: proto.Int32(1),
				Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
			},
			{
				Name:   proto.String("amount"),
				Number: proto.Int32(2),
				Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:   descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
			},
		}
		messages = append(messages, &descriptorpb.DescriptorProto{
			Name:  proto.String("Money"),
			Field: moneyFields,
		})
	}

	fileProto := &descriptorpb.FileDescriptorProto{
		Syntax:      proto.String("proto3"),
		Name:        proto.String("common/base.proto"),
		Package:     proto.String("common"),
		Dependency:  []string{"google/protobuf/timestamp.proto"},
		MessageType: messages,
		Service:     []*descriptorpb.ServiceDescriptorProto{}, // 0 Service
	}

	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{fileProto},
	}
	raw, err := proto.Marshal(fds)
	if err != nil {
		t.Fatalf("序列化 common-base.pb 失败: %v", err)
	}
	return raw
}

// buildTestOrderWithBaseDescriptorBytes 构造不带 --include_imports 的拆包 order.pb (外部依赖 common/base.proto)
func buildTestOrderWithBaseDescriptorBytes(t *testing.T, includeCancel bool) []byte {
	t.Helper()

	reqFields := []*descriptorpb.FieldDescriptorProto{
		{
			Name:     proto.String("header"),
			Number:   proto.Int32(1),
			Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
			TypeName: proto.String(".common.BaseHeader"),
		},
		{
			Name:     proto.String("total_fee"),
			Number:   proto.Int32(2),
			Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
			TypeName: proto.String(".common.Money"),
		},
		{
			Name:   proto.String("order_id"),
			Number: proto.Int32(3),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
		},
	}

	rspFields := []*descriptorpb.FieldDescriptorProto{
		{
			Name:     proto.String("header"),
			Number:   proto.Int32(1),
			Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
			TypeName: proto.String(".common.BaseHeader"),
		},
		{
			Name:   proto.String("success"),
			Number: proto.Int32(2),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_BOOL.Enum(),
		},
	}

	messages := []*descriptorpb.DescriptorProto{
		{
			Name:  proto.String("CreateOrderReq"),
			Field: reqFields,
		},
		{
			Name:  proto.String("CreateOrderRsp"),
			Field: rspFields,
		},
	}

	methods := []*descriptorpb.MethodDescriptorProto{
		{
			Name:       proto.String("CreateOrder"),
			InputType:  proto.String(".order.CreateOrderReq"),
			OutputType: proto.String(".order.CreateOrderRsp"),
		},
	}

	if includeCancel {
		messages = append(messages, &descriptorpb.DescriptorProto{
			Name: proto.String("CancelOrderReq"),
			Field: []*descriptorpb.FieldDescriptorProto{
				{
					Name:   proto.String("order_id"),
					Number: proto.Int32(1),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				},
			},
		}, &descriptorpb.DescriptorProto{
			Name: proto.String("CancelOrderRsp"),
			Field: []*descriptorpb.FieldDescriptorProto{
				{
					Name:   proto.String("cancelled"),
					Number: proto.Int32(1),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_BOOL.Enum(),
				},
			},
		})

		methods = append(methods, &descriptorpb.MethodDescriptorProto{
			Name:       proto.String("CancelOrder"),
			InputType:  proto.String(".order.CancelOrderReq"),
			OutputType: proto.String(".order.CancelOrderRsp"),
		})
	}

	fileProto := &descriptorpb.FileDescriptorProto{
		Syntax:      proto.String("proto3"),
		Name:        proto.String("order/order.proto"),
		Package:     proto.String("order"),
		Dependency:  []string{"common/base.proto"},
		MessageType: messages,
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name:   proto.String("OrderService"),
				Method: methods,
			},
		},
	}

	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{fileProto},
	}
	raw, err := proto.Marshal(fds)
	if err != nil {
		t.Fatalf("序列化 order.pb 失败: %v", err)
	}
	return raw
}

// TestProtoSchemaLedgerAPI_LatestOnlyGrouping_BatchActivate_Deactivate_GraphAndDownloadHeaders 验证 M11 文件资产台账、零 N+1 聚合、停用反向依赖门禁、批量激活与下载安全头 (F-03.5, F-03.6)
func TestProtoSchemaLedgerAPI_LatestOnlyGrouping_BatchActivate_Deactivate_GraphAndDownloadHeaders(t *testing.T) {
	r, db := setupRouterAndDB(t)
	adminUser := createTestUserInDB(t, db, "admin_ledger", config.AdminRole)
	adminToken := generateTestJWT(t, adminUser)

	baseV1Bytes := buildTestBaseLibDescriptorBytes(t, true)
	baseV2Bytes := buildTestBaseLibDescriptorBytes(t, true)
	orderV1Bytes := buildTestOrderWithBaseDescriptorBytes(t, false)
	orderV2Bytes := buildTestOrderWithBaseDescriptorBytes(t, true)

	// 1. 构造 base v1 (未生效), base v2 (生效), order v1 (生效), order v2 (草稿)
	baseV1 := models.ProtoSchema{
		Name:            "base",
		Version:         1,
		Category:        models.ProtoCategoryBaseLib,
		FileName:        "base_v1.pb",
		FileSHA256:      "sha_base_v1",
		FileSize:        len(baseV1Bytes),
		DescriptorBytes: baseV1Bytes,
		IsActive:        false,
		ProtoFiles:      datatypes.JSON(`["common/base.proto"]`),
		ProtoFileCount:  1,
	}
	baseV2 := models.ProtoSchema{
		Name:            "base",
		Version:         2,
		Category:        models.ProtoCategoryBaseLib,
		FileName:        "base_v2.pb",
		FileSHA256:      "sha_base_v2",
		FileSize:        len(baseV2Bytes),
		DescriptorBytes: baseV2Bytes,
		IsActive:        true,
		ProtoFiles:      datatypes.JSON(`["common/base.proto"]`),
		ProtoFileCount:  1,
	}
	orderV1 := models.ProtoSchema{
		Name:            "order",
		Version:         1,
		Category:        models.ProtoCategoryBusiness,
		FileName:        "order_v1.pb",
		FileSHA256:      "sha_order_v1",
		FileSize:        len(orderV1Bytes),
		DescriptorBytes: orderV1Bytes,
		IsActive:        true,
		ProtoFiles:      datatypes.JSON(`["order/order.proto"]`),
		ExternalImports: datatypes.JSON(`["common/base.proto"]`),
		ProtoFileCount:  1,
		ServiceCount:    1,
		OperationCount:  1,
	}
	orderV2 := models.ProtoSchema{
		Name:            "order",
		Version:         2,
		Category:        models.ProtoCategoryBusiness,
		FileName:        "order_v2.pb",
		FileSHA256:      "sha_order_v2",
		FileSize:        len(orderV2Bytes),
		DescriptorBytes: orderV2Bytes,
		IsActive:        false,
		ProtoFiles:      datatypes.JSON(`["order/order.proto"]`),
		ExternalImports: datatypes.JSON(`["common/base.proto"]`),
		ProtoFileCount:  1,
		ServiceCount:    1,
		OperationCount:  1,
	}
	_ = db.Create(&baseV1).Error
	_ = db.Create(&baseV2).Error
	_ = db.Create(&orderV1).Error
	_ = db.Create(&orderV2).Error

	opOrder := models.ProtoOperation{
		SchemaID:           orderV1.ID,
		ProtoFile:          "order/order.proto",
		ProtoPackage:       "order",
		ProtoService:       "OrderService",
		ServiceName:        "order.OrderService",
		Operation:          "CreateOrder",
		ReqMessageFullName: "order.CreateOrderReq",
		RspMessageFullName: "order.CreateOrderRsp",
		IsActive:           true,
	}
	_ = db.Create(&opOrder).Error

	opOrder2 := models.ProtoOperation{
		SchemaID:           orderV2.ID,
		ProtoFile:          "order/order.proto",
		ProtoPackage:       "order",
		ProtoService:       "OrderService",
		ServiceName:        "order.OrderService",
		Operation:          "CreateOrder",
		ReqMessageFullName: "order.CreateOrderReq",
		RspMessageFullName: "order.CreateOrderRsp",
		IsActive:           false,
	}
	_ = db.Create(&opOrder2).Error

	// 重置全局编解码中心（从包含激活 Schema 的数据库重新拉取）
	reg, err := decoder.NewRegistry(db)
	if err != nil {
		t.Fatalf("NewRegistry 失败: %v", err)
	}
	handlers.ResetGlobalDecoderRegistry(reg)

	// 认领 order.OrderService
	sub := models.Subsystem{Code: "SUB_LEDGER", Name: "LedgerSub"}
	_ = db.Create(&sub).Error
	grp := models.FunctionalGroup{SubsystemID: sub.ID, Code: "GRP_LEDGER", Name: "LedgerGrp"}
	_ = db.Create(&grp).Error
	mod := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_LEDGER", Name: "OrderModule"}
	_ = db.Create(&mod).Error
	ms := models.ModuleService{ModuleID: mod.ID, ServiceName: "order.OrderService"}
	_ = db.Create(&ms).Error

	// (A) 测试 GET /api/proto-schemas (默认 latest_only=true)
	req := httptest.NewRequest(http.MethodGet, "/api/proto-schemas", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("查询台账期望 200，实际: %d (body: %s)", w.Code, w.Body.String())
	}

	var res struct {
		Items       []handlers.ProtoSchemaLedgerItemDTO `json:"items"`
		Total       int64                               `json:"total"`
		PoolSummary handlers.ProtoPoolSummaryDTO        `json:"pool_summary"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("反序列化台账响应失败: %v", err)
	}
	if res.Total != 2 || len(res.Items) != 2 {
		t.Fatalf("latest_only=true 期望返回 2 条代表版本，实际 total=%d, len=%d", res.Total, len(res.Items))
	}
	if res.PoolSummary.ActiveSchemaCount != 2 || res.PoolSummary.ActiveBaseLibCount != 1 {
		t.Fatalf("PoolSummary 统计不符: %+v", res.PoolSummary)
	}

	for _, item := range res.Items {
		if item.Name == "base" {
			if item.Version != 2 || !item.IsActive || item.VersionCount != 2 {
				t.Errorf("base 代表版本期望 v2 且 version_count=2, 实际: %+v", item)
			}
		} else if item.Name == "order" {
			if item.Version != 1 || !item.IsActive || item.VersionCount != 2 {
				t.Errorf("order 代表版本期望 v1(active) 且 version_count=2, 实际: %+v", item)
			}
			if item.ClaimedRatio < 0.99 {
				t.Errorf("order 认领率期望 1.0, 实际: %f", item.ClaimedRatio)
			}
			if len(item.ResolvedDependencies) != 1 || item.ResolvedDependencies[0].Status != "READY" {
				t.Errorf("order 依赖状态期望 1 项 READY, 实际: %+v", item.ResolvedDependencies)
			}
		}
	}

	// (B) 测试 latest_only=false&name=base 拉取完整版本栈
	req = httptest.NewRequest(http.MethodGet, "/api/proto-schemas?latest_only=false&name=base", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("查询 base 历史版本期望 200，实际: %d", w.Code)
	}
	var resBaseHist struct {
		Items []handlers.ProtoSchemaLedgerItemDTO `json:"items"`
		Total int64                               `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resBaseHist)
	if resBaseHist.Total != 2 {
		t.Fatalf("base 历史版本数量期望 2，实际: %d", resBaseHist.Total)
	}

	// (C) 测试 PATCH /api/proto-schemas/:id 更新元数据
	patchBody := `{"description":"订单微服务最新定义","version_tag":"v1.1.0"}`
	req = httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/proto-schemas/%d", orderV2.ID), strings.NewReader(patchBody))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH 描述集元数据期望 200，实际: %d (body: %s)", w.Code, w.Body.String())
	}

	// (D) 测试 POST /api/proto-schemas/:id/deactivate
	// 先停用 orderV1
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/proto-schemas/%d/deactivate", orderV1.ID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("停用 orderV1 期望 200，实际: %d (body: %s)", w.Code, w.Body.String())
	}

	// 再停用 baseV2
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/proto-schemas/%d/deactivate", baseV2.ID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("停用 baseV2 期望 200，实际: %d (body: %s)", w.Code, w.Body.String())
	}

	// (E) 测试 POST /api/proto-schemas/batch-activate 批量激活 [baseV2, orderV2]
	batchBody := fmt.Sprintf(`{"schema_ids":[%d,%d]}`, baseV2.ID, orderV2.ID)
	req = httptest.NewRequest(http.MethodPost, "/api/proto-schemas/batch-activate", strings.NewReader(batchBody))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("批量激活期望 200，实际: %d (body: %s)", w.Code, w.Body.String())
	}

	// (F) 测试 GET /api/proto-schemas/dependencies-graph
	req = httptest.NewRequest(http.MethodGet, "/api/proto-schemas/dependencies-graph", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("查询依赖图期望 200，实际: %d", w.Code)
	}
	var graphRes struct {
		Nodes []struct {
			ID   uint64 `json:"id"`
			Name string `json:"name"`
		} `json:"nodes"`
		Edges []struct {
			FromSchemaID  uint64   `json:"from_schema_id"`
			ToSchemaID    uint64   `json:"to_schema_id"`
			MatchedProtos []string `json:"matched_protos"`
		} `json:"edges"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &graphRes)
	if len(graphRes.Nodes) != 2 {
		t.Fatalf("依赖图节点期望 2，实际: %d", len(graphRes.Nodes))
	}
	if len(graphRes.Edges) != 1 || graphRes.Edges[0].FromSchemaID != orderV2.ID || graphRes.Edges[0].ToSchemaID != baseV2.ID {
		t.Errorf("依赖图有向边不符合预期: %+v", graphRes.Edges)
	}

	// (G) 测试扩展名拦截 (非法扩展名返回 400)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("name", "bad_ext")
	part, _ := writer.CreateFormFile("file", "malicious.exe")
	_, _ = part.Write([]byte("not a pb"))
	_ = writer.Close()

	req = httptest.NewRequest(http.MethodPost, "/api/proto-schemas", body)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("上传 .exe 期望 400 Bad Request，实际: %d", w.Code)
	}

	// (H) 测试 GET /api/proto-schemas/:id/download 安全头
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/proto-schemas/%d/download", baseV2.ID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("下载描述集期望 200，实际: %d", w.Code)
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("期望 X-Content-Type-Options: nosniff，实际为 %q", w.Header().Get("X-Content-Type-Options"))
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("期望 Cache-Control: no-store，实际为 %q", w.Header().Get("Cache-Control"))
	}
	if !strings.Contains(w.Header().Get("Content-Disposition"), "attachment; filename=\"base_v2.pb\"") {
		t.Errorf("Content-Disposition 响应头不符: %q", w.Header().Get("Content-Disposition"))
	}
}

// TestListProtoServices_ServiceLevelPagination_SourceSchemaBadge_AndUnclaimedFilter 验证按 Service 组级分页与孤儿过滤 (F-03.1, F-03.7)
func TestListProtoServices_ServiceLevelPagination_SourceSchemaBadge_AndUnclaimedFilter(t *testing.T) {
	r, db := setupRouterAndDB(t)
	adminUser := createTestUserInDB(t, db, "admin_services", config.AdminRole)
	adminToken := generateTestJWT(t, adminUser)

	schema := models.ProtoSchema{
		Name:            "svc_schema",
		Version:         1,
		Category:        models.ProtoCategoryBusiness,
		FileName:        "svc.pb",
		FileSHA256:      "sha_svc",
		FileSize:        1024,
		DescriptorBytes: []byte{0x08, 0x01},
		IsActive:        true,
	}
	if err := db.Create(&schema).Error; err != nil {
		t.Fatalf("创建 Schema 失败: %v", err)
	}

	// 构造 5 个不同的服务，每个服务 3 个方法 (共 15 个方法)
	svcNames := []string{"test.SvcA", "test.SvcB", "test.SvcC", "test.SvcD", "test.SvcE"}
	for _, sn := range svcNames {
		for i := 1; i <= 3; i++ {
			op := models.ProtoOperation{
				SchemaID:           schema.ID,
				ProtoFile:          "test/service.proto",
				ProtoPackage:       "test",
				ProtoService:       sn[5:],
				ServiceName:        sn,
				Operation:          fmt.Sprintf("Method%d", i),
				ReqMessageFullName: fmt.Sprintf("%s.Req%d", sn, i),
				RspMessageFullName: fmt.Sprintf("%s.Rsp%d", sn, i),
				IsActive:           true,
			}
			_ = db.Create(&op).Error
		}
	}

	// 认领其中 2 个服务 (test.SvcA 与 test.SvcB)
	sub := models.Subsystem{Code: "SUB_SVC", Name: "SvcSub"}
	_ = db.Create(&sub).Error
	grp := models.FunctionalGroup{SubsystemID: sub.ID, Code: "GRP_SVC", Name: "SvcGrp"}
	_ = db.Create(&grp).Error
	mod := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_SVC", Name: "ServiceModule"}
	_ = db.Create(&mod).Error
	_ = db.Create(&models.ModuleService{ModuleID: mod.ID, ServiceName: "test.SvcA"}).Error
	_ = db.Create(&models.ModuleService{ModuleID: mod.ID, ServiceName: "test.SvcB"}).Error

	// 1. 请求 GET /api/proto-services?page=1&page_size=2
	req := httptest.NewRequest(http.MethodGet, "/api/proto-services?page=1&page_size=2", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/proto-services 期望 200，实际: %d (body: %s)", w.Code, w.Body.String())
	}

	var resPage1 struct {
		Items []handlers.ServiceContractGroupDTO `json:"items"`
		Total int64                              `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resPage1); err != nil {
		t.Fatalf("反序列化服务目录响应失败: %v", err)
	}

	// 服务级分页断言：total 必须是 5 个 Service，当前页精确返回 2 个完整 Service
	if resPage1.Total != 5 {
		t.Fatalf("服务目录 total 期望 5 个 Service，实际为: %d", resPage1.Total)
	}
	if len(resPage1.Items) != 2 {
		t.Fatalf("page_size=2 期望精确返回 2 个 Service，实际为: %d", len(resPage1.Items))
	}
	// 验证同一 Service 的方法绝不被截断 (每个 Service 必须含有完整 3 个 operations)
	for _, item := range resPage1.Items {
		if len(item.Operations) != 3 || item.OperationCount != 3 {
			t.Errorf("Service [%s] 方法被截断，期望 3 个，实际为 %d 个", item.ServiceName, len(item.Operations))
		}
		if !item.IsClaimed || item.ClaimedByModule == nil || item.ClaimedByModule.ModuleName != "ServiceModule" {
			t.Errorf("前两个服务期望已认领，实际: %+v", item)
		}
		if item.SourceSchema["name"] != "svc_schema" {
			t.Errorf("source_schema 携带信息不符合预期: %+v", item.SourceSchema)
		}
	}

	// 2. 请求 GET /api/proto-services?claimed_status=UNCLAIMED (孤儿服务过滤)
	req = httptest.NewRequest(http.MethodGet, "/api/proto-services?claimed_status=UNCLAIMED", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/proto-services?claimed_status=UNCLAIMED 期望 200，实际: %d", w.Code)
	}

	var resUnclaimed struct {
		Items []handlers.ServiceContractGroupDTO `json:"items"`
		Total int64                              `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resUnclaimed)

	if resUnclaimed.Total != 3 || len(resUnclaimed.Items) != 3 {
		t.Fatalf("未认领孤儿服务期望返回 3 个，实际 total=%d, len=%d", resUnclaimed.Total, len(resUnclaimed.Items))
	}
	for _, item := range resUnclaimed.Items {
		if item.IsClaimed {
			t.Errorf("孤儿过滤返回的服务 [%s] 状态期望为未认领，实际已认领", item.ServiceName)
		}
	}
}

// TestGetAgent_ReturnsPreloadedMountedProcesses_AndAntiIDOR 校验 GetAgent 预加载完整性与 Anti-IDOR (M12-P2, ADR-11)
func TestGetAgent_ReturnsPreloadedMountedProcesses_AndAntiIDOR(t *testing.T) {
	r, db := setupRouterAndDB(t)

	adminUser := createTestUserInDB(t, db, "admin_m12", config.AdminRole)
	adminToken := generateTestJWT(t, adminUser)

	userA := createTestUserInDB(t, db, "user_a", "developer")
	tokenA := generateTestJWT(t, userA)

	userB := createTestUserInDB(t, db, "user_b", "developer")
	tokenB := generateTestJWT(t, userB)

	// 1. 创建 Group, ArchProcess, 以及 2 个关联 Module
	group := &models.ArchProcessGroup{
		Code:      "trade_group",
		Name:      "交易核心分组",
		CreatedBy: adminUser.ID,
		UpdatedBy: adminUser.ID,
	}
	if err := db.Create(group).Error; err != nil {
		t.Fatalf("创建 ArchProcessGroup 失败: %v", err)
	}

	archProc := &models.ArchProcess{
		GroupID:           group.ID,
		Code:              "order_core",
		Name:              "订单核心服务",
		DefaultListenPort: 9001,
		DefaultCmdPattern: "./bin/order_core",
		CreatedBy:         adminUser.ID,
		UpdatedBy:         adminUser.ID,
	}
	if err := db.Create(archProc).Error; err != nil {
		t.Fatalf("创建 ArchProcess 失败: %v", err)
	}

	subsys := &models.Subsystem{Name: "订单子系统", Code: "order_subsys", CreatedBy: adminUser.ID, UpdatedBy: adminUser.ID}
	_ = db.Create(subsys).Error

	fg := &models.FunctionalGroup{
		SubsystemID: subsys.ID,
		Code:        "order_fg",
		Name:        "订单功能组",
		CreatedBy:   adminUser.ID,
		UpdatedBy:   adminUser.ID,
	}
	if err := db.Create(fg).Error; err != nil {
		t.Fatalf("创建 FunctionalGroup 失败: %v", err)
	}

	mod1 := &models.Module{
		SubsystemID:   subsys.ID,
		GroupID:       fg.ID,
		ArchProcessID: &archProc.ID,
		Code:          "order_api",
		Name:          "订单API模块",
		CreatedBy:     adminUser.ID,
		UpdatedBy:     adminUser.ID,
	}
	mod2 := &models.Module{
		SubsystemID:   subsys.ID,
		GroupID:       fg.ID,
		ArchProcessID: &archProc.ID,
		Code:          "order_worker",
		Name:          "订单后台处理模块",
		CreatedBy:     adminUser.ID,
		UpdatedBy:     adminUser.ID,
	}
	if err := db.Create(mod1).Error; err != nil {
		t.Fatalf("创建 Module1 失败: %v", err)
	}
	if err := db.Create(mod2).Error; err != nil {
		t.Fatalf("创建 Module2 失败: %v", err)
	}

	// 2. 管理员创建 SHARED Agent 并挂载该 ArchProcess (带覆盖端口 19001)
	sharedAgent := &models.Agent{
		Name:           "shared_agent_m12",
		Endpoint:       "http://127.0.0.1:8092",
		Scope:          models.ScopeShared,
		Status:         models.AgentStatusOnline,
		CreatedBy:      adminUser.ID,
		UpdatedBy:      adminUser.ID,
		MountsRevision: 0,
	}
	if err := db.Create(sharedAgent).Error; err != nil {
		t.Fatalf("创建 SHARED Agent 失败: %v", err)
	}

	overridePort := 19001
	mountReq := handlers.BatchProcessMountReq{
		ExpectedRevision: 0,
		Mounts: []handlers.ProcessMountItem{
			{
				ArchProcessID:      archProc.ID,
				OverrideListenPort: &overridePort,
				ProbeDesiredState:  models.ProbeDesiredStateArmed,
			},
		},
	}
	mountBody, _ := json.Marshal(mountReq)
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/agents/%d/process-mounts", sharedAgent.ID), bytes.NewReader(mountBody))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("批量挂载进程期望 200，实际: %d %s", w.Code, w.Body.String())
	}

	// 3. 用户 A 创建 PRIVATE Agent
	privAgentA := &models.Agent{
		Name:      "private_agent_user_a",
		Endpoint:  "http://127.0.0.1:8093",
		Scope:     models.ScopePrivate,
		OwnerID:   &userA.ID,
		Status:    models.AgentStatusOnline,
		CreatedBy: userA.ID,
		UpdatedBy: userA.ID,
	}
	if err := db.Create(privAgentA).Error; err != nil {
		t.Fatalf("创建 PRIVATE Agent A 失败: %v", err)
	}

	// 4. 断言 GET /api/agents/:id 预加载完整性 (SHARED Agent)
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/agents/%d", sharedAgent.ID), nil)
	req.Header.Set("Authorization", "Bearer "+tokenA) // 普通用户可读 SHARED
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/agents/%d 期望 200，实际: %d %s", sharedAgent.ID, w.Code, w.Body.String())
	}

	var agentDetail struct {
		Agent     models.Agent     `json:"agent"`
		Processes []models.Process `json:"processes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &agentDetail); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	if len(agentDetail.Processes) != 1 {
		t.Fatalf("进程列表期望 1 个，实际返回 %d (空表缺陷验证)", len(agentDetail.Processes))
	}
	procItem := agentDetail.Processes[0]
	if procItem.GroupName != "交易核心分组" {
		t.Errorf("期望 GroupName '交易核心分组'，实际: '%s'", procItem.GroupName)
	}
	if procItem.EffectiveListenPort != 19001 {
		t.Errorf("期望 EffectiveListenPort 19001，实际: %d", procItem.EffectiveListenPort)
	}
	if !procItem.HasOverride {
		t.Errorf("期望 HasOverride=true，实际为 false")
	}
	if procItem.ArchProcess == nil || len(procItem.ArchProcess.Modules) != 2 {
		t.Errorf("期望 Preload ArchProcess.Modules 为 2 个，实际: %+v", procItem.ArchProcess)
	}

	// 5. 断言 Anti-IDOR: 用户 B 访问用户 A 的 PRIVATE Agent 返回 403 Forbidden
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/agents/%d", privAgentA.ID), nil)
	req.Header.Set("Authorization", "Bearer "+tokenB)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("普通用户 B 访问用户 A 的私有 Agent 期望 403 Forbidden，实际: %d %s", w.Code, w.Body.String())
	}
}
