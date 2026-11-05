package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	commonAuth "code-common/backend/auth"
	commonModels "code-common/backend/models"
	"code-traefik/internal/server/callgraph"
	"code-traefik/models"
	"code-traefik/pkg/agentproto"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// GoldenSuiteDTO 带有最近一次回归状态摘要的套件传输对象
type GoldenSuiteDTO struct {
	models.GoldenSuite
	LastReplayStatus *string    `json:"last_replay_status,omitempty"`
	LastReplayAt     *time.Time `json:"last_replay_at,omitempty"`
}

// ListGoldenSuites GET /api/golden-suites
func ListGoldenSuites(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "25"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 25
	}

	db := models.DB.WithContext(c.Request.Context()).Model(&models.GoldenSuite{})

	if archPIDStr := c.Query("arch_process_id"); archPIDStr != "" {
		if archPID, err := strconv.ParseUint(archPIDStr, 10, 64); err == nil && archPID > 0 {
			db = db.Where("arch_process_id = ?", archPID)
		}
	}

	if status := c.Query("status"); status != "" {
		db = db.Where("status = ?", status)
	}

	if search := strings.TrimSpace(c.Query("search")); search != "" {
		pattern := "%" + search + "%"
		db = db.Where("code ILIKE ? OR name ILIKE ? OR description ILIKE ?", pattern, pattern, pattern)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "统计黄金套件数量失败: " + err.Error()})
		return
	}

	var suites []models.GoldenSuite
	if err := db.Preload("ArchProcess").
		Order("id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&suites).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询黄金套件列表失败: " + err.Error()})
		return
	}

	// 批量加载最近一次重放任务状态
	dtos := make([]GoldenSuiteDTO, 0, len(suites))
	if len(suites) > 0 {
		suiteIDs := make([]uint64, len(suites))
		for i, s := range suites {
			suiteIDs[i] = s.ID
		}

		type TaskSummary struct {
			GoldenSuiteID uint64
			ResultStatus  string
			CreatedAt     time.Time
		}
		var recentTasks []TaskSummary
		models.DB.WithContext(c.Request.Context()).
			Raw(`SELECT DISTINCT ON (golden_suite_id) golden_suite_id, result_status, created_at 
			     FROM traefik_replay_tasks 
			     WHERE golden_suite_id IN ? 
			     ORDER BY golden_suite_id, id DESC`, suiteIDs).
			Scan(&recentTasks)

		taskMap := make(map[uint64]TaskSummary)
		for _, t := range recentTasks {
			taskMap[t.GoldenSuiteID] = t
		}

		for _, s := range suites {
			dto := GoldenSuiteDTO{GoldenSuite: s}
			if t, ok := taskMap[s.ID]; ok {
				status := t.ResultStatus
				createdAt := t.CreatedAt
				dto.LastReplayStatus = &status
				dto.LastReplayAt = &createdAt
			}
			dtos = append(dtos, dto)
		}
	}

	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	c.JSON(http.StatusOK, gin.H{
		"items":       dtos,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": totalPages,
	})
}

// GetGoldenSuite GET /api/golden-suites/:id
func GetGoldenSuite(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的套件 ID"})
		return
	}

	var suite models.GoldenSuite
	if err := models.DB.WithContext(c.Request.Context()).Preload("ArchProcess").First(&suite, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "黄金契约套件不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询黄金套件失败: " + err.Error()})
		return
	}

	var items []models.GoldenSuiteItem
	if err := models.DB.WithContext(c.Request.Context()).
		Where("suite_id = ?", id).
		Order("order_no ASC").
		Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询套件用例明细失败: " + err.Error()})
		return
	}

	summaries := make([]models.GoldenSuiteItemSummary, 0, len(items))
	for _, it := range items {
		summaries = append(summaries, it.ToSummary())
	}

	c.JSON(http.StatusOK, gin.H{
		"suite": suite,
		"items": summaries,
	})
}

// CurateGoldenSuiteReq 沉淀用例请求体
type CurateGoldenSuiteReq struct {
	SuiteID       *uint64            `json:"suite_id"`
	NewSuite      *NewGoldenSuiteDTO `json:"new_suite"`
	SessionID     uint64             `json:"session_id" binding:"required"`
	RecordIDs     []uint64           `json:"record_ids" binding:"required"`
	MaskRulesJSON []models.MaskRule  `json:"mask_rules_json"`
}

// NewGoldenSuiteDTO 新建套件元数据
type NewGoldenSuiteDTO struct {
	Code             string   `json:"code" binding:"required"`
	Name             string   `json:"name" binding:"required"`
	ArchProcessID    uint64   `json:"arch_process_id" binding:"required"`
	Version          string   `json:"version"`
	ExtraIgnorePaths []string `json:"extra_ignore_paths"`
	Description      string   `json:"description"`
}

// CurateGoldenSuite POST /api/golden-suites/curate
func CurateGoldenSuite(c *gin.Context) {
	var req CurateGoldenSuiteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数校验失败: " + err.Error()})
		return
	}

	if len(req.RecordIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "record_ids 不能为空"})
		return
	}

	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	userID := uint(0)
	if uc != nil {
		userID = uc.UserID
	}

	// 1. 校验源 RecordingSession 与 Anti-IDOR
	var session models.RecordingSession
	if err := models.DB.WithContext(c.Request.Context()).First(&session, req.SessionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "录制批次不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询录制批次失败: " + err.Error()})
		return
	}
	if session.Scope == models.ScopePrivate && session.OwnerID != userID && !isAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权访问他人私有录制批次 (Anti-IDOR)"})
		return
	}

	// 2. 加载选定流量记录并校验
	var records []models.TrafficRecord
	if err := models.DB.WithContext(c.Request.Context()).
		Where("id IN ? AND session_id = ?", req.RecordIDs, req.SessionID).
		Find(&records).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询选定流量记录失败: " + err.Error()})
		return
	}
	if len(records) != len(req.RecordIDs) {
		c.JSON(http.StatusNotFound, gin.H{"error": "部分选定记录不存在或不属于该录制批次"})
		return
	}

	// 提取全部参与的 ProcessID 并校验同属单一标准架构进程
	procIDMap := make(map[uint64]bool)
	for _, r := range records {
		if r.Direction != models.TrafficDirIngress {
			c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持将 INGRESS 入站事务沉淀为黄金契约用例"})
			return
		}
		if !r.IsReplayable || len(r.RawReqPacket) == 0 || r.IsTruncated {
			c.JSON(http.StatusBadRequest, gin.H{"error": "选定记录包含不可重放或已被截断的报文"})
			return
		}
		procIDMap[r.ProcessID] = true
	}

	procIDs := make([]uint64, 0, len(procIDMap))
	for pid := range procIDMap {
		procIDs = append(procIDs, pid)
	}

	var procs []models.Process
	if err := models.DB.WithContext(c.Request.Context()).Where("id IN ?", procIDs).Find(&procs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询进程信息失败: " + err.Error()})
		return
	}

	var targetArchProcID uint64
	for i, p := range procs {
		if i == 0 {
			targetArchProcID = p.ArchProcessID
		} else if p.ArchProcessID != targetArchProcID {
			c.JSON(http.StatusBadRequest, gin.H{"error": "选定流量记录属于不同的架构进程，单套件仅允许同属单一标准架构进程"})
			return
		}
	}

	// 3. 确定目标 GoldenSuite (写入既有套件或新建套件)
	var suite models.GoldenSuite
	if req.SuiteID != nil && *req.SuiteID > 0 {
		if err := models.DB.WithContext(c.Request.Context()).First(&suite, *req.SuiteID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "目标黄金契约套件不存在"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查询黄金套件失败: " + err.Error()})
			return
		}
		if targetArchProcID > 0 && suite.ArchProcessID != targetArchProcID {
			c.JSON(http.StatusBadRequest, gin.H{"error": "选定流量记录所属架构进程与目标套件架构进程不一致"})
			return
		}
	} else if req.NewSuite != nil {
		if req.NewSuite.ArchProcessID == 0 {
			req.NewSuite.ArchProcessID = targetArchProcID
		}
		if targetArchProcID > 0 && req.NewSuite.ArchProcessID != targetArchProcID {
			c.JSON(http.StatusBadRequest, gin.H{"error": "选定流量记录所属架构进程与新建套件声明架构进程不一致"})
			return
		}

		// 检查 Code 唯一性
		var existingCount int64
		models.DB.WithContext(c.Request.Context()).Model(&models.GoldenSuite{}).
			Where("code = ?", req.NewSuite.Code).Count(&existingCount)
		if existingCount > 0 {
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("套件编码 [%s] 已被占用", req.NewSuite.Code)})
			return
		}

		version := req.NewSuite.Version
		if version == "" {
			version = "v1.0.0"
		}
		ignoreBytes, _ := json.Marshal(req.NewSuite.ExtraIgnorePaths)
		if len(ignoreBytes) == 0 {
			ignoreBytes = []byte("[]")
		}

		suite = models.GoldenSuite{
			Code:             req.NewSuite.Code,
			Name:             req.NewSuite.Name,
			ArchProcessID:    req.NewSuite.ArchProcessID,
			Version:          version,
			Status:           models.GoldenSuiteStatusActive,
			ExtraIgnorePaths: datatypes.JSON(ignoreBytes),
			Description:      req.NewSuite.Description,
			CreatedBy:        userID,
			CreatedAt:        time.Now(),
			UpdatedAt:        time.Now(),
		}
		if err := models.DB.WithContext(c.Request.Context()).Create(&suite).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "创建黄金套件失败: " + err.Error()})
			return
		}
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "必须提供 suite_id 或 new_suite 参数"})
		return
	}

	// 4. 检索同批次属于该进程的 EGRESS 记录以提取 ScopedMocks 和历史下游端点
	var egressRecords []models.TrafficRecord
	_ = models.DB.WithContext(c.Request.Context()).
		Where("session_id = ? AND direction = 'EGRESS' AND is_truncated = false AND exchange_type != 'REQ_ONLY'", req.SessionID).
		Limit(500).Find(&egressRecords).Error

	// 5. 组装并批量创建 GoldenSuiteItem 快照
	var maxOrder int
	models.DB.WithContext(c.Request.Context()).Model(&models.GoldenSuiteItem{}).
		Where("suite_id = ?", suite.ID).
		Select("COALESCE(MAX(order_no), 0)").Scan(&maxOrder)

	maskBytes, _ := json.Marshal(req.MaskRulesJSON)
	if len(maskBytes) == 0 {
		maskBytes = []byte("[]")
	}

	sessID := req.SessionID
	var newItems []models.GoldenSuiteItem

	for _, rec := range records {
		maxOrder++
		recID := rec.ID

		// 匹配针对此请求的下游 EGRESS 流量闭包
		var matchedEgress []models.TrafficRecord
		for _, eg := range egressRecords {
			if rec.TraceID != "" && rec.TraceID != "0" && eg.TraceID == rec.TraceID {
				matchedEgress = append(matchedEgress, eg)
			} else if eg.ReqCapturedAtNs >= rec.ReqCapturedAtNs {
				rEnd := rec.ReqCapturedAtNs + 500_000_000
				if rec.RspCapturedAtNs != nil {
					rEnd = *rec.RspCapturedAtNs + 10_000_000
				}
				if eg.ReqCapturedAtNs <= rEnd {
					matchedEgress = append(matchedEgress, eg)
				}
			}
		}

		scopedMocks := callgraph.BuildScopedMockRules(matchedEgress)
		scopedMocksJSON, _ := json.Marshal(scopedMocks)

		var egressEndpoints []agentproto.DownstreamEndpoint
		seenEP := make(map[string]bool)
		for _, eg := range matchedEgress {
			if eg.DstAddr != "" {
				host, portStr, err := net.SplitHostPort(strings.TrimSpace(eg.DstAddr))
				if err == nil {
					if port, pErr := strconv.Atoi(portStr); pErr == nil && port > 0 && port <= 65535 {
						key := fmt.Sprintf("%s:%d", host, port)
						if !seenEP[key] {
							seenEP[key] = true
							egressEndpoints = append(egressEndpoints, agentproto.DownstreamEndpoint{
								IP:   host,
								Port: uint16(port),
							})
						}
					}
				}
			}
		}
		egressEndpointsJSON, _ := json.Marshal(egressEndpoints)

		item := models.GoldenSuiteItem{
			SuiteID:         suite.ID,
			OrderNo:         maxOrder,
			Name:            fmt.Sprintf("%s.%s#%d", rec.ServiceName, rec.Operation, rec.SeqNo),
			SourceSessionID: &sessID,
			SourceRecordID:  &recID,
			ServiceName:     rec.ServiceName,
			Operation:       rec.Operation,
			BaselineStatus:  rec.StatusCode,
			RspSegCount:     rec.RspSegCount,
			RawReqPacket:    rec.RawReqPacket,
			RawRspPacket:    rec.RawRspPacket,
			ScopedMocks:     datatypes.JSON(scopedMocksJSON),
			EgressEndpoints: datatypes.JSON(egressEndpointsJSON),
			MaskRulesJSON:   datatypes.JSON(maskBytes),
			CreatedBy:       userID,
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		}
		if item.RspSegCount <= 0 {
			item.RspSegCount = 1
		}
		newItems = append(newItems, item)
	}

	if err := models.DB.WithContext(c.Request.Context()).Create(&newItems).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "持久化自包含黄金用例失败: " + err.Error()})
		return
	}

	// 维护冗余用例数
	var totalItemCount int64
	models.DB.WithContext(c.Request.Context()).Model(&models.GoldenSuiteItem{}).
		Where("suite_id = ?", suite.ID).Count(&totalItemCount)
	models.DB.WithContext(c.Request.Context()).Model(&suite).Update("item_count", totalItemCount)
	suite.ItemCount = int(totalItemCount)

	recordAudit(c, "curate", commonModels.AuditLevelP2,
		fmt.Sprintf("沉淀黄金契约用例: suite=%s, count=%d", suite.Code, len(newItems)),
		"golden_suite", suite.ID, suite.Name, nil, suite)

	c.JSON(http.StatusCreated, gin.H{
		"suite":       suite,
		"added_items": len(newItems),
	})
}

// UpdateGoldenSuite PUT /api/golden-suites/:id (AdminOnly)
func UpdateGoldenSuite(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的套件 ID"})
		return
	}

	var suite models.GoldenSuite
	if err := models.DB.WithContext(c.Request.Context()).First(&suite, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "黄金契约套件不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询黄金套件失败: " + err.Error()})
		return
	}

	var req struct {
		Name             string   `json:"name" binding:"required"`
		Version          string   `json:"version"`
		Status           string   `json:"status" binding:"required"`
		ExtraIgnorePaths []string `json:"extra_ignore_paths"`
		Description      string   `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数校验失败: " + err.Error()})
		return
	}

	if req.Status != models.GoldenSuiteStatusActive && req.Status != models.GoldenSuiteStatusArchived {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status 必须为 ACTIVE 或 ARCHIVED"})
		return
	}

	ignoreBytes, _ := json.Marshal(req.ExtraIgnorePaths)
	if len(ignoreBytes) == 0 {
		ignoreBytes = []byte("[]")
	}

	before := suite
	suite.Name = req.Name
	if req.Version != "" {
		suite.Version = req.Version
	}
	suite.Status = req.Status
	suite.ExtraIgnorePaths = datatypes.JSON(ignoreBytes)
	suite.Description = req.Description
	suite.UpdatedAt = time.Now()

	uc := commonAuth.GetUserContext(c)
	if uc != nil {
		suite.UpdatedBy = uc.UserID
	}

	if err := models.DB.WithContext(c.Request.Context()).Save(&suite).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新黄金套件失败: " + err.Error()})
		return
	}

	recordAudit(c, "update", commonModels.AuditLevelP2,
		fmt.Sprintf("更新黄金契约套件 [%s]", suite.Code),
		"golden_suite", suite.ID, suite.Name, before, suite)

	c.JSON(http.StatusOK, suite)
}

// UpdateGoldenSuiteItem PUT /api/golden-suites/:id/items/:itemId (AdminOnly)
func UpdateGoldenSuiteItem(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的套件 ID"})
		return
	}
	itemId, err := strconv.ParseUint(c.Param("itemId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的用例 ID"})
		return
	}

	var item models.GoldenSuiteItem
	if err := models.DB.WithContext(c.Request.Context()).
		Where("suite_id = ? AND id = ?", id, itemId).First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "黄金用例不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询用例失败: " + err.Error()})
		return
	}

	var req struct {
		Name          string            `json:"name"`
		OrderNo       *int              `json:"order_no"`
		MaskRulesJSON []models.MaskRule `json:"mask_rules_json"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数校验失败: " + err.Error()})
		return
	}

	before := item
	if req.Name != "" {
		item.Name = req.Name
	}
	if req.OrderNo != nil && *req.OrderNo > 0 && *req.OrderNo != item.OrderNo {
		// 校验序号冲突
		var conflictCount int64
		models.DB.WithContext(c.Request.Context()).Model(&models.GoldenSuiteItem{}).
			Where("suite_id = ? AND order_no = ? AND id != ?", id, *req.OrderNo, itemId).
			Count(&conflictCount)
		if conflictCount > 0 {
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("执行序号 %d 已被占用", *req.OrderNo)})
			return
		}
		item.OrderNo = *req.OrderNo
	}
	if req.MaskRulesJSON != nil {
		maskBytes, _ := json.Marshal(req.MaskRulesJSON)
		item.MaskRulesJSON = datatypes.JSON(maskBytes)
	}
	item.UpdatedAt = time.Now()

	if err := models.DB.WithContext(c.Request.Context()).Save(&item).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新黄金用例失败: " + err.Error()})
		return
	}

	recordAudit(c, "update_item", commonModels.AuditLevelP2,
		fmt.Sprintf("更新黄金用例 [%s] (ID: %d)", item.Name, item.ID),
		"golden_suite_item", item.ID, item.Name, before, item)

	c.JSON(http.StatusOK, item.ToSummary())
}

// DeleteGoldenSuiteItem DELETE /api/golden-suites/:id/items/:itemId (AdminOnly)
func DeleteGoldenSuiteItem(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的套件 ID"})
		return
	}
	itemId, err := strconv.ParseUint(c.Param("itemId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的用例 ID"})
		return
	}

	var item models.GoldenSuiteItem
	if err := models.DB.WithContext(c.Request.Context()).
		Where("suite_id = ? AND id = ?", id, itemId).First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "黄金用例不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询用例失败: " + err.Error()})
		return
	}

	if err := models.DB.WithContext(c.Request.Context()).Delete(&item).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除黄金用例失败: " + err.Error()})
		return
	}

	// 维护冗余用例数
	var count int64
	models.DB.WithContext(c.Request.Context()).Model(&models.GoldenSuiteItem{}).
		Where("suite_id = ?", id).Count(&count)
	models.DB.WithContext(c.Request.Context()).Model(&models.GoldenSuite{}).
		Where("id = ?", id).Update("item_count", count)

	recordAudit(c, "delete_item", commonModels.AuditLevelP2,
		fmt.Sprintf("移除黄金用例 [%s] (ID: %d)", item.Name, item.ID),
		"golden_suite_item", item.ID, item.Name, item, nil)

	c.JSON(http.StatusOK, gin.H{"message": "黄金用例已移除"})
}

// DeleteGoldenSuite DELETE /api/golden-suites/:id (AdminOnly)
func DeleteGoldenSuite(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的套件 ID"})
		return
	}

	var suite models.GoldenSuite
	if err := models.DB.WithContext(c.Request.Context()).First(&suite, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "黄金契约套件不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询黄金套件失败: " + err.Error()})
		return
	}

	// 校验处于 ACTIVE 状态时是否有运行中的回归任务
	if suite.Status == models.GoldenSuiteStatusActive {
		var runningCount int64
		models.DB.WithContext(c.Request.Context()).Model(&models.ReplayTask{}).
			Where("golden_suite_id = ? AND lifecycle_state IN ('PENDING','ARMED','INJECTING')", id).
			Count(&runningCount)
		if runningCount > 0 {
			c.JSON(http.StatusConflict, gin.H{"error": "套件当前处于生效态且存在运行中的重放任务，禁止删除"})
			return
		}
	}

	if err := models.DB.WithContext(c.Request.Context()).Delete(&suite).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除黄金套件失败: " + err.Error()})
		return
	}

	recordAudit(c, "delete", commonModels.AuditLevelP2,
		fmt.Sprintf("删除黄金契约套件 [%s] (ID: %d)", suite.Code, suite.ID),
		"golden_suite", suite.ID, suite.Name, suite, nil)

	c.JSON(http.StatusOK, gin.H{"message": "黄金契约套件已删除"})
}

// ExecuteGoldenSuite POST /api/golden-suites/:id/execute
func ExecuteGoldenSuite(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的套件 ID"})
		return
	}

	sched := GetGlobalReplayScheduler()
	if sched == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "重放调度器未就绪"})
		return
	}

	var req struct {
		EnvAgentID uint64 `json:"env_agent_id" binding:"required"`
		TimeoutMs  int    `json:"timeout_ms"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数校验失败: " + err.Error()})
		return
	}

	uc := commonAuth.GetUserContext(c)
	isAdmin := isCurrentUserAdmin(uc)
	userID := uint(0)
	if uc != nil {
		userID = uc.UserID
	}

	task, err := sched.ExecuteGoldenSuite(
		c.Request.Context(),
		id,
		req.EnvAgentID,
		models.TriggerSourceGoldenSuite,
		userID,
		isAdmin,
		req.TimeoutMs,
	)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if strings.Contains(err.Error(), "403") {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		if strings.Contains(err.Error(), "409") || strings.Contains(err.Error(), "offline") || strings.Contains(err.Error(), "leased") {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if strings.Contains(err.Error(), "400") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	recordAudit(c, "execute", commonModels.AuditLevelP2,
		fmt.Sprintf("执行黄金套件回归: task_id=%d, agent_id=%d", task.ID, req.EnvAgentID),
		"golden_suite", id, task.Name, nil, task)

	c.JSON(http.StatusCreated, task)
}
