package recorder

import (
	"encoding/binary"
	"encoding/json"
	"net"
	"testing"
	"time"

	"code-common/backend/testdb"
	"code-traefik/config"
	"code-traefik/internal/server/decoder"
	"code-traefik/models"
	"code-traefik/pkg/agentproto"
	"code-traefik/pkg/crosrpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func buildTestOrderFDS(t *testing.T) []byte {
	t.Helper()
	reqFields := []*descriptorpb.FieldDescriptorProto{
		{
			Name:   proto.String("order_id"),
			Number: proto.Int32(1),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_UINT64.Enum(),
		},
		{
			Name:   proto.String("sku_id"),
			Number: proto.Int32(2),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
		},
	}
	rspFields := []*descriptorpb.FieldDescriptorProto{
		{
			Name:   proto.String("code"),
			Number: proto.Int32(1),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
		},
		{
			Name:   proto.String("order_id"),
			Number: proto.Int32(2),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_UINT64.Enum(),
		},
		{
			Name:   proto.String("message"),
			Number: proto.Int32(3),
			Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
		},
	}

	fileProto := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("dophi/order/v1/trade.proto"),
		Package: proto.String("dophi.order.v1"),
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("TradeServiceReq"), Field: reqFields},
			{Name: proto.String("TradeServiceRsp"), Field: rspFields},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: proto.String("TradeService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:       proto.String("CreateOrder"),
						InputType:  proto.String(".dophi.order.v1.TradeServiceReq"),
						OutputType: proto.String(".dophi.order.v1.TradeServiceRsp"),
					},
				},
			},
		},
	}

	fds := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fileProto}}
	raw, err := proto.Marshal(fds)
	if err != nil {
		t.Fatalf("marshal FDS failed: %v", err)
	}
	return raw
}

// T-M13-04: 慢轨解码落盘与发起序单调自增、连接级 MNG 对端身份与业务错误提取
func TestAssembler_SlowTrackDecode_SeqNo_PeerProc_AndBizError(t *testing.T) {
	db := testdb.SetupIsolatedDB(t, config.ServiceName)
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB failed: %v", err)
	}

	dec, err := decoder.NewRegistry(db)
	if err != nil {
		t.Fatalf("NewRegistry failed: %v", err)
	}
	_, _, _, err = dec.UploadSchema(decoder.UploadSchemaInput{
		Name:         "trade_schema",
		VersionTag:   "v1.0.0",
		FileName:     "trade.pb",
		RawBytes:     buildTestOrderFDS(t),
		AutoActivate: true,
	})
	if err != nil {
		t.Fatalf("UploadSchema failed: %v", err)
	}

	// 构造 ExchangeAssembler
	cfg := &AssemblerConfig{
		WorkerShards:    2,
		BatchSize:       10,
		FlushInterval:   50 * time.Millisecond,
		DefaultTimeout:  5 * time.Second,
		MaxPendingLimit: 100,
	}
	assembler := NewExchangeAssembler(db, nil, nil, nil, dec, cfg)
	defer assembler.Close()

	sessionID := uint64(7001)
	processID := uint64(10)
	pid := uint32(54321)

	// 连接四元组
	srcIP := net.ParseIP("192.168.1.100").To4()
	dstIP := net.ParseIP("192.168.1.200").To4()
	srcPort := uint16(43210)
	dstPort := uint16(8091)
	connSeq := uint32(1)

	// 1. 发送前置 MNG 握手帧 (MsgType=5), ProcName="api-gw"
	mngSession := make([]byte, 88)
	binary.BigEndian.PutUint16(mngSession[0:2], 1)
	binary.BigEndian.PutUint16(mngSession[2:4], 1)
	copy(mngSession[8:72], []byte("api-gw\x00"))

	commMng := crosrpc.CommHead{
		TransportProto: crosrpc.TransportTCP,
		SNO:            10,
		Length:         88,
		HeadLen:        88,
		MsgType:        crosrpc.MsgTypeMng,
		IsPacked:       1,
	}
	copy(commMng.Magic[:], []byte(crosrpc.MagicPrefix))
	mngPacket := append(commMng.Marshal(), mngSession...)

	mngEnv := agentproto.EnvelopeHeader{
		Magic:           agentproto.EnvelopeMagic,
		ProtoVer:        agentproto.EnvelopeProtoVer,
		EventType:       agentproto.EventTrafficFrame,
		SessionOrTaskID: sessionID,
		ProcessID:       processID,
		CapturedPID:     pid,
		Direction:       agentproto.DirectionIngress,
		CapturedAtNs:    5_000_000,
		SrcPort:         srcPort,
		DstPort:         dstPort,
		ConnSeq:         connSeq,
	}
	mngEnv.SetSrcIP(srcIP)
	mngEnv.SetDstIP(dstIP)
	assembler.Feed(1, mngEnv, mngPacket)

	// 2. 构造父 INGRESS 报文 (SNO=1001, t=10ms)
	// order_id = 9981 (field 1 varint: 0x08, 0xfd, 0x4d), sku_id = "SKU-99" (field 2 string: 0x12, 0x06, "SKU-99")
	orderReqPayload := []byte{0x08, 0xfd, 0x4d, 0x12, 0x06, 'S', 'K', 'U', '-', '9', '9'}
	// order_rsp: code = 4009 (field 1 varint: 0x08, 0xa9, 0x1f), order_id = 9981 (field 2 varint: 0x10, 0xfd, 0x4d)
	orderRspPayload := []byte{0x08, 0xa9, 0x1f, 0x10, 0xfd, 0x4d}

	parentReqPkt := buildCrosReqFrameWithDOTR(1001, "dophi.order.v1.TradeService", "CreateOrder", 8888, 1, 0, orderReqPayload)
	parentRspPkt := buildCrosRspFrame(1001, crosrpc.MsgTypeRsp, 1, 0, "dophi.order.v1.TradeService", "CreateOrder", orderRspPayload)

	parentEnvReq := agentproto.EnvelopeHeader{
		Magic:           agentproto.EnvelopeMagic,
		ProtoVer:        agentproto.EnvelopeProtoVer,
		EventType:       agentproto.EventTrafficFrame,
		SessionOrTaskID: sessionID,
		ProcessID:       processID,
		CapturedPID:     pid,
		Direction:       agentproto.DirectionIngress,
		CapturedAtNs:    10_000_000, // t=10ms
		SrcPort:         srcPort,
		DstPort:         dstPort,
		ConnSeq:         connSeq,
	}
	parentEnvReq.SetSrcIP(srcIP)
	parentEnvReq.SetDstIP(dstIP)

	// 注入父 INGRESS REQ
	assembler.Feed(1, parentEnvReq, parentReqPkt)

	// 3. 构造子 EGRESS 报文 (SNO=2001, t=12ms, 终结于 t=15ms)
	childSrcPort := uint16(51234)
	childDstPort := uint16(9090)
	childConnSeq := uint32(2)

	childReqPkt := buildCrosReqFrameWithDOTR(2001, "dophi.order.v1.TradeService", "CreateOrder", 8888, 2, 1, orderReqPayload)
	childRspPkt := buildCrosRspFrame(2001, crosrpc.MsgTypeRsp, 1, 0, "dophi.order.v1.TradeService", "CreateOrder", orderRspPayload)

	childEnvReq := agentproto.EnvelopeHeader{
		Magic:           agentproto.EnvelopeMagic,
		ProtoVer:        agentproto.EnvelopeProtoVer,
		EventType:       agentproto.EventTrafficFrame,
		SessionOrTaskID: sessionID,
		ProcessID:       processID,
		CapturedPID:     pid,
		Direction:       agentproto.DirectionEgress,
		CapturedAtNs:    12_000_000, // t=12ms
		SrcPort:         childSrcPort,
		DstPort:         childDstPort,
		ConnSeq:         childConnSeq,
	}
	childEnvReq.SetSrcIP(srcIP)
	childEnvReq.SetDstIP(dstIP)

	childEnvRsp := agentproto.EnvelopeHeader{
		Magic:           agentproto.EnvelopeMagic,
		ProtoVer:        agentproto.EnvelopeProtoVer,
		EventType:       agentproto.EventTrafficFrame,
		SessionOrTaskID: sessionID,
		ProcessID:       processID,
		CapturedPID:     pid,
		Direction:       agentproto.DirectionEgress,
		CapturedAtNs:    15_000_000, // t=15ms
		SrcPort:         childSrcPort,
		DstPort:         childDstPort,
		ConnSeq:         childConnSeq,
	}
	childEnvRsp.SetSrcIP(srcIP)
	childEnvRsp.SetDstIP(dstIP)

	// 注入子 EGRESS REQ -> 子 EGRESS RSP
	assembler.Feed(1, childEnvReq, childReqPkt)
	assembler.Feed(1, childEnvRsp, childRspPkt)

	// 4. 注入父 INGRESS RSP (t=25ms)
	parentEnvRsp := agentproto.EnvelopeHeader{
		Magic:           agentproto.EnvelopeMagic,
		ProtoVer:        agentproto.EnvelopeProtoVer,
		EventType:       agentproto.EventTrafficFrame,
		SessionOrTaskID: sessionID,
		ProcessID:       processID,
		CapturedPID:     pid,
		Direction:       agentproto.DirectionIngress,
		CapturedAtNs:    25_000_000, // t=25ms
		SrcPort:         srcPort,
		DstPort:         dstPort,
		ConnSeq:         connSeq,
	}
	parentEnvRsp.SetSrcIP(srcIP)
	parentEnvRsp.SetDstIP(dstIP)
	assembler.Feed(1, parentEnvRsp, parentRspPkt)

	// 刷新落盘
	assembler.Flush()
	time.Sleep(100 * time.Millisecond)

	// 查询落盘数据
	var records []models.TrafficRecord
	if err := db.Where("session_id = ?", sessionID).Order("seq_no ASC").Find(&records).Error; err != nil {
		t.Fatalf("查询 traffic_records 失败: %v", err)
	}

	// 应该有 MNG(1), 父(1), 子(1) 共 3 条记录
	if len(records) < 3 {
		t.Fatalf("期望至少落盘 3 条记录，实际落盘 %d 条", len(records))
	}

	var mngRec, parentRec, childRec *models.TrafficRecord
	for i := range records {
		r := &records[i]
		if r.ExchangeType == models.ExchangeMng {
			mngRec = r
		} else if r.Direction == models.TrafficDirIngress && r.ExchangeType == models.ExchangeUnary {
			parentRec = r
		} else if r.Direction == models.TrafficDirEgress && r.ExchangeType == models.ExchangeUnary {
			childRec = r
		}
	}

	if mngRec == nil || parentRec == nil || childRec == nil {
		t.Fatalf("未完整提取到 MNG、父 INGRESS 和子 EGRESS 记录")
	}

	// 断言发起序: MNG (SeqNo=1) < 父 (SeqNo=2) < 子 (SeqNo=3)
	if !(mngRec.SeqNo < parentRec.SeqNo && parentRec.SeqNo < childRec.SeqNo) {
		t.Errorf("发起序未保持单调递增: mng=%d, parent=%d, child=%d", mngRec.SeqNo, parentRec.SeqNo, childRec.SeqNo)
	}

	// 断言父记录 payload_json 包含真实解码的 req 与 rsp（绝非 null）
	var parentPayload map[string]interface{}
	if err := json.Unmarshal([]byte(parentRec.PayloadJSON), &parentPayload); err != nil {
		t.Fatalf("反序列化父记录 payload_json 失败: %v", err)
	}
	if parentPayload["req"] == nil {
		t.Errorf("父记录 req 载荷落盘为 null: %s", string(parentRec.PayloadJSON))
	}
	if parentPayload["rsp"] == nil {
		t.Errorf("父记录 rsp 载荷落盘为 null: %s", string(parentRec.PayloadJSON))
	}

	// 断言同连接前置 MNG 帧 acProcName="api-gw" 正确注入父记录 headers_json.meta.peer_proc_name
	var headers map[string]interface{}
	if err := json.Unmarshal([]byte(parentRec.HeadersJSON), &headers); err != nil {
		t.Fatalf("反序列化 headers_json 失败: %v", err)
	}
	meta, ok := headers["meta"].(map[string]interface{})
	if !ok || meta == nil {
		t.Fatalf("父记录 headers_json 缺少 meta: %s", string(parentRec.HeadersJSON))
	}
	if meta["peer_proc_name"] != "api-gw" {
		t.Errorf("peer_proc_name 期望 api-gw，实际为 %v", meta["peer_proc_name"])
	}

	// 断言业务错误被正确识别 (code = 4009)
	if meta["biz_error"] != true {
		t.Errorf("biz_error 期望 true，实际为 %v", meta["biz_error"])
	}
	if meta["biz_status_summary"] != "code=4009" {
		t.Errorf("biz_status_summary 期望 code=4009，实际为 %v", meta["biz_status_summary"])
	}
}
