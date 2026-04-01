package cli

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Test for nil flag set handling
func TestCLIRunWithNilFlags(t *testing.T) {
	cli := New()

	// Register a command with nil flags
	cmd := &Command{
		Name:        "noflags",
		Description: "Command with no flags",
		Flags:       nil,
		Run: func(args []string) error {
			return nil
		},
	}
	cli.Register(cmd)

	err := cli.Run([]string{"noflags", "arg1", "arg2"})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

// Test for command that returns an error
func TestCLIRunCommandReturnsError(t *testing.T) {
	cli := New()

	expectedErr := errors.New("command failed")
	cmd := &Command{
		Name:        "failcmd",
		Description: "Command that fails",
		Run: func(args []string) error {
			return expectedErr
		},
	}
	cli.Register(cmd)

	err := cli.Run([]string{"failcmd"})

	if err != expectedErr {
		t.Errorf("expected error '%v', got '%v'", expectedErr, err)
	}
}

// Test GetCommands returns a copy
func TestGetCommandsIsCopy(t *testing.T) {
	cli := New()

	originalCount := len(cli.commands)
	commands := cli.GetCommands()

	// Modify the returned map
	delete(commands, "start")

	// Original should be unchanged
	if len(cli.commands) != originalCount {
		t.Error("GetCommands should return a copy, not modify original")
	}
}

// Test for empty command name
func TestEmptyCommandName(t *testing.T) {
	cmd := &Command{
		Name:        "",
		Description: "Empty name command",
		Run:         func(args []string) error { return nil },
	}

	if cmd.Name != "" {
		t.Error("expected empty name")
	}
}

// Test for multiple command registrations
func TestMultipleRegister(t *testing.T) {
	cli := New()

	// Register the same command multiple times
	cmd := &Command{
		Name:        "duplicate",
		Description: "First registration",
		Run:         func(args []string) error { return nil },
	}

	cli.Register(cmd)

	cmd2 := &Command{
		Name:        "duplicate",
		Description: "Second registration",
		Run:         func(args []string) error { return nil },
	}

	cli.Register(cmd2)

	// Should have the second command
	if cli.commands["duplicate"].Description != "Second registration" {
		t.Error("expected second registration to overwrite first")
	}
}

// Test for config flag parsing
func TestCLIRunWithConfigFlagOnly(t *testing.T) {
	cli := New()

	// Test with only -config flag and no command
	oldStderr := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w

	err := cli.Run([]string{"-config", "/test/config.yaml"})

	w.Close()
	os.Stderr = oldStderr

	// Should print usage because no command provided
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

// Test health check with various JSON structures
func TestHealthCheckerVariousJSON(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{
			name: "nested objects",
			json: `{"status":"ok","details":{"cpu":50,"memory":80}}`,
		},
		{
			name: "array values",
			json: `{"status":"ok","services":["api","db","cache"]}`,
		},
		{
			name: "boolean values",
			json: `{"status":"ok","healthy":true,"degraded":false}`,
		},
		{
			name: "numeric values",
			json: `{"status":"ok","uptime":3600,"requests":1000000}`,
		},
		{
			name: "null values",
			json: `{"status":"ok","error":null}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockClient := &MockHTTPClient{
				DoFunc: func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(tc.json)),
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

// Test health checker with different endpoints
func TestHealthCheckerEndpoints(t *testing.T) {
	endpoints := []string{
		"http://localhost:8080/health",
		"https://example.com/health",
		"http://127.0.0.1:9090/-/health",
		"http://10.0.0.1:8080/api/health",
	}

	for _, endpoint := range endpoints {
		t.Run(endpoint, func(t *testing.T) {
			capturedEndpoint := ""
			mockClient := &MockHTTPClient{
				DoFunc: func(req *http.Request) (*http.Response, error) {
					capturedEndpoint = req.URL.String()
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"status":"ok"}`)),
						Header:     make(http.Header),
					}, nil
				},
			}

			hc := NewHealthCheckerWithClient(mockClient)
			err := hc.Check([]string{"-endpoint", endpoint})

			if err != nil {
				t.Errorf("expected no error, got %v", err)
			}

			if capturedEndpoint != endpoint {
				t.Errorf("expected endpoint '%s', got '%s'", endpoint, capturedEndpoint)
			}
		})
	}
}

// Test for very large JSON response
func TestHealthCheckerLargeJSON(t *testing.T) {
	// Create a large JSON response
	var models []string
	for i := 0; i < 1000; i++ {
		models = append(models, `"model-"`+string(rune('0'+i%10)))
	}

	jsonData := `{"status":"ok","available_models":[` + strings.Join(models, ",") + `]}`

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

// Test printVersion with environment variable
func TestPrintVersionWithEnvVar(t *testing.T) {
	// Save original value
	origVersion := os.Getenv("GATEWAY_VERSION")
	defer os.Setenv("GATEWAY_VERSION", origVersion)

	tests := []struct {
		name    string
		version string
		want    string
	}{
		{"version 1.0.0", "1.0.0", "1.0.0"},
		{"version 2.5.3", "2.5.3", "2.5.3"},
		{"empty version", "", "dev"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			os.Setenv("GATEWAY_VERSION", tc.version)

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

			if !strings.Contains(output, tc.want) {
				t.Errorf("expected output to contain '%s', got '%s'", tc.want, output)
			}
		})
	}
}

// Test CLI with very long config path
func TestCLILongConfigPath(t *testing.T) {
	cli := New()

	// Create a very long path
	longPath := "/very/long/path" + strings.Repeat("/nested", 50) + "/config.yaml"

	oldStderr := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w

	err := cli.Run([]string{"-config", longPath, "help"})

	w.Close()
	os.Stderr = oldStderr

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if cli.configPath != longPath {
		t.Errorf("expected config path '%s', got '%s'", longPath, cli.configPath)
	}
}

// Test for command that panics
func TestCommandPanic(t *testing.T) {
	cli := New()

	cmd := &Command{
		Name:        "paniccmd",
		Description: "Command that panics",
		Run: func(args []string) error {
			panic("intentional panic")
		},
	}
	cli.Register(cmd)

	// This will panic - we need to recover
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()

	cli.Run([]string{"paniccmd"})
}

// Test for various flag parsing scenarios
func TestFlagParsingScenarios(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantError bool
	}{
		{
			name:      "no args after command",
			args:      []string{"start"},
			wantError: true, // will fail due to missing config
		},
		{
			name:      "multiple args",
			args:      []string{"validate", "extra", "args"},
			wantError: true, // validate doesn't take args but has config issue
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cli := New()

			oldStderr := os.Stderr
			_, w, _ := os.Pipe()
			os.Stderr = w

			err := cli.Run(tc.args)

			w.Close()
			os.Stderr = oldStderr

			if tc.wantError && err == nil {
				t.Error("expected error")
			}
		})
	}
}

// Test DefaultHTTPClient with various timeouts
func TestDefaultHTTPClientTimeouts(t *testing.T) {
	timeouts := []time.Duration{
		1 * time.Second,
		5 * time.Second,
		30 * time.Second,
		1 * time.Minute,
	}

	for _, timeout := range timeouts {
		t.Run(timeout.String(), func(t *testing.T) {
			client := NewDefaultHTTPClient(timeout)

			defaultClient, ok := client.(*DefaultHTTPClient)
			if !ok {
				t.Fatal("expected *DefaultHTTPClient")
			}

			if defaultClient.client.Timeout != timeout {
				t.Errorf("expected timeout %v, got %v", timeout, defaultClient.client.Timeout)
			}
		})
	}
}

// Test for nil HTTP client in health checker
func TestHealthCheckerNilClient(t *testing.T) {
	hc := NewHealthChecker()

	// client should be nil initially
	if hc.client != nil {
		t.Error("expected client to be nil")
	}
}

// Test for empty request to health checker
func TestHealthCheckerEmptyRequest(t *testing.T) {
	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			// Verify request method
			if req.Method != "GET" {
				t.Errorf("expected GET method, got %s", req.Method)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"status":"ok"}`)),
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

// Test command map access patterns
func TestCommandMapAccess(t *testing.T) {
	cli := New()

	// Test accessing all commands
	for name, cmd := range cli.commands {
		if cmd.Name != name {
			t.Errorf("command name mismatch: map key '%s' != cmd.Name '%s'", name, cmd.Name)
		}
	}
}

// Test for bridge field being a different type
func TestHealthCheckerBridgeTypeVariations(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{
			name: "bridge as object with extra fields",
			json: `{"status":"ok","bridge":{"enabled":true,"version":"1.0","features":["a","b"]}}`,
		},
		{
			name: "bridge as string",
			json: `{"status":"ok","bridge":"enabled"}`,
		},
		{
			name: "bridge as boolean",
			json: `{"status":"ok","bridge":true}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockClient := &MockHTTPClient{
				DoFunc: func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(tc.json)),
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

// Test for available_models type variations
func TestHealthCheckerModelsTypeVariations(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{
			name: "models as strings",
			json: `{"status":"ok","available_models":["gpt-4","gpt-3.5"]}`,
		},
		{
			name: "models as objects",
			json: `{"status":"ok","available_models":[{"name":"gpt-4"},{"name":"gpt-3.5"}]}`,
		},
		{
			name: "models as mixed",
			json: `{"status":"ok","available_models":["gpt-4",123,null]}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockClient := &MockHTTPClient{
				DoFunc: func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(tc.json)),
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

// Test for upstreams type variations
func TestHealthCheckerUpstreamsTypeVariations(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{
			name: "upstreams as map",
			json: `{"status":"ok","upstreams":{"u1":{"healthy":true},"u2":{"healthy":false}}}`,
		},
		{
			name: "upstreams as array",
			json: `{"status":"ok","upstreams":[{"name":"u1"},{"name":"u2"}]}`,
		},
		{
			name: "upstreams as number",
			json: `{"status":"ok","upstreams":5}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockClient := &MockHTTPClient{
				DoFunc: func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(tc.json)),
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
