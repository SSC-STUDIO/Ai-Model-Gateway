package clientconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListenToOrigin(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{":18080", "http://127.0.0.1:18080"},
		{"127.0.0.1:18080", "http://127.0.0.1:18080"},
		{"0.0.0.0:18080", "http://127.0.0.1:18080"},
		{"http://127.0.0.1:9999/foo", "http://127.0.0.1:9999"},
	}
	for _, tc := range tests {
		got, err := ListenToOrigin(tc.in)
		if err != nil {
			t.Fatalf("ListenToOrigin(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("ListenToOrigin(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestOpenAICompatibleBase(t *testing.T) {
	got := OpenAICompatibleBase("http://127.0.0.1:18080")
	want := "http://127.0.0.1:18080/v1"
	if got != want {
		t.Fatalf("OpenAICompatibleBase = %q, want %q", got, want)
	}
}

func TestParseTools(t *testing.T) {
	all := ParseTools("all")
	if !all.Codex || !all.Claude || !all.OpenClaw {
		t.Fatalf("ParseTools(all) = %+v", all)
	}
	sub := ParseTools(" codex , openclaw ")
	if !sub.Codex || sub.Claude || !sub.OpenClaw {
		t.Fatalf("ParseTools(subset) = %+v", sub)
	}
	none := ParseTools("none")
	if none.Codex || none.Claude || none.OpenClaw {
		t.Fatalf("ParseTools(none) = %+v", none)
	}
}

func TestMergeCodexConfig(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	if err := MergeCodexConfig(p, "http://127.0.0.1:18080/v1"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) == "" {
		t.Fatal("empty file")
	}
}

func TestMergeClaudeSettings(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	if err := MergeClaudeSettings(p, "http://127.0.0.1:18080/v1", "sk-test"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 50 {
		t.Fatalf("unexpected short file: %s", b)
	}
}

func TestMergeOpenClawConfig(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "openclaw.json")
	// JWCC with trailing comma
	if err := os.WriteFile(p, []byte(`{
  "gateway": {"port": 18789},
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MergeOpenClawConfig(p, "http://127.0.0.1:18080/v1", "${KEY}", "gpt-4o", true); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 80 {
		t.Fatalf("unexpected short file: %s", b)
	}
}

func TestResolveOriginFromFile(t *testing.T) {
	dir := t.TempDir()
	gw := filepath.Join(dir, "gatewayd.json")
	if err := os.WriteFile(gw, []byte(`{"listen":":18090"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	origin, err := ResolveOrigin("", gw)
	if err != nil {
		t.Fatal(err)
	}
	if want := "http://127.0.0.1:18090"; origin != want {
		t.Fatalf("origin = %q, want %q", origin, want)
	}
}
