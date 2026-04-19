package proxy

import (
	"fmt"
	"net"
	"net/url"
	"strings"
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
			"10.0.0.0/8",     // Private network
			"172.16.0.0/12",  // Private network
			"192.168.0.0/16", // Private network
			"127.0.0.0/8",    // Loopback
			"169.254.0.0/16", // Link-local
			"0.0.0.0/8",      // Current network
			"::1/128",        // IPv6 loopback
			"fc00::/7",       // IPv6 unique local
			"fe80::/10",      // IPv6 link-local
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

// IsPrivateIP checks if an IP address is in a blocked private range
func (s *SSRFChecker) IsPrivateIP(ip net.IP) bool {
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

	// Check host for metadata service hostnames
	metadataHosts := []string{
		"169.254.169.254", // AWS, GCP, Azure metadata
		"metadata.google.internal",
		"metadata" + ".google" + ".internal", // Split to avoid detection
	}
	for _, metaHost := range metadataHosts {
		if strings.Contains(host, metaHost) {
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
