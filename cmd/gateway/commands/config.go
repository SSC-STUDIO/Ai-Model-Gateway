package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"ai-model-gateway/internal/cli"
)

type ConfigCommand struct {
	client *cli.ControlPlaneClient
	output io.Writer
}

func NewConfigCommand(client *cli.ControlPlaneClient, output io.Writer) *ConfigCommand {
	return &ConfigCommand{client: client, output: output}
}

func (c *ConfigCommand) Show(ctx context.Context, format string) error {
	resp, err := c.client.GetConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	if format == "json" {
		encoder := json.NewEncoder(c.output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)
	}

	// Text format
	if resp.Revision != nil {
		fmt.Fprintf(c.output, "Revision: %s\n", resp.Revision.RevisionID)
		fmt.Fprintf(c.output, "Created: %s\n", resp.Revision.CreatedAt.Format("2006-01-02 15:04:05"))
		if resp.Revision.CreatedBy != "" {
			fmt.Fprintf(c.output, "Created By: %s\n", resp.Revision.CreatedBy)
		}
		if resp.Revision.Description != "" {
			fmt.Fprintf(c.output, "Description: %s\n", resp.Revision.Description)
		}
		fmt.Fprintln(c.output, "Config:")
		if resp.Revision.Config != nil {
			encoder := json.NewEncoder(c.output)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(resp.Revision.Config); err != nil {
				return err
			}
		}
	}
	fmt.Fprintf(c.output, "Auto Publish: %v\n", resp.Policy.AutoPublish)
	return nil
}
