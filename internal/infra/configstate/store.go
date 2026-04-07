package configstate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ai-model-gateway/internal/pathsecurity"
	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/infra/configloader"

	"gopkg.in/yaml.v3"
)

const historyLimit = 20

// Version is a single config history entry.
type Version struct {
	ID        string    `json:"id"`
	Filename  string    `json:"filename"`
	CreatedAt time.Time `json:"created_at"`
	Size      int64     `json:"size"`
}

type storedVersion struct {
	Version
	path string
}

// Store keeps config state and history metadata backed by files.
type Store struct {
	path    string
	current atomic.Value // stores *core.Config
	mu      sync.Mutex
}

// New creates a config state store for the config file.
func New(path string, initial *core.Config) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("config path is not set")
	}

	s := &Store{path: path}
	if initial != nil {
		s.current.Store(cloneConfig(initial))
		return s, nil
	}

	cfg, err := configloader.LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("load current config: %w", err)
	}
	s.current.Store(cloneConfig(cfg))
	return s, nil
}

// Path returns the managed config path.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Current returns the in-memory config snapshot.
func (s *Store) Current() *core.Config {
	if s == nil {
		return nil
	}
	raw := s.current.Load()
	if raw == nil {
		return nil
	}
	return cloneConfig(raw.(*core.Config))
}

// SetCurrent updates the in-memory config snapshot.
func (s *Store) SetCurrent(cfg *core.Config) {
	if s == nil || cfg == nil {
		return
	}
	s.current.Store(cloneConfig(cfg))
}

// Save writes a new config file while preserving backup/history files.
func (s *Store) Save(cfg *core.Config) error {
	if s == nil {
		return errors.New("config store is nil")
	}
	if cfg == nil {
		return errors.New("config is nil")
	}

	next := cloneConfig(cfg)
	next.Normalize()

	data, err := yaml.Marshal(next)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.writeBackupAndHistoryLocked(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	s.current.Store(next)
	return nil
}

// Rollback restores the latest history version.
func (s *Store) Rollback() (*core.Config, error) {
	return s.RollbackVersion("")
}

// RollbackVersion restores a specific history version into the main config file.
func (s *Store) RollbackVersion(versionID string) (*core.Config, error) {
	if s == nil {
		return nil, errors.New("config store is nil")
	}
	if strings.TrimSpace(versionID) != "" {
		if err := pathsecurity.ValidatePathComponent(versionID); err != nil {
			return nil, fmt.Errorf("invalid version ID: %w", err)
		}
	}

	s.mu.Lock()
	version, versionData, err := s.readVersionLocked(versionID)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}

	currentData, err := os.ReadFile(s.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		s.mu.Unlock()
		return nil, fmt.Errorf("read current config: %w", err)
	}
	if len(currentData) > 0 {
		if err := s.writeHistoryLocked(currentData); err != nil {
			s.mu.Unlock()
			return nil, err
		}
		if err := os.WriteFile(s.backupPath(), currentData, 0o600); err != nil {
			s.mu.Unlock()
			return nil, fmt.Errorf("write backup config: %w", err)
		}
	}
	if err := os.WriteFile(s.path, versionData, 0o600); err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("restore config version %s: %w", version.ID, err)
	}
	s.mu.Unlock()

	loaded, err := configloader.LoadFromFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("parse restored config: %w", err)
	}
	s.current.Store(cloneConfig(loaded))
	return loaded, nil
}

// ListVersions returns config history entries ordered newest first.
func (s *Store) ListVersions() ([]Version, error) {
	if s == nil {
		return nil, errors.New("config store is nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	versions, err := s.listVersionsLocked()
	if err != nil {
		return nil, err
	}
	result := make([]Version, 0, len(versions))
	for _, version := range versions {
		result = append(result, version.Version)
	}
	return result, nil
}

// ReadCurrentFile returns current config file bytes.
func (s *Store) ReadCurrentFile() ([]byte, error) {
	if s == nil {
		return nil, errors.New("config store is nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("read current config: %w", err)
	}
	return data, nil
}

// ReadVersionFile returns the selected history entry and raw file bytes.
func (s *Store) ReadVersionFile(versionID string) (Version, []byte, error) {
	if s == nil {
		return Version{}, nil, errors.New("config store is nil")
	}
	if strings.TrimSpace(versionID) != "" {
		if err := pathsecurity.ValidatePathComponent(versionID); err != nil {
			return Version{}, nil, fmt.Errorf("invalid version ID: %w", err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	version, data, err := s.readVersionLocked(versionID)
	if err != nil {
		return Version{}, nil, err
	}
	return version.Version, data, nil
}

func (s *Store) writeBackupAndHistoryLocked() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read current config: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	if err := s.writeHistoryLocked(data); err != nil {
		return err
	}
	if err := os.WriteFile(s.backupPath(), data, 0o600); err != nil {
		return fmt.Errorf("write backup config: %w", err)
	}
	return nil
}

func (s *Store) writeHistoryLocked(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	dir := s.historyDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config history dir: %w", err)
	}

	filename := "config-" + time.Now().UTC().Format("20060102-150405.000000000") + ".yaml"
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config history: %w", err)
	}
	return s.pruneHistoryLocked()
}

func (s *Store) pruneHistoryLocked() error {
	versions, err := s.listVersionsLocked()
	if err != nil {
		return err
	}
	if len(versions) <= historyLimit {
		return nil
	}
	for _, version := range versions[historyLimit:] {
		if err := os.Remove(version.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove old config history: %w", err)
		}
	}
	return nil
}

func (s *Store) readVersionLocked(versionID string) (storedVersion, []byte, error) {
	version, err := s.findVersionLocked(versionID)
	if err != nil {
		return storedVersion{}, nil, err
	}

	realHistory, err := filepath.Abs(s.historyDir())
	if err != nil {
		return storedVersion{}, nil, fmt.Errorf("invalid history directory: %w", err)
	}
	realHistory = filepath.Clean(realHistory)

	realVersionPath, err := filepath.Abs(version.path)
	if err != nil {
		return storedVersion{}, nil, fmt.Errorf("invalid version path: %w", err)
	}
	realVersionPath = filepath.Clean(realVersionPath)

	if !strings.HasPrefix(realVersionPath, realHistory+string(filepath.Separator)) {
		return storedVersion{}, nil, errors.New("version path is outside history directory")
	}

	data, err := os.ReadFile(version.path)
	if err != nil {
		return storedVersion{}, nil, fmt.Errorf("read config version: %w", err)
	}
	return version, data, nil
}

func (s *Store) findVersionLocked(versionID string) (storedVersion, error) {
	versions, err := s.listVersionsLocked()
	if err != nil {
		return storedVersion{}, err
	}
	if len(versions) == 0 {
		return storedVersion{}, errors.New("no config history available")
	}
	if strings.TrimSpace(versionID) == "" {
		return versions[0], nil
	}
	for _, version := range versions {
		if version.ID == versionID {
			return version, nil
		}
	}
	return storedVersion{}, fmt.Errorf("config history version not found: %s", versionID)
}

func (s *Store) listVersionsLocked() ([]storedVersion, error) {
	dir := s.historyDir()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config history dir: %w", err)
	}

	versions := make([]storedVersion, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".yaml") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("read config history info: %w", err)
		}
		versions = append(versions, storedVersion{
			Version: Version{
				ID:        entry.Name(),
				Filename:  entry.Name(),
				CreatedAt: info.ModTime().UTC(),
				Size:      info.Size(),
			},
			path: filepath.Join(dir, entry.Name()),
		})
	}

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].CreatedAt.After(versions[j].CreatedAt)
	})
	return versions, nil
}

func (s *Store) backupPath() string {
	baseDir := filepath.Dir(s.path)
	baseName := filepath.Base(s.path)
	return filepath.Join(baseDir, baseName+".bak")
}

func (s *Store) historyDir() string {
	baseDir := filepath.Dir(s.path)
	realBase, err := filepath.Abs(baseDir)
	if err != nil {
		realBase = baseDir
	}
	realBase = filepath.Clean(realBase)
	baseName := filepath.Base(s.path)
	return filepath.Join(realBase, "."+baseName+".history")
}

func cloneConfig(cfg *core.Config) *core.Config {
	if cfg == nil {
		return nil
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		fallback := *cfg
		return &fallback
	}

	var cloned core.Config
	if err := yaml.Unmarshal(data, &cloned); err != nil {
		fallback := *cfg
		return &fallback
	}
	return &cloned
}
