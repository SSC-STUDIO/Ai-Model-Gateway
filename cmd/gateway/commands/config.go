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
	view, err := c.client.GetConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	if format == "json" {
		encoder := json.NewEncoder(c.output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(view.Current)
	}

	fmt.Fprintf(c.output, "Revision: %s\n", view.Current.RevisionID)
	fmt.Fprintf(c.output, "Created: %s\n", view.Current.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintln(c.output, "Config:")
	encoder := json.NewEncoder(c.output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(view.Current.Config)
}
