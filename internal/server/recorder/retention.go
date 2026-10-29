package recorder

import (
	"context"
	"fmt"
	"log"
	"time"

	commonAudit "code-common/backend/audit"
	"code-traefik/config"
	"code-traefik/models"
	"gorm.io/gorm"
)

// RetentionCleaner 历史流量保留期自动清理器
type RetentionCleaner struct {
	db *gorm.DB
	pm *models.PartitionManager
}

// NewRetentionCleaner 构造清理器
func NewRetentionCleaner(db *gorm.DB) *RetentionCleaner {
	return &RetentionCleaner{
		db: db,
		pm: &models.PartitionManager{},
	}
}

// CleanExpired 执行 30 天历史流量物理分区清理与会话清理
func (c *RetentionCleaner) CleanExpired(ctx context.Context, retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		retentionDays = 30
	}
	now := time.Now()

	// 1. DROP 过期按日物理分区表 (轻量瞬间释放磁盘)
	droppedPartitions, err := c.pm.PurgeExpiredPartitions(c.db, retentionDays, now)
	if err != nil {
		log.Printf("[RetentionCleaner] 警告: DROP 分区表部分失败: %v", err)
	}

	// 2. 分批清理过期终态会话及其关联记录
	cutoff := now.AddDate(0, 0, -retentionDays)
	var deletedSessionsCount int64

	var sessionIDs []uint64
	if err := c.db.WithContext(ctx).Model(&models.RecordingSession{}).
		Where("created_at < ? AND status IN (?, ?, ?)", cutoff,
			models.SessionStatusCompleted, models.SessionStatusCompletedByLimit, models.SessionStatusFailed).
		Limit(5000).
		Pluck("id", &sessionIDs).Error; err != nil {
		return 0, fmt.Errorf("查询过期会话失败: %w", err)
	}

	if len(sessionIDs) > 0 {
		// 删除关联未分区的辅助数据 (如有)
		_ = c.db.WithContext(ctx).Where("session_id IN ?", sessionIDs).Delete(&models.TrafficRecord{}).Error
		res := c.db.WithContext(ctx).Where("id IN ?", sessionIDs).Delete(&models.RecordingSession{})
		deletedSessionsCount = res.RowsAffected
	}

	// 3. 记录系统操作审计日志
	commonAudit.Push(&models.SysAuditLog{
		CreatedAt:  time.Now(),
		Service:    config.ServiceName,
		Module:     config.ModuleKey,
		Action:     "purge_retention",
		Level:      "P2",
		Summary:    fmt.Sprintf("清理过期保留流量完成: droppedPartitions=%d, deletedSessions=%d, retentionDays=%d", len(droppedPartitions), deletedSessionsCount, retentionDays),
		TargetType: "retention_cleaner",
		TargetID:   "system",
		Username:   "system_cron",
		StatusCode: 200,
	})

	return deletedSessionsCount, nil
}
