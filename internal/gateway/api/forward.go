package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ai-model-gateway/internal/gateway/snapshot"
)

// forwardToUpstream forwards the request to the upstream provider.
func forwardToUpstream(ctx context.Context, runtimeState *RuntimeState, provider *snapshot.ProviderSnapshot, path string, body []byte, stream bool, origHeaders http.Header, isAnthropic bool) (statusCode int, respBody []byte, streamBody io.ReadCloser, streamContentType string, latency time.Duration, err error) {
	// Build upstream URL
	var upstreamURL string
	if isAnthropic && provider.AnthropicBaseURL != "" {
		// Use Anthropic-specific base URL for /v1/messages
		upstreamURL = strings.TrimRight(provider.AnthropicBaseURL, "/") + path
	} else {
		upstreamURL = strings.TrimRight(provider.BaseURL, "/") + path
	}

	// SSRF check
	if err := ssrfChecker.ValidateURL(upstreamURL); err != nil {
		return http.StatusBadGateway, nil, nil, "", 0, err
	}

	// Create upstream request
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}

	timeout := time.Duration(provider.ExecutionPolicy.TimeoutMs) * time.Millisecond
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	if runtimeState != nil {
		if err := runtimeState.WaitForUpstreamSlot(reqCtx, provider); err != nil {
			cancel()
			return http.StatusGatewayTimeout, nil, nil, "", 0, err
		}
	}

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, upstreamURL, bodyReader)
	if err != nil {
		cancel()
		return http.StatusInternalServerError, nil, nil, "", 0, err
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")

	// Try key rotation first, then fall back to static credentials
	var kr *KeyRotator
	if runtimeState != nil {
		kr = runtimeState.KeyRotatorForProvider(provider)
	} else {
		kr = NewKeyRotator(provider)
	}
	rotatingKey := kr.Next()
	if rotatingKey != "" {
		if provider.Credentials.Kind == "bearer" {
			httpReq.Header.Set("Authorization", "Bearer "+rotatingKey)
		} else {
			headerName := provider.Credentials.HeaderName
			if headerName == "" {
				headerName = "x-api-key"
			}
			httpReq.Header.Set(headerName, rotatingKey)
		}
	} else if provider.Credentials.Kind == "bearer" && provider.Credentials.Value != "" {
		httpReq.Header.Set("Authorization", "Bearer "+provider.Credentials.Value)
	} else if provider.Credentials.Kind == "api_key" && provider.Credentials.Value != "" {
		headerName := provider.Credentials.HeaderName
		if headerName == "" {
			headerName = "x-api-key"
		}
		httpReq.Header.Set(headerName, provider.Credentials.Value)
	}
	for k, v := range provider.Headers {
		httpReq.Header.Set(k, v)
	}

	// Add anthropic-version header for Anthropic API
	if isAnthropic {
		httpReq.Header.Set("anthropic-version", "2023-06-01")
	}

	// Forward select original headers
	if ua := origHeaders.Get("User-Agent"); ua != "" {
		httpReq.Header.Set("User-Agent", ua)
	}

	// Execute
	start := time.Now()
	resp, err := sharedHTTPClient.Do(httpReq)
	latency = time.Since(start)

	if err != nil {
		cancel()
		if reqCtx.Err() != nil || errors.Is(err, context.DeadlineExceeded) {
			return http.StatusGatewayTimeout, nil, nil, "", latency, err
		}
		return http.StatusBadGateway, nil, nil, "", latency, err
	}

	// Keep successful SSE bodies open so the caller can stream them through.
	if stream && resp.StatusCode < http.StatusBadRequest && isSSE(resp) {
		if rotatingKey != "" {
			kr.ReportSuccess(rotatingKey)
		}
		return resp.StatusCode, nil, cancelOnClose(resp.Body, cancel), resp.Header.Get("Content-Type"), latency, nil
	}

	defer cancel()
	defer resp.Body.Close()

	// Read response body with 10 MB size limit.
	const maxRespBytes = 10 << 20
	respBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, maxRespBytes))
	if readErr != nil {
		return resp.StatusCode, nil, nil, "", latency, readErr
	}
	// Detect silent truncation: if we hit the limit, the response is incomplete.
	if int64(len(respBytes)) >= maxRespBytes {
		return http.StatusBadGateway, nil, nil, "", latency, fmt.Errorf("upstream response exceeded %d MB size limit", maxRespBytes>>20)
	}

	if rotatingKey != "" {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			kr.ReportFailure(rotatingKey)
		} else if resp.StatusCode < http.StatusBadRequest {
			kr.ReportSuccess(rotatingKey)
		}
	}

	return resp.StatusCode, respBytes, nil, "", latency, nil
}
