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
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"ai-model-gateway/internal/config"
	"ai-model-gateway/internal/observability"
)

var requestCounter atomic.Uint64

const adminAuthCookie = "aigw_admin_token"

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
			if !strings.HasPrefix(r.URL.Path, "/admin") && !strings.HasPrefix(r.URL.Path, "/-/admin/") {
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

			// Audit logging for admin operations
			requestID := observability.RequestIDFromContext(r.Context())
			logAdminAccess(r, requestID, false, "")

			if token := strings.TrimSpace(r.URL.Query().Get("token")); subtle.ConstantTimeCompare([]byte(token), []byte(expected)) == 1 {
				http.SetCookie(w, &http.Cookie{
					Name:     adminAuthCookie,
					Value:    token,
					Path:     "/",
					HttpOnly: true,
					SameSite: http.SameSiteStrictMode,
					Secure:   true,
					MaxAge:   86400,
				})
				logAdminAccess(r, requestID, true, "query_token")
				next.ServeHTTP(w, r)
				return
			}

			if subtle.ConstantTimeCompare([]byte(bearerToken(r)), []byte(expected)) == 1 {
				logAdminAccess(r, requestID, true, "bearer_token")
				next.ServeHTTP(w, r)
				return
			}

			if subtle.ConstantTimeCompare([]byte(cookieToken(r)), []byte(expected)) == 1 {
				logAdminAccess(r, requestID, true, "cookie_token")
				next.ServeHTTP(w, r)
				return
			}

			// Failed authentication attempt - audit log
			logAdminAccess(r, requestID, false, "failed")
			w.Header().Set("WWW-Authenticate", `Bearer realm="aigw-admin"`)
			http.Error(w, "admin authentication required", http.StatusUnauthorized)
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

// extractClientIP extracts the client IP address from the request
func extractClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			ip := strings.TrimSpace(ips[0])
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}

	// Check X-Real-IP header
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		if net.ParseIP(xri) != nil {
			return xri
		}
	}

	// Use RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func bearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return ""
	}
	return strings.TrimSpace(auth[len("Bearer "):])
}

func cookieToken(r *http.Request) string {
	cookie, err := r.Cookie(adminAuthCookie)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

// CORSConfig 存储 CORS 配置
type CORSConfig struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
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
			if len(config.ExposedHeaders) > 0 {
				w.Header().Set("Access-Control-Expose-Headers", strings.Join(config.ExposedHeaders, ", "))
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
		// Security headers to prevent common attacks
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()")

		// Content Security Policy - prevents XSS and data injection attacks
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; "+
				"font-src 'self'; "+
				"connect-src 'self'; "+
				"media-src 'self'; "+
				"object-src 'none'; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'; "+
				"form-action 'self';")

		// HTTP Strict Transport Security - forces HTTPS
		w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")

		// Prevent browsers from MIME-type sniffing
		w.Header().Set("X-Permitted-Cross-Domain-Policies", "none")

		// Cross-Origin policies
		w.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")

		next.ServeHTTP(w, r)
	})
}
