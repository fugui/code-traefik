package main

import (
	"os/exec"
	"strings"
	"testing"
)

// T-M13-10: 数据面 Agent 物理防腐红线依赖核验
// 断言 cmd/agent, pkg/crosrpc, pkg/agentproto, pkg/ebpf, pkg/replay 零 gorm、零 pgx、零 dynamicpb、零 internal/server 依赖
func TestAgentDependencies_NoGormNoDynamicPBNoServer(t *testing.T) {
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
		"github.com/go-git/go-git",
		"github.com/go-git/go-billy",
		"github.com/bufbuild/protocompile",
		"sqlite",
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
