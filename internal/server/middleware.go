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

// DefaultCORSConfig 返回默认的 CORS 配置 - SECURITY FIX: 更严格的默认配置
func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		// SECURITY FIX: 默认不配置任何允许的来源，必须由管理员显式配置
		AllowedOrigins: []string{},
		// SECURITY FIX: 只允许必要的方法
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		// SECURITY FIX: 最小化允许的头部
		AllowedHeaders: []string{
			"Accept",
			"Authorization",
			"Content-Type",
			"X-Requested-With",
			"X-Request-ID",
		},
		// SECURITY FIX: 最小化暴露的头部
		ExposedHeaders: []string{
			"Content-Length",
			"Content-Type",
			"X-Request-ID",
		},
		// SECURITY FIX: 允许凭证，但配合严格的来源验证
		AllowCredentials: true,
		// SECURITY FIX: 缓存预检请求24小时
		MaxAge: 86400,
	}
}

// corsMiddleware 处理 CORS 请求 - SECURITY FIX: 增强的安全性
func corsMiddleware(config CORSConfig) func(http.Handler) http.Handler {
	// SECURITY FIX: 如果通配符在配置中，记录警告
	hasWildcard := false
	allowedOrigins := make([]string, 0, len(config.AllowedOrigins))
	for _, origin := range config.AllowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		if origin == "*" {
			hasWildcard = true
			// SECURITY FIX: 不允许使用通配符与 AllowCredentials: true
			// 如果检测到通配符，记录警告并跳过
			if config.AllowCredentials {
				log.Println("[SECURITY WARNING] CORS wildcard (*) origin is not allowed with AllowCredentials=true. Ignoring wildcard.")
				continue
			}
		}
		allowedOrigins = append(allowedOrigins, origin)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				origin = "*"
			}

			// SECURITY FIX: 如果没有 Origin 头，可能是同源请求或直接的API调用
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			// SECURITY FIX: 严格验证 Origin 格式
			if !isValidOrigin(origin) {
				log.Printf("[SECURITY] Invalid origin format rejected: %s", origin)
				http.Error(w, "Invalid Origin header", http.StatusBadRequest)
				return
			}

			// 检查 Origin 是否允许
			isAllowed := false
			if len(allowedOrigins) == 0 {
				// SECURITY FIX: 如果没有配置允许的来源，拒绝所有跨域请求
				isAllowed = false
			} else if hasWildcard && !config.AllowCredentials {
				// 通配符允许（但不允许凭证）
				isAllowed = true
			} else {
				// 检查具体匹配
				for _, allowed := range allowedOrigins {
					if matchOriginStrict(origin, allowed) {
						isAllowed = true
						break
					}
				}
			} else {
				allowOrigin = origin
			}

			// 如果不允许，拒绝请求
			if !isAllowed {
				log.Printf("[SECURITY] CORS origin rejected: %s", origin)
				http.Error(w, "CORS origin not allowed", http.StatusForbidden)
				return
			}

			// 设置 CORS 头
			w.Header().Set("Access-Control-Allow-Origin", origin)
			if config.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			// 处理预检请求
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", strings.Join(config.AllowedMethods, ", "))
				w.Header().Set("Access-Control-Allow-Headers", strings.Join(config.AllowedHeaders, ", "))
				if config.MaxAge > 0 {
					w.Header().Set("Access-Control-Max-Age", strconv.Itoa(config.MaxAge))
				}
				// SECURITY FIX: 添加 Vary 头
				w.Header().Set("Vary", "Origin")
				w.WriteHeader(http.StatusNoContent)
				return
			}

			// SECURITY FIX: 添加 Vary 头以防止缓存问题
			w.Header().Set("Vary", "Origin")
			next.ServeHTTP(w, r)
		})
	}
}

// isValidOrigin 验证 Origin 格式是否有效
func isValidOrigin(origin string) bool {
	if origin == "" {
		return true
	}
	
	// 验证基本格式: scheme://host[:port]
	origin = strings.ToLower(origin)
	
	// 允许的特殊值
	if origin == "null" {
		return true
	}
	
	// 检查 scheme
	validSchemes := []string{"http://", "https://"}
	hasValidScheme := false
	for _, scheme := range validSchemes {
		if strings.HasPrefix(origin, scheme) {
			hasValidScheme = true
			break
		}
	}
	
	if !hasValidScheme {
		return false
	}
	
	// 检查长度限制（防止 DoS）
	if len(origin) > 2048 {
		return false
	}
	
	// 检查禁止字符
	forbiddenChars := []string{"\n", "\r", "\x00", "<", ">", "\"", "'", "`"}
	for _, char := range forbiddenChars {
		if strings.Contains(origin, char) {
			return false
		}
	}
	
	return true
}

// matchOriginStrict 严格检查 origin 是否匹配允许的模式
func matchOriginStrict(origin, pattern string) bool {
	origin = strings.ToLower(strings.TrimSpace(origin))
	pattern = strings.ToLower(strings.TrimSpace(pattern))

	if pattern == origin {
		return true
	}

	// SECURITY FIX: 更严格的通配符匹配
	if strings.Contains(pattern, "*") {
		// 只允许在子域名位置使用通配符
		parts := strings.Split(pattern, "*")
		if len(parts) == 2 {
			// 确保通配符只用于子域名匹配，而不是顶级域或路径
			beforeWildcard := parts[0]
			afterWildcard := parts[1]
			
			// 验证通配符位置（应该只用于子域名）
			if strings.HasSuffix(beforeWildcard, ".") && strings.HasPrefix(afterWildcard, ".") {
				// 例如: https://*.example.com
				return strings.HasPrefix(origin, beforeWildcard) && strings.HasSuffix(origin, afterWildcard)
			}
		}
		
		// SECURITY FIX: 拒绝不安全的通配符使用
		return false
	}

	return false
}

// securityHeaders adds security-related HTTP headers to all responses
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}
