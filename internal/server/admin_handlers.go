package server

import (
	"net/http"
	"strings"

	"ai-model-gateway/internal/config"
	"ai-model-gateway/internal/router"
)

// adminPage 返回管理页面处理器
func adminPage(settingsView bool, manager *router.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		language := config.AdminLanguageChinese
		if manager != nil {
			language = manager.CurrentConfig().Admin.Language
		}
		queryToken := strings.TrimSpace(r.URL.Query().Get("token"))
		_, _ = w.Write([]byte(renderAdminHTML(settingsView, language, queryToken)))
	}
}

// adminFavicon 返回 favicon 处理器
func adminFavicon() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write([]byte(adminIconSVG))
	}
}
