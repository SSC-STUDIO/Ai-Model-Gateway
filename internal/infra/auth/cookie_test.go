package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var (
	testToken      = strings.Repeat("a", 34)
	testSigningKey = strings.Repeat("b", 34)
)

func newTestAuthWithTokens() *Authenticator {
	a := New(testToken, testSigningKey)
	a.SetTokens([]TokenEntry{
		{Name: "alice-admin", Token: strings.Repeat("c", 34), Role: RoleAdmin},
		{Name: "bob-viewer", Token: strings.Repeat("d", 34), Role: RoleViewer},
	})
	return a
}

func TestLogin_Success(t *testing.T) {
	a := New(testToken, testSigningKey)
	w := httptest.NewRecorder()

	if err := a.Login(w, testToken); err != nil {
		t.Fatalf("expected login success, got %v", err)
	}

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	if cookies[0].Name != CookieName {
		t.Errorf("expected cookie name %s, got %s", CookieName, cookies[0].Name)
	}
	if !cookies[0].HttpOnly {
		t.Error("expected HttpOnly cookie")
	}
	if cookies[0].SameSite != http.SameSiteStrictMode {
		t.Error("expected SameSite=Strict")
	}
}

func TestLogin_InvalidToken(t *testing.T) {
	a := New(testToken, testSigningKey)
	w := httptest.NewRecorder()

	if err := a.Login(w, "wrong-token"); err == nil {
		t.Error("expected error for invalid token")
	}
}

func TestAuthenticate_BearerToken(t *testing.T) {
	a := New(testToken, testSigningKey)
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+testToken)

	info, err := a.Authenticate(r)
	if err != nil {
		t.Errorf("expected bearer auth success, got %v", err)
	}
	if info.Role != RoleAdmin {
		t.Errorf("expected admin role for bootstrap token, got %q", info.Role)
	}
}

func TestAuthenticate_InvalidBearer(t *testing.T) {
	a := New(testToken, testSigningKey)
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer wrong-token")

	_, err := a.Authenticate(r)
	if err == nil {
		t.Error("expected error for invalid bearer token")
	}
}

func TestAuthenticate_SignedCookie(t *testing.T) {
	a := New(testToken, testSigningKey)

	// Login to get the cookie.
	w := httptest.NewRecorder()
	if err := a.Login(w, testToken); err != nil {
		t.Fatalf("login failed: %v", err)
	}
	cookie := w.Result().Cookies()[0]

	// Use the cookie in a new request.
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(cookie)

	info, err := a.Authenticate(r)
	if err != nil {
		t.Errorf("expected cookie auth success, got %v", err)
	}
	if info.Role != RoleAdmin {
		t.Errorf("expected admin role from bootstrap cookie, got %q", info.Role)
	}
}

func TestAuthenticate_TamperedCookie(t *testing.T) {
	a := New(testToken, testSigningKey)
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: "tampered.value"})

	_, err := a.Authenticate(r)
	if err == nil {
		t.Error("expected error for tampered cookie")
	}
}

func TestAuthenticate_NoCreds(t *testing.T) {
	a := New(testToken, testSigningKey)
	r := httptest.NewRequest("GET", "/", nil)

	_, err := a.Authenticate(r)
	if err == nil {
		t.Error("expected error when no credentials provided")
	}
}

func TestLogout(t *testing.T) {
	a := New(testToken, testSigningKey)
	w := httptest.NewRecorder()
	a.Logout(w)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	if cookies[0].MaxAge != -1 {
		t.Errorf("expected MaxAge=-1 for logout, got %d", cookies[0].MaxAge)
	}
}

func TestVerify_ExpiredCookie(t *testing.T) {
	a := New(testToken, testSigningKey)
	// Manually construct a signed cookie with a very old timestamp.
	signed := a.sign("admin:0") // Unix epoch = 1970-01-01
	_, err := a.verify(signed)
	if err == nil {
		t.Error("expected error for expired cookie")
	}
}

// ---------------------------------------------------------------------------
// Multi-token and role tests
// ---------------------------------------------------------------------------

func TestLogin_NamedAdminToken(t *testing.T) {
	a := newTestAuthWithTokens()
	w := httptest.NewRecorder()
	if err := a.Login(w, strings.Repeat("c", 34)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	role, err := a.verify(w.Result().Cookies()[0].Value)
	if err != nil {
		t.Fatalf("cookie verify: %v", err)
	}
	if role != RoleAdmin {
		t.Fatalf("expected admin role, got %q", role)
	}
}

func TestLogin_ViewerToken(t *testing.T) {
	a := newTestAuthWithTokens()
	w := httptest.NewRecorder()
	if err := a.Login(w, strings.Repeat("d", 34)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	role, err := a.verify(w.Result().Cookies()[0].Value)
	if err != nil {
		t.Fatalf("cookie verify: %v", err)
	}
	if role != RoleViewer {
		t.Fatalf("expected viewer role, got %q", role)
	}
}

func TestLoginInfo_Returns_Correct_Identity(t *testing.T) {
	a := newTestAuthWithTokens()

	info := a.LoginInfo(testToken)
	if info == nil || info.Role != RoleAdmin || info.Name != "bootstrap" {
		t.Fatalf("expected bootstrap admin, got %+v", info)
	}

	info = a.LoginInfo(strings.Repeat("d", 34))
	if info == nil || info.Role != RoleViewer || info.Name != "bob-viewer" {
		t.Fatalf("expected bob-viewer, got %+v", info)
	}

	info = a.LoginInfo("unknown")
	if info != nil {
		t.Fatalf("expected nil for unknown token, got %+v", info)
	}
}

func TestAuthenticate_BearerViewerToken(t *testing.T) {
	a := newTestAuthWithTokens()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+strings.Repeat("d", 34))
	info, err := a.Authenticate(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Role != RoleViewer {
		t.Fatalf("expected viewer, got %q", info.Role)
	}
	if info.Name != "bob-viewer" {
		t.Fatalf("expected bob-viewer, got %q", info.Name)
	}
}

func TestAuthenticate_CookiePreservesViewerRole(t *testing.T) {
	a := newTestAuthWithTokens()
	w := httptest.NewRecorder()
	a.Login(w, strings.Repeat("d", 34))
	cookie := w.Result().Cookies()[0]

	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(cookie)
	info, err := a.Authenticate(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Role != RoleViewer {
		t.Fatalf("expected viewer from cookie, got %q", info.Role)
	}
}

func TestVerify_LegacyTimestampPayload(t *testing.T) {
	a := New(testToken, testSigningKey)
	// Simulate legacy cookie: just a timestamp, no role prefix.
	payload := a.newPayload(RoleAdmin)
	idx := strings.LastIndex(payload, payloadFieldSep)
	legacyPayload := payload[idx+1:]
	signed := a.sign(legacyPayload)

	role, err := a.verify(signed)
	if err != nil {
		t.Fatalf("verify legacy cookie: %v", err)
	}
	if role != RoleAdmin {
		t.Fatalf("expected admin for legacy cookie, got %q", role)
	}
}
