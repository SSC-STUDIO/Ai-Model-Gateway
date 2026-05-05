package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"ai-model-gateway/internal/aigw/clientconfig"
)

func runClients(args []string, stdout, stderr io.Writer) error {
	if len(args) < 1 {
		return fmt.Errorf(`usage: aigw clients print|apply [flags]

  print   Show shell snippets and planned config changes (no writes).
  apply   Write Codex, Claude Code, and/or OpenClaw config (use -dry-run to preview).

Flags:
  -config-dir         Directory with gatewayd.json (default: configs)
  -gateway-url        Full data-plane URL; overrides gatewayd.json listen
  -tools              Comma list: codex, claude-code, openclaw, or all (default: all)
  -api-key            Gateway API key (else GATEWAY_CLIENT_API_KEY, GATEWAY_API_KEY, OPENAI_API_KEY)
  -openclaw-model     Public model id for OpenClaw (default: gpt-4o)
  -openclaw-set-primary  Set OpenClaw default primary model (default: true)
  -backup             Backup existing files before apply (default: true)
  -dry-run            With apply: print actions only`)
	}
	sub := args[0]
	if sub != "print" && sub != "apply" {
		return fmt.Errorf("unknown clients subcommand %q (want print or apply)", sub)
	}
	fs := flag.NewFlagSet("clients "+sub, flag.ContinueOnError)
	configDir := fs.String("config-dir", "configs", "directory containing gatewayd.json")
	gatewayURL := fs.String("gateway-url", "", "data plane base URL (overrides gatewayd.json listen)")
	tools := fs.String("tools", "all", "comma-separated: codex, claude-code, openclaw, or all")
	apiKey := fs.String("api-key", "", "gateway API key for client tools")
	backup := fs.Bool("backup", true, "backup existing files before overwrite (apply only)")
	dryRun := fs.Bool("dry-run", false, "print apply actions without writing (apply only)")
	openClawModel := fs.String("openclaw-model", "gpt-4o", "public model id for OpenClaw provider")
	setOpenClawPrimary := fs.Bool("openclaw-set-primary", true, "set OpenClaw agents.defaults.model.primary")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}

	gwPath := clientconfig.DefaultGatewaydPath(*configDir)
	origin, err := clientconfig.ResolveOrigin(*gatewayURL, gwPath)
	if err != nil {
		return err
	}
	openAIBase := clientconfig.OpenAICompatibleBase(origin)
	ts := clientconfig.ParseTools(*tools)
	if !ts.Codex && !ts.Claude && !ts.OpenClaw {
		return fmt.Errorf("no tools selected (use -tools codex,claude-code,openclaw, or all)")
	}

	resolvedKey := strings.TrimSpace(*apiKey)
	if resolvedKey == "" {
		for _, k := range []string{"GATEWAY_CLIENT_API_KEY", "GATEWAY_API_KEY", "OPENAI_API_KEY"} {
			if v := os.Getenv(k); strings.TrimSpace(v) != "" {
				resolvedKey = strings.TrimSpace(v)
				break
			}
		}
	}
	openClawAPIKey := resolvedKey
	if openClawAPIKey == "" {
		openClawAPIKey = "${AI_MODEL_GATEWAY_API_KEY}"
	}

	switch sub {
	case "print":
		printClientSnippets(stdout, stderr, origin, openAIBase, ts, resolvedKey, openClawAPIKey, *openClawModel, *setOpenClawPrimary)
		return nil
	case "apply":
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		return applyClients(stdout, stderr, home, openAIBase, ts, resolvedKey, openClawAPIKey, *openClawModel, *setOpenClawPrimary, *backup, *dryRun)
	}
	return fmt.Errorf("internal error: clients subcommand %q", sub)
}

func printClientSnippets(stdout, stderr io.Writer, origin, openAIBase string, ts clientconfig.ToolSet, apiKey, openClawAPIKey, openClawModel string, setPrimary bool) {
	fmt.Fprintf(stdout, "# AI Model Gateway data plane: %s\n", origin)
	fmt.Fprintf(stdout, "# OpenAI-compatible base (Codex / OpenAI clients): %s\n\n", openAIBase)

	fmt.Fprintln(stdout, "## Bash")
	fmt.Fprintf(stdout, "export OPENAI_BASE_URL=%q\n", openAIBase)
	if apiKey != "" {
		fmt.Fprintf(stdout, "export OPENAI_API_KEY=%q\n", apiKey)
	} else {
		fmt.Fprintln(stdout, "# export OPENAI_API_KEY=<your-gateway-api-key>")
	}
	fmt.Fprintf(stdout, "export ANTHROPIC_BASE_URL=%q\n", openAIBase)
	if apiKey != "" {
		fmt.Fprintf(stdout, "export ANTHROPIC_AUTH_TOKEN=%q\n", apiKey)
	} else {
		fmt.Fprintln(stdout, "# export ANTHROPIC_AUTH_TOKEN=<your-gateway-api-key>")
	}
	if openClawAPIKey == "${AI_MODEL_GATEWAY_API_KEY}" {
		fmt.Fprintf(stdout, "export AI_MODEL_GATEWAY_API_KEY=<your-gateway-api-key>  # used by OpenClaw ${...} substitution\n")
	}
	fmt.Fprintln(stdout)

	fmt.Fprintln(stdout, "## PowerShell")
	fmt.Fprintf(stdout, "$env:OPENAI_BASE_URL=%q\n", openAIBase)
	if apiKey != "" {
		fmt.Fprintf(stdout, "$env:OPENAI_API_KEY=%q\n", apiKey)
	}
	fmt.Fprintf(stdout, "$env:ANTHROPIC_BASE_URL=%q\n", openAIBase)
	if apiKey != "" {
		fmt.Fprintf(stdout, "$env:ANTHROPIC_AUTH_TOKEN=%q\n", apiKey)
	}
	fmt.Fprintln(stdout)

	fmt.Fprintln(stdout, "## Files touched by \"aigw clients apply\"")
	if ts.Codex {
		fmt.Fprintf(stdout, "- Codex: ~/.codex/config.toml (openai_base_url)\n")
	}
	if ts.Claude {
		fmt.Fprintf(stdout, "- Claude Code: ~/.claude/settings.json (env.ANTHROPIC_*)\n")
	}
	if ts.OpenClaw {
		fmt.Fprintf(stdout, "- OpenClaw: ~/.openclaw/openclaw.json (models.providers.%s; comments may be stripped on write)\n", clientconfig.OpenClawProviderID)
		if setPrimary && openClawModel != "" {
			fmt.Fprintf(stdout, "  default primary: %s/%s\n", clientconfig.OpenClawProviderID, openClawModel)
		}
	}
	if apiKey == "" {
		fmt.Fprintln(stderr, "Note: no -api-key or env key found; Claude merge will not set ANTHROPIC_AUTH_TOKEN. OpenClaw will use ${AI_MODEL_GATEWAY_API_KEY}.")
	}
}

func applyClients(stdout, stderr io.Writer, home, openAIBase string, ts clientconfig.ToolSet, apiKey, openClawAPIKey, openClawModel string, setPrimary, backup, dryRun bool) error {
	if ts.Codex {
		p := clientconfig.CodexConfigPath(home)
		if err := applyOne(stdout, stderr, p, backup, dryRun, "codex", func() error {
			return clientconfig.MergeCodexConfig(p, openAIBase)
		}); err != nil {
			return err
		}
	}
	if ts.Claude {
		p := clientconfig.ClaudeSettingsPath(home)
		if err := applyOne(stdout, stderr, p, backup, dryRun, "claude-code", func() error {
			return clientconfig.MergeClaudeSettings(p, openAIBase, apiKey)
		}); err != nil {
			return err
		}
	}
	if ts.OpenClaw {
		p := clientconfig.OpenClawConfigPath(home)
		if err := applyOne(stdout, stderr, p, backup, dryRun, "openclaw", func() error {
			return clientconfig.MergeOpenClawConfig(p, openAIBase, openClawAPIKey, openClawModel, setPrimary && strings.TrimSpace(openClawModel) != "")
		}); err != nil {
			return err
		}
	}
	if dryRun {
		fmt.Fprintln(stdout, "dry-run: no files written")
	}
	return nil
}

func applyOne(stdout, stderr io.Writer, path string, backup, dryRun bool, label string, write func() error) error {
	if dryRun {
		fmt.Fprintf(stdout, "dry-run: would write %s (%s)\n", path, label)
		return nil
	}
	if _, err := os.Stat(path); err == nil && backup {
		bak, err := clientconfig.BackupCopy(path)
		if err != nil {
			return fmt.Errorf("backup %s: %w", path, err)
		}
		if bak != "" {
			fmt.Fprintf(stderr, "backup: %s -> %s\n", path, bak)
		}
	}
	if err := write(); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	fmt.Fprintf(stdout, "wrote %s (%s)\n", path, label)
	return nil
}
