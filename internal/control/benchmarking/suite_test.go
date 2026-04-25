package benchmarking

import (
	"encoding/json"
	"testing"
)

func TestBuildRequestAnthropicProtocol(t *testing.T) {
	body, err := buildRequest(ProtocolAnthropicMessage, "claude-test", true, "system prompt", "user prompt", nil)
	if err != nil {
		t.Fatalf("buildRequest() error = %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload["model"] != "claude-test" {
		t.Fatalf("payload model = %#v, want claude-test", payload["model"])
	}
	if payload["system"] != "system prompt" {
		t.Fatalf("payload system = %#v, want system prompt", payload["system"])
	}
	if payload["stream"] != true {
		t.Fatalf("payload stream = %#v, want true", payload["stream"])
	}
	messages, ok := payload["messages"].([]interface{})
	if !ok || len(messages) != 1 {
		t.Fatalf("payload messages = %#v, want single user message", payload["messages"])
	}
}

func TestBuildToolRequestAnthropicProtocol(t *testing.T) {
	body, err := buildToolRequest(ProtocolAnthropicMessage, "claude-test")
	if err != nil {
		t.Fatalf("buildToolRequest() error = %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	toolChoice, ok := payload["tool_choice"].(map[string]interface{})
	if !ok {
		t.Fatalf("payload tool_choice = %#v, want object", payload["tool_choice"])
	}
	if toolChoice["name"] != "lookup_weather" {
		t.Fatalf("tool_choice.name = %#v, want lookup_weather", toolChoice["name"])
	}
	tools, ok := payload["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("payload tools = %#v, want one tool", payload["tools"])
	}
	tool, ok := tools[0].(map[string]interface{})
	if !ok {
		t.Fatalf("first tool = %#v, want object", tools[0])
	}
	if tool["name"] != "lookup_weather" {
		t.Fatalf("tool.name = %#v, want lookup_weather", tool["name"])
	}
	if _, ok := tool["input_schema"].(map[string]interface{}); !ok {
		t.Fatalf("tool.input_schema = %#v, want object", tool["input_schema"])
	}
}
