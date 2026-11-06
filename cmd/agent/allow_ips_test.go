package main

import (
	"os"
	"reflect"
	"testing"
)

func TestResolveDefaultAllowIPs(t *testing.T) {
	// 默认状态下包含 127.0.0.1 与现网主控 10.1.64.178
	_ = os.Unsetenv("TRAEFIK_AGENT_ALLOW_IPS")
	gotDefault := resolveDefaultAllowIPs()
	if gotDefault != "127.0.0.1,10.1.64.178" {
		t.Fatalf("expected default %q, got %q", "127.0.0.1,10.1.64.178", gotDefault)
	}

	// 环境变量覆盖
	t.Setenv("TRAEFIK_AGENT_ALLOW_IPS", "192.168.1.100,10.0.0.0/8")
	gotEnv := resolveDefaultAllowIPs()
	if gotEnv != "192.168.1.100,10.0.0.0/8" {
		t.Fatalf("expected env overridden %q, got %q", "192.168.1.100,10.0.0.0/8", gotEnv)
	}
}

func TestParseAllowIPs(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		expected []string
	}{
		{
			name:     "default string",
			raw:      "127.0.0.1,10.1.64.178",
			expected: []string{"127.0.0.1", "10.1.64.178"},
		},
		{
			name:     "extra whitespace and empty items",
			raw:      " 127.0.0.1 , , 10.1.64.178 , 192.168.0.0/16 ",
			expected: []string{"127.0.0.1", "10.1.64.178", "192.168.0.0/16"},
		},
		{
			name:     "empty string",
			raw:      "",
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseAllowIPs(tc.raw)
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("parseAllowIPs(%q) = %v; want %v", tc.raw, got, tc.expected)
			}
		})
	}
}
