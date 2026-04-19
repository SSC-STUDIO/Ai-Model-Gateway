package main

import (
	"context"
	"crypto/tls"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"ai-model-gateway/internal/gateway/snapshot"
)

func newHealthHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:          128,
			MaxIdleConnsPerHost:   16,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: time.Second,
			DialContext: (&net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2: true,
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		},
	}
}

func (d *Daemon) restartHealthProbes(snap *snapshot.Snapshot) {
	cancel, done := d.detachHealthProbeLoop()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}

	if d == nil || d.runtime == nil || d.runCtx == nil || snap == nil || !snap.RoutingPolicy.Health.Enabled {
		return
	}

	probeCtx, probeCancel := context.WithCancel(d.runCtx)
	probeDone := make(chan struct{})

	d.healthProbeMu.Lock()
	d.healthProbeCancel = probeCancel
	d.healthProbeDone = probeDone
	d.healthProbeMu.Unlock()

	go func() {
		defer close(probeDone)
		d.runHealthProbeLoop(probeCtx, snap)
	}()
}

func (d *Daemon) stopHealthProbes() {
	cancel, done := d.detachHealthProbeLoop()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (d *Daemon) detachHealthProbeLoop() (context.CancelFunc, chan struct{}) {
	if d == nil {
		return nil, nil
	}

	d.healthProbeMu.Lock()
	defer d.healthProbeMu.Unlock()

	cancel := d.healthProbeCancel
	done := d.healthProbeDone
	d.healthProbeCancel = nil
	d.healthProbeDone = nil
	return cancel, done
}

func (d *Daemon) runHealthProbeLoop(ctx context.Context, snap *snapshot.Snapshot) {
	d.runHealthProbeOnce(ctx, snap)

	interval := resolveHealthProbeInterval(snap)
	if interval <= 0 {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.runHealthProbeOnce(ctx, snap)
		}
	}
}

func (d *Daemon) runHealthProbeOnce(ctx context.Context, snap *snapshot.Snapshot) {
	if d == nil || d.runtime == nil || snap == nil || !snap.RoutingPolicy.Health.Enabled {
		return
	}

	providers := enabledProviders(snap.Providers)
	if len(providers) == 0 {
		return
	}

	var wg sync.WaitGroup
	for _, provider := range providers {
		provider := provider
		wg.Add(1)
		go func() {
			defer wg.Done()
			statusCode, latency, err := d.probeProviderHealth(ctx, provider, snap.RoutingPolicy.Health)
			if err != nil && statusCode == 0 {
				statusCode = http.StatusServiceUnavailable
			}
			logHealthProbeError(provider.ProviderID, statusCode, err)
			d.runtime.ReportProbeResult(provider.ProviderID, statusCode, latency, err, snap)
		}()
	}
	wg.Wait()
}

func (d *Daemon) probeProviderHealth(
	ctx context.Context,
	provider snapshot.ProviderSnapshot,
	healthCfg snapshot.HealthConfig,
) (int, time.Duration, error) {
	path := strings.TrimSpace(healthCfg.Path)
	if path == "" {
		path = "/v1/models"
	}

	timeout := time.Duration(healthCfg.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	url := strings.TrimRight(provider.BaseURL, "/") + path
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("Accept", "application/json")
	applyProviderHeaders(req.Header, provider)

	client := d.healthHTTP
	if client == nil {
		client = http.DefaultClient
	}

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)
	if err != nil {
		return 0, latency, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= http.StatusBadRequest {
		return resp.StatusCode, latency, errHealthStatus(resp.StatusCode)
	}
	return resp.StatusCode, latency, nil
}

func resolveHealthProbeInterval(snap *snapshot.Snapshot) time.Duration {
	if snap == nil || !snap.RoutingPolicy.Health.Enabled {
		return 0
	}
	interval := time.Duration(snap.RoutingPolicy.Health.IntervalSec) * time.Second
	if interval <= 0 {
		return 10 * time.Second
	}
	return interval
}

func enabledProviders(providers []snapshot.ProviderSnapshot) []snapshot.ProviderSnapshot {
	result := make([]snapshot.ProviderSnapshot, 0, len(providers))
	for _, provider := range providers {
		if provider.ExecutionPolicy.Enabled {
			result = append(result, provider)
		}
	}
	return result
}

func applyProviderHeaders(headers http.Header, provider snapshot.ProviderSnapshot) {
	if provider.Credentials.Kind == "bearer" && provider.Credentials.Value != "" {
		headers.Set("Authorization", "Bearer "+provider.Credentials.Value)
	} else if provider.Credentials.Kind == "api_key" && provider.Credentials.Value != "" {
		headerName := provider.Credentials.HeaderName
		if headerName == "" {
			headerName = "x-api-key"
		}
		headers.Set(headerName, provider.Credentials.Value)
	}
	for key, value := range provider.Headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		headers.Set(key, value)
	}
}

type errHealthStatus int

func (e errHealthStatus) Error() string {
	return "health check failed with status " + strconv.Itoa(int(e))
}

func (e errHealthStatus) StatusCode() int {
	return int(e)
}

func logHealthProbeError(providerID string, statusCode int, err error) {
	if err == nil {
		return
	}
	log.Printf("[gatewayd] health probe failed provider=%s status=%d err=%v", providerID, statusCode, err)
}
