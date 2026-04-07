package app

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ai-model-gateway/internal/core"
)

// selector implements core.RouteSelector with health-aware weighted routing,
// sticky-session affinity, and circuit-breaker cooldown.
type selector struct {
	mu        sync.RWMutex
	providers []core.Provider
	status    map[string]*providerStatus // keyed by provider name
	cfg       core.RoutingConfig
	cursor    atomic.Int64
	probe     healthProbeFunc
	probeHTTP *http.Client

	healthOnce sync.Once

	stickyMu sync.RWMutex
	sticky   map[string]string // stickyKey → provider name
}

type healthProbeFunc func(context.Context, core.Provider, core.HealthCheckConfig) (int, time.Duration, error)

type providerStatus struct {
	consecutiveFailures int
	consecutiveSuccess  int
	lastFailure         time.Time
	coolingDown         bool
	quotaBlocked        bool
	quotaBlockedAt      time.Time
}

// NewRouteSelector creates a RouteSelector from routing config and providers.
func NewRouteSelector(cfg core.RoutingConfig, providers []core.Provider) core.RouteSelector {
	return newRouteSelector(cfg, providers, nil)
}

func newRouteSelector(cfg core.RoutingConfig, providers []core.Provider, probe healthProbeFunc) *selector {
	status := make(map[string]*providerStatus, len(providers))
	for _, p := range providers {
		status[p.Name] = &providerStatus{}
	}
	sel := &selector{
		providers: providers,
		status:    status,
		cfg:       cfg,
		sticky:    make(map[string]string),
		probeHTTP: &http.Client{
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          64,
				MaxIdleConnsPerHost:   16,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: time.Second,
				// Security: Limit TLS to secure versions only
				TLSClientConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
				},
			},
		},
	}
	if probe != nil {
		sel.probe = probe
	} else {
		sel.probe = sel.httpProbe
	}
	return sel
}

func (s *selector) Select(_ context.Context, model string, stickyKey string) (*core.Provider, error) {
	return s.selectProvider(model, stickyKey, nil)
}

func (s *selector) SelectWithExclusions(_ context.Context, model string, stickyKey string, excluded map[string]struct{}) (*core.Provider, error) {
	return s.selectProvider(model, stickyKey, excluded)
}

func (s *selector) selectProvider(model string, stickyKey string, excluded map[string]struct{}) (*core.Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Try sticky-session first.
	if stickyKey != "" && s.cfg.StickySessions.Enabled {
		if p := s.trySticky(stickyKey, model, excluded); p != nil {
			return p, nil
		}
	}

	// Build candidate pool.
	pools := make(map[core.ProviderClass][]candidate, len(prioritizedProviderClasses()))
	for i := range s.providers {
		p := &s.providers[i]
		if _, skip := excluded[p.Name]; skip {
			continue
		}
		if !p.IsEnabled() {
			continue
		}
		if !supportsModel(p, model) {
			continue
		}
		st := s.status[p.Name]
		if st != nil && s.isCoolingDown(st) {
			continue
		}
		w := s.effectiveWeight(p.Weight, st)
		class := p.ProviderClass
		if class != core.ProviderClassFree {
			class = core.ProviderClassQuotaLimited
		}
		pools[class] = append(pools[class], candidate{provider: p, weight: w})
	}

	var selected *core.Provider
	for _, class := range prioritizedProviderClasses() {
		if pool := pools[class]; len(pool) > 0 {
			selected = s.weightedSelect(pool)
			break
		}
	}
	if selected == nil {
		return nil, core.ErrNoProvider
	}

	// Record sticky binding.
	if stickyKey != "" && s.cfg.StickySessions.Enabled && selected != nil {
		s.stickyMu.Lock()
		s.sticky[stickyKey] = selected.Name
		s.stickyMu.Unlock()
	}

	return selected, nil
}

func (s *selector) RememberSticky(stickyKey string, providerName string) {
	if stickyKey == "" || providerName == "" || !s.cfg.StickySessions.Enabled {
		return
	}

	s.stickyMu.Lock()
	s.sticky[stickyKey] = providerName
	s.stickyMu.Unlock()
}

func (s *selector) UpdateConfig(cfg core.RoutingConfig, providers []core.Provider) {
	s.mu.Lock()
	defer s.mu.Unlock()

	previousProviders := make(map[string]core.Provider, len(s.providers))
	for _, provider := range s.providers {
		previousProviders[provider.Name] = provider
	}

	currentProviders := make(map[string]core.Provider, len(providers))
	status := make(map[string]*providerStatus, len(providers))
	for _, provider := range providers {
		currentProviders[provider.Name] = provider
		if existing, ok := s.status[provider.Name]; ok {
			if shouldClearQuotaBlockOnProviderChange(previousProviders[provider.Name], provider) {
				clearQuotaBlock(existing)
			}
			status[provider.Name] = existing
			continue
		}
		status[provider.Name] = &providerStatus{}
	}

	s.providers = providers
	s.cfg = cfg
	s.status = status

	s.stickyMu.Lock()
	for key, providerName := range s.sticky {
		if _, ok := currentProviders[providerName]; !ok {
			delete(s.sticky, key)
		}
	}
	s.stickyMu.Unlock()
}

func (s *selector) StartHealthChecks(ctx context.Context) {
	s.healthOnce.Do(func() {
		s.runHealthChecksOnce(ctx)
		go s.healthLoop(ctx)
	})
}

func (s *selector) ReportResult(provider *core.Provider, statusCode int, latency time.Duration, err error) {
	if provider == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	st, ok := s.status[provider.Name]
	if !ok {
		return
	}

	isFailure := err != nil || statusCode >= 500 || statusCode == 429
	if isFailure {
		st.consecutiveFailures++
		st.consecutiveSuccess = 0
		st.lastFailure = time.Now()

		if st.consecutiveFailures >= s.cfg.FailurePolicy.Threshold {
			st.coolingDown = true
		}
		if statusCode == 429 {
			st.quotaBlocked = true
			if st.quotaBlockedAt.IsZero() {
				st.quotaBlockedAt = time.Now()
			}
		}
	} else {
		st.consecutiveSuccess++
		st.consecutiveFailures = 0
		st.coolingDown = false
		clearQuotaBlock(st)
	}
}

func (s *selector) ListModels() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	seen := make(map[string]struct{}, len(s.providers)*2)
	models := make([]string, 0, len(s.providers)*2)
	for _, p := range s.providers {
		if !p.IsEnabled() {
			continue
		}
		for _, m := range p.Models {
			if _, ok := seen[m]; !ok {
				seen[m] = struct{}{}
				models = append(models, m)
			}
		}
	}
	return models
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (s *selector) trySticky(key, model string, excluded map[string]struct{}) *core.Provider {
	s.stickyMu.RLock()
	name, ok := s.sticky[key]
	s.stickyMu.RUnlock()
	if !ok {
		return nil
	}
	if _, skip := excluded[name]; skip {
		return nil
	}
	for i := range s.providers {
		p := &s.providers[i]
		if p.Name == name && p.IsEnabled() && supportsModel(p, model) {
			st := s.status[p.Name]
			if st != nil && !s.isCoolingDown(st) {
				return p
			}
		}
	}
	return nil
}

func (s *selector) isCoolingDown(st *providerStatus) bool {
	if st.coolingDown {
		elapsed := time.Since(st.lastFailure)
		if elapsed < time.Duration(s.cfg.FailurePolicy.CooldownSec)*time.Second {
			return true
		}
		// Cooldown expired — allow passthrough.
		st.coolingDown = false
	}
	if st.quotaBlocked {
		recoveryMin := quotaRecoveryIntervalMinutes(s.cfg.FailurePolicy.QuotaRecoveryIntervalMin)
		if time.Since(st.quotaBlockedAt) < time.Duration(recoveryMin)*time.Minute {
			return true
		}
		clearQuotaBlock(st)
	}
	return false
}

func (s *selector) effectiveWeight(base int, st *providerStatus) int {
	w := base
	if w < 1 {
		w = 1
	}
	if st == nil {
		return w
	}
	if st.consecutiveFailures > 0 {
		w -= st.consecutiveFailures
	}
	if st.consecutiveSuccess >= 3 {
		w++
	}
	if w < 1 {
		w = 1
	}
	return w
}

type candidate struct {
	provider *core.Provider
	weight   int
}

func (s *selector) weightedSelect(pool []candidate) *core.Provider {
	totalWeight := 0
	for _, c := range pool {
		totalWeight += c.weight
	}
	if totalWeight <= 0 {
		return pool[0].provider
	}

	cur := int(s.cursor.Add(1)-1) % totalWeight
	for _, c := range pool {
		if cur < c.weight {
			return c.provider
		}
		cur -= c.weight
	}
	return pool[len(pool)-1].provider
}

func supportsModel(p *core.Provider, model string) bool {
	if model == "" || len(p.Models) == 0 {
		return true
	}
	for _, m := range p.Models {
		if m == model {
			return true
		}
	}
	return false
}

func prioritizedProviderClasses() []core.ProviderClass {
	return []core.ProviderClass{
		core.ProviderClassFree,
		core.ProviderClassQuotaLimited,
	}
}

func quotaRecoveryIntervalMinutes(configured int) int {
	if configured <= 0 {
		return 60
	}
	if configured < 5 {
		return 5
	}
	return configured
}

func shouldClearQuotaBlockOnProviderChange(previous core.Provider, current core.Provider) bool {
	if current.ProviderClass != core.ProviderClassQuotaLimited {
		return true
	}
	if previous.Name == "" {
		return false
	}
	if previous.ProviderClass != current.ProviderClass {
		return true
	}
	if previous.BaseURL != current.BaseURL {
		return true
	}
	if previous.AnthropicBaseURL != current.AnthropicBaseURL {
		return true
	}
	if previous.APIKey != current.APIKey {
		return true
	}
	return false
}

func clearQuotaBlock(st *providerStatus) {
	if st == nil {
		return
	}
	st.quotaBlocked = false
	st.quotaBlockedAt = time.Time{}
}

func (s *selector) healthLoop(ctx context.Context) {
	for {
		interval := s.healthInterval()
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		s.runHealthChecksOnce(ctx)
	}
}

func (s *selector) healthInterval() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.cfg.Health.Enabled {
		return time.Second
	}
	interval := time.Duration(s.cfg.Health.IntervalSec) * time.Second
	if interval <= 0 {
		return 10 * time.Second
	}
	return interval
}

func (s *selector) runHealthChecksOnce(ctx context.Context) {
	providers, cfg := s.healthSnapshot()
	if !cfg.Enabled {
		return
	}

	var wg sync.WaitGroup
	for _, provider := range providers {
		provider := provider
		wg.Add(1)
		go func() {
			defer wg.Done()
			statusCode, latency, err := s.probe(ctx, provider, cfg)
			if err != nil && statusCode == 0 {
				statusCode = http.StatusServiceUnavailable
			}
			s.ReportResult(&provider, statusCode, latency, err)
		}()
	}
	wg.Wait()
}

func (s *selector) healthSnapshot() ([]core.Provider, core.HealthCheckConfig) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	providers := make([]core.Provider, 0, len(s.providers))
	for _, provider := range s.providers {
		if provider.IsEnabled() {
			providers = append(providers, provider)
		}
	}
	return providers, s.cfg.Health
}

func (s *selector) httpProbe(ctx context.Context, provider core.Provider, cfg core.HealthCheckConfig) (int, time.Duration, error) {
	path := strings.TrimSpace(cfg.Path)
	if path == "" {
		path = "/v1/models"
	}

	timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, buildUpstreamURL(&provider, path), nil)
	if err != nil {
		return 0, 0, err
	}
	if usesAnthropicHeaders(path) {
		req.Header.Del("Authorization")
		if strings.TrimSpace(provider.APIKey) != "" {
			req.Header.Set("x-api-key", strings.TrimSpace(provider.APIKey))
		}
		if strings.TrimSpace(req.Header.Get("anthropic-version")) == "" {
			req.Header.Set("anthropic-version", "2023-06-01")
		}
	} else if strings.TrimSpace(provider.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(provider.APIKey))
	}
	for key, value := range provider.Headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		req.Header.Set(key, value)
	}

	start := time.Now()
	resp, err := s.probeHTTP.Do(req)
	latency := time.Since(start)
	if err != nil {
		return 0, latency, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= http.StatusBadRequest {
		return resp.StatusCode, latency, fmt.Errorf("health check failed with status %d", resp.StatusCode)
	}
	return resp.StatusCode, latency, nil
}
