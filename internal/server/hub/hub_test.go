package hub_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"code-traefik/config"
	"code-traefik/internal/server/hub"
	"code-traefik/models"
	"code-traefik/pkg/agentproto"

	commonModels "code-common/backend/models"
	"code-common/backend/testdb"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN 未配置，跳过 Hub 隔离数据库集成测试")
	}
	db := testdb.SetupIsolatedDB(t, "traefik_hub_test")
	if err := db.AutoMigrate(
		&models.Agent{},
		&models.Process{},
		&models.ProcessModule{},
		&models.RecordingSession{},
		&models.ReplayTask{},
	); err != nil {
		t.Fatalf("自动迁移失败: %v", err)
	}
	return db
}

// createTestUser 辅助函数：在公共 sys_users 表创建测试用户
func createTestUser(t *testing.T, db *gorm.DB, username, role string) *commonModels.User {
	t.Helper()
	user := &commonModels.User{
		Username: username,
		Name:     username,
		Email:    username + "@example.com",
		Password: "hashed_password",
		Roles:    datatypes.JSON([]byte(`["` + role + `"]`)),
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("创建测试用户失败: %v", err)
	}
	return user
}

func TestAgentSSRFGuardAndClaimCode(t *testing.T) {
	// 1. SSRF 纯函数校验
	t.Run("SSRFEndpointValidation", func(t *testing.T) {
		// 回环地址：非 Dev 模式严格拒绝
		_, err := hub.ValidateAgentEndpoint("http://127.0.0.1:9091", false)
		if err == nil {
			t.Fatalf("期望非 Dev 模式拒绝 127.0.0.1 回环地址，但校验通过")
		}

		// 回环地址：Dev 模式允许
		ep, err := hub.ValidateAgentEndpoint("http://127.0.0.1:9091", true)
		if err != nil || !strings.Contains(ep, "127.0.0.1:9091") {
			t.Fatalf("期望 Dev 模式允许 127.0.0.1，得到: %v, ep: %s", err, ep)
		}

		// 云元数据链路地址 (169.254.169.254) 严格拒绝
		_, err = hub.ValidateAgentEndpoint("http://169.254.169.254:80", true)
		if err == nil {
			t.Fatalf("期望拒绝 169.254.169.254 元数据地址")
		}

		// 阿里云内部元数据地址 (100.100.100.200) 严格拒绝
		_, err = hub.ValidateAgentEndpoint("http://100.100.100.200:80", true)
		if err == nil {
			t.Fatalf("期望拒绝 100.100.100.200 阿里云元数据地址")
		}

		// 组播与广播地址严格拒绝
		for _, invalidIP := range []string{"224.0.0.1:9090", "255.255.255.255:9090", "0.0.0.0:9090"} {
			_, err = hub.ValidateAgentEndpoint("http://"+invalidIP, true)
			if err == nil {
				t.Fatalf("期望拒绝组播/广播/零地址 %s", invalidIP)
			}
		}

		// 特权端口 (< 1024) 严格拒绝
		_, err = hub.ValidateAgentEndpoint("http://192.168.1.100:80", true)
		if err == nil {
			t.Fatalf("期望拒绝特权端口 80")
		}

		// 合法局域网大端口允许
		validEp, err := hub.ValidateAgentEndpoint("192.168.1.100:9091", false)
		if err != nil || validEp != "http://192.168.1.100:9091" {
			t.Fatalf("合法内网地址校验失败: %v, ep: %s", err, validEp)
		}
	})

	// 2. Mock Agent 握手探活、认领码与公私域权限
	t.Run("MockAgentHandshakeAndClaimCodeRBAC", func(t *testing.T) {
		db := setupTestDB(t)
		admin := createTestUser(t, db, "hub_admin", "traefik_admin")
		userA := createTestUser(t, db, "hub_user_a", "operator")
		userB := createTestUser(t, db, "hub_user_b", "operator")

		expectedClaimCode := "secret-claim-code-123"
		currentBootID := "boot-uuid-001"

		// 启动 Mock Agent (支持 /agent/v1/health)
		mockAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/agent/v1/health" {
				claimCodeHeader := r.Header.Get(agentproto.HeaderClaimCode)
				if claimCodeHeader != "" && claimCodeHeader != expectedClaimCode {
					w.WriteHeader(http.StatusForbidden)
					_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid claim code"})
					return
				}
				resp := agentproto.HandshakeResp{
					AgentVersion:    "1.0.0",
					ProtocolVersion: "1.0",
					BootID:          currentBootID,
					KernelVersion:   "5.15.0-generic",
					HasEbpfCap:      true,
					CapDetail:       "ebpf_trace,ull_sno_replay",
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer mockAgent.Close()

		mockPrivateAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/agent/v1/health" {
				claimCodeHeader := r.Header.Get(agentproto.HeaderClaimCode)
				if claimCodeHeader != expectedClaimCode {
					w.WriteHeader(http.StatusForbidden)
					_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid claim code"})
					return
				}
				resp := agentproto.HandshakeResp{
					AgentVersion:    "1.0.0",
					ProtocolVersion: "1.0",
					BootID:          currentBootID,
					KernelVersion:   "5.15.0-generic",
					HasEbpfCap:      true,
					CapDetail:       "ebpf_trace,ull_sno_replay",
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer mockPrivateAgent.Close()

		cfg := &config.Config{
			AgentHub: config.AgentHubSection{
				DevAllowLoopback:     true, // 测试允许 loopback
				HeartbeatIntervalSec: 2,
			},
		}
		mgr := hub.NewManager(db, cfg)

		// 场景 A: 普通用户注册 SHARED Agent 必须返回 403 Forbidden
		_, err := mgr.RegisterAgent(context.Background(), hub.AgentRegisterReq{
			Name:     "Shared-Agent-1",
			Scope:    models.ScopeShared,
			Endpoint: mockAgent.URL,
		}, userA.ID, false)
		if err == nil {
			t.Fatalf("普通用户注册 SHARED Agent 必须被拦截并返回 403")
		}

		// 场景 B: 管理员注册 SHARED Agent 成功
		sharedAgent, err := mgr.RegisterAgent(context.Background(), hub.AgentRegisterReq{
			Name:     "Shared-Agent-1",
			Scope:    models.ScopeShared,
			Endpoint: mockAgent.URL,
		}, admin.ID, true)
		if err != nil {
			t.Fatalf("管理员注册 SHARED Agent 失败: %v", err)
		}
		if sharedAgent.Scope != models.ScopeShared || sharedAgent.Status != models.AgentStatusOnline {
			t.Fatalf("SHARED Agent 状态不符合预期: %+v", sharedAgent)
		}

		// 场景 C: 普通用户注册 PRIVATE Agent，但 ClaimCode 错误 -> 握手失败被拒 (403)
		_, err = mgr.RegisterAgent(context.Background(), hub.AgentRegisterReq{
			Name:      "UserA-Private-Agent",
			Scope:     models.ScopePrivate,
			Endpoint:  mockPrivateAgent.URL,
			ClaimCode: "wrong-code",
		}, userA.ID, false)
		if err == nil {
			t.Fatalf("错误的认领码必须被 Agent 拒绝并阻断注册")
		}

		// 场景 D: 普通用户注册 PRIVATE Agent，携带正确 ClaimCode -> 注册成功
		privateAgent, err := mgr.RegisterAgent(context.Background(), hub.AgentRegisterReq{
			Name:      "UserA-Private-Agent",
			Scope:     models.ScopePrivate,
			Endpoint:  mockPrivateAgent.URL,
			ClaimCode: expectedClaimCode,
		}, userA.ID, false)
		if err != nil {
			t.Fatalf("普通用户 A 注册有效 PRIVATE Agent 失败: %v", err)
		}
		if privateAgent.OwnerID == nil || *privateAgent.OwnerID != userA.ID {
			t.Fatalf("PRIVATE Agent OwnerID 未正确绑定用户 A: %+v", privateAgent)
		}

		// 场景 E: 普通用户 B 横向越权查看/更新用户 A 的 PRIVATE Agent -> 403 Forbidden
		_, err = mgr.GetAgent(context.Background(), privateAgent.ID, userB.ID, false)
		if err == nil {
			t.Fatalf("普通用户 B 查看用户 A 的 PRIVATE Agent 必须被拦截 (403)")
		}

		// 场景 F: 管理员可以查看用户 A 的 PRIVATE Agent (运维巡检)
		adminView, err := mgr.GetAgent(context.Background(), privateAgent.ID, admin.ID, true)
		if err != nil || adminView.ID != privateAgent.ID {
			t.Fatalf("管理员查看 PRIVATE Agent 失败: %v", err)
		}
	})
}

func TestAcquireAgentLeasesMutexAndHeartbeatReconcile(t *testing.T) {
	db := setupTestDB(t)
	userA := createTestUser(t, db, "lease_user_a", "operator")
	userB := createTestUser(t, db, "lease_user_b", "operator")

	var bootIDAtomic atomic.Value
	bootIDAtomic.Store("boot-uuid-original")
	var isOffline atomic.Bool
	isOffline.Store(false)

	mockAgent1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isOffline.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		resp := agentproto.HandshakeResp{
			AgentVersion:    "1.0.0",
			ProtocolVersion: "1.0",
			BootID:          bootIDAtomic.Load().(string),
			KernelVersion:   "5.15.0-generic",
			HasEbpfCap:      true,
			CapDetail:       "ebpf_trace",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer mockAgent1.Close()

	mockAgent2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isOffline.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		resp := agentproto.HandshakeResp{
			AgentVersion:    "1.0.0",
			ProtocolVersion: "1.0",
			BootID:          bootIDAtomic.Load().(string),
			KernelVersion:   "5.15.0-generic",
			HasEbpfCap:      true,
			CapDetail:       "ebpf_trace",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer mockAgent2.Close()

	agent1 := &models.Agent{
		Name:     "Lease-Test-Agent-1",
		Scope:    models.ScopeShared,
		Endpoint: mockAgent1.URL,
		Status:   models.AgentStatusOnline,
		BootID:   "boot-uuid-original",
	}
	agent2 := &models.Agent{
		Name:     "Lease-Test-Agent-2",
		Scope:    models.ScopeShared,
		Endpoint: mockAgent2.URL,
		Status:   models.AgentStatusOnline,
		BootID:   "boot-uuid-original",
	}
	if err := db.Create(agent1).Error; err != nil {
		t.Fatalf("创建 Agent 1 失败: %v", err)
	}
	if err := db.Create(agent2).Error; err != nil {
		t.Fatalf("创建 Agent 2 失败: %v", err)
	}

	// 1. 并发互斥申请测试 (规避死锁，恰有 1 个成功，另 1 个返回 409 Conflict)
	t.Run("ConcurrentCrossAcquireMutex", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var wg sync.WaitGroup
		wg.Add(2)

		var userASuccess, userBSuccess bool
		var userAErr, userBErr error

		// 用户 A 申请 [agent1, agent2]
		go func() {
			defer wg.Done()
			err := hub.AcquireAgentLeases(ctx, db, []uint64{agent1.ID, agent2.ID}, models.AgentLeaseRecording, 1001, userA.ID, 30*time.Second)
			if err == nil {
				userASuccess = true
			} else {
				userAErr = err
			}
		}()

		// 用户 B 反序申请 [agent2, agent1] (考验内部排序升序加锁防死锁)
		go func() {
			defer wg.Done()
			err := hub.AcquireAgentLeases(ctx, db, []uint64{agent2.ID, agent1.ID}, models.AgentLeaseReplay, 2001, userB.ID, 30*time.Second)
			if err == nil {
				userBSuccess = true
			} else {
				userBErr = err
			}
		}()

		wg.Wait()

		// 必须恰好 1 成功 1 失败
		if userASuccess == userBSuccess {
			t.Fatalf("并发租约申请互斥失败: userA=%v (err=%v), userB=%v (err=%v)", userASuccess, userAErr, userBSuccess, userBErr)
		}

		var conflictErr *hub.LeaseConflictError
		var failedErr error
		if !userASuccess {
			failedErr = userAErr
		} else {
			failedErr = userBErr
		}

		if !strings.Contains(failedErr.Error(), "already leased") && !errors.As(failedErr, &conflictErr) {
			t.Fatalf("期望得到租约冲突错误 (409 Conflict)，实际得到: %v", failedErr)
		}

		// 释放持有的租约
		if userASuccess {
			if err := hub.ReleaseAgentLeases(context.Background(), db, []uint64{agent1.ID, agent2.ID}, models.AgentLeaseRecording, 1001); err != nil {
				t.Fatalf("释放租约失败: %v", err)
			}
		} else {
			if err := hub.ReleaseAgentLeases(context.Background(), db, []uint64{agent1.ID, agent2.ID}, models.AgentLeaseReplay, 2001); err != nil {
				t.Fatalf("释放租约失败: %v", err)
			}
		}
	})

	// 2. 心跳失联与重启自愈 (Reconciliation) 测试
	t.Run("HeartbeatOfflineAndRebootReconcile", func(t *testing.T) {
		// 先给 agent1 赋予一个活跃录制租约，并创建对应的 RecordingSession 模拟正在录制
		session := &models.RecordingSession{
			SessionNo:        "REC-TEST-001",
			Name:             "Active-Recording-Session",
			Scope:            models.ScopeShared,
			OwnerID:          userA.ID,
			Status:           models.SessionStatusRecording,
			TargetProcessIDs: []byte("[]"),
			CreatedBy:        userA.ID,
		}
		if err := db.Create(session).Error; err != nil {
			t.Fatalf("创建录制任务失败: %v", err)
		}

		if err := hub.AcquireAgentLeases(context.Background(), db, []uint64{agent1.ID}, models.AgentLeaseRecording, session.ID, userA.ID, 30*time.Second); err != nil {
			t.Fatalf("获取录制租约失败: %v", err)
		}

		cfg := &config.Config{
			AgentHub: config.AgentHubSection{
				DevAllowLoopback:     true,
				HeartbeatIntervalSec: 1,
			},
		}
		mgr := hub.NewManager(db, cfg)

		// 模拟 Agent 突然下线
		isOffline.Store(true)

		// 探活连续 3 次失败
		for i := 0; i < hub.MaxMissedHeartbeats; i++ {
			_ = mgr.ProbeAgentOnce(context.Background(), agent1.ID)
		}

		// 校验 Agent 状态应变为 OFFLINE，租约自动清空
		var freshAgent models.Agent
		if err := db.First(&freshAgent, agent1.ID).Error; err != nil {
			t.Fatalf("查询 Agent 失败: %v", err)
		}
		if freshAgent.Status != models.AgentStatusOffline {
			t.Fatalf("3 次失联后 Agent 状态未更新为 OFFLINE，实际: %s", freshAgent.Status)
		}
		if freshAgent.LeaseType != models.AgentLeaseNone {
			t.Fatalf("Agent 下线后排他租约未清空，实际: %s", freshAgent.LeaseType)
		}

		// 校验录制任务应自动被置为 FAILED 并记录失联原因
		var freshSession models.RecordingSession
		if err := db.First(&freshSession, session.ID).Error; err != nil {
			t.Fatalf("查询 Session 失败: %v", err)
		}
		if freshSession.Status != models.SessionStatusFailed {
			t.Fatalf("Agent 下线后关联的录制任务未置为 FAILED，实际: %s", freshSession.Status)
		}

		// 模拟 Agent 重启上线，但换了新的 boot_id
		isOffline.Store(false)
		bootIDAtomic.Store("boot-uuid-rebooted-999")

		// 重新给 Agent 绑定一个重放任务与租约
		replayTask := &models.ReplayTask{
			TaskNo:            "REP-TEST-001",
			Name:              "Active-Replay-Task",
			OwnerID:           userA.ID,
			SessionID:         session.ID,
			AgentID:           agent1.ID,
			LifecycleState:    "RUNNING",
			SelectedRecordIDs: []byte("[]"),
		}
		if err := db.Create(replayTask).Error; err != nil {
			t.Fatalf("创建重放任务失败: %v", err)
		}
		// 将 Agent 强制置为 ONLINE 并绑定新租约
		now := time.Now()
		exp := now.Add(30 * time.Second)
		_ = db.Model(&models.Agent{}).Where("id = ?", agent1.ID).Updates(map[string]interface{}{
			"status":           models.AgentStatusOnline,
			"lease_type":       models.AgentLeaseReplay,
			"lease_ref_id":     replayTask.ID,
			"lease_holder_id":  userA.ID,
			"lease_expires_at": &exp,
			"boot_id":          "boot-uuid-original", // 此时 DB 仍是旧 boot_id
		}).Error

		// 探活单次触发，检测到 BootID 变更
		if err := mgr.ProbeAgentOnce(context.Background(), agent1.ID); err != nil {
			t.Fatalf("探活单次执行失败: %v", err)
		}

		// 校验 Agent 的 boot_id 已更新，且租约被清空，重放任务被置为 FAILED
		if err := db.First(&freshAgent, agent1.ID).Error; err != nil {
			t.Fatalf("查询 Agent 失败: %v", err)
		}
		if freshAgent.BootID != "boot-uuid-rebooted-999" {
			t.Fatalf("Agent boot_id 未对齐，实际: %s", freshAgent.BootID)
		}
		if freshAgent.LeaseType != models.AgentLeaseNone {
			t.Fatalf("Agent 重启后悬挂租约未释放，实际: %s", freshAgent.LeaseType)
		}

		var freshReplayTask models.ReplayTask
		if err := db.First(&freshReplayTask, replayTask.ID).Error; err != nil {
			t.Fatalf("查询重放任务失败: %v", err)
		}
		if freshReplayTask.LifecycleState != models.SessionStatusFailed {
			t.Fatalf("Agent 重启后关联重放任务未置为 FAILED，实际: %s", freshReplayTask.LifecycleState)
		}
	})
}
