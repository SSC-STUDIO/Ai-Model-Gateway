package api

import (
	"context"
	"net/http"
	"sync"
	"time"
)

type executionOptionsKey struct{}

// ExecutionOptions customizes one synthetic request execution.
type ExecutionOptions struct {
	RequestID         string
	PinnedProviderID  string
	DisableCache      bool
	DisableFallback   bool
	DisableRetries    bool
	DisableSticky     bool
	SyntheticKind     string
	BenchmarkRunID    string
	BenchmarkTargetID string
	BenchmarkCaseID   string
	Result            *ExecutionResult
}

// ExecutionResult captures the resolved request outcome for synthetic callers.
type ExecutionResult struct {
	mu                  sync.Mutex
	StatusCode          int
	ContentType         string
	Latency             time.Duration
	PromptTokens        int64
	CachedPromptTokens  int64
	CompletionTokens    int64
	ProviderID          string
	EffectiveModel      string
	RouteMode           string
	PricingTotalCostUSD float64
	Error               string
}

// Snapshot returns a copy of the captured execution result.
func (r *ExecutionResult) Snapshot() ExecutionResult {
	if r == nil {
		return ExecutionResult{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return ExecutionResult{
		StatusCode:          r.StatusCode,
		ContentType:         r.ContentType,
		Latency:             r.Latency,
		PromptTokens:        r.PromptTokens,
		CachedPromptTokens:  r.CachedPromptTokens,
		CompletionTokens:    r.CompletionTokens,
		ProviderID:          r.ProviderID,
		EffectiveModel:      r.EffectiveModel,
		RouteMode:           r.RouteMode,
		PricingTotalCostUSD: r.PricingTotalCostUSD,
		Error:               r.Error,
	}
}

// WithExecutionOptions attaches execution options to the request context.
func WithExecutionOptions(ctx context.Context, opts *ExecutionOptions) context.Context {
	if opts == nil {
		return ctx
	}
	return context.WithValue(ctx, executionOptionsKey{}, opts)
}

func executionOptionsFromContext(ctx context.Context) *ExecutionOptions {
	if ctx == nil {
		return nil
	}
	opts, _ := ctx.Value(executionOptionsKey{}).(*ExecutionOptions)
	return opts
}

func captureExecutionResult(opts *ExecutionOptions, statusCode int, contentType string, latency time.Duration, promptTokens, cachedPromptTokens, completionTokens int64, providerID, effectiveModel, routeMode string, pricingTotalCostUSD float64, errMsg string) {
	if opts == nil || opts.Result == nil {
		return
	}
	opts.Result.mu.Lock()
	defer opts.Result.mu.Unlock()
	opts.Result.StatusCode = statusCode
	opts.Result.ContentType = contentType
	opts.Result.Latency = latency
	opts.Result.PromptTokens = promptTokens
	opts.Result.CachedPromptTokens = cachedPromptTokens
	opts.Result.CompletionTokens = completionTokens
	opts.Result.ProviderID = providerID
	opts.Result.EffectiveModel = effectiveModel
	opts.Result.RouteMode = routeMode
	opts.Result.PricingTotalCostUSD = pricingTotalCostUSD
	opts.Result.Error = errMsg
}

func cloneHeaders(headers http.Header) map[string][]string {
	if len(headers) == 0 {
		return nil
	}
	cloned := make(map[string][]string, len(headers))
	for key, values := range headers {
		copied := make([]string, len(values))
		copy(copied, values)
		cloned[key] = copied
	}
	return cloned
}

// CloneHeadersForRPC exposes a safe header clone for synthetic RPC callers.
func CloneHeadersForRPC(headers http.Header) map[string][]string {
	return cloneHeaders(headers)
}

func syntheticKindFromOptions(opts *ExecutionOptions) string {
	if opts == nil {
		return ""
	}
	return opts.SyntheticKind
}

func benchmarkRunIDFromOptions(opts *ExecutionOptions) string {
	if opts == nil {
		return ""
	}
	return opts.BenchmarkRunID
}

func benchmarkTargetIDFromOptions(opts *ExecutionOptions) string {
	if opts == nil {
		return ""
	}
	return opts.BenchmarkTargetID
}

func benchmarkCaseIDFromOptions(opts *ExecutionOptions) string {
	if opts == nil {
		return ""
	}
	return opts.BenchmarkCaseID
}
