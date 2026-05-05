// Package clientconfig resolves gateway URLs and merges local tool configs.
package clientconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const openAIPathPrefix = "/v1"

// GatewaydListenJSON is the subset of gatewayd bootstrap JSON we read.
type GatewaydListenJSON struct {
	Listen string `json:"listen"`
}

// ResolveOrigin returns the HTTP origin for the data plane (no path), e.g. http://127.0.0.1:18080.
// gatewayURLOverride, if non-empty, must be a full URL with scheme; path and trailing slashes are stripped.
// Otherwise gatewaydJSONPath is read for the "listen" field.
func ResolveOrigin(gatewayURLOverride, gatewaydJSONPath string) (string, error) {
	if s := strings.TrimSpace(gatewayURLOverride); s != "" {
		return normalizeOriginFromFullURL(s)
	}
	data, err := os.ReadFile(gatewaydJSONPath)
	if err != nil {
		return "", fmt.Errorf("read gatewayd config %q: %w", gatewaydJSONPath, err)
	}
	var cfg GatewaydListenJSON
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("parse gatewayd config: %w", err)
	}
	return ListenToOrigin(strings.TrimSpace(cfg.Listen))
}

// OpenAICompatibleBase returns <origin>/v1 for OpenAI-compatible clients (Codex, OpenClaw openai-completions).
func OpenAICompatibleBase(origin string) string {
	return strings.TrimSuffix(origin, "/") + openAIPathPrefix
}

func normalizeOriginFromFullURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", errors.New("gateway URL must include scheme and host (e.g. http://127.0.0.1:18080)")
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	host := u.Hostname()
	port := u.Port()
	host = clientFacingHost(host)
	if port != "" {
		h := host
		if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
			h = "[" + host + "]"
		}
		return u.Scheme + "://" + net.JoinHostPort(h, port), nil
	}
	return u.Scheme + "://" + host, nil
}

// ListenToOrigin maps gatewayd "listen" to a client-facing HTTP origin.
func ListenToOrigin(listen string) (string, error) {
	s := strings.TrimSpace(listen)
	if s == "" {
		return "", errors.New("gateway listen address is empty")
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return normalizeOriginFromFullURL(s)
	}
	hostport := s
	if strings.HasPrefix(s, ":") {
		hostport = "127.0.0.1" + s
	} else if h, p, err := net.SplitHostPort(s); err == nil {
		hostport = net.JoinHostPort(clientFacingHost(h), p)
	}
	if !strings.Contains(hostport, "://") {
		return "http://" + hostport, nil
	}
	return "", fmt.Errorf("invalid listen %q", listen)
}

func clientFacingHost(host string) string {
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		return "127.0.0.1"
	default:
		return host
	}
}

// DefaultGatewaydPath returns configs/gatewayd.json under configDir.
func DefaultGatewaydPath(configDir string) string {
	return filepath.Join(configDir, "gatewayd.json")
}
