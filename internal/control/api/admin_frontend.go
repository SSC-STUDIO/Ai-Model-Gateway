package api

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed embedded_admin_dist
var embeddedAdminDist embed.FS

var embeddedAdminFiles = mustSubFS(embeddedAdminDist, "embedded_admin_dist")

// AdminFrontendBundle serves the admin UI frontend.
// It supports both embedded (production) and disk-based (development) modes.
// In development mode, it watches the dist directory for changes and reloads automatically.
type AdminFrontendBundle struct {
	mu     sync.RWMutex
	index  []byte
	static http.Handler

	// dev mode fields
	devMode  bool
	distDir  string
	lastHash [32]byte
	stopCh   chan struct{}
}

// NewAdminFrontendBundle creates a new frontend bundle.
// If distDir is non-empty and exists, it uses disk-based mode (development).
// Otherwise it falls back to embedded assets (production).
func NewAdminFrontendBundle(distDir string) (*AdminFrontendBundle, error) {
	// Try disk mode first if distDir is specified
	if distDir != "" {
		if bundle, ok := loadDiskAdminFrontend(distDir); ok {
			b := &AdminFrontendBundle{
				index:   bundle.index,
				static:  bundle.static,
				devMode: true,
				distDir: distDir,
				stopCh:  make(chan struct{}),
			}
			b.startWatcher()
			return b, nil
		}
	}

	// Fall back to embedded
	if bundle, ok := loadEmbeddedAdminFrontend(); ok {
		return &AdminFrontendBundle{
			index:  bundle.index,
			static: bundle.static,
		}, nil
	}

	return nil, fmt.Errorf("no admin frontend assets available")
}

// Handlers returns the root (index.html) and assets HTTP handlers.
func (b *AdminFrontendBundle) Handlers() (http.HandlerFunc, http.Handler) {
	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.RLock()
		index := b.index
		b.mu.RUnlock()
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
	})

	assets := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle admin routes
		if r.URL.Path == "/admin" || r.URL.Path == "/admin/" {
			root.ServeHTTP(w, r)
			return
		}

		// Handle static assets under /admin/
		if strings.HasPrefix(r.URL.Path, "/admin/assets/") ||
			r.URL.Path == "/admin/icon.svg" ||
			r.URL.Path == "/admin/favicon.svg" ||
			r.URL.Path == "/admin/manifest.json" {
			b.mu.RLock()
			static := b.static
			b.mu.RUnlock()
			static.ServeHTTP(w, r)
			return
		}

		// Handle root-level static resources (for PWA manifest icons)
		if r.URL.Path == "/icon.svg" ||
			r.URL.Path == "/favicon.svg" ||
			r.URL.Path == "/manifest.json" {
			// For root-level assets, serve directly from embedded FS without strip
			http.FileServer(http.FS(embeddedAdminFiles)).ServeHTTP(w, r)
			return
		}

		root.ServeHTTP(w, r)
	})
	return root, assets
}

// Close stops the file watcher in development mode.
func (b *AdminFrontendBundle) Close() {
	if b.stopCh != nil {
		select {
		case <-b.stopCh:
			return
		default:
			close(b.stopCh)
		}
	}
}

// startWatcher starts a file watcher that polls the dist directory for changes.
func (b *AdminFrontendBundle) startWatcher() {
	if !b.devMode {
		return
	}

	// Compute initial hash of index.html
	b.lastHash = b.computeDistHash()

	go b.watchLoop()
}

func (b *AdminFrontendBundle) watchLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-b.stopCh:
			return
		case <-ticker.C:
			b.checkAndReload()
		}
	}
}

func (b *AdminFrontendBundle) checkAndReload() {
	newHash := b.computeDistHash()
	if newHash == b.lastHash {
		return
	}

	// Small delay to allow file writes to settle
	time.Sleep(100 * time.Millisecond)

	// Re-read and verify hash is stable
	settledHash := b.computeDistHash()
	if settledHash != newHash {
		return
	}

	// Reload from disk
	if bundle, ok := loadDiskAdminFrontend(b.distDir); ok {
		b.mu.Lock()
		b.index = bundle.index
		b.static = bundle.static
		b.lastHash = settledHash
		b.mu.Unlock()

		// Log reload (use fmt to avoid import cycle with log package)
		fmt.Fprintf(os.Stderr, "[admin] frontend reloaded from %s\n", b.distDir)
	}
}

func (b *AdminFrontendBundle) computeDistHash() [32]byte {
	indexPath := filepath.Join(b.distDir, "index.html")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return [32]byte{}
	}
	return sha256.Sum256(data)
}

func loadDiskAdminFrontend(distDir string) (adminFrontendBundle, bool) {
	indexPath := filepath.Join(distDir, "index.html")
	info, err := os.Stat(indexPath)
	if err != nil || info.IsDir() {
		return adminFrontendBundle{}, false
	}

	index, err := os.ReadFile(indexPath)
	if err != nil {
		return adminFrontendBundle{}, false
	}

	return adminFrontendBundle{
		index:  index,
		static: http.StripPrefix("/admin/", http.FileServer(http.Dir(distDir))),
	}, true
}

func loadEmbeddedAdminFrontend() (adminFrontendBundle, bool) {
	index, err := fs.ReadFile(embeddedAdminFiles, "index.html")
	if err != nil {
		return adminFrontendBundle{}, false
	}

	return adminFrontendBundle{
		index:  index,
		static: http.StripPrefix("/admin/", http.FileServer(http.FS(embeddedAdminFiles))),
	}, true
}

type adminFrontendBundle struct {
	index  []byte
	static http.Handler
}

func adminFrontendPlaceholderHandlers() (http.HandlerFunc, http.Handler) {
	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><body><h1>AI Model Gateway Admin</h1><p>Embedded admin assets are unavailable.</p></body></html>`))
	})
	return fallback, fallback
}

func mustSubFS(root fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(root, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
