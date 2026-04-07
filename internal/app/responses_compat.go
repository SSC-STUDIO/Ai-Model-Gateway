package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ai-model-gateway/internal/core"
)

func shouldAttemptResponsesCompat(req *core.GatewayRequest, resp *core.GatewayResponse) bool {
	if req == nil || resp == nil {
		return false
	}
	if req.Path != "/v1/responses" {
		return false
	}
	bridged := strings.TrimSpace(req.OriginalModel) != "" &&
		strings.TrimSpace(req.Model) != "" &&
		strings.TrimSpace(req.OriginalModel) != strings.TrimSpace(req.Model)
	claudeCompat := strings.HasPrefix(strings.ToLower(strings.TrimSpace(req.Model)), "claude-") ||
		strings.HasPrefix(strings.ToLower(strings.TrimSpace(req.OriginalModel)), "claude-")
	if !bridged && !claudeCompat {
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
	return strings.Contains(text, "not implemented") ||
		strings.Contains(text, "unsupported") ||
		strings.Contains(text, "not found")
}

func shouldAttemptResponsesAnthropicCompat(req *core.GatewayRequest, resp *core.GatewayResponse) bool {
	if req == nil || resp == nil || req.Provider == nil {
		return false
	}
	if req.Path != "/v1/responses" {
		return false
	}
	if req.Provider.AnthropicBaseURL == "" && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(req.Model)), "claude-") &&
		!strings.HasPrefix(strings.ToLower(strings.TrimSpace(req.OriginalModel)), "claude-") {
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
	return strings.Contains(text, "anthropic") ||
		strings.Contains(text, "messages api") ||
		strings.Contains(text, "unsupported") ||
		strings.Contains(text, "service temporarily unavailable") ||
		(strings.TrimSpace(req.Provider.AnthropicBaseURL) != "" && strings.Contains(text, "forbidden"))
}

func buildResponsesCompatRequest(req *core.GatewayRequest) (*core.GatewayRequest, bool, error) {
	body, streamRequested, err := ResponsesToChatRequest(req.Body, req.Model)
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

func buildResponsesCompatResponse(chatResp *core.GatewayResponse, responseModel string, streamRequested bool) (*core.GatewayResponse, error) {
	body, err := ChatToResponsesResponse(chatResp.Body, responseModel)
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

	var response map[string]any
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	streamBody := marshalResponsesCompatStream(response)
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

func buildResponsesAnthropicCompatRequest(req *core.GatewayRequest) (*core.GatewayRequest, bool, error) {
	chatBody, streamRequested, err := ResponsesToChatRequest(req.Body, req.Model)
	if err != nil {
		return nil, false, err
	}
	if streamRequested {
		chatBody = rewriteJSONStreamField(chatBody, false)
	}
	anthropicBody, _, err := ChatToAnthropicRequest(chatBody, req.Model)
	if err != nil {
		return nil, false, err
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
		Body:             anthropicBody,
		UserAgent:        req.UserAgent,
		Attempt:          req.Attempt,
		MaxAttempts:      req.MaxAttempts,
		Provider:         req.Provider,
		Ctx:              req.Ctx,
		ModelRequired:    true,
		SkipModelRewrite: true,
	}, streamRequested, nil
}

func buildResponsesAnthropicCompatResponse(messageResp *core.GatewayResponse, responseModel string, streamRequested bool) (*core.GatewayResponse, error) {
	chatBody, err := AnthropicToChatResponse(messageResp.Body, responseModel)
	if err != nil {
		return nil, err
	}
	body, err := ChatToResponsesResponse(chatBody, responseModel)
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

	var response map[string]any
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	streamBody := marshalResponsesCompatStream(response)
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

func rewriteJSONStreamField(body []byte, stream bool) []byte {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	payload["stream"] = stream
	rewritten, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return rewritten
}

func cloneHeader(src http.Header) http.Header {
	if src == nil {
		return nil
	}
	return src.Clone()
}

func replacePathPreservingQuery(originalPath string, newPath string) string {
	if strings.TrimSpace(newPath) == "" {
		return newPath
	}
	idx := strings.Index(originalPath, "?")
	if idx < 0 || idx >= len(originalPath)-1 {
		return newPath
	}
	return newPath + originalPath[idx:]
}

func marshalResponsesCompatStream(response map[string]any) []byte {
	var buf bytes.Buffer
	sequence := 1

	writeEvent := func(name string, payload map[string]any) {
		payload["type"] = name
		payload["sequence_number"] = sequence
		sequence++
		data, _ := json.Marshal(payload)
		buf.WriteString("event: ")
		buf.WriteString(name)
		buf.WriteString("\n")
		buf.WriteString("data: ")
		buf.Write(data)
		buf.WriteString("\n\n")
	}

	createdPayload := map[string]any{
		"response": map[string]any{
			"id":     response["id"],
			"object": "response",
			"status": "in_progress",
			"model":  response["model"],
		},
	}
	writeEvent("response.created", createdPayload)

	if output, ok := response["output"].([]any); ok {
		for outputIndex, rawItem := range output {
			item, ok := rawItem.(map[string]any)
			if !ok {
				continue
			}
			writeEvent("response.output_item.added", map[string]any{
				"output_index": outputIndex,
				"item":         item,
			})

			itemType, _ := item["type"].(string)
			if itemType != "message" {
				continue
			}
			itemID, _ := item["id"].(string)
			contentItems, ok := item["content"].([]any)
			if !ok {
				continue
			}
			for contentIndex, rawContent := range contentItems {
				content, ok := rawContent.(map[string]any)
				if !ok {
					continue
				}
				contentType, _ := content["type"].(string)
				if contentType != "output_text" {
					continue
				}
				text, _ := content["text"].(string)
				if strings.TrimSpace(text) == "" {
					continue
				}
				writeEvent("response.output_text.delta", map[string]any{
					"content_index": contentIndex,
					"delta":         text,
					"item_id":       itemID,
					"logprobs":      []any{},
					"output_index":  outputIndex,
				})
			}
		}
	}

	writeEvent("response.completed", map[string]any{
		"response": response,
	})
	return buf.Bytes()
}

func (pl *pipeline) tryResponsesCompat(ctx context.Context, req *core.GatewayRequest, resp *core.GatewayResponse) *core.GatewayResponse {
	if !shouldAttemptResponsesCompat(req, resp) {
		return resp
	}

	compatReq, streamRequested, err := buildResponsesCompatRequest(req)
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

	finalResp, err := buildResponsesCompatResponse(compatResp, responseModel, streamRequested)
	if err != nil {
		return resp
	}
	finalResp.RouteMode = "responses_compat"
	finalResp.Provider = req.Provider
	finalResp.Latency = compatResp.Latency
	return finalResp
}

func (pl *pipeline) tryResponsesAnthropicCompat(ctx context.Context, req *core.GatewayRequest, resp *core.GatewayResponse) *core.GatewayResponse {
	if !shouldAttemptResponsesAnthropicCompat(req, resp) {
		return resp
	}

	compatReq, streamRequested, err := buildResponsesAnthropicCompatRequest(req)
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

	finalResp, err := buildResponsesAnthropicCompatResponse(compatResp, responseModel, streamRequested)
	if err != nil {
		return resp
	}
	finalResp.RouteMode = "anthropic_messages_compat"
	finalResp.Provider = req.Provider
	finalResp.Latency = compatResp.Latency
	return finalResp
}

func ChatToAnthropicRequest(body []byte, model string) ([]byte, bool, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false, fmt.Errorf("parse chat payload: %w", err)
	}
	if strings.TrimSpace(model) == "" {
		if rawModel, ok := payload["model"].(string); ok {
			model = strings.TrimSpace(rawModel)
		}
	}
	if model == "" {
		return nil, false, fmt.Errorf("chat model is empty")
	}
	stream, _ := payload["stream"].(bool)

	var systemParts []string
	messages := make([]map[string]any, 0)
	if rawMessages, ok := payload["messages"].([]any); ok {
		for _, item := range rawMessages {
			normalized, systemText := normalizeChatMessageToAnthropic(item)
			if systemText != "" {
				systemParts = append(systemParts, systemText)
			}
			messages = append(messages, normalized...)
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
	if temperature, ok := payload["temperature"]; ok {
		anthropic["temperature"] = temperature
	}
	if topP, ok := payload["top_p"]; ok {
		anthropic["top_p"] = topP
	}
	copyChatStopToAnthropic(payload, anthropic)
	if stream {
		anthropic["stream"] = true
	}

	out, err := json.Marshal(anthropic)
	if err != nil {
		return nil, stream, err
	}
	return out, stream, nil
}

func AnthropicToChatResponse(body []byte, model string) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse anthropic message: %w", err)
	}
	if strings.TrimSpace(model) == "" {
		if rawModel, ok := payload["model"].(string); ok {
			model = rawModel
		}
	}

	text := ""
	if content, ok := payload["content"].([]any); ok {
		for _, item := range content {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if blockType, _ := block["type"].(string); strings.TrimSpace(blockType) == "text" {
				if blockText, _ := block["text"].(string); blockText != "" {
					text += blockText
				}
			}
		}
	}

	promptTokens := 0
	completionTokens := 0
	if usage, ok := payload["usage"].(map[string]any); ok {
		promptTokens, _ = toInt(usage["input_tokens"])
		completionTokens, _ = toInt(usage["output_tokens"])
	}

	out, err := json.Marshal(map[string]any{
		"id":      payload["id"],
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
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      promptTokens + completionTokens,
		},
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func normalizeChatMessageToAnthropic(item any) ([]map[string]any, string) {
	msg, ok := item.(map[string]any)
	if !ok {
		return nil, ""
	}

	role, _ := msg["role"].(string)
	role = strings.TrimSpace(role)
	if role == "" {
		role = "user"
	}
	if role == "system" {
		return nil, extractTextContent(msg["content"])
	}

	if role == "tool" {
		toolCallID, _ := msg["tool_call_id"].(string)
		toolCallID = strings.TrimSpace(toolCallID)
		if toolCallID == "" {
			return nil, ""
		}
		content := stringifyStructuredContent(msg["content"])
		return []map[string]any{
			{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":        "tool_result",
						"tool_use_id": toolCallID,
						"content":     content,
					},
				},
			},
		}, ""
	}

	contentBlocks := chatContentToAnthropicBlocks(msg["content"])
	var toolCalls []any
	if role == "assistant" {
		toolCalls = chatToolCallsToAnthropicContent(chatToolCallsRaw(msg["tool_calls"]))
	}
	content := anthropicContentFromBlocks(contentBlocks, toolCalls)
	if content == nil {
		return nil, ""
	}
	if role != "assistant" {
		role = "user"
	}
	return []map[string]any{
		{
			"role":    role,
			"content": content,
		},
	}, ""
}

func chatContentToAnthropicBlocks(content any) []any {
	switch value := content.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return nil
		}
		return []any{map[string]any{"type": "text", "text": value}}
	case []any:
		blocks := make([]any, 0, len(value))
		for _, item := range value {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			blockType, _ := block["type"].(string)
			switch strings.TrimSpace(blockType) {
			case "", "text":
				text, _ := block["text"].(string)
				if strings.TrimSpace(text) == "" {
					continue
				}
				blocks = append(blocks, map[string]any{"type": "text", "text": text})
			case "image_url":
				url := ""
				if imageURL, ok := block["image_url"].(map[string]any); ok {
					url, _ = imageURL["url"].(string)
				}
				if strings.TrimSpace(url) == "" {
					continue
				}
				blocks = append(blocks, map[string]any{
					"type": "image",
					"source": map[string]any{
						"type": "url",
						"url":  url,
					},
				})
			}
		}
		return blocks
	default:
		return nil
	}
}

func anthropicContentFromBlocks(contentBlocks []any, toolCalls []any) any {
	if len(contentBlocks) == 0 && len(toolCalls) == 0 {
		return nil
	}
	if len(toolCalls) == 0 && len(contentBlocks) == 1 {
		if block, ok := contentBlocks[0].(map[string]any); ok {
			if blockType, _ := block["type"].(string); strings.TrimSpace(blockType) == "text" {
				if text, _ := block["text"].(string); strings.TrimSpace(text) != "" {
					return text
				}
			}
		}
	}

	content := make([]any, 0, len(contentBlocks)+len(toolCalls))
	content = append(content, contentBlocks...)
	content = append(content, toolCalls...)
	return content
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

func stringifyStructuredContent(content any) string {
	if text := extractTextContent(content); text != "" {
		return text
	}
	if content == nil {
		return ""
	}
	data, err := json.Marshal(content)
	if err != nil {
		return ""
	}
	return string(data)
}

func chatToolCallsToAnthropicContent(toolCalls []any) []any {
	content := make([]any, 0, len(toolCalls))
	for _, item := range toolCalls {
		toolCall, ok := item.(map[string]any)
		if !ok {
			continue
		}
		callID, _ := toolCall["id"].(string)
		function, _ := toolCall["function"].(map[string]any)
		name, _ := function["name"].(string)
		arguments, _ := function["arguments"].(string)
		var input any = map[string]any{}
		if strings.TrimSpace(arguments) != "" {
			var parsed any
			if err := json.Unmarshal([]byte(arguments), &parsed); err == nil {
				input = parsed
			} else {
				input = map[string]any{"raw": arguments}
			}
		}
		content = append(content, map[string]any{
			"type":  "tool_use",
			"id":    strings.TrimSpace(callID),
			"name":  strings.TrimSpace(name),
			"input": input,
		})
	}
	return content
}

func chatToolCallsRaw(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, item)
		}
		return result
	default:
		return nil
	}
}
