package app

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"ai-model-gateway/internal/observability"
	"ai-model-gateway/internal/core"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type gatewayRoute struct {
	Method        string
	Pattern       string
	ModelRequired bool
	SkipRewrite   bool
}

var gatewayRoutes = []gatewayRoute{
	{Method: http.MethodPost, Pattern: "/chat/completions", ModelRequired: true},
	{Method: http.MethodPost, Pattern: "/completions", ModelRequired: true},
	{Method: http.MethodPost, Pattern: "/embeddings", ModelRequired: true},
	{Method: http.MethodPost, Pattern: "/messages", ModelRequired: true},
	{Method: http.MethodPost, Pattern: "/messages/count_tokens", ModelRequired: true},
	{Method: http.MethodPost, Pattern: "/responses", ModelRequired: true},
	{Method: http.MethodPost, Pattern: "/responses/compact", ModelRequired: true, SkipRewrite: true},
	{Method: http.MethodGet, Pattern: "/responses/{response_id}", ModelRequired: false},
	{Method: http.MethodDelete, Pattern: "/responses/{response_id}", ModelRequired: false},
	{Method: http.MethodPost, Pattern: "/moderations", ModelRequired: false},
	{Method: http.MethodPost, Pattern: "/images/generations", ModelRequired: false},
	{Method: http.MethodPost, Pattern: "/images/edits", ModelRequired: false},
	{Method: http.MethodPost, Pattern: "/images/variations", ModelRequired: false},
	{Method: http.MethodPost, Pattern: "/audio/speech", ModelRequired: true},
	{Method: http.MethodPost, Pattern: "/audio/transcriptions", ModelRequired: true},
	{Method: http.MethodPost, Pattern: "/audio/translations", ModelRequired: true},
	{Method: http.MethodGet, Pattern: "/files", ModelRequired: false},
	{Method: http.MethodPost, Pattern: "/files", ModelRequired: false},
	{Method: http.MethodGet, Pattern: "/files/{file_id}", ModelRequired: false},
	{Method: http.MethodDelete, Pattern: "/files/{file_id}", ModelRequired: false},
	{Method: http.MethodGet, Pattern: "/files/{file_id}/content", ModelRequired: false},
}

// MountGatewayRoutes registers the /v1/* proxy routes on the given router.
func MountGatewayRoutes(r chi.Router, pl core.Pipeline, sel core.RouteSelector) {
	r.Route("/v1", func(v1 chi.Router) {
		v1.Get("/models", modelsHandler(sel))
		for _, route := range gatewayRoutes {
			route := route
			v1.MethodFunc(route.Method, route.Pattern, pipelineHandler(pl, route.ModelRequired, route.SkipRewrite))
		}
	})
}

// modelsHandler returns the list of routable models.
func modelsHandler(sel core.RouteSelector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		models := sel.ListModels()
		sort.Strings(models)
		type modelItem struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
			OwnedBy string `json:"owned_by"`
		}
		items := make([]modelItem, len(models))
		createdAt := time.Now().Unix()
		for i, m := range models {
			items[i] = modelItem{ID: m, Object: "model", Created: createdAt, OwnedBy: "ai-model-gateway"}
		}
		w.Header().Set("Content-Type", "application/json")
		enc := NewEncoder(w)
		if enc != nil {
			_ = enc.Encode(map[string]interface{}{
				"object": "list",
				"data":   items,
			})
		}
	}
}

// pipelineHandler wraps the core.Pipeline as an http.HandlerFunc.
func pipelineHandler(pl core.Pipeline, modelRequired bool, skipModelRewrite bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		requestID := sanitizeRequestID(strings.TrimSpace(r.Header.Get(observability.RequestIDHeader)))
		if requestID == "" {
			requestID = uuid.NewString()
		}
		upstreamPath := buildUpstreamPath(r.URL.Path, r.URL.RawQuery)

		// Read request body.
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeRequestReadError(w, requestID, err)
			return
		}

		contentType := r.Header.Get("Content-Type")
		model := extractModel(body, contentType)

		// Build GatewayRequest.
		gwReq := &core.GatewayRequest{
			ID:               requestID,
			Model:            model,
			ModelRequired:    modelRequired,
			SkipModelRewrite: skipModelRewrite,
			Stream:           extractStream(body, contentType),
			Method:           r.Method,
			Path:             r.URL.Path,
			UpstreamPath:     upstreamPath,
			Headers:          r.Header.Clone(),
			Body:             body,
			UserAgent:        r.UserAgent(),
			Ctx:              ctx,
			StickyKey:        extractStickyRoutingKey(r.URL.Path, body, contentType),
		}
		gwReq.Headers.Set(observability.RequestIDHeader, requestID)

		// Remove hop-by-hop and auth headers before forwarding.
		gwReq.Headers.Del("Host")
		gwReq.Headers.Del("Authorization")
		gwReq.Headers.Del("Proxy-Authorization")
		gwReq.Headers.Del("Connection")
		gwReq.Headers.Del("Keep-Alive")
		gwReq.Headers.Del("Proxy-Connection")
		gwReq.Headers.Del("TE")
		gwReq.Headers.Del("Trailer")
		gwReq.Headers.Del("Transfer-Encoding")
		gwReq.Headers.Del("Upgrade")
		gwReq.Headers.Del("Cookie")
		gwReq.Headers.Del("Set-Cookie")

		// Execute pipeline.
		resp, pErr := pl.Handle(ctx, gwReq)
		if pErr != nil {
			writeGatewayError(w, requestID, statusForPipelineError(pErr), pipelineErrorType(pErr), pErr.Error())
			return
		}

		// Write response headers.
		for k, vs := range resp.Headers {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		setGatewayObservabilityHeaders(w.Header(), gwReq, resp)

		// Stream response.
		if resp.Stream && resp.BodyReader != nil {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.WriteHeader(resp.StatusCode)
			flusher, _ := w.(http.Flusher)
			buf := GetBodyBuffer()
			for {
				n, readErr := resp.BodyReader.Read(buf)
				if n > 0 {
					w.Write(buf[:n])
					if flusher != nil {
						flusher.Flush()
					}
				}
				if readErr != nil {
					break
				}
			}
			PutBodyBuffer(buf)
			resp.BodyReader.Close()
			return
		}

		// Non-streaming response.
		w.WriteHeader(resp.StatusCode)
		if len(resp.Body) > 0 {
			w.Write(resp.Body)
		}
	}
}

func statusForPipelineError(err error) int {
	switch {
	case errors.Is(err, core.ErrModelNotFound):
		return http.StatusBadRequest
	case errors.Is(err, core.ErrRequestTooLarge):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, core.ErrUnauthorized):
		return http.StatusUnauthorized
	case errors.Is(err, core.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, core.ErrNoProvider), errors.Is(err, core.ErrRetryExhausted):
		return http.StatusServiceUnavailable
	case errors.Is(err, core.ErrUpstreamTimeout):
		return http.StatusGatewayTimeout
	default:
		return http.StatusBadGateway
	}
}

func pipelineErrorType(err error) string {
	switch {
	case errors.Is(err, core.ErrRequestTooLarge), errors.Is(err, core.ErrModelNotFound):
		return "invalid_request_error"
	case errors.Is(err, core.ErrUnauthorized):
		return "authentication_error"
	case errors.Is(err, core.ErrForbidden):
		return "permission_error"
	default:
		return "gateway_error"
	}
}

func writeRequestReadError(w http.ResponseWriter, requestID string, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		writeGatewayError(w, requestID, http.StatusRequestEntityTooLarge, "request_too_large", "request body exceeds "+strconv.FormatInt(maxBytesErr.Limit, 10)+" bytes")
		return
	}
	writeGatewayError(w, requestID, http.StatusBadRequest, "invalid_request_error", "read request body: "+err.Error())
}

func writeGatewayError(w http.ResponseWriter, requestID string, status int, errType string, message string) {
	w.Header().Set(observability.RequestIDHeader, requestID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body, _ := JSONMarshal(map[string]any{
		"error": map[string]any{
			"type":    errType,
			"message": message,
		},
	})
	w.Write(body)
}

// extractModel does a best-effort extraction of the "model" field from JSON or multipart bodies.
func extractModel(body []byte, contentType string) string {
	if len(body) == 0 {
		return ""
	}

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = contentType
	}

	switch mediaType {
	case "", "application/json":
		return extractModelFromJSON(body)
	case "multipart/form-data":
		return extractModelFromMultipart(body, params["boundary"])
	default:
		return ""
	}
}

func extractModelFromJSON(body []byte) string {
	var obj struct {
		Model string `json:"model"`
	}
	if JSONUnmarshal(body, &obj) == nil {
		return obj.Model
	}
	return ""
}

func extractModelFromMultipart(body []byte, boundary string) string {
	if boundary == "" {
		return ""
	}

	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return ""
		}
		if err != nil {
			return ""
		}
		if part.FormName() != "model" {
			_, _ = io.Copy(io.Discard, part)
			_ = part.Close()
			continue
		}

		value, err := io.ReadAll(part)
		_ = part.Close()
		if err != nil {
			return ""
		}
		return string(value)
	}
}

// extractStream does a best-effort extraction of the "stream" field from JSON.
func extractStream(body []byte, contentType string) bool {
	if len(body) == 0 {
		return false
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = contentType
	}
	if mediaType != "" && mediaType != "application/json" {
		return false
	}

	var obj struct {
		Stream bool `json:"stream"`
	}
	if JSONUnmarshal(body, &obj) == nil {
		return obj.Stream
	}
	return false
}

func buildUpstreamPath(path string, rawQuery string) string {
	if strings.TrimSpace(rawQuery) == "" {
		return path
	}
	return path + "?" + rawQuery
}

// sanitizeRequestID validates and sanitizes the request ID from headers
func sanitizeRequestID(id string) string {
	if id == "" {
		return ""
	}
	// Limit length to prevent header injection
	if len(id) > 64 {
		id = id[:64]
	}
	// Only allow alphanumeric, hyphens, and underscores
	var result strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func setGatewayObservabilityHeaders(header http.Header, req *core.GatewayRequest, resp *core.GatewayResponse) {
	if header == nil || req == nil {
		return
	}

	if strings.TrimSpace(req.ID) != "" {
		header.Set(observability.RequestIDHeader, req.ID)
	}
	if resp != nil && resp.Provider != nil && strings.TrimSpace(resp.Provider.Name) != "" {
		header.Set(observability.UpstreamHeader, resp.Provider.Name)
	}
	if strings.TrimSpace(req.Model) != "" {
		header.Set(observability.ModelHeader, req.Model)
	}
	if strings.TrimSpace(req.OriginalModel) != "" && req.OriginalModel != req.Model {
		header.Set(observability.RequestedModelHeader, req.OriginalModel)
	}
	if req.Attempt >= 0 {
		header.Set(observability.AttemptsHeader, strconv.Itoa(req.Attempt+1))
	}
}
