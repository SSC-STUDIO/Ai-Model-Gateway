package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"ai-model-gateway/internal/config"
	"ai-model-gateway/internal/router"
	"ai-model-gateway/internal/server"
	"ai-model-gateway/internal/state"
	"ai-model-gateway/internal/telemetry"
)

// maxBodySizeKey 用于在上下文中存储最大请求体大小
type maxBodySizeKey struct{}

// withRequestBodyLimit 包装 handler 添加请求体大小限制
func withRequestBodyLimit(h http.Handler, maxBytes int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 设置请求体的最大读取大小
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		// 将限制添加到上下文
		ctx := context.WithValue(r.Context(), maxBodySizeKey{}, maxBytes)
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func Run(ctx context.Context, configPath string) error {
	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	store := state.NewConfigStoreWithPath(cfg, configPath)
	manager := router.NewManager(store)
	stats, err := telemetry.NewStore(cfg.Telemetry.SQLitePath)
	if err != nil {
		return fmt.Errorf("init telemetry store: %w", err)
	}
	defer func() {
		if err := stats.Close(); err != nil {
			log.Printf("close telemetry store: %v", err)
		}
	}()

	pricing := telemetry.NewPricingCatalog(cfg.Pricing)
	pricing.Start(ctx)

	go manager.StartHealthChecks(ctx)

	watcher := config.Watcher{Debounce: time.Duration(cfg.Reload.DebounceMs) * time.Millisecond}
	go func() {
		if err := watcher.WatchFile(ctx, configPath, func(newCfg config.Config) {
			currentCfg := store.Get()
			if !currentCfg.Reload.Enabled && !newCfg.Reload.Enabled {
				return
			}
			manager.SetConfig(newCfg)
			pricing.UpdateConfig(newCfg.Pricing)
			log.Printf("config reloaded")
		}); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("watch config failed: %v", err)
		}
	}()

	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Listen, err)
	}

	srv := &http.Server{
		Addr:           cfg.Listen,
		Handler:        server.NewRouter(manager, stats, pricing),
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   60 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1 MB
	}

	// 包装 handler 添加请求体大小限制
	srv.Handler = withRequestBodyLimit(srv.Handler, 100<<20) // 100 MB

	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on %s", cfg.Listen)
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("server error: %w", err)
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("server shutdown error: %v", err)
		}
		return nil
	}
}
