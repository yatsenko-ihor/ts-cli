package commands

import (
	"github.com/spf13/cobra"
)

const version = "0.1.0"

// NewRootCommand creates the root command for ts-cli
func NewRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "ts-cli",
		Short:   "Tailscale CLI - Manage Tailscale devices via API",
		Long:    `A command-line interface tool for managing Tailscale devices and resources via the Tailscale REST API.`,
		Version: version,
		// Run interactive mode by default when no subcommand is provided
		Run: func(cmd *cobra.Command, args []string) {
			// If no subcommand, run interactive mode
			interactiveCmd := NewInteractiveCommand()
			interactiveCmd.Run(cmd, args)
		},
	}

	// Add subcommands
	rootCmd.AddCommand(NewLoginCommand())
	rootCmd.AddCommand(NewListCommand())
	rootCmd.AddCommand(NewInteractiveCommand())

	return rootCmd
}
