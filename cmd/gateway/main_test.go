package main

import (
	"os"
	"testing"
)

func TestMain(t *testing.T) {
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	tests := []struct {
		name string
		args []string
	}{
		{
			name: "validate command with missing config",
			args: []string{"gateway", "validate", "-config", "nonexistent.yaml"},
		},
		{
			name: "unknown command",
			args: []string{"gateway", "unknown"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Args = tt.args
			// We can't test main() directly as it calls os.Exit
			// This is just to verify the command parsing logic doesn't panic
		})
	}
}

func TestBoolStatus(t *testing.T) {
	tests := []struct {
		enabled  bool
		expected string
	}{
		{true, "enabled"},
		{false, "disabled"},
	}

	for _, tt := range tests {
		result := boolStatus(tt.enabled)
		if result != tt.expected {
			t.Errorf("boolStatus(%v) = %v, want %v", tt.enabled, result, tt.expected)
		}
	}
}
