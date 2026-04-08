// Package auth implements stateless signed-cookie and Bearer-token
// authentication for the admin API.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// CookieName is the name of the admin authentication cookie.
	CookieName = "aigw"

	// cookieMaxAge is the default cookie lifetime.
	cookieMaxAge = 24 * time.Hour

	// signatureSep separates payload from signature in the cookie value.
	signatureSep = "."

	// payloadFieldSep separates role from timestamp in the cookie payload.
	payloadFieldSep = ":"

	// RoleAdmin grants full read-write access.
	RoleAdmin = "admin"

	// RoleViewer grants read-only access.
	RoleViewer = "viewer"
)

// AuthInfo holds the resolved identity from a successful authentication.
type AuthInfo struct {
	Name string
	Role string
}

// TokenEntry is a token the authenticator knows about.
type TokenEntry struct {
	Name  string
	Token string
	Role  string
}

// Authenticator validates admin requests via signed cookie or Bearer token.
type Authenticator struct {
	bootstrapToken   string
	cookieSigningKey []byte
	tokens           []TokenEntry
}

// New creates an Authenticator with the given bootstrap token and signing key.
func New(bootstrapToken string, cookieSigningKey string) *Authenticator {
	return &Authenticator{
		bootstrapToken:   bootstrapToken,
		cookieSigningKey: []byte(cookieSigningKey),
	}
}

// SetTokens configures the additional named tokens.
func (a *Authenticator) SetTokens(tokens []TokenEntry) {
	a.tokens = tokens
}

// Login validates the provided token against bootstrap_token and the tokens
// array. On success it sets a signed cookie (encoding the role) and returns
// the resolved AuthInfo.
func (a *Authenticator) Login(w http.ResponseWriter, token string) error {
	info := a.resolveToken(token)
	if info == nil {
		return errors.New("invalid token")
	}
	payload := a.newPayload(info.Role)
	signed := a.sign(payload)
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    signed,
		Path:     "/",
		MaxAge:   int(cookieMaxAge.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
	return nil
}

// LoginInfo validates a token and returns the AuthInfo without setting a
// cookie. Returns nil when the token is invalid.
func (a *Authenticator) LoginInfo(token string) *AuthInfo {
	return a.resolveToken(token)
}

// Logout clears the authentication cookie.
func (a *Authenticator) Logout(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

// Authenticate checks the request for a valid signed cookie or Bearer token.
// Returns the AuthInfo on success, or an error.
func (a *Authenticator) Authenticate(r *http.Request) (*AuthInfo, error) {
	// Try Bearer token first (for CLI / programmatic access).
	if bearer := extractBearer(r); bearer != "" {
		info := a.resolveToken(bearer)
		if info != nil {
			return info, nil
		}
		return nil, errors.New("invalid bearer token")
	}

	// Try signed cookie.
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return nil, errors.New("no authentication credential")
	}
	role, err := a.verify(cookie.Value)
	if err != nil {
		return nil, fmt.Errorf("invalid cookie: %w", err)
	}
	return &AuthInfo{Name: "cookie-user", Role: role}, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// resolveToken checks the token against bootstrap_token and the tokens array.
func (a *Authenticator) resolveToken(token string) *AuthInfo {
	if hmac.Equal([]byte(token), []byte(a.bootstrapToken)) {
		return &AuthInfo{Name: "bootstrap", Role: RoleAdmin}
	}
	for _, t := range a.tokens {
		if hmac.Equal([]byte(token), []byte(t.Token)) {
			role := t.Role
			if role != RoleAdmin && role != RoleViewer {
				role = RoleViewer
			}
			return &AuthInfo{Name: t.Name, Role: role}
		}
	}
	return nil
}

// newPayload returns "role:issued_at_unix" as the cookie payload.
func (a *Authenticator) newPayload(role string) string {
	return role + payloadFieldSep + strconv.FormatInt(time.Now().Unix(), 10)
}

// sign returns "payload.base64(hmac-sha256(payload))"
func (a *Authenticator) sign(payload string) string {
	mac := hmac.New(sha256.New, a.cookieSigningKey)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + signatureSep + sig
}

// verify checks that the cookie value has a valid HMAC signature and is
// not expired (older than cookieMaxAge). Returns the role encoded in the
// cookie payload.
func (a *Authenticator) verify(value string) (string, error) {
	parts := strings.SplitN(value, signatureSep, 2)
	if len(parts) != 2 {
		return "", errors.New("malformed cookie")
	}
	payload, sig := parts[0], parts[1]

	// Recompute HMAC.
	mac := hmac.New(sha256.New, a.cookieSigningKey)
	mac.Write([]byte(payload))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return "", errors.New("signature mismatch")
	}

	// Parse "role:timestamp" or legacy "timestamp" payload.
	role := RoleAdmin
	tsStr := payload
	if idx := strings.LastIndex(payload, payloadFieldSep); idx >= 0 {
		role = payload[:idx]
		tsStr = payload[idx+1:]
	}

	// Check expiry.
	issued, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return "", errors.New("invalid payload timestamp")
	}
	if time.Since(time.Unix(issued, 0)) > cookieMaxAge {
		return "", errors.New("cookie expired")
	}
	if role != RoleAdmin && role != RoleViewer {
		role = RoleAdmin
	}
	return role, nil
}

// extractBearer extracts the Bearer token from the Authorization header.
func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}
