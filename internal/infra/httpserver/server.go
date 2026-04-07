// Package httpserver provides the v2 HTTP server wiring.
// It mounts the /v1/* gateway routes and /admin/* management routes.
package httpserver

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"ai-model-gateway/internal/core"

	"github.com/go-chi/chi/v5"
)

// Server wraps the standard http.Server with v2-specific setup.
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
	return &Server{
		cfg:    cfg,
		router: r,
	}
}

// Router returns the underlying chi.Router for mounting routes.
func (s *Server) Router() chi.Router {
	return s.router
}

// ListenAndServe binds the listener and serves until the context is cancelled.
// On context cancellation it performs a graceful shutdown.
func (s *Server) ListenAndServe(ctx context.Context) error {
	var err error
	s.listener, err = net.Listen("tcp", s.cfg.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.cfg.Listen, err)
	}

	s.srv = &http.Server{
		Addr:           s.cfg.Listen,
		Handler:        withBodyLimit(s.router, s.cfg.MaxBodyBytes),
		ReadTimeout:    time.Duration(s.cfg.ReadTimeoutMs) * time.Millisecond,
		WriteTimeout:   time.Duration(s.cfg.WriteTimeoutMs) * time.Millisecond,
		IdleTimeout:    time.Duration(s.cfg.IdleTimeoutMs) * time.Millisecond,
		MaxHeaderBytes: 1 << 20, // 1 MB
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("[v2] listening on %s", s.cfg.Listen)
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
			log.Printf("[v2] shutdown error: %v", err)
		}
		return nil
	}
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
