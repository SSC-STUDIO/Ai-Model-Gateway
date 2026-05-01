// Package main is the entry point for telemetryd - the telemetry plane daemon.
// telemetryd receives telemetry events from gatewayd, maintains an append-only
// event log, runs projection workers, and provides query RPC for controld.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"ai-model-gateway/internal/contracts"
	"ai-model-gateway/internal/contracts/telemetryingest"
	"ai-model-gateway/internal/telemetry/eventlog"
	"ai-model-gateway/internal/telemetry/project"
	"ai-model-gateway/internal/telemetry/query"
	"ai-model-gateway/internal/version"
)

const (
	Version = version.ProductVersion
)

// Config is the bootstrap configuration for telemetryd.
type Config struct {
	// IngestSocket is the path to the ingest IPC socket.
	IngestSocket string `json:"ingest_socket"`

	// QuerySocket is the path to the query IPC socket.
	QuerySocket string `json:"query_socket"`

	// DataDir is the directory for telemetry data.
	DataDir string `json:"data_dir"`

	// EventLogPath is the path to the event log database.
	EventLogPath string `json:"event_log_path"`

	// QueryStorePath is the path to the query store database.
	QueryStorePath string `json:"query_store_path"`

	// RetentionDays is the retention period in days.
	RetentionDays int `json:"retention_days"`

	// LogLevel is the logging level.
	LogLevel string `json:"log_level"`
}

// Daemon represents the telemetryd daemon.
type Daemon struct {
	config     Config
	transport  contracts.Transport
	ingestRPC  *IngestRPCServer
	queryRPC   *QueryRPCServer
	eventLog   *eventlog.EventLog
	queryStore *query.Store
	projector  *project.Projector
	startedAt  time.Time
}

func main() {
	// Parse flags
	configPath := flag.String("config", "", "Path to bootstrap config file")
	ingestSocket := flag.String("ingest", "", "Ingest socket path")
	querySocket := flag.String("query", "", "Query socket path")
	dataDir := flag.String("data-dir", "", "Telemetry data directory")
	showVersion := flag.Bool("version", false, "Show version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("telemetryd version %s (%s/%s)\n", Version, runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	// Load config
	cfg := loadConfig(*configPath, *ingestSocket, *querySocket, *dataDir)

	// Create daemon
	d, err := NewDaemon(cfg)
	if err != nil {
		log.Fatalf("[telemetryd] failed to create daemon: %v", err)
	}

	// Setup signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Start daemon
	if err := d.Start(ctx); err != nil {
		log.Fatalf("[telemetryd] failed to start: %v", err)
	}

	log.Printf("[telemetryd] started")

	// Wait for shutdown
	<-ctx.Done()
	log.Printf("[telemetryd] shutting down...")

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := d.Shutdown(shutdownCtx); err != nil {
		log.Printf("[telemetryd] shutdown error: %v", err)
	}

	log.Printf("[telemetryd] stopped")
}

// loadConfig loads the bootstrap configuration.
func loadConfig(configPath, ingestSocket, querySocket, dataDir string) Config {
	cfg := Config{
		DataDir:       "data/telemetry",
		RetentionDays: 30,
		LogLevel:      "info",
	}

	// Load from file if specified
	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			log.Printf("[telemetryd] warning: could not read config file: %v", err)
		} else if err := json.Unmarshal(data, &cfg); err != nil {
			log.Printf("[telemetryd] warning: could not parse config file: %v", err)
		}
	}

	// Override with flags
	if ingestSocket != "" {
		cfg.IngestSocket = ingestSocket
	}
	if querySocket != "" {
		cfg.QuerySocket = querySocket
	}
	if dataDir != "" {
		cfg.DataDir = dataDir
	}

	// Set defaults based on platform
	if cfg.IngestSocket == "" {
		cfg.IngestSocket = defaultSocketPath("telemetry-ingest")
	}
	if cfg.QuerySocket == "" {
		cfg.QuerySocket = defaultSocketPath("telemetry-query")
	}
	if cfg.EventLogPath == "" {
		cfg.EventLogPath = filepath.Join(cfg.DataDir, "events.db")
	}
	if cfg.QueryStorePath == "" {
		cfg.QueryStorePath = filepath.Join(cfg.DataDir, "query.db")
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

// NewDaemon creates a new telemetryd daemon.
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

// Start starts the daemon.
func (d *Daemon) Start(ctx context.Context) error {
	// Initialize event log
	eventLog, err := eventlog.New(d.config.EventLogPath)
	if err != nil {
		return fmt.Errorf("create event log: %w", err)
	}
	d.eventLog = eventLog

	// Initialize query store
	queryStore, err := query.NewStore(d.config.QueryStorePath)
	if err != nil {
		return fmt.Errorf("create query store: %w", err)
	}
	d.queryStore = queryStore

	// Initialize projector
	d.projector = project.NewProjector(eventLog, queryStore)
	go d.projector.Run(ctx)

	// Start ingest RPC server
	d.ingestRPC = NewIngestRPCServer(d)
	if err := d.startIngestRPC(ctx); err != nil {
		return fmt.Errorf("start ingest RPC: %w", err)
	}

	// Start query RPC server
	d.queryRPC = NewQueryRPCServer(d)
	if err := d.startQueryRPC(ctx); err != nil {
		return fmt.Errorf("start query RPC: %w", err)
	}

	return nil
}

// startIngestRPC starts the ingest RPC server.
func (d *Daemon) startIngestRPC(ctx context.Context) error {
	listener, err := d.transport.Listen(d.config.IngestSocket)
	if err != nil {
		return fmt.Errorf("listen on ingest socket: %w", err)
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					log.Printf("[telemetryd] ingest accept error: %v", err)
					continue
				}
			}
			go d.ingestRPC.ServeConn(conn)
		}
	}()

	return nil
}

// startQueryRPC starts the query RPC server.
func (d *Daemon) startQueryRPC(ctx context.Context) error {
	listener, err := d.transport.Listen(d.config.QuerySocket)
	if err != nil {
		return fmt.Errorf("listen on query socket: %w", err)
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					log.Printf("[telemetryd] query accept error: %v", err)
					continue
				}
			}
			go d.queryRPC.ServeConn(conn)
		}
	}()

	return nil
}

// Shutdown gracefully shuts down the daemon.
func (d *Daemon) Shutdown(ctx context.Context) error {
	// Close event log
	if d.eventLog != nil {
		if err := d.eventLog.Close(); err != nil {
			log.Printf("[telemetryd] event log close error: %v", err)
		}
	}

	// Close query store
	if d.queryStore != nil {
		if err := d.queryStore.Close(); err != nil {
			log.Printf("[telemetryd] query store close error: %v", err)
		}
	}

	return nil
}

// AppendEvents appends events to the event log.
func (d *Daemon) AppendEvents(events []telemetryingest.Event) (accepted, dropped int, err error) {
	if d.eventLog == nil {
		return 0, len(events), fmt.Errorf("event log not initialized")
	}

	accepted, dropped, err = d.eventLog.Append(events)
	if err != nil {
		return 0, len(events), err
	}

	return accepted, dropped, nil
}

// GetEventCount returns the total event count.
func (d *Daemon) GetEventCount() int64 {
	if d.eventLog == nil {
		return 0
	}

	var count int64
	if err := d.eventLog.GetDB().QueryRow(`SELECT COUNT(*) FROM events`).Scan(&count); err != nil {
		log.Printf("[telemetryd] event count query error: %v", err)
		return 0
	}
	return count
}
