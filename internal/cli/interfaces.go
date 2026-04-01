//go:build windows

// Package cli provides command-line interface support for the gateway.
package cli

import (
	"context"
	"net/http"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// HTTPClient defines the interface for HTTP operations
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// HTTPClientFactory creates HTTP clients with specific timeouts
type HTTPClientFactory interface {
	CreateClient(timeout string) HTTPClient
}

// ServiceManager defines the interface for Windows service operations
type ServiceManager interface {
	Connect() (ServiceManagerConnection, error)
}

// ServiceManagerConnection represents a connection to the service manager
type ServiceManagerConnection interface {
	Disconnect() error
	OpenService(name string) (ServiceHandle, error)
	CreateService(name, exepath string, config mgr.Config, args ...string) (ServiceHandle, error)
}

// ServiceHandle represents a handle to a Windows service
type ServiceHandle interface {
	Close() error
	Start(args ...string) error
	Control(cmd svc.Cmd) (svc.Status, error)
	Query() (svc.Status, error)
	Delete() error
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

// RealServiceManager implements ServiceManager using the Windows API
type RealServiceManager struct{}

// Connect implements ServiceManager
func (r *RealServiceManager) Connect() (ServiceManagerConnection, error) {
	m, err := mgr.Connect()
	if err != nil {
		return nil, err
	}
	return &RealServiceManagerConnection{m: m}, nil
}

type RealServiceManagerConnection struct {
	m *mgr.Mgr
}

func (c *RealServiceManagerConnection) Disconnect() error {
	c.m.Disconnect()
	return nil
}

func (c *RealServiceManagerConnection) OpenService(name string) (ServiceHandle, error) {
	s, err := c.m.OpenService(name)
	if err != nil {
		return nil, err
	}
	return &RealServiceHandle{s: s}, nil
}

func (c *RealServiceManagerConnection) CreateService(name, exepath string, config mgr.Config, args ...string) (ServiceHandle, error) {
	s, err := c.m.CreateService(name, exepath, config, args...)
	if err != nil {
		return nil, err
	}
	return &RealServiceHandle{s: s}, nil
}

type RealServiceHandle struct {
	s *mgr.Service
}

func (h *RealServiceHandle) Close() error {
	h.s.Close()
	return nil
}

func (h *RealServiceHandle) Start(args ...string) error {
	return h.s.Start(args...)
}

func (h *RealServiceHandle) Control(cmd svc.Cmd) (svc.Status, error) {
	return h.s.Control(cmd)
}

func (h *RealServiceHandle) Query() (svc.Status, error) {
	return h.s.Query()
}

func (h *RealServiceHandle) Delete() error {
	return h.s.Delete()
}

// NewDefaultHTTPClient creates a new DefaultHTTPClient with the specified timeout
func NewDefaultHTTPClient(timeout time.Duration) HTTPClient {
	return &DefaultHTTPClient{
		client: &http.Client{
			Timeout: timeout,
		},
	}
}
