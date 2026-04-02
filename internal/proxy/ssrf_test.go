package proxy

import "testing"

func TestSSRFCheckerRejectsLoopbackHosts(t *testing.T) {
	checker := NewSSRFChecker()

	testCases := []struct {
		name   string
		rawURL string
	}{
		{name: "localhost", rawURL: "http://localhost:8080/v1/chat/completions"},
		{name: "ipv4 loopback", rawURL: "http://127.0.0.1:8080/v1/models"},
		{name: "ipv6 loopback", rawURL: "http://[::1]:8080/v1/models"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := checker.ValidateURL(tc.rawURL); err == nil {
				t.Fatalf("expected loopback URL %q to be rejected", tc.rawURL)
			}
		})
	}
}
