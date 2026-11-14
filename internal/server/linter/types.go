package linter

import (
	"context"
	"fmt"
	"log"
	"strings"

	"code-traefik/models"

	"github.com/bufbuild/protocompile"
	"github.com/bufbuild/protocompile/parser"
	"github.com/bufbuild/protocompile/reporter"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"gorm.io/gorm"
)

// SourceInput 待静态扫描的单个 .proto 源码或描述集输入
type SourceInput struct {
	ProtoFile      string
	Content        string
	Category       string // BUSINESS_API | BASE_LIB
	SchemaID       uint64
	FileDescriptor protoreflect.FileDescriptor
}

// LLMSettings LLM 辅助词汇识别配置（解密后仅驻留内存，严禁回显或落库）
type LLMSettings struct {
	Enabled              bool
	BaseURL              string
	APIKey               string
	Model                string
	TimeoutMs            int
	OutboundTrustedCIDRs string
}

// EvaluateOptions 引擎执行选项
type EvaluateOptions struct {
	TriggerSource       string
	TriggeredBy         uint
	IncludeDuplicates   bool
	IncludeLLM          bool
	SimilarityThreshold float64
	LLMOverride         *LLMSettings
	VocabWriter         *gorm.DB
}

// EvaluateOutput 静态检查输出聚合结果
type EvaluateOutput struct {
	Report          *models.CheckReport
	Violations      []models.CheckViolation
	DuplicateGroups []models.DuplicateGroupDTO
}

// ScanRequest 端到端扫描与持久化入库请求
type ScanRequest struct {
	DB                *gorm.DB
	TriggerSource     string
	TriggeredBy       uint
	GitRepoID         *uint64
	BundleID          *uint64
	ProtoFileID       *uint64
	SchemaID          *uint64
	SchemaIDs         []uint64
	Sources           []SourceInput
	IncludeDuplicates bool
	IncludeLLM        bool
	LLMOverride       *LLMSettings
}

type parsedFile struct {
	ProtoFile string
	Package   string
	PkgLine   int
	PkgCol    int
	Category  string
	SchemaID  uint64
	Services  []parsedService
	Messages  []parsedMessage
	Enums     []parsedEnum
}

type parsedService struct {
	Name           string
	FullName       string
	Line           int
	Col            int
	LeadingComment string
	Methods        []parsedRPC
}

type parsedRPC struct {
	Name           string
	FullName       string
	Line           int
	Col            int
	LeadingComment string
}

type parsedMessage struct {
	Name           string
	FullName       string
	Line           int
	Col            int
	LeadingComment string
	IsMapEntry     bool
	Fields         []parsedField
}

type parsedField struct {
	Name          string
	CanonicalType string
	Cardinality   string // singular | repeated | map
	Line          int
	Col           int
}

type parsedEnum struct {
	Name           string
	FullName       string
	Line           int
	Col            int
	LeadingComment string
	Values         []parsedEnumValue
}

type parsedEnumValue struct {
	Name   string
	Number int32
	Line   int
	Col    int
}

type sourceLoc struct {
	Line           int
	Col            int
	LeadingComment string
}

func pathKey(path []int32) string {
	if len(path) == 0 {
		return ""
	}
	parts := make([]string, len(path))
	for i, p := range path {
		parts[i] = fmt.Sprintf("%d", p)
	}
	return strings.Join(parts, ".")
}

func buildLocationIndex(fdp *descriptorpb.FileDescriptorProto) map[string]sourceLoc {
	out := make(map[string]sourceLoc)
	if fdp == nil || fdp.GetSourceCodeInfo() == nil {
		return out
	}
	for _, loc := range fdp.GetSourceCodeInfo().GetLocation() {
		span := loc.GetSpan()
		line, col := 1, 1
		if len(span) >= 2 {
			line = int(span[0]) + 1
			col = int(span[1]) + 1
		}
		comment := strings.TrimSpace(loc.GetLeadingComments())
		if comment == "" {
			comment = strings.TrimSpace(loc.GetTrailingComments())
		}
		if comment == "" && len(loc.GetLeadingDetachedComments()) > 0 {
			comment = strings.TrimSpace(strings.Join(loc.GetLeadingDetachedComments(), "\n"))
		}
		k := pathKey(loc.GetPath())
		// 同路径可能存在多条位置片段（如代码块与名字标识符），优先保留有注释或非 0 的首条
		prev, exists := out[k]
		if !exists || (prev.LeadingComment == "" && comment != "") {
			out[k] = sourceLoc{Line: line, Col: col, LeadingComment: comment}
		}
	}
	return out
}

func parseInputsToAST(ctx context.Context, inputs []SourceInput) ([]parsedFile, error) {
	sourceMap := make(map[string]string, len(inputs))
	targetNames := make([]string, 0, len(inputs))
	inputByPath := make(map[string]SourceInput, len(inputs))
	for _, in := range inputs {
		if in.ProtoFile == "" {
			continue
		}
		inputByPath[in.ProtoFile] = in
		if strings.TrimSpace(in.Content) != "" {
			sourceMap[in.ProtoFile] = in.Content
			targetNames = append(targetNames, in.ProtoFile)
		}
	}

	compiledByPath := make(map[string]*descriptorpb.FileDescriptorProto, len(inputs))
	if len(targetNames) > 0 {
		compiler := protocompile.Compiler{
			Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
				Accessor: protocompile.SourceAccessorFromMap(sourceMap),
			}),
			SourceInfoMode: protocompile.SourceInfoStandard | protocompile.SourceInfoExtraComments,
			Reporter:       reporter.NewReporter(func(reporter.ErrorWithPos) error { return nil }, nil),
		}
		if files, err := compiler.Compile(ctx, targetNames...); err == nil {
			for _, f := range files {
				if _, ok := sourceMap[f.Path()]; ok {
					compiledByPath[f.Path()] = protodesc.ToFileDescriptorProto(f)
				}
			}
		} else {
			// 批量交叉导入失败时回退单文件编译或无链接 AST 解析，确保局部源码照常完成体检
			for _, name := range targetNames {
				singleCompiler := protocompile.Compiler{
					Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
						Accessor: protocompile.SourceAccessorFromMap(sourceMap),
					}),
					SourceInfoMode: protocompile.SourceInfoStandard | protocompile.SourceInfoExtraComments,
					Reporter:       reporter.NewReporter(func(reporter.ErrorWithPos) error { return nil }, nil),
				}
				if singleFiles, sErr := singleCompiler.Compile(ctx, name); sErr == nil && len(singleFiles) > 0 {
					compiledByPath[name] = protodesc.ToFileDescriptorProto(singleFiles[0])
					continue
				}
				if fdp := parseASTWithoutLinking(name, sourceMap[name]); fdp != nil {
					compiledByPath[name] = fdp
				}
			}
		}
	}

	results := make([]parsedFile, 0, len(inputs))
	for _, in := range inputs {
		fdp := compiledByPath[in.ProtoFile]
		if fdp == nil && in.FileDescriptor != nil {
			fdp = protodesc.ToFileDescriptorProto(in.FileDescriptor)
		}
		if fdp == nil && strings.TrimSpace(in.Content) != "" {
			fdp = parseASTWithoutLinking(in.ProtoFile, in.Content)
		}
		if fdp == nil {
			continue
		}
		results = append(results, convertFDPToParsedFile(fdp, in))
	}
	return results, nil
}

func parseASTWithoutLinking(filename, content string) *descriptorpb.FileDescriptorProto {
	handler := reporter.NewHandler(reporter.NewReporter(func(reporter.ErrorWithPos) error { return nil }, nil))
	astNode, parseErr := parser.Parse(filename, strings.NewReader(content), handler)
	if astNode == nil {
		if parseErr != nil {
			log.Printf("[linter] 解析 .proto AST %s 失败: %v", filename, parseErr)
		}
		return nil
	}
	if parseErr != nil {
		log.Printf("[linter] 解析 .proto %s 存在非致命语法提示（继续基于局部 AST 体检）: %v", filename, parseErr)
	}
	res, resErr := parser.ResultFromAST(astNode, false, handler)
	if resErr != nil && res == nil {
		return nil
	}
	if res == nil {
		return nil
	}
	return res.FileDescriptorProto()
}

func convertFDPToParsedFile(fdp *descriptorpb.FileDescriptorProto, in SourceInput) parsedFile {
	locs := buildLocationIndex(fdp)
	pkgLoc := locs["2"]
	if pkgLoc.Line == 0 {
		pkgLoc.Line = 1
		pkgLoc.Col = 1
	}

	pf := parsedFile{
		ProtoFile: in.ProtoFile,
		Package:   fdp.GetPackage(),
		PkgLine:   pkgLoc.Line,
		PkgCol:    pkgLoc.Col,
		Category:  in.Category,
		SchemaID:  in.SchemaID,
	}
	if pf.ProtoFile == "" {
		pf.ProtoFile = fdp.GetName()
	}

	// 顶层与嵌套 Message（平铺收集供统一命名/词根/重复探测）
	for i, msg := range fdp.GetMessageType() {
		collectMessageRecursive(msg, pf.Package, []int32{4, int32(i)}, locs, &pf)
	}

	// 顶层 Enum
	for i, en := range fdp.GetEnumType() {
		pf.Enums = append(pf.Enums, convertEnum(en, pf.Package, []int32{5, int32(i)}, locs))
	}

	// Service 与 RPC 方法
	for sIdx, svc := range fdp.GetService() {
		svcPath := []int32{6, int32(sIdx)}
		svcLoc := locs[pathKey(svcPath)]
		if svcLoc.Line == 0 {
			svcLoc.Line = 1
			svcLoc.Col = 1
		}
		svcFull := joinFullName(pf.Package, svc.GetName())
		ps := parsedService{
			Name:           svc.GetName(),
			FullName:       svcFull,
			Line:           svcLoc.Line,
			Col:            svcLoc.Col,
			LeadingComment: svcLoc.LeadingComment,
			Methods:        make([]parsedRPC, 0, len(svc.GetMethod())),
		}
		for mIdx, method := range svc.GetMethod() {
			mPath := []int32{6, int32(sIdx), 2, int32(mIdx)}
			mLoc := locs[pathKey(mPath)]
			if mLoc.Line == 0 {
				mLoc.Line = svcLoc.Line
				mLoc.Col = 1
			}
			ps.Methods = append(ps.Methods, parsedRPC{
				Name:           method.GetName(),
				FullName:       svcFull + "." + method.GetName(),
				Line:           mLoc.Line,
				Col:            mLoc.Col,
				LeadingComment: mLoc.LeadingComment,
			})
		}
		pf.Services = append(pf.Services, ps)
	}

	return pf
}

func collectMessageRecursive(msg *descriptorpb.DescriptorProto, parentScope string, msgPath []int32, locs map[string]sourceLoc, pf *parsedFile) {
	if msg.GetOptions().GetMapEntry() {
		return
	}
	msgLoc := locs[pathKey(msgPath)]
	if msgLoc.Line == 0 {
		msgLoc.Line = 1
		msgLoc.Col = 1
	}
	msgFull := joinFullName(parentScope, msg.GetName())

	mapEntryByName := make(map[string]*descriptorpb.DescriptorProto)
	for _, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			mapEntryByName[nested.GetName()] = nested
			mapEntryByName["."+joinFullName(msgFull, nested.GetName())] = nested
			mapEntryByName[joinFullName(msgFull, nested.GetName())] = nested
		}
	}

	pm := parsedMessage{
		Name:           msg.GetName(),
		FullName:       msgFull,
		Line:           msgLoc.Line,
		Col:            msgLoc.Col,
		LeadingComment: msgLoc.LeadingComment,
		IsMapEntry:     false,
		Fields:         make([]parsedField, 0, len(msg.GetField())),
	}

	for fIdx, fld := range msg.GetField() {
		fPath := append(append([]int32{}, msgPath...), 2, int32(fIdx))
		fLoc := locs[pathKey(fPath)]
		if fLoc.Line == 0 {
			fLoc.Line = msgLoc.Line
			fLoc.Col = 1
		}
		canonType, cardinality := resolveFieldTypeAndCardinality(fld, mapEntryByName)
		pm.Fields = append(pm.Fields, parsedField{
			Name:          fld.GetName(),
			CanonicalType: canonType,
			Cardinality:   cardinality,
			Line:          fLoc.Line,
			Col:           fLoc.Col,
		})
	}
	pf.Messages = append(pf.Messages, pm)

	for nIdx, nested := range msg.GetNestedType() {
		if nested.GetOptions().GetMapEntry() {
			continue
		}
		nPath := append(append([]int32{}, msgPath...), 3, int32(nIdx))
		collectMessageRecursive(nested, msgFull, nPath, locs, pf)
	}
	for eIdx, nestedEnum := range msg.GetEnumType() {
		ePath := append(append([]int32{}, msgPath...), 4, int32(eIdx))
		pf.Enums = append(pf.Enums, convertEnum(nestedEnum, msgFull, ePath, locs))
	}
}

func convertEnum(en *descriptorpb.EnumDescriptorProto, parentScope string, enumPath []int32, locs map[string]sourceLoc) parsedEnum {
	eLoc := locs[pathKey(enumPath)]
	if eLoc.Line == 0 {
		eLoc.Line = 1
		eLoc.Col = 1
	}
	enumFull := joinFullName(parentScope, en.GetName())
	pe := parsedEnum{
		Name:           en.GetName(),
		FullName:       enumFull,
		Line:           eLoc.Line,
		Col:            eLoc.Col,
		LeadingComment: eLoc.LeadingComment,
		Values:         make([]parsedEnumValue, 0, len(en.GetValue())),
	}
	for vIdx, val := range en.GetValue() {
		vPath := append(append([]int32{}, enumPath...), 2, int32(vIdx))
		vLoc := locs[pathKey(vPath)]
		if vLoc.Line == 0 {
			vLoc.Line = eLoc.Line
			vLoc.Col = 1
		}
		pe.Values = append(pe.Values, parsedEnumValue{
			Name:   val.GetName(),
			Number: val.GetNumber(),
			Line:   vLoc.Line,
			Col:    vLoc.Col,
		})
	}
	return pe
}

func resolveFieldTypeAndCardinality(fld *descriptorpb.FieldDescriptorProto, mapEntryByName map[string]*descriptorpb.DescriptorProto) (string, string) {
	if fld.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED && fld.GetType() == descriptorpb.FieldDescriptorProto_TYPE_MESSAGE {
		if entry, ok := mapEntryByName[fld.GetTypeName()]; ok && entry != nil && len(entry.GetField()) >= 2 {
			kType := canonicalFieldProtoType(entry.GetField()[0])
			vType := canonicalFieldProtoType(entry.GetField()[1])
			return fmt.Sprintf("map<%s,%s>", kType, vType), "map"
		}
	}
	cardinality := "singular"
	if fld.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED {
		cardinality = "repeated"
	}
	return canonicalFieldProtoType(fld), cardinality
}

func canonicalFieldProtoType(fld *descriptorpb.FieldDescriptorProto) string {
	switch fld.GetType() {
	case descriptorpb.FieldDescriptorProto_TYPE_DOUBLE:
		return "double"
	case descriptorpb.FieldDescriptorProto_TYPE_FLOAT:
		return "float"
	case descriptorpb.FieldDescriptorProto_TYPE_INT64:
		return "int64"
	case descriptorpb.FieldDescriptorProto_TYPE_UINT64:
		return "uint64"
	case descriptorpb.FieldDescriptorProto_TYPE_INT32:
		return "int32"
	case descriptorpb.FieldDescriptorProto_TYPE_FIXED64:
		return "fixed64"
	case descriptorpb.FieldDescriptorProto_TYPE_FIXED32:
		return "fixed32"
	case descriptorpb.FieldDescriptorProto_TYPE_BOOL:
		return "bool"
	case descriptorpb.FieldDescriptorProto_TYPE_STRING:
		return "string"
	case descriptorpb.FieldDescriptorProto_TYPE_BYTES:
		return "bytes"
	case descriptorpb.FieldDescriptorProto_TYPE_UINT32:
		return "uint32"
	case descriptorpb.FieldDescriptorProto_TYPE_SFIXED32:
		return "sfixed32"
	case descriptorpb.FieldDescriptorProto_TYPE_SFIXED64:
		return "sfixed64"
	case descriptorpb.FieldDescriptorProto_TYPE_SINT32:
		return "sint32"
	case descriptorpb.FieldDescriptorProto_TYPE_SINT64:
		return "sint64"
	case descriptorpb.FieldDescriptorProto_TYPE_ENUM, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, descriptorpb.FieldDescriptorProto_TYPE_GROUP:
		tn := strings.TrimPrefix(fld.GetTypeName(), ".")
		if tn != "" {
			return tn
		}
		return "message"
	default:
		return strings.ToLower(strings.TrimPrefix(fld.GetType().String(), "TYPE_"))
	}
}

func joinFullName(scope, name string) string {
	if scope == "" {
		return name
	}
	return scope + "." + name
}
