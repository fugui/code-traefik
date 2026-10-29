package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"code-traefik/pkg/agentproto"
	"code-traefik/pkg/ebpf"
	"code-traefik/pkg/replay"

	"github.com/gorilla/websocket"
)

// AgentServerConfig Agent 服务器启动参数
type AgentServerConfig struct {
	ListenAddr            string
	AllowIPs              []string
	MaxSessionBufferBytes int64
	GracePeriod           time.Duration
	ClaimCode             string
	ClaimCodeFile         string
	AgentVersion          string
	ProcRoot              string
	CapabilityChecker     ebpf.CapabilityChecker
}

// AgentServer code-traefik-agent HTTP REST + WebSocket 二进制数据面服务端
type AgentServer struct {
	mu              sync.Mutex
	cfg             AgentServerConfig
	allowPrefixes   []netip.Prefix
	claimCode       string
	bootID          string
	startedAt       time.Time
	buffer          *BoundedSessionBuffer
	ebpfMgr         *ebpf.Manager
	replayExec      *replay.ReplayExecutor
	procScanner     *ebpf.ProcScanner
	activeLeaseType string
	activeRefID     uint64
	lastContactAt   time.Time
	wsActive        int32
	httpServer      *http.Server
	listener        net.Listener
	watchdogStop    chan struct{}
	upgrader        websocket.Upgrader
}

// NewAgentServer 构造 Agent 服务端实例
func NewAgentServer(cfg AgentServerConfig) (*AgentServer, error) {
	if cfg.AgentVersion == "" {
		cfg.AgentVersion = "1.0.0"
	}
	if cfg.GracePeriod <= 0 {
		cfg.GracePeriod = 5 * time.Minute
	}
	if cfg.MaxSessionBufferBytes <= 0 {
		cfg.MaxSessionBufferBytes = DefaultMaxSessionBufferBytes
	}

	// 1. 生成一次性认领码 (16 字节十六进制)
	claimCode := cfg.ClaimCode
	if claimCode == "" {
		randomBytes := make([]byte, 16)
		if _, err := rand.Read(randomBytes); err != nil {
			return nil, fmt.Errorf("生成 ClaimCode 失败: %w", err)
		}
		claimCode = hex.EncodeToString(randomBytes)
	}

	// 若配置了认领码输出路径，写入受保护文件
	if cfg.ClaimCodeFile != "" {
		_ = os.MkdirAll(filepath.Dir(cfg.ClaimCodeFile), 0755)
		_ = os.WriteFile(cfg.ClaimCodeFile, []byte(claimCode), 0600)
	}

	// 2. 生成实例重启 BootID
	bootRandom := make([]byte, 16)
	_, _ = rand.Read(bootRandom)
	bootID := hex.EncodeToString(bootRandom)

	// 3. 解析 IP 白名单
	var prefixes []netip.Prefix
	for _, ipStr := range cfg.AllowIPs {
		ipStr = strings.TrimSpace(ipStr)
		if ipStr == "" {
			continue
		}
		if !strings.Contains(ipStr, "/") {
			// 单 IP
			addr, err := netip.ParseAddr(ipStr)
			if err == nil {
				if addr.Is4() {
					prefixes = append(prefixes, netip.PrefixFrom(addr, 32))
				} else {
					prefixes = append(prefixes, netip.PrefixFrom(addr, 128))
				}
			}
		} else {
			// CIDR 网段
			pfx, err := netip.ParsePrefix(ipStr)
			if err == nil {
				prefixes = append(prefixes, pfx)
			}
		}
	}

	buf := NewBoundedSessionBuffer(cfg.MaxSessionBufferBytes)

	s := &AgentServer{
		cfg:           cfg,
		allowPrefixes: prefixes,
		claimCode:     claimCode,
		bootID:        bootID,
		startedAt:     time.Now(),
		buffer:        buf,
		procScanner:   ebpf.NewProcScanner(cfg.ProcRoot),
		replayExec:    replay.NewReplayExecutor(),
		lastContactAt: time.Now(),
		watchdogStop:  make(chan struct{}),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}

	// 创建 eBPF 管理器
	ebpfConfig := ebpf.ManagerConfig{
		ProcRoot:          cfg.ProcRoot,
		MaxFramerBudget:   int(cfg.MaxSessionBufferBytes),
		CapabilityChecker: cfg.CapabilityChecker,
		FrameSink: func(env []byte) {
			s.buffer.Push(env)
		},
		LifecycleEventSink: func(eventType uint8, eventData []byte) {
			// 封装为 WebSocket 信封帧直接入缓冲
			env := agentproto.EnvelopeHeader{
				Magic:        agentproto.EnvelopeMagic,
				ProtoVer:     agentproto.EnvelopeProtoVer,
				EventType:    eventType,
				CapturedAtNs: time.Now().UnixNano(),
			}
			s.buffer.Push(agentproto.MarshalEnvelope(env, eventData))
		},
	}
	s.ebpfMgr = ebpf.NewManager(ebpfConfig)

	return s, nil
}

// ClaimCode 获取当前认领码
func (s *AgentServer) ClaimCode() string {
	return s.claimCode
}

// BootID 获取当前实例 BootID
func (s *AgentServer) BootID() string {
	return s.bootID
}

// Buffer 获取会话缓冲
func (s *AgentServer) Buffer() *BoundedSessionBuffer {
	return s.buffer
}

// EbpfManager 获取 eBPF 管理器
func (s *AgentServer) EbpfManager() *ebpf.Manager {
	return s.ebpfMgr
}

// ActiveLease 获取当前持有的租约
func (s *AgentServer) ActiveLease() (leaseType string, refID uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activeLeaseType, s.activeRefID
}

// SetGracePeriod 用于测试动态修改看门狗超时时间
func (s *AgentServer) SetGracePeriod(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.GracePeriod = d
}

// Start 启动 HTTP + WebSocket 监听服务
func (s *AgentServer) Start() error {
	addr := s.cfg.ListenAddr
	if addr == "" {
		addr = "127.0.0.1:0"
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("监听 %s 失败: %w", addr, err)
	}
	s.listener = ln

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	// 注入 IP 白名单拦截
	handler := s.ipAllowlistMiddleware(mux)

	s.httpServer = &http.Server{
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// 启动失联自洁看门狗
	go s.watchdogLoop()

	go func() {
		_ = s.httpServer.Serve(ln)
	}()

	return nil
}

// Addr 返回实际监听的地址
func (s *AgentServer) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.listener.Addr()
	}
	return nil
}

// Shutdown 优雅关闭服务
func (s *AgentServer) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	select {
	case <-s.watchdogStop:
	default:
		close(s.watchdogStop)
	}
	s.mu.Unlock()

	s.PurgeAll()
	s.buffer.Close()

	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// PurgeAll 清空所有监控、取消重放并释放本地排他租约
func (s *AgentServer) PurgeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ebpfMgr != nil {
		s.ebpfMgr.PurgeAllMonitors()
	}
	if s.replayExec != nil && s.activeRefID > 0 {
		s.replayExec.Cancel(s.activeRefID)
	}
	s.activeLeaseType = ""
	s.activeRefID = 0
	s.buffer.Reset()
}

// IP 白名单中间件 (严格忽略可被伪造的 X-Forwarded-For，仅取 TCP RemoteAddr)
func (s *AgentServer) ipAllowlistMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(s.allowPrefixes) > 0 {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			addr, err := netip.ParseAddr(host)
			if err != nil {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			allowed := false
			for _, pfx := range s.allowPrefixes {
				if pfx.Contains(addr) {
					allowed = true
					break
				}
			}

			if !allowed {
				http.Error(w, "Forbidden: IP not in allowlist", http.StatusForbidden)
				return
			}
		}

		s.touchContact()
		next.ServeHTTP(w, r)
	})
}

func (s *AgentServer) touchContact() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastContactAt = time.Now()
}

func (s *AgentServer) watchdogLoop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-s.watchdogStop:
			return
		case <-ticker.C:
			s.mu.Lock()
			hasLease := s.activeLeaseType != ""
			isDisconnected := atomic.LoadInt32(&s.wsActive) == 0
			timeoutExceeded := time.Since(s.lastContactAt) > s.cfg.GracePeriod
			s.mu.Unlock()

			if hasLease && isDisconnected && timeoutExceeded {
				s.PurgeAll()
			}
		}
	}
}

func (s *AgentServer) registerRoutes(mux *http.ServeMux) {
	// 1. 握手与心跳双向对账
	mux.HandleFunc("GET /agent/v1/handshake", s.handleHandshake)
	mux.HandleFunc("GET /agent/v1/health", s.handleHandshake)

	// 2. 候选进程探查
	mux.HandleFunc("GET /agent/v1/processes/discover", s.handleDiscoverProcesses)

	// 3. 声明式下发监控
	mux.HandleFunc("PUT /agent/v1/monitors/sync", s.handleSyncMonitors)

	// 4. 单独移除进程监控
	mux.HandleFunc("DELETE /agent/v1/monitors/{sessionId}/processes/{processId}", s.handleRemoveProcessMonitor)

	// 5. 停止录制会话
	mux.HandleFunc("POST /agent/v1/monitors/{sessionId}/stop", s.handleStopMonitor)

	// 6. 执行离线重放
	mux.HandleFunc("POST /agent/v1/replays/execute", s.handleExecuteReplay)

	// 7. 查询重放结果
	mux.HandleFunc("GET /agent/v1/replays/{taskId}/result", s.handleGetReplayResult)

	// 8. 取消重放
	mux.HandleFunc("POST /agent/v1/replays/{taskId}/cancel", s.handleCancelReplay)

	// 9. 强制清理与复位
	mux.HandleFunc("POST /agent/v1/purge", s.handlePurge)

	// 10. WebSocket 二进制流
	mux.HandleFunc("GET /agent/v1/stream", s.handleStream)
}

func (s *AgentServer) handleHandshake(w http.ResponseWriter, r *http.Request) {
	// 校验 ClaimCode
	clientClaim := r.Header.Get(agentproto.HeaderClaimCode)
	if clientClaim != "" && clientClaim != s.claimCode {
		http.Error(w, "Forbidden: Invalid ClaimCode", http.StatusForbidden)
		return
	}

	// 检查主控期望租约
	expectedType := r.Header.Get(agentproto.HeaderExpectedLeaseType)
	expectedRefStr := r.Header.Get(agentproto.HeaderExpectedLeaseRef)
	var expectedRef uint64
	if expectedRefStr != "" {
		expectedRef, _ = strconv.ParseUint(expectedRefStr, 10, 64)
	}

	s.mu.Lock()
	// 若主控判定租约已过期清空 (expectedType == "" 或 "NONE")，而 Agent 仍持有租约，立即触发自洁
	if (expectedType == "" || expectedType == "NONE") && s.activeLeaseType != "" {
		s.mu.Unlock()
		s.PurgeAll()
		s.mu.Lock()
	} else if expectedType != "" && (expectedType != s.activeLeaseType || expectedRef != s.activeRefID) {
		// 租约失调自愈
		s.mu.Unlock()
		s.PurgeAll()
		s.mu.Lock()
	}

	hasCaps, _, capDetail := s.ebpfMgr.CheckPrivileges()
	uptime := uint64(time.Since(s.startedAt).Seconds())

	resp := agentproto.HandshakeResp{
		AgentVersion:    s.cfg.AgentVersion,
		ProtocolVersion: agentproto.ProtocolVersion,
		BootID:          s.bootID,
		StartedAt:       s.startedAt,
		KernelVersion:   "Linux",
		HasEbpfCap:      hasCaps,
		CapDetail:       capDetail,
		ActiveLeaseType: s.activeLeaseType,
		ActiveRefID:     s.activeRefID,
		UptimeSec:       uptime,
	}
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *AgentServer) handleDiscoverProcesses(w http.ResponseWriter, r *http.Request) {
	procs, err := s.procScanner.ScanListenProcesses()
	if err != nil {
		http.Error(w, fmt.Sprintf("扫描进程失败: %v", err), http.StatusInternalServerError)
		return
	}
	if procs == nil {
		procs = []agentproto.DiscoveredProcess{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"items": procs,
	})
}

func (s *AgentServer) handleSyncMonitors(w http.ResponseWriter, r *http.Request) {
	var req agentproto.SyncMonitorReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	// SingleTenantGuard: 租约互斥检查
	if s.activeLeaseType != "" && (s.activeLeaseType != "RECORDING" || s.activeRefID != req.SessionID) {
		s.mu.Unlock()
		http.Error(w, "Conflict: Agent leased by other task", http.StatusConflict)
		return
	}
	s.activeLeaseType = "RECORDING"
	s.activeRefID = req.SessionID
	s.mu.Unlock()

	diags, err := s.ebpfMgr.SyncMonitors(req.SessionID, req.Processes)
	if err != nil {
		http.Error(w, fmt.Sprintf("下发监控失败: %v", err), http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"diagnostics": diags,
	})
}

func (s *AgentServer) handleRemoveProcessMonitor(w http.ResponseWriter, r *http.Request) {
	procIDStr := r.PathValue("processId")
	procID, err := strconv.ParseUint(procIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid processId", http.StatusBadRequest)
		return
	}

	rem, err := s.ebpfMgr.RemoveProcessMonitor(procID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if rem == 0 {
		s.mu.Lock()
		s.activeLeaseType = ""
		s.activeRefID = 0
		s.mu.Unlock()
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"removed":             true,
		"remaining_processes": rem,
	})
}

func (s *AgentServer) handleStopMonitor(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, dropped, total := s.buffer.Stats()
	bypassed, _, _ := s.ebpfMgr.GetStats()

	// 发送 EVENT_SESSION_DRAINED 终结信号
	drainedEnv := agentproto.EnvelopeHeader{
		Magic:           agentproto.EnvelopeMagic,
		ProtoVer:        agentproto.EnvelopeProtoVer,
		EventType:       agentproto.EventSessionDrained,
		SessionOrTaskID: s.activeRefID,
		CapturedAtNs:    time.Now().UnixNano(),
	}
	s.buffer.Push(agentproto.MarshalEnvelope(drainedEnv, nil))

	s.ebpfMgr.PurgeAllMonitors()
	s.activeLeaseType = ""
	s.activeRefID = 0

	resp := agentproto.StopMonitorResp{
		CapturedFrames:    total,
		DroppedFrames:     dropped,
		BypassedSelfCount: bypassed,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *AgentServer) handleExecuteReplay(w http.ResponseWriter, r *http.Request) {
	var req agentproto.ExecuteReplayReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	if s.activeLeaseType != "" && (s.activeLeaseType != "REPLAY" || s.activeRefID != req.TaskID) {
		s.mu.Unlock()
		http.Error(w, "Conflict: Agent leased by other task", http.StatusConflict)
		return
	}
	s.activeLeaseType = "REPLAY"
	s.activeRefID = req.TaskID
	s.mu.Unlock()

	// 异步执行重放
	go func() {
		_, _ = s.replayExec.Execute(context.Background(), req)
	}()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ARMED",
		"diagnostics": agentproto.ProcessDiagStatus{
			ProcessID: req.ProcessSpec.ProcessID,
			Status:    "MONITORING",
		},
	})
}

func (s *AgentServer) handleGetReplayResult(w http.ResponseWriter, r *http.Request) {
	taskIDStr := r.PathValue("taskId")
	taskID, err := strconv.ParseUint(taskIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid taskId", http.StatusBadRequest)
		return
	}

	res, ok := s.replayExec.GetResult(taskID)
	if !ok {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

func (s *AgentServer) handleCancelReplay(w http.ResponseWriter, r *http.Request) {
	taskIDStr := r.PathValue("taskId")
	taskID, _ := strconv.ParseUint(taskIDStr, 10, 64)

	s.replayExec.Cancel(taskID)
	s.mu.Lock()
	if s.activeRefID == taskID {
		s.activeLeaseType = ""
		s.activeRefID = 0
	}
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"cancelled": true,
	})
}

func (s *AgentServer) handlePurge(w http.ResponseWriter, r *http.Request) {
	s.PurgeAll()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"purged": true,
	})
}

func (s *AgentServer) handleStream(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	atomic.StoreInt32(&s.wsActive, 1)
	defer atomic.StoreInt32(&s.wsActive, 0)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// 异步读取客户端消息 (维持握手探活)
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				cancel()
				return
			}
			s.touchContact()
		}
	}()

	for {
		item, ok := s.buffer.Pop(ctx)
		if !ok || ctx.Err() != nil {
			return
		}

		if err := conn.WriteMessage(websocket.BinaryMessage, item); err != nil {
			return
		}
	}
}

// Ensure unused check
var _ = io.EOF
