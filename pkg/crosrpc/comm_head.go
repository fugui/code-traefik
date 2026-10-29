package crosrpc

import (
	"bytes"
	"encoding/binary"
)

// CommHead CROS RPC 24 字节定长公共消息头 (RPC_COMM_HEAD_S)
type CommHead struct {
	Magic          [7]byte `json:"magic"`
	TransportProto uint8   `json:"transport_proto"`
	SNO            uint64  `json:"sno"`
	Length         uint32  `json:"length"`
	HeadLen        uint16  `json:"head_len"`
	MsgType        uint8   `json:"msg_type"`
	IsPacked       uint8   `json:"is_packed"`
}

// ParseCommHead 从原始 24 字节大端网络序反序列化公共消息头
func ParseCommHead(raw []byte) (CommHead, error) {
	if len(raw) < CommHeadLen {
		return CommHead{}, ErrBufferTooShort
	}
	var h CommHead
	copy(h.Magic[:], raw[0:7])
	h.TransportProto = raw[7]
	h.SNO = binary.BigEndian.Uint64(raw[8:16])
	h.Length = binary.BigEndian.Uint32(raw[16:20])
	h.HeadLen = binary.BigEndian.Uint16(raw[20:22])
	h.MsgType = raw[22]
	h.IsPacked = raw[23]
	return h, nil
}

// Marshal 将公共消息头序列化为 24 字节网络大端序切片
func (h CommHead) Marshal() []byte {
	buf := make([]byte, CommHeadLen)
	copy(buf[0:7], h.Magic[:])
	buf[7] = h.TransportProto
	binary.BigEndian.PutUint64(buf[8:16], h.SNO)
	binary.BigEndian.PutUint32(buf[16:20], h.Length)
	binary.BigEndian.PutUint16(buf[20:22], h.HeadLen)
	buf[22] = h.MsgType
	buf[23] = h.IsPacked
	return buf
}

// ValidateCommHead 按照 DOPHI CROS RPC 规范全面校验公共消息头合法性边界
func ValidateCommHead(h CommHead) error {
	// 1. 魔术字校验
	if !bytes.Equal(h.Magic[:], []byte(MagicPrefix)) {
		return ErrInvalidMagic
	}

	// 2. 传输协议限定 TCP 明文底座 (1) 或缺省 (0)
	if h.TransportProto > 1 {
		return ErrInvalidTransport
	}

	// 3. 消息类型合法范围 [2, MsgTypeButt)
	if h.MsgType < MsgTypeReq || h.MsgType >= MsgTypeButt {
		return ErrInvalidMsgType
	}

	// 4. 最小会话头下界检查
	switch {
	case h.MsgType == MsgTypeReq:
		if h.HeadLen < ReqHeadMinLen {
			return ErrHeadLenTooSmall
		}
	case h.MsgType == MsgTypeRsp || h.MsgType == MsgTypeRspSeg:
		if h.HeadLen < RspHeadMinLen {
			return ErrHeadLenTooSmall
		}
	case h.MsgType == MsgTypeMng || h.MsgType == MsgTypeMngReq || h.MsgType == MsgTypeUUIDReq:
		if h.HeadLen < MngHeadMinLen {
			return ErrHeadLenTooSmall
		}
	}

	// 5. 总长度与头长度关系校验: HeadLen <= Length
	if uint32(h.HeadLen) > h.Length {
		return ErrHeadLenGreaterThanLength
	}

	// 6. 最大载荷上界防溢出校验: Length <= MaxPayloadLen + HeadLen
	if uint64(h.Length) > uint64(MaxPayloadLen)+uint64(h.HeadLen) {
		return ErrLengthTooLarge
	}

	return nil
}
