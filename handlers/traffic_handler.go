package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	commonAuth "code-common/backend/auth"
	"code-traefik/internal/server/recorder"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// TrafficRecordListQuery 流量检索查询参数
type TrafficRecordListQuery struct {
	SessionID    uint64 `form:"session_id"`
	Scope        string `form:"scope"`
	SubsystemID  uint64 `form:"subsystem_id"`
	ModuleID     uint64 `form:"module_id"`
	ProcessID    uint64 `form:"process_id"`
	Direction    string `form:"direction"`
	ExchangeType string `form:"exchange_type"`
	ServiceName  string `form:"service_name"`
	Operation    string `form:"operation"`
	SNO          uint64 `form:"sno"`
	TraceID      string `form:"trace_id"`
	Keyword      string `form:"keyword"`
	IsReplayable *bool  `form:"is_replayable"`
	Page         int    `form:"page"`
	PageSize     int    `form:"page_size"`
}

// ListTrafficRecords 标准分页检索往返流量记录 (GET /api/traffic-records)
func ListTrafficRecords(c *gin.Context) {
	db := models.DB
	if db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "数据库未就绪"})
		return
	}

	var query TrafficRecordListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的查询参数: " + err.Error()})
		return
	}

	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	userID := uint(0)
	if uc != nil {
		userID = uc.UserID
	}

	// 1. 公私域权限守卫检查
	if query.SessionID > 0 {
		var session models.RecordingSession
		if err := db.WithContext(c.Request.Context()).Select("id", "scope", "owner_id").Where("id = ?", query.SessionID).First(&session).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "录制批次不存在"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查询录制批次失败: " + err.Error()})
			return
		}
		if session.Scope == models.ScopePrivate && session.OwnerID != userID && !isAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权访问该私有录制批次的流量记录"})
			return
		}
	}

	tx := db.WithContext(c.Request.Context()).Model(&models.TrafficRecord{}).Omit("raw_req_packet", "raw_rsp_packet")

	if query.SessionID > 0 {
		tx = tx.Where("session_id = ?", query.SessionID)
	} else if !isAdmin {
		// 未指定 session_id 时，非管理员只能查看公开或本人的批次流量
		tx = tx.Where("session_id IN (?)",
			db.Model(&models.RecordingSession{}).Select("id").Where("scope = ? OR owner_id = ?", models.ScopeShared, userID),
		)
	}

	if query.SubsystemID > 0 {
		tx = tx.Where("subsystem_id = ?", query.SubsystemID)
	}
	if query.ModuleID > 0 {
		tx = tx.Where("(src_module_id = ? OR dst_module_id = ?)", query.ModuleID, query.ModuleID)
	}
	if query.ProcessID > 0 {
		tx = tx.Where("process_id = ?", query.ProcessID)
	}
	if query.Direction != "" {
		tx = tx.Where("direction = ?", query.Direction)
	}
	if query.ExchangeType != "" {
		tx = tx.Where("exchange_type = ?", query.ExchangeType)
	}
	if query.ServiceName != "" {
		tx = tx.Where("service_name = ?", query.ServiceName)
	}
	if query.Operation != "" {
		tx = tx.Where("operation = ?", query.Operation)
	}
	if query.SNO > 0 {
		tx = tx.Where("sno = ?", query.SNO)
	}
	if query.TraceID != "" {
		tx = tx.Where("trace_id = ?", query.TraceID)
	}
	if query.IsReplayable != nil {
		tx = tx.Where("is_replayable = ?", *query.IsReplayable)
	}
	if query.Keyword != "" {
		kw := "%" + query.Keyword + "%"
		tx = tx.Where("(service_name ILIKE ? OR operation ILIKE ? OR trace_id ILIKE ?)", kw, kw, kw)
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "统计流量总数失败: " + err.Error()})
		return
	}

	page := query.Page
	if page <= 0 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = 25
	}

	var items []models.TrafficRecord
	if err := tx.Order("seq_no ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询流量列表失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetTrafficRecordAnatomy 查询单条往返记录的四段解剖 DTO (GET /api/traffic-records/:id/anatomy)
func GetTrafficRecordAnatomy(c *gin.Context) {
	db := models.DB
	if db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "数据库未就绪"})
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的记录 ID"})
		return
	}

	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	userID := uint(0)
	if uc != nil {
		userID = uc.UserID
	}

	var record models.TrafficRecord
	q := db.WithContext(c.Request.Context()).Where("id = ?", id)
	if sIDStr := c.Query("session_id"); sIDStr != "" {
		if sID, sErr := strconv.ParseUint(sIDStr, 10, 64); sErr == nil && sID > 0 {
			q = q.Where("session_id = ?", sID)
		}
	}

	if err := q.First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "流量记录不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询流量记录失败: " + err.Error()})
		return
	}

	// 权限检查
	var session models.RecordingSession
	if err := db.WithContext(c.Request.Context()).Select("id", "scope", "owner_id").Where("id = ?", record.SessionID).First(&session).Error; err == nil {
		if session.Scope == models.ScopePrivate && session.OwnerID != userID && !isAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权查看该私有录制批次的报文解剖"})
			return
		}
	}

	dec := GlobalDecoderRegistry
	dto, err := recorder.BuildPacketAnatomy(&record, dec)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "组装四段报文解剖失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, dto)
}

// RedecodeTrafficRecord 按需重新解码单条往返记录 (POST /api/traffic-records/:id/redecode)
func RedecodeTrafficRecord(c *gin.Context) {
	db := models.DB
	if db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "数据库未就绪"})
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的记录 ID"})
		return
	}

	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	userID := uint(0)
	if uc != nil {
		userID = uc.UserID
	}

	// 校验批次权限
	var record models.TrafficRecord
	if err := db.WithContext(c.Request.Context()).Select("id", "session_id").Where("id = ?", id).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "流量记录不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询流量记录失败: " + err.Error()})
		return
	}

	var session models.RecordingSession
	if err := db.WithContext(c.Request.Context()).Select("id", "scope", "owner_id").Where("id = ?", record.SessionID).First(&session).Error; err == nil {
		if session.Scope == models.ScopePrivate && session.OwnerID != userID && !isAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权操作该私有录制批次"})
			return
		}
	}

	dec := GlobalDecoderRegistry
	dto, err := recorder.RedecodeTrafficRecord(c.Request.Context(), db, dec, id)
	if err != nil {
		if strings.Contains(err.Error(), "无匹配") || strings.Contains(err.Error(), "not found") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "按需重解码失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, dto)
}
