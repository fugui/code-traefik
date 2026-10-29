package crosframe

import (
	"encoding/binary"

	"code-traefik/pkg/crosrpc"
)

// BuildReqPacket 拼装符合 DOPHI CROS RPC 规范的完整请求报文帧
func BuildReqPacket(sno uint64, serviceName, operation string, trace *crosrpc.TraceKeyExt, payload []byte) []byte {
	sessionHeadLen := 160
	rawSession := make([]byte, sessionHeadLen)
	binary.BigEndian.PutUint16(rawSession[0:2], 1)   // usMsgStructVer = 1
	binary.BigEndian.PutUint16(rawSession[2:4], 160) // usReqHeadLen = 160
	binary.BigEndian.PutUint32(rawSession[4:8], 0)   // uiDst2ndAddrId = 0

	// 写入 ascServiceName (64B)
	copy(rawSession[8:72], []byte(serviceName))
	// 写入 ascOperation (64B)
	copy(rawSession[72:136], []byte(operation))
	rawSession[136] = crosrpc.ReqTypeSync              // ucReqType = 1 (同步)
	rawSession[137] = 0                                // ucSubjectType = 0
	binary.BigEndian.PutUint16(rawSession[138:140], 1) // usRpcVer = 1
	binary.BigEndian.PutUint32(rawSession[140:144], 0) // uiSessionId = 0

	// 可选扩展头: TraceKey
	var extHeadBytes []byte
	if trace != nil {
		// 同时拷贝 16B 到 TraceBuff 便于兼容
		binary.BigEndian.PutUint64(rawSession[144:152], trace.TraceID)
		binary.BigEndian.PutUint64(rawSession[152:160], trace.SpanID)

		// 扩展头: 4B 公共头 + 32B 内容 (8 字节对齐)
		ext := make([]byte, crosrpc.ExtendCommHeadLen+32)
		binary.BigEndian.PutUint16(ext[0:2], 32)
		ext[2] = crosrpc.ExtTypeTraceKey
		ext[3] = 0 // ucReserved

		content := ext[crosrpc.ExtendCommHeadLen:]
		binary.BigEndian.PutUint64(content[0:8], trace.TraceID)
		binary.BigEndian.PutUint64(content[8:16], trace.SpanID)
		binary.BigEndian.PutUint64(content[16:24], trace.FatherSpanID)
		content[24] = trace.RecordSwitch
		// 25..32 padding 0

		extHeadBytes = ext
	}

	totalHeadLen := uint16(sessionHeadLen + len(extHeadBytes))
	totalLength := uint32(int(totalHeadLen) + len(payload))

	// 公共头 (24 字节)
	comm := crosrpc.CommHead{
		TransportProto: crosrpc.TransportTCP,
		SNO:            sno,
		Length:         totalLength,
		HeadLen:        totalHeadLen,
		MsgType:        crosrpc.MsgTypeReq,
		IsPacked:       1,
	}
	copy(comm.Magic[:], []byte(crosrpc.MagicPrefix))

	pkt := make([]byte, 0, crosrpc.CommHeadLen+int(totalHeadLen)+len(payload))
	pkt = append(pkt, comm.Marshal()...)
	pkt = append(pkt, rawSession...)
	if len(extHeadBytes) > 0 {
		pkt = append(pkt, extHeadBytes...)
	}
	pkt = append(pkt, payload...)

	return pkt
}

// BuildRspPacket 拼装符合 DOPHI CROS RPC 规范的完整应答报文帧 (V01 布局: 156 字节会话头)
func BuildRspPacket(sno uint64, serviceName, operation string, statusCode uint32, trace *crosrpc.TraceKeyExt, payload []byte) []byte {
	sessionHeadLen := 156
	rawSession := make([]byte, sessionHeadLen)
	binary.BigEndian.PutUint16(rawSession[0:2], 1)          // usMsgStructVer = 1
	binary.BigEndian.PutUint16(rawSession[2:4], 156)        // usRspHeadLen = 156
	binary.BigEndian.PutUint32(rawSession[4:8], statusCode) // uiStatusCode

	if trace != nil {
		binary.BigEndian.PutUint64(rawSession[8:16], trace.TraceID)
		binary.BigEndian.PutUint64(rawSession[16:24], trace.SpanID)
	}

	// 写入 ascServiceName (24..88, 64B)
	copy(rawSession[24:88], []byte(serviceName))
	// 写入 ascOperation (88..152, 64B)
	copy(rawSession[88:152], []byte(operation))
	// 152..154 RspMsgNo = 0
	binary.BigEndian.PutUint16(rawSession[152:154], 0)

	// 可选扩展头: TraceKey
	var extHeadBytes []byte
	if trace != nil {
		ext := make([]byte, crosrpc.ExtendCommHeadLen+32)
		binary.BigEndian.PutUint16(ext[0:2], 32)
		ext[2] = crosrpc.ExtTypeTraceKey
		ext[3] = 0

		content := ext[crosrpc.ExtendCommHeadLen:]
		binary.BigEndian.PutUint64(content[0:8], trace.TraceID)
		binary.BigEndian.PutUint64(content[8:16], trace.SpanID)
		binary.BigEndian.PutUint64(content[16:24], trace.FatherSpanID)
		content[24] = trace.RecordSwitch

		extHeadBytes = ext
	}

	totalHeadLen := uint16(sessionHeadLen + len(extHeadBytes))
	totalLength := uint32(int(totalHeadLen) + len(payload))

	comm := crosrpc.CommHead{
		TransportProto: crosrpc.TransportTCP,
		SNO:            sno,
		Length:         totalLength,
		HeadLen:        totalHeadLen,
		MsgType:        crosrpc.MsgTypeRsp,
		IsPacked:       1,
	}
	copy(comm.Magic[:], []byte(crosrpc.MagicPrefix))

	pkt := make([]byte, 0, crosrpc.CommHeadLen+int(totalHeadLen)+len(payload))
	pkt = append(pkt, comm.Marshal()...)
	pkt = append(pkt, rawSession...)
	if len(extHeadBytes) > 0 {
		pkt = append(pkt, extHeadBytes...)
	}
	pkt = append(pkt, payload...)

	return pkt
}
