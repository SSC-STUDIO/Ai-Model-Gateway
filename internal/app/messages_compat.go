package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"ai-model-gateway/internal/core"
)

func shouldAttemptMessagesCompat(req *core.GatewayRequest, resp *core.GatewayResponse) bool {
	if req == nil || resp == nil {
		return false
	}
	if req.Path != "/v1/messages" {
		return false
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(req.Model)), "claude-") {
		return false
	}
	if resp.Stream {
		return false
	}
	if resp.StatusCode == http.StatusForbidden ||
		resp.StatusCode == http.StatusNotFound ||
		resp.StatusCode == http.StatusMethodNotAllowed ||
		resp.StatusCode == http.StatusNotImplemented ||
		resp.StatusCode == http.StatusServiceUnavailable {
		return true
	}

	text := strings.ToLower(strings.TrimSpace(string(resp.Body)))
	return strings.Contains(text, "/v1/messages dispatch") ||
		strings.Contains(text, "anthropic") ||
		strings.Contains(text, "messages api") ||
		strings.Contains(text, "unsupported") ||
		strings.Contains(text, "not allow")
}

func buildMessagesCompatRequest(req *core.GatewayRequest) (*core.GatewayRequest, bool, error) {
	body, streamRequested, err := AnthropicToChatRequest(req.Body, req.Model)
	if err != nil {
		return nil, false, err
	}
	if streamRequested {
		body = rewriteJSONStreamField(body, false)
	}

	headers := req.Headers.Clone()
	headers.Del("anthropic-version")
	headers.Del("anthropic-beta")
	headers.Del("x-api-key")

	return &core.GatewayRequest{
		ID:               req.ID,
		OriginalModel:    req.OriginalModel,
		Model:            req.Model,
		Method:           http.MethodPost,
		Path:             "/v1/chat/completions",
		UpstreamPath:     replacePathPreservingQuery(req.UpstreamPath, "/v1/chat/completions"),
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

func buildMessagesCompatResponse(chatResp *core.GatewayResponse, responseModel string, streamRequested bool) (*core.GatewayResponse, error) {
	body, err := ChatToAnthropicResponse(chatResp.Body, responseModel)
	if err != nil {
		return nil, err
	}

	if !streamRequested {
		headers := cloneHeader(chatResp.Headers)
		if headers == nil {
			headers = http.Header{}
		}
		headers.Set("Content-Type", "application/json")
		return &core.GatewayResponse{
			StatusCode: chatResp.StatusCode,
			Headers:    headers,
			Body:       body,
			Provider:   chatResp.Provider,
			Latency:    chatResp.Latency,
			Model:      responseModel,
		}, nil
	}

	var message map[string]any
	if err := json.Unmarshal(body, &message); err != nil {
		return nil, err
	}
	streamBody := marshalAnthropicMessageCompatStream(message)
	headers := cloneHeader(chatResp.Headers)
	if headers == nil {
		headers = http.Header{}
	}
	headers.Set("Content-Type", "text/event-stream")
	headers.Set("Cache-Control", "no-cache")
	headers.Set("Connection", "keep-alive")
	return &core.GatewayResponse{
		StatusCode: chatResp.StatusCode,
		Headers:    headers,
		Body:       streamBody,
		BodyReader: io.NopCloser(bytes.NewReader(streamBody)),
		Stream:     true,
		Provider:   chatResp.Provider,
		Latency:    chatResp.Latency,
		Model:      responseModel,
	}, nil
}

func marshalAnthropicMessageCompatStream(message map[string]any) []byte {
	usage, _ := message["usage"].(map[string]any)
	outputTokens := 0
	if value, ok := usage["output_tokens"]; ok {
		if parsed, ok := toInt(value); ok {
			outputTokens = parsed
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

	var builder strings.Builder
	writeAnthropicCompatEvent(&builder, "message_start", startPayload)

	blocks, ok := message["content"].([]any)
	if !ok || len(blocks) == 0 {
		blocks = []any{map[string]any{"type": "text", "text": ""}}
	}
	for index, item := range blocks {
		block, ok := item.(map[string]any)
		if !ok {
			continue
		}
		blockType, _ := block["type"].(string)
		switch strings.TrimSpace(blockType) {
		case "tool_use":
			writeAnthropicCompatEvent(&builder, "content_block_start", map[string]any{
				"type":          "content_block_start",
				"index":         index,
				"content_block": block,
			})
			writeAnthropicCompatEvent(&builder, "content_block_stop", map[string]any{
				"type":  "content_block_stop",
				"index": index,
			})
		default:
			writeAnthropicCompatEvent(&builder, "content_block_start", map[string]any{
				"type":          "content_block_start",
				"index":         index,
				"content_block": map[string]any{"type": "text", "text": ""},
			})
			writeAnthropicCompatEvent(&builder, "content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": index,
				"delta": map[string]any{"type": "text_delta", "text": extractTextFromAnthropicBlock(block)},
			})
			writeAnthropicCompatEvent(&builder, "content_block_stop", map[string]any{
				"type":  "content_block_stop",
				"index": index,
			})
		}
	}

	writeAnthropicCompatEvent(&builder, "message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   message["stop_reason"],
			"stop_sequence": message["stop_sequence"],
		},
		"usage": map[string]any{
			"output_tokens": outputTokens,
		},
	})
	writeAnthropicCompatEvent(&builder, "message_stop", map[string]any{"type": "message_stop"})
	return []byte(builder.String())
}

func writeAnthropicCompatEvent(builder *strings.Builder, name string, payload map[string]any) {
	if builder == nil {
		return
	}
	body, _ := json.Marshal(payload)
	builder.WriteString("event: ")
	builder.WriteString(name)
	builder.WriteString("\n")
	builder.WriteString("data: ")
	builder.Write(body)
	builder.WriteString("\n\n")
}

func extractTextFromAnthropicBlock(block map[string]any) string {
	if block == nil {
		return ""
	}
	if text, ok := block["text"].(string); ok {
		return text
	}
	return ""
}

func (pl *pipeline) tryMessagesCompat(ctx context.Context, req *core.GatewayRequest, resp *core.GatewayResponse) *core.GatewayResponse {
	if !shouldAttemptMessagesCompat(req, resp) {
		return resp
	}

	compatReq, streamRequested, err := buildMessagesCompatRequest(req)
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

	finalResp, err := buildMessagesCompatResponse(compatResp, responseModel, streamRequested)
	if err != nil {
		return resp
	}
	finalResp.Provider = req.Provider
	finalResp.Latency = compatResp.Latency
	return finalResp
}
