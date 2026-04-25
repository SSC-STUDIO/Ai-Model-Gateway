package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/gateway/snapshot"
)

var errMessagesAPIRequiresAnthropicProvider = errors.New("messages API requires anthropic_messages provider")

type responseCompatMode int

const (
	responseCompatPassthrough responseCompatMode = iota
	responseCompatAnthropicToOpenAI
)

type compatPlan struct {
	forwardPath         string
	forwardBody         []byte
	upstreamIsAnthropic bool
	responseMode        responseCompatMode
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
}

type openAIToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
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

func collectProviderCandidatesForRequest(snap *snapshot.Snapshot, model string, clientAnthropic bool) ([]providerCandidate, bool) {
	if snap == nil {
		return nil, false
	}
	candidates := make([]providerCandidate, 0, len(snap.Providers))
	unsupportedMatches := false
	for i := range snap.Providers {
		p := &snap.Providers[i]
		if !p.ExecutionPolicy.Enabled {
			continue
		}
		for _, m := range p.ModelTable {
			if m.PublicModel != model {
				continue
			}
			if clientAnthropic && providerProtocolAdapter(p) != core.ProtocolAdapterAnthropicMessages {
				unsupportedMatches = true
				break
			}
			candidates = append(candidates, providerCandidate{
				provider:      p,
				upstreamModel: m.UpstreamModel,
				weight:        normalizeWeight(p.ExecutionPolicy.Weight),
			})
			break
		}
	}
	return candidates, unsupportedMatches
}

func buildCompatPlan(
	clientAnthropic bool,
	provider *snapshot.ProviderSnapshot,
	requestedModel string,
	upstreamModel string,
	body []byte,
) (compatPlan, error) {
	plan := compatPlan{
		forwardPath:         "/v1/chat/completions",
		forwardBody:         body,
		upstreamIsAnthropic: false,
		responseMode:        responseCompatPassthrough,
	}
	if provider == nil {
		return plan, fmt.Errorf("provider is required")
	}

	adapter := providerProtocolAdapter(provider)
	if clientAnthropic {
		if adapter != core.ProtocolAdapterAnthropicMessages {
			return compatPlan{}, errMessagesAPIRequiresAnthropicProvider
		}
		plan.forwardPath = "/v1/messages"
		plan.upstreamIsAnthropic = true
		if upstreamModel != "" && upstreamModel != requestedModel {
			plan.forwardBody = rewriteModelInBody(body, requestedModel, upstreamModel)
		}
		return plan, nil
	}

	if adapter == core.ProtocolAdapterAnthropicMessages {
		converted, err := convertOpenAIChatRequestToAnthropic(body, upstreamModel)
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
		plan.forwardBody = rewriteModelInBody(body, requestedModel, upstreamModel)
	}
	return plan, nil
}

func providerProtocolAdapter(provider *snapshot.ProviderSnapshot) string {
	if provider == nil {
		return core.ProtocolAdapterOpenAIChatCompletions
	}
	return core.NormalizeProtocolAdapter(provider.ProtocolAdapter, provider.AnthropicBaseURL)
}

func adaptResponseBodyForClient(plan compatPlan, statusCode int, respBody []byte) ([]byte, string, error) {
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
	if plan.responseMode == responseCompatAnthropicToOpenAI {
		return bridgeAnthropicStreamToOpenAI(w, statusCode, respBody)
	}
	return handleStreamResponse(w, statusCode, contentType, respBody)
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
		case "system":
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
			if text, ok := block["text"].(string); ok && block["type"] == "text" {
				converted = append(converted, map[string]any{
					"type": "text",
					"text": text,
				})
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
		if block["type"] != "text" {
			continue
		}
		if text, ok := block["text"].(string); ok && strings.TrimSpace(text) != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func convertOpenAIToolsToAnthropic(raw json.RawMessage) ([]map[string]any, error) {
	var tools []map[string]any
	if err := json.Unmarshal(raw, &tools); err != nil {
		return nil, fmt.Errorf("decode openai tools: %w", err)
	}
	converted := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		if strings.TrimSpace(fmt.Sprint(tool["type"])) != "function" {
			continue
		}
		fn, _ := tool["function"].(map[string]any)
		if len(fn) == 0 {
			continue
		}
		converted = append(converted, map[string]any{
			"name":         fn["name"],
			"description":  fn["description"],
			"input_schema": fn["parameters"],
		})
	}
	return converted, nil
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
			"description":  function["description"],
			"input_schema": function["parameters"],
		})
	}
	return converted, nil
}
