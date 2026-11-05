package handlers

import (
	"net/http"
	"strconv"

	"code-traefik/internal/server/dashboard"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
)

// GetDashboardOverview GET /api/dashboard/overview
// 契约治理全景大盘：聚合健康评分、架构漂移雷达、契约资产认领看板与防劣化回归防线 (F-24)
func GetDashboardOverview(c *gin.Context) {
	var subsystemID *uint64
	if subStr := c.Query("subsystem_id"); subStr != "" {
		if val, err := strconv.ParseUint(subStr, 10, 64); err == nil && val > 0 {
			subsystemID = &val
		}
	}

	overview, err := dashboard.GetOverview(c.Request.Context(), models.DB, subsystemID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取治理大盘聚合数据失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, overview)
}
