package observability

import (
	"context"
	"testing"
)

func TestWithRequestID(t *testing.T) {
	ctx := context.Background()
	requestID := "test-request-123"

	ctx = WithRequestID(ctx, requestID)
	retrieved := RequestIDFromContext(ctx)

	if retrieved != requestID {
		t.Errorf("expected %s, got %s", requestID, retrieved)
	}
}

func TestRequestIDFromContextEmpty(t *testing.T) {
	ctx := context.Background()
	retrieved := RequestIDFromContext(ctx)

	if retrieved != "" {
		t.Errorf("expected empty string, got %s", retrieved)
	}
}

func TestRequestIDFromContextOverwrite(t *testing.T) {
	ctx := context.Background()

	ctx = WithRequestID(ctx, "first")
	ctx = WithRequestID(ctx, "second")

	retrieved := RequestIDFromContext(ctx)
	if retrieved != "second" {
		t.Errorf("expected second, got %s", retrieved)
	}
}

func TestConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"RequestIDHeader", RequestIDHeader, "X-Request-Id"},
		{"UpstreamHeader", UpstreamHeader, "X-AIGW-Upstream"},
		{"AttemptsHeader", AttemptsHeader, "X-AIGW-Attempts"},
		{"ModelHeader", ModelHeader, "X-AIGW-Model"},
		{"RequestedModelHeader", RequestedModelHeader, "X-AIGW-Requested-Model"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.constant)
			}
		})
	}
}
