//go:build !windows

package runtime

import "fmt"

// IsWindowsService always returns false on non-Windows platforms.
func IsWindowsService() bool { return false }

// TryRunService always returns false on non-Windows platforms.
func TryRunService(configPath string) bool { return false }

// InstallService returns an error on non-Windows platforms.
func InstallService(configPath string) error {
	return fmt.Errorf("Windows service management not available on this platform")
}

// UninstallService returns an error on non-Windows platforms.
func UninstallService() error {
	return fmt.Errorf("Windows service management not available on this platform")
}

// StartService returns an error on non-Windows platforms.
func StartService() error {
	return fmt.Errorf("Windows service management not available on this platform")
}

// StopService returns an error on non-Windows platforms.
func StopService() error {
	return fmt.Errorf("Windows service management not available on this platform")
}

// QueryServiceStatus prints a message on non-Windows platforms.
func QueryServiceStatus() error {
	fmt.Println("  Service management not available on this platform")
	return nil
}

// RunService returns an error on non-Windows platforms.
func RunService(configPath string) error {
	return fmt.Errorf("Windows service management not available on this platform")
}
