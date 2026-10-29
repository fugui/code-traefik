package crosrpc

import (
	"testing"
)

func FuzzStreamFramer(f *testing.F) {
	// 添加种子语料
	p1 := buildValidReqPacket("pkg-TestSvc", "Op1", []byte("fuzz-seed-1"))
	p2 := buildValidReqPacket("pkg-TestSvc", "Op2", []byte("fuzz-seed-2"))
	f.Add(p1)
	f.Add(append(p1, p2...))
	f.Add([]byte("random_garbage_prefix_data_1234567890"))
	f.Add(append([]byte("dirty_bytes_prefix"), p1...))
	f.Add(buildCommHeadBytes(MsgTypeReq, 160, 160))

	f.Fuzz(func(t *testing.T, data []byte) {
		framer := NewStreamFramer(64 * 1024) // 限制 fuzz 内存分配
		frames, err := framer.Write(data)
		if err != nil {
			return
		}
		for _, frame := range frames {
			if len(frame.Data) > 64*1024 {
				t.Fatalf("单帧大小突破 MaxCaptureBytes: %d", len(frame.Data))
			}
			// 验证 ParsePacket 稳健性，零越界 panic
			_, _ = ParsePacket(frame.Data)
		}
	})
}
