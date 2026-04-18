package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"ai-model-gateway/internal/cli"
)

type ProviderCommand struct {
	client *cli.ControlPlaneClient
	output io.Writer
}

func NewProviderCommand(client *cli.ControlPlaneClient, output io.Writer) *ProviderCommand {
	return &ProviderCommand{client: client, output: output}
}

func (p *ProviderCommand) List(ctx context.Context, format string) error {
	providers, err := p.client.ListProviders(ctx)
	if err != nil {
		return fmt.Errorf("failed to list providers: %w", err)
	}

	if format == "json" {
		encoder := json.NewEncoder(p.output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(providers)
	}

	w := tabwriter.NewWriter(p.output, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATUS\tHEALTHY\tLATENCY\tMODELS")
	for _, provider := range providers {
		healthy := "✓"
		if !provider.Healthy {
			healthy = "✗"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%dms\t%d\n",
			provider.Name, provider.Status, healthy,
			provider.LatencyMs, len(provider.Models))
	}
	return w.Flush()
}

func (p *ProviderCommand) Test(ctx context.Context, name string) error {
	result, err := p.client.TestProvider(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to test provider %s: %w", name, err)
	}

	fmt.Fprintf(p.output, "Provider: %s\n", name)
	fmt.Fprintf(p.output, "Status: %s\n", result.Status)
	fmt.Fprintf(p.output, "Latency: %dms\n", result.LatencyMs)
	if result.LastError != "" {
		fmt.Fprintf(p.output, "Error: %s\n", result.LastError)
	}
	return nil
}
