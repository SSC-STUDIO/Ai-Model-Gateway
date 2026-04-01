package server

import (
	"net/http"

	"ai-model-gateway/internal/config"
	"ai-model-gateway/internal/router"
)

// adminPage returns an HTTP handler for the admin page
func adminPage(settingsView bool, manager *router.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		language := config.AdminLanguageChinese
		if manager != nil {
			language = manager.CurrentConfig().Admin.Language
		}
		_, _ = w.Write([]byte(renderAdminHTML(settingsView, language)))
	}
}

// adminFavicon returns an HTTP handler for the admin favicon
func adminFavicon() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write([]byte(adminIconSVG))
	}
}
