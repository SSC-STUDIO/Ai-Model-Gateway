package api

import (
	"encoding/json"
	"strings"

	"ai-model-gateway/internal/gateway/snapshot"
)

// resolveUpstreamModel resolves the upstream model name from the public model name.
func resolveUpstreamModel(provider *snapshot.ProviderSnapshot, publicModel string) string {
	for _, m := range provider.ModelTable {
		if m.PublicModel == publicModel {
			return m.UpstreamModel
		}
	}
	return publicModel
}

// rewriteModelInBody replaces the model name in the JSON body using proper parsing.
func rewriteModelInBody(body []byte, oldModel, newModel string) []byte {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		// Fallback: if body isn't valid JSON, return as-is
		return body
	}

	// Only rewrite if the model field matches exactly
	if modelRaw, ok := raw["model"]; ok {
		var currentModel string
		if err := json.Unmarshal(modelRaw, &currentModel); err == nil && currentModel == oldModel {
			raw["model"], _ = json.Marshal(newModel)
			result, err := json.Marshal(raw)
			if err != nil {
				return body
			}
			return result
		}
	}

	return body
}

// extractUsage extracts token usage from the response body.
func extractUsage(respBody []byte) (promptTokens, cachedPromptTokens, completionTokens int64) {
	if len(respBody) == 0 {
		return 0, 0, 0
	}

	var payload struct {
		Usage struct {
			PromptTokens             int64 `json:"prompt_tokens"`
			CompletionTokens         int64 `json:"completion_tokens"`
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			PromptDetails            struct {
				CachedTokens int64 `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			InputDetails struct {
				CachedTokens int64 `json:"cached_tokens"`
			} `json:"input_tokens_details"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(respBody, &payload); err != nil {
		return 0, 0, 0
	}

	promptTokens = payload.Usage.PromptTokens
	if promptTokens == 0 {
		promptTokens = payload.Usage.InputTokens
	}
	if payload.Usage.CacheReadInputTokens > 0 || payload.Usage.CacheCreationInputTokens > 0 {
		promptTokens = payload.Usage.InputTokens + payload.Usage.CacheCreationInputTokens + payload.Usage.CacheReadInputTokens
	}
	completionTokens = payload.Usage.CompletionTokens
	if completionTokens == 0 {
		completionTokens = payload.Usage.OutputTokens
	}
	cachedPromptTokens = payload.Usage.PromptDetails.CachedTokens
	if cachedPromptTokens == 0 {
		cachedPromptTokens = payload.Usage.InputDetails.CachedTokens
	}
	if cachedPromptTokens == 0 {
		cachedPromptTokens = payload.Usage.CacheReadInputTokens
	}
	if promptTokens < cachedPromptTokens {
		promptTokens = cachedPromptTokens
	}

	return promptTokens, cachedPromptTokens, completionTokens
}

func extractUsageFromSSEEvent(data []byte) (promptTokens, cachedPromptTokens, completionTokens int64, ok bool) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "[DONE]" {
		return 0, 0, 0, false
	}
	promptTokens, cachedPromptTokens, completionTokens = extractUsage([]byte(trimmed))
	if promptTokens == 0 && cachedPromptTokens == 0 && completionTokens == 0 {
		var payload struct {
			Message struct {
				Usage anthropicUsage `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
			promptTokens, cachedPromptTokens, completionTokens = payload.Message.Usage.tokenTriplet()
		}
	}
	if promptTokens == 0 && cachedPromptTokens == 0 && completionTokens == 0 {
		return 0, 0, 0, false
	}
	return promptTokens, cachedPromptTokens, completionTokens, true
}

func extractErrorMessage(respBody []byte, forwardErr error) string {
	if forwardErr != nil {
		// #8: strip the upstream provider name prefix so clients see a
		// generic message instead of an internal identifier.
		return sanitizeProviderError(forwardErr.Error())
	}
	if len(respBody) == 0 {
		return ""
	}
	var payload struct {
		Error any `json:"error"`
	}
	if err := json.Unmarshal(respBody, &payload); err == nil {
		switch value := payload.Error.(type) {
		case string:
			if value != "" {
				return value
			}
		case map[string]any:
			if message, ok := value["message"].(string); ok && message != "" {
				return message
			}
		}
	}
	message := strings.TrimSpace(string(respBody))
	if len(message) > 512 {
		message = message[:512]
	}
	return message
}

// sanitizeProviderError strips internal provider identifiers from upstream
// error messages so clients see a generic message instead of internal names.
// If no provider prefix is found the message is returned unchanged.
func sanitizeProviderError(msg string) string {
	if msg == "" {
		return msg
	}
	// Strip common internal framing like "station-a: " or "upstream station-a: "
	if idx := strings.Index(msg, ": "); idx > 0 && idx <= 32 {
		return msg[idx+2:]
	}
	return msg
}
