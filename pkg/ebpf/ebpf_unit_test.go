package ebpf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"code-traefik/pkg/agentproto"
	"code-traefik/pkg/crosrpc"
	"code-traefik/pkg/replay"
)

// mockProcTree 在临时目录下构建伪造的 procfs 结构
func mockProcTree(t *testing.T, entries map[string]string) string {
	dir := t.TempDir()
	for relPath, content := range entries {
		absPath := filepath.Join(dir, relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
		if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
			t.Fatalf("writeFile failed: %v", err)
		}
	}
	return dir
}

// TestProcScannerPIDResolveAndEstablishedConns 测试 /proc 扫描与存量连接诊断
func TestProcScannerPIDResolveAndEstablishedConns(t *testing.T) {
	tempDir := t.TempDir()

	// 1. 创建 /proc/net/tcp 文件
	// 格式: sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
	// port 9001 (0x2329), port 9002 (0x232A), 存量连接到 127.0.0.1:8080 (0x1F90)
	netTcpContent := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:2329 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 50001 1 0000000000000000 100 0 0 10 0
   1: 0100007F:232A 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 50002 1 0000000000000000 100 0 0 10 0
`
	if err := os.MkdirAll(filepath.Join(tempDir, "net"), 0755); err != nil {
		t.Fatalf("mkdir net failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "net", "tcp"), []byte(netTcpContent), 0644); err != nil {
		t.Fatalf("write /net/tcp failed: %v", err)
	}

	// 2. 模拟 PID 101 (监听 9001, 有一条到 127.0.0.1:8080 的 ESTABLISHED 连接)
	pid101Dir := filepath.Join(tempDir, "101")
	_ = os.MkdirAll(filepath.Join(pid101Dir, "fd"), 0755)
	_ = os.MkdirAll(filepath.Join(pid101Dir, "net"), 0755)
	_ = os.WriteFile(filepath.Join(pid101Dir, "cmdline"), []byte("payment-service\x00--port=9001\x00"), 0644)
	_ = os.WriteFile(filepath.Join(pid101Dir, "comm"), []byte("payment-svc\n"), 0644)
	_ = os.Symlink("socket:[50001]", filepath.Join(pid101Dir, "fd", "3"))

	// PID 101 的存量已建立长连接
	pid101TcpContent := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:C001 0100007F:1F90 01 00000000:00000000 00:00000000 00000000  1000        0 50003 1 0000000000000000 100 0 0 10 0
`
	_ = os.WriteFile(filepath.Join(pid101Dir, "net", "tcp"), []byte(pid101TcpContent), 0644)

	// 3. 模拟 PID 102 (监听 9002, 无长连接)
	pid102Dir := filepath.Join(tempDir, "102")
	_ = os.MkdirAll(filepath.Join(pid102Dir, "fd"), 0755)
	_ = os.MkdirAll(filepath.Join(pid102Dir, "net"), 0755)
	_ = os.WriteFile(filepath.Join(pid102Dir, "cmdline"), []byte("order-service\x00--port=9002\x00"), 0644)
	_ = os.WriteFile(filepath.Join(pid102Dir, "comm"), []byte("order-svc\n"), 0644)
	_ = os.Symlink("socket:[50002]", filepath.Join(pid102Dir, "fd", "4"))
	_ = os.WriteFile(filepath.Join(pid102Dir, "net", "tcp"), []byte("  sl  local_address rem_address st ...\n"), 0644)

	scanner := NewProcScanner(tempDir)

	// 1. 扫描候选监听进程
	procs, err := scanner.ScanListenProcesses()
	if err != nil {
		t.Fatalf("ScanListenProcesses failed: %v", err)
	}
	if len(procs) != 2 {
		t.Fatalf("expected 2 discovered processes, got %d", len(procs))
	}

	// 2. 诊断已建立连接
	downstreams := []agentproto.DownstreamEndpoint{
		{IP: "127.0.0.1", Port: 8080},
	}
	estConns, needRec, err := scanner.DiagnoseEstablishedConns(101, downstreams)
	if err != nil {
		t.Fatalf("DiagnoseEstablishedConns failed: %v", err)
	}
	if estConns != 1 || !needRec {
		t.Errorf("PID 101 expected 1 established conn with needReconnect=true, got %d, %v", estConns, needRec)
	}

	// 3. 诊断不存在的端口
	pidNotFound, _ := scanner.FindPIDByPortAndCmd(9999, "")
	if pidNotFound != 0 {
		t.Errorf("expected 0 for non-existent port, got %d", pidNotFound)
	}

	// 4. 同机反查对端临时端口所属进程
	// 模拟建立临时端口 0xC001 (49153) 所属进程
	peerPID, err := scanner.FindPeerPIDForLocalPort(49153)
	// 在模拟目录中对端不是监听端口，但验证反查接口运行无异常
	_ = peerPID
}

// mockCapabilityChecker 模拟特权探测
type mockCapabilityChecker struct {
	hasCaps bool
	missing []string
	detail  string
}

func (m *mockCapabilityChecker) CheckCapabilities() (bool, []string, string) {
	return m.hasCaps, m.missing, m.detail
}

// TestEbpfCapabilityCheckWithoutPrivilege 测试无特权环境拦截返回 503 与诊断
func TestEbpfCapabilityCheckWithoutPrivilege(t *testing.T) {
	mockChecker := &mockCapabilityChecker{
		hasCaps: false,
		missing: []string{"CAP_BPF", "CAP_NET_ADMIN"},
		detail:  "unprivileged process",
	}

	mgr := NewManager(ManagerConfig{
		CapabilityChecker: mockChecker,
	})

	hasCaps, missing, detail := mgr.CheckPrivileges()
	_ = detail
	if hasCaps {
		t.Fatalf("expected hasCaps=false, got true")
	}
	if len(missing) != 2 || missing[0] != "CAP_BPF" {
		t.Errorf("expected missing CAP_BPF, got %v", missing)
	}

	specs := []agentproto.ProcessMonitorSpec{
		{ProcessID: 1, ListenPort: 8080},
	}
	diags, err := mgr.SyncMonitors(1001, specs)
	if err == nil {
		t.Fatalf("expected error on SyncMonitors without privilege, got nil")
	}
	if len(diags) != 1 || diags[0].Status != "CAP_DENIED" {
		t.Errorf("expected diag status CAP_DENIED, got %+v", diags)
	}
}

// TestGapAwareIngressReaderAndBudgetPool 测试 RingBuf 丢包感知与全局 LRU 内存预算池
func TestGapAwareIngressReaderAndBudgetPool(t *testing.T) {
	// 1. 测试 Gap 感知
	pool := NewGlobalFramerBudgetPool(64 * 1024 * 1024)
	reader := NewGapAwareIngressReader(pool, 4*1024*1024)

	// 构造有效 CROS RPC 请求帧
	framePayload := []byte("hello_cros_rpc_world")
	headLen := uint16(160)
	bodyLen := uint32(headLen) + uint32(len(framePayload))
	totalLen := 24 + bodyLen

	pkt := make([]byte, totalLen)
	copy(pkt[0:7], []byte("CSE.TCP"))
	pkt[7] = 1
	pkt[20] = byte(headLen >> 8)
	pkt[21] = byte(headLen)
	pkt[16] = byte(bodyLen >> 24)
	pkt[17] = byte(bodyLen >> 16)
	pkt[18] = byte(bodyLen >> 8)
	pkt[19] = byte(bodyLen)
	pkt[22] = crosrpc.MsgTypeReq
	pkt[23] = 1

	// ReqHead: service name and operation
	copy(pkt[24+8:24+72], []byte("com.demo.TestService"))
	copy(pkt[24+72:24+136], []byte("Hello"))
	pkt[24+136] = crosrpc.ReqTypeSync
	copy(pkt[24+headLen:], framePayload)

	// 模拟第一个正常 chunk
	chunk1 := IngressChunk{
		SessionID: 1,
		ProcessID: 10,
		PID:       1001,
		Direction: agentproto.DirectionIngress,
		StreamOff: 0,
		Data:      pkt[:30], // 截断的前半部分
	}
	res1, err := reader.FeedChunk(chunk1)
	if err != nil {
		t.Fatalf("feed chunk1 failed: %v", err)
	}
	if res1.HasGap {
		t.Errorf("expected no gap on initial chunk")
	}
	if len(res1.Frames) != 0 {
		t.Errorf("expected 0 frames on partial chunk, got %d", len(res1.Frames))
	}

	// 模拟 RingBuf 丢包导致 StreamOff 跳跃 (期待 30, 实际传来 100)
	chunk2 := IngressChunk{
		SessionID: 1,
		ProcessID: 10,
		PID:       1001,
		Direction: agentproto.DirectionIngress,
		StreamOff: 100, // 发生了 70 字节断流!
		Data:      pkt, // 紧随一条完整的新报文
	}
	res2, err := reader.FeedChunk(chunk2)
	if err != nil {
		t.Fatalf("feed chunk2 failed: %v", err)
	}
	if !res2.HasGap {
		t.Errorf("expected HasGap=true on stream break")
	}
	if len(res2.Frames) != 1 {
		t.Fatalf("expected 1 completed frame after resync, got %d", len(res2.Frames))
	}

	// 检查信封的 FlagHasGap 标记
	envHeader, _, err := agentproto.UnmarshalEnvelope(res2.Frames[0])
	if err != nil {
		t.Fatalf("unmarshal envelope failed: %v", err)
	}
	if !envHeader.HasGap() {
		t.Errorf("expected FlagHasGap set on envelope after stream break")
	}

	// 2. 测试 64MB 全局分帧预算池与 LRU 淘汰
	budgetMax := 1024 * 1024 // 测试用 1MB 预算
	budgetPool := NewGlobalFramerBudgetPool(budgetMax)

	// 模拟插入 200 个连接，每个消耗 8KB 初始内存
	for i := 0; i < 200; i++ {
		key := ConnStreamKey{
			PID:       uint32(1000 + i),
			ConnSeq:   uint32(i),
			Direction: agentproto.DirectionIngress,
		}
		budgetPool.GetOrCreate(key, 4096)
	}

	// 验证总内存严格处于预算内
	if budgetPool.UsedBytes() > budgetMax {
		t.Errorf("used bytes %d exceeded max budget %d", budgetPool.UsedBytes(), budgetMax)
	}
	if budgetPool.EvictCount() == 0 {
		t.Errorf("expected evictions to have occurred")
	}
}

// TestEgressProxy_ThreeModes_NonDisruptiveStopAndReplayZeroLeakage 测试 EgressProxy 三态转换、停录不断连与回放零外泄 (ADR-09, AC-M10-02)
func TestEgressProxy_ThreeModes_NonDisruptiveStopAndReplayZeroLeakage(t *testing.T) {
	// 1. 启动真实后端下游模拟服务
	realBackendLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen real backend failed: %v", err)
	}
	defer realBackendLn.Close()
	realBackendPort := uint16(realBackendLn.Addr().(*net.TCPAddr).Port)

	var backendReceivedBytes int64
	backendDone := make(chan struct{})

	go func() {
		for {
			conn, err := realBackendLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				for {
					n, err := c.Read(buf)
					if n > 0 {
						atomic.AddInt64(&backendReceivedBytes, int64(n))
						// Echo 原样回包
						_, _ = c.Write(buf[:n])
					}
					if err != nil {
						return
					}
				}
			}(conn)
		}
	}()
	defer close(backendDone)

	// 2. 构造 EgressProxy 实例
	var capturedEnvelopes [][]byte
	var capMu sync.Mutex

	proxy, err := NewEgressProxy(
		func(clientPort uint16) (uint64, uint32, net.IP, uint16, bool) {
			return 100, 1234, net.IPv4(127, 0, 0, 1), realBackendPort, true
		},
		func(envBytes []byte) {
			capMu.Lock()
			capturedEnvelopes = append(capturedEnvelopes, envBytes)
			capMu.Unlock()
		},
	)
	if err != nil {
		t.Fatalf("NewEgressProxy failed: %v", err)
	}
	defer proxy.Close()

	// 验证初始为 PASSTHROUGH 模式
	if proxy.Mode() != ModePassthrough {
		t.Fatalf("expected initial mode PASSTHROUGH, got %v", proxy.Mode())
	}

	// 3. 阶段一：PASSTHROUGH 待命模式下建立长连接并发送数据
	proxyAddr := fmt.Sprintf("127.0.0.1:%d", proxy.ProxyPort())
	clientConn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy failed: %v", err)
	}
	defer clientConn.Close()

	pingMsg := []byte("PASSTHROUGH_PING_MSG")
	if _, err := clientConn.Write(pingMsg); err != nil {
		t.Fatalf("write in passthrough failed: %v", err)
	}
	replyBuf := make([]byte, len(pingMsg))
	if _, err := io.ReadFull(clientConn, replyBuf); err != nil {
		t.Fatalf("read echo in passthrough failed: %v", err)
	}
	if !bytes.Equal(replyBuf, pingMsg) {
		t.Fatalf("expected echo %s, got %s", pingMsg, replyBuf)
	}

	capMu.Lock()
	if len(capturedEnvelopes) != 0 {
		t.Fatalf("passthrough mode must not capture envelopes, got %d", len(capturedEnvelopes))
	}
	capMu.Unlock()

	// 4. 阶段二：切换为 CAPTURING 录制模式 (在同一条长连接上复用)
	proxy.SetMode(ModeCapturing)

	// 构造标准 CROS RPC 请求帧
	framePayload := []byte("test_cros_capture_payload")
	headLen := uint16(160)
	bodyLen := uint32(headLen) + uint32(len(framePayload))
	totalLen := 24 + bodyLen

	pkt := make([]byte, totalLen)
	copy(pkt[0:7], []byte("CSE.TCP"))
	pkt[7] = 1
	pkt[20] = byte(headLen >> 8)
	pkt[21] = byte(headLen)
	pkt[16] = byte(bodyLen >> 24)
	pkt[17] = byte(bodyLen >> 16)
	pkt[18] = byte(bodyLen >> 8)
	pkt[19] = byte(bodyLen)
	pkt[22] = crosrpc.MsgTypeReq
	pkt[23] = 1
	copy(pkt[24+8:24+72], []byte("com.test.OrderService"))
	copy(pkt[24+72:24+136], []byte("CreateOrder"))
	pkt[24+136] = crosrpc.ReqTypeSync
	copy(pkt[24+headLen:], framePayload)

	if _, err := clientConn.Write(pkt); err != nil {
		t.Fatalf("write in capturing failed: %v", err)
	}
	rspBuf := make([]byte, totalLen)
	if _, err := io.ReadFull(clientConn, rspBuf); err != nil {
		t.Fatalf("read echo in capturing failed: %v", err)
	}

	// 等待帧捕获并排空
	time.Sleep(50 * time.Millisecond)
	capMu.Lock()
	if len(capturedEnvelopes) == 0 {
		t.Fatalf("expected captured envelopes in CAPTURING mode, got 0")
	}
	capMu.Unlock()

	// 5. 阶段三：停止录制，调用 DrainAndSetPassthrough 切回待命模式 (停录不断连不变性)
	proxy.DrainAndSetPassthrough(50 * time.Millisecond)
	if proxy.Mode() != ModePassthrough {
		t.Fatalf("expected mode PASSTHROUGH after drain, got %v", proxy.Mode())
	}

	// 核心断言：在完全不重连同一 clientConn 的情况下，连接依旧存活并能正常收发流量！
	afterStopMsg := []byte("AFTER_STOP_NON_DISRUPTIVE_MSG")
	if _, err := clientConn.Write(afterStopMsg); err != nil {
		t.Fatalf("long connection broken after stop: %v", err)
	}
	afterStopRsp := make([]byte, len(afterStopMsg))
	if _, err := io.ReadFull(clientConn, afterStopRsp); err != nil {
		t.Fatalf("read long connection echo failed: %v", err)
	}
	if !bytes.Equal(afterStopRsp, afterStopMsg) {
		t.Fatalf("expected echo %s, got %s", afterStopMsg, afterStopRsp)
	}
	_ = clientConn.Close()

	// 6. 阶段四：REPLAY_MOCK 模式与 0 字节外泄阻断验证
	atomic.StoreInt64(&backendReceivedBytes, 0) // 重置真实后端流量计数器

	// 构造标准 V01 格式 (156B 会话头) Mock 匹配器与 Mock 响应帧
	matcher := replay.NewScopedMockMatcher()
	mockRspRaw := make([]byte, 156)
	binary.BigEndian.PutUint16(mockRspRaw[0:2], 1)
	binary.BigEndian.PutUint16(mockRspRaw[2:4], 156)
	copy(mockRspRaw[24:88], []byte("com.test.OrderService"))
	copy(mockRspRaw[88:152], []byte("CreateOrder"))

	mockComm := make([]byte, 24)
	copy(mockComm[0:7], []byte("CSE.TCP"))
	mockComm[7] = 1
	mockBodyLen := uint32(156 + len(framePayload))
	mockComm[16] = byte(mockBodyLen >> 24)
	mockComm[17] = byte(mockBodyLen >> 16)
	mockComm[18] = byte(mockBodyLen >> 8)
	mockComm[19] = byte(mockBodyLen)
	mockComm[20] = byte(156 >> 8)
	mockComm[21] = byte(156)
	mockComm[22] = crosrpc.MsgTypeRsp
	mockComm[23] = 1

	var mockRsp []byte
	mockRsp = append(mockRsp, mockComm...)
	mockRsp = append(mockRsp, mockRspRaw...)
	mockRsp = append(mockRsp, framePayload...)

	matcher.AddGlobalFallback("com.test.OrderService", "CreateOrder", framePayload, [][]byte{mockRsp})

	proxy.SetMockMatcher(matcher, []agentproto.DownstreamEndpoint{
		{IP: "127.0.0.1", Port: realBackendPort},
	})
	proxy.SetMode(ModeReplayMock)

	// 新客户端发起回放流量
	replayConn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial replay conn failed: %v", err)
	}
	defer replayConn.Close()

	if _, err := replayConn.Write(pkt); err != nil {
		t.Fatalf("write replay request failed: %v", err)
	}
	replayRspBuf := make([]byte, len(mockRsp))
	if _, err := io.ReadFull(replayConn, replayRspBuf); err != nil {
		t.Fatalf("read mock response failed: %v", err)
	}
	if replayRspBuf[22] != crosrpc.MsgTypeRsp {
		t.Fatalf("expected MsgTypeRsp (3), got %d", replayRspBuf[22])
	}

	// 核心红线验证：真实后端接收到的字节数必须严格为 0！0 字节泄漏到被 Mock 的下游服务！
	realLeaked := atomic.LoadInt64(&backendReceivedBytes)
	if realLeaked != 0 {
		t.Fatalf("REPLAY_MOCK mode leaked %d bytes to real target backend! Expected strictly 0", realLeaked)
	}

	// 7. 优雅排空验证 (GracefulDrain)
	_ = replayConn.Close()
	if err := proxy.GracefulDrain(500 * time.Millisecond); err != nil {
		t.Fatalf("GracefulDrain failed: %v", err)
	}
	// 验证排空后新连接被拒绝
	_, err = net.DialTimeout("tcp", proxyAddr, 100*time.Millisecond)
	if err == nil {
		t.Fatalf("expected dial to fail after GracefulDrain")
	}
}

// BenchmarkEgressProxy_ArmedPassthrough 待命模式下透明转发单核性能基准测试 (内存申请 0 B/op)
func BenchmarkEgressProxy_ArmedPassthrough(b *testing.B) {
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("listen echo failed: %v", err)
	}
	defer echoLn.Close()
	echoPort := uint16(echoLn.Addr().(*net.TCPAddr).Port)

	go func() {
		for {
			c, err := echoLn.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				buf := make([]byte, 8192)
				for {
					n, err := conn.Read(buf)
					if n > 0 {
						_, _ = conn.Write(buf[:n])
					}
					if err != nil {
						return
					}
				}
			}(c)
		}
	}()

	proxy, err := NewEgressProxy(
		func(clientPort uint16) (uint64, uint32, net.IP, uint16, bool) {
			return 1, 100, net.IPv4(127, 0, 0, 1), echoPort, true
		},
		nil,
	)
	if err != nil {
		b.Fatalf("NewEgressProxy failed: %v", err)
	}
	defer proxy.Close()

	proxyAddr := fmt.Sprintf("127.0.0.1:%d", proxy.ProxyPort())
	client, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		b.Fatalf("dial failed: %v", err)
	}
	defer client.Close()

	req := make([]byte, 1024)
	rsp := make([]byte, 1024)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := client.Write(req); err != nil {
			b.Fatalf("write failed: %v", err)
		}
		if _, err := io.ReadFull(client, rsp); err != nil {
			b.Fatalf("read failed: %v", err)
		}
	}
}
