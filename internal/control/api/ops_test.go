package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDiffConfigsRedactsSecretsInsideArrays(t *testing.T) {
	before := map[string]any{
		"providers": []any{
			map[string]any{
				"name":     "openai",
				"base_url": "https://old.example.test",
				"api_key":  "old-secret",
			},
		},
	}
	after := map[string]any{
		"providers": []any{
			map[string]any{
				"name":     "openai",
				"base_url": "https://new.example.test",
				"api_key":  "new-secret",
			},
		},
	}

	changes := DiffConfigs(before, after)
	data, err := json.Marshal(changes)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, leaked := range []string{"old-secret", "new-secret"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("diff leaked secret %q: %s", leaked, body)
		}
	}
	if !strings.Contains(body, "[redacted]") {
		t.Fatalf("diff did not include redaction marker: %s", body)
	}
	if !strings.Contains(body, "https://new.example.test") {
		t.Fatalf("diff redacted non-secret field: %s", body)
	}
}

func TestDiffConfigsRedactsHeaderCredentials(t *testing.T) {
	before := map[string]any{
		"providers": []any{
			map[string]any{
				"name": "custom",
				"headers": map[string]any{
					"Authorization": "Bearer old-token",
					"X-API-Key":     "old-key",
					"X-Custom":      "old-value",
				},
			},
		},
	}
	after := map[string]any{
		"providers": []any{
			map[string]any{
				"name": "custom",
				"headers": map[string]any{
					"Authorization": "Bearer new-token",
					"X-API-Key":     "new-key",
					"X-Custom":      "new-value",
				},
			},
		},
	}

	changes := DiffConfigs(before, after)
	data, err := json.Marshal(changes)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)

	// Credential headers should be redacted.
	for _, leaked := range []string{"Bearer old-token", "Bearer new-token", "old-key", "new-key"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("diff leaked header credential %q: %s", leaked, body)
		}
	}
	// Non-credential header should NOT be redacted.
	if !strings.Contains(body, "new-value") {
		t.Fatalf("diff redacted non-credential header: %s", body)
	}
}

func TestIsSecretPathRecognizesCredentialHeaders(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"providers[0].headers.authorization", true},
		{"providers[0].headers.Authorization", true},
		{"providers[0].headers.cookie", true},
		{"providers[0].headers.x-api-key", true},
		{"providers[0].headers.X-API-Key", true},
		{"providers[0].headers.api-key", true},
		{"providers[0].headers.x-auth-token", true},
		{"providers[0].headers.x-token", true},
		{"providers[0].headers.x-custom", false},
		{"providers[0].headers.content-type", false},
		{"providers[0].api_key", true},
		{"admin.bootstrap_token", true},
	}
	for _, tt := range tests {
		got := isSecretPath(tt.path)
		if got != tt.expected {
			t.Errorf("isSecretPath(%q) = %v, want %v", tt.path, got, tt.expected)
		}
	}
}
