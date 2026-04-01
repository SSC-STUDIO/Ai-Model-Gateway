package server

import (
	"bufio"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"ai-model-gateway/internal/config"
	"ai-model-gateway/internal/observability"
)

var requestCounter atomic.Uint64

const adminAuthCookie = "aigw_admin_token"

type adminAuthKind int

const (
	adminAuthNone adminAuthKind = iota
	adminAuthBearer
	adminAuthCookieOnly
)

type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get(observability.RequestIDHeader))
		if requestID == "" {
			requestID = generateRequestID()
		}

		w.Header().Set(observability.RequestIDHeader, requestID)
		next.ServeHTTP(w, r.WithContext(observability.WithRequestID(r.Context(), requestID)))
	})
}

func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &responseRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(recorder, r)

		log.Printf(
			"request_id=%s method=%s path=%s status=%d bytes=%d duration_ms=%d remote_addr=%q user_agent=%q",
			observability.RequestIDFromContext(r.Context()),
			r.Method,
			r.URL.Path,
			recorder.status,
			recorder.bytes,
			time.Since(start).Milliseconds(),
			r.RemoteAddr,
			r.UserAgent(),
		)
	})
}

func requireAdminAuth(getConfig func() config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isAdminRoute(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			cfg := getConfig()
			if !cfg.Admin.Enabled {
				http.NotFound(w, r)
				return
			}

			expected := strings.TrimSpace(cfg.Admin.AuthToken)
			if expected == "" {
				http.Error(w, "admin auth is not configured", http.StatusForbidden)
				return
			}

			authMode := adminAuthMode(r, expected)
			if authMode == adminAuthNone {
				w.Header().Set("WWW-Authenticate", `Bearer realm="aigw-admin"`)
				http.Error(w, "admin authentication required", http.StatusUnauthorized)
				return
			}
			if authMode == adminAuthCookieOnly && isAdminMutation(r) && !sameOriginAdminRequest(r) {
				http.Error(w, "admin same-origin write required", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.status = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *responseRecorder) Write(p []byte) (int, error) {
	n, err := r.ResponseWriter.Write(p)
	r.bytes += n
	return n, err
}

func (r *responseRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *responseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func (r *responseRecorder) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := r.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

func (r *responseRecorder) ReadFrom(src io.Reader) (int64, error) {
	if readerFrom, ok := r.ResponseWriter.(io.ReaderFrom); ok {
		n, err := readerFrom.ReadFrom(src)
		r.bytes += int(n)
		return n, err
	}

	n, err := io.Copy(r.ResponseWriter, src)
	r.bytes += int(n)
	return n, err
}

func generateRequestID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return hex.EncodeToString(buf[:])
	}
	return strconv.FormatUint(requestCounter.Add(1), 10)
}

func isAdminRoute(path string) bool {
	return strings.HasPrefix(path, "/admin") || strings.HasPrefix(path, "/-/admin/")
}

func isAdminMutation(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.Method == http.MethodPut && r.URL.Path == "/-/admin/config" {
		return true
	}
	if r.Method == http.MethodPost && (r.URL.Path == "/-/admin/config/rollback" || r.URL.Path == "/-/admin/upstreams/test") {
		return true
	}
	return false
}

func adminAuthMode(r *http.Request, expected string) adminAuthKind {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return adminAuthNone
	}
	if subtle.ConstantTimeCompare([]byte(bearerToken(r)), []byte(expected)) == 1 {
		return adminAuthBearer
	}
	if subtle.ConstantTimeCompare([]byte(cookieToken(r)), []byte(expected)) == 1 {
		return adminAuthCookieOnly
	}
	if subtle.ConstantTimeCompare([]byte(queryToken(r)), []byte(expected)) == 1 {
		return adminAuthBearer
	}
	return adminAuthNone
}

func sameOriginAdminRequest(r *http.Request) bool {
	if sameOriginRequestURL(r.Host, r.Header.Get("Origin")) {
		return true
	}
	return sameOriginRequestURL(r.Host, r.Header.Get("Referer"))
}

func sameOriginRequestURL(host, raw string) bool {
	host = strings.TrimSpace(host)
	raw = strings.TrimSpace(raw)
	if host == "" || raw == "" {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	if !strings.EqualFold(parsed.Host, host) {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func bearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "bearer "
	if len(auth) < len(prefix) {
		return ""
	}
	if !strings.EqualFold(auth[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(auth[len(prefix):])
}

func cookieToken(r *http.Request) string {
	cookie, err := r.Cookie(adminAuthCookie)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

func queryToken(r *http.Request) string {
	return strings.TrimSpace(r.URL.Query().Get("token"))
}
