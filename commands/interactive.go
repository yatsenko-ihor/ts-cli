package commands

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/config"
	"github.com/yatsenko-ihor/ts-cli/tui"
	"github.com/yatsenko-ihor/ts-cli/util"
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
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load configuration: %w", err)
			}

			if len(cfg.Accounts) == 0 {
				return fmt.Errorf("no accounts configured.\nRun 'ts-cli login --tailnet=<name>' first to add an account")
			}
			if notice := util.KeyStoreNotice(); notice != "" {
				fmt.Fprintf(os.Stderr, "Warning: %s\n", notice)
			}

			var devices []client.Device
			var accounts []client.AccountInfo

			if apiKey != "" && tailnet != "" {
				// Explicit credentials override the stored accounts
				apiClient := client.NewClient(apiKey)
				devices, err = apiClient.ListDevices(tailnet)
				if err != nil {
					return fmt.Errorf("failed to list devices: %w", err)
				}
				for i := range devices {
					devices[i].AccountName = tailnet
					devices[i].AccountTailnet = tailnet
				}
				if len(devices) == 0 {
					fmt.Println("No devices found in the specified tailnet.")
					return nil
				}
				accounts = []client.AccountInfo{{Name: tailnet, APIKey: apiKey, Tailnet: tailnet, Active: true}}
			} else {
				// Check if Tailscale is running (warning only, doesn't block)
				WarnIfTailscaleNotRunning()

				fmt.Println("Fetching devices from all configured accounts...")
				accounts = make([]client.AccountInfo, len(cfg.Accounts))
				for i, acc := range cfg.Accounts {
					accounts[i] = client.AccountInfo{
						Name:    acc.Name,
						APIKey:  acc.APIKey,
						Tailnet: acc.Tailnet,
						Active:  acc.Active,
					}
				}

				devices = client.ListDevicesFromAccounts(accounts)
				if len(devices) == 0 {
					fmt.Println("No devices found in any of your configured accounts.")
					return nil
				}
				fmt.Printf("Found %d device(s) from %d account(s)\n", len(devices), len(cfg.Accounts))
			}

			m := tui.NewModel(devices, Version, cfg.SSHUsername, accounts, cfg.MachineCredentials, cfg.Commands, cfg.LastMachine, cfg.UI)
			p := tea.NewProgram(m, tea.WithAltScreen())
			if _, err := p.Run(); err != nil {
				return fmt.Errorf("TUI error: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&apiKey, "api-key", "", "Tailscale API key (overrides TAILSCALE_API_KEY env var)")
	cmd.Flags().StringVar(&tailnet, "tailnet", "", "Tailnet name (overrides stored configuration)")

	return cmd
}
