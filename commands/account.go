package commands

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/yatsenko-ihor/ts-cli/config"
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
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load configuration: %w", err)
			}

			if len(cfg.Accounts) == 0 {
				fmt.Println("No accounts configured.")
				fmt.Println("\nTo add an account, run:")
				fmt.Println("  ts-cli login --tailnet=<your-tailnet>")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tTAILNET\tACTIVE")
			fmt.Fprintln(w, "----\t-------\t------")

			for _, acc := range cfg.Accounts {
				active := ""
				if acc.Active {
					active = "✓"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n", acc.Name, acc.Tailnet, active)
			}

			_ = w.Flush()
			fmt.Printf("\nTotal accounts: %d\n", len(cfg.Accounts))

			if cfg.SSHUsername != "" {
				fmt.Printf("Default SSH username: %s\n", cfg.SSHUsername)
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

			err := config.Update(func(cfg *config.Config) error {
				if len(cfg.Accounts) == 0 {
					return fmt.Errorf("no accounts configured")
				}
				if !cfg.SetActiveAccount(accountName) {
					return fmt.Errorf("account '%s' not found", accountName)
				}
				fmt.Printf("✓ Set '%s' as active account\n", cfg.GetActiveAccount().Name)
				return nil
			})
			if err != nil {
				return err
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

			return config.Update(func(cfg *config.Config) error {
				if len(cfg.Accounts) == 0 {
					return fmt.Errorf("no accounts configured")
				}
				acc := cfg.FindAccount(accountName)
				if acc == nil {
					return fmt.Errorf("account '%s' not found", accountName)
				}
				removedName := acc.Name
				wasActive := acc.Active
				cfg.RemoveAccount(accountName)
				fmt.Printf("✓ Removed account: %s\n", removedName)
				if wasActive {
					if active := cfg.GetActiveAccount(); active != nil {
						fmt.Printf("Set '%s' as the new active account\n", active.Name)
					}
				}
				return nil
			})
		},
	}
}
