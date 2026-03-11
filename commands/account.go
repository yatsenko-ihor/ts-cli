package commands

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// NewAccountCommand creates the account management command
func NewAccountCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "account",
		Short: "Manage Tailscale account configurations",
		Long:  `View and manage configured Tailscale accounts for ts-cli.`,
	}

	cmd.AddCommand(newAccountListCommand())
	cmd.AddCommand(newAccountSetActiveCommand())
	cmd.AddCommand(newAccountRemoveCommand())

	return cmd
}

// newAccountListCommand creates the account list subcommand
func newAccountListCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all configured Tailscale accounts",
		Long:    `Display all Tailscale accounts configured in ts-cli.`,
		Example: `  # List all configured accounts
  ts-cli account list

  # Short alias
  ts-cli account ls`,
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := LoadConfig()
			if err != nil {
				return fmt.Errorf("failed to load configuration: %w", err)
			}

			if len(config.Accounts) == 0 {
				fmt.Println("No accounts configured.")
				fmt.Println("\nTo add an account, run:")
				fmt.Println("  ts-cli login --tailnet=<your-tailnet>")
				return nil
			}

			// Display accounts in a table
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tTAILNET\tACTIVE")
			fmt.Fprintln(w, "----\t-------\t------")

			for _, acc := range config.Accounts {
				active := ""
				if acc.Active {
					active = "✓"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n", acc.Name, acc.Tailnet, active)
			}

			w.Flush()
			fmt.Printf("\nTotal accounts: %d\n", len(config.Accounts))

			if config.SSHUsername != "" {
				fmt.Printf("Default SSH username: %s\n", config.SSHUsername)
			}

			return nil
		},
	}
}

// newAccountSetActiveCommand creates the account set-active subcommand
func newAccountSetActiveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set-active [account-name]",
		Short: "Set the active Tailscale account",
		Long:  `Set which Tailscale account should be marked as active.`,
		Example: `  # Set an account as active
  ts-cli account set-active personal.com

  # Set work account as active
  ts-cli account set-active work.example.com`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			accountName := args[0]

			config, err := LoadConfig()
			if err != nil {
				return fmt.Errorf("failed to load configuration: %w", err)
			}

			if len(config.Accounts) == 0 {
				return fmt.Errorf("no accounts configured")
			}

			// Find and set the account as active
			found := false
			for i := range config.Accounts {
				if config.Accounts[i].Name == accountName || config.Accounts[i].Tailnet == accountName {
					config.Accounts[i].Active = true
					found = true
					fmt.Printf("✓ Set '%s' as active account\n", config.Accounts[i].Name)
				} else {
					config.Accounts[i].Active = false
				}
			}

			if !found {
				return fmt.Errorf("account '%s' not found", accountName)
			}

			// Save configuration
			if err := SaveConfig(config); err != nil {
				return fmt.Errorf("failed to save configuration: %w", err)
			}

			return nil
		},
	}
}

// newAccountRemoveCommand creates the account remove subcommand
func newAccountRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "remove [account-name]",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a configured Tailscale account",
		Long:    `Remove a Tailscale account from ts-cli configuration.`,
		Example: `  # Remove an account
  ts-cli account remove personal.com

  # Remove using alias
  ts-cli account rm work.example.com`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			accountName := args[0]

			config, err := LoadConfig()
			if err != nil {
				return fmt.Errorf("failed to load configuration: %w", err)
			}

			if len(config.Accounts) == 0 {
				return fmt.Errorf("no accounts configured")
			}

			// Find and remove the account
			found := false
			newAccounts := []Account{}
			for _, acc := range config.Accounts {
				if acc.Name == accountName || acc.Tailnet == accountName {
					found = true
					fmt.Printf("✓ Removed account: %s\n", acc.Name)
				} else {
					newAccounts = append(newAccounts, acc)
				}
			}

			if !found {
				return fmt.Errorf("account '%s' not found", accountName)
			}

			config.Accounts = newAccounts

			// If we removed the active account, set the first one as active
			if len(config.Accounts) > 0 {
				hasActive := false
				for _, acc := range config.Accounts {
					if acc.Active {
						hasActive = true
						break
					}
				}
				if !hasActive {
					config.Accounts[0].Active = true
					fmt.Printf("Set '%s' as the new active account\n", config.Accounts[0].Name)
				}
			}

			// Save configuration
			if err := SaveConfig(config); err != nil {
				return fmt.Errorf("failed to save configuration: %w", err)
			}

			return nil
		},
	}
}
