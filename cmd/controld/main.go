// Package main is the entry point for controld - the control plane daemon.
// controld provides the Admin API, Admin UI, configuration management,
// and publishes runtime snapshots to gatewayd.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"ai-model-gateway/internal/contracts"
	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/control/api"
	"ai-model-gateway/internal/control/benchmarking"
	"ai-model-gateway/internal/control/compiler"
	"ai-model-gateway/internal/control/publish"
	"ai-model-gateway/internal/core"
	authinfra "ai-model-gateway/internal/infra/auth"
	"ai-model-gateway/internal/infra/configloader"
)

const (
	Version = "1.2.0"
)

// Config is the bootstrap configuration for controld.
type Config struct {
	// Listen is the address to listen on for admin requests.
	Listen string `json:"listen"`

	// GatewaySocket is the path to the gatewayd IPC socket.
	GatewaySocket string `json:"gateway_socket"`

	// TelemetrySocket is the path to the telemetryd IPC socket.
	TelemetrySocket string `json:"telemetry_socket"`

	// DataDir is the directory for control data.
	DataDir string `json:"data_dir"`

	// ConfigPath is the path to the operator authoring YAML.
	ConfigPath string `json:"config_path"`

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

// Daemon represents the controld daemon.
type Daemon struct {
	config         Config
	transport      contracts.Transport
	httpServer     *http.Server
	gatewayRPC     *GatewayClient
	gatewayMu      sync.RWMutex
	telemetryRPC   *TelemetryClient
	telemetryMu    sync.RWMutex
	publisher      *publish.Publisher
	compiler       *compiler.Compiler
	benchmarkStore *benchmarking.Store
	benchmarkSvc   *benchmarking.Service
	startedAt      time.Time
	runCtx         context.Context
	runCancel      context.CancelFunc
	frontendBundle *api.AdminFrontendBundle
	configWatcher  *configloader.Watcher
}

type configCommandsAdapter struct {
	publisher *publish.Publisher
	reloadFn  func() (*publish.PublishResult, error)
}

type publisherGatewayAdapter struct {
	daemon *Daemon
}

func (a publisherGatewayAdapter) ApplySnapshot(req gatewaycontrol.ApplySnapshotRequest) (*gatewaycontrol.ApplySnapshotResponse, error) {
	client := a.daemon.currentGatewayRPC()
	if client == nil {
		return nil, fmt.Errorf("gateway not connected")
	}
	return client.ApplySnapshot(req)
}

func (a publisherGatewayAdapter) GetStatus() (*gatewaycontrol.GetStatusResponse, error) {
	client := a.daemon.currentGatewayRPC()
	if client == nil {
		return nil, fmt.Errorf("gateway not connected")
	}
	return client.GetStatus()
}

type benchmarkGatewayAdapter struct {
	daemon *Daemon
}

func (a benchmarkGatewayAdapter) RunBenchmarkCase(req gatewaycontrol.RunBenchmarkCaseRequest) (*gatewaycontrol.RunBenchmarkCaseResponse, error) {
	client := a.daemon.currentGatewayRPC()
	if client == nil {
		return nil, fmt.Errorf("gateway not connected")
	}
	return client.RunBenchmarkCase(req)
}

func (a configCommandsAdapter) Publish(revisionID string) (*publish.PublishResult, error) {
	return a.publisher.Publish(revisionID)
}

func (a configCommandsAdapter) Rollback(revisionID string) (*publish.PublishResult, error) {
	return a.publisher.Rollback(revisionID)
}

func (a configCommandsAdapter) ValidateConfig(cfg interface{}) (*publish.ConfigValidationResult, error) {
	return a.publisher.ValidateConfig(cfg)
}

func (a configCommandsAdapter) UpdateConfig(cfg interface{}, description string) (*publish.PublishResult, error) {
	return a.publisher.UpdateConfig(cfg, description)
}

func (a configCommandsAdapter) ReloadConfig() (*publish.PublishResult, error) {
	if a.reloadFn == nil {
		return nil, fmt.Errorf("reload is not configured")
	}
	return a.reloadFn()
}

func main() {
	// Parse flags
	configPath := flag.String("config", "", "Path to bootstrap config file")
	listen := flag.String("listen", "", "Address to listen on")
	gatewaySocket := flag.String("gateway", "", "Gateway socket path")
	telemetrySocket := flag.String("telemetry", "", "Telemetry socket path")
	dataDir := flag.String("data-dir", "", "Control-plane data directory")
	authoringConfig := flag.String("authoring-config", "", "Path to operator authoring YAML")
	showVersion := flag.Bool("version", false, "Show version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("controld version %s (%s/%s)\n", Version, runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	// Load config
	cfg := loadConfig(*configPath, *listen, *gatewaySocket, *telemetrySocket, *dataDir, *authoringConfig)

	// Create daemon
	d, err := NewDaemon(cfg)
	if err != nil {
		log.Fatalf("[controld] failed to create daemon: %v", err)
	}

	// Setup signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Start daemon
	if err := d.Start(ctx); err != nil {
		log.Fatalf("[controld] failed to start: %v", err)
	}

	log.Printf("[controld] started on %s", cfg.Listen)

	// Wait for shutdown
	<-ctx.Done()
	log.Printf("[controld] shutting down...")

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := d.Shutdown(shutdownCtx); err != nil {
		log.Printf("[controld] shutdown error: %v", err)
	}

	log.Printf("[controld] stopped")
}

// loadConfig loads the bootstrap configuration.
func loadConfig(configPath, listen, gatewaySocket, telemetrySocket, dataDir, authoringConfig string) Config {
	cfg := Config{
		Listen:     "127.0.0.1:18081",
		DataDir:    "data/control",
		ConfigPath: "configs/config.yaml",
		LogLevel:   "info",
	}

	// Load from file if specified
	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			log.Printf("[controld] warning: could not read config file: %v", err)
		} else if err := json.Unmarshal(data, &cfg); err != nil {
			log.Printf("[controld] warning: could not parse config file: %v", err)
		}
	}

	// Override with flags
	if listen != "" {
		cfg.Listen = listen
	}
	if gatewaySocket != "" {
		cfg.GatewaySocket = gatewaySocket
	}
	if telemetrySocket != "" {
		cfg.TelemetrySocket = telemetrySocket
	}
	if dataDir != "" {
		cfg.DataDir = dataDir
	}
	if authoringConfig != "" {
		cfg.ConfigPath = authoringConfig
	}

	// Set defaults based on platform
	if cfg.GatewaySocket == "" {
		cfg.GatewaySocket = defaultSocketPath("gateway-control")
	}
	if cfg.TelemetrySocket == "" {
		cfg.TelemetrySocket = defaultSocketPath("telemetry-query")
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

// NewDaemon creates a new controld daemon.
func NewDaemon(cfg Config) (*Daemon, error) {
	// Ensure data directory exists
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	return &Daemon{
		config:    cfg,
		transport: contracts.DefaultTransport,
		startedAt: time.Now(),
	}, nil
}

func newConfiguredPublisher(gateway publish.GatewayRPC, comp *compiler.Compiler, stateStore publish.StateStore) *publish.Publisher {
	publisher := publish.NewPublisher(gateway, comp)
	publisher.SetStateStore(stateStore)
	if comp != nil {
		comp.SetRevisionConfigSource(publisher)
	}
	return publisher
}

// Start starts the daemon.
func (d *Daemon) Start(ctx context.Context) error {
	d.runCtx, d.runCancel = context.WithCancel(ctx)

	// Connect to gatewayd
	if err := d.connectGateway(d.runCtx); err != nil {
		log.Printf("[controld] warning: could not connect to gateway: %v", err)
	}

	// Connect to telemetryd
	if err := d.connectTelemetry(d.runCtx); err != nil {
		log.Printf("[controld] warning: could not connect to telemetry: %v", err)
	}

	// Initialize compiler and publisher
	d.compiler = compiler.NewCompiler()
	d.publisher = newConfiguredPublisher(publisherGatewayAdapter{daemon: d}, d.compiler, d.publisherStateStore())
	benchmarkStore, err := benchmarking.NewStore(filepath.Join(d.config.DataDir, "benchmark.db"))
	if err != nil {
		return fmt.Errorf("initialize benchmark store: %w", err)
	}
	d.benchmarkStore = benchmarkStore
	d.benchmarkSvc = benchmarking.NewService(benchmarkStore, d.publisher, benchmarkGatewayAdapter{daemon: d})
	if err := d.restoreOrSeedInitialRevision(); err != nil {
		return fmt.Errorf("restore or seed initial revision: %w", err)
	}
	if err := d.publishInitialRevision(); err != nil {
		log.Printf("[controld] warning: could not publish initial revision: %v", err)
	}

	// Initialize frontend bundle (supports dev mode hot reload)
	frontendBundle, err := api.NewAdminFrontendBundle("web/admin/dist")
	if err != nil {
		log.Printf("[controld] warning: %v, using embedded assets", err)
		frontendBundle, _ = api.NewAdminFrontendBundle("")
	}
	d.frontendBundle = frontendBundle

	// Start config file watcher for hot reload
	if d.config.ConfigPath != "" {
		if watcher, err := configloader.NewWatcher(d.config.ConfigPath, 5*time.Second); err == nil {
			watcher.OnChange(func(cfg *core.Config) {
				_ = cfg
				log.Printf("[controld] config file changed, reloading...")
				if err := d.reloadWatchedConfig(); err != nil {
					log.Printf("[controld] config reload error: %v", err)
				} else {
					log.Printf("[controld] config reloaded from %s", d.config.ConfigPath)
				}
			})
			watcher.Start()
			d.configWatcher = watcher
			log.Printf("[controld] watching config file: %s", d.config.ConfigPath)
		} else {
			log.Printf("[controld] warning: could not watch config file: %v", err)
		}
	}

	go d.maintainGatewayConnection(d.runCtx)
	go d.maintainTelemetryConnection(d.runCtx)

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
		log.Printf("[controld] listening on %s", d.config.Listen)
		if err := d.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[controld] HTTP server error: %v", err)
		}
	}()

	return nil
}

func loadInitialRevision(configPath string) (publish.Revision, error) {
	cleanPath := filepath.Clean(configPath)

	cfg, err := configloader.LoadFromFile(cleanPath)
	if err != nil {
		return publish.Revision{}, fmt.Errorf("load config %s: %w", cleanPath, err)
	}

	revisionID, err := revisionIDFromConfig(cfg)
	if err != nil {
		return publish.Revision{}, err
	}

	createdAt := time.Now().UTC()
	if info, err := os.Stat(cleanPath); err == nil {
		createdAt = info.ModTime().UTC()
	}

	return publish.Revision{
		RevisionID:  revisionID,
		CreatedAt:   createdAt,
		CreatedBy:   "system",
		Description: fmt.Sprintf("seeded from %s", cleanPath),
		Config:      cfg,
	}, nil
}

func (d *Daemon) seedInitialRevision() error {
	revision, err := loadInitialRevision(d.config.ConfigPath)
	if err != nil {
		return err
	}

	if err := d.publisher.ReplaceRevisions([]publish.Revision{revision}, revision.RevisionID); err != nil {
		return err
	}
	return nil
}

func (d *Daemon) restoreOrSeedInitialRevision() error {
	if d.publisher == nil {
		return fmt.Errorf("publisher not initialized")
	}

	loaded, err := d.publisher.LoadState()
	if err != nil {
		return err
	}
	if loaded {
		log.Printf("[controld] restored publisher state from %s", d.publisherSQLiteStatePath())
		return nil
	}
	return d.seedInitialRevision()
}

func (d *Daemon) publishInitialRevision() error {
	if d.publisher == nil {
		return fmt.Errorf("publisher not initialized")
	}
	if d.currentGatewayRPC() == nil {
		log.Printf("[controld] warning: skipping initial publish because gateway is not connected")
		return nil
	}

	current, err := d.publisher.GetCurrentRevision()
	if err != nil {
		return err
	}
	if current == nil {
		return fmt.Errorf("no active revision available for initial publish")
	}

	result, err := d.publisher.Publish(current.RevisionID)
	if err != nil {
		return err
	}
	if result != nil && !result.Success {
		return fmt.Errorf("publish %s failed: %s", current.RevisionID, result.ErrorMessage)
	}
	return nil
}

func (d *Daemon) reloadWatchedConfig() error {
	_, err := d.reloadConfigFromSource()
	return err
}

func (d *Daemon) reloadConfigFromSource() (*publish.PublishResult, error) {
	if strings.TrimSpace(d.config.ConfigPath) == "" {
		return nil, fmt.Errorf("config path is not configured")
	}
	revision, err := loadInitialRevision(d.config.ConfigPath)
	if err != nil {
		return nil, err
	}
	return d.applyWatchedRevision(revision)
}

func (d *Daemon) applyWatchedRevision(revision publish.Revision) (*publish.PublishResult, error) {
	if d.publisher == nil {
		return nil, fmt.Errorf("publisher not initialized")
	}
	if err := d.publisher.UpsertRevision(revision, true); err != nil {
		return nil, err
	}
	if d.currentGatewayRPC() == nil {
		log.Printf("[controld] warning: skipping watched config publish because gateway is not connected")
		return &publish.PublishResult{
			Success:    true,
			RevisionID: revision.RevisionID,
		}, nil
	}
	result, err := d.publisher.Publish(revision.RevisionID)
	if err != nil {
		return nil, err
	}
	if result != nil && !result.Success {
		return nil, fmt.Errorf("publish %s failed: %s", revision.RevisionID, result.ErrorMessage)
	}
	return result, nil
}

func revisionIDFromConfig(cfg *core.Config) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("config is nil")
	}

	normalized := *cfg
	normalized.Normalize()

	data, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("marshal config revision: %w", err)
	}

	sum := sha256.Sum256(data)
	return "rev_" + hex.EncodeToString(sum[:12]), nil
}

func (d *Daemon) publisherSQLiteStatePath() string {
	return filepath.Join(d.config.DataDir, "publisher-state.db")
}

func (d *Daemon) legacyPublisherStatePath() string {
	return filepath.Join(d.config.DataDir, "publisher-state.json")
}

func (d *Daemon) publisherStateStore() publish.StateStore {
	return publish.NewMigratingStateStore(
		publish.NewSQLiteStateStore(d.publisherSQLiteStatePath()),
		publish.NewFileStateStore(d.legacyPublisherStatePath()),
	)
}

func (d *Daemon) currentGatewayRPC() *GatewayClient {
	d.gatewayMu.RLock()
	defer d.gatewayMu.RUnlock()
	return d.gatewayRPC
}

func (d *Daemon) currentTelemetryRPC() *TelemetryClient {
	d.telemetryMu.RLock()
	defer d.telemetryMu.RUnlock()
	return d.telemetryRPC
}

func (d *Daemon) setGatewayRPC(client *GatewayClient) {
	d.gatewayMu.Lock()
	previous := d.gatewayRPC
	d.gatewayRPC = client
	d.gatewayMu.Unlock()

	if previous != nil && previous != client {
		if err := previous.Close(); err != nil {
			log.Printf("[controld] gateway RPC close error: %v", err)
		}
	}
}

func (d *Daemon) setTelemetryRPC(client *TelemetryClient) {
	d.telemetryMu.Lock()
	previous := d.telemetryRPC
	d.telemetryRPC = client
	d.telemetryMu.Unlock()

	if previous != nil && previous != client {
		if err := previous.Close(); err != nil {
			log.Printf("[controld] telemetry RPC close error: %v", err)
		}
	}
}

func (d *Daemon) rpcRetryInterval() time.Duration {
	retryInterval := time.Duration(d.config.RPCRetryInterval) * time.Second
	if retryInterval == 0 {
		return time.Second
	}
	return retryInterval
}

func (d *Daemon) rpcRetryCount() int {
	retryCount := d.config.RPCRetryCount
	if retryCount <= 0 {
		return 10
	}
	return retryCount
}

func sleepWithContext(ctx context.Context, wait time.Duration) bool {
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (d *Daemon) connectGatewayOnce() error {
	conn, err := d.transport.Dial(d.config.GatewaySocket)
	if err != nil {
		return err
	}
	d.setGatewayRPC(NewGatewayClient(conn))
	return nil
}

func (d *Daemon) connectTelemetryOnce() error {
	conn, err := d.transport.Dial(d.config.TelemetrySocket)
	if err != nil {
		return err
	}
	d.setTelemetryRPC(NewTelemetryClient(conn))
	return nil
}

// connectGateway connects to gatewayd.
func (d *Daemon) connectGateway(ctx context.Context) error {
	var err error
	for i := 0; i < d.rpcRetryCount(); i++ {
		if err = d.connectGatewayOnce(); err == nil {
			return nil
		}
		log.Printf("[controld] waiting for gateway... (%d/%d)", i+1, d.rpcRetryCount())
		if !sleepWithContext(ctx, d.rpcRetryInterval()) {
			return ctx.Err()
		}
	}
	return fmt.Errorf("connect to gateway: %w", err)
}

// connectTelemetry connects to telemetryd.
func (d *Daemon) connectTelemetry(ctx context.Context) error {
	var err error
	for i := 0; i < d.rpcRetryCount(); i++ {
		if err = d.connectTelemetryOnce(); err == nil {
			return nil
		}
		log.Printf("[controld] waiting for telemetry... (%d/%d)", i+1, d.rpcRetryCount())
		if !sleepWithContext(ctx, d.rpcRetryInterval()) {
			return ctx.Err()
		}
	}
	return fmt.Errorf("connect to telemetry: %w", err)
}

func (d *Daemon) maintainGatewayConnection(ctx context.Context) {
	ticker := time.NewTicker(d.rpcRetryInterval())
	defer ticker.Stop()

	wasDisconnected := d.currentGatewayRPC() == nil
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		client := d.currentGatewayRPC()
		if client != nil {
			if _, err := client.GetStatus(); err == nil {
				wasDisconnected = false
				continue
			} else if !wasDisconnected {
				log.Printf("[controld] gateway connection lost: %v", err)
			}
			d.setGatewayRPC(nil)
			wasDisconnected = true
		}

		if err := d.connectGatewayOnce(); err == nil {
			log.Printf("[controld] gateway connected")
			wasDisconnected = false
		}
	}
}

func (d *Daemon) maintainTelemetryConnection(ctx context.Context) {
	ticker := time.NewTicker(d.rpcRetryInterval())
	defer ticker.Stop()

	wasDisconnected := d.currentTelemetryRPC() == nil
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		client := d.currentTelemetryRPC()
		if client != nil {
			if _, err := client.Ping(); err == nil {
				wasDisconnected = false
				continue
			} else if !wasDisconnected {
				log.Printf("[controld] telemetry connection lost: %v", err)
			}
			d.setTelemetryRPC(nil)
			wasDisconnected = true
		}

		if err := d.connectTelemetryOnce(); err == nil {
			log.Printf("[controld] telemetry connected")
			wasDisconnected = false
		}
	}
}

// createHandler creates the HTTP handler.
func (d *Daemon) createHandler() http.Handler {
	mux := http.NewServeMux()

	// Health endpoint
	mux.HandleFunc("/-/health", d.healthHandler)
	mux.HandleFunc("/admin/login", d.adminLoginPageHandler)
	mux.HandleFunc("/admin/logout", d.adminBrowserLogoutHandler)
	mux.HandleFunc("/api/admin/login", d.adminLoginAPIHandler)
	mux.HandleFunc("/api/admin/logout", d.adminLogoutAPIHandler)
	mux.HandleFunc("/api/admin/session", d.adminSessionAPIHandler)

	// Admin API
	deps := api.Deps{
		ConfigQuery: d.publisher,
		ConfigCommands: configCommandsAdapter{
			publisher: d.publisher,
			reloadFn:  d.reloadConfigFromSource,
		},
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

			// Same-origin check for cookie-authenticated write requests
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
	// Has session cookie (name is "aigw" from authinfra package)
	for _, c := range r.Cookies() {
		if c.Name == "aigw" {
			return true
		}
	}
	return false
}

// isSameOriginWriteRequired returns true for paths that require same-origin validation.
func isSameOriginWriteRequired(path, method string) bool {
	// Read operations don't require same-origin check
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		return false
	}
	// Write operations on admin API require same-origin check
	return strings.HasPrefix(path, "/api/admin/config") ||
		strings.HasPrefix(path, "/api/admin/upstreams") ||
		strings.HasPrefix(path, "/api/admin/pricing/refresh") ||
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

	// Neither header present - reject for security
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
// It validates both host and scheme (http/https).
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
		writeAdminAuthError(w, r, http.StatusUnauthorized, "invalid token")
		return
	}
	if err := authenticator.Login(w, req.Token); err != nil {
		writeAdminAuthError(w, r, http.StatusUnauthorized, "invalid token")
		return
	}
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

	// Check gateway connection
	if d.currentGatewayRPC() == nil {
		status = "degraded"
	}

	// Check telemetry connection
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

// Shutdown gracefully shuts down the daemon.
func (d *Daemon) Shutdown(ctx context.Context) error {
	if d.runCancel != nil {
		d.runCancel()
	}

	// Stop config watcher
	if d.configWatcher != nil {
		d.configWatcher.Stop()
	}

	// Close frontend bundle (stops file watcher)
	if d.frontendBundle != nil {
		d.frontendBundle.Close()
	}

	// Shutdown HTTP server
	if d.httpServer != nil {
		if err := d.httpServer.Shutdown(ctx); err != nil {
			log.Printf("[controld] HTTP shutdown error: %v", err)
		}
	}

	// Close gateway RPC
	d.setGatewayRPC(nil)

	// Close telemetry RPC
	d.setTelemetryRPC(nil)
	if d.benchmarkStore != nil {
		if err := d.benchmarkStore.Close(); err != nil {
			log.Printf("[controld] benchmark store close error: %v", err)
		}
	}

	return nil
}
