package converter

import (
	"encoding/json"
	"fmt"
)

// OpenAIToClaudeConverter 实现 OpenAI 到 Claude 的转换
type OpenAIToClaudeConverter struct {
	config *ConverterConfig
}

// NewOpenAIToClaudeConverter 创建 OpenAI 到 Claude 转换器
func NewOpenAIToClaudeConverter(config *ConverterConfig) *OpenAIToClaudeConverter {
	if config == nil {
		config = DefaultConverterConfig()
	}
	return &OpenAIToClaudeConverter{config: config}
}

// ConvertRequest 将 OpenAI 请求转换为 Claude 请求
func (c *OpenAIToClaudeConverter) ConvertRequest(openaiReq *OpenAIRequest) (*ClaudeRequest, error) {
	if openaiReq == nil {
		return nil, fmt.Errorf("openai request is nil")
	}

	claudeReq := &ClaudeRequest{
		Model:            c.mapModel(openaiReq.Model),
		MaxTokens:        openaiReq.MaxTokens,
		Temperature:      openaiReq.Temperature,
		Stream:           openaiReq.Stream,
		AdditionalFields: openaiReq.AdditionalFields,
	}

	// 转换消息
	messages, system := c.convertMessages(openaiReq.Messages)
	claudeReq.Messages = messages
	claudeReq.System = system

	// 转换工具
	if len(openaiReq.Tools) > 0 {
		claudeReq.Tools = c.convertTools(openaiReq.Tools)
	} else if len(openaiReq.Functions) > 0 {
		// 兼容旧的 functions 格式
		claudeReq.Tools = c.convertFunctionsToTools(openaiReq.Functions)
	}

	return claudeReq, nil
}

// convertMessages 转换消息列表，返回 Claude 消息和 system 提示
func (c *OpenAIToClaudeConverter) convertMessages(openaiMsgs []OpenAIMessage) ([]ClaudeMessage, string) {
	var messages []ClaudeMessage
	var systemPrompt string

	for _, msg := range openaiMsgs {
		switch msg.Role {
		case "system":
			// Claude 的 system 提示单独处理
			if systemPrompt == "" {
				systemPrompt = msg.Content
			} else {
				systemPrompt += "\n\n" + msg.Content
			}
		case "user", "assistant":
			claudeMsg := ClaudeMessage{
				Role:    msg.Role,
				Content: ClaudeContent{Text: msg.Content},
			}
			messages = append(messages, claudeMsg)
		}
	}

	return messages, systemPrompt
}

// convertTools 转换工具定义
func (c *OpenAIToClaudeConverter) convertTools(openaiTools []OpenAITool) []ClaudeTool {
	claudeTools := make([]ClaudeTool, 0, len(openaiTools))

	for _, tool := range openaiTools {
		if tool.Type == "function" {
			claudeTool := ClaudeTool{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				InputSchema: tool.Function.Parameters,
			}
			claudeTools = append(claudeTools, claudeTool)
		}
	}

	return claudeTools
}

// convertFunctionsToTools 将旧版 functions 转换为 tools
func (c *OpenAIToClaudeConverter) convertFunctionsToTools(functions []OpenAIFunction) []ClaudeTool {
	claudeTools := make([]ClaudeTool, 0, len(functions))

	for _, fn := range functions {
		claudeTool := ClaudeTool{
			Name:        fn.Name,
			Description: fn.Description,
			InputSchema: fn.Parameters,
		}
		claudeTools = append(claudeTools, claudeTool)
	}

	return claudeTools
}

// mapModel 映射模型名称
func (c *OpenAIToClaudeConverter) mapModel(openaiModel string) string {
	// 简单的模型名称映射
	// 实际应用中可能需要更复杂的映射表
	modelMap := map[string]string{
		"gpt-4":         "claude-3-opus-20240229",
		"gpt-4-turbo":   "claude-3-sonnet-20240229",
		"gpt-3.5-turbo": "claude-3-haiku-20240307",
	}

	if mapped, ok := modelMap[openaiModel]; ok {
		return mapped
	}

	// 如果没有映射，保留原始名称（可能已经是 Claude 模型名）
	return openaiModel
}

// ConvertResponse 将 Claude 响应转换为 OpenAI 响应
func (c *OpenAIToClaudeConverter) ConvertResponse(claudeResp *ClaudeResponse) (*OpenAIResponse, error) {
	if claudeResp == nil {
		return nil, fmt.Errorf("claude response is nil")
	}

	openaiResp := &OpenAIResponse{
		ID:      claudeResp.ID,
		Object:  "chat.completion",
		Created: 0, // 可以使用当前时间戳
		Model:   claudeResp.Model,
		Usage: OpenAIUsage{
			PromptTokens:     claudeResp.Usage.InputTokens,
			CompletionTokens: claudeResp.Usage.OutputTokens,
			TotalTokens:      claudeResp.Usage.InputTokens + claudeResp.Usage.OutputTokens,
		},
	}

	// 转换内容块
	choice := OpenAIChoice{
		Index:   0,
		Message: &OpenAIMessage{},
	}

	// 提取文本内容
	for _, block := range claudeResp.Content {
		if block.Type == "text" && block.Text != "" {
			choice.Message.Content = block.Text
			choice.Message.Role = "assistant"
			break
		}
	}

	// 转换停止原因
	choice.FinishReason = c.mapStopReason(claudeResp.StopReason)

	openaiResp.Choices = []OpenAIChoice{choice}

	return openaiResp, nil
}

// mapStopReason 映射停止原因
func (c *OpenAIToClaudeConverter) mapStopReason(claudeReason string) string {
	reasonMap := map[string]string{
		"end_turn":      "stop",
		"max_tokens":    "length",
		"stop_sequence": "stop",
		"tool_use":      "tool_calls",
	}

	if mapped, ok := reasonMap[claudeReason]; ok {
		return mapped
	}

	return "stop"
}

// ConvertStreamEvent 转换流式 SSE 事件
func (c *OpenAIToClaudeConverter) ConvertStreamEvent(claudeEvent []byte) ([]byte, error) {
	// 解析 Claude 事件
	var event struct {
		Type  string          `json:"type"`
		Index int             `json:"index,omitempty"`
		Delta *ClaudeDelta    `json:"delta,omitempty"`
		Message *ClaudeResponse `json:"message,omitempty"`
	}

	if err := json.Unmarshal(claudeEvent, &event); err != nil {
		return nil, fmt.Errorf("failed to unmarshal claude event: %w", err)
	}

	// 转换为 OpenAI 格式
	openaiChunk := &OpenAIResponse{
		ID:      "",
		Object:  "chat.completion.chunk",
		Created: 0,
		Model:   "",
	}

	switch event.Type {
	case "content_block_delta":
		if event.Delta != nil && event.Delta.Text != "" {
			openaiChunk.Choices = []OpenAIChoice{
				{
					Index: event.Index,
					Delta: &OpenAIMessage{
						Content: event.Delta.Text,
						Role:    "assistant",
					},
				},
			}
		}

	case "message_start":
		if event.Message != nil {
			openaiChunk.ID = event.Message.ID
			openaiChunk.Model = event.Message.Model
			openaiChunk.Choices = []OpenAIChoice{
				{
					Index: 0,
					Delta: &OpenAIMessage{
						Role: "assistant",
					},
				},
			}
		}

	case "message_delta":
		if event.Delta != nil && event.Delta.StopReason != "" {
			openaiChunk.Choices = []OpenAIChoice{
				{
					Index:        0,
					FinishReason: c.mapStopReason(event.Delta.StopReason),
				},
			}
		}
	}

	return json.Marshal(openaiChunk)
}

// ClaudeDelta Claude 流式事件增量
type ClaudeDelta struct {
	Type       string `json:"type,omitempty"`
	Text       string `json:"text,omitempty"`
	StopReason string `json:"stop_reason,omitempty"`
}
