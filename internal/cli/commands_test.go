package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRegisterCommands(t *testing.T) {
	cli := New()
	
	// Verify all expected commands are registered
	expectedCommands := map[string]struct {
		description string
		usage       string
	}{
		"start":           {"Start the gateway server", "start [-daemon]"},
		"validate":        {"Validate configuration file", "validate"},
		"config":          {"Configuration management", "config [-reload|-export -output file]"},
		"health":          {"Check gateway health status", "health [-endpoint url] [-timeout duration]"},
		"install":         {"Install as Windows service", "install"},
		"uninstall":       {"Uninstall Windows service", "uninstall"},
		"service-start":   {"Start Windows service", "service-start"},
		"service-stop":    {"Stop Windows service", "service-stop"},
		"service-status":  {"Check Windows service status", "service-status"},
	}
	
	for name, expected := range expectedCommands {
		cmd, ok := cli.commands[name]
		if !ok {
			t.Errorf("expected command '%s' to be registered", name)
			continue
		}
		
		if cmd.Description != expected.description {
			t.Errorf("command '%s': expected description '%s', got '%s'", 
				name, expected.description, cmd.Description)
		}
		
		if cmd.Usage != expected.usage {
			t.Errorf("command '%s': expected usage '%s', got '%s'", 
				name, expected.usage, cmd.Usage)
		}
	}
}

func TestCmdStartConfigNotFound(t *testing.T) {
	cli := New()
	cli.configPath = "/nonexistent/path/config.yaml"
	
	err := cli.cmdStart([]string{})
	
	if err == nil {
		t.Error("expected error for missing config file")
	}
	
	// The error should mention config file not found
	if !os.IsNotExist(err) && !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got %v", err)
	}
}

func TestStartGatewayWithMockStat(t *testing.T) {
	cli := New()
	cli.configPath = "test.yaml"
	
	mockStat := func(name string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}
	
	mockRunner := func(ctx context.Context, configPath string) error {
		return nil
	}
	
	err := cli.startGateway([]string{}, mockRunner, mockStat)
	
	if err == nil {
		t.Error("expected error for missing config file")
	}
	
	// The error should mention config file not found
	if !os.IsNotExist(err) && !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got %v", err)
	}
}

func TestStartGatewaySuccess(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	
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
	
	mockRunner := &MockAppRunner{
		RunFunc: func(ctx context.Context, configPath string) error {
			return nil
		},
	}
	
	// Create a context that will cancel quickly for testing
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	
	// Use a runner that respects context cancellation
	runner := func(rctx context.Context, cp string) error {
		<-ctx.Done()
		return ctx.Err()
	}
	
	err := cli.startGateway([]string{}, runner, os.Stat)
	
	// We expect a context deadline exceeded or similar error
	if err == nil {
		t.Error("expected some error from runner")
	}
	
	_ = mockRunner
}

func TestCmdValidateConfigNotFound(t *testing.T) {
	cli := New()
	cli.configPath = "/nonexistent/path/config.yaml"
	
	err := cli.cmdValidate([]string{})
	
	if err == nil {
		t.Error("expected error for missing config file")
	}
}

func TestCmdValidateInvalidConfig(t *testing.T) {
	// Create a temporary config file with invalid content
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	
	configContent := `
listen: ":8080"
admin:
  enabled: true
  auth_token: "short"  # Too short, will fail validation
upstreams:
  - name: test
    base_url: http://localhost:8081
    api_key: test-key
    models:
      - model1
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to create test config: %v", err)
	}
	
	cli := New()
	cli.configPath = configPath
	
	err := cli.cmdValidate([]string{})
	
	if err == nil {
		t.Error("expected error for invalid config")
	}
}

func TestCmdValidateSuccess(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	
	configContent := `
listen: ":8080"
admin:
  enabled: true
  auth_token: "this-is-a-secure-token-that-is-long-enough-32chars"
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
	
	err := cli.cmdValidate([]string{})
	
	if err != nil {
		t.Errorf("expected no error for valid config, got %v", err)
	}
}

func TestValidateConfigFunction(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	
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
	
	err := cli.validateConfig([]string{})
	
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCmdConfig(t *testing.T) {
	cli := New()
	
	err := cli.cmdConfig([]string{})
	
	if err == nil {
		t.Error("expected error from cmdConfig")
	}
	
	if err.Error() != "config command should be handled by flags" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestCmdHealth(t *testing.T) {
	// cmdHealth delegates to checkHealth which uses the default HTTP client
	// We can't easily test this without a mock server, but we can verify it doesn't panic
	
	cli := New()
	
	// This will fail because there's no server running, but we can check it doesn't panic
	// The actual call would exit via os.Exit, so we can't test the full path
	t.Skip("cmdHealth calls checkHealth which uses os.Exit on failure")
	_ = cli
}

func TestStartGatewayRunnerCalled(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	
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
	
	runnerCalled := false
	var runnerPath string
	
	runner := func(ctx context.Context, cp string) error {
		runnerCalled = true
		runnerPath = cp
		return nil
	}
	
	err := cli.startGateway([]string{}, runner, os.Stat)
	
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	
	if !runnerCalled {
		t.Error("expected runner to be called")
	}
	
	if runnerPath != configPath {
		t.Errorf("expected path '%s', got '%s'", configPath, runnerPath)
	}
}

func TestStartGatewayRunnerError(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	
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
	
	expectedErr := errors.New("runner error")
	runner := func(ctx context.Context, cp string) error {
		return expectedErr
	}
	
	err := cli.startGateway([]string{}, runner, os.Stat)
	
	if err != expectedErr {
		t.Errorf("expected error '%v', got '%v'", expectedErr, err)
	}
}
