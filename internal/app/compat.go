package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ai-model-gateway/internal/core"
)

// compatAdapter implements core.CompatAdapter.
// It handles:
//   - Rewriting the "model" field in request/response JSON bodies for bridge
//   - Protocol conversion between Anthropic Messages ↔ Chat Completions
//   - Protocol conversion between Responses API ↔ Chat Completions
type compatAdapter struct {
	bridge core.BridgeConfig
}

// NewCompatAdapter creates a CompatAdapter from compat config.
func NewCompatAdapter(compat core.CompatConfig) core.CompatAdapter {
	return &compatAdapter{
		bridge: compat.Bridge,
	}
}

// AdaptRequest rewrites the model field in the outbound JSON body to the
// resolved model name (if bridge rewrote it), so the upstream sees the
// correct model identifier.
func (c *compatAdapter) AdaptRequest(_ context.Context, req *core.GatewayRequest) error {
	if len(req.Body) == 0 {
		return nil
	}

	if req.Path == "/v1/messages/count_tokens" {
		probeBody, err := buildAnthropicCountTokensProbeBody(req.Body, req.Model)
		if err != nil {
			return err
		}
		req.Body = probeBody
		req.UpstreamPath = replacePathPreservingQuery(req.UpstreamPath, "/v1/messages")
		return nil
	}

	// If bridge changed the model, rewrite it in the JSON body.
	if req.OriginalModel != "" && req.OriginalModel != req.Model {
		req.Body = rewriteJSONModelField(req.Body, req.Model)
	}

	return nil
}

// AdaptResponse rewrites the model field in the response JSON body back to
// the originally-requested model name, so the client sees the model it asked for.
func (c *compatAdapter) AdaptResponse(_ context.Context, req *core.GatewayRequest, resp *core.GatewayResponse) error {
	if len(resp.Body) == 0 {
		return nil
	}

	if req.Path == "/v1/messages/count_tokens" {
		body, err := buildCountTokensResponseFromAnthropic(resp.Body)
		if err != nil {
			return err
		}
		resp.Body = body
		resp.RouteMode = "anthropic_count_tokens_compat"
		if resp.Headers == nil {
			resp.Headers = make(map[string][]string)
		}
		resp.Headers.Set("Content-Type", "application/json")
		return nil
	}

	// Rewrite model name back to original if bridge was active.
	if req.OriginalModel != "" && req.OriginalModel != req.Model {
		resp.Body = rewriteJSONModelField(resp.Body, req.OriginalModel)
	}

	return nil
}

func buildAnthropicCountTokensProbeBody(body []byte, model string) ([]byte, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse count_tokens request: %w", err)
	}

	if strings.TrimSpace(model) == "" {
		if rawModel, ok := payload["model"].(string); ok {
			model = rawModel
		}
	}

	payload["model"] = normalizeCountTokensModel(model)
	payload["max_tokens"] = 1
	payload["stream"] = false

	out, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("rewrite count_tokens request: %w", err)
	}
	return out, nil
}

func buildCountTokensResponseFromAnthropic(body []byte) ([]byte, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse anthropic count_tokens response: %w", err)
	}

	usage, ok := payload["usage"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("anthropic usage missing input_tokens")
	}
	inputTokens, ok := toInt(usage["input_tokens"])
	if !ok {
		return nil, fmt.Errorf("anthropic usage missing input_tokens")
	}

	out, err := json.Marshal(map[string]int{"input_tokens": inputTokens})
	if err != nil {
		return nil, fmt.Errorf("rewrite count_tokens response: %w", err)
	}
	return out, nil
}

func normalizeCountTokensModel(model string) string {
	switch strings.TrimSpace(model) {
	case "claude-opus-4-6", "claude-opus-4-6-thinking":
		return "claude-sonnet-4-6"
	default:
		return strings.TrimSpace(model)
	}
}

// ---------------------------------------------------------------------------
// JSON model-field rewriting
// ---------------------------------------------------------------------------

// rewriteJSONModelField performs a targeted rewrite of the top-level "model"
// field in a JSON body. It uses partial unmarshal to avoid disturbing
// the rest of the JSON payload.
func rewriteJSONModelField(body []byte, newModel string) []byte {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return body // not valid JSON — pass through
	}
	if _, ok := obj["model"]; !ok {
		return body // no model field
	}
	modelBytes, err := json.Marshal(newModel)
	if err != nil {
		return body
	}
	obj["model"] = modelBytes
	out, err := json.Marshal(obj)
	if err != nil {
		return body
	}
	return out
}

// ---------------------------------------------------------------------------
// Anthropic Messages → Chat Completions conversion
// ---------------------------------------------------------------------------

// AnthropicToChatRequest converts an Anthropic Messages API request body
// to an OpenAI Chat Completions request body.
func AnthropicToChatRequest(body []byte, model string) ([]byte, bool, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false, err
	}
	if model == "" {
		if m, ok := payload["model"].(string); ok {
			model = strings.TrimSpace(m)
		}
	}
	stream, _ := payload["stream"].(bool)

	var messages []map[string]interface{}

	// System message.
	if sys := extractTextContent(payload["system"]); sys != "" {
		messages = append(messages, map[string]interface{}{
			"role":    "system",
			"content": sys,
		})
	}

	// Convert Anthropic messages to Chat messages.
	if rawMessages, ok := payload["messages"].([]interface{}); ok {
		normalized, err := normalizeAnthropicMessagesForChat(rawMessages)
		if err != nil {
			return nil, false, err
		}
		messages = append(messages, normalized...)
	}

	chat := map[string]interface{}{
		"model":    model,
		"messages": messages,
	}

	// Copy compatible fields.
	for _, key := range []string{"max_tokens", "temperature", "top_p"} {
		if v, ok := payload[key]; ok {
			chat[key] = v
		}
	}
	if stop, ok := payload["stop_sequences"]; ok {
		chat["stop"] = stop
	}
	if tools := convertAnthropicToolsToChat(payload["tools"]); len(tools) > 0 {
		chat["tools"] = tools
	}
	if rawToolChoice, ok := payload["tool_choice"]; ok {
		if toolChoice := anthropicToolChoiceToChat(rawToolChoice); toolChoice != nil {
			chat["tool_choice"] = toolChoice
		}
	}
	if stream {
		chat["stream"] = true
	}

	out, err := json.Marshal(chat)
	return out, stream, err
}

// ChatToAnthropicResponse converts an OpenAI Chat Completions response
// to an Anthropic Messages API response.
func ChatToAnthropicResponse(body []byte, model string) ([]byte, error) {
	var chat map[string]interface{}
	if err := json.Unmarshal(body, &chat); err != nil {
		return nil, err
	}

	if model == "" {
		if m, ok := chat["model"].(string); ok {
			model = m
		}
	}

	// Extract first choice content.
	var content []map[string]interface{}
	var stopReason string
	if choices, ok := chat["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if msg, ok := choice["message"].(map[string]interface{}); ok {
				if text, ok := msg["content"].(string); ok && text != "" {
					content = append(content, map[string]interface{}{
						"type": "text",
						"text": text,
					})
				}
				// Convert tool_calls to Anthropic tool_use blocks.
				if toolCalls, ok := msg["tool_calls"].([]interface{}); ok {
					for _, tc := range toolCalls {
						if block := convertChatToolCallToAnthropic(tc); block != nil {
							content = append(content, block)
						}
					}
				}
			}
			if reason, ok := choice["finish_reason"].(string); ok {
				stopReason = mapFinishReasonToAnthropic(reason)
			}
		}
	}

	// Build Anthropic response.
	anthropic := map[string]interface{}{
		"id":          chat["id"],
		"type":        "message",
		"role":        "assistant",
		"model":       model,
		"content":     content,
		"stop_reason": stopReason,
	}

	// Convert usage.
	if usage, ok := chat["usage"].(map[string]interface{}); ok {
		promptTokens, _ := toInt(usage["prompt_tokens"])
		completionTokens, _ := toInt(usage["completion_tokens"])
		anthropic["usage"] = map[string]interface{}{
			"input_tokens":  promptTokens,
			"output_tokens": completionTokens,
		}
	}

	return json.Marshal(anthropic)
}

// ---------------------------------------------------------------------------
// Responses API → Chat Completions conversion
// ---------------------------------------------------------------------------

// ResponsesToChatRequest converts an OpenAI Responses API request body
// to an OpenAI Chat Completions request body.
func ResponsesToChatRequest(body []byte, model string) ([]byte, bool, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false, err
	}
	if model == "" {
		if m, ok := payload["model"].(string); ok {
			model = strings.TrimSpace(m)
		}
	}
	stream, _ := payload["stream"].(bool)

	var messages []map[string]interface{}

	// Instructions → system message.
	if instructions, ok := payload["instructions"].(string); ok && strings.TrimSpace(instructions) != "" {
		messages = append(messages, map[string]interface{}{
			"role":    "system",
			"content": instructions,
		})
	}

	// Input → user messages.
	if input, ok := payload["input"]; ok {
		switch v := input.(type) {
		case string:
			messages = append(messages, map[string]interface{}{
				"role":    "user",
				"content": v,
			})
		case []interface{}:
			for _, item := range v {
				if msg, ok := item.(map[string]interface{}); ok {
					role, _ := msg["role"].(string)
					content, ok := convertResponsesContentToChat(msg["content"])
					if role == "" {
						role = "user"
					}
					if ok {
						messages = append(messages, map[string]interface{}{
							"role":    role,
							"content": content,
						})
					}
				}
			}
		}
	}

	chat := map[string]interface{}{
		"model":    model,
		"messages": messages,
	}

	// Copy compatible fields.
	for _, key := range []string{"tools", "tool_choice", "temperature", "top_p",
		"presence_penalty", "frequency_penalty", "stop", "max_tokens"} {
		if v, ok := payload[key]; ok {
			chat[key] = v
		}
	}
	if maxOut, ok := payload["max_output_tokens"]; ok {
		chat["max_tokens"] = maxOut
	}
	if stream {
		chat["stream"] = true
	}

	out, err := json.Marshal(chat)
	return out, stream, err
}

// ChatToResponsesResponse converts an OpenAI Chat Completions response
// to an OpenAI Responses API response.
func ChatToResponsesResponse(body []byte, model string) ([]byte, error) {
	var chat map[string]interface{}
	if err := json.Unmarshal(body, &chat); err != nil {
		return nil, err
	}

	if model == "" {
		if m, ok := chat["model"].(string); ok {
			model = m
		}
	}

	chatID, _ := chat["id"].(string)

	// Extract output text from first choice.
	var outputText string
	var output []map[string]interface{}
	if choices, ok := chat["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if msg, ok := choice["message"].(map[string]interface{}); ok {
				if text, ok := msg["content"].(string); ok {
					outputText = text
					output = append(output, map[string]interface{}{
						"type": "message",
						"role": "assistant",
						"content": []map[string]interface{}{
							{"type": "output_text", "text": text},
						},
					})
				}
				// Convert tool_calls.
				if toolCalls, ok := msg["tool_calls"].([]interface{}); ok {
					for _, tc := range toolCalls {
						tcMap, ok := tc.(map[string]interface{})
						if !ok {
							continue
						}
						fn, _ := tcMap["function"].(map[string]interface{})
						if fn == nil {
							continue
						}
						output = append(output, map[string]interface{}{
							"type":      "function_call",
							"id":        tcMap["id"],
							"call_id":   tcMap["id"],
							"name":      fn["name"],
							"arguments": fn["arguments"],
						})
					}
				}
			}
		}
	}

	resp := map[string]interface{}{
		"id":          chatID,
		"object":      "response",
		"model":       model,
		"output":      output,
		"output_text": outputText,
	}

	if usage, ok := chat["usage"].(map[string]interface{}); ok {
		resp["usage"] = usage
	}

	return json.Marshal(resp)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// extractTextContent pulls plain text from an Anthropic "content" field,
// which can be a string or an array of content blocks.
func extractTextContent(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	if arr, ok := v.([]interface{}); ok {
		var builder strings.Builder
		for _, item := range arr {
			if block, ok := item.(map[string]interface{}); ok {
				if t, ok := block["text"].(string); ok {
					builder.WriteString(t)
					continue
				}
				if t, ok := block["output_text"].(string); ok {
					builder.WriteString(t)
					continue
				}
				if t, ok := block["content"].(string); ok {
					builder.WriteString(t)
					continue
				}
				if t, ok := block["input_text"].(string); ok {
					builder.WriteString(t)
					continue
				}
			}
		}
		return strings.TrimSpace(builder.String())
	}
	if block, ok := v.(map[string]interface{}); ok {
		if t, ok := block["text"].(string); ok {
			return strings.TrimSpace(t)
		}
		if t, ok := block["output_text"].(string); ok {
			return strings.TrimSpace(t)
		}
		if t, ok := block["content"].(string); ok {
			return strings.TrimSpace(t)
		}
	}
	return ""
}

func convertAnthropicContentToChat(v interface{}) (interface{}, bool) {
	if v == nil {
		return nil, false
	}
	if s, ok := v.(string); ok {
		if strings.TrimSpace(s) == "" {
			return nil, false
		}
		return s, true
	}

	blocks, ok := v.([]interface{})
	if !ok {
		return nil, false
	}

	parts := make([]interface{}, 0, len(blocks))
	textOnly := true
	var textBuilder strings.Builder

	for _, raw := range blocks {
		block, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		blockType, _ := block["type"].(string)
		switch strings.TrimSpace(blockType) {
		case "", "text":
			text, _ := block["text"].(string)
			if text == "" {
				continue
			}
			textBuilder.WriteString(text)
			parts = append(parts, map[string]interface{}{
				"type": "text",
				"text": text,
			})
		case "image":
			url := anthropicImageURL(block)
			if url == "" {
				continue
			}
			textOnly = false
			parts = append(parts, map[string]interface{}{
				"type": "image_url",
				"image_url": map[string]interface{}{
					"url": url,
				},
			})
		}
	}

	if len(parts) == 0 {
		return nil, false
	}
	if textOnly {
		if textBuilder.Len() == 0 {
			return nil, false
		}
		return textBuilder.String(), true
	}
	return parts, true
}

func normalizeAnthropicMessagesForChat(items []interface{}) ([]map[string]interface{}, error) {
	messages := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		msg, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		role = strings.TrimSpace(role)
		if role == "" {
			role = "user"
		}

		switch role {
		case "assistant":
			if assistant := anthropicAssistantMessageToChat(msg["content"]); assistant != nil {
				messages = append(messages, assistant)
			}
		case "user", "tool":
			userMessages := anthropicUserMessageToChat(msg["content"])
			messages = append(messages, userMessages...)
		case "system":
			if text := extractTextContent(msg["content"]); text != "" {
				messages = append(messages, map[string]interface{}{"role": "system", "content": text})
			}
		default:
			userMessages := anthropicUserMessageToChat(msg["content"])
			messages = append(messages, userMessages...)
		}
	}
	return messages, nil
}

func anthropicAssistantMessageToChat(content interface{}) map[string]interface{} {
	blocks, ok := content.([]interface{})
	if !ok {
		text := extractTextContent(content)
		if text == "" {
			return nil
		}
		return map[string]interface{}{
			"role":    "assistant",
			"content": text,
		}
	}

	var textParts []string
	var toolCalls []map[string]interface{}
	for _, item := range blocks {
		block, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		blockType, _ := block["type"].(string)
		switch strings.TrimSpace(blockType) {
		case "text":
			if text := extractTextContent(block); text != "" {
				textParts = append(textParts, text)
			}
		case "tool_use":
			name, _ := block["name"].(string)
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			toolID, _ := block["id"].(string)
			if strings.TrimSpace(toolID) == "" {
				toolID = fmt.Sprintf("call_%d", time.Now().UnixNano())
			}
			args, err := json.Marshal(block["input"])
			if err != nil {
				args = []byte("{}")
			}
			toolCalls = append(toolCalls, map[string]interface{}{
				"id":   toolID,
				"type": "function",
				"function": map[string]interface{}{
					"name":      name,
					"arguments": string(args),
				},
			})
		}
	}

	if len(textParts) == 0 && len(toolCalls) == 0 {
		return nil
	}

	message := map[string]interface{}{
		"role": "assistant",
	}
	if len(textParts) > 0 {
		message["content"] = strings.TrimSpace(strings.Join(textParts, "\n\n"))
	} else {
		message["content"] = ""
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	return message
}

func anthropicUserMessageToChat(content interface{}) []map[string]interface{} {
	blocks, ok := content.([]interface{})
	if !ok {
		text := extractTextContent(content)
		if text == "" {
			return nil
		}
		return []map[string]interface{}{{"role": "user", "content": text}}
	}

	var result []map[string]interface{}
	var userParts []interface{}
	flushUser := func() {
		if len(userParts) == 0 {
			return
		}
		result = append(result, map[string]interface{}{
			"role":    "user",
			"content": chatContentFromParts(userParts),
		})
		userParts = nil
	}

	for _, item := range blocks {
		block, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		blockType, _ := block["type"].(string)
		switch strings.TrimSpace(blockType) {
		case "text":
			if text := extractTextContent(block); text != "" {
				userParts = append(userParts, map[string]interface{}{"type": "text", "text": text})
			}
		case "image":
			if image := anthropicImageBlockToChatPart(block); image != nil {
				userParts = append(userParts, image)
			}
		case "tool_result":
			flushUser()
			toolCallID, _ := block["tool_use_id"].(string)
			result = append(result, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": strings.TrimSpace(toolCallID),
				"content":      extractAnthropicToolResultContent(block["content"]),
			})
		default:
			if text := extractTextContent(block); text != "" {
				userParts = append(userParts, map[string]interface{}{"type": "text", "text": text})
			}
		}
	}
	flushUser()
	return result
}

func extractAnthropicToolResultContent(content interface{}) string {
	if text := extractTextContent(content); text != "" {
		return text
	}
	if data, err := json.Marshal(content); err == nil {
		return string(data)
	}
	return ""
}

func anthropicImageBlockToChatPart(block map[string]interface{}) map[string]interface{} {
	url := anthropicImageURL(block)
	if url == "" {
		return nil
	}
	return map[string]interface{}{
		"type": "image_url",
		"image_url": map[string]interface{}{
			"url": url,
		},
	}
}

func chatContentFromParts(parts []interface{}) interface{} {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		part, ok := parts[0].(map[string]interface{})
		if ok {
			partType, _ := part["type"].(string)
			if strings.TrimSpace(partType) == "text" {
				if text, ok := part["text"].(string); ok {
					return text
				}
			}
		}
	}
	return parts
}

func anthropicImageURL(block map[string]interface{}) string {
	if block == nil {
		return ""
	}
	source, _ := block["source"].(map[string]interface{})
	if source == nil {
		return ""
	}
	sourceType, _ := source["type"].(string)
	switch strings.TrimSpace(sourceType) {
	case "url":
		url, _ := source["url"].(string)
		return strings.TrimSpace(url)
	case "base64":
		data, _ := source["data"].(string)
		if strings.TrimSpace(data) == "" {
			return ""
		}
		mediaType, _ := source["media_type"].(string)
		mediaType = strings.TrimSpace(mediaType)
		if mediaType == "" {
			mediaType = "application/octet-stream"
		}
		return "data:" + mediaType + ";base64," + data
	default:
		return ""
	}
}

func convertResponsesContentToChat(v interface{}) (interface{}, bool) {
	if v == nil {
		return nil, false
	}
	if s, ok := v.(string); ok {
		if strings.TrimSpace(s) == "" {
			return nil, false
		}
		return s, true
	}

	blocks, ok := v.([]interface{})
	if !ok {
		return nil, false
	}

	parts := make([]interface{}, 0, len(blocks))
	textOnly := true
	var textBuilder strings.Builder

	for _, raw := range blocks {
		block, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		blockType, _ := block["type"].(string)
		switch strings.TrimSpace(blockType) {
		case "", "text", "input_text", "output_text":
			text, _ := block["text"].(string)
			if text == "" {
				continue
			}
			textBuilder.WriteString(text)
			parts = append(parts, map[string]interface{}{
				"type": "text",
				"text": text,
			})
		case "input_image", "image_url":
			url := responsesImageURL(block)
			if url == "" {
				continue
			}
			textOnly = false
			parts = append(parts, map[string]interface{}{
				"type": "image_url",
				"image_url": map[string]interface{}{
					"url": url,
				},
			})
		}
	}

	if len(parts) == 0 {
		return nil, false
	}
	if textOnly {
		if textBuilder.Len() == 0 {
			return nil, false
		}
		return textBuilder.String(), true
	}
	return parts, true
}

func responsesImageURL(block map[string]interface{}) string {
	if block == nil {
		return ""
	}
	if url, ok := block["image_url"].(string); ok {
		return strings.TrimSpace(url)
	}
	if imageURL, ok := block["image_url"].(map[string]interface{}); ok {
		if url, ok := imageURL["url"].(string); ok {
			return strings.TrimSpace(url)
		}
	}
	return ""
}

func convertAnthropicToolsToChat(tools interface{}) []map[string]interface{} {
	arr, ok := tools.([]interface{})
	if !ok || len(arr) == 0 {
		return nil
	}
	var result []map[string]interface{}
	for _, item := range arr {
		tool, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := tool["name"].(string)
		desc, _ := tool["description"].(string)
		fn := map[string]interface{}{
			"name": name,
		}
		if desc != "" {
			fn["description"] = desc
		}
		if schema, ok := tool["input_schema"]; ok {
			fn["parameters"] = schema
		}
		result = append(result, map[string]interface{}{
			"type":     "function",
			"function": fn,
		})
	}
	return result
}

func anthropicToolChoiceToChat(raw interface{}) interface{} {
	switch value := raw.(type) {
	case string:
		switch strings.TrimSpace(value) {
		case "auto", "none", "required":
			return strings.TrimSpace(value)
		default:
			return nil
		}
	case map[string]interface{}:
		choiceType, _ := value["type"].(string)
		switch strings.TrimSpace(choiceType) {
		case "auto":
			return "auto"
		case "any":
			return "required"
		case "tool":
			name, _ := value["name"].(string)
			name = strings.TrimSpace(name)
			if name == "" {
				return "required"
			}
			return map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name": name,
				},
			}
		case "none":
			return "none"
		}
	}
	return nil
}

func convertChatToolCallToAnthropic(tc interface{}) map[string]interface{} {
	tcMap, ok := tc.(map[string]interface{})
	if !ok {
		return nil
	}
	fn, ok := tcMap["function"].(map[string]interface{})
	if !ok {
		return nil
	}
	name, _ := fn["name"].(string)
	argsStr, _ := fn["arguments"].(string)
	var input interface{}
	if err := json.Unmarshal([]byte(argsStr), &input); err != nil {
		input = map[string]interface{}{}
	}
	return map[string]interface{}{
		"type":  "tool_use",
		"id":    tcMap["id"],
		"name":  name,
		"input": input,
	}
}

func mapFinishReasonToAnthropic(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	default:
		return reason
	}
}

func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}
