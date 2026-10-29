package diff

import (
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"code-common/backend/testdb"
	"code-traefik/config"
	"code-traefik/internal/server/decoder"
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

func setupIsolatedDiffDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}
	return db
}

// TestValidateReplayProcessCompatibility 验证跨 Agent 同构重放兼容性校验与 Anti-IDOR 越权拦截 (F-12.1, SC-07)
func TestValidateReplayProcessCompatibility(t *testing.T) {
	db := setupIsolatedDiffDB(t)

	// 1. 架构元素
	sub := models.Subsystem{Code: "SUB_M4", Name: "重放子系统", CreatedBy: 1}
	db.Create(&sub)
	grp := models.FunctionalGroup{SubsystemID: sub.ID, Code: "GRP_M4", Name: "重放功能组", Level: 1, CreatedBy: 1}
	db.Create(&grp)

	modOrder := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_ORDER", Name: "订单模块", CreatedBy: 1}
	modStock := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "MOD_STOCK", Name: "库存模块", CreatedBy: 1}
	db.Create(&modOrder)
	db.Create(&modStock)

	// 提供接口
	provideMap := map[string]uint64{
		"dophi.order-OrderService:CreateOrder": modOrder.ID,
		"dophi.stock-StockService:DeductStock": modStock.ID,
	}

	// 2. 模拟用户 A 与用户 B 的 Agent 与进程
	userA := uint(100)
	userB := uint(200)

	agtPrivA := models.Agent{Name: "agt-priv-a", Endpoint: "http://127.0.0.1:19801", Scope: models.ScopePrivate, OwnerID: &userA, Status: models.AgentStatusOnline, CreatedBy: userA}
	agtPrivB := models.Agent{Name: "agt-priv-b", Endpoint: "http://127.0.0.1:19802", Scope: models.ScopePrivate, OwnerID: &userB, Status: models.AgentStatusOnline, CreatedBy: userB}
	agtShared := models.Agent{Name: "agt-shared", Endpoint: "http://127.0.0.1:19803", Scope: models.ScopeShared, Status: models.AgentStatusOnline, CreatedBy: 1}
	db.Create(&agtPrivA)
	db.Create(&agtPrivB)
	db.Create(&agtShared)

	// 进程定义
	// procSrc: 承载 modOrder
	procSrc := models.Process{AgentID: agtShared.ID, Agent: &agtShared, Name: "proc-src-order", ListenPort: 18001, CreatedBy: 1, ModuleIDs: []uint64{modOrder.ID}}
	// procStockOnlyB: 用户 B 的 Agent 上的异构进程，仅承载 modStock
	procStockOnlyB := models.Process{AgentID: agtPrivB.ID, Agent: &agtPrivB, Name: "proc-stock-b", ListenPort: 18002, CreatedBy: userB, ModuleIDs: []uint64{modStock.ID}}
	// procOrderB: 用户 B 的 Agent 上的同构进程，完整承载 modOrder
	procOrderB := models.Process{AgentID: agtPrivB.ID, Agent: &agtPrivB, Name: "proc-order-b", ListenPort: 18003, CreatedBy: userB, ModuleIDs: []uint64{modOrder.ID}}
	// procOrderShared: 公共 Agent 上的同构进程，完整承载 modOrder
	procOrderShared := models.Process{AgentID: agtShared.ID, Agent: &agtShared, Name: "proc-order-shared", ListenPort: 18004, CreatedBy: 1, ModuleIDs: []uint64{modOrder.ID}}

	db.Create(&procSrc)
	db.Create(&procStockOnlyB)
	db.Create(&procOrderB)
	db.Create(&procOrderShared)

	// 会话素材
	sessPrivA := models.RecordingSession{
		SessionNo:        "REC_PRIV_A",
		Name:             "用户 A 私有录制素材",
		Scope:            models.ScopePrivate,
		OwnerID:          userA,
		Status:           models.SessionStatusCompleted,
		TargetProcessIDs: datatypes.JSON([]byte(fmt.Sprintf("[%d]", procSrc.ID))),
		CreatedBy:        userA,
	}
	sessShared := models.RecordingSession{
		SessionNo:        "REC_SHARED",
		Name:             "公共共享录制素材",
		Scope:            models.ScopeShared,
		OwnerID:          1,
		Status:           models.SessionStatusCompleted,
		TargetProcessIDs: datatypes.JSON([]byte(fmt.Sprintf("[%d]", procSrc.ID))),
		CreatedBy:        1,
	}
	db.Create(&sessPrivA)
	db.Create(&sessShared)

	// 选定流量记录（调用 OrderService:CreateOrder，由 modOrder 提供）
	selectedRecords := []models.TrafficRecord{
		{
			SessionID:   sessShared.ID,
			ServiceName: "dophi.order-OrderService",
			Operation:   "CreateOrder",
		},
	}

	// 1. 用户 B 尝试选用用户 A 的 PRIVATE 录制素材 -> 403 Forbidden
	err := ValidateReplayProcessCompatibility(
		userB, false, &sessPrivA, selectedRecords, &procSrc, &procOrderB, &agtPrivB, provideMap,
	)
	if !errors.Is(err, ErrForbiddenPrivateSession) {
		t.Errorf("预期拦截私有会话返回 ErrForbiddenPrivateSession，实际: %v", err)
	}

	// 1.2 用户 B 尝试投递至用户 A 的 PRIVATE Agent -> 403 Forbidden
	err = ValidateReplayProcessCompatibility(
		userB, false, &sessShared, selectedRecords, &procSrc, &procOrderB, &agtPrivA, provideMap,
	)
	if !errors.Is(err, ErrForbiddenPrivateAgent) {
		t.Errorf("预期拦截私有 Agent 返回 ErrForbiddenPrivateAgent，实际: %v", err)
	}

	// 2. 用户 B 选用公共 SHARED 素材投递至本人 PRIVATE Agent 上仅承载 Mod_Stock 的异构进程 -> 400 Bad Request
	err = ValidateReplayProcessCompatibility(
		userB, false, &sessShared, selectedRecords, &procSrc, &procStockOnlyB, &agtPrivB, provideMap,
	)
	if !errors.Is(err, ErrIncompatibleReplayTargetModule) {
		t.Errorf("预期异构目标进程拦截返回 ErrIncompatibleReplayTargetModule，实际: %v", err)
	}

	// 3. 投递至本人 PRIVATE Agent 上承载 Mod_Order 的同构进程 -> 校验通过 (nil)
	err = ValidateReplayProcessCompatibility(
		userB, false, &sessShared, selectedRecords, &procSrc, &procOrderB, &agtPrivB, provideMap,
	)
	if err != nil {
		t.Errorf("预期投递至同构私有进程成功，实际失败: %v", err)
	}

	// 3.2 投递至空闲公共 SHARED Agent 上承载 Mod_Order 的同构进程 -> 校验通过 (nil)
	err = ValidateReplayProcessCompatibility(
		userB, false, &sessShared, selectedRecords, &procSrc, &procOrderShared, &agtShared, provideMap,
	)
	if err != nil {
		t.Errorf("预期投递至同构公共进程成功，实际失败: %v", err)
	}
}

// buildRspPacketWithAllTiming 构造包含时间戳与状态码的完整应答帧
func buildRspPacketWithAllTiming(
	sno uint64,
	statusCode uint32,
	traceBuff [16]byte,
	clientSendTime uint64,
	serverRecvTime uint64,
	serverSendTime uint64,
	serverEnqueue uint64,
	payload []byte,
) []byte {
	// 构造 V04 200B 应答头
	rawRsp := make([]byte, 200)
	binary.BigEndian.PutUint16(rawRsp[0:2], 4)
	binary.BigEndian.PutUint16(rawRsp[2:4], 200)
	binary.BigEndian.PutUint32(rawRsp[4:8], statusCode)
	copy(rawRsp[8:24], traceBuff[:])
	binary.BigEndian.PutUint64(rawRsp[32:40], clientSendTime)
	copy(rawRsp[56:120], []byte("dophi.order-OrderService"))
	copy(rawRsp[120:184], []byte("GetOrder"))
	binary.BigEndian.PutUint64(rawRsp[184:192], uint64(statusCode))
	binary.BigEndian.PutUint16(rawRsp[192:194], 1) // rspMsgNo

	// 12B TimeStamp 扩展头 (4B comm + 8B data)
	extTime := make([]byte, 12)
	binary.BigEndian.PutUint16(extTime[0:2], 8)
	extTime[2] = crosrpc.ExtTypeTimeStamp
	binary.BigEndian.PutUint64(extTime[4:12], serverEnqueue)

	totalHeadLen := uint16(200 + len(extTime))
	comm := crosrpc.CommHead{
		TransportProto: crosrpc.TransportTCP,
		SNO:            sno,
		Length:         uint32(int(totalHeadLen) + len(payload)),
		HeadLen:        totalHeadLen,
		MsgType:        crosrpc.MsgTypeRsp,
		IsPacked:       1,
	}
	copy(comm.Magic[:], []byte(crosrpc.MagicPrefix))

	var pkt []byte
	pkt = append(pkt, comm.Marshal()...)
	pkt = append(pkt, rawRsp...)
	pkt = append(pkt, extTime...)
	pkt = append(pkt, payload...)
	return pkt
}

// TestLayer1HeaderDiffAndTimestampExemption 验证第一层协议头断言与固定时间戳豁免短路 (F-14.1, F-14.3, SC-08)
func TestLayer1HeaderDiffAndTimestampExemption(t *testing.T) {
	payload := []byte("identical_response_payload_bytes_12345")

	var trace1, trace2 [16]byte
	copy(trace1[:], []byte("trace_baseline01"))
	copy(trace2[:], []byte("trace_actual_002"))

	// 1. 基线 vs 实际应答：仅时间戳、ullSNO 与 traceBuff 不同，Payload 完全一致
	baselinePkt := buildRspPacketWithAllTiming(
		1001, 0, trace1, 1700000001, 1700000005, 1700000010, 1700000003, payload,
	)
	actualPkt := buildRspPacketWithAllTiming(
		9999, 0, trace2, 1700000099, 1700000105, 1700000120, 1700000102, payload,
	)

	diffResult, payloadIdentical, err := DiffLayer1Header(baselinePkt, actualPkt)
	if err != nil {
		t.Fatalf("DiffLayer1Header 失败: %v", err)
	}
	if diffResult.Status != models.ReplayResultPass {
		t.Errorf("预期固定豁免字段被忽略短路为 PASS，实际: %s, 原因: %s", diffResult.Status, diffResult.FailureReason)
	}
	if !payloadIdentical {
		t.Errorf("预期 payload 完全相等返回 payloadIdentical=true")
	}

	// 2. 实际应答 uiStatusCode 由 0 突变为 5001 -> 第一层直接裁决为 FAILED
	failActualPkt := buildRspPacketWithAllTiming(
		1001, 5001, trace1, 1700000001, 1700000005, 1700000010, 1700000003, payload,
	)
	diffResultFail, _, err := DiffLayer1Header(baselinePkt, failActualPkt)
	if err != nil {
		t.Fatalf("DiffLayer1Header 失败: %v", err)
	}
	if diffResultFail.Status != models.ReplayResultFailed {
		t.Errorf("预期状态码不一致直接裁决为 FAILED，实际: %s", diffResultFail.Status)
	}
	if !diffResultFail.StatusCodeDiff {
		t.Errorf("预期指出状态码差异 StatusCodeDiff=true")
	}
}

// buildSemanticDiffProtoSchema 动态生成包含 map, repeated, 嵌套 message 的描述集
func buildSemanticDiffProtoSchema(t *testing.T) (*decoder.CompiledOperation, protoreflect.MessageDescriptor) {
	t.Helper()

	// 嵌套 Header Message
	headerFields := []*descriptorpb.FieldDescriptorProto{
		{
			Name:   proto.String("nonce"),
			Number: proto.Int32(1),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
		},
	}

	// 嵌套 Pricing Message
	pricingFields := []*descriptorpb.FieldDescriptorProto{
		{
			Name:   proto.String("discount_cents"),
			Number: proto.Int32(1),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
		},
	}

	// 主响应 Message
	// MapEntry for tags: map<string, int32>
	mapEntryFields := []*descriptorpb.FieldDescriptorProto{
		{
			Name:   proto.String("key"),
			Number: proto.Int32(1),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
		},
		{
			Name:   proto.String("value"),
			Number: proto.Int32(2),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
		},
	}

	rspFields := []*descriptorpb.FieldDescriptorProto{
		{
			Name:     proto.String("header"),
			Number:   proto.Int32(1),
			Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
			TypeName: proto.String(".dophi.test.Header"),
		},
		{
			Name:     proto.String("pricing"),
			Number:   proto.Int32(2),
			Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
			TypeName: proto.String(".dophi.test.Pricing"),
		},
		{
			Name:     proto.String("tags"),
			Number:   proto.Int32(3),
			Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
			Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
			TypeName: proto.String(".dophi.test.OrderRsp.TagsEntry"),
		},
		{
			Name:   proto.String("items"),
			Number: proto.Int32(4),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
		},
	}

	fileProto := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("dophi/test/test.proto"),
		Package: proto.String("dophi.test"),
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("Header"), Field: headerFields},
			{Name: proto.String("Pricing"), Field: pricingFields},
			{
				Name:  proto.String("OrderRsp"),
				Field: rspFields,
				NestedType: []*descriptorpb.DescriptorProto{
					{
						Name:    proto.String("TagsEntry"),
						Field:   mapEntryFields,
						Options: &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)},
					},
				},
			},
		},
	}

	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{fileProto},
	}
	files, err := protodesc.NewFiles(fds)
	if err != nil {
		t.Fatalf("protodesc.NewFiles 失败: %v", err)
	}
	descObj, err := files.FindDescriptorByName("dophi.test.OrderRsp")
	if err != nil {
		t.Fatalf("FindDescriptorByName 失败: %v", err)
	}
	rspDesc := descObj.(protoreflect.MessageDescriptor)

	compiledOp := &decoder.CompiledOperation{
		SchemaID:      1,
		SchemaVersion: 1,
		RspDesc:       rspDesc,
	}

	return compiledOp, rspDesc
}

// TestLayer2ProtoSemanticDiffMapRepeatedAndIgnore 验证第二层 Protobuf 语义树 Diff (F-14.2, F-14.4, F-14.5, SC-08)
func TestLayer2ProtoSemanticDiffMapRepeatedAndIgnore(t *testing.T) {
	op, desc := buildSemanticDiffProtoSchema(t)

	// 1. discount_cents 由 6500 突变为 6600
	msgExp1 := dynamicpb.NewMessage(desc)
	pricingFd := desc.Fields().ByName("pricing")
	pricingExp := dynamicpb.NewMessage(pricingFd.Message())
	pricingExp.Set(pricingFd.Message().Fields().ByName("discount_cents"), protoreflect.ValueOfInt64(6500))
	msgExp1.Set(pricingFd, protoreflect.ValueOfMessage(pricingExp))

	msgAct1 := dynamicpb.NewMessage(desc)
	pricingAct := dynamicpb.NewMessage(pricingFd.Message())
	pricingAct.Set(pricingFd.Message().Fields().ByName("discount_cents"), protoreflect.ValueOfInt64(6600))
	msgAct1.Set(pricingFd, protoreflect.ValueOfMessage(pricingAct))

	bExp1, _ := proto.Marshal(msgExp1)
	bAct1, _ := proto.Marshal(msgAct1)

	diffs1, err := DiffLayer2ProtoSemantic(bExp1, bAct1, op, nil)
	if err != nil {
		t.Fatalf("DiffLayer2ProtoSemantic 失败: %v", err)
	}
	if len(diffs1) != 1 {
		t.Fatalf("预期 1 处差异，实际得到: %d (%+v)", len(diffs1), diffs1)
	}
	if diffs1[0].FieldPath != "pricing.discount_cents" || diffs1[0].Expected != "6500" || diffs1[0].Actual != "6600" {
		t.Errorf("差异明细不匹配: %+v", diffs1[0])
	}

	// 2. map<string, int32> 序列化无序比对 (键值一致，即便字节顺序不同也裁决为 PASS)
	msgExp2 := dynamicpb.NewMessage(desc)
	tagsFd := desc.Fields().ByName("tags")
	mapExp := msgExp2.Mutable(tagsFd).Map()
	mapExp.Set(protoreflect.ValueOfString("tagA").MapKey(), protoreflect.ValueOfInt32(10))
	mapExp.Set(protoreflect.ValueOfString("tagB").MapKey(), protoreflect.ValueOfInt32(20))

	msgAct2 := dynamicpb.NewMessage(desc)
	mapAct := msgAct2.Mutable(tagsFd).Map()
	mapAct.Set(protoreflect.ValueOfString("tagB").MapKey(), protoreflect.ValueOfInt32(20))
	mapAct.Set(protoreflect.ValueOfString("tagA").MapKey(), protoreflect.ValueOfInt32(10))

	bExp2, _ := proto.Marshal(msgExp2)
	bAct2, _ := proto.Marshal(msgAct2)

	diffs2, err := DiffLayer2ProtoSemantic(bExp2, bAct2, op, nil)
	if err != nil {
		t.Fatalf("Map 比对失败: %v", err)
	}
	if len(diffs2) != 0 {
		t.Errorf("预期 Map 无序比对通过 (0 差异)，实际得到: %+v", diffs2)
	}

	// 3. repeated 列表元素顺序对调 -> repeated 保序比对裁决为 FAILED
	msgExp3 := dynamicpb.NewMessage(desc)
	itemsFd := desc.Fields().ByName("items")
	listExp := msgExp3.Mutable(itemsFd).List()
	listExp.Append(protoreflect.ValueOfString("sku_apple"))
	listExp.Append(protoreflect.ValueOfString("sku_banana"))

	msgAct3 := dynamicpb.NewMessage(desc)
	listAct := msgAct3.Mutable(itemsFd).List()
	listAct.Append(protoreflect.ValueOfString("sku_banana"))
	listAct.Append(protoreflect.ValueOfString("sku_apple"))

	bExp3, _ := proto.Marshal(msgExp3)
	bAct3, _ := proto.Marshal(msgAct3)

	diffs3, err := DiffLayer2ProtoSemantic(bExp3, bAct3, op, nil)
	if err != nil {
		t.Fatalf("List 比对失败: %v", err)
	}
	if len(diffs3) == 0 {
		t.Errorf("预期 repeated 保序比对检测出顺序差异，实际未检测到差异")
	}

	// 4. 差异字段 header.nonce 命中 extra_ignore_paths 规则 -> 忽略后裁决为 PASS
	msgExp4 := dynamicpb.NewMessage(desc)
	hdrFd := desc.Fields().ByName("header")
	hdrExp := dynamicpb.NewMessage(hdrFd.Message())
	hdrExp.Set(hdrFd.Message().Fields().ByName("nonce"), protoreflect.ValueOfString("nonce_111"))
	msgExp4.Set(hdrFd, protoreflect.ValueOfMessage(hdrExp))

	msgAct4 := dynamicpb.NewMessage(desc)
	hdrAct := dynamicpb.NewMessage(hdrFd.Message())
	hdrAct.Set(hdrFd.Message().Fields().ByName("nonce"), protoreflect.ValueOfString("nonce_222"))
	msgAct4.Set(hdrFd, protoreflect.ValueOfMessage(hdrAct))

	bExp4, _ := proto.Marshal(msgExp4)
	bAct4, _ := proto.Marshal(msgAct4)

	diffs4, err := DiffLayer2ProtoSemantic(bExp4, bAct4, op, []string{"header.nonce"})
	if err != nil {
		t.Fatalf("忽略规则比对失败: %v", err)
	}
	if len(diffs4) != 0 {
		t.Errorf("预期命中忽略规则 header.nonce 返回 0 差异，实际: %+v", diffs4)
	}

	// 5. 实际应答包含 .pb 未定义的未知 Tag 字段 -> 检出 <unknown_fields> 裁决为 FAILED
	msgAct5 := dynamicpb.NewMessage(desc)
	bAct5, _ := proto.Marshal(msgAct5)
	// 追加未知 tag 15 (varint = 999): tag 15 << 3 | 0 = 120 (0x78)
	bAct5WithUnknown := append(bAct5, 0x78, 0xe7, 0x07)

	diffs5, err := DiffLayer2ProtoSemantic(bExp4, bAct5WithUnknown, op, []string{"header.nonce"})
	if err != nil {
		t.Fatalf("未知字段比对失败: %v", err)
	}
	hasUnknownDiff := false
	for _, d := range diffs5 {
		if d.FieldPath == "<unknown_fields>" {
			hasUnknownDiff = true
			break
		}
	}
	if !hasUnknownDiff {
		t.Errorf("预期检出 <unknown_fields> 差异，实际未检出: %+v", diffs5)
	}
}
