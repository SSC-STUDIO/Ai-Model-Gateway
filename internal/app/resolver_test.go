package app

import (
	"context"
	"testing"

	"ai-model-gateway/internal/core"
)

func TestResolver_Resolve_NoBridge(t *testing.T) {
	r := NewModelResolver(core.CompatConfig{})
	got, err := r.Resolve(context.Background(), "gpt-4o", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "gpt-4o" {
		t.Errorf("expected gpt-4o, got %s", got)
	}
}

func TestResolver_Resolve_EmptyModel(t *testing.T) {
	r := NewModelResolver(core.CompatConfig{})
	got, err := r.Resolve(context.Background(), "", "")
	if err != nil {
		t.Fatalf("expected nil error for empty model (passthrough), got %v", err)
	}
	if got != "" {
		t.Errorf("expected empty resolved model, got %q", got)
	}
}

func TestResolver_Resolve_BridgeRewrite(t *testing.T) {
	r := NewModelResolver(core.CompatConfig{
		Bridge: core.BridgeConfig{
			Enabled: true,
			Rules: []core.BridgeRule{
				{From: "gpt-4", To: "gpt-4o"},
				{From: "gpt-5.2*", To: "gpt-5.4"},
			},
		},
	})

	tests := []struct {
		in, want string
	}{
		{"gpt-4", "gpt-4o"},
		{"gpt-5.2-preview", "gpt-5.4"},
		{"claude-3", "claude-3"}, // no match
	}
	for _, tt := range tests {
		got, err := r.Resolve(context.Background(), tt.in, "")
		if err != nil {
			t.Fatalf("Resolve(%q) error: %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("Resolve(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestResolver_Resolve_ExcludeUserAgent(t *testing.T) {
	r := NewModelResolver(core.CompatConfig{
		Bridge: core.BridgeConfig{
			Enabled:           true,
			Rules:             []core.BridgeRule{{From: "gpt-4", To: "gpt-4o"}},
			ExcludeUserAgents: []string{"*Codex Desktop*"},
		},
	})

	// Should NOT rewrite for excluded user agent.
	got, _ := r.Resolve(context.Background(), "gpt-4", "Mozilla Codex Desktop 1.0")
	if got != "gpt-4" {
		t.Errorf("expected gpt-4 (excluded UA), got %s", got)
	}

	// Should rewrite for other user agents.
	got, _ = r.Resolve(context.Background(), "gpt-4", "curl/7.0")
	if got != "gpt-4o" {
		t.Errorf("expected gpt-4o, got %s", got)
	}
}

func TestResolver_FallbackModel(t *testing.T) {
	r := NewModelResolver(core.CompatConfig{
		Fallback: core.FallbackConfig{
			Enabled: true,
			Models:  map[string]string{"gpt-4o": "gpt-4o-mini"},
		},
	})

	if fb := r.FallbackModel("gpt-4o"); fb != "gpt-4o-mini" {
		t.Errorf("expected gpt-4o-mini, got %s", fb)
	}
	if fb := r.FallbackModel("unknown"); fb != "" {
		t.Errorf("expected empty fallback, got %s", fb)
	}
}

func TestResolver_FallbackModel_Disabled(t *testing.T) {
	r := NewModelResolver(core.CompatConfig{
		Fallback: core.FallbackConfig{
			Enabled: false,
			Models:  map[string]string{"gpt-4o": "gpt-4o-mini"},
		},
	})
	if fb := r.FallbackModel("gpt-4o"); fb != "" {
		t.Errorf("expected empty fallback when disabled, got %s", fb)
	}
}
