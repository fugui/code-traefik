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
	schema, ops, _, err := reg.UploadSchema(UploadSchemaInput{
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
	if ops[0].ServiceName != "dophi.order.v1.TradeService" || ops[0].Operation != "CreateOrder" {
		t.Fatalf("A-07 服务名映射错误: service_name=%q operation=%q, 期望 dophi.order.v1.TradeService / CreateOrder",
			ops[0].ServiceName, ops[0].Operation)
	}

	// 2. 上传 > 16MB 文件 -> ErrDescriptorTooLarge (400)
	oversizeBytes := bytes.Repeat([]byte{0x0a}, MaxDescriptorFileSize+1)
	if _, _, _, err := reg.UploadSchema(UploadSchemaInput{
		Name:     "oversize_pb",
		FileName: "huge.pb",
		RawBytes: oversizeBytes,
	}); !errors.Is(err, ErrDescriptorTooLarge) {
		t.Fatalf("上传超 16MB 文件期望返回 ErrDescriptorTooLarge，实际为: %v", err)
	}

	// 3. 上传损坏字节流 -> ErrInvalidDescriptorProto (400)
	if _, _, _, err := reg.UploadSchema(UploadSchemaInput{
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
	orderV1, _, _, err := reg.UploadSchema(UploadSchemaInput{
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
		ServiceName:     "dophi.order.v1.TradeService",
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
	dupV1, _, _, err := reg.UploadSchema(UploadSchemaInput{
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
	if conflictErr.Conflicts[0].ConflictSchemaName != "order.pb" || conflictErr.Conflicts[0].ServiceName != "dophi.order.v1.TradeService" {
		t.Fatalf("冲突明细内容不准确: %+v", conflictErr.Conflicts[0])
	}

	// 2. 上传同名 order.pb v2 并切换激活 -> 成功，且历史 TrafficRecord.SchemaVersion 保持 1 不变
	pbV2 := buildTestOrderDescriptorBytes(t, "dophi.order.v1", "TradeService", "CreateOrder", true)
	orderV2, _, _, err := reg.UploadSchema(UploadSchemaInput{
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
	if _, _, _, err := reg.UploadSchema(UploadSchemaInput{
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
	matchedFrame := buildTestCrosReqFrame(0x000000010009A2B3, "dophi.order.v1.TradeService", "CreateOrder", payloadBytes)
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
	unregisteredFrame := buildTestCrosReqFrame(998877, "dophi.unknown.v1.GhostSvc", "Ping", []byte{0xde, 0xad, 0xbe, 0xef})
	fallback, err := reg.DecodeCrosFrame(unregisteredFrame, false)
	if err != nil {
		t.Fatalf("未注册接口降级解码不应报错，实际报错: %v", err)
	}
	if fallback.Decode.DecodeStatus != DecodeStatusNoSchemaRawHex {
		t.Fatalf("期望降级状态为 %q，实际为 %q", DecodeStatusNoSchemaRawHex, fallback.Decode.DecodeStatus)
	}
	if fallback.CommHead.SNO != 998877 || fallback.ServiceName != "dophi.unknown.v1.GhostSvc" || fallback.Operation != "Ping" {
		t.Fatalf("降级后协议头丢失: sno=%d svc=%q op=%q", fallback.CommHead.SNO, fallback.ServiceName, fallback.Operation)
	}
	if fallback.Decode.RawHex != "deadbeef" {
		t.Fatalf("期望 RawHex == \"deadbeef\"，实际为 %q", fallback.Decode.RawHex)
	}
}

// buildTestBaseLibDescriptorBytes 构造 0 Service 纯公共消息包 common-base.pb (引用 google/protobuf/timestamp.proto)
func buildTestBaseLibDescriptorBytes(t *testing.T, withMoney bool) []byte {
	t.Helper()

	baseHeaderFields := []*descriptorpb.FieldDescriptorProto{
		{
			Name:   proto.String("trace_id"),
			Number: proto.Int32(1),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
		},
		{
			Name:     proto.String("timestamp"),
			Number:   proto.Int32(2),
			Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
			TypeName: proto.String(".google.protobuf.Timestamp"),
		},
	}

	messages := []*descriptorpb.DescriptorProto{
		{
			Name:  proto.String("BaseHeader"),
			Field: baseHeaderFields,
		},
	}

	if withMoney {
		moneyFields := []*descriptorpb.FieldDescriptorProto{
			{
				Name:   proto.String("currency"),
				Number: proto.Int32(1),
				Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
			},
			{
				Name:   proto.String("amount"),
				Number: proto.Int32(2),
				Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:   descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
			},
		}
		messages = append(messages, &descriptorpb.DescriptorProto{
			Name:  proto.String("Money"),
			Field: moneyFields,
		})
	}

	fileProto := &descriptorpb.FileDescriptorProto{
		Syntax:      proto.String("proto3"),
		Name:        proto.String("common/base.proto"),
		Package:     proto.String("common"),
		Dependency:  []string{"google/protobuf/timestamp.proto"},
		MessageType: messages,
		Service:     []*descriptorpb.ServiceDescriptorProto{}, // 0 Service
	}

	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{fileProto},
	}
	raw, err := proto.Marshal(fds)
	if err != nil {
		t.Fatalf("序列化 common-base.pb 失败: %v", err)
	}
	return raw
}

// buildTestOrderWithBaseDescriptorBytes 构造不带 --include_imports 的拆包 order.pb (外部依赖 common/base.proto)
func buildTestOrderWithBaseDescriptorBytes(t *testing.T, includeCancel bool) []byte {
	t.Helper()

	reqFields := []*descriptorpb.FieldDescriptorProto{
		{
			Name:     proto.String("header"),
			Number:   proto.Int32(1),
			Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
			TypeName: proto.String(".common.BaseHeader"),
		},
		{
			Name:     proto.String("total_fee"),
			Number:   proto.Int32(2),
			Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
			TypeName: proto.String(".common.Money"),
		},
		{
			Name:   proto.String("order_id"),
			Number: proto.Int32(3),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
		},
	}

	rspFields := []*descriptorpb.FieldDescriptorProto{
		{
			Name:     proto.String("header"),
			Number:   proto.Int32(1),
			Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
			TypeName: proto.String(".common.BaseHeader"),
		},
		{
			Name:   proto.String("success"),
			Number: proto.Int32(2),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_BOOL.Enum(),
		},
	}

	messages := []*descriptorpb.DescriptorProto{
		{
			Name:  proto.String("CreateOrderReq"),
			Field: reqFields,
		},
		{
			Name:  proto.String("CreateOrderRsp"),
			Field: rspFields,
		},
	}

	methods := []*descriptorpb.MethodDescriptorProto{
		{
			Name:       proto.String("CreateOrder"),
			InputType:  proto.String(".order.CreateOrderReq"),
			OutputType: proto.String(".order.CreateOrderRsp"),
		},
	}

	if includeCancel {
		messages = append(messages, &descriptorpb.DescriptorProto{
			Name: proto.String("CancelOrderReq"),
			Field: []*descriptorpb.FieldDescriptorProto{
				{
					Name:   proto.String("order_id"),
					Number: proto.Int32(1),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				},
			},
		}, &descriptorpb.DescriptorProto{
			Name: proto.String("CancelOrderRsp"),
			Field: []*descriptorpb.FieldDescriptorProto{
				{
					Name:   proto.String("cancelled"),
					Number: proto.Int32(1),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_BOOL.Enum(),
				},
			},
		})

		methods = append(methods, &descriptorpb.MethodDescriptorProto{
			Name:       proto.String("CancelOrder"),
			InputType:  proto.String(".order.CancelOrderReq"),
			OutputType: proto.String(".order.CancelOrderRsp"),
		})
	}

	fileProto := &descriptorpb.FileDescriptorProto{
		Syntax:      proto.String("proto3"),
		Name:        proto.String("order/order.proto"),
		Package:     proto.String("order"),
		Dependency:  []string{"common/base.proto"},
		MessageType: messages,
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name:   proto.String("OrderService"),
				Method: methods,
			},
		},
	}

	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{fileProto},
	}
	raw, err := proto.Marshal(fds)
	if err != nil {
		t.Fatalf("序列化 order.pb 失败: %v", err)
	}
	return raw
}

// buildTestPaymentWithBundledBaseDescriptorBytes 构造带 --include_imports 的 payment.pb (内嵌 common/base.proto 且 base.proto 含 HealthService)
func buildTestPaymentWithBundledBaseDescriptorBytes(t *testing.T) []byte {
	t.Helper()

	baseFile := &descriptorpb.FileDescriptorProto{
		Syntax:     proto.String("proto3"),
		Name:       proto.String("common/base.proto"),
		Package:    proto.String("common"),
		Dependency: []string{"google/protobuf/timestamp.proto"},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("BaseHeader"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:   proto.String("trace_id"),
						Number: proto.Int32(1),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
					{
						Name:     proto.String("timestamp"),
						Number:   proto.Int32(2),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".google.protobuf.Timestamp"),
					},
				},
			},
			{
				Name: proto.String("Money"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:   proto.String("currency"),
						Number: proto.Int32(1),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
					{
						Name:   proto.String("amount"),
						Number: proto.Int32(2),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
					},
				},
			},
			{
				Name: proto.String("HealthCheckReq"),
			},
			{
				Name: proto.String("HealthCheckRsp"),
			},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: proto.String("HealthService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:       proto.String("CheckHealth"),
						InputType:  proto.String(".common.HealthCheckReq"),
						OutputType: proto.String(".common.HealthCheckRsp"),
					},
				},
			},
		},
	}

	paymentFile := &descriptorpb.FileDescriptorProto{
		Syntax:     proto.String("proto3"),
		Name:       proto.String("payment/payment.proto"),
		Package:    proto.String("payment"),
		Dependency: []string{"common/base.proto"},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("PayReq"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("amount"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".common.Money"),
					},
				},
			},
			{
				Name: proto.String("PayRsp"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("header"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".common.BaseHeader"),
					},
				},
			},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: proto.String("PaymentService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:       proto.String("Pay"),
						InputType:  proto.String(".payment.PayReq"),
						OutputType: proto.String(".payment.PayRsp"),
					},
				},
			},
		},
	}

	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{baseFile, paymentFile},
	}
	raw, err := proto.Marshal(fds)
	if err != nil {
		t.Fatalf("序列化 payment.pb 失败: %v", err)
	}
	return raw
}

func TestCrossSchemaClosure_DeduplicateBundledBase_ZeroServiceBaseLib_AndOwnFileOpsOnly(t *testing.T) {
	db := setupIsolatedDecoderDB(t)
	reg, err := NewRegistry(db)
	if err != nil {
		t.Fatalf("NewRegistry 失败: %v", err)
	}

	// 1. common-base.pb (0 Service, Category = BASE_LIB, 引用 google/protobuf/timestamp.proto 但未手动上传 WKT)
	baseBytes := buildTestBaseLibDescriptorBytes(t, true)
	baseSchema, baseOps, _, err := reg.UploadSchema(UploadSchemaInput{
		Name:         "common-base",
		Category:     models.ProtoCategoryBaseLib,
		VersionTag:   "v1.0.0",
		FileName:     "common-base.pb",
		RawBytes:     baseBytes,
		AutoActivate: true,
	})
	if err != nil {
		t.Fatalf("上传并激活 common-base.pb 失败: %v", err)
	}
	if baseSchema.OperationCount != 0 || len(baseOps) != 0 {
		t.Fatalf("BASE_LIB 期望 OperationCount == 0, 实际为 %d", baseSchema.OperationCount)
	}
	if !baseSchema.IsActive {
		t.Fatalf("common-base 期望处于激活态")
	}

	// 2. order.pb 不带 --include_imports 拆包编译 (外部依赖 common/base.proto)
	orderBytes := buildTestOrderWithBaseDescriptorBytes(t, false)
	orderSchema, _, _, err := reg.UploadSchema(UploadSchemaInput{
		Name:         "order-biz",
		Category:     models.ProtoCategoryBusiness,
		VersionTag:   "v1.0.0",
		FileName:     "order.pb",
		RawBytes:     orderBytes,
		AutoActivate: true,
	})
	if err != nil {
		t.Fatalf("上传并激活 order.pb 失败: %v", err)
	}
	if !orderSchema.IsActive {
		t.Fatalf("order.pb 期望处于激活态")
	}
	var orderProtoFiles []string
	_ = json.Unmarshal(orderSchema.ProtoFiles, &orderProtoFiles)
	if len(orderProtoFiles) != 1 || orderProtoFiles[0] != "order/order.proto" {
		t.Fatalf("order.pb OwnProtoFiles 期望 [order/order.proto], 实际: %v", orderProtoFiles)
	}
	var orderExtImports []string
	_ = json.Unmarshal(orderSchema.ExternalImports, &orderExtImports)
	if len(orderExtImports) != 1 || orderExtImports[0] != "common/base.proto" {
		t.Fatalf("order.pb ExternalImports 期望包含 [common/base.proto], 实际: %v", orderExtImports)
	}

	// 3. payment.pb 带 --include_imports 打包了 common/base.proto (且 base.proto 含 HealthService)
	payBytes := buildTestPaymentWithBundledBaseDescriptorBytes(t)
	paySchema, payOps, _, err := reg.UploadSchema(UploadSchemaInput{
		Name:         "payment-biz",
		Category:     models.ProtoCategoryBusiness,
		VersionTag:   "v1.0.0",
		FileName:     "payment.pb",
		RawBytes:     payBytes,
		AutoActivate: true,
	})
	if err != nil {
		t.Fatalf("上传并激活带 bundled base 的 payment.pb 失败 (可能去重冲突或 file already registered): %v", err)
	}
	if !paySchema.IsActive {
		t.Fatalf("payment.pb 期望处于激活态")
	}
	// 验证仅提取自有 Service (PaymentService)，不重复提取 base.proto 的 HealthService
	if len(payOps) != 1 || payOps[0].ServiceName != "payment.PaymentService" || payOps[0].Operation != "Pay" {
		t.Fatalf("payment.pb 提取的操作不符合预期，期望仅提取 1 个 payment.PaymentService:Pay，实际为 %d 条: %+v", len(payOps), payOps)
	}
}

// buildTestTradeBaseDescriptorBytes 构造 trade/base.proto 基础依赖库
func buildTestTradeBaseDescriptorBytes(t *testing.T, withMoney bool) []byte {
	t.Helper()
	messages := []*descriptorpb.DescriptorProto{
		{
			Name: proto.String("TradeHeader"),
			Field: []*descriptorpb.FieldDescriptorProto{
				{
					Name:   proto.String("trace_id"),
					Number: proto.Int32(1),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				},
			},
		},
	}
	if withMoney {
		messages = append(messages, &descriptorpb.DescriptorProto{
			Name: proto.String("TradeMoney"),
			Field: []*descriptorpb.FieldDescriptorProto{
				{
					Name:   proto.String("amount"),
					Number: proto.Int32(1),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
				},
			},
		})
	}
	fileProto := &descriptorpb.FileDescriptorProto{
		Syntax:      proto.String("proto3"),
		Name:        proto.String("trade/base.proto"),
		Package:     proto.String("trade.base"),
		MessageType: messages,
	}
	fds := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fileProto}}
	raw, err := proto.Marshal(fds)
	if err != nil {
		t.Fatalf("序列化 trade/base.proto 失败: %v", err)
	}
	return raw
}

// buildTestTradeBizDescriptorBytes 构造依赖 trade/base.proto 的 trade/biz.proto
func buildTestTradeBizDescriptorBytes(t *testing.T) []byte {
	t.Helper()
	fileProto := &descriptorpb.FileDescriptorProto{
		Syntax:     proto.String("proto3"),
		Name:       proto.String("trade/biz.proto"),
		Package:    proto.String("trade.biz"),
		Dependency: []string{"trade/base.proto"},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("TradePayReq"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("money"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".trade.base.TradeMoney"),
					},
				},
			},
			{
				Name: proto.String("TradePayRsp"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("header"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".trade.base.TradeHeader"),
					},
				},
			},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: proto.String("TradePayService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:       proto.String("Pay"),
						InputType:  proto.String(".trade.biz.TradePayReq"),
						OutputType: proto.String(".trade.biz.TradePayRsp"),
					},
				},
			},
		},
	}
	fds := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fileProto}}
	raw, err := proto.Marshal(fds)
	if err != nil {
		t.Fatalf("序列化 trade/biz.proto 失败: %v", err)
	}
	return raw
}

func TestUploadNewVersion_InheritsDiffIgnorePaths_AndDraftBorrowsInactiveBaseLib(t *testing.T) {
	db := setupIsolatedDecoderDB(t)
	reg, err := NewRegistry(db)
	if err != nil {
		t.Fatalf("NewRegistry 失败: %v", err)
	}

	// 先激活 BASE_LIB
	baseBytes := buildTestBaseLibDescriptorBytes(t, true)
	_, _, _, err = reg.UploadSchema(UploadSchemaInput{
		Name:         "common-base",
		Category:     models.ProtoCategoryBaseLib,
		RawBytes:     baseBytes,
		AutoActivate: true,
	})
	if err != nil {
		t.Fatalf("上传 BASE_LIB 失败: %v", err)
	}

	// 1. 上传 order-service v1 (含 CreateOrder) 并配置 diff_ignore_paths = ["header.timestamp", "trace_id"]
	orderV1Bytes := buildTestOrderWithBaseDescriptorBytes(t, false)
	orderV1, opsV1, _, err := reg.UploadSchema(UploadSchemaInput{
		Name:         "order-service",
		Category:     models.ProtoCategoryBusiness,
		RawBytes:     orderV1Bytes,
		AutoActivate: true,
	})
	if err != nil {
		t.Fatalf("上传 order-service v1 失败: %v", err)
	}
	if len(opsV1) != 1 {
		t.Fatalf("order-service v1 方法数期望 1，实际: %d", len(opsV1))
	}
	if orderV1.Version != 1 {
		t.Fatalf("order-service v1 version 期望 1，实际: %d", orderV1.Version)
	}

	// 配置忽略路径
	_, _, err = reg.UpdateOperationIgnorePaths(opsV1[0].ID, []string{"header.timestamp", "trace_id"})
	if err != nil {
		t.Fatalf("更新 v1 忽略路径失败: %v", err)
	}

	// 上传 order-service v2 (含 CreateOrder 与新增 CancelOrder)
	orderV2Bytes := buildTestOrderWithBaseDescriptorBytes(t, true)
	orderV2, opsV2, inheritedCount, err := reg.UploadSchema(UploadSchemaInput{
		Name:         "order-service",
		Category:     models.ProtoCategoryBusiness,
		RawBytes:     orderV2Bytes,
		AutoActivate: false,
	})
	if err != nil {
		t.Fatalf("上传 order-service v2 失败: %v", err)
	}
	if orderV2.Version != 2 {
		t.Fatalf("order-service v2 version 期望 2，实际: %d", orderV2.Version)
	}
	if inheritedCount != 1 {
		t.Fatalf("继承的忽略路径方法数期望 1，实际: %d", inheritedCount)
	}

	// 校验 CreateOrder 继承了忽略配置，CancelOrder 为空
	for _, op := range opsV2 {
		if op.Operation == "CreateOrder" {
			var ignores []string
			_ = json.Unmarshal(op.DiffIgnorePaths, &ignores)
			if len(ignores) != 2 || ignores[0] != "header.timestamp" || ignores[1] != "trace_id" {
				t.Fatalf("CreateOrder 继承忽略路径不符: %v", ignores)
			}
		} else if op.Operation == "CancelOrder" {
			var ignores []string
			_ = json.Unmarshal(op.DiffIgnorePaths, &ignores)
			if len(ignores) != 0 {
				t.Fatalf("新增 CancelOrder 忽略路径应为空，实际为: %v", ignores)
			}
		}
	}

	// 2. 先以 auto_activate=false 上传未激活草稿 trade-base v1 (BASE_LIB)，再以 auto_activate=false 上传依赖它的 trade-biz v1
	tradeBaseBytes := buildTestTradeBaseDescriptorBytes(t, true)
	tradeBaseDraft, _, _, err := reg.UploadSchema(UploadSchemaInput{
		Name:         "trade-base",
		Category:     models.ProtoCategoryBaseLib,
		RawBytes:     tradeBaseBytes,
		AutoActivate: false,
	})
	if err != nil {
		t.Fatalf("上传草稿 trade-base 失败: %v", err)
	}
	if tradeBaseDraft.IsActive {
		t.Fatalf("trade-base 期望为未激活草稿")
	}

	tradeBizBytes := buildTestTradeBizDescriptorBytes(t)
	tradeBizDraft, _, _, err := reg.UploadSchema(UploadSchemaInput{
		Name:         "trade-biz",
		Category:     models.ProtoCategoryBusiness,
		RawBytes:     tradeBizBytes,
		AutoActivate: false,
	})
	if err != nil {
		t.Fatalf("借用未激活 BASE_LIB 上传草稿 trade-biz 失败: %v", err)
	}
	if tradeBizDraft.IsActive {
		t.Fatalf("trade-biz 期望为未激活草稿")
	}

	// 单独激活 trade-biz v1 (其前置 trade-base 尚未激活) -> CompileTrialPool 拦截返 409 broken_dependents
	_, _, _, err = reg.ActivateSchemaVersion(tradeBizDraft.ID)
	if err == nil {
		t.Fatalf("单独激活缺失前置基础库的 trade-biz 期望被拦截，但成功了！")
	}
	var brokenErr *SchemaBrokenDependentsError
	if !errors.As(err, &brokenErr) {
		t.Fatalf("期望返回 SchemaBrokenDependentsError，实际为: %T (%v)", err, err)
	}
}

func TestCompileTrialPool_BatchActivate_DeactivateReverseDep_AndUnifiedRuntimeDecode(t *testing.T) {
	db := setupIsolatedDecoderDB(t)
	reg, err := NewRegistry(db)
	if err != nil {
		t.Fatalf("NewRegistry 失败: %v", err)
	}

	// 上传未激活 trade-base v1 与 trade-biz v1
	tradeBaseBytes := buildTestTradeBaseDescriptorBytes(t, true)
	tradeBaseV1, _, _, err := reg.UploadSchema(UploadSchemaInput{
		Name:         "trade-base",
		Category:     models.ProtoCategoryBaseLib,
		RawBytes:     tradeBaseBytes,
		AutoActivate: false,
	})
	if err != nil {
		t.Fatalf("上传 trade-base v1 失败: %v", err)
	}

	tradeBizBytes := buildTestTradeBizDescriptorBytes(t)
	tradeBizV1, _, _, err := reg.UploadSchema(UploadSchemaInput{
		Name:         "trade-biz",
		Category:     models.ProtoCategoryBusiness,
		RawBytes:     tradeBizBytes,
		AutoActivate: false,
	})
	if err != nil {
		t.Fatalf("上传 trade-biz v1 失败: %v", err)
	}

	// 1. BatchActivate([trade-base v1, trade-biz v1]) 单事务原子激活两包
	activated, count, err := reg.BatchActivate([]uint64{tradeBaseV1.ID, tradeBizV1.ID})
	if err != nil {
		t.Fatalf("批量原子激活失败: %v", err)
	}
	if len(activated) != 2 || count < 1 {
		t.Fatalf("批量激活返回数量不符: %d, ops=%d", len(activated), count)
	}

	// 验证运行时联合动态解码
	compiled := reg.compiled.Load()
	if compiled == nil || compiled.Files == nil {
		t.Fatalf("CompiledRegistry.Files 未成功构建")
	}

	payReqDesc, err := compiled.Files.FindDescriptorByName("trade.biz.TradePayReq")
	if err != nil || payReqDesc == nil {
		t.Fatalf("未能在联合类型池中找到 trade.biz.TradePayReq: %v", err)
	}
	payMsgDesc := payReqDesc.(protoreflect.MessageDescriptor)
	payMsg := dynamicpb.NewMessage(payMsgDesc)

	// 设置嵌套消息 TradeMoney
	moneyFd := payMsgDesc.Fields().ByName("money")
	moneyMsg := dynamicpb.NewMessage(moneyFd.Message())
	moneyMsg.Set(moneyFd.Message().Fields().ByName("amount"), protoreflect.ValueOfInt64(9900))
	payMsg.Set(moneyFd, protoreflect.ValueOfMessage(moneyMsg))

	payPayload, err := proto.Marshal(payMsg)
	if err != nil {
		t.Fatalf("序列化测试 Pay 报文失败: %v", err)
	}

	decodeRes := reg.DecodePayloadToJSON("trade.biz.TradePayService", "Pay", crosrpc.MsgTypeReq, payPayload)
	if decodeRes.DecodeStatus != DecodeStatusDecodedJSON {
		t.Fatalf("DecodeStatus 期望 DECODED_JSON，实际为: %s", decodeRes.DecodeStatus)
	}
	var jsonMap map[string]any
	_ = json.Unmarshal(decodeRes.JSONBytes, &jsonMap)
	if jsonMap["money"] == nil {
		t.Fatalf("嵌套 money 字段解码缺失: %s", string(decodeRes.JSONBytes))
	}

	// 2. 尝试 Deactivate(trade-base v1) -> 被 CompileTrialPool 拦截返 broken_dependents
	_, _, err = reg.DeactivateSchemaVersion(tradeBaseV1.ID)
	if err == nil {
		t.Fatalf("停用被依赖的基础库 trade-base 期望被 CompileTrialPool 拦截，但成功执行了！")
	}
	var deactBroken *SchemaBrokenDependentsError
	if !errors.As(err, &deactBroken) {
		t.Fatalf("停用期望返回 SchemaBrokenDependentsError，实际: %v", err)
	}

	// 3. 上传删除了 Money 的破坏性版本 trade-base v2 并尝试单独激活 -> 被拦截
	brokenBaseBytes := buildTestTradeBaseDescriptorBytes(t, false) // withMoney = false
	tradeBaseV2, _, _, err := reg.UploadSchema(UploadSchemaInput{
		Name:         "trade-base",
		Category:     models.ProtoCategoryBaseLib,
		RawBytes:     brokenBaseBytes,
		AutoActivate: false,
	})
	if err != nil {
		t.Fatalf("上传 trade-base v2 失败: %v", err)
	}

	_, _, _, err = reg.ActivateSchemaVersion(tradeBaseV2.ID)
	if err == nil {
		t.Fatalf("激活破坏下游依赖的 trade-base v2 期望被拦截，但成功执行了！")
	}
	var actBroken *SchemaBrokenDependentsError
	if !errors.As(err, &actBroken) {
		t.Fatalf("激活期望返回 SchemaBrokenDependentsError，实际: %v", err)
	}

	// 4. 使用 GetOperationSchemaTree 分别对激活态与未激活草稿态提取字段字典树 -> 均 100% 成功展开
	var payOp models.ProtoOperation
	if err := db.Where("service_name = ? AND operation = ?", "trade.biz.TradePayService", "Pay").First(&payOp).Error; err != nil {
		t.Fatalf("查询 trade.biz.TradePayService:Pay 失败: %v", err)
	}

	treeActive, err := reg.GetOperationSchemaTree(payOp.ID)
	if err != nil {
		t.Fatalf("提取激活态方法字段字典树失败: %v", err)
	}
	if treeActive.ReqMessage == nil || len(treeActive.ReqMessage.Fields) == 0 {
		t.Fatalf("激活态字段字典树 ReqMessage 为空")
	}

	// 测试草稿态方法字典树
	treeDraft, err := reg.GetOperationSchemaTree(payOp.ID)
	if err != nil {
		t.Fatalf("提取方法字段字典树失败: %v", err)
	}
	if treeDraft.ReqMessage == nil {
		t.Fatalf("字典树 ReqMessage 为空")
	}
}
