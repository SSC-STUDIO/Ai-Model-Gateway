// Package httpserver provides the HTTP server wiring.
// It mounts the /v1/* gateway routes and /admin/* management routes.
package httpserver

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"ai-model-gateway/internal/core"

	"github.com/go-chi/chi/v5"
)

// Server wraps the standard http.Server with gateway-specific setup.
type Server struct {
	cfg      core.ServerConfig
	router   chi.Router
	listener net.Listener
	srv      *http.Server
}

// New creates a Server but does not start listening.
// Use Mount* methods to attach route groups before calling ListenAndServe.
func New(cfg core.ServerConfig) *Server {
	r := chi.NewRouter()
	r.Use(securityHeaders)
	return &Server{
		cfg:    cfg,
		router: r,
	}
}

// securityHeaders adds standard security headers to all responses.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

// Router returns the underlying chi.Router for mounting routes.
func (s *Server) Router() chi.Router {
	return s.router
}

// ListenAndServe binds the listener and serves until the context is cancelled.
// On context cancellation it performs a graceful shutdown.
func (s *Server) ListenAndServe(ctx context.Context) error {
	var err error
	network, bindAddr := resolveListenTarget(s.cfg.Listen)
	s.listener, err = net.Listen(network, bindAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", bindAddr, err)
	}

	s.srv = newHTTPServer(s.cfg, withBodyLimit(s.router, s.cfg.MaxBodyBytes))

	errCh := make(chan error, 1)
	go func() {
		log.Printf("[gateway] listening on %s", bindAddr)
		if err := s.srv.Serve(s.listener); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.srv.Shutdown(shutCtx); err != nil {
			log.Printf("[gateway] shutdown error: %v", err)
		}
		return nil
	}
}

func newHTTPServer(cfg core.ServerConfig, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:           cfg.Listen,
		Handler:        handler,
		ReadTimeout:    core.MillisecondsToDuration(cfg.ReadTimeoutMs, 30*time.Second),
		WriteTimeout:   serverWriteTimeout(cfg),
		IdleTimeout:    core.MillisecondsToDuration(cfg.IdleTimeoutMs, 120*time.Second),
		MaxHeaderBytes: 1 << 20, // 1 MB
	}
}

func serverWriteTimeout(cfg core.ServerConfig) time.Duration {
	if cfg.WriteTimeoutMs <= 0 {
		// SSE and long-running buffered fallbacks need an unlimited write window.
		return 0
	}
	return core.MillisecondsToDuration(cfg.WriteTimeoutMs, 0)
}

// withBodyLimit wraps a handler with a request body size limit.
func withBodyLimit(h http.Handler, maxBytes int64) http.Handler {
	if maxBytes <= 0 {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		h.ServeHTTP(w, r)
	})
}

func resolveListenTarget(listen string) (network string, bindAddr string) {
	trimmed := strings.TrimSpace(listen)
	if strings.HasPrefix(trimmed, ":") {
		return "tcp4", "0.0.0.0" + trimmed
	}
	return "tcp", trimmed
}
