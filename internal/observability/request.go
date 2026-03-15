package observability

import "context"

const (
	RequestIDHeader      = "X-Request-Id"
	UpstreamHeader       = "X-AIGW-Upstream"
	AttemptsHeader       = "X-AIGW-Attempts"
	ModelHeader          = "X-AIGW-Model"
	RequestedModelHeader = "X-AIGW-Requested-Model"
)

type requestIDContextKey struct{}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDContextKey{}, requestID)
}

func RequestIDFromContext(ctx context.Context) string {
	if requestID, ok := ctx.Value(requestIDContextKey{}).(string); ok {
		return requestID
	}
	return ""
}
