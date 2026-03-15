package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"ai-model-gateway/internal/app"
	"ai-model-gateway/internal/service"
)

func main() {
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

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := app.Run(ctx, configPath); err != nil {
		log.Fatalf("gateway failed: %v", err)
	}
}
