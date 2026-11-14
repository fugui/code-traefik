package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	commonAuth "code-common/backend/auth"
	"code-traefik/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ListNotifications 查询当前登录用户的站内通知列表 (GET /api/notifications)
func ListNotifications(c *gin.Context) {
	db := models.DB
	uc := commonAuth.GetUserContext(c)
	if uc == nil || uc.UserID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录或会话已失效"})
		return
	}

	page, pageSize := parseMRPageParams(c)
	q := db.Model(&models.Notification{}).Where("recipient_user_id = ?", uc.UserID)

	if isReadStr := strings.ToLower(strings.TrimSpace(c.Query("is_read"))); isReadStr != "" {
		switch isReadStr {
		case "true", "1":
			q = q.Where("is_read = true")
		case "false", "0":
			q = q.Where("is_read = false")
		}
	}
	if cat := strings.ToUpper(strings.TrimSpace(c.Query("category"))); cat != "" {
		q = q.Where("category = ?", cat)
	}
	if sev := strings.ToUpper(strings.TrimSpace(c.Query("severity"))); sev != "" {
		q = q.Where("severity = ?", sev)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询站内通知总数失败"})
		return
	}

	var unreadCount int64
	if err := db.Model(&models.Notification{}).
		Where("recipient_user_id = ? AND is_read = false", uc.UserID).
		Count(&unreadCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询未读通知数量失败"})
		return
	}

	var items []models.Notification
	offset := (page - 1) * pageSize
	if err := q.Order("created_at DESC, id DESC").Offset(offset).Limit(pageSize).Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询站内通知列表失败"})
		return
	}
	if items == nil {
		items = []models.Notification{}
	}

	c.JSON(http.StatusOK, gin.H{
		"items":        items,
		"total":        total,
		"unread_count": unreadCount,
		"page":         page,
		"page_size":    pageSize,
	})
}

// GetUnreadNotificationCount 查询当前登录用户的未读通知计数 (GET /api/notifications/unread-count)
func GetUnreadNotificationCount(c *gin.Context) {
	db := models.DB
	uc := commonAuth.GetUserContext(c)
	if uc == nil || uc.UserID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录或会话已失效"})
		return
	}

	var unreadCount int64
	if err := db.Model(&models.Notification{}).
		Where("recipient_user_id = ? AND is_read = false", uc.UserID).
		Count(&unreadCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询未读通知数量失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"unread_count": unreadCount,
	})
}

// MarkNotificationRead 标记单条本人通知为已读 (POST /api/notifications/:id/read)
// 严格执行 Anti-IDOR 归属校验：禁止访问或修改他人通知记录
func MarkNotificationRead(c *gin.Context) {
	db := models.DB
	uc := commonAuth.GetUserContext(c)
	if uc == nil || uc.UserID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录或会话已失效"})
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的通知 ID"})
		return
	}

	var notif models.Notification
	if err := db.First(&notif, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "通知记录不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取通知记录失败"})
		return
	}

	if notif.RecipientUserID != uc.UserID {
		c.JSON(http.StatusForbidden, gin.H{
			"code":  "ERR_NOTIFICATION_FORBIDDEN",
			"error": "无权操作其他用户的站内通知",
		})
		return
	}

	if !notif.IsRead {
		now := time.Now().UTC()
		if err := db.Model(&notif).Updates(map[string]any{
			"is_read": true,
			"read_at": &now,
		}).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "更新通知已读状态失败"})
			return
		}
		notif.IsRead = true
		notif.ReadAt = &now
	}

	c.JSON(http.StatusOK, notif)
}

// MarkAllNotificationsRead 将当前用户的全部未读通知一键标记为已读 (POST /api/notifications/read-all)
func MarkAllNotificationsRead(c *gin.Context) {
	db := models.DB
	uc := commonAuth.GetUserContext(c)
	if uc == nil || uc.UserID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录或会话已失效"})
		return
	}

	now := time.Now().UTC()
	res := db.Model(&models.Notification{}).
		Where("recipient_user_id = ? AND is_read = false", uc.UserID).
		Updates(map[string]any{
			"is_read": true,
			"read_at": &now,
		})
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "批量更新已读状态失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":       "已全部标记为已读",
		"updated_count": res.RowsAffected,
	})
}
