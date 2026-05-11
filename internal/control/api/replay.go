package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ai-model-gateway/internal/infra/logger"
)

// maxReplayResponseBytes caps how many bytes we read from the replayed upstream
// response. Without a cap a hostile or buggy upstream could exhaust admin memory.
const maxReplayResponseBytes = 10 * 1024 * 1024

// replayHTTPClient is a dedicated client for re-executing failed requests. It uses
// a short header timeout and is constrained per-request by context deadlines.
var replayHTTPClient = &http.Client{
	Timeout: 120 * time.Second,
}

// FailedRequest represents a failed request for replay.
type FailedRequest struct {
	RequestID    string    `json:"request_id"`
	EventID      string    `json:"event_id"`
	Model        string    `json:"model"`
	ProviderID   string    `json:"provider_id"`
	Path         string    `json:"path"`
	RequestBody  []byte    `json:"request_body,omitempty"`
	StatusCode   int       `json:"status_code"`
	ErrorMessage string    `json:"error_message"`
	Timestamp    time.Time `json:"timestamp"`
}

// ReplayResult represents the result of a replay operation.
type ReplayResult struct {
	RequestID    string    `json:"request_id"`
	StatusCode   int       `json:"status_code"`
	ResponseBody []byte    `json:"response_body,omitempty"`
	Error        string    `json:"error,omitempty"`
	ReplayedAt   time.Time `json:"replayed_at"`
}

// ReplayHandler handles failed request listing and replay.
type ReplayHandler struct {
	db         *sql.DB
	gatewayURL string
}

// NewReplayHandler creates a new replay handler.
func NewReplayHandler(db *sql.DB, gatewayURL string) *ReplayHandler {
	return &ReplayHandler{
		db:         db,
		gatewayURL: gatewayURL,
	}
}

// ListFailed returns failed requests from the telemetry database.
func (h *ReplayHandler) ListFailed(ctx context.Context, start, end time.Time, limit int) ([]FailedRequest, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	query := `
SELECT
	event_id,
	request_id,
	timestamp,
	path,
	COALESCE(NULLIF(effective_model, ''), NULLIF(requested_model, ''), '') AS model,
	provider_id,
	status_code,
	COALESCE(error_message, '')
FROM request_facts
WHERE status_code >= 400
  AND timestamp >= ?
  AND timestamp <= ?
ORDER BY timestamp DESC
LIMIT ?`

	rows, err := h.db.QueryContext(ctx, query,
		start.UTC().Format(time.RFC3339Nano),
		end.UTC().Format(time.RFC3339Nano),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query failed requests: %w", err)
	}
	defer rows.Close()

	var requests []FailedRequest
	for rows.Next() {
		var (
			req      FailedRequest
			ts       string
			model    sql.NullString
			provider sql.NullString
			path     sql.NullString
			errorMsg sql.NullString
		)
		if err := rows.Scan(
			&req.EventID,
			&req.RequestID,
			&ts,
			&path,
			&model,
			&provider,
			&req.StatusCode,
			&errorMsg,
		); err != nil {
			return nil, fmt.Errorf("scan failed request: %w", err)
		}

		req.Timestamp = parseStoredTimestamp(ts)
		req.Model = model.String
		req.ProviderID = provider.String
		req.Path = path.String
		req.ErrorMessage = errorMsg.String
		requests = append(requests, req)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate failed requests: %w", err)
	}

	return requests, nil
}

// GetFailedRequest retrieves a single failed request by request ID.
func (h *ReplayHandler) GetFailedRequest(ctx context.Context, requestID string) (*FailedRequest, error) {
	query := `
SELECT
	event_id,
	request_id,
	timestamp,
	path,
	COALESCE(NULLIF(effective_model, ''), NULLIF(requested_model, ''), '') AS model,
	provider_id,
	status_code,
	COALESCE(error_message, '')
FROM request_facts
WHERE request_id = ?
  AND status_code >= 400
ORDER BY timestamp DESC
LIMIT 1`

	var (
		req      FailedRequest
		ts       string
		model    sql.NullString
		provider sql.NullString
		path     sql.NullString
		errorMsg sql.NullString
	)

	err := h.db.QueryRowContext(ctx, query, requestID).Scan(
		&req.EventID,
		&req.RequestID,
		&ts,
		&path,
		&model,
		&provider,
		&req.StatusCode,
		&errorMsg,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("failed request not found: %s", requestID)
	}
	if err != nil {
		return nil, fmt.Errorf("query failed request: %w", err)
	}

	req.Timestamp = parseStoredTimestamp(ts)
	req.Model = model.String
	req.ProviderID = provider.String
	req.Path = path.String
	req.ErrorMessage = errorMsg.String

	return &req, nil
}

// Replay re-executes a failed request.
// Note: Full replay requires request body storage which is not currently
// captured in the telemetry projection. This implementation returns
// an error indicating the limitation.
func (h *ReplayHandler) Replay(ctx context.Context, requestID string) (*ReplayResult, error) {
	req, err := h.GetFailedRequest(ctx, requestID)
	if err != nil {
		return nil, err
	}

	// Check if we have the request body
	// Currently the telemetry system does not store request bodies
	// This would require extending the ProjectionFact and schema
	if len(req.RequestBody) == 0 {
		return &ReplayResult{
			RequestID:  requestID,
			ReplayedAt: time.Now().UTC(),
			Error:      "request body not available for replay - telemetry projection does not store request bodies",
		}, nil
	}

	// Build the replay URL via net/url so a tampered stored path such as
	// "//evil.example/x", "/../foo", or one containing newline/query injection
	// cannot redirect the request away from the configured gateway.
	targetURL, err := buildReplayURL(h.gatewayURL, req.Path)
	if err != nil {
		return &ReplayResult{
			RequestID:  requestID,
			ReplayedAt: time.Now().UTC(),
			Error:      "replay target path rejected",
		}, nil
	}

	// Create the replay request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(req.RequestBody))
	if err != nil {
		return nil, fmt.Errorf("create replay request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := replayHTTPClient.Do(httpReq)
	if err != nil {
		logger.Error("replay request failed",
			"request_id", requestID,
			"target", targetURL,
			"error", err.Error(),
		)
		return &ReplayResult{
			RequestID:  requestID,
			ReplayedAt: time.Now().UTC(),
			Error:      "replay request failed",
		}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxReplayResponseBytes))
	if err != nil {
		logger.Error("read replay response failed",
			"request_id", requestID,
			"error", err.Error(),
		)
		return &ReplayResult{
			RequestID:  requestID,
			ReplayedAt: time.Now().UTC(),
			Error:      "read replay response failed",
		}, nil
	}

	return &ReplayResult{
		RequestID:    requestID,
		StatusCode:   resp.StatusCode,
		ResponseBody: body,
		ReplayedAt:   time.Now().UTC(),
	}, nil
}

// buildReplayURL resolves the stored request path against the configured gateway
// base URL in a way that blocks protocol-relative, scheme-injected, and upward
// traversal paths. The returned URL is always anchored to gatewayURL's host.
func buildReplayURL(gatewayURL, storedPath string) (string, error) {
	base, err := url.Parse(strings.TrimSpace(gatewayURL))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", errors.New("invalid gateway URL")
	}

	p := strings.TrimSpace(storedPath)
	if p == "" || !strings.HasPrefix(p, "/") {
		return "", errors.New("stored path must be absolute")
	}
	// Reject protocol-relative ("//host") and any attempt to smuggle a new host.
	if strings.HasPrefix(p, "//") {
		return "", errors.New("stored path must not be protocol-relative")
	}
	// Reject control characters and stray whitespace that could smuggle headers.
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return "", errors.New("stored path contains control characters")
		}
	}

	ref, err := url.Parse(p)
	if err != nil {
		return "", err
	}
	if ref.Scheme != "" || ref.Host != "" {
		return "", errors.New("stored path must not include scheme or host")
	}

	resolved := base.ResolveReference(ref)
	// Guard against ResolveReference producing a host change (belt-and-braces).
	if resolved.Host != base.Host || resolved.Scheme != base.Scheme {
		return "", errors.New("resolved URL escaped gateway origin")
	}
	return resolved.String(), nil
}

// ServeHTTP handles /api/admin/replay endpoints.
//
// Routes are method-based for a clean REST-like surface:
//
//	GET    /api/admin/replay?start=...&end=...  → list failed requests
//	POST   /api/admin/replay                     → replay by request_id
//
// Forwards all other methods to the standard 405 handler.
func (h *ReplayHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleList(w, r)
	case http.MethodPost:
		h.handleReplay(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *ReplayHandler) handleList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse time range
	start := time.Now().Add(-24 * time.Hour)
	end := time.Now()

	if startStr := r.URL.Query().Get("start"); startStr != "" {
		if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			start = t
		}
	}
	if endStr := r.URL.Query().Get("end"); endStr != "" {
		if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			end = t
		}
	}

	// Parse limit
	limit := intQuery(r, "limit", 100)

	// Query failed requests
	requests, err := h.ListFailed(ctx, start, end, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"requests": requests,
		"count":    len(requests),
		"start":    start.UTC().Format(time.RFC3339),
		"end":      end.UTC().Format(time.RFC3339),
	})
}

func (h *ReplayHandler) handleReplay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		RequestID string `json:"request_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.RequestID == "" {
		writeError(w, http.StatusBadRequest, "request_id is required")
		return
	}

	result, err := h.Replay(ctx, req.RequestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	status := http.StatusOK
	if result.Error != "" {
		status = http.StatusOK // Return 200 but with error in body
	}

	writeJSON(w, status, result)
}

// parseStoredTimestamp parses a timestamp string from the database.
func parseStoredTimestamp(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return ts
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts
	}
	return time.Time{}
}
