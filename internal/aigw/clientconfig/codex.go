package clientconfig

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// MergeCodexConfig sets openai_base_url in ~/.codex/config.toml (creates file and parent dirs if missing).
func MergeCodexConfig(path, openAIBase string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var root map[string]interface{}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(data) > 0 {
		if err := toml.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("parse codex config %q: %w", path, err)
		}
	}
	if root == nil {
		root = make(map[string]interface{})
	}
	root["openai_base_url"] = openAIBase
	out, err := toml.Marshal(root)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}
