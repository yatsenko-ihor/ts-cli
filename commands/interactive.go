package commands

import (
	"fmt"

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
			// Load configuration
			config, err := LoadConfig()
			if err != nil {
				return fmt.Errorf("failed to load configuration: %w", err)
			}

			// Check if we have any accounts configured
			if len(config.Accounts) == 0 {
				return fmt.Errorf("no accounts configured.\nRun 'ts-cli login --tailnet=<name>' first to add an account")
			}

			// If specific flags are provided, use them to override
			if apiKey != "" && tailnet != "" {
				// Use the provided credentials
				apiClient := client.NewClient(apiKey)
				devices, err := apiClient.ListDevices(tailnet)
				if err != nil {
					return fmt.Errorf("failed to list devices: %w", err)
				}

				// Tag devices with account info
				for i := range devices {
					devices[i].AccountName = tailnet
					devices[i].AccountTailnet = tailnet
				}

				if len(devices) == 0 {
					fmt.Println("No devices found in the specified tailnet.")
					return nil
				}

				// Launch TUI
				m := tui.NewModel(devices, Version, config.SSHUsername)
				p := tea.NewProgram(m, tea.WithAltScreen())
				if _, err := p.Run(); err != nil {
					return fmt.Errorf("TUI error: %w", err)
				}
				return nil
			}

			// Check if Tailscale is running (warning only, doesn't block)
			WarnIfTailscaleNotRunning()

			// Fetch devices from all configured accounts
			fmt.Println("Fetching devices from all configured accounts...")
			accounts := make([]client.AccountInfo, len(config.Accounts))
			for i, acc := range config.Accounts {
				accounts[i] = client.AccountInfo{
					Name:    acc.Name,
					APIKey:  acc.APIKey,
					Tailnet: acc.Tailnet,
				}
			}

			devices := client.ListDevicesFromAccounts(accounts)
			if len(devices) == 0 {
				fmt.Println("No devices found in any of your configured accounts.")
				return nil
			}

			fmt.Printf("Found %d device(s) from %d account(s)\n", len(devices), len(config.Accounts))

			// Launch TUI
			m := tui.NewModel(devices, Version, config.SSHUsername)
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
