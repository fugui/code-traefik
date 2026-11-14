package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"code-traefik/internal/server/hub"
	"code-traefik/internal/server/security"
	"code-traefik/models"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// DispatchInput 站内持久通知 + 定向 SSE + 跨机邮件外发入参
type DispatchInput struct {
	RecipientUserIDs []uint
	ToEmails         []string
	CcEmails         []string
	Category         string
	Severity         string
	Title            string
	BodyMarkdown     string
	LinkURL          string
	SourceType       string
	SourceID         uint64
	Metadata         map[string]any
	IdempotencyKey   string
}

// Dispatcher 统一通知调度器（站内收件箱 + SSE 定向推送 + code-notifier/SMTP 外发）
type Dispatcher struct {
	db          *gorm.DB
	broadcaster hub.EventBroadcaster
}

// NewDispatcher 创建统一通知调度器
func NewDispatcher(db *gorm.DB, broadcaster hub.EventBroadcaster) *Dispatcher {
	if broadcaster == nil {
		broadcaster = hub.GetGlobalSSEBroker()
	}
	return &Dispatcher{
		db:          db,
		broadcaster: broadcaster,
	}
}

// DispatchAsync 异步非阻塞分发站内通知与外发邮件
func (d *Dispatcher) DispatchAsync(in DispatchInput) {
	if d == nil || d.db == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := d.DispatchSync(ctx, in); err != nil {
			log.Printf("[notify] 异步通知分发异常: %v", err)
		}
	}()
}

// DispatchSync 同步执行站内持久落库、按 userID 定向 SSE 推送以及外部 code-notifier / SMTP 邮件投递（外发失败自动降级落库，不向上返回错误）
func (d *Dispatcher) DispatchSync(ctx context.Context, in DispatchInput) ([]models.Notification, error) {
	if d == nil || d.db == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = "契约治理中心通知"
	}
	bodyMD := strings.TrimSpace(in.BodyMarkdown)
	if bodyMD == "" {
		bodyMD = title
	}
	severity := strings.ToUpper(strings.TrimSpace(in.Severity))
	switch severity {
	case models.NotifSeverityInfo, models.NotifSeverityWarning, models.NotifSeverityCritical:
	default:
		severity = models.NotifSeverityInfo
	}
	category := strings.TrimSpace(in.Category)
	if category == "" {
		category = models.NotifCategoryMRGateBlocked
	}

	// 1. 去重收件人 userID 并从 users 表解析真实邮箱
	seenUID := make(map[uint]struct{}, len(in.RecipientUserIDs))
	uniqueUIDs := make([]uint, 0, len(in.RecipientUserIDs))
	for _, uid := range in.RecipientUserIDs {
		if uid == 0 {
			continue
		}
		if _, ok := seenUID[uid]; !ok {
			seenUID[uid] = struct{}{}
			uniqueUIDs = append(uniqueUIDs, uid)
		}
	}

	userEmailMap := make(map[uint]string, len(uniqueUIDs))
	if len(uniqueUIDs) > 0 {
		var users []models.User
		if err := d.db.WithContext(ctx).Select("id", "email").Where("id IN ?", uniqueUIDs).Find(&users).Error; err == nil {
			for _, u := range users {
				cleanMail := strings.TrimSpace(u.Email)
				if isValidEmail(cleanMail) {
					userEmailMap[u.ID] = cleanMail
				}
			}
		}
	}

	metaBytes := []byte(`{}`)
	if len(in.Metadata) > 0 {
		if b, mErr := json.Marshal(in.Metadata); mErr == nil {
			metaBytes = b
		}
	}

	// 2. 为每位目标用户写入 traefik_notifications 站内收件箱并定向推送 SSE
	created := make([]models.Notification, 0, len(uniqueUIDs))
	for _, uid := range uniqueUIDs {
		notif := models.Notification{
			RecipientUserID: uid,
			RecipientEmail:  userEmailMap[uid],
			Category:        category,
			Severity:        severity,
			Title:           title,
			BodyMarkdown:    bodyMD,
			LinkURL:         strings.TrimSpace(in.LinkURL),
			SourceType:      strings.TrimSpace(in.SourceType),
			SourceID:        in.SourceID,
			MetadataJSON:    datatypes.JSON(metaBytes),
			IsRead:          false,
			ExternalChannel: models.ExternalChannelNone,
			EmailStatus:     models.EmailStatusSkipped,
		}
		if err := d.db.WithContext(ctx).Create(&notif).Error; err != nil {
			return created, fmt.Errorf("写入站内通知失败: %w", err)
		}
		created = append(created, notif)

		if d.broadcaster != nil {
			d.broadcaster.BroadcastToUsers([]uint{uid}, "notification.created", notif)
		}
	}

	// 3. 汇总有效 To / Cc 邮箱列表（自动过滤空邮箱与非法格式）
	toSet := make(map[string]struct{})
	toList := make([]string, 0)
	addTo := func(raw string) {
		clean := strings.TrimSpace(raw)
		if !isValidEmail(clean) {
			return
		}
		lower := strings.ToLower(clean)
		if _, exists := toSet[lower]; !exists {
			toSet[lower] = struct{}{}
			toList = append(toList, clean)
		}
	}
	for _, uid := range uniqueUIDs {
		if email := userEmailMap[uid]; email != "" {
			addTo(email)
		}
	}
	for _, explicitTo := range in.ToEmails {
		addTo(explicitTo)
	}

	ccSet := make(map[string]struct{})
	ccList := make([]string, 0)
	for _, rawCc := range in.CcEmails {
		clean := strings.TrimSpace(rawCc)
		if !isValidEmail(clean) {
			continue
		}
		lower := strings.ToLower(clean)
		if _, inTo := toSet[lower]; inTo {
			continue
		}
		if _, exists := ccSet[lower]; !exists {
			ccSet[lower] = struct{}{}
			ccList = append(ccList, clean)
		}
	}

	if len(toList) == 0 && len(ccList) == 0 {
		return created, nil
	}

	// 4. 读取通知通道运行参数并执行外发（优先 code-notifier，其次 SMTP）
	notifierCfg, smtpCfg := loadNotifyChannelSettings(d.db)
	var chosenChannel string
	var sendErr error

	if notifierCfg.Enabled && strings.TrimSpace(notifierCfg.BaseURL) != "" {
		chosenChannel = models.ExternalChannelCodeNotifier
		idemKey := strings.TrimSpace(in.IdempotencyKey)
		if idemKey == "" {
			var firstNotifID uint64
			if len(created) > 0 {
				firstNotifID = created[0].ID
			}
			idemKey = fmt.Sprintf("code-traefik:%s:%d:%s:%d", in.SourceType, in.SourceID, category, firstNotifID)
		}
		sendErr = SendViaCodeNotifier(ctx, notifierCfg, NotifyPayload{
			Subject:         title,
			MarkdownContent: bodyMD,
			Format:          "markdown",
			Recipients: NotifyRecipients{
				To: toList,
				Cc: ccList,
			},
			SourceApp:      "code-traefik",
			EventType:      category,
			Severity:       severity,
			IdempotencyKey: idemKey,
			LinkURL:        strings.TrimSpace(in.LinkURL),
			Metadata:       in.Metadata,
		})
	} else if strings.TrimSpace(smtpCfg.Host) != "" {
		chosenChannel = models.ExternalChannelSMTP
		sendErr = SendViaSMTP(ctx, smtpCfg, toList, ccList, title, bodyMD)
	} else {
		return created, nil
	}

	// 5. 回写外发状态至具备有效邮箱的 Notification 行（不因外发失败中断主流程）
	now := time.Now().UTC()
	for i := range created {
		if created[i].RecipientEmail == "" && len(in.ToEmails) == 0 {
			continue
		}
		created[i].ExternalChannel = chosenChannel
		if sendErr != nil {
			created[i].EmailStatus = models.EmailStatusFailed
			created[i].EmailError = truncateError(sendErr.Error(), 512)
		} else {
			created[i].EmailStatus = models.EmailStatusSent
			created[i].EmailError = ""
			created[i].EmailSentAt = &now
		}
		if updErr := d.db.WithContext(ctx).Model(&created[i]).Updates(map[string]any{
			"external_channel": created[i].ExternalChannel,
			"email_status":     created[i].EmailStatus,
			"email_error":      created[i].EmailError,
			"email_sent_at":    created[i].EmailSentAt,
		}).Error; updErr != nil {
			log.Printf("[notify] 更新通知 #%d 邮件状态失败: %v", created[i].ID, updErr)
		}
	}

	return created, nil
}

// ResolveRecipientUserIDsForModules 收集受影响模块负责人、MR 作者对应系统用户及管理员 UserID 集合
func ResolveRecipientUserIDsForModules(db *gorm.DB, moduleIDs []uint64, authorUsername, authorEmail string, includeAdmins bool) []uint {
	if db == nil {
		return nil
	}
	seen := make(map[uint]struct{})
	out := make([]uint, 0)
	addUID := func(uid uint) {
		if uid == 0 {
			return
		}
		if _, ok := seen[uid]; !ok {
			seen[uid] = struct{}{}
			out = append(out, uid)
		}
	}

	if len(moduleIDs) > 0 {
		var mods []models.Module
		if err := db.Select("id", "owner_id").Where("id IN ?", moduleIDs).Find(&mods).Error; err == nil {
			for _, m := range mods {
				if m.OwnerID != nil && *m.OwnerID > 0 {
					addUID(*m.OwnerID)
				}
			}
		}
	}

	cleanUser := strings.ToLower(strings.TrimSpace(authorUsername))
	cleanMail := strings.ToLower(strings.TrimSpace(authorEmail))
	if cleanUser != "" || cleanMail != "" {
		var author models.User
		q := db.Model(&models.User{})
		switch {
		case cleanUser != "" && cleanMail != "":
			q = q.Where("LOWER(username) = ? OR LOWER(email) = ?", cleanUser, cleanMail)
		case cleanUser != "":
			q = q.Where("LOWER(username) = ?", cleanUser)
		default:
			q = q.Where("LOWER(email) = ?", cleanMail)
		}
		if err := q.Limit(1).Find(&author).Error; err == nil && author.ID > 0 {
			addUID(author.ID)
		}
	}

	if includeAdmins || len(out) == 0 {
		var users []models.User
		if err := db.Where("is_active = true").Order("id ASC").Limit(20).Find(&users).Error; err == nil {
			for _, u := range users {
				rolesRaw := string(u.Roles)
				if u.IsSuperAdmin() || strings.Contains(rolesRaw, "traefik_admin") || strings.Contains(rolesRaw, "admin") {
					addUID(u.ID)
				}
			}
		}
	}
	return out
}

func loadNotifyChannelSettings(db *gorm.DB) (CodeNotifierConfig, SMTPConfig) {
	var rows []models.TraefikSetting
	if err := db.Where("setting_key IN ?", []string{
		"code_notifier_enabled",
		"code_notifier_url",
		"code_notifier_token_enc",
		"outbound_trusted_cidrs",
		"smtp_host",
		"smtp_port",
		"smtp_username",
		"smtp_from",
		"smtp_password_enc",
	}).Find(&rows).Error; err != nil {
		return CodeNotifierConfig{}, SMTPConfig{}
	}
	kv := make(map[string]string, len(rows))
	for _, r := range rows {
		kv[r.SettingKey] = strings.TrimSpace(r.SettingVal)
	}

	var notifierToken string
	if enc := kv["code_notifier_token_enc"]; enc != "" {
		if dec, err := security.DecryptSecret(enc); err == nil {
			notifierToken = dec
		}
	}
	var smtpPass string
	if enc := kv["smtp_password_enc"]; enc != "" {
		if dec, err := security.DecryptSecret(enc); err == nil {
			smtpPass = dec
		}
	}
	smtpPort := 587
	if parsed, err := strconv.Atoi(kv["smtp_port"]); err == nil && parsed > 0 {
		smtpPort = parsed
	}

	notifierCfg := CodeNotifierConfig{
		Enabled:              strings.EqualFold(kv["code_notifier_enabled"], "true") || kv["code_notifier_enabled"] == "1",
		BaseURL:              kv["code_notifier_url"],
		Token:                notifierToken,
		OutboundTrustedCIDRs: kv["outbound_trusted_cidrs"],
	}
	smtpCfg := SMTPConfig{
		Host:                 kv["smtp_host"],
		Port:                 smtpPort,
		Username:             kv["smtp_username"],
		Password:             smtpPass,
		From:                 kv["smtp_from"],
		OutboundTrustedCIDRs: kv["outbound_trusted_cidrs"],
	}
	return notifierCfg, smtpCfg
}

func isValidEmail(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) < 3 || len(trimmed) > 254 {
		return false
	}
	at := strings.IndexByte(trimmed, '@')
	if at <= 0 || at >= len(trimmed)-1 {
		return false
	}
	return !strings.ContainsAny(trimmed, " \t\r\n<>")
}

func truncateError(msg string, max int) string {
	trimmed := strings.TrimSpace(msg)
	if len(trimmed) <= max {
		return trimmed
	}
	return trimmed[:max]
}
