package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"ai-model-gateway/internal/contracts/telemetryingest"
	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/gateway/snapshot"
	pricinginfra "ai-model-gateway/internal/infra/pricing"
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
	restore := SetSSRFCheckerForTesting(&mockSSRFChecker{})
	defer restore()
	routingSequence.Store(0)
	swapSharedHTTPClient(t, &http.Client{})

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

func TestConvertOpenAIChatRequestToAnthropicAcceptsInputTextBlocks(t *testing.T) {
	body, err := convertOpenAIChatRequestToAnthropic([]byte(`{
		"model":"public-model",
		"messages":[
			{"role":"developer","content":[{"type":"input_text","text":"You are terse."}]},
			{"role":"user","content":[{"type":"input_text","text":"hello"}]}
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
	if payload["system"] != "You are terse." {
		t.Fatalf("system = %#v, want developer block promoted to system text", payload["system"])
	}
	messages, _ := payload["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("messages = %#v, want 1 user message", payload["messages"])
	}
	userMsg, _ := messages[0].(map[string]any)
	if userMsg["role"] != "user" {
		t.Fatalf("user role = %#v, want user", userMsg["role"])
	}
	content, _ := userMsg["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("user content = %#v, want one text block", userMsg["content"])
	}
	block, _ := content[0].(map[string]any)
	if block["type"] != "text" || block["text"] != "hello" {
		t.Fatalf("user block = %#v, want {type:text,text:hello}", block)
	}
}

func TestConvertOpenAIChatRequestToAnthropicConvertsImageURLBlocks(t *testing.T) {
	body, err := convertOpenAIChatRequestToAnthropic([]byte(`{
		"model":"public-model",
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"look at this"},
				{"type":"image_url","image_url":{"url":"https://example.com/cat.png"}}
			]}
		]
	}`), "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("convertOpenAIChatRequestToAnthropic() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode anthropic body: %v", err)
	}
	messages, _ := payload["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("messages = %#v, want 1 user message", payload["messages"])
	}
	userMsg := messages[0].(map[string]any)
	content := userMsg["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("content = %#v, want 2 blocks", userMsg["content"])
	}
	img := content[1].(map[string]any)
	if img["type"] != "image" {
		t.Fatalf("image block type = %#v, want image", img["type"])
	}
	source, _ := img["source"].(map[string]any)
	if source["type"] != "url" || source["url"] != "https://example.com/cat.png" {
		t.Fatalf("image source = %#v, want url source", source)
	}
}

func TestConvertOpenAIChatRequestToAnthropicConvertsDataURLImageBlocksCaseInsensitive(t *testing.T) {
	body, err := convertOpenAIChatRequestToAnthropic([]byte(`{
		"model":"public-model",
		"messages":[
			{"role":"user","content":[
				{"type":"image_url","image_url":{"url":"DATA:image/png;base64,AAAA"}}
			]}
		]
	}`), "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("convertOpenAIChatRequestToAnthropic() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode anthropic body: %v", err)
	}
	messages, _ := payload["messages"].([]any)
	userMsg := messages[0].(map[string]any)
	content := userMsg["content"].([]any)
	img := content[0].(map[string]any)
	source, _ := img["source"].(map[string]any)
	if source["type"] != "base64" || source["media_type"] != "image/png" || source["data"] != "AAAA" {
		t.Fatalf("image source = %#v, want base64 image source", source)
	}
}

func TestConvertOpenAIChatRequestToAnthropicNormalizesNullToolDescription(t *testing.T) {
	body, err := convertOpenAIChatRequestToAnthropic([]byte(`{
		"model":"public-model",
		"tools":[
			{"type":"function","function":{"name":"bash","description":null,"parameters":{"type":"object"}}}
		],
		"messages":[{"role":"user","content":"hi"}]
	}`), "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("convertOpenAIChatRequestToAnthropic() error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode anthropic body: %v", err)
	}
	tools, _ := payload["tools"].([]any)
	tool := tools[0].(map[string]any)
	if tool["description"] != "" {
		t.Fatalf("description = %#v, want empty string", tool["description"])
	}
}

func TestConvertOpenAIChatRequestToAnthropicNormalizesMissingToolDescription(t *testing.T) {
	body, err := convertOpenAIChatRequestToAnthropic([]byte(`{
		"model":"public-model",
		"tools":[
			{"type":"function","function":{"name":"bash","parameters":{"type":"object"}}}
		],
		"messages":[{"role":"user","content":"hi"}]
	}`), "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("convertOpenAIChatRequestToAnthropic() error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode anthropic body: %v", err)
	}
	tools, _ := payload["tools"].([]any)
	tool := tools[0].(map[string]any)
	if tool["description"] != "" {
		t.Fatalf("description = %#v, want empty string", tool["description"])
	}
}

func TestConvertOpenAIChatRequestToAnthropicAcceptsResponsesStyleFunctionTool(t *testing.T) {
	body, err := convertOpenAIChatRequestToAnthropic([]byte(`{
		"model":"public-model",
		"tools":[
			{"type":"function","name":"bash","description":null,"parameters":{"type":"object"}}
		],
		"messages":[{"role":"user","content":"hi"}]
	}`), "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("convertOpenAIChatRequestToAnthropic() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode anthropic body: %v", err)
	}
	tools, _ := payload["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %#v, want 1 tool", payload["tools"])
	}
	tool := tools[0].(map[string]any)
	if tool["name"] != "bash" {
		t.Fatalf("tool.name = %#v, want bash", tool["name"])
	}
	if tool["description"] != "" {
		t.Fatalf("description = %#v, want empty string", tool["description"])
	}
}

func TestHandleChatCompletionBridgesAnthropicToolUseJSONResponse(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)
	routingSequence.Store(0)
	swapSharedHTTPClient(t, &http.Client{})

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

func TestConvertAnthropicRequestToOpenAIChatConvertsImageBlocks(t *testing.T) {
	body, err := convertAnthropicRequestToOpenAIChat([]byte(`{
		"model":"claude-sonnet-4-6",
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"look"},
				{"type":"image","source":{"type":"url","url":"https://example.com/dog.png"}}
			]}
		]
	}`), "gpt-5.4")
	if err != nil {
		t.Fatalf("convertAnthropicRequestToOpenAIChat() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode openai body: %v", err)
	}
	messages, _ := payload["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("messages = %#v, want 1 message", payload["messages"])
	}
	msg := messages[0].(map[string]any)
	parts, ok := msg["content"].([]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("content = %#v, want 2 parts", msg["content"])
	}
	img := parts[1].(map[string]any)
	if img["type"] != "image_url" {
		t.Fatalf("image part type = %#v, want image_url", img["type"])
	}
	imageURL, _ := img["image_url"].(map[string]any)
	if imageURL["url"] != "https://example.com/dog.png" {
		t.Fatalf("image_url = %#v, want https://example.com/dog.png", img["image_url"])
	}
}

func TestConvertAnthropicRequestToOpenAIChatConvertsBase64ImageBlocks(t *testing.T) {
	body, err := convertAnthropicRequestToOpenAIChat([]byte(`{
		"model":"claude-sonnet-4-6",
		"messages":[
			{"role":"user","content":[
				{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}
			]}
		]
	}`), "gpt-5.4")
	if err != nil {
		t.Fatalf("convertAnthropicRequestToOpenAIChat() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode openai body: %v", err)
	}
	messages, _ := payload["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("messages = %#v, want 1 message", payload["messages"])
	}
	msg := messages[0].(map[string]any)
	parts, ok := msg["content"].([]any)
	if !ok || len(parts) != 1 {
		t.Fatalf("content = %#v, want 1 part", msg["content"])
	}
	img := parts[0].(map[string]any)
	imageURL, _ := img["image_url"].(map[string]any)
	if imageURL["url"] != "data:image/png;base64,AAAA" {
		t.Fatalf("image_url.url = %#v, want data:image/png;base64,AAAA", imageURL["url"])
	}
}

func TestConvertAnthropicRequestToOpenAIChatNormalizesNullToolDescription(t *testing.T) {
	body, err := convertAnthropicRequestToOpenAIChat([]byte(`{
		"model":"claude-sonnet-4-6",
		"tools":[
			{"name":"lookup_weather","description":null,"input_schema":{"type":"object"}}
		],
		"messages":[{"role":"user","content":"hi"}]
	}`), "gpt-5.4")
	if err != nil {
		t.Fatalf("convertAnthropicRequestToOpenAIChat() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode openai body: %v", err)
	}
	tools, _ := payload["tools"].([]any)
	tool := tools[0].(map[string]any)
	function := tool["function"].(map[string]any)
	if function["description"] != "" {
		t.Fatalf("description = %#v, want empty string", function["description"])
	}
}

func TestHandleChatCompletionBridgesAnthropicStreamAndUsage(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)
	routingSequence.Store(0)
	swapSharedHTTPClient(t, &http.Client{})

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
	allowLocalAnthropicTestUpstreams(t)
	routingSequence.Store(0)
	swapSharedHTTPClient(t, &http.Client{})

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
	restore := SetSSRFCheckerForTesting(nil)
	defer restore()
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

func TestHandleMessagesBridgesOpenAIOnlyProvider(t *testing.T) {
	restore := SetSSRFCheckerForTesting(nil)
	defer restore()
	routingSequence.Store(0)
	allowLocalAnthropicTestUpstreams(t)

	upstream := fakeupstream.New(func(req fakeupstream.CapturedRequest) fakeupstream.Response {
		if req.Path != "/v1/chat/completions" {
			t.Fatalf("upstream path = %q, want /v1/chat/completions", req.Path)
		}
		var forwarded map[string]any
		if err := json.Unmarshal(req.Body, &forwarded); err != nil {
			t.Fatalf("decode forwarded body: %v", err)
		}
		if forwarded["model"] != testUpstreamModel {
			t.Fatalf("forwarded model = %v, want %s", forwarded["model"], testUpstreamModel)
		}
		return fakeupstream.Response{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"id":"chatcmpl_glm","object":"chat.completion","model":"glm-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`),
		}
	})
	defer upstream.Close()

	snap := testGatewaySnapshot()
	snap.Providers[0].BaseURL = upstream.URL()
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

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var decoded anthropicResponsePayload
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, body)
	}
	if decoded.Model != testPublicModel || anthropicBlocksToText(decoded.Content) != "ok" {
		t.Fatalf("unexpected bridged response: %#v", decoded)
	}
}

func TestHandleMessagesAppliesModelBridgeBeforeProviderSelection(t *testing.T) {
	restore := SetSSRFCheckerForTesting(nil)
	defer restore()
	routingSequence.Store(0)
	allowLocalAnthropicTestUpstreams(t)

	upstream := fakeupstream.New(func(req fakeupstream.CapturedRequest) fakeupstream.Response {
		if req.Path != "/v1/chat/completions" {
			t.Fatalf("upstream path = %q, want /v1/chat/completions", req.Path)
		}
		var forwarded map[string]any
		if err := json.Unmarshal(req.Body, &forwarded); err != nil {
			t.Fatalf("decode forwarded body: %v", err)
		}
		if forwarded["model"] != "GLM-5.1" {
			t.Fatalf("forwarded model = %v, want GLM-5.1", forwarded["model"])
		}
		return fakeupstream.Response{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"id":"chatcmpl_glm","object":"chat.completion","model":"GLM-5.1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`),
		}
	})
	defer upstream.Close()

	snap := &snapshot.Snapshot{
		Ingress: snapshot.IngressConfig{MaxBodyBytes: 1 << 20},
		CompatPolicy: snapshot.CompatPolicy{
			Bridge: snapshot.BridgePolicy{
				Enabled: true,
				Rules: []snapshot.BridgeRule{
					{From: "claude-opus-4-*", To: "GLM-5.1"},
				},
			},
		},
		Providers: []snapshot.ProviderSnapshot{
			{
				ProviderID: "glm-provider",
				BaseURL:    upstream.URL(),
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "GLM-5.1", UpstreamModel: "GLM-5.1"},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{Enabled: true, Weight: 1, TimeoutMs: 5000},
			},
		},
	}
	tel := &capturingTelemetryEmitter{events: make(chan telemetryingest.Event, 1)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleMessages(context.Background(), snap, NewRuntimeState(), tel, nil, w, r)
	}))
	defer server.Close()

	reqBody := `{"model":"claude-opus-4-6[1m]","messages":[{"role":"user","content":"hello"}]}`
	resp, err := server.Client().Post(server.URL+"/v1/messages", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	body, _ := io.ReadAll(resp.Body)
	var decoded anthropicResponsePayload
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, body)
	}
	if decoded.Model != "claude-opus-4-6[1m]" || anthropicBlocksToText(decoded.Content) != "ok" {
		t.Fatalf("unexpected bridged response: %#v", decoded)
	}

	select {
	case event := <-tel.events:
		if event.Payload.ProviderID != "glm-provider" || event.Payload.RequestedModel != "claude-opus-4-6[1m]" || event.Payload.EffectiveModel != "GLM-5.1" {
			t.Fatalf("unexpected telemetry payload: %#v", event.Payload)
		}
		if event.Payload.RouteMode != "model_bridge" {
			t.Fatalf("route_mode = %q, want model_bridge", event.Payload.RouteMode)
		}
	case <-time.After(time.Second):
		t.Fatal("expected telemetry event")
	}
}

func TestHandleMessagesNormalizesNullToolDescriptionBeforeForward(t *testing.T) {
	restore := SetSSRFCheckerForTesting(nil)
	defer restore()
	routingSequence.Store(0)
	allowLocalAnthropicTestUpstreams(t)

	upstream := fakeupstream.New(func(req fakeupstream.CapturedRequest) fakeupstream.Response {
		if req.Path != "/v1/chat/completions" {
			t.Fatalf("upstream path = %q, want /v1/chat/completions", req.Path)
		}
		var forwarded map[string]any
		if err := json.Unmarshal(req.Body, &forwarded); err != nil {
			t.Fatalf("decode forwarded body: %v", err)
		}
		tools, ok := forwarded["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("forwarded tools = %#v, want 1 function tool", forwarded["tools"])
		}
		tool := tools[0].(map[string]any)
		function := tool["function"].(map[string]any)
		if function["description"] != "" {
			t.Fatalf("forwarded function.description = %#v, want empty string", function["description"])
		}
		return fakeupstream.Response{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"id":"chatcmpl_ok","object":"chat.completion","model":"upstream-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`),
		}
	})
	defer upstream.Close()

	snap := testGatewaySnapshot()
	snap.Providers[0].BaseURL = upstream.URL()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleMessages(context.Background(), snap, NewRuntimeState(), nil, nil, w, r)
	}))
	defer server.Close()

	reqBody := `{
		"model":"public-model",
		"messages":[{"role":"user","content":"hello"}],
		"tools":[
			{"type":"function","function":{"name":"bash","description":null,"parameters":{"type":"object"}}}
		]
	}`
	resp, err := server.Client().Post(server.URL+"/v1/messages", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, body)
	}
}

func TestHandleChatCompletionNormalizesResponsesStyleNullToolDescriptionBeforeForward(t *testing.T) {
	restore := SetSSRFCheckerForTesting(nil)
	defer restore()
	routingSequence.Store(0)
	allowLocalAnthropicTestUpstreams(t)

	upstream := fakeupstream.New(func(req fakeupstream.CapturedRequest) fakeupstream.Response {
		if req.Path != "/v1/chat/completions" {
			t.Fatalf("upstream path = %q, want /v1/chat/completions", req.Path)
		}
		var forwarded map[string]any
		if err := json.Unmarshal(req.Body, &forwarded); err != nil {
			t.Fatalf("decode forwarded body: %v", err)
		}
		tools, ok := forwarded["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("forwarded tools = %#v, want 1 function tool", forwarded["tools"])
		}
		tool := tools[0].(map[string]any)
		function := tool["function"].(map[string]any)
		if function["description"] != "" {
			t.Fatalf("forwarded function.description = %#v, want empty string", function["description"])
		}
		return fakeupstream.Response{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"id":"chatcmpl_ok","object":"chat.completion","model":"upstream-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`),
		}
	})
	defer upstream.Close()

	snap := testGatewaySnapshot()
	snap.Providers[0].BaseURL = upstream.URL()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleChatCompletion(context.Background(), snap, NewRuntimeState(), nil, nil, w, r)
	}))
	defer server.Close()

	reqBody := `{
		"model":"public-model",
		"messages":[{"role":"user","content":"hello"}],
		"tools":[
			{"type":"function","name":"bash","description":null,"parameters":{"type":"object"}}
		]
	}`
	resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, body)
	}
}

func TestConvertResponsesRequestToChatWithStringInput(t *testing.T) {
	body, err := convertResponsesRequestToChat([]byte(`{"model":"gpt-4o","input":"hello","stream":false}`), "gpt-4o-turbo")
	if err != nil {
		t.Fatalf("convertResponsesRequestToChat() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode chat body: %v", err)
	}
	if payload["model"] != "gpt-4o-turbo" {
		t.Fatalf("model = %#v, want gpt-4o-turbo", payload["model"])
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages = %#v, want 1 message", payload["messages"])
	}
	msg := messages[0].(map[string]any)
	if msg["role"] != "user" || msg["content"] != "hello" {
		t.Fatalf("message = %#v, want role=user content=hello", msg)
	}
	if payload["stream"] != false {
		t.Fatalf("stream = %#v, want false", payload["stream"])
	}
}

func TestConvertResponsesRequestToChatPrependsInstructions(t *testing.T) {
	body, err := convertResponsesRequestToChat([]byte(`{
		"model":"gpt-4o",
		"instructions":"You are a careful coding agent.",
		"input":"edit the file"
	}`), "")
	if err != nil {
		t.Fatalf("convertResponsesRequestToChat() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode chat body: %v", err)
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("messages = %#v, want system plus user message", payload["messages"])
	}
	systemMsg := messages[0].(map[string]any)
	if systemMsg["role"] != "system" || systemMsg["content"] != "You are a careful coding agent." {
		t.Fatalf("system message = %#v, want instructions preserved", systemMsg)
	}
	userMsg := messages[1].(map[string]any)
	if userMsg["role"] != "user" || userMsg["content"] != "edit the file" {
		t.Fatalf("user message = %#v, want original input", userMsg)
	}
}

func TestConvertResponsesRequestToChatWithArrayInput(t *testing.T) {
	body, err := convertResponsesRequestToChat([]byte(`{
		"model":"gpt-4o",
		"input":[
			{"role":"system","content":"You are helpful"},
			{"role":"user","content":"hello"}
		],
		"stream":true,
		"max_output_tokens":100
	}`), "")
	if err != nil {
		t.Fatalf("convertResponsesRequestToChat() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode chat body: %v", err)
	}
	if payload["model"] != "gpt-4o" {
		t.Fatalf("model = %#v, want gpt-4o", payload["model"])
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("messages = %#v, want 2 messages", payload["messages"])
	}
	if payload["max_tokens"] != float64(100) {
		t.Fatalf("max_tokens = %#v, want 100", payload["max_tokens"])
	}
	if payload["stream"] != true {
		t.Fatalf("stream = %#v, want true", payload["stream"])
	}
}

func TestConvertResponsesRequestToChatCopiesParallelToolCalls(t *testing.T) {
	body, err := convertResponsesRequestToChat([]byte(`{
		"model":"gpt-4o",
		"input":"use one tool at a time",
		"parallel_tool_calls":false
	}`), "")
	if err != nil {
		t.Fatalf("convertResponsesRequestToChat() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode chat body: %v", err)
	}
	if payload["parallel_tool_calls"] != false {
		t.Fatalf("parallel_tool_calls = %#v, want false", payload["parallel_tool_calls"])
	}
}

func TestConvertResponsesRequestToChatMissingInput(t *testing.T) {
	_, err := convertResponsesRequestToChat([]byte(`{"model":"gpt-4o"}`), "")
	if err == nil {
		t.Fatal("expected error for missing input field")
	}
}

func TestConvertResponsesRequestToChatWithTools(t *testing.T) {
	body, err := convertResponsesRequestToChat([]byte(`{
		"model":"gpt-4o",
		"input":"what's the weather?",
		"tools":[{"type":"function","function":{"name":"get_weather","description":"Get weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}]
	}`), "")
	if err != nil {
		t.Fatalf("convertResponsesRequestToChat() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode chat body: %v", err)
	}
	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want 1 tool", payload["tools"])
	}
}

func TestConvertResponsesRequestToChatWithResponsesStyleFunctionTools(t *testing.T) {
	body, err := convertResponsesRequestToChat([]byte(`{
		"model":"gpt-4o",
		"input":"what's the weather?",
		"tools":[{"type":"function","name":"get_weather","description":null,"parameters":{"type":"object","properties":{"city":{"type":"string"}}}}]
	}`), "")
	if err != nil {
		t.Fatalf("convertResponsesRequestToChat() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode chat body: %v", err)
	}
	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want 1 tool", payload["tools"])
	}
	tool := tools[0].(map[string]any)
	function := tool["function"].(map[string]any)
	if function["name"] != "get_weather" {
		t.Fatalf("function.name = %#v, want get_weather", function["name"])
	}
	if function["description"] != "" {
		t.Fatalf("function.description = %#v, want empty string", function["description"])
	}
}

func TestConvertResponsesRequestToChatDropsUnsupportedBuiltInTool(t *testing.T) {
	body, err := convertResponsesRequestToChat([]byte(`{
		"model":"gpt-4o",
		"input":"search the web",
		"tools":[{"type":"web_search_preview"}]
	}`), "")
	if err != nil {
		t.Fatalf("convertResponsesRequestToChat() error = %v, want nil", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode forwarded body: %v", err)
	}
	if _, ok := decoded["tools"]; ok {
		t.Fatalf("expected no tools in forwarded body; got %#v", decoded["tools"])
	}
}

func TestConvertResponsesRequestToChatDropsUnsupportedToolChoice(t *testing.T) {
	body, err := convertResponsesRequestToChat([]byte(`{
		"model":"gpt-4o",
		"input":"search",
		"tools":[{"type":"web_search"}],
		"tool_choice":{"type":"web_search"}
	}`), "")
	if err != nil {
		t.Fatalf("convertResponsesRequestToChat() error = %v, want nil", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode forwarded body: %v", err)
	}
	if got := decoded["tool_choice"]; got != "auto" {
		t.Fatalf("tool_choice = %v, want \"auto\"", got)
	}
}

func TestConvertResponsesRequestToChatBridgesCustomToolAsFunction(t *testing.T) {
	body, err := convertResponsesRequestToChat([]byte(`{
		"model":"gpt-5.5",
		"input":"edit the file",
		"tools":[{"type":"custom","name":"apply_patch","description":"Apply a patch"}],
		"tool_choice":"auto"
	}`), "")
	if err != nil {
		t.Fatalf("convertResponsesRequestToChat() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode chat body: %v", err)
	}
	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want 1 tool", payload["tools"])
	}
	tool := tools[0].(map[string]any)
	if tool["type"] != "function" {
		t.Fatalf("tool.type = %#v, want function", tool["type"])
	}
	function := tool["function"].(map[string]any)
	if function["name"] != "apply_patch" {
		t.Fatalf("function.name = %#v, want apply_patch", function["name"])
	}
	parameters := function["parameters"].(map[string]any)
	properties := parameters["properties"].(map[string]any)
	if _, ok := properties["input"]; !ok {
		t.Fatalf("parameters = %#v, want input property", parameters)
	}
}

func TestConvertResponsesRequestToChatBridgesCustomToolChoice(t *testing.T) {
	body, err := convertResponsesRequestToChat([]byte(`{
		"model":"gpt-5.5",
		"input":"edit the file",
		"tools":[{"type":"custom","name":"apply_patch","description":"Apply a patch"}],
		"tool_choice":{"type":"custom","name":"apply_patch"}
	}`), "")
	if err != nil {
		t.Fatalf("convertResponsesRequestToChat() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode chat body: %v", err)
	}
	toolChoice, ok := payload["tool_choice"].(map[string]any)
	if !ok {
		t.Fatalf("tool_choice = %#v, want object", payload["tool_choice"])
	}
	if toolChoice["type"] != "function" {
		t.Fatalf("tool_choice.type = %#v, want function", toolChoice["type"])
	}
	function, ok := toolChoice["function"].(map[string]any)
	if !ok {
		t.Fatalf("tool_choice.function = %#v, want object", toolChoice["function"])
	}
	if function["name"] != "apply_patch" {
		t.Fatalf("tool_choice.function.name = %#v, want apply_patch", function["name"])
	}
	if _, exists := toolChoice["name"]; exists {
		t.Fatalf("tool_choice unexpectedly kept Responses custom name field: %#v", toolChoice)
	}
}

func TestConvertResponsesRequestToChatNormalizesObjectToolChoiceModes(t *testing.T) {
	for _, mode := range []string{"auto", "required", "none"} {
		t.Run(mode, func(t *testing.T) {
			body, err := convertResponsesRequestToChat([]byte(`{
				"model":"gpt-5.5",
				"input":"edit the file",
				"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],
				"tool_choice":{"type":`+strconv.Quote(mode)+`}
			}`), "")
			if err != nil {
				t.Fatalf("convertResponsesRequestToChat() error = %v", err)
			}

			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode chat body: %v", err)
			}
			if payload["tool_choice"] != mode {
				t.Fatalf("tool_choice = %#v, want %q", payload["tool_choice"], mode)
			}
		})
	}
}

func TestConvertResponsesRequestToChatPreservesCustomToolConversation(t *testing.T) {
	body, err := convertResponsesRequestToChat([]byte(`{
		"model":"gpt-5.5",
		"input":[
			{"role":"user","content":"edit the file"},
			{"type":"custom_tool_call","call_id":"call_patch_1","name":"apply_patch","input":"*** Begin Patch\n*** Add File: probe.txt\n+ok\n*** End Patch\n"},
			{"type":"custom_tool_call_output","call_id":"call_patch_1","output":"{\"output\":\"Success\"}"}
		]
	}`), "")
	if err != nil {
		t.Fatalf("convertResponsesRequestToChat() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode chat body: %v", err)
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 3 {
		t.Fatalf("messages = %#v, want 3 messages", payload["messages"])
	}
	assistant := messages[1].(map[string]any)
	toolCalls, ok := assistant["tool_calls"].([]any)
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("assistant tool_calls = %#v, want 1 custom tool call", assistant["tool_calls"])
	}
	call := toolCalls[0].(map[string]any)
	if call["id"] != "call_patch_1" || call["type"] != "function" {
		t.Fatalf("tool call = %#v, want bridged function call_patch_1", call)
	}
	function := call["function"].(map[string]any)
	if function["name"] != "apply_patch" {
		t.Fatalf("function.name = %#v, want apply_patch", function["name"])
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(function["arguments"].(string)), &args); err != nil {
		t.Fatalf("decode function arguments: %v", err)
	}
	if !strings.Contains(args["input"].(string), "probe.txt") {
		t.Fatalf("arguments = %#v, want patch input", args)
	}
	toolResult := messages[2].(map[string]any)
	if toolResult["role"] != "tool" || toolResult["tool_call_id"] != "call_patch_1" {
		t.Fatalf("tool result = %#v, want tool response for call_patch_1", toolResult)
	}
}

func TestAdaptChatResponseToResponses(t *testing.T) {
	chatResp := `{
		"id":"chatcmpl-123",
		"object":"chat.completion",
		"created":1700000000,
		"model":"gpt-4o",
		"choices":[{
			"index":0,
			"message":{"role":"assistant","content":"Hi there!"},
			"finish_reason":"stop"
		}],
		"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}
	}`
	body, err := adaptChatResponseToResponses([]byte(chatResp), "", nil)
	if err != nil {
		t.Fatalf("adaptChatResponseToResponses() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode responses body: %v", err)
	}
	if payload["id"] != "resp_chatcmpl-123" {
		t.Fatalf("id = %#v, want resp_chatcmpl-123", payload["id"])
	}
	if payload["object"] != "response" {
		t.Fatalf("object = %#v, want response", payload["object"])
	}
	if payload["model"] != "gpt-4o" {
		t.Fatalf("model = %#v, want gpt-4o", payload["model"])
	}
	if payload["status"] != "completed" {
		t.Fatalf("status = %#v, want completed", payload["status"])
	}

	output, ok := payload["output"].([]any)
	if !ok || len(output) != 1 {
		t.Fatalf("output = %#v, want 1 element", payload["output"])
	}
	msg := output[0].(map[string]any)
	if msg["type"] != "message" {
		t.Fatalf("output[0].type = %#v, want message", msg["type"])
	}
	if msg["role"] != "assistant" {
		t.Fatalf("output[0].role = %#v, want assistant", msg["role"])
	}

	content, ok := msg["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("output[0].content = %#v, want 1 element", msg["content"])
	}
	textBlock := content[0].(map[string]any)
	if textBlock["type"] != "output_text" || textBlock["text"] != "Hi there!" {
		t.Fatalf("content[0] = %#v, want type=output_text text=Hi there!", textBlock)
	}

	usage, ok := payload["usage"].(map[string]any)
	if !ok {
		t.Fatalf("usage = %#v, want map", payload["usage"])
	}
	if usage["input_tokens"] != float64(5) {
		t.Fatalf("input_tokens = %#v, want 5", usage["input_tokens"])
	}
	if usage["output_tokens"] != float64(3) {
		t.Fatalf("output_tokens = %#v, want 3", usage["output_tokens"])
	}
	if usage["total_tokens"] != float64(8) {
		t.Fatalf("total_tokens = %#v, want 8", usage["total_tokens"])
	}
}

func TestAdaptChatResponseToResponsesConvertsArrayContentText(t *testing.T) {
	chatResp := `{
		"id":"chatcmpl-array",
		"object":"chat.completion",
		"created":1700000000,
		"model":"gpt-4o",
		"choices":[{
			"index":0,
			"message":{"role":"assistant","content":[{"type":"text","text":"part one"},{"type":"output_text","text":"part two"}]},
			"finish_reason":"stop"
		}],
		"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}
	}`
	body, err := adaptChatResponseToResponses([]byte(chatResp), "", nil)
	if err != nil {
		t.Fatalf("adaptChatResponseToResponses() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode responses body: %v", err)
	}
	output := payload["output"].([]any)
	msg := output[0].(map[string]any)
	content := msg["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content = %#v, want one output_text block", content)
	}
	textBlock := content[0].(map[string]any)
	if textBlock["type"] != "output_text" || textBlock["text"] != "part one\npart two" {
		t.Fatalf("content[0] = %#v, want combined output_text", textBlock)
	}
}

func TestAdaptChatResponseToResponsesPreservesClientModel(t *testing.T) {
	chatResp := `{"id":"chatcmpl-456","model":"gpt-4o","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`
	body, err := adaptChatResponseToResponses([]byte(chatResp), "my-custom-model", nil)
	if err != nil {
		t.Fatalf("adaptChatResponseToResponses() error = %v", err)
	}

	var payload map[string]any
	json.Unmarshal(body, &payload)
	if payload["model"] != "my-custom-model" {
		t.Fatalf("model = %#v, want my-custom-model", payload["model"])
	}
}

func TestAdaptChatResponseToResponsesConvertsCustomToolCall(t *testing.T) {
	patch := "*** Begin Patch\n*** Add File: probe.txt\n+ok\n*** End Patch\n"
	chatResp := `{
		"id":"chatcmpl-custom-1",
		"object":"chat.completion",
		"created":1700000000,
		"model":"gpt-5.5",
		"choices":[{
			"index":0,
			"message":{
				"role":"assistant",
				"content":null,
				"tool_calls":[{"id":"call_patch_1","type":"custom","custom":{"name":"apply_patch","input":` + strconv.Quote(patch) + `}}]
			},
			"finish_reason":"tool_calls"
		}],
		"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}
	}`
	body, err := adaptChatResponseToResponses([]byte(chatResp), "public-model", nil)
	if err != nil {
		t.Fatalf("adaptChatResponseToResponses() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode responses body: %v", err)
	}
	output, ok := payload["output"].([]any)
	if !ok || len(output) != 1 {
		t.Fatalf("output = %#v, want one custom tool item", payload["output"])
	}
	item := output[0].(map[string]any)
	if item["type"] != "custom_tool_call" || item["call_id"] != "call_patch_1" || item["name"] != "apply_patch" {
		t.Fatalf("item = %#v, want custom_tool_call apply_patch", item)
	}
	if item["input"] != patch {
		t.Fatalf("input = %#v, want patch text", item["input"])
	}
}

func TestAdaptChatResponseToResponsesRestoresBridgedCustomToolCall(t *testing.T) {
	patch := "*** Begin Patch\n*** Add File: probe.txt\n+ok\n*** End Patch\n"
	chatResp := `{
		"id":"chatcmpl-custom-2",
		"object":"chat.completion",
		"created":1700000000,
		"model":"gpt-5.5",
		"choices":[{
			"index":0,
			"message":{
				"role":"assistant",
				"content":null,
				"tool_calls":[{"id":"call_patch_2","type":"function","function":{"name":"apply_patch","arguments":` + strconv.Quote(`{"input":`+strconv.Quote(patch)+`}`) + `}}]
			},
			"finish_reason":"tool_calls"
		}],
		"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}
	}`
	body, err := adaptChatResponseToResponses([]byte(chatResp), "public-model", map[string]struct{}{"apply_patch": {}})
	if err != nil {
		t.Fatalf("adaptChatResponseToResponses() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode responses body: %v", err)
	}
	output := payload["output"].([]any)
	item := output[0].(map[string]any)
	if item["type"] != "custom_tool_call" || item["call_id"] != "call_patch_2" || item["name"] != "apply_patch" {
		t.Fatalf("item = %#v, want restored custom_tool_call", item)
	}
	if item["input"] != patch {
		t.Fatalf("input = %#v, want patch text", item["input"])
	}
}

func TestAdaptChatResponseToResponsesConvertsLegacyFunctionCall(t *testing.T) {
	chatResp := `{
		"id":"chatcmpl-function-legacy",
		"object":"chat.completion",
		"created":1700000000,
		"model":"gpt-4o",
		"choices":[{
			"index":0,
			"message":{
				"role":"assistant",
				"content":null,
				"function_call":{"name":"lookup_weather","arguments":"{\"city\":\"Shanghai\"}"}
			},
			"finish_reason":"function_call"
		}],
		"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}
	}`
	body, err := adaptChatResponseToResponses([]byte(chatResp), "public-model", nil)
	if err != nil {
		t.Fatalf("adaptChatResponseToResponses() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode responses body: %v", err)
	}
	output := payload["output"].([]any)
	item := output[0].(map[string]any)
	if item["type"] != "function_call" || item["name"] != "lookup_weather" {
		t.Fatalf("item = %#v, want legacy function_call converted", item)
	}
	if item["arguments"] != `{"city":"Shanghai"}` {
		t.Fatalf("arguments = %#v, want city JSON", item["arguments"])
	}
}

func TestAdaptChatResponseToResponsesRestoresLegacyFunctionCallAsCustom(t *testing.T) {
	patch := "*** Begin Patch\n*** Add File: probe.txt\n+ok\n*** End Patch\n"
	chatResp := `{
		"id":"chatcmpl-function-custom",
		"object":"chat.completion",
		"created":1700000000,
		"model":"gpt-5.5",
		"choices":[{
			"index":0,
			"message":{
				"role":"assistant",
				"content":null,
				"function_call":{"name":"apply_patch","arguments":` + strconv.Quote(`{"input":`+strconv.Quote(patch)+`}`) + `}
			},
			"finish_reason":"function_call"
		}],
		"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}
	}`
	body, err := adaptChatResponseToResponses([]byte(chatResp), "public-model", map[string]struct{}{"apply_patch": {}})
	if err != nil {
		t.Fatalf("adaptChatResponseToResponses() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode responses body: %v", err)
	}
	output := payload["output"].([]any)
	item := output[0].(map[string]any)
	if item["type"] != "custom_tool_call" || item["name"] != "apply_patch" {
		t.Fatalf("item = %#v, want legacy function_call restored as custom", item)
	}
	if item["input"] != patch {
		t.Fatalf("input = %#v, want patch text", item["input"])
	}
}

func TestHandleResponsesConvertsAndForwards(t *testing.T) {
	restore := SetSSRFCheckerForTesting(&mockSSRFChecker{})
	defer restore()
	routingSequence.Store(0)

	forwardBodyCh := make(chan []byte, 1)

	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/v1/chat/completions" {
				t.Errorf("upstream path = %q, want /v1/chat/completions", req.URL.Path)
			}
			body, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			forwardBodyCh <- body
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl-789","object":"chat.completion","model":"upstream-model","created":1700000000,"choices":[{"index":0,"message":{"role":"assistant","content":"Hello!"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`)),
			}, nil
		}),
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleResponses(r.Context(), testGatewaySnapshot(), NewRuntimeState(), nil, nil, w, r)
	}))
	defer server.Close()

	reqBody := `{"model":"public-model","input":"hello","stream":false}`
	resp, err := server.Client().Post(server.URL+"/v1/responses", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, body)
	}

	// Verify the upstream received Chat Completions format.
	fwdBody := <-forwardBodyCh
	var fwdPayload map[string]any
	if err := json.Unmarshal(fwdBody, &fwdPayload); err != nil {
		t.Fatalf("decode forwarded body: %v", err)
	}
	if fwdPayload["model"] != testUpstreamModel {
		t.Fatalf("forwarded model = %#v, want %s", fwdPayload["model"], testUpstreamModel)
	}
	messages, ok := fwdPayload["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("forwarded messages = %#v, want 1 message", fwdPayload["messages"])
	}

	// Verify the client received Responses API format.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["object"] != "response" {
		t.Fatalf("object = %#v, want response", payload["object"])
	}
	if payload["status"] != "completed" {
		t.Fatalf("status = %#v, want completed", payload["status"])
	}

	output, ok := payload["output"].([]any)
	if !ok || len(output) != 1 {
		t.Fatalf("output = %#v, want 1 element", payload["output"])
	}
	msg := output[0].(map[string]any)
	content := msg["content"].([]any)
	textBlock := content[0].(map[string]any)
	if textBlock["text"] != "Hello!" {
		t.Fatalf("output text = %#v, want Hello!", textBlock["text"])
	}

	usage, _ := payload["usage"].(map[string]any)
	if usage["input_tokens"] != float64(5) || usage["output_tokens"] != float64(3) {
		t.Fatalf("usage = %#v, want input=5 output=3", usage)
	}
}

func TestConvertResponsesRequestToChatCoercesNullAssistantContent(t *testing.T) {
	body := []byte(`{"model":"gpt-5","input":[{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"bash","arguments":"{}"}}]},{"role":"user","content":"hi"}],"stream":false}`)
	out, err := convertResponsesRequestToChat(body, "upstream")
	if err != nil {
		t.Fatalf("convertResponsesRequestToChat: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	msgs, _ := payload["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(msgs))
	}
	a0 := msgs[0].(map[string]any)
	if a0["content"] != "" {
		t.Fatalf("assistant content = %#v, want empty string", a0["content"])
	}
}

func TestConvertResponsesRequestToChatUnwrapsMessageItems(t *testing.T) {
	body := []byte(`{"model":"gpt-5","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]}`)
	out, err := convertResponsesRequestToChat(body, "")
	if err != nil {
		t.Fatalf("convertResponsesRequestToChat: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	msgs, _ := payload["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("len(messages) = %d", len(msgs))
	}
	m0 := msgs[0].(map[string]any)
	if _, ok := m0["type"]; ok {
		t.Fatalf("expected type field stripped from chat message, got %#v", m0)
	}
	if m0["content"] != "hello" {
		t.Fatalf("content = %#v, want hello", m0["content"])
	}
}

func TestConvertResponsesRequestToChatNormalizesOutputTextHistory(t *testing.T) {
	body := []byte(`{"model":"gpt-5","input":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello from prior response"}]},{"role":"user","content":"continue"}]}`)
	out, err := convertResponsesRequestToChat(body, "")
	if err != nil {
		t.Fatalf("convertResponsesRequestToChat: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	msgs, _ := payload["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(msgs))
	}
	a0 := msgs[0].(map[string]any)
	if a0["role"] != "assistant" || a0["content"] != "hello from prior response" {
		t.Fatalf("assistant message = %#v, want normalized output_text history", a0)
	}
}

func TestHandleResponsesReturnsResponsesFormatOnError(t *testing.T) {
	restore := SetSSRFCheckerForTesting(&mockSSRFChecker{})
	defer restore()
	routingSequence.Store(0)

	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"bad request"}}`)),
			}, nil
		}),
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleResponses(r.Context(), testGatewaySnapshot(), NewRuntimeState(), nil, nil, w, r)
	}))
	defer server.Close()

	reqBody := `{"model":"public-model","input":"hello"}`
	resp, err := server.Client().Post(server.URL+"/v1/responses", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandleResponsesStreamingEmitsCompletedAndDoneMarker(t *testing.T) {
	restore := SetSSRFCheckerForTesting(&mockSSRFChecker{})
	defer restore()
	routingSequence.Store(0)

	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			stream := strings.Join([]string{
				`data: {"id":"chatcmpl-stream-1","object":"chat.completion.chunk","created":1700000000,"model":"upstream-model","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":""}]}`,
				"",
				`data: {"id":"chatcmpl-stream-1","object":"chat.completion.chunk","created":1700000000,"model":"upstream-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":22,"completion_tokens":5,"total_tokens":27}}`,
				"",
				`data: [DONE]`,
				"",
			}, "\n")

			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream"},
				},
				Body: io.NopCloser(strings.NewReader(stream)),
			}, nil
		}),
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleResponses(r.Context(), testGatewaySnapshot(), NewRuntimeState(), nil, nil, w, r)
	}))
	defer server.Close()

	reqBody := `{"model":"public-model","input":"hello","stream":true}`
	resp, err := server.Client().Post(server.URL+"/v1/responses", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, body)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)

	var payloads []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			payloads = append(payloads, strings.TrimPrefix(line, "data: "))
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan stream: %v", err)
	}

	if len(payloads) < 4 {
		t.Fatalf("payload count = %d, want at least 4; payloads=%v", len(payloads), payloads)
	}

	if payloads[len(payloads)-1] != "[DONE]" {
		t.Fatalf("last payload = %q, want [DONE]", payloads[len(payloads)-1])
	}

	foundCompleted := false
	foundDoneLegacy := false
	for _, p := range payloads {
		if p == "[DONE]" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(p), &event); err != nil {
			t.Fatalf("decode event %q: %v", p, err)
		}
		switch event["type"] {
		case "response.completed":
			foundCompleted = true
			respPayload, _ := event["response"].(map[string]any)
			if respPayload["status"] != "completed" {
				t.Fatalf("completed response status = %#v, want completed", respPayload["status"])
			}
		case "response.done":
			foundDoneLegacy = true
		}
	}

	if !foundCompleted {
		t.Fatalf("expected response.completed event; payloads=%v", payloads)
	}
	if foundDoneLegacy {
		t.Fatalf("unexpected legacy response.done event; payloads=%v", payloads)
	}
}

func TestHandleResponsesStreamingEmitsOutputLifecycleBeforeTextDelta(t *testing.T) {
	restore := SetSSRFCheckerForTesting(&mockSSRFChecker{})
	defer restore()
	routingSequence.Store(0)

	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			stream := strings.Join([]string{
				`data: {"id":"chatcmpl-stream-2","object":"chat.completion.chunk","created":1700000000,"model":"upstream-model","choices":[{"index":0,"delta":{"content":"你"},"finish_reason":""}]}`,
				"",
				`data: {"id":"chatcmpl-stream-2","object":"chat.completion.chunk","created":1700000000,"model":"upstream-model","choices":[{"index":0,"delta":{"content":"好"},"finish_reason":""}]}`,
				"",
				`data: {"id":"chatcmpl-stream-2","object":"chat.completion.chunk","created":1700000000,"model":"upstream-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
				"",
				`data: [DONE]`,
				"",
			}, "\n")

			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream"},
				},
				Body: io.NopCloser(strings.NewReader(stream)),
			}, nil
		}),
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleResponses(r.Context(), testGatewaySnapshot(), NewRuntimeState(), nil, nil, w, r)
	}))
	defer server.Close()

	reqBody := `{"model":"public-model","input":"hello","stream":true}`
	resp, err := server.Client().Post(server.URL+"/v1/responses", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, body)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)
	var payloads []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			payloads = append(payloads, strings.TrimPrefix(line, "data: "))
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan stream: %v", err)
	}

	idxItemAdded := -1
	idxPartAdded := -1
	idxTextDelta := -1
	for i, p := range payloads {
		if p == "[DONE]" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(p), &event); err != nil {
			t.Fatalf("decode event %q: %v", p, err)
		}
		switch event["type"] {
		case "response.output_item.added":
			if idxItemAdded == -1 {
				idxItemAdded = i
			}
		case "response.content_part.added":
			if idxPartAdded == -1 {
				idxPartAdded = i
			}
		case "response.output_text.delta":
			if idxTextDelta == -1 {
				idxTextDelta = i
			}
		}
	}

	if idxItemAdded == -1 {
		t.Fatalf("missing response.output_item.added; payloads=%v", payloads)
	}
	if idxPartAdded == -1 {
		t.Fatalf("missing response.content_part.added; payloads=%v", payloads)
	}
	if idxTextDelta == -1 {
		t.Fatalf("missing response.output_text.delta; payloads=%v", payloads)
	}
	if !(idxItemAdded < idxTextDelta && idxPartAdded < idxTextDelta) {
		t.Fatalf("expected output item/content part before text delta, got item=%d part=%d delta=%d payloads=%v", idxItemAdded, idxPartAdded, idxTextDelta, payloads)
	}
}

func TestHandleResponsesStreamingConvertsCustomToolCall(t *testing.T) {
	restore := SetSSRFCheckerForTesting(&mockSSRFChecker{})
	defer restore()
	routingSequence.Store(0)

	patchA := "*** Begin Patch\n"
	patchB := "*** Add File: probe.txt\n+ok\n*** End Patch\n"

	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			stream := strings.Join([]string{
				`data: {"id":"chatcmpl-stream-custom","object":"chat.completion.chunk","created":1700000000,"model":"upstream-model","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_patch_1","type":"custom","custom":{"name":"apply_patch","input":` + strconv.Quote(patchA) + `}}]},"finish_reason":""}]}`,
				"",
				`data: {"id":"chatcmpl-stream-custom","object":"chat.completion.chunk","created":1700000000,"model":"upstream-model","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"type":"custom","custom":{"input":` + strconv.Quote(patchB) + `}}]},"finish_reason":""}]}`,
				"",
				`data: {"id":"chatcmpl-stream-custom","object":"chat.completion.chunk","created":1700000000,"model":"upstream-model","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":22,"completion_tokens":5,"total_tokens":27}}`,
				"",
				`data: [DONE]`,
				"",
			}, "\n")

			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream"},
				},
				Body: io.NopCloser(strings.NewReader(stream)),
			}, nil
		}),
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleResponses(r.Context(), testGatewaySnapshot(), NewRuntimeState(), nil, nil, w, r)
	}))
	defer server.Close()

	reqBody := `{"model":"public-model","input":"edit the file","stream":true,"tools":[{"type":"custom","name":"apply_patch"}]}`
	resp, err := server.Client().Post(server.URL+"/v1/responses", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, body)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)
	var payloads []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			payloads = append(payloads, strings.TrimPrefix(line, "data: "))
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan stream: %v", err)
	}

	foundAdded := false
	var deltaText string
	foundDone := false
	foundCompletedItem := false
	for _, p := range payloads {
		if p == "[DONE]" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(p), &event); err != nil {
			t.Fatalf("decode event %q: %v", p, err)
		}
		switch event["type"] {
		case "response.output_item.added":
			item, _ := event["item"].(map[string]any)
			if item["type"] == "custom_tool_call" && item["name"] == "apply_patch" {
				foundAdded = true
			}
		case "response.custom_tool_call_input.delta":
			deltaText += event["delta"].(string)
		case "response.custom_tool_call_input.done":
			if event["input"] == patchA+patchB {
				foundDone = true
			}
		case "response.output_item.done":
			item, _ := event["item"].(map[string]any)
			if item["type"] == "custom_tool_call" && item["input"] == patchA+patchB {
				foundCompletedItem = true
			}
		}
	}

	if !foundAdded {
		t.Fatalf("missing custom_tool_call added event; payloads=%v", payloads)
	}
	if deltaText != patchA+patchB {
		t.Fatalf("delta text = %#v, want full patch", deltaText)
	}
	if !foundDone {
		t.Fatalf("missing custom_tool_call_input.done event; payloads=%v", payloads)
	}
	if !foundCompletedItem {
		t.Fatalf("missing completed custom_tool_call item; payloads=%v", payloads)
	}
}

func TestHandleResponsesStreamingRestoresBridgedCustomToolCall(t *testing.T) {
	restore := SetSSRFCheckerForTesting(&mockSSRFChecker{})
	defer restore()
	routingSequence.Store(0)

	patch := "*** Begin Patch\n*** Add File: probe.txt\n+ok\n*** End Patch\n"
	args := `{"input":` + strconv.Quote(patch) + `}`
	argsA := args[:18]
	argsB := args[18:]

	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			var forwarded map[string]any
			if err := json.Unmarshal(body, &forwarded); err != nil {
				return nil, err
			}
			tools, _ := forwarded["tools"].([]any)
			if len(tools) != 1 {
				t.Fatalf("forwarded tools = %#v, want 1 bridged function tool", forwarded["tools"])
			}
			tool := tools[0].(map[string]any)
			if tool["type"] != "function" {
				t.Fatalf("forwarded tool = %#v, want function", tool)
			}

			stream := strings.Join([]string{
				`data: {"id":"chatcmpl-stream-custom-bridge","object":"chat.completion.chunk","created":1700000000,"model":"upstream-model","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_patch_1","type":"function","function":{"name":"apply_patch","arguments":` + strconv.Quote(argsA) + `}}]},"finish_reason":""}]}`,
				"",
				`data: {"id":"chatcmpl-stream-custom-bridge","object":"chat.completion.chunk","created":1700000000,"model":"upstream-model","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"type":"function","function":{"arguments":` + strconv.Quote(argsB) + `}}]},"finish_reason":""}]}`,
				"",
				`data: {"id":"chatcmpl-stream-custom-bridge","object":"chat.completion.chunk","created":1700000000,"model":"upstream-model","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":22,"completion_tokens":5,"total_tokens":27}}`,
				"",
				`data: [DONE]`,
				"",
			}, "\n")

			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream"},
				},
				Body: io.NopCloser(strings.NewReader(stream)),
			}, nil
		}),
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleResponses(r.Context(), testGatewaySnapshot(), NewRuntimeState(), nil, nil, w, r)
	}))
	defer server.Close()

	reqBody := `{"model":"public-model","input":"edit the file","stream":true,"tools":[{"type":"custom","name":"apply_patch"}]}`
	resp, err := server.Client().Post(server.URL+"/v1/responses", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, body)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)
	var payloads []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			payloads = append(payloads, strings.TrimPrefix(line, "data: "))
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan stream: %v", err)
	}

	foundPatchDelta := false
	foundDone := false
	for _, p := range payloads {
		if p == "[DONE]" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(p), &event); err != nil {
			t.Fatalf("decode event %q: %v", p, err)
		}
		switch event["type"] {
		case "response.custom_tool_call_input.delta":
			if event["delta"] == patch {
				foundPatchDelta = true
			}
		case "response.custom_tool_call_input.done":
			if event["input"] == patch {
				foundDone = true
			}
		case "response.function_call_arguments.delta":
			t.Fatalf("unexpected function argument delta for bridged custom tool: %v", event)
		}
	}
	if !foundPatchDelta {
		t.Fatalf("missing restored patch delta; payloads=%v", payloads)
	}
	if !foundDone {
		t.Fatalf("missing restored patch done event; payloads=%v", payloads)
	}
}

func TestHandleResponsesStreamingConvertsLegacyFunctionCallDelta(t *testing.T) {
	restore := SetSSRFCheckerForTesting(&mockSSRFChecker{})
	defer restore()
	routingSequence.Store(0)

	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			stream := strings.Join([]string{
				`data: {"id":"chatcmpl-stream-function-legacy","object":"chat.completion.chunk","created":1700000000,"model":"upstream-model","choices":[{"index":0,"delta":{"function_call":{"name":"lookup_weather","arguments":"{\"city\""}},"finish_reason":""}]}`,
				"",
				`data: {"id":"chatcmpl-stream-function-legacy","object":"chat.completion.chunk","created":1700000000,"model":"upstream-model","choices":[{"index":0,"delta":{"function_call":{"arguments":":\"Shanghai\"}"}},"finish_reason":""}]}`,
				"",
				`data: {"id":"chatcmpl-stream-function-legacy","object":"chat.completion.chunk","created":1700000000,"model":"upstream-model","choices":[{"index":0,"delta":{},"finish_reason":"function_call"}],"usage":{"prompt_tokens":22,"completion_tokens":5,"total_tokens":27}}`,
				"",
				`data: [DONE]`,
				"",
			}, "\n")

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(stream)),
			}, nil
		}),
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleResponses(r.Context(), testGatewaySnapshot(), NewRuntimeState(), nil, nil, w, r)
	}))
	defer server.Close()

	reqBody := `{"model":"public-model","input":"look up weather","stream":true,"tools":[{"type":"function","name":"lookup_weather","parameters":{"type":"object"}}]}`
	resp, err := server.Client().Post(server.URL+"/v1/responses", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, body)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)
	var payloads []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			payloads = append(payloads, strings.TrimPrefix(line, "data: "))
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan stream: %v", err)
	}

	foundDone := false
	for _, p := range payloads {
		if p == "[DONE]" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(p), &event); err != nil {
			t.Fatalf("decode event %q: %v", p, err)
		}
		if event["type"] == "response.function_call_arguments.done" && event["arguments"] == `{"city":"Shanghai"}` {
			foundDone = true
		}
	}
	if !foundDone {
		t.Fatalf("missing legacy function_call done event; payloads=%v", payloads)
	}
}

func allowLocalAnthropicTestUpstreams(t *testing.T) {
	t.Helper()
	restore := SetSSRFCheckerForTesting(&mockSSRFChecker{})
	t.Cleanup(restore)
	swapSharedHTTPClient(t, &http.Client{})
}

type mockSSRFChecker struct{}

func (m *mockSSRFChecker) ValidateURL(_ string) error { return nil }

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
