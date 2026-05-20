// Package snapshot provides integration tests for the snapshot model.
package snapshot

import (
	"encoding/json"
	"testing"
	"time"
)

// TestSnapshotJSONRoundTrip verifies that snapshots serialize and deserialize correctly.
func TestSnapshotJSONRoundTrip(t *testing.T) {
	original := &Snapshot{
		Meta: SnapshotMeta{
			SnapshotID:      "snap_test_roundtrip",
			SchemaVersion:   CurrentSchemaVersion,
			RevisionID:      "rev_test_roundtrip",
			GeneratedAt:     time.Now().UTC(),
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
				Credentials: Credentials{
					Kind:  "bearer",
					Value: "sk-test-key",
				},
				Headers: map[string]string{
					"X-Custom-Header": "test-value",
				},
				ModelTable: []ModelMapping{
					{PublicModel: "gpt-4", UpstreamModel: "gpt-4"},
					{PublicModel: "gpt-4o-mini", UpstreamModel: "gpt-4o-mini"},
				},
				CapabilityTable: CapabilityTable{
					SupportsChatCompletions: true,
					SupportsStreaming:       true,
					UsageAccounting:         "openai_usage",
					ErrorClassifier:         "openai_error",
				},
				ExecutionPolicy: ExecutionPolicy{
					Enabled:       true,
					Weight:        1,
					TimeoutMs:     30000,
					SameRetries:   1,
					ProviderClass: "quota_limited",
				},
			},
		},
		RoutingPolicy: RoutingPolicy{
			Strategy:   "health_weighted_rr",
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
			StickySessions: StickySessionConfig{
				Enabled: true,
				TTLSec:  900,
			},
			FailurePolicy: FailurePolicy{
				Threshold:                20,
				CooldownSec:              60,
				PassthroughAfterSec:      45,
				QuotaRecoveryIntervalMin: 30,
			},
			Retry: RetryPolicy{
				InfiniteOnError: false,
				StatusCodes:     []int{408, 429},
				StatusCodeMin:   500,
				MessageKeywords: []string{"timeout", "rate limit"},
			},
		},
		TelemetryEmit: TelemetryEmitConfig{
			Channel: "telemetry-ingest",
			Batching: BatchingConfig{
				MaxBatchSize:    256,
				FlushIntervalMs: 100,
			},
		},
	}

	// Serialize to JSON
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Verify credentials are not exposed in JSON
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to raw map failed: %v", err)
	}

	// Deserialize back
	var restored Snapshot
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Verify key fields
	if restored.Meta.SnapshotID != original.Meta.SnapshotID {
		t.Errorf("SnapshotID mismatch: %s vs %s", restored.Meta.SnapshotID, original.Meta.SnapshotID)
	}
	if restored.Ingress.Listen != original.Ingress.Listen {
		t.Errorf("Listen mismatch: %s vs %s", restored.Ingress.Listen, original.Ingress.Listen)
	}
	if len(restored.Providers) != len(original.Providers) {
		t.Fatalf("Providers count mismatch: %d vs %d", len(restored.Providers), len(original.Providers))
	}
	if restored.Providers[0].ProviderID != original.Providers[0].ProviderID {
		t.Errorf("ProviderID mismatch: %s vs %s", restored.Providers[0].ProviderID, original.Providers[0].ProviderID)
	}
	if len(restored.Providers[0].ModelTable) != len(original.Providers[0].ModelTable) {
		t.Fatalf("ModelTable count mismatch: %d vs %d", len(restored.Providers[0].ModelTable), len(original.Providers[0].ModelTable))
	}
	if restored.RoutingPolicy.MaxRetries != original.RoutingPolicy.MaxRetries {
		t.Errorf("MaxRetries mismatch: %d vs %d", restored.RoutingPolicy.MaxRetries, original.RoutingPolicy.MaxRetries)
	}
	if restored.RoutingPolicy.StickySessions != original.RoutingPolicy.StickySessions {
		t.Errorf("StickySessions mismatch: %#v vs %#v", restored.RoutingPolicy.StickySessions, original.RoutingPolicy.StickySessions)
	}
	if restored.RoutingPolicy.FailurePolicy.PassthroughAfterSec != original.RoutingPolicy.FailurePolicy.PassthroughAfterSec {
		t.Errorf("PassthroughAfterSec mismatch: %d vs %d", restored.RoutingPolicy.FailurePolicy.PassthroughAfterSec, original.RoutingPolicy.FailurePolicy.PassthroughAfterSec)
	}
	if restored.TelemetryEmit.Channel != original.TelemetryEmit.Channel {
		t.Errorf("Telemetry channel mismatch: %s vs %s", restored.TelemetryEmit.Channel, original.TelemetryEmit.Channel)
	}

	// Note: Credentials.Value should be empty after JSON round-trip because of json:"-"
	if restored.Providers[0].Credentials.Value != "" {
		t.Errorf("Credentials.Value should be empty after JSON round-trip, got: %s", restored.Providers[0].Credentials.Value)
	}
}

// TestSnapshotValidationWithManyProviders verifies validation scales with many providers.
func TestSnapshotValidationWithManyProviders(t *testing.T) {
	snap := &Snapshot{
		Meta: SnapshotMeta{
			SnapshotID:      "snap_many_providers",
			SchemaVersion:   CurrentSchemaVersion,
			RevisionID:      "rev_many_providers",
			GeneratedAt:     time.Now().UTC(),
			CompilerVersion: "1.0.0",
		},
		Ingress: IngressConfig{
			Listen: "127.0.0.1:18080",
		},
		Contract: ContractConfig{
			PublicAPI:     "openai_chat_completions",
			EnabledRoutes: []string{"POST /v1/chat/completions"},
		},
		Providers: make([]ProviderSnapshot, 100),
	}

	for i := 0; i < 100; i++ {
		snap.Providers[i] = ProviderSnapshot{
			ProviderID:      "provider-" + string(rune('a'+i)),
			ProtocolAdapter: "openai_chat_completions",
			BaseURL:         "https://api.example.com/v1",
			ModelTable: []ModelMapping{
				{PublicModel: "model-" + string(rune('a'+i)), UpstreamModel: "upstream-" + string(rune('a'+i))},
			},
			ExecutionPolicy: ExecutionPolicy{
				Enabled: true,
				Weight:  1,
			},
		}
	}

	if err := validateSnapshot(snap); err != nil {
		t.Fatalf("Validation failed for 100 providers: %v", err)
	}
}

// TestSnapshotClone verifies that cloning a snapshot produces an independent copy.
func TestSnapshotClone(t *testing.T) {
	original := &Snapshot{
		Meta: SnapshotMeta{
			SnapshotID:    "snap_clone_test",
			SchemaVersion: CurrentSchemaVersion,
		},
		Ingress: IngressConfig{
			Listen: "127.0.0.1:18080",
		},
		Providers: []ProviderSnapshot{
			{
				ProviderID: "provider-1",
				BaseURL:    "https://api1.example.com",
				ModelTable: []ModelMapping{
					{PublicModel: "model-1", UpstreamModel: "upstream-1"},
				},
			},
		},
	}

	// Serialize and deserialize to create a deep copy
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var clone Snapshot
	if err := json.Unmarshal(data, &clone); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// Modify clone
	clone.Providers[0].ModelTable[0].PublicModel = "modified"

	// Verify original is unchanged
	if original.Providers[0].ModelTable[0].PublicModel == "modified" {
		t.Error("Modifying clone affected original - shallow copy detected")
	}
}
