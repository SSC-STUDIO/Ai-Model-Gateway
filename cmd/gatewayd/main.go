// Package main is the entry point for gatewayd - the data plane daemon.
// gatewayd serves inference requests using compiled runtime snapshots.
// It does not read YAML configuration directly - all configuration
// comes from snapshots applied via IPC from controld.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"ai-model-gateway/internal/contracts"
	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/gateway/api"
	"ai-model-gateway/internal/gateway/snapshot"
	"ai-model-gateway/internal/gateway/telemetry"
)

const (
	Version    = "1.2.0"
	modelOwner = "ai-model-gateway"
)

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
	rpcServer         *RPCServer
	telClient         *telemetry.Client
	healthHTTP        *http.Client
	runtime           *api.RuntimeState
	snapshot          *snapshot.Snapshot
	snapshotMu        sync.RWMutex
	runCtx            context.Context
	runCancel         context.CancelFunc
	healthProbeMu     sync.Mutex
	healthProbeCancel context.CancelFunc
	healthProbeDone   chan struct{}
	startedAt         time.Time
	activeReqs        atomic.Int64
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

	if *showVersion {
		fmt.Printf("gatewayd version %s (%s/%s)\n", Version, runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	// Load config
	cfg := loadConfig(*configPath, *listen, *controlSocket, *telemetrySocket, *dataDir)

	// Create daemon
	d, err := NewDaemon(cfg)
	if err != nil {
		log.Fatalf("[gatewayd] failed to create daemon: %v", err)
	}

	// Setup signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Start daemon
	if err := d.Start(ctx); err != nil {
		log.Fatalf("[gatewayd] failed to start: %v", err)
	}

	log.Printf("[gatewayd] started on %s", cfg.Listen)

	// Wait for shutdown
	<-ctx.Done()
	log.Printf("[gatewayd] shutting down...")

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := d.Shutdown(shutdownCtx); err != nil {
		log.Printf("[gatewayd] shutdown error: %v", err)
	}

	log.Printf("[gatewayd] stopped")
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
			log.Printf("[gatewayd] warning: could not read config file: %v", err)
		} else if err := json.Unmarshal(data, &cfg); err != nil {
			log.Printf("[gatewayd] warning: could not parse config file: %v", err)
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

	// Connect to telemetry plane
	if err := d.connectTelemetry(d.runCtx); err != nil {
		log.Printf("[gatewayd] warning: could not connect to telemetry: %v", err)
		// Continue without telemetry - it's not critical
	}

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

	go func() {
		log.Printf("[gatewayd] listening on %s", d.config.Listen)
		if err := d.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[gatewayd] HTTP server error: %v", err)
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
					log.Printf("[gatewayd] accept error: %v", err)
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
		log.Printf("[gatewayd] waiting for telemetry... (%d/%d)", i+1, retryCount)
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
	d.telClient = telClient

	return nil
}

// createHandler creates the HTTP handler.
func (d *Daemon) createHandler() http.Handler {
	mux := http.NewServeMux()

	// Health endpoint
	mux.HandleFunc("/-/health", d.healthHandler)

	// Models endpoint
	mux.HandleFunc("/v1/models", d.modelsHandler)

	// Chat completions endpoint
	mux.HandleFunc("/v1/chat/completions", d.chatCompletionsHandler)
	// Anthropic Messages API endpoint
	mux.HandleFunc("/v1/messages", d.messagesHandler)

	return mux
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
					providerInfo := map[string]interface{}{
						"provider_id": p.ProviderID,
						"enabled":     p.ExecutionPolicy.Enabled,
					}
					// Add health details if available
					if health, ok := providerHealth[p.ProviderID]; ok {
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
	json.NewEncoder(w).Encode(resp)
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
	json.NewEncoder(w).Encode(resp)
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
	api.HandleChatCompletion(r.Context(), snap, d.runtime, d.telClient, w, r)
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
	api.HandleMessages(r.Context(), snap, d.runtime, d.telClient, w, r)
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
			log.Printf("[gatewayd] HTTP shutdown error: %v", err)
		}
	}

	// Close telemetry client
	if d.telClient != nil {
		if err := d.telClient.Close(); err != nil {
			log.Printf("[gatewayd] telemetry client close error: %v", err)
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
	if d.runtime != nil {
		d.runtime.ApplySnapshot(snap)
	}
	d.snapshotMu.Unlock()

	d.restartHealthProbes(snap)
	log.Printf("[gatewayd] applied snapshot %s (revision %s)", snap.Meta.SnapshotID, snap.Meta.RevisionID)

	return nil
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
