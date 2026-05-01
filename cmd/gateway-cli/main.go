package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"ai-model-gateway/cmd/gateway/commands"
	"ai-model-gateway/internal/cli"
	"ai-model-gateway/internal/version"
	"gopkg.in/yaml.v3"
)

const Version = version.ProductVersion

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
	if err := validateCLIFormat(*format, cmdArgs); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	// Create client
	client := cli.NewControlPlaneClient(*server, *token)

	// Dispatch command
	ctx := context.Background()
	var err error

	switch cmdArgs[0] {
	case "health":
		cmd := commands.NewHealthCommand(client, stdout)
		if len(cmdArgs) > 1 && cmdArgs[1] == "quick" {
			err = cmd.QuickCheck(ctx)
		} else {
			err = cmd.Check(ctx, *format)
		}

	case "status":
		cmd := commands.NewStatusCommand(client, stdout)
		if len(cmdArgs) > 1 && cmdArgs[1] == "watch" {
			interval := 5 * time.Second
			if len(cmdArgs) > 2 {
				if d, parseErr := time.ParseDuration(cmdArgs[2]); parseErr == nil {
					interval = d
				}
			}
			err = cmd.Watch(ctx, interval)
		} else {
			err = cmd.Show(ctx, *format)
		}

	case "validate":
		if len(cmdArgs) < 2 {
			fmt.Fprintln(stderr, "Usage: gateway-cli validate <config-file>")
			return 1
		}
		cmd := commands.NewValidateCommand(stdout)
		err = cmd.Validate(cmdArgs[1], *format)

	case "reload":
		cmd := commands.NewReloadCommand(client, stdout)
		err = cmd.Reload(ctx, *format)

	case "config":
		if len(cmdArgs) < 2 {
			fmt.Fprintln(stderr, "Usage: gateway-cli config <show>")
			return 1
		}
		cmd := commands.NewConfigCommand(client, stdout)
		switch cmdArgs[1] {
		case "show":
			err = cmd.Show(ctx, *format)
		case "preview":
			req := map[string]interface{}{}
			if len(cmdArgs) > 2 {
				cfg, readErr := readYAMLMap(cmdArgs[2])
				if readErr != nil {
					err = readErr
					break
				}
				req["config"] = cfg
			}
			var resp map[string]interface{}
			resp, err = client.ConfigPreview(ctx, req)
			if err == nil {
				err = printGeneric(stdout, resp, *format)
			}
		case "diff":
			req, parseErr := parseConfigDiffArgs(cmdArgs[2:])
			if parseErr != nil {
				fmt.Fprintln(stderr, "Usage: gateway-cli config diff [--from rev] [--to rev] [--file config.yaml]")
				fmt.Fprintf(stderr, "Error: %v\n", parseErr)
				return 1
			}
			var resp map[string]interface{}
			resp, err = client.ConfigDiff(ctx, req)
			if err == nil {
				err = printGeneric(stdout, resp, *format)
			}
		default:
			err = fmt.Errorf("unknown config subcommand: %s", cmdArgs[1])
		}

	case "runtime":
		if len(cmdArgs) < 2 {
			fmt.Fprintln(stderr, "Usage: gateway-cli runtime <status|preflight>")
			return 1
		}
		var resp map[string]interface{}
		switch cmdArgs[1] {
		case "status":
			resp, err = client.GetRuntimeStatus(ctx)
		case "preflight":
			resp, err = client.RuntimePreflight(ctx)
		default:
			err = fmt.Errorf("unknown runtime subcommand: %s", cmdArgs[1])
		}
		if err == nil {
			err = printGeneric(stdout, resp, *format)
		}

	case "audit":
		limit := 100
		if len(cmdArgs) > 1 {
			fmt.Sscanf(cmdArgs[1], "%d", &limit)
		}
		var resp map[string]interface{}
		resp, err = client.ListAudit(ctx, limit)
		if err == nil {
			err = printGeneric(stdout, resp, *format)
		}

	case "probe":
		if len(cmdArgs) < 3 {
			fmt.Fprintln(stderr, "Usage: gateway-cli probe <provider|model> <name> [model-or-provider]")
			return 1
		}
		req := map[string]interface{}{}
		var resp map[string]interface{}
		switch cmdArgs[1] {
		case "provider":
			req["provider_id"] = cmdArgs[2]
			if len(cmdArgs) > 3 {
				req["model"] = cmdArgs[3]
			}
			resp, err = client.ProbeProvider(ctx, req)
		case "model":
			req["model"] = cmdArgs[2]
			if len(cmdArgs) > 3 {
				req["provider_id"] = cmdArgs[3]
			}
			resp, err = client.ProbeModel(ctx, req)
		default:
			err = fmt.Errorf("unknown probe subcommand: %s", cmdArgs[1])
		}
		if err == nil {
			err = printGeneric(stdout, resp, *format)
		}

	case "replay":
		requestID := ""
		if len(cmdArgs) > 1 && cmdArgs[1] != "list" {
			requestID = cmdArgs[1]
		}
		var resp map[string]interface{}
		resp, err = client.Replay(ctx, requestID)
		if err == nil {
			err = printGeneric(stdout, resp, *format)
		}

	case "diagnostics":
		var resp map[string]interface{}
		resp, err = client.Diagnostics(ctx)
		if err == nil {
			err = printGeneric(stdout, resp, *format)
		}

	case "secrets":
		if len(cmdArgs) < 2 || cmdArgs[1] != "check" {
			fmt.Fprintln(stderr, "Usage: gateway-cli secrets check")
			return 1
		}
		var resp map[string]interface{}
		resp, err = client.SecretsStatus(ctx)
		if err == nil {
			err = printGeneric(stdout, resp, *format)
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

	case "benchmark":
		if len(cmdArgs) < 2 {
			fmt.Fprintln(stderr, "Usage: gateway-cli benchmark <baseline|baselines|run|runs|show|telemetry|telemetry-summary|target-summary> ...")
			return 1
		}
		cmd := commands.NewBenchmarkCommand(client, stdout)
		switch cmdArgs[1] {
		case "baseline":
			if len(cmdArgs) == 2 {
				err = cmd.ListBaselines(ctx, *format)
				break
			}
			if len(cmdArgs) < 3 || cmdArgs[2] != "import" || len(cmdArgs) < 5 {
				fmt.Fprintln(stderr, "Usage: gateway-cli benchmark baseline import <public_standard|vendor_claim> <file> [source-name] [source-url]")
				return 1
			}
			sourceName := ""
			if len(cmdArgs) > 5 {
				sourceName = cmdArgs[5]
			}
			sourceURL := ""
			if len(cmdArgs) > 6 {
				sourceURL = cmdArgs[6]
			}
			err = cmd.ImportBaseline(ctx, cmdArgs[3], cmdArgs[4], sourceName, sourceURL, *format)
		case "baselines":
			err = cmd.ListBaselines(ctx, *format)
		case "run":
			req, parseErr := parseBenchmarkRunArgs(cmdArgs[2:])
			if parseErr != nil {
				fmt.Fprintf(stderr, "Usage: gateway-cli benchmark run [--provider <provider>] [--model <public-model>] [--protocol <auto|openai_chat_completions|anthropic_messages>] [--suite <suite>] [--public-snapshot <id>] [--vendor-snapshot <id>] [--all-active]\n")
				fmt.Fprintf(stderr, "Error: %v\n", parseErr)
				return 1
			}
			err = cmd.Run(ctx, req, *format)
		case "runs":
			err = cmd.ListRuns(ctx, 20, *format)
		case "show":
			if len(cmdArgs) < 3 {
				fmt.Fprintln(stderr, "Usage: gateway-cli benchmark show <run-id>")
				return 1
			}
			err = cmd.Show(ctx, cmdArgs[2], *format)
		case "telemetry":
			runID, query, parseErr := parseBenchmarkTelemetryArgs(cmdArgs[2:])
			if parseErr != nil {
				fmt.Fprintf(stderr, "Usage: gateway-cli benchmark telemetry <run-id> [--target <target-id>] [--case <case-id>] [--provider <provider>] [--model <public-model>] [--hours <hours>] [--limit <limit>] [--offset <offset>] [-format text|json|csv]\n")
				fmt.Fprintf(stderr, "Error: %v\n", parseErr)
				return 1
			}
			err = cmd.Telemetry(ctx, runID, query, *format)
		case "telemetry-summary":
			runID, query, parseErr := parseBenchmarkTelemetryArgs(cmdArgs[2:])
			if parseErr != nil {
				fmt.Fprintf(stderr, "Usage: gateway-cli benchmark telemetry-summary <run-id> [--target <target-id>] [--case <case-id>] [--provider <provider>] [--model <public-model>] [--hours <hours>] [--limit <limit>] [--offset <offset>] [-format text|json|csv]\n")
				fmt.Fprintf(stderr, "Error: %v\n", parseErr)
				return 1
			}
			err = cmd.TelemetrySummary(ctx, runID, query, *format)
		case "target-summary":
			runID, query, sortMode, parseErr := parseBenchmarkTargetSummaryArgs(cmdArgs[2:])
			if parseErr != nil {
				fmt.Fprintf(stderr, "Usage: gateway-cli benchmark target-summary <run-id> [--target <target-id>] [--case <case-id>] [--provider <provider>] [--model <public-model>] [--hours <hours>] [--limit <limit>] [--offset <offset>] [--sort <severity|provider|latency|cost>] [-format text|json|csv]\n")
				fmt.Fprintf(stderr, "Error: %v\n", parseErr)
				return 1
			}
			err = cmd.TargetSummary(ctx, runID, query, sortMode, *format)
		default:
			err = fmt.Errorf("unknown benchmark subcommand: %s", cmdArgs[1])
		}

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
	fmt.Fprintln(w, "  health [quick]           Health check (quick = direct gateway check)")
	fmt.Fprintln(w, "  status [watch]           System status (watch = continuous)")
	fmt.Fprintln(w, "  validate <file>          Validate configuration file")
	fmt.Fprintln(w, "  reload                   Reload configuration")
	fmt.Fprintln(w, "  config show              Show current configuration")
	fmt.Fprintln(w, "  config preview [file]    Preview active or draft configuration")
	fmt.Fprintln(w, "  config diff [--from rev] [--to rev] [--file file]")
	fmt.Fprintln(w, "  runtime status           Show bundle and daemon runtime status")
	fmt.Fprintln(w, "  runtime preflight        Run runtime preflight checks")
	fmt.Fprintln(w, "  audit [limit]            List audit events")
	fmt.Fprintln(w, "  probe provider <id> [model]")
	fmt.Fprintln(w, "  probe model <model> [provider]")
	fmt.Fprintln(w, "  replay list|<request-id>")
	fmt.Fprintln(w, "  diagnostics              Generate redacted diagnostics")
	fmt.Fprintln(w, "  secrets check            Check configured secret presence")
	fmt.Fprintln(w, "  provider list            List all providers")
	fmt.Fprintln(w, "  provider test <name>     Test provider connection")
	fmt.Fprintln(w, "  telemetry events         Query event logs")
	fmt.Fprintln(w, "  publish history          View configuration history")
	fmt.Fprintln(w, "  publish rollback <rev>   Rollback configuration")
	fmt.Fprintln(w, "  benchmark baseline import <kind> <file> [name] [url]")
	fmt.Fprintln(w, "  benchmark baselines      List verification baseline snapshots")
	fmt.Fprintln(w, "  benchmark run [--provider p --model m --public-snapshot id --vendor-snapshot id --protocol proto --suite suite --all-active]")
	fmt.Fprintln(w, "  benchmark runs           List verification benchmark runs")
	fmt.Fprintln(w, "  benchmark show <run-id>  Show verification benchmark report")
	fmt.Fprintln(w, "  benchmark telemetry <run-id> [--target id --case id --provider p --model m] [-format text|json|csv]")
	fmt.Fprintln(w, "  benchmark telemetry-summary <run-id> [--target id --case id --provider p --model m] [-format text|json|csv]")
	fmt.Fprintln(w, "  benchmark target-summary <run-id> [--target id --case id --provider p --model m --sort severity|provider|latency|cost] [-format text|json|csv]")
	fmt.Fprintln(w, "  test convert             Test protocol conversion")
	fmt.Fprintln(w, "  version                  Show version")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "  -format text|json|csv    Output format (default: text; csv for benchmark telemetry)")
	fmt.Fprintln(w, "  -server url              Control plane URL (default: http://127.0.0.1:18081)")
	fmt.Fprintln(w, "  -token token             Admin token (or ADMIN_TOKEN env)")
}

func printGeneric(w io.Writer, value interface{}, format string) error {
	switch format {
	case "", "text":
		return printGenericText(w, value)
	case "json":
		formatter := cli.NewOutputFormatter(cli.FormatJSON, w)
		return formatter.WriteJSON(value)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}

func validateCLIFormat(format string, cmdArgs []string) error {
	switch format {
	case "", "text", "json":
		return nil
	case "csv":
		if len(cmdArgs) >= 2 && cmdArgs[0] == "benchmark" {
			switch cmdArgs[1] {
			case "telemetry", "telemetry-summary", "target-summary":
				return nil
			}
		}
		return fmt.Errorf("csv format is only supported for benchmark telemetry commands")
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}

func printGenericText(w io.Writer, value interface{}) error {
	switch v := value.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if _, err := fmt.Fprintf(w, "%s: %s\n", key, formatGenericValue(v[key])); err != nil {
				return err
			}
		}
		return nil
	case []interface{}:
		for i, item := range v {
			if _, err := fmt.Fprintf(w, "%d: %s\n", i+1, formatGenericValue(item)); err != nil {
				return err
			}
		}
		return nil
	default:
		_, err := fmt.Fprintln(w, formatGenericValue(v))
		return err
	}
}

func formatGenericValue(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return "-"
	case string:
		if strings.TrimSpace(v) == "" {
			return "-"
		}
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%.0f", v)
		}
		return fmt.Sprintf("%.3f", v)
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(data)
	}
}

func readYAMLMap(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg map[string]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func parseConfigDiffArgs(args []string) (map[string]interface{}, error) {
	fs := flag.NewFlagSet("config-diff", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	from := fs.String("from", "", "source revision")
	to := fs.String("to", "", "target revision")
	file := fs.String("file", "", "target config file")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	req := map[string]interface{}{}
	if strings.TrimSpace(*from) != "" {
		req["from_revision_id"] = strings.TrimSpace(*from)
	}
	if strings.TrimSpace(*to) != "" {
		req["to_revision_id"] = strings.TrimSpace(*to)
	}
	if strings.TrimSpace(*file) != "" {
		cfg, err := readYAMLMap(*file)
		if err != nil {
			return nil, err
		}
		req["config"] = cfg
	}
	if _, hasTo := req["to_revision_id"]; !hasTo {
		if _, hasConfig := req["config"]; !hasConfig {
			return nil, fmt.Errorf("--to or --file is required")
		}
	}
	return req, nil
}

func parseBenchmarkRunArgs(args []string) (*cli.VerificationRunRequest, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("benchmark run arguments are required")
	}
	legacyPositional := len(args) >= 3 && !strings.HasPrefix(args[0], "-")
	if legacyPositional {
		req := &cli.VerificationRunRequest{
			ProviderID:       args[0],
			PublicModel:      args[1],
			PublicSnapshotID: args[2],
		}
		if len(args) > 3 {
			req.VendorSnapshotID = args[3]
		}
		return req, nil
	}

	fs := flag.NewFlagSet("benchmark-run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	req := &cli.VerificationRunRequest{}
	fs.StringVar(&req.ProviderID, "provider", "", "provider id")
	fs.StringVar(&req.PublicModel, "model", "", "public model")
	fs.StringVar(&req.Protocol, "protocol", "", "protocol")
	fs.BoolVar(&req.AllActive, "all-active", false, "run all active provider-model routes")
	fs.StringVar(&req.Suite, "suite", "", "suite")
	fs.StringVar(&req.PublicSnapshotID, "public-snapshot", "", "public baseline snapshot id")
	fs.StringVar(&req.VendorSnapshotID, "vendor-snapshot", "", "vendor baseline snapshot id")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if req.PublicSnapshotID == "" && req.VendorSnapshotID == "" {
		return nil, fmt.Errorf("at least one baseline snapshot is required")
	}
	if req.AllActive {
		if strings.TrimSpace(req.ProviderID) != "" || strings.TrimSpace(req.PublicModel) != "" {
			return nil, fmt.Errorf("--all-active cannot be combined with --provider or --model")
		}
		return req, nil
	}
	if strings.TrimSpace(req.ProviderID) == "" || strings.TrimSpace(req.PublicModel) == "" {
		return nil, fmt.Errorf("--provider and --model are required unless --all-active is set")
	}
	return req, nil
}

func parseBenchmarkTelemetryArgs(args []string) (string, *cli.VerificationRunTelemetryQuery, error) {
	if len(args) == 0 {
		return "", nil, fmt.Errorf("benchmark telemetry run id is required")
	}
	runID := strings.TrimSpace(args[0])
	if runID == "" {
		return "", nil, fmt.Errorf("benchmark telemetry run id is required")
	}

	fs := flag.NewFlagSet("benchmark-telemetry", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	query := &cli.VerificationRunTelemetryQuery{}
	var provider string
	var model string
	var targetID string
	fs.StringVar(&query.CaseID, "case", "", "benchmark case id")
	fs.StringVar(&targetID, "target", "", "benchmark target id")
	fs.StringVar(&provider, "provider", "", "provider filter")
	fs.StringVar(&model, "model", "", "model filter")
	fs.IntVar(&query.WindowHours, "hours", 24, "window hours")
	fs.IntVar(&query.Limit, "limit", 200, "max events")
	fs.IntVar(&query.Offset, "offset", 0, "offset")
	if err := fs.Parse(args[1:]); err != nil {
		return "", nil, err
	}
	if provider = strings.TrimSpace(provider); provider != "" {
		query.Providers = []string{provider}
	}
	if model = strings.TrimSpace(model); model != "" {
		query.Models = []string{model}
	}
	query.TargetID = strings.TrimSpace(targetID)
	return runID, query, nil
}

func parseBenchmarkTargetSummaryArgs(args []string) (string, *cli.VerificationRunTelemetryQuery, string, error) {
	if len(args) == 0 {
		return "", nil, "", fmt.Errorf("benchmark target summary run id is required")
	}
	runID := strings.TrimSpace(args[0])
	if runID == "" {
		return "", nil, "", fmt.Errorf("benchmark target summary run id is required")
	}

	fs := flag.NewFlagSet("benchmark-target-summary", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	query := &cli.VerificationRunTelemetryQuery{}
	var provider string
	var model string
	var targetID string
	var sortMode string
	fs.StringVar(&query.CaseID, "case", "", "benchmark case id")
	fs.StringVar(&targetID, "target", "", "benchmark target id")
	fs.StringVar(&provider, "provider", "", "provider filter")
	fs.StringVar(&model, "model", "", "model filter")
	fs.StringVar(&sortMode, "sort", "severity", "sort mode")
	fs.IntVar(&query.WindowHours, "hours", 24, "window hours")
	fs.IntVar(&query.Limit, "limit", 200, "max events")
	fs.IntVar(&query.Offset, "offset", 0, "offset")
	if err := fs.Parse(args[1:]); err != nil {
		return "", nil, "", err
	}
	switch sortMode = strings.ToLower(strings.TrimSpace(sortMode)); sortMode {
	case "", "severity":
		sortMode = "severity"
	case "provider", "latency", "cost":
	default:
		return "", nil, "", fmt.Errorf("unsupported target summary sort mode: %s", sortMode)
	}
	if provider = strings.TrimSpace(provider); provider != "" {
		query.Providers = []string{provider}
	}
	if model = strings.TrimSpace(model); model != "" {
		query.Models = []string{model}
	}
	query.TargetID = strings.TrimSpace(targetID)
	return runID, query, sortMode, nil
}
