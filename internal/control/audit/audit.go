// Package audit stores append-only control-plane audit events.
package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Event is one redacted audit log entry.
type Event struct {
	ID        string         `json:"id"`
	Time      time.Time      `json:"time"`
	Actor     string         `json:"actor,omitempty"`
	Role      string         `json:"role,omitempty"`
	Source    string         `json:"source,omitempty"`
	Action    string         `json:"action"`
	Resource  string         `json:"resource,omitempty"`
	Success   bool           `json:"success"`
	Error     string         `json:"error,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

// Query filters audit events.
type Query struct {
	Limit  int
	Action string
	Since  time.Time
}

// Store is an append-only JSONL audit store.
type Store struct {
	path string
	mu   sync.Mutex
}

// NewStore opens an audit store at path.
func NewStore(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("audit path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create audit dir: %w", err)
	}
	return &Store{path: path}, nil
}

// Path returns the audit file path.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Record appends one event after redacting sensitive details.
func (s *Store) Record(ctx context.Context, event Event) error {
	if s == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if event.ID == "" {
		event.ID = "aud_" + uuid.NewString()
	}
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	} else {
		event.Time = event.Time.UTC()
	}
	event.Action = strings.TrimSpace(event.Action)
	event.Resource = strings.TrimSpace(event.Resource)
	event.Actor = strings.TrimSpace(event.Actor)
	event.Role = strings.TrimSpace(event.Role)
	event.Error = strings.TrimSpace(event.Error)
	event.Details = RedactMap(event.Details)

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal audit event: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0640)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write audit log: %w", err)
	}
	return nil
}

// List returns recent events in reverse chronological order.
func (s *Store) List(ctx context.Context, query Query) ([]Event, error) {
	if s == nil {
		return nil, nil
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Event{}, nil
		}
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	defer f.Close()

	action := strings.TrimSpace(query.Action)
	events := make([]Event, 0)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if action != "" && event.Action != action {
			continue
		}
		if !query.Since.IsZero() && event.Time.Before(query.Since) {
			continue
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan audit log: %w", err)
	}
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Time.After(events[j].Time)
	})
	if len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

// RedactMap returns a deep copy with secret-looking values redacted.
func RedactMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = redactValue(key, value)
	}
	return out
}

func redactValue(key string, value any) any {
	lowerKey := strings.ToLower(key)
	if isSensitiveKey(lowerKey) {
		if value == nil || strings.TrimSpace(fmt.Sprint(value)) == "" {
			return ""
		}
		return "[redacted]"
	}
	switch typed := value.(type) {
	case map[string]any:
		return RedactMap(typed)
	case map[string]string:
		copied := make(map[string]any, len(typed))
		for k, v := range typed {
			copied[k] = v
		}
		return RedactMap(copied)
	case []any:
		copied := make([]any, 0, len(typed))
		for _, item := range typed {
			copied = append(copied, redactValue(key, item))
		}
		return copied
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	for _, fragment := range []string{"secret", "token", "api_key", "apikey", "password", "authorization", "cookie", "signing_key"} {
		if strings.Contains(key, fragment) {
			return true
		}
	}
	return false
}
