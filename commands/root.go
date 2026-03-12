package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Version is the current version of ts-cli
const Version = "0.1.0"

// NewRootCommand creates the root command for ts-cli
func NewRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "ts-cli",
		Short:   "Tailscale CLI - Manage Tailscale devices via API",
		Long:    `A command-line interface tool for managing Tailscale devices and resources via the Tailscale REST API.`,
		Version: Version,
		// Run interactive mode by default when no subcommand is provided
		Run: func(cmd *cobra.Command, args []string) {
			// If no subcommand, run interactive mode
			interactiveCmd := NewInteractiveCommand()
			if err := interactiveCmd.RunE(cmd, args); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
	}

	// Add subcommands
	rootCmd.AddCommand(NewLoginCommand())
	rootCmd.AddCommand(NewListCommand())
	rootCmd.AddCommand(NewInteractiveCommand())
	rootCmd.AddCommand(NewSSHCommand())
	rootCmd.AddCommand(NewUpCommand())
	rootCmd.AddCommand(NewAccountCommand())
	rootCmd.AddCommand(NewInstallCommand())

	return rootCmd
}
