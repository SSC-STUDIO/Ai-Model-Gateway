package core

import "errors"

// Sentinel errors used across the v2 pipeline.
var (
	// ErrNoProvider is returned when no healthy provider can serve the model.
	ErrNoProvider = errors.New("no available provider for model")

	// ErrModelNotFound is returned when the requested model is not configured.
	ErrModelNotFound = errors.New("model not found")

	// ErrUpstreamTimeout is returned when the upstream request exceeds its deadline.
	ErrUpstreamTimeout = errors.New("upstream request timed out")

	// ErrRetryExhausted is returned when all retry attempts have been consumed.
	ErrRetryExhausted = errors.New("all retry attempts exhausted")

	// ErrRequestTooLarge is returned when the request body exceeds MaxBodyBytes.
	ErrRequestTooLarge = errors.New("request body too large")

	// ErrUnauthorized is returned for failed authentication.
	ErrUnauthorized = errors.New("unauthorized")

	// ErrForbidden is returned when a valid identity lacks permission.
	ErrForbidden = errors.New("forbidden")
)
