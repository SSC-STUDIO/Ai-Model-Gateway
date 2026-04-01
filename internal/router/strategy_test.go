package router

import (
	"testing"

	"ai-model-gateway/internal/config"
)

func TestRoundRobinStrategy_Name(t *testing.T) {
	strategy := &RoundRobinStrategy{}
	if strategy.Name() != config.RouterStrategyRoundRobin {
		t.Errorf("expected name %s, got %s", config.RouterStrategyRoundRobin, strategy.Name())
	}
}

func TestRoundRobinStrategy_Select(t *testing.T) {
	strategy := &RoundRobinStrategy{}
	
	pool := []weightedUpstream{
		{upstream: config.Upstream{Name: "a"}, weight: 1},
		{upstream: config.Upstream{Name: "b"}, weight: 1},
		{upstream: config.Upstream{Name: "c"}, weight: 1},
	}

	// Test round-robin behavior
	expected := []string{"a", "b", "c", "a", "b"}
	cursor := 0
	for i, exp := range expected {
		upstream, nextCursor := strategy.Select(pool, cursor)
		if upstream.Name != exp {
			t.Errorf("iteration %d: expected %s, got %s", i, exp, upstream.Name)
		}
		cursor = nextCursor
	}
}

func TestRoundRobinStrategy_SelectEmptyPool(t *testing.T) {
	strategy := &RoundRobinStrategy{}
	pool := []weightedUpstream{}
	
	upstream, _ := strategy.Select(pool, 0)
	if upstream.Name != "" {
		t.Errorf("expected empty upstream for empty pool, got %s", upstream.Name)
	}
}

func TestRoundRobinStrategy_CalculateWeight(t *testing.T) {
	strategy := &RoundRobinStrategy{}
	
	// Round-robin always returns weight of 1 regardless of input
	tests := []struct {
		baseWeight int
		status     UpstreamStatus
		expected   int
	}{
		{1, UpstreamStatus{Healthy: true}, 1},
		{10, UpstreamStatus{Healthy: true}, 1},
		{5, UpstreamStatus{Healthy: false, ConsecutiveFailures: 10}, 1},
		{0, UpstreamStatus{}, 1},
	}

	for _, tt := range tests {
		weight := strategy.CalculateWeight(tt.baseWeight, tt.status)
		if weight != tt.expected {
			t.Errorf("baseWeight=%d: expected %d, got %d", tt.baseWeight, tt.expected, weight)
		}
	}
}

func TestHealthWeightedStrategy_Name(t *testing.T) {
	strategy := &HealthWeightedStrategy{}
	if strategy.Name() != config.RouterStrategyHealthWeightedRR {
		t.Errorf("expected name %s, got %s", config.RouterStrategyHealthWeightedRR, strategy.Name())
	}
}

func TestHealthWeightedStrategy_CalculateWeight(t *testing.T) {
	strategy := &HealthWeightedStrategy{}
	
	tests := []struct {
		name       string
		baseWeight int
		status     UpstreamStatus
		expected   int
	}{
		{
			name:       "healthy upstream with base weight",
			baseWeight: 5,
			status:     UpstreamStatus{Healthy: true, ConsecutiveSuccess: 0},
			expected:   5,
		},
		{
			name:       "healthy upstream with sustained success",
			baseWeight: 5,
			status:     UpstreamStatus{Healthy: true, ConsecutiveSuccess: 3},
			expected:   6, // base + 1 for sustained success
		},
		{
			name:       "upstream with failures",
			baseWeight: 5,
			status:     UpstreamStatus{Healthy: false, ConsecutiveFailures: 2},
			expected:   3, // base - 2 for failures
		},
		{
			name:       "upstream with many failures",
			baseWeight: 3,
			status:     UpstreamStatus{Healthy: false, ConsecutiveFailures: 10},
			expected:   1, // minimum weight
		},
		{
			name:       "zero base weight defaults to 1",
			baseWeight: 0,
			status:     UpstreamStatus{Healthy: true},
			expected:   1,
		},
		{
			name:       "negative result clamped to 1",
			baseWeight: 1,
			status:     UpstreamStatus{Healthy: false, ConsecutiveFailures: 5},
			expected:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			weight := strategy.CalculateWeight(tt.baseWeight, tt.status)
			if weight != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, weight)
			}
		})
	}
}

func TestWeightedRoundRobinStrategy_Name(t *testing.T) {
	strategy := &WeightedRoundRobinStrategy{}
	if strategy.Name() != "weighted_rr" {
		t.Errorf("expected name weighted_rr, got %s", strategy.Name())
	}
}

func TestWeightedRoundRobinStrategy_CalculateWeight(t *testing.T) {
	strategy := &WeightedRoundRobinStrategy{}
	
	tests := []struct {
		baseWeight int
		status     UpstreamStatus
		expected   int
	}{
		{5, UpstreamStatus{Healthy: true}, 5},
		{10, UpstreamStatus{Healthy: false, ConsecutiveFailures: 5}, 10}, // No health adjustment
		{0, UpstreamStatus{}, 1}, // Minimum weight
		{-5, UpstreamStatus{}, 1}, // Negative input clamped
	}

	for _, tt := range tests {
		weight := strategy.CalculateWeight(tt.baseWeight, tt.status)
		if weight != tt.expected {
			t.Errorf("baseWeight=%d: expected %d, got %d", tt.baseWeight, tt.expected, weight)
		}
	}
}

func TestSelectWeighted(t *testing.T) {
	pool := []weightedUpstream{
		{upstream: config.Upstream{Name: "heavy"}, weight: 3},
		{upstream: config.Upstream{Name: "light"}, weight: 1},
	}

	// With weights 3:1, the selection should be: heavy, heavy, heavy, light
	expected := []string{"heavy", "heavy", "heavy", "light", "heavy", "heavy", "heavy", "light"}
	cursor := 0
	
	for i, exp := range expected {
		upstream, nextCursor := selectWeighted(pool, cursor)
		if upstream.Name != exp {
			t.Errorf("iteration %d: expected %s, got %s", i, exp, upstream.Name)
		}
		cursor = nextCursor
	}
}

func TestSelectWeighted_EmptyPool(t *testing.T) {
	pool := []weightedUpstream{}
	
	upstream, _ := selectWeighted(pool, 0)
	if upstream.Name != "" {
		t.Errorf("expected empty upstream for empty pool, got %s", upstream.Name)
	}
}

func TestSelectWeighted_ZeroWeights(t *testing.T) {
	pool := []weightedUpstream{
		{upstream: config.Upstream{Name: "a"}, weight: 0},
		{upstream: config.Upstream{Name: "b"}, weight: 0},
	}

	// Zero weights should be treated as 1
	upstream, _ := selectWeighted(pool, 0)
	if upstream.Name != "a" {
		t.Errorf("expected a (first item), got %s", upstream.Name)
	}
}

func TestStrategyRegistry(t *testing.T) {
	registry := NewStrategyRegistry()
	
	// Test that all built-in strategies are registered
	strategies := []string{
		config.RouterStrategyRoundRobin,
		config.RouterStrategyHealthWeightedRR,
		"weighted_rr",
	}
	
	for _, name := range strategies {
		strategy := registry.Get(name)
		if strategy == nil {
			t.Errorf("strategy %s not found in registry", name)
			continue
		}
		if strategy.Name() != name {
			t.Errorf("strategy name mismatch: expected %s, got %s", name, strategy.Name())
		}
	}
}

func TestStrategyRegistry_UnknownStrategy(t *testing.T) {
	registry := NewStrategyRegistry()
	
	// Unknown strategy should default to health_weighted_rr
	strategy := registry.Get("unknown_strategy")
	if strategy.Name() != config.RouterStrategyHealthWeightedRR {
		t.Errorf("expected default strategy %s, got %s", config.RouterStrategyHealthWeightedRR, strategy.Name())
	}
}

func TestStrategyRegistry_Register(t *testing.T) {
	registry := &StrategyRegistry{}
	
	customStrategy := &RoundRobinStrategy{}
	registry.Register(customStrategy)
	
	retrieved := registry.Get(config.RouterStrategyRoundRobin)
	if retrieved == nil {
		t.Error("registered strategy not found")
	}
}

func TestCalculateHealthWeightedWeight(t *testing.T) {
	tests := []struct {
		name       string
		baseWeight int
		status     UpstreamStatus
		expected   int
	}{
		{
			name:       "basic healthy",
			baseWeight: 5,
			status:     UpstreamStatus{Healthy: true},
			expected:   5,
		},
		{
			name:       "with sustained success",
			baseWeight: 5,
			status:     UpstreamStatus{Healthy: true, ConsecutiveSuccess: 3},
			expected:   6,
		},
		{
			name:       "with consecutive failures",
			baseWeight: 5,
			status:     UpstreamStatus{Healthy: false, ConsecutiveFailures: 2},
			expected:   3,
		},
		{
			name:       "minimum weight enforced",
			baseWeight: 2,
			status:     UpstreamStatus{Healthy: false, ConsecutiveFailures: 10},
			expected:   1,
		},
		{
			name:       "zero base weight",
			baseWeight: 0,
			status:     UpstreamStatus{Healthy: true},
			expected:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateHealthWeightedWeight(tt.baseWeight, tt.status)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}
