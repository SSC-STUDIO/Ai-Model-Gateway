package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"ai-model-gateway/cmd/gateway/commands"
	"ai-model-gateway/internal/cli"
)

const Version = "1.0.0"

func main() {
	os.Exit(runCLI(os.Args, os.Stdout, os.Stderr))
}

func runCLI(args []string, stdout, stderr io.Writer) int {
	// Check for help flag before parsing
	for _, arg := range args[1:] {
		if arg == "-h" || arg == "--help" || arg == "help" {
			printUsage(stdout)
			return 0
		}
	}

	fs := flag.NewFlagSet("gateway-cli", flag.ContinueOnError)
	fs.SetOutput(stderr)

	format := fs.String("format", "text", "Output format (text|json)")
	server := fs.String("server", "http://127.0.0.1:18081", "Control plane server URL")
	token := fs.String("token", os.Getenv("ADMIN_TOKEN"), "Admin token")

	if err := fs.Parse(args[1:]); err != nil {
		return 1
	}

	cmdArgs := fs.Args()
	if len(cmdArgs) == 0 {
		printUsage(stderr)
		return 1
	}

	// Create client
	client := cli.NewControlPlaneClient(*server, *token)

	// Dispatch command
	ctx := context.Background()
	var err error

	switch cmdArgs[0] {
	case "config":
		if len(cmdArgs) < 2 {
			fmt.Fprintln(stderr, "Usage: gateway-cli config <show>")
			return 1
		}
		cmd := commands.NewConfigCommand(client, stdout)
		switch cmdArgs[1] {
		case "show":
			err = cmd.Show(ctx, *format)
		default:
			err = fmt.Errorf("unknown config subcommand: %s", cmdArgs[1])
		}

	case "provider":
		if len(cmdArgs) < 2 {
			fmt.Fprintln(stderr, "Usage: gateway-cli provider <list|test> [name]")
			return 1
		}
		cmd := commands.NewProviderCommand(client, stdout)
		switch cmdArgs[1] {
		case "list":
			err = cmd.List(ctx, *format)
		case "test":
			if len(cmdArgs) < 3 {
				fmt.Fprintln(stderr, "Usage: gateway-cli provider test <name>")
				return 1
			}
			err = cmd.Test(ctx, cmdArgs[2])
		default:
			err = fmt.Errorf("unknown provider subcommand: %s", cmdArgs[1])
		}

	case "telemetry":
		if len(cmdArgs) < 2 {
			fmt.Fprintln(stderr, "Usage: gateway-cli telemetry <events>")
			return 1
		}
		cmd := commands.NewTelemetryCommand(client, stdout)
		switch cmdArgs[1] {
		case "events":
			err = cmd.Events(ctx, 24, *format)
		default:
			err = fmt.Errorf("unknown telemetry subcommand: %s", cmdArgs[1])
		}

	case "publish":
		if len(cmdArgs) < 2 {
			fmt.Fprintln(stderr, "Usage: gateway-cli publish <history|rollback> [revision]")
			return 1
		}
		cmd := commands.NewPublishCommand(client, stdout)
		switch cmdArgs[1] {
		case "history":
			err = cmd.History(ctx, 10, *format)
		case "rollback":
			if len(cmdArgs) < 3 {
				fmt.Fprintln(stderr, "Usage: gateway-cli publish rollback <revision>")
				return 1
			}
			err = cmd.Rollback(ctx, cmdArgs[2])
		default:
			err = fmt.Errorf("unknown publish subcommand: %s", cmdArgs[1])
		}

	case "test":
		if len(cmdArgs) < 2 {
			fmt.Fprintln(stderr, "Usage: gateway-cli test <convert>")
			return 1
		}
		cmd := commands.NewTestCommand(stdout)
		switch cmdArgs[1] {
		case "convert":
			err = cmd.Convert()
		default:
			err = fmt.Errorf("unknown test subcommand: %s", cmdArgs[1])
		}

	case "version":
		fmt.Fprintf(stdout, "gateway-cli version %s\n", Version)

	default:
		err = fmt.Errorf("unknown command: %s", cmdArgs[0])
	}

	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	return 0
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: gateway-cli [options] <command> [subcommand] [args]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  config show              Show current configuration")
	fmt.Fprintln(w, "  provider list            List all providers")
	fmt.Fprintln(w, "  provider test <name>     Test provider connection")
	fmt.Fprintln(w, "  telemetry events         Query event logs")
	fmt.Fprintln(w, "  publish history          View configuration history")
	fmt.Fprintln(w, "  publish rollback <rev>   Rollback configuration")
	fmt.Fprintln(w, "  test convert             Test protocol conversion")
	fmt.Fprintln(w, "  version                  Show version")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "  -format text|json        Output format (default: text)")
	fmt.Fprintln(w, "  -server url              Control plane URL (default: http://127.0.0.1:18081)")
	fmt.Fprintln(w, "  -token token             Admin token (or ADMIN_TOKEN env)")
}
