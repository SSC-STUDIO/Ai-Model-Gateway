//go:build !windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/rpc"
	"path/filepath"
	"testing"
	"time"

	"ai-model-gateway/internal/contracts"
	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/contracts/telemetryingest"
	"ai-model-gateway/internal/contracts/telemetryquery"
	"ai-model-gateway/internal/core"
	gatewayapi "ai-model-gateway/internal/gateway/api"
	"ai-model-gateway/internal/gateway/snapshot"
	"ai-model-gateway/internal/proxy"
	"ai-model-gateway/internal/telemetry/eventlog"
	"ai-model-gateway/internal/telemetry/project"
	telemetryquerydb "ai-model-gateway/internal/telemetry/query"
	"ai-model-gateway/internal/testkit/fakeupstream"
)

func TestGatewaydBridgeAnthropicE2EWithLocalUpstreams(t *testing.T) {
	restoreSSRF := gatewayapi.SetSSRFCheckerForTesting(proxy.NewSSRFCheckerWithConfig(proxy.SSRFConfig{
		AllowLocalhost: true,
		AllowPrivateIP: true,
	}))
	t.Cleanup(restoreSSRF)
	restoreClient := gatewayapi.SetSharedHTTPClientForTesting(&http.Client{})
	t.Cleanup(restoreClient)

	primary := fakeupstream.New(func(req fakeupstream.CapturedRequest) fakeupstream.Response {
		return fakeupstream.Response{
			StatusCode: http.StatusTooManyRequests,
			Body:       []byte(`{"type":"error","error":{"type":"rate_limit_error","message":"primary quota exceeded"}}`),
		}
	})
	defer primary.Close()

	secondary := fakeupstream.New(func(req fakeupstream.CapturedRequest) fakeupstream.Response {
		return fakeupstream.Response{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"id":"msg_e2e","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"bridge ok"}],"stop_reason":"end_turn","usage":{"input_tokens":11,"cache_read_input_tokens":2,"output_tokens":5}}`),
		}
	})
	defer secondary.Close()

	gatewayd, telemetryPlane, controlSocket := startGatewayAndTelemetry(t)
	defer telemetryPlane.Close()

	snap := bridgeE2ESnapshot(primary.URL(), secondary.URL(), false)
	applySnapshotViaRPC(t, controlSocket, snap)
	waitForGatewayReady(t, gatewayd.httpListener.Addr().String())

	reqBody := []byte(`{"model":"public-model","messages":[{"role":"system","content":"Be terse"},{"role":"user","content":"hello"}]}`)
	resp, err := http.Post("http://"+gatewayd.httpListener.Addr().String()+"/v1/chat/completions", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	if len(primary.Requests()) != 1 || len(secondary.Requests()) != 1 {
		t.Fatalf("expected one request to each upstream, got primary=%d secondary=%d", len(primary.Requests()), len(secondary.Requests()))
	}
	if primary.Requests()[0].Path != "/v1/messages" || secondary.Requests()[0].Path != "/v1/messages" {
		t.Fatalf("expected bridged /v1/messages upstream path, got primary=%q secondary=%q", primary.Requests()[0].Path, secondary.Requests()[0].Path)
	}

	telemetryResp := waitForTelemetryResponse(t, telemetryPlane.querySocket, func(resp telemetryquery.TelemetryResponse) bool {
		return len(resp.Events) >= 1 && resp.Events[0].RequestedModel == "public-model"
	})

	event := telemetryResp.Events[0]
	if event.Provider != "secondary-anthropic" {
		t.Fatalf("provider = %q, want secondary-anthropic", event.Provider)
	}
	if event.RouteMode != "bridged" {
		t.Fatalf("route_mode = %q, want bridged", event.RouteMode)
	}
	if event.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", event.Attempts)
	}
	if event.CachedPromptTokens != 2 {
		t.Fatalf("cached_prompt_tokens = %d, want 2", event.CachedPromptTokens)
	}
	if telemetryResp.Pricing.Summary.ExactTotalUsd <= 0 {
		t.Fatalf("expected exact_total_usd > 0, got %f", telemetryResp.Pricing.Summary.ExactTotalUsd)
	}
}

func TestGatewaydResponseCacheE2E(t *testing.T) {
	restoreSSRF := gatewayapi.SetSSRFCheckerForTesting(proxy.NewSSRFCheckerWithConfig(proxy.SSRFConfig{
		AllowLocalhost: true,
		AllowPrivateIP: true,
	}))
	t.Cleanup(restoreSSRF)
	restoreClient := gatewayapi.SetSharedHTTPClientForTesting(&http.Client{})
	t.Cleanup(restoreClient)

	upstream := fakeupstream.New(func(req fakeupstream.CapturedRequest) fakeupstream.Response {
		return fakeupstream.Response{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"id":"msg_cache","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"cached ok"}],"stop_reason":"end_turn","usage":{"input_tokens":8,"cache_read_input_tokens":1,"output_tokens":4}}`),
		}
	})
	defer upstream.Close()

	gatewayd, telemetryPlane, controlSocket := startGatewayAndTelemetry(t)
	defer telemetryPlane.Close()

	snap := bridgeE2ESnapshot(upstream.URL(), "", true)
	applySnapshotViaRPC(t, controlSocket, snap)
	waitForGatewayReady(t, gatewayd.httpListener.Addr().String())

	reqBody := []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`)
	firstResp, err := http.Post("http://"+gatewayd.httpListener.Addr().String()+"/v1/chat/completions", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("first request: %v", err)
	}
	firstResp.Body.Close()
	if firstResp.StatusCode != http.StatusOK {
		t.Fatalf("first status = %d, want 200", firstResp.StatusCode)
	}

	secondResp, err := http.Post("http://"+gatewayd.httpListener.Addr().String()+"/v1/chat/completions", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("second request: %v", err)
	}
	secondResp.Body.Close()
	if secondResp.StatusCode != http.StatusOK {
		t.Fatalf("second status = %d, want 200", secondResp.StatusCode)
	}
	if secondResp.Header.Get("X-Cache") != "HIT" {
		t.Fatalf("expected X-Cache HIT, got %q", secondResp.Header.Get("X-Cache"))
	}

	if len(upstream.Requests()) != 1 {
		t.Fatalf("expected one upstream request after cache hit, got %d", len(upstream.Requests()))
	}

	telemetryResp := waitForTelemetryResponse(t, telemetryPlane.querySocket, func(resp telemetryquery.TelemetryResponse) bool {
		return len(resp.Events) >= 2
	})

	cacheSeen := false
	for _, event := range telemetryResp.Events {
		if event.RouteMode == "cache" {
			cacheSeen = true
		}
	}
	if !cacheSeen {
		t.Fatalf("expected one cache telemetry event, got %+v", telemetryResp.Events)
	}
}

type testTelemetryPlane struct {
	ingestSocket string
	querySocket  string
	eventLog     *eventlog.EventLog
	queryStore   *telemetryquerydb.Store
	projector    *project.Projector
	ingestListen contracts.Listener
	queryListen  contracts.Listener
}

func startGatewayAndTelemetry(t *testing.T) (*Daemon, *testTelemetryPlane, string) {
	t.Helper()

	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())

	telemetryPlane := startTestTelemetryPlane(t, ctx, dir)

	gatewayCfg := Config{
		Listen:           "127.0.0.1:0",
		ControlSocket:    filepath.Join(dir, "gateway-control.sock"),
		TelemetrySocket:  telemetryPlane.ingestSocket,
		DataDir:          filepath.Join(dir, "gateway"),
		RPCRetryCount:    10,
		RPCRetryInterval: 1,
	}
	gatewayd, err := NewDaemon(gatewayCfg)
	if err != nil {
		t.Fatalf("new gatewayd: %v", err)
	}
	if err := gatewayd.Start(ctx); err != nil {
		t.Fatalf("start gatewayd: %v", err)
	}

	t.Cleanup(func() {
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = gatewayd.Shutdown(shutdownCtx)
	})

	waitForTelemetryQueryReady(t, telemetryPlane.querySocket)
	return gatewayd, telemetryPlane, gatewayCfg.ControlSocket
}

func startTestTelemetryPlane(t *testing.T, ctx context.Context, dir string) *testTelemetryPlane {
	t.Helper()

	plane := &testTelemetryPlane{
		ingestSocket: filepath.Join(dir, "telemetry-ingest.sock"),
		querySocket:  filepath.Join(dir, "telemetry-query.sock"),
	}

	var err error
	plane.eventLog, err = eventlog.New(filepath.Join(dir, "telemetry", "events.db"))
	if err != nil {
		t.Fatalf("new event log: %v", err)
	}
	plane.queryStore, err = telemetryquerydb.NewStore(filepath.Join(dir, "telemetry", "query.db"))
	if err != nil {
		t.Fatalf("new query store: %v", err)
	}
	plane.projector = project.NewProjector(plane.eventLog, plane.queryStore)
	go plane.projector.Run(ctx)

	plane.ingestListen, err = contracts.DefaultTransport.Listen(plane.ingestSocket)
	if err != nil {
		t.Fatalf("listen ingest socket: %v", err)
	}
	plane.queryListen, err = contracts.DefaultTransport.Listen(plane.querySocket)
	if err != nil {
		t.Fatalf("listen query socket: %v", err)
	}

	ingestRPC := rpc.NewServer()
	if err := ingestRPC.RegisterName("TelemetryIngestRPC", &testTelemetryIngestRPC{plane: plane}); err != nil {
		t.Fatalf("register ingest rpc: %v", err)
	}
	queryRPC := rpc.NewServer()
	if err := queryRPC.RegisterName("TelemetryQueryRPC", &testTelemetryQueryRPC{plane: plane}); err != nil {
		t.Fatalf("register query rpc: %v", err)
	}

	go serveRPCListener(ctx, plane.ingestListen, ingestRPC)
	go serveRPCListener(ctx, plane.queryListen, queryRPC)

	t.Cleanup(func() {
		plane.Close()
	})

	return plane
}

func (p *testTelemetryPlane) Close() {
	if p == nil {
		return
	}
	if p.ingestListen != nil {
		_ = p.ingestListen.Close()
	}
	if p.queryListen != nil {
		_ = p.queryListen.Close()
	}
	if p.queryStore != nil {
		_ = p.queryStore.Close()
	}
	if p.eventLog != nil {
		_ = p.eventLog.Close()
	}
}

type testTelemetryIngestRPC struct {
	plane *testTelemetryPlane
}

func (r *testTelemetryIngestRPC) AppendBatch(req telemetryingest.AppendBatchRequest, resp *telemetryingest.AppendBatchResponse) error {
	accepted, dropped, err := r.plane.eventLog.Append(req.Events)
	resp.Accepted = accepted
	resp.Dropped = dropped
	if err != nil {
		resp.Error = err.Error()
	}
	return err
}

func (r *testTelemetryIngestRPC) Flush(req telemetryingest.FlushRequest, resp *telemetryingest.FlushResponse) error {
	resp.Success = true
	return nil
}

func (r *testTelemetryIngestRPC) Ping(req telemetryingest.PingRequest, resp *telemetryingest.PingResponse) error {
	resp.Healthy = true
	resp.ServerTime = time.Now()
	return nil
}

type testTelemetryQueryRPC struct {
	plane *testTelemetryPlane
}

func (r *testTelemetryQueryRPC) GetTelemetry(req telemetryquery.TelemetryRequest, resp *telemetryquery.TelemetryResponse) error {
	events, total, windowHours, err := r.plane.queryStore.QueryTelemetry(req)
	if err != nil {
		return err
	}
	resp.Events = events
	resp.Total = total
	resp.WindowHours = windowHours
	resp.Pricing = r.plane.queryStore.QueryPricingEconomics(windowHours)
	return nil
}

func (r *testTelemetryQueryRPC) Ping(req telemetryquery.PingRequest, resp *telemetryquery.PingResponse) error {
	resp.Healthy = true
	resp.ServerTime = time.Now()
	return nil
}

func serveRPCListener(ctx context.Context, listener contracts.Listener, server *rpc.Server) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}
		go server.ServeConn(&connAdapter{conn: conn})
	}
}

func applySnapshotViaRPC(t *testing.T, controlSocket string, snap *snapshot.Snapshot) {
	t.Helper()

	conn, err := contracts.DefaultTransport.Dial(controlSocket)
	if err != nil {
		t.Fatalf("dial gateway control socket: %v", err)
	}
	defer conn.Close()

	client := rpc.NewClient(&connAdapter{conn: conn})
	defer client.Close()

	snapBytes, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}

	req := gatewaycontrol.ApplySnapshotRequest{
		SnapshotID:    snap.Meta.SnapshotID,
		RevisionID:    snap.Meta.RevisionID,
		SnapshotBytes: snapBytes,
		SchemaVersion: snap.Meta.SchemaVersion,
		GeneratedAt:   snap.Meta.GeneratedAt,
	}
	var resp gatewaycontrol.ApplySnapshotResponse
	if err := client.Call("GatewayControlRPC.ApplySnapshot", req, &resp); err != nil {
		t.Fatalf("apply snapshot rpc: %v", err)
	}
	if !resp.Applied {
		t.Fatalf("snapshot not applied: %s", resp.Error)
	}
}

func waitForTelemetryQueryReady(t *testing.T, querySocket string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := contracts.DefaultTransport.Dial(querySocket)
		if err == nil {
			client := rpc.NewClient(&connAdapter{conn: conn})
			var resp telemetryquery.PingResponse
			callErr := client.Call("TelemetryQueryRPC.Ping", telemetryquery.PingRequest{}, &resp)
			_ = client.Close()
			_ = conn.Close()
			if callErr == nil && resp.Healthy {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("telemetry query socket %q not ready", querySocket)
}

func waitForGatewayReady(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	url := "http://" + addr + "/-/health"
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("gateway %q not ready", addr)
}

func waitForTelemetryResponse(t *testing.T, querySocket string, predicate func(telemetryquery.TelemetryResponse) bool) telemetryquery.TelemetryResponse {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		resp := queryTelemetry(t, querySocket)
		if predicate(resp) {
			return resp
		}
		time.Sleep(100 * time.Millisecond)
	}
	return queryTelemetry(t, querySocket)
}

func queryTelemetry(t *testing.T, querySocket string) telemetryquery.TelemetryResponse {
	t.Helper()
	conn, err := contracts.DefaultTransport.Dial(querySocket)
	if err != nil {
		t.Fatalf("dial telemetry query socket: %v", err)
	}
	defer conn.Close()

	client := rpc.NewClient(&connAdapter{conn: conn})
	defer client.Close()

	req := telemetryquery.TelemetryRequest{WindowHours: 1, Limit: 20}
	var resp telemetryquery.TelemetryResponse
	if err := client.Call("TelemetryQueryRPC.GetTelemetry", req, &resp); err != nil {
		t.Fatalf("query telemetry rpc: %v", err)
	}
	return resp
}

func bridgeE2ESnapshot(primaryURL string, secondaryURL string, cacheEnabled bool) *snapshot.Snapshot {
	providers := []snapshot.ProviderSnapshot{
		{
			ProviderID:       "primary-anthropic",
			ProtocolAdapter:  core.ProtocolAdapterAnthropicMessages,
			BaseURL:          primaryURL,
			AnthropicBaseURL: primaryURL,
			Credentials: snapshot.Credentials{
				Kind:       "api_key",
				Value:      "test-key",
				HeaderName: "x-api-key",
			},
			ModelTable: []snapshot.ModelMapping{
				{PublicModel: "public-model", UpstreamModel: "claude-sonnet-4-6"},
			},
			ExecutionPolicy: snapshot.ExecutionPolicy{
				Enabled:   true,
				Weight:    1,
				TimeoutMs: 5000,
			},
		},
	}
	if secondaryURL != "" {
		providers = append(providers, snapshot.ProviderSnapshot{
			ProviderID:       "secondary-anthropic",
			ProtocolAdapter:  core.ProtocolAdapterAnthropicMessages,
			BaseURL:          secondaryURL,
			AnthropicBaseURL: secondaryURL,
			Credentials: snapshot.Credentials{
				Kind:       "api_key",
				Value:      "test-key",
				HeaderName: "x-api-key",
			},
			ModelTable: []snapshot.ModelMapping{
				{PublicModel: "public-model", UpstreamModel: "claude-sonnet-4-6"},
			},
			ExecutionPolicy: snapshot.ExecutionPolicy{
				Enabled:   true,
				Weight:    1,
				TimeoutMs: 5000,
			},
		})
	}

	return &snapshot.Snapshot{
		Meta: snapshot.SnapshotMeta{
			SnapshotID:    "snap-bridge-e2e",
			SchemaVersion: snapshot.CurrentSchemaVersion,
			RevisionID:    "rev-bridge-e2e",
			GeneratedAt:   time.Now().UTC(),
		},
		Ingress: snapshot.IngressConfig{
			Listen:       "127.0.0.1:0",
			MaxBodyBytes: 1 << 20,
		},
		RoutingPolicy: snapshot.RoutingPolicy{
			MaxRetries: 1,
			Retry: snapshot.RetryPolicy{
				StatusCodes: []int{http.StatusTooManyRequests},
			},
			Cache: snapshot.CacheConfig{
				Enabled:    cacheEnabled,
				MaxEntries: 64,
				TTLSec:     300,
			},
		},
		Providers: providers,
		Pricing: snapshot.PricingConfig{
			ManualPrices: []snapshot.PricingManualPrice{
				{
					Model:            "claude-sonnet-4-6",
					Currency:         "USD",
					InputPer1M:       1,
					CachedInputPer1M: 0.25,
					OutputPer1M:      2,
					Enabled:          true,
					Source:           "manual",
				},
			},
		},
	}
}
