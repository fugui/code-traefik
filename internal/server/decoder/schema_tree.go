package decoder

import (
	"fmt"
	"regexp"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

const MaxFieldTreeDepth = 12

// ProtoFieldNodeDTO 参数字段字典树节点契约
type ProtoFieldNodeDTO struct {
	Name            string              `json:"name"`
	JSONName        string              `json:"jsonName"`
	JSONNameSnake   string              `json:"json_name,omitempty"`
	TagNumber       int                 `json:"tagNumber"`
	TagNumberSnake  int                 `json:"tag_number,omitempty"`
	Kind            string              `json:"kind"`
	TypeName        string              `json:"typeName,omitempty"`
	TypeNameSnake   string              `json:"type_name,omitempty"`
	Cardinality     string              `json:"cardinality"`
	IsMap           bool                `json:"isMap"`
	IsMapSnake      bool                `json:"is_map,omitempty"`
	Comments        string              `json:"comments"`
	FieldPath       string              `json:"fieldPath"`
	FieldPathSnake  string              `json:"field_path,omitempty"`
	IsIgnoredInDiff bool                `json:"isIgnoredInDiff"`
	IsIgnoredSnake  bool                `json:"is_ignored_in_diff,omitempty"`
	IsTruncated     bool                `json:"isTruncated,omitempty"`
	IsTruncatedSnk  bool                `json:"is_truncated,omitempty"`
	Children        []ProtoFieldNodeDTO `json:"children,omitempty"`
}

// MessageSubTreeDTO 消息体根级 DTO
type MessageSubTreeDTO struct {
	FullName string              `json:"fullName"`
	Fields   []ProtoFieldNodeDTO `json:"fields"`
}

// OperationSchemaTreeDTO 操作方法完整 Req/Rsp 参数字典树 DTO
type OperationSchemaTreeDTO struct {
	OperationID          string             `json:"operationId"`
	OperationIDSnake     string             `json:"operation_id,omitempty"`
	ServiceName          string             `json:"serviceName"`
	ServiceNameSnake     string             `json:"service_name,omitempty"`
	Operation            string             `json:"operation"`
	ProtoFile            string             `json:"protoFile"`
	ProtoFileSnake       string             `json:"proto_file,omitempty"`
	ClientStreaming      bool               `json:"clientStreaming"`
	ServerStreaming      bool               `json:"serverStreaming"`
	DiffIgnorePaths      []string           `json:"diffIgnorePaths"`
	DiffIgnoreSnake      []string           `json:"diff_ignore_paths,omitempty"`
	InheritedIgnorePaths []string           `json:"inheritedIgnorePaths"`
	InheritedIgnoreSnake []string           `json:"inherited_ignore_paths,omitempty"`
	ReqMessage           *MessageSubTreeDTO `json:"reqMessage"`
	ReqMessageSnake      *MessageSubTreeDTO `json:"req_message,omitempty"`
	RspMessage           *MessageSubTreeDTO `json:"rspMessage"`
	RspMessageSnake      *MessageSubTreeDTO `json:"rsp_message,omitempty"`
}

// ExtractMessageFieldTree 纯反射遍历 MessageDescriptor 提取树形字段定义
func ExtractMessageFieldTree(desc protoreflect.MessageDescriptor) []ProtoFieldNodeDTO {
	return ExtractMessageFieldTreeWithIgnores(desc, nil)
}

// ExtractMessageFieldTreeWithIgnores 纯反射遍历 MessageDescriptor 并按忽略规则计算 is_ignored_in_diff
func ExtractMessageFieldTreeWithIgnores(desc protoreflect.MessageDescriptor, ignorePaths []string) []ProtoFieldNodeDTO {
	if desc == nil {
		return []ProtoFieldNodeDTO{}
	}
	return extractMessageFieldsInternal(desc, "", 1, ignorePaths)
}

func extractMessageFieldsInternal(
	desc protoreflect.MessageDescriptor,
	parentPrefix string,
	currentDepth int,
	ignorePaths []string,
) []ProtoFieldNodeDTO {
	if desc == nil {
		return nil
	}

	fields := desc.Fields()
	nodes := make([]ProtoFieldNodeDTO, 0, fields.Len())

	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		fieldName := string(fd.Name())
		jsonName := fd.JSONName()
		tagNumber := int(fd.Number())
		kindStr := mapFieldKindToString(fd.Kind())

		cardinalityStr := "OPTIONAL"
		if fd.IsList() || fd.Cardinality() == protoreflect.Repeated {
			cardinalityStr = "REPEATED"
		} else if fd.Cardinality() == protoreflect.Required {
			cardinalityStr = "REQUIRED"
		}

		isMap := fd.IsMap()
		typeName := ""
		if fd.Kind() == protoreflect.EnumKind && fd.Enum() != nil {
			typeName = string(fd.Enum().FullName())
		} else if fd.Kind() == protoreflect.MessageKind && fd.Message() != nil {
			if isMap {
				mapKeyKind := mapFieldKindToString(fd.MapKey().Kind())
				mapValKind := mapFieldKindToString(fd.MapValue().Kind())
				if fd.MapValue().Kind() == protoreflect.MessageKind && fd.MapValue().Message() != nil {
					mapValKind = string(fd.MapValue().Message().FullName())
				}
				typeName = fmt.Sprintf("map<%s, %s>", mapKeyKind, mapValKind)
			} else {
				typeName = string(fd.Message().FullName())
			}
		}

		// 提取 SourceCodeInfo 注释
		comments := ""
		if parentFile := fd.ParentFile(); parentFile != nil {
			loc := parentFile.SourceLocations().ByDescriptor(fd)
			lead := strings.TrimSpace(loc.LeadingComments)
			trail := strings.TrimSpace(loc.TrailingComments)
			if lead != "" {
				comments = lead
			} else {
				comments = trail
			}
		}

		// 计算字段路径 fieldPath
		var fieldPath string
		var childPrefix string

		if parentPrefix == "" {
			if fd.IsList() && !isMap && fd.Kind() != protoreflect.MessageKind {
				fieldPath = fieldName + "[*]"
				childPrefix = fieldName + "[*]"
			} else if fd.IsList() && !isMap && fd.Kind() == protoreflect.MessageKind {
				fieldPath = fieldName
				childPrefix = fieldName + "[*]"
			} else if isMap {
				fieldPath = fieldName
				childPrefix = fieldName + "[*]"
			} else {
				fieldPath = fieldName
				childPrefix = fieldName
			}
		} else {
			if fd.IsList() && !isMap && fd.Kind() != protoreflect.MessageKind {
				fieldPath = parentPrefix + "." + fieldName + "[*]"
				childPrefix = parentPrefix + "." + fieldName + "[*]"
			} else if fd.IsList() && !isMap && fd.Kind() == protoreflect.MessageKind {
				fieldPath = parentPrefix + "." + fieldName
				childPrefix = parentPrefix + "." + fieldName + "[*]"
			} else if isMap {
				fieldPath = parentPrefix + "." + fieldName
				childPrefix = parentPrefix + "." + fieldName + "[*]"
			} else {
				fieldPath = parentPrefix + "." + fieldName
				childPrefix = parentPrefix + "." + fieldName
			}
		}

		isIgnored := checkPathIgnored(fieldPath, ignorePaths)

		node := ProtoFieldNodeDTO{
			Name:            fieldName,
			JSONName:        jsonName,
			JSONNameSnake:   jsonName,
			TagNumber:       tagNumber,
			TagNumberSnake:  tagNumber,
			Kind:            kindStr,
			TypeName:        typeName,
			TypeNameSnake:   typeName,
			Cardinality:     cardinalityStr,
			IsMap:           isMap,
			IsMapSnake:      isMap,
			Comments:        comments,
			FieldPath:       fieldPath,
			FieldPathSnake:  fieldPath,
			IsIgnoredInDiff: isIgnored,
			IsIgnoredSnake:  isIgnored,
		}

		// 针对子 Message 递归
		if fd.Kind() == protoreflect.MessageKind {
			if currentDepth >= MaxFieldTreeDepth {
				node.IsTruncated = true
				node.IsTruncatedSnk = true
			} else {
				if isMap {
					valFd := fd.MapValue()
					if valFd != nil && valFd.Kind() == protoreflect.MessageKind && valFd.Message() != nil {
						node.Children = extractMessageFieldsInternal(valFd.Message(), childPrefix, currentDepth+1, ignorePaths)
					}
				} else if fd.Message() != nil {
					node.Children = extractMessageFieldsInternal(fd.Message(), childPrefix, currentDepth+1, ignorePaths)
				}
			}
		}

		nodes = append(nodes, node)
	}

	return nodes
}

func mapFieldKindToString(k protoreflect.Kind) string {
	switch k {
	case protoreflect.BoolKind:
		return "BOOL"
	case protoreflect.Int32Kind, protoreflect.Sfixed32Kind:
		return "INT32"
	case protoreflect.Sint32Kind:
		return "SINT32"
	case protoreflect.Int64Kind, protoreflect.Sfixed64Kind:
		return "INT64"
	case protoreflect.Sint64Kind:
		return "SINT64"
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return "UINT32"
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return "UINT64"
	case protoreflect.FloatKind:
		return "FLOAT"
	case protoreflect.DoubleKind:
		return "DOUBLE"
	case protoreflect.StringKind:
		return "STRING"
	case protoreflect.BytesKind:
		return "BYTES"
	case protoreflect.EnumKind:
		return "ENUM"
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return "MESSAGE"
	default:
		return strings.ToUpper(k.String())
	}
}

func checkPathIgnored(path string, ignorePaths []string) bool {
	for _, pattern := range ignorePaths {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if pattern == path {
			return true
		}
		if pattern == path+"[*]" || pattern+"[*]" == path {
			return true
		}
		if strings.HasPrefix(path, pattern+".") || strings.HasPrefix(path, pattern+"[") {
			return true
		}
		if strings.Contains(pattern, "[*]") {
			regexPattern := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\[\*\]`, `(\[\d+\]|\[\*\])`) + "($|\\..*|\\[.*)"
			if matched, _ := regexp.MatchString(regexPattern, path); matched {
				return true
			}
		}
	}
	return false
}
