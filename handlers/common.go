package handlers

import (
	"context"
	"strconv"

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

// ResolveDestination 根据服务名与操作名查询提供者模块 ID
func (r *DBTopologyResolver) ResolveDestination(serviceName, operation string) *uint64 {
	if r.DB == nil {
		return nil
	}
	var iface models.ModuleInterface
	if err := r.DB.Where("service_name = ? AND operation = ? AND relation_type = 'PROVIDE'", serviceName, operation).First(&iface).Error; err == nil {
		return &iface.ModuleID
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
