package pathsecurity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsSafePath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pathsecurity-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name       string
		base       string
		target     string
		expectSafe bool
	}{
		{
			name:       "same directory",
			base:       tmpDir,
			target:     tmpDir,
			expectSafe: true,
		},
		{
			name:       "subdirectory",
			base:       tmpDir,
			target:     filepath.Join(tmpDir, "subdir"),
			expectSafe: true,
		},
		{
			name:       "nested file",
			base:       tmpDir,
			target:     filepath.Join(tmpDir, "subdir", "file.txt"),
			expectSafe: true,
		},
		{
			name:       "parent directory traversal",
			base:       tmpDir,
			target:     filepath.Dir(tmpDir),
			expectSafe: false,
		},
		{
			name:       "double dot traversal",
			base:       tmpDir,
			target:     filepath.Join(tmpDir, "..", ".."),
			expectSafe: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsSafePath(tt.base, tt.target)
			if result != tt.expectSafe {
				t.Errorf("IsSafePath(%q, %q) = %v, want %v", tt.base, tt.target, result, tt.expectSafe)
			}
		})
	}
}

func TestValidateFileName(t *testing.T) {
	tests := []struct {
		name      string
		filename  string
		expectErr bool
	}{
		{"valid name", "file.txt", false},
		{"valid with dash", "my-file.txt", false},
		{"valid with underscore", "my_file.txt", false},
		{"empty filename", "", true},
		{"path separator slash", "file/name.txt", true},
		{"path separator backslash", "file\\name.txt", true},
		{"null byte", "file\x00.txt", true},
		{"dot dot", "..", true},
		{"dot dot in name", "file..txt", true},
		{"CON reserved", "CON", true},
		{"PRN reserved", "PRN", true},
		{"NUL reserved", "NUL", true},
		{"COM1 reserved", "COM1", true},
		{"LPT1 reserved", "LPT1", true},
		{"CON with extension", "CON.txt", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFileName(tt.filename)
			if (err != nil) != tt.expectErr {
				t.Errorf("ValidateFileName(%q) error = %v, expectErr %v", tt.filename, err, tt.expectErr)
			}
		})
	}
}

func TestSanitizePath(t *testing.T) {
	clean := func(path string) string {
		return filepath.Clean(path)
	}
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"normal path", "path/to/file", clean("path/to/file")},
		{"null byte removal", "path\x00/to/file", clean("path/to/file")},
		{"dot dot prefix", "../etc/passwd", ""},
		{"clean path", "path/./to/../file", clean("path/file")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizePath(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizePath(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizeAndValidatePath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pathsecurity-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name       string
		base       string
		components []string
		expectErr  bool
	}{
		{
			name:       "valid subpath",
			base:       tmpDir,
			components: []string{"subdir", "file.txt"},
			expectErr:  false,
		},
		{
			name:       "traversal attempt",
			base:       tmpDir,
			components: []string{"..", "etc", "passwd"},
			expectErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeAndValidatePath(tt.base, tt.components...)
			if (err != nil) != tt.expectErr {
				t.Errorf("NormalizeAndValidatePath() error = %v, expectErr %v", err, tt.expectErr)
			}
		})
	}
}

func TestJoinSafe(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pathsecurity-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	result, err := JoinSafe(tmpDir, "subdir", "file.txt")
	if err != nil {
		t.Errorf("JoinSafe failed for valid path: %v", err)
	}
	if result == "" {
		t.Error("JoinSafe returned empty path")
	}
}

func TestValidatePathComponent(t *testing.T) {
	if err := ValidatePathComponent("valid-name"); err != nil {
		t.Errorf("ValidatePathComponent failed for valid name: %v", err)
	}

	if err := ValidatePathComponent(".."); err == nil {
		t.Error("ValidatePathComponent should fail for ..")
	}
}
