package api

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed embedded_admin_dist
var embeddedAdminDist embed.FS

var embeddedAdminFiles = mustSubFS(embeddedAdminDist, "embedded_admin_dist")

type adminFrontendBundle struct {
	index  []byte
	static http.Handler
}

func adminFrontendHandlers() (http.HandlerFunc, http.Handler) {
	if bundle, ok := loadDiskAdminFrontend(filepath.Join("web", "admin", "dist")); ok {
		return bundle.handlers()
	}
	if bundle, ok := loadEmbeddedAdminFrontend(); ok {
		return bundle.handlers()
	}
	return adminFrontendPlaceholderHandlers()
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

func (b adminFrontendBundle) handlers() (http.HandlerFunc, http.Handler) {
	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(b.index))
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
			b.static.ServeHTTP(w, r)
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

func adminFrontendPlaceholderHandlers() (http.HandlerFunc, http.Handler) {
	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><body><h1>AI-Model-Gateway Admin</h1><p>Embedded admin assets are unavailable.</p></body></html>`))
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
