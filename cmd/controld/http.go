package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"html"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ai-model-gateway/internal/control/api"
	"ai-model-gateway/internal/control/audit"
	authinfra "ai-model-gateway/internal/infra/auth"
	"ai-model-gateway/internal/infra/logger"
	_ "modernc.org/sqlite"
)

// createHandler creates the HTTP handler.
func (d *Daemon) createHandler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/-/health", d.healthHandler)
	mux.HandleFunc("/admin/login", d.adminLoginPageHandler)
	mux.HandleFunc("/admin/logout", d.adminBrowserLogoutHandler)
	mux.HandleFunc("/api/admin/login", d.adminLoginAPIHandler)
	mux.HandleFunc("/api/admin/logout", d.adminLogoutAPIHandler)
	mux.HandleFunc("/api/admin/session", d.adminSessionAPIHandler)

	deps := api.Deps{
		ConfigQuery: d.publisher,
		ConfigCommands: configCommandsAdapter{
			publisher: d.publisher,
			reloadFn:  d.reloadConfigFromSource,
		},
		ConfigTools: configToolsAdapter{
			publisher: d.publisher,
			compiler:  d.compiler,
		},
		AuditLog: d.auditStore,
		Runtime: api.RuntimeConfig{
			BundleVersion:   Version,
			BundleManifest:  "aigw-manifest.json",
			ConfigPath:      d.config.ConfigPath,
			DataDir:         d.config.DataDir,
			Listen:          d.config.Listen,
			GatewaySocket:   d.config.GatewaySocket,
			TelemetrySocket: d.config.TelemetrySocket,
			ConfigPaths: map[string]string{
				"controld":   "configs/controld.json",
				"gatewayd":   "configs/gatewayd.json",
				"telemetryd": "configs/telemetryd.json",
			},
		},
		ProbeRunner: probeRunnerAdapter{daemon: d},
		Replay:      d.replayHandler(),
		TelemetryRPCProvider: func() api.TelemetryQuerier {
			client := d.currentTelemetryRPC()
			if client == nil {
				return nil
			}
			return client
		},
		GatewayRPCProvider: func() api.GatewayController {
			client := d.currentGatewayRPC()
			if client == nil {
				return nil
			}
			return client
		},
		Version:         Version,
		StartedAt:       d.startedAt,
		AdminMiddleware: d.adminAuthMiddleware(),
	}
	if d.benchmarkSvc != nil {
		deps.Benchmarking = d.benchmarkSvc
	}
	api.Mount(mux, deps, d.frontendBundle)

	return mux
}

func (d *Daemon) replayHandler() http.Handler {
	if d.replayDB != nil {
		return api.NewReplayHandler(d.replayDB, "http://127.0.0.1:18080")
	}
	for _, candidate := range d.replayQueryStoreCandidates() {
		if _, err := os.Stat(candidate); err != nil {
			continue
		}
		db, err := sql.Open("sqlite", candidate)
		if err != nil {
			logger.Warn("could not open replay query store", "path", candidate, "error", err)
			continue
		}
		if err := db.Ping(); err != nil {
			logger.Warn("could not ping replay query store", "path", candidate, "error", err)
			_ = db.Close()
			continue
		}
		d.replayDB = db
		return api.NewReplayHandler(db, "http://127.0.0.1:18080")
	}
	return nil
}

func (d *Daemon) replayQueryStoreCandidates() []string {
	runtimeRoot := ".gateway-runtime"
	if strings.TrimSpace(d.config.TelemetrySocket) != "" {
		runtimeRoot = filepath.Dir(d.config.TelemetrySocket)
	}
	return []string{
		filepath.Join(runtimeRoot, "telemetry-migrated", "query.db"),
		filepath.Join(runtimeRoot, "telemetry", "query.db"),
		filepath.Join("data", "telemetry", "query.db"),
	}
}

func (d *Daemon) adminAuthMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authenticator, err := d.currentAuthenticator()
			if err != nil {
				writeAdminAuthError(w, r, http.StatusServiceUnavailable, "admin auth unavailable")
				return
			}
			if authenticator == nil {
				next.ServeHTTP(w, r)
				return
			}

			info, err := authenticator.Authenticate(r)
			if err != nil {
				if isPublicAdminShellRequest(r) {
					next.ServeHTTP(w, r)
					return
				}
				if isBrowserAdminPath(r.URL.Path) {
					http.Redirect(w, r, buildAdminLoginURL(r.URL.RequestURI()), http.StatusSeeOther)
					return
				}
				writeAdminAuthError(w, r, http.StatusUnauthorized, "authentication required")
				return
			}

			if isCookieAuthenticated(r) && isSameOriginWriteRequired(r.URL.Path, r.Method) {
				if !isValidSameOriginRequest(r) {
					writeAdminAuthError(w, r, http.StatusForbidden, "same-origin check failed")
					return
				}
			}

			if !canAccessAdminRoute(info.Role, r.Method) {
				writeAdminAuthError(w, r, http.StatusForbidden, "insufficient admin privileges")
				return
			}
			r = api.WithAdminRole(r, info.Role)
			next.ServeHTTP(w, r)
		})
	}
}

// isCookieAuthenticated checks if the request uses cookie authentication.
func isCookieAuthenticated(r *http.Request) bool {
	if r == nil {
		return false
	}
	for _, c := range r.Cookies() {
		if c.Name == "aigw" {
			return true
		}
	}
	return false
}

// isSameOriginWriteRequired returns true for paths that require same-origin validation.
func isSameOriginWriteRequired(path, method string) bool {
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		return false
	}
	return strings.HasPrefix(path, "/api/admin/config") ||
		strings.HasPrefix(path, "/api/admin/upstreams") ||
		strings.HasPrefix(path, "/api/admin/pricing/refresh") ||
		strings.HasPrefix(path, "/api/admin/runtime/preflight") ||
		strings.HasPrefix(path, "/api/admin/probe/") ||
		strings.HasPrefix(path, "/api/admin/replay") ||
		strings.HasPrefix(path, "/api/admin/benchmark/baselines/import") ||
		strings.HasPrefix(path, "/api/admin/benchmark/runs")
}

// isValidSameOriginRequest validates Origin/Referer for same-origin requests.
func isValidSameOriginRequest(r *http.Request) bool {
	expectedHost := requestHostForSameOrigin(r)
	expectedScheme := requestSchemeForSameOrigin(r)
	if expectedHost == "" || expectedScheme == "" {
		return false
	}

	origin := r.Header.Get("Origin")
	if origin != "" {
		return isSameOrigin(origin, expectedHost, expectedScheme)
	}

	referer := r.Header.Get("Referer")
	if referer != "" {
		return isSameOrigin(referer, expectedHost, expectedScheme)
	}

	return false
}

func requestHostForSameOrigin(r *http.Request) string {
	if r == nil {
		return ""
	}
	if forwardedHost := firstForwardedValue(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
		return forwardedHost
	}
	return strings.TrimSpace(r.Host)
}

func requestSchemeForSameOrigin(r *http.Request) string {
	if r == nil {
		return ""
	}
	if forwardedProto := firstForwardedValue(r.Header.Get("X-Forwarded-Proto")); forwardedProto != "" {
		return strings.ToLower(forwardedProto)
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func firstForwardedValue(value string) string {
	if idx := strings.Index(value, ","); idx >= 0 {
		value = value[:idx]
	}
	return strings.TrimSpace(value)
}

// isSameOrigin checks if the provided URL matches the expected origin.
func isSameOrigin(rawURL, expectedHost, expectedScheme string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	if !strings.EqualFold(u.Scheme, expectedScheme) {
		return false
	}

	actualName, actualPort := splitOriginHostPort(u.Host, expectedScheme)
	expectedName, expectedPort := splitOriginHostPort(expectedHost, expectedScheme)
	if actualPort != expectedPort {
		return false
	}
	return strings.EqualFold(actualName, expectedName)
}

func splitOriginHostPort(hostport, scheme string) (string, string) {
	hostport = strings.TrimSpace(hostport)
	if hostport == "" {
		return "", defaultPortForScheme(scheme)
	}

	if host, port, err := net.SplitHostPort(hostport); err == nil {
		return strings.Trim(host, "[]"), port
	}

	if strings.HasPrefix(hostport, "[") && strings.HasSuffix(hostport, "]") {
		return strings.Trim(hostport, "[]"), defaultPortForScheme(scheme)
	}

	if strings.Count(hostport, ":") > 1 {
		return strings.Trim(hostport, "[]"), defaultPortForScheme(scheme)
	}

	return strings.Trim(hostport, "[]"), defaultPortForScheme(scheme)
}

func defaultPortForScheme(scheme string) string {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "https":
		return "443"
	default:
		return "80"
	}
}

func (d *Daemon) currentAuthenticator() (*authinfra.Authenticator, error) {
	if d.publisher == nil {
		return nil, nil
	}

	cfg, err := d.publisher.GetCurrentConfig()
	if err != nil {
		return nil, err
	}
	if cfg == nil || !cfg.Admin.Enabled {
		return nil, nil
	}

	authenticator := authinfra.New(cfg.Admin.BootstrapToken, cfg.Admin.CookieSigningKey)
	authenticator.SetCookieSecure(!isLoopbackListenAddr(d.config.Listen))
	tokens := make([]authinfra.TokenEntry, 0, len(cfg.Admin.Tokens))
	for _, token := range cfg.Admin.Tokens {
		tokens = append(tokens, authinfra.TokenEntry{
			Name:  token.Name,
			Token: token.Token,
			Role:  token.Role,
		})
	}
	authenticator.SetTokens(tokens)
	return authenticator, nil
}

func isLoopbackListenAddr(addr string) bool {
	addr = strings.TrimSpace(addr)
	if addr == "" || strings.HasPrefix(addr, ":") {
		return false
	}

	host := addr
	if strings.Contains(addr, ":") {
		parsedHost, _, err := net.SplitHostPort(addr)
		if err != nil {
			return false
		}
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func canAccessAdminRoute(role, method string) bool {
	if role == authinfra.RoleAdmin {
		return true
	}
	if role == authinfra.RoleViewer {
		switch method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			return true
		}
	}
	return false
}

func isBrowserAdminPath(path string) bool {
	return path == "/admin" || strings.HasPrefix(path, "/admin/")
}

func isPublicAdminShellRequest(r *http.Request) bool {
	if r == nil || r.URL == nil || !isBrowserAdminPath(r.URL.Path) {
		return false
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func buildAdminLoginURL(next string) string {
	values := url.Values{}
	if strings.TrimSpace(next) != "" {
		values.Set("next", next)
	}
	if encoded := values.Encode(); encoded != "" {
		return "/admin/login?" + encoded
	}
	return "/admin/login"
}

func defaultAdminNext(next string) string {
	next = strings.TrimSpace(next)
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/admin/login") {
		return "/admin"
	}
	return next
}

func (d *Daemon) adminLoginPageHandler(w http.ResponseWriter, r *http.Request) {
	authenticator, err := d.currentAuthenticator()
	if err != nil {
		http.Error(w, "admin auth unavailable", http.StatusServiceUnavailable)
		return
	}
	if authenticator == nil {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	if info, err := authenticator.Authenticate(r); err == nil && info != nil {
		http.Redirect(w, r, defaultAdminNext(r.URL.Query().Get("next")), http.StatusSeeOther)
		return
	}

	switch r.Method {
	case http.MethodGet:
		d.renderAdminLoginPage(w, http.StatusOK, "", r.URL.Query().Get("next"))
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			d.renderAdminLoginPage(w, http.StatusBadRequest, "invalid form submission", r.FormValue("next"))
			return
		}
		token := strings.TrimSpace(r.FormValue("token"))
		next := defaultAdminNext(r.FormValue("next"))
		if err := authenticator.Login(w, token); err != nil {
			d.renderAdminLoginPage(w, http.StatusUnauthorized, "invalid token", next)
			return
		}
		http.Redirect(w, r, next, http.StatusSeeOther)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (d *Daemon) renderAdminLoginPage(w http.ResponseWriter, status int, message, next string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`<!DOCTYPE html><html><body><h1>AI-Model-Gateway Admin Login</h1>`))
	if strings.TrimSpace(message) != "" {
		_, _ = w.Write([]byte(`<p style="color:#b91c1c;">` + html.EscapeString(message) + `</p>`))
	}
	_, _ = w.Write([]byte(`<form method="post" action="/admin/login"><label>Token <input type="password" name="token" autofocus /></label><input type="hidden" name="next" value="` + html.EscapeString(defaultAdminNext(next)) + `" /><button type="submit">Login</button></form><p>Use an admin or viewer token from config.yaml.</p></body></html>`))
}

func (d *Daemon) adminBrowserLogoutHandler(w http.ResponseWriter, r *http.Request) {
	authenticator, err := d.currentAuthenticator()
	if err == nil && authenticator != nil {
		authenticator.Logout(w)
	}
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func (d *Daemon) adminLoginAPIHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAdminAuthError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	authenticator, err := d.currentAuthenticator()
	if err != nil {
		writeAdminAuthError(w, r, http.StatusServiceUnavailable, "admin auth unavailable")
		return
	}
	if authenticator == nil {
		writeAdminAuthError(w, r, http.StatusNotFound, "admin auth disabled")
		return
	}

	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminAuthError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	info := authenticator.LoginInfo(strings.TrimSpace(req.Token))
	if info == nil {
		d.recordAuditEvent(r, "auth.login", "admin", false, "invalid token", nil)
		writeAdminAuthError(w, r, http.StatusUnauthorized, "invalid token")
		return
	}
	if err := authenticator.Login(w, req.Token); err != nil {
		d.recordAuditEvent(r, "auth.login", info.Name, false, err.Error(), nil)
		writeAdminAuthError(w, r, http.StatusUnauthorized, "invalid token")
		return
	}
	d.recordAuditEvent(r, "auth.login", info.Name, true, "", map[string]any{"role": info.Role})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"authenticated": true,
		"name":          info.Name,
		"role":          info.Role,
	})
}

func (d *Daemon) adminLogoutAPIHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAdminAuthError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	authenticator, err := d.currentAuthenticator()
	if err == nil && authenticator != nil {
		authenticator.Logout(w)
	}
	d.recordAuditEvent(r, "auth.logout", "admin", true, "", nil)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"authenticated": false})
}

func (d *Daemon) adminSessionAPIHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAdminAuthError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	authenticator, err := d.currentAuthenticator()
	if err != nil {
		writeAdminAuthError(w, r, http.StatusServiceUnavailable, "admin auth unavailable")
		return
	}
	if authenticator == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"enabled":       false,
			"authenticated": true,
			"role":          authinfra.RoleAdmin,
		})
		return
	}
	info, err := authenticator.Authenticate(r)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"enabled":       true,
			"authenticated": false,
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"enabled":       true,
		"authenticated": true,
		"name":          info.Name,
		"role":          info.Role,
	})
}

func (d *Daemon) recordAuditEvent(r *http.Request, action string, resource string, success bool, errText string, details map[string]any) {
	if d.auditStore == nil {
		return
	}
	role := api.AdminRoleFromRequest(r)
	actor := role
	if actor == "" {
		actor = "anonymous"
	}
	source := ""
	if r != nil {
		source = r.RemoteAddr
	}
	_ = d.auditStore.Record(context.Background(), audit.Event{
		Actor:    actor,
		Role:     role,
		Source:   source,
		Action:   action,
		Resource: resource,
		Success:  success,
		Error:    errText,
		Details:  details,
	})
}

func writeAdminAuthError(w http.ResponseWriter, r *http.Request, status int, message string) {
	if strings.HasPrefix(r.URL.Path, "/api/admin/") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte("<!DOCTYPE html><html><body><h1>Admin Authentication Required</h1><p>" + message + "</p><p>Open /admin to sign in via the login form, or use a Bearer token for API access.</p></body></html>"))
}

// healthHandler handles health check requests.
func (d *Daemon) healthHandler(w http.ResponseWriter, r *http.Request) {
	status := "healthy"

	if d.currentGatewayRPC() == nil {
		status = "degraded"
	}
	if d.currentTelemetryRPC() == nil {
		status = "degraded"
	}

	resp := map[string]interface{}{
		"status":    status,
		"version":   Version,
		"startedAt": d.startedAt.UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
