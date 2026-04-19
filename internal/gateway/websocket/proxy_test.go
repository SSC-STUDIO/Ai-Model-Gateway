package websocket

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ai-model-gateway/internal/gateway/snapshot"
)

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
}

func TestCollectProviderCandidates(t *testing.T) {
	tests := []struct {
		name          string
		snap          *snapshot.Snapshot
		model         string
		expectCount   int
		expectEnabled bool
	}{
		{
			name:  "nil snapshot",
			snap:  nil,
			model: "gpt-4",
			expectCount: 0,
		},
		{
			name: "empty providers",
			snap: &snapshot.Snapshot{
				Providers: []snapshot.ProviderSnapshot{},
			},
			model: "gpt-4",
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
			model: "gpt-4",
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
			model: "gpt-4",
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
			model: "gpt-4",
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
