package clientconfig

import (
	"strings"
)

// ToolSet selects which local clients to configure.
type ToolSet struct {
	Codex    bool
	Claude   bool
	OpenClaw bool
}

// OpenClawProviderID is the default models.providers key for the gateway.
const OpenClawProviderID = "ai-model-gateway"

// ParseTools parses a comma-separated list. "all" or empty enables every tool.
func ParseTools(s string) ToolSet {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "all" {
		return ToolSet{Codex: true, Claude: true, OpenClaw: true}
	}
	if s == "none" {
		return ToolSet{}
	}
	var t ToolSet
	for _, part := range strings.Split(s, ",") {
		p := strings.TrimSpace(part)
		switch p {
		case "codex":
			t.Codex = true
		case "claude", "claude-code":
			t.Claude = true
		case "openclaw":
			t.OpenClaw = true
		}
	}
	return t
}
