package api

import (
	"testing"
)

func TestBuildReplayURLAcceptsNormalPath(t *testing.T) {
	got, err := buildReplayURL("http://127.0.0.1:18080", "/v1/chat/completions")
	if err != nil {
		t.Fatalf("buildReplayURL returned error: %v", err)
	}
	want := "http://127.0.0.1:18080/v1/chat/completions"
	if got != want {
		t.Fatalf("buildReplayURL = %q, want %q", got, want)
	}
}

func TestBuildReplayURLPreservesQueryString(t *testing.T) {
	got, err := buildReplayURL("http://127.0.0.1:18080", "/v1/messages?debug=1")
	if err != nil {
		t.Fatalf("buildReplayURL returned error: %v", err)
	}
	want := "http://127.0.0.1:18080/v1/messages?debug=1"
	if got != want {
		t.Fatalf("buildReplayURL = %q, want %q", got, want)
	}
}

func TestBuildReplayURLRejectsProtocolRelative(t *testing.T) {
	_, err := buildReplayURL("http://127.0.0.1:18080", "//evil.example/path")
	if err == nil {
		t.Fatal("expected error for protocol-relative path")
	}
}

func TestBuildReplayURLRejectsAbsoluteScheme(t *testing.T) {
	_, err := buildReplayURL("http://127.0.0.1:18080", "http://evil.example/x")
	if err == nil {
		t.Fatal("expected error for absolute URL path")
	}
}

func TestBuildReplayURLRejectsRelativePath(t *testing.T) {
	_, err := buildReplayURL("http://127.0.0.1:18080", "v1/foo")
	if err == nil {
		t.Fatal("expected error for non-absolute path")
	}
}

func TestBuildReplayURLRejectsEmptyPath(t *testing.T) {
	_, err := buildReplayURL("http://127.0.0.1:18080", "")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestBuildReplayURLRejectsControlChars(t *testing.T) {
	_, err := buildReplayURL("http://127.0.0.1:18080", "/v1/chat\r\nHost: evil.example")
	if err == nil {
		t.Fatal("expected error for path containing CRLF")
	}
}

func TestBuildReplayURLRejectsInvalidGatewayURL(t *testing.T) {
	if _, err := buildReplayURL("", "/foo"); err == nil {
		t.Fatal("expected error for empty gateway URL")
	}
	if _, err := buildReplayURL("not a url", "/foo"); err == nil {
		t.Fatal("expected error for malformed gateway URL")
	}
}

func TestBuildReplayURLParentDotDotStaysOnHost(t *testing.T) {
	// ResolveReference will collapse "../" segments against the base, but the
	// result must still point at the original gateway host.
	got, err := buildReplayURL("http://127.0.0.1:18080/api", "/../v1/foo")
	if err != nil {
		t.Fatalf("buildReplayURL returned error: %v", err)
	}
	// Go's ResolveReference normalises "/../v1/foo" to "/v1/foo" under the base host.
	if got != "http://127.0.0.1:18080/v1/foo" {
		t.Fatalf("unexpected resolved URL: %q", got)
	}
}
