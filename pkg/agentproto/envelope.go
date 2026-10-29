package agentproto

import (
	"encoding/binary"
	"net"
)

// EnvelopeHeader WebSocket 上行 56 字节定长网络大端信封头
type EnvelopeHeader struct {
	Magic           uint16  `json:"magic"`              // 0..1: 0x4354 ("CT")
	ProtoVer        uint8   `json:"proto_ver"`          // 2: 1
	EventType       uint8   `json:"event_type"`         // 3
	SessionOrTaskID uint64  `json:"session_or_task_id"` // 4..11 (BE)
	ProcessID       uint64  `json:"process_id"`         // 12..19 (BE)
	CapturedPID     uint32  `json:"captured_pid"`       // 20..23 (BE)
	Direction       uint8   `json:"direction"`          // 24 (1=INGRESS, 2=EGRESS)
	Flags           uint8   `json:"flags"`              // 25
	Reserved        uint16  `json:"reserved"`           // 26..27 (0)
	CapturedAtNs    int64   `json:"captured_at_ns"`     // 28..35 (BE)
	SrcIPv4         [4]byte `json:"src_ipv4"`           // 36..39
	DstIPv4         [4]byte `json:"dst_ipv4"`           // 40..43
	SrcPort         uint16  `json:"src_port"`           // 44..45 (BE)
	DstPort         uint16  `json:"dst_port"`           // 46..47 (BE)
	ConnSeq         uint32  `json:"conn_seq"`           // 48..51 (BE)
	PayloadLen      uint32  `json:"payload_len"`        // 52..55 (BE)
}

// Marshal 序列化为 56 字节网络大端切片
func (h EnvelopeHeader) Marshal() []byte {
	buf := make([]byte, EnvelopeHeaderLen)
	binary.BigEndian.PutUint16(buf[0:2], h.Magic)
	buf[2] = h.ProtoVer
	buf[3] = h.EventType
	binary.BigEndian.PutUint64(buf[4:12], h.SessionOrTaskID)
	binary.BigEndian.PutUint64(buf[12:20], h.ProcessID)
	binary.BigEndian.PutUint32(buf[20:24], h.CapturedPID)
	buf[24] = h.Direction
	buf[25] = h.Flags
	binary.BigEndian.PutUint16(buf[26:28], h.Reserved)
	binary.BigEndian.PutUint64(buf[28:36], uint64(h.CapturedAtNs))
	copy(buf[36:40], h.SrcIPv4[:])
	copy(buf[40:44], h.DstIPv4[:])
	binary.BigEndian.PutUint16(buf[44:46], h.SrcPort)
	binary.BigEndian.PutUint16(buf[46:48], h.DstPort)
	binary.BigEndian.PutUint32(buf[48:52], h.ConnSeq)
	binary.BigEndian.PutUint32(buf[52:56], h.PayloadLen)
	return buf
}

// IsTruncated 是否因超出阈值发生截断
func (h EnvelopeHeader) IsTruncated() bool {
	return h.Flags&FlagIsTruncated != 0
}

// RefKind 关联资产类型 (0 = RECORDING_SESSION, 1 = REPLAY_TASK)
func (h EnvelopeHeader) RefKind() uint8 {
	if h.Flags&FlagRefKind != 0 {
		return RefKindReplayTask
	}
	return RefKindRecordingSession
}

// HasGap 内核 RingBuf 是否丢包
func (h EnvelopeHeader) HasGap() bool {
	return h.Flags&FlagHasGap != 0
}

// SrcIP 返回 IPv4 对象
func (h EnvelopeHeader) SrcIP() net.IP {
	return net.IPv4(h.SrcIPv4[0], h.SrcIPv4[1], h.SrcIPv4[2], h.SrcIPv4[3])
}

// DstIP 返回 IPv4 对象
func (h EnvelopeHeader) DstIP() net.IP {
	return net.IPv4(h.DstIPv4[0], h.DstIPv4[1], h.DstIPv4[2], h.DstIPv4[3])
}

// SetSrcIP 赋值源 IP
func (h *EnvelopeHeader) SetSrcIP(ip net.IP) {
	if v4 := ip.To4(); v4 != nil {
		copy(h.SrcIPv4[:], v4)
	}
}

// SetDstIP 赋值目的 IP
func (h *EnvelopeHeader) SetDstIP(ip net.IP) {
	if v4 := ip.To4(); v4 != nil {
		copy(h.DstIPv4[:], v4)
	}
}

// MarshalEnvelope 打包 56 字节信封头与变长载荷切片
func MarshalEnvelope(h EnvelopeHeader, payload []byte) []byte {
	h.PayloadLen = uint32(len(payload))
	headerBytes := h.Marshal()
	return append(headerBytes, payload...)
}

// UnmarshalEnvelope 从原始二进制切片还原信封头与载荷切片
func UnmarshalEnvelope(data []byte) (EnvelopeHeader, []byte, error) {
	if len(data) < EnvelopeHeaderLen {
		return EnvelopeHeader{}, nil, ErrEnvelopeBufferTooShort
	}

	magic := binary.BigEndian.Uint16(data[0:2])
	if magic != EnvelopeMagic {
		return EnvelopeHeader{}, nil, ErrInvalidEnvelopeMagic
	}

	h := EnvelopeHeader{
		Magic:           magic,
		ProtoVer:        data[2],
		EventType:       data[3],
		SessionOrTaskID: binary.BigEndian.Uint64(data[4:12]),
		ProcessID:       binary.BigEndian.Uint64(data[12:20]),
		CapturedPID:     binary.BigEndian.Uint32(data[20:24]),
		Direction:       data[24],
		Flags:           data[25],
		Reserved:        binary.BigEndian.Uint16(data[26:28]),
		CapturedAtNs:    int64(binary.BigEndian.Uint64(data[28:36])),
		SrcPort:         binary.BigEndian.Uint16(data[44:46]),
		DstPort:         binary.BigEndian.Uint16(data[46:48]),
		ConnSeq:         binary.BigEndian.Uint32(data[48:52]),
		PayloadLen:      binary.BigEndian.Uint32(data[52:56]),
	}
	copy(h.SrcIPv4[:], data[36:40])
	copy(h.DstIPv4[:], data[40:44])

	if len(data) < EnvelopeHeaderLen+int(h.PayloadLen) {
		return EnvelopeHeader{}, nil, ErrPayloadLengthMismatch
	}

	payload := data[EnvelopeHeaderLen : EnvelopeHeaderLen+int(h.PayloadLen)]
	return h, payload, nil
}
