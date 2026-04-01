package server

import (
	"log"
	"regexp"
	"strings"
	"unicode"
)

var (
	// 控制字符正则（除了 \t 和 \n）
	controlCharsRe = regexp.MustCompile(`[\x00-\x08\x0b-\x0c\x0e-\x1f\x7f]`)
	
	// CRLF 字符
	crlfRe = regexp.MustCompile(`[\r\n]`)
	
	// 日志注入尝试检测
	logInjectionRe = regexp.MustCompile(`(?i)(?:password|secret|token|key|credential|api_key|apikey|auth_token|access_token|refresh_token|bearer\s+[a-z0-9])`)
)

// SanitizeLogValue 清理日志值，防止 CRLF 注入和敏感信息泄露
func SanitizeLogValue(value string, maxLength int) string {
	if maxLength <= 0 {
		maxLength = 1024
	}
	
	// 截断过长的值
	if len(value) > maxLength {
		value = value[:maxLength] + "[truncated]"
	}
	
	// 替换 CRLF 字符防止日志注入
	value = crlfRe.ReplaceAllString(value, " ")
	
	// 移除控制字符
	value = controlCharsRe.ReplaceAllString(value, "")
	
	// 检测并标记敏感信息
	if logInjectionRe.MatchString(value) {
		return "[REDACTED: potential sensitive data]"
	}
	
	return value
}

// SanitizeLogKey 清理日志键名
func SanitizeLogKey(key string) string {
	// 只允许字母、数字、下划线和连字符
	var result strings.Builder
	for _, r := range key {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			result.WriteRune(r)
		} else {
			result.WriteRune('_')
		}
	}
	return result.String()
}

// SafeLogf 安全地格式化日志消息
func SafeLogf(format string, args ...interface{}) string {
	// 将参数转换为字符串并清理
	safeArgs := make([]interface{}, len(args))
	for i, arg := range args {
		switch v := arg.(type) {
		case string:
			safeArgs[i] = SanitizeLogValue(v, 0)
		default:
			safeArgs[i] = arg
		}
	}
	return format // 返回格式化后的字符串供 log.Printf 使用
}

// SafeAccessLog 记录安全的访问日志
func SafeAccessLog(requestID, method, path, remoteAddr, userAgent string, status, bytes int, durationMs int64) {
	// 清理所有可能包含用户输入的字段
	safePath := SanitizeLogValue(path, 2048)
	safeUserAgent := SanitizeLogValue(userAgent, 512)
	safeRemoteAddr := SanitizeLogValue(remoteAddr, 256)
	safeRequestID := SanitizeLogValue(requestID, 64)
	safeMethod := SanitizeLogValue(method, 16)
	
	log.Printf(
		"request_id=%q method=%q path=%q status=%d bytes=%d duration_ms=%d remote_addr=%q user_agent=%q",
		safeRequestID,
		safeMethod,
		safePath,
		status,
		bytes,
		durationMs,
		safeRemoteAddr,
		safeUserAgent,
	)
}

// RedactSensitiveHeaders 从日志中移除敏感头信息
func RedactSensitiveHeaders(headers map[string][]string) map[string]string {
	sensitiveHeaders := []string{
		"authorization",
		"cookie",
		"x-api-key",
		"x-auth-token",
		"proxy-authorization",
		"set-cookie",
	}
	
	result := make(map[string]string)
	for key, values := range headers {
		lowerKey := strings.ToLower(key)
		isSensitive := false
		for _, sensitive := range sensitiveHeaders {
			if lowerKey == sensitive {
				isSensitive = true
				break
			}
		}
		
		if isSensitive {
			result[key] = "[REDACTED]"
		} else if len(values) > 0 {
			result[key] = SanitizeLogValue(values[0], 256)
		}
	}
	return result
}

// EscapeLogField 转义日志字段中的特殊字符
func EscapeLogField(value string) string {
	// 替换常见的日志特殊字符
	replacements := map[string]string{
		"\\": "\\\\",
		"\"": "\\\"",
		"\r": "\\r",
		"\n": "\\n",
		"\t": "\\t",
	}
	
	for old, new := range replacements {
		value = strings.ReplaceAll(value, old, new)
	}
	return value
}
