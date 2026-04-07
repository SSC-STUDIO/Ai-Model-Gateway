package adminapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ai-model-gateway/internal/i18n"
	"ai-model-gateway/internal/infra/auth"
)

func TestLoginHandler_I18n_Chinese(t *testing.T) {
	authenticator := auth.New("test-secret", "signing-key")
	catalog := map[string]string{
		"errors.invalid_token":       "无效令牌",
		"errors.invalid_request_body": "无效请求体",
	}
	bundle := i18n.NewWithCatalog("zh", catalog)

	deps := Deps{
		Auth: authenticator,
		I18n: bundle,
	}

	// Test invalid token with Chinese i18n
	body, _ := json.Marshal(map[string]string{"token": "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler := loginHandler(deps)
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp["error"] != "无效令牌" {
		t.Errorf("expected Chinese error message '无效令牌', got %q", resp["error"])
	}
}

func TestLoginHandler_I18n_InvalidRequestBody(t *testing.T) {
	authenticator := auth.New("test-secret", "signing-key")
	catalog := map[string]string{
		"errors.invalid_request_body": "无效请求体",
	}
	bundle := i18n.NewWithCatalog("zh", catalog)

	deps := Deps{
		Auth: authenticator,
		I18n: bundle,
	}

	// Test invalid JSON with Chinese i18n
	req := httptest.NewRequest(http.MethodPost, "/api/admin/auth/login", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler := loginHandler(deps)
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp["error"] != "无效请求体" {
		t.Errorf("expected Chinese error message '无效请求体', got %q", resp["error"])
	}
}

func TestRequireAuth_I18n_Unauthorized(t *testing.T) {
	authenticator := auth.New("test-secret", "signing-key")
	catalog := map[string]string{
		"errors.unauthorized": "未授权",
	}
	bundle := i18n.NewWithCatalog("zh", catalog)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/overview", nil)
	rec := httptest.NewRecorder()

	// Create a simple handler that should not be called
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := requireAuth(authenticator, bundle)
	middleware(nextHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp["error"] != "未授权" {
		t.Errorf("expected Chinese error message '未授权', got %q", resp["error"])
	}
}

func TestConfigSaveHandler_I18n_NotImplemented(t *testing.T) {
	catalog := map[string]string{
		"errors.config_save_unavailable": "此运行时不支持配置保存",
	}
	bundle := i18n.NewWithCatalog("zh", catalog)

	deps := Deps{
		ConfigSave: nil, // Not implemented
		I18n:       bundle,
	}

	body, _ := json.Marshal(map[string]string{"key": "value"})
	req := httptest.NewRequest(http.MethodPut, "/api/admin/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler := configSaveHandler(deps)
	handler(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Errorf("expected 501, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp["error"] != "此运行时不支持配置保存" {
		t.Errorf("expected Chinese error message, got %q", resp["error"])
	}
}

func TestConfigExportHandler_I18n_NotImplemented(t *testing.T) {
	catalog := map[string]string{
		"errors.config_export_unavailable": "配置导出不可用",
	}
	bundle := i18n.NewWithCatalog("zh", catalog)

	deps := Deps{
		GetConfig:    nil,
		ConfigExport: nil,
		I18n:         bundle,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/config/export", nil)
	rec := httptest.NewRecorder()

	handler := configExportHandler(deps)
	handler(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Errorf("expected 501, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp["error"] != "配置导出不可用" {
		t.Errorf("expected Chinese error message, got %q", resp["error"])
	}
}

func TestI18nFallback_WhenBundleNil(t *testing.T) {
	authenticator := auth.New("test-secret", "signing-key")

	deps := Deps{
		Auth: authenticator,
		I18n: nil, // No i18n bundle
	}

	// Test that it falls back to English when bundle is nil
	body, _ := json.Marshal(map[string]string{"token": "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler := loginHandler(deps)
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Should fall back to English default
	if resp["error"] != "invalid token" {
		t.Errorf("expected English fallback 'invalid token', got %q", resp["error"])
	}
}
