package cli

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"os"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	cli := New()
	if cli == nil {
		t.Fatal("New() returned nil")
	}
	if cli.commands == nil {
		t.Error("commands map is nil")
	}
	if cli.configPath != "configs/config.yaml" {
		t.Errorf("expected default config path 'configs/config.yaml', got '%s'", cli.configPath)
	}
	if cli.osExit == nil {
		t.Error("osExit is nil")
	}

	// Check that commands were registered
	expectedCommands := []string{"start", "validate", "config", "health", "install", "uninstall", "service-start", "service-stop", "service-status"}
	for _, cmdName := range expectedCommands {
		if _, ok := cli.commands[cmdName]; !ok {
			t.Errorf("expected command '%s' to be registered", cmdName)
		}
	}
}

func TestNewWithOptions(t *testing.T) {
	mockExit := func(int) {}
	cli := NewWithOptions("custom/config.yaml", mockExit)

	if cli.configPath != "custom/config.yaml" {
		t.Errorf("expected config path 'custom/config.yaml', got '%s'", cli.configPath)
	}
	if cli.osExit == nil {
		t.Error("osExit should not be nil")
	}
}

func TestNewWithOptionsDefaults(t *testing.T) {
	cli := NewWithOptions("", nil)

	if cli.configPath != "configs/config.yaml" {
		t.Errorf("expected default config path, got '%s'", cli.configPath)
	}
	if cli.osExit == nil {
		t.Error("osExit should not be nil")
	}
}

func TestCLIRegister(t *testing.T) {
	cli := New()

	cmd := &Command{
		Name:        "test",
		Description: "Test command",
		Usage:       "test",
		Run:         func(args []string) error { return nil },
	}

	cli.Register(cmd)

	if _, ok := cli.commands["test"]; !ok {
		t.Error("expected command 'test' to be registered")
	}
}

func TestCLIRunNoArgs(t *testing.T) {
	cli := New()

	// Capture stderr
	oldStderr := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w

	err := cli.Run([]string{})

	w.Close()
	os.Stderr = oldStderr

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCLIRunHelp(t *testing.T) {
	cli := New()

	tests := []struct {
		name string
		args []string
	}{
		{"help command", []string{"help"}},
		{"-h flag", []string{"-h"}},
		{"--help flag", []string{"--help"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			err := cli.Run(tt.args)

			w.Close()
			os.Stderr = oldStderr

			var buf bytes.Buffer
			io.Copy(&buf, r)
			output := buf.String()

			if err != nil {
				t.Errorf("expected no error, got %v", err)
			}

			if !strings.Contains(output, "Usage:") {
				t.Error("expected usage message to be printed")
			}
		})
	}
}

func TestCLIRunVersion(t *testing.T) {
	cli := New()

	tests := []struct {
		name string
		args []string
	}{
		{"version command", []string{"version"}},
		{"-v flag", []string{"-v"}},
		{"--version flag", []string{"--version"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			err := cli.Run(tt.args)

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			io.Copy(&buf, r)
			output := buf.String()

			if err != nil {
				t.Errorf("expected no error, got %v", err)
			}

			if !strings.Contains(output, "AI Model Gateway version") {
				t.Error("expected version message to be printed")
			}
		})
	}
}

func TestCLIRunUnknownCommand(t *testing.T) {
	cli := New()

	err := cli.Run([]string{"unknown-command"})

	if err == nil {
		t.Error("expected error for unknown command")
	}

	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("expected 'unknown command' error, got %v", err)
	}
}

func TestCLIRunWithConfigFlag(t *testing.T) {
	cli := New()

	// Test setting config path
	oldStderr := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w

	err := cli.Run([]string{"-config", "/custom/path.yaml", "help"})

	w.Close()
	os.Stderr = oldStderr

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if cli.configPath != "/custom/path.yaml" {
		t.Errorf("expected config path '/custom/path.yaml', got '%s'", cli.configPath)
	}
}

func TestCLIGetConfigPath(t *testing.T) {
	cli := New()
	cli.configPath = "test/config.yaml"

	path := cli.GetConfigPath()
	if path != "test/config.yaml" {
		t.Errorf("expected 'test/config.yaml', got '%s'", path)
	}
}

func TestCLISetConfigPath(t *testing.T) {
	cli := New()
	cli.SetConfigPath("new/path.yaml")

	if cli.configPath != "new/path.yaml" {
		t.Errorf("expected 'new/path.yaml', got '%s'", cli.configPath)
	}
}

func TestCLIGetCommands(t *testing.T) {
	cli := New()

	commands := cli.GetCommands()

	// Should return a copy
	if len(commands) != len(cli.commands) {
		t.Error("GetCommands should return all commands")
	}

	// Modifying the returned map should not affect the original
	commands["test"] = &Command{Name: "test"}
	if _, ok := cli.commands["test"]; ok {
		t.Error("modifying returned map should not affect original")
	}
}

func TestPrintCommandHelp(t *testing.T) {
	tests := []struct {
		name     string
		cmdName  string
		wantExit bool
	}{
		{"existing command", "start", false},
		{"unknown command", "unknown", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockExit := &MockOSExit{}
			cli := NewWithOptions("", mockExit.Exit)

			oldStderr := os.Stderr
			_, w, _ := os.Pipe()
			os.Stderr = w

			cli.printCommandHelp(tt.cmdName)

			w.Close()
			os.Stderr = oldStderr

			if tt.wantExit && !mockExit.Called {
				t.Error("expected os.Exit to be called for unknown command")
			}
			if !tt.wantExit && mockExit.Called {
				t.Error("expected os.Exit not to be called for existing command")
			}
		})
	}
}

func TestCLIRunCommandWithFlags(t *testing.T) {
	cli := New()

	// Register a test command that uses flags
	called := false
	var receivedArgs []string

	testCmd := &Command{
		Name:        "test",
		Description: "Test command with flags",
		Usage:       "test [-verbose]",
		Flags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			fs.Bool("verbose", false, "Verbose output")
			return fs
		}(),
		Run: func(args []string) error {
			called = true
			receivedArgs = args
			return nil
		},
	}
	cli.Register(testCmd)

	err := cli.Run([]string{"test", "-verbose", "arg1", "arg2"})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if !called {
		t.Error("expected command to be called")
	}

	if len(receivedArgs) != 2 || receivedArgs[0] != "arg1" || receivedArgs[1] != "arg2" {
		t.Errorf("expected args [arg1 arg2], got %v", receivedArgs)
	}
}

func TestCLIRunCommandError(t *testing.T) {
	cli := New()

	testError := errors.New("test error")
	testCmd := &Command{
		Name:        "error",
		Description: "Command that returns error",
		Run: func(args []string) error {
			return testError
		},
	}
	cli.Register(testCmd)

	err := cli.Run([]string{"error"})

	if err != testError {
		t.Errorf("expected test error, got %v", err)
	}
}

func TestCLIRunStartDefault(t *testing.T) {
	cli := New()

	// When no command is given but there's an arg that looks like a flag,
	// it should default to start command
	// We can't easily test this without a valid config file, but we can verify
	// the command resolution logic

	oldStderr := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w

	// Pass an unknown flag which should trigger the default "start" behavior
	err := cli.Run([]string{"-unknownflag"})

	w.Close()
	os.Stderr = oldStderr

	// Should return an error because config doesn't exist
	if err == nil {
		t.Error("expected error for invalid flag")
	}
}
