// Package main is the entry point for controld - the control plane daemon.
// controld provides the Admin API, Admin UI, configuration management,
// and publishes runtime snapshots to gatewayd.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"ai-model-gateway/internal/infra/logger"
	"ai-model-gateway/internal/version"
)

var Version = version.ProductVersion

func main() {
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

	cfg := loadConfig(*configPath, *listen, *gatewaySocket, *telemetrySocket, *dataDir, *authoringConfig)

	d, err := NewDaemon(cfg)
	if err != nil {
		logger.Error("failed to create daemon", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := d.Start(ctx); err != nil {
		logger.Error("failed to start", "error", err)
		os.Exit(1)
	}

	logger.Info("started", "listen", cfg.Listen)

	<-ctx.Done()
	logger.Info("shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := d.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "error", err)
	}

	logger.Info("stopped")
}
