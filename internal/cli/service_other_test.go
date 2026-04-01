//go:build !windows

package cli

import (
	"runtime"
	"strings"
	"testing"
)

func TestNewServiceManagerProvider(t *testing.T) {
	provider := NewServiceManagerProvider()
	if provider == nil {
		t.Fatal("NewServiceManagerProvider() returned nil")
	}
}

func TestCmdServiceInstallNotSupported(t *testing.T) {
	cli := New()

	err := cli.cmdServiceInstall([]string{})

	if err == nil {
		t.Error("expected error on non-Windows platform")
	}

	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("expected 'not supported' error, got %v", err)
	}

	if !strings.Contains(err.Error(), runtime.GOOS) {
		t.Errorf("expected error to mention OS, got %v", err)
	}
}

func TestCmdServiceUninstallNotSupported(t *testing.T) {
	cli := New()

	err := cli.cmdServiceUninstall([]string{})

	if err == nil {
		t.Error("expected error on non-Windows platform")
	}
}

func TestCmdServiceStartNotSupported(t *testing.T) {
	cli := New()

	err := cli.cmdServiceStart([]string{})

	if err == nil {
		t.Error("expected error on non-Windows platform")
	}
}

func TestCmdServiceStopNotSupported(t *testing.T) {
	cli := New()

	err := cli.cmdServiceStop([]string{})

	if err == nil {
		t.Error("expected error on non-Windows platform")
	}
}

func TestCmdServiceStatusNotSupported(t *testing.T) {
	cli := New()

	err := cli.cmdServiceStatus([]string{})

	if err == nil {
		t.Error("expected error on non-Windows platform")
	}
}

func TestInstallServiceNotSupported(t *testing.T) {
	cli := New()
	provider := NewServiceManagerProvider()

	err := cli.installService(provider)

	if err == nil {
		t.Error("expected error on non-Windows platform")
	}

	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("expected 'not supported' error, got %v", err)
	}
}

func TestUninstallServiceNotSupported(t *testing.T) {
	cli := New()
	provider := NewServiceManagerProvider()

	err := cli.uninstallService(provider)

	if err == nil {
		t.Error("expected error on non-Windows platform")
	}
}

func TestStartServiceNotSupported(t *testing.T) {
	cli := New()
	provider := NewServiceManagerProvider()

	err := cli.startService(provider)

	if err == nil {
		t.Error("expected error on non-Windows platform")
	}
}

func TestStopServiceNotSupported(t *testing.T) {
	cli := New()
	provider := NewServiceManagerProvider()

	err := cli.stopService(provider)

	if err == nil {
		t.Error("expected error on non-Windows platform")
	}
}

func TestServiceStatusNotSupported(t *testing.T) {
	cli := New()
	provider := NewServiceManagerProvider()

	err := cli.serviceStatus(provider)

	if err == nil {
		t.Error("expected error on non-Windows platform")
	}
}
