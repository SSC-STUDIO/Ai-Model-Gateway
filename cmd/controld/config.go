package main

import (
	"encoding/json"
	"os"

	"ai-model-gateway/internal/infra/logger"
	"ai-model-gateway/internal/updater"
	"path/filepath"
	"runtime"
)

// loadConfig loads the bootstrap configuration.
func loadConfig(configPath, listen, gatewaySocket, telemetrySocket, dataDir, authoringConfig string) Config {
	cfg := Config{
		Listen:     "127.0.0.1:18081",
		DataDir:    "data/control",
		ConfigPath: "configs/config.yaml",
		LogLevel:   "info",
		Update: updater.Options{
			Repository: updater.DefaultRepository,
			InstallDir: ".",
			StateDir:   filepath.Join(".gateway-runtime", "update"),
		},
	}

	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			logger.Warn("could not read config file", "error", err)
		} else if err := json.Unmarshal(data, &cfg); err != nil {
			logger.Warn("could not parse config file", "error", err)
		}
	}

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

	if cfg.GatewaySocket == "" {
		cfg.GatewaySocket = defaultSocketPath("gateway-control")
	}
	if cfg.TelemetrySocket == "" {
		cfg.TelemetrySocket = defaultSocketPath("telemetry-query")
	}
	if cfg.Update.Repository == "" {
		cfg.Update.Repository = updater.DefaultRepository
	}
	if cfg.Update.InstallDir == "" {
		cfg.Update.InstallDir = "."
	}
	if cfg.Update.StateDir == "" {
		cfg.Update.StateDir = filepath.Join(".gateway-runtime", "update")
	}

	return cfg
}

// defaultSocketPath returns the default socket path for the platform.
func defaultSocketPath(name string) string {
	if runtime.GOOS == "windows" {
		return name
	}
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return filepath.Join(runtimeDir, name+".sock")
	}
	return filepath.Join("/tmp", name+".sock")
}
