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
)

// Authenticator validates admin requests via signed cookie or Bearer token.
type Authenticator struct {
	bootstrapToken   string
	cookieSigningKey []byte
}

// New creates an Authenticator with the given bootstrap token and signing key.
func New(bootstrapToken string, cookieSigningKey string) *Authenticator {
	return &Authenticator{
		bootstrapToken:   bootstrapToken,
		cookieSigningKey: []byte(cookieSigningKey),
	}
}

// Login validates the provided token against the bootstrap token.
// On success it sets a signed cookie on the response and returns nil.
func (a *Authenticator) Login(w http.ResponseWriter, token string) error {
	if !hmac.Equal([]byte(token), []byte(a.bootstrapToken)) {
		return errors.New("invalid token")
	}
	payload := a.newPayload()
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
// Returns nil on success.
func (a *Authenticator) Authenticate(r *http.Request) error {
	// Try Bearer token first (for CLI / programmatic access).
	if bearer := extractBearer(r); bearer != "" {
		if hmac.Equal([]byte(bearer), []byte(a.bootstrapToken)) {
			return nil
		}
		return errors.New("invalid bearer token")
	}

	// Try signed cookie.
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return errors.New("no authentication credential")
	}
	if err := a.verify(cookie.Value); err != nil {
		return fmt.Errorf("invalid cookie: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// newPayload returns "issued_at_unix" as the cookie payload.
func (a *Authenticator) newPayload() string {
	return strconv.FormatInt(time.Now().Unix(), 10)
}

// sign returns "payload.base64(hmac-sha256(payload))"
func (a *Authenticator) sign(payload string) string {
	mac := hmac.New(sha256.New, a.cookieSigningKey)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + signatureSep + sig
}

// verify checks that the cookie value has a valid HMAC signature and is
// not expired (older than cookieMaxAge).
func (a *Authenticator) verify(value string) error {
	parts := strings.SplitN(value, signatureSep, 2)
	if len(parts) != 2 {
		return errors.New("malformed cookie")
	}
	payload, sig := parts[0], parts[1]

	// Recompute HMAC.
	mac := hmac.New(sha256.New, a.cookieSigningKey)
	mac.Write([]byte(payload))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return errors.New("signature mismatch")
	}

	// Check expiry.
	issued, err := strconv.ParseInt(payload, 10, 64)
	if err != nil {
		return errors.New("invalid payload timestamp")
	}
	if time.Since(time.Unix(issued, 0)) > cookieMaxAge {
		return errors.New("cookie expired")
	}
	return nil
}

// extractBearer extracts the Bearer token from the Authorization header.
func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}
