package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ControlPlaneClient provides HTTP client for control plane API
type ControlPlaneClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewControlPlaneClient creates a new control plane client
func NewControlPlaneClient(baseURL, token string) *ControlPlaneClient {
	return &ControlPlaneClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetHTTPClient sets a custom HTTP client
func (c *ControlPlaneClient) SetHTTPClient(client *http.Client) {
	c.httpClient = client
}

// ConfigResponse represents config API response
type ConfigResponse struct {
	RevisionID string                 `json:"revision_id"`
	Config     map[string]interface{} `json:"config"`
	CreatedAt  time.Time              `json:"created_at"`
	CreatedBy  string                 `json:"created_by"`
	IsActive   bool                   `json:"is_active"`
}

// ConfigView represents full config view with history
type ConfigView struct {
	Current   ConfigResponse `json:"current"`
	Revisions []RevisionInfo `json:"revisions"`
}

// RevisionInfo represents a config revision
type RevisionInfo struct {
	RevisionID  string    `json:"revision_id"`
	CreatedAt   time.Time `json:"created_at"`
	CreatedBy   string    `json:"created_by"`
	Description string    `json:"description"`
	IsActive    bool      `json:"is_active"`
}

// PublishResult represents publish operation result
type PublishResult struct {
	Success      bool      `json:"success"`
	SnapshotID   string    `json:"snapshot_id"`
	RevisionID   string    `json:"revision_id"`
	PublishedAt  time.Time `json:"published_at"`
	ErrorMessage string    `json:"error,omitempty"`
}

// OverviewResponse represents telemetry overview
type OverviewResponse struct {
	Windows map[string]WindowData `json:"windows"`
}

// WindowData represents time window statistics
type WindowData struct {
	TotalRequests   int64            `json:"total_requests"`
	SuccessRequests int64            `json:"success_requests"`
	FailedRequests  int64            `json:"failed_requests"`
	TotalTokens     int64            `json:"total_tokens"`
	TotalCost       float64          `json:"total_cost"`
	AvgLatencyMs    float64          `json:"avg_latency_ms"`
	ByModel         map[string]int64 `json:"by_model"`
	ByProvider      map[string]int64 `json:"by_provider"`
}

// TelemetryQuery represents telemetry query parameters
type TelemetryQuery struct {
	WindowHours int        `json:"window_hours,omitempty"`
	Limit       int        `json:"limit,omitempty"`
	Offset      int        `json:"offset,omitempty"`
	StartTime   *time.Time `json:"start_time,omitempty"`
	EndTime     *time.Time `json:"end_time,omitempty"`
}

// TelemetryResult represents telemetry query result
type TelemetryResult struct {
	Events []EventRecord `json:"events"`
	Total  int           `json:"total"`
}

// EventRecord represents a telemetry event
type EventRecord struct {
	RequestID    string                 `json:"request_id"`
	Timestamp    time.Time              `json:"timestamp"`
	Model        string                 `json:"model"`
	Provider     string                 `json:"provider"`
	StatusCode   int                    `json:"status_code"`
	LatencyMs    int64                  `json:"latency_ms"`
	InputTokens  int64                  `json:"input_tokens"`
	OutputTokens int64                  `json:"output_tokens"`
	Cost         float64                `json:"cost"`
	Error        string                 `json:"error,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// TimeSeriesQuery represents time series query parameters
type TimeSeriesQuery struct {
	WindowHours   int    `json:"window_hours,omitempty"`
	BucketMinutes int    `json:"bucket_minutes,omitempty"`
	GroupBy       string `json:"group_by,omitempty"`
}

// TimeSeriesResult represents time series query result
type TimeSeriesResult struct {
	Buckets []TimeBucket `json:"buckets"`
}

// TimeBucket represents a time bucket with aggregated values
type TimeBucket struct {
	Timestamp time.Time          `json:"timestamp"`
	Values    map[string]float64 `json:"values"`
}

// BenchmarkQuery represents benchmark query parameters
type BenchmarkQuery struct {
	WindowHours int        `json:"window_hours,omitempty"`
	Models      []string   `json:"models,omitempty"`
	StartTime   *time.Time `json:"start_time,omitempty"`
	EndTime     *time.Time `json:"end_time,omitempty"`
}

// BenchmarkResult represents benchmark query result
type BenchmarkResult struct {
	Models []ModelBenchmark `json:"models"`
}

// ModelBenchmark represents model benchmark data
type ModelBenchmark struct {
	Model           string  `json:"model"`
	TotalRequests   int64   `json:"total_requests"`
	SuccessRate     float64 `json:"success_rate"`
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
	P50LatencyMs    float64 `json:"p50_latency_ms"`
	P95LatencyMs    float64 `json:"p95_latency_ms"`
	P99LatencyMs    float64 `json:"p99_latency_ms"`
	TotalTokens     int64   `json:"total_tokens"`
	AvgInputTokens  float64 `json:"avg_input_tokens"`
	AvgOutputTokens float64 `json:"avg_output_tokens"`
	TotalCost       float64 `json:"total_cost"`
}

// SystemStatus represents system status response
type SystemStatus struct {
	Status          string         `json:"status"`
	Version         string         `json:"version"`
	StartedAt       time.Time      `json:"started_at"`
	Uptime          string         `json:"uptime"`
	GatewayStatus   string         `json:"gateway_status"`
	Gateway         *GatewayStatus `json:"gateway,omitempty"`
	TelemetryStatus string         `json:"telemetry_status"`
}

// GatewayStatus represents gateway status details
type GatewayStatus struct {
	Status           string           `json:"status"`
	ActiveRequests   int64            `json:"active_requests"`
	HealthyUpstreams map[string]bool  `json:"healthy_upstreams"`
	Models           []string         `json:"models"`
}

// ProviderStatus represents provider status
type ProviderStatus struct {
	Name          string   `json:"name"`
	BaseURL       string   `json:"base_url"`
	Status        string   `json:"status"`
	Healthy       bool     `json:"healthy"`
	LatencyMs     int64    `json:"latency_ms"`
	Models        []string `json:"models"`
	TotalRequests int64    `json:"total_requests"`
	ErrorCount    int64    `json:"error_count"`
	LastError     string   `json:"last_error,omitempty"`
}

// GetConfig fetches current config
func (c *ControlPlaneClient) GetConfig(ctx context.Context) (*ConfigView, error) {
	var view ConfigView
	if err := c.doRequest(ctx, http.MethodGet, "/api/admin/config", nil, &view); err != nil {
		return nil, err
	}
	return &view, nil
}

// GetConfigHistory fetches config history
func (c *ControlPlaneClient) GetConfigHistory(ctx context.Context, limit int) ([]RevisionInfo, error) {
	if limit <= 0 {
		limit = 50
	}
	var revisions []RevisionInfo
	path := fmt.Sprintf("/api/admin/config/history?limit=%d", limit)
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &revisions); err != nil {
		return nil, err
	}
	return revisions, nil
}

// PublishConfig publishes a config revision
func (c *ControlPlaneClient) PublishConfig(ctx context.Context, revisionID string) (*PublishResult, error) {
	req := struct{ RevisionID string `json:"revision_id"` }{RevisionID: revisionID}
	var result PublishResult
	if err := c.doRequest(ctx, http.MethodPost, "/api/admin/config/publish", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RollbackConfig rolls back to a config revision
func (c *ControlPlaneClient) RollbackConfig(ctx context.Context, revisionID string) (*PublishResult, error) {
	req := struct{ RevisionID string `json:"revision_id"` }{RevisionID: revisionID}
	var result PublishResult
	if err := c.doRequest(ctx, http.MethodPost, "/api/admin/config/rollback", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetOverview fetches telemetry overview
func (c *ControlPlaneClient) GetOverview(ctx context.Context) (*OverviewResponse, error) {
	var resp OverviewResponse
	if err := c.doRequest(ctx, http.MethodGet, "/api/admin/overview", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetTelemetry fetches telemetry events
func (c *ControlPlaneClient) GetTelemetry(ctx context.Context, query *TelemetryQuery) (*TelemetryResult, error) {
	if query == nil {
		query = &TelemetryQuery{WindowHours: 24, Limit: 100}
	}
	if query.WindowHours <= 0 {
		query.WindowHours = 24
	}
	if query.Limit <= 0 {
		query.Limit = 100
	}
	path := fmt.Sprintf("/api/admin/telemetry?hours=%d&limit=%d", query.WindowHours, query.Limit)
	if query.Offset > 0 {
		path += fmt.Sprintf("&offset=%d", query.Offset)
	}
	var result TelemetryResult
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetTimeSeries fetches time series data
func (c *ControlPlaneClient) GetTimeSeries(ctx context.Context, query *TimeSeriesQuery) (*TimeSeriesResult, error) {
	if query == nil {
		query = &TimeSeriesQuery{WindowHours: 24, BucketMinutes: 5}
	}
	if query.WindowHours <= 0 {
		query.WindowHours = 24
	}
	if query.BucketMinutes <= 0 {
		query.BucketMinutes = 5
	}
	path := fmt.Sprintf("/api/admin/timeseries?hours=%d&bucket=%d", query.WindowHours, query.BucketMinutes)
	if query.GroupBy != "" {
		path += "&group_by=" + url.QueryEscape(query.GroupBy)
	}
	var result TimeSeriesResult
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetBenchmark fetches benchmark data
func (c *ControlPlaneClient) GetBenchmark(ctx context.Context, query *BenchmarkQuery) (*BenchmarkResult, error) {
	if query == nil {
		query = &BenchmarkQuery{WindowHours: 24}
	}
	if query.WindowHours <= 0 {
		query.WindowHours = 24
	}
	path := fmt.Sprintf("/api/admin/benchmark?hours=%d", query.WindowHours)
	if len(query.Models) > 0 {
		path += "&models=" + url.QueryEscape(strings.Join(query.Models, ","))
	}
	var result BenchmarkResult
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetStatus fetches system status
func (c *ControlPlaneClient) GetStatus(ctx context.Context) (*SystemStatus, error) {
	var status SystemStatus
	if err := c.doRequest(ctx, http.MethodGet, "/api/admin/status", nil, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

// ListProviders lists all providers
func (c *ControlPlaneClient) ListProviders(ctx context.Context) ([]ProviderStatus, error) {
	status, err := c.GetStatus(ctx)
	if err != nil {
		return nil, err
	}
	providers := make([]ProviderStatus, 0)
	if status.Gateway != nil {
		for name, healthy := range status.Gateway.HealthyUpstreams {
			ps := ProviderStatus{
				Name:    name,
				Healthy: healthy,
				Status:  "healthy",
			}
			if !healthy {
				ps.Status = "unhealthy"
			}
			providers = append(providers, ps)
		}
	}
	return providers, nil
}

// TestProvider tests a provider connection
func (c *ControlPlaneClient) TestProvider(ctx context.Context, providerName string) (*ProviderStatus, error) {
	status, err := c.GetStatus(ctx)
	if err != nil {
		return nil, err
	}
	providerStatus := &ProviderStatus{
		Name:   providerName,
		Status: "unknown",
	}
	if status.Gateway != nil {
		if healthy, ok := status.Gateway.HealthyUpstreams[providerName]; ok {
			providerStatus.Healthy = healthy
			if healthy {
				providerStatus.Status = "healthy"
			} else {
				providerStatus.Status = "unhealthy"
			}
		}
	}
	return providerStatus, nil
}

// doRequest performs an HTTP request
func (c *ControlPlaneClient) doRequest(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	fullURL := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, fullURL, reqBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errResp struct{ Error string `json:"error"` }
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return fmt.Errorf("API error (%d): %s", resp.StatusCode, errResp.Error)
		}
		return fmt.Errorf("API error (%d): %s", resp.StatusCode, string(respBody))
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("unmarshal response: %w", err)
		}
	}

	return nil
}
