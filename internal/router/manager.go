package router

import (
	"context"
	"fmt"
	"io"
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
	store    *state.ConfigStore
	mu       sync.Mutex
	rr       map[string]int
	statuses map[string]UpstreamStatus
	sticky   map[string]stickyAssignment
}

type stickyAssignment struct {
	Upstream  string
	ExpiresAt time.Time
}

func NewManager(store *state.ConfigStore) *Manager {
	return &Manager{
		store:    store,
		rr:       make(map[string]int),
		statuses: make(map[string]UpstreamStatus),
		sticky:   make(map[string]stickyAssignment),
	}
}

func (m *Manager) CurrentConfig() config.Config {
	return m.store.Get()
}

func (m *Manager) ConfigStore() *state.ConfigStore {
	return m.store
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

	for _, class := range prioritizedUpstreamClasses() {
		healthyPool := m.pool(cfg.Upstreams, cfg.Router.Strategy, model, excluded, true, now, class)
		if len(healthyPool) > 0 {
			return m.pickFromPool(cfg.Router.Strategy, model, class, healthyPool), true
		}
	}

	for _, class := range prioritizedUpstreamClasses() {
		fallbackPool := m.pool(cfg.Upstreams, cfg.Router.Strategy, model, excluded, false, now, class)
		if len(fallbackPool) > 0 {
			return m.pickFromPool(cfg.Router.Strategy, model, class, fallbackPool), true
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
	m.sticky[key] = stickyAssignment{
		Upstream:  upstream,
		ExpiresAt: time.Now().Add(ttl),
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

			client := &http.Client{Timeout: timeout}
			start := time.Now()
			resp, err := client.Do(req)
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

func (m *Manager) pool(upstreams []config.Upstream, strategy string, model string, excluded map[string]struct{}, healthyOnly bool, now time.Time, providerClass string) []config.Upstream {
	m.mu.Lock()
	defer m.mu.Unlock()

	pool := make([]config.Upstream, 0)
	for _, upstream := range upstreams {
		if _, skip := excluded[upstream.Name]; skip {
			continue
		}
		if !SupportsModel(upstream, model) {
			continue
		}
		if upstream.ProviderClassNormalized() != providerClass {
			continue
		}

		status := m.statusLocked(upstream.Name)
		if status.QuotaBlocked {
			continue
		}
		if status.CooldownUntil.After(now) {
			continue
		}
		if healthyOnly && !status.Healthy {
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

		for i := 0; i < effectiveWeight; i++ {
			pool = append(pool, upstream)
		}
	}
	return pool
}

func (m *Manager) pickFromPool(strategy string, model string, class string, pool []config.Upstream) config.Upstream {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := strategy + ":" + class + ":" + model
	idx := m.rr[key] % len(pool)
	m.rr[key] = (m.rr[key] + 1) % len(pool)
	return pool[idx]
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

func prioritizedUpstreamClasses() []string {
	return []string{
		config.UpstreamClassFree,
		config.UpstreamClassQuotaLimited,
	}
}

func joinURL(baseURL string, path string) string {
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}
