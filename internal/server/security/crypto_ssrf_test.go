package security_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"code-traefik/internal/server/security"
)

func TestTRAEFIKCryptoKey_SafeDialer_TrustedCIDRsAndAuditRedaction(t *testing.T) {
	// 1. 测试未配置 TRAEFIK_CRYPTO_KEY 时拒绝明文回退与降级
	t.Setenv("TRAEFIK_CRYPTO_KEY", "")
	if _, err := security.EncryptSecret("sk-secret-123456"); err == nil {
		t.Fatalf("未设置 TRAEFIK_CRYPTO_KEY 时应当报错拒绝加密，禁止硬编码默认密钥")
	}
	if _, err := security.DecryptSecret("v1:invalid"); err == nil {
		t.Fatalf("未设置 TRAEFIK_CRYPTO_KEY 时应当报错拒绝解密")
	}

	// 2. 配置密钥后验证 AES-256-GCM 加解密闭环及随机 Nonce
	t.Setenv("TRAEFIK_CRYPTO_KEY", "test-traefik-master-crypto-key-32bytes!")
	plain := "sk-live-secret-token-for-llm-and-smtp"
	cipher1, err := security.EncryptSecret(plain)
	if err != nil {
		t.Fatalf("EncryptSecret 失败: %v", err)
	}
	cipher2, err := security.EncryptSecret(plain)
	if err != nil {
		t.Fatalf("第二次 EncryptSecret 失败: %v", err)
	}
	if cipher1 == plain || strings.Contains(cipher1, plain) {
		t.Fatalf("密文不得包含明文: %s", cipher1)
	}
	if cipher1 == cipher2 {
		t.Fatalf("AES-GCM 每次加密应使用随机 Nonce 产生不同密文")
	}
	got, err := security.DecryptSecret(cipher1)
	if err != nil {
		t.Fatalf("DecryptSecret 失败: %v", err)
	}
	if got != plain {
		t.Fatalf("解密结果不一致: got=%q want=%q", got, plain)
	}

	// 3. 验证常量时间比较
	if !security.ConstantTimeCompare("token-abc-123", "token-abc-123") {
		t.Fatalf("ConstantTimeCompare 相等字符串应返回 true")
	}
	if security.ConstantTimeCompare("token-abc-123", "token-abc-999") {
		t.Fatalf("ConstantTimeCompare 不等字符串应返回 false")
	}

	// 4. 启动本地 httptest 服务（绑定 127.0.0.1），验证默认 SSRF 阻断内网/回环与元数据
	localSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer localSrv.Close()

	// 构建一个未放行内网的 SafeDialer：即使尝试把云元数据写进白名单，也必须永久硬阻断
	maliciousAllowlist := "169.254.0.0/16,100.100.100.200/32,fe80::/10"
	defaultDialer, err := security.NewSafeDialer(maliciousAllowlist)
	if err != nil {
		t.Fatalf("NewSafeDialer 不应因为过滤元数据而崩溃，期望构造后依然硬阻断元数据: %v", err)
	}

	alwaysBlockedIPs := []string{
		"169.254.169.254",
		"100.100.100.200",
		"0.0.0.0",
		"224.0.0.1",
		"fe80::1",
	}
	for _, rawIP := range alwaysBlockedIPs {
		ip := net.ParseIP(rawIP)
		if err := defaultDialer.CheckIP(ip); err == nil {
			t.Fatalf("即使尝试加入 outbound_trusted_cidrs，危险地址 %s 也必须被永久硬阻断", rawIP)
		}
	}

	// 默认情况下未在信任白名单的内网 RFC1918 与回环也应当被拒绝
	for _, privateIP := range []string{"127.0.0.1", "10.10.1.5", "172.16.8.9", "192.168.56.18", "::1", "fd00::1"} {
		if err := defaultDialer.CheckIP(net.ParseIP(privateIP)); err == nil {
			t.Fatalf("未加入白名单时默认应当拦截私有/回环地址 %s", privateIP)
		}
	}
	// 通过 HTTPClient 请求本地 127.0.0.1 服务应当被阻断
	if _, err := defaultDialer.HTTPClient(2 * time.Second).Get(localSrv.URL); err == nil {
		t.Fatalf("默认未放行 127.0.0.1 时 SafeDialer.HTTPClient 请求本地服务必须被阻断")
	}

	// 5. 配置 outbound_trusted_cidrs 信任特定网段与精确 host:port 后可放行，但元数据依然硬阻断
	srvHostPort := strings.TrimPrefix(localSrv.URL, "http://")
	trustedDialer, err := security.NewSafeDialer("192.168.56.0/24, 10.20.0.0/16, " + srvHostPort)
	if err != nil {
		t.Fatalf("构造受信任 SafeDialer 失败: %v", err)
	}
	if err := trustedDialer.CheckIP(net.ParseIP("192.168.56.18")); err != nil {
		t.Fatalf("信任网段 192.168.56.0/24 内的地址 192.168.56.18 应当放行: %v", err)
	}
	if err := trustedDialer.CheckIP(net.ParseIP("172.16.1.1")); err == nil {
		t.Fatalf("未在白名单内的内网地址 172.16.1.1 应当继续被阻断")
	}
	if err := trustedDialer.CheckIP(net.ParseIP("169.254.169.254")); err == nil {
		t.Fatalf("云元数据 169.254.169.254 必须始终硬阻断")
	}
	if err := trustedDialer.CheckAddress(context.Background(), "tcp", srvHostPort); err != nil {
		t.Fatalf("精确信任 %s 后 CheckAddress 应当放行: %v", srvHostPort, err)
	}
	resp, err := trustedDialer.HTTPClient(2 * time.Second).Get(localSrv.URL)
	if err != nil {
		t.Fatalf("信任精确地址后请求本地服务应成功: %v", err)
	}
	_ = resp.Body.Close()

	// 6. 重定向目标为未授权地址时必须阻断（防 HTTP Redirect SSRF 绕过）
	redirectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer redirectSrv.Close()
	redirectHostPort := strings.TrimPrefix(redirectSrv.URL, "http://")
	redirectDialer, err := security.NewSafeDialer(redirectHostPort)
	if err != nil {
		t.Fatalf("构造 redirectDialer 失败: %v", err)
	}
	if _, err := redirectDialer.HTTPClient(2 * time.Second).Get(redirectSrv.URL); err == nil {
		t.Fatalf("跳转到云元数据的 302 Redirect 必须被阻断")
	}

	// 7. 验证非法 CIDR 配置校验失败
	if _, err := security.ParseTrustedAllowlist("not-a-valid/cidr/range"); err == nil {
		t.Fatalf("非法 outbound_trusted_cidrs 格式应当返回错误")
	}
}
