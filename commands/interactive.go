package commands

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ihor/ts-cli/client"
	"github.com/ihor/ts-cli/tui"
	"github.com/spf13/cobra"
)

// NewInteractiveCommand creates the interactive TUI command
func NewInteractiveCommand() *cobra.Command {
	var apiKey string
	var tailnet string

	cmd := &cobra.Command{
		Use:     "interactive",
		Aliases: []string{"i", "tui"},
		Short:   "Launch interactive TUI to browse and manage devices",
		Long: `Launch an interactive terminal UI for browsing Tailscale devices.
Use arrow keys or j/k to navigate, Enter to view details, and q to quit.`,
		Example: `  # Launch interactive mode
  ts-cli interactive

  # With specific tailnet
  ts-cli interactive --tailnet=example.com

  # Short alias
  ts-cli i`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load configuration from stored config if not provided
			if apiKey == "" || tailnet == "" {
				storedAPIKey, storedTailnet, err := loadConfig()
				if err == nil {
					if apiKey == "" {
						apiKey = storedAPIKey
					}
					if tailnet == "" {
						tailnet = storedTailnet
					}
				}
			}

			// Try environment variable as fallback
			if apiKey == "" {
				apiKey = os.Getenv("TAILSCALE_API_KEY")
			}

			if apiKey == "" {
				return fmt.Errorf("API key not provided.\nRun 'ts-cli login' first or set TAILSCALE_API_KEY environment variable")
			}

			if tailnet == "" {
				return fmt.Errorf("tailnet name not configured.\nRun 'ts-cli login --tailnet=<name>' first or use --tailnet flag")
			}

			// Fetch devices
			apiClient := client.NewClient(apiKey)
			devices, err := apiClient.ListDevices(tailnet)
			if err != nil {
				return fmt.Errorf("failed to list devices: %w", err)
			}

			if len(devices) == 0 {
				fmt.Println("No devices found in your tailnet.")
				return nil
			}

			// Load SSH username preference
			sshUsername, _ := LoadSSHUsername()

			// Launch TUI
			m := tui.NewModel(devices, Version, sshUsername)
			p := tea.NewProgram(m, tea.WithAltScreen())
			if _, err := p.Run(); err != nil {
				return fmt.Errorf("TUI error: %w", err)
			}

			return nil
		},
	}

	// Define flags
	cmd.Flags().StringVar(&apiKey, "api-key", "", "Tailscale API key (overrides TAILSCALE_API_KEY env var)")
	cmd.Flags().StringVar(&tailnet, "tailnet", "", "Tailnet name (overrides stored configuration)")

	return cmd
}
