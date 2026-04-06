package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"ai-model-gateway/internal/cli"
	"ai-model-gateway/internal/infra/configloader"
	"ai-model-gateway/internal/runtime"
)

var cliLang string

func init() {
	// Check for -lang flag early
	for i, arg := range os.Args {
		if arg == "-lang" && i+1 < len(os.Args) {
			cliLang = os.Args[i+1]
			break
		}
		if strings.HasPrefix(arg, "-lang=") {
			cliLang = strings.TrimPrefix(arg, "-lang=")
			break
		}
	}

	// If no -lang flag, check LANG env
	if cliLang == "" {
		cliLang = cli.GetLanguageFromEnv()
	}

	cli.SetLanguage(cliLang)
}

func main() {
	// Extract config path from args (supports both -config before and after subcommand)
	var configPath string
	var subcommandIdx = -1
	var subcommandArgs []string

	// First pass: find -config
	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		if arg == "-config" && i+1 < len(os.Args) {
			configPath = os.Args[i+1]
			i++ // Skip the config value
			continue
		}
		if strings.HasPrefix(arg, "-config=") {
			configPath = strings.TrimPrefix(arg, "-config=")
		}
	}

	// Default config path
	if configPath == "" {
		configPath = "configs/config.yaml"
	}

	// Second pass: find subcommand (first non-flag argument)
	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		// Skip -config and its value
		if arg == "-config" {
			i++ // Skip the config value too
			continue
		}
		if strings.HasPrefix(arg, "-config=") {
			continue
		}
		// Skip -lang and its value
		if arg == "-lang" {
			i++ // Skip the lang value too
			continue
		}
		if strings.HasPrefix(arg, "-lang=") {
			continue
		}
		// Found subcommand (first non-flag argument)
		if !strings.HasPrefix(arg, "-") {
			subcommandIdx = i
			// Collect remaining args, filtering out -config and -lang flags
			for j := i + 1; j < len(os.Args); j++ {
				a := os.Args[j]
				if a == "-config" && j+1 < len(os.Args) {
					j++ // Skip the config value
					continue
				}
				if strings.HasPrefix(a, "-config=") {
					continue
				}
				if a == "-lang" && j+1 < len(os.Args) {
					j++ // Skip the lang value
					continue
				}
				if strings.HasPrefix(a, "-lang=") {
					continue
				}
				subcommandArgs = append(subcommandArgs, a)
			}
			break
		}
	}

	// No subcommand -> direct startup
	if subcommandIdx == -1 {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		if err := runtime.RunGatewayRuntime(ctx, configPath, nil); err != nil {
			fmt.Fprintf(os.Stderr, cli.T("cli.gateway_failed")+"\n", err)
			os.Exit(1)
		}
		return
	}

	// Parse global flags (for validation only)
	flag.Parse()

	// Subcommand mode
	cmd := os.Args[subcommandIdx]
	switch cmd {
	case "start":
		cmdStart(configPath, subcommandArgs)
	case "validate":
		cmdValidate(configPath)
	case "health":
		cmdHealth(configPath, subcommandArgs)
	case "install":
		if err := runtime.InstallService(configPath); err != nil {
			fmt.Fprintf(os.Stderr, cli.T("cli.install_failed")+"\n", err)
			os.Exit(1)
		}
	case "uninstall":
		if err := runtime.UninstallService(); err != nil {
			fmt.Fprintf(os.Stderr, cli.T("cli.uninstall_failed")+"\n", err)
			os.Exit(1)
		}
	case "service-start":
		if err := runtime.StartService(); err != nil {
			fmt.Fprintf(os.Stderr, cli.T("cli.start_failed")+"\n", err)
			os.Exit(1)
		}
	case "service-stop":
		if err := runtime.StopService(); err != nil {
			fmt.Fprintf(os.Stderr, cli.T("cli.stop_failed")+"\n", err)
			os.Exit(1)
		}
	case "service-status":
		if err := runtime.QueryServiceStatus(); err != nil {
			fmt.Fprintf(os.Stderr, cli.T("cli.status_failed")+"\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, cli.T("cli.unknown_command")+"\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func cmdStart(configPath string, args []string) {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	daemon := fs.Bool("daemon", false, "Run as daemon (background)")
	fs.Parse(args)

	// Windows service mode
	if *daemon {
		if runtime.IsWindowsService() {
			// Already in service mode, call service runtime
			if err := runtime.RunService(configPath); err != nil {
				fmt.Fprintf(os.Stderr, cli.T("cli.gateway_failed")+"\n", err)
				os.Exit(1)
			}
			return
		}
		// Not in service mode, suggest using install
		fmt.Fprintln(os.Stderr, cli.T("cli.use_install_for_service"))
		os.Exit(1)
	}

	// Foreground mode
	if !runtime.ConfigExists(configPath) {
		fmt.Fprintf(os.Stderr, cli.T("cli.config_not_found")+"\n", configPath)
		os.Exit(1)
	}

	if err := runtime.ValidateConfig(configPath); err != nil {
		fmt.Fprintf(os.Stderr, cli.T("cli.config_invalid")+"\n", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := runtime.RunGatewayRuntime(ctx, configPath, nil); err != nil {
		fmt.Fprintf(os.Stderr, cli.T("cli.gateway_failed")+"\n", err)
		os.Exit(1)
	}
}

func cmdValidate(configPath string) {
	if !runtime.ConfigExists(configPath) {
		fmt.Fprintf(os.Stderr, cli.T("cli.config_not_found")+"\n", configPath)
		os.Exit(1)
	}

	cfg, err := configloader.LoadFromFile(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, cli.T("cli.config_invalid")+"\n", err)
		os.Exit(1)
	}

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, cli.T("cli.config_invalid")+"\n", err)
		os.Exit(1)
	}

	fmt.Println(cli.T("cli.config_valid"))
	fmt.Printf("  "+cli.T("cli.listen")+": %s\n", cfg.Server.Listen)
	fmt.Printf("  "+cli.T("cli.providers")+": %d\n", len(cfg.Providers))
	fmt.Printf("  "+cli.T("cli.admin")+": %s\n", boolStatus(cfg.Admin.Enabled))
	fmt.Printf("  "+cli.T("cli.health_check")+": %s\n", boolStatus(cfg.Routing.Health.Enabled))
	fmt.Printf("  "+cli.T("cli.bridge")+": %s\n", boolStatus(cfg.Compat.Bridge.Enabled))
}

func cmdHealth(configPath string, args []string) {
	fs := flag.NewFlagSet("health", flag.ExitOnError)
	endpoint := fs.String("endpoint", "http://127.0.0.1:18080/-/health", "Health check endpoint")
	timeout := fs.Duration("timeout", 5*time.Second, "Request timeout")
	fs.Parse(args)

	checker := runtime.NewHealthChecker(*endpoint, *timeout)
	result, err := checker.Check()
	if err != nil {
		fmt.Fprintf(os.Stderr, cli.T("cli.health_check_failed")+"\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Gateway is healthy\n")
	fmt.Printf("  Status: %s\n", result.Status)
	if result.RouterStrategy != "" {
		fmt.Printf("  Router strategy: %s\n", result.RouterStrategy)
	}
	if result.BridgeEnabled {
		fmt.Printf("  Bridge: enabled\n")
	}
	if len(result.AvailableModels) > 0 {
		fmt.Printf("  Available models: %d\n", len(result.AvailableModels))
	}
	if len(result.Upstreams) > 0 {
		fmt.Printf("  Upstreams: %d\n", len(result.Upstreams))
	}
}

func boolStatus(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func printUsage() {
	fmt.Println(cli.T("usage.title"))
	fmt.Println("")
	fmt.Println(cli.T("usage.commands_title"))
	fmt.Println("  start           " + cli.T("usage.start_desc"))
	fmt.Println("  validate        " + cli.T("usage.validate_desc"))
	fmt.Println("  health          " + cli.T("usage.health_desc"))
	fmt.Println("  install         " + cli.T("usage.install_desc"))
	fmt.Println("  uninstall       " + cli.T("usage.uninstall_desc"))
	fmt.Println("  service-start   " + cli.T("usage.service_start_desc"))
	fmt.Println("  service-stop    " + cli.T("usage.service_stop_desc"))
	fmt.Println("  service-status  " + cli.T("usage.service_status_desc"))
	fmt.Println("")
	fmt.Println(cli.T("usage.options_title"))
	fmt.Println("  -config path    " + cli.T("usage.config_desc"))
	fmt.Println("  -lang lang      " + cli.T("usage.lang_desc"))
}
