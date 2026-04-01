//go:build !windows

package cli

import (
	"context"
	"net/http"
	"time"
)

// HTTPClient defines the interface for HTTP operations
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// HTTPClientFactory creates HTTP clients with specific timeouts
type HTTPClientFactory interface {
	CreateClient(timeout string) HTTPClient
}

// ServiceManager defines the interface for service operations (stub for non-Windows)
type ServiceManager interface {
	Connect() (ServiceManagerConnection, error)
}

// ServiceManagerConnection represents a connection to the service manager (stub for non-Windows)
type ServiceManagerConnection interface {
	Disconnect() error
	OpenService(name string) (ServiceHandle, error)
	CreateService(name, exepath string, config ServiceConfig, args ...string) (ServiceHandle, error)
}

// ServiceHandle represents a handle to a service (stub for non-Windows)
type ServiceHandle interface {
	Close() error
	Start(args ...string) error
	Control(cmd ServiceCmd) (ServiceStatus, error)
	Query() (ServiceStatus, error)
	Delete() error
}

// ServiceConfig is a stub for service configuration on non-Windows platforms
type ServiceConfig struct {
	DisplayName string
	Description string
	StartType   uint32
}

// ServiceCmd is a stub for service commands on non-Windows platforms
type ServiceCmd uint32

// ServiceStatus is a stub for service status on non-Windows platforms
type ServiceStatus struct {
	State     uint32
	Accepts   uint32
	ProcessId uint32
}

// AppRunner defines the interface for running the gateway application
type AppRunner interface {
	Run(ctx context.Context, configPath string) error
}

// ConfigLoader defines the interface for loading configuration
type ConfigLoader interface {
	LoadConfig(path string) error
	Validate() error
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
