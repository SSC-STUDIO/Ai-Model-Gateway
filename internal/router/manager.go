package router

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"ai-model-gateway/internal/config"
	"ai-model-gateway/internal/state"
)

// UpstreamStatus tracks the health and performance of an upstream.
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

// Manager manages upstream selection, health checking, and routing decisions.
type Manager struct {
	store           *state.ConfigStore
	mu              sync.Mutex
	rr              map[string]int
	statuses        map[string]UpstreamStatus
	sticky          map[string]stickyAssignment
	healthClient    *http.Client
	nextStickyPrune time.Time

	// New interfaces for testability and extensibility
	strategyRegistry *StrategyRegistry
	healthChecker    HealthChecker
	metricsCollector MetricsCollector
}

// ManagerOption configures a Manager.
type ManagerOption func(*Manager)

// WithStrategyRegistry sets the strategy registry.
func WithStrategyRegistry(sr *StrategyRegistry) ManagerOption {
	return func(m *Manager) {
		m.strategyRegistry = sr
	}
}

// WithHealthChecker sets the health checker.
func WithHealthChecker(hc HealthChecker) ManagerOption {
	return func(m *Manager) {
		m.healthChecker = hc
	}
}

// WithMetricsCollector sets the metrics collector.
func WithMetricsCollector(mc MetricsCollector) ManagerOption {
	return func(m *Manager) {
		m.metricsCollector = mc
	}
}

// stickyAssignment tracks a sticky routing assignment with expiration.
type stickyAssignment struct {
	Upstream  string
	ExpiresAt time.Time
}

// weightedUpstream represents an upstream with its selection weight.
type weightedUpstream struct {
	upstream config.Upstream
	weight   int
}

const (
	stickyPruneInterval  = time.Minute
	stickyMaxEntries     = 16384
	defaultHealthTimeout = 2 * time.Second
	defaultProbeTimeout  = 10 * time.Second
)

// NewManager creates a new routing manager with the given config store.
// It initializes the health check client with optimized connection pooling.
func NewManager(store *state.ConfigStore, opts ...ManagerOption) *Manager {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: defaultProbeTimeout, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   defaultProbeTimeout,
		ExpectContinueTimeout: time.Second,
	}

	httpClient := &http.Client{Transport: transport}

	m := &Manager{
		store:            store,
		rr:               make(map[string]int, 64),
		statuses:         make(map[string]UpstreamStatus, 16),
		sticky:           make(map[string]stickyAssignment, 256),
		healthClient:     httpClient,
		strategyRegistry: NewStrategyRegistry(),
		healthChecker:    NewHTTPHealthChecker(httpClient),
		metricsCollector: &NoopMetricsCollector{},
	}

	// Apply options
	for _, opt := range opts {
		opt(m)
	}

	return m
}

// CurrentConfig returns the current configuration.
func (m *Manager) CurrentConfig() config.Config {
	return m.store.Get()
}

// ConfigStore returns the underlying config store.
func (m *Manager) ConfigStore() *state.ConfigStore {
	return m.store
}

// SetConfig updates the current configuration and reconciles internal state.
func (m *Manager) SetConfig(cfg config.Config) {
	if m == nil || m.store == nil {
		return
	}

	previous := m.store.Get()
	m.store.Set(cfg)
	m.reconcileConfig(previous, cfg)
}

// IsUpstreamEnabled checks if an upstream with the given name is enabled.
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

// Models returns a sorted list of unique models from all enabled upstreams.
func (m *Manager) Models() []string {
	cfg := m.store.Get()
	uniq := make(map[string]struct{}, len(cfg.Upstreams)*4) // Estimate capacity
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

// Snapshot returns a copy of the current status for all upstreams.
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

// Pick selects an upstream for the given model, excluding the specified upstreams.
func (m *Manager) Pick(model string, excluded map[string]struct{}) (config.Upstream, bool) {
	return m.PickSticky(model, "", excluded)
}

// PickSticky selects an upstream for the given model, preferring sticky assignments.
// The excluded set contains upstream names that should not be selected.
func (m *Manager) PickSticky(model string, stickyKey string, excluded map[string]struct{}) (config.Upstream, bool) {
	cfg := m.store.Get()
	now := time.Now()

	if upstream, ok := m.pickStickyAssignment(cfg.Upstreams, model, stickyKey, excluded, true, now); ok {
		m.metricsCollector.RecordRouting(upstream.Name, model, true)
		return upstream, true
	}

	upstream, ok := m.pickFromPools(cfg, model, excluded, now)
	if ok {
		m.metricsCollector.RecordRouting(upstream.Name, model, true)
	}
	return upstream, ok
}

// pickFromPools selects an upstream from the healthy or fallback pools.
func (m *Manager) pickFromPools(cfg config.Config, model string, excluded map[string]struct{}, now time.Time) (config.Upstream, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	strategy := m.strategyRegistry.Get(cfg.Router.Strategy)
	healthyPools, fallbackPools := m.buildPoolsLocked(cfg.Upstreams, strategy, model, excluded, now)
	classes := prioritizedUpstreamClasses()

	// Try healthy pools first
	for _, class := range classes {
		if healthyPool := healthyPools[class]; len(healthyPool) > 0 {
			return m.pickFromPoolLocked(strategy, model, class, healthyPool), true
		}
	}

	// Fall back to unhealthy but available pools
	for _, class := range classes {
		if fallbackPool := fallbackPools[class]; len(fallbackPool) > 0 {
			return m.pickFromPoolLocked(strategy, model, class, fallbackPool), true
		}
	}
	return config.Upstream{}, false
}

// RememberSticky records a sticky routing assignment.
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

// ReportProbe reports the result of a health probe.
func (m *Manager) ReportProbe(name string, latency time.Duration, err error) {
	if err == nil {
		m.reportSuccess(name, latency, http.StatusOK)
		return
	}
	m.reportFailure(name, latency, http.StatusServiceUnavailable, err, true, "probe")
}

// ReportRequestSuccess reports a successful request to an upstream.
func (m *Manager) ReportRequestSuccess(name string, latency time.Duration, statusCode int) {
	m.reportSuccess(name, latency, statusCode)
}

// ReportRequestFailure reports a failed request to an upstream.
func (m *Manager) ReportRequestFailure(name string, latency time.Duration, statusCode int, err error, retryable bool, kind string) {
	m.reportFailure(name, latency, statusCode, err, retryable, kind)
}

// BlockUpstreamQuota marks an upstream as quota-blocked.
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

// ShouldPassthroughFailure checks if failures should be passed through after prolonged issues.
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

// StartHealthChecks runs periodic health checks until the context is cancelled.
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

// runHealthChecksOnce runs a single round of health checks for all enabled upstreams.
func (m *Manager) runHealthChecksOnce(ctx context.Context) {
	cfg := m.store.Get()
	if !cfg.Health.Enabled {
		return
	}

	var wg sync.WaitGroup
	enabledUpstreams := make([]config.Upstream, 0, len(cfg.Upstreams))
	for _, upstream := range cfg.Upstreams {
		if upstream.IsEnabled() {
			enabledUpstreams = append(enabledUpstreams, upstream)
		}
	}

	for _, upstream := range enabledUpstreams {
		wg.Add(1)
		go func(upstream config.Upstream) {
			defer wg.Done()
			m.probeUpstream(ctx, upstream, cfg)
		}(upstream)
	}
	wg.Wait()
}

// probeUpstream probes a single upstream and reports the result.
func (m *Manager) probeUpstream(ctx context.Context, upstream config.Upstream, cfg config.Config) {
	result := m.healthChecker.Check(ctx, upstream, cfg.Health)

	if result.Error != nil {
		m.ReportProbe(upstream.Name, result.Latency, result.Error)
	} else {
		m.ReportProbe(upstream.Name, result.Latency, nil)
	}
}

// reportSuccess updates the status after a successful request.
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

	m.metricsCollector.RecordHealthStatus(name, true)
	m.metricsCollector.RecordLatency(name, latency)
}

// reconcileConfig updates internal state when configuration changes.
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

	// Remove status entries for deleted upstreams
	for name := range m.statuses {
		if _, ok := currentUpstreams[name]; !ok {
			delete(m.statuses, name)
		}
	}

	// Remove sticky assignments for deleted upstreams
	for name, assignment := range m.sticky {
		if _, ok := currentUpstreams[assignment.Upstream]; !ok {
			delete(m.sticky, name)
		}
	}

	// Clear quota blocks for upstreams that changed provider class
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

// shouldClearQuotaBlockOnConfigChange determines if quota block should be cleared
// when upstream configuration changes.
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

// clearQuotaBlockStatus clears the quota block state from a status.
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

// reportFailure updates the status after a failed request.
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
	m.metricsCollector.RecordHealthStatus(name, false)
	m.metricsCollector.RecordLatency(name, latency)
}

// statusLocked returns the status for an upstream, defaulting to healthy if not found.
// Caller must hold m.mu.
func (m *Manager) statusLocked(name string) UpstreamStatus {
	status, ok := m.statuses[name]
	if !ok {
		return UpstreamStatus{Healthy: true}
	}
	return status
}

// buildPoolsLocked builds healthy and fallback pools for upstream selection.
// Caller must hold m.mu.
func (m *Manager) buildPoolsLocked(upstreams []config.Upstream, strategy Strategy, model string, excluded map[string]struct{}, now time.Time) (map[string][]weightedUpstream, map[string][]weightedUpstream) {
	classes := prioritizedUpstreamClasses()
	healthyPools := make(map[string][]weightedUpstream, len(classes))
	fallbackPools := make(map[string][]weightedUpstream, len(classes))

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
			// Check if quota block should be auto-recovered
			cfg := m.store.Get()
			recoveryMinutes := cfg.Router.QuotaBlockRecoveryIntervalMinutes
			if recoveryMinutes <= 0 {
				recoveryMinutes = 60 // Default 60 minutes
			}
			if recoveryMinutes < 5 {
				recoveryMinutes = 5 // Minimum 5 minutes
			}
			if !status.QuotaBlockedAt.IsZero() && now.Sub(status.QuotaBlockedAt) >= time.Duration(recoveryMinutes)*time.Minute {
				clearQuotaBlockStatus(&status)
				m.statuses[upstream.Name] = status
			} else {
				continue
			}
		}
		if status.CooldownUntil.After(now) {
			continue
		}

		effectiveWeight := strategy.CalculateWeight(upstream.Weight, status)

		candidate := weightedUpstream{upstream: upstream, weight: effectiveWeight}
		fallbackPools[providerClass] = append(fallbackPools[providerClass], candidate)
		if status.Healthy {
			healthyPools[providerClass] = append(healthyPools[providerClass], candidate)
		}
	}
	return healthyPools, fallbackPools
}

// pickFromPoolLocked selects an upstream from a pool using weighted round-robin.
// Caller must hold m.mu.
func (m *Manager) pickFromPoolLocked(strategy Strategy, model string, class string, pool []weightedUpstream) config.Upstream {
	key := strategy.Name() + ":" + class + ":" + model
	cursor := m.rr[key]
	upstream, nextCursor := strategy.Select(pool, cursor)
	m.rr[key] = nextCursor
	return upstream
}

// pickStickyAssignment tries to select a sticky-assigned upstream.
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
		if status.QuotaBlocked && !shouldRecoverQuotaBlock(status, now, m.store.Get()) {
			m.mu.Unlock()
			return config.Upstream{}, false
		}
		if status.QuotaBlocked {
			// Quota block was recovered
			clearQuotaBlockStatus(&status)
			m.statuses[upstream.Name] = status
		}
		m.mu.Unlock()
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

// shouldRecoverQuotaBlock checks if a quota block should be auto-recovered.
func shouldRecoverQuotaBlock(status UpstreamStatus, now time.Time, cfg config.Config) bool {
	recoveryMinutes := cfg.Router.QuotaBlockRecoveryIntervalMinutes
	if recoveryMinutes <= 0 {
		recoveryMinutes = 60 // Default 60 minutes
	}
	if recoveryMinutes < 5 {
		recoveryMinutes = 5 // Minimum 5 minutes
	}
	return !status.QuotaBlockedAt.IsZero() && now.Sub(status.QuotaBlockedAt) >= time.Duration(recoveryMinutes)*time.Minute
}

// pruneStickyLocked removes expired and excess sticky entries.
// Caller must hold m.mu.
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

// prioritizedUpstreamClasses returns the provider class priority order.
func prioritizedUpstreamClasses() []string {
	return []string{
		config.UpstreamClassFree,
		config.UpstreamClassQuotaLimited,
	}
}

// joinURL joins a base URL and path, handling slashes correctly.
func joinURL(baseURL string, path string) string {
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}
