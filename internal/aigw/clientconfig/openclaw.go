package clientconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tailscale/hujson"
)

// MergeOpenClawConfig merges an OpenAI-compatible provider for AI Model Gateway into ~/.openclaw/openclaw.json.
// openAIBase must be like http://127.0.0.1:18080/v1 . apiKey is written literally (use "${AI_MODEL_GATEWAY_API_KEY}" for env substitution in OpenClaw).
// If setPrimary is true, agents.defaults.model.primary is set to OpenClawProviderID + "/" + publicModelID.
func MergeOpenClawConfig(path, openAIBase, apiKey string, publicModelID string, setPrimary bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var raw []byte
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	raw = data

	var root map[string]interface{}
	if len(raw) > 0 {
		v, err := hujson.Parse(raw)
		if err != nil {
			return fmt.Errorf("parse openclaw config %q: %w", path, err)
		}
		v.Standardize()
		std := v.Pack()
		if err := json.Unmarshal(std, &root); err != nil {
			return fmt.Errorf("decode openclaw config %q: %w", path, err)
		}
	}
	if root == nil {
		root = make(map[string]interface{})
	}

	models := ensureMap(root, "models")
	models["mode"] = "merge"
	providers := ensureMap(models, "providers")

	prov := map[string]interface{}{
		"baseUrl": openAIBase,
		"apiKey":  apiKey,
		"api":     "openai-completions",
		"models": []interface{}{
			map[string]interface{}{
				"id":              publicModelID,
				"name":            "AI Model Gateway (" + publicModelID + ")",
				"reasoning":       false,
				"input":           []interface{}{"text"},
				"contextWindow":   128000,
				"maxTokens":       16384,
			},
		},
	}
	providers[OpenClawProviderID] = prov

	if setPrimary && publicModelID != "" {
		agents := ensureMap(root, "agents")
		defaults := ensureMap(agents, "defaults")
		model := ensureMap(defaults, "model")
		model["primary"] = OpenClawProviderID + "/" + publicModelID
	}

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return os.WriteFile(path, out, 0o600)
}

func ensureMap(parent map[string]interface{}, key string) map[string]interface{} {
	if parent[key] == nil {
		m := make(map[string]interface{})
		parent[key] = m
		return m
	}
	if m, ok := parent[key].(map[string]interface{}); ok {
		return m
	}
	// Replace non-object with empty map
	m := make(map[string]interface{})
	parent[key] = m
	return m
}
