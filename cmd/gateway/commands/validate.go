package commands

import (
	"encoding/json"
	"fmt"
	"io"

	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/infra/configloader"
)

type ValidateCommand struct {
	output io.Writer
}

func NewValidateCommand(output io.Writer) *ValidateCommand {
	return &ValidateCommand{output: output}
}

func (c *ValidateCommand) Validate(path string, format string) error {
	cfg, err := configloader.LoadFromFile(path)
	if err != nil {
		if format == "json" {
			return json.NewEncoder(c.output).Encode(map[string]interface{}{
				"valid": false,
				"error": err.Error(),
				"file":  path,
			})
		}
		return fmt.Errorf("validation failed: %w", err)
	}

	if format == "json" {
		return json.NewEncoder(c.output).Encode(map[string]interface{}{
			"valid":     true,
			"file":      path,
			"providers": len(cfg.Providers),
			"models":    countModels(cfg),
		})
	}

	fmt.Fprintf(c.output, "Configuration valid: %s\n", path)
	fmt.Fprintf(c.output, "Providers: %d\n", len(cfg.Providers))
	fmt.Fprintf(c.output, "Models: %d\n", countModels(cfg))
	fmt.Fprintf(c.output, "Server listen: %s\n", cfg.Server.Listen)

	if cfg.Admin.Enabled {
		fmt.Fprintf(c.output, "Admin: enabled (port from server.listen)\n")
	}
	if cfg.Telemetry.SQLitePath != "" {
		fmt.Fprintf(c.output, "Telemetry DB: %s\n", cfg.Telemetry.SQLitePath)
	}

	return nil
}

func countModels(cfg *core.Config) int {
	if cfg == nil {
		return 0
	}
	seen := make(map[string]struct{})
	for _, p := range cfg.Providers {
		for _, m := range p.Models {
			seen[m] = struct{}{}
		}
	}
	return len(seen)
}
