package handlers

import (
	"net/http"
	"time"

	"code-traefik/config"

	"github.com/gin-gonic/gin"
)

// AppVersion 由启动入口注入构建版本号
var AppVersion = "dev"

// HealthCheck 提供免密健康探针接口 GET /api/health
func HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":     "healthy",
		"service":    config.ServiceName,
		"module_key": config.ModuleKey,
		"version":    AppVersion,
		"timestamp":  time.Now().Format(time.RFC3339),
	})
}
