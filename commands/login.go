package commands

import (
	"fmt"
	"os"

	"github.com/ihor/ts-cli/client"
	"github.com/ihor/ts-cli/util"
	"github.com/spf13/cobra"
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
or using the --api-key flag.`,
		Example: `  # Using environment variable
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

			// If tailnet not provided, return error
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

			// Validate the API key
			fmt.Println("Validating API key...")
			apiClient := client.NewClient(apiKey)

			if err := apiClient.ValidateAPIKey(tailnet); err != nil {
				return fmt.Errorf("failed to validate API key: %w", err)
			}

			fmt.Println("✓ API key is valid")

			// Load or create config
			config, err := LoadConfig()
			if err != nil {
				// If config doesn't exist, create a new one
				config = &Config{
					Accounts:    []Account{},
					SSHUsername: "",
				}
			}

			// Check if account already exists
			accountName := tailnet
			existingIdx := -1
			for i, acc := range config.Accounts {
				if acc.Name == accountName || acc.Tailnet == tailnet {
					existingIdx = i
					break
				}
			}

			if existingIdx >= 0 {
				// Update existing account
				config.Accounts[existingIdx].APIKey = apiKey
				config.Accounts[existingIdx].Tailnet = tailnet
				config.Accounts[existingIdx].Active = true
				fmt.Printf("✓ Updated account: %s\n", accountName)
			} else {
				// Add new account
				newAccount := Account{
					Name:    accountName,
					APIKey:  apiKey,
					Tailnet: tailnet,
					Active:  true,
				}
				config.Accounts = append(config.Accounts, newAccount)
				fmt.Printf("✓ Added new account: %s\n", accountName)
			}

			// Set this account as active
			config.SetActiveAccount(accountName)

			// Save the configuration
			if err := SaveConfig(config); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to store config: %s\n", err)
				fmt.Println("You can still use the API key via environment variable.")
				return nil
			}

			fmt.Println("✓ Configuration saved successfully")
			fmt.Printf("✓ Authenticated with tailnet: %s\n", tailnet)

			return nil
		},
	}

	// Define flags
	cmd.Flags().StringVar(&apiKey, "api-key", "", "Tailscale API key (overrides TAILSCALE_API_KEY env var)")
	cmd.Flags().StringVar(&tailnet, "tailnet", "", "Tailnet name (e.g., example.com or user@example.com)")
	cmd.MarkFlagRequired("tailnet")

	return cmd
}
