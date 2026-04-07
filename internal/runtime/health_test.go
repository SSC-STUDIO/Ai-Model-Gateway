package runtime

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealthChecker_Check(t *testing.T) {
	tests := []struct {
		name       string
		response   string
		statusCode int
		wantErr    bool
		wantStatus string
	}{
		{
			name:       "healthy JSON response",
			response:   `{"status":"healthy","router_strategy":"health_weighted_rr","bridge_enabled":true}`,
			statusCode: http.StatusOK,
			wantErr:    false,
			wantStatus: "healthy",
		},
		{
			name:       "healthy plain text response",
			response:   "OK",
			statusCode: http.StatusOK,
			wantErr:    false,
			wantStatus: "healthy",
		},
		{
			name:       "unhealthy status",
			response:   `{"status":"unhealthy"}`,
			statusCode: http.StatusServiceUnavailable,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.response))
			}))
			defer server.Close()

			checker := NewHealthChecker(server.URL, 5*time.Second)
			result, err := checker.Check()

			if (err != nil) != tt.wantErr {
				t.Errorf("Check() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && result.Status != tt.wantStatus {
				t.Errorf("Check() status = %v, want %v", result.Status, tt.wantStatus)
			}
		})
	}
}

func TestHealthChecker_Check_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := NewHealthChecker(server.URL, 100*time.Millisecond)
	_, err := checker.Check()

	if err == nil {
		t.Error("Check() expected timeout error, got nil")
	}
}
