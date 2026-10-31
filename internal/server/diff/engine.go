package diff

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"code-traefik/internal/server/decoder"
	"code-traefik/models"
	"code-traefik/pkg/crosrpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

var (
	ErrForbiddenPrivateSession        = errors.New("403 Forbidden: 无权选用他人私有录制素材")
	ErrForbiddenPrivateAgent          = errors.New("403 Forbidden: 目标 Agent 属于他人私有，无权投递")
	ErrIncompatibleReplayTargetModule = errors.New("400 Bad Request: 目标进程未承载源请求所需的全部公共模块")
)

// FieldDiff Protobuf 语义树字段级差异明细
type FieldDiff struct {
	FieldPath string `json:"field_path"` // 如 "pricing.discount_cents" 或 "items[2].sku_id"
	DiffKind  string `json:"diff_kind"`  // "MODIFIED" | "ADDED" | "REMOVED"
	Expected  any    `json:"expected"`   // 64 位整数字符串化以防前端丢失精度
	Actual    any    `json:"actual"`
}

// DiffResult 双层比对综合结果
type DiffResult struct {
	Status         string      `json:"status"` // PASS | FAILED
	FailedLayer    int         `json:"failed_layer,omitempty"`
	FailureReason  string      `json:"failure_reason,omitempty"`
	StatusCodeDiff bool        `json:"status_code_diff,omitempty"`
	ExpectedStatus uint64      `json:"expected_status"`
	ActualStatus   uint64      `json:"actual_status"`
	FieldDiffs     []FieldDiff `json:"field_diffs,omitempty"`
}

// ValidateReplayProcessCompatibility 校验录制源进程选定报文能否安全重放到目标进程 targetProc (F-12.1, SC-07)
func ValidateReplayProcessCompatibility(
	callerID uint,
	isAdmin bool,
	session *models.RecordingSession,
	selectedRecords []models.TrafficRecord,
	sourceProc *models.Process,
	targetProc *models.Process,
	targetAgent *models.Agent,
	provideMap map[string]uint64, // key: "serviceName:operation" -> DstModuleID
) error {
	if session == nil || targetProc == nil || targetAgent == nil {
		return errors.New("400 Bad Request: 缺少必要参数")
	}

	// 1. 录制素材与目标 Agent 的公私域归属防越权校验 (Anti-IDOR)
	if session.Scope == models.ScopePrivate && session.OwnerID != callerID && !isAdmin {
		return ErrForbiddenPrivateSession
	}
	if targetAgent.Scope == models.ScopePrivate && (targetAgent.OwnerID == nil || *targetAgent.OwnerID != callerID) && !isAdmin {
		return ErrForbiddenPrivateAgent
	}

	// 2. 原地同进程重放天然兼容
	if sourceProc != nil && sourceProc.ID == targetProc.ID {
		return nil
	}

	// 3. 跨进程/跨 Agent 重放：目标进程必须完整承载选定请求命中的全部公共架构模块
	targetModSet := make(map[uint64]struct{})
	for _, mid := range targetProc.ModuleIDs {
		targetModSet[mid] = struct{}{}
	}

	requiredMods := make(map[uint64]struct{})
	var missingModuleIDs []uint64
	hasUnregisteredIface := false

	for _, rec := range selectedRecords {
		svc := strings.TrimSpace(rec.ServiceName)
		keyOp := svc + ":" + strings.TrimSpace(rec.Operation)
		if dstModID, ok := provideMap[svc]; ok {
			requiredMods[dstModID] = struct{}{}
		} else if dstModID, ok := provideMap[keyOp]; ok {
			requiredMods[dstModID] = struct{}{}
		} else {
			hasUnregisteredIface = true
		}
	}

	// 若包含未登记接口，退化要求源进程声明的模块集合非空且被目标进程完全覆盖
	if hasUnregisteredIface || len(requiredMods) == 0 {
		if sourceProc != nil {
			sourceMods := make(map[uint64]struct{})
			for _, mid := range sourceProc.ModuleIDs {
				sourceMods[mid] = struct{}{}
			}
			if len(sourceMods) == 0 {
				return ErrIncompatibleReplayTargetModule
			}
			for sm := range sourceMods {
				if _, ok := targetModSet[sm]; !ok {
					return fmt.Errorf("%w: 缺失源进程公共模块 ID=%d", ErrIncompatibleReplayTargetModule, sm)
				}
			}
			return nil
		}
		return ErrIncompatibleReplayTargetModule
	}

	for rm := range requiredMods {
		if _, ok := targetModSet[rm]; !ok {
			missingModuleIDs = append(missingModuleIDs, rm)
		}
	}

	if len(missingModuleIDs) > 0 {
		return fmt.Errorf("%w: 目标进程缺少公共模块 ID=%v", ErrIncompatibleReplayTargetModule, missingModuleIDs)
	}

	return nil
}

// DiffLayer1Header 第一层：CROS RPC 协议头与载荷原始字节快速比对 (F-14.1, F-14.3, SC-08)
// 固定豁免：ullServerRecvTime, ullServerSendTime, ullServerEnqueueTime, ullSNO, aucTraceBuff, ullClientSendTime
// 返回: (*DiffResult, payloadIdentical, error)
func DiffLayer1Header(expectedRspPacket, actualRspPacket []byte) (*DiffResult, bool, error) {
	if len(expectedRspPacket) == 0 || len(actualRspPacket) == 0 {
		return &DiffResult{
			Status:        models.ReplayResultFailed,
			FailedLayer:   1,
			FailureReason: "空应答帧",
		}, false, nil
	}

	parsedExp, errExp := crosrpc.ParsePacket(expectedRspPacket)
	parsedAct, errAct := crosrpc.ParsePacket(actualRspPacket)
	if errExp != nil || errAct != nil {
		return &DiffResult{
			Status:        models.ReplayResultFailed,
			FailedLayer:   1,
			FailureReason: fmt.Sprintf("协议帧解析错误: expErr=%v, actErr=%v", errExp, errAct),
		}, false, nil
	}

	expStatus := uint64(0)
	actStatus := uint64(0)
	if parsedExp.Rsp != nil {
		expStatus = parsedExp.Rsp.StatusCode
	}
	if parsedAct.Rsp != nil {
		actStatus = parsedAct.Rsp.StatusCode
	}

	// 1. 检查 MsgType
	if parsedExp.Comm.MsgType != parsedAct.Comm.MsgType {
		return &DiffResult{
			Status:         models.ReplayResultFailed,
			FailedLayer:    1,
			FailureReason:  fmt.Sprintf("消息类型 MsgType 不一致: 期望 %d, 实际 %d", parsedExp.Comm.MsgType, parsedAct.Comm.MsgType),
			ExpectedStatus: expStatus,
			ActualStatus:   actStatus,
		}, false, nil
	}

	// 2. 检查状态码差异
	if expStatus != actStatus {
		return &DiffResult{
			Status:         models.ReplayResultFailed,
			FailedLayer:    1,
			StatusCodeDiff: true,
			FailureReason:  fmt.Sprintf("业务状态码不一致: 期望 %d, 实际 %d", expStatus, actStatus),
			ExpectedStatus: expStatus,
			ActualStatus:   actStatus,
			FieldDiffs: []FieldDiff{
				{
					FieldPath: "header.status_code",
					DiffKind:  "MODIFIED",
					Expected:  expStatus,
					Actual:    actStatus,
				},
			},
		}, false, nil
	}

	// 3. 检查应答帧序号 RspMsgNo
	if parsedExp.Rsp != nil && parsedAct.Rsp != nil {
		if parsedExp.Rsp.RspMsgNo != parsedAct.Rsp.RspMsgNo {
			return &DiffResult{
				Status:         models.ReplayResultFailed,
				FailedLayer:    1,
				FailureReason:  fmt.Sprintf("分片序号 RspMsgNo 不一致: 期望 %d, 实际 %d", parsedExp.Rsp.RspMsgNo, parsedAct.Rsp.RspMsgNo),
				ExpectedStatus: expStatus,
				ActualStatus:   actStatus,
			}, false, nil
		}
	}

	// 4. 提取 Payload 原始字节进行快速相等性比对 (短路 PASS)
	expPayload := expectedRspPacket[parsedExp.PayloadOffset:]
	actPayload := actualRspPacket[parsedAct.PayloadOffset:]

	if bytes.Equal(expPayload, actPayload) {
		return &DiffResult{
			Status:         models.ReplayResultPass,
			ExpectedStatus: expStatus,
			ActualStatus:   actStatus,
		}, true, nil
	}

	// 协议头通过，但载荷字节存在差异，触发第二层
	return &DiffResult{
		Status:         models.ReplayResultPass,
		ExpectedStatus: expStatus,
		ActualStatus:   actStatus,
	}, false, nil
}

// DiffLayer2ProtoSemantic 第二层：protoreflect 类型感知语义树 Diff 算法 (F-14.2, F-14.4, F-14.5, SC-08)
func DiffLayer2ProtoSemantic(
	expectedPayload, actualPayload []byte,
	op *decoder.CompiledOperation,
	ignorePaths []string,
) ([]FieldDiff, error) {
	if op == nil || op.RspDesc == nil {
		// 无 IDL 降级为原始字节切片摘要比对
		return diffRawBytesFallback(expectedPayload, actualPayload), nil
	}

	desc := op.RspDesc
	msgExp := dynamicpb.NewMessage(desc)
	msgAct := dynamicpb.NewMessage(desc)

	if err := proto.Unmarshal(expectedPayload, msgExp); err != nil {
		return diffRawBytesFallback(expectedPayload, actualPayload), nil
	}
	if err := proto.Unmarshal(actualPayload, msgAct); err != nil {
		return diffRawBytesFallback(expectedPayload, actualPayload), nil
	}

	var diffs []FieldDiff

	// 1. 未知字段防漏判检出: Actual 包含 Expected 没有的未定义 Tag 字节
	unknownAct := msgAct.GetUnknown()
	unknownExp := msgExp.GetUnknown()
	if len(unknownAct) > 0 && !bytes.Equal(unknownExp, unknownAct) {
		diffs = append(diffs, FieldDiff{
			FieldPath: "<unknown_fields>",
			DiffKind:  "ADDED",
			Expected:  hex.EncodeToString(unknownExp),
			Actual:    hex.EncodeToString(unknownAct),
		})
	}

	// 2. 三级合并忽略规则: ReplayTask.ExtraIgnorePaths ∪ Operation.DiffIgnorePaths ∪ ModuleService.DiffIgnorePaths
	ignoreSet := make(map[string]struct{})
	for _, p := range ignorePaths {
		p = strings.TrimSpace(p)
		if p != "" {
			ignoreSet[p] = struct{}{}
		}
	}
	if op != nil {
		for _, p := range op.DiffIgnorePaths {
			p = strings.TrimSpace(p)
			if p != "" {
				ignoreSet[p] = struct{}{}
			}
		}
		for _, p := range op.ServiceIgnorePaths {
			p = strings.TrimSpace(p)
			if p != "" {
				ignoreSet[p] = struct{}{}
			}
		}
	}
	combinedIgnore := make([]string, 0, len(ignoreSet))
	for p := range ignoreSet {
		combinedIgnore = append(combinedIgnore, p)
	}

	// 3. 递归语义树比对
	semanticDiffs := diffMessageReflect(msgExp.ProtoReflect(), msgAct.ProtoReflect(), "", combinedIgnore)
	diffs = append(diffs, semanticDiffs...)

	return diffs, nil
}

func diffMessageReflect(
	mExp, mAct protoreflect.Message,
	prefix string,
	ignorePaths []string,
) []FieldDiff {
	var diffs []FieldDiff
	md := mExp.Descriptor()

	for i := 0; i < md.Fields().Len(); i++ {
		fd := md.Fields().Get(i)
		fieldName := string(fd.Name())
		fieldPath := fieldName
		if prefix != "" {
			fieldPath = prefix + "." + fieldName
		}

		if isPathIgnored(fieldPath, ignorePaths) {
			continue
		}

		hasExp := mExp.Has(fd)
		hasAct := mAct.Has(fd)

		// 双方均未赋值
		if !hasExp && !hasAct {
			continue
		}

		// (a) Map 类型比对 (F-14.5: 彻底免疫 Map 迭代乱序)
		if fd.IsMap() {
			mapExp := mExp.Get(fd).Map()
			mapAct := mAct.Get(fd).Map()
			mapDiffs := diffMapReflect(mapExp, mapAct, fd, fieldPath, ignorePaths)
			diffs = append(diffs, mapDiffs...)
			continue
		}

		// (b) Repeated List 类型比对 (F-14.5: 严格保序)
		if fd.IsList() {
			listExp := mExp.Get(fd).List()
			listAct := mAct.Get(fd).List()
			listDiffs := diffListReflect(listExp, listAct, fd, fieldPath, ignorePaths)
			diffs = append(diffs, listDiffs...)
			continue
		}

		// (c) 嵌套 Message 类型比对
		if fd.Kind() == protoreflect.MessageKind {
			if hasExp && !hasAct {
				diffs = append(diffs, FieldDiff{
					FieldPath: fieldPath,
					DiffKind:  "REMOVED",
					Expected:  "<message>",
					Actual:    nil,
				})
				continue
			}
			if !hasExp && hasAct {
				diffs = append(diffs, FieldDiff{
					FieldPath: fieldPath,
					DiffKind:  "ADDED",
					Expected:  nil,
					Actual:    "<message>",
				})
				continue
			}
			subDiffs := diffMessageReflect(mExp.Get(fd).Message(), mAct.Get(fd).Message(), fieldPath, ignorePaths)
			diffs = append(diffs, subDiffs...)
			continue
		}

		// (d) 标量与枚举比对
		valExp := mExp.Get(fd)
		valAct := mAct.Get(fd)

		if !hasExp && hasAct {
			diffs = append(diffs, FieldDiff{
				FieldPath: fieldPath,
				DiffKind:  "ADDED",
				Expected:  nil,
				Actual:    formatScalarValue(valAct, fd),
			})
			continue
		}
		if hasExp && !hasAct {
			diffs = append(diffs, FieldDiff{
				FieldPath: fieldPath,
				DiffKind:  "REMOVED",
				Expected:  formatScalarValue(valExp, fd),
				Actual:    nil,
			})
			continue
		}

		if !scalarsEqual(valExp, valAct, fd) {
			diffs = append(diffs, FieldDiff{
				FieldPath: fieldPath,
				DiffKind:  "MODIFIED",
				Expected:  formatScalarValue(valExp, fd),
				Actual:    formatScalarValue(valAct, fd),
			})
		}
	}

	return diffs
}

func diffMapReflect(
	mapExp, mapAct protoreflect.Map,
	fd protoreflect.FieldDescriptor,
	parentPath string,
	ignorePaths []string,
) []FieldDiff {
	var diffs []FieldDiff

	// 提取全部 key 并排序遍历
	allKeys := make(map[string]protoreflect.MapKey)
	mapExp.Range(func(k protoreflect.MapKey, v protoreflect.Value) bool {
		allKeys[k.String()] = k
		return true
	})
	mapAct.Range(func(k protoreflect.MapKey, v protoreflect.Value) bool {
		allKeys[k.String()] = k
		return true
	})

	var sortedKeys []string
	for k := range allKeys {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	valDesc := fd.MapValue()
	for _, kStr := range sortedKeys {
		mk := allKeys[kStr]
		entryPath := fmt.Sprintf("%s[%s]", parentPath, kStr)
		if isPathIgnored(entryPath, ignorePaths) {
			continue
		}

		hasExp := mapExp.Has(mk)
		hasAct := mapAct.Has(mk)

		if hasExp && !hasAct {
			diffs = append(diffs, FieldDiff{
				FieldPath: entryPath,
				DiffKind:  "REMOVED",
				Expected:  formatScalarValue(mapExp.Get(mk), valDesc),
				Actual:    nil,
			})
			continue
		}
		if !hasExp && hasAct {
			diffs = append(diffs, FieldDiff{
				FieldPath: entryPath,
				DiffKind:  "ADDED",
				Expected:  nil,
				Actual:    formatScalarValue(mapAct.Get(mk), valDesc),
			})
			continue
		}

		vExp := mapExp.Get(mk)
		vAct := mapAct.Get(mk)
		if valDesc.Kind() == protoreflect.MessageKind {
			subDiffs := diffMessageReflect(vExp.Message(), vAct.Message(), entryPath, ignorePaths)
			diffs = append(diffs, subDiffs...)
		} else if !scalarsEqual(vExp, vAct, valDesc) {
			diffs = append(diffs, FieldDiff{
				FieldPath: entryPath,
				DiffKind:  "MODIFIED",
				Expected:  formatScalarValue(vExp, valDesc),
				Actual:    formatScalarValue(vAct, valDesc),
			})
		}
	}
	return diffs
}

func diffListReflect(
	listExp, listAct protoreflect.List,
	fd protoreflect.FieldDescriptor,
	parentPath string,
	ignorePaths []string,
) []FieldDiff {
	var diffs []FieldDiff
	lenExp := listExp.Len()
	lenAct := listAct.Len()
	maxLen := lenExp
	if lenAct > maxLen {
		maxLen = lenAct
	}

	for i := 0; i < maxLen; i++ {
		elemPath := fmt.Sprintf("%s[%d]", parentPath, i)
		if isPathIgnored(elemPath, ignorePaths) {
			continue
		}

		if i >= lenExp {
			diffs = append(diffs, FieldDiff{
				FieldPath: elemPath,
				DiffKind:  "ADDED",
				Expected:  nil,
				Actual:    formatScalarValue(listAct.Get(i), fd),
			})
			continue
		}
		if i >= lenAct {
			diffs = append(diffs, FieldDiff{
				FieldPath: elemPath,
				DiffKind:  "REMOVED",
				Expected:  formatScalarValue(listExp.Get(i), fd),
				Actual:    nil,
			})
			continue
		}

		vExp := listExp.Get(i)
		vAct := listAct.Get(i)
		if fd.Kind() == protoreflect.MessageKind {
			subDiffs := diffMessageReflect(vExp.Message(), vAct.Message(), elemPath, ignorePaths)
			diffs = append(diffs, subDiffs...)
		} else if !scalarsEqual(vExp, vAct, fd) {
			diffs = append(diffs, FieldDiff{
				FieldPath: elemPath,
				DiffKind:  "MODIFIED",
				Expected:  formatScalarValue(vExp, fd),
				Actual:    formatScalarValue(vAct, fd),
			})
		}
	}
	return diffs
}

func scalarsEqual(a, b protoreflect.Value, fd protoreflect.FieldDescriptor) bool {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return a.Bool() == b.Bool()
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return a.Int() == b.Int()
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return a.Int() == b.Int()
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return a.Uint() == b.Uint()
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return a.Uint() == b.Uint()
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		fa := a.Float()
		fb := b.Float()
		if math.IsNaN(fa) && math.IsNaN(fb) {
			return true
		}
		return fa == fb
	case protoreflect.StringKind:
		return a.String() == b.String()
	case protoreflect.BytesKind:
		return bytes.Equal(a.Bytes(), b.Bytes())
	case protoreflect.EnumKind:
		return a.Enum() == b.Enum()
	default:
		return a.Interface() == b.Interface()
	}
}

func formatScalarValue(v protoreflect.Value, fd protoreflect.FieldDescriptor) any {
	if !v.IsValid() {
		return nil
	}
	switch fd.Kind() {
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return strconv.FormatInt(v.Int(), 10)
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return strconv.FormatUint(v.Uint(), 10)
	case protoreflect.BytesKind:
		return hex.EncodeToString(v.Bytes())
	case protoreflect.EnumKind:
		return int32(v.Enum())
	default:
		return v.Interface()
	}
}

func isPathIgnored(path string, ignorePaths []string) bool {
	for _, pattern := range ignorePaths {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if pattern == path {
			return true
		}
		// 1. 前缀子树匹配: 如 "header" 匹配 "header.timestamp" 或 "header[0]"
		if strings.HasPrefix(path, pattern+".") || strings.HasPrefix(path, pattern+"[") {
			return true
		}
		// 2. 前缀通配匹配: 如 "header.*" 匹配 "header.timestamp"
		if strings.HasSuffix(pattern, ".*") {
			prefix := strings.TrimSuffix(pattern, ".*")
			if path == prefix || strings.HasPrefix(path, prefix+".") {
				return true
			}
		}
		// 3. 通配符支持: 如 "orders[*].sku" 匹配 "orders[0].sku" 或 "orders[*].sku"
		if strings.Contains(pattern, "[*]") {
			regexPattern := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\[\*\]`, `(\[\d+\]|\[\*\])`) + "$"
			if matched, _ := regexp.MatchString(regexPattern, path); matched {
				return true
			}
		}
	}
	return false
}

func diffRawBytesFallback(expected, actual []byte) []FieldDiff {
	firstDiff := -1
	minLen := len(expected)
	if len(actual) < minLen {
		minLen = len(actual)
	}
	for i := 0; i < minLen; i++ {
		if expected[i] != actual[i] {
			firstDiff = i
			break
		}
	}
	if firstDiff == -1 && len(expected) != len(actual) {
		firstDiff = minLen
	}

	return []FieldDiff{
		{
			FieldPath: "<raw_bytes>",
			DiffKind:  "MODIFIED",
			Expected:  fmt.Sprintf("length=%d, first_diff_offset=%d", len(expected), firstDiff),
			Actual:    fmt.Sprintf("length=%d", len(actual)),
		},
	}
}

// AggregateReplayStatus 遵循全序优先级 ERROR > TIMEOUT > MOCK_MISS > FAILED > PASS 聚合任务终态
func AggregateReplayStatus(statuses []string) string {
	hasFailed := false
	hasMockMiss := false
	hasTimeout := false
	hasError := false

	for _, s := range statuses {
		switch s {
		case models.ReplayResultError:
			hasError = true
		case models.ReplayResultTimeout:
			hasTimeout = true
		case models.ReplayResultMockMiss:
			hasMockMiss = true
		case models.ReplayResultFailed:
			hasFailed = true
		}
	}

	if hasError {
		return models.ReplayResultError
	}
	if hasTimeout {
		return models.ReplayResultTimeout
	}
	if hasMockMiss {
		return models.ReplayResultMockMiss
	}
	if hasFailed {
		return models.ReplayResultFailed
	}
	return models.ReplayResultPass
}
