package decoder

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"code-traefik/models"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"gorm.io/gorm"
)

// 6 类 FATAL 级破坏性规则常量 (M16 / ADR-14)
const (
	RuleServiceOrMethodRemoved = "SERVICE_OR_METHOD_REMOVED"
	RuleMethodSignatureChanged = "METHOD_SIGNATURE_CHANGED"
	RuleTagMutation            = "TAG_MUTATION"
	RuleTypeIncompatible       = "TYPE_INCOMPATIBLE"
	RuleRemovedWithoutReserve  = "REMOVED_WITHOUT_RESERVE"
	RuleEnumBreakingChange     = "ENUM_BREAKING_CHANGE"
)

// BreakingRuleViolation 单项 AST 破坏性违规项
type BreakingRuleViolation struct {
	Rule        string `json:"rule"`
	ProtoFile   string `json:"proto_file"`
	Symbol      string `json:"symbol"`
	Message     string `json:"message"`
	ServiceName string `json:"service_name,omitempty"`
}

// AffectedDependent 关联下游依赖模块 DTO
type AffectedDependent struct {
	ModuleID        uint64 `json:"module_id"`
	ModuleCode      string `json:"module_code"`
	ModuleName      string `json:"module_name"`
	ArchProcessCode string `json:"arch_process_code"`
	OwnerName       string `json:"owner_name"`
}

// SchemaBreakingError 激活门禁检出破坏性变更阻断错误
type SchemaBreakingError struct {
	BreakingRules      []BreakingRuleViolation `json:"breaking_rules"`
	AffectedDependents []AffectedDependent     `json:"affected_dependents"`
}

func (e *SchemaBreakingError) Error() string {
	return fmt.Sprintf("检测到 %d 项破坏性 Protobuf 契约变更，已阻断激活", len(e.BreakingRules))
}

type serviceEntry struct {
	FullName   string
	ProtoFile  string
	Descriptor *descriptorpb.ServiceDescriptorProto
}

type messageEntry struct {
	FullName   string
	ProtoFile  string
	Descriptor *descriptorpb.DescriptorProto
}

type enumEntry struct {
	FullName   string
	ProtoFile  string
	Descriptor *descriptorpb.EnumDescriptorProto
}

func normalizeTypeName(s string) string {
	return strings.TrimPrefix(strings.TrimSpace(s), ".")
}

// DetectBreakingChanges 对比既有生效版本与候选版本的自有 .proto AST 结构，检出 6 类 FATAL 违规
func DetectBreakingChanges(oldFiles, newFiles []*descriptorpb.FileDescriptorProto) []BreakingRuleViolation {
	var violations []BreakingRuleViolation

	oldServices := make(map[string]*serviceEntry)
	oldMessages := make(map[string]*messageEntry)
	oldEnums := make(map[string]*enumEntry)

	newServices := make(map[string]*serviceEntry)
	newMessages := make(map[string]*messageEntry)
	newEnums := make(map[string]*enumEntry)

	// 1. 递归提取 Old AST 符号表
	for _, f := range oldFiles {
		collectSymbols(f, oldServices, oldMessages, oldEnums)
	}
	// 2. 递归提取 New AST 符号表
	for _, f := range newFiles {
		collectSymbols(f, newServices, newMessages, newEnums)
	}

	// 3. 构建血缘引用反向图，以便受损 Message/Enum 自动关联对应的 ServiceName
	serviceMapForSymbol := buildServiceMapForSymbols(oldFiles, oldServices, oldMessages, oldEnums)

	resolveService := func(symbol string) string {
		norm := normalizeTypeName(symbol)
		if svc, ok := serviceMapForSymbol[norm]; ok && svc != "" {
			return svc
		}
		// 尝试剥离最末字段名再匹配
		idx := strings.LastIndex(norm, ".")
		if idx > 0 {
			parent := norm[:idx]
			if svc, ok := serviceMapForSymbol[parent]; ok && svc != "" {
				return svc
			}
		}
		return ""
	}

	// 4. 规则 1 & 2: SERVICE_OR_METHOD_REMOVED 与 METHOD_SIGNATURE_CHANGED
	for oldSvcName, oldSvc := range oldServices {
		newSvc, exists := newServices[oldSvcName]
		if !exists {
			violations = append(violations, BreakingRuleViolation{
				Rule:        RuleServiceOrMethodRemoved,
				ProtoFile:   oldSvc.ProtoFile,
				Symbol:      oldSvc.FullName,
				Message:     fmt.Sprintf("服务 '%s' 已被删除", oldSvc.FullName),
				ServiceName: oldSvc.FullName,
			})
			continue
		}

		newMethods := make(map[string]*descriptorpb.MethodDescriptorProto)
		for _, m := range newSvc.Descriptor.Method {
			newMethods[m.GetName()] = m
		}

		for _, oldM := range oldSvc.Descriptor.Method {
			mName := oldM.GetName()
			newM, ok := newMethods[mName]
			if !ok {
				violations = append(violations, BreakingRuleViolation{
					Rule:        RuleServiceOrMethodRemoved,
					ProtoFile:   oldSvc.ProtoFile,
					Symbol:      fmt.Sprintf("%s.%s", oldSvc.FullName, mName),
					Message:     fmt.Sprintf("RPC 方法 '%s' 已在服务 '%s' 中被删除", mName, oldSvc.FullName),
					ServiceName: oldSvc.FullName,
				})
				continue
			}

			methodSymbol := fmt.Sprintf("%s.%s", oldSvc.FullName, mName)

			// 检查入参变异
			if normalizeTypeName(oldM.GetInputType()) != normalizeTypeName(newM.GetInputType()) {
				violations = append(violations, BreakingRuleViolation{
					Rule:        RuleMethodSignatureChanged,
					ProtoFile:   oldSvc.ProtoFile,
					Symbol:      methodSymbol,
					Message:     fmt.Sprintf("RPC 方法 '%s' 入参类型从 '%s' 变更为 '%s'", mName, normalizeTypeName(oldM.GetInputType()), normalizeTypeName(newM.GetInputType())),
					ServiceName: oldSvc.FullName,
				})
			}
			// 检查出参变异
			if normalizeTypeName(oldM.GetOutputType()) != normalizeTypeName(newM.GetOutputType()) {
				violations = append(violations, BreakingRuleViolation{
					Rule:        RuleMethodSignatureChanged,
					ProtoFile:   oldSvc.ProtoFile,
					Symbol:      methodSymbol,
					Message:     fmt.Sprintf("RPC 方法 '%s' 出参类型从 '%s' 变更为 '%s'", mName, normalizeTypeName(oldM.GetOutputType()), normalizeTypeName(newM.GetOutputType())),
					ServiceName: oldSvc.FullName,
				})
			}
			// 检查 client_streaming
			if oldM.GetClientStreaming() != newM.GetClientStreaming() {
				violations = append(violations, BreakingRuleViolation{
					Rule:        RuleMethodSignatureChanged,
					ProtoFile:   oldSvc.ProtoFile,
					Symbol:      methodSymbol,
					Message:     fmt.Sprintf("RPC 方法 '%s' client_streaming 模式从 %t 变更为 %t", mName, oldM.GetClientStreaming(), newM.GetClientStreaming()),
					ServiceName: oldSvc.FullName,
				})
			}
			// 检查 server_streaming
			if oldM.GetServerStreaming() != newM.GetServerStreaming() {
				violations = append(violations, BreakingRuleViolation{
					Rule:        RuleMethodSignatureChanged,
					ProtoFile:   oldSvc.ProtoFile,
					Symbol:      methodSymbol,
					Message:     fmt.Sprintf("RPC 方法 '%s' server_streaming 模式从 %t 变更为 %t", mName, oldM.GetServerStreaming(), newM.GetServerStreaming()),
					ServiceName: oldSvc.FullName,
				})
			}
		}
	}

	// 5. 规则 3、4、5: TAG_MUTATION、TYPE_INCOMPATIBLE、REMOVED_WITHOUT_RESERVE
	for msgName, oldMsg := range oldMessages {
		if oldMsg.Descriptor.GetOptions().GetMapEntry() {
			continue // MapEntry 消息由其宿主字段比对
		}
		newMsg, exists := newMessages[msgName]
		if !exists {
			// 消息被直接移除
			for _, f := range oldMsg.Descriptor.Field {
				violations = append(violations, BreakingRuleViolation{
					Rule:        RuleRemovedWithoutReserve,
					ProtoFile:   oldMsg.ProtoFile,
					Symbol:      fmt.Sprintf("%s.%s", oldMsg.FullName, f.GetName()),
					Message:     fmt.Sprintf("所属消息 '%s' 已被删除，字段 '%s' (Tag: %d) 未被保留", oldMsg.FullName, f.GetName(), f.GetNumber()),
					ServiceName: resolveService(oldMsg.FullName),
				})
			}
			continue
		}

		oldFieldsByName := make(map[string]*descriptorpb.FieldDescriptorProto)
		oldFieldsByTag := make(map[int32]*descriptorpb.FieldDescriptorProto)
		for _, f := range oldMsg.Descriptor.Field {
			oldFieldsByName[f.GetName()] = f
			oldFieldsByTag[f.GetNumber()] = f
		}

		newFieldsByName := make(map[string]*descriptorpb.FieldDescriptorProto)
		newFieldsByTag := make(map[int32]*descriptorpb.FieldDescriptorProto)
		for _, f := range newMsg.Descriptor.Field {
			newFieldsByName[f.GetName()] = f
			newFieldsByTag[f.GetNumber()] = f
		}

		// 3a. 同名字段 Tag 篡改
		for fname, oldF := range oldFieldsByName {
			if newF, ok := newFieldsByName[fname]; ok {
				if oldF.GetNumber() != newF.GetNumber() {
					violations = append(violations, BreakingRuleViolation{
						Rule:        RuleTagMutation,
						ProtoFile:   newMsg.ProtoFile,
						Symbol:      fmt.Sprintf("%s.%s", oldMsg.FullName, fname),
						Message:     fmt.Sprintf("字段 '%s' 的 Tag 从 %d 变更为 %d", fname, oldF.GetNumber(), newF.GetNumber()),
						ServiceName: resolveService(oldMsg.FullName),
					})
				}
			}
		}

		// 3b. 原数字 Tag 被不同字段复用
		for tag, newF := range newFieldsByTag {
			if oldF, ok := oldFieldsByTag[tag]; ok {
				if oldF.GetName() != newF.GetName() {
					violations = append(violations, BreakingRuleViolation{
						Rule:        RuleTagMutation,
						ProtoFile:   newMsg.ProtoFile,
						Symbol:      fmt.Sprintf("%s.%s", newMsg.FullName, newF.GetName()),
						Message:     fmt.Sprintf("数字 Tag %d 原为字段 '%s'，在新版中被字段 '%s' 复用", tag, oldF.GetName(), newF.GetName()),
						ServiceName: resolveService(oldMsg.FullName),
					})
				}
			}
		}

		// 4. TYPE_INCOMPATIBLE 比对
		for fname, oldF := range oldFieldsByName {
			newF, ok := newFieldsByName[fname]
			if !ok {
				continue
			}
			fieldSymbol := fmt.Sprintf("%s.%s", oldMsg.FullName, fname)
			svcName := resolveService(oldMsg.FullName)

			// 检查单值与 repeated 之间变异
			oldRepeated := oldF.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED
			newRepeated := newF.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED
			if oldRepeated != newRepeated {
				violations = append(violations, BreakingRuleViolation{
					Rule:        RuleTypeIncompatible,
					ProtoFile:   newMsg.ProtoFile,
					Symbol:      fieldSymbol,
					Message:     fmt.Sprintf("字段 '%s' 基数标签在单值与 repeated 之间发生不兼容变更", fname),
					ServiceName: svcName,
				})
			}

			// 检查 Map 结构变异
			oldMapEntry := findMapEntry(oldF, oldMessages)
			newMapEntry := findMapEntry(newF, newMessages)
			if (oldMapEntry != nil) != (newMapEntry != nil) {
				violations = append(violations, BreakingRuleViolation{
					Rule:        RuleTypeIncompatible,
					ProtoFile:   newMsg.ProtoFile,
					Symbol:      fieldSymbol,
					Message:     fmt.Sprintf("字段 '%s' 的 map 结构发生不兼容变异", fname),
					ServiceName: svcName,
				})
			} else if oldMapEntry != nil && newMapEntry != nil {
				oldK, oldV := getMapEntryTypes(oldMapEntry)
				newK, newV := getMapEntryTypes(newMapEntry)
				if oldK != newK || oldV != newV {
					violations = append(violations, BreakingRuleViolation{
						Rule:        RuleTypeIncompatible,
						ProtoFile:   newMsg.ProtoFile,
						Symbol:      fieldSymbol,
						Message:     fmt.Sprintf("Map 字段 '%s' 的键值类型发生不兼容变更 (旧: map<%s,%s>, 新: map<%s,%s>)", fname, oldK, oldV, newK, newV),
						ServiceName: svcName,
					})
				}
			} else {
				// 检查标量或消息/枚举类型
				if oldF.GetType() != newF.GetType() {
					violations = append(violations, BreakingRuleViolation{
						Rule:        RuleTypeIncompatible,
						ProtoFile:   newMsg.ProtoFile,
						Symbol:      fieldSymbol,
						Message:     fmt.Sprintf("字段 '%s' 类型从 %s 变更为 %s", fname, oldF.GetType().String(), newF.GetType().String()),
						ServiceName: svcName,
					})
				} else if (oldF.GetType() == descriptorpb.FieldDescriptorProto_TYPE_MESSAGE || oldF.GetType() == descriptorpb.FieldDescriptorProto_TYPE_ENUM) &&
					normalizeTypeName(oldF.GetTypeName()) != normalizeTypeName(newF.GetTypeName()) {
					violations = append(violations, BreakingRuleViolation{
						Rule:        RuleTypeIncompatible,
						ProtoFile:   newMsg.ProtoFile,
						Symbol:      fieldSymbol,
						Message:     fmt.Sprintf("字段 '%s' 引用的消息/枚举类型从 '%s' 变更为 '%s'", fname, normalizeTypeName(oldF.GetTypeName()), normalizeTypeName(newF.GetTypeName())),
						ServiceName: svcName,
					})
				}
			}

			// 检查 oneof 拆并
			oldOneof := getOneofName(oldMsg.Descriptor, oldF)
			newOneof := getOneofName(newMsg.Descriptor, newF)
			if (oldOneof != "") != (newOneof != "") {
				violations = append(violations, BreakingRuleViolation{
					Rule:        RuleTypeIncompatible,
					ProtoFile:   newMsg.ProtoFile,
					Symbol:      fieldSymbol,
					Message:     fmt.Sprintf("字段 '%s' 的 oneof 归属发生不兼容变异", fname),
					ServiceName: svcName,
				})
			} else if oldOneof != "" && newOneof != "" && oldOneof != newOneof {
				violations = append(violations, BreakingRuleViolation{
					Rule:        RuleTypeIncompatible,
					ProtoFile:   newMsg.ProtoFile,
					Symbol:      fieldSymbol,
					Message:     fmt.Sprintf("字段 '%s' 从 oneof '%s' 迁移至 oneof '%s'", fname, oldOneof, newOneof),
					ServiceName: svcName,
				})
			}
		}

		// 5. REMOVED_WITHOUT_RESERVE: 删除字段但未落入 ReservedRange [Start, End) 闭开区间
		for fname, oldF := range oldFieldsByName {
			if _, ok := newFieldsByName[fname]; !ok {
				tag := oldF.GetNumber()
				if !isTagCoveredByReservedRange(newMsg.Descriptor.GetReservedRange(), tag) {
					violations = append(violations, BreakingRuleViolation{
						Rule:        RuleRemovedWithoutReserve,
						ProtoFile:   oldMsg.ProtoFile,
						Symbol:      fmt.Sprintf("%s.%s", oldMsg.FullName, fname),
						Message:     fmt.Sprintf("已删除字段 '%s' (Tag: %d) 未在保留范围 reserved 中声明数字 Tag", fname, tag),
						ServiceName: resolveService(oldMsg.FullName),
					})
				}
			}
		}
	}

	// 6. 规则 6: ENUM_BREAKING_CHANGE
	for enumName, oldEnum := range oldEnums {
		newEnum, exists := newEnums[enumName]
		if !exists {
			violations = append(violations, BreakingRuleViolation{
				Rule:        RuleEnumBreakingChange,
				ProtoFile:   oldEnum.ProtoFile,
				Symbol:      oldEnum.FullName,
				Message:     fmt.Sprintf("枚举 '%s' 已被删除", oldEnum.FullName),
				ServiceName: resolveService(oldEnum.FullName),
			})
			continue
		}

		oldValsByName := make(map[string]*descriptorpb.EnumValueDescriptorProto)
		for _, v := range oldEnum.Descriptor.Value {
			oldValsByName[v.GetName()] = v
		}
		newValsByName := make(map[string]*descriptorpb.EnumValueDescriptorProto)
		for _, v := range newEnum.Descriptor.Value {
			newValsByName[v.GetName()] = v
		}

		for vname, oldV := range oldValsByName {
			if newV, ok := newValsByName[vname]; ok {
				if oldV.GetNumber() != newV.GetNumber() {
					violations = append(violations, BreakingRuleViolation{
						Rule:        RuleEnumBreakingChange,
						ProtoFile:   newEnum.ProtoFile,
						Symbol:      fmt.Sprintf("%s.%s", oldEnum.FullName, vname),
						Message:     fmt.Sprintf("枚举项 '%s' 的数值从 %d 变更为 %d", vname, oldV.GetNumber(), newV.GetNumber()),
						ServiceName: resolveService(oldEnum.FullName),
					})
				}
			} else {
				// 枚举值被删除，检查是否在 EnumReservedRange [Start, End] 闭区间内保留
				val := oldV.GetNumber()
				if !isEnumValueCoveredByReservedRange(newEnum.Descriptor.GetReservedRange(), val) {
					violations = append(violations, BreakingRuleViolation{
						Rule:        RuleEnumBreakingChange,
						ProtoFile:   oldEnum.ProtoFile,
						Symbol:      fmt.Sprintf("%s.%s", oldEnum.FullName, vname),
						Message:     fmt.Sprintf("已删除枚举项 '%s' (数值: %d) 未在 reserved 中保留数值", vname, val),
						ServiceName: resolveService(oldEnum.FullName),
					})
				}
			}
		}
	}

	return violations
}

// 辅助方法：递归提取文件内全部服务、消息与枚举
func collectSymbols(
	file *descriptorpb.FileDescriptorProto,
	services map[string]*serviceEntry,
	messages map[string]*messageEntry,
	enums map[string]*enumEntry,
) {
	pkg := file.GetPackage()
	fileName := file.GetName()

	for _, svc := range file.Service {
		fullName := svc.GetName()
		if pkg != "" {
			fullName = pkg + "." + svc.GetName()
		}
		services[fullName] = &serviceEntry{
			FullName:   fullName,
			ProtoFile:  fileName,
			Descriptor: svc,
		}
	}

	for _, enum := range file.EnumType {
		fullName := enum.GetName()
		if pkg != "" {
			fullName = pkg + "." + enum.GetName()
		}
		enums[fullName] = &enumEntry{
			FullName:   fullName,
			ProtoFile:  fileName,
			Descriptor: enum,
		}
	}

	for _, msg := range file.MessageType {
		collectNestedSymbols(pkg, msg, fileName, messages, enums)
	}
}

func collectNestedSymbols(
	prefix string,
	msg *descriptorpb.DescriptorProto,
	fileName string,
	messages map[string]*messageEntry,
	enums map[string]*enumEntry,
) {
	fullName := msg.GetName()
	if prefix != "" {
		fullName = prefix + "." + msg.GetName()
	}

	messages[fullName] = &messageEntry{
		FullName:   fullName,
		ProtoFile:  fileName,
		Descriptor: msg,
	}

	for _, nestedMsg := range msg.NestedType {
		collectNestedSymbols(fullName, nestedMsg, fileName, messages, enums)
	}

	for _, nestedEnum := range msg.EnumType {
		enumFull := fullName + "." + nestedEnum.GetName()
		enums[enumFull] = &enumEntry{
			FullName:   enumFull,
			ProtoFile:  fileName,
			Descriptor: nestedEnum,
		}
	}
}

// 辅助方法：构建消息/枚举到顶层 RPC Service 的引用推导图 (BFS)
func buildServiceMapForSymbols(
	files []*descriptorpb.FileDescriptorProto,
	services map[string]*serviceEntry,
	messages map[string]*messageEntry,
	enums map[string]*enumEntry,
) map[string]string {
	// msg/enum -> []service
	directServices := make(map[string][]string)
	// target -> []referencingMessage
	msgRefs := make(map[string][]string)

	for _, svc := range services {
		for _, m := range svc.Descriptor.Method {
			in := normalizeTypeName(m.GetInputType())
			out := normalizeTypeName(m.GetOutputType())
			if in != "" {
				directServices[in] = append(directServices[in], svc.FullName)
			}
			if out != "" {
				directServices[out] = append(directServices[out], svc.FullName)
			}
		}
	}

	for msgName, msg := range messages {
		for _, f := range msg.Descriptor.Field {
			if f.GetType() == descriptorpb.FieldDescriptorProto_TYPE_MESSAGE || f.GetType() == descriptorpb.FieldDescriptorProto_TYPE_ENUM {
				target := normalizeTypeName(f.GetTypeName())
				if target != "" {
					msgRefs[target] = append(msgRefs[target], msgName)
				}
			}
		}
	}

	result := make(map[string]string)
	for sName := range services {
		result[sName] = sName
	}

	// 为所有 Message / Enum 进行 BFS 搜索关联到的第一个 ServiceName
	for target := range messages {
		if svc := findServiceForTarget(target, directServices, msgRefs); svc != "" {
			result[target] = svc
		}
	}
	for target := range enums {
		if svc := findServiceForTarget(target, directServices, msgRefs); svc != "" {
			result[target] = svc
		}
	}

	return result
}

func findServiceForTarget(start string, directServices map[string][]string, msgRefs map[string][]string) string {
	visited := make(map[string]bool)
	queue := []string{start}
	visited[start] = true

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		if svcs, ok := directServices[curr]; ok && len(svcs) > 0 {
			return svcs[0]
		}

		for _, parent := range msgRefs[curr] {
			if !visited[parent] {
				visited[parent] = true
				queue = append(queue, parent)
			}
		}

		// 若当前是嵌套消息，也可向上推导至外层消息
		idx := strings.LastIndex(curr, ".")
		if idx > 0 {
			outer := curr[:idx]
			if !visited[outer] {
				visited[outer] = true
				queue = append(queue, outer)
			}
		}
	}
	return ""
}

func isTagCoveredByReservedRange(ranges []*descriptorpb.DescriptorProto_ReservedRange, tag int32) bool {
	// [Start, End) 闭开区间
	for _, r := range ranges {
		if tag >= r.GetStart() && tag < r.GetEnd() {
			return true
		}
	}
	return false
}

func isEnumValueCoveredByReservedRange(ranges []*descriptorpb.EnumDescriptorProto_EnumReservedRange, val int32) bool {
	// [Start, End] 闭闭区间
	for _, r := range ranges {
		if val >= r.GetStart() && val <= r.GetEnd() {
			return true
		}
	}
	return false
}

func getOneofName(msg *descriptorpb.DescriptorProto, f *descriptorpb.FieldDescriptorProto) string {
	if f.OneofIndex != nil {
		idx := int(f.GetOneofIndex())
		if idx >= 0 && idx < len(msg.OneofDecl) {
			return msg.OneofDecl[idx].GetName()
		}
	}
	return ""
}

func findMapEntry(f *descriptorpb.FieldDescriptorProto, messages map[string]*messageEntry) *descriptorpb.DescriptorProto {
	if f.GetLabel() != descriptorpb.FieldDescriptorProto_LABEL_REPEATED || f.GetType() != descriptorpb.FieldDescriptorProto_TYPE_MESSAGE {
		return nil
	}
	targetName := normalizeTypeName(f.GetTypeName())
	if entry, ok := messages[targetName]; ok && entry.Descriptor.GetOptions().GetMapEntry() {
		return entry.Descriptor
	}
	return nil
}

func getMapEntryTypes(entry *descriptorpb.DescriptorProto) (string, string) {
	var kType, vType string
	for _, f := range entry.Field {
		if f.GetName() == "key" {
			kType = f.GetType().String()
		} else if f.GetName() == "value" {
			vType = f.GetType().String()
			if f.GetType() == descriptorpb.FieldDescriptorProto_TYPE_MESSAGE || f.GetType() == descriptorpb.FieldDescriptorProto_TYPE_ENUM {
				vType = normalizeTypeName(f.GetTypeName())
			}
		}
	}
	return kType, vType
}

// ExtractOwnFilesFromSchema 从已保存的 ProtoSchema 模型中提取其自有 .proto 的 FileDescriptorProto 集合
func ExtractOwnFilesFromSchema(schema *models.ProtoSchema) ([]*descriptorpb.FileDescriptorProto, error) {
	if schema == nil || len(schema.DescriptorBytes) == 0 {
		return nil, errors.New("描述集二进制为空")
	}
	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(schema.DescriptorBytes, &fds); err != nil {
		return nil, fmt.Errorf("解析描述集二进制失败: %w", err)
	}

	var protoFileNames []string
	if len(schema.ProtoFiles) > 0 {
		_ = json.Unmarshal(schema.ProtoFiles, &protoFileNames)
	}
	if len(protoFileNames) == 0 {
		return fds.File, nil
	}

	nameSet := make(map[string]struct{}, len(protoFileNames))
	for _, fn := range protoFileNames {
		nameSet[fn] = struct{}{}
	}

	var own []*descriptorpb.FileDescriptorProto
	for _, f := range fds.File {
		if _, ok := nameSet[f.GetName()]; ok {
			own = append(own, f)
		}
	}
	if len(own) == 0 {
		return fds.File, nil
	}
	return own, nil
}

// FindAffectedDependents 联合查询 traefik_module_services 与 traefik_module_dependencies 提取下游受影响模块血缘
func FindAffectedDependents(db *gorm.DB, violations []BreakingRuleViolation) []AffectedDependent {
	if db == nil || len(violations) == 0 {
		return []AffectedDependent{}
	}

	svcSet := make(map[string]struct{})
	for _, v := range violations {
		if v.ServiceName != "" {
			svcSet[v.ServiceName] = struct{}{}
		}
	}
	if len(svcSet) == 0 {
		return []AffectedDependent{}
	}

	services := make([]string, 0, len(svcSet))
	for s := range svcSet {
		services = append(services, s)
	}

	var modSvcs []models.ModuleService
	if err := db.Where("service_name IN ?", services).Find(&modSvcs).Error; err != nil || len(modSvcs) == 0 {
		return []AffectedDependent{}
	}

	svcIDs := make([]uint64, 0, len(modSvcs))
	for _, ms := range modSvcs {
		svcIDs = append(svcIDs, ms.ID)
	}

	var deps []models.ModuleDependency
	if err := db.Preload("ConsumerModule.ArchProcess").Preload("ConsumerModule.Owner").
		Where("service_id IN ?", svcIDs).Find(&deps).Error; err != nil {
		return []AffectedDependent{}
	}

	dedup := make(map[uint64]AffectedDependent)
	for _, dep := range deps {
		cm := dep.ConsumerModule
		if cm == nil {
			continue
		}
		archCode := ""
		if cm.ArchProcess != nil {
			archCode = cm.ArchProcess.Code
		}
		ownerName := ""
		if cm.Owner != nil {
			if cm.Owner.Name != "" {
				ownerName = cm.Owner.Name
			} else {
				ownerName = cm.Owner.Username
			}
		}

		dedup[cm.ID] = AffectedDependent{
			ModuleID:        cm.ID,
			ModuleCode:      cm.Code,
			ModuleName:      cm.Name,
			ArchProcessCode: archCode,
			OwnerName:       ownerName,
		}
	}

	result := make([]AffectedDependent, 0, len(dedup))
	for _, ad := range dedup {
		result = append(result, ad)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ModuleID < result[j].ModuleID
	})
	return result
}
