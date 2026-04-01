package state

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

	"ai-model-gateway/internal/config"
)

const configHistoryLimit = 20

type ConfigStore struct {
	v    atomic.Value
	path string
	mu   sync.Mutex
}

type ConfigVersion struct {
	ID        string    `json:"id"`
	Filename  string    `json:"filename"`
	Path      string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
	Size      int64     `json:"size"`
}

func NewConfigStore(initial config.Config) *ConfigStore {
	return NewConfigStoreWithPath(initial, "")
}

func NewConfigStoreWithPath(initial config.Config, path string) *ConfigStore {
	s := &ConfigStore{path: path}
	s.v.Store(initial)
	return s
}

func (s *ConfigStore) Get() config.Config {
	return s.v.Load().(config.Config)
}

func (s *ConfigStore) Set(v config.Config) {
	s.v.Store(v)
}

func (s *ConfigStore) Path() string {
	return s.path
}

func (s *ConfigStore) Save(v config.Config) error {
	if s.path == "" {
		return errors.New("config path is not set")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeBackupLocked(); err != nil {
		return err
	}
	return config.SaveToFile(s.path, v)
}

func (s *ConfigStore) Rollback() (config.Config, error) {
	versions, err := s.ListVersions()
	if err != nil {
		return config.Config{}, err
	}
	if len(versions) == 0 {
		return config.Config{}, errors.New("no config history available")
	}
	return s.RollbackVersion(versions[0].ID)
}

func (s *ConfigStore) RollbackVersion(versionID string) (config.Config, error) {
	if s.path == "" {
		return config.Config{}, errors.New("config path is not set")
	}
	// 验证 versionID 不包含路径遍历字符
	if strings.Contains(versionID, "..") || strings.ContainsAny(versionID, `\/:*?"<>|` ) {
		return config.Config{}, errors.New("invalid version ID")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	backupPath := s.backupPath()
	version, err := s.findVersionLocked(versionID)
	if err != nil {
		return config.Config{}, err
	}

	restore, err := config.LoadFromFile(version.Path)
	if err != nil {
		return config.Config{}, fmt.Errorf("load backup config: %w", err)
	}

	currentData, err := os.ReadFile(s.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return config.Config{}, fmt.Errorf("read current config: %w", err)
	}
	if len(currentData) > 0 {
		if err := s.writeVersionedHistoryLocked(currentData); err != nil {
			return config.Config{}, err
		}
		if err := os.WriteFile(backupPath, currentData, 0o600); err != nil {
			return config.Config{}, fmt.Errorf("write backup config: %w", err)
		}
	}

	if err := config.SaveToFile(s.path, restore); err != nil {
		return config.Config{}, err
	}
	return restore, nil
}

func (s *ConfigStore) ListVersions() ([]ConfigVersion, error) {
	if s.path == "" {
		return nil, errors.New("config path is not set")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listVersionsLocked()
}

func (s *ConfigStore) ReadCurrentFile() ([]byte, error) {
	if s.path == "" {
		return nil, errors.New("config path is not set")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("read current config: %w", err)
	}
	return data, nil
}

func (s *ConfigStore) ReadVersionFile(versionID string) (ConfigVersion, []byte, error) {
	if s.path == "" {
		return ConfigVersion{}, nil, errors.New("config path is not set")
	}
	// 验证 versionID 不包含路径遍历字符
	if strings.Contains(versionID, "..") || strings.ContainsAny(versionID, `\/:*?"<>|` ) {
		return ConfigVersion{}, nil, errors.New("invalid version ID")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	version, err := s.findVersionLocked(versionID)
	if err != nil {
		return ConfigVersion{}, nil, err
	}
	data, err := os.ReadFile(version.Path)
	if err != nil {
		return ConfigVersion{}, nil, fmt.Errorf("read config version: %w", err)
	}
	return version, data, nil
}

func (s *ConfigStore) backupPath() string {
	return s.path + ".bak"
}

func (s *ConfigStore) historyDir() string {
	base := filepath.Base(s.path)
	return filepath.Join(filepath.Dir(s.path), "."+base+".history")
}

func (s *ConfigStore) writeBackupLocked() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read current config: %w", err)
	}
	if err := os.WriteFile(s.backupPath(), data, 0o600); err != nil {
		return fmt.Errorf("write backup config: %w", err)
	}
	if err := s.writeVersionedHistoryLocked(data); err != nil {
		return err
	}
	return nil
}

func (s *ConfigStore) writeVersionedHistoryLocked(data []byte) error {
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

func (s *ConfigStore) pruneHistoryLocked() error {
	versions, err := s.listVersionsLocked()
	if err != nil {
		return err
	}
	if len(versions) <= configHistoryLimit {
		return nil
	}
	for _, version := range versions[configHistoryLimit:] {
		if err := os.Remove(version.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove old config history: %w", err)
		}
	}
	return nil
}

func (s *ConfigStore) listVersionsLocked() ([]ConfigVersion, error) {
	dir := s.historyDir()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config history dir: %w", err)
	}

	versions := make([]ConfigVersion, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".yaml") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("read config history info: %w", err)
		}
		versions = append(versions, ConfigVersion{
			ID:        entry.Name(),
			Filename:  entry.Name(),
			Path:      filepath.Join(dir, entry.Name()),
			CreatedAt: info.ModTime().UTC(),
			Size:      info.Size(),
		})
	}

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].CreatedAt.After(versions[j].CreatedAt)
	})
	return versions, nil
}

func (s *ConfigStore) findVersionLocked(versionID string) (ConfigVersion, error) {
	backupPath := s.backupPath()
	if strings.TrimSpace(versionID) == "" {
		if _, err := os.Stat(backupPath); err == nil {
			info, statErr := os.Stat(backupPath)
			if statErr != nil {
				return ConfigVersion{}, fmt.Errorf("stat backup config: %w", statErr)
			}
			return ConfigVersion{
				ID:        filepath.Base(backupPath),
				Filename:  filepath.Base(backupPath),
				Path:      backupPath,
				CreatedAt: info.ModTime().UTC(),
				Size:      info.Size(),
			}, nil
		}
		versions, err := s.listVersionsLocked()
		if err != nil {
			return ConfigVersion{}, err
		}
		if len(versions) == 0 {
			return ConfigVersion{}, errors.New("no config history available")
		}
		return versions[0], nil
	}

	versions, err := s.listVersionsLocked()
	if err != nil {
		return ConfigVersion{}, err
	}
	for _, version := range versions {
		if version.ID == versionID {
			return version, nil
		}
	}
	return ConfigVersion{}, fmt.Errorf("config history version not found: %s", versionID)
}
