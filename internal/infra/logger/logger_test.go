package logger

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestDefaultLogger(t *testing.T) {
	if DefaultLogger == nil {
		t.Fatal("DefaultLogger should be initialized")
	}
}

func TestSetLevel(t *testing.T) {
	var buf bytes.Buffer
	original := DefaultLogger
	defer func() { DefaultLogger = original }()

	DefaultLogger = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	Debug("test debug", "key", "value")
	if !strings.Contains(buf.String(), "test debug") {
		t.Error("Debug message should be logged at debug level")
	}

	buf.Reset()
	SetLevel(slog.LevelInfo)
	Debug("should not appear")
	if buf.String() != "" {
		t.Error("Debug should be suppressed at info level")
	}
}

func TestLogFunctions(t *testing.T) {
	var buf bytes.Buffer
	original := DefaultLogger
	defer func() { DefaultLogger = original }()

	DefaultLogger = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	tests := []struct {
		name    string
		logFunc func(string, ...any)
		msg     string
	}{
		{"Debug", Debug, "debug message"},
		{"Info", Info, "info message"},
		{"Warn", Warn, "warn message"},
		{"Error", Error, "error message"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			tt.logFunc(tt.msg, "key", "value")
			if !strings.Contains(buf.String(), tt.msg) {
				t.Errorf("%s message not found in output", tt.name)
			}
		})
	}
}

func TestWith(t *testing.T) {
	logger := With("component", "test")
	if logger == nil {
		t.Fatal("With should return a logger")
	}
}
