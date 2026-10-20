package commands

import (
	"github.com/spf13/cobra"
)

// NewLoginCommand creates the login command
func NewLoginCommand() *cobra.Command {
	var tailnet, name string
	var keys keyInput

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Add an account and make it active (same as `account add`)",
		Long: `Validate the Tailscale API key and store the account, making it active.
The key is read from --api-key-stdin, --api-key or the TAILSCALE_API_KEY
environment variable. Prefer stdin or the environment variable: flags are
visible in shell history and in the process list.`,
		Example: `  export TAILSCALE_API_KEY=tskey-api-xxxxx
  ts-cli login --tailnet=example.com`,
		RunE: func(cmd *cobra.Command, args []string) error {
			key, err := keys.resolve(cmd.InOrStdin())
			if err != nil {
				return err
			}
			return addAccount(cmd.OutOrStdout(), tailnet, name, key, true)
		},
	}

	cmd.Flags().StringVar(&tailnet, "tailnet", "", "Tailnet name (e.g., example.com or user@example.com)")
	cmd.Flags().StringVar(&name, "name", "", "display name (default: the tailnet)")
	keys.bind(cmd)
	_ = cmd.MarkFlagRequired("tailnet")

	return cmd
}
