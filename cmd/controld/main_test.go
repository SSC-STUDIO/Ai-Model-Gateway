package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/control/compiler"
	"ai-model-gateway/internal/control/publish"
	"ai-model-gateway/internal/gateway/snapshot"

	"gopkg.in/yaml.v3"
)

func TestLoadConfigDefaultsToOperatorYAML(t *testing.T) {
	cfg := loadConfig("", "", "", "", "", "")

	if got, want := filepath.ToSlash(cfg.ConfigPath), "configs/config.yaml"; got != want {
		t.Fatalf("ConfigPath = %q, want %q", got, want)
	}
	if cfg.Update.Repository == "" || cfg.Update.StateDir == "" {
		t.Fatalf("Update defaults not set: %#v", cfg.Update)
	}
}

func TestLoadConfigAllowsDirectFlagOverrides(t *testing.T) {
	cfg := loadConfig("", "127.0.0.1:19081", "gateway.sock", "telemetry.sock", "data/custom-control", "configs/custom.yaml")

	if cfg.Listen != "127.0.0.1:19081" {
		t.Fatalf("Listen = %q, want %q", cfg.Listen, "127.0.0.1:19081")
	}
	if cfg.GatewaySocket != "gateway.sock" {
		t.Fatalf("GatewaySocket = %q, want %q", cfg.GatewaySocket, "gateway.sock")
	}
	if cfg.TelemetrySocket != "telemetry.sock" {
		t.Fatalf("TelemetrySocket = %q, want %q", cfg.TelemetrySocket, "telemetry.sock")
	}
	if got, want := filepath.ToSlash(cfg.DataDir), "data/custom-control"; got != want {
		t.Fatalf("DataDir = %q, want %q", got, want)
	}
	if got, want := filepath.ToSlash(cfg.ConfigPath), "configs/custom.yaml"; got != want {
		t.Fatalf("ConfigPath = %q, want %q", got, want)
	}
}

func TestLoadInitialRevisionIsDeterministicAndSeedPublishesCompiledConfig(t *testing.T) {
	configPath := writeTestAuthoringConfig(t)

	first, err := loadInitialRevision(configPath)
	if err != nil {
		t.Fatalf("loadInitialRevision(first) error = %v", err)
	}
	second, err := loadInitialRevision(configPath)
	if err != nil {
		t.Fatalf("loadInitialRevision(second) error = %v", err)
	}
	if first.RevisionID != second.RevisionID {
		t.Fatalf("RevisionID mismatch: first=%q second=%q", first.RevisionID, second.RevisionID)
	}

	gateway := &stubGatewayRPC{}
	comp := compiler.NewCompiler()
	d := &Daemon{
		config:    Config{ConfigPath: configPath},
		compiler:  comp,
		publisher: newConfiguredPublisher(gateway, comp, nil),
	}
	if err := d.seedInitialRevision(); err != nil {
		t.Fatalf("seedInitialRevision() error = %v", err)
	}

	current, err := d.publisher.GetCurrentRevision()
	if err != nil {
		t.Fatalf("GetCurrentRevision() error = %v", err)
	}
	if current == nil || !current.IsActive {
		t.Fatalf("GetCurrentRevision() = %#v, want active revision", current)
	}
	if current.RevisionID != first.RevisionID {
		t.Fatalf("GetCurrentRevision().RevisionID = %q, want %q", current.RevisionID, first.RevisionID)
	}

	result, err := d.publisher.Publish(current.RevisionID)
	if err != nil {
		t.Fatalf("Publish(%q) error = %v", current.RevisionID, err)
	}
	if result == nil || !result.Success {
		t.Fatalf("Publish(%q) = %#v, want success", current.RevisionID, result)
	}
	if len(gateway.requests) != 1 {
		t.Fatalf("len(requests) = %d, want 1", len(gateway.requests))
	}

	req := gateway.requests[0]
	if req.RevisionID != current.RevisionID {
		t.Fatalf("ApplySnapshotRequest.RevisionID = %q, want %q", req.RevisionID, current.RevisionID)
	}

	var published snapshot.Snapshot
	if err := yaml.Unmarshal(req.SnapshotBytes, &published); err != nil {
		t.Fatalf("yaml.Unmarshal(snapshot) error = %v", err)
	}
	if published.Ingress.Listen != "127.0.0.1:19090" {
		t.Fatalf("snapshot.Ingress.Listen = %q, want %q", published.Ingress.Listen, "127.0.0.1:19090")
	}
	if published.Meta.RevisionID != current.RevisionID {
		t.Fatalf("snapshot.Meta.RevisionID = %q, want %q", published.Meta.RevisionID, current.RevisionID)
	}
}

func TestCreateHandlerKeepsConfigEndpointWorkingWithoutTelemetry(t *testing.T) {
	configPath := writeTestAuthoringConfig(t)

	comp := compiler.NewCompiler()
	d := &Daemon{
		config:    Config{ConfigPath: configPath},
		compiler:  comp,
		publisher: newConfiguredPublisher(nil, comp, nil),
		startedAt: time.Unix(1_710_000_000, 0),
	}
	if err := d.seedInitialRevision(); err != nil {
		t.Fatalf("seedInitialRevision() error = %v", err)
	}

	handler := d.createHandler()

	configRR := httptest.NewRecorder()
	handler.ServeHTTP(configRR, httptest.NewRequest(http.MethodGet, "/api/admin/config", nil))
	if configRR.Code != http.StatusOK {
		t.Fatalf("GET /api/admin/config status = %d, want %d", configRR.Code, http.StatusOK)
	}

	var current struct {
		Revision struct {
			RevisionID string `json:"revision_id"`
			IsActive   bool   `json:"is_active"`
		} `json:"revision"`
		Policy struct {
			PublishHistoryLimit int `json:"publish_history_limit"`
		} `json:"policy"`
	}
	if err := json.Unmarshal(configRR.Body.Bytes(), &current); err != nil {
		t.Fatalf("decode /api/admin/config error = %v", err)
	}
	if current.Revision.RevisionID == "" || !current.Revision.IsActive {
		t.Fatalf("/api/admin/config = %#v, want active revision", current)
	}
	if current.Policy.PublishHistoryLimit != 256 {
		t.Fatalf("/api/admin/config policy.publish_history_limit = %d, want 256", current.Policy.PublishHistoryLimit)
	}

	overviewRR := httptest.NewRecorder()
	handler.ServeHTTP(overviewRR, httptest.NewRequest(http.MethodGet, "/api/admin/overview", nil))
	if overviewRR.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /api/admin/overview status = %d, want %d", overviewRR.Code, http.StatusServiceUnavailable)
	}
}

func TestRestoreOrSeedInitialRevisionPrefersPersistedPublisherState(t *testing.T) {
	configPath := writeTestAuthoringConfig(t)
	dataDir := t.TempDir()
	stateStore := publish.NewSQLiteStateStore(filepath.Join(dataDir, "publisher-state.db"))

	comp := compiler.NewCompiler()
	persisted := newConfiguredPublisher(nil, comp, stateStore)
	initial, err := loadInitialRevision(configPath)
	if err != nil {
		t.Fatalf("loadInitialRevision() error = %v", err)
	}
	restoredConfig := *initial.Config
	restoredConfig.Server.Listen = "127.0.0.1:29090"
	restoredRevision := publish.Revision{
		RevisionID:  "rev_persisted",
		CreatedAt:   initial.CreatedAt.Add(time.Minute),
		CreatedBy:   "system",
		Description: "persisted revision",
		Config:      &restoredConfig,
	}
	if err := persisted.ReplaceRevisions([]publish.Revision{initial, restoredRevision}, restoredRevision.RevisionID); err != nil {
		t.Fatalf("ReplaceRevisions() error = %v", err)
	}

	gateway := &stubGatewayRPC{}
	d := &Daemon{
		config:    Config{ConfigPath: configPath, DataDir: dataDir},
		compiler:  comp,
		publisher: newConfiguredPublisher(gateway, comp, (&Daemon{config: Config{DataDir: dataDir}}).publisherStateStore()),
	}
	if err := d.restoreOrSeedInitialRevision(); err != nil {
		t.Fatalf("restoreOrSeedInitialRevision() error = %v", err)
	}

	current, err := d.publisher.GetCurrentRevision()
	if err != nil {
		t.Fatalf("GetCurrentRevision() error = %v", err)
	}
	if current == nil || current.RevisionID != restoredRevision.RevisionID {
		t.Fatalf("GetCurrentRevision() = %#v, want %q", current, restoredRevision.RevisionID)
	}
	result, err := d.publisher.Publish(current.RevisionID)
	if err != nil {
		t.Fatalf("Publish(%q) error = %v", current.RevisionID, err)
	}
	if result == nil || !result.Success {
		t.Fatalf("Publish(%q) = %#v, want success", current.RevisionID, result)
	}
	if len(gateway.requests) != 1 || gateway.requests[0].RevisionID != restoredRevision.RevisionID {
		t.Fatalf("published revision = %#v, want %q", gateway.requests, restoredRevision.RevisionID)
	}
}

func TestRestoreOrSeedInitialRevisionMigratesLegacyJSONState(t *testing.T) {
	configPath := writeTestAuthoringConfig(t)
	dataDir := t.TempDir()
	legacyStore := publish.NewFileStateStore(filepath.Join(dataDir, "publisher-state.json"))

	comp := compiler.NewCompiler()
	persisted := newConfiguredPublisher(nil, comp, legacyStore)
	initial, err := loadInitialRevision(configPath)
	if err != nil {
		t.Fatalf("loadInitialRevision() error = %v", err)
	}
	restoredConfig := *initial.Config
	restoredConfig.Server.Listen = "127.0.0.1:28080"
	restoredRevision := publish.Revision{
		RevisionID:  "rev_legacy_json",
		CreatedAt:   initial.CreatedAt.Add(2 * time.Minute),
		CreatedBy:   "system",
		Description: "legacy json revision",
		Config:      &restoredConfig,
	}
	if err := persisted.ReplaceRevisions([]publish.Revision{initial, restoredRevision}, restoredRevision.RevisionID); err != nil {
		t.Fatalf("ReplaceRevisions() error = %v", err)
	}

	d := &Daemon{
		config:    Config{ConfigPath: configPath, DataDir: dataDir},
		compiler:  comp,
		publisher: newConfiguredPublisher(nil, comp, (&Daemon{config: Config{DataDir: dataDir}}).publisherStateStore()),
	}
	if err := d.restoreOrSeedInitialRevision(); err != nil {
		t.Fatalf("restoreOrSeedInitialRevision() error = %v", err)
	}

	current, err := d.publisher.GetCurrentRevision()
	if err != nil {
		t.Fatalf("GetCurrentRevision() error = %v", err)
	}
	if current == nil || current.RevisionID != restoredRevision.RevisionID {
		t.Fatalf("GetCurrentRevision() = %#v, want %q", current, restoredRevision.RevisionID)
	}

	sqliteState, err := publish.NewSQLiteStateStore(filepath.Join(dataDir, "publisher-state.db")).Load()
	if err != nil {
		t.Fatalf("SQLiteStateStore.Load() error = %v", err)
	}
	if sqliteState == nil || sqliteState.ActiveRevisionID != restoredRevision.RevisionID {
		t.Fatalf("sqliteState = %#v, want migrated active %q", sqliteState, restoredRevision.RevisionID)
	}
}

func TestCreateHandlerRequiresAdminAuthAndSupportsLoginForm(t *testing.T) {
	configPath := writeAdminAuthoringConfig(t)

	comp := compiler.NewCompiler()
	d := &Daemon{
		config: Config{
			ConfigPath: configPath,
			Listen:     "127.0.0.1:18081",
		},
		compiler:  comp,
		publisher: newConfiguredPublisher(nil, comp, nil),
		startedAt: time.Unix(1_710_000_000, 0),
	}
	if err := d.seedInitialRevision(); err != nil {
		t.Fatalf("seedInitialRevision() error = %v", err)
	}

	handler := d.createHandler()

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/admin/config", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/admin/config without auth = %d, want %d", unauthenticated.Code, http.StatusUnauthorized)
	}

	adminShell := httptest.NewRecorder()
	handler.ServeHTTP(adminShell, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if adminShell.Code != http.StatusOK {
		t.Fatalf("GET /admin without auth = %d, want %d", adminShell.Code, http.StatusOK)
	}
	if !strings.Contains(adminShell.Body.String(), `<div id="app"></div>`) {
		t.Fatalf("GET /admin body missing embedded shell: %s", adminShell.Body.String())
	}

	bearerReq := httptest.NewRequest(http.MethodGet, "/api/admin/config", nil)
	bearerReq.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 34))
	bearer := httptest.NewRecorder()
	handler.ServeHTTP(bearer, bearerReq)
	if bearer.Code != http.StatusOK {
		t.Fatalf("GET /api/admin/config with bearer auth = %d, want %d", bearer.Code, http.StatusOK)
	}

	viewerReq := httptest.NewRequest(http.MethodGet, "/api/admin/config", nil)
	viewerReq.Header.Set("Authorization", "Bearer "+strings.Repeat("c", 34))
	viewer := httptest.NewRecorder()
	handler.ServeHTTP(viewer, viewerReq)
	if viewer.Code != http.StatusOK {
		t.Fatalf("GET /api/admin/config with viewer auth = %d, want %d", viewer.Code, http.StatusOK)
	}
	if strings.Contains(viewer.Body.String(), "bootstrap_token") || strings.Contains(viewer.Body.String(), "cookie_signing_key") || strings.Contains(viewer.Body.String(), "test-key") {
		t.Fatalf("viewer config response leaked secrets: %s", viewer.Body.String())
	}
	if strings.Contains(viewer.Body.String(), "raw_yaml") || strings.Contains(viewer.Body.String(), `"config"`) {
		t.Fatalf("viewer config response should omit config payloads: %s", viewer.Body.String())
	}

	if !strings.Contains(bearer.Body.String(), "bootstrap_token") || !strings.Contains(bearer.Body.String(), "test-key") {
		t.Fatalf("admin config response should include editable config payload: %s", bearer.Body.String())
	}

	viewerWriteReq := httptest.NewRequest(http.MethodPost, "/api/admin/config/publish", strings.NewReader(`{"revision_id":"rev"}`))
	viewerWriteReq.Header.Set("Authorization", "Bearer "+strings.Repeat("c", 34))
	viewerWriteReq.Header.Set("Content-Type", "application/json")
	viewerWrite := httptest.NewRecorder()
	handler.ServeHTTP(viewerWrite, viewerWriteReq)
	if viewerWrite.Code != http.StatusForbidden {
		t.Fatalf("POST /api/admin/config/publish with viewer auth = %d, want %d", viewerWrite.Code, http.StatusForbidden)
	}

	// Test login via POST /api/admin/login (the only supported method now)
	loginBody := strings.NewReader(`{"token":"` + strings.Repeat("a", 34) + `"}`)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/admin/login", loginBody)
	loginReq.Header.Set("Content-Type", "application/json")
	loginRR := httptest.NewRecorder()
	handler.ServeHTTP(loginRR, loginReq)
	if loginRR.Code != http.StatusOK {
		t.Fatalf("POST /api/admin/login status = %d, want %d, body: %s", loginRR.Code, http.StatusOK, loginRR.Body.String())
	}
	cookies := loginRR.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie after admin login, got %d", len(cookies))
	}
	if cookies[0].Secure {
		t.Fatalf("expected loopback admin cookie to omit Secure over local HTTP")
	}

	cookieReq := httptest.NewRequest(http.MethodGet, "/api/admin/config", nil)
	cookieReq.AddCookie(cookies[0])
	cookieRR := httptest.NewRecorder()
	handler.ServeHTTP(cookieRR, cookieReq)
	if cookieRR.Code != http.StatusOK {
		t.Fatalf("GET /api/admin/config with cookie auth = %d, want %d", cookieRR.Code, http.StatusOK)
	}

	sessionReq := httptest.NewRequest(http.MethodGet, "/api/admin/session", nil)
	sessionReq.AddCookie(cookies[0])
	sessionRR := httptest.NewRecorder()
	handler.ServeHTTP(sessionRR, sessionReq)
	if sessionRR.Code != http.StatusOK {
		t.Fatalf("GET /api/admin/session with cookie auth = %d, want %d", sessionRR.Code, http.StatusOK)
	}
	if !strings.Contains(sessionRR.Body.String(), `"authenticated":true`) {
		t.Fatalf("unexpected session body: %s", sessionRR.Body.String())
	}
}

func TestSameOriginWriteProtection(t *testing.T) {
	configPath := writeAdminAuthoringConfig(t)

	comp := compiler.NewCompiler()
	d := &Daemon{
		config: Config{
			ConfigPath: configPath,
			Listen:     "127.0.0.1:18081",
		},
		compiler:  comp,
		publisher: newConfiguredPublisher(nil, comp, nil),
		startedAt: time.Unix(1_710_000_000, 0),
	}
	if err := d.seedInitialRevision(); err != nil {
		t.Fatalf("seedInitialRevision() error = %v", err)
	}

	handler := d.createHandler()

	// Login first to get a cookie
	loginBody := strings.NewReader(`{"token":"` + strings.Repeat("a", 34) + `"}`)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/admin/login", loginBody)
	loginReq.Header.Set("Content-Type", "application/json")
	loginRR := httptest.NewRecorder()
	handler.ServeHTTP(loginRR, loginReq)
	if loginRR.Code != http.StatusOK {
		t.Fatalf("login failed: %d", loginRR.Code)
	}
	cookies := loginRR.Result().Cookies()

	// Write request without Origin/Referer should be rejected
	writeReq := httptest.NewRequest(http.MethodPost, "/api/admin/config/publish", strings.NewReader(`{"revision_id":"rev-001"}`))
	writeReq.AddCookie(cookies[0])
	writeReq.Header.Set("Content-Type", "application/json")
	writeRR := httptest.NewRecorder()
	handler.ServeHTTP(writeRR, writeReq)
	if writeRR.Code != http.StatusForbidden {
		t.Fatalf("POST /api/admin/config/publish without origin = %d, want %d", writeRR.Code, http.StatusForbidden)
	}

	// Write request with valid Origin should succeed (even if publish fails for other reasons)
	writeReq2 := httptest.NewRequest(http.MethodPost, "/api/admin/config/publish", strings.NewReader(`{"revision_id":"rev-001"}`))
	writeReq2.Host = "127.0.0.1:18081"
	writeReq2.AddCookie(cookies[0])
	writeReq2.Header.Set("Content-Type", "application/json")
	writeReq2.Header.Set("Origin", "http://127.0.0.1:18081")
	writeRR2 := httptest.NewRecorder()
	handler.ServeHTTP(writeRR2, writeReq2)
	// Should not be same-origin error (might fail for other reasons like missing revision)
	if strings.Contains(writeRR2.Body.String(), "same-origin") {
		t.Fatalf("POST with valid origin should not fail same-origin check: %s", writeRR2.Body.String())
	}

	writeReq3 := httptest.NewRequest(http.MethodPost, "/api/admin/config/publish", strings.NewReader(`{"revision_id":"rev-001"}`))
	writeReq3.Host = "localhost:18081"
	writeReq3.AddCookie(cookies[0])
	writeReq3.Header.Set("Content-Type", "application/json")
	writeReq3.Header.Set("Origin", "http://localhost:18081")
	writeRR3 := httptest.NewRecorder()
	handler.ServeHTTP(writeRR3, writeReq3)
	if strings.Contains(writeRR3.Body.String(), "same-origin") {
		t.Fatalf("POST with localhost origin should not fail same-origin check: %s", writeRR3.Body.String())
	}

	writeReq4 := httptest.NewRequest(http.MethodPost, "/api/admin/config/publish", strings.NewReader(`{"revision_id":"rev-001"}`))
	writeReq4.Host = "127.0.0.1:18081"
	writeReq4.AddCookie(cookies[0])
	writeReq4.Header.Set("Content-Type", "application/json")
	writeReq4.Header.Set("X-Forwarded-Host", "admin.example.com")
	writeReq4.Header.Set("X-Forwarded-Proto", "https")
	writeReq4.Header.Set("Origin", "https://admin.example.com")
	writeRR4 := httptest.NewRecorder()
	handler.ServeHTTP(writeRR4, writeReq4)
	if strings.Contains(writeRR4.Body.String(), "same-origin") {
		t.Fatalf("POST with forwarded public origin should not fail same-origin check: %s", writeRR4.Body.String())
	}

	// Bearer token should bypass same-origin check
	bearerReq := httptest.NewRequest(http.MethodPost, "/api/admin/config/publish", strings.NewReader(`{"revision_id":"rev-001"}`))
	bearerReq.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 34))
	bearerReq.Header.Set("Content-Type", "application/json")
	bearerRR := httptest.NewRecorder()
	handler.ServeHTTP(bearerRR, bearerReq)
	// Bearer token requests don't need same-origin check
	if strings.Contains(bearerRR.Body.String(), "same-origin") {
		t.Fatalf("Bearer token should bypass same-origin check: %s", bearerRR.Body.String())
	}

	for _, tc := range []struct {
		name string
		path string
		body string
	}{
		{
			name: "pricing refresh",
			path: "/api/admin/pricing/refresh",
			body: `{}`,
		},
		{
			name: "update check",
			path: "/api/admin/update/check",
			body: `{}`,
		},
		{
			name: "benchmark baseline import",
			path: "/api/admin/benchmark/baselines/import",
			body: `{"kind":"public_standard","source_name":"test","file_name":"baseline.json","contents":"[{\"canonical_model_id\":\"m\",\"metric_name\":\"overall\",\"score\":90,\"scale_max\":100}]"}`,
		},
		{
			name: "benchmark run",
			path: "/api/admin/benchmark/runs",
			body: `{"provider_id":"test-provider","public_model":"gpt-test","public_snapshot_id":"baseline_test"}`,
		},
	} {
		req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
		req.AddCookie(cookies[0])
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s without origin = %d, want %d", tc.name, rec.Code, http.StatusForbidden)
		}

		req = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
		req.Host = "127.0.0.1:18081"
		req.AddCookie(cookies[0])
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://127.0.0.1:18081")
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if strings.Contains(rec.Body.String(), "same-origin") {
			t.Fatalf("%s with valid origin should not fail same-origin check: %s", tc.name, rec.Body.String())
		}
	}
}

type stubGatewayRPC struct {
	requests []gatewaycontrol.ApplySnapshotRequest
}

func (s *stubGatewayRPC) ApplySnapshot(req gatewaycontrol.ApplySnapshotRequest) (*gatewaycontrol.ApplySnapshotResponse, error) {
	s.requests = append(s.requests, req)
	return &gatewaycontrol.ApplySnapshotResponse{Applied: true}, nil
}

func (s *stubGatewayRPC) GetStatus() (*gatewaycontrol.GetStatusResponse, error) {
	return &gatewaycontrol.GetStatusResponse{}, nil
}

func writeTestAuthoringConfig(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte(`server:
  listen: 127.0.0.1:19090
providers:
  - name: test-provider
    base_url: https://example.invalid/v1
    api_key: test-key
    models:
      - gpt-test
`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	return path
}

func writeAdminAuthoringConfig(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte(`admin:
  enabled: true
  bootstrap_token: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  cookie_signing_key: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
  tokens:
    - name: viewer
      token: "cccccccccccccccccccccccccccccccccc"
      role: viewer
server:
  listen: 127.0.0.1:19090
providers:
  - name: test-provider
    base_url: https://example.invalid/v1
    api_key: test-key
    models:
      - gpt-test
`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	return path
}

func TestReloadWatchedConfigPublishesUpdatedRevision(t *testing.T) {
	configPath := writeTestAuthoringConfig(t)
	gateway := &stubGatewayRPC{}
	comp := compiler.NewCompiler()
	d := &Daemon{
		config: Config{
			ConfigPath: configPath,
			DataDir:    t.TempDir(),
		},
		gatewayRPC: &GatewayClient{},
		compiler:   comp,
		publisher:  newConfiguredPublisher(gateway, comp, nil),
	}
	if err := d.seedInitialRevision(); err != nil {
		t.Fatalf("seedInitialRevision() error = %v", err)
	}

	updated := []byte(`server:
  listen: 127.0.0.1:29090
providers:
  - name: test-provider
    base_url: https://example.invalid/v1
    api_key: test-key-updated
    models:
      - gpt-test
`)
	if err := os.WriteFile(configPath, updated, 0600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", configPath, err)
	}

	if err := d.reloadWatchedConfig(); err != nil {
		t.Fatalf("reloadWatchedConfig() error = %v", err)
	}
	if len(gateway.requests) != 1 {
		t.Fatalf("len(requests) = %d, want 1", len(gateway.requests))
	}

	var published snapshot.Snapshot
	if err := yaml.Unmarshal(gateway.requests[0].SnapshotBytes, &published); err != nil {
		t.Fatalf("yaml.Unmarshal(snapshot) error = %v", err)
	}
	if published.Ingress.Listen != "127.0.0.1:29090" {
		t.Fatalf("snapshot.Ingress.Listen = %q, want %q", published.Ingress.Listen, "127.0.0.1:29090")
	}

	current, err := d.publisher.GetCurrentConfig()
	if err != nil {
		t.Fatalf("GetCurrentConfig() error = %v", err)
	}
	if current == nil || current.Server.Listen != "127.0.0.1:29090" {
		t.Fatalf("GetCurrentConfig() = %#v, want listen 127.0.0.1:29090", current)
	}

	history, err := d.publisher.GetHistory(10)
	if err != nil {
		t.Fatalf("GetHistory() error = %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("len(GetHistory()) = %d, want 2", len(history))
	}
	if history[0].RevisionID == history[1].RevisionID {
		t.Fatalf("history = %#v, want distinct revisions after reload", history)
	}
}
