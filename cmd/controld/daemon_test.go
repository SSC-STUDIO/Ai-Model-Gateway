package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/control/compiler"

	authinfra "ai-model-gateway/internal/infra/auth"
)

func TestGatewayReadinessRepublishDefaultMinInterval(t *testing.T) {
	d := &Daemon{config: Config{}}
	if got := d.gatewayReadinessRepublishMinInterval(); got != 15*time.Second {
		t.Fatalf("default min interval = %v, want 15s", got)
	}
}

func TestMaybeRepublishForGatewayReadinessThrottles(t *testing.T) {
	var calls atomic.Int32
	d := &Daemon{config: Config{GatewayReadinessRepublishMinIntervalSec: 10}}
	d.testRepublishHook = func(string) { calls.Add(1) }
	t0 := time.Unix(1700000000, 0)
	st := &gatewaycontrol.GetStatusResponse{Readiness: gatewaycontrol.ReadinessStarting}

	d.maybeRepublishForGatewayReadiness(st, t0)
	if got := calls.Load(); got != 1 {
		t.Fatalf("after first tick calls = %d, want 1", got)
	}
	d.maybeRepublishForGatewayReadiness(st, t0.Add(5*time.Second))
	if got := calls.Load(); got != 1 {
		t.Fatalf("inside throttle window calls = %d, want 1", got)
	}
	d.maybeRepublishForGatewayReadiness(st, t0.Add(10*time.Second))
	if got := calls.Load(); got != 2 {
		t.Fatalf("after throttle window calls = %d, want 2", got)
	}

	d.maybeRepublishForGatewayReadiness(&gatewaycontrol.GetStatusResponse{
		Readiness: gatewaycontrol.ReadinessReady,
	}, t0.Add(300*time.Second))
	if got := calls.Load(); got != 2 {
		t.Fatalf("when ready calls = %d, want 2", got)
	}
}

func TestNewDaemonCreatesDataDir(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "control-data")
	d, err := NewDaemon(Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("NewDaemon() error = %v", err)
	}
	if d == nil {
		t.Fatal("NewDaemon() returned nil")
	}
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		t.Fatal("NewDaemon() did not create data directory")
	}
}

func TestNewDaemonFailsOnInvalidDataDir(t *testing.T) {
	// Create a file instead of a directory to make MkdirAll fail
	filePath := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := NewDaemon(Config{DataDir: filePath})
	if err == nil {
		t.Fatal("NewDaemon() should fail when data dir cannot be created")
	}
}

func TestDefaultSocketPath(t *testing.T) {
	path := defaultSocketPath("test-socket")
	if path == "" {
		t.Fatal("defaultSocketPath() returned empty string")
	}
}

func TestLoadConfigFromFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	cfg := Config{
		Listen:          "127.0.0.1:28081",
		DataDir:         "data/custom",
		LogLevel:        "debug",
		ReadTimeoutSec:  10,
		WriteTimeoutSec: 20,
		IdleTimeoutSec:  30,
	}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	loaded := loadConfig(configPath, "", "", "", "", "")
	if loaded.Listen != "127.0.0.1:28081" {
		t.Fatalf("Listen = %q, want %q", loaded.Listen, "127.0.0.1:28081")
	}
	if loaded.LogLevel != "debug" {
		t.Fatalf("LogLevel = %q, want %q", loaded.LogLevel, "debug")
	}
}

func TestLoadConfigFromInvalidFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(configPath, []byte("not json"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Should not panic, should use defaults with warning
	cfg := loadConfig(configPath, "", "", "", "", "")
	if cfg.Listen != "127.0.0.1:18081" {
		t.Fatalf("Listen should default to 127.0.0.1:18081, got %q", cfg.Listen)
	}
}

func TestBuildAdminLoginURL(t *testing.T) {
	cases := []struct {
		next string
		want string
	}{
		{"", "/admin/login"},
		{"/admin", "/admin/login?next=%2Fadmin"},
		{"/admin/config", "/admin/login?next=%2Fadmin%2Fconfig"},
	}

	for _, tc := range cases {
		got := buildAdminLoginURL(tc.next)
		if got != tc.want {
			t.Fatalf("buildAdminLoginURL(%q) = %q, want %q", tc.next, got, tc.want)
		}
	}
}

func TestDefaultAdminNext(t *testing.T) {
	cases := []struct {
		next string
		want string
	}{
		{"", "/admin"},
		{"  ", "/admin"},
		{"/admin", "/admin"},
		{"/admin/config", "/admin/config"},
		{"/api/admin/config", "/api/admin/config"},
		{"//evil.com", "/admin"},
		{"/admin/login", "/admin"},
		{"/admin/login?next=/", "/admin"},
	}

	for _, tc := range cases {
		got := defaultAdminNext(tc.next)
		if got != tc.want {
			t.Fatalf("defaultAdminNext(%q) = %q, want %q", tc.next, got, tc.want)
		}
	}
}

func TestIsLoopbackListenAddr(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:8080", true},
		{"localhost:8080", true},
		{"[::1]:8080", true},
		{"0.0.0.0:8080", false},
		{":8080", false},
		{"", false},
		{"192.168.1.1:8080", false},
		{"invalid", false},
	}

	for _, tc := range cases {
		got := isLoopbackListenAddr(tc.addr)
		if got != tc.want {
			t.Fatalf("isLoopbackListenAddr(%q) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}

func TestCanAccessAdminRoute(t *testing.T) {
	cases := []struct {
		role   string
		method string
		want   bool
	}{
		{authinfra.RoleAdmin, http.MethodGet, true},
		{authinfra.RoleAdmin, http.MethodPost, true},
		{authinfra.RoleAdmin, http.MethodDelete, true},
		{authinfra.RoleViewer, http.MethodGet, true},
		{authinfra.RoleViewer, http.MethodHead, true},
		{authinfra.RoleViewer, http.MethodOptions, true},
		{authinfra.RoleViewer, http.MethodPost, false},
		{authinfra.RoleViewer, http.MethodPut, false},
		{"unknown", http.MethodGet, false},
	}

	for _, tc := range cases {
		got := canAccessAdminRoute(tc.role, tc.method)
		if got != tc.want {
			t.Fatalf("canAccessAdminRoute(%q, %q) = %v, want %v", tc.role, tc.method, got, tc.want)
		}
	}
}

func TestIsBrowserAdminPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/admin", true},
		{"/admin/", true},
		{"/admin/config", true},
		{"/api/admin", false},
		{"/api/admin/config", false},
		{"/", false},
	}

	for _, tc := range cases {
		got := isBrowserAdminPath(tc.path)
		if got != tc.want {
			t.Fatalf("isBrowserAdminPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestIsPublicAdminShellRequest(t *testing.T) {
	cases := []struct {
		method string
		path   string
		want   bool
	}{
		{http.MethodGet, "/admin", true},
		{http.MethodHead, "/admin", true},
		{http.MethodOptions, "/admin", true},
		{http.MethodPost, "/admin", false},
		{http.MethodGet, "/api/admin", false},
		{http.MethodGet, "/", false},
	}

	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		got := isPublicAdminShellRequest(req)
		if got != tc.want {
			t.Fatalf("isPublicAdminShellRequest(%q, %q) = %v, want %v", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestIsPublicAdminShellRequestNilSafety(t *testing.T) {
	if isPublicAdminShellRequest(nil) {
		t.Fatal("isPublicAdminShellRequest(nil) should be false")
	}
	req, _ := http.NewRequest(http.MethodGet, "/admin", nil)
	req.URL = nil
	if isPublicAdminShellRequest(req) {
		t.Fatal("isPublicAdminShellRequest with nil URL should be false")
	}
}

func TestRevisionIDFromConfigNil(t *testing.T) {
	_, err := revisionIDFromConfig(nil)
	if err == nil {
		t.Fatal("revisionIDFromConfig(nil) should return error")
	}
}

func TestPublisherStatePaths(t *testing.T) {
	d := &Daemon{config: Config{DataDir: "/tmp/control"}}
	if got := d.publisherSQLiteStatePath(); !strings.HasSuffix(got, "publisher-state.db") {
		t.Fatalf("publisherSQLiteStatePath() = %q, want suffix publisher-state.db", got)
	}
	if got := d.legacyPublisherStatePath(); !strings.HasSuffix(got, "publisher-state.json") {
		t.Fatalf("legacyPublisherStatePath() = %q, want suffix publisher-state.json", got)
	}
}

func TestHealthHandler(t *testing.T) {
	d := &Daemon{
		config:    Config{Listen: "127.0.0.1:18081"},
		startedAt: time.Unix(1_710_000_000, 0),
	}

	req := httptest.NewRequest(http.MethodGet, "/-/health", nil)
	rec := httptest.NewRecorder()
	d.healthHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("healthHandler() status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if resp["status"] != "degraded" {
		t.Fatalf("status = %q, want degraded", resp["status"])
	}
	if resp["version"] != Version {
		t.Fatalf("version = %q, want %q", resp["version"], Version)
	}
}

func TestHealthHandlerWithConnections(t *testing.T) {
	d := &Daemon{
		config:       Config{Listen: "127.0.0.1:18081"},
		startedAt:    time.Unix(1_710_000_000, 0),
		gatewayRPC:   &GatewayClient{},
		telemetryRPC: &TelemetryClient{},
	}

	req := httptest.NewRequest(http.MethodGet, "/-/health", nil)
	rec := httptest.NewRecorder()
	d.healthHandler(rec, req)

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if resp["status"] != "healthy" {
		t.Fatalf("status = %q, want healthy", resp["status"])
	}
}

func TestAdminLoginPageHandlerRedirectsWhenAuthDisabled(t *testing.T) {
	configPath := writeTestAuthoringConfig(t)
	comp := compiler.NewCompiler()
	d := &Daemon{
		config:    Config{ConfigPath: configPath, Listen: "127.0.0.1:18081"},
		compiler:  comp,
		publisher: newConfiguredPublisher(nil, comp, nil),
	}
	if err := d.seedInitialRevision(); err != nil {
		t.Fatalf("seedInitialRevision() error = %v", err)
	}

	handler := d.createHandler()
	req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
}

func TestAdminLoginPageHandlerRendersForm(t *testing.T) {
	configPath := writeAdminAuthoringConfig(t)
	comp := compiler.NewCompiler()
	d := &Daemon{
		config:    Config{ConfigPath: configPath, Listen: "127.0.0.1:18081"},
		compiler:  comp,
		publisher: newConfiguredPublisher(nil, comp, nil),
	}
	if err := d.seedInitialRevision(); err != nil {
		t.Fatalf("seedInitialRevision() error = %v", err)
	}

	handler := d.createHandler()
	req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "Admin Login") {
		t.Fatal("login page missing title")
	}
}

func TestAdminLoginPageHandlerPOSTInvalidToken(t *testing.T) {
	configPath := writeAdminAuthoringConfig(t)
	comp := compiler.NewCompiler()
	d := &Daemon{
		config:    Config{ConfigPath: configPath, Listen: "127.0.0.1:18081"},
		compiler:  comp,
		publisher: newConfiguredPublisher(nil, comp, nil),
	}
	if err := d.seedInitialRevision(); err != nil {
		t.Fatalf("seedInitialRevision() error = %v", err)
	}

	handler := d.createHandler()
	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader("token=bad-token&next=/admin"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAdminLoginPageHandlerPOSTValidToken(t *testing.T) {
	configPath := writeAdminAuthoringConfig(t)
	comp := compiler.NewCompiler()
	d := &Daemon{
		config:    Config{ConfigPath: configPath, Listen: "127.0.0.1:18081"},
		compiler:  comp,
		publisher: newConfiguredPublisher(nil, comp, nil),
	}
	if err := d.seedInitialRevision(); err != nil {
		t.Fatalf("seedInitialRevision() error = %v", err)
	}

	handler := d.createHandler()
	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader("token="+strings.Repeat("a", 34)+"&next=/admin"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
}

func TestAdminLoginPageHandlerMethodNotAllowed(t *testing.T) {
	configPath := writeAdminAuthoringConfig(t)
	comp := compiler.NewCompiler()
	d := &Daemon{
		config:    Config{ConfigPath: configPath, Listen: "127.0.0.1:18081"},
		compiler:  comp,
		publisher: newConfiguredPublisher(nil, comp, nil),
	}
	if err := d.seedInitialRevision(); err != nil {
		t.Fatalf("seedInitialRevision() error = %v", err)
	}

	handler := d.createHandler()
	req := httptest.NewRequest(http.MethodDelete, "/admin/login", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestAdminBrowserLogoutHandler(t *testing.T) {
	configPath := writeAdminAuthoringConfig(t)
	comp := compiler.NewCompiler()
	d := &Daemon{
		config:    Config{ConfigPath: configPath, Listen: "127.0.0.1:18081"},
		compiler:  comp,
		publisher: newConfiguredPublisher(nil, comp, nil),
	}
	if err := d.seedInitialRevision(); err != nil {
		t.Fatalf("seedInitialRevision() error = %v", err)
	}

	handler := d.createHandler()
	req := httptest.NewRequest(http.MethodGet, "/admin/logout", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
}

func TestAdminLoginAPIHandler(t *testing.T) {
	configPath := writeAdminAuthoringConfig(t)
	comp := compiler.NewCompiler()
	d := &Daemon{
		config:    Config{ConfigPath: configPath, Listen: "127.0.0.1:18081"},
		compiler:  comp,
		publisher: newConfiguredPublisher(nil, comp, nil),
	}
	if err := d.seedInitialRevision(); err != nil {
		t.Fatalf("seedInitialRevision() error = %v", err)
	}

	handler := d.createHandler()

	// Test invalid method
	req := httptest.NewRequest(http.MethodGet, "/api/admin/login", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /api/admin/login status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}

	// Test valid login
	req = httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"token":"`+strings.Repeat("a", 34)+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/admin/login status = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["authenticated"] != true {
		t.Fatalf("authenticated = %v, want true", resp["authenticated"])
	}

	// Test invalid token
	req = httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"token":"bad"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /api/admin/login bad token status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAdminLoginAPIHandlerAuthDisabled(t *testing.T) {
	configPath := writeTestAuthoringConfig(t)
	comp := compiler.NewCompiler()
	d := &Daemon{
		config:    Config{ConfigPath: configPath, Listen: "127.0.0.1:18081"},
		compiler:  comp,
		publisher: newConfiguredPublisher(nil, comp, nil),
	}
	if err := d.seedInitialRevision(); err != nil {
		t.Fatalf("seedInitialRevision() error = %v", err)
	}

	handler := d.createHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"token":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestAdminLogoutAPIHandler(t *testing.T) {
	configPath := writeAdminAuthoringConfig(t)
	comp := compiler.NewCompiler()
	d := &Daemon{
		config:    Config{ConfigPath: configPath, Listen: "127.0.0.1:18081"},
		compiler:  comp,
		publisher: newConfiguredPublisher(nil, comp, nil),
	}
	if err := d.seedInitialRevision(); err != nil {
		t.Fatalf("seedInitialRevision() error = %v", err)
	}

	handler := d.createHandler()

	// Test invalid method
	req := httptest.NewRequest(http.MethodGet, "/api/admin/logout", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /api/admin/logout status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}

	// Test valid logout
	req = httptest.NewRequest(http.MethodPost, "/api/admin/logout", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/admin/logout status = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["authenticated"] != false {
		t.Fatalf("authenticated = %v, want false", resp["authenticated"])
	}
}

func TestAdminSessionAPIHandler(t *testing.T) {
	configPath := writeAdminAuthoringConfig(t)
	comp := compiler.NewCompiler()
	d := &Daemon{
		config:    Config{ConfigPath: configPath, Listen: "127.0.0.1:18081"},
		compiler:  comp,
		publisher: newConfiguredPublisher(nil, comp, nil),
	}
	if err := d.seedInitialRevision(); err != nil {
		t.Fatalf("seedInitialRevision() error = %v", err)
	}

	handler := d.createHandler()

	// Test invalid method
	req := httptest.NewRequest(http.MethodPost, "/api/admin/session", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/admin/session status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}

	// Test unauthenticated session
	req = httptest.NewRequest(http.MethodGet, "/api/admin/session", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/admin/session status = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["enabled"] != true {
		t.Fatalf("enabled = %v, want true", resp["enabled"])
	}
	if resp["authenticated"] != false {
		t.Fatalf("authenticated = %v, want false", resp["authenticated"])
	}
}

func TestAdminSessionAPIHandlerAuthDisabled(t *testing.T) {
	configPath := writeTestAuthoringConfig(t)
	comp := compiler.NewCompiler()
	d := &Daemon{
		config:    Config{ConfigPath: configPath, Listen: "127.0.0.1:18081"},
		compiler:  comp,
		publisher: newConfiguredPublisher(nil, comp, nil),
	}
	if err := d.seedInitialRevision(); err != nil {
		t.Fatalf("seedInitialRevision() error = %v", err)
	}

	handler := d.createHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/session", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/admin/session status = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["enabled"] != false {
		t.Fatalf("enabled = %v, want false", resp["enabled"])
	}
	if resp["authenticated"] != true {
		t.Fatalf("authenticated = %v, want true", resp["authenticated"])
	}
	if resp["role"] != authinfra.RoleAdmin {
		t.Fatalf("role = %v, want %v", resp["role"], authinfra.RoleAdmin)
	}
}

func TestWriteAdminAuthErrorAPIPath(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/admin/config", nil)
	rec := httptest.NewRecorder()
	writeAdminAuthError(rec, req, http.StatusUnauthorized, "auth required")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content-type = %q, want application/json", rec.Header().Get("Content-Type"))
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] != "auth required" {
		t.Fatalf("error = %q, want %q", resp["error"], "auth required")
	}
}

func TestWriteAdminAuthErrorBrowserPath(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin/config", nil)
	rec := httptest.NewRecorder()
	writeAdminAuthError(rec, req, http.StatusForbidden, "forbidden")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("content-type = %q, want text/html", rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), "forbidden") {
		t.Fatal("body missing error message")
	}
}

func TestIsSameOriginWriteRequired(t *testing.T) {
	tests := []struct {
		path   string
		method string
		want   bool
	}{
		{"/api/admin/config/publish", "POST", true},
		{"/api/admin/config/rollback", "POST", true},
		{"/api/admin/config", "PUT", true},
		{"/api/admin/config", "GET", false},
		{"/api/admin/upstreams/test", "POST", true},
		{"/api/admin/pricing/refresh", "POST", true},
		{"/api/admin/overview", "GET", false},
		{"/admin", "GET", false},
		{"/admin", "POST", false},
	}

	for _, tc := range tests {
		t.Run(tc.path+"_"+tc.method, func(t *testing.T) {
			if got := isSameOriginWriteRequired(tc.path, tc.method); got != tc.want {
				t.Errorf("isSameOriginWriteRequired(%q, %q) = %v, want %v", tc.path, tc.method, got, tc.want)
			}
		})
	}
}

func TestIsValidSameOriginRequest(t *testing.T) {
	tests := []struct {
		name           string
		origin         string
		referer        string
		requestHost    string
		forwardedHost  string
		forwardedProto string
		isTLS          bool
		want           bool
	}{
		{"valid origin", "http://127.0.0.1:18081", "", "127.0.0.1:18081", "", "", false, true},
		{"valid referer", "", "http://127.0.0.1:18081/admin", "127.0.0.1:18081", "", "", false, true},
		{"invalid origin", "http://evil.com", "", "127.0.0.1:18081", "", "", false, false},
		{"invalid referer", "", "http://evil.com/admin", "127.0.0.1:18081", "", "", false, false},
		{"no headers", "", "", "127.0.0.1:18081", "", "", false, false},
		{"request host localhost", "http://localhost:18081", "", "localhost:18081", "", "", false, true},
		{"forwarded https host", "https://admin.example.com", "", "127.0.0.1:18081", "admin.example.com", "https", false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/admin/config/publish", nil)
			req.Host = tc.requestHost
			if tc.isTLS {
				req.TLS = &tls.ConnectionState{}
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.referer != "" {
				req.Header.Set("Referer", tc.referer)
			}
			if tc.forwardedHost != "" {
				req.Header.Set("X-Forwarded-Host", tc.forwardedHost)
			}
			if tc.forwardedProto != "" {
				req.Header.Set("X-Forwarded-Proto", tc.forwardedProto)
			}
			if got := isValidSameOriginRequest(req); got != tc.want {
				t.Errorf("isValidSameOriginRequest() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsSameOrigin(t *testing.T) {
	tests := []struct {
		name           string
		rawURL         string
		expectedHost   string
		expectedScheme string
		want           bool
	}{
		// HTTP cases
		{"http valid", "http://127.0.0.1:18081", "127.0.0.1:18081", "http", true},
		{"http default port", "http://127.0.0.1", "127.0.0.1", "http", true},
		{"http wrong scheme", "https://127.0.0.1:18081", "127.0.0.1:18081", "http", false},
		{"http wrong host", "http://evil.com:18081", "127.0.0.1:18081", "http", false},
		// HTTPS cases
		{"https valid", "https://127.0.0.1:18081", "127.0.0.1:18081", "https", true},
		{"https wrong scheme", "http://127.0.0.1:18081", "127.0.0.1:18081", "https", false},
		{"ipv6 default port", "https://[::1]", "[::1]", "https", true},
		// Invalid URL
		{"invalid url", "://invalid", "127.0.0.1:18081", "http", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSameOrigin(tc.rawURL, tc.expectedHost, tc.expectedScheme); got != tc.want {
				t.Errorf("isSameOrigin(%q, %q, %q) = %v, want %v", tc.rawURL, tc.expectedHost, tc.expectedScheme, got, tc.want)
			}
		})
	}
}

func TestPublishInitialRevisionNilPublisher(t *testing.T) {
	d := &Daemon{}
	if err := d.publishInitialRevision(); err == nil {
		t.Fatal("publishInitialRevision() with nil publisher should error")
	}
}

func TestPublishInitialRevisionNilGateway(t *testing.T) {
	configPath := writeTestAuthoringConfig(t)
	comp := compiler.NewCompiler()
	d := &Daemon{
		config:    Config{ConfigPath: configPath},
		compiler:  comp,
		publisher: newConfiguredPublisher(nil, comp, nil),
	}
	if err := d.seedInitialRevision(); err != nil {
		t.Fatalf("seedInitialRevision() error = %v", err)
	}
	if err := d.publishInitialRevision(); err != nil {
		t.Fatalf("publishInitialRevision() with nil gateway should not error, got: %v", err)
	}
}

func TestRestoreOrSeedInitialRevisionNilPublisher(t *testing.T) {
	d := &Daemon{}
	if err := d.restoreOrSeedInitialRevision(); err == nil {
		t.Fatal("restoreOrSeedInitialRevision() with nil publisher should error")
	}
}

func TestCurrentAuthenticatorNilPublisher(t *testing.T) {
	d := &Daemon{}
	auth, err := d.currentAuthenticator()
	if err != nil {
		t.Fatalf("currentAuthenticator() error = %v", err)
	}
	if auth != nil {
		t.Fatal("currentAuthenticator() with nil publisher should return nil")
	}
}

func TestCurrentAuthenticatorConfigError(t *testing.T) {
	configPath := writeTestAuthoringConfig(t)
	comp := compiler.NewCompiler()
	d := &Daemon{
		config:    Config{ConfigPath: configPath},
		compiler:  comp,
		publisher: newConfiguredPublisher(nil, comp, nil),
	}
	if err := d.seedInitialRevision(); err != nil {
		t.Fatalf("seedInitialRevision() error = %v", err)
	}
	auth, err := d.currentAuthenticator()
	if err != nil {
		t.Fatalf("currentAuthenticator() error = %v", err)
	}
	if auth != nil {
		t.Fatal("currentAuthenticator() should return nil when admin is disabled")
	}
}

func TestLoadInitialRevisionMissingFile(t *testing.T) {
	_, err := loadInitialRevision("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("loadInitialRevision() with missing file should error")
	}
}

func TestShutdown(t *testing.T) {
	dataDir := t.TempDir()
	d, err := NewDaemon(Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("NewDaemon() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := d.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestNewConfiguredPublisher(t *testing.T) {
	gateway := &stubGatewayRPC{}
	comp := compiler.NewCompiler()
	p := newConfiguredPublisher(gateway, comp, nil)
	if p == nil {
		t.Fatal("newConfiguredPublisher() returned nil")
	}
}

func TestDaemonShutdownWithRPC(t *testing.T) {
	dataDir := t.TempDir()
	d, err := NewDaemon(Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("NewDaemon() error = %v", err)
	}

	// Add mock RPC clients
	d.gatewayRPC = &GatewayClient{}
	d.telemetryRPC = &TelemetryClient{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := d.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestPublishInitialRevisionWithGateway(t *testing.T) {
	configPath := writeTestAuthoringConfig(t)
	gateway := &stubGatewayRPC{}
	comp := compiler.NewCompiler()
	d := &Daemon{
		config:     Config{ConfigPath: configPath},
		compiler:   comp,
		publisher:  newConfiguredPublisher(gateway, comp, nil),
		gatewayRPC: &GatewayClient{},
	}
	if err := d.seedInitialRevision(); err != nil {
		t.Fatalf("seedInitialRevision() error = %v", err)
	}

	// publishInitialRevision should succeed with gatewayRPC
	if err := d.publishInitialRevision(); err != nil {
		t.Fatalf("publishInitialRevision() error = %v", err)
	}
}

func TestConfigUpdatePersistsAuthoringConfig(t *testing.T) {
	configPath := writeAdminAuthoringConfig(t)
	gateway := &stubGatewayRPC{}
	comp := compiler.NewCompiler()
	publisher := newConfiguredPublisher(gateway, comp, nil)
	d := &Daemon{
		config:    Config{ConfigPath: configPath, Listen: "127.0.0.1:18081"},
		compiler:  comp,
		publisher: publisher,
	}
	if err := d.seedInitialRevision(); err != nil {
		t.Fatalf("seedInitialRevision() error = %v", err)
	}

	view, err := publisher.GetCurrentConfigView()
	if err != nil {
		t.Fatalf("GetCurrentConfigView() error = %v", err)
	}
	if view == nil || view.Config == nil {
		t.Fatal("expected seeded config view")
	}
	if len(view.Config.Providers) == 0 {
		t.Fatal("test config should contain a provider")
	}
	view.Config.Providers[0].Weight = 7

	adapter := configCommandsAdapter{
		publisher:  publisher,
		configPath: d.config.ConfigPath,
	}
	result, err := adapter.UpdateConfig(view.Config, "change provider weight from UI")
	if err != nil {
		t.Fatalf("UpdateConfig() error = %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("UpdateConfig() result = %#v, want success", result)
	}

	reloaded, err := loadInitialRevision(configPath)
	if err != nil {
		t.Fatalf("loadInitialRevision() after update error = %v", err)
	}
	if len(reloaded.Config.Providers) == 0 || reloaded.Config.Providers[0].Weight != 7 {
		t.Fatalf("persisted provider weight = %d, want 7", reloaded.Config.Providers[0].Weight)
	}
}

func TestRenderAdminLoginPage(t *testing.T) {
	d := &Daemon{}
	rec := httptest.NewRecorder()
	d.renderAdminLoginPage(rec, http.StatusOK, "test message", "/admin")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "test message") {
		t.Fatal("page should contain error message")
	}
	if !strings.Contains(rec.Body.String(), "Admin Login") {
		t.Fatal("page should contain title")
	}
}

func TestRenderAdminLoginPageNoMessage(t *testing.T) {
	d := &Daemon{}
	rec := httptest.NewRecorder()
	d.renderAdminLoginPage(rec, http.StatusOK, "", "/admin")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	// Should not have error paragraph
	if strings.Contains(rec.Body.String(), "style=\"color:#b91c1c;\"") {
		t.Fatal("page should not contain error styling when no message")
	}
}

func TestAdminLoginPageHandlerAlreadyAuthenticated(t *testing.T) {
	configPath := writeAdminAuthoringConfig(t)
	comp := compiler.NewCompiler()
	d := &Daemon{
		config:    Config{ConfigPath: configPath, Listen: "127.0.0.1:18081"},
		compiler:  comp,
		publisher: newConfiguredPublisher(nil, comp, nil),
	}
	if err := d.seedInitialRevision(); err != nil {
		t.Fatalf("seedInitialRevision() error = %v", err)
	}

	handler := d.createHandler()

	// Create a valid session cookie
	authenticator, _ := d.currentAuthenticator()
	req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	rec := httptest.NewRecorder()
	if err := authenticator.Login(rec, strings.Repeat("a", 34)); err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	// Use the cookie in a new request
	req = httptest.NewRequest(http.MethodGet, "/admin/login?next=/admin/config", nil)
	req.AddCookie(rec.Result().Cookies()[0])
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
}

func TestAdminLoginPageHandlerParseFormError(t *testing.T) {
	configPath := writeAdminAuthoringConfig(t)
	comp := compiler.NewCompiler()
	d := &Daemon{
		config:    Config{ConfigPath: configPath, Listen: "127.0.0.1:18081"},
		compiler:  comp,
		publisher: newConfiguredPublisher(nil, comp, nil),
	}
	if err := d.seedInitialRevision(); err != nil {
		t.Fatalf("seedInitialRevision() error = %v", err)
	}

	handler := d.createHandler()
	// Invalid form body will cause ParseForm to fail
	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader("%zz"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Should still return a page (with error)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAdminLoginAPIHandlerDecodeError(t *testing.T) {
	configPath := writeAdminAuthoringConfig(t)
	comp := compiler.NewCompiler()
	d := &Daemon{
		config:    Config{ConfigPath: configPath, Listen: "127.0.0.1:18081"},
		compiler:  comp,
		publisher: newConfiguredPublisher(nil, comp, nil),
	}
	if err := d.seedInitialRevision(); err != nil {
		t.Fatalf("seedInitialRevision() error = %v", err)
	}

	handler := d.createHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
