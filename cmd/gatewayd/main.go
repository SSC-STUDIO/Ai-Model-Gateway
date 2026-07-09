// Package main is the entry point for gatewayd - the data plane daemon.
// gatewayd serves inference requests using compiled runtime snapshots.
// It does not read YAML configuration directly - all configuration
// comes from snapshots applied via IPC from controld.
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"ai-model-gateway/internal/contracts"
	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/gateway/api"
	"ai-model-gateway/internal/gateway/snapshot"
	"ai-model-gateway/internal/gateway/telemetry"
	"ai-model-gateway/internal/infra/logger"
	pricinginfra "ai-model-gateway/internal/infra/pricing"
	"ai-model-gateway/internal/version"
)

var Version = version.ProductVersion

const modelOwner = "ai-model-gateway"

// Config is the bootstrap configuration for gatewayd.
// This is minimal - just enough to start the daemon and connect to other planes.
type Config struct {
	// Listen is the address to listen on for inference requests.
	Listen string `json:"listen"`

	// ControlSocket is the path to the control plane IPC socket.
	ControlSocket string `json:"control_socket"`

	// TelemetrySocket is the path to the telemetry plane IPC socket.
	TelemetrySocket string `json:"telemetry_socket"`

	// DataDir is the directory for runtime data.
	DataDir string `json:"data_dir"`

	// LogLevel is the logging level.
	LogLevel string `json:"log_level"`

	// AdminProxyURL optionally preserves legacy single-port admin URLs by
	// forwarding /admin and /api/admin traffic to controld.
	AdminProxyURL string `json:"admin_proxy_url"`

	// HTTP timeouts (in seconds)
	ReadTimeoutSec  int `json:"read_timeout_sec"`
	WriteTimeoutSec int `json:"write_timeout_sec"`
	IdleTimeoutSec  int `json:"idle_timeout_sec"`

	// RPC connection timeout
	RPCTimeoutSec int `json:"rpc_timeout_sec"`

	// RPC retry settings
	RPCRetryCount    int `json:"rpc_retry_count"`
	RPCRetryInterval int `json:"rpc_retry_interval_sec"`
}

// Daemon represents the gatewayd daemon.
type Daemon struct {
	config            Config
	transport         contracts.Transport
	httpServer        *http.Server
	httpListener      net.Listener
	rpcServer         *RPCServer
	telClient         *telemetry.Client
	healthHTTP        *http.Client
	runtime           *api.RuntimeState
	pricingCatalog    *pricinginfra.Catalog
	snapshot          *snapshot.Snapshot
	snapshotMu        sync.RWMutex
	runCtx            context.Context
	runCancel         context.CancelFunc
	healthProbeMu     sync.Mutex
	healthProbeCancel context.CancelFunc
	healthProbeDone   chan struct{}
	startedAt         time.Time
	activeReqs        atomic.Int64

	remediationMu             sync.Mutex
	lastAutoRemediationReason string
	lastAutoRemediationAt     time.Time
}

func main() {
	// Parse flags
	configPath := flag.String("config", "", "Path to bootstrap config file")
	listen := flag.String("listen", "", "Address to listen on")
	controlSocket := flag.String("control", "", "Control plane socket path")
	telemetrySocket := flag.String("telemetry", "", "Telemetry plane socket path")
	dataDir := flag.String("data-dir", "", "Runtime data directory")
	showVersion := flag.Bool("version", false, "Show version")
	flag.Parse()

	log := logger.With("component", "gatewayd")
	if *showVersion {
		fmt.Printf("gatewayd version %s (%s/%s)\n", Version, runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	// Load config
	cfg := loadConfig(*configPath, *listen, *controlSocket, *telemetrySocket, *dataDir)

	// Create daemon
	d, err := NewDaemon(cfg)
	if err != nil {
		log.Error("failed to create daemon", "error", err)
		os.Exit(1)
	}

	// Setup signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Start daemon
	if err := d.Start(ctx); err != nil {
		log.Error("failed to start", "error", err)
		os.Exit(1)
	}

	log.Info("started", "listen", cfg.Listen)

	// Wait for shutdown
	<-ctx.Done()
	log.Info("shutting down...")

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := d.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown error", "error", err)
	}

	log.Info("stopped")
}

// loadConfig loads the bootstrap configuration.
func loadConfig(configPath, listen, controlSocket, telemetrySocket, dataDir string) Config {
	cfg := Config{
		Listen:   "127.0.0.1:18080",
		DataDir:  "data",
		LogLevel: "info",
	}

	// Load from file if specified
	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			logger.Warn("could not read config file", "error", err)
		} else if err := json.Unmarshal(data, &cfg); err != nil {
			logger.Warn("could not parse config file", "error", err)
		}
	}

	// Override with flags
	if listen != "" {
		cfg.Listen = listen
	}
	if controlSocket != "" {
		cfg.ControlSocket = controlSocket
	}
	if telemetrySocket != "" {
		cfg.TelemetrySocket = telemetrySocket
	}
	if dataDir != "" {
		cfg.DataDir = dataDir
	}

	// Set defaults based on platform
	if cfg.ControlSocket == "" {
		cfg.ControlSocket = defaultSocketPath("gateway-control")
	}
	if cfg.TelemetrySocket == "" {
		cfg.TelemetrySocket = defaultSocketPath("telemetry-ingest")
	}

	return cfg
}

// defaultSocketPath returns the default socket path for the platform.
func defaultSocketPath(name string) string {
	if runtime.GOOS == "windows" {
		return name // Named pipe on Windows
	}
	// Unix socket on Linux/macOS
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return filepath.Join(runtimeDir, name+".sock")
	}
	return filepath.Join("/tmp", name+".sock")
}

// NewDaemon creates a new gatewayd daemon.
func NewDaemon(cfg Config) (*Daemon, error) {
	return &Daemon{
		config:     cfg,
		transport:  contracts.DefaultTransport,
		healthHTTP: newHealthHTTPClient(),
		runtime:    api.NewRuntimeState(),
		startedAt:  time.Now(),
	}, nil
}

// Start starts the daemon.
func (d *Daemon) Start(ctx context.Context) error {
	d.runCtx, d.runCancel = context.WithCancel(ctx)

	// Start RPC server for control plane
	d.rpcServer = NewRPCServer(d)
	if err := d.startRPCServer(d.runCtx); err != nil {
		return fmt.Errorf("start RPC server: %w", err)
	}

	// Connect to telemetry in the background. Blocking here delays the HTTP
	// server and lets controld miss its initial publish window while gatewayd
	// retries telemetryd (up to rpc_retry_count * rpc_retry_interval).
	go func() {
		if err := d.connectTelemetry(d.runCtx); err != nil {
			logger.Warn("could not connect to telemetry", "error", err)
		}
	}()

	d.tryRestoreSnapshotFromDisk()

	// Start HTTP server
	readTimeout := time.Duration(d.config.ReadTimeoutSec) * time.Second
	if readTimeout == 0 {
		readTimeout = 30 * time.Second
	}
	writeTimeout := time.Duration(d.config.WriteTimeoutSec) * time.Second
	if writeTimeout == 0 {
		writeTimeout = 60 * time.Second
	}
	idleTimeout := time.Duration(d.config.IdleTimeoutSec) * time.Second
	if idleTimeout == 0 {
		idleTimeout = 120 * time.Second
	}

	d.httpServer = &http.Server{
		Addr:         d.config.Listen,
		Handler:      d.createHandler(),
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
	}

	listener, err := net.Listen("tcp", d.config.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", d.config.Listen, err)
	}
	d.httpListener = listener

	go func() {
		logger.Info("listening", "address", listener.Addr().String())
		if err := d.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server error", "error", err)
		}
	}()

	return nil
}

// startRPCServer starts the RPC server for control plane communication.
func (d *Daemon) startRPCServer(ctx context.Context) error {
	listener, err := d.transport.Listen(d.config.ControlSocket)
	if err != nil {
		return fmt.Errorf("listen on control socket: %w", err)
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					logger.Error("accept error", "error", err)
					continue
				}
			}
			go d.rpcServer.ServeConn(conn)
		}
	}()

	return nil
}

// connectTelemetry connects to the telemetry plane.
func (d *Daemon) connectTelemetry(ctx context.Context) error {
	retryCount := d.config.RPCRetryCount
	if retryCount <= 0 {
		retryCount = 10
	}
	retryInterval := time.Duration(d.config.RPCRetryInterval) * time.Second
	if retryInterval == 0 {
		retryInterval = time.Second
	}

	// Try to connect initially
	var initialConn contracts.Conn
	var err error
	for i := 0; i < retryCount; i++ {
		initialConn, err = d.transport.Dial(d.config.TelemetrySocket)
		if err == nil {
			break
		}
		logger.Debug("waiting for telemetry", "attempt", i+1, "max", retryCount)
		time.Sleep(retryInterval)
	}
	if err != nil {
		return fmt.Errorf("connect to telemetry: %w", err)
	}

	// Create telemetry client with reconnection support
	dialer := func() (telemetry.RPCClient, error) {
		conn, err := d.transport.Dial(d.config.TelemetrySocket)
		if err != nil {
			return nil, err
		}
		return NewRPCClient(conn), nil
	}

	telConfig := telemetry.DefaultClientConfig()
	telConfig.ReconnectInterval = retryInterval
	telConfig.MaxReconnectAttempts = 100 // effectively unlimited during runtime

	telClient, err := telemetry.NewClientWithDialer(dialer, telConfig)
	if err != nil {
		// Fallback: use initial connection without reconnection
		rpcClient := NewRPCClient(initialConn)
		d.telClient = telemetry.NewClient(rpcClient, telConfig)
		return nil
	}
	initialConn.Close()
	d.telClient = telClient

	return nil
}

// createHandler creates the HTTP handler.
func (d *Daemon) createHandler() http.Handler {
	mux := http.NewServeMux()

	// Health endpoints: original /-/health + K8s-style /health and /ready aliases (fix for #11)
	mux.HandleFunc("/-/health", d.healthHandler)
	mux.HandleFunc("/health", d.healthHandler)
	mux.HandleFunc("/ready", d.healthHandler)

	if adminProxy := d.adminProxyHandler(); adminProxy != nil {
		mux.Handle("/admin", adminProxy)
		mux.Handle("/admin/", adminProxy)
		mux.Handle("/api/admin", adminProxy)
		mux.Handle("/api/admin/", adminProxy)
		mux.Handle("/icon.svg", adminProxy)
		mux.Handle("/favicon.svg", adminProxy)
		mux.Handle("/manifest.json", adminProxy)
	}

	// All /v1/* endpoints require a valid bearer token (fix for #9).
	mux.HandleFunc("/v1/models", d.requireDataPlaneAuth(d.modelsHandler))
	mux.HandleFunc("/v1/chat/completions", d.requireDataPlaneAuth(d.chatCompletionsHandler))
	mux.HandleFunc("/v1/messages", d.requireDataPlaneAuth(d.messagesHandler))
	mux.HandleFunc("/v1/responses", d.requireDataPlaneAuth(d.responsesHandler))

	return mux
}

// requireDataPlaneAuth wraps a data-plane handler with bearer-token validation
// against the active snapshot AdminTokens. Missing or invalid Authorization
// headers are rejected with a 401 in OpenAI-compatible JSON envelope before
// any upstream call is made.
func (d *Daemon) requireDataPlaneAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d.snapshotMu.RLock()
		snap := d.snapshot
		d.snapshotMu.RUnlock()

		authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
		if authHeader == "" || !strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
			d.writeDataPlaneUnauthorized(w, "missing bearer token")
			return
		}
		presented := strings.TrimSpace(authHeader[len("Bearer "):])
		if presented == "" {
			d.writeDataPlaneUnauthorized(w, "empty bearer token")
			return
		}

		if snap == nil || len(snap.AdminTokens) == 0 {
			// No tokens configured yet (startup race or explicit empty config).
			// Treat all callers as unauthenticated rather than silently open.
			d.writeDataPlaneUnauthorized(w, "no admin tokens configured")
			return
		}

		matched := false
		for _, t := range snap.AdminTokens {
			if strings.TrimSpace(t.Token) == "" {
				continue
			}
			if subtle.ConstantTimeCompare([]byte(presented), []byte(strings.TrimSpace(t.Token))) == 1 {
				matched = true
				break
			}
		}
		if !matched {
			d.writeDataPlaneUnauthorized(w, "invalid bearer token")
			return
		}
		next(w, r)
	}
}

// writeDataPlaneUnauthorized emits the OpenAI-compatible error envelope used
// by every protected /v1/* endpoint when authentication fails.
func (d *Daemon) writeDataPlaneUnauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="ai-model-gateway"`)
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "invalid_request_error",
			"param":   "authorization",
			"code":    "invalid_api_key",
		},
	})
}

func (d *Daemon) adminProxyHandler() http.Handler {
	rawURL := strings.TrimSpace(d.config.AdminProxyURL)
	if rawURL == "" {
		return nil
	}
	target, err := url.Parse(rawURL)
	if err != nil || target.Scheme == "" || target.Host == "" {
		logger.Warn("invalid admin proxy URL", "url", rawURL, "error", err)
		return nil
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Warn("admin proxy error", "path", r.URL.Path, "error", err)
		http.Error(w, "admin plane unavailable", http.StatusBadGateway)
	}
	return proxy
}

// healthHandler handles health check requests.
func (d *Daemon) healthHandler(w http.ResponseWriter, r *http.Request) {
	d.snapshotMu.RLock()
	snap := d.snapshot
	d.snapshotMu.RUnlock()

	status := "healthy"
	if snap == nil {
		status = "starting"
	}

	resp := map[string]interface{}{
		"status":    status,
		"version":   Version,
		"startedAt": d.startedAt.UTC().Format(time.RFC3339),
	}

	if snap != nil {
		resp["snapshot_id"] = snap.Meta.SnapshotID
		resp["revision_id"] = snap.Meta.RevisionID

		// Enhanced: return provider health status when detail=true
		if r.URL.Query().Get("detail") == "true" && d.runtime != nil {
			providerHealth := d.runtime.ProviderHealthSnapshot(snap)
			if providerHealth != nil {
				providers := make([]map[string]interface{}, 0, len(snap.Providers))
				for _, p := range snap.Providers {
					upstreamID := p.UpstreamID
					if strings.TrimSpace(upstreamID) == "" {
						upstreamID = p.ProviderID
					}
					providerInfo := map[string]interface{}{
						"provider_id":        p.ProviderID,
						"upstream_id":        upstreamID,
						"base_url":           p.BaseURL,
						"anthropic_base_url": p.AnthropicBaseURL,
						"enabled":            p.ExecutionPolicy.Enabled,
					}
					// Add health details if available
					if health, ok := providerHealth[upstreamID]; ok {
						providerInfo["healthy"] = health.Healthy
						providerInfo["latency_ms"] = health.LatencyMs
						providerInfo["consecutive_failures"] = health.ConsecutiveFailures
						if !health.LastCheck.IsZero() {
							providerInfo["last_check"] = health.LastCheck.UTC().Format(time.RFC3339)
						}
						if !health.LastSuccess.IsZero() {
							providerInfo["last_success"] = health.LastSuccess.UTC().Format(time.RFC3339)
						}
						if !health.CooldownUntil.IsZero() {
							providerInfo["cooldown_until"] = health.CooldownUntil.UTC().Format(time.RFC3339)
						}
					}
					providers = append(providers, providerInfo)
				}
				resp["providers"] = providers
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// modelsHandler handles models listing requests.
func (d *Daemon) modelsHandler(w http.ResponseWriter, r *http.Request) {
	d.snapshotMu.RLock()
	snap := d.snapshot
	d.snapshotMu.RUnlock()

	if snap == nil {
		http.Error(w, `{"error":"no snapshot loaded"}`, http.StatusServiceUnavailable)
		return
	}

	createdAt := d.startedAt.Unix()
	if !snap.Meta.GeneratedAt.IsZero() {
		createdAt = snap.Meta.GeneratedAt.Unix()
	}

	type modelObject struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}

	// Collect models from all providers in a stable, OpenAI-compatible shape.
	modelsByID := make(map[string]modelObject)
	for _, p := range snap.Providers {
		for _, m := range p.ModelTable {
			if _, ok := modelsByID[m.PublicModel]; ok {
				continue
			}
			modelsByID[m.PublicModel] = modelObject{
				ID:      m.PublicModel,
				Object:  "model",
				Created: createdAt,
				OwnedBy: modelOwner,
			}
		}
	}

	modelIDs := make([]string, 0, len(modelsByID))
	for id := range modelsByID {
		modelIDs = append(modelIDs, id)
	}
	sort.Strings(modelIDs)

	models := make([]modelObject, 0, len(modelIDs))
	for _, id := range modelIDs {
		models = append(models, modelsByID[id])
	}

	resp := map[string]interface{}{
		"object": "list",
		"data":   models,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// chatCompletionsHandler handles chat completion requests.
func (d *Daemon) chatCompletionsHandler(w http.ResponseWriter, r *http.Request) {
	d.snapshotMu.RLock()
	snap := d.snapshot
	d.snapshotMu.RUnlock()

	if snap == nil {
		http.Error(w, `{"error":"no snapshot loaded"}`, http.StatusServiceUnavailable)
		return
	}

	// Track active requests
	d.activeReqs.Add(1)
	defer d.activeReqs.Add(-1)

	// Handle the request using the pipeline
	api.HandleChatCompletion(r.Context(), snap, d.runtime, d.telClient, d.pricingCatalog, w, r)
}

// messagesHandler handles Anthropic Messages API requests.
func (d *Daemon) messagesHandler(w http.ResponseWriter, r *http.Request) {
	d.snapshotMu.RLock()
	snap := d.snapshot
	d.snapshotMu.RUnlock()

	if snap == nil {
		http.Error(w, `{"error":"no snapshot loaded"}`, http.StatusServiceUnavailable)
		return
	}

	// Track active requests
	d.activeReqs.Add(1)
	defer d.activeReqs.Add(-1)

	// Handle the request using the pipeline
	api.HandleMessages(r.Context(), snap, d.runtime, d.telClient, d.pricingCatalog, w, r)
}

// responsesHandler handles OpenAI Responses API requests.
func (d *Daemon) responsesHandler(w http.ResponseWriter, r *http.Request) {
	d.snapshotMu.RLock()
	snap := d.snapshot
	d.snapshotMu.RUnlock()

	if snap == nil {
		http.Error(w, `{"error":"no snapshot loaded"}`, http.StatusServiceUnavailable)
		return
	}

	// Track active requests
	d.activeReqs.Add(1)
	defer d.activeReqs.Add(-1)

	// Handle the request using the pipeline
	api.HandleResponses(r.Context(), snap, d.runtime, d.telClient, d.pricingCatalog, w, r)
}

// Shutdown gracefully shuts down the daemon.
func (d *Daemon) Shutdown(ctx context.Context) error {
	if d.runCancel != nil {
		d.runCancel()
	}
	d.stopHealthProbes()

	// Shutdown HTTP server
	if d.httpServer != nil {
		if err := d.httpServer.Shutdown(ctx); err != nil {
			logger.Error("HTTP shutdown error", "error", err)
		}
	}

	// Close telemetry client
	if d.telClient != nil {
		if err := d.telClient.Close(); err != nil {
			logger.Error("telemetry client close error", "error", err)
		}
	}

	return nil
}

// ApplySnapshot applies a new runtime snapshot.
func (d *Daemon) ApplySnapshot(snap *snapshot.Snapshot) error {
	d.snapshotMu.Lock()

	// Validate snapshot
	if err := validateSnapshot(snap); err != nil {
		d.snapshotMu.Unlock()
		return err
	}

	d.snapshot = snap
	pricingCfg := runtimePricingConfig(snap.Pricing)
	// Push SSRF config from the new snapshot into the api package so the
	// forwarder and health probe honor the configured allowlist on every
	// snapshot swap (fix for #13: SSRF consistency).
	api.SetSSRFCheckerFromSnapshot(snap)
	if d.pricingCatalog == nil {
		d.pricingCatalog = pricinginfra.NewCatalog(pricingCfg)
		if d.runCtx != nil {
			d.pricingCatalog.Start(d.runCtx)
		}
	} else {
		d.pricingCatalog.UpdateConfig(pricingCfg)
	}
	if d.runtime != nil {
		d.runtime.ApplySnapshot(snap)
	}
	d.snapshotMu.Unlock()

	d.restartHealthProbes(snap)
	logger.Info("applied snapshot", "snapshot_id", snap.Meta.SnapshotID, "revision_id", snap.Meta.RevisionID)

	return nil
}

func (d *Daemon) recordAutoRemediation(reason string) {
	if d == nil || strings.TrimSpace(reason) == "" {
		return
	}
	d.remediationMu.Lock()
	d.lastAutoRemediationReason = reason
	d.lastAutoRemediationAt = time.Now()
	d.remediationMu.Unlock()
}

func (d *Daemon) autoRemediationForStatus() (reason string, at time.Time) {
	if d == nil {
		return "", time.Time{}
	}
	d.remediationMu.Lock()
	defer d.remediationMu.Unlock()
	return d.lastAutoRemediationReason, d.lastAutoRemediationAt
}

// GetStatus returns the current daemon status.
func (d *Daemon) GetStatus() gatewaycontrol.GetStatusResponse {
	d.snapshotMu.RLock()
	snap := d.snapshot
	d.snapshotMu.RUnlock()

	resp := gatewaycontrol.GetStatusResponse{
		Readiness:      gatewaycontrol.ReadinessStarting,
		ActiveRequests: int(d.activeReqs.Load()),
		Listener:       d.config.Listen,
		StartedAt:      d.startedAt,
		Uptime:         time.Since(d.startedAt),
	}

	if snap != nil {
		resp.Readiness = gatewaycontrol.ReadinessReady
		resp.ActiveSnapshotID = snap.Meta.SnapshotID
		if d.runtime != nil {
			resp.ProviderHealth = d.runtime.ProviderHealthSnapshot(snap)
		}
	}

	reason, at := d.autoRemediationForStatus()
	resp.LastAutoRemediationReason = reason
	resp.LastAutoRemediationAt = at

	return resp
}

// GetPricingStatus returns the live pricing catalog status.
func (d *Daemon) GetPricingStatus() gatewaycontrol.GetPricingStatusResponse {
	d.snapshotMu.RLock()
	defer d.snapshotMu.RUnlock()
	if d.pricingCatalog == nil {
		snapshot := pricinginfra.NewCatalog(core.PricingConfig{}).Snapshot()
		return pricingStatusFromSnapshot(snapshot)
	}
	return pricingStatusFromSnapshot(d.pricingCatalog.Snapshot())
}

// RefreshPricing forces a live pricing refresh.
func (d *Daemon) RefreshPricing(ctx context.Context) gatewaycontrol.RefreshPricingResponse {
	d.snapshotMu.Lock()
	defer d.snapshotMu.Unlock()
	if d.pricingCatalog == nil {
		d.pricingCatalog = pricinginfra.NewCatalog(core.PricingConfig{})
		if d.runCtx != nil {
			d.pricingCatalog.Start(d.runCtx)
		}
	}
	err := d.pricingCatalog.RefreshNow(ctx)
	status := pricingStatusFromSnapshot(d.pricingCatalog.Snapshot())
	resp := gatewaycontrol.RefreshPricingResponse{
		Refreshed: err == nil,
		Status:    status,
	}
	if err != nil {
		resp.Error = err.Error()
	}
	return resp
}

// validateSnapshot validates a snapshot.
func validateSnapshot(snap *snapshot.Snapshot) error {
	if snap.Meta.SnapshotID == "" {
		return fmt.Errorf("snapshot_id is required")
	}
	if snap.Meta.SchemaVersion != snapshot.CurrentSchemaVersion {
		return fmt.Errorf("unsupported schema version: %d", snap.Meta.SchemaVersion)
	}
	if snap.Ingress.Listen == "" {
		return fmt.Errorf("ingress.listen is required")
	}
	if len(snap.Providers) == 0 {
		return fmt.Errorf("at least one provider is required")
	}
	return nil
}

func runtimePricingConfig(cfg snapshot.PricingConfig) core.PricingConfig {
	result := core.PricingConfig{
		CachePath:              cfg.CachePath,
		RefreshIntervalMinutes: cfg.RefreshIntervalMinutes,
		RequestTimeoutMs:       cfg.RequestTimeoutMs,
	}
	result.FX = core.PricingFXConfig{
		CachePath:              cfg.FX.CachePath,
		RefreshIntervalMinutes: cfg.FX.RefreshIntervalMinutes,
	}
	result.FX.Enabled = boolPtr(cfg.FX.Enabled)
	if len(cfg.Sources) > 0 {
		result.Sources = make([]core.PricingSourceConfig, 0, len(cfg.Sources))
		for _, source := range cfg.Sources {
			result.Sources = append(result.Sources, core.PricingSourceConfig{
				ID:                     source.ID,
				Vendor:                 source.Vendor,
				URL:                    source.URL,
				Enabled:                boolPtr(source.Enabled),
				TimeoutMs:              source.TimeoutMs,
				RefreshIntervalMinutes: source.RefreshIntervalMinutes,
			})
		}
	}
	if len(cfg.ManualPrices) > 0 {
		result.ManualPrices = make([]core.PricingManualPrice, 0, len(cfg.ManualPrices))
		for _, manual := range cfg.ManualPrices {
			result.ManualPrices = append(result.ManualPrices, core.PricingManualPrice{
				Provider:         manual.Provider,
				Model:            manual.Model,
				Currency:         manual.Currency,
				InputPer1M:       manual.InputPer1M,
				CachedInputPer1M: manual.CachedInputPer1M,
				OutputPer1M:      manual.OutputPer1M,
				Enabled:          boolPtr(manual.Enabled),
				Source:           manual.Source,
			})
		}
	}
	normalized := core.Config{Pricing: result}
	normalized.Normalize()
	return normalized.Pricing
}

func pricingStatusFromSnapshot(snapshot pricinginfra.Snapshot) gatewaycontrol.GetPricingStatusResponse {
	resp := gatewaycontrol.GetPricingStatusResponse{
		SourceURL:     snapshot.SourceURL,
		UpdatedAt:     snapshot.UpdatedAt,
		LastAttemptAt: snapshot.LastAttemptAt,
		LastError:     snapshot.LastError,
		CatalogSize:   len(snapshot.Catalog),
		FX: gatewaycontrol.PricingFXSnapshot{
			Enabled:       snapshot.FX.Enabled,
			SourceURL:     snapshot.FX.SourceURL,
			BaseCurrency:  snapshot.FX.BaseCurrency,
			UpdatedAt:     snapshot.FX.UpdatedAt,
			LastAttemptAt: snapshot.FX.LastAttemptAt,
			LastError:     snapshot.FX.LastError,
			RatesToUSD:    snapshot.FX.RatesToUSD,
		},
	}
	if len(snapshot.Sources) > 0 {
		resp.Sources = make([]gatewaycontrol.PricingSourceState, 0, len(snapshot.Sources))
		for _, state := range snapshot.Sources {
			resp.Sources = append(resp.Sources, gatewaycontrol.PricingSourceState{
				ID:            state.ID,
				Vendor:        state.Vendor,
				URL:           state.URL,
				Enabled:       state.Enabled,
				Status:        state.Status,
				UpdatedAt:     state.UpdatedAt,
				LastAttemptAt: state.LastAttemptAt,
				LastError:     state.LastError,
				ModelCount:    state.ModelCount,
			})
		}
	}
	return resp
}

func boolPtr(value bool) *bool {
	return &value
}
