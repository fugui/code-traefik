package crosrpc

import (
	"encoding/binary"
)

// ExtendHead 单段扩展头数据结构 (RPC_EXTEND_COMM_HEAD_S + 载荷)
type ExtendHead struct {
	Type uint8  `json:"type"`
	Len  uint16 `json:"len"` // 内容长度（不含 4 字节扩展公共头）
	Raw  []byte `json:"raw"`
}

// TraceKeyExt 调用链扩展头 (RPC_EXTEND_HEAD_TYPE_TRACE_KEY = 0)
type TraceKeyExt struct {
	TraceID      uint64 `json:"trace_id"`
	SpanID       uint64 `json:"span_id"`
	FatherSpanID uint64 `json:"father_span_id"`
	RecordSwitch uint8  `json:"record_switch"`
}

// ParsedPacket 单帧 CROS RPC 协议头完整解析结果
type ParsedPacket struct {
	Comm          CommHead     `json:"comm"`
	Req           *ReqHead     `json:"req,omitempty"`
	Rsp           *RspHead     `json:"rsp,omitempty"`
	Mng           *MngHead     `json:"mng,omitempty"`
	ExtendHeads   []ExtendHead `json:"extend_heads,omitempty"`
	Trace         *TraceKeyExt `json:"trace,omitempty"`
	ServerEnqueue *uint64      `json:"server_enqueue_time,omitempty"`
	PayloadOffset int          `json:"payload_offset"` // 恒等于 24 + int(Comm.HeadLen)
	PayloadLen    int          `json:"payload_len"`    // 实际保留帧中的 Payload 字节长度
	IsTruncated   bool         `json:"is_truncated"`
}

// ParsePacket 解析单帧 CROS RPC 字节切片并提取会话头与全部扩展头
func ParsePacket(frame []byte) (*ParsedPacket, error) {
	if len(frame) < CommHeadLen {
		return nil, ErrBufferTooShort
	}

	comm, err := ParseCommHead(frame[:CommHeadLen])
	if err != nil {
		return nil, err
	}
	if err := ValidateCommHead(comm); err != nil {
		return nil, err
	}

	if len(frame) < CommHeadLen+int(comm.HeadLen) {
		return nil, ErrBufferTooShort
	}

	packet := &ParsedPacket{
		Comm:          comm,
		PayloadOffset: CommHeadLen + int(comm.HeadLen),
		PayloadLen:    len(frame) - (CommHeadLen + int(comm.HeadLen)),
		IsTruncated:   len(frame) < CommHeadLen+int(comm.Length),
	}

	sessionHeadLen := 0
	sessionSlice := frame[CommHeadLen : CommHeadLen+int(comm.HeadLen)]

	switch comm.MsgType {
	case MsgTypeReq:
		req, err := ParseReqHead(sessionSlice)
		if err != nil {
			return nil, err
		}
		packet.Req = req
		sessionHeadLen = int(req.ReqHeadLen)
		if sessionHeadLen < ReqHeadMinLen || sessionHeadLen > int(comm.HeadLen) {
			sessionHeadLen = ReqHeadMinLen
		}

	case MsgTypeRsp, MsgTypeRspSeg:
		// 先尝试从会话头内部读取 usRspHeadLen
		rspLenCandidate := 0
		if len(sessionSlice) >= 4 {
			rspLenCandidate = int(binary.BigEndian.Uint16(sessionSlice[2:4]))
		}
		if _, err := LookupRspLayout(rspLenCandidate); err != nil {
			// 若内嵌长度未匹配，则回退检查整个 comm.HeadLen
			rspLenCandidate = int(comm.HeadLen)
		}

		rsp, err := ParseRspHead(sessionSlice, rspLenCandidate)
		if err != nil {
			return nil, err
		}
		packet.Rsp = rsp
		sessionHeadLen = rspLenCandidate

	case MsgTypeMng, MsgTypeMngReq, MsgTypeUUIDReq:
		mng, err := ParseMngHead(sessionSlice)
		if err != nil {
			return nil, err
		}
		packet.Mng = mng
		sessionHeadLen = MngHeadMinLen
	}

	// 循环解析 0..N 段扩展头 (sessionHeadLen .. comm.HeadLen)
	if int(comm.HeadLen) > sessionHeadLen {
		remaining := sessionSlice[sessionHeadLen:]
		for len(remaining) >= ExtendCommHeadLen {
			extLen := binary.BigEndian.Uint16(remaining[0:2])
			extType := remaining[2]

			// 扩展头合法性与 8 字节对齐约束
			if int(ExtendCommHeadLen+extLen) > len(remaining) || extType >= MaxExtendHeadType || extLen%8 != 0 {
				break
			}

			content := remaining[ExtendCommHeadLen : ExtendCommHeadLen+extLen]
			packet.ExtendHeads = append(packet.ExtendHeads, ExtendHead{
				Type: extType,
				Len:  extLen,
				Raw:  content,
			})

			switch extType {
			case ExtTypeTraceKey:
				if len(content) >= 32 {
					packet.Trace = &TraceKeyExt{
						TraceID:      binary.BigEndian.Uint64(content[0:8]),
						SpanID:       binary.BigEndian.Uint64(content[8:16]),
						FatherSpanID: binary.BigEndian.Uint64(content[16:24]),
						RecordSwitch: content[24],
					}
				}
			case ExtTypeTimeStamp:
				if len(content) >= 8 {
					t := binary.BigEndian.Uint64(content[0:8])
					packet.ServerEnqueue = &t
				}
			}

			remaining = remaining[ExtendCommHeadLen+extLen:]
		}
	}

	return packet, nil
}
