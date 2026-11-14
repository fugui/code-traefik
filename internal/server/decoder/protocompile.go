package decoder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"

	"code-traefik/models"

	"github.com/bufbuild/protocompile"
	"github.com/bufbuild/protocompile/protoutil"
	"github.com/bufbuild/protocompile/reporter"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"gorm.io/gorm"
)

var (
	// ErrPathTraversalDenied 虚拟文件路径非法（路径穿越、绝对路径或控制字符）
	ErrPathTraversalDenied = errors.New("非法的虚拟文件路径，拒绝路径穿越或绝对路径")
	// ErrMissingImports 缺失外部导入的 .proto 依赖文件
	ErrMissingImports = errors.New("缺失外部依赖 .proto 文件")
	// ErrCompileSyntax Proto 源码存在语法或语义链接错误
	ErrCompileSyntax = errors.New("Proto 源码存在语法或链接错误")
)

// CompileDiagnosticDTO 结构化编译错误定位诊断信息 (file/line/column/message)
type CompileDiagnosticDTO struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
}

// CompileError 结构化纯 Go Proto 编译错误
type CompileError struct {
	Code           string                 `json:"code"`
	HTTPStatus     int                    `json:"-"`
	Message        string                 `json:"error"`
	Diagnostics    []CompileDiagnosticDTO `json:"diagnostics,omitempty"`
	MissingImports []string               `json:"missing_imports,omitempty"`
	Err            error                  `json:"-"`
}

func (e *CompileError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "Proto 源码编译失败"
}

func (e *CompileError) Unwrap() error {
	return e.Err
}

// ProtoSourceEntry Tier 2 源码供给条目（M25 接入 traefik_proto_files 时按 BASE_LIB 优先）
type ProtoSourceEntry struct {
	Content  []byte
	Category string // BASE_LIB | BUSINESS | GATEWAY
}

// ProtoSourceProvider 虚拟编译期跨包依赖源码供给接口
// M22 由内存/缓存实现提供扩展点，M25 由 DB traefik_proto_files 快照实现注入
type ProtoSourceProvider interface {
	ResolveProtoSource(ctx context.Context, cleanPath string) (*ProtoSourceEntry, error)
}

// InMemoryProtoSourceProvider M22 内置线程安全虚拟源码供给实现（支持 BASE_LIB 优先覆盖）
type InMemoryProtoSourceProvider struct {
	mu          sync.RWMutex
	baseEntries map[string][]byte
	otherEntry  map[string]*ProtoSourceEntry
}

// NewInMemoryProtoSourceProvider 创建内存态源码供给器
func NewInMemoryProtoSourceProvider() *InMemoryProtoSourceProvider {
	return &InMemoryProtoSourceProvider{
		baseEntries: make(map[string][]byte),
		otherEntry:  make(map[string]*ProtoSourceEntry),
	}
}

// Put 注册一条虚拟源码（BASE_LIB 永远优先于非 BASE_LIB）
func (p *InMemoryProtoSourceProvider) Put(rawPath, category string, content []byte) error {
	cleaned, err := SanitizeProtoRelPath(rawPath, true)
	if err != nil {
		return err
	}
	copied := make([]byte, len(content))
	copy(copied, content)

	p.mu.Lock()
	defer p.mu.Unlock()
	if category == models.ProtoCategoryBaseLib {
		p.baseEntries[cleaned] = copied
		return nil
	}
	p.otherEntry[cleaned] = &ProtoSourceEntry{
		Category: category,
		Content:  copied,
	}
	return nil
}

// ResolveProtoSource 按 BASE_LIB 优先原则返回匹配的源码快照
func (p *InMemoryProtoSourceProvider) ResolveProtoSource(_ context.Context, cleanPath string) (*ProtoSourceEntry, error) {
	if p == nil {
		return nil, nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()

	if baseContent, ok := p.baseEntries[cleanPath]; ok {
		copied := make([]byte, len(baseContent))
		copy(copied, baseContent)
		return &ProtoSourceEntry{
			Category: models.ProtoCategoryBaseLib,
			Content:  copied,
		}, nil
	}
	if other, ok := p.otherEntry[cleanPath]; ok {
		copied := make([]byte, len(other.Content))
		copy(copied, other.Content)
		return &ProtoSourceEntry{
			Category: other.Category,
			Content:  copied,
		}, nil
	}
	return nil, nil
}

// DBProtoSourceProvider M25 Tier 2 数据库 .proto 源码跨包解析器（查询 traefik_proto_files JOIN versions，忽略软删，BASE_LIB 优先）
type DBProtoSourceProvider struct {
	db *gorm.DB
}

// NewDBProtoSourceProvider 创建基于 PostgreSQL traefik_proto_files 的 Tier 2 源码提供者
func NewDBProtoSourceProvider(db *gorm.DB) *DBProtoSourceProvider {
	return &DBProtoSourceProvider{db: db}
}

// ResolveProtoSource 按规范相对路径检索未被软删的最新只读 .proto 快照（优先 BASE_LIB）
func (p *DBProtoSourceProvider) ResolveProtoSource(ctx context.Context, cleanPath string) (*ProtoSourceEntry, error) {
	if p == nil || p.db == nil {
		return nil, nil
	}
	cleaned, err := SanitizeProtoRelPath(cleanPath, false)
	if err != nil {
		return nil, err
	}
	type dbProtoSourceRow struct {
		Category    string
		ContentText string
	}
	var row dbProtoSourceRow
	tx := p.db.WithContext(ctx).
		Table("traefik_proto_files AS f").
		Select("f.category AS category, v.content_text AS content_text").
		Joins("JOIN traefik_proto_file_versions AS v ON v.id = f.latest_version_id").
		Where("f.is_deleted = false AND f.file_path = ?", cleaned).
		Order("CASE WHEN f.category = 'BASE_LIB' THEN 0 ELSE 1 END, f.updated_at DESC, f.id DESC").
		Limit(1).
		Find(&row)
	if tx.Error != nil {
		return nil, tx.Error
	}
	if tx.RowsAffected == 0 {
		return nil, nil
	}
	return &ProtoSourceEntry{
		Category: row.Category,
		Content:  []byte(row.ContentText),
	}, nil
}

// CompileProtoBundleInput 纯 Go protocompile 在线编译入参
type CompileProtoBundleInput struct {
	Name             string
	Category         string
	VersionTag       string
	Description      string
	Files            map[string]string
	ProtoIncludeDirs []string
	EntryPoints      []string
	AutoActivate     bool
	AllowBreaking    bool
	BreakingReason   string
	SourceType       string
	GitRepoID        *uint64
	SourceBundleID   *uint64
	SourceCommitSHA  string
	SourceMRIID      *int
	OperatorID       uint
	SourceProvider   ProtoSourceProvider
}

// CompileProtoOutput 纯 Go protocompile 编译输出结果
type CompileProtoOutput struct {
	DescriptorBytes []byte
	OwnProtoFiles   []string
	ExternalImports []string
	Operations      []models.ProtoOperation
}

// SanitizeProtoRelPath 清洗虚拟 POSIX 相对路径并严禁前导 `/`、`..`、`\0`、`\` 等穿越载荷
func SanitizeProtoRelPath(raw string, requireProtoSuffix bool) (string, error) {
	if raw == "" || strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("%w: 空文件路径", ErrPathTraversalDenied)
	}
	if strings.ContainsAny(raw, "\x00\\") {
		return "", fmt.Errorf("%w: 包含非法控制字符或反斜杠 (%q)", ErrPathTraversalDenied, raw)
	}
	if strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("%w: 禁止绝对路径 (%q)", ErrPathTraversalDenied, raw)
	}
	for _, seg := range strings.Split(raw, "/") {
		if seg == ".." {
			return "", fmt.Errorf("%w: 禁止上级目录穿越段 '..' (%q)", ErrPathTraversalDenied, raw)
		}
	}

	cleaned := path.Clean(raw)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("%w: 归一化路径非法 (%q -> %q)", ErrPathTraversalDenied, raw, cleaned)
	}
	if requireProtoSuffix && !strings.HasSuffix(cleaned, ".proto") {
		return "", fmt.Errorf("%w: 虚拟源码文件必须以 .proto 结尾 (%q)", ErrPathTraversalDenied, cleaned)
	}
	return cleaned, nil
}

// sanitizeIncludeDirs 校验并归一化 include_dirs 列表（缺省为 ["."]）
func sanitizeIncludeDirs(dirs []string) ([]string, error) {
	if len(dirs) == 0 {
		return []string{"."}, nil
	}
	seen := make(map[string]struct{}, len(dirs))
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		if strings.ContainsAny(d, "\x00\\") {
			return nil, fmt.Errorf("%w: include_dir 包含非法字符 (%q)", ErrPathTraversalDenied, d)
		}
		if strings.HasPrefix(d, "/") {
			return nil, fmt.Errorf("%w: include_dir 禁止绝对路径 (%q)", ErrPathTraversalDenied, d)
		}
		for _, seg := range strings.Split(d, "/") {
			if seg == ".." {
				return nil, fmt.Errorf("%w: include_dir 禁止 '..' (%q)", ErrPathTraversalDenied, d)
			}
		}
		trimmed := strings.TrimSpace(d)
		cleaned := "."
		if trimmed != "" {
			cleaned = path.Clean(trimmed)
		}
		if cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
			return nil, fmt.Errorf("%w: include_dir 非法 (%q)", ErrPathTraversalDenied, d)
		}
		if _, ok := seen[cleaned]; !ok {
			seen[cleaned] = struct{}{}
			out = append(out, cleaned)
		}
	}
	if len(out) == 0 {
		out = []string{"."}
	}
	return out, nil
}

type emptyResolver struct{}

func (emptyResolver) FindFileByPath(_ string) (protocompile.SearchResult, error) {
	return protocompile.SearchResult{}, protoregistry.NotFound
}

// virtualProtoResolver 实现四级确定性虚拟 Protobuf 依赖解析器:
// Tier 1: 当前虚拟源码树 + proto_include_dirs
// Tier 2: 注入的 ProtoSourceProvider (BASE_LIB 优先)
// Tier 3: 全局已激活 CompiledRegistry.Files
// Tier 4: protocompile.WithStandardImports (google/protobuf/* WKT)
type virtualProtoResolver struct {
	ctx            context.Context
	rawFiles       map[string]string
	canonicalFiles map[string]string
	includeDirs    []string
	provider       ProtoSourceProvider
	registryFiles  *protoregistry.Files
	wktResolver    protocompile.Resolver

	mu               sync.Mutex
	resolvedExternal map[string]struct{}
	missingImports   map[string]struct{}
	traversalErr     error
}

func (r *virtualProtoResolver) FindFileByPath(importPath string) (protocompile.SearchResult, error) {
	cleaned, err := SanitizeProtoRelPath(importPath, false)
	if err != nil {
		r.mu.Lock()
		if r.traversalErr == nil {
			r.traversalErr = err
		}
		r.mu.Unlock()
		return protocompile.SearchResult{}, err
	}

	// Tier 1: 当前虚拟源码树 (规范路径优先 + include_dirs 搜索)
	if content, ok := r.canonicalFiles[cleaned]; ok {
		return protocompile.SearchResult{Source: strings.NewReader(content)}, nil
	}
	if content, ok := r.rawFiles[cleaned]; ok {
		return protocompile.SearchResult{Source: strings.NewReader(content)}, nil
	}
	for _, inc := range r.includeDirs {
		if inc == "." {
			continue
		}
		candidate := path.Clean(inc + "/" + cleaned)
		if content, ok := r.rawFiles[candidate]; ok {
			return protocompile.SearchResult{Source: strings.NewReader(content)}, nil
		}
	}

	// Tier 2: ProtoSourceProvider (M22 缓存 / M25 traefik_proto_files, BASE_LIB 优先)
	if r.provider != nil {
		entry, provErr := r.provider.ResolveProtoSource(r.ctx, cleaned)
		if provErr != nil && !errors.Is(provErr, protoregistry.NotFound) {
			return protocompile.SearchResult{}, provErr
		}
		if entry != nil {
			r.mu.Lock()
			r.resolvedExternal[cleaned] = struct{}{}
			r.mu.Unlock()
			return protocompile.SearchResult{Source: bytes.NewReader(entry.Content)}, nil
		}
	}

	// Tier 3: 已激活 CompiledRegistry.Files (Descriptor 零重解析直接复用)
	if r.registryFiles != nil {
		if desc, err := r.registryFiles.FindFileByPath(cleaned); err == nil && desc != nil {
			r.mu.Lock()
			r.resolvedExternal[cleaned] = struct{}{}
			r.mu.Unlock()
			return protocompile.SearchResult{Desc: desc}, nil
		}
	}

	// Tier 4: WKT 标准库 (google/protobuf/*)
	if strings.HasPrefix(cleaned, "google/protobuf/") && r.wktResolver != nil {
		if res, err := r.wktResolver.FindFileByPath(cleaned); err == nil {
			return res, nil
		}
	}

	r.mu.Lock()
	r.missingImports[cleaned] = struct{}{}
	r.mu.Unlock()
	return protocompile.SearchResult{}, protoregistry.NotFound
}

// CompileProtoBundle 使用纯 Go protocompile 在内存中编译虚拟源码树，不依赖外部 protoc，严禁触碰磁盘。
// Iron Law 1: 仅当次自有文件通过 SourceInfoStandard 保留 SourceCodeInfo，全部外部依赖均剥离 SourceCodeInfo。
// Iron Law 2: 提取 ProtoOperation 时跳过全部 external_imports（不仅 BASE_LIB），杜绝跨包服务归属冲突。
func CompileProtoBundle(ctx context.Context, registryFiles *protoregistry.Files, in CompileProtoBundleInput) (*CompileProtoOutput, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(in.Files) == 0 {
		return nil, &CompileError{
			Code:       "COMPILE_SYNTAX_ERROR",
			HTTPStatus: 400,
			Message:    "files 源码文件列表不能为空",
			Err:        ErrCompileSyntax,
		}
	}

	includeDirs, err := sanitizeIncludeDirs(in.ProtoIncludeDirs)
	if err != nil {
		return nil, &CompileError{
			Code:       "PATH_TRAVERSAL_DENIED",
			HTTPStatus: 400,
			Message:    err.Error(),
			Err:        err,
		}
	}

	rawFiles := make(map[string]string, len(in.Files))
	var totalBytes int64
	rawKeys := make([]string, 0, len(in.Files))
	for rawPath, content := range in.Files {
		cleaned, err := SanitizeProtoRelPath(rawPath, true)
		if err != nil {
			return nil, &CompileError{
				Code:       "PATH_TRAVERSAL_DENIED",
				HTTPStatus: 400,
				Message:    err.Error(),
				Err:        err,
			}
		}
		if _, dup := rawFiles[cleaned]; dup {
			return nil, &CompileError{
				Code:       "COMPILE_SYNTAX_ERROR",
				HTTPStatus: 400,
				Message:    fmt.Sprintf("存在归一化后重复的文件路径: %s", cleaned),
				Err:        ErrCompileSyntax,
			}
		}
		totalBytes += int64(len(content))
		if totalBytes > int64(MaxDescriptorFileSize) {
			return nil, &CompileError{
				Code:       "PAYLOAD_TOO_LARGE",
				HTTPStatus: 400,
				Message:    fmt.Sprintf("提交的 .proto 源码总大小超过 16MB (%d Bytes) 上限", MaxDescriptorFileSize),
				Err:        ErrDescriptorTooLarge,
			}
		}
		rawFiles[cleaned] = content
		rawKeys = append(rawKeys, cleaned)
	}
	sort.Strings(rawKeys)

	// 按非 "." 的最长 includeDir 前缀推导虚拟树规范 import 路径，避免同文件在 include 路径下重复编译
	nonRootIncludes := make([]string, 0, len(includeDirs))
	for _, d := range includeDirs {
		if d != "." {
			nonRootIncludes = append(nonRootIncludes, d)
		}
	}
	sort.Slice(nonRootIncludes, func(i, j int) bool {
		if len(nonRootIncludes[i]) != len(nonRootIncludes[j]) {
			return len(nonRootIncludes[i]) > len(nonRootIncludes[j])
		}
		return nonRootIncludes[i] < nonRootIncludes[j]
	})

	canonicalFiles := make(map[string]string, len(rawFiles))
	rawToCanonical := make(map[string]string, len(rawFiles))
	ownCanonicalSet := make(map[string]struct{}, len(rawFiles))
	for _, rk := range rawKeys {
		canon := rk
		for _, inc := range nonRootIncludes {
			prefix := inc + "/"
			if strings.HasPrefix(rk, prefix) {
				canon = strings.TrimPrefix(rk, prefix)
				break
			}
		}
		if _, dup := canonicalFiles[canon]; dup {
			return nil, &CompileError{
				Code:       "COMPILE_SYNTAX_ERROR",
				HTTPStatus: 400,
				Message:    fmt.Sprintf("include_dirs 映射导致冲突的规范导入路径: %s", canon),
				Err:        ErrCompileSyntax,
			}
		}
		canonicalFiles[canon] = rawFiles[rk]
		rawToCanonical[rk] = canon
		ownCanonicalSet[canon] = struct{}{}
	}

	var compileTargets []string
	if len(in.EntryPoints) > 0 {
		seenTarget := make(map[string]struct{}, len(in.EntryPoints))
		for _, ep := range in.EntryPoints {
			cleanedEP, err := SanitizeProtoRelPath(ep, true)
			if err != nil {
				return nil, &CompileError{
					Code:       "PATH_TRAVERSAL_DENIED",
					HTTPStatus: 400,
					Message:    err.Error(),
					Err:        err,
				}
			}
			target := cleanedEP
			if c, ok := rawToCanonical[cleanedEP]; ok {
				target = c
			}
			if _, ok := canonicalFiles[target]; !ok {
				return nil, &CompileError{
					Code:       "COMPILE_SYNTAX_ERROR",
					HTTPStatus: 400,
					Message:    fmt.Sprintf("entry_file 不在提交的文件列表中: %s", cleanedEP),
					Err:        ErrCompileSyntax,
				}
			}
			if _, ok := seenTarget[target]; !ok {
				seenTarget[target] = struct{}{}
				compileTargets = append(compileTargets, target)
			}
		}
	} else {
		compileTargets = make([]string, 0, len(canonicalFiles))
		for k := range canonicalFiles {
			compileTargets = append(compileTargets, k)
		}
		sort.Strings(compileTargets)
	}

	resolver := &virtualProtoResolver{
		ctx:              ctx,
		rawFiles:         rawFiles,
		canonicalFiles:   canonicalFiles,
		includeDirs:      includeDirs,
		provider:         in.SourceProvider,
		registryFiles:    registryFiles,
		wktResolver:      protocompile.WithStandardImports(emptyResolver{}),
		resolvedExternal: make(map[string]struct{}),
		missingImports:   make(map[string]struct{}),
	}

	var diagMu sync.Mutex
	var diagnostics []CompileDiagnosticDTO
	errRep := reporter.NewReporter(func(errWithPos reporter.ErrorWithPos) error {
		pos := errWithPos.GetPosition()
		msg := errWithPos.Error()
		if uw := errWithPos.Unwrap(); uw != nil {
			msg = uw.Error()
		}
		diagMu.Lock()
		diagnostics = append(diagnostics, CompileDiagnosticDTO{
			File:    pos.Filename,
			Line:    pos.Line,
			Column:  pos.Col,
			Message: msg,
		})
		diagMu.Unlock()
		return nil
	}, nil)

	compiler := protocompile.Compiler{
		Resolver:       resolver,
		Reporter:       errRep,
		SourceInfoMode: protocompile.SourceInfoStandard,
	}

	linkedFiles, compileErr := compiler.Compile(ctx, compileTargets...)
	if compileErr != nil {
		resolver.mu.Lock()
		tErr := resolver.traversalErr
		missing := make([]string, 0, len(resolver.missingImports))
		for m := range resolver.missingImports {
			missing = append(missing, m)
		}
		resolver.mu.Unlock()
		sort.Strings(missing)

		if tErr != nil {
			return nil, &CompileError{
				Code:        "PATH_TRAVERSAL_DENIED",
				HTTPStatus:  400,
				Message:     tErr.Error(),
				Diagnostics: diagnostics,
				Err:         tErr,
			}
		}
		if len(missing) > 0 {
			return nil, &CompileError{
				Code:           "MISSING_IMPORTS",
				HTTPStatus:     400,
				Message:        fmt.Sprintf("缺失外部依赖导入: %s", strings.Join(missing, ", ")),
				Diagnostics:    diagnostics,
				MissingImports: missing,
				Err:            fmt.Errorf("%w: %s", ErrMissingImports, strings.Join(missing, ", ")),
			}
		}
		msg := compileErr.Error()
		if len(diagnostics) > 0 && diagnostics[0].Message != "" {
			msg = fmt.Sprintf("%s (%s:%d:%d)", diagnostics[0].Message, diagnostics[0].File, diagnostics[0].Line, diagnostics[0].Column)
		}
		return nil, &CompileError{
			Code:        "COMPILE_SYNTAX_ERROR",
			HTTPStatus:  400,
			Message:     msg,
			Diagnostics: diagnostics,
			Err:         fmt.Errorf("%w: %s", ErrCompileSyntax, msg),
		}
	}

	// 按后序拓扑收集全部非 WKT 文件描述：自有文件保留 SourceCodeInfo，外部导入文件严格剥离 SourceCodeInfo
	visited := make(map[string]struct{})
	var orderedProtos []*descriptorpb.FileDescriptorProto
	var collectDFS func(fd protoreflect.FileDescriptor) error
	collectDFS = func(fd protoreflect.FileDescriptor) error {
		if fd == nil {
			return nil
		}
		p := fd.Path()
		if strings.HasPrefix(p, "google/protobuf/") {
			return nil
		}
		if _, seen := visited[p]; seen {
			return nil
		}
		visited[p] = struct{}{}

		imports := fd.Imports()
		for i := 0; i < imports.Len(); i++ {
			if err := collectDFS(imports.Get(i).FileDescriptor); err != nil {
				return err
			}
		}

		rawProto := protoutil.ProtoFromFileDescriptor(fd)
		if rawProto == nil {
			return fmt.Errorf("无法转换文件描述符: %s", p)
		}
		cloned, ok := proto.Clone(rawProto).(*descriptorpb.FileDescriptorProto)
		if !ok || cloned == nil {
			return fmt.Errorf("深拷贝文件描述符失败: %s", p)
		}
		if _, isOwn := ownCanonicalSet[p]; !isOwn {
			// 跨包外部依赖统一剥离 SourceCodeInfo 防止 .pb 体积膨胀超 16MB
			cloned.SourceCodeInfo = nil
		}
		orderedProtos = append(orderedProtos, cloned)
		return nil
	}

	for _, lf := range linkedFiles {
		if err := collectDFS(lf); err != nil {
			return nil, err
		}
	}

	fds := &descriptorpb.FileDescriptorSet{File: orderedProtos}
	descriptorBytes, err := proto.Marshal(fds)
	if err != nil {
		return nil, fmt.Errorf("序列化 FileDescriptorSet 失败: %w", err)
	}
	if len(descriptorBytes) > MaxDescriptorFileSize {
		return nil, &CompileError{
			Code:       "PAYLOAD_TOO_LARGE",
			HTTPStatus: 400,
			Message:    fmt.Sprintf("编译生成的 .pb 描述集超出 16MB (%d Bytes) 上限", MaxDescriptorFileSize),
			Err:        ErrDescriptorTooLarge,
		}
	}

	// 验证并抽取严格归属于自有文件的 Service/Operation（跳过全部 external_imports）
	ownList := make([]string, 0, len(ownCanonicalSet))
	for k := range ownCanonicalSet {
		ownList = append(ownList, k)
	}
	sort.Strings(ownList)

	_, _, ops, ownFilesOut, extImportsOut, err := extractOperationsFromDescriptorWithOwn(
		descriptorBytes,
		in.Category,
		nil,
		ownList,
	)
	if err != nil {
		return nil, err
	}

	return &CompileProtoOutput{
		DescriptorBytes: descriptorBytes,
		OwnProtoFiles:   ownFilesOut,
		ExternalImports: extImportsOut,
		Operations:      ops,
	}, nil
}

// buildActiveRegistryFilesFromDB 从活跃中的描述集构建 Tier 3 protoregistry.Files（BASE_LIB 优先）
func buildActiveRegistryFilesFromDB(db *gorm.DB) *protoregistry.Files {
	if db == nil {
		return nil
	}
	var activeSchemas []models.ProtoSchema
	if err := db.Where("is_active = true").Order("id ASC").Find(&activeSchemas).Error; err != nil || len(activeSchemas) == 0 {
		return nil
	}

	filesMap := make(map[string]*descriptorpb.FileDescriptorProto)
	// 先放非 BASE_LIB，再让 BASE_LIB 高优覆盖
	for _, passBase := range []bool{false, true} {
		for _, s := range activeSchemas {
			if (s.Category == models.ProtoCategoryBaseLib) != passBase || len(s.DescriptorBytes) == 0 {
				continue
			}
			var ownNames []string
			if len(s.ProtoFiles) > 0 {
				if err := json.Unmarshal(s.ProtoFiles, &ownNames); err != nil {
					ownNames = nil
				}
			}
			ownSet := make(map[string]struct{}, len(ownNames))
			for _, on := range ownNames {
				ownSet[on] = struct{}{}
			}
			var fds descriptorpb.FileDescriptorSet
			if err := proto.Unmarshal(s.DescriptorBytes, &fds); err != nil {
				continue
			}
			for _, f := range fds.File {
				if _, isOwn := ownSet[f.GetName()]; isOwn || len(ownSet) == 0 {
					filesMap[f.GetName()] = f
				}
			}
		}
	}

	sortedFiles, _, err := topologicalSortFiles(filesMap)
	if err != nil {
		return nil
	}
	files, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{File: sortedFiles})
	if err != nil {
		return nil
	}
	return files
}

func (r *Registry) compiledFiles() *protoregistry.Files {
	if r == nil {
		return nil
	}
	if cr := r.compiled.Load(); cr != nil {
		return cr.Files
	}
	return nil
}

// CompileAndSaveSchema 执行纯 Go 在线编译并复用 UploadSchema 资产入库/冲突/破坏性门禁/试跑流水线
func (r *Registry) CompileAndSaveSchema(ctx context.Context, in CompileProtoBundleInput) (*models.ProtoSchema, []models.ProtoOperation, int, error) {
	if r == nil || r.db == nil {
		return nil, nil, 0, errors.New("描述集注册中心未初始化")
	}

	category := in.Category
	if category == "" {
		category = models.ProtoCategoryBusiness
	}
	sourceType := in.SourceType
	if sourceType == "" {
		sourceType = models.ProtoSourceOnlineCompile
	}

	var regFiles *protoregistry.Files
	if cr := r.compiled.Load(); cr != nil && cr.Files != nil {
		regFiles = cr.Files
	} else {
		regFiles = buildActiveRegistryFilesFromDB(r.db)
	}

	in.Category = category
	if in.SourceProvider == nil {
		in.SourceProvider = NewDBProtoSourceProvider(r.db)
	}
	compiled, err := CompileProtoBundle(ctx, regFiles, in)
	if err != nil {
		return nil, nil, 0, err
	}

	cleanName := strings.TrimSpace(in.Name)
	displayFileName := cleanName + ".compiled.pb"
	if cleanName == "" {
		displayFileName = "compiled.pb"
	}

	// Tier 2 ProtoSourceProvider 预供源可能在尚未独立入池之前被草稿引入，草稿下允许使用已链接的剥离外部描述符完成校验
	allowSelfContainedExt := in.SourceProvider != nil && !in.AutoActivate

	return r.UploadSchema(UploadSchemaInput{
		Name:                       cleanName,
		Category:                   category,
		VersionTag:                 in.VersionTag,
		FileName:                   displayFileName,
		Description:                in.Description,
		RawBytes:                   compiled.DescriptorBytes,
		AutoActivate:               in.AutoActivate,
		AllowBreaking:              in.AllowBreaking,
		BreakingReason:             in.BreakingReason,
		OperatorID:                 in.OperatorID,
		SourceType:                 sourceType,
		GitRepoID:                  in.GitRepoID,
		SourceBundleID:             in.SourceBundleID,
		SourceCommitSHA:            in.SourceCommitSHA,
		SourceMRIID:                in.SourceMRIID,
		ExplicitOwnFiles:           compiled.OwnProtoFiles,
		AllowSelfContainedExternal: allowSelfContainedExt,
	})
}
