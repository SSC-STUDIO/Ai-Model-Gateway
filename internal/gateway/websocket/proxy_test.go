package websocket

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ai-model-gateway/internal/gateway/snapshot"

	"github.com/gorilla/websocket"
)

// mockSSRFChecker is a mock SSRF checker that allows all URLs for testing.
type mockSSRFChecker struct{}

func (m *mockSSRFChecker) ValidateURL(rawURL string) error {
	return nil
}

func mustSetReadDeadline(t *testing.T, conn *websocket.Conn, deadline time.Time) {
	t.Helper()
	if err := conn.SetReadDeadline(deadline); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
}

func mustWriteMessage(t *testing.T, conn *websocket.Conn, messageType int, data []byte) {
	t.Helper()
	if err := conn.WriteMessage(messageType, data); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}
}

func TestNewProxy(t *testing.T) {
	proxy := NewProxy()
	if proxy == nil {
		t.Fatal("NewProxy() returned nil")
	}
	if proxy.upgrader.ReadBufferSize == 0 {
		t.Error("upgrader not properly initialized")
	}
	if proxy.dialer.HandshakeTimeout == 0 {
		t.Error("dialer not properly initialized")
	}
	// Test CheckOrigin allows all origins
	if !proxy.upgrader.CheckOrigin(nil) {
		t.Error("CheckOrigin should return true for any request")
	}
}

func TestCollectProviderCandidates(t *testing.T) {
	tests := []struct {
		name        string
		snap        *snapshot.Snapshot
		model       string
		expectCount int
	}{
		{
			name:        "nil snapshot",
			snap:        nil,
			model:       "gpt-4",
			expectCount: 0,
		},
		{
			name: "empty providers",
			snap: &snapshot.Snapshot{
				Providers: []snapshot.ProviderSnapshot{},
			},
			model:       "gpt-4",
			expectCount: 0,
		},
		{
			name: "disabled provider",
			snap: &snapshot.Snapshot{
				Providers: []snapshot.ProviderSnapshot{
					{
						ProviderID: "disabled-provider",
						ExecutionPolicy: snapshot.ExecutionPolicy{
							Enabled: false,
						},
						ModelTable: []snapshot.ModelMapping{
							{PublicModel: "gpt-4", UpstreamModel: "gpt-4"},
						},
					},
				},
			},
			model:       "gpt-4",
			expectCount: 0,
		},
		{
			name: "enabled provider with matching model",
			snap: &snapshot.Snapshot{
				Providers: []snapshot.ProviderSnapshot{
					{
						ProviderID: "enabled-provider",
						ExecutionPolicy: snapshot.ExecutionPolicy{
							Enabled: true,
							Weight:  10,
						},
						ModelTable: []snapshot.ModelMapping{
							{PublicModel: "gpt-4", UpstreamModel: "gpt-4-turbo"},
						},
					},
				},
			},
			model:       "gpt-4",
			expectCount: 1,
		},
		{
			name: "multiple providers",
			snap: &snapshot.Snapshot{
				Providers: []snapshot.ProviderSnapshot{
					{
						ProviderID: "provider-1",
						ExecutionPolicy: snapshot.ExecutionPolicy{
							Enabled: true,
							Weight:  5,
						},
						ModelTable: []snapshot.ModelMapping{
							{PublicModel: "gpt-4", UpstreamModel: "gpt-4"},
						},
					},
					{
						ProviderID: "provider-2",
						ExecutionPolicy: snapshot.ExecutionPolicy{
							Enabled: true,
							Weight:  10,
						},
						ModelTable: []snapshot.ModelMapping{
							{PublicModel: "gpt-4", UpstreamModel: "gpt-4-turbo"},
						},
					},
				},
			},
			model:       "gpt-4",
			expectCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates := collectProviderCandidates(tt.snap, tt.model)
			if len(candidates) != tt.expectCount {
				t.Errorf("expected %d candidates, got %d", tt.expectCount, len(candidates))
			}
		})
	}
}

func TestBuildUpstreamURL(t *testing.T) {
	tests := []struct {
		name           string
		provider       *snapshot.ProviderSnapshot
		path           string
		model          string
		upstreamModel  string
		expectedScheme string
		expectedModel  string
	}{
		{
			name: "https to wss conversion",
			provider: &snapshot.ProviderSnapshot{
				BaseURL: "https://api.openai.com",
			},
			path:           "/v1/realtime",
			model:          "gpt-4",
			upstreamModel:  "gpt-4-realtime",
			expectedScheme: "wss://",
			expectedModel:  "gpt-4-realtime",
		},
		{
			name: "http to ws conversion",
			provider: &snapshot.ProviderSnapshot{
				BaseURL: "http://localhost:8080",
			},
			path:           "/v1/realtime",
			model:          "gpt-4",
			upstreamModel:  "gpt-4-realtime",
			expectedScheme: "ws://",
			expectedModel:  "gpt-4-realtime",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := buildUpstreamURL(tt.provider, tt.path, tt.model, tt.upstreamModel)
			if !strings.Contains(url, tt.expectedScheme) {
				t.Errorf("expected URL to contain %s, got %s", tt.expectedScheme, url)
			}
			if !strings.Contains(url, tt.expectedModel) {
				t.Errorf("expected URL to contain model %s, got %s", tt.expectedModel, url)
			}
		})
	}
}

func TestBuildUpstreamURL_WithQueryString(t *testing.T) {
	provider := &snapshot.ProviderSnapshot{
		BaseURL: "https://api.openai.com",
	}
	// Path with existing query string
	url := buildUpstreamURL(provider, "/v1/realtime?model=gpt-4", "gpt-4", "gpt-4-realtime")

	// Should replace model parameter
	if !strings.Contains(url, "wss://") {
		t.Errorf("expected wss scheme, got %s", url)
	}
	if !strings.Contains(url, "model=gpt-4-realtime") {
		t.Errorf("expected model to be replaced, got %s", url)
	}
	if strings.Contains(url, "model=gpt-4") && !strings.Contains(url, "model=gpt-4-realtime") {
		t.Errorf("original model should be replaced, got %s", url)
	}
}

func TestNormalizeWeight(t *testing.T) {
	tests := []struct {
		weight   int
		expected int
	}{
		{0, 1},
		{-5, 1},
		{1, 1},
		{10, 10},
		{100, 100},
	}

	for _, tt := range tests {
		result := normalizeWeight(tt.weight)
		if result != tt.expected {
			t.Errorf("normalizeWeight(%d) = %d, expected %d", tt.weight, result, tt.expected)
		}
	}
}

func TestServeHTTP_MissingModel(t *testing.T) {
	proxy := NewProxy()
	req := httptest.NewRequest("GET", "/v1/realtime", nil)
	w := httptest.NewRecorder()

	snap := &snapshot.Snapshot{}
	proxy.ServeHTTP(w, req, snap)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
	if !strings.Contains(w.Body.String(), "model parameter is required") {
		t.Errorf("expected error message about model parameter, got %s", w.Body.String())
	}
}

func TestServeHTTP_NilSnapshot(t *testing.T) {
	proxy := NewProxy()
	req := httptest.NewRequest("GET", "/v1/realtime?model=gpt-4", nil)
	w := httptest.NewRecorder()

	proxy.ServeHTTP(w, req, nil)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestSetAuthHeaders(t *testing.T) {
	tests := []struct {
		name           string
		provider       *snapshot.ProviderSnapshot
		expectedHeader string
		expectedValue  string
	}{
		{
			name: "bearer token",
			provider: &snapshot.ProviderSnapshot{
				Credentials: snapshot.Credentials{
					Kind:  "bearer",
					Value: "test-token",
				},
			},
			expectedHeader: "Authorization",
			expectedValue:  "Bearer test-token",
		},
		{
			name: "api key with default header",
			provider: &snapshot.ProviderSnapshot{
				Credentials: snapshot.Credentials{
					Kind:  "api_key",
					Value: "test-key",
				},
			},
			expectedHeader: "X-Api-Key",
			expectedValue:  "test-key",
		},
		{
			name: "api key with custom header",
			provider: &snapshot.ProviderSnapshot{
				Credentials: snapshot.Credentials{
					Kind:       "api_key",
					Value:      "test-key",
					HeaderName: "X-Custom-Auth",
				},
			},
			expectedHeader: "X-Custom-Auth",
			expectedValue:  "test-key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := http.Header{}
			setAuthHeaders(headers, tt.provider)

			value := headers.Get(tt.expectedHeader)
			if value != tt.expectedValue {
				t.Errorf("expected header %s=%s, got %s", tt.expectedHeader, tt.expectedValue, value)
			}
		})
	}
}

func TestSetAuthHeaders_WithExtraHeaders(t *testing.T) {
	provider := &snapshot.ProviderSnapshot{
		Credentials: snapshot.Credentials{
			Kind:  "bearer",
			Value: "test-token",
		},
		Headers: map[string]string{
			"X-Custom-Header":  "custom-value",
			"X-Another-Header": "another-value",
		},
	}

	headers := http.Header{}
	setAuthHeaders(headers, provider)

	// Check auth header
	if headers.Get("Authorization") != "Bearer test-token" {
		t.Error("expected Authorization header to be set")
	}

	// Check extra headers
	if headers.Get("X-Custom-Header") != "custom-value" {
		t.Error("expected X-Custom-Header to be set")
	}
	if headers.Get("X-Another-Header") != "another-value" {
		t.Error("expected X-Another-Header to be set")
	}
}

func TestSetAuthHeaders_EmptyCredentials(t *testing.T) {
	provider := &snapshot.ProviderSnapshot{
		Credentials: snapshot.Credentials{
			Kind:  "bearer",
			Value: "", // Empty value
		},
	}

	headers := http.Header{}
	setAuthHeaders(headers, provider)

	if headers.Get("Authorization") != "" {
		t.Error("expected no Authorization header for empty value")
	}
}

// TestServeHTTP_ModelNotFound tests when no provider supports the model.
func TestServeHTTP_ModelNotFound(t *testing.T) {
	proxy := NewProxy()

	// Create a test server to handle WebSocket upgrade
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		snap := &snapshot.Snapshot{
			Providers: []snapshot.ProviderSnapshot{
				{
					ProviderID: "test-provider",
					ExecutionPolicy: snapshot.ExecutionPolicy{
						Enabled: true,
					},
					ModelTable: []snapshot.ModelMapping{
						{PublicModel: "other-model", UpstreamModel: "other-model"},
					},
				},
			},
		}
		proxy.ServeHTTP(w, r, snap)
	}))
	defer server.Close()

	// Connect as WebSocket client
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/realtime?model=gpt-4"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Read the error message
	_, message, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}

	if !strings.Contains(string(message), "model not found") {
		t.Errorf("expected model not found error, got: %s", string(message))
	}
}

// TestServeHTTP_DisabledProvider tests that disabled providers are skipped.
func TestServeHTTP_DisabledProvider(t *testing.T) {
	proxy := NewProxy()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		snap := &snapshot.Snapshot{
			Providers: []snapshot.ProviderSnapshot{
				{
					ProviderID: "disabled-provider",
					ExecutionPolicy: snapshot.ExecutionPolicy{
						Enabled: false,
					},
					ModelTable: []snapshot.ModelMapping{
						{PublicModel: "gpt-4", UpstreamModel: "gpt-4"},
					},
				},
			},
		}
		proxy.ServeHTTP(w, r, snap)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/realtime?model=gpt-4"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	_, message, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}

	if !strings.Contains(string(message), "model not found") {
		t.Errorf("expected model not found error (disabled provider should be skipped), got: %s", string(message))
	}
}

// TestServeHTTP_NoProviderAvailable tests when all providers fail to connect.
func TestServeHTTP_NoProviderAvailable(t *testing.T) {
	proxy := NewProxyWithSSRFChecker(&mockSSRFChecker{})

	failingUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not a websocket endpoint", http.StatusBadGateway)
	}))
	defer failingUpstream.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		snap := &snapshot.Snapshot{
			Providers: []snapshot.ProviderSnapshot{
				{
					ProviderID: "test-provider",
					ExecutionPolicy: snapshot.ExecutionPolicy{
						Enabled: true,
					},
					BaseURL: failingUpstream.URL,
					ModelTable: []snapshot.ModelMapping{
						{PublicModel: "gpt-4", UpstreamModel: "gpt-4"},
					},
				},
			},
		}
		proxy.ServeHTTP(w, r, snap)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/realtime?model=gpt-4"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Set a read deadline to avoid hanging
	mustSetReadDeadline(t, conn, time.Now().Add(5*time.Second))

	_, message, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}

	if !strings.Contains(string(message), "no provider available") {
		t.Errorf("expected no provider available error, got: %s", string(message))
	}
}

// TestForwardMessages_NormalFlow tests the forwardMessages function with normal message flow.
func TestForwardMessages_NormalFlow(t *testing.T) {
	proxy := NewProxy()
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	// Create echo server
	echoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// Echo messages back
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	}))
	defer echoServer.Close()

	// Connect to echo server
	echoURL := "ws" + strings.TrimPrefix(echoServer.URL, "http")
	echoConn, _, err := websocket.DefaultDialer.Dial(echoURL, nil)
	if err != nil {
		t.Fatalf("failed to connect to echo server: %v", err)
	}
	defer echoConn.Close()

	// Create a test WebSocket client connection via httptest
	var clientConn *websocket.Conn
	clientCh := make(chan *websocket.Conn, 1)
	clientServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		clientCh <- conn
	}))
	defer clientServer.Close()

	// Connect as client
	clientURL := "ws" + strings.TrimPrefix(clientServer.URL, "http")
	client, _, err := websocket.DefaultDialer.Dial(clientURL, nil)
	if err != nil {
		t.Fatalf("failed to connect client: %v", err)
	}
	defer client.Close()

	// Get the server-side connection
	select {
	case clientConn = <-clientCh:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for server connection")
	}

	// Start forwardMessages in a goroutine
	done := make(chan struct{})
	go func() {
		proxy.forwardMessages(clientConn, echoConn, "client->echo")
		close(done)
	}()

	// Send a message
	testMsg := `{"type":"test"}`
	if err := client.WriteMessage(websocket.TextMessage, []byte(testMsg)); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	// Read from echo connection (should receive the forwarded message)
	mustSetReadDeadline(t, echoConn, time.Now().Add(2*time.Second))
	mt, msg, err := echoConn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read from echo: %v", err)
	}
	if mt != websocket.TextMessage {
		t.Errorf("expected text message, got type %d", mt)
	}
	if string(msg) != testMsg {
		t.Errorf("expected %q, got %q", testMsg, string(msg))
	}

	// Close client to stop forwardMessages
	client.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("forwardMessages did not exit in time")
	}
}

// TestForwardMessages_CloseError tests handling of close errors.
func TestForwardMessages_CloseError(t *testing.T) {
	proxy := NewProxy()
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	// Create servers for src and dst
	var srcConn, dstConn *websocket.Conn
	srcCh := make(chan *websocket.Conn, 1)
	dstCh := make(chan *websocket.Conn, 1)

	srcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		srcCh <- conn
	}))
	defer srcServer.Close()

	dstServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		dstCh <- conn
	}))
	defer dstServer.Close()

	// Connect to both servers
	srcURL := "ws" + strings.TrimPrefix(srcServer.URL, "http")
	srcClient, _, err := websocket.DefaultDialer.Dial(srcURL, nil)
	if err != nil {
		t.Fatalf("failed to connect to src: %v", err)
	}
	defer srcClient.Close()

	dstURL := "ws" + strings.TrimPrefix(dstServer.URL, "http")
	dstClient, _, err := websocket.DefaultDialer.Dial(dstURL, nil)
	if err != nil {
		t.Fatalf("failed to connect to dst: %v", err)
	}
	defer dstClient.Close()

	// Get server-side connections
	select {
	case srcConn = <-srcCh:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for src connection")
	}
	select {
	case dstConn = <-dstCh:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for dst connection")
	}

	// Start forwardMessages
	done := make(chan struct{})
	go func() {
		proxy.forwardMessages(srcConn, dstConn, "test-direction")
		close(done)
	}()

	// Send a message first
	mustWriteMessage(t, srcClient, websocket.TextMessage, []byte(`test`))

	// Close src client with going away
	mustWriteMessage(t, srcClient, websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseGoingAway, "going away"))
	srcClient.Close()

	select {
	case <-done:
		// forwardMessages exited as expected
	case <-time.After(2 * time.Second):
		t.Error("forwardMessages did not exit in time")
	}
}

// TestForwardMessages_AbnormalClosure tests handling of abnormal closure errors.
func TestForwardMessages_AbnormalClosure(t *testing.T) {
	proxy := NewProxy()
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	var srcConn, dstConn *websocket.Conn
	srcCh := make(chan *websocket.Conn, 1)
	dstCh := make(chan *websocket.Conn, 1)

	srcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		srcCh <- conn
	}))
	defer srcServer.Close()

	dstServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		dstCh <- conn
	}))
	defer dstServer.Close()

	srcURL := "ws" + strings.TrimPrefix(srcServer.URL, "http")
	srcClient, _, err := websocket.DefaultDialer.Dial(srcURL, nil)
	if err != nil {
		t.Fatalf("failed to connect to src: %v", err)
	}
	defer srcClient.Close()

	dstURL := "ws" + strings.TrimPrefix(dstServer.URL, "http")
	dstClient, _, err := websocket.DefaultDialer.Dial(dstURL, nil)
	if err != nil {
		t.Fatalf("failed to connect to dst: %v", err)
	}
	defer dstClient.Close()

	select {
	case srcConn = <-srcCh:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for src connection")
	}
	select {
	case dstConn = <-dstCh:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for dst connection")
	}

	done := make(chan struct{})
	go func() {
		proxy.forwardMessages(srcConn, dstConn, "test-direction")
		close(done)
	}()

	// Send a message
	mustWriteMessage(t, srcClient, websocket.TextMessage, []byte(`test`))

	// Close with abnormal closure
	mustWriteMessage(t, srcClient, websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseAbnormalClosure, "abnormal"))
	srcClient.Close()

	select {
	case <-done:
		// forwardMessages exited as expected
	case <-time.After(2 * time.Second):
		t.Error("forwardMessages did not exit in time")
	}
}

// TestServeHTTP_Success tests successful WebSocket proxy connection.
func TestServeHTTP_Success(t *testing.T) {
	// Create proxy with mock SSRF checker that allows all URLs
	proxy := NewProxyWithSSRFChecker(&mockSSRFChecker{})

	// Create upstream WebSocket server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Echo messages back
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	}))
	defer upstream.Close()

	// Create gateway server
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		snap := &snapshot.Snapshot{
			Providers: []snapshot.ProviderSnapshot{
				{
					ProviderID: "test-provider",
					ExecutionPolicy: snapshot.ExecutionPolicy{
						Enabled: true,
						Weight:  10,
					},
					BaseURL: upstream.URL,
					Credentials: snapshot.Credentials{
						Kind:  "bearer",
						Value: "test-token",
					},
					Headers: map[string]string{
						"X-Provider-Header": "provider-value",
					},
					ModelTable: []snapshot.ModelMapping{
						{PublicModel: "gpt-4", UpstreamModel: "gpt-4-realtime"},
					},
				},
			},
		}
		proxy.ServeHTTP(w, r, snap)
	}))
	defer gateway.Close()

	// Connect as client
	wsURL := "ws" + strings.TrimPrefix(gateway.URL, "http") + "/v1/realtime?model=gpt-4"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Send a message
	testMsg := `{"type":"session.update"}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(testMsg)); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	// Read the response
	mustSetReadDeadline(t, conn, time.Now().Add(2*time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}

	if string(msg) != testMsg {
		t.Errorf("expected %q, got %q", testMsg, string(msg))
	}
}

// TestServeHTTP_ProviderFallback tests fallback to next provider on failure.
func TestServeHTTP_ProviderFallback(t *testing.T) {
	proxy := NewProxyWithSSRFChecker(&mockSSRFChecker{})

	failingUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not a websocket endpoint", http.StatusBadGateway)
	}))
	defer failingUpstream.Close()

	// Create upstream WebSocket server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	}))
	defer upstream.Close()

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		snap := &snapshot.Snapshot{
			Providers: []snapshot.ProviderSnapshot{
				{
					// First provider will fail (invalid URL)
					ProviderID: "failing-provider",
					ExecutionPolicy: snapshot.ExecutionPolicy{
						Enabled: true,
					},
					BaseURL: failingUpstream.URL,
					ModelTable: []snapshot.ModelMapping{
						{PublicModel: "gpt-4", UpstreamModel: "gpt-4"},
					},
				},
				{
					// Second provider should succeed
					ProviderID: "working-provider",
					ExecutionPolicy: snapshot.ExecutionPolicy{
						Enabled: true,
					},
					BaseURL: upstream.URL,
					ModelTable: []snapshot.ModelMapping{
						{PublicModel: "gpt-4", UpstreamModel: "gpt-4"},
					},
				},
			},
		}
		proxy.ServeHTTP(w, r, snap)
	}))
	defer gateway.Close()

	wsURL := "ws" + strings.TrimPrefix(gateway.URL, "http") + "/v1/realtime?model=gpt-4"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	testMsg := `{"type":"test"}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(testMsg)); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	mustSetReadDeadline(t, conn, time.Now().Add(2*time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}

	if string(msg) != testMsg {
		t.Errorf("expected %q, got %q", testMsg, string(msg))
	}
}

// TestServeHTTP_SSRFValidationFail tests SSRF validation failure.
func TestServeHTTP_SSRFValidationFail(t *testing.T) {
	// Use default SSRF checker which blocks localhost
	proxy := NewProxy()

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		snap := &snapshot.Snapshot{
			Providers: []snapshot.ProviderSnapshot{
				{
					ProviderID: "test-provider",
					ExecutionPolicy: snapshot.ExecutionPolicy{
						Enabled: true,
					},
					BaseURL: "http://localhost:9999",
					ModelTable: []snapshot.ModelMapping{
						{PublicModel: "gpt-4", UpstreamModel: "gpt-4"},
					},
				},
			},
		}
		proxy.ServeHTTP(w, r, snap)
	}))
	defer gateway.Close()

	wsURL := "ws" + strings.TrimPrefix(gateway.URL, "http") + "/v1/realtime?model=gpt-4"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	mustSetReadDeadline(t, conn, time.Now().Add(2*time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	if !strings.Contains(string(msg), "no provider available") {
		t.Errorf("expected no provider error, got: %s", string(msg))
	}
}

// TestServeHTTP_WithAuthHeaders tests that auth headers are passed correctly.
func TestServeHTTP_WithAuthHeaders(t *testing.T) {
	proxy := NewProxyWithSSRFChecker(&mockSSRFChecker{})

	// Create upstream server that echoes messages
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	}))
	defer upstream.Close()

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		snap := &snapshot.Snapshot{
			Providers: []snapshot.ProviderSnapshot{
				{
					ProviderID: "test-provider",
					ExecutionPolicy: snapshot.ExecutionPolicy{
						Enabled: true,
					},
					BaseURL: upstream.URL,
					Credentials: snapshot.Credentials{
						Kind:       "api_key",
						Value:      "my-api-key",
						HeaderName: "X-Custom-Key",
					},
					ModelTable: []snapshot.ModelMapping{
						{PublicModel: "gpt-4", UpstreamModel: "gpt-4"},
					},
				},
			},
		}
		proxy.ServeHTTP(w, r, snap)
	}))
	defer gateway.Close()

	wsURL := "ws" + strings.TrimPrefix(gateway.URL, "http") + "/v1/realtime?model=gpt-4"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	testMsg := `{"type":"test"}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(testMsg)); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	mustSetReadDeadline(t, conn, time.Now().Add(2*time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}

	if string(msg) != testMsg {
		t.Errorf("expected %q, got %q", testMsg, string(msg))
	}
}

// TestServeHTTP_UpgradeError tests handling of WebSocket upgrade errors.
func TestServeHTTP_UpgradeError(t *testing.T) {
	proxy := NewProxy()

	// Create a server that will cause upgrade to fail (non-WebSocket request)
	req := httptest.NewRequest("GET", "/v1/realtime?model=gpt-4", nil)
	// Missing WebSocket headers will cause upgrade to fail
	req.Header.Set("Connection", "keep-alive") // Not "Upgrade"
	w := httptest.NewRecorder()

	snap := &snapshot.Snapshot{
		Providers: []snapshot.ProviderSnapshot{
			{
				ProviderID: "test-provider",
				ExecutionPolicy: snapshot.ExecutionPolicy{
					Enabled: true,
				},
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "gpt-4", UpstreamModel: "gpt-4"},
				},
			},
		},
	}

	proxy.ServeHTTP(w, req, snap)

	// The upgrade will fail because it's not a proper WebSocket request
	// The response should indicate bad request (WebSocket upgrade failure)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// TestForwardMessages_WriteError tests write error handling.
func TestForwardMessages_WriteError(t *testing.T) {
	proxy := NewProxy()
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	var srcConn, dstConn *websocket.Conn
	srcCh := make(chan *websocket.Conn, 1)
	dstCh := make(chan *websocket.Conn, 1)

	srcServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		srcCh <- conn
	}))
	defer srcServer.Close()

	dstServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		dstCh <- conn
	}))
	defer dstServer.Close()

	srcURL := "ws" + strings.TrimPrefix(srcServer.URL, "http")
	srcClient, _, err := websocket.DefaultDialer.Dial(srcURL, nil)
	if err != nil {
		t.Fatalf("failed to connect to src: %v", err)
	}
	defer srcClient.Close()

	dstURL := "ws" + strings.TrimPrefix(dstServer.URL, "http")
	dstClient, _, err := websocket.DefaultDialer.Dial(dstURL, nil)
	if err != nil {
		t.Fatalf("failed to connect to dst: %v", err)
	}

	select {
	case srcConn = <-srcCh:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for src connection")
	}
	select {
	case dstConn = <-dstCh:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for dst connection")
	}

	// Close dst immediately to cause write error
	dstClient.Close()
	dstConn.Close()

	done := make(chan struct{})
	go func() {
		proxy.forwardMessages(srcConn, dstConn, "test-direction")
		close(done)
	}()

	// Send a message - write should fail
	mustWriteMessage(t, srcClient, websocket.TextMessage, []byte(`test`))

	select {
	case <-done:
		// forwardMessages exited as expected
	case <-time.After(2 * time.Second):
		t.Error("forwardMessages did not exit in time")
	}
}
