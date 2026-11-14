package gitclient

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"code-traefik/internal/server/decoder"
	"code-traefik/internal/server/security"
	"code-traefik/models"
)

const (
	defaultRESTTimeout   = 25 * time.Second
	maxGitLabTreePages   = 100
	maxRESTResponseBytes = int64(decoder.MaxDescriptorFileSize) + 1024*1024
)

var ErrRESTFallbackAllowed = errors.New("REST 接口不支持该仓库地址，可回退至 memfs 协议拉取")

// RemoteProtoFile 远端拉取到的单个只读 .proto 文件快照
type RemoteProtoFile struct {
	RepoFilePath string // 相对于 Git 仓库根目录的规范 POSIX 路径
	FilePath     string // 可选归一化后路径
	Content      []byte
	SHA256       string
	Size         int
}

// RemoteCommitSnapshot 远端指定分支 HEAD 的无状态只读源码快照
type RemoteCommitSnapshot struct {
	CommitSHA     string
	CommitMessage string
	AuthorName    string
	AuthorEmail   string
	CommittedAt   time.Time
	Files         []RemoteProtoFile
}

type restRepoCoords struct {
	APIBase   string
	OwnerRepo string
}

func parseRESTRepoCoords(repo *models.GitRepo) (*restRepoCoords, error) {
	if repo == nil {
		return nil, errors.New("GitRepo 不能为空")
	}
	rawRepo := strings.TrimSpace(repo.RepoURL)
	if rawRepo == "" {
		return nil, errors.New("repo_url 不能为空")
	}
	u, err := url.Parse(rawRepo)
	if err != nil {
		return nil, fmt.Errorf("解析 repo_url 失败: %w", err)
	}
	ownerRepo := strings.Trim(u.Path, "/")
	ownerRepo = strings.TrimSuffix(ownerRepo, ".git")
	if ownerRepo == "" {
		return nil, fmt.Errorf("无法从 repo_url 解析仓库路径: %s", rawRepo)
	}

	var apiBase string
	if strings.TrimSpace(repo.APIBaseURL) != "" {
		rawAPI := strings.TrimRight(strings.TrimSpace(repo.APIBaseURL), "/")
		au, aErr := url.Parse(rawAPI)
		if aErr != nil {
			return nil, fmt.Errorf("解析 api_base_url 失败: %w", aErr)
		}
		if au.Path == "" || au.Path == "/" {
			switch repo.Provider {
			case models.GitProviderGitLab:
				apiBase = rawAPI + "/api/v4"
			case models.GitProviderGitea:
				apiBase = rawAPI + "/api/v1"
			default:
				apiBase = rawAPI
			}
		} else {
			apiBase = rawAPI
		}
	} else {
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, fmt.Errorf("无 api_base_url 且 repo_url 非 HTTP(S) 协议: %s", u.Scheme)
		}
		origin := u.Scheme + "://" + u.Host
		switch repo.Provider {
		case models.GitProviderGitHub:
			if strings.EqualFold(u.Hostname(), "github.com") {
				apiBase = "https://api.github.com"
			} else {
				apiBase = origin + "/api/v3"
			}
		case models.GitProviderGitea:
			apiBase = origin + "/api/v1"
		default:
			apiBase = origin + "/api/v4"
		}
	}

	return &restRepoCoords{
		APIBase:   apiBase,
		OwnerRepo: ownerRepo,
	}, nil
}

func newGuardedHTTPClient(dialer *security.SafeDialer, timeout time.Duration) *http.Client {
	client := dialer.HTTPClient(timeout)
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("REST 请求重定向次数过多")
		}
		return dialer.ValidateURL(req.Context(), req.URL.String())
	}
	return client
}

func doSafeRESTRequest(
	ctx context.Context,
	client *http.Client,
	dialer *security.SafeDialer,
	provider string,
	creds string,
	rawURL string,
	acceptHeader string,
) ([]byte, http.Header, int, error) {
	if err := dialer.ValidateURL(ctx, rawURL); err != nil {
		return nil, nil, 0, fmt.Errorf("SSRF 防护拦截出站 Git REST 请求: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, 0, err
	}
	if acceptHeader != "" {
		req.Header.Set("Accept", acceptHeader)
	}
	if strings.TrimSpace(creds) != "" {
		switch provider {
		case models.GitProviderGitLab:
			req.Header.Set("PRIVATE-TOKEN", creds)
		case models.GitProviderGitea:
			req.Header.Set("Authorization", "token "+creds)
		default:
			req.Header.Set("Authorization", "Bearer "+creds)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, 0, err
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, maxRESTResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, resp.Header, resp.StatusCode, err
	}
	if int64(len(body)) > maxRESTResponseBytes {
		return nil, resp.Header, resp.StatusCode, fmt.Errorf("远端响应体积超过 %d 字节上限", maxRESTResponseBytes)
	}
	return body, resp.Header, resp.StatusCode, nil
}

func fetchViaRESTAPI(
	ctx context.Context,
	dialer *security.SafeDialer,
	repo *models.GitRepo,
	creds string,
	branch string,
	subdir string,
) (*RemoteCommitSnapshot, error) {
	coords, err := parseRESTRepoCoords(repo)
	if err != nil {
		return nil, err
	}
	if branch == "" {
		branch = repo.DefaultBranch
		if branch == "" {
			branch = "main"
		}
	}
	client := newGuardedHTTPClient(dialer, defaultRESTTimeout)
	defer client.CloseIdleConnections()

	switch repo.Provider {
	case models.GitProviderGitLab:
		return fetchGitLabREST(ctx, client, dialer, coords, creds, branch, subdir)
	case models.GitProviderGitHub:
		return fetchGitHubREST(ctx, client, dialer, coords, creds, branch, subdir)
	case models.GitProviderGitea:
		return fetchGiteaREST(ctx, client, dialer, coords, creds, branch, subdir)
	default:
		return nil, fmt.Errorf("%w: provider=%s", ErrRESTFallbackAllowed, repo.Provider)
	}
}

func listBranchesViaREST(
	ctx context.Context,
	dialer *security.SafeDialer,
	repo *models.GitRepo,
	creds string,
) ([]string, error) {
	coords, err := parseRESTRepoCoords(repo)
	if err != nil {
		return nil, err
	}
	client := newGuardedHTTPClient(dialer, defaultRESTTimeout)
	defer client.CloseIdleConnections()

	var endpoint string
	switch repo.Provider {
	case models.GitProviderGitLab:
		endpoint = fmt.Sprintf("%s/projects/%s/repository/branches?per_page=100",
			coords.APIBase, url.PathEscape(coords.OwnerRepo))
	case models.GitProviderGitHub, models.GitProviderGitea:
		endpoint = fmt.Sprintf("%s/repos/%s/branches?per_page=100",
			coords.APIBase, coords.OwnerRepo)
	default:
		return nil, fmt.Errorf("%w: provider=%s", ErrRESTFallbackAllowed, repo.Provider)
	}

	body, _, status, err := doSafeRESTRequest(ctx, client, dialer, repo.Provider, creds, endpoint, "application/json")
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound && strings.TrimSpace(repo.APIBaseURL) == "" {
		return nil, fmt.Errorf("%w: HTTP %d", ErrRESTFallbackAllowed, status)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("查询远端分支失败 (HTTP %d): %s", status, truncateMsg(string(body), 240))
	}

	var rows []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("解析远端分支响应失败: %w", err)
	}
	branches := make([]string, 0, len(rows))
	for _, r := range rows {
		if strings.TrimSpace(r.Name) != "" {
			branches = append(branches, r.Name)
		}
	}
	sort.Strings(branches)
	return branches, nil
}

func fetchGitLabREST(
	ctx context.Context,
	client *http.Client,
	dialer *security.SafeDialer,
	coords *restRepoCoords,
	creds string,
	branch string,
	subdir string,
) (*RemoteCommitSnapshot, error) {
	encodedProject := url.PathEscape(coords.OwnerRepo)

	// 1. 获取目标分支最新 Commit 元数据
	commitURL := fmt.Sprintf("%s/projects/%s/repository/commits/%s",
		coords.APIBase, encodedProject, url.PathEscape(branch))
	commitBody, _, status, err := doSafeRESTRequest(ctx, client, dialer, models.GitProviderGitLab, creds, commitURL, "application/json")
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, fmt.Errorf("%w: GitLab commit/branch %q 不存在 (HTTP 404)", ErrRESTFallbackAllowed, branch)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("查询 GitLab Commit 失败 (HTTP %d): %s", status, truncateMsg(string(commitBody), 240))
	}
	var commitInfo struct {
		ID            string    `json:"id"`
		Message       string    `json:"message"`
		Title         string    `json:"title"`
		AuthorName    string    `json:"author_name"`
		AuthorEmail   string    `json:"author_email"`
		CommittedDate time.Time `json:"committed_date"`
	}
	if err := json.Unmarshal(commitBody, &commitInfo); err != nil {
		return nil, fmt.Errorf("解析 GitLab Commit 失败: %w", err)
	}
	commitSHA := strings.TrimSpace(commitInfo.ID)
	if commitSHA == "" {
		commitSHA = branch
	}
	msg := strings.TrimSpace(commitInfo.Message)
	if msg == "" {
		msg = strings.TrimSpace(commitInfo.Title)
	}
	committedAt := commitInfo.CommittedDate
	if committedAt.IsZero() {
		committedAt = time.Now().UTC()
	}

	// 2. 分页拉取递归目录树（只筛选 .proto 文件路径，非 .proto 直接跳过）
	type gitlabTreeNode struct {
		Type string `json:"type"`
		Path string `json:"path"`
	}
	cleanSubdir := strings.Trim(strings.TrimSpace(subdir), "/")
	if cleanSubdir == "." {
		cleanSubdir = ""
	}

	var protoPaths []string
	page := 1
	for page <= maxGitLabTreePages {
		q := url.Values{}
		q.Set("ref", commitSHA)
		q.Set("recursive", "true")
		q.Set("per_page", "100")
		q.Set("page", strconv.Itoa(page))
		if cleanSubdir != "" {
			q.Set("path", cleanSubdir)
		}
		treeURL := fmt.Sprintf("%s/projects/%s/repository/tree?%s", coords.APIBase, encodedProject, q.Encode())
		treeBody, hdr, tStatus, tErr := doSafeRESTRequest(ctx, client, dialer, models.GitProviderGitLab, creds, treeURL, "application/json")
		if tErr != nil {
			return nil, tErr
		}
		if tStatus < 200 || tStatus >= 300 {
			return nil, fmt.Errorf("读取 GitLab 文件树失败 (HTTP %d): %s", tStatus, truncateMsg(string(treeBody), 240))
		}
		var nodes []gitlabTreeNode
		if err := json.Unmarshal(treeBody, &nodes); err != nil {
			return nil, fmt.Errorf("解析 GitLab 文件树失败: %w", err)
		}
		for _, n := range nodes {
			if n.Type != "blob" || !strings.HasSuffix(n.Path, ".proto") {
				continue
			}
			sanitized, sErr := decoder.SanitizeProtoRelPath(n.Path, true)
			if sErr != nil {
				return nil, sErr
			}
			protoPaths = append(protoPaths, sanitized)
		}
		nextPage := strings.TrimSpace(hdr.Get("X-Next-Page"))
		if nextPage == "" {
			break
		}
		parsedNext, parseErr := strconv.Atoi(nextPage)
		if parseErr != nil || parsedNext <= page {
			break
		}
		page = parsedNext
	}

	sort.Strings(protoPaths)
	files := make([]RemoteProtoFile, 0, len(protoPaths))
	var totalBytes int64
	for _, p := range protoPaths {
		rawURL := fmt.Sprintf("%s/projects/%s/repository/files/%s/raw?ref=%s",
			coords.APIBase, encodedProject, url.PathEscape(p), url.QueryEscape(commitSHA))
		rawBody, _, fStatus, fErr := doSafeRESTRequest(ctx, client, dialer, models.GitProviderGitLab, creds, rawURL, "text/plain")
		if fErr != nil {
			return nil, fErr
		}
		if fStatus == http.StatusNotFound {
			// 部分 GitLab 部署或代理对 /raw 有限制时回退 JSON base64 端点
			jsonURL := fmt.Sprintf("%s/projects/%s/repository/files/%s?ref=%s",
				coords.APIBase, encodedProject, url.PathEscape(p), url.QueryEscape(commitSHA))
			jb, _, jStatus, jErr := doSafeRESTRequest(ctx, client, dialer, models.GitProviderGitLab, creds, jsonURL, "application/json")
			if jErr != nil {
				return nil, jErr
			}
			if jStatus < 200 || jStatus >= 300 {
				return nil, fmt.Errorf("读取 GitLab 文件 %s 失败 (HTTP %d)", p, jStatus)
			}
			var fileJSON struct {
				Content  string `json:"content"`
				Encoding string `json:"encoding"`
			}
			if err := json.Unmarshal(jb, &fileJSON); err != nil {
				return nil, err
			}
			if strings.EqualFold(fileJSON.Encoding, "base64") {
				decoded, decErr := base64.StdEncoding.DecodeString(strings.ReplaceAll(fileJSON.Content, "\n", ""))
				if decErr != nil {
					return nil, decErr
				}
				rawBody = decoded
			} else {
				rawBody = []byte(fileJSON.Content)
			}
		} else if fStatus < 200 || fStatus >= 300 {
			return nil, fmt.Errorf("读取 GitLab 文件 %s 失败 (HTTP %d)", p, fStatus)
		}

		totalBytes += int64(len(rawBody))
		if totalBytes > int64(decoder.MaxDescriptorFileSize) {
			return nil, fmt.Errorf("远端 .proto 源码总大小超出 16MB (%d Bytes) 安全限制", decoder.MaxDescriptorFileSize)
		}
		sum := sha256.Sum256(rawBody)
		files = append(files, RemoteProtoFile{
			RepoFilePath: p,
			FilePath:     p,
			Content:      rawBody,
			SHA256:       hex.EncodeToString(sum[:]),
			Size:         len(rawBody),
		})
	}

	return &RemoteCommitSnapshot{
		CommitSHA:     commitSHA,
		CommitMessage: msg,
		AuthorName:    commitInfo.AuthorName,
		AuthorEmail:   commitInfo.AuthorEmail,
		CommittedAt:   committedAt,
		Files:         files,
	}, nil
}

func fetchGitHubREST(
	ctx context.Context,
	client *http.Client,
	dialer *security.SafeDialer,
	coords *restRepoCoords,
	creds string,
	branch string,
	subdir string,
) (*RemoteCommitSnapshot, error) {
	commitURL := fmt.Sprintf("%s/repos/%s/commits/%s",
		coords.APIBase, coords.OwnerRepo, url.PathEscape(branch))
	commitBody, _, status, err := doSafeRESTRequest(ctx, client, dialer, models.GitProviderGitHub, creds, commitURL, "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, fmt.Errorf("%w: GitHub commit/branch %q 不存在 (HTTP 404)", ErrRESTFallbackAllowed, branch)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("查询 GitHub Commit 失败 (HTTP %d): %s", status, truncateMsg(string(commitBody), 240))
	}
	var commitInfo struct {
		SHA    string `json:"sha"`
		Commit struct {
			Message string `json:"message"`
			Author  struct {
				Name  string    `json:"name"`
				Email string    `json:"email"`
				Date  time.Time `json:"date"`
			} `json:"author"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(commitBody, &commitInfo); err != nil {
		return nil, fmt.Errorf("解析 GitHub Commit 失败: %w", err)
	}
	commitSHA := strings.TrimSpace(commitInfo.SHA)
	if commitSHA == "" {
		commitSHA = branch
	}
	committedAt := commitInfo.Commit.Author.Date
	if committedAt.IsZero() {
		committedAt = time.Now().UTC()
	}

	treeURL := fmt.Sprintf("%s/repos/%s/git/trees/%s?recursive=1",
		coords.APIBase, coords.OwnerRepo, url.PathEscape(commitSHA))
	treeBody, _, tStatus, tErr := doSafeRESTRequest(ctx, client, dialer, models.GitProviderGitHub, creds, treeURL, "application/vnd.github+json")
	if tErr != nil {
		return nil, tErr
	}
	if tStatus < 200 || tStatus >= 300 {
		return nil, fmt.Errorf("读取 GitHub Git Tree 失败 (HTTP %d): %s", tStatus, truncateMsg(string(treeBody), 240))
	}
	var treeResp struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
			SHA  string `json:"sha"`
			Size int64  `json:"size"`
		} `json:"tree"`
	}
	if err := json.Unmarshal(treeBody, &treeResp); err != nil {
		return nil, err
	}

	cleanSubdir := strings.Trim(strings.TrimSpace(subdir), "/")
	if cleanSubdir == "." {
		cleanSubdir = ""
	}

	type targetBlob struct {
		path string
		sha  string
	}
	var targets []targetBlob
	for _, n := range treeResp.Tree {
		if n.Type != "blob" || !strings.HasSuffix(n.Path, ".proto") {
			continue
		}
		if cleanSubdir != "" && !strings.HasPrefix(n.Path, cleanSubdir+"/") {
			continue
		}
		sanitized, sErr := decoder.SanitizeProtoRelPath(n.Path, true)
		if sErr != nil {
			return nil, sErr
		}
		targets = append(targets, targetBlob{path: sanitized, sha: n.SHA})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].path < targets[j].path })

	files := make([]RemoteProtoFile, 0, len(targets))
	var totalBytes int64
	for _, tb := range targets {
		var rawBytes []byte
		if tb.sha != "" {
			blobURL := fmt.Sprintf("%s/repos/%s/git/blobs/%s", coords.APIBase, coords.OwnerRepo, url.PathEscape(tb.sha))
			bb, _, bStatus, bErr := doSafeRESTRequest(ctx, client, dialer, models.GitProviderGitHub, creds, blobURL, "application/vnd.github+json")
			if bErr != nil {
				return nil, bErr
			}
			if bStatus < 200 || bStatus >= 300 {
				return nil, fmt.Errorf("读取 GitHub Blob %s 失败 (HTTP %d)", tb.path, bStatus)
			}
			var blobJSON struct {
				Content  string `json:"content"`
				Encoding string `json:"encoding"`
			}
			if err := json.Unmarshal(bb, &blobJSON); err != nil {
				return nil, err
			}
			if strings.EqualFold(blobJSON.Encoding, "base64") {
				decoded, decErr := base64.StdEncoding.DecodeString(strings.ReplaceAll(blobJSON.Content, "\n", ""))
				if decErr != nil {
					return nil, decErr
				}
				rawBytes = decoded
			} else {
				rawBytes = []byte(blobJSON.Content)
			}
		} else {
			contentURL := fmt.Sprintf("%s/repos/%s/contents/%s?ref=%s",
				coords.APIBase, coords.OwnerRepo, tb.path, url.QueryEscape(commitSHA))
			cb, _, cStatus, cErr := doSafeRESTRequest(ctx, client, dialer, models.GitProviderGitHub, creds, contentURL, "application/vnd.github.raw")
			if cErr != nil {
				return nil, cErr
			}
			if cStatus < 200 || cStatus >= 300 {
				return nil, fmt.Errorf("读取 GitHub 文件 %s 失败 (HTTP %d)", tb.path, cStatus)
			}
			rawBytes = cb
		}

		totalBytes += int64(len(rawBytes))
		if totalBytes > int64(decoder.MaxDescriptorFileSize) {
			return nil, fmt.Errorf("远端 .proto 源码总大小超出 16MB (%d Bytes) 安全限制", decoder.MaxDescriptorFileSize)
		}
		sum := sha256.Sum256(rawBytes)
		files = append(files, RemoteProtoFile{
			RepoFilePath: tb.path,
			FilePath:     tb.path,
			Content:      rawBytes,
			SHA256:       hex.EncodeToString(sum[:]),
			Size:         len(rawBytes),
		})
	}

	return &RemoteCommitSnapshot{
		CommitSHA:     commitSHA,
		CommitMessage: strings.TrimSpace(commitInfo.Commit.Message),
		AuthorName:    commitInfo.Commit.Author.Name,
		AuthorEmail:   commitInfo.Commit.Author.Email,
		CommittedAt:   committedAt,
		Files:         files,
	}, nil
}

func fetchGiteaREST(
	ctx context.Context,
	client *http.Client,
	dialer *security.SafeDialer,
	coords *restRepoCoords,
	creds string,
	branch string,
	subdir string,
) (*RemoteCommitSnapshot, error) {
	branchURL := fmt.Sprintf("%s/repos/%s/branches/%s",
		coords.APIBase, coords.OwnerRepo, url.PathEscape(branch))
	branchBody, _, status, err := doSafeRESTRequest(ctx, client, dialer, models.GitProviderGitea, creds, branchURL, "application/json")
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, fmt.Errorf("%w: Gitea branch %q 不存在 (HTTP 404)", ErrRESTFallbackAllowed, branch)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("查询 Gitea Branch 失败 (HTTP %d): %s", status, truncateMsg(string(branchBody), 240))
	}
	var branchInfo struct {
		Commit struct {
			ID        string    `json:"id"`
			Message   string    `json:"message"`
			Timestamp time.Time `json:"timestamp"`
			Author    struct {
				Name  string `json:"name"`
				Email string `json:"email"`
			} `json:"author"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(branchBody, &branchInfo); err != nil {
		return nil, fmt.Errorf("解析 Gitea Branch 失败: %w", err)
	}
	commitSHA := strings.TrimSpace(branchInfo.Commit.ID)
	if commitSHA == "" {
		commitSHA = branch
	}
	committedAt := branchInfo.Commit.Timestamp
	if committedAt.IsZero() {
		committedAt = time.Now().UTC()
	}

	treeURL := fmt.Sprintf("%s/repos/%s/git/trees/%s?recursive=1",
		coords.APIBase, coords.OwnerRepo, url.PathEscape(commitSHA))
	treeBody, _, tStatus, tErr := doSafeRESTRequest(ctx, client, dialer, models.GitProviderGitea, creds, treeURL, "application/json")
	if tErr != nil {
		return nil, tErr
	}
	if tStatus < 200 || tStatus >= 300 {
		return nil, fmt.Errorf("读取 Gitea Git Tree 失败 (HTTP %d): %s", tStatus, truncateMsg(string(treeBody), 240))
	}
	var treeResp struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
			SHA  string `json:"sha"`
		} `json:"tree"`
	}
	if err := json.Unmarshal(treeBody, &treeResp); err != nil {
		return nil, err
	}

	cleanSubdir := strings.Trim(strings.TrimSpace(subdir), "/")
	if cleanSubdir == "." {
		cleanSubdir = ""
	}
	var protoPaths []string
	for _, n := range treeResp.Tree {
		if n.Type != "blob" || !strings.HasSuffix(n.Path, ".proto") {
			continue
		}
		if cleanSubdir != "" && !strings.HasPrefix(n.Path, cleanSubdir+"/") {
			continue
		}
		sanitized, sErr := decoder.SanitizeProtoRelPath(n.Path, true)
		if sErr != nil {
			return nil, sErr
		}
		protoPaths = append(protoPaths, sanitized)
	}
	sort.Strings(protoPaths)

	files := make([]RemoteProtoFile, 0, len(protoPaths))
	var totalBytes int64
	for _, p := range protoPaths {
		rawURL := fmt.Sprintf("%s/repos/%s/raw/%s/%s",
			coords.APIBase, coords.OwnerRepo, url.PathEscape(commitSHA), p)
		rawBody, _, fStatus, fErr := doSafeRESTRequest(ctx, client, dialer, models.GitProviderGitea, creds, rawURL, "text/plain")
		if fErr != nil {
			return nil, fErr
		}
		if fStatus < 200 || fStatus >= 300 {
			return nil, fmt.Errorf("读取 Gitea 文件 %s 失败 (HTTP %d)", p, fStatus)
		}
		totalBytes += int64(len(rawBody))
		if totalBytes > int64(decoder.MaxDescriptorFileSize) {
			return nil, fmt.Errorf("远端 .proto 源码总大小超出 16MB (%d Bytes) 安全限制", decoder.MaxDescriptorFileSize)
		}
		sum := sha256.Sum256(rawBody)
		files = append(files, RemoteProtoFile{
			RepoFilePath: p,
			FilePath:     p,
			Content:      rawBody,
			SHA256:       hex.EncodeToString(sum[:]),
			Size:         len(rawBody),
		})
	}

	return &RemoteCommitSnapshot{
		CommitSHA:     commitSHA,
		CommitMessage: strings.TrimSpace(branchInfo.Commit.Message),
		AuthorName:    branchInfo.Commit.Author.Name,
		AuthorEmail:   branchInfo.Commit.Author.Email,
		CommittedAt:   committedAt,
		Files:         files,
	}, nil
}

func truncateMsg(s string, max int) string {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) <= max {
		return trimmed
	}
	return trimmed[:max] + "..."
}
