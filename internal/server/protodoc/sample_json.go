package protodoc

import (
	"bytes"
	"encoding/json"
	"strings"

	"code-traefik/models"
	"code-traefik/pkg/crosrpc"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
	"gorm.io/gorm"
)

type orderedKV struct {
	Key   string
	Value any
}

type orderedJSONObject []orderedKV

func (o orderedJSONObject) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, kv := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		kBytes, err := json.Marshal(kv.Key)
		if err != nil {
			return nil, err
		}
		vBytes, err := json.Marshal(kv.Value)
		if err != nil {
			return nil, err
		}
		buf.Write(kBytes)
		buf.WriteByte(':')
		buf.Write(vBytes)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func buildDefaultJSONByName(reg *protoregistry.Files, msgFullName string) []byte {
	if reg == nil || msgFullName == "" {
		return []byte("null")
	}
	d, err := reg.FindDescriptorByName(protoreflect.FullName(strings.TrimPrefix(msgFullName, ".")))
	if err != nil {
		return []byte("null")
	}
	md, ok := d.(protoreflect.MessageDescriptor)
	if !ok {
		return []byte("null")
	}
	raw, err := BuildDefaultMessageJSON(md)
	if err != nil {
		return []byte("null")
	}
	return raw
}

// BuildDefaultMessageJSON 基于 Protobuf AST 递归构造缺省 JSON 请求/应答示例：
// 1. int64/uint64/sint64/fixed64/sfixed64 严格按 Protobuf JSON 规范输出 string ("0")；
// 2. Active-path 环路检测 + depth <= 12 硬截断，确保树形自递归永不栈溢出。
func BuildDefaultMessageJSON(msgDesc protoreflect.MessageDescriptor) (json.RawMessage, error) {
	if msgDesc == nil {
		return json.RawMessage("null"), nil
	}
	visited := map[string]bool{
		string(msgDesc.FullName()): true,
	}
	obj := buildDefaultObject(msgDesc, visited, 1, MaxDocFieldDepth)
	raw, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func buildDefaultObject(
	msgDesc protoreflect.MessageDescriptor,
	visited map[string]bool,
	depth, maxDepth int,
) orderedJSONObject {
	fields := msgDesc.Fields()
	out := make(orderedJSONObject, 0, fields.Len())

	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		fieldName := string(fd.Name())

		if fd.IsMap() {
			mapKey := defaultMapKey(fd.MapKey())
			vDesc := fd.MapValue()
			val := defaultMapOrScalarValue(vDesc, visited, depth, maxDepth)
			out = append(out, orderedKV{
				Key:   fieldName,
				Value: orderedJSONObject{{Key: mapKey, Value: val}},
			})
			continue
		}

		val := defaultFieldSampleValue(fd, visited, depth, maxDepth)
		if fd.Cardinality() == protoreflect.Repeated {
			if fd.Kind() == protoreflect.MessageKind && fd.Message() != nil {
				subName := string(fd.Message().FullName())
				if visited[subName] || depth >= maxDepth {
					val = []any{}
				} else {
					val = []any{val}
				}
			} else {
				val = []any{val}
			}
		}

		out = append(out, orderedKV{Key: fieldName, Value: val})
	}

	return out
}

func defaultMapOrScalarValue(
	fd protoreflect.FieldDescriptor,
	visited map[string]bool,
	depth, maxDepth int,
) any {
	return defaultFieldSampleValue(fd, visited, depth, maxDepth)
}

func defaultFieldSampleValue(
	fd protoreflect.FieldDescriptor,
	visited map[string]bool,
	depth, maxDepth int,
) any {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return false
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return 0
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return "0"
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return 0.0
	case protoreflect.StringKind:
		return ""
	case protoreflect.BytesKind:
		return ""
	case protoreflect.EnumKind:
		if ed := fd.Enum(); ed != nil && ed.Values().Len() > 0 {
			return string(ed.Values().Get(0).Name())
		}
		return 0
	case protoreflect.MessageKind, protoreflect.GroupKind:
		subMsg := fd.Message()
		if subMsg == nil {
			return map[string]any{}
		}
		subName := string(subMsg.FullName())
		if visited[subName] || depth >= maxDepth {
			return map[string]any{}
		}
		visited[subName] = true
		obj := buildDefaultObject(subMsg, visited, depth+1, maxDepth)
		delete(visited, subName)
		return obj
	default:
		return nil
	}
}

func defaultMapKey(kDesc protoreflect.FieldDescriptor) string {
	switch kDesc.Kind() {
	case protoreflect.BoolKind:
		return "false"
	case protoreflect.StringKind:
		return "key"
	default:
		return "0"
	}
}

type goldenSuiteItemRow struct {
	SuiteID      uint64 `gorm:"column:suite_id"`
	SuiteCode    string `gorm:"column:suite_code"`
	SuiteName    string `gorm:"column:suite_name"`
	ItemID       uint64 `gorm:"column:item_id"`
	OrderNo      int    `gorm:"column:order_no"`
	ItemName     string `gorm:"column:item_name"`
	RawReqPacket []byte `gorm:"column:raw_req_packet"`
	RawRspPacket []byte `gorm:"column:raw_rsp_packet"`
}

// MatchGoldenSamples 匹配 ACTIVE 状态的黄金测试集样本并解码为可读 JSON（屏蔽 ARCHIVED 套件）
func MatchGoldenSamples(
	db *gorm.DB,
	serviceName, operation string,
	reqDesc, rspDesc protoreflect.MessageDescriptor,
) []GoldenSampleDTO {
	out := make([]GoldenSampleDTO, 0)
	if db == nil || serviceName == "" || operation == "" {
		return out
	}

	var rows []goldenSuiteItemRow
	err := db.Table("traefik_golden_suite_items AS i").
		Select("s.id AS suite_id, s.code AS suite_code, s.name AS suite_name, i.id AS item_id, i.order_no, i.name AS item_name, i.raw_req_packet, i.raw_rsp_packet").
		Joins("JOIN traefik_golden_suites AS s ON s.id = i.suite_id").
		Where("s.status = ? AND i.service_name = ? AND i.operation = ?", models.GoldenSuiteStatusActive, serviceName, operation).
		Order("s.id DESC, i.order_no ASC, i.id ASC").
		Limit(5).
		Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return out
	}

	for _, row := range rows {
		dto := GoldenSampleDTO{
			SuiteID:   row.SuiteID,
			SuiteCode: row.SuiteCode,
			SuiteName: row.SuiteName,
			ItemID:    row.ItemID,
			OrderNo:   row.OrderNo,
			Remark:    row.ItemName,
		}

		reqPayload := extractCrosPayloadBytes(row.RawReqPacket, false)
		rspPayload := extractCrosPayloadBytes(row.RawRspPacket, true)

		reqJSON, reqOK := decodeProtoBytesToJSON(reqPayload, reqDesc)
		rspJSON, rspOK := decodeProtoBytesToJSON(rspPayload, rspDesc)

		switch {
		case reqDesc == nil && rspDesc == nil:
			dto.DecodeStatus = "NO_SCHEMA"
		case reqOK || rspOK:
			dto.DecodeStatus = "DECODED_JSON"
			dto.ReqJSON = reqJSON
			dto.RspJSON = rspJSON
		default:
			dto.DecodeStatus = "DECODE_ERROR"
		}
		out = append(out, dto)
	}

	return out
}

func extractCrosPayloadBytes(raw []byte, allowMultiSeg bool) []byte {
	if len(raw) == 0 {
		return nil
	}
	if allowMultiSeg {
		frames, err := crosrpc.SplitFrames(raw)
		if err == nil && len(frames) > 0 {
			var buf bytes.Buffer
			for _, f := range frames {
				if parsed, parseErr := crosrpc.ParsePacket(f); parseErr == nil && parsed.PayloadOffset <= len(f) {
					buf.Write(f[parsed.PayloadOffset:])
				}
			}
			if buf.Len() > 0 {
				return buf.Bytes()
			}
		}
	}
	parsed, err := crosrpc.ParsePacket(raw)
	if err == nil && parsed.PayloadOffset <= len(raw) {
		return raw[parsed.PayloadOffset:]
	}
	return raw
}

func decodeProtoBytesToJSON(payload []byte, desc protoreflect.MessageDescriptor) (json.RawMessage, bool) {
	if desc == nil {
		return nil, false
	}
	if len(payload) == 0 {
		msg := dynamicpb.NewMessage(desc)
		b, err := (protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: false, Multiline: true, Indent: "  "}).Marshal(msg)
		if err != nil {
			return nil, false
		}
		return json.RawMessage(b), true
	}

	msg := dynamicpb.NewMessage(desc)
	if err := proto.Unmarshal(payload, msg); err != nil {
		return nil, false
	}
	opts := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: false,
		Multiline:       true,
		Indent:          "  ",
	}
	b, err := opts.Marshal(msg)
	if err != nil {
		return nil, false
	}
	return json.RawMessage(b), true
}
