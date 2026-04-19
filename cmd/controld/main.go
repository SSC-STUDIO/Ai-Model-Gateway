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
	"syscall"
	"time"

	"ai-model-gateway/internal/contracts"
	"ai-model-gateway/internal/control/api"
	"ai-model-gateway/internal/control/compiler"
	"ai-model-gateway/internal/control/publish"
	"ai-model-gateway/internal/core"
	authinfra "ai-model-gateway/internal/infra/auth"
	"ai-model-gateway/internal/infra/configloader"
)

const (
	Version = "2.0.0-alpha"
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
	config       Config
	transport    contracts.Transport
	httpServer   *http.Server
	gatewayRPC   *GatewayClient
	telemetryRPC *TelemetryClient
	publisher    *publish.Publisher
	compiler     *compiler.Compiler
	startedAt    time.Time
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
	// Connect to gatewayd
	if err := d.connectGateway(ctx); err != nil {
		log.Printf("[controld] warning: could not connect to gateway: %v", err)
	}

	// Connect to telemetryd
	if err := d.connectTelemetry(ctx); err != nil {
		log.Printf("[controld] warning: could not connect to telemetry: %v", err)
	}

	// Initialize compiler and publisher
	d.compiler = compiler.NewCompiler()
	d.publisher = newConfiguredPublisher(d.gatewayRPC, d.compiler, d.publisherStateStore())
	if err := d.restoreOrSeedInitialRevision(); err != nil {
		return fmt.Errorf("restore or seed initial revision: %w", err)
	}
	if err := d.publishInitialRevision(); err != nil {
		log.Printf("[controld] warning: could not publish initial revision: %v", err)
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
	if d.gatewayRPC == nil {
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

// connectGateway connects to gatewayd.
func (d *Daemon) connectGateway(ctx context.Context) error {
	retryCount := d.config.RPCRetryCount
	if retryCount <= 0 {
		retryCount = 10
	}
	retryInterval := time.Duration(d.config.RPCRetryInterval) * time.Second
	if retryInterval == 0 {
		retryInterval = time.Second
	}

	var conn contracts.Conn
	var err error
	for i := 0; i < retryCount; i++ {
		conn, err = d.transport.Dial(d.config.GatewaySocket)
		if err == nil {
			break
		}
		log.Printf("[controld] waiting for gateway... (%d/%d)", i+1, retryCount)
		time.Sleep(retryInterval)
	}
	if err != nil {
		return fmt.Errorf("connect to gateway: %w", err)
	}

	d.gatewayRPC = NewGatewayClient(conn)
	return nil
}

// connectTelemetry connects to telemetryd.
func (d *Daemon) connectTelemetry(ctx context.Context) error {
	retryCount := d.config.RPCRetryCount
	if retryCount <= 0 {
		retryCount = 10
	}
	retryInterval := time.Duration(d.config.RPCRetryInterval) * time.Second
	if retryInterval == 0 {
		retryInterval = time.Second
	}

	var conn contracts.Conn
	var err error
	for i := 0; i < retryCount; i++ {
		conn, err = d.transport.Dial(d.config.TelemetrySocket)
		if err == nil {
			break
		}
		log.Printf("[controld] waiting for telemetry... (%d/%d)", i+1, retryCount)
		time.Sleep(retryInterval)
	}
	if err != nil {
		return fmt.Errorf("connect to telemetry: %w", err)
	}

	d.telemetryRPC = NewTelemetryClient(conn)
	return nil
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
		ConfigQuery:     d.publisher,
		ConfigCommands:  d.publisher,
		Version:         Version,
		StartedAt:       d.startedAt,
		AdminMiddleware: d.adminAuthMiddleware(),
	}
	if d.telemetryRPC != nil {
		deps.TelemetryRPC = d.telemetryRPC
	}
	if d.gatewayRPC != nil {
		deps.GatewayRPC = d.gatewayRPC
	}
	api.Mount(mux, deps)

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

			if handled := d.maybeBootstrapAdminCookie(w, r, authenticator); handled {
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
			if !canAccessAdminRoute(info.Role, r.Method) {
				writeAdminAuthError(w, r, http.StatusForbidden, "insufficient admin privileges")
				return
			}
			next.ServeHTTP(w, r)
		})
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

func (d *Daemon) maybeBootstrapAdminCookie(w http.ResponseWriter, r *http.Request, authenticator *authinfra.Authenticator) bool {
	if authenticator == nil || r.URL == nil || !strings.HasPrefix(r.URL.Path, "/admin") {
		return false
	}
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		return false
	}
	if err := authenticator.Login(w, token); err != nil {
		writeAdminAuthError(w, r, http.StatusUnauthorized, "invalid admin token")
		return true
	}

	redirectURL := *r.URL
	query := redirectURL.Query()
	query.Del("token")
	redirectURL.RawQuery = query.Encode()
	http.Redirect(w, r, redirectURL.String(), http.StatusSeeOther)
	return true
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
	_, _ = w.Write([]byte("<!DOCTYPE html><html><body><h1>Admin Authentication Required</h1><p>" + message + "</p><p>Open /admin to sign in, open /admin?token=&lt;admin-token&gt; once to bootstrap a session, or use a Bearer token for API access.</p></body></html>"))
}

// healthHandler handles health check requests.
func (d *Daemon) healthHandler(w http.ResponseWriter, r *http.Request) {
	status := "healthy"

	// Check gateway connection
	if d.gatewayRPC == nil {
		status = "degraded"
	}

	// Check telemetry connection
	if d.telemetryRPC == nil {
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
	// Shutdown HTTP server
	if d.httpServer != nil {
		if err := d.httpServer.Shutdown(ctx); err != nil {
			log.Printf("[controld] HTTP shutdown error: %v", err)
		}
	}

	// Close gateway RPC
	if d.gatewayRPC != nil {
		if err := d.gatewayRPC.Close(); err != nil {
			log.Printf("[controld] gateway RPC close error: %v", err)
		}
	}

	// Close telemetry RPC
	if d.telemetryRPC != nil {
		if err := d.telemetryRPC.Close(); err != nil {
			log.Printf("[controld] telemetry RPC close error: %v", err)
		}
	}

	return nil
}
