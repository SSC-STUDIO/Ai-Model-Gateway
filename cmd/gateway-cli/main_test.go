package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseConfigDiffArgsWithFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  listen: :18080\nproviders: []\n"), 0644); err != nil {
		t.Fatal(err)
	}
	req, err := parseConfigDiffArgs([]string{"--from", "rev_a", "--file", path})
	if err != nil {
		t.Fatalf("parseConfigDiffArgs() error = %v", err)
	}
	if req["from_revision_id"] != "rev_a" {
		t.Fatalf("from_revision_id = %#v", req["from_revision_id"])
	}
	if req["config"] == nil {
		t.Fatal("config payload is nil")
	}
}

func TestParseConfigDiffArgsRequiresTarget(t *testing.T) {
	if _, err := parseConfigDiffArgs([]string{"--from", "rev_a"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestAuditCommandUsesPositionalLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/audit" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("limit"); got != "7" {
			t.Fatalf("limit = %q, want 7", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"events": []any{}, "count": 0})
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	code := runCLI([]string{"gateway-cli", "-server", server.URL, "audit", "7"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runCLI(audit) code = %d stderr=%s", code, stderr.String())
	}
}

func TestGenericCommandsUseTextOutput(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		method     string
		pathPrefix string
		response   map[string]any
		want       string
	}{
		{
			name:       "runtime status",
			args:       []string{"runtime", "status"},
			method:     http.MethodGet,
			pathPrefix: "/api/admin/runtime/status",
			response:   map[string]any{"status": "ok", "bundle_version": "1.3.0"},
			want:       "status: ok",
		},
		{
			name:       "runtime preflight",
			args:       []string{"runtime", "preflight"},
			method:     http.MethodPost,
			pathPrefix: "/api/admin/runtime/preflight",
			response:   map[string]any{"ok": true, "checks": []any{}},
			want:       "ok: true",
		},
		{
			name:       "audit",
			args:       []string{"audit", "3"},
			method:     http.MethodGet,
			pathPrefix: "/api/admin/audit",
			response:   map[string]any{"count": 0, "events": []any{}},
			want:       "count: 0",
		},
		{
			name:       "probe provider",
			args:       []string{"probe", "provider", "station-a"},
			method:     http.MethodPost,
			pathPrefix: "/api/admin/probe/provider",
			response:   map[string]any{"provider_id": "station-a", "healthy": true},
			want:       "healthy: true",
		},
		{
			name:       "diagnostics",
			args:       []string{"diagnostics"},
			method:     http.MethodGet,
			pathPrefix: "/api/admin/diagnostics",
			response:   map[string]any{"redacted": true, "items": []any{"runtime"}},
			want:       "redacted: true",
		},
		{
			name:       "secrets",
			args:       []string{"secrets", "check"},
			method:     http.MethodGet,
			pathPrefix: "/api/admin/secrets/status",
			response:   map[string]any{"ok": true, "missing": []any{}},
			want:       "ok: true",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.method {
					t.Fatalf("method = %s, want %s", r.Method, tc.method)
				}
				if !strings.HasPrefix(r.URL.Path, tc.pathPrefix) {
					t.Fatalf("path = %s, want prefix %s", r.URL.Path, tc.pathPrefix)
				}
				_ = json.NewEncoder(w).Encode(tc.response)
			}))
			defer server.Close()

			args := append([]string{"gateway-cli", "-server", server.URL, "-format", "text"}, tc.args...)
			var stdout, stderr bytes.Buffer
			code := runCLI(args, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("runCLI(%s) code = %d stderr=%s", tc.name, code, stderr.String())
			}
			out := strings.TrimSpace(stdout.String())
			if strings.HasPrefix(out, "{") {
				t.Fatalf("text output looks like JSON:\n%s", out)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("expected output to contain %q, got:\n%s", tc.want, stdout.String())
			}
		})
	}
}

func TestGenericCommandsUseJSONOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/runtime/status" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	code := runCLI([]string{"gateway-cli", "-server", server.URL, "-format", "json", "runtime", "status"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runCLI(runtime status json) code = %d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode json output: %v\n%s", err, stdout.String())
	}
	if payload["status"] != "ok" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestInvalidFormatRejected(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCLI([]string{"gateway-cli", "-format", "xml", "runtime", "status"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("runCLI invalid format code = 0 stdout=%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unsupported format: xml") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestCSVFormatScope(t *testing.T) {
	if err := validateCLIFormat("csv", []string{"benchmark", "telemetry", "run-1"}); err != nil {
		t.Fatalf("benchmark telemetry csv rejected: %v", err)
	}
	if err := validateCLIFormat("csv", []string{"runtime", "status"}); err == nil {
		t.Fatal("runtime csv accepted, want error")
	}
}

func TestParseBenchmarkTelemetryArgsDefaults(t *testing.T) {
	runID, query, err := parseBenchmarkTelemetryArgs([]string{"run-1"})
	if err != nil {
		t.Fatalf("parseBenchmarkTelemetryArgs returned error: %v", err)
	}
	if runID != "run-1" {
		t.Fatalf("expected run id run-1, got %q", runID)
	}
	if query.WindowHours != 24 {
		t.Fatalf("expected default window hours 24, got %d", query.WindowHours)
	}
	if query.Limit != 200 {
		t.Fatalf("expected default limit 200, got %d", query.Limit)
	}
	if query.Offset != 0 {
		t.Fatalf("expected default offset 0, got %d", query.Offset)
	}
	if query.CaseID != "" {
		t.Fatalf("expected empty case id, got %q", query.CaseID)
	}
	if len(query.Providers) != 0 {
		t.Fatalf("expected no providers, got %v", query.Providers)
	}
	if len(query.Models) != 0 {
		t.Fatalf("expected no models, got %v", query.Models)
	}
	if query.TargetID != "" {
		t.Fatalf("expected no target id, got %q", query.TargetID)
	}
}

func TestParseBenchmarkTelemetryArgsWithFilters(t *testing.T) {
	runID, query, err := parseBenchmarkTelemetryArgs([]string{
		"run-2",
		"--case", "json_schema",
		"--target", "target-2",
		"--provider", "provider-a",
		"--model", "gpt-4o",
		"--hours", "12",
		"--limit", "50",
		"--offset", "10",
	})
	if err != nil {
		t.Fatalf("parseBenchmarkTelemetryArgs returned error: %v", err)
	}
	if runID != "run-2" {
		t.Fatalf("expected run id run-2, got %q", runID)
	}
	if query.CaseID != "json_schema" {
		t.Fatalf("expected case id json_schema, got %q", query.CaseID)
	}
	if query.TargetID != "target-2" {
		t.Fatalf("expected target id target-2, got %q", query.TargetID)
	}
	if query.WindowHours != 12 {
		t.Fatalf("expected 12 window hours, got %d", query.WindowHours)
	}
	if query.Limit != 50 {
		t.Fatalf("expected limit 50, got %d", query.Limit)
	}
	if query.Offset != 10 {
		t.Fatalf("expected offset 10, got %d", query.Offset)
	}
	if len(query.Providers) != 1 || query.Providers[0] != "provider-a" {
		t.Fatalf("unexpected providers: %v", query.Providers)
	}
	if len(query.Models) != 1 || query.Models[0] != "gpt-4o" {
		t.Fatalf("unexpected models: %v", query.Models)
	}
}

func TestParseBenchmarkTelemetryArgsRequiresRunID(t *testing.T) {
	if _, _, err := parseBenchmarkTelemetryArgs(nil); err == nil {
		t.Fatal("expected error for missing run id")
	}
}

func TestParseBenchmarkTargetSummaryArgsDefaults(t *testing.T) {
	runID, query, sortMode, err := parseBenchmarkTargetSummaryArgs([]string{"run-3"})
	if err != nil {
		t.Fatalf("parseBenchmarkTargetSummaryArgs returned error: %v", err)
	}
	if runID != "run-3" {
		t.Fatalf("expected run id run-3, got %q", runID)
	}
	if sortMode != "severity" {
		t.Fatalf("expected default sort severity, got %q", sortMode)
	}
	if query.WindowHours != 24 || query.Limit != 200 || query.Offset != 0 {
		t.Fatalf("unexpected default query values: %+v", query)
	}
}

func TestParseBenchmarkTargetSummaryArgsWithSort(t *testing.T) {
	runID, query, sortMode, err := parseBenchmarkTargetSummaryArgs([]string{
		"run-4",
		"--target", "target-4",
		"--provider", "provider-z",
		"--model", "gpt-5",
		"--sort", "latency",
		"--hours", "6",
		"--limit", "20",
		"--offset", "5",
	})
	if err != nil {
		t.Fatalf("parseBenchmarkTargetSummaryArgs returned error: %v", err)
	}
	if runID != "run-4" {
		t.Fatalf("expected run id run-4, got %q", runID)
	}
	if sortMode != "latency" {
		t.Fatalf("expected sort mode latency, got %q", sortMode)
	}
	if query.TargetID != "target-4" {
		t.Fatalf("expected target id target-4, got %q", query.TargetID)
	}
	if query.WindowHours != 6 || query.Limit != 20 || query.Offset != 5 {
		t.Fatalf("unexpected query values: %+v", query)
	}
	if len(query.Providers) != 1 || query.Providers[0] != "provider-z" {
		t.Fatalf("unexpected providers: %v", query.Providers)
	}
	if len(query.Models) != 1 || query.Models[0] != "gpt-5" {
		t.Fatalf("unexpected models: %v", query.Models)
	}
}

func TestParseBenchmarkTargetSummaryArgsSupportsProviderAndCostSort(t *testing.T) {
	for _, sortMode := range []string{"provider", "cost"} {
		t.Run(sortMode, func(t *testing.T) {
			runID, _, gotSort, err := parseBenchmarkTargetSummaryArgs([]string{"run-sort", "--sort", sortMode})
			if err != nil {
				t.Fatalf("parseBenchmarkTargetSummaryArgs returned error: %v", err)
			}
			if runID != "run-sort" {
				t.Fatalf("expected run id run-sort, got %q", runID)
			}
			if gotSort != sortMode {
				t.Fatalf("expected sort mode %q, got %q", sortMode, gotSort)
			}
		})
	}
}

func TestParseBenchmarkTargetSummaryArgsRejectsUnknownSort(t *testing.T) {
	if _, _, _, err := parseBenchmarkTargetSummaryArgs([]string{"run-5", "--sort", "weird"}); err == nil {
		t.Fatal("expected error for unsupported sort mode")
	}
}
