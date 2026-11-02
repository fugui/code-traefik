package agentproto

import (
	"testing"
)

func TestComputeArmedDigest(t *testing.T) {
	// 空列表返回空串
	if digest := ComputeArmedDigest(nil); digest != "" {
		t.Fatalf("expected empty digest for nil, got %s", digest)
	}
	if digest := ComputeArmedDigest([]ProcessMonitorSpec{}); digest != "" {
		t.Fatalf("expected empty digest for empty slice, got %s", digest)
	}

	// 顺序无关测试：按 ProcessID 升序排序确定性
	specs1 := []ProcessMonitorSpec{
		{
			ProcessID:  2,
			ListenPort: 8081,
			CmdPattern: "order-svc",
			Downstreams: []DownstreamEndpoint{
				{IP: "127.0.0.1", Port: 9002},
				{IP: "192.168.1.1", Port: 9001},
			},
		},
		{
			ProcessID:  1,
			ListenPort: 8080,
			CmdPattern: "account-svc",
			Downstreams: []DownstreamEndpoint{
				{IP: "127.0.0.1", Port: 9000},
			},
		},
	}

	specs2 := []ProcessMonitorSpec{
		{
			ProcessID:  1,
			ListenPort: 8080,
			CmdPattern: "account-svc",
			Downstreams: []DownstreamEndpoint{
				{IP: "127.0.0.1", Port: 9000},
			},
		},
		{
			ProcessID:  2,
			ListenPort: 8081,
			CmdPattern: "order-svc",
			Downstreams: []DownstreamEndpoint{
				{IP: "192.168.1.1", Port: 9001},
				{IP: "127.0.0.1", Port: 9002},
			},
		},
	}

	d1 := ComputeArmedDigest(specs1)
	d2 := ComputeArmedDigest(specs2)

	if d1 == "" || d2 == "" {
		t.Fatalf("digest should not be empty")
	}
	if d1 != d2 {
		t.Fatalf("digest should be deterministic and order-independent: %s != %s", d1, d2)
	}

	// 修改任意字段哈希发生改变
	specs3 := []ProcessMonitorSpec{
		{
			ProcessID:  1,
			ListenPort: 8080,
			CmdPattern: "account-svc-modified",
		},
	}
	d3 := ComputeArmedDigest(specs3)
	if d3 == d1 {
		t.Fatalf("modified spec must yield different digest")
	}
}
