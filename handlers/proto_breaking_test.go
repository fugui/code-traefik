package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	commonAudit "code-common/backend/audit"
	commonModels "code-common/backend/models"
	"code-traefik/config"
	"code-traefik/internal/server/decoder"
	"code-traefik/models"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// buildAccountV1DescriptorBytes 构造 account v1 描述集 (AccountService.GetAccount)
func buildAccountV1DescriptorBytes(t *testing.T) []byte {
	t.Helper()
	file := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("account/v1/account.proto"),
		Package: proto.String("account.v1"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("AccountReq"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:   proto.String("user_id"),
						Number: proto.Int32(1),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
					},
					{
						Name:   proto.String("user_name"),
						Number: proto.Int32(2),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
				},
			},
			{
				Name: proto.String("AccountRsp"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:   proto.String("success"),
						Number: proto.Int32(1),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_BOOL.Enum(),
					},
				},
			},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: proto.String("AccountService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:       proto.String("GetAccount"),
						InputType:  proto.String(".account.v1.AccountReq"),
						OutputType: proto.String(".account.v1.AccountRsp"),
					},
				},
			},
		},
	}
	fds := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{file}}
	data, err := proto.Marshal(fds)
	if err != nil {
		t.Fatalf("marshal account v1 fds failed: %v", err)
	}
	return data
}

// buildAccountV2BreakingDescriptorBytes 构造包含破坏性变更 (TagMutation) 的 account v2 描述集
func buildAccountV2BreakingDescriptorBytes(t *testing.T) []byte {
	t.Helper()
	file := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("account/v1/account.proto"),
		Package: proto.String("account.v1"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("AccountReq"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:   proto.String("user_id"),
						Number: proto.Int32(2), // Tag 从 1 改为 2 (TAG_MUTATION)
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
					},
					{
						Name:   proto.String("user_name"),
						Number: proto.Int32(3),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
				},
			},
			{
				Name: proto.String("AccountRsp"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:   proto.String("success"),
						Number: proto.Int32(1),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_BOOL.Enum(),
					},
				},
			},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: proto.String("AccountService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:       proto.String("GetAccount"),
						InputType:  proto.String(".account.v1.AccountReq"),
						OutputType: proto.String(".account.v1.AccountRsp"),
					},
				},
			},
		},
	}
	fds := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{file}}
	data, err := proto.Marshal(fds)
	if err != nil {
		t.Fatalf("marshal account v2 fds failed: %v", err)
	}
	return data
}

// TestSC09_BreakingChangeDetection_DownstreamAlert_AndAllowBreakingRollbackAudit
// 验证 SC-09：破坏性变更阻断雷达、下游消费者血缘推导、管理员带因审计逃生舱全链路 (M16 / ADR-14)
func TestSC09_BreakingChangeDetection_DownstreamAlert_AndAllowBreakingRollbackAudit(t *testing.T) {
	if os.Getenv("TEST_DB_DSN") == "" {
		t.Skip("TEST_DB_DSN 未配置，跳过隔离集成测试")
	}

	r, db := setupRouterAndDB(t)
	adminUser := createTestUserInDB(t, db, "admin_breaking", config.AdminRole)
	adminToken := generateTestJWT(t, adminUser)

	// 1. 初始化下游消费者血缘架构数据：
	//    - Provider Module: "ACCOUNT_SERVICE"
	//    - ModuleService: "account.v1.AccountService"
	//    - ArchProcess: "sut-order"
	//    - Consumer Module: "ORDER_CORE" (Owner: adminUser)
	//    - ModuleDependency: ORDER_CORE 依赖 account.v1.AccountService
	subsys := models.Subsystem{Code: "SUB_BIZ", Name: "业务子系统", CreatedBy: adminUser.ID}
	_ = db.Create(&subsys).Error
	group := models.FunctionalGroup{SubsystemID: subsys.ID, Code: "GRP_CORE", Name: "核心组", CreatedBy: adminUser.ID}
	_ = db.Create(&group).Error
	procGroup := models.ArchProcessGroup{Code: "PG_APP", Name: "应用分组", CreatedBy: adminUser.ID}
	_ = db.Create(&procGroup).Error
	archProc := models.ArchProcess{GroupID: procGroup.ID, Code: "sut-order", Name: "订单标准进程", CreatedBy: adminUser.ID}
	_ = db.Create(&archProc).Error

	provMod := models.Module{SubsystemID: subsys.ID, GroupID: group.ID, Code: "ACCOUNT_CORE", Name: "账户服务模块", CreatedBy: adminUser.ID}
	_ = db.Create(&provMod).Error
	consumerMod := models.Module{
		SubsystemID:   subsys.ID,
		GroupID:       group.ID,
		ArchProcessID: &archProc.ID,
		Code:          "ORDER_CORE",
		Name:          "订单核心模块",
		OwnerID:       &adminUser.ID,
		CreatedBy:     adminUser.ID,
	}
	_ = db.Create(&consumerMod).Error

	modSvc := models.ModuleService{
		ModuleID:    provMod.ID,
		ServiceName: "account.v1.AccountService",
		CreatedBy:   adminUser.ID,
	}
	_ = db.Create(&modSvc).Error

	dep := models.ModuleDependency{
		ConsumerModuleID: consumerMod.ID,
		ServiceID:        &modSvc.ID,
		CreatedBy:        adminUser.ID,
	}
	_ = db.Create(&dep).Error

	// 2. 上传并激活 account v1 (基线版本)
	v1Bytes := buildAccountV1DescriptorBytes(t)
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, _ := w.CreateFormFile("file", "account.pb")
	_, _ = fw.Write(v1Bytes)
	_ = w.WriteField("name", "account-schema")
	_ = w.WriteField("category", "BUSINESS")
	_ = w.WriteField("auto_activate", "true")
	_ = w.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/proto-schemas", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("上传 account v1 失败: code=%d, body=%s", resp.Code, resp.Body.String())
	}

	var uploadResp struct {
		Schema models.ProtoSchema `json:"schema"`
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &uploadResp)
	v1ID := uploadResp.Schema.ID

	// 3. 上传 account v2 草稿 (auto_activate=false, 带破坏性改动) -> 201 Created, 附带 breaking_warnings
	v2Bytes := buildAccountV2BreakingDescriptorBytes(t)
	body.Reset()
	w = multipart.NewWriter(&body)
	fw, _ = w.CreateFormFile("file", "account.pb")
	_, _ = fw.Write(v2Bytes)
	_ = w.WriteField("name", "account-schema")
	_ = w.WriteField("category", "BUSINESS")
	_ = w.WriteField("auto_activate", "false")
	_ = w.Close()

	req = httptest.NewRequest(http.MethodPost, "/api/proto-schemas", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp = httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("上传 account v2 草稿应成功 201: code=%d, body=%s", resp.Code, resp.Body.String())
	}

	var draftResp struct {
		Schema           models.ProtoSchema              `json:"schema"`
		BreakingWarnings []decoder.BreakingRuleViolation `json:"breaking_warnings"`
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &draftResp)
	v2ID := draftResp.Schema.ID
	if len(draftResp.BreakingWarnings) == 0 {
		t.Fatalf("上传草稿应附带 breaking_warnings 提示，但为空: %s", resp.Body.String())
	}

	// 4. 尝试直接激活 account v2 (默认 allow_breaking=false) -> 必须返回 409 BREAKING_CHANGE_DETECTED 与下游血缘
	req = httptest.NewRequest(http.MethodPost, "/api/proto-schemas/"+strconvFormat(v2ID)+"/activate", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp = httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusConflict {
		t.Fatalf("未携带 allow_breaking 激活破坏性描述集必须返回 409 Conflict: code=%d, body=%s", resp.Code, resp.Body.String())
	}

	var conflictResp struct {
		Code               string                          `json:"code"`
		BreakingRules      []decoder.BreakingRuleViolation `json:"breaking_rules"`
		AffectedDependents []decoder.AffectedDependent     `json:"affected_dependents"`
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &conflictResp)
	if conflictResp.Code != "BREAKING_CHANGE_DETECTED" {
		t.Errorf("期望 code 为 BREAKING_CHANGE_DETECTED，实测为 %s", conflictResp.Code)
	}
	if len(conflictResp.BreakingRules) == 0 {
		t.Errorf("breaking_rules 列表不应为空")
	}
	if len(conflictResp.AffectedDependents) == 0 {
		t.Fatalf("affected_dependents 不应为空，未推导出下游模块 ORDER_CORE")
	}
	depMod := conflictResp.AffectedDependents[0]
	if depMod.ModuleCode != "ORDER_CORE" || depMod.ArchProcessCode != "sut-order" {
		t.Errorf("推导下游血缘属性不符: %+v", depMod)
	}

	// 5. 管理员逃生舱校验：allow_breaking=true 但 breaking_reason < 8 字符 -> 400 Bad Request
	reqBodyShort, _ := json.Marshal(map[string]interface{}{
		"allow_breaking":  true,
		"breaking_reason": "test", // 4 字符，不足 8
	})
	req = httptest.NewRequest(http.MethodPost, "/api/proto-schemas/"+strconvFormat(v2ID)+"/activate", bytes.NewReader(reqBodyShort))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp = httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("breaking_reason < 8 字符必须返回 400 Bad Request: code=%d, body=%s", resp.Code, resp.Body.String())
	}

	// 6. 管理员逃生舱通过：allow_breaking=true 且 breaking_reason >= 8 字符 -> 200 OK，forced_breaking=true，写入 P1 审计日志
	validReason := "生产故障紧急版本回滚或架构评审批准破坏性演进"
	reqBodyValid, _ := json.Marshal(map[string]interface{}{
		"allow_breaking":  true,
		"breaking_reason": validReason,
	})
	req = httptest.NewRequest(http.MethodPost, "/api/proto-schemas/"+strconvFormat(v2ID)+"/activate", bytes.NewReader(reqBodyValid))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp = httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("带合法原因强制激活应成功 200: code=%d, body=%s", resp.Code, resp.Body.String())
	}

	var okResp struct {
		ForcedBreaking bool `json:"forced_breaking"`
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &okResp)
	if !okResp.ForcedBreaking {
		t.Errorf("期望 forced_breaking=true")
	}

	// 刷新审计日志入库
	_ = commonAudit.Close(context.Background())

	// 验证写入了 P1 级操作审计日志
	var auditLog commonModels.SysAuditLog
	err := db.Where("module = ? AND action = ? AND level = ?", config.ModuleKey, "activate", "P1").Order("id DESC").First(&auditLog).Error
	if err != nil {
		t.Fatalf("未找到对应的 P1 级审计日志: %v", err)
	}
	if !strings.Contains(auditLog.Summary, validReason) {
		t.Errorf("审计日志 Summary 未包含原因: %s, 期望包含: %s", auditLog.Summary, validReason)
	}

	// 7. 测试 BatchActivateProtoSchemas 批量激活破坏性阻断与逃生舱
	// 将 v1ID 重新批量激活
	batchReqNoAllow, _ := json.Marshal(map[string]interface{}{
		"schema_ids": []uint64{v1ID},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/proto-schemas/batch-activate", bytes.NewReader(batchReqNoAllow))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp = httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	// 因为当前生效的是 v2 (Tag 2)，切回 v1 (Tag 1) 同样是 Tag 变异破坏性变更，必须被阻断为 409
	if resp.Code != http.StatusConflict {
		t.Fatalf("批量激活切回旧版破坏性改动必须阻断为 409: code=%d, body=%s", resp.Code, resp.Body.String())
	}

	// 带合法逃生舱参数批量激活切回
	batchReqAllowed, _ := json.Marshal(map[string]interface{}{
		"schema_ids":      []uint64{v1ID},
		"allow_breaking":  true,
		"breaking_reason": "紧急将描述集回滚至 v1 基线版本以恢复线上可用性",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/proto-schemas/batch-activate", bytes.NewReader(batchReqAllowed))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp = httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("带合法逃生舱批量激活应成功 200: code=%d, body=%s", resp.Code, resp.Body.String())
	}
}

func strconvFormat(n uint64) string {
	return strconvFormatUint(n)
}

func strconvFormatUint(n uint64) string {
	var buf [20]byte
	i := len(buf)
	if n == 0 {
		return "0"
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + (n % 10))
		n /= 10
	}
	return string(buf[i:])
}
