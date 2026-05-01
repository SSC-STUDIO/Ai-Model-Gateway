package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ai-model-gateway/internal/contracts"
	"ai-model-gateway/internal/control/api"
	"ai-model-gateway/internal/control/audit"
	"ai-model-gateway/internal/control/benchmarking"
	"ai-model-gateway/internal/control/compiler"
	"ai-model-gateway/internal/control/publish"
	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/infra/configloader"
)

// NewDaemon creates a new controld daemon.
func NewDaemon(cfg Config) (*Daemon, error) {
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

	if store, err := audit.NewStore(filepath.Join(d.config.DataDir, "audit.jsonl")); err == nil {
		d.auditStore = store
		_ = d.auditStore.Record(context.Background(), audit.Event{
			Actor:    "system",
			Action:   "daemon.start",
			Resource: "controld",
			Success:  true,
			Details: map[string]any{
				"version": Version,
				"listen":  d.config.Listen,
			},
		})
	} else {
		log.Printf("[controld] warning: could not initialize audit log: %v", err)
	}

	if err := d.connectGateway(d.runCtx); err != nil {
		log.Printf("[controld] warning: could not connect to gateway: %v", err)
	}
	if err := d.connectTelemetry(d.runCtx); err != nil {
		log.Printf("[controld] warning: could not connect to telemetry: %v", err)
	}

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

	frontendBundle, err := api.NewAdminFrontendBundle("web/admin/dist")
	if err != nil {
		log.Printf("[controld] warning: %v, using embedded assets", err)
		frontendBundle, _ = api.NewAdminFrontendBundle("")
	}
	d.frontendBundle = frontendBundle

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

// Shutdown gracefully shuts down the daemon.
func (d *Daemon) Shutdown(ctx context.Context) error {
	if d.runCancel != nil {
		d.runCancel()
	}

	if d.configWatcher != nil {
		d.configWatcher.Stop()
	}

	if d.frontendBundle != nil {
		d.frontendBundle.Close()
	}

	if d.httpServer != nil {
		if err := d.httpServer.Shutdown(ctx); err != nil {
			log.Printf("[controld] HTTP shutdown error: %v", err)
		}
	}

	d.setGatewayRPC(nil)
	d.setTelemetryRPC(nil)
	if d.benchmarkStore != nil {
		if err := d.benchmarkStore.Close(); err != nil {
			log.Printf("[controld] benchmark store close error: %v", err)
		}
	}
	if d.replayDB != nil {
		if err := d.replayDB.Close(); err != nil {
			log.Printf("[controld] replay db close error: %v", err)
		}
	}

	return nil
}
