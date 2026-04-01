// Package cli provides command-line interface support for the gateway.
package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"ai-model-gateway/internal/config"
)

// Command represents a CLI command
type Command struct {
	Name        string
	Description string
	Usage       string
	Flags       *flag.FlagSet
	Run         func(args []string) error
}

// CLI manages available commands
type CLI struct {
	commands   map[string]*Command
	configPath string
	osExit     func(int) // injectable os.Exit for testing
}

// New creates a new CLI instance
func New() *CLI {
	c := &CLI{
		commands:   make(map[string]*Command),
		configPath: "configs/config.yaml",
		osExit:     os.Exit,
	}
	c.registerCommands()
	return c
}

// NewWithOptions creates a new CLI instance with custom options
func NewWithOptions(configPath string, osExit func(int)) *CLI {
	if configPath == "" {
		configPath = "configs/config.yaml"
	}
	if osExit == nil {
		osExit = os.Exit
	}
	c := &CLI{
		commands:   make(map[string]*Command),
		configPath: configPath,
		osExit:     osExit,
	}
	c.registerCommands()
	return c
}

// Register adds a command to the CLI
func (c *CLI) Register(cmd *Command) {
	c.commands[cmd.Name] = cmd
}

// Run executes the CLI
func (c *CLI) Run(args []string) error {
	if len(args) < 1 {
		c.printUsage()
		return nil
	}

	cmdName := args[0]

	// Handle global flags before command
	if cmdName == "-config" && len(args) >= 2 {
		c.configPath = args[1]
		args = args[2:]
		if len(args) < 1 {
			c.printUsage()
			return nil
		}
		cmdName = args[0]
	}

	// Special case for help
	if cmdName == "help" || cmdName == "-h" || cmdName == "--help" {
		if len(args) > 1 {
			c.printCommandHelp(args[1])
		} else {
			c.printUsage()
		}
		return nil
	}

	// Special case for version
	if cmdName == "version" || cmdName == "-v" || cmdName == "--version" {
		c.printVersion()
		return nil
	}

	cmd, exists := c.commands[cmdName]
	if !exists {
		// If no command specified, default to "start"
		if cmdName == "" || strings.HasPrefix(cmdName, "-") {
			cmd = c.commands["start"]
		} else {
			return fmt.Errorf("unknown command: %s\nRun 'gateway help' for usage", cmdName)
		}
	}

	// Parse command-specific flags
	if cmd.Flags != nil {
		if err := cmd.Flags.Parse(args[1:]); err != nil {
			return fmt.Errorf("parse flags: %w", err)
		}
		return cmd.Run(cmd.Flags.Args())
	}

	return cmd.Run(args[1:])
}

func (c *CLI) printUsage() {
	fmt.Fprintf(os.Stderr, `AI Model Gateway - A high-performance AI model proxy gateway

Usage: gateway [global-options] <command> [options]

Global Options:
  -config string    Path to config file (default "configs/config.yaml")

Commands:
`)
	for name, cmd := range c.commands {
		fmt.Fprintf(os.Stderr, "  %-12s %s\n", name, cmd.Description)
	}
	fmt.Fprintf(os.Stderr, `
  %-12s %s
  %-12s %s
`, "help", "Show help for a command", "version", "Show version information")
	fmt.Fprintf(os.Stderr, `
Run 'gateway help <command>' for more information about a command.
`)
}

func (c *CLI) printCommandHelp(cmdName string) {
	cmd, exists := c.commands[cmdName]
	if !exists {
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmdName)
		c.osExit(1)
		return
	}
	fmt.Fprintf(os.Stderr, "Usage: gateway %s\n\n%s\n", cmd.Usage, cmd.Description)
	if cmd.Flags != nil {
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		cmd.Flags.PrintDefaults()
	}
}

// Version is set at build time via ldflags
var Version = "dev"

func (c *CLI) printVersion() {
	if Version == "" {
		Version = "dev"
	}
	fmt.Printf("AI Model Gateway version %s\n", Version)
}

// GetConfigPath returns the configured config file path
func (c *CLI) GetConfigPath() string {
	return c.configPath
}

// SetConfigPath sets the config file path
func (c *CLI) SetConfigPath(path string) {
	c.configPath = path
}

// LoadConfig loads the configuration file
func (c *CLI) LoadConfig() (*config.Config, error) {
	cfg, err := config.LoadFromFile(c.configPath)
	if err != nil {
		return nil, fmt.Errorf("load config from %s: %w", c.configPath, err)
	}
	return &cfg, nil
}

// ContextWithTimeout creates a context with the specified timeout
func ContextWithTimeout(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}

// GetCommands returns a copy of the registered commands (for testing)
func (c *CLI) GetCommands() map[string]*Command {
	result := make(map[string]*Command, len(c.commands))
	for k, v := range c.commands {
		result[k] = v
	}
	return result
}
