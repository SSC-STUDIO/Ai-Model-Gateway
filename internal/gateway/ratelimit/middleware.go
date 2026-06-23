package ratelimit

import (
	"net"
	"net/http"
	"strings"
)

// Middleware returns an HTTP middleware that rate-limits requests by API key.
// Requests with a valid Authorization header are rate-limited by API key.
// Requests without an Authorization header are rate-limited by client IP.
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
		} else {
			// Fallback: rate-limit unauthenticated requests by client IP.
			ip := extractIP(r)
			if ip != "" && !l.Allow("ip:"+ip) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":"rate limit exceeded"}`))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// extractIP returns the client IP from the request, respecting common proxy headers.
func extractIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if ip := strings.TrimSpace(strings.SplitN(fwd, ",", 2)[0]); ip != "" {
			return ip
		}
	}
	if real := r.Header.Get("X-Real-IP"); real != "" {
		return strings.TrimSpace(real)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
