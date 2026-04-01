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

		// 使用安全的日志记录
		SafeAccessLog(
			observability.RequestIDFromContext(r.Context()),
			r.Method,
			r.URL.Path,
			r.RemoteAddr,
			r.UserAgent(),
			recorder.status,
			recorder.bytes,
			time.Since(start).Milliseconds(),
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

// logAdminAccess logs admin access attempts for audit purposes
func logAdminAccess(r *http.Request, requestID string, success bool, authMethod string) {
	clientIP := extractClientIP(r)
	method := r.Method
	path := r.URL.Path
	userAgent := r.UserAgent()
	
	status := "denied"
	if success {
		status = "granted"
	}

	log.Printf(
		"[AUDIT] admin_access %s method=%s path=%s client_ip=%s user_agent=%q auth_method=%s request_id=%s",
		status,
		method,
		path,
		clientIP,
		userAgent,
		authMethod,
		requestID,
	)
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

// CORSConfig 存储 CORS 配置
type CORSConfig struct {
	AllowOrigins     []string
	AllowMethods     []string
	AllowHeaders     []string
	AllowCredentials bool
	MaxAge           int
}

// DefaultCORSConfig 返回默认 CORS 配置
func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           86400,
	}
}

// corsMiddleware 返回 CORS 中间件
func corsMiddleware(config CORSConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				origin = "*"
			}

			// 检查是否允许该来源
			allowOrigin := "*"
			if len(config.AllowOrigins) > 0 && config.AllowOrigins[0] != "*" {
				for _, o := range config.AllowOrigins {
					if o == origin {
						allowOrigin = origin
						break
					}
				}
			} else {
				allowOrigin = origin
			}

			w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
			w.Header().Set("Access-Control-Allow-Methods", strings.Join(config.AllowMethods, ", "))
			w.Header().Set("Access-Control-Allow-Headers", strings.Join(config.AllowHeaders, ", "))
			w.Header().Set("Access-Control-Max-Age", strconv.Itoa(config.MaxAge))

			if config.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			// 处理预检请求
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// securityHeaders 添加安全响应头
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}
