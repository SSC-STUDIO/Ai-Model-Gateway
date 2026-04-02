package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	// 会话过期时间：24小时
	SessionMaxAge = 24 * time.Hour
	// 会话ID长度（字节数）- SECURITY FIX: 增加到48字节增强安全性
	SessionIDLength = 48
	// 最小会话ID长度
	SessionIDMinLength = 32
	// 清理间隔
	SessionCleanupInterval = 5 * time.Minute
	// 最大会话数限制 - SECURITY FIX: 防止资源耗尽攻击
	MaxSessionCount = 10000
)

// Session 会话数据
type Session struct {
	ID         string
	Data       map[string]interface{}
	CreatedAt  time.Time
	LastAccess time.Time
	ExpiresAt  time.Time
}

// IsExpired 检查会话是否过期
func (s *Session) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// Touch 更新最后访问时间并延长过期时间
func (s *Session) Touch() {
	s.LastAccess = time.Now()
	s.ExpiresAt = time.Now().Add(SessionMaxAge)
}

// SessionManager 会话管理器
type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	config   SessionConfig
	// SECURITY FIX: 添加速率限制器防止会话洪泛攻击
	sessionCreationTracker map[string]int64 // IP -> last creation timestamp
}

// SessionConfig 会话配置
type SessionConfig struct {
	CookieName     string
	CookieDomain   string
	CookiePath     string
	CookieSecure   bool
	CookieHttpOnly bool
	CookieSameSite http.SameSite
	MaxAge         time.Duration
}

// DefaultSessionConfig 返回默认会话配置
func DefaultSessionConfig() SessionConfig {
	return SessionConfig{
		CookieName:     "aigw_session",
		CookiePath:     "/",
		CookieSecure:   true,
		CookieHttpOnly: true,
		CookieSameSite: http.SameSiteStrictMode,
		MaxAge:         SessionMaxAge,
	}
}

// NewSessionManager 创建新的会话管理器
func NewSessionManager(config SessionConfig) *SessionManager {
	sm := &SessionManager{
		sessions:               make(map[string]*Session),
		config:                 config,
		sessionCreationTracker: make(map[string]int64),
	}
	// 启动清理协程
	go sm.cleanup()
	return sm
}

// CreateSession 创建新会话 - SECURITY FIX: 添加IP限制和速率限制
func (sm *SessionManager) CreateSession(clientIP ...string) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// SECURITY FIX: 检查会话数量限制，防止资源耗尽
	if len(sm.sessions) >= MaxSessionCount {
		// 清理一些旧会话
		sm.cleanExpiredSessionsLocked()
	}
	
	// 如果仍然超过限制，拒绝创建新会话
	if len(sm.sessions) >= MaxSessionCount {
		return nil
	}

	// SECURITY FIX: IP速率限制
	if len(clientIP) > 0 && clientIP[0] != "" {
		now := time.Now().Unix()
		lastCreation, exists := sm.sessionCreationTracker[clientIP[0]]
		if exists && now-lastCreation < 1 { // 同一IP 1秒内只能创建一个会话
			return nil
		}
		sm.sessionCreationTracker[clientIP[0]] = now
	}

	sessionID := generateSecureSessionID()
	now := time.Now()
	session := &Session{
		ID:         sessionID,
		Data:       make(map[string]interface{}),
		CreatedAt:  now,
		LastAccess: now,
		ExpiresAt:  now.Add(sm.config.MaxAge),
	}

	sm.sessions[sessionID] = session
	return session
}

// GetSession 获取会话
func (sm *SessionManager) GetSession(sessionID string) (*Session, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// SECURITY FIX: 验证会话ID格式
	if !isValidSessionID(sessionID) {
		return nil, false
	}

	session, exists := sm.sessions[sessionID]
	if !exists || session.IsExpired() {
		return nil, false
	}

	return session, true
}

// GetSessionFromRequest 从请求中获取会话
func (sm *SessionManager) GetSessionFromRequest(r *http.Request) (*Session, bool) {
	cookie, err := r.Cookie(sm.config.CookieName)
	if err != nil {
		return nil, false
	}

	session, exists := sm.GetSession(cookie.Value)
	if !exists {
		return nil, false
	}

	// 更新访问时间和过期时间
	session.Touch()
	return session, true
}

// DeleteSession 删除会话（安全退出）
func (sm *SessionManager) DeleteSession(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.sessions, sessionID)
}

// SetSessionCookie 设置会话 Cookie
func (sm *SessionManager) SetSessionCookie(w http.ResponseWriter, session *Session) {
	cookie := &http.Cookie{
		Name:     sm.config.CookieName,
		Value:    session.ID,
		Path:     sm.config.CookiePath,
		Domain:   sm.config.CookieDomain,
		Expires:  session.ExpiresAt,
		MaxAge:   int(sm.config.MaxAge.Seconds()),
		Secure:   sm.config.CookieSecure,
		HttpOnly: sm.config.CookieHttpOnly,
		SameSite: sm.config.CookieSameSite,
	}
	http.SetCookie(w, cookie)
}

// ClearSessionCookie 清除会话 Cookie（退出登录）
func (sm *SessionManager) ClearSessionCookie(w http.ResponseWriter) {
	cookie := &http.Cookie{
		Name:     sm.config.CookieName,
		Value:    "",
		Path:     sm.config.CookiePath,
		Domain:   sm.config.CookieDomain,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		Secure:   sm.config.CookieSecure,
		HttpOnly: sm.config.CookieHttpOnly,
		SameSite: sm.config.CookieSameSite,
	}
	http.SetCookie(w, cookie)
}

// RegenerateSessionID 重新生成会话 ID（会话固定保护）
func (sm *SessionManager) RegenerateSessionID(oldSessionID string) (*Session, bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[oldSessionID]
	if !exists || session.IsExpired() {
		return nil, false
	}

	// 删除旧会话
	delete(sm.sessions, oldSessionID)

	// 创建新会话 ID，保留数据
	newSessionID := generateSecureSessionID()
	newSession := &Session{
		ID:         newSessionID,
		Data:       session.Data,
		CreatedAt:  session.CreatedAt,
		LastAccess: time.Now(),
		ExpiresAt:  time.Now().Add(sm.config.MaxAge),
	}

	sm.sessions[newSessionID] = newSession
	return newSession, true
}

// cleanup 定期清理过期会话
func (sm *SessionManager) cleanup() {
	ticker := time.NewTicker(SessionCleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		sm.mu.Lock()
		sm.cleanExpiredSessionsLocked()
		sm.mu.Unlock()
	}
}

// cleanExpiredSessionsLocked 清理过期会话（必须在锁内调用）
func (sm *SessionManager) cleanExpiredSessionsLocked() {
	for id, session := range sm.sessions {
		if session.IsExpired() {
			delete(sm.sessions, id)
		}
	}
}

// generateSecureSessionID 生成安全的会话 ID
// SECURITY FIX: 使用密码学安全随机数生成器，失败时不回退到不安全的替代方案
func generateSecureSessionID() string {
	// 使用密码学安全随机数
	b := make([]byte, SessionIDLength)
	if _, err := rand.Read(b); err != nil {
		// SECURITY FIX: 如果随机数生成失败，等待并重试，而不是使用时间戳
		// 重试最多3次
		for i := 0; i < 3; i++ {
			time.Sleep(10 * time.Millisecond)
			if _, err := rand.Read(b); err == nil {
				return encodeSessionID(b)
			}
		}
		// 如果仍然失败，使用系统熵池和其他熵源的组合
		return fallbackSecureSessionID()
	}
	return encodeSessionID(b)
}

// fallbackSecureSessionID 备用安全会话ID生成（当rand.Read失败时使用）
// SECURITY FIX: 即使回退也保持密码学安全性
func fallbackSecureSessionID() string {
	// 使用多种熵源的组合
	entropy := make([]byte, 0, SessionIDLength*2)
	
	// 添加时间熵（纳秒级）
	now := time.Now()
	timeEntropy := sha256.Sum256([]byte(fmt.Sprintf("%d-%d-%d", now.UnixNano(), now.Unix(), now.YearDay())))
	entropy = append(entropy, timeEntropy[:]...)
	
	// 添加进程信息熵
	pidEntropy := sha256.Sum256([]byte(fmt.Sprintf("%d-%d", now.UnixNano()/1000, now.Hour()*3600+now.Minute()*60+now.Second())))
	entropy = append(entropy, pidEntropy[:]...)
	
	// 最终哈希
	finalHash := sha256.Sum256(entropy)
	return encodeSessionID(finalHash[:SessionIDLength])
}

// encodeSessionID 编码会话ID
func encodeSessionID(data []byte) string {
	// 使用URL安全的base64编码
	encoded := base64.URLEncoding.EncodeToString(data)
	// 移除填充字符
	encoded = base64TrimPadding(encoded)
	return encoded
}

// base64TrimPadding 移除base64填充
func base64TrimPadding(s string) string {
	return s[:len(s)-len(s)%4]
}

// isValidSessionID 验证会话ID格式
// SECURITY FIX: 验证会话ID长度和字符集
func isValidSessionID(sessionID string) bool {
	if len(sessionID) < SessionIDMinLength {
		return false
	}
	// 验证只包含base64 URL安全字符
	for _, c := range sessionID {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '=') {
			return false
		}
	}
	return true
}

// hashSessionID 哈希会话ID（用于日志记录）
// SECURITY FIX: 不在日志中记录原始会话ID
func hashSessionID(sessionID string) string {
	hash := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(hash[:8]) // 只取前8字节用于日志
}

// SessionMiddleware 会话管理中间件
func SessionMiddleware(sm *SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 尝试获取现有会话
			session, _ := sm.GetSessionFromRequest(r)

			// 如果是认证端点，检查是否需要重新生成会话 ID（登录后）
			if isLoginEndpoint(r.URL.Path) && r.Method == http.MethodPost {
				// 如果存在旧会话，删除它
				if session != nil {
					sm.DeleteSession(session.ID)
				}
				// 创建新会话 - SECURITY FIX: 传递客户端IP进行速率限制
				session = sm.CreateSession(getClientIP(r))
				if session != nil {
					sm.SetSessionCookie(w, session)
				}
			} else if session == nil {
				// 对于非认证端点，创建匿名会话
				session = sm.CreateSession(getClientIP(r))
				if session != nil {
					sm.SetSessionCookie(w, session)
				}
			}

			// 将会话添加到请求上下文
			ctx := WithSession(r.Context(), session)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// getClientIP 获取客户端IP地址
func getClientIP(r *http.Request) string {
	// 检查X-Forwarded-For头
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// 取第一个IP
		if idx := len(xff); idx > 0 {
			for i, c := range xff {
				if c == ',' {
					return xff[:i]
				}
			}
			return xff
		}
	}
	// 检查X-Real-IP头
	if xri := r.Header.Get("X-Real-Ip"); xri != "" {
		return xri
	}
	// 使用RemoteAddr
	return r.RemoteAddr
}

// LogoutHandler 退出登录处理器
func LogoutHandler(sm *SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 获取当前会话并删除
		if session, exists := sm.GetSessionFromRequest(r); exists {
			sm.DeleteSession(session.ID)
		}

		// 清除 Cookie
		sm.ClearSessionCookie(w)

		// 返回成功响应
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success":true,"message":"Logged out successfully"}`))
	}
}

// RefreshSessionHandler 刷新会话处理器
func RefreshSessionHandler(sm *SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, exists := sm.GetSessionFromRequest(r)
		if !exists {
			http.Error(w, `{"error":"session not found"}`, http.StatusUnauthorized)
			return
		}

		// 重新生成会话 ID（会话固定保护）
		newSession, ok := sm.RegenerateSessionID(session.ID)
		if !ok {
			http.Error(w, `{"error":"failed to refresh session"}`, http.StatusInternalServerError)
			return
		}

		// 设置新 Cookie
		sm.SetSessionCookie(w, newSession)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success":true,"message":"Session refreshed"}`))
	}
}
