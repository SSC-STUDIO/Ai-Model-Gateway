//go:build windows

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const serviceName = "AIModelGateway"
const serviceDisplayName = "AI Model Gateway"
const serviceDescription = "High-performance AI model proxy gateway service"

// ServiceManagerProvider provides a ServiceManager instance
type ServiceManagerProvider struct {
	manager ServiceManager
}

// NewServiceManagerProvider creates a new provider with the real service manager
func NewServiceManagerProvider() *ServiceManagerProvider {
	return &ServiceManagerProvider{
		manager: &RealServiceManager{},
	}
}

// NewServiceManagerProviderWithManager creates a new provider with a custom manager
func NewServiceManagerProviderWithManager(manager ServiceManager) *ServiceManagerProvider {
	return &ServiceManagerProvider{
		manager: manager,
	}
}

func (c *CLI) cmdServiceInstall(args []string) error {
	provider := NewServiceManagerProvider()
	return c.installService(provider)
}

func (c *CLI) installService(provider *ServiceManagerProvider) error {
	fmt.Println("Installing service...")

	exepath, err := exec.LookPath(os.Args[0])
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}

	m, err := provider.manager.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err == nil {
		s.Close()
		return fmt.Errorf("service %s already installed", serviceName)
	}

	config := mgr.Config{
		DisplayName: serviceDisplayName,
		Description: serviceDescription,
		StartType:   mgr.StartAutomatic,
	}

	s, err = m.CreateService(serviceName, exepath, config, "-config", c.configPath)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()

	fmt.Printf("✓ Service '%s' installed successfully\n", serviceName)
	fmt.Printf("  Executable: %s\n", exepath)
	fmt.Printf("  Config: %s\n", c.configPath)
	fmt.Println("\nTo start the service, run: gateway service-start")

	return nil
}

func (c *CLI) cmdServiceUninstall(args []string) error {
	provider := NewServiceManagerProvider()
	return c.uninstallService(provider)
}

func (c *CLI) uninstallService(provider *ServiceManagerProvider) error {
	fmt.Println("Uninstalling service...")

	m, err := provider.manager.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service not found: %s", serviceName)
	}
	defer s.Close()

	// Check if running
	status, err := s.Query()
	if err != nil {
		return fmt.Errorf("query service status: %w", err)
	}

	if status.State != svc.Stopped {
		fmt.Println("Stopping service first...")
		if _, err := s.Control(svc.Stop); err != nil {
			return fmt.Errorf("stop service: %w", err)
		}
		// Wait for service to stop
		time.Sleep(2 * time.Second)
	}

	if err := s.Delete(); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}

	fmt.Printf("✓ Service '%s' uninstalled successfully\n", serviceName)
	return nil
}

func (c *CLI) cmdServiceStart(args []string) error {
	provider := NewServiceManagerProvider()
	return c.startService(provider)
}

func (c *CLI) startService(provider *ServiceManagerProvider) error {
	m, err := provider.manager.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service not found: %s (run 'gateway install' first)", serviceName)
	}
	defer s.Close()

	if err := s.Start(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}

	fmt.Printf("✓ Service '%s' started\n", serviceName)
	return nil
}

func (c *CLI) cmdServiceStop(args []string) error {
	provider := NewServiceManagerProvider()
	return c.stopService(provider)
}

func (c *CLI) stopService(provider *ServiceManagerProvider) error {
	m, err := provider.manager.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service not found: %s", serviceName)
	}
	defer s.Close()

	status, err := s.Control(svc.Stop)
	if err != nil {
		return fmt.Errorf("stop service: %w", err)
	}

	fmt.Printf("✓ Service '%s' stopping (current state: %v)\n", serviceName, status)
	return nil
}

func (c *CLI) cmdServiceStatus(args []string) error {
	provider := NewServiceManagerProvider()
	return c.serviceStatus(provider)
}

func (c *CLI) serviceStatus(provider *ServiceManagerProvider) error {
	m, err := provider.manager.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		fmt.Printf("Service '%s' is not installed\n", serviceName)
		return nil
	}
	defer s.Close()

	status, err := s.Query()
	if err != nil {
		return fmt.Errorf("query service: %w", err)
	}

	stateStr := serviceStateString(status.State)
	fmt.Printf("Service: %s\n", serviceName)
	fmt.Printf("State:   %s\n", stateStr)
	fmt.Printf("Pid:     %d\n", status.ProcessId)

	return nil
}

func serviceStateString(state svc.State) string {
	switch state {
	case svc.Stopped:
		return "Stopped"
	case svc.StartPending:
		return "Starting..."
	case svc.StopPending:
		return "Stopping..."
	case svc.Running:
		return "Running"
	case svc.ContinuePending:
		return "Resuming..."
	case svc.PausePending:
		return "Pausing..."
	case svc.Paused:
		return "Paused"
	default:
		return fmt.Sprintf("Unknown(%d)", state)
	}
}
