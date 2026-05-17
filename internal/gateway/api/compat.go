package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/gateway/snapshot"

	"github.com/google/uuid"
)

// clientFormat identifies the API format the client is speaking.
type clientFormat int

const (
	formatChatCompletions clientFormat = iota
	formatAnthropic
	formatResponses
)

type responseCompatMode int

var errMessagesAPIRequiresAnthropicProvider = errors.New("messages API requires an Anthropic-compatible provider")

// clientFormatFor converts a boolean isAnthropic flag to clientFormat.
func clientFormatFor(isAnthropic bool) clientFormat {
	if isAnthropic {
		return formatAnthropic
	}
	return formatChatCompletions
}

const (
	responseCompatPassthrough responseCompatMode = iota
	responseCompatAnthropicToOpenAI
	responseCompatOpenAIToAnthropic
	responseCompatChatToResponses
)

type compatPlan struct {
	forwardPath              string
	forwardBody              []byte
	upstreamIsAnthropic      bool
	responseMode             responseCompatMode
	clientModel              string
	responsesCustomToolNames map[string]struct{}
}

type anthropicUsage struct {
	InputTokens          int64 `json:"input_tokens"`
	OutputTokens         int64 `json:"output_tokens"`
	CacheReadInputTokens int64 `json:"cache_read_input_tokens"`
	CacheCreationTokens  int64 `json:"cache_creation_input_tokens"`
}

type anthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type openAIChatMessage struct {
	Role         string                  `json:"role"`
	Content      json.RawMessage         `json:"content"`
	ToolCalls    []openAIToolCall        `json:"tool_calls,omitempty"`
	ToolCallID   string                  `json:"tool_call_id,omitempty"`
	Name         string                  `json:"name,omitempty"`
	FunctionCall *openAIToolCallFunction `json:"function_call,omitempty"`
}

type openAIToolCall struct {
	ID       string                 `json:"id,omitempty"`
	Type     string                 `json:"type,omitempty"`
	Function openAIToolCallFunction `json:"function,omitempty"`
	Custom   openAICustomToolCall   `json:"custom,omitempty"`
}

type openAIToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type openAICustomToolCall struct {
	Name  string `json:"name,omitempty"`
	Input string `json:"input,omitempty"`
}

type anthropicContentBlock struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	Name      string `json:"name,omitempty"`
	ID        string `json:"id,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	Input     any    `json:"input,omitempty"`
	Content   any    `json:"content,omitempty"`
}

type anthropicResponsePayload struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	Role       string                  `json:"role"`
	Model      string                  `json:"model"`
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason,omitempty"`
	Usage      anthropicUsage          `json:"usage"`
}

type anthropicSSEEvent struct {
	Type         string                   `json:"type"`
	Index        int                      `json:"index,omitempty"`
	Delta        anthropicSSEDelta        `json:"delta,omitempty"`
	Message      anthropicResponsePayload `json:"message,omitempty"`
	ContentBlock anthropicContentBlock    `json:"content_block,omitempty"`
	Usage        anthropicUsage           `json:"usage,omitempty"`
	Error        struct {
		Type    string `json:"type,omitempty"`
		Message string `json:"message,omitempty"`
	} `json:"error,omitempty"`
}

type anthropicSSEDelta struct {
	Type        string `json:"type,omitempty"`
	Text        string `json:"text,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
}

type anthropicStreamToolCall struct {
	ID          string
	Name        string
	OpenAIIndex int
	Arguments   bytes.Buffer
}

type anthropicStreamState struct {
	id            string
	model         string
	created       int64
	stopReason    string
	usage         anthropicUsage
	toolCalls     map[int]*anthropicStreamToolCall
	nextToolIndex int
	done          bool
}

type openAIUsagePayload struct {
	PromptTokens        int64 `json:"prompt_tokens"`
	CompletionTokens    int64 `json:"completion_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
	PromptTokensDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	InputTokensDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
}

type openAIStreamToolState struct {
	id        string
	name      string
	block     int
	started   bool
	arguments bytes.Buffer
}

type openAIToAnthropicStreamState struct {
	id             string
	model          string
	created        int64
	usage          openAIUsagePayload
	messageStarted bool
	textStarted    bool
	textBlock      int
	nextBlock      int
	tools          map[int]*openAIStreamToolState
	done           bool
}

func collectProviderCandidatesForRequest(snap *snapshot.Snapshot, model string) []providerCandidate {
	if snap == nil {
		return nil
	}
	candidates := make([]providerCandidate, 0, len(snap.Providers))
	for i := range snap.Providers {
		p := &snap.Providers[i]
		if !p.ExecutionPolicy.Enabled {
			continue
		}
		for _, m := range p.ModelTable {
			if m.PublicModel != model {
				continue
			}
			candidates = append(candidates, providerCandidate{
				provider:      p,
				upstreamModel: m.UpstreamModel,
				weight:        normalizeWeight(p.ExecutionPolicy.Weight),
			})
			break
		}
	}
	return candidates
}

func buildCompatPlan(
	clientFmt clientFormat,
	provider *snapshot.ProviderSnapshot,
	requestedModel string,
	upstreamModel string,
	body []byte,
) (compatPlan, error) {
	sanitizedBody := body
	switch clientFmt {
	case formatAnthropic:
		sanitizedBody = normalizeAnthropicRequestToolDescriptions(body)
	case formatChatCompletions, formatResponses:
		sanitizedBody = normalizeOpenAIRequestToolDescriptions(body)
	}

	plan := compatPlan{
		forwardPath:         "/v1/chat/completions",
		forwardBody:         sanitizedBody,
		upstreamIsAnthropic: false,
		responseMode:        responseCompatPassthrough,
		clientModel:         requestedModel,
	}
	if provider == nil {
		return plan, fmt.Errorf("provider is required")
	}

	adapter := providerProtocolAdapter(provider)

	// Responses API clients: convert to Chat Completions for upstream, then back.
	if clientFmt == formatResponses {
		customToolNames := extractResponsesCustomToolNames(sanitizedBody)
		converted, err := convertResponsesRequestToChat(sanitizedBody, upstreamModel)
		if err != nil {
			return compatPlan{}, err
		}
		plan.forwardPath = "/v1/chat/completions"
		plan.forwardBody = converted
		plan.upstreamIsAnthropic = false
		plan.responseMode = responseCompatChatToResponses
		plan.responsesCustomToolNames = customToolNames
		return plan, nil
	}

	if clientFmt == formatAnthropic {
		if adapter != core.ProtocolAdapterAnthropicMessages {
			converted, err := convertAnthropicRequestToOpenAIChat(sanitizedBody, upstreamModel)
			if err != nil {
				return compatPlan{}, err
			}
			plan.forwardPath = "/v1/chat/completions"
			plan.forwardBody = converted
			plan.upstreamIsAnthropic = false
			plan.responseMode = responseCompatOpenAIToAnthropic
			return plan, nil
		}
		plan.forwardPath = "/v1/messages"
		plan.upstreamIsAnthropic = true
		if upstreamModel != "" && upstreamModel != requestedModel {
			plan.forwardBody = rewriteModelInBody(sanitizedBody, requestedModel, upstreamModel)
		}
		return plan, nil
	}

	if adapter == core.ProtocolAdapterAnthropicMessages {
		converted, err := convertOpenAIChatRequestToAnthropic(sanitizedBody, upstreamModel)
		if err != nil {
			return compatPlan{}, err
		}
		plan.forwardPath = "/v1/messages"
		plan.forwardBody = converted
		plan.upstreamIsAnthropic = true
		plan.responseMode = responseCompatAnthropicToOpenAI
		return plan, nil
	}

	if upstreamModel != "" && upstreamModel != requestedModel {
		plan.forwardBody = rewriteModelInBody(sanitizedBody, requestedModel, upstreamModel)
	}
	return plan, nil
}

func normalizeOpenAIRequestToolDescriptions(body []byte) []byte {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	mutated := false

	if tools, ok := payload["tools"].([]any); ok {
		for _, toolValue := range tools {
			tool, ok := toolValue.(map[string]any)
			if !ok {
				continue
			}
			if normalizeOpenAIToolDescription(tool) {
				mutated = true
			}
		}
	}

	if functions, ok := payload["functions"].([]any); ok {
		for _, functionValue := range functions {
			function, ok := functionValue.(map[string]any)
			if !ok {
				continue
			}
			desc, exists := function["description"]
			normalized := normalizeToolDescription(desc)
			if !exists {
				function["description"] = normalized
				mutated = true
				continue
			}
			if cur, ok := desc.(string); !ok || cur != normalized {
				function["description"] = normalized
				mutated = true
			}
		}
	}

	if !mutated {
		return body
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return encoded
}

func normalizeOpenAIToolDescription(tool map[string]any) bool {
	if fn, ok := tool["function"].(map[string]any); ok {
		desc, exists := fn["description"]
		normalized := normalizeToolDescription(desc)
		if !exists {
			fn["description"] = normalized
			return true
		}
		if cur, ok := desc.(string); !ok || cur != normalized {
			fn["description"] = normalized
			return true
		}
		return false
	}

	if strings.TrimSpace(fmt.Sprint(tool["type"])) != "function" {
		return false
	}
	desc := tool["description"]
	normalized := normalizeToolDescription(desc)
	tool["function"] = map[string]any{
		"name":        tool["name"],
		"description": normalized,
		"parameters":  tool["parameters"],
	}
	delete(tool, "name")
	delete(tool, "description")
	delete(tool, "parameters")
	return true
}

func normalizeAnthropicRequestToolDescriptions(body []byte) []byte {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	tools, ok := payload["tools"].([]any)
	if !ok {
		return body
	}

	mutated := false
	for _, toolValue := range tools {
		tool, ok := toolValue.(map[string]any)
		if !ok {
			continue
		}
		desc, exists := tool["description"]
		normalized := normalizeToolDescription(desc)
		if !exists {
			tool["description"] = normalized
			mutated = true
			continue
		}
		if cur, ok := desc.(string); !ok || cur != normalized {
			tool["description"] = normalized
			mutated = true
		}
	}

	if !mutated {
		return body
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return encoded
}

func providerProtocolAdapter(provider *snapshot.ProviderSnapshot) string {
	if provider == nil {
		return core.ProtocolAdapterOpenAIChatCompletions
	}
	return core.NormalizeProtocolAdapter(provider.ProtocolAdapter, provider.AnthropicBaseURL)
}

func adaptResponseBodyForClient(plan compatPlan, statusCode int, respBody []byte) ([]byte, string, error) {
	if plan.responseMode == responseCompatChatToResponses {
		if statusCode >= http.StatusBadRequest {
			return respBody, "application/json", nil
		}
		converted, err := adaptChatResponseToResponses(respBody, plan.clientModel, plan.responsesCustomToolNames)
		if err != nil {
			return nil, "", err
		}
		return converted, "application/json", nil
	}
	if plan.responseMode == responseCompatOpenAIToAnthropic {
		if statusCode >= http.StatusBadRequest {
			return adaptOpenAIErrorToAnthropic(respBody), "application/json", nil
		}
		converted, err := adaptOpenAIResponseToAnthropic(respBody, plan.clientModel)
		if err != nil {
			return nil, "", err
		}
		return converted, "application/json", nil
	}
	if plan.responseMode != responseCompatAnthropicToOpenAI {
		return respBody, "", nil
	}
	if statusCode >= http.StatusBadRequest {
		return adaptAnthropicErrorToOpenAI(respBody), "application/json", nil
	}
	converted, err := adaptAnthropicResponseToOpenAI(respBody)
	if err != nil {
		return nil, "", err
	}
	return converted, "application/json", nil
}

func writeCompatStreamResponse(w http.ResponseWriter, statusCode int, contentType string, respBody io.ReadCloser, plan compatPlan) (promptTokens, cachedPromptTokens, completionTokens int64) {
	if plan.responseMode == responseCompatChatToResponses {
		return bridgeChatStreamToResponses(w, statusCode, respBody, plan.clientModel, plan.responsesCustomToolNames)
	}
	if plan.responseMode == responseCompatOpenAIToAnthropic {
		return bridgeOpenAIStreamToAnthropic(w, statusCode, respBody, plan.clientModel)
	}
	if plan.responseMode == responseCompatAnthropicToOpenAI {
		return bridgeAnthropicStreamToOpenAI(w, statusCode, respBody)
	}
	return handleStreamResponse(w, statusCode, contentType, respBody)
}

func writeCompatStreamResponseStarted(w http.ResponseWriter, flusher http.Flusher, respBody io.ReadCloser, plan compatPlan) (promptTokens, cachedPromptTokens, completionTokens int64) {
	if plan.responseMode == responseCompatChatToResponses {
		return bridgeChatStreamToResponsesStarted(w, flusher, respBody, plan.clientModel, plan.responsesCustomToolNames)
	}
	if plan.responseMode == responseCompatOpenAIToAnthropic {
		return bridgeOpenAIStreamToAnthropicStarted(w, flusher, respBody, plan.clientModel)
	}
	if plan.responseMode == responseCompatAnthropicToOpenAI {
		return bridgeAnthropicStreamToOpenAIStarted(w, flusher, respBody)
	}
	return handleStartedStreamResponse(w, respBody, flusher)
}

func convertAnthropicRequestToOpenAIChat(body []byte, upstreamModel string) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode anthropic request: %w", err)
	}

	out := make(map[string]any)
	copyRawField := func(from, to string) {
		if value, ok := raw[from]; ok {
			var decoded any
			if err := json.Unmarshal(value, &decoded); err == nil {
				out[to] = decoded
			}
		}
	}
	copyRawField("model", "model")
	copyRawField("max_tokens", "max_tokens")
	copyRawField("temperature", "temperature")
	copyRawField("top_p", "top_p")
	copyRawField("stream", "stream")
	copyRawField("user", "user")
	copyRawField("metadata", "metadata")
	copyRawField("stop_sequences", "stop")
	if strings.TrimSpace(upstreamModel) != "" {
		out["model"] = strings.TrimSpace(upstreamModel)
	}

	messages := make([]map[string]any, 0)
	if systemRaw, ok := raw["system"]; ok {
		if systemText := anthropicRawContentToText(systemRaw); strings.TrimSpace(systemText) != "" {
			messages = append(messages, map[string]any{
				"role":    "system",
				"content": systemText,
			})
		}
	}

	var anthropicMessages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw["messages"], &anthropicMessages); err != nil {
		return nil, fmt.Errorf("decode anthropic messages: %w", err)
	}
	for _, message := range anthropicMessages {
		switch message.Role {
		case "assistant":
			messages = append(messages, convertAnthropicAssistantMessageToOpenAI(message.Content))
		case "user":
			messages = append(messages, convertAnthropicUserMessageToOpenAI(message.Content)...)
		default:
			messages = append(messages, map[string]any{
				"role":    message.Role,
				"content": anthropicRawContentToText(message.Content),
			})
		}
	}
	out["messages"] = messages

	if toolsRaw, ok := raw["tools"]; ok {
		tools, err := convertAnthropicToolsToOpenAI(toolsRaw)
		if err != nil {
			return nil, err
		}
		if len(tools) > 0 {
			out["tools"] = tools
		}
	}
	if toolChoiceRaw, ok := raw["tool_choice"]; ok {
		if toolChoice := convertAnthropicToolChoiceToOpenAI(toolChoiceRaw); toolChoice != nil {
			out["tool_choice"] = toolChoice
		}
	}

	return json.Marshal(out)
}

func anthropicRawContentToText(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err == nil {
		parts := make([]string, 0, len(blocks))
		for _, block := range blocks {
			switch normalizeTextLikeBlockType(fmt.Sprint(block["type"])) {
			case "text":
				if text, ok := extractTextFromContentBlock(block); ok && strings.TrimSpace(text) != "" {
					parts = append(parts, text)
				}
			case "tool_result":
				if content, ok := block["content"]; ok {
					parts = append(parts, stringifyContentValue(content))
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	var value any
	if err := json.Unmarshal(raw, &value); err == nil {
		return stringifyContentValue(value)
	}
	return ""
}

func convertAnthropicAssistantMessageToOpenAI(raw json.RawMessage) map[string]any {
	message := map[string]any{
		"role":    "assistant",
		"content": nil,
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		message["content"] = text
		return message
	}
	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err != nil {
		message["content"] = anthropicRawContentToText(raw)
		return message
	}
	textParts := make([]string, 0, len(blocks))
	toolCalls := make([]map[string]any, 0)
	for i, block := range blocks {
		switch normalizeTextLikeBlockType(fmt.Sprint(block["type"])) {
		case "text":
			if text, ok := extractTextFromContentBlock(block); ok && strings.TrimSpace(text) != "" {
				textParts = append(textParts, text)
			}
		case "tool_use":
			id := strings.TrimSpace(fmt.Sprint(block["id"]))
			if id == "" {
				id = fmt.Sprintf("toolu_%d", i)
			}
			toolCalls = append(toolCalls, map[string]any{
				"id":   id,
				"type": "function",
				"function": map[string]any{
					"name":      strings.TrimSpace(fmt.Sprint(block["name"])),
					"arguments": compactJSONValue(block["input"]),
				},
			})
		}
	}
	if len(textParts) > 0 {
		message["content"] = strings.Join(textParts, "\n")
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	return message
}

func convertAnthropicUserMessageToOpenAI(raw json.RawMessage) []map[string]any {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return []map[string]any{{"role": "user", "content": text}}
	}
	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return []map[string]any{{"role": "user", "content": anthropicRawContentToText(raw)}}
	}
	var messages []map[string]any
	userParts := make([]map[string]any, 0, len(blocks))
	flushUserParts := func() {
		if len(userParts) == 0 {
			return
		}
		content := openAIContentFromUserParts(userParts)
		messages = append(messages, map[string]any{"role": "user", "content": content})
		userParts = userParts[:0]
	}
	for _, block := range blocks {
		switch normalizeTextLikeBlockType(fmt.Sprint(block["type"])) {
		case "text":
			if text, ok := extractTextFromContentBlock(block); ok && strings.TrimSpace(text) != "" {
				userParts = append(userParts, map[string]any{
					"type": "text",
					"text": text,
				})
			}
		case "image":
			if imagePart, ok := convertAnthropicImageBlockToOpenAI(block); ok {
				userParts = append(userParts, imagePart)
			}
		case "tool_result":
			flushUserParts()
			toolCallID := strings.TrimSpace(fmt.Sprint(block["tool_use_id"]))
			if toolCallID == "" {
				toolCallID = strings.TrimSpace(fmt.Sprint(block["id"]))
			}
			messages = append(messages, map[string]any{
				"role":         "tool",
				"tool_call_id": toolCallID,
				"content":      stringifyContentValue(block["content"]),
			})
		}
	}
	flushUserParts()
	if len(messages) == 0 {
		messages = append(messages, map[string]any{"role": "user", "content": ""})
	}
	return messages
}

func convertAnthropicToolsToOpenAI(raw json.RawMessage) ([]map[string]any, error) {
	var tools []map[string]any
	if err := json.Unmarshal(raw, &tools); err != nil {
		return nil, fmt.Errorf("decode anthropic tools: %w", err)
	}
	converted := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		converted = append(converted, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool["name"],
				"description": normalizeToolDescription(tool["description"]),
				"parameters":  tool["input_schema"],
			},
		})
	}
	return converted, nil
}

func convertAnthropicToolChoiceToOpenAI(raw json.RawMessage) any {
	var choice map[string]any
	if err := json.Unmarshal(raw, &choice); err != nil {
		return nil
	}
	switch strings.TrimSpace(fmt.Sprint(choice["type"])) {
	case "auto":
		return "auto"
	case "any":
		return "required"
	case "tool":
		name := strings.TrimSpace(fmt.Sprint(choice["name"]))
		if name == "" {
			return "required"
		}
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": name,
			},
		}
	case "none":
		return "none"
	default:
		return nil
	}
}

func stringifyContentValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		return compactJSONValue(v)
	}
}

func adaptOpenAIResponseToAnthropic(respBody []byte, clientModel string) ([]byte, error) {
	var payload struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content   any              `json:"content"`
				ToolCalls []openAIToolCall `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage openAIUsagePayload `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return nil, fmt.Errorf("decode openai response: %w", err)
	}
	model := strings.TrimSpace(clientModel)
	if model == "" {
		model = payload.Model
	}
	content := make([]anthropicContentBlock, 0)
	stopReason := "end_turn"
	if len(payload.Choices) > 0 {
		choice := payload.Choices[0]
		if text := strings.TrimSpace(stringifyContentValue(choice.Message.Content)); text != "" {
			content = append(content, anthropicContentBlock{Type: "text", Text: text})
		}
		for _, toolCall := range choice.Message.ToolCalls {
			input := parseJSONOrString(toolCall.Function.Arguments)
			content = append(content, anthropicContentBlock{
				Type:  "tool_use",
				ID:    toolCall.ID,
				Name:  toolCall.Function.Name,
				Input: input,
			})
		}
		stopReason = mapOpenAIFinishReasonToAnthropic(choice.FinishReason)
	}
	if len(content) == 0 {
		content = append(content, anthropicContentBlock{Type: "text", Text: ""})
	}
	resp := anthropicResponsePayload{
		ID:         payload.ID,
		Type:       "message",
		Role:       "assistant",
		Model:      model,
		Content:    content,
		StopReason: stopReason,
		Usage: anthropicUsage{
			InputTokens:          payload.Usage.PromptTokens,
			OutputTokens:         payload.Usage.CompletionTokens,
			CacheReadInputTokens: payload.Usage.PromptTokensDetails.CachedTokens,
		},
	}
	if resp.ID == "" {
		resp.ID = "msg_" + uuid.NewString()
	}
	return json.Marshal(resp)
}

func adaptOpenAIErrorToAnthropic(respBody []byte) []byte {
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    any    `json:"code"`
		} `json:"error"`
		Message string `json:"message"`
		Type    string `json:"type"`
	}
	message := strings.TrimSpace(string(respBody))
	errorType := "api_error"
	if err := json.Unmarshal(respBody, &payload); err == nil {
		if payload.Error.Message != "" {
			message = payload.Error.Message
		} else if payload.Message != "" {
			message = payload.Message
		}
		if payload.Error.Type != "" {
			errorType = payload.Error.Type
		} else if payload.Type != "" {
			errorType = payload.Type
		}
	}
	if message == "" {
		message = "upstream request failed"
	}
	converted, err := json.Marshal(map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    errorType,
			"message": message,
		},
	})
	if err != nil {
		return respBody
	}
	return converted
}

func parseJSONOrString(value string) any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return map[string]any{}
	}
	var parsed any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
		return parsed
	}
	return trimmed
}

func mapOpenAIFinishReasonToAnthropic(reason string) string {
	switch strings.TrimSpace(reason) {
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	default:
		return "end_turn"
	}
}

func adaptAnthropicResponseToOpenAI(respBody []byte) ([]byte, error) {
	var payload anthropicResponsePayload
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return nil, fmt.Errorf("decode anthropic response: %w", err)
	}

	promptTokens, cachedTokens, completionTokens := anthropicUsageToOpenAITokens(payload.Usage)
	resp := map[string]any{
		"id":      payload.ID,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   payload.Model,
		"choices": []map[string]any{
			{
				"index":         0,
				"message":       anthropicBlocksToOpenAIMessage(payload.Content),
				"finish_reason": mapAnthropicStopReason(payload.StopReason),
			},
		},
	}
	if promptTokens > 0 || completionTokens > 0 || cachedTokens > 0 {
		resp["usage"] = openAIUsageMap(promptTokens, cachedTokens, completionTokens)
	}

	converted, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("encode openai response: %w", err)
	}
	return converted, nil
}

func adaptAnthropicErrorToOpenAI(respBody []byte) []byte {
	if len(respBody) == 0 {
		return []byte(`{"error":{"message":"upstream request failed"}}`)
	}

	var payload struct {
		Type  string `json:"type,omitempty"`
		Error struct {
			Type    string `json:"type,omitempty"`
			Message string `json:"message,omitempty"`
			Code    any    `json:"code,omitempty"`
			Param   any    `json:"param,omitempty"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return respBody
	}

	message := strings.TrimSpace(payload.Error.Message)
	if message == "" {
		message = extractErrorMessage(respBody, nil)
	}
	body := map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    payload.Error.Type,
			"code":    payload.Error.Code,
			"param":   payload.Error.Param,
		},
	}
	converted, err := json.Marshal(body)
	if err != nil {
		return respBody
	}
	return converted
}

func bridgeAnthropicStreamToOpenAI(w http.ResponseWriter, statusCode int, respBody io.ReadCloser) (promptTokens, cachedPromptTokens, completionTokens int64) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(statusCode)

	flusher, _ := w.(http.Flusher)
	return bridgeAnthropicStreamToOpenAIStarted(w, flusher, respBody)
}

func bridgeAnthropicStreamToOpenAIStarted(w http.ResponseWriter, flusher http.Flusher, respBody io.ReadCloser) (promptTokens, cachedPromptTokens, completionTokens int64) {
	if respBody == nil {
		return 0, 0, 0
	}
	defer respBody.Close()

	reader := bufio.NewReader(respBody)
	var eventData bytes.Buffer
	state := anthropicStreamState{created: time.Now().Unix()}

	flush := func() {
		if flusher != nil {
			flusher.Flush()
		}
	}

	emit := func(payload []byte) {
		if len(payload) == 0 {
			return
		}
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(payload)
		_, _ = w.Write([]byte("\n\n"))
		flush()
	}

	emitDone := func() {
		if state.done {
			return
		}
		state.done = true
		if usageChunk := state.usageChunk(); usageChunk != nil {
			emit(usageChunk)
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flush()
	}

	handleEvent := func(data []byte) {
		payloads, done := translateAnthropicEventToOpenAI(data, &state)
		for _, payload := range payloads {
			emit(payload)
		}
		if done {
			emitDone()
		}
	}

	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			trimmedLine := strings.TrimRight(string(line), "\r\n")
			if strings.TrimSpace(trimmedLine) == "" {
				if eventData.Len() > 0 {
					handleEvent(eventData.Bytes())
					eventData.Reset()
				}
			} else if strings.HasPrefix(trimmedLine, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(trimmedLine, "data:"))
				if eventData.Len() > 0 {
					eventData.WriteByte('\n')
				}
				eventData.WriteString(data)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				if eventData.Len() > 0 {
					handleEvent(eventData.Bytes())
				}
				emitDone()
			}
			break
		}
	}

	promptTokens, cachedPromptTokens, completionTokens = state.usage.tokenTriplet()
	return promptTokens, cachedPromptTokens, completionTokens
}

func bridgeOpenAIStreamToAnthropic(w http.ResponseWriter, statusCode int, respBody io.ReadCloser, clientModel string) (promptTokens, cachedPromptTokens, completionTokens int64) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(statusCode)

	flusher, _ := w.(http.Flusher)
	return bridgeOpenAIStreamToAnthropicStarted(w, flusher, respBody, clientModel)
}

func bridgeOpenAIStreamToAnthropicStarted(w http.ResponseWriter, flusher http.Flusher, respBody io.ReadCloser, clientModel string) (promptTokens, cachedPromptTokens, completionTokens int64) {
	if respBody == nil {
		return 0, 0, 0
	}
	defer respBody.Close()

	reader := bufio.NewReader(respBody)
	var eventData bytes.Buffer
	state := openAIToAnthropicStreamState{
		model:   strings.TrimSpace(clientModel),
		created: time.Now().Unix(),
	}

	flush := func() {
		if flusher != nil {
			flusher.Flush()
		}
	}

	emit := func(event string, payload []byte) {
		if len(payload) == 0 {
			return
		}
		if event != "" {
			_, _ = w.Write([]byte("event: "))
			_, _ = w.Write([]byte(event))
			_, _ = w.Write([]byte("\n"))
		}
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(payload)
		_, _ = w.Write([]byte("\n\n"))
		flush()
	}

	emitPayloads := func(payloads []anthropicStreamPayload) {
		for _, payload := range payloads {
			emit(payload.event, payload.data)
		}
	}

	handleEvent := func(data []byte) {
		payloads, done := translateOpenAIEventToAnthropic(data, &state)
		emitPayloads(payloads)
		if done {
			emitPayloads(finalizeOpenAIToAnthropicStream(&state, "end_turn"))
		}
	}

	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			trimmedLine := strings.TrimRight(string(line), "\r\n")
			if strings.TrimSpace(trimmedLine) == "" {
				if eventData.Len() > 0 {
					handleEvent(eventData.Bytes())
					eventData.Reset()
				}
			} else if strings.HasPrefix(trimmedLine, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(trimmedLine, "data:"))
				if eventData.Len() > 0 {
					eventData.WriteByte('\n')
				}
				eventData.WriteString(data)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				if eventData.Len() > 0 {
					handleEvent(eventData.Bytes())
				}
				emitPayloads(finalizeOpenAIToAnthropicStream(&state, "end_turn"))
			}
			break
		}
	}

	return state.usage.PromptTokens, openAIUsageCachedTokens(state.usage), state.usage.CompletionTokens
}

type anthropicStreamPayload struct {
	event string
	data  []byte
}

func translateOpenAIEventToAnthropic(data []byte, state *openAIToAnthropicStreamState) ([]anthropicStreamPayload, bool) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, false
	}
	if trimmed == "[DONE]" {
		return nil, true
	}

	var chunk struct {
		ID      string               `json:"id"`
		Model   string               `json:"model"`
		Created int64                `json:"created"`
		Choices []openAIStreamChoice `json:"choices"`
		Usage   openAIUsagePayload   `json:"usage"`
		Error   struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(trimmed), &chunk); err != nil {
		return nil, false
	}
	if chunk.Error.Message != "" {
		payload, _ := json.Marshal(map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    firstNonEmpty(chunk.Error.Type, "api_error"),
				"message": chunk.Error.Message,
			},
		})
		return []anthropicStreamPayload{{event: "error", data: payload}}, true
	}
	if chunk.ID != "" {
		state.id = chunk.ID
	}
	if state.model == "" && chunk.Model != "" {
		state.model = chunk.Model
	}
	if chunk.Created > 0 {
		state.created = chunk.Created
	}
	state.mergeUsage(chunk.Usage)

	payloads := ensureOpenAIToAnthropicMessageStarted(state)
	for _, choice := range chunk.Choices {
		if choice.Delta.Content != "" {
			payloads = append(payloads, openAITextDeltaToAnthropic(choice.Delta.Content, state)...)
		}
		for _, toolDelta := range choice.Delta.ToolCalls {
			payloads = append(payloads, openAIToolDeltaToAnthropic(toolDelta, state)...)
		}
		if choice.FinishReason != "" {
			payloads = append(payloads, finalizeOpenAIToAnthropicStream(state, mapOpenAIFinishReasonToAnthropic(choice.FinishReason))...)
		}
	}
	return payloads, false
}

type openAIStreamChoice struct {
	Delta struct {
		Role      string                  `json:"role"`
		Content   string                  `json:"content"`
		ToolCalls []openAIStreamToolDelta `json:"tool_calls"`
	} `json:"delta"`
	FinishReason string `json:"finish_reason"`
}

type openAIStreamToolDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func ensureOpenAIToAnthropicMessageStarted(state *openAIToAnthropicStreamState) []anthropicStreamPayload {
	if state.messageStarted {
		return nil
	}
	state.messageStarted = true
	if state.id == "" {
		state.id = "msg_" + uuid.NewString()
	}
	if state.model == "" {
		state.model = "unknown"
	}
	payload, _ := json.Marshal(map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            state.id,
			"type":          "message",
			"role":          "assistant",
			"model":         state.model,
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":  0,
				"output_tokens": 0,
			},
		},
	})
	return []anthropicStreamPayload{{event: "message_start", data: payload}}
}

func openAITextDeltaToAnthropic(text string, state *openAIToAnthropicStreamState) []anthropicStreamPayload {
	payloads := make([]anthropicStreamPayload, 0, 2)
	if !state.textStarted {
		state.textStarted = true
		state.textBlock = state.nextBlock
		state.nextBlock++
		startPayload, _ := json.Marshal(map[string]any{
			"type":  "content_block_start",
			"index": state.textBlock,
			"content_block": map[string]any{
				"type": "text",
				"text": "",
			},
		})
		payloads = append(payloads, anthropicStreamPayload{event: "content_block_start", data: startPayload})
	}
	deltaPayload, _ := json.Marshal(map[string]any{
		"type":  "content_block_delta",
		"index": state.textBlock,
		"delta": map[string]any{
			"type": "text_delta",
			"text": text,
		},
	})
	payloads = append(payloads, anthropicStreamPayload{event: "content_block_delta", data: deltaPayload})
	return payloads
}

func openAIToolDeltaToAnthropic(delta openAIStreamToolDelta, state *openAIToAnthropicStreamState) []anthropicStreamPayload {
	if state.tools == nil {
		state.tools = make(map[int]*openAIStreamToolState)
	}
	tool := state.tools[delta.Index]
	if tool == nil {
		tool = &openAIStreamToolState{block: state.nextBlock}
		state.nextBlock++
		state.tools[delta.Index] = tool
	}
	if strings.TrimSpace(delta.ID) != "" {
		tool.id = strings.TrimSpace(delta.ID)
	}
	if strings.TrimSpace(delta.Function.Name) != "" {
		tool.name = strings.TrimSpace(delta.Function.Name)
	}
	if tool.id == "" {
		tool.id = fmt.Sprintf("toolu_%d", delta.Index)
	}
	payloads := make([]anthropicStreamPayload, 0, 2)
	if !tool.started {
		tool.started = true
		startPayload, _ := json.Marshal(map[string]any{
			"type":  "content_block_start",
			"index": tool.block,
			"content_block": map[string]any{
				"type":  "tool_use",
				"id":    tool.id,
				"name":  tool.name,
				"input": map[string]any{},
			},
		})
		payloads = append(payloads, anthropicStreamPayload{event: "content_block_start", data: startPayload})
	}
	if delta.Function.Arguments != "" {
		tool.arguments.WriteString(delta.Function.Arguments)
		deltaPayload, _ := json.Marshal(map[string]any{
			"type":  "content_block_delta",
			"index": tool.block,
			"delta": map[string]any{
				"type":         "input_json_delta",
				"partial_json": delta.Function.Arguments,
			},
		})
		payloads = append(payloads, anthropicStreamPayload{event: "content_block_delta", data: deltaPayload})
	}
	return payloads
}

func finalizeOpenAIToAnthropicStream(state *openAIToAnthropicStreamState, stopReason string) []anthropicStreamPayload {
	if state.done {
		return nil
	}
	state.done = true
	payloads := make([]anthropicStreamPayload, 0)
	if state.textStarted {
		payload, _ := json.Marshal(map[string]any{
			"type":  "content_block_stop",
			"index": state.textBlock,
		})
		payloads = append(payloads, anthropicStreamPayload{event: "content_block_stop", data: payload})
	}
	if len(state.tools) > 0 {
		for _, tool := range state.tools {
			if !tool.started {
				continue
			}
			payload, _ := json.Marshal(map[string]any{
				"type":  "content_block_stop",
				"index": tool.block,
			})
			payloads = append(payloads, anthropicStreamPayload{event: "content_block_stop", data: payload})
		}
	}
	deltaPayload, _ := json.Marshal(map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   firstNonEmpty(stopReason, "end_turn"),
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"output_tokens": state.usage.CompletionTokens,
		},
	})
	payloads = append(payloads, anthropicStreamPayload{event: "message_delta", data: deltaPayload})
	stopPayload, _ := json.Marshal(map[string]any{"type": "message_stop"})
	payloads = append(payloads, anthropicStreamPayload{event: "message_stop", data: stopPayload})
	return payloads
}

func (s *openAIToAnthropicStreamState) mergeUsage(usage openAIUsagePayload) {
	if usage.PromptTokens > s.usage.PromptTokens {
		s.usage.PromptTokens = usage.PromptTokens
	}
	if usage.CompletionTokens > s.usage.CompletionTokens {
		s.usage.CompletionTokens = usage.CompletionTokens
	}
	if usage.TotalTokens > s.usage.TotalTokens {
		s.usage.TotalTokens = usage.TotalTokens
	}
	if cachedTokens := openAIUsageCachedTokens(usage); cachedTokens > openAIUsageCachedTokens(s.usage) {
		s.usage.PromptTokensDetails.CachedTokens = cachedTokens
		s.usage.InputTokensDetails.CachedTokens = cachedTokens
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func translateAnthropicEventToOpenAI(data []byte, state *anthropicStreamState) ([][]byte, bool) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "[DONE]" {
		return nil, false
	}

	var payload anthropicSSEEvent
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil, false
	}

	state.mergeUsage(payload.Usage)

	switch payload.Type {
	case "message_start":
		if payload.Message.ID != "" {
			state.id = payload.Message.ID
		}
		if payload.Message.Model != "" {
			state.model = payload.Message.Model
		}
		state.mergeUsage(payload.Message.Usage)
		chunk, err := marshalOpenAIChunk(map[string]any{
			"id":      state.id,
			"object":  "chat.completion.chunk",
			"created": state.created,
			"model":   state.model,
			"choices": []map[string]any{
				{
					"index": 0,
					"delta": map[string]any{
						"role": "assistant",
					},
				},
			},
		})
		if err != nil {
			return nil, false
		}
		return [][]byte{chunk}, false
	case "content_block_start":
		if payload.ContentBlock.Type != "tool_use" {
			return nil, false
		}
		toolCall := state.ensureToolCall(payload.Index, payload.ContentBlock.ID, payload.ContentBlock.Name)
		arguments := initialAnthropicToolArguments(payload.ContentBlock.Input)
		if arguments != "" && arguments != "{}" {
			toolCall.Arguments.Reset()
			toolCall.Arguments.WriteString(arguments)
		}
		chunk, err := marshalOpenAIChunk(map[string]any{
			"id":      state.id,
			"object":  "chat.completion.chunk",
			"created": state.created,
			"model":   state.model,
			"choices": []map[string]any{
				{
					"index": 0,
					"delta": map[string]any{
						"tool_calls": []map[string]any{
							openAIToolCallDelta(toolCall.OpenAIIndex, toolCall.ID, toolCall.Name, arguments),
						},
					},
				},
			},
		})
		if err != nil {
			return nil, false
		}
		return [][]byte{chunk}, false
	case "content_block_delta":
		if payload.Delta.Type == "input_json_delta" {
			if strings.TrimSpace(payload.Delta.PartialJSON) == "" {
				return nil, false
			}
			toolCall := state.ensureToolCall(payload.Index, "", "")
			toolCall.Arguments.WriteString(payload.Delta.PartialJSON)
			chunk, err := marshalOpenAIChunk(map[string]any{
				"id":      state.id,
				"object":  "chat.completion.chunk",
				"created": state.created,
				"model":   state.model,
				"choices": []map[string]any{
					{
						"index": 0,
						"delta": map[string]any{
							"tool_calls": []map[string]any{
								openAIToolCallDelta(toolCall.OpenAIIndex, "", "", payload.Delta.PartialJSON),
							},
						},
					},
				},
			})
			if err != nil {
				return nil, false
			}
			return [][]byte{chunk}, false
		}
		if payload.Delta.Text == "" {
			return nil, false
		}
		chunk, err := marshalOpenAIChunk(map[string]any{
			"id":      state.id,
			"object":  "chat.completion.chunk",
			"created": state.created,
			"model":   state.model,
			"choices": []map[string]any{
				{
					"index": 0,
					"delta": map[string]any{
						"content": payload.Delta.Text,
					},
				},
			},
		})
		if err != nil {
			return nil, false
		}
		return [][]byte{chunk}, false
	case "message_delta":
		if payload.Delta.StopReason != "" {
			state.stopReason = payload.Delta.StopReason
			chunk, err := marshalOpenAIChunk(map[string]any{
				"id":      state.id,
				"object":  "chat.completion.chunk",
				"created": state.created,
				"model":   state.model,
				"choices": []map[string]any{
					{
						"index":         0,
						"finish_reason": mapAnthropicStopReason(payload.Delta.StopReason),
					},
				},
			})
			if err != nil {
				return nil, false
			}
			return [][]byte{chunk}, false
		}
	case "message_stop":
		return nil, true
	case "error":
		if payload.Error.Message == "" {
			return nil, true
		}
		chunk, err := marshalOpenAIChunk(map[string]any{
			"error": map[string]any{
				"message": payload.Error.Message,
				"type":    payload.Error.Type,
			},
		})
		if err != nil {
			return nil, true
		}
		return [][]byte{chunk}, true
	}
	return nil, false
}

func (s *anthropicStreamState) mergeUsage(usage anthropicUsage) {
	if usage.InputTokens > s.usage.InputTokens {
		s.usage.InputTokens = usage.InputTokens
	}
	if usage.OutputTokens > s.usage.OutputTokens {
		s.usage.OutputTokens = usage.OutputTokens
	}
	if usage.CacheReadInputTokens > s.usage.CacheReadInputTokens {
		s.usage.CacheReadInputTokens = usage.CacheReadInputTokens
	}
	if usage.CacheCreationTokens > s.usage.CacheCreationTokens {
		s.usage.CacheCreationTokens = usage.CacheCreationTokens
	}
}

func (s *anthropicStreamState) ensureToolCall(index int, id, name string) *anthropicStreamToolCall {
	if s.toolCalls == nil {
		s.toolCalls = make(map[int]*anthropicStreamToolCall)
	}
	toolCall, ok := s.toolCalls[index]
	if !ok {
		toolCall = &anthropicStreamToolCall{
			OpenAIIndex: s.nextToolIndex,
		}
		s.nextToolIndex++
		s.toolCalls[index] = toolCall
	}
	if strings.TrimSpace(id) != "" {
		toolCall.ID = strings.TrimSpace(id)
	}
	if strings.TrimSpace(name) != "" {
		toolCall.Name = strings.TrimSpace(name)
	}
	if toolCall.ID == "" {
		toolCall.ID = fmt.Sprintf("tool_%d", index)
	}
	return toolCall
}

func (s anthropicStreamState) usageChunk() []byte {
	promptTokens, cachedTokens, completionTokens := s.usage.tokenTriplet()
	if promptTokens == 0 && cachedTokens == 0 && completionTokens == 0 {
		return nil
	}
	chunk, err := marshalOpenAIChunk(map[string]any{
		"id":      s.id,
		"object":  "chat.completion.chunk",
		"created": s.created,
		"model":   s.model,
		"choices": []map[string]any{},
		"usage":   openAIUsageMap(promptTokens, cachedTokens, completionTokens),
	})
	if err != nil {
		return nil
	}
	return chunk
}

func (u anthropicUsage) tokenTriplet() (promptTokens, cachedPromptTokens, completionTokens int64) {
	promptTokens = u.InputTokens + u.CacheCreationTokens + u.CacheReadInputTokens
	cachedPromptTokens = u.CacheReadInputTokens
	completionTokens = u.OutputTokens
	if promptTokens < cachedPromptTokens {
		promptTokens = cachedPromptTokens
	}
	return promptTokens, cachedPromptTokens, completionTokens
}

func anthropicUsageToOpenAITokens(usage anthropicUsage) (promptTokens, cachedPromptTokens, completionTokens int64) {
	return usage.tokenTriplet()
}

func openAIUsageMap(promptTokens, cachedTokens, completionTokens int64) map[string]any {
	return map[string]any{
		"prompt_tokens":     promptTokens,
		"completion_tokens": completionTokens,
		"total_tokens":      promptTokens + completionTokens,
		"prompt_tokens_details": map[string]any{
			"cached_tokens": cachedTokens,
		},
	}
}

func openAIUsageCachedTokens(usage openAIUsagePayload) int64 {
	if usage.PromptTokensDetails.CachedTokens > 0 {
		return usage.PromptTokensDetails.CachedTokens
	}
	return usage.InputTokensDetails.CachedTokens
}

func responsesUsageMap(inputTokens, cachedInputTokens, outputTokens, totalTokens int64) map[string]any {
	if inputTokens < cachedInputTokens {
		inputTokens = cachedInputTokens
	}
	if totalTokens == 0 || totalTokens < inputTokens+outputTokens {
		totalTokens = inputTokens + outputTokens
	}
	usage := map[string]any{
		"input_tokens":  inputTokens,
		"output_tokens": outputTokens,
		"total_tokens":  totalTokens,
	}
	if cachedInputTokens > 0 {
		usage["input_tokens_details"] = map[string]any{
			"cached_tokens": cachedInputTokens,
		}
	}
	return usage
}

func mapAnthropicStopReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		return "stop"
	}
}

func anthropicBlocksToText(blocks []anthropicContentBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func anthropicBlocksToOpenAIMessage(blocks []anthropicContentBlock) map[string]any {
	message := map[string]any{
		"role":    "assistant",
		"content": nil,
	}
	if text := anthropicBlocksToText(blocks); text != "" {
		message["content"] = text
	}
	if toolCalls := anthropicBlocksToOpenAIToolCalls(blocks); len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	return message
}

func anthropicBlocksToOpenAIToolCalls(blocks []anthropicContentBlock) []map[string]any {
	toolCalls := make([]map[string]any, 0, len(blocks))
	for i, block := range blocks {
		if block.Type != "tool_use" {
			continue
		}
		toolID := strings.TrimSpace(block.ID)
		if toolID == "" {
			toolID = fmt.Sprintf("tool_%d", i)
		}
		toolCalls = append(toolCalls, map[string]any{
			"id":   toolID,
			"type": "function",
			"function": map[string]any{
				"name":      strings.TrimSpace(block.Name),
				"arguments": compactJSONValue(block.Input),
			},
		})
	}
	return toolCalls
}

func marshalOpenAIChunk(payload map[string]any) ([]byte, error) {
	return json.Marshal(payload)
}

func convertOpenAIChatRequestToAnthropic(body []byte, upstreamModel string) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode openai request: %w", err)
	}

	var messages []openAIChatMessage
	if err := json.Unmarshal(raw["messages"], &messages); err != nil {
		return nil, fmt.Errorf("decode openai messages: %w", err)
	}

	systemParts := make([]string, 0, 1)
	anthropicMessages := make([]anthropicMessage, 0, len(messages))
	for _, message := range messages {
		switch strings.TrimSpace(message.Role) {
		case "system", "developer":
			if text := extractOpenAISystemText(message.Content); text != "" {
				systemParts = append(systemParts, text)
			}
		case "user":
			content, err := convertOpenAIContentToAnthropic(message.Content)
			if err != nil {
				return nil, err
			}
			anthropicMessages = append(anthropicMessages, anthropicMessage{
				Role:    "user",
				Content: content,
			})
		case "assistant":
			content, err := convertOpenAIAssistantMessageToAnthropic(message)
			if err != nil {
				return nil, err
			}
			anthropicMessages = append(anthropicMessages, anthropicMessage{
				Role:    "assistant",
				Content: content,
			})
		case "tool":
			toolMessage, err := convertOpenAIToolMessageToAnthropic(message)
			if err != nil {
				return nil, err
			}
			anthropicMessages = append(anthropicMessages, toolMessage)
		case "function":
			functionResultMessage, err := convertOpenAIFunctionMessageToAnthropic(message)
			if err != nil {
				return nil, err
			}
			anthropicMessages = append(anthropicMessages, functionResultMessage)
		}
	}

	converted := map[string]any{
		"model":    upstreamModel,
		"messages": anthropicMessages,
	}

	if len(systemParts) > 0 {
		converted["system"] = strings.Join(systemParts, "\n\n")
	}
	if _, ok := raw["max_tokens"]; ok {
		var maxTokens any
		if err := json.Unmarshal(raw["max_tokens"], &maxTokens); err == nil {
			converted["max_tokens"] = maxTokens
		}
	}
	if _, ok := converted["max_tokens"]; !ok {
		converted["max_tokens"] = 4096
	}
	if _, ok := raw["temperature"]; ok {
		var temperature any
		if err := json.Unmarshal(raw["temperature"], &temperature); err == nil {
			converted["temperature"] = temperature
		}
	}
	if _, ok := raw["stream"]; ok {
		var stream any
		if err := json.Unmarshal(raw["stream"], &stream); err == nil {
			converted["stream"] = stream
		}
	}
	if _, ok := raw["tool_choice"]; ok {
		var toolChoice any
		if err := json.Unmarshal(raw["tool_choice"], &toolChoice); err == nil {
			if convertedChoice := convertOpenAIToolChoiceToAnthropic(toolChoice); convertedChoice != nil {
				converted["tool_choice"] = convertedChoice
			}
		}
	}
	if _, ok := raw["tools"]; ok {
		tools, err := convertOpenAIToolsToAnthropic(raw["tools"])
		if err != nil {
			return nil, err
		}
		if len(tools) > 0 {
			converted["tools"] = tools
		}
	} else if _, ok := raw["functions"]; ok {
		tools, err := convertOpenAIFunctionsToAnthropic(raw["functions"])
		if err != nil {
			return nil, err
		}
		if len(tools) > 0 {
			converted["tools"] = tools
		}
	}

	result, err := json.Marshal(converted)
	if err != nil {
		return nil, fmt.Errorf("encode anthropic request: %w", err)
	}
	return result, nil
}

func convertOpenAIContentToAnthropic(raw json.RawMessage) (interface{}, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", nil
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return nil, fmt.Errorf("decode openai content: %w", err)
		}
		return text, nil
	}

	var blocks []map[string]any
	if err := json.Unmarshal(trimmed, &blocks); err == nil {
		converted := make([]map[string]any, 0, len(blocks))
		for _, block := range blocks {
			switch normalizeTextLikeBlockType(fmt.Sprint(block["type"])) {
			case "text":
				text, ok := extractTextFromContentBlock(block)
				if !ok {
					continue
				}
				converted = append(converted, map[string]any{
					"type": "text",
					"text": text,
				})
				continue
			case "image_url":
				imageBlock, err := convertOpenAIImageURLBlockToAnthropic(block)
				if err != nil {
					return nil, err
				}
				converted = append(converted, imageBlock)
				continue
			}
			converted = append(converted, block)
		}
		return converted, nil
	}

	var value any
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return nil, fmt.Errorf("decode openai content: %w", err)
	}
	return value, nil
}

func convertOpenAIAssistantMessageToAnthropic(message openAIChatMessage) (interface{}, error) {
	content, err := convertOpenAIContentToAnthropic(message.Content)
	if err != nil {
		return nil, err
	}

	toolCalls := append([]openAIToolCall(nil), message.ToolCalls...)
	if len(toolCalls) == 0 && message.FunctionCall != nil && strings.TrimSpace(message.FunctionCall.Name) != "" {
		toolCalls = append(toolCalls, openAIToolCall{
			ID:   fmt.Sprintf("function_%s", strings.TrimSpace(message.FunctionCall.Name)),
			Type: "function",
			Function: openAIToolCallFunction{
				Name:      message.FunctionCall.Name,
				Arguments: message.FunctionCall.Arguments,
			},
		})
	}
	if len(toolCalls) == 0 {
		return content, nil
	}
	blocks, err := anthropicBlocksFromContentValue(content)
	if err != nil {
		return nil, err
	}
	for i, toolCall := range toolCalls {
		block, err := openAIToolCallToAnthropicBlock(toolCall, i)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	if len(blocks) == 0 {
		return "", nil
	}
	return blocks, nil
}

func convertOpenAIToolMessageToAnthropic(message openAIChatMessage) (anthropicMessage, error) {
	toolUseID := strings.TrimSpace(message.ToolCallID)
	if toolUseID == "" {
		return anthropicMessage{}, fmt.Errorf("openai tool message missing tool_call_id")
	}
	content, err := decodeOpenAIToolResultContent(message.Content)
	if err != nil {
		return anthropicMessage{}, err
	}
	return anthropicMessage{
		Role: "user",
		Content: []map[string]any{
			{
				"type":        "tool_result",
				"tool_use_id": toolUseID,
				"content":     content,
			},
		},
	}, nil
}

func convertOpenAIFunctionMessageToAnthropic(message openAIChatMessage) (anthropicMessage, error) {
	functionName := strings.TrimSpace(message.Name)
	if functionName == "" {
		return anthropicMessage{}, fmt.Errorf("openai function message missing name")
	}
	content, err := decodeOpenAIToolResultContent(message.Content)
	if err != nil {
		return anthropicMessage{}, err
	}
	return anthropicMessage{
		Role: "user",
		Content: []map[string]any{
			{
				"type":        "tool_result",
				"tool_use_id": fmt.Sprintf("function_%s", functionName),
				"content":     content,
			},
		},
	}, nil
}

func anthropicBlocksFromContentValue(content any) ([]map[string]any, error) {
	switch typed := content.(type) {
	case nil:
		return nil, nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil, nil
		}
		return []map[string]any{{"type": "text", "text": typed}}, nil
	case []map[string]any:
		blocks := make([]map[string]any, 0, len(typed))
		for _, block := range typed {
			if len(block) == 0 {
				continue
			}
			blocks = append(blocks, cloneStringAnyMap(block))
		}
		return blocks, nil
	case []any:
		blocks := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if block, ok := item.(map[string]any); ok {
				blocks = append(blocks, cloneStringAnyMap(block))
				continue
			}
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": text})
			}
		}
		return blocks, nil
	default:
		return nil, fmt.Errorf("unsupported assistant content type %T", content)
	}
}

func openAIToolCallToAnthropicBlock(toolCall openAIToolCall, index int) (map[string]any, error) {
	name := strings.TrimSpace(toolCall.Function.Name)
	if name == "" {
		return nil, fmt.Errorf("openai tool call missing function name")
	}
	toolID := strings.TrimSpace(toolCall.ID)
	if toolID == "" {
		toolID = fmt.Sprintf("tool_%d", index)
	}
	input, err := decodeOpenAIToolArguments(toolCall.Function.Arguments)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"type":  "tool_use",
		"id":    toolID,
		"name":  name,
		"input": input,
	}, nil
}

func decodeOpenAIToolArguments(arguments string) (any, error) {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		return map[string]any{}, nil
	}
	var value any
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return nil, fmt.Errorf("decode openai tool arguments: %w", err)
	}
	return value, nil
}

func decodeOpenAIToolResultContent(raw json.RawMessage) (any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", nil
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return nil, fmt.Errorf("decode openai tool result: %w", err)
		}
		var nested any
		if err := json.Unmarshal([]byte(text), &nested); err == nil {
			return nested, nil
		}
		return text, nil
	}
	var value any
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return nil, fmt.Errorf("decode openai tool result: %w", err)
	}
	return value, nil
}

func convertOpenAIToolChoiceToAnthropic(value any) any {
	switch typed := value.(type) {
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "", "auto":
			return map[string]any{"type": "auto"}
		case "required":
			return map[string]any{"type": "any"}
		case "none":
			return nil
		}
	case map[string]any:
		choiceType := strings.ToLower(strings.TrimSpace(fmt.Sprint(typed["type"])))
		switch choiceType {
		case "function":
			if fn, ok := typed["function"].(map[string]any); ok {
				if name := strings.TrimSpace(fmt.Sprint(fn["name"])); name != "" {
					return map[string]any{"type": "tool", "name": name}
				}
			}
		case "auto", "required", "none":
			return convertOpenAIToolChoiceToAnthropic(choiceType)
		}
	}
	return nil
}

func compactJSONValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return "{}"
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return "{}"
		}
		return trimmed
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return "{}"
		}
		return string(data)
	}
}

func cloneStringAnyMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func initialAnthropicToolArguments(value any) string {
	arguments := compactJSONValue(value)
	if arguments == "null" || arguments == "{}" {
		return ""
	}
	return arguments
}

func openAIToolCallDelta(index int, toolID, toolName, arguments string) map[string]any {
	function := map[string]any{
		"arguments": arguments,
	}
	if strings.TrimSpace(toolName) != "" {
		function["name"] = toolName
	}
	delta := map[string]any{
		"index":    index,
		"function": function,
	}
	if strings.TrimSpace(toolID) != "" {
		delta["id"] = toolID
		delta["type"] = "function"
	}
	return delta
}

func extractOpenAISystemText(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err == nil {
			return strings.TrimSpace(text)
		}
		return ""
	}
	var blocks []map[string]any
	if err := json.Unmarshal(trimmed, &blocks); err != nil {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if normalizeTextLikeBlockType(fmt.Sprint(block["type"])) != "text" {
			continue
		}
		if text, ok := extractTextFromContentBlock(block); ok && strings.TrimSpace(text) != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func normalizeTextLikeBlockType(rawType string) string {
	switch strings.ToLower(strings.TrimSpace(rawType)) {
	case "input_text", "output_text":
		return "text"
	default:
		return strings.ToLower(strings.TrimSpace(rawType))
	}
}

func extractTextFromContentBlock(block map[string]any) (string, bool) {
	for _, key := range []string{"text", "input_text", "output_text"} {
		if text, ok := block[key].(string); ok {
			return text, true
		}
	}
	return "", false
}

func convertOpenAIImageURLBlockToAnthropic(block map[string]any) (map[string]any, error) {
	imageURL, ok := extractOpenAIImageURL(block["image_url"])
	if !ok {
		return nil, fmt.Errorf("openai image_url content block missing image_url.url")
	}
	if mediaType, data, ok := parseDataURLImageSource(imageURL); ok {
		return map[string]any{
			"type": "image",
			"source": map[string]any{
				"type":       "base64",
				"media_type": mediaType,
				"data":       data,
			},
		}, nil
	}
	return map[string]any{
		"type": "image",
		"source": map[string]any{
			"type": "url",
			"url":  imageURL,
		},
	}, nil
}

func extractOpenAIImageURL(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		url := strings.TrimSpace(typed)
		return url, url != ""
	case map[string]any:
		url := strings.TrimSpace(fmt.Sprint(typed["url"]))
		return url, url != ""
	default:
		return "", false
	}
}

func parseDataURLImageSource(raw string) (mediaType, data string, ok bool) {
	value := strings.TrimSpace(raw)
	lower := strings.ToLower(value)
	if !strings.HasPrefix(lower, "data:") {
		return "", "", false
	}
	parts := strings.SplitN(value[len("data:"):], ",", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	meta := parts[0]
	data = strings.TrimSpace(parts[1])
	if data == "" {
		return "", "", false
	}
	metaParts := strings.Split(meta, ";")
	if len(metaParts) == 0 {
		return "", "", false
	}
	mediaType = strings.TrimSpace(metaParts[0])
	if mediaType == "" || !strings.HasPrefix(strings.ToLower(mediaType), "image/") {
		return "", "", false
	}
	for _, token := range metaParts[1:] {
		if strings.EqualFold(strings.TrimSpace(token), "base64") {
			return mediaType, data, true
		}
	}
	return "", "", false
}

func convertAnthropicImageBlockToOpenAI(block map[string]any) (map[string]any, bool) {
	source, _ := block["source"].(map[string]any)
	if len(source) == 0 {
		return nil, false
	}
	sourceType := strings.ToLower(strings.TrimSpace(fmt.Sprint(source["type"])))
	switch sourceType {
	case "url":
		url := strings.TrimSpace(fmt.Sprint(source["url"]))
		if url == "" {
			return nil, false
		}
		return map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": url},
		}, true
	case "base64":
		mediaType := strings.TrimSpace(fmt.Sprint(source["media_type"]))
		data := strings.TrimSpace(fmt.Sprint(source["data"]))
		if mediaType == "" || data == "" {
			return nil, false
		}
		return map[string]any{
			"type": "image_url",
			"image_url": map[string]any{
				"url": fmt.Sprintf("data:%s;base64,%s", mediaType, data),
			},
		}, true
	default:
		return nil, false
	}
}

func openAIContentFromUserParts(parts []map[string]any) any {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 && strings.TrimSpace(fmt.Sprint(parts[0]["type"])) == "text" {
		if text, ok := parts[0]["text"].(string); ok {
			return text
		}
	}
	out := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		out = append(out, cloneStringAnyMap(part))
	}
	return out
}

func normalizeToolDescription(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func convertOpenAIToolsToAnthropic(raw json.RawMessage) ([]map[string]any, error) {
	var tools []map[string]any
	if err := json.Unmarshal(raw, &tools); err != nil {
		return nil, fmt.Errorf("decode openai tools: %w", err)
	}
	converted := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		name, description, inputSchema, ok := extractFunctionFieldsFromOpenAITool(tool)
		if !ok {
			continue
		}
		converted = append(converted, map[string]any{
			"name":         name,
			"description":  normalizeToolDescription(description),
			"input_schema": inputSchema,
		})
	}
	return converted, nil
}

func extractFunctionFieldsFromOpenAITool(tool map[string]any) (name any, description any, inputSchema any, ok bool) {
	if strings.TrimSpace(fmt.Sprint(tool["type"])) != "function" {
		return nil, nil, nil, false
	}
	if fn, hasNested := tool["function"].(map[string]any); hasNested && len(fn) > 0 {
		return fn["name"], fn["description"], fn["parameters"], true
	}
	return tool["name"], tool["description"], tool["parameters"], true
}

func convertOpenAIFunctionsToAnthropic(raw json.RawMessage) ([]map[string]any, error) {
	var functions []map[string]any
	if err := json.Unmarshal(raw, &functions); err != nil {
		return nil, fmt.Errorf("decode openai functions: %w", err)
	}
	converted := make([]map[string]any, 0, len(functions))
	for _, function := range functions {
		converted = append(converted, map[string]any{
			"name":         function["name"],
			"description":  normalizeToolDescription(function["description"]),
			"input_schema": function["parameters"],
		})
	}
	return converted, nil
}

// convertResponsesRequestToChat converts an OpenAI Responses API request body
// to a Chat Completions API request body.
//
// Responses API input format:
//
//	{"model":"gpt-4o","input":"hello","stream":false}
//	{"model":"gpt-4o","input":[{"role":"user","content":"hello"}],"stream":false}
//
// Chat Completions output format:
//
//	{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}],"stream":false}
func convertResponsesRequestToChat(body []byte, upstreamModel string) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode responses request: %w", err)
	}

	out := make(map[string]any)

	// Copy model, stream, and other passthrough fields.
	copyRawField := func(from, to string) {
		if value, ok := raw[from]; ok {
			var decoded any
			if err := json.Unmarshal(value, &decoded); err == nil {
				out[to] = decoded
			}
		}
	}
	copyRawField("model", "model")
	copyRawField("stream", "stream")
	copyRawField("temperature", "temperature")
	copyRawField("top_p", "top_p")
	copyRawField("max_output_tokens", "max_tokens")
	copyRawField("parallel_tool_calls", "parallel_tool_calls")
	if toolChoiceRaw, ok := raw["tool_choice"]; ok {
		if toolChoice := convertResponsesToolChoiceToChat(toolChoiceRaw); toolChoice != nil {
			out["tool_choice"] = toolChoice
		}
	}

	// Bridge Responses tools into Chat Completions-compatible tools. Custom
	// tools are represented as function tools upstream and restored on output
	// using the request's custom tool names.
	if toolsRaw, ok := raw["tools"]; ok {
		var tools []map[string]any
		if err := json.Unmarshal(toolsRaw, &tools); err == nil {
			chatTools := make([]map[string]any, 0, len(tools))
			for _, tool := range tools {
				switch strings.TrimSpace(fmt.Sprint(tool["type"])) {
				case "function":
					name, description, parameters, ok := extractFunctionFieldsFromOpenAITool(tool)
					if !ok {
						continue
					}
					chatTools = append(chatTools, map[string]any{
						"type": "function",
						"function": map[string]any{
							"name":        name,
							"description": normalizeToolDescription(description),
							"parameters":  parameters,
						},
					})
				case "custom":
					if functionTool, ok := convertResponsesCustomToolToChatFunction(tool); ok {
						chatTools = append(chatTools, functionTool)
					}
				default:
					// Responses API built-in tools (web_search, web_search_preview,
					// local_shell, file_search, computer_use_preview, code_interpreter, ...)
					// have no Chat Completions equivalent. Drop them silently so Codex and
					// other Responses-native clients stay compatible; the upstream will run
					// without the built-in tool instead of failing the whole request.
					continue
				}
			}
			if len(chatTools) > 0 {
				out["tools"] = chatTools
			}
		}
	}

	if strings.TrimSpace(upstreamModel) != "" {
		out["model"] = strings.TrimSpace(upstreamModel)
	}

	// Convert "input" to "messages".
	inputRaw, ok := raw["input"]
	if !ok {
		return nil, fmt.Errorf("responses request missing 'input' field")
	}

	// input can be a string or an array of message objects.
	var inputStr string
	if err := json.Unmarshal(inputRaw, &inputStr); err == nil {
		// Simple string input → single user message.
		out["messages"] = []map[string]any{
			{"role": "user", "content": inputStr},
		}
	} else {
		// Array of message objects.
		var messages []map[string]any
		if err := json.Unmarshal(inputRaw, &messages); err != nil {
			return nil, fmt.Errorf("decode responses input: %w", err)
		}
		normalized, err := convertResponsesInputToChatMessages(messages)
		if err != nil {
			return nil, err
		}
		out["messages"] = normalized
	}
	if instructions, ok := responsesInstructionsMessage(raw["instructions"]); ok {
		messages, _ := out["messages"].([]map[string]any)
		out["messages"] = append([]map[string]any{instructions}, messages...)
	}

	return json.Marshal(out)
}

func responsesInstructionsMessage(raw json.RawMessage) (map[string]any, bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, false
	}
	text := responsesTextFromRaw(raw)
	if strings.TrimSpace(text) == "" {
		return nil, false
	}
	return map[string]any{
		"role":    "system",
		"content": text,
	}, true
}

func responsesTextFromRaw(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err == nil {
			return text
		}
		return ""
	}
	var blocks []map[string]any
	if err := json.Unmarshal(trimmed, &blocks); err == nil {
		parts := make([]string, 0, len(blocks))
		for _, block := range blocks {
			if normalizeTextLikeBlockType(fmt.Sprint(block["type"])) != "text" {
				continue
			}
			if text, ok := extractTextFromContentBlock(block); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n\n")
	}
	var value any
	if err := json.Unmarshal(trimmed, &value); err == nil {
		return stringifyContentValue(value)
	}
	return ""
}

func convertResponsesToolChoiceToChat(raw json.RawMessage) any {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}

	var choice map[string]any
	if err := json.Unmarshal(raw, &choice); err != nil {
		return nil
	}
	choiceType := strings.ToLower(strings.TrimSpace(fmt.Sprint(choice["type"])))
	switch choiceType {
	case "custom", "function":
		name := strings.TrimSpace(fmt.Sprint(choice["name"]))
		if name == "" {
			if fn, ok := choice["function"].(map[string]any); ok {
				name = strings.TrimSpace(fmt.Sprint(fn["name"]))
			}
		}
		if name == "" {
			return nil
		}
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": name,
			},
		}
	case "auto", "required", "none":
		return choiceType
	default:
		// Responses tool_choice that references a built-in tool type (e.g.
		// {"type":"web_search"}) has no Chat Completions equivalent. Fall back
		// to "auto" so the request stays valid; the dropped tool is already
		// absent from the forwarded tools list.
		return "auto"
	}
}

func extractResponsesCustomToolNames(body []byte) map[string]struct{} {
	var payload struct {
		Tools []map[string]any `json:"tools"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	names := make(map[string]struct{})
	for _, tool := range payload.Tools {
		if strings.TrimSpace(fmt.Sprint(tool["type"])) != "custom" {
			continue
		}
		name := strings.TrimSpace(fmt.Sprint(tool["name"]))
		if name != "" {
			names[name] = struct{}{}
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func convertResponsesCustomToolToChatFunction(tool map[string]any) (map[string]any, bool) {
	name := strings.TrimSpace(fmt.Sprint(tool["name"]))
	if name == "" {
		return nil, false
	}
	description := normalizeToolDescription(tool["description"])
	if strings.TrimSpace(description) == "" {
		description = "Call custom tool " + name + "."
	}
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        name,
			"description": description,
			"parameters": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"input": map[string]any{
						"type":        "string",
						"description": "Raw custom tool input.",
					},
				},
				"required": []string{"input"},
			},
		},
	}, true
}

// convertResponsesInputToChatMessages normalizes OpenAI Responses-style "input"
// arrays into Chat Completions "messages" shape. Codex and other clients may
// send Responses item wrappers (type=message), input_text/input_image parts, or
// JSON null content (e.g. assistant turns with only tool_calls). It also keeps
// Responses custom tool call items in the Chat tool-call conversation shape.
func convertResponsesInputToChatMessages(input []map[string]any) ([]map[string]any, error) {
	if len(input) == 0 {
		return nil, fmt.Errorf("responses input message list is empty")
	}
	out := make([]map[string]any, 0, len(input))
	for _, raw := range input {
		itemType, _ := raw["type"].(string)
		switch itemType {
		case "custom_tool_call":
			msg, ok := convertResponsesCustomToolCallItemToChat(raw)
			if ok {
				out = append(out, msg)
			}
			continue
		case "custom_tool_call_output":
			msg, ok := convertResponsesCustomToolCallOutputItemToChat(raw)
			if ok {
				out = append(out, msg)
			}
			continue
		case "function_call":
			msg, ok := convertResponsesFunctionCallItemToChat(raw)
			if ok {
				out = append(out, msg)
			}
			continue
		case "function_call_output":
			msg, ok := convertResponsesFunctionCallOutputItemToChat(raw)
			if ok {
				out = append(out, msg)
			}
			continue
		}

		msg := make(map[string]any, len(raw)+2)
		for k, v := range raw {
			msg[k] = v
		}
		if typ, ok := msg["type"].(string); ok && typ == "message" {
			delete(msg, "type")
		}
		role, _ := msg["role"].(string)
		if strings.TrimSpace(role) == "" {
			// Skip Responses-only items (reasoning, etc.) that are not chat roles.
			continue
		}
		content, hasContent := msg["content"]
		if !hasContent || content == nil {
			msg["content"] = ""
		} else if parts, ok := coerceJSONSlice(content); ok {
			norm, err := normalizeResponsesStyleContentToChatContent(parts)
			if err != nil {
				return nil, err
			}
			msg["content"] = norm
		}
		out = append(out, msg)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("responses input contained no messages with a role")
	}
	return out, nil
}

func sanitizeResponsesInputToChatMessages(messages []map[string]any) ([]map[string]any, error) {
	return convertResponsesInputToChatMessages(messages)
}

func convertResponsesCustomToolCallItemToChat(item map[string]any) (map[string]any, bool) {
	name := strings.TrimSpace(fmt.Sprint(item["name"]))
	if name == "" {
		return nil, false
	}
	callID := responsesCallID(item)
	if callID == "" {
		callID = "call_" + uuid.NewString()
	}
	return map[string]any{
		"role":    "assistant",
		"content": "",
		"tool_calls": []map[string]any{
			{
				"id":   callID,
				"type": "function",
				"function": map[string]any{
					"name":      name,
					"arguments": compactJSONValue(map[string]any{"input": stringifyResponsesToolInput(item["input"])}),
				},
			},
		},
	}, true
}

func convertResponsesFunctionCallItemToChat(item map[string]any) (map[string]any, bool) {
	name := strings.TrimSpace(fmt.Sprint(item["name"]))
	if name == "" {
		return nil, false
	}
	callID := responsesCallID(item)
	if callID == "" {
		callID = "call_" + uuid.NewString()
	}
	return map[string]any{
		"role":    "assistant",
		"content": "",
		"tool_calls": []map[string]any{
			{
				"id":   callID,
				"type": "function",
				"function": map[string]any{
					"name":      name,
					"arguments": stringifyResponsesToolInput(item["arguments"]),
				},
			},
		},
	}, true
}

func convertResponsesCustomToolCallOutputItemToChat(item map[string]any) (map[string]any, bool) {
	callID := responsesCallID(item)
	if callID == "" {
		return nil, false
	}
	return map[string]any{
		"role":         "tool",
		"tool_call_id": callID,
		"content":      stringifyResponsesToolOutput(item["output"]),
	}, true
}

func convertResponsesFunctionCallOutputItemToChat(item map[string]any) (map[string]any, bool) {
	callID := responsesCallID(item)
	if callID == "" {
		return nil, false
	}
	return map[string]any{
		"role":         "tool",
		"tool_call_id": callID,
		"content":      stringifyResponsesToolOutput(item["output"]),
	}, true
}

func responsesCallID(item map[string]any) string {
	for _, key := range []string{"call_id", "id"} {
		if value := strings.TrimSpace(fmt.Sprint(item[key])); value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func stringifyResponsesToolInput(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(data)
	}
}

func stringifyResponsesToolOutput(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(data)
	}
}

func coerceJSONSlice(v any) ([]any, bool) {
	switch t := v.(type) {
	case []any:
		return t, true
	case []map[string]any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = t[i]
		}
		return out, true
	default:
		return nil, false
	}
}

func normalizeResponsesStyleContentToChatContent(parts []any) (any, error) {
	if len(parts) == 0 {
		return "", nil
	}
	chatParts := make([]map[string]any, 0, len(parts))
	for _, p := range parts {
		block, ok := p.(map[string]any)
		if !ok {
			continue
		}
		switch normalizeTextLikeBlockType(fmt.Sprint(block["type"])) {
		case "text":
			txt, _ := extractTextFromContentBlock(block)
			chatParts = append(chatParts, map[string]any{"type": "text", "text": txt})
		case "input_image":
			part := map[string]any{"type": "image_url"}
			switch u := block["image_url"].(type) {
			case string:
				part["image_url"] = map[string]any{"url": u}
			case map[string]any:
				part["image_url"] = u
			default:
				return nil, fmt.Errorf("responses input_image: image_url must be string or object")
			}
			chatParts = append(chatParts, part)
		case "image_url":
			chatParts = append(chatParts, cloneStringAnyMap(block))
		default:
			// Best-effort passthrough for uncommon part types.
			chatParts = append(chatParts, cloneStringAnyMap(block))
		}
	}
	if len(chatParts) == 0 {
		return "", nil
	}
	if len(chatParts) == 1 {
		if tp, _ := chatParts[0]["type"].(string); tp == "text" {
			if txt, ok := chatParts[0]["text"].(string); ok {
				return txt, nil
			}
		}
	}
	return chatParts, nil
}

// adaptChatResponseToResponses converts a Chat Completions API response body
// to the OpenAI Responses API format.
//
// Chat Completions input:
//
//	{"id":"chatcmpl-123","object":"chat.completion","created":123,"model":"gpt-4o",
//	 "choices":[{"index":0,"message":{"role":"assistant","content":"Hi!"},"finish_reason":"stop"}],
//	 "usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}
//
// Responses API output:
//
//	{"id":"resp_chatcmpl-123","object":"response","created_at":123,"model":"gpt-4o",
//	 "output":[{"type":"message","id":"msg_chatcmpl-123","status":"completed","role":"assistant",
//	 "content":[{"type":"output_text","text":"Hi!"}]}],
//	 "usage":{"input_tokens":5,"output_tokens":3,"total_tokens":8},"status":"completed"}
func adaptChatResponseToResponses(respBody []byte, clientModel string, customToolNames map[string]struct{}) ([]byte, error) {
	var payload struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Index        int    `json:"index"`
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Role         string                  `json:"role"`
				Content      json.RawMessage         `json:"content"`
				ToolCalls    []openAIToolCall        `json:"tool_calls"`
				FunctionCall *openAIToolCallFunction `json:"function_call"`
			} `json:"message"`
		} `json:"choices"`
		Usage openAIUsagePayload `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return nil, fmt.Errorf("decode chat response: %w", err)
	}

	model := strings.TrimSpace(clientModel)
	if model == "" {
		model = payload.Model
	}

	// Build output items from the first choice.
	outputItems := make([]any, 0)
	var msgID string
	if len(payload.Choices) > 0 {
		choice := payload.Choices[0]
		msgID = payload.ID
		if msgID == "" {
			msgID = "msg_" + uuid.NewString()
		} else if !strings.HasPrefix(msgID, "msg_") {
			msgID = "msg_" + msgID
		}

		outputContent := make([]map[string]any, 0)

		if text := chatMessageContentText(choice.Message.Content); strings.TrimSpace(text) != "" {
			outputContent = append(outputContent, map[string]any{
				"type": "output_text",
				"text": text,
			})
		}

		if len(outputContent) > 0 {
			outputItems = append(outputItems, map[string]any{
				"type":    "message",
				"id":      msgID,
				"status":  "completed",
				"role":    "assistant",
				"content": outputContent,
			})
		}

		toolCalls := append([]openAIToolCall(nil), choice.Message.ToolCalls...)
		if len(toolCalls) == 0 && choice.Message.FunctionCall != nil && strings.TrimSpace(choice.Message.FunctionCall.Name) != "" {
			toolCalls = append(toolCalls, openAIToolCall{
				ID:       "call_" + uuid.NewString(),
				Type:     "function",
				Function: *choice.Message.FunctionCall,
			})
		}
		for _, tc := range toolCalls {
			tc = restoreResponsesCustomToolCall(tc, customToolNames)
			if item, ok := chatToolCallToResponsesOutputItem(tc); ok {
				outputItems = append(outputItems, item)
			}
		}
	}

	if len(outputItems) == 0 {
		if strings.TrimSpace(msgID) == "" {
			msgID = "msg_" + uuid.NewString()
		}
		outputItems = append(outputItems, map[string]any{
			"type":   "message",
			"id":     msgID,
			"status": "completed",
			"role":   "assistant",
			"content": []map[string]any{
				{
					"type": "output_text",
					"text": "",
				},
			},
		})
	}

	respID := payload.ID
	if respID == "" {
		respID = "resp_" + uuid.NewString()
	} else if !strings.HasPrefix(respID, "resp_") {
		respID = "resp_" + respID
	}

	resp := map[string]any{
		"id":         respID,
		"object":     "response",
		"created_at": payload.Created,
		"model":      model,
		"output":     outputItems,
		"usage": responsesUsageMap(
			payload.Usage.PromptTokens,
			openAIUsageCachedTokens(payload.Usage),
			payload.Usage.CompletionTokens,
			payload.Usage.TotalTokens,
		),
		"status": "completed",
	}

	return json.Marshal(resp)
}

func chatMessageContentText(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err == nil {
			return text
		}
		return ""
	}
	var blocks []map[string]any
	if err := json.Unmarshal(trimmed, &blocks); err == nil {
		parts := make([]string, 0, len(blocks))
		for _, block := range blocks {
			if normalizeTextLikeBlockType(fmt.Sprint(block["type"])) != "text" {
				continue
			}
			if text, ok := extractTextFromContentBlock(block); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func restoreResponsesCustomToolCall(tc openAIToolCall, customToolNames map[string]struct{}) openAIToolCall {
	if len(customToolNames) == 0 {
		return tc
	}
	if normalizeChatToolCallKind(tc.Type) == "custom" {
		return tc
	}
	name := strings.TrimSpace(tc.Function.Name)
	if _, ok := customToolNames[name]; !ok {
		return tc
	}
	tc.Type = "custom"
	tc.Custom.Name = name
	tc.Custom.Input = extractCustomToolInputFromFunctionArguments(tc.Function.Arguments)
	tc.Function = openAIToolCallFunction{}
	return tc
}

func extractCustomToolInputFromFunctionArguments(arguments string) string {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
		if input, ok := payload["input"]; ok {
			return stringifyResponsesToolInput(input)
		}
	}
	return arguments
}

func chatToolCallToResponsesOutputItem(tc openAIToolCall) (map[string]any, bool) {
	kind := strings.ToLower(strings.TrimSpace(tc.Type))
	if kind == "" {
		if strings.TrimSpace(tc.Custom.Name) != "" {
			kind = "custom"
		} else {
			kind = "function"
		}
	}
	callID := strings.TrimSpace(tc.ID)
	if callID == "" {
		callID = "call_" + uuid.NewString()
	}
	switch kind {
	case "custom":
		name := strings.TrimSpace(tc.Custom.Name)
		if name == "" {
			return nil, false
		}
		return map[string]any{
			"id":      responsesToolItemID("ctc", callID),
			"type":    "custom_tool_call",
			"status":  "completed",
			"call_id": callID,
			"name":    name,
			"input":   tc.Custom.Input,
		}, true
	case "function":
		name := strings.TrimSpace(tc.Function.Name)
		if name == "" {
			return nil, false
		}
		return map[string]any{
			"id":        responsesToolItemID("fc", callID),
			"type":      "function_call",
			"status":    "completed",
			"call_id":   callID,
			"name":      name,
			"arguments": tc.Function.Arguments,
		}, true
	default:
		return nil, false
	}
}

func responsesToolItemID(prefix, callID string) string {
	suffix := strings.TrimSpace(callID)
	if suffix == "" {
		return prefix + "_" + uuid.NewString()
	}
	for _, knownPrefix := range []string{"call_", "fc_", "ctc_"} {
		suffix = strings.TrimPrefix(suffix, knownPrefix)
	}
	if strings.TrimSpace(suffix) == "" {
		suffix = uuid.NewString()
	}
	return prefix + "_" + suffix
}

// bridgeChatStreamToResponses converts a Chat Completions SSE stream into
// Responses API SSE format.
func bridgeChatStreamToResponses(w http.ResponseWriter, statusCode int, respBody io.ReadCloser, clientModel string, customToolNames map[string]struct{}) (promptTokens, cachedPromptTokens, completionTokens int64) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(statusCode)

	flusher, _ := w.(http.Flusher)
	return bridgeChatStreamToResponsesStarted(w, flusher, respBody, clientModel, customToolNames)
}

func bridgeChatStreamToResponsesStarted(w http.ResponseWriter, flusher http.Flusher, respBody io.ReadCloser, clientModel string, customToolNames map[string]struct{}) (promptTokens, cachedPromptTokens, completionTokens int64) {
	if respBody == nil {
		return 0, 0, 0
	}
	defer respBody.Close()

	reader := bufio.NewReader(respBody)
	var eventData bytes.Buffer

	state := responsesStreamState{
		model:           strings.TrimSpace(clientModel),
		created:         time.Now().Unix(),
		customToolNames: customToolNames,
	}

	flush := func() {
		if flusher != nil {
			flusher.Flush()
		}
	}

	emit := func(payload []byte) {
		if len(payload) == 0 {
			return
		}
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(payload)
		_, _ = w.Write([]byte("\n\n"))
		flush()
	}

	handleEvent := func(data []byte) {
		payloads, done := translateChatStreamEventToResponses(data, &state)
		for _, p := range payloads {
			emit(p)
		}
		if done {
			if state.usageEmitted {
				return
			}
			state.usageEmitted = true
			emitDone := map[string]any{
				"type":     "response.completed",
				"response": state.buildFinalResponse(),
			}
			if payload, err := json.Marshal(emitDone); err == nil {
				emit(payload)
			}
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			flush()
		}
	}

	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			trimmedLine := strings.TrimRight(string(line), "\r\n")
			if strings.TrimSpace(trimmedLine) == "" {
				if eventData.Len() > 0 {
					handleEvent(eventData.Bytes())
					eventData.Reset()
				}
			} else if strings.HasPrefix(trimmedLine, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(trimmedLine, "data:"))
				if eventData.Len() > 0 {
					eventData.WriteByte('\n')
				}
				eventData.WriteString(data)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				if eventData.Len() > 0 {
					handleEvent(eventData.Bytes())
				}
				if !state.usageEmitted {
					state.usageEmitted = true
					emitDone := map[string]any{
						"type":     "response.completed",
						"response": state.buildFinalResponse(),
					}
					if payload, err := json.Marshal(emitDone); err == nil {
						emit(payload)
					}
					_, _ = w.Write([]byte("data: [DONE]\n\n"))
					flush()
				}
			}
			break
		}
	}

	return state.promptTokens, state.cachedPromptTokens, state.completionTokens
}

type responsesStreamState struct {
	model                  string
	created                int64
	customToolNames        map[string]struct{}
	promptTokens           int64
	completionTokens       int64
	cachedPromptTokens     int64
	textStarted            bool
	textDone               bool
	text                   strings.Builder
	messageID              string
	messageOutputIndex     int
	messageContentIndex    int
	messageItemAdded       bool
	messagePartAdded       bool
	toolCalls              map[int]*responsesStreamToolCallState
	toolOutputBase         int
	responseID             string
	finishReason           string
	usageEmitted           bool
	createdEventEmitted    bool
	inProgressEventEmitted bool
}

type responsesStreamToolCallState struct {
	index           int
	outputIndex     int
	itemID          string
	callID          string
	kind            string
	name            string
	arguments       strings.Builder
	rawArguments    strings.Builder
	bridgedFunction bool
	added           bool
	done            bool
}

func (s *responsesStreamState) ensureResponseID() string {
	if strings.TrimSpace(s.responseID) != "" {
		return s.responseID
	}
	s.responseID = "resp_" + uuid.NewString()
	return s.responseID
}

func (s *responsesStreamState) ensureMessageID() string {
	if strings.TrimSpace(s.messageID) != "" {
		return s.messageID
	}
	respID := s.ensureResponseID()
	if strings.HasPrefix(respID, "resp_") && len(respID) > len("resp_") {
		s.messageID = "msg_" + strings.TrimPrefix(respID, "resp_")
	} else {
		s.messageID = "msg_" + uuid.NewString()
	}
	return s.messageID
}

func (s *responsesStreamState) buildInProgressResponse() map[string]any {
	return map[string]any{
		"id":         s.ensureResponseID(),
		"object":     "response",
		"created_at": s.created,
		"status":     "in_progress",
		"model":      s.model,
		"output":     []any{},
		"usage":      nil,
	}
}

func (s *responsesStreamState) buildCompletedMessageItem() map[string]any {
	return map[string]any{
		"id":     s.ensureMessageID(),
		"type":   "message",
		"status": "completed",
		"role":   "assistant",
		"content": []map[string]any{
			{
				"type":        "output_text",
				"text":        s.text.String(),
				"annotations": []any{},
			},
		},
	}
}

func (s *responsesStreamState) buildFinalResponse() map[string]any {
	outputItems := make([]any, 0, 1+len(s.toolCalls))
	if s.textStarted || strings.TrimSpace(s.text.String()) != "" {
		outputItems = append(outputItems, s.buildCompletedMessageItem())
	}
	for _, tc := range s.orderedToolCalls() {
		tc.finalizeBridgedCustomInput()
		outputItems = append(outputItems, tc.completedResponsesItem())
	}

	return map[string]any{
		"id":         s.ensureResponseID(),
		"object":     "response",
		"created_at": s.created,
		"model":      s.model,
		"status":     "completed",
		"output":     outputItems,
		"usage":      responsesUsageMap(s.promptTokens, s.cachedPromptTokens, s.completionTokens, 0),
	}
}

func mustJSON(payload map[string]any) []byte {
	data, _ := json.Marshal(payload)
	return data
}

func (s *responsesStreamState) ensureMessageStartedEvents() [][]byte {
	if s.messageItemAdded {
		return nil
	}
	s.messageItemAdded = true
	s.textStarted = true
	itemID := s.ensureMessageID()
	outputIndex := s.messageOutputIndex
	contentIndex := s.messageContentIndex

	return [][]byte{
		mustJSON(map[string]any{
			"type":         "response.output_item.added",
			"output_index": outputIndex,
			"item": map[string]any{
				"id":      itemID,
				"type":    "message",
				"status":  "in_progress",
				"role":    "assistant",
				"content": []any{},
			},
		}),
		mustJSON(map[string]any{
			"type":          "response.content_part.added",
			"item_id":       itemID,
			"output_index":  outputIndex,
			"content_index": contentIndex,
			"part": map[string]any{
				"type":        "output_text",
				"text":        "",
				"annotations": []any{},
			},
		}),
	}
}

func (s *responsesStreamState) finalizeTextEvents() [][]byte {
	if !s.textStarted || s.textDone {
		return nil
	}
	s.textDone = true
	s.messagePartAdded = true
	itemID := s.ensureMessageID()
	outputIndex := s.messageOutputIndex
	contentIndex := s.messageContentIndex
	text := s.text.String()
	item := s.buildCompletedMessageItem()

	return [][]byte{
		mustJSON(map[string]any{
			"type":          "response.output_text.done",
			"item_id":       itemID,
			"output_index":  outputIndex,
			"content_index": contentIndex,
			"text":          text,
		}),
		mustJSON(map[string]any{
			"type":          "response.content_part.done",
			"item_id":       itemID,
			"output_index":  outputIndex,
			"content_index": contentIndex,
			"part": map[string]any{
				"type":        "output_text",
				"text":        text,
				"annotations": []any{},
			},
		}),
		mustJSON(map[string]any{
			"type":         "response.output_item.done",
			"output_index": outputIndex,
			"item":         item,
		}),
	}
}

func (s *responsesStreamState) ensureToolCall(index int, tcID, kind, name string) *responsesStreamToolCallState {
	if s.toolCalls == nil {
		s.toolCalls = make(map[int]*responsesStreamToolCallState)
	}
	if call, ok := s.toolCalls[index]; ok {
		if strings.TrimSpace(name) != "" {
			call.name = strings.TrimSpace(name)
		}
		if strings.TrimSpace(tcID) != "" {
			call.callID = strings.TrimSpace(tcID)
		}
		if normalizedKind := normalizeChatToolCallKind(kind); normalizedKind != "" {
			if call.kind == "custom" && normalizedKind == "function" && strings.TrimSpace(name) == "" {
				return call
			}
			call.kind = normalizedKind
			call.itemID = responsesToolItemID(responsesToolItemIDPrefix(normalizedKind), call.callID)
		}
		return call
	}

	callID := strings.TrimSpace(tcID)
	if callID == "" {
		callID = "call_" + uuid.NewString()
	}
	normalizedKind := normalizeChatToolCallKind(kind)
	if normalizedKind == "" {
		normalizedKind = "function"
	}
	itemID := responsesToolItemID(responsesToolItemIDPrefix(normalizedKind), callID)
	base := s.toolOutputBase
	if s.messageItemAdded {
		base = 1
	}
	call := &responsesStreamToolCallState{
		index:       index,
		outputIndex: base + index,
		itemID:      itemID,
		callID:      callID,
		kind:        normalizedKind,
		name:        strings.TrimSpace(name),
	}
	s.toolCalls[index] = call
	return call
}

func (s *responsesStreamState) orderedToolCalls() []*responsesStreamToolCallState {
	if len(s.toolCalls) == 0 {
		return nil
	}
	keys := make([]int, 0, len(s.toolCalls))
	for idx := range s.toolCalls {
		keys = append(keys, idx)
	}
	sort.Ints(keys)
	out := make([]*responsesStreamToolCallState, 0, len(keys))
	for _, idx := range keys {
		out = append(out, s.toolCalls[idx])
	}
	return out
}

func (s *responsesStreamState) finalizeToolCallEvents() [][]byte {
	calls := s.orderedToolCalls()
	if len(calls) == 0 {
		return nil
	}
	payloads := make([][]byte, 0, len(calls)*2)
	for _, call := range calls {
		if call.done {
			continue
		}
		call.done = true
		if delta := call.finalizeBridgedCustomInput(); delta != "" {
			payloads = append(payloads, mustJSON(map[string]any{
				"type":         call.inputDeltaEventType(),
				"item_id":      call.itemID,
				"output_index": call.outputIndex,
				"call_id":      call.callID,
				"delta":        delta,
			}))
		}
		args := call.arguments.String()
		payloads = append(payloads, mustJSON(map[string]any{
			"type":                call.inputDoneEventType(),
			"item_id":             call.itemID,
			"output_index":        call.outputIndex,
			"call_id":             call.callID,
			call.inputFieldName(): args,
		}))
		payloads = append(payloads, mustJSON(map[string]any{
			"type":         "response.output_item.done",
			"output_index": call.outputIndex,
			"item":         call.completedResponsesItem(),
		}))
	}
	return payloads
}

func normalizeChatToolCallKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "custom":
		return "custom"
	case "function", "":
		return "function"
	default:
		return ""
	}
}

func responsesToolItemIDPrefix(kind string) string {
	if kind == "custom" {
		return "ctc"
	}
	return "fc"
}

func (c *responsesStreamToolCallState) responsesItemType() string {
	if c.kind == "custom" {
		return "custom_tool_call"
	}
	return "function_call"
}

func (c *responsesStreamToolCallState) inputFieldName() string {
	if c.kind == "custom" {
		return "input"
	}
	return "arguments"
}

func (c *responsesStreamToolCallState) inputDeltaEventType() string {
	if c.kind == "custom" {
		return "response.custom_tool_call_input.delta"
	}
	return "response.function_call_arguments.delta"
}

func (c *responsesStreamToolCallState) inputDoneEventType() string {
	if c.kind == "custom" {
		return "response.custom_tool_call_input.done"
	}
	return "response.function_call_arguments.done"
}

func (c *responsesStreamToolCallState) inProgressResponsesItem() map[string]any {
	return map[string]any{
		"id":               c.itemID,
		"type":             c.responsesItemType(),
		"status":           "in_progress",
		"call_id":          c.callID,
		"name":             c.name,
		c.inputFieldName(): "",
	}
}

func (c *responsesStreamToolCallState) completedResponsesItem() map[string]any {
	c.finalizeBridgedCustomInput()
	return map[string]any{
		"id":               c.itemID,
		"type":             c.responsesItemType(),
		"status":           "completed",
		"call_id":          c.callID,
		"name":             c.name,
		c.inputFieldName(): c.arguments.String(),
	}
}

func (c *responsesStreamToolCallState) finalizeBridgedCustomInput() string {
	if !c.bridgedFunction {
		return ""
	}
	input := extractCustomToolInputFromFunctionArguments(c.rawArguments.String())
	if input == c.arguments.String() {
		return ""
	}
	c.arguments.Reset()
	c.arguments.WriteString(input)
	return input
}

func translateChatStreamEventToResponses(data []byte, state *responsesStreamState) ([][]byte, bool) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, false
	}
	if trimmed == "[DONE]" {
		return nil, true
	}

	var chunk struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Created int64  `json:"created"`
		Choices []struct {
			Delta struct {
				Role         string                 `json:"role"`
				Content      string                 `json:"content"`
				FunctionCall openAIToolCallFunction `json:"function_call"`
				ToolCalls    []struct {
					Index  int    `json:"index"`
					ID     string `json:"id"`
					Type   string `json:"type"`
					Custom struct {
						Name  string `json:"name"`
						Input string `json:"input"`
					} `json:"custom"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage openAIUsagePayload `json:"usage"`
	}
	if err := json.Unmarshal([]byte(trimmed), &chunk); err != nil {
		return nil, false
	}

	if chunk.ID != "" && state.responseID == "" {
		state.responseID = "resp_" + chunk.ID
	}
	if chunk.Model != "" && state.model == "" {
		state.model = chunk.Model
	}
	if chunk.Created > 0 {
		state.created = chunk.Created
	}
	if chunk.Usage.PromptTokens > 0 {
		state.promptTokens = chunk.Usage.PromptTokens
	}
	if chunk.Usage.CompletionTokens > 0 {
		state.completionTokens = chunk.Usage.CompletionTokens
	}
	if cachedTokens := openAIUsageCachedTokens(chunk.Usage); cachedTokens > 0 {
		state.cachedPromptTokens = cachedTokens
		if state.promptTokens < cachedTokens {
			state.promptTokens = cachedTokens
		}
	}

	payloads := make([][]byte, 0)
	if !state.createdEventEmitted {
		state.createdEventEmitted = true
		payloads = append(payloads, mustJSON(map[string]any{
			"type":     "response.created",
			"response": state.buildInProgressResponse(),
		}))
	}
	if !state.inProgressEventEmitted {
		state.inProgressEventEmitted = true
		payloads = append(payloads, mustJSON(map[string]any{
			"type":     "response.in_progress",
			"response": state.buildInProgressResponse(),
		}))
	}

	for _, choice := range chunk.Choices {
		if choice.Delta.Content != "" {
			payloads = append(payloads, state.ensureMessageStartedEvents()...)
			state.text.WriteString(choice.Delta.Content)
			payloads = append(payloads, mustJSON(map[string]any{
				"type":          "response.output_text.delta",
				"item_id":       state.ensureMessageID(),
				"output_index":  state.messageOutputIndex,
				"content_index": state.messageContentIndex,
				"delta":         choice.Delta.Content,
			}))
		}

		toolCalls := choice.Delta.ToolCalls
		if strings.TrimSpace(choice.Delta.FunctionCall.Name) != "" || strings.TrimSpace(choice.Delta.FunctionCall.Arguments) != "" {
			toolCalls = append(toolCalls, struct {
				Index  int    `json:"index"`
				ID     string `json:"id"`
				Type   string `json:"type"`
				Custom struct {
					Name  string `json:"name"`
					Input string `json:"input"`
				} `json:"custom"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			}{
				Index: 0,
				Type:  "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      choice.Delta.FunctionCall.Name,
					Arguments: choice.Delta.FunctionCall.Arguments,
				},
			})
		}

		for _, tc := range toolCalls {
			kind := normalizeChatToolCallKind(tc.Type)
			name := tc.Function.Name
			inputDelta := tc.Function.Arguments
			bridgedCustom := false
			if strings.TrimSpace(tc.Custom.Name) != "" || kind == "custom" {
				kind = "custom"
				name = tc.Custom.Name
				inputDelta = tc.Custom.Input
			} else if _, ok := state.customToolNames[strings.TrimSpace(name)]; ok {
				kind = "custom"
				bridgedCustom = true
			}
			call := state.ensureToolCall(tc.Index, tc.ID, kind, name)
			call.bridgedFunction = call.bridgedFunction || bridgedCustom
			if !call.added {
				call.added = true
				payloads = append(payloads, mustJSON(map[string]any{
					"type":         "response.output_item.added",
					"output_index": call.outputIndex,
					"item":         call.inProgressResponsesItem(),
				}))
			}
			if inputDelta != "" {
				if call.bridgedFunction {
					call.rawArguments.WriteString(inputDelta)
					continue
				}
				call.arguments.WriteString(inputDelta)
				payloads = append(payloads, mustJSON(map[string]any{
					"type":         call.inputDeltaEventType(),
					"item_id":      call.itemID,
					"output_index": call.outputIndex,
					"call_id":      call.callID,
					"delta":        inputDelta,
				}))
			}
		}

		if choice.FinishReason != "" {
			state.finishReason = choice.FinishReason
			payloads = append(payloads, state.finalizeTextEvents()...)
			payloads = append(payloads, state.finalizeToolCallEvents()...)
		}
	}

	return payloads, false
}
