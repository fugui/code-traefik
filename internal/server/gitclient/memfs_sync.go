package gitclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"code-traefik/internal/server/decoder"
	"code-traefik/internal/server/security"
	"code-traefik/models"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage/memory"
)

type safeDialerCtxKey struct{}

type memfsTransportState struct {
	dialer *security.SafeDialer
	tr     *http.Transport
}

type contextSafeRoundTripper struct{}

func (rt *contextSafeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	state, ok := req.Context().Value(safeDialerCtxKey{}).(*memfsTransportState)
	if !ok || state == nil || state.dialer == nil || state.tr == nil {
		return nil, errors.New("SSRF 防护阻断: go-git HTTP 上下文未绑定 SafeDialer")
	}
	if err := state.dialer.ValidateURL(req.Context(), req.URL.String()); err != nil {
		return nil, fmt.Errorf("SSRF 防护拦截 go-git 出站请求: %w", err)
	}
	return state.tr.RoundTrip(req)
}

var installGitHTTPTransportOnce sync.Once

func ensureSafeGitHTTPTransport() {
	installGitHTTPTransportOnce.Do(func() {
		httpClient := &http.Client{
			Timeout:   45 * time.Second,
			Transport: &contextSafeRoundTripper{},
		}
		customTransport := githttp.NewClient(httpClient)
		client.InstallProtocol("http", customTransport)
		client.InstallProtocol("https", customTransport)
	})
}

func withSafeGitContext(ctx context.Context, dialer *security.SafeDialer) (context.Context, func()) {
	ensureSafeGitHTTPTransport()
	tr := &http.Transport{
		DialContext:           dialer.DialContext,
		MaxIdleConns:          16,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	state := &memfsTransportState{
		dialer: dialer,
		tr:     tr,
	}
	return context.WithValue(ctx, safeDialerCtxKey{}, state), tr.CloseIdleConnections
}

func validateAndBuildGitAuth(
	ctx context.Context,
	dialer *security.SafeDialer,
	repo *models.GitRepo,
	creds string,
) (transport.AuthMethod, error) {
	rawURL := strings.TrimSpace(repo.RepoURL)
	if rawURL == "" {
		return nil, errors.New("repo_url 不能为空")
	}

	// 支持 ssh:// 和 git@host:path 形式并强制走 SafeDialer 地址校验
	isSCPSSH := strings.Contains(rawURL, "@") && strings.Contains(rawURL, ":") && !strings.Contains(rawURL, "://")
	if isSCPSSH {
		atIdx := strings.Index(rawURL, "@")
		colonIdx := strings.Index(rawURL[atIdx+1:], ":")
		if colonIdx <= 0 {
			return nil, errors.New("非法的 SSH Git 仓库地址格式")
		}
		host := rawURL[atIdx+1 : atIdx+1+colonIdx]
		if err := dialer.CheckAddress(ctx, host, "22"); err != nil {
			return nil, fmt.Errorf("SSRF 防护拦截 SSH 出站连接: %w", err)
		}
	} else {
		u, err := url.Parse(rawURL)
		if err != nil {
			return nil, fmt.Errorf("解析 Git repo_url 失败: %w", err)
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https":
			if err := dialer.ValidateURL(ctx, rawURL); err != nil {
				return nil, fmt.Errorf("SSRF 防护拦截 Git 出站连接: %w", err)
			}
		case "ssh":
			host := u.Hostname()
			port := u.Port()
			if port == "" {
				port = "22"
			}
			if err := dialer.CheckAddress(ctx, host, port); err != nil {
				return nil, fmt.Errorf("SSRF 防护拦截 SSH 出站连接: %w", err)
			}
		default:
			return nil, fmt.Errorf("出于安全限制，仅允许 http/https/ssh 协议仓库，拒绝 scheme=%q", u.Scheme)
		}
	}

	switch repo.AuthType {
	case models.GitAuthNone, "":
		return nil, nil
	case models.GitAuthToken:
		if strings.TrimSpace(creds) == "" {
			return nil, nil
		}
		username := "oauth2"
		if repo.Provider == models.GitProviderGitHub {
			username = "x-access-token"
		}
		return &githttp.BasicAuth{
			Username: username,
			Password: creds,
		}, nil
	case models.GitAuthSSHKey:
		if strings.TrimSpace(creds) == "" {
			return nil, errors.New("auth_type=SSH_KEY 时必须配置 SSH 私钥凭证")
		}
		pubKeys, err := gitssh.NewPublicKeys("git", []byte(creds), "")
		if err != nil {
			return nil, fmt.Errorf("解析 SSH 私钥失败: %w", err)
		}
		return pubKeys, nil
	default:
		return nil, fmt.Errorf("不支持的 Git 鉴权模式: %s", repo.AuthType)
	}
}

func fetchViaMemFS(
	ctx context.Context,
	dialer *security.SafeDialer,
	repo *models.GitRepo,
	creds string,
	branch string,
	subdir string,
) (*RemoteCommitSnapshot, error) {
	if repo == nil {
		return nil, errors.New("GitRepo 不能为空")
	}
	auth, err := validateAndBuildGitAuth(ctx, dialer, repo, creds)
	if err != nil {
		return nil, err
	}
	targetBranch := strings.TrimSpace(branch)
	if targetBranch == "" {
		targetBranch = strings.TrimSpace(repo.DefaultBranch)
	}

	safeCtx, cleanup := withSafeGitContext(ctx, dialer)
	defer cleanup()

	storer := memory.NewStorage()
	fs := memfs.New()

	cloneOpts := &git.CloneOptions{
		URL:          repo.RepoURL,
		Auth:         auth,
		Depth:        1,
		SingleBranch: targetBranch != "",
		Tags:         git.NoTags,
	}
	if targetBranch != "" {
		cloneOpts.ReferenceName = plumbing.NewBranchReferenceName(targetBranch)
	}

	cloned, err := git.CloneContext(safeCtx, storer, fs, cloneOpts)
	if err != nil && cloneOpts.Depth > 0 &&
		(strings.Contains(err.Error(), "unsupported capability: shallow") ||
			(strings.Contains(err.Error(), "git-upload-pack") && strings.Contains(err.Error(), "status code: 500"))) {
		// 部分受限或内置 Smart-HTTP Git 服务端未启用 shallow 能力时，回退全历史内存克隆（仍在 memfs 内完成，零磁盘写入）
		storer = memory.NewStorage()
		fs = memfs.New()
		cloneOpts.Depth = 0
		cloned, err = git.CloneContext(safeCtx, storer, fs, cloneOpts)
	}
	if err != nil {
		return nil, fmt.Errorf("go-git 内存浅克隆失败: %w", err)
	}

	headRef, err := cloned.Head()
	if err != nil {
		return nil, fmt.Errorf("读取 HEAD 引用失败: %w", err)
	}
	commitObj, err := cloned.CommitObject(headRef.Hash())
	if err != nil {
		return nil, fmt.Errorf("读取 HEAD Commit 失败: %w", err)
	}

	cleanSubdir := strings.Trim(strings.TrimSpace(subdir), "/")
	if cleanSubdir == "." {
		cleanSubdir = ""
	}

	var files []RemoteProtoFile
	var totalBytes int64
	if err := walkMemFSProtos(fs, "", cleanSubdir, &totalBytes, &files); err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].RepoFilePath < files[j].RepoFilePath
	})

	committedAt := commitObj.Committer.When
	if committedAt.IsZero() {
		committedAt = commitObj.Author.When
	}
	if committedAt.IsZero() {
		committedAt = time.Now().UTC()
	}

	return &RemoteCommitSnapshot{
		CommitSHA:     commitObj.Hash.String(),
		CommitMessage: strings.TrimSpace(commitObj.Message),
		AuthorName:    commitObj.Author.Name,
		AuthorEmail:   commitObj.Author.Email,
		CommittedAt:   committedAt.UTC(),
		Files:         files,
	}, nil
}

func walkMemFSProtos(
	fs billy.Filesystem,
	curDir string,
	subdirPrefix string,
	totalBytes *int64,
	out *[]RemoteProtoFile,
) error {
	entries, err := fs.ReadDir(curDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == ".git" || name == "." || name == ".." {
			continue
		}
		// 拒绝软链接穿越 memfs
		if entry.Mode()&os.ModeSymlink != 0 {
			continue
		}
		rel := name
		if curDir != "" {
			rel = path.Join(curDir, name)
		}
		if entry.IsDir() {
			if err := walkMemFSProtos(fs, rel, subdirPrefix, totalBytes, out); err != nil {
				return err
			}
			continue
		}
		if !strings.HasSuffix(rel, ".proto") {
			continue
		}
		if subdirPrefix != "" && !strings.HasPrefix(rel, subdirPrefix+"/") {
			continue
		}
		sanitized, sErr := decoder.SanitizeProtoRelPath(rel, true)
		if sErr != nil {
			return sErr
		}

		f, err := fs.Open(rel)
		if err != nil {
			return err
		}
		content, readErr := io.ReadAll(io.LimitReader(f, int64(decoder.MaxDescriptorFileSize)+1))
		closeErr := f.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		*totalBytes += int64(len(content))
		if *totalBytes > int64(decoder.MaxDescriptorFileSize) {
			return fmt.Errorf("memfs .proto 源码总体积超过 16MB (%d Bytes) 安全上限", decoder.MaxDescriptorFileSize)
		}
		sum := sha256.Sum256(content)
		*out = append(*out, RemoteProtoFile{
			RepoFilePath: sanitized,
			FilePath:     sanitized,
			Content:      content,
			SHA256:       hex.EncodeToString(sum[:]),
			Size:         len(content),
		})
	}
	return nil
}

func listBranchesViaMemFS(
	ctx context.Context,
	dialer *security.SafeDialer,
	repo *models.GitRepo,
	creds string,
) ([]string, error) {
	if repo == nil {
		return nil, errors.New("GitRepo 不能为空")
	}
	auth, err := validateAndBuildGitAuth(ctx, dialer, repo, creds)
	if err != nil {
		return nil, err
	}

	safeCtx, cleanup := withSafeGitContext(ctx, dialer)
	defer cleanup()

	remote := git.NewRemote(memory.NewStorage(), &gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{repo.RepoURL},
	})
	refs, err := remote.ListContext(safeCtx, &git.ListOptions{Auth: auth})
	if err != nil {
		return nil, fmt.Errorf("列举远端分支失败: %w", err)
	}
	var branches []string
	for _, r := range refs {
		if r.Name().IsBranch() {
			branches = append(branches, r.Name().Short())
		}
	}
	sort.Strings(branches)
	return branches, nil
}
