package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"time"

	"code-traefik/internal/server/security"
)

const (
	defaultCodeNotifierTimeout = 5 * time.Second
	maxNotifierResponseBytes   = 64 * 1024
)

// NotifyRecipients code-notifier 收件人与抄送列表契约
type NotifyRecipients struct {
	To []string `json:"to"`
	Cc []string `json:"cc,omitempty"`
}

// NotifyPayload 跨机 LAN code-notifier (/api/notify/email) 完整请求载荷
type NotifyPayload struct {
	Subject         string           `json:"subject"`
	Content         string           `json:"content"`
	MarkdownContent string           `json:"markdown_content"`
	Format          string           `json:"format"`
	Recipients      NotifyRecipients `json:"recipients"`
	SourceApp       string           `json:"source_app"`
	EventType       string           `json:"event_type"`
	Severity        string           `json:"severity"`
	IdempotencyKey  string           `json:"idempotency_key"`
	LinkURL         string           `json:"link_url,omitempty"`
	Metadata        map[string]any   `json:"metadata,omitempty"`
}

// CodeNotifierConfig code-notifier 运行时外发配置
type CodeNotifierConfig struct {
	Enabled              bool
	BaseURL              string
	Token                string
	OutboundTrustedCIDRs string
}

// SendViaCodeNotifier 调用跨机或同机 code-notifier POST /api/notify/email 统一投递邮件。
// 强制保证 markdown_content 非空、source_app 固定为 "code-traefik"、经由 SafeDialer 拦截非法内网/云元数据请求。
func SendViaCodeNotifier(ctx context.Context, cfg CodeNotifierConfig, payload NotifyPayload) error {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		return errors.New("code_notifier_url 未配置")
	}

	subject := strings.TrimSpace(payload.Subject)
	if subject == "" {
		subject = "[CodeTraefik] 契约治理通知"
	}
	payload.Subject = subject

	// 核心红线：code-notifier 服务端强校验 markdown_content 非空，若为空自动以 Subject 兜底
	md := strings.TrimSpace(payload.MarkdownContent)
	if md == "" {
		md = subject
	}
	payload.MarkdownContent = md

	if strings.TrimSpace(payload.Content) == "" {
		payload.Content = renderSimpleHTMLFromMarkdown(subject, md, payload.LinkURL)
	}
	if strings.TrimSpace(payload.Format) == "" {
		payload.Format = "markdown"
	}
	payload.SourceApp = "code-traefik"
	if strings.TrimSpace(payload.IdempotencyKey) == "" {
		payload.IdempotencyKey = fmt.Sprintf("code-traefik:%s:%d", payload.EventType, time.Now().UnixNano())
	}

	if len(payload.Recipients.To) == 0 && len(payload.Recipients.Cc) == 0 {
		return errors.New("收件人邮箱列表为空")
	}

	dialer, err := security.NewSafeDialer(cfg.OutboundTrustedCIDRs)
	if err != nil {
		return fmt.Errorf("初始化 SafeDialer 失败: %w", err)
	}

	endpoint := baseURL
	if !strings.HasSuffix(endpoint, "/api/notify/email") {
		endpoint = endpoint + "/api/notify/email"
	}
	if err := dialer.ValidateURL(ctx, endpoint); err != nil {
		return fmt.Errorf("SSRF 防护拦截 code-notifier 请求: %w", err)
	}

	rawBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化 code-notifier 载荷失败: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, defaultCodeNotifierTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(rawBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(cfg.Token); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-API-Token", token)
	}

	client := dialer.HTTPClient(defaultCodeNotifierTimeout)
	defer client.CloseIdleConnections()

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("调用 code-notifier 失败: %w", err)
	}
	defer resp.Body.Close()

	respBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, maxNotifierResponseBytes))
	if readErr != nil {
		return fmt.Errorf("读取 code-notifier 响应失败: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("code-notifier 返回 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBytes)))
	}
	return nil
}

func renderSimpleHTMLFromMarkdown(title, md, linkURL string) string {
	escapedTitle := html.EscapeString(title)
	escapedBody := strings.ReplaceAll(html.EscapeString(md), "\n", "<br/>")
	var b strings.Builder
	b.WriteString("<div style=\"font-family:sans-serif;line-height:1.6;\">")
	b.WriteString("<h3>")
	b.WriteString(escapedTitle)
	b.WriteString("</h3><p>")
	b.WriteString(escapedBody)
	b.WriteString("</p>")
	if trimmedLink := strings.TrimSpace(linkURL); trimmedLink != "" {
		escapedLink := html.EscapeString(trimmedLink)
		b.WriteString("<p><a href=\"")
		b.WriteString(escapedLink)
		b.WriteString("\">查看详情</a></p>")
	}
	b.WriteString("</div>")
	return b.String()
}
