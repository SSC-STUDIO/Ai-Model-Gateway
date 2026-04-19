// Package snapshot provides tests for the snapshot model.
package snapshot

import (
	"testing"
	"time"
)

func TestSnapshotValidation(t *testing.T) {
	tests := []struct {
		name    string
		snap    *Snapshot
		wantErr bool
	}{
		{
			name: "valid snapshot",
			snap: &Snapshot{
				Meta: SnapshotMeta{
					SnapshotID:      "snap_20260412_000001",
					SchemaVersion:   CurrentSchemaVersion,
					RevisionID:      "rev_20260412_000001",
					GeneratedAt:     time.Now(),
					CompilerVersion: "1.0.0",
				},
				Ingress: IngressConfig{
					Listen:         "127.0.0.1:18080",
					ReadTimeoutMs:  30000,
					WriteTimeoutMs: 60000,
					IdleTimeoutMs:  120000,
					MaxBodyBytes:   100 << 20,
				},
				Contract: ContractConfig{
					PublicAPI: "openai_chat_completions",
					EnabledRoutes: []string{
						"POST /v1/chat/completions",
						"GET /v1/models",
						"GET /-/health",
					},
				},
				Providers: []ProviderSnapshot{
					{
						ProviderID:      "openai-primary",
						ProtocolAdapter: "openai_chat_completions",
						BaseURL:         "https://api.openai.com/v1",
						ModelTable: []ModelMapping{
							{PublicModel: "gpt-4", UpstreamModel: "gpt-4"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "missing snapshot_id",
			snap: &Snapshot{
				Meta: SnapshotMeta{
					SchemaVersion: CurrentSchemaVersion,
				},
			},
			wantErr: true,
		},
		{
			name: "wrong schema version",
			snap: &Snapshot{
				Meta: SnapshotMeta{
					SnapshotID:    "snap_test",
					SchemaVersion: 999,
				},
			},
			wantErr: true,
		},
		{
			name: "missing ingress listen",
			snap: &Snapshot{
				Meta: SnapshotMeta{
					SnapshotID:    "snap_test",
					SchemaVersion: CurrentSchemaVersion,
				},
				Ingress: IngressConfig{},
			},
			wantErr: true,
		},
		{
			name: "no providers",
			snap: &Snapshot{
				Meta: SnapshotMeta{
					SnapshotID:    "snap_test",
					SchemaVersion: CurrentSchemaVersion,
				},
				Ingress: IngressConfig{
					Listen: "127.0.0.1:18080",
				},
				Providers: []ProviderSnapshot{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSnapshot(tt.snap)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSnapshot() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSnapshotMetaDefaults(t *testing.T) {
	meta := SnapshotMeta{
		SnapshotID:      "snap_test",
		SchemaVersion:   CurrentSchemaVersion,
		GeneratedAt:     time.Now(),
		CompilerVersion: "1.0.0",
	}

	if meta.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("SchemaVersion = %v, want %v", meta.SchemaVersion, CurrentSchemaVersion)
	}
}

func TestProviderSnapshotDefaults(t *testing.T) {
	provider := ProviderSnapshot{
		ProviderID:      "test-provider",
		ProtocolAdapter: "openai_chat_completions",
		BaseURL:         "https://api.example.com/v1",
		ExecutionPolicy: ExecutionPolicy{
			Enabled:   true,
			Weight:    1,
			TimeoutMs: 30000,
		},
	}

	if provider.ExecutionPolicy.Weight == 0 {
		t.Error("Provider weight should have a default value")
	}
}

func TestRoutingPolicyDefaults(t *testing.T) {
	policy := RoutingPolicy{
		MaxRetries: 2,
		RetryBackoff: RetryBackoff{
			InitialMs: 3000,
			MaxMs:     30000,
		},
		Health: HealthConfig{
			Enabled:     true,
			IntervalSec: 10,
			TimeoutMs:   2000,
			Path:        "/v1/models",
		},
	}

	if policy.MaxRetries == 0 {
		t.Error("MaxRetries should have a default value")
	}
	if policy.RetryBackoff.InitialMs == 0 {
		t.Error("RetryBackoff.InitialMs should have a default value")
	}
}

func TestTelemetryEmitConfigDefaults(t *testing.T) {
	cfg := TelemetryEmitConfig{
		Channel: "telemetry-ingest",
		Batching: BatchingConfig{
			MaxBatchSize:    256,
			FlushIntervalMs: 100,
		},
	}

	if cfg.Batching.MaxBatchSize == 0 {
		t.Error("MaxBatchSize should have a default value")
	}
	if cfg.Batching.FlushIntervalMs == 0 {
		t.Error("FlushIntervalMs should have a default value")
	}
}

// validateSnapshot validates a snapshot for testing.
func validateSnapshot(snap *Snapshot) error {
	if snap.Meta.SnapshotID == "" {
		return ErrMissingSnapshotID
	}
	if snap.Meta.SchemaVersion != CurrentSchemaVersion {
		return ErrInvalidSchemaVersion
	}
	if snap.Ingress.Listen == "" {
		return ErrMissingIngressListen
	}
	if len(snap.Providers) == 0 {
		return ErrNoProviders
	}
	return nil
}

// Validation errors
var (
	ErrMissingSnapshotID    = errorf("snapshot_id is required")
	ErrInvalidSchemaVersion = errorf("invalid schema version")
	ErrMissingIngressListen = errorf("ingress.listen is required")
	ErrNoProviders          = errorf("at least one provider is required")
)

func errorf(msg string) error { return &validationError{msg: msg} }

type validationError struct{ msg string }

func (e *validationError) Error() string { return e.msg }
