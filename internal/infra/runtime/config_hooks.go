package runtime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"ai-model-gateway/internal/core"

	"gopkg.in/yaml.v3"
)

// BuildConfigExportHook creates an admin hook for sanitized config export.
func BuildConfigExportHook(state ConfigState) func() ([]byte, error) {
	return func() ([]byte, error) {
		cfg, err := loadRuntimeConfig(state)
		if err != nil {
			return nil, err
		}
		return yaml.Marshal(sanitizedConfigExportView(cfg))
	}
}

// BuildConfigSaveHook creates an admin hook that merges and persists config updates.
func BuildConfigSaveHook(state ConfigState) func(json.RawMessage) (interface{}, error) {
	return func(payload json.RawMessage) (interface{}, error) {
		cfg, err := loadRuntimeConfig(state)
		if err != nil {
			return nil, err
		}
		if err := mergeConfigJSON(cfg, payload); err != nil {
			return nil, fmt.Errorf("merge config payload: %w", err)
		}
		cfg.Normalize()
		if err := cfg.Validate(); err != nil {
			return nil, err
		}
		if err := state.Save(cfg); err != nil {
			return nil, err
		}
		return sanitizedConfigView(cfg), nil
	}
}

// BuildConfigHistoryHook creates an admin hook returning config version history.
func BuildConfigHistoryHook(state ConfigState) func() (interface{}, error) {
	return func() (interface{}, error) {
		versions, err := state.ListVersions()
		if err != nil {
			return nil, err
		}

		items := make([]map[string]interface{}, 0, len(versions))
		for _, version := range versions {
			items = append(items, map[string]interface{}{
				"id":         version.ID,
				"filename":   version.Filename,
				"created_at": version.CreatedAt.Format(http.TimeFormat),
				"size":       version.Size,
			})
		}
		return map[string]interface{}{"versions": items}, nil
	}
}

// BuildConfigHistoryDiffHook creates an admin hook returning line diff for one history version.
func BuildConfigHistoryDiffHook(state ConfigState) func(string) (interface{}, error) {
	return func(versionID string) (interface{}, error) {
		currentData, err := state.ReadCurrentFile()
		if err != nil {
			return nil, err
		}
		version, versionData, err := state.ReadVersionFile(versionID)
		if err != nil {
			return nil, err
		}

		lines := redactDiffLines(buildConfigDiffLines(currentData, versionData))
		return map[string]interface{}{
			"version": map[string]interface{}{
				"id":         version.ID,
				"filename":   version.Filename,
				"created_at": version.CreatedAt.Format(http.TimeFormat),
				"size":       version.Size,
			},
			"summary": summarizeConfigDiff(lines),
			"lines":   lines,
		}, nil
	}
}

// BuildConfigRollbackHook creates an admin hook that rolls back to a history version.
func BuildConfigRollbackHook(state ConfigState) func(string) (interface{}, error) {
	return func(versionID string) (interface{}, error) {
		var (
			cfg *core.Config
			err error
		)

		if strings.TrimSpace(versionID) == "" {
			cfg, err = state.Rollback()
		} else {
			cfg, err = state.RollbackVersion(versionID)
		}
		if err != nil {
			return nil, err
		}
		return sanitizedConfigView(cfg), nil
	}
}

func loadRuntimeConfig(state ConfigState) (*core.Config, error) {
	cfg := state.Current()
	if cfg != nil {
		return cfg, nil
	}

	data, err := state.ReadCurrentFile()
	if err != nil {
		return nil, err
	}
	var parsed core.Config
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	parsed.Normalize()
	return &parsed, nil
}

type diffLine struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type diffSummary struct {
	AddedLines    int `json:"added_lines"`
	RemovedLines  int `json:"removed_lines"`
	ChangedBlocks int `json:"changed_blocks"`
}

func buildConfigDiffLines(current []byte, previous []byte) []diffLine {
	left := normalizeDiffLines(previous)
	right := normalizeDiffLines(current)

	dp := make([][]int, len(left)+1)
	for i := range dp {
		dp[i] = make([]int, len(right)+1)
	}
	for i := 1; i <= len(left); i++ {
		for j := 1; j <= len(right); j++ {
			if left[i-1] == right[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
				continue
			}
			if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	var reversed []diffLine
	for i, j := len(left), len(right); i > 0 || j > 0; {
		switch {
		case i > 0 && j > 0 && left[i-1] == right[j-1]:
			reversed = append(reversed, diffLine{Kind: "context", Text: left[i-1]})
			i--
			j--
		case j > 0 && (i == 0 || dp[i][j-1] > dp[i-1][j]):
			reversed = append(reversed, diffLine{Kind: "add", Text: right[j-1]})
			j--
		default:
			reversed = append(reversed, diffLine{Kind: "remove", Text: left[i-1]})
			i--
		}
	}

	lines := make([]diffLine, 0, len(reversed))
	for i := len(reversed) - 1; i >= 0; i-- {
		lines = append(lines, reversed[i])
	}
	return lines
}

func summarizeConfigDiff(lines []diffLine) diffSummary {
	var summary diffSummary
	inChangedBlock := false
	hasAdd := false
	hasRemove := false

	flushBlock := func() {
		if hasAdd && hasRemove {
			summary.ChangedBlocks++
		}
		inChangedBlock = false
		hasAdd = false
		hasRemove = false
	}

	for _, line := range lines {
		switch line.Kind {
		case "add":
			summary.AddedLines++
			inChangedBlock = true
			hasAdd = true
		case "remove":
			summary.RemovedLines++
			inChangedBlock = true
			hasRemove = true
		default:
			if inChangedBlock {
				flushBlock()
			}
		}
	}
	if inChangedBlock {
		flushBlock()
	}
	return summary
}

func normalizeDiffLines(data []byte) []string {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func redactDiffLines(lines []diffLine) []diffLine {
	if len(lines) == 0 {
		return nil
	}
	redacted := make([]diffLine, 0, len(lines))
	for _, line := range lines {
		line.Text = redactDiffLine(line.Text)
		redacted = append(redacted, line)
	}
	return redacted
}

func redactDiffLine(line string) string {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)

	for _, prefix := range []string{"bootstrap_token:", "cookie_signing_key:", "api_key:"} {
		if strings.HasPrefix(lower, prefix) {
			key, _, _ := strings.Cut(line, ":")
			return key + ": [REDACTED]"
		}
	}

	for _, headerKey := range []string{"authorization", "proxy-authorization", "x-api-key"} {
		marker := headerKey + ":"
		idx := strings.Index(lower, marker)
		if idx >= 0 {
			return line[:idx+len(marker)] + " [REDACTED]"
		}
	}
	return line
}

func sanitizedConfigView(cfg *core.Config) map[string]interface{} {
	if cfg == nil {
		return map[string]interface{}{}
	}

	type safeAdmin struct {
		Enabled  bool   `json:"enabled"`
		Language string `json:"language"`
	}
	type safeProvider struct {
		Name             string            `json:"name"`
		BaseURL          string            `json:"base_url"`
		AnthropicBaseURL string            `json:"anthropic_base_url,omitempty"`
		ProviderClass    string            `json:"provider_class"`
		Models           []string          `json:"models"`
		Weight           int               `json:"weight"`
		TimeoutMs        int               `json:"timeout_ms"`
		SameRetries      int               `json:"same_retries"`
		Enabled          bool              `json:"enabled"`
		Headers          map[string]string `json:"headers,omitempty"`
	}

	providers := make([]safeProvider, len(cfg.Providers))
	for i, p := range cfg.Providers {
		providers[i] = safeProvider{
			Name:             p.Name,
			BaseURL:          p.BaseURL,
			AnthropicBaseURL: p.AnthropicBaseURL,
			ProviderClass:    string(p.ProviderClass),
			Models:           append([]string(nil), p.Models...),
			Weight:           p.Weight,
			TimeoutMs:        p.TimeoutMs,
			SameRetries:      p.SameRetries,
			Enabled:          p.IsEnabled(),
			Headers:          redactSensitiveHeaders(cloneStringMap(p.Headers)),
		}
	}

	return map[string]interface{}{
		"server":    cfg.Server,
		"admin":     safeAdmin{Enabled: cfg.Admin.Enabled, Language: cfg.Admin.Language},
		"routing":   cfg.Routing,
		"telemetry": cfg.Telemetry,
		"pricing":   cfg.Pricing,
		"compat":    cfg.Compat,
		"providers": providers,
	}
}

func sanitizedConfigExportView(cfg *core.Config) map[string]interface{} {
	if cfg == nil {
		return map[string]interface{}{}
	}

	type safeAdmin struct {
		Enabled  bool   `yaml:"enabled" json:"enabled"`
		Language string `yaml:"language" json:"language"`
	}
	type safeProvider struct {
		Name             string   `yaml:"name" json:"name"`
		BaseURL          string   `yaml:"base_url" json:"base_url"`
		AnthropicBaseURL string   `yaml:"anthropic_base_url,omitempty" json:"anthropic_base_url,omitempty"`
		ProviderClass    string   `yaml:"provider_class" json:"provider_class"`
		Models           []string `yaml:"models" json:"models"`
		Weight           int      `yaml:"weight" json:"weight"`
		TimeoutMs        int      `yaml:"timeout_ms" json:"timeout_ms"`
		SameRetries      int      `yaml:"same_retries" json:"same_retries"`
		Enabled          bool     `yaml:"enabled" json:"enabled"`
	}

	providers := make([]safeProvider, len(cfg.Providers))
	for i, p := range cfg.Providers {
		providers[i] = safeProvider{
			Name:             p.Name,
			BaseURL:          p.BaseURL,
			AnthropicBaseURL: p.AnthropicBaseURL,
			ProviderClass:    string(p.ProviderClass),
			Models:           append([]string(nil), p.Models...),
			Weight:           p.Weight,
			TimeoutMs:        p.TimeoutMs,
			SameRetries:      p.SameRetries,
			Enabled:          p.IsEnabled(),
		}
	}

	return map[string]interface{}{
		"server":    cfg.Server,
		"admin":     safeAdmin{Enabled: cfg.Admin.Enabled, Language: cfg.Admin.Language},
		"routing":   cfg.Routing,
		"providers": providers,
		"telemetry": cfg.Telemetry,
		"pricing":   cfg.Pricing,
		"compat":    cfg.Compat,
	}
}

func mergeConfigJSON(cfg *core.Config, payload json.RawMessage) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(payload, &root); err != nil {
		return err
	}

	for key, value := range root {
		switch key {
		case "server":
			if err := json.Unmarshal(value, &cfg.Server); err != nil {
				return fmt.Errorf("server: %w", err)
			}
		case "admin":
			if err := json.Unmarshal(value, &cfg.Admin); err != nil {
				return fmt.Errorf("admin: %w", err)
			}
		case "routing":
			if err := json.Unmarshal(value, &cfg.Routing); err != nil {
				return fmt.Errorf("routing: %w", err)
			}
		case "telemetry":
			if err := json.Unmarshal(value, &cfg.Telemetry); err != nil {
				return fmt.Errorf("telemetry: %w", err)
			}
		case "pricing":
			if err := json.Unmarshal(value, &cfg.Pricing); err != nil {
				return fmt.Errorf("pricing: %w", err)
			}
		case "compat":
			if err := json.Unmarshal(value, &cfg.Compat); err != nil {
				return fmt.Errorf("compat: %w", err)
			}
		case "providers":
			providers, err := mergeProvidersJSON(cfg.Providers, value)
			if err != nil {
				return fmt.Errorf("providers: %w", err)
			}
			cfg.Providers = providers
		}
	}
	return nil
}

func mergeProvidersJSON(current []core.Provider, payload json.RawMessage) ([]core.Provider, error) {
	var rawProviders []json.RawMessage
	if err := json.Unmarshal(payload, &rawProviders); err != nil {
		return nil, err
	}

	currentByName := make(map[string]core.Provider, len(current))
	for _, provider := range current {
		currentByName[strings.TrimSpace(provider.Name)] = provider
	}

	merged := make([]core.Provider, 0, len(rawProviders))
	for idx, rawProvider := range rawProviders {
		var seed core.Provider

		var meta struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(rawProvider, &meta); err != nil {
			return nil, err
		}

		if provider, ok := currentByName[strings.TrimSpace(meta.Name)]; ok {
			seed = provider
		} else if idx < len(current) {
			seed = current[idx]
		}
		previous := seed

		if err := json.Unmarshal(rawProvider, &seed); err != nil {
			return nil, err
		}
		if strings.TrimSpace(seed.APIKey) == "[REDACTED]" {
			seed.APIKey = previous.APIKey
		}
		for key, value := range seed.Headers {
			if strings.TrimSpace(value) == "[REDACTED]" {
				if preserved, ok := previous.Headers[key]; ok {
					seed.Headers[key] = preserved
				}
			}
		}
		merged = append(merged, seed)
	}
	return merged, nil
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return map[string]string{}
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func redactSensitiveHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return map[string]string{}
	}
	for key, value := range headers {
		if isSensitiveHeaderKey(key) && strings.TrimSpace(value) != "" {
			headers[key] = "[REDACTED]"
		}
	}
	return headers
}

func isSensitiveHeaderKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "authorization", "proxy-authorization", "x-api-key":
		return true
	default:
		return false
	}
}
