package crosrpc

import (
	"encoding/binary"
	"testing"
)

// T-M13-01: V03 (188B) 与 V04 (200B) 应答帧 ServerRecvTime (40..47) 与 ServerSendTime (48..55) 解析，
// 且 V01 (156B) / V02 (164B) 返回 0 无越界。
func TestSessionHead_V03_V04_ServerTimingAndBackwardsCompatibility(t *testing.T) {
	// 1. 测试 V03 (188B)
	v03Buf := make([]byte, 188)
	binary.BigEndian.PutUint16(v03Buf[0:2], 1)   // MsgStructVer
	binary.BigEndian.PutUint16(v03Buf[2:4], 188) // RspHeadLen
	binary.BigEndian.PutUint32(v03Buf[4:8], 0)   // StatusCode
	copy(v03Buf[8:24], []byte("trace_buff_16b!"))
	binary.BigEndian.PutUint64(v03Buf[32:40], 1700000000) // ClientSendTime
	binary.BigEndian.PutUint64(v03Buf[40:48], 1700000050) // ServerRecvTime
	binary.BigEndian.PutUint64(v03Buf[48:56], 1700000095) // ServerSendTime
	copy(v03Buf[56:120], []byte("dophi.order.v1.TradeService\x00"))
	copy(v03Buf[120:184], []byte("CreateOrder\x00"))
	binary.BigEndian.PutUint16(v03Buf[184:186], 1) // RspMsgNo

	h03, err := ParseRspHead(v03Buf, 188)
	if err != nil {
		t.Fatalf("ParseRspHead V03 failed: %v", err)
	}
	if h03.ServerRecvTime != 1700000050 {
		t.Errorf("V03 ServerRecvTime expected 1700000050, got %d", h03.ServerRecvTime)
	}
	if h03.ServerSendTime != 1700000095 {
		t.Errorf("V03 ServerSendTime expected 1700000095, got %d", h03.ServerSendTime)
	}

	// 2. 测试 V04 (200B)
	v04Buf := make([]byte, 200)
	binary.BigEndian.PutUint16(v04Buf[0:2], 1)   // MsgStructVer
	binary.BigEndian.PutUint16(v04Buf[2:4], 200) // RspHeadLen
	binary.BigEndian.PutUint32(v04Buf[4:8], 0)
	copy(v04Buf[8:24], []byte("trace_buff_16b!"))
	binary.BigEndian.PutUint64(v04Buf[32:40], 1800000000) // ClientSendTime
	binary.BigEndian.PutUint64(v04Buf[40:48], 1800000020) // ServerRecvTime
	binary.BigEndian.PutUint64(v04Buf[48:56], 1800000080) // ServerSendTime
	copy(v04Buf[56:120], []byte("dophi.inventory.v1.StockService\x00"))
	copy(v04Buf[120:184], []byte("LockStock\x00"))
	binary.BigEndian.PutUint64(v04Buf[184:192], 0) // StatusCode 64
	binary.BigEndian.PutUint16(v04Buf[192:194], 1) // RspMsgNo

	h04, err := ParseRspHead(v04Buf, 200)
	if err != nil {
		t.Fatalf("ParseRspHead V04 failed: %v", err)
	}
	if h04.ServerRecvTime != 1800000020 {
		t.Errorf("V04 ServerRecvTime expected 1800000020, got %d", h04.ServerRecvTime)
	}
	if h04.ServerSendTime != 1800000080 {
		t.Errorf("V04 ServerSendTime expected 1800000080, got %d", h04.ServerSendTime)
	}

	// 3. 测试 V01 (156B)
	v01Buf := make([]byte, 156)
	binary.BigEndian.PutUint16(v01Buf[0:2], 1)
	binary.BigEndian.PutUint16(v01Buf[2:4], 156)
	copy(v01Buf[24:88], []byte("dophi.v01.Service\x00"))
	copy(v01Buf[88:152], []byte("Op\x00"))

	h01, err := ParseRspHead(v01Buf, 156)
	if err != nil {
		t.Fatalf("ParseRspHead V01 failed: %v", err)
	}
	if h01.ServerRecvTime != 0 || h01.ServerSendTime != 0 {
		t.Errorf("V01 expected ServerRecvTime and ServerSendTime == 0, got %d, %d", h01.ServerRecvTime, h01.ServerSendTime)
	}

	// 4. 测试 V02 (164B)
	v02Buf := make([]byte, 164)
	binary.BigEndian.PutUint16(v02Buf[0:2], 1)
	binary.BigEndian.PutUint16(v02Buf[2:4], 164)
	copy(v02Buf[32:96], []byte("dophi.v02.Service\x00"))
	copy(v02Buf[96:160], []byte("Op\x00"))

	h02, err := ParseRspHead(v02Buf, 164)
	if err != nil {
		t.Fatalf("ParseRspHead V02 failed: %v", err)
	}
	if h02.ServerRecvTime != 0 || h02.ServerSendTime != 0 {
		t.Errorf("V02 expected ServerRecvTime and ServerSendTime == 0, got %d, %d", h02.ServerRecvTime, h02.ServerSendTime)
	}
}
