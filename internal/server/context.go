package server

import (
	"context"
)

// contextKey 是上下文中存储数据的键类型
type contextKey string

const (
	// sessionContextKey 会话在上下文中的键
	sessionContextKey contextKey = "aigw_session"
)

// WithSession 将会话添加到上下文
func WithSession(ctx context.Context, session *Session) context.Context {
	return context.WithValue(ctx, sessionContextKey, session)
}

// SessionFromContext 从上下文中获取会话
func SessionFromContext(ctx context.Context) (*Session, bool) {
	session, ok := ctx.Value(sessionContextKey).(*Session)
	return session, ok
}
