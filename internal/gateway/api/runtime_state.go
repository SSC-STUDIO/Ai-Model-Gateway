package api

import (
	"context"
	"strings"
	"sync"
	"time"

	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/gateway/queue"
	"ai-model-gateway/internal/gateway/snapshot"
)

type RuntimeState struct {
	mu        sync.Mutex
	now       func() time.Time
	providers map[string]*providerRuntimeState
	sticky    map[string]stickyBinding
	// keyRotators holds live API key rotation state per provider (multi-key only).
	keyRotators          map[string]*KeyRotator
	upstreamRateLimiters map[string]*upstreamRateLimiter
	requestQueue         *queue.Queue
	queueConfig          snapshot.QueueConfig
}

type providerRuntimeState struct {
	lastCheck           time.Time
	lastSuccess         time.Time
	lastFailure         time.Time
	lastLatency         time.Duration
	consecutiveFailures int
	quotaBlockedAt      time.Time
}

type stickyBinding struct {
	providerID string
	expiresAt  time.Time
}

type providerGateState struct {
	blocked             bool
	passthroughEligible bool
	cooldownUntil       time.Time
}

func NewRuntimeState() *RuntimeState {
	return &RuntimeState{
		now:                  time.Now,
		providers:            make(map[string]*providerRuntimeState),
		sticky:               make(map[string]stickyBinding),
		keyRotators:          make(map[string]*KeyRotator),
		upstreamRateLimiters: make(map[string]*upstreamRateLimiter),
	}
}

func (s *RuntimeState) ApplySnapshot(snap *snapshot.Snapshot) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	activeProviders := make(map[string]struct{})
	if snap != nil {
		for _, provider := range snap.Providers {
			activeProviders[provider.ProviderID] = struct{}{}
			if _, ok := s.providers[provider.ProviderID]; !ok {
				s.providers[provider.ProviderID] = &providerRuntimeState{}
			}
		}
	}

	for providerID := range s.providers {
		if _, ok := activeProviders[providerID]; !ok {
			delete(s.providers, providerID)
		}
	}
	for key, binding := range s.sticky {
		if _, ok := activeProviders[binding.providerID]; !ok {
			delete(s.sticky, key)
		}
	}

	s.keyRotators = make(map[string]*KeyRotator)
	activeLimiters := make(map[string]struct{})
	if snap != nil {
		for i := range snap.Providers {
			p := &snap.Providers[i]
			if len(p.APIKeys) > 0 {
				s.keyRotators[p.ProviderID] = NewKeyRotator(p)
			}
			limit := p.ExecutionPolicy.RateLimit
			if limit.Enabled && limit.RequestsPerSecond > 0 {
				key := upstreamRateLimitKey(p)
				activeLimiters[key] = struct{}{}
				if existing := s.upstreamRateLimiters[key]; existing == nil || !existing.matches(limit) {
					s.upstreamRateLimiters[key] = newUpstreamRateLimiter(limit, s.clockNow())
				}
			}
		}
	}
	for key := range s.upstreamRateLimiters {
		if _, ok := activeLimiters[key]; !ok {
			delete(s.upstreamRateLimiters, key)
		}
	}
	s.applyQueueConfigLocked(snap)
}

func (s *RuntimeState) ProviderHealthSnapshot(snap *snapshot.Snapshot) map[string]gatewaycontrol.ProviderHealth {
	if s == nil || snap == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clockNow()
	s.cleanupExpiredStickyLocked(now)

	health := make(map[string]gatewaycontrol.ProviderHealth, len(snap.Providers))
	for _, provider := range snap.Providers {
		st := s.ensureProviderStateLocked(provider.ProviderID)
		gate := providerGateStateFromLocked(st, snap.RoutingPolicy.FailurePolicy, now)
		lastCheck := st.lastCheck
		if lastCheck.IsZero() {
			lastCheck = st.lastFailure
		}
		if lastCheck.IsZero() {
			lastCheck = st.lastSuccess
		}

		upstreamID := provider.UpstreamID
		if strings.TrimSpace(upstreamID) == "" {
			upstreamID = provider.ProviderID
		}
		current, exists := health[upstreamID]
		if !exists {
			current = gatewaycontrol.ProviderHealth{
				Name:             upstreamID,
				UpstreamID:       upstreamID,
				BaseURL:          provider.BaseURL,
				AnthropicBaseURL: provider.AnthropicBaseURL,
				Healthy:          true,
			}
		}
		current.ProviderIDs = append(current.ProviderIDs, provider.ProviderID)
		if gate.blocked {
			current.Healthy = false
		}
		current.LastCheck = maxTime(current.LastCheck, lastCheck)
		current.LastSuccess = maxTime(current.LastSuccess, st.lastSuccess)
		current.ConsecutiveFailures += st.consecutiveFailures
		current.CooldownUntil = maxTime(current.CooldownUntil, gate.cooldownUntil)
		if latencyMs := st.lastLatency.Milliseconds(); latencyMs > current.LatencyMs {
			current.LatencyMs = latencyMs
		}
		health[upstreamID] = current
	}

	return health
}

func (s *RuntimeState) orderCandidates(
	snap *snapshot.Snapshot,
	model string,
	stickyKey string,
	candidates []providerCandidate,
) []providerCandidate {
	if len(candidates) == 0 {
		return nil
	}
	if s == nil || snap == nil {
		return orderProviderCandidates(candidates)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clockNow()
	s.cleanupExpiredStickyLocked(now)

	var (
		stickyPreferred providerCandidate
		hasStickyAvail  bool
		stickyFallback  providerCandidate
		hasStickyFall   bool
		available       []providerCandidate
		passthrough     []providerCandidate
	)

	stickyProviderID, stickyBound := s.lookupStickyProviderLocked(model, stickyKey)
	for _, candidate := range candidates {
		st := s.ensureProviderStateLocked(candidate.provider.ProviderID)
		gate := providerGateStateFromLocked(st, snap.RoutingPolicy.FailurePolicy, now)
		if gate.blocked {
			if gate.passthroughEligible {
				if stickyBound && stickyProviderID == candidate.provider.ProviderID && !hasStickyFall {
					stickyFallback = candidate
					hasStickyFall = true
					continue
				}
				passthrough = append(passthrough, candidate)
			}
			continue
		}
		if stickyBound && stickyProviderID == candidate.provider.ProviderID && !hasStickyAvail {
			stickyPreferred = candidate
			hasStickyAvail = true
			continue
		}
		available = append(available, candidate)
	}

	ordered := make([]providerCandidate, 0, len(candidates))
	if hasStickyAvail {
		ordered = append(ordered, stickyPreferred)
	}
	ordered = append(ordered, orderProviderCandidates(available)...)
	if hasStickyFall {
		ordered = append(ordered, stickyFallback)
	}
	ordered = append(ordered, orderProviderCandidates(passthrough)...)
	return ordered
}

func (s *RuntimeState) rememberSticky(model string, stickyKey string, providerID string, snap *snapshot.Snapshot) {
	if s == nil || snap == nil {
		return
	}

	ttlSec := snap.RoutingPolicy.StickySessions.TTLSec
	if !snap.RoutingPolicy.StickySessions.Enabled || stickyKey == "" || providerID == "" || ttlSec <= 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.sticky[stickyBindingKey(model, stickyKey)] = stickyBinding{
		providerID: providerID,
		expiresAt:  s.clockNow().Add(time.Duration(ttlSec) * time.Second),
	}
}

func (s *RuntimeState) reportAttemptResult(
	providerID string,
	statusCode int,
	latency time.Duration,
	forwardErr error,
	snap *snapshot.Snapshot,
) {
	if s == nil || snap == nil || strings.TrimSpace(providerID) == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clockNow()
	st := s.ensureProviderStateLocked(providerID)
	st.lastCheck = now
	st.lastLatency = latency

	if isProviderFailure(statusCode, forwardErr) {
		st.lastFailure = now
		st.consecutiveFailures++
		if statusCode == 429 {
			st.quotaBlockedAt = now
		}
		return
	}

	st.lastSuccess = now
	st.consecutiveFailures = 0
	st.quotaBlockedAt = time.Time{}
}

func (s *RuntimeState) ReportProbeResult(
	providerID string,
	statusCode int,
	latency time.Duration,
	forwardErr error,
	snap *snapshot.Snapshot,
) {
	s.reportAttemptResult(providerID, statusCode, latency, forwardErr, snap)
}

// KeyRotatorForProvider returns the live rotator for multi-key providers, or a
// fresh rotator when API keys are not configured (no cross-request state).
func (s *RuntimeState) KeyRotatorForProvider(provider *snapshot.ProviderSnapshot) *KeyRotator {
	if provider == nil {
		return NewKeyRotator(nil)
	}
	if len(provider.APIKeys) == 0 {
		return NewKeyRotator(provider)
	}
	if s == nil {
		return NewKeyRotator(provider)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.keyRotators == nil {
		return NewKeyRotator(provider)
	}
	if kr, ok := s.keyRotators[provider.ProviderID]; ok && kr != nil {
		return kr
	}
	return NewKeyRotator(provider)
}

// TryRecoverAPIKeys resets exhausted API keys after cooldown. Returns true if
// any key state changed.
func (s *RuntimeState) TryRecoverAPIKeys(snap *snapshot.Snapshot, cooldown time.Duration) bool {
	if s == nil || snap == nil {
		return false
	}
	changed := false
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range snap.Providers {
		p := &snap.Providers[i]
		if len(p.APIKeys) == 0 {
			continue
		}
		kr := s.keyRotators[p.ProviderID]
		if kr == nil {
			continue
		}
		if kr.TryRecover(cooldown) {
			changed = true
		}
	}
	return changed
}

func (s *RuntimeState) WaitForUpstreamSlot(ctx context.Context, provider *snapshot.ProviderSnapshot) error {
	if s == nil || provider == nil {
		return nil
	}
	limit := provider.ExecutionPolicy.RateLimit
	if !limit.Enabled || limit.RequestsPerSecond <= 0 {
		return nil
	}

	key := upstreamRateLimitKey(provider)
	s.mu.Lock()
	if s.upstreamRateLimiters == nil {
		s.upstreamRateLimiters = make(map[string]*upstreamRateLimiter)
	}
	limiter := s.upstreamRateLimiters[key]
	if limiter == nil || !limiter.matches(limit) {
		limiter = newUpstreamRateLimiter(limit, s.clockNow())
		s.upstreamRateLimiters[key] = limiter
	}
	wait := limiter.reserveDelay(s.clockNow())
	s.mu.Unlock()

	if wait <= 0 {
		return nil
	}

	timer := time.NewTimer(wait)
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		timer.Stop()
		return ctx.Err()
	}
}

type upstreamRateLimiter struct {
	rps       float64
	burst     int
	tokens    float64
	lastCheck time.Time
}

func newUpstreamRateLimiter(limit snapshot.RateLimitConfig, now time.Time) *upstreamRateLimiter {
	burst := limit.Burst
	if burst <= 0 {
		burst = 1
	}
	return &upstreamRateLimiter{
		rps:       limit.RequestsPerSecond,
		burst:     burst,
		tokens:    float64(burst),
		lastCheck: now,
	}
}

func (l *upstreamRateLimiter) matches(limit snapshot.RateLimitConfig) bool {
	if l == nil {
		return false
	}
	burst := limit.Burst
	if burst <= 0 {
		burst = 1
	}
	return l.rps == limit.RequestsPerSecond && l.burst == burst
}

func (l *upstreamRateLimiter) reserveDelay(now time.Time) time.Duration {
	if l == nil || l.rps <= 0 {
		return 0
	}
	if l.lastCheck.IsZero() {
		l.lastCheck = now
	}
	if now.After(l.lastCheck) {
		l.tokens += now.Sub(l.lastCheck).Seconds() * l.rps
		if l.tokens > float64(l.burst) {
			l.tokens = float64(l.burst)
		}
		l.lastCheck = now
	}
	if l.tokens >= 1 {
		l.tokens--
		return 0
	}
	needed := 1 - l.tokens
	wait := time.Duration((needed / l.rps) * float64(time.Second))
	if wait <= 0 {
		wait = time.Nanosecond
	}
	l.tokens = 0
	l.lastCheck = now.Add(wait)
	return wait
}

func upstreamRateLimitKey(provider *snapshot.ProviderSnapshot) string {
	if provider == nil {
		return ""
	}
	if upstreamID := strings.TrimSpace(provider.UpstreamID); upstreamID != "" {
		return upstreamID
	}
	if baseURL := strings.TrimSpace(provider.AnthropicBaseURL); baseURL != "" {
		return baseURL
	}
	if baseURL := strings.TrimSpace(provider.BaseURL); baseURL != "" {
		return baseURL
	}
	return strings.TrimSpace(provider.ProviderID)
}

func (s *RuntimeState) EnqueueRequest(ctx context.Context, snap *snapshot.Snapshot, priority queue.Priority, execute func() error) error {
	if execute == nil {
		return nil
	}
	if s == nil || snap == nil || !snap.RoutingPolicy.Queue.Enabled {
		return execute()
	}

	s.mu.Lock()
	q := s.requestQueue
	s.mu.Unlock()
	if q == nil {
		return execute()
	}

	done := make(chan error, 1)
	err := q.Enqueue(ctx, priority, queue.PendingRequest{
		Priority: priority,
		Execute: func() error {
			err := execute()
			select {
			case done <- err:
			default:
			}
			return err
		},
	})
	if err != nil {
		return err
	}

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *RuntimeState) clockNow() time.Time {
	if s == nil || s.now == nil {
		return time.Now()
	}
	return s.now()
}

func (s *RuntimeState) ensureProviderStateLocked(providerID string) *providerRuntimeState {
	if s.providers == nil {
		s.providers = make(map[string]*providerRuntimeState)
	}
	st, ok := s.providers[providerID]
	if !ok {
		st = &providerRuntimeState{}
		s.providers[providerID] = st
	}
	return st
}

func (s *RuntimeState) lookupStickyProviderLocked(model string, stickyKey string) (string, bool) {
	if stickyKey == "" {
		return "", false
	}
	binding, ok := s.sticky[stickyBindingKey(model, stickyKey)]
	if !ok {
		return "", false
	}
	if !binding.expiresAt.IsZero() && s.clockNow().After(binding.expiresAt) {
		delete(s.sticky, stickyBindingKey(model, stickyKey))
		return "", false
	}
	return binding.providerID, true
}

func (s *RuntimeState) cleanupExpiredStickyLocked(now time.Time) {
	for key, binding := range s.sticky {
		if !binding.expiresAt.IsZero() && now.After(binding.expiresAt) {
			delete(s.sticky, key)
		}
	}
}

func stickyBindingKey(model string, stickyKey string) string {
	return model + "\x00" + stickyKey
}

func providerGateStateFromLocked(st *providerRuntimeState, policy snapshot.FailurePolicy, now time.Time) providerGateState {
	var state providerGateState
	if policy.DisableCooldown {
		return state
	}

	cooldownSec := max(policy.CooldownSec, 0)
	threshold := max(policy.Threshold, 0)
	passthroughAfter := time.Duration(max(policy.PassthroughAfterSec, 0)) * time.Second

	if threshold > 0 && st.consecutiveFailures >= threshold && !st.lastFailure.IsZero() && cooldownSec > 0 {
		until := st.lastFailure.Add(time.Duration(cooldownSec) * time.Second)
		if now.Before(until) {
			state.blocked = true
			state.cooldownUntil = maxTime(state.cooldownUntil, until)
			if passthroughAfter > 0 && now.Sub(st.lastFailure) >= passthroughAfter {
				state.passthroughEligible = true
			}
		}
	}

	if !st.quotaBlockedAt.IsZero() {
		recoveryMin := max(policy.QuotaRecoveryIntervalMin, 0)
		until := st.quotaBlockedAt.Add(time.Duration(recoveryMin) * time.Minute)
		if now.Before(until) {
			state.blocked = true
			state.cooldownUntil = maxTime(state.cooldownUntil, until)
			if passthroughAfter > 0 && now.Sub(st.quotaBlockedAt) >= passthroughAfter {
				state.passthroughEligible = true
			}
		}
	}

	if !state.blocked {
		state.passthroughEligible = false
	}
	return state
}

func isProviderFailure(statusCode int, forwardErr error) bool {
	if forwardErr != nil {
		return true
	}
	return statusCode == 429 || statusCode == 408 || statusCode >= 500
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func (s *RuntimeState) applyQueueConfigLocked(snap *snapshot.Snapshot) {
	var cfg snapshot.QueueConfig
	if snap != nil {
		cfg = snap.RoutingPolicy.Queue
	}

	if !cfg.Enabled || cfg.MaxConcurrent <= 0 {
		if s.requestQueue != nil {
			s.requestQueue.Close()
			s.requestQueue = nil
		}
		s.queueConfig = snapshot.QueueConfig{}
		return
	}

	if s.requestQueue != nil &&
		s.queueConfig.Enabled &&
		s.queueConfig.MaxConcurrent == cfg.MaxConcurrent &&
		s.queueConfig.HighPriorityPct == cfg.HighPriorityPct {
		s.queueConfig = cfg
		return
	}

	if s.requestQueue != nil {
		s.requestQueue.Close()
	}
	s.requestQueue = queue.NewQueue(cfg.MaxConcurrent, cfg.HighPriorityPct)
	s.queueConfig = cfg
}
