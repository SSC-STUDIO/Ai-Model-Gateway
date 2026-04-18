package cli

import (
	"testing"
)

func TestValidateProviderName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid name", "openai", false},
		{"valid with hyphen", "my-provider", false},
		{"valid with underscore", "my_provider", false},
		{"empty name", "", true},
		{"too long", "a12345678901234567890123456789012345678901234567890123456789012345", true},
		{"invalid chars", "provider@name", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProviderName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateProviderName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateRevisionID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid revision", "rev-001", false},
		{"empty revision", "", true},
		{"too long", string(make([]byte, 129)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRevisionID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRevisionID(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateModelName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid model", "gpt-4", false},
		{"empty model", "", true},
		{"too long", string(make([]byte, 257)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateModelName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateModelName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateConfigPath(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid path", "/etc/config.yaml", false},
		{"empty path", "", true},
		{"path traversal", "../config.yaml", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfigPath(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfigPath(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateOutputFormat(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"text format", "text", false},
		{"json format", "json", false},
		{"invalid format", "xml", true},
		{"empty format", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateOutputFormat(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateOutputFormat(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid http", "http://localhost:8080", false},
		{"valid https", "https://api.example.com", false},
		{"empty url", "", true},
		{"invalid scheme", "ftp://example.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateURL(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateURL(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateToken(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid token", "sk-12345678", false},
		{"empty token", "", true},
		{"too short", "short", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateToken(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateToken(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}
