package decoder

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-traefik/config"
	"code-traefik/models"
	"code-traefik/pkg/crosrpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func setupIsolatedDecoderDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}
	return db
}

// buildTestOrderDescriptorBytes 构造合法的 FileDescriptorSet 二进制字节流
func buildTestOrderDescriptorBytes(t *testing.T, pkgName, svcName, methodName string, extraField bool) []byte {
	t.Helper()

	reqFields := []*descriptorpb.FieldDescriptorProto{
		{
			Name:   proto.String("order_id"),
			Number: proto.Int32(1),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_UINT64.Enum(),
		},
		{
			Name:   proto.String("buyer_name"),
			Number: proto.Int32(2),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
		},
	}
	if extraField {
		reqFields = append(reqFields, &descriptorpb.FieldDescriptorProto{
			Name:   proto.String("coupon_code"),
			Number: proto.Int32(3),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
		})
	}

	reqTypeName := "." + svcName + "Req"
	rspTypeName := "." + svcName + "Rsp"
	if pkgName != "" {
		reqTypeName = "." + pkgName + "." + svcName + "Req"
		rspTypeName = "." + pkgName + "." + svcName + "Rsp"
	}

	fileProto := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("dophi/order/v1/trade.proto"),
		Package: proto.String(pkgName),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:  proto.String(svcName + "Req"),
				Field: reqFields,
			},
			{
				Name: proto.String(svcName + "Rsp"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:   proto.String("accepted"),
						Number: proto.Int32(1),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_BOOL.Enum(),
					},
				},
			},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: proto.String(svcName),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:       proto.String(methodName),
						InputType:  proto.String(reqTypeName),
						OutputType: proto.String(rspTypeName),
					},
				},
			},
		},
	}

	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{fileProto},
	}
	raw, err := proto.Marshal(fds)
	if err != nil {
		t.Fatalf("序列化测试 FileDescriptorSet 失败: %v", err)
	}
	return raw
}

// buildTestCrosReqFrame 构造完整的 24B 公共头 + 256B V05 请求会话头 + Payload 的 CROS RPC 请求帧
func buildTestCrosReqFrame(sno uint64, serviceName, operation string, payload []byte) []byte {
	const reqHeadLen = 256
	reqHead := make([]byte, reqHeadLen)
	binary.BigEndian.PutUint16(reqHead[0:2], 5) // V05
	binary.BigEndian.PutUint16(reqHead[2:4], reqHeadLen)
	copy(reqHead[8:72], []byte(serviceName))
	copy(reqHead[72:136], []byte(operation))
	reqHead[136] = crosrpc.ReqTypeSync

	var comm crosrpc.CommHead
	copy(comm.Magic[:], []byte(crosrpc.MagicPrefix))
	comm.TransportProto = crosrpc.TransportTCP
	comm.SNO = sno
	comm.HeadLen = reqHeadLen
	comm.Length = uint32(reqHeadLen + len(payload))
	comm.MsgType = crosrpc.MsgTypeReq
	comm.IsPacked = 1

	frame := make([]byte, 0, crosrpc.CommHeadLen+reqHeadLen+len(payload))
	frame = append(frame, comm.Marshal()...)
	frame = append(frame, reqHead...)
	frame = append(frame, payload...)
	return frame
}

func TestProtoSchemaUploadAndDashServiceName(t *testing.T) {
	db := setupIsolatedDecoderDB(t)
	reg, err := NewRegistry(db)
	if err != nil {
		t.Fatalf("NewRegistry 失败: %v", err)
	}

	// 1. 上传合法 .pb: package = "dophi.order.v1", service = "TradeService", method = "CreateOrder"
	validPB := buildTestOrderDescriptorBytes(t, "dophi.order.v1", "TradeService", "CreateOrder", false)
	schema, ops, err := reg.UploadSchema(UploadSchemaInput{
		Name:         "order_trade",
		VersionTag:   "v1.0.0",
		FileName:     "../../unsafe/path/order_trade.pb",
		Description:  "订单交易核心 IDL",
		RawBytes:     validPB,
		AutoActivate: false,
		OperatorID:   1,
	})
	if err != nil {
		t.Fatalf("上传合法 .pb 失败: %v", err)
	}
	if schema.Version != 1 || schema.FileName != "order_trade.pb" {
		t.Fatalf("版本号或净化文件名不符合预期: version=%d file_name=%q", schema.Version, schema.FileName)
	}
	if len(ops) != 1 {
		t.Fatalf("期望提取 1 条操作映射，实际为 %d", len(ops))
	}
	if ops[0].ServiceName != "dophi.order.v1-TradeService" || ops[0].Operation != "CreateOrder" {
		t.Fatalf("A-07 服务名映射错误: service_name=%q operation=%q, 期望 dophi.order.v1-TradeService / CreateOrder",
			ops[0].ServiceName, ops[0].Operation)
	}

	// 2. 上传 > 4MB 文件 -> ErrDescriptorTooLarge (400)
	oversizeBytes := bytes.Repeat([]byte{0x0a}, MaxDescriptorFileSize+1)
	if _, _, err := reg.UploadSchema(UploadSchemaInput{
		Name:     "oversize_pb",
		FileName: "huge.pb",
		RawBytes: oversizeBytes,
	}); !errors.Is(err, ErrDescriptorTooLarge) {
		t.Fatalf("上传超 4MB 文件期望返回 ErrDescriptorTooLarge，实际为: %v", err)
	}

	// 3. 上传损坏字节流 -> ErrInvalidDescriptorProto (400)
	if _, _, err := reg.UploadSchema(UploadSchemaInput{
		Name:     "corrupted_pb",
		FileName: "bad.pb",
		RawBytes: []byte{0xff, 0x00, 0x13, 0x37, 0xde, 0xad, 0xbe, 0xef},
	}); !errors.Is(err, ErrInvalidDescriptorProto) {
		t.Fatalf("上传损坏 .pb 期望返回 ErrInvalidDescriptorProto，实际为: %v", err)
	}
}

func TestProtoSchemaActivateConflictAndHistoryImmutability(t *testing.T) {
	db := setupIsolatedDecoderDB(t)
	reg, err := NewRegistry(db)
	if err != nil {
		t.Fatalf("NewRegistry 失败: %v", err)
	}

	// 1. 上传并激活 order.pb v1
	pbV1 := buildTestOrderDescriptorBytes(t, "dophi.order.v1", "TradeService", "CreateOrder", false)
	orderV1, _, err := reg.UploadSchema(UploadSchemaInput{
		Name:         "order.pb",
		FileName:     "order.pb",
		RawBytes:     pbV1,
		AutoActivate: true,
	})
	if err != nil {
		t.Fatalf("上传并自动激活 order.pb v1 失败: %v", err)
	}
	if !orderV1.IsActive || orderV1.Version != 1 {
		t.Fatalf("期望 order.pb v1 处于激活态，实际 is_active=%v version=%d", orderV1.IsActive, orderV1.Version)
	}

	// 模拟已落盘一条历史 TrafficRecord，快照记录 schema_id=orderV1.ID, schema_version=1
	now := time.Now().UTC()
	v1Int := orderV1.Version
	histRec := models.TrafficRecord{
		CreatedAt:       now,
		SessionID:       501,
		SeqNo:           1,
		AgentID:         1,
		ProcessID:       1,
		CapturedPID:     2001,
		ConnKey:         "127.0.0.1:50000-127.0.0.1:9001",
		Direction:       models.TrafficDirIngress,
		ExchangeType:    models.ExchangeUnary,
		SrcAddr:         "127.0.0.1:50000",
		DstAddr:         "127.0.0.1:9001",
		ReqCapturedAtNs: now.UnixNano(),
		SNO:             models.U64Numeric(10001),
		ServiceName:     "dophi.order.v1-TradeService",
		Operation:       "CreateOrder",
		HeadersJSON:     datatypes.JSON([]byte(`{}`)),
		SchemaID:        &orderV1.ID,
		SchemaVersion:   &v1Int,
		DecodeStatus:    DecodeStatusDecodedJSON,
		PayloadJSON:     datatypes.JSON([]byte(`{"req":{"buyer_name":"Alice"}}`)),
	}
	if err := db.Create(&histRec).Error; err != nil {
		t.Fatalf("写入历史 TrafficRecord 失败: %v", err)
	}

	// 上传另一名称 trade_dup.pb v1（含相同接口）并尝试激活 -> 409 Conflict + 冲突明细
	dupV1, _, err := reg.UploadSchema(UploadSchemaInput{
		Name:         "trade_dup.pb",
		FileName:     "trade_dup.pb",
		RawBytes:     pbV1,
		AutoActivate: false,
	})
	if err != nil {
		t.Fatalf("上传未激活的 trade_dup.pb 失败: %v", err)
	}
	_, _, _, err = reg.ActivateSchemaVersion(dupV1.ID)
	if !errors.Is(err, ErrSchemaOperationConflict) {
		t.Fatalf("跨描述集激活冲突接口期望返回 ErrSchemaOperationConflict，实际为: %v", err)
	}
	var conflictErr *SchemaConflictError
	if !errors.As(err, &conflictErr) || len(conflictErr.Conflicts) != 1 {
		t.Fatalf("期望返回 1 条冲突明细，实际为: %+v", err)
	}
	if conflictErr.Conflicts[0].ConflictSchemaName != "order.pb" || conflictErr.Conflicts[0].ServiceName != "dophi.order.v1-TradeService" {
		t.Fatalf("冲突明细内容不准确: %+v", conflictErr.Conflicts[0])
	}

	// 2. 上传同名 order.pb v2 并切换激活 -> 成功，且历史 TrafficRecord.SchemaVersion 保持 1 不变
	pbV2 := buildTestOrderDescriptorBytes(t, "dophi.order.v1", "TradeService", "CreateOrder", true)
	orderV2, _, err := reg.UploadSchema(UploadSchemaInput{
		Name:         "order.pb",
		FileName:     "order_v2.pb",
		RawBytes:     pbV2,
		AutoActivate: false,
	})
	if err != nil {
		t.Fatalf("上传 order.pb v2 失败: %v", err)
	}
	if orderV2.Version != 2 {
		t.Fatalf("同名第二次上传期望 version=2，实际为 %d", orderV2.Version)
	}
	if _, afterV2, count, err := reg.ActivateSchemaVersion(orderV2.ID); err != nil || !afterV2.IsActive || count != 1 {
		t.Fatalf("切换激活 order.pb v2 失败: err=%v active=%v count=%d", err, afterV2, count)
	}

	var verifyHist models.TrafficRecord
	if err := db.Where("id = ? AND created_at = ?", histRec.ID, now).First(&verifyHist).Error; err != nil {
		t.Fatalf("查询历史 TrafficRecord 失败: %v", err)
	}
	if verifyHist.SchemaVersion == nil || *verifyHist.SchemaVersion != 1 {
		t.Fatalf("切换生效版本后历史记录 SchemaVersion 被篡改: 期望 1，实际为 %v", verifyHist.SchemaVersion)
	}

	// 3. 尝试删除处于 is_active=true 的 v2 -> 409 Conflict；删除已退役的 v1 -> 成功
	if _, err := reg.DeleteSchemaVersion(orderV2.ID); !errors.Is(err, ErrDeleteActiveSchema) {
		t.Fatalf("删除生效版本期望返回 ErrDeleteActiveSchema，实际为: %v", err)
	}
	if _, err := reg.DeleteSchemaVersion(orderV1.ID); err != nil {
		t.Fatalf("删除非生效旧版本 v1 期望成功，实际报错: %v", err)
	}
}

func TestDynamicPBDecodeAndFallbackHex(t *testing.T) {
	db := setupIsolatedDecoderDB(t)
	reg, err := NewRegistry(db)
	if err != nil {
		t.Fatalf("NewRegistry 失败: %v", err)
	}

	pbBytes := buildTestOrderDescriptorBytes(t, "dophi.order.v1", "TradeService", "CreateOrder", false)
	if _, _, err := reg.UploadSchema(UploadSchemaInput{
		Name:         "order_runtime.pb",
		FileName:     "order_runtime.pb",
		RawBytes:     pbBytes,
		AutoActivate: true,
	}); err != nil {
		t.Fatalf("上传并激活描述集失败: %v", err)
	}

	// 构造含 uint64 = MaxUint64 (18446744073709551615) 的真实 Protobuf 二进制载荷
	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(pbBytes, &fds); err != nil {
		t.Fatalf("Unmarshal fds 失败: %v", err)
	}
	files, err := protodesc.NewFiles(&fds)
	if err != nil {
		t.Fatalf("NewFiles 失败: %v", err)
	}
	desc, err := files.FindDescriptorByName("dophi.order.v1.TradeServiceReq")
	if err != nil {
		t.Fatalf("查找 TradeServiceReq 描述符失败: %v", err)
	}
	msgDesc := desc.(protoreflect.MessageDescriptor)
	dynMsg := dynamicpb.NewMessage(msgDesc)
	dynMsg.Set(msgDesc.Fields().ByName("order_id"), protoreflect.ValueOfUint64(math.MaxUint64))
	dynMsg.Set(msgDesc.Fields().ByName("buyer_name"), protoreflect.ValueOfString("张三"))
	payloadBytes, err := proto.Marshal(dynMsg)
	if err != nil {
		t.Fatalf("序列化动态测试请求失败: %v", err)
	}

	// 1. 输入匹配生效 .pb 且含 uint64 大整数的 CROS RPC 请求帧 (SC-04)
	matchedFrame := buildTestCrosReqFrame(0x000000010009A2B3, "dophi.order.v1-TradeService", "CreateOrder", payloadBytes)
	decoded, err := reg.DecodeCrosFrame(matchedFrame, false)
	if err != nil {
		t.Fatalf("DecodeCrosFrame 失败: %v", err)
	}
	if decoded.Decode.DecodeStatus != DecodeStatusDecodedJSON {
		t.Fatalf("期望 DecodeStatus == %q，实际为 %q", DecodeStatusDecodedJSON, decoded.Decode.DecodeStatus)
	}

	var parsedJSON map[string]any
	if err := json.Unmarshal(decoded.Decode.JSONBytes, &parsedJSON); err != nil {
		t.Fatalf("解析输出 JSON 失败: %v (raw=%s)", err, string(decoded.Decode.JSONBytes))
	}
	if parsedJSON["order_id"] != "18446744073709551615" {
		t.Fatalf("uint64 大整数未编码为无损十进制字符串: 期望 \"18446744073709551615\"，实际为 %T (%v)",
			parsedJSON["order_id"], parsedJSON["order_id"])
	}
	if parsedJSON["buyer_name"] != "张三" {
		t.Fatalf("buyer_name 解码错误: %v", parsedJSON["buyer_name"])
	}

	// 2. 输入未注册接口的 CROS RPC 帧 -> NO_SCHEMA_RAW_HEX，保留完整协议头且不报错
	unregisteredFrame := buildTestCrosReqFrame(998877, "dophi.unknown.v1-GhostSvc", "Ping", []byte{0xde, 0xad, 0xbe, 0xef})
	fallback, err := reg.DecodeCrosFrame(unregisteredFrame, false)
	if err != nil {
		t.Fatalf("未注册接口降级解码不应报错，实际报错: %v", err)
	}
	if fallback.Decode.DecodeStatus != DecodeStatusNoSchemaRawHex {
		t.Fatalf("期望降级状态为 %q，实际为 %q", DecodeStatusNoSchemaRawHex, fallback.Decode.DecodeStatus)
	}
	if fallback.CommHead.SNO != 998877 || fallback.ServiceName != "dophi.unknown.v1-GhostSvc" || fallback.Operation != "Ping" {
		t.Fatalf("降级后协议头丢失: sno=%d svc=%q op=%q", fallback.CommHead.SNO, fallback.ServiceName, fallback.Operation)
	}
	if fallback.Decode.RawHex != "deadbeef" {
		t.Fatalf("期望 RawHex == \"deadbeef\"，实际为 %q", fallback.Decode.RawHex)
	}
}
