package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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

		protected.GET("/arch/modules/:id/interfaces", handlers.ListModuleInterfaces)
		protected.POST("/arch/modules/:id/interfaces", requireAdmin, handlers.CreateModuleInterface)
		protected.PUT("/arch/module-interfaces/:ifaceId", requireAdmin, handlers.UpdateModuleInterface)
		protected.DELETE("/arch/module-interfaces/:ifaceId", requireAdmin, handlers.DeleteModuleInterface)

		protected.GET("/arch/topology/static", handlers.GetStaticTopology)

		// 描述集
		protected.GET("/proto-schemas", handlers.ListProtoSchemas)
		protected.POST("/proto-schemas", requireAdmin, handlers.UploadProtoSchema)
		protected.POST("/proto-schemas/:id/activate", requireAdmin, handlers.ActivateProtoSchema)
		protected.DELETE("/proto-schemas/:id", requireAdmin, handlers.DeleteProtoSchema)
		protected.GET("/proto-operations", handlers.ListProtoOperations)

		// Agent
		protected.GET("/agents", handlers.ListAgents)
		protected.GET("/agents/:id", handlers.GetAgent)
		protected.POST("/agents", handlers.RegisterAgent)
		protected.PUT("/agents/:id", handlers.UpdateAgent)
		protected.DELETE("/agents/:id", handlers.DeleteAgent)
		protected.POST("/agents/:id/handshake", handlers.HandshakeAgent)
		protected.GET("/agents/:id/discover-processes", handlers.DiscoverAgentProcesses)
		protected.POST("/agents/:id/lease/force-unlock", requireAdmin, handlers.ForceUnlockAgentLease)

		// 进程
		protected.GET("/processes", handlers.ListProcesses)
		protected.GET("/processes/:id", handlers.GetProcess)
		protected.POST("/processes", handlers.CreateProcess)
		protected.PUT("/processes/:id", handlers.UpdateProcess)
		protected.DELETE("/processes/:id", handlers.DeleteProcess)
		protected.GET("/processes/:id/downstreams", handlers.GetProcessDownstreams)

		// 8. 协同录制与架构拓扑域 (F-05, F-11, F-16)
		protected.GET("/recording-sessions", handlers.ListRecordingSessions)
		protected.GET("/recording-sessions/:id", handlers.GetRecordingSession)
		protected.POST("/recording-sessions", handlers.StartRecordingSession)
		protected.POST("/recording-sessions/:id/stop", handlers.StopRecordingSession)
		protected.DELETE("/recording-sessions/:id/processes/:processId", handlers.RemoveProcessFromSession)
		protected.PATCH("/recording-sessions/:id/publish", handlers.PublishRecordingSession)
		protected.DELETE("/recording-sessions/:id", handlers.DeleteRecordingSession)
		protected.GET("/recording-sessions/:id/consistency-report", handlers.GetConsistencyReport)

		// 9. 流量检索与解剖域 (F-03, F-11)
		protected.GET("/traffic-records", handlers.ListTrafficRecords)
		protected.GET("/traffic-records/:id/anatomy", handlers.GetTrafficRecordAnatomy)
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

	// 4. 为 MOD_RISK 声明 PROVIDE 接口 dophi.risk.v1-RiskService:VerifyRisk
	ifaceBody, _ := json.Marshal(map[string]interface{}{
		"relation_type": "PROVIDE",
		"service_name":  "dophi.risk.v1-RiskService",
		"operation":     "VerifyRisk",
	})
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/arch/modules/%d/interfaces", mod2.ID), bytes.NewReader(ifaceBody))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("声明 PROVIDE 接口失败: %d", w.Code)
	}

	// 5. 尝试在 MOD_PAY 上声明相同的 PROVIDE 接口 -> 409 Conflict (A-05 供给全局唯一)
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/arch/modules/%d/interfaces", mod1.ID), bytes.NewReader(ifaceBody))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("期望声明冲突 PROVIDE 返回 409，实际: %d", w.Code)
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

	// 验证 SHARED 进程同步回写 Module.ProcessID
	var refModA models.Module
	_ = db.First(&refModA, modA.ID).Error
	if refModA.ProcessID == nil || *refModA.ProcessID != proc1.ID {
		t.Fatalf("模块 A 的 ProcessID 未同步为 Proc_1: %+v", refModA)
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
