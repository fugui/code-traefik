package handlers

import (
	"strconv"

	commonAudit "code-common/backend/audit"
	commonAuth "code-common/backend/auth"
	commonModels "code-common/backend/models"
	"code-traefik/config"

	"github.com/gin-gonic/gin"
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
