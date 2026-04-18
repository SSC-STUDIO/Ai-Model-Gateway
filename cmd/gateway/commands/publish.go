package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"ai-model-gateway/internal/cli"
)

type PublishCommand struct {
	client *cli.ControlPlaneClient
	output io.Writer
}

func NewPublishCommand(client *cli.ControlPlaneClient, output io.Writer) *PublishCommand {
	return &PublishCommand{client: client, output: output}
}

func (p *PublishCommand) History(ctx context.Context, limit int, format string) error {
	if limit <= 0 {
		limit = 10
	}

	revisions, err := p.client.GetConfigHistory(ctx, limit)
	if err != nil {
		return fmt.Errorf("failed to get history: %w", err)
	}

	if format == "json" {
		encoder := json.NewEncoder(p.output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(revisions)
	}

	w := tabwriter.NewWriter(p.output, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "REVISION\tCREATED\tBY\tACTIVE\tDESCRIPTION")
	for _, rev := range revisions {
		active := ""
		if rev.IsActive {
			active = "✓"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			rev.RevisionID,
			rev.CreatedAt.Format("2006-01-02 15:04"),
			rev.CreatedBy,
			active,
			rev.Description,
		)
	}
	return w.Flush()
}

func (p *PublishCommand) Rollback(ctx context.Context, revisionID string) error {
	result, err := p.client.RollbackConfig(ctx, revisionID)
	if err != nil {
		return fmt.Errorf("failed to rollback to %s: %w", revisionID, err)
	}

	if !result.Success {
		return fmt.Errorf("rollback failed: %s", result.ErrorMessage)
	}

	fmt.Fprintf(p.output, "Successfully rolled back to revision %s\n", revisionID)
	fmt.Fprintf(p.output, "Snapshot ID: %s\n", result.SnapshotID)
	return nil
}
