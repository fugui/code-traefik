package recorder

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"code-traefik/internal/server/decoder"
	"code-traefik/models"
	"code-traefik/pkg/crosrpc"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// ByteSliceSegment 字节切片段
type ByteSliceSegment struct {
	OffsetStart  int    `json:"offsetStart"`
	ByteLength   int    `json:"byteLength"`
	FieldName    string `json:"fieldName"`
	RawHex       string `json:"rawHex"`
	DisplayValue string `json:"displayValue"`
}

// CommHeadAnatomyDTO ① 第一段：24B 公共头偏移位图
type CommHeadAnatomyDTO struct {
	MagicAscii        string             `json:"magicAscii"`
	TransportProtocol int                `json:"transportProtocol"`
	TransportName     string             `json:"transportName"`
	SNODec            string             `json:"snoDec"`
	SNOHex            string             `json:"snoHex"`
	UILength          int                `json:"uiLength"`
	USHeadLen         int                `json:"usHeadLen"`
	UCMsgType         int                `json:"ucMsgType"`
	MsgTypeName       string             `json:"msgTypeName"`
	IsPacked          int                `json:"isPacked"`
	Segments          []ByteSliceSegment `json:"segments"`
}

// SessionHeadFieldDTO 会话头字段行
type SessionHeadFieldDTO struct {
	Name        string `json:"name"`
	OffsetRange string `json:"offsetRange"`
	Value       string `json:"value"`
}

// TimingBreakdownUsDTO 会话耗时拆解
type TimingBreakdownUsDTO struct {
	ClientToServerUs *int64 `json:"clientToServerUs,omitempty"`
	ServerProcessUs  *int64 `json:"serverProcessUs,omitempty"`
}

// SessionHeadAnatomyDTO ② 第二段：会话头明细与耗时
type SessionHeadAnatomyDTO struct {
	HeadKind          string                `json:"headKind"` // "REQ" | "RSP" | "MNG"
	DetectedVersion   string                `json:"detectedVersion"`
	SessionHeadLen    int                   `json:"sessionHeadLen"`
	ServiceName       string                `json:"serviceName"`
	Operation         string                `json:"operation"`
	ReqType           *int                  `json:"reqType,omitempty"`
	UIStatusCode      *uint32               `json:"uiStatusCode,omitempty"`
	ULLStatusCode     *string               `json:"ullStatusCode,omitempty"`
	RspMsgNo          *int                  `json:"rspMsgNo,omitempty"`
	TraceBuffHex      string                `json:"traceBuffHex,omitempty"`
	TimingBreakdownUs *TimingBreakdownUsDTO `json:"timingBreakdownUs,omitempty"`
	Fields            []SessionHeadFieldDTO `json:"fields"`
}

// ExtendHeadTraceDTO DOTR Trace
type ExtendHeadTraceDTO struct {
	TraceIDHex      string `json:"traceIdHex"`
	SpanIDHex       string `json:"spanIdHex"`
	FatherSpanIDHex string `json:"fatherSpanIdHex"`
	RecordMsgSwitch int    `json:"recordMsgSwitch"`
}

// ExtendHeadItemDTO 扩展头项
type ExtendHeadItemDTO struct {
	UCType    int    `json:"ucType"`
	TypeName  string `json:"typeName"`
	USHeadLen int    `json:"usHeadLen"`
	RawHex    string `json:"rawHex"`
}

// ExtendHeadAnatomyDTO ③ 第三段：扩展头集合
type ExtendHeadAnatomyDTO struct {
	TotalExtendBytes  int                 `json:"totalExtendBytes"`
	Trace             *ExtendHeadTraceDTO `json:"trace,omitempty"`
	ServerEnqueueTime string              `json:"serverEnqueueTime,omitempty"`
	QueueWaitUs       *int64              `json:"queueWaitUs,omitempty"`
	Items             []ExtendHeadItemDTO `json:"items"`
}

// HexLineDTO 16B 对齐对照行
type HexLineDTO struct {
	OffsetHex string `json:"offsetHex"`
	HexCols   string `json:"hexCols"`
	ASCIICols string `json:"asciiCols"`
	Region    string `json:"region"` // 'COMM_HEAD' | 'SESSION_HEAD' | 'EXTEND_HEAD' | 'PAYLOAD'
}

// PayloadAnatomyDTO ④ 第四段：载荷 JSON 与 Hex
type PayloadAnatomyDTO struct {
	PayloadOffset    int          `json:"payloadOffset"`
	PayloadLength    int          `json:"payloadLength"`
	IsTruncated      bool         `json:"isTruncated"`
	IsReplayable     bool         `json:"isReplayable"`
	DecodeStatus     string       `json:"decodeStatus"`
	SchemaName       string       `json:"schemaName,omitempty"`
	SchemaVersion    *int         `json:"schemaVersion,omitempty"`
	MessageFullName  string       `json:"messageFullName,omitempty"`
	DecodedJSON      interface{}  `json:"decodedJson,omitempty"`
	RawFrameHexLines []HexLineDTO `json:"rawFrameHexLines"`
}

// FrameSideAnatomyDTO 单侧报文四段完整解剖
type FrameSideAnatomyDTO struct {
	CommHead     CommHeadAnatomyDTO    `json:"commHead"`
	SessionHead  SessionHeadAnatomyDTO `json:"sessionHead"`
	ExtendHead   ExtendHeadAnatomyDTO  `json:"extendHead"`
	Payload      PayloadAnatomyDTO     `json:"payload"`
	SegmentCount int                   `json:"segmentCount"`
}

// PacketAnatomyDrawerDTO 四段式报文解剖完整 DTO
type PacketAnatomyDrawerDTO struct {
	RecordID           string               `json:"recordId"`
	SessionID          string               `json:"sessionId"`
	Direction          string               `json:"direction"`
	ExchangeType       string               `json:"exchangeType"`
	RoundTripLatencyUs *int64               `json:"roundTripLatencyUs,omitempty"`
	SrcModuleName      string               `json:"srcModuleName,omitempty"`
	DstModuleName      string               `json:"dstModuleName,omitempty"`
	ReqSide            *FrameSideAnatomyDTO `json:"reqSide,omitempty"`
	RspSide            *FrameSideAnatomyDTO `json:"rspSide,omitempty"`
}

// BuildPacketAnatomy 组装单条 TrafficRecord 的请求侧与应答侧四段解剖 DTO
func BuildPacketAnatomy(record *models.TrafficRecord, dec *decoder.Registry) (*PacketAnatomyDrawerDTO, error) {
	if record == nil {
		return nil, fmt.Errorf("record cannot be nil")
	}

	dto := &PacketAnatomyDrawerDTO{
		RecordID:           fmt.Sprintf("%d", record.ID),
		SessionID:          fmt.Sprintf("%d", record.SessionID),
		Direction:          record.Direction,
		ExchangeType:       record.ExchangeType,
		RoundTripLatencyUs: record.LatencyUs,
	}

	if record.SrcModuleID != nil {
		dto.SrcModuleName = fmt.Sprintf("MOD_%d", *record.SrcModuleID)
	}
	if record.DstModuleID != nil {
		dto.DstModuleName = fmt.Sprintf("MOD_%d", *record.DstModuleID)
	}

	// 1. 请求侧解剖 (非 RSP_ORPHAN 时存在)
	if len(record.RawReqPacket) > 0 {
		reqSide, err := BuildSingleFrameSide(record.RawReqPacket, record.IsTruncated, record.IsReplayable, false, 1, dec)
		if err == nil {
			dto.ReqSide = reqSide
		}
	}

	// 2. 应答侧解剖 (非 ONEWAY 与 REQ_ONLY 时存在)
	if len(record.RawRspPacket) > 0 {
		segCount := record.RspSegCount
		if segCount <= 0 {
			segCount = 1
		}
		rspSide, err := BuildSingleFrameSide(record.RawRspPacket, record.IsTruncated, record.IsReplayable, true, segCount, dec)
		if err == nil {
			dto.RspSide = rspSide
		}
	}

	return dto, nil
}

// BuildSingleFrameSide 组装单侧报文（请求侧或应答侧）四段式完整解剖
func BuildSingleFrameSide(
	raw []byte,
	isTruncated bool,
	isReplayable bool,
	isRsp bool,
	segCount int,
	dec *decoder.Registry,
) (*FrameSideAnatomyDTO, error) {
	parsed, err := crosrpc.ParsePacket(raw)
	if err != nil && len(raw) < 24 {
		return nil, err
	}

	var comm crosrpc.CommHead
	if parsed != nil {
		comm = parsed.Comm
	} else {
		comm, _ = crosrpc.ParseCommHead(raw)
	}

	// 1. CommHead (24B) 6 色块切片
	commHeadDTO := CommHeadAnatomyDTO{
		MagicAscii:        "CSE.TCP",
		TransportProtocol: int(comm.TransportProto),
		TransportName:     "RPC_TRANSPORT_TCP",
		SNODec:            fmt.Sprintf("%d", comm.SNO),
		SNOHex:            fmt.Sprintf("0x%016x", comm.SNO),
		UILength:          int(comm.Length),
		USHeadLen:         int(comm.HeadLen),
		UCMsgType:         int(comm.MsgType),
		IsPacked:          int(comm.IsPacked),
	}
	switch comm.MsgType {
	case crosrpc.MsgTypeReq:
		commHeadDTO.MsgTypeName = "RPC_MSG_TYPE_REQ"
	case crosrpc.MsgTypeRsp:
		commHeadDTO.MsgTypeName = "RPC_MSG_TYPE_RSP"
	case crosrpc.MsgTypeRspSeg:
		commHeadDTO.MsgTypeName = "RPC_MSG_TYPE_RSP_SEG"
	default:
		commHeadDTO.MsgTypeName = fmt.Sprintf("RPC_MSG_TYPE_%d", comm.MsgType)
	}

	if len(raw) >= 24 {
		commHeadDTO.Segments = []ByteSliceSegment{
			{OffsetStart: 0, ByteLength: 8, FieldName: "unMagicInfo", RawHex: formatHex(raw[0:8]), DisplayValue: `"CSE.TCP" + TCP(1)`},
			{OffsetStart: 8, ByteLength: 8, FieldName: "ullSNO", RawHex: formatHex(raw[8:16]), DisplayValue: fmt.Sprintf("0x%016x", comm.SNO)},
			{OffsetStart: 16, ByteLength: 4, FieldName: "uiLength", RawHex: formatHex(raw[16:20]), DisplayValue: fmt.Sprintf("%d", comm.Length)},
			{OffsetStart: 20, ByteLength: 2, FieldName: "usHeadLen", RawHex: formatHex(raw[20:22]), DisplayValue: fmt.Sprintf("%d", comm.HeadLen)},
			{OffsetStart: 22, ByteLength: 1, FieldName: "ucMsgType", RawHex: formatHex(raw[22:23]), DisplayValue: fmt.Sprintf("%d (%s)", comm.MsgType, commHeadDTO.MsgTypeName)},
			{OffsetStart: 23, ByteLength: 1, FieldName: "isPacked", RawHex: formatHex(raw[23:24]), DisplayValue: fmt.Sprintf("%d", comm.IsPacked)},
		}
	}

	// 2. SessionHead
	sessDTO := SessionHeadAnatomyDTO{
		HeadKind:       "REQ",
		SessionHeadLen: int(comm.HeadLen),
	}
	if isRsp {
		sessDTO.HeadKind = "RSP"
	}
	if parsed != nil {
		if parsed.Req != nil {
			sessDTO.DetectedVersion = fmt.Sprintf("V%02d (%dB)", parsed.Req.MsgStructVer, comm.HeadLen)
			sessDTO.ServiceName = parsed.Req.ServiceName
			sessDTO.Operation = parsed.Req.Operation
			reqT := int(parsed.Req.ReqType)
			sessDTO.ReqType = &reqT
			sessDTO.Fields = []SessionHeadFieldDTO{
				{Name: "usVersion", OffsetRange: "24..25", Value: fmt.Sprintf("%d", parsed.Req.MsgStructVer)},
				{Name: "usHeadLen", OffsetRange: "26..27", Value: fmt.Sprintf("%d", parsed.Req.ReqHeadLen)},
				{Name: "ascServiceName", OffsetRange: "32..95", Value: parsed.Req.ServiceName},
				{Name: "ascOperation", OffsetRange: "96..159", Value: parsed.Req.Operation},
				{Name: "ucReqType", OffsetRange: "160..160", Value: fmt.Sprintf("%d", parsed.Req.ReqType)},
			}
		} else if parsed.Rsp != nil {
			sessDTO.DetectedVersion = fmt.Sprintf("V%02d (%dB)", parsed.Rsp.MsgStructVer, comm.HeadLen)
			sessDTO.ServiceName = parsed.Rsp.ServiceName
			sessDTO.Operation = parsed.Rsp.Operation
			code := uint32(parsed.Rsp.StatusCode)
			sessDTO.UIStatusCode = &code
			ullCodeStr := fmt.Sprintf("%d", parsed.Rsp.StatusCode)
			sessDTO.ULLStatusCode = &ullCodeStr
			msgNo := int(parsed.Rsp.RspMsgNo)
			sessDTO.RspMsgNo = &msgNo
			sessDTO.TraceBuffHex = hex.EncodeToString(parsed.Rsp.TraceBuff[:])

			var timing TimingBreakdownUsDTO
			if parsed.Rsp.ClientSendTime != nil && *parsed.Rsp.ClientSendTime > 0 {
				cTime := int64(*parsed.Rsp.ClientSendTime) / 1000
				timing.ClientToServerUs = &cTime
			}
			sessDTO.TimingBreakdownUs = &timing

			sessDTO.Fields = []SessionHeadFieldDTO{
				{Name: "usVersion", OffsetRange: "24..25", Value: fmt.Sprintf("%d", parsed.Rsp.MsgStructVer)},
				{Name: "usHeadLen", OffsetRange: "26..27", Value: fmt.Sprintf("%d", parsed.Rsp.RspHeadLen)},
				{Name: "uiStatusCode", OffsetRange: "28..31", Value: fmt.Sprintf("%d", parsed.Rsp.StatusCode)},
				{Name: "ascServiceName", OffsetRange: "48..111", Value: parsed.Rsp.ServiceName},
				{Name: "ascOperation", OffsetRange: "112..175", Value: parsed.Rsp.Operation},
				{Name: "usRspMsgNo", OffsetRange: "176..177", Value: fmt.Sprintf("%d", parsed.Rsp.RspMsgNo)},
			}
		}
	}

	// 3. ExtendHead
	extendDTO := ExtendHeadAnatomyDTO{}
	if parsed != nil {
		for _, ext := range parsed.ExtendHeads {
			extendDTO.TotalExtendBytes += int(ext.Len)
			tName := "UNKNOWN"
			if ext.Type == crosrpc.ExtTypeTraceKey {
				tName = "RPC_EXTEND_HEAD_TYPE_TRACE_KEY"
			} else if ext.Type == crosrpc.ExtTypeTimeStamp {
				tName = "RPC_EXTEND_HEAD_TYPE_TIME_STAMP"
			}
			extendDTO.Items = append(extendDTO.Items, ExtendHeadItemDTO{
				UCType:    int(ext.Type),
				TypeName:  tName,
				USHeadLen: int(ext.Len),
				RawHex:    hex.EncodeToString(ext.Raw),
			})
		}
		if parsed.Trace != nil {
			extendDTO.Trace = &ExtendHeadTraceDTO{
				TraceIDHex:      fmt.Sprintf("%016x", parsed.Trace.TraceID),
				SpanIDHex:       fmt.Sprintf("%016x", parsed.Trace.SpanID),
				FatherSpanIDHex: fmt.Sprintf("%016x", parsed.Trace.FatherSpanID),
				RecordMsgSwitch: int(parsed.Trace.RecordSwitch),
			}
		}
		if parsed.ServerEnqueue != nil {
			extendDTO.ServerEnqueueTime = fmt.Sprintf("%d", *parsed.ServerEnqueue)
		}
	}

	// 4. Payload & 16B aligned Hex lines
	payloadOffset := 24 + int(comm.HeadLen)
	if payloadOffset > len(raw) {
		payloadOffset = len(raw)
	}
	payloadBytes := raw[payloadOffset:]

	payloadDTO := PayloadAnatomyDTO{
		PayloadOffset: payloadOffset,
		PayloadLength: len(payloadBytes),
		IsTruncated:   isTruncated,
		IsReplayable:  isReplayable,
		DecodeStatus:  decoder.DecodeStatusNoSchemaRawHex,
	}

	if isTruncated {
		payloadDTO.DecodeStatus = decoder.DecodeStatusTruncated
	} else if dec != nil && sessDTO.ServiceName != "" {
		decRes := dec.DecodePayloadToJSON(sessDTO.ServiceName, sessDTO.Operation, comm.MsgType, payloadBytes)
		payloadDTO.DecodeStatus = decRes.DecodeStatus
		payloadDTO.SchemaName = decRes.SchemaName
		payloadDTO.SchemaVersion = decRes.SchemaVersion
		payloadDTO.MessageFullName = decRes.MessageFullName
		if len(decRes.JSONBytes) > 0 {
			var obj interface{}
			if jsonErr := json.Unmarshal(decRes.JSONBytes, &obj); jsonErr == nil {
				payloadDTO.DecodedJSON = obj
			}
		}
	}

	hexLines := build16BAlignedHexLines(raw, int(comm.HeadLen), extendDTO.TotalExtendBytes)
	payloadDTO.RawFrameHexLines = hexLines

	return &FrameSideAnatomyDTO{
		CommHead:     commHeadDTO,
		SessionHead:  sessDTO,
		ExtendHead:   extendDTO,
		Payload:      payloadDTO,
		SegmentCount: segCount,
	}, nil
}

func build16BAlignedHexLines(raw []byte, sessionHeadLen int, extendHeadLen int) []HexLineDTO {
	var lines []HexLineDTO
	n := len(raw)
	sessionEnd := 24 + sessionHeadLen
	extendEnd := sessionEnd + extendHeadLen

	for i := 0; i < n; i += 16 {
		chunkEnd := i + 16
		if chunkEnd > n {
			chunkEnd = n
		}
		chunk := raw[i:chunkEnd]

		var hexParts []string
		for idx, b := range chunk {
			hexParts = append(hexParts, fmt.Sprintf("%02x", b))
			if idx == 7 && len(chunk) > 8 {
				hexParts = append(hexParts, "")
			}
		}
		hexStr := strings.Join(hexParts, " ")

		var asciiBuilder strings.Builder
		for _, b := range chunk {
			if b >= 32 && b <= 126 {
				asciiBuilder.WriteByte(b)
			} else {
				asciiBuilder.WriteByte('.')
			}
		}

		region := "PAYLOAD"
		if i < 24 {
			region = "COMM_HEAD"
		} else if i < sessionEnd {
			region = "SESSION_HEAD"
		} else if i < extendEnd {
			region = "EXTEND_HEAD"
		}

		lines = append(lines, HexLineDTO{
			OffsetHex: fmt.Sprintf("%08x", i),
			HexCols:   hexStr,
			ASCIICols: asciiBuilder.String(),
			Region:    region,
		})
	}
	return lines
}

func formatHex(b []byte) string {
	parts := make([]string, len(b))
	for i, val := range b {
		parts[i] = fmt.Sprintf("%02X", val)
	}
	return strings.Join(parts, " ")
}

// RedecodeTrafficRecord 按需使用当前生效的最新 Protobuf 版本对历史往返记录重新解码
func RedecodeTrafficRecord(
	ctx context.Context,
	db *gorm.DB,
	dec *decoder.Registry,
	recordID uint64,
) (*PacketAnatomyDrawerDTO, error) {
	if db == nil || dec == nil {
		return nil, fmt.Errorf("db and dec are required")
	}

	var rec models.TrafficRecord
	if err := db.WithContext(ctx).Where("id = ?", recordID).First(&rec).Error; err != nil {
		return nil, fmt.Errorf("获取流量记录失败: %w", err)
	}

	payloadMap := make(map[string]interface{})
	var newSchemaID *uint64
	var newSchemaVer *int
	decodeStatus := decoder.DecodeStatusNoSchemaRawHex

	if len(rec.RawReqPacket) > 0 {
		parsedReq, err := crosrpc.ParsePacket(rec.RawReqPacket)
		if err == nil && parsedReq != nil && parsedReq.Req != nil {
			payloadBytes := rec.RawReqPacket[parsedReq.PayloadOffset:]
			decRes := dec.DecodePayloadToJSON(parsedReq.Req.ServiceName, parsedReq.Req.Operation, crosrpc.MsgTypeReq, payloadBytes)
			decodeStatus = decRes.DecodeStatus
			newSchemaID = decRes.SchemaID
			newSchemaVer = decRes.SchemaVersion
			if len(decRes.JSONBytes) > 0 {
				var obj interface{}
				_ = json.Unmarshal(decRes.JSONBytes, &obj)
				payloadMap["req"] = obj
			}
		}
	}

	if len(rec.RawRspPacket) > 0 {
		parsedRsp, err := crosrpc.ParsePacket(rec.RawRspPacket)
		if err == nil && parsedRsp != nil && parsedRsp.Rsp != nil {
			payloadBytes := rec.RawRspPacket[parsedRsp.PayloadOffset:]
			decRes := dec.DecodePayloadToJSON(parsedRsp.Rsp.ServiceName, parsedRsp.Rsp.Operation, crosrpc.MsgTypeRsp, payloadBytes)
			if decodeStatus == decoder.DecodeStatusNoSchemaRawHex {
				decodeStatus = decRes.DecodeStatus
			}
			if newSchemaID == nil {
				newSchemaID = decRes.SchemaID
				newSchemaVer = decRes.SchemaVersion
			}
			if len(decRes.JSONBytes) > 0 {
				var obj interface{}
				_ = json.Unmarshal(decRes.JSONBytes, &obj)
				payloadMap["rsp"] = obj
			}
		}
	}

	pBytes, _ := json.Marshal(payloadMap)
	rec.PayloadJSON = datatypes.JSON(pBytes)
	rec.SchemaID = newSchemaID
	rec.SchemaVersion = newSchemaVer
	rec.DecodeStatus = decodeStatus

	updates := map[string]interface{}{
		"payload_json":   rec.PayloadJSON,
		"schema_id":      rec.SchemaID,
		"schema_version": rec.SchemaVersion,
		"decode_status":  rec.DecodeStatus,
	}
	if err := db.WithContext(ctx).Model(&models.TrafficRecord{}).
		Where("id = ? AND created_at = ?", rec.ID, rec.CreatedAt).
		Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("更新重解码结果失败: %w", err)
	}

	return BuildPacketAnatomy(&rec, dec)
}

var _ = binary.BigEndian
