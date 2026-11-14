package security

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	// 永久硬阻断网段：云元数据（AWS/GCP/Azure 169.254.0.0/16、阿里云 100.100.100.200/32）、链路本地与组播/未指定地址，即使出现在 allowlist 中也强制拦截
	alwaysBlockedCIDRs = mustParseCIDRs([]string{
		"169.254.0.0/16",
		"100.100.100.200/32",
		"fe80::/10",
		"0.0.0.0/8",
		"::/128",
		"224.0.0.0/4",
		"ff00::/8",
		"255.255.255.255/32",
	})

	// 默认阻断但可通过 outbound_trusted_cidrs 显式授信的私有/回环网段
	defaultPrivateCIDRs = mustParseCIDRs([]string{
		"127.0.0.0/8",
		"::1/128",
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"100.64.0.0/10",
		"fc00::/7",
	})
)

func mustParseCIDRs(cidrs []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, raw := range cidrs {
		_, ipNet, err := net.ParseCIDR(raw)
		if err != nil {
			panic(fmt.Sprintf("invalid builtin CIDR %q: %v", raw, err))
		}
		out = append(out, ipNet)
	}
	return out
}

// TrustedAllowlist 解析后的出向信任白名单（支持 CIDR、单 IP 与 host:port）
type TrustedAllowlist struct {
	nets      []*net.IPNet
	hostPorts map[string]struct{}
}

// ParseTrustedAllowlist 解析并校验 traefik_settings.outbound_trusted_cidrs 配置文本。
func ParseTrustedAllowlist(raw string) (*TrustedAllowlist, error) {
	allow := &TrustedAllowlist{
		nets:      make([]*net.IPNet, 0),
		hostPorts: make(map[string]struct{}),
	}
	tokens := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == ' ' || r == '\t'
	})
	for _, tok := range tokens {
		entry := strings.TrimSpace(tok)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "://") || strings.Contains(entry, "/") {
			if strings.Contains(entry, "://") {
				return nil, fmt.Errorf("白名单项 %q 格式非法，请勿携带协议头", entry)
			}
			ip, ipNet, err := net.ParseCIDR(entry)
			if err != nil || ip == nil {
				return nil, fmt.Errorf("白名单 CIDR %q 格式非法", entry)
			}
			allow.nets = append(allow.nets, ipNet)
			continue
		}
		if ip := normalizeIP(net.ParseIP(entry)); ip != nil {
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			allow.nets = append(allow.nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		host, portStr, err := net.SplitHostPort(entry)
		if err != nil || strings.TrimSpace(host) == "" {
			return nil, fmt.Errorf("白名单项 %q 不是合法的 CIDR、IP 或 host:port", entry)
		}
		portNum, portErr := strconv.Atoi(portStr)
		if portErr != nil || portNum < 1 || portNum > 65535 {
			return nil, fmt.Errorf("白名单端口 %q 超出有效范围 1..65535", portStr)
		}
		cleanHost := strings.ToLower(strings.TrimSpace(host))
		if strings.ContainsAny(cleanHost, "*?/") {
			return nil, fmt.Errorf("白名单主机名 %q 不支持通配符或路径", host)
		}
		if ip := normalizeIP(net.ParseIP(cleanHost)); ip != nil {
			cleanHost = ip.String()
		}
		allow.hostPorts[net.JoinHostPort(cleanHost, strconv.Itoa(portNum))] = struct{}{}
	}
	return allow, nil
}

// SafeDialer 基于 SSRF 防护策略与 outbound_trusted_cidrs 白名单的安全拨号器
type SafeDialer struct {
	allowlist *TrustedAllowlist
	timeout   time.Duration
}

// NewSafeDialer 构造受 SSRF 硬阻断与信任白名单约束的 SafeDialer。
func NewSafeDialer(trustedCIDRsRaw string) (*SafeDialer, error) {
	allow, err := ParseTrustedAllowlist(trustedCIDRsRaw)
	if err != nil {
		return nil, err
	}
	return &SafeDialer{
		allowlist: allow,
		timeout:   5 * time.Second,
	}, nil
}

func normalizeIP(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	return ip
}

func isAlwaysBlockedIP(ip net.IP) bool {
	ip = normalizeIP(ip)
	if ip == nil {
		return true
	}
	if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	for _, n := range alwaysBlockedCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func isPrivateOrLocalIP(ip net.IP) bool {
	ip = normalizeIP(ip)
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() {
		return true
	}
	for _, n := range defaultPrivateCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// CheckIP 校验单个 IP 是否允许出向建连（无 host:port 上下文，仅依据公共网段与 CIDR 白名单判定）。
func (d *SafeDialer) CheckIP(ip net.IP) error {
	return d.checkResolvedIP("", "", ip)
}

func (d *SafeDialer) checkResolvedIP(host, port string, rawIP net.IP) error {
	ip := normalizeIP(rawIP)
	if ip == nil {
		return errors.New("SSRF 防护拦截: 目标 IP 为空或格式非法")
	}
	if isAlwaysBlockedIP(ip) {
		return fmt.Errorf("SSRF 防护拦截: 永久阻断云元数据/链路本地/组播地址 %s", ip.String())
	}
	if !isPrivateOrLocalIP(ip) {
		return nil
	}
	if d != nil && d.allowlist != nil {
		for _, n := range d.allowlist.nets {
			if n.Contains(ip) {
				return nil
			}
		}
		if port != "" {
			if _, ok := d.allowlist.hostPorts[net.JoinHostPort(ip.String(), port)]; ok {
				return nil
			}
			cleanHost := strings.ToLower(strings.TrimSpace(host))
			if cleanHost != "" {
				if _, ok := d.allowlist.hostPorts[net.JoinHostPort(cleanHost, port)]; ok {
					return nil
				}
			}
		}
	}
	return fmt.Errorf("SSRF 防护拦截: 私有/回环目标 %s 未在 outbound_trusted_cidrs 白名单授权", ip.String())
}

// CheckAddress 解析并校验 host:port 目标是否满足 SSRF 策略（同时防御直接 IP 与域名 DNS 解析到内网/元数据）。
func (d *SafeDialer) CheckAddress(ctx context.Context, _ string, addr string) error {
	_, err := d.resolveAndVerify(ctx, addr)
	return err
}

func (d *SafeDialer) resolveAndVerify(ctx context.Context, addr string) ([]net.IP, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("SSRF 防护拦截: 目标地址 %q 格式非法: %w", addr, err)
	}
	cleanHost := strings.TrimSpace(strings.Trim(host, "[]"))
	if cleanHost == "" {
		return nil, errors.New("SSRF 防护拦截: 目标主机为空")
	}
	if ip := normalizeIP(net.ParseIP(cleanHost)); ip != nil {
		if err := d.checkResolvedIP(cleanHost, port, ip); err != nil {
			return nil, err
		}
		return []net.IP{ip}, nil
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, cleanHost)
	if err != nil {
		return nil, fmt.Errorf("DNS 解析目标 %q 失败: %w", cleanHost, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("SSRF 防护拦截: 目标 %q 无可用解析地址", cleanHost)
	}
	verified := make([]net.IP, 0, len(ips))
	for _, item := range ips {
		ip := normalizeIP(item.IP)
		if err := d.checkResolvedIP(cleanHost, port, ip); err != nil {
			return nil, err
		}
		verified = append(verified, ip)
	}
	return verified, nil
}

// DialContext 先解析并校验证实 IP 合法性，再 pinned IP 建立 TCP 连接并在 socket Control 钩子二次复验，防 DNS Rebinding。
func (d *SafeDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("SSRF 防护拦截: 目标地址 %q 缺少端口或格式非法: %w", addr, err)
	}
	verifiedIPs, err := d.resolveAndVerify(ctx, addr)
	if err != nil {
		return nil, err
	}
	allowedSet := make(map[string]struct{}, len(verifiedIPs))
	for _, ip := range verifiedIPs {
		allowedSet[ip.String()] = struct{}{}
	}

	dialer := &net.Dialer{
		Timeout:   d.timeout,
		KeepAlive: 30 * time.Second,
		Control: func(_ string, controlAddr string, _ syscall.RawConn) error {
			cHost, _, splitErr := net.SplitHostPort(controlAddr)
			if splitErr != nil {
				return fmt.Errorf("SSRF 控制拦截: 非法连接地址 %q", controlAddr)
			}
			ip := normalizeIP(net.ParseIP(strings.Trim(cHost, "[]")))
			if ip == nil || isAlwaysBlockedIP(ip) {
				return fmt.Errorf("SSRF 控制拦截: 拒绝连接受限地址 %q", controlAddr)
			}
			if _, ok := allowedSet[ip.String()]; !ok {
				return fmt.Errorf("SSRF 控制拦截: 连接地址 %s 与预解析校验不一致（疑似 DNS Rebinding）", ip.String())
			}
			return nil
		},
	}

	var lastErr error
	for _, ip := range verifiedIPs {
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	if lastErr == nil {
		lastErr = errors.New("无可连接的目标 IP")
	}
	return nil, lastErr
}

// ValidateURL 检查出向 HTTP(S) URL 协议与目标地址合法性。
func (d *SafeDialer) ValidateURL(ctx context.Context, rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("目标 URL 格式非法: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("SSRF 防护拦截: 仅允许 http/https 协议，拒绝 %q", parsed.Scheme)
	}
	host := parsed.Hostname()
	if host == "" {
		return errors.New("SSRF 防护拦截: URL 缺少主机名")
	}
	port := parsed.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return d.CheckAddress(ctx, "tcp", net.JoinHostPort(host, port))
}

// HTTPClient 构建受 SafeDialer 拨号控制与跨信任 Redirect 阻断保护的 HTTP 客户端。
func (d *SafeDialer) HTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	transport := &http.Transport{
		DialContext:           d.DialContext,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   3 * time.Second,
		ResponseHeaderTimeout: timeout,
		DisableKeepAlives:     true,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("SSRF 防护拦截: 重定向次数超过上限")
			}
			if len(via) > 0 && strings.EqualFold(via[len(via)-1].URL.Scheme, "https") && strings.EqualFold(req.URL.Scheme, "http") {
				return errors.New("SSRF 防护拦截: 禁止 HTTPS 降级重定向到明文 HTTP")
			}
			return d.ValidateURL(req.Context(), req.URL.String())
		},
	}
}
