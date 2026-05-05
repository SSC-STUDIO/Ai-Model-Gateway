package clientconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const claudeSettingsSchema = "https://json.schemastore.org/claude-code-settings.json"

// MergeClaudeSettings merges env.ANTHROPIC_BASE_URL and optionally ANTHROPIC_AUTH_TOKEN into ~/.claude/settings.json.
func MergeClaudeSettings(path, anthropicBase, authToken string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var root map[string]interface{}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(data) > 0 {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if err := dec.Decode(&root); err != nil {
			return fmt.Errorf("parse claude settings %q: %w", path, err)
		}
	}
	if root == nil {
		root = map[string]interface{}{
			"$schema": claudeSettingsSchema,
		}
	}
	env, ok := root["env"].(map[string]interface{})
	if !ok || env == nil {
		env = make(map[string]interface{})
		root["env"] = env
	}
	env["ANTHROPIC_BASE_URL"] = anthropicBase
	if authToken != "" {
		env["ANTHROPIC_AUTH_TOKEN"] = authToken
	}
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return os.WriteFile(path, out, 0o600)
}
