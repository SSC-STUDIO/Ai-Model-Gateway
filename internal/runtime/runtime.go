package runtime

import (
	"context"
	"fmt"
	"os"

	v2app "ai-model-gateway/internal/app"
	"ai-model-gateway/internal/infra/configloader"
)

// RuntimeRunner starts the v2 gateway runtime with a concrete config path.
type RuntimeRunner func(ctx context.Context, configPath string) error

var defaultRuntimeRunner RuntimeRunner = v2app.Run

// RunGatewayRuntime starts the v2 runtime with the given config.
func RunGatewayRuntime(ctx context.Context, configPath string, runner RuntimeRunner) error {
	if runner == nil {
		runner = defaultRuntimeRunner
	}

	// Validate config can be loaded
	if _, err := configloader.LoadFromFile(configPath); err != nil {
		return fmt.Errorf("load runtime config %s: %w", configPath, err)
	}

	return runner(ctx, configPath)
}

// ValidateConfig checks if the config file can be loaded successfully.
func ValidateConfig(configPath string) error {
	if _, err := configloader.LoadFromFile(configPath); err != nil {
		return fmt.Errorf("validate config %s: %w", configPath, err)
	}
	return nil
}

// ConfigExists checks if the config file exists.
func ConfigExists(configPath string) bool {
	_, err := os.Stat(configPath)
	return err == nil
}