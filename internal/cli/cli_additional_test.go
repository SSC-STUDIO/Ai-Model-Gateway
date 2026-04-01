package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestContextWithTimeout(t *testing.T) {
	ctx, cancel := ContextWithTimeout(100 * time.Millisecond)
	defer cancel()

	if ctx == nil {
		t.Error("expected non-nil context")
	}

	// Context should timeout after 100ms
	select {
	case <-ctx.Done():
		// Expected
	case <-time.After(500 * time.Millisecond):
		t.Error("context should have timed out")
	}
}

func TestCLIRunHelpWithCommand(t *testing.T) {
	cli := New()

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	err := cli.Run([]string{"help", "start"})

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Should show help for start command
	if !strings.Contains(output, "start") && !strings.Contains(output, "Usage:") {
		t.Error("expected command help to be printed")
	}
}

func TestCLIRunHelpUnknownCommand(t *testing.T) {
	cli := New()

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	// Use a mock exit function that doesn't exit
	cli.osExit = func(int) {}

	err := cli.Run([]string{"help", "unknowncommand123"})

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if !strings.Contains(output, "Unknown command") {
		t.Error("expected 'Unknown command' message")
	}
}

func TestPrintUsage(t *testing.T) {
	cli := New()

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	cli.printUsage()

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "AI Model Gateway") {
		t.Error("expected title in usage")
	}

	if !strings.Contains(output, "Commands:") {
		t.Error("expected 'Commands:' in usage")
	}

	if !strings.Contains(output, "start") {
		t.Error("expected 'start' command in usage")
	}

	if !strings.Contains(output, "Global Options:") {
		t.Error("expected 'Global Options:' in usage")
	}
}

func TestPrintVersion(t *testing.T) {
	cli := New()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cli.printVersion()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "AI Model Gateway version") {
		t.Error("expected version message")
	}

	// Should show "dev" when env var is not set
	if !strings.Contains(output, "dev") {
		t.Error("expected 'dev' version when env var not set")
	}
}

func TestPrintVersionWithEnv(t *testing.T) {
	os.Setenv("GATEWAY_VERSION", "1.2.3")
	defer os.Unsetenv("GATEWAY_VERSION")

	cli := New()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cli.printVersion()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "1.2.3") {
		t.Error("expected version from env var")
	}
}

func TestCLIRunWithEmptyCommand(t *testing.T) {
	cli := New()

	// When an empty string is passed, should default to start
	// This will fail due to missing config, but we can verify the behavior
	oldStderr := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w

	err := cli.Run([]string{""})

	w.Close()
	os.Stderr = oldStderr

	// Should return an error because config doesn't exist
	if err == nil {
		t.Error("expected error for start command without config")
	}
}

func TestCLIRunWithFlagThatLooksLikeCommand(t *testing.T) {
	cli := New()

	// Pass a flag as the command, should try to use start
	oldStderr := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w

	err := cli.Run([]string{"-someflag"})

	w.Close()
	os.Stderr = oldStderr

	// Should return an error
	if err == nil {
		t.Error("expected error for unknown flag")
	}
}

func TestCommandFlags(t *testing.T) {
	cli := New()

	// Test start command flags
	startCmd := cli.commands["start"]
	if startCmd == nil {
		t.Fatal("start command not found")
	}

	if startCmd.Flags == nil {
		t.Error("start command should have flags")
	} else {
		// Check daemon flag exists
		daemonFlag := startCmd.Flags.Lookup("daemon")
		if daemonFlag == nil {
			t.Error("daemon flag should exist")
		} else if daemonFlag.DefValue != "false" {
			t.Errorf("daemon flag default should be false, got %s", daemonFlag.DefValue)
		}
	}

	// Test health command flags
	healthCmd := cli.commands["health"]
	if healthCmd == nil {
		t.Fatal("health command not found")
	}

	if healthCmd.Flags == nil {
		t.Error("health command should have flags")
	} else {
		// Check endpoint flag
		endpointFlag := healthCmd.Flags.Lookup("endpoint")
		if endpointFlag == nil {
			t.Error("endpoint flag should exist")
		}

		// Check timeout flag
		timeoutFlag := healthCmd.Flags.Lookup("timeout")
		if timeoutFlag == nil {
			t.Error("timeout flag should exist")
		}
	}

	// Test config command flags
	configCmd := cli.commands["config"]
	if configCmd == nil {
		t.Fatal("config command not found")
	}

	if configCmd.Flags == nil {
		t.Error("config command should have flags")
	} else {
		// Check reload flag
		reloadFlag := configCmd.Flags.Lookup("reload")
		if reloadFlag == nil {
			t.Error("reload flag should exist")
		}

		// Check export flag
		exportFlag := configCmd.Flags.Lookup("export")
		if exportFlag == nil {
			t.Error("export flag should exist")
		}

		// Check output flag
		outputFlag := configCmd.Flags.Lookup("output")
		if outputFlag == nil {
			t.Error("output flag should exist")
		}
	}
}

func TestCommandDescriptions(t *testing.T) {
	cli := New()

	expectedDescriptions := map[string]string{
		"start":          "Start the gateway server",
		"validate":       "Validate configuration file",
		"config":         "Configuration management",
		"health":         "Check gateway health status",
		"install":        "Install as Windows service",
		"uninstall":      "Uninstall Windows service",
		"service-start":  "Start Windows service",
		"service-stop":   "Stop Windows service",
		"service-status": "Check Windows service status",
	}

	for cmdName, expectedDesc := range expectedDescriptions {
		cmd, ok := cli.commands[cmdName]
		if !ok {
			t.Errorf("command '%s' not found", cmdName)
			continue
		}

		if cmd.Description != expectedDesc {
			t.Errorf("command '%s': expected description '%s', got '%s'",
				cmdName, expectedDesc, cmd.Description)
		}
	}
}

func TestLoadConfig(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := tmpDir + "/test_config.yaml"

	configContent := `
listen: ":8080"
upstreams:
  - name: test
    base_url: http://localhost:8081
    api_key: test-key-12345678901234567890123456789012
    models:
      - model1
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to create test config: %v", err)
	}

	cli := New()
	cli.configPath = configPath

	cfg, err := cli.LoadConfig()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if cfg == nil {
		t.Fatal("expected config, got nil")
	}

	if cfg.Listen != ":8080" {
		t.Errorf("expected listen ':8080', got '%s'", cfg.Listen)
	}
}

func TestLoadConfigNotFound(t *testing.T) {
	cli := New()
	cli.configPath = "/nonexistent/path/config.yaml"

	cfg, err := cli.LoadConfig()

	if err == nil {
		t.Error("expected error for missing config file")
	}

	if cfg != nil {
		t.Error("expected nil config on error")
	}
}
