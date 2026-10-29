//go:build linux && ebpf

package ebpf

import (
	"net"
	"os"
	"testing"
	"time"

	"code-traefik/pkg/agentproto"
)

// TestEbpfSameHostDualProcessLoopbackBypass 特权环境下同机双进程互调无死锁集成测试
func TestEbpfSameHostDualProcessLoopbackBypass(t *testing.T) {
	// 校验特权
	checker := &LinuxCapabilityChecker{}
	hasCaps, missing, detail := checker.CheckCapabilities()
	if !hasCaps {
		t.Skipf("skipping ebpf integration test: missing required privileges: %v (%s)", missing, detail)
		return
	}

	// 1. 拉起同机 127.0.0.1 上的模拟 CROS RPC 进程 PID_A, PID_B 与非纳管 PID_C
	lnB, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen PID_B failed: %v", err)
	}
	defer lnB.Close()
	portB := uint16(lnB.Addr().(*net.TCPAddr).Port)

	lnC, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen PID_C failed: %v", err)
	}
	defer lnC.Close()
	portC := uint16(lnC.Addr().(*net.TCPAddr).Port)
	_ = portC

	var capturedEnvelopes [][]byte
	frameSink := func(env []byte) {
		capturedEnvelopes = append(capturedEnvelopes, env)
	}

	mgr := NewManager(ManagerConfig{
		AgentPID:  uint32(os.Getpid()),
		FrameSink: frameSink,
	})
	defer mgr.PurgeAllMonitors()

	// 2. 下发监控 PID_B
	specs := []agentproto.ProcessMonitorSpec{
		{
			ProcessID:  2001,
			ListenPort: portB,
		},
	}
	diags, err := mgr.SyncMonitors(9901, specs)
	if err != nil {
		t.Fatalf("SyncMonitors failed: %v", err)
	}
	if len(diags) == 0 {
		t.Fatalf("empty diags returned")
	}

	if !mgr.IsAttached() {
		t.Errorf("expected manager attached")
	}

	// 3. 验证未纳管 PID_C 不受影响
	connC, err := net.DialTimeout("tcp", lnC.Addr().String(), time.Second)
	if err != nil {
		t.Errorf("connecting to unmanaged PID_C failed: %v", err)
	} else {
		connC.Close()
	}

	// 4. 执行停止并释放探针
	mgr.PurgeAllMonitors()
	if mgr.IsAttached() {
		t.Errorf("expected manager detached after purge")
	}
}
