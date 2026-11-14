package gitclient

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"code-traefik/internal/server/decoder"
	"code-traefik/internal/server/protodoc"
	"code-traefik/models"

	"gorm.io/gorm"
)

const (
	MRChangeAdded    = "ADDED"
	MRChangeModified = "MODIFIED"
	MRChangeRenamed  = "RENAMED"
	MRChangeDeleted  = "DELETED"
)

// MRPatchFile MR/PR 单文件变更补丁项
type MRPatchFile struct {
	ChangeType string `json:"change_type"` // ADDED | MODIFIED | RENAMED | DELETED
	Path       string `json:"path,omitempty"`
	OldPath    string `json:"old_path,omitempty"`
	NewPath    string `json:"new_path,omitempty"`
	Content    string `json:"content,omitempty"`
}

func (p MRPatchFile) effectiveNewPath() string {
	if strings.TrimSpace(p.NewPath) != "" {
		return strings.TrimSpace(p.NewPath)
	}
	return strings.TrimSpace(p.Path)
}

func (p MRPatchFile) effectiveOldPath() string {
	if strings.TrimSpace(p.OldPath) != "" {
		return strings.TrimSpace(p.OldPath)
	}
	return strings.TrimSpace(p.Path)
}

// MatchBundleForRepoPath 判断仓库级路径是否归属于指定 Monorepo Bundle，并返回剥离 SubdirPath 后的相对路径
func MatchBundleForRepoPath(bundle *models.GitProtoBundle, rawRepoPath string) (string, bool, error) {
	if bundle == nil {
		return "", false, errors.New("GitProtoBundle 不能为空")
	}
	trimmed := strings.TrimSpace(rawRepoPath)
	if trimmed == "" || !strings.HasSuffix(trimmed, ".proto") {
		return "", false, nil
	}
	repoRel, err := decoder.SanitizeProtoRelPath(trimmed, true)
	if err != nil {
		return "", false, err
	}
	subdir, err := SanitizeSubdirPath(bundle.SubdirPath)
	if err != nil {
		return "", false, err
	}

	relPath := repoRel
	if subdir != "." {
		prefix := subdir + "/"
		if !strings.HasPrefix(repoRel, prefix) {
			return "", false, nil
		}
		relPath = strings.TrimPrefix(repoRel, prefix)
	}
	cleanRel, err := decoder.SanitizeProtoRelPath(relPath, true)
	if err != nil {
		return "", false, err
	}

	includeGlobs := bundle.FileGlobs()
	if len(includeGlobs) > 0 && !matchAnyProtoGlob(includeGlobs, cleanRel) {
		return "", false, nil
	}
	excludeGlobs := bundle.ExcludeGlobs()
	if len(excludeGlobs) > 0 && matchAnyProtoGlob(excludeGlobs, cleanRel) {
		return "", false, nil
	}
	return cleanRel, true, nil
}

// FilterAffectedBundles 筛选 MR 变更涉及的 Monorepo Bundles（按 BASE_LIB -> BUSINESS -> GATEWAY 排序）
func FilterAffectedBundles(bundles []models.GitProtoBundle, patchFiles []MRPatchFile) ([]models.GitProtoBundle, error) {
	if len(bundles) == 0 {
		return nil, nil
	}
	// 先校验所有补丁中的 .proto 路径不含路径穿越
	for _, pf := range patchFiles {
		for _, candidate := range []string{pf.Path, pf.OldPath, pf.NewPath} {
			c := strings.TrimSpace(candidate)
			if c == "" || !strings.HasSuffix(c, ".proto") {
				continue
			}
			if _, err := decoder.SanitizeProtoRelPath(c, true); err != nil {
				return nil, err
			}
		}
	}

	if len(patchFiles) == 0 {
		out := make([]models.GitProtoBundle, len(bundles))
		copy(out, bundles)
		sortBundlesByCategory(out)
		return out, nil
	}

	matched := make([]models.GitProtoBundle, 0, len(bundles))
	for _, b := range bundles {
		bCopy := b
		hit := false
		for _, pf := range patchFiles {
			for _, candidate := range []string{pf.effectiveOldPath(), pf.effectiveNewPath()} {
				if candidate == "" {
					continue
				}
				_, ok, err := MatchBundleForRepoPath(&bCopy, candidate)
				if err != nil {
					return nil, err
				}
				if ok {
					hit = true
					break
				}
			}
			if hit {
				break
			}
		}
		if hit {
			matched = append(matched, bCopy)
		}
	}
	sortBundlesByCategory(matched)
	return matched, nil
}

func sortBundlesByCategory(bundles []models.GitProtoBundle) {
	catOrder := func(cat string) int {
		switch cat {
		case models.ProtoCategoryBaseLib:
			return 0
		case models.ProtoCategoryBusiness:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(bundles, func(i, j int) bool {
		ci, cj := catOrder(bundles[i].Category), catOrder(bundles[j].Category)
		if ci != cj {
			return ci < cj
		}
		return bundles[i].ID < bundles[j].ID
	})
}

// BuildMRVirtualTree 构建「DB 目标分支活跃基线 + MR 变更补丁」的完整内存虚拟树（含 ADDED/MODIFIED/RENAMED/DELETED 语义）
func BuildMRVirtualTree(
	db *gorm.DB,
	bundle *models.GitProtoBundle,
	patchFiles []MRPatchFile,
) (map[string]string, []string, error) {
	if db == nil {
		return nil, nil, errors.New("db 不能为空")
	}
	if bundle == nil {
		return nil, nil, errors.New("bundle 不能为空")
	}

	virtualTree := make(map[string]string)

	// 1. 加载目标分支在 DB 中的活跃基线文件 (is_deleted = false)
	type baselineRow struct {
		FilePath    string
		ContentText string
	}
	var rows []baselineRow
	if err := db.Table("traefik_proto_files AS f").
		Select("f.file_path AS file_path, v.content_text AS content_text").
		Joins("JOIN traefik_proto_file_versions AS v ON v.id = f.latest_version_id").
		Where("f.bundle_id = ? AND f.is_deleted = false", bundle.ID).
		Order("f.file_path ASC").
		Find(&rows).Error; err != nil {
		return nil, nil, fmt.Errorf("读取 Bundle 基线文件失败: %w", err)
	}
	for _, r := range rows {
		cleaned, err := decoder.SanitizeProtoRelPath(r.FilePath, true)
		if err != nil {
			continue
		}
		virtualTree[cleaned] = r.ContentText
	}

	// 若 traefik_proto_files 尚未落库但 Bundle 已关联既有 ProtoSchema（或同名活跃 Schema），从描述符重建基线文件兜底
	if len(virtualTree) == 0 {
		var baseSchema models.ProtoSchema
		var err error
		if bundle.LastSchemaID != nil && *bundle.LastSchemaID > 0 {
			err = db.First(&baseSchema, *bundle.LastSchemaID).Error
		} else if bundle.TargetSchemaName != "" {
			err = db.Where("name = ? AND is_active = true", bundle.TargetSchemaName).First(&baseSchema).Error
		}
		if err == nil && baseSchema.ID > 0 {
			protodoc.EnsureSymbolsForSchema(db, &baseSchema)
			var syms []models.ProtoDocSymbol
			if sErr := db.Where("schema_id = ?", baseSchema.ID).Find(&syms).Error; sErr == nil {
				if reconstructed, rErr := protodoc.ReconstructProtoSources(&baseSchema, syms); rErr == nil {
					for _, rf := range reconstructed {
						if cleaned, cErr := decoder.SanitizeProtoRelPath(rf.FilePath, true); cErr == nil {
							virtualTree[cleaned] = rf.Content
						}
					}
				}
			}
		}
	}

	// 2. 叠加 MR 变更补丁（ADDED / MODIFIED / RENAMED / DELETED）
	changedSet := make(map[string]struct{})
	for _, pf := range patchFiles {
		changeType := strings.ToUpper(strings.TrimSpace(pf.ChangeType))
		if changeType == "" {
			changeType = MRChangeModified
		}

		switch changeType {
		case MRChangeDeleted:
			targetRaw := pf.effectiveOldPath()
			if targetRaw == "" {
				targetRaw = pf.effectiveNewPath()
			}
			relPath, matched, err := MatchBundleForRepoPath(bundle, targetRaw)
			if err != nil {
				return nil, nil, err
			}
			if !matched {
				// 兼容传入已经是 Bundle 相对路径的情况
				if relTry, rErr := decoder.SanitizeProtoRelPath(targetRaw, true); rErr == nil {
					if _, exists := virtualTree[relTry]; exists {
						relPath = relTry
						matched = true
					}
				}
			}
			if matched {
				delete(virtualTree, relPath)
				changedSet[relPath] = struct{}{}
			}

		case MRChangeRenamed:
			oldRaw := pf.effectiveOldPath()
			if oldRaw != "" {
				oldRel, oldMatched, err := MatchBundleForRepoPath(bundle, oldRaw)
				if err != nil {
					return nil, nil, err
				}
				if !oldMatched {
					if relTry, rErr := decoder.SanitizeProtoRelPath(oldRaw, true); rErr == nil {
						if _, exists := virtualTree[relTry]; exists {
							oldRel = relTry
							oldMatched = true
						}
					}
				}
				if oldMatched {
					delete(virtualTree, oldRel)
					changedSet[oldRel] = struct{}{}
				}
			}

			newRaw := pf.effectiveNewPath()
			if newRaw != "" {
				newRel, newMatched, err := MatchBundleForRepoPath(bundle, newRaw)
				if err != nil {
					return nil, nil, err
				}
				if newMatched {
					virtualTree[newRel] = pf.Content
					changedSet[newRel] = struct{}{}
				}
			}

		default: // ADDED | MODIFIED
			newRaw := pf.effectiveNewPath()
			if newRaw == "" {
				continue
			}
			newRel, matched, err := MatchBundleForRepoPath(bundle, newRaw)
			if err != nil {
				return nil, nil, err
			}
			if !matched {
				// 若补丁本身已是相对于 Bundle 根目录的合法路径且 Bundle SubdirPath="."，MatchBundleForRepoPath 已覆盖；
				// 若 SubdirPath != "." 但调用方直接提供了已在 virtualTree 内的相对路径，也允许匹配
				if relTry, rErr := decoder.SanitizeProtoRelPath(newRaw, true); rErr == nil {
					if _, exists := virtualTree[relTry]; exists {
						newRel = relTry
						matched = true
					}
				}
			}
			if matched {
				virtualTree[newRel] = pf.Content
				changedSet[newRel] = struct{}{}
			}
		}
	}

	var totalBytes int64
	for _, content := range virtualTree {
		totalBytes += int64(len(content))
		if totalBytes > int64(decoder.MaxDescriptorFileSize) {
			return nil, nil, fmt.Errorf("MR 虚拟树 .proto 总大小超过 16MB (%d Bytes) 上限", decoder.MaxDescriptorFileSize)
		}
	}

	changedFiles := make([]string, 0, len(changedSet))
	for p := range changedSet {
		changedFiles = append(changedFiles, p)
	}
	sort.Strings(changedFiles)
	return virtualTree, changedFiles, nil
}
