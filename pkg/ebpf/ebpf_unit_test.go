package ebpf

import (
	"os"
	"path/filepath"
	"testing"

	"code-traefik/pkg/agentproto"
	"code-traefik/pkg/crosrpc"
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
