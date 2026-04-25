package core

import (
	"errors"
	"fmt"
	"testing"
)

func TestSentinelErrors_Messages(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{ErrNoProvider, "no available provider for model"},
		{ErrModelNotFound, "model not found"},
		{ErrUpstreamTimeout, "upstream request timed out"},
		{ErrRetryExhausted, "all retry attempts exhausted"},
		{ErrRequestTooLarge, "request body too large"},
		{ErrUnauthorized, "unauthorized"},
		{ErrForbidden, "forbidden"},
	}

	for _, tt := range tests {
		if got := tt.err.Error(); got != tt.want {
			t.Errorf("error message = %q, want %q", got, tt.want)
		}
	}
}

func TestSentinelErrors_ErrorsIs(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		target error
	}{
		{"ErrNoProvider matches itself", ErrNoProvider, ErrNoProvider},
		{"ErrModelNotFound matches itself", ErrModelNotFound, ErrModelNotFound},
		{"ErrUpstreamTimeout matches itself", ErrUpstreamTimeout, ErrUpstreamTimeout},
		{"ErrRetryExhausted matches itself", ErrRetryExhausted, ErrRetryExhausted},
		{"ErrRequestTooLarge matches itself", ErrRequestTooLarge, ErrRequestTooLarge},
		{"ErrUnauthorized matches itself", ErrUnauthorized, ErrUnauthorized},
		{"ErrForbidden matches itself", ErrForbidden, ErrForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !errors.Is(tt.err, tt.target) {
				t.Errorf("errors.Is(%v, %v) = false, want true", tt.err, tt.target)
			}
		})
	}
}

func TestSentinelErrors_Distinct(t *testing.T) {
	allErrors := []error{
		ErrNoProvider,
		ErrModelNotFound,
		ErrUpstreamTimeout,
		ErrRetryExhausted,
		ErrRequestTooLarge,
		ErrUnauthorized,
		ErrForbidden,
	}

	for i, err1 := range allErrors {
		for j, err2 := range allErrors {
			if i != j {
				if errors.Is(err1, err2) {
					t.Errorf("errors.Is(%v, %v) = true for distinct errors at indices %d and %d", err1, err2, i, j)
				}
			}
		}
	}
}

func TestSentinelErrors_Wrapping(t *testing.T) {
	wrapped := fmt.Errorf("operation failed: %w", ErrNoProvider)
	if !errors.Is(wrapped, ErrNoProvider) {
		t.Errorf("errors.Is(wrapped, ErrNoProvider) = false, want true")
	}

	unwrapped := errors.Unwrap(wrapped)
	if unwrapped != ErrNoProvider {
		t.Errorf("errors.Unwrap(wrapped) = %v, want ErrNoProvider", unwrapped)
	}
}
