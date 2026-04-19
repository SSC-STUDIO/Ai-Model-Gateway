// Package eventlog provides the append-only event log for telemetryd.
package eventlog

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"ai-model-gateway/internal/contracts/telemetryingest"
	_ "modernc.org/sqlite"
)

// EventLog is an append-only event log backed by SQLite.
type EventLog struct {
	db   *sql.DB
	path string
	mu   sync.Mutex
	stmt *sql.Stmt
}

// New creates a new event log.
func New(path string) (*EventLog, error) {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create event log directory: %w", err)
	}

	// Open database
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open event log: %w", err)
	}

	// Set pragmas
	pragmas := `
	PRAGMA journal_mode = WAL;
	PRAGMA synchronous = NORMAL;
	PRAGMA busy_timeout = 5000;
	`
	if _, err := db.Exec(pragmas); err != nil {
		db.Close()
		return nil, fmt.Errorf("set pragmas: %w", err)
	}

	// Create schema
	schema := `
	CREATE TABLE IF NOT EXISTS events (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		event_id      TEXT    NOT NULL UNIQUE,
		event_type    TEXT    NOT NULL,
		schema_version INTEGER NOT NULL,
		source_service TEXT   NOT NULL,
		source_instance TEXT  NOT NULL,
		emitted_at    TEXT    NOT NULL,
		imported      INTEGER NOT NULL DEFAULT 0,
		payload       TEXT    NOT NULL,
		created_at    TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_events_emitted_at ON events(emitted_at DESC);
	CREATE INDEX IF NOT EXISTS idx_events_event_type ON events(event_type);
	CREATE INDEX IF NOT EXISTS idx_events_source ON events(source_service, source_instance);
	`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	// Prepare insert statement
	stmt, err := db.Prepare(`
		INSERT INTO events (event_id, event_type, schema_version, source_service, source_instance, emitted_at, imported, payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("prepare insert: %w", err)
	}

	return &EventLog{
		db:   db,
		path: path,
		stmt: stmt,
	}, nil
}

// Append appends events to the log.
func (l *EventLog) Append(events []telemetryingest.Event) (accepted, dropped int, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	tx, err := l.db.Begin()
	if err != nil {
		return 0, len(events), err
	}
	defer tx.Rollback()

	stmt := tx.Stmt(l.stmt)

	for _, event := range events {
		payload, err := serializePayload(event.Payload)
		if err != nil {
			dropped++
			continue
		}

		imported := 0
		if event.Imported {
			imported = 1
		}

		_, err = stmt.Exec(
			event.EventID,
			event.EventType,
			event.SchemaVersion,
			event.SourceService,
			event.SourceInstance,
			event.EmittedAt.UTC().Format(time.RFC3339Nano),
			imported,
			payload,
		)
		if err != nil {
			log.Printf("[eventlog] insert error: %v", err)
			dropped++
			continue
		}
		accepted++
	}

	if err := tx.Commit(); err != nil {
		return 0, len(events), err
	}

	return accepted, dropped, nil
}

// Close closes the event log.
func (l *EventLog) Close() error {
	if l.stmt != nil {
		l.stmt.Close()
	}
	return l.db.Close()
}

// GetDB returns the underlying database for queries.
func (l *EventLog) GetDB() *sql.DB {
	return l.db
}

// serializePayload serializes an event payload to JSON.
func serializePayload(payload telemetryingest.EventPayload) (string, error) {
	data, err := json.Marshal(map[string]interface{}{
		"request_id":           payload.RequestID,
		"timestamp":            payload.Timestamp.UTC().Format(time.RFC3339Nano),
		"path":                 payload.Path,
		"requested_model":      payload.RequestedModel,
		"effective_model":      payload.EffectiveModel,
		"provider_id":          payload.ProviderID,
		"route_mode":           payload.RouteMode,
		"status_code":          payload.StatusCode,
		"latency_ms":           payload.Latency.Milliseconds(),
		"attempts":             payload.Attempts,
		"prompt_tokens":        payload.PromptTokens,
		"cached_prompt_tokens": payload.CachedPromptTokens,
		"completion_tokens":    payload.CompletionTokens,
		"stream":               payload.Stream,
		"error":                payload.Error,
	})
	if err != nil {
		return "", err
	}
	return string(data), nil
}
