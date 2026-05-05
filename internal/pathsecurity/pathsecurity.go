// Package pathsecurity provides path security validation utilities to prevent path traversal attacks.
package pathsecurity

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// IsSafePath checks whether the target path stays within the base path boundary (prevents traversal).
func IsSafePath(basePath, targetPath string) bool {
	// Resolve to absolute paths
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

	// Ensure the base path exists
	if _, err := os.Stat(realBase); os.IsNotExist(err) {
		return false
	}

	// Use filepath.Rel for robust relative-path computation.
	// If the target is outside the base, Rel returns a path starting with "..".
	rel, err := filepath.Rel(realBase, realTarget)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// NormalizeAndValidatePath joins path components and validates the result stays within the base path.
func NormalizeAndValidatePath(basePath string, components ...string) (string, error) {
	// Normalize the base path
	realBase, err := filepath.Abs(basePath)
	if err != nil {
		return "", fmt.Errorf("invalid base path: %w", err)
	}
	realBase = filepath.Clean(realBase)

	// Join path components
	allParts := append([]string{realBase}, components...)
	target := filepath.Join(allParts...)

	// Resolve symlinks to get the real target path
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		// If the file doesn't exist, fall back to Clean
		realTarget = filepath.Clean(target)
	}

	// Verify the resolved path stays within the base using filepath.Rel
	rel, err := filepath.Rel(realBase, realTarget)
	if err != nil {
		return "", fmt.Errorf("path traversal detected: %s", target)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path traversal detected: %s", target)
	}

	return realTarget, nil
}

// ValidateFileName checks whether a filename is safe (no traversal, null bytes, or reserved names).
func ValidateFileName(filename string) error {
	if filename == "" {
		return fmt.Errorf("empty filename")
	}

	// Reject null bytes
	if strings.Contains(filename, "\x00") {
		return fmt.Errorf("filename contains null bytes")
	}

	// Reject path separators
	if strings.ContainsAny(filename, `/\`) {
		return fmt.Errorf("filename contains path separators")
	}

	// Reject path traversal patterns
	dangerousPatterns := []string{
		"..", "%2e%2e", "%252e%252e",
	}

	lowerName := strings.ToLower(filename)
	for _, pattern := range dangerousPatterns {
		if strings.Contains(lowerName, pattern) {
			return fmt.Errorf("filename contains dangerous pattern: %s", pattern)
		}
	}

	// Reject Windows reserved names
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

// ValidatePathComponent validates that a single path component is safe.
func ValidatePathComponent(component string) error {
	return ValidateFileName(component)
}

// SanitizePath cleans a path by removing dangerous characters and normalizing it.
func SanitizePath(path string) string {
	// Remove null bytes
	path = strings.ReplaceAll(path, "\x00", "")

	// Normalize the path
	path = filepath.Clean(path)

	// Reject paths that start with ..
	if strings.HasPrefix(path, "..") {
		return ""
	}

	return path
}

// JoinSafe joins path components and validates the result is safe.
func JoinSafe(basePath string, components ...string) (string, error) {
	return NormalizeAndValidatePath(basePath, components...)
}
