package ratelimit

import (
	"net/http"
	"strings"
)

// Middleware returns an HTTP middleware that rate-limits requests by API key.
// Requests without a valid Authorization header are allowed through without
// rate limiting (they will be rejected by downstream authentication).
// Requests that exceed the rate limit receive 429 Too Many Requests.
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		if auth != "" {
			key := extractAPIKey(auth)
			if key != "" && !l.Allow(key) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":"rate limit exceeded"}`))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
