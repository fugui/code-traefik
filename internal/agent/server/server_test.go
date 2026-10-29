package server

import (
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"code-traefik/pkg/agentproto"
)

// TestAgentZeroSchemaAndZeroDBDeps 静态架构断言: 数据面零 gorm、零 dynamicpb/protoregistry、零 os/exec
func TestAgentZeroSchemaAndZeroDBDeps(t *testing.T) {
	// 1. 执行 go list -deps 断言数据面依赖闭包
	cmd := exec.Command("go", "list", "-deps", "./pkg/ebpf", "./pkg/replay", "./internal/agent/...")
	cmd.Dir = "../../../" // code-traefik root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps failed: %v, output: %s", err, string(out))
	}

	forbiddenDeps := []string{
		"gorm.io",
		"code-common/backend/gormdb",
		"google.golang.org/protobuf/types/dynamicpb",
		"google.golang.org/protobuf/reflect/protoregistry",
	}

	for _, line := range strings.Split(string(out), "\n") {
		trimmed := strings.TrimSpace(line)
		for _, forbidden := range forbiddenDeps {
			if strings.Contains(trimmed, forbidden) {
				t.Fatalf("数据面架构红线违背: 引入了禁止的包 %s (命中 %s)", forbidden, trimmed)
			}
		}
	}

	// 2. 扫描 AST 确保源码中没有使用 os/exec (排除测试文件)
	rootDirs := []string{"../../../pkg/ebpf", "../../../pkg/replay", "../../../internal/agent"}
	for _, rootDir := range rootDirs {
		err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
				fset := token.NewFileSet()
				node, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
				if err != nil {
					return err
				}
				for _, imp := range node.Imports {
					importPath := strings.Trim(imp.Path.Value, `"`)
					if importPath == "os/exec" {
						t.Errorf("源码文件 %s 违规引入了 os/exec 外部命令拼接", path)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("扫描目录 %s AST 失败: %v", rootDir, err)
		}
	}
}

// TestAgentIPAllowlistAndClaimCode 验证源 IP 白名单过滤与 ClaimCode 校验
func TestAgentIPAllowlistAndClaimCode(t *testing.T) {
	cfg := AgentServerConfig{
		ListenAddr: "127.0.0.1:0",
		AllowIPs:   []string{"127.0.0.1/32"},
		ClaimCode:  "my_secure_claim_code_12345",
	}

	srv, err := NewAgentServer(cfg)
	if err != nil {
		t.Fatalf("NewAgentServer failed: %v", err)
	}

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	handler := srv.ipAllowlistMiddleware(mux)

	// 1. 模拟非白名单 IP (附带伪造的 X-Forwarded-For: 127.0.0.1)
	reqForbidden := httptest.NewRequest(http.MethodGet, "/agent/v1/handshake", nil)
	reqForbidden.RemoteAddr = "10.20.30.40:54321" // 非白名单 IP
	reqForbidden.Header.Set("X-Forwarded-For", "127.0.0.1")
	recForbidden := httptest.NewRecorder()
	handler.ServeHTTP(recForbidden, reqForbidden)

	if recForbidden.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for non-whitelisted IP, got %d", recForbidden.Code)
	}

	// 2. 白名单 IP 携带错误的 ClaimCode
	reqBadClaim := httptest.NewRequest(http.MethodGet, "/agent/v1/handshake", nil)
	reqBadClaim.RemoteAddr = "127.0.0.1:54321"
	reqBadClaim.Header.Set(agentproto.HeaderClaimCode, "wrong_claim_code")
	recBadClaim := httptest.NewRecorder()
	handler.ServeHTTP(recBadClaim, reqBadClaim)

	if recBadClaim.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for wrong ClaimCode, got %d", recBadClaim.Code)
	}

	// 3. 白名单 IP 携带正确的 ClaimCode
	reqOK := httptest.NewRequest(http.MethodGet, "/agent/v1/handshake", nil)
	reqOK.RemoteAddr = "127.0.0.1:54321"
	reqOK.Header.Set(agentproto.HeaderClaimCode, "my_secure_claim_code_12345")
	recOK := httptest.NewRecorder()
	handler.ServeHTTP(recOK, reqOK)

	if recOK.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for valid ClaimCode, got %d", recOK.Code)
	}

	var handshakeResp agentproto.HandshakeResp
	if err := json.Unmarshal(recOK.Body.Bytes(), &handshakeResp); err != nil {
		t.Fatalf("unmarshal HandshakeResp failed: %v", err)
	}
	if handshakeResp.ProtocolVersion != "1.0" {
		t.Errorf("expected ProtocolVersion 1.0, got %s", handshakeResp.ProtocolVersion)
	}
	if handshakeResp.BootID == "" {
		t.Errorf("expected non-empty BootID")
	}
}

// TestBoundedSessionBufferBackpressureAndGracePurge 验证有界缓冲背压丢帧与失联看门狗自洁
func TestBoundedSessionBufferBackpressureAndGracePurge(t *testing.T) {
	// 1. 测试 1MB 缓冲上限与非阻塞丢帧
	maxBytes := int64(1024 * 1024) // 1MB
	buf := NewBoundedSessionBuffer(maxBytes)

	// 生成 256KB 的大帧
	frameChunk := make([]byte, 256*1024)

	// 灌入 4 帧 = 1MB 刚好填满
	for i := 0; i < 4; i++ {
		ok := buf.Push(frameChunk)
		if !ok {
			t.Fatalf("frame %d should be pushed", i)
		}
	}

	// 再灌入 4 帧 (共 2MB) -> 触发背压非阻塞丢弃
	for i := 0; i < 4; i++ {
		ok := buf.Push(frameChunk)
		if ok {
			t.Errorf("frame %d should have been dropped due to backpressure", i)
		}
	}

	used, dropped, total := buf.Stats()
	if used > maxBytes {
		t.Errorf("used bytes %d exceeded max buffer %d", used, maxBytes)
	}
	if dropped != 4 {
		t.Errorf("expected exactly 4 dropped frames, got %d", dropped)
	}
	if total != 4 {
		t.Errorf("expected exactly 4 captured frames, got %d", total)
	}

	// 2. 测试失联看门狗宽限期自洁
	cfg := AgentServerConfig{
		ListenAddr:  "127.0.0.1:0",
		GracePeriod: 200 * time.Millisecond,
	}
	srv, err := NewAgentServer(cfg)
	if err != nil {
		t.Fatalf("NewAgentServer failed: %v", err)
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("Start server failed: %v", err)
	}
	defer srv.Shutdown(context.Background())

	// 锁定一个模拟租约
	srv.mu.Lock()
	srv.activeLeaseType = "RECORDING"
	srv.activeRefID = 8888
	srv.lastContactAt = time.Now()
	srv.mu.Unlock()

	// 等待超过 200ms 宽限期且无 WebSocket 活跃连接
	time.Sleep(350 * time.Millisecond)

	leaseType, refID := srv.ActiveLease()
	if leaseType != "" || refID != 0 {
		t.Errorf("expected lease cleared after watchdog grace period, got %s, %d", leaseType, refID)
	}

	// 3. 测试心跳双向对账：下发 X-Expected-Lease-Type: "" 时立即自洁
	srv.mu.Lock()
	srv.activeLeaseType = "RECORDING"
	srv.activeRefID = 9999
	srv.mu.Unlock()

	reqReconcile := httptest.NewRequest(http.MethodGet, "/agent/v1/handshake", nil)
	reqReconcile.RemoteAddr = "127.0.0.1:12345"
	reqReconcile.Header.Set(agentproto.HeaderExpectedLeaseType, "") // 主控期望无租约
	recReconcile := httptest.NewRecorder()

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	mux.ServeHTTP(recReconcile, reqReconcile)

	leaseTypeAfter, refIDAfter := srv.ActiveLease()
	if leaseTypeAfter != "" || refIDAfter != 0 {
		t.Errorf("expected lease cleared after empty expected lease heartbeat, got %s, %d", leaseTypeAfter, refIDAfter)
	}
}
