package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/control/audit"
	"ai-model-gateway/internal/control/publish"
	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/gateway/snapshot"
	"ai-model-gateway/internal/infra/logger"
	"ai-model-gateway/internal/version"
)

// RuntimeConfig describes local runtime paths exposed for diagnostics.
type RuntimeConfig struct {
	BundleVersion   string            `json:"bundle_version,omitempty"`
	BundleManifest  string            `json:"bundle_manifest,omitempty"`
	ConfigPath      string            `json:"config_path,omitempty"`
	DataDir         string            `json:"data_dir,omitempty"`
	Listen          string            `json:"listen,omitempty"`
	GatewaySocket   string            `json:"gateway_socket,omitempty"`
	TelemetrySocket string            `json:"telemetry_socket,omitempty"`
	ConfigPaths     map[string]string `json:"config_paths,omitempty"`
}

// AuditLog is the audit store dependency.
type AuditLog interface {
	Record(ctx context.Context, event audit.Event) error
	List(ctx context.Context, query audit.Query) ([]audit.Event, error)
}

// ConfigTools provides preview and diff operations.
type ConfigTools interface {
	PreviewConfig(ctx context.Context, req ConfigPreviewRequest) (*ConfigPreviewResponse, error)
	DiffConfig(ctx context.Context, req ConfigDiffRequest) (*ConfigDiffResponse, error)
}

// ProbeRunner executes synthetic diagnostic provider/model probes.
type ProbeRunner interface {
	ProbeProvider(ctx context.Context, req ProbeRequest) (*ProbeResult, error)
	ProbeModel(ctx context.Context, req ProbeRequest) (*ProbeResult, error)
}

// ConfigPreviewRequest describes a draft config preview.
type ConfigPreviewRequest struct {
	Config     any    `json:"config,omitempty"`
	RevisionID string `json:"revision_id,omitempty"`
}

// ConfigPreviewResponse summarizes the compiled runtime snapshot.
type ConfigPreviewResponse struct {
	Valid                 bool              `json:"valid"`
	Errors                []string          `json:"errors,omitempty"`
	Warnings              []string          `json:"warnings,omitempty"`
	RevisionID            string            `json:"revision_id,omitempty"`
	SnapshotSchemaVersion int               `json:"snapshot_schema_version"`
	CompilerVersion       string            `json:"compiler_version,omitempty"`
	IngressListen         string            `json:"ingress_listen,omitempty"`
	ProviderCount         int               `json:"provider_count"`
	EnabledProviderCount  int               `json:"enabled_provider_count"`
	Models                []string          `json:"models,omitempty"`
	EnabledRoutes         []string          `json:"enabled_routes,omitempty"`
	PricingSources        map[string]string `json:"pricing_sources,omitempty"`
}

// ConfigDiffRequest describes a structural config diff.
type ConfigDiffRequest struct {
	FromRevisionID string `json:"from_revision_id,omitempty"`
	ToRevisionID   string `json:"to_revision_id,omitempty"`
	Config         any    `json:"config,omitempty"`
}

// ConfigDiffResponse contains structured config changes.
type ConfigDiffResponse struct {
	FromRevisionID string       `json:"from_revision_id,omitempty"`
	ToRevisionID   string       `json:"to_revision_id,omitempty"`
	Changes        []DiffChange `json:"changes"`
}

// DiffChange is one structural config change.
type DiffChange struct {
	Path   string `json:"path"`
	Before any    `json:"before,omitempty"`
	After  any    `json:"after,omitempty"`
	Kind   string `json:"kind"`
}

// ProbeRequest describes a diagnostic probe.
type ProbeRequest struct {
	ProviderID string            `json:"provider_id,omitempty"`
	Model      string            `json:"model,omitempty"`
	Protocol   string            `json:"protocol,omitempty"`
	Prompt     string            `json:"prompt,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	TimeoutMs  int               `json:"timeout_ms,omitempty"`
}

// ProbeResult is a synthetic probe response.
type ProbeResult struct {
	Diagnostic      bool              `json:"diagnostic"`
	ProviderID      string            `json:"provider_id,omitempty"`
	Model           string            `json:"model,omitempty"`
	StatusCode      int               `json:"status_code,omitempty"`
	LatencyMs       int64             `json:"latency_ms,omitempty"`
	Healthy         bool              `json:"healthy"`
	Error           string            `json:"error,omitempty"`
	ResponseExcerpt string            `json:"response_excerpt,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	ProbedAt        time.Time         `json:"probed_at"`
}

func buildStatusPayload(deps Deps) map[string]interface{} {
	resp := map[string]interface{}{
		"version":              deps.Version,
		"product_version":      version.ProductVersion,
		"rpc_contract_version": version.RPCContractVersion,
		"startedAt":            deps.StartedAt.UTC().Format(time.RFC3339),
		"uptime":               time.Since(deps.StartedAt).String(),
	}
	if !deps.StartedAt.IsZero() {
		resp["started_at"] = deps.StartedAt.UTC().Format(time.RFC3339)
	}

	gateway := deps.gatewayRPC()
	if gateway != nil {
		status, err := gateway.GetStatus()
		if err != nil {
			if isRPCDisconnectedError(err) {
				resp["gateway_status"] = "disconnected"
			} else {
				resp["gateway_status"] = "error"
			}
			resp["gateway_error"] = err.Error()
		} else {
			resp["gateway_status"] = "connected"
			resp["gateway"] = status
			resp["gateway_readiness"] = status.Readiness.String()
			resp["gateway_listener"] = status.Listener
			resp["active_snapshot_id"] = status.ActiveSnapshotID
			resp["active_requests"] = status.ActiveRequests
			resp["provider_health_count"] = len(status.ProviderHealth)
			resp["healthy_provider_count"] = countHealthyProviders(status.ProviderHealth)
			resp["unhealthy_provider_count"] = len(status.ProviderHealth) - countHealthyProviders(status.ProviderHealth)
			if status.LastAutoRemediationReason != "" {
				resp["gateway_last_auto_remediation_reason"] = status.LastAutoRemediationReason
				if !status.LastAutoRemediationAt.IsZero() {
					resp["gateway_last_auto_remediation_at"] = status.LastAutoRemediationAt.UTC().Format(time.RFC3339Nano)
				}
			}
			if pricingStatus, pricingErr := gateway.GetPricingStatus(); pricingErr == nil {
				resp["pricing"] = pricingStatus
			}
		}
	} else {
		resp["gateway_status"] = "disconnected"
	}

	resp["telemetry_last_checked_at"] = time.Now().UTC().Format(time.RFC3339)
	telemetry := deps.telemetryRPC()
	if telemetry != nil {
		if pinger, ok := telemetry.(telemetryPinger); ok {
			ping, err := pinger.Ping()
			if err != nil {
				if isRPCDisconnectedError(err) {
					resp["telemetry_status"] = "disconnected"
				} else {
					resp["telemetry_status"] = "error"
				}
				resp["telemetry_error"] = err.Error()
			} else if !ping.Healthy {
				resp["telemetry_status"] = "error"
				resp["telemetry_error"] = "telemetry unhealthy"
				resp["telemetry_version"] = ping.Version
				resp["telemetry_event_count"] = ping.EventCount
			} else {
				resp["telemetry_status"] = "connected"
				resp["telemetry_version"] = ping.Version
				resp["telemetry_event_count"] = ping.EventCount
			}
		} else {
			resp["telemetry_status"] = "connected"
		}
	} else {
		resp["telemetry_status"] = "disconnected"
	}
	return resp
}

func runtimeStatusHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := buildStatusPayload(deps)
		resp["runtime"] = deps.Runtime
		writeJSON(w, http.StatusOK, resp)
	}
}

func runtimePreflightHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		status := buildStatusPayload(deps)
		checks := []map[string]any{
			preflightCheck("gateway_connected", status["gateway_status"] == "connected", fmt.Sprint(status["gateway_error"])),
			preflightCheck("gateway_ready", status["gateway_readiness"] == "ready", fmt.Sprint(status["gateway_readiness"])),
			preflightCheck("telemetry_connected", status["telemetry_status"] == "connected", fmt.Sprint(status["telemetry_error"])),
		}
		ok := true
		for _, check := range checks {
			if check["ok"] != true {
				ok = false
				break
			}
		}
		recordAudit(deps, r, "runtime.preflight", "runtime", ok, "", map[string]any{"checks": checks})
		writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "checks": checks, "runtime": deps.Runtime})
	}
}

func updateStatusHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if deps.Updates == nil {
			writeError(w, http.StatusServiceUnavailable, "update manager not available")
			return
		}
		status, err := deps.Updates.Status()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func updateCheckHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if deps.Updates == nil {
			writeError(w, http.StatusServiceUnavailable, "update manager not available")
			return
		}
		status, err := deps.Updates.Check(r.Context())
		if err != nil {
			recordAudit(deps, r, "update.check", "release", false, err.Error(), nil)
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		recordAudit(deps, r, "update.check", status.LatestTag, true, "", map[string]any{"update_available": status.UpdateAvailable})
		writeJSON(w, http.StatusOK, status)
	}
}

func updateFetchHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if deps.Updates == nil {
			writeError(w, http.StatusServiceUnavailable, "update manager not available")
			return
		}
		var req struct {
			Force bool `json:"force"`
		}
		if err := decodeOptionalJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		status, err := deps.Updates.Fetch(r.Context(), req.Force)
		if err != nil {
			recordAudit(deps, r, "update.fetch", "release", false, err.Error(), nil)
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		recordAudit(deps, r, "update.fetch", status.CachedVersion, status.CachedVerify.OK, firstNonEmpty(status.LastCheckError, status.LastApplyError), map[string]any{"bundle_dir": status.CachedBundleDir})
		writeJSON(w, http.StatusOK, status)
	}
}

func updateApplyHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if deps.Updates == nil {
			writeError(w, http.StatusServiceUnavailable, "update manager not available")
			return
		}
		var req struct {
			BundleDir string `json:"bundle_dir"`
			Download  bool   `json:"download"`
			DryRun    bool   `json:"dry_run"`
			Force     bool   `json:"force"`
		}
		if err := decodeOptionalJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		status, err := deps.Updates.Apply(r.Context(), req.BundleDir, req.Download, req.DryRun, req.Force)
		if err != nil {
			recordAudit(deps, r, "update.apply", req.BundleDir, false, err.Error(), map[string]any{"dry_run": req.DryRun, "download": req.Download})
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		recordAudit(deps, r, "update.apply", status.CachedVersion, true, "", map[string]any{"dry_run": req.DryRun, "bundle_dir": status.CachedBundleDir, "backup_dir": status.LastBackupDir})
		writeJSON(w, http.StatusOK, status)
	}
}

func updateRollbackHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if deps.Updates == nil {
			writeError(w, http.StatusServiceUnavailable, "update manager not available")
			return
		}
		status, err := deps.Updates.Rollback()
		if err != nil {
			recordAudit(deps, r, "update.rollback", "last-backup", false, err.Error(), nil)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		recordAudit(deps, r, "update.rollback", status.LastBackupDir, true, "", nil)
		writeJSON(w, http.StatusOK, status)
	}
}

func decodeOptionalJSON(r *http.Request, target any) error {
	if r == nil || r.Body == nil {
		return nil
	}
	defer r.Body.Close()
	err := json.NewDecoder(r.Body).Decode(target)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func auditHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if deps.AuditLog == nil {
			writeJSON(w, http.StatusOK, map[string]any{"events": []audit.Event{}, "count": 0})
			return
		}
		query := audit.Query{
			Limit:  intQuery(r, "limit", 100),
			Action: strings.TrimSpace(r.URL.Query().Get("action")),
		}
		if since := timeQuery(r, "since"); since != nil {
			query.Since = *since
		}
		events, err := deps.AuditLog.List(r.Context(), query)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"events": events, "count": len(events)})
	}
}

func configPreviewHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if deps.ConfigTools == nil {
			writeError(w, http.StatusServiceUnavailable, "config preview not available")
			return
		}
		var req ConfigPreviewRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		resp, err := deps.ConfigTools.PreviewConfig(r.Context(), req)
		if err != nil {
			recordAudit(deps, r, "config.preview", req.RevisionID, false, err.Error(), nil)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		recordAudit(deps, r, "config.preview", req.RevisionID, resp.Valid, strings.Join(resp.Errors, "; "), map[string]any{"provider_count": resp.ProviderCount})
		writeJSON(w, http.StatusOK, resp)
	}
}

func configDiffHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if deps.ConfigTools == nil {
			writeError(w, http.StatusServiceUnavailable, "config diff not available")
			return
		}
		var req ConfigDiffRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		resp, err := deps.ConfigTools.DiffConfig(r.Context(), req)
		if err != nil {
			recordAudit(deps, r, "config.diff", req.ToRevisionID, false, err.Error(), nil)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		recordAudit(deps, r, "config.diff", req.ToRevisionID, true, "", map[string]any{"change_count": len(resp.Changes)})
		writeJSON(w, http.StatusOK, resp)
	}
}

func probeProviderHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handleProbe(w, r, deps, "provider")
	}
}

func probeModelHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handleProbe(w, r, deps, "model")
	}
}

func handleProbe(w http.ResponseWriter, r *http.Request, deps Deps, kind string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if deps.ProbeRunner == nil {
		writeError(w, http.StatusServiceUnavailable, "probe not available")
		return
	}
	var req ProbeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var (
		resp *ProbeResult
		err  error
	)
	if kind == "provider" {
		resp, err = deps.ProbeRunner.ProbeProvider(r.Context(), req)
	} else {
		resp, err = deps.ProbeRunner.ProbeModel(r.Context(), req)
	}
	resource := strings.TrimSpace(req.ProviderID + "/" + req.Model)
	if err != nil {
		recordAudit(deps, r, "probe."+kind, resource, false, err.Error(), nil)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	recordAudit(deps, r, "probe."+kind, resource, resp.Healthy, resp.Error, map[string]any{"status_code": resp.StatusCode, "latency_ms": resp.LatencyMs})
	writeJSON(w, http.StatusOK, resp)
}

func diagnosticsHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		events := []audit.Event{}
		if deps.AuditLog != nil {
			if listed, err := deps.AuditLog.List(r.Context(), audit.Query{Limit: 20}); err == nil {
				events = listed
			}
		}
		resp := map[string]any{
			"generated_at": time.Now().UTC().Format(time.RFC3339),
			"redacted":     true,
			"status":       buildStatusPayload(deps),
			"runtime":      deps.Runtime,
			"audit_tail":   events,
		}
		recordAudit(deps, r, "diagnostics.generate", "diagnostics", true, "", nil)
		writeJSON(w, http.StatusOK, resp)
	}
}

func secretsStatusHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if deps.ConfigQuery == nil {
			writeError(w, http.StatusServiceUnavailable, "config query not available")
			return
		}
		view, err := deps.ConfigQuery.GetCurrentConfigView()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		items := secretItemsFromConfig(nil)
		if view != nil {
			items = secretItemsFromConfig(view.Config)
		}
		missing := 0
		for _, item := range items {
			if !item.Present {
				missing++
			}
		}
		resp := map[string]any{
			"items":         items,
			"count":         len(items),
			"missing_count": missing,
			"ok":            missing == 0,
			"redacted":      true,
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// clientErrorHandler accepts frontend error reports via POST.
// Payload: {"message": "...", "stack": "...", "source": "..."}
// Errors are logged server-side; no persistent storage (audit log is ephemeral).
func clientErrorHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var payload struct {
			Message string `json:"message"`
			Stack   string `json:"stack"`
			Source  string `json:"source"`
			URL     string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		logger.Error("admin client error reported",
			"message", payload.Message,
			"source", payload.Source,
			"url", payload.URL,
			"stack", payload.Stack,
		)
		recordAudit(deps, r, "client.error", "client", true, payload.Message, nil)
		writeJSON(w, http.StatusNoContent, nil)
	}
}

func replayUnavailableHandler(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusServiceUnavailable, "replay not available")
}

func metricsHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := buildStatusPayload(deps)
		gatewayConnected := 0
		if status["gateway_status"] == "connected" {
			gatewayConnected = 1
		}
		telemetryConnected := 0
		if status["telemetry_status"] == "connected" {
			telemetryConnected = 1
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		fmt.Fprintln(w, "# HELP aigw_control_up Control plane is serving metrics.")
		fmt.Fprintln(w, "# TYPE aigw_control_up gauge")
		fmt.Fprintln(w, "aigw_control_up 1")
		fmt.Fprintln(w, "# HELP aigw_gateway_connected Gateway RPC connectivity.")
		fmt.Fprintln(w, "# TYPE aigw_gateway_connected gauge")
		fmt.Fprintf(w, "aigw_gateway_connected %d\n", gatewayConnected)
		fmt.Fprintln(w, "# HELP aigw_telemetry_connected Telemetry RPC connectivity.")
		fmt.Fprintln(w, "# TYPE aigw_telemetry_connected gauge")
		fmt.Fprintf(w, "aigw_telemetry_connected %d\n", telemetryConnected)
		if value, ok := numericStatusValue(status["active_requests"]); ok {
			fmt.Fprintln(w, "# HELP aigw_active_requests Active data-plane requests.")
			fmt.Fprintln(w, "# TYPE aigw_active_requests gauge")
			fmt.Fprintf(w, "aigw_active_requests %v\n", value)
		}
	}
}

type secretStatusItem struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Present   bool   `json:"present"`
	Reference string `json:"reference,omitempty"`
}

func secretItemsFromConfig(cfg *core.Config) []secretStatusItem {
	if cfg == nil {
		return []secretStatusItem{}
	}
	items := []secretStatusItem{
		{Name: "admin.bootstrap_token", Kind: "admin", Present: strings.TrimSpace(cfg.Admin.BootstrapToken) != ""},
		{Name: "admin.cookie_signing_key", Kind: "admin", Present: strings.TrimSpace(cfg.Admin.CookieSigningKey) != ""},
	}
	for _, token := range cfg.Admin.Tokens {
		name := strings.TrimSpace(token.Name)
		if name == "" {
			name = "unnamed"
		}
		items = append(items, secretStatusItem{Name: "admin.tokens." + name, Kind: "admin_token", Present: strings.TrimSpace(token.Token) != ""})
	}
	for _, provider := range cfg.Providers {
		name := strings.TrimSpace(provider.Name)
		if name == "" {
			name = "unnamed"
		}
		items = append(items, secretStatusItem{Name: "providers." + name + ".api_key", Kind: "provider_api_key", Present: strings.TrimSpace(provider.APIKey) != ""})
	}
	return items
}

func recordAudit(deps Deps, r *http.Request, action string, resource string, success bool, errText string, details map[string]any) {
	if deps.AuditLog == nil {
		return
	}
	role := AdminRoleFromRequest(r)
	actor := role
	if actor == "" {
		actor = "anonymous"
	}
	source := ""
	if r != nil {
		source = r.RemoteAddr
	}
	_ = deps.AuditLog.Record(context.Background(), audit.Event{
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

// SummarizeSnapshot creates a redacted preview from a compiled runtime snapshot.
func SummarizeSnapshot(snap *snapshot.Snapshot, revisionID string, warnings []string) ConfigPreviewResponse {
	resp := ConfigPreviewResponse{
		Valid:                 true,
		Warnings:              warnings,
		RevisionID:            revisionID,
		SnapshotSchemaVersion: snap.Meta.SchemaVersion,
		CompilerVersion:       snap.Meta.CompilerVersion,
		IngressListen:         snap.Ingress.Listen,
		ProviderCount:         len(snap.Providers),
		EnabledRoutes:         append([]string(nil), snap.Contract.EnabledRoutes...),
		PricingSources:        make(map[string]string),
	}
	models := make(map[string]struct{})
	for _, provider := range snap.Providers {
		if provider.ExecutionPolicy.Enabled {
			resp.EnabledProviderCount++
		}
		for _, model := range provider.ModelTable {
			models[model.PublicModel] = struct{}{}
		}
	}
	for model := range models {
		resp.Models = append(resp.Models, model)
	}
	sort.Strings(resp.Models)
	for _, source := range snap.Pricing.Sources {
		resp.PricingSources[source.ID] = source.Vendor
	}
	return resp
}

// DiffConfigs computes a redacted structural diff between two config payloads.
func DiffConfigs(before any, after any) []DiffChange {
	beforeMap := anyToJSONMap(before)
	afterMap := anyToJSONMap(after)
	changes := make([]DiffChange, 0)
	walkDiff("", beforeMap, afterMap, &changes)
	sort.SliceStable(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes
}

func walkDiff(path string, before any, after any, changes *[]DiffChange) {
	beforeMap, beforeIsMap := before.(map[string]any)
	afterMap, afterIsMap := after.(map[string]any)
	if beforeIsMap && afterIsMap {
		keys := make(map[string]struct{}, len(beforeMap)+len(afterMap))
		for key := range beforeMap {
			keys[key] = struct{}{}
		}
		for key := range afterMap {
			keys[key] = struct{}{}
		}
		for key := range keys {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			walkDiff(childPath, beforeMap[key], afterMap[key], changes)
		}
		return
	}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	if string(beforeJSON) == string(afterJSON) {
		return
	}
	kind := "changed"
	if before == nil {
		kind = "added"
	}
	if after == nil {
		kind = "removed"
	}
	*changes = append(*changes, DiffChange{Path: path, Before: redactDiffValue(path, before), After: redactDiffValue(path, after), Kind: kind})
}

func anyToJSONMap(value any) map[string]any {
	data, _ := json.Marshal(value)
	var result map[string]any
	_ = json.Unmarshal(data, &result)
	if result == nil {
		result = map[string]any{}
	}
	return result
}

func redactDiffValue(path string, value any) any {
	if isSecretPath(path) {
		if value == nil || strings.TrimSpace(fmt.Sprint(value)) == "" {
			return ""
		}
		return "[redacted]"
	}
	switch typed := value.(type) {
	case map[string]any:
		redacted := make(map[string]any, len(typed))
		for key, child := range typed {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			redacted[key] = redactDiffValue(childPath, child)
		}
		return redacted
	case []any:
		redacted := make([]any, len(typed))
		for i, child := range typed {
			childPath := fmt.Sprintf("%s[%d]", path, i)
			redacted[i] = redactDiffValue(childPath, child)
		}
		return redacted
	default:
		return value
	}
}

func isSecretPath(path string) bool {
	lower := strings.ToLower(path)
	// Match common secret field names.
	for _, fragment := range []string{"token", "api_key", "secret", "cookie_signing_key"} {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	// Match provider headers that commonly carry credentials.
	// Path patterns: providers[].headers.<name> or providers[X].headers.<name>
	if strings.Contains(lower, "headers.") {
		// Extract the header name (last segment after "headers.")
		parts := strings.SplitN(lower, "headers.", 2)
		if len(parts) == 2 {
			headerName := parts[1]
			// Common credential header names
			for _, credHeader := range []string{"authorization", "cookie", "x-api-key", "api-key", "x-auth-token", "x-token"} {
				if strings.HasPrefix(headerName, credHeader) {
					return true
				}
			}
		}
	}
	return false
}

func preflightCheck(name string, ok bool, detail string) map[string]any {
	resp := map[string]any{"name": name, "ok": ok}
	if strings.TrimSpace(detail) != "" && detail != "<nil>" {
		resp["detail"] = detail
	}
	return resp
}

func resultError(result *publish.PublishResult) string {
	if result == nil {
		return "empty result"
	}
	return result.ErrorMessage
}

func validationError(result *publish.ConfigValidationResult) string {
	if result == nil {
		return "empty result"
	}
	return strings.Join(result.Errors, "; ")
}

func countHealthyProviders(providers map[string]gatewaycontrol.ProviderHealth) int {
	count := 0
	for _, provider := range providers {
		if provider.Healthy {
			count++
		}
	}
	return count
}

func numericStatusValue(value any) (any, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return typed, true
	case float64:
		return typed, true
	default:
		return nil, false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func writeConfigToolUnavailablePreview(err error) *ConfigPreviewResponse {
	return &ConfigPreviewResponse{Valid: false, Errors: []string{err.Error()}}
}
