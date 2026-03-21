package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"ai-model-gateway/internal/config"
	"ai-model-gateway/internal/observability"
	"ai-model-gateway/internal/router"
	"ai-model-gateway/internal/telemetry"
)

type Handler struct {
	Manager *router.Manager
	Stats   *telemetry.Store
	Client  *http.Client
}

type forwardOptions struct {
	ModelRequired    bool
	SkipModelRewrite bool
}

type modelRequest struct {
	Model string `json:"model"`
}

type stickyRoutingRequest struct {
	PreviousResponseID string `json:"previous_response_id"`
	ResponseID         string `json:"response_id"`
}

type resolvedModel struct {
	Requested   string
	Effective   string
	Body        []byte
	ContentType string
}

type responseAssessment struct {
	ErrorBody bool
	Retryable bool
	Kind      string
	Message   string
}

type capturedResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

type requestDebugSummary struct {
	Path                string   `json:"path"`
	ContentType         string   `json:"content_type,omitempty"`
	UserAgent           string   `json:"user_agent,omitempty"`
	RequestedModel      string   `json:"requested_model,omitempty"`
	EffectiveModel      string   `json:"effective_model,omitempty"`
	BodyBytes           int      `json:"body_bytes"`
	JSONKeys            []string `json:"json_keys,omitempty"`
	HasPreviousResponse bool     `json:"has_previous_response_id,omitempty"`
	PreviousResponseID  string   `json:"previous_response_id,omitempty"`
	ToolCount           int      `json:"tool_count,omitempty"`
	InputItemCount      int      `json:"input_item_count,omitempty"`
	InputTextChars      int      `json:"input_text_chars,omitempty"`
	HasReasoning        bool     `json:"has_reasoning,omitempty"`
	HasTextConfig       bool     `json:"has_text,omitempty"`
	HasStore            bool     `json:"has_store,omitempty"`
	Stream              bool     `json:"stream,omitempty"`
}

const upstreamDisablePollInterval = 100 * time.Millisecond

func NewHandler(manager *router.Manager, stats *telemetry.Store) *Handler {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          256,
		MaxIdleConnsPerHost:   64,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	return &Handler{
		Manager: manager,
		Stats:   stats,
		Client:  &http.Client{Transport: transport},
	}
}

func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	h.forward(w, r, forwardOptions{ModelRequired: true})
}

func (h *Handler) Completions(w http.ResponseWriter, r *http.Request) {
	h.forward(w, r, forwardOptions{ModelRequired: true})
}

func (h *Handler) Embeddings(w http.ResponseWriter, r *http.Request) {
	h.forward(w, r, forwardOptions{ModelRequired: true})
}

func (h *Handler) Responses(w http.ResponseWriter, r *http.Request) {
	h.forward(w, r, forwardOptions{ModelRequired: true})
}

func (h *Handler) Messages(w http.ResponseWriter, r *http.Request) {
	h.forward(w, r, forwardOptions{ModelRequired: true})
}

func (h *Handler) MessageCountTokens(w http.ResponseWriter, r *http.Request) {
	requestID := observability.RequestIDFromContext(r.Context())
	startedAt := time.Now()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.Header().Set(observability.RequestIDHeader, requestID)
		writeProxyError(w, http.StatusBadRequest, fmt.Sprintf("read request body: %v", err), "invalid_request_error")
		return
	}
	_ = r.Body.Close()

	cfg := h.Manager.CurrentConfig()
	resolved, err := resolveModel(body, r.Header.Get("Content-Type"), r.UserAgent(), cfg, forwardOptions{ModelRequired: true})
	if err != nil {
		w.Header().Set(observability.RequestIDHeader, requestID)
		writeProxyError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}

	requestedModel := resolved.Requested
	model := resolved.Effective
	body = resolved.Body
	probeBody, err := buildAnthropicCountTokensProbeBody(body)
	if err != nil {
		w.Header().Set(observability.RequestIDHeader, requestID)
		writeProxyError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}

	maxAttempts := cfg.Router.MaxRetries + 1
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	excluded := make(map[string]struct{})
	sameUpstreamRetriesUsed := make(map[string]int)
	routeMode := requestRouteMode(requestedModel, model, false)
	var lastErr error
	var lastUpstream string
	var lastAttempts int

	for attempt := 0; attempt < maxAttempts; attempt++ {
		upstream, ok := h.Manager.PickSticky(model, "", excluded)
		if !ok {
			break
		}

		resp, latency, err := h.doAnthropicMessages(r, probeBody, upstream, requestID)
		if err != nil {
			h.Manager.ReportRequestFailure(upstream.Name, latency, 0, err, true, "transport")
			h.recordError(requestID, r.URL.Path, requestedModel, model, routeMode, upstream.Name, 0, attempt+1, err.Error())
			logProxyAttempt(requestID, model, upstream.Name, attempt+1, 0, latency, err)
			lastErr = err
			lastUpstream = upstream.Name
			lastAttempts = attempt + 1
			if shouldRetrySameUpstream(upstream, sameUpstreamRetriesUsed[upstream.Name]) {
				sameUpstreamRetriesUsed[upstream.Name]++
			} else {
				excluded[upstream.Name] = struct{}{}
			}
			if attempt+1 < maxAttempts {
				if sleepRetryBackoff(r.Context(), cfg.Router.RetryBackoffMs, cfg.Router.RetryBackoffMaxMs, attempt+1) != nil {
					break
				}
			}
			continue
		}

		assessment, captured, inspectErr := inspectResponse(resp, "/v1/messages", cfg.Proxy)
		if inspectErr == nil && captured != nil && !assessment.ErrorBody && captured.StatusCode < http.StatusBadRequest {
			countBody, inputTokens, buildErr := buildCountTokensResponseFromAnthropic(captured.Body)
			if buildErr == nil {
				h.Manager.ReportRequestSuccess(upstream.Name, latency, captured.StatusCode)
				logProxyAttempt(requestID, model, upstream.Name, attempt+1, captured.StatusCode, latency, nil)
				setProxyHeaders(w.Header(), requestID, upstream.Name, model, requestedModel, attempt+1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(countBody)
				h.recordRequest(startedAt, requestID, r.URL.Path, requestedModel, model, "anthropic_count_tokens_compat", upstream.Name, http.StatusOK, attempt+1, true, "", telemetry.Usage{PromptTokens: inputTokens, TotalTokens: inputTokens})
				return
			}
			inspectErr = buildErr
		}

		reason := ""
		kind := "body_error"
		retryable := true
		statusCode := 0
		if captured != nil {
			statusCode = captured.StatusCode
		}
		if inspectErr != nil {
			reason = inspectErr.Error()
			kind = "inspect"
		} else {
			reason = assessment.Message
			kind = assessment.Kind
			retryable = assessment.Retryable || shouldRetryResponse(statusCode, cfg.Proxy.Retry)
		}
		if reason == "" {
			reason = fmt.Sprintf("upstream %s did not return usable anthropic usage", upstream.Name)
		}

		h.Manager.ReportRequestFailure(upstream.Name, latency, statusCode, fmt.Errorf(reason), retryable, kind)
		h.recordError(requestID, r.URL.Path, requestedModel, model, routeMode, upstream.Name, statusCode, attempt+1, reason)
		logProxyAttempt(requestID, model, upstream.Name, attempt+1, statusCode, latency, fmt.Errorf(reason))
		lastErr = fmt.Errorf(reason)
		lastUpstream = upstream.Name
		lastAttempts = attempt + 1
		if shouldRetrySameUpstream(upstream, sameUpstreamRetriesUsed[upstream.Name]) {
			sameUpstreamRetriesUsed[upstream.Name]++
		} else {
			excluded[upstream.Name] = struct{}{}
		}
		if attempt+1 < maxAttempts {
			if sleepRetryBackoff(r.Context(), cfg.Router.RetryBackoffMs, cfg.Router.RetryBackoffMaxMs, attempt+1) != nil {
				break
			}
		}
	}

	setProxyHeaders(w.Header(), requestID, lastUpstream, model, requestedModel, lastAttempts)
	if lastErr != nil {
		h.recordRequest(startedAt, requestID, r.URL.Path, requestedModel, model, routeMode, lastUpstream, http.StatusServiceUnavailable, lastAttempts, false, lastErrString(lastErr), telemetry.Usage{})
		writeProxyError(w, http.StatusServiceUnavailable, lastErr.Error(), "service_unavailable")
		return
	}
	h.recordRequest(startedAt, requestID, r.URL.Path, requestedModel, model, routeMode, lastUpstream, http.StatusServiceUnavailable, lastAttempts, false, "no upstream available", telemetry.Usage{})
	writeProxyError(w, http.StatusServiceUnavailable, "no upstream available", "service_unavailable")
}

func (h *Handler) ResponsesCompact(w http.ResponseWriter, r *http.Request) {
	h.forward(w, r, forwardOptions{ModelRequired: true, SkipModelRewrite: true})
}

func (h *Handler) ResponseResource(w http.ResponseWriter, r *http.Request) {
	h.forward(w, r, forwardOptions{ModelRequired: false})
}

func (h *Handler) Moderations(w http.ResponseWriter, r *http.Request) {
	h.forward(w, r, forwardOptions{ModelRequired: false})
}

func (h *Handler) ImageGenerations(w http.ResponseWriter, r *http.Request) {
	h.forward(w, r, forwardOptions{ModelRequired: false})
}

func (h *Handler) AudioSpeech(w http.ResponseWriter, r *http.Request) {
	h.forward(w, r, forwardOptions{ModelRequired: true})
}

func (h *Handler) AudioTranscriptions(w http.ResponseWriter, r *http.Request) {
	h.forward(w, r, forwardOptions{ModelRequired: true})
}

func (h *Handler) AudioTranslations(w http.ResponseWriter, r *http.Request) {
	h.forward(w, r, forwardOptions{ModelRequired: true})
}

func (h *Handler) ImageEdits(w http.ResponseWriter, r *http.Request) {
	h.forward(w, r, forwardOptions{ModelRequired: false})
}

func (h *Handler) ImageVariations(w http.ResponseWriter, r *http.Request) {
	h.forward(w, r, forwardOptions{ModelRequired: false})
}

func (h *Handler) Files(w http.ResponseWriter, r *http.Request) {
	h.forward(w, r, forwardOptions{ModelRequired: false})
}

func (h *Handler) FileResource(w http.ResponseWriter, r *http.Request) {
	h.forward(w, r, forwardOptions{ModelRequired: false})
}

func (h *Handler) FileContent(w http.ResponseWriter, r *http.Request) {
	h.forward(w, r, forwardOptions{ModelRequired: false})
}

func (h *Handler) forward(w http.ResponseWriter, r *http.Request, opts forwardOptions) {
	requestID := observability.RequestIDFromContext(r.Context())
	startedAt := time.Now()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.Header().Set(observability.RequestIDHeader, requestID)
		writeProxyError(w, http.StatusBadRequest, fmt.Sprintf("read request body: %v", err), "invalid_request_error")
		return
	}
	_ = r.Body.Close()

	cfg := h.Manager.CurrentConfig()
	var originalBody []byte
	if cfg.Bridge.Enabled && len(cfg.Bridge.Rules) > 0 {
		originalBody = append([]byte(nil), body...)
	}
	originalContentType := r.Header.Get("Content-Type")
	resolved, err := resolveModel(body, r.Header.Get("Content-Type"), r.UserAgent(), cfg, opts)
	if err != nil {
		w.Header().Set(observability.RequestIDHeader, requestID)
		writeProxyError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	requestedModel := resolved.Requested
	model := resolved.Effective
	var debugSummary *requestDebugSummary
	getDebugSummary := func() requestDebugSummary {
		if debugSummary == nil {
			summary := buildRequestDebugSummary(r, body, requestedModel, model)
			debugSummary = &summary
		}
		return *debugSummary
	}
	stickyKey := extractStickyRoutingKey(r.URL.Path, body, r.Header.Get("Content-Type"))
	body = resolved.Body
	contentType := resolved.ContentType
	maxAttempts := cfg.Router.MaxRetries + 1
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	infiniteRetry := cfg.Proxy.Retry.InfiniteOnError

	excluded := make(map[string]struct{})
	sameUpstreamRetriesUsed := make(map[string]int)
	var lastErr error
	var lastCaptured *capturedResponse
	var lastUpstream string
	var lastAttempts int
	bridgeFallbackReady := requestedModel != "" && requestedModel != model
	bridgeFallbackActivated := false
	var bridgeFallback resolvedModel
	if bridgeFallbackReady {
		bridgeFallback, err = resolveModel(originalBody, originalContentType, r.UserAgent(), cfg, forwardOptions{
			ModelRequired:    opts.ModelRequired,
			SkipModelRewrite: true,
		})
		if err != nil {
			bridgeFallbackReady = false
		}
	}

	for attempt := 0; ; attempt++ {
		if !infiniteRetry && attempt >= maxAttempts {
			break
		}
		routeMode := requestRouteMode(requestedModel, model, bridgeFallbackActivated)
		upstream, ok := h.Manager.PickSticky(model, stickyKey, excluded)
		if !ok {
			if bridgeFallbackReady && !bridgeFallbackActivated && bridgeFallback.Effective != "" {
				model = bridgeFallback.Effective
				body = bridgeFallback.Body
				contentType = bridgeFallback.ContentType
				bridgeFallbackActivated = true
				continue
			}
			if infiniteRetry {
				excluded = make(map[string]struct{})
				sameUpstreamRetriesUsed = make(map[string]int)
				if sleepRetryBackoff(r.Context(), cfg.Router.RetryBackoffMs, cfg.Router.RetryBackoffMaxMs, attempt+1) != nil {
					break
				}
				continue
			}
			break
		}

		resp, latency, err := h.do(r, body, contentType, upstream, requestID)
		if err != nil {
			h.Manager.ReportRequestFailure(upstream.Name, latency, 0, err, true, "transport")
			h.recordError(requestID, r.URL.Path, requestedModel, model, routeMode, upstream.Name, 0, attempt+1, err.Error())
			logProxyAttempt(requestID, model, upstream.Name, attempt+1, 0, latency, err)
			logFailureDiagnostics(requestID, getDebugSummary(), upstream.Name, 0, nil)
			if shouldRetrySameUpstream(upstream, sameUpstreamRetriesUsed[upstream.Name]) {
				sameUpstreamRetriesUsed[upstream.Name]++
			} else {
				excluded[upstream.Name] = struct{}{}
			}
			lastErr = err
			lastUpstream = upstream.Name
			lastAttempts = attempt + 1
			if bridgeFallbackReady && !bridgeFallbackActivated && bridgeFallback.Effective != "" {
				model = bridgeFallback.Effective
				body = bridgeFallback.Body
				contentType = bridgeFallback.ContentType
				bridgeFallbackActivated = true
			}
			if sleepRetryBackoff(r.Context(), cfg.Router.RetryBackoffMs, cfg.Router.RetryBackoffMaxMs, attempt+1) != nil {
				break
			}
			continue
		}

		if shouldPassthroughStreaming(resp, r.URL.Path, infiniteRetry) {
			setProxyHeaders(w.Header(), requestID, upstream.Name, model, requestedModel, attempt+1)
			captured, streamErr := copyResponseAndCapture(w, resp)
			reason := ""
			success := streamErr == nil && captured != nil && hasCompletedEventStream(captured.Body, r.URL.Path)
			if !success {
				if streamErr != nil {
					reason = fmt.Sprintf("stream disconnected before completion: %v", streamErr)
				} else {
					reason = fmt.Sprintf("stream disconnected before completion: stream closed before %s", expectedStreamCompletionMarker(r.URL.Path))
				}
				h.Manager.ReportRequestFailure(upstream.Name, latency, resp.StatusCode, fmt.Errorf(reason), true, "stream_passthrough")
				logProxyAttempt(requestID, model, upstream.Name, attempt+1, resp.StatusCode, latency, fmt.Errorf(reason))
			} else {
				h.Manager.ReportRequestSuccess(upstream.Name, latency, resp.StatusCode)
				logProxyAttempt(requestID, model, upstream.Name, attempt+1, resp.StatusCode, latency, nil)
			}

			var usage telemetry.Usage
			if captured != nil {
				usage = extractUsage(captured.Body)
				rememberStickyRouting(h.Manager, r.URL.Path, stickyKey, upstream.Name, captured)
			}
			h.recordRequest(startedAt, requestID, r.URL.Path, requestedModel, model, routeMode, upstream.Name, resp.StatusCode, attempt+1, success, reason, usage)
			return
		}

		assessment, captured, inspectErr := inspectResponse(resp, r.URL.Path, cfg.Proxy)
		if inspectErr != nil {
			h.Manager.ReportRequestFailure(upstream.Name, latency, resp.StatusCode, inspectErr, false, "inspect")
			h.recordError(requestID, r.URL.Path, requestedModel, model, routeMode, upstream.Name, resp.StatusCode, attempt+1, inspectErr.Error())
			logProxyAttempt(requestID, model, upstream.Name, attempt+1, resp.StatusCode, latency, inspectErr)
			logFailureDiagnostics(requestID, getDebugSummary(), upstream.Name, resp.StatusCode, nil)
			lastErr = inspectErr
			lastUpstream = upstream.Name
			lastAttempts = attempt + 1
			setProxyHeaders(w.Header(), requestID, upstream.Name, model, requestedModel, attempt+1)
			h.recordRequest(startedAt, requestID, r.URL.Path, requestedModel, model, routeMode, upstream.Name, resp.StatusCode, attempt+1, false, inspectErr.Error(), telemetry.Usage{})
			copyResponse(w, resp)
			return
		}

		if shouldAttemptResponsesCompat(r.URL.Path, model, assessment, captured) {
			if h.tryResponsesCompat(w, r, startedAt, requestID, upstream, requestedModel, model, attempt+1, stickyKey, body) {
				return
			}
		}
		if shouldAttemptOpenAIChatCompatFromAnthropicMessages(r.URL.Path, model, assessment, captured) {
			if h.tryAnthropicMessagesOpenAICompat(w, r, startedAt, requestID, upstream, requestedModel, model, routeMode, attempt+1, stickyKey, body) {
				return
			}
		}
		if shouldAttemptAnthropicMessagesCompat(r.URL.Path, model, assessment, captured) {
			if strings.HasPrefix(r.URL.Path, "/v1/responses") {
				if h.tryResponsesAnthropicCompat(w, r, startedAt, requestID, upstream, requestedModel, model, attempt+1, stickyKey, body) {
					return
				}
			} else if r.URL.Path == "/v1/chat/completions" {
				if h.tryChatAnthropicCompat(w, r, startedAt, requestID, upstream, requestedModel, model, attempt+1, stickyKey, body) {
					return
				}
			}
		}

		retryableFailure := assessment.Retryable || shouldRetryResponse(resp.StatusCode, cfg.Proxy.Retry)
		bodyFailure := assessment.ErrorBody

		if retryableFailure || bodyFailure {
			reason := assessment.Message
			if reason == "" {
				reason = fmt.Sprintf("upstream %s returned status %d", upstream.Name, resp.StatusCode)
			}

			quotaExhausted := isQuotaExhaustedFailure(upstream, assessment, captured)
			if quotaExhausted {
				h.Manager.BlockUpstreamQuota(upstream.Name, reason)
			} else {
				h.Manager.ReportRequestFailure(upstream.Name, latency, resp.StatusCode, fmt.Errorf(reason), retryableFailure, assessment.Kind)
			}
			h.recordError(requestID, r.URL.Path, requestedModel, model, routeMode, upstream.Name, resp.StatusCode, attempt+1, reason)
			logProxyAttempt(requestID, model, upstream.Name, attempt+1, resp.StatusCode, latency, fmt.Errorf(reason))
			logFailureDiagnostics(requestID, getDebugSummary(), upstream.Name, resp.StatusCode, captured)

			lastErr = fmt.Errorf(reason)
			lastCaptured = captured
			lastUpstream = upstream.Name
			lastAttempts = attempt + 1

			if quotaExhausted || shouldRetryFailure(retryableFailure, bodyFailure, infiniteRetry, attempt, maxAttempts) {
				if quotaExhausted {
					excluded[upstream.Name] = struct{}{}
				} else if shouldRetrySameUpstream(upstream, sameUpstreamRetriesUsed[upstream.Name]) {
					sameUpstreamRetriesUsed[upstream.Name]++
				} else {
					excluded[upstream.Name] = struct{}{}
				}
				if bridgeFallbackReady && !bridgeFallbackActivated && bridgeFallback.Effective != "" {
					model = bridgeFallback.Effective
					body = bridgeFallback.Body
					contentType = bridgeFallback.ContentType
					bridgeFallbackActivated = true
				}
				if sleepRetryBackoff(r.Context(), cfg.Router.RetryBackoffMs, cfg.Router.RetryBackoffMaxMs, attempt+1) != nil {
					break
				}
				continue
			}

			if retryableFailure && !infiniteRetry && captured != nil && h.Manager.ShouldPassthroughFailure(upstream.Name) {
				setProxyHeaders(w.Header(), requestID, upstream.Name, model, requestedModel, attempt+1)
				h.recordRequest(startedAt, requestID, r.URL.Path, requestedModel, model, routeMode, upstream.Name, captured.StatusCode, attempt+1, false, reason, extractUsage(captured.Body))
				writeCapturedResponse(w, captured)
				return
			}

			if bodyFailure && !retryableFailure && captured != nil {
				setProxyHeaders(w.Header(), requestID, upstream.Name, model, requestedModel, attempt+1)
				h.recordRequest(startedAt, requestID, r.URL.Path, requestedModel, model, routeMode, upstream.Name, captured.StatusCode, attempt+1, false, reason, extractUsage(captured.Body))
				writeCapturedResponse(w, captured)
				return
			}
		}

		h.Manager.ReportRequestSuccess(upstream.Name, latency, resp.StatusCode)
		logProxyAttempt(requestID, model, upstream.Name, attempt+1, resp.StatusCode, latency, nil)
		setProxyHeaders(w.Header(), requestID, upstream.Name, model, requestedModel, attempt+1)
		var usage telemetry.Usage
		if captured != nil {
			usage = extractUsage(captured.Body)
		}
		rememberStickyRouting(h.Manager, r.URL.Path, stickyKey, upstream.Name, captured)
		h.recordRequest(startedAt, requestID, r.URL.Path, requestedModel, model, routeMode, upstream.Name, resp.StatusCode, attempt+1, true, "", usage)
		copyResponse(w, resp)
		return
	}

	if !infiniteRetry && lastCaptured != nil && lastUpstream != "" && h.Manager.ShouldPassthroughFailure(lastUpstream) {
		setProxyHeaders(w.Header(), requestID, lastUpstream, model, requestedModel, lastAttempts)
		h.recordRequest(startedAt, requestID, r.URL.Path, requestedModel, model, requestRouteMode(requestedModel, model, bridgeFallbackActivated), lastUpstream, lastCaptured.StatusCode, lastAttempts, false, lastErrString(lastErr), extractUsage(lastCaptured.Body))
		writeCapturedResponse(w, lastCaptured)
		return
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no available upstream for model %q", model)
	}
	setProxyHeaders(w.Header(), requestID, lastUpstream, model, requestedModel, lastAttempts)
	h.recordRequest(startedAt, requestID, r.URL.Path, requestedModel, model, requestRouteMode(requestedModel, model, bridgeFallbackActivated), lastUpstream, http.StatusServiceUnavailable, lastAttempts, false, lastErrString(lastErr), telemetry.Usage{})
	writeProxyError(w, http.StatusServiceUnavailable, lastErr.Error(), "service_unavailable")
}

func (h *Handler) do(src *http.Request, body []byte, contentType string, upstream config.Upstream, requestID string) (*http.Response, time.Duration, error) {
	timeout := time.Duration(upstream.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := h.upstreamRequestContext(src.Context(), upstream.Name)
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var timeoutCancel context.CancelFunc
		ctx, timeoutCancel = context.WithTimeout(ctx, timeout)
		cancel = combineCancel(cancel, timeoutCancel)
	}

	req, err := http.NewRequestWithContext(ctx, src.Method, joinURL(upstream.BaseURL, src.URL.Path), bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, 0, err
	}
	req.URL.RawQuery = src.URL.RawQuery
	req.Header = make(http.Header, len(src.Header))
	copyRequestHeaders(req.Header, src.Header)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	if requestID != "" {
		req.Header.Set(observability.RequestIDHeader, requestID)
	}
	if isAnthropicMessagesPath(src.URL.Path) {
		if req.Header.Get("anthropic-version") == "" {
			req.Header.Set("anthropic-version", "2023-06-01")
		}
		if upstream.APIKey != "" {
			req.Header.Set("x-api-key", upstream.APIKey)
		}
	} else if upstream.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+upstream.APIKey)
	}
	for key, value := range upstream.Headers {
		req.Header.Set(key, value)
	}

	start := time.Now()
	client := h.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		cancel()
		return nil, time.Since(start), err
	}
	if cancel != nil && resp != nil && resp.Body != nil {
		resp.Body = &cancelOnCloseReadCloser{ReadCloser: resp.Body, cancel: cancel}
	}
	return resp, time.Since(start), err
}

func (h *Handler) doAnthropicMessages(src *http.Request, body []byte, upstream config.Upstream, requestID string) (*http.Response, time.Duration, error) {
	timeout := time.Duration(upstream.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := h.upstreamRequestContext(src.Context(), upstream.Name)
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var timeoutCancel context.CancelFunc
		ctx, timeoutCancel = context.WithTimeout(ctx, timeout)
		cancel = combineCancel(cancel, timeoutCancel)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, joinURL(upstream.BaseURL, "/v1/messages"), bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, 0, err
	}
	req.Header = make(http.Header)
	req.Header.Set("Content-Type", "application/json")
	if version := strings.TrimSpace(src.Header.Get("anthropic-version")); version != "" {
		req.Header.Set("anthropic-version", version)
	} else {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	for _, value := range src.Header.Values("anthropic-beta") {
		if strings.TrimSpace(value) != "" {
			req.Header.Add("anthropic-beta", value)
		}
	}
	if requestID != "" {
		req.Header.Set(observability.RequestIDHeader, requestID)
	}
	if upstream.APIKey != "" {
		req.Header.Set("x-api-key", upstream.APIKey)
	}
	for key, value := range upstream.Headers {
		req.Header.Set(key, value)
	}

	start := time.Now()
	client := h.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		cancel()
		return nil, time.Since(start), err
	}
	if cancel != nil && resp != nil && resp.Body != nil {
		resp.Body = &cancelOnCloseReadCloser{ReadCloser: resp.Body, cancel: cancel}
	}
	return resp, time.Since(start), err
}

func (h *Handler) upstreamRequestContext(parent context.Context, upstreamName string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	if h == nil || h.Manager == nil || strings.TrimSpace(upstreamName) == "" {
		return ctx, cancel
	}

	go func() {
		ticker := time.NewTicker(upstreamDisablePollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !h.Manager.IsUpstreamEnabled(upstreamName) {
					cancel()
					return
				}
			}
		}
	}()

	return ctx, cancel
}

func combineCancel(first context.CancelFunc, second context.CancelFunc) context.CancelFunc {
	switch {
	case first == nil:
		return second
	case second == nil:
		return first
	default:
		return func() {
			first()
			second()
		}
	}
}

func resolveModel(body []byte, contentType string, userAgent string, cfg config.Config, opts forwardOptions) (resolvedModel, error) {
	resolved := resolvedModel{
		Body:        body,
		ContentType: contentType,
	}

	if len(bytes.TrimSpace(body)) == 0 {
		if opts.ModelRequired {
			return resolved, fmt.Errorf("model is required")
		}
		return resolved, nil
	}

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil && contentType != "" {
		return resolved, fmt.Errorf("parse content type: %w", err)
	}

	switch mediaType {
	case "", "application/json":
		return resolveModelFromJSON(body, mediaType, userAgent, cfg, opts)
	case "multipart/form-data":
		return resolveModelFromMultipart(body, contentType, params["boundary"], userAgent, cfg, opts)
	default:
		if opts.ModelRequired {
			return resolved, fmt.Errorf("model extraction not supported for content type %q", mediaType)
		}
		return resolved, nil
	}
}

func resolveModelFromJSON(body []byte, mediaType string, userAgent string, cfg config.Config, opts forwardOptions) (resolvedModel, error) {
	resolved := resolvedModel{
		Body:        body,
		ContentType: mediaType,
	}

	var request modelRequest
	if err := json.Unmarshal(body, &request); err != nil {
		if opts.ModelRequired {
			return resolved, fmt.Errorf("parse json body: %w", err)
		}
		return resolved, nil
	}

	model := strings.TrimSpace(request.Model)
	if opts.ModelRequired && model == "" {
		return resolved, fmt.Errorf("model is required")
	}

	resolved.Requested = model
	resolved.Effective = model
	if !opts.SkipModelRewrite {
		resolved.Effective = cfg.RewriteModelForRequest(model, userAgent)
	}
	if resolved.Effective == "" {
		resolved.Effective = model
	}

	if resolved.Requested == "" || resolved.Effective == "" || resolved.Effective == resolved.Requested {
		return resolved, nil
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return resolved, fmt.Errorf("parse json body for rewrite: %w", err)
	}
	payload["model"] = resolved.Effective
	rewrittenBody, err := json.Marshal(payload)
	if err != nil {
		return resolved, fmt.Errorf("rewrite json body: %w", err)
	}
	resolved.Body = rewrittenBody
	return resolved, nil
}

func resolveModelFromMultipart(body []byte, contentType string, boundary string, userAgent string, cfg config.Config, opts forwardOptions) (resolvedModel, error) {
	resolved := resolvedModel{
		Body:        body,
		ContentType: contentType,
	}
	if boundary == "" {
		if opts.ModelRequired {
			return resolved, fmt.Errorf("multipart boundary is required")
		}
		return resolved, nil
	}

	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	var modelFound bool

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return resolved, fmt.Errorf("read multipart body: %w", err)
		}

		if part.FormName() == "model" {
			value, err := io.ReadAll(part)
			_ = part.Close()
			if err != nil {
				return resolved, fmt.Errorf("read multipart model field: %w", err)
			}
			resolved.Requested = strings.TrimSpace(string(value))
			if opts.ModelRequired && resolved.Requested == "" {
				return resolved, fmt.Errorf("model is required")
			}
			resolved.Effective = resolved.Requested
			if !opts.SkipModelRewrite {
				resolved.Effective = cfg.RewriteModelForRequest(resolved.Requested, userAgent)
			}
			if resolved.Effective == "" {
				resolved.Effective = resolved.Requested
			}
			modelFound = true
			break
		}
		_, _ = io.Copy(io.Discard, part)
		_ = part.Close()
	}

	if modelFound && (resolved.Requested == "" || resolved.Effective == "" || resolved.Effective == resolved.Requested) {
		return resolved, nil
	}
	if !modelFound && opts.ModelRequired {
		return resolved, fmt.Errorf("model is required")
	}
	if !modelFound {
		return resolved, nil
	}

	reader = multipart.NewReader(bytes.NewReader(body), boundary)
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return resolved, fmt.Errorf("read multipart body: %w", err)
		}

		headers := cloneMIMEHeader(part.Header)
		if part.FormName() == "model" {
			_, _ = io.Copy(io.Discard, part)
			_ = part.Close()
			fieldWriter, err := writer.CreateFormField("model")
			if err != nil {
				return resolved, fmt.Errorf("rewrite multipart model field: %w", err)
			}
			if _, err := io.WriteString(fieldWriter, resolved.Effective); err != nil {
				return resolved, fmt.Errorf("write multipart model field: %w", err)
			}
			continue
		}

		dst, err := writer.CreatePart(headers)
		if err != nil {
			_ = part.Close()
			return resolved, fmt.Errorf("copy multipart part headers: %w", err)
		}
		if _, err := io.Copy(dst, part); err != nil {
			_ = part.Close()
			return resolved, fmt.Errorf("copy multipart part body: %w", err)
		}
		_ = part.Close()
	}

	if err := writer.Close(); err != nil {
		return resolved, fmt.Errorf("close multipart writer: %w", err)
	}
	resolved.Body = buffer.Bytes()
	resolved.ContentType = writer.FormDataContentType()
	return resolved, nil
}

func extractStickyRoutingKey(path string, body []byte, contentType string) string {
	if strings.HasPrefix(path, "/v1/responses/") && path != "/v1/responses/compact" {
		return strings.TrimSpace(strings.TrimPrefix(path, "/v1/responses/"))
	}
	if path != "/v1/responses" && path != "/v1/responses/compact" {
		return ""
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = contentType
	}
	if mediaType != "" && mediaType != "application/json" {
		return ""
	}

	var payload stickyRoutingRequest
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}

	for _, value := range []string{payload.PreviousResponseID, payload.ResponseID} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func rememberStickyRouting(manager *router.Manager, path string, stickyKey string, upstream string, captured *capturedResponse) {
	if manager == nil || upstream == "" {
		return
	}
	if stickyKey != "" {
		manager.RememberSticky(stickyKey, upstream)
	}
	if captured == nil || !strings.HasPrefix(path, "/v1/responses") {
		return
	}
	for _, responseID := range extractResponseIDs(captured.Body) {
		manager.RememberSticky(responseID, upstream)
	}
}

func extractResponseIDs(body []byte) []string {
	ids := make(map[string]struct{})
	collectResponseIDs(ids, body)

	result := make([]string, 0, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func collectResponseIDs(ids map[string]struct{}, body []byte) {
	collectResponseIDFromJSON(ids, body)

	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || strings.EqualFold(data, "[DONE]") {
			continue
		}
		collectResponseIDFromJSON(ids, []byte(data))
	}
}

func collectResponseIDFromJSON(ids map[string]struct{}, body []byte) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return
	}
	rememberResponseID(ids, payload["id"])
	if response, ok := payload["response"].(map[string]any); ok {
		rememberResponseID(ids, response["id"])
	}
}

func rememberResponseID(ids map[string]struct{}, value any) {
	id, ok := value.(string)
	if !ok {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	ids[id] = struct{}{}
}

func inspectResponse(resp *http.Response, path string, policy config.ProxyPolicyConfig) (responseAssessment, *capturedResponse, error) {
	assessment := responseAssessment{}
	contentType := resp.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil && contentType != "" {
		return assessment, nil, fmt.Errorf("parse response content type: %w", err)
	}

	if mediaType == "text/event-stream" {
		return inspectEventStreamResponse(resp, path, policy)
	}

	if shouldSkipInspection(mediaType, resp.StatusCode) {
		return assessment, nil, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return assessment, nil, fmt.Errorf("read upstream response body: %w", err)
	}
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))

	captured := &capturedResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		Body:       body,
	}

	assessment.Message, assessment.ErrorBody, assessment.Retryable = classifyResponseBody(body, mediaType, resp.StatusCode, policy.Retry)
	if assessment.ErrorBody {
		assessment.Kind = "body_error"
	}
	if shouldRetryResponse(resp.StatusCode, policy.Retry) && assessment.Kind == "" {
		assessment.Kind = "status"
	}
	assessment = applyInterceptRules(assessment, path, resp.StatusCode, body, policy)
	return assessment, captured, nil
}

func inspectEventStreamResponse(resp *http.Response, path string, policy config.ProxyPolicyConfig) (responseAssessment, *capturedResponse, error) {
	assessment := responseAssessment{}

	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))

	captured := &capturedResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		Body:       body,
	}

	if err != nil {
		assessment.ErrorBody = true
		assessment.Retryable = true
		assessment.Kind = "stream_error"
		assessment.Message = fmt.Sprintf("stream disconnected before completion: %v", err)
		return applyInterceptRules(assessment, path, resp.StatusCode, body, policy), captured, nil
	}

	if !hasCompletedEventStream(body, path) {
		assessment.ErrorBody = true
		assessment.Retryable = true
		assessment.Kind = "stream_incomplete"
		assessment.Message = fmt.Sprintf("stream disconnected before completion: stream closed before %s", expectedStreamCompletionMarker(path))
		return applyInterceptRules(assessment, path, resp.StatusCode, body, policy), captured, nil
	}

	return applyInterceptRules(assessment, path, resp.StatusCode, body, policy), captured, nil
}

func shouldPassthroughStreaming(resp *http.Response, path string, infiniteRetry bool) bool {
	if resp == nil || infiniteRetry || !expectsStructuredStreamCompletion(path) {
		return false
	}
	contentType := resp.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	}
	return resp.StatusCode < http.StatusBadRequest && mediaType == "text/event-stream"
}

func shouldSkipInspection(mediaType string, statusCode int) bool {
	if strings.HasPrefix(mediaType, "audio/") || strings.HasPrefix(mediaType, "image/") {
		return true
	}
	if mediaType == "application/octet-stream" {
		return true
	}
	if statusCode < http.StatusBadRequest && mediaType != "" && mediaType != "application/json" && !strings.HasPrefix(mediaType, "text/") {
		return true
	}
	return false
}

func hasResponseCompletedEvent(body []byte) bool {
	text := strings.ToLower(string(body))
	return strings.Contains(text, "response.completed")
}

func hasCompletedEventStream(body []byte, path string) bool {
	if expectsResponsesCompletedEvent(path) {
		return hasResponseCompletedEvent(body)
	}
	if expectsAnthropicMessagesCompletedEvent(path) {
		return hasAnthropicMessageStopEvent(body)
	}
	return hasDoneEvent(body)
}

func expectedStreamCompletionMarker(path string) string {
	if expectsResponsesCompletedEvent(path) {
		return "response.completed"
	}
	if expectsAnthropicMessagesCompletedEvent(path) {
		return "message_stop"
	}
	return "[DONE]"
}

func expectsResponsesCompletedEvent(path string) bool {
	return strings.HasPrefix(path, "/v1/responses")
}

func expectsAnthropicMessagesCompletedEvent(path string) bool {
	return path == "/v1/messages"
}

func expectsStructuredStreamCompletion(path string) bool {
	return expectsResponsesCompletedEvent(path) || expectsAnthropicMessagesCompletedEvent(path)
}

func hasAnthropicMessageStopEvent(body []byte) bool {
	text := strings.ToLower(string(body))
	return strings.Contains(text, "event: message_stop") || strings.Contains(text, `"type":"message_stop"`)
}

func hasDoneEvent(body []byte) bool {
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(strings.ToLower(line))
		if line == "data:[done]" || line == "data: [done]" {
			return true
		}
	}
	return false
}

func classifyResponseBody(body []byte, mediaType string, statusCode int, policy config.RetryPolicyConfig) (message string, errorBody bool, retryable bool) {
	text := strings.TrimSpace(string(body))
	if text == "" {
		if shouldRetryResponse(statusCode, policy) {
			return http.StatusText(statusCode), true, true
		}
		return "", false, false
	}

	if mediaType == "" || mediaType == "application/json" {
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err == nil {
			if usefulPayload(payload) {
				return "", false, false
			}
			if msg, ok := extractErrorMessage(payload, policy); ok {
				return msg, true, isRetryableMessage(msg, policy) || shouldRetryResponse(statusCode, policy)
			}
		}
	}

	if isRetryableMessage(text, policy) {
		return text, true, true
	}

	if statusCode >= http.StatusBadRequest {
		return text, true, shouldRetryResponse(statusCode, policy)
	}

	return "", false, false
}

func isQuotaExhaustedFailure(upstream config.Upstream, assessment responseAssessment, captured *capturedResponse) bool {
	if upstream.ProviderClassNormalized() != config.UpstreamClassQuotaLimited {
		return false
	}

	text := strings.ToLower(strings.TrimSpace(assessment.Message))
	if captured != nil && len(captured.Body) > 0 {
		if text != "" {
			text += "\n"
		}
		text += strings.ToLower(string(captured.Body))
	}

	for _, keyword := range quotaExhaustedKeywords() {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func quotaExhaustedKeywords() []string {
	return []string{
		"quota exceeded",
		"insufficient quota",
		"insufficient_quota",
		"exceeded your current quota",
		"billing hard limit",
		"credit balance is too low",
		"额度已用尽",
		"额度不足",
	}
}

func applyInterceptRules(assessment responseAssessment, path string, statusCode int, body []byte, policy config.ProxyPolicyConfig) responseAssessment {
	if len(policy.Intercepts) == 0 {
		return assessment
	}

	lowerMessage := strings.ToLower(strings.TrimSpace(assessment.Message))
	lowerBody := strings.ToLower(strings.TrimSpace(string(body)))

	for _, rule := range policy.Intercepts {
		if !rule.IsEnabled() {
			continue
		}
		if !matchesInterceptRule(rule, path, statusCode, lowerMessage, lowerBody) {
			continue
		}

		action := strings.ToLower(strings.TrimSpace(rule.Action))
		if action == "" {
			action = "fail"
		}

		assessment.ErrorBody = true
		assessment.Kind = "intercept"
		if action == "retry" {
			assessment.Retryable = true
		} else {
			assessment.Retryable = false
		}
		if strings.TrimSpace(rule.Name) != "" {
			assessment.Message = fmt.Sprintf("intercept %s", strings.TrimSpace(rule.Name))
		} else if assessment.Message == "" {
			assessment.Message = "intercept rule matched"
		}
		return assessment
	}

	return assessment
}

func matchesInterceptRule(rule config.ResponseInterceptRule, path string, statusCode int, message string, body string) bool {
	hasCondition := false

	if len(rule.Paths) > 0 {
		hasCondition = true
		matched := false
		for _, pattern := range rule.Paths {
			if strings.TrimSpace(pattern) == "" {
				continue
			}
			if config.MatchesPattern(pattern, path) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	if len(rule.StatusCodes) > 0 || rule.StatusCodeMin != nil {
		hasCondition = true
		matched := false
		for _, code := range rule.StatusCodes {
			if statusCode == code {
				matched = true
				break
			}
		}
		if !matched && rule.StatusCodeMin != nil && *rule.StatusCodeMin > 0 && statusCode >= *rule.StatusCodeMin {
			matched = true
		}
		if !matched {
			return false
		}
	}

	if len(rule.MessageKeywords) > 0 {
		hasCondition = true
		text := message
		if text == "" {
			text = body
		}
		matched := false
		for _, keyword := range rule.MessageKeywords {
			keyword = strings.ToLower(strings.TrimSpace(keyword))
			if keyword == "" {
				continue
			}
			if strings.Contains(text, keyword) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	return hasCondition
}

func extractUsage(body []byte) telemetry.Usage {
	if usage, ok := extractUsageFromJSON(body); ok {
		return usage
	}
	if usage, ok := extractUsageFromSSE(body); ok {
		return usage
	}
	return telemetry.Usage{}
}

func extractUsageFromJSON(body []byte) (telemetry.Usage, bool) {
	var payload struct {
		Usage struct {
			PromptTokens        int `json:"prompt_tokens"`
			PromptTokensDetails struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			CompletionTokens   int `json:"completion_tokens"`
			InputTokens        int `json:"input_tokens"`
			InputTokensDetails struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"input_tokens_details"`
			OutputTokens             int `json:"output_tokens"`
			TotalTokens              int `json:"total_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return telemetry.Usage{}, false
	}
	if payload.Usage.TotalTokens == 0 &&
		payload.Usage.PromptTokens == 0 &&
		payload.Usage.CompletionTokens == 0 &&
		payload.Usage.InputTokens == 0 &&
		payload.Usage.OutputTokens == 0 {
		return telemetry.Usage{}, false
	}

	promptTokens := payload.Usage.PromptTokens
	if promptTokens == 0 {
		promptTokens = payload.Usage.InputTokens
	}
	completionTokens := payload.Usage.CompletionTokens
	if completionTokens == 0 {
		completionTokens = payload.Usage.OutputTokens
	}
	cachedPromptTokens := payload.Usage.PromptTokensDetails.CachedTokens
	if cachedPromptTokens == 0 {
		cachedPromptTokens = payload.Usage.InputTokensDetails.CachedTokens
	}
	if cachedPromptTokens == 0 {
		cachedPromptTokens = payload.Usage.CacheReadInputTokens
	}
	totalTokens := payload.Usage.TotalTokens
	if totalTokens == 0 {
		totalTokens = promptTokens + completionTokens
	}
	return telemetry.Usage{
		PromptTokens:       promptTokens,
		CachedPromptTokens: cachedPromptTokens,
		CompletionTokens:   completionTokens,
		TotalTokens:        totalTokens,
	}, true
}

func extractUsageFromSSE(body []byte) (telemetry.Usage, bool) {
	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}

		var event struct {
			Type  string `json:"type"`
			Usage struct {
				PromptTokens        int `json:"prompt_tokens"`
				PromptTokensDetails struct {
					CachedTokens int `json:"cached_tokens"`
				} `json:"prompt_tokens_details"`
				CompletionTokens   int `json:"completion_tokens"`
				InputTokens        int `json:"input_tokens"`
				InputTokensDetails struct {
					CachedTokens int `json:"cached_tokens"`
				} `json:"input_tokens_details"`
				OutputTokens             int `json:"output_tokens"`
				TotalTokens              int `json:"total_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			} `json:"usage"`
			Response struct {
				Usage struct {
					PromptTokens        int `json:"prompt_tokens"`
					PromptTokensDetails struct {
						CachedTokens int `json:"cached_tokens"`
					} `json:"prompt_tokens_details"`
					CompletionTokens   int `json:"completion_tokens"`
					InputTokens        int `json:"input_tokens"`
					InputTokensDetails struct {
						CachedTokens int `json:"cached_tokens"`
					} `json:"input_tokens_details"`
					OutputTokens             int `json:"output_tokens"`
					TotalTokens              int `json:"total_tokens"`
					CacheReadInputTokens     int `json:"cache_read_input_tokens"`
					CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
				} `json:"usage"`
			} `json:"response"`
			Message struct {
				Usage struct {
					InputTokens          int `json:"input_tokens"`
					OutputTokens         int `json:"output_tokens"`
					CacheReadInputTokens int `json:"cache_read_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		usage := event.Response.Usage
		if usage.TotalTokens == 0 && usage.PromptTokens == 0 && usage.CompletionTokens == 0 && usage.InputTokens == 0 && usage.OutputTokens == 0 {
			usage = event.Usage
		}
		if usage.TotalTokens == 0 && usage.PromptTokens == 0 && usage.CompletionTokens == 0 && usage.InputTokens == 0 && usage.OutputTokens == 0 {
			// Anthropic message_start event puts usage under message.usage
			if event.Message.Usage.InputTokens > 0 || event.Message.Usage.OutputTokens > 0 {
				usage.InputTokens = event.Message.Usage.InputTokens
				usage.OutputTokens = event.Message.Usage.OutputTokens
				usage.CacheReadInputTokens = event.Message.Usage.CacheReadInputTokens
			} else {
				continue
			}
		}

		promptTokens := usage.PromptTokens
		if promptTokens == 0 {
			promptTokens = usage.InputTokens
		}
		completionTokens := usage.CompletionTokens
		if completionTokens == 0 {
			completionTokens = usage.OutputTokens
		}
		cachedPromptTokens := usage.PromptTokensDetails.CachedTokens
		if cachedPromptTokens == 0 {
			cachedPromptTokens = usage.InputTokensDetails.CachedTokens
		}
		if cachedPromptTokens == 0 {
			cachedPromptTokens = usage.CacheReadInputTokens
		}
		totalTokens := usage.TotalTokens
		if totalTokens == 0 {
			totalTokens = promptTokens + completionTokens
		}

		return telemetry.Usage{
			PromptTokens:       promptTokens,
			CachedPromptTokens: cachedPromptTokens,
			CompletionTokens:   completionTokens,
			TotalTokens:        totalTokens,
		}, true
	}

	return telemetry.Usage{}, false
}

func shouldAttemptResponsesCompat(path string, model string, assessment responseAssessment, captured *capturedResponse) bool {
	if path != "/v1/responses" {
		return false
	}
	if captured == nil {
		return false
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "claude-") {
		return false
	}
	if captured.StatusCode == http.StatusNotImplemented || captured.StatusCode == http.StatusNotFound || captured.StatusCode == http.StatusMethodNotAllowed {
		return true
	}
	message := strings.ToLower(strings.TrimSpace(assessment.Message + " " + string(captured.Body)))
	return strings.Contains(message, "not implemented") ||
		strings.Contains(message, "not supported") ||
		strings.Contains(message, "unsupported")
}

func shouldAttemptAnthropicMessagesCompat(path string, model string, assessment responseAssessment, captured *capturedResponse) bool {
	if captured == nil {
		return false
	}
	if path != "/v1/chat/completions" && path != "/v1/responses" {
		return false
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "claude-") {
		return false
	}
	if captured.StatusCode == http.StatusNotImplemented ||
		captured.StatusCode == http.StatusNotFound ||
		captured.StatusCode == http.StatusMethodNotAllowed ||
		captured.StatusCode == http.StatusServiceUnavailable {
		return true
	}
	message := strings.ToLower(strings.TrimSpace(assessment.Message + " " + string(captured.Body)))
	return strings.Contains(message, "anthropic") ||
		strings.Contains(message, "messages api") ||
		strings.Contains(message, "service temporarily unavailable") ||
		strings.Contains(message, "unsupported")
}

func shouldAttemptOpenAIChatCompatFromAnthropicMessages(path string, model string, assessment responseAssessment, captured *capturedResponse) bool {
	if captured == nil {
		return false
	}
	if path != "/v1/messages" {
		return false
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "claude-") {
		return false
	}
	if captured.StatusCode == http.StatusForbidden ||
		captured.StatusCode == http.StatusNotFound ||
		captured.StatusCode == http.StatusMethodNotAllowed ||
		captured.StatusCode == http.StatusNotImplemented ||
		captured.StatusCode == http.StatusServiceUnavailable {
		return true
	}
	message := strings.ToLower(strings.TrimSpace(assessment.Message + " " + string(captured.Body)))
	return strings.Contains(message, "/v1/messages dispatch") ||
		strings.Contains(message, "anthropic") ||
		strings.Contains(message, "messages api") ||
		strings.Contains(message, "unsupported") ||
		strings.Contains(message, "not allow")
}

func (h *Handler) tryResponsesCompat(
	w http.ResponseWriter,
	r *http.Request,
	startedAt time.Time,
	requestID string,
	upstream config.Upstream,
	requestedModel string,
	model string,
	attempt int,
	stickyKey string,
	body []byte,
) bool {
	chatPayload, streamRequested, err := buildChatCompletionsFromResponses(body, model)
	if err != nil {
		return false
	}
	if streamRequested {
		chatPayload["stream"] = false
	}

	compatBody, err := json.Marshal(chatPayload)
	if err != nil {
		return false
	}

	compatReq := r.Clone(r.Context())
	compatReq.URL.Path = "/v1/chat/completions"

	resp, latency, err := h.do(compatReq, compatBody, "application/json", upstream, requestID)
	if err != nil {
		return false
	}

	assessment, captured, inspectErr := inspectResponse(resp, "/v1/chat/completions", h.Manager.CurrentConfig().Proxy)
	if inspectErr != nil || captured == nil {
		return false
	}
	if assessment.ErrorBody || captured.StatusCode >= http.StatusBadRequest {
		return false
	}

	responsePayload, _, err := buildResponsesFromChat(captured.Body, model)
	if err != nil {
		return false
	}
	responseBody, err := json.Marshal(responsePayload)
	if err != nil {
		return false
	}

	h.Manager.ReportRequestSuccess(upstream.Name, latency, captured.StatusCode)
	logProxyAttempt(requestID, model, upstream.Name, attempt, captured.StatusCode, latency, nil)
	setProxyHeaders(w.Header(), requestID, upstream.Name, model, requestedModel, attempt)

	contentType := "application/json"
	if streamRequested {
		contentType = "text/event-stream"
		writeResponsesCompatStream(w, responsePayload)
	} else {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(responseBody)
	}

	usage := extractUsage(captured.Body)
	compatCaptured := &capturedResponse{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       responseBody,
	}
	rememberStickyRouting(h.Manager, r.URL.Path, stickyKey, upstream.Name, compatCaptured)
	h.recordRequest(startedAt, requestID, r.URL.Path, requestedModel, model, "responses_compat", upstream.Name, http.StatusOK, attempt, true, "", usage)

	if streamRequested {
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
	return true
}

func (h *Handler) tryChatAnthropicCompat(
	w http.ResponseWriter,
	r *http.Request,
	startedAt time.Time,
	requestID string,
	upstream config.Upstream,
	requestedModel string,
	model string,
	attempt int,
	stickyKey string,
	body []byte,
) bool {
	anthropicBody, streamRequested, err := buildAnthropicMessagesFromChat(body, model)
	if err != nil {
		return false
	}
	if streamRequested {
		anthropicBody, err = forceAnthropicNonStream(anthropicBody)
		if err != nil {
			return false
		}
	}

	resp, latency, err := h.doAnthropicMessages(r, anthropicBody, upstream, requestID)
	if err != nil {
		return false
	}

	assessment, captured, inspectErr := inspectResponse(resp, "/v1/messages", h.Manager.CurrentConfig().Proxy)
	if inspectErr != nil || captured == nil {
		return false
	}
	if assessment.ErrorBody || captured.StatusCode >= http.StatusBadRequest {
		return false
	}

	chatPayload, err := buildChatFromAnthropic(captured.Body, model)
	if err != nil {
		return false
	}
	chatBody, err := json.Marshal(chatPayload)
	if err != nil {
		return false
	}

	h.Manager.ReportRequestSuccess(upstream.Name, latency, captured.StatusCode)
	logProxyAttempt(requestID, model, upstream.Name, attempt, captured.StatusCode, latency, nil)
	setProxyHeaders(w.Header(), requestID, upstream.Name, model, requestedModel, attempt)

	contentType := "application/json"
	compatBody := chatBody
	if streamRequested {
		contentType = "text/event-stream"
		compatBody = marshalChatCompletionsCompatStream(chatPayload)
		writeChatCompletionsCompatStream(w, compatBody)
	} else {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(chatBody)
	}

	compatCaptured := &capturedResponse{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       compatBody,
	}
	rememberStickyRouting(h.Manager, r.URL.Path, stickyKey, upstream.Name, compatCaptured)
	h.recordRequest(startedAt, requestID, r.URL.Path, requestedModel, model, "anthropic_messages_compat", upstream.Name, http.StatusOK, attempt, true, "", extractUsage(chatBody))
	return true
}

func (h *Handler) tryResponsesAnthropicCompat(
	w http.ResponseWriter,
	r *http.Request,
	startedAt time.Time,
	requestID string,
	upstream config.Upstream,
	requestedModel string,
	model string,
	attempt int,
	stickyKey string,
	body []byte,
) bool {
	chatPayload, streamRequested, err := buildChatCompletionsFromResponses(body, model)
	if err != nil {
		return false
	}
	if streamRequested {
		chatPayload["stream"] = false
	}

	chatBody, err := json.Marshal(chatPayload)
	if err != nil {
		return false
	}
	anthropicBody, _, err := buildAnthropicMessagesFromChat(chatBody, model)
	if err != nil {
		return false
	}

	resp, latency, err := h.doAnthropicMessages(r, anthropicBody, upstream, requestID)
	if err != nil {
		return false
	}

	assessment, captured, inspectErr := inspectResponse(resp, "/v1/messages", h.Manager.CurrentConfig().Proxy)
	if inspectErr != nil || captured == nil {
		return false
	}
	if assessment.ErrorBody || captured.StatusCode >= http.StatusBadRequest {
		return false
	}

	chatCompat, err := buildChatFromAnthropic(captured.Body, model)
	if err != nil {
		return false
	}
	chatCompatBody, err := json.Marshal(chatCompat)
	if err != nil {
		return false
	}
	responsePayload, _, err := buildResponsesFromChat(chatCompatBody, model)
	if err != nil {
		return false
	}
	responseBody, err := json.Marshal(responsePayload)
	if err != nil {
		return false
	}

	h.Manager.ReportRequestSuccess(upstream.Name, latency, captured.StatusCode)
	logProxyAttempt(requestID, model, upstream.Name, attempt, captured.StatusCode, latency, nil)
	setProxyHeaders(w.Header(), requestID, upstream.Name, model, requestedModel, attempt)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(responseBody)

	compatCaptured := &capturedResponse{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       responseBody,
	}
	rememberStickyRouting(h.Manager, r.URL.Path, stickyKey, upstream.Name, compatCaptured)
	h.recordRequest(startedAt, requestID, r.URL.Path, requestedModel, model, "anthropic_messages_compat", upstream.Name, http.StatusOK, attempt, true, "", extractUsage(chatCompatBody))
	return true
}

func (h *Handler) tryAnthropicMessagesOpenAICompat(
	w http.ResponseWriter,
	r *http.Request,
	startedAt time.Time,
	requestID string,
	upstream config.Upstream,
	requestedModel string,
	model string,
	routeMode string,
	attempt int,
	stickyKey string,
	body []byte,
) bool {
	chatPayload, streamRequested, err := buildChatCompletionsFromAnthropic(body, model)
	if err != nil {
		return false
	}
	if streamRequested {
		chatPayload["stream"] = false
	}

	compatBody, err := json.Marshal(chatPayload)
	if err != nil {
		return false
	}

	compatReq := r.Clone(r.Context())
	compatReq.URL.Path = "/v1/chat/completions"
	compatReq.Header = compatReq.Header.Clone()
	compatReq.Header.Del("anthropic-version")
	compatReq.Header.Del("anthropic-beta")
	compatReq.Header.Del("x-api-key")

	resp, latency, err := h.do(compatReq, compatBody, "application/json", upstream, requestID)
	if err != nil {
		return false
	}

	assessment, captured, inspectErr := inspectResponse(resp, "/v1/chat/completions", h.Manager.CurrentConfig().Proxy)
	if inspectErr != nil || captured == nil {
		return false
	}
	if assessment.ErrorBody || captured.StatusCode >= http.StatusBadRequest {
		return false
	}

	responseModel := requestedModel
	if strings.TrimSpace(responseModel) == "" {
		responseModel = model
	}
	anthropicPayload, err := buildAnthropicMessageFromChat(captured.Body, responseModel)
	if err != nil {
		return false
	}
	anthropicBody, err := json.Marshal(anthropicPayload)
	if err != nil {
		return false
	}

	h.Manager.ReportRequestSuccess(upstream.Name, latency, captured.StatusCode)
	logProxyAttempt(requestID, model, upstream.Name, attempt, captured.StatusCode, latency, nil)
	setProxyHeaders(w.Header(), requestID, upstream.Name, model, requestedModel, attempt)

	contentType := "application/json"
	compatCapturedBody := anthropicBody
	if streamRequested {
		contentType = "text/event-stream"
		compatCapturedBody = marshalAnthropicMessageCompatStream(anthropicPayload)
		writeAnthropicMessageCompatStream(w, compatCapturedBody)
	} else {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(anthropicBody)
	}

	compatCaptured := &capturedResponse{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       compatCapturedBody,
	}
	rememberStickyRouting(h.Manager, r.URL.Path, stickyKey, upstream.Name, compatCaptured)
	h.recordRequest(startedAt, requestID, r.URL.Path, requestedModel, model, routeMode, upstream.Name, http.StatusOK, attempt, true, "", extractUsage(captured.Body))
	return true
}

func buildChatCompletionsFromResponses(body []byte, model string) (map[string]any, bool, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false, fmt.Errorf("parse responses payload: %w", err)
	}
	if model == "" {
		if m, ok := payload["model"].(string); ok {
			model = strings.TrimSpace(m)
		}
	}
	stream, _ := payload["stream"].(bool)

	var messages []map[string]any
	if instructions, ok := payload["instructions"].(string); ok && strings.TrimSpace(instructions) != "" {
		messages = append(messages, map[string]any{"role": "system", "content": instructions})
	}

	if input, ok := payload["input"]; ok {
		messages = append(messages, extractResponsesMessages(input)...)
	} else if rawMessages, ok := payload["messages"].([]any); ok {
		for _, item := range rawMessages {
			messages = append(messages, normalizeResponseInputItem(item)...)
		}
	}

	if len(messages) == 0 {
		return nil, stream, fmt.Errorf("responses input is empty")
	}

	chat := map[string]any{
		"model":    model,
		"messages": messages,
	}

	copyIfPresent(payload, chat, "tools", "tool_choice", "temperature", "top_p", "presence_penalty", "frequency_penalty", "stop", "max_tokens")
	if maxOutput, ok := payload["max_output_tokens"]; ok {
		chat["max_tokens"] = maxOutput
	}
	if stream {
		chat["stream"] = true
	}
	return chat, stream, nil
}

func buildChatCompletionsFromAnthropic(body []byte, model string) (map[string]any, bool, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false, fmt.Errorf("parse anthropic message payload: %w", err)
	}
	if model == "" {
		rawModel, _ := payload["model"].(string)
		model = strings.TrimSpace(rawModel)
	}
	if model == "" {
		return nil, false, fmt.Errorf("anthropic message model is empty")
	}

	stream, _ := payload["stream"].(bool)
	var messages []map[string]any

	systemText := extractTextFromContent(payload["system"])
	if systemText != "" {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": systemText,
		})
	}

	if rawMessages, ok := payload["messages"].([]any); ok {
		for _, item := range rawMessages {
			msg, extraSystem := normalizeAnthropicMessage(item)
			if extraSystem != "" {
				messages = append(messages, map[string]any{
					"role":    "system",
					"content": extraSystem,
				})
				continue
			}
			if msg != nil {
				messages = append(messages, msg)
			}
		}
	}
	if len(messages) == 0 {
		return nil, stream, fmt.Errorf("anthropic messages are empty")
	}

	chat := map[string]any{
		"model":    model,
		"messages": messages,
	}
	copyIfPresent(payload, chat, "max_tokens", "temperature", "top_p")
	if stopSequences, ok := payload["stop_sequences"]; ok {
		chat["stop"] = stopSequences
	}
	if stream {
		chat["stream"] = true
	}
	return chat, stream, nil
}

func buildAnthropicMessagesFromChat(body []byte, model string) ([]byte, bool, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false, fmt.Errorf("parse chat payload: %w", err)
	}
	if model == "" {
		if m, ok := payload["model"].(string); ok {
			model = strings.TrimSpace(m)
		}
	}
	if model == "" {
		return nil, false, fmt.Errorf("chat model is empty")
	}
	stream, _ := payload["stream"].(bool)

	var systemParts []string
	var messages []map[string]any
	if rawMessages, ok := payload["messages"].([]any); ok {
		for _, item := range rawMessages {
			msg, systemText := normalizeAnthropicMessage(item)
			if systemText != "" {
				systemParts = append(systemParts, systemText)
				continue
			}
			if msg != nil {
				messages = append(messages, msg)
			}
		}
	}
	if len(messages) == 0 {
		return nil, stream, fmt.Errorf("chat messages are empty")
	}

	anthropic := map[string]any{
		"model":      model,
		"messages":   messages,
		"max_tokens": 1024,
	}
	if len(systemParts) > 0 {
		anthropic["system"] = strings.TrimSpace(strings.Join(systemParts, "\n\n"))
	}
	if maxTokens, ok := payload["max_tokens"]; ok {
		anthropic["max_tokens"] = maxTokens
	}
	copyIfPresent(payload, anthropic, "temperature", "top_p", "stop_sequences")
	copyChatStopToAnthropic(payload, anthropic)
	if stream {
		anthropic["stream"] = true
	}
	data, err := json.Marshal(anthropic)
	if err != nil {
		return nil, stream, err
	}
	return data, stream, nil
}

func buildAnthropicCountTokensProbeBody(body []byte) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse anthropic count tokens payload: %w", err)
	}
	model, _ := payload["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, fmt.Errorf("anthropic count tokens model is empty")
	}
	payload["model"] = countTokensCompatModel(model)
	payload["max_tokens"] = 1
	payload["stream"] = false
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic count tokens probe: %w", err)
	}
	return data, nil
}

func countTokensCompatModel(model string) string {
	switch strings.TrimSpace(model) {
	case "claude-opus-4-6", "claude-opus-4-6-thinking":
		return "claude-sonnet-4-6"
	default:
		return model
	}
}

func buildCountTokensResponseFromAnthropic(body []byte) ([]byte, int, error) {
	var payload struct {
		Usage struct {
			InputTokens int `json:"input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, 0, fmt.Errorf("parse anthropic usage payload: %w", err)
	}
	if payload.Usage.InputTokens <= 0 {
		return nil, 0, fmt.Errorf("anthropic usage missing input_tokens")
	}
	response, err := json.Marshal(map[string]any{
		"input_tokens": payload.Usage.InputTokens,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("marshal anthropic count tokens response: %w", err)
	}
	return response, payload.Usage.InputTokens, nil
}

func copyChatStopToAnthropic(src map[string]any, dst map[string]any) {
	if _, ok := dst["stop_sequences"]; ok {
		return
	}
	stop, ok := src["stop"]
	if !ok {
		return
	}
	switch value := stop.(type) {
	case string:
		if strings.TrimSpace(value) != "" {
			dst["stop_sequences"] = []string{value}
		}
	case []any:
		if len(value) > 0 {
			dst["stop_sequences"] = value
		}
	}
}

func forceAnthropicNonStream(body []byte) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	payload["stream"] = false
	return json.Marshal(payload)
}

func normalizeAnthropicMessage(item any) (map[string]any, string) {
	msg, ok := item.(map[string]any)
	if !ok {
		return nil, ""
	}
	role, _ := msg["role"].(string)
	role = strings.TrimSpace(role)
	if role == "" {
		role = "user"
	}
	text := extractTextFromContent(msg["content"])
	if text == "" {
		return nil, ""
	}
	if role == "system" {
		return nil, text
	}
	if role != "assistant" {
		role = "user"
	}
	return map[string]any{
		"role":    role,
		"content": text,
	}, ""
}

func extractResponsesMessages(input any) []map[string]any {
	switch value := input.(type) {
	case string:
		text := strings.TrimSpace(value)
		if text == "" {
			return nil
		}
		return []map[string]any{{"role": "user", "content": text}}
	case []any:
		var messages []map[string]any
		for _, item := range value {
			messages = append(messages, normalizeResponseInputItem(item)...)
		}
		return messages
	case map[string]any:
		return normalizeResponseInputItem(value)
	default:
		return nil
	}
}

func normalizeResponseInputItem(item any) []map[string]any {
	switch value := item.(type) {
	case string:
		text := strings.TrimSpace(value)
		if text == "" {
			return nil
		}
		return []map[string]any{{"role": "user", "content": text}}
	case map[string]any:
		role, _ := value["role"].(string)
		role = strings.TrimSpace(role)
		if role == "" {
			role = "user"
		}
		content := extractTextFromContent(value["content"])
		if content == "" {
			if text, ok := value["text"].(string); ok {
				content = strings.TrimSpace(text)
			}
		}
		if content == "" {
			return nil
		}
		return []map[string]any{{"role": role, "content": content}}
	default:
		return nil
	}
}

func extractTextFromContent(content any) string {
	switch value := content.(type) {
	case string:
		return strings.TrimSpace(value)
	case []any:
		var builder strings.Builder
		for _, item := range value {
			switch part := item.(type) {
			case string:
				builder.WriteString(part)
			case map[string]any:
				if text, ok := part["text"].(string); ok {
					builder.WriteString(text)
					continue
				}
				if text, ok := part["content"].(string); ok {
					builder.WriteString(text)
					continue
				}
				if text, ok := part["input_text"].(string); ok {
					builder.WriteString(text)
					continue
				}
			}
		}
		return strings.TrimSpace(builder.String())
	case map[string]any:
		if text, ok := value["text"].(string); ok {
			return strings.TrimSpace(text)
		}
		if text, ok := value["content"].(string); ok {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func buildResponsesFromChat(chatBody []byte, model string) (map[string]any, string, error) {
	var payload struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Role    string      `json:"role"`
				Content interface{} `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
			Index        int    `json:"index"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(chatBody, &payload); err != nil {
		return nil, "", fmt.Errorf("parse chat completion: %w", err)
	}

	if model == "" {
		model = payload.Model
	}
	if payload.Created == 0 {
		payload.Created = time.Now().Unix()
	}

	var texts []string
	for _, choice := range payload.Choices {
		text := flattenChatContent(choice.Message.Content)
		if text != "" {
			texts = append(texts, text)
		}
	}
	outputText := strings.TrimSpace(strings.Join(texts, "\n\n"))
	if outputText == "" {
		outputText = ""
	}

	responseID := strings.TrimSpace(payload.ID)
	if responseID == "" {
		responseID = fmt.Sprintf("resp_%d", time.Now().UnixNano())
	}
	if !strings.HasPrefix(responseID, "resp_") {
		responseID = "resp_" + responseID
	}
	messageID := "msg_" + strings.TrimPrefix(responseID, "resp_")

	usage := map[string]any{
		"input_tokens":      payload.Usage.PromptTokens,
		"output_tokens":     payload.Usage.CompletionTokens,
		"total_tokens":      payload.Usage.TotalTokens,
		"prompt_tokens":     payload.Usage.PromptTokens,
		"completion_tokens": payload.Usage.CompletionTokens,
	}
	if payload.Usage.TotalTokens == 0 {
		usage["total_tokens"] = payload.Usage.PromptTokens + payload.Usage.CompletionTokens
	}

	response := map[string]any{
		"id":          responseID,
		"object":      "response",
		"created":     payload.Created,
		"model":       model,
		"status":      "completed",
		"output_text": outputText,
		"output": []any{
			map[string]any{
				"id":   messageID,
				"type": "message",
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type": "output_text",
						"text": outputText,
					},
				},
			},
		},
		"usage": usage,
	}
	return response, responseID, nil
}

func buildChatFromAnthropic(body []byte, model string) (map[string]any, error) {
	var payload struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Role    string `json:"role"`
		Model   string `json:"model"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse anthropic message: %w", err)
	}
	if model == "" {
		model = payload.Model
	}
	var builder strings.Builder
	for _, part := range payload.Content {
		if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
			if builder.Len() > 0 {
				builder.WriteString("\n\n")
			}
			builder.WriteString(strings.TrimSpace(part.Text))
		}
	}
	text := strings.TrimSpace(builder.String())
	totalTokens := payload.Usage.InputTokens + payload.Usage.OutputTokens
	return map[string]any{
		"id":      payload.ID,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []any{
			map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": text,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     payload.Usage.InputTokens,
			"completion_tokens": payload.Usage.OutputTokens,
			"total_tokens":      totalTokens,
		},
	}, nil
}

func buildAnthropicMessageFromChat(body []byte, model string) (map[string]any, error) {
	var payload struct {
		ID      string `json:"id"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Role    string      `json:"role"`
				Content interface{} `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse chat completion for anthropic compat: %w", err)
	}
	if model == "" {
		model = payload.Model
	}

	messageID := strings.TrimSpace(payload.ID)
	if messageID == "" {
		messageID = fmt.Sprintf("msg_%d", time.Now().UnixNano())
	} else if !strings.HasPrefix(messageID, "msg_") {
		messageID = "msg_" + strings.TrimPrefix(messageID, "chatcmpl-")
	}

	text := ""
	stopReason := "end_turn"
	if len(payload.Choices) > 0 {
		text = flattenChatContent(payload.Choices[0].Message.Content)
		stopReason = anthropicStopReasonFromChat(payload.Choices[0].FinishReason)
	}

	return map[string]any{
		"id":            messageID,
		"type":          "message",
		"role":          "assistant",
		"model":         model,
		"content":       []any{map[string]any{"type": "text", "text": text}},
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  payload.Usage.PromptTokens,
			"output_tokens": payload.Usage.CompletionTokens,
		},
	}, nil
}

func anthropicStopReasonFromChat(finishReason string) string {
	switch strings.TrimSpace(finishReason) {
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	case "content_filter":
		return "stop_sequence"
	default:
		return "end_turn"
	}
}

func flattenChatContent(content interface{}) string {
	switch value := content.(type) {
	case string:
		return strings.TrimSpace(value)
	case []any:
		var builder strings.Builder
		for _, item := range value {
			switch part := item.(type) {
			case string:
				builder.WriteString(part)
			case map[string]any:
				if text, ok := part["text"].(string); ok {
					builder.WriteString(text)
				}
			}
		}
		return strings.TrimSpace(builder.String())
	default:
		return ""
	}
}

func writeResponsesCompatStream(w http.ResponseWriter, response map[string]any) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	createdPayload := map[string]any{
		"type": "response.created",
		"response": map[string]any{
			"id":     response["id"],
			"object": "response",
			"status": "in_progress",
			"model":  response["model"],
		},
	}
	completedPayload := map[string]any{
		"type":     "response.completed",
		"response": response,
	}
	createdBytes, _ := json.Marshal(createdPayload)
	completedBytes, _ := json.Marshal(completedPayload)

	_, _ = w.Write([]byte("event: response.created\n"))
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(createdBytes)
	_, _ = w.Write([]byte("\n\n"))

	_, _ = w.Write([]byte("event: response.completed\n"))
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(completedBytes)
	_, _ = w.Write([]byte("\n\n"))
}

func writeChatCompletionsCompatStream(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func writeAnthropicMessageCompatStream(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func marshalChatCompletionsCompatStream(chat map[string]any) []byte {
	id, _ := chat["id"].(string)
	model, _ := chat["model"].(string)
	var created int64
	switch value := chat["created"].(type) {
	case int64:
		created = value
	case int:
		created = int64(value)
	case float64:
		created = int64(value)
	}
	if created == 0 {
		created = time.Now().Unix()
	}

	content := ""
	finishReason := "stop"
	if choices, ok := chat["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if reason, ok := choice["finish_reason"].(string); ok && strings.TrimSpace(reason) != "" {
				finishReason = reason
			}
			if message, ok := choice["message"].(map[string]any); ok {
				content = extractTextFromContent(message["content"])
			}
		}
	}

	firstChunk := map[string]any{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []map[string]any{
			{
				"index": 0,
				"delta": map[string]any{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": nil,
			},
		},
	}
	finalChunk := map[string]any{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []map[string]any{
			{
				"index":         0,
				"delta":         map[string]any{},
				"finish_reason": finishReason,
			},
		},
	}

	firstBytes, _ := json.Marshal(firstChunk)
	finalBytes, _ := json.Marshal(finalChunk)

	var builder strings.Builder
	builder.WriteString("data: ")
	builder.Write(firstBytes)
	builder.WriteString("\n\n")
	builder.WriteString("data: ")
	builder.Write(finalBytes)
	builder.WriteString("\n\n")
	builder.WriteString("data: [DONE]\n\n")
	return []byte(builder.String())
}

func marshalAnthropicMessageCompatStream(message map[string]any) []byte {
	usage, _ := message["usage"].(map[string]any)
	outputTokens := 0
	if value, ok := usage["output_tokens"]; ok {
		switch typed := value.(type) {
		case int:
			outputTokens = typed
		case int64:
			outputTokens = int(typed)
		case float64:
			outputTokens = int(typed)
		}
	}

	startPayload := map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            message["id"],
			"type":          "message",
			"role":          "assistant",
			"model":         message["model"],
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         usage,
		},
	}
	contentStartPayload := map[string]any{
		"type":          "content_block_start",
		"index":         0,
		"content_block": map[string]any{"type": "text", "text": ""},
	}
	contentDeltaPayload := map[string]any{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]any{"type": "text_delta", "text": extractTextFromContent(message["content"])},
	}
	contentStopPayload := map[string]any{
		"type":  "content_block_stop",
		"index": 0,
	}
	messageDeltaPayload := map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   message["stop_reason"],
			"stop_sequence": message["stop_sequence"],
		},
		"usage": map[string]any{
			"output_tokens": outputTokens,
		},
	}
	messageStopPayload := map[string]any{"type": "message_stop"}

	var builder strings.Builder
	for _, event := range []struct {
		name    string
		payload map[string]any
	}{
		{name: "message_start", payload: startPayload},
		{name: "content_block_start", payload: contentStartPayload},
		{name: "content_block_delta", payload: contentDeltaPayload},
		{name: "content_block_stop", payload: contentStopPayload},
		{name: "message_delta", payload: messageDeltaPayload},
		{name: "message_stop", payload: messageStopPayload},
	} {
		body, _ := json.Marshal(event.payload)
		builder.WriteString("event: ")
		builder.WriteString(event.name)
		builder.WriteString("\n")
		builder.WriteString("data: ")
		builder.Write(body)
		builder.WriteString("\n\n")
	}
	return []byte(builder.String())
}

func copyIfPresent(src map[string]any, dst map[string]any, keys ...string) {
	for _, key := range keys {
		if value, ok := src[key]; ok {
			dst[key] = value
		}
	}
}

func usefulPayload(payload map[string]any) bool {
	if hasNonEmptyArray(payload["choices"]) || hasNonEmptyArray(payload["output"]) || hasNonEmptyArray(payload["data"]) {
		return true
	}
	if hasNonEmptyString(payload["text"]) || hasNonEmptyString(payload["content"]) || hasNonEmptyString(payload["result"]) {
		return true
	}
	return false
}

func extractErrorMessage(payload map[string]any, policy config.RetryPolicyConfig) (string, bool) {
	if errorValue, ok := payload["error"]; ok {
		switch value := errorValue.(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				return value, true
			}
		case map[string]any:
			if msg, ok := value["message"].(string); ok && strings.TrimSpace(msg) != "" {
				return msg, true
			}
			if msg, ok := value["type"].(string); ok && strings.TrimSpace(msg) != "" {
				return msg, true
			}
		}
	}
	if msg, ok := payload["message"].(string); ok && isRetryableMessage(msg, policy) {
		return msg, true
	}
	return "", false
}

func hasNonEmptyArray(value any) bool {
	items, ok := value.([]any)
	return ok && len(items) > 0
}

func hasNonEmptyString(value any) bool {
	s, ok := value.(string)
	return ok && strings.TrimSpace(s) != ""
}

func isRetryableMessage(message string, policy config.RetryPolicyConfig) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}

	for _, keyword := range policy.MessageKeywords {
		keyword = strings.ToLower(strings.TrimSpace(keyword))
		if keyword == "" {
			continue
		}
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

func copyRequestHeaders(dst http.Header, src http.Header) {
	for key, values := range src {
		normalized := http.CanonicalHeaderKey(key)
		if normalized == "Authorization" || normalized == "Host" || normalized == "Content-Length" {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func copyResponse(w http.ResponseWriter, resp *http.Response) {
	defer resp.Body.Close()

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)

	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	_, _ = io.Copy(w, resp.Body)
}

func isAnthropicMessagesPath(path string) bool {
	return path == "/v1/messages" || path == "/v1/messages/count_tokens"
}

func copyResponseAndCapture(w http.ResponseWriter, resp *http.Response) (*capturedResponse, error) {
	defer resp.Body.Close()

	header := resp.Header.Clone()
	for key, values := range header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)

	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	var buffer bytes.Buffer
	writer := io.Writer(io.MultiWriter(w, &buffer))
	if flusher != nil {
		writer = flushWriter{writer: writer, flusher: flusher}
	}

	_, err := io.Copy(writer, resp.Body)
	return &capturedResponse{
		StatusCode: resp.StatusCode,
		Header:     header,
		Body:       buffer.Bytes(),
	}, err
}

type flushWriter struct {
	writer  io.Writer
	flusher http.Flusher
}

func (w flushWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	if err == nil && w.flusher != nil && n > 0 {
		w.flusher.Flush()
	}
	return n, err
}

type cancelOnCloseReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r *cancelOnCloseReadCloser) Close() error {
	err := r.ReadCloser.Close()
	if r.cancel != nil {
		r.cancel()
	}
	return err
}

func writeCapturedResponse(w http.ResponseWriter, resp *capturedResponse) {
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(resp.Body)
}

func logProxyAttempt(requestID string, model string, upstream string, attempt int, statusCode int, latency time.Duration, err error) {
	if err != nil {
		log.Printf(
			"request_id=%s component=proxy model=%q upstream=%q attempt=%d status=%d latency_ms=%d error=%q",
			requestID,
			model,
			upstream,
			attempt,
			statusCode,
			latency.Milliseconds(),
			err.Error(),
		)
		return
	}

	log.Printf(
		"request_id=%s component=proxy model=%q upstream=%q attempt=%d status=%d latency_ms=%d",
		requestID,
		model,
		upstream,
		attempt,
		statusCode,
		latency.Milliseconds(),
	)
}

func setProxyHeaders(header http.Header, requestID string, upstream string, model string, requestedModel string, attempts int) {
	if requestID != "" {
		header.Set(observability.RequestIDHeader, requestID)
	}
	if upstream != "" {
		header.Set(observability.UpstreamHeader, upstream)
	}
	if model != "" {
		header.Set(observability.ModelHeader, model)
	}
	if requestedModel != "" && requestedModel != model {
		header.Set(observability.RequestedModelHeader, requestedModel)
	}
	if attempts > 0 {
		header.Set(observability.AttemptsHeader, strconv.Itoa(attempts))
	}
}

func shouldRetrySameUpstream(upstream config.Upstream, retriesUsed int) bool {
	return upstream.SameUpstreamRetries > retriesUsed
}

func (h *Handler) recordRequest(startedAt time.Time, requestID string, path string, requestedModel string, model string, routeMode string, upstream string, statusCode int, attempts int, success bool, errorMessage string, usage telemetry.Usage) {
	if h.Stats == nil {
		return
	}
	h.Stats.RecordRequest(telemetry.RequestRecord{
		Timestamp:      time.Now(),
		RequestID:      requestID,
		Path:           path,
		RequestedModel: requestedModel,
		Model:          model,
		RouteMode:      routeMode,
		Upstream:       upstream,
		StatusCode:     statusCode,
		Attempts:       attempts,
		DurationMs:     time.Since(startedAt).Milliseconds(),
		Success:        success,
		Error:          errorMessage,
		Usage:          usage,
	})
}

func (h *Handler) recordError(requestID string, path string, requestedModel string, model string, routeMode string, upstream string, statusCode int, attempt int, message string) {
	if h.Stats == nil {
		return
	}
	h.Stats.RecordError(telemetry.ErrorRecord{
		Timestamp:      time.Now(),
		RequestID:      requestID,
		Path:           path,
		RequestedModel: requestedModel,
		Model:          model,
		RouteMode:      routeMode,
		Upstream:       upstream,
		StatusCode:     statusCode,
		Attempt:        attempt,
		Message:        message,
	})
}

func requestRouteMode(requestedModel string, effectiveModel string, bridgeFallbackActivated bool) string {
	if bridgeFallbackActivated {
		return "bridge_fallback"
	}
	if requestedModel != "" && effectiveModel != "" && requestedModel != effectiveModel {
		return "bridged"
	}
	return "direct"
}

func lastErrString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func shouldRetryResponse(statusCode int, policy config.RetryPolicyConfig) bool {
	for _, code := range policy.StatusCodes {
		if statusCode == code {
			return true
		}
	}
	if policy.StatusCodeMin != nil && *policy.StatusCodeMin > 0 && statusCode >= *policy.StatusCodeMin {
		return true
	}
	return false
}

func shouldRetryFailure(retryableFailure bool, bodyFailure bool, infiniteRetry bool, attempt int, maxAttempts int) bool {
	if infiniteRetry {
		return retryableFailure || bodyFailure
	}
	return retryableFailure && attempt < maxAttempts-1
}

func sleepRetryBackoff(ctx context.Context, baseMs int, maxMs int, attempt int) error {
	delay := retryBackoffDelay(baseMs, maxMs, attempt)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryBackoffDelay(baseMs int, maxMs int, attempt int) time.Duration {
	if baseMs <= 0 || attempt <= 0 {
		return 0
	}
	if maxMs > 0 && maxMs < baseMs {
		maxMs = baseMs
	}

	delayMs := int64(baseMs)
	for currentAttempt := 1; currentAttempt < attempt; currentAttempt++ {
		delayMs *= 2
		if maxMs > 0 && delayMs >= int64(maxMs) {
			delayMs = int64(maxMs)
			break
		}
	}
	return time.Duration(delayMs) * time.Millisecond
}

func writeProxyError(w http.ResponseWriter, statusCode int, message string, errorType string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    errorType,
		},
	})
}

func buildRequestDebugSummary(r *http.Request, body []byte, requestedModel string, effectiveModel string) requestDebugSummary {
	summary := requestDebugSummary{
		Path:           r.URL.Path,
		ContentType:    strings.TrimSpace(r.Header.Get("Content-Type")),
		UserAgent:      truncateForLog(strings.TrimSpace(r.UserAgent()), 120),
		RequestedModel: requestedModel,
		EffectiveModel: effectiveModel,
		BodyBytes:      len(body),
	}

	mediaType, _, err := mime.ParseMediaType(summary.ContentType)
	if err != nil {
		mediaType = summary.ContentType
	}

	if mediaType == "" || mediaType == "application/json" {
		populateJSONDebugSummary(&summary, body)
	}

	return summary
}

func populateJSONDebugSummary(summary *requestDebugSummary, body []byte) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return
	}

	summary.JSONKeys = sortedKeys(payload)
	if previousResponseID, ok := payload["previous_response_id"].(string); ok && strings.TrimSpace(previousResponseID) != "" {
		summary.HasPreviousResponse = true
		summary.PreviousResponseID = truncateForLog(previousResponseID, 64)
	}
	summary.ToolCount = arrayLen(payload["tools"])
	summary.InputItemCount = estimateInputItemCount(payload["input"])
	summary.InputTextChars = estimateInputTextChars(payload["input"])
	summary.HasReasoning = hasNonNilValue(payload["reasoning"])
	summary.HasTextConfig = hasNonNilValue(payload["text"])
	summary.HasStore = hasNonNilValue(payload["store"])
	if stream, ok := payload["stream"].(bool); ok {
		summary.Stream = stream
	}
}

func logFailureDiagnostics(requestID string, summary requestDebugSummary, upstream string, statusCode int, captured *capturedResponse) {
	if !shouldLogFailureDiagnostics(summary) {
		return
	}

	payload := map[string]any{
		"request_id": requestID,
		"component":  "proxy_failure_debug",
		"upstream":   upstream,
		"status":     statusCode,
		"request":    summary,
	}
	if captured != nil {
		payload["response_body_preview"] = responseBodyPreview(captured.Body)
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		log.Printf(
			"request_id=%s component=proxy_failure_debug upstream=%q status=%d request_path=%q requested_model=%q effective_model=%q",
			requestID,
			upstream,
			statusCode,
			summary.Path,
			summary.RequestedModel,
			summary.EffectiveModel,
		)
		return
	}
	log.Printf("%s", encoded)
}

func shouldLogFailureDiagnostics(summary requestDebugSummary) bool {
	if summary.Path != "/v1/responses" {
		return false
	}
	if strings.Contains(strings.ToLower(summary.RequestedModel), "codex") {
		return true
	}
	return strings.Contains(strings.ToLower(summary.UserAgent), "codex desktop")
}

func responseBodyPreview(body []byte) string {
	return truncateForLog(compactWhitespace(string(body)), 240)
}

func compactWhitespace(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func truncateForLog(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}

func sortedKeys(payload map[string]any) []string {
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func hasNonNilValue(value any) bool {
	return value != nil
}

func arrayLen(value any) int {
	items, ok := value.([]any)
	if !ok {
		return 0
	}
	return len(items)
}

func estimateInputItemCount(value any) int {
	switch input := value.(type) {
	case []any:
		return len(input)
	case string:
		if strings.TrimSpace(input) == "" {
			return 0
		}
		return 1
	case map[string]any:
		return 1
	default:
		return 0
	}
}

func estimateInputTextChars(value any) int {
	switch input := value.(type) {
	case string:
		return len(input)
	case []any:
		total := 0
		for _, item := range input {
			total += estimateInputTextChars(item)
		}
		return total
	case map[string]any:
		total := 0
		if text, ok := input["text"].(string); ok {
			total += len(text)
		}
		if content, ok := input["content"].([]any); ok {
			for _, part := range content {
				total += estimateInputTextChars(part)
			}
		}
		return total
	default:
		return 0
	}
}

func cloneMIMEHeader(src map[string][]string) map[string][]string {
	dst := make(map[string][]string, len(src))
	for key, values := range src {
		dst[key] = append([]string(nil), values...)
	}
	return dst
}

func joinURL(baseURL string, path string) string {
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}
