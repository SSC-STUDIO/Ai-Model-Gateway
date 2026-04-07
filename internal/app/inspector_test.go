package app

import (
	"context"
	"testing"

	"ai-model-gateway/internal/core"
)

func testRetryPolicy() core.RetryPolicyConfig {
	min := 500
	return core.RetryPolicyConfig{
		StatusCodes:     []int{408, 429},
		StatusCodeMin:   &min,
		MessageKeywords: []string{"rate limit", "quota exceeded"},
	}
}

func TestInspector_Inspect_PassThrough(t *testing.T) {
	ins := NewResponseInspector(core.RoutingConfig{Retry: testRetryPolicy()})
	req := &core.GatewayRequest{Path: "/v1/chat/completions"}
	resp := &core.GatewayResponse{StatusCode: 200, Body: []byte(`{"ok":true}`)}

	result, err := ins.Inspect(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Retryable {
		t.Error("expected non-retryable for 200 response")
	}
}

func TestInspector_Inspect_RetryOnStatusCode(t *testing.T) {
	ins := NewResponseInspector(core.RoutingConfig{Retry: testRetryPolicy()})
	req := &core.GatewayRequest{Path: "/v1/chat/completions"}

	for _, code := range []int{408, 429, 500, 502, 503} {
		resp := &core.GatewayResponse{StatusCode: code, Body: []byte(`{"error":"fail"}`)}
		result, _ := ins.Inspect(context.Background(), req, resp)
		if !result.Retryable {
			t.Errorf("expected retryable for status %d", code)
		}
	}
}

func TestInspector_Inspect_RetryOnKeyword(t *testing.T) {
	ins := NewResponseInspector(core.RoutingConfig{Retry: testRetryPolicy()})
	req := &core.GatewayRequest{Path: "/v1/chat/completions"}
	resp := &core.GatewayResponse{
		StatusCode: 400,
		Body:       []byte(`{"error":"Rate limit exceeded"}`),
	}

	result, _ := ins.Inspect(context.Background(), req, resp)
	if !result.Retryable {
		t.Error("expected retryable for rate limit keyword")
	}
}

func TestInspector_Inspect_NoRetryOnNormalError(t *testing.T) {
	ins := NewResponseInspector(core.RoutingConfig{Retry: testRetryPolicy()})
	req := &core.GatewayRequest{Path: "/v1/chat/completions"}
	resp := &core.GatewayResponse{
		StatusCode: 400,
		Body:       []byte(`{"error":"invalid model"}`),
	}

	result, _ := ins.Inspect(context.Background(), req, resp)
	if result.Retryable {
		t.Error("expected non-retryable for normal 400 error")
	}
}

func TestInspector_Inspect_InterceptFail(t *testing.T) {
	ins := NewResponseInspector(core.RoutingConfig{
		Retry: testRetryPolicy(),
		Intercepts: []core.InterceptRule{
			{
				Name:        "block-403",
				StatusCodes: []int{403},
				Action:      "fail",
			},
		},
	})
	req := &core.GatewayRequest{Path: "/v1/chat/completions"}
	resp := &core.GatewayResponse{StatusCode: 403, Body: []byte(`{"error":"forbidden"}`)}

	result, _ := ins.Inspect(context.Background(), req, resp)
	if result.Retryable {
		t.Error("expected non-retryable for intercept action=fail")
	}
}

func TestInspector_Inspect_InterceptRetry(t *testing.T) {
	ins := NewResponseInspector(core.RoutingConfig{
		Retry: testRetryPolicy(),
		Intercepts: []core.InterceptRule{
			{
				Name:        "retry-on-busy",
				StatusCodes: []int{503},
				Action:      "retry",
			},
		},
	})
	req := &core.GatewayRequest{Path: "/v1/chat/completions"}
	resp := &core.GatewayResponse{StatusCode: 503, Body: []byte(`{"error":"busy"}`)}

	result, _ := ins.Inspect(context.Background(), req, resp)
	if !result.Retryable {
		t.Error("expected retryable for intercept action=retry")
	}
}

func TestInspector_Inspect_DisabledIntercept(t *testing.T) {
	f := false
	ins := NewResponseInspector(core.RoutingConfig{
		Retry: testRetryPolicy(),
		Intercepts: []core.InterceptRule{
			{
				Name:        "disabled-rule",
				Enabled:     &f,
				StatusCodes: []int{403},
				Action:      "fail",
			},
		},
	})
	req := &core.GatewayRequest{Path: "/v1/chat/completions"}
	// 403 without keyword match → should NOT be retryable (status < 500, not in retry codes)
	resp := &core.GatewayResponse{StatusCode: 403, Body: []byte(`{"error":"forbidden"}`)}

	result, _ := ins.Inspect(context.Background(), req, resp)
	// The disabled intercept should be skipped; default retry policy: 403 < 500 and not in [408,429]
	if result.Retryable {
		t.Error("expected non-retryable when intercept is disabled")
	}
}

func TestInspector_Inspect_ErrorResponse(t *testing.T) {
	ins := NewResponseInspector(core.RoutingConfig{Retry: testRetryPolicy()})
	req := &core.GatewayRequest{Path: "/v1/chat/completions"}
	resp := &core.GatewayResponse{
		StatusCode: 502,
		Error:      core.ErrUpstreamTimeout,
	}

	result, _ := ins.Inspect(context.Background(), req, resp)
	if !result.Retryable {
		t.Error("expected retryable for error response")
	}
}
