package crosrpc

import (
	"encoding/binary"
	"testing"
)

// T-M13-02: 流式分帧首字节定戳 (WriteAt & StampedFrame)
// 将 1 帧大请求拆为 3 个分片分别在 t1=100, t2=200, t3=350 调用 WriteAt，断言凑齐输出的 StampedFrame.CapturedAtNs == 100（首字节到达时刻）；
// 粘包双帧分片正确继承分片时刻。
func TestFramer_WriteAt_FirstByteArrivalTimestampAndCoalesced(t *testing.T) {
	framer := NewStreamFramer(8192)

	// 构造 1 帧请求：24B 公共头 + 160B 会话头 + 100B 载荷 = 284 字节
	totalPayloadAndSession := 260
	reqPacket := make([]byte, CommHeadLen+totalPayloadAndSession)
	copy(reqPacket[0:7], []byte(MagicPrefix))
	reqPacket[7] = 0                                  // ucProtocol
	binary.BigEndian.PutUint64(reqPacket[8:16], 9999) // SNO
	binary.BigEndian.PutUint32(reqPacket[16:20], uint32(totalPayloadAndSession))
	binary.BigEndian.PutUint16(reqPacket[20:22], 160) // usHeadLen
	reqPacket[22] = MsgTypeReq
	reqPacket[23] = 0

	// 拆为 3 个分片: chunk1 (50B), chunk2 (100B), chunk3 (134B)
	chunk1 := reqPacket[:50]
	chunk2 := reqPacket[50:150]
	chunk3 := reqPacket[150:]

	// t1 = 100
	frames1 := framer.WriteAt(chunk1, 100)
	if len(frames1) != 0 {
		t.Fatalf("chunk1 should not produce complete frame, got %d", len(frames1))
	}

	// t2 = 200
	frames2 := framer.WriteAt(chunk2, 200)
	if len(frames2) != 0 {
		t.Fatalf("chunk2 should not produce complete frame, got %d", len(frames2))
	}

	// t3 = 350
	frames3 := framer.WriteAt(chunk3, 350)
	if len(frames3) != 1 {
		t.Fatalf("chunk3 should complete frame, got %d", len(frames3))
	}

	sf := frames3[0]
	if sf.CapturedAtNs != 100 {
		t.Errorf("expected CapturedAtNs == 100 (first byte arrival ns), got %d", sf.CapturedAtNs)
	}
	if len(sf.Data) != len(reqPacket) {
		t.Errorf("expected frame length %d, got %d", len(reqPacket), len(sf.Data))
	}

	// 测试粘包分片：一个 chunk 包含前一帧末尾 0 字节（已完成）+ 新两帧
	// 帧 A (50B) 和 帧 B (60B)
	buildFrame := func(sno uint64, totalBodyLen int) []byte {
		pkt := make([]byte, CommHeadLen+totalBodyLen)
		copy(pkt[0:7], []byte(MagicPrefix))
		pkt[7] = 0
		binary.BigEndian.PutUint64(pkt[8:16], sno)
		binary.BigEndian.PutUint32(pkt[16:20], uint32(totalBodyLen))
		binary.BigEndian.PutUint16(pkt[20:22], uint16(totalBodyLen))
		pkt[22] = MsgTypeReq
		pkt[23] = 0
		return pkt
	}

	frameA := buildFrame(1001, 180) // 204B
	frameB := buildFrame(1002, 190) // 214B

	// 分片 4 在 t4=500 发送 frameA 前 50B
	framer.WriteAt(frameA[:50], 500)
	// 分片 5 在 t5=650 发送 frameA 剩余 154B 以及完整的 frameB (214B)
	combined := append([]byte{}, frameA[50:]...)
	combined = append(combined, frameB...)

	framesCoalesced := framer.WriteAt(combined, 650)
	if len(framesCoalesced) != 2 {
		t.Fatalf("expected 2 frames from coalesced chunk, got %d", len(framesCoalesced))
	}

	// 帧 A 首字节在 t4=500 到达
	if framesCoalesced[0].CapturedAtNs != 500 {
		t.Errorf("frame A CapturedAtNs expected 500, got %d", framesCoalesced[0].CapturedAtNs)
	}
	// 帧 B 首字节在 t5=650 到达
	if framesCoalesced[1].CapturedAtNs != 650 {
		t.Errorf("frame B CapturedAtNs expected 650, got %d", framesCoalesced[1].CapturedAtNs)
	}
}
