package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonAuth "code-common/backend/auth"
	"code-common/backend/testdb"
	"code-traefik/config"
	"code-traefik/handlers"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
)

func setupTestRouter(withDB bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	config.AppConfig.Auth.JWTSecret = ""
	if err := config.LoadConfig("non_existent_for_test.yaml"); err != nil {
		// 若存在 config.yaml.example 则已正常加载，否则使用默认随机密钥
	}

	r := gin.New()
	openAPI := r.Group("/api")
	{
		openAPI.GET("/health", handlers.HealthCheck)
		openAPI.GET("/auth/config", handlers.GetAuthConfig)
	}

	authCfg := commonAuth.AuthConfig{
		JWTSecretGetter: func() string { return config.AppConfig.Auth.JWTSecret },
	}
	if withDB {
		authCfg.DB = models.DB
	}

	protected := r.Group("/api")
	protected.Use(commonAuth.AuthMiddleware(authCfg))
	{
		protected.GET("/me", handlers.GetMe)
		protected.GET("/items/stats", handlers.GetItemStats)
		protected.GET("/items", handlers.ListItems)
		protected.POST("/items", handlers.CreateItem)
		protected.PUT("/items/:id", handlers.UpdateItem)
		protected.DELETE("/items/:id", handlers.DeleteItem)

		admin := protected.Group("/admin")
		admin.Use(commonAuth.RequireAdmin(config.AdminRole))
		{
			admin.GET("/settings", handlers.GetModuleSettings)
			admin.PUT("/settings", handlers.UpdateModuleSettings)
		}
	}
	return r
}

func TestModelTablePrefixAndValidators(t *testing.T) {
	itemTable := models.TemplateItem{}.TableName()
	expectedItemTable := config.TablePrefix + "items"
	if itemTable != expectedItemTable {
		t.Fatalf("期望业务表名为 %q，实际得到 %q", expectedItemTable, itemTable)
	}

	settingTable := models.TemplateSetting{}.TableName()
	expectedSettingTable := config.TablePrefix + "settings"
	if settingTable != expectedSettingTable {
		t.Fatalf("期望配置表名为 %q，实际得到 %q", expectedSettingTable, settingTable)
	}

	if !models.IsValidItemStatus(models.ItemStatusActive) || models.IsValidItemStatus("invalid_status") {
		t.Fatal("条目状态白名单校验逻辑异常")
	}

	if !models.IsValidItemPriority(models.ItemPriorityHigh) || models.IsValidItemPriority("urgent_unknown") {
		t.Fatal("条目优先级白名单校验逻辑异常")
	}
}

func TestHealthAndAuthRBAC(t *testing.T) {
	router := setupTestRouter(false)

	// 1. 免密健康探针应返回 200
	reqHealth := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	recHealth := httptest.NewRecorder()
	router.ServeHTTP(recHealth, reqHealth)
	if recHealth.Code != http.StatusOK {
		t.Fatalf("期望 /api/health 返回 200，实际返回 %d", recHealth.Code)
	}

	// 2. 未携带 Token 访问受保护路由应返回 401
	reqUnauth := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	recUnauth := httptest.NewRecorder()
	router.ServeHTTP(recUnauth, reqUnauth)
	if recUnauth.Code != http.StatusUnauthorized {
		t.Fatalf("期望未认证请求返回 401，实际返回 %d", recUnauth.Code)
	}

	// 3. 普通用户可访问 /api/me，但访问 /api/admin/settings 应被 403 拦截
	normalToken, err := commonAuth.GenerateToken(
		101,
		"dev_user",
		"dev@example.com",
		"研发工程师",
		false,
		[]string{"member"},
		config.AppConfig.Auth.JWTSecret,
		time.Hour,
	)
	if err != nil {
		t.Fatalf("签发测试普通用户 Token 失败: %v", err)
	}

	reqMe := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	reqMe.Header.Set("Authorization", "Bearer "+normalToken)
	recMe := httptest.NewRecorder()
	router.ServeHTTP(recMe, reqMe)
	if recMe.Code != http.StatusOK {
		t.Fatalf("期望普通用户访问 /api/me 返回 200，实际返回 %d", recMe.Code)
	}

	var meResp map[string]any
	if err := json.Unmarshal(recMe.Body.Bytes(), &meResp); err != nil {
		t.Fatalf("解析 /api/me 响应失败: %v", err)
	}
	if isModAdmin, _ := meResp["is_module_admin"].(bool); isModAdmin {
		t.Fatal("普通用户不应具备模块管理员标识")
	}

	reqForbidden := httptest.NewRequest(http.MethodGet, "/api/admin/settings", nil)
	reqForbidden.Header.Set("Authorization", "Bearer "+normalToken)
	recForbidden := httptest.NewRecorder()
	router.ServeHTTP(recForbidden, reqForbidden)
	if recForbidden.Code != http.StatusForbidden {
		t.Fatalf("期望普通用户访问管理员接口返回 403，实际返回 %d", recForbidden.Code)
	}
}

func TestItemCRUDWithIsolatedDB(t *testing.T) {
	isolatedDB := testdb.SetupIsolatedDB(t, config.ModuleKey)
	if err := models.SetupWithDB(isolatedDB); err != nil {
		t.Fatalf("初始化隔离测试数据库失败: %v", err)
	}

	var adminUser models.User
	if err := models.DB.First(&adminUser).Error; err != nil {
		t.Fatalf("读取种子管理员失败: %v", err)
	}

	router := setupTestRouter(true)
	token, err := commonAuth.GenerateToken(
		adminUser.ID,
		adminUser.Username,
		adminUser.Email,
		adminUser.Name,
		true,
		adminUser.GetRoles(),
		config.AppConfig.Auth.JWTSecret,
		time.Hour,
	)
	if err != nil {
		t.Fatalf("生成管理员测试 Token 失败: %v", err)
	}

	// 新建条目
	createBody, err := json.Marshal(map[string]any{
		"code":        "TPL-TEST-99",
		"name":        "隔离单测验证条目",
		"category":    "quality",
		"status":      "active",
		"priority":    "high",
		"description": "验证创建、分页查询与删除全流程",
	})
	if err != nil {
		t.Fatalf("序列化请求体失败: %v", err)
	}

	reqCreate := httptest.NewRequest(http.MethodPost, "/api/items", bytes.NewReader(createBody))
	reqCreate.Header.Set("Authorization", "Bearer "+token)
	reqCreate.Header.Set("Content-Type", "application/json")
	recCreate := httptest.NewRecorder()
	router.ServeHTTP(recCreate, reqCreate)
	if recCreate.Code != http.StatusCreated {
		t.Fatalf("创建条目失败: status=%d, body=%s", recCreate.Code, recCreate.Body.String())
	}

	// 分页检索
	reqList := httptest.NewRequest(http.MethodGet, "/api/items?page=1&pageSize=25&search=TPL-TEST-99", nil)
	reqList.Header.Set("Authorization", "Bearer "+token)
	recList := httptest.NewRecorder()
	router.ServeHTTP(recList, reqList)
	if recList.Code != http.StatusOK {
		t.Fatalf("分页查询失败: status=%d, body=%s", recList.Code, recList.Body.String())
	}
}
