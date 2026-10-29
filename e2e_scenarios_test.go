package traefik_test

import (
	"bytes"
	"context"
	"encoding/binary"
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
	"code-traefik/internal/server/diff"
	"code-traefik/internal/server/hub"
	"code-traefik/internal/server/topology"
	"code-traefik/models"
	"code-traefik/pkg/agentproto"
	"code-traefik/pkg/crosrpc"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func setupE2EEnvironment(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN 未配置，跳过隔离集成测试")
	}

	gin.SetMode(gin.TestMode)

	// 1. 物理隔离测试数据库
	db := testdb.SetupIsolatedDB(t, "traefik_e2e_test")
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}
	if err := db.AutoMigrate(&commonModels.SysAuditLog{}); err != nil {
		t.Fatalf("迁移审计表失败: %v", err)
	}

	// 2. 初始化审计引擎
	_ = commonAudit.Close(context.Background())
	commonAudit.Init(db, commonAudit.InitOptions{
		BatchSize:   1,
		FlushPeriod: 20 * time.Millisecond,
	})

	// 3. 初始化全局配置
	config.AppConfig = config.Config{
		Auth: config.AuthSection{
			JWTSecret: "test-jwt-secret-very-secure-key-123456",
		},
		AgentHub: config.AgentHubSection{
			DevAllowLoopback:     true, // 测试环境允许 127.0.0.1 闭环 Mock
			HeartbeatIntervalSec: 2,
		},
	}

	// 4. 实例化管理服务
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

	// 5. 集中注册 API 路由
	r := gin.New()
	r.Use(gin.Recovery())
	handlers.RegisterAPIRoutes(r)

	return r, db
}

func generateE2EToken(t *testing.T, user *commonModels.User, isAdmin bool) string {
	t.Helper()
	var roles []string
	if len(user.Roles) > 0 {
		_ = json.Unmarshal(user.Roles, &roles)
	}
	token, err := commonAuth.GenerateToken(
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
		t.Fatalf("生成测试 JWT 失败: %v", err)
	}
	return token
}

func createE2EUser(t *testing.T, db *gorm.DB, username string, isAdmin bool) (*commonModels.User, string) {
	t.Helper()
	role := "developer"
	if isAdmin {
		role = config.AdminRole
	}
	user := &commonModels.User{
		Username: username,
		Name:     "Test " + username,
		Email:    username + "@code-traefik.org",
		Password: "hashed_password",
		Roles:    datatypes.JSON([]byte(`["` + role + `"]`)),
		IsActive: true,
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("创建测试用户失败: %v", err)
	}
	token := generateE2EToken(t, user, isAdmin)
	return user, token
}

func buildE2EReqPacket(sno uint64, reqType uint8, svc, op string, payload []byte) []byte {
	sessionHead := make([]byte, 160)
	binary.BigEndian.PutUint16(sessionHead[0:2], 1)
	binary.BigEndian.PutUint16(sessionHead[2:4], 160)
	copy(sessionHead[8:72], []byte(svc))
	copy(sessionHead[72:136], []byte(op))
	sessionHead[136] = reqType

	comm := crosrpc.CommHead{
		TransportProto: crosrpc.TransportTCP,
		SNO:            sno,
		Length:         uint32(160 + len(payload)),
		HeadLen:        160,
		MsgType:        crosrpc.MsgTypeReq,
		IsPacked:       1,
	}
	copy(comm.Magic[:], []byte(crosrpc.MagicPrefix))

	var pkt []byte
	pkt = append(pkt, comm.Marshal()...)
	pkt = append(pkt, sessionHead...)
	pkt = append(pkt, payload...)
	return pkt
}

func buildE2ERspPacket(sno uint64, msgType uint8, msgNo uint16, statusCode uint32, svc, op string, payload []byte) []byte {
	rawRsp := make([]byte, 156)
	binary.BigEndian.PutUint16(rawRsp[0:2], 1)
	binary.BigEndian.PutUint16(rawRsp[2:4], 156)
	binary.BigEndian.PutUint32(rawRsp[4:8], statusCode)
	copy(rawRsp[24:88], []byte(svc))
	copy(rawRsp[88:152], []byte(op))
	binary.BigEndian.PutUint16(rawRsp[152:154], msgNo)

	comm := crosrpc.CommHead{
		TransportProto: crosrpc.TransportTCP,
		SNO:            sno,
		Length:         uint32(156 + len(payload)),
		HeadLen:        156,
		MsgType:        msgType,
		IsPacked:       1,
	}
	copy(comm.Magic[:], []byte(crosrpc.MagicPrefix))

	var pkt []byte
	pkt = append(pkt, comm.Marshal()...)
	pkt = append(pkt, rawRsp...)
	pkt = append(pkt, payload...)
	return pkt
}

/**
 * TestAllScenarios_SC01_To_SC08
 * 按序串联执行 SC-01 到 SC-08 全链路端到端回归测试套件 (AC-M5-03)
 */
func TestAllScenarios_SC01_To_SC08(t *testing.T) {
	router, db := setupE2EEnvironment(t)

	// 创建测试身份：管理员与普通成员
	_, adminToken := createE2EUser(t, db, "sc_admin_runner", true)
	_, devToken := createE2EUser(t, db, "sc_dev_runner", false)
	otherUser, _ := createE2EUser(t, db, "sc_other_runner", false)

	var (
		subsystemID uint64
		groupID     uint64
		moduleID    uint64
		agentID     uint64
		processID   uint64
		sessionID   uint64
	)

	// 启动本地全局 Mock Agent HTTP Server
	mockHandler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/agent/v1/health" || r.URL.Path == "/agent/v1/handshake":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(agentproto.HandshakeResp{
				AgentVersion:    "1.0.0",
				ProtocolVersion: "1.0",
				BootID:          "boot-uuid-e2e",
				StartedAt:       time.Now(),
				KernelVersion:   "5.15.0",
				HasEbpfCap:      true,
				CapDetail:       "ebpf_trace",
			})
		case r.URL.Path == "/agent/v1/monitors/sync":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"diagnostics": []map[string]interface{}{
					{
						"process_id":              1,
						"listen_port":             8080,
						"need_reconnect":          false,
						"established_conns_count": 0,
					},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/stop"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"captured_frames": 2,
				"dropped_frames":  0,
			})
		case r.URL.Path == "/agent/v1/purge":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}

	mockAgentServer := httptest.NewServer(http.HandlerFunc(mockHandler))
	defer mockAgentServer.Close()

	mockPrivateAgentServer := httptest.NewServer(http.HandlerFunc(mockHandler))
	defer mockPrivateAgentServer.Close()

	// -------------------------------------------------------------------------
	// SC-01: 免密探针、未认证拦截与公私域 RBAC 越权
	// -------------------------------------------------------------------------
	t.Run("SC-01: Health, Auth and Public/Private RBAC", func(t *testing.T) {
		// 1. 免密健康检查
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("SC-01 /api/health 期望 200, 实际 %d", w.Code)
		}

		// 2. 未认证拦截
		req = httptest.NewRequest(http.MethodGet, "/api/me", nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("SC-01 未认证 /api/me 期望 401, 实际 %d", w.Code)
		}

		// 3. URL Query 传递 Token 拦截
		req = httptest.NewRequest(http.MethodGet, "/api/me?token=secret123", nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("SC-01 URL Query 传 Token 期望 401 拦截, 实际 %d", w.Code)
		}

		// 4. 普通成员访问受限管理接口拦截 (403)
		body := `{"code":"SC01_SUB","name":"SC01子系统"}`
		req = httptest.NewRequest(http.MethodPost, "/api/arch/subsystems", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+devToken)
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("SC-01 普通成员创建公共子系统期望 403, 实际 %d", w.Code)
		}

		// 5. 管理员创建公共子系统成功 (201)
		req = httptest.NewRequest(http.MethodPost, "/api/arch/subsystems", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("SC-01 管理员创建公共子系统期望 201, 实际 %d: %s", w.Code, w.Body.String())
		}
		var sub models.Subsystem
		_ = json.Unmarshal(w.Body.Bytes(), &sub)
		subsystemID = uint64(sub.ID)
	})

	// -------------------------------------------------------------------------
	// SC-02: 架构建模约束与静态拓扑推导
	// -------------------------------------------------------------------------
	t.Run("SC-02: Architecture Modeling Constraints and Static Topology", func(t *testing.T) {
		// 1. 创建功能组
		groupBody := fmt.Sprintf(`{"subsystem_id":%d,"code":"G_ORDER","name":"订单组"}`, subsystemID)
		req := httptest.NewRequest(http.MethodPost, "/api/arch/functional-groups", bytes.NewBufferString(groupBody))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("SC-02 创建功能组期望 201, 实际 %d", w.Code)
		}
		var grp models.FunctionalGroup
		_ = json.Unmarshal(w.Body.Bytes(), &grp)
		groupID = uint64(grp.ID)

		// 2. 创建功能模块
		modBody := fmt.Sprintf(`{"group_id":%d,"code":"MOD_ORDER_SUT","name":"订单中枢SUT"}`, groupID)
		req = httptest.NewRequest(http.MethodPost, "/api/arch/modules", bytes.NewBufferString(modBody))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("SC-02 创建功能模块期望 201, 实际 %d", w.Code)
		}
		var mod models.Module
		_ = json.Unmarshal(w.Body.Bytes(), &mod)
		moduleID = uint64(mod.ID)

		// 3. 声明 PROVIDE 接口契约
		ifaceBody := `{"relation_type":"PROVIDE","service_name":"dophi.order.v1-OrderService","operation":"CreateOrder","diff_ignore_paths":["header.server_time"]}`
		req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/arch/modules/%d/interfaces", moduleID), bytes.NewBufferString(ifaceBody))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("SC-02 声明 PROVIDE 接口契约期望 201, 实际 %d: %s", w.Code, w.Body.String())
		}

		// 4. 获取静态拓扑
		req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/arch/topology/static?view=LOGICAL&subsystem_id=%d", subsystemID), nil)
		req.Header.Set("Authorization", "Bearer "+devToken)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("SC-02 获取静态拓扑期望 200, 实际 %d", w.Code)
		}
	})

	// -------------------------------------------------------------------------
	// SC-03: Agent 纳管、排他租约、信道与白名单
	// -------------------------------------------------------------------------
	t.Run("SC-03: Agent Management, Leases, Channel and Allowlist", func(t *testing.T) {
		// 1. 注册公共 Agent (必须完成握手)
		agBody := fmt.Sprintf(`{"name":"sc03_agent","endpoint":"%s","scope":"SHARED"}`, mockAgentServer.URL)
		req := httptest.NewRequest(http.MethodPost, "/api/agents", bytes.NewBufferString(agBody))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("SC-03 注册公共 Agent 期望 201, 实际 %d: %s", w.Code, w.Body.String())
		}
		var ag models.Agent
		_ = json.Unmarshal(w.Body.Bytes(), &ag)
		agentID = uint64(ag.ID)

		// 2. 挂载业务进程并绑定模块
		procBody := fmt.Sprintf(`{"agent_id":%d,"name":"order_proc_1","listen_port":36731,"cmd_pattern":"./bin/order","module_ids":[%d]}`, agentID, moduleID)
		req = httptest.NewRequest(http.MethodPost, "/api/processes", bytes.NewBufferString(procBody))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("SC-03 挂载业务进程期望 201, 实际 %d: %s", w.Code, w.Body.String())
		}
		var proc models.Process
		_ = json.Unmarshal(w.Body.Bytes(), &proc)
		processID = uint64(proc.ID)

		// 3. 私有 Agent 防越权 (Anti-IDOR): devUser 注册私有 Agent，otherUser 访问应得 403
		privBody := fmt.Sprintf(`{"name":"priv_agent_dev","endpoint":"%s","scope":"PRIVATE","claim_code":"123"}`, mockPrivateAgentServer.URL)
		req = httptest.NewRequest(http.MethodPost, "/api/agents", bytes.NewBufferString(privBody))
		req.Header.Set("Authorization", "Bearer "+devToken)
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("SC-03 注册私有 Agent 期望 201, 实际 %d", w.Code)
		}
		var privAg models.Agent
		_ = json.Unmarshal(w.Body.Bytes(), &privAg)

		otherToken := generateE2EToken(t, otherUser, false)
		req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/agents/%d", privAg.ID), nil)
		req.Header.Set("Authorization", "Bearer "+otherToken)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("SC-03 跨租户越权访问私有 Agent 期望 403, 实际 %d", w.Code)
		}
	})

	// -------------------------------------------------------------------------
	// SC-04: CROS RPC 分帧与解码
	// -------------------------------------------------------------------------
	t.Run("SC-04: CrosRPC Framing and Decoder Fallback", func(t *testing.T) {
		validReqPacket := buildE2EReqPacket(1001, crosrpc.ReqTypeSync, "dophi.order.v1-OrderService", "CreateOrder", []byte("order_data"))

		// 混入脏字节后验证 Framer 能够恢复同步
		dirtyStream := append([]byte("junk_garbage_bytes"), validReqPacket...)
		framer := crosrpc.NewStreamFramer(4 * 1024 * 1024)
		frames, err := framer.Feed(dirtyStream)
		if err != nil {
			t.Fatalf("SC-04 Framer Feed 失败: %v", err)
		}
		if len(frames) != 1 {
			t.Fatalf("SC-04 Framer 脏字节重同步期望 1 帧, 实际 %d", len(frames))
		}
	})

	// -------------------------------------------------------------------------
	// SC-05: 单 Agent 多进程协同录制与双模物理分区落盘
	// -------------------------------------------------------------------------
	t.Run("SC-05: Collaborative Recording Session and Dual-Mode Storage", func(t *testing.T) {
		// 1. 发起协同录制任务
		startBody := fmt.Sprintf(`{"name":"SC05协同录制","scope":"SHARED","target_process_ids":[%d],"max_duration_sec":300}`, processID)
		req := httptest.NewRequest(http.MethodPost, "/api/recording-sessions", bytes.NewBufferString(startBody))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("SC-05 发起录制期望 201, 实际 %d: %s", w.Code, w.Body.String())
		}
		var sess models.RecordingSession
		_ = json.Unmarshal(w.Body.Bytes(), &sess)
		sessionID = uint64(sess.ID)

		// 2. 验证排他租约互斥 (409 Conflict)
		req = httptest.NewRequest(http.MethodPost, "/api/recording-sessions", bytes.NewBufferString(startBody))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusConflict {
			t.Fatalf("SC-05 争用已被租用的 Agent 期望 409 Conflict, 实际 %d", w.Code)
		}

		// 3. 向 ExchangeAssembler 投递往返配对流量
		glbAssembler := handlers.GetGlobalOrchestrator().Assembler()
		reqPacket := buildE2EReqPacket(1001, crosrpc.ReqTypeSync, "dophi.order.v1-OrderService", "CreateOrder", []byte("req_payload"))
		rspPacket := buildE2ERspPacket(1001, crosrpc.MsgTypeRsp, 1, 0, "dophi.order.v1-OrderService", "CreateOrder", []byte("rsp_payload"))

		makeEnv := func(dir uint8, payloadLen int) agentproto.EnvelopeHeader {
			return agentproto.EnvelopeHeader{
				Magic:           agentproto.EnvelopeMagic,
				ProtoVer:        1,
				EventType:       agentproto.EventTrafficFrame,
				SessionOrTaskID: sessionID,
				ProcessID:       processID,
				CapturedPID:     10001,
				Direction:       dir,
				SrcIPv4:         [4]byte{192, 168, 56, 100},
				DstIPv4:         [4]byte{192, 168, 56, 18},
				SrcPort:         45678,
				DstPort:         36731,
				ConnSeq:         1,
				PayloadLen:      uint32(payloadLen),
				CapturedAtNs:    time.Now().UnixNano(),
			}
		}

		glbAssembler.Feed(makeEnv(agentproto.DirectionIngress, len(reqPacket)), reqPacket)
		glbAssembler.Feed(makeEnv(agentproto.DirectionIngress, len(rspPacket)), rspPacket)

		// 强制刷新并等待落盘
		glbAssembler.Flush()
		time.Sleep(100 * time.Millisecond)

		// 4. 检索流量主表
		req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/traffic-records?session_id=%d", sessionID), nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("SC-05 检索流量记录期望 200, 实际 %d", w.Code)
		}

		var recordResp struct {
			Items []models.TrafficRecord `json:"items"`
			Total int                    `json:"total"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &recordResp)
		if recordResp.Total < 1 {
			t.Fatalf("SC-05 期望至少落盘 1 条往返记录, 实际 %d", recordResp.Total)
		}

		// 5. 停止录制并释放租约
		req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/recording-sessions/%d/stop", sessionID), nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("SC-05 停止录制期望 200, 实际 %d: %s", w.Code, w.Body.String())
		}
	})

	// -------------------------------------------------------------------------
	// SC-06: 架构一致性验证
	// -------------------------------------------------------------------------
	t.Run("SC-06: Architecture Consistency Verification", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/recording-sessions/%d/consistency-report", sessionID), nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("SC-06 一致性报告期望 200, 实际 %d", w.Code)
		}
		var rep topology.ConsistencyReportDTO
		_ = json.Unmarshal(w.Body.Bytes(), &rep)
		if rep.SessionID != sessionID {
			t.Fatalf("SC-06 报告 SessionID 不匹配: 期望 %d, 实际 %d", sessionID, rep.SessionID)
		}
	})

	// -------------------------------------------------------------------------
	// SC-07: 跨 Agent 同构进程零 .pb 重放与头部自愈
	// -------------------------------------------------------------------------
	t.Run("SC-07: Cross-Agent Isomorphic Replay and Header Healing PASS", func(t *testing.T) {
		baselineRsp := buildE2ERspPacket(1001, crosrpc.MsgTypeRsp, 1, 0, "dophi.order.v1-OrderService", "CreateOrder", []byte("identical_body"))
		actualRsp := buildE2ERspPacket(9999, crosrpc.MsgTypeRsp, 1, 0, "dophi.order.v1-OrderService", "CreateOrder", []byte("identical_body"))

		diffResult, payloadIdentical, err := diff.DiffLayer1Header(baselineRsp, actualRsp)
		if err != nil || diffResult.Status != models.ReplayResultPass || !payloadIdentical {
			t.Fatalf("SC-07 运行态字段豁免自愈断言失败: res=%+v, err=%v", diffResult, err)
		}
	})

	// -------------------------------------------------------------------------
	// SC-08: 业务突变与 Mock 未命中检出
	// -------------------------------------------------------------------------
	t.Run("SC-08: Mutation Diff and MockMiss Detection", func(t *testing.T) {
		// 1. 业务状态码突变 (0 -> 500)
		baselineRsp := buildE2ERspPacket(1001, crosrpc.MsgTypeRsp, 1, 0, "dophi.order.v1-OrderService", "CreateOrder", []byte("body"))
		mutatedRsp := buildE2ERspPacket(1001, crosrpc.MsgTypeRsp, 1, 500, "dophi.order.v1-OrderService", "CreateOrder", []byte("body"))

		diffResult, _, _ := diff.DiffLayer1Header(baselineRsp, mutatedRsp)
		if diffResult.Status != models.ReplayResultFailed || !diffResult.StatusCodeDiff {
			t.Fatalf("SC-08 状态码突变期望断言 FAILED, 实际 res=%+v", diffResult)
		}

		// 2. 全序状态聚合优先级定律验证: ERROR > TIMEOUT > MOCK_MISS > FAILED > PASS
		aggStatus := diff.AggregateReplayStatus([]string{
			models.ReplayResultPass,
			models.ReplayResultFailed,
			models.ReplayResultMockMiss,
		})
		if aggStatus != models.ReplayResultMockMiss {
			t.Fatalf("SC-08 全序状态聚合期望 MOCK_MISS, 实际得到: %s", aggStatus)
		}
	})
}
