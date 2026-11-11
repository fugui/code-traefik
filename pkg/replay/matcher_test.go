package replay

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"

	"code-traefik/pkg/agentproto"
	"code-traefik/pkg/crosrpc"
)

// TestScopedMockMatcher_DstTagIsolation 验证多实现契约下同一接口在不同目的端点 (DstTag) 之间精确隔离绝不串用 (ADR-16, M20)
func TestScopedMockMatcher_DstTagIsolation(t *testing.T) {
	matcher := NewScopedMockMatcher()
	matcher.SetActiveRecordID(100)

	// 设置网络端点到进程 DstTag 映射
	matcher.SetEndpointTags(map[string]string{
		"10.0.0.2:9001": "proc_b",
		"10.0.0.3:9001": "proc_c",
	})

	traceBuff := [16]byte{1, 2, 3}
	reqB := buildTestReqPacket(1, "demo.v1.OrderProcessor", "ProcessOrder", crosrpc.ReqTypeSync, traceBuff, 1000, []byte(`{"order_id":"001"}`))
	rspB := buildTestRspPacket(1, crosrpc.MsgTypeRsp, 156, "demo.v1.OrderProcessor", "ProcessOrder", 0, []byte("from_B"))

	reqC := buildTestReqPacket(2, "demo.v1.OrderProcessor", "ProcessOrder", crosrpc.ReqTypeSync, traceBuff, 1000, []byte(`{"order_id":"001"}`))
	rspC := buildTestRspPacket(2, crosrpc.MsgTypeRsp, 156, "demo.v1.OrderProcessor", "ProcessOrder", 0, []byte("from_C"))

	// 为同一入站 recordID=100 登记两个不同 DstTag 的 Mock
	err := matcher.RegisterScopedDownstreamRule(100, agentproto.MockRuleDownstream{
		ReqPacket:  reqB,
		RspPackets: [][]byte{rspB},
		DstTag:     "proc_b",
	})
	if err != nil {
		t.Fatalf("RegisterScopedDownstreamRule for proc_b failed: %v", err)
	}

	err = matcher.RegisterScopedDownstreamRule(100, agentproto.MockRuleDownstream{
		ReqPacket:  reqC,
		RspPackets: [][]byte{rspC},
		DstTag:     "proc_c",
	})
	if err != nil {
		t.Fatalf("RegisterScopedDownstreamRule for proc_c failed: %v", err)
	}

	// 1. 发往 10.0.0.2:9001 (ProcB) -> 必须精准命中 proc_b 并返回 from_B
	liveReqB := buildTestReqPacket(991, "demo.v1.OrderProcessor", "ProcessOrder", crosrpc.ReqTypeSync, traceBuff, 2000, []byte(`{"order_id":"001"}`))
	resB, err := matcher.MatchAndHealWithDst(liveReqB, "10.0.0.2:9001")
	if err != nil {
		t.Fatalf("MatchAndHealWithDst for ProcB failed: %v", err)
	}
	if resB.Result != MatchHitScoped {
		t.Fatalf("expected MatchHitScoped, got %v", resB.Result)
	}
	if len(resB.ResponseFrames) != 1 {
		t.Fatalf("expected 1 response frame, got %d", len(resB.ResponseFrames))
	}
	if !bytes.Contains(resB.ResponseFrames[0], []byte("from_B")) {
		t.Errorf("expected response containing 'from_B', got: %s", string(resB.ResponseFrames[0]))
	}

	// 2. 发往 10.0.0.3:9001 (ProcC) -> 必须精准命中 proc_c 并返回 from_C
	liveReqC := buildTestReqPacket(992, "demo.v1.OrderProcessor", "ProcessOrder", crosrpc.ReqTypeSync, traceBuff, 2000, []byte(`{"order_id":"001"}`))
	resC, err := matcher.MatchAndHealWithDst(liveReqC, "10.0.0.3:9001")
	if err != nil {
		t.Fatalf("MatchAndHealWithDst for ProcC failed: %v", err)
	}
	if resC.Result != MatchHitScoped {
		t.Fatalf("expected MatchHitScoped, got %v", resC.Result)
	}
	if len(resC.ResponseFrames) != 1 {
		t.Fatalf("expected 1 response frame, got %d", len(resC.ResponseFrames))
	}
	if !bytes.Contains(resC.ResponseFrames[0], []byte("from_C")) {
		t.Errorf("expected response containing 'from_C', got: %s", string(resC.ResponseFrames[0]))
	}

	// 3. 发往 10.0.0.4:9001 (未登记进程) -> 必须返回 MOCK_MISS，0 字节响应，绝不串用
	liveReqD := buildTestReqPacket(993, "demo.v1.OrderProcessor", "ProcessOrder", crosrpc.ReqTypeSync, traceBuff, 2000, []byte(`{"order_id":"001"}`))
	resD, err := matcher.MatchAndHealWithDst(liveReqD, "10.0.0.4:9001")
	if err != nil {
		t.Fatalf("MatchAndHealWithDst for ProcD failed: %v", err)
	}
	if resD.Result != MatchMiss {
		t.Errorf("expected MatchMiss for unregistered destination, got %v", resD.Result)
	}
	if len(resD.ResponseFrames) != 0 {
		t.Errorf("expected 0 response frames on MOCK_MISS, got %d", len(resD.ResponseFrames))
	}
}

// TestScopedMockMatcher_DstTagFallbackToUntagged 验证未携带 DstTag 的存量独占 Mock 规则平滑回退，保持向后兼容 (M20)
func TestScopedMockMatcher_DstTagFallbackToUntagged(t *testing.T) {
	matcher := NewScopedMockMatcher()
	matcher.SetActiveRecordID(200)

	matcher.SetEndpointTags(map[string]string{
		"10.0.0.2:9001": "proc_b",
		"10.0.0.3:9001": "proc_c",
	})

	traceBuff := [16]byte{1, 2, 3}
	reqLegacy := buildTestReqPacket(1, "demo.v1.LegacyService", "DoLegacy", crosrpc.ReqTypeSync, traceBuff, 1000, []byte(`{"k":"v"}`))
	rspLegacy := buildTestRspPacket(1, crosrpc.MsgTypeRsp, 156, "demo.v1.LegacyService", "DoLegacy", 0, []byte("from_legacy"))

	// 录入存量无标签 Mock (DstTag == "")
	err := matcher.RegisterScopedDownstreamRule(200, agentproto.MockRuleDownstream{
		ReqPacket:  reqLegacy,
		RspPackets: [][]byte{rspLegacy},
		DstTag:     "",
	})
	if err != nil {
		t.Fatalf("RegisterScopedDownstreamRule failed: %v", err)
	}

	// 请求打向带 DstTag 映射的端点 10.0.0.2:9001 -> 自动回退匹配到无标签规则
	liveReq := buildTestReqPacket(881, "demo.v1.LegacyService", "DoLegacy", crosrpc.ReqTypeSync, traceBuff, 2000, []byte(`{"k":"v"}`))
	res, err := matcher.MatchAndHealWithDst(liveReq, "10.0.0.2:9001")
	if err != nil {
		t.Fatalf("MatchAndHealWithDst failed: %v", err)
	}
	if res.Result != MatchHitScoped {
		t.Fatalf("expected MatchHitScoped, got %v", res.Result)
	}
	if !bytes.Contains(res.ResponseFrames[0], []byte("from_legacy")) {
		t.Errorf("expected response containing 'from_legacy', got: %s", string(res.ResponseFrames[0]))
	}
}

// TestAgentBinaryZeroDBAndZeroPBDependency 验证数据面 Agent 及其依赖库零数据库、零 dynamicpb 物理防腐红线 (AC-M20-02)
func TestAgentBinaryZeroDBAndZeroPBDependency(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "./cmd/agent", "./pkg/crosrpc", "./pkg/agentproto", "./pkg/ebpf", "./pkg/replay")
	cmd.Dir = "../.." // code-traefik root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps failed: %v, out: %s", err, string(out))
	}

	forbiddenKeywords := []string{
		"gorm.io",
		"pgx",
		"dynamicpb",
		"protoregistry",
		"code-traefik/internal/server",
		"code-traefik/models",
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		for _, kw := range forbiddenKeywords {
			if strings.Contains(trimmed, kw) {
				t.Errorf("Agent component forbidden dependency detected: %s (matched rule: %s)", trimmed, kw)
			}
		}
	}
}
