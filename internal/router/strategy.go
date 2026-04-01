package router

import (
	"ai-model-gateway/internal/config"
)

// Strategy defines the interface for routing strategies.
// Implementations determine how upstreams are selected from a pool.
type Strategy interface {
	// Name returns the unique name of this strategy.
	Name() string

	// Select chooses an upstream from the given pool based on the strategy's logic.
	// The cursor is used for round-robin style selection and should be updated by the caller.
	Select(pool []weightedUpstream, cursor int) (upstream config.Upstream, nextCursor int)

	// CalculateWeight computes the effective weight for an upstream based on its health status.
	CalculateWeight(baseWeight int, status UpstreamStatus) int
}

// StrategyRegistry holds all available routing strategies.
type StrategyRegistry struct {
	strategies map[string]Strategy
}

// NewStrategyRegistry creates a new registry with all built-in strategies.
func NewStrategyRegistry() *StrategyRegistry {
	sr := &StrategyRegistry{
		strategies: make(map[string]Strategy),
	}

	// Register built-in strategies
	sr.Register(&RoundRobinStrategy{})
	sr.Register(&HealthWeightedStrategy{})
	sr.Register(&WeightedRoundRobinStrategy{})

	return sr
}

// Register adds a strategy to the registry.
func (sr *StrategyRegistry) Register(s Strategy) {
	if sr.strategies == nil {
		sr.strategies = make(map[string]Strategy)
	}
	sr.strategies[s.Name()] = s
}

// Get retrieves a strategy by name, returning a default if not found.
func (sr *StrategyRegistry) Get(name string) Strategy {
	if s, ok := sr.strategies[name]; ok {
		return s
	}
	// Default to health weighted round-robin
	return sr.strategies[config.RouterStrategyHealthWeightedRR]
}

// RoundRobinStrategy implements simple round-robin selection with equal weights.
type RoundRobinStrategy struct{}

// Name returns the strategy name.
func (r *RoundRobinStrategy) Name() string {
	return config.RouterStrategyRoundRobin
}

// Select chooses the next upstream in round-robin order.
func (r *RoundRobinStrategy) Select(pool []weightedUpstream, cursor int) (config.Upstream, int) {
	if len(pool) == 0 {
		return config.Upstream{}, 0
	}
	idx := cursor % len(pool)
	return pool[idx].upstream, (cursor + 1) % len(pool)
}

// CalculateWeight returns a fixed weight of 1 for all upstreams.
func (r *RoundRobinStrategy) CalculateWeight(baseWeight int, status UpstreamStatus) int {
	return 1
}

// HealthWeightedStrategy implements health-aware weighted selection.
type HealthWeightedStrategy struct{}

// Name returns the strategy name.
func (h *HealthWeightedStrategy) Name() string {
	return config.RouterStrategyHealthWeightedRR
}

// Select chooses an upstream using weighted round-robin.
func (h *HealthWeightedStrategy) Select(pool []weightedUpstream, cursor int) (config.Upstream, int) {
	return selectWeighted(pool, cursor)
}

// CalculateWeight adjusts weight based on health status.
func (h *HealthWeightedStrategy) CalculateWeight(baseWeight int, status UpstreamStatus) int {
	return calculateHealthWeightedWeight(baseWeight, status)
}

// WeightedRoundRobinStrategy implements simple weighted round-robin without health adjustments.
type WeightedRoundRobinStrategy struct{}

// Name returns the strategy name.
func (w *WeightedRoundRobinStrategy) Name() string {
	return "weighted_rr"
}

// Select chooses an upstream using weighted round-robin.
func (w *WeightedRoundRobinStrategy) Select(pool []weightedUpstream, cursor int) (config.Upstream, int) {
	return selectWeighted(pool, cursor)
}

// CalculateWeight returns the base weight without health adjustments.
func (w *WeightedRoundRobinStrategy) CalculateWeight(baseWeight int, status UpstreamStatus) int {
	if baseWeight < 1 {
		return 1
	}
	return baseWeight
}

// selectWeighted performs weighted selection from the pool.
func selectWeighted(pool []weightedUpstream, cursor int) (config.Upstream, int) {
	if len(pool) == 0 {
		return config.Upstream{}, 0
	}

	totalWeight := 0
	for _, candidate := range pool {
		if candidate.weight < 1 {
			totalWeight++
			continue
		}
		totalWeight += candidate.weight
	}

	if totalWeight <= 0 {
		return pool[0].upstream, 0
	}

	cursor = cursor % totalWeight
	nextCursor := (cursor + 1) % totalWeight

	for _, candidate := range pool {
		weight := candidate.weight
		if weight < 1 {
			weight = 1
		}
		if cursor < weight {
			return candidate.upstream, nextCursor
		}
		cursor -= weight
	}

	// Fallback to last item (should not reach here)
	return pool[len(pool)-1].upstream, nextCursor
}

// calculateHealthWeightedWeight computes weight adjusted for health status.
func calculateHealthWeightedWeight(baseWeight int, status UpstreamStatus) int {
	weight := baseWeight
	if weight < 1 {
		weight = 1
	}

	// Decrease weight for consecutive failures
	if status.ConsecutiveFailures > 0 {
		weight -= status.ConsecutiveFailures
	}

	// Increase weight for sustained success
	if status.ConsecutiveSuccess >= 3 {
		weight++
	}

	if weight < 1 {
		return 1
	}
	return weight
}
