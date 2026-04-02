// Package pathsecurity 提供路径安全验证工具，防止路径遍历攻击
package pathsecurity

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// IsSafePath 验证目标路径是否在基础路径范围内（防止路径遍历）
func IsSafePath(basePath, targetPath string) bool {
	// 获取绝对路径
	realBase, err := filepath.Abs(basePath)
	if err != nil {
		return false
	}
	realBase = filepath.Clean(realBase)

	realTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return false
	}
	realTarget = filepath.Clean(realTarget)

	// 确保基础路径存在
	if _, err := os.Stat(realBase); os.IsNotExist(err) {
		return false
	}

	// 目标路径必须在基础路径下
	return strings.HasPrefix(realTarget, realBase+string(filepath.Separator)) || realTarget == realBase
}

// NormalizeAndValidatePath 安全地拼接路径并验证
func NormalizeAndValidatePath(basePath string, components ...string) (string, error) {
	// 规范化基础路径
	realBase, err := filepath.Abs(basePath)
	if err != nil {
		return "", fmt.Errorf("invalid base path: %w", err)
	}
	realBase = filepath.Clean(realBase)

	// 拼接路径组件
	allParts := append([]string{realBase}, components...)
	target := filepath.Join(allParts...)

	// 解析符号链接后的最终路径
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		// 如果文件不存在，只使用 Clean
		realTarget = filepath.Clean(target)
	}

	// 验证路径安全
	if !strings.HasPrefix(realTarget, realBase+string(filepath.Separator)) && realTarget != realBase {
		return "", fmt.Errorf("path traversal detected: %s", target)
	}

	return realTarget, nil
}

// ValidateFileName 验证文件名是否安全
func ValidateFileName(filename string) error {
	if filename == "" {
		return fmt.Errorf("empty filename")
	}

	// 检查 null 字节
	if strings.Contains(filename, "\x00") {
		return fmt.Errorf("filename contains null bytes")
	}

	// 检查路径分隔符
	if strings.ContainsAny(filename, `/\`) {
		return fmt.Errorf("filename contains path separators")
	}

	// 检查路径遍历模式
	dangerousPatterns := []string{
		"..", "%2e%2e", "%252e%252e",
	}

	lowerName := strings.ToLower(filename)
	for _, pattern := range dangerousPatterns {
		if strings.Contains(lowerName, pattern) {
			return fmt.Errorf("filename contains dangerous pattern: %s", pattern)
		}
	}

	// 拒绝 Windows 保留名称
	reservedNames := []string{
		"CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9",
	}

	upperName := strings.ToUpper(filename)
	for _, reserved := range reservedNames {
		if upperName == reserved || strings.HasPrefix(upperName, reserved+".") {
			return fmt.Errorf("filename is a reserved name: %s", reserved)
		}
	}

	return nil
}

// ValidatePathComponent 验证路径组件是否安全
func ValidatePathComponent(component string) error {
	return ValidateFileName(component)
}

// SanitizePath 清理路径，移除危险字符
func SanitizePath(path string) string {
	// 移除 null 字节
	path = strings.ReplaceAll(path, "\x00", "")

	// 规范化路径
	path = filepath.Clean(path)

	// 如果路径以 .. 开头，视为不安全
	if strings.HasPrefix(path, "..") {
		return ""
	}

	return path
}

// JoinSafe 安全地拼接路径，验证结果
func JoinSafe(basePath string, components ...string) (string, error) {
	return NormalizeAndValidatePath(basePath, components...)
}
