package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ai-model-gateway/internal/app"
	"ai-model-gateway/internal/config"
)

func (c *CLI) registerCommands() {
	// Start command
	startFlags := flag.NewFlagSet("start", flag.ExitOnError)
	startFlags.Bool("daemon", false, "Run as daemon (background)")
	
	c.Register(&Command{
		Name:        "start",
		Description: "Start the gateway server",
		Usage:       "start [-daemon]",
		Flags:       startFlags,
		Run:         c.cmdStart,
	})

	// Validate command
	c.Register(&Command{
		Name:        "validate",
		Description: "Validate configuration file",
		Usage:       "validate",
		Run:         c.cmdValidate,
	})

	// Config command
	configFlags := flag.NewFlagSet("config", flag.ExitOnError)
	configFlags.Bool("reload", false, "Reload configuration without restarting")
	configFlags.Bool("export", false, "Export current configuration")
	configFlags.String("output", "", "Output file for export")
	
	c.Register(&Command{
		Name:        "config",
		Description: "Configuration management",
		Usage:       "config [-reload|-export -output file]",
		Flags:       configFlags,
		Run:         c.cmdConfig,
	})

	// Health command
	healthFlags := flag.NewFlagSet("health", flag.ExitOnError)
	healthFlags.String("endpoint", "http://127.0.0.1:18080/-/health", "Health check endpoint")
	healthFlags.Duration("timeout", 5*time.Second, "Request timeout")
	
	c.Register(&Command{
		Name:        "health",
		Description: "Check gateway health status",
		Usage:       "health [-endpoint url] [-timeout duration]",
		Flags:       healthFlags,
		Run:         c.cmdHealth,
	})

	// Service management commands (Windows)
	c.Register(&Command{
		Name:        "install",
		Description: "Install as Windows service",
		Usage:       "install",
		Run:         c.cmdServiceInstall,
	})

	c.Register(&Command{
		Name:        "uninstall",
		Description: "Uninstall Windows service",
		Usage:       "uninstall",
		Run:         c.cmdServiceUninstall,
	})

	c.Register(&Command{
		Name:        "service-start",
		Description: "Start Windows service",
		Usage:       "service-start",
		Run:         c.cmdServiceStart,
	})

	c.Register(&Command{
		Name:        "service-stop",
		Description: "Stop Windows service",
		Usage:       "service-stop",
		Run:         c.cmdServiceStop,
	})

	c.Register(&Command{
		Name:        "service-status",
		Description: "Check Windows service status",
		Usage:       "service-status",
		Run:         c.cmdServiceStatus,
	})
}

func (c *CLI) cmdStart(args []string) error {
	fmt.Println("Starting AI Model Gateway...")
	fmt.Printf("Config: %s\n", c.configPath)

	// Check if config exists
	if _, err := os.Stat(c.configPath); os.IsNotExist(err) {
		return fmt.Errorf("config file not found: %s", c.configPath)
	}

	// Validate config before starting
	cfg, err := c.LoadConfig()
	if err != nil {
		return err
	}

	if err := config.ValidateConfig(cfg); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	fmt.Printf("Listening on: %s\n", cfg.Listen)
	fmt.Println("Press Ctrl+C to stop")

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	return app.Run(ctx, c.configPath)
}

func (c *CLI) cmdValidate(args []string) error {
	fmt.Printf("Validating config: %s\n", c.configPath)

	cfg, err := c.LoadConfig()
	if err != nil {
		return err
	}

	if err := config.ValidateConfig(cfg); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	fmt.Println("✓ Configuration is valid")
	fmt.Printf("  Listen: %s\n", cfg.Listen)
	fmt.Printf("  Upstreams: %d\n", len(cfg.Upstreams))
	fmt.Printf("  Admin enabled: %v\n", cfg.Admin.Enabled)
	fmt.Printf("  Health enabled: %v\n", cfg.Health.Enabled)
	fmt.Printf("  Bridge enabled: %v\n", cfg.Bridge.Enabled)
	
	return nil
}

func (c *CLI) cmdConfig(args []string) error {
	// This is handled by the flags
	return fmt.Errorf("config command should be handled by flags")
}

func (c *CLI) cmdHealth(args []string) error {
	return checkHealth(args)
}
