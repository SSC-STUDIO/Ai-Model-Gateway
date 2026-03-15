//go:build windows

package service

import (
	"context"
	"fmt"
	"time"

	"ai-model-gateway/internal/app"
	"golang.org/x/sys/windows/svc"
)

const Name = "AIModelGateway"

func Run(configPath string) (bool, error) {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return false, fmt.Errorf("detect windows service: %w", err)
	}
	if !isService {
		return false, nil
	}

	if err := svc.Run(Name, &gatewayService{configPath: configPath}); err != nil {
		return true, fmt.Errorf("run windows service: %w", err)
	}
	return true, nil
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
		errCh <- app.Run(ctx, g.configPath)
	}()

	timer := time.NewTimer(1500 * time.Millisecond)
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
