//go:build windows

package cli

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func TestServiceStateString(t *testing.T) {
	tests := []struct {
		state svc.State
		want  string
	}{
		{svc.Stopped, "Stopped"},
		{svc.StartPending, "Starting..."},
		{svc.StopPending, "Stopping..."},
		{svc.Running, "Running"},
		{svc.ContinuePending, "Resuming..."},
		{svc.PausePending, "Pausing..."},
		{svc.Paused, "Paused"},
		{svc.State(999), "Unknown(999)"},
	}
	
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := serviceStateString(tt.state)
			if got != tt.want {
				t.Errorf("serviceStateString(%v) = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

func TestNewServiceManagerProvider(t *testing.T) {
	provider := NewServiceManagerProvider()
	
	if provider == nil {
		t.Fatal("NewServiceManagerProvider() returned nil")
	}
	
	if provider.manager == nil {
		t.Error("expected manager to be set")
	}
}

func TestNewServiceManagerProviderWithManager(t *testing.T) {
	mockManager := &MockServiceManager{}
	provider := NewServiceManagerProviderWithManager(mockManager)
	
	if provider == nil {
		t.Fatal("NewServiceManagerProviderWithManager() returned nil")
	}
	
	if provider.manager != mockManager {
		t.Error("expected manager to be set to mock")
	}
}

func TestInstallServiceConnectError(t *testing.T) {
	cli := New()
	
	mockManager := &MockServiceManager{
		ConnectFunc: func() (ServiceManagerConnection, error) {
			return nil, errors.New("connection failed")
		},
	}
	
	provider := NewServiceManagerProviderWithManager(mockManager)
	
	err := cli.installService(provider)
	
	if err == nil {
		t.Error("expected error for connection failure")
	}
	
	if err.Error() != "connect to service manager: connection failed" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestInstallServiceAlreadyInstalled(t *testing.T) {
	cli := New()
	
	mockConn := &MockServiceManagerConnection{
		OpenServiceFunc: func(name string) (ServiceHandle, error) {
			return &MockServiceHandle{}, nil // Service exists
		},
	}
	
	mockManager := &MockServiceManager{
		ConnectFunc: func() (ServiceManagerConnection, error) {
			return mockConn, nil
		},
	}
	
	provider := NewServiceManagerProviderWithManager(mockManager)
	
	err := cli.installService(provider)
	
	if err == nil {
		t.Error("expected error for already installed service")
	}
	
	if err.Error() != "service AIModelGateway already installed" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestUninstallServiceConnectError(t *testing.T) {
	cli := New()
	
	mockManager := &MockServiceManager{
		ConnectFunc: func() (ServiceManagerConnection, error) {
			return nil, errors.New("connection failed")
		},
	}
	
	provider := NewServiceManagerProviderWithManager(mockManager)
	
	err := cli.uninstallService(provider)
	
	if err == nil {
		t.Error("expected error for connection failure")
	}
}

func TestUninstallServiceNotFound(t *testing.T) {
	cli := New()
	
	mockConn := &MockServiceManagerConnection{
		OpenServiceFunc: func(name string) (ServiceHandle, error) {
			return nil, errors.New("service not found")
		},
	}
	
	mockManager := &MockServiceManager{
		ConnectFunc: func() (ServiceManagerConnection, error) {
			return mockConn, nil
		},
	}
	
	provider := NewServiceManagerProviderWithManager(mockManager)
	
	err := cli.uninstallService(provider)
	
	if err == nil {
		t.Error("expected error for missing service")
	}
}

func TestStartServiceConnectError(t *testing.T) {
	cli := New()
	
	mockManager := &MockServiceManager{
		ConnectFunc: func() (ServiceManagerConnection, error) {
			return nil, errors.New("connection failed")
		},
	}
	
	provider := NewServiceManagerProviderWithManager(mockManager)
	
	err := cli.startService(provider)
	
	if err == nil {
		t.Error("expected error for connection failure")
	}
}

func TestStartServiceNotFound(t *testing.T) {
	cli := New()
	
	mockConn := &MockServiceManagerConnection{
		OpenServiceFunc: func(name string) (ServiceHandle, error) {
			return nil, errors.New("service not found")
		},
	}
	
	mockManager := &MockServiceManager{
		ConnectFunc: func() (ServiceManagerConnection, error) {
			return mockConn, nil
		},
	}
	
	provider := NewServiceManagerProviderWithManager(mockManager)
	
	err := cli.startService(provider)
	
	if err == nil {
		t.Error("expected error for missing service")
	}
}

func TestStartServiceStartError(t *testing.T) {
	cli := New()
	
	mockHandle := &MockServiceHandle{
		StartFunc: func(args ...string) error {
			return errors.New("start failed")
		},
	}
	
	mockConn := &MockServiceManagerConnection{
		OpenServiceFunc: func(name string) (ServiceHandle, error) {
			return mockHandle, nil
		},
	}
	
	mockManager := &MockServiceManager{
		ConnectFunc: func() (ServiceManagerConnection, error) {
			return mockConn, nil
		},
	}
	
	provider := NewServiceManagerProviderWithManager(mockManager)
	
	err := cli.startService(provider)
	
	if err == nil {
		t.Error("expected error for start failure")
	}
}

func TestStopServiceConnectError(t *testing.T) {
	cli := New()
	
	mockManager := &MockServiceManager{
		ConnectFunc: func() (ServiceManagerConnection, error) {
			return nil, errors.New("connection failed")
		},
	}
	
	provider := NewServiceManagerProviderWithManager(mockManager)
	
	err := cli.stopService(provider)
	
	if err == nil {
		t.Error("expected error for connection failure")
	}
}

func TestServiceStatusConnectError(t *testing.T) {
	cli := New()
	
	mockManager := &MockServiceManager{
		ConnectFunc: func() (ServiceManagerConnection, error) {
			return nil, errors.New("connection failed")
		},
	}
	
	provider := NewServiceManagerProviderWithManager(mockManager)
	
	err := cli.serviceStatus(provider)
	
	if err == nil {
		t.Error("expected error for connection failure")
	}
}

func TestServiceStatusNotInstalled(t *testing.T) {
	cli := New()
	
	mockConn := &MockServiceManagerConnection{
		OpenServiceFunc: func(name string) (ServiceHandle, error) {
			return nil, errors.New("service not found")
		},
	}
	
	mockManager := &MockServiceManager{
		ConnectFunc: func() (ServiceManagerConnection, error) {
			return mockConn, nil
		},
	}
	
	provider := NewServiceManagerProviderWithManager(mockManager)
	
	// Should not error when service is not installed - just prints message
	err := cli.serviceStatus(provider)
	
	if err != nil {
		t.Errorf("expected no error for not installed service, got %v", err)
	}
}

func TestServiceStatusQueryError(t *testing.T) {
	cli := New()
	
	mockHandle := &MockServiceHandle{
		QueryFunc: func() (svc.Status, error) {
			return svc.Status{}, errors.New("query failed")
		},
	}
	
	mockConn := &MockServiceManagerConnection{
		OpenServiceFunc: func(name string) (ServiceHandle, error) {
			return mockHandle, nil
		},
	}
	
	mockManager := &MockServiceManager{
		ConnectFunc: func() (ServiceManagerConnection, error) {
			return mockConn, nil
		},
	}
	
	provider := NewServiceManagerProviderWithManager(mockManager)
	
	err := cli.serviceStatus(provider)
	
	if err == nil {
		t.Error("expected error for query failure")
	}
}

func TestUninstallServiceQueryError(t *testing.T) {
	cli := New()
	
	mockHandle := &MockServiceHandle{
		QueryFunc: func() (svc.Status, error) {
			return svc.Status{}, errors.New("query failed")
		},
	}
	
	mockConn := &MockServiceManagerConnection{
		OpenServiceFunc: func(name string) (ServiceHandle, error) {
			return mockHandle, nil
		},
	}
	
	mockManager := &MockServiceManager{
		ConnectFunc: func() (ServiceManagerConnection, error) {
			return mockConn, nil
		},
	}
	
	provider := NewServiceManagerProviderWithManager(mockManager)
	
	err := cli.uninstallService(provider)
	
	if err == nil {
		t.Error("expected error for query failure")
	}
}

func TestUninstallServiceStopError(t *testing.T) {
	cli := New()
	
	mockHandle := &MockServiceHandle{
		QueryFunc: func() (svc.Status, error) {
			return svc.Status{State: svc.Running}, nil // Running, needs to stop
		},
		ControlFunc: func(cmd svc.Cmd) (svc.Status, error) {
			return svc.Status{}, errors.New("stop failed")
		},
	}
	
	mockConn := &MockServiceManagerConnection{
		OpenServiceFunc: func(name string) (ServiceHandle, error) {
			return mockHandle, nil
		},
	}
	
	mockManager := &MockServiceManager{
		ConnectFunc: func() (ServiceManagerConnection, error) {
			return mockConn, nil
		},
	}
	
	provider := NewServiceManagerProviderWithManager(mockManager)
	
	err := cli.uninstallService(provider)
	
	if err == nil {
		t.Error("expected error for stop failure")
	}
}

func TestUninstallServiceDeleteError(t *testing.T) {
	cli := New()
	
	mockHandle := &MockServiceHandle{
		QueryFunc: func() (svc.Status, error) {
			return svc.Status{State: svc.Stopped}, nil // Already stopped
		},
		DeleteFunc: func() error {
			return errors.New("delete failed")
		},
	}
	
	mockConn := &MockServiceManagerConnection{
		OpenServiceFunc: func(name string) (ServiceHandle, error) {
			return mockHandle, nil
		},
	}
	
	mockManager := &MockServiceManager{
		ConnectFunc: func() (ServiceManagerConnection, error) {
			return mockConn, nil
		},
	}
	
	provider := NewServiceManagerProviderWithManager(mockManager)
	
	err := cli.uninstallService(provider)
	
	if err == nil {
		t.Error("expected error for delete failure")
	}
}

func TestRealServiceManagerStructs(t *testing.T) {
	// Test that RealServiceManager implements ServiceManager
	var _ ServiceManager = &RealServiceManager{}
}

func TestServiceHandleMethods(t *testing.T) {
	// Create a mock handle for testing
	handle := &MockServiceHandle{
		CloseFunc:   func() error { return nil },
		StartFunc:   func(args ...string) error { return nil },
		ControlFunc: func(cmd svc.Cmd) (svc.Status, error) { return svc.Status{}, nil },
		QueryFunc:   func() (svc.Status, error) { return svc.Status{}, nil },
		DeleteFunc:  func() error { return nil },
	}
	
	// Verify all methods work
	if err := handle.Close(); err != nil {
		t.Errorf("Close error: %v", err)
	}
	
	if err := handle.Start(); err != nil {
		t.Errorf("Start error: %v", err)
	}
	
	if _, err := handle.Control(svc.Stop); err != nil {
		t.Errorf("Control error: %v", err)
	}
	
	if _, err := handle.Query(); err != nil {
		t.Errorf("Query error: %v", err)
	}
	
	if err := handle.Delete(); err != nil {
		t.Errorf("Delete error: %v", err)
	}
}

func TestServiceManagerConnectionMethods(t *testing.T) {
	conn := &MockServiceManagerConnection{
		DisconnectFunc: func() error { return nil },
		OpenServiceFunc: func(name string) (ServiceHandle, error) {
			return &MockServiceHandle{}, nil
		},
		CreateServiceFunc: func(name, exepath string, config mgr.Config, args ...string) (ServiceHandle, error) {
			return &MockServiceHandle{}, nil
		},
	}
	
	if err := conn.Disconnect(); err != nil {
		t.Errorf("Disconnect error: %v", err)
	}
	
	if _, err := conn.OpenService("test"); err != nil {
		t.Errorf("OpenService error: %v", err)
	}
	
	if _, err := conn.CreateService("test", "/path/to/exe", mgr.Config{}); err != nil {
		t.Errorf("CreateService error: %v", err)
	}
}
