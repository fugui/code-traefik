package handlers

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	commonAudit "code-common/backend/audit"
	commonAuth "code-common/backend/auth"
	commonModels "code-common/backend/models"
	"code-traefik/config"
	"code-traefik/internal/server/decoder"
	"code-traefik/internal/server/diff"
	"code-traefik/internal/server/recorder"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// FlexUint64 支持从 JSON 数字 (1) 或字符串 ("1") 灵活解析 uint64
type FlexUint64 uint64

func (f *FlexUint64) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), "\"")
	if s == "" || s == "null" || s == "undefined" {
		return nil
	}
	val, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return err
	}
	*f = FlexUint64(val)
	return nil
}

func (f *FlexUint64) Uint64() uint64 {
	if f == nil {
		return 0
	}
	return uint64(*f)
}

func (f *FlexUint64) Uint64Ptr() *uint64 {
	if f == nil {
		return nil
	}
	val := uint64(*f)
	if val == 0 {
		return nil
	}
	return &val
}

// FlexUint64Slice 支持从 JSON 数字数组 ([1, 2]) 或字符串数组 (["1", "2"]) 灵活解析 []uint64
type FlexUint64Slice []uint64

func (s *FlexUint64Slice) UnmarshalJSON(data []byte) error {
	var raw []any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	res := make([]uint64, 0, len(raw))
	for _, item := range raw {
		switch v := item.(type) {
		case float64:
			res = append(res, uint64(v))
		case string:
			trimmed := strings.TrimSpace(v)
			if trimmed != "" {
				u, err := strconv.ParseUint(trimmed, 10, 64)
				if err != nil {
					return err
				}
				res = append(res, u)
			}
		}
	}
	*s = FlexUint64Slice(res)
	return nil
}

// isCurrentUserAdmin 校验当前上下文是否具备模块管理员或超级管理员权限
func isCurrentUserAdmin(uc *commonAuth.UserContext) bool {
	if uc == nil {
		return false
	}
	for _, r := range uc.Roles {
		if r == commonAuth.RoleSuperAdmin || r == config.AdminRole {
			return true
		}
	}
	return false
}

// recordAudit 统一记录操作审计上下文
func recordAudit(
	c *gin.Context,
	action string,
	level commonModels.AuditLevel,
	summary string,
	targetType string,
	targetID uint64,
	targetName string,
	before any,
	after any,
) {
	commonAudit.SetAuditContext(
		c,
		config.ModuleKey,
		action,
		level,
		summary,
		targetType,
		strconv.FormatUint(targetID, 10),
		targetName,
		before,
		after,
	)
}

// DBTopologyResolver 模块目的端数据库定位适配器
type DBTopologyResolver struct {
	DB *gorm.DB
}

// ResolveDestination 根据服务名查询提供者模块 ID
func (r *DBTopologyResolver) ResolveDestination(serviceName, operation string) *uint64 {
	if r.DB == nil {
		return nil
	}
	var svc models.ModuleService
	if err := r.DB.Where("service_name = ?", serviceName).First(&svc).Error; err == nil {
		return &svc.ModuleID
	}
	return nil
}

// InitRecorderAndDiff 初始化协同录制与重放 Diff 引擎核心组件并注册全局单例
func InitRecorderAndDiff(db *gorm.DB, dec *decoder.Registry) (*recorder.ExchangeAssembler, *recorder.Orchestrator, *diff.ReplayScheduler) {
	sseBroker := InitGlobalSSEBroker(db)
	dstResolver := &DBTopologyResolver{DB: db}

	var orch *recorder.Orchestrator
	assembler := recorder.NewExchangeAssembler(
		db,
		sseBroker,
		dstResolver,
		func(sessionID uint64, reason string) {
			if orch != nil {
				_, _ = orch.StopSession(context.Background(), sessionID, 0, true, reason)
			}
		},
		nil,
	)

	orch = recorder.NewOrchestrator(db, assembler)
	InitGlobalOrchestrator(orch)

	sched := diff.NewReplayScheduler(db, dec)
	InitGlobalReplayScheduler(sched)

	return assembler, orch, sched
}
