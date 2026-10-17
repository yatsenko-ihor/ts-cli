package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/config"
	"github.com/yatsenko-ihor/ts-cli/util"
)

// NewLoginCommand creates the login command
func NewLoginCommand() *cobra.Command {
	var apiKey string
	var tailnet string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Validate and configure Tailscale API authentication",
		Long: `Validate and store the Tailscale API key for authentication.
The API key can be provided via the TAILSCALE_API_KEY environment variable
or using the --api-key flag. Prefer the environment variable: flags are
visible in shell history and in the process list.`,
		Example: `  # Using environment variable (recommended)
  export TAILSCALE_API_KEY=tskey-api-xxxxx
  ts-cli login --tailnet=example.com

  # Using flag
  ts-cli login --api-key=tskey-api-xxxxx --tailnet=example.com`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Try to get API key from flag first, then environment variable
			if apiKey == "" {
				apiKey = os.Getenv("TAILSCALE_API_KEY")
			}

			if apiKey == "" {
				return fmt.Errorf("API key not provided.\nSet TAILSCALE_API_KEY environment variable or use --api-key flag")
			}

			if tailnet == "" {
				return fmt.Errorf("tailnet name is required.\nUse --tailnet flag to specify your tailnet name")
			}

			// Sanitize inputs
			apiKey = util.SanitizeInput(apiKey)
			tailnet = util.SanitizeInput(tailnet)

			// Validate inputs
			if err := util.ValidateAPIKey(apiKey); err != nil {
				return fmt.Errorf("invalid API key: %w", err)
			}

			if err := util.ValidateTailnet(tailnet); err != nil {
				return fmt.Errorf("invalid tailnet: %w", err)
			}

			// Validate the API key against the API before touching the config
			fmt.Println("Validating API key...")
			apiClient := client.NewClient(apiKey)

			if err := apiClient.ValidateAPIKey(tailnet); err != nil {
				return fmt.Errorf("failed to validate API key: %w", err)
			}

			fmt.Println("✓ API key is valid")

			// Read-modify-write under the config lock. A corrupt or unreadable
			// config is an error, never silently replaced.
			err := config.Update(func(cfg *config.Config) error {
				if existing := cfg.FindAccount(tailnet); existing != nil {
					existing.APIKey = apiKey
					existing.Tailnet = tailnet
					fmt.Printf("✓ Updated account: %s\n", existing.Name)
				} else {
					cfg.AddAccount(tailnet, apiKey, tailnet, false)
					fmt.Printf("✓ Added new account: %s\n", tailnet)
				}
				cfg.SetActiveAccount(tailnet)
				return nil
			})
			if err != nil {
				return fmt.Errorf("failed to store config: %w", err)
			}

			fmt.Println("✓ Configuration saved successfully")
			fmt.Printf("✓ Authenticated with tailnet: %s\n", tailnet)

			return nil
		},
	}

	cmd.Flags().StringVar(&apiKey, "api-key", "", "Tailscale API key (overrides TAILSCALE_API_KEY env var)")
	cmd.Flags().StringVar(&tailnet, "tailnet", "", "Tailnet name (e.g., example.com or user@example.com)")
	_ = cmd.MarkFlagRequired("tailnet")

	return cmd
}
