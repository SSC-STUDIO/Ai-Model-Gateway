package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ai-model-gateway/internal/contracts/telemetryingest"
	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/gateway/snapshot"
	pricinginfra "ai-model-gateway/internal/infra/pricing"
	"ai-model-gateway/internal/proxy"
	"ai-model-gateway/internal/telemetry"
	"ai-model-gateway/internal/testkit/fakeupstream"
)

type staticPricingResolver struct {
	snapshot pricinginfra.Snapshot
}

func (r staticPricingResolver) Snapshot() pricinginfra.Snapshot {
	return r.snapshot
}

func TestHandleChatCompletionBridgesAnthropicJSONResponseAndTelemetry(t *testing.T) {
	routingSequence.Store(0)
	allowLocalAnthropicTestUpstreams(t)

	tel := &capturingTelemetryEmitter{events: make(chan telemetryingest.Event, 1)}
	upstream := fakeupstream.New(func(req fakeupstream.CapturedRequest) fakeupstream.Response {
		return fakeupstream.Response{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"id":"msg_123","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"Hello from Claude"}],"stop_reason":"end_turn","usage":{"input_tokens":12,"cache_creation_input_tokens":3,"cache_read_input_tokens":5,"output_tokens":7}}`),
		}
	})
	defer upstream.Close()

	pricingResolver := staticPricingResolver{
		snapshot: pricinginfra.Snapshot{
			Catalog: map[string]pricinginfra.Price{
				"claude-sonnet-4-6": {
					Currency:            "USD",
					InputPer1M:          1,
					CachedInputPer1M:    0.25,
					OutputPer1M:         2,
					InputPer1MUsd:       1,
					CachedInputPer1MUsd: 0.25,
					OutputPer1MUsd:      2,
					SourceID:            "test-manual",
					Source:              "manual",
					FXRateToUSD:         1,
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleChatCompletion(r.Context(), anthropicBridgeSnapshot(upstream.URL()), NewRuntimeState(), tel, pricingResolver, w, r)
	}))
	defer server.Close()

	reqBody := `{"model":"public-model","messages":[{"role":"system","content":"You are helpful"},{"role":"user","content":[{"type":"text","text":"hello"}]}],"tools":[{"type":"function","function":{"name":"lookup_weather","description":"Look up weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}]}`
	resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	requests := upstream.Requests()
	if len(requests) != 1 {
		t.Fatalf("expected 1 upstream request, got %d", len(requests))
	}
	if requests[0].Path != "/v1/messages" {
		t.Fatalf("unexpected upstream path %q", requests[0].Path)
	}
	if got := requests[0].Header.Get("anthropic-version"); got != "2023-06-01" {
		t.Fatalf("anthropic-version = %q, want 2023-06-01", got)
	}
	if got := requests[0].Header.Get("x-api-key"); got != "test-anthropic-key" {
		t.Fatalf("x-api-key = %q, want test-anthropic-key", got)
	}

	var forwarded map[string]any
	if err := json.Unmarshal(requests[0].Body, &forwarded); err != nil {
		t.Fatalf("unmarshal forwarded body: %v", err)
	}
	if forwarded["model"] != "claude-sonnet-4-6" {
		t.Fatalf("forwarded model = %#v, want claude-sonnet-4-6", forwarded["model"])
	}
	if forwarded["system"] != "You are helpful" {
		t.Fatalf("forwarded system = %#v, want system prompt", forwarded["system"])
	}
	if forwarded["max_tokens"] != float64(4096) {
		t.Fatalf("forwarded max_tokens = %#v, want 4096", forwarded["max_tokens"])
	}
	tools, _ := forwarded["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("expected 1 anthropic tool, got %#v", forwarded["tools"])
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["object"] != "chat.completion" {
		t.Fatalf("object = %#v, want chat.completion", payload["object"])
	}
	usage := payload["usage"].(map[string]any)
	if usage["prompt_tokens"] != float64(20) || usage["completion_tokens"] != float64(7) {
		t.Fatalf("unexpected usage: %#v", usage)
	}
	promptDetails := usage["prompt_tokens_details"].(map[string]any)
	if promptDetails["cached_tokens"] != float64(5) {
		t.Fatalf("cached_tokens = %#v, want 5", promptDetails["cached_tokens"])
	}

	select {
	case event := <-tel.events:
		if event.Payload.RouteMode != "bridged" {
			t.Fatalf("route_mode = %q, want bridged", event.Payload.RouteMode)
		}
		if event.Payload.PromptTokens != 20 || event.Payload.CachedPromptTokens != 5 || event.Payload.CompletionTokens != 7 {
			t.Fatalf("unexpected telemetry usage: %+v", event.Payload)
		}
		if event.Payload.PricingStatus != telemetry.PricingStatusFixed {
			t.Fatalf("pricing_status = %q, want fixed", event.Payload.PricingStatus)
		}
		if event.Payload.PricingSourceID != "test-manual" {
			t.Fatalf("pricing_source_id = %q, want test-manual", event.Payload.PricingSourceID)
		}
	case <-time.After(time.Second):
		t.Fatal("expected telemetry event")
	}
}

func TestAdaptAnthropicResponseToOpenAIReturnsToolCalls(t *testing.T) {
	body, err := adaptAnthropicResponseToOpenAI([]byte(`{"id":"msg_tool","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"tool_use","id":"toolu_123","name":"lookup_weather","input":{"city":"Shanghai"}}],"stop_reason":"tool_use","usage":{"input_tokens":12,"output_tokens":4}}`))
	if err != nil {
		t.Fatalf("adaptAnthropicResponseToOpenAI() error = %v", err)
	}

	var payload struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Role      string `json:"role"`
				Content   any    `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode adapted response: %v", err)
	}
	if len(payload.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(payload.Choices))
	}
	choice := payload.Choices[0]
	if choice.FinishReason != "tool_calls" {
		t.Fatalf("finish_reason = %q, want tool_calls", choice.FinishReason)
	}
	if choice.Message.Role != "assistant" {
		t.Fatalf("role = %q, want assistant", choice.Message.Role)
	}
	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls = %#v, want 1 tool call", choice.Message.ToolCalls)
	}
	if choice.Message.ToolCalls[0].ID != "toolu_123" {
		t.Fatalf("tool call id = %q, want toolu_123", choice.Message.ToolCalls[0].ID)
	}
	if choice.Message.ToolCalls[0].Function.Name != "lookup_weather" {
		t.Fatalf("tool call name = %q, want lookup_weather", choice.Message.ToolCalls[0].Function.Name)
	}
	if choice.Message.ToolCalls[0].Function.Arguments != `{"city":"Shanghai"}` {
		t.Fatalf("tool call arguments = %q, want compact JSON", choice.Message.ToolCalls[0].Function.Arguments)
	}
	if choice.Message.Content != nil {
		t.Fatalf("message content = %#v, want nil for tool-only response", choice.Message.Content)
	}
}

func TestConvertOpenAIChatRequestToAnthropicPreservesToolConversation(t *testing.T) {
	body, err := convertOpenAIChatRequestToAnthropic([]byte(`{
		"model":"public-model",
		"tool_choice":{"type":"function","function":{"name":"lookup_weather"}},
		"messages":[
			{"role":"system","content":"You are helpful"},
			{"role":"user","content":"weather?"},
			{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup_weather","arguments":"{\"city\":\"Shanghai\"}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"{\"temperature_c\":22}"}
		]
	}`), "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("convertOpenAIChatRequestToAnthropic() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode anthropic body: %v", err)
	}
	if payload["model"] != "claude-sonnet-4-6" {
		t.Fatalf("model = %#v, want claude-sonnet-4-6", payload["model"])
	}
	if payload["system"] != "You are helpful" {
		t.Fatalf("system = %#v, want system prompt", payload["system"])
	}
	toolChoice, _ := payload["tool_choice"].(map[string]any)
	if toolChoice["type"] != "tool" || toolChoice["name"] != "lookup_weather" {
		t.Fatalf("tool_choice = %#v, want forced anthropic tool choice", toolChoice)
	}

	messages, _ := payload["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("messages = %#v, want 3 non-system messages", payload["messages"])
	}
	assistant, _ := messages[1].(map[string]any)
	if assistant["role"] != "assistant" {
		t.Fatalf("assistant role = %#v, want assistant", assistant["role"])
	}
	assistantBlocks, _ := assistant["content"].([]any)
	if len(assistantBlocks) != 1 {
		t.Fatalf("assistant content = %#v, want one tool_use block", assistant["content"])
	}
	assistantTool, _ := assistantBlocks[0].(map[string]any)
	if assistantTool["type"] != "tool_use" || assistantTool["id"] != "call_1" || assistantTool["name"] != "lookup_weather" {
		t.Fatalf("assistant tool block = %#v, want tool_use block", assistantTool)
	}
	input, _ := assistantTool["input"].(map[string]any)
	if input["city"] != "Shanghai" {
		t.Fatalf("assistant tool input = %#v, want city Shanghai", input)
	}

	toolResultMsg, _ := messages[2].(map[string]any)
	if toolResultMsg["role"] != "user" {
		t.Fatalf("tool result role = %#v, want user", toolResultMsg["role"])
	}
	toolResultBlocks, _ := toolResultMsg["content"].([]any)
	if len(toolResultBlocks) != 1 {
		t.Fatalf("tool result content = %#v, want one tool_result block", toolResultMsg["content"])
	}
	toolResult, _ := toolResultBlocks[0].(map[string]any)
	if toolResult["type"] != "tool_result" || toolResult["tool_use_id"] != "call_1" {
		t.Fatalf("tool result block = %#v, want tool_result for call_1", toolResult)
	}
	content := toolResult["content"]
	contentMap, ok := content.(map[string]any)
	if !ok || contentMap["temperature_c"] != float64(22) {
		t.Fatalf("tool result content = %#v, want parsed tool JSON", content)
	}
}

func TestHandleChatCompletionBridgesAnthropicToolUseJSONResponse(t *testing.T) {
	routingSequence.Store(0)
	allowLocalAnthropicTestUpstreams(t)

	upstream := fakeupstream.New(func(req fakeupstream.CapturedRequest) fakeupstream.Response {
		return fakeupstream.Response{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"id":"msg_tool","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"Let me check."},{"type":"tool_use","id":"toolu_123","name":"lookup_weather","input":{"city":"Shanghai"}}],"stop_reason":"tool_use","usage":{"input_tokens":11,"output_tokens":5}}`),
		}
	})
	defer upstream.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleChatCompletion(r.Context(), anthropicBridgeSnapshot(upstream.URL()), NewRuntimeState(), nil, nil, w, r)
	}))
	defer server.Close()

	reqBody := `{"model":"public-model","messages":[{"role":"user","content":"weather?"}]}`
	resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	choices := payload["choices"].([]any)
	choice := choices[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Fatalf("finish_reason = %#v, want tool_calls", choice["finish_reason"])
	}
	message := choice["message"].(map[string]any)
	if message["content"] != "Let me check." {
		t.Fatalf("content = %#v, want tool preamble text", message["content"])
	}
	toolCalls := message["tool_calls"].([]any)
	if len(toolCalls) != 1 {
		t.Fatalf("tool_calls len = %d, want 1", len(toolCalls))
	}
	toolCall := toolCalls[0].(map[string]any)
	if toolCall["id"] != "toolu_123" {
		t.Fatalf("tool id = %#v, want toolu_123", toolCall["id"])
	}
	function := toolCall["function"].(map[string]any)
	if function["name"] != "lookup_weather" {
		t.Fatalf("function name = %#v, want lookup_weather", function["name"])
	}
	if function["arguments"] != `{"city":"Shanghai"}` {
		t.Fatalf("function arguments = %#v, want compact JSON args", function["arguments"])
	}
}

func TestHandleChatCompletionBridgesAnthropicStreamAndUsage(t *testing.T) {
	routingSequence.Store(0)
	allowLocalAnthropicTestUpstreams(t)

	tel := &capturingTelemetryEmitter{events: make(chan telemetryingest.Event, 1)}
	upstream := fakeupstream.New(func(req fakeupstream.CapturedRequest) fakeupstream.Response {
		return fakeupstream.Response{
			StatusCode: http.StatusOK,
			Headers:    http.Header{"Content-Type": []string{"text/event-stream"}},
			Stream: []fakeupstream.StreamChunk{
				{Data: "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_stream\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-4-6\",\"usage\":{\"input_tokens\":10,\"cache_creation_input_tokens\":4,\"cache_read_input_tokens\":3}}}\n\n"},
				{Data: "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hel\"}}\n\n"},
				{Data: "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"lo\"}}\n\n"},
				{Data: "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":6}}\n\n"},
				{Data: "data: {\"type\":\"message_stop\"}\n\n"},
			},
		}
	})
	defer upstream.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleChatCompletion(r.Context(), anthropicBridgeSnapshot(upstream.URL()), NewRuntimeState(), tel, nil, w, r)
	}))
	defer server.Close()

	reqBody := `{"model":"public-model","messages":[{"role":"user","content":"hello"}],"stream":true}`
	resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read stream body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, `"chat.completion.chunk"`) {
		t.Fatalf("expected openai chunk stream, got %s", text)
	}
	if !strings.Contains(text, `"content":"Hel"`) || !strings.Contains(text, `"content":"lo"`) {
		t.Fatalf("expected bridged content chunks, got %s", text)
	}
	if !strings.Contains(text, `"prompt_tokens":17`) || !strings.Contains(text, `"cached_tokens":3`) {
		t.Fatalf("expected bridged usage chunk, got %s", text)
	}
	if !strings.Contains(text, `data: [DONE]`) {
		t.Fatalf("expected [DONE] terminator, got %s", text)
	}

	select {
	case event := <-tel.events:
		if event.Payload.RouteMode != "bridged" {
			t.Fatalf("route_mode = %q, want bridged", event.Payload.RouteMode)
		}
		if event.Payload.PromptTokens != 17 || event.Payload.CachedPromptTokens != 3 || event.Payload.CompletionTokens != 6 {
			t.Fatalf("unexpected telemetry usage: %+v", event.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("expected telemetry event")
	}
}

func TestHandleChatCompletionBridgesAnthropicToolUseStream(t *testing.T) {
	routingSequence.Store(0)
	allowLocalAnthropicTestUpstreams(t)

	upstream := fakeupstream.New(func(req fakeupstream.CapturedRequest) fakeupstream.Response {
		return fakeupstream.Response{
			StatusCode: http.StatusOK,
			Headers:    http.Header{"Content-Type": []string{"text/event-stream"}},
			Stream: []fakeupstream.StreamChunk{
				{Data: "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_tool_stream\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-4-6\",\"usage\":{\"input_tokens\":10}}}\n\n"},
				{Data: "data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_456\",\"name\":\"lookup_weather\"}}\n\n"},
				{Data: "data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"city\\\":\\\"Shang\"}}\n\n"},
				{Data: "data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"hai\\\"}\"}}\n\n"},
				{Data: "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":3}}\n\n"},
				{Data: "data: {\"type\":\"message_stop\"}\n\n"},
			},
		}
	})
	defer upstream.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleChatCompletion(r.Context(), anthropicBridgeSnapshot(upstream.URL()), NewRuntimeState(), nil, nil, w, r)
	}))
	defer server.Close()

	reqBody := `{"model":"public-model","messages":[{"role":"user","content":"weather?"}],"stream":true}`
	resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read stream body: %v", err)
	}
	text := string(body)
	type chunk struct {
		Choices []struct {
			Delta struct {
				ToolCalls []struct {
					Index    int    `json:"index"`
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}

	var (
		sawToolStart  bool
		sawArgChunk1  bool
		sawArgChunk2  bool
		sawFinishTool bool
	)
	for _, event := range strings.Split(text, "\n\n") {
		event = strings.TrimSpace(event)
		if !strings.HasPrefix(event, "data: ") || event == "data: [DONE]" {
			continue
		}
		var payload chunk
		if err := json.Unmarshal([]byte(strings.TrimPrefix(event, "data: ")), &payload); err != nil {
			t.Fatalf("unmarshal SSE chunk: %v", err)
		}
		if len(payload.Choices) == 0 {
			continue
		}
		choice := payload.Choices[0]
		if choice.FinishReason == "tool_calls" {
			sawFinishTool = true
		}
		if len(choice.Delta.ToolCalls) == 0 {
			continue
		}
		toolCall := choice.Delta.ToolCalls[0]
		if toolCall.ID == "toolu_456" && toolCall.Function.Name == "lookup_weather" && toolCall.Function.Arguments == "" && toolCall.Index == 0 && toolCall.Type == "function" {
			sawToolStart = true
		}
		if toolCall.Function.Arguments == "{\"city\":\"Shang" && toolCall.Index == 0 {
			sawArgChunk1 = true
		}
		if toolCall.Function.Arguments == "hai\"}" && toolCall.Index == 0 {
			sawArgChunk2 = true
		}
	}
	if !sawToolStart || !sawArgChunk1 || !sawArgChunk2 || !sawFinishTool {
		t.Fatalf("missing tool-use bridge chunks: start=%v arg1=%v arg2=%v finish=%v body=%s", sawToolStart, sawArgChunk1, sawArgChunk2, sawFinishTool, text)
	}
}

func TestHandleChatCompletionForwardsPriorToolMessagesToAnthropic(t *testing.T) {
	routingSequence.Store(0)
	allowLocalAnthropicTestUpstreams(t)

	upstream := fakeupstream.New(func(req fakeupstream.CapturedRequest) fakeupstream.Response {
		return fakeupstream.Response{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"id":"msg_ok","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn","usage":{"input_tokens":9,"output_tokens":3}}`),
		}
	})
	defer upstream.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleChatCompletion(r.Context(), anthropicBridgeSnapshot(upstream.URL()), NewRuntimeState(), nil, nil, w, r)
	}))
	defer server.Close()

	reqBody := `{
		"model":"public-model",
		"messages":[
			{"role":"system","content":"You are helpful"},
			{"role":"user","content":"weather?"},
			{"role":"assistant","content":"I'll check","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup_weather","arguments":"{\"city\":\"Shanghai\"}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"{\"temp\":22}"},
			{"role":"assistant","content":null,"function_call":{"name":"lookup_weather","arguments":"{\"city\":\"Hangzhou\"}"}},
			{"role":"function","name":"lookup_weather","content":"{\"temp\":26}"},
			{"role":"user","content":"summarize both"}
		]
	}`
	resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	requests := upstream.Requests()
	if len(requests) != 1 {
		t.Fatalf("expected 1 upstream request, got %d", len(requests))
	}

	var forwarded map[string]any
	if err := json.Unmarshal(requests[0].Body, &forwarded); err != nil {
		t.Fatalf("unmarshal forwarded body: %v", err)
	}
	messages := forwarded["messages"].([]any)
	if len(messages) != 6 {
		t.Fatalf("anthropic messages len = %d, want 6", len(messages))
	}

	assistantWithTools := messages[1].(map[string]any)
	assistantBlocks := assistantWithTools["content"].([]any)
	if assistantBlocks[1].(map[string]any)["type"] != "tool_use" {
		t.Fatalf("expected assistant tool_use block, got %#v", assistantBlocks[1])
	}
	if assistantBlocks[1].(map[string]any)["id"] != "call_1" {
		t.Fatalf("tool_use id = %#v, want call_1", assistantBlocks[1].(map[string]any)["id"])
	}

	toolResult := messages[2].(map[string]any)
	resultBlocks := toolResult["content"].([]any)
	if resultBlocks[0].(map[string]any)["tool_use_id"] != "call_1" {
		t.Fatalf("tool_result id = %#v, want call_1", resultBlocks[0].(map[string]any)["tool_use_id"])
	}

	legacyAssistant := messages[3].(map[string]any)
	legacyBlocks := legacyAssistant["content"].([]any)
	if legacyBlocks[0].(map[string]any)["id"] != "function_lookup_weather" {
		t.Fatalf("legacy function tool_use id = %#v, want function_lookup_weather", legacyBlocks[0].(map[string]any)["id"])
	}

	legacyResult := messages[4].(map[string]any)
	legacyResultBlocks := legacyResult["content"].([]any)
	if legacyResultBlocks[0].(map[string]any)["tool_use_id"] != "function_lookup_weather" {
		t.Fatalf("legacy tool_result id = %#v, want function_lookup_weather", legacyResultBlocks[0].(map[string]any)["tool_use_id"])
	}
}

func TestHandleChatCompletionAdaptsAnthropicErrorsToOpenAI(t *testing.T) {
	routingSequence.Store(0)
	allowLocalAnthropicTestUpstreams(t)

	tel := &capturingTelemetryEmitter{events: make(chan telemetryingest.Event, 1)}
	upstream := fakeupstream.New(func(req fakeupstream.CapturedRequest) fakeupstream.Response {
		return fakeupstream.Response{
			StatusCode: http.StatusBadRequest,
			Body:       []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"bad request from anthropic"}}`),
		}
	})
	defer upstream.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleChatCompletion(r.Context(), anthropicBridgeSnapshot(upstream.URL()), NewRuntimeState(), tel, nil, w, r)
	}))
	defer server.Close()

	reqBody := `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`
	resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"message":"bad request from anthropic"`) {
		t.Fatalf("expected openai-style error body, got %s", body)
	}

	select {
	case event := <-tel.events:
		if event.Payload.RouteMode != "bridged" {
			t.Fatalf("route_mode = %q, want bridged", event.Payload.RouteMode)
		}
		if event.Payload.Error != "bad request from anthropic" {
			t.Fatalf("error message = %q, want adapted anthropic message", event.Payload.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("expected telemetry event")
	}
}

func TestHandleChatCompletionBridgeFallbackWritesBridgeFallbackTelemetry(t *testing.T) {
	routingSequence.Store(0)
	allowLocalAnthropicTestUpstreams(t)

	tel := &capturingTelemetryEmitter{events: make(chan telemetryingest.Event, 1)}
	primary := fakeupstream.New(func(req fakeupstream.CapturedRequest) fakeupstream.Response {
		return fakeupstream.Response{
			StatusCode: http.StatusTooManyRequests,
			Body:       []byte(`{"type":"error","error":{"type":"rate_limit_error","message":"primary quota exceeded"}}`),
		}
	})
	defer primary.Close()

	fallback := fakeupstream.New(func(req fakeupstream.CapturedRequest) fakeupstream.Response {
		return fakeupstream.Response{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"id":"msg_fb","type":"message","role":"assistant","model":"claude-haiku-3-5","content":[{"type":"text","text":"fallback ok"}],"stop_reason":"end_turn","usage":{"input_tokens":9,"cache_read_input_tokens":2,"output_tokens":4}}`),
		}
	})
	defer fallback.Close()

	snap := &snapshot.Snapshot{
		Ingress: snapshot.IngressConfig{MaxBodyBytes: 1 << 20},
		RoutingPolicy: snapshot.RoutingPolicy{
			Retry: snapshot.RetryPolicy{
				StatusCodes: []int{http.StatusRequestTimeout, http.StatusTooManyRequests},
			},
		},
		Providers: []snapshot.ProviderSnapshot{
			anthropicProviderSnapshot("anthropic-primary", primary.URL(), "public-model", "claude-sonnet-4-6", []string{"fallback-public-model"}),
			anthropicProviderSnapshot("anthropic-fallback", fallback.URL(), "fallback-public-model", "claude-haiku-3-5", nil),
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleChatCompletion(r.Context(), snap, NewRuntimeState(), tel, nil, w, r)
	}))
	defer server.Close()

	reqBody := `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`
	resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(primary.Requests()) != 1 || len(fallback.Requests()) != 1 {
		t.Fatalf("expected one request to each upstream, got primary=%d fallback=%d", len(primary.Requests()), len(fallback.Requests()))
	}
	if primary.Requests()[0].Path != "/v1/messages" || fallback.Requests()[0].Path != "/v1/messages" {
		t.Fatalf("expected bridged /v1/messages path, got primary=%q fallback=%q", primary.Requests()[0].Path, fallback.Requests()[0].Path)
	}
	if !strings.Contains(string(fallback.Requests()[0].Body), `"model":"claude-haiku-3-5"`) {
		t.Fatalf("expected fallback upstream model rewrite, got %s", fallback.Requests()[0].Body)
	}

	select {
	case event := <-tel.events:
		if event.Payload.RouteMode != "bridge_fallback" {
			t.Fatalf("route_mode = %q, want bridge_fallback", event.Payload.RouteMode)
		}
		if event.Payload.ProviderID != "anthropic-fallback" {
			t.Fatalf("provider_id = %q, want anthropic-fallback", event.Payload.ProviderID)
		}
		if event.Payload.EffectiveModel != "claude-haiku-3-5" {
			t.Fatalf("effective_model = %q, want claude-haiku-3-5", event.Payload.EffectiveModel)
		}
		if event.Payload.CachedPromptTokens != 2 {
			t.Fatalf("cached_prompt_tokens = %d, want 2", event.Payload.CachedPromptTokens)
		}
	case <-time.After(time.Second):
		t.Fatal("expected telemetry event")
	}
}

func TestHandleChatCompletionReturnsBadGatewayOnMalformedAnthropicJSON(t *testing.T) {
	routingSequence.Store(0)
	allowLocalAnthropicTestUpstreams(t)

	tel := &capturingTelemetryEmitter{events: make(chan telemetryingest.Event, 1)}
	upstream := fakeupstream.New(func(req fakeupstream.CapturedRequest) fakeupstream.Response {
		return fakeupstream.Response{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"id":"bad-json"`),
		}
	})
	defer upstream.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleChatCompletion(r.Context(), anthropicBridgeSnapshot(upstream.URL()), NewRuntimeState(), tel, nil, w, r)
	}))
	defer server.Close()

	reqBody := `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`
	resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "failed to adapt upstream response") {
		t.Fatalf("expected adapt failure body, got %s", body)
	}

	select {
	case event := <-tel.events:
		if event.Payload.Error == "" || !strings.Contains(event.Payload.Error, "decode anthropic response") {
			t.Fatalf("expected decode anthropic response error, got %q", event.Payload.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("expected telemetry event")
	}
}

func TestHandleChatCompletionReturnsGatewayTimeoutOnAnthropicTimeout(t *testing.T) {
	routingSequence.Store(0)
	allowLocalAnthropicTestUpstreams(t)

	tel := &capturingTelemetryEmitter{events: make(chan telemetryingest.Event, 1)}
	upstream := fakeupstream.New(func(req fakeupstream.CapturedRequest) fakeupstream.Response {
		return fakeupstream.Response{
			Delay:      200 * time.Millisecond,
			StatusCode: http.StatusOK,
			Body:       []byte(`{"id":"msg_timeout","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"too slow"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`),
		}
	})
	defer upstream.Close()

	snap := anthropicBridgeSnapshot(upstream.URL())
	snap.Providers[0].ExecutionPolicy.TimeoutMs = 25

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleChatCompletion(r.Context(), snap, NewRuntimeState(), tel, nil, w, r)
	}))
	defer server.Close()

	reqBody := `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`
	resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", resp.StatusCode)
	}

	select {
	case event := <-tel.events:
		if event.Payload.Error == "" || !strings.Contains(event.Payload.Error, "context deadline exceeded") {
			t.Fatalf("expected timeout error, got %q", event.Payload.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("expected telemetry event")
	}
}

func TestHandleMessagesRejectsOpenAIOnlyProvider(t *testing.T) {
	routingSequence.Store(0)

	snap := testGatewaySnapshot()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleMessages(context.Background(), snap, NewRuntimeState(), nil, nil, w, r)
	}))
	defer server.Close()

	reqBody := `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`
	resp, err := server.Client().Post(server.URL+"/v1/messages", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "anthropic_messages") {
		t.Fatalf("expected unsupported provider message, got %s", body)
	}
}

func allowLocalAnthropicTestUpstreams(t *testing.T) {
	t.Helper()
	restore := SetSSRFCheckerForTesting(proxy.NewSSRFCheckerWithConfig(proxy.SSRFConfig{
		AllowLocalhost: true,
		AllowPrivateIP: true,
	}))
	t.Cleanup(restore)
}

func anthropicBridgeSnapshot(baseURL string) *snapshot.Snapshot {
	return &snapshot.Snapshot{
		Ingress: snapshot.IngressConfig{MaxBodyBytes: 1 << 20},
		RoutingPolicy: snapshot.RoutingPolicy{
			Retry: snapshot.RetryPolicy{
				StatusCodes: []int{http.StatusRequestTimeout, http.StatusTooManyRequests},
			},
		},
		Providers: []snapshot.ProviderSnapshot{
			anthropicProviderSnapshot("anthropic-provider", baseURL, "public-model", "claude-sonnet-4-6", nil),
		},
	}
}

func anthropicProviderSnapshot(providerID string, baseURL string, publicModel string, upstreamModel string, fallbackModels []string) snapshot.ProviderSnapshot {
	return snapshot.ProviderSnapshot{
		ProviderID:       providerID,
		ProtocolAdapter:  core.ProtocolAdapterAnthropicMessages,
		BaseURL:          baseURL,
		AnthropicBaseURL: baseURL,
		Credentials: snapshot.Credentials{
			Kind:       "api_key",
			Value:      "test-anthropic-key",
			HeaderName: "x-api-key",
		},
		ModelTable: []snapshot.ModelMapping{
			{PublicModel: publicModel, UpstreamModel: upstreamModel},
		},
		ExecutionPolicy: snapshot.ExecutionPolicy{
			Enabled:   true,
			Weight:    1,
			TimeoutMs: 5000,
		},
		FallbackModels: append([]string(nil), fallbackModels...),
	}
}
