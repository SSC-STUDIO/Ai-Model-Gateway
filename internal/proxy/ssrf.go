package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// SSRFConfig provides configuration for SSRF protection checker
type SSRFConfig struct {
	// AllowLocalhost enables localhost connections (useful for testing)
	AllowLocalhost bool
	// AllowPrivateIP enables private IP connections (useful for testing)
	AllowPrivateIP bool
}

// SSRFChecker provides protection against Server-Side Request Forgery attacks
type SSRFChecker struct {
	// BlockedIPRanges contains private IP ranges that should not be accessible
	blockedRanges []*net.IPNet
	// AllowedSchemes restricts which URL schemes are permitted
	allowedSchemes map[string]bool
	// config holds the checker configuration
	config SSRFConfig
}

// NewSSRFChecker creates a new SSRF protection checker with default settings
func NewSSRFChecker() *SSRFChecker {
	return NewSSRFCheckerWithConfig(SSRFConfig{})
}

// NewSSRFCheckerWithConfig creates a new SSRF protection checker with custom configuration
func NewSSRFCheckerWithConfig(config SSRFConfig) *SSRFChecker {
	checker := &SSRFChecker{
		blockedRanges:  make([]*net.IPNet, 0),
		allowedSchemes: map[string]bool{"https": true, "http": true, "wss": true, "ws": true},
		config:         config,
	}

	// Block private IP ranges (unless explicitly allowed)
	if !config.AllowPrivateIP {
		ranges := []string{
			"10.0.0.0/8",      // Private network (RFC 1918)
			"172.16.0.0/12",   // Private network (RFC 1918)
			"192.168.0.0/16",  // Private network (RFC 1918)
			"127.0.0.0/8",     // Loopback
			"169.254.0.0/16",  // Link-local (includes cloud metadata 169.254.169.254)
			"0.0.0.0/8",       // Current network (RFC 1122)
			"100.64.0.0/10",   // CGNAT (RFC 6598)
			"192.0.0.0/24",    // IETF Protocol Assignments (RFC 6890)
			"192.0.2.0/24",    // TEST-NET-1 (RFC 5737)
			"198.51.100.0/24", // TEST-NET-2 (RFC 5737)
			"203.0.113.0/24",  // TEST-NET-3 (RFC 5737)
			"224.0.0.0/4",     // Multicast (RFC 5771)
			"240.0.0.0/4",     // Reserved (RFC 1112)
			"255.255.255.255/32", // Broadcast
			"::1/128",         // IPv6 loopback
			"fc00::/7",        // IPv6 unique local
			"fe80::/10",       // IPv6 link-local
			"ff00::/8",        // IPv6 multicast
			"100::/64",        // IPv6 discard prefix (RFC 6666)
			"2001:db8::/32",   // IPv6 documentation (RFC 3849)
		}

		for _, r := range ranges {
			_, ipNet, err := net.ParseCIDR(r)
			if err == nil {
				checker.blockedRanges = append(checker.blockedRanges, ipNet)
			}
		}
	}

	return checker
}

// IsPrivateIP checks if an IP address is in a blocked private range.
// IPv4-mapped IPv6 addresses (e.g., ::ffff:127.0.0.1) are normalized
// to their IPv4 representation before checking.
func (s *SSRFChecker) IsPrivateIP(ip net.IP) bool {
	// Normalize IPv4-mapped IPv6 addresses to IPv4 for CIDR matching.
	// net.IPNet.Contains with a 4-byte CIDR (e.g., 127.0.0.0/8) does not
	// match a 16-byte IPv4-mapped address unless we convert first.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	for _, blocked := range s.blockedRanges {
		if blocked.Contains(ip) {
			return true
		}
	}
	return false
}

// ValidateURL checks if a URL is safe to request (prevents SSRF)
func (s *SSRFChecker) ValidateURL(rawURL string) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Check scheme
	scheme := strings.ToLower(parsedURL.Scheme)
	if !s.allowedSchemes[scheme] {
		return fmt.Errorf("URL scheme not allowed: %s", scheme)
	}

	host := strings.ToLower(parsedURL.Hostname())

	// Check for localhost variations (unless explicitly allowed)
	if !s.config.AllowLocalhost {
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return fmt.Errorf("localhost access not allowed")
		}
	}

	// Check host for cloud metadata service endpoints.
	// Use exact match to avoid false positives on legitimate hostnames
	// that happen to contain these strings as substrings.
	metadataHosts := []string{
		"169.254.169.254",          // AWS, GCP, Azure instance metadata
		"metadata.google.internal", // GCP metadata
	}
	for _, metaHost := range metadataHosts {
		if host == metaHost {
			return fmt.Errorf("metadata service access not allowed")
		}
	}

	// Resolve IP and check if it's private (skip if private IPs are allowed)
	if len(s.blockedRanges) > 0 {
		ips, err := net.LookupIP(host)
		if err != nil {
			// If DNS resolution fails, block the request to prevent DNS rebinding
			return fmt.Errorf("DNS resolution failed for %s: %w", host, err)
		}

		for _, ip := range ips {
			if s.IsPrivateIP(ip) {
				return fmt.Errorf("private IP access not allowed: %s", ip.String())
			}
		}
	}

	// Check for user info in URL (e.g., https://user:pass@host)
	if parsedURL.User != nil {
		return fmt.Errorf("user info in URL not allowed")
	}

	// Check for path traversal in URL path
	if strings.Contains(parsedURL.Path, "..") {
		return fmt.Errorf("path traversal not allowed")
	}

	// Check for hex-encoded IP addresses (e.g., 0x7f000001 for 127.0.0.1)
	if strings.Contains(host, "0x") {
		return fmt.Errorf("hex-encoded host not allowed")
	}

	return nil
}

// ResolveAndValidateHost resolves the hostname and validates that all resolved
// IPs are safe (not private/blocked). Returns the resolved IPs so they can be
// pinned in a DialContext to prevent DNS rebinding TOCTOU attacks.
func (s *SSRFChecker) ResolveAndValidateHost(host string) ([]net.IP, error) {
	if len(s.blockedRanges) == 0 {
		return nil, nil
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("DNS resolution failed for %s: %w", host, err)
	}

	for _, ip := range ips {
		if s.IsPrivateIP(ip) {
			return nil, fmt.Errorf("private IP access not allowed: %s", ip.String())
		}
	}

	return ips, nil
}

// pinnedIPDialer creates a DialContext that resolves DNS once at creation time,
// validates the resolved IPs against the SSRF checker, and then pins those IPs
// for all subsequent connections. This eliminates the TOCTOU window between DNS
// validation and connection establishment (DNS rebinding attack).
func pinnedIPDialer(checker *SSRFChecker, baseDialer *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid address %q: %w", addr, err)
		}

		// If the host is already an IP address, validate and connect directly.
		if ip := net.ParseIP(host); ip != nil {
			if checker.IsPrivateIP(ip) {
				return nil, fmt.Errorf("SSRF blocked: private IP %s", ip)
			}
			return baseDialer.DialContext(ctx, network, addr)
		}

		// Resolve DNS and validate.
		ips, err := checker.ResolveAndValidateHost(host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("no addresses found for %s", host)
		}

		// Connect to the first valid resolved IP (pin the DNS result).
		pinnedAddr := net.JoinHostPort(ips[0].String(), port)
		return baseDialer.DialContext(ctx, network, pinnedAddr)
	}
}

// NewSafeTransport creates an *http.Transport that pins DNS resolution at dial
// time, preventing DNS rebinding attacks. The SSRF checker resolves the hostname
// once, validates all IPs, and the custom DialContext connects directly to the
// validated IP address without re-resolving DNS.
func (s *SSRFChecker) NewSafeTransport(base *http.Transport) *http.Transport {
	if base == nil {
		base = http.DefaultTransport.(*http.Transport).Clone()
	}

	safe := base.Clone()
	baseDialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	safe.DialContext = pinnedIPDialer(s, baseDialer)
	return safe
}

// NewSafeDialer creates a websocket.Dialer that pins DNS resolution at dial
// time, preventing DNS rebinding attacks on WebSocket connections.
func (s *SSRFChecker) NewSafeDialer(base websocket.Dialer) websocket.Dialer {
	safe := base
	baseDialer := &net.Dialer{Timeout: base.HandshakeTimeout}
	safe.NetDialContext = pinnedIPDialer(s, baseDialer)
	safe.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	return safe
}
