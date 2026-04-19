package converter

// OpenAI 请求和响应类型定义
type OpenAIRequest struct {
	Model            string                 `json:"model"`
	Messages         []OpenAIMessage        `json:"messages"`
	Temperature      *float64               `json:"temperature,omitempty"`
	MaxTokens        *int                   `json:"max_tokens,omitempty"`
	Stream           bool                   `json:"stream,omitempty"`
	Tools            []OpenAITool           `json:"tools,omitempty"`
	FunctionCall     interface{}            `json:"function_call,omitempty"`
	Functions        []OpenAIFunction       `json:"functions,omitempty"`
	AdditionalFields map[string]interface{} `json:"-"` // 保留未识别的字段
}

type OpenAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

type OpenAITool struct {
	Type     string             `json:"type"`
	Function OpenAIToolFunction `json:"function"`
}

type OpenAIToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type OpenAIFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type OpenAIResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   OpenAIUsage    `json:"usage"`
}

type OpenAIChoice struct {
	Index        int            `json:"index"`
	Message      *OpenAIMessage `json:"message,omitempty"`
	Delta        *OpenAIMessage `json:"delta,omitempty"`
	FinishReason string         `json:"finish_reason"`
}

type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Claude 请求和响应类型定义
type ClaudeRequest struct {
	Model            string                 `json:"model"`
	Messages         []ClaudeMessage        `json:"messages"`
	System           string                 `json:"system,omitempty"`
	MaxTokens        *int                   `json:"max_tokens,omitempty"`
	Temperature      *float64               `json:"temperature,omitempty"`
	Stream           bool                   `json:"stream,omitempty"`
	Tools            []ClaudeTool           `json:"tools,omitempty"`
	AdditionalFields map[string]interface{} `json:"-"` // 保留未识别的字段
}

type ClaudeMessage struct {
	Role    string        `json:"role"`
	Content ClaudeContent `json:"content"`
}

type ClaudeContent struct {
	Text       string            `json:"text,omitempty"`
	Type       string            `json:"type,omitempty"`
	ToolUseID  string            `json:"tool_use_id,omitempty"`
	ToolResult *ClaudeToolResult `json:"tool_result,omitempty"`
}

type ClaudeToolResult struct {
	Type    string      `json:"type"`
	Content interface{} `json:"content"`
}

type ClaudeTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema,omitempty"`
}

type ClaudeResponse struct {
	ID         string               `json:"id"`
	Type       string               `json:"type"`
	Model      string               `json:"model"`
	Content    []ClaudeContentBlock `json:"content"`
	StopReason string               `json:"stop_reason,omitempty"`
	Usage      ClaudeUsage          `json:"usage"`
}

type ClaudeContentBlock struct {
	Type  string      `json:"type"`
	Text  string      `json:"text,omitempty"`
	ID    string      `json:"id,omitempty"`
	Name  string      `json:"name,omitempty"`
	Input interface{} `json:"input,omitempty"`
}

type ClaudeUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// 转换器接口
type RequestConverter interface {
	ConvertOpenAIToClaude(req *OpenAIRequest) (*ClaudeRequest, error)
	ConvertClaudeToOpenAI(req *ClaudeRequest) (*OpenAIRequest, error)
}

type ResponseConverter interface {
	ConvertClaudeToOpenAI(resp *ClaudeResponse) (*OpenAIResponse, error)
	ConvertOpenAIToClaude(resp *OpenAIResponse) (*ClaudeResponse, error)
}

// 转换配置
type ConverterConfig struct {
	EnableOpenAIToClaude bool
	EnableClaudeToOpenAI bool
	StreamBufferSize     int
}

// DefaultConverterConfig 返回默认配置
func DefaultConverterConfig() *ConverterConfig {
	return &ConverterConfig{
		EnableOpenAIToClaude: true,
		EnableClaudeToOpenAI: true,
		StreamBufferSize:     4096,
	}
}
