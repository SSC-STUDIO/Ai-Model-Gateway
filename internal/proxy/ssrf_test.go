package proxy

import (
	"net"
	"testing"
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

func TestValidateURLPathTraversal(t *testing.T) {
	checker := NewSSRFChecker()

	// Path traversal should be blocked
	err := checker.ValidateURL("https://example.com/../../../etc/passwd")
	if err == nil {
		t.Error("Path traversal should be blocked")
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
