package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"ai-model-gateway/internal/core"
)

func shouldAttemptChatAnthropicCompat(req *core.GatewayRequest, resp *core.GatewayResponse) bool {
	if req == nil || resp == nil || req.Provider == nil {
		return false
	}
	if req.Path != "/v1/chat/completions" {
		return false
	}
	claudeCompat := strings.HasPrefix(strings.ToLower(strings.TrimSpace(req.Model)), "claude-") ||
		strings.HasPrefix(strings.ToLower(strings.TrimSpace(req.OriginalModel)), "claude-")
	explicitAnthropicCompat := strings.TrimSpace(req.Provider.AnthropicBaseURL) != ""
	if !claudeCompat && !explicitAnthropicCompat {
		return false
	}
	if resp.Stream {
		return false
	}
	if resp.StatusCode == http.StatusForbidden && explicitAnthropicCompat {
		return true
	}
	if resp.StatusCode == http.StatusNotImplemented ||
		resp.StatusCode == http.StatusNotFound ||
		resp.StatusCode == http.StatusMethodNotAllowed ||
		resp.StatusCode == http.StatusServiceUnavailable {
		return true
	}

	text := strings.ToLower(strings.TrimSpace(string(resp.Body)))
	return strings.Contains(text, "anthropic") ||
		strings.Contains(text, "messages api") ||
		(explicitAnthropicCompat && strings.Contains(text, "forbidden")) ||
		strings.Contains(text, "service temporarily unavailable") ||
		strings.Contains(text, "unsupported")
}

func buildChatAnthropicCompatRequest(req *core.GatewayRequest) (*core.GatewayRequest, bool, error) {
	body, streamRequested, err := ChatToAnthropicRequest(req.Body, req.Model)
	if err != nil {
		return nil, false, err
	}
	if streamRequested {
		body = rewriteJSONStreamField(body, false)
	}

	headers := req.Headers.Clone()
	headers.Del("Authorization")

	return &core.GatewayRequest{
		ID:               req.ID,
		OriginalModel:    req.OriginalModel,
		Model:            req.Model,
		Method:           http.MethodPost,
		Path:             "/v1/messages",
		UpstreamPath:     replacePathPreservingQuery(req.UpstreamPath, "/v1/messages"),
		Headers:          headers,
		Body:             body,
		UserAgent:        req.UserAgent,
		Attempt:          req.Attempt,
		MaxAttempts:      req.MaxAttempts,
		Provider:         req.Provider,
		Ctx:              req.Ctx,
		ModelRequired:    true,
		SkipModelRewrite: true,
	}, streamRequested, nil
}

func buildChatAnthropicCompatResponse(messageResp *core.GatewayResponse, responseModel string, streamRequested bool) (*core.GatewayResponse, error) {
	body, err := AnthropicToChatResponse(messageResp.Body, responseModel)
	if err != nil {
		return nil, err
	}

	if !streamRequested {
		headers := cloneHeader(messageResp.Headers)
		if headers == nil {
			headers = http.Header{}
		}
		headers.Set("Content-Type", "application/json")
		return &core.GatewayResponse{
			StatusCode: messageResp.StatusCode,
			Headers:    headers,
			Body:       body,
			Provider:   messageResp.Provider,
			Latency:    messageResp.Latency,
			Model:      responseModel,
		}, nil
	}

	var chat map[string]any
	if err := json.Unmarshal(body, &chat); err != nil {
		return nil, err
	}
	streamBody := marshalChatCompletionsCompatStream(chat)
	headers := cloneHeader(messageResp.Headers)
	if headers == nil {
		headers = http.Header{}
	}
	headers.Set("Content-Type", "text/event-stream")
	headers.Set("Cache-Control", "no-cache")
	headers.Set("Connection", "keep-alive")
	return &core.GatewayResponse{
		StatusCode: messageResp.StatusCode,
		Headers:    headers,
		Body:       streamBody,
		BodyReader: io.NopCloser(bytes.NewReader(streamBody)),
		Stream:     true,
		Provider:   messageResp.Provider,
		Latency:    messageResp.Latency,
		Model:      responseModel,
	}, nil
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
	var toolCalls []any
	finishReason := "stop"
	if choices, ok := chat["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if reason, ok := choice["finish_reason"].(string); ok && strings.TrimSpace(reason) != "" {
				finishReason = reason
			}
			if message, ok := choice["message"].(map[string]any); ok {
				content = extractTextContent(message["content"])
				toolCalls = chatToolCallsRaw(message["tool_calls"])
			}
		}
	}

	delta := map[string]any{
		"role": "assistant",
	}
	if content != "" {
		delta["content"] = content
	}
	if len(toolCalls) > 0 {
		delta["tool_calls"] = toolCalls
	}

	firstChunk := map[string]any{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []map[string]any{
			{
				"index":         0,
				"delta":         delta,
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

func (pl *pipeline) tryChatAnthropicCompat(ctx context.Context, req *core.GatewayRequest, resp *core.GatewayResponse) *core.GatewayResponse {
	if !shouldAttemptChatAnthropicCompat(req, resp) {
		return resp
	}

	compatReq, streamRequested, err := buildChatAnthropicCompatRequest(req)
	if err != nil {
		return resp
	}

	compatResp, execErr := pl.transport.Execute(ctx, compatReq)
	if execErr != nil {
		return resp
	}
	compatResp, _ = pl.inspector.Inspect(ctx, compatReq, compatResp)
	if compatResp.Error != nil || compatResp.Retryable || compatResp.StatusCode >= http.StatusBadRequest {
		return resp
	}

	responseModel := req.Model
	if strings.TrimSpace(req.OriginalModel) != "" {
		responseModel = req.OriginalModel
	}

	finalResp, err := buildChatAnthropicCompatResponse(compatResp, responseModel, streamRequested)
	if err != nil {
		return resp
	}
	finalResp.RouteMode = "anthropic_messages_compat"
	finalResp.Provider = req.Provider
	finalResp.Latency = compatResp.Latency
	return finalResp
}
