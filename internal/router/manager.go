package router

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"ai-model-gateway/internal/config"
	"ai-model-gateway/internal/state"
)

type UpstreamStatus struct {
	Healthy                     bool          `json:"healthy"`
	LastError                   string        `json:"last_error,omitempty"`
	LastFailureKind             string        `json:"last_failure_kind,omitempty"`
	LastCheckedAt               time.Time     `json:"last_checked_at,omitempty"`
	LastLatency                 time.Duration `json:"last_latency,omitempty"`
	QuotaBlocked                bool          `json:"quota_blocked,omitempty"`
	QuotaBlockedAt              time.Time     `json:"quota_blocked_at,omitempty"`
	ConsecutiveFailures         int           `json:"consecutive_failures"`
	ConsecutiveRetryableFailure int           `json:"consecutive_retryable_failures"`
	ConsecutiveSuccess          int           `json:"consecutive_success"`
	RetryableFailureSince       time.Time     `json:"retryable_failure_since,omitempty"`
	CooldownUntil               time.Time     `json:"cooldown_until,omitempty"`
}

type Manager struct {
	store           *state.ConfigStore
	mu              sync.Mutex
	rr              map[string]int
	statuses        map[string]UpstreamStatus
	sticky          map[string]stickyAssignment
	healthClient    *http.Client
	nextStickyPrune time.Time
}

type stickyAssignment struct {
	Upstream  string
	ExpiresAt time.Time
}

type weightedUpstream struct {
	upstream config.Upstream
	weight   int
}

const (
	stickyPruneInterval = time.Minute
	stickyMaxEntries    = 16384
)

func NewManager(store *state.ConfigStore) *Manager {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	return &Manager{
		store:        store,
		rr:           make(map[string]int),
		statuses:     make(map[string]UpstreamStatus),
		sticky:       make(map[string]stickyAssignment),
		healthClient: &http.Client{Transport: transport},
	}
}

func (m *Manager) CurrentConfig() config.Config {
	return m.store.Get()
}

func (m *Manager) ConfigStore() *state.ConfigStore {
	return m.store
}

func (m *Manager) SetConfig(cfg config.Config) {
	if m == nil || m.store == nil {
		return
	}

	previous := m.store.Get()
	m.store.Set(cfg)
	m.reconcileConfig(previous, cfg)
}

func (m *Manager) IsUpstreamEnabled(name string) bool {
	if m == nil || m.store == nil {
		return false
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}

	cfg := m.store.Get()
	for _, upstream := range cfg.Upstreams {
		if upstream.Name == name {
			return upstream.IsEnabled()
		}
	}
	return false
}

func (m *Manager) Models() []string {
	cfg := m.store.Get()
	uniq := make(map[string]struct{})
	for _, upstream := range cfg.Upstreams {
		if !upstream.IsEnabled() {
			continue
		}
		for _, model := range upstream.Models {
			if model != "" {
				uniq[model] = struct{}{}
			}
		}
	}

	models := make([]string, 0, len(uniq))
	for model := range uniq {
		models = append(models, model)
	}
	sort.Strings(models)
	return models
}

func (m *Manager) Snapshot() map[string]UpstreamStatus {
	cfg := m.store.Get()

	m.mu.Lock()
	defer m.mu.Unlock()

	snapshot := make(map[string]UpstreamStatus, len(cfg.Upstreams))
	for _, upstream := range cfg.Upstreams {
		snapshot[upstream.Name] = m.statusLocked(upstream.Name)
	}
	return snapshot
}

func (m *Manager) Pick(model string, excluded map[string]struct{}) (config.Upstream, bool) {
	return m.PickSticky(model, "", excluded)
}

func (m *Manager) PickSticky(model string, stickyKey string, excluded map[string]struct{}) (config.Upstream, bool) {
	cfg := m.store.Get()
	now := time.Now()

	if upstream, ok := m.pickStickyAssignment(cfg.Upstreams, model, stickyKey, excluded, true, now); ok {
		return upstream, true
	}

	return m.pickFromPools(cfg, model, excluded, now)
}

func (m *Manager) pickFromPools(cfg config.Config, model string, excluded map[string]struct{}, now time.Time) (config.Upstream, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	healthyPools, fallbackPools := m.buildPoolsLocked(cfg.Upstreams, cfg.Router.Strategy, model, excluded, now)
	for _, class := range prioritizedUpstreamClasses() {
		healthyPool := healthyPools[class]
		if len(healthyPool) > 0 {
			return m.pickFromPoolLocked(cfg.Router.Strategy, model, class, healthyPool), true
		}
	}

	for _, class := range prioritizedUpstreamClasses() {
		fallbackPool := fallbackPools[class]
		if len(fallbackPool) > 0 {
			return m.pickFromPoolLocked(cfg.Router.Strategy, model, class, fallbackPool), true
		}
	}
	return config.Upstream{}, false
}

func (m *Manager) RememberSticky(key string, upstream string) {
	key = strings.TrimSpace(key)
	upstream = strings.TrimSpace(upstream)
	if key == "" || upstream == "" {
		return
	}

	cfg := m.store.Get()
	if !cfg.Router.StickySessions.Enabled {
		return
	}

	ttl := time.Duration(cfg.Router.StickySessions.TTLSec) * time.Second
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if m.nextStickyPrune.IsZero() || now.After(m.nextStickyPrune) || len(m.sticky) >= stickyMaxEntries {
		m.pruneStickyLocked(now)
	}
	m.sticky[key] = stickyAssignment{
		Upstream:  upstream,
		ExpiresAt: now.Add(ttl),
	}
}

func (m *Manager) ReportProbe(name string, latency time.Duration, err error) {
	if err == nil {
		m.reportSuccess(name, latency, http.StatusOK)
		return
	}
	m.reportFailure(name, latency, http.StatusServiceUnavailable, err, true, "probe")
}

func (m *Manager) ReportRequestSuccess(name string, latency time.Duration, statusCode int) {
	m.reportSuccess(name, latency, statusCode)
}

func (m *Manager) ReportRequestFailure(name string, latency time.Duration, statusCode int, err error, retryable bool, kind string) {
	m.reportFailure(name, latency, statusCode, err, retryable, kind)
}

func (m *Manager) BlockUpstreamQuota(name string, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	status := m.statusLocked(name)
	status.Healthy = false
	status.LastCheckedAt = time.Now()
	status.LastError = strings.TrimSpace(reason)
	status.LastFailureKind = "quota_exhausted"
	status.QuotaBlocked = true
	if status.QuotaBlockedAt.IsZero() {
		status.QuotaBlockedAt = time.Now()
	}
	status.CooldownUntil = time.Time{}
	m.statuses[name] = status
}

func (m *Manager) ShouldPassthroughFailure(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg := m.store.Get()
	status := m.statusLocked(name)
	if status.RetryableFailureSince.IsZero() {
		return false
	}
	window := time.Duration(cfg.Router.FailurePassthroughAfterSec) * time.Second
	if window <= 0 {
		window = 10 * time.Minute
	}
	return time.Since(status.RetryableFailureSince) >= window
}

func (m *Manager) StartHealthChecks(ctx context.Context) {
	for {
		cfg := m.store.Get()
		if cfg.Health.Enabled {
			m.runHealthChecksOnce(ctx)
		}

		interval := time.Duration(cfg.Health.IntervalSec) * time.Second
		if interval <= 0 {
			interval = 10 * time.Second
		}
		if !cfg.Health.Enabled {
			interval = time.Second
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (m *Manager) runHealthChecksOnce(ctx context.Context) {
	cfg := m.store.Get()
	if !cfg.Health.Enabled {
		return
	}

	var wg sync.WaitGroup
	for _, upstream := range cfg.Upstreams {
		if !upstream.IsEnabled() {
			continue
		}

		wg.Add(1)
		go func(upstream config.Upstream) {
			defer wg.Done()

			timeout := time.Duration(cfg.Health.TimeoutMs) * time.Millisecond
			if timeout <= 0 {
				timeout = 2 * time.Second
			}

			reqCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, joinURL(upstream.BaseURL, cfg.Health.Path), nil)
			if err != nil {
				m.ReportProbe(upstream.Name, 0, err)
				return
			}

			if upstream.APIKey != "" {
				req.Header.Set("Authorization", "Bearer "+upstream.APIKey)
			}
			for key, value := range upstream.Headers {
				req.Header.Set(key, value)
			}

			start := time.Now()
			resp, err := m.healthClient.Do(req)
			latency := time.Since(start)
			if err != nil {
				m.ReportProbe(upstream.Name, latency, err)
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()

			if resp.StatusCode >= http.StatusBadRequest {
				m.ReportProbe(upstream.Name, latency, fmt.Errorf("probe status %d", resp.StatusCode))
				return
			}
			m.ReportProbe(upstream.Name, latency, nil)
		}(upstream)
	}
	wg.Wait()
}

func (m *Manager) reportSuccess(name string, latency time.Duration, statusCode int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	status := m.statusLocked(name)
	clearQuotaBlockStatus(&status)
	status.Healthy = true
	status.LastError = ""
	status.LastFailureKind = ""
	status.LastCheckedAt = time.Now()
	status.LastLatency = latency
	status.ConsecutiveFailures = 0
	status.ConsecutiveRetryableFailure = 0
	status.ConsecutiveSuccess++
	status.RetryableFailureSince = time.Time{}
	status.CooldownUntil = time.Time{}
	m.statuses[name] = status
}

func (m *Manager) reconcileConfig(previous config.Config, current config.Config) {
	previousUpstreams := make(map[string]config.Upstream, len(previous.Upstreams))
	for _, upstream := range previous.Upstreams {
		previousUpstreams[upstream.Name] = upstream
	}

	currentUpstreams := make(map[string]config.Upstream, len(current.Upstreams))
	for _, upstream := range current.Upstreams {
		currentUpstreams[upstream.Name] = upstream
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for name := range m.statuses {
		if _, ok := currentUpstreams[name]; !ok {
			delete(m.statuses, name)
		}
	}

	for name, assignment := range m.sticky {
		if _, ok := currentUpstreams[assignment.Upstream]; !ok {
			delete(m.sticky, name)
		}
	}

	for name, upstream := range currentUpstreams {
		status, ok := m.statuses[name]
		if !ok {
			continue
		}

		if shouldClearQuotaBlockOnConfigChange(previousUpstreams[name], upstream) {
			clearQuotaBlockStatus(&status)
			m.statuses[name] = status
		}
	}
}

func shouldClearQuotaBlockOnConfigChange(previous config.Upstream, current config.Upstream) bool {
	if current.ProviderClassNormalized() != config.UpstreamClassQuotaLimited {
		return true
	}
	if previous.Name == "" {
		return false
	}
	if previous.ProviderClassNormalized() != current.ProviderClassNormalized() {
		return true
	}
	if previous.BaseURL != current.BaseURL {
		return true
	}
	if previous.APIKey != current.APIKey {
		return true
	}
	return false
}

func clearQuotaBlockStatus(status *UpstreamStatus) {
	if status == nil || !status.QuotaBlocked {
		return
	}

	status.QuotaBlocked = false
	status.QuotaBlockedAt = time.Time{}
	status.Healthy = true
	status.LastError = ""
	status.LastFailureKind = ""
	status.ConsecutiveFailures = 0
	status.ConsecutiveRetryableFailure = 0
	status.ConsecutiveSuccess = 0
	status.RetryableFailureSince = time.Time{}
	status.CooldownUntil = time.Time{}
}

func (m *Manager) reportFailure(name string, latency time.Duration, statusCode int, err error, retryable bool, kind string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg := m.store.Get()
	status := m.statusLocked(name)
	status.Healthy = false
	status.LastCheckedAt = time.Now()
	status.LastLatency = latency
	status.ConsecutiveFailures++
	status.ConsecutiveSuccess = 0
	status.LastFailureKind = kind

	if err != nil {
		status.LastError = err.Error()
	} else {
		status.LastError = fmt.Sprintf("status %d", statusCode)
	}

	if retryable {
		status.ConsecutiveRetryableFailure++
		if status.RetryableFailureSince.IsZero() {
			status.RetryableFailureSince = time.Now()
		}
		if cfg.Router.FailureThreshold > 0 && status.ConsecutiveRetryableFailure >= cfg.Router.FailureThreshold {
			cooldown := time.Duration(cfg.Router.CooldownSec) * time.Second
			if cooldown <= 0 {
				cooldown = 60 * time.Second
			}
			status.CooldownUntil = time.Now().Add(cooldown)
		}
	} else {
		status.ConsecutiveRetryableFailure = 0
		status.RetryableFailureSince = time.Time{}
	}

	m.statuses[name] = status
}

func (m *Manager) statusLocked(name string) UpstreamStatus {
	status, ok := m.statuses[name]
	if !ok {
		return UpstreamStatus{Healthy: true}
	}
	return status
}

func (m *Manager) buildPoolsLocked(upstreams []config.Upstream, strategy string, model string, excluded map[string]struct{}, now time.Time) (map[string][]weightedUpstream, map[string][]weightedUpstream) {
	healthyPools := make(map[string][]weightedUpstream, len(prioritizedUpstreamClasses()))
	fallbackPools := make(map[string][]weightedUpstream, len(prioritizedUpstreamClasses()))
	for _, upstream := range upstreams {
		if _, skip := excluded[upstream.Name]; skip {
			continue
		}
		if !upstream.IsEnabled() {
			continue
		}
		if !SupportsModel(upstream, model) {
			continue
		}

		providerClass := upstream.ProviderClassNormalized()
		status := m.statusLocked(upstream.Name)
		if status.QuotaBlocked {
			continue
		}
		if status.CooldownUntil.After(now) {
			continue
		}

		effectiveWeight := upstream.Weight
		switch strategy {
		case "round_robin":
			effectiveWeight = 1
		case "health_weighted_rr":
			if status.ConsecutiveFailures > 0 {
				effectiveWeight -= status.ConsecutiveFailures
			}
			if status.ConsecutiveSuccess >= 3 {
				effectiveWeight++
			}
			if effectiveWeight < 1 {
				effectiveWeight = 1
			}
		default:
			if effectiveWeight < 1 {
				effectiveWeight = 1
			}
		}

		candidate := weightedUpstream{upstream: upstream, weight: effectiveWeight}
		fallbackPools[providerClass] = append(fallbackPools[providerClass], candidate)
		if status.Healthy {
			healthyPools[providerClass] = append(healthyPools[providerClass], candidate)
		}
	}
	return healthyPools, fallbackPools
}

func (m *Manager) pickFromPoolLocked(strategy string, model string, class string, pool []weightedUpstream) config.Upstream {
	key := strategy + ":" + class + ":" + model
	totalWeight := 0
	for _, candidate := range pool {
		if candidate.weight < 1 {
			totalWeight++
			continue
		}
		totalWeight += candidate.weight
	}
	if totalWeight <= 0 {
		return pool[0].upstream
	}

	cursor := m.rr[key] % totalWeight
	m.rr[key] = (cursor + 1) % totalWeight
	for _, candidate := range pool {
		weight := candidate.weight
		if weight < 1 {
			weight = 1
		}
		if cursor < weight {
			return candidate.upstream
		}
		cursor -= weight
	}
	return pool[len(pool)-1].upstream
}

func (m *Manager) pickStickyAssignment(upstreams []config.Upstream, model string, stickyKey string, excluded map[string]struct{}, healthyOnly bool, now time.Time) (config.Upstream, bool) {
	stickyKey = strings.TrimSpace(stickyKey)
	if stickyKey == "" {
		return config.Upstream{}, false
	}

	m.mu.Lock()
	assignment, ok := m.sticky[stickyKey]
	if ok && !assignment.ExpiresAt.IsZero() && assignment.ExpiresAt.Before(now) {
		delete(m.sticky, stickyKey)
		ok = false
	}
	m.mu.Unlock()
	if !ok {
		return config.Upstream{}, false
	}

	for _, upstream := range upstreams {
		if upstream.Name != assignment.Upstream {
			continue
		}
		if _, skip := excluded[upstream.Name]; skip {
			return config.Upstream{}, false
		}
		if !upstream.IsEnabled() || !SupportsModel(upstream, model) {
			return config.Upstream{}, false
		}

		m.mu.Lock()
		status := m.statusLocked(upstream.Name)
		m.mu.Unlock()
		if status.QuotaBlocked {
			return config.Upstream{}, false
		}
		if status.CooldownUntil.After(now) {
			return config.Upstream{}, false
		}
		if healthyOnly && !status.Healthy {
			return config.Upstream{}, false
		}
		return upstream, true
	}

	return config.Upstream{}, false
}

func (m *Manager) pruneStickyLocked(now time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	m.nextStickyPrune = now.Add(stickyPruneInterval)

	if len(m.sticky) == 0 {
		return
	}

	type stickyEntry struct {
		key       string
		expiresAt time.Time
	}

	entries := make([]stickyEntry, 0, len(m.sticky))
	for key, assignment := range m.sticky {
		if !assignment.ExpiresAt.IsZero() && assignment.ExpiresAt.Before(now) {
			delete(m.sticky, key)
			continue
		}
		entries = append(entries, stickyEntry{key: key, expiresAt: assignment.ExpiresAt})
	}

	if len(m.sticky) <= stickyMaxEntries {
		return
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].expiresAt.Equal(entries[j].expiresAt) {
			return entries[i].key < entries[j].key
		}
		if entries[i].expiresAt.IsZero() {
			return true
		}
		if entries[j].expiresAt.IsZero() {
			return false
		}
		return entries[i].expiresAt.Before(entries[j].expiresAt)
	})

	excess := len(m.sticky) - stickyMaxEntries
	for _, entry := range entries {
		if excess <= 0 {
			break
		}
		delete(m.sticky, entry.key)
		excess--
	}
}

func prioritizedUpstreamClasses() []string {
	return []string{
		config.UpstreamClassFree,
		config.UpstreamClassQuotaLimited,
	}
}

func joinURL(baseURL string, path string) string {
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}
