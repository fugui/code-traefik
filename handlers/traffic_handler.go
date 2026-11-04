package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	commonAuth "code-common/backend/auth"
	"code-traefik/internal/server/callgraph"
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
	SortDir      string `form:"sort_dir"`
	IsReplayable *bool  `form:"is_replayable"`
	HasError     *bool  `form:"has_error"`
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
	if query.HasError != nil {
		if *query.HasError {
			tx = tx.Where("(status_code <> 0 OR exchange_type = ? OR (headers_json->'meta'->>'biz_error')::boolean = true)", models.ExchangeReqOnly)
		} else {
			tx = tx.Where("(status_code = 0 AND exchange_type <> ? AND ((headers_json->'meta'->>'biz_error') IS DISTINCT FROM 'true'))", models.ExchangeReqOnly)
		}
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

	sortDir := strings.ToLower(strings.TrimSpace(query.SortDir))
	if sortDir != "desc" {
		sortDir = "asc"
	}
	orderClause := "req_captured_at_ns ASC, seq_no ASC, id ASC"
	if sortDir == "desc" {
		orderClause = "req_captured_at_ns DESC, seq_no DESC, id DESC"
	}

	items := make([]models.TrafficRecord, 0)
	if err := tx.Order(orderClause).Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询流量列表失败: " + err.Error()})
		return
	}

	var session *models.RecordingSession
	if query.SessionID > 0 {
		var s models.RecordingSession
		if err := db.WithContext(c.Request.Context()).Where("id = ?", query.SessionID).First(&s).Error; err == nil {
			session = &s
		}
	}
	hydratedItems := callgraph.HydrateRecords(db, session, items)

	c.JSON(http.StatusOK, gin.H{
		"items":     hydratedItems,
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

// TransactionListQuery 事务调用树查询参数
type TransactionListQuery struct {
	ProcessID    uint64 `form:"process_id"`
	SubsystemID  uint64 `form:"subsystem_id"`
	ModuleID     uint64 `form:"module_id"`
	Direction    string `form:"direction"`
	ExchangeType string `form:"exchange_type"`
	ServiceName  string `form:"service_name"`
	Operation    string `form:"operation"`
	SNO          uint64 `form:"sno"`
	TraceID      string `form:"trace_id"`
	EntityKey    string `form:"entity_key"`
	HasError     *bool  `form:"has_error"`
	IsReplayable *bool  `form:"is_replayable"`
	Keyword      string `form:"keyword"`
	Page         int    `form:"page"`
	PageSize     int    `form:"page_size"`
}

// GetRecordingSessionTransactions 获取录制批次的事务调用树列表 (GET /api/recording-sessions/:id/transactions)
func GetRecordingSessionTransactions(c *gin.Context) {
	db := models.DB
	if db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "数据库未就绪"})
		return
	}

	sessionID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || sessionID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的录制批次 ID"})
		return
	}

	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	userID := uint(0)
	if uc != nil {
		userID = uc.UserID
	}

	// 1. ScopeReadGuard
	var session models.RecordingSession
	if err := db.WithContext(c.Request.Context()).Where("id = ?", sessionID).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "录制批次不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询录制批次失败: " + err.Error()})
		return
	}
	if session.Scope == models.ScopePrivate && session.OwnerID != userID && !isAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权访问该私有录制批次的调用树"})
		return
	}

	var query TransactionListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的查询参数: " + err.Error()})
		return
	}

	// 2. 加载该 session 的全量轻量记录
	var records []models.TrafficRecord
	if err := db.WithContext(c.Request.Context()).Model(&models.TrafficRecord{}).
		Omit("raw_req_packet", "raw_rsp_packet").
		Where("session_id = ?", sessionID).
		Order("req_captured_at_ns ASC, seq_no ASC, id ASC").
		Find(&records).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询流量记录失败: " + err.Error()})
		return
	}

	// 3. 构建调用树
	filter := &callgraph.TreeFilterOptions{
		ProcessID:    query.ProcessID,
		SubsystemID:  query.SubsystemID,
		ModuleID:     query.ModuleID,
		Direction:    query.Direction,
		ExchangeType: query.ExchangeType,
		ServiceName:  query.ServiceName,
		Operation:    query.Operation,
		SNO:          query.SNO,
		TraceID:      query.TraceID,
		EntityKey:    query.EntityKey,
		HasError:     query.HasError,
		IsReplayable: query.IsReplayable,
		Keyword:      query.Keyword,
	}

	trees := callgraph.BuildTransactionTrees(records, &session, filter)
	entityChains := callgraph.BuildEntityChains(records)
	if entityChains == nil {
		entityChains = []callgraph.EntityLifecycleChainDTO{}
	}

	// 4. 按根事务分页 (15/25/50/100, 默认 25)
	page := query.Page
	if page <= 0 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize != 15 && pageSize != 25 && pageSize != 50 && pageSize != 100 {
		pageSize = 25
	}

	total := len(trees)
	start := (page - 1) * pageSize
	end := start + pageSize
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}

	pagedItems := make([]callgraph.TransactionTreeGroupDTO, 0)
	if start < end {
		pagedItems = trees[start:end]
	}

	c.JSON(http.StatusOK, gin.H{
		"items":         pagedItems,
		"total":         total,
		"page":          page,
		"page_size":     pageSize,
		"entity_chains": entityChains,
	})
}

// GetRecordingSessionTransactionSequence 获取单事务的时序泳道响应 (GET /api/recording-sessions/:id/transactions/:rootRecordId/sequence)
func GetRecordingSessionTransactionSequence(c *gin.Context) {
	db := models.DB
	if db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "数据库未就绪"})
		return
	}

	sessionID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || sessionID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的录制批次 ID"})
		return
	}
	rootRecordID, err := strconv.ParseUint(c.Param("rootRecordId"), 10, 64)
	if err != nil || rootRecordID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的根记录 ID"})
		return
	}

	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	userID := uint(0)
	if uc != nil {
		userID = uc.UserID
	}

	// 1. Session ScopeReadGuard
	var session models.RecordingSession
	if err := db.WithContext(c.Request.Context()).Where("id = ?", sessionID).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "录制批次不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询录制批次失败: " + err.Error()})
		return
	}
	if session.Scope == models.ScopePrivate && session.OwnerID != userID && !isAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权访问该私有录制批次的时序泳道"})
		return
	}

	// 2. 检查可选的 replay_task_id 权限
	var replayTaskID uint64
	if rtStr := c.Query("replay_task_id"); rtStr != "" {
		parsedTaskID, err := strconv.ParseUint(rtStr, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的重放任务 ID"})
			return
		}
		var task models.ReplayTask
		if err := db.WithContext(c.Request.Context()).Where("id = ?", parsedTaskID).First(&task).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "重放任务不存在"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查询重放任务失败: " + err.Error()})
			return
		}
		if task.OwnerID != userID && !isAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权查看该私有重放任务的泳道结果"})
			return
		}
		replayTaskID = parsedTaskID
	}

	dto, err := callgraph.BuildTransactionSequence(db.WithContext(c.Request.Context()), sessionID, rootRecordID, replayTaskID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, dto)
}

// GetTrafficRecordReplayClosure 事务闭包重放预览 (GET /api/traffic-records/:id/replay-closure)
func GetTrafficRecordReplayClosure(c *gin.Context) {
	db := models.DB
	if db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "数据库未就绪"})
		return
	}

	recordID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || recordID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的流量记录 ID"})
		return
	}

	var rec models.TrafficRecord
	if err := db.WithContext(c.Request.Context()).Select("id", "session_id", "direction").Where("id = ?", recordID).First(&rec).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "流量记录不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询流量记录失败: " + err.Error()})
		return
	}

	if rec.Direction != "INGRESS" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅 INGRESS 入口记录支持重放闭包预览"})
		return
	}

	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	userID := uint(0)
	if uc != nil {
		userID = uc.UserID
	}

	// 校验 Session Scope
	var session models.RecordingSession
	if err := db.WithContext(c.Request.Context()).Where("id = ?", rec.SessionID).First(&session).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询录制批次失败: " + err.Error()})
		return
	}
	if session.Scope == models.ScopePrivate && session.OwnerID != userID && !isAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权访问该私有录制批次的重放闭包"})
		return
	}

	dto, err := callgraph.BuildReplayClosurePreview(db.WithContext(c.Request.Context()), rec.SessionID, rec.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, dto)
}
