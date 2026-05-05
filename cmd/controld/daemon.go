package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/infra/configloader"
	"ai-model-gateway/internal/infra/logger"
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
		logger.Warn("could not initialize audit log", "error", err)
	}

	if err := d.connectGateway(d.runCtx); err != nil {
		logger.Warn("could not connect to gateway", "error", err)
	}
	if err := d.connectTelemetry(d.runCtx); err != nil {
		logger.Warn("could not connect to telemetry", "error", err)
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
		logger.Warn("could not publish initial revision", "error", err)
	}

	frontendBundle, err := api.NewAdminFrontendBundle("web/admin/dist")
	if err != nil {
		logger.Warn("frontend bundle not available, using embedded assets", "error", err)
		frontendBundle, _ = api.NewAdminFrontendBundle("")
	}
	d.frontendBundle = frontendBundle

	if d.config.ConfigPath != "" {
		if watcher, err := configloader.NewWatcher(d.config.ConfigPath, 5*time.Second); err == nil {
			watcher.OnChange(func(cfg *core.Config) {
				_ = cfg
				logger.Info("config file changed, reloading")
				if err := d.reloadWatchedConfig(); err != nil {
					logger.Error("config reload error", "error", err)
				} else {
					logger.Info("config reloaded", "path", d.config.ConfigPath)
				}
			})
			watcher.Start()
			d.configWatcher = watcher
			logger.Info("watching config file", "path", d.config.ConfigPath)
		} else {
			logger.Warn("could not watch config file", "error", err)
		}
	}

	go d.maintainGatewayConnection(d.runCtx)
	go d.maintainTelemetryConnection(d.runCtx)
	go d.pushSnapshotUntilGatewayReady(d.runCtx)

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
		logger.Info("listening", "address", d.config.Listen)
		if err := d.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server error", "error", err)
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
		logger.Info("restored publisher state", "path", d.publisherSQLiteStatePath())
		return nil
	}
	return d.seedInitialRevision()
}

func (d *Daemon) publishInitialRevision() error {
	if d.publisher == nil {
		return fmt.Errorf("publisher not initialized")
	}
	if d.currentGatewayRPC() == nil {
		logger.Warn("skipping initial publish because gateway is not connected")
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

// republishCurrentRevisionToGateway pushes the active revision to gatewayd.
// Used after the data plane reconnects (e.g. gatewayd restart) so gatewayd
// does not sit without a snapshot until the next manual publish.
func (d *Daemon) republishCurrentRevisionToGateway(reason string) {
	if d.testRepublishHook != nil {
		d.testRepublishHook(reason)
	}
	if d.publisher == nil || d.currentGatewayRPC() == nil {
		return
	}
	current, err := d.publisher.GetCurrentRevision()
	if err != nil {
		logger.Error("republish failed to get current revision", "reason", reason, "error", err)
		return
	}
	if current == nil || current.RevisionID == "" {
		logger.Warn("republish has no active revision", "reason", reason)
		return
	}
	result, err := d.publisher.Publish(current.RevisionID)
	if err != nil {
		logger.Error("republish error", "reason", reason, "error", err)
		return
	}
	if result != nil && !result.Success {
		logger.Error("republish publish failed", "reason", reason, "message", result.ErrorMessage)
		return
	}
	logger.Info("republished revision to gateway", "revision", current.RevisionID, "reason", reason)
}

// pushSnapshotUntilGatewayReady periodically re-publishes the active revision until
// gatewayd reports ReadinessReady or ctx is cancelled. Covers races where gatewayd
// starts after controld's initial publish, or transient ApplySnapshot failures.
func (d *Daemon) pushSnapshotUntilGatewayReady(ctx context.Context) {
	deadline := time.NewTimer(3 * time.Minute)
	defer deadline.Stop()
	next := time.NewTimer(0)
	defer next.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			logger.Warn("gateway still not ready after bootstrap window")
			return
		case <-next.C:
		}
		client := d.currentGatewayRPC()
		if client != nil {
			st, err := client.GetStatus()
			if err == nil && st.Readiness == gatewaycontrol.ReadinessReady {
				return
			}
			d.republishCurrentRevisionToGateway("bootstrap")
		}
		next.Reset(2 * time.Second)
	}
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
		logger.Warn("skipping watched config publish because gateway is not connected")
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
			logger.Error("gateway RPC close error", "error", err)
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
			logger.Error("telemetry RPC close error", "error", err)
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

func (d *Daemon) gatewayReadinessRepublishMinInterval() time.Duration {
	sec := d.config.GatewayReadinessRepublishMinIntervalSec
	if sec <= 0 {
		return 15 * time.Second
	}
	return time.Duration(sec) * time.Second
}

// maybeRepublishForGatewayReadiness republishes the active revision when gatewayd
// is reachable but not yet ready (e.g. no snapshot), subject to throttling.
func (d *Daemon) maybeRepublishForGatewayReadiness(st *gatewaycontrol.GetStatusResponse, now time.Time) {
	if st == nil || st.Readiness == gatewaycontrol.ReadinessReady {
		return
	}
	minGap := d.gatewayReadinessRepublishMinInterval()
	d.gwReadinessRepublishMu.Lock()
	if !d.lastGatewayReadinessRepublish.IsZero() && now.Sub(d.lastGatewayReadinessRepublish) < minGap {
		d.gwReadinessRepublishMu.Unlock()
		return
	}
	d.lastGatewayReadinessRepublish = now
	d.gwReadinessRepublishMu.Unlock()
	d.republishCurrentRevisionToGateway("gateway readiness")
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
		logger.Debug("waiting for gateway", "attempt", i+1, "max", d.rpcRetryCount())
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
		logger.Debug("waiting for telemetry", "attempt", i+1, "max", d.rpcRetryCount())
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
			st, err := client.GetStatus()
			if err == nil {
				wasDisconnected = false
				if st != nil && st.Readiness != gatewaycontrol.ReadinessReady {
					d.maybeRepublishForGatewayReadiness(st, time.Now())
				}
				continue
			} else if !wasDisconnected {
				logger.Warn("gateway connection lost", "error", err)
			}
			d.setGatewayRPC(nil)
			wasDisconnected = true
		}

		if err := d.connectGatewayOnce(); err == nil {
			logger.Info("gateway connected")
			// Always republish after a successful dial. Initial Start() may have
			// failed to connect while gatewayd was still waiting on telemetry;
			// this path runs on the maintain loop and restores snapshot delivery.
			d.republishCurrentRevisionToGateway("gateway connect")
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
				logger.Warn("telemetry connection lost", "error", err)
			}
			d.setTelemetryRPC(nil)
			wasDisconnected = true
		}

		if err := d.connectTelemetryOnce(); err == nil {
			logger.Info("telemetry connected")
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
			logger.Error("HTTP shutdown error", "error", err)
		}
	}

	d.setGatewayRPC(nil)
	d.setTelemetryRPC(nil)
	if d.benchmarkStore != nil {
		if err := d.benchmarkStore.Close(); err != nil {
			logger.Error("benchmark store close error", "error", err)
		}
	}
	if d.replayDB != nil {
		if err := d.replayDB.Close(); err != nil {
			logger.Error("replay db close error", "error", err)
		}
	}

	return nil
}
