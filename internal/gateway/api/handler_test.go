package api

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"ai-model-gateway/internal/contracts/telemetryingest"
	"ai-model-gateway/internal/gateway/snapshot"
)

const (
	testPublicModel   = "public-model"
	testUpstreamModel = "upstream-model"
	testUpstreamURL   = "http://203.0.113.1"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestHandleChatCompletionStreamsSSEWithoutBuffering(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)
	routingSequence.Store(0)

	const firstChunk = "data: first\n\n"
	const secondChunk = "data: second\n\n"

	firstChunkWritten := make(chan struct{})
	allowSecondChunk := make(chan struct{})
	forwardBodyCh := make(chan []byte, 1)

	var releaseSecondChunk sync.Once
	t.Cleanup(func() {
		releaseSecondChunk.Do(func() {
			close(allowSecondChunk)
		})
	})

	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/v1/chat/completions" {
				return nil, fmt.Errorf("unexpected upstream path %q", req.URL.Path)
			}

			body, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			forwardBodyCh <- body

			pr, pw := io.Pipe()
			go func() {
				_, _ = io.WriteString(pw, firstChunk)
				close(firstChunkWritten)
				<-allowSecondChunk
				_, _ = io.WriteString(pw, secondChunk)
				_ = pw.Close()
			}()

			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream; charset=utf-8"},
				},
				Body: pr,
			}, nil
		}),
	})

	server := newGatewayTestServer(t, nil, nil)
	defer server.Close()

	reqBody := `{"model":"public-model","messages":[{"role":"user","content":"hello"}],"stream":true}`
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL+"/v1/chat/completions", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "gatewayd-test")

	respCh := make(chan *http.Response, 1)
	errCh := make(chan error, 1)
	go func() {
		resp, err := server.Client().Do(req)
		if err != nil {
			errCh <- err
			return
		}
		respCh <- resp
	}()

	select {
	case <-firstChunkWritten:
	case err := <-errCh:
		t.Fatalf("request failed before streaming started: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("upstream stream did not produce the first chunk")
	}

	var resp *http.Response
	select {
	case err := <-errCh:
		t.Fatalf("request failed: %v", err)
	case resp = <-respCh:
	case <-time.After(1 * time.Second):
		t.Fatal("gateway response was not available before the upstream stream completed")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("expected event-stream response, got %q", got)
	}

	gotForwardBody := <-forwardBodyCh
	if !bytes.Contains(gotForwardBody, []byte(`"model":"`+testUpstreamModel+`"`)) {
		t.Fatalf("expected upstream model rewrite in forwarded body, got %s", gotForwardBody)
	}
	if bytes.Contains(gotForwardBody, []byte(`"model":"`+testPublicModel+`"`)) {
		t.Fatalf("forwarded body still contains public model: %s", gotForwardBody)
	}

	first := make([]byte, len(firstChunk))
	if _, err := io.ReadFull(resp.Body, first); err != nil {
		t.Fatalf("read first streamed chunk: %v", err)
	}
	if got := string(first); got != firstChunk {
		t.Fatalf("unexpected first streamed chunk %q", got)
	}

	releaseSecondChunk.Do(func() {
		close(allowSecondChunk)
	})

	rest, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read remaining stream: %v", err)
	}
	if got := string(rest); got != secondChunk {
		t.Fatalf("unexpected remaining streamed chunk %q", got)
	}
}

func TestHandleChatCompletionReturnsJSONForNonStreamingRequest(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)
	routingSequence.Store(0)

	const upstreamBody = `{"id":"cmpl-123","object":"chat.completion","model":"upstream-model","choices":[],"usage":{"prompt_tokens":11,"completion_tokens":7}}`

	forwardBodyCh := make(chan []byte, 1)

	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/v1/chat/completions" {
				return nil, fmt.Errorf("unexpected upstream path %q", req.URL.Path)
			}

			body, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			forwardBodyCh <- body

			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: io.NopCloser(strings.NewReader(upstreamBody)),
			}, nil
		}),
	})

	server := newGatewayTestServer(t, nil, nil)
	defer server.Close()

	reqBody := `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`
	resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("expected JSON response, got %q", got)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if got := string(body); got != upstreamBody {
		t.Fatalf("unexpected response body %q", got)
	}

	gotForwardBody := <-forwardBodyCh
	if !bytes.Contains(gotForwardBody, []byte(`"model":"`+testUpstreamModel+`"`)) {
		t.Fatalf("expected upstream model rewrite in forwarded body, got %s", gotForwardBody)
	}
	if bytes.Contains(gotForwardBody, []byte(`"model":"`+testPublicModel+`"`)) {
		t.Fatalf("forwarded body still contains public model: %s", gotForwardBody)
	}
}

func TestHandleChatCompletionAcceptsStructuredMessageContent(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)
	routingSequence.Store(0)

	forwardBodyCh := make(chan []byte, 1)

	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			forwardBodyCh <- body
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: io.NopCloser(strings.NewReader(`{"choices":[]}`)),
			}, nil
		}),
	})

	server := newGatewayTestServer(t, testGatewaySnapshot(), nil)
	defer server.Close()

	reqBody := `{"model":"public-model","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`
	resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	gotForwardBody := <-forwardBodyCh
	if !bytes.Contains(gotForwardBody, []byte(`"content":[{"type":"text","text":"hello"}]`)) {
		t.Fatalf("structured content was not preserved: %s", gotForwardBody)
	}
	if !bytes.Contains(gotForwardBody, []byte(`"model":"`+testUpstreamModel+`"`)) {
		t.Fatalf("expected upstream model rewrite in forwarded body, got %s", gotForwardBody)
	}
}

func TestHandleChatCompletionRetriesAcrossProvidersOnRetryableStatus(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)
	routingSequence.Store(0)

	var upstreamHosts []string
	tel := &capturingTelemetryEmitter{events: make(chan telemetryingest.Event, 1)}

	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			upstreamHosts = append(upstreamHosts, req.URL.Host)
			switch req.URL.Host {
			case "203.0.113.10":
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header: http.Header{
						"Content-Type": []string{"application/json"},
					},
					Body: io.NopCloser(strings.NewReader(`{"error":{"message":"quota exceeded"}}`)),
				}, nil
			case "203.0.113.11":
				return &http.Response{
					StatusCode: http.StatusOK,
					Header: http.Header{
						"Content-Type": []string{"application/json"},
					},
					Body: io.NopCloser(strings.NewReader(`{"id":"cmpl-456","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":4}}`)),
				}, nil
			default:
				return nil, fmt.Errorf("unexpected upstream host %q", req.URL.Host)
			}
		}),
	})

	snap := testGatewaySnapshot()
	snap.RoutingPolicy.MaxRetries = 2
	snap.Providers = []snapshot.ProviderSnapshot{
		{
			ProviderID: "provider-a",
			BaseURL:    "http://203.0.113.10",
			ModelTable: []snapshot.ModelMapping{
				{PublicModel: testPublicModel, UpstreamModel: "provider-a-model"},
			},
			ExecutionPolicy: snapshot.ExecutionPolicy{
				Enabled:   true,
				Weight:    1,
				TimeoutMs: 5000,
			},
		},
		{
			ProviderID: "provider-b",
			BaseURL:    "http://203.0.113.11",
			ModelTable: []snapshot.ModelMapping{
				{PublicModel: testPublicModel, UpstreamModel: "provider-b-model"},
			},
			ExecutionPolicy: snapshot.ExecutionPolicy{
				Enabled:   true,
				Weight:    1,
				TimeoutMs: 5000,
			},
		},
	}

	server := newGatewayTestServer(t, snap, tel)
	defer server.Close()

	reqBody := `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`
	resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if got := strings.Join(upstreamHosts, ","); got != "203.0.113.10,203.0.113.11" {
		t.Fatalf("unexpected upstream attempt order %q", got)
	}

	select {
	case event := <-tel.events:
		if event.Payload.ProviderID != "provider-b" {
			t.Fatalf("expected telemetry provider-b, got %q", event.Payload.ProviderID)
		}
		if event.Payload.Attempts != 2 {
			t.Fatalf("expected 2 attempts, got %d", event.Payload.Attempts)
		}
		if event.Payload.RouteMode != "weighted_failover" {
			t.Fatalf("expected weighted_failover route mode, got %q", event.Payload.RouteMode)
		}
		if event.Payload.EffectiveModel != "provider-b-model" {
			t.Fatalf("expected effective model provider-b-model, got %q", event.Payload.EffectiveModel)
		}
	case <-time.After(time.Second):
		t.Fatal("expected telemetry event")
	}
}

func TestHandleChatCompletionInfiniteRetryOnRetryable429UntilSuccess(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)
	routingSequence.Store(0)

	attempts := 0
	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			if attempts < 3 {
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header: http.Header{
						"Content-Type": []string{"application/json"},
					},
					Body: io.NopCloser(strings.NewReader(`{"error":{"message":"concurrency limit exceeded"}}`)),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: io.NopCloser(strings.NewReader(`{"id":"cmpl-ok","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":3}}`)),
			}, nil
		}),
	})

	snap := testGatewaySnapshot()
	snap.RoutingPolicy.MaxRetries = 0
	snap.RoutingPolicy.Retry.InfiniteOnError = true
	snap.RoutingPolicy.RetryBackoff = snapshot.RetryBackoff{InitialMs: 1, MaxMs: 1}

	server := newGatewayTestServer(t, snap, nil)
	defer server.Close()

	resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 upstream attempts, got %d", attempts)
	}
}

func TestHandleChatCompletionInfiniteRetryOnGatewayTimeoutUntilSuccess(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)
	routingSequence.Store(0)

	attempts := 0
	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			if attempts < 3 {
				return nil, context.DeadlineExceeded
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: io.NopCloser(strings.NewReader(`{"id":"cmpl-ok","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":3}}`)),
			}, nil
		}),
	})

	snap := testGatewaySnapshot()
	snap.RoutingPolicy.MaxRetries = 0
	snap.RoutingPolicy.Retry.InfiniteOnError = true
	snap.RoutingPolicy.RetryBackoff = snapshot.RetryBackoff{InitialMs: 1, MaxMs: 1}

	server := newGatewayTestServer(t, snap, nil)
	defer server.Close()

	resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 upstream attempts, got %d", attempts)
	}
}

func TestHandleChatCompletionInfiniteRetryAllErrorsRetriesNonRetryable4xxUntilSuccess(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)
	routingSequence.Store(0)

	attempts := 0
	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			if attempts < 3 {
				return &http.Response{
					StatusCode: http.StatusTeapot,
					Header: http.Header{
						"Content-Type": []string{"application/json"},
					},
					Body: io.NopCloser(strings.NewReader(`{"error":{"message":"short and stout"}}`)),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: io.NopCloser(strings.NewReader(`{"id":"cmpl-ok","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":3}}`)),
			}, nil
		}),
	})

	snap := testGatewaySnapshot()
	snap.RoutingPolicy.MaxRetries = 0
	snap.RoutingPolicy.Retry.InfiniteOnError = true
	snap.RoutingPolicy.Retry.AllErrors = true
	snap.RoutingPolicy.RetryBackoff = snapshot.RetryBackoff{InitialMs: 1, MaxMs: 1}

	server := newGatewayTestServer(t, snap, nil)
	defer server.Close()

	resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 upstream attempts, got %d", attempts)
	}
}

func TestHandleChatCompletionStreamInfiniteRetryStartsSSEBeforeUpstreamSuccess(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)
	routingSequence.Store(0)

	previousInterval := streamRetryHeartbeatInterval
	streamRetryHeartbeatInterval = 10 * time.Millisecond
	t.Cleanup(func() {
		streamRetryHeartbeatInterval = previousInterval
	})

	attempts := 0
	allowSuccess := make(chan struct{})
	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			if attempts < 3 {
				time.Sleep(15 * time.Millisecond)
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header: http.Header{
						"Content-Type": []string{"application/json"},
					},
					Body: io.NopCloser(strings.NewReader(`{"error":{"message":"concurrency limit exceeded"}}`)),
				}, nil
			}

			<-allowSuccess
			pr, pw := io.Pipe()
			go func() {
				_, _ = io.WriteString(pw, "data: final\n\n")
				_ = pw.Close()
			}()
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream"},
				},
				Body: pr,
			}, nil
		}),
	})

	snap := testGatewaySnapshot()
	snap.RoutingPolicy.MaxRetries = 0
	snap.RoutingPolicy.Retry.InfiniteOnError = true
	snap.RoutingPolicy.Retry.AllErrors = true
	snap.RoutingPolicy.RetryBackoff = snapshot.RetryBackoff{InitialMs: 1, MaxMs: 1}

	server := newGatewayTestServer(t, snap, nil)
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"stream":true}`))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	respCh := make(chan *http.Response, 1)
	errCh := make(chan error, 1)
	go func() {
		resp, err := server.Client().Do(req)
		if err != nil {
			errCh <- err
			return
		}
		respCh <- resp
	}()

	var resp *http.Response
	select {
	case err := <-errCh:
		t.Fatalf("request failed: %v", err)
	case resp = <-respCh:
	case <-time.After(time.Second):
		t.Fatal("expected streaming response headers before upstream success")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	reader := bufio.NewReader(resp.Body)
	firstLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read initial heartbeat line: %v", err)
	}
	if !strings.HasPrefix(firstLine, ":") {
		t.Fatalf("expected initial SSE comment heartbeat, got %q", firstLine)
	}

	close(allowSuccess)

	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read remaining stream: %v", err)
	}
	if !strings.Contains(string(rest), "data: final") {
		t.Fatalf("expected final streamed data, got %q", string(rest))
	}
}

func TestHandleChatCompletionInfiniteRetryStopsOnContextCancel(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)
	routingSequence.Store(0)

	attempts := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			if attempts == 3 {
				cancel()
			}
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: io.NopCloser(strings.NewReader(`{"error":{"message":"concurrency limit exceeded"}}`)),
			}, nil
		}),
	})

	snap := testGatewaySnapshot()
	snap.RoutingPolicy.MaxRetries = 0
	snap.RoutingPolicy.Retry.InfiniteOnError = true
	snap.RoutingPolicy.RetryBackoff = snapshot.RetryBackoff{InitialMs: 1, MaxMs: 1}

	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()

	HandleChatCompletion(ctx, snap, NewRuntimeState(), nil, nil, rec, req.WithContext(ctx))
	resp := rec.Result()
	defer resp.Body.Close()

	if attempts != 3 {
		t.Fatalf("expected retries to stop after context cancellation, got %d attempts", attempts)
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected final status 429 after cancellation, got %d", resp.StatusCode)
	}
}

func TestHandleChatCompletionRotatesAPIKeysAfter401(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)
	routingSequence.Store(0)

	var authHeaders []string
	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			auth := req.Header.Get("Authorization")
			authHeaders = append(authHeaders, auth)
			if !strings.Contains(auth, "good-key") {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Header: http.Header{
						"Content-Type": []string{"application/json"},
					},
					Body: io.NopCloser(strings.NewReader(`{"error":{"message":"invalid"}}`)),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: io.NopCloser(strings.NewReader(`{"id":"cmpl-789","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":2}}`)),
			}, nil
		}),
	})

	snap := testGatewaySnapshot()
	snap.Providers[0].BaseURL = "http://203.0.113.10"
	snap.Providers[0].Credentials = snapshot.Credentials{Kind: "bearer"}
	snap.Providers[0].APIKeys = []snapshot.APIKey{
		{Name: "a", Value: "bad-key"},
		{Name: "b", Value: "good-key"},
	}
	snap.Providers[0].ExecutionPolicy.SameRetries = 3
	snap.RoutingPolicy.MaxRetries = 6
	snap.RoutingPolicy.Retry.StatusCodes = append(append([]int(nil), snap.RoutingPolicy.Retry.StatusCodes...), http.StatusUnauthorized)

	state := NewRuntimeState()
	state.ApplySnapshot(snap)
	server := newGatewayTestServerWithState(t, snap, nil, state)
	defer server.Close()

	reqBody := `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`
	resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if len(authHeaders) < 2 {
		t.Fatalf("expected multiple upstream attempts for key rotation, got %d: %v", len(authHeaders), authHeaders)
	}
	last := authHeaders[len(authHeaders)-1]
	if !strings.Contains(last, "good-key") {
		t.Fatalf("expected final attempt to use good key, got %q", last)
	}
}

func TestHandleChatCompletionExtractsStreamingUsageForTelemetry(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)
	routingSequence.Store(0)

	tel := &capturingTelemetryEmitter{events: make(chan telemetryingest.Event, 1)}

	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			pr, pw := io.Pipe()
			go func() {
				_, _ = io.WriteString(pw, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
				_, _ = io.WriteString(pw, "data: {\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":7,\"prompt_tokens_details\":{\"cached_tokens\":2}}}\n\n")
				_, _ = io.WriteString(pw, "data: [DONE]\n\n")
				_ = pw.Close()
			}()
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream"},
				},
				Body: pr,
			}, nil
		}),
	})

	server := newGatewayTestServer(t, testGatewaySnapshot(), tel)
	defer server.Close()

	reqBody := `{"model":"public-model","messages":[{"role":"user","content":"hello"}],"stream":true}`
	resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read streamed body: %v", err)
	}
	if !bytes.Contains(body, []byte(`"prompt_tokens":5`)) {
		t.Fatalf("expected streamed usage chunk in body, got %s", body)
	}

	select {
	case event := <-tel.events:
		if event.Payload.PromptTokens != 5 || event.Payload.CachedPromptTokens != 2 || event.Payload.CompletionTokens != 7 {
			t.Fatalf("unexpected telemetry usage: %+v", event.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("expected telemetry event")
	}
}

func TestHandleChatCompletionKeepsStickySessionAffinity(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)
	routingSequence.Store(0)

	var upstreamHosts []string
	state := NewRuntimeState()

	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			upstreamHosts = append(upstreamHosts, req.URL.Host)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: io.NopCloser(strings.NewReader(`{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)),
			}, nil
		}),
	})

	snap := testGatewaySnapshot()
	snap.RoutingPolicy.StickySessions = snapshot.StickySessionConfig{
		Enabled: true,
		TTLSec:  300,
	}
	snap.Providers = []snapshot.ProviderSnapshot{
		{
			ProviderID: "provider-a",
			BaseURL:    "http://203.0.113.10",
			ModelTable: []snapshot.ModelMapping{
				{PublicModel: testPublicModel, UpstreamModel: "provider-a-model"},
			},
			ExecutionPolicy: snapshot.ExecutionPolicy{
				Enabled:   true,
				Weight:    1,
				TimeoutMs: 5000,
			},
		},
		{
			ProviderID: "provider-b",
			BaseURL:    "http://203.0.113.11",
			ModelTable: []snapshot.ModelMapping{
				{PublicModel: testPublicModel, UpstreamModel: "provider-b-model"},
			},
			ExecutionPolicy: snapshot.ExecutionPolicy{
				Enabled:   true,
				Weight:    1,
				TimeoutMs: 5000,
			},
		},
	}

	server := newGatewayTestServerWithState(t, snap, nil, state)
	defer server.Close()

	for range 2 {
		reqBody := `{"model":"public-model","user":"tenant-a","messages":[{"role":"user","content":"hello"}]}`
		resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
		if err != nil {
			t.Fatalf("post request: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}
	}

	if got := strings.Join(upstreamHosts, ","); got != "203.0.113.10,203.0.113.10" {
		t.Fatalf("unexpected upstream sticky order %q", got)
	}
}

func TestHandleChatCompletionAllowsPassthroughAfterConfiguredDelay(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)
	routingSequence.Store(0)

	currentTime := time.Unix(1_712_345_678, 0).UTC()
	state := NewRuntimeState()
	state.now = func() time.Time { return currentTime }

	attempts := 0
	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header: http.Header{
						"Content-Type": []string{"application/json"},
					},
					Body: io.NopCloser(strings.NewReader(`{"error":{"message":"quota exceeded"}}`)),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: io.NopCloser(strings.NewReader(`{"choices":[],"usage":{"prompt_tokens":2,"completion_tokens":3}}`)),
			}, nil
		}),
	})

	snap := testGatewaySnapshot()
	snap.Providers[0].ExecutionPolicy.ProviderClass = "quota_limited"
	snap.RoutingPolicy.FailurePolicy = snapshot.FailurePolicy{
		Threshold:                20,
		CooldownSec:              60,
		PassthroughAfterSec:      10,
		QuotaRecoveryIntervalMin: 30,
	}

	server := newGatewayTestServerWithState(t, snap, nil, state)
	defer server.Close()

	reqBody := `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`

	firstResp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("first request: %v", err)
	}
	firstResp.Body.Close()
	if firstResp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("first response status = %d, want %d", firstResp.StatusCode, http.StatusTooManyRequests)
	}

	secondResp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("second request: %v", err)
	}
	defer secondResp.Body.Close()
	if secondResp.StatusCode != http.StatusBadGateway {
		t.Fatalf("second response status = %d, want %d", secondResp.StatusCode, http.StatusBadGateway)
	}

	currentTime = currentTime.Add(11 * time.Second)
	thirdResp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("third request: %v", err)
	}
	defer thirdResp.Body.Close()
	if thirdResp.StatusCode != http.StatusOK {
		t.Fatalf("third response status = %d, want %d", thirdResp.StatusCode, http.StatusOK)
	}
	if attempts != 2 {
		t.Fatalf("upstream attempts = %d, want 2", attempts)
	}
}

func newGatewayTestServer(t *testing.T, snap *snapshot.Snapshot, tel TelemetryEmitter) *httptest.Server {
	return newGatewayTestServerWithState(t, snap, tel, NewRuntimeState())
}

func newGatewayTestServerWithState(t *testing.T, snap *snapshot.Snapshot, tel TelemetryEmitter, state *RuntimeState) *httptest.Server {
	t.Helper()

	if snap == nil {
		snap = testGatewaySnapshot()
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleChatCompletion(r.Context(), snap, state, tel, nil, w, r)
	}))
}

func testGatewaySnapshot() *snapshot.Snapshot {
	return &snapshot.Snapshot{
		Ingress: snapshot.IngressConfig{
			MaxBodyBytes: 1 << 20,
		},
		RoutingPolicy: snapshot.RoutingPolicy{
			Retry: snapshot.RetryPolicy{
				StatusCodes: []int{http.StatusRequestTimeout, http.StatusTooManyRequests},
			},
		},
		Providers: []snapshot.ProviderSnapshot{
			{
				ProviderID: "test-provider",
				BaseURL:    testUpstreamURL,
				ModelTable: []snapshot.ModelMapping{
					{
						PublicModel:   testPublicModel,
						UpstreamModel: testUpstreamModel,
					},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{
					Enabled:   true,
					Weight:    1,
					TimeoutMs: 5000,
				},
			},
		},
	}
}

type capturingTelemetryEmitter struct {
	events chan telemetryingest.Event
}

func (e *capturingTelemetryEmitter) Emit(event telemetryingest.Event) error {
	e.events <- event
	return nil
}

func swapSharedHTTPClient(t *testing.T, client *http.Client) {
	t.Helper()

	original := sharedHTTPClient
	sharedHTTPClient = client
	t.Cleanup(func() {
		sharedHTTPClient = original
	})
}

func TestHandleChatCompletionReturnsCachedResponseOnSecondRequest(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)
	routingSequence.Store(0)

	upstreamCalls := 0
	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			upstreamCalls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: io.NopCloser(strings.NewReader(`{"id":"cmpl-cached","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":3}}`)),
			}, nil
		}),
	})

	snap := testGatewaySnapshot()
	snap.RoutingPolicy.Cache = snapshot.CacheConfig{
		Enabled:    true,
		MaxEntries: 64,
		TTLSec:     300,
	}

	// Reset shared cache so test is isolated.
	responseCache.mu.Lock()
	responseCache.cache = nil
	responseCache.cfgKey = ""
	responseCache.mu.Unlock()

	server := newGatewayTestServer(t, snap, nil)
	defer server.Close()

	reqBody := `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`

	// First request: should hit upstream.
	resp1, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("first request: %v", err)
	}
	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first request status = %d", resp1.StatusCode)
	}
	if upstreamCalls != 1 {
		t.Fatalf("expected 1 upstream call, got %d", upstreamCalls)
	}

	// Second request with same body: should be a cache hit.
	resp2, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("second request: %v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second request status = %d", resp2.StatusCode)
	}
	if resp2.Header.Get("X-Cache") != "HIT" {
		t.Fatal("expected X-Cache: HIT header on cached response")
	}
	if string(body1) != string(body2) {
		t.Fatalf("cached body differs: %q vs %q", body1, body2)
	}
	if upstreamCalls != 1 {
		t.Fatalf("expected 1 upstream call after cache hit, got %d", upstreamCalls)
	}
}

func TestHandleChatCompletionPinnedProviderBypassesRuntimeGateForBenchmark(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)
	routingSequence.Store(0)

	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: io.NopCloser(strings.NewReader(`{"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2}}`)),
			}, nil
		}),
	})

	snap := testGatewaySnapshot()
	snap.RoutingPolicy.FailurePolicy = snapshot.FailurePolicy{
		Threshold:           1,
		CooldownSec:         60,
		PassthroughAfterSec: 600,
	}
	state := NewRuntimeState()
	state.reportAttemptResult("test-provider", http.StatusTooManyRequests, 0, nil, snap)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()
	result := &ExecutionResult{}
	ctx := WithExecutionOptions(req.Context(), &ExecutionOptions{
		RequestID:        "benchmark_run_case",
		PinnedProviderID: "test-provider",
		DisableCache:     true,
		DisableFallback:  true,
		DisableRetries:   true,
		DisableSticky:    true,
		SyntheticKind:    "benchmark",
		Result:           result,
	})

	HandleChatCompletion(ctx, snap, state, nil, nil, rec, req.WithContext(ctx))
	resp := rec.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resp.StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if result.Snapshot().ProviderID != "test-provider" {
		t.Fatalf("captured provider = %q, want test-provider", result.Snapshot().ProviderID)
	}
}

func TestHandleChatCompletionDoesNotCacheStreamRequests(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)
	routingSequence.Store(0)

	upstreamCalls := 0
	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			upstreamCalls++
			pr, pw := io.Pipe()
			go func() {
				_, _ = io.WriteString(pw, "data: {\"choices\":[]}\n\n")
				_ = pw.Close()
			}()
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       pr,
			}, nil
		}),
	})

	snap := testGatewaySnapshot()
	snap.RoutingPolicy.Cache = snapshot.CacheConfig{
		Enabled:    true,
		MaxEntries: 64,
		TTLSec:     300,
	}

	responseCache.mu.Lock()
	responseCache.cache = nil
	responseCache.cfgKey = ""
	responseCache.mu.Unlock()

	server := newGatewayTestServer(t, snap, nil)
	defer server.Close()

	reqBody := `{"model":"public-model","messages":[{"role":"user","content":"hello"}],"stream":true}`

	// Two streaming requests should both hit upstream.
	for i := 0; i < 2; i++ {
		resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		if _, err := io.ReadAll(resp.Body); err != nil {
			t.Fatalf("read response %d: %v", i, err)
		}
		if err := resp.Body.Close(); err != nil {
			t.Fatalf("close response %d: %v", i, err)
		}
	}

	if upstreamCalls != 2 {
		t.Fatalf("expected 2 upstream calls for streaming requests, got %d", upstreamCalls)
	}
}
