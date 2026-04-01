//go:build !windows

package cli

import (
	"fmt"
	"runtime"
)

// ServiceManagerProvider provides a ServiceManager instance (stub for non-Windows)
type ServiceManagerProvider struct{}

// NewServiceManagerProvider creates a new provider (stub for non-Windows)
func NewServiceManagerProvider() *ServiceManagerProvider {
	return &ServiceManagerProvider{}
}

func (c *CLI) cmdServiceInstall(args []string) error {
	return fmt.Errorf("service management is not supported on %s", runtime.GOOS)
}

func (c *CLI) installService(provider *ServiceManagerProvider) error {
	return fmt.Errorf("service management is not supported on %s", runtime.GOOS)
}

func (c *CLI) cmdServiceUninstall(args []string) error {
	return fmt.Errorf("service management is not supported on %s", runtime.GOOS)
}

func (c *CLI) uninstallService(provider *ServiceManagerProvider) error {
	return fmt.Errorf("service management is not supported on %s", runtime.GOOS)
}

func (c *CLI) cmdServiceStart(args []string) error {
	return fmt.Errorf("service management is not supported on %s", runtime.GOOS)
}

func (c *CLI) startService(provider *ServiceManagerProvider) error {
	return fmt.Errorf("service management is not supported on %s", runtime.GOOS)
}

func (c *CLI) cmdServiceStop(args []string) error {
	return fmt.Errorf("service management is not supported on %s", runtime.GOOS)
}

func (c *CLI) stopService(provider *ServiceManagerProvider) error {
	return fmt.Errorf("service management is not supported on %s", runtime.GOOS)
}

func (c *CLI) cmdServiceStatus(args []string) error {
	return fmt.Errorf("service management is not supported on %s", runtime.GOOS)
}

func (c *CLI) serviceStatus(provider *ServiceManagerProvider) error {
	return fmt.Errorf("service management is not supported on %s", runtime.GOOS)
}
