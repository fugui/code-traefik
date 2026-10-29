package crosrpc

import "errors"

const (
	// MagicPrefix CROS RPC 7 字节固定魔术字 ("CSE.TCP")
	MagicPrefix = "CSE.TCP"

	// 定长结构体字节大小
	CommHeadLen       = 24 // sizeof(RPC_COMM_HEAD_S)
	ExtendCommHeadLen = 4  // sizeof(RPC_EXTEND_COMM_HEAD_S)
	ServiceNameMaxLen = 64 // RPC_SERVICE_NAME_LEN
	OperationMaxLen   = 64 // RPC_OPERATION_LEN
	ProcNameMaxLen    = 64 // RPC_PROC_NAME_LEN
	TraceBuffLen      = 16 // RPC_MSG_TRACE_BYTE_NUM

	// MaxPayloadLen RPC_PAYLOAD_MAX_LEN = 32*1024*1024 - 4096 = 33,550,336 Bytes
	MaxPayloadLen = 32*1024*1024 - 4096

	// MaxExtendHeadType 扩展头类型合法上界
	MaxExtendHeadType = 63

	// DefaultMaxCaptureBytes 单帧最大截断落盘阈值（默认 4MB）
	DefaultMaxCaptureBytes = 4 * 1024 * 1024

	// 最小会话头下界
	ReqHeadMinLen = 160 // RPC_REQ_HEAD_V1_S
	RspHeadMinLen = 156 // RPC_RSP_HEAD_V1_S
	MngHeadMinLen = 88  // RPC_MNG_HEAD_S

	// 传输协议标识 RPC_TRANSPORT_TYPE_E
	TransportDefault   = uint8(0) // RPC_TRANSPORT_DEFAULT
	TransportTCP       = uint8(1) // RPC_TRANSPORT_TCP (本系统主要覆盖底座)
	TransportDMS       = uint8(2)
	TransportUC        = uint8(3)
	TransportMesh      = uint8(4)
	TransportDomain    = uint8(5)
	TransportFRPC      = uint8(6)
	TransportCustomize = uint8(7)

	// 消息类型 RPC_MSG_TYPE_E
	MsgTypeReq               = uint8(2)  // RPC_MSG_TYPE_REQ
	MsgTypeRsp               = uint8(3)  // RPC_MSG_TYPE_RSP
	MsgTypeRspSeg            = uint8(4)  // RPC_MSG_TYPE_RSP_SEG
	MsgTypeMng               = uint8(5)  // RPC_MSG_TYPE_MNG
	MsgTypeMngReq            = uint8(6)  // RPC_MSG_TYPE_MNG_REQ
	MsgTypeUUIDReq           = uint8(7)  // RPC_MSG_TYPE_UUID_REQ
	MsgTypeTLSReq            = uint8(8)  // RPC_MSG_TYPE_TLS_REQ
	MsgTypeTLSRsp            = uint8(9)  // RPC_MSG_TYPE_TLS_RSP
	MsgTypeTLSRspSeg         = uint8(10) // RPC_MSG_TYPE_TLS_RSP_SEG
	MsgTypeFRPCReq           = uint8(11) // RPC_MSG_TYPE_FRPC_REQ
	MsgTypeFRPCDEl           = uint8(12) // RPC_MSG_TYPE_FRPC_DEL
	MsgTypeDirectChanConnReq = uint8(13) // RPC_MSG_TYPE_DIRECT_CHAN_CONN_REQ
	MsgTypeDirectChanConnRsp = uint8(14) // RPC_MSG_TYPE_DIRECT_CHAN_CONN_RSP
	MsgTypeButt              = uint8(15) // RPC_MSG_TYPE_MNG_BUTT (合法上界，不含)

	// 请求类型 RPC_REQ_MSG_TYPE_E
	ReqTypeSync   = uint8(1) // 同步请求
	ReqTypeAsync  = uint8(2) // 异步请求
	ReqTypeOneway = uint8(3) // 单向通知 (不期待应答)

	// 扩展头类型 RPC_EXTEND_HEAD_TYPE_E
	ExtTypeTraceKey  = uint8(0) // RPC_EXTEND_HEAD_TYPE_TRACE_KEY
	ExtTypeTimeStamp = uint8(1) // RPC_EXTEND_HEAD_TYPE_TIME_STAMP
)

var (
	ErrInvalidMagic             = errors.New("crosrpc: invalid magic prefix")
	ErrInvalidTransport         = errors.New("crosrpc: invalid transport protocol")
	ErrInvalidMsgType           = errors.New("crosrpc: invalid message type")
	ErrHeadLenTooSmall          = errors.New("crosrpc: session head length below minimum")
	ErrHeadLenGreaterThanLength = errors.New("crosrpc: head length greater than total length")
	ErrLengthTooLarge           = errors.New("crosrpc: total length exceeds maximum allowed payload")
	ErrBufferTooShort           = errors.New("crosrpc: buffer too short")
	ErrUnknownRspLayout         = errors.New("crosrpc: unknown response head layout")
	ErrExtendHeadMalformed      = errors.New("crosrpc: malformed extend head")
)
