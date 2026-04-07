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

	if err := a.Authenticate(r); err != nil {
		t.Errorf("expected bearer auth success, got %v", err)
	}
}

func TestAuthenticate_InvalidBearer(t *testing.T) {
	a := New(testToken, testSigningKey)
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer wrong-token")

	if err := a.Authenticate(r); err == nil {
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

	if err := a.Authenticate(r); err != nil {
		t.Errorf("expected cookie auth success, got %v", err)
	}
}

func TestAuthenticate_TamperedCookie(t *testing.T) {
	a := New(testToken, testSigningKey)
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: "tampered.value"})

	if err := a.Authenticate(r); err == nil {
		t.Error("expected error for tampered cookie")
	}
}

func TestAuthenticate_NoCreds(t *testing.T) {
	a := New(testToken, testSigningKey)
	r := httptest.NewRequest("GET", "/", nil)

	if err := a.Authenticate(r); err == nil {
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
	signed := a.sign("0") // Unix epoch = 1970-01-01
	if err := a.verify(signed); err == nil {
		t.Error("expected error for expired cookie")
	}
}
