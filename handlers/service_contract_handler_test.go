package handlers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"code-traefik/config"
	"code-traefik/handlers"
	"code-traefik/models"
)

func TestServiceContract_API_RBACAndConversion(t *testing.T) {
	router, db := setupRouterAndDB(t)

	// 创建管理员用户与普通用户
	adminUser := createTestUserInDB(t, db, "admin_contract_user", config.AdminRole)
	adminToken := generateTestJWT(t, adminUser)

	ordinaryUser := createTestUserInDB(t, db, "ordinary_contract_user", "user")
	userToken := generateTestJWT(t, ordinaryUser)

	// 准备基础架构数据
	sub := models.Subsystem{Code: "SUB_API", Name: "API测试子系统"}
	db.Create(&sub)
	grp := models.FunctionalGroup{SubsystemID: sub.ID, Code: "GRP_API", Name: "API功能组", Path: fmt.Sprintf("/%d/", sub.ID), Level: 1}
	db.Create(&grp)
	libMod := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "LIB_API", Name: "契约库API", ModuleKind: models.ModuleKindContractLib}
	db.Create(&libMod)
	bizMod := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "BIZ_API", Name: "业务模块API", ModuleKind: models.ModuleKindBusiness}
	db.Create(&bizMod)

	// 1. SEC-M20-01: 普通用户调用 POST /api/arch/service-contracts 必返 403
	createBody, _ := json.Marshal(handlers.ServiceContractReq{
		ServiceName:     "demo.v1.RBACService",
		DefinerModuleID: handlers.FlexUint64(libMod.ID),
		ImplKeyLabel:    "分支",
	})
	req403, _ := http.NewRequest(http.MethodPost, "/api/arch/service-contracts", bytes.NewReader(createBody))
	req403.Header.Set("Authorization", "Bearer "+userToken)
	req403.Header.Set("Content-Type", "application/json")
	w403 := httptest.NewRecorder()
	router.ServeHTTP(w403, req403)
	if w403.Code != http.StatusForbidden {
		t.Fatalf("普通用户创建契约期望 403 Forbidden，实际: %d %s", w403.Code, w403.Body.String())
	}

	// 2. 管理员用户调用 POST /api/arch/service-contracts 成功 (201)
	req201, _ := http.NewRequest(http.MethodPost, "/api/arch/service-contracts", bytes.NewReader(createBody))
	req201.Header.Set("Authorization", "Bearer "+adminToken)
	req201.Header.Set("Content-Type", "application/json")
	w201 := httptest.NewRecorder()
	router.ServeHTTP(w201, req201)
	if w201.Code != http.StatusCreated {
		t.Fatalf("管理员创建契约期望 201 Created，实际: %d %s", w201.Code, w201.Body.String())
	}

	var created models.ServiceContract
	_ = json.Unmarshal(w201.Body.Bytes(), &created)
	if created.ID == 0 || created.ServiceName != "demo.v1.RBACService" {
		t.Fatalf("创建契约返回数据不正确: %+v", created)
	}

	// 3. GET /api/arch/service-contracts 普通用户亦可读 (200)
	reqGet, _ := http.NewRequest(http.MethodGet, "/api/arch/service-contracts", nil)
	reqGet.Header.Set("Authorization", "Bearer "+userToken)
	wGet := httptest.NewRecorder()
	router.ServeHTTP(wGet, reqGet)
	if wGet.Code != http.StatusOK {
		t.Fatalf("查询契约列表期望 200 OK，实际: %d %s", wGet.Code, wGet.Body.String())
	}

	// 4. GET /api/arch/service-contracts/:id
	reqDetail, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("/api/arch/service-contracts/%d", created.ID), nil)
	reqDetail.Header.Set("Authorization", "Bearer "+userToken)
	wDetail := httptest.NewRecorder()
	router.ServeHTTP(wDetail, reqDetail)
	if wDetail.Code != http.StatusOK {
		t.Fatalf("查询契约详情期望 200 OK，实际: %d %s", wDetail.Code, wDetail.Body.String())
	}

	// 5. 存量独占服务一键转换 API (POST /api/arch/service-contracts/convert)
	exclSvc := models.ModuleService{
		ModuleID:    bizMod.ID,
		ServiceName: "demo.v1.ConvertibleService",
		Description: "待转换独占服务",
	}
	db.Create(&exclSvc)

	convBody, _ := json.Marshal(handlers.ConvertExclusiveContractReq{
		ServiceID:                   handlers.FlexUint64(exclSvc.ID),
		DefinerModuleID:             handlers.FlexUint64(libMod.ID),
		ImplKeyLabel:                "实现标识",
		InitialImplKey:              "default_impl",
		ConvertDependentsToDispatch: true,
	})

	// 普通用户调用 convert 期望 403
	reqConv403, _ := http.NewRequest(http.MethodPost, "/api/arch/service-contracts/convert", bytes.NewReader(convBody))
	reqConv403.Header.Set("Authorization", "Bearer "+userToken)
	reqConv403.Header.Set("Content-Type", "application/json")
	wConv403 := httptest.NewRecorder()
	router.ServeHTTP(wConv403, reqConv403)
	if wConv403.Code != http.StatusForbidden {
		t.Fatalf("普通用户转换契约期望 403 Forbidden，实际: %d", wConv403.Code)
	}

	// 管理员用户调用 convert 成功 200
	reqConv200, _ := http.NewRequest(http.MethodPost, "/api/arch/service-contracts/convert", bytes.NewReader(convBody))
	reqConv200.Header.Set("Authorization", "Bearer "+adminToken)
	reqConv200.Header.Set("Content-Type", "application/json")
	wConv200 := httptest.NewRecorder()
	router.ServeHTTP(wConv200, reqConv200)
	if wConv200.Code != http.StatusOK {
		t.Fatalf("管理员转换契约期望 200 OK，实际: %d %s", wConv200.Code, wConv200.Body.String())
	}

	// 6. ADR-16 v2.0: 存量独占服务 (如 DH_SyncService) 通过 POST /api/arch/service-contracts/:id/attach 归入既有契约
	exclAttachSvc := models.ModuleService{
		ModuleID:    bizMod.ID,
		ServiceName: "DH_RBACService",
		Description: "自定义名称存量独占服务",
	}
	db.Create(&exclAttachSvc)

	attachBody, _ := json.Marshal(map[string]any{
		"service_id":                     exclAttachSvc.ID,
		"impl_key":                       "DH",
		"convert_dependents_to_dispatch": true,
	})
	reqAttach403, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("/api/arch/service-contracts/%d/attach", created.ID), bytes.NewReader(attachBody))
	reqAttach403.Header.Set("Authorization", "Bearer "+userToken)
	reqAttach403.Header.Set("Content-Type", "application/json")
	wAttach403 := httptest.NewRecorder()
	router.ServeHTTP(wAttach403, reqAttach403)
	if wAttach403.Code != http.StatusForbidden {
		t.Fatalf("普通用户归入独占服务期望 403 Forbidden，实际: %d", wAttach403.Code)
	}

	reqAttach200, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("/api/arch/service-contracts/%d/attach", created.ID), bytes.NewReader(attachBody))
	reqAttach200.Header.Set("Authorization", "Bearer "+adminToken)
	reqAttach200.Header.Set("Content-Type", "application/json")
	wAttach200 := httptest.NewRecorder()
	router.ServeHTTP(wAttach200, reqAttach200)
	if wAttach200.Code != http.StatusOK {
		t.Fatalf("管理员归入独占服务期望 200 OK，实际: %d %s", wAttach200.Code, wAttach200.Body.String())
	}

	// 7. 管理员为另一业务模块创建自定义运行期服务名实现 + PUT 重命名
	bizMod2 := models.Module{SubsystemID: sub.ID, GroupID: grp.ID, Code: "BIZ_API_2", Name: "业务模块API2", ModuleKind: models.ModuleKindBusiness}
	db.Create(&bizMod2)

	createImplBody, _ := json.Marshal(map[string]any{
		"contract_id":  created.ID,
		"impl_key":     "KH",
		"service_name": "KH_RBACService",
		"description":  "KH实现",
	})
	reqImpl201, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("/api/arch/modules/%d/services", bizMod2.ID), bytes.NewReader(createImplBody))
	reqImpl201.Header.Set("Authorization", "Bearer "+adminToken)
	reqImpl201.Header.Set("Content-Type", "application/json")
	wImpl201 := httptest.NewRecorder()
	router.ServeHTTP(wImpl201, reqImpl201)
	if wImpl201.Code != http.StatusCreated {
		t.Fatalf("创建自定义名实现期望 201 Created，实际: %d %s", wImpl201.Code, wImpl201.Body.String())
	}
	var createdImpl models.ModuleService
	_ = json.Unmarshal(wImpl201.Body.Bytes(), &createdImpl)
	if !createdImpl.IsCustomServiceName || createdImpl.ServiceName != "KH_RBACService" {
		t.Fatalf("返回实现属性不符: %+v", createdImpl)
	}

	updateBody, _ := json.Marshal(map[string]any{
		"service_name":      "KH_RBACService_V2",
		"impl_key":          "KH_V2",
		"diff_ignore_paths": []string{"timestamp"},
		"description":       "改名后的KH实现",
	})
	reqUpd, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("/api/arch/module-services/%d", createdImpl.ID), bytes.NewReader(updateBody))
	reqUpd.Header.Set("Authorization", "Bearer "+adminToken)
	reqUpd.Header.Set("Content-Type", "application/json")
	wUpd := httptest.NewRecorder()
	router.ServeHTTP(wUpd, reqUpd)
	if wUpd.Code != http.StatusOK {
		t.Fatalf("修改实现服务名期望 200 OK，实际: %d %s", wUpd.Code, wUpd.Body.String())
	}
	var updatedImpl models.ModuleService
	_ = json.Unmarshal(wUpd.Body.Bytes(), &updatedImpl)
	if updatedImpl.ServiceName != "KH_RBACService_V2" || updatedImpl.ImplKey != "KH_V2" {
		t.Fatalf("修改实现返回属性不符: %+v", updatedImpl)
	}
}
