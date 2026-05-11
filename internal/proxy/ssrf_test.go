package proxy

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestNewSSRFChecker(t *testing.T) {
	checker := NewSSRFChecker()
	if checker == nil {
		t.Fatal("NewSSRFChecker returned nil")
	}
}

func TestIsPrivateIP(t *testing.T) {
	checker := NewSSRFChecker()

	tests := []struct {
		name       string
		ip         string
		expectPriv bool
	}{
		{"loopback", "127.0.0.1", true},
		{"private 10.x", "10.0.0.1", true},
		{"private 172.16.x", "172.16.0.1", true},
		{"private 192.168.x", "192.168.1.1", true},
		{"link-local", "169.254.1.1", true},
		{"public", "8.8.8.8", false},
		{"public cloudflare", "1.1.1.1", false},
		{"ipv6 loopback", "::1", true},
		{"ipv4-mapped loopback", "::ffff:127.0.0.1", true},
		{"ipv4-mapped private 10", "::ffff:10.0.0.1", true},
		{"ipv4-mapped private 172.16", "::ffff:172.16.0.1", true},
		{"ipv4-mapped private 192.168", "::ffff:192.168.1.1", true},
		{"ipv4-mapped link-local", "::ffff:169.254.1.1", true},
		{"ipv4-mapped metadata", "::ffff:169.254.169.254", true},
		{"ipv4-mapped public", "::ffff:8.8.8.8", false},
		{"ipv6 unique local", "fc00::1", true},
		{"ipv6 link-local", "fe80::1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP: %s", tt.ip)
			}
			result := checker.IsPrivateIP(ip)
			if result != tt.expectPriv {
				t.Errorf("IsPrivateIP(%s) = %v, want %v", tt.ip, result, tt.expectPriv)
			}
		})
	}
}

func TestValidateURL(t *testing.T) {
	checker := NewSSRFChecker()

	tests := []struct {
		name      string
		url       string
		expectErr bool
	}{
		{"valid https", "https://api.openai.com/v1/models", false},
		{"valid http", "http://example.com/path", false},
		{"invalid scheme", "ftp://example.com/file", true},
		{"file scheme", "file:///etc/passwd", true},
		{"localhost", "http://localhost/admin", true},
		{"localhost 127.0.0.1", "http://127.0.0.1/admin", true},
		{"metadata service IP", "http://169.254.169.254/latest/meta-data", true},
		{"google metadata", "http://metadata.google.internal/", true},
		{"user info", "https://user:pass@example.com/", true},
		{"invalid URL", "://invalid", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checker.ValidateURL(tt.url)
			if (err != nil) != tt.expectErr {
				t.Errorf("ValidateURL(%q) error = %v, expectErr %v", tt.url, err, tt.expectErr)
			}
		})
	}
}

func TestSSRFCheckerAllowedSchemes(t *testing.T) {
	checker := NewSSRFChecker()

	if !checker.allowedSchemes["http"] {
		t.Error("http should be allowed by default")
	}
	if !checker.allowedSchemes["https"] {
		t.Error("https should be allowed by default")
	}
	if checker.allowedSchemes["ftp"] {
		t.Error("ftp should not be allowed by default")
	}
}

func TestSSRFCheckerBlockedRanges(t *testing.T) {
	checker := NewSSRFChecker()

	if len(checker.blockedRanges) == 0 {
		t.Error("blockedRanges should not be empty")
	}

	expectedRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
	}

	for _, expected := range expectedRanges {
		_, ipNet, err := net.ParseCIDR(expected)
		if err != nil {
			t.Fatalf("failed to parse CIDR: %v", err)
		}

		found := false
		for _, blocked := range checker.blockedRanges {
			if blocked.String() == ipNet.String() {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected blocked range %s not found", expected)
		}
	}
}

func TestNewSSRFCheckerWithConfigAllowLocalhost(t *testing.T) {
	// AllowLocalhost only bypasses the hostname string check; DNS resolution
	// still resolves localhost to 127.0.0.1 which is private. Both flags are
	// needed to fully allow localhost connections.
	checkerLocalhost := NewSSRFCheckerWithConfig(SSRFConfig{AllowLocalhost: true})
	checkerBoth := NewSSRFCheckerWithConfig(SSRFConfig{AllowLocalhost: true, AllowPrivateIP: true})

	tests := []struct {
		name      string
		checker   *SSRFChecker
		url       string
		expectErr bool
	}{
		{"localhost hostname-only still blocked by IP", checkerLocalhost, "http://localhost/admin", true},
		{"127.0.0.1 hostname-only still blocked by IP", checkerLocalhost, "http://127.0.0.1/admin", true},
		{"localhost both flags allowed", checkerBoth, "http://localhost/admin", false},
		{"127.0.0.1 both flags allowed", checkerBoth, "http://127.0.0.1/admin", false},
		{"::1 both flags allowed", checkerBoth, "http://[::1]/admin", false},
		{"private IP with only AllowLocalhost", checkerLocalhost, "http://10.0.0.1/admin", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.checker.ValidateURL(tt.url)
			if (err != nil) != tt.expectErr {
				t.Errorf("ValidateURL(%q) error = %v, expectErr %v", tt.url, err, tt.expectErr)
			}
		})
	}
}

func TestNewSSRFCheckerWithConfigAllowPrivateIP(t *testing.T) {
	checker := NewSSRFCheckerWithConfig(SSRFConfig{AllowPrivateIP: true})

	if len(checker.blockedRanges) != 0 {
		t.Errorf("expected no blocked ranges, got %d", len(checker.blockedRanges))
	}

	// With private IPs allowed, private IP ranges are not blocked at DNS level.
	// But localhost check, metadata check, user info check, etc. still apply.
	tests := []struct {
		name      string
		url       string
		expectErr bool
	}{
		{"localhost still blocked", "http://localhost/admin", true},
		{"metadata still blocked", "http://169.254.169.254/latest/meta-data", true},
		{"private IP allowed DNS-wise", "http://10.0.0.1/api", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checker.ValidateURL(tt.url)
			if (err != nil) != tt.expectErr {
				t.Errorf("ValidateURL(%q) error = %v, expectErr %v", tt.url, err, tt.expectErr)
			}
		})
	}
}

func TestValidateURLHexEncodedHost(t *testing.T) {
	checker := NewSSRFChecker()

	tests := []struct {
		name string
		url  string
	}{
		{"hex encoded IP", "http://0x7f000001/admin"},
		{"hex encoded loopback", "http://0x7f.0x00.0x00.0x01/admin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checker.ValidateURL(tt.url)
			if err == nil {
				t.Errorf("ValidateURL(%q) should reject hex-encoded host", tt.url)
			}
		})
	}
}

func TestValidateURLPathTraversal(t *testing.T) {
	checker := NewSSRFChecker()

	tests := []struct {
		name string
		url  string
	}{
		{"dot-dot slash", "https://example.com/../../../etc/passwd"},
		{"encoded dot-dot", "https://example.com/path/..%2F..%2Fetc/passwd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checker.ValidateURL(tt.url)
			if err == nil {
				t.Errorf("ValidateURL(%q) should block path traversal", tt.url)
			}
		})
	}
}

func TestValidateURLWSScheme(t *testing.T) {
	checker := NewSSRFChecker()

	err := checker.ValidateURL("wss://example.com/ws")
	if err != nil {
		t.Errorf("wss scheme should be allowed: %v", err)
	}

	err = checker.ValidateURL("ws://example.com/ws")
	if err != nil {
		t.Errorf("ws scheme should be allowed: %v", err)
	}
}

func TestValidateURLUserInfoVariants(t *testing.T) {
	checker := NewSSRFChecker()

	tests := []struct {
		name string
		url  string
	}{
		{"user only", "https://user@example.com/"},
		{"user and pass", "https://user:pass@example.com/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checker.ValidateURL(tt.url)
			if err == nil {
				t.Errorf("ValidateURL(%q) should reject user info in URL", tt.url)
			}
		})
	}
}

func TestIsPrivateIPWithAllowPrivateIPConfig(t *testing.T) {
	checker := NewSSRFCheckerWithConfig(SSRFConfig{AllowPrivateIP: true})

	// With no blocked ranges, nothing is private
	if checker.IsPrivateIP(net.ParseIP("10.0.0.1")) {
		t.Error("10.0.0.1 should not be private when AllowPrivateIP is true")
	}
	if checker.IsPrivateIP(net.ParseIP("127.0.0.1")) {
		t.Error("127.0.0.1 should not be private when AllowPrivateIP is true")
	}
}

func TestResolveAndValidateHostNoBlockedRanges(t *testing.T) {
	checker := NewSSRFCheckerWithConfig(SSRFConfig{AllowPrivateIP: true})

	ips, err := checker.ResolveAndValidateHost("example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With no blocked ranges, returns nil IPs
	if ips != nil {
		t.Errorf("expected nil IPs when no blocked ranges, got %v", ips)
	}
}

func TestResolveAndValidateHostPublicIP(t *testing.T) {
	checker := NewSSRFChecker()

	// example.com resolves to a public IP
	ips, err := checker.ResolveAndValidateHost("example.com")
	if err != nil {
		t.Fatalf("unexpected error for public host: %v", err)
	}
	if len(ips) == 0 {
		t.Error("expected at least one resolved IP for example.com")
	}
}

func TestResolveAndValidateHostInvalidHost(t *testing.T) {
	checker := NewSSRFChecker()

	_, err := checker.ResolveAndValidateHost("this.host.does.not.exist.invalid")
	if err == nil {
		t.Error("expected error for unresolvable host")
	}
}

func TestNewSafeTransportDefault(t *testing.T) {
	checker := NewSSRFChecker()

	transport := checker.NewSafeTransport(nil)
	if transport == nil {
		t.Fatal("NewSafeTransport(nil) returned nil")
	}
	if transport.DialContext == nil {
		t.Error("NewSafeTransport should set DialContext")
	}
}

func TestNewSafeTransportCustom(t *testing.T) {
	checker := NewSSRFChecker()

	base := &http.Transport{
		MaxIdleConns:    10,
		IdleConnTimeout: 30 * time.Second,
	}
	transport := checker.NewSafeTransport(base)
	if transport == nil {
		t.Fatal("NewSafeTransport(base) returned nil")
	}
	if transport.DialContext == nil {
		t.Error("NewSafeTransport should set DialContext")
	}
	if transport.MaxIdleConns != 10 {
		t.Errorf("expected MaxIdleConns=10, got %d", transport.MaxIdleConns)
	}
}

func TestNewSafeDialerDefault(t *testing.T) {
	checker := NewSSRFChecker()

	dialer := checker.NewSafeDialer(websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	})
	if dialer.NetDialContext == nil {
		t.Error("NewSafeDialer should set NetDialContext")
	}
	if dialer.TLSClientConfig == nil {
		t.Error("NewSafeDialer should set TLSClientConfig")
	} else if dialer.TLSClientConfig.MinVersion < tls.VersionTLS12 {
		t.Error("dialer should use TLS 1.2 minimum")
	}
}

func TestPinnedIPDialerBlocksPrivateIP(t *testing.T) {
	checker := NewSSRFChecker()
	baseDialer := &net.Dialer{Timeout: 1 * time.Second}

	dialCtx := pinnedIPDialer(checker, baseDialer)

	// Direct private IP address should be blocked
	_, err := dialCtx(context.Background(), "tcp", "10.0.0.1:443")
	if err == nil {
		t.Error("expected error when dialing private IP directly")
	}
}

func TestPinnedIPDialerBlocksLocalhostIP(t *testing.T) {
	checker := NewSSRFChecker()
	baseDialer := &net.Dialer{Timeout: 1 * time.Second}

	dialCtx := pinnedIPDialer(checker, baseDialer)

	_, err := dialCtx(context.Background(), "tcp", "127.0.0.1:443")
	if err == nil {
		t.Error("expected error when dialing localhost IP directly")
	}
}

func TestPinnedIPDialerRejectsPrivateResolvedIP(t *testing.T) {
	checker := NewSSRFChecker()
	baseDialer := &net.Dialer{Timeout: 1 * time.Second}

	dialCtx := pinnedIPDialer(checker, baseDialer)

	// localhost resolves to 127.0.0.1 which is private
	_, err := dialCtx(context.Background(), "tcp", "localhost:80")
	if err == nil {
		t.Error("expected error when dialing localhost (resolves to private IP)")
	}
}

func TestPinnedIPDialerAllowsPublicHost(t *testing.T) {
	checker := NewSSRFChecker()
	baseDialer := &net.Dialer{Timeout: 2 * time.Second}

	dialCtx := pinnedIPDialer(checker, baseDialer)

	// example.com resolves to public IPs; connection may fail because
	// there's no real HTTPS server on port 12345, but the SSRF check
	// itself should pass (we get a dial error, not an SSRF error).
	_, err := dialCtx(context.Background(), "tcp", "example.com:443")
	if err != nil {
		// Should be a dial error, not an SSRF error
		if isSSRFBlocked(err) {
			t.Errorf("public host should not be SSRF blocked: %v", err)
		}
	}
}

func TestPinnedIPDialerInvalidAddress(t *testing.T) {
	checker := NewSSRFChecker()
	baseDialer := &net.Dialer{Timeout: 1 * time.Second}

	dialCtx := pinnedIPDialer(checker, baseDialer)

	// Missing port
	_, err := dialCtx(context.Background(), "tcp", "example.com")
	if err == nil {
		t.Error("expected error for address without port")
	}
}

func TestPinnedIPDialerUnresolvableHost(t *testing.T) {
	checker := NewSSRFChecker()
	baseDialer := &net.Dialer{Timeout: 1 * time.Second}

	dialCtx := pinnedIPDialer(checker, baseDialer)

	_, err := dialCtx(context.Background(), "tcp", "this.host.invalid:443")
	if err == nil {
		t.Error("expected error for unresolvable host")
	}
}

func TestSafeTransportEndToEnd(t *testing.T) {
	// Create a local test server
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Parse the server URL
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("failed to parse test server URL: %v", err)
	}

	// Use AllowLocalhost so we can connect to the test server
	checker := NewSSRFCheckerWithConfig(SSRFConfig{AllowLocalhost: true, AllowPrivateIP: true})

	transport := checker.NewSafeTransport(ts.Client().Transport.(*http.Transport))

	client := &http.Client{Transport: transport}
	resp, err := client.Get(ts.URL)
	if err != nil {
		t.Fatalf("request to local test server failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	_ = u // suppress unused
}

func TestSafeTransportBlocksPrivateIP(t *testing.T) {
	checker := NewSSRFChecker()

	transport := checker.NewSafeTransport(nil)
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}

	// Connecting to a private IP should fail
	_, err := client.Get("http://10.0.0.1:8080/api")
	if err == nil {
		t.Error("expected error connecting to private IP via safe transport")
	}
}

func isSSRFBlocked(err error) bool {
	if err == nil {
		return false
	}
	return contains(err.Error(), "SSRF blocked") || contains(err.Error(), "private IP access not allowed")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
