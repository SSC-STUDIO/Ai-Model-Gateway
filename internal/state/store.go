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
	"ai-model-gateway/internal/pathsecurity"
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
	// 增强验证 versionID - 使用 pathsecurity 包
	if err := pathsecurity.ValidatePathComponent(versionID); err != nil {
		return config.Config{}, fmt.Errorf("invalid version ID: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// SECURITY FIX: 验证版本文件路径是否在允许目录内
	historyDir := s.historyDir()
	realHistoryDir, err := filepath.Abs(historyDir)
	if err != nil {
		return config.Config{}, fmt.Errorf("invalid history directory: %w", err)
	}
	realHistoryDir = filepath.Clean(realHistoryDir)

	backupPath := s.backupPath()
	version, err := s.findVersionLocked(versionID)
	if err != nil {
		return config.Config{}, err
	}

	// 验证版本文件路径是否在历史目录内
	realVersionPath, err := filepath.Abs(version.Path)
	if err != nil {
		return config.Config{}, fmt.Errorf("invalid version path: %w", err)
	}
	realVersionPath = filepath.Clean(realVersionPath)
	
	if !strings.HasPrefix(realVersionPath, realHistoryDir+string(filepath.Separator)) {
		return config.Config{}, errors.New("version path is outside history directory")
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
	// 增强验证 versionID
	if err := pathsecurity.ValidatePathComponent(versionID); err != nil {
		return ConfigVersion{}, nil, fmt.Errorf("invalid version ID: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// SECURITY FIX: 验证版本文件路径是否在允许目录内
	historyDir := s.historyDir()
	realHistoryDir, err := filepath.Abs(historyDir)
	if err != nil {
		return ConfigVersion{}, nil, fmt.Errorf("invalid history directory: %w", err)
	}
	realHistoryDir = filepath.Clean(realHistoryDir)

	version, err := s.findVersionLocked(versionID)
	if err != nil {
		return ConfigVersion{}, nil, err
	}

	// 验证版本文件路径是否在历史目录内
	realVersionPath, err := filepath.Abs(version.Path)
	if err != nil {
		return ConfigVersion{}, nil, fmt.Errorf("invalid version path: %w", err)
	}
	realVersionPath = filepath.Clean(realVersionPath)

	if !strings.HasPrefix(realVersionPath, realHistoryDir+string(filepath.Separator)) {
		return ConfigVersion{}, nil, errors.New("version path is outside history directory")
	}

	data, err := os.ReadFile(version.Path)
	if err != nil {
		return ConfigVersion{}, nil, fmt.Errorf("read config version: %w", err)
	}
	return version, data, nil
}

func (s *ConfigStore) backupPath() string {
	// SECURITY FIX: 确保备份路径在配置目录内
	if s.path == "" {
		return ""
	}
	baseDir := filepath.Dir(s.path)
	baseName := filepath.Base(s.path)
	return filepath.Join(baseDir, baseName+".bak")
}

func (s *ConfigStore) historyDir() string {
	if s.path == "" {
		return ""
	}
	// SECURITY FIX: 确保历史目录在配置目录内
	baseDir := filepath.Dir(s.path)
	realBaseDir, _ := filepath.Abs(baseDir)
	realBaseDir = filepath.Clean(realBaseDir)
	
	base := filepath.Base(s.path)
	historyDir := filepath.Join(realBaseDir, "."+base+".history")
	return historyDir
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
