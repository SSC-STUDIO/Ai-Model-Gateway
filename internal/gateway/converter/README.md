# Protocol Converter Package

This package implements bidirectional protocol conversion between OpenAI and Claude API formats.

## Overview

The converter package provides:
- OpenAI → Claude request/response conversion
- Claude → OpenAI request/response conversion
- Streaming SSE event conversion
- Model name mapping
- Tool/function conversion

## Components

### Types (`types.go`)
- `OpenAIRequest`, `OpenAIResponse`, `OpenAIMessage` - OpenAI API types
- `ClaudeRequest`, `ClaudeResponse`, `ClaudeMessage` - Claude API types
- `RequestConverter`, `ResponseConverter` - Converter interfaces
- `ConverterConfig` - Configuration options

### OpenAI to Claude (`openai_to_claude.go`)
- `OpenAIToClaudeConverter` - Implements OpenAI → Claude conversion
- Converts request messages, tools, and parameters
- Converts response content and stop reasons
- Handles streaming events

### Claude to OpenAI (`claude_to_openai.go`)
- `ClaudeToOpenAIConverter` - Implements Claude → OpenAI conversion
- Converts request messages, tools, and system prompts
- Converts response content and finish reasons
- Handles streaming events

## Usage

```go
// OpenAI to Claude
converter := converter.NewOpenAIToClaudeConverter(nil)

// Convert request
claudeReq, err := converter.ConvertRequest(openaiReq)

// Convert response
openaiResp, err := converter.ConvertResponse(claudeResp)

// Claude to OpenAI
converter := converter.NewClaudeToOpenAIConverter(nil)

// Convert request
openaiReq, err := converter.ConvertRequest(claudeReq)

// Convert response
claudeResp, err := converter.ConvertResponse(openaiResp)
```

## Test Coverage

Current test coverage: 85.6%

Tests include:
- Request conversion (messages, tools, parameters)
- Response conversion (content, stop reasons, usage)
- Streaming event conversion
- Edge cases (empty messages, nil inputs, additional fields)
- Model name mapping
- Error handling

## Features

### Supported Conversions

**Messages:**
- System, user, assistant roles
- System prompt extraction/injection
- Tool result content

**Tools:**
- OpenAI tools → Claude tools
- Legacy functions → Claude tools
- Tool schema conversion

**Parameters:**
- temperature
- max_tokens
- stream
- Additional fields (preserved)

**Stop Reasons:**
- end_turn ↔ stop
- max_tokens ↔ length
- tool_use ↔ tool_calls
- stop_sequence ↔ stop

### Model Mapping

Default mappings:
- gpt-4 → claude-3-opus-20240229
- gpt-4-turbo → claude-3-sonnet-20240229
- gpt-3.5-turbo → claude-3-haiku-20240307

Unknown models are preserved as-is.

## Design Notes

1. **Interface Compliance**: Both converters implement the converter interfaces defined in types.go
2. **Error Handling**: Nil inputs return explicit errors
3. **Extensibility**: AdditionalFields preserved for future compatibility
4. **Streaming**: Full support for SSE event conversion
5. **Race Safety**: All operations are race-safe (verified with -race flag)

## Future Enhancements

- Custom model mapping configuration
- More sophisticated tool call conversion
- Image/multimodal content support
- Performance optimization with object pooling
