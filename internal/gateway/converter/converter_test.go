package converter

import (
	"encoding/json"
	"testing"
)

// ============ OpenAI to Claude Tests ============

func TestOpenAIToClaudeConverter_ConvertRequest(t *testing.T) {
	converter := NewOpenAIToClaudeConverter(nil)

	tests := []struct {
		name     string
		input    *OpenAIRequest
		wantErr  bool
		validate func(*testing.T, *ClaudeRequest)
	}{
		{
			name:    "nil request",
			input:   nil,
			wantErr: true,
		},
		{
			name: "basic request",
			input: &OpenAIRequest{
				Model: "gpt-4",
				Messages: []OpenAIMessage{
					{Role: "user", Content: "Hello"},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, got *ClaudeRequest) {
				if got.Model != "claude-3-opus-20240229" {
					t.Errorf("expected claude-3-opus-20240229, got %s", got.Model)
				}
				if len(got.Messages) != 1 {
					t.Errorf("expected 1 message, got %d", len(got.Messages))
				}
			},
		},
		{
			name: "request with system message",
			input: &OpenAIRequest{
				Model: "gpt-4-turbo",
				Messages: []OpenAIMessage{
					{Role: "system", Content: "You are helpful"},
					{Role: "user", Content: "Hi"},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, got *ClaudeRequest) {
				if got.System != "You are helpful" {
					t.Errorf("expected system prompt, got %s", got.System)
				}
				if len(got.Messages) != 1 {
					t.Errorf("expected 1 message (excluding system), got %d", len(got.Messages))
				}
			},
		},
		{
			name: "request with tools",
			input: &OpenAIRequest{
				Model: "gpt-3.5-turbo",
				Messages: []OpenAIMessage{
					{Role: "user", Content: "What's the weather?"},
				},
				Tools: []OpenAITool{
					{
						Type: "function",
						Function: OpenAIToolFunction{
							Name:        "get_weather",
							Description: "Get weather",
							Parameters: map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"location": map[string]string{"type": "string"},
								},
							},
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, got *ClaudeRequest) {
				if len(got.Tools) != 1 {
					t.Errorf("expected 1 tool, got %d", len(got.Tools))
				}
				if got.Tools[0].Name != "get_weather" {
					t.Errorf("expected tool name get_weather, got %s", got.Tools[0].Name)
				}
			},
		},
		{
			name: "request with functions (legacy)",
			input: &OpenAIRequest{
				Model: "gpt-4",
				Messages: []OpenAIMessage{
					{Role: "user", Content: "Test"},
				},
				Functions: []OpenAIFunction{
					{
						Name:        "test_func",
						Description: "Test function",
						Parameters: map[string]interface{}{
							"type": "object",
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, got *ClaudeRequest) {
				if len(got.Tools) != 1 {
					t.Errorf("expected 1 tool, got %d", len(got.Tools))
				}
			},
		},
		{
			name: "request with temperature and max_tokens",
			input: &OpenAIRequest{
				Model:       "gpt-4",
				Messages:    []OpenAIMessage{{Role: "user", Content: "Test"}},
				Temperature: floatPtr(0.7),
				MaxTokens:   intPtr(100),
				Stream:      true,
			},
			wantErr: false,
			validate: func(t *testing.T, got *ClaudeRequest) {
				if got.Temperature == nil || *got.Temperature != 0.7 {
					t.Errorf("temperature not preserved")
				}
				if got.MaxTokens == nil || *got.MaxTokens != 100 {
					t.Errorf("max_tokens not preserved")
				}
				if !got.Stream {
					t.Errorf("stream not preserved")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := converter.ConvertRequest(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConvertRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.validate != nil {
				tt.validate(t, got)
			}
		})
	}
}

func TestOpenAIToClaudeConverter_ConvertResponse(t *testing.T) {
	converter := NewOpenAIToClaudeConverter(nil)

	tests := []struct {
		name     string
		input    *ClaudeResponse
		wantErr  bool
		validate func(*testing.T, *OpenAIResponse)
	}{
		{
			name:    "nil response",
			input:   nil,
			wantErr: true,
		},
		{
			name: "basic response",
			input: &ClaudeResponse{
				ID:    "msg_123",
				Model: "claude-3-opus-20240229",
				Content: []ClaudeContentBlock{
					{Type: "text", Text: "Hello!"},
				},
				StopReason: "end_turn",
				Usage: ClaudeUsage{
					InputTokens:  10,
					OutputTokens: 5,
				},
			},
			wantErr: false,
			validate: func(t *testing.T, got *OpenAIResponse) {
				if got.ID != "msg_123" {
					t.Errorf("expected ID msg_123, got %s", got.ID)
				}
				if got.Object != "chat.completion" {
					t.Errorf("expected object chat.completion, got %s", got.Object)
				}
				if len(got.Choices) != 1 {
					t.Errorf("expected 1 choice, got %d", len(got.Choices))
				}
				if got.Choices[0].Message.Content != "Hello!" {
					t.Errorf("expected content Hello!, got %s", got.Choices[0].Message.Content)
				}
				if got.Choices[0].FinishReason != "stop" {
					t.Errorf("expected finish_reason stop, got %s", got.Choices[0].FinishReason)
				}
				if got.Usage.TotalTokens != 15 {
					t.Errorf("expected total tokens 15, got %d", got.Usage.TotalTokens)
				}
			},
		},
		{
			name: "response with max_tokens stop",
			input: &ClaudeResponse{
				ID:         "msg_456",
				Model:      "claude-3-sonnet-20240229",
				Content:    []ClaudeContentBlock{{Type: "text", Text: "Partial"}},
				StopReason: "max_tokens",
				Usage:      ClaudeUsage{InputTokens: 5, OutputTokens: 10},
			},
			wantErr: false,
			validate: func(t *testing.T, got *OpenAIResponse) {
				if got.Choices[0].FinishReason != "length" {
					t.Errorf("expected finish_reason length, got %s", got.Choices[0].FinishReason)
				}
			},
		},
		{
			name: "response with tool_use stop",
			input: &ClaudeResponse{
				ID:         "msg_789",
				Model:      "claude-3-haiku-20240307",
				Content:    []ClaudeContentBlock{{Type: "text", Text: ""}},
				StopReason: "tool_use",
				Usage:      ClaudeUsage{InputTokens: 20, OutputTokens: 15},
			},
			wantErr: false,
			validate: func(t *testing.T, got *OpenAIResponse) {
				if got.Choices[0].FinishReason != "tool_calls" {
					t.Errorf("expected finish_reason tool_calls, got %s", got.Choices[0].FinishReason)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := converter.ConvertResponse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConvertResponse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.validate != nil {
				tt.validate(t, got)
			}
		})
	}
}

func TestOpenAIToClaudeConverter_ConvertStreamEvent(t *testing.T) {
	converter := NewOpenAIToClaudeConverter(nil)

	tests := []struct {
		name     string
		input    map[string]interface{}
		wantErr  bool
		validate func(*testing.T, []byte)
	}{
		{
			name: "content_block_delta",
			input: map[string]interface{}{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]string{
					"type": "text_delta",
					"text": "Hello",
				},
			},
			wantErr: false,
			validate: func(t *testing.T, got []byte) {
				var chunk OpenAIResponse
				if err := json.Unmarshal(got, &chunk); err != nil {
					t.Errorf("failed to unmarshal result: %v", err)
					return
				}
				if len(chunk.Choices) != 1 {
					t.Errorf("expected 1 choice, got %d", len(chunk.Choices))
				}
				if chunk.Choices[0].Delta.Content != "Hello" {
					t.Errorf("expected content Hello, got %s", chunk.Choices[0].Delta.Content)
				}
			},
		},
		{
			name: "message_start",
			input: map[string]interface{}{
				"type": "message_start",
				"message": map[string]interface{}{
					"id":    "msg_123",
					"model": "claude-3-opus-20240229",
				},
			},
			wantErr: false,
			validate: func(t *testing.T, got []byte) {
				var chunk OpenAIResponse
				if err := json.Unmarshal(got, &chunk); err != nil {
					t.Errorf("failed to unmarshal result: %v", err)
					return
				}
				if chunk.ID != "msg_123" {
					t.Errorf("expected ID msg_123, got %s", chunk.ID)
				}
			},
		},
		{
			name: "message_delta with stop",
			input: map[string]interface{}{
				"type": "message_delta",
				"delta": map[string]string{
					"stop_reason": "end_turn",
				},
			},
			wantErr: false,
			validate: func(t *testing.T, got []byte) {
				var chunk OpenAIResponse
				if err := json.Unmarshal(got, &chunk); err != nil {
					t.Errorf("failed to unmarshal result: %v", err)
					return
				}
				if chunk.Choices[0].FinishReason != "stop" {
					t.Errorf("expected finish_reason stop, got %s", chunk.Choices[0].FinishReason)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, err := json.Marshal(tt.input)
			if err != nil {
				t.Fatalf("failed to marshal input: %v", err)
			}

			got, err := converter.ConvertStreamEvent(input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConvertStreamEvent() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.validate != nil {
				tt.validate(t, got)
			}
		})
	}
}

// ============ Claude to OpenAI Tests ============

func TestClaudeToOpenAIConverter_ConvertRequest(t *testing.T) {
	converter := NewClaudeToOpenAIConverter(nil)

	tests := []struct {
		name     string
		input    *ClaudeRequest
		wantErr  bool
		validate func(*testing.T, *OpenAIRequest)
	}{
		{
			name:    "nil request",
			input:   nil,
			wantErr: true,
		},
		{
			name: "basic request",
			input: &ClaudeRequest{
				Model:    "claude-3-opus-20240229",
				Messages: []ClaudeMessage{{Role: "user", Content: ClaudeContent{Text: "Hello"}}},
			},
			wantErr: false,
			validate: func(t *testing.T, got *OpenAIRequest) {
				if got.Model != "gpt-4" {
					t.Errorf("expected gpt-4, got %s", got.Model)
				}
				if len(got.Messages) != 1 {
					t.Errorf("expected 1 message, got %d", len(got.Messages))
				}
			},
		},
		{
			name: "request with system prompt",
			input: &ClaudeRequest{
				Model:    "claude-3-sonnet-20240229",
				System:   "You are helpful",
				Messages: []ClaudeMessage{{Role: "user", Content: ClaudeContent{Text: "Hi"}}},
			},
			wantErr: false,
			validate: func(t *testing.T, got *OpenAIRequest) {
				if len(got.Messages) != 2 {
					t.Errorf("expected 2 messages (including system), got %d", len(got.Messages))
				}
				if got.Messages[0].Role != "system" {
					t.Errorf("expected first message to be system, got %s", got.Messages[0].Role)
				}
			},
		},
		{
			name: "request with tools",
			input: &ClaudeRequest{
				Model:    "claude-3-haiku-20240307",
				Messages: []ClaudeMessage{{Role: "user", Content: ClaudeContent{Text: "Test"}}},
				Tools: []ClaudeTool{
					{
						Name:        "test_tool",
						Description: "Test tool",
						InputSchema: map[string]interface{}{"type": "object"},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, got *OpenAIRequest) {
				if len(got.Tools) != 1 {
					t.Errorf("expected 1 tool, got %d", len(got.Tools))
				}
				if got.Tools[0].Type != "function" {
					t.Errorf("expected tool type function, got %s", got.Tools[0].Type)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := converter.ConvertRequest(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConvertRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.validate != nil {
				tt.validate(t, got)
			}
		})
	}
}

func TestClaudeToOpenAIConverter_ConvertResponse(t *testing.T) {
	converter := NewClaudeToOpenAIConverter(nil)

	tests := []struct {
		name     string
		input    *OpenAIResponse
		wantErr  bool
		validate func(*testing.T, *ClaudeResponse)
	}{
		{
			name:    "nil response",
			input:   nil,
			wantErr: true,
		},
		{
			name: "basic response",
			input: &OpenAIResponse{
				ID:     "chatcmpl-123",
				Model:  "gpt-4",
				Object: "chat.completion",
				Choices: []OpenAIChoice{
					{
						Index: 0,
						Message: &OpenAIMessage{
							Role:    "assistant",
							Content: "Hello!",
						},
						FinishReason: "stop",
					},
				},
				Usage: OpenAIUsage{
					PromptTokens:     10,
					CompletionTokens: 5,
					TotalTokens:      15,
				},
			},
			wantErr: false,
			validate: func(t *testing.T, got *ClaudeResponse) {
				if got.ID != "chatcmpl-123" {
					t.Errorf("expected ID chatcmpl-123, got %s", got.ID)
				}
				if got.Type != "message" {
					t.Errorf("expected type message, got %s", got.Type)
				}
				if len(got.Content) != 1 {
					t.Errorf("expected 1 content block, got %d", len(got.Content))
				}
				if got.Content[0].Text != "Hello!" {
					t.Errorf("expected text Hello!, got %s", got.Content[0].Text)
				}
				if got.StopReason != "end_turn" {
					t.Errorf("expected stop_reason end_turn, got %s", got.StopReason)
				}
			},
		},
		{
			name: "response with length finish",
			input: &OpenAIResponse{
				ID:     "chatcmpl-456",
				Model:  "gpt-4-turbo",
				Object: "chat.completion",
				Choices: []OpenAIChoice{
					{
						Index:        0,
						Message:      &OpenAIMessage{Role: "assistant", Content: "Partial"},
						FinishReason: "length",
					},
				},
				Usage: OpenAIUsage{PromptTokens: 5, CompletionTokens: 10},
			},
			wantErr: false,
			validate: func(t *testing.T, got *ClaudeResponse) {
				if got.StopReason != "max_tokens" {
					t.Errorf("expected stop_reason max_tokens, got %s", got.StopReason)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := converter.ConvertResponse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConvertResponse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.validate != nil {
				tt.validate(t, got)
			}
		})
	}
}

// ============ Helper Functions ============

func floatPtr(f float64) *float64 {
	return &f
}

func intPtr(i int) *int {
	return &i
}

// ============ Edge Case Tests ============

func TestOpenAIToClaudeConverter_ModelMapping(t *testing.T) {
	converter := NewOpenAIToClaudeConverter(nil)

	tests := []struct {
		input    string
		expected string
	}{
		{"gpt-4", "claude-3-opus-20240229"},
		{"gpt-4-turbo", "claude-3-sonnet-20240229"},
		{"gpt-3.5-turbo", "claude-3-haiku-20240307"},
		{"unknown-model", "unknown-model"},                   // unmapped model
		{"claude-3-opus-20240229", "claude-3-opus-20240229"}, // already Claude
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			req := &OpenAIRequest{
				Model:    tt.input,
				Messages: []OpenAIMessage{{Role: "user", Content: "test"}},
			}
			got, err := converter.ConvertRequest(req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Model != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, got.Model)
			}
		})
	}
}

func TestClaudeToOpenAIConverter_ModelMapping(t *testing.T) {
	converter := NewClaudeToOpenAIConverter(nil)

	tests := []struct {
		input    string
		expected string
	}{
		{"claude-3-opus-20240229", "gpt-4"},
		{"claude-3-sonnet-20240229", "gpt-4-turbo"},
		{"claude-3-haiku-20240307", "gpt-3.5-turbo"},
		{"unknown-model", "unknown-model"},
		{"gpt-4", "gpt-4"}, // already OpenAI
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			req := &ClaudeRequest{
				Model:    tt.input,
				Messages: []ClaudeMessage{{Role: "user", Content: ClaudeContent{Text: "test"}}},
			}
			got, err := converter.ConvertRequest(req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Model != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, got.Model)
			}
		})
	}
}

func TestOpenAIToClaudeConverter_EmptyMessages(t *testing.T) {
	converter := NewOpenAIToClaudeConverter(nil)

	req := &OpenAIRequest{
		Model:    "gpt-4",
		Messages: []OpenAIMessage{},
	}

	got, err := converter.ConvertRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got.Messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(got.Messages))
	}
}

func TestClaudeToOpenAIConverter_EmptyMessages(t *testing.T) {
	converter := NewClaudeToOpenAIConverter(nil)

	req := &ClaudeRequest{
		Model:    "claude-3-opus-20240229",
		Messages: []ClaudeMessage{},
	}

	got, err := converter.ConvertRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got.Messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(got.Messages))
	}
}

func TestOpenAIToClaudeConverter_MultipleSystemMessages(t *testing.T) {
	converter := NewOpenAIToClaudeConverter(nil)

	req := &OpenAIRequest{
		Model: "gpt-4",
		Messages: []OpenAIMessage{
			{Role: "system", Content: "First system"},
			{Role: "system", Content: "Second system"},
			{Role: "user", Content: "Hello"},
		},
	}

	got, err := converter.ConvertRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "First system\n\nSecond system"
	if got.System != expected {
		t.Errorf("expected %q, got %q", expected, got.System)
	}
}

func TestClaudeToOpenAIConverter_ToolResultContent(t *testing.T) {
	converter := NewClaudeToOpenAIConverter(nil)

	req := &ClaudeRequest{
		Model: "claude-3-opus-20240229",
		Messages: []ClaudeMessage{
			{
				Role: "user",
				Content: ClaudeContent{
					Type:      "tool_result",
					ToolUseID: "tool_123",
					ToolResult: &ClaudeToolResult{
						Type:    "tool_result",
						Content: "result data",
					},
				},
			},
		},
	}

	got, err := converter.ConvertRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(got.Messages))
	}

	if got.Messages[0].Name != "tool_123" {
		t.Errorf("expected name tool_123, got %s", got.Messages[0].Name)
	}
}

func TestOpenAIToClaudeConverter_AdditionalFields(t *testing.T) {
	converter := NewOpenAIToClaudeConverter(nil)

	req := &OpenAIRequest{
		Model: "gpt-4",
		Messages: []OpenAIMessage{
			{Role: "user", Content: "test"},
		},
		AdditionalFields: map[string]interface{}{
			"custom_field": "custom_value",
		},
	}

	got, err := converter.ConvertRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.AdditionalFields == nil {
		t.Error("expected additional fields to be preserved")
	}
}

func TestClaudeToOpenAIConverter_AdditionalFields(t *testing.T) {
	converter := NewClaudeToOpenAIConverter(nil)

	req := &ClaudeRequest{
		Model: "claude-3-opus-20240229",
		Messages: []ClaudeMessage{
			{Role: "user", Content: ClaudeContent{Text: "test"}},
		},
		AdditionalFields: map[string]interface{}{
			"custom_field": "custom_value",
		},
	}

	got, err := converter.ConvertRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.AdditionalFields == nil {
		t.Error("expected additional fields to be preserved")
	}
}

func TestOpenAIToClaudeConverter_InvalidJSON(t *testing.T) {
	converter := NewOpenAIToClaudeConverter(nil)

	_, err := converter.ConvertStreamEvent([]byte("invalid json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestClaudeToOpenAIConverter_EmptyContent(t *testing.T) {
	converter := NewClaudeToOpenAIConverter(nil)

	req := &ClaudeRequest{
		Model: "claude-3-opus-20240229",
		Messages: []ClaudeMessage{
			{Role: "user", Content: ClaudeContent{Text: ""}},
		},
	}

	got, err := converter.ConvertRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(got.Messages))
	}
}

func TestDefaultConverterConfig(t *testing.T) {
	config := DefaultConverterConfig()

	if !config.EnableOpenAIToClaude {
		t.Error("expected EnableOpenAIToClaude to be true")
	}
	if !config.EnableClaudeToOpenAI {
		t.Error("expected EnableClaudeToOpenAI to be true")
	}
	if config.StreamBufferSize != 4096 {
		t.Errorf("expected StreamBufferSize 4096, got %d", config.StreamBufferSize)
	}
}
