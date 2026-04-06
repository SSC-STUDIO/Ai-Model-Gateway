package i18n

// Error message keys for backend API
const (
	// Core errors
	ErrNoProvider      = "errors.no_provider"
	ErrModelNotFound   = "errors.model_not_found"
	ErrUpstreamTimeout = "errors.upstream_timeout"
	ErrRetryExhausted  = "errors.retry_exhausted"
	ErrRequestTooLarge = "errors.request_too_large"
	ErrUnauthorized    = "errors.unauthorized"
	ErrForbidden       = "errors.forbidden"

	// Auth errors
	ErrInvalidToken       = "errors.invalid_token"
	ErrInvalidRequestBody = "errors.invalid_request_body"
	ErrNoAuthCredential   = "errors.no_auth_credential"
	ErrMalformedCookie    = "errors.malformed_cookie"
	ErrSignatureMismatch  = "errors.signature_mismatch"
	ErrCookieExpired      = "errors.cookie_expired"

	// Config errors
	ErrConfigExportUnavailable     = "errors.config_export_unavailable"
	ErrConfigSaveUnavailable       = "errors.config_save_unavailable"
	ErrInvalidConfigPayload        = "errors.invalid_config_payload"
	ErrConfigHistoryUnavailable    = "errors.config_history_unavailable"
	ErrConfigDiffUnavailable       = "errors.config_diff_unavailable"
	ErrConfigRollbackUnavailable   = "errors.config_rollback_unavailable"
	ErrInvalidRollbackPayload      = "errors.invalid_rollback_payload"
	ErrInvalidUpstreamProbePayload = "errors.invalid_upstream_probe_payload"
)
