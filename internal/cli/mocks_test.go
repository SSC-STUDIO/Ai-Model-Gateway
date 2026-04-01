//go:build windows

package cli

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// --- Mock HTTP Client ---

type MockHTTPClient struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

func (m *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.DoFunc != nil {
		return m.DoFunc(req)
	}
	return nil, errors.New("DoFunc not implemented")
}

// --- Mock Service Manager (Windows only) ---

type MockServiceManager struct {
	ConnectFunc func() (ServiceManagerConnection, error)
}

func (m *MockServiceManager) Connect() (ServiceManagerConnection, error) {
	if m.ConnectFunc != nil {
		return m.ConnectFunc()
	}
	return &MockServiceManagerConnection{}, nil
}

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

// --- Mock App Runner ---

type MockAppRunner struct {
	RunFunc func(ctx context.Context, configPath string) error
	Called  bool
	Ctx     context.Context
	Path    string
}

func (m *MockAppRunner) Run(ctx context.Context, configPath string) error {
	m.Called = true
	m.Ctx = ctx
	m.Path = configPath
	if m.RunFunc != nil {
		return m.RunFunc(ctx, configPath)
	}
	return nil
}

// --- Mock OS Exit ---

type MockOSExit struct {
	Called bool
	Code   int
}

func (m *MockOSExit) Exit(code int) {
	m.Called = true
	m.Code = code
}

// --- Mock File Info ---

type MockFileInfo struct {
	NameFunc    func() string
	SizeFunc    func() int64
	ModeFunc    func() os.FileMode
	ModTimeFunc func() time.Time
	IsDirFunc   func() bool
	SysFunc     func() interface{}
}

func (m *MockFileInfo) Name() string       { return m.NameFunc() }
func (m *MockFileInfo) Size() int64        { return m.SizeFunc() }
func (m *MockFileInfo) Mode() os.FileMode  { return m.ModeFunc() }
func (m *MockFileInfo) ModTime() time.Time { return m.ModTimeFunc() }
func (m *MockFileInfo) IsDir() bool        { return m.IsDirFunc() }
func (m *MockFileInfo) Sys() interface{}   { return m.SysFunc() }

// --- Mock Stat Function ---

type MockStat struct {
	StatFunc func(name string) (os.FileInfo, error)
}

func (m *MockStat) Stat(name string) (os.FileInfo, error) {
	if m.StatFunc != nil {
		return m.StatFunc(name)
	}
	return nil, os.ErrNotExist
}
