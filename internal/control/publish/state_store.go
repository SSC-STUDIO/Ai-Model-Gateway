package publish

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/gateway/snapshot"

	"gopkg.in/yaml.v3"
)

const currentStateVersion = 1

// StateStore persists and restores publisher state.
type StateStore interface {
	Load() (*PublisherState, error)
	Save(state *PublisherState) error
}

// PublisherState is the durable representation of the publisher ledger.
type PublisherState struct {
	Version          int              `json:"version"`
	ActiveRevisionID string           `json:"active_revision_id,omitempty"`
	Revisions        []StoredRevision `json:"revisions"`
	Publishes        []PublishRecord  `json:"publishes,omitempty"`
}

// StoredRevision is a durable revision entry. Config and snapshot are stored as
// YAML payloads so secrets omitted from JSON tags remain durable.
type StoredRevision struct {
	RevisionID   string   `json:"revision_id"`
	CreatedAt    timeJSON `json:"created_at"`
	CreatedBy    string   `json:"created_by,omitempty"`
	Description  string   `json:"description,omitempty"`
	ConfigYAML   string   `json:"config_yaml,omitempty"`
	SnapshotYAML string   `json:"snapshot_yaml,omitempty"`
}

// FileStateStore persists publisher state as a JSON file.
type FileStateStore struct {
	path string
}

// NewFileStateStore creates a file-backed state store.
func NewFileStateStore(path string) *FileStateStore {
	return &FileStateStore{path: filepath.Clean(path)}
}

// Load loads publisher state from disk.
func (s *FileStateStore) Load() (*PublisherState, error) {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return nil, nil
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read publisher state %s: %w", s.path, err)
	}

	var state PublisherState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode publisher state %s: %w", s.path, err)
	}
	if state.Version == 0 {
		state.Version = currentStateVersion
	}
	if state.Version != currentStateVersion {
		return nil, fmt.Errorf("unsupported publisher state version %d", state.Version)
	}
	return &state, nil
}

// Save writes publisher state to disk atomically.
func (s *FileStateStore) Save(state *PublisherState) error {
	if s == nil || strings.TrimSpace(s.path) == "" || state == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return fmt.Errorf("create publisher state directory: %w", err)
	}

	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode publisher state: %w", err)
	}

	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, encoded, 0600); err != nil {
		return fmt.Errorf("write publisher state temp file: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace publisher state file: %w", err)
	}
	return nil
}

type timeJSON struct {
	Value string `json:"value"`
}

func marshalStoredRevision(rev Revision) (StoredRevision, error) {
	stored := StoredRevision{
		RevisionID:  rev.RevisionID,
		CreatedAt:   timeJSON{Value: rev.CreatedAt.UTC().Format(time.RFC3339Nano)},
		CreatedBy:   rev.CreatedBy,
		Description: rev.Description,
	}

	if rev.Config != nil {
		data, err := yaml.Marshal(rev.Config)
		if err != nil {
			return StoredRevision{}, fmt.Errorf("marshal config for revision %s: %w", rev.RevisionID, err)
		}
		stored.ConfigYAML = string(data)
	}
	if rev.Snapshot != nil {
		data, err := yaml.Marshal(rev.Snapshot)
		if err != nil {
			return StoredRevision{}, fmt.Errorf("marshal snapshot for revision %s: %w", rev.RevisionID, err)
		}
		stored.SnapshotYAML = string(data)
	}
	return stored, nil
}

func unmarshalStoredRevision(stored StoredRevision) (Revision, error) {
	rev := Revision{
		RevisionID:  strings.TrimSpace(stored.RevisionID),
		CreatedBy:   stored.CreatedBy,
		Description: stored.Description,
	}
	if stored.CreatedAt.Value != "" {
		createdAt, err := time.Parse(time.RFC3339Nano, stored.CreatedAt.Value)
		if err != nil {
			return Revision{}, fmt.Errorf("parse revision %s created_at: %w", stored.RevisionID, err)
		}
		rev.CreatedAt = createdAt
	}
	if strings.TrimSpace(stored.ConfigYAML) != "" {
		var cfg core.Config
		if err := yaml.Unmarshal([]byte(stored.ConfigYAML), &cfg); err != nil {
			return Revision{}, fmt.Errorf("decode config for revision %s: %w", stored.RevisionID, err)
		}
		cfg.Normalize()
		rev.Config = &cfg
	}
	if strings.TrimSpace(stored.SnapshotYAML) != "" {
		var snap snapshot.Snapshot
		if err := yaml.Unmarshal([]byte(stored.SnapshotYAML), &snap); err != nil {
			return Revision{}, fmt.Errorf("decode snapshot for revision %s: %w", stored.RevisionID, err)
		}
		rev.Snapshot = &snap
	}
	return rev, nil
}
