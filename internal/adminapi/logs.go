package adminapi

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// LogEntry represents a single log entry
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	Source    string    `json:"source,omitempty"`
}

// LogStreamManager manages SSE log streaming connections
type LogStreamManager struct {
	mu          sync.RWMutex
	clients     map[chan LogEntry]struct{}
	maxClients  int
	logFilePath string
}

// NewLogStreamManager creates a new log stream manager
func NewLogStreamManager(logFilePath string) *LogStreamManager {
	return &LogStreamManager{
		clients:     make(map[chan LogEntry]struct{}),
		maxClients:  50,
		logFilePath: logFilePath,
	}
}

// SetMaxClients sets the maximum number of concurrent connections
func (m *LogStreamManager) SetMaxClients(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n > 0 {
		m.maxClients = n
	}
}

// Subscribe adds a new client channel
func (m *LogStreamManager) Subscribe() (chan LogEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.clients) >= m.maxClients {
		return nil, fmt.Errorf("max connections reached (%d)", m.maxClients)
	}

	ch := make(chan LogEntry, 100)
	m.clients[ch] = struct{}{}
	return ch, nil
}

// Unsubscribe removes a client channel
func (m *LogStreamManager) Unsubscribe(ch chan LogEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.clients[ch]; ok {
		delete(m.clients, ch)
		close(ch)
	}
}

// Broadcast sends a log entry to all connected clients
func (m *LogStreamManager) Broadcast(entry LogEntry) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for ch := range m.clients {
		select {
		case ch <- entry:
		default:
			// Channel full, skip this client
		}
	}
}

// LogLevelPriority returns the priority of a log level (higher = more important)
func LogLevelPriority(level string) int {
	switch strings.ToLower(level) {
	case "debug":
		return 1
	case "info":
		return 2
	case "warn", "warning":
		return 3
	case "error":
		return 4
	case "fatal":
		return 5
	default:
		return 0
	}
}

// ShouldIncludeLevel checks if a log level should be included based on minimum level
func ShouldIncludeLevel(level, minLevel string) bool {
	if minLevel == "" || minLevel == "debug" {
		return true
	}
	return LogLevelPriority(level) >= LogLevelPriority(minLevel)
}

// ParseLogLine parses a log line into a LogEntry
// Supports common log formats:
// - "2024-01-15T10:30:00Z [INFO] message"
// - "2024-01-15 10:30:00 [ERROR] source: message"
// - "INFO: message"
func ParseLogLine(line string) LogEntry {
	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   strings.TrimSpace(line),
	}

	if line == "" {
		return entry
	}

	// Try to parse timestamp at the beginning
	// ISO 8601 format: 2024-01-15T10:30:00Z or 2024-01-15T10:30:00.000Z
	iso8601Regex := regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z?)\s*`)
	if match := iso8601Regex.FindStringSubmatch(line); match != nil {
		if ts, err := time.Parse(time.RFC3339, match[1]); err == nil {
			entry.Timestamp = ts
			line = line[len(match[0]):]
		}
	}

	// Try to parse level: [LEVEL] or LEVEL:
	levelRegex := regexp.MustCompile(`^\[(DEBUG|INFO|WARN|WARNING|ERROR|FATAL)\]\s*`)
	if match := levelRegex.FindStringSubmatch(line); match != nil {
		entry.Level = strings.ToLower(match[1])
		if entry.Level == "warning" {
			entry.Level = "warn"
		}
		line = line[len(match[0]):]
	} else {
		// Try LEVEL: format
		levelPrefixRegex := regexp.MustCompile(`^(DEBUG|INFO|WARN|WARNING|ERROR|FATAL):\s*`)
		if match := levelPrefixRegex.FindStringSubmatch(line); match != nil {
			entry.Level = strings.ToLower(match[1])
			if entry.Level == "warning" {
				entry.Level = "warn"
			}
			line = line[len(match[0]):]
		}
	}

	// Try to extract source (format: "source: message" or "[source] message")
	sourceRegex := regexp.MustCompile(`^\[([^\]]+)\]\s*`)
	if match := sourceRegex.FindStringSubmatch(line); match != nil {
		entry.Source = match[1]
		line = line[len(match[0]):]
	} else {
		// Try "source: message" format
		if idx := strings.Index(line, ": "); idx > 0 && idx < 50 {
			potentialSource := line[:idx]
			// Check if it looks like a source (no spaces, reasonable length)
			if !strings.Contains(potentialSource, " ") {
				entry.Source = potentialSource
				line = line[idx+2:]
			}
		}
	}

	entry.Message = strings.TrimSpace(line)
	return entry
}

// logsStreamHandler handles SSE log streaming
func logsStreamHandler(d Deps, manager *LogStreamManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get query parameters
		minLevel := strings.ToLower(r.URL.Query().Get("level"))
		if minLevel == "" {
			minLevel = "debug"
		}
		searchQuery := strings.ToLower(r.URL.Query().Get("search"))
		tailLines := parsePositiveInt(r.URL.Query().Get("tail"), 100, 1, 1000)

		// Validate level
		validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
		if !validLevels[minLevel] {
			minLevel = "debug"
		}

		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

		// Subscribe to log stream
		ch, err := manager.Subscribe()
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		defer manager.Unsubscribe(ch)

		// Send initial history from log file if available
		if manager.logFilePath != "" {
			history := getLogHistory(manager.logFilePath, tailLines, minLevel, searchQuery)
			for _, entry := range history {
				if err := sendLogEntry(w, entry); err != nil {
					return
				}
			}
			w.(http.Flusher).Flush()
		}

		// Stream new log entries
		ctx := r.Context()
		ticker := time.NewTicker(30 * time.Second) // Keep-alive ticker
		defer ticker.Stop()

		for {
			select {
			case entry, ok := <-ch:
				if !ok {
					return
				}
				// Filter by level
				if !ShouldIncludeLevel(entry.Level, minLevel) {
					continue
				}
				// Filter by search query
				if searchQuery != "" && !strings.Contains(strings.ToLower(entry.Message), searchQuery) {
					continue
				}
				if err := sendLogEntry(w, entry); err != nil {
					return
				}
				w.(http.Flusher).Flush()

			case <-ticker.C:
				// Send keep-alive comment
				fmt.Fprintf(w, ":keepalive\n\n")
				w.(http.Flusher).Flush()

			case <-ctx.Done():
				return
			}
		}
	}
}

// sendLogEntry sends a log entry as an SSE event
func sendLogEntry(w http.ResponseWriter, entry LogEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}

// getLogHistory reads the last N lines from a log file
func getLogHistory(logFilePath string, n int, minLevel, searchQuery string) []LogEntry {
	var entries []LogEntry

	file, err := os.Open(logFilePath)
	if err != nil {
		return entries
	}
	defer file.Close()

	// Read all lines
	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	// Get last N lines
	start := len(lines) - n
	if start < 0 {
		start = 0
	}

	for _, line := range lines[start:] {
		entry := ParseLogLine(line)
		if !ShouldIncludeLevel(entry.Level, minLevel) {
			continue
		}
		if searchQuery != "" && !strings.Contains(strings.ToLower(entry.Message), searchQuery) {
			continue
		}
		entries = append(entries, entry)
	}

	return entries
}

// logsExportHandler handles log export requests
func logsExportHandler(d Deps, manager *LogStreamManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		minLevel := strings.ToLower(r.URL.Query().Get("level"))
		if minLevel == "" {
			minLevel = "debug"
		}
		searchQuery := strings.ToLower(r.URL.Query().Get("search"))
		maxLines := parsePositiveInt(r.URL.Query().Get("max"), 10000, 1, 100000)

		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Disposition", `attachment; filename="gateway-logs.txt"`)

		// Read from log file
		if manager.logFilePath != "" {
			file, err := os.Open(manager.logFilePath)
			if err == nil {
				defer file.Close()

				scanner := bufio.NewScanner(file)
				count := 0
				for scanner.Scan() && count < maxLines {
					line := scanner.Text()
					entry := ParseLogLine(line)
					if !ShouldIncludeLevel(entry.Level, minLevel) {
						continue
					}
					if searchQuery != "" && !strings.Contains(strings.ToLower(entry.Message), searchQuery) {
						continue
					}
					fmt.Fprintf(w, "[%s] %s: %s\n", entry.Timestamp.Format(time.RFC3339), strings.ToUpper(entry.Level), entry.Message)
					count++
				}
			}
		}
	}
}

// DefaultLogStreamManager is the default log stream manager instance
var DefaultLogStreamManager = NewLogStreamManager("")

// InitLogStreamManager initializes the log stream manager with the log file path
func InitLogStreamManager(logDir string) {
	logFile := filepath.Join(logDir, "gateway.log")
	if _, err := os.Stat(logFile); err == nil {
		DefaultLogStreamManager.logFilePath = logFile
	}
}

// BroadcastLog broadcasts a log entry to all connected clients
func BroadcastLog(level, message, source string) {
	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     strings.ToLower(level),
		Message:   message,
		Source:    source,
	}
	DefaultLogStreamManager.Broadcast(entry)
}
