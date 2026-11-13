package protodoc_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"code-common/backend/testdb"
	"code-traefik/internal/server/decoder"
	"code-traefik/internal/server/protodoc"
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

func setupIsolatedProtoDocDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("TEST_DB_DSN") == "" {
		t.Skip("TEST_DB_DSN 未配置，跳过数据库隔离测试")
	}
	db := testdb.SetupIsolatedDB(t, "traefik_protodoc_test")
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}
	return db
}

func compileTestProtoSchema(t *testing.T, name string, files map[string]string) (*models.ProtoSchema, *decoder.CompileProtoOutput) {
	t.Helper()
	out, err := decoder.CompileProtoBundle(context.Background(), nil, decoder.CompileProtoBundleInput{
		Name:     name,
		Category: models.ProtoCategoryBusiness,
		Files:    files,
	})
	if err != nil {
		t.Fatalf("CompileProtoBundle 失败: %v", err)
	}
	filesJSON, err := json.Marshal(out.OwnProtoFiles)
	if err != nil {
		t.Fatalf("Marshal OwnProtoFiles 失败: %v", err)
	}
	schema := &models.ProtoSchema{
		ID:              101,
		Name:            name,
		Version:         1,
		Category:        models.ProtoCategoryBusiness,
		SourceType:      models.ProtoSourceOnlineCompile,
		FileName:        name + ".pb",
		FileSHA256:      "abc123",
		FileSize:        len(out.DescriptorBytes),
		DescriptorBytes: out.DescriptorBytes,
		IsActive:        true,
		ProtoFileCount:  len(out.OwnProtoFiles),
		ServiceCount:    1,
		OperationCount:  len(out.Operations),
		ProtoFiles:      datatypes.JSON(filesJSON),
	}
	return schema, out
}

func buildTestCrosReqPacket(service, op string, payload []byte) []byte {
	sessionHead := make([]byte, crosrpc.ReqHeadMinLen)
	binary.BigEndian.PutUint16(sessionHead[0:2], 1)
	binary.BigEndian.PutUint16(sessionHead[2:4], crosrpc.ReqHeadMinLen)
	copy(sessionHead[8:72], []byte(service))
	copy(sessionHead[72:136], []byte(op))
	sessionHead[136] = crosrpc.ReqTypeSync

	comm := crosrpc.CommHead{
		TransportProto: crosrpc.TransportTCP,
		SNO:            202601,
		Length:         uint32(crosrpc.ReqHeadMinLen + len(payload)),
		HeadLen:        crosrpc.ReqHeadMinLen,
		MsgType:        crosrpc.MsgTypeReq,
		IsPacked:       1,
	}
	copy(comm.Magic[:], []byte(crosrpc.MagicPrefix))

	out := make([]byte, 0, crosrpc.CommHeadLen+len(sessionHead)+len(payload))
	out = append(out, comm.Marshal()...)
	out = append(out, sessionHead...)
	out = append(out, payload...)
	return out
}

func buildTestCrosRspPacket(service, op string, payload []byte) []byte {
	rspHeadLen := 156
	sessionHead := make([]byte, rspHeadLen)
	binary.BigEndian.PutUint16(sessionHead[0:2], 1)
	binary.BigEndian.PutUint16(sessionHead[2:4], uint16(rspHeadLen))
	copy(sessionHead[24:88], []byte(service))
	copy(sessionHead[88:152], []byte(op))

	comm := crosrpc.CommHead{
		TransportProto: crosrpc.TransportTCP,
		SNO:            202601,
		Length:         uint32(rspHeadLen + len(payload)),
		HeadLen:        uint16(rspHeadLen),
		MsgType:        crosrpc.MsgTypeRsp,
		IsPacked:       1,
	}
	copy(comm.Magic[:], []byte(crosrpc.MagicPrefix))

	out := make([]byte, 0, crosrpc.CommHeadLen+len(sessionHead)+len(payload))
	out = append(out, comm.Marshal()...)
	out = append(out, sessionHead...)
	out = append(out, payload...)
	return out
}

func findProtoLine(src, substr string) int {
	for idx, line := range strings.Split(src, "\n") {
		if strings.Contains(line, substr) {
			return idx + 1
		}
	}
	return 0
}

func TestExtractDocs_ServiceMethodEnumTagsAndSourceLineAnchors(t *testing.T) {
	orderProto := `syntax = "proto3";
package demo.order.v1;

// 订单流转状态枚举字典
enum OrderStatus {
  ORDER_STATUS_UNSPECIFIED = 0; // 未指定默认状态
  ORDER_STATUS_PAID = 1; // 已支付待发货
  // @deprecated 请改用 ORDER_STATUS_PAID
  ORDER_STATUS_LEGACY = 2 [deprecated = true];
}

message OrderItem {
  int64 sku_id = 1; // 商品 SKU 编号
  int32 quantity = 2;
}

// 创建订单请求
message CreateOrderReq {
  int64 order_id = 1; // 全局订单唯一 ID
  OrderStatus status = 2;
  repeated OrderItem items = 3;
  map<string, string> ext_attrs = 4; // 扩展透传属性
  oneof discount_choice {
    // 优惠券编码
    string coupon_code = 5;
    int64 point_amount = 6;
  }
}

message CreateOrderRsp {
  int32 code = 1;
  OrderStatus final_status = 2;
}

// 订单交易核心服务
// 负责下单创建与库存预占流转
// @owner team-order
// @deprecated 老版创建入口，请逐步迁移至 OrderServiceV2
// @error_code ORDER_NOT_FOUND 404 订单不存在
// @error_code ERR_QUOTA 429 - 超出单日创建配额
service OrderService {
  // 创建标准交易订单
  // @owner alice.order
  // @error_code INVALID_SKU 400 无效的商品 SKU
  rpc CreateOrder(CreateOrderReq) returns (CreateOrderRsp);
}
`
	schema, _ := compileTestProtoSchema(t, "order-doc-suite", map[string]string{
		"order.proto": orderProto,
	})

	symbols, hasSourceInfo, err := protodoc.ExtractSymbols(schema)
	if err != nil {
		t.Fatalf("ExtractSymbols 失败: %v", err)
	}
	if !hasSourceInfo {
		t.Fatalf("预期存在 SourceCodeInfo，实际 hasSourceInfo=false")
	}

	byKindFull := make(map[string]models.ProtoDocSymbol)
	for _, s := range symbols {
		byKindFull[s.SymbolKind+":"+s.FullName] = s
	}

	svcSym, ok := byKindFull[models.DocSymbolService+":demo.order.v1.OrderService"]
	if !ok {
		t.Fatalf("未提取到 Service 符号: %+v", symbols)
	}
	if !strings.Contains(svcSym.LeadingComments, "订单交易核心服务") {
		t.Errorf("Service LeadingComments 未包含中文说明: %q", svcSym.LeadingComments)
	}
	if svcSym.OwnerTag != "team-order" {
		t.Errorf("预期 Service OwnerTag=team-order, 实际=%q", svcSym.OwnerTag)
	}
	if !svcSym.IsDeprecated || !strings.Contains(svcSym.DeprecationReason, "OrderServiceV2") {
		t.Errorf("预期 Service IsDeprecated=true 且保留原因，实际 is_dep=%v reason=%q", svcSym.IsDeprecated, svcSym.DeprecationReason)
	}
	wantSvcLine := findProtoLine(orderProto, "service OrderService")
	if svcSym.StartLine != wantSvcLine {
		t.Errorf("预期 Service StartLine=%d, 实际=%d", wantSvcLine, svcSym.StartLine)
	}

	var svcErrs []protodoc.ErrorCodeDoc
	if err := json.Unmarshal(svcSym.ErrorCodesJSON, &svcErrs); err != nil || len(svcErrs) != 2 {
		t.Fatalf("预期解析到 2 条 Service @error_code，实际 len=%d err=%v raw=%s", len(svcErrs), err, string(svcSym.ErrorCodesJSON))
	}
	if svcErrs[0].Code != "ORDER_NOT_FOUND" || svcErrs[0].HttpOrCrosCode != "404" || svcErrs[0].Description != "订单不存在" {
		t.Errorf("首条 error_code 解析不符: %+v", svcErrs[0])
	}
	if svcErrs[1].Code != "ERR_QUOTA" || svcErrs[1].HttpOrCrosCode != "429" || svcErrs[1].Description != "超出单日创建配额" {
		t.Errorf("次条 error_code（带 - 分隔符）解析不符: %+v", svcErrs[1])
	}

	methodSym, ok := byKindFull[models.DocSymbolMethod+":demo.order.v1.OrderService.CreateOrder"]
	if !ok {
		t.Fatalf("未提取到 Method 符号")
	}
	wantMethodLine := findProtoLine(orderProto, "rpc CreateOrder")
	if methodSym.StartLine != wantMethodLine {
		t.Errorf("预期 Method StartLine=%d, 实际=%d", wantMethodLine, methodSym.StartLine)
	}
	if methodSym.OwnerTag != "alice.order" {
		t.Errorf("预期 Method OwnerTag=alice.order, 实际=%q", methodSym.OwnerTag)
	}

	enumSym, ok := byKindFull[models.DocSymbolEnum+":demo.order.v1.OrderStatus"]
	if !ok {
		t.Fatalf("未提取到 Enum 符号")
	}
	var enumMeta []protodoc.EnumValueDoc
	if err := json.Unmarshal(enumSym.FieldsMetaJSON, &enumMeta); err != nil || len(enumMeta) != 3 {
		t.Fatalf("预期 Enum 提取 3 个枚举值，实际 len=%d err=%v", len(enumMeta), err)
	}
	if enumMeta[1].Comment != "已支付待发货" {
		t.Errorf("预期 EnumValue 尾行注释='已支付待发货', 实际=%q", enumMeta[1].Comment)
	}
	if !enumMeta[2].IsDeprecated {
		t.Errorf("预期 ORDER_STATUS_LEGACY IsDeprecated=true")
	}

	reqMsgSym, ok := byKindFull[models.DocSymbolMessage+":demo.order.v1.CreateOrderReq"]
	if !ok {
		t.Fatalf("未提取到 Message 符号 CreateOrderReq")
	}
	var msgMeta protodoc.MessageDocMeta
	if err := json.Unmarshal(reqMsgSym.FieldsMetaJSON, &msgMeta); err != nil {
		t.Fatalf("解析 CreateOrderReq FieldsMetaJSON 失败: %v", err)
	}
	foundOrderID := false
	foundMap := false
	foundOneof := false
	for _, f := range msgMeta.Fields {
		if f.Name == "order_id" && f.Comment == "全局订单唯一 ID" && f.StartLine == findProtoLine(orderProto, "int64 order_id = 1") {
			foundOrderID = true
		}
		if f.Name == "ext_attrs" && f.IsMap && f.MapKeyType == "string" && f.MapValueType == "string" {
			foundMap = true
		}
		if f.Name == "coupon_code" && f.OneofGroup == "discount_choice" && strings.Contains(f.Comment, "优惠券编码") {
			foundOneof = true
		}
	}
	if !foundOrderID || !foundMap || !foundOneof {
		t.Errorf("字段解析未完整命中 order_id=%v map=%v oneof=%v fields=%+v", foundOrderID, foundMap, foundOneof, msgMeta.Fields)
	}

	// 验证手工剥离 SourceCodeInfo (.pb 无源码信息场景) 平滑降级
	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(schema.DescriptorBytes, &fds); err != nil {
		t.Fatalf("Unmarshal fds 失败: %v", err)
	}
	for _, f := range fds.File {
		f.SourceCodeInfo = nil
	}
	strippedBytes, _ := proto.Marshal(&fds)
	strippedSchema := *schema
	strippedSchema.DescriptorBytes = strippedBytes

	strippedSyms, strippedHasInfo, err := protodoc.ExtractSymbols(&strippedSchema)
	if err != nil {
		t.Fatalf("无 SourceCodeInfo 降级提取失败: %v", err)
	}
	if strippedHasInfo {
		t.Errorf("无 SourceCodeInfo 时预期 hasSourceInfo=false")
	}
	if len(strippedSyms) != len(symbols) {
		t.Errorf("降级提取符号总数不匹配: want=%d got=%d", len(symbols), len(strippedSyms))
	}
}

func TestBuildDefaultAndGoldenJSON_CycleAndDepth12Guard(t *testing.T) {
	cycleProto := `syntax = "proto3";
package demo.tree.v1;

enum Color {
  COLOR_UNSPECIFIED = 0;
  COLOR_BLUE = 1;
}

message TreeNode {
  int64 node_id = 1;
  uint64 version = 2;
  string title = 3;
  Color color = 4;
  repeated TreeNode children = 5;
  TreeNode parent = 6;
}

message D1 { int64 v = 1; D2 next = 2; }
message D2 { D3 next = 1; }
message D3 { D4 next = 1; }
message D4 { D5 next = 1; }
message D5 { D6 next = 1; }
message D6 { D7 next = 1; }
message D7 { D8 next = 1; }
message D8 { D9 next = 1; }
message D9 { D10 next = 1; }
message D10 { D11 next = 1; }
message D11 { D12 next = 1; }
message D12 { D13 next = 1; }
message D13 { D14 next = 1; }
message D14 { D15 next = 1; }
message D15 { string leaf = 1; }

message WalkReq {
  TreeNode root = 1;
  D1 deep = 2;
}

message WalkRsp {
  int64 matched_count = 1;
  string summary = 2;
}

service TreeService {
  rpc Walk(WalkReq) returns (WalkRsp);
}
`
	schema, _ := compileTestProtoSchema(t, "tree-cycle-suite", map[string]string{
		"tree.proto": cycleProto,
	})

	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(schema.DescriptorBytes, &fds); err != nil {
		t.Fatalf("Unmarshal fds 失败: %v", err)
	}
	files, err := protodesc.NewFiles(&fds)
	if err != nil {
		t.Fatalf("protodesc.NewFiles 失败: %v", err)
	}
	walkReqDesc, err := files.FindDescriptorByName("demo.tree.v1.WalkReq")
	if err != nil {
		t.Fatalf("未找到 WalkReq 描述符: %v", err)
	}
	reqMsgDesc, ok := walkReqDesc.(protoreflect.MessageDescriptor)
	if !ok {
		t.Fatalf("WalkReq 不是 MessageDescriptor")
	}

	defJSON, err := protodoc.BuildDefaultMessageJSON(reqMsgDesc)
	if err != nil {
		t.Fatalf("BuildDefaultMessageJSON 失败: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(defJSON, &parsed); err != nil {
		t.Fatalf("默认 JSON 非法: %v raw=%s", err, string(defJSON))
	}
	rootObj, ok := parsed["root"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 root 字段: %+v", parsed)
	}
	// int64/uint64 依 Protobuf JSON 规范输出 string
	if _, isStr := rootObj["node_id"].(string); !isStr {
		t.Errorf("预期 int64 node_id 输出 string，实际 %T (%v)", rootObj["node_id"], rootObj["node_id"])
	}
	if _, isStr := rootObj["version"].(string); !isStr {
		t.Errorf("预期 uint64 version 输出 string，实际 %T (%v)", rootObj["version"], rootObj["version"])
	}
	if colorVal, _ := rootObj["color"].(string); colorVal != "COLOR_UNSPECIFIED" {
		t.Errorf("预期枚举 color 默认输出 COLOR_UNSPECIFIED，实际 %v", rootObj["color"])
	}

	// 验证 14 层嵌套被截断 (deep.next... 不应包含 leaf)
	if strings.Contains(string(defJSON), `"leaf"`) {
		t.Errorf("预期 depth > 12 时截断 D1..D15 链条，但输出仍包含 leaf: %s", string(defJSON))
	}

	// 若未配置 Postgres，已完成核心纯 AST 循环测试
	if os.Getenv("TEST_DB_DSN") == "" {
		return
	}

	db := setupIsolatedProtoDocDB(t)
	schema.ID = 0
	if err := db.Create(schema).Error; err != nil {
		t.Fatalf("插入 ProtoSchema 失败: %v", err)
	}
	if _, err := protodoc.RebuildForSchema(db, schema); err != nil {
		t.Fatalf("RebuildForSchema 失败: %v", err)
	}

	// 构造真实 protobuf 请求与应答二进制负载
	dynReq := dynamicpb.NewMessage(reqMsgDesc)
	dynReq.Set(reqMsgDesc.Fields().ByName("root"), func() protoreflect.Value {
		treeDesc := reqMsgDesc.Fields().ByName("root").Message()
		node := dynamicpb.NewMessage(treeDesc)
		node.Set(treeDesc.Fields().ByName("node_id"), protoreflect.ValueOfInt64(9007199254740991))
		node.Set(treeDesc.Fields().ByName("title"), protoreflect.ValueOfString("根节点-黄金样本"))
		node.Set(treeDesc.Fields().ByName("color"), protoreflect.ValueOfEnum(1))
		return protoreflect.ValueOfMessage(node)
	}())
	rawReqPayload, _ := proto.Marshal(dynReq)

	walkRspDesc, _ := files.FindDescriptorByName("demo.tree.v1.WalkRsp")
	rspMsgDesc := walkRspDesc.(protoreflect.MessageDescriptor)
	dynRsp := dynamicpb.NewMessage(rspMsgDesc)
	dynRsp.Set(rspMsgDesc.Fields().ByName("matched_count"), protoreflect.ValueOfInt64(42))
	dynRsp.Set(rspMsgDesc.Fields().ByName("summary"), protoreflect.ValueOfString("黄金样本命中"))
	rawRspPayload, _ := proto.Marshal(dynRsp)

	pg := models.ArchProcessGroup{Code: "PG_DOC_TEST", Name: "文档测试进程组"}
	if err := db.Create(&pg).Error; err != nil {
		t.Fatalf("创建 ArchProcessGroup 失败: %v", err)
	}
	archProc := models.ArchProcess{GroupID: pg.ID, Code: "sut-doc-proc", Name: "文档测试进程"}
	if err := db.Create(&archProc).Error; err != nil {
		t.Fatalf("创建 ArchProcess 失败: %v", err)
	}

	activeSuite := models.GoldenSuite{
		Code:          "GOLDEN-ACTIVE-01",
		Name:          "现网核心黄金集",
		ArchProcessID: archProc.ID,
		Status:        models.GoldenSuiteStatusActive,
	}
	archivedSuite := models.GoldenSuite{
		Code:          "GOLDEN-ARCHIVED-02",
		Name:          "已归档基线集",
		ArchProcessID: archProc.ID,
		Status:        models.GoldenSuiteStatusArchived,
	}
	if err := db.Create(&activeSuite).Error; err != nil {
		t.Fatalf("创建 ACTIVE GoldenSuite 失败: %v", err)
	}
	if err := db.Create(&archivedSuite).Error; err != nil {
		t.Fatalf("创建 ARCHIVED GoldenSuite 失败: %v", err)
	}

	reqPkt := buildTestCrosReqPacket("demo.tree.v1.TreeService", "Walk", rawReqPayload)
	rspPkt := buildTestCrosRspPacket("demo.tree.v1.TreeService", "Walk", rawRspPayload)
	if err := db.Create(&models.GoldenSuiteItem{
		SuiteID:      activeSuite.ID,
		OrderNo:      1,
		Name:         "活跃回归基准",
		ServiceName:  "demo.tree.v1.TreeService",
		Operation:    "Walk",
		RawReqPacket: reqPkt,
		RawRspPacket: rspPkt,
	}).Error; err != nil {
		t.Fatalf("创建 ACTIVE GoldenSuiteItem 失败: %v", err)
	}
	if err := db.Create(&models.GoldenSuiteItem{
		SuiteID:      archivedSuite.ID,
		OrderNo:      1,
		Name:         "已归档不应展示",
		ServiceName:  "demo.tree.v1.TreeService",
		Operation:    "Walk",
		RawReqPacket: reqPkt,
		RawRspPacket: rspPkt,
	}).Error; err != nil {
		t.Fatalf("创建 ARCHIVED GoldenSuiteItem 失败: %v", err)
	}

	detail, err := protodoc.GetServiceDocDetail(db, "demo.tree.v1.TreeService", &schema.ID)
	if err != nil {
		t.Fatalf("GetServiceDocDetail 失败: %v", err)
	}
	if len(detail.Methods) != 1 {
		t.Fatalf("预期 1 个方法，实际 %d", len(detail.Methods))
	}
	gs := detail.Methods[0].GoldenSamples
	if len(gs) != 1 || gs[0].SuiteCode != "GOLDEN-ACTIVE-01" {
		t.Fatalf("预期仅关联 ACTIVE 黄金样本，实际 %+v", gs)
	}
	if gs[0].DecodeStatus != "DECODED_JSON" {
		t.Errorf("预期黄金样本 DecodeStatus=DECODED_JSON，实际 %q", gs[0].DecodeStatus)
	}
	if !strings.Contains(string(gs[0].ReqJSON), "根节点-黄金样本") || !strings.Contains(string(gs[0].RspJSON), "黄金样本命中") {
		t.Errorf("解码后的黄金 JSON 内容缺失: req=%s rsp=%s", string(gs[0].ReqJSON), string(gs[0].RspJSON))
	}
}

func TestExportProtoDocsMarkdown_HeadersAndSections(t *testing.T) {
	db := setupIsolatedProtoDocDB(t)

	payProto := `syntax = "proto3";
package demo.pay.v1;

enum PayChannel {
  PAY_CHANNEL_UNSPECIFIED = 0; // 未定义渠道
  PAY_CHANNEL_WECHAT = 1; // 微信支付
}

message PayReq {
  int64 pay_id = 1; // 支付流水号
  PayChannel channel = 2; // 渠道选择
}

message PayRsp {
  bool accepted = 1;
}

// 支付统一网关服务
// @owner team-pay
// @error_code PAY_TIMEOUT 504 渠道网关超时
service PaymentService {
  // 发起收单支付
  // @error_code PAY_BAL_LOW 402 账户余额不足
  rpc SubmitPay(PayReq) returns (PayRsp);
}
`
	schema, _ := compileTestProtoSchema(t, "pay-doc-suite", map[string]string{
		"pay.proto": payProto,
	})
	schema.ID = 0
	if err := db.Create(schema).Error; err != nil {
		t.Fatalf("创建 ProtoSchema 失败: %v", err)
	}
	if _, err := protodoc.RebuildForSchema(db, schema); err != nil {
		t.Fatalf("RebuildForSchema 失败: %v", err)
	}

	filename, md, err := protodoc.ExportMarkdown(db, protodoc.ExportMarkdownRequest{
		ServiceName: "demo.pay.v1.PaymentService",
		SchemaID:    &schema.ID,
		Format:      "markdown",
	})
	if err != nil {
		t.Fatalf("ExportMarkdown 失败: %v", err)
	}
	if !strings.HasSuffix(filename, ".md") || strings.ContainsAny(filename, "/\\") {
		t.Errorf("导出文件名不合法: %q", filename)
	}
	for _, want := range []string{
		"demo.pay.v1.PaymentService",
		"SubmitPay",
		"team-pay",
		"PAY_TIMEOUT",
		"PAY_BAL_LOW",
		"PayChannel",
		"PAY_CHANNEL_WECHAT",
		"微信支付",
		"```json",
		"pay_id",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown 导出缺少期望章节/字段 %q", want)
		}
	}
	if strings.Contains(md, "RawReqPacket") || strings.Contains(md, "RawRspPacket") {
		t.Errorf("Markdown 导出不应泄露原始二进制字段名/字节流")
	}
}
