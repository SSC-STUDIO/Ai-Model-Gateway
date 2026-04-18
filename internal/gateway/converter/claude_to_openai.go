package converter

import (
	"encoding/json"
	"fmt"
)

// ClaudeToOpenAIConverter 实现 Claude 到 OpenAI 的转换
type ClaudeToOpenAIConverter struct {
	config *ConverterConfig
}

// NewClaudeToOpenAIConverter 创建 Claude 到 OpenAI 转换器
func NewClaudeToOpenAIConverter(config *ConverterConfig) *ClaudeToOpenAIConverter {
	if config == nil {
		config = DefaultConverterConfig()
	}
	return &ClaudeToOpenAIConverter{config: config}
}

// ConvertRequest 将 Claude 请求转换为 OpenAI 请求
func (c *ClaudeToOpenAIConverter) ConvertRequest(claudeReq *ClaudeRequest) (*OpenAIRequest, error) {
	if claudeReq == nil {
		return nil, fmt.Errorf("claude request is nil")
	}

	openaiReq := &OpenAIRequest{
		Model:            c.mapModel(claudeReq.Model),
		MaxTokens:        claudeReq.MaxTokens,
		Temperature:      claudeReq.Temperature,
		Stream:           claudeReq.Stream,
		AdditionalFields: claudeReq.AdditionalFields,
	}

	// 转换消息
	openaiReq.Messages = c.convertMessages(claudeReq.Messages, claudeReq.System)

	// 转换工具
	if len(claudeReq.Tools) > 0 {
		openaiReq.Tools = c.convertTools(claudeReq.Tools)
	}

	return openaiReq, nil
}

// convertMessages 转换消息列表
func (c *ClaudeToOpenAIConverter) convertMessages(claudeMsgs []ClaudeMessage, systemPrompt string) []OpenAIMessage {
	var messages []OpenAIMessage

	// 添加 system 消息
	if systemPrompt != "" {
		messages = append(messages, OpenAIMessage{
			Role:    "system",
			Content: systemPrompt,
		})
	}

	// 转换普通消息
	for _, msg := range claudeMsgs {
		openaiMsg := OpenAIMessage{
			Role: msg.Role,
		}

		// 提取文本内容
		if msg.Content.Text != "" {
			openaiMsg.Content = msg.Content.Text
		}

		// 处理工具调用结果等其他类型
		if msg.Content.Type == "tool_result" && msg.Content.ToolResult != nil {
			// 将工具结果转换为文本
			resultJSON, _ := json.Marshal(msg.Content.ToolResult.Content)
			openaiMsg.Content = string(resultJSON)
			openaiMsg.Name = msg.Content.ToolUseID
		}

		messages = append(messages, openaiMsg)
	}

	return messages
}

// convertTools 转换工具定义
func (c *ClaudeToOpenAIConverter) convertTools(claudeTools []ClaudeTool) []OpenAITool {
	openaiTools := make([]OpenAITool, 0, len(claudeTools))

	for _, tool := range claudeTools {
		openaiTool := OpenAITool{
			Type: "function",
			Function: OpenAIToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		}
		openaiTools = append(openaiTools, openaiTool)
	}

	return openaiTools
}

// mapModel 映射模型名称
func (c *ClaudeToOpenAIConverter) mapModel(claudeModel string) string {
	// 反向映射模型名称
	modelMap := map[string]string{
		"claude-3-opus-20240229":   "gpt-4",
		"claude-3-sonnet-20240229": "gpt-4-turbo",
		"claude-3-haiku-20240307":  "gpt-3.5-turbo",
	}

	if mapped, ok := modelMap[claudeModel]; ok {
		return mapped
	}

	// 如果没有映射，保留原始名称
	return claudeModel
}

// ConvertResponse 将 OpenAI 响应转换为 Claude 响应
func (c *ClaudeToOpenAIConverter) ConvertResponse(openaiResp *OpenAIResponse) (*ClaudeResponse, error) {
	if openaiResp == nil {
		return nil, fmt.Errorf("openai response is nil")
	}

	claudeResp := &ClaudeResponse{
		ID:    openaiResp.ID,
		Type:  "message",
		Model: openaiResp.Model,
		Usage: ClaudeUsage{
			InputTokens:  openaiResp.Usage.PromptTokens,
			OutputTokens: openaiResp.Usage.CompletionTokens,
		},
	}

	// 转换 choices
	if len(openaiResp.Choices) > 0 {
		choice := openaiResp.Choices[0]

		// 创建内容块
		if choice.Message != nil && choice.Message.Content != "" {
			claudeResp.Content = []ClaudeContentBlock{
				{
					Type: "text",
					Text: choice.Message.Content,
				},
			}
		}

		// 转换停止原因
		claudeResp.StopReason = c.mapFinishReason(choice.FinishReason)
	}

	return claudeResp, nil
}

// mapFinishReason 映射停止原因
func (c *ClaudeToOpenAIConverter) mapFinishReason(openaiReason string) string {
	reasonMap := map[string]string{
		"stop":        "end_turn",
		"length":      "max_tokens",
		"tool_calls":  "tool_use",
		"content_filter": "end_turn",
	}

	if mapped, ok := reasonMap[openaiReason]; ok {
		return mapped
	}

	return "end_turn"
}

// ConvertStreamEvent 转换流式 SSE 事件
func (c *ClaudeToOpenAIConverter) ConvertStreamEvent(openaiEvent []byte) ([]byte, error) {
	// 解析 OpenAI 事件
	var openaiChunk OpenAIResponse
	if err := json.Unmarshal(openaiEvent, &openaiChunk); err != nil {
		return nil, fmt.Errorf("failed to unmarshal openai event: %w", err)
	}

	// 转换为 Claude 格式
	switch openaiChunk.Object {
	case "chat.completion.chunk":
		if len(openaiChunk.Choices) > 0 {
			choice := openaiChunk.Choices[0]
			
			if choice.Delta != nil {
				// 内容增量
				if choice.Delta.Content != "" {
					event := map[string]interface{}{
						"type":  "content_block_delta",
						"index": choice.Index,
						"delta": map[string]string{
							"type": "text_delta",
							"text": choice.Delta.Content,
						},
					}
					return json.Marshal(event)
				}

				// 角色信息
				if choice.Delta.Role != "" {
					event := map[string]interface{}{
						"type": "message_start",
						"message": map[string]interface{}{
							"id":    openaiChunk.ID,
							"type":  "message",
							"role":  choice.Delta.Role,
							"model": openaiChunk.Model,
						},
					}
					return json.Marshal(event)
				}
			}

			// 完成原因
			if choice.FinishReason != "" {
				event := map[string]interface{}{
					"type": "message_delta",
					"delta": map[string]string{
						"stop_reason": c.mapFinishReason(choice.FinishReason),
					},
				}
				return json.Marshal(event)
			}
		}
	}

	return nil, nil
}
