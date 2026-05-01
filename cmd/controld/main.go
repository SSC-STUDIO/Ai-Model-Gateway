// Package main is the entry point for controld - the control plane daemon.
// controld provides the Admin API, Admin UI, configuration management,
// and publishes runtime snapshots to gatewayd.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"ai-model-gateway/internal/version"
)

const (
	Version = version.ProductVersion
)

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
		log.Fatalf("[controld] failed to create daemon: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := d.Start(ctx); err != nil {
		log.Fatalf("[controld] failed to start: %v", err)
	}

	log.Printf("[controld] started on %s", cfg.Listen)

	<-ctx.Done()
	log.Printf("[controld] shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := d.Shutdown(shutdownCtx); err != nil {
		log.Printf("[controld] shutdown error: %v", err)
	}

	log.Printf("[controld] stopped")
}
