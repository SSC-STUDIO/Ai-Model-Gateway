package main

import (
	"context"
	"database/sql"
	"net/http"
	"sync"
	"time"

	"ai-model-gateway/internal/contracts"
	"ai-model-gateway/internal/control/api"
	"ai-model-gateway/internal/control/audit"
	"ai-model-gateway/internal/control/benchmarking"
	"ai-model-gateway/internal/control/compiler"
	"ai-model-gateway/internal/control/publish"
	"ai-model-gateway/internal/infra/configloader"
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

	// GatewayReadinessRepublishMinIntervalSec is the minimum time between
	// automatic snapshot republish attempts while gatewayd reports a non-ready
	// readiness state but the RPC link is still healthy. Zero defaults to 15s.
	GatewayReadinessRepublishMinIntervalSec int `json:"gateway_readiness_republish_min_interval_sec"`
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
	auditStore     *audit.Store
	replayDB       *sql.DB
	startedAt      time.Time
	runCtx         context.Context
	runCancel      context.CancelFunc
	frontendBundle *api.AdminFrontendBundle
	configWatcher  *configloader.Watcher

	gwReadinessRepublishMu       sync.Mutex
	lastGatewayReadinessRepublish time.Time

	// testRepublishHook is invoked at the start of each republish attempt (including no-ops); tests only.
	testRepublishHook func(reason string)
}
