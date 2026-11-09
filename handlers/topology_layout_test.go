package handlers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	commonAudit "code-common/backend/audit"
	commonModels "code-common/backend/models"
	"code-traefik/config"
	"code-traefik/models"
	"context"
)

func TestTopologyLayout_SchemaConstraints_ScopeOwnerAndPartialUniques(t *testing.T) {
	_, db := setupRouterAndDB(t)
	userA := createTestUserInDB(t, db, "layout_schema_user_a", "user")

	// 1. SHARED + owner_id != NULL 违反 chk_traefik_topo_layout_owner
	badSharedOwner := models.TopologyLayout{
		Scope:      models.ScopeShared,
		View:       "LOGICAL",
		OwnerID:    &userA.ID,
		Name:       "非法SHARED带Owner",
		FocusScope: "GLOBAL",
		IsDefault:  false,
	}
	if err := db.Create(&badSharedOwner).Error; err == nil {
		t.Fatalf("期望 SHARED + owner_id!=NULL 违反 CHECK 约束，但插入成功")
	}

	// 2. PRIVATE + owner_id IS NULL 违反 chk_traefik_topo_layout_owner
	badPrivateNoOwner := models.TopologyLayout{
		Scope:      models.ScopePrivate,
		View:       "LOGICAL",
		OwnerID:    nil,
		Name:       "非法PRIVATE无Owner",
		FocusScope: "GLOBAL",
	}
	if err := db.Create(&badPrivateNoOwner).Error; err == nil {
		t.Fatalf("期望 PRIVATE + owner_id IS NULL 违反 CHECK 约束，但插入成功")
	}

	// 3. SHARED + focus_scope != GLOBAL 或 is_default = true 违反 chk_traefik_topo_layout_owner
	badSharedFocus := models.TopologyLayout{
		Scope:      models.ScopeShared,
		View:       "LOGICAL",
		OwnerID:    nil,
		Name:       "非法SHARED聚焦",
		FocusScope: "sub_1",
		IsDefault:  false,
	}
	if err := db.Create(&badSharedFocus).Error; err == nil {
		t.Fatalf("期望 SHARED + focus_scope!=GLOBAL 违反 CHECK 约束，但插入成功")
	}

	// 4. 同 view 第二条 SHARED 违反 uk_traefik_topo_layout_shared
	shared1 := models.TopologyLayout{
		Scope:      models.ScopeShared,
		View:       "LOGICAL",
		OwnerID:    nil,
		Name:       "团队基准布局",
		FocusScope: "GLOBAL",
		IsDefault:  false,
	}
	if err := db.Create(&shared1).Error; err != nil {
		t.Fatalf("插入合法 SHARED 失败: %v", err)
	}
	shared2 := models.TopologyLayout{
		Scope:      models.ScopeShared,
		View:       "LOGICAL",
		OwnerID:    nil,
		Name:       "团队基准布局2",
		FocusScope: "GLOBAL",
		IsDefault:  false,
	}
	if err := db.Create(&shared2).Error; err == nil {
		t.Fatalf("期望同 view 第二条 SHARED 违反部分唯一索引 uk_traefik_topo_layout_shared，但插入成功")
	}

	// 5. 同人同视图同名书签违反 uk_traefik_topo_layout_private_name
	priv1 := models.TopologyLayout{
		Scope:      models.ScopePrivate,
		View:       "LOGICAL",
		OwnerID:    &userA.ID,
		Name:       "我的交易核心书签",
		FocusScope: "GLOBAL",
		IsDefault:  true,
	}
	if err := db.Create(&priv1).Error; err != nil {
		t.Fatalf("插入合法 PRIVATE 书签失败: %v", err)
	}
	privDupName := models.TopologyLayout{
		Scope:      models.ScopePrivate,
		View:       "LOGICAL",
		OwnerID:    &userA.ID,
		Name:       "我的交易核心书签",
		FocusScope: "sub_1",
		IsDefault:  false,
	}
	if err := db.Create(&privDupName).Error; err == nil {
		t.Fatalf("期望同人同视图同名书签违反 uk_traefik_topo_layout_private_name，但插入成功")
	}

	// 6. 同 (owner, view, focus_scope) 第二个默认书签违反 uk_traefik_topo_layout_private_default
	privDupDefault := models.TopologyLayout{
		Scope:      models.ScopePrivate,
		View:       "LOGICAL",
		OwnerID:    &userA.ID,
		Name:       "第二个默认书签",
		FocusScope: "GLOBAL",
		IsDefault:  true,
	}
	if err := db.Create(&privDupDefault).Error; err == nil {
		t.Fatalf("期望同 (owner, view, focus_scope) 第二个默认书签违反 uk_traefik_topo_layout_private_default，但插入成功")
	}
}

func TestTopologyLayout_CanonicalUpsert_RevisionConflictAndReset_RBAC(t *testing.T) {
	r, db := setupRouterAndDB(t)
	admin := createTestUserInDB(t, db, "topo_admin", config.AdminRole)
	user := createTestUserInDB(t, db, "topo_user", "user")
	adminToken := generateTestJWT(t, admin)
	userToken := generateTestJWT(t, user)

	canonicalPayload := map[string]any{
		"view":              "LOGICAL",
		"expected_revision": 0,
		"structure_version": "s1-LOGICAL-0102030405060708",
		"node_positions": map[string]any{
			"sub_1": map[string]any{"x": 120.5, "y": 80.0, "pinned": true},
			"sub_2": map[string]any{"x": 520.0, "y": 80.0},
		},
		"expand_state": map[string]any{
			"level":     0,
			"overrides": map[string]bool{"sub_1": true},
		},
		"viewport": map[string]any{
			"x":    10.0,
			"y":    20.0,
			"zoom": 0.85,
		},
	}
	bodyBytes, _ := json.Marshal(canonicalPayload)

	// 1. 普通用户 PUT / DELETE canonical 返 403
	reqForbiddenPut := httptest.NewRequest(http.MethodPut, "/api/arch/topology/layouts/canonical", bytes.NewReader(bodyBytes))
	reqForbiddenPut.Header.Set("Authorization", "Bearer "+userToken)
	reqForbiddenPut.Header.Set("Content-Type", "application/json")
	wForbiddenPut := httptest.NewRecorder()
	r.ServeHTTP(wForbiddenPut, reqForbiddenPut)
	if wForbiddenPut.Code != http.StatusForbidden {
		t.Fatalf("普通用户 PUT canonical 期望 403，实际: %d %s", wForbiddenPut.Code, wForbiddenPut.Body.String())
	}

	reqForbiddenDel := httptest.NewRequest(http.MethodDelete, "/api/arch/topology/layouts/canonical?view=LOGICAL", nil)
	reqForbiddenDel.Header.Set("Authorization", "Bearer "+userToken)
	wForbiddenDel := httptest.NewRecorder()
	r.ServeHTTP(wForbiddenDel, reqForbiddenDel)
	if wForbiddenDel.Code != http.StatusForbidden {
		t.Fatalf("普通用户 DELETE canonical 期望 403，实际: %d %s", wForbiddenDel.Code, wForbiddenDel.Body.String())
	}

	// 2. 首次创建时 expected_revision != 0 返 409 LAYOUT_REVISION_CONFLICT
	badFirstPayload := map[string]any{
		"view":              "LOGICAL",
		"expected_revision": 1,
		"structure_version": "s1-LOGICAL-0102030405060708",
		"node_positions":    map[string]any{},
	}
	badFirstBytes, _ := json.Marshal(badFirstPayload)
	reqBadFirst := httptest.NewRequest(http.MethodPut, "/api/arch/topology/layouts/canonical", bytes.NewReader(badFirstBytes))
	reqBadFirst.Header.Set("Authorization", "Bearer "+adminToken)
	reqBadFirst.Header.Set("Content-Type", "application/json")
	wBadFirst := httptest.NewRecorder()
	r.ServeHTTP(wBadFirst, reqBadFirst)
	if wBadFirst.Code != http.StatusConflict || !strings.Contains(wBadFirst.Body.String(), "LAYOUT_REVISION_CONFLICT") {
		t.Fatalf("首次创建 expected_revision!=0 期望 409 LAYOUT_REVISION_CONFLICT，实际: %d %s", wBadFirst.Code, wBadFirst.Body.String())
	}

	// 3. 管理员 expected_revision=0 创建成功 (revision=1)
	reqCreate := httptest.NewRequest(http.MethodPut, "/api/arch/topology/layouts/canonical", bytes.NewReader(bodyBytes))
	reqCreate.Header.Set("Authorization", "Bearer "+adminToken)
	reqCreate.Header.Set("Content-Type", "application/json")
	wCreate := httptest.NewRecorder()
	r.ServeHTTP(wCreate, reqCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("管理员首次创建 canonical 期望 200，实际: %d %s", wCreate.Code, wCreate.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(wCreate.Body.Bytes(), &created)
	if int(created["revision"].(float64)) != 1 {
		t.Fatalf("首次创建后 revision 期望为 1，实际: %v", created["revision"])
	}
	if created["scope"] != "SHARED" || created["focus_scope"] != "GLOBAL" {
		t.Fatalf("SHARED 基准字段不符合预期: %+v", created)
	}

	// 4. 管理员携带正确 expected_revision=1 更新成功 (revision=2)
	canonicalPayload["expected_revision"] = 1
	canonicalPayload["node_positions"] = map[string]any{
		"sub_1": map[string]any{"x": 200.0, "y": 100.0, "pinned": true},
	}
	updBytes, _ := json.Marshal(canonicalPayload)
	reqUpd := httptest.NewRequest(http.MethodPut, "/api/arch/topology/layouts/canonical", bytes.NewReader(updBytes))
	reqUpd.Header.Set("Authorization", "Bearer "+adminToken)
	reqUpd.Header.Set("Content-Type", "application/json")
	wUpd := httptest.NewRecorder()
	r.ServeHTTP(wUpd, reqUpd)
	if wUpd.Code != http.StatusOK {
		t.Fatalf("管理员携带正确 revision 更新期望 200，实际: %d %s", wUpd.Code, wUpd.Body.String())
	}
	var updated map[string]any
	_ = json.Unmarshal(wUpd.Body.Bytes(), &updated)
	if int(updated["revision"].(float64)) != 2 {
		t.Fatalf("第二次更新后 revision 期望为 2，实际: %v", updated["revision"])
	}

	// 5. 携带过期 expected_revision=1 再次更新返 409 LAYOUT_REVISION_CONFLICT
	reqConflict := httptest.NewRequest(http.MethodPut, "/api/arch/topology/layouts/canonical", bytes.NewReader(updBytes))
	reqConflict.Header.Set("Authorization", "Bearer "+adminToken)
	reqConflict.Header.Set("Content-Type", "application/json")
	wConflict := httptest.NewRecorder()
	r.ServeHTTP(wConflict, reqConflict)
	if wConflict.Code != http.StatusConflict || !strings.Contains(wConflict.Body.String(), "LAYOUT_REVISION_CONFLICT") {
		t.Fatalf("过期 revision 期望 409 LAYOUT_REVISION_CONFLICT，实际: %d %s", wConflict.Code, wConflict.Body.String())
	}

	// 6. DELETE 成功后再次 DELETE 返 404
	reqDel1 := httptest.NewRequest(http.MethodDelete, "/api/arch/topology/layouts/canonical?view=LOGICAL", nil)
	reqDel1.Header.Set("Authorization", "Bearer "+adminToken)
	wDel1 := httptest.NewRecorder()
	r.ServeHTTP(wDel1, reqDel1)
	if wDel1.Code != http.StatusOK {
		t.Fatalf("首次 DELETE canonical 期望 200，实际: %d %s", wDel1.Code, wDel1.Body.String())
	}
	reqDel2 := httptest.NewRequest(http.MethodDelete, "/api/arch/topology/layouts/canonical?view=LOGICAL", nil)
	reqDel2.Header.Set("Authorization", "Bearer "+adminToken)
	wDel2 := httptest.NewRecorder()
	r.ServeHTTP(wDel2, reqDel2)
	if wDel2.Code != http.StatusNotFound {
		t.Fatalf("重复 DELETE canonical 期望 404，实际: %d %s", wDel2.Code, wDel2.Body.String())
	}

	// 7. 校验审计日志仅含摘要字段（不含大 JSON node_positions 坐标内容）
	_ = commonAudit.Close(context.Background())
	var logs []commonModels.SysAuditLog
	if err := db.Where("target_type = ?", "topology_layout").Find(&logs).Error; err != nil {
		t.Fatalf("查询审计日志失败: %v", err)
	}
	if len(logs) == 0 {
		t.Fatalf("期望产生 topology_layout 审计日志")
	}
	for _, lg := range logs {
		rawDiff := lg.BeforeData + "|" + lg.AfterData + "|" + lg.DiffSummary
		if strings.Contains(rawDiff, "sub_1") || strings.Contains(rawDiff, "node_positions") {
			t.Fatalf("审计日志严禁包含大 JSON node_positions 明细，实际: %s", rawDiff)
		}
	}
}

func TestTopologyLayout_PersonalCRUD_DefaultMutex_QuotaAndAntiIDOR(t *testing.T) {
	r, db := setupRouterAndDB(t)
	userA := createTestUserInDB(t, db, "personal_user_a", "user")
	userB := createTestUserInDB(t, db, "personal_user_b", "user")
	admin := createTestUserInDB(t, db, "personal_admin", config.AdminRole)
	tokenA := generateTestJWT(t, userA)
	tokenB := generateTestJWT(t, userB)
	adminToken := generateTestJWT(t, admin)

	// 准备一条 SHARED 基准行，用于验证个人接口作用于 SHARED 行返 404
	sharedRow := models.TopologyLayout{
		Scope:      models.ScopeShared,
		View:       "LOGICAL",
		Name:       "团队基准布局",
		FocusScope: "GLOBAL",
		IsDefault:  false,
	}
	if err := db.Create(&sharedRow).Error; err != nil {
		t.Fatalf("创建 SHARED 失败: %v", err)
	}

	createBookmark := func(token, name, focusScope string, isDefault bool) *httptest.ResponseRecorder {
		payload := map[string]any{
			"name":              name,
			"view":              "LOGICAL",
			"focus_scope":       focusScope,
			"is_default":        isDefault,
			"structure_version": "s1-LOGICAL-abcdef0123456789",
			"node_positions": map[string]any{
				"mod_1": map[string]any{"x": 40.0, "y": 50.0, "pinned": true},
			},
			"filter_state": map[string]any{
				"only_my_scope": true,
				"status":        "ABNORMAL",
				"ego_hops":      1,
			},
		}
		b, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/arch/topology/layouts/personal", bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	// 1. 用户 A 创建书签 1 (is_default=true) 与书签 2 (is_default=true)，验证默认书签互斥
	w1 := createBookmark(tokenA, "A的书签1", "GLOBAL", true)
	if w1.Code != http.StatusCreated {
		t.Fatalf("创建书签1期望 201，实际: %d %s", w1.Code, w1.Body.String())
	}
	var bm1 map[string]any
	_ = json.Unmarshal(w1.Body.Bytes(), &bm1)
	bm1ID := uint64(bm1["id"].(float64))

	w2 := createBookmark(tokenA, "A的书签2", "GLOBAL", true)
	if w2.Code != http.StatusCreated {
		t.Fatalf("创建书签2期望 201，实际: %d %s", w2.Code, w2.Body.String())
	}
	var bm2 map[string]any
	_ = json.Unmarshal(w2.Body.Bytes(), &bm2)
	bm2ID := uint64(bm2["id"].(float64))

	// 检查数据库内 bm1 的 is_default 已自动置为 false，bm2 为 true
	var row1, row2 models.TopologyLayout
	_ = db.First(&row1, bm1ID).Error
	_ = db.First(&row2, bm2ID).Error
	if row1.IsDefault || !row2.IsDefault {
		t.Fatalf("默认书签互斥失败: row1.IsDefault=%v, row2.IsDefault=%v", row1.IsDefault, row2.IsDefault)
	}

	// 通过 PUT 将 bm1 重新设为默认，检查 bm2 自动置 false
	updDefaultBody, _ := json.Marshal(map[string]any{"is_default": true, "name": "A的书签1-改"})
	reqUpd1 := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/arch/topology/layouts/personal/%d", bm1ID), bytes.NewReader(updDefaultBody))
	reqUpd1.Header.Set("Authorization", "Bearer "+tokenA)
	reqUpd1.Header.Set("Content-Type", "application/json")
	wUpd1 := httptest.NewRecorder()
	r.ServeHTTP(wUpd1, reqUpd1)
	if wUpd1.Code != http.StatusOK {
		t.Fatalf("更新书签1期望 200，实际: %d %s", wUpd1.Code, wUpd1.Body.String())
	}
	_ = db.First(&row1, bm1ID).Error
	_ = db.First(&row2, bm2ID).Error
	if !row1.IsDefault || row2.IsDefault {
		t.Fatalf("PUT 切换默认书签互斥失败: row1.IsDefault=%v, row2.IsDefault=%v", row1.IsDefault, row2.IsDefault)
	}

	// 2. 同名创建或重命名返 409
	wDup := createBookmark(tokenA, "A的书签2", "sub_1", false)
	if wDup.Code != http.StatusConflict {
		t.Fatalf("同名书签创建期望 409，实际: %d %s", wDup.Code, wDup.Body.String())
	}

	// 3. Anti-IDOR: 用户 B 列表不含 A 的书签；B 读取/修改/删除 A 的书签返 403
	reqListB := httptest.NewRequest(http.MethodGet, "/api/arch/topology/layouts?view=LOGICAL", nil)
	reqListB.Header.Set("Authorization", "Bearer "+tokenB)
	wListB := httptest.NewRecorder()
	r.ServeHTTP(wListB, reqListB)
	if wListB.Code != http.StatusOK {
		t.Fatalf("用户 B 获取列表期望 200，实际: %d", wListB.Code)
	}
	var listRespB struct {
		PersonalViews []map[string]any `json:"personal_views"`
	}
	_ = json.Unmarshal(wListB.Body.Bytes(), &listRespB)
	if len(listRespB.PersonalViews) != 0 {
		t.Fatalf("用户 B 列表不应包含用户 A 的书签，实际: %+v", listRespB.PersonalViews)
	}

	reqGetB := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/arch/topology/layouts/personal/%d", bm1ID), nil)
	reqGetB.Header.Set("Authorization", "Bearer "+tokenB)
	wGetB := httptest.NewRecorder()
	r.ServeHTTP(wGetB, reqGetB)
	if wGetB.Code != http.StatusForbidden {
		t.Fatalf("用户 B 读取用户 A 书签期望 403，实际: %d", wGetB.Code)
	}

	reqPutB := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/arch/topology/layouts/personal/%d", bm1ID), bytes.NewReader([]byte(`{"name":"hack"}`)))
	reqPutB.Header.Set("Authorization", "Bearer "+tokenB)
	reqPutB.Header.Set("Content-Type", "application/json")
	wPutB := httptest.NewRecorder()
	r.ServeHTTP(wPutB, reqPutB)
	if wPutB.Code != http.StatusForbidden {
		t.Fatalf("用户 B 修改用户 A 书签期望 403，实际: %d", wPutB.Code)
	}

	reqDelB := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/arch/topology/layouts/personal/%d", bm1ID), nil)
	reqDelB.Header.Set("Authorization", "Bearer "+tokenB)
	wDelB := httptest.NewRecorder()
	r.ServeHTTP(wDelB, reqDelB)
	if wDelB.Code != http.StatusForbidden {
		t.Fatalf("用户 B 删除用户 A 书签期望 403，实际: %d", wDelB.Code)
	}

	// 4. 个人接口作用于 SHARED 行返 404
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		reqShared := httptest.NewRequest(method, fmt.Sprintf("/api/arch/topology/layouts/personal/%d", sharedRow.ID), bytes.NewReader([]byte(`{"name":"x"}`)))
		reqShared.Header.Set("Authorization", "Bearer "+adminToken)
		reqShared.Header.Set("Content-Type", "application/json")
		wShared := httptest.NewRecorder()
		r.ServeHTTP(wShared, reqShared)
		if wShared.Code != http.StatusNotFound {
			t.Fatalf("个人接口 [%s] 作用于 SHARED 行期望 404，实际: %d %s", method, wShared.Code, wShared.Body.String())
		}
	}

	// 5. 配额限制：用户 A 已有 2 个书签，继续创建至 20 个成功，第 21 个返 400 PERSONAL_VIEW_QUOTA_EXCEEDED
	for i := 3; i <= 20; i++ {
		wi := createBookmark(tokenA, fmt.Sprintf("A的书签_%d", i), "GLOBAL", false)
		if wi.Code != http.StatusCreated {
			t.Fatalf("创建第 %d 个书签期望 201，实际: %d %s", i, wi.Code, wi.Body.String())
		}
	}
	w21 := createBookmark(tokenA, "A的书签_21_超限", "GLOBAL", false)
	if w21.Code != http.StatusBadRequest || !strings.Contains(w21.Body.String(), "PERSONAL_VIEW_QUOTA_EXCEEDED") {
		t.Fatalf("第 21 个书签期望 400 PERSONAL_VIEW_QUOTA_EXCEEDED，实际: %d %s", w21.Code, w21.Body.String())
	}

	// 6. 管理员可删除用户 A 的书签
	reqAdminDel := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/arch/topology/layouts/personal/%d", bm2ID), nil)
	reqAdminDel.Header.Set("Authorization", "Bearer "+adminToken)
	wAdminDel := httptest.NewRecorder()
	r.ServeHTTP(wAdminDel, reqAdminDel)
	if wAdminDel.Code != http.StatusOK {
		t.Fatalf("管理员删除用户 A 书签期望 200，实际: %d %s", wAdminDel.Code, wAdminDel.Body.String())
	}
}

func TestTopologyLayout_ConcurrentCreate_QuotaAndDefaultNeverViolated(t *testing.T) {
	r, db := setupRouterAndDB(t)
	user := createTestUserInDB(t, db, "concurrent_user", "user")
	token := generateTestJWT(t, user)

	// 先为用户创建 19 个书签
	for i := 1; i <= 19; i++ {
		bm := models.TopologyLayout{
			Scope:      models.ScopePrivate,
			View:       "LOGICAL",
			OwnerID:    &user.ID,
			Name:       fmt.Sprintf("预置书签_%d", i),
			FocusScope: "GLOBAL",
			IsDefault:  i == 1,
		}
		if err := db.Create(&bm).Error; err != nil {
			t.Fatalf("预置书签 %d 失败: %v", i, err)
		}
	}

	// 并发发起 10 个创建请求（同时携带 is_default=true），最终总数必须恰为 20，且默认书签恰为 1 个
	const workers = 10
	var wg sync.WaitGroup
	wg.Add(workers)
	createdCount := 0
	var mu sync.Mutex

	for wIdx := 0; wIdx < workers; wIdx++ {
		go func(idx int) {
			defer wg.Done()
			payload := map[string]any{
				"name":              fmt.Sprintf("并发书签_%d", idx),
				"view":              "LOGICAL",
				"focus_scope":       "GLOBAL",
				"is_default":        true,
				"structure_version": "s1-LOGICAL-1111111111111111",
				"node_positions":    map[string]any{},
			}
			b, _ := json.Marshal(payload)
			req := httptest.NewRequest(http.MethodPost, "/api/arch/topology/layouts/personal", bytes.NewReader(b))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code == http.StatusCreated {
				mu.Lock()
				createdCount++
				mu.Unlock()
			}
		}(wIdx)
	}
	wg.Wait()

	if createdCount != 1 {
		t.Fatalf("已有 19 个书签时并发 10 个创建，期望恰好成功 1 个，实际成功: %d", createdCount)
	}

	var totalCount int64
	db.Model(&models.TopologyLayout{}).Where("scope = ? AND owner_id = ?", models.ScopePrivate, user.ID).Count(&totalCount)
	if totalCount != 20 {
		t.Fatalf("并发后书签总数期望恰为 20，实际: %d", totalCount)
	}

	var defaultCount int64
	db.Model(&models.TopologyLayout{}).Where("scope = ? AND owner_id = ? AND view = ? AND focus_scope = ? AND is_default = ?", models.ScopePrivate, user.ID, "LOGICAL", "GLOBAL", true).Count(&defaultCount)
	if defaultCount != 1 {
		t.Fatalf("并发设置默认后同域默认书签期望恰为 1 个，实际: %d", defaultCount)
	}
}

func TestTopologyLayout_InputGuards_PayloadLimitMassAssignmentAndNaN(t *testing.T) {
	r, db := setupRouterAndDB(t)
	user := createTestUserInDB(t, db, "guard_user", "user")
	otherUser := createTestUserInDB(t, db, "other_user", "user")
	token := generateTestJWT(t, user)

	// 1. 请求体 > 1MB 返 413 PAYLOAD_TOO_LARGE
	hugeDesc := strings.Repeat("A", (1<<20)+1024)
	hugeBody := fmt.Sprintf(`{"name":"huge","view":"LOGICAL","description":"%s","node_positions":{}}`, hugeDesc)
	reqHuge := httptest.NewRequest(http.MethodPost, "/api/arch/topology/layouts/personal", strings.NewReader(hugeBody))
	reqHuge.Header.Set("Authorization", "Bearer "+token)
	reqHuge.Header.Set("Content-Type", "application/json")
	wHuge := httptest.NewRecorder()
	r.ServeHTTP(wHuge, reqHuge)
	if wHuge.Code != http.StatusRequestEntityTooLarge || !strings.Contains(wHuge.Body.String(), "PAYLOAD_TOO_LARGE") {
		t.Fatalf("超过 1MB 请求体期望 413 PAYLOAD_TOO_LARGE，实际: %d %s", wHuge.Code, wHuge.Body.String())
	}

	// 2. Body 夹带 scope="SHARED" / owner_id / revision 被忽略（防批量赋值）
	massBody, _ := json.Marshal(map[string]any{
		"name":              "防批量赋值测试",
		"view":              "LOGICAL",
		"scope":             "SHARED",
		"owner_id":          otherUser.ID,
		"revision":          999,
		"structure_version": "s1-LOGICAL-0000000000000000",
		"node_positions":    map[string]any{},
	})
	reqMass := httptest.NewRequest(http.MethodPost, "/api/arch/topology/layouts/personal", bytes.NewReader(massBody))
	reqMass.Header.Set("Authorization", "Bearer "+token)
	reqMass.Header.Set("Content-Type", "application/json")
	wMass := httptest.NewRecorder()
	r.ServeHTTP(wMass, reqMass)
	if wMass.Code != http.StatusCreated {
		t.Fatalf("创建书签期望 201，实际: %d %s", wMass.Code, wMass.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(wMass.Body.Bytes(), &created)
	bmID := uint64(created["id"].(float64))
	var dbRow models.TopologyLayout
	_ = db.First(&dbRow, bmID).Error
	if dbRow.Scope != models.ScopePrivate || dbRow.OwnerID == nil || *dbRow.OwnerID != user.ID || dbRow.Revision != 1 {
		t.Fatalf("批量赋值防护失效: scope=%s, owner=%v, rev=%d", dbRow.Scope, dbRow.OwnerID, dbRow.Revision)
	}

	// 3. 非法入参：view="UNKNOWN"、非法 focus_scope、坐标 NaN/Inf、> 5000 条目、zoom 越界均返 400
	over5000 := make(map[string]any, 5001)
	for i := 0; i < 5001; i++ {
		over5000[fmt.Sprintf("mod_%d", i)] = map[string]any{"x": 1.0, "y": 2.0}
	}
	over5000Bytes, _ := json.Marshal(map[string]any{
		"name":           "over5000",
		"view":           "LOGICAL",
		"node_positions": over5000,
	})

	badPayloads := []string{
		`{"name":"bad_view","view":"UNKNOWN","node_positions":{}}`,
		`{"name":"bad_focus","view":"LOGICAL","focus_scope":"evil_../etc","node_positions":{}}`,
		`{"name":"bad_nan","view":"LOGICAL","node_positions":{"sub_1":{"x":NaN,"y":0}}}`,
		`{"name":"bad_zoom_low","view":"LOGICAL","node_positions":{},"viewport":{"x":0,"y":0,"zoom":0.05}}`,
		`{"name":"bad_zoom_high","view":"LOGICAL","node_positions":{},"viewport":{"x":0,"y":0,"zoom":5.0}}`,
		`{"name":"bad_level","view":"LOGICAL","node_positions":{},"expand_state":{"level":99}}`,
		`{"name":"bad_status","view":"LOGICAL","node_positions":{},"filter_state":{"status":"INVALID"}}`,
		`{"name":"bad_hops","view":"LOGICAL","node_positions":{},"filter_state":{"ego_hops":5}}`,
		string(over5000Bytes),
	}

	for idx, raw := range badPayloads {
		reqBad := httptest.NewRequest(http.MethodPost, "/api/arch/topology/layouts/personal", strings.NewReader(raw))
		reqBad.Header.Set("Authorization", "Bearer "+token)
		reqBad.Header.Set("Content-Type", "application/json")
		wBad := httptest.NewRecorder()
		r.ServeHTTP(wBad, reqBad)
		if wBad.Code != http.StatusBadRequest {
			t.Fatalf("非法入参用例 #%d 期望 400，实际: %d %s", idx, wBad.Code, wBad.Body.String())
		}
	}
}

func TestTopologyLayout_ListOmitsPayload_DetailOwnerOnly(t *testing.T) {
	r, db := setupRouterAndDB(t)
	admin := createTestUserInDB(t, db, "list_admin", config.AdminRole)
	user := createTestUserInDB(t, db, "list_user", "user")
	adminToken := generateTestJWT(t, admin)
	userToken := generateTestJWT(t, user)

	// 1. 管理员发布团队基准
	canonicalReq, _ := json.Marshal(map[string]any{
		"view":              "LOGICAL",
		"expected_revision": 0,
		"structure_version": "s1-LOGICAL-1234567890abcdef",
		"node_positions": map[string]any{
			"sub_1": map[string]any{"x": 100.0, "y": 200.0, "pinned": true},
		},
	})
	reqCanon := httptest.NewRequest(http.MethodPut, "/api/arch/topology/layouts/canonical", bytes.NewReader(canonicalReq))
	reqCanon.Header.Set("Authorization", "Bearer "+adminToken)
	reqCanon.Header.Set("Content-Type", "application/json")
	wCanon := httptest.NewRecorder()
	r.ServeHTTP(wCanon, reqCanon)
	if wCanon.Code != http.StatusOK {
		t.Fatalf("发布基准失败: %d %s", wCanon.Code, wCanon.Body.String())
	}

	// 2. 用户创建个人书签
	persReq, _ := json.Marshal(map[string]any{
		"name":              "详情测试书签",
		"view":              "LOGICAL",
		"focus_scope":       "sub_1",
		"is_default":        true,
		"structure_version": "s1-LOGICAL-1234567890abcdef",
		"node_positions": map[string]any{
			"mod_10": map[string]any{"x": 33.0, "y": 44.0, "pinned": true},
		},
		"expand_state": map[string]any{"level": 1},
		"filter_state": map[string]any{"only_my_scope": true},
		"viewport":     map[string]any{"x": 1, "y": 2, "zoom": 1.0},
	})
	reqPers := httptest.NewRequest(http.MethodPost, "/api/arch/topology/layouts/personal", bytes.NewReader(persReq))
	reqPers.Header.Set("Authorization", "Bearer "+userToken)
	reqPers.Header.Set("Content-Type", "application/json")
	wPers := httptest.NewRecorder()
	r.ServeHTTP(wPers, reqPers)
	if wPers.Code != http.StatusCreated {
		t.Fatalf("创建个人书签失败: %d %s", wPers.Code, wPers.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(wPers.Body.Bytes(), &created)
	bmID := uint64(created["id"].(float64))

	// 3. GET /api/arch/topology/layouts?view=LOGICAL:
	//    personal_views 必须为摘要（不含 node_positions / expand_state / filter_state / viewport），
	//    canonical_layout 必须为完整详情（含 node_positions）
	reqList := httptest.NewRequest(http.MethodGet, "/api/arch/topology/layouts?view=LOGICAL", nil)
	reqList.Header.Set("Authorization", "Bearer "+userToken)
	wList := httptest.NewRecorder()
	r.ServeHTTP(wList, reqList)
	if wList.Code != http.StatusOK {
		t.Fatalf("GET /layouts 失败: %d %s", wList.Code, wList.Body.String())
	}
	var listResp struct {
		CanonicalLayout map[string]any   `json:"canonical_layout"`
		PersonalViews   []map[string]any `json:"personal_views"`
	}
	if err := json.Unmarshal(wList.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("解析列表响应失败: %v", err)
	}
	if listResp.CanonicalLayout == nil || listResp.CanonicalLayout["node_positions"] == nil {
		t.Fatalf("canonical_layout 应返回完整详情含 node_positions，实际: %+v", listResp.CanonicalLayout)
	}
	if len(listResp.PersonalViews) != 1 {
		t.Fatalf("期望 1 条个人书签摘要，实际: %d", len(listResp.PersonalViews))
	}
	pvSummary := listResp.PersonalViews[0]
	for _, forbiddenField := range []string{"node_positions", "expand_state", "filter_state", "viewport"} {
		if _, exists := pvSummary[forbiddenField]; exists {
			t.Fatalf("personal_views 摘要严禁包含大字段 %s，实际: %+v", forbiddenField, pvSummary)
		}
	}

	// 4. GET /api/arch/topology/layouts/personal/:id 本人返回完整详情
	reqDetail := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/arch/topology/layouts/personal/%d", bmID), nil)
	reqDetail.Header.Set("Authorization", "Bearer "+userToken)
	wDetail := httptest.NewRecorder()
	r.ServeHTTP(wDetail, reqDetail)
	if wDetail.Code != http.StatusOK {
		t.Fatalf("本人获取书签详情期望 200，实际: %d %s", wDetail.Code, wDetail.Body.String())
	}
	var detail map[string]any
	_ = json.Unmarshal(wDetail.Body.Bytes(), &detail)
	if detail["node_positions"] == nil || detail["filter_state"] == nil || detail["viewport"] == nil {
		t.Fatalf("书签详情应包含完整 JSON 载荷，实际: %+v", detail)
	}
}
