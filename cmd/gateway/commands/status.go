package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"ai-model-gateway/internal/cli"
)

type StatusCommand struct {
	client *cli.ControlPlaneClient
	output io.Writer
}

func NewStatusCommand(client *cli.ControlPlaneClient, output io.Writer) *StatusCommand {
	return &StatusCommand{client: client, output: output}
}

func (c *StatusCommand) Show(ctx context.Context, format string) error {
	status, err := c.client.GetStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	if format == "json" {
		encoder := json.NewEncoder(c.output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(status)
	}

	fmt.Fprintf(c.output, "Version: %s\n", status.Version)
	fmt.Fprintf(c.output, "Started: %s\n", status.StartedAt)
	fmt.Fprintf(c.output, "Uptime: %s\n", status.Uptime)
	fmt.Fprintln(c.output)
	fmt.Fprintf(c.output, "Gateway Status: %s\n", status.GatewayStatus)
	fmt.Fprintf(c.output, "Telemetry Status: %s\n", status.TelemetryStatus)

	if status.Gateway != nil {
		fmt.Fprintln(c.output)
		fmt.Fprintf(c.output, "Gateway Details:\n")
		fmt.Fprintf(c.output, "  Readiness: %s\n", status.Gateway.Readiness)
		fmt.Fprintf(c.output, "  Active Requests: %d\n", status.Gateway.ActiveRequests)
		fmt.Fprintf(c.output, "  Listener: %s\n", status.Gateway.Listener)
		fmt.Fprintf(c.output, "  Active Snapshot: %s\n", truncateID(status.Gateway.ActiveSnapshotID))

		if len(status.Gateway.ProviderHealth) > 0 {
			fmt.Fprintln(c.output)
			fmt.Fprintf(c.output, "Provider Health:\n")
			for name, health := range status.Gateway.ProviderHealth {
				statusStr := "healthy"
				if !health.Healthy {
					statusStr = "unhealthy"
				}
				fmt.Fprintf(c.output, "  %s: %s (latency: %dms)\n", name, statusStr, health.LatencyMs)
			}
		}
	}

	if status.GatewayError != "" {
		fmt.Fprintf(c.output, "\nGateway Error: %s\n", status.GatewayError)
	}

	return nil
}

func truncateID(id string) string {
	if len(id) > 12 {
		return id[:12] + "..."
	}
	return id
}

func (c *StatusCommand) Watch(ctx context.Context, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			status, err := c.client.GetStatus(ctx)
			if err != nil {
				fmt.Fprintf(c.output, "[%s] Error: %v\n", time.Now().Format("15:04:05"), err)
				continue
			}

			gatewayStatus := status.GatewayStatus
			if status.Gateway != nil {
				gatewayStatus = fmt.Sprintf("%s (%d req)", status.GatewayStatus, status.Gateway.ActiveRequests)
			}

			fmt.Fprintf(c.output, "[%s] Gateway: %s | Telemetry: %s\n",
				time.Now().Format("15:04:05"),
				gatewayStatus,
				status.TelemetryStatus,
			)
		}
	}
}
