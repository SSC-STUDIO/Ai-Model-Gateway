package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"ai-model-gateway/internal/cli"
)

type ReloadCommand struct {
	client *cli.ControlPlaneClient
	output io.Writer
}

func NewReloadCommand(client *cli.ControlPlaneClient, output io.Writer) *ReloadCommand {
	return &ReloadCommand{client: client, output: output}
}

func (c *ReloadCommand) Reload(ctx context.Context, format string) error {
	status, err := c.client.GetStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to get status before reload: %w", err)
	}

	if status.GatewayStatus != "connected" {
		return fmt.Errorf("gateway not connected, cannot reload")
	}

	fmt.Fprintf(c.output, "Triggering configuration reload...\n")
	result, err := c.client.ReloadConfig(ctx)
	if err != nil {
		if format == "json" {
			return json.NewEncoder(c.output).Encode(map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
		}
		return fmt.Errorf("reload failed: %w", err)
	}

	if format == "json" {
		return json.NewEncoder(c.output).Encode(map[string]interface{}{
			"success":      true,
			"revision_id":  result.RevisionID,
			"published_at": result.PublishedAt,
		})
	}

	fmt.Fprintf(c.output, "Reload initiated successfully\n")
	if result.RevisionID != "" {
		fmt.Fprintf(c.output, "Revision: %s\n", result.RevisionID)
	}

	return nil
}
