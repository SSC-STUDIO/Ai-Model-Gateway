// Package logger provides structured logging for the gateway.
package logger

import (
	"log/slog"
	"os"
	"sync"
)

var (
	// DefaultLogger is the default slog instance.
	DefaultLogger *slog.Logger

	// Level controls the logging verbosity.
	Level = slog.LevelInfo

	// mu protects concurrent access to DefaultLogger and Level.
	mu sync.RWMutex
)

func init() {
	DefaultLogger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: Level,
	}))
}

// SetLevel changes the global logging level.
func SetLevel(lvl slog.Level) {
	mu.Lock()
	defer mu.Unlock()
	Level = lvl
	DefaultLogger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: Level,
	}))
}

// Debug logs a debug message.
func Debug(msg string, args ...any) {
	mu.RLock()
	l := DefaultLogger
	mu.RUnlock()
	l.Debug(msg, args...)
}

// Info logs an info message.
func Info(msg string, args ...any) {
	mu.RLock()
	l := DefaultLogger
	mu.RUnlock()
	l.Info(msg, args...)
}

// Warn logs a warning message.
func Warn(msg string, args ...any) {
	mu.RLock()
	l := DefaultLogger
	mu.RUnlock()
	l.Warn(msg, args...)
}

// Error logs an error message.
func Error(msg string, args ...any) {
	mu.RLock()
	l := DefaultLogger
	mu.RUnlock()
	l.Error(msg, args...)
}

// With returns a logger with additional context.
func With(args ...any) *slog.Logger {
	mu.RLock()
	l := DefaultLogger
	mu.RUnlock()
	return l.With(args...)
}
