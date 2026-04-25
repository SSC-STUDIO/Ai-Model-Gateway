package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"ai-model-gateway/internal/control/publish"
	"ai-model-gateway/internal/core"
)

func TestAdminFrontendServesSPAAndAssets(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{}, nil)

	tests := []struct {
		name        string
		path        string
		status      int
		contains    string
		contentType string
	}{
		{
			name:        "root shell",
			path:        "/admin",
			status:      http.StatusOK,
			contains:    `id="app"`,
			contentType: "text/html",
		},
		{
			name:        "deep link shell",
			path:        "/admin/history",
			status:      http.StatusOK,
			contains:    `id="app"`,
			contentType: "text/html",
		},
		{
			name:        "icon asset",
			path:        "/admin/icon.svg",
			status:      http.StatusOK,
			contains:    "<svg",
			contentType: "image/svg+xml",
		},
		{
			name:        "missing asset",
			path:        "/admin/assets/missing.js",
			status:      http.StatusNotFound,
			contains:    "404",
			contentType: "text/plain",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			if body := rec.Body.String(); !strings.Contains(body, tc.contains) {
				t.Fatalf("body does not contain %q: %s", tc.contains, body)
			}
			if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, tc.contentType) {
				t.Fatalf("content-type = %q, want substring %q", contentType, tc.contentType)
			}
		})
	}
}

func TestAdminFrontendEmbeddedIndexReferencesExistingAssets(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{}, nil)

	indexReq := httptest.NewRequest(http.MethodGet, "/admin", nil)
	indexRec := httptest.NewRecorder()
	mux.ServeHTTP(indexRec, indexReq)
	if indexRec.Code != http.StatusOK {
		t.Fatalf("GET /admin status = %d, want %d", indexRec.Code, http.StatusOK)
	}

	matches := regexp.MustCompile(`/admin/assets/[^"' )]+`).FindAllString(indexRec.Body.String(), -1)
	if len(matches) == 0 {
		t.Fatal("expected embedded admin index to reference hashed assets")
	}

	seen := make(map[string]struct{}, len(matches))
	for _, assetPath := range matches {
		if _, ok := seen[assetPath]; ok {
			continue
		}
		seen[assetPath] = struct{}{}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, assetPath, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want %d", assetPath, rec.Code, http.StatusOK)
		}
	}
}

func TestMountWrapsAdminRoutesWithMiddleware(t *testing.T) {
	mux := http.NewServeMux()
	var wrapped int
	Mount(mux, Deps{
		AdminMiddleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wrapped++
				w.Header().Set("X-Admin-Wrapped", "1")
				next.ServeHTTP(w, r)
			})
		},
	}, nil)

	for _, path := range []string{"/admin", "/api/admin/status"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Header().Get("X-Admin-Wrapped") != "1" {
			t.Fatalf("%s was not wrapped by admin middleware", path)
		}
	}

	if wrapped != 2 {
		t.Fatalf("wrapped count = %d, want 2", wrapped)
	}
}

func TestConfigRoutesUseSplitQueryAndCommandDeps(t *testing.T) {
	mux := http.NewServeMux()
	query := &stubConfigQuery{
		current: &publish.CurrentConfigView{
			Revision: &publish.RevisionInfo{
				RevisionID: "rev_test",
				CreatedAt:  time.Date(2026, time.April, 18, 16, 0, 0, 0, time.UTC),
				IsActive:   true,
			},
			Policy: publish.PublisherPolicy{PublishHistoryLimit: 64},
		},
		history: []publish.RevisionInfo{
			{RevisionID: "rev_test", CreatedAt: time.Date(2026, time.April, 18, 16, 0, 0, 0, time.UTC), IsActive: true},
		},
	}
	commands := &stubConfigCommands{
		publishResult: &publish.PublishResult{Success: true, RevisionID: "rev_test"},
	}
	Mount(mux, Deps{
		ConfigQuery:    query,
		ConfigCommands: commands,
	}, nil)

	configReq := httptest.NewRequest(http.MethodGet, "/api/admin/config", nil)
	configRec := httptest.NewRecorder()
	mux.ServeHTTP(configRec, configReq)
	if configRec.Code != http.StatusOK {
		t.Fatalf("GET /api/admin/config status = %d, want %d", configRec.Code, http.StatusOK)
	}
	var configResp publish.CurrentConfigView
	if err := json.Unmarshal(configRec.Body.Bytes(), &configResp); err != nil {
		t.Fatalf("decode config response: %v", err)
	}
	if configResp.Revision == nil || configResp.Revision.RevisionID != "rev_test" {
		t.Fatalf("config response revision = %#v, want rev_test", configResp.Revision)
	}
	if configResp.Policy.PublishHistoryLimit != 64 {
		t.Fatalf("config response policy = %#v, want publish_history_limit 64", configResp.Policy)
	}

	historyReq := httptest.NewRequest(http.MethodGet, "/api/admin/config/history", nil)
	historyRec := httptest.NewRecorder()
	mux.ServeHTTP(historyRec, historyReq)
	if historyRec.Code != http.StatusOK {
		t.Fatalf("GET /api/admin/config/history status = %d, want %d", historyRec.Code, http.StatusOK)
	}
	if query.historyCalls != 1 {
		t.Fatalf("GetHistory calls = %d, want 1", query.historyCalls)
	}

	publishReq := httptest.NewRequest(http.MethodPost, "/api/admin/config/publish", strings.NewReader(`{"revision_id":"rev_test"}`))
	publishReq.Header.Set("Content-Type", "application/json")
	publishRec := httptest.NewRecorder()
	mux.ServeHTTP(publishRec, publishReq)
	if publishRec.Code != http.StatusOK {
		t.Fatalf("POST /api/admin/config/publish status = %d, want %d", publishRec.Code, http.StatusOK)
	}
	if commands.publishCalls != 1 || commands.lastPublishRevisionID != "rev_test" {
		t.Fatalf("publish calls = %d revision = %q, want 1 and rev_test", commands.publishCalls, commands.lastPublishRevisionID)
	}

	reloadReq := httptest.NewRequest(http.MethodPost, "/api/admin/config/reload", nil)
	reloadRec := httptest.NewRecorder()
	mux.ServeHTTP(reloadRec, reloadReq)
	if reloadRec.Code != http.StatusOK {
		t.Fatalf("POST /api/admin/config/reload status = %d, want %d", reloadRec.Code, http.StatusOK)
	}
	if commands.reloadCalls != 1 {
		t.Fatalf("reload calls = %d, want 1", commands.reloadCalls)
	}
}

func TestConfigRouteRedactsConfigForViewerRole(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{
		ConfigQuery: &stubConfigQuery{
			current: &publish.CurrentConfigView{
				Revision: &publish.RevisionInfo{RevisionID: "rev_secret", IsActive: true},
				Policy:   publish.PublisherPolicy{PublishHistoryLimit: 16},
				Config: &core.Config{
					Admin: core.AdminConfig{
						BootstrapToken:   "bootstrap-secret",
						CookieSigningKey: "cookie-secret",
					},
				},
				RawYAML: "admin:\n  bootstrap_token: bootstrap-secret\n",
			},
		},
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/config", nil)
	req = WithAdminRole(req, "viewer")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/admin/config status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp publish.CurrentConfigView
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode config response: %v", err)
	}
	if resp.Config != nil {
		t.Fatalf("viewer config response leaked config: %#v", resp.Config)
	}
	if resp.RawYAML != "" {
		t.Fatalf("viewer raw_yaml = %q, want empty", resp.RawYAML)
	}
	if !strings.Contains(rec.Body.String(), "rev_secret") {
		t.Fatalf("viewer response missing revision metadata: %s", rec.Body.String())
	}
}

type stubConfigQuery struct {
	current      *publish.CurrentConfigView
	history      []publish.RevisionInfo
	historyCalls int
}

func (s *stubConfigQuery) GetCurrentConfigView() (*publish.CurrentConfigView, error) {
	return s.current, nil
}

func (s *stubConfigQuery) GetHistory(limit int) ([]publish.RevisionInfo, error) {
	s.historyCalls++
	return append([]publish.RevisionInfo(nil), s.history...), nil
}

type stubConfigCommands struct {
	publishResult          *publish.PublishResult
	reloadResult           *publish.PublishResult
	rollbackResult         *publish.PublishResult
	publishCalls           int
	reloadCalls            int
	rollbackCalls          int
	lastPublishRevisionID  string
	lastRollbackRevisionID string
}

func (s *stubConfigCommands) Publish(revisionID string) (*publish.PublishResult, error) {
	s.publishCalls++
	s.lastPublishRevisionID = revisionID
	if s.publishResult != nil {
		return s.publishResult, nil
	}
	return &publish.PublishResult{Success: true, RevisionID: revisionID}, nil
}

func (s *stubConfigCommands) Rollback(revisionID string) (*publish.PublishResult, error) {
	s.rollbackCalls++
	s.lastRollbackRevisionID = revisionID
	if s.rollbackResult != nil {
		return s.rollbackResult, nil
	}
	return &publish.PublishResult{Success: true, RevisionID: revisionID}, nil
}

func (s *stubConfigCommands) ReloadConfig() (*publish.PublishResult, error) {
	s.reloadCalls++
	if s.reloadResult != nil {
		return s.reloadResult, nil
	}
	return &publish.PublishResult{Success: true, RevisionID: "rev_reloaded"}, nil
}

func (s *stubConfigCommands) UpdateConfig(cfg interface{}, description string) (*publish.PublishResult, error) {
	return &publish.PublishResult{Success: true, RevisionID: "rev_updated"}, nil
}

func (s *stubConfigCommands) ValidateConfig(cfg interface{}) (*publish.ConfigValidationResult, error) {
	return &publish.ConfigValidationResult{Valid: true}, nil
}
