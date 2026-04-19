package publish

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const sqliteStateSchema = `
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA busy_timeout = 5000;

CREATE TABLE IF NOT EXISTS publisher_state (
	singleton_id       INTEGER PRIMARY KEY CHECK(singleton_id = 1),
	version            INTEGER NOT NULL,
	active_revision_id TEXT    NOT NULL DEFAULT '',
	updated_at         TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS publisher_revisions (
	seq           INTEGER NOT NULL,
	revision_id   TEXT    PRIMARY KEY,
	created_at    TEXT    NOT NULL,
	created_by    TEXT    NOT NULL DEFAULT '',
	description   TEXT    NOT NULL DEFAULT '',
	config_yaml   TEXT    NOT NULL DEFAULT '',
	snapshot_yaml TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_publisher_revisions_seq ON publisher_revisions(seq);

CREATE TABLE IF NOT EXISTS publisher_publishes (
	seq          INTEGER NOT NULL,
	publish_id   TEXT    PRIMARY KEY,
	revision_id  TEXT    NOT NULL DEFAULT '',
	snapshot_id  TEXT    NOT NULL DEFAULT '',
	requested_at TEXT    NOT NULL,
	requested_by TEXT    NOT NULL DEFAULT '',
	kind         TEXT    NOT NULL DEFAULT '',
	status       TEXT    NOT NULL DEFAULT '',
	error        TEXT    NOT NULL DEFAULT '',
	observed_at  TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_publisher_publishes_seq ON publisher_publishes(seq);
`

// SQLiteStateStore persists publisher state to a SQLite database.
type SQLiteStateStore struct {
	path string
}

// NewSQLiteStateStore creates a SQLite-backed state store.
func NewSQLiteStateStore(path string) *SQLiteStateStore {
	return &SQLiteStateStore{path: filepath.Clean(path)}
}

// Load loads publisher state from SQLite.
func (s *SQLiteStateStore) Load() (*PublisherState, error) {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return nil, nil
	}
	if _, err := os.Stat(s.path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat publisher sqlite state %s: %w", s.path, err)
	}

	db, err := openSQLiteStateDB(s.path)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	state := &PublisherState{}
	err = db.QueryRow(`
		SELECT version, active_revision_id
		FROM publisher_state
		WHERE singleton_id = 1
	`).Scan(&state.Version, &state.ActiveRevisionID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load publisher state header: %w", err)
	}
	if state.Version == 0 {
		state.Version = currentStateVersion
	}
	if state.Version != currentStateVersion {
		return nil, fmt.Errorf("unsupported publisher sqlite state version %d", state.Version)
	}

	revisionRows, err := db.Query(`
		SELECT revision_id, created_at, created_by, description, config_yaml, snapshot_yaml
		FROM publisher_revisions
		ORDER BY seq ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query publisher revisions: %w", err)
	}
	defer revisionRows.Close()

	for revisionRows.Next() {
		var stored StoredRevision
		if err := revisionRows.Scan(
			&stored.RevisionID,
			&stored.CreatedAt.Value,
			&stored.CreatedBy,
			&stored.Description,
			&stored.ConfigYAML,
			&stored.SnapshotYAML,
		); err != nil {
			return nil, fmt.Errorf("scan publisher revision: %w", err)
		}
		state.Revisions = append(state.Revisions, stored)
	}
	if err := revisionRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate publisher revisions: %w", err)
	}

	publishRows, err := db.Query(`
		SELECT publish_id, revision_id, snapshot_id, requested_at, requested_by, kind, status, error, observed_at
		FROM publisher_publishes
		ORDER BY seq ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query publisher publishes: %w", err)
	}
	defer publishRows.Close()

	for publishRows.Next() {
		var (
			record         PublishRecord
			requestedAtRaw string
			observedAtRaw  string
		)
		if err := publishRows.Scan(
			&record.PublishID,
			&record.RevisionID,
			&record.SnapshotID,
			&requestedAtRaw,
			&record.RequestedBy,
			&record.Kind,
			&record.Status,
			&record.Error,
			&observedAtRaw,
		); err != nil {
			return nil, fmt.Errorf("scan publisher publish record: %w", err)
		}
		record.RequestedAt, err = parseStoreTime(requestedAtRaw)
		if err != nil {
			return nil, fmt.Errorf("parse publish requested_at %q: %w", record.PublishID, err)
		}
		record.ObservedAt, err = parseOptionalStoreTime(observedAtRaw)
		if err != nil {
			return nil, fmt.Errorf("parse publish observed_at %q: %w", record.PublishID, err)
		}
		state.Publishes = append(state.Publishes, record)
	}
	if err := publishRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate publisher publish records: %w", err)
	}

	return state, nil
}

// Save atomically writes publisher state into SQLite.
func (s *SQLiteStateStore) Save(state *PublisherState) error {
	if s == nil || strings.TrimSpace(s.path) == "" || state == nil {
		return nil
	}

	db, err := openSQLiteStateDB(s.path)
	if err != nil {
		return err
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin publisher sqlite save: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		INSERT INTO publisher_state (singleton_id, version, active_revision_id, updated_at)
		VALUES (1, ?, ?, ?)
		ON CONFLICT(singleton_id) DO UPDATE SET
			version = excluded.version,
			active_revision_id = excluded.active_revision_id,
			updated_at = excluded.updated_at
	`, state.Version, state.ActiveRevisionID, formatStoreTime(time.Now().UTC())); err != nil {
		return fmt.Errorf("upsert publisher state header: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM publisher_revisions`); err != nil {
		return fmt.Errorf("clear publisher revisions: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM publisher_publishes`); err != nil {
		return fmt.Errorf("clear publisher publishes: %w", err)
	}

	revisionStmt, err := tx.Prepare(`
		INSERT INTO publisher_revisions (seq, revision_id, created_at, created_by, description, config_yaml, snapshot_yaml)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare revision insert: %w", err)
	}
	defer revisionStmt.Close()

	for idx, revision := range state.Revisions {
		if _, err := revisionStmt.Exec(
			idx,
			revision.RevisionID,
			revision.CreatedAt.Value,
			revision.CreatedBy,
			revision.Description,
			revision.ConfigYAML,
			revision.SnapshotYAML,
		); err != nil {
			return fmt.Errorf("insert publisher revision %q: %w", revision.RevisionID, err)
		}
	}

	publishStmt, err := tx.Prepare(`
		INSERT INTO publisher_publishes (seq, publish_id, revision_id, snapshot_id, requested_at, requested_by, kind, status, error, observed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare publish insert: %w", err)
	}
	defer publishStmt.Close()

	for idx, record := range state.Publishes {
		if _, err := publishStmt.Exec(
			idx,
			record.PublishID,
			record.RevisionID,
			record.SnapshotID,
			formatStoreTime(record.RequestedAt),
			record.RequestedBy,
			record.Kind,
			record.Status,
			record.Error,
			formatOptionalStoreTime(record.ObservedAt),
		); err != nil {
			return fmt.Errorf("insert publisher publish record %q: %w", record.PublishID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit publisher sqlite save: %w", err)
	}
	return nil
}

// MigratingStateStore uses a primary store and optionally migrates from a legacy fallback store.
type MigratingStateStore struct {
	primary  StateStore
	fallback StateStore
}

// NewMigratingStateStore creates a state store that prefers the primary store
// and falls back to a legacy store if the primary has no state yet.
func NewMigratingStateStore(primary StateStore, fallback StateStore) *MigratingStateStore {
	return &MigratingStateStore{
		primary:  primary,
		fallback: fallback,
	}
}

// Load loads from the primary store first, then from the fallback store. When
// fallback state exists it is copied into the primary store on a best-effort basis.
func (s *MigratingStateStore) Load() (*PublisherState, error) {
	if s == nil {
		return nil, nil
	}
	if s.primary != nil {
		state, err := s.primary.Load()
		if err != nil {
			return nil, err
		}
		if state != nil {
			return state, nil
		}
	}
	if s.fallback == nil {
		return nil, nil
	}

	state, err := s.fallback.Load()
	if err != nil || state == nil {
		return state, err
	}
	if s.primary != nil {
		_ = s.primary.Save(state)
	}
	return state, nil
}

// Save writes only to the primary store.
func (s *MigratingStateStore) Save(state *PublisherState) error {
	if s == nil || s.primary == nil {
		return nil
	}
	return s.primary.Save(state)
}

func openSQLiteStateDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create publisher sqlite directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open publisher sqlite state %s: %w", path, err)
	}
	if _, err := db.Exec(sqliteStateSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize publisher sqlite schema %s: %w", path, err)
	}
	return db, nil
}

func formatStoreTime(value time.Time) string {
	if value.IsZero() {
		return time.Time{}.UTC().Format(time.RFC3339Nano)
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func formatOptionalStoreTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseStoreTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
}

func parseOptionalStoreTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	return parseStoreTime(value)
}
