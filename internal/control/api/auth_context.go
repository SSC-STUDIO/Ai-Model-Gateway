package api

import (
	"context"
	"net/http"
	"strings"
)

type adminRoleContextKey struct{}

// WithAdminRole annotates an authenticated admin request with its resolved role.
func WithAdminRole(r *http.Request, role string) *http.Request {
	if r == nil {
		return nil
	}
	role = strings.TrimSpace(role)
	if role == "" {
		return r
	}
	ctx := context.WithValue(r.Context(), adminRoleContextKey{}, role)
	return r.WithContext(ctx)
}

// AdminRoleFromRequest returns the authenticated admin role, if present.
func AdminRoleFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	role, _ := r.Context().Value(adminRoleContextKey{}).(string)
	return strings.TrimSpace(role)
}
