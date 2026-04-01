//go:build !windows

package cli

import (
	"net/http"
	"time"
)

// HTTPClient defines the interface for HTTP operations
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// DefaultHTTPClient wraps http.Client to implement HTTPClient interface
type DefaultHTTPClient struct {
	client *http.Client
}

// Do implements HTTPClient
func (c *DefaultHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return c.client.Do(req)
}

// NewDefaultHTTPClient creates a new DefaultHTTPClient with the specified timeout
func NewDefaultHTTPClient(timeout time.Duration) HTTPClient {
	return &DefaultHTTPClient{
		client: &http.Client{
			Timeout: timeout,
		},
	}
}
