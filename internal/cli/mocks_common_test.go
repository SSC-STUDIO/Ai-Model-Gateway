package cli

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"
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
