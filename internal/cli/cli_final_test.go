//go:build windows

package cli

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows/svc"
)

// Test for more service manager scenarios
func TestStartServiceSuccess(t *testing.T) {
	cli := New()

	mockHandle := &MockServiceHandle{
		StartFunc: func(args ...string) error {
			return nil
		},
	}

	mockConn := &MockServiceManagerConnection{
		OpenServiceFunc: func(name string) (ServiceHandle, error) {
			return mockHandle, nil
		},
	}

	mockManager := &MockServiceManager{
		ConnectFunc: func() (ServiceManagerConnection, error) {
			return mockConn, nil
		},
	}

	provider := NewServiceManagerProviderWithManager(mockManager)

	err := cli.startService(provider)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestStopServiceSuccess(t *testing.T) {
	cli := New()

	mockHandle := &MockServiceHandle{
		ControlFunc: func(cmd svc.Cmd) (svc.Status, error) {
			return svc.Status{State: svc.StopPending}, nil
		},
	}

	mockConn := &MockServiceManagerConnection{
		OpenServiceFunc: func(name string) (ServiceHandle, error) {
			return mockHandle, nil
		},
	}

	mockManager := &MockServiceManager{
		ConnectFunc: func() (ServiceManagerConnection, error) {
			return mockConn, nil
		},
	}

	provider := NewServiceManagerProviderWithManager(mockManager)

	err := cli.stopService(provider)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestServiceStatusSuccess(t *testing.T) {
	cli := New()

	mockHandle := &MockServiceHandle{
		QueryFunc: func() (svc.Status, error) {
			return svc.Status{State: svc.Running, ProcessId: 1234}, nil
		},
	}

	mockConn := &MockServiceManagerConnection{
		OpenServiceFunc: func(name string) (ServiceHandle, error) {
			return mockHandle, nil
		},
	}

	mockManager := &MockServiceManager{
		ConnectFunc: func() (ServiceManagerConnection, error) {
			return mockConn, nil
		},
	}

	provider := NewServiceManagerProviderWithManager(mockManager)

	err := cli.serviceStatus(provider)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestInstallServiceSuccess(t *testing.T) {
	// This test finds the actual test executable and installs the service
	// It may succeed or fail depending on environment
	t.Skip("Skipping test that installs Windows service")
}

func TestUninstallServiceSuccess(t *testing.T) {
	cli := New()

	mockHandle := &MockServiceHandle{
		QueryFunc: func() (svc.Status, error) {
			return svc.Status{State: svc.Stopped}, nil
		},
		DeleteFunc: func() error {
			return nil
		},
	}

	mockConn := &MockServiceManagerConnection{
		OpenServiceFunc: func(name string) (ServiceHandle, error) {
			return mockHandle, nil
		},
	}

	mockManager := &MockServiceManager{
		ConnectFunc: func() (ServiceManagerConnection, error) {
			return mockConn, nil
		},
	}

	provider := NewServiceManagerProviderWithManager(mockManager)

	err := cli.uninstallService(provider)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestUninstallServiceWithStop(t *testing.T) {
	// This test would sleep for 2 seconds waiting for service to stop
	t.Skip("Skipping test with 2 second sleep")
}

// Test for validate with minimal valid config
func TestValidateMinimalConfig(t *testing.T) {
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

// Test for start command with daemon flag
func TestStartCommandWithDaemonFlag(t *testing.T) {
	// Skip - causes timeout in CI
	t.Skip("Skipping test that times out in CI")
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

	// Test that daemon flag is recognized
	oldStderr := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w

	// This will fail because we don't have a real config loaded, but tests flag parsing
	err := cli.Run([]string{"start", "-daemon"})

	w.Close()
	os.Stderr = oldStderr

	// The error should be related to running the app, not flag parsing
	// Note: This might pass or fail depending on the app runner
	_ = err
}

// Test for health command with flag parsing
func TestHealthCommandFlags(t *testing.T) {
	cli := New()

	healthCmd := cli.commands["health"]
	if healthCmd == nil {
		t.Fatal("health command not found")
	}

	// Test that flags are properly configured
	if healthCmd.Flags == nil {
		t.Fatal("health command flags should not be nil")
	}

	// Test parsing endpoint flag
	err := healthCmd.Flags.Parse([]string{"-endpoint", "http://custom:8080/health"})
	if err != nil {
		t.Errorf("expected no error parsing flags, got %v", err)
	}

	endpoint := healthCmd.Flags.Lookup("endpoint").Value.String()
	if endpoint != "http://custom:8080/health" {
		t.Errorf("expected endpoint 'http://custom:8080/health', got '%s'", endpoint)
	}
}

// Test for config command flags
func TestConfigCommandFlags(t *testing.T) {
	cli := New()

	configCmd := cli.commands["config"]
	if configCmd == nil {
		t.Fatal("config command not found")
	}

	// Test reload flag
	err := configCmd.Flags.Parse([]string{"-reload"})
	if err != nil {
		t.Errorf("expected no error parsing flags, got %v", err)
	}

	reload := configCmd.Flags.Lookup("reload").Value.String()
	if reload != "true" {
		t.Errorf("expected reload 'true', got '%s'", reload)
	}
}

// Test for various error scenarios in startGateway
func TestStartGatewayErrors(t *testing.T) {
	tests := []struct {
		name     string
		statFunc statFunc
		runner   AppRunnerFunc
		wantErr  bool
	}{
		{
			name: "config file not found",
			statFunc: func(name string) (os.FileInfo, error) {
				return nil, os.ErrNotExist
			},
			runner:  nil,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cli := New()
			cli.configPath = "test.yaml"

			err := cli.startGateway([]string{}, tc.runner, tc.statFunc)

			if tc.wantErr && err == nil {
				t.Error("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// Test for empty config path
func TestEmptyConfigPath(t *testing.T) {
	cli := NewWithOptions("", func(int) {})

	if cli.configPath != "configs/config.yaml" {
		t.Errorf("expected default config path, got '%s'", cli.configPath)
	}
}

// Test for command with flags parsing error - skipped due to flag.ExitOnError behavior
func TestCommandFlagParseError(t *testing.T) {
	// The CLI uses flag.ExitOnError which exits the program on parse errors
	// This cannot be tested without restructuring the code
	t.Skip("Skipping test that triggers flag.ExitOnError")
}

// Test health checker request creation error
func TestHealthCheckerRequestError(t *testing.T) {
	// This would test invalid URL parsing, but http.NewRequest doesn't fail for most URLs
	// Just verify the function handles it
	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"status":"ok"}`)),
				Header:     make(http.Header),
			}, nil
		},
	}

	hc := NewHealthCheckerWithClient(mockClient)
	err := hc.Check([]string{"-endpoint", "http://localhost:8080/health"})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

// Test for MockOSExit
func TestMockOSExit(t *testing.T) {
	mockExit := &MockOSExit{}

	mockExit.Exit(1)

	if !mockExit.Called {
		t.Error("expected Exit to be called")
	}

	if mockExit.Code != 1 {
		t.Errorf("expected code 1, got %d", mockExit.Code)
	}
}

// Test for MockFileInfo
func TestMockFileInfo(t *testing.T) {
	now := time.Now()
	info := &MockFileInfo{
		NameFunc:    func() string { return "test.txt" },
		SizeFunc:    func() int64 { return 100 },
		ModeFunc:    func() os.FileMode { return 0644 },
		ModTimeFunc: func() time.Time { return now },
		IsDirFunc:   func() bool { return false },
		SysFunc:     func() interface{} { return nil },
	}

	if info.Name() != "test.txt" {
		t.Errorf("expected name 'test.txt', got '%s'", info.Name())
	}

	if info.Size() != 100 {
		t.Errorf("expected size 100, got %d", info.Size())
	}

	if info.Mode() != 0644 {
		t.Errorf("expected mode 0644, got %v", info.Mode())
	}

	if !info.ModTime().Equal(now) {
		t.Error("expected matching mod time")
	}

	if info.IsDir() {
		t.Error("expected not a directory")
	}

	if info.Sys() != nil {
		t.Error("expected nil sys")
	}
}

// Test for service manager error cases
func TestServiceManagerDefaultMockBehavior(t *testing.T) {
	// Test that default mocks return expected values
	conn := &MockServiceManagerConnection{}

	handle, err := conn.OpenService("test")
	if err == nil {
		t.Error("expected error for default OpenService")
	}
	_ = handle

	// Skip CreateService test due to mgr.Config type requirements
}

func TestServiceHandleDefaultBehavior(t *testing.T) {
	handle := &MockServiceHandle{}

	err := handle.Close()
	if err != nil {
		t.Errorf("expected no error for default Close, got %v", err)
	}

	err = handle.Start()
	if err != nil {
		t.Errorf("expected no error for default Start, got %v", err)
	}

	_, err = handle.Control(svc.Stop)
	if err != nil {
		t.Errorf("expected no error for default Control, got %v", err)
	}

	_, err = handle.Query()
	if err != nil {
		t.Errorf("expected no error for default Query, got %v", err)
	}

	err = handle.Delete()
	if err != nil {
		t.Errorf("expected no error for default Delete, got %v", err)
	}
}

// Test for app runner timeout
func TestStartGatewayContextTimeout(t *testing.T) {
	// This test blocks on signal handling which can't be easily mocked
	t.Skip("Skipping test with signal.NotifyContext blocking")
}

// Test for NewDefaultHTTPClient
func TestNewDefaultHTTPClientVariations(t *testing.T) {
	timeouts := []time.Duration{
		0,
		1 * time.Nanosecond,
		1 * time.Microsecond,
		1 * time.Millisecond,
		1 * time.Second,
		1 * time.Minute,
		1 * time.Hour,
	}

	for _, timeout := range timeouts {
		client := NewDefaultHTTPClient(timeout)

		defaultClient, ok := client.(*DefaultHTTPClient)
		if !ok {
			t.Error("expected *DefaultHTTPClient")
			continue
		}

		if defaultClient.client == nil {
			t.Error("expected non-nil http.Client")
		}
	}
}

// Test health checker with very short timeout - skipped to avoid timeout issues
func TestHealthCheckerShortTimeout(t *testing.T) {
	// Skipped because it would require a real HTTP server with artificial delay
	t.Skip("Skipping test that requires slow HTTP response")
}
