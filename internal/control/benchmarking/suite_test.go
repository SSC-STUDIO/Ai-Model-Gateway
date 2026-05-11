package benchmarking

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"ai-model-gateway/internal/contracts/gatewaycontrol"
)

func TestBuildRequestOpenAIProtocol(t *testing.T) {
	body, err := buildRequest("", "gpt-4", false, "sys", "usr", map[string]interface{}{"extra": true})
	if err != nil {
		t.Fatalf("buildRequest() error = %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload["model"] != "gpt-4" {
		t.Fatalf("payload model = %#v, want gpt-4", payload["model"])
	}
	if payload["extra"] != true {
		t.Fatalf("payload extra = %#v, want true", payload["extra"])
	}
	messages, ok := payload["messages"].([]interface{})
	if !ok || len(messages) != 2 {
		t.Fatalf("payload messages = %#v, want 2 messages", payload["messages"])
	}
}

func TestBuildRequestUnsupportedProtocol(t *testing.T) {
	_, err := buildRequest("invalid_protocol", "model", false, "sys", "usr", nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported benchmark protocol") {
		t.Fatalf("buildRequest() error = %v, want unsupported protocol error", err)
	}
}

func TestBuildToolRequestOpenAIProtocol(t *testing.T) {
	body, err := buildToolRequest("", "gpt-4")
	if err != nil {
		t.Fatalf("buildToolRequest() error = %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if _, ok := payload["tool_choice"].(map[string]interface{}); !ok {
		t.Fatalf("tool_choice not an object: %T", payload["tool_choice"])
	}
}

func TestBuildToolRequestUnsupportedProtocol(t *testing.T) {
	_, err := buildToolRequest("invalid_protocol", "model")
	if err == nil || !strings.Contains(err.Error(), "unsupported benchmark protocol") {
		t.Fatalf("buildToolRequest() error = %v, want unsupported protocol error", err)
	}
}

func TestExtractAssistantTextOpenAI(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"hello world"}}]}`)
	got := extractAssistantText("", body)
	if got != "hello world" {
		t.Fatalf("extractAssistantText() = %q, want hello world", got)
	}
}

func TestExtractAssistantTextAnthropic(t *testing.T) {
	body := []byte(`{"content":[{"type":"text","text":"answer one"},{"type":"text","text":"answer two"}]}`)
	got := extractAssistantText(ProtocolAnthropicMessage, body)
	if got != "answer one\nanswer two" {
		t.Fatalf("extractAssistantText() = %q, want answer one\\nanswer two", got)
	}
}

func TestExtractAssistantTextAnthropicSkipsNonText(t *testing.T) {
	body := []byte(`{"content":[{"type":"tool_use","name":"foo","input":{}}]}`)
	got := extractAssistantText(ProtocolAnthropicMessage, body)
	// Falls through to raw body when no text parts found
	if !strings.Contains(got, "tool_use") {
		t.Fatalf("extractAssistantText() = %q, want raw body fallback", got)
	}
}

func TestExtractAssistantTextOpenAIEmptyChoices(t *testing.T) {
	body := []byte(`{"choices":[]}`)
	got := extractAssistantText("", body)
	if !strings.Contains(got, "choices") {
		t.Fatalf("extractAssistantText() = %q, want raw body fallback", got)
	}
}

func TestExtractAssistantTextInvalidJSON(t *testing.T) {
	body := []byte(`not json`)
	got := extractAssistantText("", body)
	if got != "not json" {
		t.Fatalf("extractAssistantText() = %q, want raw body", got)
	}
}

func TestExtractToolCallOpenAIMatch(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"tool_calls":[{"function":{"name":"lookup_weather","arguments":"{\"city\":\"Shanghai\"}"}}]}}]}`)
	ok, excerpt := extractToolCall("", body, "lookup_weather", "Shanghai")
	if !ok {
		t.Fatal("extractToolCall() ok = false, want true")
	}
	if !strings.Contains(excerpt, "lookup_weather") {
		t.Fatalf("extractToolCall() excerpt = %q, want to contain lookup_weather", excerpt)
	}
}

func TestExtractToolCallOpenAIMismatch(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"tool_calls":[{"function":{"name":"other_tool","arguments":"{}"} }]}}]}`)
	ok, _ := extractToolCall("", body, "lookup_weather", "Shanghai")
	if ok {
		t.Fatal("extractToolCall() ok = true, want false for mismatched tool name")
	}
}

func TestExtractToolCallOpenAINoToolCalls(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"no tools"}}]}`)
	ok, _ := extractToolCall("", body, "lookup_weather", "Shanghai")
	if ok {
		t.Fatal("extractToolCall() ok = true, want false for no tool_calls")
	}
}

func TestExtractToolCallAnthropicMatch(t *testing.T) {
	body := []byte(`{"content":[{"type":"tool_use","name":"lookup_weather","input":{"city":"Shanghai"}}]}`)
	ok, excerpt := extractToolCall(ProtocolAnthropicMessage, body, "lookup_weather", "Shanghai")
	if !ok {
		t.Fatal("extractToolCall() ok = false, want true")
	}
	if !strings.Contains(excerpt, "lookup_weather") {
		t.Fatalf("extractToolCall() excerpt = %q", excerpt)
	}
}

func TestExtractToolCallAnthropicMismatch(t *testing.T) {
	body := []byte(`{"content":[{"type":"tool_use","name":"other","input":{"city":"Beijing"}}]}`)
	ok, _ := extractToolCall(ProtocolAnthropicMessage, body, "lookup_weather", "Shanghai")
	if ok {
		t.Fatal("extractToolCall() ok = true, want false for mismatched tool name")
	}
}

func TestExtractToolCallAnthropicInvalidJSON(t *testing.T) {
	body := []byte(`not json`)
	ok, excerpt := extractToolCall(ProtocolAnthropicMessage, body, "lookup_weather", "Shanghai")
	if ok {
		t.Fatal("extractToolCall() ok = true, want false for invalid JSON")
	}
	if excerpt != "not json" {
		t.Fatalf("extractToolCall() excerpt = %q, want raw body", excerpt)
	}
}

func TestExtractToolCallOpenAIInvalidJSON(t *testing.T) {
	body := []byte(`not json`)
	ok, excerpt := extractToolCall("", body, "lookup_weather", "Shanghai")
	if ok {
		t.Fatal("extractToolCall() ok = true, want false for invalid JSON")
	}
	if excerpt != "not json" {
		t.Fatalf("extractToolCall() excerpt = %q, want raw body", excerpt)
	}
}

func TestClampScore(t *testing.T) {
	tests := []struct {
		input float64
		want  float64
	}{
		{-10, 0},
		{0, 0},
		{50, 50},
		{100, 100},
		{110, 100},
	}
	for _, tc := range tests {
		got := clampScore(tc.input)
		if got != tc.want {
			t.Fatalf("clampScore(%f) = %f, want %f", tc.input, got, tc.want)
		}
	}
}

func TestExcerpt(t *testing.T) {
	short := "hello"
	if excerpt(short) != "hello" {
		t.Fatalf("excerpt(short) = %q, want hello", excerpt(short))
	}
	long := strings.Repeat("a", 400)
	got := excerpt(long)
	if len(got) != 320 {
		t.Fatalf("excerpt(long) length = %d, want 320", len(got))
	}
	spaced := "  hello  "
	if excerpt(spaced) != "hello" {
		t.Fatalf("excerpt(spaced) = %q, want hello", excerpt(spaced))
	}
}

func TestSuiteByName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"", false},
		{"general_protocol_v1", false},
		{"unknown_suite", true},
	}
	for _, tc := range tests {
		suite, err := suiteByName(tc.name)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("suiteByName(%q) = nil error, want error", tc.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("suiteByName(%q) error = %v", tc.name, err)
		}
		if suite == nil {
			t.Fatalf("suiteByName(%q) = nil", tc.name)
		}
	}
}

func TestExactTextScorerMatch(t *testing.T) {
	scorer := exactTextScorer("hello")
	resp := &gatewaycontrol.RunBenchmarkCaseResponse{
		StatusCode:   200,
		ResponseBody: []byte(`{"choices":[{"message":{"content":"hello"}}]}`),
	}
	result := scorer(context.Background(), "", resp, nil)
	if !result.Success || result.Score != 100 {
		t.Fatalf("exactTextScorer match: success=%v score=%f, want true 100", result.Success, result.Score)
	}
	if result.Reason != "exact_match" {
		t.Fatalf("exactTextScorer match: reason = %q, want exact_match", result.Reason)
	}
}

func TestExactTextScorerMismatch(t *testing.T) {
	scorer := exactTextScorer("hello")
	resp := &gatewaycontrol.RunBenchmarkCaseResponse{
		StatusCode:   200,
		ResponseBody: []byte(`{"choices":[{"message":{"content":"world"}}]}`),
	}
	result := scorer(context.Background(), "", resp, nil)
	if result.Success || result.Score != 0 {
		t.Fatalf("exactTextScorer mismatch: success=%v score=%f, want false 0", result.Success, result.Score)
	}
}

func TestExactTextScorerIncomplete(t *testing.T) {
	scorer := exactTextScorer("hello")
	resp := &gatewaycontrol.RunBenchmarkCaseResponse{
		Error: "upstream failed",
	}
	result := scorer(context.Background(), "", resp, nil)
	if result.Completed {
		t.Fatal("exactTextScorer error: completed = true, want false")
	}
}

func TestJudgeScorerNoJudge(t *testing.T) {
	scorer := judgeScorer("rubric")
	resp := &gatewaycontrol.RunBenchmarkCaseResponse{
		StatusCode:   200,
		ResponseBody: []byte(`{"choices":[{"message":{"content":"an answer"}}]}`),
	}
	result := scorer(context.Background(), "", resp, nil)
	if result.Completed {
		t.Fatal("judgeScorer nil judge: completed = true, want false")
	}
	if result.Reason != "judge_not_configured" {
		t.Fatalf("judgeScorer nil judge: reason = %q, want judge_not_configured", result.Reason)
	}
}

func TestJudgeScorerEmptyResponse(t *testing.T) {
	scorer := judgeScorer("rubric")
	resp := &gatewaycontrol.RunBenchmarkCaseResponse{
		StatusCode:   200,
		ResponseBody: []byte(`{"choices":[{"message":{"content":""}}]}`),
	}
	result := scorer(context.Background(), "", resp, func(ctx context.Context, p judgePrompt) (float64, string, error) {
		return 100, "ok", nil
	})
	if result.Score != 0 || result.Reason != "empty_response" {
		t.Fatalf("judgeScorer empty: score=%f reason=%q, want 0 empty_response", result.Score, result.Reason)
	}
}

func TestJudgeScorerJudgeError(t *testing.T) {
	scorer := judgeScorer("rubric")
	resp := &gatewaycontrol.RunBenchmarkCaseResponse{
		StatusCode:   200,
		ResponseBody: []byte(`{"choices":[{"message":{"content":"answer"}}]}`),
	}
	result := scorer(context.Background(), "", resp, func(ctx context.Context, p judgePrompt) (float64, string, error) {
		return 0, "", context.DeadlineExceeded
	})
	if result.Completed {
		t.Fatal("judgeScorer error: completed = true, want false")
	}
	if result.Reason != "judge_failed" {
		t.Fatalf("judgeScorer error: reason = %q, want judge_failed", result.Reason)
	}
}

func TestJudgeScorerSuccess(t *testing.T) {
	scorer := judgeScorer("rubric")
	resp := &gatewaycontrol.RunBenchmarkCaseResponse{
		StatusCode:   200,
		ResponseBody: []byte(`{"choices":[{"message":{"content":"good answer"}}]}`),
	}
	result := scorer(context.Background(), "", resp, func(ctx context.Context, p judgePrompt) (float64, string, error) {
		return 85, "well done", nil
	})
	if !result.Success || result.Score != 85 {
		t.Fatalf("judgeScorer success: success=%v score=%f, want true 85", result.Success, result.Score)
	}
}

func TestJsonScorerValid(t *testing.T) {
	scorer := jsonScorer()
	resp := &gatewaycontrol.RunBenchmarkCaseResponse{
		StatusCode:   200,
		ResponseBody: []byte(`{"choices":[{"message":{"content":"{\"animal\":\"cat\",\"count\":2}"}}]}`),
	}
	result := scorer(context.Background(), "", resp, nil)
	if !result.Success || result.Score != 100 {
		t.Fatalf("jsonScorer valid: success=%v score=%f, want true 100", result.Success, result.Score)
	}
	if result.Reason != "json_schema_match" {
		t.Fatalf("jsonScorer valid: reason = %q, want json_schema_match", result.Reason)
	}
}

func TestJsonScorerPartialMatch(t *testing.T) {
	scorer := jsonScorer()
	resp := &gatewaycontrol.RunBenchmarkCaseResponse{
		StatusCode:   200,
		ResponseBody: []byte(`{"choices":[{"message":{"content":"{\"animal\":\"cat\",\"count\":5}"}}]}`),
	}
	result := scorer(context.Background(), "", resp, nil)
	if result.Success || result.Score != 50 {
		t.Fatalf("jsonScorer partial: success=%v score=%f, want false 50", result.Success, result.Score)
	}
	if result.Reason != "json_schema_mismatch" {
		t.Fatalf("jsonScorer partial: reason = %q, want json_schema_mismatch", result.Reason)
	}
}

func TestJsonScorerInvalidJSON(t *testing.T) {
	scorer := jsonScorer()
	resp := &gatewaycontrol.RunBenchmarkCaseResponse{
		StatusCode:   200,
		ResponseBody: []byte(`{"choices":[{"message":{"content":"not valid json"}}]}`),
	}
	result := scorer(context.Background(), "", resp, nil)
	if result.Score != 0 || result.Reason != "invalid_json" {
		t.Fatalf("jsonScorer invalid: score=%f reason=%q, want 0 invalid_json", result.Score, result.Reason)
	}
}

func TestToolCallScorerMatch(t *testing.T) {
	scorer := toolCallScorer("lookup_weather", "Shanghai")
	resp := &gatewaycontrol.RunBenchmarkCaseResponse{
		StatusCode:   200,
		ResponseBody: []byte(`{"choices":[{"message":{"tool_calls":[{"function":{"name":"lookup_weather","arguments":"{\"city\":\"Shanghai\"}"}}]}}]}`),
	}
	result := scorer(context.Background(), "", resp, nil)
	if !result.Success || result.Score != 100 {
		t.Fatalf("toolCallScorer match: success=%v score=%f, want true 100", result.Success, result.Score)
	}
}

func TestToolCallScorerMismatch(t *testing.T) {
	scorer := toolCallScorer("lookup_weather", "Shanghai")
	resp := &gatewaycontrol.RunBenchmarkCaseResponse{
		StatusCode:   200,
		ResponseBody: []byte(`{"choices":[{"message":{"tool_calls":[{"function":{"name":"wrong_tool","arguments":"{}"}}]}}]}`),
	}
	result := scorer(context.Background(), "", resp, nil)
	if result.Success || result.Score != 0 {
		t.Fatalf("toolCallScorer mismatch: success=%v score=%f, want false 0", result.Success, result.Score)
	}
}

func TestStreamScorerFullScore(t *testing.T) {
	scorer := streamScorer()
	resp := &gatewaycontrol.RunBenchmarkCaseResponse{
		StatusCode:       200,
		ResponseBody:     []byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"),
		PromptTokens:     10,
		CompletionTokens: 5,
	}
	result := scorer(context.Background(), "", resp, nil)
	if !result.Success || result.Score != 100 {
		t.Fatalf("streamScorer full: success=%v score=%f, want true 100", result.Success, result.Score)
	}
	if result.Reason != "stream_protocol_ok" {
		t.Fatalf("streamScorer full: reason = %q, want stream_protocol_ok", result.Reason)
	}
}

func TestStreamScorerAnthropicDone(t *testing.T) {
	scorer := streamScorer()
	resp := &gatewaycontrol.RunBenchmarkCaseResponse{
		StatusCode:       200,
		ResponseBody:     []byte("event: content_block_delta\ndata: {\"delta\":{\"text\":\"hi\"}}\n\nevent: message_stop\n\n"),
		PromptTokens:     5,
		CompletionTokens: 3,
	}
	result := scorer(context.Background(), ProtocolAnthropicMessage, resp, nil)
	if !result.Success {
		t.Fatalf("streamScorer anthropic: success=%v, want true", result.Success)
	}
}

func TestStreamScorerNoData(t *testing.T) {
	scorer := streamScorer()
	resp := &gatewaycontrol.RunBenchmarkCaseResponse{
		StatusCode:   200,
		ResponseBody: []byte("just plain text"),
	}
	result := scorer(context.Background(), "", resp, nil)
	if result.Success || result.Score != 0 {
		t.Fatalf("streamScorer no data: success=%v score=%f, want false 0", result.Success, result.Score)
	}
	if result.Reason != "stream_protocol_invalid" {
		t.Fatalf("streamScorer no data: reason = %q, want stream_protocol_invalid", result.Reason)
	}
}

func TestBaseCaseResultNilResp(t *testing.T) {
	result := baseCaseResult(nil)
	if result.Completed || result.Error != "empty benchmark case response" {
		t.Fatalf("baseCaseResult(nil): completed=%v error=%q, want false empty benchmark case response", result.Completed, result.Error)
	}
}

func TestBaseCaseResultErrorResp(t *testing.T) {
	resp := &gatewaycontrol.RunBenchmarkCaseResponse{
		Error:      "upstream timeout",
		StatusCode: 504,
	}
	result := baseCaseResult(resp)
	if result.Completed {
		t.Fatal("baseCaseResult error: completed = true, want false")
	}
	if result.Error != "upstream timeout" {
		t.Fatalf("baseCaseResult error: error = %q, want upstream timeout", result.Error)
	}
}

func TestBaseCaseResultHTTPErrorStatus(t *testing.T) {
	resp := &gatewaycontrol.RunBenchmarkCaseResponse{
		StatusCode: 429,
	}
	result := baseCaseResult(resp)
	if !result.Completed {
		t.Fatal("baseCaseResult 429 (no explicit error): completed = false, want true")
	}
	if result.Error != "status_429" {
		t.Fatalf("baseCaseResult 429: error = %q, want status_429", result.Error)
	}
}

func TestBaseCaseResultFieldsCopied(t *testing.T) {
	resp := &gatewaycontrol.RunBenchmarkCaseResponse{
		StatusCode:          200,
		PromptTokens:        10,
		CachedPromptTokens:  5,
		CompletionTokens:    20,
		PricingTotalCostUSD: 0.05,
		ProviderID:          "p1",
		EffectiveModel:      "m1",
		RouteMode:           "fallback",
	}
	result := baseCaseResult(resp)
	if !result.Completed {
		t.Fatal("baseCaseResult fields: completed = false, want true")
	}
	if result.StatusCode != 200 || result.PromptTokens != 10 || result.CachedPromptTokens != 5 || result.CompletionTokens != 20 || result.CostUSD != 0.05 {
		t.Fatalf("baseCaseResult fields: unexpected field values: %+v", result)
	}
	if result.ProviderID != "p1" || result.EffectiveModel != "m1" || result.RouteMode != "fallback" {
		t.Fatalf("baseCaseResult fields: unexpected route fields: %+v", result)
	}
}

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
