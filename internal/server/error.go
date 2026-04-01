package server

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"runtime/debug"
)

// IsProduction 检查是否在生产环境
func IsProduction() bool {
	return os.Getenv("ENV") == "production" || os.Getenv("ENV") == "prod"
}

// APIError 标准化的 API 错误响应
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

// ErrorResponse 返回 JSON 格式的错误响应
func ErrorResponse(w http.ResponseWriter, statusCode int, errCode, message, requestID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := APIError{
		Code:    errCode,
		Message: message,
	}
	if requestID != "" {
		response.RequestID = requestID
	}

	_ = json.NewEncoder(w).Encode(response)
}

// HandleError 统一处理错误，根据环境决定是否返回详细信息
func HandleError(w http.ResponseWriter, r *http.Request, statusCode int, errCode string, err error) {
	requestID := r.Header.Get("X-Request-ID")

	// 生产环境不返回详细错误信息给客户端
	var message string
	if IsProduction() {
		switch statusCode {
		case http.StatusBadRequest:
			message = "Invalid request"
		case http.StatusUnauthorized:
			message = "Authentication required"
		case http.StatusForbidden:
			message = "Access denied"
		case http.StatusNotFound:
			message = "Resource not found"
		case http.StatusTooManyRequests:
			message = "Rate limit exceeded"
		case http.StatusInternalServerError:
			message = "Internal server error"
		case http.StatusServiceUnavailable:
			message = "Service temporarily unavailable"
		default:
			message = "An error occurred"
		}
	} else {
		// 开发环境返回详细错误信息
		if err != nil {
			message = err.Error()
		} else {
			message = "An error occurred"
		}
	}

	// 记录详细错误到日志（使用安全的日志记录）
	if err != nil {
		safeErr := SanitizeLogValue(err.Error(), 1024)
		log.Printf("[ERROR] request_id=%s code=%s status=%d error=%q", requestID, errCode, statusCode, safeErr)
	}

	ErrorResponse(w, statusCode, errCode, message, requestID)
}

// HandlePanic 处理 panic，防止信息泄露
func HandlePanic(w http.ResponseWriter, r *http.Request, recovery interface{}) {
	requestID := r.Header.Get("X-Request-ID")

	// 记录详细的 panic 信息到日志
	log.Printf("[PANIC] request_id=%s panic=%v stack=%s", requestID, recovery, string(debug.Stack()))

	// 返回通用的错误信息给客户端
	ErrorResponse(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error", requestID)
}

// RecoveryMiddleware panic 恢复中间件
func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovery := recover(); recovery != nil {
				HandlePanic(w, r, recovery)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// SanitizeError 清理错误信息，移除敏感信息
func SanitizeError(err error) string {
	if err == nil {
		return ""
	}

	message := err.Error()

	// 移除常见的敏感信息模式
	sensitivePatterns := []struct {
		prefix string
		suffix string
	}{
		{"password", ""},
		{"secret", ""},
		{"token", ""},
		{"key", ""},
		{"credential", ""},
		{"api_key", ""},
		{"apikey", ""},
		{"auth_token", ""},
		{"access_token", ""},
		{"refresh_token", ""},
		{"Bearer ", ""},
		{"Basic ", ""},
	}

	// 注意：这里只做简单的标记，实际使用时应该完全移除敏感信息
	for _, pattern := range sensitivePatterns {
		if containsCaseInsensitive(message, pattern.prefix) {
			return "[REDACTED: contains sensitive information]"
		}
	}

	return message
}

func containsCaseInsensitive(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || 
		len(s) > len(substr) && containsIgnoreCase(s, substr))
}

func containsIgnoreCase(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if stringsEqualIgnoreCase(s[i:i+len(substr)], substr) {
			return true
		}
	}
	return false
}

func stringsEqualIgnoreCase(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] && a[i] != b[i]+32 && a[i]+32 != b[i] {
			return false
		}
	}
	return true
}

// ValidationError 验证错误响应
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationResponse 返回验证错误响应
func ValidationResponse(w http.ResponseWriter, requestID string, errors []ValidationError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)

	response := struct {
		Code      string            `json:"code"`
		Message   string            `json:"message"`
		RequestID string            `json:"request_id,omitempty"`
		Errors    []ValidationError `json:"errors"`
	}{
		Code:      "VALIDATION_ERROR",
		Message:   "Validation failed",
		RequestID: requestID,
		Errors:    errors,
	}

	_ = json.NewEncoder(w).Encode(response)
}
