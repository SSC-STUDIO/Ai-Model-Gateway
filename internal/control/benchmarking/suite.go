package benchmarking

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/core"
)

type judgeFunc func(ctx context.Context, prompt judgePrompt) (score float64, reason string, err error)

type judgePrompt struct {
	Dimension string
	Prompt    string
	Response  string
	Rubric    string
}

type benchmarkSuite struct {
	Name             string
	DimensionWeights map[string]float64
	Cases            []benchmarkCaseDefinition
}

type benchmarkCaseDefinition struct {
	ID        string
	Dimension string
	Kind      string
	Critical  bool
	Stream    bool
	build     func(protocol, model string) ([]byte, error)
	score     func(ctx context.Context, protocol string, resp *gatewaycontrol.RunBenchmarkCaseResponse, judge judgeFunc) RunCaseResult
}

func suiteByName(name string) (*benchmarkSuite, error) {
	if strings.TrimSpace(name) == "" {
		name = core.BenchmarkSuiteGeneralProtocolV1
	}
	switch name {
	case core.BenchmarkSuiteGeneralProtocolV1:
		return generalProtocolSuite(), nil
	default:
		return nil, fmt.Errorf("unsupported benchmark suite: %s", name)
	}
}

func generalProtocolSuite() *benchmarkSuite {
	return &benchmarkSuite{
		Name: core.BenchmarkSuiteGeneralProtocolV1,
		DimensionWeights: map[string]float64{
			"reasoning":       35,
			"coding_proxy":    25,
			"instruction":     15,
			"tool_json":       15,
			"stream_protocol": 10,
		},
		Cases: []benchmarkCaseDefinition{
			{
				ID:        "reasoning_exact",
				Dimension: "reasoning",
				Kind:      "exact",
				build: func(protocol, model string) ([]byte, error) {
					return buildRequest(protocol, model, false,
						"You are a precise assistant. Reply with only the final answer.",
						"What is 9 + 8? Return only the number.",
						nil,
					)
				},
				score: exactTextScorer("17"),
			},
			{
				ID:        "reasoning_judge",
				Dimension: "reasoning",
				Kind:      "judge",
				build: func(protocol, model string) ([]byte, error) {
					return buildRequest(protocol, model, false,
						"You are a careful assistant. Keep the answer concise and technically correct.",
						"Explain in 2-3 sentences why binary search requires a sorted array.",
						nil,
					)
				},
				score: judgeScorer("The answer should explain that ordering lets each comparison eliminate half the remaining search space."),
			},
			{
				ID:        "coding_proxy",
				Dimension: "coding_proxy",
				Kind:      "judge",
				build: func(protocol, model string) ([]byte, error) {
					return buildRequest(protocol, model, false,
						"You are a senior engineer. Return only code.",
						"Write a Python function fib(n) that returns the nth Fibonacci number using iteration.",
						nil,
					)
				},
				score: judgeScorer("The answer should be valid Python, iterative rather than recursive, and should return the nth Fibonacci number."),
			},
			{
				ID:        "instruction_exact",
				Dimension: "instruction",
				Kind:      "exact",
				build: func(protocol, model string) ([]byte, error) {
					return buildRequest(protocol, model, false,
						"Follow the user's formatting instructions exactly.",
						"Answer with exactly three lowercase words separated by a single space: alpha beta gamma",
						nil,
					)
				},
				score: exactTextScorer("alpha beta gamma"),
			},
			{
				ID:        "json_schema",
				Dimension: "tool_json",
				Kind:      "json",
				Critical:  true,
				build: func(protocol, model string) ([]byte, error) {
					return buildRequest(protocol, model, false,
						"Return strict JSON only, no markdown.",
						`Return a JSON object exactly with keys "animal" and "count" where animal is "cat" and count is the number 2.`,
						nil,
					)
				},
				score: jsonScorer(),
			},
			{
				ID:        "tool_call",
				Dimension: "tool_json",
				Kind:      "tool_call",
				Critical:  true,
				build: func(protocol, model string) ([]byte, error) {
					return buildToolRequest(protocol, model)
				},
				score: toolCallScorer("lookup_weather", "Shanghai"),
			},
			{
				ID:        "stream_protocol",
				Dimension: "stream_protocol",
				Kind:      "stream",
				Critical:  true,
				Stream:    true,
				build: func(protocol, model string) ([]byte, error) {
					return buildRequest(protocol, model, true,
						"You are a precise assistant. Reply with only the final answer.",
						"What is 6 * 7? Return only the number.",
						nil,
					)
				},
				score: streamScorer(),
			},
		},
	}
}

func buildRequest(protocol, model string, stream bool, systemPrompt, userPrompt string, extras map[string]interface{}) ([]byte, error) {
	switch protocol {
	case "", ProtocolOpenAIChat:
		payload := map[string]interface{}{
			"model":       model,
			"stream":      stream,
			"temperature": 0,
			"messages": []map[string]interface{}{
				{"role": "system", "content": systemPrompt},
				{"role": "user", "content": userPrompt},
			},
		}
		for k, v := range extras {
			payload[k] = v
		}
		return json.Marshal(payload)
	case ProtocolAnthropicMessage:
		payload := map[string]interface{}{
			"model":       model,
			"stream":      stream,
			"max_tokens":  256,
			"temperature": 0,
			"system":      systemPrompt,
			"messages": []map[string]interface{}{
				{"role": "user", "content": userPrompt},
			},
		}
		for k, v := range extras {
			payload[k] = v
		}
		return json.Marshal(payload)
	default:
		return nil, fmt.Errorf("unsupported benchmark protocol: %s", protocol)
	}
}

func buildToolRequest(protocol, model string) ([]byte, error) {
	switch protocol {
	case "", ProtocolOpenAIChat:
		return buildRequest(protocol, model, false,
			"You must use the provided tool.",
			"Call the weather lookup tool for Shanghai. Do not answer in plain text.",
			map[string]interface{}{
				"tool_choice": map[string]interface{}{
					"type": "function",
					"function": map[string]interface{}{
						"name": "lookup_weather",
					},
				},
				"tools": []map[string]interface{}{
					{
						"type": "function",
						"function": map[string]interface{}{
							"name":        "lookup_weather",
							"description": "Lookup the weather for a city.",
							"parameters": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"city": map[string]interface{}{"type": "string"},
								},
								"required": []string{"city"},
							},
						},
					},
				},
			},
		)
	case ProtocolAnthropicMessage:
		return buildRequest(protocol, model, false,
			"You must use the provided tool.",
			"Call the weather lookup tool for Shanghai. Do not answer in plain text.",
			map[string]interface{}{
				"tool_choice": map[string]interface{}{
					"type": "tool",
					"name": "lookup_weather",
				},
				"tools": []map[string]interface{}{
					{
						"name":        "lookup_weather",
						"description": "Lookup the weather for a city.",
						"input_schema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"city": map[string]interface{}{"type": "string"},
							},
							"required": []string{"city"},
						},
					},
				},
			},
		)
	default:
		return nil, fmt.Errorf("unsupported benchmark protocol: %s", protocol)
	}
}

func exactTextScorer(expected string) func(ctx context.Context, protocol string, resp *gatewaycontrol.RunBenchmarkCaseResponse, judge judgeFunc) RunCaseResult {
	return func(ctx context.Context, protocol string, resp *gatewaycontrol.RunBenchmarkCaseResponse, judge judgeFunc) RunCaseResult {
		result := baseCaseResult(resp)
		if !result.Completed {
			return result
		}
		answer := strings.TrimSpace(extractAssistantText(protocol, resp.ResponseBody))
		result.ResponseExcerpt = excerpt(answer)
		if answer == expected {
			result.Success = true
			result.Score = 100
			result.Reason = "exact_match"
			return result
		}
		result.Score = 0
		result.Reason = fmt.Sprintf("expected_exact_%q", expected)
		return result
	}
}

func judgeScorer(rubric string) func(ctx context.Context, protocol string, resp *gatewaycontrol.RunBenchmarkCaseResponse, judge judgeFunc) RunCaseResult {
	return func(ctx context.Context, protocol string, resp *gatewaycontrol.RunBenchmarkCaseResponse, judge judgeFunc) RunCaseResult {
		result := baseCaseResult(resp)
		if !result.Completed {
			return result
		}
		answer := strings.TrimSpace(extractAssistantText(protocol, resp.ResponseBody))
		result.ResponseExcerpt = excerpt(answer)
		if answer == "" {
			result.Score = 0
			result.Reason = "empty_response"
			return result
		}
		if judge == nil {
			result.Completed = false
			result.Error = "judge not configured"
			result.Reason = "judge_not_configured"
			return result
		}
		score, reason, err := judge(ctx, judgePrompt{
			Dimension: "",
			Prompt:    "",
			Response:  answer,
			Rubric:    rubric,
		})
		if err != nil {
			result.Completed = false
			result.Error = err.Error()
			result.Reason = "judge_failed"
			return result
		}
		result.Score = clampScore(score)
		result.Success = result.Score >= 70
		result.Reason = strings.TrimSpace(reason)
		return result
	}
}

func jsonScorer() func(ctx context.Context, protocol string, resp *gatewaycontrol.RunBenchmarkCaseResponse, judge judgeFunc) RunCaseResult {
	return func(ctx context.Context, protocol string, resp *gatewaycontrol.RunBenchmarkCaseResponse, judge judgeFunc) RunCaseResult {
		result := baseCaseResult(resp)
		if !result.Completed {
			return result
		}
		answer := strings.TrimSpace(extractAssistantText(protocol, resp.ResponseBody))
		result.ResponseExcerpt = excerpt(answer)
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(answer), &payload); err != nil {
			result.Score = 0
			result.Reason = "invalid_json"
			return result
		}
		if payload["animal"] == "cat" {
			result.Score += 50
		}
		if value, ok := payload["count"].(float64); ok && int(value) == 2 {
			result.Score += 50
		}
		result.Success = result.Score == 100
		if result.Success {
			result.Reason = "json_schema_match"
		} else {
			result.Reason = "json_schema_mismatch"
		}
		return result
	}
}

func toolCallScorer(expectedTool, expectedCity string) func(ctx context.Context, protocol string, resp *gatewaycontrol.RunBenchmarkCaseResponse, judge judgeFunc) RunCaseResult {
	return func(ctx context.Context, protocol string, resp *gatewaycontrol.RunBenchmarkCaseResponse, judge judgeFunc) RunCaseResult {
		result := baseCaseResult(resp)
		if !result.Completed {
			return result
		}
		ok, excerptText := extractToolCall(protocol, resp.ResponseBody, expectedTool, expectedCity)
		result.ResponseExcerpt = excerpt(excerptText)
		if ok {
			result.Score = 100
			result.Success = true
			result.Reason = "tool_call_match"
			return result
		}
		result.Score = 0
		result.Reason = "tool_call_missing_or_invalid"
		return result
	}
}

func streamScorer() func(ctx context.Context, protocol string, resp *gatewaycontrol.RunBenchmarkCaseResponse, judge judgeFunc) RunCaseResult {
	return func(ctx context.Context, protocol string, resp *gatewaycontrol.RunBenchmarkCaseResponse, judge judgeFunc) RunCaseResult {
		result := baseCaseResult(resp)
		if !result.Completed {
			return result
		}
		body := string(resp.ResponseBody)
		result.ResponseExcerpt = excerpt(body)
		hasData := strings.Contains(body, "data:")
		hasDone := strings.Contains(body, "[DONE]") || strings.Contains(body, "message_stop")
		score := 0.0
		if hasData {
			score += 50
		}
		if hasDone {
			score += 25
		}
		if resp.PromptTokens > 0 || resp.CompletionTokens > 0 {
			score += 25
		}
		result.Score = score
		result.Success = score >= 75
		if result.Success {
			result.Reason = "stream_protocol_ok"
		} else {
			result.Reason = "stream_protocol_invalid"
		}
		return result
	}
}

func baseCaseResult(resp *gatewaycontrol.RunBenchmarkCaseResponse) RunCaseResult {
	result := RunCaseResult{}
	if resp == nil {
		result.Error = "empty benchmark case response"
		return result
	}
	result.Completed = resp.Error == ""
	result.StatusCode = resp.StatusCode
	result.LatencyMs = resp.LatencyMs
	result.PromptTokens = resp.PromptTokens
	result.CachedPromptTokens = resp.CachedPromptTokens
	result.CompletionTokens = resp.CompletionTokens
	result.CostUSD = resp.PricingTotalCostUSD
	result.ProviderID = resp.ProviderID
	result.EffectiveModel = resp.EffectiveModel
	result.RouteMode = resp.RouteMode
	if resp.Error != "" {
		result.Error = resp.Error
	}
	if resp.StatusCode >= 400 && result.Error == "" {
		result.Error = fmt.Sprintf("status_%d", resp.StatusCode)
	}
	return result
}

func extractAssistantText(protocol string, body []byte) string {
	switch protocol {
	case ProtocolAnthropicMessage:
		var payload struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(body, &payload); err == nil {
			parts := make([]string, 0, len(payload.Content))
			for _, item := range payload.Content {
				if item.Type == "text" && strings.TrimSpace(item.Text) != "" {
					parts = append(parts, item.Text)
				}
			}
			if len(parts) > 0 {
				return strings.Join(parts, "\n")
			}
		}
	default:
		var payload struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(body, &payload); err == nil && len(payload.Choices) > 0 {
			return payload.Choices[0].Message.Content
		}
	}
	return string(body)
}

func extractToolCall(protocol string, body []byte, expectedTool, expectedCity string) (bool, string) {
	switch protocol {
	case ProtocolAnthropicMessage:
		var payload struct {
			Content []struct {
				Type  string                 `json:"type"`
				Name  string                 `json:"name"`
				Input map[string]interface{} `json:"input"`
			} `json:"content"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return false, string(body)
		}
		for _, item := range payload.Content {
			if item.Type != "tool_use" || item.Name != expectedTool {
				continue
			}
			city, _ := item.Input["city"].(string)
			return strings.EqualFold(strings.TrimSpace(city), expectedCity), fmt.Sprintf("%s(%s)", item.Name, city)
		}
	default:
		var payload struct {
			Choices []struct {
				Message struct {
					ToolCalls []struct {
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return false, string(body)
		}
		if len(payload.Choices) == 0 || len(payload.Choices[0].Message.ToolCalls) == 0 {
			return false, string(body)
		}
		call := payload.Choices[0].Message.ToolCalls[0]
		if call.Function.Name != expectedTool {
			return false, call.Function.Name
		}
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			return false, call.Function.Arguments
		}
		city, _ := args["city"].(string)
		return strings.EqualFold(strings.TrimSpace(city), expectedCity), fmt.Sprintf("%s(%s)", call.Function.Name, city)
	}
	return false, string(body)
}

func clampScore(score float64) float64 {
	switch {
	case score < 0:
		return 0
	case score > 100:
		return 100
	default:
		return score
	}
}

func excerpt(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 320 {
		return value[:320]
	}
	return value
}
