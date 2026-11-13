package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	commonModels "code-common/backend/models"
	"code-traefik/internal/server/protodoc"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ProtoFileDTO M23 虚拟只读 Proto 文件清单 DTO（严格无 locked/locked_by 悲观锁字段；M25 无缝切换至 Git 持久化表）
type ProtoFileDTO struct {
	ID               uint64    `json:"id"`
	SchemaID         uint64    `json:"schema_id"`
	TargetSchemaName string    `json:"target_schema_name"`
	Category         string    `json:"category"`
	FilePath         string    `json:"file_path"`
	PackageName      string    `json:"package_name"`
	LatestVersion    int       `json:"latest_version"`
	IsActiveSchema   bool      `json:"is_active_schema"`
	HasSourceInfo    bool      `json:"has_source_info"`
	SourceOrigin     string    `json:"source_origin"`
	LineCount        int       `json:"line_count"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// ProtoFileVersionDTO 只读文件单版本内容元数据 DTO
type ProtoFileVersionDTO struct {
	SchemaID    uint64    `json:"schema_id"`
	Version     int       `json:"version"`
	VersionTag  string    `json:"version_tag"`
	CommitSHA   string    `json:"commit_sha"`
	ContentSHA  string    `json:"content_sha256"`
	SourceType  string    `json:"source_type"`
	AuthorName  string    `json:"author_name"`
	Summary     string    `json:"summary"`
	CreatedAt   time.Time `json:"created_at"`
	LineCount   int       `json:"line_count"`
	IsActiveVer bool      `json:"is_active"`
}

// ProtoFileVersionSummaryDTO 版本历史摘要列表 DTO（不含源码正文）
type ProtoFileVersionSummaryDTO struct {
	SchemaID    uint64    `json:"schema_id"`
	Version     int       `json:"version"`
	VersionTag  string    `json:"version_tag"`
	CommitSHA   string    `json:"commit_sha"`
	ContentSHA  string    `json:"content_sha256"`
	SourceType  string    `json:"source_type"`
	AuthorName  string    `json:"author_name"`
	Summary     string    `json:"summary"`
	IsActiveVer bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
}

func parseOptionalUint64Query(c *gin.Context, key string) (*uint64, bool) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return nil, true
	}
	v, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || v == 0 {
		return nil, false
	}
	return &v, true
}

func parseStandardPagination(c *gin.Context) (int, int) {
	page := 1
	if pRaw := strings.TrimSpace(c.Query("page")); pRaw != "" {
		if parsed, err := strconv.Atoi(pRaw); err == nil && parsed > 0 {
			page = parsed
		}
	}
	pageSize := 25
	if psRaw := strings.TrimSpace(c.Query("page_size")); psRaw != "" {
		if parsed, err := strconv.Atoi(psRaw); err == nil && parsed > 0 {
			if parsed > 100 {
				parsed = 100
			}
			pageSize = parsed
		}
	}
	return page, pageSize
}

func sha256HexString(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func resolveCreatorNameMap(db *gorm.DB, schemas []models.ProtoSchema) map[uint]string {
	out := make(map[uint]string)
	if db == nil || len(schemas) == 0 {
		return out
	}
	idSet := make(map[uint]struct{})
	ids := make([]uint, 0, len(schemas))
	for _, sc := range schemas {
		if sc.CreatedBy > 0 {
			if _, ok := idSet[sc.CreatedBy]; !ok {
				idSet[sc.CreatedBy] = struct{}{}
				ids = append(ids, sc.CreatedBy)
			}
		}
	}
	if len(ids) == 0 {
		return out
	}
	var users []commonModels.User
	if err := db.Select("id, username, name").Where("id IN ?", ids).Find(&users).Error; err == nil {
		for _, u := range users {
			if u.Name != "" {
				out[u.ID] = u.Name
			} else {
				out[u.ID] = u.Username
			}
		}
	}
	return out
}

func selectDisplaySchemas(db *gorm.DB, targetSchemaName string) ([]models.ProtoSchema, error) {
	var all []models.ProtoSchema
	q := db.Model(&models.ProtoSchema{})
	if targetSchemaName != "" {
		q = q.Where("name = ?", targetSchemaName)
	}
	if err := q.Order("name ASC, is_active DESC, version DESC").Find(&all).Error; err != nil {
		return nil, err
	}

	selectedByName := make(map[string]models.ProtoSchema)
	orderedNames := make([]string, 0)
	for _, sc := range all {
		if _, exists := selectedByName[sc.Name]; !exists {
			selectedByName[sc.Name] = sc
			orderedNames = append(orderedNames, sc.Name)
		}
	}
	sort.Strings(orderedNames)
	out := make([]models.ProtoSchema, 0, len(orderedNames))
	for _, name := range orderedNames {
		out = append(out, selectedByName[name])
	}
	return out, nil
}

// GetProtoDocsTree GET /api/proto-docs/tree
// 按子系统 / 制品包 / Package / Service 聚合返回活体接口文档导航树
func GetProtoDocsTree(c *gin.Context) {
	subID, ok := parseOptionalUint64Query(c, "subsystem_id")
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 subsystem_id 参数"})
		return
	}
	schemaID, ok := parseOptionalUint64Query(c, "schema_id")
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 schema_id 参数"})
		return
	}

	includeDeprecated := true
	if rawInc := strings.TrimSpace(c.Query("include_deprecated")); rawInc != "" {
		parsed, err := strconv.ParseBool(rawInc)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 include_deprecated 参数"})
			return
		}
		includeDeprecated = parsed
	}

	tree, err := protodoc.BuildDocTree(models.DB, protodoc.DocTreeQuery{
		SubsystemID:       subID,
		Category:          strings.TrimSpace(c.Query("category")),
		SchemaID:          schemaID,
		IncludeDeprecated: includeDeprecated,
		Search:            strings.TrimSpace(c.Query("search")),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, tree)
}

// GetProtoServiceDocDetail GET /api/proto-docs/services/:serviceName
// 返回指定 Service 的完整活体参考文档、缺省/黄金 JSON 样本、Enum 字典与 ADR-16 血缘
func GetProtoServiceDocDetail(c *gin.Context) {
	serviceName := strings.TrimSpace(c.Param("serviceName"))
	if serviceName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "serviceName 不能为空"})
		return
	}
	schemaID, ok := parseOptionalUint64Query(c, "schema_id")
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 schema_id 参数"})
		return
	}

	detail, err := protodoc.GetServiceDocDetail(models.DB, serviceName, schemaID)
	if err != nil {
		if errors.Is(err, protodoc.ErrServiceDocNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, detail)
}

// ExportProtoDocs GET /api/proto-docs/export
// 导出带目录、契约表、Enum 字典与 JSON 示例的离线 text/markdown 手册
func ExportProtoDocs(c *gin.Context) {
	schemaID, ok := parseOptionalUint64Query(c, "schema_id")
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 schema_id 参数"})
		return
	}
	subsystemID, ok := parseOptionalUint64Query(c, "subsystem_id")
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 subsystem_id 参数"})
		return
	}

	filename, mdContent, err := protodoc.ExportMarkdown(models.DB, protodoc.ExportMarkdownRequest{
		ServiceName: strings.TrimSpace(c.Query("service_name")),
		SchemaID:    schemaID,
		SubsystemID: subsystemID,
		Format:      strings.TrimSpace(c.Query("format")),
	})
	if err != nil {
		switch {
		case errors.Is(err, protodoc.ErrMissingExportScope), errors.Is(err, protodoc.ErrInvalidExportFormat):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, protodoc.ErrServiceDocNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}

	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/markdown; charset=utf-8", []byte(mdContent))
}

// GetProtoSchemaSources GET /api/proto-schemas/:id/sources
// M23 过渡只读源码适配器：从已编译描述集重构源码文本与精确行号锚点，供 #L42 定位浏览（无任何锁字段）
func GetProtoSchemaSources(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的描述集 ID"})
		return
	}

	var schema models.ProtoSchema
	if err := models.DB.First(&schema, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "未找到目标 ProtoSchema"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	protodoc.EnsureSymbolsForSchema(models.DB, &schema)
	var syms []models.ProtoDocSymbol
	if findErr := models.DB.Where("schema_id = ?", schema.ID).Find(&syms).Error; findErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": findErr.Error()})
		return
	}

	files, err := protodoc.ReconstructProtoSources(&schema, syms)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	filterPath := strings.TrimSpace(c.Query("file_path"))
	if filterPath != "" {
		filtered := make([]protodoc.ReconstructedProtoFile, 0, 1)
		for _, f := range files {
			if f.FilePath == filterPath {
				filtered = append(filtered, f)
			}
		}
		files = filtered
	}

	hasSourceInfo := false
	for _, f := range files {
		if f.HasSourceInfo {
			hasSourceInfo = true
			break
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"schema_id":       schema.ID,
		"schema_name":     schema.Name,
		"schema_version":  schema.Version,
		"source_type":     schema.SourceType,
		"has_source_info": hasSourceInfo,
		"source_origin":   "DESCRIPTOR_RECONSTRUCTED",
		"files":           files,
	})
}

func loadVirtualFilesForSchema(db *gorm.DB, schema *models.ProtoSchema) ([]protodoc.ReconstructedProtoFile, error) {
	protodoc.EnsureSymbolsForSchema(db, schema)
	var syms []models.ProtoDocSymbol
	if err := db.Where("schema_id = ?", schema.ID).Find(&syms).Error; err != nil {
		return nil, err
	}
	return protodoc.ReconstructProtoSources(schema, syms)
}

func resolveVirtualFileByID(db *gorm.DB, fileID uint64) (*models.ProtoSchema, *protodoc.ReconstructedProtoFile, error) {
	if fileID < 1000 {
		return nil, nil, protodoc.ErrServiceDocNotFound
	}
	schemaID := fileID / 1000
	var schema models.ProtoSchema
	if err := db.First(&schema, schemaID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, protodoc.ErrServiceDocNotFound
		}
		return nil, nil, err
	}
	files, err := loadVirtualFilesForSchema(db, &schema)
	if err != nil {
		return nil, nil, err
	}
	for i := range files {
		if files[i].FileID == fileID {
			return &schema, &files[i], nil
		}
	}
	return nil, nil, protodoc.ErrServiceDocNotFound
}

func buildFileDTO(sc models.ProtoSchema, f protodoc.ReconstructedProtoFile) ProtoFileDTO {
	return ProtoFileDTO{
		ID:               f.FileID,
		SchemaID:         sc.ID,
		TargetSchemaName: sc.Name,
		Category:         sc.Category,
		FilePath:         f.FilePath,
		PackageName:      f.PackageName,
		LatestVersion:    sc.Version,
		IsActiveSchema:   sc.IsActive,
		HasSourceInfo:    f.HasSourceInfo,
		SourceOrigin:     f.SourceOrigin,
		LineCount:        f.LineCount,
		UpdatedAt:        sc.UpdatedAt,
	}
}

func buildVersionDTO(sc models.ProtoSchema, f protodoc.ReconstructedProtoFile, authorMap map[uint]string) ProtoFileVersionDTO {
	commitSHA := sc.SourceCommitSHA
	if commitSHA == "" {
		commitSHA = fmt.Sprintf("schema-%d-v%d", sc.ID, sc.Version)
	}
	summary := strings.TrimSpace(sc.Description)
	if summary == "" {
		if sc.VersionTag != "" {
			summary = fmt.Sprintf("制品 %s v%d (%s)", sc.Name, sc.Version, sc.VersionTag)
		} else {
			summary = fmt.Sprintf("制品 %s 版本 v%d", sc.Name, sc.Version)
		}
	}
	author := authorMap[sc.CreatedBy]
	if author == "" {
		author = "system"
	}
	return ProtoFileVersionDTO{
		SchemaID:    sc.ID,
		Version:     sc.Version,
		VersionTag:  sc.VersionTag,
		CommitSHA:   commitSHA,
		ContentSHA:  sha256HexString(f.Content),
		SourceType:  sc.SourceType,
		AuthorName:  author,
		Summary:     summary,
		CreatedAt:   sc.CreatedAt,
		LineCount:   f.LineCount,
		IsActiveVer: sc.IsActive,
	}
}

// ListProtoFiles GET /api/proto-files
// M23 只读过渡列表：聚合已编译/上传 Schema 源树文件（严格按 target_schema_name, file_path 排序，无任何锁字段）
func ListProtoFiles(c *gin.Context) {
	page, pageSize := parseStandardPagination(c)
	targetSchema := strings.TrimSpace(c.Query("target_schema_name"))
	packageFilter := strings.TrimSpace(c.Query("package_name"))
	search := strings.ToLower(strings.TrimSpace(c.Query("search")))

	schemas, err := selectDisplaySchemas(models.DB, targetSchema)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	allItems := make([]ProtoFileDTO, 0)
	for i := range schemas {
		files, err := loadVirtualFilesForSchema(models.DB, &schemas[i])
		if err != nil {
			continue
		}
		for _, f := range files {
			if packageFilter != "" && f.PackageName != packageFilter {
				continue
			}
			if search != "" {
				haystack := strings.ToLower(schemas[i].Name + " " + f.FilePath + " " + f.PackageName)
				if !strings.Contains(haystack, search) {
					continue
				}
			}
			allItems = append(allItems, buildFileDTO(schemas[i], f))
		}
	}

	sort.Slice(allItems, func(i, j int) bool {
		if allItems[i].TargetSchemaName != allItems[j].TargetSchemaName {
			return allItems[i].TargetSchemaName < allItems[j].TargetSchemaName
		}
		return allItems[i].FilePath < allItems[j].FilePath
	})

	total := len(allItems)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	c.JSON(http.StatusOK, gin.H{
		"items":        allItems[start:end],
		"total":        total,
		"page":         page,
		"page_size":    pageSize,
		"storage_mode": "M23_DESCRIPTOR_VIRTUAL",
	})
}

// GetProtoFileContent GET /api/proto-files/:id/content
func GetProtoFileContent(c *gin.Context) {
	fileID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || fileID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 proto file ID"})
		return
	}

	baseSchema, baseFile, err := resolveVirtualFileByID(models.DB, fileID)
	if err != nil {
		if errors.Is(err, protodoc.ErrServiceDocNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "未找到目标只读 Proto 文件"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	targetSchema := *baseSchema
	targetFile := *baseFile
	if verRaw := strings.TrimSpace(c.Query("version")); verRaw != "" {
		verNum, parseErr := strconv.Atoi(verRaw)
		if parseErr != nil || verNum <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 version 版本号参数"})
			return
		}
		var verSchema models.ProtoSchema
		if findErr := models.DB.Where("name = ? AND version = ?", baseSchema.Name, verNum).First(&verSchema).Error; findErr != nil {
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "指定版本文件不存在"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": findErr.Error()})
			return
		}
		vFiles, vErr := loadVirtualFilesForSchema(models.DB, &verSchema)
		if vErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": vErr.Error()})
			return
		}
		matched := false
		for _, vf := range vFiles {
			if vf.FilePath == baseFile.FilePath {
				targetSchema = verSchema
				targetFile = vf
				matched = true
				break
			}
		}
		if !matched {
			c.JSON(http.StatusNotFound, gin.H{"error": "指定版本中不存在该 .proto 文件"})
			return
		}
	}

	authors := resolveCreatorNameMap(models.DB, []models.ProtoSchema{targetSchema})
	c.JSON(http.StatusOK, gin.H{
		"file":           buildFileDTO(*baseSchema, *baseFile),
		"version":        buildVersionDTO(targetSchema, targetFile, authors),
		"content":        targetFile.Content,
		"violations":     []any{},
		"symbol_anchors": targetFile.SymbolAnchors,
	})
}

type matchedSchemaFile struct {
	Schema models.ProtoSchema
	File   protodoc.ReconstructedProtoFile
}

func loadAllVersionsForVirtualFile(db *gorm.DB, baseSchema *models.ProtoSchema, filePath string) ([]matchedSchemaFile, error) {
	var schemas []models.ProtoSchema
	if err := db.Where("name = ?", baseSchema.Name).Order("version DESC").Find(&schemas).Error; err != nil {
		return nil, err
	}
	out := make([]matchedSchemaFile, 0, len(schemas))
	for i := range schemas {
		files, err := loadVirtualFilesForSchema(db, &schemas[i])
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.FilePath == filePath {
				out = append(out, matchedSchemaFile{Schema: schemas[i], File: f})
				break
			}
		}
	}
	return out, nil
}

// ListProtoFileVersions GET /api/proto-files/:id/versions
func ListProtoFileVersions(c *gin.Context) {
	fileID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || fileID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 proto file ID"})
		return
	}
	page, pageSize := parseStandardPagination(c)

	baseSchema, baseFile, err := resolveVirtualFileByID(models.DB, fileID)
	if err != nil {
		if errors.Is(err, protodoc.ErrServiceDocNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "未找到目标只读 Proto 文件"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	history, err := loadAllVersionsForVirtualFile(models.DB, baseSchema, baseFile.FilePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	schemasOnly := make([]models.ProtoSchema, 0, len(history))
	for _, h := range history {
		schemasOnly = append(schemasOnly, h.Schema)
	}
	authors := resolveCreatorNameMap(models.DB, schemasOnly)

	items := make([]ProtoFileVersionSummaryDTO, 0, len(history))
	for _, h := range history {
		fullVer := buildVersionDTO(h.Schema, h.File, authors)
		items = append(items, ProtoFileVersionSummaryDTO{
			SchemaID:    fullVer.SchemaID,
			Version:     fullVer.Version,
			VersionTag:  fullVer.VersionTag,
			CommitSHA:   fullVer.CommitSHA,
			ContentSHA:  fullVer.ContentSHA,
			SourceType:  fullVer.SourceType,
			AuthorName:  fullVer.AuthorName,
			Summary:     fullVer.Summary,
			IsActiveVer: fullVer.IsActiveVer,
			CreatedAt:   fullVer.CreatedAt,
		})
	}

	total := len(items)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     items[start:end],
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetProtoFileDiff GET /api/proto-files/:id/diff
func GetProtoFileDiff(c *gin.Context) {
	fileID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || fileID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 proto file ID"})
		return
	}
	fromParam := strings.TrimSpace(c.Query("from_version"))
	if fromParam == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "必须指定 from_version 参数 (整数版本号或 prev)"})
		return
	}
	toParam := strings.TrimSpace(c.DefaultQuery("to_version", "latest"))

	baseSchema, baseFile, err := resolveVirtualFileByID(models.DB, fileID)
	if err != nil {
		if errors.Is(err, protodoc.ErrServiceDocNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "未找到目标只读 Proto 文件"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	history, err := loadAllVersionsForVirtualFile(models.DB, baseSchema, baseFile.FilePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if len(history) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "暂无可用文件版本历史"})
		return
	}

	var toIdx int = -1
	if toParam == "" || strings.EqualFold(toParam, "latest") {
		toIdx = 0
	} else {
		toVer, convErr := strconv.Atoi(toParam)
		if convErr != nil || toVer <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 to_version 参数"})
			return
		}
		for i, h := range history {
			if h.Schema.Version == toVer {
				toIdx = i
				break
			}
		}
	}
	if toIdx < 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "目标对比版本 to_version 不存在"})
		return
	}

	var fromIdx int = -1
	if strings.EqualFold(fromParam, "prev") {
		if toIdx+1 < len(history) {
			fromIdx = toIdx + 1
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "当前已是首个历史版本，无前序 prev 版本可对比"})
			return
		}
	} else {
		fromVer, convErr := strconv.Atoi(fromParam)
		if convErr != nil || fromVer <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 from_version 参数"})
			return
		}
		for i, h := range history {
			if h.Schema.Version == fromVer {
				fromIdx = i
				break
			}
		}
	}
	if fromIdx < 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "源对比版本 from_version 不存在"})
		return
	}

	fromEntry := history[fromIdx]
	toEntry := history[toIdx]
	authors := resolveCreatorNameMap(models.DB, []models.ProtoSchema{fromEntry.Schema, toEntry.Schema})
	unifiedDiff, added, deleted := computeUnifiedDiff(
		fmt.Sprintf("%s@v%d", baseFile.FilePath, fromEntry.Schema.Version),
		fmt.Sprintf("%s@v%d", baseFile.FilePath, toEntry.Schema.Version),
		fromEntry.File.Content,
		toEntry.File.Content,
	)

	c.JSON(http.StatusOK, gin.H{
		"file":          buildFileDTO(*baseSchema, *baseFile),
		"from_version":  buildVersionDTO(fromEntry.Schema, fromEntry.File, authors),
		"to_version":    buildVersionDTO(toEntry.Schema, toEntry.File, authors),
		"unified_diff":  unifiedDiff,
		"added_lines":   added,
		"deleted_lines": deleted,
	})
}

type diffOp struct {
	Kind string // " " | "-" | "+"
	Text string
}

func computeUnifiedDiff(oldLabel, newLabel, oldText, newText string) (string, int, int) {
	oldLines := splitNormalizedLines(oldText)
	newLines := splitNormalizedLines(newText)

	n := len(oldLines)
	m := len(newLines)
	// DP LCS 表（单文件 .proto 仅百行级，空间极小且零第三方依赖）
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	ops := make([]diffOp, 0, n+m)
	i, j := 0, 0
	added := 0
	deleted := 0
	for i < n && j < m {
		switch {
		case oldLines[i] == newLines[j]:
			ops = append(ops, diffOp{Kind: " ", Text: oldLines[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			ops = append(ops, diffOp{Kind: "-", Text: oldLines[i]})
			deleted++
			i++
		default:
			ops = append(ops, diffOp{Kind: "+", Text: newLines[j]})
			added++
			j++
		}
	}
	for i < n {
		ops = append(ops, diffOp{Kind: "-", Text: oldLines[i]})
		deleted++
		i++
	}
	for j < m {
		ops = append(ops, diffOp{Kind: "+", Text: newLines[j]})
		added++
		j++
	}

	var sb strings.Builder
	sb.WriteString("--- a/" + oldLabel + "\n")
	sb.WriteString("+++ b/" + newLabel + "\n")
	sb.WriteString(fmt.Sprintf("@@ -1,%d +1,%d @@\n", n, m))
	for _, op := range ops {
		sb.WriteString(op.Kind + op.Text + "\n")
	}
	return sb.String(), added, deleted
}

func splitNormalizedLines(text string) []string {
	trimmed := strings.TrimSuffix(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if trimmed == "" {
		return []string{}
	}
	return strings.Split(trimmed, "\n")
}

// rebuildProtoDocSymbolsForSchema 在描述集上传、编译或激活后重建活体文档符号索引
func rebuildProtoDocSymbolsForSchema(schema *models.ProtoSchema) int {
	if models.DB == nil || schema == nil || schema.ID == 0 {
		return 0
	}
	count, err := protodoc.RebuildForSchema(models.DB, schema)
	if err != nil {
		log.Printf("[ProtoDoc] 重建 Schema(id=%d, name=%s) 文档符号失败: %v", schema.ID, schema.Name, err)
		return 0
	}
	return count
}
