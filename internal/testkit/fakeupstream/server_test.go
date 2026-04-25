package fakeupstream

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestServerCapturesRequests(t *testing.T) {
	upstream := New(func(req CapturedRequest) Response {
		return Response{Body: []byte(`{"captured":true}`)}
	})
	defer upstream.Close()

	resp, err := http.Post(upstream.URL()+"/v1/chat/completions", "application/json", strings.NewReader(`{"ok":true}`))
	if err != nil {
		t.Fatalf("post upstream: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"captured":true}` {
		t.Fatalf("response body = %s", string(body))
	}

	requests := upstream.Requests()
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(requests))
	}
	if requests[0].Path != "/v1/chat/completions" {
		t.Fatalf("path = %s", requests[0].Path)
	}
	if requests[0].Method != http.MethodPost {
		t.Fatalf("method = %s, want POST", requests[0].Method)
	}
	if string(requests[0].Body) != `{"ok":true}` {
		t.Fatalf("body = %s", string(requests[0].Body))
	}
}
