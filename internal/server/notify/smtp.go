package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"code-traefik/internal/server/security"
)

const defaultSMTPTimeout = 8 * time.Second

// SMTPConfig 备用 SMTP 邮件网关配置
type SMTPConfig struct {
	Host                 string
	Port                 int
	Username             string
	Password             string
	From                 string
	OutboundTrustedCIDRs string
}

// SendViaSMTP 使用经过 SafeDialer 保护的 TCP 连接执行备用 SMTP 邮件发送
func SendViaSMTP(ctx context.Context, cfg SMTPConfig, to []string, cc []string, subject, bodyMarkdown string) error {
	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		return errors.New("smtp_host 未配置")
	}
	port := cfg.Port
	if port <= 0 {
		port = 587
	}
	from := strings.TrimSpace(cfg.From)
	if from == "" {
		from = strings.TrimSpace(cfg.Username)
	}
	if from == "" {
		from = "noreply@code-traefik.local"
	}

	allRcpts := make([]string, 0, len(to)+len(cc))
	seen := make(map[string]struct{}, len(to)+len(cc))
	for _, addr := range append(append([]string{}, to...), cc...) {
		clean := strings.TrimSpace(addr)
		if clean == "" || !strings.Contains(clean, "@") {
			continue
		}
		if _, ok := seen[clean]; !ok {
			seen[clean] = struct{}{}
			allRcpts = append(allRcpts, clean)
		}
	}
	if len(allRcpts) == 0 {
		return errors.New("SMTP 收件人列表为空")
	}

	dialer, err := security.NewSafeDialer(cfg.OutboundTrustedCIDRs)
	if err != nil {
		return fmt.Errorf("初始化 SMTP SafeDialer 失败: %w", err)
	}

	dialCtx, cancel := context.WithTimeout(ctx, defaultSMTPTimeout)
	defer cancel()

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := dialer.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("连接 SMTP 服务器 %s 失败: %w", addr, err)
	}
	defer conn.Close()
	if dl, ok := dialCtx.Deadline(); ok {
		if setErr := conn.SetDeadline(dl); setErr != nil {
			return fmt.Errorf("设置 SMTP 连接超时失败: %w", setErr)
		}
	}

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("创建 SMTP 客户端失败: %w", err)
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			return
		}
	}()

	if ok, _ := client.Extension("STARTTLS"); ok {
		tlsCfg := &tls.Config{
			ServerName: host,
			MinVersion: tls.VersionTLS12,
		}
		if tlsErr := client.StartTLS(tlsCfg); tlsErr != nil {
			return fmt.Errorf("SMTP STARTTLS 握手失败: %w", tlsErr)
		}
	}

	if strings.TrimSpace(cfg.Username) != "" && cfg.Password != "" {
		if ok, _ := client.Extension("AUTH"); ok {
			auth := smtp.PlainAuth("", strings.TrimSpace(cfg.Username), cfg.Password, host)
			if authErr := client.Auth(auth); authErr != nil {
				return fmt.Errorf("SMTP 身份认证失败: %w", authErr)
			}
		}
	}

	if mailErr := client.Mail(from); mailErr != nil {
		return fmt.Errorf("SMTP MAIL FROM 失败: %w", mailErr)
	}
	for _, rcpt := range allRcpts {
		if rcptErr := client.Rcpt(rcpt); rcptErr != nil {
			return fmt.Errorf("SMTP RCPT TO (%s) 失败: %w", rcpt, rcptErr)
		}
	}

	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA 失败: %w", err)
	}

	cleanSubj := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(subject), "\r", ""), "\n", " ")
	cleanBody := strings.TrimSpace(bodyMarkdown)
	if cleanBody == "" {
		cleanBody = cleanSubj
	}
	var msg strings.Builder
	msg.WriteString("From: " + from + "\r\n")
	msg.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	if len(cc) > 0 {
		msg.WriteString("Cc: " + strings.Join(cc, ", ") + "\r\n")
	}
	msg.WriteString("Subject: " + cleanSubj + "\r\n")
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	msg.WriteString(cleanBody + "\r\n")

	if _, writeErr := wc.Write([]byte(msg.String())); writeErr != nil {
		if closeErr := wc.Close(); closeErr != nil {
			return fmt.Errorf("写入 SMTP 邮件正文失败: %w (关闭流错误: %v)", writeErr, closeErr)
		}
		return fmt.Errorf("写入 SMTP 邮件正文失败: %w", writeErr)
	}
	if closeErr := wc.Close(); closeErr != nil {
		return fmt.Errorf("提交 SMTP 邮件正文失败: %w", closeErr)
	}
	if quitErr := client.Quit(); quitErr != nil {
		// 部分 SMTP 网关在 DATA 成功后立即关闭连接，忽略非致命 Quit 尾包异常
		return nil
	}
	return nil
}
