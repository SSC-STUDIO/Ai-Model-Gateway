// Package main implements the local AI Model Gateway operations entrypoint.
package main

import (
	"fmt"
	"io"
	"os"
	"runtime"

	"ai-model-gateway/internal/version"
)

var Version = version.ProductVersion

func main() {
	os.Exit(run(os.Args, os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 || args[1] == "-h" || args[1] == "--help" || args[1] == "help" {
		printUsage(stdout)
		return 0
	}

	var err error
	switch args[1] {
	case "version":
		_, err = fmt.Fprintf(stdout, "aigw version %s (%s/%s)\n", Version, runtime.GOOS, runtime.GOARCH)
	case "supervise":
		err = runSupervise(args[2:], stdout, stderr)
	case "doctor":
		err = runDoctor(args[2:], stdout)
	case "status":
		err = runStatus(args[2:], stdout)
	case "logs":
		err = runLogs(args[2:], stdout)
	case "backup":
		err = runBackup(args[2:], stdout)
	case "bundle":
		err = runBundle(args[2:], stdout)
	case "update":
		err = runUpdate(args[2:], stdout)
	case "service":
		err = runService(args[2:], stdout)
	case "clients":
		err = runClients(args[2:], stdout, stderr)
	default:
		err = fmt.Errorf("unknown command: %s", args[1])
	}
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: aigw <command> [options]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  supervise              Run telemetryd, gatewayd, and controld as one local service")
	fmt.Fprintln(w, "  doctor                 Check local config, manifest, and runtime files")
	fmt.Fprintln(w, "  status                 Query local gateway/control status")
	fmt.Fprintln(w, "  logs [-n lines]        Print daemon logs")
	fmt.Fprintln(w, "  backup                 Back up configs and runtime state")
	fmt.Fprintln(w, "  bundle build|verify    Build or verify a release manifest")
	fmt.Fprintln(w, "  update apply|rollback  Apply a verified bundle or roll back last local payload backup")
	fmt.Fprintln(w, "  service print          Print the default systemd unit")
	fmt.Fprintln(w, "  clients print|apply    Point Codex, Claude Code, OpenClaw at this gateway")
	fmt.Fprintln(w, "  version                Show version")
}
