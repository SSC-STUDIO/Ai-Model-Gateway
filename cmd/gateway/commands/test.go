package commands

import (
	"fmt"
	"io"

	"ai-model-gateway/internal/gateway/converter"
)

type TestCommand struct {
	output io.Writer
}

func NewTestCommand(output io.Writer) *TestCommand {
	return &TestCommand{output: output}
}

func (t *TestCommand) Convert() error {
	// 创建转换器
	config := converter.DefaultConverterConfig()
	conv := converter.NewOpenAIToClaudeConverter(config)

	// 测试 OpenAI → Claude
	openaiReq := &converter.OpenAIRequest{
		Model: "gpt-4",
		Messages: []converter.OpenAIMessage{
			{Role: "user", Content: "Hello"},
		},
	}

	claudeReq, err := conv.ConvertRequest(openaiReq)
	if err != nil {
		return fmt.Errorf("OpenAI → Claude failed: %w", err)
	}

	fmt.Fprintf(t.output, "✓ OpenAI → Claude conversion: PASS\n")
	fmt.Fprintf(t.output, "  Model: %s → %s\n", openaiReq.Model, claudeReq.Model)

	// 测试 Claude → OpenAI
	claudeResp := &converter.ClaudeResponse{
		ID:    "msg-001",
		Type:  "message",
		Model: "claude-3-opus-20240229",
		Content: []converter.ClaudeContentBlock{
			{Type: "text", Text: "Hello! How can I help you?"},
		},
		StopReason: "end_turn",
		Usage: converter.ClaudeUsage{
			InputTokens:  10,
			OutputTokens: 20,
		},
	}

	openaiResp, err := conv.ConvertResponse(claudeResp)
	if err != nil {
		return fmt.Errorf("Claude → OpenAI failed: %w", err)
	}

	fmt.Fprintf(t.output, "✓ Claude → OpenAI conversion: PASS\n")
	fmt.Fprintf(t.output, "  Choices: %d\n", len(openaiResp.Choices))

	return nil
}
