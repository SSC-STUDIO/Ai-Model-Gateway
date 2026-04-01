package server

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	// 会话过期时间：24小时
	SessionMaxAge = 24 * time.Hour
	// 会话ID长度
	SessionIDLength = 32
	// 清理间隔
	SessionCleanupInterval = 5 * time.Minute
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
		sessions: make(map[string]*Session),
		config:   config,
	}
	// 启动清理协程
	go sm.cleanup()
	return sm
}

// CreateSession 创建新会话
func (sm *SessionManager) CreateSession() *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sessionID := generateSessionID()
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
	newSessionID := generateSessionID()
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
		for id, session := range sm.sessions {
			if session.IsExpired() {
				delete(sm.sessions, id)
			}
		}
		sm.mu.Unlock()
	}
}

// generateSessionID 生成安全的会话 ID
func generateSessionID() string {
	b := make([]byte, SessionIDLength)
	if _, err := rand.Read(b); err != nil {
		// 如果随机数生成失败，使用时间戳和计数器组合
		return fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().Unix())
	}
	return base64.URLEncoding.EncodeToString(b)
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
				// 创建新会话
				session = sm.CreateSession()
				sm.SetSessionCookie(w, session)
			} else if session == nil {
				// 对于非认证端点，创建匿名会话
				session = sm.CreateSession()
				sm.SetSessionCookie(w, session)
			}

			// 将会话添加到请求上下文
			ctx := WithSession(r.Context(), session)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
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
