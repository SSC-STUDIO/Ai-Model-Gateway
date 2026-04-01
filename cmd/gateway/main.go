package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"ai-model-gateway/internal/app"
	"ai-model-gateway/internal/cli"
	"ai-model-gateway/internal/service"
)

func main() {
	// Check if we should run as Windows service first
	var configPath string
	flag.StringVar(&configPath, "config", "configs/config.yaml", "path to config file")
	flag.Parse()

	handled, err := service.Run(configPath)
	if err != nil {
		log.Fatalf("service start failed: %v", err)
	}
	if handled {
		return
	}

	// Check for legacy mode (direct start without subcommand)
	args := os.Args[1:]
	if len(args) == 0 || (len(args) > 0 && args[0] == "-config") {
		// Legacy mode: just start the gateway
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		if err := app.Run(ctx, configPath); err != nil {
			log.Fatalf("gateway failed: %v", err)
		}
		return
	}

	// New CLI mode
	c := cli.New()
	if err := c.Run(args); err != nil {
		log.Fatalf("error: %v", err)
	}
}
