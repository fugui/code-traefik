package models

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"

	commonModels "code-common/backend/models"
)

// 复用 code-common 公共基础模型
type User = commonModels.User
type Department = commonModels.Department
type SysAuditLog = commonModels.SysAuditLog

// 公共领域常量
const (
	ScopeShared  = "SHARED"  // 团队共享公共资源
	ScopePrivate = "PRIVATE" // 用户个人私有资源

	AgentStatusOnline  = "ONLINE"
	AgentStatusOffline = "OFFLINE"

	AgentLeaseNone      = ""          // 空闲无租约
	AgentLeaseRecording = "RECORDING" // 录制会话排他占用中
	AgentLeaseReplay    = "REPLAY"    // 重放任务排他占用中

	TrafficDirIngress = "INGRESS"
	TrafficDirEgress  = "EGRESS"

	ExchangeUnary     = "UNARY"      // 标准一问一答 (REQ + RSP)
	ExchangeStreamRsp = "STREAM_RSP" // 流式分片应答 (REQ + N*RSP_SEG + RSP)
	ExchangeOneway    = "ONEWAY"     // 单向通知请求 (ucReqType=3, 无应答)
	ExchangeReqOnly   = "REQ_ONLY"   // 超配对窗口或停录时未收到应答的挂起请求
	ExchangeRspOrphan = "RSP_ORPHAN" // 连接中途切入捕获到的孤立应答帧
	ExchangeMng       = "MNG"        // 连接管理握手/心跳报文 (ucMsgType=5..7)

	SessionStatusPending          = "PENDING"
	SessionStatusRecording        = "RECORDING"
	SessionStatusStopping         = "STOPPING"
	SessionStatusCompleted        = "COMPLETED"
	SessionStatusCompletedByLimit = "COMPLETED_BY_LIMIT"
	SessionStatusFailed           = "FAILED"

	ReplayResultPass     = "PASS"
	ReplayResultFailed   = "FAILED"
	ReplayResultMockMiss = "MOCK_MISS"
	ReplayResultTimeout  = "TIMEOUT"
	ReplayResultError    = "ERROR"
)

// U64Numeric 无符号 64 位整数封装，精准映射 PostgreSQL NUMERIC(20,0)，支持 math.MaxUint64 并保证 JSON 字符串序列化
type U64Numeric uint64

// Value 实现 driver.Valuer 接口
func (u U64Numeric) Value() (driver.Value, error) {
	return strconv.FormatUint(uint64(u), 10), nil
}

// Scan 实现 sql.Scanner 接口
func (u *U64Numeric) Scan(value interface{}) error {
	if value == nil {
		*u = 0
		return nil
	}
	switch v := value.(type) {
	case []byte:
		parsed, err := strconv.ParseUint(string(v), 10, 64)
		if err != nil {
			return fmt.Errorf("U64Numeric.Scan 解析 byte 切片失败: %w", err)
		}
		*u = U64Numeric(parsed)
		return nil
	case string:
		parsed, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return fmt.Errorf("U64Numeric.Scan 解析字符串失败: %w", err)
		}
		*u = U64Numeric(parsed)
		return nil
	case int64:
		*u = U64Numeric(uint64(v))
		return nil
	case uint64:
		*u = U64Numeric(v)
		return nil
	case int:
		*u = U64Numeric(uint64(v))
		return nil
	default:
		return fmt.Errorf("U64Numeric.Scan 遇到不支持的数据库类型 %T: %v", value, value)
	}
}

// MarshalJSON 将其序列化为 JSON 字符串防前端精度丢失
func (u U64Numeric) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(strconv.FormatUint(uint64(u), 10))), nil
}

// UnmarshalJSON 从 JSON 字符串或数值还原
func (u *U64Numeric) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), "\"")
	if s == "" || s == "null" {
		*u = 0
		return nil
	}
	parsed, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return err
	}
	*u = U64Numeric(parsed)
	return nil
}

// GormDataType 指定 GORM 字段迁移类型为 numeric(20,0)
func (U64Numeric) GormDataType() string {
	return "numeric(20,0)"
}

// Uint64 转换为原生 uint64
func (u U64Numeric) Uint64() uint64 {
	return uint64(u)
}

// String 转换为十进制字符串
func (u U64Numeric) String() string {
	return strconv.FormatUint(uint64(u), 10)
}
