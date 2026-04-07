//go:build windows

package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const Name = "AIModelGateway"

const (
	serviceName        = "AIModelGateway"
	serviceDisplayName = "AI Model Gateway"
	serviceDescription = "AI Model Gateway v2 service"
)

var gatewayRuntimeRunner RuntimeRunner = func(ctx context.Context, configPath string) error {
	return RunGatewayRuntime(ctx, configPath, nil)
}

var serviceStartPendingDelay = 1500 * time.Millisecond

// IsWindowsService checks if the process is running as a Windows service.
func IsWindowsService() bool {
	isService, err := svc.IsWindowsService()
	return err == nil && isService
}

// TryRunService attempts to run as a Windows service if in service mode.
// Returns true if running as service, false otherwise.
func TryRunService(configPath string) bool {
	isService, err := svc.IsWindowsService()
	if err != nil || !isService {
		return false
	}

	if err := svc.Run(Name, &gatewayService{configPath: configPath}); err != nil {
		fmt.Fprintf(os.Stderr, "run windows service: %v\n", err)
		return true
	}
	return true
}

// InstallService installs the gateway as a Windows service.
func InstallService(configPath string) error {
	exepath, err := exec.LookPath(os.Args[0])
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer m.Disconnect()

	// Check if already installed
	s, err := m.OpenService(serviceName)
	if err == nil {
		s.Close()
		return fmt.Errorf("service already installed")
	}

	// Create service
	config := mgr.Config{
		DisplayName: serviceDisplayName,
		Description: serviceDescription,
		StartType:   mgr.StartAutomatic,
	}

	s, err = m.CreateService(serviceName, exepath, config, "-config", configPath)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()

	fmt.Printf("✓ Service installed successfully\n")
	fmt.Printf("  Config path: %s\n", configPath)
	fmt.Println("  Run 'gateway service-start' to start the service")
	return nil
}

// UninstallService uninstalls the Windows service.
func UninstallService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("open service (not installed): %w", err)
	}
	defer s.Close()

	// Stop service if running
	status, err := s.Query()
	if err == nil && status.State != svc.Stopped {
		fmt.Println("  Stopping service...")
		_, err = s.Control(svc.Stop)
		if err == nil {
			time.Sleep(2 * time.Second)
		}
	}

	// Delete service
	if err := s.Delete(); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}

	fmt.Println("✓ Service uninstalled successfully")
	return nil
}

// StartService starts the Windows service.
func StartService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("open service (not installed): %w", err)
	}
	defer s.Close()

	if err := s.Start(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}

	fmt.Println("✓ Service started successfully")
	return nil
}

// StopService stops the Windows service.
func StopService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("open service (not installed): %w", err)
	}
	defer s.Close()

	_, err = s.Control(svc.Stop)
	if err != nil {
		return fmt.Errorf("stop service: %w", err)
	}

	fmt.Println("✓ Service stopped successfully")
	return nil
}

// QueryServiceStatus queries the Windows service status.
func QueryServiceStatus() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		fmt.Println("  Service not installed")
		return nil
	}
	defer s.Close()

	status, err := s.Query()
	if err != nil {
		return fmt.Errorf("query service: %w", err)
	}

	stateName := map[svc.State]string{
		svc.Stopped:           "Stopped",
		svc.StartPending:      "StartPending",
		svc.StopPending:       "StopPending",
		svc.Running:           "Running",
		svc.ContinuePending:   "ContinuePending",
		svc.PausePending:      "PausePending",
		svc.Paused:            "Paused",
	}

	stateStr := "Unknown"
	if name, ok := stateName[status.State]; ok {
		stateStr = name
	}

	fmt.Printf("  Service status: %s\n", stateStr)
	return nil
}

// RunService runs the gateway as a Windows service (called by service entry point).
func RunService(configPath string) error {
	return svc.Run(serviceName, &gatewayService{configPath: configPath})
}

type gatewayService struct {
	configPath string
}

func (g *gatewayService) Execute(args []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown

	status <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- gatewayRuntimeRunner(ctx, g.configPath)
	}()

	timer := time.NewTimer(serviceStartPendingDelay)
	defer timer.Stop()

	select {
	case err := <-errCh:
		if err != nil {
			return false, 1
		}
		return false, 0
	case <-timer.C:
		status <- svc.Status{State: svc.Running, Accepts: accepted}
	}

	for {
		select {
		case change := <-req:
			switch change.Cmd {
			case svc.Interrogate:
				status <- change.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				select {
				case err := <-errCh:
					if err != nil {
						return false, 1
					}
				case <-time.After(5 * time.Second):
				}
				return false, 0
			default:
			}
		case err := <-errCh:
			if err != nil {
				return false, 1
			}
			return false, 0
		}
	}
}
