package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter 基于IP的速率限制器
type RateLimiter struct {
	limiters map[string]*rateLimiterEntry
	mu       sync.RWMutex
	config   RateLimitConfig
}

// rateLimiterEntry 单个IP的速率限制状态
type rateLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimitConfig 速率限制配置
type RateLimitConfig struct {
	Enabled      bool
	Requests     int
	Window       time.Duration
	Burst        int
	LoginLimit   int
	APIPathLimit int
}

// DefaultRateLimitConfig 返回默认速率限制配置
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		Enabled:      true,
		Requests:     100,
		Window:       time.Minute,
		Burst:        10,
		LoginLimit:   10,
		APIPathLimit: 200,
	}
}

// NewRateLimiter 创建新的速率限制器
func NewRateLimiter(config RateLimitConfig) *RateLimiter {
	rl := &RateLimiter{
		limiters: make(map[string]*rateLimiterEntry),
		config:   config,
	}
	// 启动清理协程
	go rl.cleanup()
	return rl
}

// GetLimiter 获取指定IP的限流器
func (rl *RateLimiter) GetLimiter(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	entry, exists := rl.limiters[ip]
	if !exists {
		limiter := rate.NewLimiter(rate.Every(rl.config.Window/time.Duration(rl.config.Requests)), rl.config.Burst)
		rl.limiters[ip] = &rateLimiterEntry{
			limiter:  limiter,
			lastSeen: time.Now(),
		}
		return limiter
	}

	entry.lastSeen = time.Now()
	return entry.limiter
}

// Allow 检查请求是否允许
func (rl *RateLimiter) Allow(ip string) bool {
	if !rl.config.Enabled {
		return true
	}
	return rl.GetLimiter(ip).Allow()
}

// AllowWithBurst 检查请求是否允许（使用指定的burst）
func (rl *RateLimiter) AllowWithBurst(ip string, burst int) bool {
	if !rl.config.Enabled {
		return true
	}
	// 获取限流器并检查
	return rl.GetLimiter(ip).AllowN(time.Now(), burst)
}

// cleanup 定期清理过期的限流器
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(time.Minute * 5)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		for ip, entry := range rl.limiters {
			if time.Since(entry.lastSeen) > time.Hour {
				delete(rl.limiters, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// RateLimitMiddleware 速率限制中间件
func RateLimitMiddleware(rl *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractClientIP(r)

			// 检查是否是登录端点
			if isLoginEndpoint(r.URL.Path) {
				if !rl.GetLimiter(ip).AllowN(time.Now(), rl.config.LoginLimit) {
					http.Error(w, `{"error":"rate limit exceeded","code":"RATE_LIMITED"}`, http.StatusTooManyRequests)
					return
				}
			}

			// 检查是否是API端点
			if isAPIEndpoint(r.URL.Path) {
				if !rl.GetLimiter(ip).AllowN(time.Now(), rl.config.APIPathLimit) {
					http.Error(w, `{"error":"rate limit exceeded","code":"RATE_LIMITED"}`, http.StatusTooManyRequests)
					return
				}
			}

			// 默认限流检查
			if !rl.Allow(ip) {
				http.Error(w, `{"error":"rate limit exceeded","code":"RATE_LIMITED"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractClientIP 提取客户端真实IP
func extractClientIP(r *http.Request) string {
	// 检查X-Forwarded-For头
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

	// 检查X-Real-IP头
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		if net.ParseIP(xri) != nil {
			return xri
		}
	}

	// 使用RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// isLoginEndpoint 检查是否是登录端点
func isLoginEndpoint(path string) bool {
	loginPaths := []string{
		"/auth/login",
		"/auth/token",
		"/v1/auth/login",
		"/api/auth/login",
	}
	lowerPath := strings.ToLower(path)
	for _, lp := range loginPaths {
		if strings.HasPrefix(lowerPath, lp) {
			return true
		}
	}
	return false
}

// isAPIEndpoint 检查是否是API端点
func isAPIEndpoint(path string) bool {
	apiPrefixes := []string{
		"/v1/",
		"/api/",
		"/v2/",
	}
	lowerPath := strings.ToLower(path)
	for _, prefix := range apiPrefixes {
		if strings.HasPrefix(lowerPath, prefix) {
			return true
		}
	}
	return false
}
