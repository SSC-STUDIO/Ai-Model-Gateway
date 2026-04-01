//go:build windows

package cli

import (
	"errors"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// MockServiceManager mocks the ServiceManager interface (Windows only)
type MockServiceManager struct {
	ConnectFunc func() (ServiceManagerConnection, error)
}

func (m *MockServiceManager) Connect() (ServiceManagerConnection, error) {
	if m.ConnectFunc != nil {
		return m.ConnectFunc()
	}
	return &MockServiceManagerConnection{}, nil
}

// MockServiceManagerConnection mocks the ServiceManagerConnection interface (Windows only)
type MockServiceManagerConnection struct {
	DisconnectFunc    func() error
	OpenServiceFunc   func(name string) (ServiceHandle, error)
	CreateServiceFunc func(name, exepath string, config mgr.Config, args ...string) (ServiceHandle, error)
}

func (m *MockServiceManagerConnection) Disconnect() error {
	if m.DisconnectFunc != nil {
		return m.DisconnectFunc()
	}
	return nil
}

func (m *MockServiceManagerConnection) OpenService(name string) (ServiceHandle, error) {
	if m.OpenServiceFunc != nil {
		return m.OpenServiceFunc(name)
	}
	return nil, errors.New("service not found")
}

func (m *MockServiceManagerConnection) CreateService(name, exepath string, config mgr.Config, args ...string) (ServiceHandle, error) {
	if m.CreateServiceFunc != nil {
		return m.CreateServiceFunc(name, exepath, config, args...)
	}
	return &MockServiceHandle{}, nil
}

// MockServiceHandle mocks the ServiceHandle interface (Windows only)
type MockServiceHandle struct {
	CloseFunc   func() error
	StartFunc   func(args ...string) error
	ControlFunc func(cmd svc.Cmd) (svc.Status, error)
	QueryFunc   func() (svc.Status, error)
	DeleteFunc  func() error
}

func (m *MockServiceHandle) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

func (m *MockServiceHandle) Start(args ...string) error {
	if m.StartFunc != nil {
		return m.StartFunc(args...)
	}
	return nil
}

func (m *MockServiceHandle) Control(cmd svc.Cmd) (svc.Status, error) {
	if m.ControlFunc != nil {
		return m.ControlFunc(cmd)
	}
	return svc.Status{}, nil
}

func (m *MockServiceHandle) Query() (svc.Status, error) {
	if m.QueryFunc != nil {
		return m.QueryFunc()
	}
	return svc.Status{State: svc.Stopped}, nil
}

func (m *MockServiceHandle) Delete() error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc()
	}
	return nil
}
