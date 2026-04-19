package api

import (
	"strings"
	"sync"
	"time"

	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/gateway/snapshot"
)

type RuntimeState struct {
	mu        sync.Mutex
	now       func() time.Time
	providers map[string]*providerRuntimeState
	sticky    map[string]stickyBinding
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
		now:       time.Now,
		providers: make(map[string]*providerRuntimeState),
		sticky:    make(map[string]stickyBinding),
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

		health[provider.ProviderID] = gatewaycontrol.ProviderHealth{
			Name:                provider.ProviderID,
			Healthy:             !gate.blocked,
			LastCheck:           lastCheck,
			LastSuccess:         st.lastSuccess,
			ConsecutiveFailures: st.consecutiveFailures,
			CooldownUntil:       gate.cooldownUntil,
			LatencyMs:           st.lastLatency.Milliseconds(),
		}
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

	cooldownSec := maxInt(policy.CooldownSec, 0)
	threshold := maxInt(policy.Threshold, 0)
	passthroughAfter := time.Duration(maxInt(policy.PassthroughAfterSec, 0)) * time.Second

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
		recoveryMin := maxInt(policy.QuotaRecoveryIntervalMin, 1)
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
