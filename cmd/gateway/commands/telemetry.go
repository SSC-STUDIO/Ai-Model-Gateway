package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"ai-model-gateway/internal/cli"
)

type TelemetryCommand struct {
	client *cli.ControlPlaneClient
	output io.Writer
}

func NewTelemetryCommand(client *cli.ControlPlaneClient, output io.Writer) *TelemetryCommand {
	return &TelemetryCommand{client: client, output: output}
}

func (t *TelemetryCommand) Events(ctx context.Context, windowHours int, format string) error {
	if windowHours <= 0 {
		windowHours = 24
	}

	query := &cli.TelemetryQuery{
		WindowHours: windowHours,
		Limit:       100,
	}

	result, err := t.client.GetTelemetry(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to get telemetry: %w", err)
	}

	if format == "json" {
		encoder := json.NewEncoder(t.output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}

	w := tabwriter.NewWriter(t.output, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TIME\tREQUEST\tMODEL\tPROVIDER\tSTATUS\tLATENCY\tTOKENS")
	for _, event := range result.Events {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%dms\t%d\n",
			event.Timestamp.Format("15:04:05"),
			event.RequestID[:8],
			event.Model,
			event.Provider,
			event.StatusCode,
			event.LatencyMs,
			event.InputTokens+event.OutputTokens,
		)
	}
	return w.Flush()
}
