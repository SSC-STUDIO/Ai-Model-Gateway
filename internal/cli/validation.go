package cli

import (
	"fmt"
	"regexp"
	"strings"
)

// ValidationError 验证错误
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidateProviderName 验证 provider 名称
func ValidateProviderName(name string) error {
	if name == "" {
		return &ValidationError{
			Field:   "name",
			Message: "provider name cannot be empty",
		}
	}

	// 只允许字母、数字、下划线、连字符
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, name)
	if !matched {
		return &ValidationError{
			Field:   "name",
			Message: "provider name can only contain letters, numbers, underscores, and hyphens",
		}
	}

	if len(name) > 64 {
		return &ValidationError{
			Field:   "name",
			Message: "provider name cannot exceed 64 characters",
		}
	}

	return nil
}

// ValidateRevisionID 验证版本 ID
func ValidateRevisionID(revisionID string) error {
	if revisionID == "" {
		return &ValidationError{
			Field:   "revision_id",
			Message: "revision ID cannot be empty",
		}
	}

	if len(revisionID) > 128 {
		return &ValidationError{
			Field:   "revision_id",
			Message: "revision ID cannot exceed 128 characters",
		}
	}

	return nil
}

// ValidateModelName 验证模型名称
func ValidateModelName(model string) error {
	if model == "" {
		return &ValidationError{
			Field:   "model",
			Message: "model name cannot be empty",
		}
	}

	if len(model) > 256 {
		return &ValidationError{
			Field:   "model",
			Message: "model name cannot exceed 256 characters",
		}
	}

	return nil
}

// ValidateConfigPath 验证配置文件路径
func ValidateConfigPath(path string) error {
	if path == "" {
		return &ValidationError{
			Field:   "config_path",
			Message: "config path cannot be empty",
		}
	}

	// 基本的路径安全检查
	if strings.Contains(path, "..") {
		return &ValidationError{
			Field:   "config_path",
			Message: "config path cannot contain '..'",
		}
	}

	return nil
}

// ValidateOutputFormat 验证输出格式
func ValidateOutputFormat(format string) error {
	switch OutputFormat(format) {
	case FormatText, FormatJSON:
		return nil
	default:
		return &ValidationError{
			Field:   "format",
			Message: fmt.Sprintf("invalid output format: %s (must be 'text' or 'json')", format),
		}
	}
}

// ValidatePositiveInt 验证正整数
func ValidatePositiveInt(value int, field string) error {
	if value <= 0 {
		return &ValidationError{
			Field:   field,
			Message: fmt.Sprintf("%s must be a positive integer", field),
		}
	}
	return nil
}

// ValidateURL 验证 URL
func ValidateURL(url string) error {
	if url == "" {
		return &ValidationError{
			Field:   "url",
			Message: "URL cannot be empty",
		}
	}

	// 基本 URL 格式检查
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return &ValidationError{
			Field:   "url",
			Message: "URL must start with http:// or https://",
		}
	}

	return nil
}

// ValidateToken 验证 token
func ValidateToken(token string) error {
	if token == "" {
		return &ValidationError{
			Field:   "token",
			Message: "token cannot be empty",
		}
	}

	if len(token) < 8 {
		return &ValidationError{
			Field:   "token",
			Message: "token must be at least 8 characters",
		}
	}

	return nil
}
