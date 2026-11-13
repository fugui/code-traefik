package handlers_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	commonAudit "code-common/backend/audit"
	commonModels "code-common/backend/models"
	"code-common/backend/testdb"
	"code-traefik/config"
	"code-traefik/handlers"
	"code-traefik/internal/server/decoder"
	"code-traefik/internal/server/hub"
	"code-traefik/models"
	"code-traefik/pkg/crosrpc"

	"github.com/gin-gonic/gin"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"gorm.io/gorm"
)

func setupProtoDocRouterAndDB(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	if os.Getenv("TEST_DB_DSN") == "" {
		t.Skip("TEST_DB_DSN 未配置，跳过数据库隔离测试")
	}
	gin.SetMode(gin.TestMode)

	db := testdb.SetupIsolatedDB(t, "traefik_pdoc_api_test")
	if err := models.SetupWithDB(db); err != nil {
		t.Fatalf("SetupWithDB 失败: %v", err)
	}
	if err := db.AutoMigrate(&commonModels.SysAuditLog{}); err != nil {
		t.Fatalf("迁移审计表失败: %v", err)
	}

	_ = commonAudit.Close(context.Background())
	commonAudit.Init(db, commonAudit.InitOptions{
		BatchSize:   1,
		FlushPeriod: 20 * time.Millisecond,
	})
	t.Cleanup(func() {
		_ = commonAudit.Close(context.Background())
	})

	config.AppConfig = config.Config{
		Auth: config.AuthSection{
			JWTSecret: "test-jwt-secret-very-secure-key-123456",
		},
		AgentHub: config.AgentHubSection{
			DevAllowLoopback:     true,
			HeartbeatIntervalSec: 2,
		},
	}

	mgr := hub.NewManager(db, &config.AppConfig)
	handlers.ResetGlobalHubManager(mgr)
	decReg, _ := decoder.NewRegistry(db)
	handlers.ResetGlobalDecoderRegistry(decReg)
	assembler, _, _ := handlers.InitRecorderAndDiff(db, decReg)
	t.Cleanup(func() {
		if assembler != nil {
			assembler.Close()
		}
	})

	r := gin.New()
	r.Use(gin.Recovery())
	handlers.RegisterAPIRoutes(r)
	return r, db
}

func buildHandlerTestReqPacket(service, op string, payload []byte) []byte {
	sessionHead := make([]byte, crosrpc.ReqHeadMinLen)
	binary.BigEndian.PutUint16(sessionHead[0:2], 1)
	binary.BigEndian.PutUint16(sessionHead[2:4], crosrpc.ReqHeadMinLen)
	copy(sessionHead[8:72], []byte(service))
	copy(sessionHead[72:136], []byte(op))
	sessionHead[136] = crosrpc.ReqTypeSync

	comm := crosrpc.CommHead{
		TransportProto: crosrpc.TransportTCP,
		SNO:            202614,
		Length:         uint32(crosrpc.ReqHeadMinLen + len(payload)),
		HeadLen:        crosrpc.ReqHeadMinLen,
		MsgType:        crosrpc.MsgTypeReq,
		IsPacked:       1,
	}
	copy(comm.Magic[:], []byte(crosrpc.MagicPrefix))

	pkt := make([]byte, 0, crosrpc.CommHeadLen+len(sessionHead)+len(payload))
	pkt = append(pkt, comm.Marshal()...)
	pkt = append(pkt, sessionHead...)
	pkt = append(pkt, payload...)
	return pkt
}

func buildHandlerTestRspPacket(service, op string, payload []byte) []byte {
	rspHeadLen := 156
	sessionHead := make([]byte, rspHeadLen)
	binary.BigEndian.PutUint16(sessionHead[0:2], 1)
	binary.BigEndian.PutUint16(sessionHead[2:4], uint16(rspHeadLen))
	copy(sessionHead[24:88], []byte(service))
	copy(sessionHead[88:152], []byte(op))

	comm := crosrpc.CommHead{
		TransportProto: crosrpc.TransportTCP,
		SNO:            202614,
		Length:         uint32(rspHeadLen + len(payload)),
		HeadLen:        uint16(rspHeadLen),
		MsgType:        crosrpc.MsgTypeRsp,
		IsPacked:       1,
	}
	copy(comm.Magic[:], []byte(crosrpc.MagicPrefix))

	pkt := make([]byte, 0, crosrpc.CommHeadLen+len(sessionHead)+len(payload))
	pkt = append(pkt, comm.Marshal()...)
	pkt = append(pkt, sessionHead...)
	pkt = append(pkt, payload...)
	return pkt
}

func TestSC14_LivingProtoDocsGoldenSamplesAndMarkdownExport(t *testing.T) {
	if os.Getenv("TEST_DB_DSN") == "" {
		t.Skip("TEST_DB_DSN 未配置，跳过数据库隔离测试")
	}

	r, db := setupProtoDocRouterAndDB(t)
	adminUser := createTestUserInDB(t, db, "admin_sc14", config.AdminRole)
	normalUser := createTestUserInDB(t, db, "viewer_sc14", "user")
	adminToken := generateTestJWT(t, adminUser)
	viewerToken := generateTestJWT(t, normalUser)

	// 1. 验证未登录访客访问文档中心与只读源码接口均返回 401
	for _, path := range []string{
		"/api/proto-docs/tree",
		"/api/proto-docs/services/demo.billing.v1.BillingService",
		"/api/proto-docs/export?service_name=demo.billing.v1.BillingService",
		"/api/proto-schemas/1/sources",
		"/api/proto-files",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, req)
		if resp.Code != http.StatusUnauthorized {
			t.Fatalf("未认证访问 %s 应返回 401，实际 %d", path, resp.Code)
		}
	}

	// 2. 预置 ADR-16 契约定义方 + 多实现模块 + 下游消费方
	sub := models.Subsystem{Code: "SUB_FIN", Name: "计费结算子系统", CreatedBy: adminUser.ID}
	db.Create(&sub)
	grp := models.FunctionalGroup{SubsystemID: sub.ID, Code: "GRP_FIN", Name: "账单组", Path: "/GRP_FIN", CreatedBy: adminUser.ID}
	db.Create(&grp)
	pg := models.ArchProcessGroup{Code: "PG_FIN", Name: "计费进程组", CreatedBy: adminUser.ID}
	db.Create(&pg)
	procA := models.ArchProcess{GroupID: pg.ID, Code: "billing-core-a", Name: "计费实例 A", CreatedBy: adminUser.ID}
	procB := models.ArchProcess{GroupID: pg.ID, Code: "billing-core-b", Name: "计费实例 B", CreatedBy: adminUser.ID}
	procCons := models.ArchProcess{GroupID: pg.ID, Code: "order-gateway", Name: "订单网关", CreatedBy: adminUser.ID}
	db.Create(&procA)
	db.Create(&procB)
	db.Create(&procCons)

	modContract := models.Module{
		SubsystemID: sub.ID,
		GroupID:     grp.ID,
		Code:        "LIB_BILLING_CONTRACT",
		Name:        "计费公共契约库",
		ModuleKind:  models.ModuleKindContractLib,
		CreatedBy:   adminUser.ID,
	}
	modImplA := models.Module{
		SubsystemID:   sub.ID,
		GroupID:       grp.ID,
		ArchProcessID: &procA.ID,
		Code:          "MOD_BILLING_DOMESTIC",
		Name:          "国内计费实现模块",
		ModuleKind:    models.ModuleKindBusiness,
		OwnerID:       &adminUser.ID,
		CreatedBy:     adminUser.ID,
	}
	modImplB := models.Module{
		SubsystemID:   sub.ID,
		GroupID:       grp.ID,
		ArchProcessID: &procB.ID,
		Code:          "MOD_BILLING_INTL",
		Name:          "国际计费实现模块",
		ModuleKind:    models.ModuleKindBusiness,
		OwnerID:       &normalUser.ID,
		CreatedBy:     adminUser.ID,
	}
	modCons := models.Module{
		SubsystemID:   sub.ID,
		GroupID:       grp.ID,
		ArchProcessID: &procCons.ID,
		Code:          "MOD_ORDER_GW",
		Name:          "订单调用网关",
		ModuleKind:    models.ModuleKindBusiness,
		CreatedBy:     adminUser.ID,
	}
	db.Create(&modContract)
	db.Create(&modImplA)
	db.Create(&modImplB)
	db.Create(&modCons)

	contract := models.ServiceContract{
		ServiceName:     "demo.billing.v1.BillingService",
		DefinerModuleID: modContract.ID,
		ProvideMode:     models.ContractProvideModeMultiImpl,
		ImplKeyLabel:    "地域分区",
		CreatedBy:       adminUser.ID,
	}
	db.Create(&contract)
	db.Create(&models.ModuleService{
		ModuleID:    modImplA.ID,
		ContractID:  &contract.ID,
		ServiceName: "demo.billing.v1.BillingService",
		ImplKey:     "cn-east",
		CreatedBy:   adminUser.ID,
	})
	db.Create(&models.ModuleService{
		ModuleID:    modImplB.ID,
		ContractID:  &contract.ID,
		ServiceName: "demo.billing.v1.BillingService",
		ImplKey:     "us-west",
		CreatedBy:   adminUser.ID,
	})
	db.Create(&models.ModuleDependency{
		ConsumerModuleID: modCons.ID,
		ContractID:       &contract.ID,
		CreatedBy:        adminUser.ID,
	})

	// 3. 在线编译 .proto 并自动激活，断言返回非零 doc_symbol_count
	billingProto := `syntax = "proto3";
package demo.billing.v1;

// 账单币种枚举
enum Currency {
  CURRENCY_UNSPECIFIED = 0; // 未指定
  CURRENCY_CNY = 1; // 人民币
  CURRENCY_USD = 2; // 美元
}

message ChargeReq {
  int64 account_id = 1; // 资金账户 ID
  Currency currency = 2; // 结算币种
  int64 amount_cents = 3; // 扣费金额（分）
}

message ChargeRsp {
  string tx_id = 1; // 交易流水号
  int64 balance_after = 2; // 扣费后余额
}

// 统一实时计费服务
// @owner team-fin
// @error_code INSUFFICIENT_FUNDS 402 账户可用余额不足
service BillingService {
  // 执行实时扣费记账
  // @error_code DUPLICATE_TX 409 - 幂等流水号冲突
  rpc Charge(ChargeReq) returns (ChargeRsp);
}
`
	compileBody, _ := json.Marshal(map[string]any{
		"name":          "billing-service-pb",
		"category":      models.ProtoCategoryBusiness,
		"auto_activate": true,
		"files": []map[string]string{
			{"path": "billing/v1/billing.proto", "content": billingProto},
		},
	})
	cReq := httptest.NewRequest(http.MethodPost, "/api/proto-schemas/compile", bytes.NewReader(compileBody))
	cReq.Header.Set("Content-Type", "application/json")
	cReq.Header.Set("Authorization", "Bearer "+adminToken)
	cResp := httptest.NewRecorder()
	r.ServeHTTP(cResp, cReq)
	if cResp.Code != http.StatusCreated {
		t.Fatalf("CompileProtoSchema 失败 (%d): %s", cResp.Code, cResp.Body.String())
	}
	var compileOut struct {
		Schema struct {
			ID uint64 `json:"id"`
		} `json:"schema"`
		DocSymbolCount int `json:"doc_symbol_count"`
	}
	if err := json.Unmarshal(cResp.Body.Bytes(), &compileOut); err != nil {
		t.Fatalf("解析 compile 输出失败: %v", err)
	}
	if compileOut.DocSymbolCount <= 0 {
		t.Fatalf("预期 CompileProtoSchema 自动生成并回传 doc_symbol_count > 0，实际=%d", compileOut.DocSymbolCount)
	}

	// 4. 构造 ACTIVE 黄金测试集样本
	var savedSchema models.ProtoSchema
	if err := db.First(&savedSchema, compileOut.Schema.ID).Error; err != nil {
		t.Fatalf("读取已存 Schema 失败: %v", err)
	}
	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(savedSchema.DescriptorBytes, &fds); err != nil {
		t.Fatalf("Unmarshal fds 失败: %v", err)
	}
	files, _ := protodesc.NewFiles(&fds)
	reqDescAny, _ := files.FindDescriptorByName("demo.billing.v1.ChargeReq")
	rspDescAny, _ := files.FindDescriptorByName("demo.billing.v1.ChargeRsp")
	reqDesc := reqDescAny.(protoreflect.MessageDescriptor)
	rspDesc := rspDescAny.(protoreflect.MessageDescriptor)

	dynReq := dynamicpb.NewMessage(reqDesc)
	dynReq.Set(reqDesc.Fields().ByName("account_id"), protoreflect.ValueOfInt64(88001234))
	dynReq.Set(reqDesc.Fields().ByName("currency"), protoreflect.ValueOfEnum(1))
	dynReq.Set(reqDesc.Fields().ByName("amount_cents"), protoreflect.ValueOfInt64(5000))
	rawReqPayload, _ := proto.Marshal(dynReq)

	dynRsp := dynamicpb.NewMessage(rspDesc)
	dynRsp.Set(rspDesc.Fields().ByName("tx_id"), protoreflect.ValueOfString("TX-GOLDEN-991"))
	dynRsp.Set(rspDesc.Fields().ByName("balance_after"), protoreflect.ValueOfInt64(95000))
	rawRspPayload, _ := proto.Marshal(dynRsp)

	gSuite := models.GoldenSuite{
		Code:          "GS-BILLING-PROD",
		Name:          "计费黄金基准集",
		ArchProcessID: procA.ID,
		Status:        models.GoldenSuiteStatusActive,
	}
	db.Create(&gSuite)
	db.Create(&models.GoldenSuiteItem{
		SuiteID:      gSuite.ID,
		OrderNo:      1,
		Name:         "人民币标准扣费",
		ServiceName:  "demo.billing.v1.BillingService",
		Operation:    "Charge",
		RawReqPacket: buildHandlerTestReqPacket("demo.billing.v1.BillingService", "Charge", rawReqPayload),
		RawRspPacket: buildHandlerTestRspPacket("demo.billing.v1.BillingService", "Charge", rawRspPayload),
	})

	// 5. GET /api/proto-docs/tree 验证中文搜索及 SQL 注入参数化安全
	tReq := httptest.NewRequest(http.MethodGet, "/api/proto-docs/tree?search=%E8%AE%A1%E8%B4%B9", nil)
	tReq.Header.Set("Authorization", "Bearer "+viewerToken)
	tResp := httptest.NewRecorder()
	r.ServeHTTP(tResp, tReq)
	if tResp.Code != http.StatusOK {
		t.Fatalf("GET /api/proto-docs/tree 失败 (%d): %s", tResp.Code, tResp.Body.String())
	}
	var treeOut struct {
		Items         []json.RawMessage `json:"items"`
		TotalServices int               `json:"total_services"`
		TotalMethods  int               `json:"total_methods"`
	}
	if err := json.Unmarshal(tResp.Body.Bytes(), &treeOut); err != nil {
		t.Fatalf("解析 tree 失败: %v", err)
	}
	if treeOut.TotalServices != 1 || treeOut.TotalMethods != 1 || !strings.Contains(tResp.Body.String(), "计费结算子系统") {
		t.Fatalf("导航树聚合异常: %s", tResp.Body.String())
	}

	// SQL 注入尝试不得触发 500 或破坏库表
	sqliReq := httptest.NewRequest(http.MethodGet, "/api/proto-docs/tree?search=';DROP+TABLE+traefik_proto_doc_symbols;--", nil)
	sqliReq.Header.Set("Authorization", "Bearer "+viewerToken)
	sqliResp := httptest.NewRecorder()
	r.ServeHTTP(sqliResp, sqliReq)
	if sqliResp.Code != http.StatusOK {
		t.Fatalf("SQLi 测试请求应安全返回 200 空树，实际 %d", sqliResp.Code)
	}

	// 6. GET /api/proto-docs/services/:serviceName 验证详情 + Enum 字典 + 黄金样本 + ADR-16 血缘
	dReq := httptest.NewRequest(http.MethodGet, "/api/proto-docs/services/demo.billing.v1.BillingService", nil)
	dReq.Header.Set("Authorization", "Bearer "+viewerToken)
	dResp := httptest.NewRecorder()
	r.ServeHTTP(dResp, dReq)
	if dResp.Code != http.StatusOK {
		t.Fatalf("GET service doc 失败 (%d): %s", dResp.Code, dResp.Body.String())
	}
	detailStr := dResp.Body.String()
	for _, want := range []string{
		"team-fin",
		"INSUFFICIENT_FUNDS",
		"DUPLICATE_TX",
		"CURRENCY_CNY",
		"人民币",
		"TX-GOLDEN-991",
		"LIB_BILLING_CONTRACT",
		"cn-east",
		"us-west",
		"MOD_ORDER_GW",
	} {
		if !strings.Contains(detailStr, want) {
			t.Errorf("服务文档详情缺少期望字段 %q: %s", want, detailStr)
		}
	}

	// 不存在的服务返回 404
	nfReq := httptest.NewRequest(http.MethodGet, "/api/proto-docs/services/demo.missing.Service", nil)
	nfReq.Header.Set("Authorization", "Bearer "+viewerToken)
	nfResp := httptest.NewRecorder()
	r.ServeHTTP(nfResp, nfReq)
	if nfResp.Code != http.StatusNotFound {
		t.Errorf("查询不存在服务应返回 404，实际 %d", nfResp.Code)
	}

	// 7. GET /api/proto-docs/export 验证离线 Markdown 导出及安全响应头
	eReq := httptest.NewRequest(http.MethodGet, "/api/proto-docs/export?service_name=demo.billing.v1.BillingService&format=markdown", nil)
	eReq.Header.Set("Authorization", "Bearer "+viewerToken)
	eResp := httptest.NewRecorder()
	r.ServeHTTP(eResp, eReq)
	if eResp.Code != http.StatusOK {
		t.Fatalf("GET /api/proto-docs/export 失败 (%d): %s", eResp.Code, eResp.Body.String())
	}
	if ct := eResp.Header().Get("Content-Type"); !strings.Contains(ct, "text/markdown") {
		t.Errorf("预期 Content-Type 包含 text/markdown，实际 %q", ct)
	}
	if cd := eResp.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment;") || !strings.Contains(cd, ".md") {
		t.Errorf("预期 Content-Disposition=attachment 且包含 .md，实际 %q", cd)
	}
	if nosniff := eResp.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Errorf("预期 X-Content-Type-Options=nosniff，实际 %q", nosniff)
	}
	if cc := eResp.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("预期 Cache-Control=no-store，实际 %q", cc)
	}
	if !strings.Contains(eResp.Body.String(), "TX-GOLDEN-991") || !strings.Contains(eResp.Body.String(), "人民币") {
		t.Errorf("Markdown 导出正文缺少黄金样本或 Enum 中文说明")
	}

	// 缺失 scope 返回 400
	badExpReq := httptest.NewRequest(http.MethodGet, "/api/proto-docs/export", nil)
	badExpReq.Header.Set("Authorization", "Bearer "+viewerToken)
	badExpResp := httptest.NewRecorder()
	r.ServeHTTP(badExpResp, badExpReq)
	if badExpResp.Code != http.StatusBadRequest {
		t.Errorf("缺失 export 范围参数应返回 400，实际 %d", badExpResp.Code)
	}
}

func TestProtoSourcesAndVirtualFilesAdapter_ReadOnlyNoLocksAndDiff(t *testing.T) {
	if os.Getenv("TEST_DB_DSN") == "" {
		t.Skip("TEST_DB_DSN 未配置，跳过数据库隔离测试")
	}

	r, db := setupProtoDocRouterAndDB(t)
	adminUser := createTestUserInDB(t, db, "admin_sources", config.AdminRole)
	adminToken := generateTestJWT(t, adminUser)

	compileVersion := func(vNote, methodExtra string) uint64 {
		src := fmt.Sprintf(`syntax = "proto3";
package demo.user.v1;

// 用户查询请求
message GetUserReq {
  int64 user_id = 1;
}

message GetUserRsp {
  string nickname = 1;
%s
}

// 用户资料服务 (%s)
service UserProfileService {
  // 查询用户基本信息
  rpc GetUser(GetUserReq) returns (GetUserRsp);
}
`, methodExtra, vNote)

		body, _ := json.Marshal(map[string]any{
			"name":          "user-profile-pb",
			"category":      models.ProtoCategoryBusiness,
			"version_tag":   vNote,
			"auto_activate": true,
			"files": []map[string]string{
				{"path": "user_profile.proto", "content": src},
			},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/proto-schemas/compile", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, req)
		if resp.Code != http.StatusCreated {
			t.Fatalf("编译版本 %s 失败 (%d): %s", vNote, resp.Code, resp.Body.String())
		}
		var out struct {
			Schema struct {
				ID uint64 `json:"id"`
			} `json:"schema"`
		}
		_ = json.Unmarshal(resp.Body.Bytes(), &out)
		return out.Schema.ID
	}

	_ = compileVersion("v1.0.0", "")
	s2ID := compileVersion("v2.0.0", "  string avatar_url = 2; // 头像地址")

	// 1. 测试 M23 过渡只读源码适配器 GET /api/proto-schemas/:id/sources
	sReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/proto-schemas/%d/sources", s2ID), nil)
	sReq.Header.Set("Authorization", "Bearer "+adminToken)
	sResp := httptest.NewRecorder()
	r.ServeHTTP(sResp, sReq)
	if sResp.Code != http.StatusOK {
		t.Fatalf("GET /sources 失败 (%d): %s", sResp.Code, sResp.Body.String())
	}
	if strings.Contains(sResp.Body.String(), `"locked"`) || strings.Contains(sResp.Body.String(), `"locked_by"`) {
		t.Fatalf("违反 Git-Only 无锁红线：/sources 响应中出现了 locked 字段: %s", sResp.Body.String())
	}
	if !strings.Contains(sResp.Body.String(), "UserProfileService") || !strings.Contains(sResp.Body.String(), `"symbol_anchors"`) {
		t.Errorf("/sources 未返回期望源码重构内容与行号锚点: %s", sResp.Body.String())
	}

	// 2. 测试过渡虚拟只读文件契约 /api/proto-files*
	listReq := httptest.NewRequest(http.MethodGet, "/api/proto-files?target_schema_name=user-profile-pb", nil)
	listReq.Header.Set("Authorization", "Bearer "+adminToken)
	listResp := httptest.NewRecorder()
	r.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("GET /api/proto-files 失败 (%d): %s", listResp.Code, listResp.Body.String())
	}
	if strings.Contains(listResp.Body.String(), `"locked"`) || strings.Contains(listResp.Body.String(), `"locked_by"`) {
		t.Fatalf("违反 Git-Only 无锁红线：/api/proto-files 出现了 locked 字段")
	}
	var listOut struct {
		Items []struct {
			ID       uint64 `json:"id"`
			FilePath string `json:"file_path"`
		} `json:"items"`
	}
	_ = json.Unmarshal(listResp.Body.Bytes(), &listOut)
	if len(listOut.Items) == 0 {
		t.Fatalf("预期返回至少 1 个虚拟只读 ProtoFile: %s", listResp.Body.String())
	}
	fileID := listOut.Items[0].ID

	cReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/proto-files/%d/content", fileID), nil)
	cReq.Header.Set("Authorization", "Bearer "+adminToken)
	cResp := httptest.NewRecorder()
	r.ServeHTTP(cResp, cReq)
	if cResp.Code != http.StatusOK || !strings.Contains(cResp.Body.String(), "avatar_url") {
		t.Fatalf("GET /api/proto-files/:id/content 异常 (%d): %s", cResp.Code, cResp.Body.String())
	}

	vReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/proto-files/%d/versions", fileID), nil)
	vReq.Header.Set("Authorization", "Bearer "+adminToken)
	vResp := httptest.NewRecorder()
	r.ServeHTTP(vResp, vReq)
	if vResp.Code != http.StatusOK {
		t.Fatalf("GET /versions 失败 (%d): %s", vResp.Code, vResp.Body.String())
	}

	diffReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/proto-files/%d/diff?from_version=prev&to_version=latest", fileID), nil)
	diffReq.Header.Set("Authorization", "Bearer "+adminToken)
	diffResp := httptest.NewRecorder()
	r.ServeHTTP(diffResp, diffReq)
	if diffResp.Code != http.StatusOK || !strings.Contains(diffResp.Body.String(), "+") || !strings.Contains(diffResp.Body.String(), "avatar_url") {
		t.Fatalf("GET /diff 失败 (%d): %s", diffResp.Code, diffResp.Body.String())
	}
}
