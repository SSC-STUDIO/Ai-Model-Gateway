//go:build windows

package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// Test to cover more lines in service_windows.go
func TestInstallServiceCreateError(t *testing.T) {
	cli := New()

	// Create mock that returns nil on OpenService (not installed)
	// but fails on CreateService
	mockConn := &MockServiceManagerConnection{
		OpenServiceFunc: func(name string) (ServiceHandle, error) {
			return nil, errors.New("not found")
		},
		CreateServiceFunc: func(name, exepath string, config mgr.Config, args ...string) (ServiceHandle, error) {
			return nil, errors.New("create failed")
		},
	}

	mockManager := &MockServiceManager{
		ConnectFunc: func() (ServiceManagerConnection, error) {
			return mockConn, nil
		},
	}

	provider := NewServiceManagerProviderWithManager(mockManager)

	err := cli.installService(provider)

	if err == nil {
		t.Error("expected error for create service failure")
	}
}

func TestInstallServiceDisconnect(t *testing.T) {
	mockConn := &MockServiceManagerConnection{
		DisconnectFunc: func() error {
			return nil
		},
	}

	err := mockConn.Disconnect()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestServiceHandleClose(t *testing.T) {
	mockHandle := &MockServiceHandle{
		CloseFunc: func() error {
			return nil
		},
	}

	err := mockHandle.Close()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

// Test for serviceStateString to cover more cases
func TestServiceStateStringAllStates(t *testing.T) {
	states := []struct {
		state svc.State
		want  string
	}{
		{svc.Stopped, "Stopped"},
		{svc.StartPending, "Starting..."},
		{svc.StopPending, "Stopping..."},
		{svc.Running, "Running"},
		{svc.ContinuePending, "Resuming..."},
		{svc.PausePending, "Pausing..."},
		{svc.Paused, "Paused"},
		{svc.State(12345), "Unknown(12345)"},
	}

	for _, tc := range states {
		got := serviceStateString(tc.state)
		if got != tc.want {
			t.Errorf("serviceStateString(%d) = %q, want %q", tc.state, got, tc.want)
		}
	}
}

// Test for mock HTTP client with various responses
func TestMockHTTPClientDo(t *testing.T) {
	tests := []struct {
		name    string
		doFunc  func(req *http.Request) (*http.Response, error)
		wantErr bool
	}{
		{
			name: "success",
			doFunc: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("OK")),
				}, nil
			},
			wantErr: false,
		},
		{
			name:    "default error",
			doFunc:  nil,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock := &MockHTTPClient{DoFunc: tc.doFunc}
			req, _ := http.NewRequest("GET", "http://localhost", nil)

			resp, err := mock.Do(req)

			if tc.wantErr {
				if err == nil {
					t.Error("expected error")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if resp == nil {
					t.Error("expected response")
				}
			}
		})
	}
}

// Test for health check with partial JSON data
func TestHealthCheckerPartialData(t *testing.T) {
	tests := []struct {
		name     string
		jsonData string
	}{
		{
			name:     "only status",
			jsonData: `{"status": "healthy"}`,
		},
		{
			name:     "only router_strategy",
			jsonData: `{"router_strategy": "round_robin"}`,
		},
		{
			name:     "only bridge",
			jsonData: `{"bridge": {"enabled": true}}`,
		},
		{
			name:     "only models",
			jsonData: `{"available_models": ["model1", "model2", "model3"]}`,
		},
		{
			name:     "only upstreams",
			jsonData: `{"upstreams": {"u1": {}, "u2": {}}}`,
		},
		{
			name:     "empty object",
			jsonData: `{}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockClient := &MockHTTPClient{
				DoFunc: func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(tc.jsonData)),
						Header:     make(http.Header),
					}, nil
				},
			}

			hc := NewHealthCheckerWithClient(mockClient)
			err := hc.Check([]string{})

			if err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

// Test for empty health response fields
func TestHealthCheckerEmptyFields(t *testing.T) {
	jsonData := `{
		"status": "",
		"router_strategy": "",
		"bridge": null,
		"available_models": [],
		"upstreams": {}
	}`

	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(jsonData)),
				Header:     make(http.Header),
			}, nil
		},
	}

	hc := NewHealthCheckerWithClient(mockClient)
	err := hc.Check([]string{})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

// Test for CLI with different config paths
func TestCLIDifferentConfigPaths(t *testing.T) {
	tests := []struct {
		name       string
		configPath string
	}{
		{
			name:       "absolute path",
			configPath: "/etc/gateway/config.yaml",
		},
		{
			name:       "relative path",
			configPath: "./config.yaml",
		},
		{
			name:       "path with spaces",
			configPath: "my config/config.yaml",
		},
		{
			name:       "path with parent dir",
			configPath: "../config/config.yaml",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cli := NewWithOptions(tc.configPath, func(int) {})

			if cli.GetConfigPath() != tc.configPath {
				t.Errorf("expected config path '%s', got '%s'", tc.configPath, cli.GetConfigPath())
			}
		})
	}
}

// Test commands are properly initialized
func TestCommandInitialization(t *testing.T) {
	cli := New()

	commands := []struct {
		name     string
		hasFlags bool
	}{
		{"start", true},
		{"validate", false},
		{"config", true},
		{"health", true},
		{"install", false},
		{"uninstall", false},
		{"service-start", false},
		{"service-stop", false},
		{"service-status", false},
	}

	for _, tc := range commands {
		t.Run(tc.name, func(t *testing.T) {
			cmd, ok := cli.commands[tc.name]
			if !ok {
				t.Fatalf("command '%s' not found", tc.name)
			}

			if cmd.Name != tc.name {
				t.Errorf("expected name '%s', got '%s'", tc.name, cmd.Name)
			}

			if tc.hasFlags && cmd.Flags == nil {
				t.Error("expected flags to be set")
			}

			if cmd.Run == nil {
				t.Error("expected Run to be set")
			}
		})
	}
}

// Test mock app runner
func TestMockAppRunner(t *testing.T) {
	runner := &MockAppRunner{
		RunFunc: func(ctx context.Context, configPath string) error {
			return nil
		},
	}

	ctx := context.Background()
	err := runner.Run(ctx, "/test/config.yaml")

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if !runner.Called {
		t.Error("expected runner to be called")
	}

	if runner.Path != "/test/config.yaml" {
		t.Errorf("expected path '/test/config.yaml', got '%s'", runner.Path)
	}
}

// Test for error cases in flag parsing
func TestCLIRunFlagParseError(t *testing.T) {
	cli := New()

	// Create a command with invalid flag
	cmd := &Command{
		Name:        "badflag",
		Description: "Command with bad flag",
		Flags:       func() *flag.FlagSet { fs := flag.NewFlagSet("badflag", flag.ContinueOnError); return fs }(),
		Run:         func(args []string) error { return nil },
	}
	cli.Register(cmd)

	err := cli.Run([]string{"badflag", "--invalid-flag"})

	// This will fail because the flag is not defined, but it should not panic
	if err == nil {
		t.Error("expected error for invalid flag")
	}
}

// Test to cover ContextWithTimeout
func TestContextWithTimeoutCancellation(t *testing.T) {
	ctx, cancel := ContextWithTimeout(1 * time.Second)

	// Test that cancel function works
	cancel()

	select {
	case <-ctx.Done():
		// Expected - context should be cancelled
	default:
		t.Error("expected context to be cancelled")
	}
}

// Test for health checker with different timeouts
func TestHealthCheckerTimeouts(t *testing.T) {
	timeouts := []string{
		"1s",
		"5s",
		"10s",
		"1m",
	}

	for _, timeout := range timeouts {
		t.Run(timeout, func(t *testing.T) {
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
			err := hc.Check([]string{"-timeout", timeout})

			if err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}
