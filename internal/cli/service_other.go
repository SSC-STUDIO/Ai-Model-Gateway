//go:build !windows

package cli

import (
	"fmt"
	"runtime"
)

func (c *CLI) cmdServiceInstall(args []string) error {
	return fmt.Errorf("service management is not supported on %s", runtime.GOOS)
}

func (c *CLI) cmdServiceUninstall(args []string) error {
	return fmt.Errorf("service management is not supported on %s", runtime.GOOS)
}

func (c *CLI) cmdServiceStart(args []string) error {
	return fmt.Errorf("service management is not supported on %s", runtime.GOOS)
}

func (c *CLI) cmdServiceStop(args []string) error {
	return fmt.Errorf("service management is not supported on %s", runtime.GOOS)
}

func (c *CLI) cmdServiceStatus(args []string) error {
	return fmt.Errorf("service management is not supported on %s", runtime.GOOS)
}
