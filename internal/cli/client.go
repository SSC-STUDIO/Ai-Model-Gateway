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

// BaseURL returns the configured control-plane base URL.
func (c *ControlPlaneClient) BaseURL() string {
	return c.baseURL
}

// SetHTTPClient sets a custom HTTP client
func (c *ControlPlaneClient) SetHTTPClient(client *http.Client) {
	c.httpClient = client
}

// ConfigResponse represents config API response (matches server CurrentConfigView)
type ConfigResponse struct {
	Revision *RevisionInfo   `json:"revision"`
	Policy   PublisherPolicy `json:"policy"`
}

// RevisionInfo represents a config revision
type RevisionInfo struct {
	RevisionID  string                 `json:"revision_id"`
	CreatedAt   time.Time              `json:"created_at"`
	CreatedBy   string                 `json:"created_by"`
	Description string                 `json:"description"`
	IsActive    bool                   `json:"is_active"`
	Config      map[string]interface{} `json:"config,omitempty"`
}

// PublisherPolicy represents the publisher policy
type PublisherPolicy struct {
	AutoPublish bool `json:"auto_publish"`
}

// ConfigView represents full config view with history (for backward compatibility)
type ConfigView struct {
	Current   ConfigResponse `json:"current"`
	Revisions []RevisionInfo `json:"revisions"`
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

type VerificationRunTelemetryQuery struct {
	WindowHours int      `json:"window_hours,omitempty"`
	Limit       int      `json:"limit,omitempty"`
	Offset      int      `json:"offset,omitempty"`
	Models      []string `json:"models,omitempty"`
	Providers   []string `json:"providers,omitempty"`
	TargetID    string   `json:"target_id,omitempty"`
	CaseID      string   `json:"case_id,omitempty"`
}

// TelemetryResult represents telemetry query result
type TelemetryResult struct {
	Events []EventRecord `json:"events"`
	Total  int           `json:"total"`
}

// EventRecord represents a telemetry event
type EventRecord struct {
	EventID            string                 `json:"event_id,omitempty"`
	RequestID          string                 `json:"request_id"`
	Timestamp          time.Time              `json:"timestamp"`
	Path               string                 `json:"path,omitempty"`
	RequestedModel     string                 `json:"requested_model,omitempty"`
	EffectiveModel     string                 `json:"effective_model,omitempty"`
	Model              string                 `json:"model,omitempty"`
	Provider           string                 `json:"provider"`
	RouteMode          string                 `json:"route_mode,omitempty"`
	StatusCode         int                    `json:"status_code"`
	LatencyMs          int64                  `json:"latency_ms"`
	InputTokens        int64                  `json:"input_tokens"`
	CachedPromptTokens int64                  `json:"cached_prompt_tokens,omitempty"`
	OutputTokens       int64                  `json:"output_tokens"`
	PricingStatus      string                 `json:"pricing_status,omitempty"`
	TotalCostUSD       float64                `json:"pricing_total_cost_usd,omitempty"`
	SyntheticKind      string                 `json:"synthetic_kind,omitempty"`
	BenchmarkRunID     string                 `json:"benchmark_run_id,omitempty"`
	BenchmarkTargetID  string                 `json:"benchmark_target_id,omitempty"`
	BenchmarkCaseID    string                 `json:"benchmark_case_id,omitempty"`
	Cost               float64                `json:"cost"`
	Error              string                 `json:"error,omitempty"`
	Metadata           map[string]interface{} `json:"metadata,omitempty"`
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

type VerificationBaselineSnapshot struct {
	SnapshotID string    `json:"snapshot_id"`
	Kind       string    `json:"kind"`
	SourceName string    `json:"source_name"`
	SourceURL  string    `json:"source_url,omitempty"`
	CapturedAt time.Time `json:"captured_at"`
	ImportedAt time.Time `json:"imported_at"`
	RowCount   int       `json:"row_count"`
}

type VerificationBaselineList struct {
	Snapshots []VerificationBaselineSnapshot `json:"snapshots"`
}

type VerificationBaselineImportRequest struct {
	Kind       string     `json:"kind"`
	SourceName string     `json:"source_name"`
	SourceURL  string     `json:"source_url,omitempty"`
	CapturedAt *time.Time `json:"captured_at,omitempty"`
	FileName   string     `json:"file_name"`
	Contents   string     `json:"contents"`
}

type VerificationRunRequest struct {
	ProviderID       string `json:"provider_id,omitempty"`
	PublicModel      string `json:"public_model,omitempty"`
	Protocol         string `json:"protocol,omitempty"`
	AllActive        bool   `json:"all_active,omitempty"`
	Suite            string `json:"suite,omitempty"`
	PublicSnapshotID string `json:"public_snapshot_id,omitempty"`
	VendorSnapshotID string `json:"vendor_snapshot_id,omitempty"`
}

type VerificationRunSummary struct {
	RunID            string    `json:"run_id"`
	Status           string    `json:"status"`
	SuiteVersion     string    `json:"suite_version"`
	Protocol         string    `json:"protocol"`
	PublicSnapshotID string    `json:"public_snapshot_id,omitempty"`
	VendorSnapshotID string    `json:"vendor_snapshot_id,omitempty"`
	StartedAt        time.Time `json:"started_at"`
	CompletedAt      time.Time `json:"completed_at,omitempty"`
	TargetCount      int       `json:"target_count"`
	CompletedTargets int       `json:"completed_targets"`
	Error            string    `json:"error,omitempty"`
}

type VerificationRunList struct {
	Runs []VerificationRunSummary `json:"runs"`
}

type VerificationRunCase struct {
	CaseID             string                 `json:"case_id"`
	Dimension          string                 `json:"dimension"`
	Kind               string                 `json:"kind"`
	Critical           bool                   `json:"critical"`
	Completed          bool                   `json:"completed"`
	Success            bool                   `json:"success"`
	Score              float64                `json:"score"`
	Reason             string                 `json:"reason,omitempty"`
	StatusCode         int                    `json:"status_code,omitempty"`
	LatencyMs          int64                  `json:"latency_ms,omitempty"`
	PromptTokens       int64                  `json:"prompt_tokens,omitempty"`
	CachedPromptTokens int64                  `json:"cached_prompt_tokens,omitempty"`
	CompletionTokens   int64                  `json:"completion_tokens,omitempty"`
	CostUSD            float64                `json:"cost_usd,omitempty"`
	ProviderID         string                 `json:"provider_id,omitempty"`
	EffectiveModel     string                 `json:"effective_model,omitempty"`
	RouteMode          string                 `json:"route_mode,omitempty"`
	ResponseExcerpt    string                 `json:"response_excerpt,omitempty"`
	Error              string                 `json:"error,omitempty"`
	Meta               map[string]interface{} `json:"meta,omitempty"`
}

type VerificationRunTarget struct {
	TargetID                 string                `json:"target_id"`
	RunID                    string                `json:"run_id"`
	Status                   string                `json:"status"`
	ProviderID               string                `json:"provider_id"`
	PublicModel              string                `json:"public_model"`
	EffectiveModel           string                `json:"effective_model,omitempty"`
	CanonicalModelID         string                `json:"canonical_model_id,omitempty"`
	Protocol                 string                `json:"protocol"`
	ProtocolAdapter          string                `json:"protocol_adapter,omitempty"`
	SuiteVersion             string                `json:"suite_version"`
	JudgeModel               string                `json:"judge_model,omitempty"`
	PublicSnapshotID         string                `json:"public_snapshot_id,omitempty"`
	VendorSnapshotID         string                `json:"vendor_snapshot_id,omitempty"`
	Verdict                  string                `json:"verdict,omitempty"`
	SuspicionScore           float64               `json:"suspicion_score,omitempty"`
	PublicGap                float64               `json:"public_gap,omitempty"`
	VendorGap                float64               `json:"vendor_gap,omitempty"`
	CompletionRate           float64               `json:"completion_rate,omitempty"`
	CriticalProtocolFailures int                   `json:"critical_protocol_failures,omitempty"`
	ReasonCodes              []string              `json:"reason_codes,omitempty"`
	DimensionScores          map[string]float64    `json:"dimension_scores,omitempty"`
	PromptTokens             int64                 `json:"prompt_tokens,omitempty"`
	CachedPromptTokens       int64                 `json:"cached_prompt_tokens,omitempty"`
	CompletionTokens         int64                 `json:"completion_tokens,omitempty"`
	EstimatedCostUSD         float64               `json:"estimated_cost_usd,omitempty"`
	Cases                    []VerificationRunCase `json:"cases,omitempty"`
	StartedAt                time.Time             `json:"started_at"`
	CompletedAt              time.Time             `json:"completed_at,omitempty"`
	Error                    string                `json:"error,omitempty"`
}

type VerificationRunDetail struct {
	VerificationRunSummary
	Targets []VerificationRunTarget `json:"targets"`
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

// SystemStatus represents system status response (matches server response)
type SystemStatus struct {
	Version         string                 `json:"version"`
	StartedAt       string                 `json:"startedAt"`
	Uptime          string                 `json:"uptime"`
	GatewayStatus   string                 `json:"gateway_status"`
	Gateway         *GatewayStatusResponse `json:"gateway,omitempty"`
	GatewayError    string                 `json:"gateway_error,omitempty"`
	TelemetryStatus string                 `json:"telemetry_status"`
}

// GatewayStatusResponse matches gatewaycontrol.GetStatusResponse
type GatewayStatusResponse struct {
	ActiveSnapshotID string                    `json:"active_snapshot_id"`
	Readiness        string                    `json:"readiness"`
	ActiveRequests   int                       `json:"active_requests"`
	Listener         string                    `json:"listener"`
	ProviderHealth   map[string]ProviderHealth `json:"provider_health"`
	Uptime           string                    `json:"uptime"`
	StartedAt        time.Time                 `json:"started_at"`
}

// ProviderHealth represents provider health status
type ProviderHealth struct {
	Name                string    `json:"name"`
	Healthy             bool      `json:"healthy"`
	LastCheck           time.Time `json:"last_check"`
	LastSuccess         time.Time `json:"last_success"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	CooldownUntil       time.Time `json:"cooldown_until"`
	LatencyMs           int64     `json:"latency_ms"`
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

// GetRuntimeStatus fetches v1.3 runtime status.
func (c *ControlPlaneClient) GetRuntimeStatus(ctx context.Context) (map[string]interface{}, error) {
	var resp map[string]interface{}
	if err := c.doRequest(ctx, http.MethodGet, "/api/admin/runtime/status", nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// RuntimePreflight runs v1.3 runtime preflight checks.
func (c *ControlPlaneClient) RuntimePreflight(ctx context.Context) (map[string]interface{}, error) {
	var resp map[string]interface{}
	if err := c.doRequest(ctx, http.MethodPost, "/api/admin/runtime/preflight", map[string]interface{}{}, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// ListAudit fetches recent audit events.
func (c *ControlPlaneClient) ListAudit(ctx context.Context, limit int) (map[string]interface{}, error) {
	if limit <= 0 {
		limit = 100
	}
	var resp map[string]interface{}
	if err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/api/admin/audit?limit=%d", limit), nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ControlPlaneClient) ConfigPreview(ctx context.Context, req map[string]interface{}) (map[string]interface{}, error) {
	var resp map[string]interface{}
	if err := c.doRequest(ctx, http.MethodPost, "/api/admin/config/preview", req, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ControlPlaneClient) ConfigDiff(ctx context.Context, req map[string]interface{}) (map[string]interface{}, error) {
	var resp map[string]interface{}
	if err := c.doRequest(ctx, http.MethodPost, "/api/admin/config/diff", req, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ControlPlaneClient) ProbeProvider(ctx context.Context, req map[string]interface{}) (map[string]interface{}, error) {
	var resp map[string]interface{}
	if err := c.doRequest(ctx, http.MethodPost, "/api/admin/probe/provider", req, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ControlPlaneClient) ProbeModel(ctx context.Context, req map[string]interface{}) (map[string]interface{}, error) {
	var resp map[string]interface{}
	if err := c.doRequest(ctx, http.MethodPost, "/api/admin/probe/model", req, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ControlPlaneClient) Replay(ctx context.Context, requestID string) (map[string]interface{}, error) {
	var resp map[string]interface{}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		if err := c.doRequest(ctx, http.MethodGet, "/api/admin/replay", nil, &resp); err != nil {
			return nil, err
		}
		return resp, nil
	}
	if err := c.doRequest(ctx, http.MethodPost, "/api/admin/replay", map[string]interface{}{"request_id": requestID}, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ControlPlaneClient) Diagnostics(ctx context.Context) (map[string]interface{}, error) {
	var resp map[string]interface{}
	if err := c.doRequest(ctx, http.MethodGet, "/api/admin/diagnostics", nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *ControlPlaneClient) SecretsStatus(ctx context.Context) (map[string]interface{}, error) {
	var resp map[string]interface{}
	if err := c.doRequest(ctx, http.MethodGet, "/api/admin/secrets/status", nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// GetConfig fetches current config
func (c *ControlPlaneClient) GetConfig(ctx context.Context) (*ConfigResponse, error) {
	var resp ConfigResponse
	if err := c.doRequest(ctx, http.MethodGet, "/api/admin/config", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
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
	req := struct {
		RevisionID string `json:"revision_id"`
	}{RevisionID: revisionID}
	var result PublishResult
	if err := c.doRequest(ctx, http.MethodPost, "/api/admin/config/publish", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ReloadConfig reloads the authoring config source and republishes the resulting revision.
func (c *ControlPlaneClient) ReloadConfig(ctx context.Context) (*PublishResult, error) {
	var result PublishResult
	if err := c.doRequest(ctx, http.MethodPost, "/api/admin/config/reload", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RollbackConfig rolls back to a config revision
func (c *ControlPlaneClient) RollbackConfig(ctx context.Context, revisionID string) (*PublishResult, error) {
	req := struct {
		RevisionID string `json:"revision_id"`
	}{RevisionID: revisionID}
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

func (c *ControlPlaneClient) ListVerificationBaselines(ctx context.Context) (*VerificationBaselineList, error) {
	var result VerificationBaselineList
	if err := c.doRequest(ctx, http.MethodGet, "/api/admin/benchmark/baselines", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *ControlPlaneClient) ImportVerificationBaseline(ctx context.Context, req *VerificationBaselineImportRequest) (*VerificationBaselineSnapshot, error) {
	if req == nil {
		return nil, fmt.Errorf("verification baseline import request is required")
	}
	var result VerificationBaselineSnapshot
	if err := c.doRequest(ctx, http.MethodPost, "/api/admin/benchmark/baselines/import", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *ControlPlaneClient) ListVerificationRuns(ctx context.Context, limit int) (*VerificationRunList, error) {
	if limit <= 0 {
		limit = 50
	}
	var result VerificationRunList
	if err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/api/admin/benchmark/runs?limit=%d", limit), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *ControlPlaneClient) StartVerificationRun(ctx context.Context, req *VerificationRunRequest) (*VerificationRunDetail, error) {
	if req == nil {
		return nil, fmt.Errorf("verification benchmark run request is required")
	}
	var result VerificationRunDetail
	if err := c.doRequest(ctx, http.MethodPost, "/api/admin/benchmark/runs", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *ControlPlaneClient) GetVerificationRun(ctx context.Context, runID string) (*VerificationRunDetail, error) {
	var result VerificationRunDetail
	if err := c.doRequest(ctx, http.MethodGet, "/api/admin/benchmark/runs/"+url.PathEscape(strings.TrimSpace(runID)), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *ControlPlaneClient) GetVerificationRunTelemetry(ctx context.Context, runID string, query *VerificationRunTelemetryQuery) (*TelemetryResult, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("verification benchmark run id is required")
	}
	if query == nil {
		query = &VerificationRunTelemetryQuery{WindowHours: 24, Limit: 200}
	}
	if query.WindowHours <= 0 {
		query.WindowHours = 24
	}
	if query.Limit <= 0 {
		query.Limit = 200
	}

	params := url.Values{}
	params.Set("hours", fmt.Sprintf("%d", query.WindowHours))
	params.Set("limit", fmt.Sprintf("%d", query.Limit))
	if query.Offset > 0 {
		params.Set("offset", fmt.Sprintf("%d", query.Offset))
	}
	if models := cleanQueryStrings(query.Models); len(models) > 0 {
		params.Set("models", strings.Join(models, ","))
	}
	if providers := cleanQueryStrings(query.Providers); len(providers) > 0 {
		params.Set("providers", strings.Join(providers, ","))
	}
	if targetID := strings.TrimSpace(query.TargetID); targetID != "" {
		params.Set("target_id", targetID)
	}
	if caseID := strings.TrimSpace(query.CaseID); caseID != "" {
		params.Set("case_id", caseID)
	}

	var result TelemetryResult
	path := fmt.Sprintf("/api/admin/benchmark/runs/%s/telemetry?%s", url.PathEscape(runID), params.Encode())
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

// ListProviders lists all providers from gateway status
func (c *ControlPlaneClient) ListProviders(ctx context.Context) ([]ProviderStatus, error) {
	status, err := c.GetStatus(ctx)
	if err != nil {
		return nil, err
	}
	providers := make([]ProviderStatus, 0)
	if status.Gateway != nil {
		for name, health := range status.Gateway.ProviderHealth {
			ps := ProviderStatus{
				Name:      name,
				Healthy:   health.Healthy,
				LatencyMs: health.LatencyMs,
			}
			if health.Healthy {
				ps.Status = "healthy"
			} else {
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
		if health, ok := status.Gateway.ProviderHealth[providerName]; ok {
			providerStatus.Healthy = health.Healthy
			providerStatus.LatencyMs = health.LatencyMs
			if health.Healthy {
				providerStatus.Status = "healthy"
			} else {
				providerStatus.Status = "unhealthy"
				if health.ConsecutiveFailures > 0 {
					providerStatus.ErrorCount = int64(health.ConsecutiveFailures)
				}
			}
		}
	}
	return providerStatus, nil
}

func cleanQueryStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
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
		var errResp struct {
			Error string `json:"error"`
		}
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
