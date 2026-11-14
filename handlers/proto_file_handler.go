package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"code-traefik/internal/server/protodoc"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func buildGitProtoFileDTO(
	pf models.ProtoFile,
	repoNames map[uint64]string,
	bundleNames map[uint64]string,
	schemaByID map[uint64]models.ProtoSchema,
	latestVerSchemaByFile map[uint64]uint64,
	activeSchemaByName map[string]models.ProtoSchema,
) ProtoFileDTO {
	repoID := pf.GitRepoID
	bundleID := pf.BundleID
	schemaID := latestVerSchemaByFile[pf.ID]
	var isActiveSchema bool

	if sc, ok := schemaByID[schemaID]; ok && schemaID > 0 {
		isActiveSchema = sc.IsActive
	} else if sc, ok := activeSchemaByName[pf.TargetSchemaName]; ok {
		schemaID = sc.ID
		isActiveSchema = sc.IsActive
	}

	origin := models.ProtoSourceGitSync
	if pf.IsDeleted {
		origin = "GIT_DELETED"
	}

	return ProtoFileDTO{
		ID:               pf.ID,
		SchemaID:         schemaID,
		GitRepoID:        &repoID,
		GitRepoName:      repoNames[repoID],
		BundleID:         &bundleID,
		BundleName:       bundleNames[bundleID],
		TargetSchemaName: pf.TargetSchemaName,
		Category:         pf.Category,
		FilePath:         pf.FilePath,
		RepoFilePath:     pf.RepoFilePath,
		PackageName:      pf.PackageName,
		LatestVersion:    pf.LatestVersionNum,
		LatestCommitSHA:  pf.LatestCommitSHA,
		ContentSHA256:    pf.ContentSHA256,
		IsActiveSchema:   isActiveSchema,
		HasSourceInfo:    true,
		IsDeleted:        pf.IsDeleted,
		SourceOrigin:     origin,
		LineCount:        pf.LineCount,
		UpdatedAt:        pf.UpdatedAt,
	}
}

func buildGitProtoFileVersionDTO(ver models.ProtoFileVersion, activeSchemaID uint64) ProtoFileVersionDTO {
	var schemaID uint64
	if ver.SchemaID != nil {
		schemaID = *ver.SchemaID
	}
	summary := strings.TrimSpace(ver.CommitMessage)
	if summary == "" {
		summary = fmt.Sprintf("Commit %s (%s)", ver.CommitSHA, ver.VersionTag)
	}
	author := strings.TrimSpace(ver.AuthorName)
	if author == "" {
		author = "git-bot"
	}
	return ProtoFileVersionDTO{
		ID:            ver.ID,
		ProtoFileID:   ver.ProtoFileID,
		SchemaID:      schemaID,
		Version:       ver.Version,
		VersionTag:    ver.VersionTag,
		CommitSHA:     ver.CommitSHA,
		CommitMessage: ver.CommitMessage,
		ContentSHA:    ver.ContentSHA256,
		SourceType:    ver.SourceType,
		AuthorName:    author,
		AuthorEmail:   ver.AuthorEmail,
		Summary:       summary,
		CommittedAt:   ver.CommittedAt,
		CreatedAt:     ver.CreatedAt,
		LineCount:     ver.LineCount,
		IsActiveVer:   schemaID > 0 && activeSchemaID > 0 && schemaID == activeSchemaID,
	}
}

func resolveProtoFileRelations(db *gorm.DB, files []models.ProtoFile) (
	repoNames map[uint64]string,
	bundleNames map[uint64]string,
	schemaByID map[uint64]models.ProtoSchema,
	latestVerSchemaByFile map[uint64]uint64,
	activeSchemaByName map[string]models.ProtoSchema,
) {
	repoNames = make(map[uint64]string)
	bundleNames = make(map[uint64]string)
	schemaByID = make(map[uint64]models.ProtoSchema)
	latestVerSchemaByFile = make(map[uint64]uint64)
	activeSchemaByName = make(map[string]models.ProtoSchema)
	if db == nil || len(files) == 0 {
		return
	}

	repoIDs := make([]uint64, 0, len(files))
	bundleIDs := make([]uint64, 0, len(files))
	verIDs := make([]uint64, 0, len(files))
	targetNames := make([]string, 0, len(files))

	repoSeen := make(map[uint64]struct{})
	bundleSeen := make(map[uint64]struct{})
	nameSeen := make(map[string]struct{})

	for _, f := range files {
		if _, ok := repoSeen[f.GitRepoID]; !ok && f.GitRepoID > 0 {
			repoSeen[f.GitRepoID] = struct{}{}
			repoIDs = append(repoIDs, f.GitRepoID)
		}
		if _, ok := bundleSeen[f.BundleID]; !ok && f.BundleID > 0 {
			bundleSeen[f.BundleID] = struct{}{}
			bundleIDs = append(bundleIDs, f.BundleID)
		}
		if f.LatestVersionID != nil && *f.LatestVersionID > 0 {
			verIDs = append(verIDs, *f.LatestVersionID)
		}
		if _, ok := nameSeen[f.TargetSchemaName]; !ok && f.TargetSchemaName != "" {
			nameSeen[f.TargetSchemaName] = struct{}{}
			targetNames = append(targetNames, f.TargetSchemaName)
		}
	}

	if len(repoIDs) > 0 {
		var repos []models.GitRepo
		if err := db.Select("id, name").Where("id IN ?", repoIDs).Find(&repos).Error; err == nil {
			for _, r := range repos {
				repoNames[r.ID] = r.Name
			}
		}
	}
	if len(bundleIDs) > 0 {
		var bundles []models.GitProtoBundle
		if err := db.Select("id, bundle_name, last_schema_id").Where("id IN ?", bundleIDs).Find(&bundles).Error; err == nil {
			for _, b := range bundles {
				bundleNames[b.ID] = b.BundleName
			}
		}
	}

	schemaIDs := make([]uint64, 0)
	if len(verIDs) > 0 {
		var vers []models.ProtoFileVersion
		if err := db.Select("id, proto_file_id, schema_id").Where("id IN ?", verIDs).Find(&vers).Error; err == nil {
			for _, v := range vers {
				if v.SchemaID != nil && *v.SchemaID > 0 {
					latestVerSchemaByFile[v.ProtoFileID] = *v.SchemaID
					schemaIDs = append(schemaIDs, *v.SchemaID)
				}
			}
		}
	}
	if len(schemaIDs) > 0 {
		var schemas []models.ProtoSchema
		if err := db.Select("id, name, version, is_active").Where("id IN ?", schemaIDs).Find(&schemas).Error; err == nil {
			for _, s := range schemas {
				schemaByID[s.ID] = s
			}
		}
	}
	if len(targetNames) > 0 {
		var schemas []models.ProtoSchema
		if err := db.Select("id, name, version, is_active").
			Where("name IN ?", targetNames).
			Order("is_active DESC, id DESC").
			Find(&schemas).Error; err == nil {
			for _, s := range schemas {
				if _, ok := activeSchemaByName[s.Name]; !ok {
					activeSchemaByName[s.Name] = s
				}
			}
		}
	}
	return
}

// ListProtoFiles GET /api/proto-files
// 优先返回持久化只读 Git 源文件列表（traefik_proto_files），对无 Git 绑定的存量 Schema 无缝保留 M23 只读源码浏览兼容性（严禁任何悲观锁字段）
func ListProtoFiles(c *gin.Context) {
	page, pageSize := parseStandardPagination(c)

	gitRepoID, ok := parseOptionalUint64Query(c, "git_repo_id")
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 git_repo_id 参数"})
		return
	}
	bundleID, ok := parseOptionalUint64Query(c, "bundle_id")
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 bundle_id 参数"})
		return
	}
	targetSchema := strings.TrimSpace(c.Query("target_schema_name"))
	packageFilter := strings.TrimSpace(c.Query("package_name"))
	search := strings.TrimSpace(c.Query("search"))
	searchLower := strings.ToLower(search)

	includeDeleted := false
	if rawDel := strings.TrimSpace(c.Query("include_deleted")); rawDel != "" {
		parsed, err := strconv.ParseBool(rawDel)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 include_deleted 参数"})
			return
		}
		includeDeleted = parsed
	}

	q := models.DB.Model(&models.ProtoFile{})
	if gitRepoID != nil {
		q = q.Where("git_repo_id = ?", *gitRepoID)
	}
	if bundleID != nil {
		q = q.Where("bundle_id = ?", *bundleID)
	}
	if targetSchema != "" {
		q = q.Where("target_schema_name = ?", targetSchema)
	}
	if packageFilter != "" {
		q = q.Where("package_name = ?", packageFilter)
	}
	if !includeDeleted {
		q = q.Where("is_deleted = false")
	}
	if search != "" {
		like := "%" + search + "%"
		q = q.Where("target_schema_name ILIKE ? OR file_path ILIKE ? OR repo_file_path ILIKE ? OR package_name ILIKE ?",
			like, like, like, like)
	}

	var realFiles []models.ProtoFile
	if err := q.Order("target_schema_name ASC, file_path ASC").Find(&realFiles).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	repoNames, bundleNames, schemaByID, latestVerSchemaByFile, activeSchemaByName := resolveProtoFileRelations(models.DB, realFiles)
	allItems := make([]ProtoFileDTO, 0, len(realFiles))
	coveredSchemas := make(map[string]struct{})
	for _, rf := range realFiles {
		coveredSchemas[rf.TargetSchemaName] = struct{}{}
		allItems = append(allItems, buildGitProtoFileDTO(rf, repoNames, bundleNames, schemaByID, latestVerSchemaByFile, activeSchemaByName))
	}

	storageMode := "M25_GIT_SNAPSHOTS"
	if gitRepoID == nil && bundleID == nil && !includeDeleted {
		virtualDTOs, vErr := listVirtualProtoFileDTOs(models.DB, targetSchema, packageFilter, searchLower, coveredSchemas)
		if vErr == nil && len(virtualDTOs) > 0 {
			if len(allItems) == 0 {
				storageMode = "M23_DESCRIPTOR_VIRTUAL"
			} else {
				storageMode = "HYBRID_GIT_AND_DESCRIPTOR"
			}
			allItems = append(allItems, virtualDTOs...)
		}
	}

	sort.Slice(allItems, func(i, j int) bool {
		if allItems[i].TargetSchemaName != allItems[j].TargetSchemaName {
			return allItems[i].TargetSchemaName < allItems[j].TargetSchemaName
		}
		if allItems[i].FilePath != allItems[j].FilePath {
			return allItems[i].FilePath < allItems[j].FilePath
		}
		return allItems[i].ID < allItems[j].ID
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
		"storage_mode": storageMode,
	})
}

// ProtoViolationOverlayDTO 将静态规则扫描违规信息适配为代码查看器高亮波浪线格式（同时保留 file_line 与前端 line）
type ProtoViolationOverlayDTO struct {
	models.CheckViolation
	Line   int `json:"line"`
	Column int `json:"column"`
}

func loadProtoFileViolationOverlay(db *gorm.DB, pf models.ProtoFile, schemaID uint64) []ProtoViolationOverlayDTO {
	out := make([]ProtoViolationOverlayDTO, 0)
	if db == nil {
		return out
	}

	// 优先查找关联到该单文件或该 Bundle 或对应 Schema 的最新 CheckReport
	var report models.CheckReport
	found := false
	queries := []*gorm.DB{
		db.Where("proto_file_id = ?", pf.ID),
		db.Where("bundle_id = ?", pf.BundleID),
	}
	if schemaID > 0 {
		queries = append(queries, db.Where("schema_id = ?", schemaID))
	}
	for _, q := range queries {
		if err := q.Order("id DESC").Limit(1).First(&report).Error; err == nil && report.ID > 0 {
			found = true
			break
		}
	}
	if !found {
		return out
	}

	paths := []string{pf.FilePath}
	if pf.RepoFilePath != "" && pf.RepoFilePath != pf.FilePath {
		paths = append(paths, pf.RepoFilePath)
	}
	var raw []models.CheckViolation
	if err := db.Where("report_id = ? AND proto_file IN ?", report.ID, paths).
		Order("file_line ASC, id ASC").
		Find(&raw).Error; err != nil {
		return out
	}
	for _, v := range raw {
		line := v.FileLine
		if line <= 0 {
			line = 1
		}
		col := v.FileColumn
		if col <= 0 {
			col = 1
		}
		out = append(out, ProtoViolationOverlayDTO{
			CheckViolation: v,
			Line:           line,
			Column:         col,
		})
	}
	return out
}

func loadProtoFileSymbolAnchors(db *gorm.DB, pf models.ProtoFile, schemaID uint64) []protodoc.ProtoSymbolAnchorDTO {
	anchors := make([]protodoc.ProtoSymbolAnchorDTO, 0)
	if db == nil || schemaID == 0 {
		return anchors
	}

	var syms []models.ProtoDocSymbol
	paths := []string{pf.FilePath}
	if pf.RepoFilePath != "" && pf.RepoFilePath != pf.FilePath {
		paths = append(paths, pf.RepoFilePath)
	}
	if err := db.Where("schema_id = ? AND proto_file IN ?", schemaID, paths).
		Order("start_line ASC, id ASC").
		Find(&syms).Error; err != nil {
		return anchors
	}
	if len(syms) == 0 {
		var sc models.ProtoSchema
		if err := db.First(&sc, schemaID).Error; err == nil {
			protodoc.EnsureSymbolsForSchema(db, &sc)
			_ = db.Where("schema_id = ? AND proto_file IN ?", schemaID, paths).
				Order("start_line ASC, id ASC").
				Find(&syms).Error
		}
	}
	for _, s := range syms {
		anchors = append(anchors, protodoc.ProtoSymbolAnchorDTO{
			SymbolKind:     s.SymbolKind,
			SymbolFullName: s.FullName,
			StartLine:      s.StartLine,
			EndLine:        s.EndLine,
		})
	}
	return anchors
}

// GetProtoFileContent GET /api/proto-files/:id/content
// 返回只读源码正文、静态检查 Violation 波浪线叠加列表与文档/AST Symbol 行号锚点（支持 ?version= 回溯历史 Commit）
func GetProtoFileContent(c *gin.Context) {
	fileID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || fileID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 proto file ID"})
		return
	}

	var pf models.ProtoFile
	if err := models.DB.First(&pf, fileID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			getVirtualProtoFileContent(c, fileID)
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var ver models.ProtoFileVersion
	if verRaw := strings.TrimSpace(c.Query("version")); verRaw != "" {
		verNum, parseErr := strconv.Atoi(verRaw)
		if parseErr != nil || verNum <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 version 版本号参数"})
			return
		}
		if findErr := models.DB.Where("proto_file_id = ? AND version = ?", pf.ID, verNum).First(&ver).Error; findErr != nil {
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "指定版本文件不存在"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": findErr.Error()})
			return
		}
	} else {
		if pf.LatestVersionID != nil && *pf.LatestVersionID > 0 {
			err = models.DB.First(&ver, *pf.LatestVersionID).Error
		} else {
			err = models.DB.Where("proto_file_id = ?", pf.ID).Order("version DESC").First(&ver).Error
		}
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "该源文件尚未写入任何历史 Commit 快照"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	repoNames, bundleNames, schemaByID, latestVerSchemaByFile, activeSchemaByName := resolveProtoFileRelations(models.DB, []models.ProtoFile{pf})
	fileDTO := buildGitProtoFileDTO(pf, repoNames, bundleNames, schemaByID, latestVerSchemaByFile, activeSchemaByName)

	var targetSchemaID uint64
	if ver.SchemaID != nil && *ver.SchemaID > 0 {
		targetSchemaID = *ver.SchemaID
	} else {
		targetSchemaID = fileDTO.SchemaID
	}
	var activeSchemaID uint64
	if sc, ok := activeSchemaByName[pf.TargetSchemaName]; ok {
		activeSchemaID = sc.ID
	}

	violations := loadProtoFileViolationOverlay(models.DB, pf, targetSchemaID)
	anchors := loadProtoFileSymbolAnchors(models.DB, pf, targetSchemaID)

	c.JSON(http.StatusOK, gin.H{
		"file":           fileDTO,
		"version":        buildGitProtoFileVersionDTO(ver, activeSchemaID),
		"content":        ver.ContentText,
		"violations":     violations,
		"symbol_anchors": anchors,
	})
}

// ListProtoFileVersions GET /api/proto-files/:id/versions
// 标准分页返回不可变 Commit 历史摘要列表（绝不返回大文本 content 正文）
func ListProtoFileVersions(c *gin.Context) {
	fileID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || fileID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 proto file ID"})
		return
	}
	page, pageSize := parseStandardPagination(c)

	var pf models.ProtoFile
	if err := models.DB.First(&pf, fileID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			listVirtualProtoFileVersions(c, fileID, page, pageSize)
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var total int64
	q := models.DB.Model(&models.ProtoFileVersion{}).Where("proto_file_id = ?", pf.ID)
	if err := q.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var vers []models.ProtoFileVersion
	offset := (page - 1) * pageSize
	if err := q.Omit("content_text").Order("version DESC").Offset(offset).Limit(pageSize).Find(&vers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	_, _, _, _, activeSchemaByName := resolveProtoFileRelations(models.DB, []models.ProtoFile{pf})
	var activeSchemaID uint64
	if sc, ok := activeSchemaByName[pf.TargetSchemaName]; ok {
		activeSchemaID = sc.ID
	}

	items := make([]ProtoFileVersionSummaryDTO, 0, len(vers))
	for _, v := range vers {
		full := buildGitProtoFileVersionDTO(v, activeSchemaID)
		items = append(items, ProtoFileVersionSummaryDTO{
			ID:            full.ID,
			ProtoFileID:   full.ProtoFileID,
			SchemaID:      full.SchemaID,
			Version:       full.Version,
			VersionTag:    full.VersionTag,
			CommitSHA:     full.CommitSHA,
			CommitMessage: full.CommitMessage,
			ContentSHA:    full.ContentSHA,
			SourceType:    full.SourceType,
			AuthorName:    full.AuthorName,
			AuthorEmail:   full.AuthorEmail,
			Summary:       full.Summary,
			LineCount:     full.LineCount,
			IsActiveVer:   full.IsActiveVer,
			CommittedAt:   full.CommittedAt,
			CreatedAt:     full.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetProtoFileDiff GET /api/proto-files/:id/diff
// 纯只读比较历史 Commit 版本间的源码逐行差异并输出标准 Unified Diff
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

	var pf models.ProtoFile
	if err := models.DB.First(&pf, fileID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			getVirtualProtoFileDiff(c, fileID, fromParam, toParam)
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var metas []models.ProtoFileVersion
	if err := models.DB.Omit("content_text").
		Where("proto_file_id = ?", pf.ID).
		Order("version DESC").
		Find(&metas).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if len(metas) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "暂无可用文件版本历史"})
		return
	}

	toIdx := -1
	if toParam == "" || strings.EqualFold(toParam, "latest") {
		toIdx = 0
	} else {
		toVer, convErr := strconv.Atoi(toParam)
		if convErr != nil || toVer <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "非法的 to_version 参数"})
			return
		}
		for i, m := range metas {
			if m.Version == toVer {
				toIdx = i
				break
			}
		}
	}
	if toIdx < 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "目标对比版本 to_version 不存在"})
		return
	}

	fromIdx := -1
	if strings.EqualFold(fromParam, "prev") {
		if toIdx+1 < len(metas) {
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
		for i, m := range metas {
			if m.Version == fromVer {
				fromIdx = i
				break
			}
		}
	}
	if fromIdx < 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "源对比版本 from_version 不存在"})
		return
	}

	var fromVerFull, toVerFull models.ProtoFileVersion
	if err := models.DB.First(&fromVerFull, metas[fromIdx].ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if metas[toIdx].ID == metas[fromIdx].ID {
		toVerFull = fromVerFull
	} else if err := models.DB.First(&toVerFull, metas[toIdx].ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	repoNames, bundleNames, schemaByID, latestVerSchemaByFile, activeSchemaByName := resolveProtoFileRelations(models.DB, []models.ProtoFile{pf})
	var activeSchemaID uint64
	if sc, ok := activeSchemaByName[pf.TargetSchemaName]; ok {
		activeSchemaID = sc.ID
	}

	unifiedDiff, added, deleted := computeUnifiedDiff(
		fmt.Sprintf("%s@v%d", pf.FilePath, fromVerFull.Version),
		fmt.Sprintf("%s@v%d", pf.FilePath, toVerFull.Version),
		fromVerFull.ContentText,
		toVerFull.ContentText,
	)

	c.JSON(http.StatusOK, gin.H{
		"file":          buildGitProtoFileDTO(pf, repoNames, bundleNames, schemaByID, latestVerSchemaByFile, activeSchemaByName),
		"from_version":  buildGitProtoFileVersionDTO(fromVerFull, activeSchemaID),
		"to_version":    buildGitProtoFileVersionDTO(toVerFull, activeSchemaID),
		"unified_diff":  unifiedDiff,
		"added_lines":   added,
		"deleted_lines": deleted,
	})
}
