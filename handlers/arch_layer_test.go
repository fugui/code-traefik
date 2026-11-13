package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"code-traefik/config"
)

func TestArchLayerGuardAndCycleHTTP409(t *testing.T) {
	if os.Getenv("TEST_DB_DSN") == "" {
		t.Skip("TEST_DB_DSN 未配置，跳过隔离集成测试")
	}

	r, db := setupRouterAndDB(t)
	admin := createTestUserInDB(t, db, "arch_layer_admin", config.AdminRole)
	token := generateTestJWT(t, admin)

	doJSON := func(method, path string, payload any) *httptest.ResponseRecorder {
		var bodyBytes []byte
		if payload != nil {
			bodyBytes, _ = json.Marshal(payload)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, req)
		return resp
	}

	// 1. 非法 layer_level (-1 / 100) 返回 400
	if resp := doJSON(http.MethodPost, "/api/arch/subsystems", map[string]any{
		"code": "SUB_BAD_LAYER", "name": "越界子系统", "layer_level": 100,
	}); resp.Code != http.StatusBadRequest {
		t.Fatalf("layer_level=100 应返回 400，实测 %d: %s", resp.Code, resp.Body.String())
	}

	// 2. 创建 Sub(L2)、Group、ModA(继承=L2)、ModB(L3)、ContractLib(L3)
	subResp := doJSON(http.MethodPost, "/api/arch/subsystems", map[string]any{
		"code": "SUB_HTTP_L2", "name": "HTTP二层子系统", "layer_level": 2,
	})
	if subResp.Code != http.StatusCreated {
		t.Fatalf("创建子系统失败: %d %s", subResp.Code, subResp.Body.String())
	}
	var subObj struct {
		ID         uint64 `json:"id"`
		LayerLevel int    `json:"layer_level"`
	}
	_ = json.Unmarshal(subResp.Body.Bytes(), &subObj)
	if subObj.LayerLevel != 2 {
		t.Fatalf("期望子系统 layer_level=2，实测 %d", subObj.LayerLevel)
	}

	grpResp := doJSON(http.MethodPost, "/api/arch/functional-groups", map[string]any{
		"subsystem_id": subObj.ID, "code": "GRP_HTTP", "name": "HTTP组",
	})
	if grpResp.Code != http.StatusCreated {
		t.Fatalf("创建功能组失败: %d %s", grpResp.Code, grpResp.Body.String())
	}
	var grpObj struct {
		ID uint64 `json:"id"`
	}
	_ = json.Unmarshal(grpResp.Body.Bytes(), &grpObj)

	modAResp := doJSON(http.MethodPost, "/api/arch/modules", map[string]any{
		"group_id": grpObj.ID, "code": "MOD_HTTP_L2", "name": "L2继承模块",
	})
	if modAResp.Code != http.StatusCreated {
		t.Fatalf("创建 ModA 失败: %d %s", modAResp.Code, modAResp.Body.String())
	}
	var modA, modB, libMod struct {
		ID         uint64 `json:"id"`
		LayerLevel int    `json:"layer_level"`
	}
	_ = json.Unmarshal(modAResp.Body.Bytes(), &modA)

	modBResp := doJSON(http.MethodPost, "/api/arch/modules", map[string]any{
		"group_id": grpObj.ID, "code": "MOD_HTTP_L3", "name": "L3高层模块", "layer_level": 3,
	})
	if modBResp.Code != http.StatusCreated {
		t.Fatalf("创建 ModB 失败: %d %s", modBResp.Code, modBResp.Body.String())
	}
	_ = json.Unmarshal(modBResp.Body.Bytes(), &modB)

	libResp := doJSON(http.MethodPost, "/api/arch/modules", map[string]any{
		"group_id": grpObj.ID, "code": "LIB_HTTP_L3", "name": "L3契约库", "module_kind": "CONTRACT_LIB", "layer_level": 3,
	})
	if libResp.Code != http.StatusCreated {
		t.Fatalf("创建 Lib 失败: %d %s", libResp.Code, libResp.Body.String())
	}
	_ = json.Unmarshal(libResp.Body.Bytes(), &libMod)

	// PUT /api/arch/modules/:id 未传 layer_level 保留原值 3；显式传 0 则重置为 0
	keepResp := doJSON(http.MethodPut, "/api/arch/modules/"+strconv.FormatUint(modB.ID, 10), map[string]any{
		"group_id": grpObj.ID, "code": "MOD_HTTP_L3", "name": "L3高层模块-改名",
	})
	if keepResp.Code != http.StatusOK {
		t.Fatalf("PUT retain 失败: %d %s", keepResp.Code, keepResp.Body.String())
	}
	_ = json.Unmarshal(keepResp.Body.Bytes(), &modB)
	if modB.LayerLevel != 3 {
		t.Fatalf("未传 layer_level 应保留 3，实测 %d", modB.LayerLevel)
	}

	// 3. ModB(L3) 提供服务 -> ModA(eff=2) 反向依赖 -> 409 ERR_LAYER_VIOLATION
	svcBResp := doJSON(http.MethodPost, "/api/arch/modules/"+strconv.FormatUint(modB.ID, 10)+"/services", map[string]any{
		"service_name": "http.v1.HighSvc",
	})
	if svcBResp.Code != http.StatusCreated {
		t.Fatalf("创建 HighSvc 失败: %d %s", svcBResp.Code, svcBResp.Body.String())
	}
	var svcB struct {
		ID uint64 `json:"id"`
	}
	_ = json.Unmarshal(svcBResp.Body.Bytes(), &svcB)

	depUpResp := doJSON(http.MethodPost, "/api/arch/modules/"+strconv.FormatUint(modA.ID, 10)+"/dependencies", map[string]any{
		"service_id": svcB.ID,
	})
	if depUpResp.Code != http.StatusConflict {
		t.Fatalf("L2->L3 向上依赖应返回 409，实测 %d: %s", depUpResp.Code, depUpResp.Body.String())
	}
	var errPayload map[string]any
	if err := json.Unmarshal(depUpResp.Body.Bytes(), &errPayload); err != nil {
		t.Fatalf("解析 depUpResp 失败: %v", err)
	}
	if errPayload["code"] != "ERR_LAYER_VIOLATION" {
		t.Fatalf("期望 code=ERR_LAYER_VIOLATION，实测: %+v", errPayload)
	}

	// 4. L3 契约被 L2 ModA 实现 -> 409 ERR_REALIZATION_LAYER_INVERSION
	contractResp := doJSON(http.MethodPost, "/api/arch/service-contracts", map[string]any{
		"service_name":      "http.v1.Contract",
		"definer_module_id": libMod.ID,
	})
	if contractResp.Code != http.StatusCreated {
		t.Fatalf("创建契约失败: %d %s", contractResp.Code, contractResp.Body.String())
	}
	var contractObj struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(contractResp.Body.Bytes(), &contractObj); err != nil {
		t.Fatalf("解析 contractResp 失败: %v", err)
	}

	realizeResp := doJSON(http.MethodPost, "/api/arch/modules/"+strconv.FormatUint(modA.ID, 10)+"/services", map[string]any{
		"contract_id": contractObj.ID,
		"impl_key":    "bad_impl",
	})
	if realizeResp.Code != http.StatusConflict {
		t.Fatalf("L2 实现 L3 契约应返回 409，实测 %d: %s", realizeResp.Code, realizeResp.Body.String())
	}
	errPayload = map[string]any{}
	if err := json.Unmarshal(realizeResp.Body.Bytes(), &errPayload); err != nil {
		t.Fatalf("解析 realizeResp 失败: %v", err)
	}
	if errPayload["code"] != "ERR_REALIZATION_LAYER_INVERSION" {
		t.Fatalf("期望 code=ERR_REALIZATION_LAYER_INVERSION，实测: %+v", errPayload)
	}

	// 5. 同层闭环检测：ModA 提供 SvcA，将 ModB 显式重置为 layer_level=0 (继承子系统L2)，先 ModA->ModB 还是先同层 A<->B
	resetResp := doJSON(http.MethodPut, "/api/arch/modules/"+strconv.FormatUint(modB.ID, 10), map[string]any{
		"group_id": grpObj.ID, "code": "MOD_HTTP_L3", "name": "重置为继承L2", "layer_level": 0,
	})
	if resetResp.Code != http.StatusOK {
		t.Fatalf("PUT reset=0 失败: %d %s", resetResp.Code, resetResp.Body.String())
	}
	if err := json.Unmarshal(resetResp.Body.Bytes(), &modB); err != nil {
		t.Fatalf("解析 resetResp 失败: %v", err)
	}
	if modB.LayerLevel != 0 {
		t.Fatalf("显式传 0 应重置为 0，实测 %d", modB.LayerLevel)
	}

	svcAResp := doJSON(http.MethodPost, "/api/arch/modules/"+strconv.FormatUint(modA.ID, 10)+"/services", map[string]any{
		"service_name": "http.v1.SvcA",
	})
	var svcA struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(svcAResp.Body.Bytes(), &svcA); err != nil {
		t.Fatalf("解析 svcAResp 失败: %v", err)
	}

	if okDep := doJSON(http.MethodPost, "/api/arch/modules/"+strconv.FormatUint(modA.ID, 10)+"/dependencies", map[string]any{
		"service_id": svcB.ID,
	}); okDep.Code != http.StatusCreated {
		t.Fatalf("同层 ModA->ModB 应成功，实测 %d: %s", okDep.Code, okDep.Body.String())
	}
	cycDep := doJSON(http.MethodPost, "/api/arch/modules/"+strconv.FormatUint(modB.ID, 10)+"/dependencies", map[string]any{
		"service_id": svcA.ID,
	})
	if cycDep.Code != http.StatusConflict {
		t.Fatalf("闭环 ModB->ModA 应返回 409，实测 %d: %s", cycDep.Code, cycDep.Body.String())
	}
	errPayload = map[string]any{}
	if err := json.Unmarshal(cycDep.Body.Bytes(), &errPayload); err != nil {
		t.Fatalf("解析 cycDep 失败: %v", err)
	}
	if errPayload["code"] != "ERR_CYCLIC_DEPENDENCY" {
		t.Fatalf("期望 code=ERR_CYCLIC_DEPENDENCY，实测: %+v", errPayload)
	}

	// 6. GET /api/arch/topology/graph?view=logical 验证 meta.effective_layer_level
	graphResp := doJSON(http.MethodGet, "/api/arch/topology/graph?view=logical", nil)
	if graphResp.Code != http.StatusOK {
		t.Fatalf("GET topology/graph 失败: %d %s", graphResp.Code, graphResp.Body.String())
	}
	var graphDTO struct {
		Nodes []struct {
			ID   string         `json:"id"`
			Type string         `json:"type"`
			Meta map[string]any `json:"meta"`
		} `json:"nodes"`
	}
	_ = json.Unmarshal(graphResp.Body.Bytes(), &graphDTO)
	foundModA := false
	for _, n := range graphDTO.Nodes {
		if n.ID == "mod_"+strconv.FormatUint(modA.ID, 10) {
			foundModA = true
			if n.Meta["effective_layer_level"] != float64(2) {
				t.Fatalf("期望 ModA effective_layer_level=2，实测: %+v", n.Meta)
			}
		}
	}
	if !foundModA {
		t.Fatalf("拓扑图响应中未找到 ModA 节点")
	}
}
