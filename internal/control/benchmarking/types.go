package benchmarking

import "time"

const (
	BaselineKindPublicStandard = "public_standard"
	BaselineKindVendorClaim    = "vendor_claim"

	RunStatusQueued     = "queued"
	RunStatusRunning    = "running"
	RunStatusCompleted  = "completed"
	RunStatusIncomplete = "incomplete"
	RunStatusFailed     = "failed"
	RunStatusCancelled  = "cancelled"

	TargetStatusQueued     = "queued"
	TargetStatusRunning    = "running"
	TargetStatusCompleted  = "completed"
	TargetStatusIncomplete = "incomplete"
	TargetStatusFailed     = "failed"

	VerdictNormal            = "normal"
	VerdictSuspect           = "suspect"
	VerdictHighSuspect       = "highly_suspect"
	VerdictIncomplete        = "incomplete"
	ProtocolOpenAIChat       = "openai_chat_completions"
	ProtocolAnthropicMessage = "anthropic_messages"
)

// BaselineSnapshot stores one immutable imported benchmark snapshot.
type BaselineSnapshot struct {
	SnapshotID string    `json:"snapshot_id"`
	Kind       string    `json:"kind"`
	SourceName string    `json:"source_name"`
	SourceURL  string    `json:"source_url,omitempty"`
	CapturedAt time.Time `json:"captured_at"`
	ImportedAt time.Time `json:"imported_at"`
	RowCount   int       `json:"row_count"`
}

// BaselineMetricRow stores one imported metric row.
type BaselineMetricRow struct {
	SnapshotID       string                 `json:"snapshot_id,omitempty"`
	CanonicalModelID string                 `json:"canonical_model_id"`
	SourceModelName  string                 `json:"source_model_name,omitempty"`
	Family           string                 `json:"family,omitempty"`
	MetricName       string                 `json:"metric_name"`
	Score            float64                `json:"score"`
	ScaleMax         float64                `json:"scale_max"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

// ImportBaselineRequest imports a JSON or CSV baseline file.
type ImportBaselineRequest struct {
	Kind       string     `json:"kind"`
	SourceName string     `json:"source_name"`
	SourceURL  string     `json:"source_url,omitempty"`
	CapturedAt *time.Time `json:"captured_at,omitempty"`
	FileName   string     `json:"file_name"`
	Contents   string     `json:"contents"`
}

// StartRunRequest describes a verification benchmark run request.
type StartRunRequest struct {
	ProviderID       string `json:"provider_id,omitempty"`
	PublicModel      string `json:"public_model,omitempty"`
	Protocol         string `json:"protocol,omitempty"`
	AllActive        bool   `json:"all_active,omitempty"`
	Suite            string `json:"suite,omitempty"`
	PublicSnapshotID string `json:"public_snapshot_id,omitempty"`
	VendorSnapshotID string `json:"vendor_snapshot_id,omitempty"`
}

// RunSummary contains lightweight benchmark run information.
type RunSummary struct {
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

// RunDetail contains a full run report.
type RunDetail struct {
	RunSummary
	Targets []RunTargetDetail `json:"targets"`
}

// RunTargetDetail contains one provider/model verification result.
type RunTargetDetail struct {
	TargetID                 string             `json:"target_id"`
	RunID                    string             `json:"run_id"`
	Status                   string             `json:"status"`
	ProviderID               string             `json:"provider_id"`
	PublicModel              string             `json:"public_model"`
	EffectiveModel           string             `json:"effective_model,omitempty"`
	CanonicalModelID         string             `json:"canonical_model_id,omitempty"`
	Protocol                 string             `json:"protocol"`
	ProtocolAdapter          string             `json:"protocol_adapter,omitempty"`
	SuiteVersion             string             `json:"suite_version"`
	JudgeModel               string             `json:"judge_model,omitempty"`
	PublicSnapshotID         string             `json:"public_snapshot_id,omitempty"`
	VendorSnapshotID         string             `json:"vendor_snapshot_id,omitempty"`
	Verdict                  string             `json:"verdict,omitempty"`
	SuspicionScore           float64            `json:"suspicion_score,omitempty"`
	PublicGap                float64            `json:"public_gap,omitempty"`
	VendorGap                float64            `json:"vendor_gap,omitempty"`
	CompletionRate           float64            `json:"completion_rate,omitempty"`
	CriticalProtocolFailures int                `json:"critical_protocol_failures,omitempty"`
	ReasonCodes              []string           `json:"reason_codes,omitempty"`
	DimensionScores          map[string]float64 `json:"dimension_scores,omitempty"`
	PromptTokens             int64              `json:"prompt_tokens,omitempty"`
	CachedPromptTokens       int64              `json:"cached_prompt_tokens,omitempty"`
	CompletionTokens         int64              `json:"completion_tokens,omitempty"`
	EstimatedCostUSD         float64            `json:"estimated_cost_usd,omitempty"`
	Cases                    []RunCaseResult    `json:"cases,omitempty"`
	StartedAt                time.Time          `json:"started_at"`
	CompletedAt              time.Time          `json:"completed_at,omitempty"`
	Error                    string             `json:"error,omitempty"`
}

// RunCaseResult contains one executed benchmark case result.
type RunCaseResult struct {
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
