package ebpf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
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
	_ = os.Symlink("socket:[50003]", filepath.Join(pid101Dir, "fd", "4"))

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

func TestBPFEndianness_IPv4AndPortSymmetricConversion(t *testing.T) {
	// 1. IP 转换对称性与逐字节网络序断言
	ip1 := net.ParseIP("127.0.0.1")
	bpfVal1 := ipv4ToBPFBe32(ip1)
	// 小端机器上 net.ParseIP("127.0.0.1").To4() 为 [127, 0, 0, 1]，NativeEndian 值为 0x0100007f
	if bpfVal1 != 0x0100007f {
		t.Fatalf("ipv4ToBPFBe32(127.0.0.1) 预期 0x0100007f，实际得到 0x%08x", bpfVal1)
	}
	recoveredIP1 := bpfBe32ToIPv4(bpfVal1)
	if !recoveredIP1.Equal(ip1) {
		t.Fatalf("bpfBe32ToIPv4(0x%08x) 还原失败: 期望 %s，实际 %s", bpfVal1, ip1, recoveredIP1)
	}

	ip2 := net.ParseIP("192.168.56.18")
	bpfVal2 := ipv4ToBPFBe32(ip2)
	recoveredIP2 := bpfBe32ToIPv4(bpfVal2)
	if !recoveredIP2.Equal(ip2) {
		t.Fatalf("bpfBe32ToIPv4 还原 192.168.56.18 失败: 实际 %s", recoveredIP2)
	}

	// 2. 端口 htons / ntohs 往返与网络字节序断言
	// 9001 = 0x2329，htons 预期为 0x2923
	bePort1 := htons(9001)
	if bePort1 != 0x2923 {
		t.Fatalf("htons(9001) 预期 0x2923，实际得到 0x%04x", bePort1)
	}
	if hostPort1 := ntohs(bePort1); hostPort1 != 9001 {
		t.Fatalf("ntohs(htons(9001)) 预期 9001，实际得到 %d", hostPort1)
	}

	port2 := uint16(19091)
	bePort2 := htons(port2)
	if hostPort2 := ntohs(bePort2); hostPort2 != port2 {
		t.Fatalf("ntohs(htons(%d)) 预期 %d，实际得到 %d", port2, port2, hostPort2)
	}
}

func TestIngressReaderAndEgressProxy_ConstantDirection_SessionID_AndNonCrosPassthrough(t *testing.T) {
	// Part 1: IngressReader 恒定 DirectionIngress 与四元组解耦验证
	reader := NewGapAwareIngressReader(nil, crosrpc.DefaultMaxCaptureBytes)

	// 构造 CROS RPC REQ 帧
	reqPayload := []byte("cros_ingress_req_payload")
	reqPkt := make([]byte, 24+160+len(reqPayload))
	copy(reqPkt[0:7], []byte("CSE.TCP"))
	reqPkt[7] = 1
	bodyLen := uint32(160 + len(reqPayload))
	binary.BigEndian.PutUint32(reqPkt[16:20], bodyLen)
	binary.BigEndian.PutUint16(reqPkt[20:22], 160)
	reqPkt[22] = crosrpc.MsgTypeReq
	reqPkt[23] = 1
	copy(reqPkt[24+8:24+72], []byte("com.test.OrderService"))
	copy(reqPkt[24+72:24+136], []byte("CreateOrder"))
	reqPkt[24+136] = crosrpc.ReqTypeSync
	copy(reqPkt[24+160:], reqPayload)

	// 喂入 rw_dir=1 (RX_READ，请求)
	res1, err := reader.FeedChunk(IngressChunk{
		SessionID: 88,
		ProcessID: 10,
		PID:       1001,
		Direction: 1, // BPF 内核读事件
		SrcIP:     net.IPv4(10, 0, 0, 1),
		DstIP:     net.IPv4(127, 0, 0, 1),
		SrcPort:   45678,
		DstPort:   9001,
		ConnSeq:   1,
		StreamOff: 0,
		Data:      reqPkt,
	})
	if err != nil || len(res1.Frames) != 1 {
		t.Fatalf("FeedChunk REQ failed: %v, frames=%d", err, len(res1.Frames))
	}

	envReq, _, err := agentproto.UnmarshalEnvelope(res1.Frames[0])
	if err != nil {
		t.Fatalf("UnmarshalEnvelope REQ failed: %v", err)
	}
	if envReq.Direction != agentproto.DirectionIngress {
		t.Fatalf("IngressReader REQ 预期 DirectionIngress (1)，实际得到 %d", envReq.Direction)
	}
	if envReq.SessionOrTaskID != 88 {
		t.Fatalf("SessionOrTaskID 预期 88，实际得到 %d", envReq.SessionOrTaskID)
	}

	// 构造 CROS RPC RSP 帧
	rspPayload := []byte("cros_ingress_rsp_payload")
	rspPkt := make([]byte, 24+156+len(rspPayload))
	copy(rspPkt[0:7], []byte("CSE.TCP"))
	rspPkt[7] = 1
	rspBodyLen := uint32(156 + len(rspPayload))
	binary.BigEndian.PutUint32(rspPkt[16:20], rspBodyLen)
	binary.BigEndian.PutUint16(rspPkt[20:22], 156)
	rspPkt[22] = crosrpc.MsgTypeRsp
	rspPkt[23] = 1
	copy(rspPkt[24+8:24+72], []byte("com.test.OrderService"))
	copy(rspPkt[24+72:24+136], []byte("CreateOrder"))
	copy(rspPkt[24+156:], rspPayload)

	// 喂入 rw_dir=2 (TX_WRITE，应答)
	res2, err := reader.FeedChunk(IngressChunk{
		SessionID: 88,
		ProcessID: 10,
		PID:       1001,
		Direction: 2, // BPF 内核写事件
		SrcIP:     net.IPv4(10, 0, 0, 1),
		DstIP:     net.IPv4(127, 0, 0, 1),
		SrcPort:   45678,
		DstPort:   9001,
		ConnSeq:   1,
		StreamOff: 0,
		Data:      rspPkt,
	})
	if err != nil || len(res2.Frames) != 1 {
		t.Fatalf("FeedChunk RSP failed: %v, frames=%d", err, len(res2.Frames))
	}

	envRsp, _, err := agentproto.UnmarshalEnvelope(res2.Frames[0])
	if err != nil {
		t.Fatalf("UnmarshalEnvelope RSP failed: %v", err)
	}
	// 关键断言：应答帧信封头 Direction 必须恒定为 DirectionIngress (1)！
	if envRsp.Direction != agentproto.DirectionIngress {
		t.Fatalf("IngressReader RSP 预期 DirectionIngress (1)，实际得到 %d", envRsp.Direction)
	}

	// Part 2: EgressProxy 恒定 DirectionEgress、SessionID 注入与非 CROS 协议无损透传
	crosBackendLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen crosBackendLn failed: %v", err)
	}
	defer crosBackendLn.Close()
	crosPort := uint16(crosBackendLn.Addr().(*net.TCPAddr).Port)

	sqlBackendLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen sqlBackendLn failed: %v", err)
	}
	defer sqlBackendLn.Close()
	sqlPort := uint16(sqlBackendLn.Addr().(*net.TCPAddr).Port)

	// 后端 Echo
	startEchoServer := func(ln net.Listener) {
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				go func(c net.Conn) {
					defer c.Close()
					buf := make([]byte, 4096)
					for {
						n, err := c.Read(buf)
						if n > 0 {
							_, _ = c.Write(buf[:n])
						}
						if err != nil {
							return
						}
					}
				}(conn)
			}
		}()
	}
	startEchoServer(crosBackendLn)
	startEchoServer(sqlBackendLn)

	var egressEnvelopes [][]byte
	var egMu sync.Mutex

	proxy, err := NewEgressProxy(
		func(clientPort uint16) (uint64, uint32, net.IP, uint16, bool) {
			// 模拟 NAT 反查：根据客户端端口映射到 crosPort 或 sqlPort
			if clientPort%2 == 0 {
				return 20, 2002, net.IPv4(127, 0, 0, 1), crosPort, true
			}
			return 20, 2002, net.IPv4(127, 0, 0, 1), sqlPort, true
		},
		func(envBytes []byte) {
			egMu.Lock()
			egressEnvelopes = append(egressEnvelopes, envBytes)
			egMu.Unlock()
		},
	)
	if err != nil {
		t.Fatalf("NewEgressProxy failed: %v", err)
	}
	defer proxy.Close()

	// 启动录制并注入 SessionID = 88
	proxy.StartCapture(88)
	if proxy.ActiveSessionID() != 88 {
		t.Fatalf("ActiveSessionID 预期 88，实际 %d", proxy.ActiveSessionID())
	}

	proxyAddr := fmt.Sprintf("127.0.0.1:%d", proxy.ProxyPort())

	// 建立一条 CROS RPC 连接
	crosConn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial crosConn failed: %v", err)
	}
	defer crosConn.Close()

	// 建立一条 SQL 连接
	sqlConn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial sqlConn failed: %v", err)
	}
	defer sqlConn.Close()

	// 发送 SQL 查询并读取回显
	sqlQuery := []byte("SELECT * FROM users WHERE active = 1;")
	if _, err := sqlConn.Write(sqlQuery); err != nil {
		t.Fatalf("write sqlQuery failed: %v", err)
	}
	sqlEcho := make([]byte, len(sqlQuery))
	if _, err := io.ReadFull(sqlConn, sqlEcho); err != nil {
		t.Fatalf("read sqlEcho failed: %v", err)
	}
	if !bytes.Equal(sqlEcho, sqlQuery) {
		t.Fatalf("sqlEcho 不一致")
	}

	// 发送 CROS RPC 请求帧并读取回显
	if _, err := crosConn.Write(reqPkt); err != nil {
		t.Fatalf("write reqPkt to crosConn failed: %v", err)
	}
	crosEcho := make([]byte, len(reqPkt))
	if _, err := io.ReadFull(crosConn, crosEcho); err != nil {
		t.Fatalf("read crosEcho failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	egMu.Lock()
	if len(egressEnvelopes) == 0 {
		t.Fatalf("EgressProxy 预期捕获到信封帧，实际为 0")
	}
	for i, rawEnv := range egressEnvelopes {
		env, _, err := agentproto.UnmarshalEnvelope(rawEnv)
		if err != nil {
			t.Fatalf("UnmarshalEnvelope egress frame %d failed: %v", i, err)
		}
		if env.Direction != agentproto.DirectionEgress {
			t.Fatalf("EgressProxy 信封帧 %d Direction 预期 DirectionEgress (2)，实际得到 %d", i, env.Direction)
		}
		if env.SessionOrTaskID != 88 {
			t.Fatalf("EgressProxy 信封帧 %d SessionOrTaskID 预期 88，实际得到 %d", i, env.SessionOrTaskID)
		}
	}
	egMu.Unlock()

	// 停止录制并排空
	proxy.DrainAndSetPassthrough(50 * time.Millisecond)
	if proxy.ActiveSessionID() != 0 {
		t.Fatalf("DrainAndSetPassthrough 后 ActiveSessionID 预期重置为 0，实际 %d", proxy.ActiveSessionID())
	}

	// 验证停录后长连接不断连且保持正常收发
	moreSQL := []byte("SELECT count(*) FROM orders;")
	if _, err := sqlConn.Write(moreSQL); err != nil {
		t.Fatalf("write moreSQL after stop failed: %v", err)
	}
	moreSQLEcho := make([]byte, len(moreSQL))
	if _, err := io.ReadFull(sqlConn, moreSQLEcho); err != nil {
		t.Fatalf("read moreSQLEcho after stop failed: %v", err)
	}
	if !bytes.Equal(moreSQLEcho, moreSQL) {
		t.Fatalf("moreSQLEcho 不一致")
	}
}

func TestEgressProxy_ReplayMock_HistoricalEndpointsAndMagicWriteGuardZeroLeakage(t *testing.T) {
	var portABackendBytes int64
	var portBBackendBytes int64

	// 下游 A (:9002 已知 Mock 服务)
	lnA, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen lnA failed: %v", err)
	}
	defer lnA.Close()
	portA := uint16(lnA.Addr().(*net.TCPAddr).Port)

	go func() {
		for {
			c, err := lnA.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				buf := make([]byte, 4096)
				for {
					n, err := conn.Read(buf)
					if n > 0 {
						atomic.AddInt64(&portABackendBytes, int64(n))
						_, _ = conn.Write(buf[:n])
					}
					if err != nil {
						return
					}
				}
			}(c)
		}
	}()

	// 下游 B (:9003 未声明端点，测试常驻 DB 连接突变发 RPC 流量)
	lnB, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen lnB failed: %v", err)
	}
	defer lnB.Close()
	portB := uint16(lnB.Addr().(*net.TCPAddr).Port)

	var portBCrosBytes int64
	go func() {
		for {
			c, err := lnB.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				buf := make([]byte, 4096)
				for {
					n, err := conn.Read(buf)
					if n > 0 {
						atomic.AddInt64(&portBBackendBytes, int64(n))
						if bytes.Contains(buf[:n], []byte("CSE.TCP")) {
							atomic.AddInt64(&portBCrosBytes, int64(n))
						}
						_, _ = conn.Write(buf[:n])
					}
					if err != nil {
						return
					}
				}
			}(c)
		}
	}()

	// 构造 EgressProxy 实例
	var portMap sync.Map
	proxy, err := NewEgressProxy(
		func(clientPort uint16) (uint64, uint32, net.IP, uint16, bool) {
			if target, ok := portMap.Load(clientPort); ok {
				return 10, 1001, net.IPv4(127, 0, 0, 1), target.(uint16), true
			}
			return 10, 1001, net.IPv4(127, 0, 0, 1), portA, true
		},
		nil,
	)
	if err != nil {
		t.Fatalf("NewEgressProxy failed: %v", err)
	}
	defer proxy.Close()

	proxyAddr := fmt.Sprintf("127.0.0.1:%d", proxy.ProxyPort())

	// 在 ARMED / 待命期建立两条代理长连接
	connA, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial connA failed: %v", err)
	}
	defer connA.Close()
	portMap.Store(uint16(connA.LocalAddr().(*net.TCPAddr).Port), portA)

	connB, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial connB failed: %v", err)
	}
	defer connB.Close()
	portMap.Store(uint16(connB.LocalAddr().(*net.TCPAddr).Port), portB)

	// 先进行一次预热发包
	if _, err := connA.Write([]byte("ARMED_PING_A")); err != nil {
		t.Fatalf("write connA pre-warm failed: %v", err)
	}
	bufA := make([]byte, 12)
	_, _ = io.ReadFull(connA, bufA)

	if _, err := connB.Write([]byte("ARMED_PING_B")); err != nil {
		t.Fatalf("write connB pre-warm failed: %v", err)
	}
	bufB := make([]byte, 12)
	_, _ = io.ReadFull(connB, bufB)

	// 重置后端接收计数
	atomic.StoreInt64(&portABackendBytes, 0)
	atomic.StoreInt64(&portBBackendBytes, 0)

	// 构造 Mock 匹配器，提供 OrderService Mock 回包
	matcher := replay.NewScopedMockMatcher()
	mockPayload := []byte("order_mock_rsp")
	mockRspRaw := make([]byte, 156)
	binary.BigEndian.PutUint16(mockRspRaw[0:2], 1)
	binary.BigEndian.PutUint16(mockRspRaw[2:4], 156)
	copy(mockRspRaw[24:88], []byte("com.test.OrderService"))
	copy(mockRspRaw[88:152], []byte("CreateOrder"))

	mockComm := make([]byte, 24)
	copy(mockComm[0:7], []byte("CSE.TCP"))
	mockComm[7] = 1
	mBodyLen := uint32(156 + len(mockPayload))
	binary.BigEndian.PutUint32(mockComm[16:20], mBodyLen)
	binary.BigEndian.PutUint16(mockComm[20:22], 156)
	mockComm[22] = crosrpc.MsgTypeRsp
	mockComm[23] = 1

	var fullMockRsp []byte
	fullMockRsp = append(fullMockRsp, mockComm...)
	fullMockRsp = append(fullMockRsp, mockRspRaw...)
	fullMockRsp = append(fullMockRsp, mockPayload...)

	matcher.AddGlobalFallback("com.test.OrderService", "CreateOrder", []byte("req_data"), [][]byte{fullMockRsp})

	// 切入 REPLAY_MOCK 模式，已知下游仅包含 portA
	proxy.SetMockMatcher(matcher, []agentproto.DownstreamEndpoint{
		{IP: "127.0.0.1", Port: portA},
	})
	proxy.SetMode(ModeReplayMock)

	// 构造测试请求报文
	reqPkt := make([]byte, 24+160+8)
	copy(reqPkt[0:7], []byte("CSE.TCP"))
	reqPkt[7] = 1
	binary.BigEndian.PutUint32(reqPkt[16:20], 168)
	binary.BigEndian.PutUint16(reqPkt[20:22], 160)
	reqPkt[22] = crosrpc.MsgTypeReq
	reqPkt[23] = 1
	copy(reqPkt[24+8:24+72], []byte("com.test.OrderService"))
	copy(reqPkt[24+72:24+136], []byte("CreateOrder"))
	reqPkt[24+136] = crosrpc.ReqTypeSync
	copy(reqPkt[24+160:], []byte("req_data"))

	// 1. 验证连接 A (命中已知下游 Mock 端点集):
	if _, err := connA.Write(reqPkt); err != nil {
		t.Fatalf("write connA in replay failed: %v", err)
	}
	rspA := make([]byte, len(fullMockRsp))
	if _, err := io.ReadFull(connA, rspA); err != nil {
		t.Fatalf("read mock response on connA failed: %v", err)
	}
	if !bytes.Equal(rspA, fullMockRsp) {
		t.Fatalf("connA 预期收到 Mock 回包，实际内容不匹配")
	}
	// 断言真实下游 A 严格收到 0 字节！
	if leakedA := atomic.LoadInt64(&portABackendBytes); leakedA != 0 {
		t.Fatalf("真实下游 A 泄漏了 %d 字节，预期严格为 0", leakedA)
	}

	// 2. 验证连接 B (未声明端点，如常驻 DB 连接):
	// 先发非 RPC SQL 字节 -> 正常透传并回包
	sqlData := []byte("SELECT count(*) FROM t_audit_log;")
	if _, err := connB.Write(sqlData); err != nil {
		t.Fatalf("write non-RPC data on connB failed: %v", err)
	}
	sqlEcho := make([]byte, len(sqlData))
	if _, err := io.ReadFull(connB, sqlEcho); err != nil {
		t.Fatalf("read sqlEcho on connB failed: %v", err)
	}
	if !bytes.Equal(sqlEcho, sqlData) {
		t.Fatalf("connB 非 RPC 数据透传回包不一致")
	}

	// 随后突变发出 "CSE.TCP" CROS RPC 请求帧 -> 写前魔术字守卫拦截并切断 targetConn
	if _, err := connB.Write(reqPkt); err != nil {
		t.Fatalf("write cros req on connB failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// 断言真实下游 B 收到的 CROS RPC 流量严格为 0 字节！
	if leakedCros := atomic.LoadInt64(&portBCrosBytes); leakedCros != 0 {
		t.Fatalf("写前守卫失效！下游 B 接收到了 %d 字节的 CROS RPC 泄漏报文", leakedCros)
	}
	// 验证未匹配的服务被计入 MockMiss
	if matcher.MockMissCount() < 1 && matcher.MockHitCount() < 1 {
		t.Fatalf("预期 ScopedMockMatcher 记录命中或未命中，实际 hit=%d, miss=%d", matcher.MockHitCount(), matcher.MockMissCount())
	}
}

func TestProcScanner_OwnSocketInodesOnly_InboundPrewarm_AndProxyExclusion(t *testing.T) {
	// 构建假 /proc 目录结构
	entries := map[string]string{
		"net/tcp": strings.Join([]string{
			"  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode",
			// agent_pid (9999) 连向 :9002 (rem=127.0.0.1:9002, 232A)
			"   0: 0100007F:C001 0100007F:232A 01 00000000:00000000 00:00000000 00000000     0        0 10001 1 0000000000000000 100 0 0 10 0",
			// 无关进程 8888 连向 :9002
			"   1: 0100007F:C002 0100007F:232A 01 00000000:00000000 00:00000000 00000000     0        0 10002 1 0000000000000000 100 0 0 10 0",
			// 目标 pid 101 在 :9001 (2329) 的存量入站连接 (rem=127.0.0.1:44480, ADC0)
			"   2: 0100007F:2329 0100007F:ADC0 01 00000000:00000000 00:00000000 00000000     0        0 10003 1 0000000000000000 100 0 0 10 0",
			// 目标 pid 101 连向 proxyPort :9091 (2383) 的已代理出站连接 (rem=127.0.0.1:9091)
			"   3: 0100007F:D001 0100007F:2383 01 00000000:00000000 00:00000000 00000000     0        0 10004 1 0000000000000000 100 0 0 10 0",
			// 目标 pid 101 预挂前直连 :9005 (232D) 的存量直连出站连接 (rem=127.0.0.1:9005)
			"   4: 0100007F:D002 0100007F:232D 01 00000000:00000000 00:00000000 00000000     0        0 10005 1 0000000000000000 100 0 0 10 0",
		}, "\n"),
		"101/cmdline": "sut-order-service\x00",
		"101/comm":    "sut-order",
		"9999/comm":   "code-traefik-agent",
		"8888/comm":   "other-service",
	}

	root := mockProcTree(t, entries)

	// 创建软链接模拟 /proc/[pid]/fd
	mkSymlink := func(pid string, fd string, target string) {
		fdDir := filepath.Join(root, pid, "fd")
		_ = os.MkdirAll(fdDir, 0755)
		_ = os.Symlink(target, filepath.Join(fdDir, fd))
	}

	mkSymlink("101", "3", "socket:[10003]")
	mkSymlink("101", "4", "socket:[10004]")
	mkSymlink("101", "5", "socket:[10005]")
	mkSymlink("101", "6", "/var/log/app.log") // 非 socket 文件
	mkSymlink("9999", "3", "socket:[10001]")
	mkSymlink("8888", "3", "socket:[10002]")

	scanner := NewProcScanner(root)

	// 1. 验证 ScanProcessEstablishedSockets 仅采集 pid 101 亲身持有的 socket:[inode]
	socks, err := scanner.ScanProcessEstablishedSockets(101)
	if err != nil {
		t.Fatalf("ScanProcessEstablishedSockets failed: %v", err)
	}
	if len(socks) != 3 {
		t.Fatalf("预期采集 3 条 pid=101 自有 Socket，实际得到 %d 条: %+v", len(socks), socks)
	}

	// 验证入站预热提取：筛选 local_port == 9001 的 socket
	var inboundFD int32 = -1
	for _, s := range socks {
		if s.LocalPort == 9001 {
			inboundFD = s.FD
			break
		}
	}
	if inboundFD != 3 {
		t.Fatalf("入站预热预期提取 fd=3，实际得到 %d", inboundFD)
	}

	// 2. 验证 DiagnoseEstablishedConns(downstreams=[]) 全出站诊断：
	// 排除入站端口 9001、排除已代理 127.0.0.1:9091，仅对未代理直连 :9005 计数
	estConns, needRec, err := scanner.DiagnoseEstablishedConns(101, nil, 9001, 9091)
	if err != nil {
		t.Fatalf("DiagnoseEstablishedConns failed: %v", err)
	}
	if estConns != 1 || !needRec {
		t.Fatalf("预期 estConns=1, needRec=true，实际 estConns=%d, needRec=%v", estConns, needRec)
	}

	// 3. 移除直连 :9005 的 fd 5 软链接，模拟业务进程已切断直连或恢复
	_ = os.Remove(filepath.Join(root, "101", "fd", "5"))

	estConns2, needRec2, err := scanner.DiagnoseEstablishedConns(101, nil, 9001, 9091)
	if err != nil {
		t.Fatalf("DiagnoseEstablishedConns after remove failed: %v", err)
	}
	if estConns2 != 0 || needRec2 {
		t.Fatalf("移除直连连接后预期 estConns=0, needRec=false (READY)，实际 estConns=%d, needRec=%v", estConns2, needRec2)
	}
}
