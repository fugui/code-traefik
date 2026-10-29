package crosrpc

import (
	"encoding/binary"
)

// ReqHead CROS RPC 请求会话头 (RPC_REQ_HEAD_S)
type ReqHead struct {
	MsgStructVer       uint16   `json:"msg_struct_ver"`
	ReqHeadLen         uint16   `json:"req_head_len"`
	Dst2ndAddrId       uint32   `json:"dst_2nd_addr_id"`
	ServiceName        string   `json:"service_name"` // 格式 {package}-{serviceName}
	Operation          string   `json:"operation"`    // API 名称
	ReqType            uint8    `json:"req_type"`     // 1=Sync, 2=Async, 3=Oneway
	SubjectType        uint8    `json:"subject_type"`
	RpcVer             uint16   `json:"rpc_ver"`
	SessionId          uint32   `json:"session_id"`
	TraceBuff          [16]byte `json:"trace_buff"`
	IdempotentSeq      *uint64  `json:"idempotent_seq,omitempty"`       // V02 (>=168B)
	SubjectName        *string  `json:"subject_name,omitempty"`         // V03 (>=232B)
	ClientUuid         *uint64  `json:"client_uuid,omitempty"`          // V04 (>=244B)
	UuidTimeoutS       *uint16  `json:"uuid_timeout_s,omitempty"`       // V04 (>=244B)
	TimeOutSecondLimit *uint32  `json:"timeout_second_limit,omitempty"` // V05 (>=256B)
	SendTimeStamp      *uint64  `json:"send_time_stamp,omitempty"`      // V05 (>=256B)
}

// ParseReqHead 解析请求会话头切片 (相对于偏移 24)
func ParseReqHead(b []byte) (*ReqHead, error) {
	if len(b) < ReqHeadMinLen {
		return nil, ErrBufferTooShort
	}

	h := &ReqHead{
		MsgStructVer: binary.BigEndian.Uint16(b[0:2]),
		ReqHeadLen:   binary.BigEndian.Uint16(b[2:4]),
		Dst2ndAddrId: binary.BigEndian.Uint32(b[4:8]),
		ServiceName:  cString(b[8:72]),
		Operation:    cString(b[72:136]),
		ReqType:      b[136],
		SubjectType:  b[137],
		RpcVer:       binary.BigEndian.Uint16(b[138:140]),
		SessionId:    binary.BigEndian.Uint32(b[140:144]),
	}
	copy(h.TraceBuff[:], b[144:160])

	// V02 字段扩展 (>= 168B)
	if len(b) >= 168 {
		v := binary.BigEndian.Uint64(b[160:168])
		h.IdempotentSeq = &v
	}

	// V03 字段扩展 (>= 232B)
	if len(b) >= 232 {
		s := cString(b[168:232])
		h.SubjectName = &s
	}

	// V04 字段扩展 (>= 244B)
	if len(b) >= 244 {
		uuid := binary.BigEndian.Uint64(b[232:240])
		timeout := binary.BigEndian.Uint16(b[240:242])
		h.ClientUuid = &uuid
		h.UuidTimeoutS = &timeout
	}

	// V05 字段扩展 (>= 256B)
	if len(b) >= 256 {
		timeoutLimit := binary.BigEndian.Uint32(b[244:248])
		sendTime := binary.BigEndian.Uint64(b[248:256])
		h.TimeOutSecondLimit = &timeoutLimit
		h.SendTimeStamp = &sendTime
	}

	return h, nil
}

// RspLayout 表驱动应答会话头权威字段区间
type RspLayout struct {
	Version              string
	HeadLen              int
	TraceBuffOffset      int
	TraceBuffLen         int
	ClientSendTimeOffset int // 0 表示当前版本无此字段
	ServiceNameOffset    int
	OperationOffset      int
	StatusCode64Offset   int // 0 表示当前版本仅有 32 位状态码
	RspMsgNoOffset       int
}

var rspLayouts = map[int]RspLayout{
	156: {
		Version:              "V01",
		HeadLen:              156,
		TraceBuffOffset:      8,
		TraceBuffLen:         16,
		ClientSendTimeOffset: 0,
		ServiceNameOffset:    24,
		OperationOffset:      88,
		StatusCode64Offset:   0,
		RspMsgNoOffset:       152,
	},
	164: {
		Version:              "V02",
		HeadLen:              164,
		TraceBuffOffset:      8,
		TraceBuffLen:         16,
		ClientSendTimeOffset: 0,
		ServiceNameOffset:    32,
		OperationOffset:      96,
		StatusCode64Offset:   0,
		RspMsgNoOffset:       160,
	},
	188: {
		Version:              "V03",
		HeadLen:              188,
		TraceBuffOffset:      8,
		TraceBuffLen:         16,
		ClientSendTimeOffset: 32,
		ServiceNameOffset:    56,
		OperationOffset:      120,
		StatusCode64Offset:   0,
		RspMsgNoOffset:       184,
	},
	200: {
		Version:              "V04",
		HeadLen:              200,
		TraceBuffOffset:      8,
		TraceBuffLen:         16,
		ClientSendTimeOffset: 32,
		ServiceNameOffset:    56,
		OperationOffset:      120,
		StatusCode64Offset:   184,
		RspMsgNoOffset:       192,
	},
}

// LookupRspLayout 依据应答会话头实际字节数检索权威字段偏移映射
func LookupRspLayout(headLen int) (*RspLayout, error) {
	layout, ok := rspLayouts[headLen]
	if !ok {
		return nil, ErrUnknownRspLayout
	}
	return &layout, nil
}

// RspHead CROS RPC 应答会话头 (RPC_RSP_HEAD_S)
type RspHead struct {
	MsgStructVer   uint16   `json:"msg_struct_ver"`
	RspHeadLen     uint16   `json:"rsp_head_len"`
	StatusCode     uint64   `json:"status_code"`
	TraceBuff      [16]byte `json:"trace_buff"`
	ClientSendTime *uint64  `json:"client_send_time,omitempty"`
	ServiceName    string   `json:"service_name"`
	Operation      string   `json:"operation"`
	RspMsgNo       uint16   `json:"rsp_msg_no"`
}

// ParseRspHead 表驱动解析应答会话头切片
func ParseRspHead(b []byte, headLen int) (*RspHead, error) {
	layout, err := LookupRspLayout(headLen)
	if err != nil {
		return nil, err
	}
	if len(b) < layout.HeadLen {
		return nil, ErrBufferTooShort
	}

	h := &RspHead{
		MsgStructVer: binary.BigEndian.Uint16(b[0:2]),
		RspHeadLen:   binary.BigEndian.Uint16(b[2:4]),
		ServiceName:  cString(b[layout.ServiceNameOffset : layout.ServiceNameOffset+ServiceNameMaxLen]),
		Operation:    cString(b[layout.OperationOffset : layout.OperationOffset+OperationMaxLen]),
		RspMsgNo:     binary.BigEndian.Uint16(b[layout.RspMsgNoOffset : layout.RspMsgNoOffset+2]),
	}
	copy(h.TraceBuff[:], b[layout.TraceBuffOffset:layout.TraceBuffOffset+TraceBuffLen])

	if layout.StatusCode64Offset > 0 {
		h.StatusCode = binary.BigEndian.Uint64(b[layout.StatusCode64Offset : layout.StatusCode64Offset+8])
	} else {
		h.StatusCode = uint64(binary.BigEndian.Uint32(b[4:8]))
	}

	if layout.ClientSendTimeOffset > 0 {
		t := binary.BigEndian.Uint64(b[layout.ClientSendTimeOffset : layout.ClientSendTimeOffset+8])
		h.ClientSendTime = &t
	}

	return h, nil
}

// MngHead CROS RPC 管理消息会话头 (RPC_MNG_HEAD_S, 定长 88 字节)
type MngHead struct {
	Role         uint16  `json:"role"`
	Type         uint16  `json:"type"`
	RspResult    bool    `json:"rsp_result"`
	ProcName     string  `json:"proc_name"`
	ProcRet      uint32  `json:"proc_ret"`
	SvrAuthorize bool    `json:"svr_authorize"`
	EnCryption   uint8   `json:"encryption"`
	EnIntegrity  uint8   `json:"en_integrity"`
	CipherSuite  uint8   `json:"cipher_suite"`
	Reserved     [5]byte `json:"-"`
}

// ParseMngHead 解析 88 字节管理消息会话头
func ParseMngHead(b []byte) (*MngHead, error) {
	if len(b) < MngHeadMinLen {
		return nil, ErrBufferTooShort
	}

	return &MngHead{
		Role:         binary.BigEndian.Uint16(b[0:2]),
		Type:         binary.BigEndian.Uint16(b[2:4]),
		RspResult:    binary.BigEndian.Uint32(b[4:8]) != 0,
		ProcName:     cString(b[8:72]),
		ProcRet:      binary.BigEndian.Uint32(b[72:76]),
		SvrAuthorize: binary.BigEndian.Uint32(b[76:80]) != 0,
		EnCryption:   b[80],
		EnIntegrity:  b[81],
		CipherSuite:  b[82],
	}, nil
}

// cString 辅助读取以 null 结尾的 C 字符数组
func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
