package app

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"ai-model-gateway/internal/core"
)

func TestCompatAdapter_AdaptRequest_RewritesModel(t *testing.T) {
	adapter := NewCompatAdapter(core.CompatConfig{
		Bridge: core.BridgeConfig{Enabled: true},
	})

	req := &core.GatewayRequest{
		OriginalModel: "gpt-4",
		Model:         "gpt-4o",
		Body:          []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`),
	}

	if err := adapter.AdaptRequest(context.Background(), req); err != nil {
		t.Fatalf("AdaptRequest error: %v", err)
	}

	var obj map[string]interface{}
	json.Unmarshal(req.Body, &obj)
	if obj["model"] != "gpt-4o" {
		t.Errorf("expected model gpt-4o in body, got %v", obj["model"])
	}
	// Messages should be preserved.
	if _, ok := obj["messages"]; !ok {
		t.Error("messages field missing after rewrite")
	}
}

func TestCompatAdapter_AdaptRequest_NoRewriteWhenSame(t *testing.T) {
	adapter := NewCompatAdapter(core.CompatConfig{})

	original := `{"model":"gpt-4o","messages":[]}`
	req := &core.GatewayRequest{
		OriginalModel: "gpt-4o",
		Model:         "gpt-4o",
		Body:          []byte(original),
	}

	if err := adapter.AdaptRequest(context.Background(), req); err != nil {
		t.Fatalf("AdaptRequest error: %v", err)
	}
	// Body should be unchanged.
	if string(req.Body) != original {
		t.Errorf("body changed when models are same: %s", req.Body)
	}
}

func TestCompatAdapter_AdaptResponse_RewritesModelBack(t *testing.T) {
	adapter := NewCompatAdapter(core.CompatConfig{
		Bridge: core.BridgeConfig{Enabled: true},
	})

	req := &core.GatewayRequest{
		OriginalModel: "gpt-4",
		Model:         "gpt-4o",
	}
	resp := &core.GatewayResponse{
		StatusCode: 200,
		Body:       []byte(`{"id":"resp-1","model":"gpt-4o","choices":[]}`),
	}

	if err := adapter.AdaptResponse(context.Background(), req, resp); err != nil {
		t.Fatalf("AdaptResponse error: %v", err)
	}

	var obj map[string]interface{}
	json.Unmarshal(resp.Body, &obj)
	if obj["model"] != "gpt-4" {
		t.Errorf("expected model gpt-4 in response, got %v", obj["model"])
	}
}

func TestAnthropicToChatRequest(t *testing.T) {
	anthropicBody := `{
		"model": "claude-3-opus",
		"system": "You are helpful.",
		"max_tokens": 100,
		"messages": [
			{"role": "user", "content": "Hello"}
		]
	}`

	chatBody, stream, err := AnthropicToChatRequest([]byte(anthropicBody), "")
	if err != nil {
		t.Fatalf("AnthropicToChatRequest error: %v", err)
	}
	if stream {
		t.Error("expected non-streaming")
	}

	var chat map[string]interface{}
	json.Unmarshal(chatBody, &chat)

	if chat["model"] != "claude-3-opus" {
		t.Errorf("expected model claude-3-opus, got %v", chat["model"])
	}
	messages, ok := chat["messages"].([]interface{})
	if !ok {
		t.Fatal("messages not an array")
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages (system+user), got %d", len(messages))
	}
	first := messages[0].(map[string]interface{})
	if first["role"] != "system" {
		t.Errorf("expected first message role=system, got %v", first["role"])
	}
	if first["content"] != "You are helpful." {
		t.Errorf("expected system content, got %v", first["content"])
	}
}

func TestAnthropicToChatRequest_WithTools(t *testing.T) {
	body := `{
		"model": "claude-3",
		"max_tokens": 64,
		"tools": [
			{"name":"bash","description":"Run shell","input_schema":{"type":"object","properties":{"cmd":{"type":"string"}}}}
		],
		"messages": [{"role":"user","content":"run pwd"}]
	}`

	chatBody, _, err := AnthropicToChatRequest([]byte(body), "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	var chat map[string]interface{}
	json.Unmarshal(chatBody, &chat)

	tools, ok := chat["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %v", chat["tools"])
	}
	tool := tools[0].(map[string]interface{})
	if tool["type"] != "function" {
		t.Errorf("expected tool type=function, got %v", tool["type"])
	}
	fn := tool["function"].(map[string]interface{})
	if fn["name"] != "bash" {
		t.Errorf("expected tool name=bash, got %v", fn["name"])
	}
}

func TestAnthropicToChatRequest_PreservesToolsAndToolResults(t *testing.T) {
	body := `{
		"model":"gpt-5.4",
		"system":"You are an agent.",
		"stream":true,
		"tools":[{"name":"bash","description":"Run shell","input_schema":{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"]}}],
		"tool_choice":{"type":"tool","name":"bash"},
		"messages":[
			{"role":"assistant","content":[{"type":"text","text":"checking"},{"type":"tool_use","id":"toolu_1","name":"bash","input":{"cmd":"pwd"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"C:/repo"},{"type":"text","text":"continue"}]}
		]
	}`

	chatBody, stream, err := AnthropicToChatRequest([]byte(body), "")
	if err != nil {
		t.Fatalf("AnthropicToChatRequest error: %v", err)
	}
	if !stream {
		t.Fatalf("expected stream=true")
	}

	var chat map[string]interface{}
	if err := json.Unmarshal(chatBody, &chat); err != nil {
		t.Fatalf("decode chat body: %v", err)
	}

	tools, ok := chat["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("expected one converted tool, got %#v", chat["tools"])
	}
	toolChoice, ok := chat["tool_choice"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected specific tool choice, got %#v", chat["tool_choice"])
	}
	choiceFunction, ok := toolChoice["function"].(map[string]interface{})
	if !ok || choiceFunction["name"] != "bash" {
		t.Fatalf("expected tool choice bash, got %#v", toolChoice)
	}

	messages, ok := chat["messages"].([]interface{})
	if !ok || len(messages) != 4 {
		t.Fatalf("expected system + assistant + tool + user messages, got %#v", chat["messages"])
	}
	assistant := messages[1].(map[string]interface{})
	if assistant["role"] != "assistant" || assistant["content"] != "checking" {
		t.Fatalf("expected assistant text preserved, got %#v", assistant)
	}
	toolCalls, ok := assistant["tool_calls"].([]interface{})
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("expected tool call preserved, got %#v", assistant["tool_calls"])
	}
	toolMessage := messages[2].(map[string]interface{})
	if toolMessage["role"] != "tool" || toolMessage["tool_call_id"] != "toolu_1" {
		t.Fatalf("expected tool result message, got %#v", toolMessage)
	}
	userMessage := messages[3].(map[string]interface{})
	if userMessage["role"] != "user" || userMessage["content"] != "continue" {
		t.Fatalf("expected trailing user text message, got %#v", userMessage)
	}
}

func TestChatToAnthropicResponse(t *testing.T) {
	chatResp := `{
		"id": "chatcmpl-123",
		"model": "gpt-4o",
		"choices": [{
			"index": 0,
			"message": {"role": "assistant", "content": "Hello!"},
			"finish_reason": "stop"
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	}`

	out, err := ChatToAnthropicResponse([]byte(chatResp), "claude-3-opus")
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	var resp map[string]interface{}
	json.Unmarshal(out, &resp)

	if resp["type"] != "message" {
		t.Errorf("expected type=message, got %v", resp["type"])
	}
	if resp["model"] != "claude-3-opus" {
		t.Errorf("expected model=claude-3-opus, got %v", resp["model"])
	}
	if resp["stop_reason"] != "end_turn" {
		t.Errorf("expected stop_reason=end_turn, got %v", resp["stop_reason"])
	}
	content, ok := resp["content"].([]interface{})
	if !ok || len(content) != 1 {
		t.Fatalf("expected 1 content block, got %v", resp["content"])
	}
	block := content[0].(map[string]interface{})
	if block["type"] != "text" || block["text"] != "Hello!" {
		t.Errorf("unexpected content block: %v", block)
	}
	usage := resp["usage"].(map[string]interface{})
	if v, _ := toInt(usage["input_tokens"]); v != 10 {
		t.Errorf("expected input_tokens=10, got %v", usage["input_tokens"])
	}
}

func TestChatToAnthropicResponse_ToolCalls(t *testing.T) {
	chatResp := `{
		"id": "chatcmpl-tool",
		"model": "gpt-4o",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "",
				"tool_calls": [
					{"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"cmd\":\"pwd\"}"}}
				]
			},
			"finish_reason": "tool_calls"
		}],
		"usage": {"prompt_tokens": 5, "completion_tokens": 3}
	}`

	out, err := ChatToAnthropicResponse([]byte(chatResp), "claude-3")
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	var resp map[string]interface{}
	json.Unmarshal(out, &resp)

	if resp["stop_reason"] != "tool_use" {
		t.Errorf("expected stop_reason=tool_use, got %v", resp["stop_reason"])
	}
	content, ok := resp["content"].([]interface{})
	if !ok || len(content) != 1 {
		t.Fatalf("expected 1 content block (tool_use), got %d", len(content))
	}
	block := content[0].(map[string]interface{})
	if block["type"] != "tool_use" {
		t.Errorf("expected type=tool_use, got %v", block["type"])
	}
	if block["name"] != "bash" {
		t.Errorf("expected name=bash, got %v", block["name"])
	}
}

func TestChatToAnthropicRequest_PreservesToolCallsAndToolResults(t *testing.T) {
	body := `{
		"model":"gpt-5.4",
		"stream":true,
		"messages":[
			{"role":"assistant","content":"checking","tool_calls":[{"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"cmd\":\"pwd\"}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"C:/repo"},
			{"role":"user","content":"continue"}
		]
	}`

	anthropicBody, stream, err := ChatToAnthropicRequest([]byte(body), "claude-opus-4-6")
	if err != nil {
		t.Fatalf("ChatToAnthropicRequest error: %v", err)
	}
	if !stream {
		t.Fatalf("expected stream=true")
	}

	var anthropic map[string]interface{}
	if err := json.Unmarshal(anthropicBody, &anthropic); err != nil {
		t.Fatalf("decode anthropic body: %v", err)
	}

	if anthropic["model"] != "claude-opus-4-6" {
		t.Fatalf("expected overridden model, got %#v", anthropic["model"])
	}
	rawMessages, ok := anthropic["messages"].([]interface{})
	if !ok || len(rawMessages) != 3 {
		t.Fatalf("expected assistant + tool_result + user messages, got %#v", anthropic["messages"])
	}

	assistant := rawMessages[0].(map[string]interface{})
	if assistant["role"] != "assistant" {
		t.Fatalf("expected assistant role, got %#v", assistant["role"])
	}
	assistantContent, ok := assistant["content"].([]interface{})
	if !ok || len(assistantContent) != 2 {
		t.Fatalf("expected assistant text + tool_use blocks, got %#v", assistant["content"])
	}
	toolUse := assistantContent[1].(map[string]interface{})
	if toolUse["type"] != "tool_use" || toolUse["name"] != "bash" {
		t.Fatalf("expected tool_use block, got %#v", toolUse)
	}
	input, ok := toolUse["input"].(map[string]interface{})
	if !ok || input["cmd"] != "pwd" {
		t.Fatalf("expected parsed tool input, got %#v", toolUse["input"])
	}

	toolResultMsg := rawMessages[1].(map[string]interface{})
	if toolResultMsg["role"] != "user" {
		t.Fatalf("expected tool result to become user message, got %#v", toolResultMsg["role"])
	}
	toolResultContent, ok := toolResultMsg["content"].([]interface{})
	if !ok || len(toolResultContent) != 1 {
		t.Fatalf("expected one tool_result block, got %#v", toolResultMsg["content"])
	}
	toolResult := toolResultContent[0].(map[string]interface{})
	if toolResult["type"] != "tool_result" || toolResult["tool_use_id"] != "call_1" || toolResult["content"] != "C:/repo" {
		t.Fatalf("expected tool_result block, got %#v", toolResult)
	}

	user := rawMessages[2].(map[string]interface{})
	if user["role"] != "user" || user["content"] != "continue" {
		t.Fatalf("expected trailing user message preserved, got %#v", user)
	}
}

func TestResponsesToChatRequest(t *testing.T) {
	responsesBody := `{
		"model": "gpt-4o",
		"instructions": "Be concise.",
		"input": "What is 2+2?"
	}`

	chatBody, stream, err := ResponsesToChatRequest([]byte(responsesBody), "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if stream {
		t.Error("expected non-streaming")
	}

	var chat map[string]interface{}
	json.Unmarshal(chatBody, &chat)

	if chat["model"] != "gpt-4o" {
		t.Errorf("expected model=gpt-4o, got %v", chat["model"])
	}
	messages, ok := chat["messages"].([]interface{})
	if !ok {
		t.Fatal("messages not array")
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages (system+user), got %d", len(messages))
	}
	if messages[0].(map[string]interface{})["role"] != "system" {
		t.Error("expected first message to be system")
	}
	if messages[1].(map[string]interface{})["content"] != "What is 2+2?" {
		t.Error("expected user message content")
	}
}

func TestResponsesToChatRequest_ArrayInput(t *testing.T) {
	body := `{
		"model": "gpt-4o",
		"input": [
			{"role": "user", "content": "Hello"},
			{"role": "assistant", "content": "Hi!"},
			{"role": "user", "content": "How are you?"}
		]
	}`

	chatBody, _, err := ResponsesToChatRequest([]byte(body), "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	var chat map[string]interface{}
	json.Unmarshal(chatBody, &chat)

	messages := chat["messages"].([]interface{})
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}
}

func TestAnthropicToChatRequest_PreservesImageBlocks(t *testing.T) {
	body := `{
		"model": "gpt-5.4",
		"messages": [
			{
				"role": "user",
				"content": [
					{"type":"image","source":{"type":"url","url":"https://example.com/cat.png"}},
					{"type":"text","text":"describe"}
				]
			}
		]
	}`

	chatBody, _, err := AnthropicToChatRequest([]byte(body), "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	var chat map[string]interface{}
	if err := json.Unmarshal(chatBody, &chat); err != nil {
		t.Fatalf("decode chat body: %v", err)
	}

	messages, ok := chat["messages"].([]interface{})
	if !ok || len(messages) != 1 {
		t.Fatalf("expected one message, got %#v", chat["messages"])
	}
	message, _ := messages[0].(map[string]interface{})
	content, ok := message["content"].([]interface{})
	if !ok || len(content) != 2 {
		t.Fatalf("expected image + text content, got %#v", message["content"])
	}
	image, _ := content[0].(map[string]interface{})
	imageURL, _ := image["image_url"].(map[string]interface{})
	if imageURL["url"] != "https://example.com/cat.png" {
		t.Fatalf("expected image URL preserved, got %#v", image["image_url"])
	}
}

func TestResponsesToChatRequest_PreservesStructuredImageBlocks(t *testing.T) {
	body := `{
		"model":"claude-sonnet-4-6",
		"stream":true,
		"input":[
			{
				"role":"user",
				"content":[
					{"type":"input_text","text":"describe"},
					{"type":"input_image","image_url":"https://example.com/cat.png"}
				]
			}
		]
	}`

	chatBody, stream, err := ResponsesToChatRequest([]byte(body), "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !stream {
		t.Fatal("expected stream=true")
	}

	var chat map[string]interface{}
	if err := json.Unmarshal(chatBody, &chat); err != nil {
		t.Fatalf("decode chat body: %v", err)
	}

	messages, ok := chat["messages"].([]interface{})
	if !ok || len(messages) != 1 {
		t.Fatalf("expected one message, got %#v", chat["messages"])
	}
	message, _ := messages[0].(map[string]interface{})
	content, ok := message["content"].([]interface{})
	if !ok || len(content) != 2 {
		t.Fatalf("expected text + image content, got %#v", message["content"])
	}
	image, _ := content[1].(map[string]interface{})
	imageURL, _ := image["image_url"].(map[string]interface{})
	if imageURL["url"] != "https://example.com/cat.png" {
		t.Fatalf("expected structured image preserved, got %#v", image["image_url"])
	}
}

func TestChatToResponsesResponse(t *testing.T) {
	chatResp := `{
		"id": "chatcmpl-123",
		"model": "gpt-4o",
		"choices": [{
			"index": 0,
			"message": {"role": "assistant", "content": "4"},
			"finish_reason": "stop"
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 1, "total_tokens": 11}
	}`

	out, err := ChatToResponsesResponse([]byte(chatResp), "gpt-4o")
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	var resp map[string]interface{}
	json.Unmarshal(out, &resp)

	if resp["object"] != "response" {
		t.Errorf("expected object=response, got %v", resp["object"])
	}
	if resp["model"] != "gpt-4o" {
		t.Errorf("expected model=gpt-4o, got %v", resp["model"])
	}
	if resp["output_text"] != "4" {
		t.Errorf("expected output_text=4, got %v", resp["output_text"])
	}
	output, ok := resp["output"].([]interface{})
	if !ok || len(output) != 1 {
		t.Fatalf("expected 1 output item, got %v", resp["output"])
	}
}

func TestCompatAdapter_AdaptRequest_CountTokensProbeRewrite(t *testing.T) {
	adapter := NewCompatAdapter(core.CompatConfig{})
	req := &core.GatewayRequest{
		Path:  "/v1/messages/count_tokens",
		Model: "claude-opus-4-6-thinking",
		Body:  []byte(`{"model":"claude-opus-4-6-thinking","system":"Count carefully.","stream":true,"max_tokens":256,"messages":[{"role":"user","content":"ping"}]}`),
	}

	if err := adapter.AdaptRequest(context.Background(), req); err != nil {
		t.Fatalf("AdaptRequest error: %v", err)
	}
	if req.UpstreamPath != "/v1/messages" {
		t.Fatalf("expected upstream path /v1/messages, got %q", req.UpstreamPath)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(req.Body, &payload); err != nil {
		t.Fatalf("decode rewritten body: %v", err)
	}
	if payload["model"] != "claude-sonnet-4-6" {
		t.Fatalf("expected rewritten model claude-sonnet-4-6, got %#v", payload["model"])
	}
	if payload["max_tokens"] != float64(1) {
		t.Fatalf("expected max_tokens 1, got %#v", payload["max_tokens"])
	}
	if payload["stream"] != false {
		t.Fatalf("expected stream=false, got %#v", payload["stream"])
	}
	if payload["system"] != "Count carefully." {
		t.Fatalf("expected system prompt preserved, got %#v", payload["system"])
	}
}

func TestCompatAdapter_AdaptResponse_CountTokensSuccess(t *testing.T) {
	adapter := NewCompatAdapter(core.CompatConfig{})
	req := &core.GatewayRequest{Path: "/v1/messages/count_tokens"}
	resp := &core.GatewayResponse{
		StatusCode: 200,
		Headers:    http.Header{},
		Body:       []byte(`{"id":"msg_count","usage":{"input_tokens":21,"output_tokens":1}}`),
	}

	if err := adapter.AdaptResponse(context.Background(), req, resp); err != nil {
		t.Fatalf("AdaptResponse error: %v", err)
	}
	if got := resp.Headers.Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected application/json, got %q", got)
	}
	if string(resp.Body) != `{"input_tokens":21}` {
		t.Fatalf("expected compact token response, got %q", string(resp.Body))
	}
}

func TestCompatAdapter_AdaptResponse_CountTokensMissingUsage(t *testing.T) {
	adapter := NewCompatAdapter(core.CompatConfig{})
	req := &core.GatewayRequest{Path: "/v1/messages/count_tokens"}
	resp := &core.GatewayResponse{
		StatusCode: 200,
		Body:       []byte(`{"id":"msg_bad","usage":{"output_tokens":1}}`),
	}

	if err := adapter.AdaptResponse(context.Background(), req, resp); err == nil {
		t.Fatal("expected error when input_tokens is missing")
	}
}

func TestRewriteJSONModelField(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		newModel string
		want     string
	}{
		{
			name:     "basic rewrite",
			body:     `{"model":"old","data":1}`,
			newModel: "new",
			want:     "new",
		},
		{
			name:     "no model field",
			body:     `{"data":1}`,
			newModel: "new",
			want:     "", // no model field, body unchanged
		},
		{
			name:     "invalid json",
			body:     `not json`,
			newModel: "new",
			want:     "", // passthrough
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rewriteJSONModelField([]byte(tt.body), tt.newModel)
			if tt.want == "" {
				// Should be unchanged or passthrough.
				return
			}
			var obj map[string]interface{}
			json.Unmarshal(result, &obj)
			if obj["model"] != tt.want {
				t.Errorf("expected model=%s, got %v", tt.want, obj["model"])
			}
		})
	}
}
