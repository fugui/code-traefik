package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	commonAuth "code-common/backend/auth"
	commonModels "code-common/backend/models"
	"code-traefik/internal/server/topology"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	maxLayoutPayloadBytes   = 1 << 20 // 1MB
	maxNodePositionEntries  = 5000
	maxExpandOverrideItems  = 5000
	maxNodeKeyLength        = 128
	maxSubsystemFilterItems = 100
	maxPersonalViewQuota    = 20
	minViewportZoom         = 0.15
	maxViewportZoom         = 2.0
)

var validFocusScopeRegex = regexp.MustCompile(`^(GLOBAL|(sub|grp|pgrp|proc)_[0-9]+|ext_container|undeployed_container)$`)

// TopologyLayoutSummary 布局摘要响应 DTO（列表接口使用，不含任何大 JSON 载荷）
type TopologyLayoutSummary struct {
	ID               uint64    `json:"id"`
	Scope            string    `json:"scope"`
	View             string    `json:"view"`
	Name             string    `json:"name"`
	Description      string    `json:"description"`
	FocusScope       string    `json:"focus_scope"`
	IsDefault        bool      `json:"is_default"`
	StructureVersion string    `json:"structure_version"`
	Revision         int       `json:"revision"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// TopologyLayoutDetail 布局完整详情响应 DTO
type TopologyLayoutDetail struct {
	TopologyLayoutSummary
	SchemaVersion int                                 `json:"schema_version"`
	NodePositions map[string]models.SavedNodePosition `json:"node_positions"`
	ExpandState   models.SavedExpandState             `json:"expand_state"`
	FilterState   models.SavedFilterState             `json:"filter_state"`
	Viewport      models.SavedViewport                `json:"viewport"`
	CreatedBy     uint                                `json:"created_by"`
	UpdatedBy     uint                                `json:"updated_by"`
	CreatedAt     time.Time                           `json:"created_at"`
}

// UpsertCanonicalLayoutReq 发布/更新团队基准布局请求 DTO（防批量赋值）
type UpsertCanonicalLayoutReq struct {
	View             string                              `json:"view"`
	ExpectedRevision int                                 `json:"expected_revision"`
	StructureVersion string                              `json:"structure_version"`
	NodePositions    map[string]models.SavedNodePosition `json:"node_positions"`
	ExpandState      *models.SavedExpandState            `json:"expand_state"`
	Viewport         *models.SavedViewport               `json:"viewport"`
	Name             string                              `json:"name"`
	Description      string                              `json:"description"`
}

// CreatePersonalLayoutReq 创建个人视图书签请求 DTO（防批量赋值）
type CreatePersonalLayoutReq struct {
	Name             string                              `json:"name"`
	View             string                              `json:"view"`
	FocusScope       string                              `json:"focus_scope"`
	IsDefault        bool                                `json:"is_default"`
	StructureVersion string                              `json:"structure_version"`
	NodePositions    map[string]models.SavedNodePosition `json:"node_positions"`
	ExpandState      *models.SavedExpandState            `json:"expand_state"`
	FilterState      *models.SavedFilterState            `json:"filter_state"`
	Viewport         *models.SavedViewport               `json:"viewport"`
	Description      string                              `json:"description"`
}

// UpdatePersonalLayoutReq 更新个人视图书签请求 DTO（view 不可修改，防批量赋值）
type UpdatePersonalLayoutReq struct {
	Name             *string                             `json:"name"`
	FocusScope       *string                             `json:"focus_scope"`
	IsDefault        *bool                               `json:"is_default"`
	StructureVersion *string                             `json:"structure_version"`
	NodePositions    map[string]models.SavedNodePosition `json:"node_positions"`
	ExpandState      *models.SavedExpandState            `json:"expand_state"`
	FilterState      *models.SavedFilterState            `json:"filter_state"`
	Viewport         *models.SavedViewport               `json:"viewport"`
	Description      *string                             `json:"description"`
}

// topologyLayoutAuditSummary 摘要式审计结构（严禁将大 JSON 坐标写入审计 Diff）
type topologyLayoutAuditSummary struct {
	ID               uint64 `json:"id"`
	Scope            string `json:"scope"`
	View             string `json:"view"`
	FocusScope       string `json:"focus_scope"`
	Name             string `json:"name"`
	PositionCount    int    `json:"position_count"`
	StructureVersion string `json:"structure_version"`
	Revision         int    `json:"revision"`
	IsDefault        bool   `json:"is_default"`
}

var (
	errLayoutRevisionConflict = errors.New("layout_revision_conflict")
	errPersonalQuotaExceeded  = errors.New("personal_view_quota_exceeded")
	errDuplicateLayoutName    = errors.New("duplicate_layout_name")
	errLayoutNotFound         = errors.New("layout_not_found")
	errLayoutForbidden        = errors.New("layout_forbidden")
)

func toLayoutSummary(row models.TopologyLayout) TopologyLayoutSummary {
	return TopologyLayoutSummary{
		ID:               row.ID,
		Scope:            row.Scope,
		View:             row.View,
		Name:             row.Name,
		Description:      row.Description,
		FocusScope:       row.FocusScope,
		IsDefault:        row.IsDefault,
		StructureVersion: row.StructureVersion,
		Revision:         row.Revision,
		UpdatedAt:        row.UpdatedAt,
	}
}

func toLayoutDetail(row models.TopologyLayout) TopologyLayoutDetail {
	positions := make(map[string]models.SavedNodePosition)
	if len(row.NodePositions) > 0 {
		_ = json.Unmarshal(row.NodePositions, &positions)
	}
	if positions == nil {
		positions = make(map[string]models.SavedNodePosition)
	}

	var expandState models.SavedExpandState
	if len(row.ExpandState) > 0 {
		_ = json.Unmarshal(row.ExpandState, &expandState)
	}

	var filterState models.SavedFilterState
	if len(row.FilterState) > 0 {
		_ = json.Unmarshal(row.FilterState, &filterState)
	}

	viewport := models.SavedViewport{X: 0, Y: 0, Zoom: 1.0}
	if len(row.Viewport) > 0 {
		var parsedVP models.SavedViewport
		if err := json.Unmarshal(row.Viewport, &parsedVP); err == nil && parsedVP.Zoom > 0 {
			viewport = parsedVP
		}
	}

	schemaVer := row.SchemaVersion
	if schemaVer <= 0 {
		schemaVer = 1
	}

	return TopologyLayoutDetail{
		TopologyLayoutSummary: toLayoutSummary(row),
		SchemaVersion:         schemaVer,
		NodePositions:         positions,
		ExpandState:           expandState,
		FilterState:           filterState,
		Viewport:              viewport,
		CreatedBy:             row.CreatedBy,
		UpdatedBy:             row.UpdatedBy,
		CreatedAt:             row.CreatedAt,
	}
}

func toAuditSummary(row models.TopologyLayout) topologyLayoutAuditSummary {
	posCount := 0
	if len(row.NodePositions) > 0 {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(row.NodePositions, &m); err == nil {
			posCount = len(m)
		}
	}
	return topologyLayoutAuditSummary{
		ID:               row.ID,
		Scope:            row.Scope,
		View:             row.View,
		FocusScope:       row.FocusScope,
		Name:             row.Name,
		PositionCount:    posCount,
		StructureVersion: row.StructureVersion,
		Revision:         row.Revision,
		IsDefault:        row.IsDefault,
	}
}

func readBoundedJSONBody(c *gin.Context, dst any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxLayoutPayloadBytes)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) || strings.Contains(err.Error(), "http: request body too large") {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{
				"error": "布局请求体超过 1MB 上限",
				"code":  "PAYLOAD_TOO_LARGE",
			})
			return false
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取请求体失败", "code": "INVALID_REQUEST"})
		return false
	}
	if len(raw) > maxLayoutPayloadBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"error": "布局请求体超过 1MB 上限",
			"code":  "PAYLOAD_TOO_LARGE",
		})
		return false
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "JSON 格式或数值不合法（禁止 NaN/Infinity）",
			"code":  "INVALID_JSON_PAYLOAD",
		})
		return false
	}
	return true
}

func isFiniteFloat(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func validateViewParam(view string) (string, error) {
	v := strings.ToUpper(strings.TrimSpace(view))
	if v != topology.TopologyViewLogical && v != topology.TopologyViewPhysical {
		return "", errors.New("view 参数非法，仅允许 LOGICAL 或 PHYSICAL")
	}
	return v, nil
}

func normalizeAndValidateFocusScope(focusScope string) (string, error) {
	fs := strings.TrimSpace(focusScope)
	if fs == "" {
		fs = "GLOBAL"
	}
	if !validFocusScopeRegex.MatchString(fs) {
		return "", errors.New("focus_scope 格式非法")
	}
	return fs, nil
}

func validateNodePositions(positions map[string]models.SavedNodePosition) error {
	if len(positions) > maxNodePositionEntries {
		return fmt.Errorf("node_positions 条目数不能超过 %d", maxNodePositionEntries)
	}
	for k, pos := range positions {
		trimmed := strings.TrimSpace(k)
		if trimmed == "" || len(k) > maxNodeKeyLength {
			return errors.New("node_positions 节点 ID 键不能为空且长度不能超过 128")
		}
		if !isFiniteFloat(pos.X) || !isFiniteFloat(pos.Y) {
			return errors.New("节点坐标 x/y 必须为有限实数")
		}
	}
	return nil
}

func validateExpandState(es *models.SavedExpandState) error {
	if es == nil {
		return nil
	}
	if es.Level < 0 || es.Level > 16 {
		return errors.New("expand_state.level 必须在 0..16 之间")
	}
	if len(es.Overrides) > maxExpandOverrideItems {
		return fmt.Errorf("expand_state.overrides 条目数不能超过 %d", maxExpandOverrideItems)
	}
	for k := range es.Overrides {
		if strings.TrimSpace(k) == "" || len(k) > maxNodeKeyLength {
			return errors.New("expand_state.overrides 键非法")
		}
	}
	return nil
}

func validateFilterState(fs *models.SavedFilterState) error {
	if fs == nil {
		return nil
	}
	if len(fs.SubsystemIDs) > maxSubsystemFilterItems {
		return fmt.Errorf("filter_state.subsystem_ids 条目数不能超过 %d", maxSubsystemFilterItems)
	}
	for _, id := range fs.SubsystemIDs {
		if strings.TrimSpace(id) == "" || len(id) > maxNodeKeyLength {
			return errors.New("filter_state.subsystem_ids 包含非法容器 ID")
		}
	}
	if fs.Status != "" && fs.Status != "ALL" && fs.Status != "ABNORMAL" && fs.Status != "VIOLATION_ONLY" {
		return errors.New("filter_state.status 仅允许 ALL, ABNORMAL 或 VIOLATION_ONLY")
	}
	if fs.EgoHops != 0 && fs.EgoHops != 1 && fs.EgoHops != 2 {
		return errors.New("filter_state.ego_hops 仅允许 1 或 2")
	}
	if len(fs.EgoNodeID) > maxNodeKeyLength {
		return errors.New("filter_state.ego_node_id 长度不能超过 128")
	}
	return nil
}

func validateViewport(vp *models.SavedViewport) error {
	if vp == nil {
		return nil
	}
	if !isFiniteFloat(vp.X) || !isFiniteFloat(vp.Y) || !isFiniteFloat(vp.Zoom) {
		return errors.New("viewport 数值必须为有限实数")
	}
	if vp.Zoom < minViewportZoom || vp.Zoom > maxViewportZoom {
		return fmt.Errorf("viewport.zoom 必须在 [%.2f, %.2f] 范围内", minViewportZoom, maxViewportZoom)
	}
	return nil
}

func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint") || strings.Contains(msg, "23505")
}

// ListTopologyLayouts GET /api/arch/topology/layouts?view=LOGICAL|PHYSICAL
func ListTopologyLayouts(c *gin.Context) {
	view, err := validateViewParam(c.Query("view"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_VIEW"})
		return
	}

	uc := commonAuth.GetUserContext(c)

	var canonicalRow models.TopologyLayout
	var canonicalDetail *TopologyLayoutDetail
	if err := models.DB.Where("scope = ? AND view = ?", models.ScopeShared, view).First(&canonicalRow).Error; err == nil {
		d := toLayoutDetail(canonicalRow)
		canonicalDetail = &d
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询团队基准布局失败"})
		return
	}

	var personalRows []models.TopologyLayout
	if err := models.DB.
		Select("id, scope, view, owner_id, name, description, focus_scope, is_default, structure_version, revision, updated_at").
		Where("scope = ? AND owner_id = ? AND view = ?", models.ScopePrivate, uc.UserID, view).
		Order("is_default DESC, updated_at DESC, id DESC").
		Find(&personalRows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询个人视图书签列表失败"})
		return
	}

	summaries := make([]TopologyLayoutSummary, 0, len(personalRows))
	for _, row := range personalRows {
		summaries = append(summaries, toLayoutSummary(row))
	}

	c.JSON(http.StatusOK, gin.H{
		"canonical_layout": canonicalDetail,
		"personal_views":   summaries,
	})
}

// GetPersonalTopologyLayout GET /api/arch/topology/layouts/personal/:id
func GetPersonalTopologyLayout(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的书签 ID"})
		return
	}

	uc := commonAuth.GetUserContext(c)
	var row models.TopologyLayout
	if err := models.DB.First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "个人视图书签不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询个人视图书签失败"})
		return
	}

	if row.Scope != models.ScopePrivate {
		c.JSON(http.StatusNotFound, gin.H{"error": "个人视图书签不存在"})
		return
	}

	if row.OwnerID == nil || *row.OwnerID != uc.UserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权读取他人的个人视图书签"})
		return
	}

	c.JSON(http.StatusOK, toLayoutDetail(row))
}

// UpsertCanonicalTopologyLayout PUT /api/arch/topology/layouts/canonical (AdminOnly)
func UpsertCanonicalTopologyLayout(c *gin.Context) {
	var req UpsertCanonicalLayoutReq
	if !readBoundedJSONBody(c, &req) {
		return
	}

	view, err := validateViewParam(req.View)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_VIEW"})
		return
	}
	if req.ExpectedRevision < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expected_revision 不能为负数", "code": "INVALID_REVISION"})
		return
	}
	if len(req.StructureVersion) > 64 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "structure_version 长度不能超过 64", "code": "INVALID_STRUCTURE_VERSION"})
		return
	}
	if utf8.RuneCountInString(req.Description) > 512 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "description 长度不能超过 512", "code": "INVALID_DESCRIPTION"})
		return
	}
	if err := validateNodePositions(req.NodePositions); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_NODE_POSITIONS"})
		return
	}
	if err := validateExpandState(req.ExpandState); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_EXPAND_STATE"})
		return
	}
	if err := validateViewport(req.Viewport); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_VIEWPORT"})
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "团队基准布局"
	}
	if utf8.RuneCountInString(name) > 128 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name 长度不能超过 128", "code": "INVALID_NAME"})
		return
	}

	if req.NodePositions == nil {
		req.NodePositions = make(map[string]models.SavedNodePosition)
	}
	posJSON, _ := json.Marshal(req.NodePositions)

	expandVal := models.SavedExpandState{Level: 0}
	if req.ExpandState != nil {
		expandVal = *req.ExpandState
	}
	expandJSON, _ := json.Marshal(expandVal)

	vpVal := models.SavedViewport{X: 0, Y: 0, Zoom: 1.0}
	if req.Viewport != nil {
		vpVal = *req.Viewport
	}
	vpJSON, _ := json.Marshal(vpVal)

	uc := commonAuth.GetUserContext(c)
	var beforeSummary *topologyLayoutAuditSummary
	var savedRow models.TopologyLayout

	txErr := models.DB.Transaction(func(tx *gorm.DB) error {
		var existing models.TopologyLayout
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("scope = ? AND view = ?", models.ScopeShared, view).
			First(&existing).Error

		if errors.Is(err, gorm.ErrRecordNotFound) {
			if req.ExpectedRevision != 0 {
				return errLayoutRevisionConflict
			}
			newRow := models.TopologyLayout{
				Scope:            models.ScopeShared,
				View:             view,
				OwnerID:          nil,
				Name:             name,
				Description:      strings.TrimSpace(req.Description),
				FocusScope:       "GLOBAL",
				IsDefault:        false,
				SchemaVersion:    1,
				StructureVersion: strings.TrimSpace(req.StructureVersion),
				Revision:         1,
				NodePositions:    datatypes.JSON(posJSON),
				ExpandState:      datatypes.JSON(expandJSON),
				FilterState:      datatypes.JSON([]byte(`{}`)),
				Viewport:         datatypes.JSON(vpJSON),
				CreatedBy:        uc.UserID,
				UpdatedBy:        uc.UserID,
			}
			if err := tx.Create(&newRow).Error; err != nil {
				if isUniqueConstraintErr(err) {
					return errLayoutRevisionConflict
				}
				return err
			}
			savedRow = newRow
			return nil
		} else if err != nil {
			return err
		}

		if req.ExpectedRevision != existing.Revision {
			return errLayoutRevisionConflict
		}

		bs := toAuditSummary(existing)
		beforeSummary = &bs

		existing.Name = name
		existing.Description = strings.TrimSpace(req.Description)
		existing.FocusScope = "GLOBAL"
		existing.IsDefault = false
		existing.SchemaVersion = 1
		existing.StructureVersion = strings.TrimSpace(req.StructureVersion)
		existing.Revision = existing.Revision + 1
		existing.NodePositions = datatypes.JSON(posJSON)
		if req.ExpandState != nil {
			existing.ExpandState = datatypes.JSON(expandJSON)
		}
		if req.Viewport != nil {
			existing.Viewport = datatypes.JSON(vpJSON)
		}
		existing.UpdatedBy = uc.UserID

		if err := tx.Save(&existing).Error; err != nil {
			return err
		}
		savedRow = existing
		return nil
	})

	if txErr != nil {
		if errors.Is(txErr, errLayoutRevisionConflict) {
			c.JSON(http.StatusConflict, gin.H{
				"error": "团队基准布局已被其他管理员更新，请刷新获取最新基准后重试",
				"code":  "LAYOUT_REVISION_CONFLICT",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存团队基准布局失败: " + txErr.Error()})
		return
	}

	afterSummary := toAuditSummary(savedRow)
	recordAudit(
		c,
		"publish_canonical_layout",
		commonModels.AuditLevelP2,
		fmt.Sprintf("发布团队基准拓扑布局 [%s v%d]", savedRow.View, savedRow.Revision),
		"topology_layout",
		savedRow.ID,
		fmt.Sprintf("canonical_%s", savedRow.View),
		beforeSummary,
		afterSummary,
	)

	c.JSON(http.StatusOK, toLayoutDetail(savedRow))
}

// ResetCanonicalTopologyLayout DELETE /api/arch/topology/layouts/canonical?view=LOGICAL|PHYSICAL (AdminOnly)
func ResetCanonicalTopologyLayout(c *gin.Context) {
	view, err := validateViewParam(c.Query("view"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_VIEW"})
		return
	}

	var existing models.TopologyLayout
	if err := models.DB.Where("scope = ? AND view = ?", models.ScopeShared, view).First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "该视图尚未发布团队基准布局"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询团队基准布局失败"})
		return
	}

	beforeSummary := toAuditSummary(existing)
	if err := models.DB.Delete(&existing).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "重置团队基准布局失败"})
		return
	}

	recordAudit(
		c,
		"reset_canonical_layout",
		commonModels.AuditLevelP1,
		fmt.Sprintf("重置团队基准拓扑布局 [%s]", view),
		"topology_layout",
		existing.ID,
		fmt.Sprintf("canonical_%s", view),
		beforeSummary,
		nil,
	)

	c.JSON(http.StatusOK, gin.H{"message": "团队基准布局已重置"})
}

// CreatePersonalTopologyLayout POST /api/arch/topology/layouts/personal
func CreatePersonalTopologyLayout(c *gin.Context) {
	var req CreatePersonalLayoutReq
	if !readBoundedJSONBody(c, &req) {
		return
	}

	view, err := validateViewParam(req.View)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_VIEW"})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > 128 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "书签名称不能为空且长度不能超过 128", "code": "INVALID_NAME"})
		return
	}
	if utf8.RuneCountInString(req.Description) > 512 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "description 长度不能超过 512", "code": "INVALID_DESCRIPTION"})
		return
	}
	focusScope, err := normalizeAndValidateFocusScope(req.FocusScope)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_FOCUS_SCOPE"})
		return
	}
	if len(req.StructureVersion) > 64 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "structure_version 长度不能超过 64", "code": "INVALID_STRUCTURE_VERSION"})
		return
	}
	if err := validateNodePositions(req.NodePositions); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_NODE_POSITIONS"})
		return
	}
	if err := validateExpandState(req.ExpandState); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_EXPAND_STATE"})
		return
	}
	if err := validateFilterState(req.FilterState); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_FILTER_STATE"})
		return
	}
	if err := validateViewport(req.Viewport); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_VIEWPORT"})
		return
	}

	if req.NodePositions == nil {
		req.NodePositions = make(map[string]models.SavedNodePosition)
	}
	posJSON, _ := json.Marshal(req.NodePositions)

	expandVal := models.SavedExpandState{Level: 0}
	if req.ExpandState != nil {
		expandVal = *req.ExpandState
	}
	expandJSON, _ := json.Marshal(expandVal)

	filterVal := models.SavedFilterState{}
	if req.FilterState != nil {
		filterVal = *req.FilterState
	}
	filterJSON, _ := json.Marshal(filterVal)

	vpVal := models.SavedViewport{X: 0, Y: 0, Zoom: 1.0}
	if req.Viewport != nil {
		vpVal = *req.Viewport
	}
	vpJSON, _ := json.Marshal(vpVal)

	uc := commonAuth.GetUserContext(c)
	ownerID := uc.UserID
	var createdRow models.TopologyLayout

	txErr := models.DB.Transaction(func(tx *gorm.DB) error {
		// 事务咨询锁串行化单用户书签配额计数与默认书签切换
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext('traefik_topo_layout_quota'), CAST(? AS integer))", int32(ownerID)).Error; err != nil {
			return err
		}

		var count int64
		if err := tx.Model(&models.TopologyLayout{}).
			Where("scope = ? AND owner_id = ?", models.ScopePrivate, ownerID).
			Count(&count).Error; err != nil {
			return err
		}
		if count >= maxPersonalViewQuota {
			return errPersonalQuotaExceeded
		}

		var dupCount int64
		if err := tx.Model(&models.TopologyLayout{}).
			Where("scope = ? AND owner_id = ? AND view = ? AND name = ?", models.ScopePrivate, ownerID, view, name).
			Count(&dupCount).Error; err != nil {
			return err
		}
		if dupCount > 0 {
			return errDuplicateLayoutName
		}

		if req.IsDefault {
			if err := tx.Model(&models.TopologyLayout{}).
				Where("scope = ? AND owner_id = ? AND view = ? AND focus_scope = ? AND is_default = ?", models.ScopePrivate, ownerID, view, focusScope, true).
				Update("is_default", false).Error; err != nil {
				return err
			}
		}

		newRow := models.TopologyLayout{
			Scope:            models.ScopePrivate,
			View:             view,
			OwnerID:          &ownerID,
			Name:             name,
			Description:      strings.TrimSpace(req.Description),
			FocusScope:       focusScope,
			IsDefault:        req.IsDefault,
			SchemaVersion:    1,
			StructureVersion: strings.TrimSpace(req.StructureVersion),
			Revision:         1,
			NodePositions:    datatypes.JSON(posJSON),
			ExpandState:      datatypes.JSON(expandJSON),
			FilterState:      datatypes.JSON(filterJSON),
			Viewport:         datatypes.JSON(vpJSON),
			CreatedBy:        ownerID,
			UpdatedBy:        ownerID,
		}
		if err := tx.Create(&newRow).Error; err != nil {
			if isUniqueConstraintErr(err) {
				return errDuplicateLayoutName
			}
			return err
		}
		createdRow = newRow
		return nil
	})

	if txErr != nil {
		if errors.Is(txErr, errPersonalQuotaExceeded) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "个人视图书签已达上限 (20 个)，请清理旧书签后重试",
				"code":  "PERSONAL_VIEW_QUOTA_EXCEEDED",
			})
			return
		}
		if errors.Is(txErr, errDuplicateLayoutName) {
			c.JSON(http.StatusConflict, gin.H{
				"error": "同视图下已存在同名个人视图书签",
				"code":  "DUPLICATE_LAYOUT_NAME",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建个人视图书签失败: " + txErr.Error()})
		return
	}

	afterSummary := toAuditSummary(createdRow)
	recordAudit(
		c,
		"create_personal_layout",
		commonModels.AuditLevelP2,
		fmt.Sprintf("创建个人拓扑视图书签 [%s]", createdRow.Name),
		"topology_layout",
		createdRow.ID,
		createdRow.Name,
		nil,
		afterSummary,
	)

	c.JSON(http.StatusCreated, toLayoutDetail(createdRow))
}

// UpdatePersonalTopologyLayout PUT /api/arch/topology/layouts/personal/:id
func UpdatePersonalTopologyLayout(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的书签 ID"})
		return
	}

	var req UpdatePersonalLayoutReq
	if !readBoundedJSONBody(c, &req) {
		return
	}

	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" || utf8.RuneCountInString(trimmed) > 128 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "书签名称不能为空且长度不能超过 128", "code": "INVALID_NAME"})
			return
		}
	}
	if req.Description != nil && utf8.RuneCountInString(*req.Description) > 512 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "description 长度不能超过 512", "code": "INVALID_DESCRIPTION"})
		return
	}
	var validatedFocus string
	if req.FocusScope != nil {
		fs, err := normalizeAndValidateFocusScope(*req.FocusScope)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_FOCUS_SCOPE"})
			return
		}
		validatedFocus = fs
	}
	if req.StructureVersion != nil && len(*req.StructureVersion) > 64 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "structure_version 长度不能超过 64", "code": "INVALID_STRUCTURE_VERSION"})
		return
	}
	if req.NodePositions != nil {
		if err := validateNodePositions(req.NodePositions); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_NODE_POSITIONS"})
			return
		}
	}
	if err := validateExpandState(req.ExpandState); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_EXPAND_STATE"})
		return
	}
	if err := validateFilterState(req.FilterState); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_FILTER_STATE"})
		return
	}
	if err := validateViewport(req.Viewport); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_VIEWPORT"})
		return
	}

	uc := commonAuth.GetUserContext(c)
	var beforeSummary topologyLayoutAuditSummary
	var updatedRow models.TopologyLayout

	txErr := models.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext('traefik_topo_layout_quota'), CAST(? AS integer))", int32(uc.UserID)).Error; err != nil {
			return err
		}

		var row models.TopologyLayout
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errLayoutNotFound
			}
			return err
		}

		if row.Scope != models.ScopePrivate {
			return errLayoutNotFound
		}
		if row.OwnerID == nil || *row.OwnerID != uc.UserID {
			return errLayoutForbidden
		}

		beforeSummary = toAuditSummary(row)

		if req.Name != nil {
			newName := strings.TrimSpace(*req.Name)
			if newName != row.Name {
				var dupCount int64
				if err := tx.Model(&models.TopologyLayout{}).
					Where("scope = ? AND owner_id = ? AND view = ? AND name = ? AND id <> ?", models.ScopePrivate, uc.UserID, row.View, newName, row.ID).
					Count(&dupCount).Error; err != nil {
					return err
				}
				if dupCount > 0 {
					return errDuplicateLayoutName
				}
				row.Name = newName
			}
		}

		if req.Description != nil {
			row.Description = strings.TrimSpace(*req.Description)
		}
		if req.FocusScope != nil {
			row.FocusScope = validatedFocus
		}
		if req.IsDefault != nil {
			row.IsDefault = *req.IsDefault
		}
		if req.StructureVersion != nil {
			row.StructureVersion = strings.TrimSpace(*req.StructureVersion)
		}
		if req.NodePositions != nil {
			b, _ := json.Marshal(req.NodePositions)
			row.NodePositions = datatypes.JSON(b)
		}
		if req.ExpandState != nil {
			b, _ := json.Marshal(req.ExpandState)
			row.ExpandState = datatypes.JSON(b)
		}
		if req.FilterState != nil {
			b, _ := json.Marshal(req.FilterState)
			row.FilterState = datatypes.JSON(b)
		}
		if req.Viewport != nil {
			b, _ := json.Marshal(req.Viewport)
			row.Viewport = datatypes.JSON(b)
		}

		if row.IsDefault {
			if err := tx.Model(&models.TopologyLayout{}).
				Where("scope = ? AND owner_id = ? AND view = ? AND focus_scope = ? AND is_default = ? AND id <> ?", models.ScopePrivate, uc.UserID, row.View, row.FocusScope, true, row.ID).
				Update("is_default", false).Error; err != nil {
				return err
			}
		}

		row.Revision = row.Revision + 1
		row.UpdatedBy = uc.UserID

		if err := tx.Save(&row).Error; err != nil {
			if isUniqueConstraintErr(err) {
				return errDuplicateLayoutName
			}
			return err
		}
		updatedRow = row
		return nil
	})

	if txErr != nil {
		if errors.Is(txErr, errLayoutNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "个人视图书签不存在"})
			return
		}
		if errors.Is(txErr, errLayoutForbidden) {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权修改他人的个人视图书签"})
			return
		}
		if errors.Is(txErr, errDuplicateLayoutName) {
			c.JSON(http.StatusConflict, gin.H{"error": "同视图下已存在同名个人视图书签", "code": "DUPLICATE_LAYOUT_NAME"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新个人视图书签失败: " + txErr.Error()})
		return
	}

	afterSummary := toAuditSummary(updatedRow)
	recordAudit(
		c,
		"update_personal_layout",
		commonModels.AuditLevelP2,
		fmt.Sprintf("更新个人拓扑视图书签 [%s]", updatedRow.Name),
		"topology_layout",
		updatedRow.ID,
		updatedRow.Name,
		beforeSummary,
		afterSummary,
	)

	c.JSON(http.StatusOK, toLayoutDetail(updatedRow))
}

// DeletePersonalTopologyLayout DELETE /api/arch/topology/layouts/personal/:id
func DeletePersonalTopologyLayout(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的书签 ID"})
		return
	}

	uc := commonAuth.GetUserContext(c)
	var row models.TopologyLayout
	if err := models.DB.First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "个人视图书签不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询个人视图书签失败"})
		return
	}

	if row.Scope != models.ScopePrivate {
		c.JSON(http.StatusNotFound, gin.H{"error": "个人视图书签不存在"})
		return
	}

	isOwner := row.OwnerID != nil && *row.OwnerID == uc.UserID
	if !isOwner && !isCurrentUserAdmin(uc) {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权删除他人的个人视图书签"})
		return
	}

	beforeSummary := toAuditSummary(row)
	if err := models.DB.Delete(&row).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除个人视图书签失败"})
		return
	}

	recordAudit(
		c,
		"delete_personal_layout",
		commonModels.AuditLevelP1,
		fmt.Sprintf("删除个人拓扑视图书签 [%s]", row.Name),
		"topology_layout",
		row.ID,
		row.Name,
		beforeSummary,
		nil,
	)

	c.JSON(http.StatusOK, gin.H{"message": "个人视图书签已删除"})
}
