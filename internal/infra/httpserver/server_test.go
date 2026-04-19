package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ai-model-gateway/internal/core"
)

func TestResolveListenTarget_BarePortUsesIPv4Wildcard(t *testing.T) {
	network, addr := resolveListenTarget(":18080")
	if network != "tcp4" {
		t.Fatalf("expected tcp4 network, got %q", network)
	}
	if addr != "0.0.0.0:18080" {
		t.Fatalf("expected IPv4 wildcard bind, got %q", addr)
	}
}

func TestResolveListenTarget_PreservesExplicitHost(t *testing.T) {
	network, addr := resolveListenTarget("127.0.0.1:18080")
	if network != "tcp" {
		t.Fatalf("expected tcp network, got %q", network)
	}
	if addr != "127.0.0.1:18080" {
		t.Fatalf("expected explicit host to be preserved, got %q", addr)
	}
}

func TestServerWriteTimeout_DefaultDisabledForStreaming(t *testing.T) {
	srv := newHTTPServer(core.ServerConfig{}, http.NewServeMux())
	if srv.WriteTimeout != 0 {
		t.Fatalf("expected default write timeout to be disabled, got %s", srv.WriteTimeout)
	}
}

func TestServerWriteTimeout_UsesExplicitConfig(t *testing.T) {
	srv := newHTTPServer(core.ServerConfig{WriteTimeoutMs: 45000}, http.NewServeMux())
	if srv.WriteTimeout != 45*time.Second {
		t.Fatalf("expected explicit write timeout 45s, got %s", srv.WriteTimeout)
	}
}

func TestNew(t *testing.T) {
	cfg := core.ServerConfig{Listen: ":18080"}
	srv := New(cfg)
	if srv == nil {
		t.Fatal("New returned nil")
	}
	if srv.Router() == nil {
		t.Error("Router should not be nil")
	}
}

func TestRouter(t *testing.T) {
	srv := New(core.ServerConfig{})
	if srv.Router() == nil {
		t.Error("Router returned nil")
	}
}

func TestSecurityHeaders(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	securityHeaders(handler).ServeHTTP(rec, req)

	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing X-Content-Type-Options header")
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("missing X-Frame-Options header")
	}
	if rec.Header().Get("Referrer-Policy") != "strict-origin-when-cross-origin" {
		t.Error("missing Referrer-Policy header")
	}
}

func TestWithBodyLimit_NoLimit(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/test", strings.NewReader("test"))
	rec := httptest.NewRecorder()

	// maxBytes <= 0 should return the handler unchanged
	wrapped := withBodyLimit(handler, 0)
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestWithBodyLimit_WithLimit(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/test", strings.NewReader("test"))
	rec := httptest.NewRecorder()

	wrapped := withBodyLimit(handler, 1024)
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestListenAndServe_ContextCancellation(t *testing.T) {
	srv := New(core.ServerConfig{Listen: ":0"})

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe(ctx)
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context to trigger shutdown
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("ListenAndServe did not return after context cancellation")
	}
}

func TestResolveListenTarget_Whitespace(t *testing.T) {
	network, addr := resolveListenTarget("  :18080  ")
	if network != "tcp4" {
		t.Errorf("expected tcp4, got %s", network)
	}
	if addr != "0.0.0.0:18080" {
		t.Errorf("expected 0.0.0.0:18080, got %s", addr)
	}
}

func TestNewHTTPServer_Timeouts(t *testing.T) {
	cfg := core.ServerConfig{
		Listen:         ":18080",
		ReadTimeoutMs:  10000,
		WriteTimeoutMs: 20000,
		IdleTimeoutMs:  30000,
	}

	srv := newHTTPServer(cfg, http.NewServeMux())

	if srv.ReadTimeout != 10*time.Second {
		t.Errorf("expected 10s read timeout, got %s", srv.ReadTimeout)
	}
	if srv.WriteTimeout != 20*time.Second {
		t.Errorf("expected 20s write timeout, got %s", srv.WriteTimeout)
	}
	if srv.IdleTimeout != 30*time.Second {
		t.Errorf("expected 30s idle timeout, got %s", srv.IdleTimeout)
	}
}
