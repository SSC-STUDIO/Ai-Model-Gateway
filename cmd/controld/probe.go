package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/control/api"
	"ai-model-gateway/internal/core"
)

type probeRunnerAdapter struct {
	daemon *Daemon
}

func (a probeRunnerAdapter) ProbeProvider(ctx context.Context, req api.ProbeRequest) (*api.ProbeResult, error) {
	return a.probe(ctx, req, true)
}

func (a probeRunnerAdapter) ProbeModel(ctx context.Context, req api.ProbeRequest) (*api.ProbeResult, error) {
	return a.probe(ctx, req, false)
}

func (a probeRunnerAdapter) probe(ctx context.Context, req api.ProbeRequest, providerRequired bool) (*api.ProbeResult, error) {
	if a.daemon == nil {
		return nil, fmt.Errorf("daemon not configured")
	}
	client := a.daemon.currentGatewayRPC()
	if client == nil {
		return nil, fmt.Errorf("gateway not connected")
	}
	cfg, err := a.daemon.publisher.GetCurrentConfig()
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, fmt.Errorf("active config not found")
	}
	providerID := strings.TrimSpace(req.ProviderID)
	model := strings.TrimSpace(req.Model)
	if providerRequired && providerID == "" {
		return nil, fmt.Errorf("provider_id is required")
	}
	provider, publicModel := selectProbeTarget(cfg, providerID, model)
	if provider == nil {
		return nil, fmt.Errorf("probe target not found")
	}
	if publicModel == "" {
		return nil, fmt.Errorf("probe model not found")
	}
	protocol := strings.TrimSpace(req.Protocol)
	if protocol == "" {
		protocol = core.NormalizeProtocolAdapter(provider.ProtocolAdapter, provider.AnthropicBaseURL)
	}
	body, err := probeRequestBody(protocol, publicModel, req.Prompt)
	if err != nil {
		return nil, err
	}
	timeoutMs := req.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = provider.TimeoutMs
	}
	if timeoutMs <= 0 {
		timeoutMs = 30000
	}
	rpcReq := gatewaycontrol.RunBenchmarkCaseRequest{
		RunID:           "diagnostic-probe",
		CaseID:          "manual",
		ProviderID:      provider.Name,
		PublicModel:     publicModel,
		Protocol:        protocol,
		RequestBody:     body,
		Headers:         req.Headers,
		TimeoutMs:       timeoutMs,
		SyntheticKind:   "probe",
		DisableCache:    true,
		DisableFallback: true,
		DisableRetries:  true,
	}
	resp, err := runGatewayProbeWithContext(ctx, client, rpcReq)
	if err != nil {
		return nil, err
	}
	result := &api.ProbeResult{
		Diagnostic:      true,
		ProviderID:      provider.Name,
		Model:           publicModel,
		StatusCode:      resp.StatusCode,
		LatencyMs:       resp.LatencyMs,
		Healthy:         resp.Error == "" && resp.StatusCode >= 200 && resp.StatusCode < 500,
		Error:           resp.Error,
		ResponseExcerpt: responseExcerpt(resp.ResponseBody),
		Headers:         firstHeaderValues(resp.Headers),
		ProbedAt:        time.Now().UTC(),
	}
	return result, nil
}

func selectProbeTarget(cfg *core.Config, providerID string, model string) (*core.Provider, string) {
	if cfg == nil {
		return nil, ""
	}
	providerID = strings.TrimSpace(providerID)
	model = strings.TrimSpace(model)
	for i := range cfg.Providers {
		provider := &cfg.Providers[i]
		if !provider.IsEnabled() {
			continue
		}
		if providerID != "" && !strings.EqualFold(provider.Name, providerID) {
			continue
		}
		for _, candidate := range provider.Models {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				continue
			}
			if model == "" || strings.EqualFold(candidate, model) {
				return provider, candidate
			}
		}
		if model == "" {
			return provider, ""
		}
	}
	return nil, ""
}

func probeRequestBody(protocol string, model string, prompt string) ([]byte, error) {
	if strings.TrimSpace(prompt) == "" {
		prompt = "Reply with exactly ok."
	}
	switch core.NormalizeProtocolAdapter(protocol, "") {
	case core.ProtocolAdapterAnthropicMessages:
		return json.Marshal(map[string]any{
			"model":      model,
			"max_tokens": 8,
			"messages": []map[string]string{
				{"role": "user", "content": prompt},
			},
		})
	default:
		return json.Marshal(map[string]any{
			"model":       model,
			"max_tokens":  8,
			"temperature": 0,
			"messages": []map[string]string{
				{"role": "user", "content": prompt},
			},
		})
	}
}

func runGatewayProbeWithContext(ctx context.Context, client *GatewayClient, req gatewaycontrol.RunBenchmarkCaseRequest) (*gatewaycontrol.RunBenchmarkCaseResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	done := make(chan struct {
		resp *gatewaycontrol.RunBenchmarkCaseResponse
		err  error
	}, 1)
	go func() {
		resp, err := client.RunBenchmarkCase(req)
		done <- struct {
			resp *gatewaycontrol.RunBenchmarkCaseResponse
			err  error
		}{resp: resp, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-done:
		return result.resp, result.err
	}
}

func responseExcerpt(body []byte) string {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return ""
	}
	if len(body) > 512 {
		body = body[:512]
	}
	return string(body)
}

func firstHeaderValues(headers map[string][]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for key, values := range headers {
		if len(values) == 0 {
			continue
		}
		out[key] = values[0]
	}
	return out
}
