package app

import (
	"encoding/json"
	"mime"
	"sort"
	"strings"

	"ai-model-gateway/internal/core"
)

type stickyRoutingRequest struct {
	PreviousResponseID string `json:"previous_response_id"`
	ResponseID         string `json:"response_id"`
}

func extractStickyRoutingKey(path string, body []byte, contentType string) string {
	if strings.HasPrefix(path, "/v1/responses/") && path != "/v1/responses/compact" {
		return strings.TrimSpace(strings.TrimPrefix(path, "/v1/responses/"))
	}
	if path != "/v1/responses" && path != "/v1/responses/compact" {
		return ""
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = contentType
	}
	if mediaType != "" && mediaType != "application/json" {
		return ""
	}

	var payload stickyRoutingRequest
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}

	for _, value := range []string{payload.PreviousResponseID, payload.ResponseID} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func rememberStickyRouting(sel core.RouteSelector, path string, stickyKey string, provider string, resp *core.GatewayResponse) {
	if provider == "" {
		return
	}
	if stickyKey != "" {
		sel.RememberSticky(stickyKey, provider)
	}
	if resp == nil || len(resp.Body) == 0 || !strings.HasPrefix(path, "/v1/responses") {
		return
	}
	for _, responseID := range extractResponseIDs(resp.Body) {
		sel.RememberSticky(responseID, provider)
	}
}

func extractResponseIDs(body []byte) []string {
	ids := make(map[string]struct{})
	collectResponseIDs(ids, body)

	result := make([]string, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func collectResponseIDs(ids map[string]struct{}, body []byte) {
	collectResponseIDFromJSON(ids, body)

	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || strings.EqualFold(data, "[DONE]") {
			continue
		}
		collectResponseIDFromJSON(ids, []byte(data))
	}
}

func collectResponseIDFromJSON(ids map[string]struct{}, body []byte) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return
	}
	rememberResponseID(ids, payload["id"])
	if response, ok := payload["response"].(map[string]any); ok {
		rememberResponseID(ids, response["id"])
	}
}

func rememberResponseID(ids map[string]struct{}, value any) {
	id, ok := value.(string)
	if !ok {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	ids[id] = struct{}{}
}
